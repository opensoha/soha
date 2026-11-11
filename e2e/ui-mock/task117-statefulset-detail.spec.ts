import { test, expect, type BrowserContext, type Page } from '@playwright/test'
import { baselineResponse } from '../fixtures/baseline-data'
import { installMocks, verifyBrowserEvidence } from '../fixtures/mock-app'
import { expectWorkloadDetailFrame } from '../fixtures/workload-detail-geometry'
const path = '/workloads/statefulsets/db?clusterId=cluster-a&namespace=monitoring'
const prefix = '/api/v1/clusters/cluster-a/workloads/statefulsets/db'
async function setup(context: BrowserContext, page: Page, { readonly = false, count = 3 } = {}) {
  const state = {
    writes: [] as string[],
    reads: [] as string[],
    wrongRevision: false,
    denyHistory: false,
    denyPvc: false,
    omitClaims: false,
    failMetrics: false,
  }
  const pods = Array.from({ length: count }, (_, index) => ({
    name: `db-${String(index).padStart(3, '0')}`,
    namespace: 'monitoring',
    phase: 'Running',
    readyContainers: '1/1',
    restarts: 0,
    nodeName: 'worker-a',
    ageSeconds: 600,
    labels: {
      'apps.kubernetes.io/pod-index': String(index),
      'controller-revision-hash': index === 0 ? 'db-old' : 'db-new',
    },
    persistentVolumeClaims: [`data-db-${index}`],
    containerStatuses: [{ name: 'db', state: 'running', ready: true, restartCount: 0 }],
    allowedActions: ['view', 'logs', 'exec'],
  }))
  const configuration = `apiVersion: apps/v1
kind: StatefulSet
metadata:
 name: db
 namespace: monitoring
 uid: uid-db
 generation: 4
spec:
 serviceName: db-headless
 podManagementPolicy: OrderedReady
 updateStrategy:
  type: RollingUpdate
  rollingUpdate:
   partition: 1
 persistentVolumeClaimRetentionPolicy:
  whenDeleted: Retain
  whenScaled: Delete
 revisionHistoryLimit: 10
 template:
  spec:
   containers: [{name: db, image: db:v2}]
 volumeClaimTemplates:
 - metadata: {name: data}
   spec:
    resources: {requests: {storage: 20Gi}}
    storageClassName: fast
    accessModes: [ReadWriteOnce]
status:
 observedGeneration: 4
 updatedReplicas: ${Math.max(0, count - 1)}
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
        (key) =>
          !key.startsWith('platform.workloads.stateful-sets.') && !key.startsWith('delivery.'),
      )
      body.data.permissionKeys.push(
        'platform.workloads.stateful-sets.view',
        'platform.pods.view',
        'platform.nodes.view',
        'platform.network.services.view',
        'platform.storage.persistent-volume-claims.view',
        'platform.storage.persistent-volumes.view',
        ...(readonly
          ? []
          : [
              'platform.workloads.stateful-sets.scale',
              'platform.workloads.stateful-sets.restart',
              'platform.workloads.stateful-sets.delete',
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
    if (
      endpoint === '/api/v1/clusters/cluster-a/resources/graph' &&
      method === 'GET' &&
      url.searchParams.get('namespace') === 'monitoring' &&
      url.searchParams.get('kind') === 'StatefulSet' &&
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
              ['root', 'StatefulSet', 'db'],
              ['service', 'Service', 'db-headless'],
              ['pod', 'Pod', 'db-000'],
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
              ['service', 'pod', 'selects'],
              ['root', 'pod', 'controls'],
            ].map(([sourceId, targetId, relation], index) => ({
              id: String(index),
              sourceId,
              targetId,
              relation,
            })),
          },
        },
      }
    if (
      method === 'GET' &&
      url.searchParams.get('namespace') === 'monitoring' &&
      endpoint === '/api/v1/clusters/cluster-a/network/services/db-headless/detail'
    )
      return {
        body: {
          data: {
            name: 'db-headless',
            namespace: 'monitoring',
            type: 'ClusterIP',
            clusterIp: 'None',
            ports: [],
            portMappings: [],
            endpoints: [],
            pods: [],
            selector: {},
          },
        },
      }
    if (
      method === 'GET' &&
      url.searchParams.get('namespace') === 'monitoring' &&
      endpoint === '/api/v1/clusters/cluster-a/network/services/db-headless/yaml'
    )
      return {
        body: {
          data: {
            kind: 'Service',
            name: 'db-headless',
            namespace: 'monitoring',
            content:
              'apiVersion: v1\nkind: Service\nmetadata: {name: db-headless, namespace: monitoring}\nspec: {clusterIP: None, ports: []}\n',
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
            desiredReplicas: count,
            readyReplicas: count,
            currentReplicas: count,
            currentRevision: 'db-old',
            updateRevision: 'db-new',
            updateStrategy: 'RollingUpdate',
            serviceName: 'db-headless',
            selector: { app: 'db' },
            pods: state.omitClaims
              ? pods.map((pod) => ({ ...pod, persistentVolumeClaims: [] }))
              : pods,
            relatedResources: [
              {
                kind: 'Service',
                name: 'db-headless',
                namespace: 'monitoring',
                relation: 'service',
              },
            ],
            allowedActions: readonly ? ['view'] : ['view', 'scale', 'restart', 'delete'],
          },
        },
      }
    if (endpoint === prefix + '/yaml')
      return {
        body: {
          data: {
            kind: 'StatefulSet',
            name: 'db',
            namespace: 'monitoring',
            content: configuration,
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
                  replicas: Math.max(0, count - 1),
                  readyReplicas: Math.max(0, count - 1),
                  images: ['db=db:v2'],
                },
                {
                  name: 'db-old',
                  namespace: 'monitoring',
                  revision: '3',
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
    if (endpoint === '/api/v1/clusters/cluster-a/storage/persistentvolumeclaims' && state.denyPvc)
      return { status: 403, body: { error: { code: 'forbidden', message: 'PVC access denied' } } }
    if (endpoint === '/api/v1/clusters/cluster-a/storage/persistentvolumeclaims')
      return {
        body: {
          items: pods.map((_, index) => ({
            name: `data-db-${index}`,
            namespace: 'monitoring',
            status: 'Bound',
            requested: '20Gi',
            storageClass: 'fast',
            volumeName: `pv-${index}`,
            ageSeconds: 600,
          })),
        },
      }
    if (endpoint.includes('/storage/persistentvolumeclaims/'))
      return {
        body: {
          data: {
            name: 'data-db-0',
            namespace: 'monitoring',
            status: 'Bound',
            capacity: '20Gi',
            requested: '20Gi',
            storageClass: 'fast',
            volumeName: 'pv-0',
            accessModes: ['ReadWriteOnce'],
            volumeMode: 'Filesystem',
            ageSeconds: 600,
          },
        },
      }
    if (endpoint === '/api/v1/clusters/cluster-a/events')
      return {
        body: {
          items: [
            {
              name: 'db-updated',
              namespace: 'monitoring',
              involvedKind: 'StatefulSet',
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
test('StatefulSet: nine tabs, scoped live facts, storage and retained revision templates', async ({
  context,
  page,
}, info) => {
  const { state, evidence } = await setup(context, page)
  await page.goto(path)
  await expect(page.locator('.soha-workload-detail-header h1')).toHaveText('db')
  await expect(page.locator('.soha-workload-detail-header .soha-metadata-tag')).toHaveText(
    'monitoring',
  )
  await expect(page.locator('.soha-workload-detail-header')).not.toContainText('读取于')
  await expect(page.getByRole('tab')).toHaveCount(9)
  await expect(page.locator('.soha-sts-template')).toContainText('db:v2')
  await expectWorkloadDetailFrame(page)
  expect(state.reads.some((v) => v.includes('/rollouts'))).toBe(false)
  await page.getByRole('tab', { name: '版本历史', exact: true }).click()
  await expect(page.locator('.soha-workload-history-row')).toHaveCount(2)
  await expect(page.locator('.soha-workload-history-row').first()).toContainText('目标版本')
  await page.getByRole('button', { name: '查看版本 4 模板与差异', exact: true }).click()
  await expect(page.getByRole('dialog')).toBeVisible()
  await expect(page.getByRole('dialog')).toContainText('ControllerRevision')
  await expect(
    page.getByRole('dialog').getByRole('button', { name: '关闭', exact: true }),
  ).toBeVisible()
  await page.getByRole('dialog').getByRole('button', { name: '关闭', exact: true }).click()
  await page.getByRole('tab', { name: '存储与 PVC', exact: true }).click()
  await expect(page.locator('.soha-workload-detail-tabs')).toContainText('20Gi')
  await page.getByRole('button', { name: '详情', exact: true }).first().click()
  await expect(
    page.locator('.soha-workload-overview-config').filter({ hasText: '容量' }),
  ).toContainText('20Gi')
  await page.getByRole('tab', { name: '事件', exact: true }).click()
  await expect(page.getByRole('tabpanel', { name: '事件', exact: true })).toContainText(
    'SuccessfulCreate',
  )
  await page.getByRole('tab', { name: '指标', exact: true }).click()
  await expect(page.locator('.soha-pod-detail-metric-card')).toHaveCount(4)
  await page.getByRole('tab', { name: '关联资源', exact: true }).click()
  const relationships = page.getByRole('tabpanel', { name: '关联资源', exact: true })
  await expect(relationships.locator('.react-flow__node[data-id="service"]')).toContainText(
    'db-headless',
  )
  await relationships.locator('.react-flow__node[data-id="service"]').click()
  const resourceInfo = relationships.getByRole('complementary', { name: '选中资源信息' })
  await expect(resourceInfo).toContainText('monitoring')
  await expect(resourceInfo.getByRole('button', { name: '查看详情', exact: true })).toBeEnabled()
  await resourceInfo.getByRole('button', { name: '查看详情', exact: true }).click()
  await expect(page).toHaveURL(/\/network\/services\/db-headless\?/)
  expect(new URL(page.url()).searchParams.get('clusterId')).toBe('cluster-a')
  expect(new URL(page.url()).searchParams.get('namespace')).toBe('monitoring')
  await page.goBack()
  await page
    .getByRole('radiogroup', { name: '关联资源视图' })
    .getByText('列表', { exact: true })
    .click()
  await expect(
    relationships.getByRole('button', { name: 'db-headless', exact: true }),
  ).toBeVisible()
  await expect(relationships.getByRole('button', { name: 'db（当前）', exact: true })).toBeVisible()
  await page.getByRole('tab', { name: '交付联动', exact: true }).click()
  await expect(page.getByRole('tabpanel', { name: '交付联动', exact: true })).toBeVisible()
  await page.getByRole('tab', { name: 'YAML', exact: true }).click()
  await expect(page.getByRole('tabpanel', { name: 'YAML', exact: true })).toContainText(
    /kind:\s*StatefulSet/,
  )
  expect(state.writes).toEqual([])
  await page.screenshot({ path: info.outputPath('statefulset-nine-tabs.png'), fullPage: true })
  verifyBrowserEvidence(evidence)
})
test('StatefulSet: complete virtual Pods and operation confirmation remain scoped', async ({
  context,
  page,
}) => {
  const { state, evidence } = await setup(context, page, { count: 80 })
  await page.goto(path + '&tab=pods')
  await expect(page.getByRole('tabpanel', { name: 'Pods', exact: true })).toContainText('80 / 80')
  await expect(page.locator('.soha-workload-pods-table .ant-table-pagination')).toHaveCount(0)
  expect(await page.locator('.soha-workload-pods-table .ant-table-row').count()).toBeLessThan(80)
  await page.getByRole('textbox', { name: '搜索 Pod', exact: true }).fill('db-079')
  await expect(page.getByRole('link', { name: 'db-079', exact: true })).toBeVisible()
  await expect(page.getByRole('button', { name: '扩缩容', exact: true })).toBeVisible()
  await page.getByRole('button', { name: '扩缩容', exact: true }).click()
  await expect(page.getByRole('dialog')).toContainText('monitoring')
  await page.getByRole('button', { name: '取消', exact: true }).click()
  await page.getByRole('button', { name: '删除', exact: true }).click()
  await expect(page.getByRole('dialog')).toContainText('PVC')
  await page.getByRole('button', { name: '取消', exact: true }).click()
  expect(state.writes).toEqual([])
  verifyBrowserEvidence(evidence)
})
test('StatefulSet: read-only, denied history and wrong revision identity keep truthfulness', async ({
  context,
  page,
}) => {
  const { state, evidence } = await setup(context, page, { readonly: true })
  await page.goto(path)
  await expect(page.locator('.soha-sts-template')).toContainText('db:v2')
  await expect(page.getByRole('button', { name: '扩缩容', exact: true })).toHaveCount(0)
  await expect(page.getByRole('button', { name: '删除', exact: true })).toHaveCount(0)
  await page.getByRole('tab', { name: '交付联动', exact: true }).click()
  await expect(page.getByRole('tabpanel', { name: '交付联动', exact: true })).toContainText(
    '无权查看交付联动',
  )
  state.denyHistory = true
  await page.getByRole('tab', { name: '版本历史', exact: true }).click()
  await expect(page.getByRole('tabpanel', { name: '版本历史', exact: true })).toContainText(
    'history forbidden',
  )
  expect(state.writes).toEqual([])
  verifyBrowserEvidence(evidence)
})

test('StatefulSet: storage refresh preserves search and surfaces denied reads in header', async ({
  context,
  page,
}) => {
  const { state, evidence } = await setup(context, page)
  await page.goto(path + '&tab=storage')
  await expect(page.getByRole('link', { name: 'data-db-1', exact: true })).toBeVisible()
  const pvcHeader = page.getByRole('columnheader', { name: 'PVC', exact: true })
  await pvcHeader.click()
  await expect(pvcHeader).toHaveAttribute('aria-sort', 'ascending')
  await pvcHeader.click()
  await expect(pvcHeader).toHaveAttribute('aria-sort', 'descending')
  await expect(page.locator('.ant-table-tbody').last().getByRole('link').first()).toHaveText(
    'data-db-2',
  )
  await page.getByRole('textbox', { name: '搜索 PVC', exact: true }).fill('data-db-1')
  const before = state.reads.filter((v) => v.includes('/storage/persistentvolumeclaims')).length
  await page.getByRole('button', { name: '刷新 StatefulSet', exact: true }).click()
  await expect
    .poll(() => state.reads.filter((v) => v.includes('/storage/persistentvolumeclaims')).length)
    .toBeGreaterThan(before)
  await expect(page.getByRole('textbox', { name: '搜索 PVC', exact: true })).toHaveValue(
    'data-db-1',
  )
  state.denyPvc = true
  await page.getByRole('button', { name: '刷新存储', exact: true }).click()
  await expect(page.getByRole('tabpanel', { name: '存储与 PVC', exact: true })).toContainText(
    'PVC无访问权限',
  )
  await expect(page.getByRole('switch', { name: /自动刷新/ })).toHaveAttribute(
    'data-refresh-state',
    'failed',
  )
  verifyBrowserEvidence(evidence)
})
test('StatefulSet: a claim removed from current references closes its cached detail', async ({
  context,
  page,
}) => {
  const { state, evidence } = await setup(context, page)
  await page.goto(path + '&tab=storage')
  await page.getByRole('button', { name: '详情', exact: true }).first().click()
  await expect(
    page.locator('.soha-workload-overview-config').filter({ hasText: '容量' }),
  ).toContainText('20Gi')
  for (const width of [1440, 900, 320]) {
    await page.setViewportSize({ width, height: 1000 })
    await expect
      .poll(() =>
        page.locator('.soha-sts-stack').evaluate((stack) => {
          const parent = stack.getBoundingClientRect()
          return [...stack.children].every((card) => {
            const bounds = card.getBoundingClientRect()
            return bounds.left >= parent.left - 1 && bounds.right <= parent.right + 1
          })
        }),
      )
      .toBe(true)
  }
  state.omitClaims = true
  await page.getByRole('button', { name: '刷新 StatefulSet', exact: true }).click()
  await expect(
    page.locator('.soha-workload-overview-config').filter({ hasText: '容量' }),
  ).toHaveCount(0)
  await expect(page.getByRole('tabpanel', { name: '存储与 PVC', exact: true })).toContainText(
    '当前没有已确认的 PVC 引用',
  )
  verifyBrowserEvidence(evidence)
})
test('StatefulSet: foreign revision template and zero replicas do not masquerade as valid data', async ({
  context,
  page,
}) => {
  const { state, evidence } = await setup(context, page, { count: 0 })
  await page.goto(path + '&tab=pods')
  await expect(page.locator('.soha-workload-detail-header')).toContainText('已缩容至 0')
  await expect(page.getByRole('tabpanel', { name: 'Pods', exact: true })).toContainText(
    '当前返回数据中没有 Pod',
  )
  state.wrongRevision = true
  await page.getByRole('tab', { name: '版本历史', exact: true }).click()
  await page.getByRole('button', { name: '查看版本 4 模板与差异', exact: true }).click()
  await expect(page.getByRole('dialog')).toContainText('ControllerRevision identity does not match')
  await expect(page.getByRole('dialog').locator('.soha-yaml-diff')).toHaveCount(0)
  expect(state.writes).toEqual([])
  verifyBrowserEvidence(evidence)
})
