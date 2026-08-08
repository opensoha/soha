package softwarestore

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"testing"

	appsoftware "github.com/opensoha/soha/internal/application/software"
)

func TestStorePackageLifecycle(t *testing.T) {
	store, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	payload := []byte("signed installer payload")
	item, err := store.Create(context.Background(), appsoftware.UploadInput{
		SoftwareID: "demo", Name: "Demo", Publisher: "OpenSoha", Version: "1.0.0",
		Platform: "darwin", Arch: "arm64", FileName: "demo.pkg",
	}, bytes.NewReader(payload))
	if err != nil {
		t.Fatal(err)
	}
	wantDigest := sha256.Sum256(payload)
	if item.SizeBytes != int64(len(payload)) || item.SHA256 != hex.EncodeToString(wantDigest[:]) {
		t.Fatalf("unexpected integrity metadata: %+v", item)
	}

	items, _, err := store.List(context.Background(), appsoftware.Filter{})
	if err != nil || len(items) != 1 || items[0].ID != item.ID {
		t.Fatalf("unexpected list: items=%+v err=%v", items, err)
	}
	storage, err := store.Storage(context.Background(), "", 50)
	if err != nil {
		t.Fatal(err)
	}
	if storage.Backend != "filesystem" || storage.ObjectCount != 1 || storage.TotalBytes != int64(len(payload)) || len(storage.Items) != 1 {
		t.Fatalf("unexpected storage snapshot: %+v", storage)
	}
	_, reader, err := store.Open(context.Background(), item.ID)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	got, err := io.ReadAll(reader)
	if err != nil || !bytes.Equal(got, payload) {
		t.Fatalf("unexpected payload %q: %v", got, err)
	}
	if err := store.Delete(context.Background(), item.ID); err != nil {
		t.Fatal(err)
	}
}

func TestStoreRejectsEmptyRoot(t *testing.T) {
	if _, err := New(" "); err == nil {
		t.Fatal("expected empty software storage root to be rejected")
	}
}
