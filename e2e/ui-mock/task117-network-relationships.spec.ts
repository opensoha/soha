import { test, expect, type BrowserContext, type Page } from '@playwright/test'
import type { KubernetesResourceGraph } from '@opensoha/contracts/gen/ts/sohaapi'
import { baselineResponse } from '../fixtures/baseline-data'
import { installMocks, verifyBrowserEvidence } from '../fixtures/mock-app'

const clusterId = 'cluster-a'
const namespace = 'monitoring'
const name = 'relationship-sample'
const apiRoot = `/api/v1/clusters/${clusterId}`
const roots = [
  {
    kind: 'Service',
    collection: 'services',
    route: '/network/services',
    version: 'v1',
    scope: 'namespace',
    permission: 'platform.network.services.view',
  },
  {
    kind: 'Ingress',
    collection: 'ingresses',
    route: '/network/ingresses',
    version: 'networking.k8s.io/v1',
    scope: 'namespace',
    permission: 'platform.network.ingresses.view',
  },
  {
    kind: 'IngressClass',
    collection: 'ingressclasses',
    route: '/network/ingressclasses',
    version: 'networking.k8s.io/v1',
    scope: 'cluster',
    permission: 'platform.network.ingress-classes.view',
  },
  {
    kind: 'GatewayClass',
    collection: 'gatewayclasses',
    route: '/network/gateway-api/gatewayclasses',
    version: 'gateway.networking.k8s.io/v1',
    scope: 'cluster',
    permission: 'platform.network.gateway-classes.view',
  },
  {
    kind: 'Gateway',
    collection: 'gateways',
    route: '/network/gateway-api/gateways',
    version: 'gateway.networking.k8s.io/v1',
    scope: 'namespace',
    permission: 'platform.network.gateways.view',
  },
  {
    kind: 'HTTPRoute',
    collection: 'httproutes',
    route: '/network/gateway-api/httproutes',
    version: 'gateway.networking.k8s.io/v1',
    scope: 'namespace',
    permission: 'platform.network.http-routes.view',
  },
  {
    kind: 'GRPCRoute',
    collection: 'grpcroutes',
    route: '/network/gateway-api/grpcroutes',
    version: 'gateway.networking.k8s.io/v1',
    scope: 'namespace',
    permission: 'platform.network.grpc-routes.view',
  },
  {
    kind: 'BackendTLSPolicy',
    collection: 'backendtlspolicies',
    route: '/network/gateway-api/backendtlspolicies',
    version: 'gateway.networking.k8s.io/v1',
    scope: 'namespace',
    permission: 'platform.network.backend-tls-policies.view',
  },
  {
    kind: 'ReferenceGrant',
    collection: 'referencegrants',
    route: '/network/gateway-api/referencegrants',
    version: 'gateway.networking.k8s.io/v1',
    scope: 'namespace',
    permission: 'platform.network.reference-grants.view',
  },
  {
    kind: 'EndpointSlice',
    collection: 'endpointslices',
    route: '/network/endpointslices',
    version: 'discovery.k8s.io/v1',
    scope: 'namespace',
    permission: 'platform.network.endpoint-slices.view',
  },
  {
    kind: 'NetworkPolicy',
    collection: 'networkpolicies',
    route: '/network/networkpolicies',
    version: 'networking.k8s.io/v1',
    scope: 'namespace',
    permission: 'platform.network.network-policies.view',
  },
] as const

type Root = (typeof roots)[number]
const scopedRoute = (root: Root) =>
  `${root.route}/${name}?clusterId=${clusterId}${root.scope === 'namespace' ? `&namespace=${namespace}` : ''}`
const activePanel = (page: Page) => page.getByRole('tabpanel').filter({ visible: true })
const ref = (
  kind: string,
  resourceName: string,
  resourceNamespace = namespace,
): KubernetesResourceGraph['nodes'][number]['resource'] => ({
  clusterId,
  apiVersion: kind === 'Pod' ? 'v1' : kind === 'Service' ? 'v1' : 'gateway.networking.k8s.io/v1',
  kind,
  name: resourceName,
  ...(resourceNamespace ? { namespace: resourceNamespace } : {}),
  scopeMode: resourceNamespace ? ('namespace' as const) : ('cluster' as const),
})

function graphFor(root: Root): KubernetesResourceGraph {
  const rootRef = ref(root.kind, name, root.scope === 'namespace' ? namespace : '')
  rootRef.apiVersion = root.version
  return {
    clusterId,
    ...(root.scope === 'namespace' ? { namespace } : {}),
    rootId: 'root',
    generatedAt: '2026-11-05T00:00:00Z',
    nodes: [
      { id: 'root', status: 'resolved', resource: rootRef },
      { id: 'service', status: 'resolved', health: 'ready', resource: ref('Service', 'backend') },
      { id: 'pod', status: 'resolved', health: 'Running', resource: ref('Pod', 'backend-0') },
      {
        id: 'unknown',
        status: 'unresolved',
        resource: ref('HTTPRoute', 'cross-namespace', 'other'),
      },
    ],
    edges: [
      {
        id: 'backend-edge',
        sourceId: 'root',
        targetId: 'service',
        relation: 'routes-to',
        protocol: 'HTTP',
        port: '8080',
        sectionName: 'web',
        fieldPath: 'spec.rules[0].backendRefs[0]',
        sourceRef: ref('HTTPRoute', 'declaring-route'),
        note: 'Configured backend reference',
      },
      {
        id: 'pod-edge',
        sourceId: 'service',
        targetId: 'pod',
        relation: 'selects',
        protocol: 'TCP',
        port: '8080',
      },
      {
        id: 'unknown-edge',
        sourceId: 'root',
        targetId: 'unknown',
        relation: 'routes-to',
        resolutionState: 'unresolved',
      },
    ],
    evidence: [],
    warnings: [],
  }
}

async function setup(context: BrowserContext, page: Page, root: Root) {
  const graph = graphFor(root)
  const state = { status: 200, graphReads: [] as string[], writes: [] as string[] }
  await context.addInitScript(
    ({ selectedCluster, selectedNamespace }) => {
      localStorage.setItem(
        'soha-scope',
        JSON.stringify({
          state: { clusterId: selectedCluster, namespace: selectedNamespace },
          version: 0,
        }),
      )
    },
    { selectedCluster: clusterId, selectedNamespace: namespace },
  )
  const evidence = await installMocks(context, page, (url, method) => {
    if (url.pathname === '/api/v1/access/permission-snapshot') {
      const body = baselineResponse(url.pathname)!
      if (!('permissionKeys' in body.data)) throw new Error('Missing permission fixture')
      body.data.permissionKeys = [
        ...(body.data.permissionKeys || []),
        ...roots.map((entry) => entry.permission),
        'platform.pods.view',
      ]
      return { body }
    }
    if (method !== 'GET' && url.pathname.startsWith(`${apiRoot}/`)) {
      if (url.pathname !== '/api/v1/auth/refresh') state.writes.push(`${method} ${url.pathname}`)
      return undefined
    }
    const graphPath = `${apiRoot}/resources/graph`
    if (url.pathname === graphPath && method === 'GET') {
      state.graphReads.push(url.search)
      const correct =
        url.searchParams.get('kind') === root.kind &&
        url.searchParams.get('name') === name &&
        url.searchParams.get('namespace') === (root.scope === 'namespace' ? namespace : null)
      if (!correct) {
        evidence.missing.push(`unexpected graph identity: ${url.search}`)
        return {
          status: 400,
          body: { error: { code: 'bad_identity', message: 'Unexpected graph identity' } },
        }
      }
      return state.status === 200
        ? { body: { data: graph } }
        : {
            status: state.status,
            body: {
              error: {
                code: state.status === 403 ? 'forbidden' : 'synthetic_failure',
                message: 'Relationship read failed',
              },
            },
          }
    }
    const itemPath = `${apiRoot}/network/${root.collection}/${name}`
    const correctScope =
      url.searchParams.get('namespace') === (root.scope === 'namespace' ? namespace : null)
    if (method === 'GET' && url.pathname === `${itemPath}/detail`) {
      if (!correctScope) {
        evidence.missing.push(`incorrect detail scope: ${url.pathname}${url.search}`)
        return {
          status: 400,
          body: { error: { code: 'incorrect_scope', message: 'Incorrect detail scope' } },
        }
      }
      return {
        body: {
          data: {
            name,
            ...(root.scope === 'namespace' ? { namespace } : {}),
            ageSeconds: 60,
            allowedActions: ['view'],
            labels: {},
            annotations: {},
            ports: [],
            endpoints: [],
            backendPods: [],
            controllerName: 'example.io/controller',
          },
        },
      }
    }
    if (method === 'GET' && url.pathname === `${itemPath}/yaml`) {
      if (!correctScope) {
        evidence.missing.push(`incorrect YAML scope: ${url.pathname}${url.search}`)
        return {
          status: 400,
          body: { error: { code: 'incorrect_scope', message: 'Incorrect YAML scope' } },
        }
      }
      return {
        body: {
          data: {
            content: `apiVersion: ${root.version}\nkind: ${root.kind}\nmetadata: {name: ${name}}\n`,
            resourceVersion: '1',
          },
        },
      }
    }
    if (
      method === 'GET' &&
      url.pathname === `${apiRoot}/network/httproutes/declaring-route/detail` &&
      url.searchParams.get('namespace') === namespace
    ) {
      return {
        body: {
          data: {
            name: 'declaring-route',
            namespace,
            ageSeconds: 60,
            allowedActions: ['view'],
            labels: {},
            annotations: {},
            hostnames: [],
            parentRefs: [],
            rules: [],
          },
        },
      }
    }
    if (
      method === 'GET' &&
      url.pathname === `${apiRoot}/network/httproutes/declaring-route/yaml` &&
      url.searchParams.get('namespace') === namespace
    ) {
      return {
        body: {
          data: {
            content: `apiVersion: gateway.networking.k8s.io/v1\nkind: HTTPRoute\nmetadata: {name: declaring-route, namespace: ${namespace}}\n`,
            resourceVersion: '1',
          },
        },
      }
    }
    return undefined
  })
  return { state, evidence }
}

for (const root of roots) {
  test(`${root.kind} requests its exact scoped graph only when Related resources is active`, async ({
    context,
    page,
  }) => {
    const fixture = await setup(context, page, root)
    await page.goto(scopedRoute(root))
    await expect(page.getByRole('heading', { name, exact: true })).toBeVisible()
    expect(fixture.state.graphReads).toEqual([])
    await page.getByRole('tab', { name: '关联资源', exact: true }).click()
    await expect(activePanel(page).locator('.react-flow__node[data-id="root"]')).toBeVisible()
    await expect.poll(() => fixture.state.graphReads.length).toBe(1)
    const search = new URLSearchParams(fixture.state.graphReads[0])
    expect(search.get('kind')).toBe(root.kind)
    expect(search.get('name')).toBe(name)
    expect(search.get('namespace')).toBe(root.scope === 'namespace' ? namespace : null)
    expect(fixture.state.writes).toEqual([])
    verifyBrowserEvidence(fixture.evidence)
  })
}

test('network edge facts, source navigation, unresolved cross-namespace reference, and Escape focus', async ({
  context,
  page,
}) => {
  const root = roots[0]
  const fixture = await setup(context, page, root)
  await page.goto(scopedRoute(root))
  await page.getByRole('tab', { name: '关联资源', exact: true }).click()
  const panel = activePanel(page)
  const service = panel.locator('.react-flow__node[data-id="service"]')
  await expect(service).toBeVisible()
  await service.click()
  const inspector = panel.getByRole('complementary', { name: '选中资源信息' })
  await expect(inspector).toBeFocused()
  await expect(inspector).toContainText('HTTP')
  await expect(inspector).toContainText('8080')
  await expect(inspector).toContainText('web')
  await expect(inspector).toContainText('spec.rules[0].backendRefs[0]')
  await expect(inspector).toContainText('声明来源')
  await expect(inspector.getByRole('button', { name: /HTTPRoute.*declaring-route/ })).toBeEnabled()
  const unknown = panel.locator('.react-flow__node[data-id="unknown"]')
  await unknown.click()
  await expect(inspector).toContainText('cross-namespace')
  await expect(inspector.getByRole('button', { name: '查看详情', exact: true })).toBeDisabled()
  await expect(inspector).toContainText('引用未解析、无查看权限或暂无详情入口')
  await page.keyboard.press('Escape')
  await expect(inspector).toHaveCount(0)
  await expect(unknown).toBeFocused()
  expect(fixture.state.writes).toEqual([])
  verifyBrowserEvidence(fixture.evidence)
})

test('declared source reference navigates to its exact namespace-scoped route', async ({
  context,
  page,
}) => {
  const fixture = await setup(context, page, roots[0])
  await page.goto(scopedRoute(roots[0]))
  await page.getByRole('tab', { name: '关联资源', exact: true }).click()
  const panel = activePanel(page)
  const service = panel.locator('.react-flow__node[data-id="service"]')
  await expect(service).toBeVisible()
  await service.click()
  await panel
    .getByRole('complementary', { name: '选中资源信息' })
    .getByRole('button', { name: /HTTPRoute.*declaring-route/ })
    .click()
  await expect(page).toHaveURL(
    /\/network\/gateway-api\/httproutes\/declaring-route\?namespace=monitoring&clusterId=cluster-a$/,
  )
  expect(fixture.state.writes).toEqual([])
  verifyBrowserEvidence(fixture.evidence)
})

test('failed graph refresh retains facts; forbidden refresh clears graph and inspector', async ({
  context,
  page,
}) => {
  const fixture = await setup(context, page, roots[0])
  await page.goto(scopedRoute(roots[0]))
  await page.getByRole('tab', { name: '关联资源', exact: true }).click()
  const panel = activePanel(page)
  const service = panel.locator('.react-flow__node[data-id="service"]')
  await expect(service).toBeVisible()
  await service.click()
  const inspector = panel.getByRole('complementary', { name: '选中资源信息' })
  fixture.state.status = 500
  await panel.getByRole('button', { name: '刷新资源关系', exact: true }).click()
  await expect(panel.getByRole('alert')).toContainText('正在展示上次读取结果', { timeout: 15_000 })
  await expect(inspector).toContainText('8080')
  fixture.state.status = 403
  await panel.getByRole('button', { name: '刷新资源关系', exact: true }).click()
  await expect(panel.getByRole('alert')).toContainText('资源关系无访问权限')
  await expect(inspector).toHaveCount(0)
  await expect(panel.locator('.react-flow__node')).toHaveCount(0)
  expect(fixture.state.writes).toEqual([])
  verifyBrowserEvidence(fixture.evidence)
})
