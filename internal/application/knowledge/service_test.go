package knowledge

import (
	"context"
	"fmt"
	"testing"
	"time"

	appaccess "github.com/opensoha/soha/internal/application/access"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainknowledge "github.com/opensoha/soha/internal/domain/knowledge"
)

type testRoleReader struct{ permissions map[string][]string }

func (r testRoleReader) ListRolePermissions(context.Context) (map[string][]string, error) {
	return r.permissions, nil
}

type searchRepository struct {
	Repository
	chunks      []domainknowledge.Chunk
	calls       int
	policies    map[string]domainknowledge.RetrievalPolicy
	policyCalls int
	policyErr   error
	request     domainknowledge.SearchRequest
	limit       int
}

func (r *searchRepository) ListRetrievalPolicies(_ context.Context, _ domainknowledge.PrincipalScope, ids []string) (map[string]domainknowledge.RetrievalPolicy, error) {
	r.policyCalls++
	if r.policyErr != nil {
		return nil, r.policyErr
	}
	policies := r.policies
	if policies == nil {
		policies = make(map[string]domainknowledge.RetrievalPolicy)
		for _, chunk := range r.chunks {
			policies[chunk.KnowledgeBaseID] = domainknowledge.RetrievalPolicy{}
		}
	}
	selected := make(map[string]domainknowledge.RetrievalPolicy)
	for _, id := range ids {
		if policy, ok := policies[id]; ok {
			selected[id] = policy
		}
	}
	return selected, nil
}

func (r *searchRepository) ListAuthorizedChunks(_ context.Context, _ domainknowledge.PrincipalScope, request domainknowledge.SearchRequest, limit int) ([]domainknowledge.Chunk, error) {
	r.calls++
	r.request, r.limit = request, limit
	return append([]domainknowledge.Chunk(nil), r.chunks...), nil
}

func TestServiceSearchBuildsHybridHitsAndCitations(t *testing.T) {
	t.Parallel()
	repo := &searchRepository{chunks: []domainknowledge.Chunk{{ID: "chunk-1", KnowledgeBaseID: "base-1", DocumentID: "doc-1", DocumentTitle: "Runbook", Content: "restart deployment after checking readiness probe", ContentHash: "hash", Location: domainknowledge.SourceLocation{URI: "kb://runbook"}, CreatedAt: time.Now()}}}
	permissions := appaccess.NewPermissionResolver(testRoleReader{permissions: map[string][]string{"reader": {appaccess.PermAIKnowledgeView}}})
	service, err := New(repo, permissions, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	result, err := service.Search(context.Background(), domainidentity.Principal{UserID: "u1", Roles: []string{"reader"}}, domainknowledge.SearchRequest{KnowledgeBaseIDs: []string{"base-1"}, Query: "deployment readiness", TopK: 3})
	if err != nil {
		t.Fatal(err)
	}
	if repo.calls != 1 {
		t.Fatalf("authorized repository calls=%d, want 1", repo.calls)
	}
	if len(result.Hits) != 1 || len(result.Citations) != 1 {
		t.Fatalf("hits/citations=%d/%d, want 1/1", len(result.Hits), len(result.Citations))
	}
	if result.Hits[0].Citation.ID != "citation:chunk-1" {
		t.Fatalf("citation=%q", result.Hits[0].Citation.ID)
	}
	if result.NoAnswer {
		t.Fatal("expected grounded answer")
	}
}

func TestServiceSearchRejectsBeforeRepositoryAccess(t *testing.T) {
	t.Parallel()
	repo := &searchRepository{}
	permissions := appaccess.NewPermissionResolver(testRoleReader{permissions: map[string][]string{"reader": {}}})
	service, err := New(repo, permissions, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	_, err = service.Search(context.Background(), domainidentity.Principal{UserID: "u1", Roles: []string{"reader"}}, domainknowledge.SearchRequest{KnowledgeBaseIDs: []string{"base-1"}, Query: "secret"})
	if err == nil {
		t.Fatal("expected access error")
	}
	if repo.calls != 0 || repo.policyCalls != 0 {
		t.Fatalf("repository called %d/%d times before authorization", repo.calls, repo.policyCalls)
	}
}

func TestServiceSearchHonorsRequestedTopK(t *testing.T) {
	for _, test := range []struct {
		topK int
		want int
	}{{topK: 0, want: 5}, {topK: 1, want: 1}, {topK: 3, want: 3}, {topK: 50, want: 50}, {topK: 100, want: 50}} {
		t.Run(fmt.Sprint(test.topK), func(t *testing.T) {
			repo := &searchRepository{chunks: make([]domainknowledge.Chunk, 60), policies: map[string]domainknowledge.RetrievalPolicy{"base": {MaxTopK: 50}}}
			for i := range repo.chunks {
				repo.chunks[i] = domainknowledge.Chunk{ID: fmt.Sprint(i), KnowledgeBaseID: "base", Content: "deployment readiness"}
			}
			permissions := appaccess.NewPermissionResolver(testRoleReader{permissions: map[string][]string{"reader": {PermKnowledgeView}}})
			service, err := New(repo, permissions, nil, nil)
			if err != nil {
				t.Fatal(err)
			}
			result, err := service.Search(t.Context(), domainidentity.Principal{UserID: "u1", Roles: []string{"reader"}}, domainknowledge.SearchRequest{
				KnowledgeBaseIDs: []string{"base"}, Query: "deployment", TopK: test.topK,
			})
			if err != nil {
				t.Fatal(err)
			}
			if len(result.Hits) != test.want || len(result.Citations) != test.want {
				t.Fatalf("hits/citations = %d/%d, want %d", len(result.Hits), len(result.Citations), test.want)
			}
		})
	}
}

func TestChunkDocumentIsBoundedAndOverlapping(t *testing.T) {
	t.Parallel()
	content := make([]rune, defaultChunkRunes+300)
	for i := range content {
		content[i] = 'a'
	}
	document := domainknowledge.Document{ID: "doc", KnowledgeBaseID: "base", Title: "title", UpdatedAt: time.Now()}
	chunks := chunkDocument(document, string(content))
	if len(chunks) != 2 {
		t.Fatalf("chunks=%d, want 2", len(chunks))
	}
	if chunks[0].Location.EndByte <= chunks[1].Location.StartByte {
		t.Fatal("expected bounded overlap")
	}
	if chunks[0].ContentHash == "" || chunks[1].TokenCount == 0 {
		t.Fatal("expected hashes and token estimates")
	}
}
