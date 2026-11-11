import { test, expect, type BrowserContext, type Page } from '@playwright/test'
import { baselineResponse } from '../fixtures/baseline-data'
import { installMocks, verifyBrowserEvidence } from '../fixtures/mock-app'
import { expectWorkloadDetailFrame } from '../fixtures/workload-detail-geometry'
const path = '/workloads/daemonsets/db?clusterId=cluster-a&namespace=monitoring'
const prefix = '/api/v1/clusters/cluster-a/workloads/daemonsets/db'
async function setup(context: BrowserContext, page: Page, { readonly = false, count = 3 } = {}) {
  const state = {
    writes: [] as string[],
    reads: [] as string[],
    wrongRevision: false,
    denyHistory: false,
    strategy: 'RollingUpdate',
    failMetrics: false,
  }
  const pods = Array.from({ length: count }, (_, index) => ({
    name: `db-${String(index).padStart(3, '0')}`,
    namespace: 'monitoring',
    phase: 'Running',
    readyContainers: '1/1',
    restarts: 0,
    nodeName: index === 0 ? 'worker-a' : 'worker-b',
    ageSeconds: 600,
    labels: {
      'apps.kubernetes.io/pod-index': String(index),
      'controller-revision-hash': index === 0 ? 'oldhash' : 'newhash',
    },
    persistentVolumeClaims: [`data-db-${index}`],
    containerStatuses: [{ name: 'db', state: 'running', ready: true, restartCount: 0 }],
    allowedActions: ['view', 'logs', 'exec'],
  }))
  const configuration = `apiVersion: apps/v1
kind: DaemonSet
metadata:
 name: db
 namespace: monitoring
 uid: uid-db
 generation: 4
spec:
 updateStrategy:
  type: RollingUpdate
  rollingUpdate: {maxUnavailable: 1, maxSurge: 0}
 revisionHistoryLimit: 10
 template:
  spec:
   containers: [{name: db, image: db:v2}]
status:
 observedGeneration: 4
 numberMisscheduled: 0
 numberUnavailable: 0
`
  await context.addInitScript(() =>
    localStorage.setItem(
      'soha-scope',
      JSON.stringify({ state: { clusterId: 'cluster-a', namespace: 'monitoring' }, version: 0 }),
    ),
  )
  const evidence = await installMocks(context, page, (url, method) => {
    const endpoint = url.pathname
    if (method === 'GET') state.reads.push(endpoint + url.search)
    else if (endpoint.startsWith('/api/v1/clusters/')) state.writes.push(endpoint)
    if (endpoint === '/api/v1/access/permission-snapshot') {
      const body = baselineResponse(endpoint)!
      if (!body.data || !('permissionKeys' in body.data))
        throw new Error('Missing permission fixture')
      body.data.permissionKeys = body.data.permissionKeys!.filter(
        (key) => !key.startsWith('platform.workloads.daemon-sets.') && !key.startsWith('delivery.'),
      )
      body.data.permissionKeys.push(
        'platform.workloads.daemon-sets.view',
        'platform.pods.view',
        'platform.nodes.view',
        'platform.network.services.view',
        'platform.storage.persistent-volume-claims.view',
        'platform.storage.persistent-volumes.view',
        ...(readonly
          ? []
          : [
              'platform.workloads.daemon-sets.restart',
              'platform.workloads.daemon-sets.delete',
              'delivery.applications.view',
            ]),
      )
      return { body }
    }
    if (endpoint === '/api/v1/clusters/capabilities')
      return {
        body: {
          data: [
            'resource.yaml.view',
            'resource.yaml.apply',
            'workload.mutations',
            'pod.logs',
            'pod.exec',
          ].map((key) => ({
            key,
            direct: { status: 'available' },
            agent: { status: 'available' },
          })),
        },
      }
    if (method === 'POST' && endpoint === '/api/v1/clusters/cluster-a/workloads/daemonsets/restart')
      return { body: { data: { accepted: true } } }
    if (
      endpoint === '/api/v1/clusters/cluster-a/resources/graph' &&
      method === 'GET' &&
      url.searchParams.get('namespace') === 'monitoring' &&
      url.searchParams.get('kind') === 'DaemonSet' &&
      url.searchParams.get('name') === 'db'
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
              ['root', 'DaemonSet', 'db'],
              ['pod', 'Pod', 'db-000'],
              ['node', 'Node', 'worker-a'],
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
              ['root', 'pod', 'controls'],
              ['pod', 'node', 'scheduled-on'],
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
      return {
        body: {
          data: {
            name: 'db',
            namespace: 'monitoring',
            createdAt: '2026-10-01T00:00:00Z',
            labels: { app: 'db' },
            annotations: { source: 'test' },
            desiredNumber: count,
            readyNumber: count,
            currentNumber: count,
            availableNumber: count,
            updatedNumber: count,
            updateStrategy: 'RollingUpdate',
            serviceName: 'db-headless',
            selector: { app: 'db' },
            pods,
            relatedResources: [
              {
                kind: 'Service',
                name: 'db-headless',
                namespace: 'monitoring',
                relation: 'service',
              },
            ],
            allowedActions: readonly ? ['view'] : ['view', 'restart', 'delete'],
          },
        },
      }
    if (endpoint === prefix + '/yaml')
      return {
        body: {
          data: {
            kind: 'DaemonSet',
            name: 'db',
            namespace: 'monitoring',
            content: configuration.replace('type: RollingUpdate', `type: ${state.strategy}`),
          },
        },
      }
    if (endpoint === prefix + '/rollouts')
      return state.denyHistory
        ? { status: 403, body: { error: { code: 'forbidden', message: 'history forbidden' } } }
        : {
            body: {
              items: [
                {
                  name: 'db-new',
                  namespace: 'monitoring',
                  revision: '4',
                  podRevisionHash: 'newhash',
                  matchesCurrentTemplate: true,
                  replicas: Math.max(0, count - 1),
                  readyReplicas: Math.max(0, count - 1),
                  images: ['db=db:v2'],
                },
                {
                  name: 'db-old',
                  namespace: 'monitoring',
                  revision: '3',
                  podRevisionHash: 'oldhash',
                  matchesCurrentTemplate: false,
                  replicas: count ? 1 : 0,
                  readyReplicas: count ? 1 : 0,
                  images: ['db=db:v1'],
                },
              ],
            },
          }
    if (endpoint.startsWith(prefix + '/rollouts/')) {
      const name = endpoint.includes('/db-new/') ? 'db-new' : 'db-old'
      const revision = name === 'db-new' ? 4 : 3
      return {
        body: {
          data: {
            kind: 'ControllerRevision',
            name,
            namespace: 'monitoring',
            content: `apiVersion: apps/v1\nkind: ControllerRevision\nmetadata:\n name: ${state.wrongRevision ? 'foreign' : name}\n namespace: monitoring\nrevision: ${revision}\ndata:\n spec:\n  template:\n   spec:\n    containers: [{name: db, image: db:v${revision === 4 ? 2 : 1}}]\n`,
          },
        },
      }
    }
    if (endpoint === prefix + '/metrics')
      return state.failMetrics
        ? { status: 503, body: { error: { code: 'unavailable', message: 'metrics unavailable' } } }
        : {
            body: {
              data: {
                configured: true,
                available: true,
                source: 'prometheus',
                rangeMinutes: Number(url.searchParams.get('rangeMinutes') || 60),
                series: [{ key: 'cpu', unit: 'cores', latest: 0, points: [] }],
              },
            },
          }
    if (endpoint === '/api/v1/clusters/cluster-a/workloads/pods')
      return { body: { items: pods.map((p) => ({ ...p, cpu: '0', memory: '32Mi' })) } }
    if (endpoint === '/api/v1/clusters/cluster-a/events')
      return {
        body: {
          items: [
            {
              name: 'db-updated',
              namespace: 'monitoring',
              involvedKind: 'DaemonSet',
              involvedName: 'db',
              reason: 'SuccessfulCreate',
              message: 'Created Pod db-0',
              type: 'Normal',
              count: 1,
              ageSeconds: 60,
            },
          ],
        },
      }
    if (endpoint.startsWith('/api/v1/application-environments')) return { body: { items: [] } }
    if (method === 'GET' && endpoint === '/api/v1/delivery/manifest-deployments')
      return { body: { items: [] } }
    return undefined
  })
  return { state, evidence, pods }
}

test('DaemonSet: eight aligned tabs, native coverage, template history and scoped references', async ({
  context,
  page,
}, info) => {
  const { state, evidence } = await setup(context, page)
  await page.goto(path)
  await expect(page.locator('.soha-workload-detail-header h1')).toHaveText('db')
  await expect(page.getByRole('tab')).toHaveCount(8)
  await expect(page.locator('.soha-workload-detail-header')).toContainText('模板版本 4')
  await expect(page.locator('.soha-controller-template')).toContainText('db:v2')
  await expect(page.getByRole('button', { name: '扩缩容', exact: true })).toHaveCount(0)
  await expect(page.getByRole('button', { name: '回滚', exact: true })).toHaveCount(0)
  await expectWorkloadDetailFrame(page)
  await page.getByRole('tab', { name: '版本历史', exact: true }).click()
  await expect(page.locator('.soha-workload-history-row')).toHaveCount(2)
  await expect(page.locator('.soha-workload-history-row').first()).toContainText('当前模板')
  await page.getByRole('button', { name: '查看版本 4 模板与差异', exact: true }).click()
  await expect(page.getByRole('dialog')).toContainText('ControllerRevision')
  await page.getByRole('dialog').getByRole('button', { name: '关闭', exact: true }).click()
  await page.getByRole('tab', { name: '事件', exact: true }).click()
  await expect(page.getByRole('tabpanel', { name: '事件', exact: true })).toContainText(
    'SuccessfulCreate',
  )
  await page.getByRole('tab', { name: '指标', exact: true }).click()
  await expect(page.locator('.soha-pod-detail-metric-card')).toHaveCount(4)
  await page.getByRole('tab', { name: '关联资源', exact: true }).click()
  const graph = page.getByRole('tabpanel', { name: '关联资源', exact: true })
  await expect(graph.locator('.react-flow__node[data-id="node"]')).toContainText('worker-a')
  await graph.locator('.react-flow__node[data-id="node"]').click()
  const resourceInfo = graph.getByRole('complementary', { name: '选中资源信息' })
  await expect(resourceInfo).toContainText('承载')
  await expect(resourceInfo.getByRole('button', { name: '查看详情', exact: true })).toBeEnabled()
  await resourceInfo.getByRole('button', { name: '关闭资源信息', exact: true }).click()
  await expect(graph.locator('.react-flow__node[data-id="node"]')).toBeFocused()
  await graph
    .getByRole('radiogroup', { name: '关联资源视图' })
    .getByText('列表', { exact: true })
    .click()
  await expect(graph.getByRole('button', { name: 'db（当前）', exact: true })).toBeVisible()
  await page.getByRole('tab', { name: '交付联动', exact: true }).click()
  await expect(page.getByRole('tabpanel', { name: '交付联动', exact: true })).toBeVisible()
  await page.getByRole('tab', { name: 'YAML', exact: true }).click()
  await expect(page.getByRole('tabpanel', { name: 'YAML', exact: true })).toContainText(
    /kind:\s*DaemonSet/,
  )
  expect(state.writes).toEqual([])
  await page.screenshot({ path: info.outputPath('daemonset-eight-tabs.png'), fullPage: true })
  verifyBrowserEvidence(evidence)
})
test('DaemonSet: complete virtual Pods, node and native revision filters survive refresh', async ({
  context,
  page,
}) => {
  const { state, evidence } = await setup(context, page, { count: 80 })
  await page.goto(path + '&tab=pods')
  const panel = page.getByRole('tabpanel', { name: 'Pods', exact: true })
  await expect(panel).toContainText('80 / 80')
  await expect(page.locator('.soha-workload-pods-table .ant-table-pagination')).toHaveCount(0)
  expect(await page.locator('.soha-workload-pods-table .ant-table-row').count()).toBeLessThan(80)
  const search = page.getByRole('textbox', { name: '搜索 Pod', exact: true })
  await search.fill('worker-a')
  await expect(panel).toContainText('1 / 80')
  await search.fill('db-079')
  await expect(page.getByRole('link', { name: 'db-079', exact: true })).toBeVisible()
  const before = state.reads.length
  await page.getByRole('button', { name: '刷新 DaemonSet', exact: true }).click()
  await expect.poll(() => state.reads.length).toBeGreaterThan(before)
  await expect(search).toHaveValue('db-079')
  await page.getByRole('button', { name: '重启', exact: true }).click()
  await expect(page.getByRole('dialog')).toContainText('OnDelete')
  await page.getByRole('button', { name: '取消', exact: true }).click()
  await page.getByRole('button', { name: '删除', exact: true }).click()
  await expect(page.getByRole('dialog')).toContainText('DaemonSet')
  await page.getByRole('button', { name: '取消', exact: true }).click()
  expect(state.writes).toEqual([])
  verifyBrowserEvidence(evidence)
})
test('DaemonSet: read-only and denied history remain explicit without stale template facts', async ({
  context,
  page,
}) => {
  const { state, evidence } = await setup(context, page, { readonly: true })
  state.denyHistory = true
  await page.goto(path + '&tab=revisions')
  await expect(page.getByRole('button', { name: '重启', exact: true })).toHaveCount(0)
  await expect(page.getByRole('button', { name: '删除', exact: true })).toHaveCount(0)
  await expect(page.getByRole('tabpanel', { name: '版本历史', exact: true })).toContainText(
    'history forbidden',
  )
  await expect(page.getByRole('switch', { name: /自动刷新/ })).toHaveAttribute(
    'data-refresh-state',
    'failed',
  )
  await page.getByRole('tab', { name: '交付联动', exact: true }).click()
  await expect(page.getByRole('tabpanel', { name: '交付联动', exact: true })).toContainText(
    '无权查看交付联动',
  )
  expect(state.writes).toEqual([])
  verifyBrowserEvidence(evidence)
})
test('DaemonSet: no target nodes and foreign history templates never look healthy', async ({
  context,
  page,
}) => {
  const { state, evidence } = await setup(context, page, { count: 0 })
  await page.goto(path + '&tab=overview')
  await expect(page.locator('.soha-workload-detail-header')).toContainText('无匹配目标')
  state.wrongRevision = true
  await page.getByRole('tab', { name: '版本历史', exact: true }).click()
  await page.getByRole('button', { name: '查看版本 4 模板与差异', exact: true }).click()
  await expect(page.getByRole('dialog')).toContainText('ControllerRevision identity does not match')
  verifyBrowserEvidence(evidence)
})
test('DaemonSet: related-resource cards remain bounded on narrow and dark layouts', async ({
  context,
  page,
}) => {
  const { evidence } = await setup(context, page)
  await page.goto(path + '&tab=related-resources')
  await expect(
    page.getByRole('tabpanel', { name: '关联资源', exact: true }).locator('.react-flow'),
  ).toBeVisible()
  for (const dark of [false, true]) {
    await page.setViewportSize({ width: 1440, height: 1000 })
    if (dark) await page.getByLabel('切换到深色模式').click()
    for (const width of [1440, 900, 320]) {
      await page.setViewportSize({ width, height: 1000 })
      await expect
        .poll(() =>
          page.locator('.soha-workload-relationships-view').evaluate((board) => {
            const bounds = board.getBoundingClientRect()
            return [...board.querySelectorAll('.react-flow__node')].every((node) => {
              const box = node.getBoundingClientRect()
              return box.left >= bounds.left - 1 && box.right <= bounds.right + 1
            })
          }),
        )
        .toBe(true)
      expect(await page.evaluate(() => document.documentElement.scrollWidth > innerWidth)).toBe(
        false,
      )
    }
  }
  verifyBrowserEvidence(evidence)
})

test('DaemonSet: synthetic restart submits only after scoped confirmation', async ({
  context,
  page,
}) => {
  const { state, evidence } = await setup(context, page)
  await page.goto(path)
  await page.getByRole('button', { name: '重启', exact: true }).click()
  expect(state.writes).toEqual([])
  const request = page.waitForRequest(
    (r) => r.method() === 'POST' && r.url().includes('/daemonsets/restart'),
  )
  await page.getByRole('dialog').getByRole('button', { name: '重启', exact: true }).click()
  expect((await request).postDataJSON()).toEqual({ name: 'db', namespace: 'monitoring' })
  await expect(page.getByRole('dialog')).toHaveCount(0)
  expect(state.writes).toEqual(['/api/v1/clusters/cluster-a/workloads/daemonsets/restart'])
  verifyBrowserEvidence(evidence)
})
