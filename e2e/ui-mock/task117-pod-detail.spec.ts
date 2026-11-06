import { test, expect, type BrowserContext, type Page } from '@playwright/test'
import { stringify } from 'yaml'
import { baselineResponse } from '../fixtures/baseline-data'
import { installMocks, verifyBrowserEvidence } from '../fixtures/mock-app'
import { expectWorkloadDetailFrame } from '../fixtures/workload-detail-geometry'

const podName = 'api-7d9-with-a-long-pod-name'
const path = `/workloads/pods/${podName}?clusterId=cluster-a&namespace=monitoring`
async function setup(context: BrowserContext, page: Page, sessions = true) {
  const state = {
    terminalTickets: 0,
    ranges: [] as string[],
    graphReads: 0,
    detailReads: 0,
    allowedActions: ['get', 'logs', 'exec', 'update'],
    metricsConfigured: true,
    omitOwnerGraph: false,
    yamlReads: 0,
    yamlFault: '' as '' | 'read' | 'identity',
    containerArgument: '--config=/etc/api/config.yaml',
    referencePermissions: true,
    deletePermission: false,
    deleteStatus: 200,
    deleted: false,
    deletes: [] as string[],
    mutationUnavailable: false,
    connectionMode: 'direct_kubeconfig',
    podUid: 'pod-uid',
  }
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
        'platform.pods.update',
        'platform.nodes.view',
        'platform.namespaces.view',
        ...(state.deletePermission ? ['platform.pods.delete'] : []),
        ...(state.referencePermissions
          ? [
              'platform.configuration.config-maps.view',
              'platform.configuration.secrets.view',
              'platform.storage.persistent-volume-claims.view',
            ]
          : []),
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
              connectionMode: state.connectionMode,
              environment: 'testing',
              health: { status: 'healthy' },
            },
          ],
        },
      }
    if (url.pathname === '/api/v1/clusters/capabilities')
      return {
        body: {
          data: [
            'pod.logs',
            'pod.exec',
            'workload.mutations',
            'resource.yaml.view',
            'resource.yaml.apply',
          ].map((key) => ({
            key,
            direct: {
              status:
                key === 'workload.mutations' && state.mutationUnavailable
                  ? 'unsupported'
                  : 'available',
            },
            agent: { status: 'partial' },
          })),
        },
      }
    if (url.pathname.endsWith(`/pods/${podName}`) && method === 'DELETE') {
      state.deletes.push(url.pathname + url.search)
      if (state.deleteStatus !== 200)
        return {
          status: state.deleteStatus,
          body: { error: { code: 'conflict', message: 'Synthetic Pod delete rejected' } },
        }
      state.deleted = true
      return { body: { data: null } }
    }
    if (url.pathname === '/api/v1/clusters/cluster-a/workloads/pods' && method === 'GET')
      return { body: { data: [] } }
    if (url.pathname.endsWith(`/pods/${podName}/detail`)) {
      state.detailReads += 1
      if (state.deleted)
        return {
          status: 404,
          body: { error: { code: 'not_found', message: 'Synthetic Pod deleted' } },
        }
      return {
        body: {
          data: {
            name: podName,
            namespace: 'monitoring',
            uid: state.podUid,
            phase: 'Running',
            nodeName: 'worker-a',
            podIp: '10.0.0.2',
            hostIp: '10.1.0.2',
            qosClass: 'Burstable',
            serviceAccountName: 'runner',
            createdAt: '2026-10-30T10:00:00Z',
            startedAt: '2026-10-30T10:01:00Z',
            ageSeconds: 3600,
            allowedActions: state.allowedActions,
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
                state: 'terminated:Completed',
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
                name: 'persistent-data-with-a-long-volume-name',
                type: 'PersistentVolumeClaim',
                sourceName: `data-${'long-claim-'.repeat(6)}0`,
                readOnly: false,
                volumeMounts: [{ name: 'app', mountPath: '/var/lib/data', readOnly: false }],
              },
              {
                name: 'config',
                type: 'ConfigMap',
                sourceName: 'api-config',
                volumeMounts: [
                  { name: 'app', mountPath: '/etc/config', subPath: 'settings', readOnly: true },
                ],
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
    }
    if (url.pathname.endsWith(`/pods/${podName}/metrics`)) {
      state.ranges.push(url.searchParams.get('rangeMinutes') || '')
      return {
        body: {
          data: {
            configured: state.metricsConfigured,
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
    if (url.pathname.endsWith(`/pods/${podName}/yaml`)) {
      state.yamlReads += 1
      if (state.yamlFault === 'read')
        return {
          status: 503,
          body: { error: { code: 'unavailable', message: 'Synthetic configuration read failed' } },
        }
      return {
        body: {
          data: {
            content: stringify({
              apiVersion: 'v1',
              kind: 'Pod',
              metadata: {
                name: state.yamlFault === 'identity' ? 'another-pod' : podName,
                namespace: 'monitoring',
              },
              spec: {
                initContainers: [
                  {
                    name: 'migrate',
                    imagePullPolicy: 'IfNotPresent',
                    command: ['/bin/sh', '-c'],
                    args: ['echo init'],
                  },
                ],
                containers: [
                  {
                    name: 'app',
                    imagePullPolicy: 'Always',
                    command: ['/usr/bin/api'],
                    args: [state.containerArgument],
                    resources: {
                      requests: { cpu: '100m', memory: '128Mi' },
                      limits: { cpu: '500m', memory: '256Mi' },
                    },
                    env: [
                      { name: 'LOG_LEVEL', value: 'info' },
                      {
                        name: 'API_PASSWORD',
                        valueFrom: { secretKeyRef: { name: 'app-secrets', key: 'password' } },
                      },
                      { name: 'POD_NAME', valueFrom: { fieldRef: { fieldPath: 'metadata.name' } } },
                    ],
                    envFrom: [{ configMapRef: { name: 'app-config' }, prefix: 'APP_' }],
                    ports: [{ name: 'http', containerPort: 8080, protocol: 'TCP' }],
                    volumeMounts: [
                      {
                        name: 'config',
                        mountPath: '/etc/config',
                        subPath: 'settings',
                        readOnly: true,
                      },
                    ],
                    livenessProbe: {
                      httpGet: { path: '/health', port: 8080 },
                      initialDelaySeconds: 0,
                      periodSeconds: 10,
                      timeoutSeconds: 1,
                    },
                    readinessProbe: { tcpSocket: { port: 8080 } },
                    startupProbe: {
                      exec: { command: ['test', '-f', '/ready'] },
                      failureThreshold: 30,
                    },
                  },
                ],
              },
              status: {
                initContainerStatuses: [
                  {
                    name: 'migrate',
                    state: {
                      terminated: {
                        exitCode: 0,
                        reason: 'Completed',
                        startedAt: '2026-10-30T10:00:00Z',
                        finishedAt: '2026-10-30T10:01:00Z',
                      },
                    },
                  },
                ],
                containerStatuses: [
                  {
                    name: 'app',
                    state: { running: { startedAt: '2026-10-30T10:01:00Z' } },
                    lastState: {
                      terminated: {
                        exitCode: 137,
                        signal: 9,
                        reason: 'OOMKilled',
                        message: 'Synthetic memory limit exceeded',
                        startedAt: '2026-10-30T09:00:00Z',
                        finishedAt: '2026-10-30T09:59:00Z',
                      },
                    },
                  },
                ],
              },
            }),
            resourceVersion: '1',
          },
        },
      }
    }
    if (
      url.pathname === '/api/v1/clusters/cluster-a/resources/graph' &&
      method === 'GET' &&
      url.searchParams.get('namespace') === 'monitoring' &&
      url.searchParams.get('kind') === 'Pod' &&
      url.searchParams.get('name') === podName
    ) {
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
              ['node', 'Node', 'worker-a'],
              ['account', 'ServiceAccount', 'runner'],
            ]
              .filter(
                ([id]) =>
                  !state.omitOwnerGraph ||
                  !['deployment', 'replicaset', 'other-rs', 'unrelated-rs'].includes(id),
              )
              .map(([id, kind, name]) => ({
                id,
                resource: {
                  clusterId: 'cluster-a',
                  apiVersion: ['Deployment', 'StatefulSet', 'DaemonSet', 'ReplicaSet'].includes(
                    kind,
                  )
                    ? 'apps/v1'
                    : ['Job', 'CronJob'].includes(kind)
                      ? 'batch/v1'
                      : 'v1',
                  scopeMode: kind === 'Node' ? 'cluster' : 'namespace',
                  namespace: kind === 'Node' ? undefined : 'monitoring',
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
              { id: 'scheduled', sourceId: 'pod-a', targetId: 'node', relation: 'scheduled-on' },
              {
                id: 'account-use',
                sourceId: 'pod-a',
                targetId: 'account',
                relation: 'uses-service-account',
              },
            ].filter((edge) => !state.omitOwnerGraph || edge.relation !== 'owns'),
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
  await expectWorkloadDetailFrame(page)
  await expect(page.locator('.soha-workload-detail-header')).not.toContainText('读取于')
  await expect(
    page.locator('.soha-workload-detail-header').getByRole('switch', { name: /自动刷新/ }),
  ).toBeChecked()
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
  const board = page.getByRole('tabpanel', { name: '关联资源', exact: true })
  const root = board.locator('.react-flow__node[data-id="pod-a"]')
  const node = board.locator('.react-flow__node[data-id="node"]')
  const info = board.getByRole('complementary', { name: '选中资源信息' })
  await expect(root).toBeVisible()
  await expect(root).toHaveClass(/is-root/)
  await expect(board.locator('.react-flow__node[data-id="deployment"]')).toBeVisible()
  await expect(board.locator('.react-flow__node[data-id="replicaset"]')).toBeVisible()
  await expect(board.locator('.react-flow__node[data-id="account"]')).toBeVisible()
  await expect(board.locator('.react-flow__edge[data-id="scheduled"]')).toHaveCount(1)
  await expect(board.locator('.react-flow__node[data-id="other-rs"]')).toHaveCount(0)
  await page.getByRole('switch', { name: '包含其他 ReplicaSets', exact: true }).click()
  await expect(board.locator('.react-flow__node[data-id="other-rs"]')).toBeVisible()
  await expect(board.locator('.react-flow__node[data-id="unrelated-rs"]')).toBeVisible()
  await node.focus()
  await node.press('Enter')
  await expect(info).toContainText('worker-a')
  await expect(info).toContainText('承载')
  await page.getByRole('button', { name: '关闭资源信息', exact: true }).click()
  await expect(node).toBeFocused()
  await board.locator('.react-flow__node[data-id="config"]').click()
  await expect(info).toContainText('被引用为配置')
  await page
    .getByRole('radiogroup', { name: '关联资源视图' })
    .getByText('列表', { exact: true })
    .click()
  await page.getByRole('button', { name: '关闭资源信息', exact: true }).click()
  // The selected resource remains available in the new list view after the graph unmounts.
  await expect(board.locator('button[data-resource-id="config"]')).toBeFocused()
  await expect(board).toContainText('ServiceAccount')
  await expect(board.locator('.ant-pagination')).toHaveCount(0)
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

test('Pod terminal requires reconnect after changing container and stops after policy is revoked', async ({
  context,
  page,
}) => {
  const { state, evidence } = await setup(context, page)
  await page.goto(`${path}&tab=terminal&container=app`)
  await page.getByRole('button', { name: '连接终端', exact: true }).click()
  await expect.poll(() => state.terminalTickets).toBe(1)
  const header = page.locator('.soha-workload-detail-header')
  const reads = state.detailReads
  await header.getByRole('button', { name: '刷新', exact: true }).click()
  await expect.poll(() => state.detailReads).toBeGreaterThan(reads)
  await expect(page.locator('.soha-terminal-shell')).toBeVisible()
  expect(state.terminalTickets).toBe(1)
  await page.getByRole('combobox', { name: '选择容器' }).click()
  await page
    .locator('.ant-select-item-option-content:visible')
    .filter({ hasText: /^migrate$/ })
    .click()
  await expect(page.getByRole('button', { name: '连接终端', exact: true })).toBeDisabled()
  await expect(page.locator('.soha-terminal-shell')).toHaveCount(0)
  await page.getByRole('combobox', { name: '选择容器' }).click()
  await page.locator('.ant-select-item-option-content:visible').filter({ hasText: /^app$/ }).click()
  await expect(page.getByRole('button', { name: '连接终端', exact: true })).toBeEnabled()
  expect(state.terminalTickets).toBe(1)
  await page.getByRole('button', { name: '连接终端', exact: true }).click()
  await expect.poll(() => state.terminalTickets).toBe(2)
  state.allowedActions = ['get']
  await header.getByRole('button', { name: '刷新', exact: true }).click()
  await expect(page.locator('.soha-terminal-shell')).toHaveCount(0)
  await expect(page.getByRole('tab', { name: '终端', exact: true })).toHaveAttribute(
    'aria-disabled',
    'true',
  )
  await expect(page.getByRole('tab', { name: '概览', exact: true })).toHaveAttribute(
    'aria-selected',
    'true',
  )
  expect(state.terminalTickets).toBe(2)
  state.allowedActions = ['get', 'logs', 'exec']
  await header.getByRole('button', { name: '刷新', exact: true }).click()
  await expect(page.getByRole('button', { name: '连接终端', exact: true })).toBeEnabled()
  expect(state.terminalTickets).toBe(2)
  verifyBrowserEvidence(evidence)
})

test('Pod metrics refresh is local and storage retains source and mount details', async ({
  context,
  page,
}) => {
  const { state, evidence } = await setup(context, page)
  await page.goto(`${path}&tab=metrics&range=15`)
  await expect(page.locator('.soha-pod-detail-metric-card')).toHaveCount(6)
  const reads = state.detailReads
  const samples = state.ranges.length
  await page.getByRole('button', { name: '刷新指标', exact: true }).click()
  await expect.poll(() => state.ranges.length).toBeGreaterThan(samples)
  expect(state.detailReads).toBe(reads)
  await expect(page).toHaveURL(/range=15/)
  state.metricsConfigured = false
  await page.getByRole('button', { name: '刷新指标', exact: true }).click()
  await expect(page.getByRole('tabpanel', { name: '指标' })).toContainText('尚未接入监控')
  await page.getByRole('tab', { name: '存储与挂载', exact: true }).click()
  const storage = page.getByRole('tabpanel', { name: '存储与挂载' })
  await expect(storage).toContainText('2 个卷')
  await expect(storage).toContainText('subPath: settings')
  await expect(storage).toContainText('只读')
  await expect(storage.getByRole('link', { name: 'api-config', exact: true })).toHaveAttribute(
    'href',
    /clusterId=cluster-a&namespace=monitoring/,
  )
  verifyBrowserEvidence(evidence)
})

test('Pod overview and storage keep the detail frame in both themes and narrow views', async ({
  context,
  page,
}, testInfo) => {
  const { evidence } = await setup(context, page)
  await page.goto(path)
  await expect(page.locator('.soha-workload-overview')).toBeVisible()
  for (const theme of ['light', 'dark']) {
    await page.setViewportSize({ width: 1373, height: 1502 })
    if (theme === 'dark') await page.getByLabel('切换到深色模式').click()
    for (const width of [1373, 900, 320]) {
      await page.setViewportSize({ width, height: 1502 })
      for (const label of ['概览', '存储与挂载']) {
        if (width === 320) {
          await page.getByRole('tablist').getByRole('button').click()
          await page
            .getByRole('listbox', { name: 'expanded dropdown' })
            .getByRole('option', { name: label, exact: true })
            .click()
        } else {
          await page.getByRole('tab', { name: label, exact: true }).click()
        }
        expect(
          await page.locator('.soha-pod-detail').evaluate((el) => el.scrollWidth - el.clientWidth),
        ).toBeLessThanOrEqual(1)
        const headerBox = (await page.locator('.soha-workload-detail-header').boundingBox())!
        const bodyBox = (await page
          .getByRole('tabpanel', { name: label, exact: true })
          .boundingBox())!
        if (label === '存储与挂载') {
          const source = page.locator('.soha-pod-storage-source a').filter({ hasText: /^data-/ })
          await expect(source).toHaveCSS('white-space', 'normal')
          expect((await source.boundingBox())!.height).toBeGreaterThan(20)
        }
        expect(Math.abs(headerBox.x - bodyBox.x)).toBeLessThanOrEqual(1)
        expect(
          Math.abs(headerBox.x + headerBox.width - bodyBox.x - bodyBox.width),
        ).toBeLessThanOrEqual(1)
      }
    }
  }
  await page.setViewportSize({ width: 1373, height: 1100 })
  await page.getByRole('tab', { name: '概览', exact: true }).click()
  await page.screenshot({
    path: testInfo.outputPath('pods-overview-dark-candidate.png'),
    animations: 'disabled',
  })
  verifyBrowserEvidence(evidence)
})

test('Pod YAML draft survives detail refresh and returning from another tab', async ({
  context,
  page,
}) => {
  const { state, evidence } = await setup(context, page)
  await page.goto(`${path}&tab=yaml`)
  const editor = page.getByRole('textbox', { name: 'YAML 草稿编辑器', exact: true })
  const lines = page.locator('.soha-k8s-yaml-editor .view-lines:visible').first()
  await expect(lines).toContainText('apiVersion')
  await editor.press('ControlOrMeta+Home')
  await editor.pressSequentially('# task117-pods-draft')
  await editor.press('Enter')
  const reads = state.detailReads
  await page
    .locator('.soha-workload-detail-header')
    .getByRole('button', { name: '刷新', exact: true })
    .click()
  await expect.poll(() => state.detailReads).toBeGreaterThan(reads)
  await expect(lines).toContainText('task117-pods-draft')
  await page.getByRole('tab', { name: '存储与挂载', exact: true }).click()
  await page.getByRole('tab', { name: 'YAML', exact: true }).click()
  await expect(lines).toContainText('task117-pods-draft')
  expect(state.terminalTickets).toBe(0)
  verifyBrowserEvidence(evidence)
})

test('Pod relationships do not invent owner nodes or edges omitted by graph collection', async ({
  context,
  page,
}) => {
  const { state, evidence } = await setup(context, page)
  state.omitOwnerGraph = true
  await page.goto(`${path}&tab=related-resources`)
  const board = page.getByRole('tabpanel', { name: '关联资源', exact: true })
  await expect(board.locator('.react-flow__node[data-id="pod-a"]')).toBeVisible()
  await expect(board.locator('.react-flow__node[data-id="deployment"]')).toHaveCount(0)
  await expect(board.locator('.react-flow__node[data-id="replicaset"]')).toHaveCount(0)
  await expect(board.locator('.react-flow__edge[data-id="owns-pod"]')).toHaveCount(0)
  await board.locator('.react-flow__node[data-id="pod-a"]').click()
  const info = board.getByRole('complementary', { name: '选中资源信息' })
  await expect(info).toContainText('引用配置')
  await expect(info).not.toContainText('Deployment api')
  verifyBrowserEvidence(evidence)
})

test('Pod expanded containers share a server configuration read and retain exit records on refresh', async ({
  context,
  page,
}, testInfo) => {
  const { state, evidence } = await setup(context, page)
  await page.goto(path)
  await expect(page.locator('.soha-pod-container-collapse')).toBeVisible()
  await page.getByRole('switch', { name: /^自动刷新/ }).click()
  expect(state.yamlReads).toBe(0)
  const migrate = page
    .locator('.soha-pod-container-collapse .ant-collapse-item')
    .filter({ has: page.getByRole('button', { name: /^migrate 初始化容器/ }) })
  await migrate.getByRole('button').first().focus()
  await migrate.getByRole('button').first().press('Enter')
  await expect(migrate).toContainText('Exit Code 0')
  const app = page
    .locator('.soha-pod-container-collapse .ant-collapse-item')
    .filter({ has: page.getByRole('button', { name: /^app 常规容器/ }) })
  await app.getByRole('button').first().click()
  const details = app.locator('.soha-pod-container-panel')
  await expect(details).toBeVisible()
  expect(await details.evaluate((el) => el.scrollHeight - el.clientHeight)).toBeLessThanOrEqual(1)
  await expect(details).toContainText('OOMKilled')
  await expect(details).toContainText('Exit Code 137')
  await expect(details).toContainText('Signal 9')
  await expect(details).toContainText('100m')
  await expect(details).toContainText('256Mi')
  await expect(details).toContainText('/usr/bin/api')
  await expect(details).toContainText(state.containerArgument)
  await expect(details).toContainText('API_PASSWORD')
  await expect(details).toContainText('password')
  await expect(details.getByRole('link', { name: 'app-secrets', exact: true })).toHaveAttribute(
    'href',
    /clusterId=cluster-a&namespace=monitoring/,
  )
  await expect(details).toContainText('prefix: APP_')
  await expect(details).toContainText('subPath: settings')
  await expect(details).toContainText('只读')
  await expect(details).toContainText('8080/TCP')
  for (const label of ['存活探针', '就绪探针', '启动探针'])
    await expect(details).toContainText(label)
  expect(state.yamlReads).toBe(1)
  state.containerArgument = `--config=/etc/api/${'long-path-'.repeat(30)}config.yaml`
  await page.getByRole('button', { name: '刷新容器详情', exact: true }).click()
  await expect(details).toContainText(state.containerArgument)
  await expect(migrate).toContainText('Exit Code 0')
  expect(state.yamlReads).toBe(2)
  expect(state.terminalTickets).toBe(0)
  for (const theme of ['light', 'dark']) {
    if (theme === 'dark') await page.getByLabel('切换到深色模式').click()
    for (const width of [1373, 900, 320]) {
      await page.setViewportSize({ width, height: 1502 })
      expect(
        await page.locator('.soha-pod-detail').evaluate((el) => el.scrollWidth - el.clientWidth),
      ).toBeLessThanOrEqual(1)
      await expect(details).toContainText('Exit Code 137')
    }
  }
  await page.setViewportSize({ width: 1373, height: 1502 })
  await details.screenshot({
    path: testInfo.outputPath('pod-container-details-dark.png'),
    animations: 'disabled',
  })
  verifyBrowserEvidence(evidence)
})

test('Pod container details keep read errors recoverable and gate reference navigation', async ({
  context,
  page,
}) => {
  const { state, evidence } = await setup(context, page)
  state.referencePermissions = false
  state.yamlFault = 'read'
  await page.goto(path)
  await page.getByRole('switch', { name: /^自动刷新/ }).click()
  const app = page
    .locator('.soha-pod-container-collapse .ant-collapse-item')
    .filter({ has: page.getByRole('button', { name: /^app 常规容器/ }) })
  await app.getByRole('button').first().click()
  await expect(page.getByText('容器配置与退出记录暂不可用', { exact: true })).toBeVisible()
  await expect(app).not.toContainText('未声明独立环境变量')
  state.yamlFault = ''
  await page.getByRole('button', { name: '刷新容器详情', exact: true }).click()
  await expect(app).toContainText('Exit Code 137')
  await expect(app).toContainText('app-secrets')
  await expect(app.getByRole('link', { name: 'app-secrets', exact: true })).toHaveCount(0)
  await expect(app.getByRole('link', { name: 'app-config', exact: true })).toHaveCount(0)
  state.yamlFault = 'identity'
  state.containerArgument = '--rejected-identity-value'
  await page.getByRole('button', { name: '刷新容器详情', exact: true }).click()
  await expect(page.getByRole('alert')).toContainText('Pod YAML identity does not match')
  await expect(app).toContainText('Exit Code 137')
  await expect(app).not.toContainText(state.containerArgument)
  verifyBrowserEvidence(evidence)
})

test('Pod header deletion confirms the scoped target, keeps errors and returns to its list', async ({
  context,
  page,
}, testInfo) => {
  const { state, evidence } = await setup(context, page)
  state.deletePermission = true
  state.allowedActions.push('delete')
  await page.goto(path)
  const header = page.locator('.soha-workload-detail-header')
  const remove = header.getByRole('button', { name: '删除', exact: true })
  await expect(remove).toBeEnabled()
  for (const width of [1373, 900, 320]) {
    await page.setViewportSize({ width, height: 1000 })
    await expect(remove).toBeVisible()
    await expect
      .poll(() => header.evaluate((el) => el.scrollWidth - el.clientWidth))
      .toBeLessThanOrEqual(1)
  }
  await page.setViewportSize({ width: 1373, height: 1000 })
  await page.getByLabel('切换到深色模式').click()
  await remove.click()
  const dialog = page.getByRole('dialog', { name: `确认删除 Pod ${podName}？` })
  await expect(dialog).toContainText(`cluster-a / monitoring / ${podName}`)
  await expect(dialog).toContainText('受控制器管理的 Pod 可能会自动创建替代实例')
  expect(state.deletes).toHaveLength(0)
  await dialog.getByRole('button', { name: '取消', exact: true }).click()
  await expect(dialog).not.toBeVisible()
  expect(state.deletes).toHaveLength(0)
  await page.screenshot({
    path: testInfo.outputPath('pod-header-delete-dark.png'),
    animations: 'disabled',
  })
  state.deleteStatus = 409
  await remove.click()
  await dialog.getByRole('button', { name: '删除', exact: true }).click()
  await expect(dialog).toContainText('Synthetic Pod delete rejected')
  await expect(dialog.getByRole('button', { name: '删除', exact: true })).toBeEnabled()
  expect(state.deletes).toEqual([
    `/api/v1/clusters/cluster-a/workloads/pods/${podName}?namespace=monitoring&expectedUid=pod-uid`,
  ])
  state.deleteStatus = 200
  await dialog.getByRole('button', { name: '删除', exact: true }).dblclick()
  await expect(page).toHaveURL('/workloads/pods?clusterId=cluster-a&namespace=monitoring')
  expect(state.deletes).toHaveLength(2)
  verifyBrowserEvidence(evidence)
})

test('Pod header deletion requires permission, allowed action and supported cluster capability', async ({
  context,
  page,
}) => {
  await page.clock.install()
  const { state, evidence } = await setup(context, page)
  state.allowedActions.push('delete')
  await page.goto(path)
  const header = page.locator('.soha-workload-detail-header')
  const remove = header.getByRole('button', { name: '删除', exact: true })
  await expect(header).toBeVisible()
  await expect(remove).toHaveCount(0)
  state.deletePermission = true
  state.allowedActions = ['get']
  await page.reload()
  await expect(header).toBeVisible()
  await expect(remove).toHaveCount(0)
  state.allowedActions.push('delete')
  state.mutationUnavailable = true
  await page.reload()
  await expect(remove).toBeDisabled()
  state.mutationUnavailable = false
  state.connectionMode = 'agent'
  await page.reload()
  await expect(remove).toBeDisabled()
  state.connectionMode = 'direct_kubeconfig'
  await page.reload()
  await expect(remove).toBeEnabled()
  await remove.click()
  const dialog = page.getByRole('dialog', { name: `确认删除 Pod ${podName}？` })
  state.allowedActions = ['get']
  const reads = state.detailReads
  await page.clock.runFor(15_100)
  await expect.poll(() => state.detailReads).toBeGreaterThan(reads)
  await expect(dialog.getByRole('button', { name: '删除', exact: true })).toBeDisabled()
  await expect(dialog).toContainText('资源或操作权限已变化')
  expect(state.deletes).toHaveLength(0)
  await dialog.getByRole('button', { name: /^取\s*消$/, exact: true }).click()
  state.allowedActions.push('delete')
  await page.clock.runFor(15_100)
  await expect(remove).toBeEnabled()
  await remove.click()
  await expect(dialog).toBeVisible()
  state.podUid = 'replacement-pod-uid'
  await page.clock.runFor(15_100)
  await expect(dialog).not.toBeVisible()
  expect(state.deletes).toHaveLength(0)
  verifyBrowserEvidence(evidence)
})
