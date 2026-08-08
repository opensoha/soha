package handlers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	appmonitoring "github.com/opensoha/soha/internal/application/monitoring"
	domaincopilot "github.com/opensoha/soha/internal/domain/copilot"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainobservability "github.com/opensoha/soha/internal/domain/observability"
)

type dashboardHandlerStub struct {
	rawJSON      string
	dataSourceID string
}

func (s *dashboardHandlerStub) ListDashboards(context.Context, domainidentity.Principal) ([]domainobservability.Dashboard, error) {
	return []domainobservability.Dashboard{}, nil
}

func (s *dashboardHandlerStub) ListMetricDataSources(context.Context, domainidentity.Principal) ([]domaincopilot.DataSource, error) {
	return []domaincopilot.DataSource{{ID: "prometheus-main", Name: "Production Prometheus", BackendType: "prometheus", Enabled: true}}, nil
}

func (s *dashboardHandlerStub) GetDashboard(context.Context, domainidentity.Principal, string) (domainobservability.Dashboard, error) {
	return domainobservability.Dashboard{}, nil
}

func (s *dashboardHandlerStub) ImportGrafanaDashboard(_ context.Context, _ domainidentity.Principal, rawJSON, dataSourceID string) (domainobservability.DashboardImportResult, error) {
	s.rawJSON = rawJSON
	s.dataSourceID = dataSourceID
	return domainobservability.DashboardImportResult{
		Dashboard: domainobservability.Dashboard{ID: "dashboard:1", Name: "Imported", Source: "grafana", Tags: []string{}, Panels: []domainobservability.DashboardPanel{}, CreatedAt: time.Now(), UpdatedAt: time.Now()},
		Warnings:  []domainobservability.DashboardImportWarning{}, ImportedPanelCount: 1,
	}, nil
}

func (s *dashboardHandlerStub) DeleteDashboard(context.Context, domainidentity.Principal, string) error {
	return nil
}

func (s *dashboardHandlerStub) QueryDashboardPanel(context.Context, domainidentity.Principal, string, string, time.Time, time.Time, time.Duration) (appmonitoring.MetricQueryResult, error) {
	return appmonitoring.MetricQueryResult{}, nil
}

func TestObservabilityHandlerImportsGrafanaDashboard(t *testing.T) {
	gin.SetMode(gin.TestMode)
	stub := &dashboardHandlerStub{}
	handler := &ObservabilityHandler{dashboards: stub}
	router := gin.New()
	router.POST("/observability/dashboards/imports", handler.ImportGrafanaDashboard)
	request := httptest.NewRequest(http.MethodPost, "/observability/dashboards/imports", strings.NewReader(`{"json":"{\"title\":\"Imported\"}","dataSourceId":"prometheus-main"}`))
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()

	router.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusCreated || stub.rawJSON != `{"title":"Imported"}` || stub.dataSourceID != "prometheus-main" || !strings.Contains(recorder.Body.String(), `"importedPanelCount":1`) {
		t.Fatalf("status/body/raw = %d %s %q", recorder.Code, recorder.Body.String(), stub.rawJSON)
	}
}

func TestObservabilityHandlerListsMetricDataSources(t *testing.T) {
	gin.SetMode(gin.TestMode)
	handler := &ObservabilityHandler{dashboards: &dashboardHandlerStub{}}
	router := gin.New()
	router.GET("/observability/metrics/data-sources", handler.ListMetricDataSources)
	request := httptest.NewRequest(http.MethodGet, "/observability/metrics/data-sources", nil)
	recorder := httptest.NewRecorder()

	router.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), `"id":"prometheus-main"`) || !strings.Contains(recorder.Body.String(), `"name":"Production Prometheus"`) {
		t.Fatalf("status/body = %d %s", recorder.Code, recorder.Body.String())
	}
}
