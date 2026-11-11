import { defineRoutes } from '@/routes/definitions'

export const observabilityProviderRoutes = defineRoutes([
  {
    meta: {
      id: 'monitoring-workbench-providers',
      path: '/monitoring-workbench/providers',
      title: 'Provider',
      description: '可观测性 Provider 与信号能力目录',
      icon: 'IconPlugConnected',
      group: 'observe',
      workbenchId: 'monitoring',
      requiresAuth: true,
      tabbar: true,
      navVisible: true,
      parentId: 'monitoring-workbench',
      menuId: 'monitoring-workbench-providers',
      permissionKey: 'observe.monitoring.view',
      scopeMode: 'passive',
    },
    shell: 'app',
    load: async () => {
      const module = await import('./page')
      return { default: module.ObservabilityProvidersPage }
    },
  },
] as const)
