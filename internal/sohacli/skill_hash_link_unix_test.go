//go:build !windows

package sohacli

import (
	"os"
	"path/filepath"
	"testing"
)

func createUnsafeSkillHashLink(t *testing.T, root, outside string) {
	t.Helper()
	if err := os.Symlink(outside, filepath.Join(root, "outside.md")); err != nil {
		t.Fatal(err)
	}
}
