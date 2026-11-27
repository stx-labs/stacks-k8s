package main

import (
	"fmt"
	"sort"
	"strings"
)

// targetScope says how a reference name resolves.
type targetScope string

const (
	scopeParticipant targetScope = "participant in the same network"
	scopeResource    targetScope = "same-namespace resource"
	scopeSecret      targetScope = "same-namespace Secret key"
	scopeDefinition  targetScope = "same-namespace definition of the entry's kind"
	scopeNetworkUID  targetScope = "participant of the network named by `spec.networkUID`"
)

// target is the kind and resolution scope of one reference field.
type target struct {
	Kind  string
	Scope targetScope
}

// generatedKinds are controller-written resources; users never apply them.
var generatedKinds = map[string]bool{
	"BitcoinExecution":         true,
	"BitcoinInitialization":    true,
	"StacksGenesis":            true,
	"StacksNetworkParticipant": true,
}

// referenceTargets declares what each user-authored reference field names. Every reference
// found in a user-authored schema must appear here, and every entry must still exist.
var referenceTargets = map[string]target{
	"BitcoinBlockProduction spec.initialization.minerWalletRefs": {"BitcoinWallet", scopeResource},
	"BitcoinBlockProduction spec.initialization.targetNodeRef":   {"BitcoinNode", scopeParticipant},
	"BitcoinBlockProduction spec.payoutWalletRef":                {"BitcoinWallet", scopeResource},
	"BitcoinBlockProduction spec.scheduleRef":                    {"BitcoinBlockSchedule", scopeResource},
	"BitcoinBlockProduction spec.targets[].nodeRef":              {"BitcoinNode", scopeParticipant},
	"BitcoinBlockGeneration spec.bitcoinNodeRef":                 {"BitcoinNode", scopeNetworkUID},
	"BitcoinBlockScheduleOverride spec.productionRef":            {"BitcoinBlockProduction", scopeNetworkUID},
	"BitcoinBlockScheduleOverride spec.scheduleRef":              {"BitcoinBlockSchedule", scopeResource},
	"BitcoinNode spec.config.secretRef":                          {"Secret", scopeSecret},
	"BitcoinNode spec.peers.nodeRefs":                            {"BitcoinNode", scopeParticipant},
	"BitcoinNode spec.walletRefs":                                {"BitcoinWallet", scopeResource},
	"BitcoinReorganization spec.bitcoinNodeRef":                  {"BitcoinNode", scopeNetworkUID},
	"BitcoinWallet spec.keySource.secretRef":                     {"Secret", scopeSecret},
	"BitcoinWallet spec.keySource.stacksMinerAccountRef":         {"StacksAccount", scopeResource},
	"NetworkObservation spec.networkRef":                         {"StacksNetwork", scopeResource},
	"StacksAccount spec.key.secretRef":                           {"Secret", scopeSecret},
	"StacksContractSet spec.deployerAccountRef":                  {"StacksAccount", scopeResource},
	"StacksContractSet spec.initialization.aggregateKeyAccountRef": {
		"StacksAccount", scopeResource,
	},
	"StacksContractSet spec.initialization.signerAccountRefs": {"StacksAccount", scopeResource},
	"StacksContractSet spec.targetNodeRef":                    {"StacksNode", scopeParticipant},
	"StacksFaucet spec.accountRef":                            {"StacksAccount", scopeResource},
	"StacksFaucet spec.targetNodeRef":                         {"StacksNode", scopeParticipant},
	"StacksFaucetRequest spec.destination.accountRef":         {"StacksAccount", scopeResource},
	"StacksFaucetRequest spec.faucetRef":                      {"StacksFaucet", scopeNetworkUID},
	"StacksNetwork spec.epochScheduleRef":                     {"StacksEpochSchedule", scopeResource},
	"StacksNetwork spec.genesis.allocations[].accountRef":     {"StacksAccount", scopeResource},
	"StacksNetwork spec.participants[].definition.ref":        {"entry `kind`", scopeDefinition},
	"StacksNode spec.bitcoinNodeRef":                          {"BitcoinNode", scopeParticipant},
	"StacksNode spec.config.secretRef":                        {"Secret", scopeSecret},
	"StacksNode spec.identityAccountRef":                      {"StacksAccount", scopeResource},
	"StacksNode spec.mining.bitcoinWalletRef":                 {"BitcoinWallet", scopeResource},
	"StacksNode spec.peers.nodeRefs":                          {"StacksNode", scopeParticipant},
	"StacksSigner spec.accountRef":                            {"StacksAccount", scopeResource},
	"StacksSigner spec.config.secretRef":                      {"Secret", scopeSecret},
	"StacksSigner spec.nodeRef":                               {"StacksNode", scopeParticipant},
	"StacksStacker spec.administratorAccountRef":              {"StacksAccount", scopeResource},
	"StacksStacker spec.holderAccountRef":                     {"StacksAccount", scopeResource},
	"StacksStacker spec.signerRef":                            {"StacksSigner", scopeParticipant},
	"StacksStacker spec.targetNodeRef":                        {"StacksNode", scopeParticipant},
	"StacksTransactionProduction spec.accountRef":             {"StacksAccount", scopeResource},
	"StacksTransactionProduction spec.recipient.accountRef":   {"StacksAccount", scopeResource},
	"StacksTransactionProduction spec.targetNodeRef":          {"StacksNode", scopeParticipant},
}

// resolvedReference is one reference with its declared target.
type resolvedReference struct {
	From reference
	To   target
}

// resolveReferences pairs schema references with declared targets, failing on any gap in either direction.
func resolveReferences(found []reference) ([]resolvedReference, error) {
	var problems []string
	seen := map[string]bool{}
	var out []resolvedReference
	for _, ref := range found {
		if generatedKinds[ref.Kind] {
			continue
		}
		key := ref.Kind + " " + ref.Path
		seen[key] = true
		declared, ok := referenceTargets[key]
		if !ok {
			problems = append(problems, "undeclared reference target: "+key)
			continue
		}
		out = append(out, resolvedReference{From: ref, To: declared})
	}
	for key := range referenceTargets {
		if !seen[key] {
			problems = append(problems, "declared reference no longer in schema: "+key)
		}
	}
	if len(problems) > 0 {
		sort.Strings(problems)
		return nil, fmt.Errorf("reference targets out of date:\n  %s", strings.Join(problems, "\n  "))
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].From.Kind != out[j].From.Kind {
			return out[i].From.Kind < out[j].From.Kind
		}
		return out[i].From.Path < out[j].From.Path
	})
	return out, nil
}

// renderGraph renders the generated reference table.
func renderGraph(references []resolvedReference) string {
	var b strings.Builder
	b.WriteString("Generated from the chart CRDs; do not edit.\n\n")
	b.WriteString("| From | Field | Names a | Resolved as |\n")
	b.WriteString("| --- | --- | --- | --- |\n")
	for _, ref := range references {
		to := ref.To.Kind
		if !strings.Contains(to, "`") && to != "Secret" {
			to = fmt.Sprintf("[%s](%s)", to, pageName(to))
		}
		b.WriteString(tableRow(
			fmt.Sprintf("[%s](%s)", ref.From.Kind, pageName(ref.From.Kind)),
			"`"+ref.From.Path+"`",
			to,
			string(ref.To.Scope),
		))
	}
	return b.String()
}

func pageName(kind string) string {
	return strings.ToLower(kind) + ".md"
}
