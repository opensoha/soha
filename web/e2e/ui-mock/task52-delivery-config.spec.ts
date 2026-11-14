import { test, expect, type BrowserContext, type Page } from '@playwright/test'
import { installMocks, verifyBrowserEvidence } from '../fixtures/mock-app'
import { verifyGeometry } from '../fixtures/geometry'

const route =
  '/applications/app-motion/workflows/design?bindingId=env-motion&source=template&templateId=template-motion&name=订单服务发布'
const workflowPath = '/api/v1/applications/app-motion/application-environments/env-motion/workflow'
const definition = {
  schemaVersion: 2,
  mode: 'release_dag',
  nodes: [
    {
      id: 'approval',
      type: 'manual_approval',
      name: '人工审批',
      position: { x: 120, y: 120 },
      timeoutSeconds: 300,
      continueOnFailure: false,
      config: {},
    },
  ],
  edges: [],
}

async function designerMocks(context: BrowserContext, page: Page) {
  const state = { readFailure: false, saveFailure: 409, reads: 0, writes: 0 }
  const application = {
    id: 'app-motion',
    name: '订单服务',
    key: 'orders',
    createdAt: '2026-10-16T00:00:00Z',
  }
  const template = { id: 'template-motion', name: '标准发布', publishedVersion: 1, definition }
  const binding = {
    id: 'env-motion',
    applicationId: application.id,
    environmentId: 'test',
    alias: '集成测试',
  }
  const evidence = await installMocks(context, page, (url, method) => {
    const path = url.pathname
    if (path === '/api/v1/access/permission-snapshot')
      return {
        body: {
          data: {
            permissionKeys: [
              'workbench.delivery.view',
              'delivery.applications.view',
              'delivery.workflows.view',
              'delivery.application-environments.update',
            ],
            visibleMenuIds: ['applications'],
            visibleMenus: [{ id: 'applications', path: '/applications', labelZh: '应用' }],
          },
        },
      }
    if (path === '/api/v1/applications') return { body: { data: [application] } }
    if (path === '/api/v1/applications/app-motion/detail')
      return { body: { data: { application, environments: [binding], services: [] } } }
    if (path === '/api/v1/applications/app-motion/runtime')
      return {
        body: {
          data: {
            environments: [
              {
                applicationEnvironmentId: binding.id,
                environmentId: 'test',
                environmentName: '集成测试',
                services: [],
              },
            ],
          },
        },
      }
    if (path === '/api/v1/delivery/environments')
      return { body: { data: [{ id: 'test', name: '集成测试' }] } }
    if (path === '/api/v1/application-environments') {
      state.reads++
      return state.readFailure
        ? {
            status: 503,
            body: {
              error: {
                code: 'temporarily_unavailable',
                message: '环境读取暂时不可用',
                request_id: 'config-read-demo',
              },
            },
          }
        : { body: { data: [binding] } }
    }
    if (path === '/api/v1/workflow-templates') return { body: { data: [template] } }
    if (path === '/api/v1/workflow-templates/template-motion/versions/1')
      return { body: { data: template } }
    if (path === workflowPath && method === 'PUT') {
      state.writes++
      return {
        status: state.saveFailure,
        body: {
          error: {
            code: 'save_failed',
            message:
              state.saveFailure === 409 ? '工作流版本已变化，请保留草稿并核对' : '保存响应未能确认',
            request_id: 'config-save-demo',
          },
        },
      }
    }
  })
  return { state, evidence }
}

for (const viewport of [
  { width: 1440, height: 1000 },
  { width: 390, height: 844 },
]) {
  test(`workflow configuration retains its draft and feedback at ${viewport.width}px`, async ({
    context,
    page,
  }, testInfo) => {
    await page.setViewportSize(viewport)
    await page.emulateMedia({ reducedMotion: 'reduce' })
    const { state, evidence } = await designerMocks(context, page)
    await page.goto(route)
    const name = page.getByRole('textbox', { name: '工作流名称', exact: true })
    await expect(name).toHaveValue('订单服务发布')
    await expect(page.locator('.react-flow__node')).toHaveCount(1)
    await expect(page.getByText('未保存', { exact: true })).toBeVisible()
    await name.fill('订单服务 · 审批调整')
    const save = page.getByRole('button', { name: '保存工作流', exact: true })
    await save.focus()
    await expect(save).toBeFocused()
    await verifyGeometry(page, save)
    await save.press('Enter')
    const feedback = page.locator('.soha-operation-feedback')
    await expect(feedback).toContainText('操作请求失败')
    await expect(feedback).toContainText('集成测试 · 订单服务 · 审批调整')
    await expect(feedback).toContainText('config-save-demo')
    await name.fill('继续编辑的草稿')
    await expect(feedback).toContainText('集成测试 · 订单服务 · 审批调整')
    await expect(feedback).not.toContainText('继续编辑的草稿')
    await expect(page.locator('.react-flow__node')).toHaveCount(1)
    expect(state.writes).toBe(1)
    await page.screenshot({
      path: testInfo.outputPath(`workflow-config-${viewport.width}.png`),
      fullPage: true,
    })
    verifyBrowserEvidence(evidence)
  })
}

test('workflow configuration retries an initial dependency failure', async ({ context, page }) => {
  const { state, evidence } = await designerMocks(context, page)
  state.readFailure = true
  await page.goto(route)
  const feedback = page.locator('.soha-read-feedback')
  await expect(feedback).toContainText('应用环境读取失败')
  await expect(page.locator('.react-flow__node')).toHaveCount(0)
  state.readFailure = false
  await page.getByRole('button', { name: '重试读取应用环境', exact: true }).click()
  await expect(page.locator('.react-flow__node')).toHaveCount(1)
  expect(state.writes).toBe(0)
  verifyBrowserEvidence(evidence)
})

test('an unknown workflow save blocks resubmission and checks records with GET only', async ({
  context,
  page,
}, testInfo) => {
  const { state, evidence } = await designerMocks(context, page)
  state.saveFailure = 503
  await page.goto(route)
  const save = page.getByRole('button', { name: '保存工作流', exact: true })
  await expect(page.locator('.react-flow__node')).toHaveCount(1)
  await save.click()
  await expect(page.locator('.soha-operation-feedback')).toContainText('请求结果不明')
  await expect(save).toBeDisabled()
  const reads = state.reads
  await page.getByRole('button', { name: '核对环境记录', exact: true }).click()
  await expect.poll(() => state.reads).toBe(reads + 1)
  await expect(save).toBeDisabled()
  await expect(page.getByRole('textbox', { name: '工作流名称', exact: true })).toHaveValue(
    '订单服务发布',
  )
  expect(state.writes).toBe(1)
  await page.screenshot({
    path: testInfo.outputPath('workflow-config-unknown.png'),
    fullPage: true,
  })
  verifyBrowserEvidence(evidence)
})
