import { test, expect, type Page, type BrowserContext } from '@playwright/test'
import { baselineResponse } from '../fixtures/baseline-data'
import { installMocks, verifyBrowserEvidence } from '../fixtures/mock-app'
const root = '/api/v1/clusters/cluster-a/resource-monitoring'
const route = '/resource-monitoring?clusterId=cluster-a'
async function setup(
  context: BrowserContext,
  page: Page,
  allowed = ['overview', 'cpu', 'memory', 'pods', 'conntrack'],
) {
  const now = new Date().toISOString()
  const earlier = new Date(Date.now() - 60_000).toISOString()
  const state = {
    failed: false,
    denied: false,
    partial: false,
    missing: false,
    truncated: false,
    reads: [] as string[],
    writes: [] as string[],
  }
  await context.addInitScript(() => {
    localStorage.setItem(
      'soha-scope',
      JSON.stringify({ state: { clusterId: 'cluster-a', namespace: 'monitoring' }, version: 0 }),
    )
    localStorage.setItem('soha-resource-monitoring-auto', '1')
  })
  const nodes = Array.from({ length: 25 }, (_, i) => ({
    nodeRef: {
      clusterId: 'cluster-a',
      uid: `node-${i}`,
      name: `worker-${String(i).padStart(3, '0')}`,
      kind: 'Node',
      apiVersion: 'v1',
      scopeMode: 'cluster',
    },
    quality: 'valid',
    observedAt: now,
    ready: 'True',
    cordoned: false,
    memoryPressure: 'False',
    diskPressure: 'False',
    pidPressure: 'False',
    capacity: { cpuCores: 8, memoryBytes: 16 * 1024 ** 3, podSlots: 110 },
    allocatable: { cpuCores: 7, memoryBytes: 15 * 1024 ** 3, podSlots: 110 },
    requests: { cpuCores: 0.5, memoryBytes: 1024 ** 3, podSlots: 1 },
    limits: { cpuCores: null, memoryBytes: null, podSlots: null },
    cpuLimitsComplete: false,
    memoryLimitsComplete: false,
    boundPods: 1,
    pendingPods: 0,
    remainingPodSlots: 109,
    overflowPodSlots: 0,
  }))
  const evidence = await installMocks(context, page, (url, method) => {
    if (method !== 'GET' && url.pathname.startsWith('/api/v1/clusters/'))
      state.writes.push(url.pathname)
    if (url.pathname === '/api/v1/access/permission-snapshot') {
      const body = baselineResponse(url.pathname)!
      if (!body.data || !('permissionKeys' in body.data))
        throw new Error('Missing permission fixture')
      body.data.permissionKeys = body.data.permissionKeys!.filter(
        (key) => !key.startsWith('platform.resource-monitoring.'),
      )
      body.data.permissionKeys.push(
        'platform.nodes.view',
        ...allowed.map((key) => `platform.resource-monitoring.${key}.view`),
      )
      return { body }
    }
    if (url.pathname === root) {
      state.reads.push(url.search)
      if (state.failed || state.denied)
        return {
          status: state.denied ? 403 : 503,
          body: {
            error: {
              code: state.denied ? 'forbidden' : 'unavailable',
              message: 'Monitoring read failed',
            },
          },
        }
      const selected = nodes.filter(
        (node) =>
          !url.searchParams.get('node') || node.nodeRef.name === url.searchParams.get('node'),
      )
      const values: Record<string, [number, string]> = {
        'host.cpu.used': [0, 'cores'],
        'host.memory.used': [1024 ** 3, 'bytes'],
        'host.memory.total': [16 * 1024 ** 3, 'bytes'],
        'pod.cpu.used': [0.01, 'cores'],
        'pod.memory.used': [100 * 1024 ** 2, 'bytes'],
        'conntrack.entries': [15, 'count'],
        'conntrack.limit': [1024, 'count'],
        'conntrack.drop.rate': [0, 'events/s'],
        'conntrack.early_drop.rate': [0, 'events/s'],
        'conntrack.insert_failed.rate': [0, 'events/s'],
        'pods.bound': [1, 'count'],
      }
      return {
        body: {
          data: {
            clusterId: 'cluster-a',
            view: url.searchParams.get('view'),
            from: earlier,
            to: now,
            generatedAt: now,
            snapshotObservedAt: now,
            snapshotQuality: 'valid',
            stepSeconds: 60,
            nodes: selected,
            nodeOptions: nodes.map((node) => node.nodeRef),
            unscheduledPendingPods: 2,
            truncated: state.truncated,
            panels: Object.entries(values).map(([key, [base, unit]]) => ({
              key,
              label: key,
              unit,
              quality: state.partial ? 'partial' : 'valid',
              reasonCode: state.partial ? 'node_coverage_incomplete' : undefined,
              series: selected.map((node) => {
                const i = Number(node.nodeRef.uid.slice(5))
                const value =
                  state.missing && key === 'host.cpu.used' && i === 1
                    ? null
                    : key === 'host.cpu.used'
                      ? i / 10
                      : base
                return {
                  nodeRef: node.nodeRef,
                  metricKey: key,
                  quality: 'valid',
                  unit,
                  source: 'prometheus',
                  sampledAt: now,
                  latest: value,
                  points: [
                    { timestamp: earlier, value },
                    { timestamp: now, value },
                  ],
                }
              }),
            })),
          },
        },
      }
    }
    return undefined
  })
  return { state, evidence }
}
const tableRows = (page: Page) => page.getByRole('tabpanel').locator('.ant-table-row')
const active = (page: Page) => page.getByRole('tabpanel')

test('scope totals and all 25 nodes are independent from ten chart nodes; search, sort, focus and anchor work', async ({
  context,
  page,
}) => {
  const { state, evidence } = await setup(context, page)
  await page.goto(route)
  await expect(page.getByRole('status').filter({ hasText: '25 个 Nodes' })).toBeVisible()
  await expect(page.locator('.soha-monitoring-summary-card')).toHaveCount(4)
  await expect(page.locator('.soha-monitoring-summary-card').first()).toContainText('30.00 cores')
  await expect(page.locator('.ant-pagination')).toHaveCount(0)
  await expect(page.getByRole('columnheader', { name: '节点', exact: true })).toBeVisible()
  await page.getByRole('columnheader', { name: '节点', exact: true }).click()
  await expect(tableRows(page).first()).toContainText('worker-000')
  await expect(tableRows(page).first()).toContainText('0 mCPU')
  await expect
    .poll(() =>
      tableRows(page)
        .first()
        .evaluate((row) => {
          const action = row.querySelector('[data-soha-action-column]')!.getBoundingClientRect()
          const lastMetric = row
            .querySelectorAll('.soha-monitoring-usage')[3]!
            .getBoundingClientRect()
          return action.width <= 65 && lastMetric.right <= action.left + 1
        }),
    )
    .toBe(true)
  const holder = active(page).locator('.ant-table-tbody-virtual-holder')
  await holder.evaluate((element) => {
    element.scrollTop = element.scrollHeight
  })
  await expect(tableRows(page).last()).toContainText('worker-024')
  await expect.poll(() => tableRows(page).count()).toBeLessThan(25)
  const before = await holder.evaluate((element) => element.scrollTop)
  await page.getByRole('button', { name: '刷新资源监控', exact: true }).click()
  await expect(page.getByRole('switch', { name: /自动刷新正常/ })).toBeVisible()
  expect(Math.abs((await holder.evaluate((element) => element.scrollTop)) - before)).toBeLessThan(2)
  await page.getByRole('searchbox', { name: '搜索监控节点' }).fill('worker-024')
  await expect(page.getByRole('status').filter({ hasText: '匹配 1 / 共 25' })).toBeVisible()
  await page.getByRole('button', { name: '更多操作：worker-024' }).click()
  await page.getByRole('menuitem', { name: '聚焦 CPU趋势' }).click()
  await expect(page).toHaveURL(/#monitoring-cpu$/)
  await expect(page.getByRole('combobox', { name: '曲线节点' })).toBeVisible()
  await expect(page.getByRole('status').filter({ hasText: '25 个 Nodes' })).toBeVisible()
  await expect(active(page).locator('.soha-monitoring-compare-controls')).toContainText(
    'worker-024',
  )
  await expect(active(page).locator('.soha-monitoring-trends')).toBeVisible()
  await expect(page.getByRole('dialog')).toHaveCount(0)
  expect(state.writes).toEqual([])
  verifyBrowserEvidence(evidence)
})

test('legacy detail URL stays on a full tab, preserves scope/time, and exposes samples and coverage', async ({
  context,
  page,
}) => {
  const { state, evidence } = await setup(context, page)
  await page.goto('/resource-monitoring/cpu?clusterId=cluster-a&node=worker-024&rangeMinutes=360')
  await expect(page.getByRole('tab', { name: 'CPU', exact: true })).toHaveAttribute(
    'aria-selected',
    'true',
  )
  await expect(page.getByRole('status').filter({ hasText: '1 个 Nodes' })).toBeVisible()
  await expect(page.locator('.ant-picker-range')).toHaveCount(0)
  await page.getByRole('tab', { name: '内存', exact: true }).click()
  await expect(page).toHaveURL(
    /clusterId=cluster-a&node=worker-024&rangeMinutes=360#monitoring-memory$/,
  )
  await active(page).getByRole('button', { name: '采样数据', exact: true }).first().click()
  await expect(page.getByRole('dialog')).toContainText('worker-024')
  await page.keyboard.press('Escape')
  await expect(page.getByRole('dialog')).toBeHidden()
  await page.getByRole('button', { name: '来源与覆盖' }).click()
  await expect(page.getByRole('dialog')).toContainText('主机 CPU 为非 idle cores')
  expect(
    state.reads.every(
      (read) => read.includes('rangeMinutes=360') && read.includes('node=worker-024'),
    ),
  ).toBe(true)
  verifyBrowserEvidence(evidence)
})

test('custom time pauses polling and distinguishes historical CPU from current snapshot', async ({
  context,
  page,
}) => {
  const { state, evidence } = await setup(context, page)
  await page.goto(route)
  await page.getByRole('combobox', { name: '时间范围' }).click()
  await page.getByText('自定义时间', { exact: true }).click()
  await expect(page.locator('.ant-picker-range')).toBeVisible()
  await expect(page.getByRole('switch', { name: /历史窗口/ })).toBeDisabled()
  await expect(page.locator('.soha-monitoring-summary-card').first()).toContainText(
    '历史容量未提供',
  )
  await page.getByRole('tab', { name: 'CPU', exact: true }).click()
  await expect(active(page)).toContainText('历史百分比不计算')
  await expect(active(page)).toContainText('requests、limits 和 Pod 槽位为当前快照')
  await page.getByRole('combobox', { name: '时间范围' }).click()
  await page.getByText('最近 15 分钟', { exact: true }).click()
  await expect(page.locator('.ant-picker-range')).toHaveCount(0)
  await expect(page.getByRole('switch', { name: /自动刷新正常/ })).toBeEnabled()
  expect(state.reads.some((read) => read.includes('from=') && read.includes('to='))).toBe(true)
  verifyBrowserEvidence(evidence)
})

test('missing data, partial coverage, truncation and failed refresh remain truthful', async ({
  context,
  page,
}) => {
  const { state, evidence } = await setup(context, page)
  state.missing = true
  state.partial = true
  state.truncated = true
  await page.goto(route)
  await expect(page.locator('.soha-monitoring-summary-card').first()).toContainText('—')
  await expect(page.locator('.soha-monitoring-summary-card').first()).toContainText('部分数据')
  await expect(page.getByText('已加载 25 个节点 · 结果已截断', { exact: true })).toBeVisible()
  await page.getByRole('searchbox', { name: '搜索监控节点' }).fill('worker-001')
  await expect(tableRows(page).first().locator('.soha-monitoring-usage').first()).toContainText('—')
  await expect(
    tableRows(page).first().locator('.soha-monitoring-usage').first().getByRole('progressbar'),
  ).toHaveCount(0)
  state.failed = true
  await page.getByRole('button', { name: '刷新资源监控', exact: true }).click()
  await expect(page.getByRole('switch', { name: /刷新失败/ })).toBeVisible({ timeout: 15000 })
  await expect(tableRows(page)).toHaveCount(1)
  await expect(page.locator('.soha-monitoring-summary-card').first()).toContainText('上次结果')
  verifyBrowserEvidence(evidence)
})

test('permission-limited metric view never queries other tabs or opens an unauthorized hash', async ({
  context,
  page,
}) => {
  const { state, evidence } = await setup(context, page, ['cpu'])
  await page.goto(route + '#monitoring-memory')
  await expect(page.getByRole('tab', { name: 'CPU', exact: true })).toHaveAttribute(
    'aria-selected',
    'true',
  )
  await expect(page.getByRole('tab', { name: '内存', exact: true })).toHaveCount(0)
  await expect(page.getByRole('status').filter({ hasText: '25 个 Nodes' })).toBeVisible()
  expect(state.reads.length).toBeGreaterThan(0)
  expect(state.reads.every((read) => new URLSearchParams(read).get('view') === 'cpu')).toBe(true)
  state.denied = true
  await page.getByRole('button', { name: '刷新资源监控', exact: true }).click()
  await expect(active(page).locator('.soha-kubernetes-resource-list')).toHaveCount(0, {
    timeout: 15000,
  })
  verifyBrowserEvidence(evidence)
})

for (const theme of ['light', 'dark'])
  test(`${theme} layout stays inside viewport at desktop, tablet and mobile sizes`, async ({
    context,
    page,
  }) => {
    const { evidence } = await setup(context, page)
    await context.addInitScript(
      (mode) =>
        localStorage.setItem(
          'soha-prefs',
          JSON.stringify({ state: { themeMode: mode }, version: 0 }),
        ),
      theme,
    )
    await page.goto(route)
    await expect(page.locator('html')).toHaveAttribute('data-theme-mode', theme)
    for (const width of [1440, 900, 375]) {
      await page.setViewportSize({ width, height: 1000 })
      await expect(page.locator('.soha-monitoring-summary')).toBeVisible()
      await expect
        .poll(() => page.evaluate(() => document.documentElement.scrollWidth <= innerWidth))
        .toBe(true)
      const cards = page.locator('.soha-monitoring-summary-card')
      await expect
        .poll(async () =>
          Math.min(
            ...(await Promise.all(
              (await cards.all()).map(async (card) => (await card.boundingBox())?.width ?? 0),
            )),
          ),
        )
        .toBeGreaterThan(100)
    }
    await page.getByRole('tab', { name: 'CPU', exact: true }).click()
    await expect(active(page).locator('.soha-monitoring-trends')).toBeVisible()
    await expect
      .poll(() => page.evaluate(() => document.documentElement.scrollWidth <= innerWidth))
      .toBe(true)
    verifyBrowserEvidence(evidence)
  })
