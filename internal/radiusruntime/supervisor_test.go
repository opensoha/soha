//go:build linux || darwin

package radiusruntime

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func TestMain(m *testing.M) {
	if len(os.Args) == 2 && os.Args[1] == "-v" {
		fmt.Println("FreeRADIUS Version 3.2.10")
		os.Exit(0)
	}
	if os.Getenv("SOHA_RADIUS_H1_FIXTURE") == "true" && len(os.Args) > 1 && os.Args[1] == "-f" {
		signals := make(chan os.Signal, 1)
		signal.Notify(signals, os.Interrupt)
		if conn, err := net.DialTimeout("unix", os.Getenv("SOHA_RADIUS_HOOK_SOCKET"), time.Second); err == nil { // #nosec G704 -- Fixture dials only a local Unix socket inherited from the test supervisor; no HTTP or remote address is used.
			_ = conn.Close()
		}
		if os.Getenv("SOHA_RADIUS_FIXTURE_IGNORE_INTERRUPT") == "true" {
			<-time.After(12 * time.Second)
			os.Exit(0)
		}
		select {
		case <-signals:
			os.Exit(0)
		case <-time.After(12 * time.Second):
			os.Exit(0)
		}
	}
	os.Exit(m.Run())
}

type fixtureControl struct {
	mu       sync.Mutex
	hash     string
	revision uint64
	run      bool
	fail     bool
	lease    time.Duration
	until    time.Time
}

func (c *fixtureControl) Fetch(ctx context.Context, epoch string) (Lease, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.fail {
		return Lease{}, errors.New("offline")
	}
	if err := ctx.Err(); err != nil {
		return Lease{}, err
	}
	now := time.Now()
	return Lease{Control: Control{Epoch: epoch, Binding: strings.Repeat("a", 64), Revision: c.revision, ConfigValidUntil: now.Add(c.lease), LeaseValidUntil: now.Add(c.lease)}, ConfigurationSHA256: c.hash, CertificateValidUntil: c.until, HardUntil: c.until, RunProcess: c.run, AcceptNew: true, NASID: "nas-1", RuntimeProof: `{"poolId":"fixture"}`}, nil
}

type fixtureObserver struct{ observations chan Observation }

func (o *fixtureObserver) Record(ctx context.Context, v Observation) error {
	select {
	case o.observations <- v:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
func certificateFixture(t *testing.T, path string, until time.Time) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "fixture"}, NotBefore: time.Now().Add(-time.Minute), NotAfter: until}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(path, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0600); err != nil {
		t.Fatal(err)
	}
}
func supervisorFixture(t *testing.T) (*Supervisor, *fixtureControl, *fixtureObserver) {
	t.Helper()
	base, err := os.MkdirTemp("/private/tmp", "task122-h1-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(base) })
	configDir := filepath.Join(base, "config")
	if err = os.Mkdir(configDir, 0700); err != nil {
		t.Fatal(err)
	}
	cert := filepath.Join(configDir, "server.crt")
	until := time.Now().Add(time.Hour)
	certificateFixture(t, cert, until)
	if err = os.WriteFile(filepath.Join(configDir, "radiusd.conf"), []byte("immutable fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	binary, err = filepath.EvalSymlinks(binary)
	if err != nil {
		t.Fatal(err)
	}
	binaryHash, err := hashFile(binary)
	if err != nil {
		t.Fatal(err)
	}
	paths := fixturePaths(base)
	tree, err := configurationDigest(configDir, paths)
	if err != nil {
		t.Fatal(err)
	}
	state := filepath.Join(base, "owner")
	socket := filepath.Join(state, "hook.sock")
	c := Installation{Owner: "task122-h1-fixture", StateDir: state, Socket: socket, Binary: binary, BinarySHA256: binaryHash, ConfigurationDir: configDir, ConfigurationSHA256: tree, Certificates: []string{cert}, FreeradUID: 101, FreeradGID: 101, StopGraceSeconds: 1, MaxStates: 128, MaxPending: 32, Environment: map[string]string{"SOHA_RADIUS_H1_FIXTURE": "true"}, Hooks: HookAttestation{ConfigurationSHA256: tree, Socket: socket, Routes: []string{"/admit", "/challenge", "/finish"}, NonceAttribute: "control:Tmp-String-8", ControlOnly: true, EpochEnvironment: "SOHA_RADIUS_PROCESS_EPOCH", SocketEnvironment: "SOHA_RADIUS_HOOK_SOCKET"}}
	control := &fixtureControl{hash: tree, run: true, revision: 1, lease: 10 * time.Second, until: until}
	observer := &fixtureObserver{observations: make(chan Observation, 32)}
	host := hostOps{lstat: paths.lstat, supported: func() bool { return true }, ownership: func(os.FileInfo) (uint32, uint32, bool) { return 0, 0, true }, lock: func(f *os.File) error { return unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB) }, startToken: func(pid int) (string, error) { return strconv.Itoa(pid), nil }, configure: func(*exec.Cmd) {}, chown: func(string, int, int) error { return nil }, pollInterval: 10 * time.Millisecond}
	supervisor, err := newSupervisor(c, control, observer, host)
	if err != nil {
		t.Fatal(err)
	}
	host.peer = func(net.Conn) (uint32, int, error) {
		supervisor.mu.Lock()
		defer supervisor.mu.Unlock()
		return 101, supervisor.childRecord.PID, nil
	}
	supervisor.host = host
	return supervisor, control, observer
}
func runningFixture(t *testing.T, setup ...func(*Supervisor)) (*Supervisor, *fixtureControl, *fixtureObserver, context.CancelFunc, chan error) {
	t.Helper()
	supervisor, control, observer := supervisorFixture(t)
	for _, configure := range setup {
		configure(supervisor)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- supervisor.Run(ctx) }()
	deadline := time.After(8 * time.Second)
	started := false
	for !started {
		select {
		case observation := <-observer.observations:
			if observation.ProcessStopped {
				t.Fatalf("unexpected startup %+v", observation)
			}
			started = observation.HooksReady
		case err := <-done:
			t.Fatal("start failed", err)
		case <-deadline:
			t.Fatal("start timed out")
		}
	}
	t.Cleanup(func() {
		cancel()
		select {
		case <-done:
		case <-time.After(4 * time.Second):
			if supervisor.command != nil {
				_ = supervisor.command.Process.Kill()
			}
			t.Error("fixture cleanup timed out")
		}
	})
	return supervisor, control, observer, cancel, done
}

func TestSupervisorGraceTimeoutKillsAndWaitsOwnedFixture(t *testing.T) {
	supervisor, _, observer, cancel, done := runningFixture(t, func(s *Supervisor) { s.config.Environment["SOHA_RADIUS_FIXTURE_IGNORE_INTERRUPT"] = "true" })
	before := time.Now()
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
		done <- context.Canceled
	case <-time.After(4 * time.Second):
		t.Fatal("force stop unconfirmed")
	}
	if time.Since(before) < time.Second {
		t.Fatal("ignored interrupt did not exercise fixed grace")
	}
	observation := <-observer.observations
	if !observation.ProcessStopped || observation.State.Active == nil || *observation.State.Active != 0 {
		t.Fatal("kill lacked wait proof", observation)
	}
	select {
	case <-supervisor.done:
	default:
		t.Fatal("owned wait missing")
	}
}

func TestSupervisorDisconnectedControlStopsOwnedProcess(t *testing.T) {
	_, control, observer, _, done := runningFixture(t)
	control.mu.Lock()
	control.fail = true
	control.mu.Unlock()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("disconnect treated as success")
		}
		done <- err
	case <-time.After(4 * time.Second):
		t.Fatal("disconnected child survived")
	}
	for {
		select {
		case observation := <-observer.observations:
			if observation.ProcessStopped {
				if observation.HooksReady || !observation.State.Known || observation.State.Active == nil || *observation.State.Active != 0 {
					t.Fatal("invalid stop receipt", observation)
				}
				return
			}
		default:
			t.Fatal("no stopped receipt")
		}
	}
}
func TestSupervisorCancellationStopsAndWaitsExactFixture(t *testing.T) {
	supervisor, _, observer, cancel, done := runningFixture(t)
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
		done <- context.Canceled
	case <-time.After(4 * time.Second):
		t.Fatal("stop timed out")
	}
	select {
	case observation := <-observer.observations:
		if !observation.ProcessStopped || observation.HooksReady || !observation.State.Known || observation.State.Active == nil || *observation.State.Active != 0 {
			t.Fatalf("unconfirmed stop %+v", observation)
		}
	case <-time.After(time.Second):
		t.Fatal("missing stop receipt")
	}
	if _, err := os.Lstat(supervisor.config.Socket); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("socket retained", err)
	}
	var record ownerRecord
	data, err := os.ReadFile(filepath.Join(supervisor.config.StateDir, "child.json"))
	if err != nil || json.Unmarshal(data, &record) != nil || !record.Stopped {
		t.Fatal("stop marker missing")
	}
}
func TestSupervisorUnknownForcesStopBeforeKnownZero(t *testing.T) {
	supervisor, _, observer, _, done := runningFixture(t)
	supervisor.tracker.MarkUnknown()
	select {
	case err := <-done:
		if !errors.Is(err, ErrUnknown) {
			t.Fatal(err)
		}
		done <- ErrUnknown
	case <-time.After(4 * time.Second):
		t.Fatal("unknown child survived")
	}
	observation := <-observer.observations
	if !observation.ProcessStopped || observation.State.Active == nil || *observation.State.Active != 0 {
		t.Fatal("no confirmed stop", observation)
	}
}
func TestSupervisorLockAndOrphanRefuseAdoption(t *testing.T) {
	supervisor, _, _, _, _ := runningFixture(t)
	contender, err := newSupervisor(supervisor.config, supervisor.source, supervisor.observer, supervisor.host)
	if err != nil {
		t.Fatal(err)
	}
	if err = contender.acquire(); err == nil {
		t.Fatal("second owner acquired lock")
	}
	_ = contender.close()
	orphan, _, _ := supervisorFixture(t)
	if err = orphan.acquire(); err != nil {
		t.Fatal(err)
	}
	orphan.childRecord = ownerRecord{Owner: orphan.config.Owner, PID: 12345, StartToken: "old", Epoch: "old", Stopped: false}
	if err = orphan.saveRecord(); err != nil {
		t.Fatal(err)
	}
	_ = orphan.close()
	replacement, err := newSupervisor(orphan.config, orphan.source, orphan.observer, orphan.host)
	if err != nil {
		t.Fatal(err)
	}
	if err = replacement.acquire(); err == nil {
		t.Fatal("orphan adopted")
	}
	_ = replacement.close()
}
func TestSupervisorMismatchedPIDDoesNotSignalOrACKStopped(t *testing.T) {
	supervisor, _, observer, cancel, done := runningFixture(t)
	supervisor.host.startToken = func(int) (string, error) { return "replaced-incarnation", nil }
	cancel()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("identity mismatch ignored")
		}
		done <- err
	case <-time.After(2 * time.Second):
		t.Fatal("stop hung")
	}
	select {
	case observation := <-observer.observations:
		if observation.ProcessStopped {
			t.Fatal("unconfirmed child reported stopped")
		}
	default:
	}
	// This test owns this exact fixture handle; clean it without touching any PID lookup.
	_ = supervisor.command.Process.Kill()
	select {
	case <-supervisor.done:
	case <-time.After(time.Second):
		t.Fatal("fixture wait failed")
	}
}
func TestSupervisorLeaseLossAndArtifactDriftFailClosed(t *testing.T) {
	for _, kind := range []string{"source-error", "config-drift", "certificate-expired"} {
		t.Run(kind, func(t *testing.T) {
			supervisor, control, _, _, _ := runningFixture(t)
			switch kind {
			case "source-error":
				control.mu.Lock()
				control.fail = true
				control.mu.Unlock()
			case "config-drift":
				_ = os.WriteFile(filepath.Join(supervisor.config.ConfigurationDir, "radiusd.conf"), []byte("drift"), 0600)
			case "certificate-expired":
				certificateFixture(t, supervisor.config.Certificates[0], time.Now().Add(-time.Second))
			}
			if supervisor.refresh(context.Background()) == nil {
				t.Fatal("invalid control/material accepted")
			}
		})
	}
}
func TestSupervisorRenewKeepsBindingAndReadback(t *testing.T) {
	supervisor, _, _, _, _ := runningFixture(t)
	if err := supervisor.refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	if snapshot := supervisor.tracker.Snapshot(); !snapshot.Known || snapshot.Revision != 1 {
		t.Fatal(snapshot)
	}
	lease := supervisor.leaseState
	lease.Control.Binding = strings.Repeat("b", 64)
	if supervisor.applyLease(lease) == nil {
		t.Fatal("binding change hot applied")
	}
}
func TestSupervisorPolicyRejectsUnsafeHooksPathsAndInlineSecret(t *testing.T) {
	supervisor, _, _ := supervisorFixture(t)
	for _, kind := range []string{"hash", "hook", "symlink", "secret", "foreign-directory"} {
		t.Run(kind, func(t *testing.T) {
			c := supervisor.config
			c.Environment = map[string]string{}
			switch kind {
			case "hash":
				c.BinarySHA256 = "bad"
			case "hook":
				c.Hooks.ControlOnly = false
			case "symlink":
				alias := filepath.Join(filepath.Dir(c.StateDir), "alias")
				_ = os.Symlink(c.ConfigurationDir, alias)
				c.ConfigurationDir = alias
			case "secret":
				c.Environment["SOHA_RADIUS_SHARED_SECRET"] = "not-a-file"
			case "foreign-directory":
				_ = os.Mkdir(c.StateDir, 0700)
				_ = os.WriteFile(filepath.Join(c.StateDir, "keep"), []byte("foreign"), 0600)
				if err := supervisor.acquire(); err == nil {
					t.Fatal("foreign state adopted")
				}
				_ = supervisor.close()
				return
			}
			if validateInstallation(c) == nil {
				t.Fatal("unsafe installation accepted")
			}
		})
	}
}

func TestSupervisorPinsManagedEnvironmentAndPrewarm(t *testing.T) {
	supervisor, _, _ := supervisorFixture(t)
	supervisor.config.Environment["SOHA_RADIUS_HOOK_POOL_START"] = "0"
	supervisor.config.Environment["SOHA_RADIUS_MANAGED_ENABLED"] = "false"
	values := map[string]string{}
	for _, entry := range supervisor.environment("fresh-epoch") {
		key, value, _ := strings.Cut(entry, "=")
		values[key] = value
	}
	if values["SOHA_RADIUS_HOOK_POOL_START"] != "1" || values["SOHA_RADIUS_MANAGED_ENABLED"] != "true" || values["SOHA_RADIUS_PROCESS_EPOCH"] != "fresh-epoch" || values["SOHA_RADIUS_HOOK_SOCKET"] != supervisor.config.Socket {
		t.Fatal("unsafe managed environment", values)
	}
}
