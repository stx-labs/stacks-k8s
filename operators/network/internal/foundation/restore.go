package foundation

import (
	"errors"
	"fmt"
	"slices"
	"sort"
	"strings"

	common "github.com/cylewitruk-stacks/stacks-k8s/apis/network/common/v1alpha2"
	api "github.com/cylewitruk-stacks/stacks-k8s/apis/network/v1alpha2"
	"k8s.io/utils/ptr"
)

// Restored reports whether a frozen genesis was restored from captured chain data instead of
// initialized from genesis. Everything a restored network uses is frozen in the genesis, so no
// consumer reads the snapshot resource after freeze.
func Restored(genesis *api.StacksGenesis) bool {
	return genesis.Spec.Source.Restored != nil
}

// RestoredFrom returns the binding of the snapshot a restored genesis pins, as evidence.
func RestoredFrom(genesis *api.StacksGenesis) (common.Binding, bool) {
	for _, b := range genesis.Spec.Source.Dependencies {
		if b.Kind == api.KindStacksChainstateSnapshot {
			return b, Restored(genesis)
		}
	}
	return common.Binding{}, false
}

// restoreMember pairs one initial cohort requirement with its compiled policy.
type restoreMember struct {
	requirement   api.BootstrapRequirement
	configuration api.Configuration
}

// restoreMismatchError lists every difference between a restoring network and its snapshot.
type restoreMismatchError struct {
	snapshot string
	problems []string
}

// genesisFailureReason reports a restore mismatch as SnapshotMismatch, so it shows even on a
// paused network, and any other compile failure as unavailable inputs.
func genesisFailureReason(err error) string {
	var mismatch *restoreMismatchError
	if errors.As(err, &mismatch) {
		return reasonSnapshotMismatch
	}
	return reasonGenesisInputsUnavailable
}

func (e *restoreMismatchError) Error() string {
	const shown = 8
	problems, more := e.problems, ""
	if len(problems) > shown {
		problems, more = problems[:shown], fmt.Sprintf("; and %d more", len(problems)-shown)
	}
	return fmt.Sprintf("snapshot %s does not match: %s%s", e.snapshot, strings.Join(problems, "; "), more)
}

// snapshotParticipant projects a cohort member onto the values restored data depends on: public
// identities by the role that selects them, links to other participants, mining role, stake and
// registry threshold. Allocation identities (UIDs) and whole-policy digests are excluded, so
// resources, overrides and placement may differ between the source and the restoring network.
// The payout wallet is excluded too: it only receives new coinbase, and a restored Bitcoin node
// imports the restoring network's wallet. The capture tool implements the same projection.
func snapshotParticipant(name string, m restoreMember) api.SnapshotParticipant {
	req, c := m.requirement, m.configuration
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
			p.Accounts = append(p.Accounts, api.SnapshotAccount{Role: role, PublicIdentity: identity})
		}
	}
	// The capture tool keeps its own copy of this projection (tools/stacks-snapshot/cohort.go,
	// projectParticipant); both must change together.
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
			p.RegistryThreshold = ptr.To(registry.Threshold)
		}
	}
	p.Accounts = normalizedAccounts(p.Accounts)
	return p
}

// normalizedAccounts lower-cases public keys, sorts by role and address and drops duplicates, so
// two projections of the same identities compare equal.
func normalizedAccounts(accounts []api.SnapshotAccount) []api.SnapshotAccount {
	out := make([]api.SnapshotAccount, 0, len(accounts))
	for _, a := range accounts {
		a.PublicKey = strings.ToLower(a.PublicKey)
		out = append(out, a)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Role != out[j].Role {
			return out[i].Role < out[j].Role
		}
		return out[i].Address < out[j].Address
	})
	unique := out[:0]
	for i, a := range out {
		if i == 0 || a != out[i-1] {
			unique = append(unique, a)
		}
	}
	return unique
}

func sameAccounts(a, b []api.SnapshotAccount) bool {
	a, b = normalizedAccounts(a), normalizedAccounts(b)
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// actorImage returns an actor's compiled image, or empty for worker participants.
func actorImage(c api.Configuration) string {
	switch {
	case c.BitcoinNode != nil:
		return ptr.Deref(c.BitcoinNode.Image, "")
	case c.StacksNode != nil:
		return ptr.Deref(c.StacksNode.Image, "")
	case c.StacksSigner != nil:
		return ptr.Deref(c.StacksSigner.Image, "")
	}
	return ""
}

// validateRestore checks that a restoring network reproduces the snapshot's chain configuration,
// its exact initial cohort and the actor images that wrote the captured data, and that the
// snapshot was taken past the final initialization ceiling: restore completes every gate, so a
// snapshot of a network still bootstrapping could never initialize. With AllowDifferent, Stacks
// nodes and signers may run other images. On success it returns the facts the genesis freezes:
// the restored chainstate and each actor's archive by cohort name.
func validateRestore(
	snapshot *api.StacksChainstateSnapshot,
	genesisDigest string,
	finalCeiling int64,
	cohort map[string]restoreMember,
	policy api.StacksImagePolicy,
) (*api.RestoredChainstate, map[string]api.SnapshotArchive, error) {
	var problems []string
	var changes []api.RestoredImageChange
	archives := map[string]api.SnapshotArchive{}
	if genesisDigest != snapshot.Spec.GenesisDigest {
		problems = append(
			problems,
			fmt.Sprintf("genesis digest %s differs from %s", genesisDigest, snapshot.Spec.GenesisDigest),
		)
	}
	if height := snapshot.Spec.Bitcoin.Height; height < finalCeiling {
		problems = append(problems, fmt.Sprintf(
			"Bitcoin height %d is below the final initialization ceiling %d; capture an Initialized network",
			height, finalCeiling,
		))
	}
	recorded := map[string]bool{}
	for _, want := range snapshot.Spec.Participants {
		recorded[want.Name] = true
		member, ok := cohort[want.Name]
		if !ok {
			problems = append(problems, fmt.Sprintf("%s %s is missing from the initial cohort", want.Kind, want.Name))
			continue
		}
		got := snapshotParticipant(want.Name, member)
		if got.Kind != want.Kind {
			problems = append(problems, fmt.Sprintf("%s is %s, not %s", want.Name, got.Kind, want.Kind))
			continue
		}
		if !sameAccounts(got.Accounts, want.Accounts) {
			problems = append(problems, fmt.Sprintf("%s public identities or their roles differ", want.Name))
		}
		if got.BitcoinNode != want.BitcoinNode || got.Node != want.Node || got.Signer != want.Signer {
			problems = append(problems, fmt.Sprintf("%s links to other participants differ", want.Name))
		}
		if !ptr.Equal(got.MiningEnabled, want.MiningEnabled) {
			problems = append(problems, fmt.Sprintf("%s mining role differs", want.Name))
		}
		if !ptr.Equal(got.AmountMicroSTX, want.AmountMicroSTX) || !ptr.Equal(got.LockCycles, want.LockCycles) {
			problems = append(problems, fmt.Sprintf("%s stake differs", want.Name))
		}
		if !ptr.Equal(got.RegistryThreshold, want.RegistryThreshold) {
			problems = append(problems, fmt.Sprintf("%s registry threshold differs", want.Name))
		}
		if !slices.Equal(got.RegistrySigners, want.RegistrySigners) {
			problems = append(problems, fmt.Sprintf("%s registry signer order differs", want.Name))
		}
		if want.Archive != nil {
			archives[want.Name] = *want.Archive
			image := actorImage(member.configuration)
			switch {
			case image == want.Archive.Image:
			case policy == api.StacksImagePolicyAllowDifferent && got.Kind != api.ParticipantBitcoinNode:
				changes = append(changes, api.RestoredImageChange{
					Participant:   want.Name,
					SnapshotImage: want.Archive.Image,
					Image:         image,
				})
			default:
				problem := fmt.Sprintf("%s image %s differs from %s, which wrote the data", want.Name, image,
					want.Archive.Image)
				if got.Kind != api.ParticipantBitcoinNode {
					problem += " (stacksImagePolicy AllowDifferent accepts another Stacks image)"
				}
				problems = append(problems, problem)
			}
		}
	}
	extra := []string{}
	for name := range cohort {
		if !recorded[name] {
			extra = append(extra, name)
		}
	}
	sort.Strings(extra)
	for _, name := range extra {
		problems = append(
			problems,
			fmt.Sprintf(
				"%s %s is not in the snapshot; add it after initialization",
				cohort[name].requirement.Kind,
				name,
			),
		)
	}
	if len(problems) > 0 {
		return nil, nil, &restoreMismatchError{snapshot: snapshot.Name, problems: problems}
	}
	sort.Slice(changes, func(i, j int) bool { return changes[i].Participant < changes[j].Participant })
	return &api.RestoredChainstate{
		Image:        snapshot.Spec.Image,
		Bitcoin:      snapshot.Spec.Bitcoin,
		ImageChanges: changes,
	}, archives, nil
}
