//go:build linux

package dockerruntime

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	protocol "github.com/opensoha/soha-contracts/docker/runtime"
	"golang.org/x/sys/unix"
)

func volumeReadFixture(action, name string) protocol.ReadRequest {
	p, _ := protocol.NewProjectBinding("11111111-1111-4111-8111-111111111111", "22222222-2222-4222-8222-222222222222")
	r := protocol.ReadRequest{Protocol: protocol.ReadProtocol, Action: action, ProjectID: "project", Target: "/data", Path: name,
		Container: protocol.ContainerBinding{ProjectBinding: p, ServiceName: "web", ContainerID: strings.Repeat("a", 64)}}
	r, _ = r.Normalized(action)
	return r
}

func TestVolumeDirectoryReportsEncodedByteTruncation(t *testing.T) {
	dir := t.TempDir()
	components := make([]string, 32)
	for i := range components {
		components[i] = strings.Repeat("<", 100)
	}
	relative := strings.Join(components, "/")
	deep := filepath.Join(dir, filepath.FromSlash(relative))
	if err := os.MkdirAll(deep, 0700); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 100; i++ {
		if err := os.WriteFile(filepath.Join(deep, fmt.Sprintf("%03d", i)+strings.Repeat("<", 190)), []byte("x"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	fd, err := unix.Open(dir, unix.O_PATH|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(fd)
	id, err := fileMountID(fd)
	if err != nil {
		t.Fatal(err)
	}
	req := volumeReadFixture("files.list", relative)
	req.Limit = 1000
	result, err := readMountedPath(context.Background(), fd, id, req)
	if err != nil || len(result.Files) == 0 || len(result.Files) >= 100 || !result.Truncated || result.Complete || result.ValidateFor(req) != nil {
		t.Fatal("encoded directory budget was not explicit", len(result.Files), err)
	}
}

func TestLinuxVolumeReaderAnchorsPathsAndRejectsSpecialFiles(t *testing.T) {
	dir := t.TempDir()
	for name, content := range map[string]string{"text": "中文abc", "binary": "private\x00", "invalid": string([]byte{0xff})} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink("/etc/passwd", filepath.Join(dir, "escape")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("text", filepath.Join(dir, "link")); err != nil {
		t.Fatal(err)
	}
	if err := os.Link(filepath.Join(dir, "text"), filepath.Join(dir, "hard")); err != nil {
		t.Fatal(err)
	}
	if err := unix.Mkfifo(filepath.Join(dir, "fifo"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "single"), []byte("中文abc"), 0600); err != nil {
		t.Fatal(err)
	}
	fd, err := unix.Open(dir, unix.O_PATH|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(fd)
	id, err := fileMountID(fd)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"escape", "link", "hard", "text", "fifo", "binary", "invalid", "../outside", "/etc/passwd"} {
		t.Run(name, func(t *testing.T) {
			req := volumeReadFixture("files.read", name)
			// Deliberately preserve invalid input instead of ignoring Normalized's error.
			req.Path = name
			if _, err := readMountedPath(context.Background(), fd, id, req); err == nil {
				t.Fatal("unsafe file was read")
			}
		})
	}
	req := volumeReadFixture("files.read", "single")
	req.LimitBytes = 4
	result, err := readMountedPath(context.Background(), fd, id, req)
	if err != nil || result.Content != "中" || result.SizeBytes != 9 || result.ReturnedBytes != 3 || !result.Truncated || result.Complete || result.ValidateFor(req) != nil {
		t.Fatal(result, err)
	}
	req = volumeReadFixture("files.list", "")
	req.Limit = 2
	result, err = readMountedPath(context.Background(), fd, id, req)
	if err != nil || len(result.Files) != 2 || !result.Truncated || result.ValidateFor(req) != nil {
		t.Fatal(result, err)
	}
	req.Limit = 100
	result, err = readMountedPath(context.Background(), fd, id, req)
	if err != nil || !result.Complete || result.ValidateFor(req) != nil {
		t.Fatal(result, err)
	}
	for _, item := range result.Files {
		if (item.Name == "escape" || item.Name == "link" || item.Name == "fifo" || item.Name == "hard" || item.Name == "text") && (item.ReadSupported || item.ListSupported) {
			t.Fatal("special or linked file advertised as supported", item)
		}
	}
}

func TestLinuxVolumeTargetRequiresOriginalMountIDAndNoSymlink(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "data"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("data", filepath.Join(root, "alias")); err != nil {
		t.Fatal(err)
	}
	fd, err := unix.Open(root, unix.O_PATH|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(fd)
	id, err := fileMountID(fd)
	if err != nil {
		t.Fatal(err)
	}
	opened, err := openVolumeTarget(fd, "/data", id)
	if err != nil {
		t.Fatal(err)
	}
	unix.Close(opened)
	for _, target := range []string{"/alias", "/data/..", "/"} {
		if opened, err := openVolumeTarget(fd, target, id); err == nil {
			unix.Close(opened)
			t.Fatal("unsafe mount target accepted", target)
		}
	}
	if opened, err := openVolumeTarget(fd, "/data", id+1); err == nil {
		unix.Close(opened)
		t.Fatal("changed mount ID accepted")
	}
	file, err := unix.Open(filepath.Join(root, "one-file"), unix.O_CREAT|unix.O_RDWR|unix.O_CLOEXEC, 0600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := unix.Write(file, []byte("file bind")); err != nil {
		t.Fatal(err)
	}
	unix.Close(file)
	file, err = unix.Open(filepath.Join(root, "one-file"), unix.O_PATH|unix.O_CLOEXEC, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(file)
	req := volumeReadFixture("files.read", "")
	result, err := readMountedPath(context.Background(), file, id, req)
	if err != nil || result.Content != "file bind" {
		t.Fatal(result, err)
	}
}

func TestMountInfoSelectsExactEscapedTarget(t *testing.T) {
	raw := "12 1 8:1 / /data\\040space rw - ext4 source rw\n13 1 8:1 / /data2 rw - ext4 source rw\n"
	if id, err := targetMountID(raw, "/data space"); err != nil || id != 12 {
		t.Fatal(id, err)
	}
	for _, candidate := range []string{raw + raw, "broken", strings.Replace(raw, "\\040", "\\999", 1)} {
		if _, err := targetMountID(candidate, "/data space"); err == nil {
			t.Fatal("ambiguous mount inventory accepted")
		}
	}
	if _, err := targetMountID(raw, "/data"); err == nil {
		t.Fatal("prefix mount target accepted")
	}
}
