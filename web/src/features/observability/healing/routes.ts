import { defineRoutes } from '@/routes/definitions'

export const observabilityHealingRoutes = defineRoutes([
  {
    meta: {
      id: 'monitoring-workbench-healing',
      path: '/monitoring-workbench/healing',
      title: '自愈中心',
      description: '告警自愈策略与执行审批',
      icon: 'IconTool',
      group: 'observe',
      workbenchId: 'monitoring',
      requiresAuth: true,
      tabbar: true,
      navVisible: true,
      parentId: 'monitoring-workbench',
      menuId: 'monitoring-workbench-healing',
      permissionKey: 'observe.healing.view',
      scopeMode: 'passive',
    },
    shell: 'app',
    load: async () => {
      const module = await import('./page')
      return { default: module.HealingPage }
    },
  },
] as const)
