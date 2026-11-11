import { defineRoutes } from '@/routes/definitions'

export const resourceCreationRoutes = defineRoutes([
  {
    meta: {
      id: 'platform-resource-creation',
      path: '/resource-creation',
      title: 'YAML 创建资源',
      description: '跨类型 Kubernetes YAML 资源创建',
      icon: 'IconPlus',
      group: 'platform',
      workbenchId: 'platform',
      requiresAuth: true,
      tabbar: true,
      navVisible: false,
      menuId: 'dashboard',
      scopeMode: 'namespace',
      workspace: 'resource',
    },
    shell: 'app',
    permissionExemptReason: 'modal redirect; the create API enforces the selected resource permission',
    load: async () => {
      const module = await import('./modal-redirect')
      return { default: module.ResourceCreationModalRedirect }
    },
  },
] as const)
