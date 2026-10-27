import { test, expect, type Page, type BrowserContext } from '@playwright/test'
import { baselineResponse } from '../fixtures/baseline-data'
import { installMocks, verifyBrowserEvidence } from '../fixtures/mock-app'

function makePods(count = 2000) {
  return Array.from({ length: count }, (_, index) => ({
    name: `pod-${String(index).padStart(4, '0')}`,
    namespace: 'monitoring',
    phase: index % 10 === 0 ? 'Pending' : 'Running',
    readyContainers: index % 10 === 0 ? '0/1' : '1/1',
    restarts: index,
    nodeName: `worker-${index % 8}`,
    podIp: `10.0.${Math.floor(index / 250)}.${index % 250}`,
    cpu: `${index}m`,
    memory: `${index + 1}Mi`,
    ageSeconds: index * 60,
    persistentVolumeClaims: index % 5 === 0 ? ['data'] : [],
    allowedActions: ['get', 'delete'],
  }))
}

async function setup(context: BrowserContext, page: Page, readonly = false) {
  const state = { pods: makePods(), status: 200, reads: 0 }
  await context.addInitScript(() =>
    localStorage.setItem(
      'soha-scope',
      JSON.stringify({
        state: { clusterId: 'cluster-a', namespace: 'monitoring' },
        version: 0,
      }),
    ),
  )
  const evidence = await installMocks(context, page, (url, method) => {
    if (url.pathname === '/api/v1/access/permission-snapshot') {
      const body = baselineResponse(url.pathname)!
      if (!('permissionKeys' in body.data)) throw new Error('Missing permission fixture')
      body.data.permissionKeys!.push(
        'platform.pods.view',
        ...(readonly ? [] : ['platform.pods.delete']),
      )
      body.data.visibleMenuIds!.push('workloads-pods')
      body.data.visibleMenus!.push({
        id: 'workloads-pods',
        parentId: 'workloads',
        path: '/workloads/pods',
        labelZh: 'Pods',
      })
      return { body }
    }
    if (url.pathname === '/api/v1/clusters')
      return {
        body: {
          data: [
            {
              id: 'cluster-a',
              name: 'Pods 连续浏览样板',
              connectionMode: 'direct_kubeconfig',
              environment: 'testing',
              health: { status: 'healthy' },
            },
          ],
        },
      }
    if (url.pathname === '/api/v1/clusters/capabilities')
      return {
        body: {
          data: [
            {
              key: 'workload.mutations',
              direct: { status: 'available' },
              agent: { status: 'partial' },
            },
          ],
        },
      }
    if (url.pathname === '/api/v1/clusters/cluster-a/workloads/pods' && method === 'GET') {
      state.reads += 1
      return state.status === 200
        ? { body: { data: state.pods } }
        : {
            status: state.status,
            body: { error: { code: 'test_read_failed', message: 'Synthetic read failure' } },
          }
    }
    if (url.pathname === '/api/v1/auth/stream-ticket' && method === 'POST')
      return {
        status: 503,
        body: { error: { code: 'unavailable', message: 'Synthetic polling mode' } },
      }
    return undefined
  })
  await page.goto('/workloads/pods')
  await expect(page.getByRole('status').filter({ hasText: '2000 个 Pods' })).toBeVisible()
  return { state, evidence }
}

const holder = (page: Page) => page.getByRole('region', { name: '资源列表，可连续滚动' })
const rows = (page: Page) => page.locator('.soha-pods-table .ant-table-row[data-row-key]')
const search = (page: Page) => page.getByPlaceholder('搜索 Pod / Node / IP')
async function anchor(page: Page) {
  return holder(page).evaluate((body) => {
    const top = body.getBoundingClientRect().top
    const row = Array.from(body.querySelectorAll<HTMLElement>('[data-row-key]')).find(
      (item) => item.getBoundingClientRect().bottom > top + 1,
    )!
    return {
      key: row.dataset.rowKey!,
      offset: row.getBoundingClientRect().top - top,
      top: body.scrollTop,
    }
  })
}

test('continuous virtual rows, full-result search/sort, selection and keyboard', async ({
  context,
  page,
}) => {
  const { evidence } = await setup(context, page)
  await expect(page.locator('.ant-pagination')).toHaveCount(0)
  expect(await rows(page).count()).toBeLessThan(80)
  await holder(page).hover()
  await page.mouse.wheel(0, 5000)
  await expect.poll(async () => (await anchor(page)).top).toBeGreaterThan(1000)
  expect(await rows(page).count()).toBeLessThan(80)

  await search(page).fill('pod-1999')
  await expect(rows(page)).toHaveCount(1)
  await expect(
    page.getByRole('status').filter({ hasText: '匹配 1 / 共 2000 个 Pods' }),
  ).toBeVisible()
  await expect.poll(() => holder(page).evaluate((node) => node.scrollTop)).toBe(0)
  await page.getByRole('checkbox', { name: '选择全部匹配的 1 个 Pod', exact: true }).check()
  await expect(page.getByRole('button', { name: '批量删除', exact: true })).toBeEnabled()
  await search(page).clear()
  await expect(page.getByRole('button', { name: '批量删除', exact: true })).toHaveCount(0)
  await page.getByRole('columnheader', { name: 'Pod', exact: true }).click()
  await expect(rows(page).first()).toHaveAttribute('data-row-key', 'monitoring/pod-1999')
  await holder(page).focus()
  await holder(page).press('PageDown')
  await expect.poll(() => holder(page).evaluate((node) => node.scrollTop)).toBeGreaterThan(0)
  await holder(page).press('Home')
  await expect.poll(() => holder(page).evaluate((node) => node.scrollTop)).toBe(0)
  verifyBrowserEvidence(evidence)
})

test('manual and background refresh preserve the visible resource, errors remain truthful', async ({
  context,
  page,
}) => {
  await page.clock.install()
  const { state, evidence } = await setup(context, page)
  await holder(page).hover()
  await page.mouse.wheel(0, 6000)
  await expect.poll(async () => (await anchor(page)).top).toBeGreaterThan(1000)
  const before = await anchor(page)
  state.pods = [{ ...state.pods[0], name: 'a-inserted-before-anchor' }, ...state.pods]
  await page.getByRole('button', { name: '刷新', exact: true }).click()
  await expect(page.getByRole('status').filter({ hasText: '2001 个 Pods' })).toBeVisible()
  await expect.poll(async () => (await anchor(page)).key).toBe(before.key)
  expect(Math.abs((await anchor(page)).offset - before.offset)).toBeLessThan(3)

  state.pods = state.pods.map((pod) => ({ ...pod, cpu: '400m' }))
  const reads = state.reads
  await page.clock.fastForward(15_000)
  await expect.poll(() => state.reads).toBeGreaterThan(reads)
  await expect.poll(async () => (await anchor(page)).key).toBe(before.key)
  expect(Math.abs((await anchor(page)).offset - before.offset)).toBeLessThan(3)

  state.status = 500
  await page.getByRole('button', { name: '刷新', exact: true }).click()
  await expect(
    page.getByRole('alert').filter({ hasText: 'Synthetic read failure' }).first(),
  ).toBeVisible()
  await expect.poll(async () => (await anchor(page)).key).toBe(before.key)
  state.status = 403
  await page.getByRole('button', { name: '刷新', exact: true }).click()
  await expect(rows(page)).toHaveCount(0)
  await expect(page.getByRole('status').filter({ hasText: 'Pods 未加载' })).toBeVisible()
  verifyBrowserEvidence(evidence)
})

for (const theme of ['light', 'dark']) {
  test(`layout, long values, empty filter and readonly (${theme})`, async ({
    context,
    page,
  }, info) => {
    const { state, evidence } = await setup(context, page, true)
    state.pods = state.pods.map((pod) => ({
      ...pod,
      name: `${pod.name}-very-long-kubernetes-workload-resource-name`,
      namespace: 'monitoring',
    }))
    await page.getByRole('button', { name: '刷新', exact: true }).click()
    await expect(rows(page).first()).toContainText('very-long-kubernetes')
    if (theme === 'dark') await page.getByLabel('切换到深色模式').click()
    await expect
      .poll(() =>
        page
          .locator('.soha-pods-table .ant-table-thead th')
          .first()
          .evaluate((cell) => {
            const target = document.createElement('div')
            target.style.backgroundColor = getComputedStyle(
              document.documentElement,
            ).getPropertyValue('--soha-bg-surface-muted')
            return getComputedStyle(cell).backgroundColor === target.style.backgroundColor
          }),
      )
      .toBe(true)
    await expect(page.getByRole('button', { name: '删除 Pod', exact: true })).toHaveCount(0)
    for (const width of [1440, 1024, 768, 320]) {
      await page.setViewportSize({ width, height: 900 })
      await expect(holder(page)).toBeVisible()
      expect(await rows(page).count()).toBeLessThan(80)
      const overflow = await page.evaluate(
        () => document.documentElement.scrollWidth > window.innerWidth,
      )
      expect(overflow, `no page overflow at ${width}px`).toBe(false)
      if (width === 1440) await page.screenshot({ path: info.outputPath(`pods-${theme}.png`) })
    }
    await page.setViewportSize({ width: 1280, height: 480 })
    await expect(holder(page)).toBeVisible()
    await search(page).fill('不存在的资源')
    await expect(
      page.getByRole('status').filter({ hasText: '匹配 0 / 共 2000 个 Pods' }),
    ).toBeVisible()
    await expect(page.getByText('没有匹配的 Pods', { exact: true })).toBeVisible()
    verifyBrowserEvidence(evidence)
  })
}
