package main

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	common "github.com/cylewitruk-stacks/stacks-k8s/apis/network/common/v1alpha2"
	stacks "github.com/cylewitruk-stacks/stacks-k8s/apis/network/stacks/v1alpha2"
	api "github.com/cylewitruk-stacks/stacks-k8s/apis/network/v1alpha2"
	"k8s.io/utils/ptr"
)

// member is one frozen initial cohort requirement with its participant and accounts.
type member struct {
	requirement api.BootstrapRequirement
	participant api.StacksNetworkParticipant
	accounts    []stacks.StacksAccount
}

// archived reports whether a participant kind owns chain data on a volume.
func archived(kind api.ParticipantKind) bool {
	return kind == api.ParticipantBitcoinNode || kind == api.ParticipantStacksNode ||
		kind == api.ParticipantStacksSigner
}

// project records what a restoring network must reproduce, the same projection restore
// validation compares: names, kinds, public identities by role, links between participants,
// mining role, stake, registry threshold and actor images. Every bound account must use an
// imported key, including worker accounts that are not compared: genesis funding puts them in
// the genesis digest. Checksums are filled in after export.
func project(members []member) ([]api.SnapshotParticipant, error) {
	var generated, problems []string
	out := make([]api.SnapshotParticipant, 0, len(members))
	for _, m := range members {
		generated = append(generated, generatedKeys(m.accounts)...)
		if m.participant.Status.Admission == nil {
			problems = append(problems, fmt.Sprintf("%s has no admitted configuration",
				m.participant.Spec.ParticipantName))
			continue
		}
		p := projectParticipant(
			m.participant.Spec.ParticipantName,
			m.requirement,
			m.participant.Status.Admission.Configuration,
		)
		if archived(p.Kind) {
			image := actorImage(&m.participant)
			if image == "" {
				problems = append(problems, fmt.Sprintf("%s has no admitted actor image", p.Name))
			}
			// Checked before the source is stopped: stopping deletes an emptyDir, and a
			// stopped network cannot be resumed.
			if f := actorFields(&m.participant); f != nil && f.Storage != nil && ptr.Deref(f.Storage.Ephemeral, false) {
				problems = append(problems, fmt.Sprintf("%s uses ephemeral storage, which cannot be exported", p.Name))
			}
			p.Archive = &api.SnapshotArchive{Path: p.Name + ".tar.gz", Image: image}
		}
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	if err := refuseGenerated(generated); err != nil {
		return nil, err
	}
	if len(problems) > 0 {
		return nil, errors.New(strings.Join(problems, "; "))
	}
	return out, nil
}

// generatedKeys names the accounts whose keys a restoring network would generate anew.
func generatedKeys(accounts []stacks.StacksAccount) []string {
	var out []string
	for _, a := range accounts {
		// Omission on an account means generation.
		if a.Spec.Key == nil || a.Spec.Key.Generate != nil {
			out = append(out, a.Name)
		}
	}
	return out
}

// refuseGenerated reports the named accounts, if any, as unreproducible.
func refuseGenerated(names []string) error {
	if len(names) == 0 {
		return nil
	}
	sort.Strings(names)
	return fmt.Errorf(
		"accounts %s use generated keys, which a restoring network cannot reproduce; "+
			"import their keys (key.secretRef) in the source network",
		strings.Join(unique(names), ", "),
	)
}

// lateConsensus refuses a source whose signer set may depend on participants outside the frozen
// initial cohort. A snapshot restores only that cohort: signers and stackers that joined later are
// missing from the restored network, and their weight can keep it below the signing threshold. A
// removed participant outside the cohort is refused too: its kind is no longer known, and stake it
// locked stays in the signer set until it unlocks.
func lateConsensus(root *api.StacksNetwork, members []member) error {
	initial := map[string]bool{}
	for _, m := range members {
		initial[m.participant.Spec.ParticipantName] = true
	}
	selected := map[string]bool{}
	var late, removed []string
	for _, entry := range root.Spec.Participants {
		selected[entry.Name] = true
		if !initial[entry.Name] &&
			(entry.Kind == api.ParticipantStacksSigner || entry.Kind == api.ParticipantStacksStacker) {
			late = append(late, entry.Name)
		}
	}
	for _, id := range root.Status.Identities {
		if !initial[id.Name] && (!selected[id.Name] || id.Removing) {
			removed = append(removed, id.Name)
		}
	}
	var problems []string
	if len(late) > 0 {
		sort.Strings(late)
		problems = append(problems, fmt.Sprintf(
			"signers or stackers %s joined after initialization and would be missing from a restored network",
			strings.Join(late, ", ")))
	}
	if len(removed) > 0 {
		sort.Strings(removed)
		problems = append(problems, fmt.Sprintf(
			"participants %s were removed after initialization and may still hold locked stake",
			strings.Join(removed, ", ")))
	}
	if len(problems) > 0 {
		return fmt.Errorf("the signer set may depend on participants outside the initial cohort: %s; "+
			"capture a network whose consensus participants are all initial", strings.Join(problems, "; "))
	}
	return nil
}

// initialStake refuses a source whose stackers changed their stake after initialization. The
// snapshot records the stake frozen in the genesis, so a restoring network that declares the
// source's current stake fails validation, and one that declares the initial stake starts its
// stacker against a lock it must wait out. The admitted policy is compared, not the on-chain lock.
func initialStake(members []member) error {
	var changed []string
	for _, m := range members {
		req, admission := m.requirement, m.participant.Status.Admission
		if req.Kind != api.ParticipantStacksStacker || admission == nil {
			continue
		}
		s := admission.Configuration.StacksStacker
		if s == nil || !ptr.Equal(s.AmountMicroSTX, req.AmountMicroSTX) || !ptr.Equal(s.LockCycles, req.LockCycles) {
			changed = append(changed, fmt.Sprintf("%s (initial %s, now %s)",
				m.participant.Spec.ParticipantName, stake(req.AmountMicroSTX, req.LockCycles), currentStake(s)))
		}
	}
	if len(changed) == 0 {
		return nil
	}
	sort.Strings(changed)
	return fmt.Errorf("stackers changed their stake after initialization: %s; a snapshot records the "+
		"initial stake, so capture a network whose stackers keep it", strings.Join(changed, ", "))
}

// stake formats a stake policy for a refusal.
func stake(amount *common.Amount, cycles *int32) string {
	return fmt.Sprintf("%s micro-STX for %d cycles", ptr.Deref(amount, "unset"), ptr.Deref(cycles, 0))
}

// currentStake formats an admitted stacker policy, which may be missing.
func currentStake(s *stacks.StacksStackerSpec) string {
	if s == nil {
		return "no stacker policy"
	}
	return stake(s.AmountMicroSTX, s.LockCycles)
}

// projectParticipant projects one cohort member exactly as the operator's restore validation does
// (operators/network/internal/foundation/restore.go, snapshotParticipant): accounts by the role
// that selects them, public keys in lower case, sorted by role and address.
func projectParticipant(name string, req api.BootstrapRequirement, c api.Configuration) api.SnapshotParticipant {
	p := api.SnapshotParticipant{
		Name:           name,
		Kind:           req.Kind,
		MiningEnabled:  req.MiningEnabled,
		AmountMicroSTX: req.AmountMicroSTX,
		LockCycles:     req.LockCycles,
	}
	identities := map[string]common.PublicIdentity{}
	for _, a := range req.Accounts {
		identities[a.Binding.Name] = a.Identity
	}
	add := func(role api.SnapshotAccountRole, ref *common.NameRef) {
		if ref == nil {
			return
		}
		if identity, ok := identities[ref.Name]; ok {
			identity.PublicKey = strings.ToLower(identity.PublicKey)
			p.Accounts = append(p.Accounts, api.SnapshotAccount{Role: role, PublicIdentity: identity})
		}
	}
	link := func(ref *common.NameRef) string {
		if ref == nil {
			return ""
		}
		return ref.Name
	}
	switch {
	case c.StacksNode != nil:
		add(api.SnapshotAccountRoleIdentity, c.StacksNode.IdentityAccountRef)
		p.BitcoinNode = link(c.StacksNode.BitcoinNodeRef)
	case c.StacksSigner != nil:
		add(api.SnapshotAccountRoleSigner, c.StacksSigner.AccountRef)
		p.Node = link(c.StacksSigner.NodeRef)
	case c.StacksStacker != nil:
		add(api.SnapshotAccountRoleHolder, c.StacksStacker.HolderAccountRef)
		add(api.SnapshotAccountRoleAdministrator, c.StacksStacker.AdministratorAccountRef)
		p.Signer = link(c.StacksStacker.SignerRef)
	case c.StacksContractSet != nil:
		add(api.SnapshotAccountRoleDeployer, c.StacksContractSet.DeployerAccountRef)
		if registry := c.StacksContractSet.Initialization; registry != nil {
			for i := range registry.SignerAccountRefs {
				add(api.SnapshotAccountRoleRegistrySigner, &registry.SignerAccountRefs[i])
				// Accounts are sorted; the registry order is kept apart.
				if identity, ok := identities[registry.SignerAccountRefs[i].Name]; ok {
					p.RegistrySigners = append(p.RegistrySigners, identity.Address)
				}
			}
			add(api.SnapshotAccountRoleAggregateKey, &registry.AggregateKeyAccountRef)
			threshold := registry.Threshold
			p.RegistryThreshold = &threshold
		}
	}
	sort.Slice(p.Accounts, func(i, j int) bool {
		if p.Accounts[i].Role != p.Accounts[j].Role {
			return p.Accounts[i].Role < p.Accounts[j].Role
		}
		return p.Accounts[i].Address < p.Accounts[j].Address
	})
	unique := p.Accounts[:0]
	for i, a := range p.Accounts {
		if i == 0 || a != p.Accounts[i-1] {
			unique = append(unique, a)
		}
	}
	p.Accounts = unique
	return p
}

// actorImage returns the admitted image that writes an actor's data, or empty.
func actorImage(p *api.StacksNetworkParticipant) string {
	if f := actorFields(p); f != nil && f.Image != nil {
		return *f.Image
	}
	return ""
}

// actorFields returns the admitted actor policy of a Bitcoin node, Stacks node or signer.
func actorFields(p *api.StacksNetworkParticipant) *common.ActorFields {
	if p.Status.Admission == nil {
		return nil
	}
	switch c := p.Status.Admission.Configuration; {
	case c.BitcoinNode != nil:
		return &c.BitcoinNode.ActorFields
	case c.StacksNode != nil:
		return &c.StacksNode.ActorFields
	case c.StacksSigner != nil:
		return &c.StacksSigner.ActorFields
	}
	return nil
}

func unique(sorted []string) []string {
	out := sorted[:0]
	for i, s := range sorted {
		if i == 0 || s != sorted[i-1] {
			out = append(out, s)
		}
	}
	return out
}

// accountBindings returns the distinct StacksAccounts a requirement binds.
func accountBindings(req api.BootstrapRequirement) []common.Binding {
	var out []common.Binding
	seen := map[string]bool{}
	for _, a := range req.Accounts {
		if a.Binding.Kind == stacks.KindStacksAccount && !seen[a.Binding.Name] {
			seen[a.Binding.Name] = true
			out = append(out, a.Binding)
		}
	}
	return out
}
