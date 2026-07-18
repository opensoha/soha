package resource

import (
	"context"
	"fmt"
	"strings"

	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainresource "github.com/opensoha/soha/internal/domain/resource"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

var resourceQuotaListSpec = newConfigurationListSpec(
	"ResourceQuota",
	"listed resourcequotas",
	ConfigurationAgent.ListResourceQuotas,
	DirectConfiguration.ListResourceQuotas,
	resourceQuotaNamespace,
	resourceQuotaActions,
	setResourceQuotaActions,
)

func (c *Configuration) ListResourceQuotas(ctx context.Context, principal domainidentity.Principal, clusterID, namespace string) ([]domainresource.ResourceQuotaView, error) {
	return listConfigurationCapability(ctx, c.resourceAccess, principal, clusterID, namespace, c.configurationAgentClient, c.directConfiguration, resourceQuotaListSpec)
}

func (c *Configuration) GetResourceQuotaDetail(ctx context.Context, principal domainidentity.Principal, clusterID, namespace, name string) (domainresource.ResourceQuotaDetailView, error) {
	if strings.TrimSpace(namespace) == "" {
		return domainresource.ResourceQuotaDetailView{}, fmt.Errorf("%w: namespace is required for resourcequota detail", apperrors.ErrInvalidArgument)
	}
	return getConfigurationDetail(ctx, c, principal, clusterID, namespace, name, "ResourceQuota", "viewed resourcequota detail",
		func(client ConfigurationAgent) (domainresource.ResourceQuotaDetailView, error) {
			return client.GetResourceQuotaDetail(ctx, namespace, name)
		},
		func(direct DirectConfiguration) (domainresource.ResourceQuotaDetailView, error) {
			return direct.GetResourceQuotaDetail(ctx, clusterID, namespace, name)
		},
		func(item *domainresource.ResourceQuotaDetailView, actions []string) { item.AllowedActions = actions })
}

var limitRangeListSpec = newConfigurationListSpec(
	"LimitRange",
	"listed limitranges",
	ConfigurationAgent.ListLimitRanges,
	DirectConfiguration.ListLimitRanges,
	limitRangeNamespace,
	limitRangeActions,
	setLimitRangeActions,
)

func (c *Configuration) ListLimitRanges(ctx context.Context, principal domainidentity.Principal, clusterID, namespace string) ([]domainresource.LimitRangeView, error) {
	return listConfigurationCapability(ctx, c.resourceAccess, principal, clusterID, namespace, c.configurationAgentClient, c.directConfiguration, limitRangeListSpec)
}

func (c *Configuration) GetLimitRangeDetail(ctx context.Context, principal domainidentity.Principal, clusterID, namespace, name string) (domainresource.LimitRangeDetailView, error) {
	if strings.TrimSpace(namespace) == "" {
		return domainresource.LimitRangeDetailView{}, fmt.Errorf("%w: namespace is required for limitrange detail", apperrors.ErrInvalidArgument)
	}
	return getConfigurationDetail(ctx, c, principal, clusterID, namespace, name, "LimitRange", "viewed limitrange detail",
		func(client ConfigurationAgent) (domainresource.LimitRangeDetailView, error) {
			return client.GetLimitRangeDetail(ctx, namespace, name)
		},
		func(direct DirectConfiguration) (domainresource.LimitRangeDetailView, error) {
			return direct.GetLimitRangeDetail(ctx, clusterID, namespace, name)
		},
		func(item *domainresource.LimitRangeDetailView, actions []string) { item.AllowedActions = actions })
}

var leaseListSpec = newConfigurationListSpec(
	"Lease",
	"listed leases",
	ConfigurationAgent.ListLeases,
	DirectConfiguration.ListLeases,
	leaseNamespace,
	leaseActions,
	setLeaseActions,
)

func (c *Configuration) ListLeases(ctx context.Context, principal domainidentity.Principal, clusterID, namespace string) ([]domainresource.LeaseView, error) {
	return listConfigurationCapability(ctx, c.resourceAccess, principal, clusterID, namespace, c.configurationAgentClient, c.directConfiguration, leaseListSpec)
}

func resourceQuotaNamespace(item domainresource.ResourceQuotaView) string { return item.Namespace }
func resourceQuotaActions(item domainresource.ResourceQuotaView) []string { return item.AllowedActions }
func setResourceQuotaActions(item *domainresource.ResourceQuotaView, actions []string) {
	item.AllowedActions = actions
}

func limitRangeNamespace(item domainresource.LimitRangeView) string { return item.Namespace }
func limitRangeActions(item domainresource.LimitRangeView) []string { return item.AllowedActions }
func setLimitRangeActions(item *domainresource.LimitRangeView, actions []string) {
	item.AllowedActions = actions
}

func leaseNamespace(item domainresource.LeaseView) string { return item.Namespace }
func leaseActions(item domainresource.LeaseView) []string { return item.AllowedActions }
func setLeaseActions(item *domainresource.LeaseView, actions []string) {
	item.AllowedActions = actions
}
