import { test, expect, type BrowserContext, type Page } from '@playwright/test'
import { installMocks, verifyBrowserEvidence } from '../fixtures/mock-app'
import { verifyGeometry } from '../fixtures/geometry'

const runtimePath = '/api/v1/delivery/runtime/workflows/workflow-motion-demo'
const pagePath = '/workflows/workflow-motion-demo'

test.use({ video: { mode: 'on', size: { width: 1440, height: 1000 } } })

async function executionMocks(
  context: BrowserContext,
  page: Page,
  initial = 'running',
  canReview = true,
) {
  const state = {
    status: initial,
    refreshFails: false,
    refreshStatus: 503,
    approvalFails: false,
    approvalFailureStatus: 409,
    decisions: [] as string[],
    reads: 0,
    empty: false,
    scope: 'application',
    taskListStatus: 200,
  }
  const evidence = await installMocks(context, page, (url, method) => {
    const path = url.pathname
    if (path === '/api/v1/delivery/execution-tasks')
      return state.taskListStatus === 200
        ? { body: { data: [] } }
        : {
            status: state.taskListStatus,
            body: { error: { code: 'read_failed', message: '执行任务读取失败' } },
          }
    if (path === '/api/v1/access/permission-snapshot')
      return {
        body: {
          data: {
            permissionKeys: [
              'workbench.delivery.view',
              'delivery.workflows.view',
              'delivery.execution-tasks.view',
              'delivery.builds.view',
              ...(canReview ? ['delivery.application-environments.approve'] : []),
            ],
            visibleMenuIds: ['delivery-workbench', 'execution-history'],
            visibleMenus: [
              { id: 'delivery-workbench', path: '/delivery/overview', labelZh: '交付工作台' },
              {
                id: 'execution-history',
                parentId: 'delivery-workbench',
                path: '/execution-history',
                labelZh: '执行历史',
              },
            ],
          },
        },
      }
    if (path === runtimePath) {
      state.reads++
      if (state.refreshFails)
        return {
          status: state.refreshStatus,
          body: { error: { code: 'unavailable', message: '暂时无法更新执行证据' } },
        }
      const complete = state.status === 'completed'
      const failed = state.status === 'failed'
      const waiting = state.status === 'waiting_approval'
      return {
        body: {
          data: {
            kind: 'workflow',
            id: 'workflow-motion-demo',
            object: {
              id: 'workflow-motion-demo',
              applicationId: 'checkout',
              workflowName: '订单服务 · 标准发布',
              status: state.status,
              scope: state.scope,
              createdAt: '2026-10-12T04:30:00Z',
              updatedAt: '2026-10-12T04:34:00Z',
              steps: [],
              nodeRuns: state.empty
                ? []
                : [
                    {
                      nodeId: 'build',
                      name: '构建镜像',
                      type: 'build',
                      status: 'completed',
                      summary: '镜像构建完成，制品已保存。',
                      startedAt: '2026-10-12T04:30:00Z',
                      finishedAt: '2026-10-12T04:31:00Z',
                      buildRecordId: 'build-demo',
                    },
                    {
                      nodeId: 'checks',
                      name: '交付前检查',
                      type: 'check',
                      status: 'completed',
                      summary: '目标环境与配置检查通过。',
                      startedAt: '2026-10-12T04:31:00Z',
                      finishedAt: '2026-10-12T04:32:00Z',
                    },
                    {
                      nodeId: 'approve',
                      name: '发布审批',
                      type: 'manual_approval',
                      status: waiting ? 'waiting_approval' : 'completed',
                      summary: waiting ? '等待环境负责人批准本次发布。' : '环境负责人已批准。',
                    },
                    {
                      nodeId: 'deploy',
                      name: '更新服务',
                      type: 'deploy',
                      status: waiting
                        ? 'pending'
                        : complete
                          ? 'completed'
                          : failed
                            ? 'failed'
                            : 'running',
                      summary: failed
                        ? '服务就绪检查超时，请查看任务日志确认原因。'
                        : '发布新版本，核对服务就绪状态。',
                      startedAt: '2026-10-12T04:33:00Z',
                      executionTaskId: 'task-demo',
                    },
                    {
                      nodeId: 'verify',
                      name: '发布后验证',
                      type: 'verify',
                      status: complete ? 'completed' : 'pending',
                      summary: complete ? '服务健康检查通过。' : '等待更新服务阶段结束。',
                    },
                  ],
            },
            application: { id: 'checkout', key: 'checkout', name: '订单服务', enabled: true },
            environment: {
              id: 'prod',
              key: 'production',
              name: '生产环境',
              requiresApproval: true,
            },
            binding: {
              id: 'binding-demo',
              applicationId: 'checkout',
              environmentId: 'prod',
              workflowTemplateId: 'template-demo',
              targets: [],
            },
            workflowTemplate: { id: 'template-demo', name: '标准交付流程' },
            artifacts: [
              {
                id: 'image-demo',
                kind: 'container_image',
                name: 'checkout-api:2026.10.12',
                ref: 'registry.example.com/checkout-api:2026.10.12',
                status: 'available',
              },
            ],
            links: {
              application: '/applications/checkout?tab=delivery',
              audit: '/system/audit',
              operations: '/system/operations',
            },
            permissions: { canViewArtifacts: true, canViewAudit: false, canViewOperations: false },
          },
        },
      }
    }
    if (
      path === '/api/v1/workflows/workflow-motion-demo/approve' ||
      path === '/api/v1/workflows/workflow-motion-demo/reject'
    ) {
      if (method !== 'POST') return undefined
      state.decisions.push(path.endsWith('/approve') ? 'approve' : 'reject')
      if (state.approvalFails)
        return {
          status: state.approvalFailureStatus,
          body: { error: { code: 'decision_rejected', message: '审批服务暂不可用' } },
        }
      // The accepted request intentionally leaves the runtime state waiting until a later read.
      return { body: { data: {} } }
    }
    if (path === '/api/v1/delivery/runtime/execution-tasks/task-demo')
      return {
        body: {
          data: {
            kind: 'execution_task',
            id: 'task-demo',
            object: {
              id: 'task-demo',
              applicationId: 'checkout',
              taskKind: 'manifest_apply',
              status: state.status,
              createdAt: '2026-10-12T04:33:00Z',
              updatedAt: '2026-10-12T04:34:00Z',
            },
            application: { id: 'checkout', name: '订单服务' },
            artifacts: [],
            evidence: {
              logs: [
                {
                  id: 'log-demo',
                  logLevel: 'info',
                  message: '已提交更新，正在核对服务就绪状态。',
                  createdAt: '2026-10-12T04:34:00Z',
                },
              ],
            },
            links: {},
            permissions: { canViewArtifacts: false, canViewAudit: false, canViewOperations: false },
          },
        },
      }
    if (path === '/api/v1/delivery/runtime/builds/build-demo')
      return {
        body: {
          data: {
            kind: 'build',
            id: 'build-demo',
            object: {
              id: 'build-demo',
              applicationId: 'checkout',
              status: 'completed',
              createdAt: '2026-10-12T04:30:00Z',
              finishedAt: '2026-10-12T04:31:00Z',
            },
            application: { id: 'checkout', name: '订单服务' },
            evidence: {},
            links: {},
            permissions: { canViewArtifacts: false, canViewAudit: false, canViewOperations: false },
          },
        },
      }
    return undefined
  })
  return { state, evidence }
}

test.describe('sample interaction', () => {
  test('execution refresh retains selected evidence, recovers errors and confirms terminal state', async ({
    context,
    page,
  }, info) => {
    const { state, evidence } = await executionMocks(context, page)
    await page.goto(pagePath)
    const selected = page.getByRole('button', { name: '更新服务，执行中' })
    await expect(selected).toHaveAttribute('aria-pressed', 'true')
    await expect(page.locator('.soha-execution-feedback [role="status"]')).toContainText(
      '正在处理「更新服务」',
    )
    await verifyGeometry(page, selected)
    // Give each state time to read in the sample recording.
    await page.waitForTimeout(900)
    await page.screenshot({
      path: info.outputPath('execution-running.png'),
      fullPage: true,
      animations: 'disabled',
    })
    await page.getByRole('button', { name: '任务日志与结果', exact: true }).click()
    await expect(page.getByRole('dialog')).toContainText('已提交更新，正在核对服务就绪状态。')
    await page.waitForTimeout(900)
    await page.keyboard.press('Escape')
    await expect(page.getByRole('dialog')).toHaveCount(0)
    await page.getByRole('button', { name: '构建镜像，已完成' }).click()
    state.status = 'failed'
    await page.getByRole('button', { name: /刷新证据/ }).click()
    await expect(page.getByRole('button', { name: '构建镜像，已完成' })).toHaveAttribute(
      'aria-pressed',
      'true',
    )
    await expect(page.locator('.soha-execution-feedback')).toContainText('1 节点失败')
    await page.getByRole('button', { name: '更新服务，执行失败' }).click()
    await expect(page.getByRole('region', { name: '节点证据' })).toContainText('服务就绪检查超时')
    await page.waitForTimeout(900)
    await page.screenshot({
      path: info.outputPath('execution-failed.png'),
      fullPage: true,
      animations: 'disabled',
    })
    state.refreshFails = true
    await page.getByRole('button', { name: /刷新证据/ }).click()
    await expect(page.getByText('证据未更新，正在展示上次记录')).toBeVisible()
    await page.waitForTimeout(900)
    await expect(page.getByRole('button', { name: '更新服务，执行失败' })).toHaveAttribute(
      'aria-pressed',
      'true',
    )
    state.refreshFails = false
    state.status = 'completed'
    await page.getByRole('button', { name: /^重\s*试$/ }).click()
    await expect(page.locator('.soha-execution-feedback')).toContainText('5 / 5 节点成功')
    await expect(page.locator('.soha-execution-live-dot')).toHaveCount(0)
    await expect(page.getByText('证据未更新，正在展示上次记录')).toHaveCount(0)
    await page.screenshot({
      path: info.outputPath('execution-completed.png'),
      fullPage: true,
      animations: 'disabled',
    })
    const reads = state.reads
    await page.waitForTimeout(4200)
    expect(state.reads).toBe(reads)
    verifyBrowserEvidence(evidence)
  })
})

test('approval requires confirmation and preserves failure/retry and accepted-but-unconfirmed feedback', async ({
  context,
  page,
}, info) => {
  const { state, evidence } = await executionMocks(context, page, 'waiting_approval')
  await page.goto(pagePath)
  await page.getByRole('button', { name: '批准执行', exact: true }).click()
  await expect(page.getByRole('dialog')).toContainText('批准本次执行？')
  await page
    .getByRole('dialog')
    .getByRole('button', { name: /^返\s*回$/ })
    .click()
  expect(state.decisions).toEqual([])
  await expect(page.getByRole('dialog')).toHaveCount(0)
  state.approvalFails = true
  await page.getByRole('button', { name: '批准执行', exact: true }).click()
  await page.getByRole('dialog').getByRole('button', { name: '确认批准', exact: true }).click()
  await expect(page.getByRole('dialog')).toContainText('审批未提交。审批服务暂不可用')
  await page
    .getByRole('dialog')
    .getByRole('button', { name: /^返\s*回$/ })
    .click()
  await expect(page.getByRole('dialog')).toHaveCount(0)
  state.approvalFails = false
  await page.getByRole('button', { name: '批准执行', exact: true }).click()
  await page.getByRole('dialog').getByRole('button', { name: '确认批准', exact: true }).click()
  await expect(page.getByRole('dialog')).toHaveCount(0)
  await expect(page.getByText('审批已提交，等待执行记录确认')).toBeVisible()
  await expect(page.getByRole('button', { name: '批准执行', exact: true })).toBeDisabled()
  await expect(page.locator('.soha-execution-feedback')).toContainText('等待审批')
  await expect(page.locator('.soha-execution-live-dot')).toHaveCount(0)
  await expect(page.getByText('服务端处理失败', { exact: true })).toHaveCount(0, { timeout: 7000 })
  await page.screenshot({
    path: info.outputPath('execution-approval.png'),
    fullPage: true,
    animations: 'disabled',
  })
  expect(state.decisions).toEqual(['approve', 'approve'])
  state.status = 'running'
  await page.getByRole('button', { name: /刷新证据/ }).click()
  await expect(page.getByText('审批已提交，等待执行记录确认')).toHaveCount(0)
  verifyBrowserEvidence(evidence)
})

test('approval with an unknown POST outcome cannot be repeated after a successful read', async ({
  context,
  page,
}) => {
  const { state, evidence } = await executionMocks(context, page, 'waiting_approval')
  state.approvalFails = true
  state.approvalFailureStatus = 503
  await page.goto(pagePath)
  await page.getByRole('button', { name: '批准执行', exact: true }).click()
  await page.getByRole('dialog').getByRole('button', { name: '确认批准', exact: true }).click()
  await expect(page.getByRole('dialog')).toContainText('审批请求结果不明')
  await expect(
    page.getByRole('dialog').getByRole('button', { name: '确认批准', exact: true }),
  ).toBeDisabled()
  await page
    .getByRole('dialog')
    .getByRole('button', { name: /^返\s*回$/ })
    .click()
  await page.getByRole('button', { name: /刷新证据/ }).click()
  await expect(page.getByRole('button', { name: '批准执行', exact: true })).toBeDisabled()
  expect(state.decisions).toEqual(['approve'])
  verifyBrowserEvidence(evidence)
})

for (const theme of ['light', 'dark'] as const)
  test(`execution ${theme} narrow view keeps keyboard access and reduced motion`, async ({
    context,
    page,
  }, info) => {
    await page.addInitScript(
      (themeMode) =>
        localStorage.setItem(
          'soha-prefs',
          JSON.stringify({ state: { themeMode, localeCode: 'zh_CN' }, version: 0 }),
        ),
      theme,
    )
    await page.setViewportSize({ width: 390, height: 900 })
    await page.emulateMedia({ reducedMotion: 'reduce' })
    const { evidence } = await executionMocks(context, page, 'running', false)
    await page.goto(pagePath)
    await expect(
      page.getByRole('heading', { name: '订单服务 · 标准发布', exact: true }),
    ).toBeVisible()
    await page.screenshot({
      path: info.outputPath(`execution-${theme}-mobile.png`),
      fullPage: true,
      animations: 'disabled',
    })
    const node = page.getByRole('button', { name: '构建镜像，已完成' })
    await node.focus()
    await page.keyboard.press('Enter')
    await expect(node).toHaveAttribute('aria-pressed', 'true')
    await expect(node).toBeFocused()
    await verifyGeometry(page, node)
    await expect(page.locator('.soha-workflow-execution')).toHaveAttribute(
      'data-motion-disabled',
      'true',
    )
    await expect(page.locator('.soha-execution-live-dot')).toHaveCount(0)
    expect(
      await page
        .locator('html')
        .evaluate((el) => getComputedStyle(el).getPropertyValue('--soha-motion-duration-mid')),
    ).toBe('0s')
    await page.getByRole('button', { name: '构建明细', exact: true }).scrollIntoViewIfNeeded()
    await verifyGeometry(page, page.getByRole('button', { name: '构建明细', exact: true }))
    await page.screenshot({
      path: info.outputPath(`execution-${theme}-mobile-evidence.png`),
      fullPage: true,
      animations: 'disabled',
    })
    verifyBrowserEvidence(evidence)
  })

test('readonly approval, cancelled and unknown evidence do not appear successful', async ({
  context,
  page,
}, info) => {
  const { state, evidence } = await executionMocks(context, page, 'waiting_approval', false)
  await page.goto(pagePath)
  await expect(page.getByRole('button', { name: '批准执行', exact: true })).toHaveCount(0)
  await expect(page.locator('.soha-execution-feedback')).toContainText('等待审批')
  state.status = 'cancelled'
  await page.getByRole('button', { name: /刷新证据/ }).click()
  await expect(page.locator('.soha-execution-feedback')).toContainText('已取消')
  await expect(page.locator('.soha-execution-live-dot')).toHaveCount(0)
  await page.screenshot({
    path: info.outputPath('execution-cancelled.png'),
    fullPage: true,
    animations: 'disabled',
  })
  state.status = 'external_unknown'
  state.empty = true
  await page.getByRole('button', { name: /刷新证据/ }).click()
  await expect(page.locator('.soha-execution-feedback')).toContainText(
    '状态未知 · external_unknown',
  )
  await expect(page.getByText('本次运行尚未返回节点记录。')).toBeVisible()
  verifyBrowserEvidence(evidence)
})

test('first-load failure can retry into the actual execution page', async ({ context, page }) => {
  const { state, evidence } = await executionMocks(context, page)
  state.refreshFails = true
  await page.goto(pagePath)
  await expect(page.getByRole('button', { name: '重试执行证据' })).toBeVisible()
  await expect(page.locator('.ant-notification-notice')).toHaveCount(0)
  await expect(page.locator('.soha-workflow-execution')).toHaveCount(0)
  state.refreshFails = false
  await page.getByRole('button', { name: '重试执行证据' }).click()
  await expect(page.getByRole('button', { name: '更新服务，执行中' })).toBeVisible()
  verifyBrowserEvidence(evidence)
})

test('global table feedback preserves an empty cached result and removes denied evidence', async ({
  context,
  page,
}, info) => {
  const { state, evidence } = await executionMocks(context, page)
  await page.goto('/delivery/execution-tasks')
  await expect(page.getByText('当前筛选条件下没有可展示的记录。')).toBeVisible()
  state.taskListStatus = 503
  await page.getByRole('button', { name: '刷新', exact: true }).click()
  await expect(page.getByText('列表未更新，正在展示上次读取结果')).toBeVisible()
  await expect(page.getByText('当前筛选条件下没有可展示的记录。')).toBeVisible()
  await expect(page.locator('.ant-notification-notice')).toHaveCount(0)
  await page.screenshot({ path: info.outputPath('global-table-cached-empty.png'), fullPage: true })
  state.taskListStatus = 403
  await page.getByRole('button', { name: '重试读取列表', exact: true }).click()
  await expect(page.getByText('列表无访问权限')).toBeVisible()
  await expect(page.getByText('执行状态不可用')).toBeVisible()
  await expect(page.getByText('重试状态不可用')).toBeVisible()
  await expect(page.getByText('当前筛选条件下没有可展示的记录。')).toHaveCount(0)
  await expect(page.locator('.ant-notification-notice')).toHaveCount(0)
  verifyBrowserEvidence(evidence)
})

test('batch scope and an approval state change block workflow decisions', async ({
  context,
  page,
}) => {
  const { state, evidence } = await executionMocks(context, page, 'waiting_approval')
  state.scope = 'delivery_batch'
  await page.goto(pagePath)
  await expect(page.locator('.soha-execution-feedback')).toContainText('等待审批')
  await expect(page.getByRole('button', { name: '批准执行', exact: true })).toHaveCount(0)
  state.scope = 'application'
  await page.getByRole('button', { name: /刷新证据/ }).click()
  await page.getByRole('button', { name: '批准执行', exact: true }).click()
  state.status = 'running'
  await page.getByRole('dialog').getByRole('button', { name: '确认批准', exact: true }).click()
  await expect(page.getByRole('dialog')).toContainText('执行状态已变化')
  expect(state.decisions).toEqual([])
  verifyBrowserEvidence(evidence)
})

test('delivery detail removes cached evidence after access is revoked', async ({
  context,
  page,
}) => {
  const { state, evidence } = await executionMocks(context, page, 'completed')
  await page.goto(pagePath)
  await expect(
    page.getByRole('heading', { name: '订单服务 · 标准发布', exact: true }),
  ).toBeVisible()
  state.refreshFails = true
  state.refreshStatus = 403
  await page.getByRole('button', { name: /刷新证据/ }).click()
  await expect(page.getByRole('button', { name: '重试执行证据' })).toBeVisible()
  await expect(page.locator('.soha-workflow-execution')).toHaveCount(0)
  await expect(page.getByRole('heading', { name: '订单服务 · 标准发布', exact: true })).toHaveCount(
    0,
  )
  await expect(page.locator('.ant-notification-notice')).toHaveCount(0)
  verifyBrowserEvidence(evidence)
})
