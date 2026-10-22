/** @vitest-environment jsdom */
import { act, type ReactNode } from 'react'
import { createRoot } from 'react-dom/client'
import { ConfigProvider } from 'antd'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { deliveryApi } from '../api'
import { deliveryKeys } from '../keys'
import type { DeliveryBatch, DeliveryPlan } from '../types'
import { DeliveryBatchDetailPage } from './detail-page'
import { DeliveryBatchPlanDrawer } from './plan-drawer'
import { templateUpdateKeys } from '../template-updates/queries'
import type {
  TemplateUpdatePreview,
  TemplateUpdateCandidate,
} from '@opensoha/contracts/gen/ts/sohaapi'

vi.mock('@/features/auth', () => ({
  hasPermission: () => true,
  usePermissionSnapshot: () => ({ data: { data: {} } }),
}))

let root: ReturnType<typeof createRoot>, container: HTMLDivElement, client: QueryClient
const batch: DeliveryBatch = {
  id: 'batch',
  rootRunId: 'run',
  status: 'running',
  definition: { name: '应用交付', targets: [] },
  targets: [],
  nodes: [],
  serviceCount: 0,
  targetCount: 0,
  buildCount: 0,
  createdBy: 'tester',
  createdAt: '',
  updatedAt: '',
}

it('blocks a migration Plan even with a comment, then requires that comment for the fresh legal Plan', async () => {
  const migration = {
    fromVersion: 1,
    toVersion: 2,
    blocked: true,
    requiresApproval: true,
    steps: [],
    reason: '先完成前置版本',
  }
  const plan: DeliveryPlan = {
    id: 'migration',
    source: 'delivery_batch',
    status: 'waiting_approval',
    applicationId: 'app',
    applicationEnvironmentId: 'test',
    action: 'deploy',
    riskLevel: 'high',
    requiresApproval: true,
    createdAt: '',
    updatedAt: '',
    impact: { templateMigration: migration },
  }
  client.setQueryData(deliveryKeys.plans.detail(plan.id), plan)
  const approval = vi
    .spyOn(deliveryApi.plans, 'approval')
    .mockResolvedValue({ ...plan, status: 'draft' })
  await render(<DeliveryBatchPlanDrawer id={plan.id} onClose={() => undefined} />)
  const button = [...document.querySelectorAll('button')].find((item) =>
    item.textContent?.includes('批准此计划'),
  )!
  const area = document.querySelector<HTMLTextAreaElement>('textarea[aria-label="迁移审批意见"]')!
  expect(button.disabled).toBe(true)
  await act(async () => {
    Object.getOwnPropertyDescriptor(HTMLTextAreaElement.prototype, 'value')!.set!.call(
      area,
      '已核实备份与恢复方案',
    )
    area.dispatchEvent(new Event('input', { bubbles: true }))
  })
  expect(button.disabled).toBe(true)
  await act(async () => {
    client.setQueryData(deliveryKeys.plans.detail(plan.id), {
      ...plan,
      impact: { templateMigration: { ...migration, blocked: false } },
    })
    await new Promise((resolve) => setTimeout(resolve, 20))
  })
  expect(button.disabled).toBe(false)
  await act(async () => {
    button.click()
    await new Promise((resolve) => setTimeout(resolve, 20))
  })
  expect(approval).toHaveBeenCalledWith(plan.id, {
    action: 'approve',
    comment: '已核实备份与恢复方案',
  })
})

it('requires the exact frozen template comparison before allowing configuration approval', async () => {
  const plan: DeliveryPlan = {
    id: 'template-plan',
    source: 'delivery_batch',
    status: 'waiting_approval',
    applicationId: 'app',
    applicationEnvironmentId: 'prod',
    action: 'deploy',
    templateCandidateId: 'candidate',
    riskLevel: 'high',
    requiresApproval: true,
    createdAt: '',
    updatedAt: '',
    impact: { templatePreviewId: 'preview', configurationOnly: true },
  }
  const preview: TemplateUpdatePreview = {
    id: 'preview',
    kind: 'deployment',
    templateId: 'template',
    toVersion: 4,
    digest: 'frozen',
    configurationOnly: true,
    createdAt: '',
    expiresAt: '',
    candidates: [],
  }
  client.setQueryData(deliveryKeys.plans.detail(plan.id), plan)
  client.setQueryData(templateUpdateKeys.preview(preview.id), preview)
  await render(<DeliveryBatchPlanDrawer id={plan.id} onClose={() => undefined} />)
  const approve = [...document.querySelectorAll('button')].find(
    (button) => button.textContent?.replace(/\s/g, '') === '批准此计划',
  )!
  expect(approve.disabled).toBe(true)
  expect(document.body.textContent).toContain('模板配置采用计划')
  const candidate: TemplateUpdateCandidate = {
    id: 'candidate',
    target: {
      id: 'target',
      kind: 'deployment',
      templateId: 'template',
      applicationId: 'app',
      applicationName: 'App',
      serviceId: 'svc',
      serviceName: 'API',
      applicationEnvironmentId: 'prod',
      environmentName: 'Production',
      releaseTargetId: 'release',
      fromVersion: 1,
      mode: 'approval',
      requiresApproval: true,
      eligible: true,
      reason: '',
      independentOverrides: [],
      bindingRevision: 'binding',
      scope: 'environment',
    },
    toVersion: 4,
    digest: 'candidate-digest',
    parameters: {},
    cumulative: {
      fromVersion: 1,
      toVersion: 4,
      changes: [],
      renderStatus: 'available',
      diagnostics: [],
      historyGap: false,
    },
    versions: [],
    eligible: true,
    reason: '',
    requiresApproval: true,
    wave: 0,
  }
  await act(async () => {
    client.setQueryData(templateUpdateKeys.preview(preview.id), {
      ...preview,
      candidates: [candidate],
    })
    await new Promise((resolve) => setTimeout(resolve, 20))
  })
  expect(approve.disabled).toBe(false)
  expect(document.body.textContent).toContain('累计差异 v1 → v4')
  await act(async () => {
    client.setQueryData(templateUpdateKeys.preview(preview.id), {
      ...preview,
      candidates: [{ ...candidate, id: 'other-candidate' }],
    })
    await new Promise((resolve) => setTimeout(resolve, 20))
  })
  expect(approve.disabled).toBe(true)
})

beforeEach(() => {
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
    addListener() {},
    removeListener() {},
    addEventListener() {},
    removeEventListener() {},
  })
  client = new QueryClient({
    defaultOptions: { queries: { retry: false, staleTime: Infinity }, mutations: { retry: false } },
  })
  container = document.createElement('div')
  document.body.append(container)
  root = createRoot(container)
})
afterEach(async () => {
  await act(async () => root.unmount())
  client.clear()
  container.remove()
  vi.restoreAllMocks()
  vi.unstubAllGlobals()
})
async function render(content: ReactNode, path = '/delivery/batches/batch') {
  await act(async () => {
    root.render(
      <ConfigProvider theme={{ token: { motion: false } }}>
        <QueryClientProvider client={client}>
          <MemoryRouter initialEntries={[path]}>
            <Routes>
              <Route path="/delivery/batches/:batchId" element={content} />
            </Routes>
          </MemoryRouter>
        </QueryClientProvider>
      </ConfigProvider>,
    )
    await new Promise((resolve) => setTimeout(resolve, 20))
  })
}

it('hides whole-batch cancel and retry actions for a filtered history view', async () => {
  client.setQueryData(deliveryKeys.batches.detail('batch'), { ...batch, partialView: true })
  await render(<DeliveryBatchDetailPage />)
  expect(document.body.textContent).toContain('当前仅显示可查看的目标')
  expect(document.body.textContent).not.toContain('停止交付')
  await act(async () => {
    client.setQueryData(deliveryKeys.batches.detail('batch'), {
      ...batch,
      status: 'completed',
      partialView: true,
    })
    await new Promise((resolve) => setTimeout(resolve, 20))
  })
  expect(document.body.textContent).not.toContain('按参数重跑整个流程')
  await act(async () => {
    client.setQueryData(deliveryKeys.batches.detail('batch'), { ...batch, status: 'completed' })
    await new Promise((resolve) => setTimeout(resolve, 20))
  })
  expect(document.body.textContent).toContain('按参数重跑整个流程')
})

it('restores a build evidence drawer from a batch deep link', async () => {
  client.setQueryData(deliveryKeys.batches.detail('batch'), batch)
  const detail = vi.spyOn(deliveryApi.runtime, 'detail').mockResolvedValue({
    object: {
      id: 'build-deep',
      applicationId: 'app',
      sourceSystem: 'manual',
      status: 'failed',
      createdAt: '2026-09-12T00:00:00Z',
      metadata: {},
    },
    kind: 'build',
    permissions: { canView: true },
  } as never)
  await render(
    <DeliveryBatchDetailPage />,
    '/delivery/batches/batch?evidenceKind=build&evidenceId=build-deep',
  )
  await act(async () => {
    await new Promise((resolve) => setTimeout(resolve, 50))
  })
  await vi.waitFor(
    async () => {
      await act(async () => {
        await new Promise((resolve) => setTimeout(resolve, 0))
      })
      expect(detail).toHaveBeenCalledWith('build', 'build-deep')
      expect(document.querySelector('[role="dialog"]')?.textContent).toContain('build-deep')
    },
    { timeout: 3000 },
  )
  expect(document.querySelector('[role="dialog"]')?.textContent).not.toContain('返回列表')
})

it('shows the frozen Helm rollback revision in batch history', async () => {
  client.setQueryData(deliveryKeys.batches.detail('batch'), {
    ...batch,
    targets: [
      {
        target: {
          id: 'helm',
          applicationId: 'app',
          serviceId: 'api',
          action: 'config_update',
          helmRevision: 7,
        },
        applicationName: '商城',
        serviceName: 'API',
        environmentName: '生产',
      },
    ],
  })
  await render(<DeliveryBatchDetailPage />)
  expect(document.body.textContent).toContain('恢复 Helm 版本 7')
  expect(document.body.textContent).not.toContain('仅更新配置')
})

it('shows frozen plan evidence and submits approval for that plan without confirming a deployment', async () => {
  const plan: DeliveryPlan = {
    id: 'plan',
    source: 'delivery_batch',
    status: 'waiting_approval',
    applicationId: 'app',
    applicationEnvironmentId: 'prod',
    action: 'deploy',
    riskLevel: 'high',
    requiresApproval: true,
    createdAt: '2026-09-12T00:00:00Z',
    updatedAt: '2026-09-12T00:00:00Z',
    releaseBundleId: 'verified-bundle',
    targetSummary: 'cluster / prod / web',
    dockerSnapshots: [
      {
        deliveryPlanId: 'plan',
        applicationId: 'app',
        applicationEnvironmentId: 'prod',
        serviceId: 'api',
        targetId: 'docker',
        hostId: 'approved-host',
        projectId: 'project',
        projectDigest: 'sha256:project-config',
        renderedDigest: 'sha256:docker-render',
        releaseBundleId: 'verified-bundle',
        preflightOperationId: 'docker-preflight',
        deployOperationId: 'docker-deploy',
        expectedServices: ['api'],
        images: { api: 'app@sha256:docker-image' },
      },
    ],
    impact: { approval: [{ actorName: 'Requester', status: 'requested' }] },
    manifestSnapshots: [
      {
        deliveryPlanId: 'plan',
        targetId: 'target',
        bindingId: 'binding',
        bindingVersion: 1,
        applicationEnvironmentId: 'prod',
        packageId: 'package',
        revision: 4,
        revisionDigest: 'sha256:revision',
        packageUpdatedAt: '2026-09-12T00:00:00Z',
        rendererVersion: 'raw-yaml-v1',
        inputDigest: 'sha256:input',
        expectedGeneration: 0,
        clusterId: 'cluster',
        namespace: 'prod',
        sourceCommit: 'frozen-commit',
        renderedDigest: 'sha256:frozen',
        preflightTaskId: 'preflight-task',
        documents: [
          {
            index: 0,
            path: 'deployment.yaml',
            contentDigest: 'sha256:document',
            apiVersion: 'apps/v1',
            kind: 'Deployment',
            namespace: 'prod',
            name: 'web',
            content: 'image: app@sha256:frozen',
          },
        ],
        gitOpsDocuments: [
          {
            index: 0,
            path: 'git/service.yaml',
            contentDigest: 'sha256:git-child',
            apiVersion: 'v1',
            kind: 'Service',
            namespace: 'prod',
            name: 'web',
            content: 'kind: Service # frozen GitOps child',
          },
        ],
      },
    ],
  }
  const approved: DeliveryPlan = { ...plan, status: 'draft' }
  client.setQueryData(deliveryKeys.plans.detail('plan'), plan)
  vi.spyOn(deliveryApi.plans, 'detail').mockResolvedValue(approved)
  const approval = vi.spyOn(deliveryApi.plans, 'approval').mockResolvedValue(approved)
  const confirm = vi.spyOn(deliveryApi.plans, 'confirm')
  await render(<DeliveryBatchPlanDrawer id="plan" onClose={() => {}} />)
  expect(document.body.textContent).toContain('frozen-commit')
  expect(document.body.textContent).toContain('approved-host')
  expect(document.body.textContent).toContain('app@sha256:docker-image')
  expect(
    document.querySelector(
      'a[href="/compute/tasks/operations?domain=docker&view=logs&taskId=docker-preflight"]',
    ),
  ).not.toBeNull()
  expect(document.body.textContent).toContain('image: app@sha256:frozen')
  expect(document.body.textContent).toContain('frozen GitOps child')
  expect(document.body.textContent).toContain('查看待应用资源（2）')
  expect(document.body.textContent).toContain('已申请审批')
  expect(document.body.textContent).not.toContain('已拒绝')
  expect(
    document.querySelector('a[href="/delivery/execution-tasks/preflight-task"]'),
  ).not.toBeNull()
  const button = Array.from(document.querySelectorAll('button')).find((item) =>
    item.textContent?.includes('批准此计划'),
  )
  expect(button).toBeDefined()
  await act(async () => {
    button!.click()
    await new Promise((resolve) => setTimeout(resolve, 20))
  })
  expect(approval).toHaveBeenCalledWith('plan', { action: 'approve', comment: undefined })
  expect(confirm).not.toHaveBeenCalled()
})
