import { lazy, Suspense } from "react";
import type { ReactNode } from "react";
import { Navigate, useLocation, useParams } from "react-router-dom";
import { Spin } from "antd";
import {
  getAIModelSettingsPath,
  getAIOperationsPath,
  getAIToolsPath,
  getAIWorkbenchPathForMode,
} from "@/features/copilot/workbench-navigation";

function lazyNamed<T extends Record<string, any>, K extends keyof T>(
  importer: () => Promise<T>,
  key: K,
) {
  return lazy(async () => {
    const mod = await importer();
    return { default: mod[key] as any };
  });
}

export const LoginPage = lazyNamed(
  () => import("@/features/auth/login-page"),
  "LoginPage",
);
export const OIDCCallbackPage = lazyNamed(
  () => import("@/features/auth/oidc-callback-page"),
  "OIDCCallbackPage",
);
export const UserProfilePage = lazyNamed(
  () => import("@/features/auth/user-profile-page"),
  "UserProfilePage",
);

export const OverviewPage = lazyNamed(
  () => import("@/features/platform/overview-page"),
  "OverviewPage",
);
export const ClustersPage = lazyNamed(
  () => import("@/features/platform/clusters-page"),
  "ClustersPage",
);
export const ClusterDetailPage = lazyNamed(
  () => import("@/features/platform/clusters-page"),
  "ClusterDetailPage",
);
export const ClusterNodesPage = lazyNamed(
  () => import("@/features/platform/cluster-resources-pages"),
  "ClusterNodesPage",
);
export const ClusterNamespacesPage = lazyNamed(
  () => import("@/features/platform/cluster-resources-pages"),
  "ClusterNamespacesPage",
);
export const NodeDetailPage = lazyNamed(
  () => import("@/features/platform/node-detail-page"),
  "NodeDetailPage",
);

export const WorkloadsOverviewPage = lazyNamed(
  () => import("@/features/platform/workloads-pages"),
  "WorkloadsOverviewPage",
);
export const WorkloadsDeploymentsPage = lazyNamed(
  () => import("@/features/platform/workloads-pages"),
  "WorkloadsDeploymentsPage",
);
export const WorkloadsPodsPage = lazyNamed(
  () => import("@/features/platform/workloads-pages"),
  "WorkloadsPodsPage",
);
export const WorkloadsStatefulSetsPage = lazyNamed(
  () => import("@/features/platform/workloads-pages"),
  "WorkloadsStatefulSetsPage",
);
export const WorkloadsDaemonSetsPage = lazyNamed(
  () => import("@/features/platform/workloads-pages"),
  "WorkloadsDaemonSetsPage",
);
export const WorkloadsJobsPage = lazyNamed(
  () => import("@/features/platform/workloads-pages"),
  "WorkloadsJobsPage",
);
export const WorkloadsCronJobsPage = lazyNamed(
  () => import("@/features/platform/workloads-pages"),
  "WorkloadsCronJobsPage",
);
export const WorkloadsReplicaSetsPage = lazyNamed(
  () => import("@/features/platform/platform-management-pages"),
  "WorkloadsReplicaSetsPage",
);
export const WorkloadsReplicationControllersPage = lazyNamed(
  () => import("@/features/platform/platform-management-pages"),
  "WorkloadsReplicationControllersPage",
);
export const PodDetailPage = lazyNamed(
  () => import("@/features/platform/workloads-pages"),
  "PodDetailPage",
);
export const DeploymentDetailPage = lazyNamed(
  () => import("@/features/platform/workloads-pages"),
  "DeploymentDetailPage",
);
export const StatefulSetDetailPage = lazyNamed(
  () => import("@/features/platform/workloads-pages"),
  "StatefulSetDetailPage",
);
export const DaemonSetDetailPage = lazyNamed(
  () => import("@/features/platform/workloads-pages"),
  "DaemonSetDetailPage",
);
export const JobDetailPage = lazyNamed(
  () => import("@/features/platform/workloads-pages"),
  "JobDetailPage",
);
export const CronJobDetailPage = lazyNamed(
  () => import("@/features/platform/workloads-pages"),
  "CronJobDetailPage",
);

export const ConfigurationConfigMapsPage = lazyNamed(
  () => import("@/features/platform/platform-management-pages"),
  "ConfigurationConfigMapsPage",
);
export const ConfigurationSecretsPage = lazyNamed(
  () => import("@/features/platform/platform-management-pages"),
  "ConfigurationSecretsPage",
);
export const ConfigMapDetailPage = lazyNamed(
  () => import("@/features/platform/configuration-detail-pages"),
  "ConfigMapDetailPage",
);
export const SecretDetailPage = lazyNamed(
  () => import("@/features/platform/configuration-detail-pages"),
  "SecretDetailPage",
);
export const ConfigurationResourceQuotasPage = lazyNamed(
  () => import("@/features/platform/platform-management-pages"),
  "ConfigurationResourceQuotasPage",
);
export const ConfigurationLimitRangesPage = lazyNamed(
  () => import("@/features/platform/platform-management-pages"),
  "ConfigurationLimitRangesPage",
);
export const ConfigurationHPAPage = lazyNamed(
  () => import("@/features/platform/platform-management-pages"),
  "ConfigurationHPAPage",
);
export const ConfigurationPodDisruptionBudgetsPage = lazyNamed(
  () => import("@/features/platform/platform-management-pages"),
  "ConfigurationPodDisruptionBudgetsPage",
);
export const ConfigurationPriorityClassesPage = lazyNamed(
  () => import("@/features/platform/platform-management-pages"),
  "ConfigurationPriorityClassesPage",
);
export const ConfigurationRuntimeClassesPage = lazyNamed(
  () => import("@/features/platform/platform-management-pages"),
  "ConfigurationRuntimeClassesPage",
);
export const ConfigurationLeasesPage = lazyNamed(
  () => import("@/features/platform/platform-management-pages"),
  "ConfigurationLeasesPage",
);
export const ConfigurationMutatingWebhooksPage = lazyNamed(
  () => import("@/features/platform/platform-management-pages"),
  "ConfigurationMutatingWebhooksPage",
);
export const ConfigurationValidatingWebhooksPage = lazyNamed(
  () => import("@/features/platform/platform-management-pages"),
  "ConfigurationValidatingWebhooksPage",
);
export const PlatformAccessControlServiceAccountsPage = lazyNamed(
  () => import("@/features/platform/platform-management-pages"),
  "PlatformAccessControlServiceAccountsPage",
);
export const PlatformAccessControlClusterRolesPage = lazyNamed(
  () => import("@/features/platform/platform-management-pages"),
  "PlatformAccessControlClusterRolesPage",
);
export const PlatformAccessControlRolesPage = lazyNamed(
  () => import("@/features/platform/platform-management-pages"),
  "PlatformAccessControlRolesPage",
);
export const PlatformAccessControlClusterRoleBindingsPage = lazyNamed(
  () => import("@/features/platform/platform-management-pages"),
  "PlatformAccessControlClusterRoleBindingsPage",
);
export const PlatformAccessControlRoleBindingsPage = lazyNamed(
  () => import("@/features/platform/platform-management-pages"),
  "PlatformAccessControlRoleBindingsPage",
);
export const PlatformAccessControlServiceAccountDetailPage = lazyNamed(
  () => import("@/features/platform/rbac-detail-pages"),
  "PlatformAccessControlServiceAccountDetailPage",
);
export const PlatformAccessControlRoleDetailPage = lazyNamed(
  () => import("@/features/platform/rbac-detail-pages"),
  "PlatformAccessControlRoleDetailPage",
);
export const PlatformAccessControlRoleBindingDetailPage = lazyNamed(
  () => import("@/features/platform/rbac-detail-pages"),
  "PlatformAccessControlRoleBindingDetailPage",
);
export const PlatformAccessControlClusterRoleDetailPage = lazyNamed(
  () => import("@/features/platform/rbac-detail-pages"),
  "PlatformAccessControlClusterRoleDetailPage",
);
export const PlatformAccessControlClusterRoleBindingDetailPage = lazyNamed(
  () => import("@/features/platform/rbac-detail-pages"),
  "PlatformAccessControlClusterRoleBindingDetailPage",
);

export const NetworkServicesPage = lazyNamed(
  () => import("@/features/platform/network-storage-pages"),
  "NetworkServicesPage",
);
export const ServiceDetailPage = lazyNamed(
  () => import("@/features/platform/network-storage-pages"),
  "ServiceDetailPage",
);
export const NetworkIngressesPage = lazyNamed(
  () => import("@/features/platform/network-storage-pages"),
  "NetworkIngressesPage",
);
export const NetworkGatewayClassesPage = lazyNamed(
  () => import("@/features/platform/network-storage-pages"),
  "NetworkGatewayClassesPage",
);
export const NetworkGatewaysPage = lazyNamed(
  () => import("@/features/platform/network-storage-pages"),
  "NetworkGatewaysPage",
);
export const NetworkTopologyPage = lazyNamed(
  () => import("@/features/platform/network-topology-page"),
  "NetworkTopologyPage",
);
export const NetworkEndpointSlicesPage = lazyNamed(
  () => import("@/features/platform/network-storage-pages"),
  "NetworkEndpointSlicesPage",
);
export const NetworkIngressClassesPage = lazyNamed(
  () => import("@/features/platform/network-storage-pages"),
  "NetworkIngressClassesPage",
);
export const NetworkPoliciesPage = lazyNamed(
  () => import("@/features/platform/network-storage-pages"),
  "NetworkPoliciesPage",
);
export const NetworkPortForwardPage = lazyNamed(
  () => import("@/features/platform/platform-management-pages"),
  "NetworkPortForwardPage",
);
export const StoragePvcPage = lazyNamed(
  () => import("@/features/platform/network-storage-pages"),
  "StoragePvcPage",
);
export const StoragePvPage = lazyNamed(
  () => import("@/features/platform/network-storage-pages"),
  "StoragePvPage",
);
export const StorageClassesPage = lazyNamed(
  () => import("@/features/platform/network-storage-pages"),
  "StorageClassesPage",
);
export const StoragePvcDetailPage = lazyNamed(
  () => import("@/features/platform/network-storage-pages"),
  "StoragePvcDetailPage",
);
export const StoragePvDetailPage = lazyNamed(
  () => import("@/features/platform/network-storage-pages"),
  "StoragePvDetailPage",
);
export const StorageClassDetailPage = lazyNamed(
  () => import("@/features/platform/network-storage-pages"),
  "StorageClassDetailPage",
);

export const CRDPage = lazyNamed(
  () => import("@/features/platform/extensions-pages"),
  "CRDPage",
);
export const CRDApiGroupDetailPage = lazyNamed(
  () => import("@/features/platform/extensions-pages"),
  "CRDApiGroupDetailPage",
);
export const HelmReleasesPage = lazyNamed(
  () => import("@/features/platform/extensions-pages"),
  "HelmReleasesPage",
);
export const HelmReleaseDetailPage = lazyNamed(
  () => import("@/features/platform/extensions-pages"),
  "HelmReleaseDetailPage",
);
export const HelmChartsPage = lazyNamed(
  () => import("@/features/platform/extensions-pages"),
  "HelmChartsPage",
);

export const ApplicationsPage = lazyNamed(
  () => import("@/features/delivery/delivery-app-pages"),
  "ApplicationsPage",
);
export const ApplicationDetailPage = lazyNamed(
  () => import("@/features/delivery/application-runtime-pages"),
  "ApplicationDetailPage",
);
export const ApplicationWorkloadDetailPage = lazyNamed(
  () => import("@/features/delivery/application-runtime-pages"),
  "ApplicationWorkloadDetailPage",
);
export const BuildTemplatesPage = lazyNamed(
  () => import("@/features/delivery/delivery-app-pages"),
  "BuildTemplatesPage",
);
export const DeliveryBlueprintsPage = lazyNamed(
  () => import("@/features/delivery/delivery-blueprint-pages"),
  "DeliveryBlueprintsPage",
);
export const ReleaseBundlesPage = lazyNamed(
  () => import("@/features/delivery/delivery-app-pages"),
  "ReleaseBundlesPage",
);
export const ExecutionTasksPage = lazyNamed(
  () => import("@/features/delivery/delivery-app-pages"),
  "ExecutionTasksPage",
);
export const ApprovalPoliciesPage = lazyNamed(
  () => import("@/features/delivery/delivery-app-pages"),
  "ApprovalPoliciesPage",
);
export const ApplicationEnvironmentsPage = lazyNamed(
  () => import("@/features/delivery/delivery-catalog-pages"),
  "ApplicationEnvironmentsPage",
);
export const ApplicationEnvironmentDetailPage = lazyNamed(
  () => import("@/features/delivery/delivery-catalog-pages"),
  "ApplicationEnvironmentDetailPage",
);
export const WorkflowTemplatesPage = lazyNamed(
  () => import("@/features/delivery/delivery-catalog-pages"),
  "WorkflowTemplatesPage",
);
export const ReleaseBoardPage = lazyNamed(
  () => import("@/features/delivery/delivery-catalog-pages"),
  "ReleaseBoardPage",
);
export const WorkflowsPage = lazyNamed(
  () => import("@/features/delivery/delivery-app-pages"),
  "WorkflowsPage",
);
export const ReleasesPage = lazyNamed(
  () => import("@/features/delivery/delivery-pages"),
  "ReleasesPage",
);
export const RegistriesPage = lazyNamed(
  () => import("@/features/delivery/delivery-pages"),
  "RegistriesPage",
);

export const VirtualizationOverviewPage = lazyNamed(
  () => import("@/features/virtualization/virtualization-pages"),
  "VirtualizationOverviewPage",
);
export const VirtualizationVmsPage = lazyNamed(
  () => import("@/features/virtualization/virtualization-pages"),
  "VirtualizationVmsPage",
);
export const VirtualizationVmDetailPage = lazyNamed(
  () => import("@/features/virtualization/virtualization-pages"),
  "VirtualizationVmDetailPage",
);
export const VirtualizationClustersPage = lazyNamed(
  () => import("@/features/virtualization/virtualization-pages"),
  "VirtualizationClustersPage",
);
export const VirtualizationImagesPage = lazyNamed(
  () => import("@/features/virtualization/virtualization-pages"),
  "VirtualizationImagesPage",
);
export const VirtualizationFlavorsPage = lazyNamed(
  () => import("@/features/virtualization/virtualization-pages"),
  "VirtualizationFlavorsPage",
);
export const VirtualizationOperationsPage = lazyNamed(
  () => import("@/features/virtualization/virtualization-pages"),
  "VirtualizationOperationsPage",
);
export const VirtualizationSyncPage = lazyNamed(
  () => import("@/features/virtualization/virtualization-pages"),
  "VirtualizationSyncPage",
);

export const DockerOverviewPage = lazyNamed(
  () => import("@/features/docker/docker-pages"),
  "DockerOverviewPage",
);
export const DockerHostsPage = lazyNamed(
  () => import("@/features/docker/docker-pages"),
  "DockerHostsPage",
);
export const DockerProjectsPage = lazyNamed(
  () => import("@/features/docker/docker-pages"),
  "DockerProjectsPage",
);
export const DockerProjectDetailPage = lazyNamed(
  () => import("@/features/docker/docker-pages"),
  "DockerProjectDetailPage",
);
export const DockerServicesPage = lazyNamed(
  () => import("@/features/docker/docker-pages"),
  "DockerServicesPage",
);
export const DockerPortsPage = lazyNamed(
  () => import("@/features/docker/docker-pages"),
  "DockerPortsPage",
);
export const DockerTemplatesPage = lazyNamed(
  () => import("@/features/docker/docker-pages"),
  "DockerTemplatesPage",
);
export const DockerOperationsPage = lazyNamed(
  () => import("@/features/docker/docker-pages"),
  "DockerOperationsPage",
);

export const MonitoringPage = lazyNamed(
  () => import("@/features/observability/monitoring-pages"),
  "MonitoringPage",
);
export const AlertIntegrationsPage = lazyNamed(
  () => import("@/features/observability/monitoring-pages"),
  "AlertIntegrationsPage",
);
export const AlertsPage = lazyNamed(
  () => import("@/features/observability/monitoring-pages"),
  "AlertsPage",
);
export const NotificationsPage = lazyNamed(
  () => import("@/features/observability/monitoring-pages"),
  "NotificationsPage",
);
export const EventsPage = lazyNamed(
  () => import("@/features/observability/monitoring-pages"),
  "EventsPage",
);
export const AlertRulesPage = lazyNamed(
  () => import("@/features/observability/alerting-pages"),
  "AlertRulesPage",
);
export const HealingPage = lazyNamed(
  () => import("@/features/observability/alerting-pages"),
  "HealingPage",
);
export const OnCallBoardPage = lazyNamed(
  () => import("@/features/observability/alerting-pages"),
  "OnCallBoardPage",
);
export const OnCallSettingsPage = lazyNamed(
  () => import("@/features/observability/alerting-pages"),
  "OnCallSettingsPage",
);
export const AlertEventDetailPage = lazyNamed(
  () => import("@/features/observability/alerting-pages"),
  "AlertEventDetailPage",
);

export const AIWorkbenchPage = lazyNamed(
  () => import("@/features/copilot/ai-observe-pages"),
  "AIWorkbenchPage",
);
export const AIOperationsPage = lazyNamed(
  () => import("@/features/copilot/ai-observe-pages"),
  "AIOperationsPage",
);
export const AIToolsPage = lazyNamed(
  () => import("@/features/copilot/ai-observe-pages"),
  "AIToolsPage",
);
export const AIModelSettingsPage = lazyNamed(
  () => import("@/features/copilot/ai-observe-pages"),
  "AIModelSettingsPage",
);
export const AIGatewayPage = lazyNamed(
  () => import("@/features/copilot/ai-gateway-page"),
  "AIGatewayPage",
);

export const AccessCenterPage = lazyNamed(
  () => import("@/features/access/access-pages"),
  "AccessCenterPage",
);
export const AccessUsersPage = lazyNamed(
  () => import("@/features/access/access-pages"),
  "AccessUsersPage",
);
export const AccessRolesPage = lazyNamed(
  () => import("@/features/access/access-pages"),
  "AccessRolesPage",
);
export const AccessTeamsPage = lazyNamed(
  () => import("@/features/access/access-pages"),
  "AccessTeamsPage",
);
export const AccessPoliciesPage = lazyNamed(
  () => import("@/features/access/access-pages"),
  "AccessPoliciesPage",
);
export const AccessScopeGrantsPage = lazyNamed(
  () => import("@/features/access/scope-grants-page"),
  "AccessScopeGrantsPage",
);

export const OnlineUsersPage = lazyNamed(
  () => import("@/features/system/system-pages"),
  "OnlineUsersPage",
);
export const AnnouncementsPage = lazyNamed(
  () => import("@/features/system/system-pages"),
  "AnnouncementsPage",
);
export const MenusPage = lazyNamed(
  () => import("@/features/system/system-pages"),
  "MenusPage",
);
export const AuditLogsPage = lazyNamed(
  () => import("@/features/system/system-pages"),
  "AuditLogsPage",
);
export const OperationLogsPage = lazyNamed(
  () => import("@/features/system/system-pages"),
  "OperationLogsPage",
);

export const SettingsCenterPage = lazyNamed(
  () => import("@/features/settings/settings-pages"),
  "SettingsCenterPage",
);

export function LazyPage({ children }: { children: ReactNode }) {
  return (
    <Suspense
      fallback={
        <div className="flex items-center justify-center h-full py-20">
          <Spin size="large" />
        </div>
      }
    >
      {children}
    </Suspense>
  );
}

export function ApplicationManagementRedirect() {
  const { applicationId } = useParams();
  return <Navigate to={applicationId ? `/applications/${applicationId}` : "/applications"} replace />;
}

export function AIWorkbenchModeRedirect() {
  const location = useLocation();
  return (
    <Navigate
      to={getAIWorkbenchPathForMode(new URLSearchParams(location.search).get("mode"), location.search)}
      replace
    />
  );
}

export function AIWorkbenchFixedModeRedirect({ mode }: { mode: string }) {
  const location = useLocation();
  return <Navigate to={getAIWorkbenchPathForMode(mode, location.search)} replace />;
}

export function AIWorkbenchOperationsRedirect() {
  const location = useLocation();
  return <Navigate to={getAIOperationsPath(location.search)} replace />;
}

export function AIWorkbenchToolsRedirect() {
  const location = useLocation();
  return <Navigate to={getAIToolsPath(location.search)} replace />;
}

export function AIWorkbenchModelSettingsRedirect() {
  const location = useLocation();
  return <Navigate to={getAIModelSettingsPath(location.search)} replace />;
}

const AI_GATEWAY_TAB_PATHS: Record<string, string> = {
  overview: "/ai-gateway/overview",
  manifest: "/ai-gateway/manifest",
  clients: "/ai-gateway/clients",
  tokens: "/ai-gateway/tokens",
  "service-accounts": "/ai-gateway/tokens",
  grants: "/ai-gateway/governance",
  policies: "/ai-gateway/governance",
  bindings: "/ai-gateway/governance",
  governance: "/ai-gateway/governance",
  approvals: "/ai-gateway/governance",
  audit: "/ai-gateway/call-logs",
  "call-logs": "/ai-gateway/call-logs",
};

function getAIGatewayRedirectTarget(search: string) {
  const params = new URLSearchParams(search);
  const requestedTab = params.get("tab")?.trim() ?? "";
  const hasApprovalFocus = Boolean(params.get("approvalRequestId")?.trim());
  const targetPath = AI_GATEWAY_TAB_PATHS[requestedTab] ?? (hasApprovalFocus ? "/ai-gateway/governance" : "/ai-gateway/overview");
  if (["overview", "manifest", "clients", "tokens", "governance", "call-logs"].includes(requestedTab)) {
    params.delete("tab");
  }
  const suffix = params.toString();
  return `${targetPath}${suffix ? `?${suffix}` : ""}`;
}

export function AIGatewayRedirect() {
  const location = useLocation();
  return <Navigate to={getAIGatewayRedirectTarget(location.search)} replace />;
}
