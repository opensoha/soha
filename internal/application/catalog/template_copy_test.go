package catalog

import (
	"context"
	"errors"
	"testing"

	appaccess "github.com/opensoha/soha/internal/application/access"
	domainaudit "github.com/opensoha/soha/internal/domain/audit"
	domaincatalog "github.com/opensoha/soha/internal/domain/catalog"
	domaindocument "github.com/opensoha/soha/internal/domain/deliverydocument"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type copyCatalogStub struct {
	*documentCatalogStub
	TemplateSourceRepository
	DeploymentTemplateRepository
	info             domaindocument.SourceInfo
	changeDuringRead bool
}

func (r *copyCatalogStub) GetDocumentSource(context.Context, string, string, int64) (domaindocument.SourceInfo, error) {
	if r.changeDuringRead {
		r.build.Revision++
	}
	return r.info, nil
}
func (r *copyCatalogStub) GetDeploymentTemplate(context.Context, string) (domaincatalog.DeploymentTemplate, error) {
	return domaincatalog.DeploymentTemplate{ID: "parent", Revision: 2, DeploymentTemplateSpec: domaincatalog.BuiltinDeploymentTemplates()[0]}, nil
}
func (r *copyCatalogStub) SaveDeploymentTemplate(_ context.Context, _ string, input domaincatalog.DeploymentTemplateInput) (domaincatalog.DeploymentTemplate, error) {
	return domaincatalog.DeploymentTemplate{ID: "copy", Revision: 1, DeploymentTemplateSpec: input.DeploymentTemplateSpec}, nil
}

type copyAuditRecorder struct{ entries []domainaudit.Entry }

func (r *copyAuditRecorder) Record(_ context.Context, entry domainaudit.Entry) error {
	r.entries = append(r.entries, entry)
	return nil
}

func TestTemplateCopyCreationRetainsDerivationAndRejectsStaleOrUnauthorizedOrigin(t *testing.T) {
	for _, kind := range []string{"BuildTemplate", "WorkflowTemplate", "DeploymentTemplate"} {
		t.Run(kind, func(t *testing.T) {
			documents, base, _, principal := newDocumentTestService()
			repo := &copyCatalogStub{documentCatalogStub: base, info: domaindocument.SourceInfo{Association: &domaindocument.Association{SourceID: "source", ResolvedCommit: "fixed-commit", SyncRunID: "sync"}}}
			base.workflowTemplates = map[string]domaincatalog.WorkflowTemplate{"parent": {ID: "parent", Revision: 2, Key: "parent", Name: "Parent", Definition: map[string]any{"mode": "delivery_batch"}}}
			service := documents.catalog
			audit := &copyAuditRecorder{}
			service.repo, service.deploymentTemplates, service.audit = repo, repo, audit
			service.permissions = catalogPermissions(appaccess.PermDeliveryBuildTemplatesManage, appaccess.PermDeliveryBuildTemplatesView, appaccess.PermDeliveryWorkflowTemplatesManage, appaccess.PermDeliveryWorkflowTemplatesView, appaccess.PermDeliveryDeploymentTemplatesCreate, appaccess.PermDeliveryDeploymentTemplatesView)
			origin := &domaincatalog.TemplateCopyOrigin{ID: "parent", Revision: 2}
			create := func() error {
				draft := false
				switch kind {
				case "BuildTemplate":
					_, err := service.CreateBuildTemplate(t.Context(), principal, domaincatalog.BuildTemplateInput{Key: "copy", Name: "Copy", BuildCommands: []string{"echo edited"}, Publish: &draft, CopiedFrom: origin})
					return err
				case "WorkflowTemplate":
					_, err := service.CreateWorkflowTemplate(t.Context(), principal, domaincatalog.WorkflowTemplateInput{Key: "copy", Name: "Copy", Category: "release", Publish: &draft, CopiedFrom: origin})
					return err
				default:
					_, err := service.SaveDeploymentTemplate(t.Context(), principal, "", domaincatalog.DeploymentTemplateInput{DeploymentTemplateSpec: domaincatalog.BuiltinDeploymentTemplates()[0], CopiedFrom: origin})
					return err
				}
			}
			if err := create(); err != nil {
				t.Fatal(err)
			}
			if len(audit.entries) != 1 {
				t.Fatalf("missing create audit: %+v", audit.entries)
			}
			parent, ok := audit.entries[0].Metadata["copiedFrom"].(map[string]any)
			if !ok {
				t.Fatal("missing copied-from metadata")
			}
			association, ok := parent["association"].(*domaindocument.Association)
			if !ok {
				t.Fatal("missing copied-from association")
			}
			if parent["id"] != "parent" || parent["kind"] != kind || parent["revision"] != int64(2) || association.ResolvedCommit != "fixed-commit" {
				t.Fatalf("lost origin: %+v", parent)
			}
			origin.Revision = 1
			if err := create(); !errors.Is(err, apperrors.ErrConflict) {
				t.Fatalf("stale copy accepted: %v", err)
			}
			origin.Revision = 2
			service.permissions = catalogPermissions(appaccess.PermDeliveryBuildTemplatesManage, appaccess.PermDeliveryWorkflowTemplatesManage, appaccess.PermDeliveryDeploymentTemplatesCreate)
			if err := create(); !errors.Is(err, apperrors.ErrAccessDenied) {
				t.Fatalf("source read authorization bypassed: %v", err)
			}
			if len(audit.entries) != 1 {
				t.Fatal("failed copy recorded as success")
			}
		})
	}
}

func TestTemplateCopyRechecksRevisionAfterReadingGitAssociation(t *testing.T) {
	documents, base, _, principal := newDocumentTestService()
	documents.catalog.repo = &copyCatalogStub{documentCatalogStub: base, changeDuringRead: true}
	_, err := documents.catalog.templateCopyAudit(t.Context(), principal, "BuildTemplate", "", &domaincatalog.TemplateCopyOrigin{ID: "build-1", Revision: 2})
	if !errors.Is(err, apperrors.ErrConflict) {
		t.Fatalf("concurrent import was misattributed: %v", err)
	}
}
