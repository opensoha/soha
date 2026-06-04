package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	domainidentity "github.com/soha/soha/internal/domain/identity"
	domainresource "github.com/soha/soha/internal/domain/resource"
	"github.com/soha/soha/internal/platform/apperrors"
)

type stubPlatformResourceService struct {
	ResourceService

	listPodsClusterID  string
	listPodsNamespace  string
	listPodsPrincipal  domainidentity.Principal
	listPodsCalled     bool
	listPodsErr        error
	podLogsNamespace   string
	podLogsContainer   string
	podLogsTailLines   int64
	podLogsSince       int64
	podLogsPrevious    bool
	applyPodYAMLCalled bool
	applyPodYAMLBody   string
	serviceListErr     error
	crdCreateNamespace string
	crdCreateContent   string
	helmInstallInput   domainresource.HelmChartInstallInput
}

func (s *stubPlatformResourceService) ListPods(_ context.Context, principal domainidentity.Principal, clusterID, namespace string) ([]domainresource.PodView, error) {
	s.listPodsCalled = true
	s.listPodsPrincipal = principal
	s.listPodsClusterID = clusterID
	s.listPodsNamespace = namespace
	if s.listPodsErr != nil {
		return nil, s.listPodsErr
	}
	return []domainresource.PodView{{Name: "api-0", Namespace: namespace, Phase: "Running"}}, nil
}

func (s *stubPlatformResourceService) GetPodLogs(_ context.Context, _ domainidentity.Principal, _, namespace, podName, container string, tailLines, sinceSeconds int64, previous bool) (domainresource.PodLogsView, error) {
	s.podLogsNamespace = namespace
	s.podLogsContainer = container
	s.podLogsTailLines = tailLines
	s.podLogsSince = sinceSeconds
	s.podLogsPrevious = previous
	return domainresource.PodLogsView{PodName: podName, Namespace: namespace, Container: container, TailLines: tailLines, Previous: previous}, nil
}

func (s *stubPlatformResourceService) ApplyPodYAML(_ context.Context, _ domainidentity.Principal, _, namespace, podName, content string) (domainresource.ResourceYAMLView, error) {
	s.applyPodYAMLCalled = true
	s.applyPodYAMLBody = content
	return domainresource.ResourceYAMLView{Kind: "Pod", Name: podName, Namespace: namespace, Content: content}, nil
}

func (s *stubPlatformResourceService) ListServices(context.Context, domainidentity.Principal, string, string) ([]domainresource.ServiceView, error) {
	return nil, s.serviceListErr
}

func (s *stubPlatformResourceService) CreateCRDResourceFromYAML(_ context.Context, _ domainidentity.Principal, _, _, namespace, content string) (domainresource.ResourceYAMLView, error) {
	s.crdCreateNamespace = namespace
	s.crdCreateContent = content
	return domainresource.ResourceYAMLView{Kind: "Widget", Name: "sample", Namespace: namespace, Content: content}, nil
}

func (s *stubPlatformResourceService) InstallHelmChart(_ context.Context, _ domainidentity.Principal, _ string, input domainresource.HelmChartInstallInput) (domainresource.HelmChartInstallResult, error) {
	s.helmInstallInput = input
	return domainresource.HelmChartInstallResult{Name: input.ReleaseName, Namespace: input.Namespace, Status: "deployed"}, nil
}

func newPlatformTestContext(method, target, body string, params gin.Params) (*gin.Context, *httptest.ResponseRecorder) {
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(method, target, strings.NewReader(body))
	ctx.Request.Header.Set("Content-Type", "application/json")
	ctx.Params = params
	ctx.Set("principal", domainidentity.Principal{UserID: "u-1", UserName: "operator", Roles: []string{"platform-admin"}})
	return ctx, recorder
}

func TestPlatformListPodsPassesScopeAndPrincipal(t *testing.T) {
	gin.SetMode(gin.TestMode)

	resources := &stubPlatformResourceService{}
	handler := NewPlatformHandler(nil, resources, nil, nil, nil, nil)
	ctx, recorder := newPlatformTestContext(http.MethodGet, "/api/v1/clusters/cluster-a/workloads/pods?namespace=team-a", "", gin.Params{{Key: "clusterID", Value: "cluster-a"}})

	handler.ListPods(ctx)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body=%s", recorder.Code, http.StatusOK, recorder.Body.String())
	}
	if !resources.listPodsCalled {
		t.Fatal("ListPods was not called")
	}
	if resources.listPodsClusterID != "cluster-a" || resources.listPodsNamespace != "team-a" {
		t.Fatalf("scope = %s/%s, want cluster-a/team-a", resources.listPodsClusterID, resources.listPodsNamespace)
	}
	if resources.listPodsPrincipal.UserID != "u-1" {
		t.Fatalf("principal.UserID = %q, want u-1", resources.listPodsPrincipal.UserID)
	}

	var payload struct {
		Items []domainresource.PodView `json:"items"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(payload.Items) != 1 || payload.Items[0].Name != "api-0" {
		t.Fatalf("items = %#v", payload.Items)
	}
}

func TestPlatformGetPodLogsBindsDefaultsAndQuery(t *testing.T) {
	gin.SetMode(gin.TestMode)

	resources := &stubPlatformResourceService{}
	handler := NewPlatformHandler(nil, resources, nil, nil, nil, nil)
	ctx, recorder := newPlatformTestContext(http.MethodGet, "/api/v1/clusters/cluster-a/workloads/pods/api-0/logs?tailLines=50&sinceSeconds=300&previous=true&container=api", "", gin.Params{{Key: "clusterID", Value: "cluster-a"}, {Key: "podName", Value: "api-0"}})

	handler.GetPodLogs(ctx)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body=%s", recorder.Code, http.StatusOK, recorder.Body.String())
	}
	if resources.podLogsNamespace != "default" {
		t.Fatalf("namespace = %q, want default", resources.podLogsNamespace)
	}
	if resources.podLogsContainer != "api" || resources.podLogsTailLines != 50 || resources.podLogsSince != 300 || !resources.podLogsPrevious {
		t.Fatalf("log args = container:%q tail:%d since:%d previous:%v", resources.podLogsContainer, resources.podLogsTailLines, resources.podLogsSince, resources.podLogsPrevious)
	}
}

func TestPlatformApplyPodYAMLRejectsInvalidPayload(t *testing.T) {
	gin.SetMode(gin.TestMode)

	resources := &stubPlatformResourceService{}
	handler := NewPlatformHandler(nil, resources, nil, nil, nil, nil)
	ctx, recorder := newPlatformTestContext(http.MethodPut, "/api/v1/clusters/cluster-a/workloads/pods/api-0/yaml", "{", gin.Params{{Key: "clusterID", Value: "cluster-a"}, {Key: "podName", Value: "api-0"}})

	handler.ApplyPodYAML(ctx)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d, body=%s", recorder.Code, http.StatusBadRequest, recorder.Body.String())
	}
	if resources.applyPodYAMLCalled {
		t.Fatal("ApplyPodYAML was called for invalid payload")
	}
}

func TestPlatformCreateCRDResourcePrefersBodyNamespace(t *testing.T) {
	gin.SetMode(gin.TestMode)

	resources := &stubPlatformResourceService{}
	handler := NewPlatformHandler(nil, resources, nil, nil, nil, nil)
	body := `{"namespace":"team-b","content":"apiVersion: example.io/v1\nkind: Widget\nmetadata:\n  name: sample\n"}`
	ctx, recorder := newPlatformTestContext(http.MethodPost, "/api/v1/clusters/cluster-a/extensions/crds/widgets.example.io/resources?namespace=team-a", body, gin.Params{{Key: "clusterID", Value: "cluster-a"}, {Key: "crdName", Value: "widgets.example.io"}})

	handler.CreateCRDResource(ctx)

	if recorder.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d, body=%s", recorder.Code, http.StatusCreated, recorder.Body.String())
	}
	if resources.crdCreateNamespace != "team-b" {
		t.Fatalf("namespace = %q, want team-b", resources.crdCreateNamespace)
	}
	if resources.crdCreateContent == "" {
		t.Fatal("content was not forwarded")
	}
}

func TestPlatformInstallHelmChartBindsPayload(t *testing.T) {
	gin.SetMode(gin.TestMode)

	resources := &stubPlatformResourceService{}
	handler := NewPlatformHandler(nil, resources, nil, nil, nil, nil)
	body := `{"packageId":"pkg-1","name":"nginx","version":"1.2.3","namespace":"apps","releaseName":"edge","values":"replicaCount: 2\n"}`
	ctx, recorder := newPlatformTestContext(http.MethodPost, "/api/v1/clusters/cluster-a/helm/charts/install", body, gin.Params{{Key: "clusterID", Value: "cluster-a"}})

	handler.InstallHelmChart(ctx)

	if recorder.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d, body=%s", recorder.Code, http.StatusCreated, recorder.Body.String())
	}
	if resources.helmInstallInput.ReleaseName != "edge" || resources.helmInstallInput.Namespace != "apps" || resources.helmInstallInput.Version != "1.2.3" {
		t.Fatalf("helm input = %#v", resources.helmInstallInput)
	}
}

func TestPlatformHandlerMapsApplicationErrors(t *testing.T) {
	gin.SetMode(gin.TestMode)

	resources := &stubPlatformResourceService{serviceListErr: fmt.Errorf("%w: no access", apperrors.ErrAccessDenied)}
	handler := NewPlatformHandler(nil, resources, nil, nil, nil, nil)
	ctx, recorder := newPlatformTestContext(http.MethodGet, "/api/v1/clusters/cluster-a/network/services", "", gin.Params{{Key: "clusterID", Value: "cluster-a"}})

	handler.ListServices(ctx)

	if recorder.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d, body=%s", recorder.Code, http.StatusForbidden, recorder.Body.String())
	}
	var payload struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if payload.Error.Code != "access_denied" {
		t.Fatalf("error.code = %q, want access_denied", payload.Error.Code)
	}
}
