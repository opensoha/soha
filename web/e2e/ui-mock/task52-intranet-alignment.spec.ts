import { test, expect, type BrowserContext, type Page } from '@playwright/test'
import { installMocks, verifyBrowserEvidence } from '../fixtures/mock-app'
import { verifyGeometry } from '../fixtures/geometry'
import type { VisibleMenu } from '../../src/types'

const time = '2026-10-18T01:00:00Z'
const device = {
  id: 'endpoint-engineering-long-identifier',
  name: '研发终端 MacBook · engineering-endpoint-with-a-long-name',
  hostname: 'engineering-mac.local',
  platform: 'macOS',
  deviceType: 'laptop',
  ownershipType: 'company',
  ownerUserId: 'user-engineering-long-identifier',
  siteId: 'site-office',
  status: 'active',
  postureStatus: 'compliant',
  lastSeenAt: time,
  reportedFacts: {
    osName: 'macOS',
    osVersion: '27.0',
    collectedAt: time,
    networkInterfaces: [
      {
        name: 'en0',
        kind: 'ethernet',
        status: 'up',
        ipv6Addresses: ['2001:db8:1234:5678:abcd:1234:5678:9012'],
        ipv4Addresses: ['192.0.2.10'],
        dnsServers: ['192.0.2.1'],
      },
    ],
  },
}
const profile = {
  id: 'vpn-office',
  revision: 2,
  publishedRevision: 1,
  configuration: {
    name: '研发 VPN 方案',
    enabled: true,
    gatewayIds: [],
    networkSpaceIds: [],
    allowedPathModes: ['relay'],
    preferredPathMode: 'relay',
  },
}
const routes = [
  'devices',
  'sites',
  'spaces',
  'resources',
  'gateways',
  'user-admission',
  'settings',
  'site-profile-bindings',
  'sessions',
  'enrollments',
  'access-grants',
  'policy',
  'mihomo-profiles',
  'telemetry',
  'vpn/profiles',
  'vpn/selection-policies',
  'vpn/dashboard',
]
const permissions = [
  'endpoint_devices',
  'sites',
  'spaces',
  'resources',
  'gateways',
  'mihomo_profiles',
  'enrollments',
  'access_grants',
  'policy',
  'telemetry',
  'vpn_profiles',
  'vpn_selection_policies',
  'vpn_dashboard',
].map((key) => `network_access.${key}.view`)

const sceneGroups: VisibleMenu[] = [
  {
    id: 'network-access-infrastructure',
    parentId: 'identity',
    path: '/network-access/infrastructure',
    labelZh: '站点与网络',
    labelEn: 'Sites & Networks',
    section: 'vpn',
    sortOrder: 10,
  },
  {
    id: 'network-access-controls',
    parentId: 'identity',
    path: '/network-access/controls',
    labelZh: '访问控制',
    labelEn: 'Access Control',
    section: 'vpn',
    sortOrder: 30,
  },
  {
    id: 'network-access-vpn',
    parentId: 'identity',
    path: '/network-access/vpn',
    labelZh: 'VPN 配置',
    labelEn: 'VPN Configuration',
    section: 'vpn',
    sortOrder: 40,
  },
  {
    id: 'network-access-monitoring',
    parentId: 'identity',
    path: '/network-access/monitoring',
    labelZh: '连接监控',
    labelEn: 'Connection Monitoring',
    section: 'vpn',
    sortOrder: 50,
  },
]
const groupedRoutes: Record<string, [string, number]> = {
  sites: ['infrastructure', 10],
  spaces: ['infrastructure', 20],
  gateways: ['infrastructure', 30],
  policy: ['controls', 10],
  'access-grants': ['controls', 20],
  'site-profile-bindings': ['controls', 30],
  'vpn/profiles': ['vpn', 10],
  'vpn/selection-policies': ['vpn', 20],
  sessions: ['monitoring', 10],
  telemetry: ['monitoring', 20],
  'vpn/dashboard': ['monitoring', 30],
}
const menuLabels: Record<string, [string, string]> = {
  devices: ['终端资产', 'Endpoint Assets'],
  sites: ['站点', 'Sites'],
  spaces: ['网络空间', 'Network Spaces'],
  gateways: ['网关', 'Gateways'],
  resources: ['网络资源', 'Network Resources'],
  policy: ['策略', 'Policies'],
  'access-grants': ['访问授权', 'Access Grants'],
  'site-profile-bindings': ['接入等级', 'Access Levels'],
  enrollments: ['运行时接入', 'Runtime Access'],
  settings: ['入网配置', 'Admission Configuration'],
  'user-admission': ['用户入网', 'User Admission'],
  'vpn/profiles': ['VPN 连接方案', 'VPN Profiles'],
  'vpn/selection-policies': ['VPN Auto 策略', 'VPN Auto Policies'],
  'vpn/dashboard': ['VPN 看板', 'VPN Dashboard'],
  sessions: ['会话', 'Sessions'],
  telemetry: ['遥测', 'Telemetry'],
}
function intranetMenus(): VisibleMenu[] {
  return [
    {
      id: 'identity',
      path: '/internal-workbench',
      labelZh: '内网工作台',
      labelEn: 'Internal Workbench',
    },
    ...sceneGroups,
    ...routes.map((route, index) => ({
      id: `network-access-${route.replace(/\//g, '-')}`,
      parentId: groupedRoutes[route] ? `network-access-${groupedRoutes[route][0]}` : 'identity',
      path: `/network-access/${route}`,
      labelZh: menuLabels[route]?.[0] ?? route,
      labelEn: menuLabels[route]?.[1] ?? route,
      section:
        route === 'devices'
          ? 'endpoint'
          : ['settings', 'user-admission'].includes(route)
            ? 'network'
            : route === 'mihomo-profiles'
              ? 'proxy'
              : 'vpn',
      sortOrder:
        groupedRoutes[route]?.[1] ??
        (route === 'resources' ? 20 : route === 'enrollments' ? 60 : index),
    })),
  ]
}

async function setup(context: BrowserContext, page: Page) {
  const state = {
    permissions: ['workbench.security.view', ...permissions],
    menus: intranetMenus(),
    reads: [] as string[],
    snapshotError: false,
    telemetryError: false,
    dashboardStatus: 200,
    dashboardReads: [] as URL[],
    writes: [] as string[],
  }
  const lists = [
    'sites',
    'spaces',
    'resources',
    'gateways',
    'mihomo-profiles',
    'nas-bindings',
    'site-profile-bindings',
    'sessions',
    'policies',
  ]
  const evidence = await installMocks(context, page, (url, method) => {
    const path = url.pathname.replace('/api/v1', '')
    if (path === '/access/permission-snapshot')
      return {
        body: {
          data: {
            permissionKeys: state.permissions,
            visibleMenuIds: state.menus.map((menu) => menu.id),
            visibleMenus: state.menus,
          },
        },
      }
    if (method !== 'GET') {
      if (path.startsWith('/network-access/')) state.writes.push(`${method} ${path}`)
      return undefined
    }
    if (path.startsWith('/network-access/')) state.reads.push(path)
    if (path === '/network-access/devices')
      return {
        body: {
          data: [
            device,
            { ...device, id: 'endpoint-lab', name: '验收终端 Linux', status: 'offline' },
          ],
        },
      }
    if (path === '/network-access/enrollments')
      return {
        body: {
          data: [
            {
              id: 'registration-1',
              runtimeId: 'runtime-office',
              runtimeKind: 'gateway',
              deviceId: device.id,
              subjectId: device.ownerUserId,
              status: 'pending',
              expiresAt: time,
            },
            {
              id: 'registration-radius',
              runtimeId: 'radius-office',
              runtimeKind: 'nas',
              deviceId: 'nas-office',
              subjectId: device.ownerUserId,
              status: 'pending',
              expiresAt: time,
            },
          ],
        },
      }
    if (path === '/network-access/access-grants')
      return {
        body: {
          data: [
            {
              id: 'grant-1',
              subjectId: device.ownerUserId,
              deviceId: device.id,
              siteId: device.siteId,
              networkSpaceId: 'office',
              mode: 'internal_ztna',
              resourceIds: ['resource-engineering'],
              status: 'issued',
              expiresAt: time,
            },
          ],
        },
      }
    if (lists.some((list) => path === `/network-access/${list}`)) return { body: { data: [] } }
    if (path === '/network-access/policy/snapshot')
      return state.snapshotError
        ? {
            status: 404,
            body: {
              error: { code: 'not_found', message: '未找到策略快照', request_id: 'snapshot-read' },
            },
          }
        : {
            body: {
              data: {
                policyVersion: 3,
                policyCount: 0,
                protectedResourceCount: 0,
                contentHash: 'hash-fixture',
                publishedAt: time,
              },
            },
          }
    if (path === '/network-access/telemetry/summary')
      return state.telemetryError
        ? {
            status: 503,
            body: {
              error: { code: 'unavailable', message: '遥测暂不可用', request_id: 'telemetry-read' },
            },
          }
        : {
            body: {
              data: {
                from: time,
                to: time,
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
              },
            },
          }
    if (
      path === '/network-access/vpn/profiles' ||
      path === '/network-access/vpn/selection-policies'
    )
      return { body: { data: [profile] } }
    if (path === '/network-access/vpn/dashboard') {
      state.dashboardReads.push(url)
      if (state.dashboardStatus !== 200)
        return {
          status: state.dashboardStatus,
          body: {
            error: { code: 'unavailable', message: '看板暂不可用', request_id: 'dashboard-read' },
          },
        }
      return {
        body: {
          data: {
            from: url.searchParams.get('from'),
            to: url.searchParams.get('to'),
            asOf: time,
            activeSessions: 0,
            availableGateways: 0,
            totalGateways: 0,
            attempts: 0,
            successes: 0,
            fallbacks: 0,
            telemetryAvailable: false,
            gateways: [],
            sessions: [],
            decisions: [],
            series: [
              {
                at: time,
                attempts: 0,
                successes: 0,
                fallbacks: 0,
                uploadBytes: 0,
                downloadBytes: 0,
              },
            ],
          },
        },
      }
    }
  })
  return { state, evidence }
}

for (const view of [
  { width: 1440, dark: false },
  { width: 1166, dark: true },
  { width: 390, dark: false },
  { width: 390, dark: true },
]) {
  test(`intranet assets query and detail ${view.width} ${view.dark ? 'dark' : 'light'}`, async ({
    context,
    page,
  }) => {
    await page.setViewportSize({ width: view.width, height: 1000 })
    const { state, evidence } = await setup(context, page)
    await page.goto('/network-access/devices')
    if (view.dark) await page.getByRole('button', { name: '切换到深色模式' }).click()
    const query = page.locator('.soha-management-query-card')
    await expect(query).toBeVisible()
    await expect(page.getByRole('button', { name: device.name, exact: true })).toBeVisible()
    await query.getByRole('textbox').fill('MacBook')
    await query.getByRole('textbox').press('Enter')
    await expect(page.getByRole('button', { name: '验收终端 Linux', exact: true })).toHaveCount(0)
    await page.getByRole('button', { name: '刷新', exact: true }).click()
    await expect(query.getByRole('textbox')).toHaveValue('MacBook')
    await expect(page.getByRole('button', { name: '验收终端 Linux', exact: true })).toHaveCount(0)
    await query.getByRole('button', { name: '重置', exact: true }).click()
    await expect(page.getByRole('button', { name: '验收终端 Linux', exact: true })).toBeVisible()
    await verifyGeometry(page, query.getByRole('button', { name: '查询', exact: true }))
    await page.getByRole('button', { name: device.name, exact: true }).press('Enter')
    const drawer = page.getByRole('dialog')
    await expect(drawer).toContainText('合规')
    await expect(drawer).toContainText(device.id)
    await page.screenshot({
      path: test.info().outputPath('intranet-device-candidate.png'),
      animations: 'disabled',
    })
    await verifyGeometry(page, drawer.getByRole('button', { name: '关闭', exact: true }))
    await drawer.getByRole('tab', { name: '网卡信息', exact: true }).click()
    await expect(drawer).toContainText('2001:db8:1234:5678:abcd:1234:5678:9012')
    await drawer.getByRole('button', { name: '关闭', exact: true }).click()
    expect(state.writes).toEqual([])
    verifyBrowserEvidence(evidence)
  })

  test(`VPN monitor hierarchy ${view.width} ${view.dark ? 'dark' : 'light'}`, async ({
    context,
    page,
  }) => {
    await page.setViewportSize({ width: view.width, height: 1000 })
    const { state, evidence } = await setup(context, page)
    await page.goto('/network-access/vpn/dashboard')
    if (view.dark) await page.getByRole('button', { name: '切换到深色模式' }).click()
    await expect(page.getByText('遥测暂不可用；未知指标不会按零计算。')).toBeVisible()
    const query = page.locator('.soha-management-query-card')
    await verifyGeometry(page, query.getByRole('button', { name: '查询', exact: true }))
    await query.getByRole('button', { name: /更多筛选/ }).click()
    await expect(page.getByText('全部方案', { exact: true })).toBeVisible()
    await query.getByRole('textbox', { name: /^站点 ID/ }).fill('site-office')
    await query.getByRole('button', { name: '查询', exact: true }).click()
    await expect
      .poll(() => state.dashboardReads.at(-1)?.searchParams.get('siteId'))
      .toBe('site-office')
    await page.getByRole('button', { name: '刷新', exact: true }).click()
    await expect.poll(() => state.dashboardReads.length).toBeGreaterThanOrEqual(3)
    expect(state.dashboardReads.at(-1)?.searchParams.get('siteId')).toBe('site-office')
    await page.getByRole('tab', { name: '趋势', exact: true }).click()
    await expect(page.getByRole('columnheader', { name: '上传量（B）', exact: true })).toBeVisible()
    await expect(page.getByRole('columnheader', { name: 'uploadBytes', exact: true })).toHaveCount(
      0,
    )
    await query.getByRole('button', { name: '重置', exact: true }).click()
    await expect.poll(() => state.dashboardReads.at(-1)?.searchParams.has('siteId')).toBe(false)
    await page.screenshot({
      path: test.info().outputPath('intranet-monitor-candidate.png'),
      fullPage: true,
      animations: 'disabled',
    })
    expect(state.writes).toEqual([])
    verifyBrowserEvidence(evidence)
  })
}

for (const route of [
  'sites',
  'spaces',
  'resources',
  'gateways',
  'user-admission',
  'site-profile-bindings',
  'sessions',
  'enrollments',
  'access-grants',
  'mihomo-profiles',
  'vpn/profiles',
  'vpn/selection-policies',
]) {
  test(`intranet list has a separate query region: ${route}`, async ({ context, page }) => {
    const { evidence } = await setup(context, page)
    await page.goto(`/network-access/${route}`)
    await expect(page.locator('.soha-management-query-card')).toBeVisible()
    await expect(page.getByRole('button', { name: '刷新', exact: true })).toBeVisible()
    if (route === 'enrollments') await expect(page.locator('main')).toContainText('等待中')
    if (route === 'access-grants') await expect(page.locator('main')).toContainText('已签发')
    verifyBrowserEvidence(evidence)
  })
}

test('snapshot errors use the snapshot owner and remain retryable', async ({ context, page }) => {
  const { state, evidence } = await setup(context, page)
  state.snapshotError = true
  await page.goto('/network-access/policy')
  await expect(page.getByText('策略快照读取失败', { exact: true })).toBeVisible()
  await expect(page.getByText('连接采样读取失败', { exact: true })).toHaveCount(0)
  await expect(page.getByText('尚未发布策略快照', { exact: true })).toHaveCount(0)
  state.snapshotError = false
  await page.getByRole('button', { name: '重试读取策略快照', exact: true }).click()
  await expect(page.getByText('策略快照读取失败', { exact: true })).toHaveCount(0)
  await expect(page.getByText('hash-fixture')).toBeVisible()
  verifyBrowserEvidence(evidence)
})

test('runtime category and state labels follow the language switch', async ({ context, page }) => {
  const { evidence } = await setup(context, page)
  await page.goto('/network-access/enrollments')
  await expect(page.locator('main')).toContainText('网关')
  await page.getByRole('button', { name: '切换到英文', exact: true }).click()
  await expect(page.locator('main')).toContainText('Gateway')
  await expect(page.locator('main')).toContainText('Pending')
  verifyBrowserEvidence(evidence)
})

test('VPN refresh retains the submitted range and cache, then hides denied observations', async ({
  context,
  page,
}) => {
  const { state, evidence } = await setup(context, page)
  await page.goto('/network-access/vpn/dashboard')
  await expect(page.locator('.soha-network-summary')).toContainText('0 / 0')
  const initialRange = state.dashboardReads.at(-1)!.search
  state.dashboardStatus = 503
  await page.getByRole('button', { name: '刷新', exact: true }).click()
  await expect(
    page.getByText('当前记录未更新，正在展示上次读取结果', { exact: true }),
  ).toBeVisible()
  await expect(page.locator('.soha-network-summary')).toContainText('0 / 0')
  expect(state.dashboardReads.at(-1)!.search).toBe(initialRange)
  state.dashboardStatus = 403
  await page.getByRole('button', { name: '重试读取当前记录', exact: true }).click()
  await expect(page.getByText('当前记录无访问权限', { exact: true })).toBeVisible()
  await expect(page.locator('.soha-network-summary')).not.toContainText('0 / 0')
  await expect(page.locator('.soha-network-summary')).not.toContainText('2026/10/18 09:00')
  verifyBrowserEvidence(evidence)
})

test('admission settings preserve their tab scopes and reveal the runtime guide on demand', async ({
  context,
  page,
}) => {
  const { evidence } = await setup(context, page)
  await page.goto('/network-access/settings')
  await expect(page.locator('.soha-management-query-card')).toBeVisible()
  await expect(page.getByRole('tab', { name: '网络设备', exact: true })).toHaveAttribute(
    'aria-selected',
    'true',
  )
  await page.getByRole('tab', { name: 'RADIUS 接入', exact: true }).click()
  await expect(page.getByRole('cell', { name: 'radius-office', exact: true })).toBeVisible()
  await expect(page.getByRole('cell', { name: 'runtime-office', exact: true })).toHaveCount(0)
  await expect(page.getByRole('columnheader', { name: '注册状态', exact: true })).toBeVisible()
  await expect(page.getByRole('columnheader', { name: '运行时类型', exact: true })).toHaveCount(0)
  const guide = page.getByRole('button', { name: '接入说明', exact: true })
  await guide.click()
  await expect(page.getByText(/创建一次性接入令牌/)).toBeVisible()
  await page.getByRole('tab', { name: 'SSID', exact: true }).click()
  await expect(page).toHaveURL(/tab=ssids/)
  await expect(page.locator('.soha-management-query-card')).toBeVisible()
  await page.getByRole('tab', { name: '网络设备', exact: true }).click()
  await expect(page).toHaveURL(/tab=nas-bindings/)
  await expect(page.locator('.soha-management-query-card')).toBeVisible()
  verifyBrowserEvidence(evidence)
})

test('telemetry first-load errors are owned inline and recover without a global duplicate', async ({
  context,
  page,
}) => {
  const { state, evidence } = await setup(context, page)
  state.telemetryError = true
  await page.goto('/network-access/telemetry')
  await expect(page.getByText('遥测读取失败', { exact: true })).toBeVisible()
  await expect(page.locator('.ant-notification-notice')).toHaveCount(0)
  state.telemetryError = false
  await page.getByRole('button', { name: '重试读取遥测', exact: true }).click()
  await expect(page.getByText('遥测读取失败', { exact: true })).toHaveCount(0)
  await expect(page.locator('.ant-notification-notice')).toHaveCount(0)
  verifyBrowserEvidence(evidence)
})

for (const [width, theme] of [
  [1440, 'light'],
  [1166, 'dark'],
] as const) {
  test(`six intranet scene entries preserve deep-link selection and keyboard navigation ${width} ${theme}`, async ({
    context,
    page,
  }) => {
    await page.setViewportSize({ width, height: 1000 })
    const { evidence } = await setup(context, page)
    await page.goto('/network-access/access-grants')
    if (theme === 'dark')
      await page.getByRole('button', { name: '切换到深色模式', exact: true }).click()
    const vpnGroup = page
      .locator('.ant-menu-item-group')
      .filter({ has: page.locator('.soha-nav-section-title', { hasText: '零信任网络与 VPN' }) })
    await expect(vpnGroup.locator(':scope > .ant-menu-item-group-list > li')).toHaveCount(6)
    await expect(vpnGroup.locator('.ant-menu-item-selected')).toHaveText('访问授权')
    await expect(
      vpnGroup.locator('.ant-menu-submenu-title').filter({ hasText: '访问控制' }),
    ).toHaveAttribute('aria-expanded', 'true')
    await expect(page.locator('.ant-breadcrumb')).toContainText('访问控制')
    await expect(page.locator('.ant-breadcrumb a').filter({ hasText: '访问控制' })).toHaveCount(0)
    const vpn = vpnGroup.locator('.ant-menu-submenu-title').filter({ hasText: 'VPN 配置' })
    await vpn.focus()
    await page.keyboard.press('Enter')
    await expect(vpn).toHaveAttribute('aria-expanded', 'true')
    await vpnGroup.getByRole('menuitem', { name: /VPN 连接方案$/ }).click()
    await expect(page).toHaveURL(/\/network-access\/vpn\/profiles$/)
    await expect(vpnGroup.locator('.ant-menu-item-selected')).toHaveText('VPN 连接方案')
    verifyBrowserEvidence(evidence)
  })
}
for (const tab of ['radius-services', 'ssids', 'nas-bindings']) {
  test(`legacy admission ${tab} preserves view and search/hash context`, async ({
    context,
    page,
  }) => {
    const { evidence } = await setup(context, page)
    await page.goto(`/network-access/${tab}?scope=retained#network-settings`)
    await expect(page).toHaveURL(
      new RegExp(`/network-access/settings\\?scope=retained&tab=${tab}#network-settings$`),
    )
    await expect(page.getByRole('tab', { selected: true })).toHaveText(
      tab === 'radius-services' ? 'RADIUS 接入' : tab === 'ssids' ? 'SSID' : '网络设备',
    )
    verifyBrowserEvidence(evidence)
  })
}
test('legacy admission links preserve trailing slashes and an explicit tab', async ({
  context,
  page,
}) => {
  const { evidence } = await setup(context, page)
  await page.goto('/network-access/radius-services/?scope=retained#network-settings')
  await expect(page).toHaveURL(
    '/network-access/settings?scope=retained&tab=radius-services#network-settings',
  )
  await expect(page.getByRole('tab', { selected: true })).toHaveText('RADIUS 接入')
  await page.goto('/network-access/ssids/?tab=radius-services')
  await expect(page).toHaveURL('/network-access/settings?tab=radius-services')
  await expect(page.getByRole('tab', { selected: true })).toHaveText('RADIUS 接入')
  verifyBrowserEvidence(evidence)
})

test('intranet containers prune denied children and retain custom navigation', async ({
  context,
  page,
}) => {
  const { state, evidence } = await setup(context, page)
  state.permissions = ['workbench.security.view', 'network_access.spaces.view']
  state.menus.push(
    {
      id: 'operator-folder',
      parentId: 'identity',
      path: '/operator/network',
      labelZh: '运营入口',
      section: 'vpn',
    },
    {
      id: 'operator-space',
      parentId: 'operator-folder',
      path: '/network-access/spaces',
      labelZh: '运营网络空间',
    },
  )
  await page.goto('/network-access/spaces')
  await expect(page.getByRole('menuitem', { name: /站点与网络/ })).toBeVisible()
  await expect(page.getByRole('menuitem', { name: /运营入口/ })).toBeVisible()
  await expect(page.locator('aside').getByText('访问控制', { exact: true })).toHaveCount(0)
  await expect(page.locator('aside').getByText('VPN 配置', { exact: true })).toHaveCount(0)
  await expect(page.locator('aside').getByText('网关', { exact: true })).toHaveCount(0)
  expect(state.reads).not.toContain('/network-access/gateways')
  expect(state.reads).not.toContain('/network-access/access-grants')
  verifyBrowserEvidence(evidence)
})
test('RADIUS-only readers keep the authorized tab without NAS queries', async ({
  context,
  page,
}) => {
  const { state, evidence } = await setup(context, page)
  state.permissions = ['workbench.security.view', 'network_access.enrollments.view']
  await page.goto('/network-access/settings')
  await expect(page.getByRole('tab')).toHaveCount(1)
  await expect(page.getByRole('tab', { name: 'RADIUS 接入', exact: true })).toHaveAttribute(
    'aria-selected',
    'true',
  )
  await page.getByRole('button', { name: '接入说明', exact: true }).click()
  await expect(page.getByText(/这里展示注册令牌状态，不代表服务健康/)).toBeVisible()
  await expect(page.getByRole('button', { name: '创建接入令牌', exact: true })).toHaveCount(0)
  expect(state.reads).not.toContain('/network-access/nas-bindings')
  verifyBrowserEvidence(evidence)
})

for (const dark of [false, true]) {
  test(`RADIUS enrollment remains readable at 390 ${dark ? 'dark' : 'light'}`, async ({
    context,
    page,
  }) => {
    await page.setViewportSize({ width: 390, height: 1000 })
    const { evidence } = await setup(context, page)
    await page.goto('/network-access/settings?tab=radius-services')
    if (dark) await page.getByRole('button', { name: '切换到深色模式', exact: true }).click()
    const guide = page.getByRole('button', { name: '接入说明', exact: true })
    await guide.focus()
    await page.keyboard.press('Enter')
    await expect(page.getByText(/这里展示注册令牌状态，不代表服务健康/)).toBeVisible()
    await page.screenshot({ animations: 'disabled' })
    await verifyGeometry(page, guide)
    verifyBrowserEvidence(evidence)
  })
}
