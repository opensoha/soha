import {
  ServicePodWorkspace,
  ServiceRuntimeSummary,
  ServiceRuntimeActions,
} from './runtime/service-pod-workspace'
/** @vitest-environment jsdom */

import type { ReactNode } from 'react'
import { act } from 'react'
import { App as AntApp } from 'antd'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { createRoot } from 'react-dom/client'
import { MemoryRouter, Route, Routes, useLocation, useSearchParams } from 'react-router-dom'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { BuildDetailPage } from './builds/detail-page'
import { ExecutionTaskDetailPage } from './execution-tasks/detail-page'
import { ReleaseDetailPage } from './releases/detail-page'
import { ApplicationWorkloadDetailPage } from './runtime/workload-detail-page'
import { I18nProvider } from '@/i18n'
import { api } from '@/services/api-client'
import { ApiError } from '@/services/api-error'
import { RuntimeDetailContent } from './runtime-detail/shared-page'
import { usePreferencesStore } from '@/stores/preferences-store'

const testState = vi.hoisted(() => ({
  openSession: vi.fn(),
  missingKind: '' as '' | 'build' | 'workflow' | 'release' | 'release_bundle' | 'execution_task',
  workloadRuntimePending: false,
  workloadRuntimeError: false,
  workloadRuntimeNotFound: false,
  workloadPodsEmpty: false,
  secondPod: false,
  manyPods: false,
  podDataError: false,
  hideOriginalPod: false,
  metricsAllowed: true,
  appAllowed: true,
  configuredMetrics: false,
  production: false,
  workloadAccessEmpty: false,
  workloadLinkedService: false,
  workloadServiceError: false,
  apiGet: vi.fn(async (path: string) => {
    if (path === '/delivery/runtime/workflows/workflow-cache')
      return {
        data: {
          kind: 'workflow',
          id: 'workflow-cache',
          object: {
            id: 'workflow-cache',
            workflowName: 'protected cached workflow',
            status: 'completed',
            nodeRuns: [],
            steps: [],
          },
        },
      }
    if (path.includes('/workloads/pods/') && path.includes('/detail?'))
      return {
        data: {
          containers: [{ name: 'checkout-api' }, { name: 'telemetry-sidecar', role: 'init' }],
          allowedActions: ['logs', 'exec'],
        },
      }
    if (path.includes('/workloads/pods/') && path.includes('/yaml?')) {
      if (testState.podDataError) throw new Error('Pod YAML unavailable')
      const name = path.split('/pods/')[1].split('/yaml')[0]
      return {
        data: {
          content: JSON.stringify({
            kind: 'Pod',
            metadata: { name, namespace: 'checkout' },
            spec: {
              initContainers: [{ name: 'prepare', image: 'init:1' }],
              ephemeralContainers: [{ name: 'debugger', image: 'debug:1' }],
              containers: [
                {
                  name: 'checkout-api',
                  image: 'actual-pod:1.2.3',
                  ports: [{ name: 'http', containerPort: 8080 }],
                  livenessProbe: { httpGet: { path: '/live', port: 'http' } },
                  readinessProbe: { tcpSocket: { port: 8080 } },
                  startupProbe: { exec: { command: ['sh', '-c', 'test -f /ready'] } },
                },
                { name: 'telemetry-sidecar', image: 'telemetry:1' },
              ],
            },
            status: {
              podIP: '10.42.0.18',
              containerStatuses: [
                {
                  name: 'checkout-api',
                  restartCount: 3,
                  lastState: {
                    terminated: {
                      reason: 'OOMKilled',
                      exitCode: 137,
                      finishedAt: '2026-09-09T08:00:00Z',
                    },
                  },
                },
              ],
            },
          }),
        },
      }
    }
    if (path.includes('/events?')) {
      if (testState.podDataError) throw new Error('events unavailable')
      return {
        data: [
          {
            name: 'old',
            type: 'Normal',
            reason: 'Scheduled',
            involvedKind: 'Pod',
            involvedName: 'checkout-api-7d9f6b7c5f-x2k9m',
            message: 'Older event',
            count: 1,
            ageSeconds: 300,
          },
          {
            name: 'new',
            type: 'Warning',
            reason: 'BackOff',
            involvedKind: 'Pod',
            involvedName: 'checkout-api-7d9f6b7c5f-x2k9m',
            message: 'Newest event',
            count: 3,
            ageSeconds: 10,
          },
          {
            name: 'other',
            type: 'Normal',
            reason: 'Started',
            involvedKind: 'Pod',
            involvedName: 'other-pod',
            message: 'Unrelated event',
            count: 1,
            ageSeconds: 5,
          },
        ],
      }
    }
    const application = {
      id: 'app-1',
      name: 'Checkout Platform',
      key: 'checkout-platform',
      group: 'commerce',
      language: 'go',
      enabled: true,
      createdAt: '2026-05-01T00:00:00Z',
      updatedAt: '2026-05-08T00:00:00Z',
    }
    const binding = {
      id: 'binding-1',
      applicationId: 'app-1',
      environmentId: 'env-1',
      environmentKey: 'test',
      workflowTemplateId: 'wf-template-1',
      buildPolicy: { sourceId: 'source-api' },
      releasePolicy: { requiresApproval: false },
      targets: [],
      createdAt: '2026-05-01T00:00:00Z',
      updatedAt: '2026-05-08T00:00:00Z',
    }
    const environment = {
      id: 'env-1',
      key: 'test',
      name: '测试环境',
      stageLevel: 1,
      sortOrder: 1,
      isProduction: testState.production,
      requiresApproval: false,
      enabled: true,
      createdAt: '2026-05-01T00:00:00Z',
      updatedAt: '2026-05-08T00:00:00Z',
    }
    if (path === '/delivery/environments') return { data: [environment] }
    if (path === '/applications/app-1/runtime')
      return {
        data: {
          application,
          services: testState.workloadServiceError
            ? undefined
            : testState.workloadLinkedService
              ? [
                  {
                    id: 'svc-api',
                    applicationId: 'app-1',
                    key: 'api',
                    name: 'Checkout service',
                    serviceKind: 'kubernetes_workload',
                    enabled: true,
                  },
                ]
              : [],
          environments: [
            {
              applicationEnvironmentId: 'binding-1',
              environmentId: 'env-1',
              environmentName: '测试环境',
              workloads: testState.workloadLinkedService
                ? [
                    {
                      serviceId: 'svc-api',
                      serviceKey: 'api',
                      workloadName: 'checkout-api',
                      workloadKind: 'Deployment',
                      namespace: 'checkout',
                    },
                  ]
                : [],
            },
          ],
        },
      }
    if (path === '/applications/app-1/services') {
      if (testState.workloadServiceError) throw new Error('catalog unavailable')
      return { data: [] }
    }
    const workflowTemplate = {
      id: 'wf-template-1',
      key: 'release-dag',
      name: 'Release DAG',
      enabled: true,
      createdAt: '2026-05-01T00:00:00Z',
      updatedAt: '2026-05-08T00:00:00Z',
    }
    const links = {
      application: '/applications/app-1?tab=delivery',
      audit: '/system/audit?metadataKey=runtime.execution_task.id&metadataValue=task-1',
      operations: '/system/operations?metadataKey=runtime.execution_task.id&metadataValue=task-1',
      artifacts: '/delivery/artifacts?executionTaskId=task-1',
    }
    const permissions = {
      canViewArtifacts: true,
      canViewAudit: true,
      canViewOperations: true,
      canRetry: true,
      canCancel: true,
    }
    if (path === '/delivery/runtime/builds/build-1') {
      const object = {
        id: 'build-1',
        applicationId: 'app-1',
        sourceSystem: 'application',
        status: 'completed',
        metadata: {
          applicationEnvironmentId: 'binding-1',
          externalPipeline: {
            runId: '101',
            url: 'https://gitlab.example.com/team/app/-/pipelines/101',
            status: 'success',
            stopConfirmed: true,
            artifactJobId: '9',
            artifactDigest: 'sha256:report',
          },
          imageDigest: 'sha256:image',
        },
        createdAt: '2026-05-08T00:00:00Z',
        updatedAt: '2026-05-08T01:00:00Z',
      }
      return {
        data: {
          kind: 'build',
          id: 'build-1',
          object,
          application,
          binding,
          environment,
          workflowTemplate,
          artifacts: [],
          evidence: {},
          links: { ...links, artifacts: '/delivery/artifacts' },
          permissions,
        },
      }
    }
    if (path === '/delivery/runtime/releases/release-1') {
      const object = {
        id: 'release-1',
        applicationId: 'app-1',
        clusterId: 'cluster-a',
        namespace: 'checkout',
        deploymentName: 'checkout-api',
        status: 'completed',
        metadata: { applicationEnvironmentId: 'binding-1' },
        createdAt: '2026-05-08T00:00:00Z',
        updatedAt: '2026-05-08T01:00:00Z',
      }
      return {
        data: {
          kind: 'release',
          id: 'release-1',
          object,
          application,
          binding,
          environment,
          workflowTemplate,
          artifacts: [],
          evidence: {},
          links,
          permissions,
        },
      }
    }
    if (path === '/delivery/runtime/execution-tasks/task-1') {
      const object = {
        id: 'task-1',
        releaseBundleId: 'bundle-1',
        applicationId: 'app-1',
        applicationEnvironmentId: 'binding-1',
        taskKind: 'build_release',
        providerKind: 'ci_agent_runner',
        targetKind: 'k8s_workload',
        status: 'running',
        result: {
          externalPipeline: {
            runId: '202',
            url: 'javascript:alert(1)',
            status: 'canceling',
            stopConfirmed: false,
          },
          externalPipelineError: '等待外部执行器停止',
        },
        maxRetries: 1,
        attemptCount: 1,
        timeoutSeconds: 600,
        createdAt: '2026-05-08T00:00:00Z',
        updatedAt: '2026-05-08T01:00:00Z',
      }
      const artifacts = [
        {
          id: 'artifact-2',
          kind: 'image',
          name: 'checkout-api',
          ref: 'registry.example.com/checkout/api:1.2.3',
          status: 'completed',
        },
      ]
      const evidence = {
        logs: [
          {
            id: 'log-1',
            logLevel: 'info',
            message: 'task running',
            createdAt: '2026-05-08T01:00:00Z',
          },
        ],
      }
      return {
        data: {
          kind: 'execution_task',
          id: 'task-1',
          object,
          application,
          binding,
          environment,
          workflowTemplate,
          artifacts,
          evidence,
          links,
          permissions,
        },
      }
    }
    if (
      path ===
      '/applications/app-1/application-environments/binding-1/workloads/checkout-api/runtime'
    ) {
      if (testState.workloadRuntimePending) await new Promise<never>(() => {})
      if (testState.workloadRuntimeError) throw new Error('runtime unavailable')
      if (testState.workloadRuntimeNotFound) {
        const error = new Error('not found') as Error & { status: number }
        error.status = 404
        throw error
      }
      return {
        data: {
          application,
          binding,
          environment,
          workload: {
            applicationEnvironmentId: 'binding-1',
            clusterId: 'cluster-a',
            namespace: 'checkout',
            workloadKind: 'Deployment',
            workloadName: 'checkout-api',
            desiredReplicas: 2,
            readyReplicas: 2,
            updatedReplicas: 2,
            availableReplicas: 2,
          },
          deployment: {
            allowedActions: ['scale'],
            name: 'checkout-api',
            namespace: 'checkout',
            desiredReplicas: 2,
            readyReplicas: 2,
            updatedReplicas: 2,
            availableReplicas: 2,
            observedGeneration: 3,
            strategy: 'RollingUpdate',
            labels: { app: 'checkout-api' },
            relatedResources: [
              {
                kind: 'ConfigMap',
                name: 'checkout-config',
                namespace: 'checkout',
                relation: 'config',
              },
              {
                kind: 'Secret',
                name: 'checkout-secret',
                namespace: 'checkout',
                relation: 'secret',
              },
              {
                kind: 'PersistentVolumeClaim',
                name: 'checkout-data',
                namespace: 'checkout',
                relation: 'volume',
              },
              ...(testState.workloadAccessEmpty
                ? []
                : [
                    {
                      kind: 'HTTPRoute',
                      name: 'checkout-route',
                      namespace: 'checkout',
                      relation: 'routes-service',
                    },
                  ]),
            ],
            containers: [
              {
                name: 'checkout-api',
                image: 'registry.example.com/checkout/api:1.2.3',
              },
              {
                name: 'telemetry-sidecar',
                image: 'registry.example.com/observability/agent:2.0.0',
                role: 'sidecar',
              },
            ],
          },
          pods: testState.workloadPodsEmpty
            ? []
            : [
                {
                  name: 'checkout-api-7d9f6b7c5f-x2k9m',
                  namespace: 'checkout',
                  phase: 'Running',
                  nodeName: 'worker-01',
                  podIp: '10.42.0.18',
                  readyContainers: '2/2',
                  restarts: 0,
                  ageSeconds: 3600,
                },
              ]
                .filter(
                  (pod) =>
                    !testState.hideOriginalPod || pod.name !== 'checkout-api-7d9f6b7c5f-x2k9m',
                )
                .concat(
                  testState.secondPod || testState.manyPods
                    ? [
                        {
                          name: 'checkout-api-new',
                          namespace: 'checkout',
                          phase: 'Running',
                          nodeName: 'worker-02',
                          podIp: '10.42.0.19',
                          readyContainers: '2/2',
                          restarts: 0,
                          ageSeconds: 600,
                        },
                      ]
                    : [],
                )
                .concat(
                  testState.manyPods
                    ? Array.from({ length: 14 }, (_, i) => ({
                        name: `pod-${i}`,
                        namespace: 'checkout',
                        phase:
                          i === 0
                            ? 'Pending'
                            : i === 1
                              ? 'Failed'
                              : i === 2
                                ? 'Unknown'
                                : 'Running',
                        readyContainers: i === 3 ? '0/1' : '1/1',
                        nodeName: 'node',
                        podIp: '',
                        restarts: 0,
                        ageSeconds: 10,
                      }))
                    : [],
                ),
          services: testState.workloadAccessEmpty
            ? []
            : [
                {
                  name: 'checkout-api',
                  namespace: 'checkout',
                  type: 'ClusterIP',
                  clusterIp: '10.43.18.24',
                  ports: ['http:8080/TCP'],
                  ageSeconds: 3600,
                },
              ],
          ingresses: testState.workloadAccessEmpty
            ? []
            : [
                {
                  name: 'checkout-public',
                  namespace: 'checkout',
                  className: 'nginx',
                  hosts: ['checkout.example.com'],
                  address: '10.0.0.20',
                  backendServices: ['checkout-api'],
                  ageSeconds: 3600,
                },
              ],
        },
      }
    }
    if (path.startsWith('/clusters/cluster-a/workloads/pods/') && path.includes('/metrics?')) {
      return {
        data: {
          configured: testState.configuredMetrics,
          source: 'prometheus',
          generatedAt: '2026-09-10T08:00:00Z',
          rangeMinutes: 60,
          stepSeconds: 60,
          series: testState.configuredMetrics
            ? [
                {
                  key: 'cpu',
                  label: 'CPU',
                  unit: 'cores',
                  latest: 0.12,
                  points: [
                    { timestamp: 'a', value: 0.1 },
                    { timestamp: 'b', value: 0.12 },
                  ],
                },
                {
                  key: 'memory',
                  label: 'Memory',
                  unit: 'bytes',
                  latest: 1048576,
                  points: [
                    { timestamp: 'a', value: 524288 },
                    { timestamp: 'b', value: 1048576 },
                  ],
                },
              ]
            : [],
        },
      }
    }
    throw new Error(`Unhandled GET ${path}`)
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

vi.mock('@/services/api-error', async (importOriginal) => ({
  ...(await importOriginal<typeof import('@/services/api-error')>()),
  isApiError: (error: unknown) => Boolean(error && typeof error === 'object' && 'status' in error),
}))

vi.mock('@/features/auth/permission-snapshot', () => ({
  usePermissionSnapshot: () => ({
    data: {
      data: {
        permissionKeys: [
          'delivery.applications.view',
          'delivery.workflows.view',
          'delivery.releases.view',
          'delivery.release-bundles.view',
          'delivery.execution-tasks.view',
        ],
      },
    },
    isLoading: false,
  }),
  hasPermission: (_snapshot: unknown, permission: string) =>
    (permission !== 'platform.pods.view' || testState.metricsAllowed) &&
    (permission !== 'delivery.applications.view' || testState.appAllowed),
}))

vi.mock('@/features/platform', async (importOriginal) => ({
  ...(await importOriginal<typeof import('@/features/platform')>()),
  useRealtimeSessionDock: () => ({ openSession: testState.openSession }),
  useClusterCapabilityForCluster: () => ({ disabled: false }),
}))

vi.mock('@/components/pod-log-viewer', () => ({
  PodLogViewer: ({
    container,
    podName,
    toolbarExtra,
  }: {
    container?: string
    podName: string
    toolbarExtra?: ReactNode
  }) => (
    <>
      <div data-testid="pod-log-viewer">{`${podName}:${container ?? ''}`}</div>
      {toolbarExtra}
    </>
  ),
}))

vi.mock('@/components/pod-terminal', () => ({
  PodTerminal: ({
    container,
    podName,
    toolbarContent,
  }: {
    container?: string
    podName: string
    toolbarContent?: ReactNode
  }) => (
    <div data-testid="pod-terminal">
      {toolbarContent}
      {`${podName}:${container ?? ''}`}
    </div>
  ),
}))

vi.mock('@/components/overview/overview-chart-slot', () => ({
  OverviewChartSlot: ({ chart }: { chart: { data: number[] } }) => (
    <span data-testid="pod-trend">{chart.data.join(',')}</span>
  ),
}))

vi.mock('@/components/resource-metrics-panel', () => ({
  ResourceMetricsPanel: () => <div data-testid="resource-metrics-panel">metrics</div>,
}))

let queryClients: QueryClient[] = []
let containers: HTMLDivElement[] = []
let roots: Array<ReturnType<typeof createRoot>> = []

function LocationProbe() {
  const location = useLocation()
  return <output data-testid="location">{location.pathname + location.search}</output>
}

async function renderWithProviders(node: ReactNode, route: string) {
  const container = document.createElement('div')
  document.body.appendChild(container)
  containers.push(container)
  const root = createRoot(container)
  roots.push(root)
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  queryClients.push(queryClient)
  await act(async () => {
    root.render(
      <I18nProvider>
        <AntApp>
          <QueryClientProvider client={queryClient}>
            <MemoryRouter initialEntries={[route]}>
              <Routes>
                <Route path="/applications/:applicationId" element={<LocationProbe />} />
                <Route path="/builds/:buildId" element={node} />
                <Route path="/workflows/:workflowId" element={node} />
                <Route path="/releases/:releaseId" element={node} />
                <Route path="/delivery/release-bundles/:releaseBundleId" element={node} />
                <Route path="/delivery/execution-tasks/:executionTaskId" element={node} />
                <Route
                  path="/applications/:applicationId/application-environments/:applicationEnvironmentId/workloads/:workloadName"
                  element={node}
                />
              </Routes>
            </MemoryRouter>
          </QueryClientProvider>
        </AntApp>
      </I18nProvider>,
    )
  })
  for (let index = 0; index < 6; index += 1) {
    await act(async () => {
      await new Promise((resolve) => setTimeout(resolve, 0))
    })
  }
  return container
}

describe('delivery runtime detail pages', () => {
  beforeEach(() => {
    usePreferencesStore.setState({ localeCode: 'zh_CN' })
    testState.apiGet.mockClear()
    testState.openSession.mockClear()
    testState.workloadRuntimePending = false
    testState.workloadRuntimeError = false
    testState.workloadRuntimeNotFound = false
    testState.workloadPodsEmpty = false
    testState.secondPod = false
    testState.manyPods = false
    testState.podDataError = false
    testState.hideOriginalPod = false
    testState.metricsAllowed = true
    testState.appAllowed = true
    testState.configuredMetrics = false
    testState.production = false
    vi.stubGlobal('IS_REACT_ACT_ENVIRONMENT', true)
    testState.workloadAccessEmpty = false
    testState.workloadLinkedService = false
    testState.workloadServiceError = false
    class ResizeObserverMock {
      observe() {}
      unobserve() {}
      disconnect() {}
    }
    vi.stubGlobal('ResizeObserver', ResizeObserverMock)
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
  })

  afterEach(async () => {
    await act(async () => {
      for (const root of roots) root.unmount()
    })
    roots = []
    queryClients = []
    for (const container of containers) container.remove()
    containers = []
  })

  it('loads a build detail route directly from the runtime get API', async () => {
    const container = await renderWithProviders(<BuildDetailPage />, '/builds/build-1')
    expect(testState.apiGet).toHaveBeenCalledWith('/delivery/runtime/builds/build-1')
    expect(testState.apiGet).not.toHaveBeenCalledWith('/builds')
    expect(testState.apiGet).not.toHaveBeenCalledWith('/applications/app-1/detail')
    expect(container.textContent).toContain('构建详情')
    expect(container.textContent).toContain('build-1')
    expect(container.textContent).toContain('已确认全部停止')
    expect(
      container.querySelector('a[href="https://gitlab.example.com/team/app/-/pipelines/101"]'),
    ).not.toBeNull()
  })

  it('loads an execution task detail route and renders logs and artifacts', async () => {
    const container = await renderWithProviders(
      <ExecutionTaskDetailPage />,
      '/delivery/execution-tasks/task-1',
    )
    expect(testState.apiGet).toHaveBeenCalledWith('/delivery/runtime/execution-tasks/task-1')
    expect(testState.apiGet).not.toHaveBeenCalledWith('/delivery/execution-tasks')
    expect(testState.apiGet).not.toHaveBeenCalledWith('/delivery/execution-tasks/task-1/artifacts')
    expect(testState.apiGet).not.toHaveBeenCalledWith('/delivery/execution-tasks/task-1/logs')
    expect(container.textContent).toContain('执行任务详情')
    expect(container.textContent).toContain('task-1')
    expect(container.textContent).toContain('task running')
    expect(container.textContent).toContain('尚未确认停止')
    expect(container.textContent).toContain('等待外部执行器停止')
    expect(container.querySelector('a[href^="javascript:"]')).toBeNull()
  })

  it('shows a not-found state when the record does not exist', async () => {
    testState.apiGet.mockImplementationOnce(async (path: string) => {
      if (path === '/delivery/runtime/releases/release-missing') {
        const error = new Error('not found') as Error & { status: number }
        error.status = 404
        throw error
      }
      throw new Error(`Unhandled GET ${path}`)
    })
    const container = await renderWithProviders(<ReleaseDetailPage />, '/releases/release-missing')
    expect(testState.apiGet).toHaveBeenCalledWith('/delivery/runtime/releases/release-missing')
    expect(container.textContent).toContain('未找到')
    expect(container.textContent).toContain('Release')
  })

  it.each([401, 403])(
    'hides cached workflow evidence after HTTP %s rejects the read',
    async (status) => {
      const container = await renderWithProviders(
        <RuntimeDetailContent kind="workflow" recordId="workflow-cache" />,
        '/workflows/workflow-cache',
      )
      expect(container.textContent).toContain('protected cached workflow')
      testState.apiGet.mockImplementationOnce(async () => {
        throw new ApiError(status, 'execution access denied', { requestId: 'req-runtime-denied' })
      })
      await act(async () => {
        await queryClients.at(-1)!.refetchQueries()
      })
      for (let i = 0; i < 3; i++)
        await act(async () => {
          await new Promise((resolve) => setTimeout(resolve, 0))
        })
      expect(container.textContent).not.toContain('protected cached workflow')
      expect(container.textContent).toContain('execution access denied')
      expect(container.textContent).toContain('req-runtime-denied')
    },
  )

  it.each(['build', 'release', 'release_bundle', 'execution_task'] as const)(
    'preserves %s evidence on 503 and hides it on 403 in full and embedded views',
    async (kind) => {
      for (const embedded of [false, true]) {
        const data = {
          kind,
          object: {
            id: `cached-${kind}`,
            applicationId: 'app',
            version: 'v1',
            status: 'cancelled',
            metadata: {},
            createdAt: '2026-10-16T00:00:00Z',
          },
          artifacts: [
            {
              id: 'image',
              kind: 'image',
              ref: 'protected/image:v1',
              digest: 'sha256:protected',
              workflowRunId: 'origin/run',
              executionTaskId: 'origin/task',
              releaseBundleId: 'origin/bundle',
            },
          ],
        }
        testState.apiGet.mockResolvedValueOnce({ data } as never)
        const view = await renderWithProviders(
          <RuntimeDetailContent kind={kind} recordId={`cached-${kind}`} embedded={embedded} />,
          '/builds/read-cache',
        )
        expect(view.textContent).toContain('protected/image:v1')
        expect(view.textContent).toContain('sha256:protected')
        expect([...view.querySelectorAll('a')].map((link) => link.getAttribute('href'))).toContain(
          '/workflows/origin%2Frun',
        )
        const headerTag = view.querySelector('.soha-management-detail-header .soha-status-tag')
        if (headerTag) expect(headerTag.className).not.toContain('success')
        testState.apiGet.mockRejectedValueOnce(
          new ApiError(503, 'temporary evidence failure', {
            method: 'GET',
            requestId: 'req-evidence-503',
          }),
        )
        await act(async () => {
          await queryClients.at(-1)!.refetchQueries()
        })
        await act(async () => {
          await new Promise((resolve) => setTimeout(resolve, 10))
        })
        expect(view.textContent).toContain('protected/image:v1')
        expect(view.textContent).toContain('上次读取结果')
        expect(view.textContent).toContain('req-evidence-503')
        testState.apiGet.mockRejectedValueOnce(
          new ApiError(403, 'evidence denied', { method: 'GET', requestId: 'req-evidence-403' }),
        )
        await act(async () => {
          await queryClients.at(-1)!.refetchQueries()
        })
        await act(async () => {
          await new Promise((resolve) => setTimeout(resolve, 10))
        })
        expect(view.textContent).not.toContain('protected/image:v1')
        expect(view.textContent).not.toContain('sha256:protected')
        expect(view.textContent).toContain('req-evidence-403')
      }
    },
  )

  it('shows not-found when a cached build is deleted instead of retaining its artifacts', async () => {
    testState.apiGet.mockResolvedValueOnce({
      data: {
        kind: 'build',
        object: { id: 'deleted-build', status: 'completed', metadata: {} },
        artifacts: [{ kind: 'image', ref: 'deleted/image:v1' }],
      },
    } as never)
    const view = await renderWithProviders(
      <RuntimeDetailContent kind="build" recordId="deleted-build" />,
      '/builds/deleted-build',
    )
    expect(view.textContent).toContain('deleted/image:v1')
    testState.apiGet.mockRejectedValueOnce(new ApiError(404, 'build not found', { method: 'GET' }))
    await act(async () => {
      await queryClients.at(-1)!.refetchQueries()
    })
    await act(async () => {
      await new Promise((resolve) => setTimeout(resolve, 10))
    })
    expect(view.textContent).not.toContain('deleted/image:v1')
    expect(view.textContent).toContain('未找到')
  })
  const base = '/applications/app-1/application-environments/binding-1/workloads/checkout-api'
  const ref = {
    applicationEnvironmentId: 'binding-1',
    clusterId: 'cluster-a',
    namespace: 'checkout',
    workloadKind: 'Deployment',
    workloadName: 'checkout-api',
    desiredReplicas: 1,
    readyReplicas: 1,
    updatedReplicas: 1,
    availableReplicas: 1,
  }
  function PodWorkspaceHarness({ view }: { view: 'pods' | 'related-resources' }) {
    const [params] = useSearchParams()
    return (
      <ServicePodWorkspace
        applicationId="app-1"
        workloads={[ref]}
        view={params.get('serviceTab') === 'metrics' ? 'metrics' : view}
      />
    )
  }
  const renderPods = (query = '', view: 'pods' | 'related-resources' = 'pods') =>
    renderWithProviders(
      <>
        <ServiceRuntimeSummary applicationId="app-1" workloads={[ref]} />
        <ServiceRuntimeActions applicationId="app-1" workloads={[ref]} />
        <PodWorkspaceHarness view={view} />
        <LocationProbe />
      </>,
      base + query,
    )
  const clickPodTool = async (container: HTMLElement, label: string) => {
    await act(async () =>
      Array.from(container.querySelectorAll<HTMLButtonElement>('.soha-pod-card-actions button'))
        .find((button) => button.textContent === label)!
        .click(),
    )
    await act(async () => {
      await vi.dynamicImportSettled()
    })
    for (let i = 0; i < 3; i++)
      await act(async () => {
        await new Promise((resolve) => setTimeout(resolve, 0))
      })
  }

  it.each([
    { workloadKind: 'CustomResource', sourceStatus: 'linked' as const },
    { workloadKind: 'Deployment', sourceStatus: 'linked' as const },
    { workloadKind: 'Deployment', sourceStatus: 'replaced' as const },
    { workloadKind: 'Job' },
  ])('does not expose Deployment actions for an observation root: %j', async (source) => {
    const container = await renderWithProviders(
      <ServiceRuntimeActions applicationId="app-1" workloads={[{ ...ref, ...source }]} />,
      base,
    )
    expect(container.querySelector('.soha-service-runtime-actions')).toBeNull()
    expect(testState.apiGet.mock.calls.some(([path]) => path.includes('/workloads/'))).toBe(false)
  })

  it('renders compact Pods directly, without duplicating detail panels', async () => {
    const container = await renderPods()
    expect(container.querySelectorAll('.soha-pod-card')).toHaveLength(1)
    expect(container.querySelector('.soha-pod-card-heading .ant-tag-success')).not.toBeNull()
    expect(container.textContent).toContain('checkout-api-7d9f6b7c5f-x2k9m')
    expect(container.textContent).toContain('worker-01')
    expect(container.querySelector('.soha-pod-inspection')).toBeNull()
    expect(container.querySelector('[data-testid="pod-terminal"]')).toBeNull()
  })
  it('opens details with actual restart reasons and probes in a drawer', async () => {
    const container = await renderPods()
    await clickPodTool(container, '详情')
    expect(document.querySelector('.ant-drawer')).not.toBeNull()
    expect(document.body.textContent).toContain('OOMKilled')
    expect(document.body.textContent).toContain('HTTP://10.42.0.18:8080/live')
    expect(document.body.textContent).toContain('Init')
    expect(document.body.textContent).toContain('临时')
    expect(container.querySelectorAll('.soha-pod-card')).toHaveLength(1)
  })
  it.each([
    ['日志', 'logs'],
    ['终端', 'terminal'],
  ])('opens %s directly without fetching containers or showing a modal', async (label, kind) => {
    const container = await renderPods()
    await clickPodTool(container, label)
    expect(testState.openSession).toHaveBeenCalledExactlyOnceWith({
      kind,
      clusterId: 'cluster-a',
      namespace: 'checkout',
      podName: 'checkout-api-7d9f6b7c5f-x2k9m',
      container: undefined,
      ...(kind === 'terminal' ? { shell: '/bin/sh' } : {}),
    })
    expect(
      testState.apiGet.mock.calls.some(
        ([path]) => path.includes('/pods/') && path.includes('/detail?'),
      ),
    ).toBe(false)
    expect(document.querySelector('.ant-modal-root')).toBeNull()
    expect(container.querySelector('[data-testid="location"]')?.textContent).not.toContain('tool=')
  })
  it('preserves the container explicitly selected in a shared session link', async () => {
    await renderPods('?pod=checkout-api-7d9f6b7c5f-x2k9m&tool=logs&container=telemetry-sidecar')
    expect(testState.openSession).toHaveBeenCalledWith(
      expect.objectContaining({ container: 'telemetry-sidecar' }),
    )
    expect(document.querySelector('.ant-modal-root')).toBeNull()
  })
  it('renders event timeline and read-only YAML through Pod-scoped drawer links', async () => {
    const container = await renderPods('?pod=checkout-api-7d9f6b7c5f-x2k9m&tool=events')
    expect(document.querySelector('.ant-timeline')).not.toBeNull()
    expect(document.body.textContent!.indexOf('Newest event')).toBeLessThan(
      document.body.textContent!.indexOf('Older event'),
    )
    expect(document.body.textContent).not.toContain('Unrelated event')
    await act(async () => document.querySelector<HTMLButtonElement>('.ant-drawer-close')!.click())
    await clickPodTool(container, 'YAML')
    expect(document.querySelector('pre[aria-label="Pod YAML"]')?.textContent).toContain(
      'livenessProbe',
    )
    expect(document.body.textContent).toContain('只读')
  })
  it('requests metrics only for page-visible Pods and opens the full monitor in the metrics tab', async () => {
    testState.manyPods = true
    const container = await renderPods()
    expect(container.querySelectorAll('.soha-pod-card')).toHaveLength(10)
    expect(testState.apiGet.mock.calls.filter(([path]) => path.includes('/metrics?'))).toHaveLength(
      10,
    )
    const monitor = container.querySelector<HTMLButtonElement>('.soha-pod-card-metrics')!
    const podName = monitor.getAttribute('aria-label')!.replace('查看监控 ', '')
    await act(async () => monitor.click())
    for (let i = 0; i < 3; i++)
      await act(async () => {
        await new Promise((resolve) => setTimeout(resolve, 0))
      })
    expect(document.querySelector('[data-testid="resource-metrics-panel"]')).not.toBeNull()
    expect(document.querySelector('.ant-drawer')).toBeNull()
    const location = container.querySelector('[data-testid="location"]')?.textContent
    expect(location).toContain('serviceTab=metrics')
    expect(location).toContain(`metricPod=${podName}`)
  })
  it('uses real metric values and series in the compact monitor preview', async () => {
    testState.configuredMetrics = true
    const container = await renderPods()
    expect(container.querySelector('.soha-pod-card-metrics')?.textContent).toContain('120 mCPU')
    expect(container.querySelector('.soha-pod-card-metrics')?.textContent).toContain('1.0 MB')
    expect(container.querySelectorAll('[data-testid="pod-trend"]')).toHaveLength(2)
    expect(container.querySelector('[data-testid="pod-trend"]')?.textContent).toBe('0.1,0.12')
  })

  it('keeps permission-denied tools and metric requests unavailable', async () => {
    testState.metricsAllowed = false
    const container = await renderPods('?pod=checkout-api-7d9f6b7c5f-x2k9m&tool=yaml')
    expect(container.querySelector('.soha-pod-card-metrics')).toBeNull()
    expect(
      testState.apiGet.mock.calls.some(
        ([path]) => path.includes('/metrics?') || path.includes('/yaml?'),
      ),
    ).toBe(false)
    expect(document.body.textContent).toContain('权限')
  })
  it('does not fetch or render app runtime when application access is denied', async () => {
    testState.appAllowed = false
    const container = await renderPods()
    expect(container.querySelector('.soha-pod-card')).toBeNull()
    expect(testState.apiGet).not.toHaveBeenCalled()
  })
  it('shows a retryable Pod-list error instead of an empty list on runtime failure', async () => {
    testState.workloadRuntimeError = true
    const container = await renderPods()
    expect(container.textContent).toContain('Pod 列表读取失败')
    expect(container.textContent).not.toContain('当前服务暂无 Pod')
  })
  it('does not select another Pod when a shared target disappears', async () => {
    testState.hideOriginalPod = true
    testState.secondPod = true
    await renderPods('?pod=checkout-api-7d9f6b7c5f-x2k9m&tool=terminal')
    expect(document.body.textContent).toContain('Pod 不存在或无法唯一定位')
    expect(testState.openSession).not.toHaveBeenCalled()
  })
  it('keeps production restart confirmation scoped to its workload', async () => {
    testState.production = true
    const container = await renderPods()
    await act(async () =>
      Array.from(
        container.querySelectorAll<HTMLButtonElement>('.soha-service-runtime-action-group button'),
      )[0].click(),
    )
    expect(document.body.textContent).toContain('重启生产实例')
    expect(document.body.textContent).toContain('cluster-a/checkout')
  })
  it('confirms production scaling and sends the exact workload scope', async () => {
    testState.production = true
    const container = await renderPods()
    await act(async () =>
      Array.from(
        container.querySelectorAll<HTMLButtonElement>('.soha-service-runtime-actions button'),
      )
        .find((button) => button.textContent === '扩缩容')!
        .click(),
    )
    expect(document.body.textContent).toContain('生产环境扩缩容')
    expect(document.body.textContent).toContain('cluster-a / checkout')
    expect(api.post).not.toHaveBeenCalled()
    await act(async () =>
      Array.from(document.querySelectorAll<HTMLButtonElement>('.ant-modal-footer button'))
        .find((button) => button.textContent === '确认扩缩容')!
        .click(),
    )
    expect(api.post).toHaveBeenCalledWith('/clusters/cluster-a/workloads/deployments/scale', {
      namespace: 'checkout',
      name: 'checkout-api',
      replicas: 2,
    })
  })
  it('retains related resources as scoped resource links', async () => {
    const container = await renderPods('', 'related-resources')
    expect(
      container.querySelector('a[href*="configmaps/checkout-config"]')?.getAttribute('href'),
    ).toContain('/clusters/cluster-a/')
  })
  it('redirects legacy Pod links into the two-level workspace and preserves the tool', async () => {
    const container = await renderWithProviders(
      <ApplicationWorkloadDetailPage />,
      base + '?tab=metrics&pod=checkout-api-7d9f6b7c5f-x2k9m',
    )
    expect(container.textContent).toContain('/applications/app-1?tab=services')
    expect(container.textContent).toContain('applicationEnvironmentId=binding-1')
    expect(container.textContent).toContain('tool=metrics')
    expect(container.textContent).toContain('pod=checkout-api-7d9f6b7c5f-x2k9m')
  })
})
