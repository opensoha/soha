import { test, expect, type BrowserContext, type Page } from '@playwright/test'
import { baselineResponse } from '../fixtures/baseline-data'
import { installMocks, verifyBrowserEvidence } from '../fixtures/mock-app'

const root = '/api/v1/clusters/cluster-a/configuration/configmaps'
const url =
  '/configuration/configmaps/application-settings?clusterId=cluster-a&namespace=monitoring'
async function setup(context: BrowserContext, page: Page, readonly = false) {
  const state = {
    reads: [] as string[],
    writes: [] as string[],
    deleted: false,
    immutable: readonly,
  }
  const detail = {
    name: 'application-settings',
    namespace: 'monitoring',
    ageSeconds: 3600,
    createdAt: '2026-11-03T01:00:00Z',
    immutable: readonly,
    data: { 'application.yaml': 'feature: enabled\nworkers: 3', 'empty.txt': '' },
    binaryData: { 'blob.bin': 'AP8=' },
    labels: { app: 'demo' },
    annotations: { note: 'preserved' },
  }
  const evidence = await installMocks(context, page, (requestUrl, method, request) => {
    const path = requestUrl.pathname
    if (path === '/api/v1/clusters') {
      const body = baselineResponse(path)!
      if (!Array.isArray(body.data)) throw new Error('Missing cluster list')
      return {
        body: { data: [...body.data, { ...body.data[0], id: 'cluster-b', name: 'stage-cluster' }] },
      }
    }
    if (path === '/api/v1/clusters/cluster-b/namespaces') {
      return { body: { data: [{ name: 'staging', status: 'Active', labels: {} }] } }
    }
    if (path === '/api/v1/clusters/cluster-b/configuration/configmaps') {
      return { body: { data: [{ name: detail.name, namespace: 'staging', dataEntries: 0 }] } }
    }
    if (
      path === '/api/v1/clusters/cluster-b/configuration/configmaps/application-settings/detail'
    ) {
      expect(requestUrl.searchParams.get('namespace')).toBe('staging')
      return { body: { data: { ...detail, namespace: 'staging', data: {} } } }
    }
    if (path === '/api/v1/access/permission-snapshot') {
      const body = baselineResponse(path)!
      if (!body.data || !('permissionKeys' in body.data)) throw new Error('Missing permissions')
      body.data.permissionKeys = body.data.permissionKeys!.filter(
        (permission) => !permission.startsWith('platform.configuration.config-maps.'),
      )
      body.data.permissionKeys.push(
        'platform.configuration.config-maps.view',
        'platform.pods.view',
        ...(readonly
          ? []
          : [
              'platform.configuration.config-maps.create',
              'platform.configuration.config-maps.update',
              'platform.configuration.config-maps.delete',
            ]),
      )
      return { body }
    }
    if (path === '/api/v1/clusters/cluster-a/resources/graph') {
      state.reads.push(path + requestUrl.search)
      return {
        body: {
          data: {
            clusterId: 'cluster-a',
            rootId: 'config',
            generatedAt: '',
            warnings: [],
            evidence: [],
            nodes: [
              {
                id: 'config',
                resource: {
                  clusterId: 'cluster-a',
                  apiVersion: 'v1',
                  kind: 'ConfigMap',
                  name: detail.name,
                  namespace: 'monitoring',
                  scopeMode: 'namespace',
                },
              },
              {
                id: 'pod',
                resource: {
                  clusterId: 'cluster-a',
                  apiVersion: 'v1',
                  kind: 'Pod',
                  name: 'api-worker',
                  namespace: 'monitoring',
                  scopeMode: 'namespace',
                },
              },
            ],
            edges: [
              { id: 'uses-config', sourceId: 'pod', targetId: 'config', relation: 'uses-config' },
            ],
          },
        },
      }
    }
    if (path.startsWith(root)) {
      if (method === 'GET') state.reads.push(path + requestUrl.search)
      else state.writes.push(method + ' ' + path)
      if (path === root)
        return {
          body: {
            data: state.deleted
              ? []
              : Array.from({ length: 42 }, (_, i) => ({
                  ...detail,
                  name: i === 0 ? detail.name : `config-${String(i).padStart(2, '0')}`,
                  dataEntries: 2,
                  allowedActions: readonly ? [] : ['update', 'delete'],
                })),
          },
        }
      if (requestUrl.searchParams.get('namespace') !== 'monitoring')
        return { status: 400, body: { error: { code: 'scope', message: 'Wrong namespace' } } }
      if (path === root + '/application-settings/detail') return { body: { data: detail } }
      if (path === root + '/application-settings/references')
        return {
          body: {
            data: [
              {
                kind: 'Pod',
                name: 'api-worker',
                namespace: 'monitoring',
                path: 'spec.volumes[0].configMap',
              },
            ],
          },
        }
      if (path === root + '/application-settings/yaml')
        return {
          body: {
            data: {
              content:
                'apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: application-settings\n  namespace: monitoring\ndata:\n  feature: enabled\n',
            },
          },
        }
      if (path === root + '/application-settings' && method === 'DELETE') {
        state.deleted = true
        return { body: { data: {} } }
      }
    }
    if (
      path === '/api/v1/clusters/cluster-a/resource-creation/scope-decision' &&
      method === 'POST'
    ) {
      const input = request.postDataJSON() as {
        resourceGroup: string
        kind: string
        action: string
        namespace?: string
      }
      expect(input).toMatchObject({
        resourceGroup: 'configuration',
        kind: 'ConfigMap',
        action: 'create',
      })
      expect([undefined, 'monitoring']).toContain(input.namespace)
      return {
        body: {
          data: {
            allowed: !readonly,
            allowedActions: readonly ? [] : ['create'],
            resourceScope: {
              clusterIds: ['cluster-a'],
              namespaces: ['monitoring'],
              resourceGroups: ['configuration'],
              resourceKinds: ['ConfigMap'],
            },
            capability: { key: 'resource.create', status: 'available', mode: 'agent' },
          },
        },
      }
    }
    return undefined
  })
  return { state, evidence }
}

test('ConfigMaps continuous list, namespace scope and detail navigation', async ({
  context,
  page,
}) => {
  const { evidence } = await setup(context, page)
  await page.goto('/configuration/configmaps?clusterId=cluster-a')
  await expect(page.getByRole('status').filter({ hasText: '42' })).toBeVisible()
  await page.getByRole('button', { name: '切换到命名空间 monitoring' }).first().click()
  await expect(page).toHaveURL(/namespace=monitoring/)
  await page.getByText('application-settings', { exact: true }).click()
  await expect(page.getByRole('heading', { name: 'application-settings' })).toBeVisible()
  await expect(page.getByRole('tab', { name: '数据', exact: true })).toBeVisible()
  verifyBrowserEvidence(evidence)
})

test('ConfigMap overview, searchable binary keys and lazy reference navigation', async ({
  context,
  page,
}) => {
  const { state, evidence } = await setup(context, page)
  await page.goto(url)
  await expect(page.getByRole('heading', { name: 'application-settings' })).toBeVisible()
  expect(state.reads.some((path) => /\/(references|yaml)\?/.test(path))).toBe(false)
  await expect(page.getByText('二进制键', { exact: true })).toBeVisible()
  await page.screenshot({ path: test.info().outputPath('configmap-overview.png'), fullPage: true })
  await page.getByRole('tab', { name: '数据', exact: true }).click()
  await page.getByRole('textbox', { name: '搜索数据键' }).fill('blob')
  const directory = page
    .getByRole('navigation')
    .filter({ has: page.getByRole('button', { name: /blob.bin/ }) })
  await expect(directory).toContainText('blob.bin')
  await expect(directory).not.toContainText('application.yaml')
  await page.getByRole('button', { name: /blob.bin/ }).click()
  await expect(page.getByText('Base64 · 只读', { exact: true })).toBeVisible()
  await page.getByRole('tab', { name: '关联资源', exact: true }).click()
  await page
    .getByRole('radiogroup', { name: '关联资源视图' })
    .getByText('列表', { exact: true })
    .click()
  await expect(page.getByRole('radio', { name: '列表', exact: true })).toBeChecked()
  await page.getByRole('button', { name: 'api-worker', exact: true }).click()
  const inspector = page.getByRole('complementary', { name: '选中资源信息' })
  await expect(inspector).toContainText('api-worker')
  await expect(inspector.getByRole('button', { name: '查看详情', exact: true })).toBeEnabled()
  expect(state.reads.some((path) => path.includes('/yaml?'))).toBe(false)
  verifyBrowserEvidence(evidence)
})

test('immutable ConfigMap has no edit or delete action and keeps YAML readable', async ({
  context,
  page,
}) => {
  const { state, evidence } = await setup(context, page, true)
  await page.goto(url)
  await expect(page.getByRole('heading', { name: 'application-settings' })).toBeVisible()
  await expect(page.getByRole('button', { name: '编辑数据', exact: true })).toHaveCount(0)
  await expect(page.getByRole('button', { name: '删除', exact: true })).toHaveCount(0)
  await page.getByRole('tab', { name: 'YAML', exact: true }).click()
  await expect(page.getByRole('button', { name: /^应\s*用$/ })).toBeDisabled()
  expect(state.writes).toEqual([])
  verifyBrowserEvidence(evidence)
})

test('ConfigMap YAML draft survives tab switches', async ({ context, page }) => {
  const { evidence } = await setup(context, page)
  await page.goto(url + '&tab=yaml')
  const editor = page.getByRole('textbox', { name: 'YAML 草稿编辑器' })
  await editor.focus()
  await expect(editor).toBeFocused()
  await editor.press('ControlOrMeta+End')
  await page.keyboard.type('\n# retained draft')
  await page.getByRole('tab', { name: '数据', exact: true }).click()
  await page.getByRole('tab', { name: 'YAML', exact: true }).click()
  await expect(page.getByRole('tabpanel', { name: 'YAML', exact: true })).toContainText(
    '# retained draft',
  )
  verifyBrowserEvidence(evidence)
})

test('an explicit header cluster switch replaces the pinned ConfigMap scope', async ({
  context,
  page,
}) => {
  const { evidence } = await setup(context, page)
  await page.goto(url + '&tab=data')
  await expect(page.getByRole('heading', { name: 'application-settings' })).toBeVisible()
  await page.locator('.soha-platform-scope-select.is-cluster').click()
  await page
    .locator('.ant-select-item-option-content:visible')
    .filter({ hasText: /^stage-cluster$/ })
    .click()
  await expect(page).toHaveURL(/clusterId=cluster-b&tab=data$/)
  await expect(page.getByRole('heading', { name: 'application-settings' })).toHaveCount(0)
  await page.locator('.soha-platform-scope-select.is-namespace').click()
  await page
    .locator('.ant-select-item-option-content:visible')
    .filter({ hasText: /^staging$/ })
    .click()
  await expect(page.getByRole('heading', { name: 'application-settings' })).toBeVisible()
  expect(evidence.requests).toContain(
    '/api/v1/clusters/cluster-b/configuration/configmaps/application-settings/detail?namespace=staging',
  )
  verifyBrowserEvidence(evidence)
})

for (const selectedNamespace of [null, 'monitoring']) {
  test(`ConfigMap return preserves ${selectedNamespace ?? 'all namespaces'} list scope`, async ({
    context,
    page,
  }) => {
    const { state, evidence } = await setup(context, page)
    await page.goto('/configuration/configmaps?clusterId=cluster-a')
    await expect(page.getByRole('status').filter({ hasText: '42' })).toBeVisible()
    if (selectedNamespace) {
      await page.getByRole('button', { name: '切换到命名空间 monitoring' }).first().click()
      await expect(page).toHaveURL(/namespace=monitoring/)
    }
    const scopeField = page.locator('.soha-platform-scope-select.is-namespace')
    const expectedScope = selectedNamespace ?? '全部命名空间'
    await expect(scopeField).toContainText(expectedScope)
    await page.getByText('application-settings', { exact: true }).click()
    await expect(page.getByRole('heading', { name: 'application-settings' })).toBeVisible()
    expect(
      state.reads.some(
        (path) => path === root + '/application-settings/detail?namespace=monitoring',
      ),
    ).toBe(true)
    await expect(scopeField).toContainText(expectedScope)
    await page.getByRole('tab', { name: '数据', exact: true }).click()
    // Refresh the document: the detail URL still identifies monitoring, not the list filter.
    await page.reload()
    await expect(page.getByRole('heading', { name: 'application-settings' })).toBeVisible()
    await expect(scopeField).toContainText(expectedScope)
    const back = page.getByRole('link', { name: '返回ConfigMap列表', exact: true })
    expect(
      new URL((await back.getAttribute('href'))!, 'http://127.0.0.1:4179').searchParams.get(
        'namespace',
      ),
    ).toBe(selectedNamespace)
    await back.click()
    await expect(page.getByRole('status').filter({ hasText: '42' })).toBeVisible()
    await expect(scopeField).toContainText(expectedScope)
    const readsBefore = state.reads.length
    await page.getByRole('button', { name: '刷新 ConfigMaps', exact: true }).click()
    await expect.poll(() => state.reads.length).toBeGreaterThan(readsBefore)
    const listRead = state.reads
      .filter((path) => new URL(path, 'http://localhost').pathname === root)
      .at(-1)!
    expect(new URL(listRead, 'http://localhost').searchParams.get('namespace')).toBe(
      selectedNamespace,
    )
    verifyBrowserEvidence(evidence)
  })
}
