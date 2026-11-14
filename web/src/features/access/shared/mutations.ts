import { mutationOptions, type QueryClient } from '@tanstack/react-query'
import { permissionSnapshotQueryKey } from '@/features/auth'
import { accessApi } from './api'
import { accessKeys, accessMutationKeys } from './keys'

function invalidateKeys(queryClient: QueryClient, keys: readonly (readonly unknown[])[]) {
  return Promise.all(keys.map((queryKey) => queryClient.invalidateQueries({ queryKey })))
}

export async function invalidateAccessUsers(queryClient: QueryClient) {
  await invalidateKeys(queryClient, [accessKeys.users()])
  await queryClient.invalidateQueries({ queryKey: permissionSnapshotQueryKey })
}

export async function invalidateAccessRoles(queryClient: QueryClient) {
  await invalidateKeys(queryClient, [accessKeys.roles(), accessKeys.users()])
  await queryClient.invalidateQueries({ queryKey: permissionSnapshotQueryKey })
}

export async function invalidateAccessTeams(queryClient: QueryClient) {
  await invalidateKeys(queryClient, [
    accessKeys.teams(),
    accessKeys.users(),
    accessKeys.scopeGrants(),
  ])
  await queryClient.invalidateQueries({ queryKey: permissionSnapshotQueryKey })
}

export async function invalidateAccessPolicies(queryClient: QueryClient) {
  await invalidateKeys(queryClient, [accessKeys.policies()])
  await queryClient.invalidateQueries({ queryKey: permissionSnapshotQueryKey })
}

export async function invalidateAccessScopeGrants(queryClient: QueryClient) {
  await invalidateKeys(queryClient, [accessKeys.scopeGrants()])
  await queryClient.invalidateQueries({ queryKey: permissionSnapshotQueryKey })
}

export const accessMutations = {
  users: {
    create: () =>
      mutationOptions({
        mutationKey: accessMutationKeys.users('create'),
        mutationFn: accessApi.users.create,
      }),
    update: () =>
      mutationOptions({
        mutationKey: accessMutationKeys.users('update'),
        mutationFn: accessApi.users.update,
      }),
    delete: () =>
      mutationOptions({
        mutationKey: accessMutationKeys.users('delete'),
        mutationFn: accessApi.users.delete,
      }),
    resetMFA: () =>
      mutationOptions({
        mutationKey: accessMutationKeys.users('reset-mfa'),
        mutationFn: (variables: Parameters<typeof accessApi.users.resetMFA>[0]) =>
          accessApi.users.resetMFA(variables),
      }),
  },
  roles: {
    create: () =>
      mutationOptions({
        mutationKey: accessMutationKeys.roles('create'),
        mutationFn: accessApi.roles.create,
      }),
    update: () =>
      mutationOptions({
        mutationKey: accessMutationKeys.roles('update'),
        mutationFn: accessApi.roles.update,
      }),
    delete: () =>
      mutationOptions({
        mutationKey: accessMutationKeys.roles('delete'),
        mutationFn: accessApi.roles.delete,
      }),
  },
  teams: {
    create: () =>
      mutationOptions({
        mutationKey: accessMutationKeys.teams('create'),
        mutationFn: accessApi.teams.create,
      }),
    update: () =>
      mutationOptions({
        mutationKey: accessMutationKeys.teams('update'),
        mutationFn: accessApi.teams.update,
      }),
    delete: () =>
      mutationOptions({
        mutationKey: accessMutationKeys.teams('delete'),
        mutationFn: accessApi.teams.delete,
      }),
  },
  policies: {
    create: () =>
      mutationOptions({
        mutationKey: accessMutationKeys.policies('create'),
        mutationFn: accessApi.policies.create,
      }),
    update: () =>
      mutationOptions({
        mutationKey: accessMutationKeys.policies('update'),
        mutationFn: accessApi.policies.update,
      }),
    delete: () =>
      mutationOptions({
        mutationKey: accessMutationKeys.policies('delete'),
        mutationFn: accessApi.policies.delete,
      }),
  },
  scopeGrants: {
    create: () =>
      mutationOptions({
        mutationKey: accessMutationKeys.scopeGrants('create'),
        mutationFn: accessApi.scopeGrants.create,
      }),
    update: () =>
      mutationOptions({
        mutationKey: accessMutationKeys.scopeGrants('update'),
        mutationFn: accessApi.scopeGrants.update,
      }),
    delete: () =>
      mutationOptions({
        mutationKey: accessMutationKeys.scopeGrants('delete'),
        mutationFn: accessApi.scopeGrants.delete,
      }),
  },
}
