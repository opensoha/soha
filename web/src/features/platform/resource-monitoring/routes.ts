import { defineRoutes } from '@/routes/definitions'
import { monitoringPermission, monitoringViews } from './model'
export const monitoringViewPermissions = monitoringViews.map((view) =>
  monitoringPermission(view.key),
)
export const resourceMonitoringRoutes = defineRoutes([
  {
    meta: {
      id: 'resource-monitoring',
      path: '/resource-monitoring',
      title: '资源监控',
      description: '节点资源、连接跟踪与 Pod 容量',
      icon: 'IconChartLine',
      group: 'platform',
      workbenchId: 'platform',
      requiresAuth: true,
      tabbar: true,
      navVisible: true,
      menuId: 'resource-monitoring',
      permissionKeysAny: monitoringViewPermissions,
      permissionKeysAll: ['platform.nodes.view'],
      scopeMode: 'cluster' as const,
      workspace: 'resource' as const,
    },
    shell: 'app',
    load: async () => {
      const module = await import('./page')
      return { default: module.ResourceMonitoringEntry }
    },
  },
  {
    meta: {
      id: 'resource-monitoring-overview',
      path: '/resource-monitoring/overview',
      title: '总览',
      description: '资源监控 · 总览',
      icon: 'IconChartLine',
      group: 'platform',
      requiresAuth: true,
      tabbar: false,
      navVisible: false,
      parentId: 'resource-monitoring',
      menuId: 'resource-monitoring-overview',
      permissionKey: 'platform.resource-monitoring.overview.view',
      permissionKeysAll: ['platform.nodes.view'],
      scopeMode: 'cluster',
      workspace: 'resource',
    },
    shell: 'app',
    load: async () => {
      const module = await import('./page')
      return { default: module.ResourceMonitoringOverviewPage }
    },
  },
  {
    meta: {
      id: 'resource-monitoring-cpu',
      path: '/resource-monitoring/cpu',
      title: 'CPU',
      description: '资源监控 · CPU',
      icon: 'IconChartLine',
      group: 'platform',
      requiresAuth: true,
      tabbar: false,
      navVisible: false,
      parentId: 'resource-monitoring',
      menuId: 'resource-monitoring-cpu',
      permissionKey: 'platform.resource-monitoring.cpu.view',
      permissionKeysAll: ['platform.nodes.view'],
      scopeMode: 'cluster',
      workspace: 'resource',
    },
    shell: 'app',
    load: async () => {
      const module = await import('./page')
      return { default: module.ResourceMonitoringCPUPage }
    },
  },
  {
    meta: {
      id: 'resource-monitoring-memory',
      path: '/resource-monitoring/memory',
      title: '内存',
      description: '资源监控 · 内存',
      icon: 'IconChartLine',
      group: 'platform',
      requiresAuth: true,
      tabbar: false,
      navVisible: false,
      parentId: 'resource-monitoring',
      menuId: 'resource-monitoring-memory',
      permissionKey: 'platform.resource-monitoring.memory.view',
      permissionKeysAll: ['platform.nodes.view'],
      scopeMode: 'cluster',
      workspace: 'resource',
    },
    shell: 'app',
    load: async () => {
      const module = await import('./page')
      return { default: module.ResourceMonitoringMemoryPage }
    },
  },
  {
    meta: {
      id: 'resource-monitoring-conntrack',
      path: '/resource-monitoring/conntrack',
      title: '连接跟踪',
      description: '资源监控 · 连接跟踪',
      icon: 'IconChartLine',
      group: 'platform',
      requiresAuth: true,
      tabbar: false,
      navVisible: false,
      parentId: 'resource-monitoring',
      menuId: 'resource-monitoring-conntrack',
      permissionKey: 'platform.resource-monitoring.conntrack.view',
      permissionKeysAll: ['platform.nodes.view'],
      scopeMode: 'cluster',
      workspace: 'resource',
    },
    shell: 'app',
    load: async () => {
      const module = await import('./page')
      return { default: module.ResourceMonitoringConntrackPage }
    },
  },
  {
    meta: {
      id: 'resource-monitoring-pods',
      path: '/resource-monitoring/pods',
      title: 'Pod 容量',
      description: '资源监控 · Pod 容量',
      icon: 'IconChartLine',
      group: 'platform',
      requiresAuth: true,
      tabbar: false,
      navVisible: false,
      parentId: 'resource-monitoring',
      menuId: 'resource-monitoring-pods',
      permissionKey: 'platform.resource-monitoring.pods.view',
      permissionKeysAll: ['platform.nodes.view'],
      scopeMode: 'cluster',
      workspace: 'resource',
    },
    shell: 'app',
    load: async () => {
      const module = await import('./page')
      return { default: module.ResourceMonitoringPodsPage }
    },
  },
] as const)
