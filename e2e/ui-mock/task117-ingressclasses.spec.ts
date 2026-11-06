import { test, expect, type BrowserContext, type Page } from '@playwright/test'
import { resolve } from 'node:path'
import { baselineResponse } from '../fixtures/baseline-data'
import { installMocks, verifyBrowserEvidence } from '../fixtures/mock-app'
const root = '/api/v1/clusters/cluster-a/network/ingressclasses'
const detailURL = '/network/ingressclasses/nginx?clusterId=cluster-a'
const panel = (page: Page) => page.getByRole('tabpanel').filter({ visible: true })
async function setup(context: BrowserContext, page: Page, readonly = false, noIngresses = false) {
  await context.addInitScript(() =>
    localStorage.setItem(
      'soha-scope',
      JSON.stringify({ state: { clusterId: 'cluster-a', namespace: 'monitoring' }, version: 0 }),
    ),
  )
  const detail = {
    name: 'nginx',
    controller: 'example.io/ingress-controller-with-a-very-long-identifier',
    isDefault: true,
    parameters: 'IngressParameters/edge-configuration',
    ageSeconds: 3600,
    allowedActions: readonly ? ['view'] : ['view', 'update', 'delete'],
    labels: { app: 'edge' },
    annotations: { 'ingressclass.kubernetes.io/is-default-class': 'true' },
    ingresses: Array.from({ length: 20 }, (_, i) => ({
      name: `web-${i}`,
      namespace: i % 2 ? 'default' : 'monitoring',
      hosts: ['a.example.test'],
      address: '10.0.0.2',
      ageSeconds: 300,
    })),
  }
  const state = { fail: 0, writes: [] as string[], reads: [] as string[] }
  const evidence = await installMocks(context, page, (url, method) => {
    if (url.pathname === '/api/v1/access/permission-snapshot') {
      const body = baselineResponse(url.pathname)!
      if (!('permissionKeys' in body.data)) throw new Error('Missing permissions')
      body.data.permissionKeys = [
        ...body.data.permissionKeys!.filter(
          (p) =>
            !p.startsWith('platform.network.ingress-classes.') &&
            (!noIngresses || p !== 'platform.network.ingresses.view'),
        ),
        'platform.network.ingress-classes.view',
        ...(!noIngresses ? ['platform.network.ingresses.view'] : []),
        ...(readonly
          ? []
          : ['platform.network.ingress-classes.update', 'platform.network.ingress-classes.delete']),
      ]
      return { body }
    }
    if (url.pathname.startsWith(root)) {
      if (method === 'GET') state.reads.push(url.pathname + url.search)
      else state.writes.push(method + ' ' + url.pathname)
      if (state.fail && method === 'GET')
        return {
          status: state.fail,
          body: { error: { code: 'failed', message: 'IngressClass read failed' } },
        }
      if (url.pathname.endsWith('/yaml'))
        return {
          body: {
            data: {
              content:
                'apiVersion: networking.k8s.io/v1\nkind: IngressClass\nmetadata:\n  name: nginx\n  resourceVersion: "7"\nspec:\n  controller: example.io/controller\n  parameters:\n    apiGroup: example.io\n    kind: IngressParameters\n    name: edge-configuration\n    scope: Namespace\n    namespace: team-a\n',
              resourceVersion: '7',
            },
          },
        }
      if (method === 'DELETE') return { body: { data: null } }
      if (url.pathname.endsWith('/detail')) return { body: { data: detail } }
      return {
        body: {
          data: Array.from({ length: 40 }, (_, i) => ({
            ...detail,
            name: i === 0 ? 'nginx' : `class-${String(i).padStart(2, '0')}`,
            isDefault: i === 0,
            parameters: i === 39 ? 'Parameters/search-me' : detail.parameters,
          })),
        },
      }
    }
    return undefined
  })
  return { state, evidence }
}
async function capture(page: Page, name: string) {
  if (process.env.TASK117_EVIDENCE_DIR)
    await page.screenshot({
      path: resolve(process.env.TASK117_EVIDENCE_DIR, name + '.png'),
      fullPage: true,
      animations: 'disabled',
    })
}
test('IngressClasses full-array list searches later rows, has no pagination and ignores namespace', async ({
  context,
  page,
}) => {
  const { state, evidence } = await setup(context, page)
  await page.goto('/network/ingressclasses?clusterId=cluster-a&namespace=monitoring')
  await expect(page.getByText('40 个 IngressClass', { exact: true })).toBeVisible()
  await expect(page.locator('.ant-pagination')).toHaveCount(0)
  const holder = page.locator('.ant-table-tbody-virtual-holder')
  await expect(holder).toBeVisible()
  expect(await page.locator('.ant-table-row').count()).toBeLessThan(40)
  await page.getByPlaceholder('搜索名称 / Controller / 参数').fill('search-me')
  await expect(page.getByText('class-39', { exact: true })).toBeVisible()
  expect(state.reads.every((p) => !p.includes('namespace='))).toBe(true)
  await capture(page, 'ingressclass-list-search')
  verifyBrowserEvidence(evidence)
})
test('IngressClass detail uses four aligned tabs, configuration, metadata and scoped references', async ({
  context,
  page,
}) => {
  const { state, evidence } = await setup(context, page)
  await page.goto(detailURL)
  for (const label of ['概览', 'Ingresses', '关联资源', 'YAML'])
    await expect(page.getByRole('tab', { name: label, exact: true })).toBeVisible()
  await expect(panel(page).getByText('类配置', { exact: true })).toBeVisible()
  await expect(
    panel(page).getByText('IngressParameters/edge-configuration', { exact: true }),
  ).toBeVisible()
  await expect(panel(page).getByText('team-a', { exact: true })).toBeVisible()
  await expect(page.locator('.soha-k8s-detail-header')).not.toContainText('健康')
  await expect(page.locator('.soha-k8s-detail-header')).not.toContainText('monitoring')
  await capture(page, 'ingressclass-overview')
  await page.getByRole('tab', { name: 'Ingresses', exact: true }).click()
  await expect(panel(page).getByRole('link', { name: 'web-1', exact: true })).toHaveAttribute(
    'href',
    /namespace=default/,
  )
  await expect(panel(page).locator('.ant-pagination')).toHaveCount(0)
  await page.getByRole('tab', { name: '关联资源', exact: true }).click()
  await expect(page.getByRole('group', { name: 'IngressClass 资源关系图' })).toBeVisible()
  await capture(page, 'ingressclass-relationships')
  await page.getByText('列表', { exact: true }).click()
  await expect(panel(page).getByRole('link', { name: 'web-1', exact: true })).toBeVisible()
  expect(state.writes).toEqual([])
  verifyBrowserEvidence(evidence)
})
test('view-only IngressClass hides writes and denied reference links', async ({
  context,
  page,
}) => {
  const { state, evidence } = await setup(context, page, true, true)
  await page.goto(detailURL)
  await expect(page.getByRole('heading', { name: 'nginx', exact: true })).toBeVisible()
  await expect(page.getByRole('button', { name: '删除 IngressClass', exact: true })).toHaveCount(0)
  await expect(page.getByRole('button', { name: '编辑 IngressClass', exact: true })).toHaveCount(0)
  await page.getByRole('tab', { name: 'Ingresses', exact: true }).click()
  await expect(panel(page).getByText('没有查看 Ingresses 的权限', { exact: true })).toBeVisible()
  await expect(panel(page).getByRole('link')).toHaveCount(0)
  expect(state.writes).toEqual([])
  verifyBrowserEvidence(evidence)
})
test('refresh retains data on 503 and removes protected content on 403', async ({
  context,
  page,
}) => {
  const { state, evidence } = await setup(context, page)
  await page.goto(detailURL)
  await expect(page.getByRole('heading', { name: 'nginx', exact: true })).toBeVisible()
  state.fail = 503
  await page.getByRole('button', { name: '刷新 IngressClass', exact: true }).click()
  await expect(page.getByText('IngressClass read failed', { exact: false }).first()).toBeVisible()
  await expect(page.getByRole('heading', { name: 'nginx', exact: true })).toBeVisible()
  await expect(page.getByRole('switch', { name: /刷新失败/ })).toBeVisible({ timeout: 15_000 })
  state.fail = 403
  await page.getByRole('button', { name: '刷新 IngressClass', exact: true }).click()
  await expect(page.getByRole('heading', { name: 'nginx', exact: true })).toHaveCount(0)
  verifyBrowserEvidence(evidence)
})
test('detail delete confirms the exact class and cancellation sends no write', async ({
  context,
  page,
}) => {
  const { state, evidence } = await setup(context, page)
  await page.goto(detailURL)
  await page.getByRole('button', { name: '删除 IngressClass', exact: true }).click()
  await expect(page.getByRole('dialog')).toContainText('nginx')
  await page.getByRole('button', { name: /取\s*消/ }).click()
  expect(state.writes).toEqual([])
  await page.getByRole('button', { name: '删除 IngressClass', exact: true }).click()
  await page.getByRole('dialog').getByRole('button', { name: '删除', exact: true }).click()
  await expect(page).toHaveURL(/network\/ingressclasses\?clusterId=cluster-a$/)
  expect(state.writes).toEqual(['DELETE ' + root + '/nginx'])
  verifyBrowserEvidence(evidence)
})
test('IngressClass relationship cards wrap long values in dark narrow view', async ({
  context,
  page,
}) => {
  const { evidence } = await setup(context, page)
  await page.setViewportSize({ width: 900, height: 1000 })
  await page.goto(detailURL + '&tab=relationships')
  await expect(page.getByRole('group', { name: 'IngressClass 资源关系图' })).toBeVisible()
  await page.getByRole('button', { name: '切换到深色模式' }).click()
  await expect(page.locator('html')).toHaveAttribute('data-theme-mode', 'dark')
  await capture(page, 'ingressclass-narrow')
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true)
  verifyBrowserEvidence(evidence)
})

test('IngressClass YAML draft survives tabs and refresh without writing', async ({
  context,
  page,
}) => {
  const { state, evidence } = await setup(context, page)
  await page.goto(detailURL + '&tab=yaml')
  const editor = page.getByRole('textbox', { name: 'YAML 草稿编辑器', exact: true })
  await expect(page.locator('.soha-k8s-yaml-editor .view-lines:visible').first()).toContainText(
    'IngressClass',
  )
  await editor.press('ControlOrMeta+Home')
  await editor.pressSequentially('# retained class draft')
  await editor.press('Enter')
  await page.getByRole('tab', { name: '概览', exact: true }).click()
  await page.getByRole('button', { name: '刷新 IngressClass', exact: true }).click()
  await page.getByRole('tab', { name: 'YAML', exact: true }).click()
  await expect(page.locator('.soha-k8s-yaml-editor .view-lines:visible').first()).toContainText(
    'retained class draft',
  )
  expect(state.writes).toEqual([])
  expect(state.reads.every((p) => !p.includes('namespace='))).toBe(true)
  verifyBrowserEvidence(evidence)
})
