package institution

import (
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
)

func newPack(t *testing.T) (CAS, WakePack) {
	t.Helper()
	cas := CAS{Root: t.TempDir()}
	compiler, err := cas.Put([]byte("compiler identity"), "application/json")
	must(t, err)
	template, err := cas.Put([]byte("occupy one bounded turn"), "text/plain")
	must(t, err)
	pack := WakePack{TurnID: "turn-a", Intelligence: "test-route", Responsibility: ContractRef{ID: "pf.contract.exec.build", Generation: 1}, CapturedAt: "2026-09-16T10:00:00Z", Compiler: compiler, Template: &template, BudgetBytes: 4096}
	for _, role := range MandatoryRoles {
		ref, err := cas.JSON(map[string]string{"role": role})
		must(t, err)
		pack.Mandatory = append(pack.Mandatory, Item{Role: role, Content: ref, Reason: "required for the turn"})
	}
	return cas, pack
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func compiledContext(t *testing.T, cas CAS, manifest ContentRef) (WakePack, string) {
	t.Helper()
	raw, err := cas.Get(manifest)
	must(t, err)
	var pack WakePack
	must(t, json.Unmarshal(raw, &pack))
	context, err := cas.Get(pack.Context)
	must(t, err)
	return pack, string(context)
}

// ATTN-001.
func TestMandatoryMaterialNeverCompetesForBudget(t *testing.T) {
	cas, pack := newPack(t)
	pack.BudgetBytes = 64
	if _, _, err := Compile(cas, pack); err == nil || !strings.Contains(err.Error(), "never truncated") {
		t.Fatalf("mandatory material over budget must fail compilation, got %v", err)
	}
	cas, pack = newPack(t)
	pack.Mandatory = pack.Mandatory[:3]
	if _, _, err := Compile(cas, pack); err == nil {
		t.Fatal("a missing mandatory role must fail compilation")
	}
}

// ATTN-003.
func TestCompiledTurnIsReconstructableAndTamperEvident(t *testing.T) {
	cas, pack := newPack(t)
	_, manifest, err := Compile(cas, pack)
	must(t, err)
	stored, context := compiledContext(t, cas, manifest)
	if stored.Kind != "WakePack" || stored.Intelligence != "test-route" || stored.UsedBytes != len(context) || stored.UsedBytes > stored.BudgetBytes {
		t.Fatalf("manifest must identify the route and account for the budget: %+v", stored)
	}
	for _, item := range stored.Mandatory {
		content, err := cas.Get(item.Content)
		must(t, err)
		if !strings.Contains(context, "["+item.Role+"; "+item.Content.Digest+"]\n"+string(content)) {
			t.Fatalf("mandatory %s is not reconstructable from the compiled context", item.Role)
		}
	}
	must(t, os.WriteFile(cas.Path(pack.Mandatory[0].Content), []byte(`{"role":"forged"}`), 0o600))
	if _, _, err := Compile(cas, pack); !errors.Is(err, ErrIntegrity) {
		t.Fatalf("tampered content must be rejected, got %v", err)
	}
}

// ATTN-002.
func TestAttentionIsSalientBoundedAndExpiring(t *testing.T) {
	cas, pack := newPack(t)
	evidence, err := cas.JSON(map[string]string{"observation": "service missing"})
	must(t, err)
	responsibility := pack.Responsibility
	pack.BudgetBytes = 1400
	pack.Cards = []Card{
		{ID: "expired", Subject: "pf.old", Responsibility: responsibility, Reason: "failure", Evidence: []ContentRef{evidence}, Salience: 9, ExpiresAt: "2026-09-16T09:59:59Z"},
		{ID: "quiet", Subject: strings.Repeat("x", 900), Responsibility: responsibility, Reason: "change", Evidence: []ContentRef{evidence}, Salience: 1},
		{ID: "urgent", Subject: "pf.coloured-places", Responsibility: responsibility, Reason: "contradiction", Evidence: []ContentRef{evidence}, Salience: 5, ExpiresAt: "2026-09-16T11:00:00Z"},
	}
	_, manifest, err := Compile(cas, pack)
	must(t, err)
	stored, context := compiledContext(t, cas, manifest)
	if len(stored.Cards) != 1 || stored.Cards[0].ID != "urgent" || !strings.Contains(context, "[attention; card urgent]") {
		t.Fatalf("the most salient unexpired card must be presented: %+v", stored.Cards)
	}
	reasons := map[string]string{}
	for _, omission := range stored.Omitted {
		reasons[omission.Subject] = omission.Reason
	}
	if !strings.HasPrefix(reasons["card:expired"], "attention expired") || !strings.Contains(reasons["card:quiet"], "remain") {
		t.Fatalf("omitted attention must carry its reason: %+v", stored.Omitted)
	}
	if _, err := cas.Get(evidence); err != nil || stored.Responsibility != responsibility {
		t.Fatal("expired attention must leave evidence and responsibility intact")
	}
	if stored.UsedBytes > stored.BudgetBytes {
		t.Fatal("attention exceeded the budget")
	}
}

func TestOptionalMaterialIsExplainedAndBudgeted(t *testing.T) {
	cas, pack := newPack(t)
	large, err := cas.Put([]byte(strings.Repeat("y", 8000)), "text/plain")
	must(t, err)
	pack.Optional = []Item{{Role: "history", Content: large}}
	if _, _, err := Compile(cas, pack); err == nil {
		t.Fatal("optional material without a selection reason must be refused")
	}
	pack.Optional[0].Reason = "earlier dataset revisions"
	_, manifest, err := Compile(cas, pack)
	must(t, err)
	stored, _ := compiledContext(t, cas, manifest)
	if len(stored.Optional) != 0 || len(stored.Omitted) != 1 || !strings.HasPrefix(stored.Omitted[0].Subject, "history:") {
		t.Fatalf("optional material over budget must be omitted and recorded: %+v", stored)
	}
}

func TestCanonicalScalarContent(t *testing.T) {
	for _, value := range []any{"test source", true, 42, nil} {
		if _, err := Canonical(value); err != nil {
			t.Fatal(err)
		}
	}
}
