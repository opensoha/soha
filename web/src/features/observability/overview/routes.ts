import { defineRoutes } from '@/routes/definitions'

export const observabilityOverviewRoutes = defineRoutes([
  {
    meta: {
      id: 'monitoring-workbench-overview',
      path: '/monitoring-workbench/overview',
      title: '总览',
      description: '服务健康、活跃告警、数据源状态与近期事件',
      icon: 'IconPulse',
      group: 'observe',
      workbenchId: 'monitoring',
      requiresAuth: true,
      tabbar: true,
      navVisible: true,
      parentId: 'monitoring-workbench',
      menuId: 'monitoring-workbench-overview',
      permissionKey: 'observe.monitoring.view',
      scopeMode: 'passive',
    },
    shell: 'app',
    load: async () => {
      const module = await import('./workbench-page')
      return { default: module.ObservabilityWorkbenchPage }
    },
  },
  {
    meta: {
      id: 'monitoring-workbench-alerting',
      path: '/monitoring-workbench/alerting',
      title: '告警总览',
      description: '活动告警、规则与响应链路',
      icon: 'IconAlertTriangle',
      group: 'observe',
      workbenchId: 'monitoring',
      requiresAuth: true,
      tabbar: true,
      navVisible: false,
      parentId: 'monitoring-workbench',
      menuId: 'monitoring-workbench',
      permissionKey: 'observe.monitoring.view',
      scopeMode: 'passive',
    },
    shell: 'app',
    load: async () => {
      const module = await import('./page')
      return { default: module.MonitoringPage }
    },
  },
] as const)
