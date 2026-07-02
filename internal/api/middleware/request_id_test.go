package middleware

import (
	"strings"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/opensoha/soha/internal/platform/requestctx"
)

func TestRequestIDUsesTraceparentWhenPresent(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(RequestID())
	router.GET("/", func(c *gin.Context) {
		meta := requestctx.FromContext(c.Request.Context())
		c.String(http.StatusOK, meta.RequestID+"|"+meta.TraceID+"|"+meta.SpanID+"|"+c.GetHeader("X-Request-Id"))
	})

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("traceparent", "00-0123456789abcdef0123456789abcdef-0123456789abcdef-01")
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	got := rec.Body.String()
	parts := strings.Split(got, "|")
	if len(parts) != 4 || parts[0] == "" {
		t.Fatalf("request metadata = %q, want request id|trace id|span id|header request id", got)
	}
	if parts[1] != "0123456789abcdef0123456789abcdef" || parts[2] != "0123456789abcdef" {
		t.Fatalf("request metadata trace/span = %q|%q, want traceparent values", parts[1], parts[2])
	}
	if header := rec.Header().Get("X-Request-Id"); header == "" {
		t.Fatal("missing X-Request-Id header")
	}
}
