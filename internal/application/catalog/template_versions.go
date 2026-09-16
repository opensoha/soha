package catalog

import (
	"context"
	"fmt"
	appaccess "github.com/opensoha/soha/internal/application/access"
	domaincatalog "github.com/opensoha/soha/internal/domain/catalog"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"strings"
)

func (s *Service) GetBuildTemplate(ctx context.Context, principal domainidentity.Principal, id string) (domaincatalog.BuildTemplate, error) {
	if err := s.authorize(ctx, principal, appaccess.PermDeliveryBuildTemplatesView); err != nil {
		return domaincatalog.BuildTemplate{}, err
	}
	return s.repo.GetBuildTemplate(ctx, strings.TrimSpace(id))
}

func (s *Service) ListBuildTemplateVersions(ctx context.Context, principal domainidentity.Principal, id string) ([]domaincatalog.BuildTemplate, error) {
	if _, err := s.GetBuildTemplate(ctx, principal, id); err != nil {
		return nil, err
	}
	return s.repo.ListBuildTemplateVersions(ctx, strings.TrimSpace(id))
}

func (s *Service) GetBuildTemplateVersion(ctx context.Context, principal domainidentity.Principal, id string, version int64) (domaincatalog.BuildTemplate, error) {
	if _, err := s.GetBuildTemplate(ctx, principal, id); err != nil {
		return domaincatalog.BuildTemplate{}, err
	}
	return s.repo.GetBuildTemplateVersion(ctx, strings.TrimSpace(id), version)
}

func (s *Service) PublishBuildTemplate(ctx context.Context, principal domainidentity.Principal, id string, expected int64) (domaincatalog.BuildTemplate, error) {
	if err := s.authorize(ctx, principal, appaccess.ManagedActionPermission(appaccess.PermDeliveryBuildTemplatesManage, "update")); err != nil {
		return domaincatalog.BuildTemplate{}, err
	}
	if expected < 1 {
		return domaincatalog.BuildTemplate{}, fmt.Errorf("%w: expectedRevision is required", apperrors.ErrInvalidArgument)
	}
	item, err := s.repo.PublishBuildTemplate(ctx, strings.TrimSpace(id), expected)
	if err == nil {
		s.recordWriteLogs(ctx, principal, "delivery.build_template.publish", "BuildTemplate", item.ID, item.Name, fmt.Sprintf("published template version %d", item.PublishedVersion))
	}
	return item, err
}

func (s *Service) GetWorkflowTemplate(ctx context.Context, principal domainidentity.Principal, id string) (domaincatalog.WorkflowTemplate, error) {
	if err := s.authorize(ctx, principal, appaccess.PermDeliveryWorkflowTemplatesView); err != nil {
		return domaincatalog.WorkflowTemplate{}, err
	}
	if err := s.rejectApplicationWorkflowTemplate(ctx, id); err != nil {
		return domaincatalog.WorkflowTemplate{}, err
	}
	return s.repo.GetWorkflowTemplate(ctx, strings.TrimSpace(id))
}

func (s *Service) ListWorkflowTemplateVersions(ctx context.Context, principal domainidentity.Principal, id string) ([]domaincatalog.WorkflowTemplate, error) {
	if _, err := s.GetWorkflowTemplate(ctx, principal, id); err != nil {
		return nil, err
	}
	return s.repo.ListWorkflowTemplateVersions(ctx, strings.TrimSpace(id))
}

func (s *Service) GetWorkflowTemplateVersion(ctx context.Context, principal domainidentity.Principal, id string, version int64) (domaincatalog.WorkflowTemplate, error) {
	if _, err := s.GetWorkflowTemplate(ctx, principal, id); err != nil {
		return domaincatalog.WorkflowTemplate{}, err
	}
	return s.repo.GetWorkflowTemplateVersion(ctx, strings.TrimSpace(id), version)
}

func (s *Service) PublishWorkflowTemplate(ctx context.Context, principal domainidentity.Principal, id string, expected int64) (domaincatalog.WorkflowTemplate, error) {
	if err := s.authorize(ctx, principal, appaccess.ManagedActionPermission(appaccess.PermDeliveryWorkflowTemplatesManage, "update")); err != nil {
		return domaincatalog.WorkflowTemplate{}, err
	}
	if err := s.rejectApplicationWorkflowTemplate(ctx, id); err != nil {
		return domaincatalog.WorkflowTemplate{}, err
	}
	if expected < 1 {
		return domaincatalog.WorkflowTemplate{}, fmt.Errorf("%w: expectedRevision is required", apperrors.ErrInvalidArgument)
	}
	item, err := s.repo.PublishWorkflowTemplate(ctx, strings.TrimSpace(id), expected)
	if err == nil {
		s.recordWriteLogs(ctx, principal, "delivery.workflow_template.publish", "WorkflowTemplate", item.ID, item.Name, fmt.Sprintf("published template version %d", item.PublishedVersion))
	}
	return item, err
}
