package resource

import (
	"context"
	"fmt"

	domaincluster "github.com/opensoha/soha/internal/domain/cluster"
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

func (n *Network) GetIngressClassDetail(ctx context.Context, principal domainidentity.Principal, clusterID, name string) (domainresource.IngressClassDetailView, error) {
	request := resourceDetailRequest{
		clusterID: clusterID, kind: "IngressClass", name: name,
		summary: func(source string) string { return fmt.Sprintf("viewed ingressclass detail via %s", source) },
	}
	return getModeResource(ctx, n.resourceAccess, principal, request,
		func(connection domaincluster.Connection) (domainresource.IngressClassDetailView, string, error) {
			return routeModeValue(connection, n.configurationAgentClient, n.directConfiguration,
				func(client ConfigurationAgent) (domainresource.IngressClassDetailView, error) {
					return client.GetIngressClassDetail(ctx, name)
				},
				func(direct DirectConfiguration) (domainresource.IngressClassDetailView, error) {
					return direct.GetIngressClassDetail(ctx, clusterID, name)
				},
			)
		},
		func(item *domainresource.IngressClassDetailView, actions []string) {
			item.AllowedActions = actions
			item.Ingresses = filterIngressClassRelations(ctx, n.resourceAccess, principal, clusterID, item.Ingresses)
		},
	)
}

func filterIngressClassRelations(ctx context.Context, access *resourceAccess, principal domainidentity.Principal, clusterID string, items []domainresource.IngressView) []domainresource.IngressView {
	decisions := make(map[string]bool)
	return filterRelatedItems(items, func(item domainresource.IngressView) bool {
		allowed, ok := decisions[item.Namespace]
		if !ok {
			allowed = canViewRelatedResource(ctx, access, principal, clusterID, item.Namespace, "Ingress")
			decisions[item.Namespace] = allowed
		}
		return allowed
	})
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

func (c *Configuration) GetMutatingWebhookConfigurationDetail(ctx context.Context, principal domainidentity.Principal, clusterID, name string) (domainresource.AdmissionWebhookConfigurationDetailView, error) {
	return getConfigurationDetail(ctx, c, principal, clusterID, "", name, "MutatingWebhookConfiguration", "viewed mutating webhook configuration detail",
		func(client ConfigurationAgent) (domainresource.AdmissionWebhookConfigurationDetailView, error) {
			return client.GetMutatingWebhookConfigurationDetail(ctx, name)
		},
		func(direct DirectConfiguration) (domainresource.AdmissionWebhookConfigurationDetailView, error) {
			return direct.GetMutatingWebhookConfigurationDetail(ctx, clusterID, name)
		},
		func(*domainresource.AdmissionWebhookConfigurationDetailView, []string) {})
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

func (c *Configuration) GetValidatingWebhookConfigurationDetail(ctx context.Context, principal domainidentity.Principal, clusterID, name string) (domainresource.AdmissionWebhookConfigurationDetailView, error) {
	return getConfigurationDetail(ctx, c, principal, clusterID, "", name, "ValidatingWebhookConfiguration", "viewed validating webhook configuration detail",
		func(client ConfigurationAgent) (domainresource.AdmissionWebhookConfigurationDetailView, error) {
			return client.GetValidatingWebhookConfigurationDetail(ctx, name)
		},
		func(direct DirectConfiguration) (domainresource.AdmissionWebhookConfigurationDetailView, error) {
			return direct.GetValidatingWebhookConfigurationDetail(ctx, clusterID, name)
		},
		func(*domainresource.AdmissionWebhookConfigurationDetailView, []string) {})
}

func (n *Network) directConfiguration() (DirectConfiguration, error) {
	if n.directConfig == nil {
		return nil, fmt.Errorf("%w: direct configuration adapter is not configured", apperrors.ErrClusterUnready)
	}
	return n.directConfig, nil
}
