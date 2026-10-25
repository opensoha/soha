import { test, expect } from '@playwright/test'
import { installMocks, verifyBrowserEvidence } from '../fixtures/mock-app'
import { task114Response } from '../fixtures/task114-delivery'

const now = '2026-10-25T00:00:00Z'
const resource = (kind: string, name: string, uid: string) => ({
  clusterId: 'cluster-a',
  namespace: 'retail-test',
  apiVersion: kind === 'Pod' ? 'v1' : 'apps/v1',
  kind,
  name,
  uid,
  scopeMode: 'namespace',
})
const source = {
  resource: resource('Deployment', 'order-api', 'root-uid'),
  specDigest: 'a'.repeat(64),
  observedAt: now,
}
const target = {
  id: 'native-root',
  clusterId: 'cluster-a',
  namespace: 'retail-test',
  workloadKind: 'Deployment',
  workloadName: 'order-api',
  enabled: false,
  metadata: {
    importSource: 'kubernetes',
    serviceId: 'order-api',
    resourceSource: JSON.stringify(source),
  },
}
const binding = {
  id: 'retail-test',
  applicationId: 'retail',
  environmentId: 'test',
  clusterId: 'cluster-a',
  namespace: 'retail-test',
  createdAt: now,
  updatedAt: now,
  targets: [target],
}

for (const width of [1440, 390]) {
  test(`Task114 service detail preserves Pod cards and shares verified resource relationships at ${width}px`, async ({
    context,
    page,
  }) => {
    await page.setViewportSize({ width, height: 1100 })
    if (width === 390)
      await page.addInitScript(() =>
        localStorage.setItem(
          'soha-prefs',
          JSON.stringify({ state: { themeMode: 'dark', localeCode: 'zh_CN' }, version: 0 }),
        ),
      )
    const reads: string[] = [],
      writes: string[] = []
    const evidence = await installMocks(context, page, (url, method) => {
      const path = url.pathname.replace(/^\/api\/v1/, '')
      const data = (value: unknown) => ({ body: { data: value } })
      if (method !== 'GET' && !path.startsWith('/auth/')) writes.push(path)
      if (path === '/access/permission-snapshot')
        return data({
          permissionKeys: [
            'workbench.delivery.view',
            'workspace.application.view',
            'delivery.applications.view',
            'delivery.application-services.view',
            'delivery.application-environments.view',
            'platform.pods.view',
            'platform.deployment.view',
            'platform.configuration.config-maps.view',
            'platform.configuration.secrets.view',
          ],
          visibleMenuIds: ['applications'],
          visibleMenus: [{ id: 'applications', path: '/applications', labelZh: '应用中心' }],
        })
      if (path === '/application-environments') return data([binding])
      if (path === '/application-environments/retail-test/targets/native-root/relations')
        return data({
          root: source.resource,
          config: { version: 'v1', rootUid: 'root-uid', sources: [], rules: [] },
          nodes: [
            { resource: source.resource, role: 'root', depth: 0 },
            { resource: resource('ReplicaSet', 'order-api-rs', 'rs-uid'), role: 'owned', depth: 1 },
            { resource: resource('Pod', 'order-api-pod-0', 'pod-uid-0'), role: 'owned', depth: 2 },
          ],
          edges: [
            { fromUid: 'root-uid', toUid: 'rs-uid', type: 'owns' },
            { fromUid: 'rs-uid', toUid: 'pod-uid-0', type: 'owns' },
          ],
          coverage: [],
          partial: false,
          observedAt: now,
        })
      if (path === '/application-environments/retail-test/targets/native-root/source') {
        reads.push(path)
        return data({
          name: 'order-api',
          kind: 'Deployment',
          content: JSON.stringify({
            apiVersion: 'apps/v1',
            kind: 'Deployment',
            metadata: { name: 'order-api', namespace: 'retail-test', uid: 'root-uid' },
            spec: { replicas: 2 },
          }),
        })
      }
      if (path.includes('/secrets/')) {
        reads.push(path)
        return undefined
      }
      if (path.includes('/configmaps/order-config/yaml')) {
        reads.push(path)
        return data({
          content:
            'apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: order-config\n  namespace: retail-test\ndata:\n  mode: demo',
        })
      }
      const base = task114Response(url, method)
      if (base && path === '/applications/retail/detail')
        return data({
          ...(base.body as { data: Record<string, unknown> }).data,
          bindings: [binding],
        })
      if (base && path === '/applications/retail/runtime') {
        const payload = (
          base.body as {
            data: {
              environments: Array<{
                environmentId: string
                workloads: Array<Record<string, unknown>>
              }>
            }
          }
        ).data
        const env = payload.environments.find((item) => item.environmentId === 'test')!
        const workload = env.workloads.find((item) => item.workloadName === 'order-api')!
        env.workloads = [{ ...workload, targetId: 'native-root', sourceStatus: 'linked', source }]
        return data(payload)
      }
      if (base && path.endsWith('/workloads/order-api/runtime')) {
        const payload = (
          base.body as {
            data: {
              workload: Record<string, unknown>
              deployment: Record<string, unknown>
              pods: Array<Record<string, unknown>>
            }
          }
        ).data
        payload.workload = {
          ...payload.workload,
          targetId: 'native-root',
          sourceStatus: 'linked',
          source,
        }
        payload.pods = payload.pods.map((pod, index) => ({ ...pod, uid: `pod-uid-${index}` }))
        payload.deployment.relatedResources = [
          { kind: 'Service', name: 'order-service', relation: 'Service selector' },
          { kind: 'Ingress', name: 'order-public', relation: 'backend Service' },
          { kind: 'Gateway', name: 'edge', relation: 'HTTPRoute parent' },
          { kind: 'ConfigMap', name: 'order-config', relation: 'envFrom' },
          { kind: 'Secret', name: 'order-secret', relation: 'secretKeyRef' },
          { kind: 'PersistentVolumeClaim', name: 'order-data', relation: 'volume claim' },
        ]
        return data(payload)
      }
      return base
    })
    await page.goto(
      '/applications/retail?applicationEnvironmentId=retail-test&serviceId=order-api&serviceTab=pods',
    )
    const relations = page.getByRole('region', { name: '运行资源关系', exact: true })
    await expect(
      relations.getByRole('button', { name: '选择 ReplicaSet order-api-rs', exact: true }),
    ).toBeVisible()
    await expect(relations).toContainText('Service selector')
    await expect(page.locator('.soha-service-pods-panel .soha-resource-browser')).toHaveCount(0)
    const pod = page.locator('.soha-pod-card').filter({ hasText: 'order-api-pod-0' })
    await expect(pod).toContainText('worker-1')
    for (const label of ['详情', '事件', 'YAML'])
      await expect(pod.getByRole('button', { name: label, exact: true })).toBeVisible()
    await expect(pod.getByRole('button', { name: '查看监控 order-api-pod-0' })).toBeVisible()
    expect((await pod.boundingBox())!.height).toBeLessThan(240)
    const detail = page.getByRole('complementary', { name: '所选资源详情' })
    await relations.getByRole('button', { name: '选择 Secret order-secret', exact: true }).click()
    await expect(detail).toContainText('不回显数据')
    await expect(detail.getByRole('button', { name: '查看资源 YAML' })).toHaveCount(0)
    expect(reads).toEqual([])
    await relations
      .getByRole('button', { name: '选择 ConfigMap order-config', exact: true })
      .click()
    await detail.getByRole('button', { name: '按名称查看当前 YAML', exact: true }).click()
    const yaml = page.getByRole('dialog', { name: 'ConfigMap / order-config' })
    await expect(yaml).toContainText('mode: demo')
    await expect(yaml).toContainText('此引用未固定 UID')
    await yaml.getByRole('button', { name: '关闭', exact: true }).click()
    await relations.getByRole('button', { name: '选择 Deployment order-api', exact: true }).click()
    await detail.getByRole('button', { name: '查看源 YAML', exact: true }).click()
    const sourceYaml = page.getByRole('dialog', { name: 'Deployment / order-api · 当前源配置' })
    await expect(sourceYaml).toContainText('replicas')
    await sourceYaml.getByRole('button', { name: '关闭', exact: true }).click()
    await relations.getByText('资源列表', { exact: true }).click()
    await expect(relations.getByRole('columnheader', { name: '关联依据' })).toBeVisible()
    for (const label of ['配置与模板', '状态与事件', '发布与工作流', '运行资源']) {
      if (width === 390 && label === '运行资源') {
        await page.locator('.soha-service-detail .ant-tabs-nav-more').hover()
        await page.getByRole('option', { name: label, exact: true }).click()
      } else {
        await page.getByRole('tab', { name: label, exact: true }).click()
      }
      await expect(page.getByRole('heading', { name: '订单 API', exact: true })).toBeVisible()
    }
    await expect(pod).toBeVisible()
    expect(
      await page.evaluate(
        () => document.documentElement.scrollWidth - document.documentElement.clientWidth,
      ),
    ).toBeLessThanOrEqual(1)
    await page.screenshot({
      path: `../docs/tasks/114-enterprise-delivery-workbench/evidence/m17/service-detail-${width}.png`,
      fullPage: true,
    })
    expect(reads.some((path) => path.includes('/secrets/'))).toBe(false)
    expect(writes).toEqual([])
    verifyBrowserEvidence(evidence)
  })
}
