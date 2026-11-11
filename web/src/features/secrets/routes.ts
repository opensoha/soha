import { defineRoutes } from '@/routes/definitions'

export const secretRoutes = defineRoutes([
  {
    meta: {
      id: 'settings-secrets',
      path: '/settings/secrets',
      title: 'Secret Store',
      description: '管理 AI 与自动化执行使用的 Secret 引用',
      icon: 'IconSetting',
      group: 'settings',
      workbenchId: 'settings',
      requiresAuth: true,
      tabbar: true,
      navVisible: true,
      parentId: 'settings',
      menuId: 'settings-secrets',
      permissionKey: 'secret.view',
      scopeMode: 'passive',
      workspace: 'system',
    },
    shell: 'app',
    load: async () => {
      const module = await import('./page')
      return { default: module.SecretsPage }
    },
  },
] as const)
