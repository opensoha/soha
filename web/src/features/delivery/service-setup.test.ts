import { beforeEach, describe, expect, it, vi } from 'vitest'
import { ApiError } from '@/services/api-error'
import { deliveryApi } from './api'
import {
  buildSourceRepositoryBindings,
  saveServiceSetup,
  type ServiceSetupCheckpoint,
  type ServiceSetupDraft,
} from './service-setup'
import type {
  ApplicationServiceComponent,
  DeliveryApplicationDetail,
  DeliveryRepository,
} from './types'

vi.mock('./api', () => ({
  deliveryApi: {
    repositories: { list: vi.fn(), create: vi.fn() },
    applications: {
      detail: vi.fn(),
      update: vi.fn(),
      services: vi.fn(),
      createService: vi.fn(),
      updateService: vi.fn(),
    },
  },
}))

const repository = {
  id: 'repository-created',
  name: 'API',
  provider: 'git',
  protocol: 'https',
  url: 'https://git.example.com/api.git',
  path: 'team/api',
  defaultBranch: 'main',
} as DeliveryRepository
const draft = (): ServiceSetupDraft => ({
  applicationId: 'app-1',
  service: {
    key: 'api',
    name: 'API',
    serviceKind: 'kubernetes_workload',
    buildSourceId: 'source-new',
    repositoryId: 'draft:repository',
    metadata: { sourceMode: 'build' },
  },
  repositories: [{ draftId: 'draft:repository', input: repository }],
  buildSource: {
    id: 'source-new',
    name: 'API build',
    type: 'repo_dockerfile',
    enabled: true,
    isDefault: false,
    config: { repositoryBindings: [{ repositoryId: 'draft:repository', checkoutPath: '.' }] },
  },
})
const createdService = {
  ...draft().service,
  id: 'service-created',
  repositoryId: repository.id,
} as ApplicationServiceComponent

beforeEach(() => {
  vi.resetAllMocks()
  vi.mocked(deliveryApi.repositories.list).mockResolvedValue([])
  vi.mocked(deliveryApi.repositories.create).mockResolvedValue(repository)
  vi.mocked(deliveryApi.applications.services).mockResolvedValue([])
  vi.mocked(deliveryApi.applications.createService).mockResolvedValue(createdService)
  vi.mocked(deliveryApi.applications.detail).mockResolvedValue({
    application: {
      id: 'app-1',
      version: 7,
      name: 'Latest application',
      key: 'checkout',
      enabled: true,
      repositoryIds: ['repository-existing'],
      metadata: { preserve: true },
      buildSources: [
        {
          id: 'source-existing',
          name: 'Shared',
          type: 'repo_dockerfile',
          enabled: true,
          isDefault: true,
        },
      ],
    },
  } as unknown as DeliveryApplicationDetail)
})

describe('service setup saving', () => {
  it('reconciles a lost service update response using the exact version and submitted configuration', async () => {
    const input = {
      draft: { ...draft(), serviceId: 'service-created', buildSource: undefined, repositories: [] },
      checkpoint: { repositories: {} } as ServiceSetupCheckpoint,
    }
    input.draft.service = { ...createdService, expectedVersion: 1 }
    vi.mocked(deliveryApi.applications.updateService).mockRejectedValueOnce(
      new ApiError(0, 'lost response'),
    )
    await expect(saveServiceSetup(input)).rejects.toThrow('lost response')
    vi.mocked(deliveryApi.applications.services).mockResolvedValue([
      { ...createdService, version: 2, name: 'Another edit' },
    ])
    await expect(saveServiceSetup(input)).rejects.toMatchObject({ status: 409 })
    vi.mocked(deliveryApi.applications.services).mockResolvedValue([
      { ...createdService, version: 2 },
    ])
    await expect(saveServiceSetup(input)).resolves.toBe('service-created')
    expect(deliveryApi.applications.updateService).toHaveBeenCalledTimes(1)
  })

  it('preserves repository bindings for Dockerfile and external pipeline sources', () => {
    const source = { type: 'repo_dockerfile' as const, config: { repositoryId: 'legacy' } }
    expect(buildSourceRepositoryBindings(source)).toEqual([
      { repositoryId: 'legacy', allowCommitSelection: false, submodules: false },
    ])
    expect(buildSourceRepositoryBindings({ ...source, type: 'external_pipeline' })).toEqual([
      { repositoryId: 'legacy', allowCommitSelection: false, submodules: false },
    ])
  })

  it('resumes after a rejected service write without recreating repositories or replacing current application fields', async () => {
    const checkpoint: ServiceSetupCheckpoint = { repositories: {} }
    const input = { draft: draft(), checkpoint }
    vi.mocked(deliveryApi.applications.createService).mockRejectedValueOnce(
      new ApiError(400, 'invalid service'),
    )
    await expect(saveServiceSetup(input)).rejects.toThrow('invalid service')
    expect(checkpoint.repositories['draft:repository'].id).toBe(repository.id)
    expect(deliveryApi.applications.update).toHaveBeenCalledWith(
      'app-1',
      expect.objectContaining({
        expectedVersion: 7,
        name: 'Latest application',
        metadata: { preserve: true },
        repositoryIds: ['repository-existing', repository.id],
        buildSources: [
          expect.objectContaining({ id: 'source-existing', isDefault: true }),
          expect.objectContaining({
            id: 'source-new',
            config: expect.objectContaining({ repositoryId: repository.id }),
          }),
        ],
      }),
    )
    await expect(saveServiceSetup(input)).resolves.toBe('service-created')
    expect(deliveryApi.repositories.create).toHaveBeenCalledTimes(1)
    expect(deliveryApi.applications.update).toHaveBeenCalledTimes(1)
    expect(deliveryApi.applications.createService).toHaveBeenLastCalledWith(
      'app-1',
      expect.objectContaining({ repositoryId: repository.id }),
    )
  })

  it('reconciles an uncertain repository POST before continuing', async () => {
    const input = { draft: draft(), checkpoint: { repositories: {} } as ServiceSetupCheckpoint }
    vi.mocked(deliveryApi.repositories.create).mockRejectedValueOnce(
      new ApiError(0, 'connection lost'),
    )
    await expect(saveServiceSetup(input)).rejects.toThrow('connection lost')
    await expect(saveServiceSetup(input)).rejects.toThrow('不会重复创建')
    expect(deliveryApi.repositories.create).toHaveBeenCalledTimes(1)
    vi.mocked(deliveryApi.repositories.list).mockResolvedValue([repository])
    await expect(saveServiceSetup(input)).resolves.toBe('service-created')
    expect(deliveryApi.repositories.create).toHaveBeenCalledTimes(1)
  })

  it.each(['repository', 'build'] as const)(
    'resumes after a %s save fails without discarding confirmed resources',
    async (step) => {
      const input = { draft: draft(), checkpoint: { repositories: {} } as ServiceSetupCheckpoint }
      if (step === 'repository') {
        vi.mocked(deliveryApi.repositories.create).mockRejectedValueOnce(
          new ApiError(400, 'save failed'),
        )
      } else {
        vi.mocked(deliveryApi.applications.update).mockRejectedValueOnce(
          new ApiError(502, 'save failed'),
        )
      }
      await expect(saveServiceSetup(input)).rejects.toThrow('save failed')
      expect(deliveryApi.applications.createService).not.toHaveBeenCalled()
      await expect(saveServiceSetup(input)).resolves.toBe('service-created')
      expect(deliveryApi.repositories.create).toHaveBeenCalledTimes(step === 'repository' ? 2 : 1)
      expect(deliveryApi.applications.update).toHaveBeenCalledTimes(step === 'build' ? 2 : 1)
      expect(deliveryApi.applications.detail).toHaveBeenCalledTimes(step === 'build' ? 2 : 1)
      expect(deliveryApi.applications.createService).toHaveBeenCalledTimes(1)
    },
  )

  it('does not repeat an uncertain service POST or adopt an existing matching key', async () => {
    const input = { draft: draft(), checkpoint: { repositories: {} } as ServiceSetupCheckpoint }
    vi.mocked(deliveryApi.applications.createService).mockRejectedValueOnce(
      new ApiError(502, 'lost response'),
    )
    await expect(saveServiceSetup(input)).rejects.toThrow('lost response')
    await expect(saveServiceSetup(input)).rejects.toThrow('不会重复创建')
    vi.mocked(deliveryApi.applications.services).mockResolvedValue([createdService])
    await expect(saveServiceSetup(input)).resolves.toBe('service-created')
    expect(deliveryApi.applications.createService).toHaveBeenCalledTimes(1)
    const freshInput = { draft: draft(), checkpoint: { repositories: {} } }
    await expect(saveServiceSetup(freshInput)).rejects.toThrow('服务 Key 已存在')
    expect(deliveryApi.applications.createService).toHaveBeenCalledTimes(1)
  })

  it('saves an existing image service without writing a repository or application build definition', async () => {
    const input = {
      draft: {
        ...draft(),
        serviceId: 'service-existing',
        buildSource: undefined,
        service: { name: 'API', key: 'api', buildSourceId: '', repositoryId: '' },
      },
      checkpoint: { repositories: {} },
    }
    await expect(saveServiceSetup(input)).resolves.toBe('service-existing')
    expect(deliveryApi.repositories.create).not.toHaveBeenCalled()
    expect(deliveryApi.applications.update).not.toHaveBeenCalled()
    expect(deliveryApi.applications.updateService).toHaveBeenCalledWith(
      'app-1',
      'service-existing',
      expect.objectContaining({ buildSourceId: '', repositoryId: '' }),
    )
  })
  it('keeps the draft after CAS conflict, merges unrelated updates on retry, and refuses a changed shared source', async () => {
    const { application } = await deliveryApi.applications.detail('app-1')
    const original = application.buildSources![0]
    const sharedDraft = {
      ...draft(),
      repositories: [],
      originalBuildSource: original,
      buildSource: { ...original, name: 'Edited shared build' },
      service: { ...draft().service, buildSourceId: original.id },
    }
    const input = { draft: sharedDraft, checkpoint: { repositories: {} } as ServiceSetupCheckpoint }
    vi.mocked(deliveryApi.applications.update).mockRejectedValueOnce(
      new ApiError(409, 'version conflict'),
    )
    await expect(saveServiceSetup(input)).rejects.toThrow('version conflict')
    expect(input.draft.buildSource.name).toBe('Edited shared build')
    expect(input.checkpoint.buildSourceSaved).not.toBe(true)
    expect(deliveryApi.applications.createService).not.toHaveBeenCalled()
    vi.mocked(deliveryApi.applications.detail).mockResolvedValue({
      application: { ...application, version: 9, description: 'Changed by another editor' },
    } as DeliveryApplicationDetail)
    await expect(saveServiceSetup(input)).resolves.toBe('service-created')
    expect(deliveryApi.applications.update).toHaveBeenLastCalledWith(
      'app-1',
      expect.objectContaining({
        expectedVersion: 9,
        description: 'Changed by another editor',
      }),
    )
    const changed = { ...original, name: 'Someone else changed this build' }
    vi.mocked(deliveryApi.applications.detail).mockResolvedValue({
      application: { ...application, version: 10, buildSources: [changed] },
    } as DeliveryApplicationDetail)
    await expect(
      saveServiceSetup({ draft: sharedDraft, checkpoint: { repositories: {} } }),
    ).rejects.toThrow('共享构建已被其他人修改')
    expect(deliveryApi.applications.update).toHaveBeenCalledTimes(2)
  })

  it('recognizes a successfully saved build after its response was lost', async () => {
    const input = { draft: draft(), checkpoint: { repositories: {} } as ServiceSetupCheckpoint }
    vi.mocked(deliveryApi.applications.update).mockRejectedValueOnce(
      new ApiError(502, 'response lost'),
    )
    await expect(saveServiceSetup(input)).rejects.toThrow('response lost')
    const savedPayload = vi.mocked(deliveryApi.applications.update).mock.calls[0][1]
    vi.mocked(deliveryApi.applications.detail).mockResolvedValue({
      application: { ...savedPayload, version: 8 },
    } as unknown as DeliveryApplicationDetail)
    await expect(saveServiceSetup(input)).resolves.toBe('service-created')
    expect(deliveryApi.applications.update).toHaveBeenCalledTimes(1)
    expect(deliveryApi.repositories.create).toHaveBeenCalledTimes(1)
  })
})
