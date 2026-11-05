import { test, expect, type BrowserContext, type Page } from '@playwright/test'
import { baselineResponse } from '../fixtures/baseline-data'
import { installMocks, verifyBrowserEvidence } from '../fixtures/mock-app'
import { expectWorkloadDetailFrame } from '../fixtures/workload-detail-geometry'

const prefix = '/api/v1/clusters/cluster-a/workloads/replicationcontrollers/report-abc'
const path = '/workloads/replicationcontrollers/report-abc?clusterId=cluster-a&namespace=monitoring'
async function setup(context: BrowserContext, page: Page, { readonly = false } = {}) {
  const pods = Array.from({ length: 40 }, (_, i) => ({
    name: `report-${String(i).padStart(3, '0')}`,
    namespace: 'monitoring',
    phase: 'Running',
    nodeName: 'worker-a',
    readyContainers: '1/1',
    restarts: 0,
    ageSeconds: 600,
    allowedActions: ['view'],
  }))
  const detail = {
    name: 'report-abc',
    namespace: 'monitoring',
    desiredReplicas: 40,
    currentReplicas: 40,
    readyReplicas: 40,
    availableReplicas: 40,
    createdAt: '2026-10-31T00:00:00Z',
    annotations: { 'deployment.kubernetes.io/revision': '12' },
    labels: { app: 'report' },
    selector: { app: 'report' },
    pods,
    relatedResources: [
      {
        kind: 'ConfigMap',
        name: 'report-config',
        namespace: 'monitoring',
        relation: 'uses-config',
      },
    ],
    allowedActions: readonly ? ['view'] : ['view', 'delete', 'update'],
  }
  const state = {
    detail,
    failDetail: false,
    failConfig: false,
    failDelete: false,
    bindings: [] as {
      id: string
      applicationId: string
      environmentKey: string
      targets: {
        clusterId: string
        namespace: string
        workloadName: string
        workloadKind: string
        enabled?: boolean
      }[]
    }[],
    metrics: {
      configured: true,
      available: true,
      source: 'prometheus',
      rangeMinutes: 60,
      series: [
        { key: 'cpu', unit: 'cores', latest: 0, points: [] },
        { key: 'memory', unit: 'bytes', latest: 1048576, points: [] },
      ],
    },
    reads: [] as string[],
    writes: [] as string[],
  }
  await context.addInitScript(() =>
    localStorage.setItem(
      'soha-scope',
      JSON.stringify({ state: { clusterId: 'cluster-a', namespace: 'monitoring' }, version: 0 }),
    ),
  )
  const evidence = await installMocks(context, page, (url, method) => {
    const endpoint = url.pathname
    if (method === 'GET') state.reads.push(endpoint + url.search)
    if (endpoint === '/api/v1/access/permission-snapshot') {
      const body = baselineResponse(endpoint)!
      if (!('permissionKeys' in body.data)) throw new Error('Permission fixture missing')
      body.data.permissionKeys = body.data.permissionKeys!.filter(
        (k) =>
          !k.startsWith('platform.workloads.replication-controllers.') &&
          !k.startsWith('platform.deployment.') &&
          !k.startsWith('delivery.'),
      )
      body.data.permissionKeys.push(
        'platform.workloads.replication-controllers.view',
        'platform.pods.view',
        'platform.configuration.config-maps.view',
        ...(readonly
          ? []
          : [
              'platform.workloads.replication-controllers.delete',
              'platform.workloads.replication-controllers.update',
              'platform.deployment.view',
              'delivery.applications.view',
            ]),
      )
      return { body }
    }
    if (endpoint === '/api/v1/clusters/capabilities')
      return {
        body: {
          data: ['resource.yaml.view', 'resource.yaml.apply', 'workload.mutations'].map((key) => ({
            key,
            direct: { status: 'available' },
            agent: { status: 'available' },
          })),
        },
      }
    if (endpoint === '/api/v1/clusters/cluster-a/workloads/replicationcontrollers')
      return { body: { items: [{ ...state.detail, ageSeconds: 600 }] } }
    if (
      endpoint === '/api/v1/clusters/cluster-a/resources/graph' &&
      method === 'GET' &&
      url.searchParams.get('namespace') === 'monitoring' &&
      url.searchParams.get('kind') === 'ReplicationController' &&
      url.searchParams.get('name') === 'report-abc'
    )
      return {
        body: {
          data: {
            rootId: 'root',
            clusterId: 'cluster-a',
            generatedAt: '2026-11-05T00:00:00Z',
            warnings: [],
            evidence: [],
            nodes: [
              ['root', 'ReplicationController', 'report-abc'],
              ['config', 'ConfigMap', 'report-config'],
            ].map(([id, kind, name]) => ({
              id,
              resource: {
                clusterId: 'cluster-a',
                namespace: kind === 'Node' ? undefined : 'monitoring',
                scopeMode: kind === 'Node' ? 'cluster' : 'namespace',
                apiVersion: ['Deployment', 'StatefulSet', 'DaemonSet', 'ReplicaSet'].includes(kind)
                  ? 'apps/v1'
                  : ['Job', 'CronJob'].includes(kind)
                    ? 'batch/v1'
                    : 'v1',
                kind,
                name,
              },
            })),
            edges: [['root', 'config', 'uses-config']].map(
              ([sourceId, targetId, relation], index) => ({
                id: String(index),
                sourceId,
                targetId,
                relation,
              }),
            ),
          },
        },
      }
    if (endpoint === prefix + '/detail')
      return state.failDetail
        ? {
            status: 503,
            body: { error: { code: 'unavailable', message: 'ReplicationController read failed' } },
          }
        : { body: { data: state.detail } }
    if (endpoint === prefix + '/yaml')
      return state.failConfig
        ? { status: 503, body: { error: { code: 'unavailable', message: 'Template read failed' } } }
        : {
            body: {
              data: {
                kind: 'ReplicationController',
                name: 'report-abc',
                namespace: 'monitoring',
                content:
                  'apiVersion: v1\nkind: ReplicationController\nmetadata: {name: report-abc, namespace: monitoring, generation: 3}\nspec: {minReadySeconds: 0, template: {spec: {initContainers: [{name: setup, image: busybox:1.38}], containers: [{name: app, image: report:v12}]}}}\nstatus: {replicas: 40, observedGeneration: 3, fullyLabeledReplicas: 40, conditions: []}',
              },
            },
          }
    if (endpoint === '/api/v1/delivery/manifest-deployments') return { body: { items: [] } }
    if (endpoint === '/api/v1/application-environments') return { body: { data: state.bindings } }
    if (endpoint === '/api/v1/clusters/cluster-a/workloads/pods')
      return {
        body: {
          items: [
            ...pods.map((p) => ({ ...p, cpu: '50m', memory: '128Mi' })),
            { ...pods[0], namespace: 'other', cpu: '999m' },
          ],
        },
      }
    if (endpoint === prefix + '/metrics') return { body: { data: state.metrics } }
    if (endpoint === '/api/v1/clusters/cluster-a/events')
      return {
        body: {
          items: [
            {
              name: 'scaled',
              namespace: 'monitoring',
              involvedKind: 'ReplicationController',
              involvedName: 'report-abc',
              reason: 'SuccessfulCreate',
              message: 'Created owned Pod',
              type: 'Normal',
              count: 1,
              ageSeconds: 30,
            },
          ],
        },
      }
    if (endpoint === prefix && method === 'DELETE') {
      state.writes.push(endpoint + url.search)
      return state.failDelete
        ? {
            status: 500,
            body: { error: { code: 'failed', message: 'ReplicationController delete failed' } },
          }
        : { body: { data: {} } }
    }
    return undefined
  })
  page.on('pageerror', (error) => evidence.fatal.push(error.stack || error.message))
  page.on('console', (entry) => {
    if (entry.type() === 'error' && /TextModel got disposed|DiffEditorWidget/.test(entry.text()))
      evidence.fatal.push('Monaco model lifecycle')
  })
  return { state, evidence }
}

test('ReplicationController six tabs, complete Pods, events and resource frame', async ({
  context,
  page,
}) => {
  await page.setViewportSize({ width: 1373, height: 1502 })
  const { state, evidence } = await setup(context, page)
  await page.goto(path)
  await expectWorkloadDetailFrame(page)
  await expect(page.getByRole('tab')).toHaveCount(6)
  await expect(page.getByText('当前返回的应用环境没有绑定此 RC', { exact: true })).toBeVisible()
  await expect(page.getByRole('tab', { name: '版本历史', exact: true })).toHaveCount(0)
  await expect(page.getByText('report:v12', { exact: false }).first()).toBeVisible()
  await page.screenshot({
    path: '../preview/runtime-private/wl08-replicationcontrollers/synthetic-overview.png',
    fullPage: true,
  })

  await page.getByRole('tab', { name: 'Pods', exact: true }).click()
  await expect(page.locator('.ant-table-tbody-virtual-holder')).toBeVisible()
  await expect(page.locator('.ant-pagination')).toHaveCount(0)
  await page.getByRole('textbox', { name: '搜索 Pod' }).fill('report-039')
  await expect(page.getByRole('link', { name: 'report-039', exact: true })).toBeVisible()
  await expect(page.getByText('50m', { exact: true })).toBeVisible()
  await page.getByRole('button', { name: '刷新 ReplicationController', exact: true }).click()
  await expect(page.getByRole('textbox', { name: '搜索 Pod' })).toHaveValue('report-039')
  await page.getByRole('tab', { name: '事件', exact: true }).click()
  await expect(page.getByText('Created owned Pod', { exact: true })).toBeVisible()
  await page.getByRole('tab', { name: '关联资源', exact: true }).click()
  await expect(
    page.getByRole('tabpanel', { name: '关联资源', exact: true }).locator('.react-flow'),
  ).toBeVisible()
  await expect(
    page
      .getByRole('tabpanel', { name: '关联资源', exact: true })
      .locator('.react-flow__node[data-id="config"]'),
  ).toContainText('report-config')
  expect(state.reads.filter((p) => p.includes('/resources/graph'))).toEqual([
    '/api/v1/clusters/cluster-a/resources/graph?namespace=monitoring&kind=ReplicationController&name=report-abc',
  ])
  await page
    .getByRole('radiogroup', { name: '关联资源视图' })
    .getByText('列表', { exact: true })
    .click()
  await expect(page.getByRole('radio', { name: '列表', exact: true })).toBeChecked()
  await page.getByRole('tab', { name: 'YAML', exact: true }).click()
  await expect(page.locator('.soha-k8s-yaml-editor .view-lines:visible').first()).toContainText(
    'kind: ReplicationController',
  )
  const editor = page.getByRole('textbox', { name: 'YAML 草稿编辑器', exact: true })
  await editor.press('ControlOrMeta+Home')
  await editor.pressSequentially('# rc-draft')
  await editor.press('Enter')
  const comparison = page.getByRole('radiogroup', { name: 'YAML 对比方式' })
  await comparison.getByText('双栏对比', { exact: true }).click()
  await comparison.getByText('变更审阅', { exact: true }).click()
  await comparison.getByText('编辑', { exact: true }).click()
  await expect(page.locator('.soha-k8s-yaml-editor .view-lines:visible').first()).toContainText(
    'rc-draft',
  )
  expect(state.writes).toEqual([])
  await page.getByRole('tab', { name: '概览', exact: true }).click()
  await expect(page.getByRole('tab', { name: '概览', exact: true })).toHaveAttribute(
    'aria-selected',
    'true',
  )
  await verifyBrowserEvidence(evidence)
})

test('ReplicationController metric zero, missing monitoring and range persistence', async ({
  context,
  page,
}) => {
  const { state, evidence } = await setup(context, page)
  await page.goto(path + '&tab=metrics')
  await expect(page.locator('.soha-pod-detail-metric-card')).toHaveCount(4)
  await expect(page.locator('.soha-pod-detail-metric-card').first()).toContainText('0 mCPU')
  await page.getByText('15 分钟', { exact: true }).click()
  await expect(page).toHaveURL(/range=15/)
  await expect
    .poll(() =>
      state.reads.some((p) => {
        const url = new URL(p, 'http://test')
        return (
          url.pathname.endsWith('/metrics') &&
          url.searchParams.get('namespace') === 'monitoring' &&
          url.searchParams.get('rangeMinutes') === '15'
        )
      }),
    )
    .toBe(true)
  state.metrics.configured = false
  await page.getByRole('button', { name: '刷新指标', exact: true }).click()
  await expect(page.getByText('尚未接入监控', { exact: true })).toBeVisible()
  await verifyBrowserEvidence(evidence)
})

test('ReplicationController zero, unknown and unavailable counts stay explicit', async ({
  context,
  page,
}) => {
  const { state, evidence } = await setup(context, page)
  state.detail.pods = []
  state.detail.desiredReplicas = 0
  state.detail.currentReplicas = 0
  state.detail.readyReplicas = 0
  state.detail.availableReplicas = 0
  await page.goto(path)
  await expect(page.locator('.soha-workload-detail-header .soha-status-tag')).toHaveText('期望为 0')
  await page.getByRole('tab', { name: 'Pods', exact: true }).click()
  await expect(page.getByText('当前返回数据中没有 Pod', { exact: true })).toBeVisible()
  state.detail.desiredReplicas = 3
  state.detail.readyReplicas = 3
  state.detail.currentReplicas = 3
  state.detail.availableReplicas = 1
  await page.goto(path)
  await expect(page.locator('.soha-workload-detail-header .soha-status-tag')).toHaveText(
    '副本未就绪或不可用',
  )
  Reflect.deleteProperty(state.detail, 'currentReplicas')
  await page.reload()
  await expect(page.locator('.soha-workload-detail-header .soha-status-tag')).toHaveText('状态未知')
  await verifyBrowserEvidence(evidence)
})
test('ReplicationController delivery requires an exact returned active RC binding', async ({
  context,
  page,
}) => {
  const { state, evidence } = await setup(context, page)
  const target = {
    clusterId: 'cluster-a',
    namespace: 'monitoring',
    workloadName: 'report-abc',
    workloadKind: 'ReplicationController',
  }
  state.bindings = [
    { id: 'exact', applicationId: 'rc-app', environmentKey: 'test', targets: [target] },
    ...[
      { ...target, workloadKind: 'Deployment' },
      { ...target, namespace: 'other' },
      { ...target, clusterId: 'other' },
      { ...target, enabled: false },
    ].map((t, i) => ({
      id: String(i),
      applicationId: 'wrong-app-' + i,
      environmentKey: 'test',
      targets: [t],
    })),
  ]
  await page.goto(path)
  await expect(page.getByRole('link', { name: 'rc-app', exact: true })).toHaveAttribute(
    'href',
    '/applications/rc-app',
  )
  await expect(page.getByRole('link', { name: /wrong-app/ })).toHaveCount(0)
  expect(state.reads.some((r) => /deployments.*rollout|workflows|releases/.test(r))).toBe(false)
  await verifyBrowserEvidence(evidence)
})

test('ReplicationController readonly and template errors preserve truthful state on narrow dark screen', async ({
  context,
  page,
}) => {
  const { state, evidence } = await setup(context, page, { readonly: true })
  state.failConfig = true
  await page.setViewportSize({ width: 980, height: 1000 })
  await page.goto(path)
  await expect(page.getByText('Template read failed', { exact: false }).first()).toBeVisible()
  await expect(page.getByRole('button', { name: '删除', exact: true })).toHaveCount(0)
  await expect(page.getByRole('link', { name: '查看 Deployment 版本历史' })).toHaveCount(0)
  expect(state.reads.some((p) => p.includes('/deployments/report/rollouts'))).toBe(false)
  await page.getByRole('button', { name: '切换到深色模式', exact: true }).click()
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth + 1)).toBe(
    true,
  )
  state.failDetail = true
  await page.reload()
  await expect(
    page.getByText('ReplicationController read failed', { exact: false }).first(),
  ).toBeVisible()
  await expect(page.getByRole('heading', { name: 'report-abc', exact: true })).toHaveCount(0)
  await verifyBrowserEvidence(evidence)
})

test('ReplicationController delete cancellation is inert and failures retain confirmation context', async ({
  context,
  page,
}) => {
  const { state, evidence } = await setup(context, page)
  await page.goto(path)
  await page.getByRole('button', { name: '删除', exact: true }).click()
  await expect(page.getByRole('dialog')).toContainText('ReplicationController')
  await page.getByRole('dialog').getByRole('button', { name: '取消', exact: true }).click()
  await expect(page.getByRole('dialog')).toHaveCount(0)
  expect(state.writes).toEqual([])
  state.failDelete = true
  await page.getByRole('button', { name: '删除', exact: true }).click()
  await page.getByRole('dialog').getByRole('button', { name: '删除', exact: true }).click()
  await expect(
    page.getByText('ReplicationController delete failed', { exact: false }).first(),
  ).toBeVisible()
  expect(state.writes[0]).toContain('namespace=monitoring')
  await expect(page.getByRole('dialog')).toBeVisible()
  await verifyBrowserEvidence(evidence)
})

test('ReplicationController list retains continuous table, derived health and scoped row actions', async ({
  context,
  page,
}) => {
  const { state, evidence } = await setup(context, page)
  await page.goto('/workloads/replicationcontrollers?clusterId=cluster-a&namespace=monitoring')
  await expect(page.getByText('1 个 ReplicationControllers', { exact: true })).toBeVisible()
  await expect(page.getByText('健康', { exact: true })).toBeVisible()
  await expect(page.locator('.ant-pagination')).toHaveCount(0)
  await page.getByRole('button', { name: '更多操作：report-abc', exact: true }).click()
  await expect(page.getByRole('menuitem', { name: '删除', exact: true })).toBeVisible()
  await page.keyboard.press('Escape')
  await page.getByRole('button', { name: 'report-abc', exact: true }).click()
  await expect(page).toHaveURL(
    /replicationcontrollers\/report-abc\?clusterId=cluster-a&namespace=monitoring/,
  )
  expect(state.writes).toEqual([])
  await verifyBrowserEvidence(evidence)
})
