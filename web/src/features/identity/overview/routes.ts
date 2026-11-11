import { defineRoutes } from '@/routes/definitions'

export const internalWorkbenchOverviewRoutes = defineRoutes([
  {
    meta: {
      id: 'internal-workbench',
      path: '/internal-workbench',
      title: 'Internal Workbench',
      description: '内网工作台',
      icon: 'IconShield',
      group: 'identity',
      workbenchId: 'security',
      requiresAuth: true,
      tabbar: false,
      navVisible: true,
      menuId: 'identity',
      permissionStrategy: 'any-child',
      scopeMode: 'passive',
      workspace: 'system',
    },
    shell: 'app',
    redirectTo: '/internal-workbench/overview',
  },
  {
    meta: {
      id: 'internal-workbench-overview',
      path: '/internal-workbench/overview',
      title: 'Overview',
      description: '内网工作台总览',
      icon: 'IconDesktop',
      group: 'identity',
      workbenchId: 'security',
      requiresAuth: true,
      tabbar: true,
      navVisible: true,
      parentId: 'internal-workbench',
      menuId: 'identity-overview',
      permissionKeysAny: [
        'identity.applications.view',
        'identity.providers.view',
        'identity.outposts.view',
        'identity.policies.view',
        'identity.audit.view',
      ],
      scopeMode: 'passive',
      workspace: 'system',
    },
    shell: 'app',
    load: async () => {
      const module = await import('./page')
      return { default: module.IdentityOverviewPage }
    },
  },
] as const)
