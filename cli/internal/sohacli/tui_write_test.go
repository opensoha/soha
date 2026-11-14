package sohacli

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	contractresource "github.com/opensoha/soha/contracts/resource"
)

type tuiWriteFake struct {
	tuiFake
	prepares, submits int
	prepareErr        error
}

func (f *tuiWriteFake) prepareWrite(_ context.Context, scope tuiScope, row *tuiRow, raw string) (*tuiWritePlan, error) {
	f.prepares++
	if f.prepareErr != nil {
		return nil, f.prepareErr
	}
	return &tuiWritePlan{scope: scope, target: "reviewed-target", key: "original-write", review: map[string]any{"input": raw}, submit: func(context.Context) (any, error) { f.submits++; return map[string]any{"taskId": "original-task"}, nil }}, nil
}
func TestTUIWriteRequiresActualPlanAndFullReview(t *testing.T) {
	f := &tuiWriteFake{}
	m := newTUIModel(t.Context(), f, "local", "http://localhost", "user", "cluster", "default")
	t.Cleanup(m.detach)
	m.scope.tab, m.scope.section = 1, "vms"
	m.Update(tuiKey("w"))
	if !m.writeEditing || !m.composing {
		t.Fatal("write editor not opened")
	}
	_, cmd := m.Update(tuiKey("\r"))
	// Use the actual Enter key name rather than an inserted control character.
	if cmd != nil {
		t.Fatal("raw CR must not execute")
	}
	cmd = m.stageWrite()
	if f.prepares != 0 || f.submits != 0 {
		t.Fatal("preflight executed synchronously")
	}
	applyTUICmd(m, cmd)
	if m.confirm == nil || f.prepares != 1 || f.submits != 0 {
		t.Fatal("server review was skipped")
	}
	m.Update(tuiKey("y"))
	if m.confirm == nil || f.submits != 0 {
		t.Fatal("unread plan accepted")
	}
	m.confirm.scroll = len(m.confirmContentLines()) - 1
	m.width = 30
	m.Update(tuiKey("y"))
	if f.submits != 0 {
		t.Fatal("narrow confirmation accepted")
	}
	m.width = 100
	m.scope.cluster = "changed-local-view"
	_, cmd = m.Update(tuiKey("y"))
	applyTUICmd(m, cmd)
	if f.submits != 1 || m.confirm != nil || !strings.Contains(m.writeReference, "original-write") {
		t.Fatal("frozen write not submitted once")
	}
	_, cmd = m.Update(tuiKey("y"))
	applyTUICmd(m, cmd)
	if f.submits != 1 {
		t.Fatal("confirmation replayed")
	}
}

func TestTUIWritePreflightErrorsAndStaleResultsCannotConfirm(t *testing.T) {
	m, _ := testTUI(t)
	m.epoch = 4
	plan := &tuiWritePlan{target: "other", submit: func(context.Context) (any, error) { t.Fatal("stale plan executed"); return nil, nil }}
	m.Update(tuiWriteResult{epoch: 3, plan: plan})
	if m.confirm != nil {
		t.Fatal("stale plan staged")
	}
	m.Update(tuiWriteResult{epoch: 4, err: fmt.Errorf("permission denied")})
	if m.confirm != nil || !m.failed {
		t.Fatal("denied preview accepted")
	}
}

func TestTUIWriteWorkloadFreezesUIDVersionAndAction(t *testing.T) {
	mutations := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer private-test-token" || r.URL.Query().Get("namespace") != "owned" {
			t.Error("wrong identity or namespace")
		}
		if r.Method == "GET" && strings.HasSuffix(r.URL.Path, "/identity") {
			_, _ = fmt.Fprint(w, `{"data":{"apiVersion":"apps/v1","kind":"Deployment","name":"app","namespace":"owned","uid":"uid-original","resourceVersion":"opaque:17"}}`)
			return
		}
		if r.Method != "POST" || !strings.HasSuffix(r.URL.Path, "/resources/Deployment/app/conditional/actions/scale") {
			t.Errorf("unexpected path %s %s", r.Method, r.URL.Path)
			w.WriteHeader(404)
			return
		}
		var input contractresource.ConditionalWorkloadActionInput
		if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
			t.Fatal(err)
		}
		if input.Preconditions.ExpectedUID != "uid-original" || input.Preconditions.ExpectedResourceVersion != "opaque:17" || input.Replicas == nil || *input.Replicas != 2 {
			t.Fatal("reviewed version/action changed")
		}
		if !input.DryRun {
			mutations++
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": contractresource.ConditionalWorkloadActionReceipt{Kind: "Deployment", Name: "app", Namespace: "owned", Action: "scale", Preconditions: input.Preconditions, UID: "uid-original", ResourceVersion: "opaque:17", DryRun: input.DryRun, Applied: !input.DryRun}})
	}))
	defer srv.Close()
	a := tuiAPI{client: APIClient{ServerURL: srv.URL, Token: "private-test-token"}}
	row := tuiRow{id: "app", name: "app", namespace: "owned"}
	scope := tuiScope{cluster: "cluster", namespace: "default", section: "deployments"}
	plan, err := a.prepareWrite(t.Context(), scope, &row, `{"action":"scale","replicas":2}`)
	if err != nil || mutations != 0 {
		t.Fatalf("preflight: %v mutations=%d", err, mutations)
	}
	row.name = "replacement"
	scope.namespace = "other"
	if _, err := plan.submit(t.Context()); err != nil || mutations != 1 {
		t.Fatalf("submit: %v mutations=%d", err, mutations)
	}
}

func TestTUIWriteVMUsesReadyServerPlanAndOriginalKey(t *testing.T) {
	for _, ready := range []bool{false, true} {
		t.Run(fmt.Sprint(ready), func(t *testing.T) {
			mutations := 0
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != "POST" {
					t.Fatal("unexpected method")
				}
				switch r.URL.Path {
				case virtualizationPath + "/vms/plan":
					if r.Header.Get("Idempotency-Key") != "" {
						t.Fatal("preview used execution key")
					}
					_, _ = fmt.Fprintf(w, `{"data":{"ready":%t}}`, ready)
				case virtualizationPath + "/vms":
					mutations++
					if !strings.HasPrefix(r.Header.Get("Idempotency-Key"), "tui-") {
						t.Error("missing original key")
					}
					_, _ = fmt.Fprint(w, `{"data":{"id":"operation-owned","status":"pending"}}`)
				default:
					t.Errorf("unexpected path %s", r.URL.Path)
					w.WriteHeader(404)
				}
			}))
			defer srv.Close()
			a := tuiAPI{client: APIClient{ServerURL: srv.URL}}
			plan, err := a.prepareWrite(t.Context(), tuiScope{tab: 1, section: "vms"}, nil, `{"action":"create","input":{"connectionId":"connection-owned","name":"owned","cpu":1,"memoryMiB":512,"diskGiB":4,"requireCapacity":true}}`)
			if !ready {
				if err == nil || plan != nil || mutations != 0 {
					t.Fatal("unready plan submitted")
				}
				return
			}
			if err != nil || mutations != 0 {
				t.Fatalf("preflight: %v", err)
			}
			if _, err = plan.submit(t.Context()); err != nil || mutations != 1 {
				t.Fatalf("submit: %v", err)
			}
		})
	}
}

func TestTUIWriteRejectsArbitraryRoutingAndSecretEcho(t *testing.T) {
	a := tuiAPI{client: APIClient{ServerURL: "http://127.0.0.1:1"}}
	for _, raw := range []string{`null`, `{"action":"create","input":null}`, `{"action":"create","path":"/other"}`, `{"action":"create","input":{"password":"secret-marker","unknown":true}}`, `{"action":"confirm-plan","planId":"../other"}`} {
		_, err := a.prepareWrite(t.Context(), tuiScope{tab: 1, section: "vms"}, nil, raw)
		if err == nil || strings.Contains(err.Error(), "secret-marker") {
			t.Fatal("invalid input accepted or echoed")
		}
	}
	if _, err := a.prepareWrite(t.Context(), tuiScope{tab: 1, section: "hosts"}, nil, `{"action":"create","input":null}`); err == nil || !strings.Contains(err.Error(), "name is required") {
		t.Fatalf("null host input should remain an empty typed struct, not call HTTP or panic: %v", err)
	}
}

func TestTUIWriteDeliveryReadsAndConfirmsOnlyOriginalPlan(t *testing.T) {
	writes := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "GET" && r.URL.Path == "/api/v1/delivery/plans/original" {
			_, _ = fmt.Fprint(w, `{"data":{"id":"original","applicationId":"app","applicationEnvironmentId":"env","action":"deploy","status":"waiting_approval","requiresApproval":true}}`)
			return
		}
		if r.Method != "POST" || r.URL.Path != "/api/v1/delivery/plans/original/confirm" || r.ContentLength > 0 {
			t.Errorf("unexpected confirm %s %s", r.Method, r.URL.Path)
			w.WriteHeader(400)
			return
		}
		writes++
		// The actual service may continue to require independent approval.
		_, _ = fmt.Fprint(w, `{"data":{"plan":{"id":"original","status":"waiting_approval"},"result":{}}}`)
	}))
	defer srv.Close()
	a := tuiAPI{client: APIClient{ServerURL: srv.URL}}
	plan, err := a.prepareWrite(t.Context(), tuiScope{tab: 2}, nil, `{"action":"confirm-plan","planId":"original"}`)
	if err != nil || writes != 0 {
		t.Fatalf("preview: %v", err)
	}
	if _, err := plan.submit(t.Context()); err != nil || writes != 1 {
		t.Fatalf("confirm: %v writes=%d", err, writes)
	}
}

func TestTUIWriteUnknownOutcomeKeepsInspectableOriginalTarget(t *testing.T) {
	m, _ := testTUI(t)
	plan := &tuiWritePlan{target: strings.Repeat("target", 50), key: "original-uncertain-key", review: map[string]any{"ready": true}, submit: func(context.Context) (any, error) { return nil, fmt.Errorf("connection lost") }}
	applyTUICmd(m, m.submitWrite(plan))
	if !m.failed || !strings.Contains(m.message, "unknown") || !strings.Contains(fmt.Sprint(m.snapshot.detail), plan.key) || !strings.Contains(fmt.Sprint(m.snapshot.detail), plan.target) {
		t.Fatal("uncertain original reference lost")
	}
}
