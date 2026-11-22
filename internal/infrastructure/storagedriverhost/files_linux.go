package storagedriverhost

import (
	"context"
	"crypto/sha256"
	"debug/elf"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	"golang.org/x/sys/unix"
)

const runtimeDirMode = 0o711

func openTrustedDir(path string, requireTraversal bool) (*os.File, error) {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path || path == "/" {
		return nil, fmt.Errorf("storage driver directory requires a canonical absolute path: %q", path)
	}
	fd, err := unix.Open("/", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}
	current := os.NewFile(uintptr(fd), "/")
	for _, segment := range strings.Split(path[1:], "/") {
		next, err := trustedDirectoryAt(current, segment, requireTraversal)
		_ = current.Close()
		if err != nil {
			return nil, err
		}
		current = next
	}
	return current, nil
}

func trustedDirectoryAt(parent *os.File, name string, requireTraversal bool) (*os.File, error) {
	dir, st, err := openTrustedAt(parent, name, unix.O_DIRECTORY, 0)
	if err != nil {
		return nil, err
	}
	if st.Mode&unix.S_IFMT != unix.S_IFDIR || (requireTraversal && st.Mode&0o001 == 0) {
		_ = dir.Close()
		return nil, fmt.Errorf("storage driver directory is inaccessible or unsafe: %s", dir.Name())
	}
	return dir, nil
}

func openTrustedAt(parent *os.File, name string, flags int, mode uint32) (*os.File, unix.Stat_t, error) {
	var stat unix.Stat_t
	if !filepath.IsLocal(name) || name == "." || filepath.Base(name) != name {
		return nil, stat, errors.New("invalid storage driver file name")
	}
	path := filepath.Join(parent.Name(), name)
	fd, err := unix.Openat(int(parent.Fd()), name, flags|unix.O_NOFOLLOW|unix.O_CLOEXEC|unix.O_NONBLOCK, mode)
	if err != nil {
		return nil, stat, fmt.Errorf("open protected path %s: %w", path, err)
	}
	file := os.NewFile(uintptr(fd), path)
	if err := unix.Fstat(fd, &stat); err != nil {
		_ = file.Close()
		return nil, stat, err
	}
	// Also reject set-ID/sticky bits. An extended POSIX ACL cannot grant
	// additional write permissions without setting the group-class write bit.
	if stat.Uid != 0 || stat.Gid != 0 || stat.Mode&0o7022 != 0 {
		_ = file.Close()
		return nil, stat, fmt.Errorf("unsafe ownership or permissions at %s", path)
	}
	return file, stat, nil
}

func requireMode(dir *os.File, mode os.FileMode) error {
	info, err := dir.Stat()
	if err != nil {
		return err
	}
	if info.Mode().Perm() != mode {
		return fmt.Errorf("storage driver directory %s requires mode %04o", dir.Name(), mode)
	}
	return nil
}

func openPackageDir(root string, selection DriverConfig) (*os.File, error) {
	packageRoot, err := openTrustedDir(root, false)
	if err != nil {
		return nil, err
	}
	defer packageRoot.Close()
	if err := requireMode(packageRoot, 0o700); err != nil {
		return nil, err
	}
	plugin, err := trustedDirectoryAt(packageRoot, selection.PluginID, false)
	if err != nil {
		return nil, err
	}
	defer plugin.Close()
	if err := requireMode(plugin, 0o700); err != nil {
		return nil, err
	}
	version, err := trustedDirectoryAt(plugin, selection.Version, false)
	if err != nil {
		return nil, err
	}
	if err := requireMode(version, 0o700); err != nil {
		_ = version.Close()
		return nil, err
	}
	return version, nil
}

func openRegularAt(dir *os.File, name string) (*os.File, unix.Stat_t, error) {
	file, stat, err := openTrustedAt(dir, name, unix.O_RDONLY, 0)
	if err != nil {
		return nil, stat, err
	}
	if stat.Mode&unix.S_IFMT != unix.S_IFREG || stat.Nlink != 1 {
		_ = file.Close()
		return nil, stat, fmt.Errorf("storage driver file must be a non-linked regular file: %s", file.Name())
	}
	return file, stat, nil
}

func readRegularAt(dir *os.File, name string, limit int64) ([]byte, error) {
	file, st, err := openRegularAt(dir, name)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	if st.Size <= 0 || st.Size > limit {
		return nil, fmt.Errorf("invalid size at %s", file.Name())
	}
	content, err := io.ReadAll(io.LimitReader(file, limit+1))
	if err != nil || int64(len(content)) != st.Size {
		return nil, fmt.Errorf("storage driver file could not be read safely: %s", file.Name())
	}
	return content, nil
}

func readPublisherKey(path string) ([]byte, error) {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return nil, errors.New("storage driver publisher key requires a canonical absolute path")
	}
	dir, err := openTrustedDir(filepath.Dir(path), false)
	if err != nil {
		return nil, err
	}
	defer dir.Close()
	return readRegularAt(dir, filepath.Base(path), 4096)
}

func loadPackage(dir *os.File, selection DriverConfig) (verifiedRelease, error) {
	key, err := readPublisherKey(selection.PublicKeyPath)
	if err != nil {
		return verifiedRelease{}, err
	}
	attestation, err := readRegularAt(dir, attestationName, maxDocument)
	if err != nil {
		return verifiedRelease{}, err
	}
	signature, err := readRegularAt(dir, signatureName, 64)
	if err != nil {
		return verifiedRelease{}, err
	}
	manifest, err := readRegularAt(dir, manifestName, maxDocument)
	if err != nil {
		return verifiedRelease{}, err
	}
	return verifyRelease(selection, key, attestation, signature, manifest, runtime.GOOS+"/"+runtime.GOARCH)
}

func takeRuntimeLock(root string) (*os.File, error) {
	dir, err := openTrustedDir(root, true)
	if err != nil {
		return nil, err
	}
	defer dir.Close()
	if err := requireMode(dir, runtimeDirMode); err != nil {
		return nil, err
	}
	lock, st, err := openTrustedAt(dir, ".soha-storage-driver.lock", unix.O_RDWR|unix.O_CREAT, 0o600)
	if err != nil {
		return nil, err
	}
	if st.Mode&unix.S_IFMT != unix.S_IFREG || st.Nlink != 1 || st.Mode&0o077 != 0 {
		_ = lock.Close()
		return nil, errors.New("unsafe storage driver runtime lock")
	}
	if err := unix.Flock(int(lock.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		_ = lock.Close()
		return nil, fmt.Errorf("storage driver runtime root is already held or cannot be locked: %w", err)
	}
	return lock, nil
}

func sealedBinary(ctx context.Context, dir *os.File, asset releaseArtifact) (*os.File, error) {
	source, st, err := openRegularAt(dir, asset.Name)
	if err != nil {
		return nil, err
	}
	defer source.Close()
	if st.Size != asset.ByteLength {
		return nil, errors.New("storage driver artifact size does not match the signed inventory")
	}
	fd, err := newExecutableMemfd()
	if err != nil {
		return nil, fmt.Errorf("create isolated storage driver executable: %w", err)
	}
	target := os.NewFile(uintptr(fd), "soha-storage-driver")
	defer target.Close()
	if err := copyVerifiedBinary(ctx, target, source, asset); err != nil {
		return nil, err
	}
	// The inherited descriptor is read-only even before the seals are considered.
	readOnly, err := os.Open("/proc/self/fd/" + strconv.Itoa(fd))
	if err != nil {
		return nil, fmt.Errorf("open verified storage driver executable: %w", err)
	}
	return readOnly, nil
}

func newExecutableMemfd() (int, error) {
	const flags = unix.MFD_CLOEXEC | unix.MFD_ALLOW_SEALING
	fd, err := unix.MemfdCreate("soha-storage-driver", flags|unix.MFD_EXEC)
	if errors.Is(err, unix.EINVAL) {
		// Kernels before MFD_EXEC supported executable memfd by default.
		return unix.MemfdCreate("soha-storage-driver", flags)
	}
	return fd, err
}

func copyVerifiedBinary(ctx context.Context, target, source *os.File, asset releaseArtifact) error {
	digest := sha256.New()
	input := digestReader{context: ctx, source: source}
	size, err := io.Copy(io.MultiWriter(target, digest), io.LimitReader(input, asset.ByteLength+1))
	if err != nil {
		return fmt.Errorf("copy verified storage driver executable: %w", err)
	}
	if size != asset.ByteLength || hex.EncodeToString(digest.Sum(nil)) != asset.SHA256 {
		return errors.New("storage driver artifact digest does not match the signed inventory")
	}
	if err := checkELF(target); err != nil {
		return err
	}
	if err := target.Chmod(0o555); err != nil {
		return err
	}
	_, err = unix.FcntlInt(target.Fd(), unix.F_ADD_SEALS, unix.F_SEAL_SEAL|unix.F_SEAL_WRITE|unix.F_SEAL_GROW|unix.F_SEAL_SHRINK)
	if err != nil {
		return fmt.Errorf("lock verified storage driver executable: %w", err)
	}
	return nil
}

func checkELF(executable *os.File) error {
	// Inspect only the fixed-size ELF prefix. The kernel handles the remaining
	// executable parsing after the privileges have been dropped.
	var header [18]byte
	if _, err := executable.ReadAt(header[:], 0); err != nil || string(header[:4]) != elf.ELFMAG {
		return errors.New("storage driver executable must be an ELF program")
	}
	var order binary.ByteOrder
	switch elf.Data(header[elf.EI_DATA]) {
	case elf.ELFDATA2LSB:
		order = binary.LittleEndian
	case elf.ELFDATA2MSB:
		order = binary.BigEndian
	default:
		return errors.New("invalid storage driver ELF byte order")
	}
	kind := elf.Type(order.Uint16(header[16:]))
	if kind != elf.ET_EXEC && kind != elf.ET_DYN {
		return errors.New("storage driver artifact is not an executable ELF program")
	}
	return nil
}

type digestReader struct {
	context context.Context
	source  io.Reader
}

func (r digestReader) Read(p []byte) (int, error) {
	if err := r.context.Err(); err != nil {
		return 0, err
	}
	return r.source.Read(p)
}
