import { mutationOptions, type QueryClient } from '@tanstack/react-query'
import { moduleStatusQueryKey } from '@/features/modules'
import { permissionSnapshotQueryKey } from '@/features/auth'
import { runtimeConfigurationApi } from './api'
import { runtimeConfigurationKeys, runtimeConfigurationMutationKeys } from './keys'

export function invalidateRuntimeConfiguration(queryClient: QueryClient) {
  return Promise.all([
    queryClient.invalidateQueries({ queryKey: runtimeConfigurationKeys.all }),
    queryClient.invalidateQueries({
      queryKey: moduleStatusQueryKey,
      refetchType: 'active',
    }),
    queryClient.invalidateQueries({
      queryKey: permissionSnapshotQueryKey,
      refetchType: 'active',
    }),
  ])
}

export const runtimeConfigurationMutations = {
  validate: () =>
    mutationOptions({
      mutationKey: runtimeConfigurationMutationKeys.validate(),
      mutationFn: runtimeConfigurationApi.validate,
    }),
  apply: (queryClient: QueryClient) =>
    mutationOptions({
      mutationKey: runtimeConfigurationMutationKeys.apply(),
      mutationFn: runtimeConfigurationApi.apply,
      onSuccess: () => invalidateRuntimeConfiguration(queryClient),
    }),
  rollback: (queryClient: QueryClient) =>
    mutationOptions({
      mutationKey: runtimeConfigurationMutationKeys.rollback(),
      mutationFn: runtimeConfigurationApi.rollback,
      onSuccess: () => invalidateRuntimeConfiguration(queryClient),
    }),
}
