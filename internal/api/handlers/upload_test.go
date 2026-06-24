package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/gin-gonic/gin"
	appaccess "github.com/opensoha/soha/internal/application/access"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
)

type stubUploadRolePermissionReader struct {
	matrix map[string][]string
}

func (s stubUploadRolePermissionReader) ListRolePermissions(context.Context) (map[string][]string, error) {
	return s.matrix, nil
}

func TestUploadBrandingAssetAcceptsPNGAndUsesRandomFilename(t *testing.T) {
	dir := t.TempDir()
	withBrandingUploadDir(t, dir)

	recorder := postBrandingUpload(t, "logo.png", []byte{
		0x89, 'P', 'N', 'G', 0x0d, 0x0a, 0x1a, 0x0a,
		0x00, 0x00, 0x00, 0x0d, 'I', 'H', 'D', 'R',
	})

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body=%s", recorder.Code, http.StatusOK, recorder.Body.String())
	}

	var payload struct {
		Data struct {
			URL      string `json:"url"`
			Filename string `json:"filename"`
		} `json:"data"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if filepath.Ext(payload.Data.Filename) != ".png" {
		t.Fatalf("filename = %q, want .png extension", payload.Data.Filename)
	}
	if payload.Data.Filename == "logo.png" {
		t.Fatalf("filename reused user-provided name")
	}
	if payload.Data.URL != brandingURLPathBase+payload.Data.Filename {
		t.Fatalf("url = %q, want branding asset URL", payload.Data.URL)
	}
	if _, err := os.Stat(filepath.Join(dir, payload.Data.Filename)); err != nil {
		t.Fatalf("uploaded file not saved: %v", err)
	}
}

func TestUploadBrandingAssetRejectsSVG(t *testing.T) {
	dir := t.TempDir()
	withBrandingUploadDir(t, dir)

	recorder := postBrandingUpload(t, "logo.svg", []byte(`<svg xmlns="http://www.w3.org/2000/svg"><script>alert(1)</script></svg>`))

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d, body=%s", recorder.Code, http.StatusBadRequest, recorder.Body.String())
	}
}

func TestUploadBrandingAssetRejectsExtensionSpoofing(t *testing.T) {
	dir := t.TempDir()
	withBrandingUploadDir(t, dir)

	recorder := postBrandingUpload(t, "logo.png", []byte(`<html>not an image</html>`))

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d, body=%s", recorder.Code, http.StatusBadRequest, recorder.Body.String())
	}
}

func withBrandingUploadDir(t *testing.T, dir string) {
	t.Helper()
	previous := brandingUploadDir
	brandingUploadDir = dir
	t.Cleanup(func() {
		brandingUploadDir = previous
	})
}

func postBrandingUpload(t *testing.T, filename string, content []byte) *httptest.ResponseRecorder {
	t.Helper()

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("file", filename)
	if err != nil {
		t.Fatalf("create multipart file: %v", err)
	}
	if _, err := part.Write(content); err != nil {
		t.Fatalf("write multipart file: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close multipart writer: %v", err)
	}

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/api/v1/settings/branding/upload", &body)
	ctx.Request.Header.Set("Content-Type", writer.FormDataContentType())
	ctx.Set("principal", domainidentity.Principal{UserID: "u-1", Roles: []string{"admin"}})

	handler := NewSettingsHandler(
		nil,
		appaccess.NewPermissionResolver(stubUploadRolePermissionReader{
			matrix: map[string][]string{
				"admin": {appaccess.PermSettingsBrandingManage},
			},
		}),
	)
	handler.UploadBrandingAsset(ctx)
	return recorder
}
