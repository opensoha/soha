package sohacli

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
)

func TestSkillDirectoryHashPreservesFormatAndRejectsExternalSymlinks(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "nested"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "nested", "skill.md"), []byte("content"), 0o600); err != nil {
		t.Fatal(err)
	}
	want := sha256.Sum256([]byte("nested/skill.md\x00content"))
	got, err := hashDirectory(root)
	if err != nil || got != hex.EncodeToString(want[:]) {
		t.Fatalf("installed skill fingerprint changed: %s %v", got, err)
	}
	outside := filepath.Join(t.TempDir(), "outside.md")
	if err := os.WriteFile(outside, []byte("private"), 0o600); err != nil {
		t.Fatal(err)
	}
	createUnsafeSkillHashLink(t, root, outside)
	if _, err := hashDirectory(root); err == nil {
		t.Fatal("external symlink accepted")
	}
}
