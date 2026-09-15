package effects

import "testing"

func TestUncertainRecovery(t *testing.T) {
	tests := []struct {
		class Class
		want  Recovery
	}{
		{Observe, Retry},
		{Idempotent, Retry},
		{Reconcilable, ObserveFirst},
		{AtMostOnce, HumanDecision},
		{Irreversible, HumanDecision},
	}
	for _, tt := range tests {
		got, err := Recover(tt.class)
		if err != nil {
			t.Fatalf("%s: %v", tt.class, err)
		}
		if got != tt.want {
			t.Fatalf("%s: got %s want %s", tt.class, got, tt.want)
		}
	}
}

func TestLostAcknowledgementDoesNotBecomeFailed(t *testing.T) {
	state, err := Next(Dispatched, LoseCertainty)
	if err != nil {
		t.Fatal(err)
	}
	if state != Uncertain {
		t.Fatalf("got %s want %s", state, Uncertain)
	}
}

func TestObservationCanResolveUncertainty(t *testing.T) {
	state, err := Next(Uncertain, VerifyMatch)
	if err != nil {
		t.Fatal(err)
	}
	if state != Verified {
		t.Fatalf("got %s want %s", state, Verified)
	}
}
