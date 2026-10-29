import { test, expect, definitions } from '../fixtures/crd-app'
import { verifyGeometry } from '../fixtures/geometry'

for (const variant of [
  { theme: 'light', width: 1232, scope: 'Cluster', long: false },
  { theme: 'dark', width: 1020, scope: 'Namespaced', long: true },
  { theme: 'light', width: 390, scope: 'Namespaced', long: true },
  { theme: 'light', width: 640, scope: 'Cluster', long: false },
])
  test(`resource summary keeps metadata and read-only actions usable (${variant.theme}, ${variant.width}px)`, async ({
    page,
    context,
    catalog,
  }, info) => {
    const name = `task113-envoy${variant.long ? '-long-resource-name'.repeat(8) : ''}`
    const finalizer = 'gateway-exists-finalizer.gateway.networking.k8s.io'
    const definition = catalog.data[0]
    definition.group = variant.long
      ? `${'long-api-group.'.repeat(4)}quality.example`
      : 'gateway.networking.k8s.io'
    definition.kind = variant.scope === 'Cluster' ? 'GatewayClass' : 'Gateway'
    definition.name = `${variant.scope === 'Cluster' ? 'gatewayclasses' : 'gateways'}.${definition.group}`
    definition.scope = variant.scope
    catalog.resource = {
      name,
      allowedActions: ['view'],
      apiVersion: `${definition.group}/v1`,
      finalizers: variant.long ? [finalizer, `cleanup.${definition.group}`] : [],
      summary: { controller: 'gateway.envoyproxy.io/gatewayclass-controller' },
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
    await page.goto(`/extensions/apis/${definition.group}`)
    await page.getByRole('button', { name, exact: true }).click()
    const heading = page.locator('.soha-crd-resource-heading')
    await expect(heading.getByRole('heading', { name, exact: true })).toBeVisible()
    await expect(page.getByText('YAML · 只读', { exact: true })).toBeVisible()
    await expect(page.getByLabel('资源 YAML，只读', { exact: true })).toBeVisible()
    if (variant.long) {
      const identity = await heading.locator('.soha-crd-resource-identity').boundingBox()
      const summary = await heading.boundingBox()
      expect(identity!.width / summary!.width).toBeGreaterThanOrEqual(0.5)
    }
    await expect(heading).toContainText(variant.scope === 'Cluster' ? '集群范围' : 'monitoring')
    await expect(heading).toContainText('controller')
    await expect(heading).toContainText('gateway.envoyproxy.io/gatewayclass-controller')
    await expect(heading.getByRole('button', { name: '编辑', exact: true })).toBeDisabled()
    await expect(heading.getByRole('button', { name: '删除', exact: true })).toHaveCount(0)
    const metadata = heading.locator('details')
    await expect(metadata).not.toHaveAttribute('open')
    const disclosure = metadata.locator('summary')
    await disclosure.scrollIntoViewIfNeeded()
    await verifyGeometry(page, disclosure)
    await disclosure.focus()
    await disclosure.press('Enter')
    await expect(metadata).toHaveAttribute('open', '')
    await expect(metadata).toContainText('支持版本')
    await expect(metadata).toContainText('Finalizers')
    if (variant.long) await expect(metadata).toContainText(finalizer)
    else await expect(metadata).toContainText('—')
    await verifyGeometry(page, disclosure)
    await page.screenshot({ path: info.outputPath('resource-summary.png'), fullPage: true })
    const back = heading.getByRole('button', { name: /返回实例列表/ })
    await back.scrollIntoViewIfNeeded()
    await verifyGeometry(page, back)
    await back.click()
    await expect(page.getByRole('button', { name, exact: true })).toBeVisible()
  })

test('U07: multiple kinds switch the exact scoped resource request', async ({
  page,
  catalog,
  evidence,
}) => {
  catalog.data.push({
    ...definitions[0],
    uid: 'synthetic-gadget',
    name: `gadgets.${definitions[0].group}`,
    kind: 'Gadget',
    plural: 'gadgets',
  })
  await page.goto(`/extensions/apis/${definitions[0].group}`)
  await expect(page.getByText('sample-gadget', { exact: true })).toBeVisible()
  await page
    .getByRole('button')
    .filter({ hasText: /^Widget/ })
    .click()
  await expect(page.getByText('sample-widget', { exact: true })).toBeVisible()
  expect(
    evidence.requests.some(
      (url) =>
        url.includes(`/widgets.${definitions[0].group}/resources`) &&
        url.includes('namespace=monitoring'),
    ),
  ).toBe(true)
})

test('U08: legacy page links open the complete catalog and preserve query on return', async ({
  page,
}) => {
  await page.goto('/extensions?q=group&page=2')
  await expect(page.locator('.ant-pagination')).toHaveCount(0)
  await expect(page.getByRole('button', { name: definitions[0].group, exact: true })).toBeVisible()
  const list = page.getByRole('region', { name: '资源列表，可连续滚动', exact: true })
  await list.focus()
  await list.press('End')
  await page.getByRole('button', { name: definitions[17].group, exact: true }).click()
  await expect(page.getByText('sample-widget', { exact: true })).toBeVisible()
  await page.goBack()
  await expect(page).toHaveURL(/\/extensions\?q=group&page=2$/)
  await expect(page.getByPlaceholder('搜索 API Group / CRD / Kind / Version')).toHaveValue('group')
  await expect(page.getByRole('button', { name: definitions[0].group, exact: true })).toBeVisible()
})

test('U02–U04: complete local catalog query, continuous scrolling, sorting and reset', async ({
  page,
  evidence,
}) => {
  await page.goto('/extensions')
  await expect(page.getByRole('button', { name: definitions[0].group, exact: true })).toBeVisible()
  await expect(page.locator('.ant-pagination')).toHaveCount(0)
  const list = page.getByRole('region', { name: '资源列表，可连续滚动', exact: true })
  await list.focus()
  await list.press('End')
  await expect(page.getByRole('button', { name: definitions[17].group, exact: true })).toBeVisible()
  const search = page.getByPlaceholder('搜索 API Group / CRD / Kind / Version')
  await search.fill('group17')
  await expect(page.getByRole('button', { name: definitions[17].group, exact: true })).toBeVisible()
  await expect(page.getByRole('button', { name: definitions[16].group, exact: true })).toHaveCount(
    0,
  )
  await search.fill('')
  await expect.poll(() => list.evaluate((node) => node.scrollTop)).toBe(0)
  await page.getByRole('columnheader', { name: 'API Group' }).click()
  await expect(page.getByRole('button', { name: definitions[0].group, exact: true })).toBeVisible()
  await page.getByRole('columnheader', { name: 'API Group' }).click()
  await expect(page.getByRole('button', { name: definitions[17].group, exact: true })).toBeVisible()
  expect(evidence.requests.filter((url) => url.includes('/extensions/crds'))).toEqual([
    '/api/v1/clusters/cluster-a/extensions/crds',
  ])
})

for (const keyboard of [false, true])
  test(`U05/U06: ${keyboard ? 'keyboard' : 'mouse'} name enters exact API group`, async ({
    page,
  }) => {
    await page.goto('/extensions')
    const entry = page.getByRole('button', { name: definitions[0].group, exact: true })
    if (keyboard) {
      await entry.focus()
      await expect(entry).toBeFocused()
      await entry.press('Enter')
    } else await entry.click()
    await expect(page).toHaveURL(
      new RegExp('/extensions/apis/' + definitions[0].group.replace(/\./g, '\\.')),
    )
    await expect(page.getByText('sample-widget', { exact: true })).toBeVisible()
  })

test('U09–U11: exact deletion confirmation, failure context, retry and duplicate submission', async ({
  page,
  catalog,
}) => {
  await page.goto('/extensions')
  await page.getByRole('button', { name: '删除 CRD 定义', exact: true }).first().click()
  const dialog = page.getByRole('dialog')
  await expect(dialog).toContainText('所有命名空间')
  await dialog.getByLabel('选择具体 CRD 定义').click()
  await page.getByTitle(`${definitions[0].name} · Widget`, { exact: true }).click()
  const input = dialog.getByLabel('输入完整 CRD 名称确认')
  const submit = dialog.getByRole('button', { name: '删除定义及实例' })
  await input.fill('wrong-name')
  await expect(submit).toBeDisabled()
  expect(catalog.deleteCalls).toEqual([])
  await input.fill(definitions[0].name)
  await submit.click()
  await expect(dialog).toContainText('删除 CRD 定义：操作请求失败')
  await expect(input).toHaveValue(definitions[0].name)
  expect(catalog.deleteCalls[0]).toContain('expectedUid=synthetic-uid-0')
  catalog.failDelete = false
  await submit.dblclick()
  await expect(dialog).toHaveCount(0)
  expect(catalog.deleteCalls).toHaveLength(2)
  expect(catalog.data.some((item) => item.uid === definitions[0].uid)).toBe(false)
})

test('U09: unknown deletion keeps the original confirmation and blocks another request', async ({
  page,
  catalog,
}) => {
  catalog.deleteFailureStatus = 503
  await page.goto('/extensions')
  await page.getByRole('button', { name: '删除 CRD 定义', exact: true }).first().click()
  const dialog = page.getByRole('dialog')
  await dialog.getByLabel('选择具体 CRD 定义').click()
  await page.getByTitle(`${definitions[0].name} · Widget`, { exact: true }).click()
  const input = dialog.getByLabel('输入完整 CRD 名称确认')
  const submit = dialog.getByRole('button', { name: '删除定义及实例' })
  await input.fill(definitions[0].name)
  await submit.click()
  await expect(dialog.locator('.soha-operation-feedback')).toContainText(
    '删除 CRD 定义：请求结果不明',
  )
  await expect(dialog).toContainText('crd-delete-original-request')
  await expect(dialog.locator('.soha-operation-feedback')).toHaveCount(1)
  await expect(submit).toBeDisabled()
  await expect(input).toBeDisabled()
  await expect(input).toHaveValue(definitions[0].name)
  await page.keyboard.press('Escape')
  await expect(dialog).toBeVisible()
  expect(catalog.deleteCalls).toHaveLength(1)
  expect(catalog.deleteCalls[0]).toContain('expectedUid=synthetic-uid-0')
  expect(catalog.data.some((item) => item.uid === definitions[0].uid)).toBe(true)
})

test('U12: no deletion permission removes dangerous UI entry', async ({ page, catalog }) => {
  catalog.allowDelete = false
  await page.goto('/extensions')
  await expect(page.getByRole('button', { name: definitions[0].group, exact: true })).toBeVisible()
  await expect(page.getByRole('button', { name: '删除 CRD 定义', exact: true })).toHaveCount(0)
})

test('U12: catalog failure stays distinguishable from empty results', async ({ page, catalog }) => {
  catalog.failCatalog = true
  await page.goto('/extensions')
  await expect(page.getByText(/加载失败|Synthetic catalog unavailable/).first()).toBeVisible({
    timeout: 15_000,
  })
})

test('instance namespace selection changes exact list and access requests without enabling writes', async ({
  page,
  evidence,
}) => {
  await page.goto(`/extensions/apis/${definitions[0].group}`)
  const picker = page.getByRole('combobox', { name: '选择资源命名空间', exact: true })
  await picker.click()
  await page.locator('.ant-select-dropdown').getByText('全部命名空间', { exact: true }).click()
  await expect(picker).toHaveAttribute('aria-expanded', 'false')
  await expect
    .poll(() =>
      evidence.requests.some(
        (url) =>
          url.includes(`/widgets.${definitions[0].group}/resources`) && !url.includes('namespace='),
      ),
    )
    .toBe(true)
  await expect
    .poll(() =>
      evidence.requests.some(
        (url) =>
          url.includes(`/widgets.${definitions[0].group}/access`) && !url.includes('namespace='),
      ),
    )
    .toBe(true)
  await expect(page.getByRole('button', { name: '新建实例', exact: true })).toBeDisabled()
})

test('a cluster-scoped kind hides the namespace picker and ignores its filter', async ({
  page,
  catalog,
  evidence,
}) => {
  catalog.data.push({
    ...definitions[0],
    name: `clusterwidgets.${definitions[0].group}`,
    kind: 'ClusterWidget',
    plural: 'clusterwidgets',
    scope: 'Cluster',
  })
  await page.goto(`/extensions/apis/${definitions[0].group}`)
  await expect(page.getByText('sample-clusterwidget', { exact: true })).toBeVisible()
  await expect(page.getByRole('combobox', { name: '选择资源命名空间', exact: true })).toHaveCount(0)
  const requests = evidence.requests.filter((url) =>
    url.includes(`/clusterwidgets.${definitions[0].group}/resources`),
  )
  expect(requests).toHaveLength(1)
  expect(requests[0]).not.toContain('namespace=')
})
