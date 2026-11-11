import { test, expect } from '@playwright/test'
import { installMocks, verifyBrowserEvidence } from '../fixtures/mock-app'
import { verifyGeometry } from '../fixtures/geometry'

for (const width of [1440, 390]) {
  test(`DSH processing and confirmed stop ${width}px`, async ({ page, context }, info) => {
    const session = {
      id: 'progress-chat',
      title: '主聊天',
      metadata: { conversationKind: 'main', mode: 'general', agentProviderId: 'internal' },
    }
    let status = 'running'
    let phase = 'model'
    let rejectCancel = true
    let cancellations = 0
    const run = () => ({
      id: 'progress-run',
      sessionId: session.id,
      providerId: 'dsh',
      providerKind: 'dsh',
      executionOwner: 'external',
      capabilityId: 'general',
      status,
      phase,
      queuedAt: '2026-10-08T08:00:00Z',
      input: { question: 'hi', requestId: 'progress-request', requestMessageId: 'request' },
    })
    const evidence = await installMocks(context, page, (url, method) => {
      const path = url.pathname
      if (path === '/api/v1/copilot/assistant')
        return {
          body: { data: { id: 'assistant-progress', name: 'Soha', mainSessionId: session.id } },
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
                { id: 'dsh', kind: 'dsh', name: 'Soha', enabled: true, default: true },
              ],
            },
          },
        }
      if (path === '/api/v1/access/permission-snapshot')
        return {
          body: {
            data: {
              permissionKeys: [
                'workbench.ai.view',
                'observe.ai.chat',
                'ai.gateway.invoke',
                'observe.ai.view',
              ],
              visibleMenuIds: ['ai-workbench', 'ai-workbench-chat'],
              visibleMenus: [
                { id: 'ai-workbench', path: '/ai-workbench/overview', labelZh: 'AI 工作台' },
                {
                  id: 'ai-workbench-chat',
                  parentId: 'ai-workbench',
                  path: '/ai-workbench/chat',
                  labelZh: '对话与分析',
                },
              ],
            },
          },
        }
      if (
        path === '/api/v1/copilot/sessions/main' ||
        path === `/api/v1/copilot/sessions/${session.id}`
      )
        return { body: { data: session } }
      if (path === '/api/v1/copilot/sessions') return { body: { data: [session] } }
      if (path === `/api/v1/copilot/sessions/${session.id}/messages`)
        return {
          body: {
            data: [
              {
                id: 'request',
                sessionId: session.id,
                role: 'user',
                content: 'hi',
                metadata: { agentRunId: 'progress-run', requestId: 'progress-request' },
                createdAt: '2026-10-08T08:00:00Z',
              },
              ...(status === 'canceled'
                ? [
                    {
                      id: 'final',
                      sessionId: session.id,
                      role: 'system',
                      content: '已取消本次任务。',
                      createdAt: '2026-10-08T08:00:01Z',
                      metadata: {
                        agentRunId: 'progress-run',
                        agentStatus: {
                          providerId: 'dsh',
                          providerKind: 'dsh',
                          status: 'cancelled',
                        },
                      },
                    },
                  ]
                : []),
            ],
          },
        }
      if (path === '/api/v1/copilot/agent-runs' || path === '/api/v1/copilot/assistant/tasks')
        return { body: { data: [run()] } }
      if (path === '/api/v1/copilot/agent-runs/progress-run/events')
        return { body: { data: { items: [], nextSequence: 0, hasMore: false } } }
      if (path === '/api/v1/copilot/agent-runs/progress-run/cancel' && method === 'POST') {
        cancellations++
        if (rejectCancel)
          return {
            status: 503,
            body: { error: { code: 'unavailable', message: '取消未确认，请重试' } },
          }
        phase = 'cancelling'
        return { status: 202, body: { data: run() } }
      }
      return undefined
    })
    await page.setViewportSize({ width, height: 900 })
    await page.goto(`/ai-workbench/chat?session=${session.id}`)
    const progress = page.locator('.soha-ai-message-progress')
    await expect(progress).toHaveText('正在思考')
    await expect(page.getByText('助手正在处理当前请求。')).toHaveCount(0)
    await expect(page.getByRole('button', { name: '复制回复', exact: true })).toHaveCount(0)
    await expect(progress.locator('.soha-ai-message-progress__dots > span')).toHaveCount(3)
    await verifyGeometry(page, progress)
    await page.emulateMedia({ reducedMotion: 'reduce' })
    await expect(progress.locator('.soha-ai-message-progress__dots > span').first()).toHaveCSS(
      'animation-name',
      'none',
    )
    await page.emulateMedia({ reducedMotion: 'no-preference' })
    await expect(progress.locator('.soha-ai-message-progress__dots > span').first()).toHaveCSS(
      'animation-name',
      'soha-ai-processing-dot',
    )
    await page.screenshot({ path: info.outputPath('processing.png') })
    await page.getByRole('button', { name: '停止回复', exact: true }).click()
    await page.getByRole('button', { name: '停止任务', exact: true }).click()
    await expect(page.getByText('取消未确认，请重试', { exact: true })).toBeVisible()
    await expect(page.getByRole('button', { name: '停止回复', exact: true })).toBeVisible()
    rejectCancel = false
    await page.getByRole('button', { name: '停止回复', exact: true }).click()
    await page.getByRole('button', { name: '停止任务', exact: true }).click()
    await expect(progress).toHaveText('正在停止')
    expect(cancellations).toBe(2)
    await expect(page.getByRole('button', { name: '发送消息', exact: true })).toHaveCount(0)
    status = 'canceled'
    phase = 'canceled'
    await expect(page.getByText('已取消本次任务。', { exact: true })).toBeVisible()
    await expect(progress).toHaveCount(0)
    await expect(page.getByRole('button', { name: '停止回复', exact: true })).toHaveCount(0)
    await expect(page.getByRole('button', { name: '添加内容', exact: true })).toBeEnabled()
    await expect(page.getByPlaceholder('消息', { exact: true })).toBeEditable()
    await verifyGeometry(page, page.locator('.soha-ai-workbench__composer'))
    await page.screenshot({ path: info.outputPath('stopped.png') })
    verifyBrowserEvidence(evidence)
  })
}
