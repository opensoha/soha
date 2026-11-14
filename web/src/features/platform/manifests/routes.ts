import { defineRoutes } from '@/routes/definitions'

export const manifestRoutes = defineRoutes([
  {
    meta: {
      id: 'platform-manifests',
      path: '/manifests',
      title: '应用清单',
      description: '当前集群与命名空间中的应用清单',
      icon: 'IconCode',
      group: 'platform',
      workbenchId: 'platform',
      requiresAuth: true,
      tabbar: true,
      navVisible: true,
      menuId: 'platform-manifests',
      permissionKey: 'delivery.applications.view',
      scopeMode: 'namespace',
      workspace: 'resource',
    },
    shell: 'app',
    load: async () => {
      const module = await import('./page')
      return { default: module.PlatformManifestsPage }
    },
  },
] as const)
