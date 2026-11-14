import { test, expect, type BrowserContext, type Page } from '@playwright/test'
import { installMocks, verifyBrowserEvidence } from '../fixtures/mock-app'
import { verifyGeometry } from '../fixtures/geometry'

const pagePath = '/compute/virtualization/vms/vm-task52-demo'
const apiPath = '/api/v1/virtualization/vms/vm-task52-demo'

test.use({ video: { mode: 'on', size: { width: 1440, height: 1000 } } })

async function vmMocks(context: BrowserContext, page: Page, readonly = false, canViewTasks = true) {
  const state = {
    name: 'build-worker-01',
    design: false,
    listStartAllowed: false,
    power: 'stopped',
    stale: false,
    operation: '',
    action: 'start',
    detailFails: false,
    metricsFails: false,
    metricsEmpty: false,
    metricsInvalid: false,
    submitFails: false,
    detailReads: 0,
    metricsReads: [] as number[],
    consoleReads: 0,
    writes: [] as string[],
    updatedAt: new Date().toISOString(),
    completedAt: '',
  }
  const operation = () => ({
    id: 'op-task52-demo',
    type: 'vm_action',
    action: state.action,
    vmId: 'vm-task52-demo',
    status: state.operation,
    createdAt: state.updatedAt,
    updatedAt: state.completedAt || state.updatedAt,
    completedAt: state.completedAt || undefined,
    message:
      state.operation === 'failed'
        ? 'Provider 电源操作失败，请核对任务日志。'
        : state.operation === 'completed'
          ? '电源任务已完成，请以 Provider 后续同步核对运行结果。'
          : 'Provider 正在处理电源请求。',
  })
  const evidence = await installMocks(context, page, (url, method, request) => {
    const path = url.pathname
    if (path === '/api/v1/access/permission-snapshot')
      return {
        body: {
          data: {
            permissionKeys: [
              'workbench.compute.view',
              'virtualization.vms.view',
              ...(!readonly
                ? [
                    'virtualization.vms.power',
                    'virtualization.vms.console',
                    'virtualization.vms.metrics',
                    ...(canViewTasks ? ['virtualization.operations.view'] : []),
                    ...(state.design
                      ? [
                          'virtualization.vms.create',
                          'virtualization.vms.resize',
                          'virtualization.vms.delete',
                          'virtualization.vm_access.update',
                          'virtualization.vms.connect',
                          'secret.use',
                        ]
                      : []),
                  ]
                : []),
            ],
            visibleMenuIds: ['compute-workbench', 'virtualization-workbench-vms'],
            visibleMenus: [
              { id: 'compute-workbench', path: '/compute/overview', labelZh: '计算资源工作台' },
              {
                id: 'virtualization-workbench-vms',
                parentId: 'compute-workbench',
                path: '/compute/virtualization/vms',
                labelZh: '虚拟机',
              },
            ],
          },
        },
      }
    if (path === '/api/v1/modules')
      return {
        body: {
          data: [
            {
              descriptor: {
                id: 'virtualization',
                name: 'Virtualization',
                defaultPath: '/compute/virtualization',
              },
              enabled: true,
            },
          ],
        },
      }
    if (state.design && path === '/api/v1/virtualization/vms')
      return {
        body: {
          data: {
            items: [
              {
                id: 'vm-task52-demo',
                name: state.name,
                provider: 'pve',
                connectionId: 'pve-lab',
                connectionName: '研发资源池',
                node: 'pve-node-01',
                sourceRef: '142',
                powerState: state.power,
                status: state.power,
                cpu: 4,
                memoryMiB: 8192,
                diskGiB: 128,
                ipAddresses: ['10.20.0.42'],
                allowedActions: [
                  ...(state.listStartAllowed ? ['start'] : []),
                  'stop',
                  'restart',
                  'resize',
                  'delete',
                ],
                capabilities: [
                  'vm.resource.cpu.resize',
                  'vm.resource.memory.resize',
                  'vm.resource.disk.resize',
                  'vm.resource.network.add',
                ],
                config: { rootDiskGiB: 128, diskCount: 1 },
              },
            ],
            total: 1,
            page: 1,
            pageSize: Number(url.searchParams.get('pageSize') || 15),
          },
        },
      }
    if (state.design && path === '/api/v1/virtualization/clusters')
      return {
        body: {
          data: [
            {
              id: 'pve-lab',
              name: '研发资源池',
              provider: 'pve',
              enabled: true,
              config: {
                defaultNode: 'pve-node-01',
                defaultStorage: 'local-lvm',
                defaultBridge: 'vmbr0',
              },
            },
          ],
        },
      }
    if (state.design && path === '/api/v1/virtualization/images')
      return {
        body: {
          data: [
            {
              id: 'image-demo',
              name: 'Ubuntu 24.04',
              provider: 'pve',
              connectionId: 'pve-lab',
              assetKind: 'template',
              sourceKind: 'template',
              node: 'pve-node-01',
              storage: 'local-lvm',
              sourceRef: '9001',
            },
          ],
        },
      }
    if (state.design && path === '/api/v1/virtualization/flavors')
      return {
        body: {
          data: [
            {
              id: 'flavor-demo',
              name: 'standard-4c8g',
              provider: 'pve',
              cpu: 4,
              memoryMiB: 8192,
              diskGiB: 128,
            },
          ],
        },
      }
    if (state.design && path === `${apiPath}/devices`)
      return {
        body: {
          data: [
            { id: 'scsi0', kind: 'disk', sizeGiB: 128 },
            { id: 'net0', kind: 'network', network: 'vmbr0' },
          ],
        },
      }
    if (state.design && path === `${apiPath}/access-binding`)
      return { status: 404, body: { error: { code: 'not_found', message: '尚未配置登录绑定' } } }
    if (
      state.design &&
      path === '/api/v1/compute/resources/virtualization/vm/vm-task52-demo/relations'
    ) {
      const resource = {
        domain: 'virtualization',
        kind: 'vm',
        id: 'vm-task52-demo',
        displayName: state.name,
      }
      return {
        body: {
          data: {
            resource,
            relations: [
              {
                from: {
                  domain: 'virtualization',
                  kind: 'connection',
                  id: 'pve-lab',
                  displayName: '研发资源池',
                },
                to: resource,
                type: 'provisions',
                source: 'provider',
                observedAt: state.updatedAt,
              },
            ],
          },
        },
      }
    }
    if (path === `${apiPath}/detail`) {
      state.detailReads++
      if (state.detailFails)
        return {
          status: 503,
          body: { error: { code: 'unavailable', message: '暂时无法读取虚拟机记录' } },
        }
      return {
        body: {
          data: {
            vm: {
              id: 'vm-task52-demo',
              name: state.name,
              provider: 'pve',
              connectionId: 'pve-lab',
              connectionName: '研发资源池',
              node: 'pve-node-01',
              powerState: state.power,
              status: state.stale ? 'stale' : state.power,
              cpu: 4,
              memoryMiB: 8192,
              diskGiB: 128,
              ipAddresses: ['10.20.0.42'],
              updatedAt: state.updatedAt,
              allowedActions: readonly
                ? []
                : state.power === 'running'
                  ? ['stop', 'restart']
                  : ['start'],
              config: {
                hostname: 'build-worker-01',
                osType: 'Linux',
                firmware: 'UEFI',
                cloudInitConfigured: true,
                cloudInitUser: 'soha',
                guestAgentEnabled: true,
                diskCount: 1,
                networkInterfaceCount: 1,
                networks: 'vmbr0',
                gateway: '10.20.0.1',
                dnsServers: '10.20.0.2',
              },
            },
            operations: state.operation && canViewTasks ? [operation()] : [],
            logs: [],
            providerRaw: { provider: 'pve', node: 'pve-node-01', vmid: 142 },
          },
        },
      }
    }
    if (path === `${apiPath}/power` && method === 'POST') {
      state.action = request.postDataJSON().action
      state.writes.push(state.action)
      if (state.submitFails)
        return {
          status: 503,
          body: { error: { code: 'unavailable', message: '电源请求响应丢失' } },
        }
      state.operation = 'queued'
      return { body: { data: operation() } }
    }
    if (path === '/api/v1/virtualization/operations/op-task52-demo/logs' && canViewTasks)
      return {
        body: {
          data: [
            {
              id: 'log-demo',
              taskId: 'op-task52-demo',
              createdAt: state.updatedAt,
              logLevel: state.operation === 'failed' ? 'error' : 'info',
              message:
                state.operation === 'failed'
                  ? 'Provider 拒绝电源操作，未确认资源状态。'
                  : '已受理电源请求，正在核对 Provider 返回的任务结果。',
            },
          ],
        },
      }
    if (path === '/api/v1/virtualization/operations')
      return { body: { data: state.operation ? [operation()] : [] } }
    if (path === `${apiPath}/metrics`) {
      const range = Number(url.searchParams.get('rangeMinutes'))
      state.metricsReads.push(range)
      if (state.metricsFails)
        return {
          status: 503,
          body: { error: { code: 'unavailable', message: '指标服务暂时不可用' } },
        }
      const now = Math.floor(Date.now() / 1000)
      if (state.metricsInvalid)
        return {
          body: {
            data: {
              ready: true,
              source: 'pve-rrd',
              series: [
                {
                  key: 'cpu',
                  label: 'CPU 使用率',
                  unit: '%',
                  points: [{ timestamp: 1e30, value: 1 }],
                },
              ],
            },
          },
        }
      return {
        body: {
          data: {
            ready: true,
            source: 'pve-rrd',
            series: state.metricsEmpty
              ? []
              : [
                  {
                    key: 'cpu',
                    label: 'CPU 使用率',
                    unit: '%',
                    points: [18, 22, 14, 26, 32, 24, 19, 28, 16, 12, 21, 0].map((value, i) => ({
                      timestamp: now - (11 - i) * 60,
                      value,
                    })),
                  },
                  {
                    key: 'memory',
                    label: '内存使用量',
                    unit: 'bytes',
                    points: [2.4, 2.8, 2.6, 3.1, 3.5, 3.2, 3.4, 3.1, 2.9, 3.2, 3.5, 3.2].map(
                      (value, i) => ({ timestamp: now - (11 - i) * 60, value: value * 1024 ** 3 }),
                    ),
                  },
                  {
                    key: 'networkRx',
                    label: '网络接收',
                    unit: 'bytes/s',
                    points: [12, 18, 10, 32, 24, 14, 19, 26, 18, 14, 22, 16].map((value, i) => ({
                      timestamp: now - (11 - i) * 60,
                      value: value * 1024,
                    })),
                  },
                  {
                    key: 'networkTx',
                    label: '网络发送',
                    unit: 'bytes/s',
                    points: [8, 12, 9, 16, 10, 18, 12, 15, 8, 12, 10, 14].map((value, i) => ({
                      timestamp: now - (11 - i) * 60,
                      value: value * 1024,
                    })),
                  },
                ],
          },
        },
      }
    }
    if (path === `${apiPath}/console`) {
      state.consoleReads++
      return {
        body: {
          data: {
            ready: false,
            type: 'vnc',
            message: 'Provider 当前未提供可用控制台连接，请稍后重试。',
          },
        },
      }
    }
    return undefined
  })
  return { state, evidence }
}

test('overview candidate', async ({ context, page }, info) => {
  const { evidence, state } = await vmMocks(context, page)
  await page.goto(pagePath)
  await expect(page.getByText('build-worker-01', { exact: true }).first()).toBeVisible()
  await expect(page.getByRole('tab', { name: '概览', exact: true })).toBeVisible()
  await expect(
    page.locator('.soha-vrt-vm-detail-actions').getByRole('button', { name: '刷新', exact: true }),
  ).toBeVisible()
  expect(await page.locator('.soha-vm-record-bar button').count()).toBe(0)
  expect(state.consoleReads).toBe(0)
  expect(state.metricsReads).toEqual([])
  await page.screenshot({
    path: info.outputPath('vm-overview.png'),
    fullPage: true,
    animations: 'disabled',
  })
  verifyBrowserEvidence(evidence)
})

test('dark overview candidate', async ({ context, page }, info) => {
  await page.addInitScript(() =>
    localStorage.setItem(
      'soha-prefs',
      JSON.stringify({ state: { themeMode: 'dark', localeCode: 'zh_CN' }, version: 0 }),
    ),
  )
  const { evidence } = await vmMocks(context, page)
  await page.goto(pagePath)
  await expect(page.getByText('build-worker-01', { exact: true }).first()).toBeVisible()
  await page.screenshot({
    path: info.outputPath('vm-overview-dark.png'),
    fullPage: true,
    animations: 'disabled',
  })
  verifyBrowserEvidence(evidence)
})

test('power request tracks task and record, preserves evidence and stops terminal polling', async ({
  context,
  page,
}, info) => {
  const { state, evidence } = await vmMocks(context, page)
  await page.goto(pagePath)
  await page.getByRole('button', { name: '启动', exact: true }).click()
  await expect(page.getByRole('region', { name: '当前操作任务' })).toContainText('已受理，等待执行')
  await expect(page.getByRole('button', { name: '启动', exact: true })).toBeDisabled()
  await page.getByText('任务日志与证据', { exact: true }).click()
  await expect(page.locator('.soha-vm-task-log')).toContainText('已受理电源请求')
  await page.waitForTimeout(1200)
  state.operation = 'running'
  await page.getByRole('button', { name: '刷新', exact: true }).click()
  await expect(page.getByRole('region', { name: '当前操作任务' })).toContainText('任务执行中')
  await page.waitForTimeout(900)
  await page.screenshot({
    path: info.outputPath('vm-task-running.png'),
    fullPage: true,
    animations: 'disabled',
  })
  state.detailFails = true
  await page.getByRole('button', { name: '刷新', exact: true }).click()
  await expect(page.getByText('记录未更新，正在展示上次读取结果')).toBeVisible()
  await expect(page.locator('.soha-vm-task-log')).toBeVisible()
  await page.getByRole('button', { name: '关闭', exact: true }).click()
  await page.waitForTimeout(900)
  state.detailFails = false
  state.operation = 'completed'
  state.completedAt = new Date(Date.now() + 1000).toISOString()
  await page.getByRole('button', { name: '刷新', exact: true }).click()
  await expect(page.getByRole('region', { name: '当前操作任务' })).toContainText(
    '任务完成，资源记录待核对',
  )
  await expect(page.getByRole('button', { name: '启动', exact: true })).toBeDisabled()
  await page.waitForTimeout(900)
  state.power = 'running'
  state.updatedAt = state.completedAt
  await page.getByRole('button', { name: '刷新', exact: true }).click()
  await expect(page.getByRole('region', { name: '当前操作任务' })).toContainText(
    '任务完成，资源记录已更新',
  )
  await expect(page.locator('.soha-vm-live-dot')).toHaveCount(0)
  await page.screenshot({
    path: info.outputPath('vm-task-completed.png'),
    fullPage: true,
    animations: 'disabled',
  })
  const reads = state.detailReads
  await page.waitForTimeout(4200)
  expect(state.detailReads).toBe(reads)
  expect(state.writes).toEqual(['start'])
  await page.getByRole('tab', { name: '监控指标' }).click()
  await expect(page.locator('.soha-vm-metric-value').first()).toContainText('0')
  await expect(page.locator('.soha-vm-metric-grid canvas').first()).toBeVisible()
  const rangeGeometry = await page
    .getByRole('combobox', { name: '指标时间范围' })
    .evaluate((element) => {
      const control = element.closest('.ant-select')!.getBoundingClientRect()
      const header = element.closest('.soha-vm-metrics-header')!.getBoundingClientRect()
      return {
        height: control.height,
        topGap: control.top - header.top,
        bottomGap: header.bottom - control.bottom,
      }
    })
  expect(rangeGeometry.height).toBeLessThan(32)
  expect(rangeGeometry.topGap).toBeGreaterThanOrEqual(6)
  expect(rangeGeometry.bottomGap).toBeGreaterThanOrEqual(6)
  await expect(page.getByRole('button', { name: '刷新指标', exact: true })).toHaveText('')
  await page.locator('.soha-vm-metric-grid').scrollIntoViewIfNeeded()
  await page.waitForTimeout(900)
  await page.screenshot({
    path: info.outputPath('vm-metrics.png'),
    fullPage: true,
    animations: 'disabled',
  })
  expect(state.consoleReads).toBe(0)
  await page.getByRole('tab', { name: '控制台', exact: true }).click()
  await expect(page.getByText('控制台暂不可用')).toBeVisible()
  expect(state.consoleReads).toBe(1)
  await page.waitForTimeout(900)
  await page.screenshot({
    path: info.outputPath('vm-console-unavailable.png'),
    fullPage: true,
    animations: 'disabled',
  })
  await page.getByRole('button', { name: '重新检查控制台' }).click()
  await expect.poll(() => state.consoleReads).toBe(2)
  verifyBrowserEvidence(evidence)
})

test('confirmation cancellation and changed allowed actions never submit power requests', async ({
  context,
  page,
}) => {
  const { state, evidence } = await vmMocks(context, page)
  state.power = 'running'
  await page.goto(pagePath)
  await page.getByRole('button', { name: '停止', exact: true }).click()
  await expect(page.getByText('确认停止 build-worker-01？')).toBeVisible()
  await page.getByRole('button', { name: /^取\s*消$/ }).click()
  expect(state.writes).toEqual([])
  await page.getByRole('button', { name: '停止', exact: true }).click()
  state.power = 'stopped'
  await page.getByRole('button', { name: /^确\s*定$/ }).click()
  await expect(page.getByText('资源状态或允许操作已变化，请核对最新记录。')).toBeVisible()
  expect(state.writes).toEqual([])
  verifyBrowserEvidence(evidence)
})

for (const action of ['stop', 'restart'] as const)
  test(`${action} confirmation tracks the returned task`, async ({ context, page }) => {
    const { state, evidence } = await vmMocks(context, page)
    state.power = 'running'
    await page.goto(pagePath)
    await page
      .getByRole('button', { name: action === 'stop' ? '停止' : '重启', exact: true })
      .click()
    await page.getByRole('button', { name: /^确\s*定$/ }).click()
    await expect(page.getByRole('region', { name: '当前操作任务' })).toContainText(
      '已受理，等待执行',
    )
    expect(state.writes).toEqual([action])
    state.operation = 'completed'
    state.completedAt = new Date().toISOString()
    state.updatedAt = state.completedAt
    state.power = action === 'stop' ? 'stopped' : 'running'
    await page.getByRole('button', { name: '刷新', exact: true }).click()
    await expect(page.getByRole('region', { name: '当前操作任务' })).toContainText(
      '任务完成，资源记录已更新',
    )
    await expect(page.locator('.soha-vm-live-dot')).toHaveCount(0)
    expect(state.writes).toEqual([action])
    verifyBrowserEvidence(evidence)
  })

test('unknown request outcome has persistent feedback and no automatic duplicate write', async ({
  context,
  page,
}, info) => {
  const { state, evidence } = await vmMocks(context, page)
  state.submitFails = true
  await page.goto(pagePath)
  await page.getByRole('button', { name: '启动', exact: true }).click()
  await expect(page.getByText('请求结果不明，请先核对任务和资源记录')).toBeVisible()
  await expect(page.getByRole('button', { name: '启动', exact: true })).toBeDisabled()
  await page.getByRole('button', { name: '核对任务历史' }).click()
  await expect(page.getByRole('tab', { name: '任务历史' })).toHaveAttribute('aria-selected', 'true')
  await page.getByRole('button', { name: '刷新', exact: true }).click()
  expect(state.writes).toEqual(['start'])
  await page.screenshot({
    path: info.outputPath('vm-request-unknown.png'),
    fullPage: true,
    animations: 'disabled',
  })
  verifyBrowserEvidence(evidence)
})

test('metric refresh retains range and cached series, then displays a real empty result', async ({
  context,
  page,
}, info) => {
  await page.addInitScript(() =>
    localStorage.setItem(
      'soha-prefs',
      JSON.stringify({ state: { themeMode: 'dark', localeCode: 'zh_CN' }, version: 0 }),
    ),
  )
  const { state, evidence } = await vmMocks(context, page)
  state.power = 'running'
  await page.goto(pagePath)
  await page.getByRole('button', { name: '查看指标', exact: true }).click()
  await expect(page.locator('.soha-vm-metric-grid canvas').first()).toBeVisible()
  const canvasMatchesSurface = () =>
    page
      .locator('.soha-vm-metric-grid canvas')
      .first()
      .evaluate((element) => {
        const pixel = Array.from(
          (element as HTMLCanvasElement).getContext('2d')!.getImageData(2, 2, 1, 1).data,
        ).slice(0, 3)
        const surface = getComputedStyle(element.closest('.soha-vm-metric')!)
          .backgroundColor.match(/\d+/g)!
          .slice(0, 3)
          .map(Number)
        return pixel.every((value, index) => value === surface[index])
      })
  await expect.poll(canvasMatchesSurface).toBe(true)
  await page.screenshot({
    path: info.outputPath('vm-metrics-dark.png'),
    fullPage: true,
    animations: 'disabled',
  })
  await page.getByRole('combobox', { name: '指标时间范围' }).click()
  await page.getByRole('option', { name: '最近 6 小时' }).click()
  await expect.poll(() => state.metricsReads.includes(360)).toBe(true)
  await expect(page.getByRole('button', { name: '刷新指标', exact: true })).not.toHaveClass(
    /ant-btn-loading/,
  )
  const canvas = await page.locator('.soha-vm-metric-grid canvas').first().elementHandle()
  const readsBeforeTheme = state.metricsReads.length
  await page.getByRole('button', { name: '切换到浅色模式', exact: true }).click()
  await expect.poll(canvasMatchesSurface).toBe(true)
  expect(await canvas!.evaluate((element) => element.isConnected)).toBe(true)
  await expect(page.locator('.soha-vm-metrics-source')).toContainText('最近 360 分钟')
  expect(state.metricsReads.length).toBe(readsBeforeTheme)
  await page.getByRole('button', { name: '切换到深色模式', exact: true }).click()
  await expect.poll(canvasMatchesSurface).toBe(true)
  state.metricsFails = true
  await page.getByRole('button', { name: '刷新指标', exact: true }).click()
  await expect(page.getByText('指标未更新，正在展示上次读取结果')).toBeVisible()
  await expect(page.locator('.soha-vm-metric-grid canvas').first()).toBeVisible()
  await expect(page.locator('.soha-vm-metrics-source')).toContainText('最近 360 分钟')
  await page.screenshot({
    path: info.outputPath('vm-metrics-cached-error.png'),
    fullPage: true,
    animations: 'disabled',
  })
  state.metricsFails = false
  state.metricsEmpty = true
  await page.getByRole('button', { name: '刷新指标', exact: true }).click()
  await expect(page.getByText('Provider 暂未返回可展示的指标序列。')).toBeVisible()
  await expect(page.locator('.soha-vm-metric-value')).toHaveCount(0)
  state.metricsEmpty = false
  state.metricsInvalid = true
  await page.getByRole('button', { name: '刷新指标', exact: true }).click()
  await expect(page.getByText('未采集到有效样本')).toBeVisible()
  await expect(page.locator('.soha-vm-metric-grid canvas')).toHaveCount(0)
  verifyBrowserEvidence(evidence)
})

for (const theme of ['light', 'dark'] as const)
  test(`narrow ${theme} keyboard and reduced motion`, async ({ context, page }, info) => {
    await page.addInitScript(
      (mode) =>
        localStorage.setItem(
          'soha-prefs',
          JSON.stringify({ state: { themeMode: mode, localeCode: 'zh_CN' }, version: 0 }),
        ),
      theme,
    )
    await page.setViewportSize({ width: 390, height: 900 })
    await page.emulateMedia({ reducedMotion: 'reduce' })
    const { state, evidence } = await vmMocks(context, page)
    state.operation = 'running'
    await page.goto(pagePath)
    const refresh = page.getByRole('button', { name: '刷新', exact: true })
    await refresh.focus()
    await page.keyboard.press('Enter')
    await expect.poll(() => state.detailReads).toBeGreaterThan(1)
    await expect(refresh).toBeFocused()
    await verifyGeometry(page, refresh)
    expect(
      await page.locator('.soha-vm-live-dot').evaluate((el) => getComputedStyle(el).animationName),
    ).toBe('none')
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth + 1)).toBe(
      true,
    )
    await page.screenshot({
      path: info.outputPath(`vm-${theme}-mobile.png`),
      fullPage: true,
      animations: 'disabled',
    })
    await page.getByText('任务日志与证据', { exact: true }).click()
    await expect(page.locator('.soha-vm-task-log')).toBeVisible()
    await page.locator('.soha-vm-task-log').scrollIntoViewIfNeeded()
    await page.screenshot({
      path: info.outputPath(`vm-${theme}-mobile-evidence.png`),
      fullPage: true,
      animations: 'disabled',
    })
    verifyBrowserEvidence(evidence)
  })

test('readonly stale and first-load failure remain separate from running and success', async ({
  context,
  page,
}, info) => {
  const { state, evidence } = await vmMocks(context, page, true)
  state.detailFails = true
  await page.goto(pagePath)
  await expect(page.getByText('虚拟机详情读取失败')).toBeVisible()
  state.detailFails = false
  state.stale = true
  state.power = 'running'
  await page.getByRole('button', { name: '重试读取虚拟机' }).click()
  await expect(page.getByText('资源记录已过期')).toBeVisible()
  await expect(page.getByRole('button', { name: '启动', exact: true })).toHaveCount(0)
  await expect(page.getByRole('tab', { name: '监控指标' })).toHaveCount(0)
  await expect(page.getByRole('tab', { name: '控制台', exact: true })).toHaveCount(0)
  expect(state.consoleReads).toBe(0)
  expect(state.metricsReads).toEqual([])
  expect(state.writes).toEqual([])
  await page.screenshot({
    path: info.outputPath('vm-stale-readonly.png'),
    fullPage: true,
    animations: 'disabled',
  })
  verifyBrowserEvidence(evidence)
})

test('failed, canceled and unknown tasks retain honest result labels', async ({
  context,
  page,
}, info) => {
  const { state, evidence } = await vmMocks(context, page)
  state.operation = 'running'
  await page.goto(pagePath)
  state.operation = 'failed'
  await page.getByRole('button', { name: '刷新', exact: true }).click()
  await page.getByRole('tab', { name: '任务历史' }).click()
  await page.getByRole('button', { name: '查看任务证据', exact: true }).click()
  await expect(page.getByRole('region', { name: '当前操作任务' })).toContainText('任务执行失败')
  await page.screenshot({
    path: info.outputPath('vm-task-failed.png'),
    fullPage: true,
    animations: 'disabled',
  })
  state.operation = 'canceled'
  await page.getByRole('button', { name: '刷新', exact: true }).click()
  await expect(page.getByRole('region', { name: '当前操作任务' })).toContainText('任务已取消')
  state.operation = 'provider_unknown'
  await page.getByRole('button', { name: '刷新', exact: true }).click()
  await expect(page.getByRole('region', { name: '当前操作任务' })).toContainText('任务状态未知')
  await expect(page.locator('.soha-vm-live-dot')).toHaveCount(0)
  verifyBrowserEvidence(evidence)
})

test('long resource names at the 200 percent equivalent layout viewport', async ({
  context,
  page,
}, info) => {
  const { state, evidence } = await vmMocks(context, page)
  state.name = '研发构建资源 · integration-worker-production-verification-with-a-long-resource-name'
  await page.setViewportSize({ width: 720, height: 500 })
  // 720px is the layout viewport for a 1440px display at 200%; native browser zoom is a separate manual check.
  await page.goto(pagePath)
  await expect(page.getByRole('heading', { name: state.name, exact: true })).toBeVisible()
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth + 1)).toBe(
    true,
  )
  await page.screenshot({
    path: info.outputPath('vm-long-name-zoom.png'),
    fullPage: true,
    animations: 'disabled',
  })
  verifyBrowserEvidence(evidence)
})

test('existing task and log focus deep links keep resource identity', async ({ context, page }) => {
  const { state, evidence } = await vmMocks(context, page)
  for (const focus of ['operations', 'logs']) {
    await page.goto(`${pagePath}?focus=${focus}`)
    await expect(page.getByRole('heading', { name: state.name, exact: true })).toBeVisible()
    await expect(
      page.getByRole('tab', { name: focus === 'operations' ? '任务历史' : '日志', exact: true }),
    ).toHaveAttribute('aria-selected', 'true')
  }
  expect(state.writes).toEqual([])
  expect(state.consoleReads).toBe(0)
  verifyBrowserEvidence(evidence)
})

test('power-only permission tracks the resource without requesting inaccessible task logs', async ({
  context,
  page,
}) => {
  const { state, evidence } = await vmMocks(context, page, false, false)
  await page.goto(pagePath)
  await page.getByRole('button', { name: '启动', exact: true }).click()
  await expect(page.getByText('请求已受理，任务进度不可见')).toBeVisible()
  await expect(page.getByText('任务日志与证据', { exact: true })).toHaveCount(0)
  await expect(page.locator('.soha-vm-live-dot')).toHaveCount(0)
  await expect(page.getByRole('button', { name: '启动', exact: true })).toBeDisabled()
  state.power = 'running'
  state.updatedAt = new Date(Date.now() + 1000).toISOString()
  await page.getByRole('button', { name: '刷新', exact: true }).click()
  await expect(page.getByRole('button', { name: '停止', exact: true })).toBeDisabled()
  await expect(page.getByText('请求已受理，任务进度不可见')).toBeVisible()
  await page.getByRole('tab', { name: '任务历史', exact: true }).click()
  await expect(page.getByText('无任务查看权限', { exact: true })).toBeVisible()
  await page.getByRole('tab', { name: '日志', exact: true }).click()
  await expect(page.getByText('无日志查看权限', { exact: true })).toBeVisible()
  expect(evidence.requests.filter((path) => path.includes('/operations/'))).toEqual([])
  expect(state.writes).toEqual(['start'])
  verifyBrowserEvidence(evidence)
})

test('stale running records do not assert a stopped VM or open runtime engines', async ({
  context,
  page,
}) => {
  const { state, evidence } = await vmMocks(context, page)
  state.stale = true
  state.power = 'running'
  await page.goto(pagePath)
  await page.getByRole('tab', { name: '监控指标', exact: true }).click()
  await expect(page.getByText('当前记录未确认运行')).toBeVisible()
  await page.getByRole('tab', { name: '控制台', exact: true }).click()
  await expect(page.getByText('当前记录未确认运行').last()).toBeVisible()
  expect(state.consoleReads).toBe(0)
  expect(state.metricsReads).toEqual([])
  verifyBrowserEvidence(evidence)
})

for (const theme of ['light', 'dark']) {
  test(`VM design seven detail panels ${theme}`, async ({ context, page }, info) => {
    await page.addInitScript(
      (mode) =>
        localStorage.setItem(
          'soha-prefs',
          JSON.stringify({ state: { themeMode: mode, localeCode: 'zh_CN' }, version: 0 }),
        ),
      theme,
    )
    await page.emulateMedia({ reducedMotion: 'no-preference' })
    const { state, evidence } = await vmMocks(context, page)
    state.design = true
    state.power = 'running'
    state.operation = 'running'
    state.action = 'restart'
    await page.goto(pagePath)
    await expect(page.locator('html')).toHaveAttribute('data-theme-mode', theme)
    await expect(page.getByRole('heading', { name: state.name, exact: true })).toBeVisible()
    await expect(page.locator('.soha-vm-object-icon')).toHaveCSS('animation-name', 'none')
    await expect(page.locator('.soha-vm-live-dot')).toHaveCSS('animation-name', 'soha-vm-live')
    await page.screenshot({
      path: info.outputPath(`vm-design-overview-${theme}.png`),
      fullPage: true,
      animations: 'disabled',
    })
    await page.getByRole('tab', { name: '资源关系', exact: true }).click()
    await page.getByRole('button', { name: '选中关联对象 研发资源池', exact: true }).focus()
    await page.keyboard.press('Enter')
    await expect(page.locator('.soha-compute-relation-row')).toHaveCount(1)
    await page.screenshot({
      path: info.outputPath(`vm-design-relations-${theme}.png`),
      fullPage: true,
      animations: 'disabled',
    })
    await page.getByRole('tab', { name: '任务历史', exact: true }).click()
    await expect(page.getByRole('button', { name: '查看任务证据' })).toBeVisible()
    await page.screenshot({
      path: info.outputPath(`vm-design-history-${theme}.png`),
      fullPage: true,
      animations: 'disabled',
    })
    await page.getByRole('tab', { name: '日志', exact: true }).click()
    await expect(page.getByRole('heading', { name: '运行日志' })).toBeVisible()
    await page.screenshot({
      path: info.outputPath(`vm-design-logs-${theme}.png`),
      fullPage: true,
      animations: 'disabled',
    })
    await page.getByRole('tab', { name: '监控指标', exact: true }).click()
    await expect(page.getByText('0.00 %', { exact: true })).toBeVisible()
    await page.screenshot({
      path: info.outputPath(`vm-design-metrics-${theme}.png`),
      fullPage: true,
      animations: 'disabled',
    })
    await page.getByRole('tab', { name: 'SSH 登录', exact: true }).click()
    await expect(page.getByRole('heading', { name: 'SSH 登录配置' })).toBeVisible()
    await page.screenshot({
      path: info.outputPath(`vm-design-ssh-${theme}.png`),
      fullPage: true,
      animations: 'disabled',
    })
    await page.getByRole('tab', { name: '控制台', exact: true }).click()
    await expect(page.getByText('控制台暂不可用', { exact: true })).toBeVisible()
    await page.screenshot({
      path: info.outputPath(`vm-design-console-${theme}.png`),
      fullPage: true,
      animations: 'disabled',
    })
    await page.getByRole('tab', { name: '任务历史', exact: true }).click()
    await page.getByRole('tab', { name: '概览', exact: true }).click()
    await expect(page.getByRole('tab', { name: '概览', exact: true })).toHaveAttribute(
      'aria-selected',
      'true',
    )
    expect(state.consoleReads).toBe(1)
    expect(state.writes).toEqual([])
    verifyBrowserEvidence(evidence)
  })

  test(`VM design list and form context ${theme}`, async ({ context, page }, info) => {
    await page.addInitScript(
      (mode) =>
        localStorage.setItem(
          'soha-prefs',
          JSON.stringify({ state: { themeMode: mode, localeCode: 'zh_CN' }, version: 0 }),
        ),
      theme,
    )
    const { state, evidence } = await vmMocks(context, page)
    state.design = true
    state.power = 'running'
    await page.goto('/compute/virtualization/vms')
    await expect(page.locator('html')).toHaveAttribute('data-theme-mode', theme)
    await expect(page.getByRole('link', { name: state.name, exact: true })).toBeVisible()
    const row = page
      .getByRole('row')
      .filter({ has: page.getByRole('link', { name: state.name, exact: true }) })
    await expect(row.locator('[title="pve-node-01"]')).toHaveClass(/soha-metadata-tag/)
    await expect(row.locator('[title="研发资源池"]')).toHaveClass(/soha-metadata-tag/)
    for (const value of ['运行中', '4 vCPU', '10.20.0.42']) {
      const box = await row.getByText(value, { exact: value !== '4 vCPU' }).boundingBox()
      expect(box).not.toBeNull()
      expect(box!.x + box!.width).toBeLessThan(1440)
    }
    await page.screenshot({
      path: info.outputPath(`vm-design-list-${theme}.png`),
      fullPage: true,
      animations: 'disabled',
    })
    await page.getByRole('button', { name: '创建虚拟机', exact: true }).click()
    let dialog = page.getByRole('dialog', { name: '创建虚拟机', exact: true })
    await expect(dialog.getByText('创建计算实例', { exact: true })).toBeVisible()
    await dialog.getByRole('button', { name: '下一步', exact: true }).click({ trial: true })
    await page.screenshot({
      path: info.outputPath(`vm-design-create-${theme}.png`),
      animations: 'disabled',
    })
    await dialog.getByRole('button', { name: /^取\s*消$/ }).click()
    await page.getByRole('button', { name: `更多操作：${state.name}`, exact: true }).focus()
    await page.keyboard.press('Enter')
    await page.getByRole('menuitem', { name: '克隆虚拟机', exact: true }).click()
    dialog = page.getByRole('dialog', { name: '克隆虚拟机', exact: true })
    await expect(dialog.getByText('克隆来源', { exact: true })).toBeVisible()
    await expect(dialog.getByRole('textbox', { name: /^\*?\s*名称$/ })).toHaveValue(
      `${state.name}-clone`,
    )
    await dialog.getByRole('button', { name: '下一步', exact: true }).click({ trial: true })
    await page.screenshot({
      path: info.outputPath(`vm-design-clone-${theme}.png`),
      animations: 'disabled',
    })
    await dialog.getByRole('button', { name: /^取\s*消$/ }).click()
    await page.getByRole('button', { name: `更多操作：${state.name}`, exact: true }).click()
    await page.getByRole('menuitem', { name: '调整虚拟机规格', exact: true }).click()
    dialog = page.getByRole('dialog', { name: `调整规格：${state.name}`, exact: true })
    await expect(dialog.getByText('当前配置', { exact: false })).toBeVisible()
    await expect(dialog.getByRole('spinbutton', { name: /^\*?\s*CPU 核数$/ })).toHaveValue('4')
    for (let step = 0; step < 4; step++) {
      await dialog
        .getByRole('button', { name: step < 3 ? '下一步' : '提交调整', exact: true })
        .click({ trial: true })
      await page.screenshot({
        path: info.outputPath(`vm-design-resize-${step}-${theme}.png`),
        animations: 'disabled',
      })
      if (step < 3) await dialog.getByRole('button', { name: '下一步', exact: true }).click()
    }
    await dialog.getByRole('button', { name: /^取\s*消$/ }).click()
    await page.getByRole('button', { name: `更多操作：${state.name}`, exact: true }).click()
    await page.getByRole('menuitem', { name: '删除虚拟机', exact: true }).click()
    dialog = page.getByRole('dialog', { name: `删除虚拟机：${state.name}`, exact: true })
    await expect(dialog.getByText('从 Provider 删除虚拟机', { exact: true })).toBeVisible()
    await dialog.getByRole('button', { name: /^取\s*消$/ }).click()
    expect(state.writes).toEqual([])
    verifyBrowserEvidence(evidence)
  })
}

test('VM create and clone initialize before the previous modal finishes closing', async ({
  context,
  page,
}) => {
  const { state, evidence } = await vmMocks(context, page)
  state.design = true
  await page.goto('/compute/virtualization/vms')
  await expect(page.getByRole('link', { name: state.name, exact: true })).toBeVisible()
  // Keep the closing form mounted to exercise immediate keyboard reopening.
  await page.addStyleTag({
    content:
      '.ant-zoom-leave { animation-duration: 5s !important; } .ant-modal-wrap:has(.ant-zoom-leave) { pointer-events: none !important; }',
  })
  const create = page.getByRole('button', { name: '创建虚拟机', exact: true })
  await create.click()
  const dialog = page.getByRole('dialog')
  const name = dialog.getByRole('textbox', { name: /^\*?\s*名称$/ })
  await name.fill('应当清理的创建草稿')
  await dialog.getByRole('button', { name: /^取\s*消$/ }).click()
  await expect(page.locator('#virtual-machine-create_name')).toHaveCount(1)
  await page.getByRole('button', { name: `更多操作：${state.name}`, exact: true }).focus()
  await page.keyboard.press('Enter')
  await page.getByRole('menuitem', { name: '克隆虚拟机', exact: true }).click()
  await expect(dialog.getByText('克隆来源', { exact: true })).toBeVisible()
  await expect(name).toHaveValue(`${state.name}-clone`)
  await dialog.getByRole('button', { name: /^取\s*消$/ }).click()
  await create.focus()
  await page.keyboard.press('Enter')
  await expect(dialog.getByText('创建计算实例', { exact: true })).toBeVisible()
  await expect(name).toHaveValue('')
  expect(state.writes).toEqual([])
  verifyBrowserEvidence(evidence)
})

test('VM design narrow list forms preserve geometry and reduced motion', async ({
  context,
  page,
}, info) => {
  const { state, evidence } = await vmMocks(context, page)
  state.design = true
  state.listStartAllowed = true
  state.power = 'running'
  await page.setViewportSize({ width: 390, height: 844 })
  await page.emulateMedia({ reducedMotion: 'reduce' })
  await page.goto('/compute/virtualization/vms')
  const create = page.getByRole('button', { name: '创建虚拟机', exact: true })
  await expect(create).toBeVisible()
  await verifyGeometry(page, create)
  for (const name of ['启动虚拟机', '停止虚拟机', `更多操作：${state.name}`]) {
    await verifyGeometry(page, page.getByRole('button', { name, exact: true }))
  }
  await create.click()
  const dialog = page.getByRole('dialog')
  await expect(dialog.getByText('创建计算实例', { exact: true })).toBeVisible()
  await dialog.getByRole('button', { name: '下一步', exact: true }).click({ trial: true })
  await verifyGeometry(page, dialog.getByRole('button', { name: '下一步', exact: true }))
  await page.screenshot({
    path: info.outputPath('vm-design-create-390.png'),
    animations: 'disabled',
  })
  await dialog.getByRole('button', { name: /^取\s*消$/ }).click()
  await page.getByRole('button', { name: `更多操作：${state.name}`, exact: true }).click()
  await page.getByRole('menuitem', { name: '调整虚拟机规格', exact: true }).click()
  await dialog.getByRole('button', { name: '下一步', exact: true }).click({ trial: true })
  await verifyGeometry(page, dialog.getByRole('button', { name: '下一步', exact: true }))
  await page.screenshot({
    path: info.outputPath('vm-design-resize-390.png'),
    animations: 'disabled',
  })
  expect(state.writes).toEqual([])
  verifyBrowserEvidence(evidence)
})
