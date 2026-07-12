package handlers

import (
	"context"
	"net/http"
	"strings"
	"testing"

	appresource "github.com/opensoha/soha/internal/application/resource"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainresource "github.com/opensoha/soha/internal/domain/resource"
)

type stubWorkloadCapability struct {
	*appresource.Workloads
	called bool
}

func (s *stubWorkloadCapability) ListPods(
	_ context.Context,
	_ domainidentity.Principal,
	_ string,
	_ string,
) ([]domainresource.PodView, error) {
	s.called = true
	return []domainresource.PodView{{Name: "api-0", Phase: "Running"}}, nil
}

func TestNewPlatformHandlerWithResourcesAcceptsCapabilityDependency(t *testing.T) {
	t.Parallel()
	base := newStubPlatformResourceService()
	workloads := &stubWorkloadCapability{Workloads: base.Workloads}
	resources := completeResourceServices(base)
	resources.PodReader = workloads
	handler, err := NewPlatformHandlerWithResources(PlatformDependencies{
		Clusters: &stubPlatformClusterService{}, Resources: resources,
		Audit: &stubPlatformAuditService{}, Events: &stubPlatformEventService{},
		Operations: &stubPlatformOperationService{}, Integration: stubPlatformIntegrationService{},
	})
	if err != nil {
		t.Fatalf("NewPlatformHandlerWithResources() error = %v", err)
	}
	ctx, recorder := newPlatformTestContext(
		http.MethodGet,
		"/clusters/c-1/workloads/pods?namespace=default",
		"",
		nil,
	)

	handler.ListPods(ctx)

	if !workloads.called {
		t.Fatal("workload capability was not called")
	}
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}
}

func TestNewPlatformHandlerWithResourcesRejectsMissingCapability(t *testing.T) {
	t.Parallel()

	_, err := NewPlatformHandlerWithResources(PlatformDependencies{})
	if err == nil {
		t.Fatal("NewPlatformHandlerWithResources() error = nil, want missing dependency error")
	}
}

func TestNewPlatformHandlerWithResourcesRejectsTypedNilDependency(t *testing.T) {
	t.Parallel()

	resources := completeResourceServices(newStubPlatformResourceService())
	var clusters *stubPlatformClusterService
	_, err := NewPlatformHandlerWithResources(PlatformDependencies{
		Clusters: clusters, Resources: resources,
		Audit: &stubPlatformAuditService{}, Events: &stubPlatformEventService{},
		Operations: &stubPlatformOperationService{}, Integration: stubPlatformIntegrationService{},
	})
	if err == nil || !strings.Contains(err.Error(), "clusters dependency is required") {
		t.Fatalf("NewPlatformHandlerWithResources() error = %v, want typed nil clusters error", err)
	}
}
