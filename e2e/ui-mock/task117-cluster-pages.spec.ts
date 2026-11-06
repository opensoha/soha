import { test, expect, type Page, type BrowserContext } from '@playwright/test'
import { baselineResponse } from '../fixtures/baseline-data'
import { installMocks, verifyBrowserEvidence } from '../fixtures/mock-app'

const root = '/api/v1/clusters/cluster-a'
const detail = '/clusters/cluster-a'
async function setup(
  context: BrowserContext,
  page: Page,
  { readonly = false, noMetrics = false, count = 80, dedicated = false } = {},
) {
  const state = {
    reads: [] as string[],
    writes: [] as { method: string; path: string; body: Record<string, unknown> }[],
    failSave: 0,
    failMetrics: false,
    failDetail: false,
    failList: false,
    alias: '',
    label: '',
    environment: 'test',
    name: 'demo-prod',
    provider: 'standard_kubernetes',
    mode: 'direct_kubeconfig',
  }
  const summary = () => ({
    id: 'cluster-a',
    name: state.name,
    region: '',
    environment: state.environment,
    labels: state.provider ? { provider: state.provider } : {},
    connectionMode: state.mode,
    version: 'v1.36.3',
    capabilities: ['nodes', 'pods'],
    health: { status: 'healthy', lastChecked: new Date().toISOString() },
  })
  await context.addInitScript(() =>
    localStorage.setItem(
      'soha-scope',
      JSON.stringify({ state: { clusterId: 'cluster-a', namespace: null }, version: 0 }),
    ),
  )
  const evidence = await installMocks(context, page, (url, method, request) => {
    const path = url.pathname
    if (method === 'GET') state.reads.push(path)
    else if (path.startsWith('/api/v1/clusters'))
      state.writes.push({ method, path, body: request.postDataJSON() || {} })
    if (path === '/api/v1/access/permission-snapshot') {
      const body = baselineResponse(path)!
      if (!('permissionKeys' in body.data)) throw new Error('Missing permission fixture')
      body.data.permissionKeys!.push(
        'platform.nodes.view',
        ...(readonly
          ? []
          : ['platform.clusters.create', 'platform.clusters.update', 'platform.clusters.delete']),
        ...(noMetrics ? [] : ['platform.resource-monitoring.overview.view']),
      )
      return { body }
    }
    if (path === '/api/v1/clusters' && method === 'GET')
      return state.failList
        ? {
            status: 503,
            body: { error: { code: 'unavailable', message: 'Clusters could not be read' } },
          }
        : {
            body: {
              data: [
                summary(),
                ...Array.from({ length: count - 1 }, (_, i) => ({
                  ...summary(),
                  id: 'cluster-' + (i + 1),
                  name: 'test-' + String(i + 1).padStart(3, '0'),
                })),
              ],
            },
          }
    if (path === root + '/detail')
      return state.failDetail
        ? {
            status: 503,
            body: { error: { code: 'unavailable', message: 'Cluster detail could not be read' } },
          }
        : {
            body: {
              data: {
                summary: summary(),
                connection: {
                  mode: state.mode,
                  credentialType: 'kubeconfig',
                  sourceType: 'api',
                  hasInlineKubeconfig: true,
                  hasToken: false,
                  usesInformerCache: true,
                },
                diagnostics: {
                  connectionState: 'connected',
                  cacheStatus: 'ready',
                  syncStrategy: 'watch',
                },
                monitoring: {
                  prometheus: {
                    baseUrl: 'http://metrics.example',
                    clusterLabel: state.label,
                    clusterLabelValue: state.alias,
                    hasBearerToken: true,
                    transport: 'direct',
                  },
                },
              },
            },
          }
    if (path === '/api/v1/clusters' && method === 'POST') return { body: { data: summary() } }
    if (path === root && method === 'PUT') {
      if (state.failSave)
        return {
          status: state.failSave,
          body: { error: { code: 'conflict', message: 'Cluster configuration changed' } },
        }
      const body = request.postDataJSON()
      state.alias = body.prometheusClusterLabelValue || ''
      state.label = body.prometheusClusterLabel || ''
      state.environment = body.environment || ''
      state.name = body.name || state.name
      return { body: { data: summary() } }
    }
    if (path === root && method === 'DELETE') return { body: { data: {} } }
    if (path === root + '/infrastructure/nodes')
      return {
        body: {
          items: [
            {
              name: 'worker-a',
              status: 'Ready',
              roles: ['worker'],
              internalIp: '10.0.0.1',
              version: 'v1.36.3',
              podCount: 2,
              ageSeconds: 60,
            },
          ],
        },
      }
    if (path === root + '/resource-monitoring') {
      if (state.failMetrics)
        return {
          status: 503,
          body: { error: { code: 'unavailable', message: 'Monitoring read failed' } },
        }
      const valid = dedicated
        ? state.label === '' && state.alias === ''
        : state.label === 'cluster' && state.alias === 'middleware-prod'
      const ref = {
        clusterId: 'cluster-a',
        kind: 'Node',
        name: 'worker-a',
        uid: 'node-uid',
        namespace: '',
      }
      const now = new Date().toISOString()
      return {
        body: {
          data: {
            clusterId: 'cluster-a',
            view: 'overview',
            from: now,
            to: now,
            generatedAt: now,
            stepSeconds: 60,
            snapshotQuality: 'valid',
            truncated: false,
            nodes: [
              {
                nodeRef: ref,
                quality: 'valid',
                capacity: {},
                allocatable: {},
                requests: {},
                limits: {},
              },
            ],
            panels: ['host.cpu.used', 'host.memory.used'].map((key, i) => ({
              key,
              label: key,
              unit: i ? 'bytes' : 'cores',
              quality: valid ? 'valid' : 'not_collected',
              reasonCode: valid ? '' : 'metric_not_collected',
              series: valid
                ? [
                    {
                      nodeRef: ref,
                      metricKey: key,
                      quality: 'valid',
                      unit: i ? 'bytes' : 'cores',
                      source: 'prometheus',
                      sampledAt: now,
                      latest: i ? 1024 : 0,
                      points: [{ timestamp: now, value: i ? 1024 : 0 }],
                    },
                  ]
                : [],
            })),
          },
        },
      }
    }
    if (path === root + '/observability/logging')
      return {
        body: {
          data: {
            clusterId: 'cluster-a',
            mode: 'runtime_only',
            status: 'disabled',
            collector: { status: 'unknown' },
            backend: { status: 'unknown' },
            endToEnd: { status: 'unknown' },
            historyPreserved: true,
          },
        },
      }
    return undefined
  })
  return { state, evidence }
}
async function edit(page: Page) {
  await page.getByRole('button', { name: '编辑集群', exact: true }).click()
  await expect(page.getByRole('dialog', { name: '编辑集群', exact: true })).toBeVisible()
}
async function fillIdentity(page: Page, value = 'middleware-prod') {
  await page.getByRole('textbox', { name: '集群标签名（选填）', exact: true }).fill('cluster')
  await page.getByRole('textbox', { name: '集群标签值', exact: true }).fill(value)
}
async function clickDone(page: Page) {
  await page.getByRole('button', { name: /^完\s*成$/ }).click()
  await expect(page.getByRole('dialog')).toHaveCount(0)
}

test('Clusters retain continuous browsing, filtering, row menus and cancelable unregister', async ({
  context,
  page,
}) => {
  const { state, evidence } = await setup(context, page)
  await page.goto('/clusters')
  await expect(page.getByRole('button', { name: '更多操作：demo-prod', exact: true })).toBeVisible()
  expect(await page.locator('.soha-clusters-page .ant-table-row').count()).toBeLessThan(80)
  await page.getByRole('textbox', { name: '搜索名称 / 环境 / 版本', exact: true }).fill('demo-prod')
  await page.getByRole('button', { name: /^查\s*询$/ }).click()
  await expect(page.getByRole('status').filter({ hasText: '80' }).first()).toBeVisible()
  await expect(page.getByRole('button', { name: /^更多操作：/ })).toHaveCount(1)
  await page.getByRole('button', { name: '更多操作：demo-prod', exact: true }).click()
  await page.getByRole('menuitem', { name: '删除集群', exact: true }).click()
  await expect(page.getByRole('dialog')).toContainText('不会删除 Kubernetes 集群')
  await page.getByRole('button', { name: /^取\s*消$/ }).click()
  expect(state.writes).toHaveLength(0)
  verifyBrowserEvidence(evidence)
})

test('Cluster detail shares the five-tab frame and reads logs only on their tab', async ({
  context,
  page,
}) => {
  const { state, evidence } = await setup(context, page)
  await page.goto(detail)
  await expect(page.getByRole('heading', { name: 'demo-prod', exact: true })).toBeVisible()
  await expect(page.getByRole('tab')).toHaveCount(5)
  expect(state.reads).not.toContain(root + '/observability/logging')
  await page.getByRole('tab', { name: '连接与 Agent', exact: true }).click()
  await expect(page.getByRole('tabpanel')).toContainText('已连接')
  await page.getByRole('tab', { name: '日志采集', exact: true }).click()
  await expect(page.getByRole('tabpanel')).toContainText('runtime_only')
  await page.getByRole('tab', { name: '节点', exact: true }).click()
  await expect(page.getByRole('tabpanel')).toContainText('worker-a')
  await page.getByRole('tab', { name: '监控接入', exact: true }).click()
  await expect(page.getByRole('tabpanel')).toContainText('集群标签值')
  verifyBrowserEvidence(evidence)
})

test('Cluster editor saves exact metric identity and verifies real zero samples', async ({
  context,
  page,
}) => {
  const { state, evidence } = await setup(context, page)
  await page.goto(detail + '?tab=monitoring')
  await edit(page)
  await fillIdentity(page)
  await expect(page.getByLabel('Prometheus Token', { exact: true })).toHaveValue('')
  await page.getByRole('button', { name: '保存并校验', exact: true }).click()
  await expect(page.getByRole('dialog')).toContainText('集群配置已保存')
  await expect(page.getByRole('dialog')).toContainText('主机指标已接通')
  expect(state.writes.filter((w) => w.method === 'PUT')).toHaveLength(1)
  expect(state.writes[0].body).toMatchObject({
    prometheusClusterLabel: 'cluster',
    prometheusClusterLabelValue: 'middleware-prod',
  })
  await clickDone(page)
  await expect(page.getByRole('tabpanel')).toContainText('middleware-prod')
  verifyBrowserEvidence(evidence)
})

test('Imported clusters without a type can save monitoring without changing their classification', async ({
  context,
  page,
}) => {
  const { state, evidence } = await setup(context, page)
  state.provider = ''
  await page.goto(detail + '?tab=monitoring')
  await edit(page)
  await fillIdentity(page)
  await page.getByRole('button', { name: '保存并校验', exact: true }).click()
  await expect(page.getByRole('dialog')).toContainText('主机指标已接通')
  expect(state.writes.filter((w) => w.method === 'PUT')).toHaveLength(1)
  expect(state.writes[0].body.labels).toEqual({})
  await clickDone(page)
  verifyBrowserEvidence(evidence)
})

test('Cluster editor clears a saved label to check a dedicated Prometheus source', async ({
  context,
  page,
}) => {
  const { state, evidence } = await setup(context, page, { dedicated: true })
  state.label = 'cluster'
  state.alias = 'middleware-prod'
  await page.goto(detail + '?tab=monitoring')
  await edit(page)
  const label = page.getByRole('textbox', { name: '集群标签名（选填）', exact: true })
  const value = page.getByRole('textbox', { name: '集群标签值', exact: true })
  await expect(value).toHaveValue('middleware-prod')
  await label.clear()
  await expect(value).toBeDisabled()
  await expect(value).toHaveValue('')
  await page.getByRole('button', { name: '保存并校验', exact: true }).click()
  await expect(page.getByRole('dialog')).toContainText('主机指标已接通')
  expect(state.writes).toHaveLength(1)
  expect(state.writes[0].body).toMatchObject({
    prometheusBaseUrl: 'http://metrics.example',
    prometheusClusterLabel: '',
    prometheusClusterLabelValue: '',
  })
  await clickDone(page)
  verifyBrowserEvidence(evidence)
})

test('Wrong metric identity remains a saved but unsuccessful connection and can be corrected', async ({
  context,
  page,
}) => {
  const { state, evidence } = await setup(context, page)
  await page.goto(detail + '?tab=monitoring')
  await edit(page)
  await fillIdentity(page, 'wrong-cluster')
  await page.getByRole('button', { name: '保存并校验', exact: true }).click()
  await expect(page.getByRole('dialog')).toContainText('主机指标尚未全部接通')
  await expect(page.getByRole('dialog')).toContainText('CPU 0/1 个节点')
  expect(state.alias).toBe('wrong-cluster')
  await page.getByRole('button', { name: '继续编辑', exact: true }).click()
  await fillIdentity(page)
  await page.getByRole('button', { name: '保存并校验', exact: true }).click()
  await expect(page.getByRole('dialog')).toContainText('主机指标已接通')
  await clickDone(page)
  verifyBrowserEvidence(evidence)
})

test('Monitoring query failure never becomes a successful connection or an unsaved configuration', async ({
  context,
  page,
}) => {
  const { state, evidence } = await setup(context, page)
  state.failMetrics = true
  await page.goto(detail + '?tab=monitoring')
  await edit(page)
  await fillIdentity(page)
  await page.getByRole('button', { name: '保存并校验', exact: true }).click()
  await expect(page.getByRole('dialog')).toContainText('集群配置已保存')
  await expect(page.getByRole('dialog')).toContainText('读取失败')
  await expect(page.getByRole('dialog')).not.toContainText('主机指标已接通')
  state.failMetrics = false
  await clickDone(page)
  await page.getByRole('button', { name: '校验接入', exact: true }).click()
  await expect(page.getByRole('tabpanel')).toContainText('主机指标已接通')
  verifyBrowserEvidence(evidence)
})

test('Read-only cluster users cannot mutate or check metrics without the exact permission', async ({
  context,
  page,
}) => {
  const { state, evidence } = await setup(context, page, { readonly: true, noMetrics: true })
  await page.goto(detail + '?tab=monitoring')
  await expect(page.getByRole('heading', { name: 'demo-prod', exact: true })).toBeVisible()
  await expect(page.getByRole('button', { name: '编辑集群', exact: true })).toHaveCount(0)
  await expect(page.getByRole('button', { name: '校验接入', exact: true })).toBeDisabled()
  expect(state.reads).not.toContain(root + '/resource-monitoring')
  expect(state.writes).toHaveLength(0)
  await page.goto('/clusters')
  await page.getByRole('button', { name: '更多操作：demo-prod', exact: true }).click()
  await expect(page.getByRole('menuitem', { name: '编辑集群', exact: true })).toHaveCount(0)
  await expect(page.getByRole('menuitem', { name: '删除集群', exact: true })).toHaveCount(0)
  verifyBrowserEvidence(evidence)
})

test('Cluster edit failures preserve the draft and do not silently save or check', async ({
  context,
  page,
}) => {
  const { state, evidence } = await setup(context, page)
  state.failSave = 409
  await page.goto(detail)
  await edit(page)
  await fillIdentity(page)
  const editor = page.getByRole('dialog', { name: '编辑集群', exact: true })
  // Ant Design's animated loading icon can contribute to the accessible name.
  const saveAndCheck = editor.getByRole('button').filter({ hasText: /^保存并校验$/ })
  const updates = () => state.writes.filter(({ method, path }) => method === 'PUT' && path === root)
  await saveAndCheck.click()
  await expect(editor).toContainText('Cluster configuration changed')
  await expect(editor.getByRole('textbox', { name: '集群标签值', exact: true })).toHaveValue(
    'middleware-prod',
  )
  expect(updates()).toHaveLength(1)
  expect(state.reads).not.toContain(root + '/resource-monitoring')
  state.failSave = 0
  await saveAndCheck.click()
  await expect(editor).toContainText('主机指标已接通')
  expect(updates()).toHaveLength(2)
  await clickDone(page)
  verifyBrowserEvidence(evidence)
})

test('Cluster header, tabs and content align in dark and narrow views', async ({
  context,
  page,
}) => {
  const { evidence } = await setup(context, page)
  await page.goto(detail)
  await expect(page.getByRole('heading', { name: 'demo-prod', exact: true })).toBeVisible()
  for (const width of [1440, 900, 375]) {
    await page.setViewportSize({ width, height: 1100 })
    await expect
      .poll(() =>
        page.evaluate(() => {
          const h = document.querySelector('.soha-workload-detail-header')!.getBoundingClientRect()
          const nav = document
            .querySelector('.soha-cluster-detail-page > .ant-tabs > .ant-tabs-nav')!
            .getBoundingClientRect()
          const content = document
            .querySelector(
              '.soha-cluster-detail-page > .ant-tabs > :is(.ant-tabs-body-holder,.ant-tabs-content-holder)',
            )!
            .getBoundingClientRect()
          return (
            Math.abs(h.bottom - nav.top) <= 1 &&
            Math.abs(h.left - content.left) <= 1 &&
            Math.abs(h.right - content.right) <= 1 &&
            document.documentElement.scrollWidth <= innerWidth
          )
        }),
      )
      .toBe(true)
  }
  await page.getByRole('button', { name: '切换到深色模式', exact: true }).click()
  await page.getByRole('tab', { name: '监控接入', exact: true }).click()
  await expect(page.getByRole('tabpanel')).toContainText('集群标签值')
  verifyBrowserEvidence(evidence)
})

test('Cluster editor keeps actions visible in compact, narrow and dark layouts', async ({
  context,
  page,
}) => {
  const { evidence } = await setup(context, page)
  await page.goto(detail)
  await edit(page)
  const dialog = page.getByRole('dialog', { name: '编辑集群', exact: true })
  await expect(dialog.getByRole('textbox', { name: /^\*?\s*Kubeconfig$/ })).toBeHidden()
  await expect(dialog.getByText('Kubeconfig 已配置；留空沿用现有凭据。')).toBeVisible()
  await expect(dialog.getByRole('combobox', { name: '访问方式', exact: true })).toHaveCount(0)
  for (const [width, height] of [
    [1440, 900],
    [900, 720],
    [375, 640],
  ]) {
    await page.setViewportSize({ width, height })
    const tokenToggle = dialog.getByRole('button', { name: /^(更换 Token|收起 Token)$/ })
    if ((await tokenToggle.getAttribute('aria-expanded')) === 'false') await tokenToggle.click()
    await dialog.getByLabel('Prometheus Token', { exact: true }).fill('synthetic-token')
    await expect(dialog.getByRole('button', { name: '保存并校验', exact: true })).toBeInViewport()
    const geometry = await dialog.evaluate((el) => {
      const rect = el.getBoundingClientRect()
      const footer = el.querySelector('.soha-cluster-editor__footer')!.getBoundingClientRect()
      const body = el.querySelector('.soha-cluster-editor__body')!
      return {
        bottom: rect.bottom,
        footerBottom: footer.bottom,
        footerInsideBody: body.contains(el.querySelector('.soha-cluster-editor__footer')),
        overflow: document.documentElement.scrollWidth > innerWidth,
      }
    })
    expect(geometry.bottom).toBeLessThanOrEqual(height)
    expect(geometry.footerBottom).toBeLessThanOrEqual(height)
    expect(geometry.footerInsideBody).toBe(false)
    expect(geometry.overflow).toBe(false)
  }
  await dialog.getByRole('button', { name: /^取\s*消$/ }).click()
  await page.getByRole('button', { name: '切换到深色模式', exact: true }).click()
  await edit(page)
  await expect(dialog.getByRole('button', { name: '保存并校验', exact: true })).toBeInViewport()
  verifyBrowserEvidence(evidence)
})

test('Credential disclosure preserves the draft and Enter submits through the fixed footer', async ({
  context,
  page,
}) => {
  const { state, evidence } = await setup(context, page)
  await page.goto(detail)
  await edit(page)
  const dialog = page.getByRole('dialog', { name: '编辑集群', exact: true })
  await dialog.getByRole('button', { name: '更换 Kubeconfig', exact: true }).click()
  const kubeconfig = dialog.getByRole('textbox', { name: /^\*?\s*Kubeconfig$/ })
  await kubeconfig.fill('synthetic-kubeconfig-draft')
  await dialog.getByRole('button', { name: '收起', exact: true }).click()
  await expect(kubeconfig).toBeHidden()
  await dialog.getByRole('button', { name: '更换 Kubeconfig', exact: true }).click()
  await expect(kubeconfig).toHaveValue('synthetic-kubeconfig-draft')
  await dialog.getByRole('button', { name: '更换 Token', exact: true }).click()
  await dialog.getByLabel('Prometheus Token', { exact: true }).fill('synthetic-token-draft')
  await dialog.getByRole('button', { name: '收起 Token', exact: true }).click()
  await fillIdentity(page)
  await dialog.getByRole('textbox', { name: '集群标签值', exact: true }).press('Enter')
  await expect(dialog).toContainText('集群配置已保存')
  await expect(dialog).toContainText('主机指标已接通')
  expect(state.writes[0].body).toMatchObject({
    kubeconfig: 'synthetic-kubeconfig-draft',
    prometheusBearerToken: 'synthetic-token-draft',
  })
  await page.getByRole('button', { name: '继续编辑', exact: true }).click()
  await expect(dialog).not.toContainText('集群配置已保存')
  await dialog.getByRole('button', { name: '更换 Kubeconfig', exact: true }).click()
  await expect(kubeconfig).toHaveValue('')
  verifyBrowserEvidence(evidence)
})

test('Create keeps required credentials visible and validates before posting', async ({
  context,
  page,
}) => {
  const { state, evidence } = await setup(context, page)
  await page.goto('/clusters')
  await page.getByRole('button', { name: 'plus 创建', exact: true }).click()
  const dialog = page.getByRole('dialog', { name: '添加集群', exact: true })
  await expect(dialog.getByRole('textbox', { name: /^\*?\s*Kubeconfig$/ })).toBeVisible()
  await dialog.getByRole('textbox', { name: /集群名称/ }).fill('new-direct-cluster')
  await dialog.getByRole('button', { name: '保存并校验', exact: true }).click()
  await expect(dialog).toContainText('请输入 Kubeconfig')
  expect(state.writes).toHaveLength(0)
  await dialog.getByRole('textbox', { name: /^\*?\s*Kubeconfig$/ }).fill('synthetic-kubeconfig')
  await dialog.getByRole('button', { name: '保存并校验', exact: true }).click()
  await expect(page.getByRole('dialog')).toHaveCount(0)
  expect(state.writes).toHaveLength(1)
  expect(state.writes[0].method).toBe('POST')
  verifyBrowserEvidence(evidence)
})

test('Fixed footer preserves pending submission fences and the exact target', async ({
  context,
  page,
}) => {
  const { state, evidence } = await setup(context, page)
  let release!: () => void
  const saved = new Promise<void>((resolve) => {
    release = resolve
  })
  let received = 0
  await page.route('**/api/v1/clusters/cluster-a', async (route) => {
    if (route.request().method() === 'PUT') {
      received++
      await saved
    }
    await route.fallback()
  })
  await page.goto(detail)
  await edit(page)
  await fillIdentity(page)
  const dialog = page.getByRole('dialog', { name: '编辑集群', exact: true })
  await dialog.getByRole('button', { name: '保存并校验', exact: true }).click()
  await expect(dialog.getByRole('button', { name: /保存并校验/ })).toBeDisabled()
  await expect(dialog.getByRole('button', { name: /^取\s*消$/ })).toBeDisabled()
  await expect(dialog.getByRole('button', { name: '关闭', exact: true })).toBeDisabled()
  await page.keyboard.press('Escape')
  await expect(dialog).toBeVisible()
  expect(received).toBe(1)
  release()
  await expect(dialog).toContainText('主机指标已接通')
  expect(state.writes).toHaveLength(1)
  expect(state.writes[0].path).toBe(root)
  verifyBrowserEvidence(evidence)
})

test('Switching an Agent cluster to direct mode exposes required credentials', async ({
  context,
  page,
}) => {
  const { state, evidence } = await setup(context, page)
  state.mode = 'agent'
  await page.goto(detail)
  await edit(page)
  const dialog = page.getByRole('dialog', { name: '编辑集群', exact: true })
  await dialog.getByRole('combobox', { name: '连接方式', exact: true }).click()
  await page
    .locator('.ant-select-dropdown:visible')
    .getByText('直接 Kubeconfig', { exact: true })
    .click()
  await expect(dialog.getByRole('textbox', { name: /^\*?\s*Kubeconfig$/ })).toBeVisible()
  await dialog.getByRole('button', { name: '保存并校验', exact: true }).click()
  await expect(dialog).toContainText('请输入 Kubeconfig')
  expect(state.writes).toHaveLength(0)
  verifyBrowserEvidence(evidence)
})
