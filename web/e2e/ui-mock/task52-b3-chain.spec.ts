import { test, expect, type BrowserContext, type Page } from '@playwright/test'
import { installMocks, verifyBrowserEvidence } from '../fixtures/mock-app'
import { verifyGeometry } from '../fixtures/geometry'

async function chainMocks(context: BrowserContext, page: Page) {
  const state = {
    readStatus: 200,
    writes: 0,
    approvalWrites: 0,
    buildTemplateWrites: 0,
    buildTemplates: [
      {
        id: 'build-existing',
        key: 'docker-node',
        name: '已有构建模板',
        enabled: true,
        revision: 2,
        publicationState: 'draft',
        buildCommands: ['npm run build'],
        variableSchema: {},
        defaultVariables: {},
      },
    ],
  }
  const template = {
    id: 'b3-template',
    key: 'build-release',
    name: '构建与发布流程',
    revision: 2,
    publishedVersion: 1,
    publicationState: 'published',
    enabled: true,
    definition: {
      schemaVersion: 1,
      mode: 'delivery_batch',
      stages: ['build', 'plan', 'deploy', 'health'],
      executionMode: 'build_all_then_deploy',
      maxConcurrency: 4,
      stopOnFailure: true,
    },
  }
  const artifact = {
    id: 'image-b3',
    kind: 'image',
    name: '订单服务镜像',
    ref: 'registry.example.test/orders:v1.8.0',
    digest: 'sha256:' + 'a'.repeat(64),
    releaseBundleId: 'bundle-b3',
    executionTaskId: 'task-b3',
    status: 'available',
    createdAt: '2026-10-16T04:00:00Z',
  }
  const evidence = await installMocks(context, page, (url, method) => {
    const path = url.pathname
    if (path === '/api/v1/access/permission-snapshot')
      return {
        body: {
          data: {
            permissionKeys: [
              'workbench.delivery.view',
              'workbench.settings.view',
              'delivery.applications.view',
              'delivery.builds.view',
              'delivery.releases.view',
              'delivery.release-bundles.view',
              'delivery.execution-tasks.view',
              'delivery.workflows.view',
              'delivery.workflows.trigger',
              'delivery.application-environments.approve',
              'delivery.build-templates.view',
              'delivery.build-templates.create',
              'delivery.build-templates.update',
              'delivery.workflow-templates.view',
              'delivery.workflow-templates.create',
              'delivery.workflow-templates.update',
              'system.audit.view',
              'system.operations.view',
            ],
            visibleMenuIds: ['workflow-templates', 'execution-history', 'audit'],
            visibleMenus: [
              { id: 'workflow-templates', path: '/workflow-templates', labelZh: '流程模板' },
              { id: 'execution-history', path: '/execution-history', labelZh: '执行历史' },
              { id: 'audit', path: '/system/audit', labelZh: '审计日志' },
            ],
          },
        },
      }
    if (path === '/api/v1/build-templates' && method === 'POST') {
      state.buildTemplateWrites++
      return {
        status: 503,
        body: {
          error: {
            code: 'unavailable',
            message: '构建模板响应中断',
            request_id: 'b3-build-template-write',
          },
        },
      }
    }
    if (path === '/api/v1/build-templates') return { body: { data: state.buildTemplates } }
    if (path.startsWith('/api/v1/build-templates/') && path.endsWith('/usage'))
      return {
        body: {
          data: {
            usageCount: 0,
            applicationCount: 0,
            environmentCount: 0,
            productionEnvironmentCount: 0,
            approvalBindingCount: 0,
            targetCount: 0,
            riskLevel: 'low',
          },
        },
      }
    if (path === '/api/v1/workflow-templates') return { body: { data: [template] } }
    if (path === '/api/v1/workflow-templates/b3-template' && method === 'PUT') {
      state.writes++
      return {
        status: 503,
        body: {
          error: {
            code: 'unavailable',
            message: '保存响应中断，需核对草稿',
            request_id: 'b3-template-write',
          },
        },
      }
    }
    if (
      path === '/api/v1/workflow-templates/b3-template' ||
      path === '/api/v1/workflow-templates/b3-template/versions/1'
    )
      return { body: { data: template } }
    if (path.startsWith('/api/v1/delivery/documents/')) return { body: { data: {} } }
    if (path.startsWith('/api/v1/delivery/runtime/')) {
      if (state.readStatus !== 200)
        return {
          status: state.readStatus,
          body: {
            error: {
              code: 'read_failed',
              message: '执行证据暂时不可用',
              request_id: 'b3-runtime-read',
            },
          },
        }
      const bundle = path.includes('/release-bundles/')
      return {
        body: {
          data: {
            kind: bundle ? 'release_bundle' : path.includes('/releases/') ? 'release' : 'build',
            object: {
              id: bundle ? 'bundle-b3' : 'build-b3',
              applicationId: 'orders',
              version: 'v1.8.0',
              sourceType: 'build',
              sourceSystem: 'manual',
              artifactRef: artifact.ref,
              artifactDigest: artifact.digest,
              metadata: {},
              status: bundle ? 'available' : 'completed',
              createdAt: '2026-10-16T04:00:00Z',
            },
            application: { id: 'orders', key: 'orders', name: '订单服务', enabled: true },
            artifacts: [artifact],
            permissions: { canViewArtifacts: true, canViewAudit: true, canViewOperations: true },
          },
        },
      }
    }
    if (path === '/api/v1/audit/logs' || path === '/api/v1/operations/logs')
      return {
        body: {
          data: [
            {
              id: 'audit-b3',
              actorId: 'tester',
              actorName: '演示用户',
              action: 'delivery.build.completed',
              operationType: 'delivery.build',
              resourceKind: 'build',
              resourceName: 'build-b3',
              targetScope: { module: 'delivery' },
              summary: '本次构建制品已保存',
              result: 'success',
              metadata: { 'runtime.release_bundle.id': 'bundle-b3' },
              createdAt: '2026-10-16T04:00:00Z',
            },
          ],
        },
      }
    if (path === '/api/v1/delivery-batches/batch-b3')
      return {
        body: {
          data: {
            id: 'batch-b3',
            rootRunId: 'run-b3',
            status: 'running',
            definition: { name: '订单服务交付', targets: [] },
            targets: [],
            nodes: [],
            serviceCount: 1,
            targetCount: 1,
            buildCount: 1,
            createdAt: '2026-10-16T04:00:00Z',
          },
        },
      }
    if (path === '/api/v1/delivery/plans/plan-b3/approval') {
      state.approvalWrites++
      return {
        status: 503,
        body: {
          error: { code: 'unavailable', message: '审批响应中断', request_id: 'b3-plan-write' },
        },
      }
    }
    if (path === '/api/v1/delivery/plans/plan-b3')
      return {
        body: {
          data: {
            id: 'plan-b3',
            applicationId: 'orders',
            applicationName: '订单服务',
            applicationEnvironmentId: 'test',
            environmentKey: '集成测试',
            releaseBundleId: 'bundle-b3',
            status: 'waiting_approval',
            riskLevel: 'low',
            impact: {},
          },
        },
      }
  })
  return { state, evidence }
}

for (const mode of ['light', 'dark', 'narrow'] as const)
  test(`B3 build to artifact and audit chain (${mode})`, async ({ context, page }, info) => {
    if (mode === 'dark')
      await context.addInitScript(() =>
        localStorage.setItem(
          'soha-prefs',
          JSON.stringify({ state: { themeMode: 'dark' }, version: 0 }),
        ),
      )
    if (mode === 'narrow') await page.setViewportSize({ width: 390, height: 1000 })
    await page.emulateMedia({ reducedMotion: mode === 'narrow' ? 'reduce' : 'no-preference' })
    const { evidence } = await chainMocks(context, page)
    await page.goto('/builds/build-b3')
    if (mode === 'dark')
      await expect(page.locator('html')).toHaveAttribute('data-theme-mode', 'dark')
    await expect(page.getByRole('region', { name: '运行记录状态' })).toBeVisible()
    await expect(page.locator('body')).toContainText('sha256:' + 'a'.repeat(64))
    await verifyGeometry(page, page.getByRole('button', { name: /刷新证据/ }))
    await page.screenshot({ path: info.outputPath('b3-build.png'), fullPage: true })
    const bundleLink = page.getByRole('link', { name: '版本包', exact: true })
    if (mode === 'narrow') {
      await expect(page.locator('.soha-runtime-evidence')).toHaveAttribute(
        'data-motion-disabled',
        'true',
      )
      await bundleLink.focus()
      await page.keyboard.press('Enter')
    } else await bundleLink.click()
    await expect(page).toHaveURL(/delivery\/release-bundles\/bundle-b3$/)
    await expect(page.getByRole('region', { name: '运行记录状态' })).toContainText('v1.8.0')
    await page.screenshot({ path: info.outputPath('b3-artifact.png'), fullPage: true })
    await page.getByRole('button', { name: /审计日志/ }).click()
    await expect(page).toHaveURL(/metadataKey=runtime.release_bundle.id&metadataValue=bundle-b3/)
    await expect(page.locator('body')).toContainText('本次构建制品已保存')
    expect(
      evidence.requests.some(
        (path) => path.includes('/audit/logs?') && path.includes('metadataValue=bundle-b3'),
      ),
    ).toBe(true)
    await verifyGeometry(page, page.getByRole('button', { name: '刷新审计日志', exact: true }))
    await page.screenshot({ path: info.outputPath('b3-audit.png'), fullPage: true })
    verifyBrowserEvidence(evidence)
  })

test('B3 retains runtime evidence on 503 and clears it on 403', async ({ context, page }) => {
  const { state, evidence } = await chainMocks(context, page)
  await page.goto('/builds/build-b3')
  await expect(page.locator('body')).toContainText('registry.example.test/orders:v1.8.0')
  state.readStatus = 503
  await page.getByRole('button', { name: /刷新证据/ }).click()
  await expect(page.locator('body')).toContainText('上次读取结果')
  await expect(page.locator('body')).toContainText('registry.example.test/orders:v1.8.0')
  state.readStatus = 403
  await page.getByRole('button', { name: /刷新证据/ }).click()
  await expect(page.locator('body')).not.toContainText('registry.example.test/orders:v1.8.0')
  await expect(page.locator('body')).toContainText('b3-runtime-read')
  verifyBrowserEvidence(evidence)
})

test('B3 keeps a workflow draft after an uncertain save and allows an explicit read-only reload', async ({
  context,
  page,
}) => {
  const { state, evidence } = await chainMocks(context, page)
  await page.goto('/workflow-templates?templateId=b3-template')
  await page.getByRole('button', { name: /编辑草稿/ }).click()
  const dialog = page.getByRole('dialog')
  await dialog.getByRole('textbox', { name: /模板名称/ }).fill('保留中的交付草稿')
  await dialog.getByRole('button', { name: '保存草稿', exact: true }).click()
  await expect(dialog).toContainText('请求结果不明')
  await expect(dialog.getByRole('textbox', { name: /模板名称/ })).toHaveValue('保留中的交付草稿')
  await expect(dialog.getByRole('button', { name: '保存草稿', exact: true })).toBeDisabled()
  expect(state.writes).toBe(1)
  page.once('dialog', (dialog) => dialog.accept())
  await dialog.getByRole('button', { name: '重新加载草稿', exact: true }).click()
  await expect(page.getByRole('dialog').getByRole('textbox', { name: /模板名称/ })).toHaveValue(
    '构建与发布流程',
  )
  expect(state.writes).toBe(1)
  verifyBrowserEvidence(evidence)
})

test('B3 plan approval preserves the receipt and blocks a second decision after readback', async ({
  context,
  page,
}) => {
  const { state, evidence } = await chainMocks(context, page)
  await page.goto('/delivery/batches/batch-b3?planId=plan-b3')
  const drawer = page.getByRole('dialog', { name: '最终部署计划' })
  await expect(drawer).toContainText('查看已冻结产物')
  await drawer.getByRole('button', { name: '批准此计划', exact: true }).click()
  await expect(drawer).toContainText('b3-plan-write')
  await expect(drawer).toContainText('请求结果不明')
  await drawer.getByRole('button', { name: '刷新计划', exact: true }).click()
  await expect(drawer.getByRole('button', { name: /批准此计划$/ })).toBeDisabled()
  expect(state.approvalWrites).toBe(1)
  verifyBrowserEvidence(evidence)
})

for (const added of [false, true])
  test(`B3 does not mistake an existing template for an uncertain creation (new record=${added})`, async ({
    context,
    page,
  }) => {
    const { state, evidence } = await chainMocks(context, page)
    await page.goto('/build-templates')
    await expect(page.locator('body')).toContainText('已有构建模板')
    await page.getByRole('button', { name: '新建模板', exact: true }).click()
    await page.getByRole('textbox', { name: /模板 Key/ }).fill(added ? 'new-docker' : 'docker-node')
    await page.getByRole('textbox', { name: /模板名称/ }).fill('本次创建草稿')
    await page.getByRole('button', { name: /保存草稿/ }).click()
    await expect(page.locator('body')).toContainText('请求结果不明')
    if (added)
      state.buildTemplates.push({
        ...state.buildTemplates[0],
        id: 'build-new',
        key: 'new-docker',
        name: '本次创建草稿',
      })
    page.once('dialog', (dialog) => dialog.accept())
    await page.getByRole('button', { name: '刷新', exact: true }).click()
    if (added) {
      await expect(page).toHaveURL(/templateId=build-new/)
      await expect(page.getByRole('button', { name: /保存草稿/ })).toBeEnabled()
      await expect(page.locator('body')).not.toContainText('请求结果不明')
    } else {
      await expect(page.getByRole('button', { name: /保存草稿/ })).toBeDisabled()
      await expect(page.getByRole('textbox', { name: /模板名称/ })).toHaveValue('本次创建草稿')
      await expect(page.locator('body')).toContainText('请求结果不明')
    }
    expect(state.buildTemplateWrites).toBe(1)
    verifyBrowserEvidence(evidence)
  })
