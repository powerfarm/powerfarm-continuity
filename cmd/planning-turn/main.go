// Command planning-turn renews the coverage of one responsibility for one more
// period, for one Heartime planning-review occurrence.
//
// Planning extends coverage; Heartime does not plan. This turn reads current
// institutional state, decides the next period within the Direction its mandate
// carries, freezes it as an immutable plan, asks Heartime to accept it, and then
// establishes independently whether the ledger holds the new coverage and the
// next planning evaluation. It reports verified only for that.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"time"

	"powerfarm.dev/continuity/v2/internal/compiler"
	"powerfarm.dev/continuity/v2/internal/institution"
	"powerfarm.dev/continuity/v2/internal/journal"
	"powerfarm.dev/continuity/v2/internal/model"
	"powerfarm.dev/continuity/v2/internal/policy"
	rt "powerfarm.dev/continuity/v2/internal/runtime"
)

// planningAuthority admits exactly the planning capabilities the mandate lists,
// and only while the mandate is unexpired. It is rechecked for every step.
type planningAuthority struct{ mandate institution.Mandate }

func (a planningAuthority) Decide(_ context.Context, step model.BundleStep) (policy.Decision, error) {
	expiresAt, err := time.Parse(time.RFC3339, a.mandate.ExpiresAt)
	if err != nil {
		return policy.Decision{}, err
	}
	for _, admitted := range a.mandate.AllowedCapabilities {
		if admitted == step.Capability && time.Now().Before(expiresAt) {
			return policy.Decision{Allow: true, Reason: "planning capability admitted by the bounded mandate"}, nil
		}
	}
	return policy.Decision{Allow: false, Reason: "outside the planning mandate"}, nil
}

// activation is what a Heartime delivery says about the occurrence that
// activated this turn. Only the terms planning needs are read, and the file is
// the exact bytes the ingress stored.
type activation struct {
	OccurrenceID   string                  `json:"occurrenceId"`
	Contract       institution.ContractRef `json:"contract"`
	Responsibility institution.ContractRef `json:"responsibility"`
	Kind           string                  `json:"kind"`
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	root := flag.String("state", "", "application-owned directory of this planning activation")
	assets := flag.String("assets", "examples/institution", "directory with the planning graph and capability profiles")
	mandatePath := flag.String("mandate", "", "the delegated mandate (default <assets>/planning-mandate.json)")
	delivery := flag.String("delivery", "", "the Heartime delivery that activated this turn, as the ingress stored it")
	occurrence := flag.String("occurrence", "", "occurrence identity, when no delivery file is given")
	contractID := flag.String("contract", "", "Heartime contract id to renew, when no delivery file is given")
	generation := flag.Int("generation", 0, "Heartime contract generation to renew, when no delivery file is given")
	responsibility := flag.String("responsibility", "", "responsibility being planned, when no delivery file is given")
	ledgerArgv := flag.String("ledger", "", `the ledger's own local command as a JSON array, e.g. ["heartime","-db","/var/lib/powerfarm/heartime.db"]`)
	plannerArgv := flag.String("planner", "", "planning route argv as a JSON array; the deterministic period planner is used when absent")
	plannerName := flag.String("planner-name", "", "label of the planning route, recorded in the plan")
	statePath := flag.String("responsibility-state", "", "optional state document of the responsibility being planned, referenced in the plan's basis")
	flag.Parse()
	if *root == "" || *ledgerArgv == "" {
		return errors.New("-state and -ledger are required")
	}
	activated, err := resolveActivation(*delivery, *occurrence, *contractID, *generation, *responsibility)
	if err != nil {
		return err
	}
	if *mandatePath == "" {
		*mandatePath = filepath.Join(*assets, "planning-mandate.json")
	}
	var mandate institution.Mandate
	if err := readStrictJSON(*mandatePath, &mandate); err != nil {
		return fmt.Errorf("mandate: %w", err)
	}
	if err := mandate.ValidatePlanning(time.Now().UTC()); err != nil {
		return err
	}
	var ledger []string
	if err := json.Unmarshal([]byte(*ledgerArgv), &ledger); err != nil || len(ledger) == 0 {
		return errors.New("-ledger must be a non-empty JSON array of strings")
	}

	if err := os.MkdirAll(*root, 0o700); err != nil {
		return err
	}
	lock, err := os.OpenFile(filepath.Join(*root, "planning.lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return err
	}
	defer lock.Close()
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		return fmt.Errorf("another planning turn owns this activation: %w", err)
	}
	defer syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)
	if _, err := os.Stat(filepath.Join(*root, "planning-return.json")); err == nil {
		return errors.New("this activation already has a return; inspect it instead of replaying")
	}

	cas := institution.CAS{Root: filepath.Join(*root, "cas")}
	planning := &institution.Planning{
		Root: *root, CAS: cas,
		Ledger:         institution.Ledger{Argv: ledger, Timeout: time.Duration(mandate.TimeoutSeconds) * time.Second},
		Mandate:        mandate,
		Occurrence:     activated.OccurrenceID,
		Contract:       activated.Contract,
		Responsibility: activated.Responsibility,
		Planner:        institution.PeriodPlanner{CAS: cas},
	}
	if *statePath != "" {
		raw, err := os.ReadFile(*statePath)
		if err != nil {
			return err
		}
		reference, err := cas.Put(raw, "application/json")
		if err != nil {
			return err
		}
		planning.State, planning.StateRef = raw, &reference
	}
	if *plannerArgv != "" {
		var argv []string
		if err := json.Unmarshal([]byte(*plannerArgv), &argv); err != nil || len(argv) == 0 {
			return errors.New("-planner must be a non-empty JSON array of strings")
		}
		if *plannerName == "" {
			return errors.New("-planner-name is required with -planner")
		}
		planning.Planner = institution.CommandPlanner(cas, *plannerName, argv)
	}

	graph, err := os.ReadFile(filepath.Join(*assets, "planning.workflow.json"))
	if err != nil {
		return err
	}
	bundle, err := compiler.Compile(graph, filepath.Join(*assets, "capabilities"))
	if err != nil {
		return err
	}
	// Equal graphs in different periods must never alias one effect.
	bundle.BundleDigest = institution.Hash([]byte(bundle.BundleDigest + "\x00" + activated.OccurrenceID))
	if err := institution.AtomicJSON(filepath.Join(*root, "execution-bundle.json"), bundle); err != nil {
		return err
	}
	effects, err := journal.Open(filepath.Join(*root, "effects.db"))
	if err != nil {
		return err
	}
	defer effects.Close()
	executor := rt.Executor{Journal: effects, Authorizer: planningAuthority{mandate: mandate}, Dispatcher: planning}
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(mandate.TimeoutSeconds)*time.Second)
	defer cancel()
	for _, step := range bundle.Steps {
		if record, err := executor.ExecuteStep(ctx, *bundle, step); err != nil {
			return fmt.Errorf("%s (%s): %w", step.Name, record.State, err)
		}
	}
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(map[string]any{
		"outcome": planning.Outcome, "reason": planning.Reason,
		"occurrence": planning.Occurrence, "contract": planning.Contract,
		"plan": planning.PlanRef, "renewal": planning.Renewal,
		"coverageThrough": planning.Plan.CoverageThrough, "nextReviewAt": planning.Plan.NextReviewAt,
		"renewed": planning.Renewed, "invocations": planning.Attempted,
		"directionDecision": planning.Decision,
	})
}

// resolveActivation prefers the delivery the ingress stored, because it is the
// exact evidence Heartime produced. The explicit terms exist for an operator
// running one turn by hand.
func resolveActivation(delivery, occurrence, contract string, generation int, responsibility string) (activation, error) {
	if delivery != "" {
		raw, err := os.ReadFile(delivery)
		if err != nil {
			return activation{}, err
		}
		var activated activation
		if err := json.Unmarshal(raw, &activated); err != nil {
			return activation{}, err
		}
		if activated.OccurrenceID == "" || activated.Contract.ID == "" || activated.Contract.Generation < 1 {
			return activation{}, errors.New("the delivery names no occurrence or no contract generation")
		}
		return activated, nil
	}
	if occurrence == "" || contract == "" || generation < 1 {
		return activation{}, errors.New("-delivery, or -occurrence with -contract and -generation, is required")
	}
	activated := activation{
		OccurrenceID:   occurrence,
		Contract:       institution.ContractRef{ID: contract, Generation: generation},
		Responsibility: institution.ContractRef{ID: contract, Generation: generation},
		Kind:           "planning-review",
	}
	if responsibility != "" {
		activated.Responsibility = institution.ContractRef{ID: responsibility, Generation: generation}
	}
	return activated, nil
}

func readStrictJSON(path string, target any) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	decoder := json.NewDecoder(file)
	decoder.DisallowUnknownFields()
	return decoder.Decode(target)
}
