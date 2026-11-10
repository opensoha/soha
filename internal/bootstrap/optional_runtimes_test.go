package bootstrap

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRuntimeSecretFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "service-token")
	for _, tc := range []struct {
		name, value string
		valid       bool
	}{
		{"newline", strings.Repeat("a", 32) + "\n", true},
		{"short", "not-a-service-secret", false},
		{"embedded whitespace", strings.Repeat("a", 32) + " b", false},
		{"control", strings.Repeat("a", 32) + "\x00", false},
		{"oversized", strings.Repeat("a", 8194), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := os.WriteFile(path, []byte(tc.value), 0600); err != nil {
				t.Fatal(err)
			}
			got, err := readRuntimeSecret(path)
			if (err == nil) != tc.valid {
				t.Fatalf("accepted=%t want %t", err == nil, tc.valid)
			}
			if tc.valid && got != strings.TrimSpace(tc.value) {
				t.Fatal("secret bytes changed")
			}
		})
	}
	if _, err := readRuntimeSecret(t.TempDir()); err == nil {
		t.Fatal("accepted directory")
	}
	if _, err := readRuntimeSecret(path + "-missing"); err == nil {
		t.Fatal("accepted missing file")
	}
}
