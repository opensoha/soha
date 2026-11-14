import { defineRoutes } from '@/routes/definitions'

export const identityOutpostRoutes = defineRoutes([
  {
    meta: {
      id: 'identity-outposts',
      path: '/identity/outposts',
      title: 'Outposts',
      description: 'Proxy Provider Outpost 管理',
      icon: 'IconShield',
      group: 'identity',
      workbenchId: 'security',
      requiresAuth: true,
      tabbar: true,
      navVisible: true,
      parentId: 'internal-workbench',
      menuId: 'identity-outposts',
      permissionKey: 'identity.outposts.view',
      scopeMode: 'passive',
      workspace: 'system',
    },
    shell: 'app',
    load: async () => {
      const module = await import('./list-page')
      return { default: module.IdentityOutpostsPage }
    },
  },
] as const)
