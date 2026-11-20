package bitcoincontrol

import (
	"errors"
	"fmt"
	"time"

	"github.com/go-logr/logr"
)

// refusalStage classifies where a step stopped short of sending a pending offer.
type refusalStage string

const (
	refusalObservation  refusalStage = "Observation"
	refusalAccounting   refusalStage = "GenerationUnaccounted"
	refusalAuthority    refusalStage = "Authorization"
	refusalPreflight    refusalStage = "Preflight"
	refusalPayoutCheck  refusalStage = "PayoutCheck"
	refusalArm          refusalStage = "Arm"
	refusalMessageLimit              = 256
)

// errGenerationUnaccounted holds a pending offer until the scheduler accounts the last receipt.
var errGenerationUnaccounted = errors.New("previous generation is not yet accounted")

// offerRefused is a pending offer that one step did not send.
type offerRefused struct {
	number int64
	stage  refusalStage
	err    error
}

func (e *offerRefused) Error() string {
	return fmt.Sprintf("offer %d refused at %s: %v", e.number, e.stage, e.err)
}

func (e *offerRefused) Unwrap() error { return e.err }

// refusalKey identifies one distinct refusal; repeats of it are not logged again.
type refusalKey struct {
	number  int64
	stage   refusalStage
	message string
}

// refusalLog reports each distinct refusal of a pending offer once, and when it clears.
type refusalLog struct {
	current refusalKey
	since   time.Time
}

func (l *refusalLog) observe(log logr.Logger, err error, now time.Time) {
	var refused *offerRefused
	if !errors.As(err, &refused) {
		// Other errors leave the offer's state unknown, so only a clean step clears a refusal.
		if err == nil && l.current.number != 0 {
			log.Info("generation offer refusal cleared", "offer", l.current.number,
				"stage", l.current.stage, "refusedFor", now.Sub(l.since).Round(time.Millisecond).String())
			*l = refusalLog{}
		}
		return
	}
	message := refused.err.Error()
	if len(message) > refusalMessageLimit {
		message = message[:refusalMessageLimit]
	}
	key := refusalKey{number: refused.number, stage: refused.stage, message: message}
	if key == l.current {
		return
	}
	if key.number != l.current.number {
		l.since = now
	}
	l.current = key
	log.Info("generation offer refused", "offer", key.number, "stage", key.stage, "error", key.message)
}
