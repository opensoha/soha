import { defineRoutes } from '@/routes/definitions'

export const observabilityLogRoutes = defineRoutes([
  {
    meta: {
      id: 'monitoring-workbench-logs',
      path: '/monitoring-workbench/logs',
      title: '日志',
      description: '跨 Pod 聚合实时日志',
      icon: 'IconFileSearch',
      group: 'observe',
      workbenchId: 'monitoring',
      requiresAuth: true,
      tabbar: true,
      navVisible: true,
      parentId: 'monitoring-workbench',
      menuId: 'monitoring-workbench-logs',
      permissionKey: 'observe.monitoring.view',
      scopeMode: 'passive',
    },
    shell: 'app',
    load: async () => {
      const module = await import('./page')
      return { default: module.LogsPage }
    },
  },
  {
    meta: {
      id: 'monitoring-workbench-log-data-sources',
      path: '/monitoring-workbench/log-data-sources',
      title: '日志数据源',
      description: '持久化日志后端与查询边界',
      icon: 'IconServer',
      group: 'observe',
      workbenchId: 'monitoring',
      requiresAuth: true,
      tabbar: true,
      navVisible: true,
      parentId: 'monitoring-workbench',
      menuId: 'monitoring-workbench-log-data-sources',
      permissionKey: 'observe.log-data-sources.view',
      scopeMode: 'passive',
    },
    shell: 'app',
    load: async () => {
      const module = await import('./data-sources-page')
      return { default: module.LogDataSourcesPage }
    },
  },
] as const)
