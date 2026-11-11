import { test, expect, type BrowserContext, type Page } from '@playwright/test'
import { installMocks, verifyBrowserEvidence } from '../fixtures/mock-app'
import { verifyGeometry } from '../fixtures/geometry'
import type {
  NetworkTopologyView,
  TopologyNode,
} from '../../src/features/platform/network/topology/types'

const now = '2026-10-19T08:00:00Z'
const ref = (kind: string, name: string) => ({
  clusterId: 'cluster-a',
  apiVersion: 'v1',
  kind,
  name,
  namespace: 'monitoring',
  uid: `${name}-uid`,
  resourceVersion: '42',
  scopeMode: 'namespace' as const,
})
const topology = {
  clusterId: 'cluster-a',
  namespace: 'monitoring',
  graphVersion: 'v1',
  source: 'kubernetes-snapshot',
  generatedAt: now,
  observedAt: now,
  summary: {
    entryCount: 1,
    routeCount: 1,
    serviceCount: 1,
    backendPodCount: 1,
    pendingRouteCount: 0,
    missingServiceCount: 0,
  },
  traces: [],
  nodes: [
    {
      id: 'route',
      type: 'entry',
      label: 'traefik-shop-entry',
      provider: 'traefik',
      protocol: 'HTTP',
      resolutionState: 'resolved',
      resource: { ...ref('IngressRoute', 'traefik-shop-entry'), apiVersion: 'traefik.io/v1alpha1' },
    },
    {
      id: 'rule',
      type: 'rule',
      label: 'Shop rule',
      provider: 'traefik',
      protocol: 'HTTP',
      resolutionState: 'resolved',
      rule: {
        index: 0,
        priority: 10,
        matchGroups: [
          [
            { type: 'host', value: 'shop.example' },
            { type: 'path', value: '/orders', operator: 'Prefix' },
          ],
          [{ type: 'host', value: 'backup.example' }],
        ],
      },
    },
    {
      id: 'service',
      type: 'service',
      label: 'shop-service',
      provider: 'kubernetes',
      resolutionState: 'resolved',
      resource: ref('Service', 'shop-service'),
    },
    {
      id: 'pod',
      type: 'pod',
      label: 'shop-worker-with-a-long-but-valid-resource-name-01',
      provider: 'kubernetes',
      resolutionState: 'resolved',
      resource: ref('Pod', 'shop-worker-with-a-long-but-valid-resource-name-01'),
    },
    {
      id: 'missing',
      type: 'backend-group',
      label: 'Unresolved reference',
      provider: 'apisix',
      protocol: 'UDP',
      resolutionState: 'not_visible',
    },
  ],
  edges: [
    {
      id: 'match',
      source: 'route',
      target: 'rule',
      relation: 'matches',
      protocol: 'HTTP',
      resolutionState: 'resolved',
    },
    {
      id: 'weight',
      source: 'rule',
      target: 'service',
      relation: 'weights',
      protocol: 'HTTP',
      port: '80',
      weight: 0,
      totalWeight: 10,
      resolutionState: 'resolved',
    },
    {
      id: 'mirror',
      source: 'rule',
      target: 'service',
      relation: 'mirrors',
      mirrorPercent: 15,
      resolutionState: 'resolved',
    },
    {
      id: 'pod-edge',
      source: 'service',
      target: 'pod',
      relation: 'resolves',
      resolutionState: 'resolved',
    },
  ],
  coverage: [
    {
      sourceId: 'traefik.ingressroutes',
      provider: 'traefik',
      namespace: 'monitoring',
      state: 'available',
      observedAt: now,
    },
    {
      sourceId: 'apisix.apisixroutes',
      provider: 'apisix',
      namespace: 'monitoring',
      state: 'denied',
      reasonCode: 'agent_action_denied',
      checkedAt: now,
    },
  ],
} satisfies NetworkTopologyView

function monitoring(clusterId: string, view: string, missing: boolean) {
  const nodeRef = {
    clusterId,
    apiVersion: 'v1',
    kind: 'Node',
    name: `${clusterId}-worker`,
    uid: `${clusterId}-uid`,
    scopeMode: 'cluster',
  }
  const node = {
    nodeRef,
    os: 'linux',
    ready: 'True',
    cordoned: false,
    capacity: { cpuCores: 4, memoryBytes: 8589934592, podSlots: 110 },
    allocatable: { cpuCores: 3.5, memoryBytes: 7516192768, podSlots: 110 },
    requests: { cpuCores: 5, memoryBytes: 1073741824 },
    limits: { cpuCores: null, memoryBytes: null },
    cpuLimitsComplete: false,
    memoryLimitsComplete: false,
    boundPods: 112,
    pendingPods: 3,
    remainingPodSlots: 0,
    overflowPodSlots: 2,
    memoryPressure: 'False',
    diskPressure: 'False',
    pidPressure: 'False',
    quality: 'valid',
    observedAt: now,
  }
  const keys = [
    'host.cpu.used',
    'pod.cpu.used',
    'host.memory.used',
    'host.memory.total',
    'pod.memory.used',
    'conntrack.entries',
    'conntrack.limit',
    'conntrack.drop.rate',
    'pods.bound',
  ]
  return {
    clusterId,
    view,
    from: '2026-10-19T07:00:00Z',
    to: now,
    stepSeconds: 60,
    generatedAt: now,
    snapshotQuality: 'valid',
    snapshotObservedAt: now,
    unscheduledPendingPods: 2,
    nodeOptions: [nodeRef],
    nodes: [node],
    panels: keys.map((key) => ({
      key,
      label: key,
      unit: key.includes('memory') ? 'bytes' : key.includes('cpu') ? 'cores' : 'count',
      quality: missing ? 'not_configured' : 'valid',
      reasonCode: missing ? 'prometheus_not_configured' : undefined,
      series: missing
        ? []
        : [
            {
              nodeRef,
              metricKey: key,
              unit: 'cores',
              quality: 'partial',
              source: 'prometheus',
              latest: 0,
              sampledAt: now,
              points: [
                { timestamp: '2026-10-19T07:58:00Z', value: 1 },
                { timestamp: '2026-10-19T07:59:00Z', value: null },
                { timestamp: now, value: 0 },
              ],
            },
          ],
    })),
  }
}

async function setup(
  context: BrowserContext,
  page: Page,
  options: {
    dark?: boolean
    english?: boolean
    missing?: boolean
    denied?: boolean
    views?: string[]
    updates?: boolean
    nodeStates?: boolean
    resourceEdit?: boolean
    flatNetwork?: boolean
    nodeCount?: number
    continuous?: boolean
    shifted?: boolean
    topology?: NetworkTopologyView | ((url: URL) => NetworkTopologyView)
    drainFailure?: 'pdb' | 'unavailable' | 'state-denied'
  } = {},
) {
  await context.addInitScript(({ dark, english }) => {
    localStorage.setItem(
      'soha-scope',
      JSON.stringify({ state: { clusterId: 'cluster-a', namespace: 'monitoring' }, version: 0 }),
    )
    localStorage.setItem(
      'soha-prefs',
      JSON.stringify({
        state: {
          localeCode: english ? 'en_US' : 'zh_CN',
          themeMode: dark ? 'dark' : 'light',
          sidebarCollapsed: true,
          currentWorkspace: 'resource',
          companionBubbleEnabled: false,
        },
        version: 0,
      }),
    )
    localStorage.setItem('soha-resource-monitoring-auto', '0')
  }, options)
  const permissions = [
    'workbench.platform.view',
    'workspace.resource.view',
    'platform.clusters.view',
    'platform.nodes.view',
    ...(options.drainFailure ? ['platform.nodes.drain', 'platform.nodes.update'] : []),
    'platform.namespaces.view',
    'platform.network.topology.view',
    'platform.network.services.view',
    ...(options.resourceEdit ? ['platform.network.services.update'] : []),
    'platform.pods.view',
    'platform.extensions.view',
    ...(options.views ?? ['overview', 'cpu', 'memory', 'conntrack', 'pods']).map(
      (view) => `platform.resource-monitoring.${view}.view`,
    ),
  ]
  const monitoringReads = new Map<string, number>()
  const evidence = await installMocks(context, page, (url, method) => {
    if (url.pathname === '/api/v1/access/permission-snapshot')
      return {
        body: {
          data: {
            permissionKeys: permissions,
            visibleMenuIds: [
              'clusters',
              'network',
              'network-topology',
              'network-backends',
              'network-services',
              'resource-monitoring',
              ...['overview', 'cpu', 'memory', 'conntrack', 'pods'].map(
                (view) => `resource-monitoring-${view}`,
              ),
            ],
            visibleMenus: options.flatNetwork
              ? [
                  { id: 'network', path: '/network', labelZh: '网络' },
                  {
                    id: 'network-topology',
                    parentId: 'network',
                    path: '/network/topology',
                    labelZh: '网络拓扑',
                  },
                  {
                    id: 'network-services',
                    parentId: 'network',
                    path: '/network/services',
                    labelZh: 'Services',
                  },
                ]
              : [],
          },
        },
      }
    if (url.pathname === '/api/v1/auth/stream-ticket' && method === 'POST')
      return {
        status: 403,
        body: { error: { code: 'access_denied', message: 'Stream unavailable in this fixture' } },
      }
    if (url.pathname === '/api/v1/clusters')
      return {
        body: {
          data: ['cluster-a', 'cluster-b'].map((id) => ({
            id,
            name: id,
            region: 'standard_kubernetes',
            environment: 'test',
            connectionMode: 'agent',
            labels: {},
            health: { status: 'healthy' },
          })),
        },
      }
    if (url.pathname.endsWith('/namespaces'))
      return {
        body: {
          data: ['monitoring', 'apps'].map((name) => ({ name, status: 'Active', labels: {} })),
        },
      }
    if (url.pathname.endsWith('/network/topology'))
      return {
        body: {
          data:
            typeof options.topology === 'function'
              ? options.topology(url)
              : (options.topology ?? topology),
        },
      }
    if (url.pathname.endsWith('/network/services/shop-service/detail'))
      return {
        body: {
          data: {
            name: 'shop-service',
            namespace: 'monitoring',
            type: 'ClusterIP',
            clusterIp: '10.0.0.10',
            ports: ['80/TCP'],
            labels: {},
            ageSeconds: 3600,
            allowedActions: options.resourceEdit ? ['view', 'update'] : ['view'],
          },
        },
      }
    if (url.pathname.endsWith('/network/services/shop-service/yaml'))
      return {
        body: {
          data: {
            content:
              'apiVersion: v1\nkind: Service\nmetadata:\n  name: shop-service\n  namespace: monitoring\n',
          },
        },
      }
    if (
      options.drainFailure &&
      url.pathname.endsWith('/infrastructure/nodes/cordoned-worker/drain')
    ) {
      return {
        status: options.drainFailure === 'unavailable' ? 503 : 409,
        body: {
          error: {
            code:
              options.drainFailure === 'unavailable'
                ? 'service_unavailable'
                : 'node_drain_pdb_blocked',
            message:
              options.drainFailure === 'unavailable'
                ? 'Drain response unavailable'
                : 'PodDisruptionBudget 阻止了驱逐，排水未完成。',
            request_id: 'drain-request-1',
          },
        },
      }
    }
    if (
      options.drainFailure &&
      url.pathname.endsWith('/infrastructure/nodes/cordoned-worker/detail')
    ) {
      if (options.drainFailure === 'state-denied')
        return {
          status: 403,
          body: { error: { code: 'access_denied', message: 'Node state denied' } },
        }
      return {
        body: {
          data: {
            name: 'cordoned-worker',
            status: 'ready',
            unschedulable: true,
            podCount: 2,
            roles: ['worker'],
            labels: {},
            taints: [],
            conditions: [],
            pods: [],
            allowedActions: ['view', 'drain', 'update'],
          },
        },
      }
    }
    if (options.nodeStates && url.pathname.endsWith('/infrastructure/nodes')) {
      const base = {
        status: 'ready',
        unschedulable: false,
        roles: ['worker'],
        internalIp: '10.0.0.2',
        podCount: 2,
        ageSeconds: 3600,
        version: 'v1.36.3+k3s1',
        allowedActions: ['view', 'update', 'drain', 'delete'],
      }
      return {
        body: {
          data: [
            {
              ...base,
              name: 'cordoned-worker',
              unschedulable: true,
              taints: [
                { key: 'maintenance', value: 'task113', effect: 'NoSchedule' },
                { key: 'prefer', value: 'batch', effect: 'PreferNoSchedule' },
                { key: 'node.kubernetes.io/unreachable', effect: 'NoExecute' },
              ],
              conditions: [
                { type: 'Ready', status: 'True' },
                { type: 'DiskPressure', status: 'True' },
              ],
            },
            {
              ...base,
              name: 'task113-production-healthy-worker-with-a-long-resource-name-09',
              taints: [],
              conditions: [{ type: 'Ready', status: 'True' }],
            },
            { ...base, name: 'legacy-worker', status: 'unknown' },
          ],
        },
      }
    }
    if (url.pathname.endsWith('/resource-monitoring')) {
      expect(url.searchParams.has('namespace')).toBe(false)
      if (options.denied)
        return {
          status: 403,
          body: { error: { code: 'access_denied', message: 'Monitoring access denied' } },
        }
      const view = url.searchParams.get('view') || 'overview'
      const count = (monitoringReads.get(view) || 0) + 1
      monitoringReads.set(view, count)
      const data = monitoring(url.pathname.split('/')[4], view, Boolean(options.missing))
      if (options.nodeCount) {
        const original = data.nodes[0]
        data.nodes = Array.from({ length: options.nodeCount }, (_, index) => ({
          ...original,
          nodeRef: { ...original.nodeRef, name: `worker-${index + 1}`, uid: `uid-${index + 1}` },
        }))
        data.nodeOptions = data.nodes.map((node) => node.nodeRef)
        data.panels = data.panels.map((panel) => ({
          ...panel,
          series: data.nodes.map((node, index) => {
            const values: Record<string, number> = {
              'host.cpu.used': index + 1,
              'pod.cpu.used': (index + 1) / 10,
              'host.memory.used': (index + 1) * 100 * 1024 ** 2,
              'host.memory.total': node.capacity.memoryBytes,
              'pod.memory.used': (index + 1) * 64 * 1024 ** 2,
              'conntrack.entries': (index + 1) * 100,
              'conntrack.limit': 262144,
              'conntrack.drop.rate': 0,
              'pods.bound': node.boundPods,
            }
            const value = values[panel.key]!
            return {
              ...panel.series[0],
              nodeRef: node.nodeRef,
              latest: value,
              points: [0.6, 0.8, null, 0.9, 0.75, 1].map((scale, sample) => ({
                timestamp: new Date(Date.parse(now) - (5 - sample) * 60000).toISOString(),
                value:
                  scale == null
                    ? null
                    : panel.key === 'host.memory.total' || panel.key === 'conntrack.limit'
                      ? value
                      : value * scale,
              })),
            }
          }),
        }))
      }
      if (url.searchParams.get('node')) {
        data.nodes = data.nodes.filter((node) => node.nodeRef.name === url.searchParams.get('node'))
        data.panels = data.panels.map((panel) => ({
          ...panel,
          series: panel.series.filter((series) =>
            data.nodes.some((node) => node.nodeRef.uid === series.nodeRef.uid),
          ),
        }))
      }
      if (options.continuous && view === 'cpu') {
        data.panels.find((panel) => panel.key === 'host.cpu.used')!.series[0].points[1].value = 1
      }
      if (options.updates && view === 'cpu' && count > 1) {
        const sampledAt = options.shifted ? '2026-10-19T08:00:30Z' : '2026-10-19T08:01:00Z'
        const series = data.panels.find((panel) => panel.key === 'host.cpu.used')!.series[0]
        series.latest = 2
        series.sampledAt = sampledAt
        if (options.shifted) {
          series.points = series.points.map((point, index) => ({
            timestamp: new Date(Date.parse(point.timestamp) + 30_000).toISOString(),
            value: index === series.points.length - 1 ? 2 : point.value,
          }))
        } else {
          series.points.push({ timestamp: sampledAt, value: 2 })
        }
      }
      return { body: { data } }
    }
  })
  return evidence
}

test('topology preserves zero weights, rule evidence, filtering and keyboard table access', async ({
  context,
  page,
}) => {
  const evidence = await setup(context, page)
  await page.goto('/network/topology?view=table')
  const content = page.locator('.soha-topology-page')
  await expect(content).toContainText('权重 0/10')
  await expect(content).toContainText('镜像 15%')
  await expect(content.getByRole('switch', { name: '展开 Pod 节点', exact: true })).toHaveCount(0)
  const search = content.getByRole('textbox', { name: '搜索已加载节点' })
  for (const keyword of ['SHOP.EXAMPLE', '/orders', 'backup.example']) {
    await search.fill(keyword)
    await expect(content).toContainText('Shop rule')
    await expect(content).toContainText('shop-service')
    await expect(content).not.toContainText('Unresolved reference')
  }
  await search.fill('')
  await content.getByRole('button', { name: 'Shop rule', exact: true }).first().click()
  await expect(page.getByText('shop.example', { exact: true })).toBeVisible()
  await expect(page.getByText('backup.example', { exact: true })).toBeVisible()
  await page.getByRole('button', { name: '聚焦上下游', exact: true }).click()
  await expect(page).toHaveURL(/focus=rule/)
  await content.getByRole('button', { name: /数据覆盖/ }).click()
  await expect(page.getByText('agent_action_denied', { exact: true })).toBeVisible()
  await page.keyboard.press('Escape')
  await content.getByRole('textbox', { name: '搜索已加载节点' }).fill('no-such-resource')
  await expect(content).toContainText('没有匹配的已加载资源')
  verifyBrowserEvidence(evidence)
})

test('topology preserves focus when a background snapshot is incomplete', async ({
  context,
  page,
}) => {
  let incomplete = false
  const evidence = await setup(context, page, {
    english: true,
    topology: () =>
      incomplete
        ? {
            ...topology,
            truncated: true,
            nodes: topology.nodes.filter((node) => node.id !== 'service'),
            edges: [],
          }
        : topology,
  })
  await page.goto('/network/topology?view=table&focus=service')
  const content = page.locator('.soha-topology-page')
  await expect(content).toContainText('Weight 0/10')
  await expect(page).toHaveURL(/focus=service/)
  incomplete = true
  await page.getByRole('button', { name: 'Refresh network topology', exact: true }).click()
  await expect(content).toContainText('Data budget reached')
  await expect(page).toHaveURL(/focus=service/)
  await expect(content).toContainText('The selected node is no longer in this snapshot')
  verifyBrowserEvidence(evidence)
})

for (const scope of ['cluster', 'namespace'] as const) {
  test(`topology clears stale focus after a ${scope} change and preserves other filters`, async ({
    context,
    page,
  }) => {
    const evidence = await setup(context, page, {
      english: true,
      topology: (url) => {
        const clusterId = url.pathname.split('/')[4]
        const namespace = url.searchParams.get('namespace') || ''
        const prefix = `${clusterId}|${namespace}|`
        return {
          ...topology,
          clusterId,
          namespace,
          nodes: topology.nodes.map((node: TopologyNode) => ({
            ...node,
            id: prefix + node.id,
            resource: node.resource ? { ...node.resource, clusterId, namespace } : undefined,
          })),
          edges: topology.edges.map((edge) => ({
            ...edge,
            id: prefix + edge.id,
            source: prefix + edge.source,
            target: prefix + edge.target,
          })),
        }
      },
    })
    const focus = 'cluster-a|monitoring|service'
    await page.goto(`/network/topology?view=table&protocol=HTTP&focus=${encodeURIComponent(focus)}`)
    const content = page.locator('.soha-topology-page')
    await expect(content).toContainText('Weight 0/10')
    await expect(page).toHaveURL(/focus=/)
    await expect(content).toContainText('Focused path')
    const selector = page.getByRole('combobox', {
      name: scope === 'cluster' ? 'Select cluster' : 'Select namespace',
      exact: true,
    })
    await selector.click()
    await page
      .locator('.ant-select-item-option-content')
      .filter({ hasText: scope === 'cluster' ? /^cluster-b$/ : /^apps$/ })
      .click()
    await expect
      .poll(() =>
        evidence.requests.some((request) =>
          scope === 'cluster'
            ? request.includes('/clusters/cluster-b/network/topology')
            : request.includes('/network/topology?namespace=apps'),
        ),
      )
      .toBe(true)
    await expect(page).not.toHaveURL(/focus=/)
    await expect(page).toHaveURL(/view=table/)
    await expect(page).toHaveURL(/protocol=HTTP/)
    await expect(content).toContainText('Weight 0/10')
    await expect(content).not.toContainText('Focused path')
    await expect(content).not.toContainText('No loaded nodes match these filters')
    verifyBrowserEvidence(evidence)
  })
}

test('topology unassociated resources stay collapsed and exclude nodes during continuous relationship browsing', async ({
  context,
  page,
}) => {
  const evidence = await setup(context, page, {
    topology: {
      ...topology,
      edges: [
        ...topology.edges,
        ...Array.from({ length: 21 }, (_, index) => ({
          ...topology.edges[3],
          id: `extra-pod-edge-${index}`,
        })),
      ],
    },
  })
  await page.goto('/network/topology?view=table')
  const isolated = page.locator('details.soha-topology-isolated')
  const summary = isolated.locator('summary')
  await expect(page.getByRole('heading', { name: '网络拓扑', exact: true })).toBeVisible()
  await expect(summary).toHaveText('未关联资源（1）', { timeout: 2000 })
  await expect(isolated).toHaveJSProperty('open', false)
  await expect(isolated.locator('button')).toHaveCount(1)
  await expect(
    isolated.getByRole('button', { name: 'Unresolved reference', exact: true }),
  ).not.toBeVisible()
  await expect(page.getByText('节点入口（含独立节点）：', { exact: true })).toHaveCount(0)
  await expect(page.locator('.soha-topology-page .ant-pagination')).toHaveCount(0)
  await page.locator('.soha-topology-page .ant-table-tbody-virtual-holder').evaluate((node) => {
    node.scrollTop = node.scrollHeight
    node.dispatchEvent(new Event('scroll', { bubbles: true }))
  })
  await expect(
    page.locator('.soha-topology-page .ant-table-row[data-row-key="extra-pod-edge-20"]'),
  ).toBeVisible()
  await expect(summary).toHaveText('未关联资源（1）')
  await summary.focus()
  await summary.press('Enter')
  await expect(isolated).toHaveJSProperty('open', true)
  await isolated.getByRole('button', { name: 'Unresolved reference', exact: true }).click()
  await expect(page.locator('.soha-topology-evidence')).toContainText('Unresolved reference')
  verifyBrowserEvidence(evidence)
})

test('topology unassociated resources disappear when every node has a relationship', async ({
  context,
  page,
}) => {
  const evidence = await setup(context, page, {
    topology: { ...topology, nodes: topology.nodes.filter((node) => node.id !== 'missing') },
  })
  await page.goto('/network/topology?view=table')
  await expect(page.locator('.soha-topology-page')).toContainText('权重 0/10')
  await expect(page.locator('.soha-topology-isolated')).toHaveCount(0)
  verifyBrowserEvidence(evidence)
})

test('topology unassociated resources remain reachable when there are no relationships', async ({
  context,
  page,
}) => {
  const evidence = await setup(context, page, { topology: { ...topology, edges: [] } })
  await page.goto('/network/topology?view=table')
  const isolated = page.locator('details.soha-topology-isolated')
  await expect(isolated.locator('summary')).toHaveText('未关联资源（5）')
  await expect(isolated).toHaveJSProperty('open', false)
  await isolated.locator('summary').click()
  await expect(isolated.locator('button')).toHaveCount(5)
  await isolated.getByRole('button', { name: 'shop-service', exact: true }).click()
  await expect(page.locator('.soha-topology-evidence')).toContainText('shop-service')
  verifyBrowserEvidence(evidence)
})

test('topology unassociated resources wrap long names in a narrow dark view', async ({
  context,
  page,
}, info) => {
  await page.setViewportSize({ width: 320, height: 900 })
  const longName = `unassociated-${'long-resource-name-'.repeat(10)}worker`
  const evidence = await setup(context, page, {
    dark: true,
    english: true,
    topology: { ...topology, nodes: [{ ...topology.nodes[4], label: longName }], edges: [] },
  })
  await page.goto('/network/topology?view=table')
  const isolated = page.locator('details.soha-topology-isolated')
  await expect(isolated.locator('summary')).toHaveText('Unassociated resources (1)')
  await expect(isolated).toHaveJSProperty('open', false)
  await isolated.locator('summary').click()
  const resource = isolated.getByRole('button', { name: longName, exact: true })
  await resource.scrollIntoViewIfNeeded()
  await verifyGeometry(page, resource)
  await page.screenshot({ path: info.outputPath('unassociated-dark-narrow.png'), fullPage: true })
  verifyBrowserEvidence(evidence)
})

test('topology exposes Gateway TCP TLS UDP evidence and keeps protocol filtering precise', async ({
  context,
  page,
}) => {
  const protocols = ['TCP', 'TLS', 'UDP']
  const data: NetworkTopologyView = {
    ...topology,
    nodes: protocols.flatMap((protocol) => [
      {
        id: protocol,
        type: 'entry',
        label: `gateway-${protocol}-route`,
        provider: 'gateway',
        protocol,
        resolutionState: 'resolved',
        resource: {
          ...ref(`${protocol}Route`, `gateway-${protocol}-route`),
          apiVersion: 'gateway.networking.k8s.io/v1',
        },
      },
      {
        id: `${protocol}-rule`,
        type: 'rule',
        label: `${protocol} rule`,
        provider: 'gateway',
        protocol,
        resolutionState: 'resolved',
        rule: {
          index: 0,
          ...(protocol === 'TLS'
            ? {
                tlsMode: 'Passthrough',
                matchGroups: [[{ type: 'sni', value: 'secure.example.test' }]],
              }
            : {}),
        },
      },
      { ...topology.nodes[2], id: `${protocol}-service`, label: `${protocol}-backend` },
    ]),
    edges: protocols.flatMap((protocol) => [
      {
        id: `${protocol}-match`,
        source: protocol,
        target: `${protocol}-rule`,
        relation: 'matches',
        protocol,
        port: '9000',
        resolutionState: 'resolved',
      },
      {
        id: `${protocol}-weight`,
        source: `${protocol}-rule`,
        target: `${protocol}-service`,
        relation: 'weights',
        protocol,
        port: protocol === 'TLS' ? '9443' : '9000',
        weight: 0,
        totalWeight: 3,
        resolutionState: 'resolved',
      },
    ]),
  }
  const evidence = await setup(context, page, { topology: data })
  await page.goto('/network/topology?view=table')
  for (const protocol of protocols)
    await expect(
      page.getByRole('button', { name: `gateway-${protocol}-route`, exact: true }),
    ).toBeVisible()
  await page.getByRole('button', { name: 'gateway-TLS-route', exact: true }).click()
  await expect(
    page
      .locator('.soha-topology-evidence')
      .getByRole('link', { name: '进入此 API 组的 CRD 工作区' }),
  ).toHaveAttribute('href', '/extensions/apis/gateway.networking.k8s.io')
  await page.getByRole('button', { name: '查看关联资源 TLS rule', exact: true }).click()
  await expect(page.locator('.soha-topology-evidence')).toContainText('secure.example.test')
  await expect(page.locator('.soha-topology-evidence')).toContainText('Passthrough')
  await expect(page.locator('.soha-topology-evidence')).toContainText('权重 0/3')
  await page.getByRole('combobox', { name: '协议', exact: true }).click()
  await page.locator('.ant-select-dropdown').getByText('TLS', { exact: true }).click()
  await expect(page.getByRole('button', { name: 'gateway-TLS-route', exact: true })).toBeVisible()
  await expect(page.getByRole('button', { name: 'gateway-TCP-route', exact: true })).toHaveCount(0)
  await page.getByText('关系图', { exact: true }).click()
  const canvas = page.locator('.soha-topology-canvas')
  await expect(canvas.locator('.soha-topology-node-entry')).toHaveText('SNI · secure.example.test')
  await page.getByRole('combobox', { name: '协议', exact: true }).click()
  await page.locator('.ant-select-dropdown').getByText('TCP', { exact: true }).click()
  await expect(canvas.locator('.soha-topology-node-entry')).toHaveText('监听端口 · :9000')
  await expect(canvas).not.toContainText('secure.example.test')
  verifyBrowserEvidence(evidence)
})

test('graph entry cards expose long configured hostnames in dark mode and keep keyboard details', async ({
  context,
  page,
}, info) => {
  const host = `${'long-domain-label.'.repeat(5)}example.test`
  const evidence = await setup(context, page, {
    dark: true,
    topology: {
      ...topology,
      nodes: topology.nodes.map((node) =>
        node.id === 'rule'
          ? { ...node, rule: { index: 0, matchGroups: [[{ type: 'host', value: host }]] } }
          : node,
      ),
    },
  })
  await page.goto('/network/topology?view=graph&provider=traefik')
  const card = page.locator('.soha-topology-node-card').filter({ hasText: 'traefik-shop-entry' })
  await expect(card.locator('.soha-topology-node-entry')).toHaveAttribute('title', `域名 · ${host}`)
  await verifyGeometry(page, card)
  await card.press('Enter')
  await expect(page.locator('.soha-topology-evidence')).toContainText('traefik-shop-entry')
  await page.screenshot({ path: info.outputPath('entry-hostname-dark.png'), fullPage: true })
  verifyBrowserEvidence(evidence)
})

for (const mode of ['pdb', 'unavailable', 'state-denied'] as const) {
  test(`node drain failure ${mode} keeps observed state and never replays a write`, async ({
    context,
    page,
  }) => {
    const evidence = await setup(context, page, { nodeStates: true, drainFailure: mode })
    const writes: string[] = []
    page.on('request', (request) => {
      if (request.method() !== 'GET' && /\/infrastructure\/nodes\//.test(request.url()))
        writes.push(request.url())
    })
    await page.goto('/cluster-resources/nodes')
    await page.getByRole('button', { name: '更多操作：cordoned-worker', exact: true }).click()
    await page.getByRole('menuitem', { name: /排空节点/ }).click()
    const dialog = page.getByRole('dialog')
    await dialog.getByRole('spinbutton', { name: '排水超时（秒）' }).fill('30')
    await dialog.getByRole('button', { name: '确认排空', exact: true }).click()
    await expect(dialog).toContainText('drain-request-1')
    if (mode === 'state-denied') {
      await expect(dialog).toContainText('排水后的节点状态无访问权限')
      await expect(dialog).not.toContainText('当前绑定 Pod：')
    } else {
      await expect(dialog).toContainText('失败后的节点观测')
      await expect(dialog).toContainText('禁止调度')
      await expect(dialog).toContainText('当前绑定 Pod：2')
      await expect(dialog.getByRole('link', { name: '查看节点与剩余 Pod' })).toHaveAttribute(
        'href',
        '/cluster-resources/nodes/cordoned-worker?clusterId=cluster-a',
      )
    }
    if (mode === 'unavailable')
      await expect(dialog.getByRole('button', { name: /^(?:loading )?确认排空$/ })).toBeDisabled()
    else await expect(dialog).toContainText('PodDisruptionBudget 阻止了驱逐，排水未完成。')
    expect(writes).toHaveLength(1)
    expect(new URL(writes[0]).pathname).toMatch(/\/drain$/)
    await expect(
      page
        .locator('.soha-operation-feedback')
        .filter({ hasText: 'PodDisruptionBudget 阻止了驱逐，排水未完成。' }),
    ).toHaveCount(mode === 'unavailable' ? 0 : 1)
    verifyBrowserEvidence(evidence)
  })
}

test('monitoring keeps gaps and zero samples, overcommit and unlimited values distinct', async ({
  context,
  page,
}) => {
  const evidence = await setup(context, page, { english: true })
  await page.goto('/resource-monitoring/cpu?clusterId=cluster-a')
  const details = page.getByRole('tabpanel')
  await expect(page.getByRole('tab', { name: 'CPU', exact: true })).toHaveAttribute(
    'aria-selected',
    'true',
  )
  await expect(details).toContainText('cluster-a-worker')
  await expect(details).toContainText('142.9%')
  await expect(details).toContainText('Unlimited resources / incomplete coverage')
  await expect(
    details.getByRole('img', { name: /^host\.cpu\.used: cluster-a-worker\. Exact values/ }),
  ).toBeVisible()
  await details.getByRole('button', { name: 'Samples', exact: true }).first().click()
  const samples = page.getByRole('dialog', { name: 'host.cpu.used · Samples', exact: true })
  await expect(samples).toContainText('no zero fill')
  await expect(samples.getByText('0 mCPU', { exact: true })).toBeVisible()
  await expect(samples.getByText('—', { exact: true })).toBeVisible()
  await page.keyboard.press('Escape')
  await expect(samples).toBeHidden()
  await expect(page).toHaveURL(/resource-monitoring\/cpu\?clusterId=cluster-a$/)
  await page.getByRole('tab', { name: 'Pod capacity', exact: true }).click()
  await expect(page).toHaveURL(/clusterId=cluster-a#monitoring-pods$/)
  const podRow = details.locator('.ant-table-row').filter({ hasText: 'cluster-a-worker' })
  await expect(podRow).toBeVisible()
  await expect(podRow).toContainText('112 / 110')
  await expect(podRow).toContainText('101.8% · Overcommitted')
  await expect(podRow.getByText('0 / 2', { exact: true })).toBeVisible()
  verifyBrowserEvidence(evidence)
})

test('monitoring combines compact scope trends and a node comparison table', async ({
  context,
  page,
}) => {
  const evidence = await setup(context, page)
  await page.goto('/resource-monitoring?clusterId=cluster-a&rangeMinutes=15')
  const active = page.getByRole('tabpanel')
  const overview = active.getByRole('region', { name: '当前范围资源汇总' })
  const row = active.locator('.ant-table-row').filter({ hasText: 'cluster-a-worker' })
  await expect(row).toBeVisible()
  await expect(row.locator('.soha-monitoring-usage')).toHaveCount(4)
  const charts = overview.getByRole('img', { name: /范围合计趋势/ })
  await expect(charts).toHaveCount(4)
  for (const chart of await charts.all()) expect((await chart.boundingBox())!.height).toBe(40)
  expect((await row.boundingBox())!.height).toBeLessThan(210)
  expect((await row.boundingBox())!.width).toBeGreaterThan(800)
  await expect(page.getByRole('dialog')).toHaveCount(0)
  const actions = row.getByRole('button', { name: '更多操作：cluster-a-worker', exact: true })
  await actions.press('Enter')
  await page.getByRole('menuitem', { name: '聚焦 连接跟踪趋势', exact: true }).press('Enter')
  await expect(page).toHaveURL(/rangeMinutes=15#monitoring-conntrack$/)
  await expect(
    page.getByRole('menu', { name: 'Node 操作：cluster-a-worker', exact: true }),
  ).toHaveCount(0)
  await expect(page.getByRole('tab', { name: /连接跟踪$/ })).toBeFocused()
  await page.getByRole('tab', { name: /总览$/ }).click()
  await expect(page.getByRole('tabpanel', { name: /总览$/ })).toBeVisible()
  await actions.press('Enter')
  await expect(page.getByRole('menuitem', { name: '聚焦 CPU趋势', exact: true })).toBeFocused()
  await page.keyboard.press('Escape')
  await expect(
    page.getByRole('menu', { name: 'Node 操作：cluster-a-worker', exact: true }),
  ).toHaveCount(0)
  await expect(actions).toBeFocused()
  await actions.press('ArrowDown')
  await expect(page.getByRole('menuitem', { name: '聚焦 CPU趋势', exact: true })).toBeFocused()
  await page.keyboard.press('Tab')
  await expect(
    page.getByRole('menu', { name: 'Node 操作：cluster-a-worker', exact: true }),
  ).toHaveCount(0)
  await expect(actions).not.toBeFocused()
  await actions.press('Enter')
  await expect(page.getByRole('menuitem', { name: '聚焦 CPU趋势', exact: true })).toBeFocused()
  await page.keyboard.press('ArrowDown')
  await page.keyboard.press('ArrowDown')
  await expect(page.getByRole('menuitem', { name: '聚焦 连接跟踪趋势', exact: true })).toBeFocused()
  await page.keyboard.press('Enter')
  await expect(
    page.getByRole('menu', { name: 'Node 操作：cluster-a-worker', exact: true }),
  ).toHaveCount(0)
  await expect(page.getByRole('tab', { name: /连接跟踪$/ })).toBeFocused()
  await expect(page).toHaveURL(/rangeMinutes=15#monitoring-conntrack$/)
  await expect(
    active.getByRole('img', { name: /^conntrack\.entries: cluster-a-worker\./ }),
  ).toBeVisible()
  await expect(active.getByRole('columnheader', { name: 'drop /s', exact: true })).toBeVisible()
  await expect(page.getByRole('dialog')).toHaveCount(0)
  await page.getByRole('tab', { name: /总览$/ }).click()
  await expect(page).toHaveURL(/rangeMinutes=15#monitoring-overview$/)
  verifyBrowserEvidence(evidence)
})

test('node list distinguishes Ready, cordon, pressure, taints and unavailable fields', async ({
  context,
  page,
}) => {
  const evidence = await setup(context, page, { nodeStates: true })
  await page.goto('/cluster-resources/nodes')
  const row = page.locator('.ant-table-row[data-row-key]').filter({ hasText: 'cordoned-worker' })
  await expect(row).toContainText('就绪')
  await expect(row).toContainText('禁止调度')
  await expect(row).toContainText('磁盘压力')
  await expect(row).not.toContainText('排水中')
  const taints = row.getByRole('button', { name: '查看节点污点', exact: true })
  const tags = taints.locator('.ant-tag')
  await expect(tags).toHaveText(['maintenance=task113', '+2'])
  await expect(tags.first()).toHaveClass(/\bant-tag-purple\b/)
  await taints.press('Enter')
  const details = page.locator('.soha-node-taint-details')
  await expect(details).toBeVisible()
  await expect(details.locator('code')).toHaveText([
    'maintenance=task113:NoSchedule',
    'prefer=batch:PreferNoSchedule',
    'node.kubernetes.io/unreachable:NoExecute',
  ])
  await expect(
    page
      .locator('.ant-table-row[data-row-key]')
      .filter({ hasText: 'task113-production-healthy-worker-with-a-long-resource-name-09' }),
  ).toContainText('无污点')
  await expect(
    page.locator('.ant-table-row[data-row-key]').filter({ hasText: 'legacy-worker' }),
  ).toContainText('未提供')
  verifyBrowserEvidence(evidence)
})

test('single monitoring page only requests and renders permitted detail sections', async ({
  context,
  page,
}) => {
  const evidence = await setup(context, page, { english: true, views: ['cpu'] })
  await page.goto('/resource-monitoring?clusterId=cluster-a&rangeMinutes=15#monitoring-memory')
  const tab = page.getByRole('tab', { name: 'CPU', exact: true })
  await expect(tab).toHaveAttribute('aria-selected', 'true')
  await expect(
    page.getByRole('tab', { name: /^(Overview|Memory|Conntrack|Pod capacity)$/ }),
  ).toHaveCount(0)
  const content = page.getByRole('tabpanel')
  await expect(content.getByRole('region', { name: 'CPU', exact: true })).toContainText(
    'cluster-a-worker',
  )
  await content.getByRole('button', { name: 'More actions: cluster-a-worker', exact: true }).click()
  await expect(page.getByRole('menuitem', { name: 'Focus CPU trend', exact: true })).toBeVisible()
  await expect(page.getByRole('menuitem')).toHaveCount(1)
  const requests = evidence.requests.filter((request) => request.includes('/resource-monitoring?'))
  expect(requests.length).toBeGreaterThan(0)
  expect(
    requests.every(
      (request) => new URL(request, 'http://localhost').searchParams.get('view') === 'cpu',
    ),
  ).toBe(true)
  await expect(page).toHaveURL(/rangeMinutes=15/)
  verifyBrowserEvidence(evidence)
})

test('historical CPU keeps observed usage separate from current capacity', async ({
  context,
  page,
}) => {
  const evidence = await setup(context, page, { english: true })
  await page.goto(
    '/resource-monitoring/cpu?clusterId=cluster-a&from=2026-10-19T07%3A00%3A00Z&to=2026-10-19T08%3A00%3A00Z',
  )
  const content = page.locator('.soha-monitoring-page')
  const active = page.getByRole('tabpanel')
  await expect(active).toContainText('0 mCPU')
  await expect(active).toContainText(
    'Historical percentage unavailable · Current capacity: 4.00 cores',
  )
  await expect(active).toContainText('Current allocatable: 3.50 cores')
  await expect(
    content.getByRole('switch', { name: /Historical window; auto-refresh paused/ }),
  ).toBeDisabled()
  await page.getByRole('tab', { name: 'Overview', exact: true }).click()
  await expect(active.getByText('Historical capacity unavailable', { exact: true })).toBeVisible()
  await expect(
    active.locator('.ant-table-row .soha-monitoring-usage').first().getByRole('progressbar'),
  ).toHaveCount(0)
  await page.getByRole('tab', { name: 'Pod capacity', exact: true }).click()
  await expect(active).toContainText('Requests, limits and Pod slots below are current snapshots')
  await expect(
    active.getByRole('columnheader', { name: 'Bound Pending Pods', exact: true }),
  ).toBeVisible()
  await expect(
    active.getByRole('columnheader', { name: 'Remaining / overflow slots', exact: true }),
  ).toBeVisible()
  await expect(active).toContainText('Unbound Pending Pods in this cluster: 2')
  verifyBrowserEvidence(evidence)
})

test('monitoring updates observed charts in place and stops polling when paused', async ({
  context,
  page,
}) => {
  await page.clock.install()
  await page.emulateMedia({ reducedMotion: 'reduce' })
  const evidence = await setup(context, page, { english: true, views: ['cpu'], updates: true })
  await page.goto('/resource-monitoring/cpu?clusterId=cluster-a')
  const active = page.getByRole('tabpanel')
  const host = active.locator('.ant-table-row').getByText('0 mCPU / 4.00 cores', { exact: false })
  await expect(host).toBeVisible()
  const chart = active.getByRole('img', { name: /^host\.cpu\.used: cluster-a-worker\./ })
  const canvas = await chart.locator('canvas').first().elementHandle()
  expect(canvas).not.toBeNull()
  const refresh = page.getByRole('switch', { name: /^Auto-refresh/ })
  await expect(refresh).toHaveAttribute('aria-checked', 'false')
  await refresh.click()
  await page.clock.fastForward(30_000)
  const row = active.locator('.ant-table-row')
  await expect(row).toContainText('2.00 cores / 4.00 cores')
  expect(await canvas!.evaluate((element) => element.isConnected)).toBe(true)
  await expect(row).toContainText('50.0%')
  await refresh.click()
  await expect(refresh).toHaveAttribute('aria-checked', 'false')
  const reads = evidence.requests.filter((request) =>
    request.includes('/resource-monitoring?'),
  ).length
  await page.clock.fastForward(60_000)
  expect(
    evidence.requests.filter((request) => request.includes('/resource-monitoring?')),
  ).toHaveLength(reads)
  verifyBrowserEvidence(evidence)
})

test('live sampling keeps the plot visible throughout its update transition', async ({
  context,
  page,
}) => {
  await page.emulateMedia({ reducedMotion: 'no-preference' })
  const evidence = await setup(context, page, {
    english: true,
    views: ['overview', 'cpu'],
    updates: true,
    continuous: true,
    shifted: true,
  })
  await page.goto('/resource-monitoring?clusterId=cluster-a')
  const card = page
    .getByRole('region', { name: 'Resource totals in this scope' })
    .locator('.soha-monitoring-summary-card')
    .filter({ has: page.getByRole('link', { name: 'CPU', exact: true }) })
  await expect(card).toContainText('0 mCPU')
  const canvas = await card.locator('canvas').first().elementHandle()
  expect(canvas).not.toBeNull()
  const coloredPixels = () =>
    canvas!.evaluate((element) => {
      if (!(element instanceof HTMLCanvasElement)) throw new Error('Expected canvas')
      const data = element.getContext('2d')!.getImageData(0, 0, element.width, element.height).data
      let colored = 0
      for (let i = 0; i < data.length; i += 16)
        if (
          data[i + 3] &&
          Math.max(data[i], data[i + 1], data[i + 2]) -
            Math.min(data[i], data[i + 1], data[i + 2]) >
            10
        )
          colored++
      return colored
    })
  await expect.poll(coloredPixels).toBeGreaterThan(0)
  const frames = canvas!.evaluate((element) => {
    if (!(element instanceof HTMLCanvasElement)) throw new Error('Expected canvas')
    return new Promise<number[]>((resolve) => {
      const samples: number[] = []
      const capture = () => {
        const data = element
          .getContext('2d')!
          .getImageData(0, 0, element.width, element.height).data
        let colored = 0
        for (let i = 0; i < data.length; i += 16)
          if (
            data[i + 3] &&
            Math.max(data[i], data[i + 1], data[i + 2]) -
              Math.min(data[i], data[i + 1], data[i + 2]) >
              10
          )
            colored++
        samples.push(colored)
        if (samples.length === 40) resolve(samples)
        else requestAnimationFrame(capture)
      }
      requestAnimationFrame(capture)
    })
  })
  await page.getByRole('button', { name: 'Refresh resource monitoring', exact: true }).click()
  await expect(card).toContainText('2.00 cores')
  const frameCounts = await frames
  expect(frameCounts.every((count) => count > 0)).toBe(true)
  expect(new Set(frameCounts).size).toBeGreaterThan(2)
  expect(await canvas!.evaluate((element) => element.isConnected)).toBe(true)
  verifyBrowserEvidence(evidence)
})

for (const dark of [false, true]) {
  test(`monitoring keeps the full compact node table and limits chart comparison in ${dark ? 'dark' : 'light'} mode`, async ({
    context,
    page,
  }, info) => {
    const evidence = await setup(context, page, { english: true, nodeCount: 12, dark })
    await page.goto('/resource-monitoring?clusterId=cluster-a')
    await expect(page.locator('html')).toHaveAttribute('data-theme-mode', dark ? 'dark' : 'light')
    const content = page.getByRole('tabpanel')
    const rows = content.locator('.ant-table-row')
    const summary = content.getByRole('region', { name: 'Resource totals in this scope' })
    await expect(summary).toContainText('78.00 cores')
    await expect(content.getByRole('status').filter({ hasText: '12 Nodes' })).toBeVisible()
    await expect(rows.first().locator('.soha-monitoring-usage')).toHaveCount(4)
    for (const metric of ['CPU', 'Memory']) {
      const canvas = summary
        .getByRole('img', { name: new RegExp(`^${metric} scope total trend`) })
        .locator('canvas')
        .first()
      await expect
        .poll(() =>
          canvas.evaluate((element) => {
            if (!(element instanceof HTMLCanvasElement)) throw new Error('Expected canvas')
            const pixels = element
              .getContext('2d')!
              .getImageData(0, 0, element.width, element.height).data
            let colored = 0
            for (let offset = 0; offset < pixels.length; offset += 16)
              if (
                pixels[offset + 3] &&
                Math.max(pixels[offset], pixels[offset + 1], pixels[offset + 2]) -
                  Math.min(pixels[offset], pixels[offset + 1], pixels[offset + 2]) >
                  10
              )
                colored++
            return colored
          }),
        )
        .toBeGreaterThan(0)
    }
    await page.setViewportSize({ width: 1440, height: 1000 })
    const boxes = await rows.evaluateAll((elements) =>
      elements.map((element) => {
        const rect = element.getBoundingClientRect()
        return { x: rect.x, width: rect.width, height: rect.height }
      }),
    )
    expect(
      boxes.every(
        (box) => box.x === boxes[0].x && box.width === boxes[0].width && box.height < 210,
      ),
    ).toBe(true)
    await page.screenshot({
      path: info.outputPath(`monitoring-node-table-${dark ? 'dark' : 'light'}.png`),
      fullPage: true,
    })
    await page.setViewportSize({ width: 320, height: 1000 })
    expect(
      await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth),
    ).toBe(true)
    await page.setViewportSize({ width: 1440, height: 1000 })
    await page.getByRole('tab', { name: 'CPU', exact: true }).click()
    await expect(content.getByRole('combobox', { name: 'Chart nodes', exact: true })).toBeVisible()
    await expect(content).toContainText(
      'Compare up to 10 chart nodes; the table shows the full loaded scope',
    )
    await expect(
      content.getByRole('img', { name: /^host\.cpu\.used: worker-1, .*worker-10\. Exact values/ }),
    ).toBeVisible()
    await expect(content.getByRole('status').filter({ hasText: '12 Nodes' })).toBeVisible()
    await content.getByRole('searchbox', { name: 'Search monitored nodes' }).fill('worker-12')
    await expect(rows).toHaveCount(1)
    await expect(rows.first()).toContainText('12.00 cores / 4.00 cores')
    await rows.first().getByRole('button', { name: 'More actions: worker-12', exact: true }).click()
    await page.getByRole('menuitem', { name: 'Focus CPU trend', exact: true }).click()
    await expect(page).toHaveURL(/clusterId=cluster-a#monitoring-cpu$/)
    const chart = content.getByRole('img', { name: /^host\.cpu\.used: worker-12\./ })
    await expect(chart).toBeVisible()
    await content
      .locator('.soha-monitoring-trends > .ant-card')
      .filter({ has: page.getByRole('img', { name: /^host\.cpu\.used: worker-12\./ }) })
      .getByRole('button', { name: 'Samples', exact: true })
      .click()
    const samples = page.getByRole('dialog', { name: 'host.cpu.used · Samples', exact: true })
    await expect(samples).toContainText('worker-12')
    await expect(samples).not.toContainText('worker-10')
    await expect(samples.getByText('12.00 cores', { exact: true }).first()).toBeVisible()
    verifyBrowserEvidence(evidence)
  })
}

test('topology graph preserves user zoom across an API refresh', async ({
  context,
  page,
}, info) => {
  const evidence = await setup(context, page)
  await page.goto('/network/topology?view=graph')
  await expect(page.locator('.soha-topology-node-card').first()).toBeVisible()
  await expect(page.getByRole('switch', { name: '展开 Pod 节点', exact: true })).toBeVisible()
  await page.getByRole('button', { name: 'Zoom In', exact: true }).click()
  const viewport = page.locator('.react-flow__viewport')
  const transform = await viewport.getAttribute('style')
  const previous = evidence.requests.length
  await page.getByRole('button', { name: '刷新网络拓扑', exact: true }).click()
  await expect.poll(() => evidence.requests.length).toBeGreaterThan(previous)
  await expect(viewport).toHaveAttribute('style', transform!)
  await page.screenshot({ path: info.outputPath('topology-graph.png'), fullPage: true })
  verifyBrowserEvidence(evidence)
})

test('monitoring cluster changes clear node selection and previous cluster data', async ({
  context,
  page,
}) => {
  const evidence = await setup(context, page, { english: true })
  await page.goto('/resource-monitoring/cpu?clusterId=cluster-a&node=cluster-a-worker')
  const active = page.getByRole('tabpanel')
  await expect(
    active.getByRole('img', { name: /^host\.cpu\.used: cluster-a-worker\./ }),
  ).toBeVisible()
  const cluster = page.getByRole('combobox', { name: 'Select cluster', exact: true })
  await cluster.click()
  await cluster.press('ArrowDown')
  await cluster.press('Enter')
  await expect(page).toHaveURL(/clusterId=cluster-b/)
  await expect(page).not.toHaveURL(/node=/)
  await expect(
    active.getByRole('img', { name: /^host\.cpu\.used: cluster-b-worker\./ }),
  ).toBeVisible()
  await expect(page.locator('.soha-monitoring-page')).not.toContainText('cluster-a-worker')
  expect(
    evidence.requests.some((request) => {
      const url = new URL(request, 'http://localhost')
      return (
        url.pathname === '/api/v1/clusters/cluster-b/resource-monitoring' &&
        !url.searchParams.has('node')
      )
    }),
  ).toBe(true)
  verifyBrowserEvidence(evidence)
})

test('monitoring and topology reflow at an effective 200 percent desktop zoom', async ({
  context,
  page,
}) => {
  await page.setViewportSize({ width: 720, height: 500 })
  const evidence = await setup(context, page)
  await page.goto('/resource-monitoring')
  await expect(page.locator('.soha-monitoring-page')).toContainText('cluster-a-worker')
  await verifyGeometry(page, page.getByRole('button', { name: '来源与覆盖', exact: true }))
  await page.goto('/network/topology')
  await expect(page.locator('.soha-topology-page')).toContainText('权重 0/10')
  await verifyGeometry(page, page.getByRole('textbox', { name: '搜索已加载节点' }))
  verifyBrowserEvidence(evidence)
})

test('missing metrics retain current capacity without empty chart grids', async ({
  context,
  page,
}) => {
  const evidence = await setup(context, page, { missing: true })
  await page.goto('/resource-monitoring/memory')
  const content = page.locator('.soha-monitoring-page')
  const active = page.getByRole('tabpanel')
  await expect(active.getByRole('alert')).toContainText('当前没有可展示的历史样本')
  await expect(active.getByRole('link', { name: 'cluster-a-worker', exact: true })).toBeVisible()
  await expect(active.locator('.ant-table-row')).toContainText('1.0 GB')
  await expect(active.locator('.soha-monitoring-chart')).toHaveCount(0)
  await content.getByRole('button', { name: '来源与覆盖', exact: true }).click()
  await expect(page.getByRole('dialog')).toContainText('未配置数据源')
  verifyBrowserEvidence(evidence)
})

test('monitoring denial does not reveal cached capacity', async ({ context, page }) => {
  const options = { english: true, denied: false }
  const evidence = await setup(context, page, options)
  await page.goto('/resource-monitoring/cpu')
  const active = page.getByRole('tabpanel')
  await expect(active.getByRole('link', { name: 'cluster-a-worker', exact: true })).toBeVisible()
  options.denied = true
  await page.getByRole('button', { name: 'Refresh resource monitoring', exact: true }).click()
  for (const label of ['CPU', 'Overview', 'Memory', 'Conntrack', 'Pod capacity']) {
    await page.getByRole('tab', { name: label, exact: true }).click()
    await expect(active.getByText('Monitoring access denied', { exact: false })).toBeVisible()
    await expect(active.getByRole('link', { name: 'cluster-a-worker', exact: true })).toHaveCount(0)
    await expect(
      active.locator('.soha-monitoring-chart, .soha-monitoring-summary-card'),
    ).toHaveCount(0)
  }
  await expect(
    page.getByRole('button', { name: 'Sources and coverage', exact: true }),
  ).toBeDisabled()
  verifyBrowserEvidence(evidence)
})

for (const width of [320, 768, 1024, 1440]) {
  test(`topology and monitoring remain usable at ${width}px`, async ({ context, page }, info) => {
    await page.setViewportSize({ width, height: 1000 })
    await page.emulateMedia({ reducedMotion: 'reduce' })
    const evidence = await setup(context, page, { dark: width === 768 || width === 1440 })
    await page.goto('/network/topology?view=table')
    await expect(page.locator('.soha-topology-page')).toContainText('权重 0/10')
    await verifyGeometry(page, page.getByRole('textbox', { name: '搜索已加载节点' }))
    await page.screenshot({ path: info.outputPath(`topology-${width}.png`), fullPage: true })
    await page.goto('/resource-monitoring')
    await expect(page.locator('.soha-monitoring-page')).toContainText('cluster-a-worker')
    await verifyGeometry(page, page.getByRole('button', { name: '来源与覆盖', exact: true }))
    await page.screenshot({ path: info.outputPath(`monitoring-${width}.png`), fullPage: true })
    verifyBrowserEvidence(evidence)
  })
}

for (const width of [320, 1238, 1275]) {
  test(`node table keeps compact widths and inline taints at ${width}px`, async ({
    context,
    page,
  }, info) => {
    await page.setViewportSize({ width, height: 1000 })
    const evidence = await setup(context, page, { nodeStates: true, dark: width === 320 })
    await page.goto('/cluster-resources/nodes')
    const content = page.locator('.soha-cluster-nodes-page')
    const cordonedNode = content.getByRole('link', { name: 'cordoned-worker', exact: true })
    await expect(cordonedNode).toBeVisible()
    await expect(cordonedNode).toHaveAttribute(
      'href',
      '/cluster-resources/nodes/cordoned-worker?clusterId=cluster-a&tab=overview',
    )
    expect(
      (await content.getByRole('columnheader', { name: '时长', exact: true }).boundingBox())!.width,
    ).toBeLessThan(90)
    await verifyGeometry(page, content.getByRole('link', { name: 'cordoned-worker', exact: true }))
    if (width >= 1238) {
      const body = content.getByRole('region', { name: '资源列表，可连续滚动', exact: true })
      await body.hover()
      await page.mouse.wheel(1500, 0)
      const name = content.getByRole('link', { name: 'cordoned-worker', exact: true })
      await expect(name).toBeInViewport()
      const longName = content.getByRole('link', {
        name: 'task113-production-healthy-worker-with-a-long-resource-name-09',
        exact: true,
      })
      expect(await longName.evaluate((element) => getComputedStyle(element).whiteSpace)).toBe(
        'nowrap',
      )
      expect(await longName.evaluate((element) => getComputedStyle(element).textOverflow)).toBe(
        'ellipsis',
      )
      expect(
        await longName.evaluate((element) => element.scrollWidth - element.clientWidth),
      ).toBeGreaterThan(1)
      const assertExpandAlignment = async () => {
        const deltas = await content.locator('.ant-table-row-expand-icon').evaluateAll((buttons) =>
          buttons.map((button) => {
            const icon = button.getBoundingClientRect()
            const cell = button.closest('.ant-table-cell')!.getBoundingClientRect()
            return {
              x: Math.abs(icon.x + icon.width / 2 - cell.x - cell.width / 2),
              y: Math.abs(icon.y + icon.height / 2 - cell.y - cell.height / 2),
              width: cell.width,
            }
          }),
        )
        for (const delta of deltas) {
          expect(delta.width).toBe(48)
          expect(delta.x).toBeLessThanOrEqual(1)
          expect(delta.y).toBeLessThanOrEqual(1)
        }
      }
      await assertExpandAlignment()
      await longName.hover()
      await expect(
        page.getByRole('tooltip', {
          name: 'task113-production-healthy-worker-with-a-long-resource-name-09',
          exact: true,
        }),
      ).toBeVisible()
      await page.mouse.move(0, 0)
      const healthy = content
        .locator('.ant-table-row[data-row-key]')
        .filter({ hasText: 'task113-production-healthy-worker-with-a-long-resource-name-09' })
      await healthy.getByRole('button', { name: '展开行', exact: true }).press('Tab')
      await expect(longName).toBeFocused()
      await expect(
        page.getByRole('tooltip', {
          name: 'task113-production-healthy-worker-with-a-long-resource-name-09',
          exact: true,
        }),
      ).toBeVisible()
      await healthy.getByRole('button', { name: '展开行', exact: true }).press('Enter')
      await expect(healthy.getByRole('button', { name: '关闭行', exact: true })).toHaveAttribute(
        'aria-expanded',
        'true',
      )
      await healthy.getByRole('button', { name: '关闭行', exact: true }).press('Enter')
      await content.getByRole('button', { name: '切换表格密度', exact: true }).click()
      await assertExpandAlignment()
      await content.getByRole('button', { name: '切换表格密度', exact: true }).click()
      await expect(
        content.getByRole('button', { name: '更多操作：cordoned-worker', exact: true }),
      ).toBeInViewport()
      await content.getByRole('button', { name: '更多操作：cordoned-worker', exact: true }).click()
      await expect(page.getByRole('menuitem', { name: /编辑节点/ })).toBeVisible()
      await page.keyboard.press('Escape')
    }
    await page.screenshot({ path: info.outputPath(`nodes-${width}.png`), fullPage: true })
    verifyBrowserEvidence(evidence)
  })
}

test('topology inspector groups neighbors and hides editing from readers', async ({
  context,
  page,
}) => {
  const evidence = await setup(context, page)
  await page.goto('/network/topology?view=table')
  await page.getByRole('button', { name: 'shop-service', exact: true }).first().click()
  const inspector = page.locator('.soha-topology-evidence')
  await expect(inspector.getByRole('heading', { name: 'shop-service', exact: true })).toBeVisible()
  await expect(inspector).toContainText('上游来源')
  await expect(inspector).toContainText('下游目标')
  await expect(inspector.getByRole('button', { name: '编辑 YAML', exact: true })).toHaveCount(0)
  await inspector
    .getByRole('button', { name: '查看关联资源 Shop rule', exact: true })
    .first()
    .press('Enter')
  await expect(inspector.getByRole('heading', { name: 'Shop rule', exact: true })).toBeVisible()
  await expect(inspector).toContainText('shop.example')
  verifyBrowserEvidence(evidence)
})

test('flat network menus and topology editing keep resource scope', async ({
  context,
  page,
}, info) => {
  const evidence = await setup(context, page, { resourceEdit: true, flatNetwork: true })
  await page.goto('/network/topology?view=table')
  await page.getByRole('button', { name: '展开侧栏', exact: true }).click()
  await page.getByRole('menuitem', { name: 'menu 网络', exact: true }).click()
  const leaves = page.getByRole('complementary').locator('.ant-menu-sub .ant-menu-item')
  await expect(leaves).toHaveCount(2)
  await expect(leaves.filter({ hasText: 'Services' })).toBeVisible()
  await expect(page.getByRole('complementary')).not.toContainText('服务与后端')
  await expect(page.getByRole('complementary')).not.toContainText('入口与路由')
  await page.getByRole('button', { name: 'shop-service', exact: true }).first().click()
  const inspector = page.locator('.soha-topology-evidence')
  await expect(inspector.getByRole('button', { name: '编辑 YAML', exact: true })).toBeVisible()
  await page.screenshot({ path: info.outputPath('topology-inspector.png'), fullPage: true })
  await inspector.getByRole('button', { name: '编辑 YAML', exact: true }).press('Enter')
  await expect(page).toHaveURL(
    '/network/services/shop-service?clusterId=cluster-a&namespace=monitoring&tab=yaml',
  )
  await expect(page.getByRole('tab', { name: 'YAML', exact: true })).toHaveAttribute(
    'aria-selected',
    'true',
  )
  await expect
    .poll(() =>
      evidence.requests.some((request) =>
        request.includes('/network/services/shop-service/yaml?namespace=monitoring'),
      ),
    )
    .toBe(true)
  expect(
    evidence.requests
      .filter((request) => request.includes('/network/services/shop-service'))
      .every((request) => request.includes('/clusters/cluster-a/')),
  ).toBe(true)
  verifyBrowserEvidence(evidence)
})
