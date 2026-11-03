import { test, expect } from '@playwright/test'
import { installMocks, verifyBrowserEvidence } from '../fixtures/mock-app'
import { verifyGeometry } from '../fixtures/geometry'

test('Heya approval sends the selected owner and current revision without creating an environment', async ({
  page,
  context,
}) => {
  let state = 'pending'
  let version = 1
  const decisions: unknown[] = []
  const evidence = await installMocks(context, page, (url, method, request) => {
    const path = url.pathname
    if (path === '/api/v1/access/permission-snapshot')
      return { body: { data: { permissionKeys: ['ai.environments.update'] } } }
    if (path === '/api/v1/copilot/assistant/access')
      return {
        body: { data: { userId: 'browser-regression-user', status: 'not_requested', version: 0 } },
      }
    if (path === '/api/v1/copilot/assistant/access/requests')
      return {
        body: {
          data: [
            { userId: 'applicant', status: state, version, requestedAt: '2026-10-09T08:00:00Z' },
          ],
        },
      }
    if (path === '/api/v1/copilot/assistant/access/requests/applicant' && method === 'PATCH') {
      const body = request.postDataJSON()
      decisions.push(body)
      state = body.status
      version++
      return { body: { data: { userId: 'applicant', status: state, version } } }
    }
    return undefined
  })
  await page.goto('/heya/access')
  await expect(page.getByText('applicant', { exact: true })).toBeVisible()
  await page.getByRole('button', { name: /^批\s*准$/ }).click()
  await page.getByRole('button', { name: /^确\s*定$/ }).click()
  await expect(page.getByRole('button', { name: /^撤\s*销$/ })).toBeVisible()
  expect(decisions).toEqual([{ status: 'approved', version: 1 }])
  expect(evidence.requests.some((path) => path.includes('/assistant/environment'))).toBe(false)
  verifyBrowserEvidence(evidence)
})

for (const width of [1440, 390])
  test(`Heya activation is separate from ordinary chat at ${width}px`, async ({
    page,
    context,
  }) => {
    let status = 'not_requested'
    const ordinary = {
      id: 'chat-main',
      title: '主要聊天',
      metadata: { conversationKind: 'main', agentProviderId: 'internal', mode: 'general' },
    }
    const evidence = await installMocks(context, page, (url, method) => {
      const path = url.pathname
      if (path === '/api/v1/access/permission-snapshot')
        return {
          body: {
            data: {
              permissionKeys: ['workbench.ai.view', 'observe.ai.chat'],
              visibleMenuIds: ['ai-workbench', 'ai-workbench-chat'],
              visibleMenus: [
                { id: 'ai-workbench', path: '/ai-workbench/overview', labelZh: 'AI 工作台' },
                {
                  id: 'ai-workbench-chat',
                  parentId: 'ai-workbench',
                  path: '/ai-workbench/chat',
                  labelZh: '对话',
                },
              ],
            },
          },
        }
      if (path === '/api/v1/copilot/assistant/access') {
        if (method === 'POST') status = 'pending'
        return {
          body: {
            data: {
              userId: 'browser-regression-user',
              status,
              version: status === 'pending' ? 1 : 0,
            },
          },
        }
      }
      if (path === '/api/v1/copilot/workbench/catalog')
        return {
          body: {
            data: {
              adapters: [],
              dataSources: [],
              analysisProfiles: [],
              skillsRegistry: [],
              modelOptions: [],
              agentProviders: [
                {
                  id: 'internal',
                  kind: 'internal',
                  name: 'Soha AI Chat',
                  enabled: true,
                  default: true,
                },
              ],
            },
          },
        }
      if (path === '/api/v1/copilot/sessions/main' || path === '/api/v1/copilot/sessions/chat-main')
        return { body: { data: ordinary } }
      if (path === '/api/v1/copilot/sessions') return { body: { data: [ordinary] } }
      if (
        path === '/api/v1/copilot/sessions/chat-main/messages' ||
        path === '/api/v1/copilot/agent-runs'
      )
        return { body: { data: [] } }
      if (path === '/api/v1/copilot/sessions/personal-main')
        return {
          body: {
            data: {
              ...ordinary,
              id: 'personal-main',
              metadata: { ...ordinary.metadata, conversationKind: 'personal_main' },
            },
          },
        }
      return undefined
    })
    await page.setViewportSize({ width, height: 900 })
    await page.goto('/ai-workbench/chat')
    await expect(page.getByPlaceholder('消息')).toBeVisible()
    expect(evidence.requests.some((path) => path.startsWith('/api/v1/copilot/assistant'))).toBe(
      false,
    )
    await page.evaluate(() => {
      const owner = JSON.stringify([
        new URL('/api/v1', location.origin).toString(),
        'browser-regression-user',
      ])
      localStorage.setItem(
        `soha.native-request.v1:${encodeURIComponent(owner)}/personal-main/wrong-scene-request`,
        '1',
      )
    })
    await page.goto('/ai-workbench/chat?session=personal-main')
    await expect(page.getByText('此会话属于另一个入口', { exact: true })).toBeVisible()
    expect(evidence.requests.some((path) => path.includes('/personal-main/messages'))).toBe(false)
    expect(evidence.requests.some((path) => path.includes('/personal-main/native-request'))).toBe(
      false,
    )
    await page.goto('/heya')
    await expect(page.getByText('Heya · 嘿呀', { exact: true })).toBeVisible()
    const request = page.getByRole('button', { name: '申请开通', exact: true })
    await verifyGeometry(page, request)
    await request.click()
    await expect(page.getByRole('button', { name: '申请已提交' })).toBeDisabled()
    await expect(page.getByText('等待批准', { exact: true })).toBeVisible()
    expect(
      evidence.requests.some(
        (path) =>
          path === '/api/v1/copilot/assistant' ||
          path === '/api/v1/copilot/assistant/sessions' ||
          path.includes('/assistant/environment'),
      ),
    ).toBe(false)
    await page.screenshot({
      path: test.info().outputPath(`heya-access-${width}.png`),
      fullPage: true,
    })
    verifyBrowserEvidence(evidence)
  })
