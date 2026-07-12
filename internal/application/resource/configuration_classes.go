package resource

import (
	"context"
	"fmt"

	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainresource "github.com/opensoha/soha/internal/domain/resource"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

func (n *Network) ListIngressClasses(ctx context.Context, principal domainidentity.Principal, clusterID string) ([]domainresource.IngressClassView, error) {
	return listConfigurationResources(ctx, n.resourceAccess, principal, clusterID, "", "IngressClass",
		func(client ConfigurationAgent) ([]domainresource.IngressClassView, error) {
			return client.ListIngressClasses(ctx)
		},
		func(direct DirectConfiguration) ([]domainresource.IngressClassView, error) {
			return direct.ListIngressClasses(ctx, clusterID)
		},
		n.configurationAgentClient, n.directConfiguration, nil,
		func(item domainresource.IngressClassView) []string { return item.AllowedActions },
		func(item *domainresource.IngressClassView, actions []string) { item.AllowedActions = actions },
		func(source string) string { return fmt.Sprintf("listed ingressclasses via %s", source) },
	)
}

func (c *Configuration) ListPriorityClasses(ctx context.Context, principal domainidentity.Principal, clusterID string) ([]domainresource.PriorityClassView, error) {
	return listConfigurationResources(ctx, c.resourceAccess, principal, clusterID, "", "PriorityClass",
		func(client ConfigurationAgent) ([]domainresource.PriorityClassView, error) {
			return client.ListPriorityClasses(ctx)
		},
		func(direct DirectConfiguration) ([]domainresource.PriorityClassView, error) {
			return direct.ListPriorityClasses(ctx, clusterID)
		},
		c.configurationAgentClient, c.directConfiguration, nil,
		func(item domainresource.PriorityClassView) []string { return item.AllowedActions },
		func(item *domainresource.PriorityClassView, actions []string) { item.AllowedActions = actions },
		func(source string) string { return fmt.Sprintf("listed priorityclasses via %s", source) },
	)
}

func (c *Configuration) ListRuntimeClasses(ctx context.Context, principal domainidentity.Principal, clusterID string) ([]domainresource.RuntimeClassView, error) {
	return listConfigurationResources(ctx, c.resourceAccess, principal, clusterID, "", "RuntimeClass",
		func(client ConfigurationAgent) ([]domainresource.RuntimeClassView, error) {
			return client.ListRuntimeClasses(ctx)
		},
		func(direct DirectConfiguration) ([]domainresource.RuntimeClassView, error) {
			return direct.ListRuntimeClasses(ctx, clusterID)
		},
		c.configurationAgentClient, c.directConfiguration, nil,
		func(item domainresource.RuntimeClassView) []string { return item.AllowedActions },
		func(item *domainresource.RuntimeClassView, actions []string) { item.AllowedActions = actions },
		func(source string) string { return fmt.Sprintf("listed runtimeclasses via %s", source) },
	)
}

func (c *Configuration) ListMutatingWebhookConfigurations(ctx context.Context, principal domainidentity.Principal, clusterID string) ([]domainresource.MutatingWebhookConfigurationView, error) {
	return listConfigurationResources(ctx, c.resourceAccess, principal, clusterID, "", "MutatingWebhookConfiguration",
		func(client ConfigurationAgent) ([]domainresource.MutatingWebhookConfigurationView, error) {
			return client.ListMutatingWebhookConfigurations(ctx)
		},
		func(direct DirectConfiguration) ([]domainresource.MutatingWebhookConfigurationView, error) {
			return direct.ListMutatingWebhookConfigurations(ctx, clusterID)
		},
		c.configurationAgentClient, c.directConfiguration, nil,
		func(item domainresource.MutatingWebhookConfigurationView) []string { return item.AllowedActions },
		func(item *domainresource.MutatingWebhookConfigurationView, actions []string) {
			item.AllowedActions = actions
		},
		func(source string) string { return fmt.Sprintf("listed mutatingwebhookconfigurations via %s", source) },
	)
}

func (c *Configuration) ListValidatingWebhookConfigurations(ctx context.Context, principal domainidentity.Principal, clusterID string) ([]domainresource.ValidatingWebhookConfigurationView, error) {
	return listConfigurationResources(ctx, c.resourceAccess, principal, clusterID, "", "ValidatingWebhookConfiguration",
		func(client ConfigurationAgent) ([]domainresource.ValidatingWebhookConfigurationView, error) {
			return client.ListValidatingWebhookConfigurations(ctx)
		},
		func(direct DirectConfiguration) ([]domainresource.ValidatingWebhookConfigurationView, error) {
			return direct.ListValidatingWebhookConfigurations(ctx, clusterID)
		},
		c.configurationAgentClient, c.directConfiguration, nil,
		func(item domainresource.ValidatingWebhookConfigurationView) []string { return item.AllowedActions },
		func(item *domainresource.ValidatingWebhookConfigurationView, actions []string) {
			item.AllowedActions = actions
		},
		func(source string) string {
			return fmt.Sprintf("listed validatingwebhookconfigurations via %s", source)
		},
	)
}

func (n *Network) directConfiguration() (DirectConfiguration, error) {
	if n.directConfig == nil {
		return nil, fmt.Errorf("%w: direct configuration adapter is not configured", apperrors.ErrClusterUnready)
	}
	return n.directConfig, nil
}
