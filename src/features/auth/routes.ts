import { defineRoutes } from '@/routes/definitions'

export const authRoutes = defineRoutes([
  {
    meta: {
      id: 'cli-device',
      path: '/auth/device',
      title: 'CLI 登录确认',
      description: '核对并批准终端登录请求',
      icon: 'IconLock',
      group: 'auth',
      requiresAuth: false,
      tabbar: false,
      navVisible: false,
      scopeMode: 'hidden',
    },
    shell: 'public',
    load: async () => {
      const module = await import('./cli-device-page')
      return { default: module.CLIDevicePage }
    },
  },
  {
    meta: {
      id: 'login',
      path: '/login',
      title: '登录',
      description: '用户登录',
      icon: 'IconLock',
      group: 'auth',
      requiresAuth: false,
      tabbar: false,
      navVisible: false,
      scopeMode: 'hidden',
    },
    shell: 'public',
    load: async () => {
      const module = await import('./login-page')
      return { default: module.LoginPage }
    },
  },
  {
    meta: {
      id: 'browser-handoff',
      path: '/auth/browser-handoff/:handoffId',
      title: '浏览器登录确认',
      description: '确认来自 Soha App 的一次性浏览器登录交接',
      icon: 'IconLock',
      group: 'auth',
      requiresAuth: false,
      tabbar: false,
      navVisible: false,
      scopeMode: 'hidden',
    },
    shell: 'public',
    load: async () => {
      const module = await import('./browser-handoff-page')
      return { default: module.BrowserHandoffPage }
    },
  },
] as const)

export const authUtilityRoutes = defineRoutes([
  {
    meta: {
      id: 'account-settings',
      path: '/account/settings',
      title: '个人设置',
      description: '当前账号的个人工具入口',
      icon: 'IconSetting',
      group: 'account',
      requiresAuth: true,
      tabbar: false,
      navVisible: false,
      scopeMode: 'passive',
    },
    shell: 'app',
    permissionExemptReason: 'Every authenticated user can access their personal settings.',
    redirectTo: '/account/profile',
  },
  {
    meta: {
      id: 'account-profile',
      path: '/account/profile',
      title: '个人中心',
      description: '当前账号资料、登录安全与 AI Gateway key',
      icon: 'IconUser',
      group: 'account',
      requiresAuth: true,
      tabbar: false,
      navVisible: false,
      scopeMode: 'passive',
    },
    shell: 'app',
    permissionExemptReason: 'Every authenticated user can manage their own account profile.',
    load: async () => {
      const module = await import('./user-profile-page')
      return { default: module.UserProfilePage }
    },
  },
  {
    meta: {
      id: 'settings-about-legacy-redirect',
      path: '/settings/about',
      title: '关于',
      description: '旧关于页面入口兼容跳转',
      icon: 'IconInfoCircle',
      group: 'account',
      requiresAuth: true,
      tabbar: false,
      navVisible: false,
      scopeMode: 'passive',
    },
    shell: 'app',
    permissionExemptReason: 'Compatibility redirect for authenticated users.',
    redirectTo: '/about',
  },
  {
    meta: {
      id: 'about',
      path: '/about',
      title: '关于',
      description: 'OpenSoha 版本与项目信息',
      icon: 'IconInfoCircle',
      group: 'account',
      requiresAuth: true,
      tabbar: false,
      navVisible: false,
      scopeMode: 'passive',
    },
    shell: 'app',
    permissionExemptReason: 'Product information is available to every authenticated user.',
    load: async () => {
      const module = await import('./about-page')
      return { default: module.AboutPage }
    },
  },
  {
    meta: {
      id: 'oidc-callback',
      path: '/auth/oidc/callback',
      title: 'OIDC Callback',
      description: 'OIDC 回调',
      icon: 'IconLock',
      group: 'auth',
      requiresAuth: false,
      tabbar: false,
      navVisible: false,
      scopeMode: 'hidden',
    },
    shell: 'public',
    load: async () => {
      const module = await import('./oidc-callback-page')
      return { default: module.OIDCCallbackPage }
    },
  },
  {
    meta: {
      id: 'login-callback',
      path: '/login/callback',
      title: 'Login Callback',
      description: '登录回调',
      icon: 'IconLock',
      group: 'auth',
      requiresAuth: false,
      tabbar: false,
      navVisible: false,
      scopeMode: 'hidden',
    },
    shell: 'public',
    load: async () => {
      const module = await import('./oidc-callback-page')
      return { default: module.OIDCCallbackPage }
    },
  },
] as const)
