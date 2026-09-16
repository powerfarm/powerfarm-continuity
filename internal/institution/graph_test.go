package institution

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"powerfarm.dev/continuity/v2/internal/compiler"
	"powerfarm.dev/continuity/v2/internal/effects"
	"powerfarm.dev/continuity/v2/internal/journal"
	"powerfarm.dev/continuity/v2/internal/model"
	"powerfarm.dev/continuity/v2/internal/policy"
	cruntime "powerfarm.dev/continuity/v2/internal/runtime"
)

type mandateAuthority struct{ mandate Mandate }

func (a mandateAuthority) Decide(_ context.Context, step model.BundleStep) (policy.Decision, error) {
	return policy.Decision{Allow: slices.Contains(a.mandate.AllowedCapabilities, step.Capability), Reason: "test mandate"}, nil
}

// TestWorkGraphRunsSuccessiveTurnsThroughTheEffectJournal executes the shipped
// work graph and capability profiles with the real compiler, executor and
// SQLite effect journal, occupied by two separate command-route processes that
// share nothing but the institution's durable state.
func TestWorkGraphRunsSuccessiveTurnsThroughTheEffectJournal(t *testing.T) {
	assets := filepath.Join("..", "..", "examples", "institution")
	graph, err := os.ReadFile(filepath.Join(assets, "work.workflow.json"))
	must(t, err)
	var mandate Mandate
	raw, err := os.ReadFile(filepath.Join(assets, "mandate.json"))
	must(t, err)
	must(t, json.Unmarshal(raw, &mandate))
	mandate.ExpiresAt = time.Now().Add(time.Hour).UTC().Format(time.RFC3339)

	root := t.TempDir()
	cas := CAS{Root: filepath.Join(root, "cas")}
	compilerRef, err := cas.Put([]byte(`{"program":"graph-test"}`), "application/json")
	must(t, err)
	template, err := os.ReadFile(filepath.Join(assets, "template.txt"))
	must(t, err)
	templateRef, err := cas.Put(template, "text/plain")
	must(t, err)
	source := []byte("func TestAlpha(t *testing.T) {}\nfunc TestBeta(t *testing.T) {}\nfunc TestGamma(t *testing.T) {}")
	catalog := []byte("id: HEART-001\nid: HEART-002\nid: HEART-003")
	counter := filepath.Join(root, "invocations")
	t.Setenv("POWERFARM_TEST_INVOCATIONS", counter)

	turn := func(occurrence string, script string) *Work {
		t.Helper()
		id := strings.TrimPrefix(Hash([]byte(occurrence)), "sha256:")
		route := Route{Name: "command:" + occurrence, Occupy: CommandIntelligence(cas, []string{"/bin/sh", "-c", script})}
		work, err := LoadWork(root, mandate, source, catalog, compilerRef, templateRef, id, route, time.Now().UTC())
		must(t, err)
		bundle, err := compiler.Compile(graph, filepath.Join(assets, "capabilities"))
		must(t, err)
		bundle.BundleDigest = Hash([]byte(bundle.BundleDigest + "\x00" + occurrence))
		ledger, err := journal.Open(filepath.Join(root, "effects.db"))
		must(t, err)
		defer ledger.Close()
		executor := cruntime.Executor{Journal: ledger, Authorizer: mandateAuthority{mandate: mandate}, Dispatcher: work}
		for _, step := range bundle.Steps {
			record, err := executor.ExecuteStep(context.Background(), *bundle, step)
			if err != nil || record.State != effects.Verified {
				t.Fatalf("%s: state %s: %v", step.Name, record.State, err)
			}
		}
		return work
	}

	first := `cat >/dev/null; echo a >> "$POWERFARM_TEST_INVOCATIONS"
printf '%s' '{"mappings":[{"caseId":"HEART-001","testName":"TestAlpha","rationale":"first turn"}]}'`
	a := turn("occurrence-a", first)

	second := `input=$(cat); echo b >> "$POWERFARM_TEST_INVOCATIONS"
case "$input" in *"[current-dataset; "*'"testName":"TestAlpha"'*) ;; *) exit 1;; esac
printf '%s' '{"mappings":[{"caseId":"HEART-001","testName":"TestAlpha","rationale":"first turn"},{"caseId":"HEART-002","testName":"TestBeta","rationale":"second turn"}]}'`
	b := turn("occurrence-b", second)
	if a.State.Period != 1 || b.State.Period != 2 || b.State.LastTurn == a.State.LastTurn {
		t.Fatalf("each activation must advance the institution: %+v %+v", a.State, b.State)
	}
	output, err := os.ReadFile(filepath.Join(root, mandate.AllowedOutput))
	must(t, err)
	if Hash(output) != b.State.Dataset.Digest {
		t.Fatal("the durable output must be the verified dataset")
	}

	replay := turn("occurrence-b", second)
	invocations, err := os.ReadFile(counter)
	must(t, err)
	if string(invocations) != "a\nb\n" || replay.WakePack.Digest != "" {
		t.Fatalf("replaying a verified activation must not invoke the route again: %q", invocations)
	}
}
