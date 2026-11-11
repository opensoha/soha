import { test, expect } from '@playwright/test'
import { baselineResponse } from '../fixtures/baseline-data'
import { installMocks, verifyBrowserEvidence } from '../fixtures/mock-app'

const longName = 'monitoring-kube-state-metrics-with-a-long-resource-name-75cf9cdc89-p8ftp'

for (const theme of ['light', 'dark']) {
  test(`table hints: only truncated text shows a tooltip (${theme})`, async ({ context, page }) => {
    await context.addInitScript(() =>
      localStorage.setItem(
        'soha-scope',
        JSON.stringify({ state: { clusterId: 'cluster-a', namespace: 'monitoring' }, version: 0 }),
      ),
    )
    const evidence = await installMocks(context, page, (url, method) => {
      if (url.pathname === '/api/v1/access/permission-snapshot') {
        const result = baselineResponse(url.pathname)!
        if (!result.data || !('permissionKeys' in result.data))
          throw new Error('Missing permission fixture')
        result.data.permissionKeys!.push('platform.pods.view', 'platform.pods.logs')
        result.data.visibleMenuIds!.push('workloads-pods')
        result.data.visibleMenus!.push({
          id: 'workloads-pods',
          parentId: 'workloads',
          path: '/workloads/pods',
          labelZh: 'Pods',
        })
        return { body: result }
      }
      if (url.pathname === '/api/v1/clusters/capabilities') {
        return {
          body: {
            data: [
              {
                key: 'pod.logs',
                direct: { status: 'available' },
                agent: { status: 'available' },
                requiredScopes: [],
                requiresApproval: false,
                riskLevel: 'read',
              },
            ],
          },
        }
      }
      if (url.pathname === '/api/v1/clusters/cluster-a/workloads/pods' && method === 'GET') {
        return {
          body: {
            data: ['short-pod', longName].map((name) => ({
              name,
              namespace: 'monitoring',
              phase: 'Running',
              nodeName: 'test-node',
              readyContainers: '1/1',
              restarts: 0,
              ageSeconds: 60,
              allowedActions: ['get', 'logs'],
            })),
          },
        }
      }
      if (url.pathname === '/api/v1/auth/stream-ticket' && method === 'POST') {
        return {
          status: 503,
          body: { error: { code: 'unavailable', message: 'Synthetic polling mode' } },
        }
      }
      return undefined
    })
    await page.goto('/workloads/pods')
    const shortText = page
      .locator('.soha-pod-table-name-link')
      .filter({ hasText: 'short-pod' })
      .locator('.ant-typography')
    const longText = page
      .locator('.soha-pod-table-name-link')
      .filter({ hasText: longName })
      .locator('.ant-typography')
    await expect(shortText).toBeVisible()
    if (theme === 'dark') await page.getByLabel('切换到深色模式').click()

    await shortText.hover()
    await page.waitForTimeout(250)
    await expect(page.getByRole('tooltip')).toHaveCount(0)
    await longText.hover()
    await expect(page.getByRole('tooltip')).toHaveText(longName)
    await page.setViewportSize({ width: 1280, height: 900 })
    await expect(page.getByRole('tooltip')).toHaveText(longName)
    await shortText.hover()
    await expect(page.getByRole('tooltip')).toHaveCount(0)

    const header = page.getByRole('columnheader', { name: 'Pod', exact: true })
    await header.hover()
    await page.waitForTimeout(250)
    await expect(page.getByRole('tooltip')).toHaveCount(0)
    await expect(header).not.toHaveAttribute('title')
    await header.click()
    await expect(header).toHaveAttribute('aria-sort', 'descending')
    await header.focus()
    await header.press('Enter')
    await expect(header).not.toHaveAttribute('aria-sort')

    const action = page.getByRole('button', { name: '更多操作：short-pod', exact: true })
    await action.hover()
    await page.waitForTimeout(250)
    await expect(page.getByRole('tooltip')).toHaveCount(0)
    await expect(action).not.toHaveAttribute('title')
    await action.focus()
    await expect(action).toBeFocused()
    await expect(page.getByRole('tooltip')).toHaveCount(0)
    await expect(page.getByRole('menuitem', { name: /^打开日志会话(?:\s|$)/ })).toHaveCount(0)
    await action.click()
    const logItem = page.getByRole('menuitem', { name: /^打开日志会话(?:\s|$)/ })
    await expect(logItem).toBeVisible()
    await expect(logItem).toBeEnabled()
    await expect(logItem).toBeFocused()
    await logItem.press('Escape')
    await expect(logItem).toBeHidden()
    await expect(action).toBeFocused()
    verifyBrowserEvidence(evidence)
  })
}
