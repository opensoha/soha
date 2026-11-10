package handlers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainresource "github.com/opensoha/soha/internal/domain/resource"
)

type genericRouteSpy struct {
	GenericResourceService
	method, cluster, namespace, kind, name, content string
}

func (s *genericRouteSpy) GetResourceYAML(_ context.Context, _ domainidentity.Principal, cluster, namespace, kind, name string) (domainresource.ResourceYAMLView, error) {
	s.method, s.cluster, s.namespace, s.kind, s.name = "GET", cluster, namespace, kind, name
	return domainresource.ResourceYAMLView{Kind: kind, Name: name, Namespace: namespace}, nil
}

func (s *genericRouteSpy) ApplyResourceYAMLByKind(ctx context.Context, p domainidentity.Principal, cluster, namespace, kind, name, content string) (domainresource.ResourceYAMLView, error) {
	result, err := s.GetResourceYAML(ctx, p, cluster, namespace, kind, name)
	s.method, s.content = "PUT", content
	return result, err
}

func (s *genericRouteSpy) DeleteResourceByKind(ctx context.Context, p domainidentity.Principal, cluster, namespace, kind, name string) error {
	_, err := s.GetResourceYAML(ctx, p, cluster, namespace, kind, name)
	s.method = "DELETE"
	return err
}

func TestGatewayAndCapacityGenericRoutesReachTheirResourceKind(t *testing.T) {
	for _, resource := range []struct{ path, kind string }{
		{"network/httproutes", "HTTPRoute"}, {"network/grpcroutes", "GRPCRoute"},
		{"network/backendtlspolicies", "BackendTLSPolicy"}, {"network/referencegrants", "ReferenceGrant"},
		{"configuration/hpas", "HorizontalPodAutoscaler"}, {"configuration/poddisruptionbudgets", "PodDisruptionBudget"},
		{"workloads/replicationcontrollers", "ReplicationController"},
	} {
		for _, method := range []string{"GET", "PUT", "DELETE"} {
			if resource.kind == "ReplicationController" && method != "DELETE" {
				continue
			}
			t.Run(resource.kind+"-"+method, func(t *testing.T) {
				spy := &genericRouteSpy{}
				router := gin.New()
				(&genericResourceHandler{service: spy}).RegisterGenericResourceRoutes(router)
				(&workloadInventoryResourceHandler{generic: spy}).RegisterWorkloadDeleteRoutes(router)
				path := "/clusters/prod/" + resource.path + "/sample"
				status := http.StatusNoContent
				if method != "DELETE" {
					path += "/yaml"
					status = http.StatusOK
				}
				req := httptest.NewRequest(method, path+"?namespace=acceptance", strings.NewReader(`{"content":"kind: sample"}`))
				req.Header.Set("Content-Type", "application/json")
				response := httptest.NewRecorder()
				router.ServeHTTP(response, req)
				if response.Code != status || spy.method != method || spy.cluster != "prod" || spy.namespace != "acceptance" || spy.kind != resource.kind || spy.name != "sample" {
					t.Fatalf("status=%d, routed=%+v", response.Code, spy)
				}
				if method == "PUT" && spy.content != "kind: sample" {
					t.Fatal("YAML payload was lost")
				}
			})
		}
	}
}
