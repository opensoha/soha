package application

import (
	"context"
	"errors"
	"regexp"
	"slices"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	domainapp "github.com/opensoha/soha/internal/domain/application"
)

func TestListAppliesKeysetInStableOrderAndKeepsSearchGrouped(t *testing.T) {
	for _, test := range []struct {
		name   string
		search string
	}{
		{name: "cursor"},
		{name: "search and cursor", search: " Payment "},
	} {
		t.Run(test.name, func(t *testing.T) {
			repo, mock := newApplicationRepository(t)
			cursor := &domainapp.ListCursor{Group: "group", Name: "same name", ID: "app-0100"}
			where := "WHERE (app_group, name, id) > ($1, $2, $3) ORDER BY app_group ASC, name ASC, id ASC LIMIT $4"
			mock.ExpectBegin()
			if test.search == "" {
				mock.ExpectQuery(`(?s)^SELECT .*FROM applications\s+`+regexp.QuoteMeta(where)).
					WithArgs(cursor.Group, cursor.Name, cursor.ID, 100).
					WillReturnRows(sqlmock.NewRows([]string{"id"})).RowsWillBeClosed()
			} else {
				where = "WHERE (LOWER(name) LIKE $1 OR LOWER(app_key) LIKE $2 OR LOWER(app_group) LIKE $3 OR LOWER(repository_path) LIKE $4) AND (app_group, name, id) > ($5, $6, $7) ORDER BY app_group ASC, name ASC, id ASC LIMIT $8"
				mock.ExpectQuery(`(?s)^SELECT .*FROM applications\s+`+regexp.QuoteMeta(where)).
					WithArgs("%payment%", "%payment%", "%payment%", "%payment%", cursor.Group, cursor.Name, cursor.ID, 100).
					WillReturnRows(sqlmock.NewRows([]string{"id"})).RowsWillBeClosed()
			}
			mock.ExpectCommit()
			items, err := repo.List(t.Context(), domainapp.Filter{Search: test.search, Limit: 100, After: cursor})
			if err != nil || len(items) != 0 {
				t.Fatalf("keyset list = %+v, %v", items, err)
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestListKeysetPropagatesQueryError(t *testing.T) {
	repo, mock := newApplicationRepository(t)
	want := errors.New("read failed")
	mock.ExpectBegin()
	mock.ExpectQuery(`(?s)^SELECT .*FROM applications`).WithArgs("group", "name", "app", 100).WillReturnError(want)
	mock.ExpectRollback()
	items, err := repo.List(t.Context(), domainapp.Filter{Limit: 100, After: &domainapp.ListCursor{Group: "group", Name: "name", ID: "app"}})
	if items != nil || !errors.Is(err, want) {
		t.Fatalf("query error = %+v, %v", items, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

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
