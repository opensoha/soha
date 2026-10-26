import { test, expect } from '@playwright/test'
import { installMocks, verifyBrowserEvidence } from '../fixtures/mock-app'
import { task114Response } from '../fixtures/task114-delivery'

for (const width of [1440, 390]) {
  for (const source of ['kubernetes', 'helm'] as const) {
    test(`Task114 maps ${source} to an existing service without create permission at ${width}px`, async ({
      context,
      page,
    }, testInfo) => {
      await page.setViewportSize({ width, height: 1100 })
      if (width === 390)
        await page.addInitScript(() =>
          localStorage.setItem(
            'soha-prefs',
            JSON.stringify({ state: { themeMode: 'dark', localeCode: 'zh_CN' }, version: 0 }),
          ),
        )
      const writes: { path: string; payload: Record<string, unknown> }[] = []
      const evidence = await installMocks(context, page, (url, method, request) => {
        const path = url.pathname.replace(/^\/api\/v1/, '')
        const data = (value: unknown) => ({ body: { data: value } })
        if (path === '/access/permission-snapshot')
          return data({
            permissionKeys: [
              'workbench.delivery.view',
              'workspace.application.view',
              'delivery.applications.view',
              'delivery.application-services.view',
              'delivery.application-services.update',
              'delivery.application-environments.view',
              'delivery.application-environments.create',
            ],
            visibleMenuIds: ['applications'],
            visibleMenus: [{ id: 'applications', path: '/applications', labelZh: '应用中心' }],
          })
        if (path === '/clusters/cluster-a/namespaces')
          return data([{ name: 'orders-b', status: 'Active', ageSeconds: 10 }])
        if (path === '/clusters/cluster-a/helm/releases')
          return data([
            {
              name: 'orders',
              namespace: 'orders-b',
              revision: '3',
              status: 'deployed',
              chart: 'orders-1.0.0',
            },
          ])
        if (path === '/application-environments/target-candidates')
          return {
            body: {
              items: [
                {
                  clusterId: 'cluster-a',
                  namespace: 'orders-b',
                  workloadKind: 'Deployment',
                  workloadName: 'orders',
                  readyReplicas: 1,
                  desiredReplicas: 1,
                  relatedResources: [],
                  source: {
                    resource: { uid: 'orders-uid', kind: 'Deployment', namespace: 'orders-b' },
                    specDigest: 'a'.repeat(64),
                  },
                },
              ],
              truncated: false,
            },
          }
        if (method === 'POST' && path.startsWith('/application-environments/')) {
          writes.push({ path, payload: request.postDataJSON() })
          return {
            status: source === 'helm' ? 404 : 409,
            body: {
              error: {
                code: source === 'helm' ? 'not_found' : 'conflict',
                message:
                  source === 'helm' ? '当前 Core 不支持明确服务映射' : '源资源已变化，请重新预览',
                requestId: 'synthetic-mapping',
              },
            },
          }
        }
        return task114Response(url, method)
      })
      await page.goto('/applications')
      await page.getByRole('button', { name: '接入已有资源', exact: true }).click()
      const dialog = page.getByRole('dialog', { name: '导入集群服务' })
      if (source === 'helm') await dialog.getByText('Helm Release', { exact: true }).click()
      if (source === 'helm')
        await expect(dialog.getByRole('radio', { name: 'Helm Release', exact: true })).toBeChecked()
      await dialog.getByLabel('集群', { exact: true }).click()
      await page.locator('.ant-select-item-option').filter({ hasText: '研发测试集群' }).click()
      await dialog.getByLabel('命名空间', { exact: true }).click()
      await page.locator('.ant-select-item-option').filter({ hasText: 'orders-b' }).click()
      await dialog.getByRole('button', { name: '下一步', exact: true }).click()
      await dialog.getByRole('row').filter({ hasText: 'orders' }).getByRole('checkbox').check()
      await dialog.getByRole('button', { name: '下一步', exact: true }).click()
      await dialog.getByLabel('应用', { exact: true }).click()
      await page.locator('.ant-select-item-option').filter({ hasText: '零售业务' }).click()
      await dialog.getByLabel('环境', { exact: true }).fill('test-b')
      await dialog.getByLabel('环境', { exact: true }).press('Enter')
      await dialog.getByRole('button', { name: '导入 1 个服务', exact: true }).click()
      await expect(dialog).toContainText('请选择已有服务')
      expect(writes).toEqual([])
      await dialog
        .getByLabel(source === 'helm' ? 'Helm Release / orders' : 'Deployment / orders', {
          exact: true,
        })
        .click()
      await page.locator('.ant-select-item-option').filter({ hasText: '订单 API' }).click()
      await dialog.getByText('服务归属', { exact: true }).click()
      await expect(dialog.getByText('请选择已有服务', { exact: true })).toHaveCount(0)
      await expect(dialog.getByRole('radio', { name: '托管发布', exact: true })).toBeDisabled()
      await expect(dialog).toContainText('保留已有服务的构建、模板和控制权配置')
      await page.screenshot({
        path: testInfo.outputPath(`mapping-${source}-${width}.png`),
        fullPage: true,
      })
      expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(
        true,
      )
      await dialog.getByRole('button', { name: '导入 1 个服务', exact: true }).click()
      await expect(page.locator('body')).toContainText(
        source === 'helm' ? '当前 Core 不支持明确服务映射' : '源资源已变化，请重新预览',
      )
      expect(writes).toHaveLength(1)
      expect(writes[0].path).toBe(
        source === 'helm'
          ? '/application-environments/helm-release-imports/mapped'
          : '/application-environments/imports/mapped',
      )
      expect(writes[0].payload).toMatchObject({
        namespace: 'orders-b',
        applicationKey: 'retail',
        environmentKey: 'test-b',
        ownershipMode: 'observe_only',
        ...(source === 'helm'
          ? { releases: [{ releaseName: 'orders', serviceId: 'order-api' }] }
          : {
              workloads: [
                {
                  workloadName: 'orders',
                  serviceId: 'order-api',
                  expectedUid: 'orders-uid',
                  expectedSpecDigest: 'a'.repeat(64),
                },
              ],
            }),
      })
      verifyBrowserEvidence(evidence)
    })
  }
}
