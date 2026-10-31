/** @vitest-environment jsdom */
import { act } from 'react'
import { createRoot } from 'react-dom/client'
import { App, ConfigProvider } from 'antd'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { MemoryRouter } from 'react-router-dom'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { ApiError } from '@/services/api-error'
import { deliveryApi } from '../api'
import type { DeliveryBatch, DeliveryWorkflow, DeliveryWorkflowDefinition } from '../types'
import { DeliveryBatchEditor } from './editor'
import { DeliveryTargetEditor } from './target-editor'
import { ServiceDeliveryForm } from '../applications/service-delivery-form'
import { kubernetesHelmQueries } from '@/features/platform'

const navigation = vi.hoisted(() => vi.fn())
vi.mock('react-router-dom', async (importOriginal) => ({
  ...(await importOriginal<typeof import('react-router-dom')>()),
  useNavigate: () => navigation,
}))
vi.mock('@/features/auth', () => ({
  hasPermission: () => true,
  usePermissionSnapshot: () => ({ data: { data: {} } }),
}))

const definition: DeliveryWorkflowDefinition = {
  name: '商城联合发布',
  mode: 'service_serial',
  stopOnFailure: true,
  maxConcurrency: 4,
  targets: Array.from({ length: 20 }, (_, index) => ({
    id: `target-${index}`,
    applicationId: `app-${Math.floor(index / 8)}`,
    serviceId: `service-${Math.floor(index / 2)}`,
    applicationEnvironmentId: index % 2 ? 'prod' : 'dev',
    action: 'build_deploy',
  })),
}
const workflow: DeliveryWorkflow = {
  id: 'workflow-1',
  version: 3,
  definition,
  createdBy: 'tester',
  createdAt: '',
  updatedAt: '',
}
let root: ReturnType<typeof createRoot>, container: HTMLDivElement, client: QueryClient
const onClose = vi.fn()

beforeEach(() => {
  vi.clearAllMocks()
  Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true })
  vi.stubGlobal(
    'ResizeObserver',
    class {
      observe() {}
      unobserve() {}
      disconnect() {}
    },
  )
  const getComputedStyle = window.getComputedStyle.bind(window)
  vi.spyOn(window, 'getComputedStyle').mockImplementation((element) => getComputedStyle(element))
  window.matchMedia = vi.fn().mockReturnValue({
    matches: false,
    addListener: vi.fn(),
    removeListener: vi.fn(),
    addEventListener: vi.fn(),
    removeEventListener: vi.fn(),
  })
  vi.spyOn(deliveryApi.applications, 'detail').mockImplementation(async (id) => ({
    application: {
      id,
      name: id,
      key: id,
      group: '',
      language: 'Go',
      enabled: true,
      createdAt: '',
      updatedAt: '',
    },
    bindings: ['dev', 'prod'].map((environmentId) => ({
      applicationEnvironmentId: environmentId,
      environmentId,
      environmentName: environmentId,
      targetCount: 10,
      requiresApproval: environmentId === 'prod',
    })),
  }))
  vi.spyOn(deliveryApi.applications, 'services').mockImplementation(async (applicationId) =>
    Array.from({ length: 10 }, (_, index) => ({
      id: `service-${index}`,
      key: `service-${index}`,
      name: `服务 ${index + 1}`,
      applicationId,
      enabled: true,
      serviceKind: 'kubernetes_workload',
      createdAt: '',
      updatedAt: '',
    })),
  )
  vi.spyOn(deliveryApi.workflowTemplates, 'list').mockResolvedValue([])
  vi.spyOn(deliveryApi.releaseBundles, 'list').mockResolvedValue([])
  vi.spyOn(deliveryApi.documents, 'source').mockResolvedValue({})
  vi.spyOn(deliveryApi.deliveryWorkflows, 'create').mockImplementation(async (input) => ({
    ...workflow,
    definition: input.definition,
    version: 1,
  }))
  vi.spyOn(deliveryApi.deliveryWorkflows, 'update').mockImplementation(async (_id, input) => ({
    ...workflow,
    definition: input.definition,
    version: 4,
  }))
  vi.spyOn(deliveryApi.batches, 'create').mockRejectedValue(new Error('响应连接中断，请重试'))
  client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  })
  container = document.createElement('div')
  document.body.append(container)
  root = createRoot(container)
})
afterEach(async () => {
  await act(async () => root.unmount())
  await new Promise((resolve) => setTimeout(resolve, 500))
  client.clear()
  container.remove()
  vi.restoreAllMocks()
  vi.unstubAllGlobals()
})

it('saves a selected historical Helm revision and clears it when switching back to configuration', async () => {
  const onSave = vi.fn()
  vi.spyOn(deliveryApi.applications, 'list').mockResolvedValue([])
  const releaseHistory = kubernetesHelmQueries.releaseHistory
  vi.spyOn(kubernetesHelmQueries, 'releaseHistory').mockImplementation((target) => ({
    ...releaseHistory(target),
    queryFn: async () => [
      {
        name: 'api-release',
        namespace: 'test',
        revision: '1',
        status: 'superseded',
        chart: 'api',
        chartVersion: '1.0.0',
      },
      {
        name: 'api-release',
        namespace: 'test',
        revision: '2',
        status: 'deployed',
        chart: 'api',
        chartVersion: '2.0.0',
      },
    ],
  }))
  const application = await deliveryApi.applications.detail('app-0')
  vi.mocked(deliveryApi.applications.detail).mockResolvedValue({
    ...application,
    bindings: [
      {
        applicationEnvironmentId: 'dev',
        environmentId: 'dev',
        targetCount: 1,
        requiresApproval: false,
        targets: [
          {
            id: 'helm-target',
            clusterId: 'cluster',
            namespace: 'test',
            enabled: true,
            targetKind: 'helm_release',
            executorKind: 'helm_sdk',
            workloadKind: 'HelmRelease',
            workloadName: 'api-release',
            metadata: { serviceId: 'service-0' },
            helm: {
              releaseName: 'api-release',
              source: {
                repositoryUrl: 'https://charts.example.com',
                chart: 'api',
                version: '2.0.0',
                values: {},
              },
            },
          },
        ],
      },
    ],
  })
  await act(async () => {
    root.render(
      <QueryClientProvider client={client}>
        <DeliveryTargetEditor
          target={{
            id: 'api',
            applicationId: 'app-0',
            serviceId: 'service-0',
            applicationEnvironmentId: 'dev',
            action: 'config_update',
            helmRevision: 1,
          }}
          onClose={onClose}
          onSave={onSave}
        />
      </QueryClientProvider>,
    )
    await new Promise((resolve) => setTimeout(resolve, 30))
  })
  await act(async () => {
    await new Promise((resolve) => setTimeout(resolve, 30))
  })
  expect(kubernetesHelmQueries.releaseHistory).toHaveBeenCalledWith({
    clusterId: 'cluster',
    namespace: 'test',
    name: 'api-release',
  })
  await click('保存目标')
  expect(onSave).toHaveBeenLastCalledWith(
    expect.objectContaining({
      helmRevision: 1,
      releaseTargetId: 'helm-target',
      action: 'config_update',
    }),
  )
  const configuration = document.querySelector<HTMLInputElement>(
    'input[type="radio"][value="configuration"]',
  )!
  await act(async () => configuration.click())
  await click('保存目标')
  expect(onSave).toHaveBeenLastCalledWith(
    expect.objectContaining({ helmRevision: undefined, releaseTargetId: 'helm-target' }),
  )
})
async function renderEditor(saved?: DeliveryWorkflow) {
  await act(async () => {
    root.render(
      <ConfigProvider theme={{ token: { motion: false } }}>
        <App>
          <QueryClientProvider client={client}>
            <MemoryRouter>
              <DeliveryBatchEditor
                workflow={saved}
                initialDefinition={saved ? undefined : definition}
                onClose={onClose}
              />
            </MemoryRouter>
          </QueryClientProvider>
        </App>
      </ConfigProvider>,
    )
    await new Promise((resolve) => setTimeout(resolve, 20))
  })
}
async function click(label: string) {
  const button = Array.from(document.querySelectorAll('button')).find(
    (item) =>
      item.getAttribute('aria-label') === label ||
      item.textContent?.replace(/\s/g, '') === label.replace(/\s/g, ''),
  )
  expect(button, label).toBeDefined()
  await act(async () => {
    button!.click()
    await new Promise((resolve) => setTimeout(resolve, 20))
  })
}
async function clickReady(label: string) {
  await vi.waitFor(() => {
    const button = [...document.querySelectorAll('button')].find(
      (item) => item.textContent?.replace(/\s/g, '') === label.replace(/\s/g, ''),
    )
    expect(button, label).toBeDefined()
    expect(button?.disabled).toBe(false)
  })
  await click(label)
}
async function toggleFailure() {
  const checkbox = document.querySelector<HTMLInputElement>('input[type="checkbox"]')
  expect(checkbox?.checked).toBe(true)
  await act(async () => {
    checkbox!.click()
    await new Promise((resolve) => setTimeout(resolve, 0))
  })
}

it('saves all 20 ordered targets and failure policy without starting a batch', async () => {
  await renderEditor()
  expect(document.body.textContent).toContain('10 个服务 / 20 个环境目标')
  await click('下移第 1 项')
  await toggleFailure()
  await click('保存工作流')
  const payload = vi.mocked(deliveryApi.deliveryWorkflows.create).mock.calls[0][0]
  expect(payload.definition.targets).toHaveLength(20)
  expect(payload.definition.targets.slice(0, 2).map((item) => item.id)).toEqual([
    'target-1',
    'target-0',
  ])
  expect(payload.definition.stopOnFailure).toBe(false)
  expect(definition.targets[0].id).toBe('target-0')
  expect(deliveryApi.batches.create).not.toHaveBeenCalled()
  expect(document.body.textContent).toContain('工作流已保存')
}, 15_000)

it('shows the selected Helm rollback revision in the delivery target row', async () => {
  await renderEditor({
    ...workflow,
    definition: {
      ...definition,
      targets: [{ ...definition.targets[0], action: 'config_update', helmRevision: 7 }],
    },
  })
  expect(document.body.textContent).toContain('恢复 Helm 版本 7')
  expect(document.body.textContent).not.toContain('仅更新配置')
})

it('pins the saved version and keeps one idempotency key across uncertain retries until input changes', async () => {
  await renderEditor(workflow)
  await click('开始交付')
  expect(document.body.textContent).toContain('响应连接中断')
  await click('开始交付')
  const create = vi.mocked(deliveryApi.batches.create)
  const first = create.mock.calls[0][0]
  expect(first).toMatchObject({ workflowId: 'workflow-1', workflowVersion: 3 })
  expect(first.definition).toBeUndefined()
  expect(create.mock.calls[1][0].idempotencyKey).toBe(first.idempotencyKey)
  await toggleFailure()
  create.mockResolvedValueOnce({ id: 'new-batch' } as DeliveryBatch)
  await click('开始交付')
  expect(create.mock.calls[2][0].idempotencyKey).not.toBe(first.idempotencyKey)
  expect(create.mock.calls[2][0].definition?.stopOnFailure).toBe(false)
  expect(create.mock.calls[2][0].workflowId).toBeUndefined()
  expect(onClose).toHaveBeenCalledOnce()
  expect(navigation).toHaveBeenCalledWith('/delivery/batches/new-batch')
}, 15_000)

it('keeps a Git workflow read-only while allowing its saved version to run and a copy to be edited', async () => {
  vi.mocked(deliveryApi.documents.source).mockResolvedValue({
    association: {
      sourceId: 'source-1',
      kind: 'Workflow',
      objectId: workflow.id,
      key: 'release',
      path: 'release.soha.yaml',
      lastImportedRevision: 3,
      resolvedCommit: 'a'.repeat(40),
      sourceDigest: 'source',
      normalizedSpecDigest: 'spec',
      syncRunId: 'run-1',
      removed: false,
    },
  })
  await renderEditor(workflow)
  await act(async () => {
    await new Promise((resolve) => setTimeout(resolve, 30))
  })
  expect(document.querySelector<HTMLInputElement>('input[aria-label="工作流名称"]')?.disabled).toBe(
    true,
  )
  await click('保存工作流')
  expect(deliveryApi.deliveryWorkflows.update).not.toHaveBeenCalled()
  await click('开始交付')
  expect(deliveryApi.batches.create).toHaveBeenCalledWith(
    expect.objectContaining({ workflowId: workflow.id, workflowVersion: 3 }),
  )
  await click('复制为新工作流')
  expect(document.querySelector<HTMLInputElement>('input[aria-label="工作流名称"]')?.disabled).toBe(
    false,
  )
  await click('保存工作流')
  expect(deliveryApi.deliveryWorkflows.create).toHaveBeenCalledWith(
    expect.objectContaining({
      definition: expect.objectContaining({ name: `${workflow.definition.name} 副本` }),
    }),
  )
  expect(deliveryApi.deliveryWorkflows.update).not.toHaveBeenCalled()
}, 15_000)

it('checks service delivery inputs before execution and retains its idempotency key on an uncertain retry', async () => {
  const service = (await deliveryApi.applications.services('app-0'))[0]
  vi.spyOn(deliveryApi.batches, 'list').mockResolvedValue([])
  await act(async () => {
    root.render(
      <ConfigProvider theme={{ token: { motion: false } }}>
        <App>
          <QueryClientProvider client={client}>
            <MemoryRouter>
              <ServiceDeliveryForm
                service={service}
                applicationEnvironmentId="dev"
                onClose={onClose}
              />
            </MemoryRouter>
          </QueryClientProvider>
        </App>
      </ConfigProvider>,
    )
    await new Promise((resolve) => setTimeout(resolve, 30))
  })
  await clickReady('检查交付参数')
  await vi.waitFor(() => expect(document.body.textContent).toContain('确认交付参数'))
  expect(deliveryApi.batches.create).not.toHaveBeenCalled()
  await click('确认并开始')
  await click('确认并开始')
  const calls = vi.mocked(deliveryApi.batches.create).mock.calls
  expect(calls[0][0].definition?.targets).toEqual([
    expect.objectContaining({
      applicationId: 'app-0',
      serviceId: service.id,
      applicationEnvironmentId: 'dev',
      action: 'config_update',
    }),
  ])
  expect(calls[1][0].idempotencyKey).toBe(calls[0][0].idempotencyKey)
  expect(navigation).not.toHaveBeenCalled()
})

it('allows an embedded pure build without an environment', async () => {
  const onSave = vi.fn()
  vi.spyOn(deliveryApi.repositories, 'list').mockResolvedValue([])
  await act(async () => {
    root.render(
      <QueryClientProvider client={client}>
        <DeliveryTargetEditor
          fixed
          embedded
          target={{
            id: 'service',
            applicationId: 'app-0',
            serviceId: 'service-0',
            action: 'build',
          }}
          onSave={onSave}
          onClose={onClose}
        />
      </QueryClientProvider>,
    )
    await new Promise((resolve) => setTimeout(resolve, 30))
  })
  await clickReady('检查交付参数')
  await vi.waitFor(() =>
    expect(onSave).toHaveBeenCalledWith(
      expect.objectContaining({ action: 'build', applicationEnvironmentId: undefined }),
    ),
  )
})

it('retains the original idempotency receipt and locks a batch with an unknown POST outcome', async () => {
  vi.mocked(deliveryApi.batches.create).mockRejectedValueOnce(
    new ApiError(503, 'batch response lost', { method: 'POST', requestId: 'req-batch-unknown' }),
  )
  await renderEditor(workflow)
  await click('开始交付')
  expect(document.body.textContent).toContain('请求结果不明')
  expect(document.body.textContent).toContain('req-batch-unknown')
  expect(document.body.textContent).toContain('核对执行记录')
  await click('开始交付')
  expect(deliveryApi.batches.create).toHaveBeenCalledTimes(1)
  expect(vi.mocked(deliveryApi.batches.create).mock.calls[0][0].idempotencyKey).toBeTruthy()
})
