package resource

import (
	"context"
	"fmt"
	"strings"

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
	connection, _, err := g.authorize(ctx, principal, clusterID, namespace, kind, domainaccess.ActionUpdate)
	if err != nil {
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
		item, err = client.ApplyResourceYAML(ctx, namespace, kind, name, content)
		if err != nil {
			_ = g.recordAudit(ctx, principal, clusterID, namespace, kind, name, string(domainaccess.ActionUpdate), "failure", err.Error())
			return domainresource.ResourceYAMLView{}, fmt.Errorf("%w: %v", apperrors.ErrClusterUnready, err)
		}
	} else {
		if g.direct == nil {
			return domainresource.ResourceYAMLView{}, fmt.Errorf("%w: direct generic resource adapter is not configured", apperrors.ErrClusterUnready)
		}
		item, err = g.direct.ApplyResourceYAML(ctx, clusterID, namespace, kind, name, content)
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

func (g *GenericResources) GetResourceYAML(ctx context.Context, principal domainidentity.Principal, clusterID, namespace, kind, name string) (domainresource.ResourceYAMLView, error) {
	connection, _, err := g.authorize(ctx, principal, clusterID, namespace, kind, domainaccess.ActionView)
	if err != nil {
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
			return domainresource.ResourceYAMLView{}, fmt.Errorf("%w: %v", apperrors.ErrClusterUnready, err)
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

func (g *GenericResources) DeleteResourceByKind(ctx context.Context, principal domainidentity.Principal, clusterID, namespace, kind, name string) error {
	connection, _, err := g.authorize(ctx, principal, clusterID, namespace, kind, domainaccess.ActionDelete)
	if err != nil {
		return err
	}
	if connection.Summary.ConnectionMode == domaincluster.ConnectionModeAgent {
		client, err := g.genericResourceAgentClient(connection)
		if err != nil {
			return err
		}
		if err := client.DeleteResource(ctx, namespace, kind, name); err != nil {
			_ = g.recordAudit(ctx, principal, clusterID, namespace, kind, name, string(domainaccess.ActionDelete), "failure", err.Error())
			return fmt.Errorf("%w: %v", apperrors.ErrClusterUnready, err)
		}
	} else {
		if g.direct == nil {
			return fmt.Errorf("%w: direct generic resource adapter is not configured", apperrors.ErrClusterUnready)
		}
		if err := g.direct.DeleteResource(ctx, clusterID, namespace, kind, name); err != nil {
			_ = g.recordAudit(ctx, principal, clusterID, namespace, kind, name, string(domainaccess.ActionDelete), "failure", err.Error())
			return err
		}
	}
	_ = g.recordAudit(ctx, principal, connection.Summary.ID, namespace, kind, name, string(domainaccess.ActionDelete), "success", "deleted resource")
	g.recordOperation(ctx, principal, "platform.resource.delete", connection.Summary.ID, namespace, kind, name, "deleted resource", nil)
	return nil
}
