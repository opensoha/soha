//go:build windows

package sohacli

import "testing"

func assertPrivateTestConfig(t *testing.T, path string) {
	t.Helper()
	if err := requirePrivateWindowsACL(path, false); err != nil {
		t.Fatalf("stored config DACL is not private: %v", err)
	}
}
