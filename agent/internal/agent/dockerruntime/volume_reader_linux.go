//go:build linux

package dockerruntime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"strconv"
	"strings"
	"time"

	protocol "github.com/opensoha/soha/contracts/docker/runtime"
	"golang.org/x/sys/unix"
)

const containedPath = unix.RESOLVE_BENEATH | unix.RESOLVE_NO_SYMLINKS | unix.RESOLVE_NO_MAGICLINKS

func readContainerVolume(ctx context.Context, req protocol.ReadRequest) (protocol.ReadResult, error) {
	// PID 1 is the original container init in the shared private PID namespace.
	// Following this one procfs link is deliberate; user paths use openat2 below.
	root, err := unix.Open("/proc/1/root", unix.O_PATH|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	if err != nil {
		return protocol.ReadResult{}, err
	}
	defer func() { _ = unix.Close(root) }()
	info, err := os.Open("/proc/1/mountinfo")
	if err != nil {
		return protocol.ReadResult{}, err
	}
	raw, err := io.ReadAll(io.LimitReader(info, (1<<20)+1))
	closeErr := info.Close()
	if err != nil || closeErr != nil || len(raw) > 1<<20 {
		return protocol.ReadResult{}, errors.New("container mount inventory is unavailable")
	}
	id, err := targetMountID(string(raw), req.Target)
	if err != nil {
		return protocol.ReadResult{}, err
	}
	mount, err := openVolumeTarget(root, req.Target, id)
	if err != nil {
		return protocol.ReadResult{}, err
	}
	defer func() { _ = unix.Close(mount) }()
	return readMountedPath(ctx, mount, id, req)
}

func targetMountID(raw, target string) (uint64, error) {
	var found uint64
	lines := strings.Split(strings.TrimSpace(raw), "\n")
	if len(lines) > 4096 {
		return 0, errors.New("container mount inventory exceeds its limit")
	}
	for _, line := range lines {
		fields := strings.Fields(line)
		if len(fields) < 10 {
			return 0, errors.New("invalid container mount inventory")
		}
		point, err := decodeMountPoint(fields[4])
		if err != nil {
			return 0, err
		}
		if point != target {
			continue
		}
		id, err := strconv.ParseUint(fields[0], 10, 64)
		if err != nil || id == 0 || found != 0 {
			return 0, errors.New("ambiguous container mount target")
		}
		found = id
	}
	if found == 0 {
		return 0, errors.New("requested target is not a container mount")
	}
	return found, nil
}

func decodeMountPoint(value string) (string, error) {
	var out strings.Builder
	for index := 0; index < len(value); index++ {
		if value[index] != '\\' {
			out.WriteByte(value[index])
			continue
		}
		if index+3 >= len(value) {
			return "", errors.New("invalid mount path escape")
		}
		var decoded byte
		switch value[index+1 : index+4] {
		case "040":
			decoded = ' '
		case "011":
			decoded = '\t'
		case "012":
			decoded = '\n'
		case "134":
			decoded = '\\'
		default:
			return "", errors.New("invalid mount path escape")
		}
		out.WriteByte(decoded)
		index += 3
	}
	return out.String(), nil
}

func fileMountID(fd int) (uint64, error) {
	var info unix.Statx_t
	if err := unix.Statx(fd, "", unix.AT_EMPTY_PATH|unix.AT_SYMLINK_NOFOLLOW, unix.STATX_MNT_ID, &info); err != nil || info.Mask&unix.STATX_MNT_ID == 0 || info.Mnt_id == 0 {
		return 0, errors.New("kernel mount identity is unavailable")
	}
	return info.Mnt_id, nil
}

func openVolumeTarget(root int, target string, expected uint64) (int, error) {
	if !protocol.ValidMountTarget(target) || expected == 0 {
		return -1, errors.New("invalid mount target")
	}
	// Crossing into the declared mount is allowed only at this first stage.
	fd, err := unix.Openat2(root, strings.TrimPrefix(target, "/"), &unix.OpenHow{Flags: unix.O_PATH | unix.O_CLOEXEC, Resolve: containedPath})
	if err != nil {
		return -1, err
	}
	if id, err := fileMountID(fd); err != nil || id != expected {
		_ = unix.Close(fd)
		return -1, errors.New("container mount was replaced")
	}
	return fd, nil
}

func readMountedPath(ctx context.Context, mount int, mountID uint64, req protocol.ReadRequest) (protocol.ReadResult, error) {
	if err := ctx.Err(); err != nil {
		return protocol.ReadResult{}, err
	}
	fd, err := openMountedPath(mount, req.Path)
	if err != nil {
		return protocol.ReadResult{}, err
	}
	defer func() { _ = unix.Close(fd) }()
	if id, err := fileMountID(fd); err != nil || id != mountID {
		return protocol.ReadResult{}, errors.New("file is outside the original mount")
	}
	var stat unix.Stat_t
	if err := unix.Fstat(fd, &stat); err != nil {
		return protocol.ReadResult{}, err
	}
	if req.Action == "files.list" {
		return readVolumeDirectory(ctx, fd, mountID, stat, req)
	}
	return readVolumeText(fd, stat, req)
}

func openMountedPath(mount int, relative string) (int, error) {
	if !protocol.ValidRelativePath(relative) {
		return -1, errors.New("invalid volume path")
	}
	if relative == "" || relative == "." {
		return unix.FcntlInt(uintptr(mount), unix.F_DUPFD_CLOEXEC, 0)
	}
	return unix.Openat2(mount, relative, &unix.OpenHow{Flags: unix.O_PATH | unix.O_CLOEXEC, Resolve: containedPath | unix.RESOLVE_NO_XDEV})
}

func reopenHeldFile(fd int, directory bool) (*os.File, error) {
	flags := unix.O_RDONLY | unix.O_CLOEXEC | unix.O_NONBLOCK
	if directory {
		flags |= unix.O_DIRECTORY
	}
	// Reopen our own held inode; never resolve the user path a second time.
	opened, err := unix.Open(fmt.Sprintf("/proc/self/fd/%d", fd), flags, 0)
	if err != nil {
		return nil, err
	}
	return os.NewFile(uintptr(opened), "held-volume-file"), nil
}

func readVolumeText(fd int, before unix.Stat_t, req protocol.ReadRequest) (protocol.ReadResult, error) {
	if before.Mode&unix.S_IFMT != unix.S_IFREG || before.Nlink != 1 {
		return protocol.ReadResult{}, errors.New("only regular single-link text files are supported")
	}
	file, err := reopenHeldFile(fd, false)
	if err != nil {
		return protocol.ReadResult{}, err
	}
	defer func() { _ = file.Close() }()
	raw, err := io.ReadAll(io.LimitReader(file, req.LimitBytes+1))
	if err != nil {
		return protocol.ReadResult{}, err
	}
	var after unix.Stat_t
	if unix.Fstat(int(file.Fd()), &after) != nil || !sameVolumeFile(before, after) {
		return protocol.ReadResult{}, errors.New("file changed during the read")
	}
	content, truncated, err := boundedUTF8(string(raw), int(req.LimitBytes), false)
	if err != nil {
		return protocol.ReadResult{}, err
	}
	result := newReadResult(req)
	result.Content, result.Encoding, result.SizeBytes = content, "utf-8", before.Size
	result.ReturnedBytes = int64(len(content))
	result.Truncated, result.Complete = truncated, !truncated
	return result, nil
}

func sameVolumeFile(before, after unix.Stat_t) bool {
	return before.Dev == after.Dev && before.Ino == after.Ino && before.Size == after.Size && before.Nlink == after.Nlink && before.Mtim == after.Mtim && before.Ctim == after.Ctim
}

func readVolumeDirectory(ctx context.Context, fd int, mountID uint64, stat unix.Stat_t, req protocol.ReadRequest) (protocol.ReadResult, error) {
	if stat.Mode&unix.S_IFMT != unix.S_IFDIR {
		return protocol.ReadResult{}, errors.New("requested file is not a directory")
	}
	dir, err := reopenHeldFile(fd, true)
	if err != nil {
		return protocol.ReadResult{}, err
	}
	defer func() { _ = dir.Close() }()
	entries, err := dir.ReadDir(req.Limit + 1)
	if err != nil && !errors.Is(err, io.EOF) {
		return protocol.ReadResult{}, err
	}
	result := newReadResult(req)
	if len(entries) > req.Limit {
		entries = entries[:req.Limit]
		result.Truncated, result.Complete = true, false
	}
	encodedBytes := 0
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return protocol.ReadResult{}, err
		}
		item, err := describeVolumeEntry(fd, mountID, req.Path, entry.Name())
		if err != nil {
			return protocol.ReadResult{}, err
		}
		encoded, err := json.Marshal(item)
		if err != nil {
			return protocol.ReadResult{}, err
		}
		if len(encoded) > 2<<20-encodedBytes {
			result.Truncated, result.Complete = true, false
			break
		}
		encodedBytes += len(encoded)
		result.Files = append(result.Files, item)
	}
	return result, nil
}

func describeVolumeEntry(parent int, mountID uint64, relative, name string) (protocol.FileEntry, error) {
	item := protocol.FileEntry{Name: name, Path: path.Join(relative, name), Kind: "other"}
	if !protocol.ValidRelativePath(name) || name == "." || name == ".." || strings.Contains(name, "/") {
		return item, errors.New("directory entry name is not supported")
	}
	fd, err := unix.Openat2(parent, name, &unix.OpenHow{Flags: unix.O_PATH | unix.O_NOFOLLOW | unix.O_CLOEXEC, Resolve: containedPath | unix.RESOLVE_NO_XDEV})
	if errors.Is(err, unix.EXDEV) {
		return item, nil
	}
	if err != nil {
		return item, err
	}
	defer func() { _ = unix.Close(fd) }()
	if id, err := fileMountID(fd); err != nil || id != mountID {
		return item, errors.New("directory entry mount changed")
	}
	var stat unix.Stat_t
	if err := unix.Fstat(fd, &stat); err != nil {
		return item, err
	}
	item.SizeBytes = stat.Size
	item.ModifiedAt = time.Unix(stat.Mtim.Sec, stat.Mtim.Nsec).UTC().Format(time.RFC3339Nano)
	switch stat.Mode & unix.S_IFMT {
	case unix.S_IFREG:
		item.Kind, item.ReadSupported = "file", stat.Nlink == 1
	case unix.S_IFDIR:
		item.Kind, item.ListSupported = "directory", true
	case unix.S_IFLNK:
		item.Kind = "symlink"
	}
	return item, nil
}
