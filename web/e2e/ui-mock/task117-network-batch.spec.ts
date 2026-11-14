import { test, expect, type BrowserContext, type Page } from '@playwright/test'
import { stringify } from 'yaml'
import type { KubernetesResourceGraph } from '@opensoha/contracts/gen/ts/sohaapi'
import { baselineResponse } from '../fixtures/baseline-data'
import { installMocks, verifyBrowserEvidence } from '../fixtures/mock-app'

const root = '/api/v1/clusters/cluster-a/network/'
const cases = [
  {
    kind: 'gatewayclasses',
    label: 'GatewayClass',
    permission: 'gateway-classes',
    cluster: true,
    tabs: ['概览', 'Gateways', '关联资源', 'YAML'],
    fields: { controllerName: 'example.io/controller', accepted: 'True', parametersRef: '' },
    spec: { controllerName: 'example.io/controller' },
  },
  {
    kind: 'gateways',
    label: 'Gateway',
    permission: 'gateways',
    tabs: ['概览', '监听器', '路由', '关联资源', 'YAML'],
    fields: {
      gatewayClass: 'class-a',
      addresses: ['192.0.2.1'],
      listenerCount: 1,
      listeners: [{ name: 'http', protocol: 'HTTP', port: 80, attachedRoutes: 0 }],
      routes: [],
    },
    spec: {
      gatewayClassName: 'class-a',
      listeners: [{ name: 'http', protocol: 'HTTP', port: 80 }],
    },
  },
  {
    kind: 'httproutes',
    label: 'HTTPRoute',
    permission: 'http-routes',
    tabs: ['概览', '规则与后端', '关联资源', 'YAML'],
    fields: { hostnames: ['api.test'], parentRefs: [], rules: [] },
    spec: { hostnames: ['api.test'], rules: [] },
  },
  {
    kind: 'grpcroutes',
    label: 'GRPCRoute',
    permission: 'grpc-routes',
    tabs: ['概览', '规则与后端', '关联资源', 'YAML'],
    fields: { hostnames: ['grpc.test'], parentRefs: [], rules: [], ruleCount: 0 },
    spec: { hostnames: ['grpc.test'], rules: [] },
  },
  {
    kind: 'referencegrants',
    label: 'ReferenceGrant',
    permission: 'reference-grants',
    tabs: ['概览', '关联资源', 'YAML'],
    fields: {
      from: ['source:HTTPRoute:gateway.networking.k8s.io'],
      to: ['Service/api:core'],
      fromRefs: [{ namespace: 'source', kind: 'HTTPRoute', group: 'gateway.networking.k8s.io' }],
      toRefs: [{ group: '', kind: 'Service', name: 'api' }],
    },
    spec: {
      from: [{ namespace: 'source', kind: 'HTTPRoute', group: 'gateway.networking.k8s.io' }],
      to: [{ group: '', kind: 'Service', name: 'api' }],
    },
  },
  {
    kind: 'backendtlspolicies',
    label: 'BackendTLSPolicy',
    permission: 'backend-tls-policies',
    tabs: ['概览', '关联资源', 'YAML'],
    fields: {
      targetRefs: ['monitoring:Service/api'],
      hostname: 'api.test',
      wellKnownCACertificates: 'System',
    },
    spec: {
      targetRefs: [{ group: '', kind: 'Service', name: 'api' }],
      validation: { hostname: 'api.test', wellKnownCACertificates: 'System' },
    },
  },
  {
    kind: 'endpointslices',
    label: 'EndpointSlice',
    permission: 'endpoint-slices',
    tabs: ['概览', '端点', '关联资源', 'YAML'],
    fields: {
      addressType: 'IPv4',
      endpoints: [{ address: '10.42.0.8', ready: true, targetRef: 'Pod/api', nodeName: 'worker' }],
      ports: ['8080/TCP'],
      serviceName: 'api',
    },
    spec: {},
  },
  {
    kind: 'networkpolicies',
    label: 'NetworkPolicy',
    permission: 'network-policies',
    tabs: ['概览', '规则', '选中 Pods', '关联资源', 'YAML'],
    fields: { policyTypes: ['Ingress'], podSelector: 'app=api', rules: [], matchingPods: [] },
    spec: { podSelector: { matchLabels: { app: 'api' } }, policyTypes: ['Ingress'], ingress: [] },
  },
] as const
const route = (kind: string) =>
  '/network/' + (cases.slice(0, 6).some((c) => c.kind === kind) ? 'gateway-api/' : '') + kind
const panel = (page: Page) => page.getByRole('tabpanel').filter({ visible: true })
async function setup(
  context: BrowserContext,
  page: Page,
  c: (typeof cases)[number],
  readonly = false,
) {
  const cluster = 'cluster' in c && c.cluster
  const record = {
    name: 'sample',
    ...(cluster ? {} : { namespace: 'monitoring' }),
    ageSeconds: 60,
    allowedActions: readonly ? ['view'] : ['view', 'update', 'delete'],
    ...c.fields,
  }
  const apiVersion =
    c.kind === 'endpointslices'
      ? 'discovery.k8s.io/v1'
      : c.kind === 'networkpolicies'
        ? 'networking.k8s.io/v1'
        : c.kind === 'referencegrants'
          ? 'gateway.networking.k8s.io/v1beta1'
          : 'gateway.networking.k8s.io/v1'
  const manifest = {
    apiVersion,
    kind: c.label,
    metadata: {
      name: 'sample',
      ...(cluster ? {} : { namespace: 'monitoring' }),
      resourceVersion: '7',
      generation: 1,
    },
    spec: c.spec,
    ...(c.kind === 'endpointslices'
      ? {
          addressType: 'IPv4',
          ports: [{ port: 8080, protocol: 'TCP' }],
          endpoints: [
            {
              addresses: ['10.42.0.8'],
              conditions: { ready: true },
              targetRef: { kind: 'Pod', name: 'api', namespace: 'monitoring' },
            },
          ],
        }
      : {}),
  }
  const state = {
    fail: 0,
    denied: false,
    reads: [] as string[],
    graphReads: [] as string[],
    writes: [] as string[],
  }
  const graph: KubernetesResourceGraph = {
    clusterId: 'cluster-a',
    ...(cluster ? {} : { namespace: 'monitoring' }),
    rootId: 'root',
    generatedAt: '2026-11-05T00:00:00Z',
    nodes: [
      {
        id: 'root',
        status: 'resolved',
        resource: {
          clusterId: 'cluster-a',
          apiVersion,
          kind: c.label,
          name: 'sample',
          ...(cluster ? {} : { namespace: 'monitoring' }),
          scopeMode: cluster ? 'cluster' : 'namespace',
        },
      },
    ],
    edges: [],
    evidence: [],
    warnings: [],
  }
  await context.addInitScript(() =>
    localStorage.setItem(
      'soha-scope',
      JSON.stringify({ state: { clusterId: 'cluster-a', namespace: 'monitoring' }, version: 0 }),
    ),
  )
  const evidence = await installMocks(context, page, (url, method) => {
    if (url.pathname === '/api/v1/access/permission-snapshot') {
      const body = baselineResponse(url.pathname)!
      if (!body.data || !('permissionKeys' in body.data)) throw Error('missing permissions')
      body.data.permissionKeys = state.denied
        ? []
        : [
            ...body.data.permissionKeys!,
            ...cases.map((c) => 'platform.network.' + c.permission + '.view'),
            'platform.pods.view',
            'platform.nodes.view',
            ...(!readonly
              ? ['update', 'delete', 'create'].map(
                  (a) => 'platform.network.' + c.permission + '.' + a,
                )
              : []),
          ]
      return { body }
    }
    if (url.pathname.endsWith('/resource-creation/scope-decision'))
      return {
        body: { data: { allowed: !readonly, scopeMode: cluster ? 'cluster' : 'namespace' } },
      }
    if (url.pathname === '/api/v1/clusters/cluster-a/resources/graph' && method !== 'GET') {
      state.writes.push(`${method} ${url.pathname}`)
      return undefined
    }
    if (url.pathname === '/api/v1/clusters/cluster-a/resources/graph' && method === 'GET') {
      state.graphReads.push(url.search)
      const expectedNamespace = cluster ? null : 'monitoring'
      const exactIdentity =
        url.searchParams.get('kind') === c.label &&
        url.searchParams.get('name') === 'sample' &&
        url.searchParams.get('namespace') === expectedNamespace
      if (!exactIdentity) {
        evidence.missing.push(`incorrect graph identity: ${url.search}`)
        return {
          status: 400,
          body: { error: { code: 'incorrect_scope', message: 'Incorrect graph identity' } },
        }
      }
      return { body: { data: graph } }
    }
    if (url.pathname.startsWith(root + c.kind)) {
      if (method === 'GET') state.reads.push(url.pathname + url.search)
      else state.writes.push(method + ' ' + url.pathname)
      if (state.fail)
        return {
          status: state.fail,
          body: { error: { code: 'failed', message: 'Network read failed' } },
        }
      if (url.pathname.endsWith('/yaml'))
        return { body: { data: { content: stringify(manifest), resourceVersion: '7' } } }
      if (url.pathname.endsWith('/detail')) return { body: { data: record } }
      if (method === 'DELETE') return { body: { data: null } }
      return {
        body: {
          data: Array.from({ length: 40 }, (_, i) => ({
            ...record,
            name: i === 0 ? 'sample' : 'sample-' + String(i).padStart(2, '0'),
            ...(c.kind === 'endpointslices' ? { endpoints: 1 } : {}),
          })),
        },
      }
    }
    return undefined
  })
  return { state, evidence }
}
for (const c of cases) {
  test(c.label + ' list continuous browsing, search and scope', async ({ context, page }) => {
    const { state, evidence } = await setup(context, page, c)
    await page.goto(route(c.kind) + '?clusterId=cluster-a&namespace=monitoring')
    await expect(page.getByRole('status').filter({ hasText: '40 个 ' + c.label })).toBeVisible()
    await expect(page.locator('.ant-pagination')).toHaveCount(0)
    await expect(page.locator('.ant-table-tbody-virtual-holder')).toBeVisible()
    await page.locator('.soha-management-query-fields input').first().fill('sample-39')
    await expect(page.locator('.ant-table-row')).toHaveCount(1)
    await expect(page.locator('.ant-table-row')).toContainText('sample-39')
    if ('cluster' in c) expect(state.reads.every((p) => !p.includes('namespace='))).toBe(true)
    expect(state.writes).toEqual([])
    verifyBrowserEvidence(evidence)
  })
  test(
    c.label + ' detail tabs, readonly actions, theme and aligned narrow content',
    async ({ context, page }) => {
      const { state, evidence } = await setup(context, page, c, true)
      await page.goto(route(c.kind) + '/sample?clusterId=cluster-a&namespace=monitoring')
      await expect(page.getByRole('heading', { name: 'sample', exact: true })).toBeVisible()
      await expect(page.getByRole('tab')).toHaveText([...c.tabs])
      await expect(page.getByRole('button', { name: '删除 ' + c.label, exact: true })).toHaveCount(
        0,
      )
      await expect(page.getByRole('button', { name: '编辑 ' + c.label, exact: true })).toHaveCount(
        0,
      )
      const header = await page.locator('.soha-k8s-detail-header').boundingBox(),
        content = await page.locator('.soha-workload-overview').boundingBox()
      expect(header && content ? Math.abs(header.x - content.x) : 100).toBeLessThanOrEqual(1)
      await page.getByRole('tab', { name: '关联资源', exact: true }).click()
      await expect(panel(page).locator('.react-flow__node[data-id="root"]')).toBeVisible()
      expect(state.graphReads).toHaveLength(1)
      for (const width of [900, 375]) {
        await page.setViewportSize({ width, height: 900 })
        await expect
          .poll(() => page.evaluate(() => document.documentElement.scrollWidth <= innerWidth + 1))
          .toBe(true)
      }
      await page.getByRole('button', { name: '切换到深色模式' }).click()
      await expect(page.locator('html')).toHaveAttribute('data-theme-mode', 'dark')
      expect(state.writes).toEqual([])
      verifyBrowserEvidence(evidence)
    },
  )
  test(
    c.label + ' refresh error preserves data then permission failure clears it',
    async ({ context, page }) => {
      const { state, evidence } = await setup(context, page, c)
      await page.goto(route(c.kind) + '/sample?clusterId=cluster-a&namespace=monitoring')
      await expect(page.getByRole('heading', { name: 'sample', exact: true })).toBeVisible()
      state.fail = 503
      await page.getByRole('button', { name: '刷新 ' + c.label, exact: true }).click()
      await expect(page.getByText('Network read failed', { exact: false }).first()).toBeVisible()
      await expect(page.getByRole('heading', { name: 'sample', exact: true })).toBeVisible()
      await expect(page.getByRole('switch', { name: /刷新失败/ })).toBeVisible({ timeout: 15_000 })
      // Dismiss the real, visible notifications before retrying the covered header button.
      const notices = page
        .locator('.ant-notification')
        .getByRole('button', { name: '关闭', exact: true })
      for (let i = 0; i < 4 && (await notices.count()); i++) await notices.first().click()
      await expect(notices).toHaveCount(0)
      state.fail = 403
      await page.getByRole('button', { name: '刷新 ' + c.label, exact: true }).click()
      await expect(page.getByRole('heading', { name: 'sample', exact: true })).toHaveCount(0)
      expect(state.writes).toEqual([])
      verifyBrowserEvidence(evidence)
    },
  )
}

async function forwardSetup(context: BrowserContext, page: Page, readonly = false) {
  const state = {
    writes: [] as string[],
    body: undefined as unknown,
    sessions: [
      {
        sessionId: 'session-demo',
        clusterId: 'cluster-a',
        namespace: 'monitoring',
        targetKind: 'Pod',
        targetName: 'api',
        localPort: 28080,
        remotePort: 8080,
        status: 'active',
        createdAt: '2026-11-03T01:00:00Z',
      },
    ],
  }
  await context.addInitScript(() =>
    localStorage.setItem(
      'soha-scope',
      JSON.stringify({ state: { clusterId: 'cluster-a', namespace: 'monitoring' }, version: 0 }),
    ),
  )
  const evidence = await installMocks(context, page, (url, method, request) => {
    if (url.pathname === '/api/v1/access/permission-snapshot') {
      const body = baselineResponse(url.pathname)!
      if (!body.data || !('permissionKeys' in body.data)) throw Error('missing permissions')
      body.data.permissionKeys = [
        ...body.data.permissionKeys!.filter(
          (p) => !p.startsWith('platform.network.port-forwards.'),
        ),
        'platform.network.port-forwards.view',
        ...(readonly
          ? []
          : ['platform.network.port-forwards.create', 'platform.network.port-forwards.delete']),
      ]
      return { body }
    }
    if (url.pathname === '/api/v1/clusters/capabilities')
      return {
        body: {
          data: [
            {
              key: 'port.forward',
              label: 'Port forwarding',
              category: 'network',
              direct: { status: 'available' },
              agent: { status: 'available' },
            },
          ],
        },
      }
    if (url.pathname === '/api/v1/clusters/cluster-a/workloads/pods')
      return { body: { data: [{ name: 'api', namespace: 'monitoring', phase: 'Running' }] } }
    if (url.pathname === root + 'port-forwards') {
      if (method === 'GET') return { body: { data: state.sessions } }
      if (method === 'POST') {
        state.writes.push(method + ' ' + url.pathname)
        state.body = request.postDataJSON()
        return { body: { data: state.sessions[0] } }
      }
    }
    if (url.pathname === root + 'port-forwards/session-demo' && method === 'DELETE') {
      state.writes.push(method + ' ' + url.pathname)
      state.sessions = []
      return { body: { data: null } }
    }
    return undefined
  })
  return { state, evidence }
}
test('Port Forward filters are compact and view-only role cannot create or stop', async ({
  context,
  page,
}) => {
  const { state, evidence } = await forwardSetup(context, page, true)
  await page.goto('/network/port-forward?clusterId=cluster-a&namespace=monitoring')
  await expect(page.getByRole('status').filter({ hasText: '1 个 Port Forwards' })).toBeVisible()
  await expect(page.getByRole('button', { name: /新建转发/ })).toHaveCount(0)
  await expect(
    page.getByRole('button', { name: '更多操作：session-demo', exact: true }),
  ).toHaveCount(0)
  const filter = page.getByRole('combobox', { name: '会话状态筛选' })
  await expect(filter).toBeVisible()
  await expect
    .poll(() => filter.evaluate((el) => el.closest('.ant-select')!.getBoundingClientRect().width))
    .toBeLessThan(300)
  expect(state.writes).toEqual([])
  verifyBrowserEvidence(evidence)
})
test('Port Forward creates exact draft only on submission and stops only confirmed session', async ({
  context,
  page,
}) => {
  const { state, evidence } = await forwardSetup(context, page)
  await page.goto('/network/port-forward?clusterId=cluster-a&namespace=monitoring')
  await page.getByRole('button', { name: /新建转发/ }).click()
  const dialog = page.getByRole('dialog')
  await dialog.getByLabel('目标名称', { exact: true }).fill('api')
  await dialog.getByLabel('Core 本地端口', { exact: true }).fill('28081')
  await dialog.getByLabel('Pod 远端端口', { exact: true }).fill('8080')
  expect(state.writes).toEqual([])
  await dialog.getByRole('button', { name: /创.*建/ }).click()
  await expect.poll(() => state.writes.length).toBe(1)
  expect(state.body).toMatchObject({
    targetKind: 'Pod',
    targetName: 'api',
    namespace: 'monitoring',
    localPort: 28081,
    remotePort: 8080,
  })
  await expect(dialog).toHaveCount(0)
  await page.getByRole('button', { name: '更多操作：session-demo', exact: true }).click()
  await page.getByRole('menuitem', { name: /停止/ }).click()
  await expect(page.getByRole('dialog')).toContainText('session-demo')
  await page
    .getByRole('dialog')
    .getByRole('button', { name: /取\s*消/ })
    .click()
  expect(state.writes).toHaveLength(1)
  await page.getByRole('button', { name: '更多操作：session-demo', exact: true }).click()
  await page.getByRole('menuitem', { name: /停止/ }).click()
  await page
    .getByRole('dialog')
    .getByRole('button', { name: /停.*止/ })
    .click()
  await expect.poll(() => state.writes.length).toBe(2)
  expect(state.writes[1]).toBe('DELETE ' + root + 'port-forwards/session-demo')
  verifyBrowserEvidence(evidence)
})
