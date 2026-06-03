import { Navigate, Route, Routes } from "react-router-dom";
import { AuthGuard } from "@/features/auth/auth-guard";
import { AppLayout } from "@/layouts/app-layout";
import * as RoutePages from "./route-lazy-pages";

export function AppRouter() {
  return (
    <Routes>
      <Route
        path="/login"
        element={
          <RoutePages.LazyPage>
            <RoutePages.LoginPage />
          </RoutePages.LazyPage>
        }
      />
      <Route
        path="/auth/oidc/callback"
        element={
          <RoutePages.LazyPage>
            <RoutePages.OIDCCallbackPage />
          </RoutePages.LazyPage>
        }
      />
      <Route
        path="/login/callback"
        element={
          <RoutePages.LazyPage>
            <RoutePages.OIDCCallbackPage />
          </RoutePages.LazyPage>
        }
      />
      <Route element={<AuthGuard />}>
        <Route element={<AppLayout />}>
          <Route
            path="/"
            element={
              <RoutePages.LazyPage>
                <RoutePages.OverviewPage />
              </RoutePages.LazyPage>
            }
          />
          <Route
            path="/clusters"
            element={
              <RoutePages.LazyPage>
                <RoutePages.ClustersPage />
              </RoutePages.LazyPage>
            }
          />
          <Route
            path="/clusters/:clusterId"
            element={
              <RoutePages.LazyPage>
                <RoutePages.ClusterDetailPage />
              </RoutePages.LazyPage>
            }
          />
          <Route
            path="/cluster-resources"
            element={<Navigate to="/cluster-resources/nodes" replace />}
          />
          <Route
            path="/cluster-resources/nodes"
            element={
              <RoutePages.LazyPage>
                <RoutePages.ClusterNodesPage />
              </RoutePages.LazyPage>
            }
          />
          <Route
            path="/cluster-resources/nodes/:nodeName"
            element={
              <RoutePages.LazyPage>
                <RoutePages.NodeDetailPage />
              </RoutePages.LazyPage>
            }
          />
          <Route
            path="/cluster-resources/namespaces"
            element={
              <RoutePages.LazyPage>
                <RoutePages.ClusterNamespacesPage />
              </RoutePages.LazyPage>
            }
          />

          <Route
            path="/workloads"
            element={<Navigate to="/workloads/overview" replace />}
          />
          <Route
            path="/workloads/overview"
            element={
              <RoutePages.LazyPage>
                <RoutePages.WorkloadsOverviewPage />
              </RoutePages.LazyPage>
            }
          />
          <Route
            path="/workloads/deployments"
            element={
              <RoutePages.LazyPage>
                <RoutePages.WorkloadsDeploymentsPage />
              </RoutePages.LazyPage>
            }
          />
          <Route
            path="/workloads/pods"
            element={
              <RoutePages.LazyPage>
                <RoutePages.WorkloadsPodsPage />
              </RoutePages.LazyPage>
            }
          />
          <Route
            path="/workloads/statefulsets"
            element={
              <RoutePages.LazyPage>
                <RoutePages.WorkloadsStatefulSetsPage />
              </RoutePages.LazyPage>
            }
          />
          <Route
            path="/workloads/daemonsets"
            element={
              <RoutePages.LazyPage>
                <RoutePages.WorkloadsDaemonSetsPage />
              </RoutePages.LazyPage>
            }
          />
          <Route
            path="/workloads/jobs"
            element={
              <RoutePages.LazyPage>
                <RoutePages.WorkloadsJobsPage />
              </RoutePages.LazyPage>
            }
          />
          <Route
            path="/workloads/cronjobs"
            element={
              <RoutePages.LazyPage>
                <RoutePages.WorkloadsCronJobsPage />
              </RoutePages.LazyPage>
            }
          />
          <Route
            path="/workloads/replicasets"
            element={
              <RoutePages.LazyPage>
                <RoutePages.WorkloadsReplicaSetsPage />
              </RoutePages.LazyPage>
            }
          />
          <Route
            path="/workloads/replicationcontrollers"
            element={
              <RoutePages.LazyPage>
                <RoutePages.WorkloadsReplicationControllersPage />
              </RoutePages.LazyPage>
            }
          />
          <Route
            path="/workloads/pods/:podName"
            element={
              <RoutePages.LazyPage>
                <RoutePages.PodDetailPage />
              </RoutePages.LazyPage>
            }
          />
          <Route
            path="/workloads/deployments/:deploymentName"
            element={
              <RoutePages.LazyPage>
                <RoutePages.DeploymentDetailPage />
              </RoutePages.LazyPage>
            }
          />
          <Route
            path="/workloads/statefulsets/:statefulSetName"
            element={
              <RoutePages.LazyPage>
                <RoutePages.StatefulSetDetailPage />
              </RoutePages.LazyPage>
            }
          />
          <Route
            path="/workloads/daemonsets/:daemonSetName"
            element={
              <RoutePages.LazyPage>
                <RoutePages.DaemonSetDetailPage />
              </RoutePages.LazyPage>
            }
          />
          <Route
            path="/workloads/jobs/:jobName"
            element={
              <RoutePages.LazyPage>
                <RoutePages.JobDetailPage />
              </RoutePages.LazyPage>
            }
          />
          <Route
            path="/workloads/cronjobs/:cronJobName"
            element={
              <RoutePages.LazyPage>
                <RoutePages.CronJobDetailPage />
              </RoutePages.LazyPage>
            }
          />

          <Route
            path="/configuration"
            element={<Navigate to="/configuration/configmaps" replace />}
          />
          <Route
            path="/configuration/configmaps"
            element={
              <RoutePages.LazyPage>
                <RoutePages.ConfigurationConfigMapsPage />
              </RoutePages.LazyPage>
            }
          />
          <Route
            path="/configuration/configmaps/:configMapName"
            element={
              <RoutePages.LazyPage>
                <RoutePages.ConfigMapDetailPage />
              </RoutePages.LazyPage>
            }
          />
          <Route
            path="/configuration/secrets"
            element={
              <RoutePages.LazyPage>
                <RoutePages.ConfigurationSecretsPage />
              </RoutePages.LazyPage>
            }
          />
          <Route
            path="/configuration/secrets/:secretName"
            element={
              <RoutePages.LazyPage>
                <RoutePages.SecretDetailPage />
              </RoutePages.LazyPage>
            }
          />
          <Route
            path="/configuration/resourcequotas"
            element={
              <RoutePages.LazyPage>
                <RoutePages.ConfigurationResourceQuotasPage />
              </RoutePages.LazyPage>
            }
          />
          <Route
            path="/configuration/limitranges"
            element={
              <RoutePages.LazyPage>
                <RoutePages.ConfigurationLimitRangesPage />
              </RoutePages.LazyPage>
            }
          />
          <Route
            path="/configuration/hpas"
            element={
              <RoutePages.LazyPage>
                <RoutePages.ConfigurationHPAPage />
              </RoutePages.LazyPage>
            }
          />
          <Route
            path="/configuration/poddisruptionbudgets"
            element={
              <RoutePages.LazyPage>
                <RoutePages.ConfigurationPodDisruptionBudgetsPage />
              </RoutePages.LazyPage>
            }
          />
          <Route
            path="/configuration/priorityclasses"
            element={
              <RoutePages.LazyPage>
                <RoutePages.ConfigurationPriorityClassesPage />
              </RoutePages.LazyPage>
            }
          />
          <Route
            path="/configuration/runtimeclasses"
            element={
              <RoutePages.LazyPage>
                <RoutePages.ConfigurationRuntimeClassesPage />
              </RoutePages.LazyPage>
            }
          />
          <Route
            path="/configuration/leases"
            element={
              <RoutePages.LazyPage>
                <RoutePages.ConfigurationLeasesPage />
              </RoutePages.LazyPage>
            }
          />
          <Route
            path="/configuration/mutatingwebhookconfigurations"
            element={
              <RoutePages.LazyPage>
                <RoutePages.ConfigurationMutatingWebhooksPage />
              </RoutePages.LazyPage>
            }
          />
          <Route
            path="/configuration/validatingwebhookconfigurations"
            element={
              <RoutePages.LazyPage>
                <RoutePages.ConfigurationValidatingWebhooksPage />
              </RoutePages.LazyPage>
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
              <RoutePages.LazyPage>
                <RoutePages.PlatformAccessControlServiceAccountsPage />
              </RoutePages.LazyPage>
            }
          />
          <Route
            path="/platform-access-control/serviceaccounts/:name"
            element={
              <RoutePages.LazyPage>
                <RoutePages.PlatformAccessControlServiceAccountDetailPage />
              </RoutePages.LazyPage>
            }
          />
          <Route
            path="/platform-access-control/clusterroles"
            element={
              <RoutePages.LazyPage>
                <RoutePages.PlatformAccessControlClusterRolesPage />
              </RoutePages.LazyPage>
            }
          />
          <Route
            path="/platform-access-control/clusterroles/:name"
            element={
              <RoutePages.LazyPage>
                <RoutePages.PlatformAccessControlClusterRoleDetailPage />
              </RoutePages.LazyPage>
            }
          />
          <Route
            path="/platform-access-control/roles"
            element={
              <RoutePages.LazyPage>
                <RoutePages.PlatformAccessControlRolesPage />
              </RoutePages.LazyPage>
            }
          />
          <Route
            path="/platform-access-control/roles/:name"
            element={
              <RoutePages.LazyPage>
                <RoutePages.PlatformAccessControlRoleDetailPage />
              </RoutePages.LazyPage>
            }
          />
          <Route
            path="/platform-access-control/clusterrolebindings"
            element={
              <RoutePages.LazyPage>
                <RoutePages.PlatformAccessControlClusterRoleBindingsPage />
              </RoutePages.LazyPage>
            }
          />
          <Route
            path="/platform-access-control/clusterrolebindings/:name"
            element={
              <RoutePages.LazyPage>
                <RoutePages.PlatformAccessControlClusterRoleBindingDetailPage />
              </RoutePages.LazyPage>
            }
          />
          <Route
            path="/platform-access-control/rolebindings"
            element={
              <RoutePages.LazyPage>
                <RoutePages.PlatformAccessControlRoleBindingsPage />
              </RoutePages.LazyPage>
            }
          />
          <Route
            path="/platform-access-control/rolebindings/:name"
            element={
              <RoutePages.LazyPage>
                <RoutePages.PlatformAccessControlRoleBindingDetailPage />
              </RoutePages.LazyPage>
            }
          />

          <Route
            path="/network"
            element={<Navigate to="/network/topology" replace />}
          />
          <Route
            path="/network/topology"
            element={
              <RoutePages.LazyPage>
                <RoutePages.NetworkTopologyPage />
              </RoutePages.LazyPage>
            }
          />
          <Route
            path="/network/services"
            element={
              <RoutePages.LazyPage>
                <RoutePages.NetworkServicesPage />
              </RoutePages.LazyPage>
            }
          />
          <Route
            path="/network/services/:serviceName"
            element={
              <RoutePages.LazyPage>
                <RoutePages.ServiceDetailPage />
              </RoutePages.LazyPage>
            }
          />
          <Route
            path="/network/ingresses"
            element={
              <RoutePages.LazyPage>
                <RoutePages.NetworkIngressesPage />
              </RoutePages.LazyPage>
            }
          />
          <Route
            path="/network/gateways"
            element={<Navigate to="/network/gateway-api/gateways" replace />}
          />
          <Route
            path="/network/gateway-api"
            element={<Navigate to="/network/gateway-api/gatewayclasses" replace />}
          />
          <Route
            path="/network/gateway-api/gatewayclasses"
            element={
              <RoutePages.LazyPage>
                <RoutePages.NetworkGatewayClassesPage />
              </RoutePages.LazyPage>
            }
          />
          <Route
            path="/network/gateway-api/gateways"
            element={
              <RoutePages.LazyPage>
                <RoutePages.NetworkGatewaysPage />
              </RoutePages.LazyPage>
            }
          />
          <Route
            path="/network/gateway-api/httproutes"
            element={
              <RoutePages.LazyPage>
                <RoutePages.NetworkHTTPRoutesPage />
              </RoutePages.LazyPage>
            }
          />
          <Route
            path="/network/gateway-api/backendtlspolicies"
            element={
              <RoutePages.LazyPage>
                <RoutePages.NetworkBackendTLSPoliciesPage />
              </RoutePages.LazyPage>
            }
          />
          <Route
            path="/network/gateway-api/grpcroutes"
            element={
              <RoutePages.LazyPage>
                <RoutePages.NetworkGRPCRoutesPage />
              </RoutePages.LazyPage>
            }
          />
          <Route
            path="/network/gateway-api/referencegrants"
            element={
              <RoutePages.LazyPage>
                <RoutePages.NetworkReferenceGrantsPage />
              </RoutePages.LazyPage>
            }
          />
          <Route
            path="/network/endpointslices"
            element={
              <RoutePages.LazyPage>
                <RoutePages.NetworkEndpointSlicesPage />
              </RoutePages.LazyPage>
            }
          />
          <Route
            path="/network/ingressclasses"
            element={
              <RoutePages.LazyPage>
                <RoutePages.NetworkIngressClassesPage />
              </RoutePages.LazyPage>
            }
          />
          <Route
            path="/network/networkpolicies"
            element={
              <RoutePages.LazyPage>
                <RoutePages.NetworkPoliciesPage />
              </RoutePages.LazyPage>
            }
          />
          <Route
            path="/network/port-forward"
            element={
              <RoutePages.LazyPage>
                <RoutePages.NetworkPortForwardPage />
              </RoutePages.LazyPage>
            }
          />

          <Route
            path="/storage"
            element={<Navigate to="/storage/persistentvolumeclaims" replace />}
          />
          <Route
            path="/storage/persistentvolumeclaims"
            element={
              <RoutePages.LazyPage>
                <RoutePages.StoragePvcPage />
              </RoutePages.LazyPage>
            }
          />
          <Route
            path="/storage/persistentvolumeclaims/:name"
            element={
              <RoutePages.LazyPage>
                <RoutePages.StoragePvcDetailPage />
              </RoutePages.LazyPage>
            }
          />
          <Route
            path="/storage/persistentvolumes"
            element={
              <RoutePages.LazyPage>
                <RoutePages.StoragePvPage />
              </RoutePages.LazyPage>
            }
          />
          <Route
            path="/storage/persistentvolumes/:name"
            element={
              <RoutePages.LazyPage>
                <RoutePages.StoragePvDetailPage />
              </RoutePages.LazyPage>
            }
          />
          <Route
            path="/storage/storageclasses"
            element={
              <RoutePages.LazyPage>
                <RoutePages.StorageClassesPage />
              </RoutePages.LazyPage>
            }
          />
          <Route
            path="/storage/storageclasses/:name"
            element={
              <RoutePages.LazyPage>
                <RoutePages.StorageClassDetailPage />
              </RoutePages.LazyPage>
            }
          />

          <Route
            path="/extensions"
            element={
              <RoutePages.LazyPage>
                <RoutePages.CRDPage />
              </RoutePages.LazyPage>
            }
          />
          <Route
            path="/extensions/apis/:groupName"
            element={
              <RoutePages.LazyPage>
                <RoutePages.CRDApiGroupDetailPage />
              </RoutePages.LazyPage>
            }
          />
          <Route
            path="/helm"
            element={<Navigate to="/helm/releases" replace />}
          />
          <Route
            path="/helm/releases"
            element={
              <RoutePages.LazyPage>
                <RoutePages.HelmReleasesPage />
              </RoutePages.LazyPage>
            }
          />
          <Route
            path="/helm/releases/:releaseName"
            element={
              <RoutePages.LazyPage>
                <RoutePages.HelmReleaseDetailPage />
              </RoutePages.LazyPage>
            }
          />
          <Route
            path="/helm/charts"
            element={
              <RoutePages.LazyPage>
                <RoutePages.HelmChartsPage />
              </RoutePages.LazyPage>
            }
          />

          <Route
            path="/applications"
            element={
              <RoutePages.LazyPage>
                <RoutePages.ApplicationsPage />
              </RoutePages.LazyPage>
            }
          />
          <Route
            path="/application-management"
            element={<RoutePages.ApplicationManagementRedirect />}
          />
          <Route
            path="/application-management/:applicationId"
            element={<RoutePages.ApplicationManagementRedirect />}
          />
          <Route
            path="/applications/:applicationId"
            element={
              <RoutePages.LazyPage>
                <RoutePages.ApplicationDetailPage />
              </RoutePages.LazyPage>
            }
          />
          <Route
            path="/applications/:applicationId/application-environments/:applicationEnvironmentId/workloads/:workloadName"
            element={
              <RoutePages.LazyPage>
                <RoutePages.ApplicationWorkloadDetailPage />
              </RoutePages.LazyPage>
            }
          />
          <Route
            path="/business-lines"
            element={<Navigate to="/applications" replace />}
          />
          <Route
            path="/delivery-environments"
            element={<Navigate to="/applications" replace />}
          />
          <Route
            path="/application-environments"
            element={
              <RoutePages.LazyPage>
                <RoutePages.ApplicationEnvironmentsPage />
              </RoutePages.LazyPage>
            }
          />
          <Route
            path="/application-environments/:applicationEnvironmentId"
            element={
              <RoutePages.LazyPage>
                <RoutePages.ApplicationEnvironmentDetailPage />
              </RoutePages.LazyPage>
            }
          />
          <Route
            path="/build-templates"
            element={
              <RoutePages.LazyPage>
                <RoutePages.BuildTemplatesPage />
              </RoutePages.LazyPage>
            }
          />
          <Route
            path="/delivery/blueprints"
            element={
              <RoutePages.LazyPage>
                <RoutePages.DeliveryBlueprintsPage />
              </RoutePages.LazyPage>
            }
          />
          <Route
            path="/delivery/release-bundles"
            element={
              <RoutePages.LazyPage>
                <RoutePages.ReleaseBundlesPage />
              </RoutePages.LazyPage>
            }
          />
          <Route
            path="/delivery/execution-tasks"
            element={
              <RoutePages.LazyPage>
                <RoutePages.ExecutionTasksPage />
              </RoutePages.LazyPage>
            }
          />
          <Route
            path="/delivery/approval-policies"
            element={
              <RoutePages.LazyPage>
                <RoutePages.ApprovalPoliciesPage />
              </RoutePages.LazyPage>
            }
          />
          <Route
            path="/workflow-templates"
            element={
              <RoutePages.LazyPage>
                <RoutePages.WorkflowTemplatesPage />
              </RoutePages.LazyPage>
            }
          />
          <Route
            path="/release-board"
            element={
              <RoutePages.LazyPage>
                <RoutePages.ReleaseBoardPage />
              </RoutePages.LazyPage>
            }
          />
          <Route
            path="/workflows"
            element={
              <RoutePages.LazyPage>
                <RoutePages.WorkflowsPage />
              </RoutePages.LazyPage>
            }
          />
          <Route
            path="/releases"
            element={
              <RoutePages.LazyPage>
                <RoutePages.ReleasesPage />
              </RoutePages.LazyPage>
            }
          />
          <Route
            path="/registries"
            element={
              <RoutePages.LazyPage>
                <RoutePages.RegistriesPage />
              </RoutePages.LazyPage>
            }
          />

          <Route
            path="/virtualization"
            element={<Navigate to="/virtualization/overview" replace />}
          />
          <Route
            path="/virtualization/overview"
            element={
              <RoutePages.LazyPage>
                <RoutePages.VirtualizationOverviewPage />
              </RoutePages.LazyPage>
            }
          />
          <Route
            path="/virtualization/vms"
            element={
              <RoutePages.LazyPage>
                <RoutePages.VirtualizationVmsPage />
              </RoutePages.LazyPage>
            }
          />
          <Route
            path="/virtualization/vms/:id"
            element={
              <RoutePages.LazyPage>
                <RoutePages.VirtualizationVmDetailPage />
              </RoutePages.LazyPage>
            }
          />
          <Route
            path="/virtualization/clusters"
            element={
              <RoutePages.LazyPage>
                <RoutePages.VirtualizationClustersPage />
              </RoutePages.LazyPage>
            }
          />
          <Route
            path="/virtualization/images"
            element={
              <RoutePages.LazyPage>
                <RoutePages.VirtualizationImagesPage />
              </RoutePages.LazyPage>
            }
          />
          <Route
            path="/virtualization/flavors"
            element={
              <RoutePages.LazyPage>
                <RoutePages.VirtualizationFlavorsPage />
              </RoutePages.LazyPage>
            }
          />
          <Route
            path="/virtualization/operations"
            element={
              <RoutePages.LazyPage>
                <RoutePages.VirtualizationOperationsPage />
              </RoutePages.LazyPage>
            }
          />
          <Route
            path="/virtualization/sync"
            element={
              <RoutePages.LazyPage>
                <RoutePages.VirtualizationSyncPage />
              </RoutePages.LazyPage>
            }
          />

          <Route
            path="/docker"
            element={<Navigate to="/docker/overview" replace />}
          />
          <Route
            path="/docker/overview"
            element={
              <RoutePages.LazyPage>
                <RoutePages.DockerOverviewPage />
              </RoutePages.LazyPage>
            }
          />
          <Route
            path="/docker/hosts"
            element={
              <RoutePages.LazyPage>
                <RoutePages.DockerHostsPage />
              </RoutePages.LazyPage>
            }
          />
          <Route
            path="/docker/projects"
            element={
              <RoutePages.LazyPage>
                <RoutePages.DockerProjectsPage />
              </RoutePages.LazyPage>
            }
          />
          <Route
            path="/docker/projects/:projectId"
            element={
              <RoutePages.LazyPage>
                <RoutePages.DockerProjectDetailPage />
              </RoutePages.LazyPage>
            }
          />
          <Route
            path="/docker/services"
            element={<Navigate to="/docker/projects" replace />}
          />
          <Route
            path="/docker/ports"
            element={<Navigate to="/docker/projects" replace />}
          />
          <Route
            path="/docker/templates"
            element={
              <RoutePages.LazyPage>
                <RoutePages.DockerTemplatesPage />
              </RoutePages.LazyPage>
            }
          />
          <Route
            path="/docker/operations"
            element={
              <RoutePages.LazyPage>
                <RoutePages.DockerOperationsPage />
              </RoutePages.LazyPage>
            }
          />

          <Route
            path="/monitoring-workbench"
            element={<Navigate to="/monitoring-workbench/overview" replace />}
          />
          <Route
            path="/monitoring-workbench/overview"
            element={
              <RoutePages.LazyPage>
                <RoutePages.MonitoringPage />
              </RoutePages.LazyPage>
            }
          />
          <Route
            path="/monitoring-workbench/integrations"
            element={
              <RoutePages.LazyPage>
                <RoutePages.AlertIntegrationsPage />
              </RoutePages.LazyPage>
            }
          />
          <Route
            path="/monitoring-workbench/rules"
            element={
              <RoutePages.LazyPage>
                <RoutePages.AlertRulesPage />
              </RoutePages.LazyPage>
            }
          />
          <Route
            path="/monitoring-workbench/alerts"
            element={
              <RoutePages.LazyPage>
                <RoutePages.AlertsPage />
              </RoutePages.LazyPage>
            }
          />
          <Route
            path="/monitoring-workbench/alerts/:eventId"
            element={
              <RoutePages.LazyPage>
                <RoutePages.AlertEventDetailPage />
              </RoutePages.LazyPage>
            }
          />
          <Route
            path="/monitoring-workbench/notifications"
            element={
              <RoutePages.LazyPage>
                <RoutePages.NotificationsPage />
              </RoutePages.LazyPage>
            }
          />
          <Route
            path="/monitoring-workbench/healing"
            element={
              <RoutePages.LazyPage>
                <RoutePages.HealingPage />
              </RoutePages.LazyPage>
            }
          />
          <Route
            path="/monitoring-workbench/oncall"
            element={
              <RoutePages.LazyPage>
                <RoutePages.OnCallBoardPage />
              </RoutePages.LazyPage>
            }
          />
          <Route
            path="/monitoring-workbench/oncall/settings"
            element={
              <RoutePages.LazyPage>
                <RoutePages.OnCallSettingsPage />
              </RoutePages.LazyPage>
            }
          />
          <Route
            path="/monitoring-workbench/events"
            element={
              <RoutePages.LazyPage>
                <RoutePages.EventsPage />
              </RoutePages.LazyPage>
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
              <RoutePages.LazyPage>
                <RoutePages.AlertEventDetailPage />
              </RoutePages.LazyPage>
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
            element={<RoutePages.AIWorkbenchModeRedirect />}
          />
          <Route
            path="/ai-workbench/chat"
            element={
              <RoutePages.LazyPage>
                <RoutePages.AIWorkbenchPage />
              </RoutePages.LazyPage>
            }
          />
          <Route
            path="/ai-workbench/investigation"
            element={<RoutePages.AIWorkbenchModeRedirect />}
          />
          <Route
            path="/ai-workbench/root-cause"
            element={
              <RoutePages.LazyPage>
                <RoutePages.AIWorkbenchPage />
              </RoutePages.LazyPage>
            }
          />
          <Route
            path="/ai-workbench/performance"
            element={
              <RoutePages.LazyPage>
                <RoutePages.AIWorkbenchPage />
              </RoutePages.LazyPage>
            }
          />
          <Route
            path="/ai-workbench/inspection"
            element={
              <RoutePages.LazyPage>
                <RoutePages.AIOperationsPage />
              </RoutePages.LazyPage>
            }
          />
          <Route
            path="/ai-workbench/tool-settings"
            element={
              <RoutePages.LazyPage>
                <RoutePages.AIToolsPage />
              </RoutePages.LazyPage>
            }
          />
          <Route
            path="/ai-workbench/model-settings"
            element={
              <RoutePages.LazyPage>
                <RoutePages.AIModelSettingsPage />
              </RoutePages.LazyPage>
            }
          />
          <Route
            path="/ai-gateway"
            element={<RoutePages.AIGatewayRedirect />}
          />
          <Route
            path="/ai-gateway/overview"
            element={
              <RoutePages.LazyPage>
                <RoutePages.AIGatewayPage />
              </RoutePages.LazyPage>
            }
          />
          <Route
            path="/ai-gateway/manifest"
            element={
              <RoutePages.LazyPage>
                <RoutePages.AIGatewayPage />
              </RoutePages.LazyPage>
            }
          />
          <Route
            path="/ai-gateway/clients"
            element={
              <RoutePages.LazyPage>
                <RoutePages.AIGatewayPage />
              </RoutePages.LazyPage>
            }
          />
          <Route
            path="/ai-gateway/tokens"
            element={
              <RoutePages.LazyPage>
                <RoutePages.AIGatewayPage />
              </RoutePages.LazyPage>
            }
          />
          <Route
            path="/ai-gateway/governance"
            element={
              <RoutePages.LazyPage>
                <RoutePages.AIGatewayPage />
              </RoutePages.LazyPage>
            }
          />
          <Route
            path="/ai-gateway/call-logs"
            element={
              <RoutePages.LazyPage>
                <RoutePages.AIGatewayPage />
              </RoutePages.LazyPage>
            }
          />
          <Route
            path="/ai-gateway/*"
            element={<Navigate to="/ai-gateway/overview" replace />}
          />
          <Route
            path="/ai-workbench/gateway"
            element={<RoutePages.AIGatewayRedirect />}
          />
          <Route
            path="/ai-workbench/automation"
            element={<RoutePages.AIWorkbenchOperationsRedirect />}
          />
          <Route
            path="/ai-workbench/tools"
            element={<RoutePages.AIWorkbenchToolsRedirect />}
          />
          <Route
            path="/ai-observe"
            element={<RoutePages.AIWorkbenchModeRedirect />}
          />
          <Route
            path="/ai-observe/workbench"
            element={<RoutePages.AIWorkbenchModeRedirect />}
          />
          <Route
            path="/ai-observe/operations"
            element={<RoutePages.AIWorkbenchOperationsRedirect />}
          />
          <Route
            path="/ai-observe/tools"
            element={<RoutePages.AIWorkbenchToolsRedirect />}
          />
          <Route
            path="/ai-observe/root-cause"
            element={<RoutePages.AIWorkbenchFixedModeRedirect mode="root_cause" />}
          />
          <Route
            path="/ai-observe/performance"
            element={<RoutePages.AIWorkbenchFixedModeRedirect mode="performance" />}
          />
          <Route
            path="/ai-observe/chat"
            element={<RoutePages.AIWorkbenchModeRedirect />}
          />
          <Route
            path="/ai-observe/inspection"
            element={<RoutePages.AIWorkbenchOperationsRedirect />}
          />
          <Route
            path="/chat"
            element={<RoutePages.AIWorkbenchModeRedirect />}
          />

          <Route
            path="/access"
            element={
              <RoutePages.LazyPage>
                <RoutePages.AccessCenterPage />
              </RoutePages.LazyPage>
            }
          />
          <Route
            path="/access/users"
            element={
              <RoutePages.LazyPage>
                <RoutePages.AccessUsersPage />
              </RoutePages.LazyPage>
            }
          />
          <Route
            path="/access/roles"
            element={
              <RoutePages.LazyPage>
                <RoutePages.AccessRolesPage />
              </RoutePages.LazyPage>
            }
          />
          <Route
            path="/access/teams"
            element={
              <RoutePages.LazyPage>
                <RoutePages.AccessTeamsPage />
              </RoutePages.LazyPage>
            }
          />
          <Route
            path="/access/policies"
            element={
              <RoutePages.LazyPage>
                <RoutePages.AccessPoliciesPage />
              </RoutePages.LazyPage>
            }
          />
          <Route
            path="/access/scope-grants"
            element={
              <RoutePages.LazyPage>
                <RoutePages.AccessScopeGrantsPage />
              </RoutePages.LazyPage>
            }
          />

          <Route
            path="/system"
            element={<Navigate to="/system/online-users" replace />}
          />
          <Route
            path="/system/online-users"
            element={
              <RoutePages.LazyPage>
                <RoutePages.OnlineUsersPage />
              </RoutePages.LazyPage>
            }
          />
          <Route
            path="/system/announcements"
            element={
              <RoutePages.LazyPage>
                <RoutePages.AnnouncementsPage />
              </RoutePages.LazyPage>
            }
          />
          <Route
            path="/system/menus"
            element={
              <RoutePages.LazyPage>
                <RoutePages.MenusPage />
              </RoutePages.LazyPage>
            }
          />
          <Route
            path="/system/audit"
            element={
              <RoutePages.LazyPage>
                <RoutePages.AuditLogsPage />
              </RoutePages.LazyPage>
            }
          />
          <Route
            path="/system/operations"
            element={
              <RoutePages.LazyPage>
                <RoutePages.OperationLogsPage />
              </RoutePages.LazyPage>
            }
          />

          <Route
            path="/settings"
            element={
              <RoutePages.LazyPage>
                <RoutePages.SettingsCenterPage />
              </RoutePages.LazyPage>
            }
          />
          <Route
            path="/settings/login"
            element={
              <RoutePages.LazyPage>
                <RoutePages.SettingsCenterPage />
              </RoutePages.LazyPage>
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
              <RoutePages.LazyPage>
                <RoutePages.SettingsCenterPage />
              </RoutePages.LazyPage>
            }
          />
          <Route
            path="/settings/ai"
            element={<RoutePages.AIWorkbenchModelSettingsRedirect />}
          />

          <Route
            path="/account/profile"
            element={
              <RoutePages.LazyPage>
                <RoutePages.UserProfilePage />
              </RoutePages.LazyPage>
            }
          />

          <Route path="*" element={<Navigate to="/" replace />} />
        </Route>
      </Route>
    </Routes>
  );
}
