import { defineRoutes } from '@/routes/definitions'

export const workloadViewPermissions = [
  'platform.workloads.overview.view',
  'platform.deployment.view',
  'platform.pods.view',
  'platform.workloads.replica-sets.view',
  'platform.workloads.replication-controllers.view',
  'platform.workloads.stateful-sets.view',
  'platform.workloads.daemon-sets.view',
  'platform.workloads.jobs.view',
  'platform.workloads.cron-jobs.view',
]

export const workloadRoutes = defineRoutes([
  {
    meta: {
      id: 'workloads-overview',
      path: '/workloads/overview',
      title: 'Overview',
      icon: 'IconGridView',
      group: 'platform',
      requiresAuth: true,
      tabbar: true,
      navVisible: true,
      parentId: 'workloads',
      menuId: 'workloads-overview',
      permissionKey: 'platform.workloads.overview.view',
      scopeMode: 'namespace',
      workspace: 'resource',
    },
    shell: 'app',
    load: async () => {
      const module = await import('./overview/page')
      return { default: module.WorkloadsOverviewPage }
    },
  },
  {
    meta: {
      id: 'workloads-deployments',
      path: '/workloads/deployments',
      title: 'Deployments',
      icon: 'IconGridView',
      group: 'platform',
      requiresAuth: true,
      tabbar: true,
      navVisible: true,
      parentId: 'workloads',
      menuId: 'workloads-deployments',
      permissionKey: 'platform.deployment.view',
      scopeMode: 'namespace',
      workspace: 'resource',
    },
    shell: 'app',
    load: async () => {
      const module = await import('./deployments/list-page')
      return { default: module.WorkloadsDeploymentsPage }
    },
  },
  {
    meta: {
      id: 'workloads-deployment-detail',
      path: '/workloads/deployments/:deploymentName',
      title: 'Deployment Detail',
      requiresAuth: true,
      tabbar: false,
      navVisible: false,
      scopeMode: 'namespace',
    },
    shell: 'app',
    inheritMetaFrom: 'workloads-deployments',
    load: async () => {
      const module = await import('./deployments/detail-page')
      return { default: module.DeploymentDetailPage }
    },
  },
  {
    meta: {
      id: 'workloads-pods',
      path: '/workloads/pods',
      title: 'Pods',
      icon: 'IconGridView',
      group: 'platform',
      requiresAuth: true,
      tabbar: true,
      navVisible: true,
      parentId: 'workloads',
      menuId: 'workloads-pods',
      permissionKey: 'platform.pods.view',
      scopeMode: 'namespace',
      workspace: 'resource',
    },
    shell: 'app',
    load: async () => {
      const module = await import('./pods/list-page')
      return { default: module.WorkloadsPodsPage }
    },
  },
  {
    meta: {
      id: 'workloads-pod-detail',
      path: '/workloads/pods/:podName',
      title: 'Pod Detail',
      requiresAuth: true,
      tabbar: false,
      navVisible: false,
      scopeMode: 'namespace',
    },
    shell: 'app',
    inheritMetaFrom: 'workloads-pods',
    load: async () => {
      const module = await import('./pods/detail-page')
      return { default: module.PodDetailPage }
    },
  },
  {
    meta: {
      id: 'workloads-replicasets',
      path: '/workloads/replicasets',
      title: 'ReplicaSets',
      icon: 'IconGridView',
      group: 'platform',
      requiresAuth: true,
      tabbar: true,
      navVisible: true,
      parentId: 'workloads',
      menuId: 'workloads-replicasets',
      permissionKey: 'platform.workloads.replica-sets.view',
      scopeMode: 'namespace',
      workspace: 'resource',
    },
    shell: 'app',
    load: async () => {
      const module = await import('./replicasets/list-page')
      return { default: module.WorkloadsReplicaSetsPage }
    },
  },
  {
    meta: {
      id: 'workloads-replicaset-detail',
      path: '/workloads/replicasets/:replicaSetName',
      title: 'ReplicaSet Detail',
      requiresAuth: true,
      tabbar: false,
      navVisible: false,
      scopeMode: 'namespace',
    },
    shell: 'app',
    inheritMetaFrom: 'workloads-replicasets',
    load: async () => {
      const module = await import('./replicasets/detail-page')
      return { default: module.ReplicaSetDetailPage }
    },
  },
  {
    meta: {
      id: 'workloads-replicationcontrollers',
      path: '/workloads/replicationcontrollers',
      title: 'ReplicationControllers',
      icon: 'IconGridView',
      group: 'platform',
      requiresAuth: true,
      tabbar: true,
      navVisible: true,
      parentId: 'workloads',
      menuId: 'workloads-replicationcontrollers',
      permissionKey: 'platform.workloads.replication-controllers.view',
      scopeMode: 'namespace',
      workspace: 'resource',
    },
    shell: 'app',
    load: async () => {
      const module = await import('./replicationcontrollers/list-page')
      return { default: module.WorkloadsReplicationControllersPage }
    },
  },
  {
    meta: {
      id: 'workloads-replicationcontroller-detail',
      path: '/workloads/replicationcontrollers/:replicationControllerName',
      title: 'ReplicationController Detail',
      requiresAuth: true,
      tabbar: false,
      navVisible: false,
      scopeMode: 'namespace',
    },
    shell: 'app',
    inheritMetaFrom: 'workloads-replicationcontrollers',
    load: async () => {
      const module = await import('./replicationcontrollers/detail-page')
      return { default: module.ReplicationControllerDetailPage }
    },
  },
  {
    meta: {
      id: 'workloads-statefulsets',
      path: '/workloads/statefulsets',
      title: 'StatefulSets',
      icon: 'IconGridView',
      group: 'platform',
      requiresAuth: true,
      tabbar: true,
      navVisible: true,
      parentId: 'workloads',
      menuId: 'workloads-statefulsets',
      permissionKey: 'platform.workloads.stateful-sets.view',
      scopeMode: 'namespace',
      workspace: 'resource',
    },
    shell: 'app',
    load: async () => {
      const module = await import('./statefulsets/list-page')
      return { default: module.WorkloadsStatefulSetsPage }
    },
  },
  {
    meta: {
      id: 'workloads-statefulset-detail',
      path: '/workloads/statefulsets/:statefulSetName',
      title: 'StatefulSet Detail',
      requiresAuth: true,
      tabbar: false,
      navVisible: false,
      scopeMode: 'namespace',
    },
    shell: 'app',
    inheritMetaFrom: 'workloads-statefulsets',
    load: async () => {
      const module = await import('./statefulsets/detail-page')
      return { default: module.StatefulSetDetailPage }
    },
  },
  {
    meta: {
      id: 'workloads-daemonsets',
      path: '/workloads/daemonsets',
      title: 'DaemonSets',
      icon: 'IconGridView',
      group: 'platform',
      requiresAuth: true,
      tabbar: true,
      navVisible: true,
      parentId: 'workloads',
      menuId: 'workloads-daemonsets',
      permissionKey: 'platform.workloads.daemon-sets.view',
      scopeMode: 'namespace',
      workspace: 'resource',
    },
    shell: 'app',
    load: async () => {
      const module = await import('./daemonsets/list-page')
      return { default: module.WorkloadsDaemonSetsPage }
    },
  },
  {
    meta: {
      id: 'workloads-daemonset-detail',
      path: '/workloads/daemonsets/:daemonSetName',
      title: 'DaemonSet Detail',
      requiresAuth: true,
      tabbar: false,
      navVisible: false,
      scopeMode: 'namespace',
    },
    shell: 'app',
    inheritMetaFrom: 'workloads-daemonsets',
    load: async () => {
      const module = await import('./daemonsets/detail-page')
      return { default: module.DaemonSetDetailPage }
    },
  },
  {
    meta: {
      id: 'workloads-jobs',
      path: '/workloads/jobs',
      title: 'Jobs',
      icon: 'IconGridView',
      group: 'platform',
      requiresAuth: true,
      tabbar: true,
      navVisible: true,
      parentId: 'workloads',
      menuId: 'workloads-jobs',
      permissionKey: 'platform.workloads.jobs.view',
      scopeMode: 'namespace',
      workspace: 'resource',
    },
    shell: 'app',
    load: async () => {
      const module = await import('./jobs/list-page')
      return { default: module.WorkloadsJobsPage }
    },
  },
  {
    meta: {
      id: 'workloads-job-detail',
      path: '/workloads/jobs/:jobName',
      title: 'Job Detail',
      requiresAuth: true,
      tabbar: false,
      navVisible: false,
      scopeMode: 'namespace',
    },
    shell: 'app',
    inheritMetaFrom: 'workloads-jobs',
    load: async () => {
      const module = await import('./jobs/detail-page')
      return { default: module.JobDetailPage }
    },
  },
  {
    meta: {
      id: 'workloads-cronjobs',
      path: '/workloads/cronjobs',
      title: 'CronJobs',
      icon: 'IconGridView',
      group: 'platform',
      requiresAuth: true,
      tabbar: true,
      navVisible: true,
      parentId: 'workloads',
      menuId: 'workloads-cronjobs',
      permissionKey: 'platform.workloads.cron-jobs.view',
      scopeMode: 'namespace',
      workspace: 'resource',
    },
    shell: 'app',
    load: async () => {
      const module = await import('./cronjobs/list-page')
      return { default: module.WorkloadsCronJobsPage }
    },
  },
  {
    meta: {
      id: 'workloads-cronjob-detail',
      path: '/workloads/cronjobs/:cronJobName',
      title: 'CronJob Detail',
      requiresAuth: true,
      tabbar: false,
      navVisible: false,
      scopeMode: 'namespace',
    },
    shell: 'app',
    inheritMetaFrom: 'workloads-cronjobs',
    load: async () => {
      const module = await import('./cronjobs/detail-page')
      return { default: module.CronJobDetailPage }
    },
  },
] as const)
