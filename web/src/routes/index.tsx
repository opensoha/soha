import { lazy, Suspense } from 'react'
import { Navigate, Route, Routes } from 'react-router-dom'
import { Spin } from 'antd'
import { AuthGuard } from '@/features/auth/auth-guard'
import { AppLayout } from '@/layouts/app-layout'

function lazyNamed<T extends Record<string, any>, K extends keyof T>(
  importer: () => Promise<T>,
  key: K,
) {
  return lazy(async () => {
    const mod = await importer()
    return { default: mod[key] as any }
  })
}

const LoginPage = lazyNamed(() => import('@/features/auth/login-page'), 'LoginPage')
const OIDCCallbackPage = lazyNamed(() => import('@/features/auth/oidc-callback-page'), 'OIDCCallbackPage')

const OverviewPage = lazyNamed(() => import('@/features/platform/overview-page'), 'OverviewPage')
const ClustersPage = lazyNamed(() => import('@/features/platform/clusters-page'), 'ClustersPage')
const ClusterDetailPage = lazyNamed(() => import('@/features/platform/clusters-page'), 'ClusterDetailPage')
const ClusterNodesPage = lazyNamed(() => import('@/features/platform/cluster-resources-pages'), 'ClusterNodesPage')
const ClusterNamespacesPage = lazyNamed(() => import('@/features/platform/cluster-resources-pages'), 'ClusterNamespacesPage')
const NodeDetailPage = lazyNamed(() => import('@/features/platform/node-detail-page'), 'NodeDetailPage')

const WorkloadsOverviewPage = lazyNamed(() => import('@/features/platform/workloads-pages'), 'WorkloadsOverviewPage')
const WorkloadsDeploymentsPage = lazyNamed(() => import('@/features/platform/workloads-pages'), 'WorkloadsDeploymentsPage')
const WorkloadsPodsPage = lazyNamed(() => import('@/features/platform/workloads-pages'), 'WorkloadsPodsPage')
const WorkloadsStatefulSetsPage = lazyNamed(() => import('@/features/platform/workloads-pages'), 'WorkloadsStatefulSetsPage')
const WorkloadsDaemonSetsPage = lazyNamed(() => import('@/features/platform/workloads-pages'), 'WorkloadsDaemonSetsPage')
const WorkloadsJobsPage = lazyNamed(() => import('@/features/platform/workloads-pages'), 'WorkloadsJobsPage')
const WorkloadsCronJobsPage = lazyNamed(() => import('@/features/platform/workloads-pages'), 'WorkloadsCronJobsPage')
const WorkloadsReplicaSetsPage = lazyNamed(() => import('@/features/platform/platform-management-pages'), 'WorkloadsReplicaSetsPage')
const WorkloadsReplicationControllersPage = lazyNamed(() => import('@/features/platform/platform-management-pages'), 'WorkloadsReplicationControllersPage')
const PodDetailPage = lazyNamed(() => import('@/features/platform/workloads-pages'), 'PodDetailPage')
const DeploymentDetailPage = lazyNamed(() => import('@/features/platform/workloads-pages'), 'DeploymentDetailPage')
const StatefulSetDetailPage = lazyNamed(() => import('@/features/platform/workloads-pages'), 'StatefulSetDetailPage')
const DaemonSetDetailPage = lazyNamed(() => import('@/features/platform/workloads-pages'), 'DaemonSetDetailPage')
const JobDetailPage = lazyNamed(() => import('@/features/platform/workloads-pages'), 'JobDetailPage')
const CronJobDetailPage = lazyNamed(() => import('@/features/platform/workloads-pages'), 'CronJobDetailPage')

const ConfigurationConfigMapsPage = lazyNamed(() => import('@/features/platform/platform-management-pages'), 'ConfigurationConfigMapsPage')
const ConfigurationSecretsPage = lazyNamed(() => import('@/features/platform/platform-management-pages'), 'ConfigurationSecretsPage')
const ConfigMapDetailPage = lazyNamed(() => import('@/features/platform/configuration-detail-pages'), 'ConfigMapDetailPage')
const SecretDetailPage = lazyNamed(() => import('@/features/platform/configuration-detail-pages'), 'SecretDetailPage')
const ConfigurationResourceQuotasPage = lazyNamed(() => import('@/features/platform/platform-management-pages'), 'ConfigurationResourceQuotasPage')
const ConfigurationLimitRangesPage = lazyNamed(() => import('@/features/platform/platform-management-pages'), 'ConfigurationLimitRangesPage')
const ConfigurationHPAPage = lazyNamed(() => import('@/features/platform/platform-management-pages'), 'ConfigurationHPAPage')
const ConfigurationPodDisruptionBudgetsPage = lazyNamed(() => import('@/features/platform/platform-management-pages'), 'ConfigurationPodDisruptionBudgetsPage')
const ConfigurationPriorityClassesPage = lazyNamed(() => import('@/features/platform/platform-management-pages'), 'ConfigurationPriorityClassesPage')
const ConfigurationRuntimeClassesPage = lazyNamed(() => import('@/features/platform/platform-management-pages'), 'ConfigurationRuntimeClassesPage')
const ConfigurationLeasesPage = lazyNamed(() => import('@/features/platform/platform-management-pages'), 'ConfigurationLeasesPage')
const ConfigurationMutatingWebhooksPage = lazyNamed(() => import('@/features/platform/platform-management-pages'), 'ConfigurationMutatingWebhooksPage')
const ConfigurationValidatingWebhooksPage = lazyNamed(() => import('@/features/platform/platform-management-pages'), 'ConfigurationValidatingWebhooksPage')
const PlatformAccessControlServiceAccountsPage = lazyNamed(() => import('@/features/platform/platform-management-pages'), 'PlatformAccessControlServiceAccountsPage')
const PlatformAccessControlClusterRolesPage = lazyNamed(() => import('@/features/platform/platform-management-pages'), 'PlatformAccessControlClusterRolesPage')
const PlatformAccessControlRolesPage = lazyNamed(() => import('@/features/platform/platform-management-pages'), 'PlatformAccessControlRolesPage')
const PlatformAccessControlClusterRoleBindingsPage = lazyNamed(() => import('@/features/platform/platform-management-pages'), 'PlatformAccessControlClusterRoleBindingsPage')
const PlatformAccessControlRoleBindingsPage = lazyNamed(() => import('@/features/platform/platform-management-pages'), 'PlatformAccessControlRoleBindingsPage')

const NetworkServicesPage = lazyNamed(() => import('@/features/platform/network-storage-pages'), 'NetworkServicesPage')
const ServiceDetailPage = lazyNamed(() => import('@/features/platform/network-storage-pages'), 'ServiceDetailPage')
const NetworkIngressesPage = lazyNamed(() => import('@/features/platform/network-storage-pages'), 'NetworkIngressesPage')
const NetworkGatewaysPage = lazyNamed(() => import('@/features/platform/network-storage-pages'), 'NetworkGatewaysPage')
const NetworkTopologyPage = lazyNamed(() => import('@/features/platform/network-topology-page'), 'NetworkTopologyPage')
const NetworkEndpointSlicesPage = lazyNamed(() => import('@/features/platform/platform-management-pages'), 'NetworkEndpointSlicesPage')
const NetworkIngressClassesPage = lazyNamed(() => import('@/features/platform/platform-management-pages'), 'NetworkIngressClassesPage')
const NetworkPoliciesPage = lazyNamed(() => import('@/features/platform/platform-management-pages'), 'NetworkPoliciesPage')
const NetworkPortForwardPage = lazyNamed(() => import('@/features/platform/platform-management-pages'), 'NetworkPortForwardPage')
const StoragePvcPage = lazyNamed(() => import('@/features/platform/network-storage-pages'), 'StoragePvcPage')
const StoragePvPage = lazyNamed(() => import('@/features/platform/network-storage-pages'), 'StoragePvPage')
const StorageClassesPage = lazyNamed(() => import('@/features/platform/network-storage-pages'), 'StorageClassesPage')

const CRDPage = lazyNamed(() => import('@/features/platform/extensions-pages'), 'CRDPage')
const HelmReleasesPage = lazyNamed(() => import('@/features/platform/extensions-pages'), 'HelmReleasesPage')
const HelmChartsPage = lazyNamed(() => import('@/features/platform/extensions-pages'), 'HelmChartsPage')

const ApplicationsPage = lazyNamed(() => import('@/features/delivery/delivery-pages'), 'ApplicationsPage')
const BusinessLinesPage = lazyNamed(() => import('@/features/delivery/delivery-catalog-pages'), 'BusinessLinesPage')
const DeliveryEnvironmentsPage = lazyNamed(() => import('@/features/delivery/delivery-catalog-pages'), 'DeliveryEnvironmentsPage')
const ApplicationEnvironmentsPage = lazyNamed(() => import('@/features/delivery/delivery-catalog-pages'), 'ApplicationEnvironmentsPage')
const ApplicationEnvironmentDetailPage = lazyNamed(() => import('@/features/delivery/delivery-catalog-pages'), 'ApplicationEnvironmentDetailPage')
const WorkflowTemplatesPage = lazyNamed(() => import('@/features/delivery/delivery-catalog-pages'), 'WorkflowTemplatesPage')
const ReleaseBoardPage = lazyNamed(() => import('@/features/delivery/delivery-catalog-pages'), 'ReleaseBoardPage')
const WorkflowsPage = lazyNamed(() => import('@/features/delivery/delivery-pages'), 'WorkflowsPage')
const ReleasesPage = lazyNamed(() => import('@/features/delivery/delivery-pages'), 'ReleasesPage')
const RegistriesPage = lazyNamed(() => import('@/features/delivery/delivery-pages'), 'RegistriesPage')

const MonitoringPage = lazyNamed(() => import('@/features/observability/monitoring-pages'), 'MonitoringPage')
const AlertsPage = lazyNamed(() => import('@/features/observability/monitoring-pages'), 'AlertsPage')
const NotificationsPage = lazyNamed(() => import('@/features/observability/monitoring-pages'), 'NotificationsPage')
const EventsPage = lazyNamed(() => import('@/features/observability/monitoring-pages'), 'EventsPage')
const OnCallPage = lazyNamed(() => import('@/features/observability/monitoring-pages'), 'OnCallPage')

const ChatPage = lazyNamed(() => import('@/features/copilot/chat-page'), 'ChatPage')
const RootCauseAnalysisPage = lazyNamed(() => import('@/features/copilot/analysis-pages'), 'RootCauseAnalysisPage')
const PerformanceAnalysisPage = lazyNamed(() => import('@/features/copilot/analysis-pages'), 'PerformanceAnalysisPage')
const InspectionCenterPage = lazyNamed(() => import('@/features/copilot/analysis-pages'), 'InspectionCenterPage')

const AccessCenterPage = lazyNamed(() => import('@/features/access/access-pages'), 'AccessCenterPage')
const AccessScopeGrantsPage = lazyNamed(() => import('@/features/access/scope-grants-page'), 'AccessScopeGrantsPage')

const OnlineUsersPage = lazyNamed(() => import('@/features/system/system-pages'), 'OnlineUsersPage')
const AnnouncementsPage = lazyNamed(() => import('@/features/system/system-pages'), 'AnnouncementsPage')
const MenusPage = lazyNamed(() => import('@/features/system/system-pages'), 'MenusPage')
const AuditLogsPage = lazyNamed(() => import('@/features/system/system-pages'), 'AuditLogsPage')
const OperationLogsPage = lazyNamed(() => import('@/features/system/system-pages'), 'OperationLogsPage')

const SettingsCenterPage = lazyNamed(() => import('@/features/settings/settings-pages'), 'SettingsCenterPage')

function LazyPage({ children }: { children: React.ReactNode }) {
  return (
    <Suspense fallback={<div className="flex items-center justify-center h-full py-20"><Spin size="large" /></div>}>
      {children}
    </Suspense>
  )
}

export function AppRouter() {
  return (
    <Routes>
      <Route path="/login" element={<LazyPage><LoginPage /></LazyPage>} />
      <Route path="/auth/oidc/callback" element={<LazyPage><OIDCCallbackPage /></LazyPage>} />
      <Route path="/login/callback" element={<LazyPage><OIDCCallbackPage /></LazyPage>} />
      <Route element={<AuthGuard />}>
        <Route element={<AppLayout />}>
          <Route path="/" element={<LazyPage><OverviewPage /></LazyPage>} />
          <Route path="/clusters" element={<LazyPage><ClustersPage /></LazyPage>} />
          <Route path="/clusters/:clusterId" element={<LazyPage><ClusterDetailPage /></LazyPage>} />
          <Route path="/cluster-resources" element={<Navigate to="/cluster-resources/nodes" replace />} />
          <Route path="/cluster-resources/nodes" element={<LazyPage><ClusterNodesPage /></LazyPage>} />
          <Route path="/cluster-resources/nodes/:nodeName" element={<LazyPage><NodeDetailPage /></LazyPage>} />
          <Route path="/cluster-resources/namespaces" element={<LazyPage><ClusterNamespacesPage /></LazyPage>} />

          <Route path="/workloads" element={<Navigate to="/workloads/overview" replace />} />
          <Route path="/workloads/overview" element={<LazyPage><WorkloadsOverviewPage /></LazyPage>} />
          <Route path="/workloads/deployments" element={<LazyPage><WorkloadsDeploymentsPage /></LazyPage>} />
          <Route path="/workloads/pods" element={<LazyPage><WorkloadsPodsPage /></LazyPage>} />
          <Route path="/workloads/statefulsets" element={<LazyPage><WorkloadsStatefulSetsPage /></LazyPage>} />
          <Route path="/workloads/daemonsets" element={<LazyPage><WorkloadsDaemonSetsPage /></LazyPage>} />
          <Route path="/workloads/jobs" element={<LazyPage><WorkloadsJobsPage /></LazyPage>} />
          <Route path="/workloads/cronjobs" element={<LazyPage><WorkloadsCronJobsPage /></LazyPage>} />
          <Route path="/workloads/replicasets" element={<LazyPage><WorkloadsReplicaSetsPage /></LazyPage>} />
          <Route path="/workloads/replicationcontrollers" element={<LazyPage><WorkloadsReplicationControllersPage /></LazyPage>} />
          <Route path="/workloads/pods/:podName" element={<LazyPage><PodDetailPage /></LazyPage>} />
          <Route path="/workloads/deployments/:deploymentName" element={<LazyPage><DeploymentDetailPage /></LazyPage>} />
          <Route path="/workloads/statefulsets/:statefulSetName" element={<LazyPage><StatefulSetDetailPage /></LazyPage>} />
          <Route path="/workloads/daemonsets/:daemonSetName" element={<LazyPage><DaemonSetDetailPage /></LazyPage>} />
          <Route path="/workloads/jobs/:jobName" element={<LazyPage><JobDetailPage /></LazyPage>} />
          <Route path="/workloads/cronjobs/:cronJobName" element={<LazyPage><CronJobDetailPage /></LazyPage>} />

          <Route path="/configuration" element={<Navigate to="/configuration/configmaps" replace />} />
          <Route path="/configuration/configmaps" element={<LazyPage><ConfigurationConfigMapsPage /></LazyPage>} />
          <Route path="/configuration/configmaps/:configMapName" element={<LazyPage><ConfigMapDetailPage /></LazyPage>} />
          <Route path="/configuration/secrets" element={<LazyPage><ConfigurationSecretsPage /></LazyPage>} />
          <Route path="/configuration/secrets/:secretName" element={<LazyPage><SecretDetailPage /></LazyPage>} />
          <Route path="/configuration/resourcequotas" element={<LazyPage><ConfigurationResourceQuotasPage /></LazyPage>} />
          <Route path="/configuration/limitranges" element={<LazyPage><ConfigurationLimitRangesPage /></LazyPage>} />
          <Route path="/configuration/hpas" element={<LazyPage><ConfigurationHPAPage /></LazyPage>} />
          <Route path="/configuration/poddisruptionbudgets" element={<LazyPage><ConfigurationPodDisruptionBudgetsPage /></LazyPage>} />
          <Route path="/configuration/priorityclasses" element={<LazyPage><ConfigurationPriorityClassesPage /></LazyPage>} />
          <Route path="/configuration/runtimeclasses" element={<LazyPage><ConfigurationRuntimeClassesPage /></LazyPage>} />
          <Route path="/configuration/leases" element={<LazyPage><ConfigurationLeasesPage /></LazyPage>} />
          <Route path="/configuration/mutatingwebhookconfigurations" element={<LazyPage><ConfigurationMutatingWebhooksPage /></LazyPage>} />
          <Route path="/configuration/validatingwebhookconfigurations" element={<LazyPage><ConfigurationValidatingWebhooksPage /></LazyPage>} />

          <Route path="/platform-access-control" element={<Navigate to="/platform-access-control/serviceaccounts" replace />} />
          <Route path="/platform-access-control/serviceaccounts" element={<LazyPage><PlatformAccessControlServiceAccountsPage /></LazyPage>} />
          <Route path="/platform-access-control/clusterroles" element={<LazyPage><PlatformAccessControlClusterRolesPage /></LazyPage>} />
          <Route path="/platform-access-control/roles" element={<LazyPage><PlatformAccessControlRolesPage /></LazyPage>} />
          <Route path="/platform-access-control/clusterrolebindings" element={<LazyPage><PlatformAccessControlClusterRoleBindingsPage /></LazyPage>} />
          <Route path="/platform-access-control/rolebindings" element={<LazyPage><PlatformAccessControlRoleBindingsPage /></LazyPage>} />

          <Route path="/network" element={<Navigate to="/network/topology" replace />} />
          <Route path="/network/topology" element={<LazyPage><NetworkTopologyPage /></LazyPage>} />
          <Route path="/network/services" element={<LazyPage><NetworkServicesPage /></LazyPage>} />
          <Route path="/network/services/:serviceName" element={<LazyPage><ServiceDetailPage /></LazyPage>} />
          <Route path="/network/ingresses" element={<LazyPage><NetworkIngressesPage /></LazyPage>} />
          <Route path="/network/gateways" element={<LazyPage><NetworkGatewaysPage /></LazyPage>} />
          <Route path="/network/endpointslices" element={<LazyPage><NetworkEndpointSlicesPage /></LazyPage>} />
          <Route path="/network/ingressclasses" element={<LazyPage><NetworkIngressClassesPage /></LazyPage>} />
          <Route path="/network/networkpolicies" element={<LazyPage><NetworkPoliciesPage /></LazyPage>} />
          <Route path="/network/port-forward" element={<LazyPage><NetworkPortForwardPage /></LazyPage>} />

          <Route path="/storage" element={<Navigate to="/storage/persistentvolumeclaims" replace />} />
          <Route path="/storage/persistentvolumeclaims" element={<LazyPage><StoragePvcPage /></LazyPage>} />
          <Route path="/storage/persistentvolumes" element={<LazyPage><StoragePvPage /></LazyPage>} />
          <Route path="/storage/storageclasses" element={<LazyPage><StorageClassesPage /></LazyPage>} />

          <Route path="/extensions" element={<LazyPage><CRDPage /></LazyPage>} />
          <Route path="/helm" element={<Navigate to="/helm/releases" replace />} />
          <Route path="/helm/releases" element={<LazyPage><HelmReleasesPage /></LazyPage>} />
          <Route path="/helm/charts" element={<LazyPage><HelmChartsPage /></LazyPage>} />

          <Route path="/applications" element={<LazyPage><ApplicationsPage /></LazyPage>} />
          <Route path="/business-lines" element={<LazyPage><BusinessLinesPage /></LazyPage>} />
          <Route path="/delivery-environments" element={<LazyPage><DeliveryEnvironmentsPage /></LazyPage>} />
          <Route path="/application-environments" element={<LazyPage><ApplicationEnvironmentsPage /></LazyPage>} />
          <Route path="/application-environments/:applicationEnvironmentId" element={<LazyPage><ApplicationEnvironmentDetailPage /></LazyPage>} />
          <Route path="/workflow-templates" element={<LazyPage><WorkflowTemplatesPage /></LazyPage>} />
          <Route path="/release-board" element={<LazyPage><ReleaseBoardPage /></LazyPage>} />
          <Route path="/workflows" element={<LazyPage><WorkflowsPage /></LazyPage>} />
          <Route path="/releases" element={<LazyPage><ReleasesPage /></LazyPage>} />
          <Route path="/registries" element={<LazyPage><RegistriesPage /></LazyPage>} />

          <Route path="/observability" element={<Navigate to="/observability/monitoring" replace />} />
          <Route path="/observability/monitoring" element={<LazyPage><MonitoringPage /></LazyPage>} />
          <Route path="/observability/alerts" element={<LazyPage><AlertsPage /></LazyPage>} />
          <Route path="/observability/notifications" element={<LazyPage><NotificationsPage /></LazyPage>} />
          <Route path="/observability/oncall" element={<LazyPage><OnCallPage /></LazyPage>} />
          <Route path="/observability/events" element={<LazyPage><EventsPage /></LazyPage>} />

          <Route path="/ai-observe" element={<Navigate to="/ai-observe/root-cause" replace />} />
          <Route path="/ai-observe/root-cause" element={<LazyPage><RootCauseAnalysisPage /></LazyPage>} />
          <Route path="/ai-observe/performance" element={<LazyPage><PerformanceAnalysisPage /></LazyPage>} />
          <Route path="/ai-observe/chat" element={<LazyPage><ChatPage /></LazyPage>} />
          <Route path="/ai-observe/inspection" element={<LazyPage><InspectionCenterPage /></LazyPage>} />
          <Route path="/chat" element={<Navigate to="/ai-observe/chat" replace />} />

          <Route path="/access" element={<Navigate to="/access/users" replace />} />
          <Route path="/access/users" element={<LazyPage><AccessCenterPage /></LazyPage>} />
          <Route path="/access/roles" element={<LazyPage><AccessCenterPage /></LazyPage>} />
          <Route path="/access/teams" element={<LazyPage><AccessCenterPage /></LazyPage>} />
          <Route path="/access/policies" element={<LazyPage><AccessCenterPage /></LazyPage>} />
          <Route path="/access/scope-grants" element={<LazyPage><AccessScopeGrantsPage /></LazyPage>} />

          <Route path="/system" element={<Navigate to="/system/online-users" replace />} />
          <Route path="/system/online-users" element={<LazyPage><OnlineUsersPage /></LazyPage>} />
          <Route path="/system/announcements" element={<LazyPage><AnnouncementsPage /></LazyPage>} />
          <Route path="/system/menus" element={<LazyPage><MenusPage /></LazyPage>} />
          <Route path="/system/audit" element={<LazyPage><AuditLogsPage /></LazyPage>} />
          <Route path="/system/operations" element={<LazyPage><OperationLogsPage /></LazyPage>} />

          <Route path="/settings" element={<LazyPage><SettingsCenterPage /></LazyPage>} />
          <Route path="/settings/identity" element={<LazyPage><SettingsCenterPage /></LazyPage>} />
          <Route path="/settings/monitoring" element={<Navigate to="/settings" replace />} />
          <Route path="/settings/branding" element={<LazyPage><SettingsCenterPage /></LazyPage>} />
          <Route path="/settings/ai" element={<LazyPage><SettingsCenterPage /></LazyPage>} />

          <Route path="*" element={<Navigate to="/" replace />} />
        </Route>
      </Route>
    </Routes>
  )
}
