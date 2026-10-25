package delivery

import (
	"context"
	"fmt"
	"testing"

	domainapp "github.com/opensoha/soha/internal/domain/application"
	domaincatalog "github.com/opensoha/soha/internal/domain/catalog"
	domaindelivery "github.com/opensoha/soha/internal/domain/delivery"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainresource "github.com/opensoha/soha/internal/domain/resource"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type helmRuntimeTargets struct {
	stubTargetReader
	sources   []domainresource.ResourceSourceSnapshot
	err       error
	requested string
}

func (h *helmRuntimeTargets) GetHelmReleaseWorkloadSources(_ context.Context, _ domainidentity.Principal, cluster, namespace, release string) ([]domainresource.ResourceSourceSnapshot, error) {
	h.requested = cluster + "/" + namespace + "/" + release
	return h.sources, h.err
}
func (*helmRuntimeTargets) ListDeployments(context.Context, domainidentity.Principal, string, string) ([]domainresource.DeploymentView, error) {
	return nil, fmt.Errorf("Helm observation must not guess Deployment names or labels")
}

func helmRuntimeSource(kind, name, uid string) domainresource.ResourceSourceSnapshot {
	return domainresource.ResourceSourceSnapshot{Resource: domainresource.ResourceRef{ClusterID: "cluster", Namespace: "ns", APIVersion: "apps/v1", Kind: kind, Name: name, UID: uid}}
}

func TestHelmRuntimeUsesAuthorizedReleaseInventoryForObserveOnlyTargets(t *testing.T) {
	h := &helmRuntimeTargets{sources: []domainresource.ResourceSourceSnapshot{
		helmRuntimeSource("Deployment", "reviews-v1", "uid1"), helmRuntimeSource("StatefulSet", "database", "uid2"), helmRuntimeSource("DaemonSet", "collector", "uid3"),
	}}
	s := &Service{targets: h}
	binding := domaincatalog.ApplicationEnvironment{ID: "env", Targets: []domaincatalog.ReleaseTarget{{ID: "target", ClusterID: "cluster", Namespace: "ns", WorkloadKind: "HelmRelease", WorkloadName: "reviews", Enabled: false, Metadata: map[string]any{"importSource": "helm", "serviceId": "svc"}}}}
	items, err := s.listRuntimeWorkloadsForBinding(context.Background(), domainidentity.Principal{}, domainapp.App{}, binding, nil, nil, nil, nil, nil)
	if err != nil || len(items) != 3 || h.requested != "cluster/ns/reviews" {
		t.Fatalf("Helm inventory = %#v, %v (%s)", items, err, h.requested)
	}
	for _, item := range items {
		if item.ServiceID != "svc" || item.ApplicationEnvironmentID != "env" || item.Source == nil || item.SourceStatus != "unavailable" {
			t.Fatalf("lost source identity or unknown observation: %#v", item)
		}
		if runtimeWorkloadHealth(item) != runtimeHealthUnknown {
			t.Fatal("missing replica observation became healthy")
		}
	}
}

func TestHelmRuntimeKeepsReadFailuresExplicit(t *testing.T) {
	for _, tc := range []struct {
		name   string
		err    error
		status string
	}{{"forbidden", apperrors.ErrAccessDenied, "forbidden"}, {"missing", apperrors.ErrNotFound, "missing"}, {"failed", apperrors.ErrConflict, "unavailable"}, {"no native workloads", nil, "linked"}} {
		t.Run(tc.name, func(t *testing.T) {
			s := &Service{targets: &helmRuntimeTargets{err: tc.err}}
			items := s.helmRuntimeWorkloads(context.Background(), domainidentity.Principal{}, "env", domaincatalog.ReleaseTarget{WorkloadName: "release"})
			if len(items) != 1 || items[0].SourceStatus != tc.status || runtimeWorkloadHealth(items[0]) != runtimeHealthUnknown {
				t.Fatalf("failure became deployment state: %#v", items)
			}
		})
	}
}

func TestHelmRuntimeCountsOnlyObservedNativeReplicas(t *testing.T) {
	for _, kind := range []string{"Deployment", "StatefulSet", "DaemonSet"} {
		t.Run(kind, func(t *testing.T) {
			source := helmRuntimeSource(kind, "api", "uid")
			source.Replicas = &domainresource.SourceReplicaObservation{Desired: 2, Ready: 2, Updated: 2, Available: 2}
			s := &Service{targets: &helmRuntimeTargets{sources: []domainresource.ResourceSourceSnapshot{source}}}
			items := s.helmRuntimeWorkloads(context.Background(), domainidentity.Principal{}, "env", domaincatalog.ReleaseTarget{ClusterID: "cluster", Namespace: "ns", WorkloadName: "release"})
			if len(items) != 1 || !items[0].NativeObservationAvailable || runtimeWorkloadHealth(items[0]) != runtimeHealthHealthy || items[0].ReadyReplicas != 2 {
				t.Fatalf("valid observation lost: %+v", items)
			}
		})
	}
}

func TestRuntimeSelectionAllowsTwoAssociationsOfTheSameUIDButRejectsAmbiguity(t *testing.T) {
	first := domaindelivery.ApplicationRuntimeWorkload{WorkloadName: "api", ServiceID: "helm", Source: ptrHelmSource(helmRuntimeSource("Deployment", "api", "uid"))}
	second := first
	second.ServiceID = "native"
	second.SourceStatus = "linked"
	copySource := *first.Source
	copySource.Resource.ResourceVersion = "newer"
	second.Source = &copySource
	selected, err := selectRuntimeWorkload([]domaindelivery.ApplicationRuntimeWorkload{first, second}, "api")
	if err != nil || selected.ServiceID != "native" {
		t.Fatalf("same UID association rejected: %#v, %v", selected, err)
	}
	for _, change := range []string{"uid", "kind", "namespace", "cluster"} {
		t.Run(change, func(t *testing.T) {
			other := second
			source := *second.Source
			switch change {
			case "uid":
				source.Resource.UID = "new"
			case "kind":
				source.Resource.Kind = "StatefulSet"
			case "namespace":
				source.Resource.Namespace = "other"
			case "cluster":
				source.Resource.ClusterID = "other"
			}
			other.Source = &source
			if _, err := selectRuntimeWorkload([]domaindelivery.ApplicationRuntimeWorkload{first, other}, "api"); err == nil {
				t.Fatal("different physical identities accepted")
			}
		})
	}
}
func ptrHelmSource(source domainresource.ResourceSourceSnapshot) *domainresource.ResourceSourceSnapshot {
	return &source
}
