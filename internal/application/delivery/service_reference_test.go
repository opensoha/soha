package delivery

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	appaccess "github.com/opensoha/soha/internal/application/access"
	domainapp "github.com/opensoha/soha/internal/domain/application"
	domaincatalog "github.com/opensoha/soha/internal/domain/catalog"
	domaindelivery "github.com/opensoha/soha/internal/domain/delivery"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainresource "github.com/opensoha/soha/internal/domain/resource"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type referenceCatalogStub struct {
	CatalogReader
	bindings               map[string]domaincatalog.ApplicationEnvironment
	environments           []domaincatalog.Environment
	denied                 map[string]error
	updateErr              error
	updatePermission       string
	sourceReads            int
	revokeAfterObservation bool
}

func (c *referenceCatalogStub) GetApplicationEnvironment(_ context.Context, _ domainidentity.Principal, id string) (domaincatalog.ApplicationEnvironment, error) {
	if err := c.denied[id]; err != nil {
		return domaincatalog.ApplicationEnvironment{}, err
	}
	if id == "test-b" {
		c.sourceReads++
		if c.revokeAfterObservation && c.sourceReads > 1 {
			return domaincatalog.ApplicationEnvironment{}, apperrors.ErrAccessDenied
		}
	}
	item, ok := c.bindings[id]
	if !ok {
		return item, apperrors.ErrNotFound
	}
	return item, nil
}
func (c *referenceCatalogStub) AuthorizeApplicationEnvironmentPermission(ctx context.Context, p domainidentity.Principal, id, permission string) (domaincatalog.ApplicationEnvironment, error) {
	c.updatePermission = permission
	if c.updateErr != nil {
		return domaincatalog.ApplicationEnvironment{}, c.updateErr
	}
	return c.GetApplicationEnvironment(ctx, p, id)
}
func (c *referenceCatalogStub) ListEnvironments(context.Context, domainidentity.Principal) ([]domaincatalog.Environment, error) {
	return c.environments, nil
}

type referenceTargetsStub struct {
	stubTargetReader
	mu          sync.Mutex
	sources     map[string]domainresource.ResourceSourceSnapshot
	readErr     error
	reads       []string
	afterRead   func()
	once        sync.Once
	detail      domainresource.HelmReleaseDetailView
	helmSources []domainresource.ResourceSourceSnapshot
	helmReads   int
	helmChanged bool
}

func (r *referenceTargetsStub) GetImportSource(ctx context.Context, _ domainidentity.Principal, cluster, namespace, kind, name, crd string) (domainresource.ResourceSourceSnapshot, error) {
	r.mu.Lock()
	key := cluster + "/" + namespace + "/" + kind + "/" + name + "/" + crd
	r.reads = append(r.reads, key)
	item, ok := r.sources[key]
	r.mu.Unlock()
	if _, ok := ctx.Deadline(); !ok {
		return item, errors.New("source query lacks bounded lifetime")
	}
	if err := ctx.Err(); err != nil {
		return item, err
	}
	if r.afterRead != nil {
		r.once.Do(r.afterRead)
	}
	if r.readErr != nil {
		return item, r.readErr
	}
	if !ok {
		return item, apperrors.ErrNotFound
	}
	return item, nil
}
func (r *referenceTargetsStub) GetHelmReleaseDetail(context.Context, domainidentity.Principal, string, string, string) (domainresource.HelmReleaseDetailView, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.helmReads++
	if r.readErr != nil {
		return domainresource.HelmReleaseDetailView{}, r.readErr
	}
	item := r.detail
	if r.helmChanged && r.helmReads > 1 {
		item.Revision = "8"
	}
	return item, nil
}
func (r *referenceTargetsStub) GetHelmReleaseWorkloadSources(context.Context, domainidentity.Principal, string, string, string) ([]domainresource.ResourceSourceSnapshot, error) {
	return r.helmSources, r.readErr
}

type referenceFixture struct {
	service *Service
	catalog *referenceCatalogStub
	targets *referenceTargetsStub
	input   domaindelivery.ServiceReferencePreviewInput
}

func newReferenceFixture(t *testing.T) referenceFixture {
	t.Helper()
	now := time.Date(2026, 10, 26, 8, 0, 0, 123000, time.UTC)
	view := domaincatalog.ApplicationEnvironment{ID: "test-a", ApplicationID: "app", EnvironmentID: "env-a", UpdatedAt: now, Targets: []domaincatalog.ReleaseTarget{{ID: "local-a", ServiceID: "svc", Metadata: map[string]any{"sensitiveConfig": "synthetic-local"}}}}
	root := domainresource.ResourceSourceSnapshot{Resource: domainresource.ResourceRef{ClusterID: "cluster-b", Namespace: "backend", Kind: "Deployment", APIVersion: "apps/v1", Name: "api", UID: "uid-api", ScopeMode: domainresource.ResourceScopeModeNamespace, ResourceVersion: "12"}, SpecDigest: strings.Repeat("a", 64), ObservedAt: now.Format(time.RFC3339Nano)}
	raw, err := json.Marshal(root)
	if err != nil {
		t.Fatal(err)
	}
	target := domaincatalog.ReleaseTarget{ID: "target-b", ServiceID: "svc", ApplicationEnvironmentID: "test-b", ClusterID: "cluster-b", Namespace: "backend", WorkloadKind: "Deployment", WorkloadName: "api", UpdatedAt: now, Enabled: false, Metadata: map[string]any{"serviceId": "svc", "ownershipMode": "observe_only", "importSource": "kubernetes", "resourceSource": string(raw), "secret": "synthetic-sensitive-metadata"}}
	source := domaincatalog.ApplicationEnvironment{ID: "test-b", ApplicationID: "app", EnvironmentID: "env-b", UpdatedAt: now, Targets: []domaincatalog.ReleaseTarget{target}}
	catalog := &referenceCatalogStub{bindings: map[string]domaincatalog.ApplicationEnvironment{"test-a": view, "test-b": source}, denied: map[string]error{}, environments: []domaincatalog.Environment{{ID: "env-a", Key: "test-a", Tier: "test", StageLevel: 20, Enabled: true, UpdatedAt: now}, {ID: "env-b", Key: "test-b", Tier: "test", StageLevel: 20, Enabled: true, UpdatedAt: now}}}
	targets := &referenceTargetsStub{sources: map[string]domainresource.ResourceSourceSnapshot{"cluster-b/backend/Deployment/api/": root}}
	apps := stubApplicationReader{app: domainapp.App{ID: "app"}, services: []domainapp.Service{{ID: "svc", ApplicationID: "app", Enabled: true, UpdatedAt: now}}}
	s := &Service{applications: apps, catalog: catalog, targets: targets, permissions: deliveryActionPermissions(appaccess.PermDeliveryApplicationEnvView)}
	return referenceFixture{s, catalog, targets, domaindelivery.ServiceReferencePreviewInput{ServiceID: "svc", SourceApplicationEnvironmentID: "test-b", ExpectedConsumerUpdatedAt: now, ExpectedSourceUpdatedAt: now}}
}

func referenceApplicationsFixture(t *testing.T, f *referenceFixture) stubApplicationReader {
	t.Helper()
	apps, ok := f.service.applications.(stubApplicationReader)
	if !ok {
		t.Fatalf("unexpected application fixture %T", f.service.applications)
	}
	return apps
}

func (f referenceFixture) preview(ctx context.Context) (domaindelivery.ServiceReferencePreview, error) {
	return f.service.PreviewEnvironmentServiceReference(ctx, deliveryActionPrincipal(), "test-a", f.input)
}

func TestServiceReferencePreviewPreservesObserveOnlyAndExcludesSensitiveConfiguration(t *testing.T) {
	f := newReferenceFixture(t)
	before, err := json.Marshal(f.catalog.bindings)
	if err != nil {
		t.Fatal(err)
	}
	item, err := f.preview(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if item.Consumer.ApplicationEnvironmentID != "test-a" || item.Source.ApplicationEnvironmentID != "test-b" || item.ServiceID != "svc" || !slices.Equal(item.CurrentLocalTargetIDs, []string{"local-a"}) || len(item.Targets) != 1 || len(item.Targets[0].Resources) != 1 || item.Targets[0].OwnershipMode != "observe_only" || !validReferenceDigest(item.SourceRevision) {
		t.Fatalf("unexpected preview: %+v", item)
	}
	if f.catalog.updatePermission != "delivery.application-environments.update" {
		t.Fatalf("wrong consumer permission: %s", f.catalog.updatePermission)
	}
	raw, err := json.Marshal(item)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"synthetic-sensitive", "synthetic-local", "resourceVersion", "resourceSource", "sensitiveConfig"} {
		if strings.Contains(string(raw), forbidden) {
			t.Fatalf("preview exposed %s", forbidden)
		}
	}
	after, err := json.Marshal(f.catalog.bindings)
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Fatal("preview changed existing associations or control")
	}
}

func TestServiceReferencePreviewRejectsInvalidOrUnauthorizedSourceBeforeLiveReads(t *testing.T) {
	cases := []struct {
		name   string
		change func(*referenceFixture)
		want   error
	}{
		{"self reference", func(f *referenceFixture) { f.input.SourceApplicationEnvironmentID = "test-a" }, apperrors.ErrInvalidArgument},
		{"missing source revision", func(f *referenceFixture) { f.input.ExpectedSourceUpdatedAt = time.Time{} }, apperrors.ErrInvalidArgument},
		{"empty explicit targets", func(f *referenceFixture) { f.input.SourceTargetIDs = []string{} }, apperrors.ErrInvalidArgument},
		{"duplicate target selection", func(f *referenceFixture) { f.input.SourceTargetIDs = []string{"target-b", "target-b"} }, apperrors.ErrInvalidArgument},
		{"stale consumer", func(f *referenceFixture) {
			f.input.ExpectedConsumerUpdatedAt = f.input.ExpectedConsumerUpdatedAt.Add(time.Second)
		}, apperrors.ErrConflict},
		{"stale source", func(f *referenceFixture) {
			f.input.ExpectedSourceUpdatedAt = f.input.ExpectedSourceUpdatedAt.Add(time.Second)
		}, apperrors.ErrConflict},
		{"no view permission", func(f *referenceFixture) { f.service.permissions = deliveryActionPermissions() }, apperrors.ErrAccessDenied},
		{"consumer view denied", func(f *referenceFixture) { f.catalog.denied["test-a"] = apperrors.ErrAccessDenied }, apperrors.ErrAccessDenied},
		{"consumer update denied", func(f *referenceFixture) { f.catalog.updateErr = apperrors.ErrAccessDenied }, apperrors.ErrAccessDenied},
		{"source view denied", func(f *referenceFixture) { f.catalog.denied["test-b"] = apperrors.ErrAccessDenied }, apperrors.ErrAccessDenied},
		{"application denied", func(f *referenceFixture) {
			a := referenceApplicationsFixture(t, f)
			a.getErrByID = map[string]error{"app": apperrors.ErrAccessDenied}
			f.service.applications = a
		}, apperrors.ErrAccessDenied},
		{"service update denied", func(f *referenceFixture) {
			a := referenceApplicationsFixture(t, f)
			a.importScopeErr = apperrors.ErrAccessDenied
			f.service.applications = a
		}, apperrors.ErrAccessDenied},
		{"disabled service", func(f *referenceFixture) {
			a := referenceApplicationsFixture(t, f)
			a.services[0].Enabled = false
			f.service.applications = a
		}, apperrors.ErrConflict},
		{"cross application", func(f *referenceFixture) {
			b := f.catalog.bindings["test-b"]
			b.ApplicationID = "other-app"
			f.catalog.bindings["test-b"] = b
		}, apperrors.ErrConflict},
		{"cross tier", func(f *referenceFixture) { f.catalog.environments[1].Tier = "pre" }, apperrors.ErrConflict},
		{"missing classification", func(f *referenceFixture) { f.catalog.environments[1].Tier = "" }, apperrors.ErrConflict},
		{"contradictory classification", func(f *referenceFixture) { f.catalog.environments[1].IsProduction = true }, apperrors.ErrConflict},
		{"cross stage", func(f *referenceFixture) { f.catalog.environments[1].StageLevel = 30 }, apperrors.ErrConflict},
		{"disabled environment", func(f *referenceFixture) { f.catalog.environments[1].Enabled = false }, apperrors.ErrConflict},
		{"no local source group", func(f *referenceFixture) {
			b := f.catalog.bindings["test-b"]
			b.Targets = nil
			f.catalog.bindings["test-b"] = b
		}, apperrors.ErrConflict},
		{"foreign selected target", func(f *referenceFixture) { f.input.SourceTargetIDs = []string{"different-target"} }, apperrors.ErrConflict},
		{"partial group", func(f *referenceFixture) {
			b := f.catalog.bindings["test-b"]
			other := b.Targets[0]
			other.ID = "second-root"
			b.Targets = append(b.Targets, other)
			f.catalog.bindings["test-b"] = b
			f.input.SourceTargetIDs = []string{"target-b"}
		}, apperrors.ErrConflict},
		{"unknown control", func(f *referenceFixture) { f.catalog.bindings["test-b"].Targets[0].Metadata["ownershipMode"] = "" }, apperrors.ErrConflict},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newReferenceFixture(t)
			tc.change(&f)
			item, err := f.preview(context.Background())
			if !errors.Is(err, tc.want) || item.ApplicationID != "" || len(f.targets.reads) != 0 {
				t.Fatalf("preview=%+v error=%v reads=%v", item, err, f.targets.reads)
			}
		})
	}
}

func TestServiceReferencePreviewRevalidatesUIDAndSourcePermission(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*referenceFixture)
		want   error
	}{
		{"recreated UID", func(f *referenceFixture) {
			r := f.targets.sources["cluster-b/backend/Deployment/api/"]
			r.Resource.UID = "replacement"
			f.targets.sources["cluster-b/backend/Deployment/api/"] = r
		}, apperrors.ErrConflict},
		{"other namespace", func(f *referenceFixture) {
			r := f.targets.sources["cluster-b/backend/Deployment/api/"]
			r.Resource.Namespace = "other"
			f.targets.sources["cluster-b/backend/Deployment/api/"] = r
		}, apperrors.ErrConflict},
		{"resource permission denied", func(f *referenceFixture) { f.targets.readErr = apperrors.ErrAccessDenied }, apperrors.ErrAccessDenied},
		{"source removed", func(f *referenceFixture) { f.targets.readErr = apperrors.ErrNotFound }, apperrors.ErrNotFound},
		{"revoked during observation", func(f *referenceFixture) { f.catalog.revokeAfterObservation = true }, apperrors.ErrAccessDenied},
		{"binding changed during observation", func(f *referenceFixture) {
			f.targets.afterRead = func() {
				b := f.catalog.bindings["test-b"]
				b.UpdatedAt = b.UpdatedAt.Add(time.Second)
				f.catalog.bindings["test-b"] = b
			}
		}, apperrors.ErrConflict},
		{"control changed without timestamp", func(f *referenceFixture) {
			f.targets.afterRead = func() { f.catalog.bindings["test-b"].Targets[0].Metadata["ownershipMode"] = "managed" }
		}, apperrors.ErrConflict},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newReferenceFixture(t)
			tc.change(&f)
			item, err := f.preview(context.Background())
			if !errors.Is(err, tc.want) || item.ApplicationID != "" {
				t.Fatalf("preview=%+v error=%v", item, err)
			}
		})
	}
}

func TestServiceReferencePreviewRevisionExcludesObservationChurnAndIncludesSourceChanges(t *testing.T) {
	f := newReferenceFixture(t)
	first, err := f.preview(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	r := f.targets.sources["cluster-b/backend/Deployment/api/"]
	r.Resource.ResourceVersion = "99"
	r.ObservedAt = "later"
	r.Replicas = &domainresource.SourceReplicaObservation{Ready: 5}
	f.targets.sources["cluster-b/backend/Deployment/api/"] = r
	f.input.ExpectedSourceRevision = first.SourceRevision
	second, err := f.preview(context.Background())
	if err != nil || second.SourceRevision != first.SourceRevision {
		t.Fatalf("status churn invalidated config: %v", err)
	}
	r.SpecDigest = strings.Repeat("b", 64)
	f.targets.sources["cluster-b/backend/Deployment/api/"] = r
	if item, err := f.preview(context.Background()); !errors.Is(err, apperrors.ErrConflict) || item.ApplicationID != "" {
		t.Fatalf("stale source revision accepted: %+v, %v", item, err)
	}
}

func TestServiceReferencePreviewFencesServiceVersionEvenWhenTimestampIsUnchanged(t *testing.T) {
	f := newReferenceFixture(t)
	first, err := f.preview(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	apps := referenceApplicationsFixture(t, &f)
	apps.services[0].Version++
	f.service.applications = apps
	f.input.ExpectedSourceRevision = first.SourceRevision
	if item, err := f.preview(context.Background()); !errors.Is(err, apperrors.ErrConflict) || item.ApplicationID != "" {
		t.Fatalf("service version change was ignored: %+v, %v", item, err)
	}
}

func TestServiceReferencePreviewSupportsUnknownOperatorWithoutAdapter(t *testing.T) {
	f := newReferenceFixture(t)
	b := f.catalog.bindings["test-b"]
	target := &b.Targets[0]
	target.WorkloadKind = "CustomResource"
	target.WorkloadName = "db"
	root := domainresource.ResourceSourceSnapshot{Resource: domainresource.ResourceRef{ClusterID: target.ClusterID, Namespace: target.Namespace, APIVersion: "data.example.com/v1", Kind: "UnlistedDatabase", Name: "db", UID: "generic-uid", ScopeMode: domainresource.ResourceScopeModeNamespace}, CRDName: "unlisteddatabases.data.example.com", Plural: "unlisteddatabases", SpecDigest: strings.Repeat("c", 64), Generation: 2}
	raw, err := json.Marshal(root)
	if err != nil {
		t.Fatal(err)
	}
	target.Metadata["resourceSource"] = string(raw)
	f.catalog.bindings["test-b"] = b
	f.targets.sources = map[string]domainresource.ResourceSourceSnapshot{"cluster-b/backend/CustomResource/db/unlisteddatabases.data.example.com": root}
	item, err := f.preview(context.Background())
	if err != nil || item.Targets[0].Resources[0].Resource.Kind != "UnlistedDatabase" {
		t.Fatalf("generic CR preview failed: %+v, %v", item, err)
	}
}

func TestServiceReferencePreviewHelmUsesWholeRootAndFencesRevision(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*referenceFixture)
		want   error
	}{
		{"complete release", func(*referenceFixture) {}, nil},
		{"Helm revision changed", func(f *referenceFixture) { f.targets.helmChanged = true }, apperrors.ErrConflict},
		{"Helm forbidden", func(f *referenceFixture) { f.targets.readErr = apperrors.ErrAccessDenied }, apperrors.ErrAccessDenied},
		{"Helm not deployed", func(f *referenceFixture) { f.targets.detail.Status = "failed" }, apperrors.ErrConflict},
		{"Helm projection wrong cluster", func(f *referenceFixture) { f.targets.helmSources[0].Resource.ClusterID = "other" }, apperrors.ErrConflict},
		{"Helm child selection", func(f *referenceFixture) { f.input.SourceTargetIDs = []string{"deployment-child"} }, apperrors.ErrConflict},
		{"Helm source classification mismatch", func(f *referenceFixture) {
			f.catalog.bindings["test-b"].Targets[0].Metadata["importSource"] = "kubernetes"
		}, apperrors.ErrConflict},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newReferenceFixture(t)
			b := f.catalog.bindings["test-b"]
			b.Targets[0].WorkloadKind = "HelmRelease"
			b.Targets[0].TargetKind = "helm_release"
			b.Targets[0].Metadata["importSource"] = "helm"
			b.Targets[0].WorkloadName = "release"
			f.catalog.bindings["test-b"] = b
			f.targets.detail = domainresource.HelmReleaseDetailView{Name: "release", Namespace: "backend", Revision: "7", Status: "deployed", Notes: "synthetic-secret-notes"}
			f.targets.helmSources = []domainresource.ResourceSourceSnapshot{f.targets.sources["cluster-b/backend/Deployment/api/"]}
			tc.change(&f)
			item, err := f.preview(context.Background())
			if !errors.Is(err, tc.want) {
				t.Fatalf("preview=%+v err=%v", item, err)
			}
			if tc.want == nil && (item.Targets[0].HelmRevision != 7 || item.Targets[0].WorkloadName != "release") {
				t.Fatalf("release identity lost: %+v", item)
			}
		})
	}
}

func TestServiceReferencePreviewUsesExplicitIdentityAcrossNamespaces(t *testing.T) {
	f := newReferenceFixture(t)
	binding := f.catalog.bindings["test-b"]
	other := binding.Targets[0]
	other.ID, other.ClusterID, other.Namespace = "second-root", "cluster-c", "other-team"
	other.Metadata = map[string]any{"serviceId": "svc", "ownershipMode": "observe_only", "importSource": "kubernetes"}
	root := f.targets.sources["cluster-b/backend/Deployment/api/"]
	root.Resource.ClusterID, root.Resource.Namespace, root.Resource.UID = other.ClusterID, other.Namespace, "other-uid"
	raw, err := json.Marshal(root)
	if err != nil {
		t.Fatal(err)
	}
	other.Metadata["resourceSource"] = string(raw)
	binding.Targets = append(binding.Targets, other)
	f.catalog.bindings["test-b"] = binding
	f.targets.sources["cluster-c/other-team/Deployment/api/"] = root
	f.input.SourceTargetIDs = []string{"target-b", "second-root"}
	item, err := f.preview(context.Background())
	if err != nil || len(item.Targets) != 2 || len(f.targets.reads) != 2 {
		t.Fatalf("preview=%+v, reads=%v, err=%v", item, f.targets.reads, err)
	}
	if item.Targets[0].TargetID != "second-root" || item.Targets[0].Resources[0].Resource.UID != "other-uid" || item.Targets[1].TargetID != "target-b" || item.Targets[1].Resources[0].Resource.UID != "uid-api" {
		t.Fatalf("same name conflated physical identities: %+v", item.Targets)
	}
}

func TestServiceReferencePreviewBoundsGroupsAndPropagatesCancellation(t *testing.T) {
	t.Run("group limit", func(t *testing.T) {
		f := newReferenceFixture(t)
		binding := f.catalog.bindings["test-b"]
		for i := range maxReferenceTargets {
			target := binding.Targets[0]
			target.ID = strings.Repeat("x", i+1)
			binding.Targets = append(binding.Targets, target)
		}
		f.catalog.bindings["test-b"] = binding
		if _, err := f.preview(context.Background()); !errors.Is(err, apperrors.ErrConflict) || len(f.targets.reads) != 0 {
			t.Fatalf("oversized group read: %v, %v", f.targets.reads, err)
		}
	})
	t.Run("caller cancellation", func(t *testing.T) {
		f := newReferenceFixture(t)
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		if item, err := f.preview(ctx); !errors.Is(err, context.Canceled) || item.ApplicationID != "" {
			t.Fatalf("cancelled preview returned data: %+v, %v", item, err)
		}
	})
}
