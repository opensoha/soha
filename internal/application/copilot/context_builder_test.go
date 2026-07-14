package copilot

import (
	"context"
	"testing"

	appaccess "github.com/opensoha/soha/internal/application/access"
	domaincopilot "github.com/opensoha/soha/internal/domain/copilot"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainknowledge "github.com/opensoha/soha/internal/domain/knowledge"
)

type contextRoleReader struct{ permissions map[string][]string }

func (r contextRoleReader) ListRolePermissions(context.Context) (map[string][]string, error) {
	return r.permissions, nil
}

type contextSearcher struct{ result domainknowledge.SearchResult }

func (s contextSearcher) Search(context.Context, domainidentity.Principal, domainknowledge.SearchRequest) (domainknowledge.SearchResult, error) {
	return s.result, nil
}

func TestContextBuilderInspectAppliesEvidenceBudget(t *testing.T) {
	t.Parallel()
	searcher := contextSearcher{result: domainknowledge.SearchResult{TimingMS: 7, Hits: []domainknowledge.SearchHit{{Content: "first evidence", Citation: domainknowledge.Citation{ID: "c1"}}, {Content: "this evidence is deliberately too large for the remaining budget", Citation: domainknowledge.Citation{ID: "c2"}}}}}
	permissions := appaccess.NewPermissionResolver(contextRoleReader{permissions: map[string][]string{"reader": {appaccess.PermAIContextInspect}}})
	builder := NewContextBuilder(searcher, permissions)
	inspection, err := builder.Inspect(context.Background(), domainidentity.Principal{UserID: "u1", Roles: []string{"reader"}}, domaincopilot.ContextBuildInput{Task: domaincopilot.ContextTask{Goal: "diagnose"}, Knowledge: domaincopilot.ContextKnowledgeInput{Enabled: true, KnowledgeBaseIDs: []string{"base"}}, Budgets: domaincopilot.ContextBudgets{MaxInputTokens: 100, MaxEvidenceTokens: 4}})
	if err != nil {
		t.Fatal(err)
	}
	if len(inspection.Envelope.Evidence) != 1 {
		t.Fatalf("evidence=%d, want 1", len(inspection.Envelope.Evidence))
	}
	if len(inspection.Truncations) != 1 {
		t.Fatalf("truncations=%v", inspection.Truncations)
	}
	if inspection.Envelope.ContentHash == "" || inspection.RetrievalTime != 7 {
		t.Fatal("expected stable snapshot metadata")
	}
}
