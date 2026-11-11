import { test, expect, type BrowserContext, type Page } from '@playwright/test'
import type { KubernetesResourceGraph } from '@opensoha/contracts/gen/ts/sohaapi'
import { baselineResponse } from '../fixtures/baseline-data'
import { installMocks, verifyBrowserEvidence } from '../fixtures/mock-app'

const clusterId = 'cluster-a'
const namespace = 'monitoring'
const name = 'data'
const apiRoot = `/api/v1/clusters/${clusterId}`

const roots = [
  {
    kind: 'PersistentVolumeClaim',
    collection: 'persistentvolumeclaims',
    route: '/storage/persistentvolumeclaims',
    scope: 'namespace',
    permission: 'platform.storage.persistent-volume-claims.view',
  },
  {
    kind: 'PersistentVolume',
    collection: 'persistentvolumes',
    route: '/storage/persistentvolumes',
    scope: 'cluster',
    permission: 'platform.storage.persistent-volumes.view',
  },
  {
    kind: 'StorageClass',
    collection: 'storageclasses',
    route: '/storage/storageclasses',
    scope: 'cluster',
    permission: 'platform.storage.storage-classes.view',
  },
] as const
type Root = (typeof roots)[number]

const detailRoute = (root: Root) =>
  `${root.route}/${name}?clusterId=${clusterId}${root.scope === 'namespace' ? `&namespace=${namespace}` : ''}`
const graphPath = `${apiRoot}/resources/graph`
const panel = (page: Page) => page.getByRole('tabpanel', { name: '关系', exact: true })
const inspector = (page: Page) => panel(page).getByRole('complementary', { name: '选中资源信息' })

function resource(
  id: string,
  kind: string,
  resourceName: string,
  resourceNamespace?: string,
  status: 'resolved' | 'unresolved' = 'resolved',
): KubernetesResourceGraph['nodes'][number] {
  const namespaceValue = resourceNamespace || undefined
  return {
    id,
    status,
    resource: {
      clusterId,
      apiVersion: kind === 'StorageClass' ? 'storage.k8s.io/v1' : 'v1',
      scopeMode: namespaceValue ? 'namespace' : 'cluster',
      kind,
      name: resourceName,
      ...(namespaceValue ? { namespace: namespaceValue } : {}),
    },
  }
}

function storageGraph(root: Root): KubernetesResourceGraph {
  const monitorClaim = resource(
    root.kind === 'PersistentVolumeClaim' ? 'root' : 'claim',
    'PersistentVolumeClaim',
    name,
    namespace,
  )
  const monitorPod = resource('pod', 'Pod', 'api-0', namespace)
  const boundVolume = resource(
    root.kind === 'PersistentVolume' ? 'root' : 'pv',
    'PersistentVolume',
    root.kind === 'PersistentVolume' ? name : 'pv-data',
  )
  const storageClass = resource(
    root.kind === 'StorageClass' ? 'root' : 'class',
    'StorageClass',
    root.kind === 'StorageClass' ? name : 'fast',
  )
  const rootId = 'root'
  const rootRef =
    root.kind === 'PersistentVolumeClaim'
      ? monitorClaim
      : root.kind === 'PersistentVolume'
        ? boundVolume
        : storageClass
  const duplicateClaim = resource(
    'duplicate',
    'PersistentVolumeClaim',
    name,
    'production',
    'unresolved',
  )
  const unresolvedVolume = resource('unbound-pv', 'PersistentVolume', 'pv-unbound')
  const nodes = [
    monitorClaim,
    monitorPod,
    boundVolume,
    storageClass,
    duplicateClaim,
    unresolvedVolume,
  ]
  const edges: KubernetesResourceGraph['edges'] = [
    { id: 'mount', sourceId: 'pod', targetId: monitorClaim.id, relation: 'mounts' },
    { id: 'binding', sourceId: monitorClaim.id, targetId: boundVolume.id, relation: 'binds-to' },
    {
      id: 'class-use',
      sourceId: monitorClaim.id,
      targetId: storageClass.id,
      relation: 'uses-storage-class',
    },
    {
      id: 'duplicate-class-use',
      sourceId: 'duplicate',
      targetId: storageClass.id,
      relation: 'uses-storage-class',
    },
    {
      id: 'unresolved-binding',
      sourceId: 'duplicate',
      targetId: unresolvedVolume.id,
      relation: 'binds-to',
      resolutionState: 'unresolved',
    },
  ]
  // Keep root identity and its namespace aligned with the actual detail entry point.
  rootRef.id = rootId
  return {
    rootId,
    clusterId,
    ...(root.scope === 'namespace' ? { namespace } : {}),
    generatedAt: '2026-11-05T00:00:00Z',
    evidence: [],
    warnings: [],
    nodes,
    edges,
  }
}

function storageYAML(kind: string, itemName: string) {
  if (kind === 'PersistentVolumeClaim')
    return `apiVersion: v1\nkind: PersistentVolumeClaim\nmetadata: {name: ${itemName}, namespace: ${namespace}}\nspec: {accessModes: [ReadWriteOnce], resources: {requests: {storage: 20Gi}}, storageClassName: fast, volumeName: pv-data}\nstatus: {phase: Bound}\n`
  if (kind === 'PersistentVolume')
    return `apiVersion: v1\nkind: PersistentVolume\nmetadata: {name: ${itemName}}\nspec: {accessModes: [ReadWriteOnce], capacity: {storage: 20Gi}, persistentVolumeReclaimPolicy: Retain, storageClassName: fast, claimRef: {name: data, namespace: ${namespace}}}\nstatus: {phase: Bound}\n`
  return `apiVersion: storage.k8s.io/v1\nkind: StorageClass\nmetadata: {name: ${itemName}}\nprovisioner: csi.synthetic.test\nreclaimPolicy: Retain\nvolumeBindingMode: WaitForFirstConsumer\nallowVolumeExpansion: true\n`
}

async function setup(context: BrowserContext, page: Page, root: Root) {
  const state = {
    graphStatus: 200,
    graphReads: [] as string[],
    detailReads: [] as string[],
    writes: [] as string[],
  }
  const evidence = await installMocks(context, page, (url, method) => {
    if (method !== 'GET' && url.pathname !== '/api/v1/auth/refresh') {
      state.writes.push(`${method} ${url.pathname}${url.search}`)
      return {
        status: 405,
        body: { error: { code: 'method_not_allowed', message: 'Read-only UI mock' } },
      }
    }

    if (url.pathname === '/api/v1/access/permission-snapshot' && method === 'GET') {
      const body = baselineResponse(url.pathname)!
      if (!body.data || !('permissionKeys' in body.data))
        throw new Error('Missing permission fixture')
      body.data.permissionKeys = [
        ...(body.data.permissionKeys || []),
        ...roots.map((item) => item.permission),
      ]
      return { body }
    }

    if (url.pathname === graphPath && method === 'GET') {
      state.graphReads.push(url.search)
      const expectedNamespace = root.scope === 'namespace' ? namespace : null
      const expectedKeys =
        root.scope === 'namespace' ? ['kind', 'name', 'namespace'] : ['kind', 'name']
      if (
        url.searchParams.get('kind') !== root.kind ||
        url.searchParams.get('name') !== name ||
        url.searchParams.get('namespace') !== expectedNamespace ||
        [...url.searchParams.keys()].sort().join(',') !== expectedKeys.sort().join(',')
      ) {
        evidence.missing.push(`unexpected graph identity: ${url.search}`)
        return {
          status: 400,
          body: { error: { code: 'bad_identity', message: 'Unexpected graph identity' } },
        }
      }
      return state.graphStatus === 200
        ? { body: { data: storageGraph(root) } }
        : {
            status: state.graphStatus,
            body: {
              error: {
                code: state.graphStatus === 403 ? 'forbidden' : 'synthetic_failure',
                message: 'Storage relationship read failed',
              },
            },
          }
    }

    const match = /^\/api\/v1\/clusters\/cluster-a\/storage\/([^/]+)\/([^/]+)\/(detail|yaml)$/.exec(
      url.pathname,
    )
    if (match && method === 'GET') {
      const [, collection, itemName, action] = match
      const detailRoot = roots.find((item) => item.collection === collection)
      const expectedNamespace = detailRoot?.scope === 'namespace' ? namespace : null
      const expectedKeys = expectedNamespace ? ['namespace'] : []
      const allowedName =
        detailRoot?.collection === 'persistentvolumes' && itemName === 'pv-data'
          ? true
          : itemName === name
      if (
        !detailRoot ||
        !allowedName ||
        url.searchParams.get('namespace') !== expectedNamespace ||
        [...url.searchParams.keys()].sort().join(',') !== expectedKeys.join(',')
      ) {
        evidence.missing.push(`unexpected storage detail scope: ${url.pathname}${url.search}`)
        return {
          status: 400,
          body: { error: { code: 'bad_scope', message: 'Unexpected storage detail scope' } },
        }
      }
      if (action === 'yaml')
        return {
          body: {
            data: {
              content: storageYAML(detailRoot.kind, itemName),
              resourceVersion: '1',
            },
          },
        }
      state.detailReads.push(`${url.pathname}${url.search}`)
      return {
        body: {
          data: {
            name: itemName,
            ...(detailRoot.scope === 'namespace' ? { namespace } : {}),
            ageSeconds: 60,
            createdAt: '2026-11-04T00:00:00Z',
            allowedActions: ['view'],
            labels: {},
            annotations: {},
            status: 'Bound',
            capacity: '20Gi',
            requested: '20Gi',
            storageClass: 'fast',
            accessModes: ['ReadWriteOnce'],
            reclaimPolicy: 'Retain',
            volumeMode: 'Filesystem',
            provisioner: 'csi.synthetic.test',
            volumeBindingMode: 'WaitForFirstConsumer',
            allowVolumeExpansion: true,
            volumes: [],
            claims: [],
            pods: [],
            ...(detailRoot.kind === 'PersistentVolume' && itemName === 'pv-data'
              ? { claimRef: `${namespace}/${name}`, claimNamespace: namespace, claimName: name }
              : {}),
          },
        },
      }
    }

    if (method !== 'GET' && url.pathname.startsWith(`${apiRoot}/`)) {
      if (url.pathname !== '/api/v1/auth/refresh')
        state.writes.push(`${method} ${url.pathname}${url.search}`)
      return undefined
    }
    return undefined
  })
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
  return { state, evidence }
}

for (const root of roots) {
  test(`${root.kind} loads only its active detail graph with exact Kubernetes scope`, async ({
    context,
    page,
  }) => {
    const fixture = await setup(context, page, root)
    await page.goto(detailRoute(root))
    await expect(page.getByRole('heading', { name, exact: true })).toBeVisible()
    expect(fixture.state.graphReads).toEqual([])

    await page.getByRole('tab', { name: '关系', exact: true }).click()
    await expect(panel(page).locator('.react-flow__node[data-id="root"]')).toBeVisible()
    await expect.poll(() => fixture.state.graphReads.length).toBe(1)
    const query = new URLSearchParams(fixture.state.graphReads[0])
    expect(query.get('kind')).toBe(root.kind)
    expect(query.get('name')).toBe(name)
    expect(query.get('namespace')).toBe(root.scope === 'namespace' ? namespace : null)
    expect(fixture.state.writes).toEqual([])
    verifyBrowserEvidence(fixture.evidence)
  })
}

test('PVC graph preserves relationship direction, duplicate namespace identity, inspector, list and detail navigation', async ({
  context,
  page,
}) => {
  const fixture = await setup(context, page, roots[0])
  await page.goto(`${detailRoute(roots[0])}&tab=relationships`)
  await expect(panel(page).locator('.react-flow__node[data-id="root"]')).toBeVisible()
  const rootNode = panel(page).locator('.react-flow__node[data-id="root"]')
  await expect(rootNode).toContainText('monitoring/data')
  await panel(page).locator('.react-flow__node[data-id="root"]').click()
  await expect(inspector(page)).toContainText('绑定到')
  await expect(inspector(page)).toContainText('引用存储类')
  await panel(page).locator('.react-flow__node[data-id="pod"]').click()
  await expect(inspector(page)).toContainText('声明引用卷')
  await panel(page).locator('.react-flow__node[data-id="class"]').click()
  await expect(inspector(page)).toContainText('存储类被引用于')
  await expect(
    inspector(page).getByRole('button', {
      name: 'PersistentVolumeClaim monitoring/data',
      exact: true,
    }),
  ).toBeVisible()
  await expect(
    inspector(page).getByRole('button', {
      name: 'PersistentVolumeClaim production/data',
      exact: true,
    }),
  ).toBeVisible()
  await panel(page).locator('.react-flow__node[data-id="duplicate"]').click()
  await expect(inspector(page)).toContainText('production')
  await expect(inspector(page)).toContainText('绑定待确认')
  await expect(
    panel(page).locator('.soha-unresolved-storage-binding .react-flow__edge-path'),
  ).toHaveCSS('stroke-dasharray', '5px, 4px')
  await expect(
    inspector(page).getByRole('button', { name: '查看详情', exact: true }),
  ).toBeDisabled()

  await panel(page).locator('.react-flow__node[data-id="root"]').click()
  await inspector(page)
    .getByRole('button', { name: 'PersistentVolume pv-data', exact: true })
    .click()
  await inspector(page).getByRole('button', { name: '查看详情', exact: true }).click()
  await expect.poll(() => new URL(page.url()).pathname).toBe('/storage/persistentvolumes/pv-data')
  await expect
    .poll(() =>
      fixture.state.detailReads.includes(
        '/api/v1/clusters/cluster-a/storage/persistentvolumes/pv-data/detail',
      ),
    )
    .toBe(true)
  await expect(page.getByRole('heading', { name: 'pv-data', exact: true })).toBeVisible()
  await expect(page.getByText('monitoring/data', { exact: true })).toBeVisible()
  const navigated = new URL(page.url())
  expect(navigated.searchParams.get('clusterId')).toBe(clusterId)
  expect(navigated.searchParams.has('namespace')).toBe(false)

  await page.goto(`${detailRoute(roots[0])}&tab=relationships`)
  await expect(panel(page).locator('.react-flow__node[data-id="root"]')).toBeVisible()
  await panel(page)
    .getByRole('radiogroup', { name: '关联资源视图' })
    .getByText('列表', { exact: true })
    .click()
  const monitorClaimRow = panel(page).locator('tr[data-row-key="root"]')
  const productionClaimRow = panel(page).locator('tr[data-row-key="duplicate"]')
  await expect(monitorClaimRow).toContainText('PersistentVolumeClaim')
  await expect(monitorClaimRow).toContainText('monitoring')
  await expect(productionClaimRow).toContainText('PersistentVolumeClaim')
  await expect(productionClaimRow).toContainText('production')
  await expect(panel(page).locator('tr[data-row-key="class"]')).toContainText(
    '存储类被引用于 PersistentVolumeClaim production/data',
  )
  await productionClaimRow.getByRole('button', { name: 'data', exact: true }).click()
  await expect(inspector(page)).toContainText('production')
  await expect(
    inspector(page).getByRole('button', { name: '查看详情', exact: true }),
  ).toBeDisabled()
  expect(fixture.state.writes).toEqual([])
  verifyBrowserEvidence(fixture.evidence)
})

test('partial graph failures keep a recoverable read state and forbidden responses clear protected graph content', async ({
  context,
  page,
}) => {
  const fixture = await setup(context, page, roots[0])
  await page.goto(`${detailRoute(roots[0])}&tab=relationships`)
  await expect(panel(page).locator('.react-flow__node[data-id="root"]')).toBeVisible()
  await panel(page).locator('.react-flow__node[data-id="root"]').click()
  fixture.state.graphStatus = 500
  await panel(page).getByRole('button', { name: '刷新资源关系', exact: true }).click()
  await expect(panel(page).getByRole('alert')).toContainText('正在展示上次读取结果', {
    timeout: 15_000,
  })
  await expect(inspector(page)).toContainText('绑定到')

  fixture.state.graphStatus = 403
  await panel(page).getByRole('button', { name: '刷新资源关系', exact: true }).click()
  await expect(panel(page).getByRole('alert')).toContainText('资源关系无访问权限')
  await expect(inspector(page)).toHaveCount(0)
  await expect(panel(page).locator('.react-flow__node')).toHaveCount(0)
  expect(fixture.state.writes).toEqual([])
  verifyBrowserEvidence(fixture.evidence)
})
