import { test, expect } from '@playwright/test'
import { installMocks, verifyBrowserEvidence } from '../fixtures/mock-app'
import { task114Response } from '../fixtures/task114-delivery'

const source = {
  resource: {
    clusterId: 'cluster-a',
    namespace: 'retail-test',
    apiVersion: 'example.com/v1',
    kind: 'Widget',
    name: 'search',
    uid: 'root-uid',
    scopeMode: 'namespace',
  },
  crdName: 'widgets.example.com',
  plural: 'widgets',
  specDigest: 'a'.repeat(64),
  observedAt: '2026-10-21T10:00:00Z',
}
const permissions = [
  'workbench.delivery.view',
  'workspace.application.view',
  'delivery.applications.view',
  'delivery.application.create',
  'delivery.application-services.view',
  'delivery.application-services.create',
  'delivery.application-environments.view',
  'delivery.application-environments.create',
  'platform.configuration.secret-data.view',
]

for (const width of [1440, 390]) {
  test(`Task114 imports an explicit CR with pinned preview and keeps conflicts visible at ${width}px`, async ({
    context,
    page,
  }) => {
    await page.setViewportSize({ width, height: 1000 })
    const writes: unknown[] = []
    const evidence = await installMocks(context, page, (url, method, request) => {
      const path = url.pathname.replace(/^\/api\/v1/, '')
      if (path === '/access/permission-snapshot')
        return {
          body: {
            data: {
              permissionKeys: permissions,
              visibleMenuIds: ['applications'],
              visibleMenus: [{ id: 'applications', path: '/applications', labelZh: '应用中心' }],
            },
          },
        }
      if (path === '/clusters/cluster-a/namespaces')
        return { body: { data: [{ name: 'retail-test', status: 'Active', ageSeconds: 10 }] } }
      if (path === '/application-environments/target-candidates') {
        expect(url.searchParams.get('workloadKind')).toBe('CustomResource')
        expect(url.searchParams.get('crdName')).toBe('widgets.example.com')
        return {
          body: {
            items: [
              {
                clusterId: 'cluster-a',
                namespace: 'retail-test',
                workloadKind: 'CustomResource',
                workloadName: 'search',
                desiredReplicas: 0,
                readyReplicas: 0,
                relatedResources: [],
                source,
              },
            ],
            truncated: false,
          },
        }
      }
      if (path === '/application-environments/imports' && method === 'POST') {
        writes.push(request.postDataJSON())
        return {
          status: 409,
          body: {
            error: {
              code: 'conflict',
              message: '源资源已变化，请重新预览',
              requestId: 'synthetic-import',
            },
          },
        }
      }
      return task114Response(url, method)
    })
    await page.goto('/applications')
    await page.getByRole('button', { name: '接入已有资源', exact: true }).click()
    const dialog = page.getByRole('dialog', { name: '导入集群服务' })
    await dialog.getByLabel('资源类型', { exact: true }).click()
    await page
      .locator('.ant-select-item-option')
      .filter({ hasText: 'Operator CR / 自定义资源' })
      .click()
    await dialog.getByLabel('CRD 完整名称', { exact: true }).fill('widgets.example.com')
    await dialog.getByLabel('集群', { exact: true }).click()
    await page.locator('.ant-select-item-option').filter({ hasText: '研发测试集群' }).click()
    await dialog.getByLabel('命名空间', { exact: true }).click()
    await page.locator('.ant-select-item-option').filter({ hasText: 'retail-test' }).click()
    await dialog.getByRole('button', { name: '下一步', exact: true }).click()
    await dialog.getByRole('row').filter({ hasText: 'search' }).getByRole('checkbox').check()
    await dialog.getByRole('button', { name: '下一步', exact: true }).click()
    await expect(dialog.getByLabel('源身份预览')).toContainText('root-uid')
    await expect(dialog.getByRole('radio', { name: '托管发布', exact: true })).toBeDisabled()
    await dialog.getByLabel('应用', { exact: true }).click()
    await page.locator('.ant-select-item-option').filter({ hasText: '零售业务' }).click()
    await dialog.getByLabel('环境', { exact: true }).fill('test')
    await dialog.getByLabel('环境', { exact: true }).press('Enter')
    await page.screenshot({
      path: `../docs/tasks/114-enterprise-delivery-workbench/evidence/m2/import-${width}.png`,
    })
    await dialog.getByRole('button', { name: '导入 1 个服务', exact: true }).click()
    await expect(page.locator('body')).toContainText('源资源已变化，请重新预览')
    expect(writes).toHaveLength(1)
    expect(writes[0]).toMatchObject({
      clusterId: 'cluster-a',
      namespace: 'retail-test',
      applicationKey: 'retail',
      environmentKey: 'test',
      ownershipMode: 'observe_only',
      workloads: [
        {
          workloadKind: 'CustomResource',
          workloadName: 'search',
          crdName: 'widgets.example.com',
          expectedUid: 'root-uid',
          expectedSpecDigest: 'a'.repeat(64),
        },
      ],
    })
    await expect(dialog.getByLabel('源身份预览')).toContainText('root-uid')
    const geometry = await dialog.evaluate((element) => ({
      width: element.clientWidth,
      scroll: element.scrollWidth,
    }))
    expect(geometry.scroll).toBeLessThanOrEqual(geometry.width)
    verifyBrowserEvidence(evidence)
  })
}

test('Task114 imported CR opens source YAML on demand and withdraws it across environment and history navigation', async ({
  context,
  page,
}) => {
  const reads: string[] = []
  const evidence = await installMocks(context, page, (url, method) => {
    const path = url.pathname.replace(/^\/api\/v1/, '')
    if (path === '/access/permission-snapshot')
      return {
        body: {
          data: {
            permissionKeys: permissions,
            visibleMenuIds: ['applications'],
            visibleMenus: [{ id: 'applications', path: '/applications', labelZh: '应用中心' }],
          },
        },
      }
    if (path === '/applications/retail/runtime') {
      const base = task114Response(url, method)!
      const body = base.body as {
        data: { environments: Array<{ environmentId: string; workloads: unknown[] }> }
      }
      for (const env of body.data.environments) {
        if (env.environmentId === 'test' || env.environmentId === 'prod')
          env.workloads = [
            {
              applicationEnvironmentId: `retail-${env.environmentId}`,
              targetId: `root-${env.environmentId}`,
              clusterId: 'cluster-a',
              namespace: `retail-${env.environmentId}`,
              workloadKind: 'CustomResource',
              workloadName: 'search',
              serviceId: 'search',
              healthStatus: 'unknown',
              sourceStatus: 'linked',
              desiredReplicas: 0,
              readyReplicas: 0,
              updatedReplicas: 0,
              availableReplicas: 0,
              source: {
                ...source,
                resource: {
                  ...source.resource,
                  namespace: `retail-${env.environmentId}`,
                  uid: `uid-${env.environmentId}`,
                },
              },
            },
          ]
      }
      return base
    }
    if (
      /^\/application-environments\/retail-(test|prod)\/targets\/root-(test|prod)\/relations$/.test(
        path,
      )
    ) {
      const root = {
        ...source.resource,
        namespace: path.includes('retail-prod') ? 'retail-prod' : 'retail-test',
        uid: path.includes('retail-prod') ? 'uid-prod' : 'uid-test',
      }
      return {
        body: {
          data: {
            root,
            config: { version: 'v1', rootUid: root.uid, sources: [], rules: [] },
            nodes: [{ resource: root, role: 'root', depth: 0 }],
            edges: [],
            coverage: [],
            partial: false,
            observedAt: source.observedAt,
          },
        },
      }
    }
    if (path === '/application-environments/retail-test/targets/root-test/source') {
      reads.push(path)
      return {
        body: {
          data: {
            content:
              'apiVersion: example.com/v1\nkind: Widget\nmetadata:\n  name: search\n  namespace: retail-test\n  uid: uid-test\nspec:\n  password: synthetic-secret',
          },
        },
      }
    }
    return task114Response(url, method)
  })
  await page.goto(
    '/applications/retail?tab=services&applicationEnvironmentId=retail-test&serviceId=search',
  )
  await expect(page.getByRole('heading', { name: '商品搜索', exact: true })).toBeVisible()
  await expect(page.getByRole('region', { name: '当前服务资源关系', exact: true })).toContainText(
    'Widget',
  )
  expect(reads).toHaveLength(0)
  await page.getByRole('button', { name: '查看源 YAML', exact: true }).click()
  await expect(page.getByLabel('源配置 YAML')).toContainText('synthetic-secret')
  await page.screenshot({
    path: '../docs/tasks/114-enterprise-delivery-workbench/evidence/m2/imported-source.png',
  })
  await page
    .getByRole('dialog', { name: 'Widget / search · 当前源配置', exact: true })
    .getByRole('button', { name: '关闭', exact: true })
    .click()
  await expect(page.getByLabel('源配置 YAML')).toHaveCount(0)
  await page
    .getByRole('navigation', { name: '选择环境', exact: true })
    .getByRole('button', { name: /生产环境/ })
    .click()
  await expect(page.getByRole('region', { name: '当前服务资源关系', exact: true })).toContainText(
    'uid-prod',
  )
  await expect(page.getByLabel('源配置 YAML')).toHaveCount(0)
  expect(reads).toHaveLength(1)
  await page.goBack()
  await expect(page.getByRole('region', { name: '当前服务资源关系', exact: true })).toContainText(
    'uid-test',
  )
  await page.getByRole('button', { name: '查看源 YAML', exact: true }).click()
  await expect(page.getByLabel('源配置 YAML')).toContainText('synthetic-secret')
  expect(reads).toHaveLength(2)
  await page.goForward()
  await expect(page.getByRole('region', { name: '当前服务资源关系', exact: true })).toContainText(
    'uid-prod',
  )
  await expect(page.getByRole('dialog')).toHaveCount(0)
  await expect(page.getByLabel('源配置 YAML')).toHaveCount(0)
  await expect(page.getByText('synthetic-secret', { exact: true })).toHaveCount(0)
  expect(reads).toHaveLength(2)
  verifyBrowserEvidence(evidence)
})
