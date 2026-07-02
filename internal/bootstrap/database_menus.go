package bootstrap

import (
	"context"
	"fmt"
	"strings"
	"time"

	cfgpkg "github.com/opensoha/soha/internal/infrastructure/config"
	"gorm.io/gorm"
)

type menuSeed struct {
	ID        string
	ParentID  string
	Path      string
	LabelZH   string
	LabelEN   string
	IconKey   string
	Section   string
	SortOrder int
	Enabled   bool
	Roles     []string
}

func defaultMenuSeeds() []menuSeed {
	return []menuSeed{
		{ID: "dashboard", Path: "/", LabelZH: "总览", LabelEN: "Dashboard", IconKey: "gauge", SortOrder: 10, Enabled: true},
		{ID: "cluster-resources-nodes", Path: "/cluster-resources/nodes", LabelZH: "节点", LabelEN: "Nodes", IconKey: "server", SortOrder: 20, Enabled: true},
		{ID: "extensions", Path: "/extensions", LabelZH: "CRD", LabelEN: "CRD", IconKey: "puzzle", SortOrder: 90, Enabled: true},
		{ID: "helm", Path: "/helm", LabelZH: "Helm", LabelEN: "Helm", IconKey: "puzzle", SortOrder: 80, Enabled: true},
		{ID: "helm-releases", ParentID: "helm", Path: "/helm/releases", LabelZH: "Releases", LabelEN: "Releases", IconKey: "puzzle", SortOrder: 20, Enabled: true},
		{ID: "helm-charts", ParentID: "helm", Path: "/helm/charts", LabelZH: "Charts", LabelEN: "Charts", IconKey: "puzzle", SortOrder: 21, Enabled: true},
		{ID: "platform-access-control", Path: "/platform-access-control", LabelZH: "RBAC", LabelEN: "RBAC", IconKey: "shield", SortOrder: 70, Enabled: true, Roles: []string{"admin", "ops", "developer", "readonly"}},
		{ID: "platform-access-control-serviceaccounts", ParentID: "platform-access-control", Path: "/platform-access-control/serviceaccounts", LabelZH: "ServiceAccounts", LabelEN: "ServiceAccounts", IconKey: "shield", SortOrder: 23, Enabled: true, Roles: []string{"admin", "ops", "developer", "readonly"}},
		{ID: "platform-access-control-clusterroles", ParentID: "platform-access-control", Path: "/platform-access-control/clusterroles", LabelZH: "ClusterRoles", LabelEN: "ClusterRoles", IconKey: "shield", SortOrder: 24, Enabled: true, Roles: []string{"admin", "ops", "developer", "readonly"}},
		{ID: "platform-access-control-roles", ParentID: "platform-access-control", Path: "/platform-access-control/roles", LabelZH: "Roles", LabelEN: "Roles", IconKey: "shield", SortOrder: 25, Enabled: true, Roles: []string{"admin", "ops", "developer", "readonly"}},
		{ID: "platform-access-control-clusterrolebindings", ParentID: "platform-access-control", Path: "/platform-access-control/clusterrolebindings", LabelZH: "ClusterRoleBindings", LabelEN: "ClusterRoleBindings", IconKey: "shield", SortOrder: 26, Enabled: true, Roles: []string{"admin", "ops", "developer", "readonly"}},
		{ID: "platform-access-control-rolebindings", ParentID: "platform-access-control", Path: "/platform-access-control/rolebindings", LabelZH: "RoleBindings", LabelEN: "RoleBindings", IconKey: "shield", SortOrder: 27, Enabled: true, Roles: []string{"admin", "ops", "developer", "readonly"}},
		{ID: "workloads", Path: "/workloads", LabelZH: "工作负载", LabelEN: "Workloads", IconKey: "boxes", SortOrder: 30, Enabled: true},
		{ID: "workloads-overview", ParentID: "workloads", Path: "/workloads/overview", LabelZH: "概览", LabelEN: "Overview", IconKey: "boxes", SortOrder: 31, Enabled: true},
		{ID: "workloads-deployments", ParentID: "workloads", Path: "/workloads/deployments", LabelZH: "Deployments", LabelEN: "Deployments", IconKey: "boxes", SortOrder: 32, Enabled: true},
		{ID: "workloads-pods", ParentID: "workloads", Path: "/workloads/pods", LabelZH: "Pods", LabelEN: "Pods", IconKey: "boxes", SortOrder: 33, Enabled: true},
		{ID: "workloads-statefulsets", ParentID: "workloads", Path: "/workloads/statefulsets", LabelZH: "StatefulSets", LabelEN: "StatefulSets", IconKey: "boxes", SortOrder: 34, Enabled: true},
		{ID: "workloads-daemonsets", ParentID: "workloads", Path: "/workloads/daemonsets", LabelZH: "DaemonSets", LabelEN: "DaemonSets", IconKey: "boxes", SortOrder: 35, Enabled: true},
		{ID: "workloads-jobs", ParentID: "workloads", Path: "/workloads/jobs", LabelZH: "Jobs", LabelEN: "Jobs", IconKey: "boxes", SortOrder: 36, Enabled: true},
		{ID: "workloads-cronjobs", ParentID: "workloads", Path: "/workloads/cronjobs", LabelZH: "CronJobs", LabelEN: "CronJobs", IconKey: "boxes", SortOrder: 37, Enabled: true},
		{ID: "workloads-replicasets", ParentID: "workloads", Path: "/workloads/replicasets", LabelZH: "ReplicaSets", LabelEN: "ReplicaSets", IconKey: "boxes", SortOrder: 38, Enabled: true},
		{ID: "workloads-replicationcontrollers", ParentID: "workloads", Path: "/workloads/replicationcontrollers", LabelZH: "ReplicationControllers", LabelEN: "ReplicationControllers", IconKey: "boxes", SortOrder: 39, Enabled: true},
		{ID: "configuration", Path: "/configuration", LabelZH: "配置", LabelEN: "Configuration", IconKey: "cog", SortOrder: 40, Enabled: true},
		{ID: "configuration-configmaps", ParentID: "configuration", Path: "/configuration/configmaps", LabelZH: "ConfigMaps", LabelEN: "ConfigMaps", IconKey: "cog", SortOrder: 41, Enabled: true},
		{ID: "configuration-secrets", ParentID: "configuration", Path: "/configuration/secrets", LabelZH: "Secrets", LabelEN: "Secrets", IconKey: "cog", SortOrder: 42, Enabled: true},
		{ID: "configuration-resourcequotas", ParentID: "configuration", Path: "/configuration/resourcequotas", LabelZH: "ResourceQuotas", LabelEN: "ResourceQuotas", IconKey: "cog", SortOrder: 43, Enabled: true},
		{ID: "configuration-limitranges", ParentID: "configuration", Path: "/configuration/limitranges", LabelZH: "LimitRanges", LabelEN: "LimitRanges", IconKey: "cog", SortOrder: 44, Enabled: true},
		{ID: "configuration-hpas", ParentID: "configuration", Path: "/configuration/hpas", LabelZH: "HorizontalPodAutoscalers", LabelEN: "HorizontalPodAutoscalers", IconKey: "cog", SortOrder: 45, Enabled: true},
		{ID: "configuration-poddisruptionbudgets", ParentID: "configuration", Path: "/configuration/poddisruptionbudgets", LabelZH: "PodDisruptionBudgets", LabelEN: "PodDisruptionBudgets", IconKey: "cog", SortOrder: 46, Enabled: true},
		{ID: "configuration-priorityclasses", ParentID: "configuration", Path: "/configuration/priorityclasses", LabelZH: "PriorityClasses", LabelEN: "PriorityClasses", IconKey: "cog", SortOrder: 47, Enabled: true},
		{ID: "configuration-runtimeclasses", ParentID: "configuration", Path: "/configuration/runtimeclasses", LabelZH: "RuntimeClasses", LabelEN: "RuntimeClasses", IconKey: "cog", SortOrder: 48, Enabled: true},
		{ID: "configuration-leases", ParentID: "configuration", Path: "/configuration/leases", LabelZH: "Leases", LabelEN: "Leases", IconKey: "cog", SortOrder: 49, Enabled: true},
		{ID: "configuration-mutatingwebhookconfigurations", ParentID: "configuration", Path: "/configuration/mutatingwebhookconfigurations", LabelZH: "MutatingWebhookConfigurations", LabelEN: "MutatingWebhookConfigurations", IconKey: "cog", SortOrder: 50, Enabled: true},
		{ID: "configuration-validatingwebhookconfigurations", ParentID: "configuration", Path: "/configuration/validatingwebhookconfigurations", LabelZH: "ValidatingWebhookConfigurations", LabelEN: "ValidatingWebhookConfigurations", IconKey: "cog", SortOrder: 51, Enabled: true},
		{ID: "network", Path: "/network", LabelZH: "网络", LabelEN: "Network", IconKey: "network", SortOrder: 50, Enabled: true},
		{ID: "network-topology", ParentID: "network", Path: "/network/topology", LabelZH: "网络拓扑", LabelEN: "Network Topology", IconKey: "network", SortOrder: 40, Enabled: true},
		{ID: "network-services", ParentID: "network", Path: "/network/services", LabelZH: "Services", LabelEN: "Services", IconKey: "network", SortOrder: 41, Enabled: true},
		{ID: "network-ingresses", ParentID: "network", Path: "/network/ingresses", LabelZH: "Ingresses", LabelEN: "Ingresses", IconKey: "network", SortOrder: 42, Enabled: true},
		{ID: "network-gateway-api-gatewayclasses", ParentID: "network", Path: "/network/gateway-api/gatewayclasses", LabelZH: "GatewayClasses", LabelEN: "GatewayClasses", IconKey: "network", SortOrder: 43, Enabled: true},
		{ID: "network-gateway-api-gateways", ParentID: "network", Path: "/network/gateway-api/gateways", LabelZH: "Gateways", LabelEN: "Gateways", IconKey: "network", SortOrder: 44, Enabled: true},
		{ID: "network-gateway-api-httproutes", ParentID: "network", Path: "/network/gateway-api/httproutes", LabelZH: "HTTPRoutes", LabelEN: "HTTPRoutes", IconKey: "network", SortOrder: 45, Enabled: true},
		{ID: "network-gateway-api-backendtlspolicies", ParentID: "network", Path: "/network/gateway-api/backendtlspolicies", LabelZH: "BackendTLSPolicies", LabelEN: "BackendTLSPolicies", IconKey: "network", SortOrder: 46, Enabled: true},
		{ID: "network-gateway-api-grpcroutes", ParentID: "network", Path: "/network/gateway-api/grpcroutes", LabelZH: "GRPCRoutes", LabelEN: "GRPCRoutes", IconKey: "network", SortOrder: 47, Enabled: true},
		{ID: "network-gateway-api-referencegrants", ParentID: "network", Path: "/network/gateway-api/referencegrants", LabelZH: "ReferenceGrants", LabelEN: "ReferenceGrants", IconKey: "network", SortOrder: 48, Enabled: true},
		{ID: "network-endpointslices", ParentID: "network", Path: "/network/endpointslices", LabelZH: "EndpointSlices", LabelEN: "EndpointSlices", IconKey: "network", SortOrder: 53, Enabled: true},
		{ID: "network-ingressclasses", ParentID: "network", Path: "/network/ingressclasses", LabelZH: "IngressClasses", LabelEN: "IngressClasses", IconKey: "network", SortOrder: 54, Enabled: true},
		{ID: "network-networkpolicies", ParentID: "network", Path: "/network/networkpolicies", LabelZH: "NetworkPolicies", LabelEN: "NetworkPolicies", IconKey: "network", SortOrder: 55, Enabled: true},
		{ID: "network-port-forward", ParentID: "network", Path: "/network/port-forward", LabelZH: "端口转发", LabelEN: "Port Forward", IconKey: "network", SortOrder: 56, Enabled: true},
		{ID: "storage", Path: "/storage", LabelZH: "存储", LabelEN: "Storage", IconKey: "waves", SortOrder: 60, Enabled: true},
		{ID: "storage-pvc", ParentID: "storage", Path: "/storage/persistentvolumeclaims", LabelZH: "PVC", LabelEN: "PVC", IconKey: "waves", SortOrder: 51, Enabled: true},
		{ID: "storage-pv", ParentID: "storage", Path: "/storage/persistentvolumes", LabelZH: "PV", LabelEN: "PV", IconKey: "waves", SortOrder: 52, Enabled: true},
		{ID: "storage-classes", ParentID: "storage", Path: "/storage/storageclasses", LabelZH: "StorageClasses", LabelEN: "StorageClasses", IconKey: "waves", SortOrder: 53, Enabled: true},
		{ID: "clusters", Path: "/clusters", LabelZH: "集群", LabelEN: "Clusters", IconKey: "globe", SortOrder: 99, Enabled: true},
		{ID: "monitoring-workbench", Path: "/monitoring-workbench", LabelZH: "监控工作台", LabelEN: "Monitoring Workbench", IconKey: "gauge", Section: "ops", SortOrder: 60, Enabled: true},
		{ID: "monitoring-workbench-overview", ParentID: "monitoring-workbench", Path: "/monitoring-workbench/overview", LabelZH: "总览", LabelEN: "Overview", IconKey: "gauge", Section: "ops", SortOrder: 61, Enabled: true},
		{ID: "monitoring-workbench-integrations", ParentID: "monitoring-workbench", Path: "/monitoring-workbench/integrations", LabelZH: "告警集成", LabelEN: "Alert Integrations", IconKey: "link", Section: "ops", SortOrder: 62, Enabled: true},
		{ID: "monitoring-workbench-rules", ParentID: "monitoring-workbench", Path: "/monitoring-workbench/rules", LabelZH: "告警规则", LabelEN: "Alert Rules", IconKey: "siren", Section: "ops", SortOrder: 63, Enabled: true},
		{ID: "monitoring-workbench-alerts", ParentID: "monitoring-workbench", Path: "/monitoring-workbench/alerts", LabelZH: "活跃告警", LabelEN: "Active Alerts", IconKey: "siren", Section: "ops", SortOrder: 64, Enabled: true},
		{ID: "monitoring-workbench-notifications", ParentID: "monitoring-workbench", Path: "/monitoring-workbench/notifications", LabelZH: "通知策略", LabelEN: "Notification Policies", IconKey: "bell", Section: "ops", SortOrder: 65, Enabled: true},
		{ID: "monitoring-workbench-healing", ParentID: "monitoring-workbench", Path: "/monitoring-workbench/healing", LabelZH: "自愈中心", LabelEN: "Healing Center", IconKey: "activity", Section: "ops", SortOrder: 66, Enabled: true},
		{ID: "monitoring-workbench-oncall", ParentID: "monitoring-workbench", Path: "/monitoring-workbench/oncall", LabelZH: "值班协同", LabelEN: "On-Call Coordination", IconKey: "users", Section: "ops", SortOrder: 67, Enabled: true},
		{ID: "monitoring-workbench-events", ParentID: "monitoring-workbench", Path: "/monitoring-workbench/events", LabelZH: "事件流", LabelEN: "Events", IconKey: "bell", Section: "ops", SortOrder: 68, Enabled: true},
		{ID: "ai-workbench", Path: "/ai-workbench", LabelZH: "AI工作台", LabelEN: "AI Workbench", IconKey: "bot", Section: "ops", SortOrder: 15, Enabled: true},
		{ID: "ai-workbench-chat", ParentID: "ai-workbench", Path: "/ai-workbench/chat", LabelZH: "通用聊天", LabelEN: "Chat", IconKey: "bot", Section: "ops", SortOrder: 16, Enabled: true},
		{ID: "ai-workbench-inspection", ParentID: "ai-workbench", Path: "/ai-workbench/inspection", LabelZH: "巡检", LabelEN: "Inspection", IconKey: "inspect", Section: "ops", SortOrder: 17, Enabled: true},
		{ID: "ai-workbench-tool-settings", ParentID: "ai-workbench", Path: "/ai-workbench/tool-settings", LabelZH: "工具与技能", LabelEN: "Tools & Skills", IconKey: "wrench", Section: "ops", SortOrder: 18, Enabled: true},
		{ID: "ai-workbench-model-settings", ParentID: "ai-workbench", Path: "/ai-workbench/model-settings", LabelZH: "AI 设置", LabelEN: "AI Settings", IconKey: "settings", Section: "ops", SortOrder: 19, Enabled: true},
		{ID: "ai-gateway", Path: "/ai-gateway", LabelZH: "AI Gateway", LabelEN: "AI Gateway", IconKey: "shield", Section: "ops", SortOrder: 20, Enabled: true},
		{ID: "ai-gateway-overview", ParentID: "ai-gateway", Path: "/ai-gateway/overview", LabelZH: "概览", LabelEN: "Overview", IconKey: "gauge", Section: "ops", SortOrder: 21, Enabled: true},
		{ID: "ai-gateway-relay", ParentID: "ai-gateway", Path: "/ai-gateway/relay", LabelZH: "模型中转", LabelEN: "Model Relay", IconKey: "link", Section: "ops", SortOrder: 22, Enabled: true},
		{ID: "ai-gateway-manifest", ParentID: "ai-gateway", Path: "/ai-gateway/manifest", LabelZH: "能力清单", LabelEN: "Manifest", IconKey: "shield", Section: "ops", SortOrder: 23, Enabled: true},
		{ID: "ai-gateway-clients", ParentID: "ai-gateway", Path: "/ai-gateway/clients", LabelZH: "AI Clients", LabelEN: "AI Clients", IconKey: "link", Section: "ops", SortOrder: 24, Enabled: true},
		{ID: "ai-gateway-tokens", ParentID: "ai-gateway", Path: "/ai-gateway/tokens", LabelZH: "Tokens", LabelEN: "Tokens", IconKey: "key", Section: "ops", SortOrder: 25, Enabled: true},
		{ID: "ai-gateway-governance", ParentID: "ai-gateway", Path: "/ai-gateway/governance", LabelZH: "Governance", LabelEN: "Governance", IconKey: "shield", Section: "ops", SortOrder: 26, Enabled: true},
		{ID: "ai-gateway-call-logs", ParentID: "ai-gateway", Path: "/ai-gateway/call-logs", LabelZH: "调用日志", LabelEN: "Call Logs", IconKey: "history", Section: "ops", SortOrder: 27, Enabled: true},
		{ID: "plugins", ParentID: "ai-gateway", Path: "/plugins", LabelZH: "插件", LabelEN: "Plugins", IconKey: "puzzle", Section: "ops", SortOrder: 28, Enabled: true},
		{ID: "plugins-marketplace", ParentID: "plugins", Path: "/plugins/marketplace", LabelZH: "市场", LabelEN: "Marketplace", IconKey: "puzzle", Section: "ops", SortOrder: 29, Enabled: true},
		{ID: "plugins-installed", ParentID: "plugins", Path: "/plugins/installed", LabelZH: "已安装", LabelEN: "Installed", IconKey: "blocks", Section: "ops", SortOrder: 30, Enabled: true},
		{ID: "virtualization-workbench", Path: "/virtualization", LabelZH: "虚拟化管理工作台", LabelEN: "Virtualization Workbench", IconKey: "server", Section: "ops", SortOrder: 80, Enabled: true},
		{ID: "virtualization-workbench-overview", ParentID: "virtualization-workbench", Path: "/virtualization/overview", LabelZH: "总览", LabelEN: "Overview", IconKey: "gauge", Section: "ops", SortOrder: 81, Enabled: true},
		{ID: "virtualization-workbench-vms", ParentID: "virtualization-workbench", Path: "/virtualization/vms", LabelZH: "虚拟机", LabelEN: "Virtual Machines", IconKey: "desktop", Section: "ops", SortOrder: 82, Enabled: true},
		{ID: "virtualization-workbench-clusters", ParentID: "virtualization-workbench", Path: "/virtualization/clusters", LabelZH: "集群", LabelEN: "Clusters", IconKey: "cluster", Section: "ops", SortOrder: 83, Enabled: true},
		{ID: "virtualization-workbench-images", ParentID: "virtualization-workbench", Path: "/virtualization/images", LabelZH: "镜像", LabelEN: "Images", IconKey: "image", Section: "ops", SortOrder: 84, Enabled: true},
		{ID: "virtualization-workbench-flavors", ParentID: "virtualization-workbench", Path: "/virtualization/flavors", LabelZH: "规格", LabelEN: "Flavors", IconKey: "flavor", Section: "ops", SortOrder: 85, Enabled: true},
		{ID: "virtualization-workbench-operations", ParentID: "virtualization-workbench", Path: "/virtualization/operations", LabelZH: "操作记录", LabelEN: "Operations", IconKey: "history", Section: "ops", SortOrder: 86, Enabled: true},
		{ID: "virtualization-workbench-sync", ParentID: "virtualization-workbench", Path: "/virtualization/sync", LabelZH: "同步任务", LabelEN: "Sync Tasks", IconKey: "sync", Section: "ops", SortOrder: 87, Enabled: true},
		{ID: "docker-workbench", Path: "/docker", LabelZH: "Docker 工作台", LabelEN: "Docker Workbench", IconKey: "docker", Section: "ops", SortOrder: 90, Enabled: true, Roles: []string{"admin", "ops", "developer", "readonly"}},
		{ID: "docker-workbench-overview", ParentID: "docker-workbench", Path: "/docker/overview", LabelZH: "总览", LabelEN: "Overview", IconKey: "gauge", Section: "ops", SortOrder: 91, Enabled: true, Roles: []string{"admin", "ops", "developer", "readonly"}},
		{ID: "docker-workbench-hosts", ParentID: "docker-workbench", Path: "/docker/hosts", LabelZH: "Docker 主机", LabelEN: "Docker Hosts", IconKey: "server", Section: "ops", SortOrder: 92, Enabled: true, Roles: []string{"admin", "ops", "developer", "readonly"}},
		{ID: "docker-workbench-projects", ParentID: "docker-workbench", Path: "/docker/projects", LabelZH: "容器管理", LabelEN: "Container Management", IconKey: "docker", Section: "ops", SortOrder: 93, Enabled: true, Roles: []string{"admin", "ops", "developer", "readonly"}},
		{ID: "docker-workbench-templates", ParentID: "docker-workbench", Path: "/docker/templates", LabelZH: "模板", LabelEN: "Templates", IconKey: "code", Section: "ops", SortOrder: 96, Enabled: true, Roles: []string{"admin", "ops", "developer", "readonly"}},
		{ID: "docker-workbench-operations", ParentID: "docker-workbench", Path: "/docker/operations", LabelZH: "操作记录", LabelEN: "Operations", IconKey: "history", Section: "ops", SortOrder: 97, Enabled: true, Roles: []string{"admin", "ops", "developer", "readonly"}},
		{ID: "builds", Path: "/applications", LabelZH: "应用中心", LabelEN: "Application Center", IconKey: "blocks", Section: "delivery", SortOrder: 10, Enabled: true, Roles: []string{"admin", "ops", "developer", "tester", "readonly"}},
		{ID: "delivery-onboarding", Path: "/delivery/onboarding", LabelZH: "应用接入", LabelEN: "Application Onboarding", IconKey: "code", Section: "delivery", SortOrder: 20, Enabled: true, Roles: []string{"admin", "ops", "developer"}},
		{ID: "release-board", Path: "/release-board", LabelZH: "构建发布", LabelEN: "Build & Release", IconKey: "activity", Section: "delivery", SortOrder: 30, Enabled: true, Roles: []string{"admin", "ops", "developer"}},
		{ID: "delivery-testing", Path: "/delivery/testing", LabelZH: "测试验证", LabelEN: "Testing & Verification", IconKey: "shield", Section: "delivery", SortOrder: 40, Enabled: true, Roles: []string{"admin", "ops", "developer", "tester", "readonly"}},
		{ID: "delivery-analysis", Path: "/delivery/analysis", LabelZH: "问题分析", LabelEN: "Issue Analysis", IconKey: "activity", Section: "delivery", SortOrder: 50, Enabled: true, Roles: []string{"admin", "ops", "developer", "tester", "readonly"}},
		{ID: "release-bundles", Path: "/delivery/release-bundles", LabelZH: "版本包", LabelEN: "Release Bundles", IconKey: "blocks", Section: "delivery-records", SortOrder: 10, Enabled: true, Roles: []string{"admin", "ops", "developer", "tester", "readonly"}},
		{ID: "execution-tasks", Path: "/delivery/execution-tasks", LabelZH: "执行任务", LabelEN: "Execution Tasks", IconKey: "activity", Section: "delivery-records", SortOrder: 20, Enabled: true, Roles: []string{"admin", "ops", "developer", "tester", "readonly"}},
		{ID: "workflows", Path: "/workflows", LabelZH: "工作流", LabelEN: "Workflows", IconKey: "activity", Section: "delivery-records", SortOrder: 30, Enabled: true, Roles: []string{"admin", "ops", "developer", "readonly"}},
		{ID: "releases", Path: "/releases", LabelZH: "发布记录", LabelEN: "Release Records", IconKey: "activity", Section: "delivery-records", SortOrder: 40, Enabled: true, Roles: []string{"admin", "ops", "developer", "readonly"}},
		{ID: "delivery-blueprints", Path: "/delivery/blueprints", LabelZH: "应用接入模板", LabelEN: "Onboarding Templates", IconKey: "code", Section: "delivery-platform", SortOrder: 10, Enabled: true, Roles: []string{"admin", "ops"}},
		{ID: "build-templates", Path: "/build-templates", LabelZH: "构建模板", LabelEN: "Build Templates", IconKey: "code", Section: "delivery-platform", SortOrder: 20, Enabled: true, Roles: []string{"admin", "ops"}},
		{ID: "workflow-templates", Path: "/workflow-templates", LabelZH: "发布流程模板", LabelEN: "Workflow Templates", IconKey: "activity", Section: "delivery-platform", SortOrder: 30, Enabled: true, Roles: []string{"admin", "ops"}},
		{ID: "application-environments", Path: "/application-environments", LabelZH: "环境绑定", LabelEN: "Environment Bindings", IconKey: "blocks", Section: "delivery-platform", SortOrder: 50, Enabled: true, Roles: []string{"admin", "ops"}},
		{ID: "system", Path: "/system", LabelZH: "系统", LabelEN: "System", IconKey: "panels-top-left", Section: "admin", SortOrder: 225, Enabled: true},
		{ID: "announcements", ParentID: "system", Path: "/system/announcements", LabelZH: "通知公告", LabelEN: "Announcements", IconKey: "megaphone", Section: "admin", SortOrder: 230, Enabled: true, Roles: []string{"admin"}},
		{ID: "access", Path: "/access", LabelZH: "访问控制", LabelEN: "Access Control", IconKey: "shield", Section: "admin", SortOrder: 240, Enabled: true, Roles: []string{"admin"}},
		{ID: "access-users", Path: "/access/users", LabelZH: "用户", LabelEN: "Users", IconKey: "user", Section: "admin", SortOrder: 226, Enabled: true, Roles: []string{"admin"}},
		{ID: "access-roles", Path: "/access/roles", LabelZH: "角色", LabelEN: "Roles", IconKey: "shield", Section: "admin", SortOrder: 227, Enabled: true, Roles: []string{"admin"}},
		{ID: "access-teams", Path: "/access/teams", LabelZH: "组织", LabelEN: "Organizations", IconKey: "users", Section: "admin", SortOrder: 228, Enabled: true, Roles: []string{"admin"}},
		{ID: "access-policies", Path: "/access/policies", LabelZH: "策略", LabelEN: "Policies", IconKey: "shield", Section: "admin", SortOrder: 229, Enabled: true, Roles: []string{"admin"}},
		{ID: "menus", ParentID: "system", Path: "/system/menus", LabelZH: "菜单管理", LabelEN: "Menu Management", IconKey: "menu-square", Section: "admin", SortOrder: 250, Enabled: true, Roles: []string{"admin"}},
		{ID: "system-online-users", ParentID: "system", Path: "/system/online-users", LabelZH: "在线用户", LabelEN: "Online Users", IconKey: "users", Section: "admin", SortOrder: 256, Enabled: true, Roles: []string{"admin"}},
		{ID: "operations", ParentID: "system", Path: "/system/operations", LabelZH: "操作日志", LabelEN: "Operation Logs", IconKey: "clipboard-list", Section: "admin", SortOrder: 257, Enabled: true},
		{ID: "audit", ParentID: "system", Path: "/system/audit", LabelZH: "审计日志", LabelEN: "Audit Logs", IconKey: "file-clock", Section: "admin", SortOrder: 258, Enabled: true},
		{ID: "registries", Path: "/registries", LabelZH: "镜像仓库", LabelEN: "Registry Connections", IconKey: "menu-square", Section: "delivery-platform", SortOrder: 70, Enabled: true, Roles: []string{"admin", "ops"}},
		{ID: "settings", Path: "/settings", LabelZH: "设置中心", LabelEN: "Settings Center", IconKey: "cog", Section: "admin", SortOrder: 260, Enabled: true, Roles: []string{"admin"}},
		{ID: "settings-login", ParentID: "settings", Path: "/settings/login", LabelZH: "登陆设置", LabelEN: "Login Settings", IconKey: "shield", Section: "admin", SortOrder: 261, Enabled: true, Roles: []string{"admin"}},
		{ID: "settings-branding", ParentID: "settings", Path: "/settings/branding", LabelZH: "品牌设置", LabelEN: "Branding Settings", IconKey: "palette", Section: "admin", SortOrder: 262, Enabled: true, Roles: []string{"admin"}},
	}
}

func deprecatedMenuIDs() []string {
	return []string{
		"assistant-root-cause",
		"assistant-performance",
		"assistant-chat",
		"assistant-inspection",
		"network-gateways",
		"network-http-routes",
		"observability",
		"monitoring",
		"rules",
		"alerts",
		"notifications",
		"healing",
		"oncall",
		"assistant",
		"assistant-workbench",
		"assistant-operations",
		"assistant-tools",
		"ai-workbench-gateway",
		"docker-workbench-services",
		"docker-workbench-ports",
		"events",
		"business-lines",
		"delivery-environments",
		"application-management",
	}
}

func validateMenuSeeds(items []menuSeed) error {
	ids := make(map[string]struct{}, len(items))
	for _, item := range items {
		if _, exists := ids[item.ID]; exists {
			return fmt.Errorf("duplicate menu seed id %q", item.ID)
		}
		ids[item.ID] = struct{}{}
	}
	for _, item := range items {
		if item.ParentID == "" {
			continue
		}
		if _, exists := ids[item.ParentID]; !exists {
			return fmt.Errorf("menu seed %q references missing parent %q", item.ID, item.ParentID)
		}
	}
	return nil
}

func seedMenus(ctx context.Context, db *gorm.DB, modules cfgpkg.ModulesConfig) error {
	now := time.Now().UTC()
	items := defaultMenuSeeds()
	if err := validateMenuSeeds(items); err != nil {
		return err
	}
	allItems := append([]menuSeed(nil), items...)
	items = filterSeedMenusByModules(items, modules)
	if err := upsertMenus(ctx, db, items, now); err != nil {
		return err
	}
	if err := deleteDisabledModuleMenus(ctx, db, allItems, modules); err != nil {
		return err
	}
	menuIDs := make([]string, 0, len(items))
	roleBindingValues := make([][]string, 0)
	for _, item := range items {
		menuIDs = append(menuIDs, item.ID)
		for _, roleID := range item.Roles {
			roleBindingValues = append(roleBindingValues, []string{fmt.Sprintf("%s:%s", item.ID, roleID), item.ID, roleID})
		}
	}
	if err := db.WithContext(ctx).Exec(`DELETE FROM menu_role_bindings WHERE menu_id IN ?`, menuIDs).Error; err != nil {
		return err
	}
	if len(roleBindingValues) > 0 {
		if err := insertMenuRoleBindings(ctx, db, roleBindingValues, now); err != nil {
			return err
		}
	}
	return cleanupDeprecatedMenus(ctx, db, deprecatedMenuIDs())
}

func cleanupDeprecatedMenus(ctx context.Context, db *gorm.DB, deprecatedIDs []string) error {
	if len(deprecatedIDs) == 0 {
		return nil
	}
	if err := db.WithContext(ctx).Exec(`DELETE FROM menu_role_bindings WHERE menu_id IN ?`, deprecatedIDs).Error; err != nil {
		return err
	}
	return db.WithContext(ctx).Exec(`DELETE FROM menus WHERE id IN ?`, deprecatedIDs).Error
}

func syncBuiltinMenuSeedUpgrades(ctx context.Context, db *gorm.DB) error {
	now := time.Now().UTC()
	accessItems := []struct {
		id        string
		iconKey   string
		sortOrder int
	}{
		{id: "access-users", iconKey: "user", sortOrder: 226},
		{id: "access-roles", iconKey: "shield", sortOrder: 227},
		{id: "access-teams", iconKey: "users", sortOrder: 228},
		{id: "access-policies", iconKey: "shield", sortOrder: 229},
	}
	for _, item := range accessItems {
		if err := db.WithContext(ctx).Exec(`
			UPDATE menus
			SET parent_id = NULL,
				icon_key = CASE WHEN icon_key = 'shield' THEN ? ELSE icon_key END,
				sort_order = ?,
				updated_at = ?
			WHERE id = ? AND parent_id = 'access'
		`, item.iconKey, item.sortOrder, now, item.id).Error; err != nil {
			return err
		}
	}

	labelUpdates := []struct {
		id      string
		oldZH   string
		oldEN   string
		labelZH string
		labelEN string
	}{
		{id: "operations", oldZH: "操作", oldEN: "Operations", labelZH: "操作日志", labelEN: "Operation Logs"},
		{id: "audit", oldZH: "审计", oldEN: "Audit", labelZH: "审计日志", labelEN: "Audit Logs"},
		{id: "access-teams", oldZH: "用户组", oldEN: "User Groups", labelZH: "组织", labelEN: "Organizations"},
		{id: "docker-workbench-projects", oldZH: "Compose 项目", oldEN: "Compose Projects", labelZH: "容器管理", labelEN: "Container Management"},
	}
	for _, item := range labelUpdates {
		if err := db.WithContext(ctx).Exec(`
			UPDATE menus
			SET label_zh = ?,
				label_en = ?,
				updated_at = ?
			WHERE id = ? AND label_zh = ? AND label_en = ?
		`, item.labelZH, item.labelEN, now, item.id, item.oldZH, item.oldEN).Error; err != nil {
			return err
		}
	}

	deliveryItems := []struct {
		id        string
		section   string
		sortOrder int
		labelZH   string
		labelEN   string
		oldZH     string
		oldEN     string
	}{
		{id: "builds", section: "delivery", sortOrder: 10, labelZH: "应用中心", labelEN: "Application Center", oldZH: "应用中心", oldEN: "Applications"},
		{id: "delivery-onboarding", section: "delivery", sortOrder: 20, labelZH: "应用接入", labelEN: "Application Onboarding", oldZH: "应用接入", oldEN: "Application Onboarding"},
		{id: "release-board", section: "delivery", sortOrder: 30, labelZH: "构建发布", labelEN: "Build & Release", oldZH: "发布看板", oldEN: "Release Board"},
		{id: "delivery-testing", section: "delivery", sortOrder: 40, labelZH: "测试验证", labelEN: "Testing & Verification", oldZH: "测试验证", oldEN: "Testing & Verification"},
		{id: "delivery-analysis", section: "delivery", sortOrder: 50, labelZH: "问题分析", labelEN: "Issue Analysis", oldZH: "问题分析", oldEN: "Issue Analysis"},
		{id: "release-bundles", section: "delivery-records", sortOrder: 10, labelZH: "版本包", labelEN: "Release Bundles", oldZH: "版本包", oldEN: "Release Bundles"},
		{id: "execution-tasks", section: "delivery-records", sortOrder: 20, labelZH: "执行任务", labelEN: "Execution Tasks", oldZH: "执行任务", oldEN: "Execution Tasks"},
		{id: "workflows", section: "delivery-records", sortOrder: 30, labelZH: "工作流", labelEN: "Workflows", oldZH: "工作流", oldEN: "Workflows"},
		{id: "releases", section: "delivery-records", sortOrder: 40, labelZH: "发布记录", labelEN: "Release Records", oldZH: "发布", oldEN: "Releases"},
		{id: "delivery-blueprints", section: "delivery-platform", sortOrder: 10, labelZH: "应用接入模板", labelEN: "Onboarding Templates", oldZH: "交付蓝图", oldEN: "Delivery Blueprints"},
		{id: "build-templates", section: "delivery-platform", sortOrder: 20, labelZH: "构建模板", labelEN: "Build Templates", oldZH: "构建模板", oldEN: "Build Templates"},
		{id: "workflow-templates", section: "delivery-platform", sortOrder: 30, labelZH: "发布流程模板", labelEN: "Workflow Templates", oldZH: "发布流程模板", oldEN: "Workflow Templates"},
		{id: "application-environments", section: "delivery-platform", sortOrder: 50, labelZH: "环境绑定", labelEN: "Environment Bindings", oldZH: "应用环境绑定", oldEN: "Application Environment Bindings"},
		{id: "registries", section: "delivery-platform", sortOrder: 70, labelZH: "镜像仓库", labelEN: "Registry Connections", oldZH: "镜像仓库", oldEN: "Registry Connections"},
	}
	for _, item := range deliveryItems {
		if err := db.WithContext(ctx).Exec(`
			UPDATE menus
			SET section = ?,
				sort_order = ?,
				label_zh = CASE WHEN label_zh = ? THEN ? ELSE label_zh END,
				label_en = CASE WHEN label_en = ? THEN ? ELSE label_en END,
				updated_at = ?
			WHERE id = ? AND section IN ('deliver', 'delivery', 'delivery-records', 'delivery-platform')
		`, item.section, item.sortOrder, item.oldZH, item.labelZH, item.oldEN, item.labelEN, now, item.id).Error; err != nil {
			return err
		}
	}
	gatewayItems := []struct {
		id        string
		path      string
		sortOrder int
	}{
		{id: "network-gateway-api-gatewayclasses", path: "/network/gateway-api/gatewayclasses", sortOrder: 43},
		{id: "network-gateway-api-gateways", path: "/network/gateway-api/gateways", sortOrder: 44},
		{id: "network-gateway-api-httproutes", path: "/network/gateway-api/httproutes", sortOrder: 45},
		{id: "network-gateway-api-backendtlspolicies", path: "/network/gateway-api/backendtlspolicies", sortOrder: 46},
		{id: "network-gateway-api-grpcroutes", path: "/network/gateway-api/grpcroutes", sortOrder: 47},
		{id: "network-gateway-api-referencegrants", path: "/network/gateway-api/referencegrants", sortOrder: 48},
	}
	for _, item := range gatewayItems {
		if err := db.WithContext(ctx).Exec(`
			UPDATE menus
			SET parent_id = 'network',
				path = ?,
				sort_order = ?,
				updated_at = ?
			WHERE id = ? AND (parent_id = 'network-gateway-api' OR parent_id = 'network' OR parent_id IS NULL)
		`, item.path, item.sortOrder, now, item.id).Error; err != nil {
			return err
		}
	}
	if err := db.WithContext(ctx).Exec(`DELETE FROM menu_role_bindings WHERE menu_id = 'network-gateway-api'`).Error; err != nil {
		return err
	}
	if err := db.WithContext(ctx).Exec(`DELETE FROM menus WHERE id = 'network-gateway-api'`).Error; err != nil {
		return err
	}
	return nil
}

func nullableMenu(value string) any {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return value
}

func filterSeedMenusByModules(items []menuSeed, modules cfgpkg.ModulesConfig) []menuSeed {
	filtered := make([]menuSeed, 0, len(items))
	for _, item := range items {
		switch {
		case !modules.Delivery.Enabled && isDeliveryMenuSeed(item):
			continue
		case !modules.Monitoring.Enabled && isMonitoringMenuSeed(item):
			continue
		case !modules.AI.Enabled && isAIMenuSeed(item):
			continue
		case !modules.AIGateway.Enabled && isAIGatewayMenuSeed(item):
			continue
		case !modules.Virtualization.Enabled && isVirtualizationMenuSeed(item):
			continue
		case !modules.Docker.Enabled && isDockerMenuSeed(item):
			continue
		default:
			filtered = append(filtered, item)
		}
	}
	return filtered
}

func deleteDisabledModuleMenus(ctx context.Context, db *gorm.DB, items []menuSeed, modules cfgpkg.ModulesConfig) error {
	menuIDs := disabledModuleMenuIDs(items, modules)
	if len(menuIDs) == 0 {
		return nil
	}
	if err := db.WithContext(ctx).Exec(`DELETE FROM menu_role_bindings WHERE menu_id IN ?`, menuIDs).Error; err != nil {
		return err
	}
	return db.WithContext(ctx).Exec(`DELETE FROM menus WHERE id IN ?`, menuIDs).Error
}

func disabledModuleMenuIDs(items []menuSeed, modules cfgpkg.ModulesConfig) []string {
	menuIDs := make([]string, 0)
	for _, item := range items {
		switch {
		case !modules.Delivery.Enabled && isDeliveryMenuSeed(item):
			menuIDs = append(menuIDs, item.ID)
		case !modules.Monitoring.Enabled && isMonitoringMenuSeed(item):
			menuIDs = append(menuIDs, item.ID)
		case !modules.AI.Enabled && isAIMenuSeed(item):
			menuIDs = append(menuIDs, item.ID)
		case !modules.AIGateway.Enabled && isAIGatewayMenuSeed(item):
			menuIDs = append(menuIDs, item.ID)
		case !modules.Virtualization.Enabled && isVirtualizationMenuSeed(item):
			menuIDs = append(menuIDs, item.ID)
		case !modules.Docker.Enabled && isDockerMenuSeed(item):
			menuIDs = append(menuIDs, item.ID)
		}
	}
	return menuIDs
}

func isDeliveryMenuSeed(item menuSeed) bool {
	return strings.HasPrefix(item.Path, "/applications") ||
		strings.HasPrefix(item.Path, "/application-environments") ||
		strings.HasPrefix(item.Path, "/build-templates") ||
		strings.HasPrefix(item.Path, "/delivery/onboarding") ||
		strings.HasPrefix(item.Path, "/delivery/testing") ||
		strings.HasPrefix(item.Path, "/delivery/analysis") ||
		strings.HasPrefix(item.Path, "/delivery/blueprints") ||
		strings.HasPrefix(item.Path, "/delivery/release-bundles") ||
		strings.HasPrefix(item.Path, "/delivery/execution-tasks") ||
		strings.HasPrefix(item.Path, "/workflow-templates") ||
		strings.HasPrefix(item.Path, "/release-board") ||
		strings.HasPrefix(item.Path, "/workflows") ||
		strings.HasPrefix(item.Path, "/releases") ||
		strings.HasPrefix(item.Path, "/registries")
}

func isMonitoringMenuSeed(item menuSeed) bool {
	return item.ID == "monitoring-workbench" ||
		strings.HasPrefix(item.Path, "/monitoring-workbench")
}

func isAIMenuSeed(item menuSeed) bool {
	return item.ID == "ai-workbench" ||
		strings.HasPrefix(item.Path, "/ai-workbench")
}

func isAIGatewayMenuSeed(item menuSeed) bool {
	return item.ID == "ai-gateway" ||
		strings.HasPrefix(item.Path, "/ai-gateway") ||
		strings.HasPrefix(item.Path, "/plugins")
}

func isVirtualizationMenuSeed(item menuSeed) bool {
	return item.ID == "virtualization-workbench" ||
		strings.HasPrefix(item.Path, "/virtualization")
}

func isDockerMenuSeed(item menuSeed) bool {
	return item.ID == "docker-workbench" ||
		strings.HasPrefix(item.Path, "/docker")
}

func upsertMenus(ctx context.Context, db *gorm.DB, items []menuSeed, now time.Time) error {
	if len(items) == 0 {
		return nil
	}
	var builder strings.Builder
	args := make([]any, 0, len(items)*11)
	builder.WriteString(`
		INSERT INTO menus (id, parent_id, path, label_zh, label_en, icon_key, section, sort_order, enabled, created_at, updated_at)
		VALUES
	`)
	for index, item := range items {
		if index > 0 {
			builder.WriteString(",")
		}
		builder.WriteString(" (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)")
		args = append(args, item.ID, nullableMenu(item.ParentID), item.Path, item.LabelZH, item.LabelEN, item.IconKey, item.Section, item.SortOrder, item.Enabled, now, now)
	}
	builder.WriteString(`
		ON CONFLICT (id) DO NOTHING
	`)
	return db.WithContext(ctx).Exec(builder.String(), args...).Error
}

func insertMenuRoleBindings(ctx context.Context, db *gorm.DB, values [][]string, now time.Time) error {
	var builder strings.Builder
	args := make([]any, 0, len(values)*5)
	builder.WriteString(`
		INSERT INTO menu_role_bindings (id, menu_id, role_id, created_at, updated_at)
		VALUES
	`)
	for index, value := range values {
		if index > 0 {
			builder.WriteString(",")
		}
		builder.WriteString(" (?, ?, ?, ?, ?)")
		args = append(args, value[0], value[1], value[2], now, now)
	}
	builder.WriteString(`
		ON CONFLICT (menu_id, role_id) DO UPDATE SET updated_at = EXCLUDED.updated_at
	`)
	return db.WithContext(ctx).Exec(builder.String(), args...).Error
}
