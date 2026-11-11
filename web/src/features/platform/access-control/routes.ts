import { defineRoutes } from '@/routes/definitions'

export const accessControlViewPermissions = [
  'platform.access-control.service-accounts.view',
  'platform.access-control.cluster-roles.view',
  'platform.access-control.roles.view',
  'platform.access-control.cluster-role-bindings.view',
  'platform.access-control.role-bindings.view',
]

export const accessControlRoutes = defineRoutes([
  {
    meta: {
      id: 'platform-access-control',
      path: '/platform-access-control',
      title: 'RBAC',
      description: 'Kubernetes RBAC 资源',
      icon: 'IconShield',
      group: 'platform',
      requiresAuth: true,
      tabbar: false,
      navVisible: true,
      menuId: 'platform-access-control',
      permissionKeysAny: accessControlViewPermissions,
      scopeMode: 'namespace',
      workspace: 'resource',
    },
    shell: 'app',
    redirectTo: '/platform-access-control/serviceaccounts',
  },
  {
    meta: {
      id: 'platform-access-control-serviceaccounts',
      path: '/platform-access-control/serviceaccounts',
      title: 'ServiceAccounts',
      description: '服务账户',
      icon: 'IconShield',
      group: 'platform',
      requiresAuth: true,
      tabbar: true,
      navVisible: true,
      parentId: 'platform-access-control',
      menuId: 'platform-access-control-serviceaccounts',
      permissionKey: 'platform.access-control.service-accounts.view',
      scopeMode: 'namespace',
    },
    shell: 'app',
    inheritMetaFrom: 'platform-access-control',
    load: async () => {
      const module = await import('./serviceaccounts/list-page')
      return { default: module.PlatformAccessControlServiceAccountsPage }
    },
  },
  {
    meta: {
      id: 'platform-access-control-serviceaccount-detail',
      path: '/platform-access-control/serviceaccounts/:name',
      title: 'ServiceAccount Detail',
      description: '服务账户详情',
      requiresAuth: true,
      tabbar: false,
      navVisible: false,
      scopeMode: 'namespace',
    },
    shell: 'app',
    inheritMetaFrom: 'platform-access-control-serviceaccounts',
    load: async () => {
      const module = await import('./serviceaccounts/detail-page')
      return { default: module.PlatformAccessControlServiceAccountDetailPage }
    },
  },
  {
    meta: {
      id: 'platform-access-control-clusterroles',
      path: '/platform-access-control/clusterroles',
      title: 'ClusterRoles',
      description: '集群角色',
      icon: 'IconShield',
      group: 'platform',
      requiresAuth: true,
      tabbar: true,
      navVisible: true,
      parentId: 'platform-access-control',
      menuId: 'platform-access-control-clusterroles',
      permissionKey: 'platform.access-control.cluster-roles.view',
      scopeMode: 'cluster',
    },
    shell: 'app',
    inheritMetaFrom: 'platform-access-control',
    load: async () => {
      const module = await import('./clusterroles/list-page')
      return { default: module.PlatformAccessControlClusterRolesPage }
    },
  },
  {
    meta: {
      id: 'platform-access-control-clusterrole-detail',
      path: '/platform-access-control/clusterroles/:name',
      title: 'ClusterRole Detail',
      description: '集群角色详情',
      requiresAuth: true,
      tabbar: false,
      navVisible: false,
      scopeMode: 'cluster',
    },
    shell: 'app',
    inheritMetaFrom: 'platform-access-control-clusterroles',
    load: async () => {
      const module = await import('./clusterroles/detail-page')
      return { default: module.PlatformAccessControlClusterRoleDetailPage }
    },
  },
  {
    meta: {
      id: 'platform-access-control-roles',
      path: '/platform-access-control/roles',
      title: 'Roles',
      description: '命名空间角色',
      icon: 'IconShield',
      group: 'platform',
      requiresAuth: true,
      tabbar: true,
      navVisible: true,
      parentId: 'platform-access-control',
      menuId: 'platform-access-control-roles',
      permissionKey: 'platform.access-control.roles.view',
      scopeMode: 'namespace',
    },
    shell: 'app',
    inheritMetaFrom: 'platform-access-control',
    load: async () => {
      const module = await import('./roles/list-page')
      return { default: module.PlatformAccessControlRolesPage }
    },
  },
  {
    meta: {
      id: 'platform-access-control-role-detail',
      path: '/platform-access-control/roles/:name',
      title: 'Role Detail',
      description: '命名空间角色详情',
      requiresAuth: true,
      tabbar: false,
      navVisible: false,
      scopeMode: 'namespace',
    },
    shell: 'app',
    inheritMetaFrom: 'platform-access-control-roles',
    load: async () => {
      const module = await import('./roles/detail-page')
      return { default: module.PlatformAccessControlRoleDetailPage }
    },
  },
  {
    meta: {
      id: 'platform-access-control-clusterrolebindings',
      path: '/platform-access-control/clusterrolebindings',
      title: 'ClusterRoleBindings',
      description: '集群角色绑定',
      icon: 'IconShield',
      group: 'platform',
      requiresAuth: true,
      tabbar: true,
      navVisible: true,
      parentId: 'platform-access-control',
      menuId: 'platform-access-control-clusterrolebindings',
      permissionKey: 'platform.access-control.cluster-role-bindings.view',
      scopeMode: 'cluster',
    },
    shell: 'app',
    inheritMetaFrom: 'platform-access-control',
    load: async () => {
      const module = await import('./clusterrolebindings/list-page')
      return { default: module.PlatformAccessControlClusterRoleBindingsPage }
    },
  },
  {
    meta: {
      id: 'platform-access-control-clusterrolebinding-detail',
      path: '/platform-access-control/clusterrolebindings/:name',
      title: 'ClusterRoleBinding Detail',
      description: '集群角色绑定详情',
      requiresAuth: true,
      tabbar: false,
      navVisible: false,
      scopeMode: 'cluster',
    },
    shell: 'app',
    inheritMetaFrom: 'platform-access-control-clusterrolebindings',
    load: async () => {
      const module = await import('./clusterrolebindings/detail-page')
      return { default: module.PlatformAccessControlClusterRoleBindingDetailPage }
    },
  },
  {
    meta: {
      id: 'platform-access-control-rolebindings',
      path: '/platform-access-control/rolebindings',
      title: 'RoleBindings',
      description: '命名空间角色绑定',
      icon: 'IconShield',
      group: 'platform',
      requiresAuth: true,
      tabbar: true,
      navVisible: true,
      parentId: 'platform-access-control',
      menuId: 'platform-access-control-rolebindings',
      permissionKey: 'platform.access-control.role-bindings.view',
      scopeMode: 'namespace',
    },
    shell: 'app',
    inheritMetaFrom: 'platform-access-control',
    load: async () => {
      const module = await import('./rolebindings/list-page')
      return { default: module.PlatformAccessControlRoleBindingsPage }
    },
  },
  {
    meta: {
      id: 'platform-access-control-rolebinding-detail',
      path: '/platform-access-control/rolebindings/:name',
      title: 'RoleBinding Detail',
      description: '命名空间角色绑定详情',
      requiresAuth: true,
      tabbar: false,
      navVisible: false,
      scopeMode: 'namespace',
    },
    shell: 'app',
    inheritMetaFrom: 'platform-access-control-rolebindings',
    load: async () => {
      const module = await import('./rolebindings/detail-page')
      return { default: module.PlatformAccessControlRoleBindingDetailPage }
    },
  },
] as const)
