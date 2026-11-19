package knowledge

import (
	"context"
	"testing"

	"github.com/google/uuid"
	domainknowledge "github.com/opensoha/soha/internal/domain/knowledge"
)

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
