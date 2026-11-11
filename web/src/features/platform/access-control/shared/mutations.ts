import { mutationOptions, type QueryClient } from '@tanstack/react-query'
import {
  createAccessControlResource,
  deleteAccessControlResource,
  updateAccessControlYAML,
} from './api'
import { accessControlKeys } from './keys'
import type {
  AccessControlKind,
  AccessControlTarget,
  CreateAccessControlVariables,
  UpdateAccessControlYAMLVariables,
} from './types'

async function invalidateAccessControlCaches(
  queryClient: QueryClient,
  kind: AccessControlKind,
  target?: AccessControlTarget,
) {
  await Promise.all([
    queryClient.invalidateQueries({ queryKey: accessControlKeys.lists(kind) }),
    ...(target
      ? [
          queryClient.invalidateQueries({
            queryKey: accessControlKeys.detail(kind, target.scope, target.name),
          }),
          queryClient.invalidateQueries({
            queryKey: accessControlKeys.yaml(kind, target.scope, target.name),
          }),
        ]
      : []),
  ])
}

export const accessControlMutations = {
  create: (kind: AccessControlKind, queryClient: QueryClient) =>
    mutationOptions({
      mutationKey: [...accessControlKeys.resource(kind), 'create'] as const,
      mutationFn: (variables: CreateAccessControlVariables) =>
        createAccessControlResource(kind, variables),
      onSuccess: () => invalidateAccessControlCaches(queryClient, kind),
    }),
  remove: (kind: AccessControlKind, queryClient: QueryClient) =>
    mutationOptions({
      mutationKey: [...accessControlKeys.resource(kind), 'delete'] as const,
      mutationFn: (target: AccessControlTarget) => deleteAccessControlResource(kind, target),
      onSuccess: (_data, target) => invalidateAccessControlCaches(queryClient, kind, target),
    }),
  updateYAML: (kind: AccessControlKind, queryClient: QueryClient) =>
    mutationOptions({
      mutationKey: [...accessControlKeys.resource(kind), 'update-yaml'] as const,
      mutationFn: (variables: UpdateAccessControlYAMLVariables) =>
        updateAccessControlYAML(kind, variables),
      onSuccess: (yaml, variables) => {
        queryClient.setQueryData(
          accessControlKeys.yaml(kind, variables.scope, variables.name),
          yaml,
        )
        return invalidateAccessControlCaches(queryClient, kind, variables)
      },
    }),
}
