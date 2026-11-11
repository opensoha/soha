import { defineRoutes } from '@/routes/definitions'

export const observabilityDashboardRoutes = defineRoutes([
  {
    meta: {
      id: 'monitoring-workbench-dashboards',
      path: '/monitoring-workbench/dashboards',
      title: '仪表盘',
      description: '导入并查看 Grafana 监控仪表盘',
      icon: 'IconPulse',
      group: 'observe',
      workbenchId: 'monitoring',
      requiresAuth: true,
      tabbar: true,
      navVisible: true,
      parentId: 'monitoring-workbench',
      menuId: 'monitoring-workbench-dashboards',
      permissionKey: 'observe.monitoring.view',
      scopeMode: 'passive',
    },
    shell: 'app',
    load: async () => {
      const module = await import('./list-page')
      return { default: module.ObservabilityDashboardsPage }
    },
  },
  {
    meta: {
      id: 'monitoring-workbench-dashboard-detail',
      path: '/monitoring-workbench/dashboards/:dashboardId',
      title: '仪表盘详情',
      description: '监控仪表盘面板',
      icon: 'IconPulse',
      group: 'observe',
      workbenchId: 'monitoring',
      requiresAuth: true,
      tabbar: false,
      navVisible: false,
      parentId: 'monitoring-workbench-dashboards',
      menuId: 'monitoring-workbench-dashboards',
      permissionKey: 'observe.monitoring.view',
      scopeMode: 'passive',
    },
    shell: 'app',
    load: async () => {
      const module = await import('./detail-page')
      return { default: module.ObservabilityDashboardDetailPage }
    },
  },
] as const)
