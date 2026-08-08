package software

import (
	"bytes"
	"context"
	"errors"
	"io"
	"testing"

	appaccess "github.com/opensoha/soha/internal/application/access"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type testRolePermissions struct{}

func (testRolePermissions) ListRolePermissions(context.Context) (map[string][]string, error) {
	return map[string][]string{"admin": {appaccess.PermSoftwarePackageCreate}}, nil
}

type testPackageStore struct {
	created bool
}

func (s *testPackageStore) List(context.Context, Filter) ([]Package, string, error) {
	return nil, "", nil
}

func (s *testPackageStore) Storage(context.Context, string, int) (Storage, error) {
	return Storage{Backend: "filesystem"}, nil
}

func (s *testPackageStore) Create(_ context.Context, input UploadInput, _ io.Reader) (Package, error) {
	s.created = true
	return Package{ID: "pkg-1", Name: input.Name, FileName: input.FileName}, nil
}

func (s *testPackageStore) Open(context.Context, string) (Package, io.ReadCloser, error) {
	return Package{}, nil, apperrors.ErrNotFound
}

func (s *testPackageStore) Delete(context.Context, string) error { return nil }

type testURLFetcher struct {
	called bool
}

func (f *testURLFetcher) Fetch(context.Context, string) (RemoteFile, error) {
	f.called = true
	return RemoteFile{FileName: "demo.pkg", Content: io.NopCloser(bytes.NewBufferString("payload"))}, nil
}

func TestUploadRejectsUnsupportedInstallerBeforeStorage(t *testing.T) {
	store := &testPackageStore{}
	service := New(store, nil, appaccess.NewPermissionResolver(testRolePermissions{}), nil, nil)
	principal := domainidentity.Principal{Roles: []string{"admin"}}

	_, err := service.Upload(context.Background(), principal, UploadInput{
		SoftwareID: "demo", Name: "Demo", Publisher: "OpenSoha", Version: "1.0.0",
		Platform: "darwin", Arch: "arm64", FileName: "demo.txt",
	}, bytes.NewBufferString("payload"))
	if !errors.Is(err, apperrors.ErrInvalidArgument) {
		t.Fatalf("expected invalid argument, got %v", err)
	}
	if store.created {
		t.Fatal("unsupported installer reached storage")
	}
}

func TestImportURLUsesManagedStorage(t *testing.T) {
	store := &testPackageStore{}
	fetcher := &testURLFetcher{}
	service := New(store, fetcher, appaccess.NewPermissionResolver(testRolePermissions{}), nil, nil)
	principal := domainidentity.Principal{Roles: []string{"admin"}}

	item, err := service.ImportURL(context.Background(), principal, URLImportInput{
		UploadInput: UploadInput{
			SoftwareID: "demo", Name: "Demo", Publisher: "OpenSoha", Version: "1.0.0",
			Platform: "darwin", Arch: "arm64",
		},
		URL: "https://downloads.example.com/demo.pkg",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !fetcher.called || !store.created || item.FileName != "demo.pkg" {
		t.Fatalf("unexpected import result: fetcher=%v store=%v item=%+v", fetcher.called, store.created, item)
	}
}

func TestImportURLRejectsMetadataBeforeFetch(t *testing.T) {
	store := &testPackageStore{}
	fetcher := &testURLFetcher{}
	service := New(store, fetcher, appaccess.NewPermissionResolver(testRolePermissions{}), nil, nil)
	principal := domainidentity.Principal{Roles: []string{"admin"}}

	_, err := service.ImportURL(context.Background(), principal, URLImportInput{
		UploadInput: UploadInput{SoftwareID: "../demo", Name: "Demo", Publisher: "OpenSoha", Version: "1.0.0", Platform: "darwin", Arch: "arm64"},
		URL:         "https://downloads.example.com/demo.pkg",
	})
	if !errors.Is(err, apperrors.ErrInvalidArgument) || fetcher.called || store.created {
		t.Fatalf("invalid metadata reached fetch or storage: err=%v fetcher=%v store=%v", err, fetcher.called, store.created)
	}
}
