import { mutationOptions, type QueryClient } from '@tanstack/react-query'
import {
  createIdentityOutpost,
  mutateIdentityOutpostRuntime,
  deleteIdentityOutpost,
  rotateIdentityOutpostToken,
  updateIdentityOutpost,
} from './api'
import type { OutpostRuntimeIntent } from './types'
import { identityOutpostKeys, identityOutpostMutationKeys } from './keys'

async function invalidateLists(queryClient: QueryClient) {
  await queryClient.invalidateQueries({ queryKey: identityOutpostKeys.lists() })
}

export const identityOutpostMutations = {
  runtime: (queryClient: QueryClient) =>
    mutationOptions({
      mutationKey: identityOutpostMutationKeys.runtime,
      mutationFn: mutateIdentityOutpostRuntime,
      gcTime: Infinity,
      retry: false,
      onMutate: (intent) => {
        queryClient
          .getMutationCache()
          .findAll({ mutationKey: identityOutpostMutationKeys.runtime })
          .forEach((item) => {
            if (
              item.state.status !== 'pending' &&
              (item.state.variables as OutpostRuntimeIntent | undefined)?.outpostId ===
                intent.outpostId &&
              (!(intent.kind === 'control' && intent.input.action === 'reconcile') ||
                ((item.state.variables as OutpostRuntimeIntent | undefined)?.kind === 'control' &&
                  (item.state.variables as Extract<OutpostRuntimeIntent, { kind: 'control' }>).input
                    .action === 'reconcile'))
            )
              queryClient.getMutationCache().remove(item)
          })
      },
      onSuccess: async (_result, intent) => {
        // An idempotent replay can return an older receipt; only the runtime read updates observations.
        await queryClient.invalidateQueries({
          queryKey: identityOutpostKeys.runtime(intent.outpostId),
        })
      },
      onSettled: (result, _error, intent) => {
        if (!result) return
        queryClient
          .getMutationCache()
          .findAll({ mutationKey: identityOutpostMutationKeys.runtime })
          .forEach((item) => {
            const previous = item.state.variables as OutpostRuntimeIntent | undefined
            if (
              previous?.outpostId === intent.outpostId &&
              previous.input.requestId === intent.input.requestId
            )
              queryClient.getMutationCache().remove(item)
          })
      },
    }),
  create: (queryClient: QueryClient) =>
    mutationOptions({
      mutationKey: identityOutpostMutationKeys.create,
      mutationFn: createIdentityOutpost,
      onSuccess: () => invalidateLists(queryClient),
    }),
  update: (queryClient: QueryClient) =>
    mutationOptions({
      mutationKey: identityOutpostMutationKeys.update,
      mutationFn: updateIdentityOutpost,
      onSuccess: async (_outpost, variables) => {
        await Promise.all([
          invalidateLists(queryClient),
          queryClient.invalidateQueries({
            queryKey: identityOutpostKeys.detail(variables.outpostId),
          }),
        ])
      },
    }),
  remove: (queryClient: QueryClient) =>
    mutationOptions({
      mutationKey: identityOutpostMutationKeys.remove,
      mutationFn: deleteIdentityOutpost,
      onSuccess: async (_result, outpostId) => {
        await Promise.all([
          invalidateLists(queryClient),
          queryClient.invalidateQueries({ queryKey: identityOutpostKeys.detail(outpostId) }),
        ])
      },
    }),
  rotateToken: (queryClient: QueryClient) =>
    mutationOptions({
      mutationKey: identityOutpostMutationKeys.rotateToken,
      mutationFn: rotateIdentityOutpostToken,
      onSuccess: (_outpost, outpostId) =>
        queryClient.invalidateQueries({ queryKey: identityOutpostKeys.detail(outpostId) }),
    }),
}
