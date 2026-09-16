package catalog

import (
	"context"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	domaincatalog "github.com/opensoha/soha/internal/domain/catalog"
)

func TestWorkflowCatalogReadsScopeMetadataWithoutPerDefinitionQueries(t *testing.T) {
	repo, mock := newCatalogRepository(t)
	mock.ExpectQuery("WITH app_scope AS").WillReturnRows(sqlmock.NewRows([]string{"id", "source_kind", "source_id", "name", "context", "enabled", "scopes"}).AddRow(
		"build_source/app-1/build-1", "build_source", "build-1", "Build", "buildkit", true,
		`[{"applicationId":"app-1","applicationName":"API","applicationKey":"api","exists":true}]`,
	))
	items, err := repo.ListWorkflowCatalogCandidates(context.Background())
	if err != nil || len(items) != 1 || len(items[0].AuthorizationScopes) != 1 || items[0].AuthorizationScopes[0].ApplicationID != "app-1" {
		t.Fatalf("catalog = %#v, error = %v", items, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func verifyWorkflowCatalogLegacySources(t *testing.T, ctx context.Context, repo *Repository) {
	t.Helper()
	tx := repo.db.WithContext(ctx).Begin()
	requireTemplateNoError(t, tx.Error)
	defer func() { _ = tx.Rollback().Error }()
	requireTemplateNoError(t, tx.Exec(`
		INSERT INTO applications (id, app_key, name, app_group, language, build_image, dockerfile_path, build_context_dir)
		VALUES ('catalog-legacy', 'catalog-legacy', 'Legacy', '', '', '  legacy/image  ', '', ''),
			('catalog-explicit', 'catalog-explicit', 'Explicit', '', '', 'legacy/image', '', ''),
			('catalog-empty', 'catalog-empty', 'Empty', '', '', '', ' ', '');
		INSERT INTO application_build_sources (id, application_id, source_name, source_type, enabled)
		VALUES ('catalog-source', 'catalog-explicit', 'Explicit source', 'repo_dockerfile', false);
	`).Error)
	items, err := New(tx).ListWorkflowCatalogCandidates(ctx)
	requireTemplateNoError(t, err)
	byID := map[string]domaincatalog.WorkflowCatalogCandidate{}
	for _, item := range items {
		byID[item.ID] = item
	}
	legacy := byID["build_source/catalog-legacy/default:catalog-legacy"]
	if legacy.SourceID != "default:catalog-legacy" || legacy.Name != "Repository Dockerfile" || legacy.Context != "legacy/image" || len(legacy.AuthorizationScopes) != 1 || !legacy.AuthorizationScopes[0].Exists {
		t.Fatalf("legacy source missing or changed: %#v", legacy)
	}
	if _, found := byID["build_source/catalog-explicit/default:catalog-explicit"]; found {
		t.Fatal("explicit source did not suppress legacy fallback")
	}
	if _, found := byID["build_source/catalog-empty/default:catalog-empty"]; found {
		t.Fatal("blank legacy fields created a phantom build source")
	}
	explicit := byID["build_source/catalog-explicit/catalog-source"]
	if explicit.SourceID != "catalog-source" || explicit.Enabled {
		t.Fatalf("explicit disabled source changed: %#v", explicit)
	}
}
