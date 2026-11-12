package radiusruntime

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type pathInfo struct {
	mode os.FileMode
	uid  uint32
}

func (i pathInfo) Name() string       { return "fixture" }
func (i pathInfo) Size() int64        { return 10 }
func (i pathInfo) Mode() os.FileMode  { return i.mode }
func (i pathInfo) ModTime() time.Time { return time.Time{} }
func (i pathInfo) IsDir() bool        { return i.mode.IsDir() }
func (i pathInfo) Sys() any           { return nil }

func mockPaths(entries map[string]pathInfo) pathPolicy {
	return pathPolicy{lstat: func(path string) (os.FileInfo, error) {
		info, ok := entries[path]
		if !ok {
			return nil, os.ErrNotExist
		}
		return info, nil
	}, owner: func(info os.FileInfo) (uint32, uint32, bool) {
		i, ok := info.(pathInfo)
		return i.uid, 0, ok
	}}
}

func TestProtectedPathsRejectReplaceableAncestors(t *testing.T) {
	for _, kind := range []string{"protected", "user-parent", "group-write", "world-write", "symlink-parent", "symlink-leaf", "missing-parent", "user-leaf"} {
		t.Run(kind, func(t *testing.T) {
			entries := map[string]pathInfo{"/": {os.ModeDir | 0755, 0}, "/etc": {os.ModeDir | 0755, 0}, "/etc/radius": {os.ModeDir | 0710, 0}, "/etc/radius/runtime.json": {0600, 0}}
			switch kind {
			case "user-parent":
				entries["/etc/radius"] = pathInfo{os.ModeDir | 0700, 1000}
			case "group-write":
				entries["/etc/radius"] = pathInfo{os.ModeDir | 0770, 0}
			case "world-write":
				entries["/etc"] = pathInfo{os.ModeDir | os.ModeSticky | 0777, 0}
			case "symlink-parent":
				entries["/etc/radius"] = pathInfo{os.ModeSymlink | 0755, 0}
			case "symlink-leaf":
				entries["/etc/radius/runtime.json"] = pathInfo{os.ModeSymlink | 0600, 0}
			case "missing-parent":
				delete(entries, "/etc/radius")
			case "user-leaf":
				entries["/etc/radius/runtime.json"] = pathInfo{0600, 1000}
			}
			err := approvedRootFile("/etc/radius/runtime.json", mockPaths(entries))
			if (err == nil) != (kind == "protected") {
				t.Fatalf("unexpected admission: %v", err)
			}
		})
	}
}

func TestProtectedPathsPermitOnlyMissingLeafAndSocketParent(t *testing.T) {
	paths := mockPaths(map[string]pathInfo{"/": {os.ModeDir | 0755, 0}, "/etc": {os.ModeDir | 0755, 0}, "/etc/radius": {os.ModeDir | 0710, 0}, "/etc/radius/hook.sock": {os.ModeSocket | 0660, 0}})
	if paths.check("/etc/radius/new-state") != nil {
		t.Fatal("protected missing leaf rejected")
	}
	if paths.check("/etc/radius/missing/new-state") == nil {
		t.Fatal("missing ancestor accepted")
	}
	if paths.check("/etc/radius/hook.sock") != nil {
		t.Fatal("RPC socket leaf group permission rejected")
	}
	if _, err := paths.regular("/etc/radius/hook.sock"); err == nil {
		t.Fatal("socket accepted as material")
	}
}

type fixtureModeInfo struct {
	os.FileInfo
	mode os.FileMode
}

func (i fixtureModeInfo) Mode() os.FileMode { return i.mode }

// Portable subprocess fixtures explicitly model protected installer ancestors;
// they never chown or weaken production checks for the host's /private/tmp.
func fixturePaths(base string) pathPolicy {
	return pathPolicy{lstat: func(path string) (os.FileInfo, error) {
		info, err := os.Lstat(path)
		if err == nil && info.IsDir() && path != base && !strings.HasPrefix(path, base+string(os.PathSeparator)) {
			return fixtureModeInfo{info, info.Mode() &^ 0022}, nil
		}
		return info, err
	}, owner: func(os.FileInfo) (uint32, uint32, bool) { return 0, 0, true }}
}

func TestSupervisorStateDirectoryCreationRechecksProtection(t *testing.T) {
	s, _, _ := supervisorFixture(t)
	if _, err := os.Lstat(s.config.StateDir); !os.IsNotExist(err) {
		t.Fatal("fixture state already exists", err)
	}
	if err := s.establishDirectory(); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(filepath.Dir(s.config.StateDir), 0770); err != nil {
		t.Fatal(err)
	}
	if err := s.establishDirectory(); err == nil {
		t.Fatal("changed parent permission accepted")
	}
}

func TestControlMaterialHashesAndPathsAreVerifiedBeforeTLSRead(t *testing.T) {
	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(base, "control.pem")
	if err := os.WriteFile(file, []byte("nonsecret material fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	hash, err := hashFile(file)
	if err != nil {
		t.Fatal(err)
	}
	c := ClientConfiguration{CertificateFile: file, KeyFile: file, CAFile: file, ApprovedMaterials: map[string]string{file: hash}}
	paths := fixturePaths(base)
	if err := verifyControlFiles(c, paths); err != nil {
		t.Fatal(err)
	}
	c.ApprovedMaterials[file] = strings.Repeat("f", 64)
	if verifyControlFiles(c, paths) == nil {
		t.Fatal("unapproved bytes reached TLS loading")
	}
	c.ApprovedMaterials[file] = hash
	if err := os.Chmod(base, 0770); err != nil {
		t.Fatal(err)
	}
	if verifyControlFiles(c, paths) == nil {
		t.Fatal("replaceable TLS parent accepted")
	}
}
