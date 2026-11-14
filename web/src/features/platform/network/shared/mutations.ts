import { mutationOptions, type QueryClient } from '@tanstack/react-query'
import { deleteNetworkResource, updateNetworkYAML } from './api'
import { networkKeys } from './keys'
import type { NetworkKind, NetworkTarget, UpdateNetworkYAMLVariables } from './types'

async function invalidateNetworkCaches(
  queryClient: QueryClient,
  kind: NetworkKind,
  target: NetworkTarget,
) {
  await Promise.all([
    queryClient.invalidateQueries({ queryKey: networkKeys.lists(kind) }),
    queryClient.invalidateQueries({
      queryKey: networkKeys.detail(kind, target.scope, target.name),
    }),
  ])
}

export const networkMutations = {
  remove: (kind: NetworkKind, queryClient: QueryClient) =>
    mutationOptions({
      mutationKey: [...networkKeys.resource(kind), 'delete'] as const,
      mutationFn: (target: NetworkTarget) => deleteNetworkResource(kind, target),
      onSuccess: (_data, target) => invalidateNetworkCaches(queryClient, kind, target),
    }),
  updateYAML: (kind: NetworkKind, queryClient: QueryClient, beforeUpdate?: () => Promise<void>) =>
    mutationOptions({
      mutationKey: [...networkKeys.resource(kind), 'update-yaml'] as const,
      mutationFn: async (variables: UpdateNetworkYAMLVariables) => {
        await beforeUpdate?.()
        return updateNetworkYAML(kind, variables)
      },
      onSuccess: (yaml, variables) => {
        queryClient.setQueryData(networkKeys.yaml(kind, variables.scope, variables.name), yaml)
        return invalidateNetworkCaches(queryClient, kind, variables)
      },
    }),
}
