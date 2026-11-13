package sohacli

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

func TestTUIContextRequiresAuthenticatedVisibleFrozenReview(t *testing.T) {
	m, old := testTUI(t)
	next := &tuiFake{}
	m.runID, m.sessionID, m.requestID = "old-run", "old-session", "old-request"
	m.cursors["old-run"] = 42
	m.loadContext = func(_ context.Context, input tuiContextInput) (tuiContextTarget, error) {
		return tuiContextTarget{input: input, backend: next, origin: "https://second.example.test", subject: "other-user"}, nil
	}
	m.Update(tuiKey("c"))
	if !m.contextEditing || !m.composing {
		t.Fatal("context editor missing")
	}
	m.draft = `{"profile":"second","cluster":"cluster-2","namespace":"restricted"}`
	applyTUICmd(m, m.stageContext())
	if m.profile != "local" || m.backend != old || m.confirm == nil || m.pendingContext == nil {
		t.Fatal("context switched before review")
	}
	m.width = 30
	_, command := m.confirmKey("y")
	applyTUICmd(m, command)
	if m.profile != "local" {
		t.Fatal("hidden identity was accepted")
	}
	m.width, m.height = 100, 30
	// Editing the old draft after staging cannot change the frozen target.
	m.draft = `{"profile":"attacker"}`
	_, command = m.confirmKey("y")
	applyTUICmd(m, command)
	if m.profile != "second" || m.subject != "other-user" || m.backend != next || m.scope.cluster != "cluster-2" || m.scope.namespace != "restricted" {
		t.Fatal("frozen context lost")
	}
	if m.runID != "" || m.sessionID != "" || m.requestID != "" || len(m.cursors) != 0 || old.actions != 0 || next.actions != 0 {
		t.Fatal("old references or business actions crossed context")
	}
}

func TestTUIContextCancelFailureAndStaleReplyPreserveCurrentIdentity(t *testing.T) {
	m, old := testTUI(t)
	input := tuiContextInput{Profile: "second", Namespace: "default"}
	m.applyContextResult(tuiContextResult{epoch: m.epoch, target: tuiContextTarget{input: input, backend: &tuiFake{}, origin: "https://second.test", subject: "other"}})
	m.confirmKey("n")
	if m.profile != "local" || m.backend != old || m.pendingContext != nil || m.confirm != nil {
		t.Fatal("canceled context changed identity")
	}
	m.applyContextResult(tuiContextResult{epoch: m.epoch, err: fmt.Errorf("profile denied")})
	if m.profile != "local" || !m.failed || !strings.Contains(m.message, "denied") {
		t.Fatal("failed context changed identity")
	}
	m.epoch++
	m.applyContextResult(tuiContextResult{epoch: m.epoch - 1, target: tuiContextTarget{input: input, backend: &tuiFake{}, origin: "https://stale.test", subject: "stale"}})
	if m.pendingContext != nil || m.profile != "local" {
		t.Fatal("stale context was staged")
	}
}

func TestTUIContextRejectsUnsafeOrAmbiguousInputBeforeLoadingProfile(t *testing.T) {
	for _, value := range []string{`null`, `{}`, `{"profile":"x","namespace":"../other"}`, `{"profile":"x","namespace":"default","server":"https://other.test"}`, `{"profile":"x","namespace":"default"} {}`, `{"profile":"x\nother","namespace":"default"}`} {
		t.Run(value, func(t *testing.T) {
			m, _ := testTUI(t)
			loads := 0
			m.loadContext = func(context.Context, tuiContextInput) (tuiContextTarget, error) {
				loads++
				return tuiContextTarget{}, nil
			}
			m.composing, m.contextEditing, m.draft = true, true, value
			applyTUICmd(m, m.stageContext())
			if loads != 0 || !m.composing || m.confirm != nil {
				t.Fatal("invalid context loaded credentials or bypassed editing")
			}
		})
	}
}
