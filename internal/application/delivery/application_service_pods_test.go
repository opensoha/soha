package delivery

import (
	"context"
	"errors"
	"sync"
	"testing"

	cr "github.com/opensoha/soha/contracts/resource"
	da "github.com/opensoha/soha/internal/domain/application"
	dc "github.com/opensoha/soha/internal/domain/catalog"
	dd "github.com/opensoha/soha/internal/domain/delivery"
	di "github.com/opensoha/soha/internal/domain/identity"
	dr "github.com/opensoha/soha/internal/domain/resource"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type applicationPodTargetsStub struct {
	stubTargetReader
	mu      sync.Mutex
	sources []dr.ResourceRef
}

func (s *applicationPodTargetsStub) GetDeliveryWorkloadPods(_ context.Context, _ di.Principal, source dr.ResourceSourceSnapshot, _ cr.RelationConfig) ([]dr.PodView, error) {
	ref := source.Resource
	s.mu.Lock()
	s.sources = append(s.sources, ref)
	s.mu.Unlock()
	if ref.Name == "stale" {
		return nil, apperrors.ErrConflict
	}
	return []dr.PodView{{UID: "current-pod", Name: "api-0", Namespace: ref.Namespace}}, nil
}

func TestDeliveryApplicationServicePodsSelectOnlyAssignedTargets(t *testing.T) {
	reader := &applicationPodTargetsStub{}
	target := func(name, id, serviceID string) dc.ReleaseTarget {
		return dc.ReleaseTarget{WorkloadName: name, WorkloadKind: "StatefulSet", ClusterID: "cluster", Namespace: "team", ID: id, ServiceID: serviceID, Enabled: true}
	}
	legacy := target("api", "old-record", "")
	stale := linkedSourceTarget(t, dr.ResourceSourceSnapshot{Resource: dr.ResourceRef{ClusterID: "cluster", Namespace: "team", APIVersion: "apps/v1", Kind: "Deployment", Name: "stale", UID: "pinned-uid"}})
	stale.Metadata["serviceId"] = "svc"
	wrongBinding := target("api", "wrong-binding", "svc")
	wrongBinding.ApplicationEnvironmentID = "other"
	apps := stubApplicationReader{app: da.App{ID: "app"}, services: []da.Service{{ID: "svc", ApplicationID: "app", Key: "api"}}}
	binding := dc.ApplicationEnvironment{ID: "env", ApplicationID: "app", Targets: []dc.ReleaseTarget{
		target("api", "explicit", "svc"), legacy, target("api", "other-service", "other"), stale,
		target("same-label-workload", "label-only", ""), target("", "namespace-selector", ""), wrongBinding,
	}}
	s := &Service{applications: apps, catalog: stubCatalogReader{bindings: []dc.ApplicationEnvironment{binding}}, targets: reader}
	result, err := s.GetApplicationServicePods(t.Context(), di.Principal{}, "app", "env", "svc")
	if err != nil || result.Status != "partial" || result.Reason != "identity_mismatch" || len(result.Items) != 1 || result.Items[0].TargetID != "explicit" {
		t.Fatalf("projection=%+v, err=%v", result, err)
	}
	if len(reader.sources) != 2 {
		t.Fatalf("duplicate identities or out-of-scope targets queried: %+v", reader.sources)
	}
	for _, source := range reader.sources {
		if source.Name == "stale" && source.UID != "pinned-uid" {
			t.Fatalf("lost import UID: %+v", source)
		}
	}
}

type applicationPodsReferenceStub struct {
	stubRepository
	ServiceReferenceActiveRepository
}

func (applicationPodsReferenceStub) GetServiceReference(context.Context, string, string) (dd.ServiceReferenceRecord, error) {
	return dd.ServiceReferenceRecord{ApplicationID: "app", ConsumerBindingID: "local", ServiceID: "svc", SourceBindingID: "private-origin"}, nil
}

func TestDeliveryApplicationServicePodsScopeBeforeReference(t *testing.T) {
	reader := &applicationPodTargetsStub{}
	apps := stubApplicationReader{app: da.App{ID: "app"}, services: []da.Service{{ID: "svc", ApplicationID: "app"}}, getErrByID: map[string]error{"app-denied": apperrors.ErrAccessDenied}}
	catalog := stubCatalogReader{bindings: []dc.ApplicationEnvironment{{ID: "local", ApplicationID: "app"}, {ID: "foreign", ApplicationID: "other"}}, getErrByBindingID: map[string]error{"binding-denied": apperrors.ErrAccessDenied}}
	s := &Service{applications: apps, catalog: catalog, targets: reader, repository: applicationPodsReferenceStub{}}
	for _, test := range []struct {
		app, binding, service string
		err                   error
	}{
		{"app-denied", "local", "svc", apperrors.ErrAccessDenied},
		{"app", "binding-denied", "svc", apperrors.ErrAccessDenied},
		{"app", "foreign", "svc", apperrors.ErrNotFound},
		{"app", "local", "foreign-service", apperrors.ErrNotFound},
	} {
		if got, err := s.GetApplicationServicePods(t.Context(), di.Principal{}, test.app, test.binding, test.service); !errors.Is(err, test.err) || got.Status != "" {
			t.Fatalf("scope(%+v) = %+v, %v", test, got, err)
		}
	}
	got, err := s.GetApplicationServicePods(t.Context(), di.Principal{}, "app", "local", "svc")
	if err != nil || got.Status != "relation_only" || len(got.Items) != 0 || len(reader.sources) != 0 {
		t.Fatalf("shared reference started a provider read: %+v, %v, sources=%v", got, err, reader.sources)
	}
}
