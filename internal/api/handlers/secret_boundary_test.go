package handlers

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestSecretWritesRejectUnknownTrailingAndOversizeJSON(t *testing.T) {
	gin.SetMode(gin.TestMode)
	service := &captureSecretService{}
	router := gin.New()
	h := NewSecretHandler(service)
	router.POST("/secrets", h.Create)
	router.POST("/secrets/:secretID/versions", h.Rotate)
	for _, path := range []string{"/secrets", "/secrets/secret-one/versions"} {
		for _, body := range []string{`{"value":"x","privateKeyDownload":true}`, `{"value":"x"} {}`, `{"value":"` + strings.Repeat("x", 8<<20) + `"}`} {
			recorder := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodPost, path, bytes.NewBufferString(body))
			request.Header.Set("Content-Type", "application/json")
			router.ServeHTTP(recorder, request)
			if recorder.Code != http.StatusBadRequest {
				t.Fatalf("unbounded or loose request reached service: %s %d", path, recorder.Code)
			}
		}
	}
}
