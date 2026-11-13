//go:build linux

package personalguest

import (
	"os"
	"path/filepath"
	"testing"
)

func runtimeTestDirectory(t *testing.T) string {
	t.Helper()
	base := os.Getenv("SOHA_RUNTIME_TEST_RETAIN_DIRECTORY")
	if base == "" {
		return t.TempDir()
	}
	if !filepath.IsAbs(base) || filepath.Clean(base) != base {
		t.Fatal("retained runtime test directory must be canonical")
	}
	directory, err := os.MkdirTemp(base, "runtime-proof-")
	if err != nil {
		t.Fatal(err)
	}
	t.Log("retained runtime test files:", directory)
	return directory
}
