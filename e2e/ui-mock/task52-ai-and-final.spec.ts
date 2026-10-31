import { test, expect, type BrowserContext, type Page } from '@playwright/test'
import { installMocks, verifyBrowserEvidence } from '../fixtures/mock-app'
import { verifyGeometry } from '../fixtures/geometry'
import type { NetworkTopologyView } from '../../src/features/platform/network/topology/types'

const cases = [
  ['/ai-workbench/knowledge', '/ai/knowledge-bases', '知识库样板'],
  ['/ai-workbench/evaluations', '/ai/evaluations/runs', 'qa-evaluation-run'],
  ['/ai-workbench/environments', '/ai/environments/templates', '环境样板'],
  ['/ai-workbench/knowledge-pipelines', '/ai/knowledge/connectors', '知识流水线样板'],
  ['/ai-workbench/provider-fleet', '/ai/agent-providers/rollouts', 'Provider 样板'],
  ['/ai-workbench/evaluation-lifecycle', '/ai/evaluations/replays', '回放样板'],
  ['/ai-workbench/memory', '/ai/memory', 'Memory 样板'],
  ['/ai-workbench/production-operations', '/ai/operations', '生产操作样板'],
  ['/ai-workbench/inspection', '/copilot/inspection-tasks', '巡检样板'],
  ['/ai-workbench/agent-runs', '/copilot/agent-runs', 'Agent 运行样板'],
  ['/account/profile', '/auth/profile', '资料样板'],
  ['/ai-workbench/agent-providers', '/copilot/workbench/catalog', 'Agent Provider 样板'],
] as const
const catalog = {
  adapters: [],
  dataSources: [],
  analysisProfiles: [],
  agentProviders: [
    {
      id: 'qa-provider',
      name: 'Agent Provider 样板',
      kind: 'hermes',
      enabled: true,
      capabilities: ['general'],
      runtimeStatus: { state: 'ready', queuedRuns: 0, runningRuns: 0, recentFailures: 0 },
    },
  ],
  capabilities: [],
  skillsRegistry: [],
}
const featureKeys = [
  'agent.environment_management',
  'knowledge.external_connectors',
  'knowledge.async_ingestion',
  'agent.fleet_rollout',
  'agent.conformance_suite',
  'evaluation.candidate_executor',
  'evaluation.isolated_replay',
  'evaluation.release_gate',
  'evaluation.feedback_sampling',
  'memory.long_term',
  'ai.production_operations',
]
const permissions = [
  'workspace.resource.view',
  'workbench.ai.view',
  'workbench.platform.view',
  'observe.ai.view',
  'observe.ai.chat',
  'observe.ai.inspection.create',
  'observe.ai.inspection.update',
  'observe.ai.inspection.delete',
  'observe.ai.inspection.run',
  'ai.agent-providers.view',
  'ai.knowledge.view',
  'ai.knowledge.create',
  'ai.knowledge.update',
  'ai.knowledge.delete',
  'ai.knowledge.ingestion.operate',
  'ai.knowledge.sources.view',
  'ai.knowledge.jobs.view',
  'ai.environments.view',
  'ai.environments.update',
  'ai.agent-fleet.view',
  'ai.agent-fleet.update',
  'ai.evaluations.view',
  'ai.evaluations.create',
  'ai.evaluations.execute',
  'ai.memory.view',
  'ai.memory.update',
  'ai.operations.view',
  'ai.operations.update',
  'settings.ai.view',
  'settings.ai.update',
  'ai.gateway.view',
  'ai.gateway.invoke',
  'ai.gateway.tokens.view',
  'ai.gateway.tokens.create',
  'ai.gateway.tokens.rotate',
  'ai.gateway.tokens.revoke',
  'platform.network.topology.view',
  'platform.clusters.view',
]
const extras = [
  '/ai/evaluations/datasets',
  '/ai/environments/leases',
  '/ai/knowledge/sync-jobs',
  '/ai/agent-providers/conformance-runs',
  '/ai/evaluations/gate-policies',
  '/ai/evaluations/feedback',
  '/ai/memory/policies',
  '/ai/operations/runbook-evidence',
  '/copilot/inspection-runs',
  '/copilot/automation-policies',
  '/auth/providers',
  '/ai-gateway/personal-access-tokens',
  '/copilot/sessions',
]
const row = (name: string) => ({
  id: name === 'qa-evaluation-run' ? name : 'qa-record',
  datasetId: 'qa-dataset',
  datasetVersion: 'v1',
  startedAt: '2026-10-16T00:00:00Z',
  name,
  title: name,
  providerId: 'hermes',
  providerKind: 'hermes',
  capabilityId: name,
  status: 'running',
  checks: [],
  candidateRefs: {},
  scopeType: 'platform',
  enabled: true,
  createdAt: '2026-10-16T00:00:00Z',
  updatedAt: '2026-10-16T00:00:00Z',
})
async function mocks(context: BrowserContext, page: Page) {
  const state = {
    signedOut: false,
    failedPath: '',
    status: 200,
    writeStatus: 503,
    writes: [] as { path: string; body: unknown }[],
  }
  const evidence = await installMocks(context, page, (url, method, request) => {
    const path = url.pathname.replace('/api/v1', '')
    if (state.signedOut && path === '/auth/refresh' && method === 'POST')
      return {
        status: 401,
        body: { error: { code: 'unauthenticated', message: 'No browser session' } },
      }
    if (
      (method === 'GET' && path === state.failedPath && state.status !== 200) ||
      (method === 'POST' && path === '/ai/knowledge-bases')
    ) {
      if (method === 'POST') state.writes.push({ path, body: request.postDataJSON() })
      return {
        status: method === 'GET' ? state.status : state.writeStatus,
        body: {
          error: { code: 'qa_failure', message: '本次验证请求失败', request_id: 'qa-ai-request' },
        },
      }
    }
    if (method !== 'GET') return undefined
    if (path === '/access/permission-snapshot')
      return {
        body: {
          data: {
            permissionKeys: permissions,
            visibleMenuIds: ['ai-workbench', ...cases.map(([path]) => path.split('/').at(-1))],
            visibleMenus: [
              { id: 'ai-workbench', path: '/ai-workbench/overview', labelZh: 'AI 工作台' },
            ],
          },
        },
      }
    if (path === '/modules')
      return {
        body: {
          data: [
            {
              descriptor: { id: 'ai' },
              enabled: true,
              features: Object.fromEntries(featureKeys.map((key) => [key, true])),
            },
          ],
        },
      }
    if (path === '/copilot/workbench/catalog') return { body: { data: catalog } }
    if (path === '/clusters/cluster-a/network/topology')
      return {
        body: {
          data: {
            clusterId: 'cluster-a',
            namespace: 'monitoring',
            graphVersion: 'v1',
            source: 'kubernetes-snapshot',
            generatedAt: new Date().toISOString(),
            observedAt: new Date().toISOString(),
            summary: {
              entryCount: 1,
              routeCount: 1,
              serviceCount: 2,
              missingServiceCount: 0,
              backendPodCount: 0,
              pendingRouteCount: 0,
            },
            nodes: [
              {
                id: 'qa-entry',
                type: 'entry',
                label: 'qa.example.test',
                provider: 'kubernetes',
                protocol: 'HTTP',
                resolutionState: 'resolved',
              },
              {
                id: 'qa-route',
                type: 'rule',
                label: 'qa-ingress',
                provider: 'kubernetes',
                protocol: 'HTTP',
                resolutionState: 'resolved',
              },
              {
                id: 'qa-service',
                type: 'service',
                label: 'qa-service',
                provider: 'kubernetes',
                resolutionState: 'resolved',
                resource: {
                  clusterId: 'cluster-a',
                  apiVersion: 'v1',
                  kind: 'Service',
                  namespace: 'monitoring',
                  name: 'qa-service',
                  uid: 'qa-service-uid',
                  scopeMode: 'namespace',
                },
              },
              {
                id: 'qa-unrelated',
                type: 'service',
                label: 'qa-unrelated',
                provider: 'kubernetes',
                resolutionState: 'resolved',
              },
            ],
            edges: [
              {
                id: 'qa-match',
                source: 'qa-entry',
                target: 'qa-route',
                relation: 'matches',
                protocol: 'HTTP',
                resolutionState: 'resolved',
              },
              {
                id: 'qa-backend',
                source: 'qa-route',
                target: 'qa-service',
                relation: 'weights',
                protocol: 'HTTP',
                resolutionState: 'resolved',
              },
            ],
            traces: [],
            coverage: [],
          } satisfies NetworkTopologyView,
        },
      }
    if (path === '/auth/login-options')
      return {
        body: { data: { localPasswordLoginEnabled: true, verification: { sliderEnabled: false } } },
      }
    if (path === '/auth/browser-handoffs/qa-handoff')
      return {
        body: {
          data: {
            status: 'pending',
            accountName: '交接样板',
            application: { id: 'qa-app', name: 'Soha Console' },
            expiresAt: '2099-01-01T00:00:00Z',
          },
        },
      }
    if (path === '/auth/profile')
      return {
        body: {
          data: {
            userId: 'qa-user',
            username: 'qa',
            displayName: '资料样板',
            email: 'qa@example.test',
            status: 'active',
            roles: [],
            teams: [],
            projects: [],
            tags: [],
            identities: [],
            sessions: [],
          },
        },
      }
    const item = cases.find(([, endpoint]) => endpoint === path)
    if (item) return { body: { data: [row(item[2])] } }
    if (extras.includes(path)) return { body: { data: [] } }
    return undefined
  })
  return { state, evidence }
}
for (const theme of ['light', 'dark']) {
  for (const [route, endpoint, name] of cases)
    test(`Task52 B6/B7 ${theme}: ${route} read and retry`, async ({ page, context }) => {
      await context.addInitScript(
        (theme) =>
          localStorage.setItem(
            'soha-prefs',
            JSON.stringify({ state: { themeMode: theme, localeCode: 'zh_CN' }, version: 0 }),
          ),
        theme,
      )
      const { state, evidence } = await mocks(context, page)
      state.failedPath = endpoint
      state.status = 503
      await page.goto(route)
      if (route === '/ai-workbench/memory')
        await page.getByRole('tab', { name: /^Records/ }).click()
      await expect(page.locator('.soha-read-feedback').first()).toBeVisible({ timeout: 12000 })
      state.status = 200
      await page
        .getByRole('button', { name: /重试读取/ })
        .first()
        .click()
      await expect(page.getByText(name, { exact: false }).first()).toBeVisible()
      await verifyGeometry(page, page.locator('.soha-pro-content-host').first())
      verifyBrowserEvidence(evidence)
    })
}

for (const mode of ['light-desktop', 'dark-narrow']) {
  test(`Task52 knowledge ${mode}: preserves draft and cached rows; unknown write stays fenced`, async ({
    page,
    context,
  }) => {
    if (mode === 'dark-narrow') {
      await page.setViewportSize({ width: 390, height: 844 })
      await context.addInitScript(() =>
        localStorage.setItem(
          'soha-prefs',
          JSON.stringify({ state: { themeMode: 'dark', localeCode: 'zh_CN' }, version: 0 }),
        ),
      )
    }
    const { state, evidence } = await mocks(context, page)
    await page.goto('/ai-workbench/knowledge')
    await expect(page.getByText('知识库样板', { exact: true }).first()).toBeVisible()
    state.failedPath = '/ai/knowledge-bases'
    state.status = 503
    await page.getByRole('button', { name: '刷新知识库', exact: true }).click()
    await expect(page.getByText('未更新，正在展示上次读取结果', { exact: false })).toBeVisible({
      timeout: 12000,
    })
    await expect(page.getByText('知识库样板', { exact: true }).first()).toBeVisible()
    await page.getByRole('button', { name: /新建知识库$/ }).click()
    const dialog = page.getByRole('dialog')
    await dialog.getByLabel('名称', { exact: true }).fill('保留原输入')
    await dialog.getByRole('button', { name: /^创\s*建$/ }).click()
    await expect(dialog.getByText('创建知识库：请求结果不明', { exact: true })).toBeVisible()
    await expect(dialog.getByLabel('名称', { exact: true })).toHaveValue('保留原输入')
    await expect(dialog.getByRole('button', { name: /^创\s*建$/ })).toBeDisabled()
    await expect(dialog.getByRole('button', { name: /^取\s*消$/ })).toBeDisabled()
    await page.keyboard.press('Escape')
    await expect(dialog).toBeVisible()
    expect(state.writes).toEqual([
      { path: '/ai/knowledge-bases', body: expect.objectContaining({ name: '保留原输入' }) },
    ])
    await verifyGeometry(page, dialog)
    await test.info().attach('operation-feedback', {
      body: await page.screenshot({ fullPage: true, animations: 'disabled' }),
      contentType: 'image/png',
    })
    verifyBrowserEvidence(evidence)
  })
}

test('Task52 knowledge permission loss hides cached records and clears private drafts', async ({
  page,
  context,
}) => {
  const { state, evidence } = await mocks(context, page)
  await page.goto('/ai-workbench/knowledge')
  await page.getByRole('button', { name: /新建知识库$/ }).click()
  await page.getByRole('dialog').getByLabel('描述', { exact: true }).fill('PRIVATE-DRAFT')
  await page
    .getByRole('dialog')
    .getByRole('button', { name: /^取\s*消$/ })
    .click()
  state.failedPath = '/ai/knowledge-bases'
  state.status = 403
  await page.getByRole('button', { name: '刷新知识库', exact: true }).click()
  await expect(page.getByText('无访问权限', { exact: false }).first()).toBeVisible()
  await expect(page.getByText('知识库样板', { exact: true })).toHaveCount(0)
  await expect(page.getByRole('button', { name: /新建知识库$/ })).toBeDisabled()
  expect(await page.content()).not.toContain('PRIVATE-DRAFT')
  expect(state.writes).toEqual([])
  verifyBrowserEvidence(evidence)
})

test('Task52 topology keyboard selection, relation highlight and viewport survive data refresh', async ({
  page,
  context,
}) => {
  const { state, evidence } = await mocks(context, page)
  await page.emulateMedia({ reducedMotion: 'reduce' })
  await page.goto('/network/topology?clusterId=cluster-a&namespace=monitoring')
  const node = page.locator('.react-flow__node').filter({ hasText: 'qa-service' }).first()
  const unrelated = page.locator('.react-flow__node').filter({ hasText: 'qa-unrelated' })
  await expect(node).toBeVisible()
  await node.focus()
  await page.keyboard.press('Enter')
  await expect(node).toHaveClass(/selected/)
  await expect(page.locator('.soha-topology-evidence')).toContainText('qa-service')
  await expect(unrelated).toHaveCSS('opacity', '0.4')
  const viewport = page.locator('.react-flow__viewport')
  const before = await viewport.getAttribute('style')
  await page.locator('.react-flow__controls-zoomin').click()
  await expect(viewport).not.toHaveAttribute('style', before || '')
  const zoomed = await viewport.getAttribute('style')
  state.failedPath = '/clusters/cluster-a/network/topology'
  state.status = 503
  await page.getByRole('button', { name: '刷新网络拓扑', exact: true }).click()
  await expect(page.locator('.soha-read-feedback')).toBeVisible()
  await expect(node).toHaveClass(/selected/)
  await expect(viewport).toHaveAttribute('style', zoomed || '')
  state.status = 200
  await page.getByRole('button', { name: /重试读取网络拓扑/ }).click()
  await expect(page.locator('.soha-read-feedback')).toHaveCount(0)
  await expect(node).toHaveClass(/selected/)
  await expect(viewport).toHaveAttribute('style', zoomed || '')
  await node.focus()
  await page.keyboard.press('Escape')
  await expect(node).not.toHaveClass(/selected/)
  await expect(page.locator('.soha-topology-evidence')).toHaveCount(0)
  await expect(unrelated).toHaveCSS('opacity', '1')
  // React Flow blurs an unselected node on the next animation frame.
  await expect(node).not.toBeFocused()
  await node.focus()
  await page.keyboard.press('Space')
  await expect(node).toHaveClass(/selected/)
  await expect(page.locator('.soha-topology-evidence')).toContainText('qa-service')
  await page.keyboard.press('Escape')
  await expect(node).not.toHaveClass(/selected/)
  await test.info().attach('topology-preview', {
    body: await page.screenshot({ fullPage: true, animations: 'disabled' }),
    contentType: 'image/png',
  })
  verifyBrowserEvidence(evidence)
})

for (const route of ['/auth/oidc/callback', '/login/callback'])
  test(`Task52 callback keeps a readable failure: ${route}`, async ({ page, context }) => {
    const { state, evidence } = await mocks(context, page)
    state.signedOut = true
    await page.goto(
      `${route}?error=access_denied&error_description=${encodeURIComponent('授权被拒绝，请重新登录')}`,
    )
    await expect(page.getByRole('alert')).toHaveText('授权被拒绝，请重新登录')
    await expect(page.getByRole('link', { name: '返回登录' })).toBeVisible()
    verifyBrowserEvidence(evidence)
  })

test('Task52 login uses compact Antd controls, readable option failures and reduced motion', async ({
  page,
  context,
}) => {
  const { state, evidence } = await mocks(context, page)
  state.signedOut = true
  state.failedPath = '/auth/login-options'
  state.status = 503
  await page.emulateMedia({ reducedMotion: 'reduce' })
  await page.goto('/login')
  await expect(page.locator('.soha-read-feedback').first()).toBeVisible()
  state.status = 200
  await page.getByRole('button', { name: /重试读取登录选项/ }).click()
  await expect(page.locator('.soha-read-feedback')).toHaveCount(0)
  await expect(page.getByLabel('用户名', { exact: true })).toBeEnabled()
  await expect(page.getByLabel('密码', { exact: true })).toBeEnabled()
  await page.getByLabel('用户名', { exact: true }).focus()
  await page.keyboard.press('Tab')
  await expect(page.getByLabel('密码', { exact: true })).toBeFocused()
  verifyBrowserEvidence(evidence)
})

test('Task52 handoff reads and retries without consuming the one-time login', async ({
  page,
  context,
}) => {
  const { state, evidence } = await mocks(context, page)
  state.signedOut = true
  state.failedPath = '/auth/browser-handoffs/qa-handoff'
  state.status = 503
  await page.goto('/auth/browser-handoff/qa-handoff')
  await expect(page.locator('.soha-read-feedback')).toBeVisible()
  state.status = 200
  await page.getByRole('button', { name: /重试读取登录交接/ }).click()
  await expect(page.getByText('交接样板', { exact: true })).toBeVisible()
  await expect(page.getByRole('button', { name: '继续登录', exact: true })).toBeEnabled()
  expect(state.writes).toEqual([])
  verifyBrowserEvidence(evidence)
})

for (const [route, target, label] of [
  ['/account/settings', '/account/profile', '资料样板'],
  ['/settings/about', '/about', '关于 OpenSoha'],
  ['/about', '/about', '关于 OpenSoha'],
])
  test(`Task52 final utility route: ${route}`, async ({ page, context }) => {
    const { evidence } = await mocks(context, page)
    await page.goto(route)
    await expect(page).toHaveURL(`http://127.0.0.1:4179${target}`)
    await expect(page.getByText(label, { exact: true }).first()).toBeVisible()
    verifyBrowserEvidence(evidence)
  })
