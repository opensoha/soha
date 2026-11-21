package apphosting

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/opensoha/soha/contracts/gen/go/sohaapi"
	application "github.com/opensoha/soha/internal/application/apphosting"
	domain "github.com/opensoha/soha/internal/domain/apphosting"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"github.com/stretchr/testify/require"
)

func testRuntimeConfig(t *testing.T, repo dynamicTestRepository) {
	t.Helper()
	ctx := t.Context()
	now := time.Now().UTC().Truncate(time.Second)
	enableDynamicHostingTest(t, repo)
	app := domain.App{
		ID: "configuration", Name: "Configuration", DNSLabel: "configuration", OwnerUserID: "owner",
		Kind: sohaapi.HostedAppKindDynamic, Status: sohaapi.HostedAppStatusDraft, RouteGeneration: 1,
		Visibility:        sohaapi.HostedAppVisibilityScope{Mode: sohaapi.HostedAppVisibilityModePrivate},
		MaintainerUserIDs: []string{"maintainer", "other"}, CreatedAt: now, UpdatedAt: now,
	}
	_, _, err := repo.CreateApp(ctx, app, 10)
	require.NoError(t, err)
	loaded, err := repo.GetRuntimeConfig(ctx, app.ID)
	require.NoError(t, err)
	require.Equal(t, domain.RuntimeConfig{}, loaded)

	createRevision := func(id string) {
		t.Helper()
		_, err := repo.CreateUploadingRevision(ctx, app.ID, domain.Revision{
			ID: id, AppID: app.ID, Kind: app.Kind, CreatedBy: "maintainer", CreatedAt: now,
		})
		require.NoError(t, err)
	}
	createRevision("database-before-consent")
	require.ErrorIs(t, repo.QueueDynamicRevision(ctx, app.ID, "database-before-consent", "source", "node", true, 1, now), apperrors.ErrAccessDenied)

	initial := domain.RuntimeConfig{
		Environment: map[string]string{"APP_TOKEN": "fake-ciphertext"}, DataWarningBytes: 200,
		VisitorGrant:              domain.VisitorGrant{OwnerID: app.OwnerUserID, Fields: []string{"email"}},
		VisitorRequest:            &domain.VisitorAttributeRequest{ID: "req", OwnerID: app.OwnerUserID, Reason: "Use the visitor department", Fields: []string{"department"}, RequestedAt: now},
		SensitivePublisherIDs:     []string{"maintainer", "other"},
		SensitivePublisherOwnerID: app.OwnerUserID,
	}
	saved, err := repo.MutateRuntimeConfig(ctx, app.ID, func(locked domain.App, _ domain.RuntimeConfig) (domain.RuntimeConfig, error) {
		require.Equal(t, app.OwnerUserID, locked.OwnerUserID)
		return initial, nil
	})
	require.NoError(t, err)
	assertRuntimeConfigReadLock(t, repo, app.ID)
	saved.Environment["APP_TOKEN"] = "edit-returned-map"
	saved.VisitorGrant.Fields[0] = "edit-returned-grant"
	saved.VisitorRequest.Fields[0] = "edit-returned-request"
	saved.SensitivePublisherIDs[0] = "edit-returned-consent"
	_, err = repo.MutateRuntimeConfig(ctx, app.ID, func(_ domain.App, cfg domain.RuntimeConfig) (domain.RuntimeConfig, error) {
		cfg.Environment["APP_TOKEN"] = "edit-rejected-transaction"
		return cfg, apperrors.ErrAccessDenied
	})
	require.ErrorIs(t, err, apperrors.ErrAccessDenied)
	loaded, err = repo.GetRuntimeConfig(ctx, app.ID)
	require.NoError(t, err)
	require.Equal(t, initial, loaded)
	initial.Environment["APP_TOKEN"] = "edit-input-map"
	loaded.Environment["APP_TOKEN"] = "edit-read-map"
	encoded, err := json.Marshal(loaded)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "APP_TOKEN")
	require.NoError(t, repo.QueueDynamicRevision(ctx, app.ID, "database-before-consent", "source", "node", true, 1, now))
	draft, err := repo.GetRevision(ctx, app.ID, "database-before-consent")
	require.NoError(t, err)
	require.Equal(t, "fake-ciphertext", draft.RuntimeEnvironment["APP_TOKEN"])
	encoded, err = json.Marshal(draft)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "APP_TOKEN")
	advanceToStarting(t, repo, draft.ID, now, "registry.example.com/app@sha256:"+strings.Repeat("a", 64))
	_, err = repo.ActivateRevision(ctx, application.ActivateRevisionInput{
		AppID: app.ID, RevisionID: draft.ID, ActorUserID: draft.CreatedBy, DynamicClaimID: "ready-" + draft.ID, Now: now,
	})
	require.NoError(t, err)

	for _, maintainers := range [][]string{{"other"}, {"maintainer", "other"}} {
		_, err := repo.MutateApp(ctx, app.ID, func(locked domain.App) (domain.App, error) {
			locked.MaintainerUserIDs = maintainers
			return locked, nil
		})
		require.NoError(t, err)
	}
	createRevision("environment-after-revoke")
	require.ErrorIs(t, repo.QueueDynamicRevision(ctx, app.ID, "environment-after-revoke", "source", "node", false, 0, now), apperrors.ErrAccessDenied)
	_, _, _, err = repo.RollbackRevisionAs(ctx, app.ID, draft.ID, "", draft.CreatedBy, now)
	require.ErrorIs(t, err, apperrors.ErrAccessDenied)
	_, _, _, err = repo.RollbackRevisionAs(ctx, app.ID, draft.ID, "", "other", now)
	require.NoError(t, err)
	loaded, err = repo.GetRuntimeConfig(ctx, app.ID)
	require.NoError(t, err)
	require.Equal(t, []string{"other"}, loaded.SensitivePublisherIDs)

	for _, newOwner := range []string{"new-owner", app.OwnerUserID} {
		_, _, err := repo.TransferAppOwnership(ctx, app.ID, newOwner, now)
		require.NoError(t, err)
	}
	loaded, err = repo.GetRuntimeConfig(ctx, app.ID)
	require.NoError(t, err)
	require.Empty(t, loaded.VisitorGrant)
	require.Nil(t, loaded.VisitorRequest)
	require.Empty(t, loaded.SensitivePublisherIDs)
	require.Empty(t, loaded.SensitivePublisherOwnerID)
	require.Equal(t, "fake-ciphertext", loaded.Environment["APP_TOKEN"])
	require.EqualValues(t, 200, loaded.DataWarningBytes)

	_, _, err = repo.DeleteApp(ctx, app.ID, nil)
	require.NoError(t, err)
	loaded, err = repo.GetRuntimeConfig(ctx, app.ID)
	require.NoError(t, err)
	require.Equal(t, domain.RuntimeConfig{}, loaded)
}

func assertRuntimeConfigReadLock(t *testing.T, repo dynamicTestRepository, appID string) {
	t.Helper()
	ctx := t.Context()
	err := repo.WithLockedRuntimeConfig(ctx, appID, func(app domain.App, config domain.RuntimeConfig) error {
		require.Equal(t, appID, app.ID)
		require.Equal(t, "fake-ciphertext", config.Environment["APP_TOKEN"])
		switch repo := repo.(type) {
		case *MemoryRepository:
			if repo.mu.TryLock() {
				repo.mu.Unlock()
				t.Fatal("callback ran outside the app mutation mutex")
			}
		case *PostgresRepository:
			var id string
			err := repo.db.WithContext(ctx).Raw(`SELECT id FROM public.hosted_apps WHERE id = ? FOR UPDATE NOWAIT`, appID).Row().Scan(&id)
			var pgErr *pgconn.PgError
			require.ErrorAs(t, err, &pgErr)
			require.Equal(t, "55P03", pgErr.Code)
		}
		config.Environment["APP_TOKEN"] = "edit-read-callback"
		return apperrors.ErrAccessDenied
	})
	require.ErrorIs(t, err, apperrors.ErrAccessDenied)
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	err = repo.WithLockedRuntimeConfig(canceled, appID, func(domain.App, domain.RuntimeConfig) error {
		t.Fatal("canceled handoff entered the callback")
		return nil
	})
	require.ErrorIs(t, err, context.Canceled)
}
