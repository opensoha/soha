import { defineRoutes } from '@/routes/definitions'
import { observabilityAlertRoutes } from './alerts/routes'
import { observabilityDashboardRoutes } from './dashboards/routes'
import { observabilityEventRoutes } from './events/routes'
import { observabilityHealingRoutes } from './healing/routes'
import { observabilityIntegrationRoutes } from './integrations/routes'
import { observabilityLogRoutes } from './logs/routes'
import { observabilityNotificationRoutes } from './notifications/routes'
import { observabilityOncallRoutes } from './oncall/routes'
import { observabilityOverviewRoutes } from './overview/routes'
import { observabilityProviderRoutes } from './providers/routes'
import { observabilityRuleRoutes } from './rules/routes'
import { observabilitySignalRoutes } from './signals/routes'

export const observabilityParentRoutes = defineRoutes([
  {
    meta: {
      id: 'monitoring-workbench',
      path: '/monitoring-workbench',
      title: '可观测性工作台',
      description: 'Logs、Metrics、Traces、Profiles 与告警响应',
      icon: 'IconAlertTriangle',
      group: 'observe',
      workbenchId: 'monitoring',
      requiresAuth: true,
      tabbar: false,
      navVisible: true,
      menuId: 'monitoring-workbench',
      permissionKey: 'observe.monitoring.view',
      scopeMode: 'passive',
      workspace: 'resource',
    },
    shell: 'app',
    redirectTo: '/monitoring-workbench/overview',
  },
] as const)

export const observabilityCompatibilityRoutes = defineRoutes([
  {
    meta: {
      id: 'observability-compat',
      path: '/observability',
      title: '告警中心',
      description: '兼容旧入口',
      icon: 'IconAlertTriangle',
      group: 'observe',
      requiresAuth: true,
      tabbar: false,
      navVisible: false,
      permissionKey: 'observe.monitoring.view',
      scopeMode: 'passive',
      workspace: 'resource',
    },
    shell: 'app',
    redirectTo: '/monitoring-workbench',
  },
  {
    meta: {
      id: 'observability-monitoring-compat',
      path: '/observability/monitoring',
      title: '旧概览入口',
      description: '兼容旧入口',
      icon: 'IconPulse',
      group: 'observe',
      requiresAuth: true,
      tabbar: false,
      navVisible: false,
      parentId: 'monitoring-workbench',
      permissionKey: 'observe.monitoring.view',
      scopeMode: 'passive',
    },
    shell: 'app',
    redirectTo: '/monitoring-workbench/overview',
  },
  {
    meta: {
      id: 'observability-rules-compat',
      path: '/observability/rules',
      title: '旧规则入口',
      description: '兼容旧入口',
      icon: 'IconFileSearch',
      group: 'observe',
      requiresAuth: true,
      tabbar: false,
      navVisible: false,
      parentId: 'monitoring-workbench',
      permissionKey: 'observe.alert-rules.view',
      scopeMode: 'passive',
    },
    shell: 'app',
    redirectTo: '/monitoring-workbench/rules',
  },
  {
    meta: {
      id: 'observability-alerts-compat',
      path: '/observability/alerts',
      title: '旧告警入口',
      description: '兼容旧入口',
      icon: 'IconAlertTriangle',
      group: 'observe',
      requiresAuth: true,
      tabbar: false,
      navVisible: false,
      parentId: 'monitoring-workbench',
      permissionKey: 'observe.alerts.view',
      scopeMode: 'passive',
    },
    shell: 'app',
    redirectTo: '/monitoring-workbench/alerts',
  },
  {
    meta: {
      id: 'observability-notifications-compat',
      path: '/observability/notifications',
      title: '旧通知入口',
      description: '兼容旧入口',
      icon: 'IconBell',
      group: 'observe',
      requiresAuth: true,
      tabbar: false,
      navVisible: false,
      parentId: 'monitoring-workbench',
      permissionKey: 'observe.notifications.view',
      scopeMode: 'passive',
    },
    shell: 'app',
    redirectTo: '/monitoring-workbench/notifications',
  },
  {
    meta: {
      id: 'observability-healing-compat',
      path: '/observability/healing',
      title: '旧自愈入口',
      description: '兼容旧入口',
      icon: 'IconTool',
      group: 'observe',
      requiresAuth: true,
      tabbar: false,
      navVisible: false,
      parentId: 'monitoring-workbench',
      permissionKey: 'observe.healing.view',
      scopeMode: 'passive',
    },
    shell: 'app',
    redirectTo: '/monitoring-workbench/healing',
  },
  {
    meta: {
      id: 'observability-oncall-compat',
      path: '/observability/oncall',
      title: '旧值班入口',
      description: '兼容旧入口',
      icon: 'IconUserCircle',
      group: 'observe',
      requiresAuth: true,
      tabbar: false,
      navVisible: false,
      parentId: 'monitoring-workbench',
      permissionKey: 'observe.oncall.view',
      scopeMode: 'passive',
    },
    shell: 'app',
    redirectTo: '/monitoring-workbench/oncall',
  },
  {
    meta: {
      id: 'observability-events-compat',
      path: '/observability/events',
      title: '旧事件入口',
      description: '兼容旧入口',
      icon: 'IconBell',
      group: 'observe',
      requiresAuth: true,
      tabbar: false,
      navVisible: false,
      parentId: 'monitoring-workbench',
      permissionKey: 'observe.events.view',
      scopeMode: 'passive',
    },
    shell: 'app',
    redirectTo: '/monitoring-workbench/events',
  },
  {
    meta: {
      id: 'monitoring-workbench-explore-compat',
      path: '/monitoring-workbench/explore',
      title: '旧 Explore 入口',
      description: '兼容旧信号探索链接',
      icon: 'IconPulse',
      group: 'observe',
      requiresAuth: true,
      tabbar: false,
      navVisible: false,
      parentId: 'monitoring-workbench',
      permissionKey: 'observe.monitoring.view',
      scopeMode: 'passive',
    },
    shell: 'app',
    load: async () => {
      const module = await import('./signals/explore-redirect')
      return { default: module.ObservabilityExploreRedirect }
    },
  },
] as const)

export const observabilityRouteManifests = [
  observabilityParentRoutes,
  observabilityOverviewRoutes,
  observabilityDashboardRoutes,
  observabilitySignalRoutes,
  observabilityLogRoutes,
  observabilityProviderRoutes,
  observabilityIntegrationRoutes,
  observabilityRuleRoutes,
  observabilityAlertRoutes,
  observabilityNotificationRoutes,
  observabilityHealingRoutes,
  observabilityOncallRoutes,
  observabilityEventRoutes,
  observabilityCompatibilityRoutes,
] as const
