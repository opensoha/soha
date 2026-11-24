package routes

import (
	"bytes"
	"compress/gzip"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/gin-gonic/gin"
)

func TestSPAAssetEncoding(t *testing.T) {
	script := "console.log('ready')"
	var encoded bytes.Buffer
	zip := gzip.NewWriter(&encoded)
	if _, err := zip.Write([]byte(script)); err != nil {
		t.Fatal(err)
	}
	if err := zip.Close(); err != nil {
		t.Fatal(err)
	}
	router := gin.New()
	registerSPAFS(router, fstest.MapFS{
		"index.html":     {Data: []byte("<html>Soha</html>")},
		"assets/a.js":    {Data: []byte(script)},
		"assets/a.js.gz": {Data: encoded.Bytes()},
	})

	for _, tc := range []struct {
		name, path, accept, byteRange, coding string
		status                                int
	}{
		{"compressed", "/assets/a.js", "br, gzip", "", "gzip", 200},
		{"explicit refusal overrides wildcard", "/assets/a.js", "gzip;q=0, *;q=1", "", "", 200},
		{"range remains identity", "/assets/a.js", "gzip", "bytes=0-6", "", 206},
		{"deep link", "/applications/a", "gzip", "", "", 200},
		{"reserved route", "/api/no-such-path", "gzip", "", "", 404},
	} {
		t.Run(tc.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, tc.path, nil)
			request.Header.Set("Accept-Encoding", tc.accept)
			if tc.byteRange != "" {
				request.Header.Set("Range", tc.byteRange)
			}
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			if response.Code != tc.status || response.Header().Get("Content-Encoding") != tc.coding {
				t.Fatalf("status %d, encoding %q", response.Code, response.Header().Get("Content-Encoding"))
			}
			if tc.path == "/assets/a.js" && response.Header().Get("Vary") != "Accept-Encoding" {
				t.Fatal("negotiated asset response must vary by content encoding")
			}
			if tc.coding != "gzip" {
				return
			}
			if !strings.Contains(response.Header().Get("Content-Type"), "javascript") {
				t.Fatalf("unexpected content type %q", response.Header().Get("Content-Type"))
			}
			body, err := gzip.NewReader(response.Body)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = body.Close() }()
			decoded, err := io.ReadAll(body)
			if err != nil || string(decoded) != script {
				t.Fatalf("invalid compressed asset: %s, %v", decoded, err)
			}
		})
	}
}

func TestSPAWorkbenchEntry(t *testing.T) {
	router := gin.New()
	registerSPAFS(router, fstest.MapFS{
		"index.html":                 {Data: []byte("regular document")},
		"application-workbench.html": {Data: []byte("workbench document")},
	})
	for path, expected := range map[string]string{
		"/applications/a?tab=services": "workbench document",
		"/applications/a/":             "workbench document",
		"/applications":                "regular document",
		"/applications/a/other":        "regular document",
		"/login":                       "regular document",
	} {
		response := httptest.NewRecorder()
		router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))
		if response.Code != http.StatusOK || response.Body.String() != expected {
			t.Errorf("%s: got status %d and body %q, want %q", path, response.Code, response.Body, expected)
		}
	}
}
