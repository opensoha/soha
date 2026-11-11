import { test, expect, type BrowserContext, type Page } from '@playwright/test'
import { baselineResponse } from '../fixtures/baseline-data'
import { installMocks, verifyBrowserEvidence } from '../fixtures/mock-app'

function deployments(count = 80) {
  return Array.from({ length: count }, (_, index) => ({
    name: `deployment-${String(index).padStart(3, '0')}`,
    namespace: 'monitoring',
    uid: `deployment-uid-${index}`,
    readyReplicas: 2,
    desiredReplicas: 3,
    updatedReplicas: 2,
    available: 2,
    ageSeconds: 86400,
    allowedActions: ['view', 'update', 'restart', 'scale', 'rollback', 'delete'],
  }))
}

async function setup(
  context: BrowserContext,
  page: Page,
  options: {
    readonly?: boolean
    live?: boolean
    unsupported?: boolean
    empty?: boolean
    allNamespaces?: boolean
  } = {},
) {
  const state = {
    rows: options.empty ? [] : deployments(),
    reads: 0,
    namespaceReads: [] as (string | null)[],
    status: 200,
    writes: [] as { method: string; path: string; body: Record<string, unknown> | null }[],
    failDelete: false,
    failRestart: '',
    failScale: '',
  }
  if (options.allNamespaces)
    state.rows = deployments(2).map((item, index) => ({
      ...item,
      namespace: index ? 'kube-system' : 'monitoring',
    }))
  if (options.readonly) state.rows = state.rows.map((row) => ({ ...row, allowedActions: ['view'] }))
  await context.addInitScript(
    (allNamespaces) =>
      localStorage.setItem(
        'soha-scope',
        JSON.stringify({
          state: { clusterId: 'cluster-a', namespace: allNamespaces ? null : 'monitoring' },
          version: 0,
        }),
      ),
    Boolean(options.allNamespaces),
  )
  if (options.live)
    await page.routeWebSocket('**/resources/stream?*', (socket) =>
      socket.send(
        JSON.stringify({ type: 'status', cacheStatus: 'live', observedAt: '2026-10-30T08:00:00Z' }),
      ),
    )
  const evidence = await installMocks(context, page, (url, method, request) => {
    if (options.allNamespaces && url.pathname === '/api/v1/clusters/cluster-a/namespaces')
      return {
        body: {
          data: ['monitoring', 'kube-system'].map((name) => ({
            name,
            status: 'Active',
            labels: {},
          })),
        },
      }
    if (url.pathname === '/api/v1/clusters')
      return {
        body: {
          data: [
            {
              id: 'cluster-a',
              name: 'Deployments 样板',
              connectionMode: 'direct_kubeconfig',
              health: { status: 'healthy' },
            },
          ],
        },
      }
    if (
      url.pathname === '/api/v1/clusters/cluster-a/resource-creation/scope-decision' &&
      method === 'POST'
    )
      return {
        body: {
          data: {
            allowed: !options.readonly && !options.unsupported,
            scopeMode: 'namespace',
            reason: options.readonly ? 'Synthetic read-only role' : '',
          },
        },
      }
    if (url.pathname === '/api/v1/clusters/capabilities')
      return {
        body: {
          data: ['workload.mutations', 'resource.yaml.apply'].map((key) => ({
            key,
            direct: {
              status: options.unsupported ? 'unsupported' : 'available',
              reason: options.unsupported ? 'Synthetic unavailable capability' : '',
            },
            agent: { status: 'unsupported' },
          })),
        },
      }
    if (url.pathname === '/api/v1/access/permission-snapshot') {
      const body = baselineResponse(url.pathname)!
      if (!body.data || !('permissionKeys' in body.data))
        throw new Error('Missing permission fixture')
      body.data.permissionKeys!.push('platform.deployment.view')
      return { body }
    }
    if (url.pathname === '/api/v1/auth/stream-ticket' && method === 'POST')
      return options.live
        ? {
            body: {
              data: { ticket: 'synthetic-stream-ticket', expiresAt: '2099-01-01T00:00:00Z' },
            },
          }
        : {
            status: 503,
            body: { error: { code: 'unavailable', message: 'Synthetic polling mode' } },
          }
    if (url.pathname === '/api/v1/clusters/cluster-a/workloads/deployments' && method === 'GET') {
      state.reads += 1
      const namespace = url.searchParams.get('namespace')
      state.namespaceReads.push(namespace)
      return state.status === 200
        ? {
            body: {
              data:
                options.allNamespaces && namespace
                  ? state.rows.filter((item) => item.namespace === namespace)
                  : state.rows,
            },
          }
        : {
            status: state.status,
            body: { error: { code: 'read_failed', message: 'Synthetic deployment read failure' } },
          }
    }
    if (url.pathname.endsWith('/rollouts'))
      return { body: { data: [{ revision: '3' }, { revision: '2' }] } }
    if (
      url.pathname.startsWith('/api/v1/clusters/cluster-a/workloads/deployments') &&
      method !== 'GET'
    ) {
      const body = request.postData() ? (request.postDataJSON() as Record<string, unknown>) : null
      state.writes.push({ method, path: url.pathname + url.search, body })
      if (
        (method === 'DELETE' && state.failDelete) ||
        (url.pathname.endsWith('/restart') && body?.name === state.failRestart) ||
        (url.pathname.endsWith('/scale') && body?.name === state.failScale)
      )
        return {
          status: method === 'DELETE' ? 500 : 409,
          body: { error: { code: 'write_failed', message: 'Synthetic deployment rejection' } },
        }
      return { body: { data: null } }
    }
    return undefined
  })
  await page.goto('/workloads/deployments')
  await expect(
    page.getByRole('status').filter({ hasText: `${state.rows.length} 个 Deployments` }),
  ).toBeVisible()
  return { state, evidence }
}
const row = (page: Page, name: string) =>
  page.locator(`.soha-deployments-table [data-row-key="monitoring/${name}"]`)
const trigger = (page: Page, name = 'deployment-000') =>
  page.getByRole('button', { name: `更多操作：${name}`, exact: true })
const menu = (page: Page) => page.getByRole('menu', { name: /^Deployment 操作：/ })
const batchMenu = (page: Page) => page.getByRole('menu', { name: /^Deployments 操作：/ })
async function openMenu(page: Page, name = 'deployment-000') {
  await trigger(page, name).click()
  await expect(menu(page)).toBeVisible()
}

test('second sample: supporting count, compact fixed menu, keyboard and responsive themes', async ({
  context,
  page,
}) => {
  const { state, evidence } = await setup(context, page, { live: true })
  const streamStatus = page.getByRole('status', { name: /^实时/ })
  await expect(streamStatus).toBeVisible()
  await expect(streamStatus).toHaveText('')
  await expect(streamStatus).toHaveAttribute('aria-label', /更新于/)
  await expect(streamStatus.locator('.ant-badge-status-success')).toBeVisible()
  await expect(page.getByRole('button', { name: '批量操作', exact: true })).toHaveCount(0)
  expect(await page.locator('.soha-deployments-table .ant-table-row').count()).toBeLessThan(80)
  const count = page.locator('.soha-kubernetes-resource-count')
  expect(
    await count.evaluate((node) => [
      getComputedStyle(node).fontSize,
      getComputedStyle(node).fontWeight,
    ]),
  ).toEqual(['12px', '400'])
  const first = trigger(page)
  const nameControl = row(page, 'deployment-000').getByRole('button', {
    name: 'deployment-000',
    exact: true,
  })
  await nameControl.focus()
  await expect(nameControl).toBeFocused()
  await first.press('ArrowDown')
  await expect(menu(page).getByRole('menuitem', { name: '编辑', exact: true })).toBeFocused()
  await page.keyboard.press('Escape')
  await expect(first).toBeFocused()
  await expect(menu(page)).toHaveCount(0)
  await openMenu(page)
  await expect(menu(page).getByRole('menuitem')).toHaveCount(5)
  await menu(page).getByRole('menuitem', { name: '删除', exact: true }).click()
  const dialog = page.getByRole('dialog', { name: '确认删除 deployment-000？' })
  await expect(dialog).toContainText('monitoring / deployment-000')
  expect(state.writes).toHaveLength(0)
  await dialog.getByRole('button', { name: /^取\s*消$/, exact: true }).click()
  for (const width of [1440, 768, 320]) {
    await page.setViewportSize({ width, height: 1000 })
    await expect(first).toBeVisible()
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true)
    const action = row(page, 'deployment-000').locator('.soha-table-actions-column')
    expect(Math.round((await action.boundingBox())!.width)).toBe(64)
  }
  await page.setViewportSize({ width: 1440, height: 1000 })
  await page.getByRole('button', { name: '切换到深色模式' }).click()
  await openMenu(page)
  await expect(menu(page).getByRole('menuitem', { name: '删除', exact: true })).toBeVisible()
  await page.keyboard.press('Tab')
  await expect(row(page, 'deployment-001').getByRole('checkbox')).toBeFocused()
  expect(state.writes).toHaveLength(0)
  verifyBrowserEvidence(evidence)
})

test('namespace tags switch Deployment scope, toolbar and query while clearing the previous selection', async ({
  context,
  page,
}) => {
  const { state, evidence } = await setup(context, page, { allNamespaces: true })
  const table = page.locator('.soha-deployments-table')
  await table.getByRole('checkbox').first().check()
  await expect(page.getByRole('button', { name: '批量操作', exact: true })).toBeVisible()
  const tag = table.getByRole('button', { name: '切换到命名空间 monitoring', exact: true })
  await expect(tag.locator('.ant-tag')).toHaveText('monitoring')
  await tag.click()
  await expect.poll(() => state.namespaceReads.at(-1)).toBe('monitoring')
  await expect(page.getByRole('group', { name: '资源范围' })).toContainText('monitoring')
  await expect(page.getByRole('status').filter({ hasText: '1 个 Deployments' })).toBeVisible()
  await expect(page.getByRole('button', { name: 'deployment-001', exact: true })).toHaveCount(0)
  await expect(page.getByRole('button', { name: '批量操作', exact: true })).toHaveCount(0)
  await expect(table.getByRole('columnheader', { name: '命名空间', exact: true })).toBeVisible()
  await expect(
    table.getByRole('button', { name: '切换到命名空间 monitoring', exact: true }),
  ).toBeVisible()
  const url = new URL(page.url())
  expect(url.searchParams.get('namespace')).toBe('monitoring')
  expect(url.searchParams.get('clusterId')).toBe('cluster-a')
  await page.reload()
  await expect(table.getByRole('columnheader', { name: '命名空间', exact: true })).toBeVisible()
  await expect(
    table.getByRole('button', { name: '切换到命名空间 monitoring', exact: true }),
  ).toBeVisible()
  expect(state.writes).toHaveLength(0)
  verifyBrowserEvidence(evidence)
})

test('row scale/restart/rollback and delete failure keep exact target, feedback and confirmation', async ({
  context,
  page,
}) => {
  const { state, evidence } = await setup(context, page)
  await openMenu(page)
  await menu(page).getByRole('menuitem', { name: '扩缩', exact: true }).click()
  const scale = page.getByRole('dialog', { name: '扩缩容', exact: true })
  await scale.getByRole('spinbutton').fill('4')
  await scale.getByRole('button', { name: /确\s*定|OK/, exact: true }).click()
  await expect.poll(() => state.writes.length).toBe(1)
  expect(state.writes[0]).toMatchObject({
    path: '/api/v1/clusters/cluster-a/workloads/deployments/scale',
    body: { name: 'deployment-000', namespace: 'monitoring', replicas: 4 },
  })
  await expect(scale).not.toBeVisible()
  for (const [label, path] of [
    ['重启', 'restart'],
    ['回滚', 'rollback'],
  ]) {
    await openMenu(page)
    await menu(page).getByRole('menuitem', { name: label, exact: true }).click()
    await expect
      .poll(() => state.writes.at(-1)?.path)
      .toBe(`/api/v1/clusters/cluster-a/workloads/deployments/${path}`)
    expect(state.writes.at(-1)?.body).toMatchObject({
      name: 'deployment-000',
      namespace: 'monitoring',
    })
    await expect(page.getByText(/请求已返回/).first()).toBeVisible()
  }
  state.failDelete = true
  await openMenu(page)
  await menu(page).getByRole('menuitem', { name: '删除', exact: true }).click()
  const dialog = page.getByRole('dialog', { name: '确认删除 deployment-000？' })
  await dialog.getByRole('button', { name: /^删\s*除$/, exact: true }).click()
  await expect(dialog).toContainText('Synthetic deployment rejection')
  await expect(dialog).toBeVisible()
  expect(state.writes.at(-1)).toMatchObject({
    method: 'DELETE',
    path: '/api/v1/clusters/cluster-a/workloads/deployments/deployment-000?namespace=monitoring&expectedUid=deployment-uid-0',
  })
  verifyBrowserEvidence(evidence)
})

test('selection-only bulk menu covers full matching collection and retains rejected items', async ({
  context,
  page,
}) => {
  const { state, evidence } = await setup(context, page)
  state.failRestart = 'deployment-079'
  await page
    .getByRole('checkbox', { name: '选择全部匹配的 80 个 Deployments', exact: true })
    .check()
  await expect(page.getByRole('status').filter({ hasText: /^已选 80 个$/ })).toBeVisible()
  await page.getByRole('button', { name: '批量操作', exact: true }).click()
  await batchMenu(page).getByRole('menuitem', { name: '批量重启', exact: true }).click()
  await expect.poll(() => state.writes.length).toBe(80)
  await expect(page.getByRole('status').filter({ hasText: /^已选 1 个$/ })).toBeVisible()
  expect(new Set(state.writes.map((item) => item.body?.name)).size).toBe(80)
  await expect(
    page.getByText('批量请求已返回：79 项成功，1 项失败或待核对', { exact: true }),
  ).toBeVisible()
  await page.getByRole('button', { name: '清除', exact: true }).click()
  await expect(page.getByRole('button', { name: '批量操作', exact: true })).toHaveCount(0)
  verifyBrowserEvidence(evidence)
})

test('filtered count, refresh anchor and cached read failure remain truthful', async ({
  context,
  page,
}) => {
  const { state, evidence } = await setup(context, page)
  const holder = page.getByRole('region', { name: '资源列表，可连续滚动' })
  await holder.evaluate((node) => {
    node.scrollTop = 1000
    node.dispatchEvent(new Event('scroll'))
  })
  const before = await holder.evaluate((node) => node.scrollTop)
  const reads = state.reads
  await page.getByRole('button', { name: '刷新', exact: true }).click()
  await expect.poll(() => state.reads).toBeGreaterThan(reads)
  await expect.poll(() => holder.evaluate((node) => node.scrollTop)).toBe(before)
  await page.getByPlaceholder('搜索 Deployment 名称').fill('deployment-079')
  await expect(
    page.getByRole('status').filter({ hasText: '匹配 1 / 共 80 个 Deployments' }),
  ).toBeVisible()
  await expect(row(page, 'deployment-079')).toBeVisible()
  state.status = 500
  await page.getByRole('button', { name: '刷新', exact: true }).click()
  await expect(
    page.getByRole('alert').filter({ hasText: 'Synthetic deployment read failure' }),
  ).toBeVisible()
  await expect(row(page, 'deployment-079')).toBeVisible()
  await expect(
    page.getByRole('status').filter({ hasText: '匹配 1 / 共 80 个 Deployments' }),
  ).toBeVisible()
  verifyBrowserEvidence(evidence)
})

test('read-only and unavailable capability do not expose usable mutations', async ({
  context,
  page,
}) => {
  const { state, evidence } = await setup(context, page, { readonly: true })
  await expect(page.getByRole('button', { name: /更多操作：/ })).toHaveCount(0)
  await expect(page.getByRole('checkbox', { name: /选择全部匹配/ })).toHaveCount(0)
  expect(state.writes).toHaveLength(0)
  verifyBrowserEvidence(evidence)
})

test('unavailable actions show reasons while empty data remains a successful empty scope', async ({
  context,
  page,
}) => {
  const { state, evidence } = await setup(context, page, { unsupported: true })
  await openMenu(page)
  const items = menu(page).getByRole('menuitem')
  await expect(items).toHaveCount(5)
  for (const item of await items.all()) await expect(item).toHaveAttribute('aria-disabled', 'true')
  await expect(menu(page)).toContainText('Synthetic unavailable capability')
  await page.keyboard.press('Escape')
  state.rows = []
  await page.getByRole('button', { name: '刷新', exact: true }).click()
  await expect(page.getByRole('status').filter({ hasText: '0 个 Deployments' })).toBeVisible()
  await expect(page.getByText('当前范围没有 Deployments', { exact: true })).toBeVisible()
  expect(state.writes).toHaveLength(0)
  verifyBrowserEvidence(evidence)
})

test('mixed permissions and filters restrict bulk actions to current visible selected targets', async ({
  context,
  page,
}) => {
  const { state, evidence } = await setup(context, page)
  state.rows = state.rows.slice(0, 2)
  state.rows[0].allowedActions = ['view', 'restart']
  await page.getByRole('button', { name: '刷新', exact: true }).click()
  await expect(page.getByRole('status').filter({ hasText: '2 个 Deployments' })).toBeVisible()
  await openMenu(page)
  await expect(menu(page).getByRole('menuitem')).toHaveCount(1)
  await expect(menu(page).getByRole('menuitem', { name: '重启', exact: true })).toBeEnabled()
  await page.keyboard.press('Escape')
  await page.getByRole('checkbox', { name: '选择全部匹配的 2 个 Deployments', exact: true }).check()
  await page.getByRole('button', { name: '批量操作', exact: true }).click()
  await expect(batchMenu(page).getByRole('menuitem', { name: /批量扩缩/ })).toHaveAttribute(
    'aria-disabled',
    'true',
  )
  await expect(batchMenu(page)).toContainText('部分所选资源不允许此操作')
  await page.keyboard.press('Escape')
  await page.getByPlaceholder('搜索 Deployment 名称').fill('deployment-001')
  await expect(page.getByRole('status').filter({ hasText: /^已选 1 个$/ })).toBeVisible()
  await page.getByRole('button', { name: '批量操作', exact: true }).click()
  await batchMenu(page).getByRole('menuitem', { name: '批量重启', exact: true }).click()
  await expect.poll(() => state.writes.length).toBe(1)
  expect(state.writes[0].body).toMatchObject({ name: 'deployment-001', namespace: 'monitoring' })
  await expect(page.getByRole('button', { name: '批量操作', exact: true })).toHaveCount(0)
  verifyBrowserEvidence(evidence)
})

test('batch scale retries only rejected targets after partial success', async ({
  context,
  page,
}) => {
  const { state, evidence } = await setup(context, page)
  state.rows = state.rows.slice(0, 2)
  state.failScale = 'deployment-001'
  await page.getByRole('button', { name: '刷新', exact: true }).click()
  await expect(page.getByRole('status').filter({ hasText: '2 个 Deployments' })).toBeVisible()
  await page.getByRole('checkbox', { name: '选择全部匹配的 2 个 Deployments', exact: true }).check()
  await page.getByRole('button', { name: '批量操作', exact: true }).click()
  await batchMenu(page).getByRole('menuitem', { name: '批量扩缩', exact: true }).click()
  const dialog = page.getByRole('dialog', { name: '批量扩缩容', exact: true })
  await expect(dialog).toContainText('将对 2 个 Deployment 应用相同副本数')
  await dialog.getByRole('button', { name: /确\s*定|OK/, exact: true }).click()
  await expect.poll(() => state.writes.length).toBe(2)
  await expect(dialog).toContainText('将对 1 个 Deployment 应用相同副本数')
  await expect(page.getByRole('status').filter({ hasText: /^已选 1 个$/ })).toBeVisible()
  state.failScale = ''
  await dialog.getByRole('button', { name: /确\s*定|OK/, exact: true }).click()
  await expect.poll(() => state.writes.length).toBe(3)
  expect(state.writes.map((write) => write.body?.name)).toEqual([
    'deployment-000',
    'deployment-001',
    'deployment-001',
  ])
  await expect(dialog).not.toBeVisible()
  await expect(page.getByRole('button', { name: '批量操作', exact: true })).toHaveCount(0)
  verifyBrowserEvidence(evidence)
})

test('batch rollback ignores revision reads from a cancelled draft in another scope', async ({
  context,
  page,
}) => {
  const { state, evidence } = await setup(context, page, { allNamespaces: true })
  let releaseOldRead!: () => void
  const oldRead = new Promise<void>((resolve) => {
    releaseOldRead = resolve
  })
  await page.route('**/workloads/deployments/*/rollouts?*', async (route) => {
    const old = route.request().url().includes('/deployment-000/')
    if (old) await oldRead
    await route.fulfill({ json: { data: [{ revision: '3' }, { revision: old ? '11' : '22' }] } })
  })
  const openRollback = async () => {
    await page.getByRole('button', { name: '批量操作', exact: true }).click()
    await batchMenu(page).getByRole('menuitem', { name: '批量回滚', exact: true }).click()
  }
  const dialog = page.getByRole('dialog', { name: '批量回滚', exact: true })
  const confirm = dialog.getByRole('button', { name: /确\s*定|OK/, exact: true })
  await row(page, 'deployment-000').getByRole('checkbox').check()
  const oldRequest = page.waitForRequest((request) =>
    request.url().includes('/deployment-000/rollouts?'),
  )
  await openRollback()
  await oldRequest
  await expect(confirm).toBeDisabled()
  await dialog.getByRole('button', { name: /^取\s*消$|^Cancel$/ }).click()
  await page.getByRole('button', { name: '切换到命名空间 kube-system', exact: true }).click()
  await expect.poll(() => state.namespaceReads.at(-1)).toBe('kube-system')
  await page.locator('[data-row-key="kube-system/deployment-001"]').getByRole('checkbox').check()
  await openRollback()
  await expect(dialog).toContainText('deployment-001')
  await expect(dialog).toContainText('22')
  const oldResponse = page.waitForResponse((response) =>
    response.url().includes('/deployment-000/rollouts?'),
  )
  releaseOldRead()
  await (await oldResponse).finished()
  await expect(dialog).not.toContainText('deployment-000')
  await confirm.click()
  await expect
    .poll(() => state.writes.map((write) => write.body))
    .toEqual([{ name: 'deployment-001', namespace: 'kube-system', revision: '22' }])
  verifyBrowserEvidence(evidence)
})
