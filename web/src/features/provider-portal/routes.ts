import { defineRoutes } from '@/routes/definitions'

export const providerPortalRoutes = defineRoutes([
  {
    meta: {
      id: 'provider-portal',
      path: '/portal',
      title: '门户首页',
      description: '统一应用门户',
      icon: 'IconDesktop',
      group: 'identity',
      workbenchId: 'home',
      requiresAuth: true,
      tabbar: false,
      navVisible: true,
      menuId: 'home-workbench',
      permissionKey: 'identity.portal.view',
      scopeMode: 'passive',
      workspace: 'system',
    },
    shell: 'portal',
    load: async () => {
      const module = await import('./catalog/page')
      return { default: module.SohaProviderPortalPage }
    },
  },
  {
    meta: {
      id: 'provider-portal-application-detail',
      path: '/portal/applications/:applicationId',
      title: 'Application Detail',
      description: 'Provider Portal 应用详情',
      icon: 'IconGridView',
      group: 'identity',
      workbenchId: 'home',
      requiresAuth: true,
      tabbar: false,
      navVisible: false,
      parentId: 'provider-portal',
      menuId: 'home-workbench',
      permissionKey: 'identity.applications.view',
      scopeMode: 'passive',
      workspace: 'system',
    },
    shell: 'portal',
    load: async () => {
      const module = await import('./application-detail/page')
      return { default: module.PortalApplicationDetailPage }
    },
  },
  {
    meta: {
      id: 'provider-portal-security',
      path: '/portal/security',
      title: 'Security',
      description: '个人身份安全中心',
      icon: 'IconShield',
      group: 'identity',
      workbenchId: 'home',
      requiresAuth: true,
      tabbar: false,
      navVisible: false,
      parentId: 'provider-portal',
      menuId: 'home-workbench',
      permissionKey: 'identity.portal.view',
      scopeMode: 'passive',
      workspace: 'system',
    },
    shell: 'portal',
    load: async () => {
      const module = await import('./security/page')
      return { default: module.PortalSecurityPage }
    },
  },
] as const)
