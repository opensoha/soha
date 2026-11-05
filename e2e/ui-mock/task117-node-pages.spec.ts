import { test, expect, type Page, type BrowserContext } from '@playwright/test'
import { baselineResponse } from '../fixtures/baseline-data'
import { installMocks, verifyBrowserEvidence } from '../fixtures/mock-app'
async function expectNodeFrame(page: Page) {
  await expect(page.locator('.soha-workload-detail-header')).toBeVisible()
  await expect(page.locator('.soha-workload-detail-namespace')).toHaveCount(0)
  for (const width of [1440, 900, 320]) {
    await page.setViewportSize({ width, height: 1000 })
    await expect
      .poll(() =>
        page.evaluate(() => {
          const h = document.querySelector('.soha-workload-detail-header')!.getBoundingClientRect()
          const tabs = document
            .querySelector('.soha-node-detail > .ant-tabs > .ant-tabs-nav')!
            .getBoundingClientRect()
          const body = document
            .querySelector(
              '.soha-node-detail > .ant-tabs > :is(.ant-tabs-content-holder,.ant-tabs-body-holder)',
            )!
            .getBoundingClientRect()
          return (
            Math.abs(h.bottom - tabs.top) <= 1 &&
            Math.abs(h.left - body.left) <= 1 &&
            Math.abs(h.right - body.right) <= 1 &&
            document.documentElement.scrollWidth <= innerWidth
          )
        }),
      )
      .toBe(true)
  }
  await page.setViewportSize({ width: 1440, height: 1000 })
}
const root = '/api/v1/clusters/cluster-a'
const collection = '/cluster-resources/nodes'
const detail = collection + '/worker-000?clusterId=cluster-a'
async function setup(
  context: BrowserContext,
  page: Page,
  { readonly = false, noMetrics = false, count = 80 } = {},
) {
  const now = new Date().toISOString()
  const state = {
    reads: [] as string[],
    writes: [] as string[],
    failNodes: false,
    failMetrics: false,
    connQuality: 'valid',
    rows: Array.from({ length: count }, (_, i) => ({
      name: `worker-${String(i).padStart(3, '0')}`,
      status: i === 1 ? 'NotReady' : 'Ready',
      unschedulable: i === 0,
      roles: ['worker'],
      labels: { pool: i === 0 ? 'gpu' : 'general' },
      internalIp: `10.0.0.${i + 1}`,
      version: 'v1.34.1',
      podCount: i === 0 ? 1 : 0,
      ageSeconds: 600,
      taints:
        i === 0
          ? [
              { key: 'dedicated', value: 'gpu', effect: 'NoSchedule' },
              { key: 'batch', effect: 'NoExecute' },
            ]
          : [],
      conditions: [
        { type: 'Ready', status: 'True', reason: 'KubeletReady' },
        { type: 'MemoryPressure', status: 'False', reason: 'KubeletHasSufficientMemory' },
      ],
      resources: {
        requests: { cpu: '500m', memory: '1Gi' },
        allocatable: { cpu: '8', memory: '16Gi', pods: '110' },
      },
      allowedActions: readonly ? ['view'] : ['view', 'update', 'drain', 'delete'],
    })),
  }
  const pod = {
    name: 'app-0',
    namespace: 'monitoring',
    nodeName: 'worker-000',
    phase: 'Running',
    readyContainers: '1/1',
    restarts: 0,
    ageSeconds: 600,
    allowedActions: ['view'],
    containerStatuses: [{ name: 'app', kind: 'container', ready: true, state: 'running' }],
  }
  await context.addInitScript(() =>
    localStorage.setItem(
      'soha-scope',
      JSON.stringify({ state: { clusterId: 'cluster-a', namespace: 'monitoring' }, version: 0 }),
    ),
  )
  await page.routeWebSocket('**/resources/stream?*', (socket) =>
    socket.send(
      JSON.stringify({ type: 'status', cacheStatus: 'live', observedAt: new Date().toISOString() }),
    ),
  )
  const evidence = await installMocks(context, page, (url, method) => {
    const path = url.pathname
    if (method === 'GET') state.reads.push(path + url.search)
    else if (path.startsWith(root)) state.writes.push(`${method} ${path}`)
    if (path === '/api/v1/auth/stream-ticket')
      return { body: { data: { ticket: 'test-stream-ticket', expiresAt: '2099-01-01T00:00:00Z' } } }
    if (path === '/api/v1/access/permission-snapshot') {
      const body = baselineResponse(path)!
      if (!('permissionKeys' in body.data)) throw new Error('Missing fixture')
      body.data.permissionKeys!.push(
        'platform.nodes.view',
        'platform.pods.view',
        'platform.workloads.overview.view',
        ...(noMetrics ? [] : ['platform.resource-monitoring.overview.view']),
        ...(readonly
          ? []
          : ['platform.nodes.update', 'platform.nodes.drain', 'platform.nodes.delete']),
      )
      return { body }
    }
    if (path === '/api/v1/clusters/capabilities')
      return {
        body: {
          data: ['resource.yaml.view', 'resource.yaml.apply', 'pod.logs', 'pod.exec'].map(
            (key) => ({ key, direct: { status: 'available' }, agent: { status: 'available' } }),
          ),
        },
      }
    if (path === root + '/infrastructure/nodes')
      return state.failNodes
        ? { status: 503, body: { error: { code: 'unavailable', message: 'Node read failed' } } }
        : { body: { data: state.rows } }
    if (path === root + '/infrastructure/nodes/worker-000/detail')
      return {
        body: {
          data: {
            ...state.rows[0],
            labels: { pool: 'gpu' },
            annotations: { 'example.test/owner': 'platform' },
            pods: [pod],
          },
        },
      }
    if (path === root + '/infrastructure/nodes/worker-000/yaml')
      return {
        body: {
          data: {
            kind: 'Node',
            name: 'worker-000',
            content: 'apiVersion: v1\nkind: Node\nmetadata:\n  name: worker-000\n',
          },
        },
      }
    if (path === root + '/workloads/pods') return { body: { items: [pod] } }
    if (path === root + '/events')
      return {
        body: {
          data: [
            {
              name: 'node-ready',
              involvedKind: 'Node',
              involvedName: 'worker-000',
              type: 'Normal',
              reason: 'NodeReady',
              message: 'Node is ready',
              count: 1,
              ageSeconds: 60,
              lastTimestamp: now,
            },
            {
              name: 'pod-ready',
              involvedKind: 'Pod',
              involvedName: 'worker-000',
              type: 'Normal',
              reason: 'UnrelatedPodEvent',
              message: 'not node',
              count: 1,
              ageSeconds: 60,
              lastTimestamp: now,
            },
          ],
        },
      }
    if (path === root + '/resource-monitoring') {
      if (state.failMetrics)
        return {
          status: 503,
          body: { error: { code: 'unavailable', message: 'Monitoring read failed' } },
        }
      const values: Record<string, [number, string]> = {
        'host.cpu.used': [0, 'cores'],
        'host.memory.used': [1024 ** 3, 'bytes'],
        'host.memory.total': [16 * 1024 ** 3, 'bytes'],
        'conntrack.entries': [15, 'count'],
        'conntrack.limit': [1024, 'count'],
        'pods.bound': [1, 'count'],
      }
      return {
        body: {
          data: {
            clusterId: 'cluster-a',
            view: 'overview',
            from: now,
            to: now,
            generatedAt: now,
            stepSeconds: 60,
            snapshotQuality: 'valid',
            unscheduledPendingPods: 0,
            nodes: [
              {
                nodeRef: { clusterId: 'cluster-a', name: 'worker-000' },
                quality: 'valid',
                ready: 'True',
                cordoned: true,
                memoryPressure: 'False',
                diskPressure: 'False',
                pidPressure: 'False',
                capacity: { cpuCores: 8, memoryBytes: 16 * 1024 ** 3, podSlots: 110 },
                allocatable: { cpuCores: 8, memoryBytes: 16 * 1024 ** 3, podSlots: 110 },
                requests: { cpuCores: 0.5, memoryBytes: 1024 ** 3, podSlots: 1 },
                limits: { cpuCores: null, memoryBytes: null, podSlots: null },
                cpuLimitsComplete: false,
                memoryLimitsComplete: false,
                boundPods: 1,
                pendingPods: 0,
                remainingPodSlots: 109,
                overflowPodSlots: 0,
              },
            ],
            panels: Object.entries(values).map(([key, [value, unit]]) => ({
              key,
              label: key,
              unit,
              quality: 'valid',
              series: [
                {
                  nodeRef: { clusterId: 'cluster-a', name: 'worker-000' },
                  metricKey: key,
                  quality: key === 'conntrack.limit' ? state.connQuality : 'valid',
                  unit,
                  source: 'prometheus',
                  sampledAt: now,
                  latest: value,
                  points: [
                    { timestamp: new Date(Date.now() - 60000).toISOString(), value },
                    { timestamp: now, value },
                  ],
                },
              ],
            })),
          },
        },
      }
    }
    return undefined
  })
  return { state, evidence }
}
const rows = (p: Page) => p.locator('.soha-cluster-nodes-page .ant-table-row')

test('Node continuous list filters actual labels and preserves anchor through refresh', async ({
  context,
  page,
}) => {
  const { state, evidence } = await setup(context, page)
  await page.setViewportSize({ width: 1373, height: 1502 })
  await page.goto(collection)
  await expect(page.getByRole('status').filter({ hasText: '80 个 Nodes' })).toBeVisible()
  await expect(page.locator('.ant-pagination')).toHaveCount(0)
  await expect(page.locator('.ant-table-tbody-virtual-holder')).toBeVisible()
  expect(await rows(page).count()).toBeLessThan(80)
  await expect(rows(page).first()).toContainText('0 mCPU')
  await expect(rows(page).nth(1)).toContainText('—')
  await expect(rows(page).nth(1).getByRole('progressbar')).toHaveCount(0)
  const search = page.getByPlaceholder('搜索名称 / IP / 标签 / 污点')
  await search.fill('POOL=GPU')
  await expect(rows(page)).toHaveCount(1)
  await expect(rows(page).first()).toContainText('禁止调度')
  await page.getByRole('button', { name: '重置', exact: true }).click()
  const holder = page.locator('.ant-table-tbody-virtual-holder')
  await holder.evaluate((el) => (el.scrollTop = 1000))
  await expect.poll(() => holder.evaluate((el) => el.scrollTop)).toBeGreaterThan(900)
  await page.getByRole('button', { name: '刷新节点', exact: true }).click()
  await expect.poll(() => holder.evaluate((el) => el.scrollTop)).toBeGreaterThan(900)
  expect(
    state.reads
      .filter((p) => p.includes('/resource-monitoring'))
      .every((p) => !p.includes('node=')),
  ).toBe(true)
  verifyBrowserEvidence(evidence)
})

test('Node row actions stay in a three-dot menu and mutation confirmations are cancellable', async ({
  context,
  page,
}) => {
  const { state, evidence } = await setup(context, page, { count: 2 })
  await page.goto(collection)
  await page.getByRole('button', { name: '更多操作：worker-000' }).click()
  await page.getByRole('menuitem', { name: '删除', exact: true }).click()
  await expect(page.getByRole('dialog')).toContainText('不会自动回收底层机器')
  await page.getByRole('button', { name: /^取\s*消$/ }).click()
  expect(state.writes).toEqual([])
  verifyBrowserEvidence(evidence)
})

test('Node detail shares the header and tabs, condition semantics and filtered event timeline', async ({
  context,
  page,
}) => {
  const { state, evidence } = await setup(context, page, { count: 2 })
  await page.setViewportSize({ width: 1373, height: 1502 })
  await page.goto(detail)
  await expect(page.locator('.soha-workload-detail-header')).toContainText('worker-000')
  await expect(page.getByRole('tab')).toHaveCount(6)
  await expect(page.getByRole('button', { name: '排空节点', exact: true })).toBeVisible()
  await expect(page.getByRole('button', { name: '删除', exact: true })).toBeVisible()
  expect(state.reads.some((p) => p.endsWith('/yaml'))).toBe(false)
  await expectNodeFrame(page)
  await page.getByRole('tab', { name: '运行条件', exact: true }).click()
  await expect(
    page.getByRole('row').filter({ hasText: 'MemoryPressure' }).locator('.ant-tag'),
  ).toHaveClass(/ant-tag-success/)
  await page.getByRole('tab', { name: '事件', exact: true }).click()
  await expect(page.locator('.ant-timeline')).toContainText('NodeReady')
  await expect(page.locator('.ant-timeline')).not.toContainText('UnrelatedPodEvent')
  await page.getByRole('tab', { name: 'Pods', exact: true }).click()
  await expect(page.locator('.soha-workload-pods-table')).toContainText('app-0')
  verifyBrowserEvidence(evidence)
})

test('Node metrics render six honest cards, zero host usage and no unknown-node fabrication', async ({
  context,
  page,
}) => {
  const { state, evidence } = await setup(context, page, { count: 2 })
  await page.goto(detail + '&tab=metrics')
  await expect(page.locator('.soha-node-metrics-grid > .ant-card')).toHaveCount(6)
  await expect(page.locator('.soha-node-metrics-grid')).toContainText('0 mCPU')
  await expect(page.locator('.soha-node-metrics-grid')).toContainText('CPU 分配')
  await page.getByText('1 小时', { exact: true }).click()
  await expect.poll(() => state.reads.some((p) => p.includes('rangeMinutes=60'))).toBe(true)
  state.connQuality = 'partial'
  await page.getByRole('button', { name: '刷新 Node', exact: true }).click()
  await expect(
    page
      .locator('.soha-node-metrics-grid > .ant-card')
      .filter({ has: page.getByText('连接跟踪', { exact: true }) })
      .locator('.soha-node-metric-summary'),
  ).toContainText('/ —')
  state.failMetrics = true
  await page.getByRole('button', { name: '刷新 Node', exact: true }).click()
  await expect(page.locator('.soha-node-metrics-grid')).toContainText('监控读取失败')
  await expect(page.locator('.soha-node-metric-summary').first()).toContainText('—')
  verifyBrowserEvidence(evidence)
})

test('Read-only nodes hide writes and monitoring permission absence sends no metric request', async ({
  context,
  page,
}) => {
  const { state, evidence } = await setup(context, page, {
    readonly: true,
    noMetrics: true,
    count: 2,
  })
  await page.goto(detail)
  await expect(page.getByRole('button', { name: '编辑节点', exact: true })).toHaveCount(0)
  await expect(page.getByRole('button', { name: '删除', exact: true })).toHaveCount(0)
  expect(state.reads.some((p) => p.includes('resource-monitoring'))).toBe(false)
  await page.getByRole('tab', { name: 'YAML', exact: true }).click()
  await expect(page.getByRole('textbox', { name: 'YAML 草稿编辑器', exact: true })).toBeVisible()
  await expect(page.getByRole('button', { name: /^应\s*用$|^Apply$/ })).toBeDisabled()
  const editor = page.getByRole('textbox', { name: 'YAML 草稿编辑器', exact: true })
  const lines = page.locator('.soha-k8s-yaml-editor .view-lines:visible').first()
  await editor.press('ControlOrMeta+Home')
  await editor.pressSequentially('# retained-node-draft')
  await editor.press('Enter')
  await expect(lines).toContainText('retained-node-draft')
  await page.getByRole('tab', { name: '概览', exact: true }).click()
  await page.getByRole('tab', { name: 'YAML', exact: true }).click()
  await expect(lines).toContainText('retained-node-draft')
  const before = state.reads.filter((p) => p.endsWith('/yaml')).length
  await page.getByRole('button', { name: '刷新 Node', exact: true }).click()
  await expect
    .poll(() => state.reads.filter((p) => p.endsWith('/yaml')).length)
    .toBeGreaterThan(before)
  await expect(lines).toContainText('retained-node-draft')
  expect(state.writes).toEqual([])
  verifyBrowserEvidence(evidence)
})

test('Node edit draft survives background refresh and drain remains explicit with safe defaults', async ({
  context,
  page,
}) => {
  const { state, evidence } = await setup(context, page, { count: 2 })
  await page.clock.install()
  await page.goto(detail)
  await page.getByRole('button', { name: '编辑节点', exact: true }).click()
  const dialog = page.getByRole('dialog')
  const field = dialog.getByLabel('Labels(JSON)')
  await field.fill('{"pool":"new-draft"}')
  const readsBefore = state.reads.filter((p) => p.endsWith('/worker-000/detail')).length
  await page.clock.fastForward(16000)
  await expect
    .poll(() => state.reads.filter((p) => p.endsWith('/worker-000/detail')).length)
    .toBeGreaterThan(readsBefore)
  await expect(field).toHaveValue('{"pool":"new-draft"}')
  await dialog.getByRole('button', { name: /^取\s*消$/ }).click()
  await page.getByRole('button', { name: '排空节点', exact: true }).click()
  await expect(page.getByRole('dialog')).toContainText('DaemonSet 和静态 Pod 不会被驱逐')
  await expect(page.locator('.soha-workload-detail-meta')).not.toContainText('排水中')
  await expect(page.getByRole('dialog').getByRole('checkbox').first()).not.toBeChecked()
  await page
    .getByRole('dialog')
    .getByRole('button', { name: /^取\s*消$/ })
    .click()
  expect(state.writes).toEqual([])
  verifyBrowserEvidence(evidence)
})

test('Node details remain aligned in dark and narrow views', async ({ context, page }) => {
  const { evidence } = await setup(context, page, { count: 2 })
  await page.goto(detail)
  await expectNodeFrame(page)
  await page.screenshot({
    path: '../preview/runtime-private/nd01-nodes/synthetic-overview.png',
    fullPage: true,
  })
  await page.evaluate(() => {
    const raw = JSON.parse(localStorage.getItem('soha-prefs') || '{"state":{}}')
    raw.state.themeMode = 'dark'
    localStorage.setItem('soha-prefs', JSON.stringify(raw))
  })
  await page.reload()
  await page.setViewportSize({ width: 760, height: 1000 })
  await expect(page.locator('h1')).toContainText('worker-000')
  expect(
    await page.evaluate(() => document.documentElement.scrollWidth > window.innerWidth + 1),
  ).toBe(false)
  verifyBrowserEvidence(evidence)
})
