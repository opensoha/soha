package handlers

import (
	"context"
	"github.com/gin-gonic/gin"
	domaincluster "github.com/opensoha/soha/internal/domain/cluster"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type metricIdentityClusterService struct {
	ClusterService
	registered domaincluster.RegisterInput
	updated    domaincluster.UpdateInput
}

func (s *metricIdentityClusterService) Register(_ context.Context, _ domainidentity.Principal, input domaincluster.RegisterInput) (domaincluster.Summary, error) {
	s.registered = input
	return domaincluster.Summary{ID: "real-id"}, nil
}
func (s *metricIdentityClusterService) Update(_ context.Context, _ domainidentity.Principal, _ string, input domaincluster.UpdateInput) (domaincluster.Summary, error) {
	s.updated = input
	return domaincluster.Summary{ID: "real-id"}, nil
}
func TestClusterHandlersBindMetricIdentityAndExplicitReset(t *testing.T) {
	service := &metricIdentityClusterService{}
	handler := &clusterHandler{service: service}
	for _, body := range []string{`{"prometheusClusterLabel":"cluster","prometheusClusterLabelValue":"middleware-prod","prometheusBaseUrl":"http://metrics.invalid"}`, `{"prometheusClusterLabel":"","prometheusClusterLabelValue":"","prometheusBaseUrl":""}`, `{}`} {
		recorder := httptest.NewRecorder()
		ctx, _ := gin.CreateTestContext(recorder)
		ctx.Request = httptest.NewRequest(http.MethodPut, "/clusters/real-id", strings.NewReader(body))
		ctx.Request.Header.Set("Content-Type", "application/json")
		ctx.Params = gin.Params{{Key: "clusterID", Value: "real-id"}}
		handler.UpdateCluster(ctx)
		if recorder.Code != 200 {
			t.Fatalf("status %d", recorder.Code)
		}
		if body == `{}` {
			if service.updated.PrometheusClusterLabel != nil || service.updated.PrometheusClusterLabelValue != nil || service.updated.PrometheusBaseURL != nil {
				t.Fatal("omitted value became a reset")
			}
		} else if service.updated.PrometheusClusterLabel == nil || service.updated.PrometheusClusterLabelValue == nil || service.updated.PrometheusBaseURL == nil {
			t.Fatal("explicit value lost")
		} else if strings.Contains(body, `"prometheusBaseUrl":""`) && *service.updated.PrometheusBaseURL != "" {
			t.Fatal("explicit empty Prometheus URL was changed")
		} else if strings.Contains(body, `"prometheusClusterLabel":""`) && *service.updated.PrometheusClusterLabel != "" {
			t.Fatal("explicit empty Prometheus label was changed")
		}
	}
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/clusters", strings.NewReader(`{"prometheusClusterLabel":"cluster","prometheusClusterLabelValue":"middleware-prod"}`))
	ctx.Request.Header.Set("Content-Type", "application/json")
	handler.CreateCluster(ctx)
	if recorder.Code != 201 || service.registered.PrometheusClusterLabelValue != "middleware-prod" {
		t.Fatal("registration lost metric alias")
	}
}
