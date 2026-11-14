import { test, expect } from '@playwright/test'
import { installMocks, verifyBrowserEvidence } from '../fixtures/mock-app'
import { verifyGeometry } from '../fixtures/geometry'

const session = {
  id: 'main-chat',
  title: '主聊天',
  updatedAt: '2026-10-09T00:00:00Z',
  metadata: { conversationKind: 'personal_main', mode: 'general', agentProviderId: 'internal' },
}
const history = Array.from({ length: 121 }, (_, index) => ({
  id: `message-${index}`,
  sessionId: session.id,
  role: index % 2 ? 'assistant' : 'user',
  content: index === 5 ? '飞牛 TV 的安装方法' : '历史消息 ' + index,
  metadata: {},
  createdAt: new Date(Date.UTC(2026, 0, 1, 0, index)).toISOString(),
}))
const article = {
  id: 'feed-1',
  title: '文件服务的近期进展',
  body: '根据来源整理的简短动态，方便继续讨论。',
  reason: '与你最近讨论的文件服务相关。',
  sources: [{ url: 'https://example.com/article', title: '文章原文', excerpt: '源资料' }],
  liked: false,
  createdAt: '2026-10-09T00:00:00Z',
}

for (const theme of ['light', 'dark'])
  for (const width of [1440, 1024, 390, 320]) {
    test(`Feed and message search: ${theme} ${width}px`, async ({ page, context }, info) => {
      await page.setViewportSize({ width, height: 1000 })
      await context.addInitScript(
        (value) =>
          localStorage.setItem(
            'soha-prefs',
            JSON.stringify({
              state: { themeMode: value, themeId: 'soha', localeCode: 'zh_CN' },
              version: 0,
            }),
          ),
        theme,
      )
      let liked = false
      let hidden = false
      let refreshes = 0
      let settings = {
        instructions: '简短的文件服务资讯',
        searchTool: 'web.search',
        autoUpdate: false,
      }
      const evidence = await installMocks(context, page, (url, method, request) => {
        const path = url.pathname
        if (path === '/api/v1/copilot/assistant/access')
          return { body: { data: { userId: 'user-1', status: 'approved', version: 1 } } }
        if (path === '/api/v1/access/permission-snapshot')
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
        if (path === '/api/v1/copilot/assistant')
          return { body: { data: { id: 'assistant-1', name: 'Soha', mainSessionId: session.id } } }
        if (
          path === '/api/v1/copilot/sessions/main' ||
          path === '/api/v1/copilot/sessions/main-chat'
        )
          return { body: { data: session } }
        if (path === '/api/v1/copilot/assistant/sessions') return { body: { data: [session] } }
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
                  { id: 'internal', kind: 'internal', name: 'Soha', enabled: true, default: true },
                ],
              },
            },
          }
        if (
          [
            '/api/v1/copilot/agent-runs',
            '/api/v1/copilot/assistant/tasks',
            '/api/v1/copilot/assistant/artifacts',
          ].includes(path)
        )
          return { body: { data: [] } }
        if (path === '/api/v1/copilot/sessions/main-chat/messages')
          return { body: { data: history.slice(0, 100) } }
        if (path === '/api/v1/copilot/sessions/main-chat/messages/window') {
          const around = url.searchParams.get('around')
          const before = url.searchParams.get('before')
          return {
            body: {
              data: around
                ? { messages: history.slice(0, 25), afterCursor: 'message-24' }
                : before
                  ? { messages: history.slice(0, 5), afterCursor: 'message-4' }
                  : { messages: history.slice(81), beforeCursor: 'message-81' },
            },
          }
        }
        if (path === '/api/v1/copilot/messages/search')
          return {
            body: {
              data: {
                items: [
                  {
                    sessionId: session.id,
                    sessionTitle: session.title,
                    messageId: 'message-5',
                    role: 'assistant',
                    snippet: '飞牛 TV 的安装方法',
                    createdAt: history[5].createdAt,
                  },
                ],
              },
            },
          }
        if (path === '/api/v1/copilot/assistant/feed/settings') {
          settings = request.postDataJSON()
          return {
            body: {
              data: {
                items: [],
                settings,
                state: 'idle',
                searchTools: [{ name: 'web.search', title: '已授权网页搜索' }],
              },
            },
          }
        }
        if (path === '/api/v1/copilot/assistant/feed/refresh') {
          refreshes++
          return {
            body: {
              data: {
                items: [],
                settings,
                state: 'queued',
                searchTools: [{ name: 'web.search', title: '已授权网页搜索' }],
              },
            },
          }
        }
        if (path === '/api/v1/copilot/assistant/feed/items/feed-1') {
          if (method === 'PATCH') {
            const input = request.postDataJSON()
            liked = input.liked ?? liked
            hidden = input.hidden ?? hidden
          }
          return { body: { data: { ...article, liked } } }
        }
        if (path === '/api/v1/copilot/assistant/feed')
          return {
            body: {
              data: {
                items: hidden ? [] : [{ ...article, liked }],
                settings,
                state: 'idle',
                searchTools: [{ name: 'web.search', title: '已授权网页搜索' }],
              },
            },
          }
        return undefined
      })
      await page.goto('/heya?session=main-chat')
      const composer = page.getByRole('textbox', { name: '消息', exact: true })
      await composer.fill('已有草稿')
      if (width < 1024) await page.getByRole('button', { name: '展开话题', exact: true }).click()
      await page.getByRole('button', { name: '搜索对话', exact: true }).last().click()
      const search = page.getByRole('dialog', { name: '搜索对话', exact: true })
      await expect(search.getByRole('textbox', { name: '搜索消息' })).toBeFocused()
      await search.getByRole('textbox', { name: '搜索消息' }).fill('飞牛tv')
      const hit = search.locator('.soha-message-search__result')
      await expect(hit).toContainText('飞牛 TV')
      await expect(hit.locator('mark').first()).toHaveText('飞牛')
      await verifyGeometry(page, search)
      await search.getByRole('textbox', { name: '搜索消息' }).press('Enter')
      await expect(page).toHaveURL(/message=message-5/)
      await expect(search).toBeHidden()
      if (width < 1024)
        await expect(page.getByRole('dialog', { name: '会话记录', exact: true })).toBeHidden()
      const target = page.locator('.soha-message-hit')
      await expect(target).toBeVisible()
      await expect(target.locator('mark').first()).toHaveText('飞牛')
      await verifyGeometry(page, target)
      await page.getByRole('button', { name: '回到最新消息', exact: true }).click()
      await expect(page).not.toHaveURL(/message=/)
      await expect(page.getByText('历史消息 120', { exact: true })).toBeVisible()
      await expect(composer).toHaveValue('已有草稿')
      await page
        .locator(width < 1024 ? '.soha-ai-mobile-nav' : '.soha-ai-rail-actions')
        .getByRole('button', { name: '动态', exact: true })
        .click()
      const feed = page.getByRole('region', { name: '个人动态' })
      await expect(feed.getByRole('heading', { name: article.title })).toBeVisible()
      await expect(feed.getByRole('link', { name: '文章原文' })).toHaveAttribute(
        'href',
        article.sources[0].url,
      )
      expect(refreshes).toBe(0)
      await verifyGeometry(page, feed.getByRole('button', { name: '动态偏好' }))
      await feed.getByRole('button', { name: '动态偏好' }).click()
      const preferences = page.getByRole('dialog', { name: '动态偏好' })
      await expect(preferences.getByRole('switch')).not.toBeChecked()
      await preferences
        .getByRole('textbox', { name: '希望看到什么' })
        .fill('只看技术进展，保持简短')
      await preferences.getByRole('button', { name: /保\s*存/ }).click()
      await expect(preferences).toBeHidden()
      expect(settings.instructions).toBe('只看技术进展，保持简短')
      await feed.getByRole('button', { name: '喜欢', exact: true }).click()
      await expect(feed.getByRole('button', { name: '取消喜欢', exact: true })).toBeVisible()
      await page.screenshot({ path: info.outputPath(`feed-${theme}-${width}.png`) })
      await feed.getByRole('button', { name: /讨论/ }).click()
      await expect(composer).toHaveValue('已有草稿')
      await expect(composer).toBeFocused()
      await expect(page.getByText(`讨论动态 · ${article.title}`, { exact: true })).toBeVisible()
      expect(evidence.requests.some((path) => path.includes('/messages/stream'))).toBe(false)
      await page
        .locator(width < 1024 ? '.soha-ai-mobile-nav' : '.soha-ai-rail-actions')
        .getByRole('button', { name: '动态', exact: true })
        .click()
      await page.reload()
      await expect(page.getByRole('button', { name: '取消喜欢', exact: true })).toBeVisible()
      await page.getByRole('button', { name: `动态选项：${article.title}` }).click()
      await page.getByRole('menuitem', { name: '不再显示' }).click()
      await expect(page.getByRole('heading', { name: article.title })).toBeHidden()
      verifyBrowserEvidence(evidence)
    })
  }
