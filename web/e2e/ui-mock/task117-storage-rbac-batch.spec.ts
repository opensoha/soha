import { test, expect, type BrowserContext, type Page } from '@playwright/test'
import { stringify } from 'yaml'
import { baselineResponse } from '../fixtures/baseline-data'
import { installMocks, verifyBrowserEvidence } from '../fixtures/mock-app'

const namespace = 'monitoring'
const name = 'synthetic-resource'
const lastName = 'zz-final-resource'
const owners = [
  {
    group: 'storage',
    kind: 'persistentvolumeclaims',
    label: 'PVC',
    native: 'PersistentVolumeClaim',
    permission: 'persistent-volume-claims',
    namespaced: true,
    tabs: ['概览', 'Pods', '关系', 'YAML'],
  },
  {
    group: 'storage',
    kind: 'persistentvolumes',
    label: 'PV',
    native: 'PersistentVolume',
    permission: 'persistent-volumes',
    namespaced: false,
    tabs: ['概览', '关系', 'YAML'],
  },
  {
    group: 'storage',
    kind: 'storageclasses',
    label: 'StorageClass',
    native: 'StorageClass',
    permission: 'storage-classes',
    namespaced: false,
    tabs: ['概览', 'PVs', 'PVCs', '关系', 'YAML'],
  },
  {
    group: 'access-control',
    kind: 'serviceaccounts',
    label: 'ServiceAccount',
    native: 'ServiceAccount',
    permission: 'service-accounts',
    namespaced: true,
    tabs: ['概览', '绑定', '有效权限', '关系', 'YAML'],
  },
  {
    group: 'access-control',
    kind: 'roles',
    label: 'Role',
    native: 'Role',
    permission: 'roles',
    namespaced: true,
    tabs: ['概览', '权限规则', '绑定', '关系', 'YAML'],
  },
  {
    group: 'access-control',
    kind: 'clusterroles',
    label: 'ClusterRole',
    native: 'ClusterRole',
    permission: 'cluster-roles',
    namespaced: false,
    tabs: ['概览', '权限规则', '绑定', '关系', 'YAML'],
  },
  {
    group: 'access-control',
    kind: 'rolebindings',
    label: 'RoleBinding',
    native: 'RoleBinding',
    permission: 'role-bindings',
    namespaced: true,
    tabs: ['概览', '关系', 'YAML'],
  },
  {
    group: 'access-control',
    kind: 'clusterrolebindings',
    label: 'ClusterRoleBinding',
    native: 'ClusterRoleBinding',
    permission: 'cluster-role-bindings',
    namespaced: false,
    tabs: ['概览', '关系', 'YAML'],
  },
] as const
type Owner = (typeof owners)[number]
function collection(owner: Owner) {
  return `/${owner.group === 'storage' ? 'storage' : 'platform-access-control'}/${owner.kind}`
}
function detailURL(owner: Owner, tab = 'overview') {
  return `${collection(owner)}/${name}?${new URLSearchParams({ clusterId: 'cluster-a', namespace, tab })}`
}
function record(owner: Owner, resourceName = name, missingActions = false) {
  return {
    name: resourceName,
    ...(owner.namespaced ? { namespace } : {}),
    ageSeconds: 3600,
    createdAt: '2026-11-03T01:00:00Z',
    labels: {},
    annotations: {},
    ...(!missingActions ? { allowedActions: ['update', 'delete'] } : {}),
    rules: 2,
    aggregationRules: 0,
    ruleSummaries: ['Synthetic API summary'],
    roleRef: 'ClusterRole/reader',
    subjects: ['User:oidc:team/alice@example.test'],
    automountServiceAccountToken: false,
    imagePullSecrets: [],
    secrets: [],
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
  }
}
function yaml(owner: Owner) {
  return stringify({
    apiVersion:
      owner.native === 'StorageClass'
        ? 'storage.k8s.io/v1'
        : owner.group === 'access-control' && owner.native !== 'ServiceAccount'
          ? 'rbac.authorization.k8s.io/v1'
          : 'v1',
    kind: owner.native,
    metadata: { name, ...(owner.namespaced ? { namespace } : {}) },
    ...(owner.native === 'PersistentVolume'
      ? {
          spec: {
            csi: { driver: 'csi.synthetic.test', volumeHandle: 'synthetic-handle' },
            storageClassName: 'fast',
          },
        }
      : {}),
    ...(owner.native === 'PersistentVolumeClaim' ? { spec: { storageClassName: 'fast' } } : {}),
    ...(owner.native === 'StorageClass'
      ? { provisioner: 'csi.synthetic.test', mountOptions: ['noatime'] }
      : {}),
    ...(owner.native === 'ServiceAccount' ? { automountServiceAccountToken: false } : {}),
    ...(owner.kind.endsWith('roles')
      ? {
          rules: [
            {
              apiGroups: [''],
              resources: ['pods/log', '*'],
              verbs: ['get', '*'],
              resourceNames: ['api:blue'],
            },
            { nonResourceURLs: ['/healthz/*'], verbs: ['get'] },
          ],
        }
      : {}),
    ...(owner.kind.endsWith('bindings')
      ? {
          roleRef: { kind: 'ClusterRole', name: 'reader', apiGroup: 'rbac.authorization.k8s.io' },
          subjects: [
            {
              kind: 'User',
              name: 'oidc:team/alice@example.test',
              apiGroup: 'rbac.authorization.k8s.io',
            },
            {
              kind: 'Group',
              name: 'system:serviceaccounts:monitoring',
              apiGroup: 'rbac.authorization.k8s.io',
            },
            { kind: 'ServiceAccount', name: 'api', namespace },
          ],
        }
      : {}),
  })
}
async function setup(
  context: BrowserContext,
  page: Page,
  options: { missingActions?: boolean; fail?: string } = {},
) {
  const state = { reads: [] as string[], writes: [] as string[], fail: options.fail }
  const evidence = await installMocks(context, page, (url, method, request) => {
    if (url.pathname === '/api/v1/access/permission-snapshot') {
      const body = baselineResponse(url.pathname)!
      if (!body.data || !('permissionKeys' in body.data) || !body.data.permissionKeys)
        throw new Error('Missing permissions')
      body.data.permissionKeys.push(
        ...owners.flatMap((owner) =>
          ['view', 'update', 'delete'].map(
            (action) => `platform.${owner.group}.${owner.permission}.${action}`,
          ),
        ),
      )
      return { body }
    }
    if (
      url.pathname === '/api/v1/clusters/cluster-a/resource-creation/scope-decision' &&
      method === 'POST'
    ) {
      const input = request.postDataJSON() as {
        kind: string
        resourceGroup: string
        action: string
        namespace?: string
      }
      const owner = owners.find(
        (item) => item.native === input.kind && item.group === input.resourceGroup,
      )
      if (!owner || input.action !== 'create') return undefined
      expect(owner.namespaced ? [undefined, namespace] : [undefined]).toContain(input.namespace)
      return {
        body: {
          data: {
            allowed: false,
            reason: 'Synthetic readonly create scope',
            allowedActions: [],
            resourceScope: {
              clusterIds: ['cluster-a'],
              namespaces: owner.namespaced ? [namespace] : [],
              resourceGroups: [owner.group],
              resourceKinds: [owner.native],
            },
            capability: { key: 'resource.create', status: 'available', mode: 'agent' },
          },
        },
      }
    }
    const match =
      /^\/api\/v1\/clusters\/cluster-a\/(storage|access-control)\/([^/]+)(?:\/([^/]+)\/(detail|yaml))?$/.exec(
        url.pathname,
      )
    if (!match) return undefined
    const owner = owners.find((item) => item.group === match[1] && item.kind === match[2])
    if (!owner) return undefined
    if (method !== 'GET') {
      state.writes.push(method + ' ' + url.pathname)
      return undefined
    }
    if (
      owner.namespaced
        ? url.searchParams.has('namespace') && url.searchParams.get('namespace') !== namespace
        : url.searchParams.has('namespace')
    )
      return undefined
    state.reads.push(url.pathname + url.search)
    if (state.fail === owner.kind)
      return {
        status: 503,
        body: {
          error: { code: 'unavailable', message: 'Synthetic storage/RBAC read unavailable' },
        },
      }
    if (!match[3])
      return {
        body: {
          data: Array.from({ length: 42 }, (_, i) =>
            record(
              owner,
              i === 0 ? name : i === 41 ? lastName : `sample-${i}`,
              options.missingActions,
            ),
          ),
        },
      }
    if (match[3] !== name || (owner.namespaced && url.searchParams.get('namespace') !== namespace))
      return undefined
    if (match[4] === 'detail')
      return { body: { data: record(owner, name, options.missingActions) } }
    if (match[4] === 'yaml') return { body: { data: { content: yaml(owner) } } }
    return undefined
  })
  return { evidence, state }
}

for (const owner of owners) {
  test(`${owner.label}: full collection, scoped detail header, tabs and refresh`, async ({
    context,
    page,
  }) => {
    const { evidence, state } = await setup(context, page)
    await page.goto(`${collection(owner)}?clusterId=cluster-a&namespace=${namespace}`)
    await expect(page.getByRole('status').filter({ hasText: '42' })).toBeVisible()
    await expect(page.locator('.ant-pagination')).toHaveCount(0)
    const search = page.getByPlaceholder(/^搜索 /)
    await search.fill(lastName)
    await expect(page.getByRole('status').filter({ hasText: '匹配 1 / 共 42' })).toBeVisible()
    await expect(page.getByText(lastName, { exact: true })).toBeVisible()
    await search.fill('')
    await page.getByText(name, { exact: true }).click()
    await expect(page.getByRole('heading', { name, exact: true })).toBeVisible()
    if (owner.namespaced) await expect(page).toHaveURL(/namespace=monitoring/)
    await expect(page.locator('.soha-resource-tabs').getByRole('tab')).toHaveText([...owner.tabs])
    await page.getByRole('tab', { name: 'YAML', exact: true }).click()
    await expect(page.getByRole('tabpanel', { name: 'YAML', exact: true })).toContainText(
      owner.native,
    )
    const before = state.reads.length
    await page.getByRole('button', { name: `刷新 ${owner.label}`, exact: true }).click()
    await expect.poll(() => state.reads.length).toBeGreaterThan(before)
    await expect(page.getByRole('tab', { name: 'YAML', exact: true })).toHaveAttribute(
      'aria-selected',
      'true',
    )
    await expect(page).toHaveURL(/tab=yaml/)
    for (const path of state.reads) {
      const params = new URL(path, 'http://localhost').searchParams
      for (const key of ['page', 'pageSize', 'keyword']) expect(params.has(key)).toBe(false)
      if (!owner.namespaced) expect(params.has('namespace')).toBe(false)
    }
    expect(state.writes).toEqual([])
    verifyBrowserEvidence(evidence)
  })
}

for (const owner of owners.filter((owner) => owner.kind.endsWith('bindings'))) {
  test(`${owner.label}: raw User/Group identities remain text and explicit ServiceAccount links stay scoped`, async ({
    context,
    page,
  }) => {
    const { evidence, state } = await setup(context, page)
    await page.goto(detailURL(owner))
    const panel = page.getByRole('tabpanel', { name: '概览', exact: true })
    for (const identity of ['oidc:team/alice@example.test', 'system:serviceaccounts:monitoring']) {
      await expect(panel.getByText(identity, { exact: true })).toBeVisible()
      await expect(panel.getByRole('link', { name: identity, exact: true })).toHaveCount(0)
    }
    await expect(panel.getByRole('link', { name: 'api', exact: true })).toHaveAttribute(
      'href',
      '/platform-access-control/serviceaccounts/api?clusterId=cluster-a&namespace=monitoring',
    )
    await expect(panel.getByRole('link', { name: 'reader', exact: true })).toHaveAttribute(
      'href',
      '/platform-access-control/clusterroles/reader?clusterId=cluster-a',
    )
    await panel.getByRole('textbox', { name: '搜索授权主体' }).fill('oidc:team/')
    await expect(panel.getByText('oidc:team/alice@example.test', { exact: true })).toBeVisible()
    await expect(panel.getByText('system:serviceaccounts:monitoring', { exact: true })).toHaveCount(
      0,
    )
    expect(state.writes).toEqual([])
    verifyBrowserEvidence(evidence)
  })
}

test('Role and ClusterRole rules preserve raw core groups, subresources and URL patterns', async ({
  context,
  page,
}) => {
  const { evidence, state } = await setup(context, page)
  for (const owner of [owners[4], owners[5]]) {
    await page.goto(detailURL(owner, 'rules'))
    const panel = page.getByRole('tabpanel', { name: '权限规则', exact: true })
    for (const value of ['core ("")', 'pods/log', 'api:blue', '/healthz/*'])
      await expect(panel.getByText(value, { exact: true })).toBeVisible()
    await panel.getByPlaceholder('搜索 API 组、资源、操作、名称或 URL').fill('/healthz/*')
    await expect(panel).toContainText('当前显示 1 / 2 条')
    await expect(panel.getByText('/healthz/*', { exact: true })).toBeVisible()
    await expect(panel.getByText('pods/log', { exact: true })).toHaveCount(0)
  }
  expect(state.writes).toEqual([])
  verifyBrowserEvidence(evidence)
})

test('Missing allowedActions keeps PV and Role YAML read-only despite global update/delete permission', async ({
  context,
  page,
}) => {
  const { evidence, state } = await setup(context, page, { missingActions: true })
  for (const owner of [owners[1], owners[4]]) {
    await page.goto(detailURL(owner, 'yaml'))
    await expect(page.getByRole('heading', { name, exact: true })).toBeVisible()
    await expect(page.getByRole('button', { name: '编辑 YAML', exact: true })).toHaveCount(0)
    await expect(page.getByRole('button', { name: '删除', exact: true })).toHaveCount(0)
    await expect(page.getByRole('tabpanel', { name: 'YAML', exact: true })).toContainText(
      '资源内容 · 只读',
    )
    await expect(page.getByRole('button', { name: /^应\s*用$/ })).toHaveCount(0)
  }
  expect(state.writes).toEqual([])
  verifyBrowserEvidence(evidence)
})

test('StorageClass read error is visible and retry recovers the same detail identity', async ({
  context,
  page,
}) => {
  const owner = owners[2]
  const { evidence, state } = await setup(context, page, { fail: owner.kind })
  await page.goto(detailURL(owner))
  await expect(page.getByRole('alert').filter({ hasText: '读取失败' }).first()).toContainText(
    'Synthetic storage/RBAC read unavailable',
  )
  await expect(page.getByRole('heading', { name, exact: true })).toHaveCount(0)
  state.fail = undefined
  await page.getByRole('button', { name: /重试/ }).first().click()
  await expect(page.getByRole('heading', { name, exact: true })).toBeVisible()
  expect(state.writes).toEqual([])
  verifyBrowserEvidence(evidence)
})

async function expectContainedLayout(page: Page) {
  const geometry = await page.evaluate(() => {
    const width = document.documentElement.clientWidth
    const containers = Array.from(
      document.querySelectorAll(
        '.soha-kubernetes-resource-list, .soha-k8s-detail-header, .soha-workload-detail-page .ant-card',
      ),
    )
      .map((element) => {
        const rect = element.getBoundingClientRect()
        return { left: rect.left, right: rect.right, width: rect.width, height: rect.height }
      })
      .filter((rect) => rect.width > 0 && rect.height > 0)
    return { width, content: document.documentElement.scrollWidth, containers }
  })
  expect(
    geometry.content,
    'Horizontal scrolling stays inside the resource tables',
  ).toBeLessThanOrEqual(geometry.width + 1)
  expect(
    geometry.containers.length,
    'At least one visible resource container is measured',
  ).toBeGreaterThan(0)
  for (const rect of geometry.containers) {
    expect(rect.left, 'Card/header left edge stays within the viewport').toBeGreaterThanOrEqual(-1)
    expect(rect.right, 'Card/header right edge stays within the viewport').toBeLessThanOrEqual(
      geometry.width + 1,
    )
  }
}

for (const theme of ['light', 'dark'] as const) {
  test(`${theme}: 1280px storage and RBAC tables contain scrolling and retain visible header actions`, async ({
    context,
    page,
  }) => {
    await page.setViewportSize({ width: 1280, height: 900 })
    await page.addInitScript(
      (mode) =>
        localStorage.setItem(
          'soha-prefs',
          JSON.stringify({
            state: { themeMode: mode, themeId: 'soha', localeCode: 'zh_CN' },
            version: 0,
          }),
        ),
      theme,
    )
    const { evidence, state } = await setup(context, page)
    for (const owner of [owners[1], owners[2], owners[6], owners[3]]) {
      await page.goto(`${collection(owner)}?clusterId=cluster-a&namespace=${namespace}`)
      await expect(page.locator('html')).toHaveAttribute('data-theme-mode', theme)
      await expect(page.getByRole('status').filter({ hasText: '42' })).toBeVisible()
      await expect(page.getByPlaceholder(/^搜索 /)).toBeInViewport()
      await expectContainedLayout(page)
      await page.getByText(name, { exact: true }).click()
      await expect(page.getByRole('heading', { name, exact: true })).toBeVisible()
      // Exercise embedded relationship tables as well as the collection viewport.
      const tab =
        owner.kind === 'storageclasses' ? 'PVs' : owner.kind === 'serviceaccounts' ? '绑定' : '概览'
      await page.getByRole('tab', { name: tab, exact: true }).click()
      await expect(page.getByRole('tabpanel', { name: tab, exact: true })).toBeVisible()
      if (owner.kind === 'rolebindings')
        await expect(page.getByRole('textbox', { name: '搜索授权主体' })).toBeVisible()
      if (owner.kind === 'serviceaccounts')
        await expect(page.getByRole('textbox', { name: '搜索当前返回的绑定' })).toHaveCount(2)
      await expectContainedLayout(page)
      const header = page.locator('.soha-k8s-detail-header')
      // Antd inserts spacing into the accessible name of two-character Chinese buttons.
      // The actual detail headers have no delete aria-label; the snapshot is “删 除”.
      const actions = [
        { label: `刷新 ${owner.label}`, name: `刷新 ${owner.label}` },
        { label: '编辑 YAML', name: '编辑 YAML' },
        { label: '删除', name: /^删\s*除$/ },
      ]
      for (const { label, name: actionName } of actions) {
        const action = header.getByRole('button', { name: actionName, exact: true })
        await expect(action).toBeVisible()
        await expect(action).toBeInViewport()
        const unobscured = await action.evaluate((element) => {
          const rect = element.getBoundingClientRect()
          const hit = document.elementFromPoint(
            rect.left + rect.width / 2,
            rect.top + rect.height / 2,
          )
          return (
            rect.left >= 0 &&
            rect.right <= window.innerWidth &&
            rect.top >= 0 &&
            rect.bottom <= window.innerHeight &&
            Boolean(hit && element.contains(hit))
          )
        })
        expect(unobscured, `${owner.label} ${label} is fully visible and unobscured`).toBe(true)
      }
    }
    expect(state.writes).toEqual([])
    verifyBrowserEvidence(evidence)
  })
}
