import { test, expect } from '@playwright/test'
import { installMocks, verifyBrowserEvidence } from '../fixtures/mock-app'
import { task114Response } from '../fixtures/task114-delivery'

for (const height of [1206, 800]) {
  test(`Task114 service rail stays independent across tabs at ${height}px height`, async ({
    context,
    page,
  }) => {
    await page.setViewportSize({ width: 1112, height })
    const evidence = await installMocks(context, page, (url, method) => {
      if (/\/workloads\/pods\/order-api-pod-\d+\/metrics$/.test(url.pathname)) {
        const metricURL = new URL(url)
        metricURL.pathname = metricURL.pathname.replace(/order-api-pod-\d+/, 'order-api-pod-0')
        return task114Response(metricURL, method)
      }
      const response = task114Response(url, method)
      if (url.pathname.endsWith('/workloads/order-api/runtime') && response) {
        const body = response.body as { data: { pods: Array<{ name: string }> } }
        const pod = body.data.pods[0]
        body.data.pods = Array.from({ length: 24 }, (_, index) => ({
          ...pod,
          name: `order-api-pod-${index}`,
        }))
      }
      return response
    })
    await page.goto(
      '/applications/retail?applicationEnvironmentId=retail-test&serviceId=order-api&serviceTab=runtime',
    )
    const rail = page.locator('.soha-service-selector')
    await expect(page.getByRole('heading', { name: '订单 API', exact: true })).toBeVisible()
    await expect(page.locator('.soha-service-runtime-overview')).toBeVisible()
    const initial = (await rail.boundingBox())!
    for (const name of [
      /^Pods/,
      /^指标$/,
      /^发布与工作流$/,
      /^配置与模板$/,
      /^状态$/,
      /^资源关系$/,
    ]) {
      await page.getByRole('tab', { name }).click()
      await expect(page.getByRole('tab', { name })).toHaveAttribute('aria-selected', 'true')
      await page.evaluate(
        () => new Promise((resolve) => requestAnimationFrame(() => requestAnimationFrame(resolve))),
      )
      await expect
        .poll(async () => (await rail.boundingBox())!.height)
        .toBeCloseTo(initial.height, 0)
      expect((await rail.boundingBox())!.y).toBeCloseTo(initial.y, 0)
    }
    await page.getByRole('tab', { name: /^Pods/ }).click()
    const services = rail.locator('.soha-service-selector__list')
    await services.hover()
    await page.mouse.wheel(0, 200)
    if (height === 800)
      await expect.poll(() => services.evaluate((el) => el.scrollTop)).toBeGreaterThan(0)
    const leftScroll = await services.evaluate((el) => el.scrollTop)
    const pods = page.locator('.soha-pod-list-scroll')
    await pods.hover()
    await page.mouse.wheel(0, 400)
    await expect.poll(() => pods.evaluate((el) => el.scrollTop)).toBeGreaterThan(0)
    expect(await services.evaluate((el) => el.scrollTop)).toBe(leftScroll)
    await expect(rail.getByRole('button', { name: /添加服务$/ })).toBeInViewport()
    await expect(page.getByRole('heading', { name: '订单 API', exact: true })).toBeInViewport()
    await page.getByRole('tab', { name: /^资源关系$/ }).click()
    expect(await services.evaluate((el) => el.scrollTop)).toBe(leftScroll)
    await rail.getByRole('button', { name: '服务总览', exact: true }).click()
    await expect.poll(async () => (await rail.boundingBox())!.height).toBeCloseTo(initial.height, 0)
    verifyBrowserEvidence(evidence)
  })
}

for (const width of [1440, 390]) {
  test(`Task114 application context and metrics stay isolated at ${width}px`, async ({
    context,
    page,
  }) => {
    await page.setViewportSize({ width, height: 1000 })
    const evidence = await installMocks(context, page, task114Response)
    page.on('pageerror', (error) => evidence.fatal.push(error.message))
    await page.goto(
      '/applications/retail?tab=services&applicationEnvironmentId=retail-test&serviceId=order-api',
    )
    await expect(page.getByRole('heading', { name: '订单 API', exact: true })).toBeVisible()
    const rail = page.locator('.soha-service-selector')
    await expect(rail.getByRole('button', { name: /添加服务$/ })).toBeVisible()
    await rail.getByRole('textbox', { name: '搜索服务', exact: true }).fill('no-match')
    await expect(rail).toContainText('没有匹配的服务')
    await expect(page.getByRole('heading', { name: '订单 API', exact: true })).toBeVisible()
    await rail.getByRole('textbox', { name: '搜索服务', exact: true }).fill('')
    await page.getByRole('tab', { name: /^Pods/ }).click()
    const monitor = page.getByRole('button', { name: '查看监控 order-api-pod-0', exact: true })
    await monitor.scrollIntoViewIfNeeded()
    await expect(page.locator('.soha-pod-card-chart canvas').first()).toBeVisible()
    await monitor.click()
    await expect(page.getByRole('tab', { name: '指标', exact: true })).toHaveAttribute(
      'aria-selected',
      'true',
    )
    await expect(page.locator('.soha-service-metrics')).toContainText('order-api-pod-0')
    await expect(page.locator('.ant-drawer-open')).toHaveCount(0)
    await page
      .getByRole('navigation', { name: '选择环境', exact: true })
      .getByRole('button', { name: /生产环境/ })
      .click()
    await expect(page).toHaveURL(/applicationEnvironmentId=retail-prod/)
    await expect(page).not.toHaveURL(/metricPod=/)
    await expect(page.locator('.soha-service-console__body')).not.toContainText('order-api-pod-0')
    await page
      .getByRole('navigation', { name: '选择环境', exact: true })
      .getByRole('button', { name: /测试环境/ })
      .click()
    await expect(page.locator('.soha-service-metrics')).toContainText('请选择 Pod 查看指标')
    await page.getByRole('button', { name: '切换应用', exact: true }).click()
    const picker = page.getByRole('region', { name: '应用切换面板', exact: true })
    await picker.getByRole('textbox', { name: '搜索应用', exact: true }).fill('数据平台')
    await picker.getByRole('button', { name: /实时计算/ }).click()
    await expect(page).toHaveURL(/applications\/analytics/)
    await expect(page.locator('.soha-service-console__body')).toContainText('暂无服务')
    await page.getByRole('button', { name: '切换应用', exact: true }).click()
    await page
      .getByRole('region', { name: '应用切换面板', exact: true })
      .getByRole('button', { name: /零售业务/ })
      .click()
    await expect(page).toHaveURL(/applicationEnvironmentId=retail-test/)
    await expect(page).toHaveURL(/serviceTab=metrics/)
    await expect(page.getByRole('heading', { name: '订单 API', exact: true })).toBeVisible()
    await expect(picker).toBeHidden()
    const geometry = await page.evaluate(() => ({
      width: document.documentElement.clientWidth,
      scroll: document.documentElement.scrollWidth,
    }))
    expect(geometry.scroll).toBe(geometry.width)
    verifyBrowserEvidence(evidence)
  })
}
