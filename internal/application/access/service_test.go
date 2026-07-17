package access

import (
	"context"
	"slices"
	"strings"
	"testing"

	domainaccess "github.com/opensoha/soha/internal/domain/access"
	domaincatalog "github.com/opensoha/soha/internal/domain/catalog"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainscopegrant "github.com/opensoha/soha/internal/domain/scopegrant"
	"github.com/opensoha/soha/internal/policy"
)

type stubScopeGrantReader struct {
	items []domainscopegrant.Record
}

func TestAdminRoleContainsDirectoryPermissions(t *testing.T) {
	permissions := defaultRolePermissions()["admin"]
	for _, key := range []string{PermAccessDirectoryView, PermAccessDirectoryManage, PermAccessDirectorySync, PermAccessDirectoryPeopleManage, PermAccessIdentityLinkManage} {
		if !slices.Contains(permissions, key) {
			t.Fatalf("admin permissions missing %q", key)
		}
	}
}

func TestGlobalResourceCreateEntryPermissionDefaultsToAdminAndOps(t *testing.T) {
	SetRolePermissionMatrix(nil)
	for _, role := range []string{"admin", "ops"} {
		if !HasPermission([]string{role}, PermPlatformResourceCreate) {
			t.Fatalf("%s should include %s", role, PermPlatformResourceCreate)
		}
	}
	for _, role := range []string{"developer", "tester", "readonly", "auditor"} {
		if HasPermission([]string{role}, PermPlatformResourceCreate) {
			t.Fatalf("%s must not include %s", role, PermPlatformResourceCreate)
		}
	}
}

func (s stubScopeGrantReader) List(context.Context) ([]domainscopegrant.Record, error) {
	return s.items, nil
}

type stubCatalogReader struct {
	environments            []domaincatalog.Environment
	applicationEnvironments []domaincatalog.ApplicationEnvironment
}

func (s stubCatalogReader) ListEnvironments(context.Context) ([]domaincatalog.Environment, error) {
	return s.environments, nil
}

func (s stubCatalogReader) ListApplicationEnvironments(context.Context) ([]domaincatalog.ApplicationEnvironment, error) {
	return s.applicationEnvironments, nil
}

func TestRoleMatrixIncludesTesterAsViewOnlyRole(t *testing.T) {
	matrix := RoleMatrix()
	actions := matrix["tester"]
	if len(actions) == 0 {
		t.Fatal("RoleMatrix missing tester role")
	}
	for _, action := range []domainaccess.Action{
		domainaccess.ActionView,
		domainaccess.ActionList,
		domainaccess.ActionWatch,
		domainaccess.ActionLogs,
	} {
		if !slices.Contains(actions, action) {
			t.Fatalf("tester role actions = %v, missing %s", actions, action)
		}
	}
	for _, action := range []domainaccess.Action{
		domainaccess.ActionCreate,
		domainaccess.ActionUpdate,
		domainaccess.ActionDelete,
		domainaccess.ActionTrigger,
		domainaccess.ActionRollback,
	} {
		if slices.Contains(actions, action) {
			t.Fatalf("tester role actions = %v, should not include %s", actions, action)
		}
	}
}

func TestDefaultPoliciesIncludeTesterViewPolicy(t *testing.T) {
	for _, policy := range DefaultPolicies() {
		if policy.ID != "tester-view" {
			continue
		}
		if !slices.Contains(policy.Subjects.Roles, "tester") {
			t.Fatalf("tester-view subjects = %v, want tester role", policy.Subjects.Roles)
		}
		if slices.Contains(policy.Actions, domainaccess.ActionTrigger) || slices.Contains(policy.Actions, domainaccess.ActionUpdate) {
			t.Fatalf("tester-view actions should stay view-only: %v", policy.Actions)
		}
		return
	}
	t.Fatal("DefaultPolicies missing tester-view")
}

func TestAuthorizeAppliesScopeGrantToPlatformNamespaces(t *testing.T) {
	service := New(
		policy.NewEngine(),
		nil,
		stubScopeGrantReader{items: []domainscopegrant.Record{
			{
				ID:             "grant-1",
				SubjectType:    "team",
				SubjectID:      "team-a",
				BusinessLineID: "bl-retail",
				EnvironmentIDs: []string{"env-dev"},
				Role:           "developer",
				Effect:         "allow",
				Enabled:        true,
			},
		}},
		stubCatalogReader{
			environments: []domaincatalog.Environment{
				{ID: "env-dev", Key: "dev"},
			},
			applicationEnvironments: []domaincatalog.ApplicationEnvironment{
				{
					ID:             "ae-1",
					ApplicationID:  "app-1",
					BusinessLineID: "bl-retail",
					EnvironmentID:  "env-dev",
					EnvironmentKey: "dev",
					Targets: []domaincatalog.ReleaseTarget{
						{ClusterID: "cluster-a", Namespace: "erp-front", WorkloadKind: "Deployment", WorkloadName: "erp-front-web", Enabled: true},
						{ClusterID: "cluster-a", Namespace: "erp-api", WorkloadKind: "Deployment", WorkloadName: "erp-api-web", Enabled: true},
					},
				},
			},
		},
	)

	decision, err := service.Authorize(context.Background(), domainaccess.Request{
		Principal: domainidentity.Principal{
			UserID: "user-1",
			Roles:  []string{"admin"},
			Teams:  []string{"team-a"},
		},
		Action: domainaccess.ActionList,
		Subject: domainaccess.SubjectAttributes{
			UserID: "user-1",
			Roles:  []string{"admin"},
			Teams:  []string{"team-a"},
		},
		Cluster: domainaccess.ClusterAttributes{
			ClusterID:   "cluster-a",
			Environment: "production",
		},
		Resource: domainaccess.ResourceAttributes{Kind: "Namespace"},
	})
	if err != nil {
		t.Fatalf("Authorize returned error: %v", err)
	}
	if !decision.Allowed {
		t.Fatalf("decision.Allowed = false, reason=%q", decision.Reason)
	}
	if decision.ResourceScope == nil {
		t.Fatalf("decision.ResourceScope = nil, want namespace scope")
	}
	if len(decision.ResourceScope.Namespaces) != 2 {
		t.Fatalf("decision.ResourceScope.Namespaces = %v, want 2 namespaces", decision.ResourceScope.Namespaces)
	}
}

func TestAuthorizeAppliesScopeGrantToPlatformNamespacesByApplicationGroup(t *testing.T) {
	service := New(
		policy.NewEngine(),
		nil,
		stubScopeGrantReader{items: []domainscopegrant.Record{
			{
				ID:             "grant-1",
				SubjectType:    "user",
				SubjectID:      "user-1",
				BusinessLineID: "retail",
				Role:           "readonly",
				Effect:         "allow",
				Enabled:        true,
			},
		}},
		stubCatalogReader{
			applicationEnvironments: []domaincatalog.ApplicationEnvironment{
				{
					ID:               "ae-1",
					ApplicationID:    "app-1",
					ApplicationGroup: "core, retail",
					Targets: []domaincatalog.ReleaseTarget{
						{ClusterID: "cluster-a", Namespace: "retail-web", WorkloadKind: "Deployment", WorkloadName: "retail-web", Enabled: true},
					},
				},
			},
		},
	)

	decision, err := service.Authorize(context.Background(), domainaccess.Request{
		Principal: domainidentity.Principal{
			UserID: "user-1",
			Roles:  []string{"admin"},
		},
		Action: domainaccess.ActionList,
		Subject: domainaccess.SubjectAttributes{
			UserID: "user-1",
			Roles:  []string{"admin"},
		},
		Cluster:  domainaccess.ClusterAttributes{ClusterID: "cluster-a"},
		Resource: domainaccess.ResourceAttributes{Kind: "Namespace"},
	})
	if err != nil {
		t.Fatalf("Authorize returned error: %v", err)
	}
	if !decision.Allowed {
		t.Fatalf("decision.Allowed = false, reason=%q", decision.Reason)
	}
	if decision.ResourceScope == nil || len(decision.ResourceScope.Namespaces) != 1 || decision.ResourceScope.Namespaces[0] != "retail-web" {
		var namespaces []string
		if decision.ResourceScope != nil {
			namespaces = decision.ResourceScope.Namespaces
		}
		t.Fatalf("decision.ResourceScope.Namespaces = %v, want [retail-web]", namespaces)
	}
}

func TestAuthorizeDeniesPlatformClusterOutsideScopeGrant(t *testing.T) {
	service := New(
		policy.NewEngine(),
		nil,
		stubScopeGrantReader{items: []domainscopegrant.Record{
			{
				ID:             "grant-1",
				SubjectType:    "user",
				SubjectID:      "user-1",
				BusinessLineID: "bl-retail",
				Role:           "readonly",
				Effect:         "allow",
				Enabled:        true,
			},
		}},
		stubCatalogReader{
			applicationEnvironments: []domaincatalog.ApplicationEnvironment{
				{
					ID:             "ae-1",
					ApplicationID:  "app-1",
					BusinessLineID: "bl-retail",
					Targets: []domaincatalog.ReleaseTarget{
						{ClusterID: "cluster-a", Namespace: "erp-front", WorkloadKind: "Deployment", WorkloadName: "erp-front-web", Enabled: true},
					},
				},
			},
		},
	)

	decision, err := service.Authorize(context.Background(), domainaccess.Request{
		Principal: domainidentity.Principal{
			UserID: "user-1",
			Roles:  []string{"admin"},
		},
		Action: domainaccess.ActionView,
		Subject: domainaccess.SubjectAttributes{
			UserID: "user-1",
			Roles:  []string{"admin"},
		},
		Cluster: domainaccess.ClusterAttributes{
			ClusterID: "cluster-b",
		},
		Resource: domainaccess.ResourceAttributes{Kind: "Cluster", Name: "cluster-b"},
	})
	if err != nil {
		t.Fatalf("Authorize returned error: %v", err)
	}
	if decision.Allowed {
		t.Fatalf("decision.Allowed = true, want false")
	}
}

func TestAuthorizeFiltersPlatformActionsByScopeGrantRole(t *testing.T) {
	service := New(
		policy.NewEngine(),
		nil,
		stubScopeGrantReader{items: []domainscopegrant.Record{
			{
				ID:             "grant-1",
				SubjectType:    "user",
				SubjectID:      "user-1",
				BusinessLineID: "bl-retail",
				Role:           "readonly",
				Effect:         "allow",
				Enabled:        true,
			},
		}},
		stubCatalogReader{
			applicationEnvironments: []domaincatalog.ApplicationEnvironment{
				{
					ID:             "ae-1",
					ApplicationID:  "app-1",
					BusinessLineID: "bl-retail",
					Targets: []domaincatalog.ReleaseTarget{
						{ClusterID: "cluster-a", Namespace: "erp-front", WorkloadKind: "Deployment", WorkloadName: "erp-front-web", Enabled: true},
					},
				},
			},
		},
	)

	decision, err := service.Authorize(context.Background(), domainaccess.Request{
		Principal: domainidentity.Principal{
			UserID: "user-1",
			Roles:  []string{"admin"},
		},
		Action: domainaccess.ActionDelete,
		Subject: domainaccess.SubjectAttributes{
			UserID: "user-1",
			Roles:  []string{"admin"},
		},
		Cluster: domainaccess.ClusterAttributes{
			ClusterID:   "cluster-a",
			Environment: "development",
		},
		Namespace: domainaccess.NamespaceAttributes{Namespace: "erp-front"},
		Resource:  domainaccess.ResourceAttributes{Kind: "Namespace"},
	})
	if err != nil {
		t.Fatalf("Authorize returned error: %v", err)
	}
	if decision.Allowed {
		t.Fatalf("decision.Allowed = true, want false for readonly platform scope")
	}
}

func TestAuthorizeAllowsDeveloperTriggerActionWithinScopedGrant(t *testing.T) {
	assertDeveloperTriggerAllowed(t, "bl-retail", domainaccess.DeliveryAttributes{
		BusinessLineID: "bl-retail", EnvironmentKey: "dev", ApplicationID: "app-1",
	})
}

func TestAuthorizeAllowsApplicationUpdateWithinApplicationGroupScopeGrant(t *testing.T) {
	service := New(
		policy.NewEngine(),
		nil,
		stubScopeGrantReader{items: []domainscopegrant.Record{
			{
				ID:             "grant-1",
				SubjectType:    "user",
				SubjectID:      "user-1",
				BusinessLineID: "retail",
				Role:           "ops",
				Effect:         "allow",
				Enabled:        true,
			},
		}},
		nil,
	)

	decision, err := service.Authorize(context.Background(), domainaccess.Request{
		Principal: domainidentity.Principal{
			UserID: "user-1",
			Roles:  []string{"ops"},
		},
		Action: domainaccess.ActionUpdate,
		Subject: domainaccess.SubjectAttributes{
			UserID: "user-1",
			Roles:  []string{"ops"},
		},
		Resource: domainaccess.ResourceAttributes{Kind: "Application", Name: "new-app"},
		Delivery: domainaccess.DeliveryAttributes{
			ApplicationGroup: "core, retail",
		},
	})
	if err != nil {
		t.Fatalf("Authorize returned error: %v", err)
	}
	if !decision.Allowed {
		t.Fatalf("decision.Allowed = false, reason=%q", decision.Reason)
	}
}

func TestAuthorizeAllowsDeveloperTriggerActionWithinApplicationGroupScopeGrant(t *testing.T) {
	assertDeveloperTriggerAllowed(t, "retail", domainaccess.DeliveryAttributes{
		ApplicationGroup: "core, retail", EnvironmentKey: "dev", ApplicationID: "app-1",
	})
}

func assertDeveloperTriggerAllowed(t *testing.T, grantBusinessLine string, delivery domainaccess.DeliveryAttributes) {
	t.Helper()
	service := New(
		policy.NewEngine(),
		nil,
		stubScopeGrantReader{items: []domainscopegrant.Record{
			{
				ID:             "grant-1",
				SubjectType:    "user",
				SubjectID:      "user-1",
				BusinessLineID: grantBusinessLine,
				EnvironmentIDs: []string{"env-dev"},
				ApplicationIDs: []string{"app-1"},
				Role:           "developer",
				Effect:         "allow",
				Enabled:        true,
			},
		}},
		stubCatalogReader{
			environments: []domaincatalog.Environment{
				{ID: "env-dev", Key: "dev"},
			},
		},
	)

	decision, err := service.Authorize(context.Background(), domainaccess.Request{
		Principal: domainidentity.Principal{
			UserID: "user-1",
			Roles:  []string{"developer"},
		},
		Action: domainaccess.ActionTrigger,
		Subject: domainaccess.SubjectAttributes{
			UserID: "user-1",
			Roles:  []string{"developer"},
		},
		Resource: domainaccess.ResourceAttributes{Kind: "Workflow", Name: "app-1"},
		Delivery: delivery,
	})
	if err != nil {
		t.Fatalf("Authorize returned error: %v", err)
	}
	if !decision.Allowed {
		t.Fatalf("decision.Allowed = false, reason=%q", decision.Reason)
	}
}

func TestAuthorizeAllowsDeveloperRollbackWithinPlatformScopeGrant(t *testing.T) {
	service := New(
		policy.NewEngine(),
		nil,
		stubScopeGrantReader{items: []domainscopegrant.Record{
			{
				ID:             "grant-1",
				SubjectType:    "user",
				SubjectID:      "user-1",
				BusinessLineID: "bl-retail",
				Role:           "developer",
				Effect:         "allow",
				Enabled:        true,
			},
		}},
		stubCatalogReader{
			applicationEnvironments: []domaincatalog.ApplicationEnvironment{
				{
					ID:             "ae-1",
					ApplicationID:  "app-1",
					BusinessLineID: "bl-retail",
					Targets: []domaincatalog.ReleaseTarget{
						{ClusterID: "cluster-a", Namespace: "erp-front", WorkloadKind: "Deployment", WorkloadName: "erp-front-web", Enabled: true},
					},
				},
			},
		},
	)

	decision, err := service.Authorize(context.Background(), domainaccess.Request{
		Principal: domainidentity.Principal{
			UserID: "user-1",
			Roles:  []string{"developer"},
		},
		Action: domainaccess.ActionRollback,
		Subject: domainaccess.SubjectAttributes{
			UserID: "user-1",
			Roles:  []string{"developer"},
		},
		Cluster: domainaccess.ClusterAttributes{
			ClusterID:   "cluster-a",
			Environment: "development",
		},
		Namespace: domainaccess.NamespaceAttributes{Namespace: "erp-front"},
		Resource:  domainaccess.ResourceAttributes{Kind: "Deployment", Name: "erp-front-web"},
	})
	if err != nil {
		t.Fatalf("Authorize returned error: %v", err)
	}
	if !decision.Allowed {
		t.Fatalf("decision.Allowed = false, reason=%q", decision.Reason)
	}
}

func TestAuthorizeAllowsUnknownPlatformKindWhenRBACAndABACMatch(t *testing.T) {
	service := New(policy.NewEngine(), nil, nil, nil)

	decision, err := service.Authorize(context.Background(), domainaccess.Request{
		Principal: domainidentity.Principal{
			UserID: "user-1",
			Roles:  []string{"ops"},
		},
		Action: domainaccess.ActionCreate,
		Subject: domainaccess.SubjectAttributes{
			UserID: "user-1",
			Roles:  []string{"ops"},
		},
		Cluster: domainaccess.ClusterAttributes{
			ClusterID:   "cluster-a",
			Environment: "development",
		},
		Namespace: domainaccess.NamespaceAttributes{Namespace: "team-a"},
		Resource:  domainaccess.ResourceAttributes{Kind: "Widget"},
	})
	if err != nil {
		t.Fatalf("Authorize returned error: %v", err)
	}
	if !decision.Allowed {
		t.Fatalf("decision.Allowed = false, reason=%q", decision.Reason)
	}
	if len(decision.AllowedActions) == 0 {
		t.Fatal("decision.AllowedActions is empty, want effective actions for custom resource kind")
	}
}

func TestAuthorizeDirectPlatformGrantsUnionUserAndTeamNamespaces(t *testing.T) {
	service := New(policy.NewEngine(), nil, stubScopeGrantReader{items: []domainscopegrant.Record{
		{
			ID: "user-grant", SubjectType: "user", SubjectID: "user-1", ScopeType: domainscopegrant.ScopeTypePlatform,
			ClusterIDs: []string{"cluster-a"}, Namespaces: []string{"minio"}, ResourceGroups: []string{"configuration"},
			Role: "ops", Effect: "allow", Enabled: true,
		},
		{
			ID: "team-grant", SubjectType: "team", SubjectID: "team-a", ScopeType: domainscopegrant.ScopeTypePlatform,
			ClusterIDs: []string{"cluster-a"}, Namespaces: []string{"ops"}, ResourceKinds: []string{"ConfigMap"},
			Role: "ops", Effect: "allow", Enabled: true,
		},
	}}, nil)

	decision, err := service.Authorize(context.Background(), platformRequest(
		domainidentity.Principal{UserID: "user-1", Roles: []string{"admin"}, Teams: []string{"team-a"}},
		domainaccess.ActionList, "", "configuration", "ConfigMap",
	))
	if err != nil {
		t.Fatalf("Authorize returned error: %v", err)
	}
	if !decision.Allowed {
		t.Fatalf("decision.Allowed = false, reason=%q", decision.Reason)
	}
	if decision.ResourceScope == nil || !slices.Equal(decision.ResourceScope.Namespaces, []string{"minio", "ops"}) {
		t.Fatalf("decision.ResourceScope.Namespaces = %#v, want [minio ops]", decision.ResourceScope)
	}
}

func TestAuthorizeDirectPlatformKindDenyTakesPriority(t *testing.T) {
	service := New(policy.NewEngine(), nil, stubScopeGrantReader{items: []domainscopegrant.Record{
		{
			ID: "allow", SubjectType: "user", SubjectID: "user-1", ScopeType: domainscopegrant.ScopeTypePlatform,
			ClusterIDs: []string{"cluster-a"}, Namespaces: []string{"minio", "ops"}, ResourceGroups: []string{"configuration"},
			Role: "ops", Effect: "allow", Enabled: true,
		},
		{
			ID: "deny", SubjectType: "team", SubjectID: "team-a", ScopeType: domainscopegrant.ScopeTypePlatform,
			ClusterIDs: []string{"cluster-a"}, Namespaces: []string{"ops"}, ResourceKinds: []string{"ConfigMap"},
			Role: "ops", Effect: "deny", Enabled: true,
		},
	}}, nil)

	decision, err := service.Authorize(context.Background(), platformRequest(
		domainidentity.Principal{UserID: "user-1", Roles: []string{"admin"}, Teams: []string{"team-a"}},
		domainaccess.ActionCreate, "ops", "configuration", "ConfigMap",
	))
	if err != nil {
		t.Fatalf("Authorize returned error: %v", err)
	}
	if decision.Allowed || !strings.Contains(decision.Reason, "explicitly denies") {
		t.Fatalf("decision = %#v, want explicit deny", decision)
	}
}

func TestAuthorizeDirectPlatformCreateActionUsesGrantRoleIntersection(t *testing.T) {
	tests := []struct {
		name string
		role string
		want bool
	}{
		{name: "ops can create", role: "ops", want: true},
		{name: "readonly cannot create", role: "readonly", want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			service := New(policy.NewEngine(), nil, stubScopeGrantReader{items: []domainscopegrant.Record{{
				ID: "grant", SubjectType: "user", SubjectID: "user-1", ScopeType: domainscopegrant.ScopeTypePlatform,
				ClusterIDs: []string{"cluster-a"}, Namespaces: []string{"minio"}, ResourceKinds: []string{"ConfigMap"},
				Role: tt.role, Effect: "allow", Enabled: true,
			}}}, nil)
			decision, err := service.Authorize(context.Background(), platformRequest(
				domainidentity.Principal{UserID: "user-1", Roles: []string{"admin"}},
				domainaccess.ActionCreate, "minio", "configuration", "ConfigMap",
			))
			if err != nil {
				t.Fatalf("Authorize returned error: %v", err)
			}
			if decision.Allowed != tt.want {
				t.Fatalf("decision.Allowed = %v, reason=%q, want %v", decision.Allowed, decision.Reason, tt.want)
			}
		})
	}
}

func TestNamespaceDiscoveryDoesNotGrantNamespaceMutationFromOtherResourceGroup(t *testing.T) {
	service := New(policy.NewEngine(), nil, stubScopeGrantReader{items: []domainscopegrant.Record{{
		ID: "grant", SubjectType: "user", SubjectID: "user-1", ScopeType: domainscopegrant.ScopeTypePlatform,
		ClusterIDs: []string{"cluster-a"}, Namespaces: []string{"minio"}, ResourceGroups: []string{"configuration"},
		Role: "ops", Effect: "allow", Enabled: true,
	}}}, nil)
	request := platformRequest(domainidentity.Principal{UserID: "user-1", Roles: []string{"admin"}}, domainaccess.ActionList, "", "inventory", "Namespace")
	decision, err := service.Authorize(context.Background(), request)
	if err != nil {
		t.Fatalf("Authorize returned error: %v", err)
	}
	if !decision.Allowed || !slices.Contains(decision.AllowedActions, domainaccess.ActionList) {
		t.Fatalf("decision = %#v, want namespace list", decision)
	}
	if slices.Contains(decision.AllowedActions, domainaccess.ActionCreate) || slices.Contains(decision.AllowedActions, domainaccess.ActionDelete) {
		t.Fatalf("namespace discovery leaked mutation actions: %v", decision.AllowedActions)
	}
}

func platformRequest(principal domainidentity.Principal, action domainaccess.Action, namespace, group, kind string) domainaccess.Request {
	return domainaccess.Request{
		Principal: principal,
		Action:    action,
		Subject: domainaccess.SubjectAttributes{
			UserID: principal.UserID, Roles: principal.Roles, Teams: principal.Teams,
		},
		Cluster:   domainaccess.ClusterAttributes{ClusterID: "cluster-a", Environment: "development"},
		Namespace: domainaccess.NamespaceAttributes{Namespace: namespace},
		Resource:  domainaccess.ResourceAttributes{Group: group, Kind: kind},
	}
}
