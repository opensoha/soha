package catalog

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	contractdelivery "github.com/opensoha/soha-contracts/delivery"
	appaccess "github.com/opensoha/soha/internal/application/access"
	domaincatalog "github.com/opensoha/soha/internal/domain/catalog"
	domaindocument "github.com/opensoha/soha/internal/domain/deliverydocument"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

func (s *DocumentService) prepareDocument(ctx context.Context, principal domainidentity.Principal, candidate domaindocument.Candidate) (domaindocument.Mutation, error) {
	mutation := domaindocument.Mutation{Candidate: candidate}
	if candidate.Document.Kind != "Workflow" {
		if err := s.authorizeDocumentWrite(ctx, principal, candidate.Document.Kind, candidate.TargetID); err != nil {
			return mutation, err
		}
		if strings.TrimSpace(candidate.Document.Metadata.DisplayName) == "" {
			return mutation, fmt.Errorf("%w: template display name is required", apperrors.ErrInvalidArgument)
		}
	}
	data, err := json.Marshal(candidate.Document.Spec)
	if err != nil {
		return mutation, err
	}
	revision := candidate.ExpectedRevision
	var expected *int64
	if candidate.TargetID != "" {
		expected = &revision
	}
	switch candidate.Document.Kind {
	case "BuildTemplate":
		if err = json.Unmarshal(data, &mutation.Build); err == nil {
			mutation.Build.Key, mutation.Build.Name, mutation.Build.Description = documentNames(candidate.Document)
			publish := false
			mutation.Build.Publish, mutation.Build.ExpectedRevision = &publish, expected
			mutation.Build = normalizeBuildTemplateInput(mutation.Build)
			mutation.Build.BuilderKind = strings.TrimSpace(mutation.Build.BuilderKind)
			err = validateBuildTemplateInput(mutation.Build)
			if err == nil {
				mutation.Document, err = templateDocument(candidate.Document.Kind, mutation.Build.Key, mutation.Build.Name, mutation.Build.Description, mutation.Build)
			}
		}
	case "WorkflowTemplate":
		err = prepareWorkflowTemplateDocument(data, expected, &mutation)
	case "DeploymentTemplate":
		err = s.prepareDeploymentDocument(ctx, principal, data, expected, &mutation)
	case "Workflow":
		if err = json.Unmarshal(data, &mutation.Workflow); err == nil {
			mutation.Workflow.ExpectedVersion = expected
			mutation.Workflow, err = s.workflows.PrepareDeliveryWorkflow(ctx, principal, candidate.TargetID, mutation.Workflow)
			if err == nil {
				mutation.Document.Spec, err = documentJSON(map[string]any{"definition": mutation.Workflow.Definition})
			}
		}
	default:
		err = fmt.Errorf("%w: unsupported document kind", apperrors.ErrInvalidArgument)
	}
	if err != nil {
		return mutation, err
	}
	encoded, err := mutation.Document.Encode("json")
	if err != nil {
		return mutation, err
	}
	if _, invalid := contractdelivery.Parse(candidate.Path, encoded); len(invalid) > 0 {
		return mutation, fmt.Errorf("%w: normalized document cannot be represented without loss", apperrors.ErrInvalidArgument)
	}
	mutation.NormalizedSpecDigest, err = mutation.Document.NormalizedSpecDigest()
	return mutation, err
}

func prepareWorkflowTemplateDocument(data []byte, expected *int64, mutation *domaindocument.Mutation) error {
	if err := json.Unmarshal(data, &mutation.Template); err != nil {
		return err
	}
	mutation.Template.Key, mutation.Template.Name, mutation.Template.Description = documentNames(mutation.Document)
	publish := false
	mutation.Template.Publish, mutation.Template.ExpectedRevision = &publish, expected
	mutation.Template = normalizeWorkflowTemplateInput(mutation.Template)
	mutation.Template.Category = strings.TrimSpace(mutation.Template.Category)
	if isApplicationWorkflowCategory(mutation.Template.Category) {
		return fmt.Errorf("%w: application workflows must be saved through their application environment", apperrors.ErrInvalidArgument)
	}
	if err := validateWorkflowTemplateDefinition(mutation.Template.Definition); err != nil {
		return err
	}
	var err error
	mutation.Document, err = templateDocument(mutation.Document.Kind, mutation.Template.Key, mutation.Template.Name, mutation.Template.Description, mutation.Template)
	return err
}

func (s *DocumentService) prepareDeploymentDocument(ctx context.Context, principal domainidentity.Principal, data []byte, expected *int64, mutation *domaindocument.Mutation) error {
	if err := json.Unmarshal(data, &mutation.Deployment); err != nil {
		return err
	}
	mutation.Deployment.Key, mutation.Deployment.Name, mutation.Deployment.Description = documentNames(mutation.Document)
	mutation.Deployment.ExpectedRevision = expected
	if err := normalizeDeploymentTemplate(&mutation.Deployment.DeploymentTemplateSpec); err != nil {
		return err
	}
	if source := mutation.Deployment.Source.Git; source != nil {
		if _, err := s.sources.GetRepository(ctx, principal, source.RepositoryID); err != nil {
			return err
		}
	}
	var err error
	mutation.Document, err = templateDocument(mutation.Document.Kind, mutation.Deployment.Key, mutation.Deployment.Name, mutation.Deployment.Description, mutation.Deployment.DeploymentTemplateSpec)
	return err
}

func (s *DocumentService) authorizeDocumentWrite(ctx context.Context, principal domainidentity.Principal, kind, id string) error {
	permission := ""
	action := "create"
	if id != "" {
		action = "update"
	}
	switch kind {
	case "BuildTemplate":
		permission = appaccess.ManagedActionPermission(appaccess.PermDeliveryBuildTemplatesManage, action)
	case "WorkflowTemplate":
		permission = appaccess.ManagedActionPermission(appaccess.PermDeliveryWorkflowTemplatesManage, action)
	case "DeploymentTemplate":
		permission = appaccess.PermDeliveryDeploymentTemplatesCreate
		if id != "" {
			permission = appaccess.PermDeliveryDeploymentTemplatesUpdate
		}
	default:
		return fmt.Errorf("%w: unsupported template kind", apperrors.ErrInvalidArgument)
	}
	if err := s.catalog.authorize(ctx, principal, permission); err != nil {
		return err
	}
	if kind == "WorkflowTemplate" && id != "" {
		return s.catalog.rejectApplicationWorkflowTemplate(ctx, id)
	}
	return nil
}

func documentNames(document contractdelivery.Document) (string, string, string) {
	return document.Metadata.Name, strings.TrimSpace(document.Metadata.DisplayName), strings.TrimSpace(document.Metadata.Description)
}

func (s *DocumentService) readDocument(ctx context.Context, principal domainidentity.Principal, kind, id string, version int64) (contractdelivery.Document, int64, string, error) {
	switch kind {
	case "BuildTemplate":
		item, err := s.catalog.GetBuildTemplate(ctx, principal, id)
		if err == nil && version > 0 {
			item, err = s.catalog.GetBuildTemplateVersion(ctx, principal, id, version)
		}
		if err != nil {
			return contractdelivery.Document{}, 0, "", err
		}
		input := domaincatalog.BuildTemplateInput{Key: item.Key, Name: item.Name, Description: item.Description, BuilderKind: item.BuilderKind, DockerfileTemplate: item.DockerfileTemplate, BuildCommands: item.BuildCommands, VariableSchema: item.VariableSchema, DefaultVariables: item.DefaultVariables, Enabled: item.Enabled}
		document, err := templateDocument(kind, item.Key, item.Name, item.Description, input)
		return document, item.Revision, item.PublicationState, err
	case "WorkflowTemplate":
		item, err := s.catalog.GetWorkflowTemplate(ctx, principal, id)
		if err == nil && version > 0 {
			item, err = s.catalog.GetWorkflowTemplateVersion(ctx, principal, id, version)
		}
		if err != nil {
			return contractdelivery.Document{}, 0, "", err
		}
		input := domaincatalog.WorkflowTemplateInput{Key: item.Key, Name: item.Name, Description: item.Description, Category: item.Category, Definition: item.Definition, Enabled: item.Enabled}
		document, err := templateDocument(kind, item.Key, item.Name, item.Description, input)
		return document, item.Revision, item.PublicationState, err
	case "DeploymentTemplate":
		item, err := s.catalog.GetDeploymentTemplate(ctx, principal, id)
		if err == nil && version > 0 {
			item, err = s.catalog.GetDeploymentTemplateVersion(ctx, principal, id, version)
		}
		if err != nil {
			return contractdelivery.Document{}, 0, "", err
		}
		document, err := templateDocument(kind, item.Key, item.Name, item.Description, item.DeploymentTemplateSpec)
		return document, item.Revision, item.PublicationState, err
	case "Workflow":
		item, err := s.workflows.GetDeliveryWorkflow(ctx, principal, id)
		if err != nil {
			return contractdelivery.Document{}, 0, "", err
		}
		spec, err := documentJSON(map[string]any{"definition": item.Definition})
		return contractdelivery.Document{APIVersion: contractdelivery.APIVersion, Kind: kind, Metadata: contractdelivery.Metadata{Name: "workflow-" + id}, Spec: spec}, item.Version, "", err
	default:
		return contractdelivery.Document{}, 0, "", fmt.Errorf("%w: unsupported document kind", apperrors.ErrInvalidArgument)
	}
}

func templateDocument(kind, key, name, description string, input any) (contractdelivery.Document, error) {
	spec, err := documentJSON(input)
	if err != nil {
		return contractdelivery.Document{}, err
	}
	for _, field := range []string{"id", "key", "name", "description", "publish", "expectedRevision", "copiedFrom"} {
		delete(spec, field)
	}
	document := contractdelivery.Document{APIVersion: contractdelivery.APIVersion, Kind: kind, Metadata: contractdelivery.Metadata{Name: key, DisplayName: name, Description: description}, Spec: spec}
	data, err := json.Marshal(document)
	if err != nil {
		return document, err
	}
	normalized, invalid := contractdelivery.Parse("document.json", data)
	if len(invalid) == 0 {
		return normalized, nil
	}
	// Legacy objects remain readable for an explicit migration comparison.
	return document, nil
}

func (s *DocumentService) Export(ctx context.Context, principal domainidentity.Principal, kind, id string, version int64, format string) (domaindocument.Export, error) {
	if kind != "Workflow" && version < 1 || kind == "Workflow" && version != 0 {
		return domaindocument.Export{}, fmt.Errorf("%w: templates require a published version; workflows export their current definition", apperrors.ErrInvalidArgument)
	}
	document, _, _, err := s.readDocument(ctx, principal, kind, id, version)
	if err != nil {
		return domaindocument.Export{}, err
	}
	data, err := json.Marshal(document)
	if err != nil {
		return domaindocument.Export{}, err
	}
	document, invalid := contractdelivery.Parse("export.json", data)
	if len(invalid) > 0 {
		return domaindocument.Export{}, fmt.Errorf("%w: legacy template cannot be represented without loss; use its original API to export the original definition", apperrors.ErrConflict)
	}
	content, err := document.Encode(format)
	if err != nil {
		return domaindocument.Export{}, fmt.Errorf("%w: unsupported export format", apperrors.ErrInvalidArgument)
	}
	digest, err := document.NormalizedSpecDigest()
	return domaindocument.Export{Format: format, Content: string(content), Document: document, NormalizedSpecDigest: digest}, err
}
