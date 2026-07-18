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

var configMapListSpec = newConfigurationListSpec(
	"ConfigMap",
	"listed configmaps",
	ConfigurationAgent.ListConfigMaps,
	DirectConfiguration.ListConfigMaps,
	configMapNamespace,
	configMapActions,
	setConfigMapActions,
)

func (c *Configuration) ListConfigMaps(ctx context.Context, principal domainidentity.Principal, clusterID, namespace string) ([]domainresource.ConfigMapView, error) {
	return listConfigurationCapability(ctx, c.resourceAccess, principal, clusterID, namespace, c.configurationAgentClient, c.directConfiguration, configMapListSpec)
}

var secretListSpec = newConfigurationListSpec(
	"Secret",
	"listed secrets",
	ConfigurationAgent.ListSecrets,
	DirectConfiguration.ListSecrets,
	secretNamespace,
	secretActions,
	setSecretActions,
)

func (c *Configuration) ListSecrets(ctx context.Context, principal domainidentity.Principal, clusterID, namespace string) ([]domainresource.SecretView, error) {
	return listConfigurationCapability(ctx, c.resourceAccess, principal, clusterID, namespace, c.configurationAgentClient, c.directConfiguration, secretListSpec)
}

func (c *Configuration) GetConfigMapDetail(ctx context.Context, principal domainidentity.Principal, clusterID, namespace, name string) (domainresource.ConfigMapDetailView, error) {
	connection, _, err := c.authorize(ctx, principal, clusterID, namespace, "ConfigMap", domainaccess.ActionView)
	if err != nil {
		return domainresource.ConfigMapDetailView{}, err
	}
	if connection.Summary.ConnectionMode == domaincluster.ConnectionModeAgent {
		return domainresource.ConfigMapDetailView{}, unsupportedAgentOperation("configmap detail is not supported for agent-connected clusters yet")
	}
	direct, err := c.directConfiguration()
	if err != nil {
		return domainresource.ConfigMapDetailView{}, err
	}
	item, err := direct.GetConfigMapDetail(ctx, clusterID, namespace, name)
	if err != nil {
		return domainresource.ConfigMapDetailView{}, err
	}
	_ = c.recordAudit(ctx, principal, connection.Summary.ID, namespace, "ConfigMap", name, string(domainaccess.ActionView), "success", "viewed configmap detail")
	return item, nil
}

func (c *Configuration) UpdateConfigMapData(ctx context.Context, principal domainidentity.Principal, clusterID, namespace, name string, data, binaryData map[string]string) (domainresource.ConfigMapDetailView, error) {
	connection, _, err := c.authorize(ctx, principal, clusterID, namespace, "ConfigMap", domainaccess.ActionUpdate)
	if err != nil {
		return domainresource.ConfigMapDetailView{}, err
	}
	if connection.Summary.ConnectionMode == domaincluster.ConnectionModeAgent {
		return domainresource.ConfigMapDetailView{}, unsupportedAgentOperation("configmap data update is not supported for agent-connected clusters yet")
	}
	direct, err := c.directConfiguration()
	if err != nil {
		return domainresource.ConfigMapDetailView{}, err
	}
	item, err := direct.UpdateConfigMapData(ctx, clusterID, namespace, name, data, binaryData)
	if err != nil {
		_ = c.recordAudit(ctx, principal, connection.Summary.ID, namespace, "ConfigMap", name, string(domainaccess.ActionUpdate), "failure", err.Error())
		return domainresource.ConfigMapDetailView{}, err
	}
	_ = c.recordAudit(ctx, principal, connection.Summary.ID, namespace, "ConfigMap", name, string(domainaccess.ActionUpdate), "success", "updated configmap data")
	c.recordOperation(ctx, principal, "platform.configmap.data.update", connection.Summary.ID, namespace, "ConfigMap", name, "updated configmap data", nil)
	return item, nil
}

func (c *Configuration) GetSecretDetail(ctx context.Context, principal domainidentity.Principal, clusterID, namespace, name string) (domainresource.SecretDetailView, error) {
	connection, _, err := c.authorize(ctx, principal, clusterID, namespace, "Secret", domainaccess.ActionView)
	if err != nil {
		return domainresource.SecretDetailView{}, err
	}
	if connection.Summary.ConnectionMode == domaincluster.ConnectionModeAgent {
		return domainresource.SecretDetailView{}, unsupportedAgentOperation("secret detail is not supported for agent-connected clusters yet")
	}
	direct, err := c.directConfiguration()
	if err != nil {
		return domainresource.SecretDetailView{}, err
	}
	item, err := direct.GetSecretDetail(ctx, clusterID, namespace, name)
	if err != nil {
		return domainresource.SecretDetailView{}, err
	}
	_ = c.recordAudit(ctx, principal, connection.Summary.ID, namespace, "Secret", name, string(domainaccess.ActionView), "success", "viewed secret detail")
	return item, nil
}

func (c *Configuration) UpdateSecretData(ctx context.Context, principal domainidentity.Principal, clusterID, namespace, name string, data map[string]string) (domainresource.SecretDetailView, error) {
	connection, _, err := c.authorize(ctx, principal, clusterID, namespace, "Secret", domainaccess.ActionUpdate)
	if err != nil {
		return domainresource.SecretDetailView{}, err
	}
	if connection.Summary.ConnectionMode == domaincluster.ConnectionModeAgent {
		return domainresource.SecretDetailView{}, unsupportedAgentOperation("secret data update is not supported for agent-connected clusters yet")
	}
	direct, err := c.directConfiguration()
	if err != nil {
		return domainresource.SecretDetailView{}, err
	}
	item, err := direct.UpdateSecretData(ctx, clusterID, namespace, name, data)
	if err != nil {
		_ = c.recordAudit(ctx, principal, connection.Summary.ID, namespace, "Secret", name, string(domainaccess.ActionUpdate), "failure", err.Error())
		return domainresource.SecretDetailView{}, err
	}
	_ = c.recordAudit(ctx, principal, connection.Summary.ID, namespace, "Secret", name, string(domainaccess.ActionUpdate), "success", "updated secret data")
	c.recordOperation(ctx, principal, "platform.secret.data.update", connection.Summary.ID, namespace, "Secret", name, "updated secret data", nil)
	return item, nil
}

func (c *Configuration) ListConfigMapReferences(ctx context.Context, principal domainidentity.Principal, clusterID, namespace, name string) ([]domainresource.ConfigReferenceView, error) {
	return c.listConfigReferences(ctx, principal, clusterID, namespace, name, true)
}

func (c *Configuration) ListSecretReferences(ctx context.Context, principal domainidentity.Principal, clusterID, namespace, name string) ([]domainresource.ConfigReferenceView, error) {
	return c.listConfigReferences(ctx, principal, clusterID, namespace, name, false)
}

func (c *Configuration) listConfigReferences(ctx context.Context, principal domainidentity.Principal, clusterID, namespace, name string, configMap bool) ([]domainresource.ConfigReferenceView, error) {
	kind := "Secret"
	unsupported := "secret references are not supported for agent-connected clusters yet"
	if configMap {
		kind = "ConfigMap"
		unsupported = "configmap references are not supported for agent-connected clusters yet"
	}
	connection, _, err := c.authorize(ctx, principal, clusterID, namespace, kind, domainaccess.ActionView)
	if err != nil {
		return nil, err
	}
	if connection.Summary.ConnectionMode == domaincluster.ConnectionModeAgent {
		return nil, unsupportedAgentOperation(unsupported)
	}
	direct, err := c.directConfiguration()
	if err != nil {
		return nil, err
	}
	items, err := direct.ListConfigReferences(ctx, clusterID, namespace, name, configMap)
	if err != nil {
		return nil, err
	}
	_ = c.recordAudit(ctx, principal, connection.Summary.ID, namespace, kind, name, string(domainaccess.ActionView), "success", "viewed "+strings.ToLower(kind)+" references")
	return items, nil
}

func (c *Configuration) CreateResourceFromYAML(ctx context.Context, principal domainidentity.Principal, clusterID, namespace, kind, content string) (domainresource.ResourceYAMLView, error) {
	if c.creation == nil {
		return domainresource.ResourceYAMLView{}, fmt.Errorf("%w: unified resource creation service is not configured", apperrors.ErrClusterUnready)
	}
	result, err := c.creation.ExecuteCreate(ctx, principal, clusterID, domainresource.ResourceCreateRequest{
		Source: domainresource.ResourceCreateSourceList, DefaultNamespace: strings.TrimSpace(namespace),
		ExpectedKind: kind, Content: content,
	})
	if err != nil {
		return domainresource.ResourceYAMLView{}, err
	}
	if result.Status != "succeeded" || len(result.Documents) != 1 || result.Documents[0].Status != "succeeded" {
		if len(result.Documents) == 1 && result.Documents[0].Error != "" {
			return domainresource.ResourceYAMLView{}, fmt.Errorf("%s", result.Documents[0].Error)
		}
		return domainresource.ResourceYAMLView{}, fmt.Errorf("%w: resource creation did not complete", apperrors.ErrInvalidArgument)
	}
	document := result.Documents[0]
	if c.generic != nil {
		if created, getErr := c.generic.GetResourceYAML(ctx, clusterID, document.Resource.Namespace, document.Resource.Kind, document.Resource.Name); getErr == nil {
			return created, nil
		}
	}
	return domainresource.ResourceYAMLView{Kind: document.Resource.Kind, Name: document.Resource.Name, Namespace: document.Resource.Namespace}, nil
}

func configurationItems[T any](connection domaincluster.Connection, agentCall func(ConfigurationAgent) ([]T, error), directCall func(DirectConfiguration) ([]T, error), agentFactory func(domaincluster.Connection) (ConfigurationAgent, error), directFactory func() (DirectConfiguration, error)) ([]T, string, error) {
	return routeModeItems(connection, agentFactory, directFactory, agentCall, directCall)
}

type configurationListSpec[T any] struct {
	kind        string
	auditText   string
	agentCall   func(ConfigurationAgent, context.Context, string) ([]T, error)
	directCall  func(DirectConfiguration, context.Context, string, string) ([]T, error)
	namespaceOf func(T) string
	actionsOf   func(T) []string
	setActions  func(*T, []string)
}

func newConfigurationListSpec[T any](kind, auditText string, agentCall func(ConfigurationAgent, context.Context, string) ([]T, error), directCall func(DirectConfiguration, context.Context, string, string) ([]T, error), namespaceOf func(T) string, actionsOf func(T) []string, setActions func(*T, []string)) configurationListSpec[T] {
	return configurationListSpec[T]{
		kind: kind, auditText: auditText, agentCall: agentCall, directCall: directCall,
		namespaceOf: namespaceOf, actionsOf: actionsOf, setActions: setActions,
	}
}

func listConfigurationCapability[T any](ctx context.Context, access *resourceAccess, principal domainidentity.Principal, clusterID, namespace string, agentFactory func(domaincluster.Connection) (ConfigurationAgent, error), directFactory func() (DirectConfiguration, error), spec configurationListSpec[T]) ([]T, error) {
	return listConfigurationResources(
		ctx,
		access,
		principal,
		clusterID,
		namespace,
		spec.kind,
		func(client ConfigurationAgent) ([]T, error) {
			return spec.agentCall(client, ctx, namespace)
		},
		func(direct DirectConfiguration) ([]T, error) {
			return spec.directCall(direct, ctx, clusterID, namespace)
		},
		agentFactory,
		directFactory,
		spec.namespaceOf,
		spec.actionsOf,
		spec.setActions,
		func(source string) string {
			return fmt.Sprintf("%s via %s in namespace %s", spec.auditText, source, displayNamespace(namespace))
		},
	)
}

func configMapNamespace(item domainresource.ConfigMapView) string { return item.Namespace }
func configMapActions(item domainresource.ConfigMapView) []string { return item.AllowedActions }
func setConfigMapActions(item *domainresource.ConfigMapView, actions []string) {
	item.AllowedActions = actions
}

func secretNamespace(item domainresource.SecretView) string { return item.Namespace }
func secretActions(item domainresource.SecretView) []string { return item.AllowedActions }
func setSecretActions(item *domainresource.SecretView, actions []string) {
	item.AllowedActions = actions
}

func (c *Configuration) directConfiguration() (DirectConfiguration, error) {
	if c.direct == nil {
		return nil, fmt.Errorf("%w: direct configuration adapter is not configured", apperrors.ErrClusterUnready)
	}
	return c.direct, nil
}

func listConfigurationResources[T any](ctx context.Context, access *resourceAccess, principal domainidentity.Principal, clusterID, namespace, kind string, agentCall func(ConfigurationAgent) ([]T, error), directCall func(DirectConfiguration) ([]T, error), agentFactory func(domaincluster.Connection) (ConfigurationAgent, error), directFactory func() (DirectConfiguration, error), namespaceOf func(T) string, actionsOf func(T) []string, setActions func(*T, []string), auditSummary func(string) string) ([]T, error) {
	request := resourceListRequest{clusterID: clusterID, namespace: namespace, kind: kind, summary: auditSummary}
	return listModeResources(ctx, access, principal, request,
		func(connection domaincluster.Connection) ([]T, string, error) {
			return configurationItems(connection, agentCall, directCall, agentFactory, directFactory)
		},
		namespaceOf, actionsOf, setActions,
	)
}

func getConfigurationDetail[T any](ctx context.Context, c *Configuration, principal domainidentity.Principal, clusterID, namespace, name, kind, auditText string, agentCall func(ConfigurationAgent) (T, error), directCall func(DirectConfiguration) (T, error), setActions func(*T, []string)) (T, error) {
	request := resourceDetailRequest{clusterID: clusterID, namespace: namespace, kind: kind, name: name, summary: func(source string) string { return fmt.Sprintf("%s via %s", auditText, source) }}
	return getModeResource(ctx, c.resourceAccess, principal, request, func(connection domaincluster.Connection) (T, string, error) {
		return routeModeValue(connection, c.configurationAgentClient, c.directConfiguration, agentCall, directCall)
	}, setActions)
}

func populateAllowedActions[T any](items []T, decision domainaccess.Decision, actionsOf func(T) []string, set func(*T, []string)) {
	actions := stringifyActions(decision.AllowedActions)
	for index := range items {
		if len(actionsOf(items[index])) == 0 {
			set(&items[index], actions)
		}
	}
}
