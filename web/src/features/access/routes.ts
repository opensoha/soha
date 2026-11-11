import { defineRoutes } from '@/routes/definitions'

export const accessRoutes = defineRoutes([
  {
    meta: {
      id: 'access-users',
      path: '/access/users',
      title: '用户',
      description: '用户管理',
      icon: 'IconUser',
      group: 'access',
      requiresAuth: true,
      tabbar: true,
      navVisible: true,
      menuId: 'access-users',
      permissionKey: 'access.users.view',
      scopeMode: 'passive',
    },
    shell: 'app',
    load: async () => {
      const module = await import('./users/page')
      return { default: module.AccessUsersPage }
    },
  },
  {
    meta: {
      id: 'access-roles',
      path: '/access/roles',
      title: '角色',
      description: '角色管理',
      icon: 'IconUserCircle',
      group: 'access',
      requiresAuth: true,
      tabbar: true,
      navVisible: true,
      menuId: 'access-roles',
      permissionKey: 'access.roles.view',
      scopeMode: 'passive',
    },
    shell: 'app',
    load: async () => {
      const module = await import('./roles/page')
      return { default: module.AccessRolesPage }
    },
  },
  {
    meta: {
      id: 'access-teams',
      path: '/access/teams',
      title: '组织',
      description: '组织架构管理',
      icon: 'IconUserGroup',
      group: 'access',
      requiresAuth: true,
      tabbar: true,
      navVisible: true,
      menuId: 'access-teams',
      permissionKey: 'access.groups.view',
      scopeMode: 'passive',
    },
    shell: 'app',
    load: async () => {
      const module = await import('./teams/page')
      return { default: module.AccessTeamsPage }
    },
  },
  {
    meta: {
      id: 'access-policies',
      path: '/access/policies',
      title: '策略',
      description: '策略管理',
      icon: 'IconShield',
      group: 'access',
      requiresAuth: true,
      tabbar: true,
      navVisible: true,
      menuId: 'access-policies',
      permissionKey: 'access.policies.view',
      scopeMode: 'passive',
    },
    shell: 'app',
    load: async () => {
      const module = await import('./policies/page')
      return { default: module.AccessPoliciesPage }
    },
  },
  {
    meta: {
      id: 'access-directory-sync',
      path: '/access/directory-sync',
      title: '目录同步',
      description: '外部目录组织与人员同步',
      icon: 'IconSync',
      group: 'access',
      requiresAuth: true,
      tabbar: true,
      navVisible: true,
      menuId: 'access-directory-sync',
      permissionKey: 'access.directory.view',
      scopeMode: 'passive',
    },
    shell: 'app',
    load: async () => {
      const module = await import('./directory-sync/page')
      return { default: module.DirectorySyncPage }
    },
  },
] as const)
