package ingress

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

// maxCapturedBytes bounds what one route may return through each stream. A
// route that produces more is still executed; only the record is bounded.
const maxCapturedBytes = 1 << 20

// verdict is how one route's exit was read.
type verdict struct {
	outcome  Outcome
	reason   string
	timedOut bool
	exitCode int
}

// execute hands one activation to the relationship that owns it and derives the
// outcome from what that relationship did. The ingress never inspects the work
// itself: it reads the exit status and the single field the route's
// configuration declares, and nothing else.
//
// The intent to invoke is recorded and synced before the first byte of the
// route runs, so an interrupted activation is never mistaken for one that never
// began.
func (r *Receiver) execute(ctx context.Context, evidence Evidence, route Route) (Result, error) {
	directory := r.Store.Dir(evidence.OccurrenceID)
	work := filepath.Join(directory, "work")
	if err := os.MkdirAll(work, 0o700); err != nil {
		return Result{}, err
	}
	argv := expand(route.Argv, evidence, work, filepath.Join(directory, deliveryFile))
	started := r.Now().UTC()
	if err := r.Store.Start(evidence.OccurrenceID, Start{
		Occurrence: evidence.OccurrenceID,
		Route:      route.Name,
		Argv:       argv,
		At:         started.Format(utcLayout),
	}); err != nil {
		return Result{}, err
	}

	deadline, cancel := context.WithTimeout(ctx, time.Duration(route.TimeoutSeconds)*time.Second)
	defer cancel()
	stdout := &capped{limit: maxCapturedBytes}
	stderr := &capped{limit: maxCapturedBytes}
	command := exec.CommandContext(deadline, argv[0], argv[1:]...)
	// Credentials reach a route only through its environment, never through the
	// delivery, and never through a shell.
	command.Env = os.Environ()
	command.Stdout, command.Stderr = stdout, stderr
	command.WaitDelay = 5 * time.Second
	runErr := command.Run()
	read := classify(route, stdout.Bytes(), runErr, errors.Is(deadline.Err(), context.DeadlineExceeded))

	result := r.base(evidence, read.outcome, read.reason)
	result.Route, result.Argv, result.StartedAt = route.Name, argv, started.Format(utcLayout)
	result.TimedOut, result.ExitCode = read.timedOut, read.exitCode
	if reference, err := r.Store.CAS().Put(stdout.Bytes(), "application/json"); err == nil {
		result.StdoutDigest = reference.Digest
	}
	if reference, err := r.Store.CAS().Put(stderr.Bytes(), "text/plain"); err == nil {
		result.StderrDigest = reference.Digest
	}
	return result, nil
}

// classify reads a route's exit as execution feedback.
//
// Two meanings are doctrine and cannot be configured: a route that never
// started did not do the work, and a route stopped by a bound or a signal may
// have done part of it. Everything else is the meaning the route's own
// configuration declares.
func classify(route Route, stdout []byte, runErr error, timedOut bool) verdict {
	if timedOut {
		return verdict{Uncertain, "the route exceeded its time bound and was stopped; whether it produced effects cannot be established here", true, -1}
	}
	if runErr != nil {
		exitErr := new(exec.ExitError)
		if !errors.As(runErr, &exitErr) {
			return verdict{Failed, fmt.Sprintf("the route could not be started (%v); no work was attempted", runErr), false, 0}
		}
		switch code := exitErr.ExitCode(); {
		case code < 0:
			return verdict{Uncertain, fmt.Sprintf("the route was terminated by a signal (%s); whether it produced effects cannot be established here", exitErr.String()), false, code}
		case code == refusalExit:
			return verdict{route.Outcome.OnRefusal, "the route reported a credential, quota or budget refusal rather than a technical failure", false, code}
		default:
			return verdict{route.Outcome.OnFailure, fmt.Sprintf("the route exited %d", code), false, code}
		}
	}
	missing := func(reason string) verdict { return verdict{route.Outcome.OnMissing, reason, false, 0} }
	if route.Outcome.Field == "" {
		return missing("the route exited cleanly but declares no field by which success is established")
	}
	var reported map[string]json.RawMessage
	if err := json.Unmarshal(bytes.TrimSpace(stdout), &reported); err != nil {
		return missing("the route exited cleanly but did not return a JSON object")
	}
	raw, present := reported[route.Outcome.Field]
	if !present {
		return missing(fmt.Sprintf("the route exited cleanly without the declared field %q", route.Outcome.Field))
	}
	var established bool
	if err := json.Unmarshal(raw, &established); err != nil {
		return missing(fmt.Sprintf("the route's field %q is not a boolean", route.Outcome.Field))
	}
	if established {
		return verdict{route.Outcome.WhenTrue, fmt.Sprintf("the route exited 0 with %s true", route.Outcome.Field), false, 0}
	}
	return verdict{route.Outcome.WhenFalse, fmt.Sprintf("the route exited 0 with %s false", route.Outcome.Field), false, 0}
}

// capped collects at most limit bytes and counts what it discarded, so a record
// never silently claims to be the whole of a route's output.
type capped struct {
	limit     int
	buffer    bytes.Buffer
	discarded int
}

func (c *capped) Write(content []byte) (int, error) {
	room := c.limit - c.buffer.Len()
	if room > len(content) {
		room = len(content)
	}
	if room > 0 {
		c.buffer.Write(content[:room])
	} else {
		room = 0
	}
	c.discarded += len(content) - room
	return len(content), nil
}

// Bytes returns what was captured.
func (c *capped) Bytes() []byte { return c.buffer.Bytes() }
