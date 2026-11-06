import { test, expect, type BrowserContext, type Page } from '@playwright/test'
import { stringify } from 'yaml'
import { baselineResponse } from '../fixtures/baseline-data'
import { installMocks, verifyBrowserEvidence } from '../fixtures/mock-app'

// Synthetic owner-specific payloads. This suite proves browser behavior, not cluster acceptance.
const owners = [
  { kind: 'configmaps', permission: 'config-maps', label: 'ConfigMap', namespaced: true },
  { kind: 'secrets', permission: 'secrets', label: 'Secret', namespaced: true },
  {
    kind: 'resourcequotas',
    permission: 'resource-quotas',
    label: 'ResourceQuota',
    namespaced: true,
  },
  { kind: 'limitranges', permission: 'limit-ranges', label: 'LimitRange', namespaced: true },
  {
    kind: 'hpas',
    permission: 'horizontal-pod-autoscalers',
    label: 'HorizontalPodAutoscaler',
    namespaced: true,
  },
  {
    kind: 'poddisruptionbudgets',
    permission: 'pod-disruption-budgets',
    label: 'PodDisruptionBudget',
    namespaced: true,
  },
  {
    kind: 'priorityclasses',
    permission: 'priority-classes',
    label: 'PriorityClass',
    namespaced: false,
  },
  {
    kind: 'runtimeclasses',
    permission: 'runtime-classes',
    label: 'RuntimeClass',
    namespaced: false,
  },
  { kind: 'leases', permission: 'leases', label: 'Lease', namespaced: true },
  {
    kind: 'mutatingwebhookconfigurations',
    permission: 'mutating-webhook-configurations',
    label: 'MutatingWebhookConfiguration',
    namespaced: false,
  },
  {
    kind: 'validatingwebhookconfigurations',
    permission: 'validating-webhook-configurations',
    label: 'ValidatingWebhookConfiguration',
    namespaced: false,
  },
] as const
type Owner = (typeof owners)[number]
const namespace = 'monitoring'
const root = '/api/v1/clusters/cluster-a/configuration/'
const sampleName = 'configuration-sample'
const lastName = 'zz-final-collection-item'
const conditions = [
  { type: 'AbleToScale', status: 'True', reason: 'ReadyForNewScale' },
  { type: 'ScalingActive', status: 'True', reason: 'ValidMetricFound' },
]
const rules = [
  {
    type: 'Container',
    min: { cpu: '50m' },
    max: { cpu: '2' },
    defaultRequest: { cpu: '100m' },
    default: { cpu: '500m' },
    maxLimitRequestRatio: { cpu: '5' },
  },
]

function record(owner: Owner, name: string, allowedActions?: string[]) {
  const metadata = {
    name,
    ...(owner.namespaced ? { namespace } : {}),
    ageSeconds: 3600,
    createdAt: '2026-11-03T01:00:00Z',
    labels: { app: 'synthetic' },
    annotations: {},
    ...(allowedActions ? { allowedActions } : {}),
  }
  switch (owner.kind) {
    case 'configmaps':
      return {
        ...metadata,
        immutable: false,
        dataEntries: 1,
        data: { 'app.yaml': 'enabled: true' },
        binaryData: {},
      }
    case 'secrets':
      return { ...metadata, type: 'Opaque', immutable: false, dataEntries: 1 }
    case 'resourcequotas':
      return {
        ...metadata,
        hard: { pods: '10', 'requests.cpu': '4' },
        used: { pods: '3', 'requests.cpu': '1' },
        scopes: [],
      }
    case 'limitranges':
      return { ...metadata, limits: 1, rules }
    case 'hpas':
      return {
        ...metadata,
        minReplicas: 2,
        maxReplicas: 8,
        currentReplicas: 3,
        desiredReplicas: 4,
        targetRef: 'Deployment/api',
        conditions,
        metrics: [],
      }
    case 'poddisruptionbudgets':
      return {
        ...metadata,
        currentHealthy: 3,
        desiredHealthy: 2,
        disruptionsAllowed: 1,
        minAvailable: '2',
        selector: 'app=synthetic',
        pods: [],
        conditions: [],
      }
    case 'priorityclasses':
      return {
        ...metadata,
        value: 1000,
        globalDefault: false,
        preemptionPolicy: 'Never',
        description: 'Synthetic scheduling policy',
      }
    case 'runtimeclasses':
      return { ...metadata, handler: 'synthetic-runtime' }
    case 'leases':
      return {
        ...metadata,
        holderIdentity: 'synthetic-controller',
        leaseDurationSeconds: 30,
        renewTime: '2026-11-03T01:00:00Z',
      }
    case 'mutatingwebhookconfigurations':
    case 'validatingwebhookconfigurations':
      return { ...metadata, webhooks: 2 }
  }
}

function yaml(owner: Owner, name: string) {
  const metadata = {
    name,
    ...(owner.namespaced ? { namespace } : {}),
    generation: 1,
    labels: { app: 'synthetic' },
  }
  const base = { metadata, kind: owner.label }
  switch (owner.kind) {
    case 'configmaps':
      return stringify({ ...base, apiVersion: 'v1', data: { 'app.yaml': 'enabled: true' } })
    case 'secrets':
      return stringify({
        ...base,
        apiVersion: 'v1',
        type: 'Opaque',
        data: { token: 'c3ludGhldGlj' },
      })
    case 'resourcequotas':
      return stringify({
        ...base,
        apiVersion: 'v1',
        spec: {
          hard: { pods: '10', 'requests.cpu': '4' },
          scopeSelector: {
            matchExpressions: [
              { scopeName: 'PriorityClass', operator: 'In', values: ['synthetic-priority'] },
            ],
          },
        },
      })
    case 'limitranges':
      return stringify({ ...base, apiVersion: 'v1', spec: { limits: rules } })
    case 'hpas':
      return stringify({
        ...base,
        apiVersion: 'autoscaling/v2',
        spec: {
          minReplicas: 2,
          maxReplicas: 8,
          scaleTargetRef: { apiVersion: 'apps/v1', kind: 'Deployment', name: 'api' },
          metrics: [
            {
              type: 'Resource',
              resource: { name: 'cpu', target: { type: 'Utilization', averageUtilization: 75 } },
            },
          ],
          behavior: { scaleDown: { stabilizationWindowSeconds: 300 } },
        },
        status: {
          currentReplicas: 3,
          desiredReplicas: 4,
          observedGeneration: 1,
          conditions,
          currentMetrics: [
            { type: 'Resource', resource: { name: 'cpu', current: { averageUtilization: 61 } } },
          ],
        },
      })
    case 'poddisruptionbudgets':
      return stringify({
        ...base,
        apiVersion: 'policy/v1',
        spec: {
          minAvailable: 2,
          selector: { matchLabels: { app: 'synthetic' } },
          unhealthyPodEvictionPolicy: 'AlwaysAllow',
        },
        status: {
          currentHealthy: 3,
          desiredHealthy: 2,
          disruptionsAllowed: 1,
          expectedPods: 3,
          observedGeneration: 1,
          conditions: [],
        },
      })
    case 'priorityclasses':
      return stringify({
        ...base,
        apiVersion: 'scheduling.k8s.io/v1',
        value: 1000,
        globalDefault: false,
        preemptionPolicy: 'Never',
        description: 'Synthetic scheduling policy',
      })
    case 'runtimeclasses':
      return stringify({
        ...base,
        apiVersion: 'node.k8s.io/v1',
        handler: 'synthetic-runtime',
        overhead: { podFixed: { cpu: '100m', memory: '64Mi' } },
        scheduling: { nodeSelector: { runtime: 'synthetic' }, tolerations: [] },
      })
    case 'leases':
      return stringify({
        ...base,
        apiVersion: 'coordination.k8s.io/v1',
        spec: {
          holderIdentity: 'synthetic-controller',
          leaseDurationSeconds: 30,
          renewTime: '2026-11-03T01:00:00Z',
          leaseTransitions: 2,
        },
      })
    case 'mutatingwebhookconfigurations':
    case 'validatingwebhookconfigurations':
      return stringify({
        ...base,
        apiVersion: 'admissionregistration.k8s.io/v1',
        webhooks: [
          {
            name: 'service.synthetic.test',
            clientConfig: {
              service: { name: 'admission', namespace, path: '/admit', port: 443 },
              caBundle: 'c3ludGhldGlj',
            },
            sideEffects: 'None',
            failurePolicy: 'Fail',
            admissionReviewVersions: ['v1'],
            rules: [
              {
                operations: ['CREATE'],
                apiGroups: [''],
                apiVersions: ['v1'],
                resources: ['pods'],
                scope: 'Namespaced',
              },
            ],
          },
          {
            name: 'url.synthetic.test',
            clientConfig: { url: 'https://admission.invalid/synthetic' },
            sideEffects: 'None',
            failurePolicy: 'Ignore',
            admissionReviewVersions: ['v1'],
            reinvocationPolicy: 'IfNeeded',
            namespaceSelector: { matchLabels: { team: 'synthetic' } },
            matchConditions: [
              { name: 'synthetic-condition', expression: 'object.metadata.name != "excluded"' },
            ],
            rules: [],
          },
        ],
      })
  }
}

function detailURL(owner: Owner, name = sampleName, tab?: string) {
  return `/configuration/${owner.kind}/${name}?${new URLSearchParams({ clusterId: 'cluster-a', namespace, ...(tab ? { tab } : {}) })}`
}

async function setup(
  context: BrowserContext,
  page: Page,
  options: {
    missingActions?: boolean
    denied?: string
    fail?: string
    name?: string
  } = {},
) {
  const state = { reads: [] as string[], writes: [] as string[], fail: options.fail }
  const name = options.name ?? sampleName
  const evidence = await installMocks(context, page, (url, method, request) => {
    if (url.pathname === '/api/v1/access/permission-snapshot') {
      const body = baselineResponse(url.pathname)!
      if (!('permissionKeys' in body.data) || !body.data.permissionKeys)
        throw new Error('Missing baseline permissions')
      body.data.permissionKeys.push(
        ...owners.flatMap((owner) =>
          owner.kind === options.denied
            ? []
            : ['view', 'update', 'delete'].map(
                (action) => `platform.configuration.${owner.permission}.${action}`,
              ),
        ),
      )
      // Deliberately do not grant platform.configuration.secret-data.view.
      return { body }
    }
    if (
      url.pathname === '/api/v1/clusters/cluster-a/resource-creation/scope-decision' &&
      method === 'POST'
    ) {
      const input = request.postDataJSON() as {
        kind: string
        resourceGroup: string
        action: string
        namespace?: string
      }
      const owner = owners.find((item) => item.label === input.kind)
      if (!owner || input.resourceGroup !== 'configuration' || input.action !== 'create')
        return undefined
      if (owner.namespaced) expect([undefined, namespace]).toContain(input.namespace)
      else expect(input.namespace).toBeUndefined()
      return {
        body: {
          data: {
            allowed: false,
            reason: 'Synthetic role has no create permission',
            allowedActions: [],
            resourceScope: {
              clusterIds: ['cluster-a'],
              namespaces: owner.namespaced ? [namespace] : [],
              resourceGroups: ['configuration'],
              resourceKinds: [owner.label],
            },
            capability: { key: 'resource.create', status: 'available', mode: 'agent' },
          },
        },
      }
    }
    if (url.pathname === '/api/v1/clusters/cluster-a/resources/graph') {
      state.reads.push(url.pathname + url.search)
      const owner = owners.find((item) => item.label === url.searchParams.get('kind'))!
      const apiVersion =
        owner.kind === 'hpas'
          ? 'autoscaling/v2'
          : owner.kind === 'poddisruptionbudgets'
            ? 'policy/v1'
            : owner.kind === 'priorityclasses'
              ? 'scheduling.k8s.io/v1'
              : owner.kind === 'runtimeclasses'
                ? 'node.k8s.io/v1'
                : owner.kind.includes('webhook')
                  ? 'admissionregistration.k8s.io/v1'
                  : 'v1'
      expect(url.searchParams.get('namespace')).toBe(owner.namespaced ? namespace : null)
      return {
        body: {
          data: {
            clusterId: 'cluster-a',
            rootId: 'root',
            generatedAt: '2026-11-05T01:00:00Z',
            warnings: ['Synthetic partial coverage'],
            evidence: [],
            nodes: [
              {
                id: 'root',
                resource: {
                  clusterId: 'cluster-a',
                  kind: owner.label,
                  name: sampleName,
                  apiVersion,
                  scopeMode: owner.namespaced ? 'namespace' : 'cluster',
                  ...(owner.namespaced ? { namespace } : {}),
                },
              },
              {
                id: 'consumer',
                status: 'unresolved',
                resource: {
                  clusterId: 'cluster-a',
                  kind: 'Pod',
                  name: 'declared-consumer',
                  apiVersion: 'v1',
                  namespace,
                  scopeMode: 'namespace',
                },
              },
            ],
            edges: [
              { id: 'reference', sourceId: 'consumer', targetId: 'root', relation: 'uses-config' },
            ],
          },
        },
      }
    }
    if (!url.pathname.startsWith(root)) return undefined
    const [kind, requestedName, endpoint] = url.pathname.slice(root.length).split('/')
    const owner = owners.find((item) => item.kind === kind)
    if (!owner) return undefined
    if (method !== 'GET') {
      state.writes.push(`${method} ${url.pathname}`)
      return undefined
    }
    state.reads.push(url.pathname + url.search)
    if (
      url.searchParams.has('namespace') &&
      (!owner.namespaced || url.searchParams.get('namespace') !== namespace)
    )
      return {
        status: 400,
        body: { error: { code: 'scope_mismatch', message: 'Incorrect configuration scope' } },
      }
    if (kind === state.fail)
      return {
        status: 503,
        body: {
          error: { code: 'unavailable', message: 'Synthetic configuration read unavailable' },
        },
      }
    if (!requestedName)
      return {
        body: {
          data: Array.from({ length: 42 }, (_, index) =>
            record(
              owner,
              index === 0
                ? name
                : index === 41
                  ? lastName
                  : `sample-${String(index).padStart(2, '0')}`,
              options.missingActions ? undefined : ['update', 'delete'],
            ),
          ),
        },
      }
    if (
      requestedName !== name ||
      (owner.namespaced && url.searchParams.get('namespace') !== namespace)
    )
      return undefined
    // Secret metadata tests intentionally have no sensitive endpoint mocks.
    if (kind === 'secrets') return undefined
    if (endpoint === 'detail')
      return {
        body: {
          data: record(owner, name, options.missingActions ? undefined : ['update', 'delete']),
        },
      }
    if (endpoint === 'yaml') return { body: { data: { content: yaml(owner, name) } } }
    if (endpoint === 'references' && kind === 'configmaps') return { body: { data: [] } }
    return undefined
  })
  return { evidence, state }
}

async function noPageOverflow(page: Page) {
  const size = await page.evaluate(() => ({
    width: document.documentElement.clientWidth,
    content: document.documentElement.scrollWidth,
  }))
  expect(size.content, 'Horizontal scrolling belongs inside tables').toBeLessThanOrEqual(
    size.width + 1,
  )
}

for (const owner of owners) {
  test(`${owner.label}: continuous collection, namespace semantics and name navigation`, async ({
    context,
    page,
  }) => {
    const { evidence, state } = await setup(context, page)
    await page.goto(`/configuration/${owner.kind}?clusterId=cluster-a&namespace=${namespace}`)
    await expect(page.getByRole('status').filter({ hasText: '42' })).toBeVisible()
    await expect(page.locator('.ant-pagination')).toHaveCount(0)
    if (owner.namespaced) {
      await page
        .getByRole('button', { name: `切换到命名空间 ${namespace}` })
        .first()
        .click()
      await expect(page).toHaveURL(/namespace=monitoring/)
    } else {
      await expect(page.getByRole('columnheader', { name: '命名空间', exact: true })).toHaveCount(0)
    }
    // Filter the last item of the complete collection; no fake server pagination/search.
    const search = page.getByPlaceholder(/^搜索 /)
    await search.fill(lastName)
    await expect(page.getByRole('status').filter({ hasText: '匹配 1 / 共 42' })).toBeVisible()
    await expect(page.getByText(lastName, { exact: true })).toBeVisible()
    await search.fill('')
    await page.getByText(sampleName, { exact: true }).click()
    await expect(page.getByRole('heading', { name: sampleName, exact: true })).toBeVisible()
    await expect(page.getByRole('tab', { name: '概览', exact: true })).toHaveAttribute(
      'aria-selected',
      'true',
    )
    await expect(page.getByRole('tab', { name: 'YAML', exact: true })).toBeVisible()
    const expectedTabs =
      owner.kind === 'configmaps' || owner.kind === 'secrets'
        ? ['概览', '数据', '关联资源', 'YAML']
        : owner.kind === 'poddisruptionbudgets'
          ? ['概览', 'Pods', '关联资源', 'YAML']
          : owner.kind.includes('webhook')
            ? ['概览', 'Webhooks', '关联资源', 'YAML']
            : ['hpas', 'priorityclasses', 'runtimeclasses'].includes(owner.kind)
              ? ['概览', '关联资源', 'YAML']
              : ['概览', 'YAML']
    await expect(page.locator('.soha-resource-tabs').getByRole('tab')).toHaveText(expectedTabs)
    if (['hpas', 'poddisruptionbudgets', 'resourcequotas'].includes(owner.kind)) {
      await expect
        .poll(() =>
          state.reads.some((path) =>
            path.startsWith(root + owner.kind + '/' + sampleName + '/yaml'),
          ),
        )
        .toBe(true)
      await expect(page.getByRole('alert').filter({ hasText: '无法解析' })).toHaveCount(0)
    }
    const requests = state.reads.map((path) => new URL(path, 'http://127.0.0.1:4179'))
    expect(requests.length).toBeGreaterThan(0)
    for (const request of requests) {
      expect(request.searchParams.has('page')).toBe(false)
      expect(request.searchParams.has('pageSize')).toBe(false)
      expect(request.searchParams.has('keyword')).toBe(false)
      if (!owner.namespaced) expect(request.searchParams.has('namespace')).toBe(false)
    }
    expect(state.writes).toEqual([])
    verifyBrowserEvidence(evidence)
  })
}

test('Secret deep link remains metadata-only across data, references and YAML tabs', async ({
  context,
  page,
}) => {
  const owner = owners[1]
  const { evidence, state } = await setup(context, page)
  await page.goto(detailURL(owner))
  await expect(page.getByRole('heading', { name: sampleName, exact: true })).toBeVisible()
  for (const tab of ['数据', 'YAML']) {
    await page.getByRole('tab', { name: tab, exact: true }).click()
    await expect(page.getByRole('tabpanel', { name: tab, exact: true })).toContainText(
      '敏感数据查看权限',
    )
  }
  await page.getByRole('tab', { name: '关联资源', exact: true }).click()
  await expect(page.getByText('Synthetic partial coverage', { exact: true })).toBeVisible()
  await expect(page.getByRole('button', { name: '编辑数据', exact: true })).toHaveCount(0)
  expect(
    state.reads.filter((path) => /\/(detail|yaml|references|certificate)(\?|$)/.test(path)),
  ).toEqual([])
  expect(state.writes).toEqual([])
  verifyBrowserEvidence(evidence)
})

test('Missing allowedActions keeps readable LimitRange detail and YAML read-only', async ({
  context,
  page,
}) => {
  const owner = owners[3]
  const { evidence, state } = await setup(context, page, { missingActions: true })
  await page.goto(detailURL(owner))
  await expect(page.getByRole('heading', { name: sampleName, exact: true })).toBeVisible()
  await expect(page.getByRole('button', { name: '编辑限制', exact: true })).toHaveCount(0)
  await expect(page.getByRole('button', { name: '删除', exact: true })).toHaveCount(0)
  await page.getByRole('tab', { name: 'YAML', exact: true }).click()
  await expect(page.getByRole('tabpanel', { name: 'YAML', exact: true })).toContainText(
    'LimitRange',
  )
  await expect(page.getByRole('button', { name: /^应\s*用$/ })).toBeDisabled()
  expect(
    state.reads.some((path) =>
      path.includes('/limitranges/' + sampleName + '/yaml?namespace=monitoring'),
    ),
  ).toBe(true)
  expect(state.writes).toEqual([])
  verifyBrowserEvidence(evidence)
})

test('Denied configuration route returns to an accessible page without reading the owner API', async ({
  context,
  page,
}) => {
  const { evidence, state } = await setup(context, page, { denied: 'runtimeclasses' })
  await page.goto(detailURL(owners[7]))
  await expect(page).toHaveURL(/\/configuration\/configmaps(?:\?|$)/)
  await expect(page.getByRole('status').filter({ hasText: '42' })).toBeVisible()
  expect(state.reads.some((path) => path.startsWith(root + 'runtimeclasses'))).toBe(false)
  expect(state.reads.every((path) => path.startsWith(root + 'configmaps'))).toBe(true)
  verifyBrowserEvidence(evidence)
})

test('ResourceQuota read error stays visible and explicit retry recovers the same identity', async ({
  context,
  page,
}) => {
  const owner = owners[2]
  const { evidence, state } = await setup(context, page, { fail: owner.kind })
  await page.goto(detailURL(owner))
  await expect(page.getByRole('alert').filter({ hasText: '读取失败' })).toContainText(
    'Synthetic configuration read unavailable',
  )
  await expect(page.getByRole('heading', { name: sampleName, exact: true })).toHaveCount(0)
  state.fail = undefined
  await page.getByRole('button', { name: /重试/ }).click()
  await expect(page.getByRole('heading', { name: sampleName, exact: true })).toBeVisible()
  await expect(page.getByText('synthetic-priority', { exact: false })).toBeVisible()
  expect(state.writes).toEqual([])
  verifyBrowserEvidence(evidence)
})

test('HPA YAML deep link, draft and selected tab survive resource refresh and tab switches', async ({
  context,
  page,
}) => {
  const owner = owners[4]
  const { evidence, state } = await setup(context, page)
  await page.goto(detailURL(owner, sampleName, 'yaml'))
  const tab = page.getByRole('tab', { name: 'YAML', exact: true })
  await expect(tab).toHaveAttribute('aria-selected', 'true')
  const editor = page.getByRole('textbox', { name: 'YAML 草稿编辑器' })
  await editor.focus()
  await expect(editor).toBeFocused()
  await editor.press('ControlOrMeta+End')
  await page.keyboard.type('\n# retained configuration batch draft')
  const before = state.reads.length
  await page.getByRole('button', { name: '刷新 HorizontalPodAutoscaler', exact: true }).click()
  await expect.poll(() => state.reads.length).toBeGreaterThan(before)
  await expect(tab).toHaveAttribute('aria-selected', 'true')
  await page.getByRole('tab', { name: '概览', exact: true }).click()
  await expect(page.getByText('75%', { exact: true })).toBeVisible()
  await expect(page.getByText('61%', { exact: true })).toBeVisible()
  await tab.click()
  await expect(page.getByRole('tabpanel', { name: 'YAML', exact: true })).toContainText(
    '# retained configuration batch draft',
  )
  expect(state.writes).toEqual([])
  verifyBrowserEvidence(evidence)
})

for (const owner of [owners[9], owners[10]]) {
  test(`${owner.label}: selected Webhook is URL-addressable and never probes a callback`, async ({
    context,
    page,
  }) => {
    const { evidence, state } = await setup(context, page)
    await page.goto(detailURL(owner, sampleName, 'webhooks') + '&webhook=url.synthetic.test')
    await expect(page.getByRole('tab', { name: 'Webhooks', exact: true })).toHaveAttribute(
      'aria-selected',
      'true',
    )
    await expect(
      page.getByText('https://admission.invalid/synthetic', { exact: true }).last(),
    ).toBeVisible()
    if (owner.kind === 'validatingwebhookconfigurations')
      await expect(page.getByText('ReinvocationPolicy', { exact: true })).toHaveCount(0)
    else await expect(page.getByText('IfNeeded', { exact: true })).toBeVisible()
    const chooser = page.getByRole('combobox', { name: 'Webhook', exact: true })
    await chooser.click()
    await chooser.fill('service.synthetic.test')
    await page
      .locator('.ant-select-item-option-content')
      .filter({ hasText: /^service\.synthetic\.test · monitoring\/admission$/ })
      .click()
    await expect(page).toHaveURL(/webhook=service.synthetic.test/)
    await expect(page.getByText('monitoring/admission', { exact: true }).last()).toBeVisible()
    await page.getByRole('button', { name: '刷新 ' + owner.label, exact: true }).click()
    await expect(page.getByRole('tab', { name: 'Webhooks', exact: true })).toHaveAttribute(
      'aria-selected',
      'true',
    )
    await expect(page).toHaveURL(/webhook=service.synthetic.test/)
    expect(state.reads.every((path) => path.startsWith(root + owner.kind))).toBe(true)
    expect(state.writes).toEqual([])
    verifyBrowserEvidence(evidence)
  })
}

for (const theme of ['light', 'dark'] as const) {
  test(`${theme}: 1280px long configuration names stay within the viewport`, async ({
    context,
    page,
  }) => {
    const name = 'configuration-' + 'long-resource-name-'.repeat(9) + 'tail'
    const { evidence } = await setup(context, page, { name })
    await page.setViewportSize({ width: 1280, height: 900 })
    await page.addInitScript(
      (mode) =>
        localStorage.setItem(
          'soha-prefs',
          JSON.stringify({
            state: { themeMode: mode, themeId: 'soha', localeCode: 'zh_CN' },
            version: 0,
          }),
        ),
      theme,
    )
    // Exercise a wide namespaced table and an admission workspace with long metadata.
    for (const owner of [owners[4], owners[10]]) {
      await page.goto(`/configuration/${owner.kind}?clusterId=cluster-a&namespace=${namespace}`)
      await expect(page.getByRole('status').filter({ hasText: '42' })).toBeVisible()
      await expect(page.locator('html')).toHaveAttribute('data-theme-mode', theme)
      await noPageOverflow(page)
      await page.getByText(name, { exact: true }).click()
      await expect(page.getByRole('heading', { name, exact: true })).toBeVisible()
      if (owner.kind.includes('webhook'))
        await page.getByRole('tab', { name: 'Webhooks', exact: true }).click()
      await noPageOverflow(page)
      const refresh = page.getByRole('button', { name: '刷新 ' + owner.label, exact: true })
      await expect(refresh).toBeInViewport()
      await refresh.click()
      const selectedTab = owner.kind.includes('webhook') ? 'Webhooks' : '概览'
      await expect(page.getByRole('tab', { name: selectedTab, exact: true })).toHaveAttribute(
        'aria-selected',
        'true',
      )
      await expect(page.getByRole('tabpanel', { name: selectedTab, exact: true })).toBeVisible()
      await noPageOverflow(page)
      await page.screenshot({
        path: test.info().outputPath(`${owner.kind}-${theme}-1280.png`),
        animations: 'disabled',
      })
    }
    verifyBrowserEvidence(evidence)
  })
}

for (const owner of owners) {
  const hasRelationships = !['resourcequotas', 'limitranges', 'leases'].includes(owner.kind)
  test(`${owner.label}: shared relationships preserve metadata permission and scope`, async ({
    context,
    page,
  }) => {
    const { evidence, state } = await setup(context, page)
    await page.goto(detailURL(owner))
    const tab = page.getByRole('tab', { name: '关联资源', exact: true })
    if (!hasRelationships) {
      await expect(page.getByRole('tab', { name: '概览', exact: true })).toBeVisible()
      await expect(tab).toHaveCount(0)
      expect(state.reads.some((url) => url.includes('/resources/graph'))).toBe(false)
    } else {
      await tab.click()
      await expect(page.getByText('Synthetic partial coverage', { exact: true })).toBeVisible()
      await page
        .getByRole('radiogroup', { name: '关联资源视图' })
        .getByText('列表', { exact: true })
        .click()
      await page.getByRole('button', { name: 'declared-consumer', exact: true }).click()
      const inspector = page.getByRole('complementary', { name: '选中资源信息' })
      await expect(inspector).toContainText('未解析：仅有引用声明')
      await expect(inspector.getByRole('button', { name: '查看详情', exact: true })).toBeDisabled()
      expect(state.reads.filter((url) => url.includes('/resources/graph'))).toHaveLength(1)
      expect(state.reads.some((url) => url.includes('/references'))).toBe(false)
      if (owner.kind === 'secrets')
        expect(state.reads.some((url) => /secrets\/[^/]+\/(detail|yaml)/.test(url))).toBe(false)
    }
    expect(state.writes).toEqual([])
    verifyBrowserEvidence(evidence)
  })
}
