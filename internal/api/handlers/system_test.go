package handlers

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/opensoha/soha/internal/platform/runtimeobs"
)

type failingReadinessProbe struct {
	err error
}

func (p failingReadinessProbe) Ping(context.Context) error { return p.err }

type noopRuntimeMetricsProvider struct{}

func (noopRuntimeMetricsProvider) Snapshot() runtimeobs.Snapshot { return runtimeobs.Snapshot{} }

func TestSystemHealthzRedactsPostgresError(t *testing.T) {
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/healthz", nil)
	handler := NewSystemHandler(failingReadinessProbe{err: errors.New("connection refused")}, noopRuntimeMetricsProvider{})

	handler.Healthz(ctx)

	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusServiceUnavailable)
	}
	if got := recorder.Body.String(); !strings.Contains(got, `"postgres":"unavailable"`) {
		t.Fatalf("response = %s, want postgres unavailable", got)
	}
	if strings.Contains(recorder.Body.String(), "refused") {
		t.Fatalf("response should not include backend details: %s", recorder.Body.String())
	}
}

func TestSystemReadyzUsesFixedUnavailableMessage(t *testing.T) {
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/readyz", nil)
	handler := NewSystemHandler(failingReadinessProbe{err: errors.New("tls handshake failed")}, noopRuntimeMetricsProvider{})

	handler.Readyz(ctx)

	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusServiceUnavailable)
	}
	if strings.Contains(recorder.Body.String(), "handshake") {
		t.Fatalf("response should not include backend details: %s", recorder.Body.String())
	}
	if !strings.Contains(recorder.Body.String(), "postgres unavailable") {
		t.Fatalf("response = %s, want fixed unavailable message", recorder.Body.String())
	}
}
