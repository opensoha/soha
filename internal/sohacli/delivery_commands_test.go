package sohacli

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDeliveryCommandsUseBatchAndFinalApprovalEndpoints(t *testing.T) {
	definition := `{"name":"release","stopOnFailure":false,"targets":[{"id":"web","applicationId":"app","serviceId":"web","action":"build"}]}`
	source := `{"name":"Templates","repositoryId":"repo","refType":"branch","refValue":"main","path":".","kinds":["BuildTemplate"],"enabled":true,"expectedGeneration":0}`
	trigger := `{"name":"Daily","targetKind":"workflow","targetId":"flow","workflowVersion":2,"type":"schedule","enabled":true,"expectedRevision":0,"serviceAccountToken":"fixture-token","schedule":{"timeZone":"Asia/Shanghai","cron":"0 9 * * *"}}`
	apply := `{"expectedGeneration":3,"candidateDigest":"sha256:` + strings.Repeat("a", 64) + `","idempotencyKey":"apply-attempt-1"}`
	for _, tc := range []struct {
		args                         []string
		method, path, body, response string
	}{
		{[]string{"triggers", "list", "--target-kind", "workflow", "--target-id", "flow"}, "GET", "/api/v1/delivery/triggers?limit=50&offset=0&targetId=flow&targetKind=workflow", "", `{"data":[]}`},
		{[]string{"triggers", "get", "trigger:1"}, "GET", "/api/v1/delivery/triggers/trigger:1", "", `{"data":{"id":"trigger:1","revision":1}}`},
		{[]string{"triggers", "create", "--input", "-", "--yes"}, "POST", "/api/v1/delivery/triggers", trigger, `{"data":{"id":"trigger:1","revision":1}}`},
		{[]string{"triggers", "update", "trigger:1", "--input", "-", "--yes"}, "PUT", "/api/v1/delivery/triggers/trigger:1", strings.Replace(trigger, `"expectedRevision":0`, `"expectedRevision":1`, 1), `{"data":{"id":"trigger:1","revision":2}}`},
		{[]string{"triggers", "events", "trigger:1", "--limit", "10", "--offset", "50"}, "GET", "/api/v1/delivery/triggers/trigger:1/events?limit=10&offset=50", "", `{"data":[{"id":"event:1","status":"succeeded","batchId":"batch:1"}]}`},
		{[]string{"template-sources", "list", "--offset", "50", "--limit", "10"}, "GET", "/api/v1/delivery/template-sources?limit=10&offset=50", "", `{"data":[]}`},
		{[]string{"template-sources", "get", "source:1"}, "GET", "/api/v1/delivery/template-sources/source:1", "", `{"data":{"id":"source:1","generation":3}}`},
		{[]string{"template-sources", "create", "--input", "-", "--yes"}, "POST", "/api/v1/delivery/template-sources", source, `{"data":{"id":"source:1","generation":1}}`},
		{[]string{"template-sources", "update", "source:1", "--input", "-", "--yes"}, "PUT", "/api/v1/delivery/template-sources/source:1", strings.Replace(source, `"expectedGeneration":0`, `"expectedGeneration":3`, 1), `{"data":{"id":"source:1","generation":4}}`},
		{[]string{"template-sources", "objects", "source:1"}, "GET", "/api/v1/delivery/template-sources/source:1/objects?limit=50&offset=0", "", `{"data":[]}`},
		{[]string{"template-sources", "runs", "source:1"}, "GET", "/api/v1/delivery/template-sources/source:1/sync-runs?limit=50&offset=0", "", `{"data":[]}`},
		{[]string{"template-sources", "run", "source:1", "--run-id", "run:1"}, "GET", "/api/v1/delivery/template-sources/source:1/sync-runs/run:1", "", `{"data":{"id":"run:1","status":"failed"}}`},
		{[]string{"template-sources", "sync", "source:1", "--input", "-", "--yes"}, "POST", "/api/v1/delivery/template-sources/source:1/sync", `{"expectedGeneration":3,"idempotencyKey":"read-attempt-1"}`, `{"data":{"id":"run:1","status":"ready"}}`},
		{[]string{"template-sources", "apply", "source:1", "--run-id", "run:1", "--input", "-", "--yes"}, "POST", "/api/v1/delivery/template-sources/source:1/sync-runs/run:1/apply", apply, `{"data":{"id":"run:1","status":"applied"}}`},
		{[]string{"template-sources", "detach", "source:1", "--kind", "BuildTemplate", "--object-id", "template:1", "--input", "-", "--yes"}, "POST", "/api/v1/delivery/template-sources/source:1/objects/BuildTemplate/template:1/detach", `{"expectedGeneration":3,"disposition":"keep"}`, ""},
		{[]string{"template-sources", "remove", "source:1", "--input", "-", "--yes"}, "DELETE", "/api/v1/delivery/template-sources/source:1", `{"expectedGeneration":3,"disposition":"deprecate"}`, ""},
		{[]string{"documents", "source", "BuildTemplate", "template:1", "--version", "2"}, "GET", "/api/v1/delivery/documents/BuildTemplate/template:1/source?version=2", "", `{"data":{}}`},
		{[]string{"batches", "list", "--application-id", "app", "--service-id", "web", "--limit", "10"}, "GET", "/api/v1/delivery-batches?applicationId=app&limit=10&serviceId=web", "", `{"data":[]}`},
		{[]string{"batches", "get", "batch:1"}, "GET", "/api/v1/delivery-batches/batch:1", "", `{"data":{"id":"batch:1","partialView":true,"status":"failed"}}`},
		{[]string{"batches", "create", "--input", "-", "--yes"}, "POST", "/api/v1/delivery-batches", `{"idempotencyKey":"same-attempt","definition":` + definition + `}`, `{"data":{"id":"batch:1","definition":` + definition + `,"status":"queued"}}`},
		{[]string{"batches", "cancel", "batch:1", "--reason", "paused", "--yes"}, "POST", "/api/v1/delivery-batches/batch:1/cancel", `{"reason":"paused"}`, `{"data":{"id":"batch:1","status":"canceling"}}`},
		{[]string{"workflows", "list"}, "GET", "/api/v1/delivery-workflows", "", `{"data":[]}`},
		{[]string{"workflows", "get", "flow:1"}, "GET", "/api/v1/delivery-workflows/flow:1", "", `{"data":{"id":"flow:1","version":1}}`},
		{[]string{"workflows", "create", "--input", "-", "--yes"}, "POST", "/api/v1/delivery-workflows", `{"definition":` + definition + `}`, `{"data":{"id":"flow:1","version":1}}`},
		{[]string{"workflows", "update", "flow:1", "--input", "-", "--yes"}, "PUT", "/api/v1/delivery-workflows/flow:1", `{"expectedVersion":1,"definition":` + definition + `}`, `{"data":{"id":"flow:1","version":2}}`},
		{[]string{"plans", "get", "plan:1"}, "GET", "/api/v1/delivery/plans/plan:1", "", `{"data":{"id":"plan:1","status":"waiting_approval","requiresApproval":true}}`},
		{[]string{"plans", "approve", "plan:1", "--comment", "reviewed", "--yes"}, "POST", "/api/v1/delivery/plans/plan:1/approval", `{"action":"approve","comment":"reviewed"}`, `{"data":{"id":"plan:1","status":"draft"}}`},
		{[]string{"plans", "reject", "plan:1", "--yes"}, "POST", "/api/v1/delivery/plans/plan:1/approval", `{"action":"reject"}`, `{"data":{"id":"plan:1","status":"draft"}}`},
	} {
		t.Run(strings.Join(tc.args[:2], "/"), func(t *testing.T) {
			requests := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests++
				if r.Method != tc.method || r.URL.RequestURI() != tc.path || r.Header.Get("Authorization") == "" {
					t.Errorf("unexpected request: %s %s", r.Method, r.URL.RequestURI())
				}
				if tc.body != "" {
					var actual, expected map[string]any
					if err := json.NewDecoder(r.Body).Decode(&actual); err != nil {
						t.Error(err)
					}
					if err := json.Unmarshal([]byte(tc.body), &expected); err != nil {
						t.Fatal(err)
					}
					a, _ := json.Marshal(actual)
					b, _ := json.Marshal(expected)
					if string(a) != string(b) {
						t.Errorf("request body = %s, want %s", a, b)
					}
				}
				w.Header().Set("Content-Type", "application/json")
				if tc.response == "" {
					w.WriteHeader(http.StatusNoContent)
					return
				}
				_, _ = w.Write([]byte(tc.response))
			}))
			defer server.Close()
			var out, errs bytes.Buffer
			args := append(append([]string{"delivery"}, tc.args...), "--profile", "dev")
			code := Run(context.Background(), args, Runtime{In: strings.NewReader(tc.body), Out: &out, Err: &errs, ConfigPath: writeTestConfig(t, server.URL)})
			if code != 0 || requests != 1 {
				t.Fatalf("exit=%d requests=%d: %s", code, requests, errs.String())
			}
			if !json.Valid(out.Bytes()) {
				t.Fatalf("invalid output: %s", out.String())
			}
			if strings.Contains(out.String()+errs.String(), "fixture-token") {
				t.Fatal("input credential leaked to output")
			}
			if tc.args[0] == "batches" && tc.args[1] == "create" && !strings.Contains(out.String(), `"stopOnFailure": false`) {
				t.Fatalf("explicit false lost in output: %s", out.String())
			}
		})
	}
}

func TestDeliveryRejectsInvalidRequestsBeforeSending(t *testing.T) {
	for _, args := range [][]string{
		{"triggers", "list"},
		{"triggers", "events", "trigger", "--limit", "201"},
		{"triggers", "update", "trigger", "--input-json", `{"expectedRevision":0}`, "--yes"},
		{"triggers", "create", "--input-json", `{"expectedRevision":0,"executionTokenId":"bad"}`, "--yes"},
		{"triggers", "create", "--input-json", `{"expectedRevision":0,"schedule":{"timeZone":"UTC","cron":"* * * * *","unknown":true}}`, "--yes"},
		{"template-sources", "list", "--offset", "-1"},
		{"template-sources", "list", "--limit", "201"},
		{"template-sources", "run", "source", "--run-id", "../run"},
		{"template-sources", "detach", "source", "--kind", "Unknown", "--object-id", "tpl", "--input-json", `{"expectedGeneration":1,"disposition":"keep"}`, "--yes"},
		{"template-sources", "sync", "source", "--input-json", `{"expectedGeneration":0,"idempotencyKey":"same-attempt"}`, "--yes"},
		{"template-sources", "sync", "source", "--input-json", `{"expectedGeneration":1,"idempotencyKey":"same-attempt","credentialRef":"token"}`, "--yes"},
		{"template-sources", "remove", "source", "--input-json", `{"expectedGeneration":1}`, "--yes"},
		{"template-sources", "apply", "source", "--run-id", "run", "--input-json", `{"expectedGeneration":1,"idempotencyKey":"same-attempt","candidateDigest":"bad"}`, "--yes"},
		{"template-sources", "create", "--input-json", `{"expectedGeneration":1}`, "--yes"},
		{"documents", "source", "Workflow", "flow", "--version", "1"},
		{"batches", "create", "--input-json", `{"definition":{}}`, "--yes"},
		{"batches", "create", "--input-json", `{"idempotencyKey":"key","definition":{},"workflowId":"flow"}`, "--yes"},
		{"batches", "create", "--input-json", `{"idempotencyKey":"key","workflowId":"flow"}`, "--yes"},
		{"batches", "create", "--input-json", `{`, "--yes"},
		{"workflows", "update", "flow", "--input-json", `{"definition":{}}`, "--yes"},
		{"plans", "confirm", "plan", "--input-json", `{}`, "--yes"},
		{"batches", "get", "../plans"},
		{"batches", "list", "--limit", "201"},
		{"batches", "cancel", "batch"},
	} {
		var out, errs bytes.Buffer
		code := Run(context.Background(), append([]string{"delivery"}, args...), Runtime{In: strings.NewReader("n\n"), Out: &out, Err: &errs, ConfigPath: t.TempDir() + "/config.json"})
		if code == 0 || out.Len() != 0 {
			t.Fatalf("unexpected success %v: %s %s", args, out.String(), errs.String())
		}
	}
}

func TestDeliveryTemplateSyncRetryPreservesRequestAndFailedRun(t *testing.T) {
	body := `{"expectedGeneration":3,"idempotencyKey":"read-attempt-1"}`
	var requests []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var input json.RawMessage
		if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
			t.Error(err)
		}
		requests = append(requests, string(input))
		w.Header().Set("Content-Type", "application/json")
		if len(requests) == 1 {
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		_, _ = w.Write([]byte(`{"data":{"id":"run-1","status":"invalid","errorCode":"invalid_documents","preview":{"valid":false,"diagnostics":[{"code":"unknown_field"}]}}}`))
	}))
	defer server.Close()
	config := writeTestConfig(t, server.URL)
	for attempt := 0; attempt < 2; attempt++ {
		var out, errs bytes.Buffer
		code := Run(context.Background(), []string{"delivery", "template-sources", "sync", "source-1", "--input-json", body, "--yes", "--profile", "dev"}, Runtime{In: strings.NewReader(""), Out: &out, Err: &errs, ConfigPath: config})
		if code == 0 {
			t.Fatal("failed read must return a nonzero exit code")
		}
		if attempt == 1 && (!strings.Contains(out.String(), `"id": "run-1"`) || !strings.Contains(out.String(), "unknown_field")) {
			t.Fatalf("failed run diagnostics lost: %s %s", out.String(), errs.String())
		}
	}
	if len(requests) != 2 || requests[0] != requests[1] {
		t.Fatalf("retry changed request: %v", requests)
	}
}
