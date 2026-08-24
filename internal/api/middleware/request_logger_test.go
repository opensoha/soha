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
	router.GET("/failed/:id", func(c *gin.Context) {
		_ = c.Error(errors.New("database unavailable"))
		c.Status(http.StatusInternalServerError)
	})

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/failed/operation-1?token=must-not-be-logged", nil)
	request.RemoteAddr = "192.0.2.10:4321"
	request.Header.Set("traceparent", "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01")
	router.ServeHTTP(recorder, request)

	entries := logs.All()
	if len(entries) != 1 {
		t.Fatalf("log entries = %d, want 1", len(entries))
	}
	if entries[0].Message != "http request failed" {
		t.Fatalf("message = %q", entries[0].Message)
	}
	if entries[0].Level != zapcore.ErrorLevel {
		t.Fatalf("level = %s, want error", entries[0].Level)
	}
	context := entries[0].ContextMap()
	if context["route"] != "/failed/:id" {
		t.Fatalf("route = %#v, want /failed/:id", context["route"])
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
	if _, exists := context["path"]; exists {
		t.Fatal("raw request path must not be logged")
	}
	if context["event"] != "http.request.failed" || context["request_id"] == "" {
		t.Fatalf("request identity fields = %#v", context)
	}
	if context["trace_id"] != "4bf92f3577b34da6a3ce929d0e0e4736" || context["span_id"] != "00f067aa0ba902b7" {
		t.Fatalf("trace fields = trace %#v span %#v", context["trace_id"], context["span_id"])
	}
	if _, ok := context["latency_ms"].(float64); !ok {
		t.Fatalf("latency_ms = %#v, want number", context["latency_ms"])
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
	if entries[0].Level != zapcore.WarnLevel {
		t.Fatalf("level = %s, want warn", entries[0].Level)
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

func TestRequestLoggerKeepsExpectedClientErrorsAtInfo(t *testing.T) {
	gin.SetMode(gin.TestMode)
	core, logs := observer.New(zapcore.DebugLevel)
	router := gin.New()
	router.Use(RequestLogger(zap.New(core)))
	router.GET("/bad-request", func(c *gin.Context) { c.Status(http.StatusBadRequest) })

	router.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/bad-request", nil))
	if entries := logs.All(); len(entries) != 1 || entries[0].Level != zapcore.InfoLevel {
		t.Fatalf("entries = %#v, want one info log", entries)
	}
}

func TestRequestLoggerSuppressesSuccessfulHealthChecks(t *testing.T) {
	gin.SetMode(gin.TestMode)
	core, logs := observer.New(zapcore.DebugLevel)
	router := gin.New()
	router.Use(RequestLogger(zap.New(core)))
	router.GET("/healthz", func(c *gin.Context) { c.Status(http.StatusOK) })

	router.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if logs.Len() != 0 {
		t.Fatalf("health log entries = %d, want 0", logs.Len())
	}
}
