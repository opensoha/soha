import { defineRoutes } from '@/routes/definitions'

export const observabilityRuleRoutes = defineRoutes([
  {
    meta: {
      id: 'monitoring-workbench-rules',
      path: '/monitoring-workbench/rules',
      title: '告警规则',
      description: '告警规则与数据源选择',
      icon: 'IconFileSearch',
      group: 'observe',
      workbenchId: 'monitoring',
      requiresAuth: true,
      tabbar: true,
      navVisible: true,
      parentId: 'monitoring-workbench',
      menuId: 'monitoring-workbench-rules',
      permissionKey: 'observe.alert-rules.view',
      scopeMode: 'passive',
    },
    shell: 'app',
    load: async () => {
      const module = await import('./page')
      return { default: module.AlertRulesPage }
    },
  },
] as const)
