package apphosting_test

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/opensoha/soha/contracts/gen/go/sohaapi"
	"github.com/opensoha/soha/internal/application/access"
	apphosting "github.com/opensoha/soha/internal/application/apphosting"
	domain "github.com/opensoha/soha/internal/domain/apphosting"
	"github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/apperrors"
	repository "github.com/opensoha/soha/internal/repository/apphosting"
	"github.com/stretchr/testify/require"
)

type handoffRaceRepository struct {
	*repository.MemoryRepository
	beforeLock func(context.Context)
}

func (r *handoffRaceRepository) WithLockedDynamicRuntime(ctx context.Context, appID string, use func(context.Context, domain.App, domain.RuntimeConfig) error) error {
	r.beforeLock(ctx)
	return r.MemoryRepository.WithLockedDynamicRuntime(ctx, appID, use)
}

type handoffProbe struct {
	dynamicDeploymentProbe
	handoffs int
}

func (p *handoffProbe) PrepareRuntime(context.Context, domain.Revision) error {
	p.handoffs++
	return nil
}

func TestSensitiveDynamicRuntimeRechecksAuthorityAtHandoff(t *testing.T) {
	cases := []struct {
		name     string
		database bool
		env      bool
		change   func(context.Context, *testHarness, string) error
	}{
		{name: "owner transferred", database: true, change: func(ctx context.Context, h *testHarness, appID string) error {
			_, _, err := h.repo.TransferAppOwnership(ctx, appID, h.bob.UserID, time.Now())
			return err
		}},
		{name: "credential replaced", env: true, change: func(ctx context.Context, h *testHarness, appID string) error {
			_, err := h.repo.MutateRuntimeConfig(ctx, appID, func(_ domain.App, config domain.RuntimeConfig) (domain.RuntimeConfig, error) {
				config.Environment["APP_TOKEN"] = "replacement-ciphertext"
				return config, nil
			})
			return err
		}},
		{name: "app taken offline", database: true, change: func(ctx context.Context, h *testHarness, appID string) error {
			_, err := h.repo.MutateApp(ctx, appID, func(app domain.App) (domain.App, error) {
				app.Status = sohaapi.HostedAppStatusOffline
				return app, nil
			})
			return err
		}},
		{name: "account disabled", database: true, change: func(_ context.Context, h *testHarness, _ string) error {
			h.audience.SetUserStatus(h.alice.UserID, "disabled")
			return nil
		}},
		{name: "hosting disabled", database: true, change: disableHosting},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			ctx := t.Context()
			probe := &handoffProbe{}
			h := newTestHarness(t, apphosting.WithClock(time.Now), apphosting.WithDynamicBackend(probe))
			app, _, err := h.svc.CreateApp(ctx, h.alice, sohaapi.CreateHostedAppRequest{Name: "Sensitive handoff", Kind: sohaapi.HostedAppKindDynamic})
			require.NoError(t, err)
			if test.env {
				_, err := h.repo.MutateRuntimeConfig(ctx, app.ID, func(_ domain.App, config domain.RuntimeConfig) (domain.RuntimeConfig, error) {
					config.Environment = map[string]string{"APP_TOKEN": "original-ciphertext"}
					return config, nil
				})
				require.NoError(t, err)
			}
			source := makeTarGz(t, []tarEntrySpec{
				{name: "soha-host.json", body: fmt.Sprintf(`{"template":"python","database":%t}`, test.database)},
				{name: "app.py", body: "print('hello')"},
				{name: "requirements.txt", body: ""},
			})
			receipt := uploadTestDynamic(t, h, app.ID, "handoff", source)
			reconcileDynamicSteps(t, h, 2)
			beforeLock := 0
			repo := &handoffRaceRepository{MemoryRepository: h.repo, beforeLock: func(handoff context.Context) {
				beforeLock++
				_, hasDeadline := handoff.Deadline()
				require.True(t, hasDeadline)
				require.NoError(t, test.change(handoff, h, app.ID))
			}}
			perms := &stubPermissions{grants: map[string][]string{h.alice.UserID: {access.PermAppHostingAppsPublish}}}
			worker, err := apphosting.New(repo, h.store, h.audience, perms, h.audit, apphosting.WithClock(time.Now), apphosting.WithDynamicBackend(probe))
			require.NoError(t, err)
			require.NoError(t, worker.ReconcileDynamicRevision(ctx))
			require.Equal(t, 1, beforeLock)
			require.Zero(t, probe.handoffs)
			require.Equal(t, receipt.Revision.ID, probe.discarded)
			stored, err := h.repo.GetRevision(ctx, app.ID, receipt.Revision.ID)
			require.NoError(t, err)
			require.Equal(t, sohaapi.HostedAppRevisionStatusFailed, stored.Status)
		})
	}
}

func disableHosting(ctx context.Context, h *testHarness, _ string) error {
	_, err := h.repo.SaveSettings(ctx, func(settings domain.Settings) (domain.Settings, error) {
		settings.Enabled = false
		return settings, nil
	})
	return err
}

type blockingReadyProbe struct {
	dynamicDeploymentProbe
	started chan struct{}
	resume  chan struct{}
}

func (p *blockingReadyProbe) RuntimeReady(ctx context.Context, _ domain.Revision) (bool, error) {
	close(p.started)
	select {
	case <-p.resume:
		return true, nil
	case <-ctx.Done():
		return false, ctx.Err()
	}
}

type dynamicPublisherPermissions struct {
	stubPermissions
	failure error
}

func (p *dynamicPublisherPermissions) Authorize(ctx context.Context, principal identity.Principal, key string) error {
	if p.failure != nil {
		return p.failure
	}
	return p.stubPermissions.Authorize(ctx, principal, key)
}

func TestDynamicRuntimeRechecksLiveAuthorityAfterReadiness(t *testing.T) {
	resolverUnavailable := errors.New("temporary permission resolver failure")
	for _, tc := range []struct {
		name   string
		change func(*testing.T, *testHarness, *dynamicPublisherPermissions)
		want   error
	}{
		{name: "account disabled", change: func(_ *testing.T, h *testHarness, _ *dynamicPublisherPermissions) {
			h.audience.SetUserStatus(h.alice.UserID, "disabled")
		}},
		{name: "publish permission revoked", change: func(_ *testing.T, _ *testHarness, permissions *dynamicPublisherPermissions) {
			permissions.failure = apperrors.ErrAccessDenied
		}},
		{name: "hosting disabled", change: func(t *testing.T, h *testHarness, _ *dynamicPublisherPermissions) {
			require.NoError(t, disableHosting(t.Context(), h, ""))
		}},
		{name: "permission resolver unavailable", change: func(_ *testing.T, _ *testHarness, permissions *dynamicPublisherPermissions) {
			permissions.failure = resolverUnavailable
		}, want: resolverUnavailable},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			backend := &blockingReadyProbe{started: make(chan struct{}), resume: make(chan struct{})}
			h := newTestHarness(t, apphosting.WithClock(time.Now), apphosting.WithDynamicBackend(backend))
			app, _, err := h.svc.CreateApp(ctx, h.alice, sohaapi.CreateHostedAppRequest{Name: "Dynamic handoff", Kind: sohaapi.HostedAppKindDynamic})
			require.NoError(t, err)
			receipt := uploadTestDynamic(t, h, app.ID, "handoff", dynamicDataTestBundle(t))
			reconcileDynamicSteps(t, h, 2)
			permissions := &dynamicPublisherPermissions{stubPermissions: stubPermissions{grants: map[string][]string{h.alice.UserID: {access.PermAppHostingAppsPublish}}}}
			worker, err := apphosting.New(h.repo, h.store, h.audience, permissions, h.audit, apphosting.WithClock(time.Now), apphosting.WithDynamicBackend(backend))
			require.NoError(t, err)
			done := make(chan error, 1)
			go func() { done <- worker.ReconcileDynamicRevision(ctx) }()
			select {
			case <-backend.started:
			case <-ctx.Done():
				t.Fatal("candidate never entered readiness")
			}
			tc.change(t, h, permissions)
			close(backend.resume)
			select {
			case err = <-done:
			case <-ctx.Done():
				t.Fatal("reconciliation did not return")
			}
			require.ErrorIs(t, err, tc.want)
			require.Empty(t, backend.promoted)
			stored, err := h.repo.GetRevision(ctx, app.ID, receipt.Revision.ID)
			require.NoError(t, err)
			if tc.want != nil {
				require.Equal(t, sohaapi.HostedAppRevisionStatusValidating, stored.Status)
				require.Empty(t, backend.discarded)
			} else {
				require.Equal(t, sohaapi.HostedAppRevisionStatusFailed, stored.Status)
				require.Equal(t, stored.ID, backend.discarded)
			}
			current, err := h.repo.GetAppByID(ctx, app.ID)
			require.NoError(t, err)
			require.Empty(t, current.ActiveRevisionID)
		})
	}
}
