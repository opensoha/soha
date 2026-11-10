import { test, expect } from '@playwright/test'
import { verifyBrowserEvidence } from '../fixtures/mock-app'
import { verifyGeometry } from '../fixtures/geometry'
import { adoptionMocks, openAdoption } from '../fixtures/template-adoption'

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
