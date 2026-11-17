package bitcoincontrol

import (
	"errors"
	"fmt"
	"time"

	bitcoin "github.com/cylewitruk-stacks/stacks-k8s/apis/network/bitcoin/v1alpha2"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/foundation"
	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/utils/ptr"
)

// errCadenceHeightUnavailable reports that height cadences cannot be selected without a fresh tip.
var errCadenceHeightUnavailable = errors.New("observed Bitcoin height unavailable for height cadences")

// selectCadence chooses timing for the next opportunity: an active override's single cadence, else
// the admitted schedule entry with the greatest fromHeight not above the observed tip.
func selectCadence(
	record *bitcoin.BitcoinInitialization,
	admitted *bitcoin.BitcoinBlockScheduleSpec,
	height *int64,
) (*bitcoin.BitcoinCadenceSelection, error) {
	selection := &bitcoin.BitcoinCadenceSelection{}
	if height != nil {
		selection.Height = ptr.To(*height)
	}
	if active := record.Status.Override; active != nil {
		if active.Schedule.Cadence == nil {
			return nil, fmt.Errorf("override cadence unavailable")
		}
		override := active.Override
		selection.Cadence, selection.Override = *active.Schedule.Cadence, &override
		return selection, nil
	}
	if admitted == nil {
		return nil, fmt.Errorf("compiled schedule unavailable")
	}
	if admitted.Cadence != nil {
		selection.Cadence = *admitted.Cadence
		return selection, nil
	}
	if height == nil {
		return nil, errCadenceHeightUnavailable
	}
	var chosen *bitcoin.HeightCadence
	for i := range admitted.Cadences {
		entry := &admitted.Cadences[i]
		if entry.FromHeight <= *height && (chosen == nil || entry.FromHeight > chosen.FromHeight) {
			chosen = entry
		}
	}
	if chosen == nil {
		return nil, fmt.Errorf("no cadence covers height %d", *height)
	}
	selection.Cadence, selection.FromHeight = chosen.Cadence, ptr.To(chosen.FromHeight)
	return selection, nil
}

// sameCadence compares the timing authority of two selections, ignoring the height each was made at.
func sameCadence(a, b *bitcoin.BitcoinCadenceSelection) bool {
	if a == nil || b == nil {
		return a == b
	}
	return equality.Semantic.DeepEqual(a.Cadence, b.Cadence) &&
		equality.Semantic.DeepEqual(a.Override, b.Override) &&
		equality.Semantic.DeepEqual(a.FromHeight, b.FromHeight)
}

// cadenceFailure classifies a selection error; a missing tip is transient, anything else blocks.
func cadenceFailure(err error) (bitcoin.InitializationPhase, string) {
	if errors.Is(err, errCadenceHeightUnavailable) {
		return bitcoin.InitializationWaiting, reasonCadenceHeightUnavailable
	}
	return bitcoin.InitializationBlocked, reasonCadenceUnavailable
}

// scheduleValid checks every cadence a compiled schedule can select.
func scheduleValid(schedule *bitcoin.BitcoinBlockScheduleSpec) error {
	if schedule == nil {
		return fmt.Errorf("compiled schedule unavailable")
	}
	if (schedule.Cadence == nil) == (len(schedule.Cadences) == 0) {
		return fmt.Errorf("compiled schedule needs exactly one of cadence or cadences")
	}
	if schedule.Cadence != nil {
		_, _, err := cadenceBounds(schedule.Cadence)
		return err
	}
	for i := range schedule.Cadences {
		if _, _, err := cadenceBounds(&schedule.Cadences[i].Cadence); err != nil {
			return err
		}
	}
	return nil
}

// progressWindowSeconds bounds expected receipt progress for one cadence.
func progressWindowSeconds(cadence *bitcoin.Cadence) (int64, error) {
	_, upper, err := cadenceBounds(cadence)
	if err != nil {
		return 0, err
	}
	return int64((foundation.ProgressWindow(upper) + time.Second - 1) / time.Second), nil
}

// scheduleWindowSeconds is a single-cadence schedule's window; height cadences publish their selection's.
func scheduleWindowSeconds(schedule *bitcoin.BitcoinBlockScheduleSpec) int64 {
	if schedule == nil || schedule.Cadence == nil {
		return 0
	}
	window, _ := progressWindowSeconds(schedule.Cadence)
	return window
}
