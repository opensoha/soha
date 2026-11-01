import { test, expect, type BrowserContext, type Page } from '@playwright/test'
import { baselineResponse } from '../fixtures/baseline-data'
import { installMocks, verifyBrowserEvidence } from '../fixtures/mock-app'

const root = '/api/v1/clusters/cluster-a'
const path = '/cluster-resources/namespaces'
async function setup(
  context: BrowserContext,
  page: Page,
  { readonly = false, noPods = false, count = 60 } = {},
) {
  const state = {
    rows: Array.from({ length: count }, (_, i) => ({
      name: `team-${String(i).padStart(3, '0')}`,
      status: i === 1 ? 'Terminating' : 'Active',
      ageSeconds: i === 2 ? undefined : i === 0 ? 0 : 86400 + i,
      labels: {
        'kubernetes.io/metadata.name': `team-${i}`,
        owner: `platform-${i}`,
        'example.test/long-label': 'long-value-'.repeat(15),
      },
      allowedActions: readonly ? ['view'] : ['view', 'update', 'delete'],
    })),
    failPods: 0,
    failNamespaces: false,
    failWrite: 0,
    reads: [] as string[],
    writes: [] as string[],
  }
  const pods = [
    { name: 'app', phase: 'Running', readyContainers: '1/1' },
    { name: 'completed', phase: 'Succeeded', readyContainers: '0/1' },
    { name: 'pending', phase: 'Pending', readyContainers: '0/1' },
    {
      name: 'init-completed',
      phase: 'Running',
      readyContainers: '1/1',
      containerStatuses: [
        { name: 'init', kind: 'init', state: 'terminated', ready: false, exitCode: 0 },
      ],
    },
  ].map((pod) => ({
    ...pod,
    namespace: 'team-000',
    ageSeconds: 600,
    restarts: 0,
    nodeName: 'worker-a',
  }))
  await context.addInitScript(() =>
    localStorage.setItem(
      'soha-scope',
      JSON.stringify({ state: { clusterId: 'cluster-a', namespace: 'monitoring' }, version: 0 }),
    ),
  )
  const evidence = await installMocks(context, page, (url, method) => {
    if (method === 'GET') state.reads.push(url.pathname + url.search)
    else state.writes.push(method + ' ' + url.pathname + url.search)
    if (url.pathname === '/api/v1/access/permission-snapshot') {
      const body = baselineResponse(url.pathname)!
      if (!('permissionKeys' in body.data)) throw new Error('Missing permission fixture')
      body.data.permissionKeys!.push(
        'platform.namespaces.view',
        'platform.workloads.overview.view',
        ...(noPods ? [] : ['platform.pods.view']),
        ...(readonly ? [] : ['platform.namespaces.create']),
      )
      return { body }
    }
    if (url.pathname === root + '/namespaces' && method === 'GET')
      return state.failNamespaces
        ? {
            status: 503,
            body: { error: { code: 'unavailable', message: 'Namespace read failed' } },
          }
        : { body: { data: state.rows } }
    if (url.pathname.startsWith(root + '/namespaces') && method !== 'GET')
      return state.failWrite
        ? {
            status: state.failWrite,
            body: { error: { code: 'failed', message: 'Namespace write failed' } },
          }
        : { body: { data: {} } }
    if (url.pathname === root + '/workloads/pods')
      return state.failPods
        ? {
            status: state.failPods,
            body: { error: { code: 'unavailable', message: 'Pod statistics unavailable' } },
          }
        : { body: { items: pods } }
    if (
      /^\/api\/v1\/clusters\/cluster-a\/workloads\/(overview|deployments|statefulsets|daemonsets|jobs|cronjobs|replicasets|replicationcontrollers)$/.test(
        url.pathname,
      )
    )
      return { body: { items: [], data: {} } }
    if (url.pathname === '/api/v1/application-environments') return { body: { data: [] } }
    if (url.pathname === '/api/v1/delivery/manifest-deployments') return { body: { items: [] } }
    return undefined
  })
  page.on('pageerror', (error) => evidence.fatal.push(error.stack || error.message))
  return { state, evidence }
}

const rows = (page: Page) => page.locator('.soha-namespaces-list .ant-table-row')
const query = (page: Page) => page.getByPlaceholder('搜索命名空间 / 标签')
const refresh = (page: Page) => page.getByRole('button', { name: '刷新', exact: true })

test('Namespace continuous list, accurate zero/visible Pod counts, native phase and label search', async ({
  context,
  page,
}) => {
  const { state, evidence } = await setup(context, page)
  await page.setViewportSize({ width: 1373, height: 1502 })
  await page.goto(path)
  await expect(page.getByRole('status').filter({ hasText: '60 个 Namespaces' })).toBeVisible()
  await expect(page.locator('.ant-pagination')).toHaveCount(0)
  await expect(page.locator('.ant-table-tbody-virtual-holder')).toBeVisible()
  await expect(rows(page).first()).toContainText('0m')
  await expect(rows(page).first()).toContainText('4')
  await expect(rows(page).nth(2)).toContainText('—')
  await expect(rows(page).nth(1)).toContainText('终止中')
  expect(state.reads.filter((p) => p.startsWith(root + '/workloads/pods'))).toEqual([
    root + '/workloads/pods',
  ])
  await query(page).fill('OWNER=PLATFORM-59')
  await expect(rows(page)).toHaveCount(1)
  await expect(rows(page).first()).toContainText('team-059')
  await refresh(page).click()
  await expect(query(page)).toHaveValue('OWNER=PLATFORM-59')
  await expect(rows(page).first()).toContainText('team-059')
  state.rows.reverse()
  await page.getByRole('button', { name: '重置', exact: true }).click()
  await refresh(page).click()
  await expect(rows(page).first()).toContainText('team-000')
  await page.getByRole('combobox', { name: '命名空间状态' }).click()
  await page.getByText('Terminating', { exact: true }).last().click()
  await expect(rows(page)).toHaveCount(1)
  await expect(rows(page).first()).toContainText('team-001')
  verifyBrowserEvidence(evidence)
})

test('Namespace labels reveal full copyable values with keyboard and preserve row alignment', async ({
  context,
  page,
}) => {
  const { evidence } = await setup(context, page, { count: 2 })
  await page.goto(path)
  const trigger = page.getByRole('button', { name: '查看全部 3 个标签' }).first()
  await trigger.scrollIntoViewIfNeeded()
  await trigger.focus()
  await page.keyboard.press('Enter')
  await expect(page.locator('.soha-namespace-label-details')).toContainText(
    'long-value-'.repeat(15),
  )
  await expect(page.locator('.soha-namespace-label-details .ant-typography-copy')).toHaveCount(3)
  const overflow = await page
    .locator('.soha-namespace-label-details')
    .evaluate((el) => el.scrollWidth > el.clientWidth + 1)
  expect(overflow).toBe(false)
  await page.keyboard.press('Escape')
  await expect(page.locator('.soha-namespace-label-details')).toBeHidden()
  await expect(trigger).toBeFocused()
  await page.screenshot({
    path: '../preview/runtime-private/ns01-namespaces/synthetic-list.png',
    fullPage: true,
  })
  verifyBrowserEvidence(evidence)
})

test('Namespace name updates the global scope and opens the workload overview', async ({
  context,
  page,
}) => {
  const { evidence } = await setup(context, page, { count: 2 })
  await page.goto(path)
  await page.getByRole('button', { name: 'team-000', exact: true }).click()
  await expect(page).toHaveURL(/\/workloads\/overview\?clusterId=cluster-a&namespace=team-000/)
  expect(
    await page.evaluate(() => JSON.parse(localStorage.getItem('soha-scope')!).state.namespace),
  ).toBe('team-000')
  verifyBrowserEvidence(evidence)
})

test('Pod permission absence never becomes fabricated zero counts', async ({ context, page }) => {
  const { state, evidence } = await setup(context, page, { noPods: true, readonly: true, count: 2 })
  await page.goto(path)
  await expect(rows(page).first()).toContainText('—')
  expect(state.reads.some((p) => p.includes('/workloads/pods'))).toBe(false)
  await expect(
    page.locator('.soha-namespaces-list').getByRole('button', { name: /创建$/ }),
  ).toHaveCount(0)
  verifyBrowserEvidence(evidence)
})

test('Pod read error retains namespace metadata and recovers counts after retry', async ({
  context,
  page,
}) => {
  const { state, evidence } = await setup(context, page, { count: 2 })
  await page.goto(path)
  await expect(rows(page).first().getByRole('button', { name: '4', exact: true })).toBeVisible()
  state.failPods = 503
  await refresh(page).click()
  await expect(page.getByText('Pod 统计读取失败', { exact: true })).toBeVisible()
  await expect(rows(page).first().getByRole('button', { name: '4', exact: true })).toHaveCount(0)
  await expect(page.getByRole('button', { name: 'team-000', exact: true })).toBeVisible()
  state.failPods = 0
  await page.getByRole('button', { name: '重试读取Pod 统计' }).click()
  await expect(rows(page).first().getByRole('button', { name: '4', exact: true })).toBeVisible()
  state.failPods = 403
  await refresh(page).click()
  await expect(page.getByText('Pod 统计无访问权限', { exact: true })).toBeVisible()
  await expect(rows(page).first().getByRole('button', { name: '4', exact: true })).toHaveCount(0)
  verifyBrowserEvidence(evidence)
})

test('Refresh keeps scroll anchor and edit draft; delete requires confirmation', async ({
  context,
  page,
}) => {
  const { state, evidence } = await setup(context, page)
  await page.clock.install()
  await page.goto(path)
  await page.locator('.ant-table-tbody-virtual-holder').evaluate((el) => {
    el.scrollTop = 700
  })
  await expect
    .poll(() => page.locator('.ant-table-tbody-virtual-holder').evaluate((el) => el.scrollTop))
    .toBeGreaterThan(600)
  const before = await page
    .locator('.ant-table-tbody-virtual-holder')
    .evaluate((el) => el.scrollTop)
  await refresh(page).click()
  await expect
    .poll(() =>
      page
        .locator('.ant-table-tbody-virtual-holder')
        .evaluate((el, before) => Math.abs(el.scrollTop - before), before),
    )
    .toBeLessThan(2)
  await query(page).fill('team-000')
  await page.getByRole('button', { name: '更多操作：team-000' }).click()
  await page.getByRole('menuitem', { name: '编辑', exact: true }).click()
  await page.getByRole('textbox', { name: 'Labels(JSON)' }).fill('{"owner":"draft"}')
  state.rows[0].labels.owner = 'changed-on-server'
  const readsBefore = state.reads.filter((p) => p === root + '/namespaces').length
  await page.clock.fastForward(16000)
  await expect
    .poll(() => state.reads.filter((p) => p === root + '/namespaces').length)
    .toBeGreaterThan(readsBefore)
  await expect(page.getByRole('textbox', { name: 'Labels(JSON)' })).toHaveValue('{"owner":"draft"}')
  await page
    .getByRole('dialog')
    .getByRole('button', { name: /^取\s*消$/ })
    .click()
  await refresh(page).click()
  await page.getByRole('button', { name: '更多操作：team-000' }).click()
  await page.getByRole('menuitem', { name: '删除', exact: true }).click()
  await expect(page.getByRole('dialog')).toContainText('该命名空间下的资源会一并回收')
  expect(state.writes.filter((p) => p.startsWith('DELETE'))).toHaveLength(0)
  await page
    .getByRole('dialog')
    .getByRole('button', { name: /^取\s*消$/ })
    .click()
  expect(state.writes.filter((p) => p.startsWith('DELETE'))).toHaveLength(0)
  verifyBrowserEvidence(evidence)
})

test('Namespace dark narrow layout stays inside viewport and controls remain reachable', async ({
  context,
  page,
}) => {
  const { evidence } = await setup(context, page, { count: 2 })
  await page.addInitScript(() =>
    localStorage.setItem(
      'soha-prefs',
      JSON.stringify({ state: { themeMode: 'dark' }, version: 0 }),
    ),
  )
  await page.setViewportSize({ width: 900, height: 800 })
  await page.goto(path)
  await expect(page.getByRole('button', { name: 'team-000', exact: true })).toBeVisible()
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth + 1)).toBe(
    true,
  )
  await query(page).fill('platform-1')
  await expect(rows(page)).toHaveCount(1)
  await expect(rows(page).first()).toContainText('team-001')
  verifyBrowserEvidence(evidence)
})

test('Namespace read and write errors preserve metadata, drafts and destructive confirmation', async ({
  context,
  page,
}) => {
  const { state, evidence } = await setup(context, page, { count: 2 })
  await page.goto(path)
  await expect(page.getByRole('button', { name: 'team-000', exact: true })).toBeVisible()
  state.failNamespaces = true
  await refresh(page).click()
  await expect(page.getByText('Namespace read failed', { exact: false }).first()).toBeVisible()
  await expect(page.getByRole('button', { name: 'team-000', exact: true })).toBeVisible()
  state.failNamespaces = false
  await refresh(page).click()
  await page.locator('.soha-namespaces-list').getByRole('button', { name: /创建$/ }).click()
  await page.getByRole('textbox', { name: '命名空间名称' }).fill('demo-new')
  await page
    .getByRole('dialog')
    .getByRole('button', { name: /^创\s*建$/ })
    .click()
  await expect(page.getByRole('dialog')).toHaveCount(0)
  expect(state.writes.filter((p) => p === 'POST ' + root + '/namespaces')).toHaveLength(1)
  await page.getByRole('button', { name: '更多操作：team-000' }).click()
  await page.getByRole('menuitem', { name: '编辑', exact: true }).click()
  await page.getByRole('textbox', { name: 'Labels(JSON)' }).fill('{"owner":"updated"}')
  state.failWrite = 409
  await page
    .getByRole('dialog')
    .getByRole('button', { name: /^保\s*存$/ })
    .click()
  await expect(page.getByRole('dialog')).toContainText('Namespace write failed')
  await expect(page.getByRole('textbox', { name: 'Labels(JSON)' })).toHaveValue(
    '{"owner":"updated"}',
  )
  state.failWrite = 0
  await page
    .getByRole('dialog')
    .getByRole('button', { name: /^保\s*存$/ })
    .click()
  await expect(page.getByRole('dialog')).toHaveCount(0)
  await page.getByRole('button', { name: '更多操作：team-000' }).click()
  await page.getByRole('menuitem', { name: '删除', exact: true }).click()
  state.failWrite = 500
  await page.getByRole('dialog').getByRole('button', { name: '删除', exact: true }).click()
  await expect(page.getByRole('dialog')).toContainText('Namespace write failed')
  await expect(
    page.getByRole('dialog').getByRole('button', { name: '删除', exact: true }),
  ).toBeDisabled()
  await expect(page.getByRole('dialog')).toContainText('普通刷新不会确认或解锁原请求')
  await expect(page.getByRole('button', { name: 'team-000', exact: true })).toBeVisible()
  expect(state.writes.filter((p) => p === 'DELETE ' + root + '/namespaces/team-000')).toHaveLength(
    1,
  )
  await page
    .getByRole('dialog')
    .getByRole('button', { name: /^取\s*消$/ })
    .click()
  verifyBrowserEvidence(evidence)
})

test('Namespace name sorting initializes before a slow first response', async ({
  context,
  page,
}) => {
  const { state, evidence } = await setup(context, page, { count: 3 })
  let release!: () => void
  const pending = new Promise<void>((resolve) => {
    release = resolve
  })
  await context.route('**/api/v1/clusters/cluster-a/namespaces', async (route) => {
    evidence.requests.push(root + '/namespaces')
    state.reads.push(root + '/namespaces')
    await pending
    await route.fulfill({ json: { data: [...state.rows].reverse() } })
  })
  await page.goto(path)
  await expect(page.locator('.soha-namespaces-list')).toBeVisible()
  await expect(rows(page)).toHaveCount(0)
  release()
  await expect(rows(page).first()).toContainText('team-000')
  await expect(page.getByRole('columnheader', { name: '名称', exact: true })).toHaveAttribute(
    'aria-sort',
    'ascending',
  )
  verifyBrowserEvidence(evidence)
})
