package storagedriverhost

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/opensoha/soha/contracts/plugins/storagedriver"
	"github.com/opensoha/soha/contracts/plugins/storagedriver/sdk"
	"golang.org/x/sys/unix"
)

func TestMain(m *testing.M) {
	switch {
	case IsSandboxChild():
		os.Exit(RunSandboxChild())
	case os.Args[0] == "soha-storage-driver":
		ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM)
		err := sdk.Serve(ctx, os.Stdin, sdk.ServerOptions{PluginID: fixturePlugin, Version: fixtureVersion}, testStorageDriver{})
		stop()
		if err != nil {
			os.Exit(1)
		}
		os.Exit(0)
	case len(os.Args) == 2 && os.Args[1] == "--test-detached-storage-child":
		pid, err := os.Readlink("/proc/self")
		if err != nil || os.WriteFile("detached-pid", []byte(pid), 0o600) != nil {
			os.Exit(1)
		}
		time.Sleep(time.Minute)
		os.Exit(0)
	case len(os.Args) == 2 && os.Args[1] == "--test-orphan-storage-child":
		cmd := exec.Command("/proc/self/exe", "--test-zombie-storage-child")
		if cmd.Start() != nil || cmd.Process.Release() != nil {
			os.Exit(1)
		}
		os.Exit(0)
	case len(os.Args) == 2 && os.Args[1] == "--test-zombie-storage-child":
		pid, err := os.Readlink("/proc/self")
		if err != nil || os.WriteFile("orphan-pid", []byte(pid), 0o600) != nil {
			os.Exit(1)
		}
		os.Exit(0)
	default:
		os.Exit(m.Run())
	}
}

type linuxTestFixture struct {
	cfg          Config
	binary       string
	version      string
	secret       string
	publicSecret string
	digest       [sha256.Size]byte
}

func makeLinuxTestFixture(t *testing.T) linuxTestFixture {
	t.Helper()
	root := os.Getenv("SOHA_STORAGE_DRIVER_TEST_ROOT")
	if root == "" {
		t.Skip("Linux acceptance requires the explicitly provisioned root and dedicated test UID/GID")
	}
	uid, err := strconv.ParseUint(os.Getenv("SOHA_STORAGE_DRIVER_TEST_UID"), 10, 32)
	if err != nil {
		t.Fatal(err)
	}
	gid, err := strconv.ParseUint(os.Getenv("SOHA_STORAGE_DRIVER_TEST_GID"), 10, 32)
	if err != nil {
		t.Fatal(err)
	}
	base, err := os.MkdirTemp(root, "t-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.RemoveAll(base); err != nil {
			t.Error(err)
		}
	})
	mustChmod(t, base, 0o711)
	binaryPath := os.Getenv("SOHA_STORAGE_DRIVER_TEST_BINARY")
	if binaryPath == "" {
		binaryPath = "/proc/self/exe"
	}
	binary, err := os.ReadFile(binaryPath)
	if err != nil {
		t.Fatal(err)
	}
	fixture := newSignedFixture(t, runtime.GOOS+"/"+runtime.GOARCH, binary)
	fixture.config.UID = uint32(uid)
	fixture.config.GID = uint32(gid)
	fixture.config.PublicKeyPath = filepath.Join(base, "key.pem")
	cfg := Config{
		PackageRoot: filepath.Join(base, "packages"), RuntimeRoot: filepath.Join(base, "run"),
		Drivers: []DriverConfig{fixture.config},
	}
	version := filepath.Join(cfg.PackageRoot, fixture.config.PluginID, fixture.config.Version)
	if err := os.MkdirAll(version, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(cfg.RuntimeRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	mustChmod(t, cfg.RuntimeRoot, 0o711)
	manifest, attest, sig := fixture.documents(t)
	artifactPath := filepath.Join(version, fixtureArtifact)
	secretPath := filepath.Join(base, "core-private")
	publicSecretPath := filepath.Join(base, "core-world-readable")
	for name, content := range map[string][]byte{
		filepath.Join(version, manifestName): manifest, filepath.Join(version, attestationName): attest,
		filepath.Join(version, signatureName): sig, fixture.config.PublicKeyPath: fixture.public,
		artifactPath: binary, secretPath: []byte("synthetic Core-only marker"),
	} {
		if err := os.WriteFile(name, content, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(publicSecretPath, []byte("synthetic accidental public Core marker"), 0o600); err != nil {
		t.Fatal(err)
	}
	mustChmod(t, publicSecretPath, 0o644)
	return linuxTestFixture{
		cfg: cfg, binary: artifactPath, version: version, digest: sha256.Sum256(binary),
		secret: secretPath, publicSecret: publicSecretPath,
	}
}

func TestLinuxSupervisorIsolationAndLifecycle(t *testing.T) {
	fixture := makeLinuxTestFixture(t)
	if marker, err := os.ReadFile(fixture.secret); err != nil || string(marker) != "synthetic Core-only marker" {
		t.Fatal("test Core cannot read its own synthetic marker", err)
	}
	if marker, err := os.ReadFile(fixture.publicSecret); err != nil || string(marker) != "synthetic accidental public Core marker" {
		t.Fatal("test Core cannot read the synthetic world-readable marker", err)
	}
	t.Setenv("DATABASE_URL", "synthetic ambient database marker")
	t.Setenv("SOHA_JWT_SECRET", "synthetic ambient jwt marker")
	leakTestFD := testInheritableFD(t, fixture.secret)
	s, err := New(fixture.cfg)
	if err != nil {
		t.Fatal(err)
	}
	s.bounds.backoff = 400 * time.Millisecond
	s.bounds.drain = 3 * time.Second
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
	defer cancel()
	t.Cleanup(func() {
		cleanupCtx, finish := context.WithTimeout(context.Background(), 10*time.Second)
		defer finish()
		if err := s.Close(cleanupCtx); err != nil {
			t.Error(err)
		}
	})
	driver, err := s.Driver(fixtureDriver)
	if err != nil || len(s.Descriptors()) != 1 {
		t.Fatalf("missing verified driver descriptor: %v", err)
	}
	if err := s.Start(ctx); err != nil {
		t.Fatal(err)
	}
	binding := storagedriver.Binding{
		ID: "test-1", Container: "probe", Credentials: map[string]string{"probe": "synthetic-per-call"},
	}
	binding.Settings, err = json.Marshal(testProbe{
		Secret: fixture.secret, PublicSecret: fixture.publicSecret, Binary: fixture.binary, InheritableFD: leakTestFD,
		UID: fixture.cfg.Drivers[0].UID, GID: fixture.cfg.Drivers[0].GID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := driver.TestConnection(ctx, binding); err != nil {
		t.Fatalf("authenticated OS-isolation probe failed: %v", err)
	}
	contents, err := os.ReadFile(fixture.binary)
	if err != nil || sha256.Sum256(contents) != fixture.digest {
		t.Fatal("signed original executable changed during isolation probe", err)
	}
	t.Log("authenticated driver wrote private scratch data, was denied world-readable Core markers and executable writes, and received no inherited secrets or descriptors")
	exerciseOrphanReaping(t, ctx, s, driver)
	exerciseCrash(t, ctx, s, driver)
	exerciseDrain(t, ctx, s, driver)
}

func testInheritableFD(t *testing.T, path string) int {
	t.Helper()
	secret, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer secret.Close()
	fd, err := unix.FcntlInt(secret.Fd(), unix.F_DUPFD, 512)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = unix.Close(fd) })
	return fd
}

func exerciseOrphanReaping(t *testing.T, ctx context.Context, s *Supervisor, driver storagedriver.Driver) {
	t.Helper()
	selection := s.byID[fixtureDriver]
	selection.mu.Lock()
	work := selection.running.work
	selection.mu.Unlock()
	binding := storagedriver.Binding{ID: "test-1", Container: "orphan"}
	if err := driver.TestConnection(ctx, binding); err != nil {
		t.Fatal(err)
	}
	var orphan string
	waitCondition(t, func() bool {
		pid, err := os.ReadFile(filepath.Join(work, "orphan-pid"))
		_, conversion := strconv.Atoi(string(pid))
		if err == nil && conversion == nil {
			orphan = string(pid)
			return true
		}
		return false
	})
	waitCondition(t, func() bool {
		_, err := os.Stat(filepath.Join("/proc", orphan))
		return errors.Is(err, os.ErrNotExist)
	})
	if !s.Ready(fixtureDriver) {
		t.Fatal("reaping an adopted child retired the healthy driver")
	}
	t.Log("namespace PID 1 reaped an orphan while the authenticated driver remained healthy")
}

func exerciseCrash(t *testing.T, ctx context.Context, s *Supervisor, driver storagedriver.Driver) {
	t.Helper()
	current := func() *launch {
		d := s.byID[fixtureDriver]
		d.mu.Lock()
		defer d.mu.Unlock()
		return d.running
	}
	first := current()
	binding := storagedriver.Binding{ID: "test-1", Container: "spawn"}
	if err := driver.TestConnection(ctx, binding); err != nil {
		t.Fatal(err)
	}
	var detached string
	waitCondition(t, func() bool {
		pid, err := os.ReadFile(filepath.Join(first.work, "detached-pid"))
		_, conversion := strconv.Atoi(string(pid))
		if err == nil && conversion == nil {
			detached = string(pid)
			return true
		}
		return false
	})
	binding.Container = "crash"
	if err := driver.TestConnection(ctx, binding); err == nil {
		t.Fatal("crashing RPC unexpectedly succeeded")
	}
	waitCondition(t, func() bool { return !s.Ready(fixtureDriver) })
	start := time.Now()
	waitCondition(t, func() bool { return s.Ready(fixtureDriver) })
	if time.Since(start) < 200*time.Millisecond || current() == first {
		t.Fatal("driver did not wait for crash backoff and a new handshake")
	}
	if _, err := os.Stat(filepath.Join("/proc", detached)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("detached test process %s survived its launch: %v", detached, err)
	}
	t.Log("driver crash left Core alive, detached descendant was killed by the PID namespace, and the handle recovered after backoff")
}

func exerciseDrain(t *testing.T, ctx context.Context, s *Supervisor, driver storagedriver.Driver) {
	t.Helper()
	binding := storagedriver.Binding{ID: "test-1", Container: "stream"}
	result, err := driver.OpenObject(ctx, storagedriver.OpenObjectRequest{Binding: binding, Key: "fixture"})
	if err != nil {
		t.Fatal(err)
	}
	defer result.Body.Close()
	closed := make(chan error, 1)
	go func() { closed <- s.Close(ctx) }()
	waitCondition(t, func() bool { return !s.Ready(fixtureDriver) })
	if err := driver.TestConnection(ctx, binding); err == nil {
		t.Fatal("draining driver accepted a new call")
	}
	select {
	case err := <-closed:
		t.Fatalf("supervisor tore down an active stream: %v", err)
	case <-time.After(100 * time.Millisecond):
	}
	if err := result.Body.Close(); err != nil {
		t.Fatal(err)
	}
	if err := <-closed; err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(s.config.RuntimeRoot)
	if err != nil || len(entries) != 1 || entries[0].Name() != ".soha-storage-driver.lock" {
		t.Fatalf("launch work directory still present: %v (error %v)", entries, err)
	}
	t.Log("drain rejected new requests, kept the open stream until close, and cleaned up the launch")
}

func TestLinuxSupervisorRejectsTampering(t *testing.T) {
	fixture := makeLinuxTestFixture(t)
	s, err := New(fixture.cfg)
	if err != nil {
		t.Fatal(err)
	}
	file, err := os.OpenFile(fixture.binary, os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.WriteAt([]byte{0}, 0); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	if err := s.Start(context.Background()); err == nil || !strings.Contains(err.Error(), "digest") || s.Ready(fixtureDriver) {
		t.Fatalf("tampered artifact was not refused before launch: %v", err)
	}
	if err := s.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(fixture.version, fixture.version+"-real"); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(fixture.version+"-real", fixture.version); err != nil {
		t.Fatal(err)
	}
	if _, err := New(fixture.cfg); err == nil {
		t.Fatal("symlinked release directory accepted")
	}
	fixture.cfg.Drivers[0].UID = 0
	if _, err := New(fixture.cfg); err == nil || !strings.Contains(err.Error(), "non-privileged") {
		t.Fatalf("unsafe driver identity accepted: %v", err)
	}
}

func TestLocalAccountsRejectSharedIdentity(t *testing.T) {
	accounts := localAccounts{
		users:  []string{"driver:x:901:901::/nonexistent:/usr/sbin/nologin"},
		groups: []string{"driver:x:901:"},
	}
	used := driverSelections{plugins: make(map[string]bool), users: make(map[uint32]bool), groups: make(map[uint32]bool)}
	pin := DriverConfig{PluginID: fixturePlugin, Version: fixtureVersion, UID: 901, GID: 901}
	if err := used.add(pin, accounts); err != nil {
		t.Fatal(err)
	}
	pin.PluginID = "example.second"
	if err := used.add(pin, accounts); err == nil {
		t.Fatal("accepted a second plugin with the same host identity")
	}
	accounts.users = append(accounts.users, "other:x:902:901::/nonexistent:/usr/sbin/nologin")
	if err := accounts.validate(901, 901); err == nil {
		t.Fatal("accepted a shared primary group")
	}
}

func waitCondition(t *testing.T, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(8 * time.Second)
	for !condition() && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if !condition() {
		t.Fatal("condition did not become true before deadline")
	}
}

func mustChmod(t *testing.T, path string, mode os.FileMode) {
	t.Helper()
	if err := os.Chmod(path, mode); err != nil {
		t.Fatal(err)
	}
}

type testProbe struct {
	Secret        string
	PublicSecret  string
	Binary        string
	InheritableFD int
	UID           uint32
	GID           uint32
}

type testStorageDriver struct {
	storagedriver.Driver // Only TestConnection and OpenObject are used by this fixture.
}

func (testStorageDriver) TestConnection(_ context.Context, binding storagedriver.Binding) error {
	switch binding.Container {
	case "crash":
		os.Exit(42)
	case "spawn":
		cmd := exec.Command("/proc/self/exe", "--test-detached-storage-child")
		cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
		if err := cmd.Start(); err != nil {
			return err
		}
		return cmd.Process.Release()
	case "probe":
		return runIsolationProbe(binding)
	case "orphan":
		return exec.Command("/proc/self/exe", "--test-orphan-storage-child").Run()
	}
	return nil
}

func runIsolationProbe(binding storagedriver.Binding) error {
	var probe testProbe
	if err := json.Unmarshal(binding.Settings, &probe); err != nil {
		return err
	}
	credentials := binding.Credentials
	groups, err := os.Getgroups()
	if err != nil || len(groups) != 0 || os.Geteuid() != int(probe.UID) || os.Getegid() != int(probe.GID) ||
		os.Getenv("DATABASE_URL") != "" || os.Getenv("SOHA_JWT_SECRET") != "" || credentials["probe"] != "synthetic-per-call" {
		return errors.New("unexpected effective account or inherited environment")
	}
	if _, err := os.ReadFile(probe.Secret); !errors.Is(err, os.ErrPermission) {
		return errors.New("Core-only secret was accessible")
	}
	var inherited unix.Stat_t
	if err := unix.Fstat(probe.InheritableFD, &inherited); !errors.Is(err, unix.EBADF) {
		return errors.New("storage driver received an unrelated Core file descriptor")
	}
	for _, path := range []string{probe.PublicSecret, "/proc/self/root" + probe.PublicSecret} {
		if _, err := os.ReadFile(path); !errors.Is(err, os.ErrPermission) {
			return errors.New("world-readable Core secret was accessible through the storage sandbox")
		}
	}
	for _, path := range []string{probe.Binary, "/proc/self/exe"} {
		if file, err := os.OpenFile(path, os.O_WRONLY, 0); err == nil {
			_ = file.Close()
			return fmt.Errorf("write access to driver executable: %s", path)
		}
	}
	return os.WriteFile("driver-owned-test", []byte("writable"), 0o600)
}

func (testStorageDriver) OpenObject(ctx context.Context, _ storagedriver.OpenObjectRequest) (storagedriver.OpenObjectResult, error) {
	return storagedriver.OpenObjectResult{
		Body: blockedStream{ctx}, ContentLength: 8, ContentType: "application/octet-stream", ETag: `"fixture"`,
	}, nil
}

type blockedStream struct{ context.Context }

func (b blockedStream) Read([]byte) (int, error) {
	<-b.Done()
	return 0, io.ErrUnexpectedEOF
}

func (blockedStream) Close() error { return nil }
