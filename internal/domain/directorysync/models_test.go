package directorysync

import (
	"errors"
	"testing"
)

func TestDefaultPolicyIsOrganizationOnly(t *testing.T) {
	p := DefaultPolicy("c1")
	if !p.SyncOrganizations || p.SyncPeople {
		t.Fatalf("unsafe defaults: %+v", p)
	}
	if err := p.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestPolicyRejectsDisablingOrganizations(t *testing.T) {
	p := DefaultPolicy("c1")
	p.SyncOrganizations = false
	if !errors.Is(p.Validate(), ErrInvalidPolicy) {
		t.Fatalf("expected invalid policy")
	}
}

func TestRunTransitionsAreTerminal(t *testing.T) {
	if !CanTransitionRun(RunQueued, RunRunning) || !CanTransitionRun(RunRunning, RunSucceeded) {
		t.Fatal("valid transition rejected")
	}
	if CanTransitionRun(RunSucceeded, RunRunning) {
		t.Fatal("terminal run accepted transition")
	}
}
