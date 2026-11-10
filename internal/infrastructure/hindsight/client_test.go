package hindsight

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	app "github.com/opensoha/soha/internal/application/memory"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestClientRawProtocol(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer service-secret" {
			t.Error("missing service identity")
		}
		switch {
		case r.Method == "PUT":
			t.Error("retain must create the bank inside its transaction")
			w.WriteHeader(http.StatusInternalServerError)
		case strings.HasSuffix(r.URL.Path, "/operations/op"):
			w.Write([]byte(`{"operation_id":"op","operation_type":"retain","status":"completed"}`))
		case strings.HasSuffix(r.URL.Path, "/recall"):
			w.Write([]byte(`{"results":[{"type":"world","text":"provider generated text","document_id":"doc","metadata":{"source_revision":"2"}},{"type":"observation","document_id":"doc","metadata":{"source_revision":"2"}},{"type":"world","document_id":"missing-source"}]}`))
		case r.Method == "DELETE":
			w.Write([]byte(`{"success":true,"document_id":"doc"}`))
		default:
			var p map[string]any
			if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
				t.Fatal(err)
			}
			if p["operation_id"] != "op" || p["async"] != true {
				t.Error("retain is not asynchronous/idempotent")
			}
			w.Write([]byte(`{"success":true,"async":true,"operation_id":"op","bank_id":"bank"}`))
		}
	}))
	defer server.Close()
	c, err := NewClient(Config{BaseURL: server.URL, Token: "service-secret"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := c.Retain(ctx, "bank", app.SyncJob{ID: "op", DocumentID: "doc", Revision: 2, Record: app.Record{Fact: "trusted"}}); err != nil {
		t.Fatal(err)
	}
	if state, err := c.Operation(ctx, "bank", "op"); err != nil || state != "completed" {
		t.Fatalf("%s %v", state, err)
	}
	if err := c.DeleteDocument(ctx, "bank", "doc", ""); err != nil {
		t.Fatal(err)
	}
	got, err := c.Recall(ctx, "bank", "query")
	if err != nil || len(got) != 1 || got[0].Revision != 2 {
		t.Fatalf("%v %v", got, err)
	}
}
func TestClientFailures(t *testing.T) {
	for _, tc := range []struct {
		name string
		code int
		body string
	}{{"unauthorized", 401, `secret-leak`}, {"upstream", 503, `secret-leak`}, {"malformed", 200, `oops`}, {"missing results", 200, `{}`}, {"not found", 404, `{}`}} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(tc.code); w.Write([]byte(tc.body)) }))
			defer server.Close()
			c, _ := NewClient(Config{BaseURL: server.URL, Token: "secret"}, nil)
			_, err := c.Operation(context.Background(), "bank", "op")
			if err == nil || strings.Contains(err.Error(), "secret-leak") {
				t.Fatalf("unsafe failure %v", err)
			}
			if tc.code == 404 && !errors.Is(err, app.ErrBackendNotFound) {
				t.Fatal(err)
			}
			_, err = c.Recall(context.Background(), "bank", "q")
			if tc.code != 404 && err == nil {
				t.Fatal("accepted invalid recall")
			}
		})
	}
	if _, err := NewClient(Config{BaseURL: "http://localhost"}, nil); err == nil {
		t.Fatal("accepted absent token")
	}
}

func TestClientRetainOperationQualification(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(map[string]any)
		want   string
		bad    bool
	}{
		{name: "batch completed", want: "completed"},
		{name: "batch pending", change: func(v map[string]any) {
			v["status"] = "pending"
			delete(v, "child_operations")
			delete(v["result_metadata"].(map[string]any), "extraction_errors_count")
		}, want: "pending"},
		{name: "batch processing", change: func(v map[string]any) { v["status"] = "processing" }, want: "processing"},
		{name: "batch failed", change: func(v map[string]any) { v["status"] = "failed" }, want: "failed"},
		{name: "batch cancelled", change: func(v map[string]any) { v["status"] = "cancelled" }, want: "failed"},
		{name: "legacy retain", change: func(v map[string]any) {
			v["operation_type"] = "retain"
			delete(v, "child_operations")
			delete(v, "result_metadata")
		}, want: "completed"},
		{name: "legacy extraction errors", change: func(v map[string]any) {
			v["operation_type"] = "retain"
			v["result_metadata"].(map[string]any)["extraction_errors_count"] = 1
		}, want: "failed"},
		{name: "partial extraction errors", change: func(v map[string]any) { v["result_metadata"].(map[string]any)["extraction_errors_count"] = 1 }, want: "failed"},
		{name: "pending partial errors", change: func(v map[string]any) {
			v["status"] = "pending"
			v["result_metadata"].(map[string]any)["extraction_errors_count"] = 1
		}, want: "pending"},
		{name: "child failed", change: func(v map[string]any) { v["child_operations"].([]any)[0].(map[string]any)["status"] = "failed" }, bad: true},
		{name: "child cancelled", change: func(v map[string]any) { v["child_operations"].([]any)[0].(map[string]any)["status"] = "cancelled" }, bad: true},
		{name: "child error message", change: func(v map[string]any) {
			v["child_operations"].([]any)[0].(map[string]any)["error_message"] = "sensitive-child-error"
		}, want: "failed"},
		{name: "parent error message", change: func(v map[string]any) { v["error_message"] = "sensitive-parent-error" }, want: "failed"},
		{name: "child pending under completed", change: func(v map[string]any) { v["child_operations"].([]any)[0].(map[string]any)["status"] = "pending" }, bad: true},
		{name: "missing outcome count", change: func(v map[string]any) { delete(v["result_metadata"].(map[string]any), "extraction_errors_count") }, bad: true},
		{name: "negative outcome count", change: func(v map[string]any) { v["result_metadata"].(map[string]any)["extraction_errors_count"] = -1 }, bad: true},
		{name: "malformed outcome count", change: func(v map[string]any) {
			v["result_metadata"].(map[string]any)["extraction_errors_count"] = "sensitive-invalid"
		}, bad: true},
		{name: "missing children", change: func(v map[string]any) { delete(v, "child_operations") }, bad: true},
		{name: "missing child identity", change: func(v map[string]any) { delete(v["child_operations"].([]any)[0].(map[string]any), "operation_id") }, bad: true},
		{name: "child uses parent identity", change: func(v map[string]any) { v["child_operations"].([]any)[0].(map[string]any)["operation_id"] = "op" }, bad: true},
		{name: "duplicate child identity", change: func(v map[string]any) {
			child := v["child_operations"].([]any)[0]
			v["child_operations"] = []any{child, child}
		}, bad: true},
		{name: "missing parent metadata", change: func(v map[string]any) { delete(v, "result_metadata") }, bad: true},
		{name: "child masquerading as parent", change: func(v map[string]any) { v["result_metadata"].(map[string]any)["is_parent"] = false }, bad: true},
		{name: "multiple submitted items", change: func(v map[string]any) { v["result_metadata"].(map[string]any)["items_count"] = 2 }, bad: true},
		{name: "wrong operation", change: func(v map[string]any) { v["operation_id"] = "other" }, bad: true},
		{name: "wrong explicit bank", change: func(v map[string]any) { v["bank_id"] = "other" }, bad: true},
		{name: "matching explicit bank", change: func(v map[string]any) { v["bank_id"] = "bank" }, want: "completed"},
		{name: "not found", change: func(v map[string]any) { v["status"] = "not_found"; v["operation_type"] = nil }, bad: true},
		{name: "unknown state", change: func(v map[string]any) { v["status"] = "unknown" }, bad: true},
		{name: "reflect", change: func(v map[string]any) { v["operation_type"] = "reflect" }, bad: true},
		{name: "consolidation", change: func(v map[string]any) { v["operation_type"] = "consolidation" }, bad: true},
		{name: "delete", change: func(v map[string]any) { v["operation_type"] = "delete" }, bad: true},
		{name: "file conversion", change: func(v map[string]any) { v["operation_type"] = "file_convert_retain" }, bad: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			response := map[string]any{"operation_id": "op", "operation_type": "batch_retain", "status": "completed", "error_message": nil, "result_metadata": map[string]any{"is_parent": true, "items_count": 1, "extraction_errors_count": 0}, "child_operations": []any{map[string]any{"operation_id": "child", "status": "completed", "error_message": nil}}}
			if tc.change != nil {
				tc.change(response)
			}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != "GET" || r.URL.Path != "/v1/default/banks/bank/operations/op" {
					t.Error("operation not bound to requested bank and id")
				}
				if r.Header.Get("Authorization") != "Bearer service-secret" {
					t.Error("missing service identity")
				}
				_ = json.NewEncoder(w).Encode(response)
			}))
			defer server.Close()
			client, err := NewClient(Config{BaseURL: server.URL, Token: "service-secret"}, nil)
			if err != nil {
				t.Fatal(err)
			}
			state, err := client.Operation(context.Background(), "bank", "op")
			if tc.bad {
				if err == nil || state != "" {
					t.Fatalf("invalid operation accepted: state=%s error=%v", state, err)
				}
			} else if err != nil || state != tc.want {
				t.Fatalf("state=%s want=%s error=%v", state, tc.want, err)
			}
			if tc.name == "not found" && !errors.Is(err, app.ErrBackendNotFound) {
				t.Fatalf("not-found classification lost: %v", err)
			}
			if err != nil && strings.Contains(err.Error(), "sensitive") {
				t.Fatal("upstream error data leaked")
			}
		})
	}
}

// One dependent delete is eligible only after its retain outbox row is done or
// failed, matching repository/memory. No Core DB or external service is used.
type settlementSyncStore struct {
	job   app.SyncJob
	state string
}

func (s *settlementSyncStore) EnsureBank(context.Context, app.Record, string) error { return nil }
func (s *settlementSyncStore) ClaimSync(context.Context) (app.SyncJob, error) {
	j := s.job
	if s.state == "done" || s.state == "failed" {
		j.Kind = "delete"
	}
	return j, nil
}
func (s *settlementSyncStore) MarkSubmitting(context.Context, app.SyncJob) error {
	return fmt.Errorf("unexpected retain replay")
}
func (s *settlementSyncStore) FinishSync(_ context.Context, _ app.SyncJob, state string) error {
	s.state = state
	return nil
}
func (s *settlementSyncStore) InvalidateSync(context.Context, app.Record, bool) error { return nil }
func (s *settlementSyncStore) ResolveDocument(context.Context, string) (app.Record, error) {
	return app.Record{}, app.ErrNotFound
}

type settlementSource struct{}

func (settlementSource) ValidateSource(context.Context, app.Record) error { return nil }

func TestRetainSettlementBlocksDependentDelete(t *testing.T) {
	for _, parent := range []string{"failed", "cancelled", "completed"} {
		for _, child := range []string{"pending", "processing", "failed", "cancelled", "missing", "duplicate"} {
			t.Run(parent+"/"+child, func(t *testing.T) {
				children := []any{map[string]any{"operation_id": "child", "status": child}}
				if child == "missing" {
					children = nil
				}
				if child == "duplicate" {
					children = []any{map[string]any{"operation_id": "child", "status": "completed"}, map[string]any{"operation_id": "child", "status": "completed"}}
				}
				response := map[string]any{"operation_id": "op", "operation_type": "batch_retain", "status": parent, "error_message": "sensitive-parent-error", "result_metadata": map[string]any{"is_parent": true, "items_count": 1, "extraction_errors_count": 1}, "child_operations": children}
				deletes, retains := 0, 0
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					switch r.Method {
					case http.MethodGet:
						_ = json.NewEncoder(w).Encode(response)
					case http.MethodDelete:
						deletes++
						_, _ = w.Write([]byte(`{"success":true,"document_id":"doc"}`))
					default:
						retains++
						t.Error("unknown operation replayed")
					}
				}))
				defer server.Close()
				client, err := NewClient(Config{BaseURL: server.URL, Token: "synthetic-service"}, nil)
				if err != nil {
					t.Fatal(err)
				}
				syncStore := &settlementSyncStore{job: app.SyncJob{ID: "op", Kind: "retain", Attempted: true, DocumentID: "doc", Record: app.Record{OwnerType: "user", OwnerID: "owner", ScopeHash: "sha256:scope"}}}
				backend, err := app.NewBackendService(app.NewMemoryStore(), syncStore, client, settlementSource{}, "instance")
				if err != nil {
					t.Fatal(err)
				}
				for i := 0; i < 2; i++ {
					worked, err := backend.SyncOne(context.Background())
					if !worked || !errors.Is(err, app.ErrSyncUnknown) || syncStore.state != "unknown" || deletes != 0 || retains != 0 {
						t.Fatalf("unsafe delete/replay: state=%s deletes=%d retains=%d error=%v", syncStore.state, deletes, retains, err)
					}
				}
				// An explicit completed-child barrier now proves the vendor writer returned;
				// parent failures and partial extraction still remain failed, never success.
				response["child_operations"] = []any{map[string]any{"operation_id": "child", "status": "completed"}}
				if _, err = backend.SyncOne(context.Background()); err == nil || errors.Is(err, app.ErrSyncUnknown) || syncStore.state != "failed" {
					t.Fatalf("completed child did not settle failed parent: %s %v", syncStore.state, err)
				}
				if _, err = backend.SyncOne(context.Background()); err != nil || deletes != 1 || retains != 0 {
					t.Fatalf("safe dependent delete: deletes=%d retains=%d error=%v", deletes, retains, err)
				}
			})
		}
	}
}

func TestLegacyRetainFailureNeedsStopEvidence(t *testing.T) {
	for _, status := range []string{"failed", "cancelled"} {
		t.Run(status, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_ = json.NewEncoder(w).Encode(map[string]any{"operation_id": "op", "operation_type": "retain", "status": status})
			}))
			defer server.Close()
			client, _ := NewClient(Config{BaseURL: server.URL, Token: "synthetic-service"}, nil)
			got, err := client.Operation(context.Background(), "bank", "op")
			if got != "" || !errors.Is(err, app.ErrSyncUnknown) {
				t.Fatalf("legacy stop not confirmed: state=%s error=%v", got, err)
			}
		})
	}
}

func TestDeleteDocumentRequiresOperationPurgeReceipt(t *testing.T) {
	for _, tc := range []struct {
		name      string
		status    int
		proof     string
		wantError bool
	}{
		{"success", 200, "op", false},
		{"missing_document_with_cleanup", 404, "op", false},
		{"old_gateway_success", 200, "", true},
		{"old_gateway_missing_document", 404, "", true},
		{"wrong_operation", 200, "other", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodDelete || r.Header.Get("X-Soha-Retain-Operation-ID") != "op" {
					t.Error("delete did not identify the settled retain")
				}
				if tc.proof != "" {
					w.Header().Set("X-Soha-Retain-Operation-Purged", tc.proof)
				}
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(`{"success":true,"document_id":"doc"}`))
			}))
			defer server.Close()
			client, err := NewClient(Config{BaseURL: server.URL, Token: "synthetic-service"}, nil)
			if err != nil {
				t.Fatal(err)
			}
			err = client.DeleteDocument(t.Context(), "bank", "doc", "op")
			if (err != nil) != tc.wantError {
				t.Fatalf("expected failure=%t, got %v", tc.wantError, err)
			}
		})
	}
}
