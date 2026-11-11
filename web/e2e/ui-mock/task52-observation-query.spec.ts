import { expect, test, type BrowserContext, type Page } from '@playwright/test'
import { installMocks, verifyBrowserEvidence } from '../fixtures/mock-app'
import { verifyGeometry } from '../fixtures/geometry'

const from = '2026-10-16T01:00:00Z'
const to = '2026-10-16T02:00:00Z'
const windowQuery = `cluster=cluster-a&namespace=monitoring&service=checkout&from=${from}&to=${to}`
const providers = [
  {
    providerKey: 'loki',
    displayName: 'Loki QA',
    signals: ['logs'],
    capabilities: ['logs.query'],
    status: 'supported',
    runtimeStatus: 'healthy',
    configured: true,
    builtIn: true,
    protocolVersion: 'v1',
  },
]
const source = {
  id: 'logs-qa',
  name: '生产日志 QA',
  backendType: 'loki',
  enabled: true,
  config: { endpoint: 'https://logs.example.test' },
  scope: { clusterIds: [], namespaces: [] },
  queryBudget: { maxEntries: 200, maxRangeSeconds: 86400, timeoutSeconds: 10 },
  redactionPolicy: {},
  credentialKeys: ['bearer_token'],
  validationStatus: 'healthy',
}
const series = {
  key: 'cpu_usage',
  label: 'Checkout CPU',
  unit: 'percent',
  latest: 12.5,
  points: [
    { timestamp: from, value: 10 },
    { timestamp: to, value: 12.5 },
  ],
}
const dashboard = {
  id: 'dash-qa',
  name: 'Checkout RED QA',
  source: 'grafana',
  dataSourceId: 'prom-qa',
  tags: [],
  variables: [],
  panels: [
    {
      id: 'cpu',
      title: '服务 CPU',
      type: 'stat',
      queryable: true,
      layout: { x: 0, y: 0, w: 24, h: 6 },
      targets: [{ refId: 'A', expression: 'rate(qa[5m])' }],
    },
  ],
}
const service = {
  id: 'checkout',
  name: 'checkout',
  displayName: 'Checkout QA',
  status: 'healthy',
  namespace: 'monitoring',
  instances: [],
  endpoints: [],
}
const span = {
  traceId: 'trace-qa',
  spanId: 'span-qa',
  service: 'checkout',
  operation: 'GET /checkout',
  startTime: from,
  durationMs: 12.5,
  error: false,
  attributes: {},
}
const meta = { state: 'ok', warnings: [] }
const routes = [
  'overview',
  'providers',
  'log-data-sources',
  'logs',
  'services',
  'metrics',
  'traces',
  'dashboards',
]

async function mocks(context: BrowserContext, page: Page) {
  const state = {
    failedPath: '',
    status: 200,
    writeStatus: 200,
    validationStatus: 'healthy',
    reads: [] as { path: string; body: any }[],
    writes: [] as { path: string; body: any }[],
  }
  const failure = (status: number) => ({
    status,
    body: {
      error: { code: 'qa_failure', message: '诊断后端暂不可用', request_id: 'qa-observe-request' },
    },
  })
  const evidence = await installMocks(context, page, (url, method, request) => {
    const path = url.pathname.replace('/api/v1', '')
    if (path === '/access/permission-snapshot')
      return {
        body: {
          data: {
            permissionKeys: [
              'workbench.home.view',
              'workbench.monitoring.view',
              'observe.monitoring.view',
              'observe.alerts.view',
              'observe.events.view',
              'observe.log-data-sources.view',
              'observe.log-data-sources.create',
              'observe.log-data-sources.update',
              'observe.log-data-sources.validate',
              'observe.dashboards.create',
              'observe.dashboards.delete',
            ],
            visibleMenuIds: routes.map((p) => `monitoring-workbench-${p}`),
            visibleMenus: routes.map((p) => ({
              id: `monitoring-workbench-${p}`,
              path: `/monitoring-workbench/${p}`,
              labelZh: p,
            })),
          },
        },
      }
    if (path === '/announcements/inbox') return { body: { data: { items: [], unreadCount: 0 } } }
    if (path === '/auth/refresh') return undefined
    const read = method === 'GET' || path.endsWith('/query')
    if (read) {
      state.reads.push({ path, body: method === 'POST' ? request.postDataJSON() : undefined })
      if (path === state.failedPath && state.status !== 200) return failure(state.status)
    } else {
      state.writes.push({ path, body: request.postDataJSON() })
      if (state.writeStatus !== 200) return failure(state.writeStatus)
      if (path.endsWith('/validate'))
        return {
          body: {
            data: {
              ...source,
              validationStatus: state.validationStatus,
              validationMessage: '验证目标未就绪',
            },
          },
        }
      if (path === '/observability/dashboards/imports')
        return { body: { data: { dashboard, importedPanelCount: 1, warnings: [] } } }
      if (path === '/observability/data-sources' || path === '/observability/data-sources/logs-qa')
        return { body: { data: source } }
      if (method === 'DELETE') return { body: { data: {} } }
    }
    if (path === '/monitoring/summary')
      return {
        body: {
          data: {
            totalCount: 7,
            firingCount: 2,
            criticalCount: 1,
            resolvedCount: 5,
            warningCount: 1,
            infoCount: 0,
          },
        },
      }
    if (path === '/alert-events')
      return {
        body: {
          data: [
            {
              id: 'alert-qa',
              title: 'Checkout latency QA',
              severity: 'warning',
              status: 'firing',
              summary: 'P95 changed',
              startsAt: from,
            },
          ],
        },
      }
    if (path === '/events')
      return {
        body: {
          data: [
            {
              id: 'event-qa',
              summary: 'Checkout event QA',
              source: 'otel',
              category: 'application',
              occurredAt: from,
            },
          ],
        },
      }
    if (path === '/observability/providers') return { body: { data: providers } }
    if (path === '/observability/data-sources') return { body: { data: [source] } }
    if (path === '/observability/metrics/data-sources')
      return {
        body: {
          data: [
            { id: 'prom-qa', name: 'Prometheus QA', providerKey: 'prometheus', enabled: true },
          ],
        },
      }
    if (path === '/observability/metrics/catalog')
      return { body: { data: [{ key: 'cpu_usage', label: 'CPU', available: true }] } }
    if (path === '/observability/metrics/query' || path.endsWith('/panels/cpu/query'))
      return { body: { data: { meta, series: [series] } } }
    if (path === '/observability/traces/query')
      return { body: { data: { meta, services: [], spans: [span] } } }
    if (path === '/observability/services') return { body: { items: [service], meta } }
    if (path === '/observability/services/checkout') return { body: { data: service, meta } }
    if (path === '/observability/services/checkout/topology')
      return {
        body: {
          data: {
            nodes: [],
            edges: [
              {
                sourceServiceId: 'checkout',
                targetServiceId: 'inventory-qa',
                status: 'healthy',
                requestRate: 2,
                latencyP95Ms: 10,
              },
            ],
          },
          meta,
        },
      }
    if (path === '/observability/dashboards') return { body: { data: [dashboard] } }
    if (path === '/observability/dashboards/dash-qa') return { body: { data: dashboard } }
    if (path === '/clusters/cluster-a/logs/query')
      return {
        body: {
          data: {
            entries: [
              {
                timestamp: from,
                message: 'checkout log evidence QA',
                severity: 'info',
                source: {
                  domain: 'kubernetes',
                  podName: 'checkout-0',
                  namespace: 'monitoring',
                  containerName: 'app',
                },
                sourceMode: 'durable',
              },
            ],
            partial: false,
            truncated: false,
            scopeRestricted: false,
          },
        },
      }
  })
  return { state, evidence }
}

test('overview retains independent stale evidence and hides a refused region', async ({
  context,
  page,
}, info) => {
  const { state, evidence } = await mocks(context, page)
  await page.goto('/monitoring-workbench/overview')
  await expect(page.getByText('Checkout latency QA', { exact: true })).toBeVisible()
  state.failedPath = '/alert-events'
  state.status = 503
  await page.getByRole('button', { name: '刷新观测总览' }).click()
  await expect(page.getByText(/最近告警未更新/)).toBeVisible()
  await expect(page.getByText('Checkout latency QA', { exact: true })).toBeVisible()
  await expect(page.getByText('Checkout event QA', { exact: true })).toBeVisible()
  await page.screenshot({
    path: info.outputPath('observation-overview-stale.png'),
    animations: 'disabled',
  })
  state.status = 403
  await page.getByRole('button', { name: '重试读取' }).click()
  await expect(page.getByText('最近告警无访问权限')).toBeVisible()
  await expect(page.getByText('Checkout latency QA', { exact: true })).toBeHidden()
  verifyBrowserEvidence(evidence)
})

test('metric reads retain the exact window, retry a read failure and hide denied series', async ({
  context,
  page,
}, info) => {
  const { state, evidence } = await mocks(context, page)
  await page.goto(`/monitoring-workbench/metrics?metricKey=cpu_usage&${windowQuery}`)
  await expect(page.getByText('Checkout CPU', { exact: true })).toBeVisible()
  expect(state.reads.filter((r) => r.path === '/observability/metrics/query')).toHaveLength(1)
  state.failedPath = '/observability/metrics/query'
  state.status = 503
  await page.getByRole('button', { name: /查询指标/ }).click()
  await expect(page.getByText(/指标查询未更新/)).toBeVisible()
  await expect(page.getByText('Checkout CPU', { exact: true })).toBeVisible()
  const requests = state.reads.filter((r) => r.path === state.failedPath)
  expect(requests[1].body).toEqual(requests[0].body)
  expect(requests[1].body.timeFrom).toBe('2026-10-16T01:00:00.000Z')
  await page.screenshot({
    path: info.outputPath('observation-metric-stale.png'),
    animations: 'disabled',
  })
  state.status = 403
  await page.getByRole('button', { name: '重试读取' }).click()
  await expect(page.getByText('指标查询无访问权限')).toBeVisible()
  await expect(page.getByText('Checkout CPU', { exact: true })).toBeHidden()
  expect(state.writes).toEqual([])
  verifyBrowserEvidence(evidence)
})

test('same absolute log query refresh retains evidence and hides it after denial', async ({
  context,
  page,
}) => {
  const { state, evidence } = await mocks(context, page)
  await page.goto(`/monitoring-workbench/logs?source=kubernetes&${windowQuery}`)
  await page.getByRole('button', { name: '查询日志', exact: true }).click()
  await expect(page.getByTitle('checkout log evidence QA', { exact: true })).toBeVisible()
  state.failedPath = '/clusters/cluster-a/logs/query'
  state.status = 503
  await page.getByRole('button', { name: '查询日志', exact: true }).click()
  await expect(page.getByText(/日志查询未更新/)).toBeVisible()
  await expect(page.getByTitle('checkout log evidence QA', { exact: true })).toBeVisible()
  state.status = 403
  await page.getByRole('button', { name: '重试读取' }).click()
  await expect(page.getByText('日志查询无访问权限')).toBeVisible()
  await expect(page.getByTitle('checkout log evidence QA', { exact: true })).toBeHidden()
  expect(state.reads.filter((r) => r.path === state.failedPath)).toHaveLength(3)
  expect(state.reads.filter((r) => r.path === state.failedPath).map((r) => r.body.cursor)).toEqual([
    undefined,
    undefined,
    undefined,
  ])
  verifyBrowserEvidence(evidence)
})

test('trace evidence keeps its absolute window when opening associated logs', async ({
  context,
  page,
}) => {
  const { evidence } = await mocks(context, page)
  await page.goto(`/monitoring-workbench/traces?traceId=trace-qa&${windowQuery}`)
  await expect(page.getByText('GET /checkout', { exact: true })).toBeVisible()
  await page.getByRole('button', { name: '查看关联日志' }).click()
  await expect(page).toHaveURL(/monitoring-workbench\/logs/)
  const url = new URL(page.url())
  for (const [key, value] of Object.entries({
    from,
    to,
    cluster: 'cluster-a',
    namespace: 'monitoring',
    service: 'checkout',
    traceId: 'trace-qa',
    spanId: 'span-qa',
  }))
    expect(url.searchParams.get(key)).toBe(value)
  await page.goBack()
  await expect(page.getByText('GET /checkout', { exact: true })).toBeVisible()
  verifyBrowserEvidence(evidence)
})

test('dashboard failed reads pause playback until explicitly restarted', async ({
  context,
  page,
}) => {
  const { state, evidence } = await mocks(context, page)
  await page.goto(`/monitoring-workbench/dashboards/dash-qa?from=${from}&to=${to}`)
  await expect(page.getByText('12.50 percent')).toBeVisible()
  await page.getByRole('button', { name: '播放时间窗' }).click()
  state.failedPath = '/observability/dashboards/dash-qa'
  state.status = 503
  await page.getByRole('button', { name: '刷新仪表盘记录' }).click()
  await expect(page.getByText(/仪表盘未更新/)).toBeVisible()
  await expect(page.getByText('自动播放：已暂停')).toBeVisible()
  const stopped = page.url()
  state.status = 200
  await page.getByRole('button', { name: '重试读取' }).click()
  await expect(page.getByText(/仪表盘未更新/)).toBeHidden()
  await page.clock.install()
  await page.clock.fastForward(6000)
  expect(page.url()).toBe(stopped)
  await expect(page.getByText('自动播放：已暂停')).toBeVisible()
  verifyBrowserEvidence(evidence)
})

test('panel and service detail retain only authorized same-scope evidence', async ({
  context,
  page,
}) => {
  const { state, evidence } = await mocks(context, page)
  await page.goto(`/monitoring-workbench/dashboards/dash-qa?from=${from}&to=${to}`)
  await expect(page.getByText('12.50 percent')).toBeVisible()
  await page.getByRole('button', { name: '播放时间窗' }).click()
  state.failedPath = '/observability/dashboards/dash-qa/panels/cpu/query'
  state.status = 503
  await page.getByRole('button', { name: '刷新 服务 CPU 指标' }).click()
  await expect(page.getByText(/面板查询未更新/)).toBeVisible()
  await expect(page.getByText('自动播放：已暂停')).toBeVisible()
  await expect(page.getByText('12.50 percent')).toBeVisible()
  state.status = 403
  await page.getByRole('button', { name: '重试读取' }).click()
  await expect(page.getByText('12.50 percent')).toBeHidden()
  state.status = 200
  state.failedPath = ''
  await page.goto(`/monitoring-workbench/services?${windowQuery}`)
  await expect(page.getByText('Checkout QA', { exact: true })).toBeVisible()
  state.failedPath = '/observability/services'
  state.status = 503
  await page.getByRole('button', { name: '刷新列表', exact: true }).click()
  await expect(page.getByText(/上次读取结果/)).toBeVisible()
  await expect(page.getByText('Checkout QA', { exact: true })).toBeVisible()
  state.status = 403
  await page.getByRole('button', { name: '重试读取' }).click()
  await expect(page.getByText('Checkout QA', { exact: true })).toBeHidden()
  verifyBrowserEvidence(evidence)
})

test('service drawer keeps independently readable dependencies and hides refused detail', async ({
  context,
  page,
}) => {
  const { state, evidence } = await mocks(context, page)
  await page.goto(`/monitoring-workbench/services?${windowQuery}`)
  await page.getByRole('button', { name: '查看服务详情' }).click()
  const drawer = page.getByRole('dialog')
  await expect(drawer.getByText('inventory-qa', { exact: true })).toBeVisible()
  state.failedPath = '/observability/services/checkout/topology'
  state.status = 503
  await drawer.getByRole('button', { name: '刷新服务详情' }).click()
  await expect(drawer.getByText(/服务依赖未更新/)).toBeVisible()
  await expect(drawer.getByText('inventory-qa', { exact: true })).toBeVisible()
  state.status = 403
  await drawer.getByRole('button', { name: '重试读取' }).click()
  await expect(drawer.getByText('服务依赖无访问权限')).toBeVisible()
  await expect(drawer.getByText('inventory-qa', { exact: true })).toBeHidden()
  state.failedPath = '/observability/services/checkout'
  state.status = 503
  await drawer.getByRole('button', { name: '刷新服务详情' }).click()
  await expect(drawer.getByText(/服务详情未更新/)).toBeVisible()
  await expect(drawer.getByText('Checkout QA', { exact: true })).toBeVisible()
  state.status = 403
  await drawer.getByRole('button', { name: '重试读取' }).click()
  await expect(drawer.getByText('服务详情无访问权限')).toBeVisible()
  await expect(drawer.getByText('Checkout QA', { exact: true })).toBeHidden()
  verifyBrowserEvidence(evidence)
})

test('refused data source save clears private draft while preserving the write receipt', async ({
  context,
  page,
}) => {
  const { state, evidence } = await mocks(context, page)
  await page.goto('/monitoring-workbench/log-data-sources')
  await page.getByRole('button', { name: '编辑日志数据源' }).click()
  await page.getByRole('button', { name: '下一步' }).click()
  await page.getByRole('button', { name: '下一步' }).click()
  await page.getByLabel('Bearer Token').fill('qa-private-draft-canary')
  state.writeStatus = 403
  await page
    .getByRole('dialog')
    .getByRole('button', { name: /保\s*存/, exact: true })
    .click()
  await expect(page.getByRole('dialog')).toBeHidden()
  await expect(page.getByText(/更新日志数据源：操作请求失败/)).toBeVisible()
  await page.getByRole('button', { name: '编辑日志数据源' }).click()
  await page.getByRole('button', { name: '下一步' }).click()
  await page.getByRole('button', { name: '下一步' }).click()
  await expect(page.getByLabel('Bearer Token')).toHaveValue('')
  expect(await page.locator('body').textContent()).not.toContain('qa-private-draft-canary')
  verifyBrowserEvidence(evidence)
})

test('unknown data source update preserves its reviewed form and prevents replay', async ({
  context,
  page,
}) => {
  const { state, evidence } = await mocks(context, page)
  await page.goto('/monitoring-workbench/log-data-sources')
  await page.getByRole('button', { name: '编辑日志数据源' }).click()
  await page.getByLabel('名称', { exact: true }).fill('生产日志 draft QA')
  await page.getByRole('button', { name: '下一步' }).click()
  await page.getByRole('button', { name: '下一步' }).click()
  state.writeStatus = 503
  await page.getByRole('button', { name: /保\s*存/, exact: true }).click()
  const dialog = page.getByRole('dialog')
  await expect(dialog.getByText(/请求结果不明/)).toBeVisible()
  await page.keyboard.press('Escape')
  await expect(dialog).toBeVisible()
  await expect(dialog.getByRole('button', { name: /保\s*存/, exact: true })).toBeDisabled()
  expect(state.writes.filter((r) => r.path.includes('/data-sources/'))).toHaveLength(1)
  expect(state.writes[0].body.name).toBe('生产日志 draft QA')
  verifyBrowserEvidence(evidence)
})

test('validation returned unhealthy is an observed failure and can be retried', async ({
  context,
  page,
}) => {
  const { state, evidence } = await mocks(context, page)
  await page.goto('/monitoring-workbench/log-data-sources')
  state.validationStatus = 'failed'
  await page.getByRole('button', { name: '验证日志数据源' }).click()
  await expect(page.getByText('验证目标未就绪')).toBeVisible()
  state.validationStatus = 'healthy'
  await page.getByRole('button', { name: '验证日志数据源' }).click()
  await expect(page.getByText('连接验证记录已返回')).toBeVisible()
  expect(state.writes.filter((r) => r.path.endsWith('/validate'))).toHaveLength(2)
  verifyBrowserEvidence(evidence)
})

test('unknown dashboard import locks its inputs and close without retrying', async ({
  context,
  page,
}, info) => {
  const { state, evidence } = await mocks(context, page)
  await page.goto('/monitoring-workbench/dashboards')
  await page.getByRole('button', { name: '导入仪表盘', exact: true }).click()
  state.writeStatus = 503
  await page.getByRole('dialog').getByRole('button', { name: '添加仪表盘', exact: true }).click()
  const dialog = page.getByRole('dialog')
  await expect(dialog.getByText(/请求结果不明/)).toBeVisible()
  await page.keyboard.press('Escape')
  await expect(dialog).toBeVisible()
  await expect(dialog.getByRole('button', { name: '添加仪表盘', exact: true })).toBeDisabled()
  expect(state.writes.filter((r) => r.path.endsWith('/imports'))).toHaveLength(1)
  await page.screenshot({
    path: info.outputPath('observation-import-feedback.png'),
    animations: 'disabled',
  })
  verifyBrowserEvidence(evidence)
})

test('successful dashboard import carries the receipt to the saved record', async ({
  context,
  page,
}) => {
  const { state, evidence } = await mocks(context, page)
  await page.goto('/monitoring-workbench/dashboards')
  await page.getByRole('button', { name: '导入仪表盘', exact: true }).click()
  await page.getByRole('dialog').getByRole('button', { name: '添加仪表盘', exact: true }).click()
  await expect(page).toHaveURL(/dashboards\/dash-qa/)
  await expect(page.getByText('已导入 1 个面板')).toBeVisible()
  expect(state.writes.filter((r) => r.path.endsWith('/imports'))).toHaveLength(1)
  verifyBrowserEvidence(evidence)
})

test('dark narrow observation controls support keyboard and reduced motion', async ({
  context,
  page,
}, info) => {
  const { evidence } = await mocks(context, page)
  await page.emulateMedia({ reducedMotion: 'reduce' })
  await page.goto(`/monitoring-workbench/metrics?metricKey=cpu_usage&${windowQuery}`)
  await expect(page.getByText('Checkout CPU', { exact: true })).toBeVisible()
  await page.getByRole('button', { name: '切换到深色模式' }).click()
  await expect(page.getByRole('button', { name: '切换到浅色模式' })).toBeVisible()
  await page.setViewportSize({ width: 390, height: 844 })
  await page.getByRole('button', { name: /查询指标/ }).focus()
  await page.keyboard.press('Enter')
  await expect(page.getByText('Checkout CPU', { exact: true })).toBeVisible()
  await verifyGeometry(page, page.getByRole('button', { name: /查询指标/ }))
  await expect
    .poll(async () => {
      const canvas = await page.locator('.soha-signal-metric-chart canvas').boundingBox()
      const host = await page.locator('.soha-signal-metric-chart').boundingBox()
      return Math.abs((canvas?.width ?? 0) - (host?.width ?? 0))
    })
    .toBeLessThanOrEqual(1)
  const card = await page.locator('.soha-signal-metric-card').boundingBox()
  const chart = await page.locator('.soha-signal-metric-chart canvas').boundingBox()
  expect(card).not.toBeNull()
  expect(chart).not.toBeNull()
  expect(card!.x).toBeGreaterThanOrEqual(0)
  expect(card!.x + card!.width).toBeLessThanOrEqual(390)
  expect(chart!.x).toBeGreaterThanOrEqual(card!.x)
  expect(chart!.x + chart!.width).toBeLessThanOrEqual(card!.x + card!.width)
  expect(chart!.width).toBeLessThanOrEqual(card!.width)
  const chartHost = await page.locator('.soha-signal-metric-chart').boundingBox()
  expect(chart!.width).toBeGreaterThanOrEqual(chartHost!.width - 1)

  await page.screenshot({
    path: info.outputPath('observation-narrow-dark.png'),
    animations: 'disabled',
  })
  verifyBrowserEvidence(evidence)
})
