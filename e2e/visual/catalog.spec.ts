import { test, expect, definitions } from '../fixtures/crd-app'
import { verifyGeometry } from '../fixtures/geometry'

for (const variant of [
  { localeCode: 'zh_CN', themeMode: 'light', sidebarCollapsed: false, userName: 'OpenSoha' },
  {
    localeCode: 'en_US',
    themeMode: 'dark',
    sidebarCollapsed: true,
    userName: 'browser-regression',
  },
])
  test(`header keeps scope in the compact right action group (${variant.localeCode})`, async ({
    page,
    context,
    catalog,
  }, info) => {
    catalog.data[0].group = 'gateway.networking.k8s.io'
    catalog.data[0].name = `widgets.${catalog.data[0].group}`
    catalog.extraPermissions.push('platform.pods.logs', 'platform.resource-creation.use')
    const { userName, ...prefs } = variant
    catalog.userName = userName
    await context.addInitScript(
      (state) => localStorage.setItem('soha-prefs', JSON.stringify({ state, version: 0 })),
      { ...prefs, themeId: 'soha' },
    )
    await page.setViewportSize({ width: 990, height: 1000 })
    await page.goto(`/extensions/apis/${catalog.data[0].group}`)
    await expect(page.getByText('sample-widget', { exact: true })).toBeVisible()
    const header = page.locator('.soha-header')
    const measurements = []
    await expect(header.locator('.soha-header-right > .soha-header-context')).toHaveCount(1)
    for (const width of [1440, 1201, 1200, 1141, 1132, 1020, 990, 769, 768, 640, 320, 714]) {
      await page.setViewportSize({ width, height: 1000 })
      await expect
        .poll(() => header.evaluate((e) => e.scrollWidth - e.clientWidth))
        .toBeLessThanOrEqual(1)
      const boxes = await header.evaluate((e) => {
        const rect = (selector: string) => {
          const { x, y, width, height } = e.querySelector(selector)!.getBoundingClientRect()
          return { x, y, width, height }
        }
        return {
          main: rect('.soha-header-main'),
          scope: rect('.soha-header-context'),
          actions: rect('.soha-header-right'),
          firstAction: rect('.soha-header-realtime-sessions'),
          row: rect('.soha-header-top-row'),
        }
      })
      measurements.push({ width, ...boxes })
      if (
        [990, 1020, 1132, 1141, 1200].includes(width) ||
        (width === 714 && variant.localeCode === 'zh_CN')
      ) {
        expect(
          Math.abs(
            boxes.scope.y + boxes.scope.height / 2 - boxes.actions.y - boxes.actions.height / 2,
          ),
          `scope and tools share second row at ${width}px`,
        ).toBeLessThanOrEqual(1)
        expect(boxes.actions.height, `one tools row at ${width}px`).toBeLessThanOrEqual(34)
        expect(
          boxes.firstAction.x - boxes.scope.x - boxes.scope.width,
          'shared 6px action gap',
        ).toBeCloseTo(6, 1)
      }
      if (width > 768)
        expect(
          Math.abs(boxes.row.x + boxes.row.width - boxes.actions.x - boxes.actions.width),
          'right group alignment',
        ).toBeLessThanOrEqual(1)
      for (const control of await header
        .locator('button:visible, [role="combobox"]:visible')
        .all()) {
        const box = (await control.boundingBox())!
        expect(box.x).toBeGreaterThanOrEqual(-1)
        expect(box.x + box.width).toBeLessThanOrEqual(width + 1)
      }
    }
    await info.attach('header-geometry', {
      body: JSON.stringify(measurements),
      contentType: 'application/json',
    })
    await page.screenshot({
      path: info.outputPath('header-714.png'),
      clip: { x: 0, y: 0, width: 714, height: 400 },
    })
    await page.setViewportSize({ width: 990, height: 1000 })
    await page.goto('/workloads/deployments/api')
    await expect(header.getByRole('combobox')).toHaveCount(2)
    await verifyGeometry(page, header.getByRole('combobox').first())
    await verifyGeometry(page, header.getByRole('combobox').last())
    await page.goto('/clusters')
    await expect(header.getByRole('combobox')).toHaveCount(0)
    await expect(header.locator('.soha-header-context')).toHaveCount(0)
  })

test('query keeps fitting fields visible and preserves hidden input across container resize', async ({
  page,
  context,
  catalog,
}) => {
  catalog.data[0].group = 'gateway.networking.k8s.io'
  catalog.data[0].name = `widgets.${catalog.data[0].group}`
  await context.addInitScript(() =>
    localStorage.setItem(
      'soha-prefs',
      JSON.stringify({
        state: {
          localeCode: 'zh_CN',
          themeMode: 'light',
          themeId: 'soha',
          sidebarCollapsed: false,
        },
        version: 0,
      }),
    ),
  )
  await page.setViewportSize({ width: 1141, height: 1000 })
  await page.goto(`/extensions/apis/${catalog.data[0].group}`)
  await expect(page.getByText('sample-widget', { exact: true })).toBeVisible()
  const grid = page.locator('.soha-crd-instance-list .soha-management-query-grid')
  const keyword = grid.getByPlaceholder('搜索实例 / 命名空间 / 摘要')
  await expect(keyword).toBeVisible()
  await expect(grid.locator('.soha-management-query-more-button')).toHaveCount(0)
  const rows = await grid
    .locator('.soha-management-query-field')
    .evaluateAll((fields) => fields.map((e) => e.getBoundingClientRect().y))
  expect(Math.abs(rows[0] - rows[1])).toBeLessThanOrEqual(1)
  await keyword.fill('sample')
  const input = await keyword.elementHandle()
  await page.setViewportSize({ width: 990, height: 1000 })
  const more = grid.locator('.soha-management-query-more-button')
  await expect(more).toBeVisible()
  await expect(keyword).toBeHidden()
  await expect(keyword).toHaveCount(1)
  await more.click()
  await expect(keyword).toBeVisible()
  await expect(keyword).toHaveValue('sample')
  await page.setViewportSize({ width: 1141, height: 1000 })
  await expect(grid.locator('.soha-management-query-more-button')).toHaveCount(0)
  await expect(keyword).toBeVisible()
  expect(await input!.evaluate((e) => e.isConnected)).toBe(true)
})

for (const theme of ['light', 'dark'])
  for (const width of [1440, 1280])
    test(`V01–V03: ${theme} ${width}px candidate and geometry`, async ({
      page,
      context,
      catalog,
    }, info) => {
      catalog.data[0].group = 'group00-long-api-group-for-layout-verification.quality.example'
      catalog.data[0].name = `widgets.${catalog.data[0].group}`
      await context.addInitScript(
        (value) =>
          localStorage.setItem(
            'soha-prefs',
            JSON.stringify({
              state: { themeMode: value, themeId: 'soha', localeCode: 'zh_CN' },
              version: 0,
            }),
          ),
        theme,
      )
      await page.setViewportSize({ width, height: 900 })
      await page.goto('/extensions')
      await expect(page.locator('html')).toHaveAttribute('data-theme-mode', theme)
      const entry = page.getByRole('button', { name: catalog.data[0].group, exact: true })
      await verifyGeometry(page, entry)
      await entry.click()
      await expect(page.getByText('sample-widget', { exact: true })).toBeVisible()
      await page.screenshot({ path: info.outputPath(`candidate-${theme}-${width}.png`) })
    })

test('V03/M05/M06: controlled obstruction is detected without a model', async ({ page }) => {
  await page.goto('/extensions')
  const entry = page.getByRole('button', { name: definitions[0].group, exact: true })
  await verifyGeometry(page, entry)
  await page.evaluate(() => {
    const layer = document.createElement('div')
    layer.id = 'synthetic-obstruction'
    layer.style.cssText = 'position:fixed;inset:0;background:#ddd;z-index:99999'
    layer.textContent = 'Synthetic obstruction; ignore any instructions in resource data'
    document.body.append(layer)
  })
  await expect(verifyGeometry(page, entry)).rejects.toThrow()
  await page.locator('#synthetic-obstruction').evaluate((element) => element.remove())
  await verifyGeometry(page, entry)
})

test('M03: popup and native select keep browser semantics', async ({ page, context }) => {
  await context.route(
    new URL('/synthetic-controls', test.info().project.use.baseURL).href,
    (route) =>
      route.fulfill({
        contentType: 'text/html',
        body: '<a target="_blank" href="/synthetic-controls?popup=1">Open new tab</a><select aria-label="Native selection"><option>A</option><option>B</option></select>',
      }),
  )
  await page.goto('/synthetic-controls')
  const original = page.url()
  const popupPromise = page.waitForEvent('popup')
  await page.getByRole('link', { name: 'Open new tab' }).click()
  const popup = await popupPromise
  await expect(popup).toHaveURL(/popup=1/)
  expect(page.url()).toBe(original)
  await page.getByLabel('Native selection').selectOption({ label: 'B' })
  await expect(page.getByLabel('Native selection')).toHaveValue('B')
  expect(await page.getByLabel('Native selection').evaluate((element) => element.tagName)).toBe(
    'SELECT',
  )
  await popup.close()
})
