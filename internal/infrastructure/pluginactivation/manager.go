// Package pluginactivation switches verified executable plugin releases on a
// single Core host. Durable release selection and trust policy are owned by
// the application.
package pluginactivation

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sync"

	"github.com/opensoha/soha/contracts/plugins/storagedriver"
	"github.com/opensoha/soha/internal/application/systemintegration"
	"github.com/opensoha/soha/internal/infrastructure/storagedriverhost"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

// Account identifies a dedicated Linux execution identity.
type Account struct {
	UID uint32
	GID uint32
}

// Config maps each authorized backend plugin ID to two dedicated accounts.
// RuntimeRoot is a trusted root directory reserved for the manager.
type Config struct {
	PackageRoot string
	RuntimeRoot string
	Slots       map[string][2]Account
}

// Release describes a verified, local package. An empty DriverID selects a
// Web-only package and needs no process account.
type Release struct {
	PluginID      string
	DriverID      string
	Version       string
	Repository    string
	Commit        string
	PublicKeyPath string
}

// Manager coordinates local release activation and runner retirement.
type Manager struct {
	ctx        context.Context
	cancel     context.CancelFunc
	slots      map[string]driverSlots
	newBackend backendFactory
	closeRoot  func() error

	// Lifecycle operations serialize candidate preparation and persist/publish.
	// Readiness and storage calls do not wait on a lifecycle operation.
	operation chan struct{}
	mu        sync.RWMutex
	active    map[string]activation
	running   map[uint32]*backend

	closeOnce sync.Once
	done      chan struct{}
	closeErr  error
}

type activation struct {
	release Release
	backend *backend
}

type driverSlots struct {
	owner    *systemintegration.ManagedStorageDrivers
	accounts [2]Account
}

// New takes the managed runtime lease when backend accounts are configured.
func New(ctx context.Context, cfg Config, registry *systemintegration.StorageDriverRegistry) (*Manager, error) {
	if registry == nil {
		return nil, errors.New("plugin activation requires a storage driver registry")
	}
	accounts, err := validateSlots(cfg)
	if err != nil {
		return nil, err
	}
	if len(accounts) == 0 {
		return newManager(ctx, cfg, registry, nil, nil), nil
	}
	runtime, err := storagedriverhost.LeaseManagedRuntime(cfg.RuntimeRoot, accounts)
	if err != nil {
		return nil, fmt.Errorf("configure managed storage driver runtime: %w", err)
	}
	return newManager(ctx, cfg, registry, supervisorFactory(cfg.PackageRoot, runtime), runtime.Close), nil
}

func validateSlots(cfg Config) (map[uint32]uint32, error) {
	users, groups := make(map[uint32]uint32), make(map[uint32]bool)
	if len(cfg.Slots) > 32 {
		return nil, errors.New("managed storage drivers can configure up to 32 plugins")
	}
	if len(cfg.Slots) > 0 && (!filepath.IsAbs(cfg.PackageRoot) || filepath.Clean(cfg.PackageRoot) != cfg.PackageRoot || cfg.PackageRoot == "/") {
		return nil, errors.New("managed storage drivers require a canonical absolute package root")
	}
	for pluginID, slots := range cfg.Slots {
		if err := releaseIdentity(pluginID, "0.0.0").Validate(); err != nil {
			return nil, errors.New("invalid managed storage driver plugin ID")
		}
		for _, slot := range slots {
			if slot.UID == 0 || slot.GID == 0 || users[slot.UID] != 0 || groups[slot.GID] {
				return nil, errors.New("managed storage drivers require two unique dedicated UID/GID pairs per plugin")
			}
			users[slot.UID] = slot.GID
			groups[slot.GID] = true
		}
	}
	return users, nil
}

func newManager(ctx context.Context, cfg Config, registry *systemintegration.StorageDriverRegistry, factory backendFactory, closeRoot func() error) *Manager {
	ctx, cancel := context.WithCancel(ctx)
	manager := &Manager{
		ctx: ctx, cancel: cancel, slots: make(map[string]driverSlots, len(cfg.Slots)),
		newBackend: factory, closeRoot: closeRoot, operation: make(chan struct{}, 1), active: make(map[string]activation),
		running: make(map[uint32]*backend), done: make(chan struct{}),
	}
	for pluginID, slots := range cfg.Slots {
		manager.slots[pluginID] = driverSlots{owner: registry.ManagedDrivers(), accounts: slots}
	}
	context.AfterFunc(ctx, manager.initiateClose)
	return manager
}

// Switch completes the candidate's authenticated handshake before invoking
// commit. During commit, callers continue to use the previous descriptor.
// After success, accepted calls drain on the retired process in the background.
func (m *Manager) Switch(ctx context.Context, release Release, commit func() error) error {
	if err := releaseIdentity(release.PluginID, release.Version).Validate(); err != nil {
		return fmt.Errorf("%w: invalid executable plugin release identity", apperrors.ErrInvalidArgument)
	}
	if err := m.lock(ctx); err != nil {
		return err
	}
	defer m.unlock()

	previous := m.current(release.PluginID)
	if previous.release.DriverID != "" && previous.release.DriverID != release.DriverID {
		return fmt.Errorf("%w: storage driver ID must remain stable across releases", apperrors.ErrConflict)
	}
	next := activation{release: release}
	if release.DriverID == "" {
		if err := m.commit(ctx, commit); err != nil {
			return err
		}
	} else {
		candidate, err := m.switchBackend(ctx, release, previous.backend, commit)
		if err != nil {
			return err
		}
		next.backend = candidate
	}
	m.setCurrent(release.PluginID, next)
	if previous.backend != nil {
		previous.backend.retire(m.ctx, true)
	}
	return nil
}

func (m *Manager) switchBackend(ctx context.Context, release Release, previous *backend, commit func() error) (*backend, error) {
	slots, exists := m.slots[release.PluginID]
	if !exists {
		return nil, fmt.Errorf("%w: no dedicated account slots configured for this storage driver", apperrors.ErrUnsupportedOperation)
	}
	if err := slots.owner.CheckOwner(release.DriverID); err != nil {
		return nil, err
	}
	account, err := m.availableAccount(ctx, release.PluginID, previous)
	if err != nil {
		return nil, err
	}
	supervisor, cleanup, err := m.newBackend(release, account)
	if err != nil {
		return nil, err
	}
	lifetime, cancel := context.WithCancel(m.ctx)
	candidate := &backend{host: supervisor, cleanup: cleanup, cancel: cancel, done: make(chan struct{})}
	m.running[account.UID] = candidate

	descriptor, driver, err := candidate.start(ctx, lifetime, release.DriverID)
	if err == nil {
		err = slots.owner.Switch(descriptor.ID, descriptor.Name, release.Version, descriptor.ConfigSchema, driver, func() error {
			if !candidate.host.Ready(descriptor.ID) {
				return fmt.Errorf("%w: storage driver candidate unavailable", apperrors.ErrServiceUnavailable)
			}
			return m.commit(ctx, commit)
		})
	}
	if err != nil {
		candidate.retire(m.ctx, false)
		return nil, err
	}
	return candidate, nil
}

// Disable withdraws an active release after the caller commits reference and
// lifecycle checks. It also supports removing a persisted release whose
// process was unavailable during startup restore.
func (m *Manager) Disable(ctx context.Context, pluginID string, commit func() error) error {
	if err := m.lock(ctx); err != nil {
		return err
	}
	defer m.unlock()
	previous := m.current(pluginID)
	commitSelection := func() error { return m.commit(ctx, commit) }
	var err error
	if previous.backend == nil {
		err = commitSelection()
	} else {
		err = m.slots[pluginID].owner.Remove(previous.release.DriverID, commitSelection)
	}
	if err != nil {
		return err
	}
	m.setCurrent(pluginID, activation{})
	if previous.backend != nil {
		previous.backend.retire(m.ctx, false)
	}
	return nil
}

// WaitRetired waits for both account slots of a disabled plugin before its
// artifacts are removed. The caller must serialize reactivation against
// retirement and the artifact cleanup.
func (m *Manager) WaitRetired(ctx context.Context, pluginID string) error {
	if err := m.lock(ctx); err != nil {
		return err
	}
	if m.current(pluginID).release.Version != "" {
		m.unlock()
		return fmt.Errorf("%w: plugin release is still active", apperrors.ErrConflict)
	}
	var retired [2]*backend
	for index, slot := range m.slots[pluginID].accounts {
		retired[index] = m.running[slot.UID]
	}
	m.unlock()

	var failures error
	for _, running := range retired {
		if running == nil {
			continue
		}
		select {
		case <-running.done:
			failures = errors.Join(failures, running.closeErr)
		case <-ctx.Done():
			return errors.Join(failures, ctx.Err())
		}
	}
	return failures
}

// Ready checks the exact selected release. For a backend this reports an
// authenticated B7 process. For Web-only packages it reports local selection.
func (m *Manager) Ready(pluginID, version string) bool {
	active := m.current(pluginID)
	if active.release.Version != version || version == "" || m.ctx.Err() != nil {
		return false
	}
	return active.backend == nil || (m.slots[pluginID].owner.ActiveVersion(active.release.DriverID) == version &&
		active.backend.host.Ready(active.release.DriverID))
}

func (m *Manager) current(pluginID string) activation {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.active[pluginID]
}

func (m *Manager) setCurrent(pluginID string, value activation) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if value.release.Version == "" {
		delete(m.active, pluginID)
	} else {
		m.active[pluginID] = value
	}
}

func (m *Manager) commit(ctx context.Context, commit func() error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := m.ctx.Err(); err != nil {
		return err
	}
	if commit == nil {
		return errors.New("missing plugin release commit")
	}
	return commit()
}

func releaseIdentity(id, version string) storagedriver.Identity {
	return storagedriver.Identity{
		PluginID: id, Version: version, ProtocolMajor: storagedriver.ProtocolMajor, Capabilities: storagedriver.V1Capabilities,
	}
}

func (m *Manager) lock(ctx context.Context) error {
	select {
	case m.operation <- struct{}{}:
		if err := errors.Join(ctx.Err(), m.ctx.Err()); err != nil {
			m.unlock()
			return err
		}
		return nil
	case <-ctx.Done():
		return ctx.Err()
	case <-m.ctx.Done():
		return m.ctx.Err()
	}
}

func (m *Manager) unlock() { <-m.operation }

// Close withdraws managed entries and waits for every process/slot to drain.
// A timed-out caller may wait again. The root lease remains held until cleanup.
func (m *Manager) Close(ctx context.Context) error {
	m.initiateClose()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-m.done:
		return m.closeErr
	}
}

func (m *Manager) initiateClose() {
	m.closeOnce.Do(func() {
		m.cancel()
		go m.close()
	})
}

func (m *Manager) close() {
	m.operation <- struct{}{}
	defer m.unlock()
	defer close(m.done)
	for id, active := range m.active {
		if active.backend != nil {
			m.closeErr = errors.Join(m.closeErr, m.slots[id].owner.Remove(active.release.DriverID, func() error { return nil }))
		}
		m.setCurrent(id, activation{})
	}
	for _, running := range m.running {
		running.retire(m.ctx, false)
	}
	for _, running := range m.running {
		<-running.done
		m.closeErr = errors.Join(m.closeErr, running.closeErr)
	}
	if m.closeRoot != nil {
		m.closeErr = errors.Join(m.closeErr, m.closeRoot())
	}
}
