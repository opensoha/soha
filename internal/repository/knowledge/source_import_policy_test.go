package knowledge

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	appaccess "github.com/opensoha/soha/internal/application/access"
	appknowledge "github.com/opensoha/soha/internal/application/knowledge"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainknowledge "github.com/opensoha/soha/internal/domain/knowledge"
	dbstore "github.com/opensoha/soha/internal/infrastructure/db"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

func knowledgeExternalImportScope(t *testing.T, repo *Repository, store *dbstore.Store, ctx context.Context) {
	service, err := appknowledge.New(repo, appaccess.NewPermissionResolver(sourcePolicyPermissions{}), nil, nil, appknowledge.WithSourceLoader(sourcePolicyLoader{}))
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name    string
		policy  any
		allowed string
	}{
		{name: "default-owner", allowed: "owner"},
		{name: "explicit-reader", policy: sourcePolicyConfig(domainknowledge.AccessScope{Visibility: "scoped", Users: []string{"reader"}}), allowed: "reader"},
		{name: "explicit-deny", policy: sourcePolicyConfig(domainknowledge.AccessScope{Visibility: "private"})},
	} {
		t.Run(test.name, func(t *testing.T) {
			now := time.Now().UTC()
			base, err := repo.CreateBase(ctx, domainknowledge.KnowledgeBase{ID: uuid.NewString(), Name: "External policy", OwnerID: "owner", Status: "active", Scope: domainknowledge.AccessScope{Visibility: "public"}, CreatedAt: now, UpdatedAt: now})
			if err != nil {
				t.Fatal(err)
			}
			source := domainknowledge.Source{ID: uuid.NewString(), KnowledgeBaseID: base.ID, Name: "External policy", Kind: "http", Config: map[string]any{}, Status: "ready", CreatedAt: now, UpdatedAt: now}
			if test.policy != nil {
				source.Config["importPolicy"] = test.policy
			}
			if _, err := repo.CreateSource(ctx, domainknowledge.PrincipalScope{UserID: "owner"}, source); err != nil {
				t.Fatal(err)
			}
			if _, err := service.SyncSource(ctx, domainidentity.Principal{UserID: "owner", Roles: []string{"ingester"}}, base.ID, source.ID); err != nil {
				t.Fatal(err)
			}
			assertExternalImportReaders(t, repo, store, ctx, base.ID, source.ID, test.allowed)
		})
	}
}

func assertExternalImportReaders(t *testing.T, repo *Repository, store *dbstore.Store, ctx context.Context, baseID, sourceID, allowed string) {
	t.Helper()
	var citation domainknowledge.Citation
	if err := store.DB().Raw(`SELECT d.id,d.knowledge_base_id,c.id,c.content_hash,v.id FROM ai_knowledge_documents d JOIN ai_knowledge_document_versions v ON v.id=d.current_version_id JOIN ai_knowledge_version_chunks c ON c.document_version_id=v.id WHERE d.source_id=?`, sourceID).Row().Scan(&citation.DocumentID, &citation.KnowledgeBaseID, &citation.ChunkID, &citation.ContentHash, &citation.DocumentVersionID); err != nil || citation.ChunkID == "" {
		t.Fatalf("governed document was not persisted: %v", err)
	}
	request := domainknowledge.SearchRequest{KnowledgeBaseIDs: []string{baseID}, Query: "bounded remote evidence"}
	for _, userID := range []string{"owner", "reader", "intruder"} {
		principal := domainknowledge.PrincipalScope{UserID: userID}
		hits, err := repo.SearchAuthorizedChunks(ctx, principal, request, domainknowledge.EmbeddingBatch{}, 10)
		if err != nil {
			t.Fatal(err)
		}
		if userID == allowed {
			if len(hits) != 1 {
				t.Fatalf("authorized user %s: %d hits", userID, len(hits))
			}
			if err := repo.ValidateCitations(ctx, principal, []domainknowledge.Citation{citation}); err != nil {
				t.Fatal(err)
			}
		} else {
			if len(hits) != 0 {
				t.Fatalf("remote public ACL exposed content to %s", userID)
			}
			if err := repo.ValidateCitations(ctx, principal, []domainknowledge.Citation{citation}); !errors.Is(err, apperrors.ErrAccessDenied) {
				t.Fatalf("remote public ACL exposed citation to %s: %v", userID, err)
			}
		}
	}
}

func sourcePolicyConfig(scope domainknowledge.AccessScope) map[string]any {
	return map[string]any{"schemaVersion": "opensoha.dev/knowledge-source-import-policy/v1", "scope": scope}
}

type sourcePolicyPermissions struct{}

func (sourcePolicyPermissions) ListRolePermissions(context.Context) (map[string][]string, error) {
	return map[string][]string{"ingester": {appknowledge.PermKnowledgeIngestionOperate}}, nil
}

type sourcePolicyLoader struct{}

func (sourcePolicyLoader) Load(context.Context, domainidentity.Principal, domainknowledge.Source) ([]domainknowledge.SourceDocument, string, error) {
	return []domainknowledge.SourceDocument{{ExternalID: "remote", Title: "Remote", Content: "bounded remote evidence", ACL: domainknowledge.AccessScope{Visibility: "public"}}}, "", nil
}
