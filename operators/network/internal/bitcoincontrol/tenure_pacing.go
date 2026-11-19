package bitcoincontrol

import (
	"time"

	bitcoin "github.com/cylewitruk-stacks/stacks-k8s/apis/network/bitcoin/v1alpha2"
	api "github.com/cylewitruk-stacks/stacks-k8s/apis/network/v1alpha2"
)

// tenurePacingWait bounds how long bootstrap advancement waits for a block's tenure.
const tenurePacingWait = 30 * time.Second

// pacingDecision is either a wait for tenure progress or an outcome; neither when pacing does not apply.
type pacingDecision struct {
	outcome   bitcoin.TenurePacingOutcome
	waiting   bool
	remaining time.Duration
}

// paceTenure decides whether bootstrap may advance past tip after epoch 3.0 starts at start.
// Advancement waits until every captured miner holds a canonical tip from the tenure tip
// elected, or until tenurePacingWait after the receipt that generated tip. It never relaxes
// any other advancement check.
func paceTenure(
	root *api.StacksNetwork,
	miners []*api.ParticipantRuntimeStatus,
	receipt *bitcoin.BitcoinRPCReceipt,
	start, height int64,
	tip string,
	now time.Time,
) pacingDecision {
	if height < start || len(miners) == 0 {
		return pacingDecision{}
	}
	if receipt == nil || receipt.Request.Method != bitcoin.RPCGenerate || receipt.BlockHash != tip {
		return pacingDecision{outcome: bitcoin.TenurePacingUnbound}
	}
	progressed := true
	for _, miner := range miners {
		progressed = progressed && tenureObserved(root, miner, height, tip, now)
	}
	if progressed {
		return pacingDecision{outcome: bitcoin.TenurePacingProgressed}
	}
	if deadline := receipt.ReceivedAt.Add(tenurePacingWait); now.Before(deadline) {
		return pacingDecision{waiting: true, remaining: deadline.Sub(now)}
	}
	return pacingDecision{outcome: bitcoin.TenurePacingExpired}
}

// tenureObserved requires a fresh view whose canonical tip belongs to the tenure that the
// producer's exact block elected, on a node whose processed Bitcoin tip is that block.
func tenureObserved(
	root *api.StacksNetwork,
	runtime *api.ParticipantRuntimeStatus,
	height int64,
	tip string,
	now time.Time,
) bool {
	view := currentProtocolView(root, runtime, now)
	return view != nil && view.Available && view.Tenure != nil && height >= 0 &&
		view.BurnHeight == uint64(height) &&
		view.Tenure.BurnHeight == uint64(height) &&
		view.Tenure.BurnBlockHash == tip &&
		view.Tenure.ConsensusHash == view.BurnConsensusHash
}

// recordPacing counts a decided outcome once per producer block and reports whether it did.
func recordPacing(
	record *bitcoin.BitcoinInitialization,
	outcome bitcoin.TenurePacingOutcome,
	height int64,
	tip string,
	now time.Time,
) bool {
	previous := record.Status.TenurePacing
	if outcome == "" || previous != nil && previous.Tip == tip && previous.Height == height {
		return false
	}
	pacing := bitcoin.BitcoinTenurePacing{}
	if previous != nil {
		pacing = *previous
	}
	pacing.Tip, pacing.Height, pacing.Outcome, pacing.DelayMilliseconds = tip, height, outcome, 0
	if due := record.Status.NextOpportunityAt; due != nil && now.After(due.Time) {
		pacing.DelayMilliseconds = now.Sub(due.Time).Milliseconds()
	}
	switch outcome {
	case bitcoin.TenurePacingProgressed:
		pacing.Progressed++
	case bitcoin.TenurePacingExpired:
		pacing.Expired++
	case bitcoin.TenurePacingUnbound:
		pacing.Unbound++
	}
	record.Status.TenurePacing = &pacing
	return true
}
