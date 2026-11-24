package handlers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	dd "github.com/opensoha/soha/internal/domain/delivery"
	di "github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type applicationPodsHandlerStub func(context.Context, di.Principal, string, string, string) (dd.ApplicationServicePods, error)

func (s applicationPodsHandlerStub) GetApplicationServicePods(ctx context.Context, p di.Principal, app, binding, service string) (dd.ApplicationServicePods, error) {
	return s(ctx, p, app, binding, service)
}

func TestDeliveryApplicationServicePodsResponse(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tc := range []struct {
		name string
		err  error
		code int
	}{
		{"available", nil, http.StatusOK},
		{"unauthenticated", apperrors.ErrUnauthorized, http.StatusUnauthorized},
		{"forbidden", apperrors.ErrAccessDenied, http.StatusForbidden},
		{"unknown or mismatched", apperrors.ErrNotFound, http.StatusNotFound},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := &DeliveryHandler{pods: applicationPodsHandlerStub(func(_ context.Context, _ di.Principal, app, binding, service string) (dd.ApplicationServicePods, error) {
				if app != "app" || binding != "env" || service != "svc" {
					t.Fatalf("app=%q binding=%q service=%q", app, binding, service)
				}
				return dd.ApplicationServicePods{Status: "available", Items: []dd.ApplicationServicePod{}}, tc.err
			})}
			path := "/applications/:applicationID/application-environments/:applicationEnvironmentID/services/:serviceID/pods"
			router := gin.New()
			router.GET(path, h.GetApplicationServicePods)
			out := httptest.NewRecorder()
			router.ServeHTTP(out, httptest.NewRequest(http.MethodGet, "/applications/app/application-environments/env/services/svc/pods", nil))
			if got := out.Result(); got.StatusCode != tc.code || got.Header.Get("Cache-Control") != "no-store" {
				t.Fatalf("status=%d headers=%v body=%s", got.StatusCode, got.Header, out.Body)
			}
			if tc.err == nil && !strings.Contains(out.Body.String(), `"data":{"status":"available","items":[]}`) {
				t.Fatalf("unexpected envelope %s", out.Body)
			}
		})
	}
}
