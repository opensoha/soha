// Package storagedriverhost supervises administrator-pinned, signed storage
// driver releases on a single Linux host.
package storagedriverhost

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/opensoha/soha/contracts/plugins/storagedriver"
	"github.com/opensoha/soha/contracts/plugins/storagedriver/sdk"
)

var ErrUnsupported = errors.New("the storage driver supervisor requires Linux process isolation")

// Config selects trusted local releases. The supervisor has no download path.
type Config struct {
	PackageRoot string
	RuntimeRoot string
	Drivers     []DriverConfig
}

// DriverConfig pins the local release and its dedicated host account.
type DriverConfig struct {
	PluginID      string
	Version       string
	Repository    string
	Commit        string
	PublicKeyPath string
	UID           uint32
	GID           uint32
}

// Descriptor identifies the storage backend declared by the signed manifest.
type Descriptor struct {
	ID           string
	Name         string
	ConfigSchema json.RawMessage
}

// Supervisor owns running drivers and the exclusive lease on their runtime root.
type Supervisor struct {
	config  Config
	bounds  deadlines
	drivers []*managedDriver
	byID    map[string]*managedDriver

	mu        sync.Mutex
	started   bool
	closed    bool
	cleanup   error
	cancel    context.CancelFunc
	done      chan struct{}
	force     chan struct{}
	forceOnce sync.Once
}

type deadlines struct {
	startup time.Duration
	call    time.Duration
	drain   time.Duration
	term    time.Duration
	backoff time.Duration
	maxWait time.Duration
	stable  time.Duration
}

func defaultDeadlines() deadlines {
	return deadlines{
		startup: 10 * time.Second, call: 5 * time.Minute, drain: 5 * time.Second, term: time.Second,
		backoff: time.Second, maxWait: 30 * time.Second, stable: time.Minute,
	}
}

// Descriptors returns the verified storage backends in configuration order.
func (s *Supervisor) Descriptors() []Descriptor {
	result := make([]Descriptor, 0, len(s.drivers))
	for _, driver := range s.drivers {
		descriptor := driver.release.descriptor
		descriptor.ConfigSchema = append(json.RawMessage(nil), descriptor.ConfigSchema...)
		result = append(result, descriptor)
	}
	return result
}

// Driver resolves a stable backend ID. Calls during startup, restart or drain
// return the storage RPC unavailable error. The same handle works after relaunch.
func (s *Supervisor) Driver(driverID string) (storagedriver.Driver, error) {
	driver := s.byID[driverID]
	if driver == nil {
		return nil, fmt.Errorf("storage driver %q is not configured", driverID)
	}
	return driver, nil
}

// Ready reports whether an authenticated launch is available for new calls.
// Individual storage bindings still need an authenticated TestConnection.
func (s *Supervisor) Ready(driverID string) bool {
	driver := s.byID[driverID]
	if driver == nil {
		return false
	}
	driver.mu.Lock()
	defer driver.mu.Unlock()
	return driver.available()
}

// Start waits for every configured driver's first handshake. Cancelling ctx
// drains and stops the supervisor, as does Close.
func (s *Supervisor) Start(ctx context.Context) error {
	owner, err := s.startContext(ctx)
	if err != nil {
		return err
	}
	var workers sync.WaitGroup
	lock, err := takeRuntimeLock(s.config.RuntimeRoot)
	var releaseAccounts func()
	if err == nil {
		releaseAccounts, err = s.leaseAccounts()
	}
	if err == nil {
		err = s.startDrivers(owner, &workers)
	}
	go func() {
		workers.Wait()
		if releaseAccounts != nil {
			releaseAccounts()
		}
		if lock != nil {
			_ = lock.Close()
		}
		close(s.done)
	}()
	if err != nil {
		s.cancel()
		<-s.done
		s.mu.Lock()
		defer s.mu.Unlock()
		return errors.Join(err, s.cleanup)
	}
	return nil
}

func (s *Supervisor) startContext(ctx context.Context) (context.Context, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.started || s.closed {
		return nil, errors.New("storage driver supervisor cannot be started again")
	}
	s.started = true
	child, cancel := context.WithCancel(ctx)
	s.cancel = cancel
	return child, nil
}

func (s *Supervisor) startDrivers(ctx context.Context, workers *sync.WaitGroup) error {
	for _, driver := range s.drivers {
		running, err := s.launch(ctx, driver)
		if err != nil {
			return fmt.Errorf("start storage driver %q: %w", driver.release.descriptor.ID, err)
		}
		driver.attach(running)
		workers.Go(func() { s.supervise(ctx, driver, running) })
	}
	return ctx.Err()
}

// Close rejects new calls, drains active calls, and terminates all launched
// processes. Its deadline can force an early end to the grace period.
func (s *Supervisor) Close(ctx context.Context) error {
	s.mu.Lock()
	if !s.closed {
		s.closed = true
		if s.started {
			s.cancel()
		} else {
			close(s.done)
		}
	}
	s.mu.Unlock()
	select {
	case <-s.done:
		s.mu.Lock()
		defer s.mu.Unlock()
		return s.cleanup
	case <-ctx.Done():
		s.forceOnce.Do(func() { close(s.force) })
		return ctx.Err()
	}
}

func (s *Supervisor) supervise(ctx context.Context, driver *managedDriver, running *launch) {
	delay := s.bounds.backoff
	for {
		select {
		case <-ctx.Done():
			drained := driver.detach()
			s.recordCleanup(running.stop(drained, s.force, s.bounds))
			return
		case <-running.exited:
			driver.detach()
			if err := running.cleanup(); err != nil {
				s.recordCleanup(err)
				return
			}
		}
		if time.Since(running.startedAt) >= s.bounds.stable {
			delay = s.bounds.backoff
		}
		for {
			if !waitRetry(ctx, delay) {
				return
			}
			delay = min(s.bounds.maxWait, 2*delay)
			replacement, err := s.launch(ctx, driver)
			if err == nil {
				running = replacement
				driver.attach(running)
				break
			}
		}
	}
}

func (s *Supervisor) recordCleanup(err error) {
	if err != nil {
		s.mu.Lock()
		defer s.mu.Unlock()
		s.cleanup = errors.Join(s.cleanup, err)
	}
}

func waitRetry(ctx context.Context, delay time.Duration) bool {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return ctx.Err() == nil
	}
}

type managedDriver struct {
	selection DriverConfig
	release   verifiedRelease
	timeout   time.Duration

	mu      sync.Mutex
	running *launch
}

var _ storagedriver.Driver = (*managedDriver)(nil)

func (d *managedDriver) attach(running *launch) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.running = running
}

func (d *managedDriver) detach() <-chan struct{} {
	d.mu.Lock()
	defer d.mu.Unlock()
	running := d.running
	d.running = nil
	if running.active == 0 {
		close(running.drained)
	}
	return running.drained
}

// available requires d.mu to be held.
func (d *managedDriver) available() bool {
	if d.running == nil || d.running.owner.Err() != nil {
		return false
	}
	select {
	case <-d.running.exited:
		return false
	default:
		return true
	}
}

func (d *managedDriver) acquire(ctx context.Context) (context.Context, *sdk.Client, func(), error) {
	d.mu.Lock()
	if !d.available() {
		d.mu.Unlock()
		return nil, nil, nil, storagedriver.RPCError{Code: storagedriver.CodeUnavailable}
	}
	running := d.running
	running.active++
	d.mu.Unlock()
	ctx, cancel := context.WithTimeout(ctx, d.timeout)
	release := sync.OnceFunc(func() {
		d.mu.Lock()
		defer d.mu.Unlock()
		running.active--
		if d.running != running && running.active == 0 {
			close(running.drained)
		}
	})
	stop := context.AfterFunc(ctx, release)
	return ctx, running.client, func() {
		stop()
		cancel()
		release()
	}, nil
}

func (d *managedDriver) TestConnection(ctx context.Context, binding storagedriver.Binding) error {
	ctx, client, release, err := d.acquire(ctx)
	if err != nil {
		return err
	}
	defer release()
	return client.TestConnection(ctx, binding)
}

func (d *managedDriver) PutObject(ctx context.Context, req storagedriver.PutObjectRequest, body io.Reader) (storagedriver.PutObjectResult, error) {
	ctx, client, release, err := d.acquire(ctx)
	if err != nil {
		return storagedriver.PutObjectResult{}, err
	}
	defer release()
	return client.PutObject(ctx, req, body)
}

func (d *managedDriver) OpenObject(ctx context.Context, req storagedriver.OpenObjectRequest) (storagedriver.OpenObjectResult, error) {
	ctx, client, release, err := d.acquire(ctx)
	if err != nil {
		return storagedriver.OpenObjectResult{}, err
	}
	result, err := client.OpenObject(ctx, req)
	if err != nil || result.Body == nil {
		release()
		return result, err
	}
	result.Body = &drainingBody{body: result.Body, release: release}
	return result, nil
}

func (d *managedDriver) DeleteKeys(ctx context.Context, req storagedriver.DeleteKeysRequest) error {
	ctx, client, release, err := d.acquire(ctx)
	if err != nil {
		return err
	}
	defer release()
	return client.DeleteKeys(ctx, req)
}

func (d *managedDriver) ListPrefix(ctx context.Context, req storagedriver.ListPrefixRequest) (storagedriver.ListPrefixResult, error) {
	ctx, client, release, err := d.acquire(ctx)
	if err != nil {
		return storagedriver.ListPrefixResult{}, err
	}
	defer release()
	return client.ListPrefix(ctx, req)
}

type drainingBody struct {
	body    io.ReadCloser
	release func()
}

func (b *drainingBody) Read(p []byte) (int, error) {
	n, err := b.body.Read(p)
	if err != nil {
		b.release()
	}
	return n, err
}

func (b *drainingBody) Close() error {
	defer b.release()
	return b.body.Close()
}
