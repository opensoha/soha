package middleware

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	apiresponse "github.com/opensoha/soha/internal/api/response"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"
)

func TestRequestLoggerRecordsCauseWithoutQueryString(t *testing.T) {
	gin.SetMode(gin.TestMode)
	core, logs := observer.New(zapcore.DebugLevel)
	router := gin.New()
	router.Use(RequestID(), RequestLogger(zap.New(core)))
	router.GET("/failed", func(c *gin.Context) {
		_ = c.Error(errors.New("database unavailable"))
		c.Status(http.StatusInternalServerError)
	})

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/failed?token=must-not-be-logged", nil)
	request.RemoteAddr = "192.0.2.10:4321"
	router.ServeHTTP(recorder, request)

	entries := logs.All()
	if len(entries) != 1 {
		t.Fatalf("log entries = %d, want 1", len(entries))
	}
	if entries[0].Message != "http request failed" {
		t.Fatalf("message = %q", entries[0].Message)
	}
	context := entries[0].ContextMap()
	if context["path"] != "/failed" {
		t.Fatalf("path = %#v, want /failed", context["path"])
	}
	if context["error"] != "database unavailable" {
		t.Fatalf("error = %#v", context["error"])
	}
	if context["peer_ip"] != "192.0.2.10" || context["client_ip"] != "192.0.2.10" {
		t.Fatalf("request IP fields = peer %#v client %#v", context["peer_ip"], context["client_ip"])
	}
	if _, exists := context["query"]; exists {
		t.Fatal("request query must not be logged")
	}
}

func TestRequestLoggerRecordsStableErrorCode(t *testing.T) {
	gin.SetMode(gin.TestMode)
	core, logs := observer.New(zapcore.DebugLevel)
	router := gin.New()
	router.Use(RequestID(), RequestLogger(zap.New(core)))
	router.GET("/denied", func(c *gin.Context) {
		apiresponse.Error(c, http.StatusForbidden, "access_denied", "access denied")
	})

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/denied", nil))

	entries := logs.All()
	if len(entries) != 1 {
		t.Fatalf("log entries = %d, want 1", len(entries))
	}
	if got := entries[0].ContextMap()["error_code"]; got != "access_denied" {
		t.Fatalf("error_code = %#v, want access_denied", got)
	}
}

func TestRequestLoggerRedactsAndBoundsErrorCause(t *testing.T) {
	gin.SetMode(gin.TestMode)
	core, logs := observer.New(zapcore.DebugLevel)
	router := gin.New()
	router.Use(RequestLogger(zap.New(core)))
	router.GET("/oauth", func(c *gin.Context) {
		_ = c.Error(errors.New("upstream failed: https://idp.example/callback?code=oauth-secret\nAuthorization: Bearer bearer-secret " + strings.Repeat("x", 4096)))
		c.Status(http.StatusBadGateway)
	})

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/oauth", nil))
	cause, _ := logs.All()[0].ContextMap()["error"].(string)
	if strings.Contains(cause, "oauth-secret") || strings.Contains(cause, "bearer-secret") || strings.Contains(cause, "\n") {
		t.Fatalf("logged cause was not sanitized: %q", cause)
	}
	if len(cause) > 2051 {
		t.Fatalf("logged cause length = %d, want bounded", len(cause))
	}
}
