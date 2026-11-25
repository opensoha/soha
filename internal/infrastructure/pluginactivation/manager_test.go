package pluginactivation

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"

	"github.com/opensoha/soha/contracts/plugins/storagedriver"
	"github.com/opensoha/soha/internal/application/systemintegration"
	"github.com/opensoha/soha/internal/infrastructure/storagedriverhost"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"github.com/stretchr/testify/require"
)

const fixturePlugin = "example.storage"
const fixtureDriver = "example.blob"

type fixtureDriverClient struct{ storagedriver.Driver }

type fixtureSupervisor struct {
	ready atomic.Bool
	start func(context.Context) error
	drain func(context.Context) error
	stop  func()
}

func (*fixtureSupervisor) Descriptors() []storagedriverhost.Descriptor {
	return []storagedriverhost.Descriptor{{
		ID: fixtureDriver, Name: "Example Blob",
		ConfigSchema: []byte(`{"type":"object","additionalProperties":false,"x-soha-test-connection-action":"storage.testConnection","properties":{"container":{"type":"string"}}}`),
	}}
}

func (*fixtureSupervisor) Driver(string) (storagedriver.Driver, error) {
	return &fixtureDriverClient{}, nil
}
func (s *fixtureSupervisor) Ready(string) bool { return s.ready.Load() }

func (s *fixtureSupervisor) Start(ctx context.Context) error {
	if s.start != nil {
		if err := s.start(ctx); err != nil {
			return err
		}
	}
	s.ready.Store(true)
	return nil
}

func (s *fixtureSupervisor) Close(context.Context) error {
	s.ready.Store(false)
	if s.stop != nil {
		s.stop()
	}
	return nil
}

func (s *fixtureSupervisor) Retire(ctx context.Context) error {
	if s.drain != nil {
		return s.drain(ctx)
	}
	return s.Close(ctx)
}

func fixtureConfig() Config {
	return Config{Slots: map[string][2]Account{fixturePlugin: {{UID: 501, GID: 601}, {UID: 502, GID: 602}}}}
}

func fixtureRelease(version string) Release {
	return Release{PluginID: fixturePlugin, DriverID: fixtureDriver, Version: version}
}

func TestSwitchKeepsSelectedBackendAfterHandshakeOrCommitFailure(t *testing.T) {
	ctx := t.Context()
	v1 := &fixtureSupervisor{}
	handshakeErr := errors.New("candidate cannot authenticate")
	commitErr := errors.New("durable compare-and-swap rejected")
	attempts := []*fixtureSupervisor{v1, {start: func(context.Context) error { return handshakeErr }}, {}, {}}
	used := make([]Account, 0, len(attempts))
	factory := func(_ Release, account Account) (storageSupervisor, func() error, error) {
		host := attempts[len(used)]
		used = append(used, account)
		return host, func() error { return nil }, nil
	}
	m := newManager(ctx, fixtureConfig(), systemintegration.NewStorageDriverRegistry(nil), factory, nil)
	t.Cleanup(func() { require.NoError(t, m.Close(context.Background())) })
	require.NoError(t, m.Switch(ctx, fixtureRelease("1.0.0"), func() error {
		require.True(t, v1.Ready(fixtureDriver))
		require.False(t, m.Ready(fixturePlugin, "1.0.0"))
		return nil
	}))
	require.ErrorIs(t, m.Switch(ctx, fixtureRelease("2.0.0"), func() error {
		t.Fatal("handshake failure reached durable commit")
		return nil
	}), handshakeErr)
	require.True(t, m.Ready(fixturePlugin, "1.0.0"))
	require.ErrorIs(t, m.Switch(ctx, fixtureRelease("2.0.0"), func() error {
		require.True(t, m.Ready(fixturePlugin, "1.0.0"))
		require.True(t, attempts[2].Ready(fixtureDriver))
		require.False(t, m.Ready(fixturePlugin, "2.0.0"))
		return commitErr
	}), commitErr)
	require.True(t, m.Ready(fixturePlugin, "1.0.0"))
	require.NoError(t, m.Switch(ctx, fixtureRelease("2.0.0"), func() error { return nil }))
	require.False(t, m.Ready(fixturePlugin, "1.0.0"))
	require.True(t, m.Ready(fixturePlugin, "2.0.0"))
	attempts[3].ready.Store(false)
	require.False(t, m.Ready(fixturePlugin, "2.0.0"))
	require.Equal(t, []Account{{501, 601}, {502, 602}, {502, 602}, {502, 602}}, used)
}

func TestSelectedBackendDrainsBeforeItsAccountCanBeReused(t *testing.T) {
	ctx := t.Context()
	draining, drain := make(chan struct{}), make(chan struct{})
	releaseDrain := func() {
		select {
		case <-drain:
		default:
			close(drain)
		}
	}
	used := make([]uint32, 0, 3)
	factory := func(_ Release, account Account) (storageSupervisor, func() error, error) {
		host := &fixtureSupervisor{}
		if len(used) == 0 {
			host.stop = func() {
				close(draining)
				<-drain
			}
		}
		used = append(used, account.UID)
		return host, func() error { return nil }, nil
	}
	m := newManager(ctx, fixtureConfig(), systemintegration.NewStorageDriverRegistry(nil), factory, nil)
	t.Cleanup(func() {
		releaseDrain()
		require.NoError(t, m.Close(context.Background()))
	})
	commit := func() error { return nil }
	require.NoError(t, m.Switch(ctx, fixtureRelease("1.0.0"), commit))
	require.NoError(t, m.Switch(ctx, fixtureRelease("2.0.0"), commit))
	<-draining
	require.True(t, m.Ready(fixturePlugin, "2.0.0"))
	done := make(chan error, 1)
	go func() { done <- m.Switch(ctx, fixtureRelease("1.0.0"), commit) }()
	select {
	case err := <-done:
		t.Fatalf("rollback used a slot that was still draining: %v", err)
	default:
	}
	releaseDrain()
	require.NoError(t, <-done)
	require.Equal(t, []uint32{501, 502, 501}, used)
	require.True(t, m.Ready(fixturePlugin, "1.0.0"))

	referenced := errors.New("storage driver still referenced")
	require.ErrorIs(t, m.Disable(ctx, fixturePlugin, func() error { return referenced }), referenced)
	require.True(t, m.Ready(fixturePlugin, "1.0.0"))
	require.NoError(t, m.Disable(ctx, fixturePlugin, commit))
	require.False(t, m.Ready(fixturePlugin, "1.0.0"))
	require.NoError(t, m.WaitRetired(ctx, fixturePlugin))
}

func TestWaitRetiredIncludesEveryPriorBackendSlot(t *testing.T) {
	ctx := t.Context()
	oldStopping, stayOld := make(chan struct{}), make(chan struct{})
	currentStopping, stayCurrent := make(chan struct{}), make(chan struct{})
	release := func(ch chan struct{}) {
		select {
		case <-ch:
		default:
			close(ch)
		}
	}
	cleanupErr := errors.New("old slot directory cleanup failed")
	factory := func(_ Release, account Account) (storageSupervisor, func() error, error) {
		if account.UID == 501 {
			return &fixtureSupervisor{stop: func() { close(oldStopping); <-stayOld }}, func() error { return cleanupErr }, nil
		}
		return &fixtureSupervisor{stop: func() { close(currentStopping); <-stayCurrent }}, func() error { return nil }, nil
	}
	m := newManager(ctx, fixtureConfig(), systemintegration.NewStorageDriverRegistry(nil), factory, nil)
	t.Cleanup(func() {
		release(stayOld)
		release(stayCurrent)
		require.ErrorIs(t, m.Close(context.Background()), cleanupErr)
	})
	commit := func() error { return nil }
	require.NoError(t, m.Switch(ctx, fixtureRelease("1.0.0"), commit))
	require.ErrorIs(t, m.WaitRetired(ctx, fixturePlugin), apperrors.ErrConflict)
	require.NoError(t, m.Switch(ctx, fixtureRelease("2.0.0"), commit))
	<-oldStopping
	require.NoError(t, m.Disable(ctx, fixturePlugin, commit))
	<-currentStopping

	done := make(chan error, 1)
	go func() { done <- m.WaitRetired(ctx, fixturePlugin) }()
	release(stayCurrent)
	select {
	case err := <-done:
		t.Fatalf("retirement completed while previous slot was still running: %v", err)
	default:
	}
	release(stayOld)
	require.ErrorIs(t, <-done, cleanupErr)
}

func TestCloseForcesGracefulRetirementAndWaitsForCleanup(t *testing.T) {
	ctx := t.Context()
	draining, reaped, cleaned := make(chan struct{}), make(chan struct{}), make(chan struct{})
	factory := func(release Release, _ Account) (storageSupervisor, func() error, error) {
		if release.Version == "1.0.0" {
			host := &fixtureSupervisor{
				drain: func(ctx context.Context) error {
					close(draining)
					<-ctx.Done()
					return ctx.Err()
				},
				stop: func() { close(reaped) },
			}
			return host, func() error { close(cleaned); return nil }, nil
		}
		return &fixtureSupervisor{}, func() error { return nil }, nil
	}
	m := newManager(ctx, fixtureConfig(), systemintegration.NewStorageDriverRegistry(nil), factory, nil)
	t.Cleanup(func() { require.NoError(t, m.Close(context.Background())) })
	commit := func() error { return nil }
	require.NoError(t, m.Switch(ctx, fixtureRelease("1.0.0"), commit))
	require.NoError(t, m.Switch(ctx, fixtureRelease("2.0.0"), commit))
	<-draining
	require.NoError(t, m.Close(ctx))
	select {
	case <-reaped:
	default:
		t.Fatal("forced retirement returned before the supervisor reaped its process")
	}
	select {
	case <-cleaned:
	default:
		t.Fatal("forced retirement returned before the runtime slot was cleaned up")
	}
}

func TestSwitchRejectsStorageDriverIDsReservedByAnotherPlugin(t *testing.T) {
	ctx := t.Context()
	cfg := fixtureConfig()
	cfg.Slots["other.storage"] = [2]Account{{UID: 503, GID: 603}, {UID: 504, GID: 604}}
	factory := func(Release, Account) (storageSupervisor, func() error, error) {
		return &fixtureSupervisor{}, func() error { return nil }, nil
	}
	m := newManager(ctx, cfg, systemintegration.NewStorageDriverRegistry(nil), factory, nil)
	t.Cleanup(func() { require.NoError(t, m.Close(context.Background())) })
	original := fixtureRelease("1.0.0")
	require.NoError(t, m.Switch(ctx, original, func() error { return nil }))

	other := original
	other.PluginID = "other.storage"
	require.ErrorIs(t, m.Switch(ctx, other, func() error {
		t.Fatal("another plugin tried to commit a duplicate driver ID")
		return nil
	}), apperrors.ErrConflict)
	require.Len(t, m.running, 1)
	require.True(t, m.Ready(fixturePlugin, original.Version))
	require.False(t, m.Ready(other.PluginID, other.Version))
}

func TestWebOnlyActivationAndStartupRestoreUseNoStorageAccount(t *testing.T) {
	ctx := t.Context()
	registry := systemintegration.NewStorageDriverRegistry(nil)
	m, err := New(ctx, Config{}, registry)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, m.Close(context.Background())) })

	release := fixtureRelease("1.0.0")
	commit := func() error { return nil }
	require.ErrorIs(t, m.Switch(ctx, release, commit), apperrors.ErrUnsupportedOperation)
	release.DriverID = ""
	require.NoError(t, m.Switch(ctx, release, commit))
	require.True(t, m.Ready(fixturePlugin, release.Version))
	require.NoError(t, m.Close(ctx))
	require.False(t, m.Ready(fixturePlugin, release.Version))

	restarted, err := New(ctx, Config{}, registry)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, restarted.Close(context.Background())) })
	require.NoError(t, restarted.Switch(ctx, release, commit))
	require.True(t, restarted.Ready(fixturePlugin, release.Version))
}
