/** @vitest-environment jsdom */

import type { ReactNode } from 'react'
import { act } from 'react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { createRoot } from 'react-dom/client'
import { MemoryRouter } from 'react-router-dom'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { App } from 'antd'
import { VirtualizationClustersPage, VirtualizationFlavorsPage, VirtualizationImagesPage, VirtualizationOperationsPage, VirtualizationOverviewPage, VirtualizationSyncPage, VirtualizationVmDetailPage, VirtualizationVmsPage, buildClusterPayload, buildCreateVmPayload } from './virtualization-pages'

vi.mock('@visactor/react-vchart', () => ({
  LineChart: () => null,
}))

vi.mock('@/components/resource-metrics-panel', () => ({
  buildCompactChartSpec: () => ({}),
  compactMetricColors: {
    cpu: '#000',
    memory: '#000',
    networkRx: '#000',
    networkTx: '#000',
    default: '#000',
  },
  formatMetricValue: (value: unknown) => String(value ?? '-'),
}))

const testState = vi.hoisted(() => ({
  permissionSnapshot: {
    permissionKeys: ['virtualization.vms.manage', 'virtualization.sync.manage', 'virtualization.clusters.manage', 'virtualization.operations.manage'],
    visibleMenuIds: [],
    visibleMenus: [],
  },
  apiGet: vi.fn(async (path: string) => {
    if (path === '/virtualization/vms?page=1&pageSize=10') {
      return { data: { items: [{ id: 'vm-1', name: 'build-vm', provider: 'kubevirt', status: 'running', flavorName: 'standard-2c4g', bootImageName: 'ubuntu-24.04', cpu: 2, memoryMiB: 4096, diskGiB: 40 }], total: 1, page: 1, pageSize: 10 } }
    }
    if (path === '/virtualization/vms/vm-1/detail') {
      return { data: {
        vm: { id: 'vm-1', name: 'build-vm', provider: 'kubevirt', status: 'running', flavorName: 'standard-2c4g', bootImageName: 'ubuntu-24.04', ipAddresses: ['10.0.0.8'] },
        providerRaw: { kind: 'VirtualMachine', metadata: { name: 'build-vm' } },
        operations: [
          { id: 'op-vm-failed', operationType: 'vm_action', status: 'failed', targetName: 'build-vm', message: 'restart failed', createdAt: '2026-05-21T01:00:00Z' },
          { id: 'op-vm', operationType: 'vm_create', status: 'completed', targetName: 'build-vm', createdAt: '2026-05-21T00:00:00Z' },
        ],
        logs: [{ id: 'log-vm', taskId: 'op-vm', logLevel: 'info', message: 'vm ready', createdAt: '2026-05-21T00:00:00Z' }],
      } }
    }
    if (path === '/virtualization/overview') {
      return { data: {
        stats: {
          connections: { total: 2, healthy: 0, degraded: 1, unavailable: 1 },
          vmCount: 3,
          runningVmCount: 2,
          stoppedVmCount: 1,
          pendingTaskCount: 1,
          failedTaskCount: 1,
        },
        connectionSummary: { total: 2, healthy: 0, degraded: 1, unavailable: 1, neverSynced: 1, credentialMissing: 1 },
        taskSummary: { queued: 0, running: 1, failed: 1, timeout: 0, canceled: 0, completed: 1 },
        providerSummary: [{ provider: 'pve', connections: 1, unavailable: 1 }, { provider: 'kubevirt', connections: 1, degraded: 1 }],
        recentOperations: [
          { id: 'op-recent', operationType: 'vm_create', status: 'running', targetName: 'build-vm', createdAt: '2026-05-21T00:00:00Z' },
        ],
        attention: {
          riskyConnections: [
            { id: 'conn-pve', name: 'pve-a', provider: 'pve', endpoint: 'https://pve.example:8006', enabled: true, verifyTls: true, health: 'unavailable', credentialConfigured: false, riskLevel: 'critical', riskReasons: ['连接不可用', '未配置凭证', '尚未同步'] },
          ],
          failedSyncTasks: [
            { id: 'op-retry', operationType: 'asset_sync', status: 'failed', targetName: 'conn-a', message: 'sync failed', allowedActions: ['retry'] },
          ],
          failedOperations: [
            { id: 'op-retry', operationType: 'asset_sync', status: 'failed', targetName: 'conn-a', message: 'sync failed', allowedActions: ['retry'] },
          ],
        },
      } }
    }
    if (path === '/virtualization/clusters') {
      return { data: [
        { id: 'conn-pve', name: 'pve-a', provider: 'pve', endpoint: 'https://pve.example:8006', enabled: true, verifyTls: true, health: 'unavailable', credentialConfigured: false, config: { defaultNode: 'pve-1', defaultStorage: 'local-lvm', defaultBridge: 'vmbr0' } },
        { id: 'conn-1', name: 'kubevirt-a', provider: 'kubevirt', kubernetesClusterId: 'cluster-a', enabled: true, verifyTls: true, health: 'degraded', credentialConfigured: true, lastSyncedAt: '2026-05-21T00:00:00Z', config: { backendUrl: 'https://kube.example:6443' } },
      ] }
    }
    if (path === '/virtualization/images' || path === '/virtualization/images?page=1&pageSize=10') {
      return { data: { items: [
        { id: 'image-1', name: 'ubuntu-24.04', provider: 'kubevirt', connectionId: 'conn-1', sourceKind: 'datasource', sourceRef: 'default/ubuntu', osType: 'ubuntu' },
        { id: 'image-2', name: 'debian-template', provider: 'pve', connectionId: 'conn-pve', sourceKind: 'template', sourceRef: 'local:vztmpl/debian.tar.zst', node: 'pve-1', storage: 'local', osType: 'debian' },
      ], total: 2, page: 1, pageSize: 10 } }
    }
    if (path === '/virtualization/flavors') {
      return { data: [{ id: 'flavor-1', name: 'standard-2c4g', cpu: 2, memoryMiB: 4096, diskGiB: 40, enabled: true }] }
    }
    if (path === '/virtualization/operations?assetType=asset_sync') {
      return { data: [{ id: 'op-1', operationType: 'asset_sync', status: 'completed', targetName: 'kubevirt-a' }] }
    }
    if (path === '/virtualization/operations?abnormal=true') {
      return { data: [
        { id: 'op-retry', operationType: 'asset_sync', status: 'failed', targetName: 'conn-a', connectionId: 'conn-a', message: 'sync failed', allowedActions: ['retry'] },
      ] }
    }
    if (path === '/virtualization/operations?abnormal=true&connectionId=conn-a') {
      return { data: [
        { id: 'op-retry', operationType: 'asset_sync', status: 'failed', targetName: 'conn-a', connectionId: 'conn-a', message: 'sync failed', allowedActions: ['retry'] },
      ] }
    }
    if (path === '/virtualization/operations') {
      return { data: [
        { id: 'op-cancel', operationType: 'vm_create', status: 'running', targetName: 'vm-a', allowedActions: ['cancel'] },
        { id: 'op-retry', operationType: 'asset_sync', status: 'failed', targetName: 'conn-a', connectionId: 'conn-a', message: 'sync failed', allowedActions: ['retry'] },
      ] }
    }
    if (path === '/virtualization/operations/op-1/logs') {
      return { data: [{ id: 'log-1', taskId: 'op-1', logLevel: 'info', message: 'sync completed', createdAt: '2026-05-21T00:00:00Z' }] }
    }
    throw new Error(`Unhandled GET ${path}`)
  }),
  apiPost: vi.fn(async (_path: string, _body?: unknown) => ({ data: { id: 'op-new' } })),
  apiPut: vi.fn(async (_path: string, _body?: unknown) => ({ data: { id: 'updated' } })),
  apiDelete: vi.fn(async (_path: string) => ({ data: undefined })),
}))

vi.mock('@/features/auth/permission-snapshot', () => ({
  hasAllowedAction: (actions: string[] | undefined, action: string) => actions?.includes(action) ?? false,
  hasPermission: (snapshot: { permissionKeys?: string[] } | undefined, key: string) => snapshot?.permissionKeys?.includes(key) ?? false,
  usePermissionSnapshot: () => ({
    data: { data: testState.permissionSnapshot },
    isLoading: false,
  }),
}))

vi.mock('@/services/api-client', () => ({
  api: {
    get: (path: string) => testState.apiGet(path),
    post: (path: string, body?: unknown) => testState.apiPost(path, body),
    put: (path: string, body?: unknown) => testState.apiPut(path, body),
    delete: (path: string) => testState.apiDelete(path),
  },
}))

vi.mock('@/stores/preferences-store', () => ({
  usePreferencesStore: {
    getState: () => ({ localeCode: 'zh_CN' }),
  },
}))

let containers: HTMLDivElement[] = []
let roots: Array<ReturnType<typeof createRoot>> = []

async function renderWithProviders(node: ReactNode, route = '/virtualization/vms') {
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
          <App>{node}</App>
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

async function waitForText(text: string) {
  for (let index = 0; index < 10; index += 1) {
    await act(async () => {
      await new Promise((resolve) => setTimeout(resolve, 0))
    })
    if (document.body.textContent?.includes(text)) {
      return
    }
  }
}

async function clickButtonByLabel(container: ParentNode, label: string) {
  const button = Array.from(container.querySelectorAll('button')).find((node) => node.getAttribute('aria-label') === label)
  if (!(button instanceof HTMLButtonElement)) {
    throw new Error(`button not found by aria-label: ${label}`)
  }
  await act(async () => {
    button.dispatchEvent(new MouseEvent('click', { bubbles: true }))
    await Promise.resolve()
  })
}

function hasButtonByLabel(container: ParentNode, label: string) {
  return Array.from(container.querySelectorAll('button')).some((node) => node.getAttribute('aria-label') === label)
}

describe('virtualization pages', () => {
  beforeEach(() => {
    testState.permissionSnapshot = {
      permissionKeys: ['virtualization.vms.manage', 'virtualization.sync.manage', 'virtualization.clusters.manage', 'virtualization.operations.manage'],
      visibleMenuIds: [],
      visibleMenus: [],
    }
    testState.apiGet.mockClear()
    testState.apiPost.mockClear()
    testState.apiPut.mockClear()
    testState.apiDelete.mockClear()
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
    vi.spyOn(window, 'getComputedStyle').mockImplementation(
      () =>
        ({
          getPropertyValue: () => '',
        }) as unknown as CSSStyleDeclaration,
    )
  })

  afterEach(async () => {
    await act(async () => {
      for (const root of roots) {
        root.unmount()
      }
    })
    roots = []
    for (const container of containers) {
      container.remove()
    }
    containers = []
    vi.clearAllMocks()
  })

  it('surfaces abnormal clusters and failed operations in overview', async () => {
    const container = await renderWithProviders(<VirtualizationOverviewPage />, '/virtualization/overview')

    expect(testState.apiGet).toHaveBeenCalledWith('/virtualization/overview')
    expect(testState.apiGet).not.toHaveBeenCalledWith('/virtualization/clusters')
    expect(testState.apiGet).not.toHaveBeenCalledWith('/virtualization/operations')
    expect(container.textContent).toContain('高风险连接')
    expect(container.textContent).toContain('失败与超时任务')
    expect(container.textContent).toContain('连接不可用')
    expect(container.textContent).toContain('sync failed')
    expect(container.textContent).toContain('Provider 分布')
  })

  it('loads paginated VMs and creates from flavor plus image with provider fields', async () => {
    const container = await renderWithProviders(<VirtualizationVmsPage />)

    expect(testState.apiGet).toHaveBeenCalledWith('/virtualization/vms?page=1&pageSize=10')
    expect(testState.apiGet).toHaveBeenCalledWith('/virtualization/clusters')
    expect(testState.apiGet).toHaveBeenCalledWith('/virtualization/images')
    expect(testState.apiGet).toHaveBeenCalledWith('/virtualization/flavors')
    expect(container.textContent).toContain('build-vm')

    await act(async () => {
      const createButton = Array.from(container.querySelectorAll('button')).find((button) => button.textContent?.includes('创建虚拟机'))
      createButton?.dispatchEvent(new MouseEvent('click', { bubbles: true }))
      await Promise.resolve()
    })

    expect(document.body.textContent).toContain('Cloud Init')
    expect(document.body.textContent).toContain('规格')
    expect(document.body.textContent).toContain('启动镜像')
    expect(document.body.textContent).toContain('StorageClass')
    expect(document.body.textContent).toContain('DataSource 克隆')
    expect(document.body.textContent).toContain('DataVolume')
    expect(document.body.textContent).not.toContain('raw YAML')
    expect(document.body.textContent).not.toContain('raw PVE config')
  })

  it('keeps the VM filter row on the compact aligned query layout', async () => {
    const container = await renderWithProviders(<VirtualizationVmsPage />)
    const query = container.querySelector('.soha-vrt-query.soha-vrt-vms-query')
    const fields = Array.from(query?.querySelectorAll<HTMLElement>('.soha-management-query-field') ?? [])

    expect(query).not.toBeNull()
    expect(fields).toHaveLength(4)
    expect(fields.map((field) => field.textContent)).toEqual([
      expect.stringContaining('关键字'),
      expect.stringContaining('连接'),
      expect.stringContaining('状态'),
      expect.stringContaining('Provider'),
    ])
    expect(fields.map((field) => field.style.getPropertyValue('--soha-management-query-field-width'))).toEqual([
      '260px',
      '180px',
      '136px',
      '160px',
    ])
    expect(fields.map((field) => field.style.getPropertyValue('--soha-management-query-field-min-width'))).toEqual([
      '260px',
      '180px',
      '136px',
      '160px',
    ])
  })

  it('uses the aligned query shell across virtualization list pages', async () => {
    const pages: Array<[ReactNode, string]> = [
      [<VirtualizationClustersPage />, '/virtualization/clusters'],
      [<VirtualizationImagesPage />, '/virtualization/images'],
      [<VirtualizationFlavorsPage />, '/virtualization/flavors'],
      [<VirtualizationOperationsPage />, '/virtualization/operations'],
      [<VirtualizationSyncPage />, '/virtualization/sync'],
    ]

    for (const [page, route] of pages) {
      const container = await renderWithProviders(page, route)
      expect(container.querySelector('.soha-vrt-query .soha-management-query-card')).not.toBeNull()
    }
  })

  it('builds VM create payload from the selected source mode', () => {
    const payload = buildCreateVmPayload({
      provider: 'kubevirt',
      connectionId: 'conn-1',
      name: 'build-vm',
      flavorId: 'flavor-1',
      bootImageId: 'image-pvc',
      sourceMode: 'pvc_clone',
      startAfterCreate: true,
    })

    expect(payload.sourceMode).toBe('pvc_clone')
    expect(payload.sourceId).toBe('image-pvc')
    expect(payload.imageId).toBe('image-pvc')
  })

  it('builds provider connection config for PVE and KubeVirt runtime fields', () => {
    const pvePayload = buildClusterPayload({
      provider: 'pve',
      name: 'pve-a',
      endpoint: 'https://pve.example:8006',
      defaultNode: 'pve-1',
      defaultStorage: 'local-lvm',
      defaultBridge: 'vmbr0',
      tokenID: 'root@pam!soha',
      tokenSecret: 'secret',
    })
    expect(pvePayload.config).toMatchObject({ defaultNode: 'pve-1', defaultStorage: 'local-lvm', defaultBridge: 'vmbr0' })
    expect(pvePayload.credential).toMatchObject({ tokenID: 'root@pam!soha', tokenSecret: 'secret' })

    const kubevirtPayload = buildClusterPayload({
      provider: 'kubevirt',
      name: 'kubevirt-a',
      kubernetesClusterId: 'cluster-a',
      backendUrl: 'https://kube.example:6443',
      prometheusUrl: 'https://prometheus.example',
    })
    expect(kubevirtPayload.config).toMatchObject({ backendUrl: 'https://kube.example:6443', prometheusUrl: 'https://prometheus.example' })
    expect(kubevirtPayload.endpoint).toBeUndefined()
  })

  it('renders VM detail with provider raw, operations, logs and AI investigation entry', async () => {
    const container = await renderWithProviders(<VirtualizationVmDetailPage />, '/virtualization/vms/vm-1')

    expect(testState.apiGet).toHaveBeenCalledWith('/virtualization/vms/vm-1/detail')
    expect(container.textContent).toContain('build-vm')
    expect(container.textContent).toContain('standard-2c4g')
    expect(container.textContent).toContain('ubuntu-24.04')
    expect(container.textContent).toContain('10.0.0.8')
    expect(container.textContent).toContain('Provider Raw')
    expect(container.textContent).toContain('任务历史')
    expect(container.textContent).toContain('vm ready')
    expect(container.textContent).toContain('最近异常任务')
    expect(container.textContent).toContain('restart failed')
    expect(container.textContent).toContain('AI调查')
    expect(container.textContent).toContain('来源模式')
    expect(container.textContent).toContain('来源引用')
    expect(container.textContent).toContain('Console 能力摘要')
    expect(container.textContent).toContain('Metrics 能力摘要')
  })

  it('hides VM actions without manage permission', async () => {
    testState.permissionSnapshot = {
      permissionKeys: ['virtualization.vms.view'],
      visibleMenuIds: [],
      visibleMenus: [],
    }

    const container = await renderWithProviders(<VirtualizationVmsPage />)

    expect(container.textContent).not.toContain('创建虚拟机')
    expect(container.textContent).not.toContain('启动')
    expect(container.textContent).not.toContain('停止')
  })

  it('uses asset_sync filtering for the sync task page', async () => {
    const container = await renderWithProviders(<VirtualizationSyncPage />, '/virtualization/sync')

    expect(testState.apiGet).toHaveBeenCalledWith('/virtualization/operations?assetType=asset_sync')
    expect(container.textContent).toContain('asset_sync')

    await clickButtonByLabel(container, '查看日志')
    await waitForText('sync completed')

    expect(testState.apiGet).toHaveBeenCalledWith('/virtualization/operations/op-1/logs')
    expect(document.body.textContent).toContain('sync completed')
  })

  it('shows provider-specific cluster connection fields', async () => {
    const container = await renderWithProviders(<VirtualizationClustersPage />, '/virtualization/clusters')

    await act(async () => {
      const addButton = Array.from(container.querySelectorAll('button')).find((button) => button.textContent?.includes('新增连接'))
      addButton?.dispatchEvent(new MouseEvent('click', { bubbles: true }))
      await Promise.resolve()
    })

    expect(testState.apiGet).toHaveBeenCalledWith('/clusters')
    expect(document.body.textContent).toContain('Kubernetes 集群')
    expect(document.body.textContent).not.toContain('Other')
    expect(document.body.textContent).toContain('校验 TLS')
  })

  it('renders PVE credential fields without raw config editing', async () => {
    const container = await renderWithProviders(<VirtualizationClustersPage />, '/virtualization/clusters')

    await clickButtonByLabel(container, '编辑连接')

    expect(document.body.textContent).toContain('Token ID')
    expect(document.body.textContent).toContain('Token Secret')
    expect(document.body.textContent).toContain('默认节点')
    expect(document.body.textContent).toContain('默认存储')
    expect(document.body.textContent).not.toContain('raw JSON')
    expect(container.textContent).toContain('风险等级')
    expect(container.textContent).toContain('最近失败同步')
    expect(container.textContent).toContain('最近异常任务')
  })

  it('shows image management entries for KubeVirt and PVE sources', async () => {
    testState.permissionSnapshot = {
      permissionKeys: ['virtualization.images.view', 'virtualization.images.manage'],
      visibleMenuIds: [],
      visibleMenus: [],
    }
    const container = await renderWithProviders(<VirtualizationImagesPage />, '/virtualization/images')

    expect(testState.apiGet).toHaveBeenCalledWith('/virtualization/images?page=1&pageSize=10')
    expect(container.textContent).toContain('KubeVirt')
    expect(container.textContent).toContain('PVE')
    expect(container.textContent).toContain('datasource')
    expect(container.textContent).toContain('template')
    expect(container.textContent).toContain('新增镜像入口')

    await act(async () => {
      const addButton = Array.from(container.querySelectorAll('button')).find((button) => button.textContent?.includes('新增镜像入口'))
      addButton?.dispatchEvent(new MouseEvent('click', { bubbles: true }))
      await Promise.resolve()
    })

    expect(document.body.textContent).toContain('KubeVirt DataSource')
    expect(document.body.textContent).toContain('来源类型')
  })

  it('initializes operations view from abnormal query preset', async () => {
    const container = await renderWithProviders(<VirtualizationOperationsPage />, '/virtualization/operations?abnormal=true')

    expect(container.textContent).toContain('失败/超时')
    expect(container.textContent).toContain('sync failed')
    expect(container.textContent).not.toContain('vm-a')
  })

  it('initializes operations view from connection abnormal query preset', async () => {
    const container = await renderWithProviders(<VirtualizationOperationsPage />, '/virtualization/operations?connectionId=conn-a&abnormal=true')

    expect(testState.apiGet).toHaveBeenCalledWith('/virtualization/operations?abnormal=true&connectionId=conn-a')
    expect(container.textContent).toContain('sync failed')
    expect(container.textContent).not.toContain('vm-a')
  })

  it('gates operation cancel and retry by manage permission and allowed actions', async () => {
    const container = await renderWithProviders(<VirtualizationOperationsPage />, '/virtualization/operations')

    expect(hasButtonByLabel(container, '取消任务')).toBe(true)
    expect(hasButtonByLabel(container, '重试任务')).toBe(true)

    testState.permissionSnapshot = {
      permissionKeys: ['virtualization.operations.view'],
      visibleMenuIds: [],
      visibleMenus: [],
    }
    const readonlyContainer = await renderWithProviders(<VirtualizationOperationsPage />, '/virtualization/operations')

    expect(hasButtonByLabel(readonlyContainer, '取消任务')).toBe(false)
    expect(hasButtonByLabel(readonlyContainer, '重试任务')).toBe(false)
  })
})
