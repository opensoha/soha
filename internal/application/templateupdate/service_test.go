package templateupdate

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/opensoha/soha-contracts/gen/go/sohaapi"
	domainapp "github.com/opensoha/soha/internal/domain/application"
	domaincatalog "github.com/opensoha/soha/internal/domain/catalog"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainmanifest "github.com/opensoha/soha/internal/domain/manifest"
	domainupdate "github.com/opensoha/soha/internal/domain/templateupdate"
	domainworkflow "github.com/opensoha/soha/internal/domain/workflow"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type memoryStore struct {
	previews   map[string]sohaapi.TemplateUpdatePreview
	candidates map[string]domainupdate.FrozenCandidate
	policies   map[string]domainupdate.Policy
}

func (r *memoryStore) SavePreview(_ context.Context, p sohaapi.TemplateUpdatePreview, candidates []domainupdate.FrozenCandidate) error {
	r.previews[p.ID] = p
	for _, c := range candidates {
		r.candidates[c.Candidate.ID] = c
	}
	return nil
}
func (r *memoryStore) GetPreview(_ context.Context, id string) (sohaapi.TemplateUpdatePreview, error) {
	v, ok := r.previews[id]
	if !ok {
		return v, apperrors.ErrNotFound
	}
	return v, nil
}
func (r *memoryStore) GetCandidate(_ context.Context, id string) (domainupdate.FrozenCandidate, error) {
	v, ok := r.candidates[id]
	if !ok {
		return v, apperrors.ErrNotFound
	}
	return v, nil
}
func (r *memoryStore) ApplyCandidate(ctx context.Context, id, planID string, apply func(context.Context, domainupdate.FrozenCandidate) error) error {
	v := r.candidates[id]
	if v.AppliedPlanID != "" {
		if v.AppliedPlanID == planID {
			return nil
		}
		return apperrors.ErrConflict
	}
	if err := apply(ctx, v); err != nil {
		return err
	}
	v.AppliedPlanID = planID
	r.candidates[id] = v
	return nil
}
func (r *memoryStore) ListPolicies(_ context.Context, f domainupdate.Filter) ([]domainupdate.Policy, error) {
	out := []domainupdate.Policy{}
	for _, v := range r.policies {
		if v.Value.Kind == f.Kind && v.Value.TemplateID == f.TemplateID && (f.ApplicationID == "" || v.ApplicationID == f.ApplicationID) {
			out = append(out, v)
		}
	}
	return out, nil
}
func (r *memoryStore) SavePolicy(_ context.Context, v domainupdate.Policy, expected int64) (domainupdate.Policy, error) {
	old := r.policies[v.Value.ID]
	if old.Value.Version != expected {
		return v, apperrors.ErrConflict
	}
	v.Value.Version = expected + 1
	r.policies[v.Value.ID] = v
	return v, nil
}
func (r *memoryStore) ListDuePolicies(_ context.Context, _ int) ([]domainupdate.Policy, error) {
	out := []domainupdate.Policy{}
	for _, v := range r.policies {
		if !v.Value.Paused && (v.Value.Mode == "automatic" || v.Value.Mode == "approval") {
			out = append(out, v)
		}
	}
	return out, nil
}
func (r *memoryStore) WithPolicy(ctx context.Context, id string, expected int64, apply func(context.Context, *domainupdate.Policy) error) error {
	v := r.policies[id]
	if v.Value.Version != expected {
		return nil
	}
	if err := apply(ctx, &v); err != nil {
		return err
	}
	r.policies[id] = v
	return nil
}

type testApps struct {
	items    []domainapp.App
	services map[string][]domainapp.Service
	updated  []domainapp.UpsertInput
}

func (r *testApps) List(context.Context, domainidentity.Principal, domainapp.Filter) ([]domainapp.App, error) {
	return r.items, nil
}
func (r *testApps) Get(_ context.Context, _ domainidentity.Principal, id string) (domainapp.App, error) {
	for _, v := range r.items {
		if v.ID == id {
			return v, nil
		}
	}
	return domainapp.App{}, apperrors.ErrNotFound
}
func (r *testApps) ListServices(_ context.Context, _ domainidentity.Principal, id string) ([]domainapp.Service, error) {
	return r.services[id], nil
}
func (r *testApps) Update(ctx context.Context, _ domainidentity.Principal, id string, input domainapp.UpsertInput) (domainapp.App, error) {
	if !domainupdate.IsAdoption(ctx, "build", id) {
		return domainapp.App{}, apperrors.ErrAccessDenied
	}
	for i, v := range r.items {
		if v.ID == id {
			if input.ExpectedVersion == nil || *input.ExpectedVersion != v.Version {
				return v, apperrors.ErrConflict
			}
			_ = copyJSON(input, &v)
			v.Version++
			r.items[i] = v
			r.updated = append(r.updated, input)
			return v, nil
		}
	}
	return domainapp.App{}, apperrors.ErrNotFound
}

type testCatalog struct {
	environments []domaincatalog.ApplicationEnvironment
	definitions  []domaincatalog.Environment
	deployment   []domaincatalog.DeploymentTemplate
	build        []domaincatalog.BuildTemplate
	workflow     []domaincatalog.WorkflowTemplate
	denied       map[string]bool
	fullScopeErr error
	readHidden   map[string]bool
	readErr      error
	updated      []domaincatalog.ApplicationEnvironmentInput
}

func (r *testCatalog) ListEnvironments(context.Context, domainidentity.Principal) ([]domaincatalog.Environment, error) {
	return r.definitions, nil
}
func (r *testCatalog) ListApplicationEnvironments(context.Context, domainidentity.Principal) ([]domaincatalog.ApplicationEnvironment, error) {
	if r.readErr != nil {
		return nil, r.readErr
	}
	visible := []domaincatalog.ApplicationEnvironment{}
	for _, environment := range r.environments {
		if !r.readHidden[environment.ID] {
			visible = append(visible, environment)
		}
	}
	return visible, nil
}
func (r *testCatalog) ListTemplateAdoptionEnvironments(_ context.Context, _ domainidentity.Principal, id string) ([]domaincatalog.ApplicationEnvironment, error) {
	if r.fullScopeErr != nil {
		return nil, r.fullScopeErr
	}
	out := []domaincatalog.ApplicationEnvironment{}
	for _, v := range r.environments {
		if v.ApplicationID == id {
			out = append(out, v)
		}
	}
	return out, nil
}
func (r *testCatalog) GetApplicationEnvironment(_ context.Context, _ domainidentity.Principal, id string) (domaincatalog.ApplicationEnvironment, error) {
	for _, v := range r.environments {
		if v.ID == id {
			return v, nil
		}
	}
	return domaincatalog.ApplicationEnvironment{}, apperrors.ErrNotFound
}
func (r *testCatalog) AuthorizeApplicationEnvironmentPermission(ctx context.Context, p domainidentity.Principal, id, _ string) (domaincatalog.ApplicationEnvironment, error) {
	if r.denied[id] {
		return domaincatalog.ApplicationEnvironment{}, apperrors.ErrAccessDenied
	}
	return r.GetApplicationEnvironment(ctx, p, id)
}
func (r *testCatalog) UpdateApplicationEnvironment(ctx context.Context, _ domainidentity.Principal, id string, input domaincatalog.ApplicationEnvironmentInput) (domaincatalog.ApplicationEnvironment, error) {
	if !domainupdate.IsAdoption(ctx, "workflow", input.ApplicationID) && !domainupdate.IsAdoption(ctx, "deployment", input.ApplicationID) {
		return domaincatalog.ApplicationEnvironment{}, apperrors.ErrAccessDenied
	}
	for i, v := range r.environments {
		if v.ID == id {
			if input.ExpectedUpdatedAt == nil || !input.ExpectedUpdatedAt.Equal(v.UpdatedAt) {
				return v, apperrors.ErrConflict
			}
			_ = copyJSON(input, &v)
			v.UpdatedAt = v.UpdatedAt.Add(time.Second)
			r.environments[i] = v
			r.updated = append(r.updated, input)
			return v, nil
		}
	}
	return domaincatalog.ApplicationEnvironment{}, apperrors.ErrNotFound
}
func (r *testCatalog) GetDeploymentTemplate(context.Context, domainidentity.Principal, string) (domaincatalog.DeploymentTemplate, error) {
	return r.deployment[len(r.deployment)-1], nil
}
func (r *testCatalog) ListDeploymentTemplateVersions(context.Context, domainidentity.Principal, string) ([]domaincatalog.DeploymentTemplate, error) {
	return r.deployment, nil
}
func (r *testCatalog) GetDeploymentTemplateVersion(_ context.Context, _ domainidentity.Principal, _ string, n int64) (domaincatalog.DeploymentTemplate, error) {
	for _, v := range r.deployment {
		if v.PublishedVersion == n {
			return v, nil
		}
	}
	return domaincatalog.DeploymentTemplate{}, apperrors.ErrNotFound
}
func (r *testCatalog) PreviewDeploymentTemplate(ctx context.Context, p domainidentity.Principal, _ string, input domaincatalog.DeploymentTemplatePreviewInput) (domaincatalog.DeploymentTemplatePreview, error) {
	v, err := r.GetDeploymentTemplateVersion(ctx, p, input.TemplateID, input.Version)
	if err != nil {
		return domaincatalog.DeploymentTemplatePreview{}, err
	}
	parameters, err := v.ParameterSchema.ResolveParameters(v.Defaults, input.Parameters, input.Overrides, v.EnvironmentOverrides)
	return domaincatalog.DeploymentTemplatePreview{Source: v.Source, Parameters: parameters, ConfigurationOnly: true}, err
}
func (r *testCatalog) GetBuildTemplate(context.Context, domainidentity.Principal, string) (domaincatalog.BuildTemplate, error) {
	return r.build[len(r.build)-1], nil
}
func (r *testCatalog) ListBuildTemplateVersions(context.Context, domainidentity.Principal, string) ([]domaincatalog.BuildTemplate, error) {
	return r.build, nil
}
func (r *testCatalog) GetBuildTemplateVersion(_ context.Context, _ domainidentity.Principal, _ string, n int64) (domaincatalog.BuildTemplate, error) {
	for _, v := range r.build {
		if v.PublishedVersion == n {
			return v, nil
		}
	}
	return domaincatalog.BuildTemplate{}, apperrors.ErrNotFound
}
func (r *testCatalog) GetWorkflowTemplate(context.Context, domainidentity.Principal, string) (domaincatalog.WorkflowTemplate, error) {
	return r.workflow[len(r.workflow)-1], nil
}
func (r *testCatalog) ListWorkflowTemplateVersions(context.Context, domainidentity.Principal, string) ([]domaincatalog.WorkflowTemplate, error) {
	return r.workflow, nil
}
func (r *testCatalog) GetWorkflowTemplateVersion(_ context.Context, _ domainidentity.Principal, _ string, n int64) (domaincatalog.WorkflowTemplate, error) {
	for _, v := range r.workflow {
		if v.PublishedVersion == n {
			return v, nil
		}
	}
	return domaincatalog.WorkflowTemplate{}, apperrors.ErrNotFound
}

type testManifests struct {
	sensitiveErr error
	packages     map[string]domainmanifest.Package
	bindings     map[string]domainmanifest.EnvironmentBinding
	catalog      *testCatalog
	adopted      []int64
}

func (r *testManifests) AuthorizeSensitiveTemplatePreview(context.Context, domainidentity.Principal) error {
	return r.sensitiveErr
}

func (r *testManifests) GetTemplatePackage(_ context.Context, _ domainidentity.Principal, id string) (domainmanifest.Package, error) {
	return r.packages[id], nil
}
func (r *testManifests) ListBindings(_ context.Context, _ domainidentity.Principal, id string) ([]domainmanifest.EnvironmentBinding, error) {
	out := []domainmanifest.EnvironmentBinding{}
	for _, v := range r.bindings {
		if v.PackageID == id {
			out = append(out, v)
		}
	}
	return out, nil
}
func (r *testManifests) PreviewTemplateAdoption(ctx context.Context, p domainidentity.Principal, id string, selection domainmanifest.TemplateSelection) (domainmanifest.RenderResult, error) {
	v, err := r.catalog.GetDeploymentTemplateVersion(ctx, p, selection.TemplateID, selection.Version)
	if err != nil {
		return domainmanifest.RenderResult{}, err
	}
	binding := r.bindings[id]
	parameters, err := v.ParameterSchema.ResolveParameters(v.Defaults, selection.Parameters, binding.TemplateParameters, v.EnvironmentOverrides)
	if err != nil {
		return domainmanifest.RenderResult{}, err
	}
	return domainmanifest.RenderResult{Documents: []domainmanifest.RenderedDocument{{APIVersion: "apps/v1", Kind: "Deployment", Name: id, Namespace: binding.Namespace, Content: fmt.Sprintf("version=%d parameters=%s", selection.Version, displayJSON(parameters, true))}}}, nil
}
func (r *testManifests) AdoptTemplate(ctx context.Context, _ domainidentity.Principal, id string, expected int64, selection domainmanifest.TemplateSelection) error {
	b := r.bindings[id]
	if b.Version != expected {
		return apperrors.ErrConflict
	}
	if !domainupdate.IsAdoption(ctx, "deployment", r.packages[b.PackageID].ApplicationID) {
		return apperrors.ErrAccessDenied
	}
	b.TemplateSelection = &selection
	b.Version++
	r.bindings[id] = b
	r.adopted = append(r.adopted, selection.Version)
	return nil
}

type testBatches struct {
	owner  *Service
	inputs []domainworkflow.DeliveryBatchInput
	values map[string]domainworkflow.DeliveryBatch
}

func (r *testBatches) CreateDeliveryBatch(ctx context.Context, p domainidentity.Principal, input domainworkflow.DeliveryBatchInput) (domainworkflow.DeliveryBatch, error) {
	for _, target := range input.Definition.Targets {
		if _, err := r.owner.Candidate(ctx, p, target.TemplateCandidateID, true); err != nil {
			return domainworkflow.DeliveryBatch{}, err
		}
	}
	r.inputs = append(r.inputs, input)
	v := domainworkflow.DeliveryBatch{ID: fmt.Sprintf("batch-%d", len(r.inputs)), Definition: *input.Definition, Status: "queued"}
	r.values[v.ID] = v
	return v, nil
}
func (r *testBatches) GetDeliveryBatch(_ context.Context, _ domainidentity.Principal, id string) (domainworkflow.DeliveryBatch, error) {
	return r.values[id], nil
}

type testPrincipals struct {
	calls int
	err   error
}

func (r *testPrincipals) CurrentExecutionPrincipal(_ context.Context, id, tokenID string) (domainidentity.Principal, error) {
	r.calls++
	return domainidentity.Principal{UserID: id, AccessTokenID: tokenID}, r.err
}

type fixture struct {
	s          *Service
	repo       *memoryStore
	apps       *testApps
	catalog    *testCatalog
	manifests  *testManifests
	batches    *testBatches
	principals *testPrincipals
	p          domainidentity.Principal
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	f := &fixture{repo: &memoryStore{previews: map[string]sohaapi.TemplateUpdatePreview{}, candidates: map[string]domainupdate.FrozenCandidate{}, policies: map[string]domainupdate.Policy{}}, apps: &testApps{services: map[string][]domainapp.Service{}}, catalog: &testCatalog{denied: map[string]bool{}}, batches: &testBatches{values: map[string]domainworkflow.DeliveryBatch{}}, principals: &testPrincipals{}, p: domainidentity.Principal{UserID: "user-1", AccessTokenID: "pat-1"}}
	f.manifests = &testManifests{packages: map[string]domainmanifest.Package{}, bindings: map[string]domainmanifest.EnvironmentBinding{}, catalog: f.catalog}
	for n := int64(1); n <= 4; n++ {
		schema := domaincatalog.ParameterSchema{Type: "object", Properties: map[string]domaincatalog.ParameterSchema{"replicas": {Type: "integer"}}}
		if n == 2 {
			schema.Properties["transientRequired"] = domaincatalog.ParameterSchema{Type: "string"}
			schema.Required = []string{"transientRequired"}
		}
		f.catalog.deployment = append(f.catalog.deployment, domaincatalog.DeploymentTemplate{ID: "tpl", PublishedVersion: n, ContentDigest: fmt.Sprintf("digest%d", n), PublicationState: "published", DeploymentTemplateSpec: domaincatalog.DeploymentTemplateSpec{Enabled: true, Source: domaincatalog.DeploymentTemplateSource{Renderer: "raw_yaml", Files: []domainmanifest.File{{Path: "deployment.yaml", Content: fmt.Sprintf("v%d", n)}}}, ParameterSchema: schema, Defaults: map[string]any{"replicas": float64(n)}, EnvironmentOverrides: []string{"replicas"}}})
	}
	f.catalog.definitions = []domaincatalog.Environment{{ID: "test-def", Key: "test", Enabled: true}, {ID: "prod-def", Key: "custom-live", IsProduction: true, Enabled: true}}
	for appIndex := 0; appIndex < 3; appIndex++ {
		id := fmt.Sprintf("app-%d", appIndex)
		app := domainapp.App{ID: id, Key: id, Name: id, Version: 1, Enabled: true}
		f.apps.items = append(f.apps.items, app)
		for _, envKey := range []string{"test", "prod"} {
			envID := id + "-" + envKey
			env := domaincatalog.ApplicationEnvironment{ID: envID, ApplicationID: id, EnvironmentID: envKey + "-def", EnvironmentKey: "custom-" + envKey, Alias: envKey, ClusterID: "cluster", Namespace: id, UpdatedAt: time.Unix(1, 0)}
			for serviceIndex := 0; serviceIndex < 4; serviceIndex++ {
				if appIndex > 0 && serviceIndex == 3 {
					continue
				}
				serviceID := fmt.Sprintf("%s-s%d", id, serviceIndex)
				packageID := serviceID + "-pkg"
				bindingID := serviceID + "-" + envKey
				if envKey == "test" {
					f.apps.services[id] = append(f.apps.services[id], domainapp.Service{ID: serviceID, ApplicationID: id, Key: fmt.Sprintf("s%d", serviceIndex), Name: serviceID, Version: 1, DeploymentTemplate: &domaincatalog.DeploymentTemplateBinding{TemplateID: "tpl", Version: 1, Parameters: map[string]any{"replicas": float64(1)}, ManifestPackageID: packageID}})
					f.manifests.packages[packageID] = domainmanifest.Package{ID: packageID, ApplicationID: id, ServiceID: serviceID, Renderer: "raw_yaml", Files: f.catalog.deployment[0].Source.Files}
				}
				f.manifests.bindings[bindingID] = domainmanifest.EnvironmentBinding{ID: bindingID, PackageID: packageID, ApplicationEnvironmentID: envID, Namespace: id, ClusterID: "cluster", Enabled: true, Version: 1}
				env.Targets = append(env.Targets, domaincatalog.ReleaseTarget{ID: bindingID, ConfigRef: bindingID, ApplicationEnvironmentID: envID, Metadata: map[string]any{"serviceId": serviceID}, Enabled: true})
			}
			f.catalog.environments = append(f.catalog.environments, env)
		}
	}
	var err error
	f.s, err = New(Dependencies{Repository: f.repo, Applications: f.apps, Environments: f.catalog, Deployment: f.catalog, Build: f.catalog, Workflow: f.catalog, Manifests: f.manifests, Batches: f.batches, Principals: f.principals})
	if err != nil {
		t.Fatal(err)
	}
	f.batches.owner = f.s
	return f
}
func (f *fixture) testTargets(t *testing.T) []sohaapi.TemplateUpdateTarget {
	t.Helper()
	page, err := f.s.ListTargets(context.Background(), f.p, domainupdate.Filter{Kind: "deployment", TemplateID: "tpl", PageSize: 100})
	if err != nil {
		t.Fatal(err)
	}
	out := []sohaapi.TemplateUpdateTarget{}
	for _, v := range page.Items {
		if v.EnvironmentName == "test" {
			out = append(out, v)
		}
	}
	return out
}
func (f *fixture) preview(t *testing.T, targets []sohaapi.TemplateUpdateTarget) sohaapi.TemplateUpdatePreview {
	t.Helper()
	selection := []sohaapi.TemplateUpdateSelection{}
	for _, target := range targets {
		selection = append(selection, sohaapi.TemplateUpdateSelection{TargetID: target.ID})
	}
	v, err := f.s.CreatePreview(context.Background(), f.p, sohaapi.TemplateUpdatePreviewInput{Kind: "deployment", TemplateID: "tpl", ToVersion: 4, Targets: selection})
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func TestManualAdoptionThreeApplicationsTenServicesAndVersionHistory(t *testing.T) {
	f := newFixture(t)
	targets := f.testTargets(t)
	if len(targets) != 10 {
		t.Fatalf("wanted 10 services, got %d", len(targets))
	}
	preview := f.preview(t, targets)
	ids := []string{}
	for _, candidate := range preview.Candidates {
		if !candidate.Eligible || candidate.Target.Mode != "fixed" || candidate.RequiresApproval {
			t.Fatalf("fixed manual adoption incorrectly blocked: %+v", candidate)
		}
		if len(candidate.Versions) != 3 || candidate.Cumulative.FromVersion != 1 || candidate.Cumulative.ToVersion != 4 {
			t.Fatalf("missing cumulative or adjacent history: %+v", candidate)
		}
		if candidate.Versions[0].RenderStatus != "invalid" || candidate.Parameters["replicas"] != float64(1) {
			t.Fatal("intermediate failure blocked final version or explicit override was lost")
		}
		ids = append(ids, candidate.ID)
	}
	_, err := f.s.CreateBatch(context.Background(), f.p, preview.ID, sohaapi.TemplateUpdateBatchInput{PreviewDigest: preview.Digest, CandidateIDs: ids, WaveSize: 3, StopOnFailure: true, IdempotencyKey: "manual-key"})
	if err != nil {
		t.Fatal(err)
	}
	definition := f.batches.inputs[0].Definition
	if definition.Mode != domainworkflow.DeliveryModeBuildAll {
		t.Fatal("adoption waves were serialized instead of using the existing grouped DAG")
	}
	for i, target := range definition.Targets {
		if target.Group != i/3 || target.Action != "config_update" || target.TemplateCandidateID == "" || target.ReleaseBundleID != "" {
			t.Fatalf("wrong wave or executable artifact: %+v", target)
		}
	}
	if len(f.manifests.adopted) != 0 {
		t.Fatal("preview or batch creation applied configuration")
	}
}

func TestCanonicalProductionApprovalAndIndependentOverride(t *testing.T) {
	f := newFixture(t)
	page, err := f.s.ListTargets(context.Background(), f.p, domainupdate.Filter{Kind: "deployment", TemplateID: "tpl", PageSize: 100})
	if err != nil {
		t.Fatal(err)
	}
	target := page.Items[slices.IndexFunc(page.Items, func(v sohaapi.TemplateUpdateTarget) bool { return v.EnvironmentName == "prod" })]
	if !target.RequiresApproval {
		t.Fatal("custom-key production environment bypassed approval")
	}
	b := f.manifests.bindings[target.ReleaseTargetID]
	b.TemplateParameters = map[string]any{"replicas": float64(7)}
	f.manifests.bindings[b.ID] = b
	preview := f.preview(t, []sohaapi.TemplateUpdateTarget{target})
	c := preview.Candidates[0]
	if !c.RequiresApproval || !c.Eligible {
		t.Fatal("production should be eligible but require approval")
	}
	if !slices.Contains(c.Target.IndependentOverrides, "environment.replicas") {
		t.Fatal("environment override provenance missing")
	}
	complete := false
	if err := f.s.Apply(context.Background(), f.p, c.ID, "plan-1", func(context.Context) error { complete = true; return nil }); err != nil {
		t.Fatal(err)
	}
	if !complete || !slices.Equal(f.manifests.adopted, []int64{4}) {
		t.Fatal("intermediate versions were applied or receipt missing")
	}
	if f.manifests.bindings[b.ID].TemplateParameters["replicas"] != float64(7) {
		t.Fatal("independent environment override changed")
	}
	if err := f.s.Apply(context.Background(), f.p, c.ID, "plan-1", func(context.Context) error { t.Fatal("idempotent replay completed twice"); return nil }); err != nil {
		t.Fatal(err)
	}
}

func TestCandidateIsolationCASExpiryAndNewDefaults(t *testing.T) {
	f := newFixture(t)
	targets := f.testTargets(t)
	first := targets[0]
	preview, err := f.s.CreatePreview(context.Background(), f.p, sohaapi.TemplateUpdatePreviewInput{Kind: "deployment", TemplateID: "tpl", ToVersion: 4, Targets: []sohaapi.TemplateUpdateSelection{{TargetID: first.ID, UseNewDefaults: []string{"replicas"}}}})
	if err != nil {
		t.Fatal(err)
	}
	c := preview.Candidates[0]
	if _, exists := c.Parameters["replicas"]; exists {
		t.Fatal("explicit new default retained old materialized value")
	}
	// Another target in the same environment may change without invalidating this binding.
	for i := range f.catalog.environments {
		if f.catalog.environments[i].ID == first.ApplicationEnvironmentID {
			f.catalog.environments[i].UpdatedAt = time.Now()
			for j := range f.catalog.environments[i].Targets {
				if f.catalog.environments[i].Targets[j].ID != first.ReleaseTargetID {
					f.catalog.environments[i].Targets[j].WorkloadName = "unrelated-change"
				}
			}
		}
	}
	if _, err := f.s.Candidate(context.Background(), f.p, c.ID, true); err != nil {
		t.Fatalf("sibling change invalidated independent target: %v", err)
	}
	b := f.manifests.bindings[first.ReleaseTargetID]
	b.Version++
	f.manifests.bindings[b.ID] = b
	if _, err := f.s.Candidate(context.Background(), f.p, c.ID, true); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatalf("stale binding accepted: %v", err)
	}
	if _, err := f.s.CreateBatch(context.Background(), f.p, preview.ID, sohaapi.TemplateUpdateBatchInput{PreviewDigest: "forged", CandidateIDs: []string{c.ID}, WaveSize: 1, IdempotencyKey: "key"}); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatal("forged preview digest accepted")
	}
}

func TestDetachedFixedPoliciesAndRevokedAutomaticIdentity(t *testing.T) {
	f := newFixture(t)
	targets := f.testTargets(t)
	target := targets[0]
	policy, err := f.s.SavePolicy(context.Background(), f.p, sohaapi.TemplateUpdatePolicyInput{Kind: "deployment", TemplateID: "tpl", TargetID: target.ID, Mode: "fixed"})
	if err != nil {
		t.Fatal(err)
	}
	if err := f.s.ReconcilePolicies(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(f.batches.inputs) != 0 {
		t.Fatal("fixed policy propagated")
	}
	_, err = f.s.SavePolicy(context.Background(), f.p, sohaapi.TemplateUpdatePolicyInput{Kind: "deployment", TemplateID: "tpl", TargetID: target.ID, Mode: "automatic", ExpectedVersion: policy.Version})
	if err != nil {
		t.Fatal(err)
	}
	f.principals.err = apperrors.ErrUnauthorized
	if err := f.s.ReconcilePolicies(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(f.batches.inputs) != 0 {
		t.Fatal("revoked execution identity created work")
	}
	if f.repo.policies[policy.ID].Value.LastError == "" {
		t.Fatal("policy lost actionable failure")
	}
	f.principals.err = nil
	if err := f.s.ReconcilePolicies(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(f.batches.inputs) != 1 || f.principals.calls < 3 {
		t.Fatal("automatic adoption did not restore current principal")
	}
	stored := f.repo.policies[policy.ID]
	stored.Value.Paused = true
	stored.Value.Version++
	f.repo.policies[policy.ID] = stored
	candidateID := f.batches.inputs[0].Definition.Targets[0].TemplateCandidateID
	if _, err := f.s.Candidate(context.Background(), f.p, candidateID, true); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatal("paused policy still dequeued a candidate")
	}
	services := f.apps.services[target.ApplicationID]
	for i := range services {
		if services[i].ID == target.ServiceID {
			copy := *services[i].DeploymentTemplate
			copy.Detached = true
			services[i].DeploymentTemplate = &copy
		}
	}
	f.apps.services[target.ApplicationID] = services
	page, err := f.s.ListTargets(context.Background(), f.p, domainupdate.Filter{Kind: "deployment", TemplateID: "tpl", ApplicationID: target.ApplicationID, PageSize: 100})
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range page.Items {
		if v.ServiceID == target.ServiceID && v.Eligible {
			t.Fatal("detached copy can propagate")
		}
	}
}

func TestApplicationWideBuildCannotHideAnUnauthorizedEnvironment(t *testing.T) {
	f := newFixture(t)
	f.catalog.build = []domaincatalog.BuildTemplate{{ID: "build", PublishedVersion: 1, Enabled: true}, {ID: "build", PublishedVersion: 2, Enabled: true}}
	f.apps.items[0].BuildSources = []domainapp.BuildSource{{ID: "source", Type: domainapp.BuildSourceTypePlatformTemplate, Config: map[string]any{"buildTemplateId": "build", "buildTemplateVersion": 1}}}
	f.catalog.fullScopeErr = apperrors.ErrAccessDenied
	if _, err := f.s.ListTargets(context.Background(), f.p, domainupdate.Filter{Kind: "build", TemplateID: "build"}); !errors.Is(err, apperrors.ErrAccessDenied) {
		t.Fatalf("filtered environment list hid part of a global update: %v", err)
	}
}

func TestHistoryGapRequiresAcknowledgementAndOnlyFinalVersionCanApply(t *testing.T) {
	f := newFixture(t)
	f.catalog.deployment = append(f.catalog.deployment[:1], f.catalog.deployment[3])
	preview := f.preview(t, f.testTargets(t)[:1])
	candidate := preview.Candidates[0]
	if !candidate.Eligible || !candidate.Cumulative.HistoryGap || len(candidate.Versions) != 1 {
		t.Fatal("history gap was hidden or blocked valid final version")
	}
	input := sohaapi.TemplateUpdateBatchInput{PreviewDigest: preview.Digest, CandidateIDs: []string{candidate.ID}, WaveSize: 1, IdempotencyKey: "gap-key-1"}
	if _, err := f.s.CreateBatch(context.Background(), f.p, preview.ID, input); !errors.Is(err, apperrors.ErrInvalidArgument) {
		t.Fatal("history gap silently accepted")
	}
	input.AcknowledgeHistoryGaps = true
	if _, err := f.s.CreateBatch(context.Background(), f.p, preview.ID, input); err != nil {
		t.Fatal(err)
	}
}

func TestSensitivePreviewRequiresCurrentPermissionOnCreationAndRead(t *testing.T) {
	f := newFixture(t)
	target := f.testTargets(t)[0]
	f.manifests.sensitiveErr = apperrors.ErrAccessDenied
	_, err := f.s.CreatePreview(context.Background(), f.p, sohaapi.TemplateUpdatePreviewInput{Kind: "deployment", TemplateID: "tpl", ToVersion: 4, Targets: []sohaapi.TemplateUpdateSelection{{TargetID: target.ID}}})
	if !errors.Is(err, apperrors.ErrAccessDenied) || len(f.repo.previews) != 0 {
		t.Fatal("sensitive preview persisted without permission")
	}
	f.manifests.sensitiveErr = nil
	preview := f.preview(t, []sohaapi.TemplateUpdateTarget{target})
	f.manifests.sensitiveErr = apperrors.ErrAccessDenied
	if result, err := f.s.GetPreview(context.Background(), f.p, preview.ID); !errors.Is(err, apperrors.ErrAccessDenied) || result.ID != "" {
		t.Fatal("revoked user read frozen sensitive diff")
	}
	if _, err := f.s.Candidate(context.Background(), f.p, preview.Candidates[0].ID, true); !errors.Is(err, apperrors.ErrAccessDenied) {
		t.Fatal("revoked sensitive permission accepted adoption")
	}
}

func TestFrozenPreviewIsReadableWithinCurrentScope(t *testing.T) {
	f := newFixture(t)
	selected := []sohaapi.TemplateUpdateTarget{}
	for _, target := range f.testTargets(t) {
		if (target.ApplicationID == "app-0" || target.ApplicationID == "app-1") && target.ServiceID == target.ApplicationID+"-s0" {
			selected = append(selected, target)
		}
	}
	if len(selected) != 2 {
		t.Fatal("fixture must cover two applications")
	}
	preview := f.preview(t, selected)
	f.catalog.readHidden = map[string]bool{selected[1].ApplicationEnvironmentID: true}
	visible, err := f.s.GetPreview(context.Background(), f.p, preview.ID)
	if err != nil || visible.Digest != preview.Digest || len(visible.Candidates) != 1 || visible.Candidates[0].ID != preview.Candidates[0].ID {
		t.Fatalf("scoped reviewer lost their frozen diff or digest: %+v %v", visible, err)
	}
	if err := f.s.AuthorizeApproval(context.Background(), f.p, preview.Candidates[0].ID); err != nil {
		t.Fatalf("unrelated application blocked scoped approval: %v", err)
	}
	_, err = f.s.CreateBatch(context.Background(), f.p, preview.ID, sohaapi.TemplateUpdateBatchInput{PreviewDigest: preview.Digest, CandidateIDs: []string{preview.Candidates[1].ID}, WaveSize: 1, IdempotencyKey: "scope-key"})
	if !errors.Is(err, apperrors.ErrNotFound) || len(f.batches.inputs) != 0 {
		t.Fatalf("hidden candidate entered a batch: %v", err)
	}
	offline := errors.New("environment reader unavailable")
	f.catalog.readErr = offline
	if _, err := f.s.GetPreview(context.Background(), f.p, preview.ID); !errors.Is(err, offline) {
		t.Fatalf("infrastructure failure was silently filtered: %v", err)
	}
	f.catalog.readErr = nil
	f.catalog.readHidden[selected[0].ApplicationEnvironmentID] = true
	if result, err := f.s.GetPreview(context.Background(), f.p, preview.ID); !errors.Is(err, apperrors.ErrAccessDenied) || result.ID != "" {
		t.Fatal("preview with no visible candidates leaked its metadata")
	}
}

func TestHelmIndependentValuesRemainAdoptableAndSourceEditsAreProtected(t *testing.T) {
	f := newFixture(t)
	source := sohaapi.DeploymentTemplateHelmSource{RepositoryURL: "https://charts.example.test", Chart: "service", Version: "1.0.0"}
	values := sohaapi.TemplateParameterValues{"replicas": float64(7)}
	b := domainupdate.Binding{Target: sohaapi.TemplateUpdateTarget{TemplateID: "template", Eligible: true}, ReleaseTarget: domaincatalog.ReleaseTarget{Helm: &sohaapi.HelmDeliveryConfiguration{Source: source, Values: &values}, Metadata: map[string]any{"templateSelection": domainmanifest.TemplateSelection{TemplateID: "template", Version: 2, Parameters: map[string]any{"replicas": float64(1)}, BaseFilesDigest: digest(source), AdoptionID: "plan"}}}}
	f.s.helmBinding(&b)
	if !b.Target.Eligible || b.Target.FromVersion != 2 {
		t.Fatal("independent Helm Values disabled adoption")
	}
	values["replicas"] = float64(9)
	f.s.helmBinding(&b)
	if !b.Target.Eligible {
		t.Fatal("updated independent Values were treated as template source edits")
	}
	b.ReleaseTarget.Helm.Source.Version = "manual-change"
	f.s.helmBinding(&b)
	if b.Target.Eligible {
		t.Fatal("independent Helm source could be overwritten")
	}
}

func TestPausingPolicyDoesNotInvalidateCommittedAdoptionReceipt(t *testing.T) {
	f := newFixture(t)
	target := f.testTargets(t)[0]
	policy, err := f.s.SavePolicy(context.Background(), f.p, sohaapi.TemplateUpdatePolicyInput{Kind: "deployment", TemplateID: "tpl", TargetID: target.ID, Mode: "automatic"})
	if err != nil {
		t.Fatal(err)
	}
	if err := f.s.ReconcilePolicies(context.Background()); err != nil {
		t.Fatal(err)
	}
	id := f.batches.inputs[0].Definition.Targets[0].TemplateCandidateID
	if err := f.s.Apply(context.Background(), f.p, id, "plan", func(context.Context) error { return nil }); err != nil {
		t.Fatal(err)
	}
	_, err = f.s.SavePolicy(context.Background(), f.p, sohaapi.TemplateUpdatePolicyInput{Kind: "deployment", TemplateID: "tpl", TargetID: target.ID, Mode: "automatic", Paused: true, ExpectedVersion: policy.Version})
	if err != nil {
		t.Fatal(err)
	}
	frozen, err := f.s.Candidate(context.Background(), f.p, id, true)
	if err != nil || frozen.AppliedPlanID != "plan" {
		t.Fatalf("pause hid a committed receipt: %+v %v", frozen, err)
	}
	if err := f.s.Apply(context.Background(), f.p, id, "plan", func(context.Context) error { t.Fatal("committed adoption repeated after pause"); return nil }); err != nil {
		t.Fatal(err)
	}
}
