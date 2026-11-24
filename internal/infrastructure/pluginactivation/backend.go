package pluginactivation

import (
	"context"
	"errors"
	"fmt"

	"github.com/opensoha/soha/contracts/plugins/storagedriver"
	"github.com/opensoha/soha/internal/infrastructure/storagedriverhost"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type storageSupervisor interface {
	Descriptors() []storagedriverhost.Descriptor
	Driver(string) (storagedriver.Driver, error)
	Ready(string) bool
	Start(context.Context) error
	Retire(context.Context) error
	Close(context.Context) error
}

type backendFactory func(Release, Account) (storageSupervisor, func() error, error)

type backend struct {
	host    storageSupervisor
	cleanup func() error
	cancel  context.CancelFunc
	done    chan struct{}

	// retired is guarded by Manager.operation. closeErr is published by done.
	retired  bool
	closeErr error
}

func supervisorFactory(packageRoot string, runtime *storagedriverhost.ManagedRuntime) backendFactory {
	return func(release Release, account Account) (storageSupervisor, func() error, error) {
		runtimeRoot, cleanup, err := runtime.PrepareSlot(account.UID)
		if err != nil {
			return nil, nil, err
		}
		supervisor, err := storagedriverhost.New(storagedriverhost.Config{
			PackageRoot: packageRoot,
			RuntimeRoot: runtimeRoot,
			Drivers: []storagedriverhost.DriverConfig{{
				PluginID: release.PluginID, Version: release.Version, Commit: release.Commit, Repository: release.Repository,
				PublicKeyPath: release.PublicKeyPath, UID: account.UID, GID: account.GID,
			}},
		})
		if err != nil {
			return nil, nil, errors.Join(err, cleanup())
		}
		return supervisor, cleanup, nil
	}
}

func (b *backend) start(ctx, lifetime context.Context, driverID string) (storagedriverhost.Descriptor, storagedriver.Driver, error) {
	descriptors := b.host.Descriptors()
	if len(descriptors) != 1 || descriptors[0].ID != driverID {
		return storagedriverhost.Descriptor{}, nil, fmt.Errorf("%w: signed storage driver ID differs from selected release", apperrors.ErrConflict)
	}
	driver, err := b.host.Driver(driverID)
	if err != nil {
		return storagedriverhost.Descriptor{}, nil, err
	}
	// Request cancellation interrupts the initial handshake. After the
	// handshake, the process belongs to Core's lifecycle context.
	stop := context.AfterFunc(ctx, b.cancel)
	defer stop()
	if err := b.host.Start(lifetime); err != nil {
		return storagedriverhost.Descriptor{}, nil, fmt.Errorf("start storage driver candidate: %w", err)
	}
	return descriptors[0], driver, ctx.Err()
}

func (b *backend) retire(ctx context.Context, graceful bool) {
	if b.retired {
		return
	}
	b.retired = true
	go func() {
		defer close(b.done)
		defer b.cancel()
		b.closeErr = errors.Join(b.stop(ctx, graceful), b.cleanup())
	}()
}

func (b *backend) stop(ctx context.Context, graceful bool) error {
	if graceful {
		if err := b.host.Retire(ctx); ctx.Err() == nil || !errors.Is(err, ctx.Err()) {
			return err
		}
		// Server shutdown has forced the retire wait to return. Keep the
		// slot until the supervisor has actually reaped the process.
	}
	return b.host.Close(context.Background())
}

func (m *Manager) availableAccount(ctx context.Context, pluginID string, active *backend) (Account, error) {
	var wait *backend
	for _, slot := range m.slots[pluginID].accounts {
		running := m.running[slot.UID]
		switch {
		case running == nil:
			return slot, nil
		case running != active:
			select {
			case <-running.done:
				if running.closeErr == nil {
					return slot, nil
				}
			default:
				wait = running
			}
		}
	}
	if wait == nil {
		return Account{}, fmt.Errorf("%w: no clean storage driver account slot", apperrors.ErrServiceUnavailable)
	}
	select {
	case <-ctx.Done():
		return Account{}, ctx.Err()
	case <-m.ctx.Done():
		return Account{}, m.ctx.Err()
	case <-wait.done:
		return m.availableAccount(ctx, pluginID, active)
	}
}
