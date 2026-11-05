import { mutationOptions, type QueryClient } from '@tanstack/react-query'
import { workloadKeys } from '@/features/platform/workloads/shared/keys'
import { deleteCronJob, suspendCronJob } from './api'
import type { CronJobTarget, SuspendCronJobVariables } from './types'

async function invalidateCronJobCaches(queryClient: QueryClient, target: CronJobTarget) {
  await Promise.all([
    queryClient.invalidateQueries({ queryKey: workloadKeys.lists('cronjobs') }),
    queryClient.invalidateQueries({
      queryKey: workloadKeys.detail('cronjobs', target.scope, target.name),
    }),
    queryClient.invalidateQueries({ queryKey: workloadKeys.lists('jobs') }),
  ])
}

export const cronJobMutations = {
  suspendDetail: (queryClient: QueryClient) =>
    mutationOptions<void, Error, SuspendCronJobVariables>({
      mutationFn: async (target) => {
        await suspendCronJob(target)
      },
      onSuccess: (_data, target) => invalidateCronJobCaches(queryClient, target),
    }),
  suspend: (queryClient: QueryClient) =>
    mutationOptions<CronJobDetailResult, Error, SuspendCronJobVariables>({
      mutationKey: [...workloadKeys.resource('cronjobs'), 'suspend'] as const,
      mutationFn: suspendCronJob,
      onSuccess: (_data, variables) => invalidateCronJobCaches(queryClient, variables),
    }),
  remove: (queryClient: QueryClient) =>
    mutationOptions({
      mutationKey: [...workloadKeys.resource('cronjobs'), 'delete'] as const,
      mutationFn: deleteCronJob,
      onSuccess: (_data, variables) => invalidateCronJobCaches(queryClient, variables),
    }),
}

type CronJobDetailResult = Awaited<ReturnType<typeof suspendCronJob>>
