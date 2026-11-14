import { mutationOptions, type QueryClient } from '@tanstack/react-query'
import {
  createSecret,
  generateSSHKey,
  disableSecret,
  revokeSecretVersion,
  rotateSecret,
  updateSecret,
} from './api'
import { secretKeys } from './keys'

function invalidateSecret(queryClient: QueryClient, secretId?: string) {
  return Promise.all([
    queryClient.invalidateQueries({ queryKey: secretKeys.lists() }),
    ...(secretId
      ? [
          queryClient.invalidateQueries({ queryKey: secretKeys.detail(secretId) }),
          queryClient.invalidateQueries({ queryKey: secretKeys.versions(secretId) }),
        ]
      : []),
  ])
}

export const secretMutations = {
  generateSSHKey: (queryClient: QueryClient) =>
    mutationOptions({
      mutationKey: [...secretKeys.all, 'generate-ssh-key'] as const,
      mutationFn: generateSSHKey,
      onSuccess: () => invalidateSecret(queryClient),
    }),
  create: (queryClient: QueryClient) =>
    mutationOptions({
      mutationKey: [...secretKeys.all, 'create'] as const,
      mutationFn: createSecret,
      onSuccess: () => invalidateSecret(queryClient),
    }),
  update: (queryClient: QueryClient) =>
    mutationOptions({
      mutationKey: [...secretKeys.all, 'update'] as const,
      mutationFn: updateSecret,
      onSuccess: (_secret, variables) => invalidateSecret(queryClient, variables.secretId),
    }),
  disable: (queryClient: QueryClient) =>
    mutationOptions({
      mutationKey: [...secretKeys.all, 'disable'] as const,
      mutationFn: disableSecret,
      onSuccess: (_secret, secretId) => invalidateSecret(queryClient, secretId),
    }),
  rotate: (queryClient: QueryClient) =>
    mutationOptions({
      mutationKey: [...secretKeys.all, 'rotate'] as const,
      mutationFn: rotateSecret,
      onSuccess: (_version, variables) => invalidateSecret(queryClient, variables.secretId),
    }),
  revokeVersion: (queryClient: QueryClient) =>
    mutationOptions({
      mutationKey: [...secretKeys.all, 'version', 'revoke'] as const,
      mutationFn: revokeSecretVersion,
      onSuccess: (_version, variables) => invalidateSecret(queryClient, variables.secretId),
    }),
}
