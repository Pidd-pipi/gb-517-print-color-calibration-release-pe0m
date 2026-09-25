package constants

import "testing"

func TestPressUnitTransitionGraph(t *testing.T) {
	if !CanTransition(PressUnitTransitions, "ready", "setup") {
		t.Fatalf("expected ready -> setup transition to be allowed")
	}
	if CanTransition(PressUnitTransitions, "ready", "unknown") {
		t.Fatal("unknown status must never be accepted")
	}
}

func TestReworkTransitionGraph(t *testing.T) {
	// A released batch has no generic outgoing edge: post-release rework must
	// go through the dedicated StartRework workflow.
	if CanTransition(PrintRunTransitions, "released", "rework_pending") {
		t.Fatal("released -> rework_pending must not be reachable via generic transitions")
	}
	// While waiting the only exit is the proof-gated re-release handled by the
	// rework service; generic moves (e.g. back to proofing) must not bypass it.
	if CanTransition(PrintRunTransitions, "rework_pending", "proofing") {
		t.Fatal("rework_pending -> proofing must not bypass the re-release gate")
	}
	if !CanTransition(PrintRunTransitions, "rework_pending", "released") {
		t.Fatal("rework_pending -> released must be allowed through the gated re-release")
	}
	// Invalidated proofs are terminal.
	if CanTransition(ColorProofTransitions, "invalidated", "review") {
		t.Fatal("invalidated proofs must never move again")
	}
}
