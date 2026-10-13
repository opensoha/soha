package knowledge

import (
	"context"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	domainknowledge "github.com/opensoha/soha/internal/domain/knowledge"
	"github.com/opensoha/soha/internal/infrastructure/config"
	dbstore "github.com/opensoha/soha/internal/infrastructure/db"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"go.uber.org/zap"
)

func TestKnowledgeRetrievalWithPostgres(t *testing.T) {
	repo, store, ctx := newKnowledgePostgresRepository(t)
	t.Run("candidate_window", func(t *testing.T) { knowledgeRetrievalCandidateWindow(t, repo, store, ctx) })
	t.Run("acl_revoke_delete", func(t *testing.T) { knowledgeRetrievalACLRevokeDelete(t, repo, store, ctx) })
	t.Run("external_import_scope", func(t *testing.T) { knowledgeExternalImportScope(t, repo, store, ctx) })

	t.Run("explicit_source_tombstone", func(t *testing.T) { knowledgeRetrievalExplicitSourceTombstone(t, repo, store, ctx) })
	t.Run("immutable_current_version", func(t *testing.T) { knowledgeRetrievalImmutableCurrentVersion(t, repo, store, ctx) })
	t.Run("model_dimension_isolation", func(t *testing.T) { knowledgeRetrievalModelDimensionIsolation(t, repo, store, ctx) })
	t.Run("failed_publication_atomic", func(t *testing.T) { knowledgeRetrievalFailedPublicationAtomic(t, repo, store, ctx) })
	t.Run("legacy_writer_compatibility", func(t *testing.T) { knowledgeRetrievalLegacyWriterCompatibility(t, repo, store, ctx) })
	t.Run("ingestion_staging_takeover", func(t *testing.T) { knowledgeRetrievalIngestionStagingTakeover(t, repo, store, ctx) })
}

func seedRetrievalDocument(t *testing.T, repo *Repository, ctx context.Context, count int) (domainknowledge.Document, []domainknowledge.Chunk, domainknowledge.PrincipalScope) {
	t.Helper()
	now := time.Now().UTC()
	scope := domainknowledge.AccessScope{Visibility: "private", Users: []string{"owner", "reader"}}
	base := domainknowledge.KnowledgeBase{ID: uuid.NewString(), Name: "retrieval fixture", Status: "active", OwnerID: "owner", Scope: scope, CreatedAt: now, UpdatedAt: now}
	if _, err := repo.CreateBase(ctx, base); err != nil {
		t.Fatal(err)
	}
	source := domainknowledge.Source{ID: uuid.NewString(), KnowledgeBaseID: base.ID, Name: "fixture", Kind: "inline", Status: "ready", CreatedAt: now, UpdatedAt: now}
	if _, err := repo.CreateSource(ctx, domainknowledge.PrincipalScope{UserID: "owner"}, source); err != nil {
		t.Fatal(err)
	}
	doc := domainknowledge.Document{ID: uuid.NewString(), KnowledgeBaseID: base.ID, SourceID: source.ID, ExternalID: "fixture", Title: "Runbook", Version: "v1", ContentHash: "hash:v1", ACL: scope, Status: "indexed", ChunkCount: count, CreatedAt: now, UpdatedAt: now}
	chunks := make([]domainknowledge.Chunk, count)
	for i := range chunks {
		chunks[i] = domainknowledge.Chunk{ID: uuid.NewString(), KnowledgeBaseID: base.ID, DocumentID: doc.ID, DocumentTitle: doc.Title, Ordinal: i, Content: fmt.Sprintf("unrelated operational entry %d", i), ContentHash: fmt.Sprintf("hash:%d", i), ACL: scope, CreatedAt: now, EmbeddingModel: "fixture", Embedding: []float64{0, 1, 0}}
	}
	chunks[0].Content, chunks[0].Embedding, chunks[0].CreatedAt = "ancient deployment needle", []float64{1, 0, 0}, now.Add(-time.Hour)
	if err := repo.UpsertDocument(ctx, doc, chunks); err != nil {
		t.Fatal(err)
	}
	return doc, chunks, domainknowledge.PrincipalScope{UserID: "reader"}
}

func assertNoKnowledgeHits(t *testing.T, repo *Repository, ctx context.Context, p domainknowledge.PrincipalScope, request domainknowledge.SearchRequest) {
	t.Helper()
	hits, err := repo.SearchAuthorizedChunks(ctx, p, request, domainknowledge.EmbeddingBatch{}, 10)
	if err != nil || len(hits) != 0 {
		t.Fatalf("unauthorized/stale content returned: %v %v", err, hits)
	}
}

func newKnowledgePostgresRepository(t *testing.T) (*Repository, *dbstore.Store, context.Context) {
	t.Helper()
	portText := os.Getenv("SOHA_APPLICATION_TEST_POSTGRES_PORT")
	if portText == "" {
		t.Skip("set SOHA_APPLICATION_TEST_POSTGRES_PORT to an isolated PostgreSQL instance")
	}
	port, err := strconv.Atoi(portText)
	if err != nil {
		t.Fatal(err)
	}
	cfg := config.DatabaseConfig{Driver: "postgres", Host: "127.0.0.1", Port: port, Name: "soha", User: "pgsql", Password: "test-only", SSLMode: "disable", MaxOpenConns: 12, MaxIdleConns: 4}
	admin, err := dbstore.New(cfg, zap.NewNop())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = admin.Close() })
	cfg.Name = "knowledge_test_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if err := admin.DB().Exec(`CREATE DATABASE "` + cfg.Name + `"`).Error; err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := admin.DB().Exec(`DROP DATABASE "` + cfg.Name + `" WITH (FORCE)`).Error; err != nil {
			t.Error(err)
		}
	})
	store, err := dbstore.New(cfg, zap.NewNop())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	t.Cleanup(cancel)
	if err := store.MigrateFromFile(ctx, filepath.Join("..", "..", "..", "migrations", "postgres")); err != nil {
		t.Fatal(err)
	}
	return New(store.DB()), store, ctx
}

func knowledgeRetrievalCandidateWindow(t *testing.T, repo *Repository, store *dbstore.Store, ctx context.Context) {
	doc, chunks, p := seedRetrievalDocument(t, repo, ctx, 601)
	request := domainknowledge.SearchRequest{KnowledgeBaseIDs: []string{doc.KnowledgeBaseID}, Query: "ancient deployment needle"}
	old, err := repo.ListAuthorizedChunks(ctx, p, request, 500)
	if err != nil {
		t.Fatal(err)
	}
	for _, chunk := range old {
		if chunk.ID == chunks[0].ID {
			t.Fatal("fixture target lies inside old candidate window")
		}
	}
	hits, err := repo.SearchAuthorizedChunks(ctx, p, request, domainknowledge.EmbeddingBatch{Model: "fixture", Vectors: [][]float64{{1, 0, 0}}}, 10)
	if err != nil || len(hits) == 0 || hits[0].Content != chunks[0].Content || hits[0].DocumentVersionID == "" || hits[0].VectorScore < 0.99 {
		t.Fatalf("hybrid recall: %v %+v", err, hits)
	}
	lexical, err := repo.SearchAuthorizedChunks(ctx, p, request, domainknowledge.EmbeddingBatch{}, 10)
	if err != nil || len(lexical) != 1 || lexical[0].Content != chunks[0].Content {
		t.Fatalf("lexical fallback: %v %+v", err, lexical)
	}

}

func knowledgeRetrievalACLRevokeDelete(t *testing.T, repo *Repository, store *dbstore.Store, ctx context.Context) {
	knowledgeRetrievalDoesNotGrantBaseOwner(t, repo, store, ctx)
	doc, _, p := seedRetrievalDocument(t, repo, ctx, 1)
	request := domainknowledge.SearchRequest{KnowledgeBaseIDs: []string{doc.KnowledgeBaseID}, Query: "ancient deployment needle"}
	hits, err := repo.SearchAuthorizedChunks(ctx, p, request, domainknowledge.EmbeddingBatch{}, 10)
	if err != nil || len(hits) != 1 {
		t.Fatalf("initial ACL: %v %v", err, hits)
	}
	citation := domainknowledge.Citation{KnowledgeBaseID: doc.KnowledgeBaseID, DocumentID: doc.ID, ChunkID: hits[0].ID, ContentHash: hits[0].ContentHash, DocumentVersionID: hits[0].DocumentVersionID}
	if err := repo.ValidateCitations(ctx, p, []domainknowledge.Citation{citation}); err != nil {
		t.Fatal(err)
	}
	other := domainknowledge.PrincipalScope{UserID: "other"}
	assertNoKnowledgeHits(t, repo, ctx, other, request)
	if err := store.DB().Exec(`UPDATE ai_knowledge_bases SET scope='{"visibility":"private","users":["owner"]}' WHERE id=?`, doc.KnowledgeBaseID).Error; err != nil {
		t.Fatal(err)
	}
	assertNoKnowledgeHits(t, repo, ctx, p, request)
	if err := store.DB().Exec(`UPDATE ai_knowledge_bases SET scope='{"visibility":"private","users":["owner","reader"]}' WHERE id=?`, doc.KnowledgeBaseID).Error; err != nil {
		t.Fatal(err)
	}
	if err := store.DB().Exec(`UPDATE ai_knowledge_documents SET acl='{"visibility":"private","users":["owner"]}' WHERE id=?`, doc.ID).Error; err != nil {
		t.Fatal(err)
	}
	assertNoKnowledgeHits(t, repo, ctx, p, request)
	if err := repo.ValidateCitations(ctx, p, []domainknowledge.Citation{citation}); !errors.Is(err, apperrors.ErrAccessDenied) {
		t.Fatalf("revoked citation accepted: %v", err)
	}
	if err := store.DB().Exec(`UPDATE ai_knowledge_documents SET acl='{"visibility":"private","users":["owner","reader"]}',status='deleted' WHERE id=?`, doc.ID).Error; err != nil {
		t.Fatal(err)
	}
	assertNoKnowledgeHits(t, repo, ctx, p, request)
	if err := repo.DeleteBase(ctx, domainknowledge.PrincipalScope{UserID: "owner"}, doc.KnowledgeBaseID); err != nil {
		t.Fatal(err)
	}
	assertNoKnowledgeHits(t, repo, ctx, p, request)

}

func knowledgeRetrievalDoesNotGrantBaseOwner(t *testing.T, repo *Repository, store *dbstore.Store, ctx context.Context) {
	t.Helper()
	doc, _, reader := seedRetrievalDocument(t, repo, ctx, 1)
	request := domainknowledge.SearchRequest{KnowledgeBaseIDs: []string{doc.KnowledgeBaseID}, Query: "ancient deployment needle"}
	owner := domainknowledge.PrincipalScope{UserID: "owner"}
	var citation domainknowledge.Citation
	for _, test := range []struct {
		acl        string
		readerHits int
	}{
		{acl: `{"visibility":"scoped","users":["reader"]}`, readerHits: 1},
		{acl: `{"visibility":"private","users":[]}`},
	} {
		if err := store.DB().Exec(`UPDATE ai_knowledge_documents SET acl=?::jsonb WHERE id=?`, test.acl, doc.ID).Error; err != nil {
			t.Fatal(err)
		}
		assertNoKnowledgeHits(t, repo, ctx, owner, request)
		hits, err := repo.SearchAuthorizedChunks(ctx, reader, request, domainknowledge.EmbeddingBatch{}, 10)
		if err != nil || len(hits) != test.readerHits {
			t.Fatalf("explicit document ACL ignored: err=%v reader hits=%d", err, len(hits))
		}
		if len(hits) > 0 {
			citation = domainknowledge.Citation{KnowledgeBaseID: doc.KnowledgeBaseID, DocumentID: doc.ID, ChunkID: hits[0].ID, ContentHash: hits[0].ContentHash, DocumentVersionID: hits[0].DocumentVersionID}
		}
		if err := repo.ValidateCitations(ctx, owner, []domainknowledge.Citation{citation}); !errors.Is(err, apperrors.ErrAccessDenied) {
			t.Fatalf("base ownership bypassed document citation ACL: %v", err)
		}
	}
}

func knowledgeRetrievalExplicitSourceTombstone(t *testing.T, repo *Repository, store *dbstore.Store, ctx context.Context) {
	doc, _, p := seedRetrievalDocument(t, repo, ctx, 1)
	request := domainknowledge.SearchRequest{KnowledgeBaseIDs: []string{doc.KnowledgeBaseID}, Query: "ancient deployment needle"}
	hits, err := repo.SearchAuthorizedChunks(ctx, p, request, domainknowledge.EmbeddingBatch{}, 10)
	if err != nil || len(hits) != 1 {
		t.Fatalf("fixture: %v %+v", err, hits)
	}
	citation := domainknowledge.Citation{KnowledgeBaseID: doc.KnowledgeBaseID, DocumentID: doc.ID, ChunkID: hits[0].ID, ContentHash: hits[0].ContentHash, DocumentVersionID: hits[0].DocumentVersionID}
	doc.Status = domainknowledge.DocumentStatusDeleted
	foreign := doc
	foreign.SourceID = uuid.NewString()
	if err := repo.UpsertDocument(ctx, foreign, nil); err != nil {
		t.Fatal(err)
	}
	if err := repo.ValidateCitations(ctx, p, []domainknowledge.Citation{citation}); err != nil {
		t.Fatalf("foreign source deleted document: %v", err)
	}
	if err := repo.UpsertDocument(ctx, doc, nil); err != nil {
		t.Fatal(err)
	}
	assertNoKnowledgeHits(t, repo, ctx, p, request)
	if err := repo.ValidateCitations(ctx, p, []domainknowledge.Citation{citation}); !errors.Is(err, apperrors.ErrAccessDenied) {
		t.Fatalf("deleted citation remains accessible: %v", err)
	}
	var vectors int64
	if err := store.DB().Raw(`SELECT count(*) FROM ai_knowledge_version_chunks WHERE document_version_id=?`, citation.DocumentVersionID).Scan(&vectors).Error; err != nil || vectors != 0 {
		t.Fatalf("derived vectors retained: %d %v", vectors, err)
	}
	if err := repo.UpsertDocument(ctx, doc, nil); err != nil {
		t.Fatalf("tombstone retry: %v", err)
	}

}

func knowledgeRetrievalImmutableCurrentVersion(t *testing.T, repo *Repository, store *dbstore.Store, ctx context.Context) {
	doc, chunks, p := seedRetrievalDocument(t, repo, ctx, 1)
	var oldVersion string
	if err := store.DB().Raw(`SELECT current_version_id FROM ai_knowledge_documents WHERE id=?`, doc.ID).Scan(&oldVersion).Error; err != nil {
		t.Fatal(err)
	}
	doc.Version, doc.ContentHash = "v2", "hash:v2"
	chunks[0].Content, chunks[0].ContentHash = "replacement release evidence", "hash:v2"
	if err := repo.UpsertDocument(ctx, doc, chunks); err != nil {
		t.Fatal(err)
	}
	var count int64
	if err := store.DB().Raw(`SELECT count(*) FROM ai_knowledge_document_versions WHERE document_id=?`, doc.ID).Scan(&count).Error; err != nil || count != 2 {
		t.Fatalf("history: %d %v", count, err)
	}
	var content string
	if err := store.DB().Raw(`SELECT content FROM ai_knowledge_version_chunks WHERE document_version_id=?`, oldVersion).Scan(&content).Error; err != nil || content != "ancient deployment needle" {
		t.Fatalf("history changed: %s %v", content, err)
	}
	if err := store.DB().Exec(`UPDATE ai_knowledge_version_chunks SET content='changed' WHERE document_version_id=?`, oldVersion).Error; err == nil {
		t.Fatal("immutable chunk update accepted")
	}
	request := domainknowledge.SearchRequest{KnowledgeBaseIDs: []string{doc.KnowledgeBaseID}, Query: "ancient deployment needle"}
	assertNoKnowledgeHits(t, repo, ctx, p, request)
	request.Query = "replacement release evidence"
	hits, err := repo.SearchAuthorizedChunks(ctx, p, request, domainknowledge.EmbeddingBatch{}, 10)
	if err != nil || len(hits) != 1 || hits[0].DocumentVersionID == oldVersion {
		t.Fatalf("current pointer: %v %v", err, hits)
	}

}

func knowledgeRetrievalModelDimensionIsolation(t *testing.T, repo *Repository, store *dbstore.Store, ctx context.Context) {
	doc, _, p := seedRetrievalDocument(t, repo, ctx, 1)
	request := domainknowledge.SearchRequest{KnowledgeBaseIDs: []string{doc.KnowledgeBaseID}, Query: "unmatched words"}
	for _, batch := range []domainknowledge.EmbeddingBatch{{Model: "different-model", Vectors: [][]float64{{1, 0, 0}}}, {Model: "fixture", Vectors: [][]float64{{1, 0}}}} {
		hits, err := repo.SearchAuthorizedChunks(ctx, p, request, batch, 10)
		if err != nil || len(hits) != 0 {
			t.Fatalf("embedding space crossed: %v %v", err, hits)
		}
	}

}

func knowledgeRetrievalFailedPublicationAtomic(t *testing.T, repo *Repository, store *dbstore.Store, ctx context.Context) {
	doc, chunks, p := seedRetrievalDocument(t, repo, ctx, 1)
	doc.Version, doc.ContentHash = "broken", "broken"
	chunks[0].Content = "failed publication"
	chunks[0].Embedding = []float64{math.NaN(), 0, 0}
	if err := repo.UpsertDocument(ctx, doc, chunks); err == nil {
		t.Fatal("invalid vector publication succeeded")
	}
	var version string
	if err := store.DB().Raw(`SELECT version FROM ai_knowledge_documents WHERE id=?`, doc.ID).Scan(&version).Error; err != nil || version != "v1" {
		t.Fatalf("failed publication changed document: %s %v", version, err)
	}
	request := domainknowledge.SearchRequest{KnowledgeBaseIDs: []string{doc.KnowledgeBaseID}, Query: "ancient deployment needle"}
	hits, err := repo.SearchAuthorizedChunks(ctx, p, request, domainknowledge.EmbeddingBatch{}, 10)
	if err != nil || len(hits) != 1 {
		t.Fatalf("failed publication changed current retrieval: %v %v", err, hits)
	}

}

func knowledgeRetrievalLegacyWriterCompatibility(t *testing.T, repo *Repository, store *dbstore.Store, ctx context.Context) {
	doc, _, p := seedRetrievalDocument(t, repo, ctx, 1)
	if err := store.DB().Exec(`UPDATE ai_knowledge_documents SET content_hash='legacy-update',version='legacy-update' WHERE id=?`, doc.ID).Error; err != nil {
		t.Fatal(err)
	}
	if err := store.DB().Exec(`UPDATE ai_knowledge_chunks SET content='legacy replacement',content_hash='legacy-update' WHERE document_id=?`, doc.ID).Error; err != nil {
		t.Fatal(err)
	}
	request := domainknowledge.SearchRequest{KnowledgeBaseIDs: []string{doc.KnowledgeBaseID}, Query: "legacy replacement"}
	hits, err := repo.SearchAuthorizedChunks(ctx, p, request, domainknowledge.EmbeddingBatch{}, 10)
	if err != nil || len(hits) != 1 || hits[0].DocumentVersionID != "" {
		t.Fatalf("legacy writer: %v %v", err, hits)
	}

}

func knowledgeRetrievalIngestionStagingTakeover(t *testing.T, repo *Repository, store *dbstore.Store, ctx context.Context) {
	doc, chunks, _ := seedRetrievalDocument(t, repo, ctx, 2)
	doc.Status = domainknowledge.DocumentStatusPending
	now := time.Now().UTC()
	job := domainknowledge.IngestionJob{ID: uuid.NewString(), KnowledgeBaseID: doc.KnowledgeBaseID, SourceID: doc.SourceID, TargetRevision: now.UnixNano(), Stage: domainknowledge.IngestionStageDiscovering, Status: domainknowledge.IngestionJobQueued, MaxAttempts: 3, CreatedAt: now, UpdatedAt: now}
	if err := repo.CreateIngestionJob(ctx, job); err != nil {
		t.Fatal(err)
	}
	first, err := repo.ClaimIngestionJob(ctx, now, "first", now.Add(time.Minute))
	if err != nil || first == nil || first.ID != job.ID {
		t.Fatalf("first claim: %+v %v", first, err)
	}
	chunks[1].Embedding = nil
	if err := repo.StageIngestionDocument(ctx, job.ID, first.LeaseToken, doc, chunks); err != nil {
		t.Fatal(err)
	}
	first.Status = domainknowledge.IngestionJobRetryWait
	first.NextAttemptAt = &now
	if err := repo.AdvanceIngestionJob(ctx, *first, domainknowledge.IngestionJobRunning, first.Stage); err != nil {
		t.Fatal(err)
	}
	second, err := repo.ClaimIngestionJob(ctx, now, "second", now.Add(time.Minute))
	if err != nil || second == nil || second.ID != job.ID {
		t.Fatalf("second claim: %+v %v", second, err)
	}
	restored, err := repo.LoadIngestionDocument(ctx, job.ID, second.LeaseToken, doc)
	if err != nil || len(restored) != 2 || len(restored[0].Embedding) != 3 || len(restored[1].Embedding) != 0 {
		t.Fatalf("partial vectors lost: %+v %v", restored, err)
	}
	if err := repo.StageIngestionDocument(ctx, job.ID, first.LeaseToken, doc, chunks); !errors.Is(err, domainknowledge.ErrIngestionConflict) {
		t.Fatalf("stale lease overwrote vectors: %v", err)
	}
	changed := doc
	changed.ContentHash = "changed"
	if old, err := repo.LoadIngestionDocument(ctx, job.ID, second.LeaseToken, changed); err != nil || len(old) != 0 {
		t.Fatalf("changed content reused vectors: %+v %v", old, err)
	}
	assertKnowledgeStagingPublication(t, repo, store, ctx, now, job, doc, second, restored)

}

func assertKnowledgeStagingPublication(t *testing.T, repo *Repository, store *dbstore.Store, ctx context.Context, now time.Time, job domainknowledge.IngestionJob, doc domainknowledge.Document, second *domainknowledge.IngestionJob, restored []domainknowledge.Chunk) {
	t.Helper()
	completed := now.Add(time.Second)
	second.Stage = domainknowledge.IngestionStagePublishing
	if err := repo.AdvanceIngestionJob(ctx, *second, domainknowledge.IngestionJobRunning, domainknowledge.IngestionStageDiscovering); err != nil {
		t.Fatal(err)
	}
	second.Status, second.UpdatedAt, second.CompletedAt = domainknowledge.IngestionJobSucceeded, completed, &completed
	source, err := repo.GetSourceInternal(ctx, job.KnowledgeBaseID, job.SourceID)
	if err != nil {
		t.Fatal(err)
	}
	revision := domainknowledge.IndexRevision{ID: uuid.NewString(), KnowledgeBaseID: doc.KnowledgeBaseID, Revision: int(job.TargetRevision), DocumentCount: 1, ChunkCount: 2, Status: domainknowledge.IndexStatusActive, CreatedAt: completed, ActivatedAt: &completed}
	if err := repo.PublishIngestionJob(ctx, *second, source, revision); !errors.Is(err, domainknowledge.ErrIngestionConflict) {
		t.Fatalf("old-lease staging published: %v", err)
	}
	restored[1].Embedding = []float64{0, 1, 0}
	if err := repo.StageIngestionDocument(ctx, job.ID, "second", doc, restored); err != nil {
		t.Fatal(err)
	}
	if err := repo.PublishIngestionJob(ctx, *second, source, revision); err != nil {
		t.Fatal(err)
	}
	var staged int64
	if err := store.DB().Raw(`SELECT count(*) FROM ai_knowledge_ingestion_document_staging WHERE job_id=?`, job.ID).Scan(&staged).Error; err != nil || staged != 0 {
		t.Fatalf("staging cleanup: %d %v", staged, err)
	}

}
