import { beforeEach, describe, expect, it, vi } from 'vitest'
import {
  deleteCronJob,
  getCronJobDetail,
  listCronJobEvents,
  listCronJobs,
  suspendCronJob,
} from './api'

const apiMocks = vi.hoisted(() => ({
  delete: vi.fn(),
  get: vi.fn(),
  post: vi.fn(),
  put: vi.fn(),
}))

vi.mock('@/services/api-client', () => ({ api: apiMocks }))

const scope = { clusterId: 'cluster-a', namespace: 'team/a' }
const target = { scope, name: 'nightly/report' }

describe('cron job api', () => {
  beforeEach(() => vi.clearAllMocks())

  it('unwraps list/detail and filters events', async () => {
    apiMocks.get
      .mockResolvedValueOnce({ data: [{ name: 'nightly/report' }] })
      .mockResolvedValueOnce({
        data: {
          name: 'nightly/report',
          namespace: 'team/a',
          jobs: [{ name: 'nightly-report-123' }],
          relatedResources: [{ kind: 'ConfigMap', name: 'job-config' }],
        },
      })
      .mockResolvedValueOnce({
        data: [
          { involvedKind: 'CronJob', involvedName: 'nightly/report' },
          { involvedKind: 'Job', involvedName: 'nightly/report' },
        ],
      })

    await expect(listCronJobs(scope)).resolves.toEqual([{ name: 'nightly/report' }])
    await expect(getCronJobDetail(target)).resolves.toMatchObject({
      name: 'nightly/report',
      jobs: [{ name: 'nightly-report-123' }],
      relatedResources: [{ kind: 'ConfigMap', name: 'job-config' }],
    })
    await expect(listCronJobEvents(target)).resolves.toHaveLength(1)
  })

  it('suspends and deletes in the record namespace', async () => {
    apiMocks.post.mockResolvedValueOnce({ data: { name: 'nightly/report', suspend: true } })
    apiMocks.delete.mockResolvedValueOnce({})

    await expect(suspendCronJob({ ...target, suspend: true })).resolves.toMatchObject({
      suspend: true,
    })
    await expect(deleteCronJob(target)).resolves.toBeUndefined()
    expect(apiMocks.post).toHaveBeenCalledWith(
      '/clusters/cluster-a/workloads/cronjobs/nightly%2Freport/suspend?namespace=team%2Fa',
      { suspend: true },
    )
    expect(apiMocks.delete).toHaveBeenCalledWith(
      '/clusters/cluster-a/workloads/cronjobs/nightly%2Freport?namespace=team%2Fa',
    )
  })
})
