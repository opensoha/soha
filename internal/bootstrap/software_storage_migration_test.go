package bootstrap

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLegacySoftwareStorageBlocksSilentUpgrade(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "index.json"), []byte("[]"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := checkLegacySoftwareStorage([]string{root}); err == nil {
		t.Fatal("legacy index should block startup")
	}
	if err := checkLegacySoftwareStorage([]string{filepath.Join(root, "missing")}); err != nil {
		t.Fatalf("missing legacy storage should be accepted: %v", err)
	}
}
