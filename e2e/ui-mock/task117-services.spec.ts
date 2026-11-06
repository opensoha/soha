import { test, expect, type BrowserContext, type Page } from '@playwright/test'
import { parse } from 'yaml'
import type { KubernetesResourceGraph } from '@opensoha/contracts/gen/ts/sohaapi'
import { baselineResponse } from '../fixtures/baseline-data'
import { installMocks, verifyBrowserEvidence } from '../fixtures/mock-app'

const root = '/api/v1/clusters/cluster-a'
const detailRoute = '/network/services/api?clusterId=cluster-a&namespace=monitoring'
const active = (page: Page) => page.getByRole('tabpanel').filter({ visible: true })
async function setup(
  context: BrowserContext,
  page: Page,
  options: { readonly?: boolean; noSelector?: boolean; type?: string } = {},
) {
  const now = '2026-11-02T01:00:00Z'
  const service = {
    name: 'api',
    namespace: 'monitoring',
    type: options.type || 'NodePort',
    clusterIp: options.type === 'ExternalName' ? '' : '10.43.0.20',
    ageSeconds: 3600,
    allowedActions: options.readonly ? [] : ['update', 'delete'],
    selector: options.noSelector
      ? {}
      : { app: 'api', 'app.kubernetes.io/component': 'backend-api-v1' },
    labels: { app: 'api' },
    annotations: { note: 'retained' },
    ports: ['80/TCP', '53/UDP'],
    portMappings: [
      { name: 'http', protocol: 'TCP', port: 80, targetPort: 'web-http', nodePort: 30080 },
      { name: 'dns', protocol: 'UDP', port: 53, targetPort: '53' },
    ],
    endpoints: [
      {
        address: '10.42.0.8',
        ready: true,
        serving: true,
        terminating: false,
        targetRef: 'Pod/api-worker',
        nodeName: 'worker-01',
        zone: 'zone-a',
      },
      { address: '2001:db8::1' },
    ],
    backendPods: [
      {
        name: 'api-worker',
        namespace: 'monitoring',
        phase: 'Running',
        readyContainers: '1/1',
        restarts: 0,
        nodeName: 'worker-01',
        podIp: '10.42.0.8',
        ageSeconds: 3500,
      },
    ],
  }
  const yaml = `apiVersion: v1
kind: Service
metadata:
  name: api
  namespace: monitoring
  resourceVersion: '7'
  labels: {app: api}
spec:
  type: ${service.type}
  clusterIP: 10.43.0.20
  externalName: database.internal.example.test
  internalTrafficPolicy: Local
  publishNotReadyAddresses: false
  selector: {app: api}
  ports: [{name: http, port: 80, targetPort: web-http, protocol: TCP, nodePort: 30080}]
`
  const graph: KubernetesResourceGraph = {
    clusterId: 'cluster-a',
    rootId: 'service',
    generatedAt: now,
    warnings: [],
    evidence: [],
    nodes: [
      {
        id: 'service',
        resource: {
          clusterId: 'cluster-a',
          kind: 'Service',
          apiVersion: 'v1',
          name: 'api',
          namespace: 'monitoring',
          scopeMode: 'namespace',
        },
      },
      {
        id: 'pod',
        resource: {
          clusterId: 'cluster-a',
          kind: 'Pod',
          apiVersion: 'v1',
          name: 'api-worker',
          namespace: 'monitoring',
          scopeMode: 'namespace',
        },
      },
    ],
    edges: [{ id: 'edge', sourceId: 'service', targetId: 'pod', relation: 'selects' }],
  }
  const state = {
    reads: [] as string[],
    writes: [] as string[],
    fail: 0,
    graphFail: 0,
    graph,
    yaml,
    service,
  }
  await context.addInitScript(() =>
    localStorage.setItem(
      'soha-scope',
      JSON.stringify({ state: { clusterId: 'cluster-a', namespace: null }, version: 0 }),
    ),
  )
  const evidence = await installMocks(context, page, (url, method) => {
    if (url.pathname.startsWith(root)) {
      if (method === 'GET') state.reads.push(url.pathname + url.search)
      else if (!url.pathname.endsWith('/resource-creation/scope-decision'))
        state.writes.push(method + ' ' + url.pathname)
    }
    if (url.pathname === '/api/v1/access/permission-snapshot') {
      const body = baselineResponse(url.pathname)!
      if (!('permissionKeys' in body.data)) throw new Error('Missing permissions')
      body.data.permissionKeys = body.data.permissionKeys!.filter(
        (p) => !p.startsWith('platform.network.services.') && p !== 'platform.events.view',
      )
      body.data.permissionKeys.push(
        'platform.network.services.view',
        'platform.network.endpoint-slices.view',
        'platform.pods.view',
        'platform.pods.exec',
        'platform.nodes.view',
        ...(options.readonly
          ? []
          : [
              'platform.network.services.update',
              'platform.network.services.delete',
              'platform.network.services.create',
            ]),
        'platform.events.view',
      )
      return { body }
    }
    if (url.pathname.endsWith('/resource-creation/scope-decision'))
      return {
        body: {
          data: {
            allowed: true,
            scopeMode: 'namespace',
            allowedNamespaces: ['monitoring', 'default'],
          },
        },
      }
    if (url.pathname === root + '/network/services' && method === 'GET')
      return {
        body: {
          data: Array.from({ length: 35 }, (_, i) => ({
            ...service,
            name: i === 0 ? 'api' : `service-${String(i).padStart(2, '0')}`,
            namespace: i < 20 ? 'monitoring' : 'default',
          })).filter(
            (s) =>
              !url.searchParams.get('namespace') ||
              s.namespace === url.searchParams.get('namespace'),
          ),
        },
      }
    if (url.pathname === root + '/network/services/api/detail')
      return state.fail
        ? {
            status: state.fail,
            body: { error: { code: 'read_failed', message: 'Service read failed' } },
          }
        : { body: { data: service } }
    if (url.pathname === root + '/network/services/api/yaml' && method === 'GET')
      return {
        body: {
          data: { kind: 'Service', name: 'api', namespace: 'monitoring', content: state.yaml },
        },
      }
    if (url.pathname === root + '/network/services/api/yaml' && method === 'PUT')
      return {
        status: 409,
        body: { error: { code: 'conflict', message: 'Service resourceVersion changed' } },
      }
    if (url.pathname === root + '/network/services/api' && method === 'DELETE')
      return { body: { data: {} } }
    if (url.pathname === root + '/events')
      return {
        body: {
          data: [
            {
              name: 'event-1',
              namespace: 'monitoring',
              involvedKind: 'Service',
              involvedName: 'api',
              type: 'Warning',
              reason: 'SyncLoadBalancerFailed',
              message: 'Could not provision address',
              count: 2,
              ageSeconds: 20,
              lastTimestamp: now,
            },
            {
              name: 'event-2',
              namespace: 'monitoring',
              involvedKind: 'Service',
              involvedName: 'api',
              type: 'Normal',
              reason: 'Updated',
              message: 'No timestamp record',
              count: 1,
              ageSeconds: 1,
            },
          ],
        },
      }
    if (url.pathname === root + '/network/services/api/metrics')
      return {
        body: {
          data: {
            resourceKind: 'Service',
            resourceName: 'api',
            namespace: 'monitoring',
            configured: true,
            source: 'prometheus',
            rangeMinutes: 60,
            stepSeconds: 30,
            generatedAt: now,
            series: [],
          },
        },
      }
    if (url.pathname === root + '/workloads/pods')
      return { body: { data: [{ name: 'api-worker', namespace: 'monitoring', phase: 'Running' }] } }
    if (url.pathname === root + '/resources/graph')
      return state.graphFail
        ? {
            status: state.graphFail,
            body: { error: { code: 'graph_failed', message: 'Graph read failed' } },
          }
        : {
            body: {
              data: {
                ...state.graph,
              },
            },
          }
    if (url.pathname === root + '/resources/update-plan')
      return {
        body: {
          data: {
            ready: true,
            riskLevel: 'medium',
            capability: 'kubernetes-resource-update',
            requiresApproval: false,
            target: 'cluster-a/monitoring/Service/api',
            summary: 'Service update',
            changes: [],
            warnings: [],
            blockers: [],
          },
        },
      }
    return undefined
  })
  return { state, evidence }
}

test('continuous list, full port mappings, namespace switch and fixed action geometry', async ({
  context,
  page,
}) => {
  const { state, evidence } = await setup(context, page)
  await page.goto('/network/services?clusterId=cluster-a')
  await expect(page.getByRole('status').filter({ hasText: '35 个 Service' })).toBeVisible()
  await expect(page.locator('.ant-pagination')).toHaveCount(0)
  await expect(
    page.locator('.ant-table-row').first().locator('.soha-service-port-row'),
  ).toHaveCount(2)
  const firstMapping = page
    .locator('.ant-table-row')
    .first()
    .locator('.soha-service-port-row')
    .first()
  await expect(firstMapping.locator('[data-port-field]')).toHaveText([
    'http',
    'TCP',
    '80',
    'web-http',
    'NodePort: 30080',
  ])
  await expect(firstMapping.locator('.soha-service-port-tag')).toHaveCount(5)
  await expect(firstMapping.locator('[data-port-field=protocol]').locator('..')).toHaveClass(
    /ant-tag-processing/,
  )
  for (const field of ['name', 'port', 'targetPort', 'nodePort']) {
    await expect(firstMapping.locator(`[data-port-field=${field}]`).locator('..')).not.toHaveClass(
      /ant-tag-processing|ant-tag-cyan|ant-tag-purple/,
    )
  }
  await expect(page.locator('.ant-table-row').first()).toContainText('web-http')
  await expect(page.getByRole('columnheader', { name: '选择器', exact: true })).toBeVisible()
  await page.getByRole('columnheader', { name: '名称' }).click()
  const holder = page.locator('.ant-table-tbody-virtual-holder')
  await holder.evaluate((el) => {
    el.scrollTop = el.scrollHeight
  })
  await expect(page.locator('.ant-table-row').last()).toContainText('service-34')
  const before = await holder.evaluate((el) => el.scrollTop)
  await page.getByRole('button', { name: '刷新 Services', exact: true }).click()
  await expect(page.getByRole('switch', { name: /自动刷新正常/ })).toBeVisible()
  expect(Math.abs((await holder.evaluate((el) => el.scrollTop)) - before)).toBeLessThan(3)
  await page.getByPlaceholder('搜索 Service / namespace / type / port / selector').fill('web-http')
  await expect(page.getByRole('status').filter({ hasText: '35 个 Service' })).toBeVisible()
  await page
    .getByPlaceholder('搜索 Service / namespace / type / port / selector')
    .fill('app.kubernetes.io/component=backend-api-v1')
  await expect(page.getByRole('status').filter({ hasText: '35 个 Service' })).toBeVisible()
  await page
    .getByPlaceholder('搜索 Service / namespace / type / port / selector')
    .fill('service-34')
  await expect(page.locator('.ant-table-row')).toHaveCount(1)
  await expect(page.locator('.ant-table-row')).toContainText('service-34')
  await page.getByRole('button', { name: '切换到命名空间 default' }).click()
  await expect(page).toHaveURL(/namespace=default/)
  await expect(page.getByRole('columnheader', { name: '命名空间' })).toBeVisible()
  await expect
    .poll(() =>
      page
        .locator('.ant-table-row')
        .first()
        .locator('.soha-table-actions-column')
        .evaluate((el) => el.getBoundingClientRect().width),
    )
    .toBeLessThanOrEqual(65)
  expect(state.reads.every((r) => !r.includes('/detail'))).toBe(true)
  expect(state.writes).toEqual([])
  verifyBrowserEvidence(evidence)
})

test('five tabs, tagged port configuration and backend links keep scope', async ({
  context,
  page,
}) => {
  const { state, evidence } = await setup(context, page)
  await page.goto(detailRoute)
  await expect(page.getByRole('heading', { name: 'api', exact: true })).toBeVisible()
  await expect(page.getByRole('tab')).toHaveText(['概览', '后端', '诊断', '关联资源', 'YAML'])
  await expect(active(page)).toContainText('Local')
  await expect(active(page)).toContainText('web-http')
  await expect(active(page).locator('.soha-service-port-row')).toHaveCount(2)
  await expect(
    active(page).locator('.soha-service-port-row').first().locator('[data-port-field=port]'),
  ).toHaveText('80')
  await expect(
    active(page).locator('.soha-service-port-row').first().locator('[data-port-field=protocol]'),
  ).toHaveText('TCP')
  await expect(active(page).locator('.soha-service-port-row').first()).toContainText(
    'NodePort: 30080',
  )
  expect(state.reads.some((r) => r.includes('/metrics') || r.includes('/events'))).toBe(false)
  await page.getByRole('tab', { name: '后端', exact: true }).click()
  await expect(
    active(page).getByRole('link', { name: 'EndpointSlices', exact: true }),
  ).toHaveAttribute('href', /clusterId=cluster-a&namespace=monitoring/)
  await expect(active(page)).toContainText('Serving')
  await expect(active(page)).toContainText('Terminating')
  await expect(active(page).getByRole('link', { name: 'api-worker' }).first()).toHaveAttribute(
    'href',
    /clusterId=cluster-a&namespace=monitoring/,
  )
  await expect(active(page).getByRole('link', { name: 'worker-01' }).first()).toHaveAttribute(
    'href',
    /clusterId=cluster-a/,
  )
  expect(state.writes).toEqual([])
  verifyBrowserEvidence(evidence)
})

for (const legacyTab of ['events', 'metrics']) {
  test(`removed ${legacyTab} deep link returns to overview without hidden reads`, async ({
    context,
    page,
  }) => {
    const { state, evidence } = await setup(context, page)
    await page.goto(detailRoute + '&tab=' + legacyTab)
    await expect(page).toHaveURL(/clusterId=cluster-a&namespace=monitoring&tab=overview$/)
    await expect(page.getByRole('tab', { name: '概览', exact: true })).toHaveAttribute(
      'aria-selected',
      'true',
    )
    await expect(page.getByRole('tab', { name: '事件', exact: true })).toHaveCount(0)
    await expect(page.getByRole('tab', { name: '指标', exact: true })).toHaveCount(0)
    expect(state.reads.some((r) => r.includes('/events') || r.includes('/metrics'))).toBe(false)
    verifyBrowserEvidence(evidence)
  })
}

test('no selector stays explicit and diagnostics never run automatically or pretend UDP is TCP', async ({
  context,
  page,
}) => {
  const { state, evidence } = await setup(context, page, { noSelector: true, type: 'ExternalName' })
  await page.goto(detailRoute)
  await expect(active(page)).toContainText('database.internal.example.test')
  await expect(active(page)).toContainText('无选择器')
  expect(state.reads.some((r) => r.includes('/metrics'))).toBe(false)
  await page.getByRole('tab', { name: '诊断', exact: true }).click()
  await expect(active(page)).toContainText('不检测 UDP/SCTP')
  await page.getByRole('combobox', { name: 'Service 端口' }).click()
  await expect(page.locator('.ant-select-dropdown').filter({ visible: true })).not.toContainText(
    'dns',
  )
  expect(state.writes).toEqual([])
  verifyBrowserEvidence(evidence)
})

test('read-only actions and YAML permission are respected', async ({ context, page }) => {
  const { state, evidence } = await setup(context, page, { readonly: true })
  await page.goto(detailRoute)
  await expect(
    page
      .locator('.soha-k8s-detail-actions')
      .getByRole('button', { name: '编辑 Service', exact: true }),
  ).toHaveCount(0)
  await expect(
    page
      .locator('.soha-k8s-detail-actions')
      .getByRole('button', { name: '删除 Service', exact: true }),
  ).toHaveCount(0)
  expect(state.reads.some((r) => r.includes('/events'))).toBe(false)
  await page.getByRole('tab', { name: 'YAML', exact: true }).click()
  await expect(active(page).getByRole('button', { name: /应\s*用|Apply/ })).toBeDisabled()
  expect(state.writes).toEqual([])
  verifyBrowserEvidence(evidence)
})

test('refresh retains cached values on 503, removes protected content on 403; delete cancel never writes', async ({
  context,
  page,
}) => {
  const { state, evidence } = await setup(context, page)
  await page.goto(detailRoute)
  await page
    .locator('.soha-k8s-detail-actions')
    .getByRole('button', { name: '删除 Service', exact: true })
    .click()
  await expect(page.getByRole('dialog')).toContainText('monitoring / api')
  await page
    .getByRole('dialog')
    .getByRole('button', { name: /取\s*消/ })
    .click()
  expect(state.writes).toEqual([])
  state.fail = 503
  await page.getByRole('button', { name: '刷新 Service', exact: true }).click()
  await expect(page.getByRole('switch', { name: /刷新失败/ })).toBeVisible()
  await expect(active(page)).toContainText('10.43.0.20')
  state.fail = 403
  await page.getByRole('button', { name: '刷新 Service', exact: true }).click()
  await expect(page.getByRole('heading', { name: 'api', exact: true })).toHaveCount(0)
  expect(state.writes).toEqual([])
  verifyBrowserEvidence(evidence)
})

test('related graph and list share data; header/card align at desktop, dark and narrow widths', async ({
  context,
  page,
}) => {
  const { evidence } = await setup(context, page)
  await page.goto(detailRoute + '&tab=relationships')
  await expect(
    active(page).getByRole('button', { name: /Pod monitoring.*api-worker/ }),
  ).toBeVisible()
  await active(page).getByRole('button', { name: 'Zoom Out', exact: true }).click()
  const viewport = active(page).locator('.react-flow__viewport')
  let transform: string | null = null
  await expect
    .poll(
      async () => {
        const current = await viewport.getAttribute('style')
        const stable = current === transform
        transform = current
        return stable
      },
      { intervals: [250] },
    )
    .toBe(true)
  await active(page).getByText('列表', { exact: true }).click()
  await expect(active(page).getByRole('link', { name: 'api-worker', exact: true })).toBeVisible()
  await active(page).getByText('关系图', { exact: true }).click()
  await expect(viewport).toHaveAttribute('style', transform || '')
  await expect(
    active(page).getByRole('button', { name: /Pod monitoring.*api-worker/ }),
  ).toBeVisible()
  for (const width of [900, 375]) {
    await page.setViewportSize({ width, height: 900 })
    await expect
      .poll(() => page.evaluate(() => document.documentElement.scrollWidth <= innerWidth + 1))
      .toBe(true)
    await expect(active(page).locator('.soha-service-relationships')).toBeVisible()
  }
  await page.getByRole('button', { name: '切换到深色模式' }).click()
  await expect(
    active(page).locator('.soha-resource-graph-kind').filter({ hasText: 'Service' }),
  ).toBeVisible()
  await page.getByRole('button', { name: '切换到浅色模式' }).click()
  await page.setViewportSize({ width: 1440, height: 1000 })
  await page.getByRole('tab', { name: '概览', exact: true }).click()
  await expect
    .poll(async () => {
      const header = await page.locator('.soha-k8s-detail-header').boundingBox()
      const content = await page.locator('.soha-workload-overview').boundingBox()
      return header && content
        ? Math.max(
            Math.abs(header.x - content.x),
            Math.abs(header.x + header.width - content.x - content.width),
          )
        : 100
    })
    .toBeLessThanOrEqual(1)
  await expect
    .poll(async () => {
      const header = await page.locator('.soha-k8s-detail-header').boundingBox()
      const nav = await page.locator('.soha-service-detail .ant-tabs-nav').first().boundingBox()
      return header && nav ? Math.abs(nav.y - header.y - header.height) : 100
    })
    .toBeLessThanOrEqual(1)
  for (const width of [1440, 900, 375]) {
    await page.setViewportSize({ width, height: 900 })
    await expect
      .poll(() =>
        page.locator('.soha-workload-overview').evaluate((el) => el.getBoundingClientRect().width),
      )
      .toBeGreaterThan(100)
    await expect
      .poll(() => page.evaluate(() => document.documentElement.scrollWidth <= innerWidth + 1))
      .toBe(true)
  }
  await page.getByRole('button', { name: '切换到深色模式' }).click()
  await expect(active(page)).toContainText('web-http')
  verifyBrowserEvidence(evidence)
})

test('YAML draft survives tab changes, refresh and a conflict response', async ({
  context,
  page,
}) => {
  const { state, evidence } = await setup(context, page)
  await page.goto(detailRoute + '&tab=yaml')
  const editor = page.getByRole('textbox', { name: 'YAML 草稿编辑器', exact: true })
  await expect(page.locator('.soha-k8s-yaml-editor .view-lines:visible').first()).toContainText(
    'apiVersion',
  )
  await editor.press('ControlOrMeta+Home')
  await editor.pressSequentially('# task117-service-draft')
  await editor.press('Enter')
  await page.getByRole('tab', { name: '后端', exact: true }).click()
  await page.getByRole('tab', { name: 'YAML', exact: true }).click()
  await expect(page.locator('.soha-k8s-yaml-editor .view-lines:visible').first()).toContainText(
    'task117-service-draft',
  )
  const before = state.reads.filter((r) => r.includes('/yaml')).length
  await page.getByRole('button', { name: '刷新 Service', exact: true }).click()
  await expect
    .poll(() => state.reads.filter((r) => r.includes('/yaml')).length)
    .toBeGreaterThan(before)
  await expect(page.locator('.soha-k8s-yaml-editor .view-lines:visible').first()).toContainText(
    'task117-service-draft',
  )
  expect(state.writes).toEqual([])
  await active(page)
    .getByRole('button', { name: /应\s*用|Apply/ })
    .click()
  await expect(active(page)).toContainText('Service resourceVersion changed')
  await expect(page.locator('.soha-k8s-yaml-editor .view-lines:visible').first()).toContainText(
    'task117-service-draft',
  )
  expect(state.writes).toEqual(['PUT ' + root + '/network/services/api/yaml'])
  verifyBrowserEvidence(evidence)
})

test('detail deletion confirms the exact Service target and navigates after the response', async ({
  context,
  page,
}) => {
  const { state, evidence } = await setup(context, page)
  await page.goto(detailRoute)
  await page
    .locator('.soha-k8s-detail-actions')
    .getByRole('button', { name: '删除 Service', exact: true })
    .click()
  const dialog = page.getByRole('dialog')
  await expect(dialog).toContainText('monitoring / api')
  await dialog.getByRole('button', { name: '删除', exact: true }).click()
  await expect(page).toHaveURL(/network\/services\?clusterId=cluster-a&namespace=monitoring$/)
  expect(state.writes).toEqual(['DELETE ' + root + '/network/services/api'])
  verifyBrowserEvidence(evidence)
})

test('missing backend arrays remain unknown rather than a false zero', async ({
  context,
  page,
}) => {
  const { state, evidence } = await setup(context, page)
  Reflect.deleteProperty(state.service, 'endpoints')
  Reflect.deleteProperty(state.service, 'backendPods')
  await page.goto(detailRoute + '&tab=backends')
  await expect(active(page).getByText('后端数据未确认', { exact: true })).toHaveCount(2)
  await expect(active(page)).not.toContainText('Endpoints · 0')
  verifyBrowserEvidence(evidence)
})

test('selector tags open backends with the selected cluster and namespace', async ({
  context,
  page,
}) => {
  const { evidence } = await setup(context, page)
  await page.goto('/network/services?clusterId=cluster-a')
  const row = page
    .locator('.ant-table-row')
    .filter({ has: page.getByRole('button', { name: 'api', exact: true }) })
  await expect(row.locator('.soha-service-selector-tags')).toContainText('app=api')
  await expect(row.locator('.soha-service-selector-tags')).toContainText(
    'app.kubernetes.io/component=backend-api-v1',
  )
  await row.getByRole('link', { name: '查看 api 的后端', exact: true }).press('Enter')
  await expect(page).toHaveURL(
    /network\/services\/api\?namespace=monitoring&clusterId=cluster-a&tab=backends$/,
  )
  await expect(active(page)).toContainText('10.42.0.8')
  verifyBrowserEvidence(evidence)
})

test('compact selectors expand complete fields with keyboard access and stay within narrow cells', async ({
  context,
  page,
}) => {
  const { state, evidence } = await setup(context, page)
  Object.assign(state.service.selector, {
    'operator.prometheus.io/name': 'monitoring-prometheus',
    'networking.example.test/very-long-backend-component-identifier':
      'backend-service-version-with-a-long-name',
  })
  await page.goto('/network/services?clusterId=cluster-a')
  await page.getByPlaceholder('搜索 Service / namespace / type / port / selector').fill('api')
  const row = page
    .locator('.ant-table-row')
    .filter({ has: page.getByRole('button', { name: 'api', exact: true }) })
  await expect(row.locator('.soha-service-selector-tag')).toHaveCount(2)
  const more = row.getByRole('button', { name: '查看完整选择器', exact: true })
  await expect(more).toHaveText('+2')
  const insideCell = () =>
    row.locator('.soha-service-selector-summary').evaluate((el) => {
      const cell = el.closest('.ant-table-cell')!.getBoundingClientRect()
      return [...el.querySelectorAll('.soha-service-selector-tag, button')].every((child) => {
        const r = child.getBoundingClientRect()
        return r.left >= cell.left - 1 && r.right <= cell.right + 1
      })
    })
  await expect.poll(insideCell).toBe(true)
  const handle = page.getByRole('separator', { name: '调整选择器列宽', exact: true })
  for (let i = 0; i < 10; i++) await handle.press('ArrowLeft')
  await expect
    .poll(() =>
      page
        .getByRole('columnheader', { name: '选择器', exact: true })
        .evaluate((el) => el.getBoundingClientRect().width),
    )
    .toBe(180)
  await expect.poll(insideCell).toBe(true)
  await more.press('Enter')
  const expanded = page.locator('.soha-service-selector-expanded')
  await expect(expanded.locator('.soha-service-selector-tag')).toHaveCount(4)
  await expect(expanded).toContainText(
    'networking.example.test/very-long-backend-component-identifier=backend-service-version-with-a-long-name',
  )
  await expect
    .poll(() =>
      expanded.locator('.soha-service-selector-tag').evaluateAll((tags) =>
        tags.every((tag) => {
          const box = tag.getBoundingClientRect()
          const range = document.createRange()
          range.selectNodeContents(tag)
          return [...range.getClientRects()].every(
            (r) => r.left >= box.left - 1 && r.right <= box.right + 1,
          )
        }),
      ),
    )
    .toBe(true)
  await expect(expanded.getByRole('button', { name: '收起', exact: true })).toBeFocused()
  await page.keyboard.press('Escape')
  await expect(expanded).toHaveCount(0)
  await expect(more).toBeFocused()
  expect(state.writes).toEqual([])
  verifyBrowserEvidence(evidence)
})

test('Service editor presents configuration together, keeps footer visible and requires preflight before update', async ({
  context,
  page,
}) => {
  const { state, evidence } = await setup(context, page)
  await page.goto(detailRoute)
  await page
    .locator('.soha-k8s-detail-actions')
    .getByRole('button', { name: '编辑 Service', exact: true })
    .click()
  const editor = page.getByRole('dialog', { name: '编辑 Service · api', exact: true })
  await expect(editor.getByLabel('容器端口')).toHaveValue('web-http')
  await expect(editor.getByRole('button', { name: '下一步', exact: true })).toHaveCount(0)
  await editor.getByLabel('容器端口').fill('updated-http')
  for (const width of [1440, 900, 375]) {
    await page.setViewportSize({ width, height: 650 })
    await expect(editor.getByRole('button', { name: '预检更改', exact: true })).toBeInViewport()
    await expect
      .poll(() =>
        editor.evaluate((el) => {
          const r = el.getBoundingClientRect()
          return r.left >= 0 && r.right <= innerWidth + 1
        }),
      )
      .toBe(true)
  }
  await page.setViewportSize({ width: 1440, height: 900 })
  await editor.getByText('标签 1 · 注释 0', { exact: true }).click()
  await expect(editor.locator('input').filter({ visible: true }).first()).toBeVisible()
  expect(state.writes).toEqual([])
  const planRequest = page.waitForRequest((request) =>
    request.url().includes('/resources/update-plan'),
  )
  await editor.getByRole('button', { name: '预检更改', exact: true }).click()
  const planned = (await planRequest).postDataJSON() as { content: string }
  expect(parse(planned.content)).toMatchObject({
    metadata: { name: 'api', namespace: 'monitoring', resourceVersion: '7' },
    spec: {
      clusterIP: '10.43.0.20',
      internalTrafficPolicy: 'Local',
      publishNotReadyAddresses: false,
      ports: [{ name: 'http', targetPort: 'updated-http', nodePort: 30080 }],
    },
  })
  const plan = page.getByRole('dialog', { name: '确认资源变更', exact: true })
  await expect(plan).toBeVisible()
  expect(state.writes).toEqual(['POST ' + root + '/resources/update-plan'])
  await expect(editor.getByLabel('容器端口')).toBeDisabled()
  await plan.getByRole('button', { name: '确认更新', exact: true }).click()
  await expect(
    page.getByText('Service resourceVersion changed', { exact: false }).first(),
  ).toBeVisible()
  await plan.getByRole('button', { name: /取\s*消/ }).click()
  await expect(editor.getByLabel('容器端口')).toHaveValue('updated-http')
  expect(state.writes).toEqual([
    'POST ' + root + '/resources/update-plan',
    'PUT ' + root + '/network/services/api/yaml',
  ])
  verifyBrowserEvidence(evidence)
})

test('Service relationships adapt only explicit scope changes and clear protected graph content on permission failure', async ({
  context,
  page,
}) => {
  const { state, evidence } = await setup(context, page)
  state.graph.nodes.push({
    id: 'other',
    resource: {
      clusterId: 'cluster-a',
      namespace: 'monitoring',
      name: 'another-service',
      kind: 'Service',
      apiVersion: 'v1',
      scopeMode: 'namespace',
    },
  })
  await page.goto(detailRoute + '&tab=relationships')
  await expect(active(page).locator('.react-flow__node')).toHaveCount(2)
  await expect(active(page)).toContainText('选择器匹配')
  await active(page)
    .getByRole('button', { name: /Pod monitoring.*api-worker/ })
    .click()
  await expect(
    active(page).getByRole('button', { name: '查看资源详情', exact: true }),
  ).toBeEnabled()
  const viewport = active(page).locator('.react-flow__viewport')
  let previous: string | null = null
  await expect
    .poll(
      async () => {
        const next = await viewport.getAttribute('style')
        const stable = next === previous
        previous = next
        return stable
      },
      { intervals: [250] },
    )
    .toBe(true)
  await active(page).getByRole('switch', { name: '其他关联', exact: true }).click()
  await expect(active(page).locator('.react-flow__node')).toHaveCount(3)
  await expect(viewport).not.toHaveAttribute('style', previous || '')
  await active(page).getByText('列表', { exact: true }).click()
  await expect(
    active(page).getByRole('link', { name: 'another-service', exact: true }),
  ).toHaveAttribute('href', /clusterId=cluster-a/)
  expect(state.reads.filter((path) => path.includes('/resources/graph'))).toHaveLength(1)
  state.graphFail = 503
  await active(page).getByRole('button', { name: '刷新资源关系', exact: true }).click()
  await expect(active(page)).toContainText('Graph read failed')
  await expect(active(page).getByRole('link', { name: 'api-worker', exact: true })).toBeVisible()
  state.graphFail = 403
  await active(page).getByRole('button', { name: '刷新资源关系', exact: true }).click()
  await expect(active(page).getByRole('link', { name: 'api-worker', exact: true })).toHaveCount(0)
  await expect(active(page).locator('.react-flow__node')).toHaveCount(0)
  expect(state.writes).toEqual([])
  verifyBrowserEvidence(evidence)
})
