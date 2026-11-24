package response

import (
	"compress/gzip"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestGzipItemPreservesJSON(t *testing.T) {
	payload := strings.Repeat("service", 400)
	router := gin.New()
	router.GET("/", func(c *gin.Context) { GzipItem(c, payload) })
	for _, accept := range []string{"gzip", "gzip;q=0", "identity"} {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.Header.Set("Accept-Encoding", accept)
		result := httptest.NewRecorder()
		router.ServeHTTP(result, req)

		var body io.Reader = result.Body
		if accept == "gzip" {
			if result.Header().Get("Content-Encoding") != "gzip" {
				t.Fatal("expected gzip response")
			}
			decoded, err := gzip.NewReader(body)
			if err != nil {
				t.Fatal(err)
			}
			defer decoded.Close()
			body = decoded
		} else if result.Header().Get("Content-Encoding") != "" {
			t.Fatal("expected identity response")
		}
		decoded, err := io.ReadAll(body)
		if err != nil || string(decoded) != `{"data":"`+payload+`"}` {
			t.Fatalf("unexpected JSON response for %s: %v", accept, err)
		}
	}
}
