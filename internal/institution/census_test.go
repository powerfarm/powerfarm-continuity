package institution

import (
	"strings"
	"testing"
)

// CENSUS-002: a routed receipt is not a recorded observation.
func TestReceiptRequiresCompletedRunAndExactPayload(t *testing.T) {
	digest := strings.Repeat("a", 64)
	run := map[string]any{"status": "completed", "receipt_id": "rcp_1", "output": `{"outputs":{"preserve":{"digest":"` + digest + `"}}}`}
	body := map[string]any{"result": map[string]any{"receipt_id": "rcp_1", "status": "routed", "runs": []any{run}}}
	if !receiptMatches(body, "rcp_1", digest) {
		t.Fatal("a completed run preserving the exact payload must verify")
	}
	run["status"] = "dispatched"
	if receiptMatches(body, "rcp_1", digest) {
		t.Fatal("transport progress was accepted as a recorded effect")
	}
	run["status"] = "completed"
	if receiptMatches(body, "rcp_1", strings.Repeat("b", 64)) || receiptMatches(body, "rcp_2", digest) || receiptMatches(nil, "rcp_1", digest) {
		t.Fatal("another payload or receipt must not verify")
	}
}

// CENSUS-001.
func TestCensusClassesStayDistinct(t *testing.T) {
	cohort := []Expected{{"pf.present", "pf.app-park.8gb"}, {"pf.missing", "pf.app-park.8gb"}, {"pf.moved", "pf.app-park.8gb"}, {"pf.elsewhere-member", "pf.engine-park.8gb"}}
	observed := ClassifyInventory(cohort, Inventory{Place: "pf.app-park.8gb", Entries: []string{"present", "stranger", "moved"}, Complete: true})
	for _, observation := range observed {
		if observation.ID == "pf.elsewhere-member" {
			t.Fatal("a probe of one place must not observe members declared at another place")
		}
	}
	observed = append(observed,
		Observation{ID: "pf.moved", Place: "pf.engine-park.8gb", Conclusive: true, Present: true},
		Observation{ID: "pf.forbidden", Place: "pf.app-park.8gb", Conclusive: true, Present: true, Prohibited: true, Rule: "pf.contract.parks.no-foreign-runtimes"},
		Observation{ID: "pf.flagged-without-rule", Place: "pf.app-park.8gb", Conclusive: true, Present: true, Prohibited: true},
	)
	classes := map[string][]string{}
	for _, discrepancy := range Reconcile(cohort, observed) {
		classes[discrepancy.ID] = append(classes[discrepancy.ID], discrepancy.Class)
	}
	want := map[string]string{
		"pf.present":                      ClassRecognizedExpected,
		"pf.missing":                      ClassExpectedAbsent,
		"unrecognized-directory:stranger": ClassUnrecognizedPresent,
		"pf.forbidden":                    ClassPresentProhibited,
		"pf.flagged-without-rule":         ClassUnrecognizedPresent,
		"pf.elsewhere-member":             ClassUnknown,
	}
	for id, class := range want {
		if len(classes[id]) != 1 || classes[id][0] != class {
			t.Fatalf("%s: got %v, want %s", id, classes[id], class)
		}
	}
	if len(classes["pf.moved"]) != 2 || classes["pf.moved"][0] != ClassRecognizedElsewhere || classes["pf.moved"][1] != ClassRecognizedExpected {
		t.Fatalf("observations of one member at two places must stay distinct: %v", classes["pf.moved"])
	}
	inconclusive := Reconcile(cohort[:1], []Observation{{ID: "pf.present", Place: "pf.app-park.8gb", Conclusive: false}})
	if inconclusive[0].Class != ClassUnknown {
		t.Fatal("an inconclusive probe must not become absence")
	}
}

func TestAttentionOnlyForDiscrepancies(t *testing.T) {
	evidence := ContentRef{Digest: "sha256:" + strings.Repeat("c", 64), MediaType: "application/json", Size: 2}
	discrepancies := []Discrepancy{{ID: "pf.present", Class: ClassRecognizedExpected}, {ID: "pf.missing", Class: ClassExpectedAbsent}, {ID: "stranger", Class: ClassUnrecognizedPresent}}
	cards := Attention(discrepancies, ContractRef{ID: "pf.contract.exec.census", Generation: 1}, evidence, "2026-09-16T10:10:00Z")
	if len(cards) != 2 || cards[0].Subject != "pf.missing" || cards[0].Salience <= cards[1].Salience || cards[0].Evidence[0] != evidence {
		t.Fatalf("attention must point at evidence for discrepancies only: %+v", cards)
	}
}
