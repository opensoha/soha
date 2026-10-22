package manifest

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"

	appaccess "github.com/opensoha/soha/internal/application/access"
	domainaccess "github.com/opensoha/soha/internal/domain/access"
	domaincatalog "github.com/opensoha/soha/internal/domain/catalog"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainmanifest "github.com/opensoha/soha/internal/domain/manifest"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type templateSelectionRepository interface {
	SetTemplateSelection(context.Context, string, int64, domainmanifest.TemplateSelection) error
}

func (s *DeclarativeService) GetTemplatePackage(ctx context.Context, principal domainidentity.Principal, id string) (domainmanifest.Package, error) {
	return s.base.Get(ctx, principal, id)
}

func (s *DeclarativeService) AuthorizeSensitiveTemplatePreview(ctx context.Context, principal domainidentity.Principal) error {
	return s.base.authorize(ctx, principal, appaccess.PermPlatformConfigurationSecretDataView)
}

func (s *DeclarativeService) PreviewTemplateAdoption(ctx context.Context, principal domainidentity.Principal, id string, selection domainmanifest.TemplateSelection) (domainmanifest.RenderResult, error) {
	binding, err := s.repository.GetBinding(ctx, id)
	if err != nil {
		return domainmanifest.RenderResult{}, err
	}
	item, _, err := s.bindingPackage(ctx, principal, binding.PackageID, domainaccess.ActionView)
	if err != nil {
		return domainmanifest.RenderResult{}, err
	}
	if err := s.validateTemplateAdoptionBaseline(ctx, principal, item, binding); err != nil {
		return domainmanifest.RenderResult{}, err
	}
	binding.TemplateSelection = &selection
	files, _, err := s.renderDeliveryTemplateFiles(ctx, principal, item, binding, item.Files, domainmanifest.DeliveryArtifacts{}, true)
	if err != nil {
		return domainmanifest.RenderResult{}, err
	}
	if err := s.base.authorizeSensitiveFiles(ctx, principal, files); err != nil {
		return domainmanifest.RenderResult{}, err
	}
	result, err := s.renderer.Render(ctx, item, binding, files, max(item.CurrentRevision, 1))
	if err == nil && domainmanifest.SensitiveDocuments(result.Documents) {
		err = s.AuthorizeSensitiveTemplatePreview(ctx, principal)
	}
	if err != nil {
		return domainmanifest.RenderResult{}, err
	}
	return result, nil
}

func TemplateFilesDigest(files []domainmanifest.File) string {
	data, _ := json.Marshal(files)
	hash := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(hash[:])
}

func (s *DeclarativeService) AdoptTemplate(ctx context.Context, principal domainidentity.Principal, id string, expected int64, selection domainmanifest.TemplateSelection) error {
	if err := s.base.authorize(ctx, principal, appaccess.ManagedActionPermission(appaccess.PermDeliveryManifestDeploymentsManage, "update")); err != nil {
		return err
	}
	binding, err := s.repository.GetBinding(ctx, id)
	if err != nil {
		return err
	}
	item, _, err := s.bindingPackage(ctx, principal, binding.PackageID, domainaccess.ActionUpdate)
	if err != nil {
		return err
	}
	template, err := s.validateTemplateSelection(ctx, principal, item, binding, expected, selection)
	if err != nil {
		return err
	}
	if _, err := s.PreviewTemplateAdoption(ctx, principal, id, selection); err != nil {
		return err
	}
	if len(binding.Overlay) > 0 {
		return fmt.Errorf("%w: independent overlays require manual reconciliation", apperrors.ErrConflict)
	}
	if _, err := template.ParameterSchema.ResolveParameters(template.Defaults, selection.Parameters, binding.TemplateParameters, template.EnvironmentOverrides); err != nil {
		return fmt.Errorf("%w: template parameters do not satisfy the selected version", apperrors.ErrInvalidArgument)
	}
	writer, ok := s.repository.(templateSelectionRepository)
	if !ok {
		return fmt.Errorf("%w: template selection repository is unavailable", apperrors.ErrConflict)
	}
	if err := writer.SetTemplateSelection(ctx, id, expected, selection); err != nil {
		return err
	}
	s.base.record(ctx, principal, "delivery.manifest.template.adopt", item, "adopted immutable template configuration; deployment unchanged")
	return nil
}

func (s *DeclarativeService) validateTemplateSelection(ctx context.Context, principal domainidentity.Principal, item domainmanifest.Package, binding domainmanifest.EnvironmentBinding, expected int64, selection domainmanifest.TemplateSelection) (domaincatalog.DeploymentTemplate, error) {
	service, err := s.base.applications.GetService(ctx, principal, item.ApplicationID, item.ServiceID)
	if err != nil {
		return domaincatalog.DeploymentTemplate{}, err
	}
	ref := service.DeploymentTemplate
	if ref == nil || ref.Detached || ref.TemplateID != selection.TemplateID || ref.ManifestPackageID != item.ID || !binding.Enabled {
		return domaincatalog.DeploymentTemplate{}, fmt.Errorf("%w: template binding is detached or changed", apperrors.ErrConflict)
	}
	if binding.Version != expected || selection.BaseFilesDigest != TemplateFilesDigest(item.Files) || selection.AdoptionID == "" {
		return domaincatalog.DeploymentTemplate{}, apperrors.ErrConflict
	}
	template, err := s.base.deploymentTemplates.GetDeploymentTemplateVersion(ctx, principal, selection.TemplateID, selection.Version)
	if err != nil {
		return domaincatalog.DeploymentTemplate{}, err
	}
	if template.Source.Renderer != item.Renderer || len(template.Source.Files) == 0 && template.Source.Git == nil {
		return domaincatalog.DeploymentTemplate{}, fmt.Errorf("%w: adoption requires a matching renderer and immutable packaged template files", apperrors.ErrInvalidArgument)
	}
	return template, nil
}
