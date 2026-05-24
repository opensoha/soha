import { lazy, Suspense } from "react";
import { Navigate, Route, Routes } from "react-router-dom";
import { Spin } from "antd";
import { AuthGuard } from "@/features/auth/auth-guard";
import { AppLayout } from "@/layouts/app-layout";

function lazyNamed<T extends Record<string, any>, K extends keyof T>(
  importer: () => Promise<T>,
  key: K,
) {
  return lazy(async () => {
    const mod = await importer();
    return { default: mod[key] as any };
  });
}

const LoginPage = lazyNamed(
  () => import("@/features/auth/login-page"),
  "LoginPage",
);
const OIDCCallbackPage = lazyNamed(
  () => import("@/features/auth/oidc-callback-page"),
  "OIDCCallbackPage",
);

const OverviewPage = lazyNamed(
  () => import("@/features/platform/overview-page"),
  "OverviewPage",
);
const ClustersPage = lazyNamed(
  () => import("@/features/platform/clusters-page"),
  "ClustersPage",
);
const ClusterDetailPage = lazyNamed(
  () => import("@/features/platform/clusters-page"),
  "ClusterDetailPage",
);
const ClusterNodesPage = lazyNamed(
  () => import("@/features/platform/cluster-resources-pages"),
  "ClusterNodesPage",
);
const ClusterNamespacesPage = lazyNamed(
  () => import("@/features/platform/cluster-resources-pages"),
  "ClusterNamespacesPage",
);
const NodeDetailPage = lazyNamed(
  () => import("@/features/platform/node-detail-page"),
  "NodeDetailPage",
);

const WorkloadsOverviewPage = lazyNamed(
  () => import("@/features/platform/workloads-pages"),
  "WorkloadsOverviewPage",
);
const WorkloadsDeploymentsPage = lazyNamed(
  () => import("@/features/platform/workloads-pages"),
  "WorkloadsDeploymentsPage",
);
const WorkloadsPodsPage = lazyNamed(
  () => import("@/features/platform/workloads-pages"),
  "WorkloadsPodsPage",
);
const WorkloadsStatefulSetsPage = lazyNamed(
  () => import("@/features/platform/workloads-pages"),
  "WorkloadsStatefulSetsPage",
);
const WorkloadsDaemonSetsPage = lazyNamed(
  () => import("@/features/platform/workloads-pages"),
  "WorkloadsDaemonSetsPage",
);
const WorkloadsJobsPage = lazyNamed(
  () => import("@/features/platform/workloads-pages"),
  "WorkloadsJobsPage",
);
const WorkloadsCronJobsPage = lazyNamed(
  () => import("@/features/platform/workloads-pages"),
  "WorkloadsCronJobsPage",
);
const WorkloadsReplicaSetsPage = lazyNamed(
  () => import("@/features/platform/platform-management-pages"),
  "WorkloadsReplicaSetsPage",
);
const WorkloadsReplicationControllersPage = lazyNamed(
  () => import("@/features/platform/platform-management-pages"),
  "WorkloadsReplicationControllersPage",
);
const PodDetailPage = lazyNamed(
  () => import("@/features/platform/workloads-pages"),
  "PodDetailPage",
);
const DeploymentDetailPage = lazyNamed(
  () => import("@/features/platform/workloads-pages"),
  "DeploymentDetailPage",
);
const StatefulSetDetailPage = lazyNamed(
  () => import("@/features/platform/workloads-pages"),
  "StatefulSetDetailPage",
);
const DaemonSetDetailPage = lazyNamed(
  () => import("@/features/platform/workloads-pages"),
  "DaemonSetDetailPage",
);
const JobDetailPage = lazyNamed(
  () => import("@/features/platform/workloads-pages"),
  "JobDetailPage",
);
const CronJobDetailPage = lazyNamed(
  () => import("@/features/platform/workloads-pages"),
  "CronJobDetailPage",
);

const ConfigurationConfigMapsPage = lazyNamed(
  () => import("@/features/platform/platform-management-pages"),
  "ConfigurationConfigMapsPage",
);
const ConfigurationSecretsPage = lazyNamed(
  () => import("@/features/platform/platform-management-pages"),
  "ConfigurationSecretsPage",
);
const ConfigMapDetailPage = lazyNamed(
  () => import("@/features/platform/configuration-detail-pages"),
  "ConfigMapDetailPage",
);
const SecretDetailPage = lazyNamed(
  () => import("@/features/platform/configuration-detail-pages"),
  "SecretDetailPage",
);
const ConfigurationResourceQuotasPage = lazyNamed(
  () => import("@/features/platform/platform-management-pages"),
  "ConfigurationResourceQuotasPage",
);
const ConfigurationLimitRangesPage = lazyNamed(
  () => import("@/features/platform/platform-management-pages"),
  "ConfigurationLimitRangesPage",
);
const ConfigurationHPAPage = lazyNamed(
  () => import("@/features/platform/platform-management-pages"),
  "ConfigurationHPAPage",
);
const ConfigurationPodDisruptionBudgetsPage = lazyNamed(
  () => import("@/features/platform/platform-management-pages"),
  "ConfigurationPodDisruptionBudgetsPage",
);
const ConfigurationPriorityClassesPage = lazyNamed(
  () => import("@/features/platform/platform-management-pages"),
  "ConfigurationPriorityClassesPage",
);
const ConfigurationRuntimeClassesPage = lazyNamed(
  () => import("@/features/platform/platform-management-pages"),
  "ConfigurationRuntimeClassesPage",
);
const ConfigurationLeasesPage = lazyNamed(
  () => import("@/features/platform/platform-management-pages"),
  "ConfigurationLeasesPage",
);
const ConfigurationMutatingWebhooksPage = lazyNamed(
  () => import("@/features/platform/platform-management-pages"),
  "ConfigurationMutatingWebhooksPage",
);
const ConfigurationValidatingWebhooksPage = lazyNamed(
  () => import("@/features/platform/platform-management-pages"),
  "ConfigurationValidatingWebhooksPage",
);
const PlatformAccessControlServiceAccountsPage = lazyNamed(
  () => import("@/features/platform/platform-management-pages"),
  "PlatformAccessControlServiceAccountsPage",
);
const PlatformAccessControlClusterRolesPage = lazyNamed(
  () => import("@/features/platform/platform-management-pages"),
  "PlatformAccessControlClusterRolesPage",
);
const PlatformAccessControlRolesPage = lazyNamed(
  () => import("@/features/platform/platform-management-pages"),
  "PlatformAccessControlRolesPage",
);
const PlatformAccessControlClusterRoleBindingsPage = lazyNamed(
  () => import("@/features/platform/platform-management-pages"),
  "PlatformAccessControlClusterRoleBindingsPage",
);
const PlatformAccessControlRoleBindingsPage = lazyNamed(
  () => import("@/features/platform/platform-management-pages"),
  "PlatformAccessControlRoleBindingsPage",
);
const PlatformAccessControlServiceAccountDetailPage = lazyNamed(
  () => import("@/features/platform/rbac-detail-pages"),
  "PlatformAccessControlServiceAccountDetailPage",
);
const PlatformAccessControlRoleDetailPage = lazyNamed(
  () => import("@/features/platform/rbac-detail-pages"),
  "PlatformAccessControlRoleDetailPage",
);
const PlatformAccessControlRoleBindingDetailPage = lazyNamed(
  () => import("@/features/platform/rbac-detail-pages"),
  "PlatformAccessControlRoleBindingDetailPage",
);
const PlatformAccessControlClusterRoleDetailPage = lazyNamed(
  () => import("@/features/platform/rbac-detail-pages"),
  "PlatformAccessControlClusterRoleDetailPage",
);
const PlatformAccessControlClusterRoleBindingDetailPage = lazyNamed(
  () => import("@/features/platform/rbac-detail-pages"),
  "PlatformAccessControlClusterRoleBindingDetailPage",
);

const NetworkServicesPage = lazyNamed(
  () => import("@/features/platform/network-storage-pages"),
  "NetworkServicesPage",
);
const ServiceDetailPage = lazyNamed(
  () => import("@/features/platform/network-storage-pages"),
  "ServiceDetailPage",
);
const NetworkIngressesPage = lazyNamed(
  () => import("@/features/platform/network-storage-pages"),
  "NetworkIngressesPage",
);
const NetworkGatewaysPage = lazyNamed(
  () => import("@/features/platform/network-storage-pages"),
  "NetworkGatewaysPage",
);
const NetworkTopologyPage = lazyNamed(
  () => import("@/features/platform/network-topology-page"),
  "NetworkTopologyPage",
);
const NetworkEndpointSlicesPage = lazyNamed(
  () => import("@/features/platform/platform-management-pages"),
  "NetworkEndpointSlicesPage",
);
const NetworkIngressClassesPage = lazyNamed(
  () => import("@/features/platform/platform-management-pages"),
  "NetworkIngressClassesPage",
);
const NetworkPoliciesPage = lazyNamed(
  () => import("@/features/platform/platform-management-pages"),
  "NetworkPoliciesPage",
);
const NetworkPortForwardPage = lazyNamed(
  () => import("@/features/platform/platform-management-pages"),
  "NetworkPortForwardPage",
);
const StoragePvcPage = lazyNamed(
  () => import("@/features/platform/network-storage-pages"),
  "StoragePvcPage",
);
const StoragePvPage = lazyNamed(
  () => import("@/features/platform/network-storage-pages"),
  "StoragePvPage",
);
const StorageClassesPage = lazyNamed(
  () => import("@/features/platform/network-storage-pages"),
  "StorageClassesPage",
);
const StoragePvcDetailPage = lazyNamed(
  () => import("@/features/platform/network-storage-pages"),
  "StoragePvcDetailPage",
);
const StoragePvDetailPage = lazyNamed(
  () => import("@/features/platform/network-storage-pages"),
  "StoragePvDetailPage",
);
const StorageClassDetailPage = lazyNamed(
  () => import("@/features/platform/network-storage-pages"),
  "StorageClassDetailPage",
);

const CRDPage = lazyNamed(
  () => import("@/features/platform/extensions-pages"),
  "CRDPage",
);
const CRDApiGroupDetailPage = lazyNamed(
  () => import("@/features/platform/extensions-pages"),
  "CRDApiGroupDetailPage",
);
const HelmReleasesPage = lazyNamed(
  () => import("@/features/platform/extensions-pages"),
  "HelmReleasesPage",
);
const HelmReleaseDetailPage = lazyNamed(
  () => import("@/features/platform/extensions-pages"),
  "HelmReleaseDetailPage",
);
const HelmChartsPage = lazyNamed(
  () => import("@/features/platform/extensions-pages"),
  "HelmChartsPage",
);

const ApplicationsPage = lazyNamed(
  () => import("@/features/delivery/delivery-app-pages"),
  "ApplicationsPage",
);
const ApplicationManagementPage = lazyNamed(
  () => import("@/features/delivery/application-management-pages"),
  "ApplicationManagementPage",
);
const ApplicationManagementDetailPage = lazyNamed(
  () => import("@/features/delivery/application-management-pages"),
  "ApplicationManagementDetailPage",
);
const ApplicationDetailPage = lazyNamed(
  () => import("@/features/delivery/application-runtime-pages"),
  "ApplicationDetailPage",
);
const ApplicationWorkloadDetailPage = lazyNamed(
  () => import("@/features/delivery/application-runtime-pages"),
  "ApplicationWorkloadDetailPage",
);
const BuildTemplatesPage = lazyNamed(
  () => import("@/features/delivery/delivery-app-pages"),
  "BuildTemplatesPage",
);
const DeliveryBlueprintsPage = lazyNamed(
  () => import("@/features/delivery/delivery-blueprint-pages"),
  "DeliveryBlueprintsPage",
);
const ReleaseBundlesPage = lazyNamed(
  () => import("@/features/delivery/delivery-app-pages"),
  "ReleaseBundlesPage",
);
const ExecutionTasksPage = lazyNamed(
  () => import("@/features/delivery/delivery-app-pages"),
  "ExecutionTasksPage",
);
const ApprovalPoliciesPage = lazyNamed(
  () => import("@/features/delivery/delivery-app-pages"),
  "ApprovalPoliciesPage",
);
const BusinessLinesPage = lazyNamed(
  () => import("@/features/delivery/delivery-catalog-pages"),
  "BusinessLinesPage",
);
const DeliveryEnvironmentsPage = lazyNamed(
  () => import("@/features/delivery/delivery-catalog-pages"),
  "DeliveryEnvironmentsPage",
);
const ApplicationEnvironmentsPage = lazyNamed(
  () => import("@/features/delivery/delivery-catalog-pages"),
  "ApplicationEnvironmentsPage",
);
const ApplicationEnvironmentDetailPage = lazyNamed(
  () => import("@/features/delivery/delivery-catalog-pages"),
  "ApplicationEnvironmentDetailPage",
);
const WorkflowTemplatesPage = lazyNamed(
  () => import("@/features/delivery/delivery-catalog-pages"),
  "WorkflowTemplatesPage",
);
const ReleaseBoardPage = lazyNamed(
  () => import("@/features/delivery/delivery-catalog-pages"),
  "ReleaseBoardPage",
);
const WorkflowsPage = lazyNamed(
  () => import("@/features/delivery/delivery-app-pages"),
  "WorkflowsPage",
);
const ReleasesPage = lazyNamed(
  () => import("@/features/delivery/delivery-pages"),
  "ReleasesPage",
);
const RegistriesPage = lazyNamed(
  () => import("@/features/delivery/delivery-pages"),
  "RegistriesPage",
);

const VirtualizationOverviewPage = lazyNamed(
  () => import("@/features/virtualization/virtualization-pages"),
  "VirtualizationOverviewPage",
);
const VirtualizationVmsPage = lazyNamed(
  () => import("@/features/virtualization/virtualization-pages"),
  "VirtualizationVmsPage",
);
const VirtualizationVmDetailPage = lazyNamed(
  () => import("@/features/virtualization/virtualization-pages"),
  "VirtualizationVmDetailPage",
);
const VirtualizationClustersPage = lazyNamed(
  () => import("@/features/virtualization/virtualization-pages"),
  "VirtualizationClustersPage",
);
const VirtualizationImagesPage = lazyNamed(
  () => import("@/features/virtualization/virtualization-pages"),
  "VirtualizationImagesPage",
);
const VirtualizationFlavorsPage = lazyNamed(
  () => import("@/features/virtualization/virtualization-pages"),
  "VirtualizationFlavorsPage",
);
const VirtualizationOperationsPage = lazyNamed(
  () => import("@/features/virtualization/virtualization-pages"),
  "VirtualizationOperationsPage",
);
const VirtualizationSyncPage = lazyNamed(
  () => import("@/features/virtualization/virtualization-pages"),
  "VirtualizationSyncPage",
);

const DockerOverviewPage = lazyNamed(
  () => import("@/features/docker/docker-pages"),
  "DockerOverviewPage",
);
const DockerHostsPage = lazyNamed(
  () => import("@/features/docker/docker-pages"),
  "DockerHostsPage",
);
const DockerProjectsPage = lazyNamed(
  () => import("@/features/docker/docker-pages"),
  "DockerProjectsPage",
);
const DockerServicesPage = lazyNamed(
  () => import("@/features/docker/docker-pages"),
  "DockerServicesPage",
);
const DockerPortsPage = lazyNamed(
  () => import("@/features/docker/docker-pages"),
  "DockerPortsPage",
);
const DockerTemplatesPage = lazyNamed(
  () => import("@/features/docker/docker-pages"),
  "DockerTemplatesPage",
);
const DockerOperationsPage = lazyNamed(
  () => import("@/features/docker/docker-pages"),
  "DockerOperationsPage",
);

const MonitoringPage = lazyNamed(
  () => import("@/features/observability/monitoring-pages"),
  "MonitoringPage",
);
const AlertsPage = lazyNamed(
  () => import("@/features/observability/monitoring-pages"),
  "AlertsPage",
);
const NotificationsPage = lazyNamed(
  () => import("@/features/observability/monitoring-pages"),
  "NotificationsPage",
);
const EventsPage = lazyNamed(
  () => import("@/features/observability/monitoring-pages"),
  "EventsPage",
);
const AlertRulesPage = lazyNamed(
  () => import("@/features/observability/alerting-pages"),
  "AlertRulesPage",
);
const HealingPage = lazyNamed(
  () => import("@/features/observability/alerting-pages"),
  "HealingPage",
);
const OnCallBoardPage = lazyNamed(
  () => import("@/features/observability/alerting-pages"),
  "OnCallBoardPage",
);
const OnCallSettingsPage = lazyNamed(
  () => import("@/features/observability/alerting-pages"),
  "OnCallSettingsPage",
);
const AlertEventDetailPage = lazyNamed(
  () => import("@/features/observability/alerting-pages"),
  "AlertEventDetailPage",
);

const AIWorkbenchPage = lazyNamed(
  () => import("@/features/copilot/ai-observe-pages"),
  "AIWorkbenchPage",
);
const AIOperationsPage = lazyNamed(
  () => import("@/features/copilot/ai-observe-pages"),
  "AIOperationsPage",
);
const AIToolsPage = lazyNamed(
  () => import("@/features/copilot/ai-observe-pages"),
  "AIToolsPage",
);
const AIModelSettingsPage = lazyNamed(
  () => import("@/features/copilot/ai-observe-pages"),
  "AIModelSettingsPage",
);

const AccessCenterPage = lazyNamed(
  () => import("@/features/access/access-pages"),
  "AccessCenterPage",
);
const AccessUsersPage = lazyNamed(
  () => import("@/features/access/access-pages"),
  "AccessUsersPage",
);
const AccessRolesPage = lazyNamed(
  () => import("@/features/access/access-pages"),
  "AccessRolesPage",
);
const AccessTeamsPage = lazyNamed(
  () => import("@/features/access/access-pages"),
  "AccessTeamsPage",
);
const AccessPoliciesPage = lazyNamed(
  () => import("@/features/access/access-pages"),
  "AccessPoliciesPage",
);
const AccessScopeGrantsPage = lazyNamed(
  () => import("@/features/access/scope-grants-page"),
  "AccessScopeGrantsPage",
);

const OnlineUsersPage = lazyNamed(
  () => import("@/features/system/system-pages"),
  "OnlineUsersPage",
);
const AnnouncementsPage = lazyNamed(
  () => import("@/features/system/system-pages"),
  "AnnouncementsPage",
);
const MenusPage = lazyNamed(
  () => import("@/features/system/system-pages"),
  "MenusPage",
);
const AuditLogsPage = lazyNamed(
  () => import("@/features/system/system-pages"),
  "AuditLogsPage",
);
const OperationLogsPage = lazyNamed(
  () => import("@/features/system/system-pages"),
  "OperationLogsPage",
);

const SettingsCenterPage = lazyNamed(
  () => import("@/features/settings/settings-pages"),
  "SettingsCenterPage",
);

function LazyPage({ children }: { children: React.ReactNode }) {
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

export function AppRouter() {
  return (
    <Routes>
      <Route
        path="/login"
        element={
          <LazyPage>
            <LoginPage />
          </LazyPage>
        }
      />
      <Route
        path="/auth/oidc/callback"
        element={
          <LazyPage>
            <OIDCCallbackPage />
          </LazyPage>
        }
      />
      <Route
        path="/login/callback"
        element={
          <LazyPage>
            <OIDCCallbackPage />
          </LazyPage>
        }
      />
      <Route element={<AuthGuard />}>
        <Route element={<AppLayout />}>
          <Route
            path="/"
            element={
              <LazyPage>
                <OverviewPage />
              </LazyPage>
            }
          />
          <Route
            path="/clusters"
            element={
              <LazyPage>
                <ClustersPage />
              </LazyPage>
            }
          />
          <Route
            path="/clusters/:clusterId"
            element={
              <LazyPage>
                <ClusterDetailPage />
              </LazyPage>
            }
          />
          <Route
            path="/cluster-resources"
            element={<Navigate to="/cluster-resources/nodes" replace />}
          />
          <Route
            path="/cluster-resources/nodes"
            element={
              <LazyPage>
                <ClusterNodesPage />
              </LazyPage>
            }
          />
          <Route
            path="/cluster-resources/nodes/:nodeName"
            element={
              <LazyPage>
                <NodeDetailPage />
              </LazyPage>
            }
          />
          <Route
            path="/cluster-resources/namespaces"
            element={
              <LazyPage>
                <ClusterNamespacesPage />
              </LazyPage>
            }
          />

          <Route
            path="/workloads"
            element={<Navigate to="/workloads/overview" replace />}
          />
          <Route
            path="/workloads/overview"
            element={
              <LazyPage>
                <WorkloadsOverviewPage />
              </LazyPage>
            }
          />
          <Route
            path="/workloads/deployments"
            element={
              <LazyPage>
                <WorkloadsDeploymentsPage />
              </LazyPage>
            }
          />
          <Route
            path="/workloads/pods"
            element={
              <LazyPage>
                <WorkloadsPodsPage />
              </LazyPage>
            }
          />
          <Route
            path="/workloads/statefulsets"
            element={
              <LazyPage>
                <WorkloadsStatefulSetsPage />
              </LazyPage>
            }
          />
          <Route
            path="/workloads/daemonsets"
            element={
              <LazyPage>
                <WorkloadsDaemonSetsPage />
              </LazyPage>
            }
          />
          <Route
            path="/workloads/jobs"
            element={
              <LazyPage>
                <WorkloadsJobsPage />
              </LazyPage>
            }
          />
          <Route
            path="/workloads/cronjobs"
            element={
              <LazyPage>
                <WorkloadsCronJobsPage />
              </LazyPage>
            }
          />
          <Route
            path="/workloads/replicasets"
            element={
              <LazyPage>
                <WorkloadsReplicaSetsPage />
              </LazyPage>
            }
          />
          <Route
            path="/workloads/replicationcontrollers"
            element={
              <LazyPage>
                <WorkloadsReplicationControllersPage />
              </LazyPage>
            }
          />
          <Route
            path="/workloads/pods/:podName"
            element={
              <LazyPage>
                <PodDetailPage />
              </LazyPage>
            }
          />
          <Route
            path="/workloads/deployments/:deploymentName"
            element={
              <LazyPage>
                <DeploymentDetailPage />
              </LazyPage>
            }
          />
          <Route
            path="/workloads/statefulsets/:statefulSetName"
            element={
              <LazyPage>
                <StatefulSetDetailPage />
              </LazyPage>
            }
          />
          <Route
            path="/workloads/daemonsets/:daemonSetName"
            element={
              <LazyPage>
                <DaemonSetDetailPage />
              </LazyPage>
            }
          />
          <Route
            path="/workloads/jobs/:jobName"
            element={
              <LazyPage>
                <JobDetailPage />
              </LazyPage>
            }
          />
          <Route
            path="/workloads/cronjobs/:cronJobName"
            element={
              <LazyPage>
                <CronJobDetailPage />
              </LazyPage>
            }
          />

          <Route
            path="/configuration"
            element={<Navigate to="/configuration/configmaps" replace />}
          />
          <Route
            path="/configuration/configmaps"
            element={
              <LazyPage>
                <ConfigurationConfigMapsPage />
              </LazyPage>
            }
          />
          <Route
            path="/configuration/configmaps/:configMapName"
            element={
              <LazyPage>
                <ConfigMapDetailPage />
              </LazyPage>
            }
          />
          <Route
            path="/configuration/secrets"
            element={
              <LazyPage>
                <ConfigurationSecretsPage />
              </LazyPage>
            }
          />
          <Route
            path="/configuration/secrets/:secretName"
            element={
              <LazyPage>
                <SecretDetailPage />
              </LazyPage>
            }
          />
          <Route
            path="/configuration/resourcequotas"
            element={
              <LazyPage>
                <ConfigurationResourceQuotasPage />
              </LazyPage>
            }
          />
          <Route
            path="/configuration/limitranges"
            element={
              <LazyPage>
                <ConfigurationLimitRangesPage />
              </LazyPage>
            }
          />
          <Route
            path="/configuration/hpas"
            element={
              <LazyPage>
                <ConfigurationHPAPage />
              </LazyPage>
            }
          />
          <Route
            path="/configuration/poddisruptionbudgets"
            element={
              <LazyPage>
                <ConfigurationPodDisruptionBudgetsPage />
              </LazyPage>
            }
          />
          <Route
            path="/configuration/priorityclasses"
            element={
              <LazyPage>
                <ConfigurationPriorityClassesPage />
              </LazyPage>
            }
          />
          <Route
            path="/configuration/runtimeclasses"
            element={
              <LazyPage>
                <ConfigurationRuntimeClassesPage />
              </LazyPage>
            }
          />
          <Route
            path="/configuration/leases"
            element={
              <LazyPage>
                <ConfigurationLeasesPage />
              </LazyPage>
            }
          />
          <Route
            path="/configuration/mutatingwebhookconfigurations"
            element={
              <LazyPage>
                <ConfigurationMutatingWebhooksPage />
              </LazyPage>
            }
          />
          <Route
            path="/configuration/validatingwebhookconfigurations"
            element={
              <LazyPage>
                <ConfigurationValidatingWebhooksPage />
              </LazyPage>
            }
          />

          <Route
            path="/platform-access-control"
            element={
              <Navigate to="/platform-access-control/serviceaccounts" replace />
            }
          />
          <Route
            path="/platform-access-control/serviceaccounts"
            element={
              <LazyPage>
                <PlatformAccessControlServiceAccountsPage />
              </LazyPage>
            }
          />
          <Route
            path="/platform-access-control/serviceaccounts/:name"
            element={
              <LazyPage>
                <PlatformAccessControlServiceAccountDetailPage />
              </LazyPage>
            }
          />
          <Route
            path="/platform-access-control/clusterroles"
            element={
              <LazyPage>
                <PlatformAccessControlClusterRolesPage />
              </LazyPage>
            }
          />
          <Route
            path="/platform-access-control/clusterroles/:name"
            element={
              <LazyPage>
                <PlatformAccessControlClusterRoleDetailPage />
              </LazyPage>
            }
          />
          <Route
            path="/platform-access-control/roles"
            element={
              <LazyPage>
                <PlatformAccessControlRolesPage />
              </LazyPage>
            }
          />
          <Route
            path="/platform-access-control/roles/:name"
            element={
              <LazyPage>
                <PlatformAccessControlRoleDetailPage />
              </LazyPage>
            }
          />
          <Route
            path="/platform-access-control/clusterrolebindings"
            element={
              <LazyPage>
                <PlatformAccessControlClusterRoleBindingsPage />
              </LazyPage>
            }
          />
          <Route
            path="/platform-access-control/clusterrolebindings/:name"
            element={
              <LazyPage>
                <PlatformAccessControlClusterRoleBindingDetailPage />
              </LazyPage>
            }
          />
          <Route
            path="/platform-access-control/rolebindings"
            element={
              <LazyPage>
                <PlatformAccessControlRoleBindingsPage />
              </LazyPage>
            }
          />
          <Route
            path="/platform-access-control/rolebindings/:name"
            element={
              <LazyPage>
                <PlatformAccessControlRoleBindingDetailPage />
              </LazyPage>
            }
          />

          <Route
            path="/network"
            element={<Navigate to="/network/topology" replace />}
          />
          <Route
            path="/network/topology"
            element={
              <LazyPage>
                <NetworkTopologyPage />
              </LazyPage>
            }
          />
          <Route
            path="/network/services"
            element={
              <LazyPage>
                <NetworkServicesPage />
              </LazyPage>
            }
          />
          <Route
            path="/network/services/:serviceName"
            element={
              <LazyPage>
                <ServiceDetailPage />
              </LazyPage>
            }
          />
          <Route
            path="/network/ingresses"
            element={
              <LazyPage>
                <NetworkIngressesPage />
              </LazyPage>
            }
          />
          <Route
            path="/network/gateways"
            element={
              <LazyPage>
                <NetworkGatewaysPage />
              </LazyPage>
            }
          />
          <Route
            path="/network/endpointslices"
            element={
              <LazyPage>
                <NetworkEndpointSlicesPage />
              </LazyPage>
            }
          />
          <Route
            path="/network/ingressclasses"
            element={
              <LazyPage>
                <NetworkIngressClassesPage />
              </LazyPage>
            }
          />
          <Route
            path="/network/networkpolicies"
            element={
              <LazyPage>
                <NetworkPoliciesPage />
              </LazyPage>
            }
          />
          <Route
            path="/network/port-forward"
            element={
              <LazyPage>
                <NetworkPortForwardPage />
              </LazyPage>
            }
          />

          <Route
            path="/storage"
            element={<Navigate to="/storage/persistentvolumeclaims" replace />}
          />
          <Route
            path="/storage/persistentvolumeclaims"
            element={
              <LazyPage>
                <StoragePvcPage />
              </LazyPage>
            }
          />
          <Route
            path="/storage/persistentvolumeclaims/:name"
            element={
              <LazyPage>
                <StoragePvcDetailPage />
              </LazyPage>
            }
          />
          <Route
            path="/storage/persistentvolumes"
            element={
              <LazyPage>
                <StoragePvPage />
              </LazyPage>
            }
          />
          <Route
            path="/storage/persistentvolumes/:name"
            element={
              <LazyPage>
                <StoragePvDetailPage />
              </LazyPage>
            }
          />
          <Route
            path="/storage/storageclasses"
            element={
              <LazyPage>
                <StorageClassesPage />
              </LazyPage>
            }
          />
          <Route
            path="/storage/storageclasses/:name"
            element={
              <LazyPage>
                <StorageClassDetailPage />
              </LazyPage>
            }
          />

          <Route
            path="/extensions"
            element={
              <LazyPage>
                <CRDPage />
              </LazyPage>
            }
          />
          <Route
            path="/extensions/apis/:groupName"
            element={
              <LazyPage>
                <CRDApiGroupDetailPage />
              </LazyPage>
            }
          />
          <Route
            path="/helm"
            element={<Navigate to="/helm/releases" replace />}
          />
          <Route
            path="/helm/releases"
            element={
              <LazyPage>
                <HelmReleasesPage />
              </LazyPage>
            }
          />
          <Route
            path="/helm/releases/:releaseName"
            element={
              <LazyPage>
                <HelmReleaseDetailPage />
              </LazyPage>
            }
          />
          <Route
            path="/helm/charts"
            element={
              <LazyPage>
                <HelmChartsPage />
              </LazyPage>
            }
          />

          <Route
            path="/applications"
            element={
              <LazyPage>
                <ApplicationsPage />
              </LazyPage>
            }
          />
          <Route
            path="/application-management"
            element={
              <LazyPage>
                <ApplicationManagementPage />
              </LazyPage>
            }
          />
          <Route
            path="/application-management/:applicationId"
            element={
              <LazyPage>
                <ApplicationManagementDetailPage />
              </LazyPage>
            }
          />
          <Route
            path="/applications/:applicationId"
            element={
              <LazyPage>
                <ApplicationDetailPage />
              </LazyPage>
            }
          />
          <Route
            path="/applications/:applicationId/application-environments/:applicationEnvironmentId/workloads/:workloadName"
            element={
              <LazyPage>
                <ApplicationWorkloadDetailPage />
              </LazyPage>
            }
          />
          <Route
            path="/business-lines"
            element={
              <LazyPage>
                <BusinessLinesPage />
              </LazyPage>
            }
          />
          <Route
            path="/delivery-environments"
            element={
              <LazyPage>
                <DeliveryEnvironmentsPage />
              </LazyPage>
            }
          />
          <Route
            path="/application-environments"
            element={
              <LazyPage>
                <ApplicationEnvironmentsPage />
              </LazyPage>
            }
          />
          <Route
            path="/application-environments/:applicationEnvironmentId"
            element={
              <LazyPage>
                <ApplicationEnvironmentDetailPage />
              </LazyPage>
            }
          />
          <Route
            path="/build-templates"
            element={
              <LazyPage>
                <BuildTemplatesPage />
              </LazyPage>
            }
          />
          <Route
            path="/delivery/blueprints"
            element={
              <LazyPage>
                <DeliveryBlueprintsPage />
              </LazyPage>
            }
          />
          <Route
            path="/delivery/release-bundles"
            element={
              <LazyPage>
                <ReleaseBundlesPage />
              </LazyPage>
            }
          />
          <Route
            path="/delivery/execution-tasks"
            element={
              <LazyPage>
                <ExecutionTasksPage />
              </LazyPage>
            }
          />
          <Route
            path="/delivery/approval-policies"
            element={
              <LazyPage>
                <ApprovalPoliciesPage />
              </LazyPage>
            }
          />
          <Route
            path="/workflow-templates"
            element={
              <LazyPage>
                <WorkflowTemplatesPage />
              </LazyPage>
            }
          />
          <Route
            path="/release-board"
            element={
              <LazyPage>
                <ReleaseBoardPage />
              </LazyPage>
            }
          />
          <Route
            path="/workflows"
            element={
              <LazyPage>
                <WorkflowsPage />
              </LazyPage>
            }
          />
          <Route
            path="/releases"
            element={
              <LazyPage>
                <ReleasesPage />
              </LazyPage>
            }
          />
          <Route
            path="/registries"
            element={
              <LazyPage>
                <RegistriesPage />
              </LazyPage>
            }
          />

          <Route
            path="/virtualization"
            element={<Navigate to="/virtualization/overview" replace />}
          />
          <Route
            path="/virtualization/overview"
            element={
              <LazyPage>
                <VirtualizationOverviewPage />
              </LazyPage>
            }
          />
          <Route
            path="/virtualization/vms"
            element={
              <LazyPage>
                <VirtualizationVmsPage />
              </LazyPage>
            }
          />
          <Route
            path="/virtualization/vms/:id"
            element={
              <LazyPage>
                <VirtualizationVmDetailPage />
              </LazyPage>
            }
          />
          <Route
            path="/virtualization/clusters"
            element={
              <LazyPage>
                <VirtualizationClustersPage />
              </LazyPage>
            }
          />
          <Route
            path="/virtualization/images"
            element={
              <LazyPage>
                <VirtualizationImagesPage />
              </LazyPage>
            }
          />
          <Route
            path="/virtualization/flavors"
            element={
              <LazyPage>
                <VirtualizationFlavorsPage />
              </LazyPage>
            }
          />
          <Route
            path="/virtualization/operations"
            element={
              <LazyPage>
                <VirtualizationOperationsPage />
              </LazyPage>
            }
          />
          <Route
            path="/virtualization/sync"
            element={
              <LazyPage>
                <VirtualizationSyncPage />
              </LazyPage>
            }
          />

          <Route
            path="/docker"
            element={<Navigate to="/docker/overview" replace />}
          />
          <Route
            path="/docker/overview"
            element={
              <LazyPage>
                <DockerOverviewPage />
              </LazyPage>
            }
          />
          <Route
            path="/docker/hosts"
            element={
              <LazyPage>
                <DockerHostsPage />
              </LazyPage>
            }
          />
          <Route
            path="/docker/projects"
            element={
              <LazyPage>
                <DockerProjectsPage />
              </LazyPage>
            }
          />
          <Route
            path="/docker/services"
            element={
              <LazyPage>
                <DockerServicesPage />
              </LazyPage>
            }
          />
          <Route
            path="/docker/ports"
            element={
              <LazyPage>
                <DockerPortsPage />
              </LazyPage>
            }
          />
          <Route
            path="/docker/templates"
            element={
              <LazyPage>
                <DockerTemplatesPage />
              </LazyPage>
            }
          />
          <Route
            path="/docker/operations"
            element={
              <LazyPage>
                <DockerOperationsPage />
              </LazyPage>
            }
          />

          <Route
            path="/monitoring-workbench"
            element={<Navigate to="/monitoring-workbench/overview" replace />}
          />
          <Route
            path="/monitoring-workbench/overview"
            element={
              <LazyPage>
                <MonitoringPage />
              </LazyPage>
            }
          />
          <Route
            path="/monitoring-workbench/rules"
            element={
              <LazyPage>
                <AlertRulesPage />
              </LazyPage>
            }
          />
          <Route
            path="/monitoring-workbench/alerts"
            element={
              <LazyPage>
                <AlertsPage />
              </LazyPage>
            }
          />
          <Route
            path="/monitoring-workbench/alerts/:eventId"
            element={
              <LazyPage>
                <AlertEventDetailPage />
              </LazyPage>
            }
          />
          <Route
            path="/monitoring-workbench/notifications"
            element={
              <LazyPage>
                <NotificationsPage />
              </LazyPage>
            }
          />
          <Route
            path="/monitoring-workbench/healing"
            element={
              <LazyPage>
                <HealingPage />
              </LazyPage>
            }
          />
          <Route
            path="/monitoring-workbench/oncall"
            element={
              <LazyPage>
                <OnCallBoardPage />
              </LazyPage>
            }
          />
          <Route
            path="/monitoring-workbench/oncall/settings"
            element={
              <LazyPage>
                <OnCallSettingsPage />
              </LazyPage>
            }
          />
          <Route
            path="/monitoring-workbench/events"
            element={
              <LazyPage>
                <EventsPage />
              </LazyPage>
            }
          />
          <Route
            path="/observability"
            element={<Navigate to="/monitoring-workbench" replace />}
          />
          <Route
            path="/observability/monitoring"
            element={<Navigate to="/monitoring-workbench/overview" replace />}
          />
          <Route
            path="/observability/rules"
            element={<Navigate to="/monitoring-workbench/rules" replace />}
          />
          <Route
            path="/observability/alerts"
            element={<Navigate to="/monitoring-workbench/alerts" replace />}
          />
          <Route
            path="/observability/alerts/:eventId"
            element={
              <LazyPage>
                <AlertEventDetailPage />
              </LazyPage>
            }
          />
          <Route
            path="/observability/notifications"
            element={
              <Navigate to="/monitoring-workbench/notifications" replace />
            }
          />
          <Route
            path="/observability/healing"
            element={<Navigate to="/monitoring-workbench/healing" replace />}
          />
          <Route
            path="/observability/oncall"
            element={<Navigate to="/monitoring-workbench/oncall" replace />}
          />
          <Route
            path="/observability/events"
            element={<Navigate to="/monitoring-workbench/events" replace />}
          />

          <Route
            path="/ai-workbench"
            element={<Navigate to="/ai-workbench/chat" replace />}
          />
          <Route
            path="/ai-workbench/chat"
            element={
              <LazyPage>
                <AIWorkbenchPage />
              </LazyPage>
            }
          />
          <Route
            path="/ai-workbench/investigation"
            element={<Navigate to="/ai-workbench/chat" replace />}
          />
          <Route
            path="/ai-workbench/root-cause"
            element={
              <LazyPage>
                <AIWorkbenchPage />
              </LazyPage>
            }
          />
          <Route
            path="/ai-workbench/performance"
            element={
              <LazyPage>
                <AIWorkbenchPage />
              </LazyPage>
            }
          />
          <Route
            path="/ai-workbench/inspection"
            element={
              <LazyPage>
                <AIOperationsPage />
              </LazyPage>
            }
          />
          <Route
            path="/ai-workbench/tool-settings"
            element={
              <LazyPage>
                <AIToolsPage />
              </LazyPage>
            }
          />
          <Route
            path="/ai-workbench/model-settings"
            element={
              <LazyPage>
                <AIModelSettingsPage />
              </LazyPage>
            }
          />
          <Route
            path="/ai-workbench/automation"
            element={<Navigate to="/ai-workbench/inspection" replace />}
          />
          <Route
            path="/ai-workbench/tools"
            element={<Navigate to="/ai-workbench/tool-settings" replace />}
          />
          <Route
            path="/ai-observe"
            element={<Navigate to="/ai-workbench" replace />}
          />
          <Route
            path="/ai-observe/workbench"
            element={<Navigate to="/ai-workbench/chat" replace />}
          />
          <Route
            path="/ai-observe/operations"
            element={<Navigate to="/ai-workbench/inspection" replace />}
          />
          <Route
            path="/ai-observe/tools"
            element={<Navigate to="/ai-workbench/tool-settings" replace />}
          />
          <Route
            path="/ai-observe/root-cause"
            element={<Navigate to="/ai-workbench/root-cause" replace />}
          />
          <Route
            path="/ai-observe/performance"
            element={<Navigate to="/ai-workbench/performance" replace />}
          />
          <Route
            path="/ai-observe/chat"
            element={<Navigate to="/ai-workbench/chat" replace />}
          />
          <Route
            path="/ai-observe/inspection"
            element={<Navigate to="/ai-workbench/inspection" replace />}
          />
          <Route
            path="/chat"
            element={<Navigate to="/ai-workbench/chat" replace />}
          />

          <Route
            path="/access"
            element={
              <LazyPage>
                <AccessCenterPage />
              </LazyPage>
            }
          />
          <Route
            path="/access/users"
            element={
              <LazyPage>
                <AccessUsersPage />
              </LazyPage>
            }
          />
          <Route
            path="/access/roles"
            element={
              <LazyPage>
                <AccessRolesPage />
              </LazyPage>
            }
          />
          <Route
            path="/access/teams"
            element={
              <LazyPage>
                <AccessTeamsPage />
              </LazyPage>
            }
          />
          <Route
            path="/access/policies"
            element={
              <LazyPage>
                <AccessPoliciesPage />
              </LazyPage>
            }
          />
          <Route
            path="/access/scope-grants"
            element={
              <LazyPage>
                <AccessScopeGrantsPage />
              </LazyPage>
            }
          />

          <Route
            path="/system"
            element={<Navigate to="/system/online-users" replace />}
          />
          <Route
            path="/system/online-users"
            element={
              <LazyPage>
                <OnlineUsersPage />
              </LazyPage>
            }
          />
          <Route
            path="/system/announcements"
            element={
              <LazyPage>
                <AnnouncementsPage />
              </LazyPage>
            }
          />
          <Route
            path="/system/menus"
            element={
              <LazyPage>
                <MenusPage />
              </LazyPage>
            }
          />
          <Route
            path="/system/audit"
            element={
              <LazyPage>
                <AuditLogsPage />
              </LazyPage>
            }
          />
          <Route
            path="/system/operations"
            element={
              <LazyPage>
                <OperationLogsPage />
              </LazyPage>
            }
          />

          <Route
            path="/settings"
            element={
              <LazyPage>
                <SettingsCenterPage />
              </LazyPage>
            }
          />
          <Route
            path="/settings/login"
            element={
              <LazyPage>
                <SettingsCenterPage />
              </LazyPage>
            }
          />
          <Route
            path="/settings/identity"
            element={<Navigate to="/settings/login" replace />}
          />
          <Route
            path="/settings/monitoring"
            element={<Navigate to="/settings" replace />}
          />
          <Route
            path="/settings/branding"
            element={
              <LazyPage>
                <SettingsCenterPage />
              </LazyPage>
            }
          />
          <Route
            path="/settings/ai"
            element={<Navigate to="/ai-workbench/model-settings" replace />}
          />

          <Route path="*" element={<Navigate to="/" replace />} />
        </Route>
      </Route>
    </Routes>
  );
}
