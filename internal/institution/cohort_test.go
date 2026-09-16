package institution

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"powerfarm.dev/continuity/v2/internal/compiler"
	"powerfarm.dev/continuity/v2/internal/journal"
	cruntime "powerfarm.dev/continuity/v2/internal/runtime"
)

const (
	censusPlace      = "pf.app-park.8gb"
	censusOccurrence = "sha256:2222222222222222222222222222222222222222222222222222222222222222"
	machineToken     = "registry-machine-service-credential"
)

// registryStub is the Registry's cohort read as a census actually reaches it: an
// HTTP operation that resolves the caller from a machine service credential.
type registryStub struct {
	t       *testing.T
	server  *httptest.Server
	members []CohortMember
	status  int
	calls   int
	token   string
}

func newRegistry(t *testing.T, members ...CohortMember) *registryStub {
	t.Helper()
	stub := &registryStub{t: t, members: members, status: http.StatusOK}
	stub.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(io.LimitReader(r.Body, 1<<16))
		var asked struct{ Token, Place string }
		var body map[string]string
		if err := json.Unmarshal(raw, &body); err == nil {
			asked.Token, asked.Place = body["p_token"], body["p_place"]
		}
		stub.calls++
		stub.token = asked.Token
		// A human session is never presented: the Registry resolves the calling
		// identity from the credential in the request itself.
		if r.Header.Get("Authorization") != "" {
			stub.t.Error("the census presented a session credential to the Registry")
		}
		if stub.status != http.StatusOK {
			w.WriteHeader(stub.status)
			_, _ = w.Write([]byte(`{"message":"registry.cohort.read required"}`))
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"resolvedAt":  time.Now().UTC().Format(time.RFC3339),
			"identity":    "8f1d0f4e-0000-4000-8000-000000000001",
			"place":       CohortPlace{ID: asked.Place, Path: "/Users/danvoulez/App Park", Machine: "pf.lab-8gb"},
			"members":     stub.members,
			"limitations": []string{"recognition is declared placement, not liveness"},
		})
	}))
	t.Cleanup(stub.server.Close)
	return stub
}

func (s *registryStub) authority(grant string) RegistryCohort {
	return RegistryCohort{Endpoint: s.server.URL, APIKey: "publishable", Token: machineToken, Grant: grant, HTTP: s.server.Client()}
}

func censusMandate(t *testing.T) CensusMandate {
	t.Helper()
	var mandate CensusMandate
	raw, err := os.ReadFile(filepath.Join("..", "..", "examples", "institution", "census-mandate.json"))
	must(t, err)
	must(t, json.Unmarshal(raw, &mandate))
	mandate.ExpiresAt = time.Now().Add(time.Hour).UTC().Format(time.RFC3339)
	return mandate
}

func TestTheCohortIsResolvedUnderAMachineCredentialAndFrozenForTheOccurrence(t *testing.T) {
	registry := newRegistry(t, CohortMember{ID: "pf.coloured-places", Kind: "app", Path: "/Users/danvoulez/App Park/coloured-places"})
	manifest, raw, err := registry.authority("registry.cohort.read").Resolve(context.Background(), censusPlace, censusOccurrence)
	must(t, err)

	if registry.token != machineToken {
		t.Fatalf("the Registry was not asked with the machine credential: %q", registry.token)
	}
	if manifest.Schema != CohortSchema || manifest.Occurrence != censusOccurrence || manifest.Place.ID != censusPlace {
		t.Fatalf("the manifest does not answer this occurrence at this place: %+v", manifest)
	}
	if manifest.Authority.Mechanism != "registry-service-credential" || manifest.Authority.Grant != "registry.cohort.read" || manifest.Authority.Identity == "" {
		t.Fatalf("the manifest does not record under what authority it was resolved: %+v", manifest.Authority)
	}
	if len(manifest.Limitations) == 0 {
		t.Fatal("the authority's coverage limitations were dropped")
	}
	if strings.Contains(string(raw), machineToken) {
		t.Fatal("the authority's answer carries the credential back")
	}
	// The manifest is a value: identical content is the same digest, and it is
	// the digest the occurrence reconciles against.
	cas := CAS{Root: filepath.Join(t.TempDir(), "cas")}
	first, err := cas.JSON(manifest)
	must(t, err)
	again, err := cas.JSON(manifest)
	must(t, err)
	if first.Digest != again.Digest {
		t.Fatal("the same cohort froze to two different digests")
	}
	expected := manifest.Expected()
	if len(expected) != 1 || expected[0].ID != "pf.coloured-places" || expected[0].Place != censusPlace {
		t.Fatalf("the manifest does not project onto what reconciliation compares: %+v", expected)
	}
}

func TestACohortAnswerThatDoesNotMatchTheActivationIsRefused(t *testing.T) {
	registry := newRegistry(t, CohortMember{ID: "pf.coloured-places", Kind: "app"})
	authority := registry.authority("registry.cohort.read")
	if _, _, err := authority.Resolve(context.Background(), "pf.engine-park.512", censusOccurrence); err != nil {
		t.Fatal("the stub answers for whatever place it is asked about")
	}
	// A manifest is refused when it does not answer the activation it claims to.
	manifest := CohortManifest{
		Schema: CohortSchema, Occurrence: censusOccurrence, ResolvedAt: time.Now().UTC().Format(time.RFC3339),
		Authority: CohortProvenance{Source: "powerfarm-registry", Mechanism: "registry-service-credential", Grant: "registry.cohort.read"},
		Place:     CohortPlace{ID: censusPlace},
		Members:   []CohortMember{{ID: "pf.coloured-places", Kind: "app"}},
	}
	cases := map[string]CohortManifest{
		"another occurrence":  withManifest(manifest, func(m *CohortManifest) { m.Occurrence = "sha256:" + strings.Repeat("9", 64) }),
		"another place":       withManifest(manifest, func(m *CohortManifest) { m.Place.ID = "pf.engine-park.512" }),
		"no stated authority": withManifest(manifest, func(m *CohortManifest) { m.Authority.Grant = "" }),
		"a duplicated member": withManifest(manifest, func(m *CohortManifest) {
			m.Members = append(m.Members, CohortMember{ID: "pf.coloured-places", Kind: "app"})
		}),
		"an unnamed kind": withManifest(manifest, func(m *CohortManifest) { m.Members[0].Kind = "" }),
	}
	for name, corrupt := range cases {
		if err := corrupt.Validate(censusPlace, censusOccurrence); !errors.Is(err, ErrCohortAuthority) {
			t.Fatalf("%s was accepted: %v", name, err)
		}
	}
	if err := manifest.Validate(censusPlace, censusOccurrence); err != nil {
		t.Fatalf("a sound manifest was refused: %v", err)
	}
}

func withManifest(manifest CohortManifest, change func(*CohortManifest)) CohortManifest {
	copied := manifest
	copied.Members = append([]CohortMember{}, manifest.Members...)
	change(&copied)
	return copied
}

func TestACensusWithNoInstitutionalAuthorityIsContainedAndAsksDirection(t *testing.T) {
	registry := newRegistry(t, CohortMember{ID: "pf.coloured-places", Kind: "app"})
	registry.status = http.StatusForbidden
	census := runCensus(t, registry.authority("registry.cohort.read"))

	if census.Outcome != OutcomeContained {
		t.Fatalf("a census that cannot establish what is expected reported %s: %s", census.Outcome, census.Reason)
	}
	if census.Cohort.Digest != "" || len(census.Expected) != 0 {
		t.Fatal("a contained census still carried a cohort")
	}
	if census.ReceiptID != "" || census.ObservationRef.Digest != "" {
		t.Fatal("a census with no cohort observed or recorded something anyway")
	}
	if census.Decision == nil || census.Decision.Boundary != BoundaryHumanOnlyAuthority {
		t.Fatalf("the authority boundary was not raised: %+v", census.Decision)
	}
	if !strings.Contains(census.Decision.ExactDecision, "registry.cohort.read") {
		t.Fatalf("the decision does not name the grant required: %q", census.Decision.ExactDecision)
	}
	if _, err := os.Stat(filepath.Join(census.Root, "direction-decision.json")); err != nil {
		t.Fatalf("the Direction decision was not recorded durably: %v", err)
	}
}

func TestReconciliationFollowsTheCohortTheRegistryCurrentlyRecognizes(t *testing.T) {
	// One place, one inventory, two different recognized cohorts.
	inventory := Inventory{Place: censusPlace, Entries: []string{"coloured-places", "cockpit"}, Complete: true}

	before := newRegistry(t, CohortMember{ID: "pf.coloured-places", Kind: "app"})
	first, _, err := before.authority("registry.cohort.read").Resolve(context.Background(), censusPlace, censusOccurrence)
	must(t, err)
	classes := classesOf(Reconcile(first.Expected(), ClassifyInventory(first.Expected(), inventory)))
	if classes["pf.coloured-places"] != ClassRecognizedExpected || classes["unrecognized-directory:cockpit"] != ClassUnrecognizedPresent {
		t.Fatalf("before the change: %v", classes)
	}

	// The Registry now recognizes cockpit at this place, and nothing else moved.
	after := newRegistry(t,
		CohortMember{ID: "pf.coloured-places", Kind: "app"},
		CohortMember{ID: "pf.cockpit", Kind: "app"})
	second, _, err := after.authority("registry.cohort.read").Resolve(context.Background(), censusPlace, censusOccurrence)
	must(t, err)
	classes = classesOf(Reconcile(second.Expected(), ClassifyInventory(second.Expected(), inventory)))
	if classes["pf.cockpit"] != ClassRecognizedExpected {
		t.Fatalf("a newly recognized member stayed unrecognized: %v", classes)
	}
	if _, present := classes["unrecognized-directory:cockpit"]; present {
		t.Fatalf("the same directory is both recognized and unrecognized: %v", classes)
	}
	if first.Members[0] != second.Members[0] || len(first.Members) == len(second.Members) {
		t.Fatal("the two resolutions were not actually different cohorts")
	}
}

func TestAResolvedCohortDoesNotChangeDuringItsOccurrence(t *testing.T) {
	registry := newRegistry(t, CohortMember{ID: "pf.coloured-places", Kind: "app"})
	authority := registry.authority("registry.cohort.read")
	manifest, _, err := authority.Resolve(context.Background(), censusPlace, censusOccurrence)
	must(t, err)
	frozen, err := CAS{Root: filepath.Join(t.TempDir(), "cas")}.JSON(manifest)
	must(t, err)

	// The Registry changes underneath while the occurrence is still running.
	registry.members = append(registry.members, CohortMember{ID: "pf.cockpit", Kind: "app"})
	again, err := CAS{Root: filepath.Join(t.TempDir(), "cas")}.JSON(manifest)
	must(t, err)
	if again.Digest != frozen.Digest || len(manifest.Members) != 1 {
		t.Fatal("the cohort of an occurrence in flight followed the Registry")
	}
	// A later occurrence resolves its own, and gets the new one.
	later, _, err := authority.Resolve(context.Background(), censusPlace, "sha256:"+strings.Repeat("3", 64))
	must(t, err)
	if len(later.Members) != 2 {
		t.Fatalf("a later occurrence did not resolve the current cohort: %+v", later.Members)
	}
}

func TestACensusMandateRefusesWhatItMustNotDelegate(t *testing.T) {
	sound := censusMandate(t)
	if err := sound.ValidateCensus(time.Now().UTC()); err != nil {
		t.Fatalf("the shipped census mandate is not valid: %v", err)
	}
	cases := map[string]func(CensusMandate) CensusMandate{
		"repair":            func(m CensusMandate) CensusMandate { m.Census.Repair = true; return m },
		"no place":          func(m CensusMandate) CensusMandate { m.Census.Place = ""; return m },
		"no contract":       func(m CensusMandate) CensusMandate { m.Census.ObservabilityContract = ""; return m },
		"no cohort grant":   func(m CensusMandate) CensusMandate { m.Census.CohortGrant = ""; return m },
		"no time bound":     func(m CensusMandate) CensusMandate { m.TimeoutSeconds = 0; return m },
		"no containment":    func(m CensusMandate) CensusMandate { m.ContainmentReviewSeconds = 0; return m },
		"an expired grant":  func(m CensusMandate) CensusMandate { m.ExpiresAt = "2020-01-01T00:00:00Z"; return m },
		"no responsibility": func(m CensusMandate) CensusMandate { m.Contract = ContractRef{}; return m },
	}
	for name, corrupt := range cases {
		if err := corrupt(sound).ValidateCensus(time.Now().UTC()); !errors.Is(err, ErrUnboundedMandate) {
			t.Fatalf("%s was accepted: %v", name, err)
		}
	}
}

// runCensus executes the shipped census graph and capability profiles with the
// real compiler, executor and SQLite effect journal.
func runCensus(t *testing.T, authority CohortAuthority) *Census {
	t.Helper()
	assets := filepath.Join("..", "..", "examples", "institution")
	graph, err := os.ReadFile(filepath.Join(assets, "census.workflow.json"))
	must(t, err)
	mandate := censusMandate(t)
	root := t.TempDir()
	census := &Census{
		Root: root, CAS: CAS{Root: filepath.Join(root, "cas")}, Responsibility: mandate.Contract,
		Mandate: mandate, Occurrence: censusOccurrence, Authority: authority,
		HTTP: &http.Client{Timeout: 5 * time.Second},
	}
	bundle, err := compiler.Compile(graph, filepath.Join(assets, "capabilities"))
	must(t, err)
	bundle.BundleDigest = Hash([]byte(bundle.BundleDigest + "\x00" + census.Occurrence))
	effects, err := journal.Open(filepath.Join(root, "effects.db"))
	must(t, err)
	defer effects.Close()
	executor := cruntime.Executor{Journal: effects, Authorizer: mandateAuthority{mandate: mandate.Mandate}, Dispatcher: census}
	for _, step := range bundle.Steps {
		if _, err := executor.ExecuteStep(context.Background(), *bundle, step); err != nil {
			t.Fatalf("%s: %v", step.Name, err)
		}
	}
	return census
}

func classesOf(discrepancies []Discrepancy) map[string]string {
	classes := map[string]string{}
	for _, discrepancy := range discrepancies {
		classes[discrepancy.ID] = discrepancy.Class
	}
	return classes
}
