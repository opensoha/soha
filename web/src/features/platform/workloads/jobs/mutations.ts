import { mutationOptions, type QueryClient } from '@tanstack/react-query'
import { workloadKeys } from '@/features/platform/workloads/shared/keys'
import { deleteJob } from './api'
import type { JobTarget } from './types'

async function invalidateJobCaches(queryClient: QueryClient, target: JobTarget) {
  await Promise.all([
    queryClient.invalidateQueries({ queryKey: workloadKeys.lists('jobs') }),
    queryClient.invalidateQueries({
      queryKey: workloadKeys.detail('jobs', target.scope, target.name),
    }),
    queryClient.invalidateQueries({ queryKey: workloadKeys.lists('pods') }),
    queryClient.invalidateQueries({ queryKey: workloadKeys.details('cronjobs') }),
  ])
}

export const jobMutations = {
  remove: (queryClient: QueryClient) =>
    mutationOptions({
      mutationKey: [...workloadKeys.resource('jobs'), 'delete'] as const,
      mutationFn: deleteJob,
      onSuccess: (_data, variables) => invalidateJobCaches(queryClient, variables),
    }),
}
