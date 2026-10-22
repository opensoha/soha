import { test, expect } from '@playwright/test'
import { adoptionMocks, openAdoption } from '../fixtures/template-adoption'
import { installMocks, verifyBrowserEvidence } from '../fixtures/mock-app'
import { verifyGeometry } from '../fixtures/geometry'

const migration = {
  fromVersion: 1,
  toVersion: 4,
  blocked: true,
  requiresApproval: true,
  reason: 'v2 要求按相邻版本发布，请先完成 v2；审批不能跳过该边界',
  steps: [
    {
      version: 2,
      contentDigest: 'digest2',
      policy: {
        sequential: true,
        irreversible: true,
        guide: '先备份，再逐版发布；不可逆变更只允许前向修复',
      },
    },
  ],
}

test('migration jump is visible in cumulative diff and cannot create an adoption batch', async ({
  context,
  page,
}, testInfo) => {
  const { state, evidence } = await adoptionMocks(context, page)
  state.migration = migration
  const drawer = await openAdoption(page)
  await drawer.locator('tbody input[type=checkbox]').first().check()
  await drawer.getByRole('button', { name: '比较所选 1 项', exact: true }).click()
  await expect(drawer.getByText(migration.reason, { exact: true }).last()).toBeVisible()
  await expect(drawer.getByText(migration.steps[0].policy.guide, { exact: true })).toBeVisible()
  await expect(drawer.getByRole('button', { name: /分批采用/ })).toBeDisabled()
  expect(state.batches).toHaveLength(0)
  await page.screenshot({ path: testInfo.outputPath('migration-jump.png') })
  verifyBrowserEvidence(evidence)
})

test('template migration form validates the guide and retains declarations in saved input', async ({
  context,
  page,
}) => {
  const { state, evidence } = await adoptionMocks(context, page)
  await page.goto('/deployment-templates')
  await page.getByRole('button', { name: '标准应用配置', exact: true }).click()
  const dialog = page.getByRole('dialog', { name: '标准应用配置', exact: true })
  const sequential = dialog.getByRole('switch', { name: '必须按相邻版本发布', exact: true })
  await expect(sequential).toBeEnabled()
  await sequential.click()
  await dialog.getByRole('switch', { name: '不可逆数据变更', exact: true }).click()
  await dialog.getByRole('button', { name: '保存草稿', exact: true }).click()
  await expect(
    dialog.getByText('请填写迁移前提、验证方式和恢复方案', { exact: true }),
  ).toBeVisible()
  expect(state.templateWrites).toHaveLength(0)
  await dialog
    .getByRole('textbox', { name: '迁移与恢复指引', exact: true })
    .fill(' 核实备份，逐版执行并验证；前向修复 ')
  await dialog.getByRole('button', { name: '保存草稿', exact: true }).click()
  await expect.poll(() => state.templateWrites.length).toBe(1)
  expect(state.templateWrites[0]).toMatchObject({
    expectedRevision: 4,
    migrationPolicy: {
      sequential: true,
      irreversible: true,
      guide: '核实备份，逐版执行并验证；前向修复',
    },
  })
  verifyBrowserEvidence(evidence)
})

test('migration plan approval requires a comment and submits only the existing approval action', async ({
  context,
  page,
}, testInfo) => {
  const decisions: unknown[] = []
  let approved = false
  let blocked = true
  const assessment = {
    ...migration,
    blocked: false,
    toVersion: 2,
    reason: '迁移变更需要 Plan 审批及明确审批意见',
  }
  const plan = () => ({
    id: 'migration-plan',
    source: 'delivery_batch',
    status: approved ? 'draft' : 'waiting_approval',
    applicationId: 'retail',
    applicationEnvironmentId: 'retail-test',
    action: 'deploy',
    riskLevel: 'high',
    requiresApproval: true,
    createdAt: '2026-10-23T00:00:00Z',
    updatedAt: '2026-10-23T00:00:00Z',
    manifestSnapshots: [
      {
        migration: { ...assessment, blocked },
        deliveryPlanId: 'migration-plan',
        targetId: 'target',
        packageId: 'package',
        bindingId: 'binding',
        bindingVersion: 1,
        applicationEnvironmentId: 'retail-test',
        clusterId: 'cluster',
        namespace: 'test',
        revision: 2,
        revisionDigest: 'revision2',
        packageUpdatedAt: '2026-10-23T00:00:00Z',
        rendererVersion: 'raw-yaml/v1',
        inputDigest: 'input',
        renderedDigest: 'rendered',
        expectedGeneration: 1,
        preflightTaskId: 'preflight',
        documents: [],
      },
    ],
  })
  const evidence = await installMocks(context, page, (url, method, request) => {
    const path = url.pathname.replace('/api/v1', '')
    const data = (value: unknown) => ({ body: { data: value } })
    if (path === '/access/permission-snapshot')
      return data({
        permissionKeys: [
          'workbench.delivery.view',
          'delivery.workflows.view',
          'delivery.application-environments.approve',
        ],
        visibleMenuIds: ['delivery-batches'],
        visibleMenus: [{ id: 'delivery-batches', path: '/delivery/batches', labelZh: '交付记录' }],
      })
    if (path === '/delivery-batches/migration')
      return data({
        id: 'migration',
        rootRunId: 'run',
        status: 'running',
        definition: { name: '顺序迁移批次', targets: [] },
        targets: [],
        nodes: [],
        serviceCount: 0,
        targetCount: 0,
        buildCount: 0,
        createdBy: 'tester',
        createdAt: '2026-10-23T00:00:00Z',
        updatedAt: '2026-10-23T00:00:00Z',
      })
    if (path === '/delivery/plans/migration-plan' && method === 'GET') return data(plan())
    if (path === '/delivery/plans/migration-plan/approval' && method === 'POST') {
      decisions.push(request.postDataJSON())
      approved = true
      return data(plan())
    }
  })
  await page.goto('/delivery/batches/migration?planId=migration-plan')
  const drawer = page.getByRole('dialog', { name: '最终部署计划', exact: true })
  await expect(drawer.getByText(migration.steps[0].policy.guide, { exact: true })).toBeVisible()
  const approve = drawer.getByRole('button', { name: '批准此计划', exact: true })
  await expect(approve).toBeDisabled()
  await drawer.getByRole('textbox', { name: '迁移审批意见', exact: true }).fill('  ')
  await expect(approve).toBeDisabled()
  await drawer
    .getByRole('textbox', { name: '迁移审批意见', exact: true })
    .fill('已核实备份、逐版计划与恢复方案')
  await expect(approve).toBeDisabled()
  blocked = false
  await drawer.getByRole('button', { name: '刷新计划', exact: true }).click()
  await expect(approve).toBeEnabled()
  await verifyGeometry(page, approve)
  await page.screenshot({ path: testInfo.outputPath('migration-approval.png') })
  await approve.focus()
  await approve.press('Enter')
  await expect.poll(() => decisions.length).toBe(1)
  expect(decisions[0]).toEqual({ action: 'approve', comment: '已核实备份、逐版计划与恢复方案' })
  verifyBrowserEvidence(evidence)
})
