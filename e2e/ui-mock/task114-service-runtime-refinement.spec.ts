import { test, expect, type BrowserContext, type Page } from '@playwright/test'
import { installMocks, verifyBrowserEvidence } from '../fixtures/mock-app'
import { task114Response } from '../fixtures/task114-delivery'

const target = '/applications/retail?applicationEnvironmentId=retail-test&serviceId=order-api'
async function setup(context: BrowserContext, page: Page) {
  let uid = 'uid-order-api-pod-0'
  const evidence = await installMocks(context, page, (url, method) => {
    const path = url.pathname.replace(/^\/api\/v1/, '')
    const data = (value: unknown) => ({ body: { data: value } })
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
  }
}

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
