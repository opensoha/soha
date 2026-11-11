import { test, expect, type BrowserContext, type Page } from '@playwright/test'
import { installMocks, verifyBrowserEvidence } from '../fixtures/mock-app'
import { verifyGeometry } from '../fixtures/geometry'

const stamp = '2026-10-14T00:00:00Z'
const vm = {
  id: 'vm-demo',
  name: 'sample-vm',
  provider: 'pve',
  connectionId: 'pve-demo',
  powerState: 'running',
  status: 'running',
  cpu: 2,
  memoryMiB: 2048,
  diskGiB: 20,
  allowedActions: ['stop', 'restart'],
  updatedAt: stamp,
}
const host = {
  id: 'host-demo',
  name: 'sample-host',
  status: 'online',
  endpoint: 'http://agent.example.test',
  architecture: 'amd64',
  vmId: vm.id,
  updatedAt: stamp,
}
const project = {
  id: 'project-demo',
  name: 'sample-project',
  hostId: host.id,
  sourceKind: 'inline_compose',
  status: 'running',
  desiredState: 'running',
  composeContent: 'services: {}',
  updatedAt: stamp,
}
const resource = (domain: string, kind: string, id: string, displayName: string) => ({
  domain,
  kind,
  id,
  displayName,
})
const vmRef = resource('virtualization', 'vm', vm.id, vm.name)
const hostRef = resource('container_runtime', 'runtime_host', host.id, host.name)
const projectRef = resource('container_runtime', 'project', project.id, project.name)
const menus = [
  ['compute-workbench', '/compute'],
  ['compute-workbench-overview', '/compute/overview'],
  ['virtualization-workbench', '/compute/virtualization'],
  ['docker-workbench', '/compute/runtimes'],
  ...['vms', 'clusters', 'images', 'storage', 'flavors'].map((kind) => [
    `virtualization-workbench-${kind}`,
    `/compute/virtualization/${kind}`,
  ]),
  ...['hosts', 'projects', 'templates'].map((kind) => [
    `docker-workbench-${kind}`,
    `/compute/runtimes/${kind}`,
  ]),
  ['compute-workbench-tasks-operations', '/compute/tasks/operations'],
]
async function mocks(context: BrowserContext, page: Page) {
  const state = {
    relationPaged: false,
    relationStatus: 200,
    hostStatus: 200,
    taskStatus: 'running',
    detailStatus: 'failed',
    writes: [] as string[],
    relationReads: 0,
  }
  const task = (status: string) => ({
    id: 'task-demo',
    domain: 'container_runtime',
    providerKey: 'docker',
    kind: 'service_action',
    category: 'operation',
    normalizedStatus: status,
    createdAt: stamp,
    summary: 'sample task',
    resources: [projectRef],
    availableActions: ['logs', 'cancel'],
    verification: { status: 'unknown', summary: 'waiting for the source task to finish' },
  })
  const evidence = await installMocks(context, page, (url, method) => {
    const path = url.pathname
    if (path === '/api/v1/access/permission-snapshot')
      return {
        body: {
          data: {
            permissionKeys: [
              'workbench.compute.view',
              ...['vms', 'clusters', 'images', 'storage', 'flavors', 'operations', 'sync'].map(
                (kind) => `virtualization.${kind}.view`,
              ),
              'virtualization.flavors.create',
              'virtualization.clusters.test',
              ...['hosts', 'projects', 'templates', 'services', 'ports', 'operations'].map(
                (kind) => `docker.${kind}.view`,
              ),
            ],
            visibleMenuIds: menus.map(([id]) => id),
            visibleMenus: menus.map(([id, path]) => ({
              id,
              path,
              labelZh: id,
              parentId: id === 'compute-workbench' ? undefined : 'compute-workbench',
            })),
          },
        },
      }
    if (path === '/api/v1/modules')
      return {
        body: {
          data: ['virtualization', 'docker'].map((id) => ({
            descriptor: { id, name: id, defaultPath: '/compute/overview' },
            enabled: true,
          })),
        },
      }
    if (method !== 'GET') {
      state.writes.push(`${method} ${path}`)
      if (path === '/api/v1/auth/stream-ticket')
        return { body: { data: { ticket: 'synthetic-ticket', expiresAt: stamp } } }
      if (path === '/api/v1/virtualization/flavors' && method === 'POST')
        return {
          status: 422,
          body: { error: { code: 'invalid', message: '规格名称已被使用，请修改后保存' } },
        }
      if (path.endsWith('/cancel')) return { body: { data: task('running') } }
      return undefined
    }
    if (path === '/api/v1/compute/overview')
      return {
        body: {
          data: {
            generatedAt: stamp,
            freshness: { status: 'fresh', observedAt: stamp },
            virtualization: {
              status: 'ok',
              summary: {
                connectionsTotal: 1,
                connectionsHealthy: 1,
                connectionsDegraded: 0,
                connectionsUnsynced: 0,
                vmsTotal: 1,
                vmsRunning: 1,
                vmsStopped: 0,
                vmsError: 0,
              },
            },
            runtimes: { status: 'ok', summary: { total: 1, available: 1, error: 0 } },
            tasks: { status: 'ok', summary: { queued: 0, running: 1, failed: 0 } },
            attention: [],
            providerHealth: [],
          },
        },
      }
    if (path === '/api/v1/compute/provider-instances') return { body: { items: [] } }
    if (path === '/api/v1/compute/tasks') return { body: { items: [task(state.taskStatus)] } }
    if (path === '/api/v1/compute/tasks/container_runtime/task-demo')
      return { body: { data: task(state.detailStatus) } }
    if (path.endsWith('/task-demo/logs'))
      return {
        body: {
          items: [
            { id: 'log-demo', logLevel: 'info', createdAt: stamp, message: 'sample task evidence' },
          ],
        },
      }
    if (path.endsWith('/task-demo/stream'))
      return {
        status: 503,
        body: {
          error: {
            code: 'unavailable',
            message: 'Synthetic stream unavailable; polling remains available',
          },
        },
      }
    if (path.startsWith('/api/v1/compute/resources/') && path.endsWith('/relations')) {
      state.relationReads++
      if (state.relationStatus !== 200)
        return {
          status: state.relationStatus,
          body: { error: { code: 'unavailable', message: '关系记录暂不可读' } },
        }
      return {
        body: {
          data: {
            resource: path.includes('/project/') ? projectRef : hostRef,
            nextCursor:
              state.relationPaged && !url.searchParams.has('cursor') ? 'next-batch' : undefined,
            relations: url.searchParams.has('cursor')
              ? [
                  {
                    from: resource('virtualization', 'image', 'image-demo', 'source-image'),
                    to: hostRef,
                    type: 'derived_from',
                    source: 'persisted',
                    observedAt: stamp,
                  },
                ]
              : [
                  {
                    from: vmRef,
                    to: hostRef,
                    type: 'provisions',
                    source: 'persisted',
                    observedAt: stamp,
                    stale: true,
                  },
                  {
                    from: projectRef,
                    to: hostRef,
                    type: 'runs_on',
                    source: 'derived',
                    observedAt: stamp,
                  },
                ],
          },
        },
      }
    }
    if (path === '/api/v1/virtualization/clusters')
      return {
        body: {
          data: [
            {
              id: 'pve-demo',
              name: 'sample-connection',
              provider: 'pve',
              enabled: true,
              endpoint: 'https://pve.example.test',
              vmCount: 1,
              config: {},
            },
          ],
        },
      }
    if (path === '/api/v1/virtualization/vms')
      return { body: { data: { items: [vm], total: 1, page: 1, pageSize: 15 } } }
    if (path === '/api/v1/virtualization/vms/vm-demo/detail')
      return { body: { data: { vm, disks: [], networks: [], operations: [] } } }
    if (path === '/api/v1/virtualization/images')
      return {
        body: {
          data: {
            items: [
              {
                id: 'image-demo',
                name: 'sample-image',
                provider: 'pve',
                type: url.searchParams.get('category') === 'storage' ? 'storage' : 'template',
                connectionId: 'pve-demo',
              },
            ],
            total: 1,
            page: 1,
            pageSize: 15,
          },
        },
      }
    if (path === '/api/v1/virtualization/flavors')
      return {
        body: {
          data: [
            { id: 'flavor-demo', name: 'sample-flavor', cpu: 2, memoryMiB: 2048, diskGiB: 20 },
          ],
        },
      }
    if (path === '/api/v1/virtualization/operations') return { body: { data: [] } }
    if (path === '/api/v1/docker/hosts/host-demo')
      return state.hostStatus === 200
        ? { body: { data: host } }
        : {
            status: state.hostStatus,
            body: { error: { code: 'forbidden', message: '主机访问已拒绝' } },
          }
    if (path === '/api/v1/docker/projects/project-demo') return { body: { data: project } }
    const lists: Record<string, unknown[]> = {
      hosts: [host],
      projects: [project],
      templates: [
        {
          id: 'template-demo',
          name: 'sample-template',
          templateKind: 'compose',
          composeContent: 'services: {}',
        },
      ],
      services: [],
      ports: [],
    }
    const kind = path.replace('/api/v1/docker/', '')
    if (lists[kind])
      return {
        body: { data: { items: lists[kind], total: lists[kind].length, page: 1, pageSize: 15 } },
      }
    return undefined
  })
  return { state, evidence }
}

const routes = [
  ['/compute', '/compute/overview', '/compute/overview'],
  ['/compute/overview', '/compute/overview', '/compute/overview'],
  ['/compute/virtualization', '/compute/virtualization/vms', '/virtualization/vms'],
  ['/compute/virtualization/vms', '/compute/virtualization/vms', '/virtualization/vms'],
  [
    '/compute/virtualization/vms/vm-demo',
    '/compute/virtualization/vms/vm-demo',
    '/virtualization/vms/vm-demo/detail',
  ],
  ...['clusters', 'images', 'storage', 'flavors'].map((kind) => [
    `/compute/virtualization/${kind}`,
    `/compute/virtualization/${kind}`,
    `/virtualization/${kind === 'storage' ? 'images' : kind}`,
  ]),
  ['/compute/runtimes', '/compute/runtimes/hosts', '/docker/hosts'],
  ...['hosts', 'projects', 'templates'].map((kind) => [
    `/compute/runtimes/${kind}`,
    `/compute/runtimes/${kind}`,
    `/docker/${kind}`,
  ]),
  [
    '/compute/runtimes/hosts/host-demo',
    '/compute/runtimes/hosts/host-demo',
    '/docker/hosts/host-demo',
  ],
  [
    '/compute/runtimes/projects/project-demo',
    '/compute/runtimes/projects/project-demo',
    '/docker/projects/project-demo',
  ],
  ...['sync', 'build', 'operations'].map((kind) => [
    `/compute/tasks/${kind}`,
    '/compute/tasks/operations',
    '/compute/tasks',
  ]),
]
for (const [route, target, request] of routes)
  test(`compute entry ${route}`, async ({ page, context }) => {
    const { evidence } = await mocks(context, page)
    await page.goto(route)
    await expect(page).toHaveURL(
      new RegExp(`${target.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')}(?:\\?|$)`),
    )
    await expect
      .poll(() => evidence.requests.some((path) => path.startsWith(`/api/v1${request}`)))
      .toBe(true)
    await expect(page.locator('body')).not.toContainText('页面加载失败')
    verifyBrowserEvidence(evidence)
  })

test('relationship selection survives refresh and read failures while access denial removes evidence', async ({
  page,
  context,
}) => {
  const { state, evidence } = await mocks(context, page)
  await page.goto('/compute/runtimes/hosts/host-demo')
  await page.getByRole('tab', { name: '资源关系' }).click()
  await page.getByRole('button', { name: '选中关联对象 sample-project', exact: true }).focus()
  await page.keyboard.press('Enter')
  await expect(page.locator('.soha-compute-relation-row')).toHaveCount(1)
  await expect(
    page.getByRole('button', { name: '选中关联对象 sample-project', exact: true }),
  ).toHaveAttribute('aria-pressed', 'true')
  await page.getByRole('button', { name: '刷新资源关系' }).click()
  await expect.poll(() => state.relationReads).toBe(2)
  await expect(page.locator('.soha-compute-relation-row')).toHaveCount(1)
  await page.getByRole('textbox', { name: '筛选资源关系' }).fill('no-match')
  await expect(page.getByText('当前筛选没有关系')).toBeVisible()
  await page.getByRole('textbox', { name: '筛选资源关系' }).fill('')
  state.relationStatus = 503
  await page.getByRole('button', { name: '刷新资源关系' }).click()
  await expect(page.getByText('资源关系未更新，正在展示上次读取结果')).toBeVisible({
    timeout: 15_000,
  })
  await expect(page.locator('.soha-compute-relation-row')).toHaveCount(1)
  state.relationStatus = 403
  await expect(page.locator('.ant-notification-notice')).toHaveCount(0)
  await page.getByRole('button', { name: '重试读取资源关系' }).click()
  await expect(page.getByText('资源关系无访问权限')).toBeVisible({ timeout: 15_000 })
  await expect(page.locator('.ant-notification-notice')).toHaveCount(0)
  await expect(page.locator('.soha-compute-relation-row')).toHaveCount(0)
  expect(state.writes.filter((path) => !path.includes('/auth/'))).toEqual([])
  verifyBrowserEvidence(evidence)
})

test('relationship pagination loads the remaining actual records without resource writes', async ({
  page,
  context,
}) => {
  const { state, evidence } = await mocks(context, page)
  state.relationPaged = true
  await page.goto('/compute/runtimes/hosts/host-demo')
  await page.getByRole('tab', { name: '资源关系' }).click()
  await expect(page.getByText('本次读取 2 条关系')).toBeVisible()
  await page.getByRole('button', { name: '继续加载关系' }).click()
  await expect(page.getByText('本次读取 3 条关系')).toBeVisible()
  await expect(page.locator('.soha-compute-relation-row')).toHaveCount(3)
  await expect(page.getByRole('button', { name: '继续加载关系' })).toHaveCount(0)
  expect(evidence.requests.some((path) => path.includes('cursor=next-batch'))).toBe(true)
  expect(state.writes.filter((path) => !path.includes('/auth/'))).toEqual([])
  verifyBrowserEvidence(evidence)
})

test('form rejection keeps input and contextual feedback', async ({ page, context }) => {
  const { state, evidence } = await mocks(context, page)
  await page.goto('/compute/virtualization/flavors')
  await page.getByRole('button', { name: '新增规格' }).click()
  const dialog = page.getByRole('dialog')
  await dialog.getByLabel('名称', { exact: true }).fill('sample-flavor')
  await dialog.getByRole('button', { name: /^保\s*存$/ }).click()
  await expect(dialog.getByText('保存规格：操作请求失败')).toBeVisible()
  await expect(dialog.getByLabel('名称', { exact: true })).toHaveValue('sample-flavor')
  expect(state.writes.filter((path) => path.includes('/flavors'))).toHaveLength(1)
  verifyBrowserEvidence(evidence)
})

test('task drawer uses fresh task detail and cancellation acceptance remains separate from terminal state', async ({
  page,
  context,
}) => {
  const { state, evidence } = await mocks(context, page)
  await page.goto('/compute/tasks/operations?domain=container_runtime&taskId=task-demo&view=logs')
  const drawer = page.getByRole('dialog')
  await expect(drawer.getByText('sample task evidence')).toBeVisible()
  await expect(drawer.locator('.ant-descriptions')).toContainText('失败')
  await page.getByRole('button', { name: '关闭', exact: true }).click()
  state.detailStatus = 'running'
  await page.getByRole('button', { name: '取消任务', exact: true }).click()
  await page.getByRole('button', { name: /^确\s*定$/ }).click()
  await expect(page.getByText('取消请求已提交，请以任务状态为准')).toBeVisible()
  await expect(page.locator('.ant-table-tbody')).toContainText('运行中')
  expect(state.writes.filter((path) => path.endsWith('/cancel'))).toHaveLength(1)
  verifyBrowserEvidence(evidence)
})

for (const width of [1440, 390])
  test(`compute detail and relationships geometry ${width}`, async ({ page, context }) => {
    const { evidence } = await mocks(context, page)
    await page.setViewportSize({ width, height: 1000 })
    await page.goto('/compute/runtimes/hosts/host-demo')
    await page.getByRole('tab', { name: '资源关系' }).click()
    await expect(page.getByText('本次读取 2 条关系')).toBeVisible()
    await verifyGeometry(page, page.getByRole('button', { name: '刷新资源关系' }))
    await page.screenshot({
      path: test.info().outputPath(`compute-relations-${width}.png`),
      fullPage: true,
    })
    verifyBrowserEvidence(evidence)
  })
