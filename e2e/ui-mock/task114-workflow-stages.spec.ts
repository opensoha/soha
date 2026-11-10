import { test, expect } from '@playwright/test'
import { installMocks, verifyBrowserEvidence } from '../fixtures/mock-app'
import { task114Response } from '../fixtures/task114-delivery'

for (const width of [1440, 390]) {
  test(`Task114 recorded workflow stages and evidence at ${width}px`, async ({
    context,
    page,
  }, testInfo) => {
    let deployed = false
    let canViewWorkflows = true
    const run = () => ({
      id: 'retail-run',
      applicationId: 'retail',
      applicationEnvironmentId: 'retail-test',
      workflowName: '零售交付',
      status: 'running',
      createdAt: '2026-10-21T08:00:00Z',
      updatedAt: '2026-10-21T08:01:00Z',
      steps: [],
      nodeRuns: [
        { nodeId: 'build', name: '构建镜像', type: 'build', status: 'succeeded' },
        {
          nodeId: 'deploy / api',
          name: '部署服务',
          type: 'deploy',
          status: deployed ? 'succeeded' : 'running',
          summary: '镜像已提交，等待控制器就绪',
        },
        {
          nodeId: 'approve',
          name: '生产确认',
          type: 'manual_approval',
          status: 'waiting_approval',
        },
        { nodeId: 'unreported', name: '业务验证', type: 'verify', status: 'provider_unknown' },
        { nodeId: 'parallel', name: '并行检查', type: 'check', status: 'running' },
      ],
    })
    const binding = () => ({
      applicationEnvironmentId: 'retail-test',
      environmentId: 'test',
      environmentName: '测试环境',
      workflowTemplateId: 'retail-template',
      workflowTemplateName: '零售交付',
      targetCount: 0,
      requiresApproval: false,
      latestWorkflow: run(),
    })
    await page.setViewportSize({ width, height: 1000 })
    if (width === 390)
      await page.addInitScript(() =>
        localStorage.setItem(
          'soha-prefs',
          JSON.stringify({ state: { themeMode: 'dark', localeCode: 'zh_CN' }, version: 0 }),
        ),
      )
    const evidence = await installMocks(context, page, (url, method) => {
      const path = url.pathname.replace(/^\/api\/v1/, '')
      if (method === 'GET' && path === '/delivery/runtime/workflows/retail-run')
        return {
          body: {
            data: {
              kind: 'workflow',
              id: 'retail-run',
              object: run(),
              application: { id: 'retail', key: 'retail', name: '零售业务' },
              binding: binding(),
              artifacts: [],
            },
          },
        }
      if (method === 'GET' && path === '/delivery-batches') return { body: { data: [] } }
      const result = task114Response(url, method)
      if (result && path === '/access/permission-snapshot') {
        const body = result.body as { data: { permissionKeys: string[] } }
        return {
          ...result,
          body: {
            data: {
              ...body.data,
              permissionKeys: [
                ...body.data.permissionKeys,
                ...(canViewWorkflows ? ['delivery.workflows.view'] : []),
              ],
            },
          },
        }
      }
      if (result && path === '/applications/retail/detail') {
        const body = result.body as { data: Record<string, unknown> }
        return { ...result, body: { data: { ...body.data, bindings: [binding()] } } }
      }
      return result
    })
    page.on('pageerror', (error) => evidence.fatal.push(error.message))
    const appUrl = '/applications/retail?tab=delivery&applicationEnvironmentId=retail-test'
    await page.goto(appUrl)
    await page.getByText('环境工作流', { exact: true }).click()
    const stages = page.getByRole('list', { name: '工作流步骤', exact: true })
    await expect(stages.getByRole('listitem')).toHaveCount(5)
    await expect(stages.locator('.soha-workflow-stage-connector')).toHaveCount(4)
    await expect
      .poll(async () => {
        const [first, second] = await Promise.all([
          stages.getByRole('listitem').nth(0).boundingBox(),
          stages.getByRole('listitem').nth(1).boundingBox(),
        ])
        if (!first || !second || first.height >= 180) return false
        return width > 767
          ? Math.abs(first.y - second.y) < 2 && second.x > first.x + first.width
          : Math.abs(first.x - second.x) < 2 && second.y > first.y + first.height
      })
      .toBe(true)
    await expect(
      stages.getByRole('button', { name: '生产确认，等待审批', exact: true }),
    ).toBeVisible()
    await expect(stages).toContainText('状态未知 · provider_unknown')
    const deploy = stages.getByRole('button', { name: '部署服务，执行中', exact: true })
    await expect(deploy).toBeVisible()
    await page.emulateMedia({ reducedMotion: 'reduce' })
    await expect(stages.locator('.soha-execution-live-dot')).toHaveCount(0)
    await page.emulateMedia({ reducedMotion: 'no-preference' })
    await expect(stages.locator('.soha-execution-live-dot')).toHaveCount(2)
    await page.evaluate(() => {
      Object.defineProperty(document, 'hidden', { value: true, configurable: true })
      document.dispatchEvent(new Event('visibilitychange'))
    })
    for (const dot of await stages.locator('.soha-execution-live-dot').all())
      await expect(dot).toHaveCSS('animation-play-state', 'paused')
    await page.evaluate(() => {
      Object.defineProperty(document, 'hidden', { value: false, configurable: true })
      document.dispatchEvent(new Event('visibilitychange'))
    })
    await stages.screenshot({ path: testInfo.outputPath('workflow-stages.png') })
    await deploy.focus()
    await page.keyboard.press('Enter')
    await expect(page).toHaveURL(/\/workflows\/retail-run\?stage=deploy\+%2F\+api/)
    const inspection = page.getByRole('region', { name: '节点证据', exact: true })
    await expect(inspection.getByRole('heading', { name: '部署服务', exact: true })).toBeVisible()
    await expect(inspection).toContainText('镜像已提交，等待控制器就绪')
    await page.screenshot({ path: testInfo.outputPath('workflow-node-evidence.png') })
    await page.getByRole('button', { name: '生产确认，等待审批', exact: true }).click()
    await expect(inspection.getByRole('heading', { name: '生产确认', exact: true })).toBeVisible()
    await page.goBack()
    await expect(page).toHaveURL(/applicationEnvironmentId=retail-test/)
    await page.getByText('环境工作流', { exact: true }).click()
    await expect(
      stages.getByRole('button', { name: '部署服务，执行中', exact: true }),
    ).toBeVisible()
    deployed = true
    await expect(stages.getByRole('button', { name: '部署服务，已完成', exact: true })).toBeVisible(
      { timeout: 10_000 },
    )
    await expect(stages).toContainText('等待审批')
    await expect(stages).toContainText('状态未知 · provider_unknown')
    const geometry = await page.evaluate(() => ({
      width: document.documentElement.clientWidth,
      scroll: document.documentElement.scrollWidth,
    }))
    expect(geometry.scroll).toBe(geometry.width)
    await page.goto('/workflows/retail-run?stage=removed-node')
    await expect(page.getByRole('alert')).toContainText('所选节点不在本次执行记录中')
    canViewWorkflows = false
    await page.goto(appUrl)
    await page.getByText('环境工作流', { exact: true }).click()
    await expect(
      stages.getByRole('button', { name: '构建镜像，已完成', exact: true }),
    ).toBeDisabled()
    verifyBrowserEvidence(evidence)
  })
}
