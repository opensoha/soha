package apphosting

import (
	"strings"
	"testing"
	"time"

	"github.com/opensoha/soha/contracts/gen/go/sohaapi"
	app "github.com/opensoha/soha/internal/application/apphosting"
	domain "github.com/opensoha/soha/internal/domain/apphosting"
	"github.com/stretchr/testify/require"
)

func testPostgresPublicationVisibility(t *testing.T, repo *PostgresRepository) {
	t.Helper()
	ctx := t.Context()
	now := time.Now().UTC()
	_, err := repo.SaveSettings(ctx, func(settings domain.Settings) (domain.Settings, error) {
		settings.OrganizationVisibilityPolicy = sohaapi.AppHostingOrganizationVisibilityPolicyRequireApproval
		return settings, nil
	})
	require.NoError(t, err)
	current, _, err := repo.CreateApp(ctx, domain.App{
		ID: "publication-app", Name: "Publication", DNSLabel: "publication", OwnerUserID: "author",
		Kind: sohaapi.HostedAppKindDynamic, Status: sohaapi.HostedAppStatusDraft,
		Visibility:      sohaapi.HostedAppVisibilityScope{Mode: sohaapi.HostedAppVisibilityModePrivate},
		RouteGeneration: 1, CreatedAt: now, UpdatedAt: now,
	}, 20)
	require.NoError(t, err)
	image := "registry.example.com/app@sha256:" + strings.Repeat("a", 64)
	queue := func(id string, mode sohaapi.HostedAppVisibilityMode) domain.Revision {
		t.Helper()
		intent := &domain.RevisionVisibility{
			Scope:  sohaapi.HostedAppVisibilityScope{Mode: mode},
			Reason: "Release review", BaseRouteGeneration: current.RouteGeneration,
		}
		if mode == sohaapi.HostedAppVisibilityModeSelected {
			intent.Scope.UserIDs = []string{"colleague"}
		}
		_, err := repo.CreateUploadingRevision(ctx, current.ID, domain.Revision{
			ID: id, AppID: current.ID, Kind: current.Kind, CreatedBy: current.OwnerUserID,
			CreatedAt: now, PublishVisibility: intent,
		})
		require.NoError(t, err)
		stored, err := repo.GetRevision(ctx, current.ID, id)
		require.NoError(t, err)
		require.Equal(t, intent, stored.PublishVisibility)
		require.NoError(t, repo.QueueDynamicRevision(ctx, current.ID, id, "source/"+id, "node", false, 0, now))
		advanceToStarting(t, repo, id, now, image)
		return stored
	}
	activate := func(rev domain.Revision) app.ActivateRevisionOutput {
		t.Helper()
		out, err := repo.ActivateRevision(ctx, app.ActivateRevisionInput{
			AppID: current.ID, RevisionID: rev.ID, ActorUserID: "author",
			DynamicClaimID: "ready-" + rev.ID, Now: now,
		})
		require.NoError(t, err)
		return out
	}
	first := queue("publication-approval", sohaapi.HostedAppVisibilityModeOrganization)
	before, err := repo.LoadAppEnrichments(ctx, []string{current.ID})
	require.NoError(t, err)
	require.Nil(t, before[current.ID].PendingApproval)
	out := activate(first)
	require.Equal(t, first.ID, out.App.ActiveRevisionID)
	require.Equal(t, sohaapi.HostedAppVisibilityModePrivate, out.App.Visibility.Mode)
	require.NotNil(t, out.Visibility)
	require.NotNil(t, out.Visibility.PendingApproval)
	require.Equal(t, out.App.RouteGeneration, out.Visibility.PendingApproval.BaseRouteGeneration)

	current = out.App
	second := queue("publication-revoked", sohaapi.HostedAppVisibilityModeSelected)
	_, err = repo.UpdateAppVisibility(ctx, app.UpdateVisibilityTxInput{
		AppID: current.ID, ActorUserID: "author", Now: now,
		RequestedScope: sohaapi.HostedAppVisibilityScope{Mode: sohaapi.HostedAppVisibilityModePrivate},
	})
	require.NoError(t, err)
	out = activate(second)
	require.Equal(t, second.ID, out.App.ActiveRevisionID)
	require.Nil(t, out.Visibility)
	require.Equal(t, sohaapi.HostedAppVisibilityModePrivate, out.App.Visibility.Mode)

	for _, action := range []string{"withdraw", "reject"} {
		t.Run(action, func(t *testing.T) {
			current = out.App
			approval, err := repo.UpdateAppVisibility(ctx, app.UpdateVisibilityTxInput{
				AppID: current.ID, ActorUserID: "author", Now: now, NewApprovalID: "apv_before_" + action,
				RequestedScope: sohaapi.HostedAppVisibilityScope{Mode: sohaapi.HostedAppVisibilityModeOrganization},
			})
			require.NoError(t, err)
			require.NotNil(t, approval.PendingApproval)
			queued := queue("publication-"+action, sohaapi.HostedAppVisibilityModeOrganization)
			if action == "withdraw" {
				_, _, err = repo.WithdrawVisibilityApproval(ctx, current.ID, "author", now)
			} else {
				_, _, err = repo.RejectVisibilityApproval(ctx, approval.PendingApproval.ID, "reviewer", "declined", now)
			}
			require.NoError(t, err)
			out = activate(queued)
			require.Nil(t, out.Visibility)
			info, err := repo.LoadAppEnrichments(ctx, []string{current.ID})
			require.NoError(t, err)
			require.Nil(t, info[current.ID].PendingApproval)
		})
	}
}
