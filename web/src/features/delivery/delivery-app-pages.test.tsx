/** @vitest-environment jsdom */

import type { ReactNode } from 'react'
import { act } from 'react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { createRoot } from 'react-dom/client'
import { MemoryRouter } from 'react-router-dom'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { ApplicationsPage, ExecutionTasksPage, ReleaseBundlesPage, WorkflowsPage } from './delivery-app-pages'
import { defaultBuildSources } from './application-management-pages'
import { ReleaseBoardPage } from './delivery-catalog-pages'

const testState = vi.hoisted(() => ({
  permissionSnapshot: {
    permissionKeys: [
      'delivery.applications.view',
      'delivery.application.create',
      'delivery.application.update',
      'delivery.application-environments.view',
      'delivery.release-board.view',
    ],
    visibleMenuIds: [],
    visibleMenus: [],
  },
  apiGet: vi.fn(async (path: string) => {
    if (path === '/applications') {
      return {
        data: [
          {
            id: 'app-1',
            name: 'ERP Front Main',
            key: 'erp-front-main',
            group: 'erp-front, frontend',
            language: 'node',
            repositoryPath: 'erp/front/main',
            defaultBranch: 'main',
            enabled: true,
            buildSources: [
              {
                id: 'source-1',
                name: 'Repo Dockerfile',
                type: 'repo_dockerfile',
                enabled: true,
                isDefault: true,
                buildImage: '',
                defaultTag: '',
                config: { contextDir: '.', dockerfilePath: 'Dockerfile', builderKind: 'docker' },
              },
            ],
            createdAt: '2026-05-01T00:00:00Z',
            updatedAt: '2026-05-08T12:00:00Z',
          },
          {
            id: 'app-2',
            name: 'Mall API',
            key: 'mall-api',
            group: 'mall',
            language: 'go',
            repositoryPath: 'mall/api',
            defaultBranch: 'main',
            enabled: true,
            buildSources: [
              {
                id: 'source-2',
                name: 'Platform Template',
                type: 'platform_build_template',
                enabled: true,
                isDefault: true,
                buildImage: '',
                defaultTag: '',
                config: { buildTemplateId: 'tpl-1', contextDir: '.' },
              },
            ],
            createdAt: '2026-05-02T00:00:00Z',
            updatedAt: '2026-05-08T12:00:00Z',
          },
        ],
      }
    }
    if (path === '/application-environments') {
      return { data: [] }
    }
    if (path === '/workflow-templates') {
      return { data: [] }
    }
    if (path === '/clusters') {
      return { data: [] }
    }
    if (path === '/delivery/release-board') {
      return {
        data: [
          {
            applicationEnvironmentId: 'binding-1',
            applicationId: 'app-1',
            applicationName: 'ERP Front Main',
            environmentId: 'env-test',
            environmentName: '测试环境',
            requiresApproval: false,
            buildSource: { id: 'source-1', name: 'Repo Dockerfile', type: 'repo_dockerfile', enabled: true, isDefault: true },
            targets: [{ clusterId: 'cluster-a', namespace: 'erp-test', workloadName: 'erp-front', workloadKind: 'deployment' }],
            latestBuild: { id: 'build-1', applicationId: 'app-1', status: 'completed', sourceSystem: 'application', createdAt: '2026-05-08T10:00:00Z', updatedAt: '2026-05-08T10:30:00Z' },
            latestBundle: { id: 'bundle-1', applicationId: 'app-1', applicationEnvironmentId: 'binding-1', version: '1.2.3', sourceType: 'build', status: 'completed', artifactRef: 'registry.local/erp-front:1.2.3', createdAt: '2026-05-08T10:30:00Z', updatedAt: '2026-05-08T10:40:00Z' },
            latestExecutionTask: {
              id: 'task-1',
              releaseBundleId: 'bundle-1',
              applicationId: 'app-1',
              applicationEnvironmentId: 'binding-1',
              taskKind: 'build_deploy',
              providerKind: 'ci_agent_runner',
              targetKind: 'k8s_workload',
              status: 'running',
              maxRetries: 1,
              attemptCount: 1,
              timeoutSeconds: 600,
              artifacts: [{ kind: 'image', name: 'erp-front', ref: 'registry.local/erp-front:1.2.3' }],
              createdAt: '2026-05-08T10:40:00Z',
              updatedAt: '2026-05-08T11:20:00Z',
            },
            latestWorkflow: {
              id: 'wf-1',
              applicationId: 'app-1',
              workflowName: 'deploy',
              status: 'running',
              steps: [],
              nodeRuns: [{ nodeId: 'smoke', name: 'Smoke', type: 'smoke_test', status: 'completed' }],
              createdAt: '2026-05-08T11:00:00Z',
              updatedAt: '2026-05-08T11:30:00Z',
            },
            latestRelease: { id: 'release-1', applicationId: 'app-1', clusterId: 'cluster-a', namespace: 'erp-test', deploymentName: 'erp-front', status: 'running', createdAt: '2026-05-08T11:15:00Z', updatedAt: '2026-05-08T11:25:00Z' },
          },
          {
            applicationEnvironmentId: 'binding-2',
            applicationId: 'app-2',
            applicationName: 'Mall API',
            environmentId: 'env-staging',
            environmentName: '预发环境',
            requiresApproval: false,
            targets: [{ clusterId: 'cluster-b', namespace: 'mall-staging', workloadName: 'mall-api', workloadKind: 'deployment' }],
            latestWorkflow: { id: 'wf-2', applicationId: 'app-2', workflowName: 'deploy', status: 'failed', steps: [], createdAt: '2026-05-08T11:00:00Z', updatedAt: '2026-05-08T11:30:00Z' },
          },
        ],
      }
    }
    if (path === '/build-templates') {
      return { data: [{ id: 'tpl-1', key: 'docker-node', name: 'Node Docker', enabled: true, createdAt: '2026-05-01T00:00:00Z', updatedAt: '2026-05-01T00:00:00Z' }] }
    }
    if (path === '/delivery/release-bundles') {
      return {
        data: [
          {
            id: 'bundle-1',
            applicationId: 'app-1',
            applicationEnvironmentId: 'binding-1',
            version: '1.2.3',
            sourceType: 'build',
            status: 'completed',
            artifactRef: 'registry.local/erp-front:1.2.3',
            artifactDigest: 'sha256:123',
            createdAt: '2026-05-08T10:30:00Z',
            updatedAt: '2026-05-08T10:40:00Z',
          },
          {
            id: 'bundle-2',
            applicationId: 'app-2',
            applicationEnvironmentId: 'binding-2',
            version: '2.0.0-rc1',
            sourceType: 'workflow',
            status: 'failed',
            createdAt: '2026-05-08T10:30:00Z',
            updatedAt: '2026-05-08T10:40:00Z',
          },
        ],
      }
    }
    if (path === '/delivery/execution-tasks') {
      return {
        data: [
          {
            id: 'task-running',
            releaseBundleId: 'bundle-1',
            applicationId: 'app-1',
            applicationEnvironmentId: 'binding-1',
            taskKind: 'build_deploy',
            providerKind: 'ci_agent_runner',
            targetKind: 'k8s_workload',
            status: 'running',
            maxRetries: 1,
            attemptCount: 1,
            timeoutSeconds: 600,
            callbackToken: 'token-running',
            artifacts: [{ kind: 'image', name: 'erp-front', ref: 'registry.local/erp-front:1.2.3' }],
            lastHeartbeatAt: '2026-05-08T11:20:00Z',
            createdAt: '2026-05-08T10:40:00Z',
            updatedAt: '2026-05-08T11:20:00Z',
          },
          {
            id: 'task-failed',
            releaseBundleId: 'bundle-2',
            applicationId: 'app-2',
            applicationEnvironmentId: 'binding-2',
            taskKind: 'verify',
            providerKind: 'k8s_job_runner',
            targetKind: 'quality_gate',
            status: 'failed',
            maxRetries: 2,
            attemptCount: 1,
            timeoutSeconds: 300,
            artifacts: [],
            createdAt: '2026-05-08T10:40:00Z',
            updatedAt: '2026-05-08T11:20:00Z',
          },
        ],
      }
    }
    if (path === '/delivery/execution-tasks/task-running/logs' || path === '/delivery/execution-tasks/task-failed/logs') {
      return { data: [] }
    }
    if (path === '/workflows') {
      return {
        data: [
          {
            id: 'workflow-1',
            applicationId: 'app-1',
            workflowName: 'deploy-prod',
            clusterId: 'cluster-a',
            namespace: 'prod',
            deploymentName: 'erp-front',
            status: 'waiting_approval',
            steps: [],
            nodeRuns: [
              {
                nodeId: 'approve',
                name: '人工审批',
                type: 'manual_approval',
                status: 'waiting_approval',
                summary: 'Waiting for production approver',
                startedAt: '2026-05-08T11:10:00Z',
              },
            ],
            metadata: {
              aiGatewayApprovalRequestId: 'approval-1',
              aiGatewayToolName: 'delivery.actions.trigger',
              aiGatewayApprovalPolicyRef: 'policy-prod',
            },
            createdAt: '2026-05-08T11:00:00Z',
            updatedAt: '2026-05-08T11:30:00Z',
          },
        ],
      }
    }
    throw new Error(`Unhandled GET ${path}`)
  }),
}))

vi.mock('@/features/auth/permission-snapshot', () => ({
  hasPermission: (snapshot: { permissionKeys?: string[] } | undefined, key: string) => snapshot?.permissionKeys?.includes(key) ?? false,
  usePermissionSnapshot: () => ({
    data: { data: testState.permissionSnapshot },
    isLoading: false,
  }),
}))

vi.mock('@/services/api-client', () => ({
  api: {
    get: (path: string) => testState.apiGet(path),
    post: vi.fn(),
    put: vi.fn(),
    delete: vi.fn(),
  },
}))

vi.mock('@/i18n', () => ({
  useI18n: () => ({
    localeCode: 'zh_CN',
    t: (_key: string, fallback: string) => fallback,
  }),
}))

let containers: HTMLDivElement[] = []
let roots: Array<ReturnType<typeof createRoot>> = []

async function renderWithProviders(node: ReactNode, route = '/applications') {
  const container = document.createElement('div')
  document.body.appendChild(container)
  containers.push(container)

  const root = createRoot(container)
  roots.push(root)

  const queryClient = new QueryClient({
    defaultOptions: {
      queries: { retry: false },
    },
  })

  await act(async () => {
    root.render(
      <QueryClientProvider client={queryClient}>
        <MemoryRouter initialEntries={[route]}>
          {node}
        </MemoryRouter>
      </QueryClientProvider>,
    )
  })

  await act(async () => {
    await new Promise((resolve) => setTimeout(resolve, 0))
    await new Promise((resolve) => setTimeout(resolve, 0))
    await new Promise((resolve) => setTimeout(resolve, 0))
  })

  return container
}

describe('ApplicationsPage workspace layout', () => {
  beforeEach(() => {
    testState.apiGet.mockClear()
    class ResizeObserverMock {
      observe() {}
      unobserve() {}
      disconnect() {}
    }

    Object.defineProperty(window, 'matchMedia', {
      writable: true,
      value: vi.fn().mockImplementation(() => ({
        matches: false,
        media: '',
        onchange: null,
        addListener: vi.fn(),
        removeListener: vi.fn(),
        addEventListener: vi.fn(),
        removeEventListener: vi.fn(),
        dispatchEvent: vi.fn(),
      })),
    })

    Object.defineProperty(window, 'getComputedStyle', {
      writable: true,
      value: vi.fn().mockReturnValue({
        width: '0px',
        height: '0px',
        overflow: 'auto',
        getPropertyValue: () => '',
      }),
    })

    vi.stubGlobal('IS_REACT_ACT_ENVIRONMENT', true)
    vi.stubGlobal('ResizeObserver', ResizeObserverMock)
  })

  afterEach(async () => {
    await act(async () => {
      for (const root of roots) root.unmount()
    })
    roots = []
    for (const container of containers) container.remove()
    containers = []
    vi.clearAllMocks()
  })

  it('renders application-centered cards before the detailed table', async () => {
    const container = await renderWithProviders(<ApplicationsPage />)

    expect(container.textContent).toContain('新建应用')
    expect(container.textContent).toContain('ERP Front Main')
    expect(container.textContent).toContain('全部')
    expect(container.textContent).toContain('erp-front')
    expect(container.textContent).toContain('frontend')
    expect(container.textContent).toContain('mall')
    expect(container.textContent).toContain('交付: 执行中')
    expect(container.textContent).toContain('门禁: 等待执行')
    expect(container.textContent).toContain('交付: 失败待处理')
    expect(container.textContent).toContain('门禁: 阻塞')
    expect(container.querySelector('.soha-application-card-list')).not.toBeNull()
    expect(container.querySelector('.soha-application-center-toolbar')).not.toBeNull()
    expect(container.querySelector('.soha-application-card__more')).not.toBeNull()
    expect(container.querySelector('.soha-application-card .ant-card-actions')).toBeNull()
    expect(container.querySelector('.soha-management-detail-header')).toBeNull()
    expect(container.querySelector('.soha-application-create-card')).toBeNull()
    expect(container.textContent).not.toContain('erp/front/main')
    expect(container.textContent).not.toContain('Repo Dockerfile')
    expect(container.textContent).not.toContain('erp-front-main')
    expect(container.textContent).not.toContain('erp-front / frontend')
    expect(container.textContent).not.toContain('进入应用')
    expect(container.textContent).not.toContain('按应用统一维护配置')
    expect(container.textContent).not.toContain('应用管理')
    expect(container.textContent).not.toContain('围绕应用聚合研发、测试和交付上下文')
    expect(container.textContent).not.toContain('应用详细清单')
    expect(container.querySelector('.soha-admin-table-shell')).toBeNull()
  })

  it('seeds new applications with a repository dockerfile build source', () => {
    expect(defaultBuildSources()).toEqual([
      {
        id: '',
        name: 'Repository Dockerfile',
        type: 'repo_dockerfile',
        enabled: true,
        isDefault: true,
        buildImage: '',
        defaultTag: '',
        config: { contextDir: '.', dockerfilePath: 'Dockerfile', builderKind: 'docker' },
      },
    ])
  })

  it('shows Gateway approval drilldown context on workflow list', async () => {
    const container = await renderWithProviders(
      <WorkflowsPage />,
      '/workflows?workflowRunId=workflow-1&gatewayApprovalRequestId=approval-1',
    )

    expect(testState.apiGet).toHaveBeenCalledWith('/workflows')
    expect(container.textContent).toContain('已定位工作流 workflow-1')
    expect(container.textContent).toContain('gatewayApprovalRequestId=approval-1')
    expect(container.textContent).toContain('approval-1')
    expect(container.textContent).toContain('delivery.actions.trigger')
    expect(container.textContent).toContain('已定位')
    expect(container.textContent).toContain('Manual approval detail')
    expect(container.textContent).toContain('Workflow node timeline')
    expect(container.textContent).toContain('Raw trace')
    expect(container.textContent).toContain('approve')
    expect(container.textContent).toContain('Waiting for production approver')
  })

  it('renders unified release board workbench signals', async () => {
    const container = await renderWithProviders(<ReleaseBoardPage />, '/release-board')

    expect(testState.apiGet).toHaveBeenCalledWith('/delivery/release-board')
    for (const scope of ['开发', '测试']) {
      expect(container.textContent).not.toContain(`${scope}视角`)
    }
    expect(container.textContent).toContain('环境绑定')
    expect(container.textContent).toContain('2 个发布目标')
    expect(container.textContent).toContain('ERP Front Main')
    expect(container.textContent).toContain('Repo Dockerfile')
    expect(container.textContent).toContain('候选版本')
    expect(container.textContent).toContain('交付态势')
    expect(container.textContent).toContain('交付物')
    expect(container.textContent).toContain('1.2.3')
    expect(container.textContent).toContain('执行中')
    expect(container.textContent).toContain('阻塞')
    expect(container.textContent).toContain('Task')
  })

  it('renders execution task summary for delivery triage', async () => {
    const container = await renderWithProviders(<ExecutionTasksPage />, '/delivery/execution-tasks')

    expect(testState.apiGet).toHaveBeenCalledWith('/delivery/execution-tasks')
    expect(container.textContent).toContain('任务总数')
    expect(container.textContent).toContain('1 个执行中')
    expect(container.textContent).toContain('阻塞任务')
    expect(container.textContent).toContain('1 个可重试')
    expect(container.textContent).toContain('交付物线索')
    expect(container.textContent).toContain('回调可用')
    expect(container.textContent).toContain('task-running')
    expect(container.textContent).toContain('binding-1')
    expect(container.textContent).toContain('1 · erp-front')
    expect(container.textContent).toContain('task-failed')
  })

  it('renders release bundle candidate summary', async () => {
    const container = await renderWithProviders(<ReleaseBundlesPage />, '/delivery/release-bundles')

    expect(testState.apiGet).toHaveBeenCalledWith('/delivery/release-bundles')
    expect(container.textContent).toContain('候选版本')
    expect(container.textContent).toContain('1 个可验证 / 可推广')
    expect(container.textContent).toContain('阻塞版本')
    expect(container.textContent).toContain('缺少交付物')
    expect(container.textContent).toContain('1.2.3')
    expect(container.textContent).toContain('bundle-1')
    expect(container.textContent).toContain('registry.local/erp-front:1.2.3')
    expect(container.textContent).toContain('2.0.0-rc1')
  })
})
