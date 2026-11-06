import { test, expect } from '@playwright/test'
import { installMocks, verifyBrowserEvidence } from '../fixtures/mock-app'

for (const width of [1440, 390])
  test(`task53 admission preview uses enrolled targets and current versions ${width}px`, async ({
    page,
    context,
  }) => {
    const requests: unknown[] = []
    const version = `sha256:${'a'.repeat(64)}`
    const evidence = await installMocks(context, page, (url, method, request) => {
      if (url.pathname === '/api/v1/access/permission-snapshot')
        return {
          body: {
            data: {
              permissionKeys: ['workbench.security.view', 'network_access.policy.view'],
              visibleMenuIds: ['network-access', 'network-access-policy'],
              visibleMenus: [
                { id: 'network-access', path: '/network-access', labelZh: '网络访问' },
                {
                  id: 'network-access-policy',
                  parentId: 'network-access',
                  path: '/network-access/policy',
                  labelZh: '访问策略',
                },
              ],
            },
          },
        }
      if (url.pathname === '/api/v1/network-access/policies') return { body: { data: [] } }
      if (url.pathname === '/api/v1/network-access/policy/snapshot')
        return {
          body: {
            data: {
              policyVersion: 7,
              policyCount: 0,
              protectedResourceCount: 0,
              contentHash: version,
              publishedAt: '2026-10-15T05:00:00Z',
            },
          },
        }
      if (url.pathname === '/api/v1/network-access/admission/preview' && method === 'POST') {
        requests.push(request.postDataJSON())
        if (requests.length > 1)
          return {
            status: 503,
            body: { error: { code: 'unavailable', message: 'Admission preview unavailable' } },
          }
        return {
          body: {
            data: {
              decision: 'deny',
              path: 'deny',
              networkProfile: 'deny',
              policyVersion: 7,
              attributeVersion: version,
              evaluatedAt: '2026-10-15T05:00:00Z',
              protected: false,
              networkLeaseRequired: false,
              resourceLeaseRequired: false,
              reasons: ['no_matching_allow_policy'],
            },
          },
        }
      }
      return undefined
    })
    await page.setViewportSize({ width, height: 1000 })
    await page.goto('/network-access/policy')
    await page.getByRole('tab', { name: '准入预览', exact: true }).click()
    const preview = page.getByRole('tabpanel', { name: '准入预览', exact: true })
    await preview.getByLabel('终端', { exact: true }).fill('device-one')
    await preview.getByLabel('接入网络', { exact: true }).fill('nas-binding-one')
    await preview.getByRole('button', { name: /预览准入$/ }).click()
    await expect(preview.getByText('当前策略拒绝接入', { exact: true })).toBeVisible()
    await expect(preview.getByText(version, { exact: true })).toBeVisible()
    expect(requests).toEqual([{ deviceId: 'device-one', nasBindingId: 'nas-binding-one' }])
    expect(
      evidence.requests.some((path) => path.startsWith('/api/v1/network-access/devices')),
    ).toBe(false)
    expect(evidence.requests.some((path) => path.includes('/nas-bindings'))).toBe(false)
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true)
    await page.screenshot({ path: `test-results/task53-admission-${width}.png` })
    await preview.getByLabel('终端', { exact: true }).fill('device-two')
    await expect(preview.getByText(version, { exact: true })).toHaveCount(0)
    expect(requests).toHaveLength(1)
    await expect(preview.getByRole('button', { name: /预览准入$/ })).not.toHaveClass(
      /ant-btn-loading/,
    )
    await preview.getByRole('button', { name: /预览准入$/ }).click()
    await expect(preview.getByText('准入预览失败', { exact: true })).toBeVisible()
    await expect(preview.getByText('当前策略拒绝接入', { exact: true })).toHaveCount(0)
    verifyBrowserEvidence(evidence)
  })
