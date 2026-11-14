import { defineRoutes } from '@/routes/definitions'

export const observabilityEventRoutes = defineRoutes([
  {
    meta: {
      id: 'monitoring-workbench-events',
      path: '/monitoring-workbench/events',
      title: '事件流',
      description: '事件时间线与上下文',
      icon: 'IconBell',
      group: 'observe',
      workbenchId: 'monitoring',
      requiresAuth: true,
      tabbar: true,
      navVisible: true,
      parentId: 'monitoring-workbench',
      menuId: 'monitoring-workbench-events',
      permissionKey: 'observe.events.view',
      scopeMode: 'passive',
    },
    shell: 'app',
    load: async () => {
      const module = await import('./page')
      return { default: module.EventsPage }
    },
  },
] as const)
