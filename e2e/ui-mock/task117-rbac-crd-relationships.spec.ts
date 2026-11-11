import { test, expect, type BrowserContext, type Page } from '@playwright/test'
import type { KubernetesResourceGraph } from '@opensoha/contracts/gen/ts/sohaapi'
import { baselineResponse } from '../fixtures/baseline-data'
import { installMocks, verifyBrowserEvidence } from '../fixtures/mock-app'

const clusterId = 'cluster-a'
const namespace = 'monitoring'
const rootName = 'rbac-sample'
const apiRoot = `/api/v1/clusters/${clusterId}`
const rbacRoots = [
  {
    route: 'serviceaccounts',
    kind: 'ServiceAccount',
    namespaced: true,
    permission: 'service-accounts',
  },
  { route: 'clusterroles', kind: 'ClusterRole', namespaced: false, permission: 'cluster-roles' },
  { route: 'roles', kind: 'Role', namespaced: true, permission: 'roles' },
  {
    route: 'clusterrolebindings',
    kind: 'ClusterRoleBinding',
    namespaced: false,
    permission: 'cluster-role-bindings',
  },
  { route: 'rolebindings', kind: 'RoleBinding', namespaced: true, permission: 'role-bindings' },
] as const

type RBACRoot = (typeof rbacRoots)[number]
const apiVersion = (root: RBACRoot) =>
  root.kind === 'ServiceAccount' ? 'v1' : 'rbac.authorization.k8s.io/v1'
const rootRoute = (root: RBACRoot) =>
  `/platform-access-control/${root.route}/${rootName}?clusterId=${clusterId}${root.namespaced ? `&namespace=${namespace}` : ''}`

function rbacGraph(root: RBACRoot): KubernetesResourceGraph {
  const resource = (kind: string, name: string, ns = namespace) => ({
    clusterId,
    ...(ns ? { namespace: ns } : {}),
    apiVersion: kind === 'ServiceAccount' ? 'v1' : 'rbac.authorization.k8s.io/v1',
    scopeMode: ns ? ('namespace' as const) : ('cluster' as const),
    kind,
    name,
    uid: `${kind}-${name}-uid`,
  })
  const nodes: KubernetesResourceGraph['nodes'] = [
    {
      id: 'root',
      status: 'resolved',
      resource: resource(root.kind, rootName, root.namespaced ? namespace : ''),
    },
  ]
  const edges: KubernetesResourceGraph['edges'] = []
  if (root.kind === 'RoleBinding' || root.kind === 'ClusterRoleBinding') {
    nodes.push(
      { id: 'role', status: 'resolved', resource: resource('ClusterRole', 'reader', '') },
      { id: 'sa', status: 'resolved', resource: resource('ServiceAccount', 'api') },
      { id: 'other-sa', status: 'resolved', resource: resource('ServiceAccount', 'api', 'other') },
    )
    edges.push(
      {
        id: 'role-ref',
        sourceId: 'root',
        targetId: 'role',
        relation: 'binds-role',
        fieldPath: 'roleRef',
      },
      {
        id: 'subject',
        sourceId: 'root',
        targetId: 'sa',
        relation: 'binds-subject',
        fieldPath: 'subjects[0]',
      },
      {
        id: 'other-subject',
        sourceId: 'root',
        targetId: 'other-sa',
        relation: 'binds-subject',
        fieldPath: 'subjects[1]',
      },
    )
  }
  return {
    rootId: 'root',
    clusterId,
    ...(root.namespaced ? { namespace } : {}),
    generatedAt: '2026-11-05T00:00:00Z',
    evidence: [],
    warnings: [],
    nodes,
    edges,
  }
}

async function setupRBAC(context: BrowserContext, page: Page, root: RBACRoot) {
  const state = { graphReads: [] as string[], writes: [] as string[], graphStatus: 200 }
  await context.addInitScript(
    ({ selectedCluster, selectedNamespace }) =>
      localStorage.setItem(
        'soha-scope',
        JSON.stringify({
          state: { clusterId: selectedCluster, namespace: selectedNamespace },
          version: 0,
        }),
      ),
    { selectedCluster: clusterId, selectedNamespace: namespace },
  )
  const evidence = await installMocks(context, page, (url, method) => {
    if (method !== 'GET' && url.pathname !== '/api/v1/auth/refresh') {
      state.writes.push(`${method} ${url.pathname}${url.search}`)
      return undefined
    }
    if (url.pathname === '/api/v1/access/permission-snapshot') {
      const body = baselineResponse(url.pathname)!
      if (!body.data || !('permissionKeys' in body.data))
        throw new Error('Missing permission snapshot')
      body.data.permissionKeys!.push(
        ...rbacRoots.map((entry) => `platform.access-control.${entry.permission}.view`),
      )
      body.data.visibleMenuIds = [
        ...(body.data.visibleMenuIds ?? []),
        'platform-access-control',
        ...rbacRoots.map((entry) => `platform-access-control-${entry.route}`),
      ]
      return { body }
    }
    if (
      method === 'GET' &&
      url.pathname === `${apiRoot}/access-control/serviceaccounts/api/detail` &&
      url.searchParams.get('namespace') === namespace
    )
      return {
        body: {
          data: {
            name: 'api',
            namespace,
            ageSeconds: 60,
            allowedActions: ['view'],
            automountServiceAccountToken: false,
            labels: {},
            annotations: {},
          },
        },
      }
    if (
      method === 'GET' &&
      url.pathname === `${apiRoot}/access-control/serviceaccounts/api/yaml` &&
      url.searchParams.get('namespace') === namespace
    )
      return {
        body: {
          data: {
            content: `apiVersion: v1\nkind: ServiceAccount\nmetadata: {name: api, namespace: ${namespace}}\n`,
          },
        },
      }
    if (url.pathname === `${apiRoot}/resources/graph`) {
      if (method !== 'GET') {
        state.writes.push(`${method} ${url.pathname}${url.search}`)
        return undefined
      }
      state.graphReads.push(url.search)
      const expectedNamespace = root.namespaced ? namespace : null
      if (
        url.searchParams.get('kind') !== root.kind ||
        url.searchParams.get('name') !== rootName ||
        url.searchParams.get('namespace') !== expectedNamespace
      ) {
        evidence.missing.push(`unexpected RBAC graph identity: ${url.search}`)
        return {
          status: 400,
          body: { error: { code: 'bad_identity', message: 'Unexpected RBAC root' } },
        }
      }
      return state.graphStatus === 200
        ? { body: { data: rbacGraph(root) } }
        : {
            status: state.graphStatus,
            body: { error: { code: 'forbidden', message: 'Relationship access denied' } },
          }
    }
    const match = new RegExp(
      `^${apiRoot}/access-control/${root.route}/${rootName}(?:/(detail|yaml))?$`,
    ).exec(url.pathname)
    if (!match) return undefined
    if (method !== 'GET') {
      state.writes.push(`${method} ${url.pathname}${url.search}`)
      return undefined
    }
    const requestedNamespace = url.searchParams.get('namespace')
    if (requestedNamespace !== (root.namespaced ? namespace : null)) {
      evidence.missing.push(`unexpected RBAC scope: ${url.pathname}${url.search}`)
      return {
        status: 400,
        body: { error: { code: 'bad_scope', message: 'Unexpected RBAC scope' } },
      }
    }
    if (match[1] === 'yaml')
      return {
        body: {
          data: {
            content:
              root.kind === 'RoleBinding'
                ? `apiVersion: rbac.authorization.k8s.io/v1\nkind: RoleBinding\nmetadata: {name: ${rootName}, namespace: ${namespace}}\nroleRef: {apiGroup: rbac.authorization.k8s.io, kind: ClusterRole, name: reader}\nsubjects:\n  - {kind: ServiceAccount, name: api, namespace: ${namespace}}\n`
                : `apiVersion: ${apiVersion(root)}\nkind: ${root.kind}\nmetadata: {name: ${rootName}}\n`,
          },
        },
      }
    return {
      body: {
        data: {
          name: rootName,
          ...(root.namespaced ? { namespace } : {}),
          ageSeconds: 60,
          allowedActions: ['view'],
          rules: [],
          subjects: [],
          roleRef: 'ClusterRole/reader',
          labels: {},
          annotations: {},
        },
      },
    }
  })
  return { evidence, state }
}

for (const root of rbacRoots) {
  test(`${root.kind} relationship root uses its exact GVK, scope and lazy read`, async ({
    context,
    page,
  }) => {
    const fixture = await setupRBAC(context, page, root)
    await page.goto(rootRoute(root))
    await expect(page.getByRole('heading', { name: rootName, exact: true })).toBeVisible()
    expect(fixture.state.graphReads).toEqual([])
    await page.getByRole('tab', { name: '关系', exact: true }).click()
    await expect(page).toHaveURL(/tab=relationships/)
    await expect(page.locator('.react-flow__node[data-id="root"]')).toBeVisible()
    expect(fixture.state.graphReads).toHaveLength(1)
    const query = new URLSearchParams(fixture.state.graphReads[0])
    expect(query.get('kind')).toBe(root.kind)
    expect(query.get('name')).toBe(rootName)
    expect(query.get('namespace')).toBe(root.namespaced ? namespace : null)
    expect(fixture.state.writes).toEqual([])
    verifyBrowserEvidence(fixture.evidence)
  })
}

test('RoleBinding declared roleRef and namespaced ServiceAccount subject navigate only to exact namespace', async ({
  context,
  page,
}) => {
  const root = rbacRoots.find((item) => item.kind === 'RoleBinding')!
  const fixture = await setupRBAC(context, page, root)
  await page.goto(rootRoute(root) + '&tab=relationships')
  const panel = page.getByRole('tabpanel', { name: '关系', exact: true })
  await expect(panel.locator('.react-flow__node[data-id="role"]')).toBeVisible()
  await panel
    .getByRole('radiogroup', { name: '关联资源视图' })
    .getByText('列表', { exact: true })
    .click()
  const roleRow = panel.getByRole('row').filter({ hasText: 'ClusterRole reader' })
  await expect(roleRow).toContainText('roleRef')
  const subjectRow = panel.getByRole('row').filter({ hasText: 'ServiceAccount monitoring/api' })
  await expect(subjectRow).toContainText('subjects[0]')
  await panel.locator('button[data-resource-id="sa"]').click()
  const inspector = panel.getByRole('complementary', { name: '选中资源信息' })
  await expect(inspector.getByText('ServiceAccount', { exact: true })).toBeVisible()
  await expect(inspector.getByText('api', { exact: true })).toBeVisible()
  await expect(inspector.locator('dd').nth(0)).toHaveText('monitoring')
  await expect(inspector.locator('dd').nth(1)).toHaveText('v1')
  const details = inspector.getByRole('button', { name: '查看详情', exact: true })
  await expect(details).toBeEnabled()
  await details.click()
  await expect(page).toHaveURL(
    `/platform-access-control/serviceaccounts/api?namespace=${namespace}&clusterId=${clusterId}`,
  )
  await expect(page.getByRole('heading', { name: 'api', exact: true })).toBeVisible()
  expect(fixture.state.writes).toEqual([])
  verifyBrowserEvidence(fixture.evidence)
})

test('stale RBAC graph permission returns a visible 403 state without fallback or writes', async ({
  context,
  page,
}) => {
  const root = rbacRoots.find((item) => item.kind === 'RoleBinding')!
  const fixture = await setupRBAC(context, page, root)
  fixture.state.graphStatus = 403
  await page.goto(rootRoute(root) + '&tab=relationships')
  await expect(page.getByRole('alert')).toContainText('Relationship access denied')
  expect(fixture.state.graphReads).toHaveLength(1)
  expect(fixture.state.writes).toEqual([])
  verifyBrowserEvidence(fixture.evidence)
})

const crd = {
  uid: 'synthetic-widgets-crd-uid',
  name: 'widgets.quality.example',
  group: 'quality.example',
  kind: 'Widget',
  plural: 'widgets',
  version: 'v1',
  versions: ['v1', 'v2'],
  scope: 'Namespaced',
  allowedActions: ['get', 'list'],
}
const crdName = 'sample-widget'
const crdGroupRoute = `/extensions/apis/${crd.group}`
const crdListRoute = `${crdGroupRoute}?clusterId=${clusterId}&kind=${crd.name}&namespace=&tab=instances`

test('CRD definition relationship root uses apiextensions GVK and exact cluster scope', async ({
  context,
  page,
}) => {
  const graphReads: string[] = []
  const writes: string[] = []
  const evidence = await installMocks(context, page, (url, method) => {
    if (method !== 'GET' && url.pathname !== '/api/v1/auth/refresh') {
      writes.push(`${method} ${url.pathname}${url.search}`)
      return undefined
    }
    if (url.pathname === '/api/v1/access/permission-snapshot') {
      const body = baselineResponse(url.pathname)!
      if (!body.data || !('permissionKeys' in body.data))
        throw new Error('Missing permission snapshot')
      body.data.permissionKeys!.push('platform.extensions.view')
      body.data.visibleMenuIds = [...(body.data.visibleMenuIds ?? []), 'extensions']
      return { body }
    }
    if (url.pathname === '/api/v1/clusters/capabilities' && method === 'GET')
      return {
        body: {
          data: [
            {
              key: 'custom.resources',
              direct: { status: 'available' },
              agent: { status: 'available' },
              requiredScopes: [],
              requiresApproval: false,
              riskLevel: 'read',
            },
          ],
        },
      }
    if (url.pathname === `${apiRoot}/resources/graph`) {
      if (method !== 'GET') {
        writes.push(`${method} ${url.pathname}${url.search}`)
        return undefined
      }
      graphReads.push(url.search)
      if (
        url.searchParams.get('kind') !== 'CustomResourceDefinition' ||
        url.searchParams.get('name') !== crd.name ||
        url.searchParams.get('namespace') !== null ||
        url.searchParams.get('apiVersion') !== 'apiextensions.k8s.io/v1' ||
        url.searchParams.get('resource') !== 'customresourcedefinitions'
      ) {
        evidence.missing.push(`unexpected CRD definition graph identity: ${url.search}`)
        return {
          status: 400,
          body: { error: { code: 'bad_identity', message: 'Wrong definition root' } },
        }
      }
      const graph: KubernetesResourceGraph = {
        rootId: 'root',
        clusterId,
        generatedAt: '2026-11-05T00:00:00Z',
        evidence: [],
        warnings: [],
        nodes: [
          {
            id: 'root',
            status: 'resolved',
            resource: {
              clusterId,
              apiVersion: 'apiextensions.k8s.io/v1',
              scopeMode: 'cluster',
              kind: 'CustomResourceDefinition',
              name: crd.name,
              uid: crd.uid,
            },
          },
        ],
        edges: [],
      }
      return { body: { data: graph } }
    }
    if (url.pathname === `${apiRoot}/extensions/crds` && method === 'GET')
      return { body: { data: [crd] } }
    if (
      url.pathname === `${apiRoot}/extensions/crds/${crd.name}/access` &&
      method === 'GET' &&
      url.searchParams.get('version') === crd.version
    )
      return { body: { data: { allowedActions: ['get', 'list', 'view'] } } }
    if (
      url.pathname === `${apiRoot}/extensions/crds/${crd.name}/resources` &&
      method === 'GET' &&
      url.searchParams.get('version') === crd.version &&
      !url.searchParams.has('namespace')
    )
      return { body: { data: [] } }
    return undefined
  })
  await page.goto(crdListRoute)
  const kindPanel = page.locator('.soha-crd-kind-panel')
  await kindPanel.getByRole('tab', { name: '定义信息', exact: true }).click()
  await kindPanel.getByRole('tab', { name: '关系', exact: true }).click()
  await expect(page.locator('.react-flow__node[data-id="root"]')).toBeVisible()
  expect(graphReads).toHaveLength(1)
  expect(new URLSearchParams(graphReads[0]).get('kind')).toBe('CustomResourceDefinition')
  expect(new URLSearchParams(graphReads[0]).get('name')).toBe(crd.name)
  expect(new URLSearchParams(graphReads[0]).has('namespace')).toBe(false)
  expect(new URLSearchParams(graphReads[0]).get('apiVersion')).toBe('apiextensions.k8s.io/v1')
  expect(new URLSearchParams(graphReads[0]).get('resource')).toBe('customresourcedefinitions')
  expect(writes).toEqual([])
  verifyBrowserEvidence(evidence)
})

test('CRD instance relationship root uses served plural GVK, namespace and blocks same-name GVK collisions', async ({
  context,
  page,
}) => {
  const graphReads: string[] = []
  const writes: string[] = []
  const evidence = await installMocks(context, page, (url, method) => {
    if (method !== 'GET' && url.pathname !== '/api/v1/auth/refresh') {
      writes.push(`${method} ${url.pathname}${url.search}`)
      return undefined
    }
    if (url.pathname === '/api/v1/access/permission-snapshot') {
      const body = baselineResponse(url.pathname)!
      if (!body.data || !('permissionKeys' in body.data))
        throw new Error('Missing permission snapshot')
      body.data.permissionKeys!.push('platform.extensions.view')
      body.data.visibleMenuIds = [...(body.data.visibleMenuIds ?? []), 'extensions']
      return { body }
    }
    if (url.pathname === '/api/v1/clusters/capabilities' && method === 'GET')
      return {
        body: {
          data: [
            {
              key: 'custom.resources',
              direct: { status: 'available' },
              agent: { status: 'available' },
              requiredScopes: [],
              requiresApproval: false,
              riskLevel: 'read',
            },
          ],
        },
      }
    if (url.pathname === `${apiRoot}/resources/graph`) {
      if (method !== 'GET') {
        writes.push(`${method} ${url.pathname}${url.search}`)
        return undefined
      }
      graphReads.push(url.search)
      if (
        url.searchParams.get('kind') !== crd.kind ||
        url.searchParams.get('name') !== crdName ||
        url.searchParams.get('namespace') !== namespace ||
        url.searchParams.get('apiVersion') !== `${crd.group}/${crd.version}` ||
        url.searchParams.get('resource') !== crd.plural
      ) {
        evidence.missing.push(`unexpected CRD instance graph identity: ${url.search}`)
        return {
          status: 400,
          body: { error: { code: 'bad_identity', message: 'Wrong instance root' } },
        }
      }
      const graph: KubernetesResourceGraph = {
        rootId: 'root',
        clusterId,
        namespace,
        generatedAt: '2026-11-05T00:00:00Z',
        evidence: [],
        warnings: [],
        nodes: [
          {
            id: 'root',
            status: 'resolved',
            resource: {
              clusterId,
              namespace,
              apiVersion: `${crd.group}/${crd.version}`,
              scopeMode: 'namespace',
              kind: crd.kind,
              name: crdName,
              uid: 'synthetic-widget-uid',
            },
          },
          {
            id: 'collision',
            status: 'resolved',
            resource: {
              clusterId,
              namespace,
              apiVersion: 'different.example/v9',
              scopeMode: 'namespace',
              kind: crd.kind,
              name: crdName,
              uid: 'wrong-gvk-uid',
            },
          },
        ],
        edges: [
          { id: 'collision-edge', sourceId: 'root', targetId: 'collision', relation: 'declares' },
        ],
      }
      return { body: { data: graph } }
    }
    if (url.pathname === `${apiRoot}/extensions/crds`) return { body: { data: [crd] } }
    if (url.pathname === `${apiRoot}/extensions/crds/${crd.name}/access` && method === 'GET') {
      if (url.searchParams.has('namespace') || url.searchParams.get('version') !== crd.version) {
        evidence.missing.push(`unexpected CRD access scope: ${url.search}`)
        return {
          status: 400,
          body: { error: { code: 'bad_scope', message: 'Wrong CRD access scope' } },
        }
      }
      return { body: { data: { allowedActions: ['view', 'get', 'list'] } } }
    }
    if (url.pathname === `${apiRoot}/extensions/crds/${crd.name}/resources` && method === 'GET') {
      if (
        (url.searchParams.has('namespace') && url.searchParams.get('namespace') !== namespace) ||
        url.searchParams.get('version') !== crd.version
      ) {
        evidence.missing.push(`unexpected CRD collection scope: ${url.search}`)
        return {
          status: 400,
          body: { error: { code: 'bad_scope', message: 'Wrong CRD collection scope' } },
        }
      }
      return {
        body: {
          data: [
            {
              uid: 'synthetic-widget-uid',
              name: crdName,
              namespace,
              kind: crd.kind,
              apiVersion: `${crd.group}/${crd.version}`,
              allowedActions: ['view', 'get', 'list'],
            },
          ],
        },
      }
    }
    if (
      url.pathname === `${apiRoot}/extensions/crds/${crd.name}/resources/${crdName}/access` &&
      method === 'GET' &&
      url.searchParams.get('namespace') === namespace &&
      url.searchParams.get('version') === crd.version
    )
      return { body: { data: { allowedActions: ['view', 'get', 'list'] } } }
    if (url.pathname === `${apiRoot}/extensions/crds/${crd.name}/resources/${crdName}/yaml`)
      return {
        body: {
          data: {
            content: `apiVersion: ${crd.group}/${crd.version}\nkind: ${crd.kind}\nmetadata: {name: ${crdName}, namespace: ${namespace}}\n`,
          },
        },
      }
    if (method !== 'GET' && url.pathname.startsWith(`${apiRoot}/extensions/crds/`))
      writes.push(`${method} ${url.pathname}${url.search}`)
    return undefined
  })
  await page.goto(crdListRoute)
  await page.getByRole('button', { name: crdName, exact: true }).click()
  const instancePanel = page.locator('.soha-crd-resource-detail')
  await instancePanel.getByRole('tab', { name: '关系', exact: true }).click()
  await expect(page.locator('.react-flow__node[data-id="root"]')).toBeVisible()
  const query = new URLSearchParams(graphReads[0])
  expect(query.get('kind')).toBe(crd.kind)
  expect(query.get('name')).toBe(crdName)
  expect(query.get('namespace')).toBe(namespace)
  expect(query.get('apiVersion')).toBe(`${crd.group}/${crd.version}`)
  expect(query.get('resource')).toBe(crd.plural)
  await page
    .getByRole('radiogroup', { name: '关联资源视图' })
    .getByText('列表', { exact: true })
    .click()
  const collisionNode = page.locator('button[data-resource-id="collision"]')
  await expect(collisionNode).toBeVisible()
  await collisionNode.click()
  const inspector = page.getByRole('complementary', { name: '选中资源信息' })
  await expect(inspector.locator('dd').nth(1)).toHaveText('different.example/v9')
  await expect(inspector.getByRole('button', { name: '查看详情', exact: true })).toBeDisabled()
  expect(writes).toEqual([])
  verifyBrowserEvidence(evidence)
})
