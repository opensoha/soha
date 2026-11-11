import { test, expect, type BrowserContext, type Page } from '@playwright/test'
import { baselineResponse } from '../fixtures/baseline-data'
import { installMocks, verifyBrowserEvidence } from '../fixtures/mock-app'
import type { NetworkTopologyView } from '../../src/features/platform/network/topology/types'

const topology: NetworkTopologyView = {
  clusterId: 'cluster-a',
  namespace: 'monitoring',
  graphVersion: 'v1',
  source: 'kubernetes-snapshot',
  generatedAt: '2026-11-02T00:00:00Z',
  summary: {
    entryCount: 1,
    routeCount: 0,
    serviceCount: 1,
    backendPodCount: 1,
    pendingRouteCount: 0,
    missingServiceCount: 0,
  },
  traces: [],
  nodes: [
    {
      id: 'entry',
      type: 'entry',
      label: 'shop.example.test',
      provider: 'kubernetes',
      protocol: 'HTTP',
      resolutionState: 'resolved',
    },
    {
      id: 'service',
      type: 'service',
      label: 'shop-service',
      provider: 'kubernetes',
      resolutionState: 'resolved',
      resource: {
        clusterId: 'cluster-a',
        apiVersion: 'v1',
        kind: 'Service',
        name: 'shop-service',
        namespace: 'monitoring',
        scopeMode: 'namespace',
      },
    },
    {
      id: 'pod',
      type: 'pod',
      label: 'shop-worker-01',
      provider: 'kubernetes',
      resolutionState: 'resolved',
    },
    {
      id: 'unknown',
      type: 'external',
      label: 'backend-reference',
      provider: 'gateway',
      resolutionState: 'not_visible',
    },
  ],
  edges: [
    {
      id: 'service-edge',
      source: 'entry',
      target: 'service',
      relation: 'weights',
      protocol: 'HTTP',
      port: '80',
      weight: 0,
      totalWeight: 10,
      resolutionState: 'resolved',
    },
    {
      id: 'pod-edge',
      source: 'service',
      target: 'pod',
      relation: 'resolves',
      resolutionState: 'resolved',
    },
  ],
  coverage: [
    {
      sourceId: 'kubernetes.services',
      provider: 'kubernetes',
      state: 'available',
      observedAt: '2026-11-02T00:00:00Z',
    },
    {
      sourceId: 'gateway.httproutes',
      provider: 'gateway',
      state: 'denied',
      reasonCode: 'soha_permission',
      checkedAt: '2026-11-02T00:00:00Z',
    },
  ],
}
async function setup(
  context: BrowserContext,
  page: Page,
  options: { dark?: boolean; allowed?: boolean } = {},
) {
  const state = {
    data: structuredClone(topology),
    fail: false,
    denied: false,
    reads: 0,
    writes: [] as string[],
  }
  await context.addInitScript(({ dark }) => {
    localStorage.setItem(
      'soha-scope',
      JSON.stringify({ state: { clusterId: 'cluster-a', namespace: 'monitoring' }, version: 0 }),
    )
    localStorage.setItem(
      'soha-prefs',
      JSON.stringify({
        state: {
          themeMode: dark ? 'dark' : 'light',
          localeCode: 'zh_CN',
          sidebarCollapsed: true,
          currentWorkspace: 'resource',
          companionBubbleEnabled: false,
        },
        version: 0,
      }),
    )
  }, options)
  const evidence = await installMocks(context, page, (url, method) => {
    if (method !== 'GET' && url.pathname.startsWith('/api/v1/clusters/'))
      state.writes.push(url.pathname)
    if (url.pathname === '/api/v1/access/permission-snapshot') {
      const body = baselineResponse(url.pathname)!
      if (!body.data || !('permissionKeys' in body.data))
        throw new Error('Missing permission fixture')
      body.data.permissionKeys = [
        'workbench.platform.view',
        'workspace.resource.view',
        'platform.clusters.view',
        'platform.namespaces.view',
        ...(options.allowed === false ? [] : ['platform.network.services.view']),
        ...(options.allowed === false ? [] : ['platform.network.topology.view']),
      ]
      body.data.visibleMenuIds = ['network', 'network-topology', 'network-services', 'clusters']
      return { body }
    }
    if (url.pathname.endsWith('/network/topology')) {
      state.reads++
      if (state.fail || state.denied)
        return {
          status: state.denied ? 403 : 503,
          body: {
            error: {
              code: state.denied ? 'forbidden' : 'unavailable',
              message: 'Topology read failed',
            },
          },
        }
      return { body: { data: state.data } }
    }
    return undefined
  })
  return { state, evidence }
}
const root = (page: Page) => page.locator('.soha-topology-page')
const refresh = (page: Page) => page.getByRole('button', { name: '刷新网络拓扑', exact: true })

test('topology distinguishes source failure, true empty, filtered empty and hidden Pods', async ({
  context,
  page,
}) => {
  const { state, evidence } = await setup(context, page)
  state.data.nodes = []
  state.data.edges = []
  await page.goto('/network/topology')
  await expect(root(page)).toContainText('尚未读取到拓扑资源')
  await expect(root(page)).not.toContainText('当前范围没有拓扑资源')
  await page.getByRole('button', { name: '检查数据覆盖', exact: true }).click()
  await expect(page.getByRole('dialog')).toContainText('缺少 Soha 读取权限')
  await expect(page.getByRole('dialog')).toContainText('soha_permission')
  await expect(page.getByRole('dialog')).toContainText('检查')
  await page.keyboard.press('Escape')
  state.data.coverage = [topology.coverage![0]]
  await refresh(page).click()
  await expect(root(page)).toContainText('当前范围没有拓扑资源')
  state.data = structuredClone(topology)
  await refresh(page).click()
  await expect(page.locator('.soha-topology-node-card').first()).toBeVisible()
  await page.getByRole('textbox', { name: '搜索已加载节点' }).fill('does-not-exist')
  await expect(root(page)).toContainText('没有匹配的已加载资源')
  await page.getByRole('button', { name: '清除筛选', exact: true }).click()
  state.data.nodes = [topology.nodes![2]]
  state.data.edges = []
  await refresh(page).click()
  await expect(root(page)).toContainText('当前资源均为已收起的 Pod')
  await page.getByRole('button', { name: '展开 Pod', exact: true }).click()
  await expect(page.locator('.soha-topology-node-card')).toContainText('shop-worker-01')
  expect(state.writes).toEqual([])
  verifyBrowserEvidence(evidence)
})

test('graph keeps viewport on refresh and round-trip view changes; table keyboard evidence restores focus', async ({
  context,
  page,
}, info) => {
  const { state, evidence } = await setup(context, page)
  await page.setViewportSize({ width: 1440, height: 1050 })
  await page.goto('/network/topology?view=graph')
  await expect(page.locator('.soha-topology-node-card').first()).toBeVisible()
  await page.getByRole('button', { name: 'Zoom In', exact: true }).click()
  const viewport = page.locator('.react-flow__viewport')
  const before = await viewport.getAttribute('style')
  const reads = state.reads
  await refresh(page).click()
  await expect.poll(() => state.reads).toBeGreaterThan(reads)
  await expect(viewport).toHaveAttribute('style', before!)
  await page.getByText('关系表', { exact: true }).click()
  await expect(root(page)).toContainText('权重 0/10')
  const target = root(page).getByRole('button', { name: 'shop-service', exact: true }).first()
  await target.focus()
  await page.keyboard.press('Enter')
  await expect(page.locator('.soha-topology-evidence')).toContainText('shop-service')
  await expect(
    page.locator('.soha-topology-evidence').getByRole('link', { name: '查看资源详情' }),
  ).toHaveAttribute(
    'href',
    '/network/services/shop-service?clusterId=cluster-a&namespace=monitoring',
  )
  await page.screenshot({ path: info.outputPath('topology-evidence-light.png'), fullPage: true })
  await root(page).getByRole('button', { name: '关闭', exact: true }).click()
  await expect(target).toBeFocused()
  await page.getByText('关系图', { exact: true }).click()
  await expect(viewport).toHaveAttribute('style', before!)
  const canvas = await page.locator('.soha-topology-canvas').boundingBox()
  expect(canvas!.y + canvas!.height).toBeLessThanOrEqual(1050 - 8)
  await page.screenshot({ path: info.outputPath('topology-graph-light.png'), fullPage: true })
  expect(state.writes).toEqual([])
  verifyBrowserEvidence(evidence)
})

test('failed background read keeps evidence and graph; denied read hides both', async ({
  context,
  page,
}) => {
  const { state, evidence } = await setup(context, page)
  await page.goto('/network/topology?view=table')
  await root(page).getByRole('button', { name: 'shop-service', exact: true }).first().click()
  state.fail = true
  await refresh(page).click()
  await expect(page.locator('.soha-topology-evidence')).toContainText('shop-service')
  await expect(page.locator('.soha-topology-read-status')).toHaveClass(/is-error/)
  state.denied = true
  await refresh(page).click()
  await expect(page.locator('.soha-topology-evidence')).toHaveCount(0)
  await expect(root(page)).not.toContainText('shop-service')
  await expect(root(page)).toContainText('当前范围的拓扑读取被拒绝')
  verifyBrowserEvidence(evidence)
})

test('coverage filtering and narrow dark inspector remain usable without page overflow', async ({
  context,
  page,
}, info) => {
  const { evidence } = await setup(context, page, { dark: true })
  await page.setViewportSize({ width: 640, height: 950 })
  await page.goto('/network/topology?view=table')
  await root(page)
    .getByRole('button', { name: /数据覆盖/ })
    .click()
  const drawer = page.getByRole('dialog')
  await expect(drawer).toContainText('项可完整读取')
  await drawer.getByRole('combobox', { name: '覆盖结果', exact: true }).click()
  await page.getByText('仅需关注', { exact: true }).last().click()
  await expect(drawer.getByText('services', { exact: true })).toHaveCount(0)
  await expect(drawer).toContainText('httproutes')
  await page.screenshot({ path: info.outputPath('topology-coverage-dark.png'), fullPage: true })
  await drawer.getByRole('button', { name: '关闭', exact: true }).click()
  await expect(drawer).toBeHidden()
  await root(page).getByRole('button', { name: 'shop-service', exact: true }).first().click()
  await expect(page.getByRole('dialog')).toContainText('关系证据')
  await page.keyboard.press('Escape')
  expect(
    await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth + 1),
  ).toBe(true)
  await page.screenshot({ path: info.outputPath('topology-table-narrow-dark.png'), fullPage: true })
  verifyBrowserEvidence(evidence)
})

test('missing page permission does not request topology', async ({ context, page }) => {
  const { state, evidence } = await setup(context, page, { allowed: false })
  await page.goto('/network/topology')
  await expect(page).not.toHaveURL(/network\/topology/)
  await expect(root(page)).toHaveCount(0)
  expect(state.reads).toBe(0)
  verifyBrowserEvidence(evidence)
})
