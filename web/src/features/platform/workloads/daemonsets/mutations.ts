import { mutationOptions, type QueryClient } from '@tanstack/react-query'
import { workloadKeys } from '@/features/platform/workloads/shared/keys'
import { deleteDaemonSet, restartDaemonSet } from './api'
import type { DaemonSetTarget } from './types'

async function invalidateDaemonSetCaches(queryClient: QueryClient, target: DaemonSetTarget) {
  await Promise.all([
    queryClient.invalidateQueries({ queryKey: workloadKeys.lists('daemonsets') }),
    queryClient.invalidateQueries({
      queryKey: workloadKeys.detail('daemonsets', target.scope, target.name),
    }),
    queryClient.invalidateQueries({ queryKey: workloadKeys.lists('pods') }),
  ])
}

export const daemonSetMutations = {
  restart: (queryClient: QueryClient) =>
    mutationOptions({
      mutationKey: [...workloadKeys.resource('daemonsets'), 'restart'] as const,
      mutationFn: restartDaemonSet,
      onSuccess: (_data, variables) => invalidateDaemonSetCaches(queryClient, variables),
    }),
  remove: (queryClient: QueryClient) =>
    mutationOptions({
      mutationKey: [...workloadKeys.resource('daemonsets'), 'delete'] as const,
      mutationFn: deleteDaemonSet,
      onSuccess: (_data, variables) => invalidateDaemonSetCaches(queryClient, variables),
    }),
}
