package plugin_test

import (
	"context"
	"testing"
	"time"

	domainplugin "github.com/opensoha/soha/internal/domain/plugin"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"github.com/stretchr/testify/require"
)

func TestExecutableReleaseHistoryWithPostgres(t *testing.T) {
	repository, id := newPluginIntegrationRepository(t)
	ctx := t.Context()
	now := time.Now().UTC().Truncate(time.Microsecond)
	first := domainplugin.InstalledPlugin{
		ID: id, Name: "Signed driver", Version: "1.0.0", Type: "resource-extension",
		Status: "enabled", Publisher: "Fixture", Source: "github-repo",
		InstalledBy: "test-operator", InstalledAt: now, UpdatedAt: now,
		SignatureStatus: "verified", ChecksumStatus: "verified",
		ConfiguredSecretRefs: map[string]string{"credentials": "private-ref"},
		SourceInfo:           &domainplugin.PluginSourceInfo{Kind: domainplugin.SourceKindGitHubRepo, CanonicalSource: "https://github.com/example/driver"},
	}
	expected, created, err := repository.CreateInstalled(ctx, first, nil)
	require.NoError(t, err)
	require.True(t, created)

	v2 := expected
	v2.Version = "2.0.0"
	commit := func(stop error) func(context.Context, *domainplugin.InstalledPlugin) error {
		return func(tx context.Context, _ *domainplugin.InstalledPlugin) error {
			if err := repository.RetainRelease(tx, expected); err != nil {
				return err
			}
			return stop
		}
	}
	_, err = repository.UpdateInstalled(ctx, expected, v2, commit(apperrors.ErrServiceUnavailable))
	require.ErrorIs(t, err, apperrors.ErrServiceUnavailable)
	_, err = repository.PreviousRelease(ctx, id)
	require.ErrorIs(t, err, apperrors.ErrNotFound)
	v2, err = repository.UpdateInstalled(ctx, expected, v2, commit(nil))
	require.NoError(t, err)
	require.Equal(t, "2.0.0", v2.Version)
	previous, err := repository.PreviousRelease(ctx, id)
	require.NoError(t, err)
	require.Equal(t, expected.Version, previous.Version)
	require.Equal(t, expected.UpdatedAt, previous.UpdatedAt)
	require.Empty(t, previous.ConfiguredSecretRefs)

	_, err = repository.UpdateInstalled(ctx, expected, v2, commit(nil))
	require.ErrorIs(t, err, apperrors.ErrConflict)
	_, err = repository.RemoveInstalled(ctx, id, func(context.Context, domainplugin.InstalledPlugin) error { return nil })
	require.NoError(t, err)
	_, err = repository.PreviousRelease(ctx, id)
	require.ErrorIs(t, err, apperrors.ErrNotFound)
}
