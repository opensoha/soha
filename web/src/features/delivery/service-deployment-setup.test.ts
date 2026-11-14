import { beforeEach, expect, it, vi } from 'vitest'
import { ApiError } from '@/services/api-error'
import { deliveryApi } from './api'
import { manifestApi } from './manifests/api'
import { saveServiceDeployment } from './service-deployment-setup'
import type { ServiceSetupCheckpoint, ServiceSetupDraft } from './service-setup'
import type {
  ApplicationEnvironment,
  ApplicationServiceComponent,
  ReleaseTarget,
  ServiceDeploymentTemplate,
} from './types'
import type { ManifestPackage } from './manifests/types'

vi.mock('./api', () => ({
  deliveryApi: {
    applications: { services: vi.fn(), updateService: vi.fn() },
    environments: { list: vi.fn(), update: vi.fn() },
  },
}))
vi.mock('./manifests/api', () => ({
  manifestApi: {
    applicationPackages: vi.fn(),
    create: vi.fn(),
    get: vi.fn(),
    update: vi.fn(),
    saveRevision: vi.fn(),
  },
}))

const reference = { templateId: 'http', version: 1, parameters: { replicas: 0, enabled: false } }
const draft: ServiceSetupDraft = {
  applicationId: 'app',
  service: { name: 'API', deploymentTemplate: reference },
  repositories: [],
  deployment: {
    template: {
      id: 'http',
      source: {
        renderer: 'raw_yaml',
        files: [{ path: 'deployment.yaml', content: 'kind: Deployment' }],
      },
    } as ServiceDeploymentTemplate,
    environments: [
      {
        environment: {
          id: 'app-dev',
          applicationId: 'app',
          environmentId: 'dev',
          clusterId: 'cluster',
          namespace: 'api-dev',
          createdAt: '2026-09-12T00:00:00Z',
          updatedAt: '2026-09-12T00:00:00Z',
        },
        parameters: { enabled: false, replicas: 0 },
      },
    ],
  },
}
const storedPackage = {
  id: 'package',
  name: 'API 部署配置',
  applicationId: 'app',
  serviceId: 'service',
  renderer: 'raw_yaml',
  status: 'draft',
  currentRevision: 0,
  createdAt: '2026-09-12T00:00:00Z',
  files: draft.deployment!.template!.source.files,
  bindings: [
    {
      id: 'manifest-binding',
      applicationEnvironmentId: 'app-dev',
      environmentKey: 'dev',
      clusterId: 'cluster',
      namespace: 'api-dev',
      templateParameters: { enabled: false, replicas: 0 },
    },
  ],
  updatedAt: '2026-09-12T00:00:00Z',
} as ManifestPackage
const service = {
  id: 'service',
  applicationId: 'app',
  key: 'api',
  name: 'API',
  serviceKind: 'kubernetes_workload',
  enabled: true,
  createdAt: '2026-09-12T00:00:00Z',
  updatedAt: '2026-09-12T00:00:00Z',
  version: 1,
  deploymentTemplate: reference,
} as ApplicationServiceComponent
const checkpoint = (): ServiceSetupCheckpoint => ({
  repositories: {},
  serviceId: 'service',
  serviceVersion: 1,
})

beforeEach(() => {
  vi.resetAllMocks()
  vi.mocked(manifestApi.applicationPackages).mockResolvedValue([])
  vi.mocked(manifestApi.create).mockResolvedValue(storedPackage)
  vi.mocked(manifestApi.get).mockResolvedValue(storedPackage)
  vi.mocked(manifestApi.saveRevision).mockResolvedValue({
    ...storedPackage,
    status: 'published',
    currentRevision: 1,
  })
  vi.mocked(deliveryApi.applications.services).mockResolvedValue([service])
  vi.mocked(deliveryApi.environments.list).mockResolvedValue(
    draft.deployment!.environments.map(({ environment }) => environment),
  )
})

it('saves Docker bindings without Kubernetes resources and restores a lost save response', async () => {
  const unrelated = {
    id: 'keep',
    clusterId: 'cluster',
    namespace: 'test',
    workloadKind: 'Deployment',
    workloadName: 'keep',
    enabled: true,
  }
  const environment = {
    ...draft.deployment!.environments[0].environment,
    clusterId: '',
    namespace: '',
    targets: [unrelated],
  }
  const docker = { hostId: 'host', projectId: 'project', imageMappings: { api: 'main' } }
  const input: ServiceSetupDraft = {
    ...draft,
    deployment: { docker: true, environments: [{ environment, parameters: {}, docker }] },
  }
  let stored: ApplicationEnvironment = structuredClone(environment)
  vi.mocked(deliveryApi.environments.list).mockImplementation(async () => [structuredClone(stored)])
  vi.mocked(deliveryApi.environments.update).mockImplementation(async (_id, payload) => {
    stored = {
      ...stored,
      targets: (payload.targets as ReleaseTarget[]).map((target) => ({
        ...target,
        id: target.id || 'docker-target',
      })),
      updatedAt: '2026-09-15T01:00:00Z',
    }
    throw new ApiError(0, 'lost response')
  })
  const state = checkpoint()
  await expect(saveServiceDeployment(input, state)).rejects.toThrow('lost response')
  await saveServiceDeployment(input, state)
  expect(deliveryApi.environments.update).toHaveBeenCalledTimes(1)
  expect(stored.targets).toContainEqual(unrelated)
  expect(stored.targets?.find((item) => item.docker)).toMatchObject({
    clusterId: '',
    namespace: '',
    workloadKind: 'ComposeProject',
    executorKind: 'docker_compose',
    metadata: { serviceId: 'service' },
    docker,
  })
  expect(manifestApi.create).not.toHaveBeenCalled()
  expect(state.deploymentComplete).toBe(true)
})

it('saves environment overrides with the package and links the service using its version', async () => {
  const state = checkpoint()
  await saveServiceDeployment(draft, state)
  expect(manifestApi.create).toHaveBeenCalledWith(
    expect.objectContaining({
      bindings: [
        expect.objectContaining({
          clusterId: 'cluster',
          namespace: 'api-dev',
          templateParameters: { enabled: false, replicas: 0 },
        }),
      ],
    }),
  )
  expect(deliveryApi.applications.updateService).toHaveBeenCalledWith(
    'app',
    'service',
    expect.objectContaining({
      expectedVersion: 1,
      deploymentTemplate: { ...reference, manifestPackageId: 'package' },
    }),
  )
  expect(deliveryApi.environments.update).toHaveBeenCalledWith(
    'app-dev',
    expect.objectContaining({
      expectedUpdatedAt: draft.deployment!.environments[0].environment.updatedAt,
      targets: [
        expect.objectContaining({
          executorKind: 'manifest_ssa',
          configRef: 'manifest-binding',
          metadata: expect.objectContaining({ serviceId: 'service' }),
        }),
      ],
    }),
  )
  await saveServiceDeployment(draft, state)
  expect(manifestApi.create).toHaveBeenCalledTimes(1)
  expect(deliveryApi.applications.updateService).toHaveBeenCalledTimes(1)
})

it('reconciles a lost create response without repeating the POST', async () => {
  const state = checkpoint()
  vi.mocked(manifestApi.create).mockRejectedValueOnce(new ApiError(0, 'lost response'))
  await expect(saveServiceDeployment(draft, state)).rejects.toThrow('lost response')
  await expect(saveServiceDeployment(draft, state)).rejects.toThrow('不会重复创建')
  vi.mocked(manifestApi.applicationPackages).mockResolvedValue([storedPackage])
  await saveServiceDeployment(draft, state)
  expect(manifestApi.create).toHaveBeenCalledTimes(1)
  expect(state.deploymentComplete).toBe(true)
})

it('retains the saved package when another writer changes the service', async () => {
  const state = checkpoint()
  vi.mocked(deliveryApi.applications.services).mockResolvedValue([{ ...service, version: 2 }])
  await expect(saveServiceDeployment(draft, state)).rejects.toMatchObject({ status: 409 })
  expect(state.manifestPackage?.id).toBe('package')
  expect(deliveryApi.applications.updateService).not.toHaveBeenCalled()
})

it('accepts an already linked service despite JSON key order after a lost response', async () => {
  const state = checkpoint()
  vi.mocked(deliveryApi.applications.updateService).mockRejectedValueOnce(
    new ApiError(0, 'lost response'),
  )
  await expect(saveServiceDeployment(draft, state)).rejects.toThrow('lost response')
  vi.mocked(deliveryApi.applications.services).mockResolvedValue([
    {
      ...service,
      version: 2,
      deploymentTemplate: {
        manifestPackageId: 'package',
        parameters: { enabled: false, replicas: 0 },
        version: 1,
        templateId: 'http',
      },
    },
  ])
  await saveServiceDeployment(draft, state)
  expect(deliveryApi.applications.updateService).toHaveBeenCalledTimes(1)
  expect(manifestApi.create).toHaveBeenCalledTimes(1)
  expect(state.deploymentComplete).toBe(true)
})

it('recovers a lost version response without creating another revision or execution', async () => {
  const state = checkpoint()
  vi.mocked(manifestApi.saveRevision).mockRejectedValueOnce(
    new ApiError(0, 'lost version response'),
  )
  await expect(saveServiceDeployment(draft, state)).rejects.toThrow('lost version response')
  expect(deliveryApi.applications.updateService).not.toHaveBeenCalled()
  vi.mocked(manifestApi.get).mockResolvedValue({
    ...storedPackage,
    status: 'published',
    currentRevision: 1,
    updatedAt: '2026-09-12T01:00:00Z',
  })
  await saveServiceDeployment(draft, state)
  expect(manifestApi.saveRevision).toHaveBeenCalledTimes(1)
  expect(manifestApi.saveRevision).toHaveBeenCalledWith('package', storedPackage.updatedAt)
  expect(state.manifestPackage?.currentRevision).toBe(1)
  expect(state.deploymentComplete).toBe(true)
})

it('rejects a concurrent configuration change before saving its revision', async () => {
  vi.mocked(manifestApi.get).mockResolvedValue({
    ...storedPackage,
    files: [{ path: 'deployment.yaml', content: 'kind: Job' }],
  })
  await expect(saveServiceDeployment(draft, checkpoint())).rejects.toMatchObject({ status: 409 })
  expect(manifestApi.saveRevision).not.toHaveBeenCalled()
  expect(deliveryApi.applications.updateService).not.toHaveBeenCalled()
})

it('resumes a lost Manifest target save, preserves other targets and refuses stale environment writes', async () => {
  const environment = draft.deployment!.environments[0].environment
  const unrelated = {
    id: 'other',
    clusterId: 'cluster',
    namespace: 'api-dev',
    workloadKind: 'Deployment',
    workloadName: 'other',
    enabled: true,
  } as ReleaseTarget
  let stored = { ...environment, targets: [unrelated] }
  const state = checkpoint()
  vi.mocked(deliveryApi.environments.list).mockImplementation(async () => structuredClone([stored]))
  vi.mocked(deliveryApi.applications.updateService).mockImplementation(async () => {
    vi.mocked(deliveryApi.applications.services).mockResolvedValue([
      {
        ...service,
        version: 2,
        deploymentTemplate: { ...reference, manifestPackageId: 'package' },
      },
    ])
  })
  vi.mocked(deliveryApi.environments.update).mockImplementation(async (_id, payload) => {
    stored = {
      ...stored,
      updatedAt: '2026-09-14T00:00:00Z',
      targets: (payload.targets as ReleaseTarget[]).map((target) => ({
        ...target,
        id: target.id || 'target',
      })),
    }
    throw new ApiError(0, 'lost target response')
  })
  await expect(saveServiceDeployment(draft, state)).rejects.toThrow('lost target response')
  expect(state.deploymentComplete).not.toBe(true)
  await saveServiceDeployment(draft, state)
  expect(state.deploymentComplete).toBe(true)
  expect(stored.targets).toContainEqual(unrelated)
  expect(deliveryApi.environments.update).toHaveBeenCalledTimes(1)
  stored.targets = [unrelated]
  await expect(saveServiceDeployment(draft, checkpoint())).rejects.toMatchObject({ status: 409 })
  expect(deliveryApi.environments.update).toHaveBeenCalledTimes(1)
})

it('resumes Helm environment saves after a lost response without replacing unrelated targets', async () => {
  const helm = {
    releaseName: 'api',
    source: {
      repositoryUrl: 'https://charts.example.com',
      chart: 'api',
      version: '1.0.0',
      values: {},
    },
    values: { replicas: 0, enabled: false },
  }
  const unrelated: ReleaseTarget = {
    id: 'other',
    clusterId: 'cluster',
    namespace: 'api-dev',
    workloadKind: 'Deployment',
    workloadName: 'worker',
    enabled: true,
  }
  const original = { ...draft.deployment!.environments[0].environment, targets: [unrelated] }
  const prod = {
    ...original,
    id: 'app-prod',
    environmentId: 'prod',
    namespace: 'api-prod',
    targets: [],
  }
  const input: ServiceSetupDraft = {
    ...draft,
    deployment: {
      helm: true,
      environments: [original, prod].map((environment) => ({ environment, parameters: {}, helm })),
    },
  }
  let stored: ApplicationEnvironment[] = structuredClone([original, prod])
  vi.mocked(deliveryApi.environments.list).mockImplementation(async () => structuredClone(stored))
  vi.mocked(deliveryApi.environments.update).mockImplementation(async (id, payload) => {
    stored = stored.map((environment) =>
      environment.id === id
        ? {
            ...environment,
            targets: (payload.targets as ReleaseTarget[]).map((target) => ({
              ...target,
              id: target.id || `target-${id}`,
            })),
            updatedAt: '2026-09-13T00:00:00Z',
          }
        : environment,
    )
    if (id === original.id) throw new ApiError(0, 'lost response')
  })
  const state = checkpoint()
  await expect(saveServiceDeployment(input, state)).rejects.toThrow('lost response')
  await saveServiceDeployment(input, state)
  expect(vi.mocked(deliveryApi.environments.update).mock.calls.map(([id]) => id)).toEqual([
    'app-dev',
    'app-prod',
  ])
  expect(deliveryApi.environments.update).toHaveBeenCalledWith(
    'app-dev',
    expect.objectContaining({ expectedUpdatedAt: original.updatedAt }),
  )
  expect(stored[0].targets).toContainEqual(unrelated)
  expect(stored[0].targets?.find((item) => item.helm)).toMatchObject({
    id: 'target-app-dev',
    metadata: { serviceId: 'service' },
    helm,
  })
  expect(state.deploymentComplete).toBe(true)
  expect(manifestApi.create).not.toHaveBeenCalled()
})

it('rejects a concurrent environment edit before changing Helm targets', async () => {
  const environment = draft.deployment!.environments[0].environment
  const input: ServiceSetupDraft = {
    ...draft,
    deployment: {
      helm: true,
      environments: [
        {
          environment,
          parameters: {},
          helm: {
            releaseName: 'api',
            source: {
              repositoryUrl: 'https://charts.example.com',
              chart: 'api',
              version: '1.0.0',
              values: {},
            },
          },
        },
      ],
    },
  }
  vi.mocked(deliveryApi.environments.list).mockResolvedValue([
    { ...environment, updatedAt: '2026-09-13T00:00:00Z' },
  ])
  await expect(saveServiceDeployment(input, checkpoint())).rejects.toMatchObject({ status: 409 })
  expect(deliveryApi.environments.update).not.toHaveBeenCalled()
})
