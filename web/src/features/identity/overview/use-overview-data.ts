import { hasPermission, usePermissionSnapshot } from '@/features/auth'
import { systemIntegrationQueries, type SystemIntegrationCategory } from '@/features/settings'
import { readableQueryData } from '@/utils/query-feedback'
import { useQuery } from '@tanstack/react-query'
import { identityApplicationQueries } from '../applications'
import { identityOutpostQueries } from '../outposts'
import { identityProviderQueries } from '../providers'
import { identityRuntimeQueries } from '../runtime'
import { softwarePackageQueries } from '../software'
import { identityOverviewQueries } from './queries'

function hasAnyPermission(snapshot: Parameters<typeof hasPermission>[0], keys: string[]) {
  return keys.some((key) => hasPermission(snapshot, key))
}

export function useIdentityOverviewData() {
  const snapshot = usePermissionSnapshot().data?.data
  const permissions = {
    applications: hasPermission(snapshot, 'identity.applications.view'),
    providers: hasPermission(snapshot, 'identity.providers.view'),
    outposts: hasPermission(snapshot, 'identity.outposts.view'),
    sessions: hasPermission(snapshot, 'system.online-users.view'),
    audit: hasAnyPermission(snapshot, ['identity.audit.view', 'system.audit.view']),
    software: hasPermission(snapshot, 'software.package.view'),
    storageIntegrations: hasPermission(snapshot, 'settings.system-integrations.view'),
  }

  const applicationsQuery = useQuery({
    ...identityApplicationQueries.list({}),
    enabled: permissions.applications,
  })
  const providersQuery = useQuery({
    ...identityProviderQueries.list(),
    enabled: permissions.providers,
  })
  const outpostsQuery = useQuery({
    ...identityOutpostQueries.list(),
    enabled: permissions.outposts,
  })
  const sessionsQuery = useQuery({
    ...identityOverviewQueries.sessions(),
    enabled: permissions.sessions,
  })
  const auditQuery = useQuery({
    ...identityOverviewQueries.audit(),
    enabled: permissions.audit,
  })
  const softwareStorageQuery = useQuery({
    ...softwarePackageQueries.storage(),
    enabled: permissions.software,
  })
  const storageIntegrationsQuery = useQuery(
    systemIntegrationQueries.list(
      { category: 'storage' as SystemIntegrationCategory },
      permissions.software && permissions.storageIntegrations,
    ),
  )
  const runtimeQuery = useQuery(identityRuntimeQueries.capabilities())

  const refreshAll = () => {
    if (permissions.applications) void applicationsQuery.refetch()
    if (permissions.providers) void providersQuery.refetch()
    if (permissions.outposts) void outpostsQuery.refetch()
    if (permissions.sessions) void sessionsQuery.refetch()
    if (permissions.audit) void auditQuery.refetch()
    if (permissions.software) void softwareStorageQuery.refetch()
    if (permissions.software && permissions.storageIntegrations) {
      void storageIntegrationsQuery.refetch()
    }
    void runtimeQuery.refetch()
  }

  return {
    reads: {
      applications: applicationsQuery,
      providers: providersQuery,
      outposts: outpostsQuery,
      sessions: sessionsQuery,
      audits: auditQuery,
      softwareStorage: softwareStorageQuery,
      storageIntegrations: storageIntegrationsQuery,
      runtime: runtimeQuery,
    },
    applications:
      (applicationsQuery.isEnabled === false ? undefined : readableQueryData(applicationsQuery)) ??
      [],
    providers:
      (providersQuery.isEnabled === false ? undefined : readableQueryData(providersQuery)) ?? [],
    outposts:
      (outpostsQuery.isEnabled === false ? undefined : readableQueryData(outpostsQuery)) ?? [],
    sessions:
      (sessionsQuery.isEnabled === false ? undefined : readableQueryData(sessionsQuery)) ?? [],
    audits: (auditQuery.isEnabled === false ? undefined : readableQueryData(auditQuery)) ?? [],
    softwareStorage:
      softwareStorageQuery.isEnabled === false
        ? undefined
        : readableQueryData(softwareStorageQuery),
    storageIntegrations:
      (storageIntegrationsQuery.isEnabled === false
        ? undefined
        : readableQueryData(storageIntegrationsQuery)) ?? [],
    softwareError:
      softwareStorageQuery.isError ||
      (permissions.storageIntegrations && storageIntegrationsQuery.isError),
    runtime: runtimeQuery.isEnabled === false ? undefined : readableQueryData(runtimeQuery),
    loading: {
      applications: applicationsQuery.isLoading,
      providers: providersQuery.isLoading,
      outposts: outpostsQuery.isLoading,
      sessions: sessionsQuery.isLoading,
      software:
        softwareStorageQuery.isLoading ||
        (permissions.storageIntegrations && storageIntegrationsQuery.isLoading),
    },
    permissions,
    refreshAll,
  }
}
