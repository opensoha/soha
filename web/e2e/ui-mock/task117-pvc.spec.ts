import { test, expect, type BrowserContext, type Page } from '@playwright/test'
import { stringify } from 'yaml'
import { baselineResponse } from '../fixtures/baseline-data'
import { installMocks, verifyBrowserEvidence } from '../fixtures/mock-app'

// Synthetic browser evidence only. Every API route and scope is explicitly constrained.
const namespace = 'monitoring'
const name = 'data-claim'
const root = '/api/v1/clusters/cluster-a/storage/persistentvolumeclaims'
const permission = 'platform.storage.persistent-volume-claims'
const detailURL = (tab = 'overview') =>
  `/storage/persistentvolumeclaims/${name}?clusterId=cluster-a&namespace=${namespace}&tab=${tab}`
function claim(claimName = name, allowedActions?: string[]) {
  return {
    name: claimName,
    namespace,
    status: 'Bound',
    requested: '20Gi',
    capacity: '24Gi',
    volumeName: 'pv-data',
    storageClass: 'fast',
    accessModes: ['ReadWriteOnce'],
    ageSeconds: 3600,
    createdAt: '2026-11-03T01:00:00Z',
    labels: { app: 'synthetic' },
    annotations: {},
    ...(allowedActions ? { allowedActions } : {}),
    pods: [{ name: 'api-0', namespace, phase: 'Running', nodeName: 'node-a' }],
  }
}
function content(generation: number) {
  return stringify({
    apiVersion: 'v1',
    kind: 'PersistentVolumeClaim',
    metadata: { name, namespace, generation },
    spec: {
      storageClassName: 'fast',
      volumeMode: 'Filesystem',
      selector: { matchLabels: { tier: 'archive' } },
    },
    status: {
      conditions: [
        { type: 'FileSystemResizePending', status: 'True', reason: 'NodeExpansionPending' },
      ],
    },
  })
}
async function setup(
  context: BrowserContext,
  page: Page,
  options: { readonly?: boolean; missingActions?: boolean } = {},
) {
  const state = { reads: [] as string[], writes: [] as string[], generation: 1 }
  const evidence = await installMocks(context, page, (url, method, request) => {
    if (url.pathname === '/api/v1/access/permission-snapshot') {
      const body = baselineResponse(url.pathname)!
      if (!body.data || !('permissionKeys' in body.data) || !body.data.permissionKeys)
        throw new Error('Missing baseline permissions')
      body.data.permissionKeys.push(
        ...(options.readonly ? ['view'] : ['view', 'update', 'delete']).map(
          (action) => `${permission}.${action}`,
        ),
        'platform.storage.persistent-volumes.view',
        'platform.storage.storage-classes.view',
        'platform.pods.view',
        'platform.nodes.view',
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
      if (
        input.kind !== 'PersistentVolumeClaim' ||
        input.resourceGroup !== 'storage' ||
        input.action !== 'create'
      )
        return undefined
      expect([undefined, namespace]).toContain(input.namespace)
      return {
        body: {
          data: {
            allowed: false,
            reason: 'Synthetic role has no create permission',
            allowedActions: [],
            resourceScope: {
              clusterIds: ['cluster-a'],
              namespaces: [namespace],
              resourceGroups: ['storage'],
              resourceKinds: ['PersistentVolumeClaim'],
            },
            capability: { key: 'resource.create', status: 'available', mode: 'agent' },
          },
        },
      }
    }
    if (!url.pathname.startsWith(root)) return undefined
    if (method !== 'GET') {
      state.writes.push(`${method} ${url.pathname}`)
      return undefined
    }
    if (url.searchParams.has('namespace') && url.searchParams.get('namespace') !== namespace)
      return undefined
    state.reads.push(url.pathname + url.search)
    const actions = options.missingActions ? undefined : ['update', 'delete']
    if (url.pathname === root)
      return {
        body: {
          data: Array.from({ length: 42 }, (_, index) =>
            claim(index === 0 ? name : index === 41 ? 'zz-final-claim' : `claim-${index}`, actions),
          ),
        },
      }
    if (url.searchParams.get('namespace') !== namespace) return undefined
    if (url.pathname === `${root}/${name}/detail`) return { body: { data: claim(name, actions) } }
    if (url.pathname === `${root}/${name}/yaml`)
      return { body: { data: { content: content(state.generation) } } }
    return undefined
  })
  return { state, evidence }
}

test('PVC collection retains continuous browsing and namespace context through detail/back', async ({
  context,
  page,
}, testInfo) => {
  const { state, evidence } = await setup(context, page)
  await page.goto('/storage/persistentvolumeclaims?clusterId=cluster-a')
  await expect(page.getByRole('status').filter({ hasText: '42' })).toBeVisible()
  await expect(page.locator('.ant-pagination')).toHaveCount(0)
  await page.screenshot({ path: testInfo.outputPath('pvc-list.png'), fullPage: true })
  const search = page.getByPlaceholder('搜索 PVC / namespace / storageClass')
  await search.fill('zz-final-claim')
  await expect(page.getByRole('status').filter({ hasText: '匹配 1 / 共 42' })).toBeVisible()
  await search.fill('')
  await page
    .getByRole('button', { name: `切换到命名空间 ${namespace}` })
    .first()
    .click()
  await expect(page).toHaveURL(/namespace=monitoring/)
  await page.getByText(name, { exact: true }).click()
  await expect(page.getByRole('heading', { name, exact: true })).toBeVisible()
  await expect(page).toHaveURL(/namespace=monitoring/)
  await page.getByRole('link', { name: '返回PVC列表' }).click()
  await expect(page).toHaveURL(/persistentvolumeclaims\?[^#]*namespace=monitoring/)
  for (const path of state.reads) {
    const params = new URL(path, 'http://localhost').searchParams
    expect(params.has('page')).toBe(false)
    expect(params.has('pageSize')).toBe(false)
    expect(params.has('keyword')).toBe(false)
  }
  expect(state.writes).toEqual([])
  verifyBrowserEvidence(evidence)
})

test('PVC overview and Pods show native facts and permission-aware scoped links', async ({
  context,
  page,
}, testInfo) => {
  await page.setViewportSize({ width: 1280, height: 900 })
  await page.emulateMedia({ colorScheme: 'dark' })
  const { state, evidence } = await setup(context, page)
  await page.goto(detailURL())
  const overview = page.getByRole('tabpanel', { name: '概览', exact: true })
  await expect(overview).toContainText('20Gi')
  await expect(overview).toContainText('24Gi')
  await expect(overview).toContainText('FileSystemResizePending')
  await expect(overview).toContainText('tier=archive')
  await expect(overview).toContainText('当前返回 1 条引用')
  await expect(overview.getByRole('link', { name: 'pv-data', exact: true })).toHaveAttribute(
    'href',
    '/storage/persistentvolumes/pv-data?clusterId=cluster-a',
  )
  await expect(overview.getByRole('link', { name: 'fast', exact: true })).toHaveAttribute(
    'href',
    '/storage/storageclasses/fast?clusterId=cluster-a',
  )
  await expect(page.locator('.monaco-editor')).toHaveCount(0)
  await page.screenshot({ path: testInfo.outputPath('pvc-overview.png'), fullPage: true })
  await page.getByRole('tab', { name: 'Pods', exact: true }).click()
  const pods = page.getByRole('tabpanel', { name: 'Pods', exact: true })
  await expect(pods.getByRole('link', { name: 'api-0', exact: true })).toHaveAttribute(
    'href',
    '/workloads/pods/api-0?clusterId=cluster-a&namespace=monitoring',
  )
  await expect(pods.getByRole('link', { name: 'node-a', exact: true })).toHaveAttribute(
    'href',
    '/cluster-resources/nodes/node-a?clusterId=cluster-a',
  )
  await page.screenshot({ path: testInfo.outputPath('pvc-pods.png'), fullPage: true })
  await pods.getByPlaceholder('搜索名称、Namespace、Phase、节点').fill('absent')
  await expect(pods).toContainText('显示 0 / 当前返回 1 条')
  await expect(pods).toContainText('当前返回范围内没有匹配引用')
  expect(
    await page.evaluate(
      () => document.documentElement.scrollWidth <= document.documentElement.clientWidth + 1,
    ),
  ).toBe(true)
  expect(state.writes).toEqual([])
  verifyBrowserEvidence(evidence)
})

for (const options of [{ readonly: true }, { missingActions: true }]) {
  test(`PVC YAML stays read-only with ${options.readonly ? 'view permission only' : 'missing allowedActions'}`, async ({
    context,
    page,
  }) => {
    const { state, evidence } = await setup(context, page, options)
    await page.goto(detailURL('yaml'))
    await expect(page.getByRole('heading', { name, exact: true })).toBeVisible()
    await expect(page.getByRole('button', { name: '编辑 YAML', exact: true })).toHaveCount(0)
    await expect(page.getByRole('button', { name: '删除', exact: true })).toHaveCount(0)
    await expect(page.getByRole('tabpanel', { name: 'YAML', exact: true })).toContainText(
      '资源内容 · 只读',
    )
    await expect(page.getByRole('button', { name: /^应\s*用$/ })).toHaveCount(0)
    expect(state.writes).toEqual([])
    verifyBrowserEvidence(evidence)
  })
}

test('PVC dirty YAML survives tab switches and a changed server refresh', async ({
  context,
  page,
}) => {
  const { state, evidence } = await setup(context, page)
  await page.goto(detailURL())
  await page.getByRole('button', { name: '编辑 YAML', exact: true }).click()
  const editor = page.getByRole('textbox', { name: 'YAML 草稿编辑器', exact: true })
  await expect(editor).toBeVisible()
  await editor.focus()
  await expect(editor).toBeFocused()
  await page.keyboard.press('ControlOrMeta+End')
  await page.keyboard.insertText('\n# task117 local draft\n')
  await page.getByRole('tab', { name: 'Pods', exact: true }).click()
  await page.getByRole('tab', { name: '概览', exact: true }).click()
  state.generation = 2
  const reads = state.reads.length
  await page.getByRole('button', { name: '刷新 PVC', exact: true }).click()
  await expect.poll(() => state.reads.length).toBeGreaterThan(reads)
  await page.getByRole('tab', { name: 'YAML', exact: true }).click()
  const yaml = page.getByRole('tabpanel', { name: 'YAML', exact: true })
  await expect(yaml).toContainText('当前草稿已保留')
  await expect(
    yaml.locator('.view-lines').filter({ hasText: 'task117 local draft' }).first(),
  ).toBeVisible()
  await expect(page.getByRole('button', { name: /^应\s*用$/ })).toBeDisabled()
  expect(state.writes).toEqual([])
  verifyBrowserEvidence(evidence)
})
