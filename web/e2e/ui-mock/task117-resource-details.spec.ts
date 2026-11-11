import { test, expect, type BrowserContext, type Page } from '@playwright/test'
import { baselineResponse } from '../fixtures/baseline-data'
import { installMocks, verifyBrowserEvidence, type MockResolver } from '../fixtures/mock-app'
import { expectWorkloadDetailFrame } from '../fixtures/workload-detail-geometry'

const root = '/api/v1/clusters/cluster-a'
const controllers = [
  ['statefulsets', 'StatefulSet', 'stateful-sets'],
  ['daemonsets', 'DaemonSet', 'daemon-sets'],
  ['jobs', 'Job', 'jobs'],
  ['cronjobs', 'CronJob', 'cron-jobs'],
  ['replicasets', 'ReplicaSet', 'replica-sets'],
  ['replicationcontrollers', 'ReplicationController', 'replication-controllers'],
] as const
const base = {
  name: 'sample',
  namespace: 'monitoring',
  createdAt: '2026-10-01T00:00:00Z',
  labels: { app: 'sample' },
  annotations: { 'example.test/source': 'fixture' },
}
async function setup(
  context: BrowserContext,
  page: Page,
  permissions: string[],
  responses: Record<string, unknown>,
  resolveDaemonSetDetail?: () => ReturnType<MockResolver>,
) {
  await context.addInitScript(() =>
    localStorage.setItem(
      'soha-scope',
      JSON.stringify({ state: { clusterId: 'cluster-a', namespace: 'monitoring' }, version: 0 }),
    ),
  )
  const reads: string[] = []
  const writes: string[] = []
  const evidence = await installMocks(context, page, (url, method) => {
    if (method === 'GET') reads.push(url.pathname + url.search)
    if (method === 'DELETE') writes.push(url.pathname + url.search)
    if (resolveDaemonSetDetail && url.pathname === `${root}/workloads/daemonsets/sample/detail`)
      return method === 'GET' && url.search === '?namespace=monitoring'
        ? resolveDaemonSetDetail()
        : undefined
    if (url.pathname === '/api/v1/access/permission-snapshot') {
      const body = baselineResponse(url.pathname)!
      if (!body.data || !('permissionKeys' in body.data))
        throw new Error('Missing permission fixture')
      body.data.permissionKeys!.push(...permissions)
      return { body }
    }
    if (url.pathname === '/api/v1/application-environments') return { body: { data: [] } }
    if (url.pathname === '/api/v1/clusters/capabilities')
      return {
        body: {
          data: ['resource.yaml.view', 'resource.yaml.apply', 'helm.releases', 'port.forward'].map(
            (key) => ({
              key,
              direct: { status: 'available' },
              agent: { status: 'available' },
            }),
          ),
        },
      }
    if (url.pathname === `${root}/workloads/jobs/sample/yaml`)
      return {
        body: {
          data: {
            kind: 'Job',
            name: 'sample',
            namespace: 'monitoring',
            content:
              'apiVersion: batch/v1\nkind: Job\nmetadata: {name: sample, namespace: monitoring}\nspec: {template: {spec: {containers: [{name: sample, image: demo:v1}]}}}\n',
          },
        },
      }
    if (/\/workloads\/jobs\/sample-\d+\/detail$/.test(url.pathname))
      return {
        body: {
          data: {
            name: url.pathname.split('/').at(-2),
            namespace: 'monitoring',
            pods: [],
            succeeded: 1,
            active: 0,
            failed: 0,
          },
        },
      }
    if (url.pathname === `${root}/workloads/cronjobs/sample/yaml`)
      return {
        body: {
          data: {
            kind: 'CronJob',
            name: 'sample',
            namespace: 'monitoring',
            content:
              'apiVersion: batch/v1\nkind: CronJob\nmetadata: {name: sample, namespace: monitoring}\nspec: {schedule: "*/5 * * * *", jobTemplate: {spec: {template: {spec: {containers: [{name: app, image: app:1}]}}}}}\n',
          },
        },
      }
    if (url.pathname === `${root}/workloads/replicasets/sample/yaml`)
      return {
        body: {
          data: {
            kind: 'ReplicaSet',
            name: 'sample',
            namespace: 'monitoring',
            content:
              'apiVersion: apps/v1\nkind: ReplicaSet\nmetadata: {name: sample, namespace: monitoring}\nspec: {template: {spec: {containers: [{name: app, image: app:1}]}}}\n',
          },
        },
      }
    if (url.pathname === `${root}/workloads/replicationcontrollers/sample/yaml`)
      return {
        body: {
          data: {
            kind: 'ReplicationController',
            name: 'sample',
            namespace: 'monitoring',
            content:
              'apiVersion: v1\nkind: ReplicationController\nmetadata: {name: sample, namespace: monitoring}\nspec: {template: {spec: {containers: [{name: app, image: app:1}]}}}\n',
          },
        },
      }
    if (url.pathname === `${root}/workloads/daemonsets/sample/rollouts`)
      return { body: { items: [] } }
    if (url.pathname === `${root}/workloads/daemonsets/sample/yaml`)
      return {
        body: {
          data: {
            kind: 'DaemonSet',
            name: 'sample',
            namespace: 'monitoring',
            content:
              'apiVersion: apps/v1\nkind: DaemonSet\nmetadata: {name: sample, namespace: monitoring, generation: 1}\nspec: {updateStrategy: {type: RollingUpdate}, template: {spec: {containers: []}}}\nstatus: {observedGeneration: 1, numberMisscheduled: 0, numberUnavailable: 0}\n',
          },
        },
      }
    if (url.pathname === `${root}/workloads/statefulsets/sample/yaml`) {
      const detail = responses[`${root}/workloads/statefulsets/sample/detail`] as
        | { desiredReplicas?: number }
        | undefined
      return {
        body: {
          data: {
            kind: 'StatefulSet',
            name: 'sample',
            namespace: 'monitoring',
            content: `apiVersion: apps/v1\nkind: StatefulSet\nmetadata:\n name: sample\n namespace: monitoring\nspec:\n template:\n  spec:\n   containers: []\nstatus:\n updatedReplicas: ${detail?.desiredReplicas ?? 0}\n`,
          },
        },
      }
    }
    if (Object.prototype.hasOwnProperty.call(responses, url.pathname + url.search))
      return { body: { data: responses[url.pathname + url.search] } }
    if (Object.prototype.hasOwnProperty.call(responses, url.pathname))
      return { body: { data: responses[url.pathname] } }
    return undefined
  })
  return { reads, writes, evidence }
}
// StatefulSet now has its dedicated nine-tab acceptance suite.
for (const [kind, label, permission] of controllers.filter(
  (entry): boolean => entry[0] !== 'statefulsets',
))
  test(`${label} detail: shared identity, complete children, lazy event timeline and URL tabs`, async ({
    context,
    page,
  }, info) => {
    const pods = Array.from({ length: 80 }, (_, index) => ({
      name: `sample-${String(index).padStart(3, '0')}`,
      namespace: 'monitoring',
      phase: 'Running',
      nodeName: 'worker-a',
      readyContainers: '1/1',
      restarts: 0,
      ageSeconds: 600,
      allowedActions: ['view'],
    }))
    const jobs = pods.map((pod, index) => ({
      ...pod,
      startTime: new Date(Date.now() - (index + 1) * 1000).toISOString(),
      succeeded: 1,
      failed: 0,
      active: 0,
    }))
    const detail = {
      ...base,
      desiredReplicas: 3,
      readyReplicas: 2,
      currentReplicas: 3,
      availableReplicas: 2,
      desiredNumber: 4,
      readyNumber: 3,
      currentNumber: 4,
      updatedNumber: 3,
      availableNumber: 3,
      serviceName: 'database',
      selector: { app: 'sample' },
      updateStrategy: 'RollingUpdate',
      currentRevision: `sample-${'a'.repeat(80)}`,
      updateRevision: `sample-${'b'.repeat(80)}`,
      completions: 3,
      succeeded: 2,
      failed: 0,
      active: 1,
      parallelism: 2,
      schedule: '*/5 * * * *',
      suspend: false,
      activeJobs: 1,
      relatedResources: [
        {
          kind: 'ConfigMap',
          name: 'sample-config',
          namespace: 'monitoring',
          relation: 'uses-config',
        },
      ],
      ...(kind === 'cronjobs' ? { jobs } : { pods }),
    }
    const { reads, evidence } = await setup(
      context,
      page,
      [
        `platform.workloads.${permission}.view`,
        'platform.pods.view',
        'platform.nodes.view',
        'platform.workloads.jobs.view',
      ],
      {
        [`${root}/workloads/${kind}/sample/detail`]: detail,
        [`${root}/workloads/${kind}/sample/metrics`]: {
          configured: true,
          available: true,
          source: 'prometheus',
          rangeMinutes: 60,
          series: [{ key: 'cpu', unit: 'cores', latest: 0.125, points: [] }],
        },
        [`${root}/workloads/${kind}/sample/yaml`]: {
          yaml: `apiVersion: v1\nkind: ${label}\nmetadata:\n  name: sample\n`,
        },
        [`${root}/resources/graph?namespace=monitoring&kind=${label}&name=sample`]: {
          rootId: 'sample',
          clusterId: 'cluster-a',
          generatedAt: '2026-10-31T00:00:00Z',
          nodes: [
            ['sample', label, 'sample'],
            ['config', 'ConfigMap', 'sample-config'],
          ].map(([id, kind, name]) => ({
            id,
            resource: {
              clusterId: 'cluster-a',
              namespace: 'monitoring',
              kind,
              name,
              apiVersion: ['Deployment', 'StatefulSet', 'DaemonSet', 'ReplicaSet'].includes(kind)
                ? 'apps/v1'
                : ['Job', 'CronJob'].includes(kind)
                  ? 'batch/v1'
                  : 'v1',
              scopeMode: 'namespace',
            },
          })),
          edges: [
            { id: 'config-use', sourceId: 'sample', targetId: 'config', relation: 'uses-config' },
          ],
          warnings: [],
          evidence: [],
        },
        [`${root}/workloads/pods`]: pods.map((pod) => ({
          ...pod,
          cpu: '125m',
          memory: '128Mi',
          containerStatuses: [
            { name: 'init', kind: 'init', state: 'terminated', ready: false, exitCode: 0 },
            { name: 'app', kind: 'container', state: 'running', ready: true },
          ],
        })),
        [`${root}/events`]: [
          {
            name: 'sample-started',
            involvedKind: label,
            involvedName: 'sample',
            namespace: 'monitoring',
            reason: 'ResourceObserved',
            message: 'Synthetic controller event',
            type: 'Normal',
            count: 1,
            ageSeconds: 60,
          },
        ],
      },
    )
    await page.goto(`/workloads/${kind}/sample?clusterId=cluster-a&namespace=monitoring`)
    await expect(page.locator('.soha-workload-detail-header h1')).toHaveText('sample')
    await expect(page.locator('.soha-workload-detail-header .soha-metadata-tag')).toHaveText(
      'monitoring',
    )
    const header = page.locator('.soha-workload-detail-header')
    await expect(header.locator('.soha-status-tag')).toHaveText(
      kind === 'cronjobs'
        ? '调度启用'
        : kind === 'jobs'
          ? '执行中'
          : kind === 'daemonsets'
            ? '未就绪'
            : ['replicasets', 'replicationcontrollers'].includes(kind)
              ? '副本未就绪或不可用'
              : '副本未就绪',
    )
    await expect(header).toContainText('已创建')
    await expect(header).not.toContainText('读取于')
    await expect(header.getByRole('switch', { name: /自动刷新/ })).toBeChecked()
    if (kind === 'daemonsets') {
      await expect(header).toContainText('已更新 3 / 4')
      await expect(header).toContainText('RollingUpdate')
    } else if (kind === 'cronjobs') {
      await expect(header).toContainText('*/5 * * * *')
      await expect(header).toContainText('活跃 1')
    } else if (kind === 'jobs') {
      await expect(header).toContainText('成功 2 / 3')
      await expect(header).toContainText('失败 Pod 0')
    }
    await expectWorkloadDetailFrame(page)
    const overview = page.locator('.soha-workload-overview')
    await expect(overview.locator('aside')).toContainText(
      kind === 'cronjobs' ? '调度配置' : '运行配置',
    )
    await expect(overview.locator('.soha-k8s-detail-metadata')).toHaveCount(1)
    const columns = await overview.evaluate((element) => {
      const main = element.children[0].getBoundingClientRect()
      const side = element.children[1].getBoundingClientRect()
      return { sideIsRight: side.left > main.right, aligned: Math.abs(main.top - side.top) < 1 }
    })
    expect(columns).toEqual({ sideIsRight: true, aligned: true })
    await overview.getByRole('button', { name: /标签 · 1/ }).click()
    await expect(overview.locator('.soha-workload-overview-metadata')).toContainText('app:')
    await overview.getByRole('button', { name: /标签 · 1/ }).click()
    expect(
      reads.some(
        (path) =>
          path.includes('/events') ||
          path.includes('/metrics') ||
          (!['daemonsets', 'jobs', 'cronjobs', 'replicasets', 'replicationcontrollers'].includes(
            kind,
          ) &&
            path.includes('/yaml')) ||
          path.startsWith(`${root}/workloads/pods?`),
      ),
    ).toBe(false)
    const children = kind === 'cronjobs' ? 'Jobs' : 'Pods'
    await page.getByRole('tab', { name: children, exact: true }).click()
    await expect(page).toHaveURL(new RegExp(`tab=${children.toLowerCase()}`))
    const panel = page.getByRole('tabpanel', { name: children, exact: true })
    if (kind !== 'cronjobs') {
      await expect(panel.locator('.soha-pod-resource-limit-cell').first()).toContainText('125m')
      await expect(panel.locator('.soha-pod-container-cell').first()).toHaveAttribute(
        'aria-label',
        /init.*app/,
      )
      expect(reads.filter((path) => path.startsWith(`${root}/workloads/pods?`))).toEqual([
        `${root}/workloads/pods?namespace=monitoring`,
      ])
    }
    await expect(panel.locator('.ant-pagination')).toHaveCount(0)
    expect(await panel.locator('.ant-table-row[data-row-key]').count()).toBeLessThan(80)
    const holder = panel.locator('.ant-table-tbody-virtual-holder')
    await holder.evaluate((node) => {
      node.scrollTop = node.scrollHeight
      node.dispatchEvent(new Event('scroll', { bubbles: true }))
    })
    await expect(panel.getByRole('link', { name: 'sample-079', exact: true })).toBeVisible()
    if (kind !== 'cronjobs') {
      const before = reads.filter((path) => path.startsWith(`${root}/workloads/pods?`)).length
      if (kind === 'statefulsets') {
        detail.readyReplicas = 3
        detail.currentRevision = detail.updateRevision
      } else if (kind === 'daemonsets') {
        detail.readyNumber = 4
        detail.updatedNumber = 4
        detail.availableNumber = 4
      } else if (['replicasets', 'replicationcontrollers'].includes(kind)) {
        detail.readyReplicas = 3
        detail.currentReplicas = 3
        detail.availableReplicas = 3
      }
      await page.getByRole('button', { name: `刷新 ${label}`, exact: true }).click()
      await expect
        .poll(() => reads.filter((path) => path.startsWith(`${root}/workloads/pods?`)).length)
        .toBe(before + 1)
      if (kind !== 'jobs') {
        await expect(header.locator('.soha-status-tag')).toHaveText('健康')
        if (kind === 'statefulsets') {
          await expect(header.locator('.soha-controller-header-revision')).toHaveCount(1)
          await expect(header.locator(`[title="${detail.currentRevision}"]`)).toContainText(
            '当前版本',
          )
        } else if (kind === 'daemonsets') {
          await expect(header).toContainText('已更新 4 / 4')
        }
      }
      await expect(panel.getByRole('link', { name: 'sample-079', exact: true })).toBeVisible()
    }
    await page.getByRole('tab', { name: '事件', exact: true }).click()
    await expect(page.getByRole('tabpanel', { name: '事件' })).toContainText(
      'Synthetic controller event',
    )
    expect(reads.some((path) => path.includes('/events?namespace=monitoring'))).toBe(true)
    const eventReads = reads.filter((path) => path.startsWith(`${root}/events?`)).length
    await page.getByRole('button', { name: `刷新 ${label} 事件`, exact: true }).click()
    await expect
      .poll(() => reads.filter((path) => path.startsWith(`${root}/events?`)).length)
      .toBeGreaterThan(eventReads)
    await page.reload()
    await expect(page.getByRole('tab', { name: '事件', exact: true })).toHaveAttribute(
      'aria-selected',
      'true',
    )

    if (kind === 'cronjobs') {
      await page.getByRole('tab', { name: '指标', exact: true }).click()
      await expect(page.locator('.soha-cronjob-metrics .ant-card')).toHaveCount(6)
      expect(
        reads.some((path) => path.startsWith(`${root}/workloads/cronjobs/sample/metrics`)),
      ).toBe(false)
    } else if (
      ['statefulsets', 'daemonsets', 'jobs', 'replicasets', 'replicationcontrollers'].includes(kind)
    ) {
      await page.getByRole('tab', { name: '指标', exact: true }).click()
      await expect(page.locator('.soha-pod-detail-metric-card')).toHaveCount(4)
      await expect(page.locator('.soha-pod-detail-metric-card').first()).toContainText('125 mCPU')
    } else {
      await expect(page.getByRole('tab', { name: '指标', exact: true })).toHaveCount(0)
    }
    expect(reads.some((path) => path.includes('/resources/graph?'))).toBe(false)
    await page.getByRole('tab', { name: '关联资源', exact: true }).click()
    await expect(
      page.getByRole('tabpanel', { name: '关联资源' }).locator('.react-flow'),
    ).toBeVisible()
    expect(reads.filter((path) => path.includes('/resources/graph?'))).toEqual([
      `${root}/resources/graph?namespace=monitoring&kind=${label}&name=sample`,
    ])
    await page
      .getByRole('radiogroup', { name: '关联资源视图' })
      .getByText('列表', { exact: true })
      .click()
    await expect(page.getByRole('radio', { name: '列表', exact: true })).toBeChecked()
    await expect(page.getByRole('tabpanel', { name: '关联资源' })).toContainText('sample-config')
    await expect(
      page.getByRole('tabpanel', { name: '关联资源' }).locator('.ant-pagination'),
    ).toHaveCount(0)
    await page.getByRole('tab', { name: 'YAML', exact: true }).click()
    await expect
      .poll(() =>
        reads.some((path) => path.includes(`/workloads/${kind}/sample/yaml?namespace=monitoring`)),
      )
      .toBe(true)
    await expect(page.getByRole('tabpanel', { name: 'YAML' })).toBeVisible()
    if (kind === 'statefulsets') {
      await page.getByRole('tab', { name: '概览', exact: true }).click()
      await page.setViewportSize({ width: 900, height: 850 })
      await page.getByLabel('切换到深色模式').click()
      await expect(overview.locator('aside')).toContainText(detail.currentRevision)
      await expect(overview.locator('aside')).toContainText(detail.updateRevision)
      expect(await page.evaluate(() => document.documentElement.scrollWidth > innerWidth)).toBe(
        false,
      )
      await page.setViewportSize({ width: 320, height: 850 })
      expect(await page.evaluate(() => document.documentElement.scrollWidth > innerWidth)).toBe(
        false,
      )
      const narrow = await overview.evaluate((element) => {
        const main = element.children[0].getBoundingClientRect()
        const side = element.children[1].getBoundingClientRect()
        return side.top >= main.bottom
      })
      expect(narrow).toBe(true)
      await page.setViewportSize({ width: 900, height: 850 })
      await page.screenshot({
        path: info.outputPath('statefulset-detail-dark.png'),
        fullPage: true,
        animations: 'disabled',
      })
    }
    verifyBrowserEvidence(evidence)
  })

test('detail summary auto refresh has truthful colors, pause, failure retry and preserves YAML drafts', async ({
  context,
  page,
}) => {
  const detail = {
    ...base,
    desiredNumber: 2,
    currentNumber: 2,
    readyNumber: 2,
    updatedNumber: 2,
    availableNumber: 2,
    updateStrategy: 'RollingUpdate',
    allowedActions: ['view', 'update'],
    pods: [],
    relatedResources: [],
  }
  let detailReads = 0
  let failing = false
  let held: Promise<void> | undefined
  const { reads, evidence } = await setup(
    context,
    page,
    ['platform.workloads.daemon-sets.view', 'platform.workloads.daemon-sets.update'],
    {},
    async () => {
      detailReads++
      if (held) await held
      return {
        status: failing ? 500 : 200,
        body: failing
          ? { error: { code: 'test_refresh_failure', message: 'Synthetic refresh failure' } }
          : { data: detail },
      }
    },
  )
  await page.goto('/workloads/daemonsets/sample?clusterId=cluster-a&namespace=monitoring')
  const header = page.locator('.soha-workload-detail-header')
  const toggle = header.getByRole('switch', { name: /自动刷新/ })
  const manual = header.getByRole('button', { name: '刷新 DaemonSet', exact: true })
  await expect(header.locator('.soha-status-tag')).toHaveText('健康')
  await expect(toggle).toHaveAttribute('data-refresh-state', 'live')
  await page.clock.install()
  await toggle.focus()
  await page.keyboard.press('Space')
  await expect(toggle).not.toBeChecked()
  await expect(toggle).toHaveAttribute('data-refresh-state', 'paused')
  let before = detailReads
  await page.clock.fastForward(30_100)
  expect(detailReads).toBe(before)
  await toggle.click()
  await expect(toggle).toBeChecked()
  await page.clock.fastForward(15_100)
  await expect.poll(() => detailReads).toBeGreaterThan(before)
  await expect(toggle).toHaveAttribute('data-refresh-state', 'live')
  await page.getByRole('tab', { name: 'YAML', exact: true }).click()
  const editor = page.getByRole('textbox', { name: 'YAML 草稿编辑器', exact: true })
  await expect(page.locator('.soha-k8s-yaml-editor .view-lines:visible').first()).toContainText(
    'apiVersion',
  )
  await editor.press('ControlOrMeta+Home')
  await editor.pressSequentially('# preserved-ds-draft')
  await editor.press('Enter')
  const yamlBefore = reads.filter((path) => path.includes('/sample/yaml')).length
  before = detailReads
  await page.clock.fastForward(15_100)
  await expect.poll(() => detailReads).toBeGreaterThan(before)
  await expect
    .poll(() => reads.filter((path) => path.includes('/sample/yaml')).length)
    .toBeGreaterThan(yamlBefore)
  await expect(page.locator('.soha-k8s-yaml-editor .view-lines:visible').first()).toContainText(
    'preserved-ds-draft',
  )
  await page.getByRole('tab', { name: '概览', exact: true }).click()
  let release!: () => void
  held = new Promise<void>((resolve) => {
    release = resolve
  })
  try {
    await manual.click()
    await expect(toggle).toHaveAttribute('data-refresh-state', 'refreshing')
    await expect(header.locator('.ant-badge-status-warning')).toHaveCount(1)
    failing = true
  } finally {
    release()
    held = undefined
  }
  await expect(toggle).toHaveAttribute('data-refresh-state', 'failed', { timeout: 10_000 })
  await expect(header.locator('.ant-badge-status-error')).toHaveCount(1)
  await expect(header.locator('.soha-status-tag')).toHaveText('健康')
  before = detailReads
  await page.clock.fastForward(60_100)
  expect(detailReads).toBe(before)
  failing = false
  await manual.click()
  await expect(toggle).toHaveAttribute('data-refresh-state', 'live')
  before = detailReads
  await page.clock.fastForward(15_100)
  await expect.poll(() => detailReads).toBeGreaterThan(before)
  expect(
    reads.some(
      (path) =>
        path.includes('/metrics') || path.includes('/events') || path.includes('/resources/graph'),
    ),
  ).toBe(false)
  verifyBrowserEvidence(evidence)
})

test('a short Pod child table keeps a compact action column and keyboard access to usage', async ({
  context,
  page,
}) => {
  const pod = {
    name: 'sample-0',
    namespace: 'monitoring',
    phase: 'Running',
    readyContainers: '1/1',
    restarts: 0,
    ageSeconds: 60,
    allowedActions: ['view'],
  }
  const { evidence } = await setup(
    context,
    page,
    ['platform.workloads.stateful-sets.view', 'platform.pods.view'],
    {
      [`${root}/workloads/statefulsets/sample/detail`]: {
        ...base,
        desiredReplicas: 1,
        readyReplicas: 1,
        pods: [pod],
      },
      [`${root}/workloads/pods`]: [{ ...pod, cpu: '125m', memory: '128Mi' }],
    },
  )
  await page.setViewportSize({ width: 1280, height: 850 })
  await page.goto(
    '/workloads/statefulsets/sample?clusterId=cluster-a&namespace=monitoring&tab=pods',
  )
  const panel = page.getByRole('tabpanel', { name: 'Pods', exact: true })
  const actionHeader = panel.locator('thead th.soha-table-actions-column')
  await expect.poll(async () => Math.round((await actionHeader.boundingBox())?.width || 0)).toBe(64)
  const cpu = panel.getByRole('button', { name: /sample-0 · CPU · 使用 125m/ })
  const memory = panel.getByRole('button', { name: /sample-0 · 内存 · 使用 128Mi/ })
  await cpu.press('Tab')
  await expect(memory).toBeFocused()
  await expect(memory).toBeInViewport()
  await expect
    .poll(() => panel.locator('.ant-table-body').evaluate((node) => node.scrollLeft))
    .toBeGreaterThan(0)
  await expect(
    panel.getByRole('button', { name: '更多操作：sample-0', exact: true }),
  ).toBeInViewport()
  verifyBrowserEvidence(evidence)
})

const families = [
  {
    route: '/configuration/configmaps/sample',
    path: '/configuration/configmaps/sample/detail',
    permission: 'platform.configuration.config-maps.view',
    detail: { ...base, data: { MODE: 'production' }, dataEntries: 1 },
  },
  {
    route: '/network/services/sample',
    path: '/network/services/sample/detail',
    permission: 'platform.network.services.view',
    configurationYaml: {
      apiVersion: 'v1',
      kind: 'Service',
      configuration: 'spec: {type: ClusterIP, clusterIP: 10.0.0.1}',
    },
    detail: {
      ...base,
      type: 'ClusterIP',
      clusterIp: '10.0.0.1',
      ports: [],
      portMappings: [],
      endpoints: [],
      pods: [],
      selector: { app: 'sample' },
    },
  },
  {
    route: '/storage/persistentvolumeclaims/sample',
    path: '/storage/persistentvolumeclaims/sample/detail',
    permission: 'platform.storage.persistent-volume-claims.view',
    configurationYaml: {
      apiVersion: 'v1',
      kind: 'PersistentVolumeClaim',
      configuration: 'spec: {volumeMode: Filesystem}',
    },
    detail: {
      ...base,
      status: 'Bound',
      requested: '20Gi',
      capacity: '20Gi',
      accessModes: ['ReadWriteOnce'],
      pods: [],
    },
  },
  {
    route: '/platform-access-control/roles/sample',
    path: '/access-control/roles/sample/detail',
    permission: 'platform.access-control.roles.view',
    configurationYaml: {
      apiVersion: 'rbac.authorization.k8s.io/v1',
      kind: 'Role',
      configuration: 'rules: [{apiGroups: [""], resources: [pods], verbs: [get]}]',
    },
    detail: { ...base, rules: 1, ruleSummaries: ['get pods'] },
  },
] as const

test('a zero-replica controller keeps its empty Pods leaf available', async ({ context, page }) => {
  const { evidence } = await setup(
    context,
    page,
    ['platform.workloads.stateful-sets.view', 'platform.pods.view'],
    {
      [`${root}/workloads/statefulsets/sample/detail`]: {
        ...base,
        desiredReplicas: 0,
        readyReplicas: 0,
        currentReplicas: 0,
        pods: null,
      },
    },
  )
  await page.goto('/workloads/statefulsets/sample?namespace=monitoring&tab=pods')
  await expect(page.getByRole('tab', { name: 'Pods', exact: true })).toHaveAttribute(
    'aria-selected',
    'true',
  )
  await expect(page.getByRole('tabpanel', { name: 'Pods' })).toContainText('当前返回数据中没有 Pod')
  expect(await page.locator('.ant-pagination').count()).toBe(0)
  verifyBrowserEvidence(evidence)
})

for (const family of families)
  test(`${family.route}: uniform header, collapsed metadata and scoped refresh`, async ({
    context,
    page,
  }) => {
    const detailPath = `${root}${family.path}?namespace=monitoring`
    const yamlPath = detailPath.replace('/detail?', '/yaml?')
    const configuration = 'configurationYaml' in family ? family.configurationYaml : undefined
    const { reads, evidence } = await setup(context, page, [family.permission], {
      [detailPath]: family.detail,
      ...(configuration
        ? {
            [yamlPath]: {
              content: [
                `apiVersion: ${configuration.apiVersion}`,
                `kind: ${configuration.kind}`,
                'metadata: {name: sample, namespace: monitoring}',
                configuration.configuration,
              ].join('\n'),
            },
          }
        : {}),
      ...(family.route.startsWith('/configuration/configmaps/')
        ? {
            [`${root}/configuration/configmaps`]: [
              { ...base, dataEntries: 1, allowedActions: ['view'] },
            ],
          }
        : {}),
    })
    await page.goto(`${family.route}?clusterId=cluster-a&namespace=monitoring`)
    await expect(page.locator('.soha-k8s-detail-header h1')).toHaveText('sample')
    const metadata = page.locator('.soha-k8s-detail-metadata')
    await expect(metadata.getByRole('button', { name: /标签/ })).toHaveAttribute(
      'aria-expanded',
      'false',
    )
    await metadata.getByRole('button', { name: /标签/ }).click()
    await expect(metadata).toContainText('app:')
    if (configuration) await expect.poll(() => reads).toContain(yamlPath)
    const before = reads.filter((path) => path === detailPath).length
    await page
      .locator('.soha-k8s-detail-header')
      .getByRole('button', { name: /^刷新 / })
      .click()
    await expect
      .poll(() => reads.filter((path) => path === detailPath).length)
      .toBeGreaterThan(before)
    expect([...new Set(reads.filter((path) => path.includes('/yaml')))]).toEqual(
      configuration ? [yamlPath] : [],
    )
    await page.setViewportSize({ width: 900, height: 850 })
    expect(await page.evaluate(() => document.documentElement.scrollWidth > innerWidth)).toBe(false)
    verifyBrowserEvidence(evidence)
  })

test('Helm history uses a compact menu and reads manifests only after compare', async ({
  context,
  page,
}) => {
  const history = Array.from({ length: 30 }, (_, index) => ({
    revision: String(30 - index),
    status: 'deployed',
    chart: 'example-1.0',
    allowedActions: ['rollback'],
  }))
  const { reads, evidence } = await setup(
    context,
    page,
    ['platform.helm.view', 'platform.helm.values.view'],
    {
      [`${root}/helm/releases/sample/detail`]: {
        ...base,
        revision: '30',
        status: 'deployed',
        chart: 'example-1.0',
      },
      [`${root}/helm/releases/sample/history`]: history,
      [`${root}/helm/releases/sample/manifest`]: { content: 'kind: Deployment', revision: '30' },
    },
  )
  await page.goto('/helm/releases/sample?namespace=monitoring')
  await expect(page.locator('.soha-k8s-detail-header h1')).toHaveText('sample')
  expect(
    reads.some(
      (path) => path.includes('/history') || path.includes('/manifest') || path.includes('/values'),
    ),
  ).toBe(false)
  await page.getByRole('tab', { name: /Revision 历史|Revision History/ }).click()
  await expect(page.getByRole('button', { name: '更多操作：29', exact: true })).toBeVisible()
  await expect(page.locator('.ant-pagination')).toHaveCount(0)
  expect(reads.some((path) => path.includes('/manifest'))).toBe(false)
  await page.getByRole('button', { name: '更多操作：29', exact: true }).click()
  await page.getByRole('menuitem', { name: '比较 revision 29', exact: true }).click()
  await expect.poll(() => reads.filter((path) => path.includes('/manifest')).length).toBe(2)
  verifyBrowserEvidence(evidence)
})

test('Port Forward keeps a complete scoped collection and confirms the exact registered session', async ({
  context,
  page,
}) => {
  const sessions = Array.from({ length: 80 }, (_, index) => ({
    clusterId: 'cluster-a',
    sessionId: `session-${String(index).padStart(3, '0')}`,
    namespace: 'monitoring',
    targetKind: 'Pod',
    targetName: `pod-${index}`,
    status: 'active',
    localPort: 9000 + index,
    remotePort: 80,
    createdAt: '2026-10-01T00:00:00Z',
  }))
  const { writes, evidence } = await setup(
    context,
    page,
    ['platform.network.port-forwards.view', 'platform.network.port-forwards.delete'],
    {
      [`${root}/network/port-forwards`]: [
        ...sessions,
        { ...sessions[0], sessionId: 'other-session', namespace: 'default' },
      ],
      [`${root}/network/port-forwards/session-000`]: null,
    },
  )
  await page.goto('/network/port-forward')
  await expect(page.getByRole('status').filter({ hasText: '80 个 Port Forwards' })).toBeVisible()
  await expect(page.locator('.ant-pagination')).toHaveCount(0)
  const trigger = page.getByRole('button', { name: '更多操作：session-000', exact: true })
  await trigger.press('ArrowDown')
  await page.getByRole('menuitem', { name: '停止会话', exact: true }).click()
  const dialog = page.getByRole('dialog')
  await expect(dialog).toContainText('将请求后端停止此会话并释放其监听')
  await expect(dialog).toContainText('目标资源不被删除')
  await expect(dialog).toContainText('monitoring · Pod/pod-0')
  await expect(dialog).toContainText('Core 127.0.0.1:9000 → 80')
  await expect(dialog).toContainText('session-000')
  expect(writes).toEqual([])
  await dialog.getByRole('button', { name: /^取\s*消$/ }).click()
  await trigger.click()
  await page.getByRole('menuitem', { name: '停止会话', exact: true }).click()
  await dialog.getByRole('button', { name: '停止会话', exact: true }).click()
  await expect(dialog).toHaveCount(0)
  expect(writes).toEqual([`${root}/network/port-forwards/session-000`])
  verifyBrowserEvidence(evidence)
})

test('Helm Charts keeps server pagination while its actions use the shared menu', async ({
  context,
  page,
}) => {
  const artifactHubUrl = 'https://artifacthub.io/packages/helm/example/sample-chart'
  const chart = {
    artifactHubUrl,
    packageId: 'chart-package',
    name: 'sample-chart',
    repositoryName: 'example',
    latestVersion: '1.0.0',
    description: 'Synthetic chart',
    allowedActions: ['view', 'create'],
  }
  const catalog = (offset: number) => ({
    repository: { id: 'example', name: 'example', url: 'https://charts.example.test' },
    charts: [chart],
    totalCount: 31,
    loadedCount: 1,
    chartCount: 1,
    versionCount: 1,
    refreshedAt: '2026-10-01T00:00:00Z',
    offset,
    limit: 15,
  })
  const { reads, evidence } = await setup(
    context,
    page,
    ['platform.helm.view', 'platform.helm.releases.create'],
    {
      [`${root}/helm/charts?limit=15&offset=0`]: catalog(0),
      [`${root}/helm/charts?limit=15&offset=15`]: catalog(15),
      [`${root}/helm/charts/example/sample-chart?version=1.0.0`]: {
        ...chart,
        readme: '# Synthetic chart',
      },
    },
  )
  let hubNavigations = 0
  await context.route(artifactHubUrl, async (route) => {
    hubNavigations += 1
    await route.fulfill({ contentType: 'text/html', body: '<h1>Synthetic Artifact Hub</h1>' })
  })
  await page.goto('/helm/charts')
  await expect(page.getByRole('status').filter({ hasText: '31 个 Charts' })).toBeVisible()
  await page.locator('.ant-pagination-item-2').click()
  await expect
    .poll(() => reads.some((path) => path.includes('/helm/charts?limit=15&offset=15')))
    .toBe(true)
  const actions = page.getByRole('button', { name: '更多操作：sample-chart', exact: true })
  await actions.press('ArrowDown')
  await expect(page.getByRole('menuitem', { name: '查看详情', exact: true })).toBeFocused()
  await page.keyboard.press('ArrowDown')
  await expect(page.getByRole('link', { name: 'Artifact Hub', exact: true })).toBeFocused()
  const popupOpened = context.waitForEvent('page')
  await page.keyboard.press('Enter')
  const popup = await popupOpened
  await expect(popup).toHaveURL(artifactHubUrl)
  await expect(popup.getByRole('heading', { name: 'Synthetic Artifact Hub' })).toBeVisible()
  expect(hubNavigations).toBe(1)
  await popup.close()
  await expect(
    page.getByRole('menu', { name: 'Helm Chart 操作：sample-chart', exact: true }),
  ).toHaveCount(0)
  await expect(actions).toBeFocused()
  await actions.press('ArrowDown')
  await expect(page.getByRole('menuitem', { name: '安装 Chart', exact: true })).toBeVisible()
  await page.getByRole('menuitem', { name: '查看详情', exact: true }).click()
  await expect(page.getByRole('dialog')).toContainText('Chart: example/sample-chart')
  expect(reads.some((path) => path.includes('/charts/values'))).toBe(false)
  verifyBrowserEvidence(evidence)
})

test('Manifests keeps page-scoped totals and opens complete inventory from the shared menu', async ({
  context,
  page,
}) => {
  const manifest = (index: number) => ({
    id: `manifest-${index}`,
    name: `sample-manifest-${index}`,
    applicationId: 'application-a',
    serviceId: 'service-a',
    renderer: 'raw_yaml',
    status: 'published',
    currentRevision: 1,
    files: [{ path: 'resource.yaml', content: 'kind: ConfigMap' }],
    bindings: [
      {
        id: `binding-${index}`,
        environmentKey: 'test',
        clusterId: 'cluster-a',
        namespace: 'monitoring',
        status: 'ready',
      },
    ],
    createdAt: '2026-10-01T00:00:00Z',
    updatedAt: '2026-10-01T00:00:00Z',
  })
  const deployment = (index: number) => ({
    id: `deployment-${index}`,
    packageId: `manifest-${index}`,
    bindingId: `binding-${index}`,
    spec: { desiredRevision: 1 },
    status: {
      phase: 'ready',
      appliedRevision: 1,
      inventory: Array.from({ length: 25 }, (_, i) => ({
        apiVersion: 'v1',
        kind: 'ConfigMap',
        namespace: 'monitoring',
        name: `config-${i}`,
        health: 'healthy',
      })),
    },
  })
  const { reads, evidence } = await setup(context, page, ['delivery.applications.view'], {
    '/api/v1/delivery/manifest-packages?clusterId=cluster-a&namespace=monitoring&page=1&pageSize=15':
      { items: [manifest(1)], total: 31, page: 1, pageSize: 15 },
    '/api/v1/delivery/manifest-packages?clusterId=cluster-a&namespace=monitoring&page=2&pageSize=15':
      { items: [manifest(2)], total: 31, page: 2, pageSize: 15 },
    '/api/v1/delivery/manifest-deployments?packageId=manifest-1&page=1&pageSize=100': {
      items: [deployment(1)],
      total: 1,
      page: 1,
      pageSize: 100,
    },
    '/api/v1/delivery/manifest-deployments?packageId=manifest-2&page=1&pageSize=100': {
      items: [deployment(2)],
      total: 1,
      page: 1,
      pageSize: 100,
    },
  })
  await page.goto('/manifests')
  await expect(page.getByRole('status').filter({ hasText: '31 份清单' })).toBeVisible()
  await expect(page.locator('main')).toContainText('当前页 1 份清单 / 1 条绑定')
  await page.locator('.ant-pagination-item-2').click()
  await expect(
    page.getByRole('button', { name: '更多操作：sample-manifest-2', exact: true }),
  ).toBeVisible()
  expect(
    reads.some((path) => path.includes('manifest-packages?') && path.includes('page=2&')),
  ).toBe(true)
  await page.getByRole('button', { name: '更多操作：sample-manifest-2', exact: true }).click()
  await page.getByRole('menuitem', { name: '查看 YAML', exact: true }).click()
  const drawer = page.getByRole('dialog')
  await drawer.getByRole('tab', { name: '实际资源 (25)', exact: true }).click()
  const inventory = drawer.getByRole('tabpanel', { name: '实际资源 (25)', exact: true })
  const lastResource = inventory.getByRole('row').filter({ hasText: 'config-24' })
  await expect(lastResource.getByText('config-24', { exact: true })).toBeVisible()
  await expect(lastResource).toContainText('ConfigMap · v1')
  await expect(lastResource).toContainText('monitoring')
  await expect(drawer.locator('.ant-pagination')).toHaveCount(0)
  verifyBrowserEvidence(evidence)
})
