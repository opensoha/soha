package manifest

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"

	appaccess "github.com/opensoha/soha/internal/application/access"
	domainaccess "github.com/opensoha/soha/internal/domain/access"
	domainapp "github.com/opensoha/soha/internal/domain/application"
	domaincatalog "github.com/opensoha/soha/internal/domain/catalog"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainmanifest "github.com/opensoha/soha/internal/domain/manifest"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

func (s *DeclarativeService) FreezeDeliveryConfiguration(ctx context.Context, principal domainidentity.Principal, applicationID, serviceID, environmentID string, target domaincatalog.ReleaseTarget, revision int) (domainmanifest.DeliveryConfiguration, error) {
	for _, action := range []string{"preflight", "trigger"} {
		if err := s.base.authorize(ctx, principal, appaccess.ManagedActionPermission(appaccess.PermDeliveryManifestDeploymentsManage, action)); err != nil {
			return domainmanifest.DeliveryConfiguration{}, err
		}
	}
	binding, err := s.repository.GetBinding(ctx, target.ConfigRef)
	if err != nil {
		return domainmanifest.DeliveryConfiguration{}, err
	}
	item, app, err := s.bindingPackage(ctx, principal, binding.PackageID, domainaccess.ActionTrigger)
	if err != nil {
		return domainmanifest.DeliveryConfiguration{}, err
	}
	if item.ApplicationID != applicationID || item.ServiceID != serviceID || !binding.Enabled || binding.ApplicationEnvironmentID != environmentID || binding.ClusterID != target.ClusterID || binding.Namespace != target.Namespace || binding.Namespace == "" {
		return domainmanifest.DeliveryConfiguration{}, fmt.Errorf("%w: Manifest target must belong to the selected service and environment", apperrors.ErrInvalidArgument)
	}
	if err := s.validateBinding(ctx, principal, domainaccess.ActionTrigger, item, app, &binding); err != nil {
		return domainmanifest.DeliveryConfiguration{}, err
	}
	if err := s.validateDeliverySource(ctx, item.ID); err != nil {
		return domainmanifest.DeliveryConfiguration{}, err
	}
	if revision == 0 {
		revision = item.CurrentRevision
	}
	published, err := s.deliveryRevision(ctx, item.ID, revision)
	if err != nil {
		return domainmanifest.DeliveryConfiguration{}, err
	}
	service, err := s.base.applications.GetService(ctx, principal, applicationID, serviceID)
	if err != nil {
		return domainmanifest.DeliveryConfiguration{}, err
	}
	resourceKeys, err := s.deliveryConfigurationResourceKeys(ctx, principal, app, item, binding, published)
	if err != nil {
		return domainmanifest.DeliveryConfiguration{}, err
	}
	data, err := json.Marshal(struct {
		PackageID      string
		Renderer       string
		RevisionDigest string
		Binding        domainmanifest.EnvironmentBinding
		ServiceVersion int64
		Template       *domaincatalog.DeploymentTemplateBinding
	}{item.ID, item.Renderer, published.Digest, binding, service.Version, service.DeploymentTemplate})
	if err != nil {
		return domainmanifest.DeliveryConfiguration{}, err
	}
	digest := sha256.Sum256(data)
	return domainmanifest.DeliveryConfiguration{PackageID: item.ID, BindingID: binding.ID, Revision: revision, ConfigurationDigest: "sha256:" + hex.EncodeToString(digest[:]), ResourceKeys: resourceKeys}, nil
}

func (s *DeclarativeService) deliveryConfigurationResourceKeys(ctx context.Context, principal domainidentity.Principal, app domainapp.App, item domainmanifest.Package, binding domainmanifest.EnvironmentBinding, published domainmanifest.Revision) ([]string, error) {
	files, _, err := s.renderDeliveryTemplateFiles(ctx, principal, item, binding, published.Files, domainmanifest.DeliveryArtifacts{}, true)
	if err != nil {
		return nil, err
	}
	rendered, err := s.renderer.Render(ctx, item, binding, files, published.Version)
	if err != nil {
		return nil, err
	}
	if err := validateDeliveryDocuments(rendered.Documents, binding.Namespace); err != nil {
		return nil, err
	}
	children, err := s.freezeGitOpsDocuments(ctx, app, binding, rendered.Documents)
	if err != nil {
		return nil, err
	}
	return domainmanifest.ResourceKeys(binding.ClusterID, rendered.Documents, children), nil
}
