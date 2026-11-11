import { test, expect, type BrowserContext, type Page } from '@playwright/test'
import { baselineResponse } from '../fixtures/baseline-data'
import { installMocks, verifyBrowserEvidence } from '../fixtures/mock-app'
import { expectWorkloadDetailFrame } from '../fixtures/workload-detail-geometry'

const prefix = '/api/v1/clusters/cluster-a/workloads/replicasets/report-abc'
const path = '/workloads/replicasets/report-abc?clusterId=cluster-a&namespace=monitoring'
async function setup(
  context: BrowserContext,
  page: Page,
  { standalone = false, readonly = false, retained = false } = {},
) {
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
    readyReplicas: 40,
    availableReplicas: 40,
    createdAt: '2026-10-31T00:00:00Z',
    annotations: { 'deployment.kubernetes.io/revision': '12' },
    labels: { app: 'report' },
    selector: { app: 'report' },
    pods,
    relatedResources: [
      ...(standalone
        ? []
        : [{ kind: 'Deployment', name: 'report', namespace: 'monitoring', relation: 'owner' }]),
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
      if (!body.data || !('permissionKeys' in body.data))
        throw new Error('Permission fixture missing')
      body.data.permissionKeys = body.data.permissionKeys!.filter(
        (k) =>
          !k.startsWith('platform.workloads.replica-sets.') &&
          !k.startsWith('platform.deployment.') &&
          !k.startsWith('delivery.'),
      )
      body.data.permissionKeys.push(
        'platform.workloads.replica-sets.view',
        'platform.pods.view',
        'platform.configuration.config-maps.view',
        ...(readonly
          ? []
          : [
              'platform.workloads.replica-sets.delete',
              'platform.workloads.replica-sets.update',
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
    if (
      endpoint === '/api/v1/clusters/cluster-a/resources/graph' &&
      method === 'GET' &&
      url.searchParams.get('namespace') === 'monitoring' &&
      url.searchParams.get('kind') === 'ReplicaSet' &&
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
              ['root', 'ReplicaSet', 'report-abc'],
              ['config', 'ConfigMap', 'report-config'],
              ...(standalone ? [] : [['owner', 'Deployment', 'report']]),
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
            edges: [
              ['root', 'config', 'uses-config'],
              ...(standalone ? [] : [['owner', 'root', 'controls']]),
            ].map(([sourceId, targetId, relation], index) => ({
              id: String(index),
              sourceId,
              targetId,
              relation,
            })),
          },
        },
      }
    if (endpoint === prefix + '/detail')
      return state.failDetail
        ? {
            status: 503,
            body: { error: { code: 'unavailable', message: 'ReplicaSet read failed' } },
          }
        : { body: { data: state.detail } }
    if (endpoint === prefix + '/yaml')
      return state.failConfig
        ? { status: 503, body: { error: { code: 'unavailable', message: 'Template read failed' } } }
        : {
            body: {
              data: {
                kind: 'ReplicaSet',
                name: 'report-abc',
                namespace: 'monitoring',
                content:
                  'apiVersion: apps/v1\nkind: ReplicaSet\nmetadata: {name: report-abc, namespace: monitoring, generation: 3}\nspec: {minReadySeconds: 0, template: {spec: {initContainers: [{name: setup, image: busybox:1.38}], containers: [{name: app, image: report:v12}]}}}\nstatus: {replicas: 40, observedGeneration: 3, fullyLabeledReplicas: 40, conditions: []}',
              },
            },
          }
    if (endpoint === '/api/v1/clusters/cluster-a/workloads/deployments/report/rollout-status')
      return {
        body: {
          data: {
            name: 'report',
            namespace: 'monitoring',
            revision: retained ? '13' : '12',
            status: 'healthy',
            message: '',
            desiredReplicas: 40,
            updatedReplicas: 40,
            readyReplicas: 40,
            availableReplicas: 40,
            observedGeneration: 3,
          },
        },
      }
    if (endpoint === '/api/v1/delivery/manifest-deployments') return { body: { items: [] } }
    if (endpoint === '/api/v1/clusters/cluster-a/workloads/deployments/report/rollouts')
      return {
        body: {
          data: [
            {
              name: 'report-abc',
              namespace: 'monitoring',
              revision: '12',
              images: ['report:v12'],
              replicas: 40,
              readyReplicas: 40,
            },
          ],
        },
      }
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
              involvedKind: 'ReplicaSet',
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
        ? { status: 500, body: { error: { code: 'failed', message: 'ReplicaSet delete failed' } } }
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

test('ReplicaSet six tabs, scoped parent links, complete Pods, events and resource frame', async ({
  context,
  page,
}) => {
  const { state, evidence } = await setup(context, page)
  await page.goto(path)
  await expectWorkloadDetailFrame(page)
  await expect(page.getByRole('tab')).toHaveCount(6)
  await expect(page.getByText('Deployment 当前修订', { exact: true }).first()).toBeVisible()
  await expect(page.getByRole('link', { name: '查看 Deployment 版本历史' })).toHaveAttribute(
    'href',
    /deployments\/report\?clusterId=cluster-a&namespace=monitoring&tab=revisions/,
  )
  await expect(page.getByRole('link', { name: '查看 Deployment 交付联动' })).toHaveAttribute(
    'href',
    /tab=delivery/,
  )
  await expect(page.getByText('report:v12', { exact: false }).first()).toBeVisible()
  await page.getByRole('tab', { name: 'Pods', exact: true }).click()
  await expect(page.locator('.ant-table-tbody-virtual-holder')).toBeVisible()
  await expect(page.locator('.ant-pagination')).toHaveCount(0)
  await page.getByRole('textbox', { name: '搜索 Pod' }).fill('report-039')
  await expect(page.getByRole('link', { name: 'report-039', exact: true })).toBeVisible()
  await expect(page.getByText('50m', { exact: true })).toBeVisible()
  await page.getByRole('button', { name: '刷新 ReplicaSet', exact: true }).click()
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
    '/api/v1/clusters/cluster-a/resources/graph?namespace=monitoring&kind=ReplicaSet&name=report-abc',
  ])
  await page
    .getByRole('radiogroup', { name: '关联资源视图' })
    .getByText('列表', { exact: true })
    .click()
  await expect(page.getByRole('radio', { name: '列表', exact: true })).toBeChecked()
  await page.getByRole('tab', { name: 'YAML', exact: true }).click()
  await expect(page.locator('.soha-k8s-yaml-editor .view-lines:visible').first()).toContainText(
    'kind: ReplicaSet',
  )
  const editor = page.getByRole('textbox', { name: 'YAML 草稿编辑器', exact: true })
  await editor.press('ControlOrMeta+Home')
  await editor.pressSequentially('# retained-rs-draft')
  await editor.press('Enter')
  const comparison = page.getByRole('radiogroup', { name: 'YAML 对比方式' })
  await comparison.getByText('双栏对比', { exact: true }).click()
  await comparison.getByText('变更审阅', { exact: true }).click()
  await comparison.getByText('编辑', { exact: true }).click()
  await expect(page.locator('.soha-k8s-yaml-editor .view-lines:visible').first()).toContainText(
    'retained-rs-draft',
  )
  expect(state.writes).toEqual([])
  await page.getByRole('tab', { name: '概览', exact: true }).click()
  await expect(page.getByRole('tab', { name: '概览', exact: true })).toHaveAttribute(
    'aria-selected',
    'true',
  )
  await verifyBrowserEvidence(evidence)
})

test('ReplicaSet metric zero, missing monitoring and range persistence', async ({
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

test('ReplicaSet retained template and independent empty ownership stay explicit', async ({
  context,
  page,
}) => {
  const { state, evidence } = await setup(context, page, { retained: true })
  await page.goto(path)
  await expect(page.getByText('Deployment 历史修订', { exact: true }).first()).toBeVisible()
  state.detail.relatedResources = []
  state.detail.pods = []
  state.detail.desiredReplicas = 0
  state.detail.readyReplicas = 0
  state.detail.availableReplicas = 0
  await page.reload()
  await expect(page.getByText('未关联 Deployment', { exact: true }).first()).toBeVisible()
  await expect(page.getByRole('link', { name: '查看 Deployment 版本历史' })).toHaveCount(0)
  await expect(page.locator('.soha-workload-detail-header .soha-status-tag')).toHaveText('期望为 0')
  await page.getByRole('tab', { name: 'Pods', exact: true }).click()
  await expect(page.getByText('当前返回数据中没有 Pod', { exact: true })).toBeVisible()
  await verifyBrowserEvidence(evidence)
})

test('ReplicaSet readonly and template errors preserve truthful state on narrow dark screen', async ({
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
  await expect(page.getByText('ReplicaSet read failed', { exact: false }).first()).toBeVisible()
  await expect(page.getByRole('heading', { name: 'report-abc', exact: true })).toHaveCount(0)
  await verifyBrowserEvidence(evidence)
})

test('ReplicaSet delete cancellation is inert and failures retain confirmation context', async ({
  context,
  page,
}) => {
  const { state, evidence } = await setup(context, page)
  await page.goto(path)
  await page.getByRole('button', { name: '删除', exact: true }).click()
  await expect(page.getByRole('dialog')).toContainText('Deployment')
  await page.getByRole('dialog').getByRole('button', { name: '取消', exact: true }).click()
  await expect(page.getByRole('dialog')).toHaveCount(0)
  expect(state.writes).toEqual([])
  state.failDelete = true
  await page.getByRole('button', { name: '删除', exact: true }).click()
  await page.getByRole('dialog').getByRole('button', { name: '删除', exact: true }).click()
  await expect(page.getByText('ReplicaSet delete failed', { exact: false }).first()).toBeVisible()
  expect(state.writes[0]).toContain('namespace=monitoring')
  await expect(page.getByRole('dialog')).toBeVisible()
  await verifyBrowserEvidence(evidence)
})
