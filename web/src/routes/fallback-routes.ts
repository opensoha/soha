import { defineRoutes } from './definitions'

export const fallbackRoutes = defineRoutes([
  {
    meta: {
      id: 'app-fallback',
      path: '*',
      title: '页面未找到',
      description: '未知控制台路径返回平台总览',
      requiresAuth: true,
      tabbar: false,
      navVisible: false,
      scopeMode: 'hidden',
    },
    shell: 'app',
    wildcard: true,
    redirectTo: '/',
  },
] as const)
