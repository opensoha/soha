import { defineRoutes } from '@/routes/definitions'

export const settingsRoutes = defineRoutes([
  {
    meta: {
      id: 'settings',
      path: '/settings',
      title: '设置中心',
      description: '设置中心总览与配置入口',
      icon: 'IconSetting',
      group: 'settings',
      requiresAuth: true,
      tabbar: false,
      navVisible: false,
      menuId: 'settings',
      workbenchId: 'settings',
      permissionStrategy: 'any-child',
      scopeMode: 'passive',
      workspace: 'system',
    },
    shell: 'app',
    permissionExemptReason: 'Settings center visibility is resolved from accessible child menus.',
    redirectTo: '/settings/overview',
  },
  {
    meta: {
      id: 'settings-overview',
      path: '/settings/overview',
      title: '总览',
      description: '设置中心总览',
      icon: 'IconDashboard',
      group: 'settings',
      requiresAuth: true,
      tabbar: true,
      navVisible: true,
      parentId: 'settings',
      menuId: 'settings-overview',
      permissionKeysAny: [
        'access.users.view',
        'access.roles.view',
        'access.groups.view',
        'access.policies.view',
        'settings.identity.view',
        'settings.ai.view',
        'settings.branding.view',
        'settings.system-integrations.view',
        'settings.runtime-config.view',
        'system.menus.view',
        'system.online-users.view',
        'system.audit.view',
        'system.operations.view',
        'secret.view',
      ],
      scopeMode: 'passive',
    },
    shell: 'app',
    load: async () => {
      const module = await import('./overview/page')
      return { default: module.SettingsOverviewPage }
    },
  },
  {
    meta: {
      id: 'settings-login',
      path: '/settings/login',
      title: '登录设置',
      description: 'OIDC、OAuth2 与 SAML 登录配置',
      icon: 'IconSetting',
      group: 'settings',
      requiresAuth: true,
      tabbar: true,
      navVisible: true,
      parentId: 'settings',
      menuId: 'settings-login',
      permissionKey: 'settings.identity.view',
      scopeMode: 'passive',
    },
    shell: 'app',
    load: async () => {
      const module = await import('./identity/page')
      return { default: module.LoginSettingsPage }
    },
  },
  {
    meta: {
      id: 'settings-branding',
      path: '/settings/branding',
      title: '品牌设置',
      description: '品牌 Logo 与标题配置',
      icon: 'IconSetting',
      group: 'settings',
      requiresAuth: true,
      tabbar: true,
      navVisible: true,
      parentId: 'settings',
      menuId: 'settings-branding',
      permissionKey: 'settings.branding.view',
      scopeMode: 'passive',
    },
    shell: 'app',
    load: async () => {
      const module = await import('./branding/page')
      return { default: module.BrandingSettingsPage }
    },
  },
  {
    meta: {
      id: 'settings-source-control',
      path: '/settings/source-control',
      title: '代码源',
      description: '管理全局代码源连接',
      icon: 'IconGitBranch',
      group: 'settings',
      requiresAuth: true,
      tabbar: true,
      navVisible: true,
      parentId: 'settings',
      menuId: 'settings-source-control',
      permissionKey: 'settings.system-integrations.view',
      scopeMode: 'passive',
    },
    shell: 'app',
    load: async () => {
      const module = await import('./system-integrations/source-list-page')
      return { default: module.SourceConnectionsPage }
    },
  },
  {
    meta: {
      id: 'settings-source-control-create',
      path: '/settings/source-control/new',
      title: '新增代码源连接',
      description: '新建代码源连接',
      icon: 'IconGitBranch',
      group: 'settings',
      requiresAuth: true,
      tabbar: true,
      navVisible: false,
      parentId: 'settings-source-control',
      menuId: 'settings-source-control',
      permissionKey: 'settings.system-integrations.create',
      scopeMode: 'passive',
    },
    shell: 'app',
    load: async () => {
      const module = await import('./system-integrations/source-detail-page')
      return { default: module.SourceConnectionDetailPage }
    },
  },
  {
    meta: {
      id: 'settings-source-control-detail',
      path: '/settings/source-control/:integrationId',
      title: '代码源连接',
      description: '新建或编辑代码源连接',
      icon: 'IconGitBranch',
      group: 'settings',
      requiresAuth: true,
      tabbar: true,
      navVisible: false,
      parentId: 'settings-source-control',
      menuId: 'settings-source-control',
      permissionKey: 'settings.system-integrations.view',
      scopeMode: 'passive',
    },
    shell: 'app',
    load: async () => {
      const module = await import('./system-integrations/source-detail-page')
      return { default: module.SourceConnectionDetailPage }
    },
  },
  {
    meta: {
      id: 'settings-system-integrations-legacy-redirect',
      path: '/settings/system-integrations',
      title: '代码源',
      description: '旧系统集成入口兼容跳转',
      icon: 'IconGitBranch',
      group: 'settings',
      requiresAuth: true,
      tabbar: false,
      navVisible: false,
      parentId: 'settings',
      menuId: 'settings-source-control',
      permissionKey: 'settings.system-integrations.view',
      scopeMode: 'passive',
    },
    shell: 'app',
    redirectTo: '/settings/source-control',
  },
  {
    meta: {
      id: 'settings-system-integrations-source-control-legacy-redirect',
      path: '/settings/system-integrations/source-control',
      title: '代码源',
      description: '旧代码源入口兼容跳转',
      icon: 'IconGitBranch',
      group: 'settings',
      requiresAuth: true,
      tabbar: false,
      navVisible: false,
      parentId: 'settings',
      menuId: 'settings-source-control',
      permissionKey: 'settings.system-integrations.view',
      scopeMode: 'passive',
    },
    shell: 'app',
    redirectTo: '/settings/source-control',
  },
  {
    meta: {
      id: 'settings-system-integrations-source-control-detail-legacy-redirect',
      path: '/settings/system-integrations/source-control/:integrationId',
      title: '代码源连接',
      description: '旧代码源详情入口兼容跳转',
      icon: 'IconGitBranch',
      group: 'settings',
      requiresAuth: true,
      tabbar: false,
      navVisible: false,
      parentId: 'settings',
      menuId: 'settings-source-control',
      permissionKey: 'settings.system-integrations.view',
      scopeMode: 'passive',
    },
    shell: 'app',
    load: async () => {
      const module = await import('./system-integrations/legacy-detail-redirect')
      return { default: module.LegacySourceConnectionDetailRedirect }
    },
  },
  {
    meta: {
      id: 'settings-runtime-configuration',
      path: '/settings/runtime-configuration',
      title: '运行时配置',
      description: '校验、应用与回滚运行时配置',
      icon: 'IconAdjustments',
      group: 'settings',
      requiresAuth: true,
      tabbar: true,
      navVisible: true,
      parentId: 'settings',
      menuId: 'settings-runtime-configuration',
      permissionKey: 'settings.runtime-config.view',
      scopeMode: 'passive',
    },
    shell: 'app',
    load: async () => {
      const module = await import('./runtime-configuration/page')
      return { default: module.RuntimeConfigurationPage }
    },
  },
] as const)
