import { afterEach, describe, expect, it, vi } from 'vitest'
import { virtualizationApi } from './virtualization-api'
import { virtualizationKeys } from './keys'
import { virtualizationQueries } from './queries'

async function executeQuery(options: { queryFn?: unknown }) {
  if (typeof options.queryFn !== 'function') throw new Error('Expected a query function')
  return options.queryFn({} as never)
}

describe('virtualizationQueries', () => {
  afterEach(() => {
    vi.restoreAllMocks()
  })

  it('uses factory keys and passes the same normalized parameters to list APIs', async () => {
    const vms = vi.spyOn(virtualizationApi, 'vms').mockResolvedValue(undefined as never)
    const images = vi.spyOn(virtualizationApi, 'images').mockResolvedValue(undefined as never)
    const operations = vi
      .spyOn(virtualizationApi, 'operations')
      .mockResolvedValue(undefined as never)

    const vmOptions = virtualizationQueries.vms({ page: 1, status: '' })
    const imageOptions = virtualizationQueries.images({
      pageSize: 25,
      search: '',
      category: 'catalog',
    })
    const operationOptions = virtualizationQueries.operations({
      statuses: ['running', 'failed', 'running'],
      abnormal: false,
      pending: true,
    })

    expect(vmOptions.queryKey).toEqual(virtualizationKeys.vmList({ page: 1 }))
    expect(imageOptions.queryKey).toEqual(
      virtualizationKeys.imageList({ pageSize: 25, category: 'catalog' }),
    )
    expect(operationOptions.queryKey).toEqual(
      virtualizationKeys.operationList({ pending: true, statuses: ['failed', 'running'] }),
    )

    await executeQuery(vmOptions)
    await executeQuery(imageOptions)
    await executeQuery(operationOptions)

    expect(vms).toHaveBeenCalledWith({ page: 1 })
    expect(images).toHaveBeenCalledWith({ pageSize: 25, category: 'catalog' })
    expect(operations).toHaveBeenCalledWith({
      pending: true,
      statuses: ['failed', 'running'],
    })
  })

  it('covers every domain query with a factory key', async () => {
    const vmDetail = vi.spyOn(virtualizationApi, 'vmDetail').mockResolvedValue(undefined as never)
    const vmMetrics = vi.spyOn(virtualizationApi, 'vmMetrics').mockResolvedValue(undefined as never)
    const vmConsole = vi
      .spyOn(virtualizationApi, 'vmConsoleURL')
      .mockResolvedValue(undefined as never)
    const clusters = vi.spyOn(virtualizationApi, 'clusters').mockResolvedValue(undefined as never)
    const images = vi.spyOn(virtualizationApi, 'images').mockResolvedValue(undefined as never)
    const flavors = vi.spyOn(virtualizationApi, 'flavors').mockResolvedValue(undefined as never)
    const operationLogs = vi
      .spyOn(virtualizationApi, 'operationLogs')
      .mockResolvedValue(undefined as never)

    const options = [
      virtualizationQueries.vmDetail(' vm-1 '),
      virtualizationQueries.vmMetrics(' vm-1 ', { rangeMinutes: 15, stepSeconds: 30 }, true),
      virtualizationQueries.vmConsole(' vm-1 '),
      virtualizationQueries.clusters(),
      virtualizationQueries.imageOptions(),
      virtualizationQueries.flavors(),
      virtualizationQueries.operationLogs(' op-1 '),
    ]

    await Promise.all(options.map(executeQuery))

    expect(vmDetail).toHaveBeenCalledWith('vm-1')
    expect(vmMetrics).toHaveBeenCalledWith('vm-1', 15, 30)
    expect(vmConsole).toHaveBeenCalledWith('vm-1')
    expect(clusters).toHaveBeenCalledOnce()
    expect(images).toHaveBeenCalledWith({ pageSize: 500 })
    expect(flavors).toHaveBeenCalledOnce()
    expect(operationLogs).toHaveBeenCalledWith('op-1')
  })

  it('disables identifier queries when their identifier is empty', () => {
    expect(virtualizationQueries.vmDetail(' ').enabled).toBe(false)
    expect(virtualizationQueries.vmMetrics('', {}, true).enabled).toBe(false)
    expect(virtualizationQueries.vmConsole('', true).enabled).toBe(false)
    expect(virtualizationQueries.operationLogs('', true).enabled).toBe(false)
  })
})
