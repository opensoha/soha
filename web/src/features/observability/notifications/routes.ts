import { defineRoutes } from '@/routes/definitions'

export const observabilityNotificationRoutes = defineRoutes([
  {
    meta: {
      id: 'monitoring-workbench-notifications',
      path: '/monitoring-workbench/notifications',
      title: '通知策略',
      description: '通知渠道与路由策略',
      icon: 'IconBell',
      group: 'observe',
      workbenchId: 'monitoring',
      requiresAuth: true,
      tabbar: true,
      navVisible: true,
      parentId: 'monitoring-workbench',
      menuId: 'monitoring-workbench-notifications',
      permissionKey: 'observe.notifications.view',
      scopeMode: 'passive',
    },
    shell: 'app',
    load: async () => {
      const module = await import('./page')
      return { default: module.NotificationsPage }
    },
  },
] as const)
