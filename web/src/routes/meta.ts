import { MENU_SECTION_ORDER } from '@/features/system/menu-schema'
import type { PermissionSnapshot, RouteMeta, RuntimeMenuNode, VisibleMenu } from '@/types'

export const routeMeta: RouteMeta[] = [
  { id: 'overview', path: '/', title: '概览', description: '平台总览', icon: 'IconDesktop', group: 'overview', requiresAuth: true, tabbar: true, navVisible: true, menuId: 'dashboard', permissionKey: 'overview.view' },

  { id: 'cluster-resources-nodes', path: '/cluster-resources/nodes', title: '节点', description: '节点管理', icon: 'IconServer', group: 'platform', requiresAuth: true, tabbar: true, navVisible: true, menuId: 'cluster-resources-nodes', permissionKey: 'platform.nodes.view' },
  { id: 'cluster-resources-node-detail', path: '/cluster-resources/nodes/:nodeName', title: '节点详情', description: '节点详情', icon: 'IconServer', group: 'platform', requiresAuth: true, tabbar: false, navVisible: false, parentId: 'cluster-resources-nodes' },

  { id: 'workloads', path: '/workloads', title: '工作负载', description: '工作负载管理', icon: 'IconGridView', group: 'platform', requiresAuth: true, tabbar: false, navVisible: true, redirectTo: '/workloads/overview', menuId: 'workloads', permissionKey: 'platform.workloads.view' },
  { id: 'workloads-overview', path: '/workloads/overview', title: 'Overview', description: '资源概览与事件', icon: 'IconGridView', group: 'platform', requiresAuth: true, tabbar: true, navVisible: true, parentId: 'workloads' },
  { id: 'workloads-deployments', path: '/workloads/deployments', title: 'Deployments', description: '部署管理', icon: 'IconGridView', group: 'platform', requiresAuth: true, tabbar: true, navVisible: true, parentId: 'workloads' },
  { id: 'workloads-pods', path: '/workloads/pods', title: 'Pods', description: 'Pod 管理', icon: 'IconGridView', group: 'platform', requiresAuth: true, tabbar: true, navVisible: true, parentId: 'workloads' },
  { id: 'workloads-statefulsets', path: '/workloads/statefulsets', title: 'StatefulSets', description: '有状态副本集', icon: 'IconGridView', group: 'platform', requiresAuth: true, tabbar: true, navVisible: true, parentId: 'workloads' },
  { id: 'workloads-daemonsets', path: '/workloads/daemonsets', title: 'DaemonSets', description: '守护进程集', icon: 'IconGridView', group: 'platform', requiresAuth: true, tabbar: true, navVisible: true, parentId: 'workloads' },
  { id: 'workloads-jobs', path: '/workloads/jobs', title: 'Jobs', description: '批量作业', icon: 'IconGridView', group: 'platform', requiresAuth: true, tabbar: true, navVisible: true, parentId: 'workloads' },
  { id: 'workloads-cronjobs', path: '/workloads/cronjobs', title: 'CronJobs', description: '定时任务', icon: 'IconGridView', group: 'platform', requiresAuth: true, tabbar: true, navVisible: true, parentId: 'workloads' },
  { id: 'workloads-replicasets', path: '/workloads/replicasets', title: 'ReplicaSets', description: '副本集', icon: 'IconGridView', group: 'platform', requiresAuth: true, tabbar: true, navVisible: true, parentId: 'workloads' },
  { id: 'workloads-replicationcontrollers', path: '/workloads/replicationcontrollers', title: 'ReplicationControllers', description: '复制控制器', icon: 'IconGridView', group: 'platform', requiresAuth: true, tabbar: true, navVisible: true, parentId: 'workloads' },

  { id: 'configuration', path: '/configuration', title: 'Configuration', description: '平台配置资源', icon: 'IconSetting', group: 'platform', requiresAuth: true, tabbar: false, navVisible: true, redirectTo: '/configuration/configmaps', menuId: 'configuration', permissionKey: 'platform.configuration.view' },
  { id: 'configuration-configmaps', path: '/configuration/configmaps', title: 'ConfigMaps', description: '配置映射', icon: 'IconSetting', group: 'platform', requiresAuth: true, tabbar: true, navVisible: true, parentId: 'configuration' },
  { id: 'configuration-secrets', path: '/configuration/secrets', title: 'Secrets', description: '密钥对象', icon: 'IconSetting', group: 'platform', requiresAuth: true, tabbar: true, navVisible: true, parentId: 'configuration' },
  { id: 'configuration-resourcequotas', path: '/configuration/resourcequotas', title: 'ResourceQuotas', description: '资源配额', icon: 'IconSetting', group: 'platform', requiresAuth: true, tabbar: true, navVisible: true, parentId: 'configuration' },
  { id: 'configuration-limitranges', path: '/configuration/limitranges', title: 'LimitRanges', description: '资源限制范围', icon: 'IconSetting', group: 'platform', requiresAuth: true, tabbar: true, navVisible: true, parentId: 'configuration' },
  { id: 'configuration-hpas', path: '/configuration/hpas', title: 'HorizontalPodAutoscalers', description: '自动扩缩容', icon: 'IconSetting', group: 'platform', requiresAuth: true, tabbar: true, navVisible: true, parentId: 'configuration' },
  { id: 'configuration-poddisruptionbudgets', path: '/configuration/poddisruptionbudgets', title: 'PodDisruptionBudgets', description: '驱逐预算', icon: 'IconSetting', group: 'platform', requiresAuth: true, tabbar: true, navVisible: true, parentId: 'configuration' },
  { id: 'configuration-priorityclasses', path: '/configuration/priorityclasses', title: 'PriorityClasses', description: '优先级类', icon: 'IconSetting', group: 'platform', requiresAuth: true, tabbar: true, navVisible: true, parentId: 'configuration' },
  { id: 'configuration-runtimeclasses', path: '/configuration/runtimeclasses', title: 'RuntimeClasses', description: '运行时类', icon: 'IconSetting', group: 'platform', requiresAuth: true, tabbar: true, navVisible: true, parentId: 'configuration' },
  { id: 'configuration-leases', path: '/configuration/leases', title: 'Leases', description: '租约', icon: 'IconSetting', group: 'platform', requiresAuth: true, tabbar: true, navVisible: true, parentId: 'configuration' },
  { id: 'configuration-mutatingwebhookconfigurations', path: '/configuration/mutatingwebhookconfigurations', title: 'MutatingWebhookConfigurations', description: '变更 Webhook', icon: 'IconSetting', group: 'platform', requiresAuth: true, tabbar: true, navVisible: true, parentId: 'configuration' },
  { id: 'configuration-validatingwebhookconfigurations', path: '/configuration/validatingwebhookconfigurations', title: 'ValidatingWebhookConfigurations', description: '校验 Webhook', icon: 'IconSetting', group: 'platform', requiresAuth: true, tabbar: true, navVisible: true, parentId: 'configuration' },

  { id: 'network', path: '/network', title: '网络', description: '网络资源', icon: 'IconConnection', group: 'platform', requiresAuth: true, tabbar: false, navVisible: true, redirectTo: '/network/topology', menuId: 'network', permissionKey: 'platform.network.view' },
  { id: 'network-topology', path: '/network/topology', title: '网络拓扑', description: '入口、路由、Service 与后端的网络拓扑', icon: 'IconConnection', group: 'platform', requiresAuth: true, tabbar: true, navVisible: true, parentId: 'network' },
  { id: 'network-services', path: '/network/services', title: 'Services', description: '服务管理', icon: 'IconConnection', group: 'platform', requiresAuth: true, tabbar: true, navVisible: true, parentId: 'network' },
  { id: 'network-service-detail', path: '/network/services/:serviceName', title: 'Service Detail', description: '服务详情', icon: 'IconConnection', group: 'platform', requiresAuth: true, tabbar: false, navVisible: false, parentId: 'network-services' },
  { id: 'network-ingresses', path: '/network/ingresses', title: 'Ingresses', description: '入口管理', icon: 'IconConnection', group: 'platform', requiresAuth: true, tabbar: true, navVisible: true, parentId: 'network' },
  { id: 'network-gateways', path: '/network/gateways', title: 'Gateways', description: '网关管理', icon: 'IconConnection', group: 'platform', requiresAuth: true, tabbar: true, navVisible: true, parentId: 'network' },
  { id: 'network-endpointslices', path: '/network/endpointslices', title: 'EndpointSlices', description: '服务后端切片', icon: 'IconConnection', group: 'platform', requiresAuth: true, tabbar: true, navVisible: true, parentId: 'network' },
  { id: 'network-ingressclasses', path: '/network/ingressclasses', title: 'IngressClasses', description: 'Ingress 类', icon: 'IconConnection', group: 'platform', requiresAuth: true, tabbar: true, navVisible: true, parentId: 'network' },
  { id: 'network-networkpolicies', path: '/network/networkpolicies', title: 'NetworkPolicies', description: '网络策略', icon: 'IconConnection', group: 'platform', requiresAuth: true, tabbar: true, navVisible: true, parentId: 'network' },
  { id: 'network-port-forward', path: '/network/port-forward', title: 'Port Forward', description: '端口转发', icon: 'IconConnection', group: 'platform', requiresAuth: true, tabbar: true, navVisible: true, parentId: 'network' },

  { id: 'storage', path: '/storage', title: '存储', description: '存储资源', icon: 'IconServer', group: 'platform', requiresAuth: true, tabbar: false, navVisible: true, redirectTo: '/storage/persistentvolumeclaims', menuId: 'storage', permissionKey: 'platform.storage.view' },
  { id: 'storage-pvc', path: '/storage/persistentvolumeclaims', title: 'PVC', description: '持久卷声明', icon: 'IconServer', group: 'platform', requiresAuth: true, tabbar: true, navVisible: true, parentId: 'storage' },
  { id: 'storage-pv', path: '/storage/persistentvolumes', title: 'PV', description: '持久卷', icon: 'IconServer', group: 'platform', requiresAuth: true, tabbar: true, navVisible: true, parentId: 'storage' },
  { id: 'storage-classes', path: '/storage/storageclasses', title: 'StorageClasses', description: '存储类', icon: 'IconServer', group: 'platform', requiresAuth: true, tabbar: true, navVisible: true, parentId: 'storage' },

  { id: 'platform-access-control', path: '/platform-access-control', title: 'RBAC', description: 'Kubernetes RBAC 资源', icon: 'IconShield', group: 'platform', requiresAuth: true, tabbar: false, navVisible: true, redirectTo: '/platform-access-control/serviceaccounts', menuId: 'platform-access-control' },
  { id: 'platform-access-control-serviceaccounts', path: '/platform-access-control/serviceaccounts', title: 'ServiceAccounts', description: '服务账户', icon: 'IconShield', group: 'platform', requiresAuth: true, tabbar: true, navVisible: true, parentId: 'platform-access-control' },
  { id: 'platform-access-control-clusterroles', path: '/platform-access-control/clusterroles', title: 'ClusterRoles', description: '集群角色', icon: 'IconShield', group: 'platform', requiresAuth: true, tabbar: true, navVisible: true, parentId: 'platform-access-control' },
  { id: 'platform-access-control-roles', path: '/platform-access-control/roles', title: 'Roles', description: '命名空间角色', icon: 'IconShield', group: 'platform', requiresAuth: true, tabbar: true, navVisible: true, parentId: 'platform-access-control' },
  { id: 'platform-access-control-clusterrolebindings', path: '/platform-access-control/clusterrolebindings', title: 'ClusterRoleBindings', description: '集群角色绑定', icon: 'IconShield', group: 'platform', requiresAuth: true, tabbar: true, navVisible: true, parentId: 'platform-access-control' },
  { id: 'platform-access-control-rolebindings', path: '/platform-access-control/rolebindings', title: 'RoleBindings', description: '命名空间角色绑定', icon: 'IconShield', group: 'platform', requiresAuth: true, tabbar: true, navVisible: true, parentId: 'platform-access-control' },

  { id: 'helm', path: '/helm', title: 'Helm', description: 'Helm 管理', icon: 'IconPuzzle', group: 'platform', requiresAuth: true, tabbar: false, navVisible: true, redirectTo: '/helm/releases', menuId: 'helm', permissionKey: 'platform.helm.view' },
  { id: 'helm-releases', path: '/helm/releases', title: 'Helm Releases', description: 'Helm 发布', icon: 'IconPuzzle', group: 'platform', requiresAuth: true, tabbar: true, navVisible: true, parentId: 'helm' },
  { id: 'helm-release-detail', path: '/helm/releases/:releaseName', title: 'Helm Release Detail', description: 'Helm 发布详情', icon: 'IconPuzzle', group: 'platform', requiresAuth: true, tabbar: false, navVisible: false, parentId: 'helm-releases' },
  { id: 'helm-charts', path: '/helm/charts', title: 'Helm Charts', description: 'Helm 图表', icon: 'IconPuzzle', group: 'platform', requiresAuth: true, tabbar: true, navVisible: true, parentId: 'helm' },

  { id: 'extensions', path: '/extensions', title: 'CRD', description: 'CRD 管理', icon: 'IconPuzzle', group: 'platform', requiresAuth: true, tabbar: true, navVisible: true, menuId: 'extensions', permissionKey: 'platform.extensions.view' },
  { id: 'extensions-group-detail', path: '/extensions/apis/:groupName', title: 'CRD API Detail', description: 'CRD API 详情', icon: 'IconPuzzle', group: 'platform', requiresAuth: true, tabbar: false, navVisible: false, parentId: 'extensions' },
  { id: 'cluster-resources-namespaces', path: '/cluster-resources/namespaces', title: '命名空间', description: '命名空间管理', icon: 'IconServer', group: 'platform', requiresAuth: true, tabbar: true, navVisible: true, menuId: 'cluster-resources-namespaces', permissionKey: 'platform.namespaces.view' },
  { id: 'clusters', path: '/clusters', title: '集群管理', description: '集群生命周期管理', icon: 'IconGlobe', group: 'platform', requiresAuth: true, tabbar: true, navVisible: true, menuId: 'clusters', permissionKey: 'platform.clusters.view' },
  { id: 'cluster-detail', path: '/clusters/:clusterId', title: '集群详情', description: '集群详情', icon: 'IconGlobe', group: 'platform', requiresAuth: true, tabbar: false, navVisible: false, parentId: 'clusters' },

  { id: 'applications', path: '/applications', title: '应用管理', description: '应用交付', icon: 'IconAppCenter', group: 'delivery', requiresAuth: true, tabbar: true, navVisible: true, menuId: 'builds', permissionKey: 'delivery.applications.view' },
  { id: 'application-detail', path: '/applications/:applicationId', title: '应用详情', description: '应用交付详情', icon: 'IconAppCenter', group: 'delivery', requiresAuth: true, tabbar: false, navVisible: false, parentId: 'applications' },
  { id: 'business-lines', path: '/business-lines', title: '业务线管理', description: '业务线主数据', icon: 'IconAppCenter', group: 'catalog', requiresAuth: true, tabbar: true, navVisible: true, permissionKey: 'delivery.business-lines.view' },
  { id: 'delivery-environments', path: '/delivery-environments', title: '环境管理', description: '交付环境主数据', icon: 'IconAppCenter', group: 'catalog', requiresAuth: true, tabbar: true, navVisible: true, permissionKey: 'delivery.environments.view' },
  { id: 'application-environments', path: '/application-environments', title: '应用环境绑定', description: '应用与环境绑定', icon: 'IconAppCenter', group: 'catalog', requiresAuth: true, tabbar: true, navVisible: true, permissionKey: 'delivery.application-environments.view' },
  { id: 'application-environment-detail', path: '/application-environments/:applicationEnvironmentId', title: '环境详情', description: '应用环境详情', icon: 'IconAppCenter', group: 'delivery', requiresAuth: true, tabbar: false, navVisible: false, parentId: 'application-environments' },
  { id: 'build-templates', path: '/build-templates', title: '构建模板', description: '平台构建模板', icon: 'IconCode', group: 'delivery', requiresAuth: true, tabbar: true, navVisible: true, permissionKey: 'delivery.build-templates.view' },
  { id: 'workflow-templates', path: '/workflow-templates', title: '发布流程模板', description: '交付发布流程模板', icon: 'IconFlow', group: 'delivery', requiresAuth: true, tabbar: true, navVisible: true, permissionKey: 'delivery.workflow-templates.view' },
  { id: 'release-board', path: '/release-board', title: '发布看板', description: '应用环境发布矩阵', icon: 'IconSend', group: 'delivery', requiresAuth: true, tabbar: true, navVisible: true, permissionKey: 'delivery.release-board.view' },
  { id: 'workflows', path: '/workflows', title: '工作流', description: '工作流管理', icon: 'IconFlow', group: 'delivery', requiresAuth: true, tabbar: true, navVisible: true, menuId: 'workflows', permissionKey: 'delivery.workflows.view' },
  { id: 'releases', path: '/releases', title: '发布管理', description: '发布编排', icon: 'IconSend', group: 'delivery', requiresAuth: true, tabbar: true, navVisible: true, menuId: 'releases', permissionKey: 'delivery.releases.view' },
  { id: 'registries', path: '/registries', title: '镜像仓库', description: '镜像仓库连接', icon: 'IconInbox', group: 'delivery', requiresAuth: true, tabbar: true, navVisible: true, menuId: 'registries', permissionKey: 'delivery.registries.view' },

  { id: 'observability', path: '/observability', title: '告警中心', description: '监控、告警、通知和值班协同', icon: 'IconAlertTriangle', group: 'observe', requiresAuth: true, tabbar: false, navVisible: true, redirectTo: '/observability/monitoring', menuId: 'observability', permissionKey: 'observe.monitoring.view' },
  { id: 'monitoring', path: '/observability/monitoring', title: '中心概览', description: '告警与监控概览', icon: 'IconPulse', group: 'observe', requiresAuth: true, tabbar: true, navVisible: true, parentId: 'observability', permissionKey: 'observe.monitoring.view' },
  { id: 'alerts', path: '/observability/alerts', title: '活跃告警', description: '当前告警处理面板', icon: 'IconAlertTriangle', group: 'observe', requiresAuth: true, tabbar: true, navVisible: true, parentId: 'observability', permissionKey: 'observe.alerts.view' },
  { id: 'notifications', path: '/observability/notifications', title: '通知策略', description: '通知渠道与路由策略', icon: 'IconBell', group: 'observe', requiresAuth: true, tabbar: true, navVisible: true, parentId: 'observability', permissionKey: 'observe.notifications.view' },
  { id: 'oncall', path: '/observability/oncall', title: '值班协同', description: '值班轮换与升级联动', icon: 'IconUserCircle', group: 'observe', requiresAuth: true, tabbar: true, navVisible: true, parentId: 'observability', permissionKey: 'observe.oncall.view' },
  { id: 'events', path: '/observability/events', title: '事件流', description: '事件时间线与上下文', icon: 'IconBell', group: 'observe', requiresAuth: true, tabbar: true, navVisible: true, parentId: 'observability', permissionKey: 'observe.events.view' },

  { id: 'ai-observe', path: '/ai-observe', title: 'AI观测分析中心', description: 'AIOps 总览与调查入口', icon: 'IconComment', group: 'observe', requiresAuth: true, tabbar: true, navVisible: true, menuId: 'assistant', permissionKey: 'observe.ai.view' },
  { id: 'ai-workbench', path: '/ai-observe/workbench', title: '调查工作台', description: '统一承载 AI Chat、根因、性能与链路分析', icon: 'IconComment', group: 'observe', requiresAuth: true, tabbar: true, navVisible: true, parentId: 'ai-observe', permissionKey: 'observe.ai.chat' },
  { id: 'ai-operations', path: '/ai-observe/operations', title: '巡检与自动化', description: '巡检任务、运行记录与自动化策略', icon: 'IconComment', group: 'observe', requiresAuth: true, tabbar: true, navVisible: false, parentId: 'ai-observe', permissionKey: 'observe.ai.view' },
  { id: 'ai-tools', path: '/ai-observe/tools', title: '工具与技能', description: 'MCP adapters、数据源与技能装配', icon: 'IconComment', group: 'observe', requiresAuth: true, tabbar: true, navVisible: false, parentId: 'ai-observe', permissionKey: 'observe.ai.view' },
  { id: 'ai-root-cause', path: '/ai-observe/root-cause', title: '链路根因分析', description: '兼容旧入口，跳转到工作台根因模式', icon: 'IconComment', group: 'observe', requiresAuth: true, tabbar: false, navVisible: false, parentId: 'ai-observe', permissionKey: 'observe.ai.view' },
  { id: 'ai-performance', path: '/ai-observe/performance', title: '性能分析', description: '兼容旧入口，跳转到工作台性能模式', icon: 'IconComment', group: 'observe', requiresAuth: true, tabbar: false, navVisible: false, parentId: 'ai-observe', permissionKey: 'observe.ai.view' },
  { id: 'ai-chat', path: '/ai-observe/chat', title: 'AI Chat', description: '兼容旧入口，跳转到调查工作台', icon: 'IconComment', group: 'observe', requiresAuth: true, tabbar: false, navVisible: false, parentId: 'ai-observe', permissionKey: 'observe.ai.chat' },
  { id: 'ai-inspection', path: '/ai-observe/inspection', title: '智能巡检', description: '兼容旧入口，跳转到巡检与自动化', icon: 'IconComment', group: 'observe', requiresAuth: true, tabbar: false, navVisible: false, parentId: 'ai-observe', permissionKey: 'observe.ai.view' },
  { id: 'chat', path: '/chat', title: 'AI Chat', description: '兼容旧入口', icon: 'IconComment', group: 'observe', requiresAuth: true, tabbar: false, navVisible: false, parentId: 'ai-workbench', permissionKey: 'observe.ai.chat' },

  { id: 'access', path: '/access', title: '访问控制', description: '身份、角色、用户组与策略', icon: 'IconShield', group: 'access', requiresAuth: true, tabbar: false, navVisible: false, menuId: 'access', permissionKey: 'access.users.view', permissionStrategy: 'any-child' },
  { id: 'access-users', path: '/access/users', title: '用户', description: '用户管理', icon: 'IconUser', group: 'access', requiresAuth: true, tabbar: true, navVisible: true, parentId: 'access', menuId: 'access', permissionKey: 'access.users.view' },
  { id: 'access-roles', path: '/access/roles', title: '角色', description: '角色管理', icon: 'IconUserCircle', group: 'access', requiresAuth: true, tabbar: true, navVisible: true, parentId: 'access', menuId: 'access', permissionKey: 'access.roles.view' },
  { id: 'access-teams', path: '/access/teams', title: '用户组', description: '用户组管理', icon: 'IconUserGroup', group: 'access', requiresAuth: true, tabbar: true, navVisible: true, parentId: 'access', menuId: 'access', permissionKey: 'access.groups.view' },
  { id: 'access-policies', path: '/access/policies', title: '策略', description: '策略管理', icon: 'IconShield', group: 'access', requiresAuth: true, tabbar: true, navVisible: true, parentId: 'access', menuId: 'access', permissionKey: 'access.policies.view' },
  { id: 'access-scope-grants', path: '/access/scope-grants', title: '授权范围', description: '业务线环境应用授权', icon: 'IconShield', group: 'access', requiresAuth: true, tabbar: false, navVisible: false, permissionKey: 'access.scope-grants.view' },

  { id: 'system', path: '/system', title: '系统管理', description: '公告、菜单、审计与操作记录', icon: 'IconSetting', group: 'system', requiresAuth: true, tabbar: false, navVisible: false, redirectTo: '/system/online-users' },
  { id: 'system-online-users', path: '/system/online-users', title: '在线用户', description: '在线用户监控', icon: 'IconUser', group: 'system', requiresAuth: true, tabbar: true, navVisible: true, menuId: 'system-online-users', permissionKey: 'system.online-users.view' },
  { id: 'system-announcements', path: '/system/announcements', title: '公告', description: '公告管理', icon: 'IconBell', group: 'system', requiresAuth: true, tabbar: true, navVisible: true, menuId: 'announcements', permissionKey: 'system.announcements.view' },
  { id: 'system-menus', path: '/system/menus', title: '菜单', description: '菜单管理', icon: 'IconMenu', group: 'system', requiresAuth: true, tabbar: true, navVisible: true, menuId: 'menus', permissionKey: 'system.menus.view' },
  { id: 'audit', path: '/system/audit', title: '审计日志', description: '审计记录', icon: 'IconFile', group: 'system', requiresAuth: true, tabbar: true, navVisible: true, menuId: 'audit', permissionKey: 'system.audit.view' },
  { id: 'operations', path: '/system/operations', title: '操作日志', description: '操作记录', icon: 'IconList', group: 'system', requiresAuth: true, tabbar: true, navVisible: true, menuId: 'operations', permissionKey: 'system.operations.view' },

  { id: 'settings', path: '/settings', title: '设置中心', description: '身份与 AI 配置', icon: 'IconSetting', group: 'settings', requiresAuth: true, tabbar: false, navVisible: true, menuId: 'settings', permissionStrategy: 'any-child' },
  { id: 'settings-identity', path: '/settings/identity', title: '身份设置', description: 'OIDC 配置', icon: 'IconSetting', group: 'settings', requiresAuth: true, tabbar: false, navVisible: false, parentId: 'settings', menuId: 'settings', permissionKey: 'settings.identity.view' },
  { id: 'settings-branding', path: '/settings/branding', title: '品牌设置', description: '品牌 Logo 与标题配置', icon: 'IconSetting', group: 'settings', requiresAuth: true, tabbar: false, navVisible: false, parentId: 'settings', menuId: 'settings', permissionKey: 'settings.branding.view' },
  { id: 'settings-monitoring', path: '/settings/monitoring', title: '监控设置', description: 'Prometheus 配置', icon: 'IconSetting', group: 'settings', requiresAuth: true, tabbar: false, navVisible: false, menuId: 'settings', permissionKey: 'settings.monitoring.view' },
  { id: 'settings-ai', path: '/settings/ai', title: 'AI 设置', description: 'AI 提供商配置', icon: 'IconSetting', group: 'settings', requiresAuth: true, tabbar: false, navVisible: false, parentId: 'settings', menuId: 'settings', permissionKey: 'settings.ai.view' },

  { id: 'login', path: '/login', title: '登录', description: '用户登录', icon: 'IconLock', group: 'auth', requiresAuth: false, tabbar: false, navVisible: false },
  { id: 'oidc-callback', path: '/auth/oidc/callback', title: 'OIDC Callback', description: 'OIDC 回调', icon: 'IconLock', group: 'auth', requiresAuth: false, tabbar: false, navVisible: false },
  { id: 'login-callback', path: '/login/callback', title: 'Login Callback', description: '登录回调', icon: 'IconLock', group: 'auth', requiresAuth: false, tabbar: false, navVisible: false },
]

export function getRouteMeta(pathname: string): RouteMeta {
  const candidates = [...routeMeta].sort((a, b) => b.path.length - a.path.length)
  return (
    candidates.find((r) => {
      if (r.path === '/') return pathname === '/'
      const routeSegments = r.path.split('/').filter(Boolean)
      const pathSegments = pathname.split('/').filter(Boolean)
      if (routeSegments.length > pathSegments.length) return false
      return routeSegments.every((segment, index) => segment.startsWith(':') || segment === pathSegments[index])
    }) ?? routeMeta[0]
  )
}

export function getParentRouteMeta(route: RouteMeta): RouteMeta | null {
  return route.parentId ? routeMeta.find((r) => r.id === route.parentId) ?? null : null
}

export function resolveRoutePermission(route: RouteMeta): string | undefined {
  if (route.permissionKey) {
    return route.permissionKey
  }
  const parent = getParentRouteMeta(route)
  return parent ? resolveRoutePermission(parent) : undefined
}

export function resolveRouteMenuId(route: RouteMeta): string | undefined {
  if (route.menuId) {
    return route.menuId
  }
  const parent = getParentRouteMeta(route)
  return parent ? resolveRouteMenuId(parent) : undefined
}

export function canAccessRoute(route: RouteMeta, snapshot?: PermissionSnapshot | null): boolean {
  if (!route.requiresAuth) {
    return true
  }
  if (!snapshot) {
    return false
  }
  if (route.permissionStrategy === 'any-child') {
    return routeMeta
      .filter((child) => child.parentId === route.id)
      .some((child) => canAccessRoute(child, snapshot))
  }
  const permissionKey = resolveRoutePermission(route)
  const menuId = resolveRouteMenuId(route)
  const hasPermission = !permissionKey || snapshot.permissionKeys.includes(permissionKey)
  const hasMenu = !menuId || snapshot.visibleMenuIds.includes(menuId)
  return hasPermission && hasMenu
}

function getSectionOrder(section?: string) {
  const index = MENU_SECTION_ORDER.indexOf(String(section || '').trim() as (typeof MENU_SECTION_ORDER)[number])
  return index >= 0 ? index : Number.MAX_SAFE_INTEGER
}

function sortRuntimeMenuTree(items: RuntimeMenuNode[]): RuntimeMenuNode[] {
  return [...items]
    .sort((left, right) => {
      const sectionCompare = getSectionOrder(left.section) - getSectionOrder(right.section)
      if (sectionCompare !== 0) return sectionCompare
      if (left.sortOrder !== right.sortOrder) return left.sortOrder - right.sortOrder
      return left.path.localeCompare(right.path)
    })
    .map((item): RuntimeMenuNode => ({
      ...item,
      children: item.children && item.children.length > 0 ? sortRuntimeMenuTree(item.children) : undefined,
    }))
}

function findBestRouteForMenu(menu: VisibleMenu, snapshot?: PermissionSnapshot | null): RouteMeta | undefined {
  const candidates = routeMeta.filter((route) => {
    const routeMenuId = resolveRouteMenuId(route)
    return routeMenuId === menu.id || route.path === menu.path
  }).filter((route) => canAccessRoute(route, snapshot))

  if (candidates.length === 0) return undefined

  return candidates.sort((left, right) => {
    const leftExactPath = left.path === menu.path ? 0 : 1
    const rightExactPath = right.path === menu.path ? 0 : 1
    if (leftExactPath !== rightExactPath) return leftExactPath - rightExactPath

    const leftNavVisible = left.navVisible ? 0 : 1
    const rightNavVisible = right.navVisible ? 0 : 1
    if (leftNavVisible !== rightNavVisible) return leftNavVisible - rightNavVisible

    const leftRedirect = left.redirectTo ? 1 : 0
    const rightRedirect = right.redirectTo ? 1 : 0
    if (leftRedirect !== rightRedirect) return leftRedirect - rightRedirect

    return left.path.localeCompare(right.path)
  })[0]
}

function buildRuntimeMenuTree(snapshot?: PermissionSnapshot | null): RuntimeMenuNode[] {
  const visibleMenus = snapshot?.visibleMenus ?? []
  const nodes = new Map<string, RuntimeMenuNode>()

  visibleMenus.forEach((menu) => {
    nodes.set(menu.id, {
      id: menu.id,
      parentId: menu.parentId,
      path: menu.path,
      labelZh: menu.labelZh || menu.id,
      labelEn: menu.labelEn || menu.labelZh || menu.id,
      iconKey: menu.iconKey || '',
      section: menu.section || 'control',
      sortOrder: typeof menu.sortOrder === 'number' ? menu.sortOrder : 0,
      enabled: menu.enabled ?? true,
      route: findBestRouteForMenu(menu, snapshot),
      children: [],
    })
  })

  const roots: RuntimeMenuNode[] = []
  nodes.forEach((node) => {
    if (node.parentId && nodes.has(node.parentId)) {
      nodes.get(node.parentId)?.children?.push(node)
      return
    }
    roots.push(node)
  })

  const prune = (node: RuntimeMenuNode): RuntimeMenuNode | null => {
    const nextChildren = (node.children ?? []).map(prune).filter((item): item is RuntimeMenuNode => Boolean(item))
    const keepAsContainer = nextChildren.length > 0
    const keepAsLeaf = Boolean(node.route)
    if (!keepAsContainer && !keepAsLeaf) {
      return null
    }
    return {
      ...node,
      children: nextChildren.length > 0 ? nextChildren : undefined,
    }
  }

  return sortRuntimeMenuTree(
    roots
      .map(prune)
      .filter((item): item is RuntimeMenuNode => Boolean(item)),
  )
}

export function getAccessibleSidebarNav(snapshot?: PermissionSnapshot | null): RuntimeMenuNode[] {
  return buildRuntimeMenuTree(snapshot)
}

export function findFirstAccessiblePath(snapshot?: PermissionSnapshot | null): string | null {
  const firstRoute = routeMeta.find((route) => route.requiresAuth && route.navVisible && canAccessRoute(route, snapshot))
  if (!firstRoute) {
    return null
  }
  return firstRoute.redirectTo ?? firstRoute.path
}
