package handlers

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	appsoftware "github.com/opensoha/soha/internal/application/software"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
)

type softwareHandlerServiceStub struct {
	imported             appsoftware.URLImportInput
	storageIntegrationID string
	cursor               string
	limit                int
}

func (s *softwareHandlerServiceStub) List(context.Context, domainidentity.Principal, appsoftware.Filter) ([]appsoftware.Package, string, error) {
	return nil, "", nil
}

func (s *softwareHandlerServiceStub) Storage(_ context.Context, _ domainidentity.Principal, storageIntegrationID, cursor string, limit int) (appsoftware.Storage, error) {
	s.storageIntegrationID, s.cursor, s.limit = storageIntegrationID, cursor, limit
	return appsoftware.Storage{Backend: "s3", ObjectCount: 1, TotalBytes: 7, Items: []appsoftware.Package{}}, nil
}

func (s *softwareHandlerServiceStub) Upload(context.Context, domainidentity.Principal, appsoftware.UploadInput, io.Reader) (appsoftware.Package, error) {
	return appsoftware.Package{}, nil
}

func (s *softwareHandlerServiceStub) ImportURL(_ context.Context, _ domainidentity.Principal, input appsoftware.URLImportInput) (appsoftware.Package, error) {
	s.imported = input
	return appsoftware.Package{ID: "pkg-1", Name: input.Name}, nil
}

func (s *softwareHandlerServiceStub) Open(context.Context, domainidentity.Principal, string) (appsoftware.Package, io.ReadCloser, error) {
	return appsoftware.Package{}, io.NopCloser(bytes.NewReader(nil)), nil
}

func (s *softwareHandlerServiceStub) CompleteDownload(context.Context, domainidentity.Principal, appsoftware.Package, int64, time.Duration, error) error {
	return nil
}

func (s *softwareHandlerServiceStub) DownloadRecords(context.Context, domainidentity.Principal, string, int) ([]appsoftware.DownloadRecord, error) {
	return nil, nil
}

func (s *softwareHandlerServiceStub) Delete(context.Context, domainidentity.Principal, string) error {
	return nil
}

func TestSoftwareHandlerImportsURL(t *testing.T) {
	gin.SetMode(gin.TestMode)
	service := &softwareHandlerServiceStub{}
	router := gin.New()
	router.POST("/software/packages/import", NewSoftwareHandler(service).ImportURL)
	request := httptest.NewRequest(http.MethodPost, "/software/packages/import", bytes.NewBufferString(`{
		"softwareId":"demo","name":"Demo","publisher":"OpenSoha","version":"1.0.0",
		"platform":"darwin","arch":"arm64","storageIntegrationId":"storage-2","url":"https://downloads.example.com/demo.pkg","fileName":"release.pkg"
	}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()

	router.ServeHTTP(response, request)

	if response.Code != http.StatusCreated {
		t.Fatalf("unexpected status %d: %s", response.Code, response.Body.String())
	}
	if service.imported.URL != "https://downloads.example.com/demo.pkg" || service.imported.FileName != "release.pkg" || service.imported.StorageIntegrationID != "storage-2" {
		t.Fatalf("unexpected import input: %+v", service.imported)
	}
}

func TestSoftwareHandlerReturnsStoragePage(t *testing.T) {
	gin.SetMode(gin.TestMode)
	service := &softwareHandlerServiceStub{}
	router := gin.New()
	router.GET("/software/storage", NewSoftwareHandler(service).Storage)
	request := httptest.NewRequest(http.MethodGet, "/software/storage?storageIntegrationId=storage-2&cursor=pkg-1&limit=25", nil)
	response := httptest.NewRecorder()

	router.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("unexpected status %d: %s", response.Code, response.Body.String())
	}
	if service.storageIntegrationID != "storage-2" || service.cursor != "pkg-1" || service.limit != 25 {
		t.Fatalf("unexpected storage page cursor=%q limit=%d", service.cursor, service.limit)
	}
}
