package storagedriverhost

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"syscall"
	"time"

	"github.com/opensoha/soha/contracts/plugins/storagedriver"
	"github.com/opensoha/soha/contracts/plugins/storagedriver/sdk"
	"golang.org/x/sys/unix"
)

const (
	childArg         = "--soha-storage-driver-child"
	driverBinaryPath = "/proc/self/fd/4"
	parentProcessFD  = 5
	childRejected    = 120
	childIsolation   = 121
	childOrphaned    = 122
	childExecFailed  = 123
	childFilesystem  = 124
)

// IsSandboxChild selects the private re-exec entry before Core initializes.
func IsSandboxChild() bool {
	return len(os.Args) == 2 && os.Args[1] == childArg
}

// RunSandboxChild runs as the non-root init of a new Linux PID namespace.
// It owns the parent-death guard and supervises the verified executable.
func RunSandboxChild() int {
	runtime.LockOSThread()
	if !IsSandboxChild() || os.Getpid() != 1 || os.Getuid() < 100 || os.Getgid() < 100 ||
		os.Geteuid() != os.Getuid() || os.Getegid() != os.Getgid() {
		return childRejected
	}
	if err := verifyChildPrivileges(); err != nil {
		return childIsolation
	}
	if err := unix.Prctl(unix.PR_SET_PDEATHSIG, uintptr(unix.SIGKILL), 0, 0, 0); err != nil {
		return childIsolation
	}
	if err := checkParentProcess(); err != nil {
		return childOrphaned
	}
	if err := unix.Setrlimit(unix.RLIMIT_CORE, &unix.Rlimit{}); err != nil {
		return childIsolation
	}
	// Same-UID executable code cannot attach to the guard or access its FDs.
	if err := unix.Prctl(unix.PR_SET_DUMPABLE, 0, 0, 0, 0); err != nil {
		return childIsolation
	}
	// Use CLOEXEC so the verified binary FD remains available for pathname
	// resolution at exec, while all non-stdio FDs are closed before it runs.
	if err := unix.CloseRange(3, uint(^uint32(0)), unix.CLOSE_RANGE_CLOEXEC); err != nil {
		return childIsolation
	}
	if err := restrictDriverFilesystem(); err != nil {
		return childFilesystem
	}
	return runDriverProgram()
}

func runDriverProgram() int {
	signals := make(chan os.Signal, 8)
	signal.Notify(signals, syscall.SIGTERM, syscall.SIGCHLD)
	defer signal.Stop(signals)
	cmd := exec.Command(driverBinaryPath)
	cmd.Args[0] = "soha-storage-driver"
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		return childExecFailed
	}
	_ = os.Stdin.Close()
	defer cmd.Process.Release()
	for {
		if reapNamespaceChildren(cmd.Process.Pid) {
			// The guard's exit destroys the namespace and all other processes.
			return 0
		}
		if <-signals == syscall.SIGTERM {
			_ = cmd.Process.Signal(syscall.SIGTERM)
		}
	}
}

// The namespace init becomes the parent of children orphaned by the driver.
// Wait4 owns all reaping here, including the main driver's exit.
func reapNamespaceChildren(driverPID int) bool {
	for {
		pid, err := unix.Wait4(-1, nil, unix.WNOHANG, nil)
		if errors.Is(err, unix.EINTR) {
			continue
		}
		if err != nil || pid == driverPID {
			return true
		}
		if pid == 0 {
			return false
		}
	}
}

func verifyChildPrivileges() error {
	caps, capErr := currentCapabilities()
	groups, groupErr := os.Getgroups()
	noNewPrivs, prctlErr := unix.PrctlRetInt(unix.PR_GET_NO_NEW_PRIVS, 0, 0, 0, 0)
	if capErr != nil || caps != [2]unix.CapUserData{} || groupErr != nil || len(groups) != 0 ||
		prctlErr != nil || noNewPrivs != 1 {
		return errors.New("storage driver subprocess has unexpected process privileges")
	}
	return nil
}

func checkParentProcess() error {
	defer unix.Close(parentProcessFD)
	fds := []unix.PollFd{{Fd: parentProcessFD, Events: unix.POLLIN}}
	for {
		n, err := unix.Poll(fds, 0)
		if errors.Is(err, unix.EINTR) {
			continue
		}
		if err != nil || n != 0 {
			return errors.New("storage driver parent process unavailable")
		}
		return nil
	}
}

type launch struct {
	owner     context.Context
	client    *sdk.Client
	process   *os.Process
	exited    chan struct{}
	exitErr   error // Written before exited is closed.
	startedAt time.Time
	work      string

	// protected by the owning managedDriver mutex
	active  int
	drained chan struct{}
}

func startProcess(ctx, owner context.Context, work string, binary *os.File, driver *managedDriver) (*launch, error) {
	cmd, bootstrap, closeInherited, err := prepareChild(work, binary, driver.selection)
	if err != nil {
		return nil, err
	}
	defer bootstrap.Close()
	running := &launch{owner: owner, work: work, exited: make(chan struct{}), drained: make(chan struct{})}
	if err := running.start(cmd, closeInherited); err != nil {
		return nil, err
	}
	client, err := establishRPC(ctx, running, bootstrap, driver)
	if err != nil {
		running.kill()
		return nil, err
	}
	running.client = client
	running.startedAt = time.Now()
	return running, nil
}

func prepareChild(work string, binary *os.File, driver DriverConfig) (*exec.Cmd, *os.File, func(), error) {
	core, err := os.Open("/proc/self/exe")
	if err != nil {
		return nil, nil, nil, fmt.Errorf("open storage driver sandbox entry: %w", err)
	}
	pidfd, err := unix.PidfdOpen(os.Getpid(), 0)
	if err != nil {
		_ = core.Close()
		return nil, nil, nil, fmt.Errorf("open storage driver parent process handle: %w", err)
	}
	parent := os.NewFile(uintptr(pidfd), "storage-driver-parent")
	reader, writer, err := os.Pipe()
	if err != nil {
		_ = core.Close()
		_ = parent.Close()
		return nil, nil, nil, err
	}
	cmd := exec.Command("/proc/self/fd/3", childArg)
	cmd.Stdin = reader
	cmd.Dir = work
	cmd.Env = []string{"HOME=" + work, "TMPDIR=" + work, "LANG=C", "PATH=/usr/bin:/bin"}
	cmd.ExtraFiles = []*os.File{core, binary, parent}
	// Killing namespace PID 1 removes all of its descendants, even those that
	// create another process group, session, or nested PID namespace.
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setsid:     true,
		Cloneflags: unix.CLONE_NEWPID,
		Credential: &syscall.Credential{Uid: driver.UID, Gid: driver.GID},
	}
	return cmd, writer, func() {
		_ = core.Close()
		_ = parent.Close()
		_ = reader.Close()
	}, nil
}

func (p *launch) start(cmd *exec.Cmd, closeInherited func()) error {
	started := make(chan error, 1)
	go func() {
		// Both no_new_privs and the armed parent-death watcher are thread
		// scoped. Keep this private OS thread alive until the child is reaped.
		runtime.LockOSThread()
		err := startSandbox(cmd)
		closeInherited()
		started <- err
		if err != nil {
			return
		}
		p.exitErr = cmd.Wait()
		close(p.exited)
	}()
	if err := <-started; err != nil {
		return err
	}
	p.process = cmd.Process
	return nil
}

func startSandbox(cmd *exec.Cmd) error {
	const message = "start isolated storage driver (Linux PID namespace and dedicated account required)"
	if err := unix.Prctl(unix.PR_SET_NO_NEW_PRIVS, 1, 0, 0, 0); err != nil {
		return fmt.Errorf("%s: %w", message, err)
	}
	if err := unix.Prctl(unix.PR_CAP_AMBIENT, unix.PR_CAP_AMBIENT_CLEAR_ALL, 0, 0, 0); err != nil {
		return fmt.Errorf("%s: %w", message, err)
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("%s: %w", message, err)
	}
	return nil
}

func establishRPC(ctx context.Context, p *launch, pipe *os.File, driver *managedDriver) (*sdk.Client, error) {
	var secret [storagedriver.LaunchSecretBytes]byte
	defer clear(secret[:])
	if _, err := rand.Read(secret[:]); err != nil {
		return nil, err
	}
	socket := filepath.Join(p.work, "s")
	if err := sdk.WriteBootstrap(pipe, sdk.Bootstrap{Network: "unix", Address: socket, LaunchSecret: secret[:]}); err != nil {
		return nil, fmt.Errorf("provision storage driver startup pipe: %w", err)
	}
	if err := pipe.Close(); err != nil {
		return nil, err
	}
	if err := awaitSocket(ctx, p, socket, driver.selection); err != nil {
		return nil, err
	}
	// Once the socket is listening, attempt the handshake exactly once. The
	// driver could have accepted an earlier request whose response was lost.
	client, err := sdk.Dial(ctx, sdk.ClientOptions{
		Network: "unix", Address: socket, Expected: driver.release.identity, LaunchSecret: secret[:],
		CallTimeout: driver.timeout,
	})
	if err != nil {
		return nil, fmt.Errorf("authenticate storage driver launch: %w", err)
	}
	return client, nil
}

func awaitSocket(ctx context.Context, p *launch, path string, driver DriverConfig) error {
	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()
	for {
		var st unix.Stat_t
		err := unix.Lstat(path, &st)
		if err == nil {
			if st.Uid != driver.UID || st.Gid != driver.GID || st.Mode&unix.S_IFMT != unix.S_IFSOCK {
				return errors.New("storage driver provided an unsafe Unix socket")
			}
			// The SDK applies mode 0600 just after listen returns.
			if st.Mode&0o7777 == 0o600 {
				return nil
			}
		} else if !errors.Is(err, unix.ENOENT) {
			return fmt.Errorf("inspect storage driver socket: %w", err)
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("storage driver socket did not become ready: %w", ctx.Err())
		case <-p.exited:
			return p.startupExitError()
		case <-ticker.C:
		}
	}
}

func (p *launch) startupExitError() error {
	var exit *exec.ExitError
	if errors.As(p.exitErr, &exit) {
		switch exit.ExitCode() {
		case childRejected, childIsolation:
			return errors.New("storage driver sandbox rejected the effective process identity or privileges")
		case childOrphaned:
			return errors.New("storage driver sandbox could not verify its parent process")
		case childExecFailed:
			return errors.New("storage driver sandbox could not execute the verified binary")
		case childFilesystem:
			return errors.New("storage driver sandbox could not enforce filesystem restrictions with Landlock")
		}
	}
	return errors.New("storage driver exited before its socket became ready")
}

func (p *launch) stop(drained, force <-chan struct{}, bounds deadlines) error {
	waitPhase(drained, p.exited, force, bounds.drain)
	_ = p.client.Close()
	_ = p.process.Signal(syscall.SIGTERM)
	waitPhase(p.exited, nil, force, bounds.term)
	p.kill()
	return p.cleanup()
}

func waitPhase(done, exited, force <-chan struct{}, duration time.Duration) {
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-done:
	case <-exited:
	case <-force:
	case <-timer.C:
	}
}

func (p *launch) kill() {
	_ = p.process.Kill()
	<-p.exited
}

func (p *launch) cleanup() error {
	_ = p.client.Close()
	return os.RemoveAll(filepath.Dir(p.work))
}
