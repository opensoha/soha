package securefile

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReadConfinesSymlinksAndChecksSecretMode(t *testing.T) {
	directory := t.TempDir()
	secret := filepath.Join(directory, "secret")
	// #nosec G306 -- exercises supported group-readable secret mounts.
	if err := os.WriteFile(secret, []byte("value"), 0o640); err != nil {
		t.Fatal(err)
	}
	if raw, err := Read(secret, 16, true); err != nil || string(raw) != "value" {
		t.Fatalf("Read() = %q, %v", raw, err)
	}
	inside := filepath.Join(directory, "inside-link")
	if err := os.Symlink("secret", inside); err != nil {
		t.Fatal(err)
	}
	if _, err := Read(inside, 16, true); err != nil {
		t.Fatalf("confined symlink rejected: %v", err)
	}
	outside := filepath.Join(t.TempDir(), "outside")
	if err := os.WriteFile(outside, []byte("outside"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(directory, "outside-link")); err != nil {
		t.Fatal(err)
	}
	if _, err := Read(filepath.Join(directory, "outside-link"), 16, true); err == nil {
		t.Fatal("symlink escaping the configured directory must fail")
	}
	// #nosec G302 -- verifies rejection of world-readable test data.
	if err := os.Chmod(secret, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Read(secret, 16, true); err == nil {
		t.Fatal("world-readable secret must fail")
	}
}
