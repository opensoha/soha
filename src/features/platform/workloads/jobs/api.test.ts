import { beforeEach, describe, expect, it, vi } from 'vitest'
import { deleteJob, getJobDetail, listJobEvents, listJobs, getJobMetrics } from './api'

const apiMocks = vi.hoisted(() => ({
  delete: vi.fn(),
  get: vi.fn(),
  post: vi.fn(),
  put: vi.fn(),
}))

vi.mock('@/services/api-client', () => ({ api: apiMocks }))

const scope = { clusterId: 'cluster-a', namespace: 'team/a' }
const target = { scope, name: 'nightly/report' }

describe('job api', () => {
  beforeEach(() => vi.clearAllMocks())

  it('unwraps list and detail data while preserving encoded scope and name', async () => {
    apiMocks.get
      .mockResolvedValueOnce({ data: [{ name: 'nightly/report' }] })
      .mockResolvedValueOnce({
        data: {
          name: 'nightly/report',
          namespace: 'team/a',
          pods: [{ name: 'nightly-report-1' }],
          relatedResources: [{ kind: 'CronJob', name: 'nightly' }],
        },
      })

    await expect(listJobs(scope)).resolves.toEqual([{ name: 'nightly/report' }])
    await expect(getJobDetail(target)).resolves.toMatchObject({
      name: 'nightly/report',
      pods: [{ name: 'nightly-report-1' }],
      relatedResources: [{ kind: 'CronJob', name: 'nightly' }],
    })
    expect(apiMocks.get.mock.calls[0][0]).toBe(
      '/clusters/cluster-a/workloads/jobs?namespace=team%2Fa',
    )
    expect(apiMocks.get.mock.calls[1][0]).toContain(
      '/jobs/nightly%2Freport/detail?namespace=team%2Fa',
    )
  })

  it('filters events for only the requested Job', async () => {
    apiMocks.get.mockResolvedValueOnce({
      data: [
        { involvedKind: 'Job', involvedName: 'nightly/report' },
        { involvedKind: 'CronJob', involvedName: 'nightly/report' },
      ],
    })
    await expect(listJobEvents(target)).resolves.toHaveLength(1)
  })

  it('deletes the record in its own namespace', async () => {
    apiMocks.delete.mockResolvedValueOnce({})
    await expect(deleteJob(target)).resolves.toBeUndefined()
    expect(apiMocks.delete).toHaveBeenCalledWith(
      '/clusters/cluster-a/workloads/jobs/nightly%2Freport?namespace=team%2Fa',
    )
  })
})

it('scopes Job metrics to the requested resource and range', async () => {
  apiMocks.get.mockResolvedValueOnce({
    data: { resourceKind: 'Job', resourceName: 'nightly/report' },
  })
  await getJobMetrics(target, 15)
  expect(apiMocks.get).toHaveBeenLastCalledWith(
    '/clusters/cluster-a/workloads/jobs/nightly%2Freport/metrics?namespace=team%2Fa&rangeMinutes=15',
  )
})
