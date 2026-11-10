import { test, expect } from '@playwright/test'
import { installMocks, verifyBrowserEvidence } from '../fixtures/mock-app'
import { task114Response } from '../fixtures/task114-delivery'

const now = '2026-10-22T00:00:00Z'
const source = {
  resource: {
    clusterId: 'cluster-a',
    namespace: 'retail-test',
    apiVersion: 'unregistered.example/v1',
    kind: 'UnknownWidget',
    name: 'search',
    uid: 'unknown-root',
    scopeMode: 'namespace',
  },
  crdName: 'unknownwidgets.unregistered.example',
  plural: 'unknownwidgets',
  specDigest: 'a'.repeat(64),
  observedAt: now,
  generation: 2,
}
const originalTarget = {
  id: 'root-test',
  clusterId: 'cluster-a',
  namespace: 'retail-test',
  workloadKind: 'CustomResource',
  workloadName: 'search',
  enabled: true,
  executorKind: 'observe_only',
  configRef: '',
  metadata: {
    importSource: 'kubernetes',
    ownershipMode: 'observe_only',
    resourceSource: JSON.stringify(source),
    serviceId: 'search',
    unrelated: 'keep',
  },
}
const rootYAML = `apiVersion: unregistered.example/v1
kind: UnknownWidget
metadata:
  name: search
  namespace: retail-test
  uid: unknown-root
  generation: 2
  resourceVersion: "12"
  finalizers: [operator.example/hold]
spec:
  replicas: 1
status:
  ready: false
`
const draft = `apiVersion: unregistered.example/v1
kind: UnknownWidget
metadata:
  name: search
  namespace: retail-test
spec:
  replicas: 2
`

for (const width of [1440, 390]) {
  test(`Task114 unknown root CR uses shared configuration diff and explicit publication at ${width}px`, async ({
    context,
    page,
  }) => {
    await page.setViewportSize({ width, height: 1000 })
    const runtimeErrors: string[] = []
    page.on('pageerror', (error) => runtimeErrors.push(error.message))
    if (width === 390)
      await context.addInitScript(() =>
        localStorage.setItem(
          'soha-prefs',
          JSON.stringify({ state: { themeMode: 'dark', localeCode: 'zh_CN' }, version: 0 }),
        ),
      )
    let binding: Record<string, unknown> = {
      id: 'retail-test',
      applicationId: 'retail',
      environmentId: 'test',
      environmentKey: 'test',
      alias: '测试环境',
      clusterId: 'cluster-a',
      namespace: 'retail-test',
      createdAt: now,
      updatedAt: now,
      targets: [originalTarget],
    }
    let packages: Record<string, unknown>[] = []
    const writes: string[] = []
    let attempt = 0
    const data = (value: unknown) => ({ body: { data: value } })
    const evidence = await installMocks(context, page, (url, method, request) => {
      const path = url.pathname.replace(/^\/api\/v1/, '')
      if (path === '/access/permission-snapshot')
        return data({
          permissionKeys: [
            'workbench.delivery.view',
            'workspace.application.view',
            'delivery.applications.view',
            'delivery.application-services.view',
            'delivery.application.update',
            'delivery.application-environments.view',
            'delivery.application-environments.update',
            'platform.configuration.secret-data.view',
            'delivery.releases.trigger',
            'delivery.manifest-deployments.manage.preflight',
            'delivery.manifest-deployments.manage.trigger',
          ],
          visibleMenuIds: ['applications'],
          visibleMenus: [{ id: 'applications', path: '/applications', labelZh: '应用中心' }],
        })
      if (path === '/application-environments' && method === 'GET') return data([binding])
      if (path === '/application-environments/retail-test' && method === 'PUT') {
        writes.push(path)
        const body = request.postDataJSON()
        expect(body.expectedUpdatedAt).toBe(now)
        expect(body.targets[0]).toMatchObject({
          id: 'root-test',
          executorKind: 'manifest_ssa',
          configRef: 'manifest-root',
          metadata: {
            ownershipMode: 'managed',
            rootCRGeneration: 2,
            unrelated: 'keep',
            resourceSource: JSON.stringify(source),
          },
        })
        binding = { ...binding, ...body, updatedAt: '2026-10-22T00:03:00Z' }
        return data(binding)
      }
      if (path === '/application-environments/retail-test/targets/root-test/source') {
        return data({ content: rootYAML })
      }
      if (path === '/application-environments/retail-test/targets/root-test/relations')
        return data({
          root: source.resource,
          config: { version: 'v1', rootUid: source.resource.uid, sources: [], rules: [] },
          nodes: [{ resource: source.resource, role: 'root', depth: 0 }],
          edges: [],
          coverage: [],
          observedAt: now,
          partial: false,
        })
      if (path === '/delivery/manifest-packages' && method === 'GET')
        return data({ items: packages, total: packages.length, page: 1, pageSize: 100 })
      if (path === '/delivery/manifest-packages' && method === 'POST') {
        attempt += 1
        if (attempt === 1)
          return {
            status: 409,
            body: {
              error: {
                code: 'conflict',
                message: '配置记录已变化，请保留草稿并重试',
                requestId: 'synthetic-root-conflict',
              },
            },
          }
        writes.push(path)
        const body = request.postDataJSON()
        expect(body.files).toHaveLength(1)
        expect(body.files[0].content).toBe(draft)
        expect(body.files[0].content).not.toMatch(/status:|finalizers:|resourceVersion:|uid:/)
        packages = [
          {
            ...body,
            id: 'root-package',
            status: 'draft',
            currentRevision: 0,
            createdAt: now,
            updatedAt: now,
            bindings: [{ ...body.bindings[0], id: 'manifest-root', enabled: true }],
          },
        ]
        return data(packages[0])
      }
      if (path === '/delivery/manifest-packages/root-package/revisions') {
        if (method === 'GET')
          return data(
            packages[0]?.currentRevision ? [{ version: 1, files: packages[0].files }] : [],
          )
        writes.push(path)
        expect(request.postDataJSON()).toEqual({ expectedUpdatedAt: now })
        packages = [
          {
            ...packages[0],
            status: 'published',
            currentRevision: 1,
            updatedAt: '2026-10-22T00:02:00Z',
          },
        ]
        return data(packages[0])
      }
      const base = task114Response(url, method)
      if (base && path === '/applications/retail/detail') {
        const body = base.body as { data: Record<string, unknown> }
        return data({ ...body.data, bindings: [binding] })
      }
      if (base && path === '/applications/retail/runtime') {
        const body = base.body as {
          data: { environments: Array<{ environmentId: string; workloads: unknown[] }> }
        }
        for (const environment of body.data.environments)
          if (environment.environmentId === 'test')
            environment.workloads = [
              {
                applicationEnvironmentId: 'retail-test',
                targetId: 'root-test',
                serviceId: 'search',
                ...source.resource,
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
    await page.getByRole('tab', { name: '配置与模板', exact: true }).click()
    await page.getByRole('button', { name: '配置与发布控制', exact: true }).click()
    const drawer = page.getByRole('dialog', { name: '根 CR 配置与发布控制' })
    await expect(drawer).toContainText('当前仅观察')
    await expect(drawer).toContainText('generation 2')
    const editor = drawer.locator('.soha-yaml-pane').first().locator('.monaco-editor')
    await expect(editor).toBeVisible()
    const input = editor.getByRole('textbox', { name: 'Editor content', exact: true })
    await input.press(process.platform === 'darwin' ? 'Meta+ArrowDown' : 'Control+End')
    await input.press('ArrowLeft')
    await input.press('Backspace')
    await input.press('2')
    await drawer.getByRole('button', { name: '保存草稿', exact: true }).click()
    await expect(drawer).toContainText('配置记录已变化')
    await expect(drawer.getByRole('button', { name: '保存草稿', exact: true })).toBeEnabled()
    await drawer.getByRole('button', { name: '保存草稿', exact: true }).click()
    await drawer.getByRole('button', { name: '保存配置版本', exact: true }).click()
    await expect(drawer).toContainText('配置版本 v1 已保存')
    await drawer.getByRole('button', { name: '允许 Soha 发布根配置', exact: true }).click()
    await expect(drawer).toContainText('Soha 发布根配置 · Operator 协调子资源')
    expect(writes).toHaveLength(3)
    const geometry = await drawer.evaluate((element) => ({
      width: element.clientWidth,
      scroll: element.scrollWidth,
    }))
    expect(geometry.scroll).toBeLessThanOrEqual(geometry.width + 1)
    await page.screenshot({
      path: `../docs/tasks/114-enterprise-delivery-workbench/evidence/m6/root-cr-${width}.png`,
    })
    await drawer.getByRole('button', { name: '生成发布计划', exact: true }).click()
    const plan = page.getByRole('dialog', { name: '发布清单', exact: true })
    await expect(plan).toBeVisible()
    await plan.getByRole('button', { name: /^取\s*消$/ }).click()
    await page.getByRole('button', { name: '配置与发布控制', exact: true }).click()
    await expect(drawer).toContainText('Soha 发布根配置 · Operator 协调子资源')
    await drawer.getByRole('button', { name: '关闭', exact: true }).click()
    await expect(drawer).not.toBeVisible()
    expect(writes).toHaveLength(3)
    expect(runtimeErrors, 'root CR editor and Plan lifecycle').toEqual([])
    verifyBrowserEvidence(evidence)
  })
}
