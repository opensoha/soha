package handlers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	domaincluster "github.com/opensoha/soha/internal/domain/cluster"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
)

type agentProfileClusterService struct {
	ClusterService
	profile string
}

func (s *agentProfileClusterService) Register(_ context.Context, _ domainidentity.Principal, input domaincluster.RegisterInput) (domaincluster.Summary, error) {
	s.profile = input.AgentActionProfile
	return domaincluster.Summary{ID: input.ID}, nil
}

func (s *agentProfileClusterService) Update(_ context.Context, _ domainidentity.Principal, id string, input domaincluster.UpdateInput) (domaincluster.Summary, error) {
	s.profile = input.AgentActionProfile
	return domaincluster.Summary{ID: id}, nil
}

func TestClusterHTTPPreservesAgentActionProfile(t *testing.T) {
	for _, tc := range []struct{ method, profile string }{{http.MethodPost, "workbench"}, {http.MethodPut, "read-only"}} {
		t.Run(tc.method, func(t *testing.T) {
			service := &agentProfileClusterService{}
			handler := &clusterHandler{service: service}
			router := gin.New()
			router.POST("/clusters", handler.CreateCluster)
			router.PUT("/clusters/:clusterID", handler.UpdateCluster)
			path := "/clusters"
			if tc.method == http.MethodPut {
				path += "/c"
			}
			request := httptest.NewRequest(tc.method, path, strings.NewReader(`{"id":"c","agentActionProfile":"`+tc.profile+`"}`))
			request.Header.Set("Content-Type", "application/json")
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			if response.Code >= http.StatusBadRequest || service.profile != tc.profile {
				t.Fatalf("status=%d profile=%q, want %q", response.Code, service.profile, tc.profile)
			}
		})
	}
}
