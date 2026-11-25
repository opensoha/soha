package apphosting

import (
	"context"
	"testing"
	"time"

	"github.com/opensoha/soha/contracts/gen/go/sohaapi"
	domain "github.com/opensoha/soha/internal/domain/apphosting"
	"github.com/stretchr/testify/require"
)

func testPostgresGovernance(t *testing.T, repo *PostgresRepository) {
	ctx := t.Context()
	now := time.Now().UTC().Truncate(time.Microsecond)
	defaults, err := repo.GetSettings(ctx)
	require.NoError(t, err)
	require.Nil(t, defaults.Governance)

	governance := defaults.EffectiveGovernance()
	governance.MaxDynamicAppsPerOwner = 8
	governance.RuntimeLimits.MaxReplicas = 3
	governance.QuotaOverrides = []sohaapi.AppHostingQuotaOverride{
		{ID: "finance", Scope: sohaapi.AppHostingQuotaOverrideScopeTeam, MaxDynamicApps: 6},
	}
	_, err = repo.SaveSettings(ctx, func(current domain.Settings) (domain.Settings, error) {
		current.Governance = &governance
		return current, nil
	})
	require.NoError(t, err)
	t.Cleanup(func() {
		_, err := repo.SaveSettings(context.Background(), func(current domain.Settings) (domain.Settings, error) {
			current.Governance = defaults.Governance
			return current, nil
		})
		require.NoError(t, err)
	})
	cfg, err := repo.GetSettings(ctx)
	require.NoError(t, err)
	require.Equal(t, &governance, cfg.Governance)

	app := domain.App{
		ID: "capacity-check", DNSLabel: "capacity-check", Name: "Capacity check",
		OwnerUserID: "capacity-owner", Kind: sohaapi.HostedAppKindDynamic,
		Status: sohaapi.HostedAppStatusDraft, Visibility: sohaapi.HostedAppVisibilityScope{Mode: sohaapi.HostedAppVisibilityModePrivate},
		RouteGeneration: 1, CreatedAt: now, UpdatedAt: now,
	}
	_, _, err = repo.CreateApp(ctx, app, 10)
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, repo.db.Exec("DELETE FROM public.hosted_apps WHERE id = ?", app.ID).Error)
	})
	rev, err := repo.CreateUploadingRevision(ctx, app.ID, domain.Revision{
		ID: "capacity-rev", AppID: app.ID, Kind: app.Kind, CreatedBy: app.OwnerUserID,
		StorageIntegrationID: "store", StoragePrefix: "prefix", CreatedAt: now, RuntimeLimits: &cfg.Governance.RuntimeLimits,
	})
	require.NoError(t, err)
	read, err := repo.GetRevision(ctx, app.ID, rev.ID)
	require.NoError(t, err)
	require.Equal(t, rev.RuntimeLimits, read.RuntimeLimits)
	require.NoError(t, repo.db.Exec("UPDATE public.hosted_app_revisions SET status = 'ready', database_enabled = true, data_schema = 1 WHERE id = ?", rev.ID).Error)
	require.NoError(t, repo.db.Exec("UPDATE public.hosted_apps SET status = 'online', active_revision_id = ? WHERE id = ?", rev.ID, app.ID).Error)

	first, ok, err := repo.ClaimDataCapacityCheck(ctx, "worker-one", now, now.Add(time.Minute))
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, app.ID, first.AppID)
	_, ok, err = repo.ClaimDataCapacityCheck(ctx, "worker-two", now, now.Add(time.Minute))
	require.NoError(t, err)
	require.False(t, ok)
	retry, ok, err := repo.ClaimDataCapacityCheck(ctx, "worker-two", now.Add(time.Minute), now.Add(2*time.Minute))
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, first.WarningID, retry.WarningID)
	require.NoError(t, repo.CompleteDataCapacityCheck(ctx, first, false, now))
	_, ok, err = repo.ClaimDataCapacityCheck(ctx, "worker-three", now.Add(time.Minute), now.Add(2*time.Minute))
	require.NoError(t, err)
	require.False(t, ok)
	require.NoError(t, repo.CompleteDataCapacityCheck(ctx, retry, true, now.Add(3*time.Minute)))

	stillAbove, ok, err := repo.ClaimDataCapacityCheck(ctx, "worker-three", now.Add(3*time.Minute), now.Add(4*time.Minute))
	require.NoError(t, err)
	require.True(t, ok)
	require.Error(t, repo.RotateDataCapacityWarning(ctx, retry, "stale-warning"))
	require.NoError(t, repo.RotateDataCapacityWarning(ctx, stillAbove, "new-warning"))
	require.NoError(t, repo.CompleteDataCapacityCheck(ctx, stillAbove, false, now.Add(5*time.Minute)))
	rearmed, ok, err := repo.ClaimDataCapacityCheck(ctx, "worker-four", now.Add(5*time.Minute), now.Add(6*time.Minute))
	require.NoError(t, err)
	require.True(t, ok)
	require.NotEqual(t, first.WarningID, rearmed.WarningID)
}
