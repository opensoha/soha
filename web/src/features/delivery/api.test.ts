import { beforeEach, describe, expect, it, vi } from 'vitest'
import { deliveryApi, deliveryRuntimeDetailPath } from './api'

const apiMocks = vi.hoisted(() => ({
  delete: vi.fn(),
  get: vi.fn(),
  getEnvelope: vi.fn(),
  post: vi.fn(),
  put: vi.fn(),
}))

vi.mock('@/services/api-client', () => ({ api: apiMocks }))

describe('deliveryApi', () => {
  beforeEach(() => vi.clearAllMocks())

  it('uses explicit mapping endpoints so older servers cannot silently create services', async () => {
    apiMocks.post.mockResolvedValue({ data: {} })
    const scope = {
      clusterId: 'cluster',
      namespace: 'orders',
      applicationKey: 'shop',
      applicationName: 'Shop',
      environmentKey: 'test-b',
      environmentName: 'Test B',
      ownershipMode: 'observe_only' as const,
    }
    const native = {
      ...scope,
      workloads: [
        { serviceId: 'logical-api', workloadKind: 'Deployment' as const, workloadName: 'api' },
      ],
    }
    const helm = { ...scope, releases: [{ serviceId: 'logical-cache', releaseName: 'cache' }] }
    await deliveryApi.environments.importKubernetesServices(native)
    await deliveryApi.environments.importHelmReleases(helm)
    expect(apiMocks.post).toHaveBeenCalledWith('/application-environments/imports/mapped', native)
    expect(apiMocks.post).toHaveBeenCalledWith(
      '/application-environments/helm-release-imports/mapped',
      helm,
    )
    apiMocks.post.mockRejectedValue(new Error('unsupported endpoint'))
    await expect(deliveryApi.environments.importKubernetesServices(native)).rejects.toThrow(
      'unsupported endpoint',
    )
    expect(apiMocks.post).not.toHaveBeenCalledWith('/application-environments/imports', native)
    apiMocks.post.mockResolvedValue({ data: {} })
    const legacy = {
      ...scope,
      workloads: [{ workloadKind: 'Deployment' as const, workloadName: 'new-api' }],
    }
    await deliveryApi.environments.importKubernetesServices(legacy)
    expect(apiMocks.post).toHaveBeenCalledWith('/application-environments/imports', legacy)
  })

  it('uses the guarded configuration endpoint when targets are omitted and never retries old PUT', async () => {
    const defaults = {
      applicationId: 'app',
      environmentId: 'test',
      expectedUpdatedAt: '2026-10-26T00:00:00Z',
      namespace: 'new-default',
    }
    apiMocks.put.mockRejectedValueOnce(new Error('unsupported endpoint'))
    await expect(deliveryApi.environments.update('binding/a', defaults)).rejects.toThrow(
      'unsupported endpoint',
    )
    expect(apiMocks.put.mock.calls).toEqual([
      ['/application-environments/binding%2Fa/configuration', defaults],
    ])
    apiMocks.put.mockResolvedValue(undefined)
    const replacement = { ...defaults, targets: [] }
    await deliveryApi.environments.update('binding/a', replacement)
    expect(apiMocks.put).toHaveBeenCalledWith('/application-environments/binding%2Fa', replacement)
  })

  it('sends exact definition filters for recent executions', async () => {
    apiMocks.get.mockResolvedValue({ data: [] })
    await deliveryApi.builds.list({ applicationId: 'app', buildSourceId: ' source/a ', limit: 10 })
    await deliveryApi.workflows.list({
      applicationId: 'app',
      applicationEnvironmentId: ' env/a ',
      limit: 10,
    })
    await deliveryApi.batches.list({ workflowId: ' workflow/a ', limit: 10 })
    expect(apiMocks.get.mock.calls.map(([path]) => path)).toEqual([
      '/builds?applicationId=app&buildSourceId=source%2Fa&limit=10',
      '/workflows?applicationId=app&applicationEnvironmentId=env%2Fa&limit=10',
      '/delivery-batches?workflowId=workflow%2Fa&limit=10',
    ])
  })

  it('keeps Git source versions, candidate identities and explicit removal disposition', async () => {
    apiMocks.get.mockResolvedValue({ data: [] })
    apiMocks.post.mockResolvedValue({ data: { id: 'run-1' } })
    apiMocks.delete.mockResolvedValue(undefined)
    await deliveryApi.templateSources.runs('source/a', 50, 50)
    await deliveryApi.documents.source('BuildTemplate', 'template/a', 3)
    const input = {
      expectedGeneration: 7,
      candidateDigest: 'sha256:fixed',
      idempotencyKey: 'stable-key',
    }
    await deliveryApi.templateSources.apply('source/a', 'run/a', input)
    const removal = { expectedGeneration: 7, disposition: 'keep' as const }
    await deliveryApi.templateSources.remove('source/a', removal)
    await deliveryApi.templateSources.detach('source/a', 'Workflow', 'workflow/a', removal)
    expect(apiMocks.get).toHaveBeenCalledWith(
      '/delivery/template-sources/source%2Fa/sync-runs?offset=50&limit=50',
    )
    expect(apiMocks.get).toHaveBeenCalledWith(
      '/delivery/documents/BuildTemplate/template%2Fa/source?version=3',
    )
    expect(apiMocks.post).toHaveBeenCalledWith(
      '/delivery/template-sources/source%2Fa/sync-runs/run%2Fa/apply',
      input,
    )
    expect(apiMocks.delete).toHaveBeenCalledWith('/delivery/template-sources/source%2Fa', removal)
    expect(apiMocks.post).toHaveBeenCalledWith(
      '/delivery/template-sources/source%2Fa/objects/Workflow/workflow%2Fa/detach',
      removal,
    )
  })

  it('unwraps application list/detail/runtime values and encodes identifiers', async () => {
    apiMocks.get
      .mockResolvedValueOnce({ data: [{ id: 'app-1' }] })
      .mockResolvedValueOnce({ data: { application: { id: 'app/1' } } })
      .mockResolvedValueOnce({ data: { application: { id: 'app/1' }, environments: [] } })

    await expect(deliveryApi.applications.list()).resolves.toEqual([{ id: 'app-1' }])
    await expect(deliveryApi.applications.detail(' app/1 ')).resolves.toMatchObject({
      application: { id: 'app/1' },
    })
    await expect(deliveryApi.applications.runtime('app/1')).resolves.toMatchObject({
      environments: [],
    })

    expect(apiMocks.get.mock.calls.map(([path]) => path)).toEqual([
      '/applications',
      '/applications/app%2F1/detail',
      '/applications/app%2F1/runtime',
    ])
  })

  it('preserves candidate, application filter, workload, and gateway query paths', async () => {
    apiMocks.getEnvelope.mockResolvedValue({ items: [], truncated: true })

    await expect(
      deliveryApi.environments.targetCandidates({
        clusterId: 'cluster/a',
        namespace: 'team dev',
        search: ' api ',
        limit: 200,
      }),
    ).resolves.toEqual({ items: [], truncated: true })
    await deliveryApi.workflows.list({ applicationId: 'app/a', limit: 200 })
    await deliveryApi.workloads.metrics({
      clusterId: 'cluster/a',
      namespace: 'team dev',
      workloadName: 'api/web',
    })
    await deliveryApi.gateway.readiness({ skillId: 'delivery/onboarding' })

    expect(apiMocks.getEnvelope).toHaveBeenCalledWith(
      '/application-environments/target-candidates?clusterId=cluster%2Fa&namespace=team+dev&search=api&limit=200',
    )
    expect(apiMocks.get.mock.calls.map(([path]) => path)).toEqual([
      '/workflows?applicationId=app%2Fa&limit=200',
      '/clusters/cluster%2Fa/workloads/deployments/api%2Fweb/metrics?namespace=team+dev&rangeMinutes=60',
      '/ai-gateway/capabilities?source=delivery-workbench&skillId=delivery%2Fonboarding',
    ])
  })

  it('keeps the platform environment catalog separate from application bindings', async () => {
    apiMocks.get.mockResolvedValue({ data: [] })

    await deliveryApi.environmentCatalog.list()
    await deliveryApi.environments.list()

    expect(apiMocks.get.mock.calls.map(([path]) => path)).toEqual([
      '/delivery/environments',
      '/application-environments',
    ])
  })

  it('saves an application workflow through the atomic application-scoped endpoint', async () => {
    const payload = {
      name: 'Release workflow',
      definition: { nodes: [] },
      enabled: true,
    }
    apiMocks.put.mockResolvedValue({ data: { id: 'binding-1' } })

    await expect(
      deliveryApi.environments.saveWorkflow(' app/1 ', ' binding/1 ', payload),
    ).resolves.toEqual({ id: 'binding-1' })

    expect(apiMocks.put).toHaveBeenCalledWith(
      '/applications/app%2F1/application-environments/binding%2F1/workflow',
      payload,
    )
  })

  it('imports selected Kubernetes workloads in observe-only mode', async () => {
    const payload = {
      clusterId: 'cluster-a',
      namespace: 'erp',
      applicationKey: 'erp',
      applicationName: 'ERP',
      environmentKey: 'prod',
      environmentName: 'prod',
      ownershipMode: 'observe_only' as const,
      workloads: [{ workloadKind: 'Deployment' as const, workloadName: 'api' }],
    }
    apiMocks.post.mockResolvedValue({ data: { ownershipMode: 'observe_only' } })

    await expect(deliveryApi.environments.importKubernetesServices(payload)).resolves.toEqual({
      ownershipMode: 'observe_only',
    })
    expect(apiMocks.post).toHaveBeenCalledWith('/application-environments/imports', payload)
  })

  it('lists and imports managed Helm releases', async () => {
    const payload = {
      clusterId: 'cluster-a',
      namespace: 'erp',
      applicationKey: 'erp',
      applicationName: 'ERP',
      environmentKey: 'prod',
      environmentName: 'prod',
      ownershipMode: 'managed' as const,
      releases: [{ releaseName: 'erp' }],
    }
    apiMocks.get.mockResolvedValue({ data: [{ name: 'erp', namespace: 'erp' }] })
    apiMocks.post.mockResolvedValue({ data: { ownershipMode: 'managed' } })

    await expect(deliveryApi.environments.helmReleases('cluster/a', 'erp')).resolves.toEqual([
      { name: 'erp', namespace: 'erp' },
    ])
    await expect(deliveryApi.environments.importHelmReleases(payload)).resolves.toEqual({
      ownershipMode: 'managed',
    })
    expect(apiMocks.get).toHaveBeenCalledWith('/clusters/cluster%2Fa/helm/releases?namespace=erp')
    expect(apiMocks.post).toHaveBeenCalledWith(
      '/application-environments/helm-release-imports',
      payload,
    )
  })

  it('uses repository and GitLab project, branch, tag, and commit endpoints', async () => {
    apiMocks.get
      .mockResolvedValueOnce({ data: [] })
      .mockResolvedValueOnce({ items: [] })
      .mockResolvedValueOnce({ items: [] })
      .mockResolvedValueOnce({ items: [] })
      .mockResolvedValueOnce({ items: [], page: 1, limit: 20, hasMore: false })

    await deliveryApi.repositories.list({ applicationId: ' app/1 ', search: ' api ' })
    await deliveryApi.gitlab.projects({ search: ' api ' })
    await deliveryApi.gitlab.branches({ projectId: 'group/api', search: 'main' })
    await deliveryApi.gitlab.tags({ projectId: 'group/api' })
    await deliveryApi.gitlab.commits({ projectId: 'group/api', page: 1, limit: 20 })

    expect(apiMocks.get.mock.calls.map(([path]) => path)).toEqual([
      '/repositories?applicationId=app%2F1&search=api',
      '/integrations/gitlab/projects?search=api',
      '/integrations/gitlab/branches?projectId=group%2Fapi&search=main',
      '/integrations/gitlab/tags?projectId=group%2Fapi',
      '/integrations/gitlab/commits?projectId=group%2Fapi&limit=20&page=1',
    ])
  })

  it('uses the registry items envelope and canonical registry fields', async () => {
    const connection = {
      id: 'registry-1',
      name: 'Harbor Prod',
      registryType: 'harbor',
      endpoint: 'https://harbor.example.com',
      insecure: false,
      metadata: { secretConfigured: true, secretStorage: 'encrypted' },
      createdAt: '2026-08-08T08:00:00Z',
      updatedAt: '2026-08-08T08:00:00Z',
    }
    const payload = {
      name: connection.name,
      registryType: connection.registryType,
      endpoint: connection.endpoint,
      secret: 'fixture-token',
      insecure: false,
    }
    apiMocks.getEnvelope.mockResolvedValue({ items: [connection] })
    apiMocks.post.mockResolvedValue({ data: connection })
    apiMocks.put.mockResolvedValue({ data: connection })

    await expect(deliveryApi.registries.list()).resolves.toEqual([connection])
    await deliveryApi.registries.create(payload)
    await deliveryApi.registries.update('registry/1', payload)

    expect(apiMocks.getEnvelope).toHaveBeenCalledWith('/registries')
    expect(apiMocks.post).toHaveBeenCalledWith('/registries', payload)
    expect(apiMocks.put).toHaveBeenCalledWith('/registries/registry%2F1', payload)
  })

  it('uses the five canonical runtime detail endpoints and unwraps details', async () => {
    apiMocks.get.mockResolvedValue({ data: { id: 'runtime-1', object: { id: 'record-1' } } })

    const kinds = ['build', 'workflow', 'release', 'release_bundle', 'execution_task'] as const
    for (const kind of kinds) {
      await expect(deliveryApi.runtime.detail(kind, 'record/1')).resolves.toMatchObject({
        id: 'runtime-1',
      })
    }

    expect(kinds.map((kind) => deliveryRuntimeDetailPath(kind, 'record/1'))).toEqual([
      '/delivery/runtime/builds/record%2F1',
      '/delivery/runtime/workflows/record%2F1',
      '/delivery/runtime/releases/record%2F1',
      '/delivery/runtime/release-bundles/record%2F1',
      '/delivery/runtime/execution-tasks/record%2F1',
    ])
  })

  it('keeps template, execution, plan, and rollback mutation payloads intact', async () => {
    apiMocks.post
      .mockResolvedValueOnce({ data: { applicationDraft: {} } })
      .mockResolvedValueOnce({ data: { id: 'plan-1' } })
      .mockResolvedValueOnce({ data: { plan: { id: 'plan-1' } } })
      .mockResolvedValue(undefined)

    await deliveryApi.blueprints.renderSpec('blueprint/1')
    await deliveryApi.plans.create({ applicationId: 'app-1' } as never)
    await deliveryApi.plans.confirm('plan/1')
    await deliveryApi.executionTasks.cancel({ id: 'task/1', reason: 'manual cancel' })
    await deliveryApi.deployments.rollback({
      clusterId: 'cluster/1',
      namespace: 'default',
      workloadName: 'api',
      revision: '2',
    })

    expect(apiMocks.post).toHaveBeenNthCalledWith(
      1,
      '/delivery/blueprints/blueprint%2F1/render-spec',
      {},
    )
    expect(apiMocks.post).toHaveBeenNthCalledWith(4, '/delivery/execution-tasks/task%2F1/cancel', {
      reason: 'manual cancel',
    })
    expect(apiMocks.post).toHaveBeenNthCalledWith(
      5,
      '/clusters/cluster%2F1/workloads/deployments/rollback',
      { namespace: 'default', name: 'api', revision: '2' },
    )
  })
  it('requires the edited application version and sends only input fields', async () => {
    vi.mocked(apiMocks.put).mockResolvedValue({})
    await deliveryApi.applications.update('app-1', {
      name: 'API',
      key: 'api',
      enabled: true,
      version: 4,
      createdAt: 'old',
      updatedAt: 'old',
      environmentCount: 2,
    })
    expect(apiMocks.put).toHaveBeenLastCalledWith('/applications/app-1', {
      name: 'API',
      key: 'api',
      enabled: true,
      expectedVersion: 4,
    })
    await expect(
      deliveryApi.applications.update('app-1', { name: 'Missing version' }),
    ).rejects.toThrow('缺少有效版本')
  })
})

it('preserves batch idempotency and workflow versions through dedicated endpoints', async () => {
  apiMocks.get.mockResolvedValue({ data: [] })
  apiMocks.post.mockResolvedValue({ data: { id: 'batch-1' } })
  apiMocks.put.mockResolvedValue({ data: { id: 'workflow-1' } })
  const definition = {
    name: 'Release',
    targets: [{ id: 'api', applicationId: 'app', serviceId: 'service', action: 'build' as const }],
  }
  const input = { idempotencyKey: 'stable-request-id', definition }
  await deliveryApi.batches.list({ applicationId: ' app/a ', serviceId: ' service/1 ', limit: 20 })
  await deliveryApi.batches.create(input)
  await deliveryApi.batches.cancel(' batch/1 ', { reason: 'user stopped' })
  await deliveryApi.deliveryWorkflows.update(' workflow/1 ', { expectedVersion: 3, definition })
  expect(apiMocks.get).toHaveBeenCalledWith(
    '/delivery-batches?applicationId=app%2Fa&serviceId=service%2F1&limit=20',
  )
  expect(apiMocks.post).toHaveBeenCalledWith('/delivery-batches', input)
  expect(apiMocks.post).toHaveBeenCalledWith('/delivery-batches/batch%2F1/cancel', {
    reason: 'user stopped',
  })
  expect(apiMocks.put).toHaveBeenCalledWith('/delivery-workflows/workflow%2F1', {
    expectedVersion: 3,
    definition,
  })
})
