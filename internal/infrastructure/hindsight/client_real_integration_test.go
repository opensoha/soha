package hindsight_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	app "github.com/opensoha/soha/internal/application/memory"
	hindsight "github.com/opensoha/soha/internal/infrastructure/hindsight"
)

// This deliberately bounded fixture models one Core outbox item, not PostgreSQL
// transactions or real Soha authorization. It never retries a Retain submission.
type realSyncStore struct {
	store                                  *app.MemoryStore
	owner, scope, document, recordID, bank string
	job                                    app.SyncJob
	state                                  string
	marked                                 bool
}

func (s *realSyncStore) EnsureBank(_ context.Context, r app.Record, bank string) error {
	if r.OwnerType != "user" || r.OwnerID != s.owner || r.ScopeHash != s.scope || !regexp.MustCompile(`^soha-[a-f0-9]{64}$`).MatchString(bank) {
		return errors.New("unexpected server bank scope")
	}
	if s.bank != "" && s.bank != bank {
		return errors.New("bank identity changed")
	}
	s.bank = bank
	return nil
}
func (s *realSyncStore) ClaimSync(context.Context) (app.SyncJob, error) {
	if s.state == "done" || s.state == "failed" {
		return app.SyncJob{}, app.ErrNotFound
	}
	return s.job, nil
}
func (s *realSyncStore) MarkSubmitting(_ context.Context, j app.SyncJob) error {
	if j.ID != s.job.ID || s.marked {
		return errors.New("duplicate or foreign retain submission")
	}
	s.marked = true
	s.job.Attempted = true
	return nil
}
func (s *realSyncStore) FinishSync(_ context.Context, j app.SyncJob, state string) error {
	if j.ID != s.job.ID {
		return errors.New("foreign sync completion")
	}
	s.state = state
	return nil
}
func (s *realSyncStore) InvalidateSync(context.Context, app.Record, bool) error { return nil }
func (s *realSyncStore) ResolveDocument(ctx context.Context, document string) (app.Record, error) {
	if document != s.document {
		return app.Record{}, app.ErrNotFound
	}
	return s.store.GetRecord(ctx, s.recordID)
}

type realSourceChecker struct {
	allowed     bool
	record      app.Record
	validations int
}

func (v *realSourceChecker) ValidateSource(_ context.Context, r app.Record) error {
	v.validations++
	if !v.allowed || r.ID != v.record.ID || r.Revision != v.record.Revision || r.OwnerID != v.record.OwnerID || r.ScopeHash != v.record.ScopeHash || len(r.SourceRefs) != 1 || r.SourceRefs[0] != v.record.SourceRefs[0] || r.SourceHashes[r.SourceRefs[0]] != v.record.SourceHashes[r.SourceRefs[0]] {
		return fmt.Errorf("%w: synthetic source revision/access rejected", apperrors.ErrAccessDenied)
	}
	return nil
}

type realBackendCounters struct {
	app.Backend
	retains, operations, recalls, deletes int
}

func (b *realBackendCounters) Retain(ctx context.Context, bank string, j app.SyncJob) error {
	b.retains++
	if b.retains > 1 {
		return errors.New("retain budget exceeded; replay forbidden")
	}
	return b.Backend.Retain(ctx, bank, j)
}
func (b *realBackendCounters) Operation(ctx context.Context, bank, id string) (string, error) {
	b.operations++
	return b.Backend.Operation(ctx, bank, id)
}
func (b *realBackendCounters) Recall(ctx context.Context, bank, query string) ([]app.Candidate, error) {
	b.recalls++
	if b.recalls > 4 {
		return nil, errors.New("recall request budget exceeded")
	}
	return b.Backend.Recall(ctx, bank, query)
}
func (b *realBackendCounters) DeleteDocument(ctx context.Context, bank, document, operation string) error {
	b.deletes++
	return b.Backend.DeleteDocument(ctx, bank, document, operation)
}

func realSourceHashes(t *testing.T) map[string]string {
	t.Helper()
	out := map[string]string{}
	for _, p := range []string{"client.go", "../../application/memory/backend.go", "../../application/memory/memory_store.go", "client_real_integration_test.go"} {
		data, e := os.ReadFile(filepath.Clean(p))
		if e != nil {
			t.Fatal(e)
		}
		sum := sha256.Sum256(data)
		out[p] = hex.EncodeToString(sum[:])
	}
	return out
}

func TestClientRealHindsightLifecycle(t *testing.T) {
	if os.Getenv("SOHA_TEST_HINDSIGHT_REAL") != "1" {
		t.Skip("opt-in real private Hindsight integration")
	}
	endpoint, token := os.Getenv("SOHA_TEST_HINDSIGHT_URL"), os.Getenv("SOHA_TEST_HINDSIGHT_TOKEN")
	parsed, err := url.Parse(endpoint)
	if err != nil || parsed == nil || parsed.Scheme != "http" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || strings.TrimSpace(token) == "" {
		t.Fatal("explicit private loopback API URL/token required")
	}
	ip := net.ParseIP(parsed.Hostname())
	if parsed.Hostname() != "localhost" && (ip == nil || !ip.IsLoopback()) {
		t.Fatal("refusing non-loopback Hindsight test endpoint")
	}
	cleanupEndpoint := os.Getenv("SOHA_TEST_HINDSIGHT_CLEANUP_URL")
	if cleanupEndpoint == "" {
		cleanupEndpoint = endpoint
	}
	cleanupURL, err := url.Parse(cleanupEndpoint)
	if err != nil || cleanupURL == nil || cleanupURL.Scheme != "http" || cleanupURL.User != nil || cleanupURL.RawQuery != "" || cleanupURL.Fragment != "" {
		t.Fatal("private loopback cleanup URL required")
	}
	cleanupIP := net.ParseIP(cleanupURL.Hostname())
	if cleanupURL.Hostname() != "localhost" && (cleanupIP == nil || !cleanupIP.IsLoopback()) {
		t.Fatal("refusing non-loopback bank cleanup endpoint")
	}
	t.Logf("H2 endpoint identity: apiOrigin=%s://%s cleanupOrigin=%s://%s", parsed.Scheme, parsed.Host, cleanupURL.Scheme, cleanupURL.Host)
	ctx, cancel := context.WithTimeout(context.Background(), 115*time.Second)
	defer cancel()
	transport := &http.Transport{DialContext: (&net.Dialer{Timeout: 5 * time.Second}).DialContext, ResponseHeaderTimeout: 30 * time.Second}
	defer transport.CloseIdleConnections()
	httpClient := &http.Client{Transport: transport, Timeout: 30 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	client, err := hindsight.NewClient(hindsight.Config{BaseURL: endpoint, Token: token, Timeout: 30 * time.Second}, httpClient)
	if err != nil {
		t.Fatal(err)
	}
	store := app.NewMemoryStore()
	owner := "core-real-" + uuid.NewString()
	instance := "h2-" + uuid.NewString()
	scope := app.Scope(owner, "chat")
	document := "h2-doc-" + uuid.NewString()
	operation := uuid.NewString()
	expires := time.Now().Add(time.Hour)
	source := "synthetic-message/" + uuid.NewString()
	fact := "The user's preferred incident dashboard color is violet."
	digest := sha256.Sum256([]byte(fact))
	policy := app.Policy{ID: app.PrivatePolicyID(owner, "chat"), Version: "v1", OwnerID: owner, Mode: "chat", OwnerTypes: []string{"user"}, Enabled: true, ExplicitWriteOnly: true, MinimumConfidence: 0.5, DefaultTTL: time.Hour, MaximumTTL: 2 * time.Hour}
	service, err := app.NewService(store)
	if err != nil {
		t.Fatal(err)
	}
	if err = service.PutPolicy(ctx, policy); err != nil {
		t.Fatal(err)
	}
	record, err := service.PutRecord(ctx, app.Record{ID: "h2-record-" + uuid.NewString(), Revision: 1, OwnerType: "user", OwnerID: owner, ScopeHash: scope, Fact: fact, SourceType: "explicit_user", SourceRefs: []string{source}, SourceHashes: map[string]string{source: hex.EncodeToString(digest[:])}, Confidence: 1, PolicyVer: policy.Version, ExpiresAt: &expires}, policy)
	if err != nil {
		t.Fatal(err)
	}
	ss := &realSyncStore{store: store, owner: owner, scope: scope, document: document, recordID: record.ID, job: app.SyncJob{ID: operation, Kind: "retain", Record: record, DocumentID: document, Revision: record.Revision, LeaseToken: uuid.NewString()}}
	checker := &realSourceChecker{allowed: true, record: record}
	backend := &realBackendCounters{Backend: client}
	bs, err := app.NewBackendService(store, ss, backend, checker, instance)
	if err != nil {
		t.Fatal(err)
	}
	service.SetBackend(bs)
	// Cleanup is registered before the first external request, including retain failures.
	t.Cleanup(func() {
		if ss.bank == "" {
			return
		}
		cleanupCtx, stop := context.WithTimeout(context.Background(), 15*time.Second)
		defer stop()
		req, e := http.NewRequestWithContext(cleanupCtx, http.MethodDelete, strings.TrimRight(cleanupEndpoint, "/")+"/v1/default/banks/"+url.PathEscape(ss.bank), nil)
		if e != nil {
			t.Error("bank cleanup request failed")
			return
		}
		req.Header.Set("Authorization", "Bearer "+token)
		resp, e := httpClient.Do(req)
		if e != nil {
			t.Error("exact bank cleanup transport failed")
			return
		}
		defer resp.Body.Close()
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 8192))
		if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusNotFound {
			t.Errorf("exact bank cleanup HTTP %d", resp.StatusCode)
			return
		}
		t.Logf("H2 cleanup: bank=%s deleted=true http=%d", ss.bank, resp.StatusCode)
	})
	t.Logf("H2 identities: owner=%s document=%s operation=%s sourceRevision=%d", owner, document, operation, record.Revision)
	until := time.Now().Add(85 * time.Second)
	for ss.state != "done" {
		if !time.Now().Before(until) {
			t.Fatalf("retain operation did not complete; state=%s polls=%d", ss.state, backend.operations)
		}
		worked, e := bs.SyncOne(ctx)
		if e != nil {
			t.Fatalf("sync failed without replay: state=%s error=%v", ss.state, e)
		}
		if !worked {
			t.Fatal("outbox ended before operation confirmed")
		}
		if ss.state != "done" {
			select {
			case <-ctx.Done():
				t.Fatal("operation deadline")
			case <-time.After(500 * time.Millisecond):
			}
		}
	}
	if backend.retains != 1 || !ss.marked || !ss.job.Attempted {
		t.Fatalf("expected one submitted retain; retains=%d", backend.retains)
	}
	query := "What color does the user prefer for incident dashboards?"
	got, err := service.Recall(ctx, owner, "chat", query)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].ID != record.ID || got[0].Fact != fact || got[0].Revision != record.Revision || got[0].SourceHashes[source] != record.SourceHashes[source] {
		t.Fatalf("real recall did not project original current record/revision; count=%d", len(got))
	}
	checker.allowed = false
	got, err = service.Recall(ctx, owner, "chat", query)
	if err != nil || len(got) != 0 {
		t.Fatalf("revoked source recalled: count=%d err=%v", len(got), err)
	}
	checker.allowed = true
	if err = store.DeleteRecord(ctx, record.ID, time.Now()); err != nil {
		t.Fatal(err)
	}
	got, err = service.Recall(ctx, owner, "chat", query)
	if err != nil || len(got) != 0 {
		t.Fatalf("local tombstone recalled: count=%d err=%v", len(got), err)
	}
	operationPath := "/v1/default/banks/" + url.PathEscape(ss.bank) + "/operations/" + url.PathEscape(operation)
	lookup, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(endpoint, "/")+operationPath, nil)
	if err != nil {
		t.Fatal(err)
	}
	lookup.Header.Set("Authorization", "Bearer "+token)
	operationResponse, err := httpClient.Do(lookup)
	if err != nil {
		t.Fatal(err)
	}
	var parent struct {
		ID       string `json:"operation_id"`
		Status   string `json:"status"`
		Children []struct {
			ID string `json:"operation_id"`
		} `json:"child_operations"`
	}
	err = json.NewDecoder(io.LimitReader(operationResponse.Body, 2*1024*1024)).Decode(&parent)
	_ = operationResponse.Body.Close()
	if err != nil || operationResponse.StatusCode != http.StatusOK || parent.ID != operation ||
		parent.Status != "completed" || len(parent.Children) != 1 || parent.Children[0].ID == "" || parent.Children[0].ID == operation {
		t.Fatal("expected the settled parent and exact retain child before deletion")
	}
	ss.job = app.SyncJob{ID: uuid.NewString(), Kind: "delete", Record: record, DocumentID: document, Revision: record.Revision, LeaseToken: uuid.NewString(), RetainOperationID: operation}
	ss.state = ""
	worked, err := bs.SyncOne(ctx)
	if err != nil || !worked || ss.state != "done" {
		t.Fatalf("real document delete failed: worked=%t state=%s error=%v", worked, ss.state, err)
	}
	for _, id := range []string{operation, parent.Children[0].ID} {
		if _, err := client.Operation(ctx, ss.bank, id); !errors.Is(err, app.ErrBackendNotFound) {
			t.Fatalf("deleted retain operation still present or unverified: %v", err)
		}
	}
	candidates, err := backend.Recall(ctx, ss.bank, query)
	if err != nil {
		t.Fatal(err)
	}
	for _, candidate := range candidates {
		if candidate.DocumentID == document {
			t.Fatalf("deleted external document still recalled (revision=%d)", candidate.Revision)
		}
	}
	t.Logf("H2 PASS: retain=%d operationPolls=%d recall=%d deleteDocument=%d sourceChecks=%d; original fact/revision, revoked source, local tombstone, external document and parent/child payload deletion verified; CoreStore=in-memory SourceValidator=synthetic Hindsight=real; sourceHashes=%v", backend.retains, backend.operations, backend.recalls, backend.deletes, checker.validations, realSourceHashes(t))
}
