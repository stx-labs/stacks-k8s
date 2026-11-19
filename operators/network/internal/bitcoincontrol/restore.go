package bitcoincontrol

import (
	"context"
	"errors"
	"math"
	"time"

	bitcoin "github.com/cylewitruk-stacks/stacks-k8s/apis/network/bitcoin/v1alpha2"
	api "github.com/cylewitruk-stacks/stacks-k8s/apis/network/v1alpha2"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/foundation"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/objectref"
	"k8s.io/apimachinery/pkg/api/equality"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// restoredChainstate returns the restore facts frozen in the network's exact genesis, or nil for a
// network initialized from genesis. It fails when the genesis cannot be established, so a failed
// read never selects the bootstrap path for a restored chain.
func restoredChainstate(
	ctx context.Context,
	reader client.Reader,
	root *api.StacksNetwork,
	initial *bitcoin.BitcoinInitialization,
) (*api.RestoredChainstate, error) {
	ref := root.Status.GenesisRef
	if ref == nil || *ref != initial.Spec.Genesis {
		return nil, errors.New("frozen genesis binding unavailable")
	}
	var genesis api.StacksGenesis
	if err := reader.Get(ctx, client.ObjectKey{Namespace: root.Namespace, Name: ref.Name}, &genesis); err != nil {
		return nil, err
	}
	if genesis.UID != ref.UID || genesis.DeletionTimestamp != nil || !metav1.IsControlledBy(&genesis, root) ||
		foundation.Digest(genesis.Spec) != ref.Fingerprint {
		return nil, errors.New("frozen genesis identity differs")
	}
	return genesis.Spec.Source.Restored, nil
}

// reconcileRestore prepares Bitcoin for a network restored from a chainstate snapshot. Instead of
// generating up to the first gate, it proves the restored chain (the snapshot's exact height and
// tip, ready wallets and a mature output per miner), then generates exactly one block: stacks-node
// refuses a burnchain tip older than two hours, and a snapshot is usually older. Only then does it
// record preparation, which lets the Stacks actors start on a fresh tip.
func (s *Scheduler) reconcileRestore(
	ctx context.Context,
	root *api.StacksNetwork,
	record *bitcoin.BitcoinInitialization,
	chainstate *api.RestoredChainstate,
	report func(bitcoin.InitializationPhase, string) (ctrl.Result, error),
) (ctrl.Result, error) {
	record.Status.NextOpportunityAt = nil
	if record.Status.PreparedAt != nil {
		return report(bitcoin.InitializationHeld, reasonRestorePrepared)
	}
	production, e := s.currentProduction(ctx, root, record)
	if e != nil {
		return report(bitcoin.InitializationBlocked, reasonProductionIdentityUnavailable)
	}
	current := objectref.Participant(production)
	record.Status.Production = &current
	if root.Spec.Operation == api.NetworkOperationPaused ||
		production.Spec.Control != nil && ptr.Deref(production.Spec.Control.Paused, false) {
		return report(bitcoin.InitializationPaused, api.ReasonDesiredPause)
	}
	allRecords, reason := s.executions(ctx, root)
	if reason != "" {
		return report(bitcoin.InitializationBlocked, reason)
	}
	restored := chainstate.Bitcoin
	if target := allRecords[string(record.Spec.Target.UID)]; target != nil {
		accountRestoreReceipt(root, record, target, restored)
	}
	view, phase, reason := s.converge(ctx, root, record, allRecords)
	if reason != "" {
		return report(phase, reason)
	}
	if !restoredMinersFunded(record, view.mature) {
		return report(bitcoin.InitializationBlocked, reasonRestoredWalletsImmature)
	}
	// Before preparation only the refresh receipt is ever accounted, so an accounted offer means the
	// refresh block landed, whichever offer is current: an override change or a re-issued offer
	// cleared or replaced since then must not hide it.
	if record.Status.LastAccountedOffer > 0 {
		if view.height == restored.Height && view.tip == restored.BlockHash {
			return report(bitcoin.InitializationWaiting, reasonAwaitingPostReceiptObservation)
		}
		if view.height != restored.Height+1 || !refreshAtTip(view, record) {
			return report(bitcoin.InitializationBlocked, reasonRestoredChainDiffers)
		}
		now := metav1.NewTime(s.Now().UTC())
		record.Status.PreparedAt = &now
		if record.Status.FirstCeilingObservedAt == nil {
			record.Status.FirstCeilingObservedAt = &now
		}
		// Withdraw any re-issued refresh: the worker must not generate on the refreshed tip.
		record.Status.Offer = nil
		return report(bitcoin.InitializationHeld, reasonRestorePrepared)
	}
	if view.height != restored.Height || view.tip != restored.BlockHash {
		return report(bitcoin.InitializationBlocked, reasonRestoredChainDiffers)
	}
	offer := record.Status.Offer
	if offer != nil && offer.Mode == bitcoin.OfferRestore && offer.Number > record.Status.LastAccountedOffer &&
		s.Now().Before(offer.ExpiresAt.Time) && offer.Production == current &&
		offer.PolicyDigest == production.Status.Admission.PolicyDigest {
		if !equality.Semantic.DeepEqual(view.target.Spec.Offer, offer) {
			view.target.Spec.Offer = offer.DeepCopy()
			if e := s.Client.Update(ctx, view.target); e != nil {
				return ctrl.Result{}, e
			}
		}
		return report(
			bitcoin.InitializationPreparing,
			refreshReason(record, offer.Number, reasonOpportunityOutstanding),
		)
	}
	if e := foundation.ValidateAdmissionEligibility(ctx, s.Reader, production); e != nil {
		record.Status.Offer = nil
		return report(bitcoin.InitializationBlocked, reasonProductionSourceUnavailable)
	}
	number := max(record.Status.LastAccountedOffer, view.target.Status.CompletedOffer)
	if offer != nil {
		number = max(number, offer.Number)
	}
	if view.target.Spec.Offer != nil {
		number = max(number, view.target.Spec.Offer.Number)
	}
	if number == math.MaxInt64 {
		return report(bitcoin.InitializationBlocked, reasonOpportunityCounterExhausted)
	}
	payout := record.Spec.PayoutWallet
	record.Status.Offer = &bitcoin.BitcoinBlockOffer{
		Mode: bitcoin.OfferRestore,
		// An active schedule override must authorize the refresh, as it does every offer.
		Override:       overrideBinding(record),
		Initialization: objectref.BitcoinInitialization(record),
		Production:     current,
		PolicyDigest:   production.Status.Admission.PolicyDigest,
		Number:         number + 1,
		Address:        payout.Address,
		Wallet:         payout.Wallet,
		ExpectedHeight: restored.Height,
		ExpectedTip:    restored.BlockHash,
		Ceiling:        restored.Height + 1,
		ExpiresAt:      metav1.NewTime(s.Now().Add(bootstrapAdmissionWindow).UTC()),
	}
	result, e := report(bitcoin.InitializationPreparing, refreshReason(record, number+1, reasonOpportunitySelected))
	result.RequeueAfter = time.Millisecond
	return result, e
}

// unansweredRefreshes is how many refresh offers may expire without a receipt before the scheduler
// reports that the worker is not acknowledging them. The worker refuses silently; this surfaces it.
const unansweredRefreshes = 2

// refreshReason reports the restore refresh as unacknowledged once offer number has outlived
// unansweredRefreshes earlier offers without an accounted receipt.
func refreshReason(record *bitcoin.BitcoinInitialization, number int64, reason string) string {
	if number-record.Status.LastAccountedOffer > unansweredRefreshes {
		return reasonRestoreRefreshUnacknowledged
	}
	return reason
}

// refreshAtTip checks that the node's tip is the accounted refresh block while the target still
// retains that receipt. A later operation's receipt leaves only the height to prove it.
func refreshAtTip(view convergence, record *bitcoin.BitcoinInitialization) bool {
	receipt := view.target.Status.LastReceipt
	if receipt == nil || receipt.Request.Offer == nil || receipt.Request.Offer.Mode != bitcoin.OfferRestore ||
		receipt.Request.Offer.Number != record.Status.LastAccountedOffer {
		return true
	}
	return receipt.BlockHash == view.tip
}

// restoredMinerOutputs is the mature coinbase outputs a restored miner wallet needs: what the miner
// start gate requires. MatureOutputsPerMiner is the funding target of a bootstrap from genesis; a
// restored chain has usually spent part of it on block commits already.
const restoredMinerOutputs = 1

// restoredMinersFunded reports whether every frozen miner wallet can start mining on the restored
// chain.
func restoredMinersFunded(record *bitcoin.BitcoinInitialization, mature map[string]int32) bool {
	for _, wallet := range record.Spec.MinerWallets {
		if mature[string(wallet.Wallet.UID)] < restoredMinerOutputs {
			return false
		}
	}
	return true
}

// accountRestoreReceipt advances the scheduler cursor once the worker reports the refresh block on
// the frozen snapshot tip. The refresh pays the payout wallet, so no miner funding is counted.
func accountRestoreReceipt(
	root *api.StacksNetwork,
	initial *bitcoin.BitcoinInitialization,
	execution *bitcoin.BitcoinExecution,
	restored api.SnapshotBitcoinTip,
) {
	if execution.Spec.NetworkUID != root.UID || !metav1.IsControlledBy(execution, root) ||
		execution.Spec.Participant != initial.Spec.Target {
		return
	}
	receipt := execution.Status.LastReceipt
	if receipt == nil || receipt.Request.Method != bitcoin.RPCGenerate || receipt.Request.Action != nil ||
		receipt.Request.Offer == nil ||
		receipt.Request.Reservation != objectref.BitcoinInitialization(initial) ||
		receipt.Request.Target.Participant != initial.Spec.Target ||
		!hashValid(receipt.BlockHash) {
		return
	}
	offer := receipt.Request.Offer
	if offer.Mode != bitcoin.OfferRestore || offer.Initialization != objectref.BitcoinInitialization(initial) ||
		offer.Number <= initial.Status.LastAccountedOffer ||
		offer.Number != execution.Status.CompletedOffer ||
		offerBlocks(offer) != 1 ||
		offer.Ceiling != offer.ExpectedHeight+1 ||
		offer.ExpectedHeight != restored.Height ||
		offer.ExpectedTip != restored.BlockHash ||
		offer.Wallet != initial.Spec.PayoutWallet.Wallet ||
		offer.Address != initial.Spec.PayoutWallet.Address {
		return
	}
	initial.Status.LastAccountedOffer = offer.Number
}

// authorizeRestoreOffer permits only the single refresh block, to the payout wallet, on top of the
// exact restored tip, and only before preparation is recorded.
func (w *Worker) authorizeRestoreOffer(ctx context.Context, a admitted, offer *bitcoin.BitcoinBlockOffer) error {
	initial := a.initialization
	if initial.Spec.Target.UID != a.participant.UID || initial.Status.PreparedAt != nil ||
		offerBlocks(offer) != 1 ||
		offer.Ceiling != offer.ExpectedHeight+1 ||
		offer.Wallet != initial.Spec.PayoutWallet.Wallet ||
		offer.Address != initial.Spec.PayoutWallet.Address {
		return errors.New("restore refresh is not authorized")
	}
	chainstate, err := restoredChainstate(ctx, w.Reader, a.root, initial)
	if err != nil || chainstate == nil || offer.ExpectedHeight != chainstate.Bitcoin.Height ||
		offer.ExpectedTip != chainstate.Bitcoin.BlockHash {
		return errors.New("restore refresh does not extend the snapshot tip")
	}
	return nil
}
