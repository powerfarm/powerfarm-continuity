// Command institution-turn executes one bounded turn of the Powerfarm
// traceability responsibility for one Heartime occurrence: it compiles the work
// graph, occupies the turn through an intelligence route, recovers technically
// and records the return. It never schedules anything itself.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"runtime/debug"
	"slices"
	"strings"
	"syscall"
	"time"

	"powerfarm.dev/continuity/v2/internal/compiler"
	"powerfarm.dev/continuity/v2/internal/institution"
	"powerfarm.dev/continuity/v2/internal/journal"
	"powerfarm.dev/continuity/v2/internal/model"
	"powerfarm.dev/continuity/v2/internal/policy"
	rt "powerfarm.dev/continuity/v2/internal/runtime"
)

// mandateAuthority admits exactly the capabilities the mandate lists, and only
// while the mandate is unexpired. It is rechecked for every step.
type mandateAuthority struct{ mandate institution.Mandate }

func (a mandateAuthority) Decide(_ context.Context, step model.BundleStep) (policy.Decision, error) {
	expiresAt, err := time.Parse(time.RFC3339, a.mandate.ExpiresAt)
	if err != nil {
		return policy.Decision{}, err
	}
	if time.Now().Before(expiresAt) && slices.Contains(a.mandate.AllowedCapabilities, step.Capability) {
		return policy.Decision{Allow: true, Reason: "capability admitted by the bounded mandate"}, nil
	}
	return policy.Decision{Allow: false, Reason: "outside the mandate"}, nil
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	root := flag.String("state", "", "application-owned directory of the responsibility's durable state")
	assets := flag.String("assets", "examples/institution", "directory with the work graph, capability profiles, mandate and template")
	source := flag.String("test-source", "", "current Heartime test source")
	requirements := flag.String("requirements", "", "current conformance catalog")
	occurrence := flag.String("occurrence", "", "Heartime occurrence identity that activated this turn")
	primaryName := flag.String("primary-name", "", "label of the primary route, recorded in the WakePack")
	primary := flag.String("primary", "", `primary command route argv as a JSON array; "{schema}" is replaced by the output schema path`)
	primaryCodex := flag.String("primary-codex", "", "use a Codex CLI executable as the primary route instead of a command route")
	alternateName := flag.String("alternate-name", "", "label of the alternate route")
	alternate := flag.String("alternate", "", "alternate command route argv as a JSON array")
	flag.Parse()
	if *root == "" || *source == "" || *requirements == "" || *occurrence == "" || *primaryName == "" || (*primary == "") == (*primaryCodex == "") {
		return errors.New("-state, -test-source, -requirements, -occurrence, -primary-name and exactly one of -primary or -primary-codex are required")
	}
	if err := os.MkdirAll(*root, 0o700); err != nil {
		return err
	}
	lock, err := os.OpenFile(filepath.Join(*root, "executor.lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return err
	}
	defer lock.Close()
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		return fmt.Errorf("another executor owns this responsibility's state: %w", err)
	}
	defer syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)

	turn := strings.TrimPrefix(institution.Hash([]byte(*occurrence)), "sha256:")
	if _, err := os.Stat(filepath.Join(*root, turn+".return.json")); err == nil {
		return errors.New("this activation already has a return; inspect it instead of replaying")
	}
	var mandate institution.Mandate
	if err := readStrictJSON(filepath.Join(*assets, "mandate.json"), &mandate); err != nil {
		return fmt.Errorf("mandate: %w", err)
	}
	testSource, err := os.ReadFile(*source)
	if err != nil {
		return err
	}
	catalog, err := os.ReadFile(*requirements)
	if err != nil {
		return err
	}
	cas := institution.CAS{Root: filepath.Join(*root, "cas")}
	compilerRef, err := compilerIdentity(cas)
	if err != nil {
		return err
	}
	template, err := os.ReadFile(filepath.Join(*assets, "template.txt"))
	if err != nil {
		return err
	}
	templateRef, err := cas.Put(template, "text/plain")
	if err != nil {
		return err
	}
	schemaRef, err := cas.Put([]byte(institution.ProposalSchema), "application/schema+json")
	if err != nil {
		return err
	}

	primaryRoute := institution.Route{Name: *primaryName}
	if *primaryCodex != "" {
		primaryRoute.Occupy = institution.CodexIntelligence(cas, *primaryCodex)
	} else if primaryRoute.Occupy, err = commandRoute(cas, *primary, cas.Path(schemaRef)); err != nil {
		return fmt.Errorf("-primary: %w", err)
	}
	work, err := institution.LoadWork(*root, mandate, testSource, catalog, compilerRef, templateRef, turn, primaryRoute, time.Now().UTC())
	if err != nil {
		return err
	}
	if *alternate != "" {
		if *alternateName == "" {
			return errors.New("-alternate-name is required with -alternate")
		}
		work.Alternate = institution.Route{Name: *alternateName}
		if work.Alternate.Occupy, err = commandRoute(cas, *alternate, cas.Path(schemaRef)); err != nil {
			return fmt.Errorf("-alternate: %w", err)
		}
	}

	graph, err := os.ReadFile(filepath.Join(*assets, "work.workflow.json"))
	if err != nil {
		return err
	}
	bundle, err := compiler.Compile(graph, filepath.Join(*assets, "capabilities"))
	if err != nil {
		return err
	}
	// The compiled bundle identity is kept; the execution identity adds the
	// activation so equal graphs in different periods never alias one effect.
	compiled := bundle.BundleDigest
	bundle.BundleDigest = institution.Hash([]byte(compiled + "\x00" + *occurrence))
	if err := institution.AtomicJSON(filepath.Join(*root, turn+".bundle.json"), map[string]any{"compiledBundleDigest": compiled, "activation": *occurrence, "execution": bundle}); err != nil {
		return err
	}
	effects, err := journal.Open(filepath.Join(*root, "effects.db"))
	if err != nil {
		return err
	}
	defer effects.Close()
	executor := rt.Executor{Journal: effects, Authorizer: mandateAuthority{mandate: mandate}, Dispatcher: work}
	for _, step := range bundle.Steps {
		// Within one activation, a restarted process resumes from the structured
		// stage results it already persisted. Across activations nothing is
		// restored: each turn starts from the responsibility's current state.
		_ = work.RestoreStage(step.Capability)
		if record, err := executor.ExecuteStep(context.Background(), *bundle, step); err != nil {
			return fmt.Errorf("%s (%s): %w", step.Name, record.State, err)
		}
	}
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(map[string]any{"turn": turn, "wakePack": work.WakePack, "state": work.State, "verified": work.Verified, "invocations": work.Attempted, "directionDecision": work.State.DirectionDecision})
}

// compilerIdentity stores a small manifest identifying this executable: its
// digest and size plus the Go and version-control build settings.
func compilerIdentity(cas institution.CAS) (institution.ContentRef, error) {
	executable, err := os.Executable()
	if err != nil {
		return institution.ContentRef{}, err
	}
	content, err := os.ReadFile(executable)
	if err != nil {
		return institution.ContentRef{}, err
	}
	manifest := map[string]any{"program": "institution-turn", "executableSha256": institution.Hash(content), "executableBytes": len(content)}
	if info, ok := debug.ReadBuildInfo(); ok {
		manifest["goVersion"] = info.GoVersion
		manifest["module"] = info.Main.Path
		vcs := map[string]string{}
		for _, setting := range info.Settings {
			if strings.HasPrefix(setting.Key, "vcs.") {
				vcs[setting.Key] = setting.Value
			}
		}
		manifest["vcs"] = vcs
	}
	return cas.JSON(manifest)
}

func commandRoute(cas institution.CAS, argvJSON, schemaPath string) (institution.Intelligence, error) {
	var argv []string
	if err := json.Unmarshal([]byte(argvJSON), &argv); err != nil || len(argv) == 0 {
		return nil, errors.New("route must be a non-empty JSON array of strings")
	}
	for index := range argv {
		argv[index] = strings.ReplaceAll(argv[index], "{schema}", schemaPath)
	}
	return institution.CommandIntelligence(cas, argv), nil
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
