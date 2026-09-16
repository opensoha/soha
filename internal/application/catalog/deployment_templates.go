package catalog

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"

	appaccess "github.com/opensoha/soha/internal/application/access"
	domainaccess "github.com/opensoha/soha/internal/domain/access"
	domaincatalog "github.com/opensoha/soha/internal/domain/catalog"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type DeploymentTemplateRepository interface {
	ListDeploymentTemplates(context.Context) ([]domaincatalog.DeploymentTemplate, error)
	GetDeploymentTemplate(context.Context, string) (domaincatalog.DeploymentTemplate, error)
	SaveDeploymentTemplate(context.Context, string, domaincatalog.DeploymentTemplateInput) (domaincatalog.DeploymentTemplate, error)
	PublishDeploymentTemplate(context.Context, string, int64) (domaincatalog.DeploymentTemplate, error)
	DeprecateDeploymentTemplate(context.Context, string) error
	ListDeploymentTemplateVersions(context.Context, string) ([]domaincatalog.DeploymentTemplate, error)
	GetDeploymentTemplateVersion(context.Context, string, int64) (domaincatalog.DeploymentTemplate, error)
}

type DeploymentTemplateRenderer interface {
	RenderDeploymentTemplate(context.Context, domaincatalog.DeploymentTemplateSource, map[string]any, map[string]string, map[string]string) (domaincatalog.DeploymentTemplateSource, error)
}

func (s *Service) SetDeploymentTemplates(repo DeploymentTemplateRepository, renderer DeploymentTemplateRenderer) {
	s.deploymentTemplates, s.deploymentRenderer = repo, renderer
}

func (s *Service) ListDeploymentTemplates(ctx context.Context, principal domainidentity.Principal) ([]domaincatalog.DeploymentTemplate, error) {
	if err := s.authorize(ctx, principal, appaccess.PermDeliveryDeploymentTemplatesView); err != nil {
		return nil, err
	}
	return s.deploymentTemplates.ListDeploymentTemplates(ctx)
}

func (s *Service) GetDeploymentTemplate(ctx context.Context, principal domainidentity.Principal, id string) (domaincatalog.DeploymentTemplate, error) {
	if err := s.authorize(ctx, principal, appaccess.PermDeliveryDeploymentTemplatesView); err != nil {
		return domaincatalog.DeploymentTemplate{}, err
	}
	return s.deploymentTemplates.GetDeploymentTemplate(ctx, strings.TrimSpace(id))
}

func (s *Service) SaveDeploymentTemplate(ctx context.Context, principal domainidentity.Principal, id string, input domaincatalog.DeploymentTemplateInput) (domaincatalog.DeploymentTemplate, error) {
	permission := appaccess.PermDeliveryDeploymentTemplatesCreate
	if id != "" {
		permission = appaccess.PermDeliveryDeploymentTemplatesUpdate
	}
	if err := s.authorize(ctx, principal, permission); err != nil {
		return domaincatalog.DeploymentTemplate{}, err
	}
	if err := normalizeDeploymentTemplate(&input.DeploymentTemplateSpec); err != nil {
		return domaincatalog.DeploymentTemplate{}, err
	}
	origin, err := s.templateCopyAudit(ctx, principal, "DeploymentTemplate", id, input.CopiedFrom)
	if err != nil {
		return domaincatalog.DeploymentTemplate{}, err
	}
	item, err := s.deploymentTemplates.SaveDeploymentTemplate(ctx, strings.TrimSpace(id), input)
	if err == nil {
		s.recordWriteLogDetails(ctx, principal, "delivery.deployment_template.save", "DeploymentTemplate", item.ID, item.Name, "saved deployment template draft", origin)
	}
	return item, err
}

func (s *Service) PublishDeploymentTemplate(ctx context.Context, principal domainidentity.Principal, id string, revision int64) (domaincatalog.DeploymentTemplate, error) {
	if err := s.authorize(ctx, principal, appaccess.PermDeliveryDeploymentTemplatesUpdate); err != nil {
		return domaincatalog.DeploymentTemplate{}, err
	}
	item, err := s.deploymentTemplates.GetDeploymentTemplate(ctx, id)
	if err != nil {
		return item, err
	}
	if revision < 1 {
		return item, fmt.Errorf("%w: expectedRevision is required", apperrors.ErrInvalidArgument)
	}
	if err := normalizeDeploymentTemplate(&item.DeploymentTemplateSpec); err != nil {
		return item, err
	}
	item, err = s.deploymentTemplates.PublishDeploymentTemplate(ctx, id, revision)
	if err == nil {
		s.recordWriteLogs(ctx, principal, "delivery.deployment_template.publish", "DeploymentTemplate", item.ID, item.Name, fmt.Sprintf("published version %d", item.PublishedVersion))
	}
	return item, err
}

func (s *Service) DeprecateDeploymentTemplate(ctx context.Context, principal domainidentity.Principal, id string) error {
	if err := s.authorize(ctx, principal, appaccess.PermDeliveryDeploymentTemplatesDelete); err != nil {
		return err
	}
	item, err := s.deploymentTemplates.GetDeploymentTemplate(ctx, id)
	if err != nil {
		return err
	}
	if err := s.deploymentTemplates.DeprecateDeploymentTemplate(ctx, id); err != nil {
		return err
	}
	s.recordWriteLogs(ctx, principal, "delivery.deployment_template.deprecate", "DeploymentTemplate", id, item.Name, "deprecated template; published history retained")
	return nil
}

func (s *Service) ListDeploymentTemplateVersions(ctx context.Context, principal domainidentity.Principal, id string) ([]domaincatalog.DeploymentTemplate, error) {
	if _, err := s.GetDeploymentTemplate(ctx, principal, id); err != nil {
		return nil, err
	}
	return s.deploymentTemplates.ListDeploymentTemplateVersions(ctx, id)
}

func (s *Service) GetDeploymentTemplateVersion(ctx context.Context, principal domainidentity.Principal, id string, version int64) (domaincatalog.DeploymentTemplate, error) {
	if _, err := s.GetDeploymentTemplate(ctx, principal, id); err != nil {
		return domaincatalog.DeploymentTemplate{}, err
	}
	return s.deploymentTemplates.GetDeploymentTemplateVersion(ctx, id, version)
}

func (s *Service) PreviewDeploymentTemplate(ctx context.Context, principal domainidentity.Principal, applicationID string, input domaincatalog.DeploymentTemplatePreviewInput) (domaincatalog.DeploymentTemplatePreview, error) {
	result := domaincatalog.DeploymentTemplatePreview{TemplateID: input.TemplateID, Version: input.Version, ConfigurationOnly: true, Diagnostics: []string{"配置预览；部署时使用真实产物并执行环境预检。"}}
	system, err := s.deploymentTemplatePreviewScope(ctx, principal, applicationID, input)
	if err != nil {
		return result, err
	}
	item, err := s.GetDeploymentTemplateVersion(ctx, principal, input.TemplateID, input.Version)
	if err != nil {
		return result, err
	}
	result.Parameters, err = item.ParameterSchema.ResolveParameters(item.Defaults, input.Parameters, input.Overrides, item.EnvironmentOverrides)
	if err != nil {
		return result, fmt.Errorf("%w: %v", apperrors.ErrInvalidArgument, err)
	}
	artifacts := make(map[string]string, len(item.Artifacts))
	for key := range item.Artifacts {
		artifacts[key] = "preview.invalid/" + input.ServiceKey + "@sha256:" + strings.Repeat("0", 64)
	}
	result.Source, err = s.deploymentRenderer.RenderDeploymentTemplate(ctx, item.Source, result.Parameters, system, artifacts)
	if err != nil {
		return result, err
	}
	data, err := json.Marshal(result.Source)
	if err != nil {
		return result, err
	}
	digest := sha256.Sum256(data)
	result.Digest = "sha256:" + hex.EncodeToString(digest[:])
	if item.Source.Git != nil || item.Source.Helm != nil {
		result.Diagnostics = append(result.Diagnostics, "此预览展示固定来源与参数；远端文件或 Chart 尚未渲染。")
	}
	return result, nil
}

func (s *Service) deploymentTemplatePreviewScope(ctx context.Context, principal domainidentity.Principal, applicationID string, input domaincatalog.DeploymentTemplatePreviewInput) (map[string]string, error) {
	if !deploymentServiceKeyPattern.MatchString(input.ServiceKey) || len(input.ServiceKey) > 63 {
		return nil, fmt.Errorf("%w: invalid serviceKey", apperrors.ErrInvalidArgument)
	}
	if err := s.authorize(ctx, principal, appaccess.PermDeliveryApplicationsView); err != nil {
		return nil, err
	}
	application, err := s.apps.Get(ctx, applicationID)
	if err != nil {
		return nil, err
	}
	if s.authorizer != nil {
		if err := s.authorizeDelivery(ctx, principal, appaccess.PermDeliveryApplicationsView, domainaccess.ActionView, "Application", application.ID, application.BusinessLineID, application.Group, "", application.ID); err != nil {
			return nil, err
		}
	}
	namespace := "preview"
	if input.ApplicationEnvironmentID != "" {
		binding, err := s.GetApplicationEnvironment(ctx, principal, input.ApplicationEnvironmentID)
		if err != nil {
			return nil, err
		}
		if binding.ApplicationID != applicationID {
			return nil, fmt.Errorf("%w: environment belongs to another application", apperrors.ErrAccessDenied)
		}
		namespace = binding.Namespace
		if namespace == "" {
			return nil, fmt.Errorf("%w: environment namespace is required", apperrors.ErrInvalidArgument)
		}
	}
	return map[string]string{"applicationId": application.ID, "serviceKey": input.ServiceKey, "namespace": namespace}, nil
}
