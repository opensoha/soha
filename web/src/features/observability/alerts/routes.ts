import { defineRoutes } from '@/routes/definitions'

export const observabilityAlertRoutes = defineRoutes([
  {
    meta: {
      id: 'monitoring-workbench-alerts',
      path: '/monitoring-workbench/alerts',
      title: '活跃告警',
      description: '当前告警处理面板',
      icon: 'IconAlertTriangle',
      group: 'observe',
      workbenchId: 'monitoring',
      requiresAuth: true,
      tabbar: true,
      navVisible: true,
      parentId: 'monitoring-workbench',
      menuId: 'monitoring-workbench-alerts',
      permissionKey: 'observe.alerts.view',
      scopeMode: 'passive',
    },
    shell: 'app',
    load: async () => {
      const module = await import('./page')
      return { default: module.AlertsPage }
    },
  },
  {
    meta: {
      id: 'alert-event-detail',
      path: '/monitoring-workbench/alerts/:eventId',
      title: '告警事件详情',
      description: '单条告警事件详情',
      icon: 'IconAlertTriangle',
      group: 'observe',
      workbenchId: 'monitoring',
      requiresAuth: true,
      tabbar: false,
      navVisible: false,
      parentId: 'monitoring-workbench-alerts',
      permissionKey: 'observe.alerts.view',
      scopeMode: 'passive',
    },
    shell: 'app',
    load: async () => {
      const module = await import('./detail-page')
      return { default: module.AlertEventDetailPage }
    },
  },
  {
    meta: {
      id: 'observability-alert-event-detail-compat',
      path: '/observability/alerts/:eventId',
      title: '告警事件详情',
      description: '兼容旧告警事件详情入口',
      icon: 'IconAlertTriangle',
      group: 'observe',
      workbenchId: 'monitoring',
      requiresAuth: true,
      tabbar: false,
      navVisible: false,
      parentId: 'monitoring-workbench-alerts',
      permissionKey: 'observe.alerts.view',
      scopeMode: 'passive',
    },
    shell: 'app',
    load: async () => {
      const module = await import('./detail-page')
      return { default: module.AlertEventDetailPage }
    },
  },
] as const)
