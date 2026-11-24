package apphosting

import (
	"context"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/opensoha/soha/contracts/gen/go/sohaapi"
	"github.com/opensoha/soha/internal/application/access"
	appapphosting "github.com/opensoha/soha/internal/application/apphosting"
	domainapphosting "github.com/opensoha/soha/internal/domain/apphosting"
	"github.com/opensoha/soha/internal/platform/apperrors"
	policyrepo "github.com/opensoha/soha/internal/repository/policy"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

type dynamicTestRepository interface {
	appapphosting.Repository
	GetRuntimeConfig(context.Context, string) (domainapphosting.RuntimeConfig, error)
	MutateRuntimeConfig(context.Context, string, func(domainapphosting.App, domainapphosting.RuntimeConfig) (domainapphosting.RuntimeConfig, error)) (domainapphosting.RuntimeConfig, error)
	WithLockedRuntimeConfig(context.Context, string, func(domainapphosting.App, domainapphosting.RuntimeConfig) error) error
	QueueDynamicRevision(context.Context, string, string, string, string, bool, int, time.Time) error
	ClaimDynamicRevision(context.Context, string, time.Time, time.Duration) (domainapphosting.Revision, error)
	AdvanceDynamicRevision(context.Context, string, string, string, string, time.Time) error
	FailDynamicRevision(context.Context, string, string, string, string) error
	CompleteDynamicCleanup(context.Context, string, string) error
	ClaimDeletedDynamicRevision(context.Context, string, time.Time, time.Duration) (domainapphosting.Revision, error)
	CompleteDeletedDynamicCleanup(context.Context, string, string) error
	RequeueDeletedDynamicCleanup(context.Context, domainapphosting.Revision) error
	RollbackRevisionAs(context.Context, string, string, string, string, time.Time) (domainapphosting.App, domainapphosting.Revision, bool, error)
}

func TestMemoryDynamicRevisionLifecycle(t *testing.T) {
	t.Parallel()
	testDynamicRevisionLifecycle(t, NewMemoryRepository())
}

func TestMemoryDeletedDynamicRuntimeCleanup(t *testing.T) {
	t.Parallel()
	testDeletedDynamicRuntimeCleanup(t, NewMemoryRepository())
}

func TestMemoryRuntimeConfig(t *testing.T) {
	t.Parallel()
	testRuntimeConfig(t, NewMemoryRepository())
}

func TestDynamicDataCandidateStillRequiresOwnerAtActivation(t *testing.T) {
	app := domainapphosting.App{
		OwnerUserID: "new-owner", MaintainerUserIDs: []string{"uploader"}, Status: sohaapi.HostedAppStatusOnline,
	}
	input := appapphosting.ActivateRevisionInput{ActorUserID: "uploader"}
	_, err := revisionActivationRejection(app, domainapphosting.Revision{Database: true}, input, domainapphosting.RuntimeConfig{})
	assertRevisionError(t, err, apperrors.ErrAccessDenied)
	_, err = revisionActivationRejection(app, domainapphosting.Revision{}, input, domainapphosting.RuntimeConfig{})
	assertRevisionError(t, err, nil)
}

func TestPostgresDynamicRevisionLifecycle(t *testing.T) {
	socketDir := os.Getenv("SOHA_APP_HOSTING_TEST_POSTGRES_SOCKET_DIR")
	if socketDir == "" {
		t.Skip("assign a disposable local PostgreSQL instance through SOHA_APP_HOSTING_TEST_POSTGRES_SOCKET_DIR")
	}
	requirePostgresTestSocket(t, socketDir)
	conn := url.URL{Scheme: "postgres", Path: "/apphosting_test", RawQuery: url.Values{
		"host": {socketDir}, "port": {"5432"}, "user": {"apphosting_test"}, "sslmode": {"disable"},
	}.Encode()}
	db, err := gorm.Open(postgres.Open(conn.String()), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	migrations, err := os.OpenRoot("../../../migrations/postgres")
	require.NoError(t, err)
	t.Cleanup(func() { _ = migrations.Close() })
	applyMigration := func(name string) {
		t.Helper()
		ddl, err := migrations.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		if err := db.WithContext(t.Context()).Exec(string(ddl)).Error; err != nil {
			t.Fatal(err)
		}
	}
	applyMigration("0154_app_hosting.sql")
	applyMigration("0155_app_hosting_b2.sql")
	if err := db.Exec(`
		INSERT INTO public.hosted_apps (id, name, dns_label, owner_user_id) VALUES ('legacy', 'Legacy', 'legacy', 'author');
		INSERT INTO public.hosted_app_revisions (id, app_id, revision_number, status, created_by)
		VALUES ('legacy-rev', 'legacy', 1, 'ready', 'author');
	`).Error; err != nil {
		t.Fatal(err)
	}
	applyMigration("0156_app_hosting_b3.sql")
	applyMigration("0157_app_hosting_runtime_controls.sql")
	applyMigration("0158_app_hosting_publication_visibility.sql")
	applyMigration("0159_app_hosting_static_artifact_cleanup.sql")
	applyMigration("0160_app_hosting_managed_images.sql")
	applyMigration("0161_app_hosting_governance.sql")
	repo, err := NewPostgresRepository(db)
	if err != nil {
		t.Fatal(err)
	}
	legacy, err := repo.GetRevision(t.Context(), "legacy", "legacy-rev")
	if err != nil || legacy.Kind != sohaapi.HostedAppKindStatic || legacy.BuildPhase != "" || legacy.ImageRef != "" || legacy.Database || legacy.DataSchema != 0 || legacy.RuntimeEnvironment != nil || legacy.PublishVisibility != nil {
		t.Fatalf("legacy static revision after migration = %#v, %v", legacy, err)
	}
	t.Run("governance", func(t *testing.T) { testPostgresGovernance(t, repo) })
	testDynamicRevisionLifecycle(t, repo)
	t.Run("publication-visibility", func(t *testing.T) { testPostgresPublicationVisibility(t, repo) })
	t.Run("static-artifact-cleanup", func(t *testing.T) { testPostgresStaticArtifactCleanup(t, repo) })
	t.Run("managed-image-cleanup", func(t *testing.T) { testPostgresManagedImageCleanup(t, repo) })
	t.Run("deleted-runtime-cleanup", func(t *testing.T) { testDeletedDynamicRuntimeCleanup(t, repo) })
	t.Run("runtime-config", func(t *testing.T) { testRuntimeConfig(t, repo) })
	t.Run("runtime-authority-with-one-connection", func(t *testing.T) {
		sqlDB.SetMaxOpenConns(1)
		defer sqlDB.SetMaxOpenConns(0)
		testTransactionBoundRuntimeAuthority(t, db, repo)
	})
}

func requirePostgresTestSocket(t *testing.T, socketDir string) {
	t.Helper()
	require.True(t, filepath.IsAbs(socketDir), "expected an absolute PostgreSQL socket directory")
	root, err := os.OpenRoot(socketDir)
	require.NoError(t, err)
	defer func() { _ = root.Close() }()
	socket, err := root.Stat(".s.PGSQL.5432")
	require.NoError(t, err)
	require.NotZero(t, socket.Mode()&os.ModeSocket, "expected a disposable PostgreSQL Unix socket at %q", socketDir)
}

func testDeletedDynamicRuntimeCleanup(t *testing.T, repo dynamicTestRepository) {
	t.Helper()
	ctx := t.Context()
	now := time.Now().UTC().Truncate(time.Microsecond)
	app := domainapphosting.App{
		ID: "removed-app", DNSLabel: "removed-app", Name: "Removed app", OwnerUserID: "author", Kind: sohaapi.HostedAppKindDynamic,
		Visibility: sohaapi.HostedAppVisibilityScope{Mode: sohaapi.HostedAppVisibilityModePrivate},
		Status:     sohaapi.HostedAppStatusDraft, RouteGeneration: 1, CreatedAt: now, UpdatedAt: now,
	}
	_, _, err := repo.CreateApp(ctx, app, 10)
	require.NoError(t, err)
	_, err = repo.MutateRuntimeConfig(ctx, app.ID, func(_ domainapphosting.App, cfg domainapphosting.RuntimeConfig) (domainapphosting.RuntimeConfig, error) {
		cfg.Environment = map[string]string{"APP_TOKEN": "secret-ciphertext"}
		return cfg, nil
	})
	require.NoError(t, err)
	rev, err := repo.CreateUploadingRevision(ctx, app.ID, domainapphosting.Revision{
		ID: "removed-revision", AppID: app.ID, Kind: app.Kind, StorageIntegrationID: "store", StoragePrefix: "prefix", CreatedBy: app.OwnerUserID,
		CreatedAt: now.Add(-appapphosting.DynamicSourceCleanupDelay),
	})
	require.NoError(t, err)
	require.NoError(t, repo.QueueDynamicRevision(ctx, app.ID, rev.ID, "prefix/source", "node", true, 2, now))
	_, err = repo.CreateUploadingRevision(ctx, app.ID, domainapphosting.Revision{
		ID: "upload-in-flight", AppID: app.ID, Kind: app.Kind, StorageIntegrationID: "store", StoragePrefix: "prefix", CreatedBy: app.OwnerUserID, CreatedAt: now,
	})
	require.NoError(t, err)
	active, err := repo.ClaimDynamicRevision(ctx, "running", now, time.Minute)
	require.NoError(t, err)
	require.Equal(t, rev.ID, active.ID)
	require.Equal(t, "secret-ciphertext", active.RuntimeEnvironment["APP_TOKEN"])
	_, err = repo.CreateUploadingRevision(ctx, app.ID, domainapphosting.Revision{
		ID: "old-terminal", AppID: app.ID, Kind: app.Kind, StorageIntegrationID: "store", StoragePrefix: "prefix", CreatedBy: app.OwnerUserID,
		CreatedAt: now.Add(-appapphosting.DynamicSourceCleanupDelay),
	})
	require.NoError(t, err)
	require.NoError(t, repo.QueueDynamicRevision(ctx, app.ID, "old-terminal", "prefix/terminal-source", "node", true, 2, now))
	terminal, err := repo.ClaimDynamicRevision(ctx, "old-failure", now, time.Minute)
	require.NoError(t, err)
	require.Equal(t, "old-terminal", terminal.ID)
	require.NoError(t, repo.FailDynamicRevision(ctx, terminal.ID, terminal.ClaimID, "queued", "build rejected"))
	require.NoError(t, repo.CompleteDynamicCleanup(ctx, terminal.ID, terminal.ClaimID))
	_, err = repo.CreateUploadingRevision(ctx, app.ID, domainapphosting.Revision{
		ID: "young-queued", AppID: app.ID, Kind: app.Kind, StorageIntegrationID: "store", StoragePrefix: "prefix", CreatedBy: app.OwnerUserID, CreatedAt: now,
	})
	require.NoError(t, err)
	require.NoError(t, repo.QueueDynamicRevision(ctx, app.ID, "young-queued", "prefix/queued-source", "node", true, 2, now))
	_, _, err = repo.DeleteApp(ctx, app.ID, func(domainapphosting.App) error { return apperrors.ErrAccessDenied })
	require.ErrorIs(t, err, apperrors.ErrAccessDenied)
	_, _, err = repo.DeleteApp(ctx, app.ID, nil)
	require.NoError(t, err)
	_, err = repo.GetAppByID(ctx, app.ID)
	require.ErrorIs(t, err, apperrors.ErrNotFound)
	_, err = repo.GetRevision(ctx, app.ID, rev.ID)
	require.ErrorIs(t, err, apperrors.ErrNotFound)
	assertDeletedDynamicCleanupRetry(t, repo, app.ID, active, now)

	lateUploadDue := now.Add(appapphosting.DynamicSourceCleanupDelay)
	_, err = repo.ClaimDeletedDynamicRevision(ctx, "upload-still-possible", lateUploadDue.Add(-time.Second), time.Minute)
	require.ErrorIs(t, err, apperrors.ErrNotFound)
	work, err := repo.ClaimDeletedDynamicRevision(ctx, "upload-expired", lateUploadDue, time.Minute)
	require.NoError(t, err)
	require.Equal(t, "upload-in-flight", work.ID)
	require.Empty(t, work.SourceKey)
	require.NoError(t, repo.CompleteDeletedDynamicCleanup(ctx, work.ID, work.ClaimID))
	work, err = repo.ClaimDeletedDynamicRevision(ctx, "queued-upload-expired", lateUploadDue, time.Minute)
	require.NoError(t, err)
	require.Equal(t, "young-queued", work.ID)
	require.Equal(t, "prefix/queued-source", work.SourceKey)
	require.NoError(t, repo.CompleteDeletedDynamicCleanup(ctx, work.ID, work.ClaimID))
}

func assertDeletedDynamicCleanupRetry(t *testing.T, repo dynamicTestRepository, appID string, active domainapphosting.Revision, now time.Time) {
	t.Helper()
	ctx := t.Context()
	oldDue := now.Add(30 * time.Second)
	old, err := repo.ClaimDeletedDynamicRevision(ctx, "older-terminal-even-after-cleanup", oldDue, time.Minute)
	require.NoError(t, err)
	require.Equal(t, "old-terminal", old.ID)
	require.Equal(t, "prefix/terminal-source", old.SourceKey)
	require.NoError(t, repo.CompleteDeletedDynamicCleanup(ctx, old.ID, old.ClaimID))
	_, err = repo.ClaimDeletedDynamicRevision(ctx, "too-early", oldDue, time.Minute)
	require.ErrorIs(t, err, apperrors.ErrNotFound)
	claimTime := now.Add(time.Minute)
	work, err := repo.ClaimDeletedDynamicRevision(ctx, "cleanup", claimTime, time.Minute)
	require.NoError(t, err)
	require.Equal(t, active.ID, work.ID)
	require.Equal(t, appID, work.AppID)
	require.Equal(t, "store", work.StorageIntegrationID)
	require.Equal(t, "prefix", work.StoragePrefix)
	require.Equal(t, "prefix/source", work.SourceKey)
	require.True(t, work.Database)
	require.Empty(t, work.RuntimeEnvironment)
	require.NoError(t, repo.RequeueDeletedDynamicCleanup(ctx, active))
	require.ErrorIs(t, repo.CompleteDeletedDynamicCleanup(ctx, active.ID, "cleanup"), apperrors.ErrConflict)
	work, err = repo.ClaimDeletedDynamicRevision(ctx, "retry", claimTime, time.Minute)
	require.NoError(t, err)
	require.Empty(t, work.RuntimeEnvironment)
	require.NoError(t, repo.CompleteDeletedDynamicCleanup(ctx, work.ID, work.ClaimID))
	_, err = repo.ClaimDeletedDynamicRevision(ctx, "completed", claimTime, time.Minute)
	require.ErrorIs(t, err, apperrors.ErrNotFound)
}

func testDynamicRevisionLifecycle(t *testing.T, repo dynamicTestRepository) {
	t.Helper()
	ctx := t.Context()
	now := time.Now().UTC().Truncate(time.Microsecond)
	enableDynamicHostingTest(t, repo)
	app := domainapphosting.App{
		ID: "app", Name: "App", DNSLabel: "app", OwnerUserID: "author", Kind: sohaapi.HostedAppKindDynamic,
		Visibility: sohaapi.HostedAppVisibilityScope{Mode: sohaapi.HostedAppVisibilityModePrivate},
		Status:     sohaapi.HostedAppStatusDraft, RouteGeneration: 1, CreatedAt: now, UpdatedAt: now,
	}
	if _, _, err := repo.CreateApp(ctx, app, 10); err != nil {
		t.Fatal(err)
	}
	setEnvironment := func(value string, extra ...string) {
		t.Helper()
		_, err := repo.MutateRuntimeConfig(ctx, app.ID, func(_ domainapphosting.App, cfg domainapphosting.RuntimeConfig) (domainapphosting.RuntimeConfig, error) {
			cfg.Environment = map[string]string{"APP_TOKEN": value}
			if len(extra) > 0 {
				cfg.Environment["EXTRA_TOKEN"] = extra[0]
			}
			return cfg, nil
		})
		require.NoError(t, err)
	}
	setEnvironment("ciphertext-one")
	createQueued := func(id string, kind sohaapi.HostedAppKind, due time.Time) {
		t.Helper()
		_, err := repo.CreateUploadingRevision(ctx, app.ID, domainapphosting.Revision{
			ID: id, AppID: app.ID, Kind: kind, CreatedBy: app.OwnerUserID, CreatedAt: now,
		})
		assertRevisionError(t, err, nil)
		var want error
		if kind == sohaapi.HostedAppKindStatic {
			want = apperrors.ErrConflict
		}
		if id == "older" {
			assertRevisionError(t, repo.QueueDynamicRevision(ctx, app.ID, id, "source/"+id, "node", true, 0, due), apperrors.ErrInvalidArgument)
		}
		assertRevisionError(t, repo.QueueDynamicRevision(ctx, app.ID, id, "source/"+id, "node", true, 2, due), want)
	}
	createQueued("static", sohaapi.HostedAppKindStatic, now)
	createQueued("older", sohaapi.HostedAppKindDynamic, now.Add(-time.Minute))
	setEnvironment("ciphertext-one", "ciphertext-two")
	require.ErrorIs(t, repo.QueueDynamicRevision(ctx, app.ID, "older", "source/older", "node", true, 2, now), apperrors.ErrConflict)
	static, err := repo.GetRevision(ctx, app.ID, "static")
	require.NoError(t, err)
	require.Empty(t, static.RuntimeEnvironment)
	_, err = repo.ClaimDynamicRevision(ctx, "premature", now.Add(-2*time.Minute), time.Minute)
	assertRevisionError(t, err, apperrors.ErrNotFound)
	_, err = repo.ClaimDynamicRevision(ctx, "expired", now.Add(-time.Minute), 30*time.Second)
	assertRevisionError(t, err, nil)
	assertRevisionError(t, repo.FailDynamicRevision(ctx, "older", "expired", "queued", "expired lease"), apperrors.ErrConflict)
	first := claimRevisionConcurrently(t, repo, now)
	assertClaimedRevisionSnapshot(t, repo, app.ID, first)
	assertRevisionError(t, repo.AdvanceDynamicRevision(ctx, first.ID, "expired", "building", "", now), apperrors.ErrConflict)
	image := "registry.example.com/app@sha256:" + strings.Repeat("a", 64)
	assertRevisionError(t, repo.AdvanceDynamicRevision(ctx, first.ID, first.ClaimID, "starting", image, now), apperrors.ErrConflict)
	activate := func(id, claim string) (appapphosting.ActivateRevisionOutput, error) {
		return repo.ActivateRevision(ctx, appapphosting.ActivateRevisionInput{
			AppID: app.ID, RevisionID: id, ActorUserID: app.OwnerUserID, DynamicClaimID: claim, Now: now,
		})
	}
	_, err = activate(first.ID, first.ClaimID)
	assertRevisionError(t, err, apperrors.ErrConflict)

	createQueued("newer", sohaapi.HostedAppKindDynamic, now)
	advanceToStarting(t, repo, "newer", now, image)
	result, err := activate("newer", "ready-newer")
	assertRevisionError(t, err, nil)
	if result.App.ActiveRevisionID != "newer" || result.Revision.Status != sohaapi.HostedAppRevisionStatusReady || result.Revision.ImageRef != image || result.Revision.BuildPhase != "" || result.Revision.ClaimID != "" {
		t.Fatalf("activated revision = %#v", result)
	}
	require.Equal(t, "ciphertext-two", result.Revision.RuntimeEnvironment["EXTRA_TOKEN"])
	assertRevisionError(t, repo.AdvanceDynamicRevision(ctx, first.ID, first.ClaimID, "building", "", now), nil)
	advanceToStarting(t, repo, "older", now, image)
	_, err = activate("older", "ready-older")
	assertRevisionError(t, err, apperrors.ErrConflict)
	older, err := repo.GetRevision(ctx, app.ID, "older")
	if err != nil || older.Status != sohaapi.HostedAppRevisionStatusFailed || older.FailureStage != "activate" || older.ClaimID != "" {
		t.Fatalf("superseded revision = %#v, %v", older, err)
	}
	require.Equal(t, "a newer revision was already activated", older.FailureReason)
	claimAndCompleteDynamicCleanup(t, repo, "older", sohaapi.HostedAppRevisionStatusFailed, now)
	_, _, _, err = repo.RollbackRevision(ctx, app.ID, "newer", "", now)
	assertRevisionError(t, err, apperrors.ErrAccessDenied)
	_, _, _, err = repo.RollbackRevisionAs(ctx, app.ID, "newer", "", "former-author", now)
	assertRevisionError(t, err, apperrors.ErrAccessDenied)
	_, _, _, err = repo.RollbackRevisionAs(ctx, app.ID, "static", "", app.OwnerUserID, now)
	var businessErr *apperrors.BusinessError
	if !errors.As(err, &businessErr) || businessErr.Code() != "revision_kind_mismatch" {
		t.Fatalf("cross-kind rollback = %v, want revision_kind_mismatch", err)
	}
	beforeRevoke, target, _, err := repo.RollbackRevisionAs(ctx, app.ID, "newer", "rollback-key", app.OwnerUserID, now)
	assertRevisionError(t, err, nil)
	if !target.Database || target.DataSchema != 2 || target.ImageRef != image {
		t.Fatalf("rollback target metadata = %#v", target)
	}
	require.Equal(t, "ciphertext-two", target.RuntimeEnvironment["EXTRA_TOKEN"])
	createQueued("revoked", sohaapi.HostedAppKindDynamic, now)
	advanceToStarting(t, repo, "revoked", now, image)
	setEnvironment("ciphertext-one", "ciphertext-three")
	_, err = activate("revoked", "ready-revoked")
	require.ErrorAs(t, err, &businessErr)
	require.Equal(t, "revision_environment_revoked", businessErr.Code())
	revoked, err := repo.GetRevision(ctx, app.ID, "revoked")
	require.NoError(t, err)
	require.Equal(t, sohaapi.HostedAppRevisionStatusFailed, revoked.Status)
	require.Equal(t, "ciphertext-two", revoked.RuntimeEnvironment["EXTRA_TOKEN"])
	claimAndCompleteDynamicCleanup(t, repo, "revoked", sohaapi.HostedAppRevisionStatusFailed, now)
	for _, key := range []string{"", "rollback-key"} {
		_, _, _, err := repo.RollbackRevisionAs(ctx, app.ID, "newer", key, app.OwnerUserID, now)
		require.ErrorAs(t, err, &businessErr)
		require.Equal(t, "revision_environment_revoked", businessErr.Code())
	}
	unchanged, err := repo.GetAppByID(ctx, app.ID)
	require.NoError(t, err)
	require.Equal(t, beforeRevoke.RouteGeneration, unchanged.RouteGeneration)
	createQueued("retained", sohaapi.HostedAppKindDynamic, now)
	advanceToStarting(t, repo, "retained", now, image)
	result, err = repo.ActivateRevision(ctx, appapphosting.ActivateRevisionInput{
		AppID: app.ID, RevisionID: "retained", ActorUserID: app.OwnerUserID, DynamicClaimID: "ready-retained", Now: now, RetainedRevisionsMax: 1,
	})
	require.NoError(t, err)
	require.Equal(t, "retained", result.App.ActiveRevisionID)
	claimAndCompleteDynamicCleanup(t, repo, "newer", sohaapi.HostedAppRevisionStatusEvicted, now)

	createQueued("failed", sohaapi.HostedAppKindDynamic, now)
	rev, err := repo.ClaimDynamicRevision(ctx, "fail", now, time.Minute)
	assertRevisionError(t, err, nil)
	assertRevisionError(t, repo.FailDynamicRevision(ctx, rev.ID, rev.ClaimID, "queued", "build rejected"), nil)
	_, err = repo.ClaimDynamicRevision(ctx, "finished", now, time.Minute)
	assertRevisionError(t, err, apperrors.ErrNotFound)
	claimAndCompleteDynamicCleanup(t, repo, "failed", sohaapi.HostedAppRevisionStatusFailed, now.Add(time.Minute))
}

func assertClaimedRevisionSnapshot(t *testing.T, repo dynamicTestRepository, appID string, claimed domainapphosting.Revision) {
	t.Helper()
	require.Equal(t, "older", claimed.ID)
	require.Equal(t, "source/older", claimed.SourceKey)
	require.Equal(t, "node", claimed.BuildTemplate)
	require.True(t, claimed.Database)
	require.Equal(t, 2, claimed.DataSchema)
	require.Equal(t, "queued", claimed.BuildPhase)
	require.Equal(t, "ciphertext-one", claimed.RuntimeEnvironment["APP_TOKEN"])
	claimed.RuntimeEnvironment["APP_TOKEN"] = "caller-map-edit"
	oldSnapshot, err := repo.GetRevision(t.Context(), appID, claimed.ID)
	require.NoError(t, err)
	require.Equal(t, "ciphertext-one", oldSnapshot.RuntimeEnvironment["APP_TOKEN"])
	require.NotContains(t, oldSnapshot.RuntimeEnvironment, "EXTRA_TOKEN")
}

func enableDynamicHostingTest(t *testing.T, repo dynamicTestRepository) {
	t.Helper()
	_, err := repo.SaveSettings(t.Context(), func(settings domainapphosting.Settings) (domainapphosting.Settings, error) {
		settings.Enabled = true
		return settings, nil
	})
	require.NoError(t, err)
}

func claimAndCompleteDynamicCleanup(t *testing.T, repo dynamicTestRepository, id string, status sohaapi.HostedAppRevisionStatus, now time.Time) {
	t.Helper()
	ctx := t.Context()
	rev, err := repo.ClaimDynamicRevision(ctx, "cleanup-"+id, now, time.Minute)
	require.NoError(t, err)
	require.Equal(t, id, rev.ID)
	require.Equal(t, status, rev.Status)
	require.Equal(t, "source/"+id, rev.SourceKey)
	require.ErrorIs(t, repo.CompleteDynamicCleanup(ctx, rev.ID, "stale-cleanup-claim"), apperrors.ErrConflict)
	require.NoError(t, repo.CompleteDynamicCleanup(ctx, rev.ID, rev.ClaimID))
	stored, err := repo.GetRevision(ctx, rev.AppID, rev.ID)
	require.NoError(t, err)
	require.Equal(t, status, stored.Status)
	require.True(t, stored.NextCheckAt.IsZero())
}

func testTransactionBoundRuntimeAuthority(t *testing.T, db *gorm.DB, repo *PostgresRepository) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	require.NoError(t, db.WithContext(ctx).Exec(`
		SET search_path TO public;
		CREATE TABLE public.users (id text PRIMARY KEY, username text, status text);
		CREATE TABLE public.roles (id text PRIMARY KEY, permission_keys jsonb);
		CREATE TABLE public.user_role_bindings (user_id text, role_id text);
		CREATE TABLE public.user_team_bindings (user_id text, team_id text);
		INSERT INTO public.users VALUES ('author', 'Author', 'active');
		INSERT INTO public.roles VALUES ('publisher', '["apphosting.apps.publish"]');
		INSERT INTO public.user_role_bindings VALUES ('author', 'publisher');
	`).Error)
	audience := NewAudienceDirectory(db)
	permissions := access.NewPermissionResolver(policyrepo.New(db))
	checkAuthority := func(ctx context.Context) error {
		principal, active, err := audience.ResolveUserPrincipal(ctx, "author")
		if err != nil {
			return err
		}
		if !active {
			return apperrors.ErrAccessDenied
		}
		return permissions.Authorize(ctx, principal, access.PermAppHostingAppsPublish)
	}
	require.NoError(t, repo.WithLockedDynamicRuntime(ctx, "app", func(ctx context.Context, _ domainapphosting.App, _ domainapphosting.RuntimeConfig) error {
		return checkAuthority(ctx)
	}))
	now := time.Now().UTC().Truncate(time.Microsecond)
	_, err := repo.CreateUploadingRevision(ctx, "app", domainapphosting.Revision{
		ID: "authority", AppID: "app", Kind: sohaapi.HostedAppKindDynamic, CreatedBy: "author", CreatedAt: now,
	})
	require.NoError(t, err)
	require.NoError(t, repo.QueueDynamicRevision(ctx, "app", "authority", "source/authority", "node", true, 2, now))
	advanceToStarting(t, repo, "authority", now, "registry.example.com/app@sha256:"+strings.Repeat("a", 64))
	_, err = repo.ActivateRevision(ctx, appapphosting.ActivateRevisionInput{
		AppID: "app", RevisionID: "authority", ActorUserID: "author", DynamicClaimID: "ready-authority", Now: now, BeforeActivate: checkAuthority,
	})
	require.NoError(t, err)
}

func claimRevisionConcurrently(t *testing.T, repo dynamicTestRepository, now time.Time) domainapphosting.Revision {
	t.Helper()
	type result struct {
		rev domainapphosting.Revision
		err error
	}
	results := make(chan result, 2)
	var workers sync.WaitGroup
	for _, claim := range []string{"claim-one", "claim-two"} {
		workers.Go(func() {
			rev, err := repo.ClaimDynamicRevision(t.Context(), claim, now, time.Minute)
			results <- result{rev, err}
		})
	}
	workers.Wait()
	close(results)
	var winner domainapphosting.Revision
	for got := range results {
		if got.err == nil {
			if winner.ID != "" {
				t.Fatal("both workers claimed the same revision")
			}
			winner = got.rev
			continue
		}
		assertRevisionError(t, got.err, apperrors.ErrNotFound)
	}
	return winner
}

func advanceToStarting(t *testing.T, repo dynamicTestRepository, id string, now time.Time, image string) {
	t.Helper()
	ctx := t.Context()
	rev, err := repo.ClaimDynamicRevision(ctx, "build-"+id, now, time.Minute)
	if err != nil || rev.ID != id {
		t.Fatalf("building claim = %#v, %v", rev, err)
	}
	if rev.BuildPhase == "queued" {
		assertRevisionError(t, repo.AdvanceDynamicRevision(ctx, id, rev.ClaimID, "building", "", now), nil)
		rev, err = repo.ClaimDynamicRevision(ctx, "inspect-"+id, now, time.Minute)
		assertRevisionError(t, err, nil)
	}
	assertRevisionError(t, repo.AdvanceDynamicRevision(ctx, id, rev.ClaimID, "building", "", now), nil)
	rev, err = repo.ClaimDynamicRevision(ctx, "poll-"+id, now, time.Minute)
	assertRevisionError(t, err, nil)
	next := now.Add(time.Second)
	assertRevisionError(t, repo.AdvanceDynamicRevision(ctx, id, rev.ClaimID, "starting", image, next), nil)
	_, err = repo.ClaimDynamicRevision(ctx, "before-poll-"+id, now, time.Minute)
	assertRevisionError(t, err, apperrors.ErrNotFound)
	rev, err = repo.ClaimDynamicRevision(ctx, "ready-"+id, next, time.Minute)
	if err != nil || rev.ID != id || rev.BuildPhase != "starting" || rev.ImageRef != image {
		t.Fatalf("readiness claim = %#v, %v", rev, err)
	}
}

func assertRevisionError(t *testing.T, err, want error) {
	t.Helper()
	if !errors.Is(err, want) {
		t.Fatalf("error = %v, want %v", err, want)
	}
}
