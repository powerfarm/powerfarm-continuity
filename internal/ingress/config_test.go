package ingress

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"powerfarm.dev/continuity/v2/internal/institution"
)

func base(routes ...Route) Config {
	return Config{TokenFile: "token", State: "state", Report: []string{"heartime", "report"}, Routes: routes}
}

func route() Route {
	return Route{Name: "census", Handoff: censusHandoff, Kinds: []string{KindWork}, Argv: []string{"census-turn"}}
}

func TestConfigurationRefusesWhatWouldOtherwiseBeGuessed(t *testing.T) {
	claimsReviews := route()
	claimsReviews.Kinds = []string{ReturnReviewPrefix + "sha256:" + strings.Repeat("0", 64)}
	unknownKind := route()
	unknownKind.Kinds = []string{"census"}
	unacceptable := route()
	unacceptable.Outcome = OutcomeMap{Field: "verified", WhenTrue: "acknowledged"}
	second := route()
	second.Name = "second"

	cases := map[string]Config{
		"no credential":                       {State: "state", Report: []string{"heartime"}, Routes: []Route{route()}},
		"no local state":                      {TokenFile: "token", Report: []string{"heartime"}, Routes: []Route{route()}},
		"no way to return an outcome":         {TokenFile: "token", State: "state", Routes: []Route{route()}},
		"no route":                            base(),
		"a route claiming reviews":            base(claimsReviews),
		"a route for an unknown kind":         base(unknownKind),
		"two routes for one kind":             base(route(), second),
		"an outcome Heartime does not accept": base(unacceptable),
	}
	for name, config := range cases {
		if err := config.normalize(); !errors.Is(err, ErrInvalidConfig) {
			t.Fatalf("%s was accepted: %v", name, err)
		}
	}
}

func TestConfigurationDefaultsAreExplicit(t *testing.T) {
	config := base(route())
	if err := config.normalize(); err != nil {
		t.Fatal(err)
	}
	if config.Listen != defaultListen || config.ReviewBound != defaultReviewBound || config.SweepSeconds != defaultSweepSeconds {
		t.Fatalf("operating defaults were not applied: %+v", config)
	}
	declared := config.Routes[0].Outcome
	// A route that does not say how success is established never produces one.
	if declared.OnMissing != Uncertain || declared.WhenFalse != Uncertain {
		t.Fatalf("a silent route must not imply verification: %+v", declared)
	}
	if declared.OnRefusal != Contained || declared.OnFailure != Failed || declared.WhenTrue != Verified {
		t.Fatalf("declared meanings were not defaulted: %+v", declared)
	}
	if config.Routes[0].TimeoutSeconds != defaultTimeoutSeconds {
		t.Fatal("a route without a time bound must still be bounded")
	}
}

func TestConfigurationRefusesUnknownTerms(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "ingress.json")
	if err := os.WriteFile(path, []byte(`{"tokenFile":"t","state":"s","report":["heartime"],"reviewBounds":4,"routes":[]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadConfig(path); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("a misspelled term kept its default silently: %v", err)
	}
}

func TestRouteSelectionFollowsTheHandoffAndItsGeneration(t *testing.T) {
	pinned := route()
	pinned.Name, pinned.Generation = "pinned", 2
	config := base(pinned)
	if err := config.normalize(); err != nil {
		t.Fatal(err)
	}
	delivered, _ := evidence(t, censusContract, 1, obligation, KindWork, nominal, "")
	if _, found := config.Route(delivered); found {
		t.Fatal("a route pinned to a generation accepted another one")
	}
	delivered.Handoff.Generation = 2
	if _, found := config.Route(delivered); !found {
		t.Fatal("a route pinned to a generation refused its own")
	}
}

func TestActivationTermsReachARouteOnlyThroughPlaceholders(t *testing.T) {
	delivered, _ := evidence(t, censusContract, 1, obligation, KindWork, nominal, "")
	expanded := expand([]string{"census-turn", "-occurrence", "{occurrence}", "-state", "{stateDir}", "-at", "{nominal}", "-of", "{contract}/{generation}"},
		delivered, "/state/work", "/state/delivery.json")
	want := []string{"census-turn", "-occurrence", delivered.OccurrenceID, "-state", "/state/work", "-at", nominal, "-of", censusContract + "/1"}
	for index := range want {
		if expanded[index] != want[index] {
			t.Fatalf("argument %d expanded to %q, want %q", index, expanded[index], want[index])
		}
	}
}

func TestEvidenceValidationRefusesWhatItCannotRecompute(t *testing.T) {
	delivered, _ := evidence(t, censusContract, 1, obligation, KindWork, nominal, "")
	if err := delivered.Validate(); err != nil {
		t.Fatal(err)
	}
	mislabelled := delivered
	mislabelled.ObligationID = "another-obligation"
	if err := mislabelled.Validate(); !errors.Is(err, ErrInvalidEvidence) {
		t.Fatalf("evidence whose terms do not produce its identity was accepted: %v", err)
	}
	review, _ := evidence(t, censusContract, 1, obligation, ReturnReviewPrefix+institution.Hash([]byte("parent")), nominal, institution.Hash([]byte("parent")))
	if err := review.Validate(); err != nil {
		t.Fatal(err)
	}
	disowned := review
	disowned.Parent = institution.Hash([]byte("a different parent"))
	if err := disowned.Validate(); !errors.Is(err, ErrInvalidEvidence) {
		t.Fatalf("a review naming two different parents was accepted: %v", err)
	}
}

func TestTheShippedExampleConfigurationIsValid(t *testing.T) {
	config, err := LoadConfig(filepath.Join("..", "..", "examples", "ingress", "ingress.json"))
	if err != nil {
		t.Fatal(err)
	}
	// The example wires the two responsibilities that exist today, and leaves
	// each route's own placeholders untouched for that route to expand.
	if len(config.Routes) != 2 {
		t.Fatalf("the example wires %d route(s)", len(config.Routes))
	}
	delivered, _ := evidence(t, censusContract, 1, obligation, KindWork, nominal, "")
	delivered.Handoff.ID = "pf.contract.exec.build-powerfarm"
	route, found := config.Route(delivered)
	if !found {
		t.Fatal("the example does not serve the work responsibility it documents")
	}
	if !strings.Contains(strings.Join(expand(route.Argv, delivered, "/state", "/state/delivery.json"), " "), "{schema}") {
		t.Fatal("the ingress consumed a placeholder that belongs to the route it invokes")
	}
}
