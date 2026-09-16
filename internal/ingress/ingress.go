// Package ingress terminates the Heartime delivery relationship for Continuity.
//
// It is an ingress of Continuity, not a new subsystem and not an organ. It
// accepts temporal evidence that something is due, deduplicates by occurrence
// identity before any effect is claimed, hands the activation to the executable
// relationship that already owns that work, and returns the outcome to Heartime
// bound to immutable evidence.
//
// It never schedules, never chooses salience, never invents authority and never
// treats transport acknowledgement as an executed effect.
package ingress

import (
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"powerfarm.dev/continuity/v2/internal/institution"
)

// ReturnReviewPrefix marks the kind of an occurrence that asks what became of
// an earlier occurrence. Its suffix is the parent occurrence identity.
const ReturnReviewPrefix = "return-review/"

// Ordinary Heartime occurrence kinds.
const (
	KindWork           = "work"
	KindPlanningReview = "planning-review"
	KindFallback       = "fallback"
)

// Outcome is the execution feedback Heartime accepts. Acknowledgement is never
// an outcome: only these four say anything about an effect.
type Outcome string

// Reportable outcomes.
const (
	// Verified means the executed relationship established success independently.
	Verified Outcome = "verified"
	// Failed means the work did not happen; effects may remain.
	Failed Outcome = "failed"
	// Uncertain means what happened cannot be established here.
	Uncertain Outcome = "uncertain"
	// Contained means the condition is bounded and recorded and the occurrence
	// owns no active work. It never means the intended work succeeded.
	Contained Outcome = "contained"
)

// Errors returned by the ingress are part of its contract.
var (
	ErrInvalidEvidence = errors.New("invalid occurrence evidence")
	ErrIntegrity       = errors.New("stored bytes do not match the delivered occurrence")
	ErrInvalidConfig   = errors.New("invalid ingress configuration")
)

// utcLayout is the timestamp profile Heartime delivers: RFC 3339 UTC, whole seconds.
const utcLayout = "2006-01-02T15:04:05Z"

var (
	digestPattern     = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)
	contractIDPattern = regexp.MustCompile(`^pf\.contract(?:\.[a-z0-9][a-z0-9-]*)+$`)
)

// Evidence is the temporal evidence Heartime persists and delivers for one
// occurrence. It asserts due-ness only. Nothing in it is an instruction, and
// nothing in it grants authority.
type Evidence struct {
	OccurrenceID    string                  `json:"occurrenceId"`
	Contract        institution.ContractRef `json:"contract"`
	ContractDigest  string                  `json:"contractDigest"`
	Responsibility  institution.ContractRef `json:"responsibility"`
	ObligationID    string                  `json:"obligationId"`
	Kind            string                  `json:"kind"`
	Nominal         string                  `json:"nominal"`
	CoveredFrom     string                  `json:"coveredFrom"`
	CoveredCount    int64                   `json:"coveredCount"`
	Predicate       string                  `json:"predicate"`
	PredicateResult bool                    `json:"predicateResult"`
	Disposition     string                  `json:"disposition"`
	EvaluatedAt     string                  `json:"evaluatedAt"`
	ClockSource     string                  `json:"clockSource"`
	Handoff         institution.ContractRef `json:"handoff"`
	Parent          string                  `json:"parent,omitempty"`
	Overlap         string                  `json:"overlap"`
	FallbackMode    string                  `json:"fallbackMode"`
}

// Validate checks that the delivery is self-consistent before it is stored.
// The occurrence identity is recomputed from the terms it is defined over, so a
// mislabelled or tampered delivery cannot claim an identity it does not have.
func (e Evidence) Validate() error {
	invalid := func(reason string) error { return fmt.Errorf("%w: %s", ErrInvalidEvidence, reason) }
	if !digestPattern.MatchString(e.OccurrenceID) {
		return invalid("occurrenceId must be sha256:<64 lowercase hex>")
	}
	if !contractIDPattern.MatchString(e.Contract.ID) || e.Contract.Generation < 1 {
		return invalid("contract must name a contract id and a positive generation")
	}
	if e.ObligationID == "" {
		return invalid("obligationId is required")
	}
	if e.Kind == "" {
		return invalid("kind is required")
	}
	nominal, err := time.Parse(utcLayout, e.Nominal)
	if err != nil {
		return invalid("nominal must be RFC 3339 UTC with whole seconds")
	}
	if parent, ok := strings.CutPrefix(e.Kind, ReturnReviewPrefix); ok {
		if !digestPattern.MatchString(parent) {
			return invalid("a return review must name its parent occurrence in its kind")
		}
		if e.Parent != parent {
			return invalid("the parent field and the return review kind must name the same occurrence")
		}
	}
	computed, err := OccurrenceID(e.Contract, e.ObligationID, e.Kind, nominal)
	if err != nil {
		return err
	}
	if computed != e.OccurrenceID {
		return invalid("occurrenceId does not match the terms it is computed over")
	}
	return nil
}

// IsReturnReview reports whether this occurrence asks what became of a parent.
func (e Evidence) IsReturnReview() bool { return strings.HasPrefix(e.Kind, ReturnReviewPrefix) }

// OccurrenceID is the restart-stable identity of one nominal temporal
// satisfaction, as defined by HEARTIME_CONTRACT_v0 §2:
// sha256(RFC8785([contractId, generation, obligationId, kind, nominalUTC])).
func OccurrenceID(contract institution.ContractRef, obligationID, kind string, nominal time.Time) (string, error) {
	canonical, err := institution.Canonical([]any{contract.ID, contract.Generation, obligationID, kind, nominal.UTC().Format(utcLayout)})
	if err != nil {
		return "", err
	}
	return institution.Hash(canonical), nil
}

// short is the filesystem-safe form of an occurrence identity.
func short(occurrence string) string { return strings.TrimPrefix(occurrence, "sha256:") }

// validOutcome reports whether value is one of the four reportable outcomes.
func validOutcome(value Outcome) bool {
	switch value {
	case Verified, Failed, Uncertain, Contained:
		return true
	}
	return false
}

// validDigest reports whether value is a lowercase sha256 content reference.
func validDigest(value string) bool {
	if !strings.HasPrefix(value, "sha256:") || len(value) != len("sha256:")+64 {
		return false
	}
	_, err := hex.DecodeString(value[len("sha256:"):])
	return err == nil
}
