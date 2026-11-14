import { defineRoutes } from '@/routes/definitions'

export const observabilitySignalRoutes = defineRoutes([
  {
    meta: {
      id: 'monitoring-workbench-services',
      path: '/monitoring-workbench/services',
      title: '服务',
      description: '从 Trace 归纳跨信号服务实体',
      icon: 'IconServer',
      group: 'observe',
      workbenchId: 'monitoring',
      requiresAuth: true,
      tabbar: true,
      navVisible: true,
      parentId: 'monitoring-workbench',
      menuId: 'monitoring-workbench-services',
      permissionKey: 'observe.monitoring.view',
      scopeMode: 'passive',
    },
    shell: 'app',
    load: async () => {
      const module = await import('./services-page')
      return { default: module.ObservabilityServicesPage }
    },
  },
  {
    meta: {
      id: 'monitoring-workbench-metrics',
      path: '/monitoring-workbench/metrics',
      title: '指标',
      description: '按服务与资源范围查询标准指标',
      icon: 'IconPulse',
      group: 'observe',
      workbenchId: 'monitoring',
      requiresAuth: true,
      tabbar: true,
      navVisible: true,
      parentId: 'monitoring-workbench',
      menuId: 'monitoring-workbench-metrics',
      permissionKey: 'observe.monitoring.view',
      scopeMode: 'passive',
    },
    shell: 'app',
    load: async () => {
      const module = await import('./metrics-page')
      return { default: module.ObservabilityMetricsPage }
    },
  },
  {
    meta: {
      id: 'monitoring-workbench-traces',
      path: '/monitoring-workbench/traces',
      title: '链路',
      description: '按服务查询 Trace 与 Span',
      icon: 'IconShare',
      group: 'observe',
      workbenchId: 'monitoring',
      requiresAuth: true,
      tabbar: true,
      navVisible: true,
      parentId: 'monitoring-workbench',
      menuId: 'monitoring-workbench-traces',
      permissionKey: 'observe.monitoring.view',
      scopeMode: 'passive',
    },
    shell: 'app',
    load: async () => {
      const module = await import('./traces-page')
      return { default: module.ObservabilityTracesPage }
    },
  },
] as const)
