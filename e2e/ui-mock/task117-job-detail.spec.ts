import { test, expect, type BrowserContext, type Page } from '@playwright/test'
import { baselineResponse } from '../fixtures/baseline-data'
import { installMocks, verifyBrowserEvidence } from '../fixtures/mock-app'
import { expectWorkloadDetailFrame } from '../fixtures/workload-detail-geometry'
const prefix = '/api/v1/clusters/cluster-a/workloads/jobs/report'
const path = '/workloads/jobs/report?clusterId=cluster-a&namespace=monitoring'
async function setup(context: BrowserContext, page: Page, { readonly = false, count = 3 } = {}) {
  const pods = Array.from({ length: count }, (_, i) => ({
    name: `run-${String(i).padStart(3, '0')}`,
    namespace: 'monitoring',
    phase: 'Succeeded',
    nodeName: 'worker-a',
    readyContainers: '0/1',
    restarts: 0,
    ageSeconds: 600,
    allowedActions: ['view', 'logs'],
    containerStatuses: [
      { name: 'app', kind: 'container', state: 'terminated', ready: false, exitCode: 0 },
    ],
  }))
  const detail = {
    name: 'report',
    namespace: 'monitoring',
    createdAt: '2026-11-01T00:00:00Z',
    startTime: '2026-11-01T00:00:01Z',
    completionTime: '2026-11-01T00:00:43Z',
    completions: 1,
    completionTarget: 1,
    parallelism: 1,
    succeeded: 1,
    failed: 1,
    active: 0,
    completionMode: 'NonIndexed',
    backoffLimit: 0,
    suspend: false,
    conditions: [
      {
        type: 'Complete',
        status: 'True',
        reason: 'CompletionsReached',
        message: 'Reached expected number of succeeded pods',
        lastTransitionTime: '2026-11-01T00:00:43Z',
      },
    ],
    labels: { app: 'report' },
    annotations: { source: 'fixture' },
    pods,
    relatedResources: [
      { kind: 'CronJob', name: 'nightly', namespace: 'monitoring', relation: 'owner' },
      { kind: 'ConfigMap', name: 'report-config', namespace: 'monitoring', relation: 'config' },
    ],
    allowedActions: readonly ? ['view'] : ['view', 'delete'],
  }
  const state = {
    detail,
    reads: [] as string[],
    writes: [] as string[],
    failMetrics: false,
    failDelete: false,
    denyDetail: false,
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
      if (!('permissionKeys' in body.data)) throw new Error('Missing permission fixture')
      body.data.permissionKeys = body.data.permissionKeys!.filter(
        (k) => !k.startsWith('platform.workloads.jobs.') && !k.startsWith('delivery.'),
      )
      body.data.permissionKeys.push(
        'platform.workloads.jobs.view',
        'platform.pods.view',
        'platform.pods.logs',
        'platform.nodes.view',
        'platform.workloads.cron-jobs.view',
        ...(readonly ? [] : ['platform.workloads.jobs.delete', 'delivery.applications.view']),
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
      url.searchParams.get('kind') === 'Job' &&
      url.searchParams.get('name') === 'report'
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
              ['root', 'Job', 'report'],
              ['owner', 'CronJob', 'nightly'],
              ['config', 'ConfigMap', 'report-config'],
              ['pod', 'Pod', 'run-000'],
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
              ['owner', 'root', 'controls'],
              ['root', 'pod', 'controls'],
              ['pod', 'config', 'uses-config'],
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
      return state.denyDetail
        ? { status: 403, body: { error: { code: 'denied', message: 'Job denied' } } }
        : { body: { data: state.detail } }
    if (endpoint === prefix + '/yaml')
      return {
        body: {
          data: {
            kind: 'Job',
            name: 'report',
            namespace: 'monitoring',
            content:
              'apiVersion: batch/v1\nkind: Job\nmetadata: {name: report, namespace: monitoring}\nspec: {template: {spec: {containers: [{name: app, image: busybox:1.38, command: [sh, -c], args: [echo done]}]}}}\n',
          },
        },
      }
    if (endpoint === prefix + '/metrics')
      return state.failMetrics
        ? {
            status: 503,
            body: { error: { code: 'unavailable', message: 'Job metrics unavailable' } },
          }
        : {
            body: {
              data: {
                configured: true,
                available: true,
                resourceKind: 'Job',
                resourceName: 'report',
                namespace: 'monitoring',
                source: 'prometheus',
                series: [
                  {
                    key: 'cpu',
                    unit: 'cores',
                    latest: 0,
                    points: [{ timestamp: '2026-11-01T00:00:00Z', value: 0 }],
                  },
                ],
              },
            },
          }
    if (endpoint === '/api/v1/clusters/cluster-a/workloads/pods')
      return { body: { items: pods.map((p) => ({ ...p, cpu: '2m', memory: '16Mi' })) } }
    if (/^\/api\/v1\/clusters\/cluster-a\/workloads\/pods\/run-\d+\/detail$/.test(endpoint))
      return {
        body: {
          data: {
            name: endpoint.split('/').at(-2),
            namespace: 'monitoring',
            phase: 'Succeeded',
            allowedActions: ['logs'],
            containers: [
              { name: 'app', image: 'busybox', ready: false, restartCount: 0, state: 'terminated' },
              {
                name: 'sidecar',
                image: 'busybox',
                ready: false,
                restartCount: 0,
                state: 'terminated',
              },
            ],
          },
        },
      }
    if (/^\/api\/v1\/clusters\/cluster-a\/workloads\/pods\/run-\d+\/logs$/.test(endpoint))
      return {
        body: {
          data: {
            content: `demo log ${endpoint.split('/').at(-2)} ${url.searchParams.get('container')}`,
          },
        },
      }
    if (endpoint === '/api/v1/clusters/cluster-a/events')
      return {
        body: {
          items: [
            {
              name: 'done',
              namespace: 'monitoring',
              involvedKind: 'Job',
              involvedName: 'report',
              reason: 'Completed',
              message: 'Job completed successfully',
              count: 2,
              type: 'Normal',
              ageSeconds: 20,
            },
          ],
        },
      }
    if (endpoint === '/api/v1/delivery/manifest-deployments') return { body: { items: [] } }
    if (endpoint === '/api/v1/application-environments')
      return {
        body: {
          items: [
            {
              id: 'binding',
              applicationId: 'app-a',
              environmentId: 'test',
              targets: [
                {
                  clusterId: 'cluster-a',
                  namespace: 'monitoring',
                  workloadKind: 'CronJob',
                  workloadName: 'nightly',
                  enabled: true,
                },
              ],
            },
            {
              id: 'wrong-kind',
              applicationId: 'wrong-app',
              environmentId: 'test',
              targets: [
                {
                  clusterId: 'cluster-a',
                  namespace: 'monitoring',
                  workloadKind: 'Deployment',
                  workloadName: 'report',
                  enabled: true,
                },
              ],
            },
          ],
        },
      }
    if (endpoint === prefix && method === 'DELETE') {
      state.writes.push(endpoint + url.search)
      return state.failDelete
        ? { status: 409, body: { error: { code: 'rejected', message: 'Job deletion rejected' } } }
        : { body: { data: null } }
    }
    return undefined
  })
  return { state, evidence }
}

test('Job: eight tabs, native result, complete virtual Pods, timeline and scoped metrics', async ({
  context,
  page,
}) => {
  const { state, evidence } = await setup(context, page, { count: 80 })
  await page.goto(path)
  await expect(page.locator('.soha-workload-detail-header h1')).toHaveText('report')
  await expect(page.getByRole('tab')).toHaveCount(8)
  await expect(page.locator('.soha-workload-detail-header')).toContainText('已完成')
  await expect(page.locator('.soha-workload-detail-header')).toContainText('42s')
  await expect(page.getByRole('button', { name: '扩缩容', exact: true })).toHaveCount(0)
  await expect(page.getByRole('button', { name: '重启', exact: true })).toHaveCount(0)
  await expectWorkloadDetailFrame(page)
  await expect(page.locator('.soha-workload-overview')).toContainText(
    '失败 Pod 记录不等于 Job 终态失败',
  )
  expect(state.reads.some((p) => p.includes('/metrics') || p.includes('/events'))).toBe(false)
  await page.getByRole('tab', { name: 'Pods', exact: true }).click()
  const region = page.getByRole('tabpanel', { name: 'Pods', exact: true })
  expect(await region.locator('.ant-table-row[data-row-key]').count()).toBeLessThan(80)
  await expect(region.locator('.ant-pagination')).toHaveCount(0)
  await region.locator('.ant-table-tbody-virtual-holder').evaluate((el) => {
    el.scrollTop = el.scrollHeight
    el.dispatchEvent(new Event('scroll', { bubbles: true }))
  })
  await expect(region.getByRole('link', { name: 'run-079', exact: true })).toBeVisible()
  await page.getByRole('button', { name: '刷新 Job', exact: true }).click()
  await expect(region.getByRole('link', { name: 'run-079', exact: true })).toBeVisible()
  await page.getByRole('tab', { name: '事件', exact: true }).click()
  await expect(page.getByRole('tabpanel', { name: '事件' })).toContainText(
    'Job completed successfully',
  )
  await page.getByRole('tab', { name: '指标', exact: true }).click()
  await expect(page.locator('.soha-pod-detail-metric-card')).toHaveCount(4)
  await expect(page.locator('.soha-pod-detail-metric-card').first()).toContainText('0 mCPU')
  state.failMetrics = true
  await page.getByRole('button', { name: '刷新指标', exact: true }).click()
  await expect(page.getByRole('tabpanel', { name: '指标' })).toContainText(
    'Job metrics unavailable',
  )
  await page.getByRole('tab', { name: '关联资源', exact: true }).click()
  await expect(
    page.getByRole('tabpanel', { name: '关联资源', exact: true }).locator('.react-flow'),
  ).toContainText('nightly')
  await page.locator('.ant-segmented-item-label').getByText('列表', { exact: true }).click()
  await expect(page.getByRole('tabpanel', { name: '关联资源' })).toContainText('report-config')
  await page.getByRole('tab', { name: '交付联动', exact: true }).click()
  await expect(page.getByRole('tabpanel', { name: '交付联动' })).toContainText(
    '绑定来源: CronJob / nightly',
  )
  await expect(page.getByRole('tabpanel', { name: '交付联动' })).not.toContainText('wrong-app')
  expect(state.reads.some((p) => p.includes('/workflows') || p.includes('/releases'))).toBe(false)
  await page.getByRole('tab', { name: 'YAML', exact: true }).click()
  await expect(page.getByRole('tabpanel', { name: 'YAML' })).toBeVisible()
  expect(state.writes).toEqual([])
  verifyBrowserEvidence(evidence)
})

test('Job logs use the selected owned Pod and container, and retain identity on refresh', async ({
  context,
  page,
}) => {
  const { state, evidence } = await setup(context, page)
  await page.goto(path + '&tab=logs')
  await expect(page.getByRole('tabpanel', { name: '日志' })).toContainText('demo log run-000 app')
  await page.getByRole('combobox', { name: '日志 Pod', exact: true }).click()
  await page
    .locator('.ant-select-item-option-content:visible')
    .getByText('run-001 · Succeeded', { exact: true })
    .click()
  await expect(page.getByRole('tabpanel', { name: '日志' })).toContainText('demo log run-001 app')
  await page.getByRole('combobox', { name: '选择容器', exact: true }).click()
  await page
    .locator('.ant-select-item-option-content:visible')
    .getByText('sidecar', { exact: true })
    .click()
  await expect(page.getByRole('tabpanel', { name: '日志' })).toContainText(
    'demo log run-001 sidecar',
  )
  await page.getByRole('button', { name: '刷新 Job', exact: true }).click()
  await expect(page).toHaveURL(/pod=run-001/)
  await expect(page).toHaveURL(/container=sidecar/)
  expect(
    state.reads
      .filter((p) => p.includes('/logs?'))
      .every((p) => p.includes('namespace=monitoring')),
  ).toBe(true)
  verifyBrowserEvidence(evidence)
})

test('Job conditions share the compact workload summary and retain expanded facts on refresh', async ({
  context,
  page,
}) => {
  const { state, evidence } = await setup(context, page)
  state.detail.conditions.push(
    { type: 'FailureTarget', status: 'False', reason: '', message: '', lastTransitionTime: '' },
    { type: 'Suspended', status: 'Unknown', reason: '', message: '', lastTransitionTime: '' },
  )
  await page.goto(path)
  const conditions = page
    .locator('.ant-card')
    .filter({ has: page.locator('.soha-workload-conditions') })
  const disclosure = conditions.locator('details')
  const summary = conditions.locator('summary')
  await expect(summary).toContainText('Complete')
  await expect(summary).toContainText('FailureTarget')
  await expect(summary.locator('.ant-tag')).toHaveText(['是', '否', '未知'])
  await expect(disclosure).not.toHaveAttribute('open')
  await expect(conditions.getByRole('table')).not.toBeVisible()
  await summary.focus()
  await summary.press('Enter')
  await expect(disclosure).toHaveAttribute('open')
  await expect(conditions.getByRole('table')).toBeVisible()
  await expect(conditions.getByRole('table')).toContainText('CompletionsReached')
  await expect(conditions.getByRole('table')).toContainText(
    'Reached expected number of succeeded pods',
  )
  await expect(
    conditions.getByRole('columnheader', { name: '最近变化', exact: true }),
  ).toBeVisible()
  await expect(conditions.getByRole('table')).toContainText('2026/11/01')
  await page.getByRole('button', { name: '刷新 Job', exact: true }).click()
  await expect(disclosure).toHaveAttribute('open')
  await page.getByLabel('切换到深色模式').click()
  await page.setViewportSize({ width: 320, height: 850 })
  await expect
    .poll(() => page.evaluate(() => document.documentElement.scrollWidth - innerWidth))
    .toBeLessThanOrEqual(1)
  await summary.click()
  await expect(disclosure).not.toHaveAttribute('open')
  state.detail.conditions = []
  await page.getByRole('button', { name: '刷新 Job', exact: true }).click()
  await expect(summary).toContainText('未返回运行条件')
  await expect(summary.locator('.ant-tag')).toHaveCount(0)
  verifyBrowserEvidence(evidence)
})

test('Job: missing Pods and readonly feedback, narrow and dark layout remain truthful', async ({
  context,
  page,
}) => {
  const { evidence } = await setup(context, page, { readonly: true, count: 0 })
  await page.goto(path + '&tab=logs')
  await expect(page.getByRole('tabpanel', { name: '日志' })).toContainText('没有可读取日志的 Pod')
  await expect(page.getByRole('button', { name: '删除', exact: true })).toHaveCount(0)
  await page.getByRole('tab', { name: '概览', exact: true }).click()
  await page.setViewportSize({ width: 900, height: 1000 })
  await page.getByLabel('切换到深色模式').click()
  await expectWorkloadDetailFrame(page)
  await page.setViewportSize({ width: 320, height: 850 })
  await expect
    .poll(() => page.evaluate(() => document.documentElement.scrollWidth - innerWidth))
    .toBeLessThanOrEqual(1)
  verifyBrowserEvidence(evidence)
})

test('Job delete retains exact scope and rejected confirmation', async ({ context, page }) => {
  const { state, evidence } = await setup(context, page)
  await page.goto(path)
  await page.getByRole('button', { name: '删除', exact: true }).click()
  const modal = page.getByRole('dialog')
  await expect(modal).toContainText('cluster-a / monitoring / report')
  state.failDelete = true
  await modal.getByRole('button', { name: '删除', exact: true }).click()
  await expect(modal).toContainText('Job deletion rejected')
  expect(state.writes).toEqual([prefix + '?namespace=monitoring'])
  await modal.getByRole('button', { name: '取消', exact: true }).click()
  verifyBrowserEvidence(evidence)
})
