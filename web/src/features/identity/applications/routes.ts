import { defineRoutes } from '@/routes/definitions'

export const identityApplicationRoutes = defineRoutes([
  {
    meta: {
      id: 'identity-applications',
      path: '/identity/applications',
      title: 'Applications',
      description: 'Provider Portal 应用目录',
      icon: 'IconGridView',
      group: 'identity',
      workbenchId: 'security',
      requiresAuth: true,
      tabbar: true,
      navVisible: true,
      parentId: 'internal-workbench',
      menuId: 'identity-applications',
      permissionKey: 'identity.applications.view',
      scopeMode: 'passive',
      workspace: 'system',
    },
    shell: 'app',
    load: async () => {
      const module = await import('./list-page')
      return { default: module.IdentityApplicationsPage }
    },
  },
] as const)
