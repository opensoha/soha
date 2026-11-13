package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"
)

func TestRequestLoggerUsesValidatedIDWithoutLoggingQuery(t *testing.T) {
	gin.SetMode(gin.TestMode)
	core, logs := observer.New(zapcore.DebugLevel)
	router := gin.New()
	router.Use(RequestID(), RequestLogger(zap.New(core)))
	router.GET("/failed/:id", func(c *gin.Context) { c.Status(http.StatusInternalServerError) })

	request := httptest.NewRequest(http.MethodGet, "/failed/task-1?token=must-not-be-logged", nil)
	request.RemoteAddr = "192.0.2.10:4321"
	request.Header.Set("X-Request-Id", "invalid id with spaces")
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)

	entries := logs.All()
	if len(entries) != 1 || entries[0].Level != zapcore.ErrorLevel {
		t.Fatalf("entries = %#v, want one error log", entries)
	}
	fields := entries[0].ContextMap()
	requestID, _ := fields["request_id"].(string)
	if requestID == "" || requestID == "invalid id with spaces" || recorder.Header().Get("X-Request-Id") != requestID {
		t.Fatalf("request ID fields = %#v, response = %q", fields, recorder.Header().Get("X-Request-Id"))
	}
	if fields["route"] != "/failed/:id" || fields["peer_ip"] != "192.0.2.10" || fields["event"] != "http.request.failed" {
		t.Fatalf("request log fields = %#v", fields)
	}
	if _, exists := fields["query"]; exists {
		t.Fatal("request query must not be logged")
	}
}
