//go:build !windows

package sohacli

import (
	"os"
	"testing"
)

func assertPrivateTestConfig(t *testing.T, path string) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("config permissions = %o, want 0600", info.Mode().Perm())
	}
}
