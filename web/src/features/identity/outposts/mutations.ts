import { mutationOptions, type QueryClient } from '@tanstack/react-query'
import {
  createIdentityOutpost,
  deleteIdentityOutpost,
  rotateIdentityOutpostToken,
  updateIdentityOutpost,
} from './api'
import { identityOutpostKeys, identityOutpostMutationKeys } from './keys'

async function invalidateLists(queryClient: QueryClient) {
  await queryClient.invalidateQueries({ queryKey: identityOutpostKeys.lists() })
}

export const identityOutpostMutations = {
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
