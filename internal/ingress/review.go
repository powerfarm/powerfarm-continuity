package ingress

import (
	"context"
	"fmt"
	"path/filepath"
)

// answerReview answers the question a return review asks — what became of that
// occurrence — from this ingress's own durable state.
//
// The ingress never re-executes the parent: a return review is a question, not
// a second activation. Once a return has been produced for the parent, the
// review has done its work, even when that return said the parent failed;
// resolving the parent's failure belongs to the fallback relationship Heartime
// already names, not to this ingress.
func (r *Receiver) answerReview(ctx context.Context, review Evidence) error {
	parent := review.Parent
	reviews, err := r.Store.Reviews(parent)
	if err != nil {
		return err
	}
	delivered, err := exists(filepath.Join(r.Store.Dir(parent), deliveryFile))
	if err != nil {
		return err
	}
	if !delivered {
		return r.answerForUnknownParent(ctx, review, reviews)
	}
	phase, err := r.Store.Phase(parent)
	if err != nil {
		return err
	}
	switch phase {
	case PhaseComplete:
		return r.resolveAndReport(ctx, r.reviewResult(review, reviews, Verified,
			"the parent's return had already been accepted by Heartime when this review arrived"))
	case PhaseUnreported:
		resolution, found, err := r.Store.Resolution(parent)
		if err != nil {
			return err
		}
		if !found {
			break
		}
		if err := r.report(ctx, resolution); err != nil {
			return r.resolveAndReport(ctx, r.reviewResult(review, reviews, Uncertain,
				"the parent's outcome is recorded here but could not be returned to Heartime: "+err.Error()))
		}
		return r.resolveAndReport(ctx, r.reviewResult(review, reviews, Verified,
			"the parent's return was produced from local state and accepted as "+string(resolution.Outcome)))
	}
	// The parent is recorded here and has not finished. It owns its own return,
	// so this review owns no work and is contained rather than left to be asked
	// again.
	return r.resolveAndReport(ctx, r.reviewResult(review, reviews, Contained,
		fmt.Sprintf("the parent occurrence is recorded here and is %s; it owns its own return and this review owns no work", phase)))
}

// answerForUnknownParent accounts for an occurrence this ingress has no record
// of. What happened genuinely cannot be established, so the first answers say
// exactly that — and that inability is bounded: after reviewBound reviews the
// parent is contained, because an occurrence nothing here can account for must
// not be reviewable forever.
func (r *Receiver) answerForUnknownParent(ctx context.Context, review Evidence, reviews int) error {
	outcome, reason := Uncertain, "this ingress has no record of the parent occurrence, so what happened to it cannot be established here"
	if reviews >= r.Config.ReviewBound {
		outcome = Contained
		reason = fmt.Sprintf("this ingress has no record of the parent occurrence after %d return review(s); no active work is owned here and no institutional state was changed", reviews)
	}
	parentResult := Result{
		Occurrence:     review.Parent,
		Contract:       review.Contract,
		Responsibility: review.Responsibility,
		EndedAt:        r.Now().UTC().Format(utcLayout),
		Reviews:        reviews,
		Outcome:        outcome,
		Reason:         reason,
	}
	if err := r.resolveAndReport(ctx, parentResult); err != nil {
		return r.resolveAndReport(ctx, r.reviewResult(review, reviews, Uncertain,
			"the parent has no local record and its return could not be delivered: "+err.Error()))
	}
	return r.resolveAndReport(ctx, r.reviewResult(review, reviews, Verified,
		"a return was produced for the parent from what this ingress can establish: "+string(outcome)))
}

// reviewResult describes what this ingress did with one return review.
func (r *Receiver) reviewResult(review Evidence, reviews int, outcome Outcome, reason string) Result {
	result := r.base(review, outcome, reason)
	result.Reviews = reviews
	return result
}
