package handlers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	domain "github.com/opensoha/soha/internal/domain/copilot"
	identity "github.com/opensoha/soha/internal/domain/identity"
)

type runtimeAssetHandlerTestService struct {
	CopilotRuntimeAssetService
	ref domain.RuntimeAssetRef
}

func (s *runtimeAssetHandlerTestService) SetRuntimeAssetEnabled(_ context.Context, _ identity.Principal, ref domain.RuntimeAssetRef, enabled bool) (domain.RuntimeAsset, error) {
	s.ref = ref
	return domain.RuntimeAsset{RuntimeAssetDescriptor: domain.RuntimeAssetDescriptor{Kind: ref.Kind, ID: ref.ID, Version: ref.Version}, Enabled: enabled}, nil
}

func TestRuntimeAssetHandlerToggleBody(t *testing.T) {
	const path = "/settings/ai/workbench-runtime-assets/mcp/example/2"
	for _, tc := range []struct {
		name, body string
		status     int
	}{
		{"absent", `{}`, http.StatusBadRequest},
		{"operator source fields rejected", `{"enabled":true,"url":"https://example.invalid"}`, http.StatusBadRequest},
		{"explicit false", `{"enabled":false}`, http.StatusOK},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc := &runtimeAssetHandlerTestService{}
			handler := &copilotRuntimeAssetHandler{service: svc}
			router := gin.New()
			router.PUT("/settings/ai/workbench-runtime-assets/:kind/:assetId/:version", handler.SetRuntimeAssetEnabled)
			res := httptest.NewRecorder()
			router.ServeHTTP(res, httptest.NewRequest(http.MethodPut, path, strings.NewReader(tc.body)))
			if res.Code != tc.status {
				t.Fatalf("status = %d, want %d: %s", res.Code, tc.status, res.Body.String())
			}
			if tc.status == http.StatusOK {
				if svc.ref != (domain.RuntimeAssetRef{Kind: "mcp", ID: "example", Version: "2"}) || !strings.Contains(res.Body.String(), `"enabled":false`) {
					t.Fatalf("toggle binding: %+v, %s", svc.ref, res.Body.String())
				}
			} else if svc.ref != (domain.RuntimeAssetRef{}) {
				t.Fatalf("invalid body called service: %+v", svc.ref)
			}
		})
	}
}
