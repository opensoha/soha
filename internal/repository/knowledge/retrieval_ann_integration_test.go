package knowledge

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	domainknowledge "github.com/opensoha/soha/internal/domain/knowledge"
	dbstore "github.com/opensoha/soha/internal/infrastructure/db"
	"gorm.io/gorm"
)

const annFixtureRows = 8192

func TestKnowledgeVectorIndexesWithPostgres(t *testing.T) {
	for _, dimension := range []int{1536, 3072} {
		t.Run(fmt.Sprint(dimension), func(t *testing.T) {
			repo, store, ctx := newKnowledgePostgresRepository(t)
			knowledgeVectorIndexRecall(t, repo, store, ctx, dimension)
		})
	}
}

func knowledgeVectorIndexRecall(t *testing.T, repo *Repository, store *dbstore.Store, ctx context.Context, dimension int) {
	t.Helper()
	doc, target, principal := seedANNDocument(t, repo, store, ctx, dimension)
	request := domainknowledge.SearchRequest{KnowledgeBaseIDs: []string{doc.KnowledgeBaseID}, Query: "no-lexical-match"}
	batch := domainknowledge.EmbeddingBatch{Model: target.EmbeddingModel, Route: target.EmbeddingRoute, Vectors: [][]float64{target.Embedding}}
	if hits, err := repo.SearchAuthorizedChunks(ctx, principal, request, batch, 50); err != nil || len(hits) != 1 {
		t.Fatalf("initial vector query: error=%v hits=%d", err, len(hits))
	}
	seedANNCorpus(t, store, target)
	query, args := captureKnowledgeSearchQuery(t, repo)
	benchmarkKnowledgeRecall(t, repo, ctx, principal, request, batch, target, "default")
	logKnowledgeQueryPlan(t, store.DB(), ctx, *query, *args, dimension, false)
	// Fixture-only settings exercise HNSW even when a selective scan costs less.
	withKnowledgeANN(t, store, ctx, func(indexed *Repository) {
		benchmarkKnowledgeRecall(t, indexed, ctx, principal, request, batch, target, "forced-ann")
		if rows := logKnowledgeQueryPlan(t, indexed.db, ctx, *query, *args, dimension, true); rows != 50 {
			t.Fatalf("ANN benchmark unexpectedly used fallback: candidates=%d", rows)
		}
	})
	knowledgeEmbeddingSpaceAndACL(t, repo, store, ctx, principal, request, batch, doc)
	knowledgeFilteredANNRecall(t, repo, store, ctx, doc, target, query, args)
	t.Run("partial_ann_fallback", func(t *testing.T) {
		knowledgePartialANNRecall(t, repo, store, ctx, dimension, query, args)
	})
}

func knowledgePartialANNRecall(t *testing.T, repo *Repository, store *dbstore.Store, ctx context.Context, dimension int, query *string, args *[]any) {
	t.Helper()
	doc, near, principal := seedANNDocument(t, repo, store, ctx, dimension)
	near.Embedding[0], near.Embedding[1] = 0, 1
	second := near
	second.ID, second.Ordinal, second.ContentHash = near.ID+":second", 1, "partial-ann-second"
	second.Embedding = append([]float64(nil), near.Embedding...)
	second.Embedding[2] = .01
	doc.Version, doc.ContentHash = "partial-ann", "partial-ann"
	if err := repo.UpsertDocument(ctx, doc, []domainknowledge.Chunk{near, second}); err != nil {
		t.Fatal(err)
	}
	request := domainknowledge.SearchRequest{KnowledgeBaseIDs: []string{doc.KnowledgeBaseID}, Query: "no-lexical-match"}
	batch := domainknowledge.EmbeddingBatch{Model: near.EmbeddingModel, Route: near.EmbeddingRoute, Vectors: [][]float64{near.Embedding}}
	withKnowledgeANN(t, store, ctx, func(indexed *Repository) {
		hits, err := indexed.SearchAuthorizedChunks(ctx, principal, request, batch, 50)
		if err != nil || len(hits) != 2 {
			t.Fatalf("partial fixture has no complete authorized baseline: %v hits=%d", err, len(hits))
		}
		for _, hit := range hits {
			validateANNHit(t, indexed, ctx, principal, hit)
		}
		// Cap only the actual ANN stage; graph traversal limits are nondeterministic.
		sql := *query
		begin, end := strings.Index(sql, ", vector_candidates AS MATERIALIZED ("), strings.Index(sql, ", vectors AS (")
		if begin < 0 || end <= begin {
			t.Fatal("actual ANN stage was not captured")
		}
		match := regexp.MustCompile(`LIMIT \$(\d+)\s*\)$`).FindStringSubmatch(sql[begin:end])
		if len(match) != 2 {
			t.Fatal("actual ANN candidate bound is missing")
		}
		parameter, err := strconv.Atoi(match[1])
		if err != nil || parameter < 1 || parameter > len(*args) {
			t.Fatal("invalid captured ANN candidate bound")
		}
		limited := append([]any(nil), (*args)...)
		limited[parameter-1] = 1
		if rows := logKnowledgeQueryPlan(t, indexed.db, ctx, sql, limited, dimension, true); rows != 1 {
			t.Fatalf("fixture did not exhaust ANN with a nonempty partial result: candidates=%d", rows)
		}
		var ordinals []int
		rows, err := indexed.db.Statement.ConnPool.QueryContext(ctx, "SELECT ordinal FROM ("+sql+") recovered", limited...)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = rows.Close() }()
		for rows.Next() {
			var ordinal int
			if err := rows.Scan(&ordinal); err != nil {
				t.Fatal(err)
			}
			ordinals = append(ordinals, ordinal)
		}
		if err := rows.Err(); err != nil {
			t.Fatal(err)
		}
		if !slices.Equal(ordinals, []int{0, 1}) {
			t.Fatalf("partial ANN lost authorized current chunks: ordinals=%v", ordinals)
		}
		if hits, err := indexed.SearchAuthorizedChunks(ctx, domainknowledge.PrincipalScope{UserID: "other"}, request, batch, 50); err != nil || len(hits) != 0 {
			t.Fatalf("partial fallback bypassed ACL: %v hits=%d", err, len(hits))
		}
	})
}

func seedANNDocument(t *testing.T, repo *Repository, store *dbstore.Store, ctx context.Context, dimension int) (domainknowledge.Document, domainknowledge.Chunk, domainknowledge.PrincipalScope) {
	t.Helper()
	doc, chunks, principal := seedRetrievalDocument(t, repo, ctx, 1)
	vector := make([]float64, dimension)
	vector[0] = 1
	doc.Version, doc.ContentHash = "ann-v1", "ann-hash-v1"
	chunks[0].Embedding, chunks[0].EmbeddingRoute = vector, "fixture-route"
	if err := repo.UpsertDocument(ctx, doc, chunks); err != nil {
		t.Fatal(err)
	}
	var versionID string
	if err := store.DB().Raw(`SELECT current_version_id FROM ai_knowledge_documents WHERE id=?`, doc.ID).Scan(&versionID).Error; err != nil {
		t.Fatal(err)
	}
	chunks[0].DocumentVersionID = versionID
	return doc, chunks[0], principal
}

func seedANNCorpus(t *testing.T, store *dbstore.Store, target domainknowledge.Chunk) {
	t.Helper()
	// Synthetic neighborhoods plus background vectors, beyond the former 500 window.
	err := store.DB().Exec(`INSERT INTO ai_knowledge_version_chunks(id,document_version_id,ordinal,content,content_hash,location,token_count,embedding_model,embedding_route,embedding)
		SELECT ? || ':' || i,?,i,'ann unrelated entry ' || i,'ann-hash-' || i,'{}',8,'fixture','fixture-route',
		ARRAY(SELECT CASE WHEN i%16=0 THEN CASE WHEN j=1 THEN 1 ELSE .01*sin((i*j+j*j)::double precision) END ELSE sin((i*j+j*j)::double precision) END FROM generate_series(1,?) j)::vector
		FROM generate_series(1,?) i`, target.DocumentVersionID, target.DocumentVersionID, len(target.Embedding), annFixtureRows-1).Error
	if err != nil {
		t.Fatal(err)
	}
	if err := store.DB().Exec(`INSERT INTO ai_knowledge_chunks(id,knowledge_base_id,document_id,document_title,ordinal,content,content_hash,location,token_count,acl,created_at)
		SELECT c.id,d.knowledge_base_id,d.id,d.title,c.ordinal,c.content,c.content_hash,c.location,c.token_count,d.acl,c.created_at
		FROM ai_knowledge_version_chunks c JOIN ai_knowledge_documents d ON d.id=? WHERE c.document_version_id=? AND c.ordinal>0`, target.DocumentID, target.DocumentVersionID).Error; err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"ai_knowledge_version_chunks", "ai_knowledge_document_versions", "ai_knowledge_documents", "ai_knowledge_bases"} {
		if err := store.DB().Exec("ANALYZE " + table).Error; err != nil {
			t.Fatal(err)
		}
	}
	var bytes int64
	if err := store.DB().Raw(`SELECT pg_total_relation_size('ai_knowledge_version_chunks')`).Scan(&bytes).Error; err != nil {
		t.Fatal(err)
	}
	t.Logf("dimension=%d corpus_rows=%d version_chunk_total_bytes=%d", len(target.Embedding), annFixtureRows, bytes)
}

func benchmarkKnowledgeRecall(t *testing.T, repo *Repository, ctx context.Context, p domainknowledge.PrincipalScope, request domainknowledge.SearchRequest, batch domainknowledge.EmbeddingBatch, target domainknowledge.Chunk, mode string) {
	t.Helper()
	if mode == "default" {
		old, err := repo.ListAuthorizedChunks(ctx, p, request, 500)
		if err != nil || len(old) != 500 {
			t.Fatalf("former candidate window: %d %v", len(old), err)
		}
		for _, chunk := range old {
			if chunk.Content == target.Content {
				t.Fatal("fixture target lies inside the old window")
			}
		}
	}
	samples := make([]time.Duration, 20)
	for i := range samples {
		started := time.Now()
		hits, err := repo.SearchAuthorizedChunks(ctx, p, request, batch, 50)
		samples[i] = time.Since(started)
		if err != nil || len(hits) == 0 || hits[0].Content != target.Content || hits[0].VectorScore < .99 || hits[0].DocumentVersionID != target.DocumentVersionID {
			t.Fatalf("indexed recall failed: error=%v hits=%d", err, len(hits))
		}
		if i == 0 {
			validateANNHit(t, repo, ctx, p, hits[0])
		}
	}
	sort.Slice(samples, func(i, j int) bool { return samples[i] < samples[j] })
	t.Logf("dimension=%d mode=%s samples=20 p50=%s p95=%s max=%s", len(target.Embedding), mode, samples[9], samples[18], samples[19])
}

func logKnowledgeQueryPlan(t *testing.T, db *gorm.DB, ctx context.Context, query string, args []any, dimension int, requireIndex bool) int {
	t.Helper()
	if query == "" {
		t.Fatal("actual repository search SQL was not captured")
	}
	var plan string
	if err := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec(`SET LOCAL hnsw.iterative_scan = 'strict_order'`).Error; err != nil {
			return err
		}
		return tx.Statement.ConnPool.QueryRowContext(ctx, "EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON) "+query, args...).Scan(&plan)
	}); err != nil {
		t.Fatal(err)
	}
	var compact any
	if err := json.Unmarshal([]byte(plan), &compact); err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(compact)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("forced_ann=%t query_plan=%s", requireIndex, encoded)
	index := fmt.Sprintf("idx_knowledge_version_chunks_vector%d", dimension)
	if requireIndex && !strings.Contains(plan, `"Index Name": "`+index+`"`) {
		t.Fatalf("actual hybrid query did not use %s", index)
	}
	var parsed []struct{ Plan knowledgeANNPlanNode }
	if err := json.Unmarshal([]byte(plan), &parsed); err != nil || len(parsed) != 1 {
		t.Fatalf("invalid query plan: %v", err)
	}
	rows, found := knowledgeANNPlanRows(parsed[0].Plan)
	if !found {
		t.Fatal("actual ANN candidate plan is missing")
	}
	return rows
}

type knowledgeANNPlanNode struct {
	SubplanName string  `json:"Subplan Name"`
	ActualRows  float64 `json:"Actual Rows"`
	Plans       []knowledgeANNPlanNode
}

func knowledgeANNPlanRows(node knowledgeANNPlanNode) (int, bool) {
	if node.SubplanName == "CTE vector_candidates" {
		return int(node.ActualRows), true
	}
	for _, child := range node.Plans {
		if rows, found := knowledgeANNPlanRows(child); found {
			return rows, true
		}
	}
	return 0, false
}

func withKnowledgeANN(t *testing.T, store *dbstore.Store, ctx context.Context, check func(*Repository)) {
	t.Helper()
	if err := store.DB().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		for _, setting := range []string{"enable_seqscan", "enable_bitmapscan", "enable_sort"} {
			if err := tx.Exec("SET LOCAL " + setting + " = off").Error; err != nil {
				return err
			}
		}
		if err := tx.Exec("SET LOCAL jit = off").Error; err != nil {
			return err
		}
		check(New(tx))
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func knowledgeEmbeddingSpaceAndACL(t *testing.T, repo *Repository, store *dbstore.Store, ctx context.Context, principal domainknowledge.PrincipalScope, request domainknowledge.SearchRequest, batch domainknowledge.EmbeddingBatch, doc domainknowledge.Document) {
	t.Helper()
	for _, forbidden := range []domainknowledge.EmbeddingBatch{
		{Model: "other-model", Route: batch.Route, Vectors: batch.Vectors},
		{Model: batch.Model, Route: "other-route", Vectors: batch.Vectors},
	} {
		if items, err := repo.SearchAuthorizedChunks(ctx, principal, request, forbidden, 10); err != nil || len(items) != 0 {
			t.Fatalf("embedding space crossed: %v %+v", err, items)
		}
	}
	if err := store.DB().Exec(`UPDATE ai_knowledge_documents SET acl='{"visibility":"private","users":["owner"]}' WHERE id=?`, doc.ID).Error; err != nil {
		t.Fatal(err)
	}
	if items, err := repo.SearchAuthorizedChunks(ctx, principal, request, batch, 10); err != nil || len(items) != 0 {
		t.Fatalf("revoked vector content returned: %v %+v", err, items)
	}
}

func knowledgeFilteredANNRecall(t *testing.T, repo *Repository, store *dbstore.Store, ctx context.Context, private domainknowledge.Document, near domainknowledge.Chunk, query *string, args *[]any) {
	t.Helper()
	vector, err := knowledgeVector(near.Embedding)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.DB().Exec(`INSERT INTO ai_knowledge_version_chunks(id,document_version_id,ordinal,content,content_hash,location,token_count,embedding_model,embedding_route,embedding)
		SELECT ? || ':private:' || i,?,?+i,'private nearest','private-hash-' || i,'{}',8,'fixture','fixture-route',?::vector FROM generate_series(1,512) i`, near.DocumentVersionID, near.DocumentVersionID, annFixtureRows, vector).Error; err != nil {
		t.Fatal(err)
	}
	allowed, target, principal := seedANNDocument(t, repo, store, ctx, len(near.Embedding))
	allowed.Version, allowed.ContentHash = "allowed-current", "allowed-current-hash"
	target.Embedding[1] = .1
	if err := repo.UpsertDocument(ctx, allowed, []domainknowledge.Chunk{target}); err != nil {
		t.Fatal(err)
	}
	request := domainknowledge.SearchRequest{KnowledgeBaseIDs: []string{private.KnowledgeBaseID, allowed.KnowledgeBaseID}, Query: "no-lexical-match"}
	for _, mismatch := range []string{"model", "route"} {
		doc, chunk, _ := seedANNDocument(t, repo, store, ctx, len(near.Embedding))
		doc.Version, doc.ContentHash = mismatch, mismatch+"-hash"
		if mismatch == "model" {
			chunk.EmbeddingModel = "other-model"
		} else {
			chunk.EmbeddingRoute = "other-route"
		}
		if err := repo.UpsertDocument(ctx, doc, []domainknowledge.Chunk{chunk}); err != nil {
			t.Fatal(err)
		}
		request.KnowledgeBaseIDs = append(request.KnowledgeBaseIDs, doc.KnowledgeBaseID)
	}
	batch := domainknowledge.EmbeddingBatch{Model: near.EmbeddingModel, Route: near.EmbeddingRoute, Vectors: [][]float64{near.Embedding}}
	withKnowledgeANN(t, store, ctx, func(indexed *Repository) {
		for _, setting := range []string{"hnsw.ef_search", "hnsw.max_scan_tuples"} {
			if err := indexed.db.Exec("SET LOCAL " + setting + " = 1").Error; err != nil {
				t.Fatal(err)
			}
		}
		hits, err := indexed.SearchAuthorizedChunks(ctx, principal, request, batch, 50)
		if err != nil || len(hits) != 1 || hits[0].DocumentID != allowed.ID || hits[0].VectorScore < .99 {
			t.Fatalf("selective ANN lost the authorized current target: %v %+v", err, hits)
		}
		validateANNHit(t, indexed, ctx, principal, hits[0])
		assertCapturedKnowledgeBases(t, *args, request.KnowledgeBaseIDs)
		if rows := logKnowledgeQueryPlan(t, indexed.db, ctx, *query, *args, len(near.Embedding), true); rows != 0 {
			t.Fatalf("filtered fixture did not exercise exact fallback: candidates=%d", rows)
		}
	})
}

func assertCapturedKnowledgeBases(t *testing.T, args []any, baseIDs []string) {
	t.Helper()
	for _, id := range baseIDs {
		count := 0
		for _, arg := range args {
			if value, ok := arg.(string); ok && value == id {
				count++
			}
		}
		if count != 2 {
			t.Fatalf("captured query omitted current/legacy knowledge base %s: count=%d", id, count)
		}
	}
}

func validateANNHit(t *testing.T, repo *Repository, ctx context.Context, p domainknowledge.PrincipalScope, hit domainknowledge.Chunk) {
	t.Helper()
	citation := domainknowledge.Citation{KnowledgeBaseID: hit.KnowledgeBaseID, DocumentID: hit.DocumentID, ChunkID: hit.ID, ContentHash: hit.ContentHash, DocumentVersionID: hit.DocumentVersionID}
	if err := repo.ValidateCitations(ctx, p, []domainknowledge.Citation{citation}); err != nil {
		t.Fatalf("ANN citation rejected: %v", err)
	}
}

// Capture the SQL the production method actually executes, avoiding a second query implementation.
func captureKnowledgeSearchQuery(t *testing.T, repo *Repository) (*string, *[]any) {
	t.Helper()
	query := ""
	args := []any{}
	const callback = "knowledge:ann-test-query"
	if err := repo.db.Callback().Row().Before("gorm:row").Register(callback, func(db *gorm.DB) {
		if strings.HasPrefix(db.Statement.SQL.String(), "WITH authorized") && strings.Contains(db.Statement.SQL.String(), ", lexical_raw AS (") {
			query = db.Statement.SQL.String()
			args = append([]any(nil), db.Statement.Vars...)
		}
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := repo.db.Callback().Row().Remove(callback); err != nil {
			t.Error(err)
		}
	})
	return &query, &args
}
