package resource

import (
	"context"
	"fmt"
	"strings"

	contractresource "github.com/opensoha/soha/contracts/resource"
	appaccess "github.com/opensoha/soha/internal/application/access"
	domainaccess "github.com/opensoha/soha/internal/domain/access"
	domaincluster "github.com/opensoha/soha/internal/domain/cluster"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainresource "github.com/opensoha/soha/internal/domain/resource"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

func (g *GenericResources) ApplyResourceYAMLByKind(ctx context.Context, principal domainidentity.Principal, clusterID, namespace, kind, name, content string) (domainresource.ResourceYAMLView, error) {
	return g.applyResourceYAML(ctx, principal, clusterID, namespace, kind, name, content)
}

func (g *GenericResources) applyResourceYAML(ctx context.Context, principal domainidentity.Principal, clusterID, namespace, kind, name, content string) (domainresource.ResourceYAMLView, error) {
	return g.applyResourceYAMLWithIdentity(ctx, principal, clusterID, namespace, kind, name, content, false)
}

func (g *GenericResources) ApplyObservedResourceYAMLByKind(ctx context.Context, principal domainidentity.Principal, clusterID, namespace, kind, name, content string) (domainresource.ResourceYAMLView, error) {
	return g.applyResourceYAMLWithIdentity(ctx, principal, clusterID, namespace, kind, name, content, true)
}

func (g *GenericResources) applyResourceYAMLWithIdentity(ctx context.Context, principal domainidentity.Principal, clusterID, namespace, kind, name, content string, requireIdentity bool) (domainresource.ResourceYAMLView, error) {
	namespace = strings.TrimSpace(namespace)
	if err := validateResourceYAMLNamespace(kind, namespace); err != nil {
		return domainresource.ResourceYAMLView{}, err
	}
	connection, _, err := g.authorize(ctx, principal, clusterID, namespace, kind, domainaccess.ActionUpdate)
	if err != nil {
		return domainresource.ResourceYAMLView{}, err
	}
	if err := g.authorizeResourceYAMLData(ctx, principal, kind); err != nil {
		return domainresource.ResourceYAMLView{}, err
	}
	if strings.TrimSpace(content) == "" {
		return domainresource.ResourceYAMLView{}, fmt.Errorf("%w: yaml content is required", apperrors.ErrInvalidArgument)
	}
	var item domainresource.ResourceYAMLView
	if connection.Summary.ConnectionMode == domaincluster.ConnectionModeAgent {
		client, err := g.genericResourceAgentClient(connection)
		if err != nil {
			return domainresource.ResourceYAMLView{}, err
		}
		if requireIdentity {
			guarded, ok := client.(observedYAMLAgent)
			if !ok {
				return item, apperrors.ErrUnsupportedOperation
			}
			item, err = guarded.ApplyObservedResourceYAML(ctx, namespace, kind, name, content)
		} else {
			item, err = client.ApplyResourceYAML(ctx, namespace, kind, name, content)
		}
		if err != nil {
			_ = g.recordAudit(ctx, principal, clusterID, namespace, kind, name, string(domainaccess.ActionUpdate), "failure", err.Error())
			return domainresource.ResourceYAMLView{}, wrapAgentResourceError(err)
		}
	} else {
		if g.direct == nil {
			return domainresource.ResourceYAMLView{}, fmt.Errorf("%w: direct generic resource adapter is not configured", apperrors.ErrClusterUnready)
		}
		if requireIdentity {
			guarded, ok := g.direct.(observedYAMLDirect)
			if !ok {
				return item, apperrors.ErrUnsupportedOperation
			}
			item, err = guarded.ApplyObservedResourceYAML(ctx, clusterID, namespace, kind, name, content)
		} else {
			item, err = g.direct.ApplyResourceYAML(ctx, clusterID, namespace, kind, name, content)
		}
		if err != nil {
			_ = g.recordAudit(ctx, principal, clusterID, namespace, kind, name, string(domainaccess.ActionUpdate), "failure", err.Error())
			return domainresource.ResourceYAMLView{}, err
		}
	}
	source := "direct"
	if connection.Summary.ConnectionMode == domaincluster.ConnectionModeAgent {
		source = "agent"
	}
	_ = g.recordAudit(ctx, principal, connection.Summary.ID, namespace, kind, name, string(domainaccess.ActionUpdate), "success", "applied resource yaml via "+source)
	g.recordOperation(ctx, principal, "platform.resource.apply", connection.Summary.ID, namespace, kind, name, "applied resource yaml via "+source, nil)
	return item, nil
}

func validateResourceYAMLNamespace(kind, namespace string) error {
	if definition, known := contractresource.LookupKubernetesResourceKind(kind); known && definition.Namespaced && namespace == "" {
		return fmt.Errorf("%w: namespace is required for %s YAML updates", apperrors.ErrInvalidArgument, definition.Kind)
	}
	return nil
}

func (g *GenericResources) GetResourceYAML(ctx context.Context, principal domainidentity.Principal, clusterID, namespace, kind, name string) (domainresource.ResourceYAMLView, error) {
	connection, _, err := g.authorize(ctx, principal, clusterID, namespace, kind, domainaccess.ActionView)
	if err != nil {
		return domainresource.ResourceYAMLView{}, err
	}
	if err := g.authorizeResourceYAMLData(ctx, principal, kind); err != nil {
		return domainresource.ResourceYAMLView{}, err
	}
	var item domainresource.ResourceYAMLView
	if connection.Summary.ConnectionMode == domaincluster.ConnectionModeAgent {
		client, err := g.genericResourceAgentClient(connection)
		if err != nil {
			return domainresource.ResourceYAMLView{}, err
		}
		item, err = client.GetResourceYAML(ctx, namespace, kind, name)
		if err != nil {
			return domainresource.ResourceYAMLView{}, wrapAgentResourceError(err)
		}
	} else {
		if g.direct == nil {
			return domainresource.ResourceYAMLView{}, fmt.Errorf("%w: direct generic resource adapter is not configured", apperrors.ErrClusterUnready)
		}
		item, err = g.direct.GetResourceYAML(ctx, clusterID, namespace, kind, name)
		if err != nil {
			return domainresource.ResourceYAMLView{}, err
		}
	}
	_ = g.recordAudit(ctx, principal, connection.Summary.ID, namespace, kind, name, string(domainaccess.ActionView), "success", "viewed resource yaml")
	return item, nil
}

func (g *GenericResources) authorizeResourceYAMLData(ctx context.Context, principal domainidentity.Principal, kind string) error {
	if strings.EqualFold(strings.TrimSpace(kind), "Secret") {
		return g.authorizeRuntimePermission(ctx, principal, appaccess.PermPlatformConfigurationSecretDataView)
	}
	return nil
}

func (g *GenericResources) DeleteResourceByKind(ctx context.Context, principal domainidentity.Principal, clusterID, namespace, kind, name string) error {
	return g.deleteResourceByKind(ctx, principal, clusterID, namespace, kind, name, "")
}

func (g *GenericResources) DeleteDeploymentWithExpectedUID(ctx context.Context, principal domainidentity.Principal, clusterID, namespace, name, expectedUID string) error {
	return g.deleteResourceByKind(ctx, principal, clusterID, namespace, "Deployment", name, expectedUID)
}

func (g *GenericResources) deleteResourceByKind(ctx context.Context, principal domainidentity.Principal, clusterID, namespace, kind, name, expectedUID string) error {
	connection, _, err := g.authorize(ctx, principal, clusterID, namespace, kind, domainaccess.ActionDelete)
	if err != nil {
		return err
	}
	if connection.Summary.ConnectionMode == domaincluster.ConnectionModeAgent {
		client, err := g.genericResourceAgentClient(connection)
		if err != nil {
			return err
		}
		if expectedUID == "" {
			err = client.DeleteResource(ctx, namespace, kind, name)
		} else {
			err = deleteObservedAgentWorkload(ctx, client, namespace, kind, name, expectedUID)
		}
		if err != nil {
			_ = g.recordAudit(ctx, principal, clusterID, namespace, kind, name, string(domainaccess.ActionDelete), "failure", err.Error())
			return wrapAgentResourceError(err)
		}
	} else {
		if g.direct == nil {
			return fmt.Errorf("%w: direct generic resource adapter is not configured", apperrors.ErrClusterUnready)
		}
		if expectedUID == "" {
			err = g.direct.DeleteResource(ctx, clusterID, namespace, kind, name)
		} else {
			err = deleteObservedDirectDeployment(ctx, g.direct, clusterID, namespace, name, expectedUID)
		}
		if err != nil {
			_ = g.recordAudit(ctx, principal, clusterID, namespace, kind, name, string(domainaccess.ActionDelete), "failure", err.Error())
			return err
		}
	}
	_ = g.recordAudit(ctx, principal, connection.Summary.ID, namespace, kind, name, string(domainaccess.ActionDelete), "success", "deleted resource")
	g.recordOperation(ctx, principal, "platform.resource.delete", connection.Summary.ID, namespace, kind, name, "deleted resource", nil)
	return nil
}
