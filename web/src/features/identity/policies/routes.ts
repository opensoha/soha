import { defineRoutes } from '@/routes/definitions'

export const identityPolicyRoutes = defineRoutes([
  {
    meta: {
      id: 'identity-policies',
      path: '/identity/policies',
      title: 'Policies',
      description: 'Provider Portal 访问策略兼容入口',
      icon: 'IconShield',
      group: 'identity',
      requiresAuth: true,
      tabbar: false,
      navVisible: false,
      permissionKey: 'identity.applications.view',
      scopeMode: 'passive',
      workspace: 'system',
    },
    shell: 'app',
    redirectTo: '/identity/applications',
  },
] as const)
