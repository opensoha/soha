/** @vitest-environment jsdom */

import type { ReactNode } from 'react'
import { act, StrictMode } from 'react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { createRoot } from 'react-dom/client'
import { MemoryRouter } from 'react-router-dom'
import { afterAll, afterEach, beforeAll, beforeEach, describe, expect, it, vi } from 'vitest'
import { App } from 'antd'
import { ApiError } from '@/services/api-error'
import { VirtualizationConnectionStepModal } from './clusters/create-page'
import { VirtualizationClustersPage } from './clusters/list-page'
import { VirtualizationFlavorsPage } from './flavors/list-page'
import { VirtualizationImagesPage } from './images/list-page'
import { VirtualizationVmDetailPage } from './virtual-machines/detail-page'
import {
  defaultRootDisk,
  pveVMSourceRef,
  VirtualizationVmsPage,
} from './virtual-machines/list-page'
import {
  buildClusterPayload,
  buildCreateVmPayload,
  buildRuntimeHostProvisionPayload,
  filterVmCreateFlavors,
  virtualMachineRootDiskGiB,
} from './virtualization-model'
import type { CreateVirtualMachineInput, VirtualizationClusterInput } from './virtualization-types'

const lazyRuntimeState = vi.hoisted(() => ({
  metricsLoads: 0,
  consoleLoads: 0,
}))

vi.mock('@visactor/react-vchart', () => {
  lazyRuntimeState.metricsLoads += 1
  return { LineChart: () => null }
})

vi.mock('@novnc/novnc', () => {
  lazyRuntimeState.consoleLoads += 1
  return {
    default: class MockRFB {
      scaleViewport = false
      resizeSession = false
      addEventListener() {}
      clipboardPasteFrom() {}
      disconnect() {}
      sendCtrlAltDel() {}
    },
  }
})

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
  modules: {
    docker: true,
    virtualization: true,
  },
  permissionSnapshot: {
    permissionKeys: [
      'virtualization.vms.create',
      'virtualization.vms.power',
      'virtualization.vms.resize',
      'virtualization.vms.delete',
      'virtualization.sync.view',
      'virtualization.sync.sync',
      'virtualization.clusters.create',
      'virtualization.clusters.update',
      'virtualization.clusters.delete',
      'virtualization.clusters.test',
      'virtualization.operations.view',
      'virtualization.operations.cancel',
      'virtualization.operations.retry',
      'docker.hosts.create',
    ],
    visibleMenuIds: [],
    visibleMenus: [],
  },
  vmAllowedActions: [] as string[],
  catalogAllowedActions: [] as string[],
  apiGet: vi.fn(async (path: string) => {
    if (path === '/modules') {
      return {
        data: [
          {
            descriptor: {
              id: 'virtualization',
              name: 'Virtualization',
              defaultPath: '/virtualization',
            },
            enabled: testState.modules.virtualization,
          },
          {
            descriptor: {
              id: 'docker',
              name: 'Docker',
              defaultPath: '/compute/runtimes',
            },
            enabled: testState.modules.docker,
          },
        ],
      }
    }
    if (path === '/virtualization/vms?page=1&pageSize=15') {
      return {
        data: {
          items: [
            {
              id: 'vm-1',
              name: 'build-vm',
              provider: 'kubevirt',
              status: 'running',
              flavorName: 'standard-2c4g',
              bootImageName: 'ubuntu-24.04',
              cpu: 2,
              memoryMiB: 4096,
              diskGiB: 40,
              ipAddresses: ['10.0.0.8'],
              allowedActions: testState.vmAllowedActions,
              capabilities: [
                'vm.resource.cpu.resize',
                'vm.resource.memory.resize',
                'vm.resource.disk.resize',
              ],
              config: {
                hostname: 'build-vm',
                fqdn: 'build-vm.apps.svc.cluster.local',
                dnsServers: '10.96.0.10',
                searchDomains: 'apps.svc.cluster.local',
                cloudInitConfigured: 'true',
                cloudInitSource: 'cloudInitNoCloud',
                cloudInitUserDataConfigured: 'true',
                diskCount: '1',
                networkInterfaceCount: '1',
                networks: 'default',
              },
            },
            {
              id: 'vm-stale',
              name: 'stale-vm',
              provider: 'kubevirt',
              status: 'stale',
              powerState: 'running',
              flavorName: 'standard-2c4g',
              bootImageName: 'ubuntu-24.04',
              cpu: 2,
              memoryMiB: 4096,
              diskGiB: 40,
            },
          ],
          total: 2,
          page: 1,
          pageSize: 15,
        },
      }
    }
    if (path === '/virtualization/vms/vm-1/devices') {
      return { data: [{ id: 'scsi0', kind: 'disk', sizeGiB: 40 }] }
    }
    if (path === '/virtualization/vms/vm-1/detail') {
      return {
        data: {
          vm: {
            id: 'vm-1',
            name: 'build-vm',
            provider: 'kubevirt',
            status: 'running',
            flavorName: 'standard-2c4g',
            bootImageName: 'ubuntu-24.04',
            ipAddresses: ['10.0.0.8'],
            cpu: 2,
            memoryMiB: 4096,
            diskGiB: 40,
            config: {
              hostname: 'build-vm',
              fqdn: 'build-vm.apps.svc.cluster.local',
              dnsServers: '10.96.0.10',
              searchDomains: 'apps.svc.cluster.local',
              cloudInitConfigured: 'true',
              cloudInitSource: 'cloudInitNoCloud',
              cloudInitUserDataConfigured: 'true',
              diskCount: '1',
              networkInterfaceCount: '1',
              networks: 'default',
            },
          },
          providerRaw: { kind: 'VirtualMachine', metadata: { name: 'build-vm' } },
          operations: [
            {
              id: 'op-vm-failed',
              operationType: 'vm_action',
              status: 'failed',
              targetName: 'build-vm',
              message: 'restart failed',
              createdAt: '2026-05-21T01:00:00Z',
            },
            {
              id: 'op-vm',
              operationType: 'vm_create',
              status: 'completed',
              targetName: 'build-vm',
              createdAt: '2026-05-21T00:00:00Z',
            },
          ],
          logs: [
            {
              id: 'log-vm',
              taskId: 'op-vm',
              logLevel: 'info',
              message: 'vm ready',
              createdAt: '2026-05-21T00:00:00Z',
            },
          ],
        },
      }
    }
    if (path === '/virtualization/vms/vm-stale/detail') {
      return {
        data: {
          vm: {
            id: 'vm-stale',
            name: 'stale-vm',
            provider: 'kubevirt',
            status: 'stale',
            powerState: 'running',
            flavorName: 'standard-2c4g',
            bootImageName: 'ubuntu-24.04',
          },
          providerRaw: { kind: 'VirtualMachine', metadata: { name: 'stale-vm' } },
          operations: [],
          logs: [],
        },
      }
    }
    if (path === '/virtualization/vms/vm-1/metrics?rangeMinutes=60&stepSeconds=60') {
      return { data: { ready: true, series: [] } }
    }
    if (path === '/virtualization/vms/vm-1/console') {
      return { data: { ready: false, message: 'Console disabled in tests' } }
    }
    if (path === '/virtualization/clusters') {
      return {
        data: [
          {
            id: 'conn-pve',
            name: 'pve-a',
            provider: 'pve',
            endpoint: 'https://pve.example:8006',
            enabled: true,
            verifyTls: true,
            health: 'unavailable',
            credentialConfigured: false,
            config: {
              defaultNode: 'pve-1',
              sshHost: 'node.example',
              sshHostKeys: { 'pve-1': 'trusted-public-key' },
              defaultStorage: 'local-lvm',
              defaultBridge: 'vmbr0',
              defaultSnippetStorage: 'local',
            },
          },
          {
            id: 'conn-1',
            name: 'kubevirt-a',
            provider: 'kubevirt',
            kubernetesClusterId: 'cluster-a',
            enabled: true,
            verifyTls: true,
            health: 'degraded',
            credentialConfigured: true,
            lastSyncedAt: '2026-05-21T00:00:00Z',
            config: {
              backendUrl: 'https://kube.example:6443',
              prometheusBearerTokenConfigured: true,
            },
          },
        ],
      }
    }
    if (path === '/clusters') {
      return {
        data: [
          {
            id: 'cluster-a',
            name: 'prod-k8s',
            region: 'cn',
            environment: 'prod',
            labels: {},
            connectionMode: 'direct_kubeconfig',
            version: 'v1.30.0',
            health: { status: 'healthy' },
          },
          {
            id: 'cluster-agent',
            name: 'edge-agent',
            region: 'cn',
            environment: 'edge',
            labels: {},
            connectionMode: 'agent',
            version: 'v1.30.0',
            health: { status: 'healthy' },
          },
        ],
      }
    }
    if (path === '/virtualization/clusters/conn-pve/delete-dependencies') {
      return {
        data: {
          connection: { id: 'conn-pve', name: 'pve-a', provider: 'pve' },
          vmCount: 1,
          imageCount: 2,
          flavorCount: 0,
          taskCount: 3,
          pendingTaskCount: 0,
          dockerHostCount: 1,
          vmSamples: [{ id: 'vm-1', name: 'build-vm', externalId: '101' }],
          taskSamples: [{ id: 'op-1', name: 'vm_create', status: 'completed' }],
          forceRequired: true,
          blocking: true,
          blockingReasons: ['virtual_machines', 'docker_hosts'],
        },
      }
    }
    if (path === '/virtualization/images?page=1&pageSize=15&category=catalog') {
      return {
        data: {
          items: [
            {
              id: 'image-1',
              name: 'ubuntu-24.04',
              allowedActions: testState.catalogAllowedActions,
              provider: 'kubevirt',
              connectionId: 'conn-1',
              sourceKind: 'datasource',
              sourceRef: 'default/ubuntu',
              osType: 'ubuntu',
            },
            {
              id: 'image-2',
              name: 'debian-template',
              provider: 'pve',
              connectionId: 'conn-pve',
              sourceKind: 'template',
              sourceRef: 'local:vztmpl/debian.tar.zst',
              node: 'pve-1',
              storage: 'local',
              osType: 'debian',
            },
          ],
          total: 2,
          page: 1,
          pageSize: 15,
        },
      }
    }
    if (path === '/virtualization/images?page=1&pageSize=15&category=storage') {
      return {
        data: {
          items: [
            {
              id: 'storage-local',
              name: 'local',
              provider: 'pve',
              connectionId: 'conn-pve',
              sourceKind: 'storage',
            },
            {
              id: 'vm-disk',
              name: 'vm-152-disk-0',
              provider: 'pve',
              connectionId: 'conn-pve',
              sourceKind: 'image',
              config: { contentType: 'images' },
            },
            {
              id: 'ct-root-volume',
              name: 'subvol-201-disk-0',
              provider: 'pve',
              connectionId: 'conn-pve',
              sourceKind: 'image',
              config: { contentType: 'rootdir' },
            },
          ],
          total: 3,
          page: 1,
          pageSize: 15,
        },
      }
    }
    if (path === '/virtualization/images?pageSize=500') {
      return {
        data: {
          items: [
            {
              id: 'image-1',
              name: 'ubuntu-24.04',
              provider: 'kubevirt',
              connectionId: 'conn-1',
              sourceKind: 'datasource',
              sourceRef: 'default/ubuntu',
              osType: 'ubuntu',
            },
            {
              id: 'image-2',
              name: 'debian-template',
              provider: 'pve',
              connectionId: 'conn-pve',
              sourceKind: 'template',
              sourceRef: 'local:vztmpl/debian.tar.zst',
              node: 'pve-1',
              storage: 'local',
              osType: 'debian',
            },
            {
              id: 'storage-local',
              name: 'local',
              provider: 'pve',
              connectionId: 'conn-pve',
              sourceKind: 'storage',
              assetKind: 'storage',
              node: 'pve-1',
              storage: 'local',
              config: { supportsSnippets: true, supportsISO: true, content: 'iso,snippets' },
            },
            {
              id: 'network-vmbr0',
              name: 'vmbr0',
              provider: 'pve',
              connectionId: 'conn-pve',
              sourceKind: 'network',
              assetKind: 'network',
              node: 'pve-1',
              config: { bridge: true, network: 'vmbr0' },
            },
          ],
          total: 4,
          page: 1,
          pageSize: 10,
        },
      }
    }
    if (path === '/virtualization/flavors') {
      return {
        data: [
          {
            id: 'flavor-1',
            name: 'standard-2c4g',
            allowedActions: testState.catalogAllowedActions,
            cpu: 2,
            memoryMiB: 4096,
            diskGiB: 40,
            enabled: true,
          },
        ],
      }
    }
    if (path === '/virtualization/operations?assetType=asset_sync') {
      return {
        data: [
          {
            id: 'op-1',
            operationType: 'asset_sync',
            status: 'completed',
            targetName: 'kubevirt-a',
          },
        ],
      }
    }
    if (path === '/virtualization/operations?abnormal=true') {
      return {
        data: [
          {
            id: 'op-retry',
            operationType: 'asset_sync',
            status: 'failed',
            targetName: 'conn-a',
            connectionId: 'conn-a',
            message: 'sync failed',
            allowedActions: ['retry'],
          },
        ],
      }
    }
    if (path === '/virtualization/operations?abnormal=true&connectionId=conn-a') {
      return {
        data: [
          {
            id: 'op-retry',
            operationType: 'asset_sync',
            status: 'failed',
            targetName: 'conn-a',
            connectionId: 'conn-a',
            message: 'sync failed',
            allowedActions: ['retry'],
          },
        ],
      }
    }
    if (path === '/virtualization/operations') {
      return {
        data: [
          {
            id: 'op-cancel',
            operationType: 'vm_create',
            status: 'running',
            targetName: 'vm-a',
            allowedActions: ['cancel'],
          },
          {
            id: 'op-retry',
            operationType: 'asset_sync',
            status: 'failed',
            targetName: 'conn-a',
            connectionId: 'conn-a',
            message: 'sync failed',
            allowedActions: ['retry'],
          },
        ],
      }
    }
    if (path === '/virtualization/operations/op-1/logs') {
      return {
        data: [
          {
            id: 'log-1',
            taskId: 'op-1',
            logLevel: 'info',
            message: 'sync completed',
            createdAt: '2026-05-21T00:00:00Z',
          },
        ],
      }
    }
    throw new Error(`Unhandled GET ${path}`)
  }),
  apiPost: vi.fn(async (_path: string, _body?: unknown) => ({ data: { id: 'op-new' } })),
  apiPostWithHeaders: vi.fn(async (_path: string, _body?: unknown, _headers?: HeadersInit) => ({
    data: { id: 'op-new' },
  })),
  apiPut: vi.fn(async (_path: string, _body?: unknown) => ({ data: { id: 'updated' } })),
  apiDelete: vi.fn(async (_path: string) => ({ data: undefined })),
}))

vi.mock('@/features/auth/permission-snapshot', () => ({
  hasAllowedAction: (actions: string[] | undefined, action: string) =>
    actions?.includes(action) ?? false,
  hasPermission: (snapshot: { permissionKeys?: string[] } | undefined, key: string) =>
    snapshot?.permissionKeys?.includes(key) ?? false,
  usePermissionSnapshot: () => ({
    data: { data: testState.permissionSnapshot },
    isLoading: false,
  }),
}))

vi.mock('@/services/api-client', () => ({
  api: {
    get: (path: string) => testState.apiGet(path),
    post: (path: string, body?: unknown) => testState.apiPost(path, body),
    postWithHeaders: (path: string, body: unknown, headers: HeadersInit) =>
      testState.apiPostWithHeaders(path, body, headers),
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

  await settleQueries(queryClient)

  return container
}

async function settleQueries(queryClient: QueryClient) {
  let idleTicks = 0
  for (let index = 0; index < 50; index += 1) {
    await act(async () => {
      await Promise.resolve()
      await new Promise((resolve) => setTimeout(resolve, 0))
    })
    if (queryClient.isFetching() === 0 && queryClient.isMutating() === 0) {
      idleTicks += 1
      if (idleTicks >= 3) {
        return
      }
    } else {
      idleTicks = 0
    }
  }
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

async function waitForDeleteCall(path: string) {
  for (let index = 0; index < 10; index += 1) {
    await act(async () => {
      await new Promise((resolve) => setTimeout(resolve, 0))
    })
    if (testState.apiDelete.mock.calls.some(([calledPath]) => calledPath === path)) {
      return
    }
  }
}

async function waitForPostCall(path: string) {
  for (let index = 0; index < 10; index += 1) {
    await act(async () => {
      await new Promise((resolve) => setTimeout(resolve, 0))
    })
    if (testState.apiPost.mock.calls.some(([calledPath]) => calledPath === path)) {
      return
    }
  }
}

async function waitForGetCall(path: string) {
  for (let index = 0; index < 50; index += 1) {
    await act(async () => {
      await new Promise((resolve) => setTimeout(resolve, 0))
    })
    if (testState.apiGet.mock.calls.some(([calledPath]) => calledPath === path)) {
      return
    }
  }
  throw new Error(`GET call not observed: ${path}`)
}

async function clickTabByText(container: ParentNode, text: string) {
  const tab = Array.from(container.querySelectorAll('[role="tab"]')).find(
    (node) => node.textContent?.trim() === text,
  )
  if (!(tab instanceof HTMLElement)) {
    throw new Error(`tab not found by text: ${text}`)
  }
  await act(async () => {
    tab.click()
    await new Promise((resolve) => setTimeout(resolve, 0))
  })
}

async function clickButtonByLabel(container: ParentNode, label: string) {
  const button = Array.from(container.querySelectorAll('button')).find(
    (node) => node.getAttribute('aria-label') === label,
  )
  if (!(button instanceof HTMLButtonElement)) {
    throw new Error(`button not found by aria-label: ${label}`)
  }
  await act(async () => {
    button.dispatchEvent(new MouseEvent('click', { bubbles: true }))
    await Promise.resolve()
  })
}

async function clickButtonByText(container: ParentNode, text: string) {
  const button = Array.from(container.querySelectorAll('button')).find((node) =>
    node.textContent?.includes(text),
  )
  if (!(button instanceof HTMLButtonElement)) {
    throw new Error(`button not found by text: ${text}`)
  }
  await act(async () => {
    button.dispatchEvent(new MouseEvent('click', { bubbles: true }))
    await Promise.resolve()
  })
}

async function chooseVmAction(container: ParentNode, label: string) {
  await clickButtonByLabel(container, '更多操作：build-vm')
  await waitForText(label)
  const item = Array.from(document.body.querySelectorAll<HTMLElement>('[role="menuitem"]')).find(
    (node) => node.textContent?.trim() === label,
  )
  expect(item).toBeDefined()
  await act(async () => {
    item!.click()
    await Promise.resolve()
  })
}

function hasButtonByLabel(container: ParentNode, label: string) {
  return Array.from(container.querySelectorAll('button')).some(
    (node) => node.getAttribute('aria-label') === label,
  )
}

describe('virtualization pages', () => {
  beforeAll(() => {
    vi.stubGlobal('IS_REACT_ACT_ENVIRONMENT', true)
  })

  afterAll(() => {
    vi.unstubAllGlobals()
  })

  it('initializes resize values on first open and after the modal is destroyed', async () => {
    testState.vmAllowedActions = ['resize']
    const container = await renderWithProviders(
      <StrictMode>
        <VirtualizationVmsPage />
      </StrictMode>,
    )
    for (let attempt = 0; attempt < 2; attempt += 1) {
      await chooseVmAction(container, '调整虚拟机规格')
      await waitForGetCall('/virtualization/vms/vm-1/devices')
      const dialog = document.body.querySelector('[role="dialog"]')!
      expect(dialog.querySelector<HTMLInputElement>('#cpu')?.value).toBe('2')
      expect(dialog.querySelector<HTMLInputElement>('#memoryMiB')?.value).toBe('4096')
      await clickButtonByText(dialog, '下一步')
      await waitForText('系统盘设备')
      expect(dialog.querySelector<HTMLInputElement>('#rootDiskSizeGiB')?.value).toBe('40')
      await clickButtonByText(dialog, '取 消')
      await act(async () => {
        await new Promise((resolve) => setTimeout(resolve, 400))
      })
    }
    expect(testState.apiPost).not.toHaveBeenCalled()
    expect(testState.apiPostWithHeaders).not.toHaveBeenCalled()
  }, 15_000)
  it('uses root capacity rather than the multi-disk total for resize defaults', () => {
    expect(
      virtualMachineRootDiskGiB({ diskGiB: 48, config: { diskCount: 2, rootDiskGiB: '40' } }),
    ).toBe(40)
    expect(virtualMachineRootDiskGiB({ diskGiB: 48, config: { diskCount: 2 } })).toBeUndefined()
    expect(virtualMachineRootDiskGiB({ diskGiB: 40, config: { diskCount: 1 } })).toBe(40)
  })
  it('selects the real zero-slot boot disk across PVE bus types', () => {
    const disks = [
      { id: 'sata0', kind: 'disk', sizeGiB: 40 },
      { id: 'scsi1', kind: 'disk', sizeGiB: 100 },
    ] as const

    expect(defaultRootDisk([...disks], 40)?.id).toBe('sata0')
    expect(
      defaultRootDisk(
        [
          { id: 'virtio0', kind: 'disk', sizeGiB: 20 },
          { id: 'virtio1', kind: 'disk', sizeGiB: 80 },
        ],
        20,
      )?.id,
    ).toBe('virtio0')
  })
  beforeEach(() => {
    testState.modules = {
      docker: true,
      virtualization: true,
    }
    testState.permissionSnapshot = {
      permissionKeys: [
        'virtualization.vms.create',
        'virtualization.vms.power',
        'virtualization.vms.resize',
        'virtualization.vms.delete',
        'virtualization.sync.view',
        'virtualization.sync.sync',
        'virtualization.clusters.create',
        'virtualization.clusters.update',
        'virtualization.clusters.delete',
        'virtualization.clusters.test',
        'virtualization.operations.view',
        'virtualization.operations.cancel',
        'virtualization.operations.retry',
      ],
      visibleMenuIds: [],
      visibleMenus: [],
    }
    testState.vmAllowedActions = []
    testState.catalogAllowedActions = []
    testState.apiGet.mockClear()
    testState.apiPost.mockClear()
    testState.apiPostWithHeaders.mockClear()
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

  it('loads paginated VMs and creates from flavor plus image with provider fields', async () => {
    const container = await renderWithProviders(<VirtualizationVmsPage />)

    expect(testState.apiGet).toHaveBeenCalledWith('/virtualization/vms?page=1&pageSize=15')
    expect(testState.apiGet).toHaveBeenCalledWith('/virtualization/clusters')
    expect(testState.apiGet).toHaveBeenCalledWith('/virtualization/images?pageSize=500')
    expect(testState.apiGet).toHaveBeenCalledWith('/virtualization/flavors')
    expect(container.textContent).toContain('build-vm')
    expect(container.textContent).toContain('10.0.0.8')
    expect(container.querySelector('.soha-metadata-tag')).not.toBeNull()

    await act(async () => {
      const createButton = Array.from(container.querySelectorAll('button')).find((button) =>
        button.textContent?.includes('创建虚拟机'),
      )
      createButton?.dispatchEvent(new MouseEvent('click', { bubbles: true }))
      await Promise.resolve()
    })

    expect(document.body.textContent).toContain('Cloud-Init')
    expect(document.body.textContent).toContain('存储网络')
    expect(document.body.textContent).toContain('规格')
    expect(document.body.textContent).toContain('启动镜像')
    expect(document.body.textContent).toContain('StorageClass')
    expect(document.body.textContent).toContain('DataSource 克隆')
    expect(document.body.textContent).toContain('DataVolume')
    expect(document.body.textContent).toContain('KubeVirt 网络类型')
    expect(document.body.textContent).toContain('Interface Model')
    expect(document.body.textContent).not.toContain('Raw Cloud-Init user-data')
    expect(document.body.textContent).not.toContain('raw YAML')
    expect(document.body.textContent).not.toContain('raw PVE config')
  })

  it('shows stale VM records before their old power state', async () => {
    const listContainer = await renderWithProviders(<VirtualizationVmsPage />)
    const staleRow = Array.from(listContainer.querySelectorAll('tr')).find((row) =>
      row.textContent?.includes('stale-vm'),
    )

    expect(staleRow?.textContent).toContain('stale')
    expect(staleRow?.textContent).not.toContain('running')

    const detailContainer = await renderWithProviders(
      <VirtualizationVmDetailPage />,
      '/virtualization/vms/vm-stale',
    )

    expect(testState.apiGet).toHaveBeenCalledWith('/virtualization/vms/vm-stale/detail')
    expect(detailContainer.textContent).toContain('stale-vm')
    expect(detailContainer.textContent).toContain('stale')
  })

  it('submits VM deletion from the confirmation modal', async () => {
    testState.vmAllowedActions = ['delete']
    const container = await renderWithProviders(<VirtualizationVmsPage />)

    await chooseVmAction(container, '删除虚拟机')
    await waitForText('从 Provider 删除虚拟机')
    expect(document.body.textContent).toContain('此操作不会删除虚拟化连接')

    await clickButtonByText(document.body, '确认删除')
    await waitForPostCall('/virtualization/vms/vm-1/power')

    expect(testState.apiPost).toHaveBeenCalledWith('/virtualization/vms/vm-1/power', {
      action: 'delete',
    })
  })

  it('keeps the VM filter row on the compact aligned query layout', async () => {
    const container = await renderWithProviders(<VirtualizationVmsPage />)
    const query = container.querySelector('.soha-vrt-query.soha-vrt-vms-query')
    const fields = Array.from(
      query?.querySelectorAll<HTMLElement>('.soha-management-query-field') ?? [],
    )

    expect(query).not.toBeNull()
    expect(fields).toHaveLength(4)
    expect(fields.map((field) => field.textContent)).toEqual([
      expect.stringContaining('关键字'),
      expect.stringContaining('连接'),
      expect.stringContaining('状态'),
      expect.stringContaining('提供方'),
    ])
    expect(
      fields.map((field) => field.style.getPropertyValue('--soha-management-query-field-width')),
    ).toEqual(['300px', '180px', '136px', '160px'])
    expect(
      fields.map((field) =>
        field.style.getPropertyValue('--soha-management-query-field-min-width'),
      ),
    ).toEqual(['', '180px', '136px', '160px'])
  })

  it('uses the aligned query shell across virtualization list pages', async () => {
    const pages: Array<[ReactNode, string]> = [
      [<VirtualizationClustersPage />, '/virtualization/clusters'],
      [<VirtualizationImagesPage />, '/virtualization/images'],
      [<VirtualizationFlavorsPage />, '/virtualization/flavors'],
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
      kubevirtStorageClass: 'fast-ssd',
      kubevirtDataVolumeName: 'build-vm-root',
      kubevirtNetworkType: 'multus',
      kubevirtNetworkAttachmentDefinition: 'apps/docker-build-net',
      kubevirtInterfaceModel: 'virtio',
      kubevirtInterfaceBinding: 'bridge',
      kubevirtInterfaceName: 'net1',
    }) satisfies CreateVirtualMachineInput

    expect(payload.sourceMode).toBe('pvc_clone')
    expect(payload.sourceId).toBe('image-pvc')
    expect(payload.imageId).toBe('image-pvc')
    expect(payload.providerParams).toMatchObject({
      storageClass: 'fast-ssd',
      dataVolumeName: 'build-vm-root',
      networkType: 'multus',
      networkAttachmentDefinition: 'apps/docker-build-net',
      interfaceModel: 'virtio',
      interfaceBinding: 'bridge',
      interfaceName: 'net1',
    })
    expect(payload).not.toHaveProperty('kubevirtStorageClass')
    expect(payload).not.toHaveProperty('kubevirtDataVolumeName')
  })

  it('only offers VM flavors compatible with the selected provider and connection', () => {
    const flavors = [
      {
        id: 'pve-local',
        name: 'pve-small',
        provider: 'pve',
        connectionId: 'conn-pve',
        cpu: 2,
        memoryMiB: 4096,
        diskGiB: 20,
      },
      {
        id: 'kubevirt-local',
        name: 'u1.large',
        provider: 'kubevirt',
        connectionId: 'conn-kubevirt',
        cpu: 2,
        memoryMiB: 8192,
        diskGiB: 0,
      },
      {
        id: 'global',
        name: 'global-small',
        cpu: 1,
        memoryMiB: 2048,
        diskGiB: 10,
      },
    ]

    expect(filterVmCreateFlavors(flavors, 'pve', 'conn-pve').map((item) => item.id)).toEqual([
      'pve-local',
      'global',
    ])
  })

  it('builds PVE VM create payload with raw cloud-init snippet fields', () => {
    const payload = buildCreateVmPayload({
      provider: 'pve',
      connectionId: 'conn-pve',
      name: 'pve-vm',
      flavorId: 'flavor-1',
      bootImageId: 'image-pve-template',
      sourceMode: 'template_clone',
      enableCloudInit: true,
      cloudInit: '#cloud-config\npackages:\n  - docker.io',
      pveStorage: 'local-lvm',
      pveBridge: 'vmbr0',
      pveCloudInitUser: 'ubuntu',
      pveCloudInitSSHKeys: 'ssh-rsa AAAA',
      pveSnippetStorage: 'local',
      pveCICustom: 'user=local:snippets/docker-agent.yaml',
      startAfterCreate: true,
    }) satisfies CreateVirtualMachineInput

    expect(payload.sourceMode).toBe('template_clone')
    expect(payload.sourceId).toBe('image-pve-template')
    expect(payload.imageId).toBe('image-pve-template')
    expect(payload.cloudInit).toBe('#cloud-config\npackages:\n  - docker.io')
    expect(payload.providerParams).toMatchObject({
      storage: 'local-lvm',
      bridge: 'vmbr0',
      ciuser: 'ubuntu',
      sshkeys: 'ssh-rsa AAAA',
      snippetStorage: 'local',
      cicustom: 'user=local:snippets/docker-agent.yaml',
    })
    expect(payload).not.toHaveProperty('pveSnippetStorage')
    expect(payload).not.toHaveProperty('pveCICustom')
  })

  it('builds a PVE full clone from an ordinary VM source', () => {
    const payload = buildCreateVmPayload({
      provider: 'pve',
      connectionId: 'conn-pve',
      name: 'cloned-vm',
      sourceMode: 'vm_clone',
      templateId: '101',
      pveStorage: 'local-lvm',
      pveBridge: 'vmbr0',
    })

    expect(payload).toMatchObject({
      sourceMode: 'vm_clone',
      sourceId: '101',
      templateId: '101',
    })
    expect(payload.bootImageId).toBeUndefined()
    expect(payload.imageId).toBeUndefined()
  })

  it('clones the VM identity rather than its cloud image or clone origin', () => {
    const source = {
      id: 'created-vm',
      name: 'created-vm',
      externalId: '129',
      sourceRef: 'local:import/debian.qcow2',
      config: { sourceRef: 'local:import/debian.qcow2' },
    }
    const payload = buildCreateVmPayload({
      provider: 'pve',
      connectionId: 'conn-pve',
      name: 'cloned-vm',
      sourceMode: 'vm_clone',
      templateId: pveVMSourceRef(source),
    })
    expect(payload).toMatchObject({ sourceId: '129', templateId: '129' })
    expect(pveVMSourceRef({ ...source, sourceRef: '101' })).toBe('129')
    expect(pveVMSourceRef({ ...source, externalId: undefined, config: { vmid: 130 } })).toBe('130')
    expect(pveVMSourceRef({ ...source, externalId: undefined, config: undefined })).toBe('')
    expect(pveVMSourceRef({ id: 'legacy', name: 'legacy', sourceRef: '101' })).toBe('101')
  })

  it('creates from a catalog cloud image with managed credential references', () => {
    const payload = buildCreateVmPayload({
      provider: 'pve',
      connectionId: 'conn-pve',
      name: 'cloud-vm',
      sourceMode: 'cloud_image',
      bootImageId: 'verified-cloud-image',
      cpu: 2,
      memoryMiB: 2048,
      diskGiB: 20,
      enableCloudInit: true,
      managedGuestAccess: true,
      guestUsername: 'soha',
      guestPublicKeyRefs: ['secret://key@1'],
    })
    expect(payload).toMatchObject({
      sourceMode: 'cloud_image',
      imageId: 'verified-cloud-image',
      sourceId: 'verified-cloud-image',
      cpu: 2,
      memoryMiB: 2048,
      diskGiB: 20,
      guestAccess: { username: 'soha', publicKeyRefs: ['secret://key@1'] },
    })
    expect(payload.templateId).toBeUndefined()
  })

  it('maps VM creation to runtime-host onboarding without replacing Agent bootstrap', () => {
    const values = {
      provider: 'kubevirt',
      connectionId: 'conn-kubevirt',
      name: 'runtime-vm',
      bootImageId: 'image-pvc',
      sourceMode: 'pvc_clone',
      cpu: 4,
      memoryMiB: 8192,
      diskGiB: 80,
      network: 'pod',
      runtimeEnvironment: 'production',
      runtimeAvailablePortStart: 20000,
      runtimeAvailablePortEnd: 39999,
      runtimeControlPlaneBaseURL: 'http://soha.internal:8080',
      cloudInit: '#cloud-config\nusers: []',
      enableCloudInit: true,
    }
    const payload = buildRuntimeHostProvisionPayload(values)

    expect(payload).toMatchObject({
      name: 'runtime-vm',
      virtualizationConnectionId: 'conn-kubevirt',
      imageId: 'image-pvc',
      cpuCoreCount: 4,
      memoryBytes: 8192 * 1024 ** 2,
      diskBytes: 80 * 1024 ** 3,
      network: 'pod',
      environment: 'production',
      availablePortStart: 20000,
      availablePortEnd: 39999,
      config: {
        providerParams: {
          sourceMode: 'pvc_clone',
          controlPlaneBaseURL: 'http://soha.internal:8080',
        },
      },
    })
    expect(payload).not.toHaveProperty('cloudInit')
  })

  it('keeps managed public key references when onboarding a runtime host', () => {
    const payload = buildRuntimeHostProvisionPayload({
      connectionId: 'pve-one',
      name: 'runtime-vm',
      registerRuntimeHost: true,
      enableCloudInit: false,
      managedGuestAccess: true,
      guestUsername: 'soha',
      guestPublicKeyRefs: ['soha://secrets/key/versions/1'],
    })
    expect(payload.guestAccess).toEqual({
      username: 'soha',
      publicKeyRefs: ['soha://secrets/key/versions/1'],
    })
    expect(payload).not.toHaveProperty('cloudInit')
  })

  it('omits Cloud-Init fields when the create option is disabled', () => {
    const payload = buildCreateVmPayload({
      provider: 'pve',
      connectionId: 'conn-pve',
      name: 'pve-vm',
      bootImageId: 'image-pve-template',
      enableCloudInit: false,
      cloudInit: '#cloud-config',
      pveStorage: 'local-lvm',
      pveBridge: 'vmbr0',
      pveCloudInitUser: 'ubuntu',
      pveCloudInitSSHKeys: 'ssh-rsa AAAA',
      pveSnippetStorage: 'local',
      pveCICustom: 'user=local:snippets/docker-agent.yaml',
    })

    expect(payload.cloudInit).toBeUndefined()
    expect(payload.providerParams).toEqual({ storage: 'local-lvm', bridge: 'vmbr0' })
  })

  it('builds provider connection config for PVE and KubeVirt runtime fields', () => {
    const pvePayload = buildClusterPayload({
      provider: 'pve',
      name: 'pve-a',
      endpoint: 'https://pve.example:8006',
      defaultNode: 'pve-1',
      defaultStorage: 'local-lvm',
      defaultBridge: 'vmbr0',
      defaultSnippetStorage: 'local',
      tokenID: 'root@pam!soha',
      tokenSecret: 'secret',
    }) satisfies VirtualizationClusterInput
    expect(pvePayload.config).toMatchObject({
      defaultNode: 'pve-1',
      defaultStorage: 'local-lvm',
      defaultBridge: 'vmbr0',
      defaultSnippetStorage: 'local',
      snippetStorage: 'local',
    })
    expect(pvePayload.credential).toMatchObject({ tokenID: 'root@pam!soha', tokenSecret: 'secret' })
    expect(pvePayload).not.toHaveProperty('tokenID')
    expect(pvePayload).not.toHaveProperty('defaultNode')

    const kubevirtPayload = buildClusterPayload({
      provider: 'kubevirt',
      name: 'kubevirt-a',
      kubernetesClusterId: 'cluster-a',
      backendUrl: 'https://kube.example:6443',
      prometheusUrl: 'https://prometheus.example',
      prometheusBearerToken: 'secret',
      mode: 'direct_kubeconfig',
    }) satisfies VirtualizationClusterInput
    expect(kubevirtPayload.config).toMatchObject({
      backendUrl: 'https://kube.example:6443',
      prometheusUrl: 'https://prometheus.example',
      mode: 'direct_kubeconfig',
    })
    expect(kubevirtPayload.credential).toMatchObject({ prometheusBearerToken: 'secret' })
    expect(kubevirtPayload.endpoint).toBeUndefined()
    expect(kubevirtPayload).not.toHaveProperty('backendUrl')
    expect(kubevirtPayload).not.toHaveProperty('prometheusUrl')
    expect(kubevirtPayload.config).not.toHaveProperty('prometheusBearerToken')
  })

  it('preserves connection extensions without retaining cleared fields or crossing providers', () => {
    const existing = {
      id: 'conn-pve',
      name: 'pve-a',
      provider: 'pve',
      config: {
        defaultNode: 'old-node',
        defaultStorage: 'old-storage',
        snippetStorage: 'old-snippets',
        sshHost: 'node.example',
        sshHostKeys: { 'old-node': 'trusted-public-key' },
        providerExtension: { enabled: true },
        mode: 'provider-extension-mode',
      },
    }
    const payload = buildClusterPayload(
      { name: 'renamed', provider: 'pve', defaultNode: 'new-node' },
      existing,
    )
    expect(payload.config).toEqual({
      defaultNode: 'new-node',
      sshHost: 'node.example',
      sshHostKeys: { 'old-node': 'trusted-public-key' },
      providerExtension: { enabled: true },
      mode: 'provider-extension-mode',
    })
    expect(payload.credential).toBeUndefined()
    expect(existing.config.defaultNode).toBe('old-node')
    expect(
      buildClusterPayload({ name: 'changed-provider', provider: 'kubevirt' }, existing).config,
    ).toEqual({})
    expect(
      buildClusterPayload(
        { name: 'cleared', provider: 'pve' },
        { ...existing, config: { defaultNode: 'old-node' } },
      ).config,
    ).toEqual({})
  })

  it('renders VM detail with provider raw, operations, logs and AI investigation entry', async () => {
    const container = await renderWithProviders(
      <VirtualizationVmDetailPage />,
      '/virtualization/vms/vm-1',
    )

    expect(testState.apiGet).toHaveBeenCalledWith('/virtualization/vms/vm-1/detail')
    expect(container.textContent).toContain('build-vm')
    expect(container.textContent).toContain('概览')
    expect(container.textContent).toContain('standard-2c4g')
    expect(container.textContent).toContain('ubuntu-24.04')
    expect(container.textContent).toContain('10.0.0.8')
    expect(container.textContent).toContain('高级信息')
    expect(container.textContent).toContain('资源关系')
    expect(container.textContent).toContain('运行与硬件')
    expect(container.textContent).toContain('网络与身份')
    expect(container.textContent).toContain('初始化与来源')
    expect(container.textContent).toContain('build-vm.apps.svc.cluster.local')
    expect(container.textContent).toContain('10.96.0.10')
    expect(container.textContent).toContain('Cloud-Init NoCloud')
    expect(container.textContent).toContain('任务历史')
    expect(container.textContent).toContain('vm ready')
    expect(container.textContent).toContain('最近异常任务')
    expect(container.textContent).toContain('restart failed')
    expect(container.textContent).toContain('AI调查')
    expect(container.textContent).toContain('来源模式')
    expect(container.textContent).toContain('来源引用')
    expect(container.textContent).not.toContain('Console 能力摘要')
    expect(container.textContent).not.toContain('Metrics 能力摘要')
    expect(container.querySelector('.soha-management-detail-header')).toBeNull()
    expect(
      container.querySelector('[role="tabpanel"][aria-hidden="false"] > .soha-detail-stack'),
    ).not.toBeNull()
    expect(container.textContent).not.toContain('返回列表')
    expect(container.querySelector('.ant-tabs-tab-active')?.textContent).toContain('概览')
    const tabTexts = Array.from(container.querySelectorAll('.ant-tabs-tab-btn')).map((node) =>
      node.textContent?.trim(),
    )
    expect(tabTexts).not.toContain('Provider Raw')
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

  it('uses exact VM create and metrics permissions', async () => {
    testState.permissionSnapshot = {
      permissionKeys: [
        'virtualization.vms.create',
        'virtualization.vms.metrics',
        'virtualization.vms.console',
      ],
      visibleMenuIds: [],
      visibleMenus: [],
    }

    const listContainer = await renderWithProviders(<VirtualizationVmsPage />)
    expect(listContainer.textContent).toContain('创建虚拟机')

    const detailContainer = await renderWithProviders(
      <VirtualizationVmDetailPage />,
      '/virtualization/vms/vm-1',
    )
    const tabTexts = Array.from(detailContainer.querySelectorAll('.ant-tabs-tab-btn'))
      .map((node) => node.textContent?.trim())
      .filter(Boolean)
    expect(tabTexts).toContain('监控指标')
    expect(tabTexts).toContain('控制台')
    const metricsPath = '/virtualization/vms/vm-1/metrics?rangeMinutes=60&stepSeconds=60'
    const consolePath = '/virtualization/vms/vm-1/console'
    expect(testState.apiGet).not.toHaveBeenCalledWith(metricsPath)
    expect(testState.apiGet).not.toHaveBeenCalledWith(consolePath)
    expect(lazyRuntimeState.metricsLoads).toBe(0)
    expect(lazyRuntimeState.consoleLoads).toBe(0)

    await clickTabByText(detailContainer, '监控指标')
    await waitForGetCall(metricsPath)

    expect(testState.apiGet).toHaveBeenCalledWith(metricsPath)
    expect(lazyRuntimeState.metricsLoads).toBeGreaterThan(0)
    expect(testState.apiGet).not.toHaveBeenCalledWith(consolePath)
    expect(lazyRuntimeState.consoleLoads).toBe(0)

    await clickTabByText(detailContainer, '控制台')
    await waitForGetCall(consolePath)
    await waitForText('Console disabled in tests')

    expect(testState.apiGet).toHaveBeenCalledWith(consolePath)
    expect(lazyRuntimeState.consoleLoads).toBe(1)
    expect(detailContainer.textContent).toContain('控制台暂不可用')
    expect(detailContainer.textContent).toContain('Console disabled in tests')
  })

  it('does not load virtualization data or expose actions when the module is disabled', async () => {
    testState.modules = {
      docker: true,
      virtualization: false,
    }
    testState.permissionSnapshot = {
      permissionKeys: [
        'virtualization.vms.create',
        'virtualization.sync.sync',
        'virtualization.clusters.create',
        'virtualization.operations.view',
      ],
      visibleMenuIds: [],
      visibleMenus: [],
    }

    const container = await renderWithProviders(<VirtualizationVmsPage />)

    expect(testState.apiGet).toHaveBeenCalledWith('/modules')
    expect(testState.apiGet).not.toHaveBeenCalledWith('/virtualization/vms?page=1&pageSize=15')
    expect(testState.apiGet).not.toHaveBeenCalledWith('/virtualization/clusters')
    expect(testState.apiGet).not.toHaveBeenCalledWith('/virtualization/images')
    expect(testState.apiGet).not.toHaveBeenCalledWith('/virtualization/flavors')
    expect(container.textContent).not.toContain('创建虚拟机')
  })

  it('requires explicit VM allowed actions for power and delete controls', async () => {
    testState.permissionSnapshot = {
      permissionKeys: ['virtualization.vms.power', 'virtualization.vms.delete'],
      visibleMenuIds: [],
      visibleMenus: [],
    }

    const container = await renderWithProviders(<VirtualizationVmsPage />)

    expect(container.textContent).toContain('build-vm')
    expect(hasButtonByLabel(container, '启动虚拟机')).toBe(false)
    expect(hasButtonByLabel(container, '停止虚拟机')).toBe(false)
    expect(hasButtonByLabel(container, '更多操作：build-vm')).toBe(false)
  })

  it('gates VM metrics query and console tab by dedicated permissions', async () => {
    testState.permissionSnapshot = {
      permissionKeys: ['virtualization.vms.view'],
      visibleMenuIds: [],
      visibleMenus: [],
    }

    const container = await renderWithProviders(
      <VirtualizationVmDetailPage />,
      '/virtualization/vms/vm-1',
    )

    expect(testState.apiGet).toHaveBeenCalledWith('/virtualization/vms/vm-1/detail')
    const tabTexts = Array.from(container.querySelectorAll('.ant-tabs-tab-btn'))
      .map((node) => node.textContent?.trim())
      .filter(Boolean)
    expect(tabTexts).toContain('概览')
    expect(tabTexts).not.toContain('Provider Raw')
    expect(tabTexts).not.toContain('监控指标')
    expect(tabTexts).not.toContain('控制台')
    expect(testState.apiGet).not.toHaveBeenCalledWith(
      '/virtualization/vms/vm-1/metrics?rangeMinutes=60&stepSeconds=60',
    )
  })

  it('shows provider-specific cluster connection fields', async () => {
    await renderWithProviders(
      <VirtualizationConnectionStepModal
        initialProvider="kubevirt"
        onClose={() => undefined}
        open
      />,
      '/compute/virtualization/clusters',
    )

    expect(testState.apiGet).toHaveBeenCalledWith('/clusters')
    expect(document.body.textContent).toContain('Kubernetes 集群')
    expect(document.body.textContent).toContain('Prometheus Bearer Token')
    expect(document.body.textContent).not.toContain('Other')
    expect(document.body.textContent).toContain('校验 TLS')
  })

  it('shows and requires only the selected PVE credential mode', async () => {
    await renderWithProviders(
      <VirtualizationConnectionStepModal initialProvider="pve" onClose={() => undefined} open />,
      '/compute/virtualization/clusters',
    )

    expect(document.body.textContent).toContain('认证方式')
    expect(document.querySelector('input[id$="username"]')).not.toBeNull()
    expect(document.querySelector('input[id$="password"]')).not.toBeNull()
    expect(document.querySelector('input[id$="tokenID"]')).toBeNull()
    expect(document.querySelector('label[for$="username"]')?.className).toContain(
      'ant-form-item-required',
    )
    expect(document.querySelector('label[for$="password"]')?.className).toContain(
      'ant-form-item-required',
    )

    const apiTokenMode = Array.from(
      document.querySelectorAll<HTMLElement>('.ant-segmented-item'),
    ).find((item) => item.textContent?.trim() === 'API Token')
    expect(apiTokenMode).toBeDefined()
    await act(async () => {
      apiTokenMode?.click()
      await Promise.resolve()
    })

    expect(document.querySelector('input[id$="username"]')).toBeNull()
    expect(document.querySelector('input[id$="password"]')).toBeNull()
    expect(document.querySelector('input[id$="tokenID"]')).not.toBeNull()
    expect(document.querySelector('input[id$="tokenSecret"]')).not.toBeNull()
    expect(document.querySelector('label[for$="tokenID"]')?.className).toContain(
      'ant-form-item-required',
    )
    expect(document.querySelector('label[for$="tokenSecret"]')?.className).toContain(
      'ant-form-item-required',
    )
  })

  it.each([
    { status: 'healthy', healthy: true, label: '连接正常' },
    { status: 'degraded', healthy: false, label: '连接异常' },
    { status: 'unavailable', healthy: false, label: '连接异常' },
  ])(
    'shows the actual connection health result ($status/$healthy)',
    async ({ status, healthy, label }) => {
      testState.apiPost.mockResolvedValueOnce({
        data: { status, healthy, message: 'probe details', checkedAt: '2026-09-22T00:00:00Z' },
      } as never)
      const container = await renderWithProviders(
        <VirtualizationClustersPage />,
        '/virtualization/clusters',
      )
      await clickButtonByLabel(container, '测试连接')
      await waitForText('连接测试结果')
      expect(document.body.textContent).toContain(label)
      expect(document.body.textContent).toContain('probe details')
      expect(document.body.textContent).not.toContain('测试任务已提交')
      expect(testState.apiPost).toHaveBeenCalledWith(
        '/virtualization/clusters/conn-pve/test',
        undefined,
      )
    },
  )

  it('reports every batch connection result even when one request fails', async () => {
    testState.apiPost.mockResolvedValueOnce({
      data: { status: 'healthy', healthy: true, checkedAt: '2026-09-22T00:00:00Z' },
    } as never)
    testState.apiPost.mockRejectedValueOnce(new Error('连接已移除'))
    const container = await renderWithProviders(
      <VirtualizationClustersPage />,
      '/virtualization/clusters',
    )
    await act(async () =>
      container.querySelector<HTMLInputElement>('thead input[type="checkbox"]')?.click(),
    )
    await clickButtonByText(container, '批量测试')
    const confirm = document.querySelector<HTMLButtonElement>(
      '.ant-popconfirm-buttons .ant-btn-primary',
    )
    expect(confirm).not.toBeNull()
    await act(async () => confirm?.click())
    await waitForText('连接测试结果')
    const result = document.querySelector('.ant-modal-confirm-content')
    expect(result?.textContent).toContain('连接正常')
    expect(result?.textContent).toContain('连接已移除')
    expect(result?.textContent).toContain('pve-a')
    expect(result?.textContent).toContain('kubevirt-a')
  })

  it('restores saved connection fields after closing and reopening the editor', async () => {
    const container = await renderWithProviders(
      <StrictMode>
        <VirtualizationClustersPage />
      </StrictMode>,
      '/virtualization/clusters',
    )
    await clickButtonByLabel(container, '编辑连接')
    expect(document.querySelector<HTMLInputElement>('#name')?.value).toBe('pve-a')
    await act(async () => {
      const name = document.querySelector<HTMLInputElement>('#name')!
      Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')?.set?.call(
        name,
        'unsaved name',
      )
      name.dispatchEvent(new Event('input', { bubbles: true }))
    })
    await clickButtonByText(document.body, '下一步')
    expect(document.querySelector<HTMLInputElement>('#endpoint')?.value).toBe(
      'https://pve.example:8006',
    )
    await act(async () => document.querySelector<HTMLButtonElement>('.ant-modal-close')!.click())
    await act(async () => new Promise((resolve) => setTimeout(resolve, 400)))
    expect(document.querySelector('.ant-modal')).toBeNull()

    await clickButtonByLabel(container, '编辑连接')
    expect(document.querySelector<HTMLInputElement>('#name')?.value).toBe('pve-a')
    await clickButtonByText(document.body, '下一步')
    expect(document.querySelector<HTMLInputElement>('#endpoint')?.value).toBe(
      'https://pve.example:8006',
    )
    expect(document.querySelector('#verifyTls')?.getAttribute('aria-checked')).toBe('true')
    expect(document.querySelector('#password')).toBeNull()
    await clickButtonByText(document.body, '上一步')
    expect(document.querySelector<HTMLInputElement>('#name')?.value).toBe('pve-a')
    await clickButtonByText(document.body, '下一步')
    await clickButtonByText(document.body, '下一步')
    const defaults = Array.from(
      document.querySelectorAll<HTMLElement>('.ant-collapse-header'),
    ).find((element) => element.textContent?.includes('PVE 资源默认值（可选）'))!
    await act(async () => defaults.click())
    expect(document.querySelector<HTMLInputElement>('#defaultNode')?.value).toBe('pve-1')
    await clickButtonByText(document.body, '保存连接')
    expect(testState.apiPut).toHaveBeenCalledWith(
      '/virtualization/clusters/conn-pve',
      expect.objectContaining({
        name: 'pve-a',
        provider: 'pve',
        endpoint: 'https://pve.example:8006',
        verifyTls: true,
        config: expect.objectContaining({
          defaultNode: 'pve-1',
          sshHost: 'node.example',
          sshHostKeys: { 'pve-1': 'trusted-public-key' },
        }),
      }),
    )
    expect(
      (testState.apiPut.mock.calls[0]?.[1] as VirtualizationClusterInput).credential,
    ).toBeUndefined()
  }, 15_000)

  it.each([
    {
      label: '连接',
      page: <VirtualizationClustersPage />,
      route: '/virtualization/clusters',
      edit: '编辑连接',
      steps: 2,
    },
    {
      label: '镜像',
      page: <VirtualizationImagesPage />,
      route: '/virtualization/images',
      edit: '编辑镜像',
      steps: 0,
    },
    {
      label: '规格',
      page: <VirtualizationFlavorsPage />,
      route: '/virtualization/flavors',
      edit: '编辑规格',
      steps: 0,
    },
  ])(
    'clears the previous $label save error when opening a fresh editor',
    async ({ page, route, edit, steps }) => {
      testState.permissionSnapshot.permissionKeys.push(
        'virtualization.images.view',
        'virtualization.images.update',
        'virtualization.flavors.view',
        'virtualization.flavors.update',
      )
      testState.catalogAllowedActions = ['update']
      testState.apiPut.mockRejectedValueOnce(new Error('previous save rejected'))
      const container = await renderWithProviders(page, route)
      await clickButtonByLabel(container, edit)
      for (let index = 0; index < steps; index++) await clickButtonByText(document.body, '下一步')
      if (steps) await clickButtonByText(document.body, '保存连接')
      else
        await act(async () =>
          document.querySelector<HTMLButtonElement>('.ant-modal-footer .ant-btn-primary')!.click(),
        )
      await waitForText('previous save rejected')
      await act(async () => document.querySelector<HTMLButtonElement>('.ant-modal-close')!.click())
      await act(async () => new Promise((resolve) => setTimeout(resolve, 400)))
      await clickButtonByLabel(container, edit)
      expect(document.querySelector('.ant-modal')?.textContent).not.toContain(
        'previous save rejected',
      )
    },
  )

  it('does not carry edited connection values into another connection or a new one', async () => {
    const container = await renderWithProviders(
      <StrictMode>
        <VirtualizationClustersPage />
      </StrictMode>,
      '/virtualization/clusters',
    )
    await clickButtonByLabel(container, '编辑连接')
    await act(async () => document.querySelector<HTMLButtonElement>('.ant-modal-close')!.click())
    await act(async () => new Promise((resolve) => setTimeout(resolve, 400)))

    await act(async () => {
      container.querySelectorAll<HTMLButtonElement>('button[aria-label="编辑连接"]')[1]!.click()
    })
    expect(document.querySelector<HTMLInputElement>('#name')?.value).toBe('kubevirt-a')
    await clickButtonByText(document.body, '下一步')
    expect(document.body.textContent).toContain('Kubernetes 集群')
    expect(document.querySelector<HTMLInputElement>('#backendUrl')?.value).toBe(
      'https://kube.example:6443',
    )
    expect(document.querySelector('#endpoint')).toBeNull()
    await act(async () => document.querySelector<HTMLButtonElement>('.ant-modal-close')!.click())
    await act(async () => new Promise((resolve) => setTimeout(resolve, 400)))

    await clickButtonByText(container, '新增连接')
    expect(document.querySelector<HTMLInputElement>('#name')?.value).toBe('')
    expect(document.querySelector<HTMLInputElement>('#backendUrl')?.value).toBe('')
    expect(document.querySelector('#verifyTls')?.getAttribute('aria-checked')).toBe('true')
    expect(document.querySelector('#enabled')?.getAttribute('aria-checked')).toBe('true')
    expect(testState.apiPost).not.toHaveBeenCalled()
    expect(testState.apiPut).not.toHaveBeenCalled()
  }, 15_000)

  it('preserves an unresolved write receipt when reopening a flavor editor', async () => {
    testState.permissionSnapshot.permissionKeys.push(
      'virtualization.flavors.view',
      'virtualization.flavors.update',
    )
    testState.catalogAllowedActions = ['update']
    testState.apiPut.mockRejectedValueOnce(
      new ApiError(503, 'pending record verification', { method: 'PUT' }),
    )
    const container = await renderWithProviders(
      <VirtualizationFlavorsPage />,
      '/virtualization/flavors',
    )
    await clickButtonByLabel(container, '编辑规格')
    await act(async () =>
      document.querySelector<HTMLButtonElement>('.ant-modal-footer .ant-btn-primary')!.click(),
    )
    await waitForText('pending record verification')
    await act(async () => document.querySelector<HTMLButtonElement>('.ant-modal-close')!.click())
    await act(async () => new Promise((resolve) => setTimeout(resolve, 400)))
    await clickButtonByLabel(container, '编辑规格')
    expect(document.querySelector('.ant-modal')?.textContent).toContain('请求结果不明')
    expect(document.querySelector('.ant-modal')?.textContent).toContain('flavor-1')
  })

  it('keeps optional PVE defaults collapsed while editing a connection', async () => {
    const container = await renderWithProviders(
      <VirtualizationClustersPage />,
      '/virtualization/clusters',
    )

    await clickButtonByLabel(container, '编辑连接')

    expect(document.body.textContent).toContain('更新凭证')
    expect(document.body.textContent).not.toContain('Token Secret')
    expect(document.body.textContent).toContain('PVE 资源默认值（可选）')
    const pveDefaults = Array.from(
      document.querySelectorAll<HTMLElement>('.ant-collapse-item'),
    ).find((item) => item.textContent?.includes('PVE 资源默认值（可选）'))
    expect(pveDefaults?.classList.contains('ant-collapse-item-active')).toBe(false)
    expect(document.body.textContent).not.toContain('raw JSON')
    expect(container.textContent).toContain('风险')
    expect(container.textContent).not.toContain('最近失败同步')
    const latestTaskHeader = Array.from(container.querySelectorAll('th')).find(
      (header) => header.textContent?.trim() === '最近任务',
    )
    expect(latestTaskHeader?.className).toContain('ant-table-cell-fix-end')

    const expandButton = container.querySelector<HTMLButtonElement>(
      'button.ant-table-row-expand-icon',
    )
    expect(expandButton).not.toBeNull()
    await act(async () => {
      expandButton?.click()
      await Promise.resolve()
    })

    expect(container.textContent).toContain('风险等级')
    expect(container.textContent).toContain('最近失败同步')
    expect(container.textContent).toContain('最近异常任务')
  })

  it('previews dependencies before force deleting a virtualization connection', async () => {
    const container = await renderWithProviders(
      <VirtualizationClustersPage />,
      '/virtualization/clusters',
    )

    await clickButtonByLabel(container, '删除连接')
    await waitForText('仅删除 Soha 中的连接与同步记录')

    expect(testState.apiGet).toHaveBeenCalledWith(
      '/virtualization/clusters/conn-pve/delete-dependencies',
    )
    expect(document.body.textContent).toContain(
      '不会删除 PVE/KubeVirt 中的 VM、镜像、磁盘或其他 Provider 资源',
    )
    expect(document.body.textContent).toContain('Docker Host')
    expect(document.body.textContent).toContain('build-vm')

    await clickButtonByText(document.body, '确认从 Soha 删除')
    await waitForDeleteCall('/virtualization/clusters/conn-pve?force=true')

    expect(testState.apiDelete).toHaveBeenCalledWith('/virtualization/clusters/conn-pve?force=true')
  })

  it('keeps the delete preview open and reports a force delete failure', async () => {
    testState.apiDelete.mockRejectedValueOnce(new Error('历史任务快照写入失败'))
    const container = await renderWithProviders(
      <VirtualizationClustersPage />,
      '/virtualization/clusters',
    )

    await clickButtonByLabel(container, '删除连接')
    await waitForText('仅删除 Soha 中的连接与同步记录')
    await clickButtonByText(document.body, '确认从 Soha 删除')
    await waitForText('历史任务快照写入失败')

    expect(document.body.textContent).toContain('历史任务快照写入失败')
    expect(document.body.textContent).toContain('删除连接：pve-a')
  })

  it('shows image management entries for KubeVirt and PVE sources', async () => {
    testState.permissionSnapshot = {
      permissionKeys: ['virtualization.images.view', 'virtualization.images.create'],
      visibleMenuIds: [],
      visibleMenus: [],
    }
    const container = await renderWithProviders(
      <VirtualizationImagesPage />,
      '/virtualization/images',
    )

    expect(testState.apiGet).toHaveBeenCalledWith(
      '/virtualization/images?page=1&pageSize=15&category=catalog',
    )
    expect(container.textContent).toContain('KubeVirt')
    expect(container.textContent).toContain('PVE')
    expect(container.textContent).toContain('DataSource')
    expect(container.textContent).toContain('VM 模板')
    expect(container.textContent).toContain('新增镜像入口')

    await act(async () => {
      const addButton = Array.from(container.querySelectorAll('button')).find((button) =>
        button.textContent?.includes('新增镜像入口'),
      )
      addButton?.dispatchEvent(new MouseEvent('click', { bubbles: true }))
      await Promise.resolve()
    })

    expect(document.body.textContent).toContain('KubeVirt DataSource')
    expect(document.body.textContent).toContain('来源类型')
    expect(document.querySelector('.soha-step-form__steps')).toBeNull()
    expect(document.querySelector('#sourceRef')?.closest('[hidden]')).toBeNull()
  })

  it('validates and saves all flavor fields in one form, resetting cancelled changes', async () => {
    testState.permissionSnapshot.permissionKeys = [
      'virtualization.flavors.view',
      'virtualization.flavors.create',
    ]
    const container = await renderWithProviders(
      <StrictMode>
        <VirtualizationFlavorsPage />
      </StrictMode>,
    )
    await clickButtonByText(container, '新增规格')
    expect(document.querySelector('.soha-step-form__steps')).toBeNull()
    expect(document.querySelector<HTMLInputElement>('#cpu')?.value).toBe('2')
    expect(document.querySelector<HTMLInputElement>('#memoryMiB')?.value).toBe('4096')

    const submit = async () => {
      await act(async () => {
        document
          .querySelector('.ant-modal form')
          ?.dispatchEvent(new Event('submit', { bubbles: true, cancelable: true }))
        await new Promise((resolve) => setTimeout(resolve, 0))
      })
    }
    const setName = async (value: string) => {
      await act(async () => {
        const input = document.querySelector<HTMLInputElement>('#name')
        Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')?.set?.call(
          input,
          value,
        )
        input?.dispatchEvent(new Event('input', { bubbles: true }))
      })
    }
    await submit()
    expect(testState.apiPost).not.toHaveBeenCalled()
    expect(document.querySelector('#name')?.getAttribute('aria-invalid')).toBe('true')
    await setName('cancelled-flavor')
    await act(async () => {
      document.querySelector<HTMLButtonElement>('.ant-modal-close')?.click()
    })
    await clickButtonByText(container, '新增规格')
    expect(document.querySelector<HTMLInputElement>('#name')?.value).toBe('')
    await setName('small-flavor')
    await submit()
    await waitForPostCall('/virtualization/flavors')
    expect(testState.apiPost).toHaveBeenCalledWith('/virtualization/flavors', {
      name: 'small-flavor',
      cpu: 2,
      memoryMiB: 4096,
      diskGiB: 40,
      enabled: true,
      description: undefined,
    })
  })

  it('shows storage pools, VM disks, and CT volumes on the storage route', async () => {
    const container = await renderWithProviders(
      <VirtualizationImagesPage category="storage" />,
      '/compute/virtualization/storage',
    )

    expect(testState.apiGet).toHaveBeenCalledWith(
      '/virtualization/images?page=1&pageSize=15&category=storage',
    )
    expect(container.textContent).toContain('存储池')
    expect(container.textContent).toContain('VM 磁盘')
    expect(container.textContent).toContain('CT 根卷')
    expect(container.textContent).not.toContain('新增镜像入口')
  })

  it('uses image and flavor allowed actions for row edit and delete controls', async () => {
    testState.permissionSnapshot = {
      permissionKeys: [
        'virtualization.images.view',
        'virtualization.images.create',
        'virtualization.flavors.view',
        'virtualization.flavors.create',
      ],
      visibleMenuIds: [],
      visibleMenus: [],
    }

    const imageContainer = await renderWithProviders(
      <VirtualizationImagesPage />,
      '/virtualization/images',
    )
    expect(imageContainer.textContent).toContain('ubuntu-24.04')
    expect(imageContainer.textContent).toContain('新增镜像入口')
    expect(hasButtonByLabel(imageContainer, '编辑镜像')).toBe(false)
    expect(hasButtonByLabel(imageContainer, '删除镜像')).toBe(false)

    const flavorContainer = await renderWithProviders(
      <VirtualizationFlavorsPage />,
      '/virtualization/flavors',
    )
    expect(flavorContainer.textContent).toContain('standard-2c4g')
    expect(flavorContainer.textContent).toContain('新增规格')
    expect(hasButtonByLabel(flavorContainer, '编辑规格')).toBe(false)
    expect(hasButtonByLabel(flavorContainer, '删除规格')).toBe(false)
  })
})
