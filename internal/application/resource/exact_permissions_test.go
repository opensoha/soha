package resource

import (
	"context"
	"slices"
	"testing"

	appaccess "github.com/opensoha/soha/internal/application/access"
	domainaccess "github.com/opensoha/soha/internal/domain/access"
	domaincluster "github.com/opensoha/soha/internal/domain/cluster"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
)

type exactPermissionAuthorizer map[string]bool

func (a exactPermissionAuthorizer) Authorize(_ context.Context, request domainaccess.Request) (domainaccess.Decision, error) {
	return domainaccess.Decision{Allowed: a[request.PermissionKey]}, nil
}

func TestPodAllowedActionsUseExactPermissionKeys(t *testing.T) {
	access := &resourceAccess{authorizer: exactPermissionAuthorizer{
		appaccess.PermPlatformPodsView:   true,
		appaccess.PermPlatformPodsLogs:   true,
		appaccess.PermPlatformPodsUpdate: true,
	}}
	actions := access.allowedActionsForResource(context.Background(), domainidentity.Principal{}, domaincluster.Connection{}, "team-a", "Pod", domainaccess.ActionView)
	if len(actions) != 3 || actions[0] != "view" || actions[1] != "logs" || actions[2] != "update" {
		t.Fatalf("allowed actions = %#v, want [view logs update]", actions)
	}
	if key := resourcePermissionKey("workloads", "Deployment", domainaccess.ActionDelete); key != appaccess.PermPlatformDeploymentDelete {
		t.Fatalf("deployment delete permission = %q", key)
	}
}

func TestWorkloadCronJobUsesCustomResourceCreatePermission(t *testing.T) {
	t.Parallel()

	group := resourceGroupForKind("WorkloadCronJob")
	if group != "extensions" {
		t.Fatalf("resource group = %q, want extensions", group)
	}
	const want = "platform.extensions.custom-resources.create"
	if key := resourcePermissionKey(group, "WorkloadCronJob", domainaccess.ActionCreate); key != want {
		t.Fatalf("create permission = %q, want %q", key, want)
	}
}

func TestKubernetesResourceReadsUseExactPermissionKeys(t *testing.T) {
	t.Parallel()

	tests := []struct {
		group string
		kind  string
		want  string
	}{
		{"workloads", "ReplicaSet", "platform.workloads.replica-sets.view"},
		{"workloads", "ReplicationController", "platform.workloads.replication-controllers.view"},
		{"workloads", "StatefulSet", "platform.workloads.stateful-sets.view"},
		{"workloads", "DaemonSet", "platform.workloads.daemon-sets.view"},
		{"workloads", "Job", "platform.workloads.jobs.view"},
		{"workloads", "CronJob", "platform.workloads.cron-jobs.view"},
		{"workloads", "Event", "platform.workloads.overview.view"},
		{"configuration", "ConfigMap", "platform.configuration.config-maps.view"},
		{"configuration", "Secret", "platform.configuration.secrets.view"},
		{"network", "Service", "platform.network.services.view"},
		{"network", "Ingress", "platform.network.ingresses.view"},
		{"network", "PortForward", "platform.network.port-forwards.view"},
		{"network", "NetworkTopology", "platform.network.topology.view"},
		{"network", "HTTPRoute", "platform.network.http-routes.view"},
		{"network", "GRPCRoute", "platform.network.grpc-routes.view"},
		{"network", "BackendTLSPolicy", "platform.network.backend-tls-policies.view"},
		{"storage", "PersistentVolumeClaim", "platform.storage.persistent-volume-claims.view"},
		{"storage", "PersistentVolume", "platform.storage.persistent-volumes.view"},
		{"access-control", "ServiceAccount", "platform.access-control.service-accounts.view"},
		{"access-control", "Role", "platform.access-control.roles.view"},
		{"extensions", "CustomResourceDefinition", "platform.extensions.view"},
	}
	for _, tt := range tests {
		t.Run(tt.kind, func(t *testing.T) {
			if got := resourcePermissionKey(tt.group, tt.kind, domainaccess.ActionView); got != tt.want {
				t.Fatalf("resourcePermissionKey(%q, %q, view) = %q, want %q", tt.group, tt.kind, got, tt.want)
			}
		})
	}
}

func TestKubernetesReadAuthorizationPopulatesRowActions(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		resourceGroup string
		kind          string
		permissions   []string
		wantActions   []string
	}{
		{"workload", "workloads", "StatefulSet", []string{"platform.workloads.stateful-sets.view", "platform.workloads.stateful-sets.restart", "platform.workloads.stateful-sets.delete"}, []string{"view", "delete", "restart"}},
		{"configuration", "configuration", "ConfigMap", []string{"platform.configuration.config-maps.view", "platform.configuration.config-maps.delete"}, []string{"view", "delete"}},
		{"network", "network", "Service", []string{"platform.network.services.view", "platform.network.services.delete"}, []string{"view", "delete"}},
		{"storage", "storage", "PersistentVolumeClaim", []string{"platform.storage.persistent-volume-claims.view", "platform.storage.persistent-volume-claims.delete"}, []string{"view", "delete"}},
		{"rbac", "access-control", "Role", []string{"platform.access-control.roles.view", "platform.access-control.roles.delete"}, []string{"view", "delete"}},
		{"node", "inventory", "Node", []string{"platform.nodes.view", "platform.nodes.update", "platform.nodes.delete"}, []string{"view", "update", "delete"}},
		{"custom resource", "extensions", "Addon", []string{"platform.extensions.view", "platform.extensions.custom-resources.delete"}, []string{"view", "delete"}},
		{"helm release", "extensions", "HelmRelease", []string{"platform.helm.view", "platform.helm.releases.update", "platform.helm.releases.delete"}, []string{"view", "update", "delete"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			permissions := exactPermissionAuthorizer{}
			for _, permission := range tt.permissions {
				permissions[permission] = true
			}
			access := &resourceAccess{
				resolver:   stubConnectionResolver{connection: domaincluster.Connection{Summary: domaincluster.Summary{ID: "cluster-a"}}},
				authorizer: permissions,
			}
			_, decision, err := access.authorizeResourceGroup(context.Background(), domainidentity.Principal{}, "cluster-a", "team-a", tt.resourceGroup, tt.kind, domainaccess.ActionList)
			if err != nil {
				t.Fatalf("authorizeResourceGroup() error = %v", err)
			}
			if got := stringifyActions(decision.AllowedActions); !slices.Equal(got, tt.wantActions) {
				t.Fatalf("allowed actions = %#v, want %#v", got, tt.wantActions)
			}
		})
	}
}

func TestCustomResourcesUseExtensionsPermissionGroup(t *testing.T) {
	t.Parallel()

	customResources := &CustomResources{resourceAccess: &resourceAccess{
		resolver: stubConnectionResolver{connection: domaincluster.Connection{Summary: domaincluster.Summary{ID: "cluster-a"}}},
		authorizer: exactPermissionAuthorizer{
			appaccess.PermPlatformExtensionsView: true,
		},
	}}
	if _, _, err := customResources.authorizeCustomResourceAccess(context.Background(), domainidentity.Principal{}, "cluster-a", "team-a", "Addon", domainaccess.ActionList); err != nil {
		t.Fatalf("authorizeCustomResourceAccess() error = %v", err)
	}
}
