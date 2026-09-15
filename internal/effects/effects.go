package effects

import "fmt"

type Class string

const (
	Observe      Class = "observe"
	Idempotent   Class = "idempotent"
	Reconcilable Class = "reconcilable"
	AtMostOnce   Class = "at_most_once"
	Irreversible Class = "irreversible"
)

type State string

const (
	Planned      State = "planned"
	Authorized   State = "authorized"
	Dispatched   State = "dispatched"
	Acknowledged State = "acknowledged"
	Verified     State = "verified"
	Uncertain    State = "uncertain"
	Failed       State = "failed"
)

type Event string

const (
	Authorize     Event = "authorize"
	Dispatch      Event = "dispatch"
	Acknowledge   Event = "acknowledge"
	VerifyMatch   Event = "verify_match"
	LoseCertainty Event = "lose_certainty"
	Fail          Event = "fail"
)

type Recovery string

const (
	Retry         Recovery = "retry"
	ObserveFirst  Recovery = "observe_first"
	HumanDecision Recovery = "human_decision"
	NoAction      Recovery = "no_action"
)

// Next advances only the certainty lifecycle. It deliberately knows nothing
// about transports or device protocols; those belong to adapters.
func Next(current State, event Event) (State, error) {
	switch current {
	case Planned:
		if event == Authorize {
			return Authorized, nil
		}
	case Authorized:
		if event == Dispatch {
			return Dispatched, nil
		}
	case Dispatched:
		switch event {
		case Acknowledge:
			return Acknowledged, nil
		case LoseCertainty:
			return Uncertain, nil
		case VerifyMatch:
			return Verified, nil
		case Fail:
			return Failed, nil
		}
	case Acknowledged:
		switch event {
		case VerifyMatch:
			return Verified, nil
		case LoseCertainty:
			return Uncertain, nil
		case Fail:
			return Failed, nil
		}
	case Uncertain:
		switch event {
		case VerifyMatch:
			return Verified, nil
		case Fail:
			return Failed, nil
		}
	}
	return current, fmt.Errorf("invalid effect transition %s --%s--> ?", current, event)
}

// Recover is the core rule Continuity applies when dispatch may have happened
// but the outcome is unknown. It never equates a lost acknowledgement with a
// failed real-world effect.
func Recover(class Class) (Recovery, error) {
	switch class {
	case Observe, Idempotent:
		return Retry, nil
	case Reconcilable:
		return ObserveFirst, nil
	case AtMostOnce, Irreversible:
		return HumanDecision, nil
	default:
		return NoAction, fmt.Errorf("unknown effect class %q", class)
	}
}
