package companionartifact

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"testing"

	domaincompanion "github.com/opensoha/soha/internal/domain/companion"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

func TestStoreInstallAndOpenVerifiedAsset(t *testing.T) {
	asset := []byte(`<svg xmlns="http://www.w3.org/2000/svg"><circle cx="8" cy="8" r="6"/></svg>`)
	archive := zipArchive(t, "orbit.svg", asset)
	store, err := NewStore(t.TempDir(), 1<<20)
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}
	pack := testPack("orbit.svg", asset)
	digest, err := store.Install(context.Background(), testDescriptor(archive), pack, bytes.NewReader(archive))
	if err != nil {
		t.Fatalf("Install() error = %v", err)
	}

	reader, err := store.Open(digest, "orbit.svg")
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer func() { _ = reader.Close() }()
	got, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("ReadAll() error = %v", err)
	}
	if !bytes.Equal(got, asset) {
		t.Fatalf("asset = %q, want %q", got, asset)
	}
}

func TestStoreRejectsArchiveTraversal(t *testing.T) {
	asset := []byte("bad")
	archive := zipArchive(t, "../escape.svg", asset)
	store, err := NewStore(t.TempDir(), 1<<20)
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}
	pack := testPack("orbit.svg", asset)

	_, err = store.Install(context.Background(), testDescriptor(archive), pack, bytes.NewReader(archive))
	if !errors.Is(err, apperrors.ErrInvalidArgument) {
		t.Fatalf("Install() error = %v, want invalid argument", err)
	}
}

func TestStoreRejectsActiveSVG(t *testing.T) {
	asset := []byte(`<svg xmlns="http://www.w3.org/2000/svg"><script>alert(1)</script></svg>`)
	archive := zipArchive(t, "orbit.svg", asset)
	store, err := NewStore(t.TempDir(), 1<<20)
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}

	_, err = store.Install(context.Background(), testDescriptor(archive), testPack("orbit.svg", asset), bytes.NewReader(archive))
	if !errors.Is(err, apperrors.ErrInvalidArgument) {
		t.Fatalf("Install() error = %v, want invalid argument", err)
	}
}

func zipArchive(t *testing.T, name string, body []byte) []byte {
	t.Helper()
	var buffer bytes.Buffer
	writer := zip.NewWriter(&buffer)
	entry, err := writer.Create(name)
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if _, err := entry.Write(body); err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	return buffer.Bytes()
}

func testPack(path string, body []byte) domaincompanion.PackManifest {
	return domaincompanion.PackManifest{
		Renderer: "svg", EntryAsset: path,
		Assets: []domaincompanion.Asset{{
			Path: path, Kind: "model", ContentType: "image/svg+xml", SizeBytes: int64(len(body)), Sha256: sha256String(body),
		}},
	}
}

func testDescriptor(body []byte) domaincompanion.PackageDescriptor {
	return domaincompanion.PackageDescriptor{ContentType: "application/zip", SizeBytes: int64(len(body)), Sha256: sha256String(body)}
}

func sha256String(body []byte) string {
	digest := sha256.Sum256(body)
	return "sha256:" + hex.EncodeToString(digest[:])
}
