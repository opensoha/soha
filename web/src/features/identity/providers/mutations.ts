import { mutationOptions, type QueryClient } from '@tanstack/react-query'
import {
  createIdentityProvider,
  deleteIdentityOIDCClient,
  deleteIdentityProvider,
  rotateIdentityProviderSAMLCertificate,
  rotateIdentityProviderSigningKey,
  updateIdentityOIDCClient,
  updateIdentityProvider,
} from './api'
import { identityProviderKeys, identityProviderMutationKeys } from './keys'

async function invalidateProviderLists(queryClient: QueryClient) {
  await queryClient.invalidateQueries({ queryKey: identityProviderKeys.lists() })
}

async function invalidateProvider(
  queryClient: QueryClient,
  providerId: string,
  includeLists = false,
) {
  await Promise.all([
    ...(includeLists ? [invalidateProviderLists(queryClient)] : []),
    queryClient.invalidateQueries({ queryKey: identityProviderKeys.detail(providerId) }),
  ])
}

export const identityProviderMutations = {
  create: (queryClient: QueryClient) =>
    mutationOptions({
      mutationKey: identityProviderMutationKeys.create,
      mutationFn: createIdentityProvider,
      onSuccess: () => invalidateProviderLists(queryClient),
    }),
  update: (queryClient: QueryClient) =>
    mutationOptions({
      mutationKey: identityProviderMutationKeys.update,
      mutationFn: updateIdentityProvider,
      onSuccess: (_provider, variables) =>
        invalidateProvider(queryClient, variables.providerId, true),
    }),
  remove: (queryClient: QueryClient) =>
    mutationOptions({
      mutationKey: identityProviderMutationKeys.remove,
      mutationFn: deleteIdentityProvider,
      onSuccess: (_result, providerId) => invalidateProvider(queryClient, providerId, true),
    }),
  updateOIDCClient: (queryClient: QueryClient) =>
    mutationOptions({
      mutationKey: identityProviderMutationKeys.updateOIDCClient,
      mutationFn: updateIdentityOIDCClient,
      onSuccess: (_client, variables) =>
        queryClient.invalidateQueries({
          queryKey: identityProviderKeys.oidcClients(variables.providerId),
        }),
    }),
  removeOIDCClient: (queryClient: QueryClient) =>
    mutationOptions({
      mutationKey: identityProviderMutationKeys.removeOIDCClient,
      mutationFn: deleteIdentityOIDCClient,
      onSuccess: (_result, variables) =>
        queryClient.invalidateQueries({
          queryKey: identityProviderKeys.oidcClients(variables.providerId),
        }),
    }),
  rotateSigningKey: (queryClient: QueryClient) =>
    mutationOptions({
      mutationKey: identityProviderMutationKeys.rotateSigningKey,
      mutationFn: rotateIdentityProviderSigningKey,
      onSuccess: (_key, providerId) => invalidateProvider(queryClient, providerId, true),
    }),
  rotateSAMLCertificate: (queryClient: QueryClient) =>
    mutationOptions({
      mutationKey: identityProviderMutationKeys.rotateSAMLCertificate,
      mutationFn: rotateIdentityProviderSAMLCertificate,
      onSuccess: (_rotation, variables) =>
        invalidateProvider(queryClient, variables.providerId, true),
    }),
}
