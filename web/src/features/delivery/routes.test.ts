import { describe, expect, it, vi } from 'vitest'
import { validateRouteDefinitions } from '@/routes/definitions'
import { deliveryRoutes } from './routes'

const routePages = vi.hoisted(() => ({
  deliveryWorkflows: () => null,
  deliveryBatches: () => null,
  deliveryBatchDetail: () => null,
  deploymentTemplates: () => null,
  manifests: () => null,
  applications: () => null,
  overview: () => null,
  applicationDetail: () => null,
  workloadDetail: () => null,
  workflowDesigner: () => null,
  environments: () => null,
  environmentCatalog: () => null,
  buildTemplates: () => null,
  blueprints: () => null,
  applicationCreateRedirect: () => null,
  testing: () => null,
  analysis: () => null,
  releaseBundles: () => null,
  releaseBundleDetail: () => null,
  executionTasks: () => null,
  executionTaskDetail: () => null,
  workflowTemplates: () => null,
  releaseBoard: () => null,
  executionHistory: () => null,
  workflowDetail: () => null,
  releases: () => null,
  releaseDetail: () => null,
  buildDetail: () => null,
  registries: () => null,
}))

vi.mock('./manifests/library-page', () => ({ ManifestLibraryPage: routePages.manifests }))
vi.mock('./workflows/list-page', () => ({ DeliveryWorkflowsPage: routePages.deliveryWorkflows }))
vi.mock('./batches/list-page', () => ({ DeliveryBatchesPage: routePages.deliveryBatches }))
vi.mock('./batches/detail-page', () => ({
  DeliveryBatchDetailPage: routePages.deliveryBatchDetail,
}))
vi.mock('./deployment-templates/page', () => ({
  DeploymentTemplatesPage: routePages.deploymentTemplates,
}))
vi.mock('./applications/list-page', () => ({ ApplicationsPage: routePages.applications }))
vi.mock('./workbench/overview-page', () => ({ DeliveryOverviewPage: routePages.overview }))
vi.mock('./applications/detail-page', () => ({
  ApplicationDetailPage: routePages.applicationDetail,
}))
vi.mock('./runtime/workload-detail-page', () => ({
  ApplicationWorkloadDetailPage: routePages.workloadDetail,
}))
vi.mock('./applications/workflow-designer-page', () => ({
  ApplicationWorkflowDesignerPage: routePages.workflowDesigner,
}))
vi.mock('./environments/list-page', () => ({
  ApplicationEnvironmentsPage: routePages.environments,
}))
vi.mock('./environments/catalog-page', () => ({
  EnvironmentCatalogPage: routePages.environmentCatalog,
}))
vi.mock('./build-templates/page', () => ({ BuildTemplatesPage: routePages.buildTemplates }))
vi.mock('./blueprints/page', () => ({ DeliveryBlueprintsPage: routePages.blueprints }))
vi.mock('./applications/legacy-redirect', () => ({
  ApplicationCreateRedirect: routePages.applicationCreateRedirect,
}))
vi.mock('./workbench/testing-page', () => ({ DeliveryTestingPage: routePages.testing }))
vi.mock('./workbench/analysis-page', () => ({ DeliveryAnalysisPage: routePages.analysis }))
vi.mock('./release-bundles/list-page', () => ({ ReleaseBundlesPage: routePages.releaseBundles }))
vi.mock('./release-bundles/detail-page', () => ({
  ReleaseBundleDetailPage: routePages.releaseBundleDetail,
}))
vi.mock('./execution-tasks/list-page', () => ({ ExecutionTasksPage: routePages.executionTasks }))
vi.mock('./execution-tasks/detail-page', () => ({
  ExecutionTaskDetailPage: routePages.executionTaskDetail,
}))
vi.mock('./workflow-templates/page', () => ({
  WorkflowTemplatesPage: routePages.workflowTemplates,
}))
vi.mock('./release-board/page', () => ({ ExecutionHistoryPage: routePages.executionHistory }))
vi.mock('./release-board/workflow-catalog', () => ({ WorkflowCatalog: routePages.releaseBoard }))
vi.mock('./workflows/detail-page', () => ({ WorkflowDetailPage: routePages.workflowDetail }))
vi.mock('./releases/list-page', () => ({ ReleasesPage: routePages.releases }))
vi.mock('./releases/detail-page', () => ({ ReleaseDetailPage: routePages.releaseDetail }))
vi.mock('./builds/detail-page', () => ({ BuildDetailPage: routePages.buildDetail }))
vi.mock('./registries/page', () => ({ RegistriesPage: routePages.registries }))

describe('delivery route manifest', () => {
  it('maps routes directly to distinct leaf modules', async () => {
    const expectedPages = new Map([
      ['delivery-workflows', routePages.deliveryWorkflows],
      ['delivery-batches', routePages.deliveryBatches],
      ['delivery-batch-detail', routePages.deliveryBatchDetail],
      ['deployment-templates', routePages.deploymentTemplates],
      ['delivery-manifest-library', routePages.manifests],
      ['applications', routePages.applications],
      ['delivery-overview', routePages.overview],
      ['application-detail', routePages.applicationDetail],
      ['application-workload-detail', routePages.workloadDetail],
      ['application-workflow-designer', routePages.workflowDesigner],
      ['application-environments', routePages.environments],
      ['delivery-environments', routePages.environmentCatalog],
      ['build-templates', routePages.buildTemplates],
      ['delivery-blueprints', routePages.blueprints],
      ['delivery-onboarding', routePages.applicationCreateRedirect],
      ['delivery-testing', routePages.testing],
      ['delivery-analysis', routePages.analysis],
      ['release-bundles', routePages.releaseBundles],
      ['release-bundles-detail', routePages.releaseBundleDetail],
      ['execution-tasks', routePages.executionTasks],
      ['execution-tasks-detail', routePages.executionTaskDetail],
      ['workflow-templates', routePages.workflowTemplates],
      ['release-board', routePages.releaseBoard],
      ['execution-history', routePages.executionHistory],
      ['workflows-detail', routePages.workflowDetail],
      ['releases', routePages.releases],
      ['releases-detail', routePages.releaseDetail],
      ['builds-detail', routePages.buildDetail],
      ['registries', routePages.registries],
    ])

    const loadedPages = await Promise.all(
      deliveryRoutes.map(async (route) => {
        const module = await route.load()
        expect(module.default).toBe(expectedPages.get(route.meta.id))
        return module.default
      }),
    )

    expect(loadedPages).toHaveLength(expectedPages.size)
    expect(new Set(loadedPages).size).toBe(expectedPages.size)
  })

  it('preserves paths, permissions, and navigation metadata', () => {
    expect(
      deliveryRoutes.map((route) => ({
        id: route.meta.id,
        menuId: 'menuId' in route.meta ? route.meta.menuId : undefined,
        navVisible: route.meta.navVisible,
        path: route.meta.path,
        permissionKey: 'permissionKey' in route.meta ? route.meta.permissionKey : undefined,
        permissionKeysAny:
          'permissionKeysAny' in route.meta ? route.meta.permissionKeysAny : undefined,
      })),
    ).toEqual([
      {
        id: 'delivery-workflows',
        menuId: undefined,
        navVisible: false,
        path: '/delivery/workflows',
        permissionKey: 'delivery.workflows.view',
        permissionKeysAny: undefined,
      },
      {
        id: 'delivery-batches',
        menuId: undefined,
        navVisible: false,
        path: '/delivery/batches',
        permissionKey: 'delivery.workflows.view',
        permissionKeysAny: undefined,
      },
      {
        id: 'delivery-batch-detail',
        menuId: undefined,
        navVisible: false,
        path: '/delivery/batches/:batchId',
        permissionKey: 'delivery.workflows.view',
        permissionKeysAny: undefined,
      },
      {
        id: 'delivery-manifest-library',
        menuId: undefined,
        navVisible: false,
        path: '/delivery/manifests',
        permissionKey: 'delivery.applications.view',
        permissionKeysAny: undefined,
      },
      {
        id: 'applications',
        menuId: 'builds',
        navVisible: true,
        path: '/applications',
        permissionKey: 'delivery.applications.view',
        permissionKeysAny: undefined,
      },
      {
        id: 'delivery-overview',
        menuId: 'delivery-overview',
        navVisible: true,
        path: '/delivery/overview',
        permissionKey: undefined,
        permissionKeysAny: [
          'delivery.applications.view',
          'delivery.application-environments.view',
          'delivery.release-board.view',
          'delivery.release-bundles.view',
          'delivery.execution-tasks.view',
        ],
      },
      {
        id: 'application-detail',
        menuId: undefined,
        navVisible: false,
        path: '/applications/:applicationId',
        permissionKey: undefined,
        permissionKeysAny: undefined,
      },
      {
        id: 'application-workload-detail',
        menuId: undefined,
        navVisible: false,
        path: '/applications/:applicationId/application-environments/:applicationEnvironmentId/workloads/:workloadName',
        permissionKey: undefined,
        permissionKeysAny: undefined,
      },
      {
        id: 'application-workflow-designer',
        menuId: undefined,
        navVisible: false,
        path: '/applications/:applicationId/workflows/design',
        permissionKey: 'delivery.application-environments.update',
        permissionKeysAny: undefined,
      },
      {
        id: 'application-environments',
        menuId: undefined,
        navVisible: false,
        path: '/application-environments',
        permissionKey: 'delivery.application-environments.view',
        permissionKeysAny: undefined,
      },
      {
        id: 'delivery-environments',
        menuId: 'delivery-environment-directory',
        navVisible: true,
        path: '/delivery/environments',
        permissionKey: 'delivery.application-environments.view',
        permissionKeysAny: undefined,
      },
      {
        id: 'deployment-templates',
        menuId: 'deployment-templates',
        navVisible: true,
        path: '/deployment-templates',
        permissionKey: 'delivery.deployment-templates.view',
        permissionKeysAny: undefined,
      },
      {
        id: 'build-templates',
        menuId: 'build-templates',
        navVisible: true,
        path: '/build-templates',
        permissionKey: 'delivery.build-templates.view',
        permissionKeysAny: undefined,
      },
      {
        id: 'delivery-blueprints',
        menuId: 'delivery-blueprints',
        navVisible: true,
        path: '/delivery/blueprints',
        permissionKey: 'delivery.applications.view',
        permissionKeysAny: undefined,
      },
      {
        id: 'delivery-onboarding',
        menuId: undefined,
        navVisible: false,
        path: '/delivery/onboarding',
        permissionKey: 'delivery.applications.view',
        permissionKeysAny: undefined,
      },
      {
        id: 'delivery-testing',
        menuId: 'delivery-testing',
        navVisible: true,
        path: '/delivery/testing',
        permissionKey: undefined,
        permissionKeysAny: [
          'delivery.release-bundles.view',
          'delivery.execution-tasks.view',
          'delivery.release-board.view',
        ],
      },
      {
        id: 'delivery-analysis',
        menuId: 'delivery-analysis',
        navVisible: true,
        path: '/delivery/analysis',
        permissionKey: undefined,
        permissionKeysAny: [
          'delivery.execution-tasks.view',
          'delivery.release-board.view',
          'delivery.release-bundles.view',
        ],
      },
      {
        id: 'release-bundles',
        menuId: 'release-bundles',
        navVisible: true,
        path: '/delivery/release-bundles',
        permissionKey: 'delivery.release-bundles.view',
        permissionKeysAny: undefined,
      },
      {
        id: 'release-bundles-detail',
        menuId: undefined,
        navVisible: false,
        path: '/delivery/release-bundles/:releaseBundleId',
        permissionKey: 'delivery.release-bundles.view',
        permissionKeysAny: undefined,
      },
      {
        id: 'execution-tasks',
        menuId: undefined,
        navVisible: false,
        path: '/delivery/execution-tasks',
        permissionKey: 'delivery.execution-tasks.view',
        permissionKeysAny: undefined,
      },
      {
        id: 'execution-tasks-detail',
        menuId: undefined,
        navVisible: false,
        path: '/delivery/execution-tasks/:executionTaskId',
        permissionKey: 'delivery.execution-tasks.view',
        permissionKeysAny: undefined,
      },
      {
        id: 'workflow-templates',
        menuId: 'workflow-templates',
        navVisible: true,
        path: '/workflow-templates',
        permissionKey: 'delivery.workflow-templates.view',
        permissionKeysAny: undefined,
      },
      {
        id: 'release-board',
        menuId: 'release-board',
        navVisible: true,
        path: '/release-board',
        permissionKey: undefined,
        permissionKeysAny: ['delivery.workflows.view', 'delivery.applications.view'],
      },
      {
        id: 'execution-history',
        menuId: 'execution-history',
        navVisible: true,
        path: '/execution-history',
        permissionKey: undefined,
        permissionKeysAny: ['delivery.workflows.view', 'delivery.applications.view'],
      },
      {
        id: 'workflows-detail',
        menuId: undefined,
        navVisible: false,
        path: '/workflows/:workflowId',
        permissionKey: 'delivery.workflows.view',
        permissionKeysAny: undefined,
      },
      {
        id: 'releases',
        menuId: 'releases',
        navVisible: true,
        path: '/releases',
        permissionKey: 'delivery.releases.view',
        permissionKeysAny: undefined,
      },
      {
        id: 'releases-detail',
        menuId: undefined,
        navVisible: false,
        path: '/releases/:releaseId',
        permissionKey: 'delivery.releases.view',
        permissionKeysAny: undefined,
      },
      {
        id: 'builds-detail',
        menuId: undefined,
        navVisible: false,
        path: '/builds/:buildId',
        permissionKey: 'delivery.applications.view',
        permissionKeysAny: undefined,
      },
      {
        id: 'registries',
        menuId: 'registries',
        navVisible: true,
        path: '/registries',
        permissionKey: 'delivery.registries.view',
        permissionKeysAny: undefined,
      },
    ])

    expect(deliveryRoutes.find((route) => route.meta.id === 'execution-tasks')?.meta.title).toBe(
      '执行队列',
    )
    const workflowDetailRoute = deliveryRoutes.find((route) => route.meta.id === 'workflows-detail')
    expect(
      workflowDetailRoute && 'parentId' in workflowDetailRoute.meta
        ? workflowDetailRoute.meta.parentId
        : undefined,
    ).toBe('execution-history')
  })

  it('passes standalone route-definition validation', () => {
    expect(validateRouteDefinitions(deliveryRoutes)).toEqual([])
  })
})
