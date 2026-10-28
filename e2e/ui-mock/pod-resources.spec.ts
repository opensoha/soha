import { test, expect } from '@playwright/test'
import { baselineResponse } from '../fixtures/baseline-data'
import { installMocks, verifyBrowserEvidence } from '../fixtures/mock-app'

const pods = [
  {
    name: 'normal',
    cpu: '34m',
    memory: '146Mi',
    requests: { cpu: '100m', memory: '200Mi' },
    limits: { cpu: '500m', memory: '1Gi' },
  },
  {
    name: 'warning',
    cpu: '400m',
    memory: '768Mi',
    requests: { cpu: '200m', memory: '512Mi' },
    limits: { cpu: '500m', memory: '1Gi' },
  },
  {
    name: 'danger',
    cpu: '450m',
    memory: '942Mi',
    requests: { cpu: '200m', memory: '512Mi' },
    limits: { cpu: '500m', memory: '1Gi' },
  },
  {
    name: 'over-limit',
    cpu: '600m',
    memory: '1.2Gi',
    requests: { cpu: '200m', memory: '512Mi' },
    limits: { cpu: '500m', memory: '1Gi' },
  },
  { name: 'unlimited', cpu: '150m', memory: '35Mi', requests: { cpu: '100m', memory: '64Mi' } },
  { name: 'unbounded', cpu: '9m', memory: '64Ki' },
  {
    name: 'zero',
    cpu: '0',
    memory: '0',
    requests: { cpu: '100m', memory: '200Mi' },
    limits: { cpu: '500m', memory: '1Gi' },
  },
  {
    name: 'missing',
    requests: { cpu: '100m', memory: '200Mi' },
    limits: { cpu: '500m', memory: '1Gi' },
  },
]

for (const theme of ['light', 'dark']) {
  test(`pod resource values, thresholds and accessible details (${theme})`, async ({
    context,
    page,
  }) => {
    await context.addInitScript(() =>
      localStorage.setItem(
        'soha-scope',
        JSON.stringify({ state: { clusterId: 'cluster-a', namespace: 'monitoring' }, version: 0 }),
      ),
    )
    const evidence = await installMocks(context, page, (url, method) => {
      if (url.pathname === '/api/v1/access/permission-snapshot') {
        const body = baselineResponse(url.pathname)!
        if (!('permissionKeys' in body.data)) throw new Error('Missing permission fixture')
        body.data.permissionKeys!.push('platform.pods.view')
        body.data.visibleMenuIds!.push('workloads-pods')
        body.data.visibleMenus!.push({
          id: 'workloads-pods',
          parentId: 'workloads',
          path: '/workloads/pods',
          labelZh: 'Pods',
        })
        return { body }
      }
      if (url.pathname === '/api/v1/clusters/capabilities') return { body: { data: [] } }
      if (url.pathname === '/api/v1/clusters/cluster-a/workloads/pods' && method === 'GET') {
        return {
          body: {
            data: pods.map((pod) => ({
              ...pod,
              namespace: 'monitoring',
              phase: 'Running',
              readyContainers: '1/1',
              restarts: 0,
              ageSeconds: 60,
              allowedActions: ['get'],
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
    const bar = (pod: string, resource: 'CPU' | '内存') =>
      page.getByRole('button', { name: new RegExp(`^${pod} · ${resource} ·`) })
    await expect(bar('normal', 'CPU')).toHaveCount(1)
    if (theme === 'dark') await page.getByLabel('切换到深色模式').click()
    for (const [pod, tone] of [
      ['normal', 'success'],
      ['warning', 'warning'],
      ['danger', 'danger'],
      ['over-limit', 'danger'],
      ['unlimited', 'warning'],
      ['unbounded', 'unknown'],
      ['zero', 'success'],
      ['missing', 'unknown'],
    ]) {
      await expect(bar(pod, 'CPU')).toHaveClass(new RegExp(`is-${tone}`))
    }
    await expect(bar('zero', 'CPU').locator('.soha-pod-resource-value').first()).toHaveText('0m')
    await expect(bar('missing', 'CPU').locator('.soha-pod-resource-value').first()).toHaveText('—')
    await expect(bar('unbounded', '内存').locator('.soha-pod-resource-value').first()).toHaveText(
      '64Ki',
    )
    await expect(bar('unlimited', 'CPU').locator('.is-limit')).toHaveCount(0)
    await expect(bar('over-limit', 'CPU')).toHaveAccessibleName(/接近或达到限制/)

    const colors = await page
      .locator(
        '.soha-pod-resource-limit-cell.is-success, .soha-pod-resource-limit-cell.is-warning, .soha-pod-resource-limit-cell.is-danger',
      )
      .evaluateAll((bars) => [
        ...new Set(
          bars.map(
            (node) => getComputedStyle(node.querySelector('.ant-progress-track')!).backgroundColor,
          ),
        ),
      ])
    expect(colors).toHaveLength(3)
    for (const width of [1440, 900]) {
      await page.setViewportSize({ width, height: 1000 })
      if (width === 900) await page.getByRole('button', { name: '收起侧栏', exact: true }).click()
      const geometry = await page.locator('.soha-pod-resource-limit-cell').evaluateAll((cells) =>
        cells.map((node) => {
          const cell = node.getBoundingClientRect()
          const bar = node
            .querySelector('.soha-pod-resource-progress-wrap')!
            .getBoundingClientRect()
          const rail = node.querySelector('.ant-progress-rail')!.getBoundingClientRect()
          const range = document.createRange()
          range.selectNodeContents(node.querySelector('.soha-pod-resource-value')!)
          const label = range.getBoundingClientRect()
          const markers = Array.from(node.querySelectorAll('.soha-pod-resource-marker')).map(
            (marker) => marker.getBoundingClientRect(),
          )
          return (
            bar.width === 80 &&
            bar.height === 4 &&
            rail.height === bar.height &&
            rail.width === bar.width &&
            rail.top >= bar.top &&
            rail.bottom <= bar.bottom &&
            rail.left >= bar.left &&
            rail.right <= bar.right &&
            cell.height >= 24 &&
            label.left >= cell.left &&
            label.right <= cell.right &&
            label.top >= cell.top &&
            label.left >= bar.right + 7.9 &&
            label.bottom <= cell.bottom &&
            bar.bottom <= cell.bottom &&
            markers.every(
              (marker) =>
                marker.left >= bar.left &&
                marker.right <= bar.right &&
                marker.top >= bar.top &&
                marker.bottom <= bar.bottom,
            )
          )
        }),
      )
      expect(geometry.every(Boolean), `thin bars with readable values at ${width}px`).toBe(true)
      await bar('normal', 'CPU').hover()
      await expect(page.getByRole('tooltip')).toHaveText(
        'CPU · 使用 34m · 请求 100m · 限制 500m · 请求以内',
      )
    }
    await page.mouse.move(0, 0)
    await bar('warning', 'CPU').focus()
    await bar('warning', 'CPU').press('Tab')
    await expect(bar('warning', '内存')).toBeFocused()
    await expect(
      page.getByRole('tooltip', {
        name: '内存 · 使用 768Mi · 请求 512Mi · 限制 1Gi · 超过请求',
        exact: true,
      }),
    ).toBeVisible()
    await expect(page.getByRole('tooltip')).toHaveCount(1)
    verifyBrowserEvidence(evidence)
  })
}
