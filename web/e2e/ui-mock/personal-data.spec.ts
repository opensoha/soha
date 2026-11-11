import { test, expect } from '@playwright/test'
import { createHash } from 'node:crypto'
import { readFile } from 'node:fs/promises'
import type { PersonalEnvironment } from '@opensoha/contracts/gen/ts/sohaapi'
import { installMocks, verifyBrowserEvidence } from '../fixtures/mock-app'

for (const width of [1440, 390])
  for (const theme of ['light', 'dark'] as const) {
    test(`personal data confirmation: ${theme} ${width}px`, async ({ page, context }) => {
      test.setTimeout(45000)
      await page.setViewportSize({ width, height: 1000 })
      await context.addInitScript(
        (theme) =>
          localStorage.setItem(
            'soha-prefs',
            JSON.stringify({ version: 0, state: { themeMode: theme } }),
          ),
        theme,
      )
      const session = {
        id: 'main-data',
        title: '主聊天',
        updatedAt: '2026-10-08T00:00:00Z',
        metadata: {
          conversationKind: 'personal_main',
          mode: 'general',
          agentProviderId: 'internal',
        },
      }
      let environment: PersonalEnvironment = {
        assistantId: 'assistant-data',
        state: 'sleeping',
        version: 3,
        dataInitialized: true,
      }
      const deletions: unknown[] = [],
        actions: unknown[] = []
      const evidence = await installMocks(context, page, (url, method, request) => {
        const path = url.pathname
        if (path === '/api/v1/copilot/assistant/access')
          return { body: { data: { userId: 'user-1', status: 'approved', version: 1 } } }
        if (path === '/api/v1/access/permission-snapshot')
          return {
            body: {
              data: {
                permissionKeys: [
                  'workbench.ai.view',
                  'observe.ai.chat',
                  'observe.ai.view',
                  'observe.monitoring.view',
                  'ai.gateway.invoke',
                ],
                visibleMenuIds: ['ai-workbench-chat'],
                visibleMenus: [
                  { id: 'ai-workbench-chat', path: '/ai-workbench/chat', labelZh: '对话与分析' },
                ],
              },
            },
          }
        if (path === '/api/v1/copilot/assistant/environment') {
          if (method === 'DELETE') {
            deletions.push(request.postDataJSON())
            environment = {
              ...environment,
              state: deletions.length === 1 ? 'deletion_unknown' : 'deleted',
              version: environment.version + 1,
            }
            if (deletions.length === 1)
              return {
                status: 503,
                body: { error: { code: 'service_unavailable', message: '删除回执未确认' } },
              }
          }
          if (method === 'POST') {
            actions.push(request.postDataJSON())
            environment = { ...environment, version: environment.version + 1 }
          }
          return { body: { data: environment } }
        }
        if (path === '/api/v1/copilot/assistant')
          return {
            body: {
              data: {
                id: environment.assistantId,
                name: 'Soha',
                mainSessionId: session.id,
                createdAt: session.updatedAt,
                updatedAt: session.updatedAt,
              },
            },
          }
        if (
          path === '/api/v1/copilot/sessions/main' ||
          path === `/api/v1/copilot/sessions/${session.id}`
        )
          return { body: { data: session } }
        if (path === '/api/v1/copilot/assistant/sessions') return { body: { data: [session] } }
        if (
          [
            '/api/v1/copilot/assistant/tasks',
            '/api/v1/copilot/assistant/artifacts',
            '/api/v1/copilot/agent-runs',
            `/api/v1/copilot/sessions/${session.id}/messages`,
          ].includes(path)
        )
          return { body: { data: [] } }
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
                    name: 'Soha',
                    enabled: true,
                    default: true,
                    supportsAsync: false,
                  },
                ],
              },
            },
          }
        return undefined
      })
      const archive = Buffer.from('personal archive browser fixture\0')
      let archiveHash = '0'.repeat(64)
      const exports: unknown[] = []
      await page.route('**/api/v1/copilot/assistant/environment/export', async (route) => {
        exports.push(route.request().postDataJSON())
        await route.fulfill({
          status: 200,
          contentType: 'application/x-tar',
          headers: {
            'Content-Length': String(archive.length),
            'X-Assistant-ID': environment.assistantId,
            'X-Content-SHA256': archiveHash,
          },
          body: archive,
        })
      })
      await page.goto('/heya')
      await expect(page.locator('html')).toHaveAttribute('data-theme-mode', theme)
      if (width < 992)
        await page
          .locator('#root .soha-ai-mobile-nav')
          .getByRole('button', { name: '我的', exact: true })
          .click()
      else await page.getByRole('button', { name: '助手身份', exact: true }).click()
      const downloads: string[] = []
      page.on('download', (download) => downloads.push(download.suggestedFilename()))
      const exportButton = page.getByRole('button', { name: '导出持久文件', exact: true })
      await exportButton.click()
      await expect(page.getByText('导出环境数据：操作请求失败', { exact: true })).toBeVisible()
      await expect(
        page.getByRole('alert').filter({ hasText: '导出环境数据：操作请求失败' }),
      ).toContainText('个人数据导出被截断或摘要不匹配')
      expect(downloads).toEqual([])
      archiveHash = createHash('sha256').update(archive).digest('hex')
      await expect(exportButton).toBeEnabled({ timeout: 15000 })
      const downloaded = page.waitForEvent('download')
      await exportButton.click()
      const download = await downloaded
      expect(download.suggestedFilename()).toBe('personal-data.tar')
      expect(await readFile((await download.path())!)).toEqual(archive)
      expect(exports).toEqual([
        { assistantId: environment.assistantId, environmentVersion: 3 },
        { assistantId: environment.assistantId, environmentVersion: 3 },
      ])
      const trigger = page.getByRole('button', { name: '删除持久数据', exact: true })
      await expect(trigger).toBeEnabled()
      await trigger.click()
      const dialog = page.getByRole('dialog', { name: '删除个人持久数据', exact: true })
      const name = dialog.getByRole('textbox', { name: '确认删除数据的助手全名', exact: true })
      const confirm = dialog.getByRole('button', { name: '确认删除数据', exact: true })
      await name.fill('Soh')
      await expect(confirm).toBeDisabled()
      await dialog.getByRole('button', { name: /^取\s*消$/ }).click()
      await expect(dialog).toBeHidden()
      expect(deletions).toEqual([])
      await trigger.click()
      await name.fill('Soha')
      await expect(confirm).toBeEnabled()
      environment = { ...environment, version: 4 }
      await expect(confirm).toBeDisabled({ timeout: 10000 })
      await dialog.getByRole('button', { name: /^取\s*消$/ }).click()
      await trigger.click()
      await name.fill('Soha')
      const bounds = await dialog.boundingBox()
      expect(bounds!.x).toBeGreaterThanOrEqual(0)
      expect(bounds!.x + bounds!.width).toBeLessThanOrEqual(width)
      await confirm.click()
      await expect(trigger).toBeDisabled()
      expect(deletions).toEqual([
        { assistantId: 'assistant-data', environmentVersion: 4, confirmationName: 'Soha' },
      ])
      await dialog.getByRole('button', { name: /^取\s*消$/ }).click()
      await page.getByRole('button', { name: '核对环境', exact: true }).click()
      await expect(trigger).toBeEnabled()
      expect(actions).toEqual([{ action: 'reconcile' }])
      expect(deletions).toHaveLength(1)
      await trigger.click()
      await name.fill('Soha')
      await confirm.click()
      await expect(dialog).toBeHidden()
      await expect(page.locator('.soha-ai-assistant-facts')).toContainText('持久数据已删除')
      expect(deletions).toEqual([
        { assistantId: 'assistant-data', environmentVersion: 4, confirmationName: 'Soha' },
        { assistantId: 'assistant-data', environmentVersion: 6, confirmationName: 'Soha' },
      ])
      await page.reload()
      expect(deletions).toHaveLength(2)
      verifyBrowserEvidence(evidence)
    })
  }
