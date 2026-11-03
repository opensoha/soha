import { test, expect } from '@playwright/test'
import { readFileSync } from 'node:fs'
import { installMocks, verifyBrowserEvidence } from '../fixtures/mock-app'

for (const width of [1440, 390])
  test(`Ordinary image and AI reaction ${width}px`, async ({ page, context }) => {
    await page.setViewportSize({ width, height: 1000 })
    const messages: unknown[] = []
    const requests: Record<string, any>[] = []
    let allowReadReceipt = false
    const session = {
      id: 'main-chat',
      title: '主聊天',
      metadata: { conversationKind: 'main', mode: 'general', agentProviderId: 'internal' },
    }
    const evidence = await installMocks(context, page, (url) => {
      if (
        url.pathname.endsWith('/native-request') &&
        allowReadReceipt &&
        url.searchParams.get('requestId') === requests[0]?.requestId
      )
        return {
          body: {
            data: {
              id: 'image-failed-run',
              sessionId: 'main-chat',
              providerId: 'internal',
              providerKind: 'native',
              executionOwner: 'native',
              capabilityId: 'general',
              status: 'failed',
              input: { requestId: requests[0].requestId },
              errorMessage: '图片请求执行失败',
              createdAt: '2026-10-09T01:00:00Z',
            },
          },
        }
      if (url.pathname.endsWith('/native-request'))
        return {
          status: 404,
          body: { error: { code: 'not_found', message: 'No request receipt' } },
        }
      if (url.pathname === '/api/v1/access/permission-snapshot')
        return {
          body: {
            data: {
              permissionKeys: ['workbench.ai.view', 'observe.ai.chat', 'ai.gateway.invoke'],
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
        url.pathname === '/api/v1/copilot/sessions/main-chat' ||
        url.pathname === '/api/v1/copilot/sessions/main'
      )
        return { body: { data: session } }
      if (url.pathname === '/api/v1/copilot/sessions') return { body: { data: [session] } }
      if (url.pathname === '/api/v1/copilot/workbench/catalog')
        return {
          body: {
            data: {
              adapters: [],
              dataSources: [],
              analysisProfiles: [],
              skillsRegistry: [],
              modelOptions: [],
              agentProviders: [
                { id: 'internal', kind: 'internal', name: 'Soha', enabled: true, default: true },
              ],
            },
          },
        }
      if (url.pathname === '/api/v1/copilot/assistant')
        return { body: { data: { id: 'assistant', name: 'Soha', mainSessionId: 'main-chat' } } }
      if (url.pathname.endsWith('/messages')) return { body: { data: messages } }
      if (
        [
          '/api/v1/copilot/assistant/tasks',
          '/api/v1/copilot/assistant/artifacts',
          '/api/v1/copilot/agent-runs',
        ].includes(url.pathname)
      )
        return { body: { data: [] } }
      return undefined
    })
    await context.route('**/copilot/sessions/main-chat/messages/stream', async (route) => {
      const body = route.request().postDataJSON()
      requests.push(body)
      if (requests.length === 3) {
        await route.fulfill({ status: 503, body: 'unknown image request outcome' })
        return
      }
      if (requests.length === 1) {
        await route.fulfill({ status: 503, body: 'request outcome unknown' })
        return
      }
      const user = {
        id: 'user-image',
        sessionId: 'main-chat',
        role: 'user',
        content: body.content,
        metadata: { images: body.contextSelection.images },
        createdAt: '2026-10-09T01:00:00Z',
      }
      const reply = {
        id: 'reply-image',
        sessionId: 'main-chat',
        role: 'assistant',
        content: '这是红色与蓝色的色块。',
        metadata: { requestMessageId: user.id, assistantReaction: '👀' },
        createdAt: '2026-10-09T01:00:01Z',
      }
      messages.push(user, reply)
      await route.fulfill({
        contentType: 'text/event-stream',
        body: `data: ${JSON.stringify({ id: 'done', type: 'message.done', sequence: 1, sessionId: 'main-chat', messageId: reply.id, role: 'assistant', content: reply.content, metadata: reply.metadata })}\n\ndata: ${JSON.stringify({ id: 'status', type: 'agent.status', sequence: 2, sessionId: 'main-chat', status: 'succeeded' })}\n\n`,
      })
    })
    await page.goto('/ai-workbench/chat')
    const composer = page.getByPlaceholder('消息')
    await expect(composer).toBeVisible()
    await composer.fill('分析这张图片')
    await page.getByRole('button', { name: '添加内容', exact: true }).click()
    const picker = page.waitForEvent('filechooser')
    await page.getByRole('button', { name: '添加图片', exact: true }).click()
    await (
      await picker
    ).setFiles({
      name: 'diagram.png',
      mimeType: 'image/png',
      buffer: readFileSync(new URL('../fixtures/chat-image.png', import.meta.url)),
    })
    await expect(page.getByAltText('diagram.png')).toBeVisible()
    await expect(composer).toHaveValue('分析这张图片')
    await expect(page.getByText('形象参考图', { exact: true })).toHaveCount(0)
    await composer.press('Enter')
    await expect(composer).toHaveValue('分析这张图片')
    await expect(page.getByRole('button', { name: '移除图片 diagram.png' })).toBeVisible()
    await expect(page.getByText('任务提交结果未确认', { exact: false }).first()).toBeVisible()
    await composer.press('Enter')
    expect(requests).toHaveLength(1)
    allowReadReceipt = true
    await page.getByRole('button', { name: '重新读取', exact: true }).click()
    await expect(page.getByText('任务提交结果未确认', { exact: false })).toHaveCount(0)
    await composer.press('Enter')
    await expect(page.getByRole('img', { name: '助手回应 👀' })).toBeVisible()
    expect(requests[1].assistantTask).toBeUndefined()
    expect(requests[1].contextSelection.images[0].dataUrl).toMatch(/^data:image\/png;base64,/)
    await page.reload()
    await expect(page.getByRole('img', { name: '助手回应 👀' })).toBeVisible()
    const image = page.locator('.soha-ai-chat-image')
    await expect(image).toBeVisible()
    expect(
      await image.evaluate((node: HTMLImageElement) => node.naturalWidth / node.naturalHeight),
    ).toBe(2)
    await expect(composer).toHaveValue('')
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth + 1)).toBe(
      true,
    )
    await composer.fill('原草稿保持')
    await composer.evaluate(
      (node, base64) => {
        const bytes = Uint8Array.from(atob(base64), (character) => character.charCodeAt(0))
        const transfer = new DataTransfer()
        transfer.items.add(new File([bytes], 'pasted.png', { type: 'image/png' }))
        node.dispatchEvent(new ClipboardEvent('paste', { clipboardData: transfer, bubbles: true }))
      },
      readFileSync(new URL('../fixtures/chat-image.png', import.meta.url)).toString('base64'),
    )
    await expect(page.getByRole('button', { name: '移除图片 pasted.png' })).toBeVisible()
    await expect(composer).toHaveValue('原草稿保持')
    await expect(page.getByText('形象参考图', { exact: true })).toHaveCount(0)
    await composer.press('Enter')
    await expect(page.getByText('任务提交结果未确认', { exact: false }).first()).toBeVisible()
    await expect(page.getByRole('button', { name: '移除图片 pasted.png' })).toBeVisible()
    await expect(composer).toHaveValue('原草稿保持')
    await composer.press('Enter')
    expect(requests).toHaveLength(3)
    await page.screenshot({
      path: test.info().outputPath(`chat-image-reaction-${width}.png`),
      fullPage: true,
    })
    await verifyBrowserEvidence(evidence)
  })
