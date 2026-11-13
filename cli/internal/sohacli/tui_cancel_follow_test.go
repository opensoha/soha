package sohacli

import (
	"fmt"
	"strings"
	"testing"
)

func TestTUICancelRefollowRetainsFrozenSessionContext(t *testing.T) {
	m, _ := testTUI(t)
	const run = "agent:11111111-1111-4111-8111-111111111111"
	const session = "22222222-2222-4222-8222-222222222222"
	m.runID, m.sessionID, m.busy = run, session, true
	m.reviewAction("x")
	if m.confirm == nil || m.confirm.row.id != run {
		t.Fatal("cancellation did not freeze the original run")
	}
	if tuiString(m.confirm.row.value, "sessionId") != session {
		t.Fatal("cancellation review lost the owned session context")
	}
	// The action response retains the reviewed row. A later follow must use its
	// frozen session even when the current-session field changed meanwhile.
	m.scope = m.confirm.scope
	m.snapshot.rows = []tuiRow{m.confirm.row}
	m.sessionID, m.busy = "other-local-context", false
	if m.followKey() == nil || m.runID != run || m.sessionID != session {
		t.Fatal("refollow did not preserve the reviewed run and session")
	}
}

func TestTUICanceledReceiptRemainsVisibleAfterFollowOutcomeError(t *testing.T) {
	m, _ := testTUI(t)
	const run = "agent:11111111-1111-4111-8111-111111111111"
	m.runID, m.sessionID, m.busy = run, "owned-session", true
	m.snapshot.detail = "Durable run " + run + " · cursor 4"
	m.applyRunStream(aiRunStreamOutput{Receipt: &aiRunStreamReceipt{RunID: run, Status: "canceled", NextSequence: 9}})
	// The shared CLI reader emits the terminal receipt before returning the
	// nonzero canceled outcome. The TUI must retain both pieces of information.
	m.applyStream(tuiStreamResult{done: true, err: fmt.Errorf("AI run ended with status canceled")})
	detail, ok := m.snapshot.detail.(string)
	if !ok || !strings.Contains(detail, "Run "+run+" · canceled · cursor 9") ||
		!strings.Contains(detail, "Durable run "+run+" · cursor 4") ||
		!strings.Contains(m.message, "ended with status canceled") || m.busy || m.cursors[run] != 9 {
		t.Fatal("terminal outcome error hid the authoritative canceled run and cursor")
	}
}
