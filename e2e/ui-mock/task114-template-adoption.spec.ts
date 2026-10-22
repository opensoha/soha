import { test, expect, type BrowserContext, type Page } from '@playwright/test'
import type {
  TemplateUpdatePreviewInput,
  TemplateUpdateBatchInput,
  TemplateUpdateTarget,
  TemplateUpdatePreview,
} from '@opensoha/contracts/gen/ts/sohaapi'
import { installMocks, verifyBrowserEvidence } from '../fixtures/mock-app'
import { verifyGeometry } from '../fixtures/geometry'

const now = '2026-10-22T00:00:00Z'
const template = {
  id: 'http',
  key: 'http',
  name: '标准应用配置',
  enabled: true,
  publishedVersion: 4,
  publicationState: 'published',
  revision: 4,
  createdAt: now,
  updatedAt: now,
  source: { renderer: 'raw_yaml', files: [] },
  parameterSchema: { type: 'object', properties: { replicas: { type: 'integer' } } },
  defaults: { replicas: 4 },
  artifacts: {},
  health: { mode: 'configuration_only', timeoutSeconds: 300 },
}
const targets: TemplateUpdateTarget[] = Array.from({ length: 10 }, (_, i) => ({
  id: `target-${i}`,
  kind: 'deployment',
  templateId: 'http',
  applicationId: `app-${Math.floor(i / 4)}`,
  applicationName: ['零售业务', '实时计算', '基础服务'][Math.floor(i / 4)],
  serviceId: `svc-${i}`,
  serviceName: `服务 ${i + 1}`,
  applicationEnvironmentId: i === 7 ? 'prod' : 'test',
  environmentName: i === 7 ? '生产环境' : '测试环境',
  releaseTargetId: `release-${i}`,
  fromVersion: 1,
  mode: 'fixed',
  requiresApproval: i === 7,
  eligible: i !== 9,
  reason: i === 9 ? '独立副本不参与模板传播' : '',
  independentOverrides: ['environment.replicas'],
  bindingRevision: `revision-${i}`,
  scope: 'environment',
}))

async function adoptionMocks(context: BrowserContext, page: Page, writable = true) {
  const state = {
    previews: [] as TemplateUpdatePreviewInput[],
    batches: [] as TemplateUpdateBatchInput[],
    policyWrites: [] as Record<string, unknown>[],
    failRead: false,
    failBatch: false,
    previewError: 0,
  }
  let frozen: TemplateUpdatePreview | undefined
  const evidence = await installMocks(context, page, (url, method, request) => {
    const path = url.pathname.replace('/api/v1', '')
    const data = (value: unknown) => ({ body: { data: value } })
    if (path === '/access/permission-snapshot')
      return data({
        permissionKeys: [
          'workbench.delivery.view',
          'delivery.deployment-templates.view',
          ...(writable
            ? ['delivery.application-environments.update', 'delivery.workflows.trigger']
            : []),
        ],
        visibleMenuIds: ['deployment-templates'],
        visibleMenus: [
          { id: 'deployment-templates', path: '/deployment-templates', labelZh: '部署模板' },
        ],
      })
    if (path === '/deployment-templates') return data([template])
    if (path === '/deployment-templates/http/versions')
      return data([1, 2, 3, 4].map((publishedVersion) => ({ ...template, publishedVersion })))
    if (path === '/delivery/template-updates/targets')
      return state.failRead
        ? {
            status: 503,
            body: {
              error: {
                code: 'unavailable',
                message: '暂时无法读取应用绑定',
                request_id: 'targets-read',
              },
            },
          }
        : data({ items: targets, total: 10, page: 1, pageSize: 20 })
    if (path === '/delivery/template-updates/policies' && method === 'GET') return data([])
    if (path === '/delivery/template-updates/policies' && method === 'PUT') {
      const input = request.postDataJSON()
      state.policyWrites.push(input)
      return data({ ...input, id: 'policy', version: 1, lastError: '', updatedAt: now })
    }
    if (path === '/delivery/template-updates/previews' && method === 'POST') {
      const input = request.postDataJSON() as TemplateUpdatePreviewInput
      state.previews.push(input)
      if (state.previewError)
        return {
          status: state.previewError,
          body: {
            error: {
              code: state.previewError === 403 ? 'forbidden' : 'unavailable',
              message: '无法重新比较模板',
              request_id: 'preview-retry',
            },
          },
        }
      const sequence = state.previews.length
      frozen = {
        id: `preview-${sequence}`,
        kind: 'deployment',
        templateId: 'http',
        toVersion: input.toVersion,
        digest: `digest-${sequence}`,
        configurationOnly: true,
        createdAt: now,
        expiresAt: '2026-10-23T00:00:00Z',
        candidates: input.targets.map((selection, i) => {
          const target = targets.find((item) => item.id === selection.targetId)!
          const changes = [
            { section: 'defaults' as const, path: 'replicas', before: '1', after: '4' },
          ]
          return {
            id: `candidate-${sequence}-${i}`,
            target,
            toVersion: 4,
            digest: `candidate-digest-${sequence}-${i}`,
            parameters: selection.parameters ?? { replicas: 1 },
            eligible: true,
            reason: '',
            requiresApproval: target.requiresApproval,
            wave: 0,
            cumulative: {
              fromVersion: 1,
              toVersion: 4,
              changes,
              renderStatus: 'available',
              diagnostics: [],
              historyGap: false,
            },
            versions: [1, 2, 3].map((fromVersion) => ({
              fromVersion,
              toVersion: fromVersion + 1,
              changes,
              renderStatus: 'available' as const,
              diagnostics: [],
              historyGap: false,
            })),
          }
        }),
      }
      return data(frozen)
    }
    if (
      /^\/delivery\/template-updates\/previews\/preview-\d+\/batches$/.test(path) &&
      method === 'POST'
    ) {
      state.batches.push(request.postDataJSON())
      return state.failBatch
        ? {
            status: 409,
            body: {
              error: {
                code: 'conflict',
                message: '绑定已变化，请重新比较',
                request_id: 'adoption-stale',
              },
            },
          }
        : data({ id: 'batch-adoption' })
    }
  })
  return { state, evidence }
}

async function openAdoption(page: Page) {
  await page.goto('/deployment-templates')
  await page.getByRole('button', { name: '检查模板更新', exact: true }).click()
  const drawer = page.getByRole('dialog', { name: '模板采用 · http', exact: true })
  await expect(drawer.getByText('独立副本不参与模板传播', { exact: true })).toBeVisible()
  return drawer
}

test('manual adoption preserves exclusions after recompare, adjacent diffs and waves', async ({
  context,
  page,
}) => {
  const { state, evidence } = await adoptionMocks(context, page)
  const drawer = await openAdoption(page)
  const checks = drawer.locator('tbody input[type=checkbox]')
  await expect(checks).toHaveCount(10)
  await expect(checks.nth(9)).toBeDisabled()
  await checks.nth(0).check()
  await checks.nth(1).check()
  await checks.nth(7).check()
  let releaseInitial!: () => void
  const initialRequest = new Promise<void>((resolve) => {
    releaseInitial = resolve
  })
  let initialPending = false
  await context.route('**/delivery/template-updates/previews', async (route) => {
    if (state.previews.length === 0) {
      initialPending = true
      await initialRequest
    }
    await route.fallback()
  })
  await drawer.getByRole('button', { name: '比较所选 3 项', exact: true }).click()
  await expect.poll(() => initialPending).toBe(true)
  await expect(drawer.getByRole('combobox', { name: '目标发布版本', exact: true })).toBeDisabled()
  await expect(checks.nth(0)).toBeDisabled()
  releaseInitial()
  await expect(drawer.getByRole('tab', { name: '累计差异 v1 → v4', exact: true })).toBeVisible()
  await drawer.getByRole('tab', { name: '逐版本差异（3 段）', exact: true }).click()
  await drawer.getByRole('combobox', { name: '版本差异段', exact: true }).click()
  await page.getByTitle('v3 → v4', { exact: true }).click()
  await expect(drawer.getByText('采用前 · v3', { exact: true })).toBeVisible()
  const segment = drawer.getByRole('tabpanel', { name: '逐版本差异（3 段）', exact: true })
  await expect(segment.locator('.soha-yaml-pane').first().locator('.view-lines')).toHaveText('1')
  await expect(
    segment
      .locator('.soha-yaml-pane')
      .last()
      .locator('.editor.modified .lines-content > .view-lines'),
  ).toHaveText('4')
  const excluded = () =>
    drawer
      .getByRole('row')
      .filter({ hasText: '零售业务 / 测试环境 / 服务 2' })
      .getByRole('checkbox')
  await excluded().uncheck()
  await drawer.getByText('调整此项变量（保留现有覆盖）', { exact: true }).click()
  await drawer.getByRole('textbox', { name: '采用版本变量', exact: true }).fill('{"replicas":2}')
  await expect(
    drawer.getByRole('button', { name: '分批采用 2 项配置', exact: true }),
  ).toBeDisabled()
  let releasePreview!: () => void
  const pendingPreview = new Promise<void>((resolve) => {
    releasePreview = resolve
  })
  let comparing = false
  await context.route('**/delivery/template-updates/previews', async (route) => {
    comparing = true
    await pendingPreview
    await route.fallback()
  })
  await drawer.getByRole('button', { name: '重新生成差异', exact: true }).click()
  await expect.poll(() => comparing).toBe(true)
  await expect(
    drawer.getByRole('button', { name: '分批采用 2 项配置', exact: true }),
  ).toBeDisabled()
  await expect(excluded()).toBeDisabled()
  await expect(drawer.getByRole('textbox', { name: '采用版本变量', exact: true })).toBeDisabled()
  releasePreview()
  await expect(drawer.getByRole('button', { name: '分批采用 2 项配置', exact: true })).toBeEnabled()
  expect(state.previews[1].targets[0].parameters).toEqual({ replicas: 2 })
  await expect(excluded()).not.toBeChecked()
  const submit = drawer.getByRole('button', { name: '分批采用 2 项配置', exact: true })
  await verifyGeometry(page, submit)
  await submit.focus()
  await submit.press('Enter')
  await expect(drawer.getByText('批次已创建', { exact: true })).toBeVisible()
  expect(state.batches).toHaveLength(1)
  expect(state.batches[0]).toMatchObject({
    previewDigest: 'digest-2',
    candidateIds: ['candidate-2-0', 'candidate-2-2'],
    waveSize: 2,
    stopOnFailure: true,
  })
  verifyBrowserEvidence(evidence)
})

test('read failures retry without writes and readonly users cannot submit', async ({
  context,
  page,
}) => {
  const { state, evidence } = await adoptionMocks(context, page, false)
  state.failRead = true
  await page.goto('/deployment-templates')
  await page.getByRole('button', { name: '检查模板更新', exact: true }).click()
  const drawer = page.getByRole('dialog', { name: '模板采用 · http' })
  await expect(drawer).toContainText('暂时无法读取应用绑定')
  state.failRead = false
  await drawer.getByRole('button', { name: /重试/ }).click()
  await drawer.locator('tbody input[type=checkbox]').first().check()
  await drawer.getByRole('button', { name: '比较所选 1 项', exact: true }).click()
  await expect(
    drawer.getByRole('button', { name: '分批采用 1 项配置', exact: true }),
  ).toBeDisabled()
  expect(state.batches).toHaveLength(0)
  expect(state.policyWrites).toHaveLength(0)
  verifyBrowserEvidence(evidence)
})

test('policy is explicit, stale adoption retains the frozen comparison', async ({
  context,
  page,
}) => {
  const { state, evidence } = await adoptionMocks(context, page)
  state.failBatch = true
  const drawer = await openAdoption(page)
  const policy = drawer.getByRole('combobox', { name: '服务 1采用策略', exact: true })
  await policy.click()
  await page.getByTitle('自动更新', { exact: true }).click()
  await drawer.getByRole('button', { name: '保存策略', exact: true }).first().click()
  await expect.poll(() => state.policyWrites.length).toBe(1)
  expect(state.policyWrites[0]).toMatchObject({
    targetId: 'target-0',
    mode: 'automatic',
    expectedVersion: 0,
  })
  await drawer.locator('tbody input[type=checkbox]').first().check()
  await drawer.getByRole('button', { name: '比较所选 1 项', exact: true }).click()
  await drawer.getByRole('button', { name: '分批采用 1 项配置', exact: true }).click()
  await expect(drawer).toContainText('绑定已变化，请重新比较')
  await expect(drawer.getByRole('tab', { name: '累计差异 v1 → v4', exact: true })).toBeVisible()
  expect(state.batches).toHaveLength(1)
  verifyBrowserEvidence(evidence)
})

test('failed recompare retains the frozen diff, permission denial clears it', async ({
  context,
  page,
}) => {
  const { state, evidence } = await adoptionMocks(context, page)
  const drawer = await openAdoption(page)
  await drawer.locator('tbody input[type=checkbox]').first().check()
  await drawer.getByRole('button', { name: '比较所选 1 项', exact: true }).click()
  const cumulative = drawer.getByRole('tab', { name: '累计差异 v1 → v4', exact: true })
  await expect(cumulative).toBeVisible()
  state.previewError = 503
  await drawer.getByRole('button', { name: '重新生成差异', exact: true }).click()
  await expect(drawer).toContainText('无法重新比较模板')
  await expect(cumulative).toBeVisible()
  await expect(
    drawer.getByRole('button', { name: '分批采用 1 项配置', exact: true }),
  ).toBeDisabled()
  state.previewError = 403
  await drawer.getByRole('button', { name: '重新生成差异', exact: true }).click()
  await expect(cumulative).toHaveCount(0)
  expect(state.batches).toHaveLength(0)
  verifyBrowserEvidence(evidence)
})
