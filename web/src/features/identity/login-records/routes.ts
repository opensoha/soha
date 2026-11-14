import { defineRoutes } from '@/routes/definitions'

export const identityLoginRecordRoutes = defineRoutes([
  {
    meta: {
      id: 'identity-login-records',
      path: '/identity/login-records',
      title: 'Login Records',
      description: '应用登录记录',
      icon: 'IconFileText',
      group: 'identity',
      workbenchId: 'security',
      requiresAuth: true,
      tabbar: true,
      navVisible: true,
      parentId: 'internal-workbench',
      menuId: 'identity-login-records',
      permissionKeysAny: ['identity.audit.view', 'system.audit.view'],
      scopeMode: 'passive',
      workspace: 'system',
    },
    shell: 'app',
    load: async () => {
      const module = await import('./page')
      return { default: module.IdentityLoginRecordsPage }
    },
  },
] as const)
