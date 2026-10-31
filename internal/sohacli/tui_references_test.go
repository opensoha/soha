package sohacli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	contractresource "github.com/opensoha/soha-contracts/resource"
)

func TestTUIRecoveryReferencesSurviveUnknownContextSwitchAndQuit(t *testing.T) {
	m, _ := testTUI(t)
	preconditions := contractresource.ResourceMutationPreconditions{ExpectedUID: "original-uid", ExpectedResourceVersion: "opaque-original"}
	plan := &tuiWritePlan{target: "original-target", key: "original-key", scope: m.scope, preconditions: &preconditions, review: map[string]any{"password": "never-copy-this"}, submit: func(context.Context) (any, error) { return nil, fmt.Errorf("response lost") }}
	applyTUICmd(m, m.submitWrite(plan))
	m.pendingContext = &tuiContextTarget{input: tuiContextInput{Profile: "second", Cluster: "second-cluster", Namespace: "other"}, backend: &tuiFake{}, origin: "https://second.example.test", subject: "second-user"}
	m.activateContext()
	if m.writeReference != "" {
		t.Fatal("current view kept an old context reference")
	}
	m.Update(tuiKey("q"))
	var out bytes.Buffer
	if err := m.writeRecoveryReferences(&out); err != nil {
		t.Fatal(err)
	}
	var result struct {
		Source    string
		Reference tuiRequestReference
	}
	if err := json.Unmarshal(out.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	ref := result.Reference
	if result.Source != "tui-original-request" || ref.RequestID != "original-key" || ref.Target != "original-target" || ref.Server != "http://localhost" || ref.Profile != "local" || ref.Subject != "subject" || ref.Preconditions == nil || *ref.Preconditions != preconditions {
		t.Fatalf("original recovery metadata lost: %s", out.String())
	}
	if strings.Contains(out.String(), "never-copy-this") || strings.Contains(out.String(), "password") {
		t.Fatal("raw plan copied to recovery output")
	}
}

func TestTUIRecoveryReferenceLimitBlocksBeforeNewSubmission(t *testing.T) {
	m, _ := testTUI(t)
	m.references = make([]tuiRequestReference, maxTUIReferences)
	plan := &tuiWritePlan{target: "next", key: "next-key", submit: func(context.Context) (any, error) { t.Fatal("unrecorded request executed"); return nil, nil }}
	if cmd := m.submitWrite(plan); cmd != nil || !m.failed || len(m.references) != maxTUIReferences {
		t.Fatal("reference budget silently dropped an original request")
	}
	if cmd := m.startChat(tuiConfirmation{row: tuiRow{id: "session"}, requestID: "turn"}); cmd != nil {
		t.Fatal("unrecorded chat submitted")
	}
}

func TestTUIRecoveryAIReferenceKeepsOriginalIdentityAndRun(t *testing.T) {
	m, _ := testTUI(t)
	m.requestID = "turn-original"
	if !m.rememberReference(tuiRequestReference{Kind: "ai-turn", RequestID: m.requestID, SessionID: "session-original"}) {
		t.Fatal("reference rejected")
	}
	m.rememberRunID("run-original")
	m.origin = "https://new.example.test"
	m.rememberRunID("unrelated-run")
	var out bytes.Buffer
	if err := m.writeRecoveryReferences(&out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "run-original") || strings.Contains(out.String(), "unrelated-run") || !strings.Contains(out.String(), "session-original") {
		t.Fatalf("cross-context run reference: %s", out.String())
	}
}
