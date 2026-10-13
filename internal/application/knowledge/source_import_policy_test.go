package knowledge

import (
	"errors"
	"slices"
	"testing"
	"time"

	domainknowledge "github.com/opensoha/soha/internal/domain/knowledge"
)

func TestExternalSourceCannotExpandTrustedImportScope(t *testing.T) {
	owner := domainknowledge.AccessScope{Visibility: "private", Users: []string{"owner"}}
	shared := domainknowledge.AccessScope{Visibility: "scoped", Users: []string{"reader"}, Roles: []string{"allowed-role"}, Teams: []string{"allowed-team"}, Projects: []string{"allowed-project"}}
	for _, kind := range []domainknowledge.SourceKind{domainknowledge.SourceKindHTTP, domainknowledge.SourceKindGit, domainknowledge.SourceKindObject} {
		t.Run(string(kind), func(t *testing.T) {
			for _, test := range []struct {
				name   string
				policy any
				remote domainknowledge.AccessScope
				want   domainknowledge.AccessScope
			}{
				{name: "default-public", remote: domainknowledge.AccessScope{Visibility: "public"}, want: owner},
				{name: "default-empty", want: owner},
				{name: "default-outside-user", remote: domainknowledge.AccessScope{Visibility: "private", Users: []string{"other"}}, want: domainknowledge.AccessScope{Visibility: "private"}},
				{name: "explicit-empty-denies", remote: domainknowledge.AccessScope{Visibility: "private"}, want: domainknowledge.AccessScope{Visibility: "private"}},
				{name: "shared-default", policy: importPolicyFixture(shared), want: shared},
				{name: "shared-public", policy: importPolicyFixture(shared), remote: domainknowledge.AccessScope{Visibility: "public"}, want: shared},
				{name: "shared-intersection", policy: importPolicyFixture(shared), remote: domainknowledge.AccessScope{Visibility: "scoped", Users: []string{"reader", "intruder"}, Roles: []string{"outside-role"}, Teams: []string{"allowed-team"}, Projects: []string{"outside-project"}}, want: domainknowledge.AccessScope{Visibility: "scoped", Users: []string{"reader"}, Teams: []string{"allowed-team"}}},
				{name: "policy-empty-denies", policy: importPolicyFixture(domainknowledge.AccessScope{Visibility: "private"}), remote: domainknowledge.AccessScope{Visibility: "public"}, want: domainknowledge.AccessScope{Visibility: "private"}},
				{name: "policy-public-keeps-private", policy: importPolicyFixture(domainknowledge.AccessScope{Visibility: "public"}), remote: owner, want: owner},
			} {
				t.Run(test.name, func(t *testing.T) {
					source := domainknowledge.Source{ID: "source", KnowledgeBaseID: "base", Kind: kind, Config: map[string]any{}}
					if test.policy != nil {
						source.Config["importPolicy"] = test.policy
					}
					prepared, err := prepareIngestion(source, "owner", []domainknowledge.SourceDocument{{ExternalID: "doc", Content: "bounded", ACL: test.remote}}, "", time.Now)
					if err != nil {
						t.Fatal(err)
					}
					got := prepared.documents[0].ACL
					if !equalImportedScope(got, test.want) {
						t.Fatalf("remote ACL expanded trusted policy: got %+v, want %+v", got, test.want)
					}
					if !equalImportedScope(prepared.chunks[0][0].ACL, got) {
						t.Fatal("chunk ACL differs from governed document")
					}
				})
			}
		})
	}
}

func TestExternalSourceRejectsInvalidImportPolicy(t *testing.T) {
	for _, value := range []any{nil, "public", map[string]any{}, map[string]any{"schemaVersion": "future", "scope": map[string]any{"visibility": "public"}}, map[string]any{"schemaVersion": "opensoha.dev/knowledge-source-import-policy/v1", "scope": map[string]any{"visibility": "private", "users": []string{""}}}, map[string]any{"schemaVersion": "opensoha.dev/knowledge-source-import-policy/v1", "scope": map[string]any{"visibility": "public"}, "unknown": true}} {
		source := domainknowledge.Source{ID: "source", Kind: domainknowledge.SourceKindHTTP, Config: map[string]any{"importPolicy": value}}
		if _, err := prepareIngestion(source, "owner", []domainknowledge.SourceDocument{{ExternalID: "doc", Content: "bounded"}}, "", time.Now); !errors.Is(err, domainknowledge.ErrInvalidInput) {
			t.Fatalf("invalid import policy accepted: %v", err)
		}
	}
}

func importPolicyFixture(scope domainknowledge.AccessScope) map[string]any {
	return map[string]any{"schemaVersion": "opensoha.dev/knowledge-source-import-policy/v1", "scope": scope}
}

func equalImportedScope(a, b domainknowledge.AccessScope) bool {
	return a.Visibility == b.Visibility && slices.Equal(a.Users, b.Users) && slices.Equal(a.Roles, b.Roles) && slices.Equal(a.Teams, b.Teams) && slices.Equal(a.Projects, b.Projects)
}
