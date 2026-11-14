import { defineRoutes } from '@/routes/definitions'

const loadSessionsPage = async () => {
  const module = await import('./sessions/page')
  return { default: module.OnlineUsersPage }
}

const loadAuditPage = async () => {
  const module = await import('./audit/page')
  return { default: module.AuditLogsPage }
}

export const systemRoutes = defineRoutes([
  {
    meta: {
      id: 'system',
      path: '/system',
      title: '系统管理',
      description: '公告、菜单、审计与操作记录',
      icon: 'IconSetting',
      group: 'system',
      workbenchId: 'settings',
      requiresAuth: true,
      tabbar: false,
      navVisible: false,
      permissionStrategy: 'any-child',
      scopeMode: 'passive',
      workspace: 'system',
    },
    shell: 'app',
    redirectTo: '/system/online-users',
  },
  {
    meta: {
      id: 'system-online-users',
      path: '/system/online-users',
      title: '在线用户',
      description: '在线用户监控',
      icon: 'IconUser',
      group: 'system',
      workbenchId: 'settings',
      requiresAuth: true,
      tabbar: true,
      navVisible: true,
      parentId: 'system',
      menuId: 'system-online-users',
      permissionKey: 'system.online-users.view',
      scopeMode: 'passive',
      workspace: 'system',
    },
    shell: 'app',
    load: loadSessionsPage,
  },
  {
    meta: {
      id: 'system-announcements',
      path: '/system/announcements',
      title: '公告',
      description: '公告管理',
      icon: 'IconBell',
      group: 'system',
      workbenchId: 'settings',
      requiresAuth: true,
      tabbar: true,
      navVisible: true,
      parentId: 'system',
      menuId: 'announcements',
      permissionKey: 'system.announcements.view',
      scopeMode: 'passive',
      workspace: 'system',
    },
    shell: 'app',
    load: async () => {
      const module = await import('./announcements/page')
      return { default: module.AnnouncementsPage }
    },
  },
  {
    meta: {
      id: 'system-menus',
      path: '/system/menus',
      title: '菜单',
      description: '菜单管理',
      icon: 'IconMenu',
      group: 'system',
      workbenchId: 'settings',
      requiresAuth: true,
      tabbar: true,
      navVisible: true,
      parentId: 'system',
      menuId: 'menus',
      permissionKey: 'system.menus.view',
      scopeMode: 'passive',
      workspace: 'system',
    },
    shell: 'app',
    load: async () => {
      const module = await import('./menus/page')
      return { default: module.MenusPage }
    },
  },
  {
    meta: {
      id: 'audit',
      path: '/system/audit',
      title: '审计日志',
      description: '审计记录',
      icon: 'IconFile',
      group: 'system',
      workbenchId: 'settings',
      requiresAuth: true,
      tabbar: true,
      navVisible: true,
      parentId: 'system',
      menuId: 'audit',
      permissionKey: 'system.audit.view',
      scopeMode: 'passive',
      workspace: 'system',
    },
    shell: 'app',
    load: loadAuditPage,
  },
  {
    meta: {
      id: 'operations',
      path: '/system/operations',
      title: '操作日志',
      description: '操作记录',
      icon: 'IconList',
      group: 'system',
      workbenchId: 'settings',
      requiresAuth: true,
      tabbar: true,
      navVisible: true,
      parentId: 'system',
      menuId: 'operations',
      permissionKey: 'system.operations.view',
      scopeMode: 'passive',
      workspace: 'system',
    },
    shell: 'app',
    load: async () => {
      const module = await import('./operation-logs/page')
      return { default: module.OperationLogsPage }
    },
  },
  {
    meta: {
      id: 'identity-audit',
      path: '/identity/audit',
      title: 'Audit',
      description: '审计日志兼容入口',
      icon: 'IconFile',
      group: 'identity',
      workbenchId: 'settings',
      requiresAuth: true,
      tabbar: false,
      navVisible: false,
      permissionKey: 'system.audit.view',
      scopeMode: 'passive',
      workspace: 'system',
    },
    shell: 'app',
    redirectTo: '/system/audit',
  },
] as const)
