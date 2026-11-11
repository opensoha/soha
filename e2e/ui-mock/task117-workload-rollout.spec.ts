import { test, expect, type Page, type BrowserContext } from '@playwright/test'
import { baselineResponse } from '../fixtures/baseline-data'
import { installMocks, verifyBrowserEvidence } from '../fixtures/mock-app'

const kinds = [
  ['statefulsets', 'StatefulSets', 'stateful-sets'],
  ['daemonsets', 'DaemonSets', 'daemon-sets'],
  ['jobs', 'Jobs', 'jobs'],
  ['cronjobs', 'CronJobs', 'cron-jobs'],
  ['replicasets', 'ReplicaSets', 'replica-sets'],
  ['replicationcontrollers', 'ReplicationControllers', 'replication-controllers'],
] as const
async function setup(context: BrowserContext, page: Page, kind: string, readonly = false) {
  const state = {
    reads: 0,
    readStatus: 200,
    pendingRead: undefined as Promise<void> | undefined,
    fail: false,
    writes: [] as string[],
    rows: Array.from({ length: 120 }, (_, index) => ({
      name: `resource-${String(index).padStart(3, '0')}`,
      namespace: 'monitoring',
      uid: `uid-${index}`,
      allowedActions: readonly
        ? ['view']
        : ['view', 'update', 'restart', 'scale', 'suspend', 'delete'],
      desiredReplicas: 3,
      readyReplicas: 2,
      currentReplicas: 3,
      availableReplicas: 2,
      desiredNumber: 3,
      readyNumber: 2,
      currentNumber: 3,
      availableNumber: 2,
      updatedNumber: 2,
      serviceName: 'database',
      completions: 3,
      succeeded: 2,
      failed: 0,
      active: 1,
      schedule: '*/5 * * * *',
      suspend: false,
      activeJobs: 1,
      ageSeconds: 3600,
    })),
  }
  await context.addInitScript(() =>
    localStorage.setItem(
      'soha-scope',
      JSON.stringify({ state: { clusterId: 'cluster-a', namespace: 'monitoring' }, version: 0 }),
    ),
  )
  const evidence = await installMocks(context, page, async (url, method, request) => {
    if (url.pathname === '/api/v1/access/permission-snapshot') {
      const body = baselineResponse(url.pathname)!
      if (!body.data || !('permissionKeys' in body.data)) throw new Error('Missing permissions')
      body.data.permissionKeys!.push(
        'platform.workloads.overview.view',
        ...kinds.map((item) => `platform.workloads.${item[2]}.view`),
      )
      return { body }
    }
    if (url.pathname === '/api/v1/clusters/capabilities')
      return {
        body: {
          data: ['workload.mutations', 'resource.yaml.apply'].map((key) => ({
            key,
            direct: { status: 'available' },
            agent: { status: 'available' },
          })),
        },
      }
    if (url.pathname.endsWith('/resource-creation/scope-decision'))
      return { body: { data: { allowed: !readonly, scopeMode: 'namespace', reason: '' } } }
    if (url.pathname === '/api/v1/auth/stream-ticket')
      return { status: 403, body: { error: { code: 'denied', message: 'Synthetic polling' } } }
    if (
      url.pathname === `/api/v1/clusters/cluster-a/workloads/${kind}` &&
      url.search === '?namespace=monitoring' &&
      method === 'GET'
    ) {
      state.reads++
      if (state.pendingRead) await state.pendingRead
      if (state.readStatus !== 200)
        return {
          status: state.readStatus,
          body: { error: { code: 'unavailable', message: 'Synthetic workload unavailable' } },
        }
      return { body: { data: state.rows } }
    }
    if (url.pathname.endsWith('/cronjobs/resource-000/suspend') && method === 'POST') {
      const body = request.postDataJSON() as { suspend: boolean }
      state.writes.push(`${url.pathname + url.search} suspend=${body.suspend}`)
      state.rows[0].suspend = body.suspend
      return { body: { data: state.rows[0] } }
    }
    if (
      url.pathname.startsWith(`/api/v1/clusters/cluster-a/workloads/${kind}/`) &&
      method === 'DELETE'
    ) {
      state.writes.push(url.pathname + url.search)
      if (state.fail)
        return {
          status: 409,
          body: { error: { code: 'rejected', message: 'Synthetic deletion rejected' } },
        }
      return { body: { data: null } }
    }
    return undefined
  })
  return { state, evidence }
}

test('DaemonSets: virtual text, namespace and actions stay centered at both densities', async ({
  context,
  page,
}) => {
  const { evidence } = await setup(context, page, 'daemonsets')
  await page.goto('/workloads/daemonsets')
  await expect(page.getByRole('status').filter({ hasText: '120 个 DaemonSets' })).toBeVisible()
  const row = page.locator('.ant-table-row[data-row-key]').first()
  const measure = () =>
    row.evaluate((element) => {
      const rowRect = element.getBoundingClientRect()
      return {
        height: rowRect.height,
        cells: Array.from(element.children).flatMap((cell) => {
          if (!cell.textContent?.trim() && !cell.querySelector('button')) return []
          const range = document.createRange()
          range.selectNodeContents(cell)
          const rect = range.getBoundingClientRect()
          return [
            {
              text: cell.textContent,
              offset: rect.y + rect.height / 2 - (rowRect.y + rowRect.height / 2),
            },
          ]
        }),
      }
    })
  const compact = await measure()
  expect(compact.cells.length).toBeGreaterThanOrEqual(8)
  for (const cell of compact.cells)
    expect(Math.abs(cell.offset), cell.text ?? 'actions').toBeLessThanOrEqual(2)
  await page.getByRole('button', { name: '切换表格密度', exact: true }).click()
  await expect.poll(async () => (await measure()).height).toBeGreaterThan(compact.height)
  for (const cell of (await measure()).cells)
    expect(Math.abs(cell.offset), cell.text ?? 'actions').toBeLessThanOrEqual(2)
  await page.getByRole('button', { name: '切换表格密度', exact: true }).click()
  await expect.poll(async () => (await measure()).height).toBe(compact.height)
  verifyBrowserEvidence(evidence)
})

for (const [kind, label] of kinds)
  test(`${label}: continuous list, stable refresh, fixed menu and exact deletion`, async ({
    context,
    page,
  }, info) => {
    const { state, evidence } = await setup(context, page, kind)
    await page.goto(`/workloads/${kind}`)
    await expect(page.getByRole('status').filter({ hasText: `120 个 ${label}` })).toBeVisible()
    await expect(page.locator('.ant-pagination')).toHaveCount(0)
    expect(await page.locator('.ant-table-row[data-row-key]').count()).toBeLessThan(120)
    const trigger = page.getByRole('button', { name: '更多操作：resource-000', exact: true })
    await trigger.press('ArrowDown')
    const menu = page.getByRole('menu', { name: / 操作：/ })
    await expect(menu).toBeVisible()
    await menu.getByRole('menuitem', { name: '删除', exact: true }).click()
    const dialog = page.getByRole('dialog')
    await expect(dialog).toContainText('monitoring / resource-000')
    expect(state.writes).toEqual([])
    const region = page.getByRole('region', { name: '资源列表，可连续滚动', exact: true })
    await region.evaluate((node) => {
      node.scrollTop = 1800
      node.dispatchEvent(new Event('scroll', { bubbles: true }))
    })
    await expect(trigger).toHaveCount(0)
    await expect(dialog).toBeVisible()
    await dialog.getByRole('button', { name: /^取\s*消$/ }).click()
    expect(state.writes).toEqual([])
    const before = await region.evaluate((node) => node.scrollTop)
    const reads = state.reads
    await page.getByRole('button', { name: '刷新', exact: true }).click()
    await expect.poll(() => state.reads).toBeGreaterThan(reads)
    await expect.poll(() => region.evaluate((node) => node.scrollTop)).toBe(before)
    await region.focus()
    await region.press('Home')
    await trigger.click()
    await page.getByRole('menuitem', { name: '删除', exact: true }).click()
    state.fail = true
    await dialog.getByRole('button', { name: /^删\s*除$/ }).click()
    await expect(dialog).toContainText('Synthetic deletion rejected')
    state.fail = false
    await dialog.getByRole('button', { name: /^删\s*除$/ }).click()
    await expect(dialog).toHaveCount(0)
    expect(state.writes).toEqual(
      Array(2).fill(
        `/api/v1/clusters/cluster-a/workloads/${kind}/resource-000?namespace=monitoring`,
      ),
    )
    await region.focus()
    await region.press('End')
    await expect(
      page.getByRole('button', { name: '更多操作：resource-119', exact: true }),
    ).toBeVisible()
    await page.setViewportSize({ width: 900, height: 800 })
    await page.getByLabel('切换到深色模式').click()
    expect(await page.evaluate(() => document.documentElement.scrollWidth > innerWidth)).toBe(false)
    await page.screenshot({
      path: info.outputPath(`${kind}-dark.png`),
      fullPage: true,
      animations: 'disabled',
    })
    verifyBrowserEvidence(evidence)
  })
for (const [kind, label] of kinds)
  test(`${label}: shared automatic refresh pauses, preserves position and stops after failure`, async ({
    context,
    page,
  }) => {
    const { state, evidence } = await setup(context, page, kind)
    await page.goto(`/workloads/${kind}`)
    await expect(page.getByRole('status').filter({ hasText: `120 个 ${label}` })).toBeVisible()
    const toggle = page.getByRole('switch', { name: /自动刷新/ })
    const manual = page.getByRole('button', { name: '刷新', exact: true })
    await expect(toggle).toHaveAttribute('data-refresh-state', 'live')
    const region = page.getByRole('region', { name: '资源列表，可连续滚动', exact: true })
    await region.evaluate((node) => {
      node.scrollTop = 1800
      node.dispatchEvent(new Event('scroll', { bubbles: true }))
    })
    const top = await region.evaluate((node) => node.scrollTop)
    await page.clock.install()
    await toggle.focus()
    await page.keyboard.press('Space')
    await expect(toggle).not.toBeChecked()
    await expect(toggle).toHaveAttribute('data-refresh-state', 'paused')
    let before = state.reads
    await page.clock.fastForward(30_100)
    expect(state.reads).toBe(before)
    await toggle.click()
    await page.clock.fastForward(15_100)
    await expect.poll(() => state.reads).toBeGreaterThan(before)
    await expect(toggle).toHaveAttribute('data-refresh-state', 'live')
    await expect.poll(() => region.evaluate((node) => node.scrollTop)).toBe(top)
    let release!: () => void
    state.pendingRead = new Promise<void>((resolve) => {
      release = resolve
    })
    await manual.click()
    await expect(toggle).toHaveAttribute('data-refresh-state', 'refreshing')
    await expect(toggle.locator('.ant-badge-status-warning')).toHaveCount(1)
    state.readStatus = 503
    release()
    state.pendingRead = undefined
    await expect(toggle).toHaveAttribute('data-refresh-state', 'failed')
    await expect(toggle.locator('.ant-badge-status-error')).toHaveCount(1)
    await expect(page.getByRole('status').filter({ hasText: `120 个 ${label}` })).toBeVisible()
    await expect(
      page.getByText('Synthetic workload unavailable', { exact: false }).first(),
    ).toBeVisible()
    before = state.reads
    await page.clock.fastForward(60_100)
    expect(state.reads).toBe(before)
    state.readStatus = 200
    await manual.click()
    await expect(toggle).toHaveAttribute('data-refresh-state', 'live')
    before = state.reads
    await page.clock.fastForward(15_100)
    await expect.poll(() => state.reads).toBeGreaterThan(before)
    await expect.poll(() => region.evaluate((node) => node.scrollTop)).toBe(top)
    expect(state.writes).toEqual([])
    verifyBrowserEvidence(evidence)
  })

test('read-only workload has no mutation menu', async ({ context, page }) => {
  const { state, evidence } = await setup(context, page, 'cronjobs', true)
  await page.goto('/workloads/cronjobs')
  await expect(page.getByRole('status').filter({ hasText: '120 个 CronJobs' })).toBeVisible()
  await expect(
    page.getByRole('button', { name: '更多操作：resource-000', exact: true }),
  ).toHaveCount(0)
  expect(state.writes).toEqual([])
  verifyBrowserEvidence(evidence)
})

test('CronJob pause and resume keep explicit intent and scoped confirmation', async ({
  context,
  page,
}) => {
  const { state, evidence } = await setup(context, page, 'cronjobs')
  await page.goto('/workloads/cronjobs')
  for (const [label, suspend] of [
    ['暂停', true],
    ['恢复', false],
  ] as const) {
    await page.getByRole('button', { name: '更多操作：resource-000', exact: true }).click()
    await page.getByRole('menuitem', { name: label, exact: true }).click()
    const dialog = page.getByRole('dialog')
    await expect(dialog).toContainText('monitoring / resource-000')
    await dialog.getByRole('button', { name: /^确\s*认$/ }).click()
    await expect(dialog).toHaveCount(0)
    await expect.poll(() => state.rows[0].suspend).toBe(suspend)
  }
  expect(state.writes).toEqual([
    '/api/v1/clusters/cluster-a/workloads/cronjobs/resource-000/suspend?namespace=monitoring suspend=true',
    '/api/v1/clusters/cluster-a/workloads/cronjobs/resource-000/suspend?namespace=monitoring suspend=false',
  ])
  verifyBrowserEvidence(evidence)
})
test('empty and failed workload reads remain distinguishable', async ({ context, page }) => {
  const { state, evidence } = await setup(context, page, 'jobs')
  state.rows = []
  await page.goto('/workloads/jobs')
  await expect(page.getByRole('status').filter({ hasText: '0 个 Jobs' })).toBeVisible()
  state.readStatus = 503
  await page.reload()
  await expect(page.getByRole('status').filter({ hasText: 'Jobs 未加载' })).toBeVisible()
  await expect(
    page.getByText('Synthetic workload unavailable', { exact: false }).first(),
  ).toBeVisible()
  state.readStatus = 200
  await page.getByRole('button', { name: /^重试读取/ }).click()
  await expect(page.getByRole('status').filter({ hasText: '0 个 Jobs' })).toBeVisible()
  verifyBrowserEvidence(evidence)
})
