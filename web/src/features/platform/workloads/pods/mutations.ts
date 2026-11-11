import { mutationOptions, type QueryClient } from '@tanstack/react-query'
import { workloadKeys } from '../shared/keys'
import { deletePod } from './api'
import type { BatchDeletePodsResult, BatchDeletePodsVariables, PodTarget } from './types'

export const POD_BATCH_DELETE_CONCURRENCY = 8

export async function mapSettledWithConcurrency<T, R>(
  items: readonly T[],
  concurrency: number,
  task: (item: T) => Promise<R>,
) {
  const results = new Array<PromiseSettledResult<R>>(items.length)
  const workerCount = Math.min(Math.max(1, concurrency), items.length)
  let nextIndex = 0

  await Promise.all(
    Array.from({ length: workerCount }, async () => {
      while (nextIndex < items.length) {
        const currentIndex = nextIndex
        nextIndex += 1
        try {
          results[currentIndex] = {
            status: 'fulfilled',
            value: await task(items[currentIndex]),
          }
        } catch (reason) {
          results[currentIndex] = { status: 'rejected', reason }
        }
      }
    }),
  )

  return results
}

export function deletePodsWithConcurrency({
  targets,
}: BatchDeletePodsVariables): Promise<BatchDeletePodsResult> {
  return mapSettledWithConcurrency(targets, POD_BATCH_DELETE_CONCURRENCY, deletePod)
}

async function invalidatePodCaches(queryClient: QueryClient, target?: PodTarget) {
  await Promise.all([
    queryClient.invalidateQueries({ queryKey: workloadKeys.lists('pods') }),
    ...(target
      ? [
          queryClient.invalidateQueries({
            queryKey: workloadKeys.detail('pods', target.scope, target.name),
          }),
        ]
      : []),
  ])
}

export const podMutations = {
  remove: (queryClient: QueryClient) =>
    mutationOptions({
      mutationKey: [...workloadKeys.resource('pods'), 'delete'] as const,
      mutationFn: deletePod,
      onSuccess: (_data, variables) => invalidatePodCaches(queryClient, variables),
    }),
  removeBatch: (queryClient: QueryClient) =>
    mutationOptions<BatchDeletePodsResult, Error, BatchDeletePodsVariables>({
      mutationKey: [...workloadKeys.resource('pods'), 'batch-delete'] as const,
      mutationFn: deletePodsWithConcurrency,
      onSuccess: () => invalidatePodCaches(queryClient),
    }),
}
