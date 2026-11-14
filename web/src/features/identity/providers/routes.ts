import { defineRoutes } from '@/routes/definitions'

export const identityProviderRoutes = defineRoutes([
  {
    meta: {
      id: 'identity-providers',
      path: '/identity/providers',
      title: 'Providers',
      description: 'OIDC / Proxy Provider 管理',
      icon: 'IconShield',
      group: 'identity',
      workbenchId: 'security',
      requiresAuth: true,
      tabbar: true,
      navVisible: true,
      parentId: 'internal-workbench',
      menuId: 'identity-providers',
      permissionKey: 'identity.providers.view',
      scopeMode: 'passive',
      workspace: 'system',
    },
    shell: 'app',
    load: async () => {
      const module = await import('./list-page')
      return { default: module.IdentityProvidersPage }
    },
  },
] as const)
