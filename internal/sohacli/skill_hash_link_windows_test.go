//go:build windows

package sohacli

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func createUnsafeSkillHashLink(t *testing.T, root, outside string) {
	t.Helper()
	link := filepath.Join(root, "outside")
	// A directory junction is a real Windows reparse point and needs no
	// symlink privilege or machine-wide Developer Mode setting.
	if err := exec.Command("cmd.exe", "/d", "/c", "mklink", "/J", link, filepath.Dir(outside)).Run(); err != nil {
		t.Fatalf("create owned junction: %v", err)
	}
	t.Cleanup(func() {
		if err := os.Remove(link); err != nil {
			t.Errorf("remove owned junction: %v", err)
		}
	})
}
