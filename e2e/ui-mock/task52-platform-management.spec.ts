import { test, expect, type BrowserContext, type Page } from '@playwright/test'
import { installMocks, verifyBrowserEvidence } from '../fixtures/mock-app'
import { verifyGeometry } from '../fixtures/geometry'

const root = '/api/v1/clusters/cluster-a'
const configMapRefresh = '刷新 ConfigMaps'
const hpaYAML =
  'apiVersion: autoscaling/v2\nkind: HorizontalPodAutoscaler\nmetadata:\n  name: autoscale-api\n  namespace: monitoring\nspec:\n  minReplicas: 2\n  maxReplicas: 8\n  scaleTargetRef:\n    apiVersion: apps/v1\n    kind: Deployment\n    name: api\n'
const allowedActions = [
  'view',
  'create',
  'update',
  'delete',
  'restart',
  'scale',
  'rollback',
  'drain',
]
const base = { namespace: 'monitoring', ageSeconds: 3600, allowedActions }
const lists = [
  {
    route: '/configuration/configmaps',
    path: '/configuration/configmaps',
    name: 'application-settings',
    refreshLabel: configMapRefresh,
    data: [
      { ...base, name: 'application-settings', dataEntries: 3, binaryEntries: 0, immutable: false },
    ],
  },
  {
    route: '/configuration/hpas',
    path: '/configuration/hpas',
    name: 'autoscale-api',
    refreshLabel: '刷新 HorizontalPodAutoscalers',
    data: [
      {
        ...base,
        name: 'autoscale-api',
        minReplicas: 2,
        maxReplicas: 8,
        currentReplicas: 3,
        desiredReplicas: 3,
        target: 'Deployment/api',
      },
    ],
  },
  {
    route: '/network/services',
    path: '/network/services',
    name: 'frontend-service',
    refreshLabel: '刷新 Services',
    data: [
      {
        ...base,
        name: 'frontend-service',
        type: 'ClusterIP',
        clusterIp: '10.10.0.10',
        externalIPs: [],
        ports: ['80/TCP'],
        portMappings: [{ port: 80, protocol: 'TCP', targetPort: '8080' }],
        selector: { app: 'frontend' },
      },
    ],
  },
  {
    route: '/storage/persistentvolumeclaims',
    path: '/storage/persistentvolumeclaims',
    name: 'database-data',
    refreshLabel: '刷新 PVC',
    data: [
      {
        ...base,
        name: 'database-data',
        status: 'Bound',
        capacity: '20Gi',
        accessModes: ['ReadWriteOnce'],
        storageClass: 'local',
        volumeName: 'pv-data',
      },
    ],
  },
  {
    route: '/platform-access-control/roles',
    path: '/access-control/roles',
    name: 'namespace-reader',
    refreshLabel: '刷新 Roles',
    data: [{ ...base, name: 'namespace-reader', rules: 2 }],
  },
  {
    route: '/workloads/deployments',
    path: '/workloads/deployments',
    name: 'api',
    refreshLabel: '刷新',
    data: [
      {
        ...base,
        name: 'api',
        desiredReplicas: 3,
        readyReplicas: 3,
        updatedReplicas: 3,
        availableReplicas: 3,
        images: ['example.test/api:v1'],
        labels: { app: 'api' },
      },
    ],
  },
  {
    route: '/cluster-resources/nodes',
    path: '/infrastructure/nodes',
    name: 'worker-prod-01',
    refreshLabel: '刷新节点',
    data: [
      {
        name: 'worker-prod-01',
        status: 'Ready',
        roles: ['worker'],
        version: 'v1.31.0',
        internalIp: '10.0.0.11',
        labels: {},
        taints: [],
        allowedActions,
        podCount: 11,
        resources: {
          allocatable: { cpu: '8', memory: '16Gi', ephemeralStorage: '40Gi', pods: '110' },
          requests: { cpu: '2', memory: '4Gi', ephemeralStorage: '10Gi' },
          requestPercentages: { cpu: 25, memory: 25, ephemeralStorage: 25 },
          usagePercentages: { pods: 10 },
        },
        conditions: [],
      },
    ],
  },
  {
    route: '/helm/releases',
    path: '/helm/releases',
    name: 'ingress-controller',
    refreshLabel: '刷新',
    data: [
      {
        ...base,
        name: 'ingress-controller',
        revision: '3',
        status: 'deployed',
        chart: 'ingress-1.2.0',
        appVersion: '1.2.0',
        updatedAt: '2026-10-16T00:00:00Z',
      },
    ],
  },
] as const

async function platformMocks(context: BrowserContext, page: Page) {
  const state = {
    failedPath: '',
    readStatus: 200,
    writeStatus: 503,
    writes: [] as string[],
    reads: [] as string[],
    deleted: [] as string[],
    configMapName: 'application-settings',
    serviceName: 'frontend-service',
    servicePorts: [{ port: 80, protocol: 'TCP', targetPort: '8080' }] as Array<{
      name?: string
      port: number
      protocol: string
      targetPort: string
      nodePort?: number
    }>,
    collectionCopies: 1,
    namespaceActions: false,
  }
  const failure = (status: number) => ({
    status,
    body: {
      error: {
        code: 'synthetic_failure',
        message: '平台资源请求暂时不可用',
        request_id: 'platform-original-request',
      },
    },
  })
  await context.addInitScript(() =>
    localStorage.setItem(
      'soha-scope',
      JSON.stringify({ state: { clusterId: 'cluster-a', namespace: 'monitoring' }, version: 0 }),
    ),
  )
  const permissions = [
    'workbench.platform.view',
    'workspace.resource.view',
    'platform.clusters.view',
    'platform.clusters.create',
    'platform.clusters.update',
    'platform.clusters.delete',
    'platform.nodes.view',
    'platform.namespaces.view',
    'platform.deployment.view',
    'platform.deployment.update',
    'platform.configuration.config-maps.delete',
    'platform.pods.view',
    'platform.configuration.config-maps.view',
    'platform.configuration.config-maps.update',
    'platform.configuration.horizontal-pod-autoscalers.view',
    'platform.configuration.horizontal-pod-autoscalers.update',
    'platform.configuration.horizontal-pod-autoscalers.delete',
    'platform.network.services.view',
    'platform.storage.persistent-volume-claims.view',
    'platform.storage.persistent-volume-claims.delete',
    'platform.access-control.roles.view',
    'platform.access-control.roles.delete',
    'platform.helm.view',
    'platform.helm.releases.view',
    'platform.helm.releases.delete',
    'platform.helm.charts.view',
  ]
  const evidence = await installMocks(context, page, (url, method, request) => {
    const path = url.pathname
    if (path === '/api/v1/auth/stream-ticket' && method === 'POST') return failure(403)
    if (path === '/api/v1/access/permission-snapshot')
      return {
        body: {
          data: {
            permissionKeys: permissions,
            visibleMenuIds: [
              'clusters',
              'configuration',
              'configuration-configmaps',
              'configuration-hpas',
              'workloads',
              'workloads-deployments',
              'network',
              'network-services',
              'storage',
              'storage-persistentvolumeclaims',
              'platform-access-control',
              'platform-access-control-roles',
              'helm',
              'helm-releases',
            ],
            visibleMenus: [],
          },
        },
      }
    if (method === 'GET') {
      state.reads.push(path)
      if (state.failedPath === path && state.readStatus !== 200) return failure(state.readStatus)
      if (path === '/api/v1/clusters')
        return {
          body: {
            data: [
              {
                id: 'cluster-a',
                name: 'production-east',
                region: 'standard_kubernetes',
                environment: 'production',
                connectionMode: 'direct',
                labels: {},
                version: 'v1.31.0',
                health: { status: 'healthy' },
                allowedActions,
              },
              {
                id: 'cluster-b',
                name: 'production-west',
                region: 'standard_kubernetes',
                environment: 'production',
                connectionMode: 'direct',
                labels: {},
                version: 'v1.31.0',
                health: { status: 'healthy' },
                allowedActions,
              },
            ].filter((item) => !state.deleted.includes(item.id)),
          },
        }
      if (path === '/api/v1/clusters/capabilities')
        return {
          body: {
            data: ['resource.yaml.apply', 'helm.releases', 'workload.mutations'].map((key) => ({
              key,
              category: 'configuration',
              label: key,
              direct: { status: 'available' },
              agent: { status: 'available' },
              requiredScopes: ['cluster', 'namespace'],
              riskLevel: 'mutate',
            })),
          },
        }
      if (state.namespaceActions && path === root + '/namespaces')
        return {
          body: { data: [{ name: 'monitoring', status: 'Active', labels: {}, allowedActions }] },
        }
      if (state.namespaceActions && path === root + '/workloads/pods') {
        expect(url.search).toBe('')
        return { body: { data: [] } }
      }
      const list = lists.find((item) => path === root + item.path)
      if (list) {
        if (list.path !== '/infrastructure/nodes')
          expect(url.searchParams.get('namespace')).toBe('monitoring')
        return {
          body: {
            data:
              state.collectionCopies > 1
                ? Array.from({ length: state.collectionCopies }, (_, index) => ({
                    ...list.data[0],
                    name: `${list.name}-${String(index).padStart(4, '0')}`,
                  }))
                : list.path === '/configuration/configmaps'
                  ? list.data.map((item) => ({ ...item, name: state.configMapName }))
                  : list.path === '/network/services'
                    ? list.data.map((item) => ({
                        ...item,
                        name: state.serviceName,
                        ports: state.servicePorts.map((port) => `${port.port}/${port.protocol}`),
                        portMappings: state.servicePorts,
                      }))
                    : list.data,
          },
        }
      }
      if (path === '/api/v1/clusters/cluster-b/namespaces')
        return { body: { data: [{ name: 'monitoring', status: 'Active', labels: {} }] } }
      if (path === root + '/configuration/hpas/autoscale-api/yaml')
        return { body: { data: { content: hpaYAML } } }
    }
    if (path === root + '/resource-creation/scope-decision' && method === 'POST')
      return { body: { data: { allowed: true, scopeMode: 'namespace', reason: '' } } }
    if (path === root + '/resources/update-plan' && method === 'POST') {
      const input = request.postDataJSON()
      expect(input.name).toBe('autoscale-api')
      expect(input.namespace).toBe('monitoring')
      return {
        body: {
          data: {
            id: 'plan-platform',
            ready: true,
            capability: 'resource.yaml.apply',
            target: 'HorizontalPodAutoscaler monitoring/autoscale-api',
            riskLevel: 'medium',
            requiresApproval: true,
            warnings: [],
            changes: [
              {
                action: 'update',
                resource: 'HorizontalPodAutoscaler',
                summary: '调整副本范围，保留现有目标与指标配置',
              },
            ],
          },
        },
      }
    }
    if (path === root + '/workloads/deployments/restart' && method === 'POST') {
      expect(request.postDataJSON()).toEqual({ namespace: 'monitoring', name: 'api' })
      state.writes.push(path)
      return failure(state.writeStatus)
    }
    if (path === root + '/configuration/hpas/autoscale-api/yaml' && method === 'PUT') {
      state.writes.push(path)
      if (state.writeStatus !== 200) return failure(state.writeStatus)
      return { body: { data: { content: request.postDataJSON().content } } }
    }
    if (
      ['/api/v1/clusters/cluster-a', '/api/v1/clusters/cluster-b'].includes(path) &&
      method === 'DELETE'
    ) {
      state.writes.push(path)
      if (path.endsWith('cluster-b')) return failure(state.writeStatus)
      state.deleted.push('cluster-a')
      return { body: { data: null } }
    }
    return undefined
  })
  return { state, evidence }
}

for (const theme of ['light', 'dark']) {
  test(`Task117 short tables keep exact widths and long service content stays inside its cell (${theme})`, async ({
    context,
    page,
  }) => {
    const { state, evidence } = await platformMocks(context, page)
    await page.goto('/configuration/configmaps')
    if (theme === 'dark') await page.getByLabel('切换到深色模式').click()
    const table = page.locator('.soha-kubernetes-resource-table')
    const header = table.locator('.ant-table-thead th').first()
    const cell = table
      .locator('.ant-table-row[data-row-key]')
      .first()
      .locator(':scope > .ant-table-cell')
      .first()
    const handle = header.getByRole('separator', { name: '调整名称列宽', exact: true })
    const declared = Number(await handle.getAttribute('aria-valuenow'))
    await expect
      .poll(() => header.evaluate((node) => node.getBoundingClientRect().width))
      .toBeCloseTo(declared, 0)
    await page.getByRole('button', { name: '列设置', exact: true }).click()
    await page.getByRole('spinbutton', { name: '列宽像素', exact: true }).fill('280')
    await page.getByRole('spinbutton', { name: '列宽像素', exact: true }).press('Tab')
    await page.keyboard.press('Escape')
    for (const width of [1440, 900, 390]) {
      await page.setViewportSize({ width, height: 900 })
      await expect
        .poll(() => header.evaluate((node) => node.getBoundingClientRect().width))
        .toBeCloseTo(280, 0)
      expect(await cell.evaluate((node) => node.getBoundingClientRect().width)).toBeCloseTo(280, 0)
      expect(await page.evaluate(() => document.documentElement.scrollWidth > innerWidth)).toBe(
        false,
      )
    }
    await page.setViewportSize({ width: 1440, height: 900 })
    await page.reload()
    await expect
      .poll(() => header.evaluate((node) => node.getBoundingClientRect().width))
      .toBeCloseTo(280, 0)
    state.serviceName = `frontend-${'long-service-name-'.repeat(8)}`
    state.servicePorts = [
      {
        name: 'public-http',
        port: 80,
        protocol: 'TCP',
        targetPort: 'upstream-http',
        nodePort: 30080,
      },
      { name: 'metrics', port: 9090, protocol: 'TCP', targetPort: 'metrics' },
    ]
    await page.goto('/network/services')
    const name = table.getByRole('button', { name: state.serviceName, exact: true })
    const portRows = table.locator('.soha-service-port-row')
    await expect(name).toBeVisible()
    await expect(portRows).toHaveCount(2)
    for (const width of [1440, 900, 390]) {
      await page.setViewportSize({ width, height: 900 })
      await expect
        .poll(() =>
          name.evaluate((button) => {
            const bounds = button.getBoundingClientRect()
            const cell = button.closest('.ant-table-cell')
            if (!button.isConnected || !cell) return false
            const parent = cell.getBoundingClientRect()
            return bounds.left >= parent.left && bounds.right <= parent.right
          }),
        )
        .toBe(true)
      await expect
        .poll(() =>
          portRows.evaluateAll(
            (rows) =>
              rows.length === 2 &&
              rows.every((row) => {
                const parent = row.closest('.ant-table-cell')
                if (!row.isConnected || !parent) return false
                const cell = parent.getBoundingClientRect()
                return Array.from(row.children).every((child) => {
                  const bounds = child.getBoundingClientRect()
                  return bounds.left >= cell.left - 1 && bounds.right <= cell.right + 1
                })
              }),
          ),
        )
        .toBe(true)
      expect(await page.evaluate(() => document.documentElement.scrollWidth > innerWidth)).toBe(
        false,
      )
    }
    expect(state.writes).toEqual([])
    verifyBrowserEvidence(evidence)
  })
}

for (const item of lists)
  test(`Platform ${item.route} retains refresh evidence and hides denied cache`, async ({
    context,
    page,
  }) => {
    const { state, evidence } = await platformMocks(context, page)
    await page.goto(item.route)
    await expect(page.getByText(item.name, { exact: true })).toBeVisible()
    const refresh = page.getByRole('button', {
      name: item.refreshLabel,
      exact: true,
    })
    await expect(refresh).toHaveCount(1)
    state.failedPath = root + item.path
    state.readStatus = 503
    await refresh.click()
    await expect(page.getByText('列表未更新，正在展示上次读取结果')).toBeVisible()
    await expect(page.getByText(item.name, { exact: true })).toBeVisible()
    state.readStatus = 403
    await refresh.click()
    await expect(page.getByText('列表无访问权限')).toBeVisible()
    await expect(page.getByText(item.name, { exact: true })).toHaveCount(0)
    expect(state.writes).toEqual([])
    verifyBrowserEvidence(evidence)
  })

for (const mode of ['light', 'dark', 'narrow'] as const)
  test(`Platform feedback design (${mode})`, async ({ context, page }, info) => {
    if (mode === 'dark')
      await context.addInitScript(() =>
        localStorage.setItem(
          'soha-prefs',
          JSON.stringify({ state: { themeMode: 'dark' }, version: 0 }),
        ),
      )
    if (mode === 'narrow') await page.setViewportSize({ width: 390, height: 1000 })
    await page.emulateMedia({ reducedMotion: mode === 'narrow' ? 'reduce' : 'no-preference' })
    const { state, evidence } = await platformMocks(context, page)
    await page.goto('/configuration/configmaps')
    await expect(page.getByText('application-settings', { exact: true })).toBeVisible()
    state.failedPath = root + '/configuration/configmaps'
    state.readStatus = 503
    const refresh = page.getByRole('button', { name: configMapRefresh, exact: true })
    await refresh.press('Enter')
    await expect(page.getByText('列表未更新，正在展示上次读取结果')).toBeVisible()
    await expect(page.locator('.ant-notification-notice')).toHaveCount(0)
    if (mode === 'dark')
      await expect(page.locator('html')).toHaveAttribute('data-theme-mode', 'dark')
    await verifyGeometry(page, refresh)
    await page.screenshot({ path: info.outputPath(`platform-${mode}.png`), fullPage: true })
    verifyBrowserEvidence(evidence)
  })

test('Platform long resource identity remains usable at 125 percent zoom', async ({
  context,
  page,
}) => {
  const { state, evidence } = await platformMocks(context, page)
  state.configMapName = 'production-payments-application-settings-feature-flags-20261016'
  await page.goto('/configuration/configmaps')
  await expect(page.getByText(state.configMapName, { exact: true })).toBeVisible()
  await page.addStyleTag({ content: 'html { zoom: 1.25; }' })
  const refresh = page.getByRole('button', { name: configMapRefresh, exact: true })
  await verifyGeometry(page, refresh)
  await refresh.press('Enter')
  await expect(page.getByText(state.configMapName, { exact: true })).toBeVisible()
  expect(state.writes).toEqual([])
  verifyBrowserEvidence(evidence)
})

async function openHPAEditor(page: Page) {
  await page.goto('/configuration/hpas')
  await page.getByRole('button', { name: '更多操作：autoscale-api', exact: true }).click()
  await page.getByRole('menuitem', { name: '编辑', exact: true }).click()
  const editor = page.getByRole('dialog', { name: /编辑 HorizontalPodAutoscaler/ })
  await editor.getByLabel('最小副本数', { exact: true }).fill('3')
  await editor.getByRole('button', { name: '保存更改', exact: true }).press('Enter')
  const plan = page.getByRole('dialog', { name: '确认资源变更', exact: true })
  await expect(plan.getByRole('button', { name: '确认更新', exact: true })).toBeDisabled()
  await plan.getByRole('checkbox').check()
  return { editor, plan }
}

test('Platform edit keeps its exact draft and confirmation locked after an unknown write', async ({
  context,
  page,
}, info) => {
  await page.setViewportSize({ width: 390, height: 1000 })
  await page.emulateMedia({ reducedMotion: 'reduce' })
  const { state, evidence } = await platformMocks(context, page)
  const { editor, plan } = await openHPAEditor(page)
  await plan.getByRole('button', { name: '确认更新', exact: true }).click()
  await expect(plan.getByText('更新资源：请求结果不明')).toBeVisible()
  await expect(plan.getByRole('button', { name: '确认更新', exact: true })).toBeDisabled()
  await expect(editor.getByLabel('最小副本数', { exact: true })).toHaveValue('3')
  await page.keyboard.press('Escape')
  await expect(plan).toBeVisible()
  expect(state.writes).toHaveLength(1)
  await expect(
    plan.locator('.soha-operation-feedback').filter({ hasText: '更新资源：请求结果不明' }),
  ).toHaveAttribute('data-motion-disabled', 'true')
  await verifyGeometry(page, plan.getByRole('button', { name: /取\s*消/ }))
  await page.screenshot({ path: info.outputPath('platform-unknown.png'), fullPage: true })
  verifyBrowserEvidence(evidence)
})

test('Platform known rejection retains values and successful correction stays on its receipt', async ({
  context,
  page,
}, info) => {
  const { state, evidence } = await platformMocks(context, page)
  state.writeStatus = 409
  const { editor, plan } = await openHPAEditor(page)
  await plan.getByRole('button', { name: '确认更新', exact: true }).click()
  await expect(plan.getByText('更新资源：操作请求失败')).toBeVisible()
  await expect(editor.getByLabel('最小副本数', { exact: true })).toHaveValue('3')
  state.writeStatus = 200
  await plan.getByRole('button', { name: '确认更新', exact: true }).click()
  await expect(plan).toHaveCount(0)
  await expect(page.getByRole('dialog').getByText('更新资源：请求已返回')).toBeVisible()
  await page.screenshot({ path: info.outputPath('platform-receipt.png'), fullPage: true })
  await page
    .getByRole('dialog')
    .getByRole('button', { name: /完\s*成/ })
    .click()
  await expect(page.getByRole('dialog')).toHaveCount(0)
  expect(state.writes).toHaveLength(2)
  verifyBrowserEvidence(evidence)
})

test('Platform cluster batch keeps per-target evidence and only failed selection', async ({
  context,
  page,
}, info) => {
  const { state, evidence } = await platformMocks(context, page)
  await page.goto('/clusters')
  await expect(page.getByText('production-west', { exact: true })).toBeVisible()
  await page.locator('.ant-table-thead input[type="checkbox"]').check()
  await page.getByRole('button', { name: '批量删除', exact: true }).click()
  const confirm = page.getByRole('tooltip')
  await expect(confirm).toContainText('确认删除 2 个集群')
  await confirm.getByRole('button', { name: /删\s*除/ }).click()
  const feedback = page.locator('.soha-operation-feedback')
  await expect(feedback).toContainText('批量删除集群：请求结果不明')
  await expect(feedback).toContainText('cluster-a · 请求成功')
  await expect(feedback).toContainText('cluster-b · 结果不明')
  await expect(page.getByText('已选 1 项', { exact: true })).toBeVisible()
  await expect(page.getByRole('button', { name: '批量删除', exact: true })).toBeDisabled()
  expect(state.writes).toEqual(['/api/v1/clusters/cluster-a', '/api/v1/clusters/cluster-b'])
  await page.screenshot({ path: info.outputPath('platform-batch.png'), fullPage: true })
  verifyBrowserEvidence(evidence)
})

test('Deployment unknown batch restart also blocks the row restart and other writes', async ({
  context,
  page,
}) => {
  const { state, evidence } = await platformMocks(context, page)
  await page.goto('/workloads/deployments')
  const row = page
    .locator('.ant-table-row[data-row-key]')
    .filter({ has: page.getByText('api', { exact: true }) })
  await row.getByRole('checkbox').check()
  const batchActions = page.getByRole('button', { name: '批量操作', exact: true })
  await batchActions.click()
  await page
    .getByRole('menu', { name: 'Deployments 操作：已选 1 个', exact: true })
    .getByRole('menuitem', { name: '批量重启', exact: true })
    .click()
  await expect(page.getByText('批量重启 Deployment：请求结果不明', { exact: true })).toBeVisible()
  await row.getByRole('button', { name: '更多操作：api', exact: true }).click()
  const actions = page.getByRole('menu', { name: 'Deployment 操作：api', exact: true })
  await expect(actions.getByRole('menuitem', { name: '重启', exact: true })).toHaveAttribute(
    'aria-disabled',
    'true',
  )
  await expect(actions.getByRole('menuitem', { name: '编辑', exact: true })).toHaveAttribute(
    'aria-disabled',
    'true',
  )
  await expect(row.getByRole('checkbox')).toBeChecked()
  await expect(batchActions).toBeDisabled()
  await expect(page.getByRole('button', { name: '清除', exact: true })).toBeDisabled()
  expect(state.writes).toEqual([root + '/workloads/deployments/restart'])
  verifyBrowserEvidence(evidence)
})

for (const entry of lists) {
  test(`Task117 shared continuous resource list ${entry.route}`, async ({ context, page }) => {
    const { state, evidence } = await platformMocks(context, page)
    state.collectionCopies = entry.route === '/workloads/deployments' ? 2000 : 80
    await page.goto(entry.route)
    const table = page.locator('.soha-kubernetes-resource-table')
    const rows = table.locator('.ant-table-row[data-row-key]')
    const body = page.getByRole('region', { name: '资源列表，可连续滚动', exact: true })
    await expect(body).toBeVisible()
    await expect(page.locator('.ant-pagination')).toHaveCount(0)
    await expect(
      page.getByRole('status').filter({ hasText: `${state.collectionCopies} 个` }),
    ).toBeVisible()
    expect(await rows.count()).toBeLessThan(80)
    if (entry.route === '/workloads/deployments') {
      await table
        .getByRole('checkbox', { name: '选择全部匹配的 2000 个 Deployments', exact: true })
        .check()
      await expect(page.getByRole('status').filter({ hasText: /^已选 2000 个$/ })).toBeVisible()
      await expect(page.getByRole('button', { name: '批量操作', exact: true })).toBeEnabled()
      await table
        .getByRole('checkbox', { name: '选择全部匹配的 2000 个 Deployments', exact: true })
        .uncheck()
    }
    if (entry.route === '/cluster-resources/nodes') {
      const before = await rows.nth(1).evaluate((row) => row.getBoundingClientRect().top)
      await table.locator('.ant-table-row-expand-icon').first().click()
      const preview = page.locator('.soha-node-expand-grid')
      await expect(preview).toBeVisible()
      for (const [label, value] of [
        ['cpu', '25'],
        ['memory', '25'],
        ['disk', '25'],
        ['pod', '10'],
      ]) {
        await expect(
          preview.getByRole('progressbar', { name: `${label} allocation for worker-prod-01-0000` }),
        ).toHaveAttribute('aria-valuenow', value)
      }
      await expect(preview).toContainText('11 / 110')
      await expect
        .poll(() => rows.nth(1).evaluate((row) => row.getBoundingClientRect().top))
        .toBeGreaterThan(before + 50)
      await body.hover()
      await page.mouse.wheel(0, 500)
      const anchor = () =>
        body.evaluate((holder) => {
          const top = holder.getBoundingClientRect().top
          const row = Array.from(holder.querySelectorAll<HTMLElement>('[data-row-key]')).find(
            (item) => item.getBoundingClientRect().bottom > top + 1,
          )
          return row
            ? {
                key: row.dataset.rowKey,
                offset: row.getBoundingClientRect().top - top,
                scroll: holder.scrollTop,
              }
            : null
        })
      await expect.poll(async () => (await anchor())?.scroll ?? 0).toBeGreaterThan(100)
      const saved = (await anchor())!
      state.collectionCopies += 1
      await page.getByRole('button', { name: '刷新节点', exact: true }).click()
      await expect(page.getByRole('status').filter({ hasText: '81 个' })).toBeVisible()
      await expect.poll(async () => (await anchor())?.key).toBe(saved.key)
      expect(Math.abs((await anchor())!.offset - saved.offset)).toBeLessThan(3)
      await body.focus()
      await body.press('Home')
      await expect(page.locator('.soha-node-expand-grid')).toBeVisible()
      await table.locator('.ant-table-row-expand-icon').first().click()
      await expect(page.locator('.soha-node-expand-grid')).toHaveCount(0)
    }
    await body.focus()
    await body.press('End')
    const last = `${entry.name}-${String(state.collectionCopies - 1).padStart(4, '0')}`
    await expect(table.getByText(last, { exact: true }).first()).toBeInViewport()
    expect(await rows.count()).toBeLessThan(80)
    for (const width of [900, 390]) {
      await page.setViewportSize({ width, height: 900 })
      await expect(body).toBeVisible()
      expect(await page.evaluate(() => document.documentElement.scrollWidth > innerWidth)).toBe(
        false,
      )
    }
    expect(state.writes).toEqual([])
    verifyBrowserEvidence(evidence)
  })
}

for (const item of lists.filter((entry) => entry.path !== '/workloads/deployments')) {
  test(`Task117 shared action menu retains confirmation and permission semantics: ${item.route}`, async ({
    context,
    page,
  }) => {
    const { state, evidence } = await platformMocks(context, page)
    await page.goto(item.route)
    const trigger = page.getByRole('button', { name: `更多操作：${item.name}`, exact: true })
    await expect(trigger).toBeVisible()
    const column = trigger.locator('xpath=ancestor::*[contains(@class,"ant-table-cell")][1]')
    expect((await column.boundingBox())!.width).toBeCloseTo(64, 0)
    await trigger.press('ArrowDown')
    const menu = page.getByRole('menu', { name: / 操作：/ })
    await expect(menu).toBeVisible()
    await page.keyboard.press('Escape')
    await expect(trigger).toBeFocused()
    await trigger.click()
    const deleteLabel = item.path === '/helm/releases' ? '卸载 Helm Release' : '删除'
    await menu.getByRole('menuitem', { name: deleteLabel, exact: true }).click()
    const dialog = page.getByRole('dialog')
    await expect(dialog).toContainText(item.name)
    if (item.path === '/infrastructure/nodes')
      await expect(dialog).toContainText('不会自动回收底层机器')
    await dialog.getByRole('button', { name: /^取\s*消$/ }).click()
    expect(state.writes).toEqual([])
    verifyBrowserEvidence(evidence)
  })
}

test('Task117 namespace navigation and destructive confirmation stay distinct', async ({
  context,
  page,
}) => {
  const { state, evidence } = await platformMocks(context, page)
  state.namespaceActions = true
  await page.goto('/cluster-resources/namespaces')
  const trigger = page.getByRole('button', { name: '更多操作：monitoring', exact: true })
  await trigger.click()
  const menu = page.getByRole('menu', { name: '命名空间 操作：monitoring', exact: true })
  await expect(menu.getByRole('menuitem', { name: '查看资源', exact: true })).toBeVisible()
  await expect(menu.getByRole('menuitem', { name: 'Helm', exact: true })).toBeVisible()
  await menu.getByRole('menuitem', { name: '删除', exact: true }).click()
  const dialog = page.getByRole('dialog')
  await expect(dialog).toContainText('该命名空间下的资源会一并回收')
  await dialog.getByRole('button', { name: /^取\s*消$/ }).click()
  await expect(page).toHaveURL('/cluster-resources/namespaces')
  await trigger.click()
  await menu.getByRole('menuitem', { name: 'Helm', exact: true }).click()
  await expect(page).toHaveURL('/helm/releases?clusterId=cluster-a&namespace=monitoring')
  await expect(page.getByText('ingress-controller', { exact: true })).toBeVisible()
  expect(evidence.requests).toContain(root + '/helm/releases?namespace=monitoring')
  expect(state.writes).toEqual([])
  verifyBrowserEvidence(evidence)
})
test('Task117 node drain retains its explicit risk and parameter form', async ({
  context,
  page,
}) => {
  const { state, evidence } = await platformMocks(context, page)
  await page.goto('/cluster-resources/nodes')
  await page.getByRole('button', { name: '更多操作：worker-prod-01', exact: true }).click()
  await page.getByRole('menuitem', { name: '排空节点', exact: true }).click()
  const dialog = page.getByRole('dialog')
  await expect(dialog).toContainText('DaemonSet 和静态 Pod 不会被驱逐')
  await expect(dialog.getByRole('spinbutton')).toHaveValue('300')
  for (const box of await dialog.getByRole('checkbox').all()) await expect(box).not.toBeChecked()
  await dialog.getByRole('button', { name: /^取\s*消$/ }).click()
  expect(state.writes).toEqual([])
  verifyBrowserEvidence(evidence)
})
