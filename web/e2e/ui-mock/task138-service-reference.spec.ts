import { expect, test } from '@playwright/test'
import { installMocks, verifyBrowserEvidence } from '../fixtures/mock-app'
import { task114Response } from '../fixtures/task114-delivery'

const now = '2026-11-22T00:00:00Z'
const serviceId = 'order-api'
const consumerId = 'retail-test'
const sourceId = 'retail-test-source'
const previewPath = `/application-environments/${consumerId}/service-reference-previews`
const candidatesPath = `/application-environments/${consumerId}/service-reference-candidates`
const activePath = `/application-environments/${consumerId}/service-references`
const target = {
  id: 'source-target',
  applicationEnvironmentId: sourceId,
  serviceId,
  enabled: true,
  clusterId: 'cluster-a',
  namespace: 'retail-source',
  workloadKind: 'Deployment',
  workloadName: 'order-source',
  metadata: { ownershipMode: 'managed' },
}
const environments = [
  { id: 'test', name: '消费测试环境', tier: 'test', stageLevel: 2 },
  { id: 'test-source', name: '来源测试环境', tier: 'test', stageLevel: 2 },
  { id: 'pre', name: '不兼容预发环境', tier: 'pre', stageLevel: 3 },
].map((environment) => ({
  ...environment,
  key: environment.id,
  enabled: true,
  isProduction: false,
  requiresApproval: false,
}))
const bindings = environments.map((environment) => ({
  id: `retail-${environment.id}`,
  applicationId: 'retail',
  environmentId: environment.id,
  environmentKey: environment.key,
  updatedAt: now,
  targets:
    environment.id === 'test'
      ? [
          {
            ...target,
            id: 'payment-target',
            applicationEnvironmentId: consumerId,
            serviceId: 'payment-api',
            workloadName: 'payment-local',
            metadata: { ownershipMode: 'managed', serviceId: 'payment-api' },
          },
        ]
      : [target],
}))
const scope = (bindingId: string, environmentId: string) => ({
  applicationEnvironmentId: bindingId,
  environmentId,
  environmentKey: environmentId,
  tier: 'test',
  stageLevel: 2,
  isProduction: false,
  updatedAt: now,
})
const sourceRevision = 'a'.repeat(64)
const preview = {
  applicationId: 'retail',
  serviceId,
  consumer: scope(consumerId, 'test'),
  source: scope(sourceId, 'test-source'),
  targets: [{ ...target, targetId: target.id, ownershipMode: 'managed', resources: [] }],
  sourceRevision,
  observedAt: now,
  currentLocalTargetIds: [],
}
const candidate = { id: 'candidate-1', revision: 1, serviceId }
const active = { id: 'reference-1', revision: 4, serviceId }
const candidateDetail = {
  ...candidate,
  status: 'current',
  currentSourceRevision: sourceRevision,
  preview,
}
const preflight = {
  serviceId,
  sourceApplicationEnvironmentId: sourceId,
  expectedConsumerUpdatedAt: now,
  expectedSourceUpdatedAt: now,
}

test('saves and activates a checked source, then detaches even after source permission is revoked', async ({
  context,
  page,
}, info) => {
  await page.setViewportSize({ width: 390, height: 900 })
  let saved = false
  let activated = false
  let sourceAllowed = true
  const operations: Array<{ method: string; path: string; payload: unknown }> = []
  const evidence = await installMocks(context, page, (url, method, request) => {
    const path = url.pathname.replace(/^\/api\/v1/, '')
    const data = (value: unknown) => ({ body: { data: value } })
    if (path === '/access/permission-snapshot')
      return data({
        permissionKeys: [
          'workbench.delivery.view',
          'workspace.application.view',
          'delivery.applications.view',
          'delivery.application-services.view',
          'delivery.application-services.update',
          'delivery.application-environments.view',
          'delivery.application-environments.update',
          'delivery.workflows.view',
          'delivery.workflows.trigger',
          'delivery.releases.trigger',
          'platform.pods.view',
        ],
        visibleMenuIds: ['applications'],
        visibleMenus: [{ id: 'applications', path: '/applications', labelZh: '应用中心' }],
      })
    if (path === '/delivery/environments') return { body: { items: environments } }
    if (path === '/application-environments')
      return {
        body: {
          items: bindings.filter((binding) => sourceAllowed || binding.id !== sourceId),
        },
      }
    if (path === candidatesPath) {
      if (!url.searchParams.has('cursor') && saved)
        return {
          body: { items: [{ ...candidate, serviceId: 'another-service' }], nextCursor: 'second' },
        }
      return { body: { items: saved ? [candidate] : [] } }
    }
    if (path === activePath) return { body: { items: activated ? [active] : [] } }
    if (path === `${candidatesPath}/${serviceId}` && method === 'GET') return data(candidateDetail)
    if (path === `${activePath}/${serviceId}` && method === 'GET') {
      return sourceAllowed
        ? data({ ...active, preview })
        : { status: 403, body: { error: { code: 'forbidden', message: 'Access denied' } } }
    }
    if (method === 'POST' && path === previewPath) {
      operations.push({ method, path, payload: request.postDataJSON() })
      return data(preview)
    }
    if (method === 'PUT' && path === `${candidatesPath}/${serviceId}`) {
      operations.push({ method, path, payload: request.postDataJSON() })
      saved = true
      return data(candidateDetail)
    }
    if (method === 'PUT' && path === `${activePath}/${serviceId}`) {
      operations.push({ method, path, payload: request.postDataJSON() })
      saved = false
      activated = true
      return data({ ...active, preview })
    }
    if (method === 'DELETE' && path === `${activePath}/${serviceId}`) {
      operations.push({ method, path, payload: request.postDataJSON() })
      activated = false
      return { status: 204, body: undefined }
    }
    if (path === '/delivery-batches' || path === '/delivery/release-bundles') return data([])
    const base = task114Response(url, method)
    if (path === '/applications/retail/application-environments')
      return data(
        bindings.map((binding) => {
          const environment = environments.find((item) => item.id === binding.environmentId)!
          return {
            ...environment,
            applicationEnvironmentId: binding.id,
            environmentId: environment.id,
            environmentName: environment.name,
            environmentKey: environment.key,
          }
        }),
      )
    if (
      base &&
      path === '/applications/retail/application-environments/retail-test/runtime-summary'
    ) {
      const body = structuredClone(base.body) as { data: { environment: { targets: unknown[] } } }
      body.data.environment.targets = bindings[0].targets
      return { ...base, body }
    }
    if (base && path === '/applications/retail/detail') {
      return data({
        ...(base.body as { data: Record<string, unknown> }).data,
        bindings: bindings.map((binding) => ({
          ...binding,
          applicationEnvironmentId: binding.id,
        })),
      })
    }
    return base
  })

  const configuration =
    '/applications/retail?tab=services&applicationEnvironmentId=retail-test&serviceId=order-api&serviceTab=configuration'
  await page.goto(configuration)
  const entry = page.getByRole('button', { name: '配置环境目标', exact: true })
  await expect(entry).toBeVisible()
  await expect(page.getByLabel('当前环境服务目标统计')).toContainText('本地 1 · 引用 0 · 未配置 9')
  await page.setViewportSize({ width: 1440, height: 900 })
  const catalogue = page.getByRole('navigation', { name: '服务列表' })
  await expect(
    catalogue.getByRole('button', { name: /支付服务/ }).getByText('本地', { exact: true }),
  ).toBeVisible()
  await expect(
    catalogue.getByRole('button', { name: /订单 API/ }).getByText('未配置', { exact: true }),
  ).toBeVisible()
  await page.setViewportSize({ width: 390, height: 900 })
  expect(evidence.requests.some((path) => path.includes('service-reference-candidates'))).toBe(
    false,
  )
  await entry.click()
  const drawer = page.getByRole('dialog', { name: '配置环境目标 · 订单 API' })
  await expect(drawer.getByText('未配置', { exact: true })).toBeVisible()
  const source = drawer.getByRole('combobox', { name: '引用源环境' })
  await source.click()
  await expect(page.getByText('不兼容预发环境', { exact: true })).toHaveCount(0)
  await page.getByText('来源测试环境', { exact: true }).click()
  await expect(drawer.getByText('Deployment / order-source', { exact: true })).toBeVisible()
  expect(operations).toEqual([{ method: 'POST', path: previewPath, payload: preflight }])
  await drawer.getByRole('button', { name: '保存引用候选', exact: true }).click()
  await expect(drawer.getByText('已保存的引用候选', { exact: true })).toBeVisible()
  await expect(drawer.getByText('未配置', { exact: true })).toBeVisible()
  expect(operations[1]).toEqual({
    method: 'PUT',
    path: `${candidatesPath}/${serviceId}`,
    payload: {
      sourceApplicationEnvironmentId: sourceId,
      expectedConsumerUpdatedAt: now,
      expectedSourceUpdatedAt: now,
      sourceTargetIds: [target.id],
      expectedSourceRevision: sourceRevision,
      expectedRevision: 0,
    },
  })
  await drawer.getByRole('button', { name: '激活引用', exact: true }).click()
  await page
    .locator('.ant-popconfirm')
    .getByRole('button', { name: '激活引用', exact: true })
    .click()
  await expect(drawer.getByText('引用自 test-source', { exact: true })).toBeVisible()
  await expect(page.getByLabel('当前环境服务目标统计')).toContainText('本地 1 · 引用 1 · 未配置 8')
  await expect(page.locator('.soha-service-selector__mobile')).toContainText(
    '订单 API · 跨环境引用',
  )
  await expect(drawer.getByRole('button', { name: '查看来源环境' })).toBeVisible()
  expect(operations[2]).toEqual({
    method: 'PUT',
    path: `${activePath}/${serviceId}`,
    payload: {
      expectedCandidateId: candidate.id,
      expectedCandidateRevision: candidate.revision,
      expectedConsumerUpdatedAt: now,
      expectedSourceUpdatedAt: now,
      expectedSourceRevision: sourceRevision,
      expectedRevision: 0,
    },
  })
  await page.goto(
    '/applications/retail?tab=delivery&applicationEnvironmentId=retail-test&serviceId=order-api',
  )
  await expect(
    page.getByRole('dialog').getByText('已跳过 1 个跨环境引用服务。请前往源环境发布。'),
  ).toBeVisible()
  await expect(page.getByRole('button', { name: '检查交付参数', exact: true })).toBeDisabled()
  await page.goto(
    '/applications/retail?tab=delivery&applicationEnvironmentId=retail-test&serviceId=payment-api',
  )
  await expect(page.getByRole('button', { name: '检查交付参数', exact: true })).toBeEnabled()
  await page.goto(configuration)
  await entry.click()
  await expect(drawer.getByText('引用自 test-source', { exact: true })).toBeVisible()
  sourceAllowed = false
  await drawer.getByRole('button', { name: '刷新配置' }).click()
  await expect(drawer.getByText('引用来源无访问权限')).toBeVisible()
  await expect(drawer.getByRole('button', { name: '查看来源环境' })).toHaveCount(0)
  await expect(drawer.getByText('Deployment / order-source', { exact: true })).toHaveCount(0)
  await expect
    .poll(() => drawer.evaluate((node) => node.scrollWidth <= node.clientWidth))
    .toBe(true)
  await drawer.screenshot({ path: info.outputPath('source-access-revoked.png') })
  await drawer.getByRole('button', { name: '解除引用', exact: true }).click()
  await page
    .locator('.ant-popconfirm')
    .getByRole('button', { name: '解除引用', exact: true })
    .click()
  await expect(drawer.getByText('未配置', { exact: true })).toBeVisible()
  expect(operations[3]).toEqual({
    method: 'DELETE',
    path: `${activePath}/${serviceId}`,
    payload: {
      expectedReferenceId: active.id,
      expectedRevision: active.revision,
      expectedConsumerUpdatedAt: now,
    },
  })
  expect(operations).toHaveLength(4)
  verifyBrowserEvidence(evidence)
})
