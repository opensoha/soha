import { expect, test, type BrowserContext, type Page } from '@playwright/test'
import { verifyGeometry } from '../fixtures/geometry'
import { installMocks, verifyBrowserEvidence } from '../fixtures/mock-app'

const time = '2026-10-16T04:00:00Z'
const event = {
  id: 'alert-qa',
  title: 'Latency incident QA',
  severity: 'warning',
  status: 'firing',
  ruleId: 'rule-qa',
  clusterId: 'cluster-a',
  namespace: 'monitoring',
  summary: 'P95 over threshold',
  startsAt: time,
  lastSeenAt: time,
}
const rule = {
  id: 'rule-qa',
  name: 'Latency rule QA',
  ruleType: 'metrics',
  datasourceSelector: { sourceKind: 'metrics', clusterId: 'cluster-a', namespace: 'monitoring' },
  querySpec: { metricKey: 'cpu_usage', windowMinutes: 60, stepSeconds: 60 },
  thresholdSpec: { operator: 'gt', reducer: 'latest', value: 80 },
  labels: { severity: 'warning' },
  annotations: {},
  groupBy: [],
  forSeconds: 60,
  notificationPolicyId: 'policy-qa',
  healingPolicyIds: ['heal-qa'],
  enabled: true,
}
const healing = {
  id: 'heal-qa',
  name: 'Recovery policy QA',
  triggerMode: 'approval_then_auto',
  workflowTemplateId: 'recovery-template',
  cooldownSeconds: 300,
  safetyWindowSeconds: 600,
  enabled: true,
}
const run = {
  id: 'run-qa',
  policyId: healing.id,
  eventId: event.id,
  status: 'pending',
  approvalStatus: 'pending',
  workflowStatus: 'queued',
  createdAt: time,
  updatedAt: time,
}
const policy = {
  id: 'policy-qa',
  name: 'Notification policy QA',
  matchers: {},
  processorChain: ['template_render'],
  channelRefs: ['channel-qa'],
  enabled: true,
}
const template = {
  id: 'template-qa',
  name: 'Template QA',
  templateType: 'generic_json',
  contentType: 'application/json',
  bodyTemplate: '{}',
  headers: {},
  queryParams: {},
  enabled: true,
}
const channel = {
  id: 'channel-qa',
  name: 'Channel QA',
  channelType: 'webhook',
  config: { url: 'https://example.test/hook' },
  enabled: true,
}
const integration = {
  id: 'integration-qa',
  name: 'Integration QA',
  integrationType: 'alertmanager_v1',
  tokenPreview: 'qa…mask',
  enabled: true,
  status: 'pending',
  labelMapping: {},
  dedupeConfig: {},
}
const schedule = {
  id: 'schedule-qa',
  name: 'Schedule QA',
  enabled: true,
  timeZone: 'Asia/Shanghai',
  createdAt: time,
  updatedAt: time,
}
const rotation = {
  id: 'rotation-qa',
  scheduleId: schedule.id,
  name: 'Rotation QA',
  participants: ['responder-qa'],
  rotationConfig: { mode: 'daily', startAt: time },
  enabled: true,
  createdAt: time,
  updatedAt: time,
}
const escalation = {
  id: 'escalation-qa',
  name: 'Escalation QA',
  steps: [{ scheduleId: schedule.id, delayMinutes: 0 }],
  enabled: true,
}
const assignment = {
  id: 'assignment-qa',
  name: 'Assignment QA',
  targetType: 'schedule',
  targetRef: schedule.id,
  enabled: true,
  routeOrder: 1,
  priority: 1,
}
const menuNames = [
  'alerts',
  'rules',
  'healing',
  'notifications',
  'integrations',
  'events',
  'oncall',
  'oncall/settings',
]

async function mocks(context: BrowserContext, page: Page) {
  const state = {
    readFailure: '',
    readStatus: 200,
    writeStatus: 200,
    testState: 'ok',
    returnedToken: '',
    writes: [] as { path: string; body: any }[],
  }
  const failure = (status: number) => ({
    status,
    body: {
      error: {
        code: 'qa_failure',
        message: '处置后端暂不可用 QA',
        request_id: 'qa-response-request',
      },
    },
  })
  const evidence = await installMocks(context, page, (url, method, request) => {
    const path = url.pathname.replace('/api/v1', '')
    if (path === '/access/permission-snapshot')
      return {
        body: {
          data: {
            permissionKeys: [
              'workbench.home.view',
              'workbench.monitoring.view',
              'observe.monitoring.view',
              'observe.alerts.view',
              'observe.alerts.ack',
              'observe.alerts.update',
              'observe.alert-rules.view',
              'observe.alert-rules.manage',
              'observe.healing.view',
              'observe.healing.create',
              'observe.healing.update',
              'observe.healing.heal',
              'observe.healing.approve',
              'observe.healing.reject',
              'observe.healing.retry',
              'observe.notifications.view',
              'observe.notifications.create',
              'observe.notifications.update',
              'observe.alert-integrations.view',
              'observe.alert-integrations.create',
              'observe.alert-integrations.update',
              'observe.alert-integrations.test',
              'observe.events.view',
              'observe.oncall.view',
              'observe.oncall.create',
              'observe.oncall.update',
            ],
            visibleMenuIds: menuNames.map(
              (name) => `monitoring-workbench-${name.replace('/', '-')}`,
            ),
            visibleMenus: menuNames.map((name) => ({
              id: `monitoring-workbench-${name.replace('/', '-')}`,
              path: `/monitoring-workbench/${name}`,
              labelZh: name,
            })),
          },
        },
      }
    if (path === '/announcements/inbox') return { body: { data: { items: [], unreadCount: 0 } } }
    if (method !== 'GET') {
      if (path === '/auth/refresh') return undefined
      state.writes.push({ path, body: request.postDataJSON() })
      if (state.writeStatus !== 200) return failure(state.writeStatus)
      if (path.endsWith('/test') && path.startsWith('/alert-rules/'))
        return {
          body: {
            data: {
              state: state.testState,
              matched: false,
              summary: state.testState === 'error' ? '规则测试业务错误 QA' : '规则测试完成 QA',
              errors: [],
              samples: [],
            },
          },
        }
      if (path === '/alert-integrations/test')
        return {
          body: {
            data: {
              integrationType: 'alertmanager_v1',
              source: 'alertmanager',
              acceptedCount: 1,
              alerts: [],
              summary: 'Normalized QA',
            },
          },
        }
      if (path === '/alert-integrations' || path.startsWith('/alert-integrations/'))
        return { body: { data: { ...integration, token: state.returnedToken || undefined } } }
      if (path.includes('/heal') || path.startsWith('/healing-runs')) return { body: { data: run } }
      return {
        body: { data: path.includes('acknowledge') ? { ...event, status: 'acknowledged' } : {} },
      }
    }
    if (path === state.readFailure && state.readStatus !== 200) return failure(state.readStatus)
    if (path === '/alert-events')
      return {
        body: {
          items: url.searchParams.has('limit')
            ? [event, { ...event, id: 'other-qa', title: 'Other event QA' }]
            : [event],
        },
      }
    if (path === '/alert-events/alert-qa') return { body: { data: event } }
    if (path === '/alert-rules') return { body: { items: [rule] } }
    if (path === '/alert-rules/rule-qa') return { body: { data: rule } }
    if (path === '/alert-rule-runs')
      return {
        body: {
          items: [
            {
              id: 'rule-run-qa',
              ruleId: rule.id,
              status: 'completed',
              matched: true,
              createdAt: time,
            },
          ],
        },
      }
    if (path === '/healing-policies') return { body: { items: [healing] } }
    if (path === '/healing-runs') return { body: { items: [run] } }
    if (path === '/alert-delivery-logs')
      return {
        body: {
          items: [
            {
              id: 'delivery-qa',
              channelId: channel.id,
              status: 'failed',
              summary: 'Delivery failed QA',
              createdAt: time,
            },
          ],
        },
      }
    if (path === '/notification-policies') return { body: { items: [policy] } }
    if (path === '/notification-templates') return { body: { items: [template] } }
    if (path === '/notification-channels') return { body: { items: [channel] } }
    if (path === '/alert-routes')
      return {
        body: {
          items: [
            {
              id: 'route-qa',
              name: 'Route QA',
              channelIds: [channel.id],
              matchers: {},
              enabled: true,
            },
          ],
        },
      }
    if (path === '/alert-silences')
      return {
        body: {
          items: [
            {
              id: 'silence-qa',
              name: 'Silence QA',
              matchers: {},
              startsAt: time,
              endsAt: '2026-10-17T00:00:00Z',
              enabled: true,
            },
          ],
        },
      }
    if (path.endsWith('/preview'))
      return {
        body: {
          items: [
            {
              channelId: channel.id,
              templateId: template.id,
              body:
                url.searchParams.get('eventId') === 'other-qa'
                  ? 'Preview other QA'
                  : 'Preview first QA',
              url: 'https://example.test/hook',
              method: 'POST',
            },
          ],
        },
      }
    if (path === '/alert-integrations') return { body: { items: [integration] } }
    if (path === '/events')
      return {
        body: {
          items: [
            {
              id: 'event-entry-qa',
              summary: 'Evidence event QA',
              source: 'audit',
              category: 'application',
              severity: 'warning',
              occurredAt: time,
            },
          ],
        },
      }
    if (path === '/oncall/schedules') return { body: { items: [schedule] } }
    if (path === '/oncall/rotations') return { body: { items: [rotation] } }
    if (path === '/oncall/escalation-policies') return { body: { items: [escalation] } }
    if (path === '/oncall/routes') return { body: { items: [assignment] } }
    if (path === '/oncall/tasks')
      return {
        body: {
          items: [
            {
              id: 'task-qa',
              title: 'Response task QA',
              severity: 'warning',
              status: 'pending',
              resolutionStatus: 'unresolved',
              createdAt: time,
              updatedAt: time,
            },
          ],
        },
      }
    if (path === '/access/users')
      return {
        body: {
          items: [{ id: 'responder-qa', username: 'responder-qa', displayName: 'Responder QA' }],
        },
      }
  })
  return { state, evidence }
}

for (const [route, api, row] of [
  ['alerts', '/alert-events', event.title],
  ['rules', '/alert-rules', rule.name],
  ['healing', '/healing-policies', healing.name],
  ['notifications', '/notification-policies', policy.name],
  ['integrations', '/alert-integrations', integration.name],
  ['events', '/events', 'Evidence event QA'],
  ['oncall/settings', '/oncall/routes', assignment.name],
] as const) {
  test(`${route} retains stale records and hides them after access denial`, async ({
    context,
    page,
  }) => {
    const { state, evidence } = await mocks(context, page)
    await page.goto(`/monitoring-workbench/${route}`)
    await expect(page.getByText(row, { exact: true })).toBeVisible()
    state.readFailure = api
    state.readStatus = 503
    await page.getByRole('button', { name: '刷新列表', exact: true }).click()
    await expect(page.getByText(/上次读取结果/)).toBeVisible()
    await expect(page.getByText(row, { exact: true })).toBeVisible()
    state.readStatus = 403
    await page.getByRole('button', { name: /重试读取/ }).click()
    await expect(page.getByText(/无访问权限/)).toBeVisible()
    await expect(page.getByText(row, { exact: true })).toBeHidden()
    expect(state.writes).toEqual([])
    verifyBrowserEvidence(evidence)
  })
}

test('alert detail reads independently and unknown acknowledgement blocks replay', async ({
  context,
  page,
}, info) => {
  const { state, evidence } = await mocks(context, page)
  await page.goto('/monitoring-workbench/alerts/alert-qa')
  await expect(page.getByText(event.title, { exact: true })).toBeVisible()
  state.readFailure = '/alert-rules/rule-qa'
  state.readStatus = 503
  await page.getByRole('button', { name: '刷新告警详情' }).click()
  await expect(page.getByText(/告警规则上下文未更新/)).toBeVisible()
  await expect(page.getByText(rule.name, { exact: true })).toBeVisible()
  state.readStatus = 403
  await page.getByRole('button', { name: /重试读取告警规则上下文/ }).click()
  await expect(page.getByText(rule.name, { exact: true })).toBeHidden()
  state.writeStatus = 503
  await page.getByRole('button', { name: /确\s*认/, exact: true }).click()
  await expect(page.getByText(/确认告警：请求结果不明/)).toBeVisible()
  await expect(page.getByRole('button', { name: /确\s*认/, exact: true })).toBeDisabled()
  await expect(page.getByRole('button', { name: /恢\s*复/, exact: true })).toBeDisabled()
  expect(state.writes.filter((write) => write.path.endsWith('/acknowledge'))).toHaveLength(1)
  await page.screenshot({
    path: info.outputPath('response-alert-detail.png'),
    animations: 'disabled',
  })
  state.readFailure = '/alert-events/alert-qa'
  state.readStatus = 403
  await page.getByRole('button', { name: '刷新告警详情' }).click()
  await expect(page.getByText('告警事件无访问权限')).toBeVisible()
  await expect(page.getByText(/确认告警：请求结果不明/)).toBeVisible()
  await expect(page.getByText(event.title, { exact: true })).toBeHidden()
  verifyBrowserEvidence(evidence)
})

test('healing submission preserves its event and policy and shows a run receipt', async ({
  context,
  page,
}) => {
  const { state, evidence } = await mocks(context, page)
  await page.goto('/monitoring-workbench/alerts')
  await page.getByRole('button', { name: '触发自愈' }).click()
  const dialog = page.getByRole('dialog')
  await dialog.getByRole('combobox').click()
  await page
    .locator('.ant-select-item-option-content')
    .getByText(healing.name, { exact: true })
    .click()
  await dialog.getByRole('button', { name: /确\s*定|OK/, exact: true }).click()
  await expect(dialog).toBeHidden()
  await expect(page.getByText('自愈请求已返回')).toBeVisible()
  await expect(page.getByText('运行 run-qa')).toBeVisible()
  expect(state.writes[0].path).toBe('/alert-events/alert-qa/heal')
  verifyBrowserEvidence(evidence)
})

test('rule business errors remain retryable reads', async ({ context, page }) => {
  const { state, evidence } = await mocks(context, page)
  await page.goto('/monitoring-workbench/rules')
  state.testState = 'error'
  await page.getByRole('button', { name: '测试告警规则' }).click()
  const dialog = page.getByRole('dialog')
  await expect(dialog.getByText('规则测试业务错误 QA', { exact: true }).first()).toBeVisible()
  await expect(dialog.getByText(/请求结果不明/)).toBeHidden()
  state.testState = 'ok'
  await dialog.getByRole('button', { name: '重试测试' }).click()
  await expect(dialog.getByText('规则测试完成 QA', { exact: true })).toBeVisible()
  expect(state.writes.filter((write) => write.path.endsWith('/test'))).toHaveLength(2)
  verifyBrowserEvidence(evidence)
})

test('unknown rule saves keep the editor and original form locked', async ({ context, page }) => {
  const { state, evidence } = await mocks(context, page)
  await page.goto('/monitoring-workbench/rules')
  await page.getByRole('button', { name: '编辑告警规则' }).click()
  await page.getByLabel('名称', { exact: true }).fill('Rule draft QA')
  await page.getByRole('button', { name: '下一步' }).click()
  state.writeStatus = 503
  await page
    .getByRole('dialog')
    .getByRole('button', { name: /保\s*存/, exact: true })
    .click()
  await expect(page.getByText(/更新告警规则：请求结果不明/)).toBeVisible()
  await page.keyboard.press('Escape')
  await expect(page.getByRole('dialog')).toBeVisible()
  await expect(page.getByRole('button', { name: /保\s*存/, exact: true })).toBeDisabled()
  expect(state.writes[0].body.name).toBe('Rule draft QA')
  expect(state.writes).toHaveLength(1)
  verifyBrowserEvidence(evidence)
})

test('notification channel unknown saves preserve the private draft in the locked modal', async ({
  context,
  page,
}, info) => {
  const { state, evidence } = await mocks(context, page)
  await page.goto('/monitoring-workbench/notifications')
  await page.getByRole('tab', { name: '通知渠道', exact: true }).click()
  await page.getByRole('button', { name: '编辑通知渠道' }).click()
  await page
    .getByLabel('配置(JSON)', { exact: true })
    .fill('{"url":"https://example.test/qa-draft"}')
  state.writeStatus = 503
  await page
    .getByRole('dialog')
    .getByRole('button', { name: /保\s*存/, exact: true })
    .click()
  await expect(page.getByText(/更新通知渠道：请求结果不明/)).toBeVisible()
  await page.keyboard.press('Escape')
  await expect(page.getByRole('dialog')).toBeVisible()
  await expect(page.getByLabel('配置(JSON)', { exact: true })).toHaveValue(
    '{"url":"https://example.test/qa-draft"}',
  )
  await expect(page.getByLabel('配置(JSON)', { exact: true })).toBeDisabled()
  expect(state.writes).toHaveLength(1)
  await page.screenshot({
    path: info.outputPath('response-notification-unknown.png'),
    animations: 'disabled',
  })
  verifyBrowserEvidence(evidence)
})

test('notification preview isolates evidence when selecting a different event', async ({
  context,
  page,
}) => {
  const { state, evidence } = await mocks(context, page)
  await page.goto('/monitoring-workbench/notifications')
  await page.getByRole('button', { name: '预览通知策略' }).click()
  const dialog = page.getByRole('dialog')
  await expect(dialog.getByText('Preview first QA', { exact: true })).toBeVisible()
  state.readFailure = '/notification-policies/policy-qa/preview'
  state.readStatus = 503
  await page.getByLabel('通知预览事件').click()
  await page
    .locator('.ant-select-item-option-content')
    .getByText('Other event QA (firing)', { exact: true })
    .click()
  await expect(dialog.getByText(/读取失败/)).toBeVisible()
  await expect(dialog.getByText('Preview first QA', { exact: true })).toBeHidden()
  expect(state.writes).toEqual([])
  verifyBrowserEvidence(evidence)
})

test('integration one-time token stays in its private display and clears on close', async ({
  context,
  page,
}) => {
  const { state, evidence } = await mocks(context, page)
  await page.goto('/monitoring-workbench/integrations')
  await page.getByRole('button', { name: '编辑告警集成' }).click()
  state.returnedToken = 'qa-one-time-token'
  await page
    .getByRole('dialog')
    .getByRole('button', { name: /保\s*存/, exact: true })
    .click()
  const dialog = page.getByRole('dialog', { name: '集成 Token' })
  await expect(dialog.locator('input')).toHaveValue(state.returnedToken)
  await expect(page.locator('#root').getByText('告警集成记录已更新')).toBeVisible()
  await dialog.getByRole('button', { name: /完\s*成/, exact: true }).click()
  await expect(dialog).toBeHidden()
  await expect(page.locator('input[value="qa-one-time-token"]')).toHaveCount(0)
  verifyBrowserEvidence(evidence)
})

test('integration normalization failures are retryable and refused drafts are cleared', async ({
  context,
  page,
}) => {
  const { state, evidence } = await mocks(context, page)
  await page.goto('/monitoring-workbench/integrations')
  await page.getByRole('button', { name: /测试 Payload/ }).click()
  state.writeStatus = 503
  const dialog = page.getByRole('dialog')
  await dialog.getByRole('button', { name: /归一化测试/ }).click()
  await expect(dialog.getByText(/归一化测试：操作请求失败/)).toBeVisible()
  await expect(dialog.getByText(/请求结果不明/)).toBeHidden()
  state.writeStatus = 403
  await dialog.getByRole('button', { name: /归一化测试/ }).click()
  await expect(dialog).toBeHidden()
  await expect(page.getByText(/归一化测试：操作请求失败/)).toBeVisible()
  expect(state.writes).toHaveLength(2)
  verifyBrowserEvidence(evidence)
})

test('oncall board distinguishes unavailable rotation from an empty schedule', async ({
  context,
  page,
}) => {
  const { state, evidence } = await mocks(context, page)
  state.readFailure = '/oncall/rotations'
  state.readStatus = 503
  await page.goto('/monitoring-workbench/oncall')
  await expect(page.getByText('轮值读取失败')).toBeVisible()
  await expect(page.getByText('轮值暂不可用', { exact: true })).toBeVisible()
  await expect(page.getByText('暂无轮值', { exact: true })).toBeHidden()
  state.readStatus = 200
  await page.getByRole('button', { name: '刷新值班板' }).click()
  await expect(page.getByText(rotation.name, { exact: true })).toBeVisible()
  verifyBrowserEvidence(evidence)
})

test('oncall auxiliary errors can be retried without discarding the rotation draft', async ({
  context,
  page,
}) => {
  const { state, evidence } = await mocks(context, page)
  state.readFailure = '/access/users'
  state.readStatus = 503
  await page.goto('/monitoring-workbench/oncall/settings')
  await page.getByRole('tab', { name: '轮值', exact: true }).click()
  await page.getByRole('button', { name: '编辑轮值' }).click()
  await expect(page.getByText('值班人员选项读取失败')).toBeVisible()
  await expect(page.getByLabel('名称', { exact: true })).toHaveValue(rotation.name)
  state.readStatus = 200
  await page.getByRole('button', { name: /重试读取值班人员选项/ }).click()
  await expect(page.getByLabel('名称', { exact: true })).toBeEnabled()
  await expect(page.getByLabel('名称', { exact: true })).toHaveValue(rotation.name)
  verifyBrowserEvidence(evidence)
})

test('unknown oncall override fixes its target and blocks duplicate submission', async ({
  context,
  page,
}, info) => {
  const { state, evidence } = await mocks(context, page)
  await page.clock.setFixedTime(new Date(time))
  await page.goto('/monitoring-workbench/oncall')
  await page.getByTitle('2026-10-16', { exact: true }).click()
  await page.getByRole('button', { name: /覆盖当日/ }).click()
  state.writeStatus = 503
  const modal = page.getByRole('dialog', { name: /值班覆盖/ })
  await modal.getByRole('button', { name: '保存覆盖', exact: true }).click()
  await expect(modal.getByText(/更新值班覆盖：请求结果不明/)).toBeVisible()
  await page.keyboard.press('Escape')
  await expect(modal).toBeVisible()
  await expect(modal.getByRole('button', { name: '保存覆盖', exact: true })).toBeDisabled()
  expect(state.writes).toHaveLength(1)
  expect(state.writes[0].path).toBe('/oncall/rotations/rotation-qa')
  expect(state.writes[0].body.name).toBe(rotation.name)
  expect(state.writes[0].body.rotationConfig.overrides).toHaveProperty('2026-10-16')
  await page.screenshot({
    path: info.outputPath('response-oncall-override.png'),
    animations: 'disabled',
  })
  verifyBrowserEvidence(evidence)
})

test('dark narrow notification editor supports keyboard and reduced motion', async ({
  context,
  page,
}, info) => {
  const { evidence } = await mocks(context, page)
  await page.emulateMedia({ reducedMotion: 'reduce' })
  await page.goto('/monitoring-workbench/notifications')
  await page.getByRole('button', { name: '切换到深色模式' }).click()
  await page.setViewportSize({ width: 390, height: 844 })
  await page.getByRole('button', { name: /新建策略/ }).focus()
  await page.keyboard.press('Enter')
  const save = page.getByRole('dialog').getByRole('button', { name: /保\s*存/, exact: true })
  await save.scrollIntoViewIfNeeded()
  await verifyGeometry(page, save)
  await page.screenshot({
    path: info.outputPath('response-dark-narrow.png'),
    animations: 'disabled',
  })
  verifyBrowserEvidence(evidence)
})

test('narrow oncall board keeps its calendar actions within the viewport', async ({
  context,
  page,
}, info) => {
  const { evidence } = await mocks(context, page)
  await page.clock.setFixedTime(new Date(time))
  await page.setViewportSize({ width: 390, height: 844 })
  await page.goto('/monitoring-workbench/oncall')
  const refresh = page.getByRole('button', { name: '刷新值班板' })
  await expect(page.getByText(rotation.name, { exact: true })).toBeVisible()
  await refresh.scrollIntoViewIfNeeded()
  await verifyGeometry(page, refresh)
  await page.screenshot({
    path: info.outputPath('response-oncall-narrow.png'),
    animations: 'disabled',
  })
  verifyBrowserEvidence(evidence)
})

test('unknown healing approval blocks the other run commands', async ({ context, page }) => {
  const { state, evidence } = await mocks(context, page)
  await page.goto('/monitoring-workbench/healing')
  await page.getByRole('tab', { name: '执行记录', exact: true }).click()
  state.writeStatus = 503
  await page.getByRole('button', { name: /通过/ }).click()
  await expect(page.getByText(/请求结果不明/)).toBeVisible()
  for (const name of ['通过', '拒绝', '重试']) {
    await expect(page.getByRole('button', { name: new RegExp(name) })).toBeDisabled()
  }
  expect(state.writes).toHaveLength(1)
  expect(state.writes[0].path).toBe('/healing-runs/run-qa/approve')
  verifyBrowserEvidence(evidence)
})
