import { beforeEach, describe, expect, it, vi } from 'vitest'
import { dockerApi } from './docker-api'
import type { DockerHostInput, DockerProjectInput } from './docker-types'

const apiMocks = vi.hoisted(() => ({
  delete: vi.fn(),
  get: vi.fn(),
  post: vi.fn(),
  postWithHeaders: vi.fn(),
  put: vi.fn(),
}))

vi.mock('@/services/api-client', () => ({ api: apiMocks }))

describe('dockerApi', () => {
  beforeEach(() => vi.clearAllMocks())

  it('unwraps list and detail responses while preserving query wire behavior', async () => {
    const page = { items: [{ id: 'host-1', name: 'Host 1' }], total: 1, page: 0, pageSize: 20 }
    const host = { id: 'host/a', name: 'Host A' }
    apiMocks.get.mockResolvedValueOnce({ data: page }).mockResolvedValueOnce({ data: host })

    await expect(
      dockerApi.hosts({ search: '', status: 'online', page: 0, pageSize: 20 }),
    ).resolves.toEqual(page)
    await expect(dockerApi.host('host/a')).resolves.toEqual(host)

    expect(apiMocks.get).toHaveBeenNthCalledWith(
      1,
      '/docker/hosts?status=online&page=0&pageSize=20',
    )
    expect(apiMocks.get).toHaveBeenNthCalledWith(2, '/docker/hosts/host%2Fa')
  })

  it('unwraps runtime values and serializes required volume file parameters', async () => {
    const logs = { projectId: 'project/a', tailLines: 200, content: 'ready' }
    const files = {
      projectId: 'project/a',
      target: '/data',
      path: '/',
      items: [],
    }
    apiMocks.get.mockResolvedValueOnce({ data: logs }).mockResolvedValueOnce({ data: files })

    await expect(
      dockerApi.projectLogs('project/a', { serviceName: 'api', tailLines: 200 }),
    ).resolves.toEqual(logs)
    await expect(
      dockerApi.projectVolumeFiles('project/a', {
        serviceName: 'api',
        target: '/data',
        path: '/',
        limit: 300,
      }),
    ).resolves.toEqual(files)

    expect(apiMocks.get).toHaveBeenNthCalledWith(
      1,
      '/docker/projects/project%2Fa/runtime/logs?serviceName=api&tailLines=200',
    )
    expect(apiMocks.get).toHaveBeenNthCalledWith(
      2,
      '/docker/projects/project%2Fa/runtime/volume-files?serviceName=api&target=%2Fdata&path=%2F&limit=300',
    )
  })

  it('unwraps mutation entities and returns void for deletes', async () => {
    const hostInput = { name: 'Host A' } as DockerHostInput
    const projectInput = { hostId: 'host-1', name: 'Project A' } as DockerProjectInput
    apiMocks.post.mockResolvedValueOnce({ data: { id: 'host-1', ...hostInput } })
    apiMocks.put.mockResolvedValueOnce({ data: { id: 'project-1', ...projectInput } })
    apiMocks.delete.mockResolvedValueOnce({ data: { deleted: true } })

    await expect(dockerApi.createHost(hostInput)).resolves.toMatchObject({ id: 'host-1' })
    await expect(dockerApi.updateProject('project/1', projectInput)).resolves.toMatchObject({
      id: 'project-1',
    })
    await expect(dockerApi.deleteProject('project/1')).resolves.toBeUndefined()

    expect(apiMocks.post).toHaveBeenCalledWith('/docker/hosts', hostInput)
    expect(apiMocks.put).toHaveBeenCalledWith('/docker/projects/project%2F1', projectInput)
    expect(apiMocks.delete).toHaveBeenCalledWith('/docker/projects/project%2F1')
  })

  it('creates a quick Agent installation for an existing host record', async () => {
    const installation = {
      hostId: 'host/a',
      operationId: 'operation-1',
      scriptUrl: 'http://localhost/api/v1/docker/agent-installations/ticket/install.sh',
      command: 'curl -fsSL http://localhost/install.sh | sh',
      expiresAt: '2026-08-11T12:15:00Z',
    }
    apiMocks.post.mockResolvedValueOnce({ data: installation })

    await expect(dockerApi.createHostAgentInstallation('host/a')).resolves.toEqual(installation)

    expect(apiMocks.post).toHaveBeenCalledWith('/docker/hosts/host%2Fa/agent-installation')
  })

  it('plans host and project operations before idempotent execution', async () => {
    const hostInput = { name: 'preview-host' }
    const plan = {
      capability: 'docker.hosts.quick_create.trigger',
      target: 'preview-host',
      ready: true,
      riskLevel: 'execute' as const,
      requiresApproval: true,
      changes: [{ action: 'create', resource: 'docker-host', summary: 'Create host' }],
      warnings: [],
    }
    apiMocks.post.mockResolvedValue({ data: plan })
    apiMocks.postWithHeaders.mockResolvedValue({ data: { id: 'operation-1' } })

    await expect(dockerApi.planQuickCreateHost(hostInput)).resolves.toBe(plan)
    await expect(dockerApi.quickCreateHost(hostInput, 'idem-host-1234')).resolves.toEqual({
      id: 'operation-1',
    })
    await expect(dockerApi.planProjectDeploy('project/1', 'deploy')).resolves.toBe(plan)
    await dockerApi.deployProject('project/1', 'deploy', 'idem-project-1234')

    expect(apiMocks.post).toHaveBeenCalledWith('/docker/hosts/quick-create/plan', hostInput)
    expect(apiMocks.post).toHaveBeenCalledWith('/docker/projects/project%2F1/deploy/plan', {
      action: 'deploy',
    })
    expect(apiMocks.postWithHeaders).toHaveBeenCalledWith('/docker/hosts/quick-create', hostInput, {
      'Idempotency-Key': 'idem-host-1234',
    })
    expect(apiMocks.postWithHeaders).toHaveBeenCalledWith(
      '/docker/projects/project%2F1/deploy',
      { action: 'deploy' },
      { 'Idempotency-Key': 'idem-project-1234' },
    )
  })
})
