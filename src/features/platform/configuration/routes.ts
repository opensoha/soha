import { defineRoutes } from '@/routes/definitions'

export const configurationViewPermissions = [
  'platform.configuration.config-maps.view',
  'platform.configuration.secrets.view',
  'platform.configuration.resource-quotas.view',
  'platform.configuration.limit-ranges.view',
  'platform.configuration.horizontal-pod-autoscalers.view',
  'platform.configuration.pod-disruption-budgets.view',
  'platform.configuration.priority-classes.view',
  'platform.configuration.runtime-classes.view',
  'platform.configuration.leases.view',
  'platform.configuration.mutating-webhook-configurations.view',
  'platform.configuration.validating-webhook-configurations.view',
]

export const configurationRoutes = defineRoutes([
  {
    meta: {
      id: 'configuration-configmaps',
      path: '/configuration/configmaps',
      title: 'ConfigMaps',
      description: '配置映射',
      icon: 'IconSetting',
      group: 'platform',
      requiresAuth: true,
      tabbar: true,
      navVisible: true,
      parentId: 'configuration',
      menuId: 'configuration-configmaps',
      permissionKey: 'platform.configuration.config-maps.view',
      scopeMode: 'namespace',
      workspace: 'resource',
    },
    shell: 'app',
    load: async () => {
      const module = await import('./configmaps/list-page')
      return { default: module.ConfigurationConfigMapsPage }
    },
  },
  {
    meta: {
      id: 'configuration-configmap-detail',
      path: '/configuration/configmaps/:configMapName',
      title: 'ConfigMap Detail',
      description: 'ConfigMap 详情',
      requiresAuth: true,
      tabbar: false,
      navVisible: false,
      scopeMode: 'namespace',
    },
    shell: 'app',
    inheritMetaFrom: 'configuration-configmaps',
    load: async () => {
      const module = await import('./configmaps/detail-page')
      return { default: module.ConfigMapDetailPage }
    },
  },
  {
    meta: {
      id: 'configuration-secrets',
      path: '/configuration/secrets',
      title: 'Secrets',
      description: '密钥对象',
      icon: 'IconSetting',
      group: 'platform',
      requiresAuth: true,
      tabbar: true,
      navVisible: true,
      parentId: 'configuration',
      menuId: 'configuration-secrets',
      permissionKey: 'platform.configuration.secrets.view',
      scopeMode: 'namespace',
      workspace: 'resource',
    },
    shell: 'app',
    load: async () => {
      const module = await import('./secrets/list-page')
      return { default: module.ConfigurationSecretsPage }
    },
  },
  {
    meta: {
      id: 'configuration-secret-detail',
      path: '/configuration/secrets/:secretName',
      title: 'Secret Detail',
      description: 'Secret 详情',
      requiresAuth: true,
      tabbar: false,
      navVisible: false,
      permissionKey: 'platform.configuration.secrets.view',
      scopeMode: 'namespace',
    },
    shell: 'app',
    inheritMetaFrom: 'configuration-secrets',
    load: async () => {
      const module = await import('./secrets/detail-page')
      return { default: module.SecretDetailPage }
    },
  },
  {
    meta: {
      id: 'configuration-resourcequotas',
      path: '/configuration/resourcequotas',
      title: 'ResourceQuotas',
      description: '资源配额',
      icon: 'IconSetting',
      group: 'platform',
      requiresAuth: true,
      tabbar: true,
      navVisible: true,
      parentId: 'configuration',
      menuId: 'configuration-resourcequotas',
      permissionKey: 'platform.configuration.resource-quotas.view',
      scopeMode: 'namespace',
      workspace: 'resource',
    },
    shell: 'app',
    load: async () => {
      const module = await import('./resourcequotas/list-page')
      return { default: module.ConfigurationResourceQuotasPage }
    },
  },
  {
    meta: {
      id: 'configuration-resourcequota-detail',
      path: '/configuration/resourcequotas/:name',
      title: 'ResourceQuota Detail',
      description: 'ResourceQuota 详情',
      requiresAuth: true,
      tabbar: false,
      navVisible: false,
      scopeMode: 'namespace',
    },
    shell: 'app',
    inheritMetaFrom: 'configuration-resourcequotas',
    load: async () => {
      const module = await import('./resourcequotas/detail-page')
      return { default: module.ConfigurationResourceQuotaDetailPage }
    },
  },
  {
    meta: {
      id: 'configuration-limitranges',
      path: '/configuration/limitranges',
      title: 'LimitRanges',
      description: '资源限制范围',
      icon: 'IconSetting',
      group: 'platform',
      requiresAuth: true,
      tabbar: true,
      navVisible: true,
      parentId: 'configuration',
      menuId: 'configuration-limitranges',
      permissionKey: 'platform.configuration.limit-ranges.view',
      scopeMode: 'namespace',
      workspace: 'resource',
    },
    shell: 'app',
    load: async () => {
      const module = await import('./limitranges/list-page')
      return { default: module.ConfigurationLimitRangesPage }
    },
  },
  {
    meta: {
      id: 'configuration-limitrange-detail',
      path: '/configuration/limitranges/:name',
      title: 'LimitRange Detail',
      description: 'LimitRange 详情',
      requiresAuth: true,
      tabbar: false,
      navVisible: false,
      scopeMode: 'namespace',
    },
    shell: 'app',
    inheritMetaFrom: 'configuration-limitranges',
    load: async () => {
      const module = await import('./limitranges/detail-page')
      return { default: module.ConfigurationLimitRangeDetailPage }
    },
  },
  {
    meta: {
      id: 'configuration-hpas',
      path: '/configuration/hpas',
      title: 'HorizontalPodAutoscalers',
      description: '自动扩缩容',
      icon: 'IconSetting',
      group: 'platform',
      requiresAuth: true,
      tabbar: true,
      navVisible: true,
      parentId: 'configuration',
      menuId: 'configuration-hpas',
      permissionKey: 'platform.configuration.horizontal-pod-autoscalers.view',
      scopeMode: 'namespace',
      workspace: 'resource',
    },
    shell: 'app',
    load: async () => {
      const module = await import('./hpas/list-page')
      return { default: module.ConfigurationHPAPage }
    },
  },
  {
    meta: {
      id: 'configuration-hpa-detail',
      path: '/configuration/hpas/:name',
      title: 'HorizontalPodAutoscaler Detail',
      description: 'HorizontalPodAutoscaler 详情',
      requiresAuth: true,
      tabbar: false,
      navVisible: false,
      scopeMode: 'namespace',
    },
    shell: 'app',
    inheritMetaFrom: 'configuration-hpas',
    load: async () => {
      const module = await import('./hpas/detail-page')
      return { default: module.ConfigurationHPADetailPage }
    },
  },
  {
    meta: {
      id: 'configuration-poddisruptionbudgets',
      path: '/configuration/poddisruptionbudgets',
      title: 'PodDisruptionBudgets',
      description: '驱逐预算',
      icon: 'IconSetting',
      group: 'platform',
      requiresAuth: true,
      tabbar: true,
      navVisible: true,
      parentId: 'configuration',
      menuId: 'configuration-poddisruptionbudgets',
      permissionKey: 'platform.configuration.pod-disruption-budgets.view',
      scopeMode: 'namespace',
      workspace: 'resource',
    },
    shell: 'app',
    load: async () => {
      const module = await import('./poddisruptionbudgets/list-page')
      return { default: module.ConfigurationPodDisruptionBudgetsPage }
    },
  },
  {
    meta: {
      id: 'configuration-poddisruptionbudget-detail',
      path: '/configuration/poddisruptionbudgets/:name',
      title: 'PodDisruptionBudget Detail',
      description: 'PodDisruptionBudget 详情',
      requiresAuth: true,
      tabbar: false,
      navVisible: false,
      scopeMode: 'namespace',
    },
    shell: 'app',
    inheritMetaFrom: 'configuration-poddisruptionbudgets',
    load: async () => {
      const module = await import('./poddisruptionbudgets/detail-page')
      return { default: module.ConfigurationPDBDetailPage }
    },
  },
  {
    meta: {
      id: 'configuration-priorityclasses',
      path: '/configuration/priorityclasses',
      title: 'PriorityClasses',
      description: '优先级类',
      icon: 'IconSetting',
      group: 'platform',
      requiresAuth: true,
      tabbar: true,
      navVisible: true,
      parentId: 'configuration',
      menuId: 'configuration-priorityclasses',
      permissionKey: 'platform.configuration.priority-classes.view',
      scopeMode: 'cluster',
      workspace: 'resource',
    },
    shell: 'app',
    load: async () => {
      const module = await import('./priorityclasses/list-page')
      return { default: module.ConfigurationPriorityClassesPage }
    },
  },
  {
    meta: {
      id: 'configuration-priorityclass-detail',
      path: '/configuration/priorityclasses/:name',
      title: 'PriorityClass 详情',
      description: '优先级类详情',
      scopeMode: 'cluster',
      requiresAuth: true,
      tabbar: false,
      navVisible: false,
      parentId: 'configuration-priorityclasses',
    },
    inheritMetaFrom: 'configuration-priorityclasses',
    shell: 'app',
    load: async () => {
      const module = await import('./priorityclasses/detail-page')
      return { default: module.ConfigurationPriorityClassDetailPage }
    },
  },
  {
    meta: {
      id: 'configuration-runtimeclasses',
      path: '/configuration/runtimeclasses',
      title: 'RuntimeClasses',
      description: '运行时类',
      icon: 'IconSetting',
      group: 'platform',
      requiresAuth: true,
      tabbar: true,
      navVisible: true,
      parentId: 'configuration',
      menuId: 'configuration-runtimeclasses',
      permissionKey: 'platform.configuration.runtime-classes.view',
      scopeMode: 'cluster',
      workspace: 'resource',
    },
    shell: 'app',
    load: async () => {
      const module = await import('./runtimeclasses/list-page')
      return { default: module.ConfigurationRuntimeClassesPage }
    },
  },
  {
    meta: {
      id: 'configuration-runtimeclass-detail',
      path: '/configuration/runtimeclasses/:name',
      title: 'RuntimeClass Detail',
      description: '运行时类详情',
      requiresAuth: true,
      tabbar: false,
      navVisible: false,
      scopeMode: 'cluster',
      parentId: 'configuration-runtimeclasses',
    },
    inheritMetaFrom: 'configuration-runtimeclasses',
    shell: 'app',
    load: async () => {
      const module = await import('./runtimeclasses/detail-page')
      return { default: module.ConfigurationRuntimeClassDetailPage }
    },
  },
  {
    meta: {
      id: 'configuration-leases',
      path: '/configuration/leases',
      title: 'Leases',
      description: '租约',
      icon: 'IconSetting',
      group: 'platform',
      requiresAuth: true,
      tabbar: true,
      navVisible: true,
      parentId: 'configuration',
      menuId: 'configuration-leases',
      permissionKey: 'platform.configuration.leases.view',
      scopeMode: 'namespace',
      workspace: 'resource',
    },
    shell: 'app',
    load: async () => {
      const module = await import('./leases/list-page')
      return { default: module.ConfigurationLeasesPage }
    },
  },
  {
    meta: {
      id: 'configuration-lease-detail',
      path: '/configuration/leases/:name',
      title: 'Lease Detail',
      description: '租约详情',
      group: 'platform',
      requiresAuth: true,
      tabbar: false,
      navVisible: false,
      scopeMode: 'namespace',
      parentId: 'configuration-leases',
    },
    inheritMetaFrom: 'configuration-leases',
    shell: 'app',
    load: async () => {
      const module = await import('./leases/detail-page')
      return { default: module.ConfigurationLeaseDetailPage }
    },
  },
  {
    meta: {
      id: 'configuration-mutatingwebhookconfigurations',
      path: '/configuration/mutatingwebhookconfigurations',
      title: 'MutatingWebhookConfigurations',
      description: '变更 Webhook',
      icon: 'IconSetting',
      group: 'platform',
      requiresAuth: true,
      tabbar: true,
      navVisible: true,
      parentId: 'configuration',
      menuId: 'configuration-mutatingwebhookconfigurations',
      permissionKey: 'platform.configuration.mutating-webhook-configurations.view',
      scopeMode: 'cluster',
      workspace: 'resource',
    },
    shell: 'app',
    load: async () => {
      const module = await import('./mutatingwebhookconfigurations/list-page')
      return { default: module.ConfigurationMutatingWebhooksPage }
    },
  },
  {
    meta: {
      id: 'configuration-mutatingwebhookconfiguration-detail',
      path: '/configuration/mutatingwebhookconfigurations/:name',
      title: 'MutatingWebhookConfiguration Detail',
      description: 'MutatingWebhookConfiguration 详情',
      requiresAuth: true,
      tabbar: false,
      navVisible: false,
      scopeMode: 'cluster',
    },
    shell: 'app',
    inheritMetaFrom: 'configuration-mutatingwebhookconfigurations',
    load: async () => {
      const module = await import('./mutatingwebhookconfigurations/detail-page')
      return { default: module.ConfigurationMutatingWebhookConfigurationDetailPage }
    },
  },
  {
    meta: {
      id: 'configuration-validatingwebhookconfigurations',
      path: '/configuration/validatingwebhookconfigurations',
      title: 'ValidatingWebhookConfigurations',
      description: '校验 Webhook',
      icon: 'IconSetting',
      group: 'platform',
      requiresAuth: true,
      tabbar: true,
      navVisible: true,
      parentId: 'configuration',
      menuId: 'configuration-validatingwebhookconfigurations',
      permissionKey: 'platform.configuration.validating-webhook-configurations.view',
      scopeMode: 'cluster',
      workspace: 'resource',
    },
    shell: 'app',
    load: async () => {
      const module = await import('./validatingwebhookconfigurations/list-page')
      return { default: module.ConfigurationValidatingWebhooksPage }
    },
  },
  {
    meta: {
      id: 'configuration-validatingwebhookconfiguration-detail',
      path: '/configuration/validatingwebhookconfigurations/:name',
      title: 'ValidatingWebhookConfiguration Detail',
      description: 'ValidatingWebhookConfiguration 详情',
      requiresAuth: true,
      tabbar: false,
      navVisible: false,
      scopeMode: 'cluster',
    },
    shell: 'app',
    inheritMetaFrom: 'configuration-validatingwebhookconfigurations',
    load: async () => {
      const module = await import('./validatingwebhookconfigurations/detail-page')
      return { default: module.ConfigurationValidatingWebhookConfigurationDetailPage }
    },
  },
] as const)
