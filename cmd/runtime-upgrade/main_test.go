package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestBackupGuard(t *testing.T) {
	p := filepath.Join(t.TempDir(), "backup.dump")
	if _, err := checkBackup(p); err == nil {
		t.Fatal("missing backup accepted")
	}
	if err := os.WriteFile(p, []byte("backup"), 0600); err != nil {
		t.Fatal(err)
	}
	a, err := checkBackup(p)
	if err != nil || len(a) != 64 {
		t.Fatalf("%s %v", a, err)
	}
	if err := os.Chmod(p, 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := checkBackup(p); err == nil {
		t.Fatal("public backup accepted")
	}
}
