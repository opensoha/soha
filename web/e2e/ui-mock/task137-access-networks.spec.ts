import { expect, test } from '@playwright/test'
import { installMocks, verifyBrowserEvidence } from '../fixtures/mock-app'

test('authorizes an unsaved access network through the site preview', async ({ context, page }) => {
  const siteId = 'site-hq'
  const root = '/api/v1/network-access'
  const time = {
    createdAt: '2026-11-21T00:00:00Z',
    updatedAt: '2026-11-21T00:00:00Z',
  }
  const evidence = await installMocks(context, page, (url, method, request) => {
    const path = url.pathname
    if (path === '/api/v1/access/permission-snapshot') {
      return {
        body: {
          data: {
            permissionKeys: [
              'workbench.security.view',
              'network_access.sites.view',
              'network_access.sites.update',
            ],
            visibleMenuIds: ['network-access', 'network-access-settings'],
            visibleMenus: [
              { id: 'network-access', path: '/network-access', labelZh: '网络访问' },
              {
                id: 'network-access-settings',
                path: '/network-access/settings',
                labelZh: '入网配置',
              },
            ],
          },
        },
      }
    }
    if (path === `${root}/sites`)
      return { body: { items: [{ ...time, id: siteId, name: '总部', status: 'active' }] } }
    if (path === `${root}/nas-bindings` && url.searchParams.get('siteId') === siteId) {
      return {
        body: {
          items: [
            {
              ...time,
              id: 'nas-hq',
              siteId,
              name: '总部无线控制器',
              accessMedium: 'wifi',
              status: 'active',
            },
          ],
        },
      }
    }
    if (
      path === `${root}/access-networks` &&
      method === 'GET' &&
      url.searchParams.get('siteId') === siteId
    )
      return { body: { items: [] } }
    if (path === `${root}/access-networks/preview` && method === 'POST') {
      expect(request.postDataJSON()).toMatchObject({
        siteId,
        userId: 'staff-member',
        deviceId: 'laptop-01',
        authenticationMethod: 'eap_tls',
        draft: {
          siteId,
          name: '办公 Wi-Fi',
          ssid: 'Office',
          nasBindingIds: ['nas-hq'],
          enabled: false,
          autoConnect: false,
          rules: [{ effect: 'allow', subjects: { users: [], teams: [], tags: [] } }],
        },
      })
      return {
        body: {
          items: [
            {
              accessNetworkId: 'draft',
              name: '办公 Wi-Fi',
              allowed: true,
              reasonCode: 'access_network_allowed',
              matchedRuleIndex: 1,
            },
          ],
        },
      }
    }
  })
  await page.goto('/network-access/settings?tab=ssids')
  await expect(page.getByText('请选择站点以查看配置。')).toBeVisible()
  const sitePicker = page.getByRole('combobox', { name: /^站点[ ：:]*$/ })
  await sitePicker.press('Enter')
  await expect(sitePicker).toHaveAttribute('aria-expanded', 'true')
  await expect(page.getByText('总部', { exact: true })).toBeVisible()
  await sitePicker.press('Enter')
  await expect(page).toHaveURL(/siteId=site-hq/)
  await expect(page.getByText('此站点暂无接入网络。')).toBeVisible()
  await page.getByRole('button', { name: '新增接入网络' }).click()
  await page
    .getByRole('dialog')
    .getByLabel(/^名称[ ：:]*$/)
    .fill('办公 Wi-Fi')
  await page
    .getByRole('dialog')
    .getByLabel(/^SSID[ ：:]*$/)
    .fill('Office')
  const nasPicker = page.getByRole('combobox', { name: /^(?:\*\s*)?站点 NAS[ ：:]*$/ })
  await nasPicker.press('Enter')
  await expect(nasPicker).toHaveAttribute('aria-expanded', 'true')
  await expect(page.getByText('总部无线控制器', { exact: true })).toBeVisible()
  await nasPicker.press('Enter')
  await page
    .getByRole('dialog')
    .getByLabel(/^名称[ ：:]*$/)
    .click()
  await page.getByRole('button', { name: '添加规则' }).click()
  await page.getByRole('button', { name: '预览草稿' }).click()

  const preview = page.getByRole('dialog', { name: '预览草稿授权' })
  await expect(preview.getByText('模拟启用后的规则，草稿保持未保存。')).toBeVisible()
  await preview.getByRole('combobox', { name: '用户' }).fill('staff-member')
  await preview.getByRole('combobox', { name: '终端' }).fill('laptop-01')
  await preview.getByRole('button', { name: '运行预览' }).click()
  await expect(preview.getByText('命中允许规则 · 规则 1')).toBeVisible()
  await expect(preview.getByRole('cell', { name: '100' })).toBeVisible()
  await page.screenshot({ path: test.info().outputPath('access-network-preview.png') })
  await preview
    .locator('.ant-modal-footer')
    .getByRole('button', { name: /^关\s*闭$/ })
    .click()
  await expect(page.getByRole('button', { name: /^保\s*存$/ })).toBeVisible()
  verifyBrowserEvidence(evidence)
})
