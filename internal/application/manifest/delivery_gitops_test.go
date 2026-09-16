package manifest

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	domainapp "github.com/opensoha/soha/internal/domain/application"
	domaindocument "github.com/opensoha/soha/internal/domain/deliverydocument"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainmanifest "github.com/opensoha/soha/internal/domain/manifest"
	"github.com/opensoha/soha/internal/infrastructure/manifestruntime"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type gitOpsTestApplications struct {
	testApplications
	repositoryIDs []string
}

func (a gitOpsTestApplications) Get(ctx context.Context, principal domainidentity.Principal, id string) (domainapp.App, error) {
	app, err := a.testApplications.Get(ctx, principal, id)
	app.RepositoryIDs = a.repositoryIDs
	return app, err
}

type gitOpsTestReader struct {
	repository    domainapp.SourceRepository
	source        domaindocument.Source
	calls         int
	wrongCommit   bool
	overrideFile  string
	originalImage string
}

func (r *gitOpsTestReader) GetRepository(context.Context, string) (domainapp.SourceRepository, error) {
	return r.repository, nil
}
func (r *gitOpsTestReader) ReadDeliveryDocuments(_ context.Context, _ domainapp.SourceRepository, source domaindocument.Source) (domaindocument.GitDocuments, error) {
	r.source = source
	r.calls++
	commit := source.RefValue
	if r.wrongCommit {
		commit = strings.Repeat("c", 40)
	}
	result := domaindocument.GitDocuments{ResolvedCommit: commit, Files: []domaindocument.File{
		{Path: "kustomization.yaml", Content: "resources: [deployment.yaml]"},
		{Path: "deployment.yaml", Content: `apiVersion: apps/v1
kind: Deployment
metadata: {name: web}
spec:
  replicas: 1
  selector: {matchLabels: {app: web}}
  template:
    metadata: {labels: {app: web}}
    spec: {containers: [{name: app, image: app:placeholder}]}
`},
	}}
	if r.originalImage != "" {
		result.Files[1].Content = strings.ReplaceAll(result.Files[1].Content, "app:placeholder", r.originalImage)
	}
	if r.overrideFile != "" {
		result.Files = append(result.Files, domaindocument.File{Path: r.overrideFile, Content: "kustomize: {images: [app=mutable:latest]}"})
	}
	return result, nil
}

func TestGitOpsPlanFreezesAuthorizedSourceAndCarriesChildrenToTasks(t *testing.T) {
	s, repo, _, target := newSnapshotTestService()
	reader := &gitOpsTestReader{repository: domainapp.SourceRepository{ID: "repo-1", URL: "https://git.example/config.git"}}
	s.sources, s.deliveryGit, s.renderer = reader, reader, manifestruntime.NewRenderer()
	s.base.applications = gitOpsTestApplications{repositoryIDs: []string{"repo-1"}}
	repo.base.revisions[0].Files = []domainmanifest.File{{Path: "app.yaml", Content: `apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: web
  annotations: {delivery.soha.io/repository-id: repo-1}
spec:
  project: payments
  destination: {server: "https://kubernetes.default.svc", namespace: payments}
  source:
    repoURL: https://git.example/config.git
    targetRevision: "` + strings.Repeat("b", 40) + `"
    path: web
    kustomize: {namespace: payments, images: ["app=registry.example/app@sha256:` + strings.Repeat("a", 64) + `"]}
  syncPolicy: {syncOptions: ["FailOnSharedResource=true"]}
`}}
	snapshot, err := s.CreateDeliverySnapshot(t.Context(), testPrincipal(), "payments", "payments-dev", target, 1, "plan", domainmanifest.DeliveryArtifacts{})
	requireSnapshotTestNoError(t, err)
	if len(snapshot.GitOpsDocuments) != 1 || snapshot.GitOpsDocuments[0].Kind != "Deployment" || !strings.Contains(snapshot.GitOpsDocuments[0].Content, "registry.example/app@sha256:") || reader.source.RefType != "commit" || reader.source.Path != "web" {
		t.Fatalf("source was not frozen: %#v", snapshot.GitOpsDocuments)
	}
	approveSnapshotPreflight(repo, snapshot)
	_, task, err := s.ApplyDeliverySnapshot(t.Context(), testPrincipal(), snapshot)
	requireSnapshotTestNoError(t, err)
	payload, err := decodeTaskPayload(task.Payload)
	requireSnapshotTestNoError(t, err)
	if !reflect.DeepEqual(payload.GitOpsDocuments, snapshot.GitOpsDocuments) || reader.calls != 1 {
		t.Fatal("execution lost children or re-read Git")
	}
	keys, err := s.deliveryConfigurationResourceKeys(t.Context(), testPrincipal(), domainapp.App{RepositoryIDs: []string{"repo-1"}}, repo.base.item, repo.binding, repo.base.revisions[0])
	requireSnapshotTestNoError(t, err)
	if len(keys) != 2 {
		t.Fatalf("batch configuration missed child resources: %v", keys)
	}
	changed := snapshot
	changed.GitOpsDocuments = nil
	if err := s.ValidateDeliverySnapshot(t.Context(), testPrincipal(), changed); err == nil {
		t.Fatal("missing child freeze accepted")
	}
	changed = snapshot
	changed.GitOpsDocuments = append([]domainmanifest.RenderedDocument(nil), snapshot.GitOpsDocuments...)
	changed.GitOpsDocuments[0].Content = strings.ReplaceAll(changed.GitOpsDocuments[0].Content, `"replicas":1`, `"replicas":2`)
	if err := s.ValidateDeliverySnapshot(t.Context(), testPrincipal(), changed); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatalf("changed child accepted: %v", err)
	}
	s.base.applications = gitOpsTestApplications{}
	if err := s.ValidateDeliverySnapshot(t.Context(), testPrincipal(), snapshot); !errors.Is(err, apperrors.ErrAccessDenied) {
		t.Fatalf("revoked repository accepted: %v", err)
	}
	reader.wrongCommit = true
	if _, err := s.freezeGitOpsDocuments(t.Context(), domainapp.App{RepositoryIDs: []string{"repo-1"}}, repo.binding, snapshot.Documents); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatalf("wrong source commit accepted: %v", err)
	}
	reader.wrongCommit = false
	for _, file := range []string{".argocd-source.yaml", ".argocd-source-web.yaml"} {
		reader.overrideFile = file
		if _, err := s.freezeGitOpsDocuments(t.Context(), domainapp.App{RepositoryIDs: []string{"repo-1"}}, repo.binding, snapshot.Documents); !errors.Is(err, apperrors.ErrInvalidArgument) {
			t.Fatalf("source override accepted: %v", err)
		}
	}
	reader.overrideFile, reader.originalImage = "", "registry.example/app:placeholder"
	documents := append([]domainmanifest.RenderedDocument(nil), snapshot.Documents...)
	documents[0].Content = strings.ReplaceAll(documents[0].Content, "app=registry.example/app@", "registry.example/app@")
	children, err := s.freezeGitOpsDocuments(t.Context(), domainapp.App{RepositoryIDs: []string{"repo-1"}}, repo.binding, documents)
	requireSnapshotTestNoError(t, err)
	if !reflect.DeepEqual(children, snapshot.GitOpsDocuments) {
		t.Fatal("plain Kustomize override changed frozen child output")
	}
}
