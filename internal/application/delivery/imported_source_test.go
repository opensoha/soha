package delivery

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	domaincatalog "github.com/opensoha/soha/internal/domain/catalog"
	domaindelivery "github.com/opensoha/soha/internal/domain/delivery"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainresource "github.com/opensoha/soha/internal/domain/resource"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

func linkedSourceTarget(t *testing.T, source domainresource.ResourceSourceSnapshot) domaincatalog.ReleaseTarget {
	t.Helper()
	raw, err := json.Marshal(source)
	if err != nil {
		t.Fatal(err)
	}
	return domaincatalog.ReleaseTarget{ID: "target", ClusterID: source.Resource.ClusterID, Namespace: source.Resource.Namespace, WorkloadKind: "Deployment", WorkloadName: source.Resource.Name, Metadata: map[string]any{"importSource": "kubernetes", "resourceSource": string(raw), "serviceId": "service"}}
}

func TestImportedRuntimeSeparatesRootIdentityFromHealth(t *testing.T) {
	for _, test := range []struct {
		name              string
		kind, uid, digest string
		denied            error
		counts            *domainresource.SourceReplicaObservation
		status, health    string
	}{
		{name: "CR unknown", kind: "CustomResource", status: "linked", health: "unknown"},
		{name: "native ready", counts: &domainresource.SourceReplicaObservation{Desired: 2, Ready: 2, Updated: 2, Available: 2}, status: "linked", health: "healthy"},
		{name: "native stale health", status: "linked", health: "unknown"},
		{name: "missing", denied: apperrors.ErrNotFound, status: "missing", health: "unknown"},
		{name: "forbidden", denied: apperrors.ErrAccessDenied, status: "forbidden", health: "unknown"},
		{name: "replacement", uid: "new-uid", status: "replaced", health: "unknown"},
		{name: "changed", digest: "other", status: "changed", health: "unknown"},
	} {
		t.Run(test.name, func(t *testing.T) {
			source := identityImportSource("Deployment")
			target := linkedSourceTarget(t, source)
			if test.kind != "" {
				target.WorkloadKind = test.kind
				source.CRDName = "widgets.example.com"
				source.Resource.Kind = "Widget"
				source.Resource.APIVersion = "example.com/v1"
				raw, _ := json.Marshal(source)
				target.Metadata["resourceSource"] = string(raw)
			}
			current := source
			current.Replicas = test.counts
			if test.uid != "" {
				current.Resource.UID = test.uid
			}
			if test.digest != "" {
				current.SpecDigest = test.digest
			}
			service := identityImportService(stubApplicationReader{}, stubCatalogReader{}, stubTargetReader{importSources: map[string]domainresource.ResourceSourceSnapshot{target.WorkloadKind + "/api": current}, importSourceErr: test.denied}, stubRepository{})
			item := service.importedRuntimeWorkload(context.Background(), deliveryActionPrincipal(), "binding", target)
			if item.SourceStatus != test.status || runtimeWorkloadHealth(item) != test.health {
				t.Fatalf("runtime=%+v health=%s", item, runtimeWorkloadHealth(item))
			}
		})
	}
}

func TestImportedRuntimeRejectsAmbiguousNamesAndReplacement(t *testing.T) {
	items := []domaindelivery.ApplicationRuntimeWorkload{{WorkloadName: "api", Namespace: "one"}, {WorkloadName: "api", Namespace: "two"}}
	if _, err := selectRuntimeWorkload(items, "api"); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatalf("ambiguous=%v", err)
	}
	source := identityImportSource("Deployment")
	replacement := source
	replacement.Resource.UID = "replacement"
	service := identityImportService(stubApplicationReader{}, stubCatalogReader{}, stubTargetReader{importSources: map[string]domainresource.ResourceSourceSnapshot{"Deployment/api": replacement}}, stubRepository{})
	err := service.verifyImportedRuntimeIdentity(context.Background(), deliveryActionPrincipal(), domaindelivery.ApplicationRuntimeWorkload{ClusterID: "cluster", Namespace: "team", WorkloadKind: "Deployment", WorkloadName: "api", Source: &source})
	if !errors.Is(err, apperrors.ErrConflict) {
		t.Fatalf("post-read identity=%v", err)
	}
}

type sourceViewTargetStub struct {
	stubTargetReader
	source *domainresource.ResourceSourceSnapshot
	calls  *int
}

func (s sourceViewTargetStub) GetApplicationSourceYAML(_ context.Context, _ domainidentity.Principal, source domainresource.ResourceSourceSnapshot) (domainresource.ResourceYAMLView, error) {
	*s.source = source
	*s.calls++
	return domainresource.ResourceYAMLView{Content: "kind: Deployment"}, nil
}

func TestImportedSourceReadUsesAuthorizedBindingTarget(t *testing.T) {
	source := identityImportSource("Deployment")
	target := linkedSourceTarget(t, source)
	var captured domainresource.ResourceSourceSnapshot
	calls := 0
	reader := sourceViewTargetStub{source: &captured, calls: &calls}
	service := New(stubApplicationReader{}, stubCatalogReader{bindings: []domaincatalog.ApplicationEnvironment{{ID: "binding", ApplicationID: "app", Targets: []domaincatalog.ReleaseTarget{target}}}}, nil, nil, nil, stubRepository{}, nil, reader, nil)
	if _, err := service.GetImportedTargetSource(context.Background(), deliveryActionPrincipal(), "binding", "foreign-target"); !errors.Is(err, apperrors.ErrNotFound) || calls != 0 {
		t.Fatalf("foreign target=%v calls=%d", err, calls)
	}
	if _, err := service.GetImportedTargetSource(context.Background(), deliveryActionPrincipal(), "binding", "target"); err != nil || captured.Resource.UID != "uid-1" || calls != 1 {
		t.Fatalf("source=%+v err=%v", captured, err)
	}
	target.Namespace = "forged"
	if _, err := importedTargetSource(target); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatalf("forged scope=%v", err)
	}
}
