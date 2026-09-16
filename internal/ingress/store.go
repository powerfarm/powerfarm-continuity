package ingress

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"powerfarm.dev/continuity/v2/internal/institution"
)

// File names of one occurrence's durable record. Each name is a fact about the
// activation, and the set of names present is the whole recovery state machine:
// a delivery without a start was never executed, a start without a resolution
// may have executed, and a resolution without a report still owes Heartime its
// return.
const (
	deliveryFile   = "delivery.json"
	arrivalsFile   = "arrivals.jsonl"
	startedFile    = "started.json"
	resolutionFile = "outcome.json"
	reportedFile   = "reported.json"
)

// Store is the ingress's own local state. Deduplication happens here, before
// any effect is claimed, because an outbox may publish one occurrence more than
// once and a 2xx response proves delivery only.
type Store struct{ Root string }

// Arrival records one delivery of an occurrence to this ingress, including
// every repeat of an occurrence already executed.
type Arrival struct {
	At       string `json:"at"`
	Digest   string `json:"digest"`
	Repeated bool   `json:"repeated"`
}

// Start records that a route was about to be invoked. It is written and synced
// before the first byte of the route runs, so a crash is never mistaken for an
// activation that never began.
type Start struct {
	Occurrence string   `json:"occurrence"`
	Route      string   `json:"route"`
	Argv       []string `json:"argv"`
	At         string   `json:"at"`
}

// Result is the immutable evidence of what this ingress did with one
// occurrence. Its digest is what Heartime is given; the bytes never change.
type Result struct {
	Occurrence     string                  `json:"occurrence"`
	Contract       institution.ContractRef `json:"contract"`
	Responsibility institution.ContractRef `json:"responsibility"`
	Kind           string                  `json:"kind"`
	Nominal        string                  `json:"nominal"`
	DeliveryDigest string                  `json:"deliveryDigest"`
	Route          string                  `json:"route,omitempty"`
	Argv           []string                `json:"argv,omitempty"`
	StartedAt      string                  `json:"startedAt,omitempty"`
	EndedAt        string                  `json:"endedAt"`
	ExitCode       int                     `json:"exitCode,omitempty"`
	Interrupted    bool                    `json:"interrupted,omitempty"`
	TimedOut       bool                    `json:"timedOut,omitempty"`
	StdoutDigest   string                  `json:"stdoutDigest,omitempty"`
	StderrDigest   string                  `json:"stderrDigest,omitempty"`
	Parent         string                  `json:"parent,omitempty"`
	Reviews        int                     `json:"reviews,omitempty"`
	Outcome        Outcome                 `json:"outcome"`
	Reason         string                  `json:"reason"`
}

// Resolution is the outcome this ingress owes Heartime, bound to the digest of
// the immutable Result it was derived from.
type Resolution struct {
	Occurrence string  `json:"occurrence"`
	Outcome    Outcome `json:"outcome"`
	Evidence   string  `json:"evidence"`
	Reason     string  `json:"reason"`
	DecidedAt  string  `json:"decidedAt"`
}

// Report records that Heartime accepted the return.
type Report struct {
	Occurrence string   `json:"occurrence"`
	Outcome    Outcome  `json:"outcome"`
	Evidence   string   `json:"evidence"`
	Command    []string `json:"command"`
	Attempts   int      `json:"attempts"`
	ReportedAt string   `json:"reportedAt"`
}

// CAS is where immutable results are stored under their digest.
func (s Store) CAS() institution.CAS { return institution.CAS{Root: filepath.Join(s.Root, "cas")} }

// Dir is the durable record of one occurrence.
func (s Store) Dir(occurrence string) string {
	return filepath.Join(s.Root, "occurrences", short(occurrence))
}

// Receive durably records one arrival and reports whether this is the first.
// The exact delivered bytes are stored once; a repeat carrying different bytes
// under the same identity is an integrity failure, never a silent overwrite.
func (s Store) Receive(evidence Evidence, raw []byte, now time.Time) (bool, error) {
	directory := s.Dir(evidence.OccurrenceID)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return false, err
	}
	first, err := writeExclusive(filepath.Join(directory, deliveryFile), raw)
	if err != nil {
		return false, err
	}
	if !first {
		stored, err := os.ReadFile(filepath.Join(directory, deliveryFile))
		if err != nil {
			return false, err
		}
		if !bytes.Equal(stored, raw) {
			return false, fmt.Errorf("%w: %s", ErrIntegrity, evidence.OccurrenceID)
		}
	}
	arrival, err := json.Marshal(Arrival{At: now.UTC().Format(utcLayout), Digest: institution.Hash(raw), Repeated: !first})
	if err != nil {
		return false, err
	}
	if err := appendLine(filepath.Join(directory, arrivalsFile), arrival); err != nil {
		return false, err
	}
	if first && evidence.IsReturnReview() {
		if err := s.indexReview(evidence.Parent, evidence.OccurrenceID); err != nil {
			return false, err
		}
	}
	return first, nil
}

// Load returns the evidence exactly as it was delivered.
func (s Store) Load(occurrence string) (Evidence, []byte, error) {
	raw, err := os.ReadFile(filepath.Join(s.Dir(occurrence), deliveryFile))
	if err != nil {
		return Evidence{}, nil, err
	}
	var evidence Evidence
	if err := json.Unmarshal(raw, &evidence); err != nil {
		return Evidence{}, raw, err
	}
	return evidence, raw, nil
}

// Start records the intent to invoke a route before it is invoked.
func (s Store) Start(occurrence string, start Start) error {
	return institution.AtomicJSON(filepath.Join(s.Dir(occurrence), startedFile), start)
}

// Resolve stores the immutable result and the outcome derived from it.
// The result is written to content-addressed storage first, so the recorded
// outcome can never reference evidence that does not exist.
func (s Store) Resolve(result Result, now time.Time) (Resolution, error) {
	if !validOutcome(result.Outcome) {
		return Resolution{}, fmt.Errorf("%w: %q is not a reportable outcome", ErrInvalidConfig, result.Outcome)
	}
	reference, err := s.CAS().JSON(result)
	if err != nil {
		return Resolution{}, err
	}
	resolution := Resolution{
		Occurrence: result.Occurrence,
		Outcome:    result.Outcome,
		Evidence:   reference.Digest,
		Reason:     result.Reason,
		DecidedAt:  now.UTC().Format(utcLayout),
	}
	if err := institution.AtomicJSON(filepath.Join(s.Dir(result.Occurrence), resolutionFile), resolution); err != nil {
		return Resolution{}, err
	}
	return resolution, nil
}

// Resolution returns the stored outcome of an occurrence, if it has one.
func (s Store) Resolution(occurrence string) (Resolution, bool, error) {
	var resolution Resolution
	found, err := readJSONIfPresent(filepath.Join(s.Dir(occurrence), resolutionFile), &resolution)
	return resolution, found, err
}

// MarkReported records that Heartime accepted the return.
func (s Store) MarkReported(report Report) error {
	return institution.AtomicJSON(filepath.Join(s.Dir(report.Occurrence), reportedFile), report)
}

// Phase is what an occurrence's durable record says still has to happen.
type Phase string

// The phases of one occurrence, derived from files alone so that a restart
// reads the same state a running process would.
const (
	// PhaseUndispatched: delivered, never started. No effect can exist yet.
	PhaseUndispatched Phase = "undispatched"
	// PhaseInFlight: a route was started and no outcome was recorded. After a
	// restart this is indistinguishable from an interrupted activation.
	PhaseInFlight Phase = "in-flight"
	// PhaseUnreported: an outcome exists and Heartime has not accepted it.
	PhaseUnreported Phase = "unreported"
	// PhaseComplete: the return was accepted.
	PhaseComplete Phase = "complete"
)

// Phase reports what an occurrence still owes.
func (s Store) Phase(occurrence string) (Phase, error) {
	directory := s.Dir(occurrence)
	reported, err := exists(filepath.Join(directory, reportedFile))
	if err != nil || reported {
		return PhaseComplete, err
	}
	resolved, err := exists(filepath.Join(directory, resolutionFile))
	if err != nil {
		return "", err
	}
	if resolved {
		return PhaseUnreported, nil
	}
	started, err := exists(filepath.Join(directory, startedFile))
	if err != nil {
		return "", err
	}
	if started {
		return PhaseInFlight, nil
	}
	return PhaseUndispatched, nil
}

// Occurrences lists every recorded occurrence identity, oldest record first by
// identity so that recovery is deterministic.
func (s Store) Occurrences() ([]string, error) {
	entries, err := os.ReadDir(filepath.Join(s.Root, "occurrences"))
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	identities := []string{}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		identity := "sha256:" + entry.Name()
		if !validDigest(identity) {
			continue
		}
		// A record may exist because an occurrence was delivered here, or
		// because a return review made this ingress account for one it never
		// received. Both still owe Heartime a return.
		for _, name := range []string{deliveryFile, resolutionFile} {
			present, err := exists(filepath.Join(s.Root, "occurrences", entry.Name(), name))
			if err != nil {
				return nil, err
			}
			if present {
				identities = append(identities, identity)
				break
			}
		}
	}
	sort.Strings(identities)
	return identities, nil
}

// indexReview records that one return review was received for a parent, so the
// ingress can bound how long it answers reviews it cannot resolve.
func (s Store) indexReview(parent, review string) error {
	directory := filepath.Join(s.Root, "parents", short(parent))
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return err
	}
	if _, err := writeExclusive(filepath.Join(directory, short(review)), nil); err != nil {
		return err
	}
	return nil
}

// Reviews counts the distinct return reviews received for one parent.
func (s Store) Reviews(parent string) (int, error) {
	entries, err := os.ReadDir(filepath.Join(s.Root, "parents", short(parent)))
	if os.IsNotExist(err) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	return len(entries), nil
}

// writeExclusive creates path with content and syncs it and its directory.
// It reports whether this call created the file.
func writeExclusive(path string, content []byte) (bool, error) {
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if os.IsExist(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if _, err := file.Write(content); err != nil {
		file.Close()
		return false, err
	}
	if err := file.Sync(); err != nil {
		file.Close()
		return false, err
	}
	if err := file.Close(); err != nil {
		return false, err
	}
	return true, syncDir(filepath.Dir(path))
}

// appendLine durably appends one JSON line to path.
func appendLine(path string, line []byte) error {
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	if _, err := file.Write(append(line, '\n')); err != nil {
		file.Close()
		return err
	}
	if err := file.Sync(); err != nil {
		file.Close()
		return err
	}
	return file.Close()
}

func syncDir(path string) error {
	directory, err := os.Open(path)
	if err != nil {
		return err
	}
	defer directory.Close()
	return directory.Sync()
}

func exists(path string) (bool, error) {
	_, err := os.Stat(path)
	if err == nil {
		return true, nil
	}
	if os.IsNotExist(err) {
		return false, nil
	}
	return false, err
}

func readJSONIfPresent(path string, target any) (bool, error) {
	raw, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, json.Unmarshal(raw, target)
}
