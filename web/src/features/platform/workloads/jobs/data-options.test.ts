import { MutationObserver, QueryClient } from '@tanstack/react-query'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { workloadKeys } from '@/features/platform/workloads/shared/keys'
import { jobMutations } from './mutations'
import { jobQueries } from './queries'

const apiMocks = vi.hoisted(() => ({
  deleteJob: vi.fn(),
  getJobDetail: vi.fn(),
  listJobEvents: vi.fn(),
  listJobs: vi.fn(),
}))

vi.mock('./api', () => apiMocks)

const scope = { clusterId: 'cluster-a', namespace: 'team-a' }
const target = { scope, name: 'nightly' }

describe('job query and mutation options', () => {
  beforeEach(() => vi.clearAllMocks())

  it('uses canonical list, detail, and event keys', () => {
    expect(jobQueries.list(scope).queryKey).toEqual(workloadKeys.list('jobs', scope))
    expect(jobQueries.detail(scope, 'nightly').queryKey).toEqual(
      workloadKeys.detail('jobs', scope, 'nightly'),
    )
    expect(jobQueries.events(scope, 'nightly').queryKey).toEqual(
      workloadKeys.events('jobs', scope, 'nightly'),
    )
    expect(jobQueries.detail({ clusterId: 'cluster-a', namespace: null }, 'nightly').enabled).toBe(
      false,
    )
  })

  it('invalidates Job, Pod, and CronJob child caches after delete', async () => {
    apiMocks.deleteJob.mockResolvedValueOnce(undefined)
    const queryClient = new QueryClient()
    const invalidate = vi.spyOn(queryClient, 'invalidateQueries').mockResolvedValue()
    const observer = new MutationObserver(queryClient, jobMutations.remove(queryClient))

    await expect(observer.mutate(target)).resolves.toBeUndefined()
    expect(invalidate).toHaveBeenCalledWith({ queryKey: workloadKeys.lists('jobs') })
    expect(invalidate).toHaveBeenCalledWith({
      queryKey: workloadKeys.detail('jobs', scope, 'nightly'),
    })
    expect(invalidate).toHaveBeenCalledWith({ queryKey: workloadKeys.lists('pods') })
    expect(invalidate).toHaveBeenCalledWith({ queryKey: workloadKeys.details('cronjobs') })
  })
})
