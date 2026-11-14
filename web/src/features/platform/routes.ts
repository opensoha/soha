import { resourceMonitoringRoutes } from './resource-monitoring/routes'
import { defineRoutes } from '@/routes/definitions'
import { accessControlRoutes } from './access-control/routes'
import { clusterResourceRoutes } from './cluster-resources/routes'
import { clusterRoutes } from './clusters/routes'
import { configurationRoutes, configurationViewPermissions } from './configuration/routes'
import { extensionRoutes } from './extensions/routes'
import { manifestRoutes } from './manifests/routes'
import { networkRoutes, networkViewPermissions } from './network/routes'
import { resourceCreationRoutes } from './resource-creation/routes'
import { storageRoutes, storageViewPermissions } from './storage/routes'
import { workloadRoutes, workloadViewPermissions } from './workloads/routes'

export const platformShellRoutes = defineRoutes([
  {
    meta: {
      id: 'overview',
      path: '/',
      title: 'k8s工作台',
      description: '平台总览',
      icon: 'IconDesktop',
      group: 'overview',
      workbenchId: 'platform',
      requiresAuth: true,
      tabbar: true,
      navVisible: true,
      menuId: 'dashboard',
      permissionKey: 'overview.view',
      scopeMode: 'cluster',
      workspace: 'resource',
    },
    shell: 'app',
    load: async () => {
      const module = await import('./overview-page')
      return { default: module.OverviewPage }
    },
  },
  {
    meta: {
      id: 'workloads',
      path: '/workloads',
      title: '工作负载',
      description: '工作负载管理',
      icon: 'IconGridView',
      group: 'platform',
      requiresAuth: true,
      tabbar: false,
      navVisible: true,
      menuId: 'workloads',
      permissionKeysAny: workloadViewPermissions,
      scopeMode: 'namespace',
      workspace: 'resource',
    },
    shell: 'app',
    redirectTo: '/workloads/overview',
  },
  {
    meta: {
      id: 'configuration',
      path: '/configuration',
      title: 'Configuration',
      description: '平台配置资源',
      icon: 'IconSetting',
      group: 'platform',
      requiresAuth: true,
      tabbar: false,
      navVisible: true,
      menuId: 'configuration',
      permissionKeysAny: configurationViewPermissions,
      scopeMode: 'namespace',
      workspace: 'resource',
    },
    shell: 'app',
    redirectTo: '/configuration/configmaps',
  },
  {
    meta: {
      id: 'network',
      path: '/network',
      title: '网络',
      description: '网络资源',
      icon: 'IconConnection',
      group: 'platform',
      requiresAuth: true,
      tabbar: false,
      navVisible: true,
      menuId: 'network',
      permissionKeysAny: networkViewPermissions,
      scopeMode: 'namespace',
      workspace: 'resource',
    },
    shell: 'app',
    load: async () => {
      const module = await import('./network/group-entry')
      return { default: module.NetworkEntry }
    },
  },
  {
    meta: {
      id: 'storage',
      path: '/storage',
      title: '存储',
      description: '存储资源',
      icon: 'IconServer',
      group: 'platform',
      requiresAuth: true,
      tabbar: false,
      navVisible: true,
      menuId: 'storage',
      permissionKeysAny: storageViewPermissions,
      scopeMode: 'namespace',
      workspace: 'resource',
    },
    shell: 'app',
    redirectTo: '/storage/persistentvolumeclaims',
  },
] as const)

export const platformRouteManifests = [
  accessControlRoutes,
  manifestRoutes,
  clusterRoutes,
  clusterResourceRoutes,
  workloadRoutes,
  configurationRoutes,
  extensionRoutes,
  networkRoutes,
  resourceMonitoringRoutes,
  storageRoutes,
  resourceCreationRoutes,
] as const
