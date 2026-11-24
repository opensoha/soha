import { expect, test } from '@playwright/test'
import { baselineResponse } from '../fixtures/baseline-data'
import { installMocks, verifyBrowserEvidence } from '../fixtures/mock-app'

test('opens source plan impact, pages by keyboard, and stops approval after drift', async ({
  context,
  page,
}, info) => {
  await page.setViewportSize({ width: 390, height: 900 })
  await page.addInitScript(() =>
    localStorage.setItem(
      'soha-prefs',
      JSON.stringify({ state: { themeMode: 'dark', localeCode: 'zh_CN' }, version: 0 }),
    ),
  )
  const planId = 'source-plan'
  const applicationId = 'retail'
  const applicationEnvironmentId = 'source-test'
  const timestamp = '2026-11-22T00:00:00Z'
  const impactPath = `/api/v1/delivery/plans/${planId}/service-reference-impact`
  const evidence = await installMocks(context, page, (url, method) => {
    if (method !== 'GET') return undefined
    if (url.pathname === '/api/v1/access/permission-snapshot') {
      const body = structuredClone(baselineResponse(url.pathname)) as {
        data: { permissionKeys: string[] }
      }
      body.data.permissionKeys.push(
        'workbench.delivery.view',
        'delivery.workflows.view',
        'delivery.application-environments.approve',
      )
      return { body }
    }
    if (url.pathname === '/api/v1/delivery-batches/reference-batch')
      return {
        body: {
          data: {
            id: 'reference-batch',
            rootRunId: 'reference-run',
            status: 'waiting_approval',
            definition: { name: '源环境交付', targets: [] },
            targets: [],
            nodes: [],
            serviceCount: 0,
            targetCount: 0,
            buildCount: 0,
            createdBy: 'tester',
            createdAt: timestamp,
            updatedAt: timestamp,
          },
        },
      }
    if (url.pathname === `/api/v1/delivery/plans/${planId}`)
      return {
        body: {
          data: {
            id: planId,
            source: 'delivery_batch',
            status: 'waiting_approval',
            applicationId,
            applicationEnvironmentId,
            action: 'deploy',
            riskLevel: 'high',
            requiresApproval: true,
            createdAt: timestamp,
            updatedAt: timestamp,
          },
        },
      }
    if (url.pathname !== impactPath) return undefined
    const cursor = url.searchParams.get('cursor')
    expect([null, 'reference-a']).toContain(cursor)
    const second = cursor === 'reference-a'
    const suffix = second ? 'b' : 'a'
    return {
      body: {
        planId,
        sourceApplicationEnvironmentId: applicationEnvironmentId,
        revision: 'a'.repeat(64),
        currentRevision: (second ? 'b' : 'a').repeat(64),
        isCurrent: !second,
        hasRestrictedConsumers: true,
        nextCursor: second ? undefined : 'reference-a',
        items: [
          {
            referenceId: `reference-${suffix}`,
            referenceRevision: 1,
            applicationEnvironmentId: `consumer-binding-${suffix}`,
            environmentId: `consumer-environment-${suffix}`,
            environmentKey: `consumer-test-${suffix}`,
            serviceId: `retail-test-${'order-service-'.repeat(5)}${suffix}`,
            sourceApplicationEnvironmentId: applicationEnvironmentId,
            sourceTargetIds: ['source-target'],
          },
        ],
      },
    }
  })

  await page.goto('/delivery/batches/reference-batch?planId=source-plan')
  const drawer = page.getByRole('dialog', { name: '最终部署计划' })
  const next = drawer.getByRole('button', { name: '下一页', exact: true })
  const approve = drawer.getByRole('button', { name: '批准此计划', exact: true })
  await expect(drawer.getByText('consumer-test-a', { exact: true })).toBeVisible()
  await expect(drawer).toContainText('部分引用环境受权限保护')
  await expect(approve).toBeEnabled()
  await next.focus()
  await page.keyboard.press('Enter')
  await expect(drawer.getByText('consumer-test-b', { exact: true })).toBeVisible()
  await expect(drawer.getByText('consumer-test-a', { exact: true })).toHaveCount(0)
  await expect(drawer).toContainText('请重新预检并生成新的交付计划')
  await expect(approve).toBeDisabled()
  await expect(drawer.getByRole('button', { name: /^拒\s*绝$/ })).toBeEnabled()
  await expect
    .poll(() => drawer.evaluate((element) => element.scrollWidth <= element.clientWidth))
    .toBe(true)
  await drawer.screenshot({ path: info.outputPath('stale-reference-impact.png') })
  verifyBrowserEvidence(evidence)
})
