import { test, expect, type BrowserContext, type Page } from '@playwright/test'
import { baselineResponse } from '../fixtures/baseline-data'
import { installMocks, verifyBrowserEvidence } from '../fixtures/mock-app'

const path = '/workloads/deployments/api?clusterId=cluster-a&namespace=monitoring'
async function setup(
  context: BrowserContext,
  page: Page,
  options: { readonly?: boolean; count?: number; zero?: boolean; bound?: boolean } = {},
) {
  const count = options.zero ? 0 : options.count || 3
  const pods = Array.from({ length: count }, (_, index) => ({
    name: `api-${index === 2 && count === 3 ? 'old' : 'new'}-${String(index).padStart(3, '0')}`,
    namespace: 'monitoring',
    phase: 'Running',
    readyContainers: '1/1',
    restarts: 0,
    nodeName: 'worker-a',
    podIp: '10.0.0.2',
    ageSeconds: 300,
    cpu: '140m',
    memory: '400Mi',
    requests: { cpu: '100m', memory: '256Mi' },
    limits: { cpu: '1', memory: '1Gi' },
    allowedActions: options.readonly ? ['view'] : ['view', 'logs', 'exec'],
    containerStatuses: [{ name: 'app', state: 'running', ready: true, restartCount: 0 }],
  }))
  const state = {
    reads: [] as string[],
    ranges: [] as string[],
    writes: [] as { path: string; body: unknown }[],
    failScale: false,
    failMetrics: false,
    failDelivery: false,
    pods,
  }
  await context.addInitScript(() =>
    localStorage.setItem(
      'soha-scope',
      JSON.stringify({ state: { clusterId: 'cluster-a', namespace: 'monitoring' }, version: 0 }),
    ),
  )
  const evidence = await installMocks(context, page, (url, method, request) => {
    const endpoint = url.pathname.replace('/api/v1', '')
    state.reads.push(endpoint)
    if (endpoint === '/access/permission-snapshot') {
      const body = baselineResponse(url.pathname)!
      if (!('permissionKeys' in body.data)) throw new Error('Missing permission fixture')
      body.data.permissionKeys = body.data.permissionKeys!.filter(
        (key) => !key.startsWith('platform.deployment.') && !key.startsWith('delivery.'),
      )
      body.data.permissionKeys.push(
        'platform.deployment.view',
        'platform.pods.view',
        'platform.nodes.view',
        'platform.configuration.config-maps.view',
        ...(options.readonly
          ? []
          : [
              'platform.deployment.scale',
              'platform.deployment.restart',
              'platform.deployment.rollback',
              'platform.deployment.delete',
              'platform.pods.logs',
              'platform.pods.exec',
              'delivery.applications.view',
              'delivery.workflows.view',
              'delivery.releases.view',
            ]),
      )
      return { body }
    }
    if (endpoint === '/clusters')
      return {
        body: {
          data: [
            {
              id: 'cluster-a',
              name: 'Deployment 详情样板',
              connectionMode: 'direct_kubeconfig',
              health: { status: 'healthy' },
            },
          ],
        },
      }
    if (endpoint === '/clusters/capabilities')
      return {
        body: {
          data: ['workload.mutations', 'resource.yaml.apply', 'pod.logs', 'pod.exec'].map(
            (key) => ({ key, direct: { status: 'available' }, agent: { status: 'unsupported' } }),
          ),
        },
      }
    if (endpoint === '/clusters/cluster-a/workloads/deployments/api/detail')
      return {
        body: {
          data: {
            name: 'api',
            namespace: 'monitoring',
            desiredReplicas: count,
            updatedReplicas: count === 3 ? 2 : count,
            readyReplicas: count,
            availableReplicas: count,
            observedGeneration: 17,
            strategy: 'RollingUpdate',
            createdAt: '2026-10-21T10:00:00Z',
            selector: { app: 'api' },
            labels: { app: 'api' },
            annotations: { checksum: 'abc' },
            containers: [{ name: 'app', image: 'registry/api:v12' }],
            conditions: [{ type: 'Available', status: 'True', reason: 'MinimumReplicasAvailable' }],
            pods,
            allowedActions: options.readonly
              ? ['view']
              : ['view', 'scale', 'restart', 'rollback', 'delete'],
          },
        },
      }
    if (endpoint.endsWith('/deployments/api/rollout-status'))
      return {
        body: {
          data: {
            name: 'api',
            namespace: 'monitoring',
            revision: '12',
            status: 'ready',
            desiredReplicas: count,
            updatedReplicas: count === 3 ? 2 : count,
            readyReplicas: count,
            availableReplicas: count,
            conditions: [
              { type: 'Available', status: 'True', message: 'Condition must not become an Event' },
            ],
          },
        },
      }
    if (endpoint.endsWith('/deployments/api/rollouts'))
      return {
        body: {
          data: Array.from({ length: 8 }, (_, index) => ({
            name: index === 0 ? 'api-new' : index === 1 ? 'api-old' : `api-history-${index}`,
            namespace: 'monitoring',
            revision: String(12 - index),
            replicas: index === 0 ? (count === 3 ? 2 : count) : index === 1 && count === 3 ? 1 : 0,
            readyReplicas:
              index === 0 ? (count === 3 ? 2 : count) : index === 1 && count === 3 ? 1 : 0,
            images: [`registry/api:v${12 - index}`],
            createdAt: '2026-10-30T10:00:00Z',
          })),
        },
      }
    if (endpoint === '/clusters/cluster-a/resources/graph')
      return {
        body: {
          data: {
            rootId: 'deployment',
            clusterId: 'cluster-a',
            generatedAt: '2026-10-30T10:00:00Z',
            warnings: [],
            evidence: [],
            nodes: [
              ['deployment', 'Deployment', 'api'],
              ['new', 'ReplicaSet', 'api-new'],
              ['old', 'ReplicaSet', 'api-old'],
              ['config', 'ConfigMap', 'api-config'],
              ...pods.map((pod) => [pod.name, 'Pod', pod.name]),
            ].map(([id, kind, name]) => ({
              id,
              resource: {
                clusterId: 'cluster-a',
                namespace: 'monitoring',
                kind,
                name,
                apiVersion: 'v1',
                scopeMode: 'namespace',
              },
            })),
            edges: [
              ['deployment', 'new', 'owns'],
              ['deployment', 'old', 'owns'],
              ['deployment', 'config', 'uses-config'],
              ...pods.map((pod) => [pod.name.includes('-old-') ? 'old' : 'new', pod.name, 'owns']),
            ].map(([sourceId, targetId, relation], index) => ({
              id: String(index),
              sourceId,
              targetId,
              relation,
            })),
          },
        },
      }
    if (endpoint.endsWith('/deployments/api/metrics')) {
      state.ranges.push(url.searchParams.get('rangeMinutes') || '')
      if (state.failMetrics)
        return {
          status: 503,
          body: {
            error: { code: 'metrics_unavailable', message: 'Synthetic metrics unavailable' },
          },
        }
      return {
        body: {
          data: {
            configured: true,
            source: 'synthetic',
            resourceKind: 'Deployment',
            resourceName: 'api',
            rangeMinutes: Number(url.searchParams.get('rangeMinutes') || 60),
            stepSeconds: 60,
            generatedAt: '2026-10-30T10:00:00Z',
            series: [
              ['cpu', 'cores', 0.42],
              ['memory', 'bytes', 1200000000],
              ['network_rx', 'bytes/s', 24576],
              ['network_tx', 'bytes/s', 9523],
              ['disk_read', 'bytes/s', 36864],
              ['disk_write', 'bytes/s', 24576],
              ['connections', 'count', 36],
            ].map(([key, unit, latest]) => ({
              key,
              unit,
              latest,
              label: key,
              points: [
                { timestamp: '2026-10-30T09:59:00Z', value: latest },
                { timestamp: '2026-10-30T10:00:00Z', value: latest },
              ],
            })),
          },
        },
      }
    }
    if (endpoint === '/delivery/manifest-deployments' && method === 'GET')
      return { body: { data: { items: [], total: 0, page: 1, pageSize: 100 } } }
    if (endpoint === '/clusters/cluster-a/workloads/pods' && method === 'GET')
      return { body: { data: pods } }
    if (endpoint === '/clusters/cluster-a/events') return { body: { data: [] } }
    if (endpoint.endsWith('/deployments/api/yaml'))
      return {
        body: {
          data: {
            kind: 'Deployment',
            name: 'api',
            content: 'apiVersion: apps/v1\nkind: Deployment\nmetadata:\n  name: api\n',
          },
        },
      }
    if (options.bound) {
      if (endpoint === '/application-environments')
        return {
          body: {
            data: [
              {
                id: 'binding-a',
                applicationId: 'app-a',
                environmentId: 'test',
                environmentKey: '测试',
                targets: [
                  {
                    id: 'target-a',
                    clusterId: 'cluster-a',
                    namespace: 'monitoring',
                    workloadKind: 'Deployment',
                    workloadName: 'api',
                    enabled: true,
                  },
                ],
              },
            ],
          },
        }
      if (endpoint === '/applications')
        return { body: { data: [{ id: 'app-a', name: '绑定的应用' }] } }
      if (['/builds', '/workflows', '/releases'].includes(endpoint)) {
        if (state.failDelivery && endpoint === '/builds')
          return {
            status: 503,
            body: {
              error: { code: 'builds_unavailable', message: 'Synthetic builds unavailable' },
            },
          }
        const prefix = endpoint.slice(1)
        return {
          body: {
            data: [
              {
                id: `${prefix}-older`,
                applicationId: 'app-a',
                clusterId: 'cluster-a',
                namespace: 'monitoring',
                deploymentName: 'api',
                status: 'succeeded',
                createdAt: '2026-10-29T10:00:00Z',
              },
              {
                id: `${prefix}-latest`,
                applicationId: 'app-a',
                clusterId: 'cluster-a',
                namespace: 'monitoring',
                deploymentName: 'api',
                status: 'succeeded',
                createdAt: '2026-10-30T10:00:00Z',
              },
              {
                id: `${prefix}-unrelated`,
                applicationId: 'app-b',
                clusterId: 'cluster-b',
                namespace: 'other',
                deploymentName: 'api',
                status: 'succeeded',
                createdAt: '2026-10-31T10:00:00Z',
              },
            ],
          },
        }
      }
    }
    if (
      ['/applications', '/application-environments', '/builds', '/workflows', '/releases'].includes(
        endpoint,
      )
    )
      return { body: { data: [] } }
    if (endpoint.startsWith('/clusters/cluster-a/workloads/deployments') && method !== 'GET') {
      state.writes.push({
        path: endpoint,
        body: request.postData() ? request.postDataJSON() : null,
      })
      if (state.failScale && endpoint.endsWith('/scale'))
        return {
          status: 409,
          body: { error: { code: 'scale_rejected', message: 'Synthetic scale rejection' } },
        }
      return { body: { data: null } }
    }
    return undefined
  })
  return { state, evidence }
}

test('Deployment detail connects eight tabs, four header actions, continuous history and actual ownership', async ({
  context,
  page,
}) => {
  const { state, evidence } = await setup(context, page)
  await page.goto(path)
  await expect(page.getByRole('heading', { name: 'api', exact: true })).toBeVisible()
  for (const width of [1440, 900, 320]) {
    await page.setViewportSize({ width, height: 1000 })
    await expect
      .poll(() =>
        page.evaluate(() => {
          const header = document.querySelector('.soha-deployment-header')!.getBoundingClientRect()
          const overview = document
            .querySelector('.soha-deployment-overview')!
            .getBoundingClientRect()
          return {
            leftAligned: Math.abs(header.left - overview.left) <= 1,
            rightAligned: Math.abs(header.right - overview.right) <= 1,
            pageOverflow: document.documentElement.scrollWidth > innerWidth,
          }
        }),
      )
      .toEqual({ leftAligned: true, rightAligned: true, pageOverflow: false })
  }
  await page.setViewportSize({ width: 1440, height: 1000 })
  const actions = page.locator('.soha-deployment-actions')
  await expect(actions.getByRole('button')).toHaveCount(4)
  await expect(actions).not.toContainText('YAML')
  await expect(actions).not.toContainText('回滚')
  expect(
    state.reads.some((endpoint) =>
      ['/applications', '/application-environments', '/builds', '/workflows', '/releases'].includes(
        endpoint,
      ),
    ),
  ).toBe(false)
  await page.getByRole('tab', { name: '版本历史', exact: true }).click()
  await expect(page.locator('.soha-deployment-history-row')).toHaveCount(8)
  await expect(page.locator('.soha-deployment-history .ant-pagination')).toHaveCount(0)
  await page.getByRole('textbox', { name: '搜索版本' }).fill('v5')
  await expect(page.locator('.soha-deployment-history-row')).toHaveCount(1)
  await expect(page.locator('.soha-deployment-history-row')).toContainText('版本 5')
  await page.getByRole('textbox', { name: '搜索版本' }).fill('')
  await page.getByRole('button', { name: '回滚到此版本', exact: true }).nth(1).click()
  await expect(page.getByRole('dialog')).toContainText('目标版本 11')
  await page.getByRole('button', { name: '取消', exact: true }).click()
  expect(state.writes).toHaveLength(0)
  await page.getByRole('tab', { name: '关联资源', exact: true }).click()
  const branches = page.locator('.soha-deployment-relation-branches > section')
  await expect(branches).toHaveCount(2)
  await expect(branches.nth(0)).toContainText('api-new-000')
  await expect(branches.nth(0)).not.toContainText('api-old-002')
  await expect(branches.nth(1)).toContainText('api-old-002')
  await page.getByRole('switch', { name: '历史 ReplicaSets' }).click()
  await expect(branches).toHaveCount(8)
  const config = page.getByRole('button', { name: /ConfigMap api-config/ })
  await config.focus()
  await config.press('Enter')
  await expect(page.locator('.soha-deployment-selected')).toContainText('api-config')
  await page.getByRole('button', { name: '关闭资源信息' }).click()
  await expect(config).toBeFocused()
  verifyBrowserEvidence(evidence)
})

test('Deployment metrics keep four cards, range scope and true empty Events', async ({
  context,
  page,
}) => {
  const { state, evidence } = await setup(context, page)
  await page.goto(`${path}&tab=metrics&range=15`)
  await expect(page.locator('.soha-pod-detail-metric-card')).toHaveCount(4)
  await expect(page.locator('.soha-pod-detail-metric-card').first()).toContainText('420 mCPU')
  await expect.poll(() => state.ranges).toContain('15')
  await page
    .getByRole('radiogroup', { name: '指标时间范围' })
    .getByText('6 小时', { exact: true })
    .click()
  await expect(page).toHaveURL(/range=360/)
  await expect.poll(() => state.ranges).toContain('360')
  state.failMetrics = true
  await page.getByRole('button', { name: '刷新 Deployment', exact: true }).click()
  const metricsPanel = page.getByRole('tabpanel', { name: '指标', exact: true })
  await expect(metricsPanel).toContainText('Synthetic metrics unavailable')
  state.failMetrics = false
  await metricsPanel.getByRole('button', { name: /^重\s*试$/ }).click()
  await expect(page.locator('.soha-pod-detail-metric-card')).toHaveCount(4)
  await page.getByRole('tab', { name: '事件', exact: true }).click()
  await expect(page.getByRole('tabpanel', { name: '事件', exact: true })).toContainText(
    '没有匹配的 Deployment 事件',
  )
  await expect(page.getByRole('tabpanel', { name: '事件', exact: true })).not.toContainText(
    'Condition must not become an Event',
  )
  verifyBrowserEvidence(evidence)
})

test('Deployment delivery reads exact binding, selects latest records and preserves cached results on failure', async ({
  context,
  page,
}) => {
  const { state, evidence } = await setup(context, page, { bound: true })
  await page.goto(`${path}&tab=delivery`)
  const panel = page.getByRole('tabpanel', { name: '交付联动', exact: true })
  await expect(panel).toContainText('绑定的应用')
  for (const prefix of ['builds', 'workflows', 'releases']) {
    await expect(panel).toContainText(`${prefix}-latest`)
    await expect(panel).not.toContainText(`${prefix}-older`)
    await expect(panel).not.toContainText(`${prefix}-unrelated`)
  }
  state.failDelivery = true
  await page.getByRole('button', { name: '刷新 Deployment', exact: true }).click()
  await expect(panel).toContainText('Synthetic builds unavailable')
  await expect(panel).toContainText('builds-latest')
  state.failDelivery = false
  await panel.getByRole('button', { name: '重试读取构建记录', exact: true }).click()
  await expect(panel).not.toContainText('Synthetic builds unavailable')
  expect(state.writes).toHaveLength(0)
  verifyBrowserEvidence(evidence)
})

test('Deployment mutations keep target confirmations, failure feedback and exact scale payload', async ({
  context,
  page,
}) => {
  const { state, evidence } = await setup(context, page)
  await page.goto(path)
  await page.getByRole('button', { name: '重启', exact: true }).click()
  await expect(page.getByRole('dialog')).toContainText('cluster-a / monitoring / api')
  await page.getByRole('button', { name: '取消', exact: true }).click()
  expect(state.writes).toHaveLength(0)
  await page.getByRole('button', { name: '扩缩容', exact: true }).click()
  await page.getByRole('spinbutton', { name: '期望副本数', exact: true }).fill('4')
  state.failScale = true
  await page.getByRole('dialog').getByRole('button', { name: '扩缩容', exact: true }).click()
  await expect(page.getByRole('dialog')).toContainText('Synthetic scale rejection')
  expect(state.writes).toEqual([
    {
      path: '/clusters/cluster-a/workloads/deployments/scale',
      body: { namespace: 'monitoring', name: 'api', replicas: 4 },
    },
  ])
  state.failScale = false
  await page.getByRole('dialog').getByRole('button', { name: '扩缩容', exact: true }).click()
  await expect(page.getByRole('dialog')).toHaveCount(0)
  expect(state.writes).toHaveLength(2)
  verifyBrowserEvidence(evidence)
})

test('Deployment complete Pod table virtualizes, filters and stays bounded on narrow screens', async ({
  context,
  page,
}) => {
  const { state, evidence } = await setup(context, page, { count: 80 })
  await page.goto(`${path}&tab=pods`)
  await expect(page.locator('.soha-deployment-pods')).toContainText('80 / 80')
  expect(
    await page.locator('.ant-table-tbody-virtual-holder-inner .ant-table-row').count(),
  ).toBeLessThan(50)
  await expect(page.locator('.soha-deployment-pods .ant-pagination')).toHaveCount(0)
  const region = page
    .getByRole('tabpanel', { name: 'Pods', exact: true })
    .getByRole('region', { name: '资源列表，可连续滚动' })
  await region.focus()
  await region.press('End')
  await expect.poll(() => region.evaluate((el) => el.scrollTop)).toBeGreaterThan(500)
  const anchor = () =>
    region.evaluate((el) => {
      const top = el.getBoundingClientRect().top
      const row = [...el.querySelectorAll<HTMLElement>('[data-row-key]')].find(
        (item) => item.getBoundingClientRect().bottom > top + 1,
      )
      return {
        key: row?.dataset.rowKey,
        offset: row ? Math.round(row.getBoundingClientRect().top - top) : 0,
      }
    })
  const before = await anchor()
  state.pods.unshift({ ...state.pods[0], name: 'api-new-inserted' })
  await page.getByRole('button', { name: '刷新 Deployment', exact: true }).click()
  await expect(page.locator('.soha-deployment-pods')).toContainText('81 / 81')
  await expect.poll(anchor).toEqual(before)
  await page.getByRole('textbox', { name: '搜索 Pod' }).fill('079')
  await expect(page.getByRole('link', { name: 'api-new-079', exact: true })).toHaveAttribute(
    'href',
    /clusterId=cluster-a&namespace=monitoring/,
  )
  await page.getByLabel('切换到深色模式').click()
  await page.setViewportSize({ width: 900, height: 1000 })
  await expect
    .poll(() =>
      page.locator('.soha-deployment-detail').evaluate((el) => el.scrollWidth - el.clientWidth),
    )
    .toBeLessThanOrEqual(1)
  await page.setViewportSize({ width: 320, height: 900 })
  await expect
    .poll(() =>
      page.locator('.soha-deployment-detail').evaluate((el) => el.scrollWidth - el.clientWidth),
    )
    .toBeLessThanOrEqual(1)
  verifyBrowserEvidence(evidence)
})

test('Deployment readonly and zero-replica states do not fabricate write capability or zero metrics', async ({
  context,
  page,
}) => {
  const { state, evidence } = await setup(context, page, { readonly: true, zero: true })
  await page.goto(`${path}&tab=metrics`)
  await expect(page.locator('.soha-deployment-actions').getByRole('button')).toHaveCount(1)
  await expect(page.locator('.soha-pod-detail-metric-missing')).toHaveCount(4)
  await expect(page.locator('.soha-pod-detail-metrics')).toContainText('当前没有 Pod，暂无指标')
  await page.getByRole('tab', { name: '交付联动', exact: true }).click()
  await expect(page.getByRole('tabpanel', { name: '交付联动', exact: true })).toContainText(
    '无权查看交付联动',
  )
  expect(
    state.reads.some((endpoint) =>
      ['/applications', '/application-environments', '/builds', '/workflows', '/releases'].includes(
        endpoint,
      ),
    ),
  ).toBe(false)
  expect(state.writes).toHaveLength(0)
  verifyBrowserEvidence(evidence)
})
