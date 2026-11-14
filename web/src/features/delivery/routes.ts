import { defineRoutes } from '@/routes/definitions'

export const deliveryRoutes = defineRoutes([
  {
    meta: {
      id: 'delivery-workflows',
      path: '/delivery/workflows',
      title: '发布工作流',
      description: '跨应用服务编排与发布',
      icon: 'IconFlow',
      group: 'delivery',
      workbenchId: 'delivery',
      requiresAuth: true,
      tabbar: false,
      navVisible: false,
      permissionKey: 'delivery.workflows.view',
      scopeMode: 'passive',
      workspace: 'application',
    },
    shell: 'app',
    load: async () => ({ default: (await import('./workflows/list-page')).DeliveryWorkflowsPage }),
  },
  {
    meta: {
      id: 'delivery-batches',
      path: '/delivery/batches',
      title: '交付记录',
      description: '统一交付进度与历史',
      icon: 'IconHistory',
      group: 'delivery',
      workbenchId: 'delivery',
      requiresAuth: true,
      tabbar: false,
      navVisible: false,
      permissionKey: 'delivery.workflows.view',
      scopeMode: 'passive',
      workspace: 'application',
    },
    shell: 'app',
    load: async () => ({ default: (await import('./batches/list-page')).DeliveryBatchesPage }),
  },
  {
    meta: {
      id: 'delivery-batch-detail',
      path: '/delivery/batches/:batchId',
      title: '交付详情',
      description: '目标阶段、计划、日志与产物',
      icon: 'IconHistory',
      group: 'delivery',
      workbenchId: 'delivery',
      requiresAuth: true,
      tabbar: false,
      navVisible: false,
      parentId: 'execution-history',
      permissionKey: 'delivery.workflows.view',
      scopeMode: 'passive',
      workspace: 'application',
    },
    shell: 'app',
    load: async () => ({
      default: (await import('./batches/detail-page')).DeliveryBatchDetailPage,
    }),
  },
  {
    meta: {
      id: 'delivery-manifest-library',
      path: '/delivery/manifests',
      title: '应用清单库',
      description: '应用 YAML、环境差异与不可变版本',
      icon: 'IconCode',
      group: 'delivery',
      workbenchId: 'delivery',
      requiresAuth: true,
      tabbar: false,
      navVisible: false,
      permissionKey: 'delivery.applications.view',
      scopeMode: 'passive',
      workspace: 'application',
    },
    shell: 'app',
    load: async () => {
      const module = await import('./manifests/library-page')
      return { default: module.ManifestLibraryPage }
    },
  },
  {
    meta: {
      id: 'applications',
      path: '/applications',
      title: '应用中心',
      description: '应用入口视角',
      icon: 'IconAppCenter',
      group: 'delivery',
      workbenchId: 'delivery',
      requiresAuth: true,
      tabbar: true,
      navVisible: true,
      menuId: 'builds',
      permissionKey: 'delivery.applications.view',
      scopeMode: 'passive',
      workspace: 'application',
    },
    shell: 'app',
    load: async () => {
      const module = await import('./applications/list-page')
      return { default: module.ApplicationsPage }
    },
  },
  {
    meta: {
      id: 'delivery-overview',
      path: '/delivery/overview',
      title: '总览',
      description: '应用、环境、发布与执行态势',
      icon: 'IconGauge',
      group: 'delivery',
      workbenchId: 'delivery',
      requiresAuth: true,
      tabbar: true,
      navVisible: true,
      menuId: 'delivery-overview',
      permissionKeysAny: [
        'delivery.applications.view',
        'delivery.application-environments.view',
        'delivery.release-board.view',
        'delivery.release-bundles.view',
        'delivery.execution-tasks.view',
      ],
      scopeMode: 'passive',
      workspace: 'application',
    },
    shell: 'app',
    load: async () => {
      const module = await import('./workbench/overview-page')
      return { default: module.DeliveryOverviewPage }
    },
  },
  {
    meta: {
      id: 'application-detail',
      path: '/applications/:applicationId',
      title: '应用详情',
      description: '应用运行详情',
      icon: 'IconAppCenter',
      group: 'delivery',
      requiresAuth: true,
      tabbar: false,
      navVisible: false,
      parentId: 'applications',
      scopeMode: 'passive',
    },
    shell: 'app',
    permissionExemptReason:
      'Application detail authorization is enforced by the parent resource and backend.',
    load: async () => {
      const module = await import('./applications/detail-page')
      return { default: module.ApplicationDetailPage }
    },
  },
  {
    meta: {
      id: 'application-workload-detail',
      path: '/applications/:applicationId/application-environments/:applicationEnvironmentId/workloads/:workloadName',
      title: '服务详情',
      description: '应用服务运行详情',
      icon: 'IconAppCenter',
      group: 'delivery',
      requiresAuth: true,
      tabbar: false,
      navVisible: false,
      parentId: 'application-detail',
      scopeMode: 'passive',
    },
    shell: 'app',
    permissionExemptReason:
      'Workload detail authorization is enforced by the parent application resource and backend.',
    load: async () => {
      const module = await import('./runtime/workload-detail-page')
      return { default: module.ApplicationWorkloadDetailPage }
    },
  },
  {
    meta: {
      id: 'application-workflow-designer',
      path: '/applications/:applicationId/workflows/design',
      title: '工作流设计',
      description: '应用工作流 DAG 设计',
      icon: 'IconFlow',
      group: 'delivery',
      requiresAuth: true,
      tabbar: false,
      navVisible: false,
      parentId: 'application-detail',
      permissionKey: 'delivery.application-environments.update',
      scopeMode: 'passive',
    },
    shell: 'app',
    load: async () => {
      const module = await import('./applications/workflow-designer-page')
      return { default: module.ApplicationWorkflowDesignerPage }
    },
  },
  {
    meta: {
      id: 'application-environments',
      path: '/application-environments',
      title: '应用环境绑定',
      description: '应用与环境绑定',
      icon: 'IconAppCenter',
      group: 'delivery',
      requiresAuth: true,
      tabbar: false,
      navVisible: false,
      permissionKey: 'delivery.application-environments.view',
      scopeMode: 'passive',
      workspace: 'application',
    },
    shell: 'app',
    load: async () => {
      const module = await import('./environments/list-page')
      return { default: module.ApplicationEnvironmentsPage }
    },
  },
  {
    meta: {
      id: 'delivery-environments',
      path: '/delivery/environments',
      title: '环境目录',
      description: '可复用交付环境与应用使用关系',
      icon: 'IconAppCenter',
      group: 'delivery',
      workbenchId: 'delivery',
      requiresAuth: true,
      tabbar: true,
      navVisible: true,
      menuId: 'delivery-environment-directory',
      permissionKey: 'delivery.application-environments.view',
      scopeMode: 'passive',
      workspace: 'application',
    },
    shell: 'app',
    load: async () => {
      const module = await import('./environments/catalog-page')
      return { default: module.EnvironmentCatalogPage }
    },
  },
  {
    meta: {
      id: 'deployment-templates',
      path: '/deployment-templates',
      title: '部署模板',
      description: '平台部署模板',
      icon: 'IconCode',
      group: 'delivery',
      requiresAuth: true,
      tabbar: true,
      navVisible: true,
      menuId: 'deployment-templates',
      permissionKey: 'delivery.deployment-templates.view',
      scopeMode: 'passive',
      workspace: 'application',
    },
    shell: 'app',
    load: async () => {
      const module = await import('./deployment-templates/page')
      return { default: module.DeploymentTemplatesPage }
    },
  },
  {
    meta: {
      id: 'build-templates',
      path: '/build-templates',
      title: '构建模板',
      description: '平台构建模板',
      icon: 'IconCode',
      group: 'delivery',
      requiresAuth: true,
      tabbar: true,
      navVisible: true,
      menuId: 'build-templates',
      permissionKey: 'delivery.build-templates.view',
      scopeMode: 'passive',
      workspace: 'application',
    },
    shell: 'app',
    load: async () => {
      const module = await import('./build-templates/page')
      return { default: module.BuildTemplatesPage }
    },
  },
  {
    meta: {
      id: 'delivery-blueprints',
      path: '/delivery/blueprints',
      title: '应用接入模板',
      description: '应用接入模板、规范渲染与平台编排入口',
      icon: 'IconCode',
      group: 'delivery',
      workbenchId: 'delivery',
      requiresAuth: true,
      tabbar: true,
      navVisible: true,
      menuId: 'delivery-blueprints',
      permissionKey: 'delivery.applications.view',
      scopeMode: 'passive',
      workspace: 'application',
    },
    shell: 'app',
    load: async () => {
      const module = await import('./blueprints/page')
      return { default: module.DeliveryBlueprintsPage }
    },
  },
  {
    meta: {
      id: 'delivery-onboarding',
      path: '/delivery/onboarding',
      title: '创建应用',
      description: '旧接入地址转到应用创建入口',
      icon: 'IconCode',
      group: 'delivery',
      workbenchId: 'delivery',
      requiresAuth: true,
      tabbar: false,
      navVisible: false,
      permissionKey: 'delivery.applications.view',
      scopeMode: 'passive',
      workspace: 'application',
    },
    shell: 'app',
    load: async () => {
      const module = await import('./applications/legacy-redirect')
      return { default: module.ApplicationCreateRedirect }
    },
  },
  {
    meta: {
      id: 'delivery-testing',
      path: '/delivery/testing',
      title: '测试验证',
      description: '候选版本、验证结果、测试证据和晋级判断入口',
      icon: 'IconShield',
      group: 'delivery',
      workbenchId: 'delivery',
      requiresAuth: true,
      tabbar: true,
      navVisible: true,
      menuId: 'delivery-testing',
      permissionKeysAny: [
        'delivery.release-bundles.view',
        'delivery.execution-tasks.view',
        'delivery.release-board.view',
      ],
      scopeMode: 'passive',
      workspace: 'application',
    },
    shell: 'app',
    load: async () => {
      const module = await import('./workbench/testing-page')
      return { default: module.DeliveryTestingPage }
    },
  },
  {
    meta: {
      id: 'delivery-analysis',
      path: '/delivery/analysis',
      title: '问题分析',
      description: '失败任务、日志证据、影响范围和修复建议入口',
      icon: 'IconFlow',
      group: 'delivery',
      workbenchId: 'delivery',
      requiresAuth: true,
      tabbar: true,
      navVisible: true,
      menuId: 'delivery-analysis',
      permissionKeysAny: [
        'delivery.execution-tasks.view',
        'delivery.release-board.view',
        'delivery.release-bundles.view',
      ],
      scopeMode: 'passive',
      workspace: 'application',
    },
    shell: 'app',
    load: async () => {
      const module = await import('./workbench/analysis-page')
      return { default: module.DeliveryAnalysisPage }
    },
  },
  {
    meta: {
      id: 'release-bundles',
      path: '/delivery/release-bundles',
      title: '版本包',
      description: '不可变交付版本包',
      icon: 'IconInbox',
      group: 'delivery',
      requiresAuth: true,
      tabbar: true,
      navVisible: true,
      menuId: 'release-bundles',
      permissionKey: 'delivery.release-bundles.view',
      scopeMode: 'passive',
      workspace: 'application',
    },
    shell: 'app',
    load: async () => {
      const module = await import('./release-bundles/list-page')
      return { default: module.ReleaseBundlesPage }
    },
  },
  {
    meta: {
      id: 'release-bundles-detail',
      path: '/delivery/release-bundles/:releaseBundleId',
      title: '版本包详情',
      description: '版本包运行态详情',
      icon: 'IconInbox',
      group: 'delivery',
      requiresAuth: true,
      tabbar: false,
      navVisible: false,
      parentId: 'release-bundles',
      permissionKey: 'delivery.release-bundles.view',
      scopeMode: 'passive',
      workspace: 'application',
    },
    shell: 'app',
    load: async () => {
      const module = await import('./release-bundles/detail-page')
      return { default: module.ReleaseBundleDetailPage }
    },
  },
  {
    meta: {
      id: 'execution-tasks',
      path: '/delivery/execution-tasks',
      title: '执行队列',
      description: '底层执行任务、日志与重试',
      icon: 'IconFlow',
      group: 'delivery',
      requiresAuth: true,
      tabbar: false,
      navVisible: false,
      permissionKey: 'delivery.execution-tasks.view',
      scopeMode: 'passive',
      workspace: 'application',
    },
    shell: 'app',
    load: async () => {
      const module = await import('./execution-tasks/list-page')
      return { default: module.ExecutionTasksPage }
    },
  },
  {
    meta: {
      id: 'execution-tasks-detail',
      path: '/delivery/execution-tasks/:executionTaskId',
      title: '执行任务详情',
      description: '执行任务运行态详情',
      icon: 'IconFlow',
      group: 'delivery',
      requiresAuth: true,
      tabbar: false,
      navVisible: false,
      parentId: 'execution-tasks',
      permissionKey: 'delivery.execution-tasks.view',
      scopeMode: 'passive',
      workspace: 'application',
    },
    shell: 'app',
    load: async () => {
      const module = await import('./execution-tasks/detail-page')
      return { default: module.ExecutionTaskDetailPage }
    },
  },
  {
    meta: {
      id: 'workflow-templates',
      path: '/workflow-templates',
      title: '发布流程模板',
      description: '交付发布流程模板',
      icon: 'IconFlow',
      group: 'delivery',
      requiresAuth: true,
      tabbar: true,
      navVisible: true,
      menuId: 'workflow-templates',
      permissionKey: 'delivery.workflow-templates.view',
      scopeMode: 'passive',
      workspace: 'application',
    },
    shell: 'app',
    load: async () => {
      const module = await import('./workflow-templates/page')
      return { default: module.WorkflowTemplatesPage }
    },
  },
  {
    meta: {
      id: 'release-board',
      path: '/release-board',
      title: '工作流中心',
      description: '管理工作流、配置构建发布步骤、查看最近执行',
      icon: 'IconSend',
      group: 'delivery',
      requiresAuth: true,
      tabbar: true,
      navVisible: true,
      menuId: 'release-board',
      permissionKeysAny: ['delivery.workflows.view', 'delivery.applications.view'],
      scopeMode: 'passive',
      workspace: 'application',
    },
    shell: 'app',
    load: async () => {
      const module = await import('./release-board/workflow-catalog')
      return { default: module.WorkflowCatalog }
    },
  },
  {
    meta: {
      id: 'execution-history',
      path: '/execution-history',
      title: '执行记录',
      description: '工作流执行与构建历史',
      icon: 'IconHistory',
      group: 'delivery',
      requiresAuth: true,
      tabbar: true,
      navVisible: true,
      menuId: 'execution-history',
      permissionKeysAny: ['delivery.workflows.view', 'delivery.applications.view'],
      scopeMode: 'passive',
      workspace: 'application',
    },
    shell: 'app',
    load: async () => ({ default: (await import('./release-board/page')).ExecutionHistoryPage }),
  },
  {
    meta: {
      id: 'workflows-detail',
      path: '/workflows/:workflowId',
      title: '工作流详情',
      description: '工作流运行态详情',
      icon: 'IconFlow',
      group: 'delivery',
      requiresAuth: true,
      tabbar: false,
      navVisible: false,
      parentId: 'execution-history',
      permissionKey: 'delivery.workflows.view',
      scopeMode: 'passive',
      workspace: 'application',
    },
    shell: 'app',
    load: async () => {
      const module = await import('./workflows/detail-page')
      return { default: module.WorkflowDetailPage }
    },
  },
  {
    meta: {
      id: 'releases',
      path: '/releases',
      title: '发布记录',
      description: '发布结果、环境变更和回滚记录',
      icon: 'IconSend',
      group: 'delivery',
      requiresAuth: true,
      tabbar: true,
      navVisible: true,
      menuId: 'releases',
      permissionKey: 'delivery.releases.view',
      scopeMode: 'passive',
      workspace: 'application',
    },
    shell: 'app',
    load: async () => {
      const module = await import('./releases/list-page')
      return { default: module.ReleasesPage }
    },
  },
  {
    meta: {
      id: 'releases-detail',
      path: '/releases/:releaseId',
      title: '发布详情',
      description: '发布运行态详情',
      icon: 'IconSend',
      group: 'delivery',
      requiresAuth: true,
      tabbar: false,
      navVisible: false,
      parentId: 'releases',
      permissionKey: 'delivery.releases.view',
      scopeMode: 'passive',
      workspace: 'application',
    },
    shell: 'app',
    load: async () => {
      const module = await import('./releases/detail-page')
      return { default: module.ReleaseDetailPage }
    },
  },
  {
    meta: {
      id: 'builds-detail',
      path: '/builds/:buildId',
      title: '构建详情',
      description: '构建运行态详情',
      icon: 'IconCode',
      group: 'delivery',
      requiresAuth: true,
      tabbar: false,
      navVisible: false,
      parentId: 'execution-history',
      permissionKey: 'delivery.applications.view',
      scopeMode: 'passive',
      workspace: 'application',
    },
    shell: 'app',
    load: async () => {
      const module = await import('./builds/detail-page')
      return { default: module.BuildDetailPage }
    },
  },
  {
    meta: {
      id: 'registries',
      path: '/registries',
      title: '镜像仓库',
      description: '镜像仓库连接',
      icon: 'IconInbox',
      group: 'delivery',
      requiresAuth: true,
      tabbar: true,
      navVisible: true,
      menuId: 'registries',
      permissionKey: 'delivery.registries.view',
      scopeMode: 'passive',
      workspace: 'application',
    },
    shell: 'app',
    load: async () => {
      const module = await import('./registries/page')
      return { default: module.RegistriesPage }
    },
  },
] as const)
