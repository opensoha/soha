package storagedriverhost

import (
	"errors"
	"fmt"
	"path/filepath"
	"runtime"
	"unsafe"

	"golang.org/x/sys/unix"
)

const (
	minimumLandlockABI = 6

	readFile   = unix.LANDLOCK_ACCESS_FS_READ_FILE
	execute    = unix.LANDLOCK_ACCESS_FS_EXECUTE
	writeFile  = unix.LANDLOCK_ACCESS_FS_WRITE_FILE
	workAccess = readFile | writeFile | unix.LANDLOCK_ACCESS_FS_READ_DIR |
		unix.LANDLOCK_ACCESS_FS_TRUNCATE | unix.LANDLOCK_ACCESS_FS_REFER |
		unix.LANDLOCK_ACCESS_FS_MAKE_DIR | unix.LANDLOCK_ACCESS_FS_MAKE_REG |
		unix.LANDLOCK_ACCESS_FS_MAKE_SOCK | unix.LANDLOCK_ACCESS_FS_MAKE_SYM |
		unix.LANDLOCK_ACCESS_FS_REMOVE_DIR | unix.LANDLOCK_ACCESS_FS_REMOVE_FILE
	handledFiles = workAccess | execute | unix.LANDLOCK_ACCESS_FS_MAKE_FIFO |
		unix.LANDLOCK_ACCESS_FS_MAKE_CHAR | unix.LANDLOCK_ACCESS_FS_MAKE_BLOCK |
		unix.LANDLOCK_ACCESS_FS_IOCTL_DEV
)

func requireLandlock() error {
	version, _, err := unix.Syscall(unix.SYS_LANDLOCK_CREATE_RULESET, 0, 0, unix.LANDLOCK_CREATE_RULESET_VERSION)
	if err != 0 {
		return fmt.Errorf("storage driver isolation requires enabled Linux Landlock ABI %d or later: %w", minimumLandlockABI, err)
	}
	if version < minimumLandlockABI {
		return fmt.Errorf("storage driver isolation requires Linux Landlock ABI %d or later (found %d)", minimumLandlockABI, version)
	}
	return nil
}

// restrictDriverFilesystem runs on the same locked thread that forks the
// driver, so the new process inherits all restrictions before its first exec.
func restrictDriverFilesystem() error {
	attr := unix.LandlockRulesetAttr{
		Access_fs: handledFiles,
		Scoped:    unix.LANDLOCK_SCOPE_ABSTRACT_UNIX_SOCKET | unix.LANDLOCK_SCOPE_SIGNAL,
	}
	fd, _, err := unix.Syscall(unix.SYS_LANDLOCK_CREATE_RULESET, uintptr(unsafe.Pointer(&attr)), unsafe.Sizeof(attr), 0) // #nosec G103 -- The syscall takes a pointer to the fixed Linux Landlock ruleset ABI.
	if err != 0 {
		return err
	}
	defer func() { _ = unix.Close(int(fd)) }()
	if err := addFileRule(fd, ".", workAccess, false); err != nil {
		return err
	}
	if err := addSystemRuntimeRules(fd); err != nil {
		return err
	}
	if _, _, err := unix.Syscall(unix.SYS_LANDLOCK_RESTRICT_SELF, fd, 0, 0); err != 0 {
		return err
	}
	return nil
}

func addFileRule(ruleset uintptr, path string, access uint64, optional bool) error {
	fd, err := unix.Open(path, unix.O_PATH|unix.O_CLOEXEC, 0)
	if optional && errors.Is(err, unix.ENOENT) {
		return nil
	}
	if err != nil {
		return err
	}
	defer func() { _ = unix.Close(fd) }()
	attr := unix.LandlockPathBeneathAttr{Allowed_access: access, Parent_fd: int32(fd)}                                                                       // #nosec G115 -- Linux file descriptors returned by open are signed 32-bit values.
	if _, _, err := unix.Syscall6(unix.SYS_LANDLOCK_ADD_RULE, ruleset, unix.LANDLOCK_RULE_PATH_BENEATH, uintptr(unsafe.Pointer(&attr)), 0, 0, 0); err != 0 { // #nosec G103 -- The syscall takes a pointer to the fixed Linux path rule ABI.
		return err
	}
	return nil
}

func addSystemRuntimeRules(ruleset uintptr) error {
	if err := addFileRule(ruleset, "/dev/null", readFile|writeFile, false); err != nil {
		return err
	}
	for _, path := range []string{
		"/dev/urandom", "/dev/random",
		"/etc/resolv.conf", "/etc/hosts", "/etc/nsswitch.conf", "/etc/gai.conf",
		"/etc/localtime", "/etc/ld.so.cache",
		"/etc/ssl/certs/ca-certificates.crt", "/etc/pki/tls/certs/ca-bundle.crt",
		"/etc/ssl/ca-bundle.pem", "/etc/pki/tls/cacert.pem",
		"/etc/pki/ca-trust/extracted/pem/tls-ca-bundle.pem", "/etc/ssl/cert.pem",
	} {
		if err := addFileRule(ruleset, path, readFile, true); err != nil {
			return err
		}
	}
	return addSystemLoaderRules(ruleset)
}

func addSystemLoaderRules(ruleset uintptr) error {
	var names []string
	var libraryArch string
	switch runtime.GOARCH {
	case "amd64":
		names = []string{"/lib64/ld-linux-x86-64.so.2", "/lib/ld-musl-x86_64.so.1"}
		libraryArch = "x86_64-linux-gnu"
	case "arm64":
		names = []string{"/lib/ld-linux-aarch64.so.1", "/lib/ld-musl-aarch64.so.1"}
		libraryArch = "aarch64-linux-gnu"
	default:
		// Other architectures can use a self-contained executable.
		return nil
	}
	for _, name := range names {
		if err := addFileRule(ruleset, name, readFile|execute, true); err != nil {
			return err
		}
	}
	// Permit the exact standard libraries commonly used by Go with cgo. The
	// driver receives no directory-wide permission to Core or system files.
	for _, dir := range []string{"/lib/" + libraryArch, "/usr/lib/" + libraryArch, "/lib64", "/usr/lib64"} {
		for _, name := range []string{"libc.so.6", "libpthread.so.0", "libgcc_s.so.1", "libdl.so.2", "libm.so.6", "librt.so.1"} {
			if err := addFileRule(ruleset, filepath.Join(dir, name), readFile, true); err != nil {
				return err
			}
		}
	}
	return nil
}
