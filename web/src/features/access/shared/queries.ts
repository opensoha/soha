import { queryOptions } from '@tanstack/react-query'
import { accessApi } from './api'
import { accessKeys } from './keys'
import type { AccessScopeGrantSubject } from './types'

export const accessQueries = {
  permissionCatalog: (enabled = true) =>
    queryOptions({
      queryKey: accessKeys.permissionCatalog(),
      queryFn: accessApi.permissions.catalog,
      enabled,
    }),
  users: (enabled = true) =>
    queryOptions({
      queryKey: accessKeys.userList(),
      queryFn: accessApi.users.list,
      enabled,
    }),
  roles: (enabled = true) =>
    queryOptions({
      queryKey: accessKeys.roleList(),
      queryFn: accessApi.roles.list,
      enabled,
    }),
  teams: (enabled = true) =>
    queryOptions({
      queryKey: accessKeys.teamList(),
      queryFn: accessApi.teams.list,
      enabled,
    }),
  policies: (enabled = true) =>
    queryOptions({
      queryKey: accessKeys.policyList(),
      queryFn: accessApi.policies.list,
      enabled,
    }),
  scopeGrantsAll: (enabled = true) =>
    queryOptions({
      queryKey: accessKeys.scopeGrantListAll(),
      queryFn: accessApi.scopeGrants.listAll,
      enabled,
    }),
  scopeGrants: (subject: AccessScopeGrantSubject, enabled = true) =>
    queryOptions({
      queryKey: accessKeys.scopeGrantList(subject),
      queryFn: () => accessApi.scopeGrants.list(subject),
      enabled,
    }),
  applicationOptions: (enabled = true) =>
    queryOptions({
      queryKey: accessKeys.applicationOptions(),
      queryFn: accessApi.dependencies.applications,
      enabled,
    }),
  applicationEnvironments: (enabled = true) =>
    queryOptions({
      queryKey: accessKeys.applicationEnvironments(),
      queryFn: accessApi.dependencies.applicationEnvironments,
      enabled,
    }),
  clusterOptions: (enabled = true) =>
    queryOptions({
      queryKey: accessKeys.clusterOptions(),
      queryFn: accessApi.dependencies.clusters,
      enabled,
    }),
  loginProviders: (enabled = true) =>
    queryOptions({
      queryKey: accessKeys.loginProviders(),
      queryFn: accessApi.dependencies.loginProviders,
      enabled,
      retry: false,
    }),
}
