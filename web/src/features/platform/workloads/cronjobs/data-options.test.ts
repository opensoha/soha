import { MutationObserver, QueryClient } from '@tanstack/react-query'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { workloadKeys } from '@/features/platform/workloads/shared/keys'
import { cronJobMutations } from './mutations'
import { cronJobQueries } from './queries'

const apiMocks = vi.hoisted(() => ({
  deleteCronJob: vi.fn(),
  getCronJobDetail: vi.fn(),
  listCronJobEvents: vi.fn(),
  listCronJobs: vi.fn(),
  suspendCronJob: vi.fn(),
}))

vi.mock('./api', () => apiMocks)

const scope = { clusterId: 'cluster-a', namespace: 'team-a' }
const target = { scope, name: 'nightly' }

describe('cron job query and mutation options', () => {
  beforeEach(() => vi.clearAllMocks())

  it('uses canonical keys for details and events', () => {
    expect(cronJobQueries.list(scope).queryKey).toEqual(workloadKeys.list('cronjobs', scope))
    expect(cronJobQueries.detail(scope, 'nightly').queryKey).toEqual(
      workloadKeys.detail('cronjobs', scope, 'nightly'),
    )
    expect(cronJobQueries.events(scope, 'nightly').queryKey).toEqual(
      workloadKeys.events('cronjobs', scope, 'nightly'),
    )
  })

  it('invalidates CronJob and Job caches after suspend and delete', async () => {
    apiMocks.suspendCronJob.mockResolvedValueOnce({ suspend: true })
    apiMocks.deleteCronJob.mockResolvedValueOnce(undefined)
    const queryClient = new QueryClient()
    const invalidate = vi.spyOn(queryClient, 'invalidateQueries').mockResolvedValue()

    await expect(
      new MutationObserver(queryClient, cronJobMutations.suspend(queryClient)).mutate({
        ...target,
        suspend: true,
      }),
    ).resolves.toMatchObject({ suspend: true })
    await expect(
      new MutationObserver(queryClient, cronJobMutations.remove(queryClient)).mutate(target),
    ).resolves.toBeUndefined()

    expect(invalidate).toHaveBeenCalledWith({ queryKey: workloadKeys.lists('cronjobs') })
    expect(invalidate).toHaveBeenCalledWith({
      queryKey: workloadKeys.detail('cronjobs', scope, 'nightly'),
    })
    expect(invalidate).toHaveBeenCalledWith({ queryKey: workloadKeys.lists('jobs') })
  })
})
