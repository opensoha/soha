import { afterEach, describe, expect, it, vi } from 'vitest'
import { dockerApi } from './docker-api'
import { dockerKeys } from './keys'
import { dockerQueries } from './queries'

async function executeQuery(options: { queryFn?: unknown }) {
  if (typeof options.queryFn !== 'function') throw new Error('Expected a query function')
  return options.queryFn({} as never)
}

describe('dockerQueries', () => {
  afterEach(() => vi.restoreAllMocks())

  it('passes the same normalized filters to list keys and APIs', async () => {
    const hosts = vi.spyOn(dockerApi, 'hosts').mockResolvedValue(undefined as never)
    const templates = vi.spyOn(dockerApi, 'templates').mockResolvedValue(undefined as never)
    const operations = vi.spyOn(dockerApi, 'operations').mockResolvedValue(undefined as never)

    const hostOptions = dockerQueries.hosts({ search: '', status: 'online', page: 0 })
    const templateOptions = dockerQueries.templates({ kind: '', enabled: false, pageSize: 25 })
    const operationOptions = dockerQueries.operations({
      operationKind: '',
      abnormal: false,
      pending: true,
    })

    expect(hostOptions.queryKey).toEqual(dockerKeys.hostList({ status: 'online', page: 0 }))
    expect(templateOptions.queryKey).toEqual(
      dockerKeys.templateList({ enabled: false, pageSize: 25 }),
    )
    expect(operationOptions.queryKey).toEqual(
      dockerKeys.operationList({ abnormal: false, pending: true }),
    )

    await Promise.all(
      [hostOptions, templateOptions, operationOptions].map((options) => executeQuery(options)),
    )

    expect(hosts).toHaveBeenCalledWith({ status: 'online', page: 0 })
    expect(templates).toHaveBeenCalledWith({ pageSize: 25, enabled: false })
    expect(operations).toHaveBeenCalledWith({ abnormal: false, pending: true })
  })

  it('returns domain values and covers option, detail, and runtime factories', async () => {
    const hostPage = { items: [], total: 0, page: 1, pageSize: 200 }
    const project = { id: 'project-1', hostId: 'host-1', name: 'Project 1' }
    const logs = { projectId: 'project-1', tailLines: 200, content: 'ready' }
    vi.spyOn(dockerApi, 'hosts').mockResolvedValue(hostPage)
    vi.spyOn(dockerApi, 'project').mockResolvedValue(project)
    vi.spyOn(dockerApi, 'projectLogs').mockResolvedValue(logs)

    await expect(executeQuery(dockerQueries.hostOptions())).resolves.toBe(hostPage)
    await expect(executeQuery(dockerQueries.project(' project-1 '))).resolves.toBe(project)
    await expect(
      executeQuery(
        dockerQueries.projectLogs(' project-1 ', { serviceName: 'api', tailLines: 200 }),
      ),
    ).resolves.toBe(logs)

    expect(dockerApi.hosts).toHaveBeenCalledWith({ page: 1, pageSize: 200 })
    expect(dockerApi.project).toHaveBeenCalledWith('project-1')
    expect(dockerApi.projectLogs).toHaveBeenCalledWith('project-1', {
      serviceName: 'api',
      tailLines: 200,
    })
  })

  it('disables identifier and runtime queries until their required target exists', () => {
    expect(dockerQueries.host(' ').enabled).toBe(false)
    expect(dockerQueries.project('', true).enabled).toBe(false)
    expect(dockerQueries.operationLogs('', true).enabled).toBe(false)
    expect(dockerQueries.projectLogs('', {}, true).enabled).toBe(false)
    expect(dockerQueries.projectLogs('project-1', {}, true).enabled).toBe(true)
    expect(
      dockerQueries.projectVolumeFiles(
        'project-1',
        { serviceName: 'api', target: '', path: '/', limit: 300 },
        true,
      ).enabled,
    ).toBe(false)
  })
})
