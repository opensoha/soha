import { mutationOptions, type QueryClient } from '@tanstack/react-query'
import { workloadKeys } from '@/features/platform/workloads/shared/keys'
import { deleteStatefulSet, restartStatefulSet, scaleStatefulSet } from './api'
import type { ScaleStatefulSetVariables, StatefulSetTarget } from './types'

async function invalidateStatefulSetCaches(queryClient: QueryClient, target: StatefulSetTarget) {
  await Promise.all([
    queryClient.invalidateQueries({ queryKey: workloadKeys.lists('statefulsets') }),
    queryClient.invalidateQueries({
      queryKey: workloadKeys.detail('statefulsets', target.scope, target.name),
    }),
    queryClient.invalidateQueries({ queryKey: workloadKeys.lists('pods') }),
  ])
}

export const statefulSetMutations = {
  restart: (queryClient: QueryClient) =>
    mutationOptions({
      mutationKey: [...workloadKeys.resource('statefulsets'), 'restart'] as const,
      mutationFn: restartStatefulSet,
      onSuccess: (_data, variables) => invalidateStatefulSetCaches(queryClient, variables),
    }),
  scale: (queryClient: QueryClient) =>
    mutationOptions<void, Error, ScaleStatefulSetVariables>({
      mutationKey: [...workloadKeys.resource('statefulsets'), 'scale'] as const,
      mutationFn: scaleStatefulSet,
      onSuccess: (_data, variables) => invalidateStatefulSetCaches(queryClient, variables),
    }),
  remove: (queryClient: QueryClient) =>
    mutationOptions({
      mutationKey: [...workloadKeys.resource('statefulsets'), 'delete'] as const,
      mutationFn: deleteStatefulSet,
      onSuccess: (_data, variables) => invalidateStatefulSetCaches(queryClient, variables),
    }),
}
