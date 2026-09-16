package manifest

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"

	resourceruntime "github.com/opensoha/soha-contracts/resource/runtime"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	domainapp "github.com/opensoha/soha/internal/domain/application"
	domaincatalog "github.com/opensoha/soha/internal/domain/catalog"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainmanifest "github.com/opensoha/soha/internal/domain/manifest"
	"github.com/opensoha/soha/internal/infrastructure/manifestruntime"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type snapshotApplications struct {
	testApplications
	service       domainapp.Service
	repositoryIDs []string
}

func TestRolloutTemplatesFreezeNativePolicyAndArtifactIdentity(t *testing.T) {
	for _, spec := range domaincatalog.BuiltinDeploymentTemplates() {
		if spec.Key != "soha-canary" && spec.Key != "soha-bluegreen" {
			continue
		}
		t.Run(spec.Key, func(t *testing.T) {
			s, repo, _, target := newSnapshotTestService()
			template := domaincatalog.DeploymentTemplate{ID: spec.Key, DeploymentTemplateSpec: spec, PublishedVersion: 1, ContentDigest: "sha256:" + strings.Repeat("a", 64)}
			parameters := map[string]any{"metricURL": "http://api-preview.payments.svc:8080/metrics"}
			if spec.Key == "soha-canary" {
				parameters["routeMatch"] = "Host(\"app.example.com\")"
			}
			apps := &snapshotApplications{service: domainapp.Service{ID: "payments-api", ApplicationID: "payments", Key: "api", Version: 3,
				DeploymentTemplate: &domaincatalog.DeploymentTemplateBinding{TemplateID: template.ID, Version: 1, ManifestPackageID: "package", Detached: true, DetachedTemplate: &template, Parameters: parameters},
			}}
			s.base.applications, s.renderer = apps, manifestruntime.NewRenderer()
			repo.base.item.ServiceID, repo.base.revisions[0].Files = apps.service.ID, template.Source.Files
			frozen, err := s.FreezeDeliveryConfiguration(t.Context(), testPrincipal(), "payments", apps.service.ID, "payments-dev", target, 1)
			requireSnapshotTestNoError(t, err)
			image := "registry.example/app@sha256:" + strings.Repeat("c", 64)
			snapshot, err := s.CreateDeliverySnapshot(t.Context(), testPrincipal(), "payments", "payments-dev", target, 1, "plan", domainmanifest.DeliveryArtifacts{ReleaseBundleID: "bundle", ServiceID: apps.service.ID, ContainerImages: map[string]string{"main": image}})
			requireSnapshotTestNoError(t, err)
			if !slices.Equal(frozen.ResourceKeys, domainmanifest.ResourceKeys(snapshot.ClusterID, snapshot.Documents)) {
				t.Fatal("artifact changed frozen identities")
			}
			var documents []*unstructured.Unstructured
			for _, document := range snapshot.Documents {
				object := &unstructured.Unstructured{}
				requireSnapshotTestNoError(t, object.UnmarshalJSON([]byte(document.Content)))
				documents = append(documents, object)
			}
			_, err = resourceruntime.ValidateRolloutResources(documents)
			requireSnapshotTestNoError(t, err)
			raw, _ := json.Marshal(snapshot.Documents)
			if !strings.Contains(string(raw), image) || len(snapshot.Documents) < 4 {
				t.Fatal("missing artifact or native dependencies")
			}
		})
	}
}

func (a *snapshotApplications) Get(ctx context.Context, principal domainidentity.Principal, id string) (domainapp.App, error) {
	app, err := a.testApplications.Get(ctx, principal, id)
	app.RepositoryIDs = a.repositoryIDs
	return app, err
}

func (a *snapshotApplications) GetService(context.Context, domainidentity.Principal, string, string) (domainapp.Service, error) {
	return a.service, nil
}

func TestServiceTemplatePlanFreezesParametersArtifactsAndServiceVersion(t *testing.T) {
	s, repo, _, target := newSnapshotTestService()
	ctx := context.Background()
	template := domaincatalog.DeploymentTemplate{ID: "http", DeploymentTemplateSpec: domaincatalog.BuiltinDeploymentTemplates()[0], PublishedVersion: 1, ContentDigest: "sha256:" + strings.Repeat("a", 64)}
	apps := &snapshotApplications{service: domainapp.Service{ID: "payments-api", ApplicationID: "payments", Key: "api", Version: 3,
		DeploymentTemplate: &domaincatalog.DeploymentTemplateBinding{TemplateID: "http", Version: 1, ManifestPackageID: "package", Parameters: map[string]any{"port": 8081, "replicas": 0}, Detached: true, DetachedTemplate: &template},
	}}
	s.base.applications, s.renderer = apps, manifestruntime.NewRenderer()
	repo.base.item.ServiceID, repo.base.revisions[0].Files = apps.service.ID, template.Source.Files
	repo.binding.TemplateParameters = map[string]any{"replicas": 2}
	if _, err := s.CreateDeliverySnapshot(ctx, testPrincipal(), "payments", "payments-dev", target, 1, "plan", domainmanifest.DeliveryArtifacts{}); !errors.Is(err, apperrors.ErrInvalidArgument) || len(repo.tasks) != 0 {
		t.Fatalf("missing artifact queued a preflight: %v", err)
	}
	frozen, err := s.FreezeDeliveryConfiguration(ctx, testPrincipal(), "payments", apps.service.ID, "payments-dev", target, 1)
	requireSnapshotTestNoError(t, err)
	if len(frozen.ResourceKeys) != 2 || len(repo.tasks) != 0 {
		t.Fatalf("configuration did not freeze identities without tasks: %+v", frozen)
	}
	image := "registry.invalid/api@sha256:" + strings.Repeat("b", 64)
	artifacts := domainmanifest.DeliveryArtifacts{ReleaseBundleID: "bundle", ServiceID: apps.service.ID, ContainerImages: map[string]string{"main": image}}
	snapshot, err := s.CreateDeliverySnapshot(ctx, testPrincipal(), "payments", "payments-dev", target, 1, "plan", artifacts)
	requireSnapshotTestNoError(t, err)
	if !slices.Equal(frozen.ResourceKeys, domainmanifest.ResourceKeys(snapshot.ClusterID, snapshot.Documents)) {
		t.Fatal("real artifact changed resource identity")
	}
	data, err := json.Marshal(snapshot)
	requireSnapshotTestNoError(t, err)
	var stored domainmanifest.DeliverySnapshot
	requireSnapshotTestNoError(t, json.Unmarshal(data, &stored))
	if stored.TemplateInputs.ServiceVersion != 3 || stored.TemplateInputs.ReleaseBundleID != "bundle" || stored.TemplateInputs.Parameters["replicas"] != float64(2) || stored.TemplateInputs.Parameters["port"] != float64(8081) {
		t.Fatalf("lost frozen template inputs: %+v", stored.TemplateInputs)
	}
	if !strings.Contains(string(data), image) || strings.Contains(string(data), "preview.invalid") || strings.Contains(string(data), "${{") {
		t.Fatal("plan did not render immutable artifact and typed parameters")
	}
	approveSnapshotPreflight(repo, snapshot)
	requireSnapshotTestNoError(t, s.ValidateDeliverySnapshot(ctx, testPrincipal(), stored))
	apps.service.Version++
	if err := s.ValidateDeliverySnapshot(ctx, testPrincipal(), stored); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatalf("service change did not invalidate approved inputs: %v", err)
	}
	apps.service.Version--
	apps.service.DeploymentTemplate.Parameters["port"] = 9090
	if err := s.ValidateDeliverySnapshot(ctx, testPrincipal(), stored); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatalf("parameter change did not invalidate approved inputs: %v", err)
	}
}

func TestGitOpsServiceTemplateFreezesSameResourceKeysBeforeAndAfterBuild(t *testing.T) {
	s, repo, _, target := newSnapshotTestService()
	var template domaincatalog.DeploymentTemplate
	for _, spec := range domaincatalog.BuiltinDeploymentTemplates() {
		if spec.Key == "soha-gitops" {
			template = domaincatalog.DeploymentTemplate{ID: spec.Key, DeploymentTemplateSpec: spec, PublishedVersion: 1, ContentDigest: "sha256:" + strings.Repeat("a", 64)}
		}
	}
	reader := &gitOpsTestReader{repository: domainapp.SourceRepository{ID: "repo", URL: "https://git.example/config.git"}, originalImage: "registry.example/app:placeholder"}
	apps := &snapshotApplications{repositoryIDs: []string{"repo"}, service: domainapp.Service{ID: "payments-api", ApplicationID: "payments", Key: "api", Version: 3,
		Containers: []domainapp.ServiceContainer{{Name: "main", ImageRepository: "registry.example/app"}},
		DeploymentTemplate: &domaincatalog.DeploymentTemplateBinding{TemplateID: template.ID, Version: 1, ManifestPackageID: "package", Detached: true, DetachedTemplate: &template,
			Parameters: map[string]any{"repositoryId": "repo", "repositoryURL": reader.repository.URL, "commit": strings.Repeat("b", 40), "project": "payments"}},
	}}
	s.base.applications, s.renderer, s.sources, s.deliveryGit = apps, manifestruntime.NewRenderer(), reader, reader
	repo.base.item.ServiceID, repo.base.revisions[0].Files = apps.service.ID, template.Source.Files
	frozen, err := s.FreezeDeliveryConfiguration(t.Context(), testPrincipal(), "payments", apps.service.ID, "payments-dev", target, 1)
	requireSnapshotTestNoError(t, err)
	if len(frozen.ResourceKeys) != 2 || len(repo.tasks) != 0 {
		t.Fatal("configuration freeze lost children or created tasks")
	}
	image := "registry.example/app@sha256:" + strings.Repeat("c", 64)
	snapshot, err := s.CreateDeliverySnapshot(t.Context(), testPrincipal(), "payments", "payments-dev", target, 1, "plan", domainmanifest.DeliveryArtifacts{ReleaseBundleID: "bundle", ServiceID: apps.service.ID, ContainerImages: map[string]string{"main": image}})
	requireSnapshotTestNoError(t, err)
	if !slices.Equal(frozen.ResourceKeys, domainmanifest.ResourceKeys(snapshot.ClusterID, snapshot.Documents, snapshot.GitOpsDocuments)) || !strings.Contains(snapshot.GitOpsDocuments[0].Content, image) {
		t.Fatal("verified artifact changed identities or failed to replace the Git image")
	}
}
