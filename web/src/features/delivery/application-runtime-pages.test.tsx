/** @vitest-environment jsdom */

import type { ReactNode } from 'react'
import { act } from 'react'
import { App as AntApp } from 'antd'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { createRoot } from 'react-dom/client'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { ApplicationDetailPage } from './application-runtime-pages'
import { api } from '@/services/api-client'

const workflowDefinition = {
  schemaVersion: 2,
  mode: 'release_dag',
  nodes: [
    { id: 'approval', type: 'manual_approval', name: '审批', position: { x: 120, y: 120 }, timeoutSeconds: 300, continueOnFailure: false, config: {} },
    { id: 'deploy', type: 'deploy_update_image', name: '更新镜像', position: { x: 320, y: 120 }, timeoutSeconds: 300, continueOnFailure: false, config: {} },
  ],
  edges: [
    { id: 'edge-1', source: 'approval', target: 'deploy', condition: 'success' },
  ],
}

const testState = vi.hoisted(() => ({
  permissionSnapshot: {
    permissionKeys: [
      'delivery.applications.view',
      'delivery.application.update',
      'delivery.application-environments.manage',
      'delivery.application-services.view',
      'delivery.application-services.manage',
      'delivery.builds.trigger',
      'delivery.workflows.trigger',
      'delivery.releases.trigger',
    ],
    visibleMenuIds: [],
    visibleMenus: [],
  },
  detailWithoutWorkflow: false,
  detailWithoutValidationNodes: false,
  detailWithoutImageTagDefaults: false,
  apiGet: vi.fn(async (path: string) => {
    if (path === '/applications') {
      return {
        data: [
          {
            id: 'app-1',
            name: 'Checkout Platform',
            key: 'checkout-platform',
            group: 'commerce',
            language: 'go',
            repositoryPath: 'commerce/checkout',
            defaultBranch: 'main',
            defaultTag: testState.detailWithoutImageTagDefaults ? undefined : 'latest',
            enabled: true,
            buildSources: [
              {
                id: 'source-api',
                name: 'API Dockerfile',
                type: 'repo_dockerfile',
                enabled: true,
                isDefault: true,
                defaultTag: testState.detailWithoutImageTagDefaults ? undefined : 'latest',
              },
            ],
            createdAt: '2026-05-01T00:00:00Z',
            updatedAt: '2026-05-10T00:00:00Z',
          },
        ],
      }
    }
    if (path === '/application-environments') {
      return {
        data: [
          {
            id: 'binding-test',
            applicationId: 'app-1',
            environmentId: 'env-test',
            workflowTemplateId: 'wf-template-1',
            buildPolicy: { sourceId: 'source-api', refType: 'branch' },
            releasePolicy: { actionKind: 'deploy', requiresApproval: false },
            resourceSelector: { matchLabels: { app: 'checkout-api' } },
            targets: [
              {
                id: 'target-1',
                applicationEnvironmentId: 'binding-test',
                clusterId: 'cluster-a',
                namespace: 'checkout-test',
                workloadKind: 'Deployment',
                workloadName: 'checkout-api',
                containerName: 'api',
                enabled: true,
              },
            ],
            createdAt: '2026-05-01T00:00:00Z',
            updatedAt: '2026-05-10T00:00:00Z',
          },
        ],
      }
    }
    if (path === '/workflow-templates') {
      return { data: [{ id: 'wf-template-1', key: 'release-dag', name: 'Release DAG', enabled: true, definition: workflowDefinition, createdAt: '2026-05-01T00:00:00Z', updatedAt: '2026-05-10T00:00:00Z' }] }
    }
    if (path === '/clusters') {
      return { data: [{ id: 'cluster-a', name: 'cluster-a', status: 'ready', createdAt: '2026-05-01T00:00:00Z', updatedAt: '2026-05-10T00:00:00Z' }] }
    }
    if (path === '/applications/app-1/runtime') {
      const defaultTag = testState.detailWithoutImageTagDefaults ? undefined : 'latest'
      return {
        data: {
          application: {
            id: 'app-1',
            name: 'Checkout Platform',
            key: 'checkout-platform',
            group: 'commerce',
            language: 'go',
            repositoryPath: 'commerce/checkout',
            defaultBranch: 'main',
            defaultTag,
            enabled: true,
            buildSources: [
              {
                id: 'source-api',
                name: 'API Dockerfile',
                type: 'repo_dockerfile',
                enabled: true,
                isDefault: true,
                defaultTag,
              },
            ],
            createdAt: '2026-05-01T00:00:00Z',
            updatedAt: '2026-05-10T00:00:00Z',
          },
          environments: [
            {
              applicationEnvironmentId: 'binding-test',
              environmentId: 'env-test',
              environmentName: '测试环境',
              requiresApproval: false,
              workloads: [
                {
                  applicationEnvironmentId: 'binding-test',
                  clusterId: 'cluster-a',
                  namespace: 'checkout-test',
                  workloadKind: 'Deployment',
                  workloadName: 'checkout-api',
                  desiredReplicas: 2,
                  readyReplicas: 2,
                  updatedReplicas: 2,
                  availableReplicas: 2,
                },
              ],
            },
          ],
        },
      }
    }
    if (path === '/applications/app-1/detail') {
      const defaultTag = testState.detailWithoutImageTagDefaults ? undefined : 'latest'
      return {
        data: {
          application: {
            id: 'app-1',
            name: 'Checkout Platform',
            key: 'checkout-platform',
            group: 'commerce',
            language: 'go',
            repositoryPath: 'commerce/checkout',
            defaultBranch: 'main',
            defaultTag,
            enabled: true,
            buildSources: [
              {
                id: 'source-api',
                name: 'API Dockerfile',
                type: 'repo_dockerfile',
                enabled: true,
                isDefault: true,
                defaultTag,
              },
            ],
            createdAt: '2026-05-01T00:00:00Z',
            updatedAt: '2026-05-10T00:00:00Z',
          },
          bindings: [
            {
              applicationEnvironmentId: 'binding-test',
              environmentId: 'env-test',
              environmentName: '测试环境',
              workflowTemplateId: 'wf-template-1',
              workflowTemplateName: 'Release DAG',
              workflowTemplate: testState.detailWithoutWorkflow ? undefined : {
                id: 'wf-template-1',
                key: 'release-dag',
                name: 'Release DAG',
                category: 'release',
                definition: testState.detailWithoutValidationNodes
                  ? {
                    ...workflowDefinition,
                    nodes: workflowDefinition.nodes.filter((node) => node.type !== 'check_http' && node.type !== 'check_k8s_event' && node.type !== 'smoke_test' && node.type !== 'verify' && node.type !== 'check'),
                  }
                  : {
                    ...workflowDefinition,
                    nodes: [
                      ...workflowDefinition.nodes,
                      { id: 'verify', type: 'check_http', name: 'HTTP 验证', config: { url: 'https://example.com/healthz' } },
                    ],
                  },
              },
              targetCount: 1,
              targets: [
                {
                  id: 'target-1',
                  applicationEnvironmentId: 'binding-test',
                  clusterId: 'cluster-a',
                  namespace: 'checkout-test',
                  workloadKind: 'Deployment',
                  workloadName: 'checkout-api',
                  containerName: 'api',
                  enabled: true,
                },
              ],
              latestBundle: {
                id: 'bundle-1',
                applicationId: 'app-1',
                applicationEnvironmentId: 'binding-test',
                version: '1.2.3',
                sourceType: 'build',
                status: 'completed',
                artifactRef: 'registry.example.com/checkout/api:1.2.3',
                artifactDigest: 'sha256:abc',
                createdAt: '2026-05-10T00:00:00Z',
                updatedAt: '2026-05-10T00:00:00Z',
              },
              latestExecutionTask: {
                id: 'task-1',
                applicationId: 'app-1',
                releaseBundleId: 'bundle-1',
                taskKind: 'build_release',
                providerKind: 'ci_agent_runner',
                targetKind: 'k8s_workload',
                status: 'completed',
                maxRetries: 1,
                attemptCount: 1,
                timeoutSeconds: 600,
                artifacts: [
                  { kind: 'image', name: 'checkout-api', ref: 'registry.example.com/checkout/api:1.2.3' },
                ],
                createdAt: '2026-05-10T00:00:00Z',
                updatedAt: '2026-05-10T00:00:00Z',
              },
              latestBuild: {
                id: 'build-1',
                applicationId: 'app-1',
                sourceSystem: 'application',
                status: 'completed',
                createdAt: '2026-05-10T00:00:00Z',
              },
              latestWorkflow: {
                id: 'workflow-1',
                applicationId: 'app-1',
                workflowName: 'release-dag',
                status: 'completed',
                steps: [],
                nodeRuns: [
                  { nodeId: 'approval', name: '审批', type: 'manual_approval', status: 'completed' },
                  { nodeId: 'deploy', name: '更新镜像', type: 'deploy_update_image', status: 'completed' },
                ],
                metadata: { nodes: workflowDefinition.nodes },
                createdAt: '2026-05-10T00:00:00Z',
                updatedAt: '2026-05-10T00:00:00Z',
              },
              latestRelease: {
                id: 'release-1',
                applicationId: 'app-1',
                clusterId: 'cluster-a',
                namespace: 'checkout-test',
                deploymentName: 'checkout-api',
                status: 'completed',
                createdAt: '2026-05-10T00:00:00Z',
              },
            },
          ],
          latestBundle: {
            id: 'bundle-1',
            applicationId: 'app-1',
            applicationEnvironmentId: 'binding-test',
            version: '1.2.3',
            sourceType: 'build',
            status: 'completed',
            artifactRef: 'registry.example.com/checkout/api:1.2.3',
            artifactDigest: 'sha256:abc',
            createdAt: '2026-05-10T00:00:00Z',
            updatedAt: '2026-05-10T00:00:00Z',
          },
          latestExecutionTask: {
            id: 'task-1',
            applicationId: 'app-1',
            releaseBundleId: 'bundle-1',
            taskKind: 'build_release',
            providerKind: 'ci_agent_runner',
            targetKind: 'k8s_workload',
            status: 'completed',
            maxRetries: 1,
            attemptCount: 1,
            timeoutSeconds: 600,
            artifacts: [
              { kind: 'image', name: 'checkout-api', ref: 'registry.example.com/checkout/api:1.2.3' },
            ],
            createdAt: '2026-05-10T00:00:00Z',
            updatedAt: '2026-05-10T00:00:00Z',
          },
          latestBuild: {
            id: 'build-1',
            applicationId: 'app-1',
            sourceSystem: 'application',
            status: 'completed',
            createdAt: '2026-05-10T00:00:00Z',
          },
          latestWorkflow: {
            id: 'workflow-1',
            applicationId: 'app-1',
            workflowName: 'release-dag',
            status: 'completed',
            steps: [],
            nodeRuns: [
              { nodeId: 'approval', name: '审批', type: 'manual_approval', status: 'completed' },
              { nodeId: 'deploy', name: '更新镜像', type: 'deploy_update_image', status: 'completed' },
            ],
            metadata: { nodes: workflowDefinition.nodes },
            createdAt: '2026-05-10T00:00:00Z',
            updatedAt: '2026-05-10T00:00:00Z',
          },
          latestRelease: {
            id: 'release-1',
            applicationId: 'app-1',
            clusterId: 'cluster-a',
            namespace: 'checkout-test',
            deploymentName: 'checkout-api',
            status: 'completed',
            createdAt: '2026-05-10T00:00:00Z',
          },
        },
      }
    }
    if (path === '/applications/app-1/services') {
      return {
        data: [
          {
            id: 'svc-api',
            applicationId: 'app-1',
            key: 'api',
            name: 'Checkout API',
            serviceKind: 'kubernetes_workload',
            ownerTeam: 'checkout-dev',
            repositoryPath: 'commerce/checkout/api',
            buildSourceId: 'source-api',
            enabled: true,
            containers: [
              {
                id: 'svc-api:api',
                serviceId: 'svc-api',
                name: 'api',
                imageRepository: 'registry.example.com/checkout/api',
                runtimePorts: [8080],
              },
            ],
            createdAt: '2026-05-10T00:00:00Z',
            updatedAt: '2026-05-10T00:00:00Z',
          },
        ],
      }
    }
    if (path === '/delivery/release-bundles/bundle-1/artifacts') {
      return {
        data: [
          {
            id: 'artifact-1',
            releaseBundleId: 'bundle-1',
            applicationId: 'app-1',
            kind: 'image',
            name: 'checkout-api',
            ref: 'registry.example.com/checkout/api:1.2.3',
            digest: 'sha256:abc',
            status: 'completed',
          },
        ],
      }
    }
    if (path === '/delivery/execution-tasks/task-1/artifacts') {
      return {
        data: [
          {
            id: 'artifact-2',
            executionTaskId: 'task-1',
            applicationId: 'app-1',
            kind: 'image',
            name: 'checkout-api',
            ref: 'registry.example.com/checkout/api:1.2.3',
            digest: 'sha256:abc',
            status: 'completed',
          },
        ],
      }
    }
    if (path === '/builds?applicationId=app-1') {
      return { items: [{ id: 'build-1', applicationId: 'app-1', sourceSystem: 'application', status: 'completed', createdAt: '2026-05-10T00:00:00Z' }] }
    }
    if (path === '/releases?applicationId=app-1') {
      return { items: [{ id: 'release-1', applicationId: 'app-1', clusterId: 'cluster-a', namespace: 'checkout-test', deploymentName: 'checkout-api', status: 'completed', createdAt: '2026-05-10T00:00:00Z' }] }
    }
    if (path === '/workflows?applicationId=app-1') {
      return {
        items: [
          {
            id: 'workflow-1',
            applicationId: 'app-1',
            workflowName: 'release-dag',
            status: 'completed',
            steps: [],
            nodeRuns: [
              { nodeId: 'approval', name: '审批', type: 'manual_approval', status: 'completed' },
              { nodeId: 'deploy', name: '更新镜像', type: 'deploy_update_image', status: 'completed' },
            ],
            metadata: { nodes: workflowDefinition.nodes },
            createdAt: '2026-05-10T00:00:00Z',
            updatedAt: '2026-05-10T00:00:00Z',
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

vi.mock('@/components/pod-log-viewer', () => ({
  PodLogViewer: () => null,
}))

vi.mock('@/components/pod-terminal', () => ({
  PodTerminal: () => null,
}))

vi.mock('@/components/resource-metrics-panel', () => ({
  ResourceMetricsPanel: () => null,
}))

vi.mock('@/services/api-client', () => ({
  api: {
    get: async (path: string) => {
      const body = await testState.apiGet(path)
      if (body && typeof body === 'object' && 'items' in body && !('data' in body)) {
        return { data: (body as { items: unknown }).items }
      }
      return body
    },
    post: vi.fn(async (path: string, body?: unknown) => ({ data: { id: 'ok', path, body } })),
    put: vi.fn(),
    delete: vi.fn(),
  },
}))

let containers: HTMLDivElement[] = []
let roots: Array<ReturnType<typeof createRoot>> = []

async function renderWithProviders(node: ReactNode, route = '/applications/app-1') {
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
      <AntApp>
        <QueryClientProvider client={queryClient}>
          <MemoryRouter initialEntries={[route]}>
            <Routes>
              <Route path="/applications/:applicationId" element={node} />
            </Routes>
          </MemoryRouter>
        </QueryClientProvider>
      </AntApp>,
    )
  })

  await act(async () => {
    await new Promise((resolve) => setTimeout(resolve, 0))
    await new Promise((resolve) => setTimeout(resolve, 0))
    await new Promise((resolve) => setTimeout(resolve, 0))
  })

  return container
}

function clickTab(container: HTMLElement, text: string) {
  const tab = Array.from(container.querySelectorAll('[role="tab"]')).find((item) => item.textContent?.includes(text)) as HTMLElement | undefined
  if (!tab) {
    throw new Error(`tab not found: ${text}`)
  }
  act(() => {
    tab.dispatchEvent(new MouseEvent('click', { bubbles: true, cancelable: true }))
  })
}

function findButton(container: HTMLElement, text: string) {
  const button = Array.from(container.querySelectorAll('button')).find((item) => item.textContent?.includes(text)) as HTMLButtonElement | undefined
  if (!button) {
    throw new Error(`button not found: ${text}`)
  }
  return button
}

describe('ApplicationDetailPage workbench', () => {
  beforeEach(() => {
    testState.apiGet.mockClear()
    testState.detailWithoutWorkflow = false
    testState.detailWithoutValidationNodes = false
    testState.detailWithoutImageTagDefaults = false
    vi.mocked(api.post).mockClear()
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

  it('renders overview, service, delivery artifact and verification views', async () => {
    const container = await renderWithProviders(<ApplicationDetailPage />)

    expect(testState.apiGet).toHaveBeenCalledWith('/applications/app-1/runtime')
    expect(testState.apiGet).toHaveBeenCalledWith('/applications/app-1/detail')
    expect(testState.apiGet).toHaveBeenCalledWith('/applications/app-1/services')
    expect(testState.apiGet).toHaveBeenCalledWith('/delivery/release-bundles/bundle-1/artifacts')
    expect(testState.apiGet).toHaveBeenCalledWith('/delivery/execution-tasks/task-1/artifacts')
    expect(testState.apiGet).toHaveBeenCalledWith('/builds?applicationId=app-1')
    expect(testState.apiGet).toHaveBeenCalledWith('/releases?applicationId=app-1')
    expect(testState.apiGet).toHaveBeenCalledWith('/workflows?applicationId=app-1')
    expect(testState.apiGet).toHaveBeenCalledWith('/applications')
    expect(testState.apiGet).toHaveBeenCalledWith('/application-environments')
    expect(testState.apiGet).not.toHaveBeenCalledWith('/delivery-environments')
    expect(testState.apiGet).toHaveBeenCalledWith('/workflow-templates')
    expect(testState.apiGet).toHaveBeenCalledWith('/clusters')
    expect(container.textContent).toContain('Checkout Platform')
    expect(container.textContent).toContain('总览')
    expect(container.textContent).toContain('配置')
    expect(container.textContent).toContain('服务组件')
    expect(container.textContent).toContain('流水线')
    expect(container.textContent).toContain('交付操作')
    expect(container.textContent).toContain('交付态势')
    expect(container.textContent).toContain('门禁状态')
    expect(container.textContent).toContain('候选版本')
    expect(container.textContent).toContain('环境矩阵')
    expect(container.textContent).toContain('构建完成')
    expect(container.textContent).toContain('可验证')
    expect(container.textContent).toContain('1.2.3')
    expect(container.textContent).toContain('个交付物线索')
    expect(container.textContent).toContain('构建并部署')
    expect(container.textContent).toContain('运行验证')

    clickTab(container, '配置')
    await act(async () => {
      await new Promise((resolve) => setTimeout(resolve, 0))
    })
    expect(container.textContent).toContain('应用配置')
    expect(container.textContent).toContain('编辑应用')
    expect(container.textContent).toContain('构建来源')
    expect(container.textContent).toContain('环境绑定')
    expect(container.textContent).toContain('新建绑定')
    expect(container.textContent).toContain('app=checkout-api')
    expect(container.querySelector('.soha-application-runtime-settings-grid')).not.toBeNull()

    clickTab(container, '交付物')
    await act(async () => {
      await new Promise((resolve) => setTimeout(resolve, 0))
    })
    expect(container.textContent).toContain('Release Bundle')
    expect(container.textContent).toContain('Task 交付物')
    expect(container.textContent).toContain('build-1')
    expect(container.textContent).toContain('release-1')
    expect(container.textContent).toContain('workflow-1')
    expect(container.querySelector('.soha-application-runtime-delivery-grid')).not.toBeNull()

    clickTab(container, '流水线')
    await act(async () => {
      await new Promise((resolve) => setTimeout(resolve, 0))
    })
    expect(container.textContent).toContain('DAG 模板')
    expect(container.textContent).toContain('最近工作流运行')
    expect(container.textContent).toContain('release-dag')
    expect(container.querySelector('.soha-application-runtime-pipeline-grid')).not.toBeNull()

    clickTab(container, '测试验证')
    await act(async () => {
      await new Promise((resolve) => setTimeout(resolve, 0))
    })
    expect(container.textContent).toContain('验证门禁')
    expect(container.textContent).toContain('DAG 节点数')
    expect(container.querySelector('.soha-application-runtime-verification-grid')).not.toBeNull()

    clickTab(container, '服务组件')
    await act(async () => {
      await new Promise((resolve) => setTimeout(resolve, 0))
    })
    expect(container.textContent).toContain('Checkout API')
    expect(container.textContent).toContain('registry.example.com/checkout/api')
    expect(container.querySelector('.soha-application-service-grid')).not.toBeNull()
    expect(container.querySelector('.soha-application-container-row')).not.toBeNull()
  })

  it('calls aggregated delivery action endpoint when build is clicked', async () => {
    const container = await renderWithProviders(<ApplicationDetailPage />)
    const buildButton = findButton(container, '构建')

    await act(async () => {
      buildButton.dispatchEvent(new MouseEvent('click', { bubbles: true, cancelable: true }))
      await new Promise((resolve) => setTimeout(resolve, 0))
    })

    expect(api.post).toHaveBeenCalledWith('/applications/app-1/delivery-actions', expect.objectContaining({
      action: 'build',
      applicationEnvironmentId: 'binding-test',
      targetId: 'target-1',
      buildSourceId: 'source-api',
      refType: 'branch',
      refName: 'main',
    }))
  })

  it('calls aggregated delivery action endpoint when deploy is clicked', async () => {
    const container = await renderWithProviders(<ApplicationDetailPage />)
    const deployButton = findButton(container, '部署')

    await act(async () => {
      deployButton.dispatchEvent(new MouseEvent('click', { bubbles: true, cancelable: true }))
      await new Promise((resolve) => setTimeout(resolve, 0))
    })

    expect(api.post).toHaveBeenCalledWith('/applications/app-1/delivery-actions', expect.objectContaining({
      action: 'deploy',
      applicationEnvironmentId: 'binding-test',
      targetId: 'target-1',
      imageTag: expect.any(String),
    }))
  })

  it('disables build and deploy when workflow template is missing', async () => {
    testState.detailWithoutWorkflow = true
    const container = await renderWithProviders(<ApplicationDetailPage />)

    expect(findButton(container, '构建并部署').disabled).toBe(true)
  })

  it('disables image-producing actions when image tag defaults are missing', async () => {
    testState.detailWithoutImageTagDefaults = true
    const container = await renderWithProviders(<ApplicationDetailPage />)

    expect(findButton(container, '构建').disabled).toBe(true)
    expect(findButton(container, '部署').disabled).toBe(true)
    expect(findButton(container, '构建并部署').disabled).toBe(true)
  })

  it('disables verification when workflow template has no validation nodes', async () => {
    testState.detailWithoutValidationNodes = true
    const container = await renderWithProviders(<ApplicationDetailPage />)

    expect(findButton(container, '运行验证').disabled).toBe(true)
  })
})
