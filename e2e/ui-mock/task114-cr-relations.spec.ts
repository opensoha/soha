import { test, expect } from '@playwright/test'
import { installMocks, verifyBrowserEvidence } from '../fixtures/mock-app'
import { task114Response } from '../fixtures/task114-delivery'

const updatedAt = '2026-10-22T00:00:00Z'
const source = {
  resource: {
    clusterId: 'cluster-a',
    namespace: 'retail-test',
    apiVersion: 'unregistered.example.com/v1',
    kind: 'UnknownWidget',
    name: 'search',
    uid: 'unknown-root',
    scopeMode: 'namespace',
  },
  crdName: 'unknownwidgets.unregistered.example.com',
  plural: 'unknownwidgets',
  specDigest: 'a'.repeat(64),
  observedAt: updatedAt,
}
const target = {
  id: 'root-test',
  clusterId: 'cluster-a',
  namespace: 'retail-test',
  workloadKind: 'CustomResource',
  workloadName: 'search',
  enabled: true,
  metadata: {
    importSource: 'kubernetes',
    resourceSource: JSON.stringify(source),
    serviceId: 'search',
    unrelated: 'keep',
  },
}
const binding = {
  id: 'retail-test',
  applicationId: 'retail',
  environmentId: 'test',
  clusterId: 'cluster-a',
  namespace: 'retail-test',
  createdAt: updatedAt,
  updatedAt,
  targets: [target],
}
const pod = {
  clusterId: 'cluster-a',
  namespace: 'retail-test',
  apiVersion: 'v1',
  kind: 'Pod',
  name: 'search-pod-0',
  uid: 'unknown-child',
  scopeMode: 'namespace',
}
const defaultRules = { version: 'v1', rootUid: 'unknown-root', sources: [], rules: [] }

for (const width of [1440, 390]) {
  test(`Task114 unknown CR relationships, declarative rule CAS and shared metrics at ${width}px`, async ({
    context,
    page,
  }) => {
    await page.setViewportSize({ width, height: 1000 })
    let revision = updatedAt
    let rules: unknown = defaultRules
    const writes: Record<string, unknown>[] = []
    const reads: string[] = []
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
            'delivery.application-environments.view',
            'delivery.application-environments.update',
            'platform.pods.view',
          ],
          visibleMenuIds: ['applications'],
          visibleMenus: [{ id: 'applications', path: '/applications', labelZh: '应用中心' }],
        })
      if (path === '/application-environments') return data([{ ...binding, updatedAt: revision }])
      if (path === '/application-environments/retail-test' && method === 'PUT') {
        const body = request.postDataJSON() as Record<string, unknown>
        writes.push(body)
        expect(body.expectedUpdatedAt).toBe(writes.length === 1 ? updatedAt : revision)
        if (writes.length === 1) {
          revision = '2026-10-22T00:01:00Z'
          return {
            status: 409,
            body: {
              error: {
                code: 'conflict',
                message: '环境已被修改，请重新读取后合并',
                requestId: 'synthetic-cas',
              },
            },
          }
        }
        const targets = body.targets as Array<{ metadata: Record<string, unknown> }>
        expect(targets[0].metadata.unrelated).toBe('keep')
        rules = targets[0].metadata.resourceRelations
        return data({ ...binding, ...body })
      }
      if (path === '/application-environments/retail-test/targets/root-test/relations') {
        reads.push(path)
        return data({
          root: source.resource,
          config: rules,
          nodes: [
            { resource: source.resource, role: 'root', depth: 0 },
            { resource: pod, role: 'owned', depth: 1 },
            {
              resource: { ...pod, name: 'shared-runtime', uid: 'shared' },
              role: 'reference',
              depth: 1,
            },
            {
              resource: { ...pod, name: 'possible-pod', uid: 'candidate' },
              role: 'candidate',
              depth: 1,
            },
          ],
          edges: [{ fromUid: source.resource.uid, toUid: pod.uid, type: 'owns' }],
          coverage: [{ queryId: 'owner.2', state: 'denied', reasonCode: 'source_permission' }],
          observedAt: updatedAt,
          partial: true,
        })
      }
      if (path === '/clusters/cluster-a/workloads/pods/search-pod-0/detail')
        return data({
          ...pod,
          phase: 'Running',
          containers: [
            { name: 'main', role: 'main', ready: true, image: 'test:1', restartCount: 0 },
          ],
        })
      const base = task114Response(url, method)
      if (base && path === '/applications/retail/detail') {
        const body = base.body as { data: Record<string, unknown> }
        return data({ ...body.data, bindings: [{ ...binding, updatedAt: revision }] })
      }
      if (base && path === '/applications/retail/runtime') {
        const body = base.body as {
          data: { environments: Array<{ environmentId: string; workloads: unknown[] }> }
        }
        for (const env of body.data.environments)
          if (env.environmentId === 'test')
            env.workloads = [
              {
                applicationEnvironmentId: 'retail-test',
                targetId: 'root-test',
                serviceId: 'search',
                clusterId: 'cluster-a',
                namespace: 'retail-test',
                workloadKind: 'CustomResource',
                workloadName: 'search',
                sourceStatus: 'linked',
                healthStatus: 'unknown',
                desiredReplicas: 0,
                readyReplicas: 0,
                updatedReplicas: 0,
                availableReplicas: 0,
                source,
              },
            ]
      }
      return base
    })
    await page.goto(
      '/applications/retail?tab=services&applicationEnvironmentId=retail-test&serviceId=search',
    )
    const relations = page.getByRole('region', { name: '运行资源关系', exact: true })
    await expect(relations).toContainText('UnknownWidget')
    await expect(relations).toContainText('关系不完整')
    await expect(relations).toContainText('共享引用')
    await expect(relations.getByRole('button', { name: '查看 possible-pod 指标' })).toHaveCount(0)
    await expect(page.getByRole('button', { name: '查看源 YAML', exact: true })).toHaveCount(0)
    await page.getByRole('button', { name: '编辑关系规则', exact: true }).click()
    const dialog = page.getByRole('dialog', { name: '当前目标的声明式关系规则' })
    await dialog.getByLabel('关系规则 JSON').fill(
      JSON.stringify({
        ...defaultRules,
        rules: [
          {
            id: 'shared',
            type: 'reference',
            target: {
              apiVersion: 'v1',
              resource: 'services',
              kind: 'Service',
              namespace: 'retail-test',
            },
            name: 'shared-service',
          },
        ],
      }),
    )
    await dialog.getByRole('button', { name: '保存观察规则', exact: true }).click()
    await expect(dialog).toContainText('环境已被修改')
    await expect(dialog.getByLabel('关系规则 JSON')).toHaveValue(/shared-service/)
    expect(writes).toHaveLength(1)
    await page.reload()
    await page.getByRole('button', { name: '编辑关系规则', exact: true }).click()
    await dialog.getByLabel('关系规则 JSON').fill(
      JSON.stringify({
        ...defaultRules,
        rules: [
          {
            id: 'shared',
            type: 'reference',
            target: {
              apiVersion: 'v1',
              resource: 'services',
              kind: 'Service',
              namespace: 'retail-test',
            },
            name: 'shared-service',
          },
        ],
      }),
    )
    await dialog.getByRole('button', { name: '保存观察规则', exact: true }).click()
    await expect(dialog).toContainText('关系规则已保存')
    expect(writes).toHaveLength(2)
    await dialog.getByRole('button', { name: '关闭', exact: true }).click()
    await relations.getByRole('button', { name: '查看 search-pod-0 指标' }).click()
    await expect(page.getByRole('region', { name: '关联 Pod 指标', exact: true })).toBeVisible()
    await expect(page.getByRole('region', { name: '关联 Pod 指标', exact: true })).toContainText(
      'Pod 指标',
    )
    await expect(page.getByRole('button', { name: /实时会话/ })).toHaveCount(0)
    const geometry = await page.evaluate(() => ({
      width: document.documentElement.clientWidth,
      scroll: document.documentElement.scrollWidth,
    }))
    expect(geometry.scroll).toBeLessThanOrEqual(geometry.width)
    await page.screenshot({
      path: `../docs/tasks/114-enterprise-delivery-workbench/evidence/m5/relations-metrics-${width}.png`,
    })
    await page
      .getByRole('navigation', { name: '选择环境', exact: true })
      .getByRole('button', { name: /生产环境/ })
      .click()
    await expect(page.getByRole('region', { name: '关联 Pod 指标', exact: true })).toHaveCount(0)
    expect(reads.length).toBeGreaterThan(0)
    verifyBrowserEvidence(evidence)
  })
}
