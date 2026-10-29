import { test, expect, type Page, type BrowserContext } from '@playwright/test'
import { baselineResponse } from '../fixtures/baseline-data'
import { installMocks, verifyBrowserEvidence } from '../fixtures/mock-app'
import type { Pod } from '../../src/types/platform'

function containerStates(index: number): NonNullable<Pod['containerStatuses']> {
  const app = { name: 'app', kind: 'container', state: 'running', ready: true } as const
  const init = {
    name: 'migrate',
    kind: 'init',
    state: 'terminated',
    ready: false,
    reason: 'Completed',
    exitCode: 0,
  } as const
  if (index === 1)
    return [
      { ...init, state: 'waiting', reason: 'PodInitializing', exitCode: undefined },
      { ...app, state: 'unknown', ready: false },
    ]
  if (index === 2)
    return [
      { ...init, reason: 'Error', exitCode: 1 },
      { ...app, state: 'unknown', ready: false },
    ]
  if (index === 3)
    return [
      { name: 'proxy', kind: 'sidecar', state: 'running', ready: false },
      { ...app, state: 'unknown', ready: false },
    ]
  if (index === 4) return [{ ...app, state: 'waiting', ready: false, reason: 'CrashLoopBackOff' }]
  if (index === 5)
    return [{ ...app, state: 'terminated', ready: false, reason: 'Completed', exitCode: 0 }]
  if (index === 6)
    return [{ ...app, state: 'terminated', ready: false, reason: 'Error', exitCode: 1 }]
  if (index === 7)
    return [...Array.from({ length: 7 }, (_, n) => ({ ...init, name: `init-${n}` })), app]
  return [init, app]
}

function makePods(count = 2000): Pod[] {
  return Array.from({ length: count }, (_, index) => ({
    name: `pod-${String(index).padStart(4, '0')}`,
    namespace: 'monitoring',
    phase:
      index > 0 && index < 4
        ? 'Pending'
        : index === 5
          ? 'Succeeded'
          : index === 6
            ? 'Failed'
            : 'Running',
    readyContainers: index > 0 && index < 7 ? '0/1' : '1/1',
    qosClass: ['Guaranteed', 'Burstable', 'BestEffort'][index % 3],
    containerStatuses: containerStates(index),
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

async function setup(context: BrowserContext, page: Page, readonly = false, withSessions = false) {
  const state = {
    pods: makePods(),
    status: 200,
    reads: 0,
    deletes: 0,
    terminalTickets: 0,
    terminalRows: [] as number[],
    nodeReads: 0,
  }
  if (withSessions)
    await page.routeWebSocket('**/workloads/pods/pod-0000/terminal?*', (socket) => {
      socket.onMessage((message) => {
        const value = JSON.parse(String(message))
        if (value.type === 'resize') state.terminalRows.push(value.rows)
      })
      socket.send(JSON.stringify({ type: 'stdout', data: 'Synthetic terminal ready\r\n' }))
    })
  await context.addInitScript(() =>
    localStorage.setItem(
      'soha-scope',
      JSON.stringify({
        state: { clusterId: 'cluster-a', namespace: 'monitoring' },
        version: 0,
      }),
    ),
  )
  const evidence = await installMocks(context, page, (url, method, request) => {
    if (url.pathname === '/api/v1/access/permission-snapshot') {
      const body = baselineResponse(url.pathname)!
      if (!('permissionKeys' in body.data)) throw new Error('Missing permission fixture')
      body.data.permissionKeys!.push(
        'platform.pods.view',
        'platform.nodes.view',
        ...(readonly ? [] : ['platform.pods.delete']),
        ...(withSessions ? ['platform.pods.logs', 'platform.pods.exec'] : []),
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
            ...(withSessions
              ? ['pod.logs', 'pod.exec'].map((key) => ({
                  key,
                  direct: { status: 'available' },
                  agent: { status: 'partial' },
                }))
              : []),
          ],
        },
      }
    if (
      url.pathname ===
        '/api/v1/clusters/cluster-a/infrastructure/nodes/worker-with-a-long-dns-name.example.internal/detail' &&
      method === 'GET'
    ) {
      state.nodeReads += 1
      return {
        body: {
          data: {
            name: 'worker-with-a-long-dns-name.example.internal',
            status: 'Ready',
            labels: {},
            annotations: {},
            taints: [],
            roles: [],
            pods: [],
            conditions: [],
            ageSeconds: 3600,
            podCount: 0,
          },
        },
      }
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
    if (
      withSessions &&
      method === 'GET' &&
      url.pathname === '/api/v1/clusters/cluster-a/workloads/pods/pod-0000/detail'
    ) {
      return {
        body: {
          data: {
            ...state.pods[0],
            allowedActions: ['get', 'logs', 'exec', 'delete'],
            containers: [{ name: 'app', role: 'main', image: 'synthetic/app', ready: true }],
          },
        },
      }
    }
    if (
      withSessions &&
      method === 'GET' &&
      url.pathname === '/api/v1/clusters/cluster-a/workloads/pods/pod-0000/logs'
    ) {
      return {
        body: {
          data: {
            podName: 'pod-0000',
            namespace: 'monitoring',
            container: 'app',
            content: Array.from({ length: 600 }, (_, index) => `Synthetic log line ${index}`).join(
              '\n',
            ),
            contentBytes: 14000,
            truncated: false,
          },
        },
      }
    }
    if (
      withSessions &&
      url.pathname === '/api/v1/clusters/cluster-a/workloads/pods/pod-0000' &&
      method === 'DELETE' &&
      url.searchParams.get('namespace') === 'monitoring'
    ) {
      state.deletes += 1
      return {
        status: 500,
        body: { error: { code: 'test_delete_failed', message: 'Synthetic delete rejection' } },
      }
    }
    if (url.pathname === '/api/v1/auth/stream-ticket' && method === 'POST') {
      if (withSessions && request.postDataJSON()?.path?.endsWith('/terminal')) {
        state.terminalTickets += 1
        return {
          body: {
            data: { ticket: 'synthetic-terminal-ticket', expiresAt: '2099-01-01T00:00:00Z' },
          },
        }
      }
      return {
        status: 503,
        body: { error: { code: 'unavailable', message: 'Synthetic polling mode' } },
      }
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

async function expectContainerColumnAligned(page: Page) {
  await expect
    .poll(() =>
      page.evaluate(() => {
        const table = document.querySelector('.soha-pods-table')!
        const header = Array.from(table.querySelectorAll<HTMLElement>('.ant-table-thead th')).find(
          (cell) => cell.textContent?.trim() === '容器',
        )!
        const button = table.querySelector<HTMLElement>('.soha-pod-container-cell')!
        const cell = button.closest('.ant-table-cell')!
        const viewport = table.getBoundingClientRect()
        const column = header.getBoundingClientRect()
        const body = cell.getBoundingClientRect()
        const aligned =
          column.left >= viewport.left &&
          column.right <= viewport.right &&
          Math.abs(column.left - body.left) < 2
        if (aligned) return 'aligned'
        const ancestors = []
        for (
          let node: HTMLElement | null = button;
          node && node !== table;
          node = node.parentElement
        ) {
          ancestors.push({
            class: node.className,
            x: node.scrollLeft,
            width: node.clientWidth,
            scrollWidth: node.scrollWidth,
          })
        }
        return JSON.stringify({
          column: [column.left, column.right],
          cell: [body.left, body.right],
          viewport: [viewport.left, viewport.right],
          headerX: table.querySelector('.ant-table-header')?.scrollLeft,
          active: document.activeElement?.getAttribute('aria-label'),
          ancestors,
        })
      }),
    )
    .toBe('aligned')
}

test('init and app states, overflow details, keyboard, QoS sorting and older APIs', async ({
  context,
  page,
}, info) => {
  const { state, evidence } = await setup(context, page)
  const cell = () => rows(page).first().locator('.soha-pod-container-cell')
  await search(page).fill('pod-0000')
  await expect(cell().locator('.is-success')).toHaveCount(2)
  await expect(cell()).toContainText('1/1')
  await expect(rows(page).first()).toContainText('Guaranteed')
  await cell().focus()
  await expectContainerColumnAligned(page)
  await cell().press('Enter')
  const detail = page.getByRole('region', { name: '容器状态', exact: true })
  await expect(detail).toBeFocused()
  await expect(detail).toContainText('常规容器就绪 1/1')
  await expect(detail).toContainText('初始化容器 migrate：正常完成')
  await expect
    .poll(() =>
      detail.evaluate((node) => {
        const popup = node.closest('.ant-popover')!
        return Number(getComputedStyle(popup).opacity)
      }),
    )
    .toBe(1)
  await page.screenshot({ path: info.outputPath('pods-init-details.png') })
  await detail.press('Escape')
  await expect(cell()).toBeFocused()
  await expect(cell()).toHaveAttribute('aria-expanded', 'false')

  for (const [index, tone] of [
    [1, 'warning'],
    [2, 'danger'],
    [3, 'warning'],
    [4, 'danger'],
    [5, 'success'],
    [6, 'danger'],
  ] as const) {
    await search(page).fill(`pod-000${index}`)
    await expect(cell().locator('.soha-pod-container-square').first()).toHaveClass(
      new RegExp(`is-${tone}`),
    )
  }
  await search(page).fill('pod-0003')
  await cell().click()
  await expect(detail).toContainText('常驻初始化容器 proxy：运行中，未就绪')
  await page.keyboard.press('Escape')
  await expect(cell()).toHaveAttribute('aria-expanded', 'false')
  await search(page).fill('pod-0007')
  await expect(cell().locator('.soha-pod-container-square')).toHaveCount(6)
  await expect(cell()).toContainText('+2')
  await cell().press('Enter')
  await expect(detail.locator('.soha-pod-container-detail')).toHaveCount(8)
  await expect(detail).toContainText('初始化容器 init-6：正常完成')
  await expect(detail).toContainText('容器 app：运行中，已就绪')
  await detail.press('Escape')
  await expect(detail).toBeHidden()
  await expect(cell()).toBeFocused()

  await search(page).clear()
  await expect(search(page)).toHaveValue('')
  await expect(page.getByRole('status').filter({ hasText: '2000 个 Pods' })).toBeVisible()
  await page.getByRole('columnheader', { name: 'QoS', exact: true }).click()
  await expect(rows(page).first()).toContainText('BestEffort')
  state.pods = [{ ...state.pods[0], containerStatuses: undefined, qosClass: undefined }]
  await page.getByRole('button', { name: '刷新', exact: true }).click()
  await expect(rows(page)).toHaveCount(1)
  await expect(cell().locator('.is-unknown')).toHaveCount(1)
  await expect(cell()).toHaveAccessibleName(/逐容器状态未知/)
  await expect(rows(page).first().getByText('—', { exact: true })).toHaveCount(1)
  verifyBrowserEvidence(evidence)
})
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
  const beforeFocus = await anchor(page)
  const focusKey = await holder(page).evaluate((body) => {
    const viewport = body.getBoundingClientRect()
    return Array.from(body.querySelectorAll<HTMLElement>('[data-row-key]')).find((row) => {
      const rect = row.getBoundingClientRect()
      return rect.top >= viewport.top + 1 && rect.bottom <= viewport.bottom
    })!.dataset.rowKey!
  })
  await page
    .locator(`.soha-pods-table [data-row-key="${focusKey}"] .soha-pod-container-cell`)
    .focus()
  await expectContainerColumnAligned(page)
  await expect.poll(() => holder(page).evaluate((body) => body.scrollTop)).toBe(beforeFocus.top)

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
    await expect(page.getByRole('button', { name: /^更多操作：/ })).toHaveCount(0)
    await expect(page.getByRole('menuitem', { name: '删除 Pod', exact: true })).toHaveCount(0)
    for (const width of [1440, 1024, 768, 320]) {
      await page.setViewportSize({ width, height: 900 })
      await expect(holder(page)).toBeVisible()
      expect(await rows(page).count()).toBeLessThan(80)
      const overflow = await page.evaluate(
        () => document.documentElement.scrollWidth > window.innerWidth,
      )
      expect(overflow, `no page overflow at ${width}px`).toBe(false)
      if (width === 1440) {
        await rows(page).first().locator('.soha-pod-container-cell').focus()
        await expectContainerColumnAligned(page)
        await page.screenshot({ path: info.outputPath(`pods-${theme}.png`) })
      }
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

for (const theme of ['light', 'dark']) {
  test(`namespace ellipsis and scheduled node links remain readable (${theme})`, async ({
    context,
    page,
  }) => {
    const { state, evidence } = await setup(context, page, true)
    state.pods = makePods(2).map((pod) => ({
      ...pod,
      namespace: 'envoy-gateway-system',
      nodeName: 'worker-with-a-long-dns-name.example.internal',
    }))
    await page.getByRole('button', { name: '刷新', exact: true }).click()
    await expect(rows(page)).toHaveCount(2)
    if (theme === 'dark') await page.getByLabel('切换到深色模式').click()
    const tag = rows(page).first().locator('.soha-pod-table-namespace-tag')
    await expect(tag).toHaveText('envoy-gateway-system')
    await expect
      .poll(() =>
        tag.locator('.soha-pod-table-namespace-label').evaluate((element) => ({
          display: getComputedStyle(element).display,
          overflow: getComputedStyle(element).overflow,
          ellipsis: getComputedStyle(element).textOverflow,
          clipped: element.scrollWidth > element.clientWidth,
          fits:
            element.getBoundingClientRect().width <=
            element.parentElement!.getBoundingClientRect().width,
        })),
      )
      .toEqual({
        display: 'block',
        overflow: 'hidden',
        ellipsis: 'ellipsis',
        clipped: true,
        fits: true,
      })
    await tag.hover()
    await expect(
      page.getByRole('tooltip', { name: 'envoy-gateway-system', exact: true }),
    ).toBeVisible()
    const node = rows(page)
      .first()
      .getByRole('link', { name: 'worker-with-a-long-dns-name.example.internal', exact: true })
    await expect(node).toHaveAttribute(
      'href',
      '/cluster-resources/nodes/worker-with-a-long-dns-name.example.internal?clusterId=cluster-a',
    )
    await node.focus()
    await expect(node).toBeFocused()
    await node.press('Tab')
    await expect(
      rows(page)
        .first()
        .getByRole('button', { name: /^容器状态：/ }),
    ).toBeFocused()
    await page.keyboard.press('Shift+Tab')
    await expect(node).toBeFocused()
    await expect(node.locator('.soha-pod-table-node-text')).toHaveText(
      'worker-with-a-long-dns-name.example.internal',
    )
    expect(await page.evaluate(() => document.documentElement.scrollWidth > innerWidth)).toBe(false)
    if (theme === 'light') await node.press('Enter')
    else await node.click()
    await expect(page).toHaveURL(
      /\/cluster-resources\/nodes\/worker-with-a-long-dns-name\.example\.internal\?clusterId=cluster-a$/,
    )
    await expect(page.getByText('资源分配', { exact: true })).toBeVisible()
    expect(state.nodeReads).toBe(1)
    verifyBrowserEvidence(evidence)
  })
}

test('click-only Pods action menu supports keyboard, cancellation and failed delete feedback', async ({
  context,
  page,
}) => {
  const { state, evidence } = await setup(context, page, false, true)
  state.pods = state.pods
    .slice(0, 2)
    .map((pod) => ({ ...pod, allowedActions: ['get', 'logs', 'exec', 'delete'] }))
  await page.getByRole('button', { name: '刷新', exact: true }).click()
  await expect(rows(page)).toHaveCount(2)
  const more = page.getByRole('button', { name: '更多操作：pod-0000', exact: true })
  await expect(more).toHaveAttribute('aria-haspopup', 'menu')
  await more.hover()
  const menu = page.getByRole('menu', { name: 'Pod 操作：pod-0000', exact: true })
  await expect(menu).toHaveCount(0)
  await more.press('Enter')
  await expect(menu).toBeVisible()
  await expect(menu.getByRole('menuitem', { name: '打开日志会话', exact: true })).toBeVisible()
  await expect(menu.getByRole('menuitem', { name: '打开终端会话', exact: true })).toBeVisible()
  await expect(more).toHaveAttribute('aria-expanded', 'true')
  await menu.press('Escape')
  await expect(menu).toBeHidden()
  await expect(more).toBeFocused()
  await expect(more).toHaveAttribute('aria-expanded', 'false')
  await more.press('ArrowDown')
  await expect(menu).toBeVisible()
  await expect(menu.getByRole('menuitem', { name: '打开日志会话', exact: true })).toBeFocused()
  await menu.press('Shift+Tab')
  await expect(menu).toBeHidden()
  await expect(
    rows(page)
      .first()
      .getByRole('button', { name: /^pod-0000 · 内存 ·/ }),
  ).toBeFocused()
  await more.press('ArrowDown')
  await expect(menu).toBeVisible()
  await menu.getByRole('menuitem', { name: '删除 Pod', exact: true }).click()
  const dialog = page.getByRole('dialog', { name: '确认删除 Pod pod-0000？', exact: true })
  await expect(dialog).toBeVisible()
  await dialog.getByRole('button', { name: /^取\s*消$/ }).click()
  await expect(dialog).toBeHidden()
  expect(state.deletes).toBe(0)
  await expect(more).toBeFocused()
  await more.click()
  await menu.getByRole('menuitem', { name: '删除 Pod', exact: true }).click()
  await dialog.getByRole('button', { name: /^删\s*除$/ }).click()
  await expect(page.getByText('Synthetic delete rejection', { exact: true }).first()).toBeVisible()
  await expect(dialog).toBeVisible()
  await expect(rows(page)).toHaveCount(2)
  expect(state.deletes).toBe(1)
  await dialog.getByRole('button', { name: /^取\s*消$/ }).click()
  verifyBrowserEvidence(evidence)
})

for (const theme of ['light', 'dark']) {
  test(`fixed 80px rails and adaptive value columns (${theme})`, async ({ context, page }) => {
    const { state, evidence } = await setup(context, page)
    const values = ['2m', '223m', '1000m', '12345m', '123456.78m', '123456789m']
    state.pods = state.pods.slice(0, values.length).map((pod, index) => ({
      ...pod,
      cpu: values[index],
      memory: index === values.length - 1 ? '123456789Gi' : '146Mi',
      requests: { cpu: '100m', memory: '200Mi' },
      limits: { memory: '1Gi' },
    }))
    await page.getByRole('button', { name: '刷新', exact: true }).click()
    await expect(rows(page)).toHaveCount(values.length)
    await page.getByRole('switch').click()
    if (theme === 'dark') await page.getByLabel('切换到深色模式').click()
    await expect(page.getByRole('group', { name: '指标展示方案' })).toHaveCount(0)
    const cells = page.locator('.soha-pod-resource-limit-cell')
    await expect(cells).toHaveCount(values.length * 2)
    for (const [index, value] of values.entries()) {
      await expect(
        page
          .getByRole('button', { name: new RegExp(`^pod-000${index} · CPU ·`) })
          .locator('.soha-pod-resource-value'),
      ).toHaveText(value)
    }
    for (const width of [1440, 768, 320]) {
      await page.setViewportSize({ width, height: 900 })
      expect(
        await page.evaluate(() => document.documentElement.scrollWidth > window.innerWidth),
      ).toBe(false)
      const geometry = await cells.evaluateAll((nodes) =>
        nodes.map((node) => {
          const button = node.getBoundingClientRect()
          const cell = node.closest('.ant-table-cell')!.getBoundingClientRect()
          const rail = node.querySelector('.ant-progress-rail')!.getBoundingClientRect()
          const range = document.createRange()
          range.selectNodeContents(node.querySelector('.soha-pod-resource-value')!)
          const value = range.getBoundingClientRect()
          return {
            width: rail.width,
            fits:
              rail.width === 80 &&
              rail.height === 4 &&
              value.left >= rail.right + 7.9 &&
              value.right <= button.right + 0.1 &&
              value.top >= button.top &&
              value.bottom <= button.bottom &&
              button.left >= cell.left &&
              button.right <= cell.right,
          }
        }),
      )
      expect(
        geometry.every((item) => item.fits),
        `80px rails and full values fit at ${width}`,
      ).toBe(true)
      expect(new Set(geometry.map((item) => item.width))).toEqual(new Set([80]))
    }
    await page.setViewportSize({ width: 1440, height: 900 })
    const columnWidth = async () =>
      page
        .getByRole('columnheader', { name: 'CPU', exact: true })
        .evaluate((node) => node.getBoundingClientRect().width)
    const widthBeforeFilter = await columnWidth()
    await search(page).fill('pod-0000')
    await expect(rows(page)).toHaveCount(1)
    expect(await columnWidth()).toBe(widthBeforeFilter)
    await rows(page).first().getByRole('checkbox').check()
    const cpu = page.getByRole('button', { name: /^pod-0000 · CPU ·/ })
    await cpu.focus()
    const tooltip = page.getByRole('tooltip')
    await expect(tooltip).toBeVisible()
    await expect(tooltip.locator('dd')).toHaveText(['2m', '100m', '未设置'])
    await expect
      .poll(async () => {
        const box = await tooltip.boundingBox()
        return Boolean(box && box.x >= 0 && box.x + box.width <= 1440)
      })
      .toBe(true)
    await expect(rows(page).first().getByRole('checkbox')).toBeChecked()
    verifyBrowserEvidence(evidence)
  })
}

test('maximized session dock fills logs and terminal and keeps resize continuity', async ({
  context,
  page,
}) => {
  const { state, evidence } = await setup(context, page, false, true)
  state.pods = state.pods
    .slice(0, 2)
    .map((pod) => ({ ...pod, allowedActions: ['get', 'logs', 'exec', 'delete'] }))
  await page.getByRole('button', { name: '刷新', exact: true }).click()
  await expect(rows(page)).toHaveCount(2)
  const more = page.getByRole('button', { name: '更多操作：pod-0000', exact: true })
  const menu = page.getByRole('menu', { name: 'Pod 操作：pod-0000', exact: true })
  await more.click()
  await menu.getByRole('menuitem', { name: '打开日志会话', exact: true }).click()
  const dock = page.getByRole('region', { name: '实时会话', exact: true })
  await expect(dock).toBeVisible()
  await expect(dock.locator('.soha-log-shell')).toContainText('Synthetic log line')
  await dock.getByRole('button', { name: '最大化实时会话', exact: true }).click()
  const fits = async (selector: string) => {
    await expect
      .poll(() =>
        dock.evaluate((node, selector) => {
          const body = node
            .querySelector('.soha-realtime-session-dock__body')!
            .getBoundingClientRect()
          const panel = Array.from(node.querySelectorAll('[role="tabpanel"]')).find(
            (item) => item.getAttribute('aria-hidden') === 'false',
          )!
          const pane = panel.getBoundingClientRect()
          const shell = panel.querySelector(selector)!.getBoundingClientRect()
          const toolbar = panel.querySelector('.soha-terminal-toolbar')!.getBoundingClientRect()
          const rect = node.getBoundingClientRect()
          return {
            bodyHeight: body.height,
            paneHeight: pane.height,
            shellHeight: shell.height,
            bottomGap: body.bottom - shell.bottom,
            fits:
              Math.abs(rect.height - window.innerHeight) < 2 &&
              pane.height > body.height - 50 &&
              shell.height > body.height - toolbar.height - 120 &&
              shell.bottom <= body.bottom + 1 &&
              body.bottom - shell.bottom < 30,
          }
        }, selector),
      )
      .toMatchObject({ fits: true })
  }
  for (const viewport of [
    { width: 1367, height: 1511 },
    { width: 1440, height: 900 },
    { width: 390, height: 700 },
  ]) {
    await page.setViewportSize(viewport)
    await fits('.soha-log-shell')
  }
  await page.setViewportSize({ width: 1440, height: 900 })
  await dock.getByRole('button', { name: '收起实时会话', exact: true }).click()
  await more.click()
  await menu.getByRole('menuitem', { name: '打开终端会话', exact: true }).click()
  await expect(dock.locator('.soha-terminal-shell .xterm')).toBeVisible()
  await dock.getByRole('button', { name: '最大化实时会话', exact: true }).click()
  for (const viewport of [
    { width: 1440, height: 900 },
    { width: 390, height: 700 },
    { width: 1367, height: 1511 },
  ]) {
    await page.setViewportSize(viewport)
    await fits('.soha-terminal-shell')
    await expect.poll(() => state.terminalRows.length).toBeGreaterThan(0)
    const screen = dock.locator('.soha-terminal-shell .xterm-screen')
    await expect
      .poll(async () => {
        const box = await screen.boundingBox()
        const shell = await dock.locator('.soha-terminal-shell-inner').boundingBox()
        return Boolean(
          box && shell && box.height > shell.height - 36 && box.height <= shell.height + 1,
        )
      })
      .toBe(true)
  }
  await expect.poll(() => new Set(state.terminalRows).size).toBeGreaterThan(1)
  const maximizedRows = Math.max(...state.terminalRows)
  expect(state.terminalTickets).toBe(1)
  await dock.getByRole('button', { name: '还原实时会话', exact: true }).click()
  await expect(dock).not.toHaveClass(/is-maximized/)
  await expect
    .poll(async () => {
      const box = await dock.boundingBox()
      return box?.height ?? 0
    })
    .toBeLessThan(1511)
  await expect.poll(() => state.terminalRows.at(-1) ?? maximizedRows).toBeLessThan(maximizedRows)
  expect(state.terminalTickets).toBe(1)
  verifyBrowserEvidence(evidence)
})
