import { test, expect } from '@playwright/test'
import { installMocks, verifyBrowserEvidence } from '../fixtures/mock-app'
import { task114Response } from '../fixtures/task114-delivery'

const target =
  '/applications/retail?tab=delivery&applicationEnvironmentId=retail-test&serviceId=order-api'
for (const width of [1440, 1024, 768, 320]) {
  test(`Service delivery cards and preparation at ${width}px`, async ({ context, page }, info) => {
    const writes: string[] = []
    await page.setViewportSize({ width, height: 1000 })
    if (width === 320)
      await page.addInitScript(() =>
        localStorage.setItem(
          'soha-prefs',
          JSON.stringify({ state: { themeMode: 'dark', localeCode: 'zh_CN' }, version: 0 }),
        ),
      )
    const evidence = await installMocks(context, page, (url, method) => {
      const path = url.pathname.replace(/^\/api\/v1/, '')
      if (method !== 'GET' && path !== '/auth/refresh') {
        writes.push(`${method} ${path}`)
        return undefined
      }
      if (path === '/delivery-batches')
        return {
          body: {
            data: ['completed', 'failed'].map((status, index) => ({
              id: `delivery-${index}`,
              rootRunId: `run-${index}`,
              status,
              definition: { name: 'Application delivery', mode: 'service_serial', targets: [] },
              targets: [
                {
                  target: {
                    id: 'target',
                    applicationId: 'retail',
                    serviceId: 'order-api',
                    applicationEnvironmentId: 'retail-test',
                    action: 'config_update',
                  },
                  serviceName: '订单 API',
                  applicationName: '零售业务',
                  environmentName: '测试环境',
                  manifestRevision: 12,
                  serviceVersion: 1,
                  configurationDigest: 'digest',
                },
                ...(index === 1
                  ? [
                      {
                        target: {
                          id: 'target-prod',
                          applicationId: 'retail',
                          serviceId: 'order-api',
                          applicationEnvironmentId: 'retail-prod',
                          action: 'config_update',
                        },
                        serviceName: '订单 API',
                        applicationName: '零售业务',
                        environmentName: '生产环境',
                        manifestRevision: 13,
                        serviceVersion: 1,
                        configurationDigest: 'prod-digest',
                      },
                    ]
                  : []),
              ],
              nodes: [],
              serviceCount: 1,
              targetCount: index === 1 ? 2 : 1,
              buildCount: 0,
              createdBy: 'tester',
              createdAt: `2026-11-08T0${index + 1}:00:00Z`,
              updatedAt: `2026-11-08T0${index + 1}:01:00Z`,
            })),
          },
        }
      if (path === '/delivery/release-bundles') return { body: { data: [] } }
      const response = task114Response(url, method)
      if (path === '/access/permission-snapshot' && response) {
        const body = structuredClone(response.body) as { data: { permissionKeys: string[] } }
        body.data.permissionKeys.push(
          'delivery.workflows.view',
          'delivery.workflows.trigger',
          'delivery.releases.trigger',
          'delivery.builds.trigger',
        )
        return { ...response, body }
      }
      return response
    })
    await page.goto(target)
    const drawer = page.getByRole('dialog')
    await expect(drawer.getByText('最近交付记录', { exact: true })).toBeVisible()
    await expect(drawer.getByText('环境工作流', { exact: true })).toHaveCount(0)
    const records = drawer.getByRole('list', { name: '最近交付记录' })
    await expect(records.getByRole('listitem')).toHaveCount(2)
    await expect(records).toContainText('已完成')
    await expect(records).toContainText('失败')
    await expect(records.getByRole('listitem').first()).toContainText('配置版本 12')
    const multiTarget = records.getByRole('listitem').last()
    await expect(multiTarget).toContainText('2 个交付目标')
    await expect(multiTarget).toContainText('测试环境、生产环境')
    await expect(multiTarget).not.toContainText('配置版本')
    await expect(records.getByRole('link').first()).toHaveAttribute(
      'href',
      '/delivery/batches/delivery-0',
    )
    const header = drawer.locator('.ant-drawer-header')
    await expect(header).toHaveCSS('min-height', '56px')
    await drawer.getByRole('button', { name: '检查交付参数', exact: true }).click()
    await expect(drawer.getByText('确认交付参数', { exact: true })).toBeVisible()
    await expect(drawer.getByRole('button', { name: '确认并开始', exact: true })).toBeVisible()
    expect(writes).toEqual([])
    await drawer.getByRole('button', { name: '返回修改', exact: true }).click()
    await expect(drawer.getByText('确认交付参数', { exact: true })).toHaveCount(0)
    await expect
      .poll(() =>
        page.evaluate(
          () => document.documentElement.scrollWidth <= document.documentElement.clientWidth,
        ),
      )
      .toBe(true)
    await drawer.screenshot({
      path: info.outputPath(`delivery-${width}.png`),
      animations: 'disabled',
    })
    verifyBrowserEvidence(evidence)
  })
}
