import { test, expect, type Locator, type Page } from '@playwright/test'
import { installMocks, verifyBrowserEvidence } from '../fixtures/mock-app'
import { verifyGeometry } from '../fixtures/geometry'

// Popovers animate into position; verify the final hit target before the usual geometry check.
async function verifyMenuGeometry(page: Page, menu: Locator) {
  await menu.evaluate(async (element) => {
    // Antd schedules alignment and motion classes on the next frames.
    await new Promise<void>((resolve) =>
      requestAnimationFrame(() => requestAnimationFrame(() => resolve())),
    )
    const popup = element.closest('.ant-popover')
    if (popup)
      await Promise.all(
        popup.getAnimations({ subtree: true }).map((animation) => animation.finished),
      )
  })
  await expect
    .poll(() =>
      menu.evaluate((element) => {
        const rect = element.getBoundingClientRect()
        const hit = document.elementFromPoint(rect.x + rect.width / 2, rect.y + rect.height / 2)
        return !!hit && (hit === element || element.contains(hit))
      }),
    )
    .toBe(true)
  await verifyGeometry(page, menu)
}

const main = {
  id: 'main-chat',
  title: '主聊天',
  updatedAt: '2026-10-08T08:00:00Z',
  metadata: {
    conversationKind: 'personal_main',
    mode: 'general',
    agentProviderId: 'internal',
    scope: { alertId: 'context-alert' },
    analysisRunRefs: [
      { id: 'analysis-main', kind: 'root_cause', status: 'completed' },
      { id: 'trace-only', kind: 'trace', status: 'failed' },
    ],
  },
}
const side = {
  id: 'side-chat',
  title: '发布方案与一个很长的旁聊名称，用于检查列表截断与窄屏显示',
  updatedAt: '2026-10-07T07:00:00Z',
  metadata: {
    conversationKind: 'personal_side',
    mode: 'general',
    agentProviderId: 'internal',
    tags: ['soha:project:发布准备'],
  },
}
const report = {
  kind: 'report',
  runId: 'report-1',
  title: '本周运维摘要',
  summary: '报告已生成，可在右侧继续阅读。',
  dataSourceSnapshot: {
    format: 'markdown',
    content:
      '# 本周运维摘要\n\n测试数据：本周完成了资源清点与发布准备。\n\n## 下一步\n\n- 核对发布清单\n- 安排下一次巡检',
  },
}
const messages = [
  {
    id: 'main-user',
    sessionId: main.id,
    role: 'user',
    content: '帮我整理本周的工作，并生成一份简洁的摘要。',
    createdAt: '2026-10-08T07:55:00Z',
  },
  {
    id: 'main-reply',
    sessionId: main.id,
    role: 'assistant',
    content:
      '周报已经准备好了 📄\n\n你可以在右侧阅读成果，也可以继续在这里讨论。需要专注推进发布方案时，开启一个旁聊就好。',
    createdAt: '2026-10-08T08:00:00Z',
    metadata: {
      agentRunId: 'main-task',
      contextUsage: {
        measurement: 'reported',
        model: 'example-model',
        inputTokens: 8000,
        outputTokens: 200,
        totalTokens: 8200,
        contextWindowTokens: 128000,
      },
      contextEnvelope: {
        citations: [{ id: 'source-file', documentTitle: '发布清单.md' }],
        budgetUsage: { evidenceTokens: 6000, evidenceItems: 1 },
      },
      analysisArtifacts: [report],
      toolExecutions: [
        {
          id: 'tool-1',
          adapterId: 'native',
          toolName: 'artifact.preview',
          status: 'success',
          summary: '生成本周运维摘要',
          startedAt: '2026-10-08T07:59:00Z',
          completedAt: '2026-10-08T08:00:00Z',
        },
      ],
    },
  },
]
const completedTask = {
  id: 'main-task',
  rootCauseRunId: 'analysis-main',
  sessionId: main.id,
  providerKind: 'native',
  status: 'completed',
  updatedAt: main.updatedAt,
  input: {
    question:
      '整理本周运维工作，读取发布清单与资源状态，生成摘要并保留引用，供团队检查下一步安排。',
  },
  output: {
    usage: { totalTokens: 50000, taskTokenLimit: 100000, estimatedTokens: false, steps: 8 },
    summary:
      '只读核验已完成。维护窗口、负责人和适用范围的结果如下，已保留证据并逐项核对。\n\n- 维护窗口：每周二 22:00–23:00。\n- 引用来源 [citation:missing]。',
  },
  analysisArtifacts: [report],
  toolExecutions: messages[1].metadata!.toolExecutions,
}
const delegatedStep = {
  ...completedTask,
  id: 'checklist-step',
  parentRunId: completedTask.id,
  analysisArtifacts: [],
  toolExecutions: [
    {
      id: 'tool-checklist',
      adapterId: 'native',
      toolName: 'knowledge.search',
      summary: 'knowledge.search executed',
      input: { query: '发布维护窗口' },
      output: { result: '核对发布清单：每周二 22:00–23:00。' },
      status: 'success',
      startedAt: '2026-10-08T07:58:00Z',
      completedAt: '2026-10-08T07:59:00Z',
    },
  ],
}

for (const theme of ['light', 'dark'])
  for (const width of [1920, 1440, 1024, 390, 320]) {
    test(`Assistant: ${theme} ${width}px main, side and artifact`, async ({
      page,
      context,
    }, info) => {
      // This full assistant workflow includes identity, attachments, environment changes and reload.
      test.setTimeout(60_000)
      const requests: { path: string; body: unknown }[] = []
      let assistantName = 'Soha'
      let assistantAvatar = { avatarUrl: '', avatarPosterUrl: '', avatarMotion: 'auto' }
      const generationFails = true
      const avatarWrites: unknown[] = []
      const imageBase64 =
        'iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+XvlkAAAAASUVORK5CYII='
      let environmentState = 'unprovisioned'
      await page.clock.setSystemTime(new Date('2026-10-08T08:30:00Z'))
      page.on('request', (request) => {
        if (
          request.method() === 'POST' &&
          (request.url().includes('/copilot/sessions') ||
            request.url().includes('/copilot/assistant/sessions') ||
            request.url().includes('/copilot/assistant/environment'))
        )
          requests.push({ path: new URL(request.url()).pathname, body: request.postDataJSON() })
      })
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
      const evidence = await installMocks(context, page, (url, method, request) => {
        const path = url.pathname
        if (method === 'GET' && /^\/api\/v1\/copilot\/sessions\/[^/]+\/native-request$/.test(path))
          return {
            status: 404,
            body: { error: { code: 'not_found', message: 'No native request receipt' } },
          }
        if (path === '/api/v1/copilot/assistant/access')
          return { body: { data: { userId: 'user-1', status: 'approved', version: 1 } } }
        if (path === '/api/v1/copilot/assistant/feed')
          return {
            body: {
              data: {
                items: [],
                settings: { instructions: '简短动态', searchTool: '', autoUpdate: false },
                state: 'idle',
                searchTools: [],
              },
            },
          }
        if (path === '/api/v1/copilot/assistant/avatar' && method === 'PATCH') {
          assistantAvatar = request.postDataJSON()
          avatarWrites.push(assistantAvatar)
          return {
            body: {
              data: {
                id: 'assistant-1',
                name: assistantName,
                mainSessionId: main.id,
                ...assistantAvatar,
              },
            },
          }
        }
        if (path === '/api/v1/ai-gateway/llm/openai/v1/images/generations' && method === 'POST')
          return generationFails
            ? { status: 502, body: { code: 'image_error', message: '测试图像模型不可用' } }
            : { body: { data: [{ b64_json: imageBase64 }] } }
        if (path === '/api/v1/copilot/assistant') {
          if (method === 'PATCH') assistantName = request.postDataJSON().name
          return {
            body: {
              data: {
                id: 'assistant-1',
                name: assistantName,
                ...assistantAvatar,
                mainSessionId: main.id,
                createdAt: main.updatedAt,
                updatedAt: main.updatedAt,
              },
            },
          }
        }
        if (path === '/api/v1/copilot/assistant/environment/templates')
          return {
            body: {
              data: [
                {
                  id: 'personal',
                  name: '个人 Docker',
                  mode: 'restricted-write',
                  leaseSeconds: 600,
                  quota: {
                    diskBytes: 33554432,
                    memoryBytes: 67108864,
                    cpuUnits: 250,
                    maxProcesses: 16,
                    networkEgress: false,
                  },
                },
              ],
            },
          }
        if (path === '/api/v1/copilot/assistant/environment') {
          if (method === 'POST') {
            const input = request.postDataJSON()
            expect(input).toEqual(
              input.action === 'wake' && environmentState === 'unprovisioned'
                ? { action: 'wake', templateId: 'personal' }
                : { action: input.action },
            )
            environmentState = input.action === 'sleep' ? 'sleeping' : 'ready'
          }
          return {
            body: {
              data: {
                assistantId: 'assistant-1',
                state: environmentState,
                version: 0,
                dataInitialized: false,
                ...(environmentState === 'unprovisioned'
                  ? {}
                  : { templateId: 'personal', observedAt: '2026-10-08T08:29:00Z' }),
              },
            },
          }
        }
        if (path === '/api/v1/copilot/assistant/tasks')
          return {
            body: {
              data: [
                completedTask,
                delegatedStep,
                {
                  id: 'running-task',
                  sessionId: side.id,
                  providerKind: 'native',
                  status: 'running',
                  updatedAt: '2026-10-08T08:25:00Z',
                  input: { question: '检查发布前的资源状态' },
                },
                {
                  id: 'failed-task',
                  sessionId: side.id,
                  providerKind: 'hermes',
                  status: 'failed',
                  updatedAt: '2026-10-08T08:10:00Z',
                  input: { question: '检查服务健康状态' },
                  errorMessage: 'agent run timed out after 600 seconds without callback',
                },
                {
                  id: 'side-task',
                  sessionId: side.id,
                  providerKind: 'hermes',
                  status: 'completed',
                  input: { question: '旁聊中的发布检查' },
                  updatedAt: side.updatedAt,
                  analysisArtifacts: [report],
                },
              ],
            },
          }
        if (path === '/api/v1/copilot/assistant/artifacts')
          return {
            body: {
              data: [
                {
                  id: 'task:main-task:0',
                  taskId: completedTask.id,
                  sessionId: main.id,
                  messageId: 'main-reply',
                  createdAt: main.updatedAt,
                  artifact: report,
                },
                {
                  id: 'message:side-report:0',
                  sessionId: side.id,
                  messageId: 'side-report',
                  createdAt: side.updatedAt,
                  artifact: { ...report, title: '旁聊直接生成的报告' },
                },
              ],
            },
          }
        if (path === '/api/v1/access/permission-snapshot')
          return {
            body: {
              data: {
                permissionKeys: [
                  'workbench.ai.view',
                  'observe.ai.chat',
                  'ai.memory.view',
                  'ai.gateway.invoke',
                  'observe.ai.view',
                  'observe.ai.root-cause.run',
                  'observe.monitoring.view',
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
        if (path === '/api/v1/copilot/sessions/main' && method === 'POST')
          return { body: { data: main } }
        if (path === '/api/v1/copilot/assistant/sessions' && method === 'GET')
          return { body: { data: [side, main] } }
        if (path === '/api/v1/copilot/assistant/sessions' && method === 'POST')
          return { body: { data: { ...side, id: 'new-side', title: '发布方案' } } }
        if (path === '/api/v1/copilot/sessions/main-chat') return { body: { data: main } }
        if (path === '/api/v1/copilot/sessions/main-chat/messages')
          return { body: { data: messages } }
        if (path === '/api/v1/copilot/sessions/new-side')
          return { body: { data: { ...side, id: 'new-side', title: '发布方案' } } }
        if (path === '/api/v1/copilot/sessions/new-side/messages') return { body: { data: [] } }
        if (path === '/api/v1/copilot/agent-runs')
          return {
            body: {
              data:
                url.searchParams.get('sessionId') === main.id ? [completedTask, delegatedStep] : [],
            },
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
        return undefined
      })
      await page.setViewportSize({ width, height: 900 })
      await page.goto('/heya')
      await expect(page).toHaveURL(/session=main-chat/)
      const composer = page.getByPlaceholder('消息')
      await expect(composer).toBeVisible()
      await expect(page.locator('.soha-ai-main-chat-hint')).toHaveCount(0)
      if (width >= 992) {
        const mainEntry = page.getByRole('button', { name: '主要聊天', exact: true })
        await expect(mainEntry).toHaveText('主要聊天')
        const pane = page.locator('.soha-ai-workbench__detail.is-assistant')
        await expect(pane).toBeVisible()
        const panelBox = (await pane.boundingBox())!
        expect(Math.abs(panelBox.width - Math.min(360, Math.max(312, width * 0.28)))).toBeLessThan(
          1,
        )
        const [brand, actions, footer] = await Promise.all([
          page.locator('.soha-ai-rail-brand').boundingBox(),
          page.locator('.soha-ai-rail-actions').boundingBox(),
          page.locator('.soha-ai-rail-footer').boundingBox(),
        ])
        const center = actions!.y + actions!.height / 2
        expect(Math.abs(center - (brand!.y + brand!.height + footer!.y) / 2)).toBeLessThan(1)
        await verifyGeometry(page, page.locator('.soha-ai-rail-actions'))
      }
      await verifyGeometry(page, composer)
      const initialHeight = (await page.locator('.ant-sender').boundingBox())!.height
      expect(initialHeight).toBeLessThan(65)
      await composer.fill('菜单测试草稿')
      const modelTrigger = page.getByRole('button', { name: '模型与思考强度', exact: true })
      await expect(modelTrigger.locator('.soha-ai-workbench__model-effort')).toHaveText('自动')
      await modelTrigger.press('Enter')
      const modelMenu = page.locator('.soha-ai-workbench__model-menu')
      await expect(modelMenu).toBeVisible()
      await verifyMenuGeometry(page, modelMenu)
      await expect(
        modelMenu.getByRole('radiogroup', { name: '思考强度', exact: true }),
      ).toBeVisible()
      await page.getByRole('button', { name: '模型与思考强度', exact: true }).press('Escape')
      await expect(modelMenu).toBeHidden()
      await expect(composer).toBeFocused()
      await expect(composer).toHaveValue('菜单测试草稿')
      expect(requests.filter((item) => item.path.includes('/messages/stream'))).toHaveLength(0)
      await composer.fill('第一行\n第二行\n第三行')
      await expect
        .poll(async () => (await page.locator('.ant-sender').boundingBox())!.height)
        .toBeGreaterThan(initialHeight)
      const contextEntry = page.getByRole('button', { name: '上下文用量与背景来源', exact: true })
      await expect(contextEntry).toHaveAttribute('title', '上下文 8K / 128K')
      await expect(contextEntry).toHaveText('')
      await verifyGeometry(page, contextEntry)
      const ringBox = (await contextEntry.boundingBox())!
      const modelBox = (await modelTrigger.boundingBox())!
      const senderBox = (await page.locator('.ant-sender').boundingBox())!
      expect(ringBox.x + ringBox.width).toBeLessThanOrEqual(modelBox.x)
      expect(ringBox.y + ringBox.height).toBeLessThanOrEqual(senderBox.y + senderBox.height)
      if (width <= 390) expect(ringBox.width).toBeGreaterThanOrEqual(44)
      if (theme === 'light' && [1440, 390].includes(width)) {
        await page.screenshot({ path: `.tmp/quality/composer-ring-${width}.png`, fullPage: true })
      }
      await contextEntry.click()
      const contextPopover = page.locator('.soha-ai-workbench__context-popover:visible')
      await expect(contextPopover).toContainText('模型上游回报')
      await expect(contextPopover).toContainText('8K tokens')
      await expect(
        contextPopover.getByRole('button', { name: '查看上下文详情', exact: true }),
      ).toHaveCount(0)
      await expect(
        contextPopover.getByRole('button', { name: '查看原始告警详情', exact: true }),
      ).toBeVisible()
      await expect(page.getByRole('heading', { name: '会话上下文', exact: true })).toHaveCount(0)
      await contextPopover.getByRole('tab', { name: '最近实际使用' }).click()
      await expect(contextPopover).toContainText('发布清单.md')
      await expect(contextPopover).toContainText('估算 6K tokens')
      await expect(contextPopover).not.toContainText('50K')
      await verifyMenuGeometry(page, contextPopover)
      if (theme === 'light' && [1440, 390].includes(width)) {
        await page.screenshot({ path: `.tmp/quality/context-usage-${width}.png`, fullPage: true })
      }
      await contextPopover.getByRole('tab', { name: '最近实际使用' }).press('Escape')
      await expect(contextPopover).toBeHidden()
      await expect(composer).toBeFocused()
      const contextTools = page.locator('.ant-sender .soha-ai-workbench__composer-context')
      await page.getByRole('button', { name: '添加内容', exact: true }).press('Enter')
      const addMenu = page.locator('.soha-ai-workbench__add-menu')
      await verifyMenuGeometry(page, addMenu)
      await expect(addMenu.getByRole('button', { name: '个人记忆', exact: true })).toBeVisible()
      await expect(addMenu.getByRole('button', { name: '查看上下文', exact: true })).toHaveCount(0)
      await composer.fill('背景选择不改变草稿')
      await addMenu.getByRole('button', { name: '添加背景', exact: true }).click()
      const attachmentName = '发布计划及资源说明-用于检查窄屏下超长附件名称的换行与移除.txt'
      await page.getByLabel('添加文本附件', { exact: true }).setInputFiles({
        name: attachmentName,
        mimeType: 'text/plain',
        buffer: Buffer.from('这是本次对话引用的发布背景。'),
      })
      const attachment = contextTools.locator('.ant-tag').filter({ hasText: attachmentName })
      await expect(attachment).toContainText('已解析')
      await addMenu.getByRole('button', { name: '添加背景', exact: true }).click()
      await expect(page.getByLabel('添加文本附件', { exact: true })).toBeHidden()
      await expect(addMenu).toBeVisible()
      await page.getByRole('button', { name: '添加内容', exact: true }).click()
      await expect(addMenu).toBeHidden()
      await page.getByRole('button', { name: '添加内容', exact: true }).click()
      await expect(addMenu).toBeVisible()
      await addMenu.getByRole('button', { name: '添加背景', exact: true }).click()
      await expect(page.getByLabel('添加文本附件', { exact: true })).toBeVisible()
      await composer.click()
      await expect(page.getByLabel('添加文本附件', { exact: true })).toBeHidden()
      await expect(addMenu).toBeHidden()
      await verifyGeometry(page, contextTools)
      await verifyGeometry(page, attachment)
      if (theme === 'light' && width === 1440)
        await page.screenshot({ path: '.tmp/quality/composer-context-desktop.png', fullPage: true })
      await attachment.getByRole('button', { name: '关闭', exact: true }).click()
      await expect(attachment).toHaveCount(0)
      await expect(composer).toHaveValue('背景选择不改变草稿')
      await expect(page.locator('.soha-ai-workbench__toolbar')).toHaveCount(0)
      await expect(page.locator('.soha-ai-workbench__tools-header')).toHaveCount(0)
      await expect(page.locator('.soha-ai-workbench__back')).toHaveCount(0)
      expect((await page.locator('.soha-ai-workbench__workspace').boundingBox())?.y).toBeLessThan(8)
      await expect(page.locator('html')).toHaveAttribute('data-theme-mode', theme)
      await composer.fill('/analysis')
      await page.getByRole('menuitemcheckbox', { name: /专业分析任务 \/analysis/ }).click()
      const analysis = page.getByRole('dialog', { name: '专业分析任务', exact: true })
      await expect(analysis).toBeVisible()
      await verifyGeometry(page, analysis)
      await analysis.getByRole('combobox', { name: '任务能力', exact: true }).click()
      for (const label of ['根因分析', '性能分析', '链路分析', '巡检复盘']) {
        await expect(
          page.locator('.ant-select-item-option-content').getByText(label, { exact: true }),
        ).toBeVisible()
      }
      if (theme === 'light' && width === 1440)
        await page.screenshot({ path: '.tmp/quality/analysis-task-desktop.png', fullPage: true })
      await page.keyboard.press('Escape')
      await analysis.getByRole('button', { name: /^取\s*消$/ }).click()
      await expect(analysis).toBeHidden()
      await expect(page).toHaveURL(/\/heya\?session=main-chat$/)
      expect(requests.filter((request) => request.path.endsWith('/messages/stream'))).toHaveLength(
        0,
      )
      await composer.fill('主聊天的独立草稿')
      const verifyActivity = async () => {
        const profile = page.locator('.soha-ai-assistant-profile')
        await expect(profile).toContainText('已连接')
        await expect(profile).not.toContainText('AI Chat')
        const avatarBox = await profile.locator('.soha-ai-assistant-avatar').boundingBox()
        const nameBox = await profile
          .getByRole('heading', { name: 'Soha', exact: true })
          .boundingBox()
        expect(nameBox!.y - avatarBox!.y - avatarBox!.height).toBeGreaterThanOrEqual(0)
        expect(nameBox!.y - avatarBox!.y - avatarBox!.height).toBeLessThanOrEqual(8)
        if (width < 992) {
          for (const target of [
            profile.getByRole('button', { name: '编辑助手资料', exact: true }),
            page.locator('.soha-ai-workbench__mobile-detail.is-assistant .ant-drawer-close'),
          ]) {
            await expect(target).toBeInViewport({ ratio: 1 })
            await expect(target).toHaveCSS('width', '44px')
            await expect(target).toHaveCSS('height', '44px')
            const box = await target.boundingBox()
            // Chromium can report a 44px CSS target as 43.999984px.
            expect(Number(box!.width.toFixed(3))).toBeGreaterThanOrEqual(44)
            expect(Number(box!.height.toFixed(3))).toBeGreaterThanOrEqual(44)
            await verifyGeometry(page, target)
          }
        }
        await profile.getByRole('button', { name: '编辑助手资料', exact: true }).click()
        await page.getByRole('menuitem', { name: '编辑昵称' }).click()
        await expect(composer).toHaveValue('把你的名称改为 ')
        await expect(composer).toBeFocused()
        await expect(page.getByRole('textbox', { name: '助手名称', exact: true })).toHaveCount(0)
        expect(assistantName).toBe('Soha')
        await expect(page.getByRole('button', { name: '返回原草稿', exact: true })).toBeVisible()
        await page.getByRole('button', { name: '返回原草稿', exact: true }).click()
        await expect(composer).toHaveValue('主聊天的独立草稿')
        if (width < 992) await page.getByRole('button', { name: '任务', exact: true }).click()
        await profile.getByRole('button', { name: '编辑助手资料', exact: true }).click()
        await page.getByRole('menuitem', { name: '更改形象' }).click()
        await expect(composer).toHaveValue('把虚拟形象改成 ')
        await expect(page.getByRole('dialog', { name: '更改助手形象' })).toHaveCount(0)
        await expect(composer).toBeFocused()
        await page.getByRole('button', { name: '退出形象调整' }).click()
        await composer.fill('主聊天的独立草稿')
        if (width < 992) await page.getByRole('button', { name: '任务', exact: true }).click()
        if (theme === 'light' && [1440, 390].includes(width))
          await page.screenshot({
            path: `.tmp/quality/assistant-profile-${width}.png`,
            fullPage: true,
          })
        const feed = page.locator('.soha-ai-activity-feed')
        await expect(feed.getByText('正在同步任务…', { exact: true })).toBeHidden()
        await expect(feed.getByRole('heading', { name: /需要关注/ })).toHaveCount(0)
        await expect(feed.getByRole('heading', { name: '今天', exact: true })).toBeVisible()
        await expect(feed.getByRole('heading', { name: '昨天', exact: true })).toBeVisible()
        const task = feed.locator('[data-activity-id="task:main-task"]')
        await expect(task).toHaveCount(1)
        await expect(feed.locator('[data-activity-id="task:checklist-step"]')).toHaveCount(0)
        await expect(feed.locator('[data-activity-id="tool:tool-1"]')).toHaveCount(0)
        await expect(feed.locator('[data-activity-id="artifact:task:main-task:0"]')).toHaveCount(0)
        await expect(task.getByText(completedTask.input.question, { exact: true })).toHaveCount(0)
        await expect(feed.locator('details, .ant-tag')).toHaveCount(0)
        await expect(task.getByRole('button')).toHaveCount(1)
        await expect(feed.getByRole('button', { name: '取消任务', exact: true })).toHaveCount(0)
        await expect(task.locator('.soha-ai-activity-summary')).toHaveText(/^只读核验已完成。.*…$/)
        const icon = task.getByRole('button', { name: /^查看动态：/ })
        await icon.click()
        const detail = page
          .getByRole('dialog')
          .filter({ has: page.getByRole('navigation', { name: '动态步骤', exact: true }) })
        await expect(detail).toHaveCSS('opacity', '1')
        await expect(detail.getByRole('heading', { name: '执行结果', exact: true })).toBeVisible()
        await expect(detail.getByRole('heading', { name: '关联运行', exact: true })).toBeVisible()
        await expect(detail).toContainText('根因分析')
        await expect(
          feed.locator('[data-activity-id="analysis:main-chat:root_cause:analysis-main"]'),
        ).toHaveCount(0)
        await expect(detail).toContainText('累计消耗')
        await expect(detail).toContainText('50K tokens')
        await expect(detail.locator('.soha-ai-markdown')).toHaveCount(1)
        await expect(detail.locator('.soha-ai-markdown')).toContainText('每周二 22:00–23:00。')
        await expect(detail.locator('.soha-ai-markdown')).toContainText('[来源未验证]')
        await expect(detail).not.toContainText('[citation:missing]')
        await expect(detail.getByText(completedTask.input.question, { exact: true })).toBeHidden()
        if (theme === 'light' && [1440, 390].includes(width))
          await page.screenshot({
            path: `.tmp/quality/assistant-detail-overview-${width}.png`,
            fullPage: true,
            animations: 'disabled',
          })
        await detail
          .locator('summary')
          .filter({ hasText: /^任务请求$/ })
          .click()
        await expect(detail.getByText(completedTask.input.question, { exact: true })).toBeVisible()
        const steps = detail.getByRole('navigation', { name: '动态步骤', exact: true })
        const connectors = await steps.getByRole('button').evaluateAll((buttons) =>
          buttons.map((button) => {
            const style = getComputedStyle(button, '::before')
            return { content: style.content, width: style.width }
          }),
        )
        expect(connectors.length).toBeGreaterThan(2)
        expect(connectors.every((line) => line.content === '""' && line.width === '1px')).toBe(true)
        await detail
          .getByRole('navigation', { name: '动态步骤', exact: true })
          .getByRole('button', { name: '检索知识库', exact: true })
          .click()
        await expect(detail.getByRole('heading', { name: '检索知识库', exact: true })).toBeVisible()
        await expect(detail.locator('.soha-ai-activity-detail-content')).not.toContainText(
          'knowledge.search executed',
        )
        await expect(detail.locator('.soha-ai-activity-record[open] pre')).toContainText(
          '核对发布清单：每周二 22:00–23:00。',
        )
        await verifyGeometry(page, detail)
        if (theme === 'light' && [1440, 390].includes(width))
          await page.screenshot({
            path: `.tmp/quality/activity-detail-${width}.png`,
            fullPage: true,
            animations: 'disabled',
          })
        await detail
          .getByRole('navigation', { name: '动态步骤', exact: true })
          .getByRole('button', { name: report.title, exact: true })
          .click()
        await expect(detail.locator('.soha-ai-activity-detail-content')).toContainText(
          report.summary,
        )
        await expect(detail.getByRole('button', { name: '查看成果', exact: true })).toHaveCount(1)
        await page.keyboard.press('Escape')
        await expect(detail).toBeHidden()
        await expect(icon).toBeFocused()
        await icon.press('Enter')
        await expect(detail).toHaveCSS('opacity', '1')
        await expect(detail.getByText(completedTask.input.question, { exact: true })).toBeHidden()
        await detail
          .locator('summary')
          .filter({ hasText: /^任务请求$/ })
          .click()
        await expect(detail.getByText(completedTask.input.question, { exact: true })).toBeVisible()
        await detail.getByRole('button', { name: '关闭动态详情', exact: true }).click()
        await expect(detail).toBeHidden()
        await expect(icon).toBeFocused()
        await expect(feed.getByText('执行超时，请核对执行端状态', { exact: true })).toBeVisible()
        await verifyGeometry(page, icon)
        if (width < 992) await expect(icon).toHaveCSS('height', '44px')
        await task.locator('strong').click()
        if (theme === 'light' && [1440, 390].includes(width))
          await page.screenshot({
            path: `.tmp/quality/activity-minimal-${width}.png`,
            fullPage: true,
            animations: 'disabled',
          })
        const reference = feed.locator('[data-activity-id="analysis:main-chat:trace:trace-only"]')
        await expect(reference).toHaveCount(1)
        const referenceIcon = reference.getByRole('button', { name: /^查看动态：/ })
        await referenceIcon.click()
        await expect(detail).toContainText('链路分析')
        await expect(detail.getByRole('heading', { name: '关联运行', exact: true })).toBeVisible()
        await expect(detail.getByRole('button', { name: '取消任务', exact: true })).toHaveCount(0)
        await expect(detail.getByRole('button', { name: '查看对话', exact: true })).toBeVisible()
        await detail.getByRole('button', { name: '关闭动态详情', exact: true }).click()
        await expect(detail).toBeHidden()
        await expect(referenceIcon).toBeFocused()
      }
      await expect(page.locator('.soha-ai-workbench__session-slot')).not.toContainText('个旁聊')
      if (width < 992) {
        await page
          .locator('.soha-ai-mobile-nav')
          .getByRole('button', { name: '任务', exact: true })
          .click()
        await expect(page.getByText('旁聊中的发布检查', { exact: true })).toBeVisible()
        await verifyActivity()
        await page
          .locator('.ant-drawer-footer')
          .getByRole('button', { name: '成果', exact: true })
          .click()
        await expect(page.getByText('旁聊直接生成的报告', { exact: true })).toBeVisible()
        await page
          .locator('.soha-ai-assistant-event')
          .filter({ hasText: '旁聊直接生成的报告' })
          .getByRole('button', { name: '查看成果', exact: true })
          .click()
        await expect(
          page.getByRole('heading', { name: '本周运维摘要', exact: true, level: 1 }),
        ).toBeVisible()
        await page
          .getByRole('dialog', { name: '旁聊直接生成的报告', exact: true })
          .getByRole('button', { name: '关闭', exact: true })
          .click()
        await page
          .locator('#root .soha-ai-mobile-nav')
          .getByRole('button', { name: '我的', exact: true })
          .click()
        await expect(page.getByText('未开通', { exact: true })).toBeVisible()
        await expect(
          page
            .getByRole('dialog', { name: 'Soha 助手', exact: true })
            .getByRole('link', { name: 'AI 工作台', exact: true }),
        ).toHaveAttribute('href', '/ai-workbench/overview')
        await page
          .locator('.ant-drawer-footer')
          .getByRole('button', { name: '对话', exact: true })
          .click()
        await expect(page.getByRole('dialog', { name: 'Soha 助手', exact: true })).toBeHidden()
        await expect(composer).toHaveValue('主聊天的独立草稿')
      } else {
        const settings = page
          .locator('.soha-ai-rail-footer')
          .getByRole('button', { name: '会话设置', exact: true })
        const workbench = page
          .locator('.soha-ai-rail-footer')
          .getByRole('link', { name: 'AI 工作台', exact: true })
        await expect(page.getByRole('button', { name: '主要聊天', exact: true })).toHaveCSS(
          'height',
          '40px',
        )
        await expect(
          page
            .getByRole('navigation', { name: 'Heya 导航', exact: true })
            .locator('.soha-ai-animated-icon'),
        ).toHaveCount(9)
        const footerBox = (await page.locator('.soha-ai-rail-footer').boundingBox())!
        expect(900 - footerBox.y - footerBox.height).toBeCloseTo(20, 1)
        await expect(workbench).toHaveAttribute('href', '/ai-workbench/overview')
        await settings.click()
        await expect(page.getByRole('heading', { name: '会话设置', exact: true })).toBeVisible()
        await expect(page.getByText('对话类型', { exact: true })).toHaveCount(0)
        await page.getByRole('button', { name: '关闭会话设置', exact: true }).click()
        await expect(settings).toBeFocused()
        await expect(composer).toHaveValue('主聊天的独立草稿')
        const tasks = page
          .locator('.soha-ai-rail-actions')
          .getByRole('button', { name: '动态', exact: true })
        await tasks.click()
        await expect(tasks).toHaveAttribute('aria-pressed', 'true')
        await page.emulateMedia({ reducedMotion: 'no-preference' })
        await tasks.hover()
        const check = tasks.locator('.soha-ai-icon-check').first()
        await expect(tasks.locator('svg')).toHaveCSS('transform', 'none')
        await expect(check).toHaveCSS('animation-name', 'soha-icon-write')
        const drawing = await check.evaluate((element) => {
          const animation = element.getAnimations()[0]
          if (!animation) throw new Error('Task stroke animation is missing')
          animation.pause()
          animation.currentTime = 0
          const start = Number.parseFloat(getComputedStyle(element).strokeDashoffset)
          animation.currentTime = 600
          const end = Number.parseFloat(getComputedStyle(element).strokeDashoffset)
          animation.play()
          return { start, end }
        })
        expect(drawing).toEqual({ start: 1, end: 0 })
        await page.emulateMedia({ reducedMotion: 'reduce' })
        await expect(page.locator('html')).toHaveCSS('--soha-motion-duration-mid', '0s')
        await expect(composer).toHaveValue('主聊天的独立草稿')
        await expect(check).toHaveCSS('animation-name', 'none')
        await expect(check).toHaveCSS('opacity', '1')
        await page.emulateMedia({ reducedMotion: 'no-preference' })
        await expect(page.locator('html')).not.toHaveCSS('--soha-motion-duration-mid', '0s')
        await expect(composer).toHaveValue('主聊天的独立草稿')
        await page
          .locator('.soha-ai-rail-actions')
          .getByRole('button', { name: '返回对话', exact: true })
          .click()
        await page
          .locator('.soha-ai-rail-actions')
          .getByRole('button', { name: '成果', exact: true })
          .click()
        const activityTab = page.getByRole('radio', { name: /任务活动/ })
        await page.locator('.ant-segmented-item').filter({ has: activityTab }).click()
        await expect(activityTab).toBeChecked()
        await expect(page.getByText('旁聊中的发布检查', { exact: true })).toBeVisible()
        await verifyActivity()
        await page.getByRole('button', { name: '成果', exact: true }).click()
        await expect(page.getByText('旁聊直接生成的报告', { exact: true })).toBeVisible()
        await page
          .locator('.soha-ai-assistant-event')
          .filter({ hasText: '旁聊直接生成的报告' })
          .getByRole('button', { name: '查看成果', exact: true })
          .click()
        await expect(
          page.getByRole('heading', { name: '本周运维摘要', exact: true, level: 1 }),
        ).toBeVisible()
        await page.getByRole('button', { name: '关闭旁聊直接生成的报告', exact: true }).click()
        await expect(page).not.toHaveURL(/view=feed/)
        await expect(composer).toBeVisible()
      }
      await page.screenshot({ path: info.outputPath(`assistant-${theme}-${width}.png`) })
      if (width >= 992) {
        await page.getByRole('button', { name: '引用回复', exact: true }).click()
        await expect(contextTools.getByText(/回复 · 周报已经准备好了/)).toBeVisible()
        await page.getByRole('button', { name: '回复表情', exact: true }).click()
        await page.getByRole('button', { name: '标记 ❤️', exact: true }).click()
        await expect(page.getByRole('button', { name: '回复表情', exact: true })).toContainText(
          '❤️',
        )
        await expect(page.getByRole('button', { name: '标记 ❤️', exact: true })).not.toBeVisible()
        await composer.click()
        await page.getByRole('button', { name: '查看报告', exact: true }).click()
        await expect(
          page.getByRole('heading', { name: '本周运维摘要', exact: true, level: 1 }),
        ).toBeVisible()
        await verifyGeometry(page, composer)
        await expect(composer).toHaveValue('主聊天的独立草稿')
        await page.screenshot({ path: info.outputPath(`artifact-${theme}-${width}.png`) })
        await page.getByRole('button', { name: '关闭本周运维摘要', exact: true }).click()
      } else {
        await page.getByRole('button', { name: '展开话题', exact: true }).click()
        await expect(
          page
            .getByRole('dialog', { name: '会话记录', exact: true })
            .getByRole('button', { name: '主要聊天', exact: true }),
        ).toHaveCSS('height', '44px')
      }
      await expect(page.locator('.ant-conversations-creation')).toHaveCount(0)
      await expect(page.locator('[aria-label="个人项目集合"]')).toHaveCount(0)
      await expect(page.getByRole('button', { name: '新增旁聊', exact: true })).toHaveCount(1)
      await page.getByRole('button', { name: '搜索对话', exact: true }).last().click()
      const search = page.getByRole('dialog', { name: '搜索对话', exact: true })
      await expect(search.getByRole('textbox', { name: '搜索消息' })).toBeVisible()
      await search.getByRole('textbox', { name: '搜索消息' }).press('Escape')
      await expect(search).toBeHidden()
      await expect(page.locator('.ant-conversations-item')).toHaveCount(1)
      await page.getByRole('button', { name: `重命名 ${side.title}`, exact: true }).click()
      const rename = page.getByRole('dialog', { name: '重命名会话', exact: true })
      await expect(rename.getByRole('textbox', { name: '会话名称', exact: true })).toBeVisible()
      await expect(rename.getByRole('switch', { name: '置顶会话', exact: true })).toBeVisible()
      await expect(rename.getByLabel('个人项目名称', { exact: true })).toHaveCount(0)
      await expect(rename).toHaveCSS('opacity', '1')
      await rename.getByRole('textbox', { name: '会话名称', exact: true }).click()
      await verifyGeometry(page, rename)
      if (theme === 'light' && width === 1440)
        await page.screenshot({
          path: '.tmp/quality/session-classification-retired.png',
          fullPage: true,
          animations: 'disabled',
        })
      await rename.getByRole('button', { name: /^取\s*消$/ }).click()
      await expect(rename).toBeHidden()
      await expect(composer).toHaveValue('主聊天的独立草稿')
      await page.getByRole('button', { name: '新增旁聊', exact: true }).click()
      if (theme === 'dark') {
        await page.getByRole('textbox', { name: '旁聊名称' }).press('Escape')
        await expect(page.getByRole('textbox', { name: '旁聊名称' })).not.toBeVisible()
        await page.getByRole('button', { name: '新增旁聊', exact: true }).click()
      }
      await page.getByRole('textbox', { name: '旁聊名称' }).fill('发布方案')
      if (theme === 'dark') await page.getByRole('textbox', { name: '旁聊名称' }).press('Enter')
      else await page.getByRole('button', { name: '开启旁聊', exact: true }).click()
      await expect(page).toHaveURL(/session=new-side/)
      await expect(composer).toHaveValue('')
      await expect(page.locator('.soha-ai-workbench__conversation-scroll')).not.toContainText(
        '周报已经准备好了',
      )
      await composer.fill('旁聊的独立草稿')
      if (width < 992) await page.getByRole('button', { name: '展开话题', exact: true }).click()
      await page.getByRole('button', { name: '主要聊天', exact: true }).click()
      await expect(composer).toHaveValue('主聊天的独立草稿')
      expect(requests.some((request) => request.path.endsWith('/messages/stream'))).toBe(false)
      expect(
        requests.find((request) => request.path === '/api/v1/copilot/assistant/sessions')?.body,
      ).toMatchObject({ title: '发布方案', mode: 'general', scope: {} })
      if (width < 992)
        await page
          .locator('#root .soha-ai-mobile-nav')
          .getByRole('button', { name: '我的', exact: true })
          .click()
      else await page.getByRole('button', { name: '助手身份', exact: true }).click()
      const environment = page.locator('.soha-ai-assistant-facts dd').first()
      await expect(environment).toHaveText('未开通')
      await expect(page.getByRole('button', { name: '开通个人环境', exact: true })).toBeDisabled()
      await page.getByRole('combobox', { name: '个人环境模板', exact: true }).click()
      await page
        .locator('.ant-select-dropdown:visible .ant-select-item-option-content')
        .getByText('个人 Docker（Docker）', { exact: true })
        .click()
      await page.getByRole('button', { name: '开通个人环境', exact: true }).click()
      await expect(environment).toHaveText('就绪')
      await page.getByRole('button', { name: '休眠环境', exact: true }).click()
      await expect(environment).toHaveText('休眠')
      await page.getByRole('button', { name: '唤醒环境', exact: true }).click()
      await expect(environment).toHaveText('就绪')
      for (const [state, label] of [
        ['ready', '就绪'],
        ['sleeping', '休眠'],
        ['unknown', '状态未确认'],
      ]) {
        environmentState = state
        await page.getByRole('button', { name: '刷新环境状态', exact: true }).click()
        await expect(environment).toHaveText(label)
      }
      await expect(
        page.getByText('上次操作结果未确认，需核对原租约后再恢复。', { exact: true }),
      ).toBeVisible()
      await expect(page.getByRole('button', { name: '唤醒环境', exact: true })).toHaveCount(0)
      await page.getByRole('button', { name: '核对环境', exact: true }).click()
      await expect(environment).toHaveText('就绪')
      const operations = () =>
        requests.filter((request) => request.path === '/api/v1/copilot/assistant/environment')
      expect(operations().map((request) => request.body)).toEqual([
        { action: 'wake', templateId: 'personal' },
        { action: 'sleep' },
        { action: 'wake' },
        { action: 'reconcile' },
      ])
      environmentState = 'unknown'
      await page.reload()
      if (width < 992)
        await page
          .locator('#root .soha-ai-mobile-nav')
          .getByRole('button', { name: '我的', exact: true })
          .click()
      else await page.getByRole('button', { name: '助手身份', exact: true }).click()
      await expect(environment).toHaveText('状态未确认')
      expect(operations()).toHaveLength(4)
      await verifyGeometry(page, page.getByRole('button', { name: '刷新环境状态', exact: true }))
      if (width < 992)
        expect(
          (await page.getByRole('button', { name: '刷新环境状态', exact: true }).boundingBox())!
            .height,
        ).toBeGreaterThanOrEqual(44)
      const reconcile = page.getByRole('button', { name: '核对环境', exact: true })
      await reconcile.evaluate((element) => element.scrollIntoView({ block: 'center' }))
      await verifyGeometry(page, reconcile)
      if (width < 992) expect((await reconcile.boundingBox())!.height).toBeGreaterThanOrEqual(44)
      await page.screenshot({
        path: info.outputPath(`personal-environment-${theme}-${width}.png`),
        animations: 'disabled',
      })
      verifyBrowserEvidence(evidence)
    })
  }
