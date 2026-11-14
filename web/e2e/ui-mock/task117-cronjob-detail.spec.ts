import { test, expect, type BrowserContext, type Page } from '@playwright/test'
import { baselineResponse } from '../fixtures/baseline-data'
import { installMocks, verifyBrowserEvidence } from '../fixtures/mock-app'
import { expectWorkloadDetailFrame } from '../fixtures/workload-detail-geometry'
const prefix = '/api/v1/clusters/cluster-a/workloads/cronjobs/nightly'
const path = '/workloads/cronjobs/nightly?clusterId=cluster-a&namespace=monitoring'
async function setup(
  context: BrowserContext,
  page: Page,
  { readonly = false, empty = false } = {},
) {
  const now = Date.now()
  const jobs = Array.from({ length: empty ? 0 : 35 }, (_, i) => ({
    name: `nightly-${String(i).padStart(3, '0')}`,
    namespace: 'monitoring',
    succeeded: i === 1 ? 0 : 1,
    failed: i === 1 ? 3 : 0,
    active: 0,
    ageSeconds: 100,
    startTime: new Date(now - 180_000 - i * 1000).toISOString(),
    conditions: [
      {
        type: i === 1 ? 'Failed' : 'Complete',
        status: 'True',
        lastTransitionTime: new Date(now - 120_000 - i * 1000).toISOString(),
      },
    ],
  }))
  const detail = {
    name: 'nightly',
    namespace: 'monitoring',
    schedule: '*/2 * * * *',
    timeZone: 'Asia/Shanghai',
    suspend: empty,
    activeJobs: 0,
    ageSeconds: 1000,
    concurrencyPolicy: 'Forbid',
    jobs,
    allowedActions: readonly ? ['view'] : ['view', 'suspend', 'delete'],
  }
  const state = {
    detail,
    failDetail: false,
    failConfig: false,
    failMutation: false,
    writes: [] as string[],
    reads: [] as string[],
  }
  await context.addInitScript(() =>
    localStorage.setItem(
      'soha-scope',
      JSON.stringify({ state: { clusterId: 'cluster-a', namespace: 'monitoring' }, version: 0 }),
    ),
  )
  const evidence = await installMocks(context, page, (url, method, body) => {
    const endpoint = url.pathname
    if (method === 'GET') state.reads.push(endpoint)
    if (endpoint === '/api/v1/access/permission-snapshot') {
      const body = baselineResponse(endpoint)!
      if (!body.data || !('permissionKeys' in body.data))
        throw new Error('Permission fixture missing')
      body.data.permissionKeys = body.data.permissionKeys!.filter(
        (k) => !k.startsWith('platform.workloads.cron-jobs.') && !k.startsWith('delivery.'),
      )
      body.data.permissionKeys.push(
        'platform.workloads.cron-jobs.view',
        'platform.workloads.jobs.view',
        'platform.pods.view',
        ...(readonly
          ? []
          : [
              'platform.workloads.cron-jobs.suspend',
              'platform.workloads.cron-jobs.delete',
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
      url.searchParams.get('kind') === 'CronJob' &&
      url.searchParams.get('name') === 'nightly'
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
              ['root', 'CronJob', 'nightly'],
              ...jobs.map((job) => [job.name, 'Job', job.name]),
              ['pod', 'Pod', 'execution-pod'],
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
              ...jobs.map((job) => ['root', job.name, 'controls']),
              ...(jobs.length ? [[jobs[0].name, 'pod', 'controls']] : []),
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
        ? { status: 503, body: { error: { code: 'unavailable', message: 'CronJob read failed' } } }
        : { body: { data: { ...state.detail, jobs: empty ? undefined : state.detail.jobs } } }
    if (endpoint === prefix + '/yaml')
      return state.failConfig
        ? {
            status: 503,
            body: { error: { code: 'unavailable', message: 'CronJob configuration unavailable' } },
          }
        : {
            body: {
              data: {
                kind: 'CronJob',
                name: 'nightly',
                namespace: 'monitoring',
                content:
                  'apiVersion: batch/v1\nkind: CronJob\nmetadata: {name: nightly, namespace: monitoring}\nspec: {schedule: "*/2 * * * *", successfulJobsHistoryLimit: 3, failedJobsHistoryLimit: 1, startingDeadlineSeconds: 0, jobTemplate: {spec: {backoffLimit: 0, template: {spec: {containers: [{name: app, image: busybox:1.38, command: [sh, -c], args: [echo done]}]}}}}}',
              },
            },
          }
    if (endpoint === prefix + '/suspend' && method === 'POST') {
      state.writes.push(endpoint + url.search)
      if (state.failMutation)
        return {
          status: 500,
          body: { error: { code: 'failed', message: 'Schedule change failed' } },
        }
      state.detail.suspend = Boolean((body.postDataJSON() as { suspend: boolean }).suspend)
      return { body: { data: state.detail } }
    }
    if (endpoint === prefix && method === 'DELETE') {
      state.writes.push(endpoint)
      return { body: { data: {} } }
    }
    if (/\/workloads\/jobs\/nightly-\d+\/detail$/.test(endpoint))
      return {
        body: {
          data: {
            name: endpoint.split('/').at(-2),
            namespace: 'monitoring',
            pods: [
              {
                name: 'execution-pod',
                namespace: 'monitoring',
                phase: 'Succeeded',
                readyContainers: '0/1',
                restarts: 0,
                ageSeconds: 100,
              },
            ],
          },
        },
      }
    if (endpoint === '/api/v1/clusters/cluster-a/events')
      return {
        body: {
          items: [
            {
              name: 'scheduled',
              namespace: 'monitoring',
              involvedKind: 'CronJob',
              involvedName: 'nightly',
              type: 'Normal',
              reason: 'SuccessfulCreate',
              message: 'Created execution',
              count: 3,
              ageSeconds: 10,
            },
          ],
        },
      }
    if (endpoint === '/api/v1/application-environments')
      return {
        body: {
          items: [
            {
              id: 'right',
              applicationId: 'cron-app',
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
              id: 'wrong',
              applicationId: 'wrong-app',
              targets: [
                {
                  clusterId: 'cluster-a',
                  namespace: 'monitoring',
                  workloadKind: 'Deployment',
                  workloadName: 'nightly',
                  enabled: true,
                },
              ],
            },
          ],
        },
      }
    if (endpoint === '/api/v1/delivery/manifest-deployments') return { body: { items: [] } }
    return undefined
  })
  return { state, evidence }
}

test('CronJob seven tabs, retained execution metrics, selected Job Pods and scoped delivery', async ({
  context,
  page,
}) => {
  const { evidence, state } = await setup(context, page)
  await page.goto(path)
  await expect(page.getByRole('heading', { name: 'nightly', exact: true })).toBeVisible()
  await expectWorkloadDetailFrame(page)
  for (const label of ['概览', 'Jobs', '事件', '指标', '关联资源', '交付联动', 'YAML'])
    await expect(page.getByRole('tab', { name: label, exact: true })).toBeVisible()
  await expect(page.getByText('Asia/Shanghai', { exact: true }).first()).toBeVisible()
  await page.getByRole('tab', { name: 'Jobs', exact: true }).click()
  await expect(page.locator('.soha-cronjob-jobs .ant-pagination')).toHaveCount(0)
  await expect(page.locator('.soha-cronjob-jobs .ant-table-tbody-virtual-holder')).toBeVisible()
  await page.getByRole('textbox', { name: '搜索 Job' }).fill('nightly-001')
  await expect(page.getByRole('link', { name: 'nightly-001', exact: true })).toHaveAttribute(
    'href',
    /namespace=monitoring/,
  )
  await expect(page.getByText('失败', { exact: true }).last()).toBeVisible()
  await page.getByRole('button', { name: '刷新 CronJob', exact: true }).click()
  await expect(page.getByRole('textbox', { name: '搜索 Job' })).toHaveValue('nightly-001')
  await page.getByRole('tab', { name: '指标', exact: true }).click()
  await expect(page.locator('.soha-cronjob-metrics .ant-card')).toHaveCount(6)
  await expect(page.getByText('当前 UID 保留的 35 个 Jobs', { exact: false })).toBeVisible()
  await page.getByText('15 分钟', { exact: true }).click()
  await expect(page).toHaveURL(/range=15/)
  await page.getByRole('tab', { name: '关联资源', exact: true }).click()
  await expect(
    page.getByRole('tabpanel', { name: '关联资源', exact: true }).locator('.react-flow'),
  ).toBeVisible()
  const relationships = page.getByRole('tabpanel', { name: '关联资源', exact: true })
  await expect(relationships.locator('.react-flow__node[data-id="pod"]')).toContainText(
    'execution-pod',
  )
  await relationships.locator('.react-flow__node[data-id="pod"]').click()
  await expect(relationships.getByRole('complementary', { name: '选中资源信息' })).toContainText(
    '受控于',
  )
  await page.getByRole('tab', { name: '交付联动', exact: true }).click()
  await expect(page.getByRole('link', { name: 'cron-app', exact: true })).toBeVisible()
  await expect(page.getByRole('link', { name: 'wrong-app', exact: true })).toHaveCount(0)
  expect(state.reads.some((p) => /\/workloads\/cronjobs\/nightly\/metrics$/.test(p))).toBe(false)
  await page.getByRole('tab', { name: '事件', exact: true }).click()
  await expect(page.getByText('Created execution', { exact: true })).toBeVisible()
  await verifyBrowserEvidence(evidence)
})

test('CronJob suspend requires confirmation, cancel sends nothing, resume preserves scope', async ({
  context,
  page,
}) => {
  const { state, evidence } = await setup(context, page)
  await page.goto(path)
  await page.getByRole('button', { name: '暂停', exact: true }).click()
  await expect(page.getByRole('dialog')).toContainText('已经启动的 Jobs 会继续执行')
  await page.getByRole('dialog').getByRole('button', { name: '取消', exact: true }).click()
  await expect(page.getByRole('dialog')).toHaveCount(0)
  expect(state.writes).toEqual([])
  await page.getByRole('button', { name: '暂停', exact: true }).click()
  await page.getByRole('dialog').getByRole('button', { name: '暂停', exact: true }).click()
  await expect(page.getByRole('dialog')).toHaveCount(0)
  await expect(page.getByRole('button', { name: '恢复', exact: true })).toBeVisible()
  expect(state.writes[0]).toContain('namespace=monitoring')
  await page.getByRole('button', { name: '恢复', exact: true }).click()
  await page.getByRole('dialog').getByRole('button', { name: '恢复', exact: true }).click()
  await expect(page.getByRole('dialog')).toHaveCount(0)
  await expect(page.getByRole('button', { name: '暂停', exact: true })).toBeVisible()
  await verifyBrowserEvidence(evidence)
})

test('CronJob paused empty execution, readonly permission and narrow layout', async ({
  context,
  page,
}) => {
  const { evidence } = await setup(context, page, { readonly: true, empty: true })
  await page.setViewportSize({ width: 980, height: 1000 })
  await page.goto(path)
  await expect(page.getByText('后续调度已暂停', { exact: true })).toBeVisible()
  await expect(page.getByRole('button', { name: '恢复', exact: true })).toHaveCount(0)
  await expect(page.getByRole('button', { name: '删除', exact: true })).toHaveCount(0)
  await page.getByRole('tab', { name: 'Jobs', exact: true }).click()
  await expect(
    page.getByText('暂无保留的 Job，可能尚未调度或已按保留策略清理', { exact: true }),
  ).toBeVisible()
  await page.getByRole('tab', { name: '交付联动', exact: true }).click()
  await expect(page.getByText('无权查看交付联动', { exact: true })).toBeVisible()
  expect(
    await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth + 1),
  ).toBe(true)
  await verifyBrowserEvidence(evidence)
})

test('CronJob errors remain visible and a declined read cannot show previous data', async ({
  context,
  page,
}) => {
  const { state, evidence } = await setup(context, page)
  state.failConfig = true
  await page.goto(path)
  await expect(
    page.getByText('CronJob configuration unavailable', { exact: false }).first(),
  ).toBeVisible()
  await expect(page.getByRole('heading', { name: 'nightly', exact: true })).toBeVisible()
  state.failConfig = false
  state.failDetail = true
  await page.reload()
  await expect(page.getByText('CronJob read failed', { exact: false }).first()).toBeVisible()
  await expect(page.getByRole('heading', { name: 'nightly', exact: true })).toHaveCount(0)
  await verifyBrowserEvidence(evidence)
})
