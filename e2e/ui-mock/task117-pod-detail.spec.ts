import { test, expect, type BrowserContext, type Page } from '@playwright/test'
import { baselineResponse } from '../fixtures/baseline-data'
import { installMocks, verifyBrowserEvidence } from '../fixtures/mock-app'

const podName = 'api-7d9-with-a-long-pod-name'
const path = `/workloads/pods/${podName}?clusterId=cluster-a&namespace=monitoring`
async function setup(context: BrowserContext, page: Page, sessions = true) {
  const state = { terminalTickets: 0, ranges: [] as string[], graphReads: 0 }
  await context.addInitScript(() =>
    localStorage.setItem(
      'soha-scope',
      JSON.stringify({ state: { clusterId: 'cluster-a', namespace: 'monitoring' }, version: 0 }),
    ),
  )
  await page.routeWebSocket(`**/workloads/pods/${podName}/terminal?*`, (socket) =>
    socket.send(JSON.stringify({ type: 'stdout', data: 'Synthetic terminal ready\r\n' })),
  )
  const evidence = await installMocks(context, page, (url, method, request) => {
    if (url.pathname === '/api/v1/access/permission-snapshot') {
      const body = baselineResponse(url.pathname)!
      if (!('permissionKeys' in body.data)) throw new Error('Missing permission fixture')
      body.data.permissionKeys!.push(
        'platform.pods.view',
        'platform.nodes.view',
        'platform.namespaces.view',
        ...(sessions ? ['platform.pods.logs', 'platform.pods.exec'] : []),
      )
      body.data.visibleMenuIds!.push('workloads-pods')
      body.data.visibleMenus!.push({
        id: 'workloads-pods',
        parentId: 'workloads',
        path: '/workloads/pods',
        labelZh: 'Pods',
      })
      return { body }
    }
    if (url.pathname === '/api/v1/clusters')
      return {
        body: {
          data: [
            {
              id: 'cluster-a',
              name: 'Pod 详情样板',
              connectionMode: 'direct_kubeconfig',
              environment: 'testing',
              health: { status: 'healthy' },
            },
          ],
        },
      }
    if (url.pathname === '/api/v1/clusters/capabilities')
      return {
        body: {
          data: ['pod.logs', 'pod.exec', 'workload.mutations'].map((key) => ({
            key,
            direct: { status: 'available' },
            agent: { status: 'partial' },
          })),
        },
      }
    if (url.pathname.endsWith(`/pods/${podName}/detail`))
      return {
        body: {
          data: {
            name: podName,
            namespace: 'monitoring',
            phase: 'Running',
            nodeName: 'worker-a',
            podIp: '10.0.0.2',
            hostIp: '10.1.0.2',
            qosClass: 'Burstable',
            serviceAccountName: 'runner',
            createdAt: '2026-10-30T10:00:00Z',
            startedAt: '2026-10-30T10:01:00Z',
            ageSeconds: 3600,
            allowedActions: ['get', 'logs', 'exec'],
            requests: { cpu: '100m', memory: '128Mi' },
            limits: { cpu: '1', memory: '512Mi' },
            labels: { app: 'api' },
            annotations: { checksum: 'abc' },
            containers: [
              {
                name: 'app',
                role: 'main',
                image: 'registry/api:v1',
                state: 'running',
                ready: true,
                restartCount: 2,
              },
              {
                name: 'migrate',
                role: 'init',
                image: 'registry/migrate:v1',
                state: 'terminated',
                reason: 'Completed',
                ready: false,
                exitCode: 0,
              },
            ],
            conditions: [
              { type: 'Ready', status: 'True', lastTransitionTime: '2026-10-30T10:01:00Z' },
            ],
            volumes: [
              {
                name: 'config',
                type: 'ConfigMap',
                sourceName: 'api-config',
                volumeMounts: [{ name: 'app', mountPath: '/etc/config', readOnly: true }],
              },
            ],
            relatedResources: [
              { kind: 'Deployment', name: 'api', namespace: 'monitoring', relations: ['owner'] },
              {
                kind: 'ConfigMap',
                name: 'api-config',
                namespace: 'monitoring',
                relations: ['volume'],
                details: ['Mount /etc/config'],
              },
              {
                kind: 'ServiceAccount',
                name: 'runner',
                namespace: 'monitoring',
                relations: ['service-account'],
              },
            ],
          },
        },
      }
    if (url.pathname.endsWith(`/pods/${podName}/metrics`)) {
      state.ranges.push(url.searchParams.get('rangeMinutes') || '')
      return {
        body: {
          data: {
            configured: true,
            resourceKind: 'Pod',
            resourceName: podName,
            source: 'prometheus',
            rangeMinutes: 60,
            stepSeconds: 60,
            generatedAt: '2026-10-30T10:02:00Z',
            series: [
              ['cpu', 'cores', 0.004],
              ['memory', 'bytes', 64000000],
              ['network_rx', 'bytes/s', 2048],
              ['network_tx', 'bytes/s', 1024],
              ['restarts', 'count', 2],
            ].map(([key, unit, value]) => ({
              key,
              label: key,
              unit,
              latest: value,
              points: [
                { timestamp: '2026-10-30T10:01:00Z', value },
                { timestamp: '2026-10-30T10:02:00Z', value },
              ],
            })),
          },
        },
      }
    }
    if (url.pathname === '/api/v1/clusters/cluster-a/events')
      return {
        body: {
          data: [
            {
              name: 'event-1',
              namespace: 'monitoring',
              involvedName: podName,
              type: 'Warning',
              reason: 'BackOff',
              message: 'Back-off restarting failed container',
              count: 7,
              ageSeconds: 60,
              lastTimestamp: '2026-10-30T10:02:00Z',
            },
          ],
        },
      }
    if (url.pathname.endsWith(`/pods/${podName}/logs`))
      return {
        body: {
          data: {
            podName,
            namespace: 'monitoring',
            container: 'app',
            content: Array.from({ length: 600 }, (_, n) => `Synthetic log line ${n}`).join('\n'),
            contentBytes: 19,
            truncated: false,
          },
        },
      }
    if (url.pathname.endsWith(`/pods/${podName}/yaml`))
      return {
        body: {
          data: {
            yaml: `apiVersion: v1\nkind: Pod\nmetadata:\n  name: ${podName}\n`,
            resourceVersion: '1',
          },
        },
      }
    if (url.pathname === '/api/v1/clusters/cluster-a/resources/graph') {
      state.graphReads += 1
      return {
        body: {
          data: {
            rootId: 'pod-a',
            clusterId: 'cluster-a',
            generatedAt: '2026-10-30T10:00:00Z',
            warnings: [],
            evidence: [],
            nodes: [
              ['deployment', 'Deployment', 'api'],
              ['replicaset', 'ReplicaSet', 'api-7d9'],
              ['other-rs', 'ReplicaSet', 'api-old'],
              ['unrelated-rs', 'ReplicaSet', 'unrelated'],
              ['pod-a', 'Pod', podName],
              ['config', 'ConfigMap', 'api-config'],
              ['secret', 'Secret', 'api-secret'],
            ].map(([id, kind, name]) => ({
              id,
              resource: {
                clusterId: 'cluster-a',
                apiVersion: 'v1',
                scopeMode: 'namespace',
                namespace: 'monitoring',
                kind,
                name,
              },
            })),
            edges: [
              { id: 'owns-rs', sourceId: 'deployment', targetId: 'replicaset', relation: 'owns' },
              { id: 'owns-other', sourceId: 'deployment', targetId: 'other-rs', relation: 'owns' },
              { id: 'owns-pod', sourceId: 'replicaset', targetId: 'pod-a', relation: 'owns' },
              { id: 'uses-config', sourceId: 'pod-a', targetId: 'config', relation: 'uses-config' },
              { id: 'uses-secret', sourceId: 'pod-a', targetId: 'secret', relation: 'uses-secret' },
            ],
          },
        },
      }
    }
    if (url.pathname === '/api/v1/auth/stream-ticket' && method === 'POST') {
      if (request.postDataJSON()?.path?.endsWith('/terminal')) {
        state.terminalTickets += 1
        return { body: { data: { ticket: 'synthetic-ticket', expiresAt: '2099-01-01T00:00:00Z' } } }
      }
      return {
        status: 503,
        body: { error: { code: 'unavailable', message: 'Synthetic polling mode' } },
      }
    }
    return undefined
  })
  return { state, evidence }
}

test('Pod detail connects overview, init containers, six metric cards and scoped storage', async ({
  context,
  page,
}) => {
  const { state, evidence } = await setup(context, page)
  await page.goto(path)
  await expect(page.getByRole('heading', { name: podName, exact: true })).toBeVisible()
  const overview = page.getByRole('tabpanel', { name: '概览', exact: true })
  await expect(overview.getByText('Pod 运行正常', { exact: true })).toBeVisible()
  const containers = overview.locator('.soha-pod-detail-container-label')
  await expect(containers).toHaveCount(2)
  await expect(containers.nth(0)).toContainText('migrate')
  await expect(overview.locator('.ant-pagination')).toHaveCount(0)
  await expect(overview.getByRole('link', { name: 'runner', exact: true })).toHaveAttribute(
    'href',
    /clusterId=cluster-a/,
  )
  await page.getByRole('tab', { name: '指标', exact: true }).click()
  await expect(page.locator('.soha-pod-detail-metric-card')).toHaveCount(6)
  await expect(page.locator('.soha-pod-detail-metric-card').first()).toContainText('4 mCPU')
  await expect(page.locator('.soha-pod-detail-metric-missing')).toHaveCount(2)
  await page
    .getByRole('radiogroup', { name: '指标时间范围' })
    .getByText('15 分钟', { exact: true })
    .click()
  await expect(page).toHaveURL(/range=15/)
  await expect.poll(() => state.ranges).toContain('15')
  await page.getByRole('tab', { name: '存储与挂载', exact: true }).click()
  await expect(page.getByRole('tabpanel', { name: '存储与挂载' })).toContainText('/etc/config')
  await expect(
    page.getByRole('tabpanel', { name: '存储与挂载' }).locator('.ant-pagination'),
  ).toHaveCount(0)
  await page.getByRole('tab', { name: '概览', exact: true }).click()
  await page.setViewportSize({ width: 900, height: 1000 })
  expect(
    await page.locator('.soha-pod-detail').evaluate((el) => el.scrollWidth - el.clientWidth),
  ).toBeLessThanOrEqual(1)
  verifyBrowserEvidence(evidence)
})

test('Pod event timeline preserves actual repetition and filters returned events', async ({
  context,
  page,
}) => {
  const { evidence } = await setup(context, page)
  await page.goto(`${path}&tab=events`)
  const events = page.getByRole('tabpanel', { name: '事件', exact: true })
  await expect(events).toContainText('BackOff')
  await expect(events).toContainText('7 次')
  await expect(events.locator('time')).toHaveAttribute('datetime', '2026-10-30T10:02:00.000Z')
  await events.getByPlaceholder('搜索原因、消息或对象').fill('not-found')
  await expect(events).toContainText('没有匹配')
  await expect(events.locator('.ant-pagination')).toHaveCount(0)
  verifyBrowserEvidence(evidence)
})

test('Pod terminal stays disconnected until explicit connect', async ({ context, page }) => {
  const { state, evidence } = await setup(context, page)
  await page.goto(`${path}&tab=terminal&container=app`)
  await expect(page.getByRole('button', { name: '连接终端', exact: true })).toBeVisible()
  expect(state.terminalTickets).toBe(0)
  await page.getByRole('button', { name: '连接终端', exact: true }).click()
  await expect.poll(() => state.terminalTickets).toBe(1)
  await expect(page.locator('.soha-terminal-shell')).toBeVisible()
  const terminalHeight = await page
    .locator('.soha-terminal-shell')
    .evaluate((el) => el.getBoundingClientRect().height)
  expect(terminalHeight).toBeGreaterThan(300)
  expect(terminalHeight).toBeLessThan(900)
  await page.getByRole('tab', { name: '概览', exact: true }).click()
  await expect(page.locator('.soha-terminal-shell')).toHaveCount(0)
  verifyBrowserEvidence(evidence)
})

test('Pod detail does not expose logs or exec without page permission', async ({
  context,
  page,
}) => {
  const { state, evidence } = await setup(context, page, false)
  await page.goto(`${path}&tab=terminal`)
  await expect(page.getByRole('tab', { name: '概览', exact: true })).toHaveAttribute(
    'aria-selected',
    'true',
  )
  await expect(page.getByRole('tab', { name: '终端', exact: true })).toHaveCount(0)
  await expect(page.getByRole('tab', { name: '日志', exact: true })).toHaveCount(0)
  expect(state.terminalTickets).toBe(0)
  verifyBrowserEvidence(evidence)
})

test('Pod logs fill available height and remain bounded during window resize', async ({
  context,
  page,
}) => {
  const { evidence } = await setup(context, page)
  await page.goto(`${path}&tab=logs`)
  const shell = page.locator('.soha-pod-detail .soha-log-shell')
  await expect(shell).toBeVisible()
  const height = () => shell.evaluate((el) => el.getBoundingClientRect().height)
  await expect.poll(height).toBeGreaterThan(400)
  expect(await height()).toBeLessThan(850)
  const initial = await height()
  await page.setViewportSize({ width: 1440, height: 1400 })
  await expect.poll(height).toBeGreaterThan(initial + 300)
  await page.setViewportSize({ width: 1440, height: 900 })
  await expect.poll(height).toBeLessThan(initial)
  expect(await shell.locator('.soha-log-row').count()).toBeLessThan(100)
  verifyBrowserEvidence(evidence)
})

test('Pod relationships follow the approved owner, scheduling and reference layout', async ({
  context,
  page,
}, testInfo) => {
  const { state, evidence } = await setup(context, page)
  await page.goto(`${path}&tab=related-resources`)
  const board = page.getByRole('group', { name: '当前 Pod 资源关系图', exact: true })
  const root = board.getByRole('button', { name: `Pod ${podName}`, exact: true })
  const deployment = board.getByRole('button', { name: 'Deployment api', exact: true })
  const rs = board.getByRole('button', { name: 'ReplicaSet api-7d9', exact: true })
  const node = board.getByRole('button', { name: 'Node worker-a', exact: true })
  const account = board.getByRole('button', { name: 'ServiceAccount runner', exact: true })
  await expect(rs).toBeVisible()
  const rb = (await root.boundingBox())!
  const nb = (await node.boundingBox())!
  expect((await deployment.boundingBox())!.y).toBeLessThan(rb.y)
  expect((await rs.boundingBox())!.y).toBeLessThan(rb.y)
  expect(nb.x + nb.width).toBeLessThan(rb.x)
  expect(Math.abs(nb.y + nb.height / 2 - rb.y - rb.height / 2)).toBeLessThan(3)
  expect((await account.boundingBox())!.y).toBeGreaterThan(rb.y + rb.height)
  await expect(board.getByRole('button', { name: 'ReplicaSet api-old', exact: true })).toHaveCount(
    0,
  )
  await page.getByRole('switch', { name: '其他 ReplicaSets', exact: true }).click()
  await expect(board.getByRole('button', { name: 'ReplicaSet api-old', exact: true })).toBeVisible()
  await expect(
    board.getByRole('button', { name: 'ReplicaSet unrelated', exact: true }),
  ).toHaveCount(0)
  await node.focus()
  await node.press('Enter')
  await expect(page.locator('.soha-pod-relation-inspector')).toContainText('worker-a')
  await page.getByRole('button', { name: '关闭资源信息', exact: true }).click()
  await expect(node).toBeFocused()
  await board.getByRole('button', { name: 'ConfigMap api-config', exact: true }).click()
  await expect(page.locator('.soha-pod-relation-inspector')).toContainText('Mount /etc/config')
  await page
    .getByRole('radiogroup', { name: '关联资源视图' })
    .getByText('列表', { exact: true })
    .click()
  await page.getByRole('button', { name: '关闭资源信息', exact: true }).click()
  await expect(page.getByRole('group', { name: '关联资源视图切换', exact: true })).toBeFocused()
  await expect(page.locator('.soha-pod-relations-card')).toContainText('ServiceAccount')
  await expect(page.locator('.soha-pod-relations-card .ant-pagination')).toHaveCount(0)
  expect(state.graphReads).toBe(1)
  await page
    .getByRole('radiogroup', { name: '关联资源视图' })
    .getByText('关系图', { exact: true })
    .click()
  await page.getByLabel('切换到深色模式').click()
  await page.setViewportSize({ width: 900, height: 1100 })
  expect(
    await page.locator('.soha-pod-detail').evaluate((el) => el.scrollWidth - el.clientWidth),
  ).toBeLessThanOrEqual(1)
  await page.screenshot({
    path: testInfo.outputPath('pod-relations-dark.png'),
    fullPage: true,
    animations: 'disabled',
  })
  verifyBrowserEvidence(evidence)
})
