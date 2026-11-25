import { defineRoutes } from '@/routes/definitions'

export const internalWorkbenchAppHostingRoutes = defineRoutes([
  {
    meta: {
      id: 'internal-workbench-app-hosting',
      path: '/internal-workbench/app-hosting',
      title: 'App Hosting',
      description: '应用托管',
      icon: 'IconGridView',
      group: 'identity',
      workbenchId: 'security',
      requiresAuth: true,
      tabbar: true,
      navVisible: true,
      parentId: 'internal-workbench',
      menuId: 'identity-app-hosting',
      permissionKeysAny: ['apphosting.admin.view', 'apphosting.admin.manage'],
      scopeMode: 'passive',
      workspace: 'system',
    },
    shell: 'app',
    load: async () => {
      const module = await import('./page')
      return { default: module.InternalWorkbenchAppHostingPage }
    },
  },
] as const)
