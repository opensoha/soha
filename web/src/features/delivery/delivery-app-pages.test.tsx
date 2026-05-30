/** @vitest-environment jsdom */

import type { ReactNode } from 'react'
import { act } from 'react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { createRoot } from 'react-dom/client'
import { MemoryRouter } from 'react-router-dom'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { ApplicationsPage, WorkflowsPage } from './delivery-app-pages'

const testState = vi.hoisted(() => ({
  permissionSnapshot: {
    permissionKeys: ['delivery.applications.view', 'delivery.application.create', 'delivery.application.update'],
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
            group: 'erp-front',
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
    if (path === '/business-lines') {
      return { data: [{ id: 'biz-1', name: 'ERP 业务线' }] }
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
            targets: [{ clusterId: 'cluster-a', namespace: 'erp-test', workloadName: 'erp-front', workloadKind: 'deployment' }],
            latestWorkflow: { id: 'wf-1', applicationId: 'app-1', workflowName: 'deploy', status: 'running', steps: [], createdAt: '2026-05-08T11:00:00Z', updatedAt: '2026-05-08T11:30:00Z' },
          },
          {
            applicationEnvironmentId: 'binding-2',
            applicationId: 'app-2',
            applicationName: 'Mall API',
            environmentId: 'env-staging',
            environmentName: '预发环境',
            requiresApproval: false,
            targets: [{ clusterId: 'cluster-b', namespace: 'mall-staging', workloadName: 'mall-api', workloadKind: 'deployment' }],
            latestWorkflow: { id: 'wf-2', applicationId: 'app-2', workflowName: 'deploy', status: 'unknown', steps: [], createdAt: '2026-05-08T11:00:00Z', updatedAt: '2026-05-08T11:30:00Z' },
          },
        ],
      }
    }
    if (path === '/build-templates') {
      return { data: [{ id: 'tpl-1', key: 'docker-node', name: 'Node Docker', enabled: true, createdAt: '2026-05-01T00:00:00Z', updatedAt: '2026-05-01T00:00:00Z' }] }
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
    expect(container.textContent).toContain('mall')
    expect(container.textContent).toContain('进入应用')
    expect(container.querySelector('.soha-application-card-grid')).not.toBeNull()
    expect(container.querySelector('.soha-application-create-card')).toBeNull()
    expect(container.textContent).not.toContain('erp/front/main')
    expect(container.textContent).not.toContain('应用管理')
    expect(container.textContent).not.toContain('围绕应用聚合研发、测试和交付上下文')
    expect(container.textContent).not.toContain('应用详细清单')
    expect(container.querySelector('.soha-admin-table-shell')).toBeNull()
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
})
