import {
  getMenuSectionOrder,
  normalizeMenuSection,
} from "@/features/system/menu-schema";
import type {
  BusinessWorkspaceType,
  PermissionSnapshot,
  RouteMeta,
  RuntimeMenuNode,
  VisibleMenu,
  WorkspaceType,
} from "@/types";

const WORKSPACE_PERMISSION_KEYS: Record<BusinessWorkspaceType, string> = {
  application: "workspace.application.view",
  resource: "workspace.resource.view",
};

const DEFAULT_WORKSPACE_PATHS: Record<BusinessWorkspaceType, string> = {
  application: "/applications",
  resource: "/",
};

const RESOURCE_DEFAULT_ROLES = new Set(["admin", "ops", "readonly", "auditor"]);
const APPLICATION_PATH_PREFIXES = [
  "/applications",
  "/application-management",
  "/business-lines",
  "/delivery-environments",
  "/application-environments",
  "/build-templates",
  "/delivery/blueprints",
  "/delivery/release-bundles",
  "/delivery/execution-tasks",
  "/delivery/approval-policies",
  "/workflow-templates",
  "/release-board",
  "/workflows",
  "/releases",
  "/registries",
];

const WORKBENCH_DEFAULT_PATHS = {
  platform: "/",
  virtualization: "/virtualization",
  docker: "/docker",
  delivery: "/applications",
  ai: "/ai-workbench",
  aiGateway: "/ai-gateway/overview",
  monitoring: "/monitoring-workbench",
} as const;

export type WorkbenchId = keyof typeof WORKBENCH_DEFAULT_PATHS;

function matchesRoutePrefix(pathname: string, prefixes: string[]) {
  return prefixes.some((prefix) => pathname.startsWith(prefix));
}

export const routeMeta: RouteMeta[] = [
  {
    id: "overview",
    path: "/",
    title: "k8s工作台",
    description: "平台总览",
    icon: "IconDesktop",
    group: "overview",
    workbenchId: "platform",
    requiresAuth: true,
    tabbar: true,
    navVisible: true,
    menuId: "dashboard",
    permissionKey: "overview.view",
    scopeMode: "cluster",
    workspace: "resource",
  },

  {
    id: "cluster-resources-nodes",
    path: "/cluster-resources/nodes",
    title: "节点",
    description: "节点管理",
    icon: "IconServer",
    group: "platform",
    requiresAuth: true,
    tabbar: true,
    navVisible: true,
    menuId: "cluster-resources-nodes",
    permissionKey: "platform.nodes.view",
    scopeMode: "cluster",
    workspace: "resource",
  },
  {
    id: "cluster-resources-node-detail",
    path: "/cluster-resources/nodes/:nodeName",
    title: "节点详情",
    description: "节点详情",
    icon: "IconServer",
    group: "platform",
    requiresAuth: true,
    tabbar: false,
    navVisible: false,
    parentId: "cluster-resources-nodes",
    scopeMode: "cluster",
  },

  {
    id: "workloads",
    path: "/workloads",
    title: "工作负载",
    description: "工作负载管理",
    icon: "IconGridView",
    group: "platform",
    requiresAuth: true,
    tabbar: false,
    navVisible: true,
    redirectTo: "/workloads/overview",
    menuId: "workloads",
    permissionKey: "platform.workloads.view",
    scopeMode: "namespace",
    workspace: "resource",
  },
  {
    id: "workloads-overview",
    path: "/workloads/overview",
    title: "Overview",
    description: "资源概览与事件",
    icon: "IconGridView",
    group: "platform",
    requiresAuth: true,
    tabbar: true,
    navVisible: true,
    parentId: "workloads",
    scopeMode: "namespace",
  },
  {
    id: "workloads-deployments",
    path: "/workloads/deployments",
    title: "Deployments",
    description: "部署管理",
    icon: "IconGridView",
    group: "platform",
    requiresAuth: true,
    tabbar: true,
    navVisible: true,
    parentId: "workloads",
    scopeMode: "namespace",
  },
  {
    id: "workloads-pods",
    path: "/workloads/pods",
    title: "Pods",
    description: "Pod 管理",
    icon: "IconGridView",
    group: "platform",
    requiresAuth: true,
    tabbar: true,
    navVisible: true,
    parentId: "workloads",
    scopeMode: "namespace",
  },
  {
    id: "workloads-statefulsets",
    path: "/workloads/statefulsets",
    title: "StatefulSets",
    description: "有状态副本集",
    icon: "IconGridView",
    group: "platform",
    requiresAuth: true,
    tabbar: true,
    navVisible: true,
    parentId: "workloads",
    scopeMode: "namespace",
  },
  {
    id: "workloads-daemonsets",
    path: "/workloads/daemonsets",
    title: "DaemonSets",
    description: "守护进程集",
    icon: "IconGridView",
    group: "platform",
    requiresAuth: true,
    tabbar: true,
    navVisible: true,
    parentId: "workloads",
    scopeMode: "namespace",
  },
  {
    id: "workloads-jobs",
    path: "/workloads/jobs",
    title: "Jobs",
    description: "批量作业",
    icon: "IconGridView",
    group: "platform",
    requiresAuth: true,
    tabbar: true,
    navVisible: true,
    parentId: "workloads",
    scopeMode: "namespace",
  },
  {
    id: "workloads-cronjobs",
    path: "/workloads/cronjobs",
    title: "CronJobs",
    description: "定时任务",
    icon: "IconGridView",
    group: "platform",
    requiresAuth: true,
    tabbar: true,
    navVisible: true,
    parentId: "workloads",
    scopeMode: "namespace",
  },
  {
    id: "workloads-replicasets",
    path: "/workloads/replicasets",
    title: "ReplicaSets",
    description: "副本集",
    icon: "IconGridView",
    group: "platform",
    requiresAuth: true,
    tabbar: true,
    navVisible: true,
    parentId: "workloads",
    scopeMode: "namespace",
  },
  {
    id: "workloads-replicationcontrollers",
    path: "/workloads/replicationcontrollers",
    title: "ReplicationControllers",
    description: "复制控制器",
    icon: "IconGridView",
    group: "platform",
    requiresAuth: true,
    tabbar: true,
    navVisible: true,
    parentId: "workloads",
    scopeMode: "namespace",
  },

  {
    id: "configuration",
    path: "/configuration",
    title: "Configuration",
    description: "平台配置资源",
    icon: "IconSetting",
    group: "platform",
    requiresAuth: true,
    tabbar: false,
    navVisible: true,
    redirectTo: "/configuration/configmaps",
    menuId: "configuration",
    permissionKey: "platform.configuration.view",
    scopeMode: "namespace",
    workspace: "resource",
  },
  {
    id: "configuration-configmaps",
    path: "/configuration/configmaps",
    title: "ConfigMaps",
    description: "配置映射",
    icon: "IconSetting",
    group: "platform",
    requiresAuth: true,
    tabbar: true,
    navVisible: true,
    parentId: "configuration",
    scopeMode: "namespace",
  },
  {
    id: "configuration-secrets",
    path: "/configuration/secrets",
    title: "Secrets",
    description: "密钥对象",
    icon: "IconSetting",
    group: "platform",
    requiresAuth: true,
    tabbar: true,
    navVisible: true,
    parentId: "configuration",
    scopeMode: "namespace",
  },
  {
    id: "configuration-resourcequotas",
    path: "/configuration/resourcequotas",
    title: "ResourceQuotas",
    description: "资源配额",
    icon: "IconSetting",
    group: "platform",
    requiresAuth: true,
    tabbar: true,
    navVisible: true,
    parentId: "configuration",
    scopeMode: "namespace",
  },
  {
    id: "configuration-limitranges",
    path: "/configuration/limitranges",
    title: "LimitRanges",
    description: "资源限制范围",
    icon: "IconSetting",
    group: "platform",
    requiresAuth: true,
    tabbar: true,
    navVisible: true,
    parentId: "configuration",
    scopeMode: "namespace",
  },
  {
    id: "configuration-hpas",
    path: "/configuration/hpas",
    title: "HorizontalPodAutoscalers",
    description: "自动扩缩容",
    icon: "IconSetting",
    group: "platform",
    requiresAuth: true,
    tabbar: true,
    navVisible: true,
    parentId: "configuration",
    scopeMode: "namespace",
  },
  {
    id: "configuration-poddisruptionbudgets",
    path: "/configuration/poddisruptionbudgets",
    title: "PodDisruptionBudgets",
    description: "驱逐预算",
    icon: "IconSetting",
    group: "platform",
    requiresAuth: true,
    tabbar: true,
    navVisible: true,
    parentId: "configuration",
    scopeMode: "namespace",
  },
  {
    id: "configuration-priorityclasses",
    path: "/configuration/priorityclasses",
    title: "PriorityClasses",
    description: "优先级类",
    icon: "IconSetting",
    group: "platform",
    requiresAuth: true,
    tabbar: true,
    navVisible: true,
    parentId: "configuration",
    scopeMode: "cluster",
  },
  {
    id: "configuration-runtimeclasses",
    path: "/configuration/runtimeclasses",
    title: "RuntimeClasses",
    description: "运行时类",
    icon: "IconSetting",
    group: "platform",
    requiresAuth: true,
    tabbar: true,
    navVisible: true,
    parentId: "configuration",
    scopeMode: "cluster",
  },
  {
    id: "configuration-leases",
    path: "/configuration/leases",
    title: "Leases",
    description: "租约",
    icon: "IconSetting",
    group: "platform",
    requiresAuth: true,
    tabbar: true,
    navVisible: true,
    parentId: "configuration",
    scopeMode: "namespace",
  },
  {
    id: "configuration-mutatingwebhookconfigurations",
    path: "/configuration/mutatingwebhookconfigurations",
    title: "MutatingWebhookConfigurations",
    description: "变更 Webhook",
    icon: "IconSetting",
    group: "platform",
    requiresAuth: true,
    tabbar: true,
    navVisible: true,
    parentId: "configuration",
    scopeMode: "cluster",
  },
  {
    id: "configuration-validatingwebhookconfigurations",
    path: "/configuration/validatingwebhookconfigurations",
    title: "ValidatingWebhookConfigurations",
    description: "校验 Webhook",
    icon: "IconSetting",
    group: "platform",
    requiresAuth: true,
    tabbar: true,
    navVisible: true,
    parentId: "configuration",
    scopeMode: "cluster",
  },

  {
    id: "network",
    path: "/network",
    title: "网络",
    description: "网络资源",
    icon: "IconConnection",
    group: "platform",
    requiresAuth: true,
    tabbar: false,
    navVisible: true,
    redirectTo: "/network/topology",
    menuId: "network",
    permissionKey: "platform.network.view",
    scopeMode: "namespace",
    workspace: "resource",
  },
  {
    id: "network-topology",
    path: "/network/topology",
    title: "网络拓扑",
    description: "入口、路由、Service 与后端的网络拓扑",
    icon: "IconConnection",
    group: "platform",
    requiresAuth: true,
    tabbar: true,
    navVisible: true,
    parentId: "network",
    scopeMode: "namespace",
  },
  {
    id: "network-services",
    path: "/network/services",
    title: "Services",
    description: "服务管理",
    icon: "IconConnection",
    group: "platform",
    requiresAuth: true,
    tabbar: true,
    navVisible: true,
    parentId: "network",
    scopeMode: "namespace",
  },
  {
    id: "network-service-detail",
    path: "/network/services/:serviceName",
    title: "Service Detail",
    description: "服务详情",
    icon: "IconConnection",
    group: "platform",
    requiresAuth: true,
    tabbar: false,
    navVisible: false,
    parentId: "network-services",
    scopeMode: "namespace",
  },
  {
    id: "network-ingresses",
    path: "/network/ingresses",
    title: "Ingresses",
    description: "入口管理",
    icon: "IconConnection",
    group: "platform",
    requiresAuth: true,
    tabbar: true,
    navVisible: true,
    parentId: "network",
    scopeMode: "namespace",
  },
  {
    id: "network-gateway-api",
    path: "/network/gateway-api",
    title: "Gateway API",
    description: "Gateway API 资源",
    icon: "IconConnection",
    group: "platform",
    requiresAuth: true,
    tabbar: false,
    navVisible: true,
    parentId: "network",
    redirectTo: "/network/gateway-api/gatewayclasses",
    scopeMode: "namespace",
  },
  {
    id: "network-gateway-api-gatewayclasses",
    path: "/network/gateway-api/gatewayclasses",
    title: "GatewayClasses",
    description: "Gateway API 控制器类",
    icon: "IconConnection",
    group: "platform",
    requiresAuth: true,
    tabbar: true,
    navVisible: true,
    parentId: "network-gateway-api",
    scopeMode: "cluster",
  },
  {
    id: "network-gateway-api-gateways",
    path: "/network/gateway-api/gateways",
    title: "Gateways",
    description: "Gateway API 网关",
    icon: "IconConnection",
    group: "platform",
    requiresAuth: true,
    tabbar: true,
    navVisible: true,
    parentId: "network-gateway-api",
    scopeMode: "namespace",
  },
  {
    id: "network-gateway-api-httproutes",
    path: "/network/gateway-api/httproutes",
    title: "HTTPRoutes",
    description: "Gateway API HTTP 路由",
    icon: "IconConnection",
    group: "platform",
    requiresAuth: true,
    tabbar: true,
    navVisible: true,
    parentId: "network-gateway-api",
    scopeMode: "namespace",
  },
  {
    id: "network-gateway-api-backendtlspolicies",
    path: "/network/gateway-api/backendtlspolicies",
    title: "BackendTLSPolicies",
    description: "Gateway API 后端 TLS 策略",
    icon: "IconConnection",
    group: "platform",
    requiresAuth: true,
    tabbar: true,
    navVisible: true,
    parentId: "network-gateway-api",
    scopeMode: "namespace",
  },
  {
    id: "network-gateway-api-grpcroutes",
    path: "/network/gateway-api/grpcroutes",
    title: "GRPCRoutes",
    description: "Gateway API gRPC 路由",
    icon: "IconConnection",
    group: "platform",
    requiresAuth: true,
    tabbar: true,
    navVisible: true,
    parentId: "network-gateway-api",
    scopeMode: "namespace",
  },
  {
    id: "network-gateway-api-referencegrants",
    path: "/network/gateway-api/referencegrants",
    title: "ReferenceGrants",
    description: "Gateway API 跨命名空间引用授权",
    icon: "IconConnection",
    group: "platform",
    requiresAuth: true,
    tabbar: true,
    navVisible: true,
    parentId: "network-gateway-api",
    scopeMode: "namespace",
  },
  {
    id: "network-endpointslices",
    path: "/network/endpointslices",
    title: "EndpointSlices",
    description: "服务后端切片",
    icon: "IconConnection",
    group: "platform",
    requiresAuth: true,
    tabbar: true,
    navVisible: true,
    parentId: "network",
    scopeMode: "namespace",
  },
  {
    id: "network-ingressclasses",
    path: "/network/ingressclasses",
    title: "IngressClasses",
    description: "Ingress 类",
    icon: "IconConnection",
    group: "platform",
    requiresAuth: true,
    tabbar: true,
    navVisible: true,
    parentId: "network",
    scopeMode: "cluster",
  },
  {
    id: "network-networkpolicies",
    path: "/network/networkpolicies",
    title: "NetworkPolicies",
    description: "网络策略",
    icon: "IconConnection",
    group: "platform",
    requiresAuth: true,
    tabbar: true,
    navVisible: true,
    parentId: "network",
    scopeMode: "namespace",
  },
  {
    id: "network-port-forward",
    path: "/network/port-forward",
    title: "Port Forward",
    description: "端口转发",
    icon: "IconConnection",
    group: "platform",
    requiresAuth: true,
    tabbar: true,
    navVisible: true,
    parentId: "network",
    scopeMode: "namespace",
  },

  {
    id: "storage",
    path: "/storage",
    title: "存储",
    description: "存储资源",
    icon: "IconServer",
    group: "platform",
    requiresAuth: true,
    tabbar: false,
    navVisible: true,
    redirectTo: "/storage/persistentvolumeclaims",
    menuId: "storage",
    permissionKey: "platform.storage.view",
    scopeMode: "namespace",
    workspace: "resource",
  },
  {
    id: "storage-pvc",
    path: "/storage/persistentvolumeclaims",
    title: "PVC",
    description: "持久卷声明",
    icon: "IconServer",
    group: "platform",
    requiresAuth: true,
    tabbar: true,
    navVisible: true,
    parentId: "storage",
    scopeMode: "namespace",
  },
  {
    id: "storage-pv",
    path: "/storage/persistentvolumes",
    title: "PV",
    description: "持久卷",
    icon: "IconServer",
    group: "platform",
    requiresAuth: true,
    tabbar: true,
    navVisible: true,
    parentId: "storage",
    scopeMode: "cluster",
  },
  {
    id: "storage-classes",
    path: "/storage/storageclasses",
    title: "StorageClasses",
    description: "存储类",
    icon: "IconServer",
    group: "platform",
    requiresAuth: true,
    tabbar: true,
    navVisible: true,
    parentId: "storage",
    scopeMode: "cluster",
  },

  {
    id: "platform-access-control",
    path: "/platform-access-control",
    title: "RBAC",
    description: "Kubernetes RBAC 资源",
    icon: "IconShield",
    group: "platform",
    requiresAuth: true,
    tabbar: false,
    navVisible: true,
    redirectTo: "/platform-access-control/serviceaccounts",
    menuId: "platform-access-control",
    scopeMode: "namespace",
    workspace: "resource",
  },
  {
    id: "platform-access-control-serviceaccounts",
    path: "/platform-access-control/serviceaccounts",
    title: "ServiceAccounts",
    description: "服务账户",
    icon: "IconShield",
    group: "platform",
    requiresAuth: true,
    tabbar: true,
    navVisible: true,
    parentId: "platform-access-control",
    scopeMode: "namespace",
  },
  {
    id: "platform-access-control-serviceaccount-detail",
    path: "/platform-access-control/serviceaccounts/:name",
    title: "ServiceAccount Detail",
    description: "服务账户详情",
    icon: "IconShield",
    group: "platform",
    requiresAuth: true,
    tabbar: false,
    navVisible: false,
    parentId: "platform-access-control-serviceaccounts",
    menuId: "platform-access-control",
    scopeMode: "namespace",
  },
  {
    id: "platform-access-control-clusterroles",
    path: "/platform-access-control/clusterroles",
    title: "ClusterRoles",
    description: "集群角色",
    icon: "IconShield",
    group: "platform",
    requiresAuth: true,
    tabbar: true,
    navVisible: true,
    parentId: "platform-access-control",
    scopeMode: "cluster",
  },
  {
    id: "platform-access-control-clusterrole-detail",
    path: "/platform-access-control/clusterroles/:name",
    title: "ClusterRole Detail",
    description: "集群角色详情",
    icon: "IconShield",
    group: "platform",
    requiresAuth: true,
    tabbar: false,
    navVisible: false,
    parentId: "platform-access-control-clusterroles",
    menuId: "platform-access-control",
    scopeMode: "cluster",
  },
  {
    id: "platform-access-control-roles",
    path: "/platform-access-control/roles",
    title: "Roles",
    description: "命名空间角色",
    icon: "IconShield",
    group: "platform",
    requiresAuth: true,
    tabbar: true,
    navVisible: true,
    parentId: "platform-access-control",
    scopeMode: "namespace",
  },
  {
    id: "platform-access-control-role-detail",
    path: "/platform-access-control/roles/:name",
    title: "Role Detail",
    description: "命名空间角色详情",
    icon: "IconShield",
    group: "platform",
    requiresAuth: true,
    tabbar: false,
    navVisible: false,
    parentId: "platform-access-control-roles",
    menuId: "platform-access-control",
    scopeMode: "namespace",
  },
  {
    id: "platform-access-control-clusterrolebindings",
    path: "/platform-access-control/clusterrolebindings",
    title: "ClusterRoleBindings",
    description: "集群角色绑定",
    icon: "IconShield",
    group: "platform",
    requiresAuth: true,
    tabbar: true,
    navVisible: true,
    parentId: "platform-access-control",
    scopeMode: "cluster",
  },
  {
    id: "platform-access-control-clusterrolebinding-detail",
    path: "/platform-access-control/clusterrolebindings/:name",
    title: "ClusterRoleBinding Detail",
    description: "集群角色绑定详情",
    icon: "IconShield",
    group: "platform",
    requiresAuth: true,
    tabbar: false,
    navVisible: false,
    parentId: "platform-access-control-clusterrolebindings",
    menuId: "platform-access-control",
    scopeMode: "cluster",
  },
  {
    id: "platform-access-control-rolebindings",
    path: "/platform-access-control/rolebindings",
    title: "RoleBindings",
    description: "命名空间角色绑定",
    icon: "IconShield",
    group: "platform",
    requiresAuth: true,
    tabbar: true,
    navVisible: true,
    parentId: "platform-access-control",
    scopeMode: "namespace",
  },
  {
    id: "platform-access-control-rolebinding-detail",
    path: "/platform-access-control/rolebindings/:name",
    title: "RoleBinding Detail",
    description: "命名空间角色绑定详情",
    icon: "IconShield",
    group: "platform",
    requiresAuth: true,
    tabbar: false,
    navVisible: false,
    parentId: "platform-access-control-rolebindings",
    menuId: "platform-access-control",
    scopeMode: "namespace",
  },

  {
    id: "helm",
    path: "/helm",
    title: "Helm",
    description: "Helm 管理",
    icon: "IconPuzzle",
    group: "platform",
    requiresAuth: true,
    tabbar: false,
    navVisible: true,
    redirectTo: "/helm/releases",
    menuId: "helm",
    permissionKey: "platform.helm.view",
    scopeMode: "namespace",
    workspace: "resource",
  },
  {
    id: "helm-releases",
    path: "/helm/releases",
    title: "Helm Releases",
    description: "Helm 发布",
    icon: "IconPuzzle",
    group: "platform",
    requiresAuth: true,
    tabbar: true,
    navVisible: true,
    parentId: "helm",
    scopeMode: "namespace",
  },
  {
    id: "helm-release-detail",
    path: "/helm/releases/:releaseName",
    title: "Helm Release Detail",
    description: "Helm 发布详情",
    icon: "IconPuzzle",
    group: "platform",
    requiresAuth: true,
    tabbar: false,
    navVisible: false,
    parentId: "helm-releases",
    scopeMode: "namespace",
  },
  {
    id: "helm-charts",
    path: "/helm/charts",
    title: "Helm Charts",
    description: "Helm 图表",
    icon: "IconPuzzle",
    group: "platform",
    requiresAuth: true,
    tabbar: true,
    navVisible: true,
    parentId: "helm",
    scopeMode: "cluster",
  },

  {
    id: "extensions",
    path: "/extensions",
    title: "CRD",
    description: "CRD 管理",
    icon: "IconPuzzle",
    group: "platform",
    requiresAuth: true,
    tabbar: true,
    navVisible: true,
    menuId: "extensions",
    permissionKey: "platform.extensions.view",
    scopeMode: "cluster",
    workspace: "resource",
  },
  {
    id: "extensions-group-detail",
    path: "/extensions/apis/:groupName",
    title: "CRD API Detail",
    description: "CRD API 详情",
    icon: "IconPuzzle",
    group: "platform",
    requiresAuth: true,
    tabbar: false,
    navVisible: false,
    parentId: "extensions",
    scopeMode: "cluster",
  },
  {
    id: "cluster-resources-namespaces",
    path: "/cluster-resources/namespaces",
    title: "命名空间",
    description: "命名空间管理",
    icon: "IconServer",
    group: "platform",
    requiresAuth: true,
    tabbar: true,
    navVisible: true,
    menuId: "cluster-resources-namespaces",
    permissionKey: "platform.namespaces.view",
    scopeMode: "cluster",
    workspace: "resource",
  },
  {
    id: "clusters",
    path: "/clusters",
    title: "集群管理",
    description: "集群生命周期管理",
    icon: "IconGlobe",
    group: "platform",
    requiresAuth: true,
    tabbar: true,
    navVisible: true,
    menuId: "clusters",
    permissionKey: "platform.clusters.view",
    scopeMode: "passive",
    workspace: "resource",
  },
  {
    id: "cluster-detail",
    path: "/clusters/:clusterId",
    title: "集群详情",
    description: "集群详情",
    icon: "IconGlobe",
    group: "platform",
    requiresAuth: true,
    tabbar: false,
    navVisible: false,
    parentId: "clusters",
    scopeMode: "passive",
  },

  {
    id: "applications",
    path: "/applications",
    title: "应用中心",
    description: "应用入口视角",
    icon: "IconAppCenter",
    group: "delivery",
    workbenchId: "delivery",
    requiresAuth: true,
    tabbar: true,
    navVisible: true,
    menuId: "builds",
    permissionKey: "delivery.applications.view",
    scopeMode: "passive",
    workspace: "application",
  },
  {
    id: "application-management",
    path: "/application-management",
    title: "应用管理",
    description: "应用配置与发布管理",
    icon: "IconAppCenter",
    group: "delivery",
    requiresAuth: true,
    tabbar: true,
    navVisible: true,
    menuId: "application-management",
    permissionKey: "delivery.applications.view",
    scopeMode: "passive",
    workspace: "application",
  },
  {
    id: "application-management-detail",
    path: "/application-management/:applicationId",
    title: "应用管理详情",
    description: "应用配置详情",
    icon: "IconAppCenter",
    group: "delivery",
    requiresAuth: true,
    tabbar: false,
    navVisible: false,
    parentId: "application-management",
    scopeMode: "passive",
  },
  {
    id: "application-detail",
    path: "/applications/:applicationId",
    title: "应用详情",
    description: "应用运行详情",
    icon: "IconAppCenter",
    group: "delivery",
    requiresAuth: true,
    tabbar: false,
    navVisible: false,
    parentId: "applications",
    scopeMode: "passive",
  },
  {
    id: "application-workload-detail",
    path: "/applications/:applicationId/application-environments/:applicationEnvironmentId/workloads/:workloadName",
    title: "服务详情",
    description: "应用服务运行详情",
    icon: "IconAppCenter",
    group: "delivery",
    requiresAuth: true,
    tabbar: false,
    navVisible: false,
    parentId: "application-detail",
    scopeMode: "passive",
  },
  {
    id: "business-lines",
    path: "/business-lines",
    title: "业务线管理",
    description: "业务线主数据",
    icon: "IconAppCenter",
    group: "catalog",
    requiresAuth: true,
    tabbar: true,
    navVisible: true,
    permissionKey: "delivery.business-lines.view",
    scopeMode: "passive",
    workspace: "application",
  },
  {
    id: "delivery-environments",
    path: "/delivery-environments",
    title: "环境管理",
    description: "交付环境主数据",
    icon: "IconAppCenter",
    group: "catalog",
    requiresAuth: true,
    tabbar: true,
    navVisible: true,
    permissionKey: "delivery.environments.view",
    scopeMode: "passive",
    workspace: "application",
  },
  {
    id: "application-environments",
    path: "/application-environments",
    title: "应用环境绑定",
    description: "应用与环境绑定",
    icon: "IconAppCenter",
    group: "delivery",
    requiresAuth: true,
    tabbar: true,
    navVisible: true,
    permissionKey: "delivery.application-environments.view",
    scopeMode: "passive",
    workspace: "application",
  },
  {
    id: "application-environment-detail",
    path: "/application-environments/:applicationEnvironmentId",
    title: "环境详情",
    description: "应用环境详情",
    icon: "IconAppCenter",
    group: "delivery",
    requiresAuth: true,
    tabbar: false,
    navVisible: false,
    parentId: "application-environments",
    scopeMode: "passive",
  },
  {
    id: "build-templates",
    path: "/build-templates",
    title: "构建模板",
    description: "平台构建模板",
    icon: "IconCode",
    group: "delivery",
    requiresAuth: true,
    tabbar: true,
    navVisible: true,
    permissionKey: "delivery.build-templates.view",
    scopeMode: "passive",
    workspace: "application",
  },
  {
    id: "delivery-blueprints",
    path: "/delivery/blueprints",
    title: "交付蓝图",
    description: "应用接入模板、规范渲染与平台编排入口",
    icon: "IconCode",
    group: "delivery",
    workbenchId: "delivery",
    requiresAuth: true,
    tabbar: true,
    navVisible: true,
    menuId: "delivery-blueprints",
    permissionKey: "delivery.applications.view",
    scopeMode: "passive",
    workspace: "application",
  },
  {
    id: "release-bundles",
    path: "/delivery/release-bundles",
    title: "版本包",
    description: "不可变交付版本包",
    icon: "IconInbox",
    group: "delivery",
    requiresAuth: true,
    tabbar: true,
    navVisible: true,
    permissionKey: "delivery.release-bundles.view",
    scopeMode: "passive",
    workspace: "application",
  },
  {
    id: "execution-tasks",
    path: "/delivery/execution-tasks",
    title: "执行任务",
    description: "执行平面任务与日志",
    icon: "IconFlow",
    group: "delivery",
    requiresAuth: true,
    tabbar: true,
    navVisible: true,
    permissionKey: "delivery.execution-tasks.view",
    scopeMode: "passive",
    workspace: "application",
  },
  {
    id: "approval-policies",
    path: "/delivery/approval-policies",
    title: "审批策略",
    description: "交付审批策略中心",
    icon: "IconShield",
    group: "delivery",
    requiresAuth: true,
    tabbar: true,
    navVisible: true,
    permissionKey: "delivery.approval-policies.view",
    scopeMode: "passive",
    workspace: "application",
  },
  {
    id: "workflow-templates",
    path: "/workflow-templates",
    title: "发布流程模板",
    description: "交付发布流程模板",
    icon: "IconFlow",
    group: "delivery",
    requiresAuth: true,
    tabbar: true,
    navVisible: true,
    permissionKey: "delivery.workflow-templates.view",
    scopeMode: "passive",
    workspace: "application",
  },
  {
    id: "release-board",
    path: "/release-board",
    title: "发布看板",
    description: "应用环境发布矩阵",
    icon: "IconSend",
    group: "delivery",
    requiresAuth: true,
    tabbar: true,
    navVisible: true,
    permissionKey: "delivery.release-board.view",
    scopeMode: "passive",
    workspace: "application",
  },
  {
    id: "workflows",
    path: "/workflows",
    title: "工作流",
    description: "工作流管理",
    icon: "IconFlow",
    group: "delivery",
    requiresAuth: true,
    tabbar: true,
    navVisible: true,
    menuId: "workflows",
    permissionKey: "delivery.workflows.view",
    scopeMode: "passive",
    workspace: "application",
  },
  {
    id: "releases",
    path: "/releases",
    title: "发布管理",
    description: "发布编排",
    icon: "IconSend",
    group: "delivery",
    requiresAuth: true,
    tabbar: true,
    navVisible: true,
    menuId: "releases",
    permissionKey: "delivery.releases.view",
    scopeMode: "passive",
    workspace: "application",
  },
  {
    id: "registries",
    path: "/registries",
    title: "镜像仓库",
    description: "镜像仓库连接",
    icon: "IconInbox",
    group: "delivery",
    requiresAuth: true,
    tabbar: true,
    navVisible: true,
    menuId: "registries",
    permissionKey: "delivery.registries.view",
    scopeMode: "passive",
    workspace: "application",
  },

  {
    id: "virtualization-workbench",
    path: "/virtualization",
    title: "虚拟化管理工作台",
    description: "虚拟化资源总览、虚拟机、集群、镜像、规格与操作记录",
    icon: "IconServer",
    group: "virtualization",
    workbenchId: "virtualization",
    requiresAuth: true,
    tabbar: false,
    navVisible: true,
    redirectTo: "/virtualization/overview",
    menuId: "virtualization-workbench",
    permissionStrategy: "any-child",
    scopeMode: "passive",
    workspace: "resource",
  },
  {
    id: "virtualization-workbench-overview",
    path: "/virtualization/overview",
    title: "总览",
    description: "虚拟化资源接入状态与后续目标",
    icon: "IconServer",
    group: "virtualization",
    workbenchId: "virtualization",
    requiresAuth: true,
    tabbar: true,
    navVisible: true,
    parentId: "virtualization-workbench",
    menuId: "virtualization-workbench-overview",
    permissionKey: "virtualization.overview.view",
    scopeMode: "passive",
  },
  {
    id: "virtualization-workbench-vms",
    path: "/virtualization/vms",
    title: "虚拟机",
    description: "虚拟机实例入口",
    icon: "IconServer",
    group: "virtualization",
    workbenchId: "virtualization",
    requiresAuth: true,
    tabbar: true,
    navVisible: true,
    parentId: "virtualization-workbench",
    menuId: "virtualization-workbench-vms",
    permissionKey: "virtualization.vms.view",
    scopeMode: "passive",
  },
  {
    id: "virtualization-workbench-vm-detail",
    path: "/virtualization/vms/:id",
    title: "虚拟机详情",
    description: "虚拟机规格、镜像、网络和任务详情",
    icon: "IconServer",
    group: "virtualization",
    workbenchId: "virtualization",
    requiresAuth: true,
    tabbar: false,
    navVisible: false,
    parentId: "virtualization-workbench-vms",
    permissionKey: "virtualization.vms.view",
    scopeMode: "passive",
  },
  {
    id: "virtualization-workbench-clusters",
    path: "/virtualization/clusters",
    title: "集群",
    description: "虚拟化连接与集群入口",
    icon: "IconServer",
    group: "virtualization",
    workbenchId: "virtualization",
    requiresAuth: true,
    tabbar: true,
    navVisible: true,
    parentId: "virtualization-workbench",
    menuId: "virtualization-workbench-clusters",
    permissionKey: "virtualization.clusters.view",
    scopeMode: "passive",
  },
  {
    id: "virtualization-workbench-images",
    path: "/virtualization/images",
    title: "镜像",
    description: "虚拟机镜像入口",
    icon: "IconInbox",
    group: "virtualization",
    workbenchId: "virtualization",
    requiresAuth: true,
    tabbar: true,
    navVisible: true,
    parentId: "virtualization-workbench",
    menuId: "virtualization-workbench-images",
    permissionKey: "virtualization.images.view",
    scopeMode: "passive",
  },
  {
    id: "virtualization-workbench-flavors",
    path: "/virtualization/flavors",
    title: "规格",
    description: "虚拟机规格入口",
    icon: "IconGridView",
    group: "virtualization",
    workbenchId: "virtualization",
    requiresAuth: true,
    tabbar: true,
    navVisible: true,
    parentId: "virtualization-workbench",
    menuId: "virtualization-workbench-flavors",
    permissionKey: "virtualization.flavors.view",
    scopeMode: "passive",
  },
  {
    id: "virtualization-workbench-operations",
    path: "/virtualization/operations",
    title: "操作记录",
    description: "虚拟化操作记录入口",
    icon: "IconFileSearch",
    group: "virtualization",
    workbenchId: "virtualization",
    requiresAuth: true,
    tabbar: true,
    navVisible: true,
    parentId: "virtualization-workbench",
    menuId: "virtualization-workbench-operations",
    permissionKey: "virtualization.operations.view",
    scopeMode: "passive",
  },
  {
    id: "virtualization-workbench-sync",
    path: "/virtualization/sync",
    title: "同步任务",
    description: "虚拟化资产同步任务",
    icon: "IconFileSearch",
    group: "virtualization",
    workbenchId: "virtualization",
    requiresAuth: true,
    tabbar: true,
    navVisible: true,
    parentId: "virtualization-workbench",
    menuId: "virtualization-workbench-sync",
    permissionKey: "virtualization.sync.view",
    scopeMode: "passive",
  },

  {
    id: "docker-workbench",
    path: "/docker",
    title: "Docker 工作台",
    description: "Docker 主机、Compose 项目、容器服务、端口映射、模板与操作记录",
    icon: "IconServer",
    group: "docker",
    workbenchId: "docker",
    requiresAuth: true,
    tabbar: false,
    navVisible: true,
    redirectTo: "/docker/overview",
    menuId: "docker-workbench",
    permissionStrategy: "any-child",
    scopeMode: "passive",
    workspace: "resource",
  },
  {
    id: "docker-workbench-overview",
    path: "/docker/overview",
    title: "总览",
    description: "Docker 主机、Compose 与端口暴露概览",
    icon: "IconServer",
    group: "docker",
    workbenchId: "docker",
    requiresAuth: true,
    tabbar: true,
    navVisible: true,
    parentId: "docker-workbench",
    menuId: "docker-workbench-overview",
    permissionKey: "docker.overview.view",
    scopeMode: "passive",
  },
  {
    id: "docker-workbench-hosts",
    path: "/docker/hosts",
    title: "Docker 主机",
    description: "Docker 主机接入与 PVE 快速构建",
    icon: "IconServer",
    group: "docker",
    workbenchId: "docker",
    requiresAuth: true,
    tabbar: true,
    navVisible: true,
    parentId: "docker-workbench",
    menuId: "docker-workbench-hosts",
    permissionKey: "docker.hosts.view",
    scopeMode: "passive",
  },
  {
    id: "docker-workbench-projects",
    path: "/docker/projects",
    title: "Compose 项目",
    description: "Docker Compose 项目与部署动作",
    icon: "IconGridView",
    group: "docker",
    workbenchId: "docker",
    requiresAuth: true,
    tabbar: true,
    navVisible: true,
    parentId: "docker-workbench",
    menuId: "docker-workbench-projects",
    permissionKey: "docker.projects.view",
    scopeMode: "passive",
  },
  {
    id: "docker-workbench-services",
    path: "/docker/services",
    title: "容器服务",
    description: "Compose 服务与容器运行态",
    icon: "IconGridView",
    group: "docker",
    workbenchId: "docker",
    requiresAuth: true,
    tabbar: true,
    navVisible: true,
    parentId: "docker-workbench",
    menuId: "docker-workbench-services",
    permissionKey: "docker.services.view",
    scopeMode: "passive",
  },
  {
    id: "docker-workbench-ports",
    path: "/docker/ports",
    title: "端口映射",
    description: "Docker 主机端口池、访问地址与映射冲突校验",
    icon: "IconShare",
    group: "docker",
    workbenchId: "docker",
    requiresAuth: true,
    tabbar: true,
    navVisible: true,
    parentId: "docker-workbench",
    menuId: "docker-workbench-ports",
    permissionKey: "docker.ports.view",
    scopeMode: "passive",
  },
  {
    id: "docker-workbench-templates",
    path: "/docker/templates",
    title: "模板",
    description: "Compose 模板与环境变量模板",
    icon: "IconCode",
    group: "docker",
    workbenchId: "docker",
    requiresAuth: true,
    tabbar: true,
    navVisible: true,
    parentId: "docker-workbench",
    menuId: "docker-workbench-templates",
    permissionKey: "docker.templates.view",
    scopeMode: "passive",
  },
  {
    id: "docker-workbench-operations",
    path: "/docker/operations",
    title: "操作记录",
    description: "Docker 主机构建、Compose 与服务操作记录",
    icon: "IconFileSearch",
    group: "docker",
    workbenchId: "docker",
    requiresAuth: true,
    tabbar: true,
    navVisible: true,
    parentId: "docker-workbench",
    menuId: "docker-workbench-operations",
    permissionKey: "docker.operations.view",
    scopeMode: "passive",
  },

  {
    id: "monitoring-workbench",
    path: "/monitoring-workbench",
    title: "监控工作台",
    description: "监控、告警、通知和值班协同",
    icon: "IconAlertTriangle",
    group: "observe",
    workbenchId: "monitoring",
    requiresAuth: true,
    tabbar: false,
    navVisible: true,
    redirectTo: "/monitoring-workbench/overview",
    menuId: "monitoring-workbench",
    permissionKey: "observe.monitoring.view",
    scopeMode: "passive",
    workspace: "resource",
  },
  {
    id: "monitoring-workbench-overview",
    path: "/monitoring-workbench/overview",
    title: "总览",
    description: "监控总览",
    icon: "IconPulse",
    group: "observe",
    workbenchId: "monitoring",
    requiresAuth: true,
    tabbar: true,
    navVisible: true,
    parentId: "monitoring-workbench",
    menuId: "monitoring-workbench-overview",
    permissionKey: "observe.monitoring.view",
    scopeMode: "passive",
  },
  {
    id: "monitoring-workbench-rules",
    path: "/monitoring-workbench/rules",
    title: "告警规则",
    description: "告警规则与数据源选择",
    icon: "IconFileSearch",
    group: "observe",
    workbenchId: "monitoring",
    requiresAuth: true,
    tabbar: true,
    navVisible: true,
    parentId: "monitoring-workbench",
    menuId: "monitoring-workbench-rules",
    permissionKey: "observe.alert-rules.view",
    scopeMode: "passive",
  },
  {
    id: "monitoring-workbench-alerts",
    path: "/monitoring-workbench/alerts",
    title: "活跃告警",
    description: "当前告警处理面板",
    icon: "IconAlertTriangle",
    group: "observe",
    workbenchId: "monitoring",
    requiresAuth: true,
    tabbar: true,
    navVisible: true,
    parentId: "monitoring-workbench",
    menuId: "monitoring-workbench-alerts",
    permissionKey: "observe.alerts.view",
    scopeMode: "passive",
  },
  {
    id: "alert-event-detail",
    path: "/monitoring-workbench/alerts/:eventId",
    title: "告警事件详情",
    description: "单条告警事件详情",
    icon: "IconAlertTriangle",
    group: "observe",
    workbenchId: "monitoring",
    requiresAuth: true,
    tabbar: false,
    navVisible: false,
    parentId: "monitoring-workbench-alerts",
    permissionKey: "observe.alerts.view",
    scopeMode: "passive",
  },
  {
    id: "monitoring-workbench-notifications",
    path: "/monitoring-workbench/notifications",
    title: "通知策略",
    description: "通知渠道与路由策略",
    icon: "IconBell",
    group: "observe",
    workbenchId: "monitoring",
    requiresAuth: true,
    tabbar: true,
    navVisible: true,
    parentId: "monitoring-workbench",
    menuId: "monitoring-workbench-notifications",
    permissionKey: "observe.notifications.view",
    scopeMode: "passive",
  },
  {
    id: "monitoring-workbench-healing",
    path: "/monitoring-workbench/healing",
    title: "自愈中心",
    description: "告警自愈策略与执行审批",
    icon: "IconTool",
    group: "observe",
    workbenchId: "monitoring",
    requiresAuth: true,
    tabbar: true,
    navVisible: true,
    parentId: "monitoring-workbench",
    menuId: "monitoring-workbench-healing",
    permissionKey: "observe.healing.view",
    scopeMode: "passive",
  },
  {
    id: "monitoring-workbench-oncall",
    path: "/monitoring-workbench/oncall",
    title: "值班协同",
    description: "值班轮换与升级联动",
    icon: "IconUserCircle",
    group: "observe",
    workbenchId: "monitoring",
    requiresAuth: true,
    tabbar: true,
    navVisible: true,
    parentId: "monitoring-workbench",
    menuId: "monitoring-workbench-oncall",
    permissionKey: "observe.oncall.view",
    scopeMode: "passive",
  },
  {
    id: "monitoring-workbench-oncall-settings",
    path: "/monitoring-workbench/oncall/settings",
    title: "值班设置",
    description: "排班、轮值、升级链与 IRM 路由",
    icon: "IconSettings",
    group: "observe",
    workbenchId: "monitoring",
    requiresAuth: true,
    tabbar: true,
    navVisible: false,
    parentId: "monitoring-workbench-oncall",
    menuId: "monitoring-workbench-oncall",
    permissionKey: "observe.oncall.manage",
    scopeMode: "passive",
  },
  {
    id: "monitoring-workbench-events",
    path: "/monitoring-workbench/events",
    title: "事件流",
    description: "事件时间线与上下文",
    icon: "IconBell",
    group: "observe",
    workbenchId: "monitoring",
    requiresAuth: true,
    tabbar: true,
    navVisible: true,
    parentId: "monitoring-workbench",
    menuId: "monitoring-workbench-events",
    permissionKey: "observe.events.view",
    scopeMode: "passive",
  },
  {
    id: "observability-compat",
    path: "/observability",
    title: "告警中心",
    description: "兼容旧入口",
    icon: "IconAlertTriangle",
    group: "observe",
    requiresAuth: true,
    tabbar: false,
    navVisible: false,
    redirectTo: "/monitoring-workbench",
    permissionKey: "observe.monitoring.view",
    scopeMode: "passive",
    workspace: "resource",
  },
  {
    id: "observability-monitoring-compat",
    path: "/observability/monitoring",
    title: "旧概览入口",
    description: "兼容旧入口",
    icon: "IconPulse",
    group: "observe",
    requiresAuth: true,
    tabbar: false,
    navVisible: false,
    parentId: "monitoring-workbench",
    permissionKey: "observe.monitoring.view",
    scopeMode: "passive",
  },
  {
    id: "observability-rules-compat",
    path: "/observability/rules",
    title: "旧规则入口",
    description: "兼容旧入口",
    icon: "IconFileSearch",
    group: "observe",
    requiresAuth: true,
    tabbar: false,
    navVisible: false,
    parentId: "monitoring-workbench",
    permissionKey: "observe.alert-rules.view",
    scopeMode: "passive",
  },
  {
    id: "observability-alerts-compat",
    path: "/observability/alerts",
    title: "旧告警入口",
    description: "兼容旧入口",
    icon: "IconAlertTriangle",
    group: "observe",
    requiresAuth: true,
    tabbar: false,
    navVisible: false,
    parentId: "monitoring-workbench",
    permissionKey: "observe.alerts.view",
    scopeMode: "passive",
  },
  {
    id: "observability-notifications-compat",
    path: "/observability/notifications",
    title: "旧通知入口",
    description: "兼容旧入口",
    icon: "IconBell",
    group: "observe",
    requiresAuth: true,
    tabbar: false,
    navVisible: false,
    parentId: "monitoring-workbench",
    permissionKey: "observe.notifications.view",
    scopeMode: "passive",
  },
  {
    id: "observability-healing-compat",
    path: "/observability/healing",
    title: "旧自愈入口",
    description: "兼容旧入口",
    icon: "IconTool",
    group: "observe",
    requiresAuth: true,
    tabbar: false,
    navVisible: false,
    parentId: "monitoring-workbench",
    permissionKey: "observe.healing.view",
    scopeMode: "passive",
  },
  {
    id: "observability-oncall-compat",
    path: "/observability/oncall",
    title: "旧值班入口",
    description: "兼容旧入口",
    icon: "IconUserCircle",
    group: "observe",
    requiresAuth: true,
    tabbar: false,
    navVisible: false,
    parentId: "monitoring-workbench",
    permissionKey: "observe.oncall.view",
    scopeMode: "passive",
  },
  {
    id: "observability-events-compat",
    path: "/observability/events",
    title: "旧事件入口",
    description: "兼容旧入口",
    icon: "IconBell",
    group: "observe",
    requiresAuth: true,
    tabbar: false,
    navVisible: false,
    parentId: "monitoring-workbench",
    permissionKey: "observe.events.view",
    scopeMode: "passive",
  },

  {
    id: "ai-workbench",
    path: "/ai-workbench",
    title: "AI工作台",
    description: "AI 对话、分析与巡检入口",
    icon: "IconComment",
    group: "observe",
    workbenchId: "ai",
    requiresAuth: true,
    tabbar: false,
    navVisible: true,
    redirectTo: "/ai-workbench/chat",
    menuId: "ai-workbench",
    permissionKey: "observe.ai.view",
    scopeMode: "passive",
    workspace: "resource",
  },
  {
    id: "ai-workbench-chat",
    path: "/ai-workbench/chat",
    title: "通用聊天",
    description: "通用会话与问答排障",
    icon: "IconComment",
    group: "observe",
    workbenchId: "ai",
    requiresAuth: true,
    tabbar: true,
    navVisible: true,
    parentId: "ai-workbench",
    menuId: "ai-workbench-chat",
    permissionKey: "observe.ai.chat",
    scopeMode: "passive",
  },
  {
    id: "ai-workbench-root-cause",
    path: "/ai-workbench/root-cause",
    title: "根因分析",
    description: "围绕告警、事件和异常做根因分析",
    icon: "IconComment",
    group: "observe",
    workbenchId: "ai",
    requiresAuth: true,
    tabbar: true,
    navVisible: true,
    parentId: "ai-workbench",
    menuId: "ai-workbench-chat",
    permissionKey: "observe.ai.chat",
    scopeMode: "passive",
  },
  {
    id: "ai-workbench-performance",
    path: "/ai-workbench/performance",
    title: "性能分析",
    description: "聚焦容量、时延和吞吐分析",
    icon: "IconComment",
    group: "observe",
    workbenchId: "ai",
    requiresAuth: true,
    tabbar: true,
    navVisible: true,
    parentId: "ai-workbench",
    menuId: "ai-workbench-chat",
    permissionKey: "observe.ai.chat",
    scopeMode: "passive",
  },
  {
    id: "ai-workbench-inspection",
    path: "/ai-workbench/inspection",
    title: "巡检",
    description: "巡检任务、运行记录与自动化策略",
    icon: "IconComment",
    group: "observe",
    workbenchId: "ai",
    requiresAuth: true,
    tabbar: true,
    navVisible: true,
    parentId: "ai-workbench",
    menuId: "ai-workbench-inspection",
    permissionKey: "observe.ai.view",
    scopeMode: "passive",
  },
  {
    id: "ai-workbench-tool-settings",
    path: "/ai-workbench/tool-settings",
    title: "工具与技能",
    description: "查看并配置工具、技能与数据源",
    icon: "IconComment",
    group: "observe",
    workbenchId: "ai",
    requiresAuth: true,
    tabbar: true,
    navVisible: true,
    parentId: "ai-workbench",
    menuId: "ai-workbench-tool-settings",
    permissionKey: "observe.ai.view",
    scopeMode: "passive",
  },
  {
    id: "ai-workbench-model-settings",
    path: "/ai-workbench/model-settings",
    title: "AI 设置",
    description: "查看并调整 AI Provider、数据源、技能与自动化策略",
    icon: "IconComment",
    group: "observe",
    workbenchId: "ai",
    requiresAuth: true,
    tabbar: true,
    navVisible: true,
    parentId: "ai-workbench",
    menuId: "ai-workbench-model-settings",
    permissionKey: "settings.ai.view",
    scopeMode: "passive",
  },
  {
    id: "ai-gateway",
    path: "/ai-gateway",
    title: "AI Gateway",
    description: "AI Gateway 独立工作台",
    icon: "IconShield",
    group: "ai-gateway",
    workbenchId: "aiGateway",
    requiresAuth: true,
    tabbar: false,
    navVisible: true,
    redirectTo: "/ai-gateway/overview",
    menuId: "ai-gateway",
    permissionStrategy: "any-child",
    scopeMode: "passive",
    workspace: "resource",
  },
  {
    id: "ai-gateway-overview",
    path: "/ai-gateway/overview",
    title: "概览",
    description: "AI Gateway 能力、身份与治理摘要",
    icon: "IconShield",
    group: "ai-gateway",
    workbenchId: "aiGateway",
    requiresAuth: true,
    tabbar: true,
    navVisible: true,
    parentId: "ai-gateway",
    menuId: "ai-gateway-overview",
    permissionKey: "ai.gateway.view",
    scopeMode: "passive",
  },
  {
    id: "ai-gateway-manifest",
    path: "/ai-gateway/manifest",
    title: "能力清单",
    description: "查看当前身份可见的 MCP tools、resources、prompts 和 skills",
    icon: "IconShield",
    group: "ai-gateway",
    workbenchId: "aiGateway",
    requiresAuth: true,
    tabbar: true,
    navVisible: true,
    parentId: "ai-gateway",
    menuId: "ai-gateway-manifest",
    permissionKey: "ai.gateway.view",
    scopeMode: "passive",
  },
  {
    id: "ai-gateway-clients",
    path: "/ai-gateway/clients",
    title: "AI Clients",
    description: "管理外部 AI 客户端注册入口",
    icon: "IconShield",
    group: "ai-gateway",
    workbenchId: "aiGateway",
    requiresAuth: true,
    tabbar: true,
    navVisible: true,
    parentId: "ai-gateway",
    menuId: "ai-gateway-clients",
    permissionKey: "ai.gateway.manage",
    scopeMode: "passive",
  },
  {
    id: "ai-gateway-tokens",
    path: "/ai-gateway/tokens",
    title: "Tokens",
    description: "管理 personal access tokens 与服务账号 token",
    icon: "IconShield",
    group: "ai-gateway",
    workbenchId: "aiGateway",
    requiresAuth: true,
    tabbar: true,
    navVisible: true,
    parentId: "ai-gateway",
    menuId: "ai-gateway-tokens",
    permissionKeysAny: [
      "ai.gateway.view",
      "ai.gateway.invoke",
      "ai.gateway.manage",
    ],
    scopeMode: "passive",
  },
  {
    id: "ai-gateway-governance",
    path: "/ai-gateway/governance",
    title: "Governance",
    description: "管理 Gateway 授权、策略与审批治理",
    icon: "IconShield",
    group: "ai-gateway",
    workbenchId: "aiGateway",
    requiresAuth: true,
    tabbar: true,
    navVisible: true,
    parentId: "ai-gateway",
    menuId: "ai-gateway-governance",
    permissionKey: "ai.gateway.manage",
    scopeMode: "passive",
  },
  {
    id: "ai-gateway-call-logs",
    path: "/ai-gateway/call-logs",
    title: "调用日志",
    description: "查看 AI Gateway 调用者、调用内容与结果",
    icon: "IconShield",
    group: "ai-gateway",
    workbenchId: "aiGateway",
    requiresAuth: true,
    tabbar: true,
    navVisible: true,
    parentId: "ai-gateway",
    menuId: "ai-gateway-call-logs",
    permissionKey: "ai.gateway.manage",
    scopeMode: "passive",
  },
  {
    id: "ai-workbench-gateway-compat",
    path: "/ai-workbench/gateway",
    title: "AI Gateway",
    description: "兼容旧入口，跳转到独立 AI Gateway 工作台",
    icon: "IconShield",
    group: "ai-gateway",
    workbenchId: "aiGateway",
    requiresAuth: true,
    tabbar: false,
    navVisible: false,
    redirectTo: "/ai-gateway/overview",
    menuId: "ai-gateway",
    permissionKeysAny: [
      "ai.gateway.view",
      "ai.gateway.invoke",
      "ai.gateway.manage",
    ],
    scopeMode: "passive",
    workspace: "resource",
  },
  {
    id: "ai-workbench-tools",
    path: "/ai-workbench/tools",
    title: "工具与技能",
    description: "兼容旧入口，跳转到工具与技能设置",
    icon: "IconComment",
    group: "observe",
    workbenchId: "ai",
    requiresAuth: true,
    tabbar: false,
    navVisible: false,
    parentId: "ai-workbench",
    menuId: "ai-workbench-tool-settings",
    permissionKey: "observe.ai.view",
    scopeMode: "passive",
  },
  {
    id: "ai-workbench-investigation",
    path: "/ai-workbench/investigation",
    title: "调查工作台",
    description: "兼容旧入口，跳转到通用聊天",
    icon: "IconComment",
    group: "observe",
    requiresAuth: true,
    tabbar: false,
    navVisible: false,
    parentId: "ai-workbench",
    permissionKey: "observe.ai.chat",
    scopeMode: "passive",
  },
  {
    id: "ai-observe-compat",
    path: "/ai-observe",
    title: "Ai可观测性",
    description: "兼容旧入口",
    icon: "IconComment",
    group: "observe",
    requiresAuth: true,
    tabbar: false,
    navVisible: false,
    redirectTo: "/ai-workbench",
    permissionKey: "observe.ai.view",
    scopeMode: "passive",
    workspace: "resource",
  },
  {
    id: "ai-observe-workbench-compat",
    path: "/ai-observe/workbench",
    title: "旧调查入口",
    description: "兼容旧入口",
    icon: "IconComment",
    group: "observe",
    requiresAuth: true,
    tabbar: false,
    navVisible: false,
    parentId: "ai-workbench",
    permissionKey: "observe.ai.chat",
    scopeMode: "passive",
  },
  {
    id: "ai-observe-operations-compat",
    path: "/ai-observe/operations",
    title: "旧巡检入口",
    description: "兼容旧入口",
    icon: "IconComment",
    group: "observe",
    requiresAuth: true,
    tabbar: false,
    navVisible: false,
    parentId: "ai-workbench",
    permissionKey: "observe.ai.view",
    scopeMode: "passive",
  },
  {
    id: "ai-observe-tools-compat",
    path: "/ai-observe/tools",
    title: "旧工具入口",
    description: "兼容旧入口",
    icon: "IconComment",
    group: "observe",
    requiresAuth: true,
    tabbar: false,
    navVisible: false,
    parentId: "ai-workbench",
    permissionKey: "observe.ai.view",
    scopeMode: "passive",
  },
  {
    id: "ai-root-cause",
    path: "/ai-observe/root-cause",
    title: "链路根因分析",
    description: "兼容旧入口，跳转到工作台根因模式",
    icon: "IconComment",
    group: "observe",
    requiresAuth: true,
    tabbar: false,
    navVisible: false,
    parentId: "ai-workbench",
    permissionKey: "observe.ai.view",
    scopeMode: "passive",
  },
  {
    id: "ai-performance",
    path: "/ai-observe/performance",
    title: "性能分析",
    description: "兼容旧入口，跳转到工作台性能模式",
    icon: "IconComment",
    group: "observe",
    requiresAuth: true,
    tabbar: false,
    navVisible: false,
    parentId: "ai-workbench",
    permissionKey: "observe.ai.view",
    scopeMode: "passive",
  },
  {
    id: "ai-chat",
    path: "/ai-observe/chat",
    title: "AI Chat",
    description: "兼容旧入口，跳转到调查工作台",
    icon: "IconComment",
    group: "observe",
    requiresAuth: true,
    tabbar: false,
    navVisible: false,
    parentId: "ai-workbench",
    permissionKey: "observe.ai.chat",
    scopeMode: "passive",
  },
  {
    id: "ai-inspection",
    path: "/ai-observe/inspection",
    title: "智能巡检",
    description: "兼容旧入口，跳转到巡检与自动化",
    icon: "IconComment",
    group: "observe",
    requiresAuth: true,
    tabbar: false,
    navVisible: false,
    parentId: "ai-workbench",
    permissionKey: "observe.ai.view",
    scopeMode: "passive",
  },
  {
    id: "chat",
    path: "/chat",
    title: "AI Chat",
    description: "兼容旧入口",
    icon: "IconComment",
    group: "observe",
    requiresAuth: true,
    tabbar: false,
    navVisible: false,
    parentId: "ai-workbench",
    permissionKey: "observe.ai.chat",
    scopeMode: "passive",
  },

  {
    id: "access",
    path: "/access",
    title: "访问控制",
    description: "身份、角色、用户组与策略",
    icon: "IconShield",
    group: "access",
    requiresAuth: true,
    tabbar: false,
    navVisible: false,
    menuId: "access",
    permissionKey: "access.users.view",
    permissionStrategy: "any-child",
    scopeMode: "passive",
    workspace: "system",
  },
  {
    id: "access-users",
    path: "/access/users",
    title: "用户",
    description: "用户管理",
    icon: "IconUser",
    group: "access",
    requiresAuth: true,
    tabbar: true,
    navVisible: true,
    parentId: "access",
    menuId: "access",
    permissionKey: "access.users.view",
    scopeMode: "passive",
  },
  {
    id: "access-roles",
    path: "/access/roles",
    title: "角色",
    description: "角色管理",
    icon: "IconUserCircle",
    group: "access",
    requiresAuth: true,
    tabbar: true,
    navVisible: true,
    parentId: "access",
    menuId: "access",
    permissionKey: "access.roles.view",
    scopeMode: "passive",
  },
  {
    id: "access-teams",
    path: "/access/teams",
    title: "用户组",
    description: "用户组管理",
    icon: "IconUserGroup",
    group: "access",
    requiresAuth: true,
    tabbar: true,
    navVisible: true,
    parentId: "access",
    menuId: "access",
    permissionKey: "access.groups.view",
    scopeMode: "passive",
  },
  {
    id: "access-policies",
    path: "/access/policies",
    title: "策略",
    description: "策略管理",
    icon: "IconShield",
    group: "access",
    requiresAuth: true,
    tabbar: true,
    navVisible: true,
    parentId: "access",
    menuId: "access",
    permissionKey: "access.policies.view",
    scopeMode: "passive",
  },
  {
    id: "access-scope-grants",
    path: "/access/scope-grants",
    title: "授权范围",
    description: "业务线环境应用授权",
    icon: "IconShield",
    group: "access",
    requiresAuth: true,
    tabbar: false,
    navVisible: false,
    permissionKey: "access.scope-grants.view",
    scopeMode: "passive",
  },

  {
    id: "system",
    path: "/system",
    title: "系统管理",
    description: "公告、菜单、审计与操作记录",
    icon: "IconSetting",
    group: "system",
    requiresAuth: true,
    tabbar: false,
    navVisible: false,
    redirectTo: "/system/online-users",
    scopeMode: "passive",
    workspace: "system",
  },
  {
    id: "system-online-users",
    path: "/system/online-users",
    title: "在线用户",
    description: "在线用户监控",
    icon: "IconUser",
    group: "system",
    requiresAuth: true,
    tabbar: true,
    navVisible: true,
    menuId: "system-online-users",
    permissionKey: "system.online-users.view",
    scopeMode: "passive",
  },
  {
    id: "system-announcements",
    path: "/system/announcements",
    title: "公告",
    description: "公告管理",
    icon: "IconBell",
    group: "system",
    requiresAuth: true,
    tabbar: true,
    navVisible: true,
    menuId: "announcements",
    permissionKey: "system.announcements.view",
    scopeMode: "passive",
  },
  {
    id: "system-menus",
    path: "/system/menus",
    title: "菜单",
    description: "菜单管理",
    icon: "IconMenu",
    group: "system",
    requiresAuth: true,
    tabbar: true,
    navVisible: true,
    menuId: "menus",
    permissionKey: "system.menus.view",
    scopeMode: "passive",
  },
  {
    id: "audit",
    path: "/system/audit",
    title: "审计日志",
    description: "审计记录",
    icon: "IconFile",
    group: "system",
    requiresAuth: true,
    tabbar: true,
    navVisible: true,
    menuId: "audit",
    permissionKey: "system.audit.view",
    scopeMode: "passive",
  },
  {
    id: "operations",
    path: "/system/operations",
    title: "操作日志",
    description: "操作记录",
    icon: "IconList",
    group: "system",
    requiresAuth: true,
    tabbar: true,
    navVisible: true,
    menuId: "operations",
    permissionKey: "system.operations.view",
    scopeMode: "passive",
  },

  {
    id: "settings",
    path: "/settings",
    title: "设置中心",
    description: "登陆与品牌配置",
    icon: "IconSetting",
    group: "settings",
    requiresAuth: true,
    tabbar: false,
    navVisible: false,
    redirectTo: "/settings/login",
    menuId: "settings",
    permissionStrategy: "any-child",
    scopeMode: "passive",
    workspace: "system",
  },
  {
    id: "settings-login",
    path: "/settings/login",
    title: "登陆设置",
    description: "OIDC、OAuth2 与 SAML 登录配置",
    icon: "IconSetting",
    group: "settings",
    requiresAuth: true,
    tabbar: true,
    navVisible: true,
    parentId: "settings",
    menuId: "settings-login",
    permissionKey: "settings.identity.view",
    scopeMode: "passive",
  },
  {
    id: "settings-identity",
    path: "/settings/identity",
    title: "身份设置",
    description: "兼容旧入口，跳转到登陆设置",
    icon: "IconSetting",
    group: "settings",
    requiresAuth: true,
    tabbar: false,
    navVisible: false,
    parentId: "settings",
    menuId: "settings-login",
    permissionKey: "settings.identity.view",
    scopeMode: "passive",
  },
  {
    id: "settings-branding",
    path: "/settings/branding",
    title: "品牌设置",
    description: "品牌 Logo 与标题配置",
    icon: "IconSetting",
    group: "settings",
    requiresAuth: true,
    tabbar: true,
    navVisible: true,
    parentId: "settings",
    menuId: "settings-branding",
    permissionKey: "settings.branding.view",
    scopeMode: "passive",
  },
  {
    id: "settings-monitoring",
    path: "/settings/monitoring",
    title: "监控设置",
    description: "Prometheus 配置",
    icon: "IconSetting",
    group: "settings",
    requiresAuth: true,
    tabbar: false,
    navVisible: false,
    menuId: "settings",
    permissionKey: "settings.monitoring.view",
    scopeMode: "passive",
  },
  {
    id: "settings-ai",
    path: "/settings/ai",
    title: "AI 设置",
    description: "兼容旧入口，跳转到 AI 工作台设置页",
    icon: "IconSetting",
    group: "settings",
    requiresAuth: true,
    tabbar: false,
    navVisible: false,
    parentId: "settings",
    menuId: "settings",
    permissionKey: "settings.ai.view",
    scopeMode: "passive",
  },

  {
    id: "login",
    path: "/login",
    title: "登录",
    description: "用户登录",
    icon: "IconLock",
    group: "auth",
    requiresAuth: false,
    tabbar: false,
    navVisible: false,
    scopeMode: "hidden",
  },
  {
    id: "oidc-callback",
    path: "/auth/oidc/callback",
    title: "OIDC Callback",
    description: "OIDC 回调",
    icon: "IconLock",
    group: "auth",
    requiresAuth: false,
    tabbar: false,
    navVisible: false,
    scopeMode: "hidden",
  },
  {
    id: "login-callback",
    path: "/login/callback",
    title: "Login Callback",
    description: "登录回调",
    icon: "IconLock",
    group: "auth",
    requiresAuth: false,
    tabbar: false,
    navVisible: false,
    scopeMode: "hidden",
  },
];

export function getRouteMeta(pathname: string): RouteMeta {
  const candidates = [...routeMeta].sort(
    (a, b) => b.path.length - a.path.length,
  );
  return (
    candidates.find((r) => {
      if (r.path === "/") return pathname === "/";
      const routeSegments = r.path.split("/").filter(Boolean);
      const pathSegments = pathname.split("/").filter(Boolean);
      if (routeSegments.length > pathSegments.length) return false;
      return routeSegments.every(
        (segment, index) =>
          segment.startsWith(":") || segment === pathSegments[index],
      );
    }) ?? routeMeta[0]
  );
}

export function getParentRouteMeta(route: RouteMeta): RouteMeta | null {
  return route.parentId
    ? (routeMeta.find((r) => r.id === route.parentId) ?? null)
    : null;
}

export function resolveRoutePermission(route: RouteMeta): string | undefined {
  if (route.permissionKey) {
    return route.permissionKey;
  }
  const parent = getParentRouteMeta(route);
  return parent ? resolveRoutePermission(parent) : undefined;
}

export function resolveRouteMenuId(route: RouteMeta): string | undefined {
  if (route.menuId) {
    return route.menuId;
  }
  const parent = getParentRouteMeta(route);
  return parent ? resolveRouteMenuId(parent) : undefined;
}

function deriveWorkspaceFromPath(
  pathname: string,
  requiresAuth: boolean,
): WorkspaceType | null {
  if (
    pathname === "/login" ||
    pathname.startsWith("/auth/") ||
    pathname.startsWith("/login/")
  ) {
    return null;
  }
  if (
    pathname.startsWith("/access") ||
    pathname.startsWith("/system") ||
    pathname.startsWith("/settings")
  ) {
    return "system";
  }
  if (matchesRoutePrefix(pathname, APPLICATION_PATH_PREFIXES)) {
    return "application";
  }
  return requiresAuth ? "resource" : null;
}

export function getRouteWorkspace(route: RouteMeta): WorkspaceType | null {
  if (route.workspace) {
    return route.workspace;
  }
  const parent = getParentRouteMeta(route);
  if (parent) {
    return getRouteWorkspace(parent);
  }
  return deriveWorkspaceFromPath(route.path, route.requiresAuth);
}

function deriveWorkbenchIdFromPath(pathname: string): WorkbenchId | null {
  if (
    pathname === "/" ||
    pathname.startsWith("/cluster-resources") ||
    pathname.startsWith("/workloads") ||
    pathname.startsWith("/configuration") ||
    pathname.startsWith("/network") ||
    pathname.startsWith("/storage") ||
    pathname.startsWith("/platform-access-control") ||
    pathname.startsWith("/helm") ||
    pathname.startsWith("/extensions") ||
    pathname.startsWith("/clusters")
  ) {
    return "platform";
  }
  if (matchesRoutePrefix(pathname, APPLICATION_PATH_PREFIXES)) {
    return "delivery";
  }
  if (pathname.startsWith("/virtualization")) {
    return "virtualization";
  }
  if (pathname.startsWith("/docker")) {
    return "docker";
  }
  if (pathname.startsWith("/ai-gateway")) {
    return "aiGateway";
  }
  if (
    pathname.startsWith("/ai-workbench") ||
    pathname.startsWith("/ai-observe") ||
    pathname.startsWith("/chat")
  ) {
    return "ai";
  }
  if (
    pathname.startsWith("/monitoring-workbench") ||
    pathname.startsWith("/observability")
  ) {
    return "monitoring";
  }
  return null;
}

function rankMenuRouteCandidates(candidates: RouteMeta[], menuPath: string) {
  return [...candidates].sort((left, right) => {
    const leftExactPath = left.path === menuPath ? 0 : 1;
    const rightExactPath = right.path === menuPath ? 0 : 1;
    if (leftExactPath !== rightExactPath) return leftExactPath - rightExactPath;

    const leftNavVisible = left.navVisible ? 0 : 1;
    const rightNavVisible = right.navVisible ? 0 : 1;
    if (leftNavVisible !== rightNavVisible)
      return leftNavVisible - rightNavVisible;

    const leftRedirect = left.redirectTo ? 1 : 0;
    const rightRedirect = right.redirectTo ? 1 : 0;
    if (leftRedirect !== rightRedirect) return leftRedirect - rightRedirect;

    return left.path.localeCompare(right.path);
  });
}

function findBestRouteForMenuMeta(menu: Pick<VisibleMenu, "id" | "path">) {
  const candidates = routeMeta.filter((route) => {
    const routeMenuId = resolveRouteMenuId(route);
    return routeMenuId === menu.id || route.path === menu.path;
  });
  if (candidates.length === 0) {
    return undefined;
  }
  return rankMenuRouteCandidates(candidates, menu.path)[0];
}

export function getMenuWorkspace(
  menu: Pick<VisibleMenu, "id" | "path">,
): WorkspaceType | null {
  const route = findBestRouteForMenuMeta(menu);
  if (route) {
    return getRouteWorkspace(route);
  }
  return deriveWorkspaceFromPath(menu.path, true);
}

export function getMenuWorkbenchId(
  menu: Pick<VisibleMenu, "id" | "path">,
): WorkbenchId | null {
  const route = findBestRouteForMenuMeta(menu);
  if (route) {
    return getRouteWorkbenchId(route);
  }
  return deriveWorkbenchIdFromPath(menu.path);
}

export function getRouteWorkbenchId(route: RouteMeta): WorkbenchId | null {
  if (route.workbenchId && route.workbenchId in WORKBENCH_DEFAULT_PATHS) {
    return route.workbenchId as WorkbenchId;
  }
  const parent = getParentRouteMeta(route);
  if (parent) {
    return getRouteWorkbenchId(parent);
  }
  return deriveWorkbenchIdFromPath(route.path);
}

export function getRouteScopeMode(
  route: RouteMeta,
): NonNullable<RouteMeta["scopeMode"]> {
  if (route.scopeMode) {
    return route.scopeMode;
  }
  const parent = getParentRouteMeta(route);
  if (parent) {
    return getRouteScopeMode(parent);
  }
  const pathname = route.path;
  if (
    pathname === "/login" ||
    pathname.startsWith("/auth/") ||
    pathname.startsWith("/login/")
  ) {
    return "hidden";
  }
  if (
    pathname === "/" ||
    pathname === "/clusters" ||
    pathname.startsWith("/cluster-resources/nodes") ||
    pathname.startsWith("/cluster-resources/namespaces") ||
    pathname.startsWith("/extensions") ||
    pathname.startsWith("/platform-access-control/clusterroles") ||
    pathname.startsWith("/platform-access-control/clusterrolebindings") ||
    pathname.startsWith("/storage/persistentvolumes") ||
    pathname.startsWith("/storage/storageclasses")
  ) {
    return "cluster";
  }
  if (
    pathname.startsWith("/access") ||
    pathname.startsWith("/system") ||
    pathname.startsWith("/settings") ||
    pathname.startsWith("/applications") ||
    pathname.startsWith("/business-lines") ||
    pathname.startsWith("/delivery-environments") ||
    pathname.startsWith("/application-environments") ||
    pathname.startsWith("/build-templates") ||
    pathname.startsWith("/delivery/blueprints") ||
    pathname.startsWith("/delivery/release-bundles") ||
    pathname.startsWith("/delivery/execution-tasks") ||
    pathname.startsWith("/delivery/approval-policies") ||
    pathname.startsWith("/workflow-templates") ||
    pathname.startsWith("/release-board") ||
    pathname.startsWith("/releases") ||
    pathname.startsWith("/registries") ||
    pathname.startsWith("/monitoring-workbench") ||
    pathname.startsWith("/observability") ||
    pathname.startsWith("/virtualization") ||
    pathname.startsWith("/docker") ||
    pathname.startsWith("/ai-gateway") ||
    pathname.startsWith("/ai-workbench") ||
    pathname.startsWith("/ai-observe")
  ) {
    return "passive";
  }
  return "namespace";
}

export function getAccessibleWorkbenchIds(
  snapshot?: PermissionSnapshot | null,
): Array<keyof typeof WORKBENCH_DEFAULT_PATHS> {
  const seen = new Set<keyof typeof WORKBENCH_DEFAULT_PATHS>();
  for (const route of routeMeta) {
    const workbenchId = getRouteWorkbenchId(route);
    if (!workbenchId || seen.has(workbenchId)) {
      continue;
    }
    if (!route.requiresAuth || !canAccessRoute(route, snapshot)) {
      continue;
    }
    seen.add(workbenchId);
  }
  return Array.from(seen);
}

export function findFirstAccessiblePathForWorkbench(
  workbenchId: keyof typeof WORKBENCH_DEFAULT_PATHS,
  snapshot?: PermissionSnapshot | null,
): string | null {
  const defaultPath = WORKBENCH_DEFAULT_PATHS[workbenchId];
  const defaultRoute = routeMeta.find((route) => route.path === defaultPath);
  if (defaultRoute && canAccessRoute(defaultRoute, snapshot)) {
    const defaultAccessiblePath = resolveAccessibleRoutePath(
      defaultRoute,
      snapshot,
    );
    if (defaultAccessiblePath) {
      return defaultAccessiblePath;
    }
  }
  for (const route of routeMeta) {
    if (
      route.requiresAuth &&
      route.navVisible &&
      getRouteWorkbenchId(route) === workbenchId &&
      canAccessRoute(route, snapshot)
    ) {
      const accessiblePath = resolveAccessibleRoutePath(route, snapshot);
      if (accessiblePath) {
        return accessiblePath;
      }
    }
  }
  return null;
}

function resolveAccessibleRoutePath(
  route: RouteMeta,
  snapshot?: PermissionSnapshot | null,
): string | null {
  if (!route.redirectTo) {
    return route.path;
  }
  const redirectRoute = routeMeta.find((item) => item.path === route.redirectTo);
  if (!redirectRoute) {
    return route.redirectTo;
  }
  return canAccessRoute(redirectRoute, snapshot) ? route.redirectTo : null;
}

export function getWorkspacePermissionKey(workspace: BusinessWorkspaceType) {
  return WORKSPACE_PERMISSION_KEYS[workspace];
}

export function hasWorkspaceAccess(
  workspace: BusinessWorkspaceType,
  snapshot?: PermissionSnapshot | null,
) {
  return (
    snapshot?.permissionKeys.includes(WORKSPACE_PERMISSION_KEYS[workspace]) ??
    false
  );
}

export function getAccessibleWorkspaces(
  snapshot?: PermissionSnapshot | null,
): BusinessWorkspaceType[] {
  const workspaces: BusinessWorkspaceType[] = [];
  (["application", "resource"] as const).forEach((workspace) => {
    if (!hasWorkspaceAccess(workspace, snapshot)) {
      return;
    }
    if (findFirstAccessiblePathForWorkspace(workspace, snapshot)) {
      workspaces.push(workspace);
    }
  });
  return workspaces;
}

export function getDefaultWorkspaceForRoles(
  roles: string[] = [],
): BusinessWorkspaceType {
  return roles.some((role) =>
    RESOURCE_DEFAULT_ROLES.has(String(role || "").trim()),
  )
    ? "resource"
    : "application";
}

export function findPreferredWorkspace(
  snapshot?: PermissionSnapshot | null,
  persistedWorkspace?: BusinessWorkspaceType | null,
  roles: string[] = [],
): BusinessWorkspaceType | null {
  const accessibleWorkspaces = getAccessibleWorkspaces(snapshot);
  if (accessibleWorkspaces.length === 0) {
    return null;
  }
  if (persistedWorkspace && accessibleWorkspaces.includes(persistedWorkspace)) {
    return persistedWorkspace;
  }
  const roleDefault = getDefaultWorkspaceForRoles(roles);
  if (accessibleWorkspaces.includes(roleDefault)) {
    return roleDefault;
  }
  return accessibleWorkspaces[0];
}

export function canAccessRoute(
  route: RouteMeta,
  snapshot?: PermissionSnapshot | null,
): boolean {
  if (!route.requiresAuth) {
    return true;
  }
  if (!snapshot) {
    return false;
  }
  if (route.permissionStrategy === "any-child") {
    return routeMeta
      .filter((child) => child.parentId === route.id)
      .some((child) => canAccessRoute(child, snapshot));
  }
  const permissionKey = resolveRoutePermission(route);
  const permissionKeysAny = route.permissionKeysAny ?? [];
  const menuId = resolveRouteMenuId(route);
  const workspace = getRouteWorkspace(route);
  const hasWorkspacePermission =
    workspace === "application" || workspace === "resource"
      ? hasWorkspaceAccess(workspace, snapshot)
      : true;
  const hasPermission = permissionKeysAny.length > 0
    ? permissionKeysAny.some((key) => snapshot.permissionKeys.includes(key))
    : !permissionKey || snapshot.permissionKeys.includes(permissionKey);
  const hasMenu = !menuId || snapshot.visibleMenuIds.includes(menuId);
  return hasWorkspacePermission && hasPermission && hasMenu;
}

function getSectionOrder(section?: string) {
  const normalized = normalizeMenuSection(String(section || ""));
  return normalized ? getMenuSectionOrder(normalized) : -1;
}

function sortRuntimeMenuTree(items: RuntimeMenuNode[]): RuntimeMenuNode[] {
  return [...items]
    .sort((left, right) => {
      const sectionCompare =
        getSectionOrder(left.section) - getSectionOrder(right.section);
      if (sectionCompare !== 0) return sectionCompare;
      if (left.sortOrder !== right.sortOrder)
        return left.sortOrder - right.sortOrder;
      return left.path.localeCompare(right.path);
    })
    .map(
      (item): RuntimeMenuNode => ({
        ...item,
        children:
          item.children && item.children.length > 0
            ? sortRuntimeMenuTree(item.children)
            : undefined,
      }),
    );
}

const APPLICATION_SECTION_ORDER: Record<string, number> = {
  builds: 10,
  applications: 10,
  "application-management": 15,
  "application-environments": 20,
  "release-board": 30,
  workflows: 40,
  releases: 50,
  "build-templates": 60,
  "approval-policies": 70,
  "release-bundles": 80,
  "execution-tasks": 90,
  registries: 100,
  "business-lines": 110,
  "delivery-environments": 120,
};

const SYSTEM_ROOT_ORDER: Record<string, number> = {
  access: 10,
  system: 20,
  settings: 30,
};

function deriveRuntimeMenuSection(node: RuntimeMenuNode): string {
  return normalizeMenuSection(node.section || "");
}

function deriveRuntimeMenuSortOrder(
  node: RuntimeMenuNode,
  workspace: WorkspaceType | null,
): number {
  if (workspace === "application" && node.id in APPLICATION_SECTION_ORDER) {
    return APPLICATION_SECTION_ORDER[node.id];
  }
  if (
    workspace === "system" &&
    !node.parentId &&
    node.id in SYSTEM_ROOT_ORDER
  ) {
    return SYSTEM_ROOT_ORDER[node.id];
  }
  return node.sortOrder;
}

function findBestRouteForMenu(
  menu: VisibleMenu,
  snapshot?: PermissionSnapshot | null,
): RouteMeta | undefined {
  const candidates = routeMeta
    .filter((route) => {
      const routeMenuId = resolveRouteMenuId(route);
      return routeMenuId === menu.id || route.path === menu.path;
    })
    .filter((route) => canAccessRoute(route, snapshot));

  if (candidates.length === 0) return undefined;

  return rankMenuRouteCandidates(candidates, menu.path)[0];
}

function buildRuntimeMenuTree(
  snapshot?: PermissionSnapshot | null,
): RuntimeMenuNode[] {
  const visibleMenus = snapshot?.visibleMenus ?? [];
  const nodes = new Map<string, RuntimeMenuNode>();

  visibleMenus.forEach((menu) => {
    const route = findBestRouteForMenu(menu, snapshot);
    nodes.set(menu.id, {
      id: menu.id,
      parentId: menu.parentId,
      path: menu.path,
      labelZh: menu.labelZh || menu.id,
      labelEn: menu.labelEn || menu.labelZh || menu.id,
      iconKey: menu.iconKey || "",
      section: normalizeMenuSection(menu.section || ""),
      sortOrder: typeof menu.sortOrder === "number" ? menu.sortOrder : 0,
      enabled: menu.enabled ?? true,
      workspace: route ? (getRouteWorkspace(route) ?? undefined) : undefined,
      workbenchId: route
        ? (getRouteWorkbenchId(route) ?? undefined)
        : undefined,
      route,
      children: [],
    });
  });

  const roots: RuntimeMenuNode[] = [];
  nodes.forEach((node) => {
    if (node.parentId && nodes.has(node.parentId)) {
      nodes.get(node.parentId)?.children?.push(node);
      return;
    }
    roots.push(node);
  });

  const prune = (node: RuntimeMenuNode): RuntimeMenuNode | null => {
    const nextChildren = (node.children ?? [])
      .map(prune)
      .filter((item): item is RuntimeMenuNode => Boolean(item));
    const keepAsContainer = nextChildren.length > 0;
    const keepAsLeaf = Boolean(node.route);
    if (!keepAsContainer && !keepAsLeaf) {
      return null;
    }
    return {
      ...node,
      children: nextChildren.length > 0 ? nextChildren : undefined,
    };
  };

  return sortRuntimeMenuTree(
    roots.map(prune).filter((item): item is RuntimeMenuNode => Boolean(item)),
  );
}

export function getAccessibleSidebarNav(
  snapshot?: PermissionSnapshot | null,
): RuntimeMenuNode[] {
  return buildRuntimeMenuTree(snapshot);
}

export function filterSidebarNavByWorkspace(
  sidebarNav: RuntimeMenuNode[],
  workspace: WorkspaceType,
): RuntimeMenuNode[] {
  const filterNode = (node: RuntimeMenuNode): RuntimeMenuNode | null => {
    const nextChildren = (node.children ?? [])
      .map(filterNode)
      .filter((item): item is RuntimeMenuNode => Boolean(item));
    const routeWorkspace = node.route
      ? getRouteWorkspace(node.route)
      : (node.workspace ?? null);
    const keepLeaf =
      Boolean(node.route && node.route.navVisible !== false) &&
      routeWorkspace === workspace;
    if (!keepLeaf && nextChildren.length === 0) {
      return null;
    }
    return {
      ...node,
      section: deriveRuntimeMenuSection(node),
      sortOrder: deriveRuntimeMenuSortOrder(node, workspace),
      workspace: workspace,
      workbenchId: node.route
        ? (getRouteWorkbenchId(node.route) ?? undefined)
        : node.workbenchId,
      children:
        nextChildren.length > 0 ? sortRuntimeMenuTree(nextChildren) : undefined,
    };
  };

  return sortRuntimeMenuTree(
    sidebarNav
      .map(filterNode)
      .filter((item): item is RuntimeMenuNode => Boolean(item)),
  );
}

export function filterSidebarNavByWorkbench(
  sidebarNav: RuntimeMenuNode[],
  workbenchId: WorkbenchId,
): RuntimeMenuNode[] {
  const filterNode = (node: RuntimeMenuNode): RuntimeMenuNode | null => {
    const nextChildren = (node.children ?? [])
      .map(filterNode)
      .filter((item): item is RuntimeMenuNode => Boolean(item));
    const nodeWorkbenchId = node.route
      ? getRouteWorkbenchId(node.route)
      : (node.workbenchId ?? getMenuWorkbenchId(node));
    const keepLeaf =
      Boolean(node.route && node.route.navVisible !== false) &&
      nodeWorkbenchId === workbenchId;
    if (!keepLeaf && nextChildren.length === 0) {
      return null;
    }
    return {
      ...node,
      workbenchId: nodeWorkbenchId ?? undefined,
      children:
        nextChildren.length > 0 ? sortRuntimeMenuTree(nextChildren) : undefined,
    };
  };

  const filteredTree = sortRuntimeMenuTree(
    sidebarNav
      .map(filterNode)
      .filter((item): item is RuntimeMenuNode => Boolean(item)),
  );

  const flattenedWorkbenchRootIds: Partial<Record<WorkbenchId, string>> = {
    docker: "docker-workbench",
    monitoring: "monitoring-workbench",
    virtualization: "virtualization-workbench",
  };
  const flattenedRootId = flattenedWorkbenchRootIds[workbenchId];

  if (!flattenedRootId) {
    return filteredTree;
  }

  return sortRuntimeMenuTree(
    filteredTree.flatMap((node) => {
      if (node.id === flattenedRootId && node.children?.length) {
        return node.children;
      }
      return [node];
    }),
  );
}

export function findFirstAccessiblePathForWorkspace(
  workspace: BusinessWorkspaceType,
  snapshot?: PermissionSnapshot | null,
): string | null {
  const defaultRoute = routeMeta.find(
    (route) => route.path === DEFAULT_WORKSPACE_PATHS[workspace],
  );
  if (defaultRoute && canAccessRoute(defaultRoute, snapshot)) {
    const defaultAccessiblePath = resolveAccessibleRoutePath(
      defaultRoute,
      snapshot,
    );
    if (defaultAccessiblePath) {
      return defaultAccessiblePath;
    }
  }
  for (const route of routeMeta) {
    if (!route.requiresAuth || !route.navVisible) {
      continue;
    }
    if (
      getRouteWorkspace(route) === workspace &&
      canAccessRoute(route, snapshot)
    ) {
      const accessiblePath = resolveAccessibleRoutePath(route, snapshot);
      if (accessiblePath) {
        return accessiblePath;
      }
    }
  }
  return null;
}

export function findFirstAccessiblePath(
  snapshot?: PermissionSnapshot | null,
  preferredWorkspace?: BusinessWorkspaceType | null,
): string | null {
  if (preferredWorkspace) {
    const preferredPath = findFirstAccessiblePathForWorkspace(
      preferredWorkspace,
      snapshot,
    );
    if (preferredPath) {
      return preferredPath;
    }
  }
  for (const route of routeMeta) {
    if (
      route.requiresAuth &&
      route.navVisible &&
      canAccessRoute(route, snapshot)
    ) {
      const accessiblePath = resolveAccessibleRoutePath(route, snapshot);
      if (accessiblePath) {
        return accessiblePath;
      }
    }
  }
  return null;
}

export function findLandingPath(
  snapshot?: PermissionSnapshot | null,
  persistedWorkspace?: BusinessWorkspaceType | null,
  roles: string[] = [],
): string | null {
  const workspace = findPreferredWorkspace(snapshot, persistedWorkspace, roles);
  return findFirstAccessiblePath(snapshot, workspace);
}
