import { test, expect } from '@playwright/test'
import { installMocks, verifyBrowserEvidence } from '../fixtures/mock-app'
const png =
  'iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+XvlkAAAAASUVORK5CYII='
for (const width of [1440, 390])
  test('Assistant appearance through chat ' + width + 'px', async ({ page, context }) => {
    await page.setViewportSize({ width, height: 1000 })
    let avatar = { avatarUrl: '', avatarPosterUrl: '', avatarMotion: 'auto' }
    const messages: unknown[] = []
    const requests: {
      content: string
      assistantTask?: {
        kind: string
        image?: typeof avatar
        useCurrentAvatar?: boolean
        messageId?: string
      }
    }[] = []
    const evidence = await installMocks(context, page, (url, method) => {
      if (url.pathname === '/api/v1/copilot/assistant/access')
        return { body: { data: { userId: 'user-1', status: 'approved', version: 1 } } }
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
      if (url.pathname === '/api/v1/copilot/sessions/main-chat')
        return {
          body: {
            data: {
              id: 'main-chat',
              title: '主聊天',
              metadata: {
                conversationKind: 'personal_main',
                mode: 'general',
                agentProviderId: 'internal',
              },
            },
          },
        }
      if (url.pathname === '/api/v1/copilot/assistant/sessions')
        return {
          body: {
            data: [
              {
                id: 'main-chat',
                title: '主聊天',
                metadata: {
                  conversationKind: 'personal_main',
                  mode: 'general',
                  agentProviderId: 'internal',
                },
              },
            ],
          },
        }
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
                { id: 'dsh', kind: 'dsh', name: 'Soha', enabled: true, default: true },
              ],
            },
          },
        }
      if (url.pathname === '/api/v1/copilot/agent-runs') return { body: { data: [] } }
      if (url.pathname === '/api/v1/copilot/assistant/environment')
        return {
          body: { data: { id: 'env-1', assistantId: 'assistant-1', state: 'unprovisioned' } },
        }
      if (url.pathname === '/api/v1/copilot/assistant/environment/templates')
        return { body: { data: [] } }
      if (url.pathname.endsWith('/copilot/assistant') && method === 'POST')
        return {
          body: {
            data: { id: 'assistant-1', name: 'Soha', mainSessionId: 'main-chat', ...avatar },
          },
        }
      if (url.pathname.endsWith('/copilot/sessions/main') && method === 'POST')
        return {
          body: {
            data: {
              id: 'main-chat',
              title: '主聊天',
              metadata: {
                conversationKind: 'personal_main',
                mode: 'general',
                agentProviderId: 'internal',
              },
            },
          },
        }
      if (url.pathname.endsWith('/copilot/sessions/main-chat/messages'))
        return { body: { data: messages } }
      if (
        url.pathname.endsWith('/copilot/assistant/tasks') ||
        url.pathname.endsWith('/copilot/assistant/artifacts')
      )
        return { body: { data: [] } }
      return undefined
    })
    await context.route('**/copilot/sessions/main-chat/messages/stream', async (route) => {
      const body = route.request().postDataJSON()
      requests.push(body)
      if (body.content === '回执丢失测试') {
        avatar = {
          avatarUrl: 'data:image/png;base64,' + png,
          avatarPosterUrl: 'data:image/png;base64,' + png,
          avatarMotion: 'auto',
        }
        await route.fulfill({
          status: 200,
          contentType: 'text/event-stream',
          body:
            'data: ' +
            JSON.stringify({
              type: 'error',
              code: 'stream_unavailable',
              message: '回执未送达，请重新核对资料',
              sessionId: 'main-chat',
              id: 'lost-receipt',
              sequence: 1,
              createdAt: new Date().toISOString(),
            }) +
            '\n\n',
        })
        return
      }
      const failure = body.content.includes('失败测试')
      const next =
        body.assistantTask?.kind === 'undo'
          ? { avatarUrl: '', avatarPosterUrl: '', avatarMotion: 'auto' }
          : body.assistantTask?.image || {
              avatarUrl: 'data:image/png;base64,' + png,
              avatarPosterUrl: 'data:image/png;base64,' + png,
              avatarMotion: 'auto',
            }
      if (!failure) avatar = next
      const id = 'reply-' + requests.length
      messages.push({
        id: 'user-' + requests.length,
        sessionId: 'main-chat',
        role: 'user',
        content: body.content,
        createdAt: new Date().toISOString(),
        metadata: body.assistantTask?.image ? { assistantImage: body.assistantTask.image } : {},
      })
      const metadata = failure
        ? {}
        : { assistantProfileUpdated: true, assistantImage: next, assistantProfileApplied: next }
      const content = failure
        ? '图片服务暂时无法完成这次调整，原头像已保留。'
        : '形象已更新，可以继续调整。'
      messages.push({
        id,
        sessionId: 'main-chat',
        role: 'assistant',
        content,
        metadata,
        createdAt: new Date().toISOString(),
      })
      const events = [
        { type: 'message.delta', id: id + '-delta', messageId: id, contentDelta: content },
        {
          type: 'tool.completed',
          id: id + '-tool',
          toolCall: {
            id: id + '-tool',
            adapterId: 'conversation',
            toolName: 'assistant.avatar',
            status: failure ? 'error' : 'success',
            summary: content,
          },
        },
        { type: 'message.done', id: id + '-done', messageId: id, content, metadata },
        { type: 'agent.status', id: id + '-status', status: 'succeeded' },
      ].map((event, index) => ({
        ...event,
        sessionId: 'main-chat',
        sequence: index + 1,
        createdAt: new Date().toISOString(),
      }))
      await route.fulfill({
        status: 200,
        contentType: 'text/event-stream',
        body: events.map((event) => 'data: ' + JSON.stringify(event) + '\n\n').join(''),
      })
    })
    await page.goto('/heya')
    const composer = page.getByPlaceholder('消息')
    await expect(composer).toBeVisible()
    if (width < 992) await page.getByRole('button', { name: '我的', exact: true }).click()
    await page.getByRole('button', { name: '编辑助手资料', exact: true }).click()
    await page.getByRole('menuitem', { name: /更改形象$/ }).click()
    await expect(composer).toBeFocused()
    await composer.fill('把虚拟形象改成白色小机器人')
    await composer.press('Enter')
    await expect(
      page.getByText('形象已更新，可以继续调整。', { exact: true }).first(),
    ).toBeVisible()
    await expect(page.locator('.soha-ai-assistant-chat-result img')).toHaveAttribute(
      'src',
      /^data:image/,
    )
    expect(requests[0].assistantTask?.kind).toBe('avatar')
    await page.getByRole('button', { name: '继续调整', exact: true }).click()
    await expect(composer).toHaveValue('调整你的头像：')
    await composer.fill('把头发改成蓝色，其他保持')
    await composer.press('Enter')
    await expect.poll(() => requests.length).toBe(2)
    expect(requests[1].assistantTask?.useCurrentAvatar).toBe(true)
    await expect(
      page.getByRole('button', { name: '撤销这次更改', exact: true }).last(),
    ).toBeEnabled()
    await page.getByRole('button', { name: '撤销这次更改', exact: true }).last().click()
    await expect.poll(() => requests.length).toBe(3)
    expect(requests[2].assistantTask).toEqual({ kind: 'undo', messageId: 'reply-2' })
    if (width < 992) await page.getByRole('button', { name: '我的', exact: true }).click()
    await page.getByRole('button', { name: '编辑助手资料', exact: true }).click()
    await page.getByRole('menuitem', { name: /更改形象$/ }).click()
    await expect(composer).toHaveValue('把虚拟形象改成 ')
    await page.getByRole('button', { name: '添加内容', exact: true }).click()
    const picker = page.waitForEvent('filechooser')
    await page.getByRole('button', { name: '添加图片', exact: true }).click()
    await (
      await picker
    ).setFiles({
      name: 'avatar.png',
      mimeType: 'image/png',
      buffer: Buffer.from(png, 'base64'),
    })
    await expect(page.getByAltText('待发送的形象图片')).toBeVisible()
    await expect(composer).toHaveValue('用这张图片作为你的头像')
    await composer.press('Enter')
    await expect.poll(() => requests.length).toBe(4)
    expect(requests[3].assistantTask?.image?.avatarUrl).toMatch(/^data:image/)
    await expect(page.getByRole('button', { name: '继续调整', exact: true }).last()).toBeEnabled()
    await page.getByRole('button', { name: '继续调整', exact: true }).last().click()
    await composer.fill('失败测试')
    await composer.press('Enter')
    await expect(
      page.getByText('图片服务暂时无法完成这次调整，原头像已保留。', { exact: true }).first(),
    ).toBeVisible()
    await expect(composer).toHaveValue('失败测试')
    expect(avatar.avatarUrl).toMatch(/^data:image/)
    await composer.fill('回执丢失测试')
    await composer.press('Enter')
    await expect(composer).toHaveValue('回执丢失测试')
    if (width < 992) await page.getByRole('button', { name: '我的', exact: true }).click()
    await expect(page.locator('.soha-ai-assistant-profile img')).toHaveAttribute(
      'src',
      avatar.avatarUrl,
    )
    await page.screenshot({
      path: test.info().outputPath('assistant-chat-appearance-' + width + '.png'),
      fullPage: true,
    })
    await verifyBrowserEvidence(evidence)
  })
