package storagedriverhost

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/opensoha/soha/contracts/plugins/storagedriver"
	"golang.org/x/sys/unix"
)

// New verifies the locally selected releases before exposing their descriptors.
// It leaves process startup and the runtime directory lease to Start.
func New(cfg Config) (*Supervisor, error) {
	if len(cfg.Drivers) == 0 || len(cfg.Drivers) > 32 {
		return nil, errors.New("storage driver supervisor needs between 1 and 32 configured releases")
	}
	if err := checkIsolation(); err != nil {
		return nil, err
	}
	// A Unix pathname socket on Linux has 107 usable bytes, including the
	// runtime prefix and our bounded launch directory name.
	if len(cfg.RuntimeRoot)+17 > 107 {
		return nil, errors.New("storage driver runtime root is too long for a Unix socket")
	}
	accounts, err := readLocalAccounts()
	if err != nil {
		return nil, err
	}
	bounds := defaultDeadlines()
	s := &Supervisor{
		config: Config{PackageRoot: cfg.PackageRoot, RuntimeRoot: cfg.RuntimeRoot}, bounds: bounds,
		byID: make(map[string]*managedDriver), done: make(chan struct{}), force: make(chan struct{}),
	}
	used := driverSelections{plugins: make(map[string]bool), users: make(map[uint32]bool), groups: make(map[uint32]bool)}
	for _, selection := range cfg.Drivers {
		if err := used.add(selection, accounts); err != nil {
			return nil, err
		}
		driver, err := s.newDriver(selection)
		if err != nil {
			return nil, fmt.Errorf("verify storage driver package %q: %w", selection.PluginID, err)
		}
		id := driver.release.descriptor.ID
		if s.byID[id] != nil {
			return nil, fmt.Errorf("duplicate signed storage driver ID %q", id)
		}
		s.drivers = append(s.drivers, driver)
		s.byID[id] = driver
	}
	return s, nil
}

func checkIsolation() error {
	if os.Getuid() != 0 || os.Geteuid() != 0 || os.Getgid() != 0 || os.Getegid() != 0 {
		return errors.New("storage driver isolation requires a root Core service and dedicated non-root driver accounts")
	}
	var filesystem unix.Statfs_t
	if err := unix.Statfs("/proc", &filesystem); err != nil || filesystem.Type != unix.PROC_SUPER_MAGIC {
		return errors.New("storage driver isolation requires a Linux proc filesystem")
	}
	caps, err := currentCapabilities()
	if err != nil {
		return fmt.Errorf("inspect storage driver supervisor privileges: %w", err)
	}
	for _, capability := range []int{unix.CAP_CHOWN, unix.CAP_DAC_OVERRIDE, unix.CAP_FOWNER, unix.CAP_KILL,
		unix.CAP_SETUID, unix.CAP_SETGID, unix.CAP_SYS_ADMIN} {
		if caps[capability/32].Effective&(uint32(1)<<uint(capability%32)) == 0 {
			return errors.New("storage driver isolation requires Core capability to create PID namespaces and manage driver accounts")
		}
	}
	return requireLandlock()
}

func currentCapabilities() ([2]unix.CapUserData, error) {
	header := unix.CapUserHeader{Version: unix.LINUX_CAPABILITY_VERSION_3}
	var caps [2]unix.CapUserData
	err := unix.Capget(&header, &caps[0])
	return caps, err
}

type driverSelections struct {
	plugins map[string]bool
	users   map[uint32]bool
	groups  map[uint32]bool
}

func (used driverSelections) add(selection DriverConfig, accounts localAccounts) error {
	identity := storagedriver.Identity{
		PluginID: selection.PluginID, Version: selection.Version, ProtocolMajor: storagedriver.ProtocolMajor,
		Capabilities: storagedriver.V1Capabilities,
	}
	if identity.Validate() != nil {
		return errors.New("invalid configured storage driver plugin identity")
	}
	if used.plugins[selection.PluginID] || used.users[selection.UID] || used.groups[selection.GID] {
		return errors.New("storage driver packages must use distinct plugin IDs and dedicated UID/GID pairs")
	}
	if err := accounts.validate(selection.UID, selection.GID); err != nil {
		return fmt.Errorf("storage driver %q: %w", selection.PluginID, err)
	}
	used.plugins[selection.PluginID] = true
	used.users[selection.UID] = true
	used.groups[selection.GID] = true
	return nil
}

type localAccounts struct {
	users  []string
	groups []string
}

func readLocalAccounts() (localAccounts, error) {
	dir, err := openTrustedDir("/etc", false)
	if err != nil {
		return localAccounts{}, err
	}
	defer dir.Close()
	passwd, err := readRegularAt(dir, "passwd", maxDocument)
	if err != nil {
		return localAccounts{}, err
	}
	group, err := readRegularAt(dir, "group", maxDocument)
	if err != nil {
		return localAccounts{}, err
	}
	return localAccounts{users: strings.Split(string(passwd), "\n"), groups: strings.Split(string(group), "\n")}, nil
}

func (a localAccounts) validate(uid, gid uint32) error {
	if !dedicatedID(uid) || !dedicatedID(gid) {
		return errors.New("storage driver requires dedicated non-privileged numeric UID and GID")
	}
	id := strconv.FormatUint(uint64(uid), 10)
	groupID := strconv.FormatUint(uint64(gid), 10)
	name, err := a.accountName(id, groupID)
	if err != nil {
		return err
	}
	matched := false
	for _, line := range a.groups {
		fields := strings.Split(line, ":")
		if len(fields) != 4 || fields[2] != groupID {
			continue
		}
		if matched || (fields[3] != "" && fields[3] != name) {
			return errors.New("storage driver group must be private to its configured account")
		}
		matched = true
	}
	if !matched {
		return errors.New("storage driver group must be provisioned in /etc/group")
	}
	return nil
}

func (a localAccounts) accountName(uid, gid string) (string, error) {
	name := ""
	for _, line := range a.users {
		fields := strings.Split(line, ":")
		if len(fields) != 7 || (fields[2] != uid && fields[3] != gid) {
			continue
		}
		if name != "" || fields[2] != uid || fields[3] != gid || !nonLoginShell(fields[6]) {
			return "", errors.New("storage driver requires a unique local non-login user with the configured primary group")
		}
		name = fields[0]
	}
	if name == "" {
		return "", errors.New("storage driver account must be provisioned in /etc/passwd")
	}
	return name, nil
}

func nonLoginShell(shell string) bool {
	switch shell {
	case "/sbin/nologin", "/usr/sbin/nologin", "/bin/false", "/usr/bin/false":
		return true
	default:
		return false
	}
}

func dedicatedID(value uint32) bool {
	return value >= 100 && value <= 1<<31-1 && value != 65534
}

func (s *Supervisor) newDriver(selection DriverConfig) (*managedDriver, error) {
	dir, err := openPackageDir(s.config.PackageRoot, selection)
	if err != nil {
		return nil, err
	}
	defer dir.Close()
	release, err := loadPackage(dir, selection)
	if err != nil {
		return nil, err
	}
	return &managedDriver{selection: selection, release: release, timeout: s.bounds.call}, nil
}

func (s *Supervisor) leaseAccounts() (func(), error) {
	var leases []int
	release := func() {
		for _, fd := range leases {
			_ = unix.Close(fd)
		}
	}
	for _, driver := range s.drivers {
		for kind, id := range map[string]uint32{"uid": driver.selection.UID, "gid": driver.selection.GID} {
			fd, err := leaseAccount(kind, id)
			if err != nil {
				release()
				return nil, err
			}
			leases = append(leases, fd)
		}
	}
	return release, nil
}

func leaseAccount(kind string, id uint32) (int, error) {
	// The Linux abstract datagram socket is only an exclusive kernel name.
	// No transport, application requests or filesystem paths use this FD.
	fd, err := unix.Socket(unix.AF_UNIX, unix.SOCK_DGRAM|unix.SOCK_CLOEXEC|unix.SOCK_NONBLOCK, 0)
	if err != nil {
		return -1, err
	}
	addr := &unix.SockaddrUnix{Name: fmt.Sprintf("@soha-storage-driver-%s-%d", kind, id)}
	if err := unix.Bind(fd, addr); err != nil {
		_ = unix.Close(fd)
		return -1, fmt.Errorf("storage driver %s %d already leased or unavailable: %w", kind, id, err)
	}
	return fd, nil
}

func (s *Supervisor) launch(owner context.Context, driver *managedDriver) (*launch, error) {
	ctx, cancel := context.WithTimeout(owner, s.bounds.startup)
	defer cancel()
	binary, err := s.openLaunchBinary(ctx, driver)
	if err != nil {
		return nil, err
	}
	defer binary.Close()
	work, err := makeWorkDirectory(s.config.RuntimeRoot, driver.selection)
	if err != nil {
		return nil, err
	}
	running, err := startProcess(ctx, owner, work, binary, driver)
	if err != nil {
		return nil, errors.Join(err, os.RemoveAll(filepath.Dir(work)))
	}
	return running, nil
}

func (s *Supervisor) openLaunchBinary(ctx context.Context, driver *managedDriver) (*os.File, error) {
	accounts, err := readLocalAccounts()
	if err != nil {
		return nil, err
	}
	if err := accounts.validate(driver.selection.UID, driver.selection.GID); err != nil {
		return nil, err
	}
	dir, err := openPackageDir(s.config.PackageRoot, driver.selection)
	if err != nil {
		return nil, err
	}
	defer dir.Close()
	release, err := loadPackage(dir, driver.selection)
	if err != nil {
		return nil, err
	}
	if release.attestationDigest != driver.release.attestationDigest || release.publisherDigest != driver.release.publisherDigest {
		return nil, errors.New("pinned storage driver release or publisher changed since supervisor construction")
	}
	return sealedBinary(ctx, dir, release.binary)
}

func makeWorkDirectory(root string, driver DriverConfig) (string, error) {
	dir, err := openTrustedDir(root, true)
	if err != nil {
		return "", err
	}
	defer dir.Close()
	if err := requireMode(dir, runtimeDirMode); err != nil {
		return "", err
	}
	parent, err := os.MkdirTemp(root, "d-")
	if err != nil {
		return "", err
	}
	work := filepath.Join(parent, "w")
	if err := initializeWorkDirectory(parent, work, driver); err != nil {
		return "", errors.Join(err, os.RemoveAll(parent))
	}
	return work, nil
}

func initializeWorkDirectory(parent, work string, driver DriverConfig) error {
	if err := os.Mkdir(work, 0o700); err != nil {
		return err
	}
	if err := os.Chmod(work, 0o700); err != nil {
		return err
	}
	if err := os.Chown(work, int(driver.UID), int(driver.GID)); err != nil {
		return err
	}
	if err := os.Chown(parent, 0, int(driver.GID)); err != nil {
		return err
	}
	return os.Chmod(parent, 0o710)
}
