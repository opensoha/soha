//go:build linux

package personalguest

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	guest "github.com/opensoha/soha-contracts/agent/guest"
)

func TestRuntimeFileContainmentAndContentCAS(t *testing.T) {
	dir := runtimeTestDirectory(t)
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = root.Close() }()
	op := guest.RuntimeOperation{Kind: "file_write", File: &guest.RuntimeFileInput{Path: "notes/proof.txt", ContentBase64: base64.StdEncoding.EncodeToString([]byte("old"))}}
	if _, err := runtimeFile(context.Background(), root, op); err != nil {
		t.Fatal(err)
	}
	if _, err := runtimeFile(context.Background(), root, op); err == nil {
		t.Fatal("existing file overwritten without CAS")
	}
	op.File.ExpectedSHA256 = fmt.Sprintf("%x", sha256.Sum256([]byte("other")))
	if _, err := runtimeFile(context.Background(), root, op); err == nil {
		t.Fatal("stale file revision accepted")
	}
	op.File.ExpectedSHA256 = fmt.Sprintf("%x", sha256.Sum256([]byte("old")))
	op.File.ContentBase64 = base64.StdEncoding.EncodeToString([]byte("new"))
	if _, err := runtimeFile(context.Background(), root, op); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(runtimeTestDirectory(t), "private")
	if err := os.WriteFile(outside, []byte("SECRET"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(dir, "escape")); err != nil {
		t.Fatal(err)
	}
	if _, err := readRuntimeFile(root, "escape"); err == nil {
		t.Fatal("outside symlink exposed content")
	}
	if err := os.Link(filepath.Join(dir, "notes/proof.txt"), filepath.Join(dir, "alias")); err != nil {
		t.Fatal(err)
	}
	if _, err := readRuntimeFile(root, "alias"); err == nil {
		t.Fatal("hardlink alias accepted")
	}
}
