package aigateway

import (
	"context"
	"errors"
	"testing"

	appaccess "github.com/opensoha/soha/internal/application/access"
	domainaigateway "github.com/opensoha/soha/internal/domain/aigateway"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainresource "github.com/opensoha/soha/internal/domain/resource"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type diagnosticReadStub struct {
	fakeResourceService
	called        string
	crdName       string
	resourceName  string
	minutes, step int
}

func TestNamespaceReadToolHonorsPinnedNamespace(t *testing.T) {
	resources := &fakeResourceService{namespaces: []domainresource.NamespaceView{{Name: "prod"}, {Name: "private"}}}
	service := &Service{resources: resources}
	for _, namespace := range []string{"prod", "missing"} {
		output, related, err := service.invokeKubernetesWorkbenchReadTool(context.Background(), domainidentity.Principal{}, "k8s.namespaces.list", kubernetesToolRequest{ClusterID: "cluster", Namespace: namespace}, map[string]any{})
		if err != nil {
			t.Fatal(err)
		}
		items, ok := output.([]domainresource.NamespaceView)
		if !ok {
			t.Fatalf("unexpected namespace output: %T", output)
		}
		for _, item := range items {
			if item.Name != namespace {
				t.Fatal("query escaped its namespace scope")
			}
		}
		if related["count"] != len(items) || (namespace == "prod" && len(items) != 1) || (namespace == "missing" && len(items) != 0) {
			t.Fatalf("incorrect namespace result: %v, %v", items, related)
		}
	}
}

func (s *diagnosticReadStub) ListCRDs(_ context.Context, _ domainidentity.Principal, cluster string) ([]domainresource.CRDView, error) {
	s.called, s.clusterID = "crds", cluster
	return []domainresource.CRDView{{Name: "widgets.example.com"}}, nil
}

func (s *diagnosticReadStub) ListCRDResources(_ context.Context, _ domainidentity.Principal, cluster, crd, namespace string) ([]domainresource.CustomResourceView, error) {
	s.called, s.clusterID, s.crdName, s.namespace = "custom", cluster, crd, namespace
	return []domainresource.CustomResourceView{{Name: "widget", Namespace: namespace}}, nil
}

func (s *diagnosticReadStub) GetPodMetrics(_ context.Context, _ domainidentity.Principal, cluster, namespace, name string, minutes, step int) (domainresource.PodMetricsView, error) {
	s.called, s.clusterID, s.namespace, s.resourceName, s.minutes, s.step = "pod", cluster, namespace, name, minutes, step
	return domainresource.PodMetricsView{PodName: name, Namespace: namespace, RangeMinutes: minutes, StepSeconds: step}, nil
}

func (s *diagnosticReadStub) GetDeploymentMetrics(_ context.Context, _ domainidentity.Principal, cluster, namespace, name string, minutes, step int) (domainresource.ResourceMetricsView, error) {
	s.called, s.clusterID, s.namespace, s.resourceName, s.minutes, s.step = "deployment", cluster, namespace, name, minutes, step
	return domainresource.ResourceMetricsView{ResourceKind: "Deployment", ResourceName: name, Namespace: namespace, RangeMinutes: minutes, StepSeconds: step}, nil
}

func TestKubernetesDiagnosticReadToolsUseScopedResourceService(t *testing.T) {
	for _, tc := range []struct {
		tool, called, permission string
		input                    map[string]any
	}{
		{"k8s.crds.list", "crds", appaccess.PermPlatformExtensionsView, map[string]any{"clusterId": "cluster-a"}},
		{"k8s.custom_resources.list", "custom", appaccess.PermPlatformExtensionsView, map[string]any{"clusterId": "cluster-a", "crdName": "widgets.example.com", "namespace": "prod"}},
		{"k8s.pods.metrics", "pod", appaccess.PermPlatformPodsView, map[string]any{"clusterId": "cluster-a", "namespace": "prod", "podName": "api"}},
		{"k8s.deployments.metrics", "deployment", appaccess.PermPlatformDeploymentView, map[string]any{"clusterId": "cluster-a", "namespace": "prod", "deploymentName": "api", "rangeMinutes": 30, "stepSeconds": 120}},
	} {
		t.Run(tc.tool, func(t *testing.T) {
			resources := &diagnosticReadStub{}
			permissions := []string{appaccess.PermAIGatewayInvoke, appaccess.PermWorkspaceResourceView, tc.permission}
			service := newTestService(appaccess.NewPermissionResolver(stubRolePermissionReader{matrix: map[string][]string{"sre": permissions}}), nil)
			service.SetResourceService(resources)
			result := invokeKubernetesDiagnostic(t, service, testPrincipal("sre"), tc.tool, tc.input)
			if resources.called != tc.called || resources.clusterID != "cluster-a" || result.RelatedIDs["clusterId"] != "cluster-a" {
				t.Fatalf("lost tool routing or cluster: %+v, %+v", resources, result.RelatedIDs)
			}
			if tc.called != "crds" && resources.namespace != "prod" {
				t.Fatal("lost namespace")
			}
			if tc.called == "custom" && resources.crdName != "widgets.example.com" {
				t.Fatal("lost CRD name")
			}
			if tc.called == "pod" && (resources.resourceName != "api" || resources.minutes != 15 || resources.step != 60) {
				t.Fatal("lost Pod scope or metric defaults")
			}
			if tc.called == "deployment" && (resources.resourceName != "api" || resources.minutes != 30 || resources.step != 120) {
				t.Fatal("lost Deployment scope or metric bounds")
			}
			denied := newTestService(appaccess.NewPermissionResolver(stubRolePermissionReader{matrix: map[string][]string{"sre": {appaccess.PermAIGatewayInvoke, appaccess.PermWorkspaceResourceView}}}), nil)
			resources.called = ""
			denied.SetResourceService(resources)
			_, err := denied.InvokeTool(context.Background(), testPrincipal("sre"), domainaigateway.ToolInvocationRequest{ToolName: tc.tool, Input: tc.input})
			if err == nil || resources.called != "" {
				t.Fatal("missing resource permission reached owning service")
			}
		})
	}
}

func TestKubernetesDiagnosticReadRejectsInvalidMetricRange(t *testing.T) {
	resources := &diagnosticReadStub{}
	service := &Service{resources: resources}
	for _, req := range []kubernetesToolRequest{
		{Namespace: "prod", PodName: "api", RangeMinutes: 1441},
		{Namespace: "prod", PodName: "api", StepSeconds: 3601},
		{Namespace: "prod", PodName: "api", RangeMinutes: -1},
		{PodName: "api"},
		{Namespace: "prod"},
	} {
		_, _, err := service.invokeKubernetesDiagnosticReadTool(context.Background(), domainidentity.Principal{}, "k8s.pods.metrics", req, map[string]any{})
		if !errors.Is(err, apperrors.ErrInvalidArgument) || resources.called != "" {
			t.Fatalf("invalid input reached metrics service: %+v, %v", req, err)
		}
	}
}
