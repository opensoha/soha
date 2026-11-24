package knowledge

import (
	"context"
	"math"
	"testing"

	"github.com/google/uuid"
	domainknowledge "github.com/opensoha/soha/internal/domain/knowledge"
)

func knowledgeRetrievalCompletesComponents(t *testing.T, repo *Repository, ctx context.Context) {
	for _, direction := range []string{"lexical_only_candidate", "vector_only_candidate"} {
		t.Run(direction, func(t *testing.T) {
			doc, chunks, reader := seedRetrievalDocument(t, repo, ctx, 11)
			for i := range chunks {
				chunks[i].Content, chunks[i].Embedding = "different topic", []float64{.9, math.Sqrt(.19), 0}
			}
			chunks[0].Content, chunks[0].Embedding = "ancient deployment needle", []float64{.8, .6, 0}
			wantLexical, wantVector, minimum := 1.0, .8, .85
			if direction == "vector_only_candidate" {
				for i := 1; i < len(chunks); i++ {
					chunks[i].Content, chunks[i].Embedding = "ancient deployment needle", []float64{.1, math.Sqrt(.99), 0}
				}
				chunks[0].Content, chunks[0].Embedding = "ancient instructions for deployment with a needle recorded in the operational runbook", []float64{1, 0, 0}
				wantLexical, wantVector, minimum = 61.0/71, 1, .9
			}
			publishComponentFixture(t, repo, ctx, &doc, chunks)
			request := domainknowledge.SearchRequest{KnowledgeBaseIDs: []string{doc.KnowledgeBaseID}, Query: "ancient deployment needle"}
			batch := domainknowledge.EmbeddingBatch{Model: "fixture", Source: fixtureEmbeddingSource, Vectors: [][]float64{{1, 0, 0}}}
			for _, threshold := range []float64{0, minimum} {
				request.Policies = map[string]domainknowledge.RetrievalPolicy{doc.KnowledgeBaseID: {LexicalWeight: .5, VectorWeight: .5, MinScore: threshold}}
				rows, err := repo.SearchAuthorizedChunks(ctx, reader, request, batch, 10)
				if err != nil || len(rows) == 0 || rows[0].Content != chunks[0].Content {
					t.Fatalf("threshold %g lost the hybrid winner: rows=%d err=%v", threshold, len(rows), err)
				}
				got := rows[0]
				// pgvector stores vector elements as float32.
				if math.Abs(got.LexicalScore-wantLexical) > 1e-9 || math.Abs(got.VectorScore-wantVector) > 1e-6 || !got.VectorAvailable || got.DocumentVersionID == "" {
					t.Fatalf("threshold %g incomplete components: lexical=%g vector=%g available=%t version=%q", threshold, got.LexicalScore, got.VectorScore, got.VectorAvailable, got.DocumentVersionID)
				}
			}
		})
	}
	t.Run("preserves_existing_lexical_rrf", func(t *testing.T) {
		high, heavy, reader := seedRetrievalDocument(t, repo, ctx, 50)
		low, light, _ := seedRetrievalDocument(t, repo, ctx, 1)
		republishRetrievalFixture(t, repo, ctx, high, heavy, "ancient deployment needle", []float64{0, 1, 0})
		republishRetrievalFixture(t, repo, ctx, low, light, "ancient instructions for deployment with a needle recorded in the operational runbook", nil)
		request := domainknowledge.SearchRequest{
			KnowledgeBaseIDs: []string{high.KnowledgeBaseID, low.KnowledgeBaseID}, Query: "ancient deployment needle",
			Policies: map[string]domainknowledge.RetrievalPolicy{high.KnowledgeBaseID: {VectorWeight: 1}, low.KnowledgeBaseID: {LexicalWeight: 1}},
		}
		batch := domainknowledge.EmbeddingBatch{Model: "fixture", Source: fixtureEmbeddingSource, Vectors: [][]float64{{1, 0, 0}}}
		rows, err := repo.SearchAuthorizedChunks(ctx, reader, request, batch, 10)
		if err != nil || len(rows) == 0 || rows[0].DocumentID != low.ID || math.Abs(rows[0].LexicalScore-61.0/111) > 1e-9 {
			t.Fatalf("known global rank 51 was rescored: rows=%+v err=%v", rows, err)
		}
		request.Policies[high.KnowledgeBaseID] = domainknowledge.RetrievalPolicy{VectorWeight: 1, MinScore: .7}
		request.Policies[low.KnowledgeBaseID] = domainknowledge.RetrievalPolicy{LexicalWeight: 1, MinScore: .7}
		rows, err = repo.SearchAuthorizedChunks(ctx, reader, request, batch, 10)
		if err != nil || len(rows) != 0 {
			t.Fatalf("shortlist rank inflated an existing score past MinScore: rows=%+v err=%v", rows, err)
		}
	})
	t.Run("incompatible_embedding_identity", func(t *testing.T) {
		assertIncompatibleEmbeddingIdentity(t, repo, ctx)
	})
}

func assertIncompatibleEmbeddingIdentity(t *testing.T, repo *Repository, ctx context.Context) {
	t.Helper()
	for _, mismatch := range []string{"model", "route", "upstream", "provider", "upstream_model", "dimension"} {
		t.Run(mismatch, func(t *testing.T) {
			doc, chunks, reader := seedRetrievalDocument(t, repo, ctx, 11)
			for i := range chunks {
				chunks[i].Content, chunks[i].Embedding = "different topic", []float64{1, 0, 0}
			}
			target := chunks[0]
			target.Content = "ancient deployment needle"
			switch mismatch {
			case "model":
				target.EmbeddingModel = "other-model"
			case "route":
				target.EmbeddingRoute = "other-route"
			case "upstream":
				target.EmbeddingSource.UpstreamID = "other-upstream"
			case "provider":
				target.EmbeddingSource.ProviderKind = "other-provider"
			case "upstream_model":
				target.EmbeddingSource.UpstreamModel = "other-model"
			case "dimension":
				target.Embedding = []float64{1, 0}
			}
			publishComponentFixture(t, repo, ctx, &doc, chunks[1:])
			targetDoc := doc
			targetDoc.ID, targetDoc.ExternalID = uuid.NewString(), uuid.NewString()
			target.DocumentID = targetDoc.ID
			publishComponentFixture(t, repo, ctx, &targetDoc, []domainknowledge.Chunk{target})
			request := domainknowledge.SearchRequest{KnowledgeBaseIDs: []string{doc.KnowledgeBaseID}, Query: target.Content,
				Policies: map[string]domainknowledge.RetrievalPolicy{doc.KnowledgeBaseID: {LexicalWeight: .5, VectorWeight: .5, MinScore: .85}}}
			batch := domainknowledge.EmbeddingBatch{Model: "fixture", Source: fixtureEmbeddingSource, Vectors: [][]float64{{1, 0, 0}}}
			rows, err := repo.SearchAuthorizedChunks(ctx, reader, request, batch, 10)
			if err != nil || len(rows) != 0 {
				t.Fatalf("incompatible %s contributed a vector score: rows=%d err=%v", mismatch, len(rows), err)
			}
		})
	}
}

func publishComponentFixture(t *testing.T, repo *Repository, ctx context.Context, document *domainknowledge.Document, chunks []domainknowledge.Chunk) {
	t.Helper()
	document.ContentHash = uuid.NewString()
	document.ChunkCount = len(chunks)
	for i := range chunks {
		chunks[i].ID, chunks[i].ContentHash = uuid.NewString(), document.ContentHash
	}
	if err := repo.UpsertDocument(ctx, *document, chunks); err != nil {
		t.Fatal(err)
	}
}

func knowledgeRetrievalUsesPolicy(t *testing.T, repo *Repository, ctx context.Context) {
	t.Helper()
	document, chunks, reader := seedRetrievalDocument(t, repo, ctx, 12)
	owner := domainknowledge.PrincipalScope{UserID: "owner"}
	base, err := repo.GetBase(ctx, owner, document.KnowledgeBaseID)
	if err != nil {
		t.Fatal(err)
	}
	request := domainknowledge.SearchRequest{KnowledgeBaseIDs: []string{base.ID}, Query: chunks[0].Content}
	embedding := domainknowledge.EmbeddingBatch{
		Model: "fixture", Source: fixtureEmbeddingSource, Vectors: [][]float64{{0, 1, 0}},
	}
	setPolicy := func(policy domainknowledge.RetrievalPolicy) {
		t.Helper()
		base.RetrievalPolicy = policy
		if _, err := repo.UpdateBase(ctx, owner, base); err != nil {
			t.Fatal(err)
		}
	}
	search := func(batch domainknowledge.EmbeddingBatch) []domainknowledge.Chunk {
		t.Helper()
		rows, err := repo.SearchAuthorizedChunks(ctx, reader, request, batch, 10)
		if err != nil {
			t.Fatal(err)
		}
		return rows
	}

	setPolicy(domainknowledge.RetrievalPolicy{LexicalWeight: 1})
	lexical := search(embedding)
	if len(lexical) == 0 {
		t.Fatal("lexical weight produced no candidates")
	}
	first := lexical[0]
	if first.Content != chunks[0].Content || !first.VectorAvailable {
		t.Fatalf("lexical weight did not promote the text match: match=%t, lexical=%g, vector=%g, available=%t",
			first.Content == chunks[0].Content, first.LexicalScore, first.VectorScore, first.VectorAvailable)
	}
	setPolicy(domainknowledge.RetrievalPolicy{VectorWeight: 1, MinScore: 0.9})
	vector := search(embedding)
	if len(vector) != 10 {
		t.Fatalf("vector candidates = %d, want 10", len(vector))
	}
	for _, chunk := range vector {
		if chunk.Content == chunks[0].Content || chunk.VectorScore < 0.9 || !chunk.VectorAvailable {
			t.Fatalf("nonmatching vector survived candidate limit and threshold: %s", chunk.ID)
		}
	}
	fallback := search(domainknowledge.EmbeddingBatch{})
	if len(fallback) != 1 || fallback[0].Content != chunks[0].Content || fallback[0].VectorAvailable {
		t.Fatalf("lexical fallback did not retain the text match; got %d candidates", len(fallback))
	}
}

func knowledgeRetrievalWeightedWindow(t *testing.T, repo *Repository, ctx context.Context) {
	t.Helper()
	high, heavy, reader := seedRetrievalDocument(t, repo, ctx, 12)
	low, light, _ := seedRetrievalDocument(t, repo, ctx, 1)
	request := domainknowledge.SearchRequest{
		KnowledgeBaseIDs: []string{high.KnowledgeBaseID, low.KnowledgeBaseID},
		Query:            "ancient deployment needle",
	}
	embedding := domainknowledge.EmbeddingBatch{
		Model: "fixture", Source: fixtureEmbeddingSource, Vectors: [][]float64{{0, 1, 0}},
	}
	t.Run("lexical", func(t *testing.T) {
		republishRetrievalFixture(t, repo, ctx, high, heavy, "ancient deployment needle", []float64{1, 0, 0})
		republishRetrievalFixture(t, repo, ctx, low, light, "ancient instructions for deployment with a needle recorded in the operational runbook", nil)
		request.Policies = map[string]domainknowledge.RetrievalPolicy{
			high.KnowledgeBaseID: {VectorWeight: 1},
			low.KnowledgeBaseID:  {LexicalWeight: 1},
		}
		assertWeightedDocumentFirst(t, repo, ctx, reader, request, embedding, low.ID)
	})
	t.Run("vector", func(t *testing.T) {
		republishRetrievalFixture(t, repo, ctx, high, heavy, "unrelated text", []float64{0, 1, 0})
		republishRetrievalFixture(t, repo, ctx, low, light, "different topic", []float64{1, 1, 0})
		request.Policies = map[string]domainknowledge.RetrievalPolicy{
			high.KnowledgeBaseID: {LexicalWeight: 1},
			low.KnowledgeBaseID:  {VectorWeight: 1},
		}
		assertWeightedDocumentFirst(t, repo, ctx, reader, request, embedding, low.ID)
	})
}

func knowledgeRetrievalPreservesTruncatedLexicalRank(t *testing.T, repo *Repository, ctx context.Context) {
	t.Helper()
	high, heavy, reader := seedRetrievalDocument(t, repo, ctx, 50)
	low, light, _ := seedRetrievalDocument(t, repo, ctx, 1)
	republishRetrievalFixture(t, repo, ctx, high, heavy, "ancient deployment needle", nil)
	republishRetrievalFixture(t, repo, ctx, low, light, "ancient instructions for deployment with a needle recorded in the operational runbook", []float64{.4, math.Sqrt(.84), 0})
	request := domainknowledge.SearchRequest{
		KnowledgeBaseIDs: []string{high.KnowledgeBaseID, low.KnowledgeBaseID},
		Query:            "ancient deployment needle",
		Policies: map[string]domainknowledge.RetrievalPolicy{
			high.KnowledgeBaseID: {LexicalWeight: 1, MinScore: 1},
			low.KnowledgeBaseID:  {LexicalWeight: .2, VectorWeight: .8},
		},
	}
	batch := domainknowledge.EmbeddingBatch{Model: "fixture", Source: fixtureEmbeddingSource, Vectors: [][]float64{{1, 0, 0}}}
	rows, err := repo.SearchAuthorizedChunks(ctx, reader, request, batch, 10)
	if err != nil || len(rows) != 2 || rows[1].DocumentID != low.ID || math.Abs(rows[1].LexicalScore-61.0/111) > 1e-9 {
		t.Fatalf("vector-admitted candidate lost its known lexical rank: rows=%+v err=%v", rows, err)
	}
	request.Policies[low.KnowledgeBaseID] = domainknowledge.RetrievalPolicy{LexicalWeight: .2, VectorWeight: .8, MinScore: .46}
	rows, err = repo.SearchAuthorizedChunks(ctx, reader, request, batch, 10)
	if err != nil || len(rows) != 1 || rows[0].DocumentID != high.ID {
		t.Fatalf("lexical truncation inflated a candidate past MinScore: rows=%+v err=%v", rows, err)
	}
}

func republishRetrievalFixture(t *testing.T, repo *Repository, ctx context.Context, document domainknowledge.Document, original []domainknowledge.Chunk, content string, vector []float64) {
	t.Helper()
	document.ContentHash = uuid.NewString()
	chunks := append([]domainknowledge.Chunk(nil), original...)
	for i := range chunks {
		chunks[i].ID, chunks[i].ContentHash = uuid.NewString(), document.ContentHash
		chunks[i].Content, chunks[i].Embedding = content, vector
	}
	if err := repo.UpsertDocument(ctx, document, chunks); err != nil {
		t.Fatal(err)
	}
}

func assertWeightedDocumentFirst(t *testing.T, repo *Repository, ctx context.Context, reader domainknowledge.PrincipalScope, request domainknowledge.SearchRequest, embedding domainknowledge.EmbeddingBatch, want string) {
	t.Helper()
	rows, err := repo.SearchAuthorizedChunks(ctx, reader, request, embedding, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) == 0 || rows[0].DocumentID != want {
		t.Fatalf("weighted candidate did not survive the global limit: got %d candidates", len(rows))
	}
}
