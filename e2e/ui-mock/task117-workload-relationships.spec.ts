import { test, expect, type BrowserContext, type Page } from '@playwright/test'
import type { KubernetesResourceGraph } from '@opensoha/contracts/gen/ts/sohaapi'
import { baselineResponse } from '../fixtures/baseline-data'
import { installMocks, verifyBrowserEvidence } from '../fixtures/mock-app'

const apiRoot = '/api/v1/clusters/cluster-a'
const name = 'relationship-sample'
const namespace = 'monitoring'
const workloads = [
  ['pods', 'Pod', 'platform.pods.view'],
  ['deployments', 'Deployment', 'platform.deployment.view'],
  ['statefulsets', 'StatefulSet', 'platform.workloads.stateful-sets.view'],
  ['daemonsets', 'DaemonSet', 'platform.workloads.daemon-sets.view'],
  ['jobs', 'Job', 'platform.workloads.jobs.view'],
  ['cronjobs', 'CronJob', 'platform.workloads.cron-jobs.view'],
  ['replicasets', 'ReplicaSet', 'platform.workloads.replica-sets.view'],
  [
    'replicationcontrollers',
    'ReplicationController',
    'platform.workloads.replication-controllers.view',
  ],
] as const
type Workload = (typeof workloads)[number]
const deployment = workloads[1]

function graphFor(kind: string): KubernetesResourceGraph {
  const node = (
    id: string,
    resourceKind: string,
    resourceName: string,
  ): KubernetesResourceGraph['nodes'][number] => ({
    id,
    status: 'resolved',
    resource: {
      clusterId: 'cluster-a',
      namespace,
      apiVersion: ['Deployment', 'StatefulSet', 'DaemonSet', 'ReplicaSet'].includes(resourceKind)
        ? 'apps/v1'
        : ['Job', 'CronJob'].includes(resourceKind)
          ? 'batch/v1'
          : resourceKind === 'FutureResource'
            ? 'future.example.test/v2'
            : 'v1',
      scopeMode: 'namespace',
      kind: resourceKind,
      name: resourceName,
      uid: `${id}-uid`,
    },
  })
  return {
    rootId: 'root',
    clusterId: 'cluster-a',
    generatedAt: '2026-11-05T00:00:00Z',
    evidence: [],
    warnings: [],
    nodes: [
      node('root', kind, name),
      node('owner', 'ReplicaSet', 'parent-controller'),
      node('config', 'ConfigMap', 'runtime-config'),
      { ...node('unresolved', 'Secret', 'missing-secret'), status: 'unresolved' },
      node('future', 'FutureResource', 'future-reference'),
    ],
    edges: [
      { id: 'controls', sourceId: 'owner', targetId: 'root', relation: 'controls' },
      { id: 'config-use', sourceId: 'root', targetId: 'config', relation: 'uses-config' },
      { id: 'secret-use', sourceId: 'root', targetId: 'unresolved', relation: 'uses-secret' },
      { id: 'future-use', sourceId: 'root', targetId: 'future', relation: 'future-link-v2' },
    ],
  }
}

async function setup(context: BrowserContext, page: Page, workload: Workload = deployment) {
  const [plural, kind, permission] = workload
  const item = `${apiRoot}/workloads/${plural}/${name}`
  const state = {
    graph: graphFor(kind),
    status: 200,
    graphReads: 0,
    extraPermissions: [] as string[],
  }
  const reads: string[] = []
  const writes: string[] = []
  await context.addInitScript(() => {
    localStorage.setItem(
      'soha-scope',
      JSON.stringify({ state: { clusterId: 'cluster-a', namespace: 'monitoring' }, version: 0 }),
    )
  })
  const evidence = await installMocks(context, page, (url, method) => {
    if (method !== 'GET') {
      // Authentication refresh is the existing fixture's session setup, not a resource write.
      if (url.pathname !== '/api/v1/auth/refresh')
        writes.push(`${method} ${url.pathname}${url.search}`)
      return undefined
    }
    reads.push(url.pathname + url.search)
    if (url.pathname === '/api/v1/access/permission-snapshot') {
      const body = baselineResponse(url.pathname)!
      if (!('permissionKeys' in body.data)) throw new Error('Missing permission fixture')
      body.data.permissionKeys!.push(
        permission,
        'platform.configuration.config-maps.view',
        ...state.extraPermissions,
      )
      return { body }
    }
    if (url.pathname === `${apiRoot}/resources/graph`) {
      // Fail incorrect root/scope requests explicitly; never fall back to the baseline graph.
      if (
        url.searchParams.get('namespace') !== namespace ||
        url.searchParams.get('kind') !== kind ||
        url.searchParams.get('name') !== name
      ) {
        evidence.missing.push(`incorrect graph identity: ${url.search}`)
        return {
          status: 400,
          body: { error: { code: 'incorrect_scope', message: 'Incorrect graph identity' } },
        }
      }
      state.graphReads++
      return state.status === 200
        ? { body: { data: state.graph } }
        : {
            status: state.status,
            body: {
              error: {
                code: state.status === 403 ? 'forbidden' : 'synthetic_failure',
                message: 'Synthetic graph read failure',
              },
            },
          }
    }
    if (url.pathname.startsWith(item + '/') && url.searchParams.get('namespace') === namespace) {
      if (url.pathname === item + '/detail')
        return {
          body: {
            data: {
              name,
              namespace,
              createdAt: '2026-10-01T00:00:00Z',
              labels: { app: name },
              annotations: {},
              phase: 'Running',
              containers: [],
              conditions: [],
              volumes: [],
              allowedActions: ['get', 'view'],
              desiredReplicas: 1,
              readyReplicas: 1,
              currentReplicas: 1,
              updatedReplicas: 1,
              availableReplicas: 1,
              desiredNumber: 1,
              currentNumber: 1,
              readyNumber: 1,
              updatedNumber: 1,
              availableNumber: 1,
              strategy: 'RollingUpdate',
              updateStrategy: 'RollingUpdate',
              selector: { app: name },
              schedule: '*/5 * * * *',
              suspend: false,
              active: 0,
              succeeded: 1,
              failed: 0,
              completions: 1,
              pods: [],
              jobs: [],
              relatedResources: [],
            },
          },
        }
      if (url.pathname === item + '/yaml')
        return {
          body: {
            data: {
              kind,
              name,
              namespace,
              content: `apiVersion: apps/v1\nkind: ${kind}\nmetadata: {name: ${name}, namespace: ${namespace}}\nspec: {schedule: "*/5 * * * *", template: {spec: {containers: []}}, jobTemplate: {spec: {template: {spec: {containers: []}}}}}\n`,
            },
          },
        }
      if (plural === 'deployments' && url.pathname === item + '/rollout-status')
        return {
          body: {
            data: {
              name,
              namespace,
              revision: '1',
              status: 'ready',
              desiredReplicas: 1,
              readyReplicas: 1,
              updatedReplicas: 1,
              availableReplicas: 1,
              conditions: [],
            },
          },
        }
      if (['deployments', 'daemonsets'].includes(plural) && url.pathname === item + '/rollouts')
        return { body: { data: [] } }
      if (plural === 'pods' && url.pathname === item + '/metrics')
        return {
          body: {
            data: {
              configured: false,
              resourceKind: 'Pod',
              resourceName: name,
              rangeMinutes: 60,
              stepSeconds: 60,
              generatedAt: '2026-11-05T00:00:00Z',
              series: [],
            },
          },
        }
    }
    return undefined
  })
  return {
    state,
    reads,
    writes,
    evidence,
    route: `/workloads/${plural}/${name}?clusterId=cluster-a&namespace=${namespace}`,
  }
}

const panel = (page: Page) => page.getByRole('tabpanel', { name: '关联资源', exact: true })
const inspector = (page: Page) => panel(page).getByRole('complementary', { name: '选中资源信息' })
async function list(page: Page) {
  await panel(page)
    .getByRole('radiogroup', { name: '关联资源视图' })
    .getByText('列表', { exact: true })
    .click()
  await expect(panel(page).getByRole('radio', { name: '列表', exact: true })).toBeChecked()
}
async function open(page: Page, route: string) {
  await page.goto(`${route}&tab=related-resources`)
  await expect(panel(page).locator('.react-flow__node[data-id="root"]')).toBeVisible()
}

for (const workload of workloads) {
  test(`${workload[1]} relationships use the detail root, lazy graph and scoped read-only requests`, async ({
    context,
    page,
  }) => {
    const fixture = await setup(context, page, workload)
    await page.goto(fixture.route)
    await expect(page.getByRole('heading', { name, exact: true })).toBeVisible()
    expect(fixture.state.graphReads).toBe(0)
    await page.getByRole('tab', { name: '关联资源', exact: true }).click()
    await expect(page).toHaveURL(/tab=related-resources/)
    await expect(panel(page).locator('.react-flow__node[data-id="root"]')).toBeVisible()
    expect(fixture.state.graphReads).toBe(1)
    await list(page)
    const rootRow = panel(page)
      .getByRole('row')
      .filter({ has: page.getByRole('button', { name: `${name}（当前）`, exact: true }) })
    await expect(rootRow).toContainText(workload[1])
    await expect(rootRow).toContainText('受控于 ReplicaSet parent-controller')
    await rootRow.getByRole('button').click()
    await expect(inspector(page)).toContainText(namespace)
    await expect(inspector(page)).toContainText(workload[1])
    const before = fixture.state.graphReads
    await page.getByRole('tab', { name: '概览', exact: true }).click()
    await expect(page.getByRole('tabpanel', { name: '概览', exact: true })).toBeVisible()
    await page
      .locator('.soha-workload-detail-header')
      .getByRole('button', {
        name: workload[1] === 'Pod' ? '刷新' : `刷新 ${workload[1]}`,
        exact: true,
      })
      .click()
    await expect
      .poll(() => fixture.reads.filter((path) => path.includes('/detail?')).length)
      .toBeGreaterThan(1)
    expect(fixture.state.graphReads).toBe(before)
    expect(fixture.writes).toEqual([])
    verifyBrowserEvidence(fixture.evidence)
  })
}

test('graph selection, direction, unknown tokens and list refresh preserve selected identity', async ({
  context,
  page,
}) => {
  const fixture = await setup(context, page)
  await open(page, fixture.route)
  await panel(page).locator('.react-flow__node[data-id="root"]').click()
  await expect(inspector(page)).toBeFocused()
  await expect(inspector(page)).toContainText('受控于')
  await expect(inspector(page)).toContainText('引用配置')
  await expect(inspector(page)).toContainText('future-link-v2')
  await inspector(page)
    .getByRole('button', { name: 'ConfigMap runtime-config', exact: true })
    .click()
  await expect(inspector(page)).toContainText('被引用为配置')
  await expect(inspector(page).getByRole('button', { name: '查看详情', exact: true })).toBeEnabled()
  await page.keyboard.press('Escape')
  await expect(inspector(page)).toHaveCount(0)

  await expect(panel(page).locator('.react-flow__node[data-id="config"]')).toBeFocused()
  await list(page)
  const configButton = panel(page).getByRole('button', { name: 'runtime-config', exact: true })
  await configButton.click()
  await expect(inspector(page)).toContainText('runtime-config')
  await panel(page)
    .getByRole('radiogroup', { name: '关联资源视图' })
    .getByText('关系图', { exact: true })
    .click()
  await expect(inspector(page)).toContainText('runtime-config')
  const before = fixture.state.graphReads
  await panel(page).getByRole('button', { name: '刷新资源关系', exact: true }).click()
  await expect.poll(() => fixture.state.graphReads).toBeGreaterThan(before)
  await expect(inspector(page)).toContainText('runtime-config')
  await list(page)
  await expect(configButton).toHaveAttribute('aria-pressed', 'true')
  await inspector(page).getByRole('button', { name: '关闭资源信息', exact: true }).click()
  await expect(inspector(page)).toHaveCount(0)
  await expect(configButton).toBeFocused()
  await panel(page).getByRole('button', { name: 'parent-controller', exact: true }).click()
  await expect(inspector(page)).toContainText('控制')
  await expect(
    inspector(page).getByRole('button', { name: '查看详情', exact: true }),
  ).toBeDisabled()
  await page.keyboard.press('Escape')
  await panel(page).getByRole('button', { name: 'missing-secret', exact: true }).click()
  await expect(inspector(page)).toContainText('未解析：仅有引用声明')
  await expect(
    inspector(page).getByRole('button', { name: '查看详情', exact: true }),
  ).toBeDisabled()
  await page.keyboard.press('Escape')
  await panel(page).getByRole('button', { name: 'future-reference', exact: true }).click()
  await expect(inspector(page)).toContainText('future.example.test/v2')
  await expect(
    inspector(page).getByRole('button', { name: '查看详情', exact: true }),
  ).toBeDisabled()
  expect(fixture.writes).toEqual([])
  verifyBrowserEvidence(fixture.evidence)
})

test('custom group resources named Pod or Service cannot use built-in detail routes', async ({
  context,
  page,
}) => {
  const fixture = await setup(context, page)
  fixture.state.extraPermissions.push('platform.pods.view', 'platform.network.services.view')
  const template = fixture.state.graph.nodes[2]
  fixture.state.graph.nodes.push(
    ...['Pod', 'Service'].map((kind) => ({
      ...template,
      id: `custom-${kind}`,
      resource: {
        ...template.resource,
        kind,
        name: `custom-${kind.toLowerCase()}`,
        apiVersion: 'custom.example.test/v1',
        uid: `custom-${kind}-uid`,
      },
    })),
  )
  fixture.state.graph.edges.push(
    ...['Pod', 'Service'].map((kind) => ({
      id: `custom-edge-${kind}`,
      sourceId: 'root',
      targetId: `custom-${kind}`,
      relation: 'future-link-v2',
    })),
  )
  await page.goto(`${fixture.route}&tab=related-resources`)
  await list(page)
  for (const kind of ['Pod', 'Service']) {
    await panel(page)
      .getByRole('button', { name: `custom-${kind.toLowerCase()}`, exact: true })
      .click()
    await expect(inspector(page)).toContainText(kind)
    await expect(inspector(page)).toContainText('custom.example.test/v1')
    await expect(
      inspector(page).getByRole('button', { name: '查看详情', exact: true }),
    ).toBeDisabled()
    await expect(inspector(page)).toContainText('暂无详情入口')
    await page.keyboard.press('Escape')
  }
  await panel(page).getByRole('button', { name: 'runtime-config', exact: true }).click()
  await expect(inspector(page).getByRole('button', { name: '查看详情', exact: true })).toBeEnabled()
  expect(fixture.writes).toEqual([])
  verifyBrowserEvidence(fixture.evidence)
})

test('known edge tokens use source and target wording without reversing the server graph', async ({
  context,
  page,
}) => {
  const fixture = await setup(context, page)
  const relations = [
    ['controls', '控制', '受控于'],
    ['owns', '拥有', '归属于'],
    ['scheduled-on', '调度到', '承载'],
    ['uses-config', '引用配置', '被引用为配置'],
    ['uses-secret', '引用密钥', '被引用为密钥'],
    ['mounts', '声明引用卷', '被声明引用为卷'],
    ['uses-service-account', '使用服务账号', '服务账号用于'],
    ['selects', '选择器匹配', '被选择器匹配'],
    ['routes-to', '声明路由后端', '被声明为路由后端'],
    ['scales', '伸缩', '被伸缩于'],
    ['protects', '保护', '受保护于'],
    ['future-link-v2', 'future-link-v2', 'future-link-v2'],
  ] as const
  const root = fixture.state.graph.nodes[0]
  const target = fixture.state.graph.nodes[2]
  fixture.state.graph = {
    ...fixture.state.graph,
    nodes: [
      root,
      ...relations.map(([token]) => ({
        ...target,
        id: token,
        resource: { ...target.resource, name: `target-${token}` },
      })),
    ],
    edges: relations.map(([token]) => ({
      id: `edge-${token}`,
      sourceId: 'root',
      targetId: token,
      relation: token,
    })),
  }
  await page.goto(`${fixture.route}&tab=related-resources`)
  await list(page)
  const sourceRow = panel(page)
    .getByRole('row')
    .filter({ has: page.getByRole('button', { name: `${name}（当前）`, exact: true }) })
  for (const [token, outgoing, incoming] of relations) {
    await expect(sourceRow).toContainText(`${outgoing} ConfigMap target-${token}`)
    const targetRow = panel(page)
      .getByRole('row')
      .filter({ has: page.getByRole('button', { name: `target-${token}`, exact: true }) })
    await expect(targetRow).toContainText(`${incoming} Deployment ${name}`)
    await targetRow.getByRole('button').click()
    await expect(inspector(page).locator('li')).toContainText(`${incoming} Deployment ${name}`)
    await page.keyboard.press('Escape')
  }
  expect(fixture.writes).toEqual([])
  verifyBrowserEvidence(fixture.evidence)
})

for (const status of [403, 500]) {
  test(`graph refresh ${status}: ${status === 403 ? 'hides cached resources' : 'keeps the last graph with retry feedback'}`, async ({
    context,
    page,
  }) => {
    const fixture = await setup(context, page)
    await open(page, fixture.route)
    await list(page)
    await panel(page).getByRole('button', { name: 'runtime-config', exact: true }).click()
    fixture.state.status = status
    await panel(page).getByRole('button', { name: '刷新资源关系', exact: true }).click()
    await expect(panel(page).getByRole('alert')).toContainText(
      status === 403 ? '资源关系无访问权限' : '资源关系未更新，正在展示上次读取结果',
      { timeout: 15_000 },
    )
    if (status === 403) {
      await expect(inspector(page)).toHaveCount(0)
      await expect(
        panel(page).getByRole('button', { name: 'runtime-config', exact: true }),
      ).toHaveCount(0)
      await expect(panel(page)).not.toContainText('runtime-config')
    } else {
      await expect(inspector(page)).toContainText('runtime-config')
      fixture.state.status = 200
      await panel(page).getByRole('button', { name: '重试读取资源关系', exact: true }).click()
      await expect(panel(page).getByRole('alert')).toHaveCount(0)
      await expect(inspector(page)).toContainText('runtime-config')
    }
    expect(fixture.writes).toEqual([])
    verifyBrowserEvidence(fixture.evidence)
  })
}

test('root mismatch and refreshed deletion clear stale graph selection', async ({
  context,
  page,
}) => {
  const fixture = await setup(context, page)
  fixture.state.graph.nodes[0].resource.namespace = 'another-namespace'
  await page.goto(`${fixture.route}&tab=relationships`)
  await expect(panel(page)).toContainText('资源关系数据未返回')
  await expect(panel(page).locator('.react-flow__node')).toHaveCount(0)
  fixture.state.graph = graphFor('Deployment')
  await panel(page).getByRole('button', { name: '刷新资源关系', exact: true }).click()
  await expect(panel(page).locator('.react-flow__node[data-id="root"]')).toBeVisible()
  await list(page)
  await panel(page).getByRole('button', { name: 'runtime-config', exact: true }).click()
  await expect(inspector(page)).toContainText('runtime-config')
  fixture.state.graph = {
    ...fixture.state.graph,
    nodes: fixture.state.graph.nodes.filter((node) => node.id !== 'config'),
    edges: fixture.state.graph.edges.filter((edge) => edge.targetId !== 'config'),
  }
  await panel(page).getByRole('button', { name: '刷新资源关系', exact: true }).click()
  await expect(inspector(page)).toHaveCount(0)
  await expect(
    panel(page).getByRole('button', { name: 'runtime-config', exact: true }),
  ).toHaveCount(0)
  expect(fixture.writes).toEqual([])
  verifyBrowserEvidence(fixture.evidence)
})

for (const dark of [false, true]) {
  test(`long relationship values fit narrow ${dark ? 'dark' : 'light'} viewports`, async ({
    context,
    page,
  }) => {
    const fixture = await setup(context, page)
    const longName = 'config-' + 'long-reference-'.repeat(14)
    fixture.state.graph.nodes[2].resource.name = longName
    fixture.state.graph.nodes[2].resource.apiVersion =
      'example.test/' + 'long-api-version-'.repeat(10)
    await page.setViewportSize({ width: 900, height: 850 })
    await open(page, fixture.route)
    if (dark) await page.getByLabel('切换到深色模式').click()
    await list(page)
    await panel(page).getByRole('button', { name: longName, exact: true }).click()
    await expect(inspector(page)).toContainText(longName)
    for (const width of [900, 320]) {
      await page.setViewportSize({ width, height: 850 })
      await expect(
        inspector(page).getByRole('button', { name: '关闭资源信息', exact: true }),
      ).toBeInViewport()
      expect(await page.evaluate(() => document.documentElement.scrollWidth > innerWidth)).toBe(
        false,
      )
      const bounds = await inspector(page).boundingBox()
      expect(bounds).not.toBeNull()
      expect(bounds!.x).toBeGreaterThanOrEqual(0)
      expect(bounds!.x + bounds!.width).toBeLessThanOrEqual(width + 1)
    }
    await inspector(page).getByRole('button', { name: '关闭资源信息', exact: true }).click()
    await expect(inspector(page)).toHaveCount(0)
    expect(fixture.writes).toEqual([])
    verifyBrowserEvidence(fixture.evidence)
  })
}
