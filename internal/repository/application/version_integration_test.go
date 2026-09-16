package application

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"testing"
	"time"

	"github.com/google/uuid"
	domainapp "github.com/opensoha/soha/internal/domain/application"
	"github.com/opensoha/soha/internal/infrastructure/config"
	dbstore "github.com/opensoha/soha/internal/infrastructure/db"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"go.uber.org/zap"
)

func TestApplicationVersionWithPostgres(t *testing.T) {
	portText := os.Getenv("SOHA_APPLICATION_TEST_POSTGRES_PORT")
	if portText == "" {
		t.Skip("set SOHA_APPLICATION_TEST_POSTGRES_PORT to an isolated PostgreSQL instance")
	}
	port, err := strconv.Atoi(portText)
	requireApplicationIntegrationNoError(t, err)
	store, err := dbstore.New(config.DatabaseConfig{
		Driver: "postgres", Host: "127.0.0.1", Port: port, Name: "soha", User: "pgsql", Password: "test-only", SSLMode: "disable",
		MaxOpenConns: 6, MaxIdleConns: 4, ConnMaxLifetime: time.Minute,
	}, zap.NewNop())
	requireApplicationIntegrationNoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	requireApplicationIntegrationNoError(t, store.MigrateFromFile(ctx, filepath.Join("..", "..", "..", "migrations", "postgres")))
	db := store.DB().WithContext(ctx)
	repo := New(db)
	id := uuid.NewString()
	input := domainapp.UpsertInput{ID: id, Name: "Version test", Key: id, Enabled: true, BuildSources: []domainapp.BuildSourceInput{{ID: id + "-build", Name: "Original", Type: domainapp.BuildSourceTypeRepoDockerfile, Enabled: true}}}
	original, err := repo.Create(ctx, input)
	requireApplicationIntegrationNoError(t, err)
	t.Cleanup(func() { _ = store.DB().Exec(`DELETE FROM applications WHERE id = ?`, id).Error })
	if original.Version != 1 {
		t.Fatalf("initial version = %d", original.Version)
	}

	// Force the second editor's UPDATE to overlap an uncommitted first editor.
	input.ExpectedVersion = &original.Version
	first, second := db.Begin(), db.Begin()
	t.Cleanup(func() { _ = first.Rollback().Error; _ = second.Rollback().Error })
	var lockedVersion int64
	requireApplicationIntegrationNoError(t, first.Raw(`SELECT version FROM applications WHERE id = ? FOR UPDATE`, id).Row().Scan(&lockedVersion))
	var secondPID int
	requireApplicationIntegrationNoError(t, second.Raw(`SELECT pg_backend_pid()`).Row().Scan(&secondPID))
	stale := input
	stale.Name = "Stale editor"
	stale.BuildSources = []domainapp.BuildSourceInput{{ID: id + "-stale", Name: "Stale", Type: domainapp.BuildSourceTypeRepoDockerfile, Enabled: true}}
	result := make(chan error, 1)
	go func() { _, err := New(second).Update(ctx, id, stale); result <- err }()
	for {
		var blocked bool
		requireApplicationIntegrationNoError(t, db.Raw(`SELECT cardinality(pg_blocking_pids(?)) > 0`, secondPID).Row().Scan(&blocked))
		if blocked {
			break
		}
		select {
		case err := <-result:
			t.Fatalf("second writer did not wait: %v", err)
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		case <-time.After(10 * time.Millisecond):
		}
	}
	fresh := input
	fresh.Name = "Winning editor"
	fresh.BuildSources = []domainapp.BuildSourceInput{{ID: id + "-winner", Name: "Winner", Type: domainapp.BuildSourceTypeRepoDockerfile, Enabled: true}}
	winner, err := New(first).Update(ctx, id, fresh)
	requireApplicationIntegrationNoError(t, err)
	requireApplicationIntegrationNoError(t, first.Commit().Error)
	if err := <-result; !errors.Is(err, apperrors.ErrConflict) {
		t.Fatalf("stale writer error = %v", err)
	}
	_ = second.Rollback().Error
	saved, err := repo.Get(ctx, id)
	requireApplicationIntegrationNoError(t, err)
	if saved.Version != winner.Version || saved.Name != fresh.Name || len(saved.BuildSources) != 1 || saved.BuildSources[0].Name != "Winner" {
		t.Fatalf("lost update: %#v", saved)
	}
	list, err := repo.List(ctx, domainapp.Filter{Search: id})
	if err != nil || len(list) != 1 || list[0].Version != saved.Version || len(list[0].BuildSources) != 1 {
		t.Fatalf("list snapshot: %#v, %v", list, err)
	}

	verifyRepositoryAndLegacyApplicationVersions(t, ctx, repo, id, fresh, saved)
}

func requireApplicationIntegrationNoError(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func verifyRepositoryAndLegacyApplicationVersions(t *testing.T, ctx context.Context, repo *Repository, id string, input domainapp.UpsertInput, saved domainapp.App) {
	t.Helper()
	// Repository CRUD is another writer of application repositoryIds.
	source, err := repo.CreateRepository(ctx, domainapp.SourceRepositoryInput{Name: id, Provider: "git", Protocol: "https", URL: "https://example.invalid/test.git", ApplicationIDs: []string{id}})
	requireApplicationIntegrationNoError(t, err)
	t.Cleanup(func() {
		_ = repo.db.WithContext(context.Background()).Exec(`DELETE FROM repositories WHERE id = ?`, source.ID).Error
	})
	linked, err := repo.Get(ctx, id)
	requireApplicationIntegrationNoError(t, err)
	if linked.Version <= saved.Version || len(linked.RepositoryIDs) != 1 || linked.RepositoryIDs[0] != source.ID {
		t.Fatalf("link did not advance version: %#v", linked)
	}
	listed, err := repo.List(ctx, domainapp.Filter{Search: id})
	requireApplicationIntegrationNoError(t, err)
	// PostgreSQL's JSON timestamps and the driver's timestamps may use different
	// locations for the same instant.
	for i := range linked.BuildSources {
		linked.BuildSources[i].CreatedAt = linked.BuildSources[i].CreatedAt.UTC()
		linked.BuildSources[i].UpdatedAt = linked.BuildSources[i].UpdatedAt.UTC()
	}
	for i := range listed {
		for j := range listed[i].BuildSources {
			listed[i].BuildSources[j].CreatedAt = listed[i].BuildSources[j].CreatedAt.UTC()
			listed[i].BuildSources[j].UpdatedAt = listed[i].BuildSources[j].UpdatedAt.UTC()
		}
	}
	if len(listed) != 1 || !reflect.DeepEqual(listed[0], linked) {
		t.Fatalf("list relations differ from detail: list=%#v detail=%#v", listed, linked)
	}
	fresh := input
	fresh.ExpectedVersion = &saved.Version
	if _, err := repo.Update(ctx, id, fresh); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatalf("stale repository array accepted: %v", err)
	}
	if err := repo.DeleteRepository(ctx, source.ID); err != nil {
		t.Fatal(err)
	}
	unlinked, err := repo.Get(ctx, id)
	requireApplicationIntegrationNoError(t, err)
	if unlinked.Version <= linked.Version || len(unlinked.RepositoryIDs) != 0 {
		t.Fatalf("repository delete did not invalidate version: %#v", unlinked)
	}

	// Legacy omission still writes, but invalidates every versioned editor.
	fresh.ExpectedVersion = nil
	legacy, err := repo.Update(ctx, id, fresh)
	requireApplicationIntegrationNoError(t, err)
	fresh.ExpectedVersion = &unlinked.Version
	if legacy.Version <= unlinked.Version {
		t.Fatal("legacy update did not advance version")
	}
	if _, err := repo.Update(ctx, id, fresh); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatalf("legacy update not detected: %v", err)
	}
}
