package catalog

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	contractdelivery "github.com/opensoha/soha-contracts/delivery"
	appaccess "github.com/opensoha/soha/internal/application/access"
	domainapp "github.com/opensoha/soha/internal/domain/application"
	domaincatalog "github.com/opensoha/soha/internal/domain/catalog"
	domaindocument "github.com/opensoha/soha/internal/domain/deliverydocument"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainworkflow "github.com/opensoha/soha/internal/domain/workflow"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

const buildDocumentYAML = `apiVersion: delivery.soha.io/v1alpha1
kind: BuildTemplate
metadata:
  name: example-build
  displayName: Example build
spec:
  buildCommands: [echo build]
`

type documentCatalogStub struct {
	stubCatalogRepository
	build          domaincatalog.BuildTemplate
	preview        domaindocument.StoredPreview
	mutations      []domaindocument.Mutation
	saves, applies int
	keyExists      bool
}

func (r *documentCatalogStub) FindDocumentKey(context.Context, string, string) (string, error) {
	if r.keyExists {
		return r.build.ID, nil
	}
	return "", apperrors.ErrNotFound
}
func (r *documentCatalogStub) SaveDocumentPreview(_ context.Context, preview domaindocument.StoredPreview) error {
	r.preview, r.saves = preview, r.saves+1
	return nil
}
func (r *documentCatalogStub) GetDocumentPreview(_ context.Context, id, actor string) (domaindocument.StoredPreview, error) {
	if id != r.preview.ID || actor != r.preview.ActorID {
		return domaindocument.StoredPreview{}, apperrors.ErrNotFound
	}
	return r.preview, nil
}
func (r *documentCatalogStub) ApplyDocumentImport(_ context.Context, preview domaindocument.StoredPreview, key string, mutations []domaindocument.Mutation) (domaindocument.Import, error) {
	r.mutations, r.applies = mutations, r.applies+1
	result := domaindocument.Import{PreviewID: preview.ID, Objects: []domaindocument.ImportedObject{{Kind: "BuildTemplate", ID: "build-1", Revision: 1, Action: "create", Path: preview.Candidates[0].Path}}}
	r.preview.IdempotencyKey, r.preview.Result = key, &result
	return result, nil
}
func (r *documentCatalogStub) GetBuildTemplate(context.Context, string) (domaincatalog.BuildTemplate, error) {
	return r.build, nil
}
func (r *documentCatalogStub) GetBuildTemplateVersion(context.Context, string, int64) (domaincatalog.BuildTemplate, error) {
	return r.build, nil
}

type documentWorkflowStub struct {
	DocumentWorkflowService
	err      error
	existing domainworkflow.DeliveryWorkflow
	prepared int
}

func (s *documentWorkflowStub) PrepareDeliveryWorkflow(_ context.Context, _ domainidentity.Principal, _ string, input domainworkflow.DeliveryWorkflowInput) (domainworkflow.DeliveryWorkflowInput, error) {
	s.prepared++
	return input, s.err
}
func (s *documentWorkflowStub) GetDeliveryWorkflow(context.Context, domainidentity.Principal, string) (domainworkflow.DeliveryWorkflow, error) {
	return s.existing, s.err
}

type documentSourceStub struct{ err error }

func (s documentSourceStub) GetRepository(context.Context, domainidentity.Principal, string) (domainapp.SourceRepository, error) {
	return domainapp.SourceRepository{}, s.err
}

func newDocumentTestService() (*DocumentService, *documentCatalogStub, *documentWorkflowStub, domainidentity.Principal) {
	repo := &documentCatalogStub{build: domaincatalog.BuildTemplate{ID: "build-1", Key: "example-build", Name: "Example build", BuilderKind: "custom", BuildCommands: []string{"echo build"}, Enabled: true, Revision: 2, PublishedVersion: 1, PublicationState: "published"}}
	catalog := New(repo, nil, nil, catalogPermissions(appaccess.PermDeliveryBuildTemplatesManage, appaccess.PermDeliveryBuildTemplatesView, appaccess.PermDeliveryWorkflowTemplatesManage, appaccess.PermDeliveryWorkflowTemplatesView, appaccess.PermDeliveryDeploymentTemplatesCreate), nil, nil)
	workflows := &documentWorkflowStub{}
	return NewDocuments(catalog, workflows, documentSourceStub{}, repo), repo, workflows, domainidentity.Principal{UserID: "author", Roles: []string{"admin"}}
}

func TestDocumentPreviewApplyAndReplayBoundary(t *testing.T) {
	ctx := context.Background()
	service, repo, _, principal := newDocumentTestService()
	preview, err := service.Preview(ctx, principal, domaindocument.PreviewInput{Files: []domaindocument.File{{Path: "build.yaml", Content: buildDocumentYAML}}})
	if err != nil || !preview.Valid || repo.saves != 1 || repo.applies != 0 || len(preview.Candidates) != 1 {
		t.Fatalf("preview must only save candidates: %+v %v", preview, err)
	}
	input := domaindocument.ApplyInput{CandidateDigest: preview.CandidateDigest, IdempotencyKey: "document-import"}
	bad := input
	bad.CandidateDigest = contractdelivery.Digest([]byte("tampered"))
	if _, err := service.Apply(ctx, principal, preview.ID, bad); !errors.Is(err, apperrors.ErrConflict) || repo.applies != 0 {
		t.Fatalf("changed digest accepted: %v", err)
	}
	if _, err := service.Apply(ctx, domainidentity.Principal{UserID: "other", Roles: principal.Roles}, preview.ID, input); !errors.Is(err, apperrors.ErrNotFound) {
		t.Fatalf("another actor applied preview: %v", err)
	}
	result, err := service.Apply(ctx, principal, preview.ID, input)
	if err != nil || repo.applies != 1 || !reflect.DeepEqual(repo.mutations[0].Candidate, preview.Candidates[0]) || repo.mutations[0].Build.Publish == nil || *repo.mutations[0].Build.Publish {
		t.Fatalf("apply changed content or published it: %+v %v", repo.mutations, err)
	}
	replayed, err := service.Apply(ctx, principal, preview.ID, input)
	if err != nil || repo.applies != 1 || !reflect.DeepEqual(result, replayed) {
		t.Fatalf("replay repeated a write: %+v %v", replayed, err)
	}
	service.catalog.permissions = catalogPermissions()
	if _, err := service.Apply(ctx, principal, preview.ID, input); !errors.Is(err, apperrors.ErrAccessDenied) {
		t.Fatalf("replay bypassed current read permission: %v", err)
	}
}

func TestDocumentSourceValidationDoesNotStoreImportCandidate(t *testing.T) {
	service, repo, _, principal := newDocumentTestService()
	result, err := service.Preview(context.Background(), principal, domaindocument.PreviewInput{ValidateOnly: true, Files: []domaindocument.File{{Path: "build.yaml", Content: buildDocumentYAML}}})
	if err != nil || !result.Valid || result.ID != "" || result.ExpiresAt != nil || len(result.Candidates) != 1 || repo.saves != 0 || repo.applies != 0 {
		t.Fatalf("source validation stored a preview or changed a template: %+v, %v, saves=%d applies=%d", result, err, repo.saves, repo.applies)
	}
}

func TestDocumentPreviewRejectsInvalidImportsBeforePersistence(t *testing.T) {
	for _, test := range []struct {
		name  string
		files []domaindocument.File
	}{
		{"empty", nil},
		{"duplicate path", []domaindocument.File{{Path: "build.yaml", Content: buildDocumentYAML}, {Path: "build.yaml", Content: buildDocumentYAML}}},
		{"duplicate identity", []domaindocument.File{{Path: "a.yaml", Content: buildDocumentYAML}, {Path: "b.yaml", Content: buildDocumentYAML}}},
		{"one invalid", []domaindocument.File{{Path: "a.yaml", Content: buildDocumentYAML}, {Path: "b.yaml", Content: "kind: invalid"}}},
		{"missing revision", []domaindocument.File{{Path: "a.yaml", Content: buildDocumentYAML, TargetID: "build-1"}}},
		{"empty normalized name", []domaindocument.File{{Path: "a.yaml", Content: strings.Replace(buildDocumentYAML, "displayName: Example build", `displayName: " "`, 1)}}},
		{"invalid business content", []domaindocument.File{{Path: "a.yaml", Content: strings.Replace(buildDocumentYAML, "buildCommands: [echo build]", "dockerfileTemplate: ' '", 1)}}},
	} {
		t.Run(test.name, func(t *testing.T) {
			service, repo, _, principal := newDocumentTestService()
			preview, err := service.Preview(context.Background(), principal, domaindocument.PreviewInput{Files: test.files})
			if err == nil && len(preview.Diagnostics) == 0 || preview.Valid || repo.saves != 0 || repo.applies != 0 {
				t.Fatalf("invalid import persisted: %+v %v", preview, err)
			}
		})
	}
}

func TestDocumentPreviewCASPermissionsAndExport(t *testing.T) {
	ctx := context.Background()
	service, repo, _, principal := newDocumentTestService()
	file := domaindocument.File{Path: "build.yaml", Content: buildDocumentYAML, TargetID: repo.build.ID, ExpectedRevision: 2}
	preview, err := service.Preview(ctx, principal, domaindocument.PreviewInput{Files: []domaindocument.File{file}})
	if err != nil || !preview.Valid || preview.Candidates[0].Action != "unchanged" {
		t.Fatalf("equivalent document changed: %+v %v", preview, err)
	}
	for _, format := range []string{"yaml", "json"} {
		exported, err := service.Export(ctx, principal, "BuildTemplate", repo.build.ID, 1, format)
		if err != nil || exported.NormalizedSpecDigest != preview.Candidates[0].NormalizedSpecDigest {
			t.Fatalf("lossy export: %+v %v", exported, err)
		}
	}
	if _, err := service.Export(ctx, principal, "BuildTemplate", repo.build.ID, 0, "yaml"); !errors.Is(err, apperrors.ErrInvalidArgument) {
		t.Fatalf("export did not require an immutable version: %v", err)
	}
	file.ExpectedRevision = 1
	if _, err := service.Preview(ctx, principal, domaindocument.PreviewInput{Files: []domaindocument.File{file}}); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatalf("stale preview accepted: %v", err)
	}
	file.ExpectedRevision = 2
	repo.build.PublicationState = "deprecated"
	if _, err := service.Preview(ctx, principal, domaindocument.PreviewInput{Files: []domaindocument.File{file}}); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatalf("deprecated preview accepted: %v", err)
	}
	service.catalog.permissions = catalogPermissions()
	if _, err := service.Preview(ctx, principal, domaindocument.PreviewInput{Files: []domaindocument.File{file}}); !errors.Is(err, apperrors.ErrAccessDenied) {
		t.Fatalf("missing update permission accepted: %v", err)
	}
}

func TestDocumentApplyRechecksExpiryAndWritePermission(t *testing.T) {
	ctx := context.Background()
	service, repo, _, principal := newDocumentTestService()
	preview, err := service.Preview(ctx, principal, domaindocument.PreviewInput{Files: []domaindocument.File{{Path: "build.yaml", Content: buildDocumentYAML}}})
	if err != nil {
		t.Fatal(err)
	}
	input := domaindocument.ApplyInput{CandidateDigest: preview.CandidateDigest, IdempotencyKey: "document-import"}
	service.catalog.permissions = catalogPermissions(appaccess.PermDeliveryBuildTemplatesView)
	if _, err := service.Apply(ctx, principal, preview.ID, input); !errors.Is(err, apperrors.ErrAccessDenied) || repo.applies != 0 {
		t.Fatalf("revoked write permission accepted: %v", err)
	}
	past := time.Now().Add(-time.Minute)
	repo.preview.ExpiresAt = &past
	if _, err := service.Apply(ctx, principal, preview.ID, input); !errors.Is(err, apperrors.ErrConflict) || repo.applies != 0 {
		t.Fatalf("expired preview accepted: %v", err)
	}
}

func TestDocumentValidationDiagnosticLocatesBusinessReference(t *testing.T) {
	cause := apperrors.NewBusiness(apperrors.ErrInvalidArgument, "delivery_service_build_source_missing", "bind a build source to the service first", "请先绑定构建源")
	diagnostic := documentValidationDiagnostic("workflow.yaml", &domainworkflow.TargetValidationError{Index: 2, Err: cause})
	if diagnostic.Pointer != "/spec/definition/targets/2/serviceId" || diagnostic.Code != "delivery_service_build_source_missing" || diagnostic.Message != "bind a build source to the service first" {
		t.Fatalf("unactionable diagnostic: %+v", diagnostic)
	}
	hidden := documentValidationDiagnostic("workflow.yaml", errors.New("private upstream credential"))
	if strings.Contains(hidden.Message, "credential") || hidden.Code != "invalid_definition" {
		t.Fatalf("unsafe diagnostic: %+v", hidden)
	}
}
