import { test, expect, definitions } from '../fixtures/crd-app'
import { verifyGeometry } from '../fixtures/geometry'
import type { Page } from '@playwright/test'

const definition = definitions[0]
const groupPath = `/extensions/apis/${definition.group}`
const listPath = `${groupPath}?clusterId=cluster-a&kind=${definition.name}&namespace=&tab=instances`

async function selectOtherCluster(page: Page) {
  await page.locator('.soha-platform-scope-select.is-cluster').click()
  await page
    .locator('.ant-select-item-option-content:visible')
    .filter({ hasText: /^stage-cluster$/ })
    .click()
}

async function expectSwitchedToCollection(page: Page) {
  await expect(page).toHaveURL((url) => {
    const params = url.searchParams
    return (
      params.get('clusterId') === 'cluster-b' &&
      params.get('kind') === definition.name &&
      ['resource', 'resourceNamespace', 'namespace', 'tab'].every((key) => !params.has(key))
    )
  })
  await expect(page.locator('.soha-crd-resource-detail')).toHaveCount(0)
}

async function openDirtyEditor(page: Page) {
  await page.goto(listPath)
  await page.getByRole('button', { name: 'sample-widget', exact: true }).click()
  await page.getByRole('button', { name: '编辑 YAML', exact: true }).click()
  const editor = page.getByRole('textbox', { name: 'YAML 草稿编辑器', exact: true })
  await expect(editor).toBeVisible()
  await editor.press('ControlOrMeta+Home')
  await editor.pressSequentially('# audit-unsaved-browser-draft')
  await editor.press('Enter')
  await expect(page.locator('.soha-k8s-yaml-editor .view-lines:visible').first()).toContainText(
    'audit-unsaved-browser-draft',
  )
}

test('CRD instance return retains the all-namespace list and its search independently of object namespace', async ({
  page,
  catalog,
  evidence,
}) => {
  catalog.resource = { allowedActions: ['view'] }
  await page.goto(listPath)
  const search = page.getByPlaceholder('搜索实例 / 命名空间 / 摘要', { exact: true })
  await search.fill('sample-widget')
  await page.getByRole('button', { name: 'sample-widget', exact: true }).click()
  await expect(page.locator('.soha-crd-resource-detail')).toContainText('资源摘要')
  let params = new URL(page.url()).searchParams
  expect(params.get('namespace')).toBe('')
  expect(params.get('resourceNamespace')).toBe('monitoring')
  expect(params.get('resource')).toBe('sample-widget')
  await page.getByRole('button', { name: /返回实例列表$/ }).click()
  await expect(search).toHaveValue('sample-widget')
  await expect(page.getByRole('button', { name: 'sample-widget', exact: true })).toBeVisible()
  params = new URL(page.url()).searchParams
  expect(params.get('namespace')).toBe('')
  expect(params.has('resource')).toBe(false)
  expect(params.has('resourceNamespace')).toBe(false)
  const collectionReads = evidence.requests.filter((path) =>
    path.includes(`/${definition.name}/resources?`),
  )
  expect(collectionReads.length).toBeGreaterThan(0)
  expect(
    collectionReads.every((path) => !new URL(path, 'http://fixture').searchParams.has('namespace')),
  ).toBe(true)
})

test('CRD instance with no view/update authorization never reads YAML, including a direct YAML link', async ({
  page,
  catalog,
  evidence,
}) => {
  catalog.resource = { allowedActions: [] }
  await page.goto(
    `${groupPath}?clusterId=cluster-a&kind=${definition.name}&namespace=&resource=sample-widget&resourceNamespace=monitoring&tab=yaml`,
  )
  await expect(page.getByText('当前授权不允许查看此实例内容', { exact: true })).toBeVisible()
  await expect(page.getByRole('tab', { name: 'YAML', exact: true })).toHaveCount(0)
  expect(
    evidence.requests.filter(
      (path) => path.includes('/extensions/crds/') && path.includes('/yaml'),
    ),
  ).toEqual([])
  expect(catalog.deleteCalls).toEqual([])
})

test('the header cluster switch clears a clean CRD instance URL without opening its same-name peer', async ({
  page,
  catalog,
  evidence,
}) => {
  catalog.includeSecondCluster = true
  catalog.resource = { allowedActions: ['view'] }
  await page.goto(
    `${groupPath}?clusterId=cluster-a&kind=${definition.name}&namespace=&resource=sample-widget&resourceNamespace=monitoring&tab=yaml`,
  )
  await expect(page.getByLabel('资源 YAML，只读', { exact: true })).toBeVisible()
  await selectOtherCluster(page)
  await expectSwitchedToCollection(page)
  expect(evidence.requests.filter((path) => path.includes('cluster-b') && path.includes('/resources/sample-widget/'))).toEqual([])
})

test('the header cluster switch honors the CRD draft confirmation and cancellation', async ({
  page,
  catalog,
  evidence,
}) => {
  catalog.includeSecondCluster = true
  catalog.resource = { allowedActions: ['view', 'update'] }
  await openDirtyEditor(page)
  const original = page.url()
  await selectOtherCluster(page)
  const confirm = page.getByRole('dialog', { name: '离开当前资源？', exact: true })
  await expect(confirm).toBeVisible()
  await expect(page).toHaveURL(original)
  expect(evidence.requests.filter((path) => path.includes('cluster-b/extensions'))).toEqual([])
  await confirm.getByRole('button', { name: /^取\s*消$/ }).click()
  await expect(page).toHaveURL(original)
  await expect(page.locator('.soha-k8s-yaml-editor .view-lines:visible').first()).toContainText(
    'audit-unsaved-browser-draft',
  )
  await selectOtherCluster(page)
  await confirm.getByRole('button', { name: '放弃更改并切换', exact: true }).click()
  await expectSwitchedToCollection(page)
})

test('an explicitly missing CRD kind cannot fall back to another kind with the same resource name', async ({
  page,
  catalog,
  evidence,
}) => {
  catalog.resource = { allowedActions: ['view'] }
  await page.goto(
    `${groupPath}?clusterId=cluster-a&kind=missing.${definition.group}&namespace=&resource=sample-widget&resourceNamespace=monitoring&tab=overview`,
  )
  await expect(page.locator('.soha-crd-group-detail')).toContainText(
    /不存在|不可用|未找到|not found|not available/i,
  )
  await expect(page.locator('.soha-crd-resource-detail h1')).toHaveCount(0)
  expect(
    evidence.requests.filter((path) => path.includes(`/${definition.name}/resources`)),
  ).toEqual([])
})

for (const variant of [
  { theme: 'light', width: 1440 },
  { theme: 'dark', width: 1020 },
  { theme: 'dark', width: 390 },
] as const) {
  test(`CRD overview and readonly YAML retain long identity and bounded geometry (${variant.theme}, ${variant.width}px)`, async ({
    page,
    context,
    catalog,
    evidence,
  }, info) => {
    const name = `sample-widget-${'long-resource-identity-'.repeat(5)}`
    catalog.resource = {
      name,
      uid: 'synthetic-long-widget-uid',
      allowedActions: ['view'],
      labels: { 'quality.example/long-label': 'long-label-value-'.repeat(10) },
      finalizers: ['cleanup.quality.example/finalizer'],
      summary: { controller: 'controller.quality.example/'.repeat(8), replicas: 0 },
    }
    await context.addInitScript(
      (themeMode) =>
        localStorage.setItem(
          'soha-prefs',
          JSON.stringify({ state: { themeMode, localeCode: 'zh_CN' }, version: 0 }),
        ),
      variant.theme,
    )
    await page.setViewportSize({ width: variant.width, height: 1000 })
    await page.emulateMedia({ reducedMotion: 'reduce' })
    await page.goto(listPath)
    await page.getByRole('button', { name, exact: true }).click()
    const detail = page.locator('.soha-crd-resource-detail')
    await expect(page.locator('html')).toHaveAttribute('data-theme-mode', variant.theme)
    await expect(detail.getByRole('heading', { name, exact: true })).toBeVisible()
    await expect(detail.getByRole('tab', { name: '概览', exact: true })).toHaveAttribute(
      'aria-selected',
      'true',
    )
    await expect(detail.getByText('资源摘要', { exact: true })).toBeVisible()
    await expect(detail.getByText('synthetic-long-widget-uid', { exact: true })).toBeVisible()
    expect(evidence.requests.filter((path) => path.includes('/yaml'))).toEqual([])
    const back = detail.getByRole('button', { name: /返回实例列表$/ })
    await back.scrollIntoViewIfNeeded()
    await verifyGeometry(page, back)
    await page.screenshot({ path: info.outputPath('crd-overview.png'), fullPage: true })
    await detail.getByRole('tab', { name: 'YAML', exact: true }).click()
    await expect(page.getByLabel('资源 YAML，只读', { exact: true })).toBeVisible()
    await expect(page.locator('.soha-k8s-yaml-editor .view-lines:visible').first()).toContainText(
      'apiVersion',
    )
    await expect(detail.getByRole('button', { name: '编辑 YAML', exact: true })).toHaveCount(0)
    await back.scrollIntoViewIfNeeded()
    await verifyGeometry(page, back)
    const yamlReads = evidence.requests.filter((path) => path.includes('/yaml'))
    expect(yamlReads.length).toBeGreaterThan(0)
    expect(
      yamlReads.every(
        (path) => new URL(path, 'http://fixture').searchParams.get('namespace') === 'monitoring',
      ),
    ).toBe(true)
    await page.screenshot({ path: info.outputPath('crd-readonly-yaml.png'), fullPage: true })
  })
}

test('BrowserRouter restores the exact history entry on dirty back, cancels safely, then confirms one discard', async ({
  page,
  catalog,
}) => {
  catalog.resource = { allowedActions: ['view', 'update'] }
  await openDirtyEditor(page)
  const original = page.url()
  const index = await page.evaluate(() => window.history.state.idx)
  await page.evaluate(() => window.history.back())
  const confirm = page.getByRole('dialog', { name: '离开当前资源？', exact: true })
  await expect(confirm).toBeVisible()
  await expect(page).toHaveURL(original)
  expect(await page.evaluate(() => window.history.state.idx)).toBe(index)
  await expect(page.locator('.soha-k8s-yaml-editor .view-lines:visible').first()).toContainText(
    'audit-unsaved-browser-draft',
  )
  await confirm.getByRole('button', { name: /^取\s*消$/ }).click()
  await expect(confirm).toHaveCount(0)
  await expect(page).toHaveURL(original)
  await page.evaluate(() => window.history.back())
  await expect(confirm).toBeVisible()
  await confirm.getByRole('button', { name: '放弃更改并切换', exact: true }).click()
  await expect(confirm).toHaveCount(0)
  await expect(page.getByRole('tab', { name: '概览', exact: true })).toHaveAttribute(
    'aria-selected',
    'true',
  )
  expect(await page.evaluate(() => window.history.state.idx)).toBe(index - 1)
  await expect(page.getByRole('textbox', { name: 'YAML 草稿编辑器', exact: true })).toHaveCount(0)
  // An accepted exit must also restore the navigator: subsequent clean back reaches the list.
  await page.evaluate(() => window.history.back())
  await expect(page.getByRole('button', { name: 'sample-widget', exact: true })).toBeVisible()
  expect(new URL(page.url()).searchParams.has('resource')).toBe(false)
})

test('BrowserRouter pending apply blocks actual back and Group links without remounting or replaying the request', async ({
  page,
  catalog,
  evidence,
}) => {
  catalog.includeSecondCluster = true
  catalog.resource = { allowedActions: ['view', 'update'] }
  await page.addInitScript(() =>
    window.addEventListener(
      'popstate',
      () => {
        document.documentElement.dataset.auditPopCount = String(
          Number(document.documentElement.dataset.auditPopCount || 0) + 1,
        )
      },
      true,
    ),
  )
  let release!: () => void
  const pending = new Promise<void>((resolve) => {
    release = resolve
  })
  const writes: Array<{ path: string; content: string }> = []
  const yamlPath = `/api/v1/clusters/cluster-a/extensions/crds/${definition.name}/resources/sample-widget/yaml`
  catalog.resolveApply = async (url, method, request) => {
    if (
      method !== 'PUT' ||
      url.pathname !== yamlPath ||
      url.search !== '?namespace=monitoring&version=v1'
    )
      return undefined
    const body = request.postDataJSON() as { content: string; namespace: string }
    expect(body.namespace).toBe('monitoring')
    writes.push({ path: url.pathname + url.search, content: body.content })
    await pending
    return { body: { data: { content: body.content } } }
  }
  try {
    await openDirtyEditor(page)
    const original = page.url()
    const index = await page.evaluate(() => window.history.state.idx)
    await page.getByRole('button', { name: /^应\s*用$/ }).click()
    await expect.poll(() => writes.length).toBe(1)
    expect(writes[0].content).toContain('audit-unsaved-browser-draft')
    const visibleDraft = page.locator('.soha-k8s-yaml-editor .view-lines:visible').first()
    await expect(visibleDraft).toContainText('audit-unsaved-browser-draft')
    await expect(page.getByRole('button', { name: /应\s*用$/ })).toBeDisabled()
    await selectOtherCluster(page)
    await expect(page).toHaveURL(original)
    expect(evidence.requests.filter((path) => path.includes('cluster-b/extensions'))).toEqual([])
    await page.evaluate(() => window.history.back())
    await expect
      .poll(() => page.evaluate(() => Number(document.documentElement.dataset.auditPopCount || 0)))
      .toBeGreaterThanOrEqual(2)
    await expect.poll(() => page.evaluate(() => window.history.state.idx)).toBe(index)
    await expect(page).toHaveURL(original)
    await expect(visibleDraft).toContainText('audit-unsaved-browser-draft')
    await expect(page.getByRole('button', { name: /应\s*用$/ })).toBeDisabled()
    await page.getByRole('link', { name: '返回API Group列表', exact: true }).click()
    await expect(page).toHaveURL(original)
    await expect(page.getByRole('dialog', { name: '离开当前资源？', exact: true })).toHaveCount(0)
    await expect(visibleDraft).toContainText('audit-unsaved-browser-draft')
    expect(writes).toHaveLength(1)
  } finally {
    release()
  }
  await expect(page.locator('.soha-operation-feedback')).toContainText('请求已返回')
  expect(writes).toHaveLength(1)
})
