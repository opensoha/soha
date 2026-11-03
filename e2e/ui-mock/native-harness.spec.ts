import { test, expect } from '@playwright/test'
import type { WorkbenchStreamEvent } from '@opensoha/contracts/gen/ts/sohaapi'
import { installMocks, verifyBrowserEvidence } from '../fixtures/mock-app'
import { verifyGeometry } from '../fixtures/geometry'

for (const width of [1440, 390]) {
  test(`Native professional task: ${width}px capability combination and explicit inputs`, async ({
    page,
    context,
  }) => {
    const session = {
      id: 'mixed-session',
      title: '主聊天',
      updatedAt: '2026-10-08T00:00:00Z',
      metadata: {
        conversationKind: 'main',
        mode: 'general',
        agentProviderId: 'internal',
        toolset: { enabledAdapterIds: ['metrics.v1', 'traces.v1'] },
      },
    }
    const requests: Array<Record<string, unknown>> = []
    const evidence = await installMocks(context, page, (url, method) => {
      const path = url.pathname
      if (method === 'GET' && path === `/api/v1/copilot/sessions/${session.id}/native-request`) {
        const request = requests.find(
          (item) => item.requestId === url.searchParams.get('requestId'),
        )
        return request
          ? {
              body: {
                data: {
                  id: `mixed-run-${request.requestId}`,
                  sessionId: session.id,
                  providerId: 'internal',
                  providerKind: 'native',
                  executionOwner: 'native',
                  capabilityId: 'general',
                  status: 'completed',
                  phase: 'completed',
                  input: { requestId: request.requestId },
                  createdAt: session.updatedAt,
                  completedAt: session.updatedAt,
                },
              },
            }
          : {
              status: 404,
              body: { error: { code: 'not_found', message: 'No matching request receipt' } },
            }
      }
      if (path === '/api/v1/access/permission-snapshot')
        return {
          body: {
            data: {
              permissionKeys: [
                'workbench.ai.view',
                'observe.ai.chat',
                'observe.ai.view',
                'observe.ai.root-cause.run',
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
      if (path === '/api/v1/copilot/assistant')
        return {
          body: {
            data: {
              id: 'assistant',
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
      if (path === '/api/v1/copilot/sessions') return { body: { data: [session] } }
      if (
        path === '/api/v1/copilot/assistant/tasks' ||
        path === '/api/v1/copilot/assistant/artifacts' ||
        path === '/api/v1/copilot/agent-runs' ||
        path === `/api/v1/copilot/sessions/${session.id}/messages`
      )
        return { body: { data: [] } }
      if (path === '/api/v1/observability/metrics/catalog')
        return {
          body: {
            data: [
              {
                key: 'latency_p95',
                label: 'P95 延迟',
                unit: 'ms',
                signal: 'metrics',
                available: true,
              },
            ],
          },
        }
      if (path === '/api/v1/copilot/inspection-runs')
        return {
          body: {
            data: [
              {
                id: 'inspection-done',
                taskId: 'task',
                status: 'completed',
                summary: '实际巡检记录',
                completedAt: session.updatedAt,
              },
              {
                id: 'inspection-running',
                taskId: 'task',
                status: 'running',
                summary: '正在执行的巡检',
              },
            ],
          },
        }
      if (path === '/api/v1/copilot/workbench/catalog')
        return {
          body: {
            data: {
              adapters: [
                { id: 'metrics.v1', name: 'Metrics', sourceKind: 'metrics' },
                { id: 'traces.v1', name: 'Traces', sourceKind: 'traces' },
              ],
              dataSources: [
                {
                  id: 'metrics-source',
                  name: 'Prometheus',
                  enabled: true,
                  sourceKind: 'metrics',
                  backendType: 'prometheus',
                  mcpAdapter: 'metrics.v1',
                },
                {
                  id: 'traces-source',
                  name: 'SkyWalking',
                  enabled: true,
                  sourceKind: 'traces',
                  backendType: 'skywalking',
                  mcpAdapter: 'traces.v1',
                },
              ],
              analysisProfiles: [
                { id: 'root-profile', name: '发布关联模板', enabled: true, mode: 'root_cause' },
              ],
              skillsRegistry: [],
              modelOptions: [],
              agentProviders: [
                { id: 'internal', kind: 'internal', name: 'Soha', enabled: true, default: true },
              ],
            },
          },
        }
      return undefined
    })
    await page.route(`**/api/v1/copilot/sessions/${session.id}/messages/stream`, async (route) => {
      requests.push(route.request().postDataJSON())
      await route.fulfill({
        contentType: 'text/event-stream',
        body: `data: ${JSON.stringify({ type: 'message.done', role: 'assistant', messageId: 'reply', content: '调查完成', metadata: {} })}\n\n`,
      })
    })
    await page.setViewportSize({ width, height: 900 })
    await page.goto('/ai-workbench/chat')
    await expect(page).toHaveURL(/session=mixed-session/)
    const composer = page.getByPlaceholder('消息', { exact: true })
    await composer.fill('/analysis')
    await page.getByRole('menuitemcheckbox', { name: /专业分析任务 \/analysis/ }).click()
    const dialog = page.getByRole('dialog', { name: '专业分析任务', exact: true })
    const choose = async (label: string, option: string | RegExp) => {
      await dialog.getByRole('combobox', { name: label, exact: true }).click()
      await page
        .locator('.ant-select-dropdown:visible .ant-select-item-option-content')
        .getByText(option, { exact: true })
        .click()
      await dialog.getByText('专业分析任务', { exact: true }).click()
      await dialog.getByPlaceholder('描述这轮分析要回答的问题', { exact: true }).click()
      await expect(page.locator('.ant-select-dropdown:visible')).toHaveCount(0)
    }
    await choose('任务能力', '性能分析')
    await choose('任务能力', '链路分析')
    await choose('任务能力', '巡检复盘')
    await choose('根因分析模板', '发布关联模板')
    await choose('指标数据源', 'Prometheus')
    await choose('性能指标', 'P95 延迟')
    await choose('链路数据源', 'SkyWalking')
    await dialog.getByRole('textbox', { name: 'Trace ID', exact: true }).fill('trace-selected')
    await expect(dialog.getByRole('spinbutton', { name: '链路最短耗时', exact: true })).toHaveValue(
      '0',
    )
    await choose('巡检执行记录', /实际巡检记录/)
    const start = dialog.getByRole('button', { name: '开始分析', exact: true })
    await expect(start).toBeEnabled()
    await verifyGeometry(page, dialog)
    await verifyGeometry(page, start)
    await page.screenshot({
      path: test.info().outputPath(`native-professional-${width}.png`),
      fullPage: true,
    })
    await start.click()
    await expect
      .poll(() => requests[0]?.taskIntent)
      .toEqual({
        capabilities: ['root_cause', 'performance', 'trace', 'inspection_review'],
        rootCauseProfileId: 'root-profile',
        performanceDataSourceId: 'metrics-source',
        metricKey: 'latency_p95',
        traceDataSourceId: 'traces-source',
        traceId: 'trace-selected',
        minDurationMs: 0,
        inspectionRunIds: ['inspection-done'],
      })
    await expect(dialog).toBeHidden()
    await expect
      .poll(() =>
        evidence.requests.some(
          (path) =>
            path ===
            `/api/v1/copilot/sessions/${session.id}/native-request?requestId=${requests[0]?.requestId}`,
        ),
      )
      .toBe(true)
    await composer.fill('继续普通对话')
    await composer.press('Enter')
    await expect.poll(() => requests[1]?.taskIntent).toEqual({ capabilities: [] })
    verifyBrowserEvidence(evidence)
  })

  test(`Native Harness: ${width}px cursor recovery, same-run resume, approval and cancel acknowledgement`, async ({
    page,
    context,
  }) => {
    const session = {
      id: 'native-session',
      title: '主聊天',
      updatedAt: '2026-10-08T00:00:00Z',
      metadata: { conversationKind: 'personal_main', mode: 'general', agentProviderId: 'internal' },
    }
    const run = {
      id: 'native-run',
      sessionId: session.id,
      providerId: 'internal',
      providerKind: 'native',
      executionOwner: 'native',
      capabilityId: 'general',
      status: 'running',
      phase: 'paused',
      stopReason: 'waiting_input',
      queuedAt: session.updatedAt,
      input: { question: '检查发布窗口' },
      output: {
        pendingQuestion: '请补充发布窗口。',
        approvalRequestIds: [] as string[],
        capabilityTaskId: '',
      },
    }
    const events: WorkbenchStreamEvent[] = [
      {
        id: 'event-1',
        type: 'message.delta',
        role: 'assistant',
        messageId: 'native-run:reply',
        contentDelta: '请补充发布窗口。',
        sequence: 1,
        createdAt: session.updatedAt,
        sessionId: session.id,
        runId: run.id,
      },
      {
        id: 'event-2',
        type: 'thinking.delta',
        textDelta: '等待输入',
        sequence: 2,
        createdAt: session.updatedAt,
        sessionId: session.id,
        runId: run.id,
      },
      {
        id: 'event-3',
        type: 'thinking.done',
        summary: '需要你的补充信息',
        collapsed: true,
        sequence: 3,
        createdAt: session.updatedAt,
        sessionId: session.id,
        runId: run.id,
      },
    ]
    const posts: Array<{ path: string; body: unknown }> = []
    page.on('request', (request) => {
      if (request.method() === 'POST' && request.url().includes('/copilot/')) {
        posts.push({ path: new URL(request.url()).pathname, body: request.postDataJSON() })
      }
    })
    const evidence = await installMocks(context, page, (url, method) => {
      const path = url.pathname
      if (path === '/api/v1/access/permission-snapshot')
        return {
          body: {
            data: {
              permissionKeys: [
                'workbench.ai.view',
                'observe.ai.chat',
                'ai.gateway.invoke',
                'ai.gateway.approvals.view',
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
      if (path === '/api/v1/copilot/assistant/access' && method === 'GET')
        return { body: { data: { userId: 'user-1', status: 'approved', version: 1 } } }
      if (path === '/api/v1/copilot/assistant')
        return {
          body: {
            data: {
              id: 'assistant',
              name: 'Soha',
              mainSessionId: session.id,
              createdAt: session.updatedAt,
              updatedAt: session.updatedAt,
            },
          },
        }
      if (path === '/api/v1/copilot/assistant/tasks' || path === '/api/v1/copilot/agent-runs')
        return { body: { data: [run] } }
      if (path === '/api/v1/copilot/assistant/artifacts') return { body: { data: [] } }
      if (
        path === '/api/v1/copilot/sessions/main' ||
        path === `/api/v1/copilot/sessions/${session.id}`
      )
        return { body: { data: session } }
      if (method === 'GET' && path === '/api/v1/copilot/assistant/sessions')
        return { body: { data: [session] } }
      if (path === '/api/v1/copilot/sessions') return { body: { data: [session] } }
      if (path === `/api/v1/copilot/sessions/${session.id}/messages`)
        return {
          body: {
            data: [
              {
                id: 'native-run:request',
                sessionId: session.id,
                role: 'user',
                content: '检查发布窗口',
                createdAt: session.updatedAt,
                metadata: { agentRunId: run.id },
              },
              ...(run.status === 'canceled'
                ? [
                    {
                      id: 'native-run:reply',
                      sessionId: session.id,
                      role: 'system',
                      content: '已取消本次任务。',
                      createdAt: session.updatedAt,
                      metadata: {
                        agentRunId: run.id,
                        agentStatus: {
                          providerId: 'internal',
                          providerKind: 'native',
                          status: 'canceled',
                        },
                      },
                    },
                  ]
                : []),
            ],
          },
        }
      if (path === `/api/v1/copilot/agent-runs/${run.id}/events`) {
        const after = Number(url.searchParams.get('afterSequence') ?? 0)
        const pending = events.filter((event) => event.sequence > after)
        const items = pending.slice(0, 2)
        return {
          body: {
            data: {
              items,
              nextSequence: items.at(-1)?.sequence ?? after,
              hasMore: pending.length > 2,
              resyncRequired: false,
            },
          },
        }
      }
      if (path === `/api/v1/copilot/agent-runs/${run.id}/resume` && method === 'POST') {
        run.phase = 'waiting_approval'
        run.stopReason = ''
        run.output.approvalRequestIds = ['native-approval']
        run.output.capabilityTaskId = 'native-capability-task'
        events.push({
          id: 'event-4',
          type: 'message.delta',
          role: 'assistant',
          messageId: 'native-run:model:2',
          contentDelta: '已收到发布窗口，继续检查。',
          sequence: 4,
          createdAt: session.updatedAt,
          sessionId: session.id,
          runId: run.id,
        })
        return { body: { data: run } }
      }
      if (path === '/api/v1/ai-gateway/approval-requests')
        return {
          body: {
            data: [
              {
                id: 'native-approval',
                status: 'pending',
                toolName: 'docker.projects.deploy.trigger',
                toolInput: { projectId: 'approved-project', idempotencyKey: 'native:operation' },
                summary: '等待批准已保存的变更',
              },
            ],
          },
        }
      if (path === `/api/v1/copilot/agent-runs/${run.id}/cancel` && method === 'POST') {
        run.phase = 'cancelling'
        return { body: { data: run } }
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
                { id: 'internal', kind: 'internal', name: 'Soha', enabled: true, default: true },
              ],
            },
          },
        }
      return undefined
    })
    await page.setViewportSize({ width, height: 900 })
    await page.goto('/heya?session=native-session')
    await expect(page).toHaveURL(/session=native-session/)
    await expect(page.locator('body')).toContainText('请补充发布窗口。')
    await expect
      .poll(() =>
        evidence.requests.includes('/api/v1/copilot/agent-runs/native-run/events?afterSequence=3'),
      )
      .toBe(true)
    if (width < 992)
      await page
        .locator('#root .soha-ai-mobile-nav')
        .getByRole('button', { name: '任务', exact: true })
        .click()
    await page
      .locator('[data-activity-id="task:native-run"]')
      .getByRole('button', { name: /^查看动态：/ })
      .click()
    const input = page.getByRole('textbox', { name: '任务 native-run 的补充信息', exact: true })
    await expect(input).toBeVisible()
    await input.click()
    await input.fill('今晚 22:00 至 23:00')
    await page.getByRole('button', { name: '关闭动态详情', exact: true }).click()
    const activityIcon = page
      .locator('[data-activity-id="task:native-run"]')
      .getByRole('button', { name: /^查看动态：/ })
    await expect(activityIcon).toBeFocused()
    await activityIcon.click()
    await expect(input).toHaveValue('今晚 22:00 至 23:00')
    await expect(page.getByRole('button', { name: '补充并继续', exact: true })).toBeEnabled()
    await verifyGeometry(page, input)
    await page.screenshot({
      path: test.info().outputPath(`native-harness-waiting-${width}.png`),
      fullPage: true,
    })
    await page.getByRole('button', { name: '补充并继续', exact: true }).click()
    await expect
      .poll(() => posts.find((request) => request.path.endsWith('/native-run/resume'))?.body)
      .toEqual({ input: '今晚 22:00 至 23:00' })
    await expect(input).toHaveCount(0)
    await expect(page.locator('body')).toContainText('等待变更审批')
    await page.getByRole('button', { name: '查看批准', exact: true }).click()
    await expect(page.locator('.soha-ai-activity-dialog')).toHaveCount(0)
    const approval = page.locator('.soha-ai-approval')
    await expect(approval).toContainText('approved-project')
    await expect(approval).toContainText('审批结果会继续推进原任务')
    await expect(approval).not.toContainText('助手本轮已结束')
    await expect(approval.getByRole('button', { name: '批准', exact: true })).toHaveCount(0)
    await expect
      .poll(() =>
        evidence.requests.some(
          (request) =>
            request.startsWith('/api/v1/ai-gateway/approval-requests?') &&
            new URLSearchParams(request.split('?')[1]).get('id') === 'native-approval',
        ),
      )
      .toBe(true)
    await verifyGeometry(page, approval)
    await page.screenshot({
      path: test.info().outputPath(`native-approval-${width}.png`),
      fullPage: true,
    })
    await page
      .locator('.ant-segmented-item')
      .filter({ has: page.getByRole('radio', { name: /任务活动/ }) })
      .click()
    await page
      .locator('[data-activity-id="task:native-run"]')
      .getByRole('button', { name: /^查看动态：/ })
      .click()
    await page.getByRole('button', { name: '取消任务', exact: true }).click()
    await expect(page.locator('body')).toContainText('已请求取消，等待执行端确认')
    expect(run.status).toBe('running')
    expect(posts.filter((request) => request.path.endsWith('/messages/stream'))).toHaveLength(0)
    run.status = 'canceled'
    run.phase = 'canceled'
    await page.reload()
    await expect(page.locator('body')).toContainText('已取消本次任务。')
    await page.screenshot({
      path: test.info().outputPath(`native-harness-${width}.png`),
      fullPage: true,
    })
    verifyBrowserEvidence(evidence)
  })
}
