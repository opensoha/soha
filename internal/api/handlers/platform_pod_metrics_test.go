package handlers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/gin-gonic/gin"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainresource "github.com/opensoha/soha/internal/domain/resource"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type podMetricsHandlerSpy struct {
	PodDiagnostics
	calls                                                 int
	principal                                             domainidentity.Principal
	clusterID, namespace, podName, expectedUID, container string
	rangeMinutes, stepSeconds                             int
	err                                                   error
}

func (s *podMetricsHandlerSpy) GetPodMetricsForIdentity(_ context.Context, principal domainidentity.Principal, clusterID, namespace, podName, expectedUID, container string, rangeMinutes, stepSeconds int) (domainresource.PodMetricsView, error) {
	s.calls++
	s.principal = principal
	s.clusterID = clusterID
	s.namespace = namespace
	s.podName = podName
	s.expectedUID = expectedUID
	s.container = container
	s.rangeMinutes = rangeMinutes
	s.stepSeconds = stepSeconds
	if s.err != nil {
		return domainresource.PodMetricsView{}, s.err
	}
	return domainresource.PodMetricsView{
		PodName:      podName,
		Namespace:    namespace,
		RangeMinutes: rangeMinutes,
		StepSeconds:  stepSeconds,
	}, nil
}

func servePodMetrics(t *testing.T, spy *podMetricsHandlerSpy, rawQuery string) *httptest.ResponseRecorder {
	t.Helper()
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set("principal", domainidentity.Principal{UserID: "metrics-reader"})
	})
	router.GET("/clusters/:clusterID/workloads/pods/:podName/metrics", (&podResourceHandler{diagnostics: spy}).GetPodMetrics)

	target := "/clusters/cluster-a/workloads/pods/api-0/metrics"
	if rawQuery != "" {
		target += "?" + rawQuery
	}
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, target, nil))
	return response
}

func TestGetPodMetricsForwardsIdentityFiltersNamespaceAndWindow(t *testing.T) {
	const expectedUID = "5c0d4c8e-04a4-4a3d-93a5-1c3f58c9a300"
	spy := &podMetricsHandlerSpy{}
	query := url.Values{
		"namespace":    {"team-a"},
		"expectedUid":  {expectedUID},
		"container":    {"api-sidecar"},
		"rangeMinutes": {"120"},
		"stepSeconds":  {"30"},
	}.Encode()

	response := servePodMetrics(t, spy, query)

	if response.Code != http.StatusOK || spy.calls != 1 {
		t.Fatalf("status = %d, calls = %d, body = %s", response.Code, spy.calls, response.Body.String())
	}
	if spy.principal.UserID != "metrics-reader" || spy.clusterID != "cluster-a" || spy.namespace != "team-a" || spy.podName != "api-0" {
		t.Fatalf("request identity = %+v", spy)
	}
	if spy.expectedUID != expectedUID || spy.container != "api-sidecar" {
		t.Fatalf("identity filters = uid %q container %q", spy.expectedUID, spy.container)
	}
	if spy.rangeMinutes != 120 || spy.stepSeconds != 30 {
		t.Fatalf("window = %d/%d", spy.rangeMinutes, spy.stepSeconds)
	}
	if response.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("Cache-Control = %q", response.Header().Get("Cache-Control"))
	}
}

func TestGetPodMetricsForwardsLegacyRequestWithoutIdentityFilters(t *testing.T) {
	spy := &podMetricsHandlerSpy{}
	query := url.Values{
		"namespace":    {"team-a"},
		"rangeMinutes": {"90"},
		"stepSeconds":  {"45"},
	}.Encode()

	response := servePodMetrics(t, spy, query)

	if response.Code != http.StatusOK || spy.calls != 1 {
		t.Fatalf("status = %d, calls = %d, body = %s", response.Code, spy.calls, response.Body.String())
	}
	if spy.expectedUID != "" || spy.container != "" {
		t.Fatalf("legacy request gained identity filters: uid %q container %q", spy.expectedUID, spy.container)
	}
	if spy.namespace != "team-a" || spy.rangeMinutes != 90 || spy.stepSeconds != 45 {
		t.Fatalf("legacy request = %+v", spy)
	}
	if response.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("Cache-Control = %q", response.Header().Get("Cache-Control"))
	}
}

func TestGetPodMetricsRejectsInvalidFiltersBeforeDiagnostics(t *testing.T) {
	for _, tc := range []struct {
		name  string
		query string
	}{
		{name: "empty uid", query: "namespace=team-a&expectedUid=&container=api-sidecar"},
		{name: "whitespace uid", query: "namespace=team-a&expectedUid=%20&container=api-sidecar"},
		{name: "duplicate uid", query: "namespace=team-a&expectedUid=uid-a&expectedUid=uid-b&container=api-sidecar"},
		{name: "empty container", query: "namespace=team-a&expectedUid=uid-a&container="},
		{name: "whitespace container", query: "namespace=team-a&expectedUid=uid-a&container=%20"},
		{name: "duplicate container", query: "namespace=team-a&expectedUid=uid-a&container=api&container=sidecar"},
		{name: "malformed uid", query: "namespace=team-a&expectedUid=%XX&container=api-sidecar"},
		{name: "malformed container", query: "namespace=team-a&expectedUid=uid-a&container=%XX"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			spy := &podMetricsHandlerSpy{}
			response := servePodMetrics(t, spy, tc.query)
			if response.Code != http.StatusBadRequest || spy.calls != 0 {
				t.Fatalf("status = %d, calls = %d, body = %s", response.Code, spy.calls, response.Body.String())
			}
		})
	}
}

func TestGetPodMetricsReturnsConflictForChangedUID(t *testing.T) {
	spy := &podMetricsHandlerSpy{err: apperrors.ErrConflict}
	query := url.Values{
		"namespace":   {"team-a"},
		"expectedUid": {"old-uid"},
		"container":   {"api-sidecar"},
	}.Encode()

	response := servePodMetrics(t, spy, query)

	if response.Code != http.StatusConflict || spy.calls != 1 {
		t.Fatalf("status = %d, calls = %d, body = %s", response.Code, spy.calls, response.Body.String())
	}
	if spy.expectedUID != "old-uid" || spy.container != "api-sidecar" {
		t.Fatalf("conflict request = %+v", spy)
	}
	if response.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("Cache-Control = %q", response.Header().Get("Cache-Control"))
	}
}
