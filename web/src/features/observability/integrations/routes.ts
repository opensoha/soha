import { defineRoutes } from '@/routes/definitions'

export const observabilityIntegrationRoutes = defineRoutes([
  {
    meta: {
      id: 'monitoring-workbench-integrations',
      path: '/monitoring-workbench/integrations',
      title: '告警集成',
      description: 'Alertmanager、Grafana Alerting 与通用 Webhook 接入',
      icon: 'IconConnection',
      group: 'observe',
      workbenchId: 'monitoring',
      requiresAuth: true,
      tabbar: true,
      navVisible: true,
      parentId: 'monitoring-workbench',
      menuId: 'monitoring-workbench-integrations',
      permissionKey: 'observe.alert-integrations.view',
      scopeMode: 'passive',
    },
    shell: 'app',
    load: async () => {
      const module = await import('./page')
      return { default: module.AlertIntegrationsPage }
    },
  },
] as const)
