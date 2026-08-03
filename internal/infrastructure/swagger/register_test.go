package swagger

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	contractsopenapi "github.com/opensoha/soha-contracts/openapi"
)

func TestRegisterServesCanonicalOpenAPI(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	Register(router, true, "/swagger/*any")

	for _, test := range []struct {
		path        string
		contentType string
		body        []byte
		sha256      string
	}{
		{path: "/swagger/openapi.json", contentType: "application/json; charset=utf-8", body: contractsopenapi.JSON(), sha256: contractsopenapi.JSONSHA256},
		{path: "/swagger/openapi.yaml", contentType: "application/yaml; charset=utf-8", body: contractsopenapi.YAML(), sha256: contractsopenapi.YAMLSHA256},
	} {
		t.Run(test.path, func(t *testing.T) {
			response := httptest.NewRecorder()
			router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, test.path, nil))

			if response.Code != http.StatusOK || !bytes.Equal(response.Body.Bytes(), test.body) {
				t.Fatalf("response = %d (%d bytes), want 200 (%d bytes)", response.Code, response.Body.Len(), len(test.body))
			}
			if got := response.Header().Get("Content-Type"); got != test.contentType {
				t.Fatalf("Content-Type = %q, want %q", got, test.contentType)
			}
			if got := response.Header().Get("X-Soha-Contracts-Version"); got != contractsopenapi.Version {
				t.Fatalf("contracts version = %q, want %q", got, contractsopenapi.Version)
			}
			if got := response.Header().Get("X-Soha-Contracts-SHA256"); got != test.sha256 {
				t.Fatalf("contracts sha256 = %q, want %q", got, test.sha256)
			}
		})
	}
}

func TestRegisterRedirectsRootAndRejectsUnknownFiles(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	Register(router, true, "/swagger/*any")

	root := httptest.NewRecorder()
	router.ServeHTTP(root, httptest.NewRequest(http.MethodGet, "/swagger/", nil))
	if root.Code != http.StatusTemporaryRedirect || root.Header().Get("Location") != "/swagger/openapi.json" {
		t.Fatalf("root redirect = %d %q", root.Code, root.Header().Get("Location"))
	}

	unknown := httptest.NewRecorder()
	router.ServeHTTP(unknown, httptest.NewRequest(http.MethodGet, "/swagger/unknown", nil))
	if unknown.Code != http.StatusNotFound {
		t.Fatalf("unknown status = %d, want 404", unknown.Code)
	}
}
