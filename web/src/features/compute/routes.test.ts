import { describe, expect, it, vi } from 'vitest'
import { computeRoutes } from './routes'

const routePages = vi.hoisted(() => ({
  overview: () => null,
  syncTasks: () => null,
  buildTasks: () => null,
  operationRecords: () => null,
  vms: () => null,
  vmDetail: () => null,
  clusters: () => null,
  images: () => null,
  storage: () => null,
  flavors: () => null,
  hosts: () => null,
  hostDetail: () => null,
  projects: () => null,
  projectDetail: () => null,
  templates: () => null,
}))

vi.mock('./overview/page', () => ({ ComputeOverviewPage: routePages.overview }))
vi.mock('./tasks/sync-page', () => ({ ComputeSyncTasksPage: routePages.syncTasks }))
vi.mock('./tasks/build-page', () => ({ ComputeBuildTasksPage: routePages.buildTasks }))
vi.mock('./tasks/operations-page', () => ({
  ComputeOperationRecordsPage: routePages.operationRecords,
}))
vi.mock('@/features/virtualization/virtual-machines/list-page', () => ({
  VirtualizationVmsPage: routePages.vms,
}))
vi.mock('@/features/virtualization/virtual-machines/detail-page', () => ({
  VirtualizationVmDetailPage: routePages.vmDetail,
}))
vi.mock('@/features/virtualization/clusters/list-page', () => ({
  VirtualizationClustersPage: routePages.clusters,
}))
vi.mock('@/features/virtualization/images/list-page', () => ({
  VirtualizationImagesPage: routePages.images,
}))
vi.mock('@/features/virtualization/storage/list-page', () => ({
  VirtualizationStoragePage: routePages.storage,
}))
vi.mock('@/features/virtualization/flavors/list-page', () => ({
  VirtualizationFlavorsPage: routePages.flavors,
}))
vi.mock('@/features/docker/hosts/page', () => ({ DockerHostsPage: routePages.hosts }))
vi.mock('@/features/docker', () => ({
  DockerHostDetailPage: routePages.hostDetail,
}))
vi.mock('@/features/docker/projects/list-page', () => ({
  DockerProjectsPage: routePages.projects,
}))
vi.mock('@/features/docker/projects/detail-page', () => ({
  DockerProjectDetailPage: routePages.projectDetail,
}))
vi.mock('@/features/docker/templates/page', () => ({
  DockerTemplatesPage: routePages.templates,
}))

describe('compute route manifest', () => {
  it('owns one workbench and reuses every domain page at a canonical route', async () => {
    const pageRoutes = computeRoutes.filter(
      (route): route is Extract<(typeof computeRoutes)[number], { readonly load: unknown }> =>
        'load' in route,
    )
    const loadedPages = await Promise.all(
      pageRoutes.map(async (route) => (await route.load()).default),
    )

    expect(pageRoutes).toHaveLength(15)
    expect(new Set(loadedPages).size).toBe(15)
    expect(
      computeRoutes
        .filter((route) => route.meta.navVisible)
        .every((route) => route.meta.workbenchId === 'compute'),
    ).toBe(true)
  })

  it('does not expose legacy aliases or standalone create routes', () => {
    expect(computeRoutes.some((route) => 'aliases' in route)).toBe(false)
    expect(computeRoutes.map((route) => route.meta.path)).not.toEqual(
      expect.arrayContaining([
        '/compute/access',
        '/compute/access/new',
        '/compute/virtualization/clusters/new',
        '/compute/runtimes/hosts/new',
        '/compute/tasks',
        '/compute/tasks/all',
      ]),
    )
  })

  it('exposes one Task Center menu and keeps old task routes hidden', () => {
    const taskRoutes = computeRoutes.filter((route) =>
      route.meta.path.startsWith('/compute/tasks/'),
    )
    expect(
      taskRoutes.filter((route) => route.meta.navVisible).map((route) => route.meta.title),
    ).toEqual(['任务中心'])
    expect(
      taskRoutes
        .filter((route) =>
          ['/compute/tasks/sync', '/compute/tasks/build'].includes(route.meta.path),
        )
        .every((route) => !route.meta.navVisible),
    ).toBe(true)
    expect(
      taskRoutes
        .filter((route) =>
          ['/compute/tasks/sync', '/compute/tasks/build'].includes(route.meta.path),
        )
        .every(
          (route) =>
            'menuId' in route.meta && route.meta.menuId === 'compute-workbench-tasks-operations',
        ),
    ).toBe(true)
  })

  it('uses the independent workbench entry permission for the root and overview', () => {
    const root = computeRoutes.find((route) => route.meta.id === 'compute-workbench')
    const overview = computeRoutes.find((route) => route.meta.id === 'compute-workbench-overview')
    if (
      !root ||
      !overview ||
      !('permissionKey' in root.meta) ||
      !('permissionKey' in overview.meta)
    ) {
      throw new Error('missing Compute workbench entry permission metadata')
    }

    expect(root.meta.permissionKey).toBe('workbench.compute.view')
    expect(overview.meta.permissionKey).toBe('workbench.compute.view')
  })

  it('uses a dedicated permission for virtualization storage', () => {
    const storage = computeRoutes.find(
      (route) => route.meta.id === 'virtualization-workbench-storage',
    )
    if (!storage || !('permissionKey' in storage.meta)) {
      throw new Error('missing virtualization storage permission metadata')
    }

    expect(storage.meta.permissionKey).toBe('virtualization.storage.view')
  })
})
