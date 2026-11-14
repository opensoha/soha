import { mutationOptions, type QueryClient } from '@tanstack/react-query'
import { systemIntegrationsApi } from './api'
import { systemIntegrationKeys, systemIntegrationMutationKeys } from './keys'

export function invalidateSystemIntegrations(queryClient: QueryClient) {
  return queryClient.invalidateQueries({ queryKey: systemIntegrationKeys.all })
}

export const systemIntegrationMutations = {
  create: (queryClient: QueryClient) =>
    mutationOptions({
      mutationKey: systemIntegrationMutationKeys.create(),
      mutationFn: systemIntegrationsApi.create,
      onSuccess: () => invalidateSystemIntegrations(queryClient),
    }),
  update: (queryClient: QueryClient) =>
    mutationOptions({
      mutationKey: systemIntegrationMutationKeys.update(),
      mutationFn: systemIntegrationsApi.update,
      onSuccess: () => invalidateSystemIntegrations(queryClient),
    }),
  remove: (queryClient: QueryClient) =>
    mutationOptions({
      mutationKey: systemIntegrationMutationKeys.remove(),
      mutationFn: systemIntegrationsApi.remove,
      onSuccess: () => invalidateSystemIntegrations(queryClient),
    }),
  test: (queryClient: QueryClient) =>
    mutationOptions({
      mutationKey: systemIntegrationMutationKeys.test(),
      mutationFn: systemIntegrationsApi.test,
      onSettled: () => invalidateSystemIntegrations(queryClient),
    }),
  authorizeOAuth: () =>
    mutationOptions({
      mutationKey: systemIntegrationMutationKeys.authorizeOAuth(),
      mutationFn: systemIntegrationsApi.authorizeOAuth,
    }),
}
