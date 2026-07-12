package resource

import (
	"context"

	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainresource "github.com/opensoha/soha/internal/domain/resource"
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
