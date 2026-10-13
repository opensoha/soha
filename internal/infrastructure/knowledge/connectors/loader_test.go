package connectors

import (
	"context"
	"testing"

	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainknowledge "github.com/opensoha/soha/internal/domain/knowledge"
)

func TestInlineAdapterRejectsUnboundedContent(t *testing.T) {
	t.Parallel()
	_, _, err := (InlineAdapter{}).Load(context.Background(), domainidentity.Principal{}, domainknowledge.Source{Config: map[string]any{"documents": []map[string]any{{"externalId": "doc", "content": string(make([]byte, maxInlineBytes+1))}}}})
	if err == nil {
		t.Fatal("expected inline content limit error")
	}
}

func TestLoaderRejectsUnregisteredNetworkSource(t *testing.T) {
	t.Parallel()
	_, _, err := DefaultLoader().Load(context.Background(), domainidentity.Principal{}, domainknowledge.Source{Kind: domainknowledge.SourceKindHTTP})
	if err == nil {
		t.Fatal("expected unavailable connector error")
	}
}

func TestInlineAdapterRequiresExplicitTombstones(t *testing.T) {
	for _, test := range []struct {
		name      string
		documents []map[string]any
		valid     bool
	}{
		{"empty_delta", []map[string]any{}, true},
		{"explicit_delete", []map[string]any{{"externalId": "doc", "title": "Doc", "content": "", "deleted": true}}, true},
		{"missing_content", []map[string]any{{"externalId": "doc", "content": ""}}, false},
		{"delete_with_content", []map[string]any{{"externalId": "doc", "content": "keep", "deleted": true}}, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			documents, _, err := (InlineAdapter{}).Load(context.Background(), domainidentity.Principal{}, domainknowledge.Source{Config: map[string]any{"documents": test.documents}})
			if (err == nil) != test.valid {
				t.Fatalf("valid=%t err=%v", test.valid, err)
			}
			if test.valid && len(documents) != len(test.documents) {
				t.Fatal("delta documents changed")
			}
			if test.name == "explicit_delete" && !documents[0].Deleted {
				t.Fatal("explicit deletion lost")
			}
		})
	}
}

// HTTP and Git do not declare maxObjects as a configurable fetch budget.
func TestExternalAdapterFetchItemBudgetByKind(t *testing.T) {
	for _, test := range []struct {
		name   string
		kind   domainknowledge.SourceKind
		config map[string]any
		want   int
	}{
		{"http_ignores_object_budget", domainknowledge.SourceKindHTTP, map[string]any{"url": "https://docs.example.com/runbooks", "allowedHosts": []string{"docs.example.com"}, "maxBytes": 1024, "maxObjects": 1000000}, maxInlineDocuments},
		{"git_ignores_object_budget", domainknowledge.SourceKindGit, map[string]any{"repositoryUrl": "https://git.example.com/team/repo", "allowedHosts": []string{"git.example.com"}, "depth": 1, "maxBytes": 1024, "maxObjects": -1}, maxInlineDocuments},
		{"object_uses_validated_budget", domainknowledge.SourceKindObject, map[string]any{"endpoint": "https://objects.example.com", "allowedHosts": []string{"objects.example.com"}, "bucket": "runbooks", "maxBytes": 1024, "maxObjects": 100}, 100},
	} {
		t.Run(test.name, func(t *testing.T) {
			called := false
			loader := ProductionLoader(fetchBudgetFunc(func(request FetchRequest) FetchResult {
				called = true
				if request.MaxItems != test.want {
					t.Fatalf("fetch budget=%d want=%d", request.MaxItems, test.want)
				}
				return FetchResult{}
			}), NewValidator())
			_, _, err := loader.Load(t.Context(), domainidentity.Principal{}, domainknowledge.Source{Kind: test.kind, ConfigRef: "secret:source", Config: test.config})
			if err != nil || !called {
				t.Fatalf("fetch called=%t err=%v", called, err)
			}
		})
	}
}

type fetchBudgetFunc func(FetchRequest) FetchResult

func (f fetchBudgetFunc) Fetch(_ context.Context, request FetchRequest) (FetchResult, error) {
	return f(request), nil
}
