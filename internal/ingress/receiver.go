package ingress

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"powerfarm.dev/continuity/v2/internal/institution"
)

// Receiver accepts Heartime deliveries and returns their outcomes.
//
// Accepting a delivery and executing it are deliberately separate. The request
// handler only records the arrival durably and acknowledges it; every effect is
// produced by the same sweep that recovers after a restart, so normal operation
// and recovery are one code path rather than two.
type Receiver struct {
	Config Config
	Store  Store
	Token  string
	// Now supplies the instants written into evidence. Business logic never
	// reads the clock directly.
	Now func() time.Time
	// Logger records what the ingress did. It is never the record of an effect.
	Logger *log.Logger

	notify chan struct{}
}

// New prepares a receiver from operator configuration, loading the credential
// that the delivering Heartime must present.
func New(config Config) (*Receiver, error) {
	secret, err := os.ReadFile(config.TokenFile)
	if err != nil {
		return nil, err
	}
	token := strings.TrimSpace(string(secret))
	if token == "" {
		return nil, fmt.Errorf("%w: the credential file is empty", ErrInvalidConfig)
	}
	return &Receiver{
		Config: config,
		Store:  Store{Root: config.State},
		Token:  token,
		Now:    func() time.Time { return time.Now().UTC() },
		Logger: log.Default(),
		notify: make(chan struct{}, 1),
	}, nil
}

// Run serves deliveries until ctx is cancelled. It recovers before it listens,
// so nothing an earlier process left unresolved waits for new traffic.
func (r *Receiver) Run(ctx context.Context) error {
	if err := r.Sweep(ctx); err != nil {
		return err
	}
	listener, err := net.Listen("tcp", r.Config.Listen)
	if err != nil {
		return err
	}
	server := &http.Server{Handler: r.Handler(), ReadHeaderTimeout: 10 * time.Second}
	served := make(chan error, 1)
	go func() { served <- server.Serve(listener) }()
	r.logf("ingress listening on %s for %d route(s)", listener.Addr(), len(r.Config.Routes))
	ticker := time.NewTicker(time.Duration(r.Config.SweepSeconds) * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			_ = server.Shutdown(shutdown)
			return ctx.Err()
		case err := <-served:
			if errors.Is(err, http.ErrServerClosed) {
				return nil
			}
			return err
		case <-r.notify:
			r.sweepLogged(ctx)
		case <-ticker.C:
			r.sweepLogged(ctx)
		}
	}
}

// Handler is the delivery endpoint. A 2xx response means the delivery was
// recorded durably and nothing more: it never asserts that an effect occurred.
func (r *Receiver) Handler() http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodPost {
			refuse(writer, http.StatusMethodNotAllowed, "deliveries are POSTed")
			return
		}
		if !r.authorized(request) {
			writer.Header().Set("WWW-Authenticate", "Bearer")
			refuse(writer, http.StatusUnauthorized, "a delivery must present the receiver credential")
			return
		}
		raw, err := io.ReadAll(http.MaxBytesReader(writer, request.Body, r.Config.MaxBodyBytes))
		if err != nil {
			refuse(writer, http.StatusRequestEntityTooLarge, "the delivery body is larger than this ingress accepts")
			return
		}
		var evidence Evidence
		if err := json.Unmarshal(raw, &evidence); err != nil {
			refuse(writer, http.StatusBadRequest, "the delivery body is not occurrence evidence")
			return
		}
		if err := evidence.Validate(); err != nil {
			refuse(writer, http.StatusBadRequest, err.Error())
			return
		}
		// The transport key is a convenience. Identity is the recomputed
		// occurrence id, so a disagreement between them is refused rather than
		// resolved in favour of either.
		if key := request.Header.Get("Idempotency-Key"); key != "" && key != evidence.OccurrenceID {
			refuse(writer, http.StatusBadRequest, "the idempotency key and the occurrence identity disagree")
			return
		}
		first, err := r.Store.Receive(evidence, raw, r.Now())
		if err != nil {
			r.logf("refusing %s: %v", evidence.OccurrenceID, err)
			refuse(writer, http.StatusInternalServerError, "the delivery could not be recorded durably")
			return
		}
		status := http.StatusAccepted
		if !first {
			status = http.StatusOK
		} else {
			r.wake()
		}
		writer.Header().Set("Content-Type", "application/json")
		writer.WriteHeader(status)
		_ = json.NewEncoder(writer).Encode(map[string]any{
			"occurrence":   evidence.OccurrenceID,
			"acknowledged": true,
			"verified":     false,
			"repeated":     !first,
			"note":         "acknowledgement records delivery only; the outcome is reported to Heartime separately",
		})
	})
}

// authorized compares the presented credential in constant time.
func (r *Receiver) authorized(request *http.Request) bool {
	presented, found := strings.CutPrefix(request.Header.Get("Authorization"), "Bearer ")
	if !found {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(strings.TrimSpace(presented)), []byte(r.Token)) == 1
}

// wake asks the sweep to run without blocking the request that asked.
func (r *Receiver) wake() {
	if r.notify == nil {
		return
	}
	select {
	case r.notify <- struct{}{}:
	default:
	}
}

// Sweep advances every occurrence that still owes something: undispatched
// deliveries, activations interrupted by a restart, and outcomes Heartime has
// not accepted yet. Occurrences of one contract are advanced in sequence, so a
// responsibility never has two activations of its state at once; different
// contracts advance independently.
func (r *Receiver) Sweep(ctx context.Context) error {
	identities, err := r.Store.Occurrences()
	if err != nil {
		return err
	}
	byContract := map[string][]string{}
	for _, identity := range identities {
		phase, err := r.Store.Phase(identity)
		if err != nil {
			return err
		}
		if phase == PhaseComplete {
			continue
		}
		relationship := r.handoffOf(identity)
		byContract[relationship] = append(byContract[relationship], identity)
	}
	contracts := make([]string, 0, len(byContract))
	for contract := range byContract {
		contracts = append(contracts, contract)
	}
	sort.Strings(contracts)
	var group sync.WaitGroup
	for _, contract := range contracts {
		group.Add(1)
		go func(queue []string) {
			defer group.Done()
			for _, identity := range queue {
				if ctx.Err() != nil {
					return
				}
				if err := r.advance(ctx, identity); err != nil {
					r.logf("occurrence %s: %v", identity, err)
				}
			}
		}(byContract[contract])
	}
	group.Wait()
	return nil
}

func (r *Receiver) sweepLogged(ctx context.Context) {
	if err := r.Sweep(ctx); err != nil && ctx.Err() == nil {
		r.logf("sweep: %v", err)
	}
}

// handoffOf groups an occurrence by the relationship whose state it touches, so
// that relationship never has two activations of its state at once. An
// unreadable delivery groups under its own identity, so one damaged record
// cannot serialize everything else behind it.
func (r *Receiver) handoffOf(occurrence string) string {
	evidence, _, err := r.Store.Load(occurrence)
	if err != nil || evidence.Handoff.ID == "" {
		return occurrence
	}
	return evidence.Handoff.ID
}

// advance moves one occurrence to the next durable state.
func (r *Receiver) advance(ctx context.Context, occurrence string) error {
	phase, err := r.Store.Phase(occurrence)
	if err != nil {
		return err
	}
	switch phase {
	case PhaseComplete:
		return nil
	case PhaseUnreported:
		resolution, found, err := r.Store.Resolution(occurrence)
		if err != nil || !found {
			return err
		}
		return r.report(ctx, resolution)
	case PhaseInFlight:
		// A start with no outcome means the route may have run. Nothing here can
		// establish what it did, and re-running it could repeat an effect, so the
		// activation is resolved as uncertain and Heartime is told exactly that.
		return r.resolveAndReport(ctx, r.interrupted(occurrence))
	}
	evidence, _, err := r.Store.Load(occurrence)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if evidence.IsReturnReview() {
		return r.answerReview(ctx, evidence)
	}
	route, found := r.Config.Route(evidence)
	if !found {
		// Silence would leave Heartime re-reviewing this forever. An obligation
		// this ingress does not serve is contained: recorded, bounded, and owning
		// no active work.
		return r.resolveAndReport(ctx, r.base(evidence, Contained,
			fmt.Sprintf("no route of this ingress serves kind %q handed off to %s; no work was started and institutional state is unchanged", evidence.Kind, evidence.Handoff.ID)))
	}
	result, err := r.execute(ctx, evidence, route)
	if err != nil {
		// Nothing was invoked, or the intent to invoke could not be made durable.
		// The occurrence stays undispatched and the next sweep tries again.
		return err
	}
	return r.resolveAndReport(ctx, result)
}

// base is the result skeleton every resolution shares.
func (r *Receiver) base(evidence Evidence, outcome Outcome, reason string) Result {
	digest := ""
	if _, raw, err := r.Store.Load(evidence.OccurrenceID); err == nil {
		digest = institution.Hash(raw)
	}
	return Result{
		Occurrence:     evidence.OccurrenceID,
		Contract:       evidence.Contract,
		Responsibility: evidence.Responsibility,
		Kind:           evidence.Kind,
		Nominal:        evidence.Nominal,
		DeliveryDigest: digest,
		EndedAt:        r.Now().UTC().Format(utcLayout),
		Parent:         evidence.Parent,
		Outcome:        outcome,
		Reason:         reason,
	}
}

// interrupted describes an activation that was started and never resolved.
func (r *Receiver) interrupted(occurrence string) Result {
	evidence, _, err := r.Store.Load(occurrence)
	if err != nil {
		evidence = Evidence{OccurrenceID: occurrence}
	}
	result := r.base(evidence, Uncertain, "the activation was started and this ingress was interrupted before it recorded an outcome; whether the route produced effects cannot be established here")
	result.Interrupted = true
	var start Start
	if _, err := readJSONIfPresent(filepath.Join(r.Store.Dir(occurrence), startedFile), &start); err == nil {
		result.Route, result.Argv, result.StartedAt = start.Route, start.Argv, start.At
	}
	return result
}

// resolveAndReport stores the immutable result and returns it to Heartime.
func (r *Receiver) resolveAndReport(ctx context.Context, result Result) error {
	resolution, err := r.Store.Resolve(result, r.Now())
	if err != nil {
		return err
	}
	return r.report(ctx, resolution)
}

func (r *Receiver) logf(format string, arguments ...any) {
	if r.Logger != nil {
		r.Logger.Printf(format, arguments...)
	}
}

func refuse(writer http.ResponseWriter, status int, reason string) {
	writer.Header().Set("Content-Type", "application/json")
	writer.WriteHeader(status)
	_ = json.NewEncoder(writer).Encode(map[string]any{"accepted": false, "reason": reason})
}
