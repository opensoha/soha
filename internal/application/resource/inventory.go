package resource

import (
	"context"
	"fmt"

	domainaccess "github.com/opensoha/soha/internal/domain/access"
	domaincluster "github.com/opensoha/soha/internal/domain/cluster"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainresource "github.com/opensoha/soha/internal/domain/resource"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

func (i *Inventory) directInventory() (DirectInventory, error) {
	if i.direct == nil {
		return nil, fmt.Errorf("%w: direct inventory adapter is not configured", apperrors.ErrClusterUnready)
	}
	return i.direct, nil
}

func (i *Inventory) ListNamespaces(ctx context.Context, principal domainidentity.Principal, clusterID string) ([]domainresource.NamespaceView, error) {
	request := resourceListRequest{clusterID: clusterID, kind: "Namespace", summary: func(source string) string { return fmt.Sprintf("listed namespaces via %s", source) }}
	return listSourcedModeResources(
		ctx, i.resourceAccess, principal, request, i.inventoryAgentClient, i.directInventory,
		bindClusterAgentList(ctx, InventoryAgent.ListNamespaces),
		bindClusterSourcedDirectList(ctx, clusterID, DirectInventory.ListNamespaces),
		nil, namespaceActions, setNamespaceActions,
	)
}

func (i *Inventory) CreateNamespace(ctx context.Context, principal domainidentity.Principal, clusterID string, input domainresource.NamespaceUpsertInput) (domainresource.NamespaceView, error) {
	connection, decision, err := i.authorize(ctx, principal, clusterID, input.Name, "Namespace", domainaccess.ActionCreate)
	if err != nil {
		return domainresource.NamespaceView{}, err
	}
	if connection.Summary.ConnectionMode == domaincluster.ConnectionModeAgent {
		return domainresource.NamespaceView{}, unsupportedAgentOperation("namespace creation is not supported for agent-connected clusters yet")
	}
	direct, err := i.directInventory()
	if err != nil {
		return domainresource.NamespaceView{}, err
	}
	item, err := direct.CreateNamespace(ctx, clusterID, input)
	if err != nil {
		_ = i.recordAudit(ctx, principal, clusterID, input.Name, "Namespace", input.Name, string(domainaccess.ActionCreate), "failure", err.Error())
		return domainresource.NamespaceView{}, err
	}
	item.AllowedActions = stringifyActions(decision.AllowedActions)
	_ = i.recordAudit(ctx, principal, clusterID, item.Name, "Namespace", item.Name, string(domainaccess.ActionCreate), "success", "created namespace")
	i.recordOperation(ctx, principal, "platform.namespace.create", clusterID, item.Name, "Namespace", item.Name, "created namespace", nil)
	return item, nil
}

func (i *Inventory) UpdateNamespace(ctx context.Context, principal domainidentity.Principal, clusterID, namespace string, input domainresource.NamespaceUpsertInput) (domainresource.NamespaceView, error) {
	connection, decision, err := i.authorize(ctx, principal, clusterID, namespace, "Namespace", domainaccess.ActionUpdate)
	if err != nil {
		return domainresource.NamespaceView{}, err
	}
	if connection.Summary.ConnectionMode == domaincluster.ConnectionModeAgent {
		return domainresource.NamespaceView{}, unsupportedAgentOperation("namespace update is not supported for agent-connected clusters yet")
	}
	direct, err := i.directInventory()
	if err != nil {
		return domainresource.NamespaceView{}, err
	}
	item, err := direct.UpdateNamespace(ctx, clusterID, namespace, input)
	if err != nil {
		_ = i.recordAudit(ctx, principal, clusterID, namespace, "Namespace", namespace, string(domainaccess.ActionUpdate), "failure", err.Error())
		return domainresource.NamespaceView{}, err
	}
	item.AllowedActions = stringifyActions(decision.AllowedActions)
	_ = i.recordAudit(ctx, principal, clusterID, namespace, "Namespace", namespace, string(domainaccess.ActionUpdate), "success", "updated namespace metadata")
	i.recordOperation(ctx, principal, "platform.namespace.update", clusterID, namespace, "Namespace", namespace, "updated namespace metadata", nil)
	return item, nil
}

func (i *Inventory) DeleteNamespace(ctx context.Context, principal domainidentity.Principal, clusterID, namespace string) error {
	connection, _, err := i.authorize(ctx, principal, clusterID, namespace, "Namespace", domainaccess.ActionDelete)
	if err != nil {
		return err
	}
	if connection.Summary.ConnectionMode == domaincluster.ConnectionModeAgent {
		return unsupportedAgentOperation("namespace deletion is not supported for agent-connected clusters yet")
	}
	direct, err := i.directInventory()
	if err != nil {
		return err
	}
	if err := direct.DeleteNamespace(ctx, clusterID, namespace); err != nil {
		_ = i.recordAudit(ctx, principal, clusterID, namespace, "Namespace", namespace, string(domainaccess.ActionDelete), "failure", err.Error())
		return err
	}
	_ = i.recordAudit(ctx, principal, clusterID, namespace, "Namespace", namespace, string(domainaccess.ActionDelete), "success", "deleted namespace")
	i.recordOperation(ctx, principal, "platform.namespace.delete", clusterID, namespace, "Namespace", namespace, "deleted namespace", nil)
	return nil
}

func (i *Inventory) ListNodes(ctx context.Context, principal domainidentity.Principal, clusterID string) ([]domainresource.NodeView, error) {
	request := resourceListRequest{clusterID: clusterID, kind: "Node", summary: func(source string) string { return fmt.Sprintf("listed nodes via %s", source) }}
	return listSourcedModeResources(
		ctx, i.resourceAccess, principal, request, i.inventoryAgentClient, i.directInventory,
		bindClusterAgentList(ctx, InventoryAgent.ListNodes),
		bindClusterSourcedDirectList(ctx, clusterID, DirectInventory.ListNodes),
		nil, nodeActions, setNodeActions,
	)
}

func namespaceActions(item domainresource.NamespaceView) []string { return item.AllowedActions }
func setNamespaceActions(item *domainresource.NamespaceView, actions []string) {
	item.AllowedActions = actions
}
func nodeActions(item domainresource.NodeView) []string              { return item.AllowedActions }
func setNodeActions(item *domainresource.NodeView, actions []string) { item.AllowedActions = actions }

func (i *Inventory) UpdateNode(ctx context.Context, principal domainidentity.Principal, clusterID, nodeName string, input domainresource.NodeUpdateInput) (domainresource.NodeDetailView, error) {
	connection, decision, err := i.authorize(ctx, principal, clusterID, "", "Node", domainaccess.ActionUpdate)
	if err != nil {
		return domainresource.NodeDetailView{}, err
	}
	if connection.Summary.ConnectionMode == domaincluster.ConnectionModeAgent {
		return domainresource.NodeDetailView{}, unsupportedAgentOperation("node mutation is not supported for agent-connected clusters yet")
	}
	direct, err := i.directInventory()
	if err != nil {
		return domainresource.NodeDetailView{}, err
	}
	item, err := direct.UpdateNode(ctx, clusterID, nodeName, input)
	if err != nil {
		_ = i.recordAudit(ctx, principal, clusterID, "", "Node", nodeName, string(domainaccess.ActionUpdate), "failure", err.Error())
		return domainresource.NodeDetailView{}, err
	}
	item.AllowedActions = stringifyActions(decision.AllowedActions)
	i.applyNodeUsageMetrics(ctx, clusterID, &item)
	_ = i.recordAudit(ctx, principal, clusterID, "", "Node", nodeName, string(domainaccess.ActionUpdate), "success", "updated node labels and taints")
	i.recordOperation(ctx, principal, "platform.node.update", clusterID, "", "Node", nodeName, "updated node labels and taints", nil)
	return item, nil
}

func (i *Inventory) GetNodeYAML(ctx context.Context, principal domainidentity.Principal, clusterID, name string) (domainresource.ResourceYAMLView, error) {
	connection, _, err := i.authorize(ctx, principal, clusterID, "", "Node", domainaccess.ActionView)
	if err != nil {
		return domainresource.ResourceYAMLView{}, err
	}
	if connection.Summary.ConnectionMode == domaincluster.ConnectionModeAgent {
		return domainresource.ResourceYAMLView{}, unsupportedAgentOperation("node yaml is not supported for agent-connected clusters yet")
	}
	direct, err := i.directInventory()
	if err != nil {
		return domainresource.ResourceYAMLView{}, err
	}
	item, err := direct.GetNodeYAML(ctx, clusterID, name)
	if err != nil {
		return domainresource.ResourceYAMLView{}, err
	}
	_ = i.recordAudit(ctx, principal, connection.Summary.ID, "", "Node", name, string(domainaccess.ActionView), "success", "viewed node yaml via live")
	return item, nil
}

func (i *Inventory) ApplyNodeYAML(ctx context.Context, principal domainidentity.Principal, clusterID, name, content string) (domainresource.ResourceYAMLView, error) {
	return i.yaml.applyResourceYAML(ctx, principal, clusterID, "", "Node", name, content)
}

func (i *Inventory) DeleteNode(ctx context.Context, principal domainidentity.Principal, clusterID, nodeName string) error {
	connection, _, err := i.authorize(ctx, principal, clusterID, "", "Node", domainaccess.ActionDelete)
	if err != nil {
		return err
	}
	if connection.Summary.ConnectionMode == domaincluster.ConnectionModeAgent {
		return unsupportedAgentOperation("node deletion is not supported for agent-connected clusters yet")
	}
	direct, err := i.directInventory()
	if err != nil {
		return err
	}
	if err := direct.DeleteNode(ctx, clusterID, nodeName); err != nil {
		_ = i.recordAudit(ctx, principal, clusterID, "", "Node", nodeName, string(domainaccess.ActionDelete), "failure", err.Error())
		return err
	}
	_ = i.recordAudit(ctx, principal, clusterID, "", "Node", nodeName, string(domainaccess.ActionDelete), "success", "deleted node object")
	i.recordOperation(ctx, principal, "platform.node.delete", clusterID, "", "Node", nodeName, "deleted node object", nil)
	return nil
}
