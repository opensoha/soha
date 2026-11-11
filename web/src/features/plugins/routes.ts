import { defineRoutes } from '@/routes/definitions'

export const pluginRoutes = defineRoutes([
  {
    meta: {
      id: 'plugins',
      path: '/plugins',
      title: '插件',
      description: 'Soha 插件市场与已安装插件',
      icon: 'IconPuzzle',
      group: 'extension-center',
      workbenchId: 'settings',
      requiresAuth: true,
      tabbar: false,
      navVisible: false,
      permissionStrategy: 'any-child',
      scopeMode: 'passive',
      workspace: 'system',
    },
    shell: 'app',
    redirectTo: '/plugins/marketplace',
  },
  {
    meta: {
      id: 'extension-center',
      path: '/settings/extensions',
      title: '扩展',
      description: 'Soha 平台插件市场、扩展能力与运行治理',
      icon: 'IconPuzzle',
      group: 'extension-center',
      workbenchId: 'settings',
      requiresAuth: true,
      tabbar: false,
      navVisible: true,
      menuId: 'settings-extensions',
      permissionStrategy: 'any-child',
      scopeMode: 'passive',
      workspace: 'system',
    },
    shell: 'app',
    redirectTo: '/plugins/marketplace',
  },
  {
    meta: {
      id: 'plugins-marketplace',
      path: '/plugins/marketplace',
      title: '市场',
      description: '搜索、审阅并安装 Soha 插件',
      icon: 'IconPuzzle',
      group: 'extension-center',
      workbenchId: 'settings',
      requiresAuth: true,
      tabbar: true,
      navVisible: true,
      parentId: 'extension-center',
      menuId: 'settings-extensions-marketplace',
      permissionKey: 'plugin.view',
      scopeMode: 'passive',
      workspace: 'system',
    },
    shell: 'app',
    load: async () => {
      const module = await import('./marketplace/list-page')
      return { default: module.PluginMarketplacePage }
    },
  },
  {
    meta: {
      id: 'plugins-marketplace-detail',
      path: '/plugins/marketplace/:pluginId',
      title: '插件详情',
      description: '市场插件 manifest 与安装前审阅',
      icon: 'IconPuzzle',
      group: 'extension-center',
      workbenchId: 'settings',
      requiresAuth: true,
      tabbar: false,
      navVisible: false,
      parentId: 'plugins-marketplace',
      menuId: 'settings-extensions-marketplace',
      permissionKey: 'plugin.view',
      scopeMode: 'passive',
      workspace: 'system',
    },
    shell: 'app',
    load: async () => {
      const module = await import('./marketplace/detail-page')
      return { default: module.PluginMarketplaceDetailPage }
    },
  },
  {
    meta: {
      id: 'plugins-installed-detail',
      path: '/plugins/installed/:pluginId',
      title: '已安装插件详情',
      description: '插件安装记录、配置、权限声明和 manifest 快照',
      icon: 'IconPuzzle',
      group: 'extension-center',
      workbenchId: 'settings',
      requiresAuth: true,
      tabbar: false,
      navVisible: false,
      parentId: 'plugins-marketplace',
      menuId: 'settings-extensions-marketplace',
      permissionKey: 'plugin.view',
      scopeMode: 'passive',
      workspace: 'system',
    },
    shell: 'app',
    load: async () => {
      const module = await import('./installed/detail-page')
      return { default: module.InstalledPluginDetailPage }
    },
  },
] as const)
