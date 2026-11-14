import { test as base, expect, type APIRequestContext } from '@playwright/test'
import { loadTarget } from './real-target'

export function catalogState(items: Record<string, unknown>[]) {
  return items
    .map((item) => {
      const stable = { ...item }
      delete stable.ageSeconds // elapsed wall time is not a Kubernetes mutation
      return stable
    })
    .sort((left, right) => String(left.uid).localeCompare(String(right.uid)))
}

export const test = base.extend<{
  real: { target: ReturnType<typeof loadTarget>; api: APIRequestContext; catalogPath: string }
}>({
  real: async ({ page, context, playwright }, provide) => {
    const target = loadTarget()
    if (target.scenario === 'task114' || process.env.SOHA_E2E_SCENARIO === 'task114')
      expect(process.env.SOHA_E2E_SCENARIO).toBe(target.scenario)
    const origin = new URL(target.baseURL).origin
    await context.route('**/*', (route) =>
      new URL(route.request().url()).origin === origin
        ? route.continue()
        : route.abort('blockedbyclient'),
    )
    await context.addInitScript(
      ({ clusterId, namespace }) =>
        localStorage.setItem(
          'soha-scope',
          JSON.stringify({ state: { clusterId, namespace }, version: 0 }),
        ),
      target,
    )
    await page.goto(target.baseURL + '/login')
    await page.getByPlaceholder('请输入用户名').fill(process.env[target.loginEnv]!)
    await page.getByPlaceholder('请输入密码').fill(process.env[target.passwordEnv]!)
    const slider = page.getByRole('slider')
    if (await slider.isVisible()) {
      await slider.press('End')
      await expect(slider).toHaveAttribute('aria-valuenow', '100')
    }
    const submit = page.getByRole('button', { name: '登录控制台', exact: true })
    await expect(submit).toBeEnabled()
    const loginResponse = page.waitForResponse(
      (response) =>
        new URL(response.url()).pathname === '/api/v1/auth/login' &&
        response.request().method() === 'POST',
    )
    await submit.click()
    const login = await loginResponse
    expect(login.ok()).toBe(true)
    const token = (await login.json()).data.tokens.accessToken
    const api = await playwright.request.newContext({
      baseURL: origin,
      extraHTTPHeaders: { Authorization: `Bearer ${token}` },
      maxRedirects: 0,
    })
    try {
      const permissions = await api.get('/api/v1/access/permission-snapshot')
      expect(permissions.ok()).toBe(true)
      const keys = (await permissions.json()).data.permissionKeys as string[]
      if (target.scenario === 'task114') {
        for (const permission of [
          'delivery.applications.view',
          'delivery.application-environments.view',
          'platform.pods.view',
        ])
          expect(keys).toContain(permission)
        for (const permission of [
          'platform.pods.exec',
          'delivery.application-services.create',
          'delivery.application-environments.update',
          'delivery.application-environments.approve',
          'delivery.workflows.trigger',
        ])
          expect(keys.includes(permission)).toBe(target.role === 'test-writer')
      } else {
        expect(keys.includes('platform.extensions.crds.delete')).toBe(target.role === 'test-writer')
      }
      const clusters = await api.get('/api/v1/clusters')
      expect(clusters.ok()).toBe(true)
      const cluster = (await clusters.json()).items.find(
        (item: { id: string }) => item.id === target.clusterId,
      )
      expect(cluster).toBeDefined()
      expect(cluster.connectionMode === 'agent').toBe(target.mode === 'e2e-agent')
      await provide({
        target,
        api,
        catalogPath: `/api/v1/clusters/${encodeURIComponent(target.clusterId)}/extensions/crds`,
      })
    } finally {
      await api.post('/api/v1/auth/logout')
      await api.dispose()
    }
  },
})
export { expect }
