package knowledge

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	appaccess "github.com/opensoha/soha/internal/application/access"
	appaigateway "github.com/opensoha/soha/internal/application/aigateway"
	appknowledge "github.com/opensoha/soha/internal/application/knowledge"
	domainaigateway "github.com/opensoha/soha/internal/domain/aigateway"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainknowledge "github.com/opensoha/soha/internal/domain/knowledge"
	"github.com/opensoha/soha/internal/infrastructure/aigatewayrelay"
	dbstore "github.com/opensoha/soha/internal/infrastructure/db"
	"github.com/opensoha/soha/internal/infrastructure/knowledge/connectors"
	"github.com/opensoha/soha/internal/infrastructure/knowledge/modelgateway"
	repositorygateway "github.com/opensoha/soha/internal/repository/aigateway"
	"go.uber.org/zap"
)

const (
	fixturePublicModel   = "knowledge-public-alias"
	fixtureUpstreamModel = "knowledge-configured-model"
)

func TestKnowledgeGatewayFallbackIngestionWithPostgres(t *testing.T) {
	store, cfg, ctx := newKnowledgePostgresDatabase(t)
	var primarySuccesses atomic.Int32
	primary := knowledgeEmbeddingServer(t, func() bool { return primarySuccesses.Add(-1) >= 0 })
	backup := knowledgeEmbeddingServer(t, func() bool { return true })
	repo, gateway, service, principal := newKnowledgeGatewayFixture(t, store)
	primaryID, backupID, route := seedKnowledgeGatewayRoute(t, ctx, gateway, principal, primary.URL, backup.URL)
	base, source := seedGatewayIngestionSource(t, ctx, repo, principal)
	job, err := service.CreateIngestionJob(ctx, principal, base.ID, source.ID)
	if err != nil {
		t.Fatal(err)
	}
	published := runKnowledgeIngestion(t, ctx, service, principal, job.ID)
	if published.Status != domainknowledge.IngestionJobSucceeded || published.Stage != domainknowledge.IngestionStagePublishing {
		logKnowledgeGatewayAttempts(t, ctx, store)
		t.Fatalf("fallback ingestion failed: %+v", published)
	}

	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := dbstore.New(cfg, zap.NewNop())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = reopened.Close() })
	repo, _, service, principal = newKnowledgeGatewayFixture(t, reopened)
	wantSource := domainknowledge.EmbeddingSource{UpstreamID: backupID, ProviderKind: "openai", UpstreamModel: fixtureUpstreamModel}
	assertKnowledgeGatewayFallbackLogs(t, ctx, reopened, route, primaryID, wantSource)
	revision := assertKnowledgeGatewayRevision(t, ctx, service, principal, base.ID, wantSource)
	request := domainknowledge.SearchRequest{KnowledgeBaseIDs: []string{base.ID}, Query: "zqxv unrelated synthetic query"}
	assertKnowledgeGatewaySearch(t, ctx, service, principal, request, revision.ID, 2)
	chunks, err := repo.SearchAuthorizedChunks(ctx, domainknowledge.PrincipalScope{UserID: principal.UserID}, request, domainknowledge.EmbeddingBatch{
		Model: fixturePublicModel, Route: route, Source: wantSource, Vectors: [][]float64{{1, 0}},
	}, 10)
	if err != nil || len(chunks) != 2 {
		t.Fatalf("reopened vectors: %v %+v", err, chunks)
	}
	for _, chunk := range chunks {
		if chunk.EmbeddingSource != wantSource || chunk.EmbeddingRoute != route || chunk.EmbeddingModel != fixturePublicModel || chunk.EmbeddingDimension != 2 {
			t.Fatalf("reopened chunk lost fallback identity: %+v", chunk)
		}
	}

	primarySuccesses.Store(1)
	assertKnowledgeGatewaySearch(t, ctx, service, principal, request, revision.ID, 0)
	primarySuccesses.Store(1) // Probe uses primary; remaining batch falls back to backup.
	failedJob := job
	failedJob.ID, failedJob.TargetRevision, failedJob.MaxAttempts = uuid.NewString(), job.TargetRevision+1, 1
	if err := repo.CreateIngestionJob(ctx, failedJob); err != nil {
		t.Fatal(err)
	}
	failed := runKnowledgeIngestion(t, ctx, service, principal, failedJob.ID)
	if failed.Status != domainknowledge.IngestionJobFailed || failed.ErrorCode != "embedding_failed" {
		logKnowledgeGatewayAttempts(t, ctx, reopened)
		t.Fatalf("source change published or failed at an unexpected stage: %+v", failed)
	}
	current := assertKnowledgeGatewayRevision(t, ctx, service, principal, base.ID, wantSource)
	if current.ID != revision.ID {
		t.Fatalf("failed ingestion replaced active revision: %s -> %s", revision.ID, current.ID)
	}
	assertKnowledgeGatewaySearch(t, ctx, service, principal, request, revision.ID, 2)
}

func logKnowledgeGatewayAttempts(t *testing.T, ctx context.Context, store *dbstore.Store) {
	t.Helper()
	logs, err := repositorygateway.New(store.DB()).ListLLMCallLogs(ctx, domainaigateway.LLMCallLogFilter{PublicModel: fixturePublicModel, Endpoint: "embeddings"})
	if err != nil {
		t.Logf("read fixture gateway attempts: %v", err)
		return
	}
	for _, log := range logs {
		t.Logf("fixture gateway attempt request=%s upstream=%s route=%v status=%s http=%d code=%s",
			log.RequestID, log.UpstreamID, log.RouteTrace["routeId"], log.Status, log.UpstreamStatus, log.ErrorCode)
	}
}

func knowledgeEmbeddingServer(t *testing.T, succeeds func() bool) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			Model string   `json:"model"`
			Input []string `json:"input"`
		}
		if r.Method != http.MethodPost || r.URL.Path != "/v1/embeddings" || json.NewDecoder(r.Body).Decode(&request) != nil || request.Model != fixtureUpstreamModel || len(request.Input) == 0 {
			t.Error("unexpected outbound embedding request")
			http.Error(w, "invalid fixture request", http.StatusBadRequest)
			return
		}
		if !succeeds() {
			http.Error(w, "fixture unavailable", http.StatusServiceUnavailable)
			return
		}
		type entry struct {
			Index     int       `json:"index"`
			Embedding []float64 `json:"embedding"`
		}
		entries := make([]entry, len(request.Input))
		for i := range entries {
			entries[i] = entry{Index: i, Embedding: []float64{1, 0}}
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(map[string]any{"data": entries, "usage": map[string]int{"prompt_tokens": 2, "total_tokens": 2}}); err != nil {
			t.Error(err)
		}
	}))
	t.Cleanup(server.Close)
	return server
}

type knowledgeGatewayRoleGrants map[string][]string

func (grants knowledgeGatewayRoleGrants) ListRolePermissions(context.Context) (map[string][]string, error) {
	return grants, nil
}

type knowledgeGatewayPrincipal struct{ principal domainidentity.Principal }

func (reader knowledgeGatewayPrincipal) CurrentExecutionPrincipal(context.Context, string, string) (domainidentity.Principal, error) {
	return reader.principal, nil
}

func newKnowledgeGatewayFixture(t *testing.T, store *dbstore.Store) (*Repository, *appaigateway.Service, *appknowledge.Service, domainidentity.Principal) {
	t.Helper()
	principal := domainidentity.Principal{UserID: "knowledge-fixture-owner", Roles: []string{"fixture"}}
	permissions := appaccess.NewPermissionResolver(knowledgeGatewayRoleGrants{
		"fixture": {
			appaccess.PermAIGatewayRelayInvoke, appaccess.ManagedActionPermission(appaccess.PermAIGatewayRelayManage, "create"),
			appknowledge.PermKnowledgeView, appknowledge.PermKnowledgeConnectorsView, appknowledge.PermKnowledgeIngestionOperate,
		},
	})
	relayRepo := repositorygateway.New(store.DB())
	gateway := appaigateway.NewWithDeps(appaigateway.ServiceDeps{
		Permissions: permissions, LLMRelay: relayRepo, RateLimits: relayRepo,
		RelayConfig: appaigateway.LLMRelayConfig{
			Enabled: true, DefaultTimeout: 2 * time.Second, CredentialEncryptionKey: "knowledge-fixture-encryption-key",
		},
		RelayTransport: aigatewayrelay.New(aigatewayrelay.Config{AllowPrivateUpstreamHosts: true, AllowInsecureUpstreamHTTP: true}),
	})
	adapter, err := modelgateway.New(gateway, modelgateway.Config{EmbeddingModel: fixturePublicModel, Dimension: 2})
	if err != nil {
		t.Fatal(err)
	}
	repo := New(store.DB())
	service, err := appknowledge.New(repo, permissions, nil, adapter, appknowledge.WithProductionRepository(repo),
		appknowledge.WithSourceLoader(connectors.DefaultLoader()), appknowledge.WithExecutionPrincipalReader(knowledgeGatewayPrincipal{principal}))
	if err != nil {
		t.Fatal(err)
	}
	return repo, gateway, service, principal
}

func seedKnowledgeGatewayRoute(t *testing.T, ctx context.Context, gateway *appaigateway.Service, principal domainidentity.Principal, firstURL, secondURL string) (string, string, string) {
	t.Helper()
	var ids []string
	for i, baseURL := range []string{firstURL, secondURL} {
		upstream, err := gateway.CreateLLMUpstream(ctx, principal, domainaigateway.LLMUpstreamInput{
			Name: []string{"primary", "backup"}[i], ProviderKind: "openai", BaseURL: baseURL + "/v1", APIKey: "test-only",
			Priority: i + 1, Metadata: map[string]any{"circuitBreaker": map[string]any{"failureThreshold": 20}},
		})
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, upstream.ID)
	}
	route, err := gateway.CreateLLMModelRoute(ctx, principal, domainaigateway.LLMModelRouteInput{
		PublicModel: fixturePublicModel, ProviderKind: "openai", UpstreamModel: fixtureUpstreamModel,
	})
	if err != nil {
		t.Fatal(err)
	}
	return ids[0], ids[1], route.ID
}

func seedGatewayIngestionSource(t *testing.T, ctx context.Context, repo *Repository, principal domainidentity.Principal) (domainknowledge.KnowledgeBase, domainknowledge.Source) {
	t.Helper()
	now := time.Now().UTC()
	scope := domainknowledge.AccessScope{Visibility: "private", Users: []string{principal.UserID}}
	base, err := repo.CreateBase(ctx, domainknowledge.KnowledgeBase{
		ID: uuid.NewString(), Name: "fallback fixture", Status: domainknowledge.BaseStatusActive, OwnerID: principal.UserID, Scope: scope, CreatedAt: now, UpdatedAt: now,
	})
	if err != nil {
		t.Fatal(err)
	}
	source, err := repo.CreateSource(ctx, domainknowledge.PrincipalScope{UserID: principal.UserID}, domainknowledge.Source{
		ID: uuid.NewString(), KnowledgeBaseID: base.ID, Name: "fallback inline", Kind: domainknowledge.SourceKindInline, Status: domainknowledge.SourceStatusReady, CreatedAt: now, UpdatedAt: now,
		Config: map[string]any{"documents": []domainknowledge.SourceDocument{
			{ExternalID: "first", Title: "First", Content: "deployment handbook", ACL: scope},
			{ExternalID: "second", Title: "Second", Content: "incident runbook", ACL: scope},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	return base, source
}

func runKnowledgeIngestion(t *testing.T, ctx context.Context, service *appknowledge.Service, principal domainidentity.Principal, id string) domainknowledge.IngestionJob {
	t.Helper()
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	done := make(chan struct{})
	go func() {
		defer close(done)
		service.RunIngestionWorker(ctx)
	}()
	defer func() {
		cancel()
		<-done
	}()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		job, err := service.GetIngestionJob(ctx, principal, id)
		if err != nil {
			t.Fatal(err)
		}
		if job.Terminal() || job.Status == domainknowledge.IngestionJobRetryWait {
			return job
		}
		select {
		case <-ctx.Done():
			t.Fatalf("ingestion did not settle: %+v", job)
		case <-ticker.C:
		}
	}
}

func assertKnowledgeGatewayFallbackLogs(t *testing.T, ctx context.Context, store *dbstore.Store, route, primary string, want domainknowledge.EmbeddingSource) {
	t.Helper()
	logs, err := repositorygateway.New(store.DB()).ListLLMCallLogs(ctx, domainaigateway.LLMCallLogFilter{PublicModel: fixturePublicModel, Endpoint: "embeddings"})
	if err != nil || len(logs) != 4 {
		t.Fatalf("expected two fallback attempts with durable receipts: %v %+v", err, logs)
	}
	failed, succeeded := map[string]bool{}, map[string]bool{}
	for _, log := range logs {
		if log.RouteTrace["routeId"] != route || log.RouteTrace["upstreamId"] != log.UpstreamID || log.ProviderKind != want.ProviderKind || log.UpstreamModel != want.UpstreamModel {
			t.Fatalf("unexpected durable attribution: %+v", log)
		}
		switch {
		case log.UpstreamID == primary && log.UpstreamStatus == http.StatusServiceUnavailable && log.Status != "success":
			failed[log.RequestID] = true
		case log.UpstreamID == want.UpstreamID && log.Status == "success" && log.UpstreamStatus == http.StatusOK:
			succeeded[log.RequestID] = true
		default:
			t.Fatalf("unexpected fallback attempt: %+v", log)
		}
	}
	if len(failed) != 2 || len(succeeded) != 2 {
		t.Fatalf("unexpected fallback request IDs: %v %v", failed, succeeded)
	}
	for id := range failed {
		if id == "" || !succeeded[id] {
			t.Fatalf("primary failure has no corresponding backup receipt: %q", id)
		}
	}
}

func assertKnowledgeGatewayRevision(t *testing.T, ctx context.Context, service *appknowledge.Service, principal domainidentity.Principal, base string, want domainknowledge.EmbeddingSource) domainknowledge.IndexRevision {
	t.Helper()
	revisions, err := service.ListIndexRevisions(ctx, principal, base, 5)
	if err != nil || len(revisions) != 1 || revisions[0].Status != domainknowledge.IndexStatusActive || revisions[0].EmbeddingSource != want || revisions[0].ChunkCount != 2 {
		t.Fatalf("unexpected persisted revision: %v %+v", err, revisions)
	}
	return revisions[0]
}

func assertKnowledgeGatewaySearch(t *testing.T, ctx context.Context, service *appknowledge.Service, principal domainidentity.Principal, request domainknowledge.SearchRequest, revision string, want int) {
	t.Helper()
	result, err := service.Search(ctx, principal, request)
	if err != nil || len(result.Hits) != want {
		t.Fatalf("unexpected search result: %v %+v", err, result)
	}
	if want == 0 && (result.RetrievalMode != "lexical" || result.DegradationReason != "embedding_missing_or_mismatched") {
		t.Fatalf("cross-upstream query did not degrade to lexical: %+v", result)
	}
	if want > 0 && result.RetrievalMode != "hybrid" {
		t.Fatalf("backup query did not use vector retrieval: %+v", result)
	}
	for _, hit := range result.Hits {
		if hit.VectorScore < 0.99 || hit.Citation.IndexRevisionID != revision || hit.Citation.DocumentVersionID == "" {
			t.Fatalf("hit lacks the published vector attribution: %+v", hit)
		}
	}
}
