import { mutationOptions, type QueryClient } from '@tanstack/react-query'
import type { ResourceYAMLView } from '@/types'
import { storageKeys } from './keys'
import type {
  CreateStorageVariables,
  StorageKind,
  StorageTarget,
  UpdateStorageYAMLVariables,
} from './types'

type CreateStorageMutation = (variables: CreateStorageVariables) => Promise<ResourceYAMLView>
type UpdateStorageYAMLMutation = (
  variables: UpdateStorageYAMLVariables,
) => Promise<ResourceYAMLView>
type DeleteStorageMutation = (target: StorageTarget) => Promise<void>

async function invalidateStorageTarget(
  queryClient: QueryClient,
  kind: StorageKind,
  target: StorageTarget,
) {
  await Promise.all([
    queryClient.invalidateQueries({ queryKey: storageKeys.lists(kind) }),
    queryClient.invalidateQueries({
      queryKey: storageKeys.detail(kind, target.scope, target.name),
    }),
    queryClient.invalidateQueries({ queryKey: storageKeys.yaml(kind, target.scope, target.name) }),
  ])
}

export const storageMutations = {
  create: (kind: StorageKind, queryClient: QueryClient, mutationFn: CreateStorageMutation) =>
    mutationOptions({
      mutationKey: [...storageKeys.resource(kind), 'create'] as const,
      mutationFn,
      onSuccess: () => queryClient.invalidateQueries({ queryKey: storageKeys.lists(kind) }),
    }),
  updateYAML: (
    kind: StorageKind,
    queryClient: QueryClient,
    mutationFn: UpdateStorageYAMLMutation,
  ) =>
    mutationOptions({
      mutationKey: [...storageKeys.resource(kind), 'update-yaml'] as const,
      mutationFn,
      onSuccess: (_data, variables) => invalidateStorageTarget(queryClient, kind, variables),
    }),
  remove: (kind: StorageKind, queryClient: QueryClient, mutationFn: DeleteStorageMutation) =>
    mutationOptions({
      mutationKey: [...storageKeys.resource(kind), 'delete'] as const,
      mutationFn,
      onSuccess: (_data, variables) => invalidateStorageTarget(queryClient, kind, variables),
    }),
}
