package resource

import (
	"context"
	"errors"
	"slices"
	"testing"

	appaccess "github.com/opensoha/soha/internal/application/access"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type selectiveHighRiskPermissions map[string]bool

func (p selectiveHighRiskPermissions) Authorize(_ context.Context, _ domainidentity.Principal, permission string) error {
	if p[permission] {
		return nil
	}
	return apperrors.ErrAccessDenied
}

func TestClassifyHighRiskResource(t *testing.T) {
	tests := []struct {
		name        string
		apiVersion  string
		group       string
		resource    string
		kind        string
		wantRisk    ResourceRisk
		permissions []string
	}{
		{name: "ordinary resource", apiVersion: "v1", group: "", resource: "configmaps", kind: "ConfigMap", wantRisk: ResourceRiskNone},
		{name: "legacy role fallback ignores api version spoof", apiVersion: "evil.example/v9", kind: "Role", wantRisk: ResourceRiskAccessEscalation, permissions: []string{appaccess.PlatformActionPermission("access-control", "Role", "create"), appaccess.PermPlatformRBACEscalate}},
		{name: "binding GVR is case insensitive", group: "RBAC.AUTHORIZATION.K8S.IO", resource: " ClusterRoleBindings ", kind: "ignored", wantRisk: ResourceRiskAccessEscalation, permissions: []string{appaccess.PlatformActionPermission("access-control", "ClusterRoleBinding", "create"), appaccess.PermPlatformRBACBind}},
		{name: "custom group kind role is not kubernetes RBAC", group: "example.io", resource: "roles", kind: "Role", wantRisk: ResourceRiskNone},
		{name: "canonical GVR cannot be downgraded by kind", group: "rbac.authorization.k8s.io", resource: "rolebindings", kind: "ConfigMap", wantRisk: ResourceRiskAccessEscalation, permissions: []string{appaccess.PlatformActionPermission("access-control", "RoleBinding", "create"), appaccess.PermPlatformRBACBind}},
		{name: "namespace", group: "", resource: "namespaces", kind: "Namespace", wantRisk: ResourceRiskClusterInfrastructure, permissions: []string{appaccess.PlatformActionPermission("", "Namespace", "create")}},
		{name: "crd name cannot influence kind classification", group: "apiextensions.k8s.io", resource: "customresourcedefinitions", kind: "CustomResourceDefinition", wantRisk: ResourceRiskExtensionDefinition, permissions: []string{appaccess.PlatformActionPermission("extensions", "CustomResourceDefinition", "create")}},
		{name: "mutating admission", group: "admissionregistration.k8s.io", resource: "mutatingwebhookconfigurations", kind: "MutatingWebhookConfiguration", wantRisk: ResourceRiskAdmissionControl, permissions: []string{appaccess.PlatformActionPermission("configuration", "MutatingWebhookConfiguration", "create")}},
		{name: "validating admission", group: "admissionregistration.k8s.io", resource: "validatingwebhookconfigurations", kind: "ValidatingWebhookConfiguration", wantRisk: ResourceRiskAdmissionControl, permissions: []string{appaccess.PlatformActionPermission("configuration", "ValidatingWebhookConfiguration", "create")}},
		{name: "storage class", group: "storage.k8s.io", resource: "storageclasses", kind: "StorageClass", wantRisk: ResourceRiskClusterInfrastructure, permissions: []string{appaccess.PlatformActionPermission("storage", "StorageClass", "create")}},
		{name: "priority class", group: "scheduling.k8s.io", resource: "priorityclasses", kind: "PriorityClass", wantRisk: ResourceRiskClusterInfrastructure, permissions: []string{appaccess.PlatformActionPermission("configuration", "PriorityClass", "create")}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := ClassifyHighRiskResource(test.group, test.resource, test.kind)
			if got.Risk != test.wantRisk {
				t.Fatalf("Risk = %q, want %q", got.Risk, test.wantRisk)
			}
			if !slices.Equal(got.RequiredPermissions, test.permissions) {
				t.Fatalf("RequiredPermissions = %v, want %v", got.RequiredPermissions, test.permissions)
			}
		})
	}
}

func TestHighRiskResourcePolicySecurityMatrix(t *testing.T) {
	allRBAC := selectiveHighRiskPermissions{
		appaccess.PlatformActionPermission("access-control", "Role", "create"):               true,
		appaccess.PlatformActionPermission("access-control", "ClusterRole", "create"):        true,
		appaccess.PlatformActionPermission("access-control", "RoleBinding", "create"):        true,
		appaccess.PlatformActionPermission("access-control", "ClusterRoleBinding", "create"): true,
		appaccess.PermPlatformRBACEscalate:                                                   true,
		appaccess.PermPlatformRBACBind:                                                       true,
	}
	tests := []struct {
		name        string
		permissions selectiveHighRiskPermissions
		request     HighRiskResourceRequest
		wantDenied  bool
	}{
		{name: "config map creator may create ordinary resource", permissions: selectiveHighRiskPermissions{}, request: HighRiskResourceRequest{APIVersion: "v1", Kind: "ConfigMap", Namespace: "minio"}},
		{name: "config map creator cannot create role binding", permissions: selectiveHighRiskPermissions{}, request: HighRiskResourceRequest{APIVersion: "rbac.authorization.k8s.io/v1", Kind: "RoleBinding", Namespace: "minio"}, wantDenied: true},
		{name: "role binding create without bind cannot bind cluster admin", permissions: selectiveHighRiskPermissions{appaccess.PlatformActionPermission("access-control", "RoleBinding", "create"): true}, request: HighRiskResourceRequest{Kind: "RoleBinding", Namespace: "minio", Object: map[string]any{"roleRef": map[string]any{"apiGroup": "rbac.authorization.k8s.io", "kind": "ClusterRole", "name": "cluster-admin"}}}, wantDenied: true},
		{name: "role create without escalate cannot create wildcard role", permissions: selectiveHighRiskPermissions{appaccess.PlatformActionPermission("access-control", "Role", "create"): true}, request: HighRiskResourceRequest{Kind: "Role", Namespace: "minio", Object: map[string]any{"rules": []any{map[string]any{"apiGroups": []any{"*"}, "resources": []any{"*"}, "verbs": []any{"*"}}}}}, wantDenied: true},
		{name: "namespace A operator cannot target namespace B", permissions: allRBAC, request: HighRiskResourceRequest{Kind: "RoleBinding", Namespace: "minio", Object: map[string]any{"metadata": map[string]any{"namespace": "ops"}}}, wantDenied: true},
		{name: "namespace A operator cannot inject role namespace B", permissions: allRBAC, request: HighRiskResourceRequest{Kind: "RoleBinding", Namespace: "minio", Object: map[string]any{"roleRef": map[string]any{"kind": "Role", "name": "writer", "namespace": "ops"}}}, wantDenied: true},
		{name: "cluster role binding needs cluster bind permission", permissions: selectiveHighRiskPermissions{appaccess.PlatformActionPermission("access-control", "ClusterRoleBinding", "create"): true}, request: HighRiskResourceRequest{Kind: "ClusterRoleBinding"}, wantDenied: true},
		{name: "cluster role binding allowed with create and bind", permissions: allRBAC, request: HighRiskResourceRequest{Kind: "ClusterRoleBinding"}},
		{name: "custom api version cannot bypass role policy", permissions: selectiveHighRiskPermissions{}, request: HighRiskResourceRequest{APIVersion: "attacker.example/v1", Kind: "rOlE", Namespace: "minio"}, wantDenied: true},
		{name: "canonical RBAC GVR cannot be hidden by kind", permissions: selectiveHighRiskPermissions{}, request: HighRiskResourceRequest{Group: "rbac.authorization.k8s.io", Resource: "roles", Kind: "ConfigMap", Namespace: "minio"}, wantDenied: true},
		{name: "custom resource named Role is not Kubernetes RBAC", permissions: selectiveHighRiskPermissions{}, request: HighRiskResourceRequest{Group: "example.io", Resource: "roles", Kind: "Role", Namespace: "minio"}},
		{name: "custom CRD name cannot bypass CRD policy", permissions: selectiveHighRiskPermissions{}, request: HighRiskResourceRequest{APIVersion: "attacker.example/v1", Kind: "CustomResourceDefinition", Object: map[string]any{"metadata": map[string]any{"name": "configmaps.attacker.example"}}}, wantDenied: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := NewHighRiskResourcePolicy(test.permissions).Check(context.Background(), domainidentity.Principal{UserID: "user-1"}, test.request)
			if test.wantDenied && !errors.Is(err, apperrors.ErrAccessDenied) {
				t.Fatalf("Check() error = %v, want access denied", err)
			}
			if !test.wantDenied && err != nil {
				t.Fatalf("Check() error = %v, want nil", err)
			}
		})
	}
}

func TestHighRiskResourcePolicyRechecksRevokedPermission(t *testing.T) {
	permissions := selectiveHighRiskPermissions{
		appaccess.PlatformActionPermission("access-control", "ClusterRoleBinding", "create"): true,
		appaccess.PermPlatformRBACBind: true,
	}
	policy := NewHighRiskResourcePolicy(permissions)
	request := HighRiskResourceRequest{Kind: "ClusterRoleBinding"}
	if err := policy.Check(context.Background(), domainidentity.Principal{UserID: "user-1"}, request); err != nil {
		t.Fatalf("preflight Check() error = %v, want nil", err)
	}
	delete(permissions, appaccess.PermPlatformRBACBind)
	if err := policy.Check(context.Background(), domainidentity.Principal{UserID: "user-1"}, request); !errors.Is(err, apperrors.ErrAccessDenied) {
		t.Fatalf("execute Check() error = %v after revocation, want access denied", err)
	}
}

func TestHighRiskResourcePolicyFailsClosedWithoutPermissionResolver(t *testing.T) {
	err := NewHighRiskResourcePolicy(nil).Check(context.Background(), domainidentity.Principal{}, HighRiskResourceRequest{Kind: "Namespace"})
	if !errors.Is(err, apperrors.ErrAccessDenied) {
		t.Fatalf("Check() error = %v, want access denied", err)
	}
}
