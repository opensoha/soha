import { test, expect, type BrowserContext, type Page } from '@playwright/test'
import { parse, stringify } from 'yaml'
import { resolve } from 'node:path'
import permissionCatalog from '@opensoha/contracts/auth/permission-catalog.json' with { type: 'json' }
const capture = async (page: Page, name: string) => {
  if (process.env.TASK117_EVIDENCE_DIR)
    await page.screenshot({
      path: resolve(process.env.TASK117_EVIDENCE_DIR, name + '.png'),
      fullPage: true,
      animations: 'disabled',
    })
}
import { baselineResponse } from '../fixtures/baseline-data'
import { installMocks, verifyBrowserEvidence } from '../fixtures/mock-app'
const root = '/api/v1/clusters/cluster-a',
  route = '/network/ingresses/web?clusterId=cluster-a&namespace=monitoring'
const active = (page: Page) => page.getByRole('tabpanel').filter({ visible: true })
async function setup(
  context: BrowserContext,
  page: Page,
  options: { readonly?: boolean; noRefs?: boolean; hostless?: boolean } = {},
) {
  const now = '2026-11-02T01:00:00Z'
  const manifest = {
    apiVersion: 'networking.k8s.io/v1',
    kind: 'Ingress',
    metadata: {
      name: 'web',
      namespace: 'monitoring',
      resourceVersion: '7',
      labels: { app: 'web' },
      annotations: { 'nginx.ingress.kubernetes.io/rewrite-target': '/$1' },
      managedFields: [{ manager: 'helm' }],
    },
    spec: {
      ingressClassName: 'nginx',
      extension: { preserved: true },
      defaultBackend: { service: { name: 'fallback', port: { number: 8080 } } },
      rules: [
        {
          host: 'a.example.test',
          extension: 1,
          http: {
            paths: [
              {
                path: '/api',
                pathType: 'Exact',
                extension: 'first',
                backend: { service: { name: 'api', port: { name: 'http' } } },
              },
              {
                path: '/assets',
                pathType: 'ImplementationSpecific',
                extension: 'second',
                backend: { resource: { apiGroup: 'example.test', kind: 'Bucket', name: 'files' } },
              },
            ],
          },
        },
        {
          host: 'b.example.test',
          http: {
            paths: [
              {
                path: '/',
                pathType: 'Prefix',
                backend: { service: { name: 'web', port: { number: 80 } } },
              },
            ],
          },
        },
      ],
      tls: [
        { hosts: ['a.example.test'], secretName: 'tls-a' },
        { hosts: ['b.example.test'], secretName: 'tls-b' },
      ],
    },
    status: { loadBalancer: { ingress: [{ ip: '1.2.3.4' }] } },
  }
  const ingress = {
    name: 'web',
    namespace: 'monitoring',
    className: 'nginx',
    hosts: options.hostless ? undefined : ['a.example.test', 'b.example.test'],
    address: '1.2.3.4',
    ageSeconds: 3600,
    allowedActions: options.readonly ? ['view'] : ['view', 'update', 'delete'],
    labels: { app: 'web' },
    annotations: manifest.metadata.annotations,
    backendServices: ['api', 'fallback'],
    routes: [
      {
        host: 'a.example.test',
        path: '/api',
        pathType: 'Exact',
        serviceName: options.noRefs ? '' : 'api',
        servicePort: 'http',
        tls: true,
      },
    ],
    backends: options.noRefs
      ? undefined
      : [
          {
            serviceName: 'api',
            endpoints: [
              { address: '10.42.0.8', ready: true, targetRef: 'Pod/api-worker' },
              { address: '2001:db8::1' },
            ],
            pods: [
              {
                name: 'api-worker',
                namespace: 'monitoring',
                phase: 'Running',
                readyContainers: '1/1',
                restarts: 0,
                ageSeconds: 300,
                workloads: [{ kind: 'Deployment', name: 'api', namespace: 'monitoring' }],
              },
            ],
          },
          { serviceName: 'fallback', endpoints: [], pods: [] },
        ],
  }
  const state = {
    reads: [] as string[],
    writes: [] as string[],
    fail: 0,
    eventFail: 0,
    graphFail: 0,
    yamlFail: 0,
    certificateFail: 0,
    certificateMismatch: false,
    certificateStatus: 'available',
    yaml: stringify(manifest),
    ingress,
    manifest,
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
    const failure = (status: number) => ({
      status,
      body: { error: { code: 'read_failed', message: 'Ingress read failed' } },
    })
    if (url.pathname === '/api/v1/access/permission-snapshot') {
      const body = baselineResponse(url.pathname)!
      if (!body.data || !('permissionKeys' in body.data)) throw new Error('permissions missing')
      body.data.permissionKeys = body.data.permissionKeys!.filter(
        (p) =>
          !p.startsWith('platform.network.ingresses.') &&
          p !== 'platform.configuration.secret-data.view' &&
          (!options.noRefs ||
            ![
              'platform.workloads.overview.view',
              'platform.network.services.view',
              'platform.pods.view',
              'platform.configuration.secrets.view',
              'platform.network.ingress-classes.view',
            ].includes(p)),
      )
      body.data.permissionKeys.push(
        'platform.network.ingresses.view',
        ...(!options.noRefs
          ? [
              'platform.workloads.overview.view',
              'platform.network.services.view',
              'platform.pods.view',
              'platform.configuration.secrets.view',
              'platform.network.ingress-classes.view',
              'platform.network.endpoint-slices.view',
              'platform.deployment.view',
            ]
          : []),
        ...(options.readonly
          ? []
          : [
              'platform.network.ingresses.update',
              'platform.network.ingresses.delete',
              'platform.network.ingresses.create',
            ]),
      )
      return { body }
    }
    const certificate = url.pathname.match(/\/configuration\/secrets\/([^/]+)\/certificate$/)
    if (certificate) {
      if (url.searchParams.get('namespace') !== 'monitoring') return failure(403)
      if (state.certificateFail) return failure(state.certificateFail)
      return {
        body: {
          data: {
            name: certificate[1],
            namespace: state.certificateMismatch ? 'other-team' : 'monitoring',
            status: state.certificateStatus,
            selfSigned: true,
            notBefore: '2026-01-01T00:00:00Z',
            notAfter: certificate[1] === 'tls-a' ? '2027-02-01T00:00:00Z' : '2026-10-01T00:00:00Z',
          },
        },
      }
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
    if (url.pathname === root + '/network/ingresses' && method === 'GET')
      return {
        body: {
          data: Array.from({ length: 35 }, (_, i) => ({
            ...ingress,
            name: i === 0 ? 'web' : `ingress-${String(i).padStart(2, '0')}`,
            namespace: i < 20 ? 'monitoring' : 'default',
          })).filter(
            (i) =>
              !url.searchParams.get('namespace') ||
              i.namespace === url.searchParams.get('namespace'),
          ),
        },
      }
    if (url.pathname === root + '/network/ingresses/web/detail')
      return state.fail ? failure(state.fail) : { body: { data: ingress } }
    if (url.pathname === root + '/network/ingresses/web/yaml' && method === 'GET')
      return state.yamlFail
        ? failure(state.yamlFail)
        : {
            body: {
              data: { kind: 'Ingress', name: 'web', namespace: 'monitoring', content: state.yaml },
            },
          }
    if (url.pathname === root + '/network/ingresses/web/yaml' && method === 'PUT')
      return {
        status: 409,
        body: { error: { code: 'conflict', message: 'Ingress resourceVersion changed' } },
      }
    if (url.pathname === root + '/network/ingresses/web' && method === 'DELETE')
      return { body: { data: {} } }
    if (url.pathname === root + '/resources/update-plan')
      return {
        body: {
          data: {
            ready: true,
            riskLevel: 'medium',
            capability: 'kubernetes-resource-update',
            target: 'cluster-a/monitoring/Ingress/web',
            requiresApproval: false,
            warnings: [],
            changes: [],
            blockers: [],
          },
        },
      }
    if (url.pathname === root + '/events')
      return state.eventFail
        ? failure(state.eventFail)
        : {
            body: {
              data: [
                {
                  name: 'e1',
                  namespace: 'monitoring',
                  involvedKind: 'Ingress',
                  involvedName: 'web',
                  type: 'Warning',
                  reason: 'SyncFailed',
                  message: 'certificate pending',
                  count: 2,
                  ageSeconds: 10,
                  lastTimestamp: now,
                },
                {
                  name: 'e2',
                  namespace: 'monitoring',
                  involvedKind: 'Ingress',
                  involvedName: 'web',
                  type: 'Normal',
                  reason: 'Scheduled',
                  message: 'without timestamp',
                  count: 1,
                  ageSeconds: 30,
                },
                {
                  name: 'wrong-kind',
                  namespace: 'monitoring',
                  involvedKind: 'Service',
                  involvedName: 'web',
                  type: 'Warning',
                  reason: 'SHOULD-NOT-RENDER',
                  message: 'service event',
                  count: 1,
                  ageSeconds: 10,
                },
                {
                  name: 'wrong-ns',
                  namespace: 'default',
                  involvedKind: 'Ingress',
                  involvedName: 'web',
                  type: 'Warning',
                  reason: 'WRONG-NAMESPACE',
                  message: 'other namespace',
                  count: 1,
                  ageSeconds: 10,
                },
              ],
            },
          }
    if (
      method === 'GET' &&
      url.pathname === root + '/workloads/pods/api-worker/detail' &&
      url.searchParams.get('namespace') === 'monitoring'
    ) {
      return {
        body: {
          data: {
            name: 'api-worker',
            namespace: 'monitoring',
            uid: 'mock-api-worker',
            phase: 'Running',
            containers: [],
          },
        },
      }
    }
    if (
      method === 'GET' &&
      url.pathname === root + '/workloads/pods/api-worker/metrics' &&
      url.searchParams.get('namespace') === 'monitoring'
    ) {
      return { body: { data: { configured: false, series: [] } } }
    }
    if (
      method === 'GET' &&
      url.pathname === root + '/resources/graph' &&
      url.searchParams.get('kind') === 'Ingress' &&
      url.searchParams.get('name') === 'web' &&
      url.searchParams.get('namespace') === 'monitoring'
    ) {
      const nodes = [
        {
          id: 'ingress',
          resource: {
            clusterId: 'cluster-a',
            namespace: 'monitoring',
            apiVersion: 'networking.k8s.io/v1',
            kind: 'Ingress',
            name: 'web',
            scopeMode: 'namespace',
          },
        },
        ...(!options.noRefs
          ? [
              {
                id: 'service',
                resource: {
                  clusterId: 'cluster-a',
                  namespace: 'monitoring',
                  apiVersion: 'v1',
                  kind: 'Service',
                  name: 'api',
                  scopeMode: 'namespace',
                },
              },
              {
                id: 'pod',
                resource: {
                  clusterId: 'cluster-a',
                  namespace: 'monitoring',
                  apiVersion: 'v1',
                  kind: 'Pod',
                  name: 'api-worker',
                  scopeMode: 'namespace',
                },
              },
            ]
          : []),
      ]
      return state.graphFail
        ? failure(state.graphFail)
        : {
            body: {
              data: {
                clusterId: 'cluster-a',
                generatedAt: now,
                rootId: 'ingress',
                nodes,
                edges: options.noRefs
                  ? []
                  : [
                      { id: '1', sourceId: 'ingress', targetId: 'service', relation: 'routes-to' },
                      { id: '2', sourceId: 'service', targetId: 'pod', relation: 'selects' },
                    ],
                warnings: [],
                evidence: [],
              },
            },
          }
    }
    return undefined
  })
  return { state, evidence }
}

test('Ingress list uses continuous rows, handles omitted hosts, keeps scope and never fans out detail reads', async ({
  context,
  page,
}) => {
  const { state, evidence } = await setup(context, page, { hostless: true })
  await page.goto('/network/ingresses?clusterId=cluster-a')
  await expect(page.getByRole('status').filter({ hasText: '35 个 Ingress' })).toBeVisible()
  await expect(page.locator('.ant-pagination')).toHaveCount(0)
  await page.getByPlaceholder('搜索 Ingress / namespace / host / service').fill('web')
  await expect(page.getByRole('button', { name: 'web', exact: true })).toBeVisible()
  expect(state.reads.some((p) => /\/detail|\/yaml|\/events|resources\/graph/.test(p))).toBe(false)
  await page.getByRole('button', { name: '更多操作：web', exact: true }).click()
  await expect(page.getByRole('menuitem', { name: '编辑', exact: true })).toBeVisible()
  await expect(page.getByRole('menuitem', { name: '删除', exact: true })).toBeVisible()
  await page.getByRole('menuitem', { name: '编辑', exact: true }).click()
  await expect(page.getByRole('dialog', { name: '编辑 Ingress · web', exact: true })).toBeVisible()
  expect(
    state.reads.some((path) => path === root + '/network/ingresses/web/yaml?namespace=monitoring'),
  ).toBe(true)
  await page
    .getByRole('dialog')
    .getByRole('button', { name: /取\s*消/ })
    .click()
  await page.getByRole('button', { name: '切换到命名空间 monitoring', exact: true }).click()
  await expect
    .poll(() =>
      state.reads.some(
        (path) =>
          path === root.replace('/api/v1', '') + '/network/ingresses?namespace=monitoring' ||
          path === root + '/network/ingresses?namespace=monitoring',
      ),
    )
    .toBe(true)
  await page.getByRole('button', { name: 'web', exact: true }).click()
  await expect(page).toHaveURL(/network\/ingresses\/web\?namespace=monitoring&clusterId=cluster-a$/)
  expect(state.writes).toEqual([])
  verifyBrowserEvidence(evidence)
})

test('five Ingress tabs expose complete YAML-backed rules and TLS with scoped backend references', async ({
  context,
  page,
}) => {
  const { state, evidence } = await setup(context, page)
  await page.goto(route)
  await expect(page.getByRole('tab')).toHaveText(['概览', '规则与后端', '事件', '关联资源', 'YAML'])
  await expect(active(page)).toContainText('TLS 配置')
  await expect(active(page)).toContainText('tls-a')
  await expect(active(page)).toContainText('tls-b')
  await expect(active(page)).toContainText('默认后端')
  await expect(active(page)).toContainText('Bucket / files')
  await expect(active(page).getByRole('link', { name: 'nginx', exact: true })).toHaveAttribute(
    'href',
    /clusterId=cluster-a/,
  )
  expect(
    state.reads.some(
      (p) => p.includes('/events') || p.includes('/resources/graph') || p.includes('/metrics'),
    ),
  ).toBe(false)
  await capture(page, 'mock-overview')
  await page.getByRole('tab', { name: '规则与后端', exact: true }).click()
  await expect(active(page)).toContainText('2001:db8::1')
  await expect(
    active(page).getByRole('link', { name: 'api-worker', exact: true }).first(),
  ).toHaveAttribute('href', /clusterId=cluster-a&namespace=monitoring/)
  await expect(
    active(page).getByRole('link', { name: 'api', exact: true }).first(),
  ).toHaveAttribute('href', /namespace=monitoring&clusterId=cluster-a/)
  await expect(active(page)).toContainText('ImplementationSpecific')
  await expect(active(page)).toContainText('0 / 0')
  await capture(page, 'mock-rules')
  expect(state.writes).toEqual([])
  verifyBrowserEvidence(evidence)
})

test('Ingress events use a bounded precise namespace-kind-name timeline and never fabricate timestamps', async ({
  context,
  page,
}) => {
  expect(
    permissionCatalog.permissions.some((p) => p.key === 'platform.workloads.overview.view'),
  ).toBe(true)
  const { state, evidence } = await setup(context, page)
  await page.goto(route + '&tab=events')
  await expect(active(page)).toContainText('certificate pending')
  await expect(active(page)).toContainText('without timestamp')
  await expect(active(page)).toContainText('时间未知')
  await expect(active(page)).not.toContainText('SHOULD-NOT-RENDER')
  await expect(active(page)).not.toContainText('WRONG-NAMESPACE')
  await expect(
    active(page).getByText('当前命名空间最近 100 条事件中的匹配记录', { exact: true }),
  ).toHaveCount(0)
  await active(page).getByRole('button', { name: '事件范围说明', exact: true }).focus()
  await expect(page.getByRole('tooltip')).toContainText('最近 100 条')
  await active(page).getByPlaceholder('搜索原因、消息或对象').fill('certificate')
  await expect(active(page)).not.toContainText('without timestamp')
  state.eventFail = 503
  await active(page).getByRole('button', { name: '刷新 Ingress 事件', exact: true }).click()
  await expect(active(page)).toContainText('Ingress read failed')
  await expect(active(page)).toContainText('certificate pending')
  state.eventFail = 403
  await active(page).getByRole('button', { name: '刷新 Ingress 事件', exact: true }).click()
  await expect(active(page)).not.toContainText('certificate pending')
  verifyBrowserEvidence(evidence)
})

test('TLS certificates show public validity metadata without Secret data permission and preserve error boundaries', async ({
  context,
  page,
}) => {
  const { state, evidence } = await setup(context, page)
  await page.goto(route)
  const current = page.getByRole('region', { name: 'TLS 证书 tls-a', exact: true })
  const expired = page.getByRole('region', { name: 'TLS 证书 tls-b', exact: true })
  await expect(current).toContainText('有效期内')
  await expect(current).toContainText('自签名')
  await expect(current).toContainText(/剩余 \d+ 天/)
  await expect(current).toContainText('2027/02/01')
  await expect(expired).toContainText('已过期')
  expect(state.reads.filter((p) => p.includes('/certificate'))).toHaveLength(2)
  expect(state.reads.some((p) => /\/configuration\/secrets\/[^/]+\/(detail|yaml)/.test(p))).toBe(
    false,
  )
  await expect(current.getByRole('link')).toHaveCount(0)
  state.certificateFail = 503
  await page.getByRole('button', { name: '刷新 Ingress', exact: true }).click()
  await expect(current).toContainText('正在展示上次读取结果')
  await expect(current).toContainText('2027/02/01')
  state.certificateFail = 404
  await page.getByRole('button', { name: '刷新 Ingress', exact: true }).click()
  await expect(current).toContainText('证书信息读取失败')
  await expect(current).not.toContainText('2027/02/01')
  state.certificateFail = 403
  await page.getByRole('button', { name: '刷新 Ingress', exact: true }).click()
  await expect(current).toContainText('无访问权限')
  await expect(current).not.toContainText('2027/02/01')
  state.certificateFail = 0
  state.certificateMismatch = true
  await page.getByRole('button', { name: '刷新 Ingress', exact: true }).click()
  await expect(current).toContainText('Certificate identity does not match')
  await expect(current).not.toContainText('有效期内')
  verifyBrowserEvidence(evidence)
})

test('TLS invalid states and dark narrow cards stay readable', async ({ context, page }) => {
  const { state, evidence } = await setup(context, page)
  state.certificateStatus = 'invalid'
  await page.setViewportSize({ width: 900, height: 750 })
  await page.goto(route)
  await expect(page.getByRole('region', { name: 'TLS 证书 tls-a', exact: true })).toContainText(
    '证书无法解析',
  )
  await page.getByRole('button', { name: '切换到深色模式', exact: true }).click()
  await page.setViewportSize({ width: 375, height: 750 })
  const certificate = page.getByRole('region', { name: 'TLS 证书 tls-a', exact: true })
  await expect(certificate).toContainText('证书无法解析')
  const bounds = await certificate.boundingBox()
  expect(bounds!.width).toBeLessThanOrEqual(375)
  expect(state.writes).toEqual([])
  verifyBrowserEvidence(evidence)
})

test('TLS metadata reads deduplicate references and keep a bounded request budget', async ({
  context,
  page,
}) => {
  const { state, evidence } = await setup(context, page)
  state.manifest.spec.tls = Array.from({ length: 17 }, (_, index) => ({
    hosts: [
      index === 0
        ? `${'a'.repeat(60)}.${'b'.repeat(60)}.${'c'.repeat(60)}.test`
        : `host-${index}.example.test`,
    ],
    secretName: `tls-${index}`,
  }))
  state.manifest.spec.tls.push(state.manifest.spec.tls[0])
  state.yaml = stringify(state.manifest)
  await page.setViewportSize({ width: 375, height: 750 })
  await page.goto(route)
  await expect(active(page)).toContainText('本页最多 16 个 Secret')
  await expect.poll(() => state.reads.filter((p) => p.includes('/certificate')).length).toBe(16)
  expect(new Set(state.reads.filter((p) => p.includes('/certificate'))).size).toBe(16)
  const card = page.getByRole('region', { name: 'TLS 证书 tls-0', exact: true }).first()
  await expect(card).toContainText('到期时间')
  expect((await card.boundingBox())!.width).toBeLessThanOrEqual(375)
  expect(state.reads.some((p) => p.includes('/tls-16/certificate'))).toBe(false)
  verifyBrowserEvidence(evidence)
})

test('Ingress relationships share graph and list, retain zoom and respect dark narrow layout', async ({
  context,
  page,
}) => {
  const { state, evidence } = await setup(context, page)
  await page.goto(route + '&tab=relationships')
  await expect(active(page).locator('.react-flow__node')).toHaveCount(3)
  const graphPod = active(page).getByRole('button', { name: 'Pod monitoring/api-worker' })
  await expect(graphPod).toBeVisible()
  await expect(active(page)).toContainText('声明路由后端')
  await capture(page, 'mock-relationships')
  const viewport = active(page).locator('.react-flow__viewport')
  const initialStyle = await viewport.getAttribute('style')
  await active(page).getByRole('button', { name: 'Zoom Out', exact: true }).click()
  await expect(viewport).not.toHaveAttribute('style', initialStyle || '')
  let style: string | null = null
  await expect
    .poll(
      async () => {
        const next = await viewport.getAttribute('style')
        const equal = next === style
        style = next
        return equal
      },
      { intervals: [250] },
    )
    .toBe(true)
  await active(page).getByText('列表', { exact: true }).click()
  await expect(active(page).getByRole('button', { name: 'api-worker', exact: true })).toBeVisible()
  await active(page).getByText('关系图', { exact: true }).click()
  await expect(viewport).toHaveAttribute('style', style || '')
  for (const width of [1440, 900, 375]) {
    await page.setViewportSize({ width, height: 900 })
    await expect
      .poll(() => page.evaluate(() => document.documentElement.scrollWidth <= innerWidth + 1))
      .toBe(true)
  }
  await page.getByRole('button', { name: '切换到深色模式' }).click()
  await expect(
    active(page).locator('.soha-resource-graph-kind').filter({ hasText: 'Ingress' }),
  ).toBeVisible()
  await active(page).getByText('列表', { exact: true }).click()
  await active(page).getByRole('button', { name: 'api-worker', exact: true }).click()
  const inspector = active(page).getByRole('complementary', { name: '选中资源信息' })
  await expect(inspector).toContainText('monitoring')
  await inspector.getByRole('button', { name: '查看详情', exact: true }).click()
  await expect(page).toHaveURL(
    /\/workloads\/pods\/api-worker\?clusterId=cluster-a&namespace=monitoring$/,
  )
  await expect(page.getByRole('heading', { name: 'api-worker', exact: true })).toBeVisible()
  expect(state.reads.filter((p) => p.includes('/resources/graph'))).toEqual([
    root + '/resources/graph?namespace=monitoring&kind=Ingress&name=web',
  ])
  verifyBrowserEvidence(evidence)
})

test('multi-rule editor retains source associations and requires preflight before a conflict-preserving update', async ({
  context,
  page,
}) => {
  const { state, evidence } = await setup(context, page)
  await page.goto(route)
  await page.getByRole('button', { name: '编辑 Ingress', exact: true }).click()
  const editor = page.getByRole('dialog', { name: '编辑 Ingress · web', exact: true })
  await expect(editor.getByLabel('规则 Host')).toHaveCount(2)
  await expect(editor.getByLabel('规则 Host').first()).toBeVisible()
  await capture(page, 'mock-editor')
  await editor
    .locator('.soha-ingress-editor-rule')
    .first()
    .getByRole('button', { name: '删除路径', exact: true })
    .first()
    .click()
  await expect(editor).toContainText('Bucket/files')
  const second = editor.locator('.soha-ingress-editor-rule').nth(1)
  await second.getByLabel('后端端口').fill('8081')
  await second.getByRole('button', { name: /添加路径$/ }).click()
  await second.getByLabel('后端 Service').nth(1).fill('new-api')
  await second.getByLabel('后端端口').nth(1).fill('https')
  for (const width of [1440, 900, 375]) {
    await page.setViewportSize({ width, height: 650 })
    await expect(editor.getByRole('button', { name: '预检更改', exact: true })).toBeInViewport()
  }
  await page.setViewportSize({ width: 1440, height: 900 })
  expect(state.writes).toEqual([])
  const request = page.waitForRequest((r) => r.url().includes('/resources/update-plan'))
  await editor.getByRole('button', { name: '预检更改', exact: true }).click()
  const body = (await request).postDataJSON() as { content: string }
  expect(parse(body.content)).toMatchObject({
    metadata: state.manifest.metadata,
    status: state.manifest.status,
    spec: {
      extension: { preserved: true },
      defaultBackend: state.manifest.spec.defaultBackend,
      rules: [
        {
          host: 'a.example.test',
          extension: 1,
          http: {
            paths: [
              { extension: 'second', backend: { resource: { kind: 'Bucket', name: 'files' } } },
            ],
          },
        },
        {
          host: 'b.example.test',
          http: {
            paths: [
              { backend: { service: { name: 'web', port: { number: 8081 } } } },
              { backend: { service: { name: 'new-api', port: { name: 'https' } } } },
            ],
          },
        },
      ],
      tls: state.manifest.spec.tls,
    },
  })
  const plan = page.getByRole('dialog', { name: '确认资源变更', exact: true })
  await expect(plan).toBeVisible()
  expect(state.writes).toEqual(['POST ' + root + '/resources/update-plan'])
  await expect(second.getByLabel('后端端口').first()).toBeDisabled()
  await plan.getByRole('button', { name: '确认更新', exact: true }).click()
  await expect(
    page.getByText('Ingress resourceVersion changed', { exact: false }).first(),
  ).toBeVisible()
  await plan.getByRole('button', { name: /取\s*消/ }).click()
  await expect(second.getByLabel('后端端口').first()).toHaveValue('8081')
  await editor.getByRole('button', { name: /取\s*消/ }).click()
  expect(state.writes).toEqual([
    'POST ' + root + '/resources/update-plan',
    'PUT ' + root + '/network/ingresses/web/yaml',
  ])
  verifyBrowserEvidence(evidence)
})

test('view-only Ingress keeps references readable but cannot navigate denied targets, edit YAML or read events', async ({
  context,
  page,
}) => {
  const { state, evidence } = await setup(context, page, { readonly: true, noRefs: true })
  await page.goto(route)
  await expect(page.getByRole('button', { name: '编辑 Ingress', exact: true })).toHaveCount(0)
  await expect(page.getByRole('button', { name: '删除 Ingress', exact: true })).toHaveCount(0)
  await expect(active(page)).toContainText('tls-a')
  await expect(active(page).getByRole('link', { name: 'tls-a', exact: true })).toHaveCount(0)
  await expect(active(page)).toContainText('无 Service 查看权限')
  await expect(active(page)).toContainText('无证书信息查看权限')
  expect(state.reads.some((p) => p.includes('/certificate'))).toBe(false)
  await page.getByRole('tab', { name: '事件', exact: true }).click()
  await expect(active(page)).toContainText('无事件查看权限')
  expect(state.reads.some((p) => p.includes('/events'))).toBe(false)
  await page.getByRole('tab', { name: 'YAML', exact: true }).click()
  await expect(active(page).getByRole('button', { name: /应\s*用|Apply/ })).toBeDisabled()
  expect(state.writes).toEqual([])
  verifyBrowserEvidence(evidence)
})

test('Ingress refresh retains usable values on 503 and hides protected content on 403', async ({
  context,
  page,
}) => {
  const { state, evidence } = await setup(context, page)
  await page.goto(route)
  await expect(active(page)).toContainText('a.example.test')
  state.fail = 503
  await page.getByRole('button', { name: '刷新 Ingress', exact: true }).click()
  await expect(page.getByRole('switch', { name: /刷新失败/ })).toBeVisible()
  await expect(active(page)).toContainText('a.example.test')
  state.fail = 403
  await page.getByRole('button', { name: '刷新 Ingress', exact: true }).click()
  await expect(page.getByRole('heading', { name: 'web', exact: true })).toHaveCount(0)
  expect(state.writes).toEqual([])
  verifyBrowserEvidence(evidence)
})

test('Ingress YAML draft and scope survive tabs and refresh; deletion confirms the exact target', async ({
  context,
  page,
}) => {
  const { state, evidence } = await setup(context, page)
  await page.goto(route + '&tab=yaml')
  const editor = page.getByRole('textbox', { name: 'YAML 草稿编辑器', exact: true })
  await expect(page.locator('.soha-k8s-yaml-editor .view-lines:visible').first()).toContainText(
    'Ingress',
  )
  await editor.press('ControlOrMeta+Home')
  await editor.pressSequentially('# retained draft')
  await editor.press('Enter')
  await page.getByRole('tab', { name: '规则与后端', exact: true }).click()
  await page.getByRole('button', { name: '刷新 Ingress', exact: true }).click()
  await page.getByRole('tab', { name: 'YAML', exact: true }).click()
  await expect(page.locator('.soha-k8s-yaml-editor .view-lines:visible').first()).toContainText(
    'retained draft',
  )
  await page.getByRole('button', { name: '删除 Ingress', exact: true }).click()
  const confirm = page.getByRole('dialog')
  await expect(confirm).toContainText('monitoring / web')
  await confirm.getByRole('button', { name: /取\s*消/ }).click()
  expect(state.writes).toEqual([])
  await page.getByRole('button', { name: '删除 Ingress', exact: true }).click()
  await page.getByRole('dialog').getByRole('button', { name: '删除', exact: true }).click()
  await expect(page).toHaveURL(/network\/ingresses\?clusterId=cluster-a&namespace=monitoring$/)
  expect(state.writes).toEqual(['DELETE ' + root + '/network/ingresses/web'])
  verifyBrowserEvidence(evidence)
})

test('unreadable configuration and missing enrichment remain unknown rather than false zero or no TLS', async ({
  context,
  page,
}) => {
  const { state, evidence } = await setup(context, page)
  state.yamlFail = 403
  state.ingress.backends = undefined
  await page.goto(route)
  await expect(active(page)).toContainText('TLS 引用未确认')
  await expect(active(page)).not.toContainText('未配置 TLS')
  await page.getByRole('tab', { name: '规则与后端', exact: true }).click()
  await expect(active(page)).toContainText('未返回后端数据')
  await expect(active(page)).toContainText('— / —')
  await expect(active(page)).not.toContainText('0 / 0')
  expect(state.writes).toEqual([])
  verifyBrowserEvidence(evidence)
})
