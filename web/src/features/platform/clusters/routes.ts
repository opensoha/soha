import { defineRoutes } from '@/routes/definitions'

export const clusterRoutes = defineRoutes([
  {
    meta: {
      id: 'clusters',
      path: '/clusters',
      title: '集群',
      description: '集群生命周期管理',
      icon: 'IconGlobe',
      group: 'platform',
      requiresAuth: true,
      tabbar: true,
      navVisible: true,
      menuId: 'clusters',
      permissionKey: 'platform.clusters.view',
      scopeMode: 'passive',
      workspace: 'resource',
    },
    shell: 'app',
    load: async () => {
      const module = await import('./list-page')
      return { default: module.ClustersPage }
    },
  },
  {
    meta: {
      id: 'cluster-detail',
      path: '/clusters/:clusterId',
      title: '集群详情',
      description: '集群详情',
      requiresAuth: true,
      tabbar: false,
      navVisible: false,
      scopeMode: 'passive',
    },
    shell: 'app',
    inheritMetaFrom: 'clusters',
    load: async () => {
      const module = await import('./detail-page')
      return { default: module.ClusterDetailPage }
    },
  },
] as const)
