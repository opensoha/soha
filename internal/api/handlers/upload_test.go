package handlers

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
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

func TestUploadBrandingAssetAcceptsPNGAndReturnsDataURL(t *testing.T) {
	content := []byte{
		0x89, 'P', 'N', 'G', 0x0d, 0x0a, 0x1a, 0x0a,
		0x00, 0x00, 0x00, 0x0d, 'I', 'H', 'D', 'R',
	}
	recorder := postBrandingUpload(t, "logo.png", content)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body=%s", recorder.Code, http.StatusOK, recorder.Body.String())
	}
	decoded := brandingUploadContent(t, recorder, "image/png")
	if !bytes.Equal(decoded, content) {
		t.Fatalf("decoded content = %v, want %v", decoded, content)
	}
}

func TestUploadBrandingAssetAcceptsStaticSVG(t *testing.T) {
	content := []byte(`<?xml version="1.0"?><svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24"><metadata>removed</metadata><defs><linearGradient id="brand"><stop offset="0" stop-color="#1677ff"/></linearGradient><symbol id="mark"><path d="M0 0h24v24H0z" fill="url(#brand)"/></symbol></defs><use href="#mark"/></svg>`)
	recorder := postBrandingUpload(t, "logo.svg", content)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body=%s", recorder.Code, http.StatusOK, recorder.Body.String())
	}
	decoded := brandingUploadContent(t, recorder, "image/svg+xml")
	if strings.Contains(string(decoded), "metadata") || !strings.Contains(string(decoded), `fill="url(#brand)"`) {
		t.Fatalf("sanitized SVG = %q", decoded)
	}
}

func TestUploadBrandingAssetRejectsUnsafeSVG(t *testing.T) {
	tests := map[string]string{
		"script":             `<svg xmlns="http://www.w3.org/2000/svg"><script>alert(1)</script></svg>`,
		"foreign object":     `<svg xmlns="http://www.w3.org/2000/svg"><foreignObject><div>bad</div></foreignObject></svg>`,
		"event handler":      `<svg xmlns="http://www.w3.org/2000/svg" onload="alert(1)"/>`,
		"external reference": `<svg xmlns="http://www.w3.org/2000/svg"><use href="https://example.com/icon.svg#mark"/></svg>`,
		"embedded image":     `<svg xmlns="http://www.w3.org/2000/svg"><image href="data:image/png;base64,AAAA"/></svg>`,
		"style":              `<svg xmlns="http://www.w3.org/2000/svg"><style>path { fill: red; }</style><path d="M0 0"/></svg>`,
		"animation":          `<svg xmlns="http://www.w3.org/2000/svg"><animate attributeName="x" from="0" to="1"/></svg>`,
		"doctype":            `<!DOCTYPE svg [<!ENTITY xxe SYSTEM "file:///etc/passwd">]><svg xmlns="http://www.w3.org/2000/svg"/>`,
	}

	for name, content := range tests {
		t.Run(name, func(t *testing.T) {
			recorder := postBrandingUpload(t, "logo.svg", []byte(content))

			if recorder.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want %d, body=%s", recorder.Code, http.StatusBadRequest, recorder.Body.String())
			}
			if !strings.Contains(recorder.Body.String(), "unsafe or unsupported SVG content") {
				t.Fatalf("body = %s", recorder.Body.String())
			}
		})
	}
}

func TestUploadBrandingAssetRejectsExtensionSpoofing(t *testing.T) {
	recorder := postBrandingUpload(t, "logo.png", []byte(`<html>not an image</html>`))

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d, body=%s", recorder.Code, http.StatusBadRequest, recorder.Body.String())
	}
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

func brandingUploadContent(t *testing.T, recorder *httptest.ResponseRecorder, contentType string) []byte {
	t.Helper()
	var payload struct {
		Data struct {
			URL string `json:"url"`
		} `json:"data"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	raw, ok := strings.CutPrefix(payload.Data.URL, "data:"+contentType+";base64,")
	if !ok {
		t.Fatalf("url = %q, want %s data URL", payload.Data.URL, contentType)
	}
	decoded, err := base64.StdEncoding.DecodeString(raw)
	if err != nil {
		t.Fatalf("decode data URL: %v", err)
	}
	return decoded
}
