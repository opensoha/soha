import { defineRoutes } from '@/routes/definitions'

export const clusterResourceRoutes = defineRoutes([
  {
    meta: {
      id: 'cluster-resources',
      path: '/cluster-resources',
      title: '集群资源',
      description: '集群节点与命名空间入口',
      icon: 'IconServer',
      group: 'platform',
      requiresAuth: true,
      tabbar: false,
      navVisible: false,
      permissionKey: 'platform.nodes.view',
      scopeMode: 'cluster',
      workspace: 'resource',
    },
    shell: 'app',
    redirectTo: '/cluster-resources/nodes',
  },
  {
    meta: {
      id: 'cluster-resources-nodes',
      path: '/cluster-resources/nodes',
      title: '节点',
      description: '节点管理',
      icon: 'IconServer',
      group: 'platform',
      requiresAuth: true,
      tabbar: true,
      navVisible: true,
      menuId: 'cluster-resources-nodes',
      permissionKey: 'platform.nodes.view',
      scopeMode: 'cluster',
      workspace: 'resource',
    },
    shell: 'app',
    load: async () => {
      const module = await import('./nodes-list-page')
      return { default: module.ClusterNodesPage }
    },
  },
  {
    meta: {
      id: 'cluster-resources-node-detail',
      path: '/cluster-resources/nodes/:nodeName',
      title: '节点详情',
      description: '节点详情',
      requiresAuth: true,
      tabbar: false,
      navVisible: false,
      scopeMode: 'cluster',
    },
    shell: 'app',
    inheritMetaFrom: 'cluster-resources-nodes',
    load: async () => {
      const module = await import('./node-detail-page')
      return { default: module.NodeDetailPage }
    },
  },
  {
    meta: {
      id: 'cluster-resources-namespaces',
      path: '/cluster-resources/namespaces',
      title: '命名空间',
      description: '命名空间管理',
      icon: 'IconServer',
      group: 'platform',
      requiresAuth: true,
      tabbar: true,
      navVisible: true,
      menuId: 'cluster-resources-namespaces',
      permissionKey: 'platform.namespaces.view',
      scopeMode: 'cluster',
      workspace: 'resource',
    },
    shell: 'app',
    load: async () => {
      const module = await import('./namespaces-list-page')
      return { default: module.ClusterNamespacesPage }
    },
  },
] as const)
