/** @vitest-environment jsdom */

import { act } from 'react'
import { App as AntdApp } from 'antd'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { createRoot } from 'react-dom/client'
import { MemoryRouter } from 'react-router-dom'
import { afterEach, beforeAll, beforeEach, describe, expect, it, vi } from 'vitest'
import type {
  EndpointDevice,
  NetworkGateway,
  NetworkMihomoProfile,
  NetworkNASBinding,
  NetworkTelemetrySummary,
} from '@opensoha/contracts/gen/ts/sohaapi'
import type { PermissionSnapshot } from '@/types'
import { ApiError, isApiErrorPresented } from '@/services/api-error'
import { networkAccessQueries } from './queries'
import { NetworkAccessPage } from './page'

const state = vi.hoisted(() => ({
  snapshot: { permissionKeys: [], visibleMenuIds: [], visibleMenus: [] } as PermissionSnapshot,
}))

const apiMocks = vi.hoisted(() => ({
  analyzeNetworkAccessConflicts: vi.fn(),
  compileNetworkAccessPolicySnapshot: vi.fn(),
  createNetworkAccessPolicy: vi.fn(),
  createNetworkAccessGrant: vi.fn(),
  createNetworkGateway: vi.fn(),
  createNetworkMihomoProfile: vi.fn(),
  createNetworkNASBinding: vi.fn(),
  createNetworkResource: vi.fn(),
  createNetworkRuntimeEnrollment: vi.fn(),
  createNetworkSite: vi.fn(),
  createNetworkSiteProfileBinding: vi.fn(),
  createNetworkSpace: vi.fn(),
  deleteNetworkAccessPolicy: vi.fn(),
  deleteNetworkMihomoProfile: vi.fn(),
  deleteNetworkNASBinding: vi.fn(),
  deleteNetworkResource: vi.fn(),
  deleteNetworkSite: vi.fn(),
  deleteNetworkSiteProfileBinding: vi.fn(),
  deleteNetworkSpace: vi.fn(),
  getNetworkAccessPolicySnapshot: vi.fn(() =>
    Promise.resolve({
      policyVersion: 3,
      contentHash: 'sha256:' + 'a'.repeat(64),
      policyCount: 1,
      protectedResourceCount: 1,
      publishedAt: '2026-09-02T00:00:00Z',
    }),
  ),
  getNetworkTelemetrySummary: vi.fn(() =>
    Promise.resolve({
      from: '2026-09-02T00:00:00Z',
      to: '2026-09-02T01:00:00Z',
      eventCount: 0,
      heartbeatCount: 0,
      radiusAccountingCount: 0,
      networkFlowCount: 0,
      connectionSummaryCount: 0,
      proxyFlowCount: 0,
      uploadBytes: 0,
      downloadBytes: 0,
      activeConnections: 0,
      producers: [],
      proxyFlows: [],
    } as NetworkTelemetrySummary),
  ),
  listNetworkAccessPolicies: vi.fn(() => Promise.resolve([])),
  listNetworkAccessGrants: vi.fn(() => Promise.resolve([])),
  listNetworkNASBindings: vi.fn(() => Promise.resolve([] as NetworkNASBinding[])),
  listEndpointDevices: vi.fn(() => Promise.resolve([] as EndpointDevice[])),
  listNetworkGateways: vi.fn(() => Promise.resolve([] as NetworkGateway[])),
  listNetworkMihomoProfiles: vi.fn(() => Promise.resolve([] as NetworkMihomoProfile[])),
  listNetworkResources: vi.fn(() => Promise.resolve([])),
  listNetworkRuntimeEnrollments: vi.fn(() => Promise.resolve([])),
  listNetworkSites: vi.fn(() => Promise.resolve([])),
  listNetworkSiteProfileBindings: vi.fn(() => Promise.resolve([])),
  listNetworkSessions: vi.fn(() => Promise.resolve([])),
  listNetworkSpaces: vi.fn(() => Promise.resolve([])),
  previewNetworkAccessPolicy: vi.fn(),
  previewNetworkAdmission: vi.fn(),
  executeNetworkSessionAction: vi.fn(),
  planNetworkSessionAction: vi.fn(),
  revokeNetworkRuntimeEnrollment: vi.fn(),
  revokeNetworkAccessGrant: vi.fn(),
  updateEndpointDevice: vi.fn(),
  updateNetworkResource: vi.fn(),
  updateNetworkSite: vi.fn(),
  updateNetworkSpace: vi.fn(),
  updateNetworkAccessPolicy: vi.fn(),
  updateNetworkGateway: vi.fn(),
  updateNetworkMihomoProfile: vi.fn(),
  updateNetworkNASBinding: vi.fn(),
  updateNetworkSiteProfileBinding: vi.fn(),
}))

vi.mock('@/features/auth', () => ({
  usePermissionSnapshot: () => ({ data: { data: state.snapshot }, isLoading: false }),
  hasPermission: (snapshot: PermissionSnapshot | undefined, key: string) =>
    snapshot?.permissionKeys.includes(key) ?? false,
}))
vi.mock('./api', () => apiMocks)

let container: HTMLDivElement
let root: ReturnType<typeof createRoot>

async function renderPage(path = '/network-access') {
  container = document.createElement('div')
  document.body.appendChild(container)
  root = createRoot(container)
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  await act(async () => {
    root.render(
      <AntdApp>
        <QueryClientProvider client={queryClient}>
          <MemoryRouter initialEntries={[path]}>
            <NetworkAccessPage />
          </MemoryRouter>
        </QueryClientProvider>
      </AntdApp>,
    )
  })
  await act(async () => new Promise((resolve) => window.setTimeout(resolve, 0)))
  return queryClient
}

describe('network access page', () => {
  beforeAll(() => {
    vi.stubGlobal('IS_REACT_ACT_ENVIRONMENT', true)
    vi.stubGlobal(
      'ResizeObserver',
      class {
        observe() {}
        unobserve() {}
        disconnect() {}
      },
    )
    Object.defineProperty(window, 'matchMedia', {
      writable: true,
      value: vi.fn(() => ({
        matches: false,
        addListener: vi.fn(),
        removeListener: vi.fn(),
        addEventListener: vi.fn(),
        removeEventListener: vi.fn(),
      })),
    })
    const getComputedStyle = window.getComputedStyle.bind(window)
    vi.spyOn(window, 'getComputedStyle').mockImplementation((element) => getComputedStyle(element))
  })

  beforeEach(() => {
    state.snapshot = { permissionKeys: [], visibleMenuIds: [], visibleMenus: [] }
    vi.clearAllMocks()
    apiMocks.listEndpointDevices.mockResolvedValue([])
  })

  afterEach(async () => {
    await act(async () => root?.unmount())
    container?.remove()
  })

  it('fails closed when no Phase 1 view permission is present', async () => {
    await renderPage()
    expect(container.textContent).toContain('当前账号没有网络访问工作台权限。')
    expect(apiMocks.listNetworkSites).not.toHaveBeenCalled()
  })

  it('renders the section selected by the global navigation route', async () => {
    state.snapshot.permissionKeys = ['network_access.sites.view', 'network_access.sites.update']
    await renderPage('/network-access/settings?tab=nas-bindings')

    expect(container.querySelector('.soha-network-access-nav')).toBeNull()
    expect(container.querySelector('h1')).toBeNull()
    expect(container.querySelector('.soha-resource-tabs')).not.toBeNull()
    expect(container.textContent).not.toContain(
      '统一管理设备、站点、网络空间、ProtectedSet 资源、网关与访问路径决策。',
    )
    expect(container.textContent).toContain('新增网络设备')
    expect(apiMocks.listNetworkNASBindings).toHaveBeenCalledWith({ limit: 200 })
    expect(apiMocks.listNetworkSites).toHaveBeenCalledWith({ limit: 200 })
    expect(apiMocks.listEndpointDevices).not.toHaveBeenCalled()
  })

  it('combines Wi-Fi and wired records on the user admission page', async () => {
    const base = {
      runtimeId: 'freeradius-hq',
      siteId: 'site-hq',
      status: 'active',
      coaSupported: true,
      disconnectSupported: true,
      createdAt: '2026-09-04T00:00:00Z',
      updatedAt: '2026-09-04T00:00:00Z',
    } as const
    apiMocks.listNetworkNASBindings.mockResolvedValueOnce([
      {
        ...base,
        id: 'wifi-1',
        name: '总部办公 Wi-Fi',
        nasId: 'nas-hq-wifi',
        accessMedium: 'wifi',
        deviceType: 'wireless_controller',
        ssid: 'Soha-Staff',
      },
      {
        ...base,
        id: 'wired-1',
        name: '总部接入交换机',
        nasId: 'nas-hq-switch',
        accessMedium: 'wired',
        deviceType: 'switch',
      },
    ])
    state.snapshot.permissionKeys = ['network_access.sites.view', 'network_access.sites.update']

    await renderPage('/network-access/user-admission')
    await act(async () => {
      await vi.waitFor(() => expect(container.textContent).toContain('Soha-Staff'))
    })

    expect(container.textContent).toContain('总部办公 Wi-Fi')
    expect(container.textContent).toContain('总部接入交换机')
    expect(container.textContent).toContain('新增入网配置')
  })

  it('keeps SSID settings limited to Wi-Fi records', async () => {
    const base = {
      runtimeId: 'freeradius-hq',
      siteId: 'site-hq',
      status: 'active',
      coaSupported: true,
      disconnectSupported: true,
      createdAt: '2026-09-04T00:00:00Z',
      updatedAt: '2026-09-04T00:00:00Z',
    } as const
    apiMocks.listNetworkNASBindings.mockResolvedValueOnce([
      {
        ...base,
        id: 'wifi-1',
        name: '总部办公 Wi-Fi',
        nasId: 'nas-hq-wifi',
        accessMedium: 'wifi',
        deviceType: 'wireless_controller',
        ssid: 'Soha-Staff',
      },
      {
        ...base,
        id: 'wired-1',
        name: '总部接入交换机',
        nasId: 'nas-hq-switch',
        accessMedium: 'wired',
        deviceType: 'switch',
      },
    ])
    state.snapshot.permissionKeys = ['network_access.sites.view', 'network_access.sites.update']

    await renderPage('/network-access/settings?tab=ssids')
    await act(async () => {
      await vi.waitFor(() => expect(container.textContent).toContain('Soha-Staff'))
    })

    expect(container.textContent).not.toContain('RADIUS 接入')
    expect(container.textContent).toContain('新增 SSID')
    expect(container.textContent).not.toContain('总部接入交换机')
  })

  it('switches among the permission-scoped admission setting tabs', async () => {
    state.snapshot.permissionKeys = [
      'network_access.sites.view',
      'network_access.sites.update',
      'network_access.enrollments.view',
    ]
    await renderPage('/network-access/settings')

    const tabs = Array.from(container.querySelectorAll<HTMLElement>('[role="tab"]'))
    expect(tabs.map((tab) => tab.textContent)).toEqual(['网络设备', 'SSID', 'RADIUS 接入'])
    expect(container.textContent).toContain('新增网络设备')
    expect(apiMocks.listNetworkNASBindings).toHaveBeenCalledWith({ limit: 200 })
    expect(apiMocks.listNetworkRuntimeEnrollments).not.toHaveBeenCalled()

    await act(async () => tabs[1]?.click())
    await act(async () => {
      await vi.waitFor(() =>
        expect(apiMocks.listNetworkNASBindings).toHaveBeenCalledWith({ limit: 200 }),
      )
    })
    expect(tabs[1]?.getAttribute('aria-selected')).toBe('true')
    expect(container.textContent).toContain('新增 SSID')
    await act(async () => tabs[2]?.click())
    await act(async () => {
      await vi.waitFor(() =>
        expect(apiMocks.listNetworkRuntimeEnrollments).toHaveBeenCalledWith({ limit: 200 }),
      )
    })
    expect(container.textContent).toContain('接入说明')
    expect(container.textContent).not.toContain('运行时类型')
    expect(container.textContent).toContain('注册状态')
  })

  it('shows terminal type and collected network details', async () => {
    const device = {
      id: 'endpoint-mac-1',
      name: 'shanchui-mac.local',
      ownerUserId: 'user-1',
      status: 'active',
      platform: 'darwin',
      deviceType: 'laptop',
      ownershipType: 'company',
      postureStatus: 'compliant',
      postureVersion: 2,
      lastSeenAt: '2026-09-03T09:00:00Z',
      reportedFacts: {
        osName: 'macOS',
        osVersion: '15.6.1',
        architecture: 'arm64',
        manufacturer: 'Apple',
        model: 'MacBookAir15,2',
        serialNumber: 'C02TEST00001',
        agentVersion: '0.2.0',
        collectedAt: '2026-09-03T09:00:00Z',
        networkInterfaces: [
          {
            name: 'en0',
            kind: 'physical',
            status: 'up',
            macAddress: '00:11:22:33:44:55',
            ipv4Addresses: ['192.168.1.10'],
            ipv6Addresses: [],
          },
        ],
      },
      createdAt: '2026-09-03T09:00:00Z',
      updatedAt: '2026-09-03T09:00:00Z',
    } as EndpointDevice
    state.snapshot.permissionKeys = ['network_access.endpoint_devices.view']
    apiMocks.listEndpointDevices.mockResolvedValueOnce([device])
    await renderPage('/network-access/devices')

    await act(async () => {
      await vi.waitFor(() => expect(container.textContent).toContain('macOS 15.6.1'))
    })
    expect(container.textContent).toContain('笔记本')
    const view = container.querySelector<HTMLButtonElement>('button[aria-label="查看终端详情"]')
    expect(view).not.toBeNull()
    await act(async () => view?.click())
    const basicTab = Array.from(document.body.querySelectorAll<HTMLElement>('[role="tab"]')).find(
      (tab) => tab.textContent?.includes('基本信息'),
    )
    expect(basicTab).not.toBeUndefined()
    expect(basicTab?.getAttribute('aria-selected')).toBe('true')
    await act(async () => {
      await vi.waitFor(() => expect(document.body.textContent).toContain('C02TEST00001'))
    })
    const networkTab = Array.from(document.body.querySelectorAll<HTMLElement>('[role="tab"]')).find(
      (tab) => tab.textContent?.includes('网卡信息'),
    )
    expect(networkTab).not.toBeUndefined()
    await act(async () => networkTab?.click())
    await act(async () => {
      await vi.waitFor(() => expect(document.body.textContent).toContain('192.168.1.10'))
    })
  })

  it('keeps policy authoring behind policy permissions', async () => {
    state.snapshot.permissionKeys = [
      'network_access.policy.view',
      'network_access.policy.create',
      'network_access.policy.update',
    ]
    await renderPage('/network-access/policy')

    expect(container.textContent).toContain('策略草稿')
    expect(container.textContent).toContain('新增策略')
    expect(container.textContent).toContain('发布草稿')
    expect(apiMocks.listNetworkAccessPolicies).toHaveBeenCalledWith({ limit: 200 })
    expect(apiMocks.getNetworkAccessPolicySnapshot).toHaveBeenCalled()
  })

  it('saves a policy scoped only to current role bindings', async () => {
    state.snapshot.permissionKeys = ['network_access.policy.view', 'network_access.policy.create']
    await renderPage('/network-access/policy')
    const create = Array.from(container.querySelectorAll('button')).find((button) =>
      button.textContent?.includes('新增策略'),
    )
    await act(async () => create?.click())
    const dialog = document.querySelector('[role="dialog"]')!
    const name = dialog.querySelector('input#name') as HTMLInputElement
    const roles = dialog.querySelector('textarea#rolesText') as HTMLTextAreaElement
    expect(roles).not.toBeNull()
    await act(async () => {
      Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')?.set?.call(
        name,
        'Role admission',
      )
      name.dispatchEvent(new Event('input', { bubbles: true }))
      name.dispatchEvent(new Event('change', { bubbles: true }))
      Object.getOwnPropertyDescriptor(HTMLTextAreaElement.prototype, 'value')?.set?.call(
        roles,
        'network-staff',
      )
      roles.dispatchEvent(new Event('input', { bubbles: true }))
      roles.dispatchEvent(new Event('change', { bubbles: true }))
    })
    const save = Array.from(dialog.querySelectorAll('button')).find(
      (button) => button.textContent?.replace(/\s/g, '') === '保存',
    )
    expect(save).toBeDefined()
    await act(async () => save?.click())
    await act(async () => {
      await vi.waitFor(() =>
        expect(apiMocks.createNetworkAccessPolicy, dialog.textContent ?? '').toHaveBeenCalled(),
      )
    })
    expect(apiMocks.createNetworkAccessPolicy.mock.calls[0]?.[0]).toMatchObject({
      name: 'Role admission',
      subjects: { users: [], roles: ['network-staff'], teams: [], tags: [] },
    })
  })

  it('previews admission for an endpoint and clears the old decision when its target changes', async () => {
    state.snapshot.permissionKeys = ['network_access.policy.view']
    const version = 'sha256:' + 'a'.repeat(64)
    apiMocks.previewNetworkAdmission.mockResolvedValueOnce({
      decision: 'deny',
      path: 'deny',
      networkProfile: 'deny',
      policyVersion: 4,
      attributeVersion: version,
      evaluatedAt: '2026-10-15T05:00:00Z',
      protected: false,
      networkLeaseRequired: false,
      resourceLeaseRequired: false,
      reasons: ['no_matching_allow_policy'],
    })
    await renderPage('/network-access/policy')
    const tab = Array.from(container.querySelectorAll('[role="tab"]')).find((item) =>
      item.textContent?.includes('准入预览'),
    ) as HTMLElement
    await act(async () => tab.click())
    const device = container.querySelector('input#deviceId') as HTMLInputElement
    const binding = container.querySelector('input#nasBindingId') as HTMLInputElement
    const setter = Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')?.set
    await act(async () => {
      setter?.call(device, 'device-1')
      device.dispatchEvent(new Event('input', { bubbles: true }))
      setter?.call(binding, 'nas-binding-1')
      binding.dispatchEvent(new Event('input', { bubbles: true }))
    })
    const submit = Array.from(container.querySelectorAll('button')).find((item) =>
      item.textContent?.includes('预览准入'),
    )!
    await act(async () => submit.click())
    await vi.waitFor(async () => {
      await act(async () => new Promise((resolve) => window.setTimeout(resolve, 0)))
      expect(container.textContent).toContain('当前策略拒绝接入')
    })
    expect(apiMocks.previewNetworkAdmission.mock.calls[0]?.[0]).toEqual({
      deviceId: 'device-1',
      nasBindingId: 'nas-binding-1',
    })
    expect(container.textContent).toContain(version)
    expect(apiMocks.listEndpointDevices).not.toHaveBeenCalled()
    expect(apiMocks.listNetworkNASBindings).not.toHaveBeenCalled()
    await act(async () => {
      setter?.call(device, 'device-2')
      device.dispatchEvent(new Event('input', { bubbles: true }))
    })
    expect(container.textContent).not.toContain('当前策略拒绝接入')
  })

  it.each([401, 403, 503])(
    'handles cached admission choices after a %i response',
    async (status) => {
      state.snapshot.permissionKeys = [
        'network_access.policy.view',
        'network_access.endpoint_devices.view',
      ]
      const device: EndpointDevice = {
        id: 'cached-device',
        name: 'Cached endpoint',
        ownerUserId: 'user-1',
        status: 'active',
        platform: 'linux',
        postureStatus: 'compliant',
        postureVersion: 1,
        createdAt: '2026-10-15T00:00:00Z',
        updatedAt: '2026-10-15T00:00:00Z',
      }
      apiMocks.listEndpointDevices.mockResolvedValue([device])
      const client = await renderPage('/network-access/policy')
      const tab = Array.from(container.querySelectorAll<HTMLElement>('[role="tab"]')).find((item) =>
        item.textContent?.includes('准入预览'),
      )!
      await act(async () => tab.click())
      await vi.waitFor(async () => {
        await act(async () => new Promise((resolve) => window.setTimeout(resolve, 0)))
        expect(client.getQueryData(networkAccessQueries.devices({ limit: 200 }).queryKey)).toEqual([
          device,
        ])
      })
      const input = container.querySelector<HTMLInputElement>('input#deviceId')!
      const setter = Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')?.set
      await act(async () => {
        input.focus()
        setter?.call(input, 'Cached')
        input.dispatchEvent(new Event('input', { bubbles: true }))
        input.dispatchEvent(
          new KeyboardEvent('keydown', { key: 'ArrowDown', keyCode: 40, bubbles: true }),
        )
      })
      await vi.waitFor(async () => {
        await act(async () => new Promise((resolve) => window.setTimeout(resolve, 0)))
        expect(document.body.textContent).toContain('Cached endpoint')
      })
      const error = new ApiError(status, 'Endpoint choices unavailable', {
        method: 'GET',
        path: '/network-access/devices',
        requestId: 'admission-choices-request',
      })
      apiMocks.listEndpointDevices.mockRejectedValueOnce(error)
      await act(async () => {
        await client.refetchQueries({
          queryKey: networkAccessQueries.devices({ limit: 200 }).queryKey,
        })
      })
      await vi.waitFor(async () => {
        await act(async () => new Promise((resolve) => window.setTimeout(resolve, 0)))
        expect(container.textContent).toContain('admission-choices-request')
      })
      if (status === 503) {
        expect(document.body.textContent).toContain('Cached endpoint')
        expect(input.getAttribute('aria-expanded')).toBe('true')
        expect(container.textContent).toContain('终端选项未更新')
      } else {
        await act(async () => {
          input.dispatchEvent(
            new KeyboardEvent('keydown', { key: 'ArrowDown', keyCode: 40, bubbles: true }),
          )
        })
        expect(input.getAttribute('aria-expanded')).toBe('false')
        expect(container.textContent).toContain(status === 401 ? '登录状态失效' : '无访问权限')
      }
      expect(input.value).toBe('Cached')
      expect(client.getQueryData(networkAccessQueries.devices({ limit: 200 }).queryKey)).toEqual([
        device,
      ])
      expect(isApiErrorPresented(error)).toBe(status !== 401)
      expect(apiMocks.previewNetworkAdmission).not.toHaveBeenCalled()
    },
  )

  it('renders runtime enrollments only with the enrollment view permission', async () => {
    state.snapshot.permissionKeys = ['network_access.enrollments.view']
    await renderPage('/network-access/enrollments')

    expect(apiMocks.listNetworkRuntimeEnrollments).toHaveBeenCalledWith({ limit: 200 })
    expect(apiMocks.listNetworkSites).not.toHaveBeenCalled()
  })

  it('renders access grants only with the access-grant view permission', async () => {
    state.snapshot.permissionKeys = ['network_access.access_grants.view']
    await renderPage('/network-access/access-grants')

    expect(apiMocks.listNetworkAccessGrants).toHaveBeenCalledWith({ limit: 200 })
    expect(apiMocks.listNetworkSites).not.toHaveBeenCalled()
  })

  it('renders mihomo profiles only with the profile view permission', async () => {
    state.snapshot.permissionKeys = ['network_access.mihomo_profiles.view']
    await renderPage('/network-access/mihomo-profiles')

    expect(apiMocks.listNetworkMihomoProfiles).toHaveBeenCalledWith({ limit: 200 })
    expect(apiMocks.listNetworkSites).not.toHaveBeenCalled()
    expect(container.querySelector('.soha-management-query-card')).not.toBeNull()
    expect(
      container.querySelector('.soha-management-table-shell .soha-admin-table-header'),
    ).not.toBeNull()
  })

  it('separates proxy sources and execution policy in the tunnel editor', async () => {
    state.snapshot.permissionKeys = [
      'network_access.mihomo_profiles.view',
      'network_access.mihomo_profiles.create',
    ]
    await renderPage('/network-access/mihomo-profiles')

    expect(container.textContent).not.toContain('独立启动的代理服务不会自动出现')

    const create = Array.from(document.querySelectorAll('button')).find((button) =>
      button.textContent?.includes('新增代理隧道'),
    )
    await act(async () => create?.click())

    expect(document.body.textContent).toContain('连接来源')
    expect(document.body.textContent).toContain('订阅导入')
    expect(document.body.textContent).toContain('手动节点')
    expect(document.body.textContent).toContain('执行策略')
    expect(document.body.textContent).toContain('订阅 URL（只写）')
    expect(document.body.textContent).toContain('跟随模式')

    const manual = Array.from(document.querySelectorAll('.ant-segmented-item')).find((item) =>
      item.textContent?.includes('手动节点'),
    ) as HTMLElement | undefined
    await act(async () => manual?.click())

    expect(document.body.textContent).toContain('地址')
    expect(document.body.textContent).toContain('端口')
  })

  it('renders aggregate telemetry only with the telemetry view permission', async () => {
    state.snapshot.permissionKeys = ['network_access.telemetry.view']
    await renderPage('/network-access/telemetry')

    expect(container.textContent).toContain('遥测事件')
    expect(apiMocks.getNetworkTelemetrySummary).toHaveBeenCalledWith({ limit: 100 })
    expect(apiMocks.listNetworkSites).not.toHaveBeenCalled()
  })

  it('renders the proxy traffic overview from bounded telemetry', async () => {
    state.snapshot.permissionKeys = ['network_access.telemetry.view']
    apiMocks.getNetworkTelemetrySummary.mockResolvedValueOnce({
      from: '2026-09-02T00:00:00Z',
      to: '2026-09-02T01:00:00Z',
      eventCount: 4,
      heartbeatCount: 1,
      radiusAccountingCount: 0,
      networkFlowCount: 0,
      connectionSummaryCount: 0,
      proxyFlowCount: 3,
      uploadBytes: 1024,
      downloadBytes: 4096,
      activeConnections: 2,
      producers: [],
      proxyFlows: [
        {
          producerId: 'runtime-1',
          engine: 'mihomo',
          profileId: 'profile-1',
          profileRevision: 1,
          mode: 'managed_follow',
          selectedProxy: '上海节点',
          uploadBytes: 1024,
          downloadBytes: 1024,
          activeConnections: 1,
          lastOccurredAt: '2026-09-02T00:59:00Z',
        },
        {
          producerId: 'runtime-2',
          engine: 'mihomo',
          profileId: 'profile-2',
          profileRevision: 1,
          mode: 'managed_follow',
          selectedProxy: '东京节点',
          uploadBytes: 0,
          downloadBytes: 3072,
          activeConnections: 1,
          lastOccurredAt: '2026-09-02T00:59:00Z',
        },
      ],
    })

    await renderPage('/network-access/proxy-overview')

    expect(container.textContent).toContain('代理上报事件')
    expect(container.textContent).toContain('采样连接数')
    expect(container.textContent).toContain('5.0 KB')
    expect(container.textContent).toContain('列表最多展示 100 组汇总')
    expect(container.textContent).toContain('代理流量')
    expect(container.querySelector('.soha-management-query-card')).not.toBeNull()
    expect(container.querySelectorAll('.soha-proxy-flow-list > li')).toHaveLength(2)
    expect(container.querySelector('.soha-proxy-flow-list > li strong')?.textContent).toBe(
      '东京节点',
    )
    expect(container.querySelector('.soha-proxy-list-panel')).not.toBeNull()
    expect(container.querySelector('.soha-management-table-shell')).toBeNull()
    expect(apiMocks.getNetworkTelemetrySummary).toHaveBeenCalledWith({ limit: 100 })
  })

  it('shows an actionable error instead of zero traffic when telemetry is unavailable', async () => {
    state.snapshot.permissionKeys = ['network_access.telemetry.view']
    apiMocks.getNetworkTelemetrySummary.mockRejectedValueOnce(new Error('unavailable'))

    await renderPage('/network-access/proxy-overview')

    expect(container.textContent).toContain('代理遥测暂不可用')
    expect(container.textContent).not.toContain('总传输量')
    expect(container.querySelector('.soha-management-query-card')).toBeNull()
    expect(container.querySelector('.soha-proxy-list-panel')).not.toBeNull()
    const refresh = Array.from(container.querySelectorAll('button')).find((button) =>
      button.textContent?.replace(/\s/g, '').includes('刷新'),
    )
    expect(refresh).toBeDefined()
    await act(async () => refresh?.click())
    expect(apiMocks.getNetworkTelemetrySummary).toHaveBeenCalledTimes(2)
    await act(async () => new Promise((resolve) => window.setTimeout(resolve, 0)))
    expect(container.textContent).toContain('总传输量')
  })

  it('joins proxy flows to their user and endpoint without raw connection data', async () => {
    state.snapshot.permissionKeys = [
      'network_access.telemetry.view',
      'network_access.mihomo_profiles.view',
      'network_access.endpoint_devices.view',
    ]
    apiMocks.listNetworkMihomoProfiles.mockResolvedValueOnce([
      {
        id: 'profile-1',
        deviceId: 'device-1',
        name: '办公代理',
        mode: 'managed_follow',
        status: 'active',
        subscriptionConfigured: true,
        revision: 1,
        mixedPort: 7890,
        controllerPort: 9090,
        dnsMode: 'fake_ip',
        fakeIpRange: '198.18.0.0/15',
        selectorGroup: 'PROXY',
        selectedProxy: '上海节点',
        bypassCidrs: [],
        bypassHosts: [],
        failClosed: true,
        createdAt: '2026-09-02T00:00:00Z',
        updatedAt: '2026-09-02T00:00:00Z',
      },
    ])
    apiMocks.listEndpointDevices.mockResolvedValueOnce([
      {
        id: 'device-1',
        name: '张三的 MacBook',
        ownerUserId: 'user-zhangsan',
        status: 'active',
        platform: 'darwin',
        postureStatus: 'compliant',
        postureVersion: 1,
        createdAt: '2026-09-02T00:00:00Z',
        updatedAt: '2026-09-02T00:00:00Z',
      } as EndpointDevice,
    ])
    apiMocks.getNetworkTelemetrySummary.mockResolvedValueOnce({
      from: '2026-09-02T00:00:00Z',
      to: '2026-09-02T01:00:00Z',
      eventCount: 1,
      heartbeatCount: 0,
      radiusAccountingCount: 0,
      networkFlowCount: 0,
      connectionSummaryCount: 0,
      proxyFlowCount: 1,
      uploadBytes: 1024,
      downloadBytes: 4096,
      activeConnections: 2,
      producers: [],
      proxyFlows: [
        {
          producerId: 'runtime-1',
          engine: 'mihomo',
          profileId: 'profile-1',
          profileRevision: 1,
          mode: 'managed_follow',
          selectedProxy: '上海节点',
          uploadBytes: 1024,
          downloadBytes: 4096,
          activeConnections: 2,
          lastOccurredAt: '2026-09-02T00:59:00Z',
        },
      ],
    })

    await renderPage('/network-access/proxy-connections')

    expect(container.textContent).toContain('user-zhangsan')
    expect(container.textContent).toContain('张三的 MacBook')
    expect(container.textContent).toContain('办公代理')
    expect(container.textContent).toContain('上海节点')
    expect(container.textContent).toContain('采样连接数')
    expect(container.textContent).not.toContain('连接中')
    expect(container.textContent).not.toContain('destination')
    expect(container.querySelector('.soha-management-query-card')).not.toBeNull()
    expect(container.querySelectorAll('.soha-proxy-connection-list > li')).toHaveLength(1)
    expect(container.querySelector('.soha-management-table-shell')).toBeNull()

    const input = container.querySelector('.soha-management-query-card input') as HTMLInputElement
    const setter = Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')?.set
    await act(async () => {
      setter?.call(input, 'no-such-user')
      input.dispatchEvent(new Event('input', { bubbles: true }))
      input.dispatchEvent(new Event('change', { bubbles: true }))
    })
    await act(async () => {
      container
        .querySelector('form.soha-management-query-form')
        ?.dispatchEvent(new Event('submit', { bubbles: true, cancelable: true }))
    })
    expect(container.textContent).not.toContain('user-zhangsan')
    expect(container.querySelectorAll('.soha-proxy-connection-list > li')).toHaveLength(0)
    expect(container.textContent).toContain('没有符合查询条件的连接采样组')
  })

  it('keeps the connection query and retry visible when telemetry is unavailable', async () => {
    state.snapshot.permissionKeys = [
      'network_access.telemetry.view',
      'network_access.mihomo_profiles.view',
      'network_access.endpoint_devices.view',
    ]
    apiMocks.getNetworkTelemetrySummary.mockRejectedValueOnce(new Error('unavailable'))

    await renderPage('/network-access/proxy-connections')

    expect(container.textContent).toContain('无法读取代理汇总')
    expect(container.querySelector('.soha-management-query-card')).toBeNull()
    expect(container.querySelector('.soha-proxy-list-panel')).not.toBeNull()
  })

  it('shows hub-and-spoke gateway fields and management actions', async () => {
    state.snapshot.permissionKeys = [
      'network_access.gateways.view',
      'network_access.gateways.create',
      'network_access.gateways.update',
    ]
    apiMocks.listNetworkGateways.mockResolvedValueOnce([
      {
        id: 'gateway-b',
        runtimeId: 'gateway-b',
        siteId: 'site-b',
        name: 'Branch B',
        administrativeStatus: 'active',
        status: 'online',
        publicEndpointHost: 'vpn-b.example.com',
        publicEndpointPort: 51821,
        overlayCidr: '100.96.1.0/24',
        routingMode: 'routed',
        hubGatewayId: 'gateway-a',
        advertisedCidrs: ['10.20.0.0/16'],
        mtu: 1420,
        persistentKeepaliveSeconds: 25,
        dnsServers: ['10.20.0.53'],
        createdAt: '2026-09-03T00:00:00Z',
        updatedAt: '2026-09-03T00:00:00Z',
      },
    ])
    await renderPage('/network-access/gateways')

    expect(container.textContent).toContain('总部网关')
    expect(container.textContent).toContain('gateway-a')
    expect(container.textContent).toContain('10.20.0.0/16')
    expect(container.textContent).toContain('新增网关')
    expect(container.querySelector('[aria-label="编辑网关"]')).not.toBeNull()
  })
})
