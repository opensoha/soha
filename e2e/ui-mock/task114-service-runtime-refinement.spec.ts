import { test, expect, type BrowserContext, type Page } from '@playwright/test'
import { installMocks, verifyBrowserEvidence } from '../fixtures/mock-app'
import { task114Response } from '../fixtures/task114-delivery'

const target = '/applications/retail?applicationEnvironmentId=retail-test&serviceId=order-api'
async function setup(context: BrowserContext, page: Page, monitoring = true) {
  let uid = 'uid-order-api-pod-0'
  let replaceDuringMetrics = false
  let metricResponseError = false
  const evidence = await installMocks(context, page, (url, method) => {
    const path = url.pathname.replace(/^\/api\/v1/, '')
    const data = (value: unknown) => ({ body: { data: value } })
    if (path.endsWith('/metrics') && metricResponseError)
      return data({ configured: true, series: [], message: 'Prometheus fixture unavailable' })
    if (path.endsWith('/order-api-pod-0/metrics') && replaceDuringMetrics) uid = 'replacement-uid'
    if (path.endsWith('/metrics') && !monitoring) return data({ configured: false, series: [] })
    const detailPod = path.match(
      /^\/clusters\/cluster-a\/workloads\/pods\/(order-api-pod-\d+)\/detail$/,
    )?.[1]
    if (detailPod)
      return data({
        name: detailPod,
        namespace: 'retail-test',
        uid: detailPod === 'order-api-pod-0' ? uid : `uid-${detailPod}`,
        phase: 'Running',
        allowedActions: ['logs', 'exec'],
        containers: [{ name: 'main', role: 'main', image: 'test:1', ready: true, restartCount: 0 }],
      })
    if (path === '/clusters/cluster-a/workloads/pods/order-api-pod-0/yaml')
      return data({
        content: JSON.stringify({
          apiVersion: 'v1',
          kind: 'Pod',
          metadata: { name: 'order-api-pod-0', namespace: 'retail-test', uid },
          spec: { containers: [{ name: 'main', image: 'test:1' }] },
          status: {
            containerStatuses: [
              { name: 'main', ready: true, restartCount: 0, state: { running: {} } },
            ],
          },
        }),
      })
    if (path === '/clusters/cluster-a/events') return data([])
    const response = task114Response(url, method)
    if (!response) return undefined
    if (path === '/applications') {
      const body = structuredClone(response.body) as { data: Array<{ name: string }> }
      body.data[0].name = 'AWS Retail Store · 零售微服务'
      return { ...response, body }
    }
    if (path.endsWith('/detail')) {
      const body = structuredClone(response.body) as { data: { application?: { name: string } } }
      if (body.data.application) body.data.application.name = 'AWS Retail Store · 零售微服务'
      return { ...response, body }
    }
    if (path.endsWith('/runtime')) {
      const body = structuredClone(response.body) as {
        data: {
          application?: { name: string }
          pods?: Array<{ name: string; uid?: string }>
        }
      }
      if (body.data.application) body.data.application.name = 'AWS Retail Store · 零售微服务'
      if (body.data.pods)
        body.data.pods = body.data.pods.map((pod) => ({ ...pod, uid: `uid-${pod.name}` }))
      return { ...response, body }
    }
    return response
  })
  return {
    evidence,
    replacePod: () => {
      uid = 'replacement-uid'
    },
    raceMetrics: () => {
      replaceDuringMetrics = true
    },
    failMetricsResponse: () => {
      metricResponseError = true
    },
  }
}

test('Task114 service summary rejects Pod replacement during a metrics read', async ({
  context,
  page,
}) => {
  const { evidence, raceMetrics } = await setup(context, page)
  raceMetrics()
  await page.goto(`${target}&serviceTab=runtime`)
  const indicators = page.getByRole('region', { name: '服务运行指标', exact: true })
  await expect(indicators).toContainText('指标读取失败')
  await expect(indicators.locator('dd strong')).toHaveText(['2/2', '1', '—', '—'])
  await expect(indicators.getByRole('button', { name: '重试运行指标', exact: true })).toBeVisible()
  verifyBrowserEvidence(evidence)
})

test('Task114 service summary surfaces HTTP 200 monitoring failures', async ({ context, page }) => {
  const { evidence, failMetricsResponse } = await setup(context, page)
  failMetricsResponse()
  await page.goto(`${target}&serviceTab=runtime`)
  const indicators = page.getByRole('region', { name: '服务运行指标', exact: true })
  await expect(indicators).toContainText('指标读取失败')
  await expect(indicators.getByRole('button', { name: '重试运行指标', exact: true })).toBeVisible()
  verifyBrowserEvidence(evidence)
})

for (const width of [1280, 390]) {
  test(`Task114 service landing shows scoped runtime indicators at ${width}px`, async ({
    context,
    page,
  }) => {
    await page.setViewportSize({ width, height: 1000 })
    const { evidence } = await setup(context, page)
    await page.goto(`${target}&serviceTab=runtime`)
    const indicators = page.getByRole('region', { name: '服务运行指标', exact: true })
    await expect(indicators).toContainText('2/2')
    await expect(indicators.locator('dd strong')).toHaveText(['2/2', '1', '368 mCPU', '824 MB'])
    await indicators.getByRole('button', { name: 'Pod 就绪', exact: true }).click()
    await expect(page.getByRole('tab', { name: /^Pods/ })).toHaveAttribute('aria-selected', 'true')
    await page.getByRole('tab', { name: '运行资源', exact: true }).click()
    await expect(indicators).toContainText('368 mCPU')
    await page
      .getByRole('navigation', { name: '选择环境', exact: true })
      .getByRole('button', { name: /生产环境/ })
      .click()
    await expect(indicators).not.toContainText('368 mCPU')
    await expect(indicators).not.toContainText('2/2')
    verifyBrowserEvidence(evidence)
  })
}

test('Task114 service landing separates missing monitoring from zero usage', async ({
  context,
  page,
}) => {
  const { evidence } = await setup(context, page, false)
  await page.goto(`${target}&serviceTab=runtime`)
  const indicators = page.getByRole('region', { name: '服务运行指标', exact: true })
  await expect(indicators.locator('dd strong')).toHaveText(['2/2', '1', '—', '—'])
  await expect(indicators.getByText('未接入监控', { exact: true })).toHaveCount(2)
  verifyBrowserEvidence(evidence)
})

for (const width of [1440, 1034, 900, 390]) {
  test(`Task114 header pushes the environment below wrapped chrome at ${width}px`, async ({
    context,
    page,
  }) => {
    await page.setViewportSize({ width, height: 1110 })
    const { evidence } = await setup(context, page)
    await page.goto(`${target}&serviceTab=runtime`)
    await expect(page.getByRole('heading', { name: '订单 API', exact: true })).toBeVisible()
    const topbar = page.locator('.soha-application-topbar')
    await expect(topbar.getByText('AWS Retail Store · 零售微服务', { exact: true })).toBeVisible()
    const environment = page.locator('.soha-application-environments')
    await expect
      .poll(async () => {
        const a = (await topbar.boundingBox())!
        const b = (await environment.boundingBox())!
        return b.y - a.y - a.height
      })
      .toBeGreaterThanOrEqual(8)
    const chrome = (await topbar.boundingBox())!
    if (width === 1440) expect(chrome.height).toBe(56)
    if (width === 900) expect(chrome.height).toBeGreaterThan(56)
    if (width > 767) {
      const rail = (await page.locator('.soha-service-selector').boundingBox())!
      expect(rail.y + rail.height).toBeLessThanOrEqual(1110)
    }
    expect(await page.evaluate(() => document.documentElement.scrollWidth > innerWidth)).toBe(false)
    verifyBrowserEvidence(evidence)
  })
}

for (const width of [1280, 390]) {
  test(`Task114 Pod inspectors share the application drawer frame at ${width}px`, async ({
    context,
    page,
  }) => {
    await page.setViewportSize({ width, height: 1000 })
    const { evidence, replacePod } = await setup(context, page)
    await page.goto(`${target}&serviceTab=pods`)
    const filter = page.locator('.soha-pod-list-filter')
    await expect(filter).toContainText('2 个 Pod')
    await expect(filter.getByText('Pods', { exact: true })).toHaveCount(0)
    const pod = page.locator('.soha-pod-card').filter({ hasText: 'order-api-pod-0' })
    for (const [action, title] of [
      ['详情', 'Pod详情'],
      ['事件', 'Pod事件'],
      ['YAML', 'Pod YAML'],
    ]) {
      await pod.getByRole('button', { name: action, exact: true }).click()
      const drawer = page.locator('.ant-drawer-open')
      await expect(drawer.locator('.soha-pod-drawer-title strong')).toHaveText(title)
      await expect(drawer.locator('.soha-pod-drawer-identity')).toContainText('order-api-pod-0')
      await expect(drawer.locator('.soha-pod-drawer-body')).toHaveCSS('padding', '16px')
      await expect
        .poll(async () => (await drawer.locator('.ant-drawer-section').boundingBox())!.width)
        .toBeCloseTo(Math.min(width, 1040), 0)
      const header = (await drawer.locator('.soha-pod-drawer-header').boundingBox())!
      expect(header.height).toBeGreaterThanOrEqual(56)
      const close = (await drawer.locator('.ant-drawer-close').boundingBox())!
      expect(close.x).toBeGreaterThan(width - 60)
      if (action === 'YAML')
        await expect(drawer.getByLabel('Pod YAML', { exact: true })).toContainText(
          'uid-order-api-pod-0',
        )
      await drawer.locator('.ant-drawer-close').click()
      await expect(drawer).toHaveCount(0)
    }
    replacePod()
    await pod.getByRole('button', { name: '详情', exact: true }).click()
    await expect(page.locator('.ant-drawer-open')).toContainText('Pod 已重建或 UID 无法确认')
    verifyBrowserEvidence(evidence)
  })
}
