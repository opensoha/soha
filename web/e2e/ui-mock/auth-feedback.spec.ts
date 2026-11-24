import { test, expect } from '@playwright/test'
import { installMocks, verifyBrowserEvidence } from '../fixtures/mock-app'

const handoffURL = '/auth/browser-handoff/test-handoff'
const handoffAPI = '/api/v1/auth/browser-handoffs/test-handoff'

test('login options failure blocks the form until retry succeeds', async ({ context, page }) => {
  let optionsReads = 0
  const evidence = await installMocks(context, page, (url, method) => {
    if (method === 'POST' && url.pathname === '/api/v1/auth/refresh') {
      return { status: 401, body: { error: { message: '没有可恢复的浏览器会话' } } }
    }
    if (method === 'GET' && url.pathname === '/api/v1/auth/login-options') {
      if (++optionsReads === 1) {
        return { status: 503, body: { error: { message: '登录选项暂时不可用' } } }
      }
      return { body: { data: { verification: { sliderEnabled: false } } } }
    }
    if (method === 'GET' && url.pathname === '/api/v1/auth/providers')
      return { body: { items: [] } }
  })

  await page.goto('/login')
  const username = page.getByLabel('用户名')
  const submit = page.locator('button[type="submit"]')
  const retry = page.getByRole('button', { name: '重试读取登录选项' })
  await expect(retry).toBeVisible()
  await expect(username).toBeDisabled()
  await expect(submit).toBeDisabled()
  await retry.click()
  await expect(username).toBeEnabled()
  await expect(submit).toBeEnabled()
  await expect(retry).toBeHidden()
  expect(optionsReads).toBe(2)
  expect(evidence.requests).not.toContain('/api/v1/auth/login')
  verifyBrowserEvidence(evidence)
})

test('expired browser handoff explains the failed read and offers no completion', async ({
  context,
  page,
}) => {
  const evidence = await installMocks(context, page, (url, method) => {
    if (method === 'GET' && url.pathname === handoffAPI) {
      return { status: 410, body: { error: { message: '测试交接已过期' } } }
    }
  })

  await page.goto(handoffURL)
  const notice = page.getByRole('status').filter({ hasText: '测试交接已过期' })
  await notice.getByText('查看详情').click()
  await expect(notice.getByText('测试交接已过期', { exact: false })).toBeVisible()
  await expect(page.getByRole('button', { name: '继续登录' })).toHaveCount(0)
  expect(evidence.requests).not.toContain(`${handoffAPI}/complete`)
  verifyBrowserEvidence(evidence)
})

test('account conflict leaves browser handoff open after explicit confirmation', async ({
  context,
  page,
}) => {
  const evidence = await installMocks(context, page, (url, method) => {
    if (method === 'GET' && url.pathname === handoffAPI) {
      return {
        body: {
          data: {
            status: 'pending',
            expiresAt: '2099-01-01T00:01:00Z',
            accountName: '合成交接账户',
            application: { id: 'test-app', name: 'Soha Console' },
          },
        },
      }
    }
    if (method === 'POST' && url.pathname === `${handoffAPI}/complete`) {
      return { status: 409, body: { error: { message: '当前浏览器账号冲突' } } }
    }
  })

  await page.goto(handoffURL)
  const confirm = page.getByRole('button', { name: '继续登录' })
  await expect(page.getByText('合成交接账户')).toBeVisible()
  expect(evidence.requests).not.toContain(`${handoffAPI}/complete`)
  await confirm.click()
  await expect(page.getByRole('alert').filter({ hasText: '当前浏览器账号冲突' })).toBeVisible()
  await expect(page).toHaveURL(new RegExp(`${handoffURL}$`))
  await expect(confirm).toBeVisible()
  expect(evidence.requests.filter((request) => request === `${handoffAPI}/complete`)).toHaveLength(
    1,
  )
  verifyBrowserEvidence(evidence)
})

test('returning from a failed OIDC callback skips restoration of the previous browser user', async ({
  context,
  page,
}) => {
  await context.addInitScript(() => {
    localStorage.setItem(
      'soha-auth',
      JSON.stringify({ state: { user: { userId: 'previous-user' } }, version: 0 }),
    )
  })
  const evidence = await installMocks(context, page, (url, method) => {
    if (method !== 'GET') return
    if (url.pathname === '/api/v1/auth/login-options')
      return { body: { data: { verification: { sliderEnabled: false } } } }
    if (url.pathname === '/api/v1/auth/providers') return { body: { items: [] } }
  })

  await page.goto('/login/callback?error=access_denied')
  await page.getByRole('link', { name: '返回登录' }).click()
  await expect(page.getByLabel('用户名')).toBeEnabled()
  await expect(page).toHaveURL(/\/login$/)
  expect(evidence.requests).not.toContain('/api/v1/auth/refresh')
  verifyBrowserEvidence(evidence)
})
