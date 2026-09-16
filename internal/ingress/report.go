package ingress

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"path/filepath"
	"time"
)

// Reporting bounds. An outcome that cannot be returned now stays durable and is
// retried by the next sweep and by the next process, because an unreachable
// ledger is an unresolved condition, not a resolved one.
const (
	reportAttempts    = 3
	reportTimeout     = 30 * time.Second
	reportBackoffBase = time.Second
	reportErrorFile   = "report-error.log"
)

// report returns one outcome to Heartime, bound to the digest of the immutable
// evidence it was derived from.
//
// Outcomes travel to the ledger's own local command, which is the only
// authenticated surface Heartime exposes for execution feedback. Replaying an
// identical report is idempotent there, so retrying is safe; a conflicting
// report is refused, which is the behaviour this ingress wants.
func (r *Receiver) report(ctx context.Context, resolution Resolution) error {
	if !validOutcome(resolution.Outcome) {
		return fmt.Errorf("%w: %q is not a reportable outcome", ErrInvalidConfig, resolution.Outcome)
	}
	if !validDigest(resolution.Evidence) {
		return fmt.Errorf("%w: a report must carry the digest of immutable evidence", ErrInvalidConfig)
	}
	argv := make([]string, 0, len(r.Config.Report)+3)
	argv = append(argv, r.Config.Report...)
	argv = append(argv, resolution.Occurrence, string(resolution.Outcome), resolution.Evidence)

	var last error
	for attempt := 1; attempt <= reportAttempts; attempt++ {
		if attempt > 1 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(reportBackoffBase << (attempt - 2)):
			}
		}
		output, err := runReport(ctx, argv)
		if err == nil {
			r.logf("reported %s as %s", resolution.Occurrence, resolution.Outcome)
			return r.Store.MarkReported(Report{
				Occurrence: resolution.Occurrence,
				Outcome:    resolution.Outcome,
				Evidence:   resolution.Evidence,
				Command:    argv,
				Attempts:   attempt,
				ReportedAt: r.Now().UTC().Format(utcLayout),
			})
		}
		last = fmt.Errorf("attempt %d: %w", attempt, err)
		if output != "" {
			last = fmt.Errorf("%w: %s", last, output)
		}
	}
	r.recordReportFailure(resolution, last)
	return last
}

// runReport invokes the ledger's local command once and returns what it said.
func runReport(ctx context.Context, argv []string) (string, error) {
	deadline, cancel := context.WithTimeout(ctx, reportTimeout)
	defer cancel()
	command := exec.CommandContext(deadline, argv[0], argv[1:]...)
	combined := &capped{limit: 8 << 10}
	command.Stdout, command.Stderr = combined, combined
	command.WaitDelay = 5 * time.Second
	err := command.Run()
	return string(combined.Bytes()), err
}

// recordReportFailure keeps an unreturned outcome visible to an operator. The
// outcome itself stays on disk and is retried; this is only the account of why
// it has not been accepted yet.
func (r *Receiver) recordReportFailure(resolution Resolution, cause error) {
	r.logf("could not report %s as %s: %v", resolution.Occurrence, resolution.Outcome, cause)
	line, err := json.Marshal(map[string]string{
		"at":         r.Now().UTC().Format(utcLayout),
		"occurrence": resolution.Occurrence,
		"outcome":    string(resolution.Outcome),
		"evidence":   resolution.Evidence,
		"error":      cause.Error(),
	})
	if err != nil {
		return
	}
	if err := appendLine(filepath.Join(r.Store.Dir(resolution.Occurrence), reportErrorFile), line); err != nil {
		r.logf("could not record the report failure of %s: %v", resolution.Occurrence, err)
	}
}
