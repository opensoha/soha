import { test, expect } from '@playwright/test'
import { installMocks, verifyBrowserEvidence } from '../fixtures/mock-app'
import { task114Response } from '../fixtures/task114-delivery'

for (const width of [1440, 390]) {
  test(`Task114 ten service cards and source types preserve context at ${width}px`, async ({
    context,
    page,
  }) => {
    await page.setViewportSize({ width, height: 1000 })
    const evidence = await installMocks(context, page, task114Response)
    await page.goto('/applications/retail?applicationEnvironmentId=retail-test')
    const cards = page.getByRole('list', { name: '服务卡片', exact: true })
    await expect(cards.getByRole('listitem')).toHaveCount(10)
    for (const kind of [
      'Kubernetes 工作负载',
      'Helm Release',
      'Operator CR',
      '托管 YAML',
      'Kustomize',
    ]) {
      await expect(cards.getByText(kind, { exact: true })).toHaveCount(2)
    }
    await cards.getByRole('button', { name: '订单 API', exact: true }).click()
    await expect(page.getByRole('heading', { name: '订单 API', exact: true })).toBeVisible()
    await page.getByRole('button', { name: '服务总览', exact: true }).click()
    await expect(page).not.toHaveURL(/serviceId=/)
    await expect(cards.getByRole('listitem')).toHaveCount(10)
    await page.getByRole('textbox', { name: '搜索服务', exact: true }).fill('订单')
    await expect(cards.getByRole('listitem')).toHaveCount(1)
    await page
      .getByRole('navigation', { name: '选择环境', exact: true })
      .getByRole('button', { name: /生产环境/ })
      .click()
    await expect(page).toHaveURL(/applicationEnvironmentId=retail-prod/)
    await expect(cards).toContainText('尚未部署')
    await expect(cards).not.toContainText('2 / 2')
    const size = await page.evaluate(() => ({
      width: document.documentElement.clientWidth,
      scroll: document.documentElement.scrollWidth,
    }))
    expect(size.scroll).toBe(size.width)
    verifyBrowserEvidence(evidence)
  })
}

test('service cards refresh recorded workflow stages without copying them across services or environments', async ({
  context,
  page,
}) => {
  let completed = false
  const currentRuns = () => [
    {
      id: 'order-run',
      applicationId: 'retail',
      workflowName: '订单发布',
      metadata: { bindingId: 'retail-test', serviceId: 'order-api' },
      status: completed ? 'completed' : 'running',
      steps: [],
      nodeRuns: [
        { nodeId: 'build', name: '构建镜像', type: 'build', status: 'succeeded' },
        {
          nodeId: 'deploy',
          name: '发布服务',
          type: 'deploy',
          status: completed ? 'succeeded' : 'running',
        },
      ],
      createdAt: '2026-10-25T00:00:00Z',
      updatedAt: '2026-10-25T00:00:01Z',
    },
    {
      id: 'payment-run',
      applicationId: 'retail',
      workflowName: '支付发布',
      metadata: { bindingId: 'retail-test', serviceId: 'payment-api' },
      status: 'waiting_approval',
      steps: [],
      nodeRuns: [
        {
          nodeId: 'approve',
          name: '生产确认',
          type: 'manual_approval',
          status: 'waiting_approval',
        },
      ],
      createdAt: '2026-10-25T00:01:00Z',
      updatedAt: '2026-10-25T00:01:01Z',
    },
  ]
  const evidence = await installMocks(context, page, (url, method) => {
    const response = task114Response(url, method)
    if (url.pathname === '/api/v1/access/permission-snapshot' && response) {
      const body = response.body as { data: { permissionKeys: string[] } }
      body.data.permissionKeys.push('delivery.workflows.view')
    }
    if (url.pathname === '/api/v1/workflows' && response) {
      expect(url.searchParams.get('applicationId')).toBe('retail')
      expect(url.searchParams.get('limit')).toBe('200')
      response.body = {
        data:
          url.searchParams.get('applicationEnvironmentId') === 'retail-test' ? currentRuns() : [],
      }
    }
    if (url.pathname === '/api/v1/applications/retail/detail' && response) {
      const body = response.body as { data: { bindings: unknown[] } }
      body.data.bindings = [
        {
          applicationEnvironmentId: 'retail-test',
          environmentId: 'test',
          targetCount: 1,
          requiresApproval: false,
          latestWorkflow: currentRuns()[1],
        },
      ]
    }
    return response
  })
  await page.goto('/applications/retail?applicationEnvironmentId=retail-test')
  const cards = page.getByRole('list', { name: '服务卡片', exact: true })
  const order = cards.getByRole('listitem', { name: '服务 订单 API', exact: true })
  const payment = cards.getByRole('listitem', { name: '服务 支付服务', exact: true })
  await expect(order.getByRole('button', { name: '发布服务，执行中', exact: true })).toBeEnabled()
  await expect(order).toContainText('运行正常')
  await expect(payment).not.toContainText('订单发布')
  await expect(payment).toContainText('支付发布')
  await expect(
    payment.getByRole('button', { name: '生产确认，等待审批', exact: true }),
  ).toBeEnabled()
  completed = true
  await page.getByRole('button', { name: '刷新当前环境', exact: true }).click()
  await expect(order.getByRole('button', { name: '发布服务，已完成', exact: true })).toBeEnabled()
  await page
    .getByRole('navigation', { name: '选择环境', exact: true })
    .getByRole('button', { name: /生产环境/ })
    .click()
  await expect(cards).not.toContainText('订单发布')
  await expect(cards).toContainText('尚未部署')
  verifyBrowserEvidence(evidence)
})
