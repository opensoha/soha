package application

import (
	"context"
	"slices"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	domainapp "github.com/opensoha/soha/internal/domain/application"
)

func TestListLoadsApplicationRelationsWithoutPerItemQueries(t *testing.T) {
	repo, mock := newApplicationRepository(t)
	rows := sqlmock.NewRows([]string{
		"id", "name", "app_key", "app_group", "business_line_id", "language", "description", "owner_team", "repository_provider",
		"repository_project_id", "repository_path", "default_branch", "default_tag", "build_image", "build_context_dir",
		"dockerfile_path", "enabled", "metadata", "created_at", "updated_at", "version", "build_sources", "repository_ids",
	})
	now := time.Date(2026, 9, 16, 1, 2, 3, 0, time.UTC)
	for _, fixture := range []struct{ id, image, sources, repositories string }{
		{"configured", "", `[{"id":"source-1","name":"Build","type":"repo_buildpacks","enabled":false,"isDefault":true,"config":{"flag":false,"count":0}}]`, `["repo-1","repo-2"]`},
		{"legacy", "registry.local/legacy", `[]`, `[]`},
		{"empty", "", `[]`, `[]`},
	} {
		rows.AddRow(fixture.id, fixture.id, fixture.id, "group", nil, "go", nil, nil, nil, nil, nil, nil, nil,
			fixture.image, nil, nil, true, `{}`, now, now, 3, fixture.sources, fixture.repositories)
	}
	mock.ExpectBegin()
	mock.ExpectQuery(`(?s)^SELECT .*FROM applications`).WithArgs(100).WillReturnRows(rows).RowsWillBeClosed()
	mock.ExpectCommit()
	items, err := repo.List(context.Background(), domainapp.Filter{})
	if err != nil || len(items) != 3 {
		t.Fatalf("list returned %#v, %v", items, err)
	}
	configured := items[0]
	if len(configured.BuildSources) != 1 || configured.BuildSources[0].Enabled || !configured.BuildSources[0].IsDefault || configured.BuildSources[0].Config["flag"] != false || configured.BuildSources[0].Config["count"] != float64(0) {
		t.Fatalf("build source values lost: %#v", configured.BuildSources)
	}
	if !slices.Equal(configured.RepositoryIDs, []string{"repo-1", "repo-2"}) || configured.Version != 3 {
		t.Fatalf("repository links or version lost: %#v", configured)
	}
	if len(items[1].BuildSources) != 1 || items[1].BuildSources[0].ID != "default:legacy" || items[1].BuildSources[0].BuildImage != "registry.local/legacy" {
		t.Fatalf("legacy fallback lost: %#v", items[1])
	}
	if items[2].BuildSources == nil || len(items[2].BuildSources) != 0 || items[2].RepositoryIDs == nil || len(items[2].RepositoryIDs) != 0 {
		t.Fatalf("empty relations changed: %#v", items[2])
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
