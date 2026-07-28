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

func TestPolicyRejectsRealtimeModeForUnsupportedProvider(t *testing.T) {
	p := DefaultPolicy("c1")
	p.Mode = PolicyScheduledAndRealtime
	if err := p.ValidateForProvider(ProviderWeCom); !errors.Is(err, ErrInvalidPolicy) {
		t.Fatalf("ValidateForProvider() error = %v", err)
	}
	if err := p.ValidateForProvider(ProviderFeishu); err != nil {
		t.Fatalf("ValidateForProvider() error = %v", err)
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
