import { mutationOptions, type QueryClient } from '@tanstack/react-query'
import { providerPortalKeys } from '@/features/provider-portal'
import { identityProviderKeys } from '@/features/identity/providers'
import {
  createIdentityApplication,
  onboardIdentityApplication,
  deleteIdentityApplication,
  updateIdentityApplication,
} from './api'
import { identityApplicationKeys, identityApplicationMutationKeys } from './keys'

async function invalidateApplicationCaches(queryClient: QueryClient) {
  await Promise.all([
    queryClient.invalidateQueries({ queryKey: identityApplicationKeys.all }),
    queryClient.invalidateQueries({ queryKey: providerPortalKeys.all }),
  ])
}

export const identityApplicationMutations = {
  onboard: (queryClient: QueryClient) =>
    mutationOptions({
      mutationKey: identityApplicationMutationKeys.onboard,
      mutationFn: onboardIdentityApplication,
      gcTime: 0,
      onSuccess: async () => {
        await invalidateApplicationCaches(queryClient)
        await queryClient.invalidateQueries({ queryKey: identityProviderKeys.all })
      },
    }),
  create: (queryClient: QueryClient) =>
    mutationOptions({
      mutationKey: identityApplicationMutationKeys.create,
      mutationFn: createIdentityApplication,
      onSuccess: () => invalidateApplicationCaches(queryClient),
    }),
  update: (queryClient: QueryClient) =>
    mutationOptions({
      mutationKey: identityApplicationMutationKeys.update,
      mutationFn: updateIdentityApplication,
      onSuccess: () => invalidateApplicationCaches(queryClient),
    }),
  remove: (queryClient: QueryClient) =>
    mutationOptions({
      mutationKey: identityApplicationMutationKeys.remove,
      mutationFn: deleteIdentityApplication,
      onSuccess: () => invalidateApplicationCaches(queryClient),
    }),
}
