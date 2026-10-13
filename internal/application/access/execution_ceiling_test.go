package access

import (
	"context"
	"testing"

	domainaccess "github.com/opensoha/soha/internal/domain/access"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainscopegrant "github.com/opensoha/soha/internal/domain/scopegrant"
	"github.com/opensoha/soha/internal/policy"
)

func TestExecutionCeilingRejectsGrantAndSubjectExpansion(t *testing.T) {
	for _, change := range []string{"unchanged", "namespace_expansion", "selector_expansion", "membership_expansion", "role_permissions", "empty_ceiling"} {
		t.Run(change, func(t *testing.T) {
			grants := &stubScopeGrantReader{items: []domainscopegrant.Record{{ID: "grant", SubjectType: "user", SubjectID: "user-1", ScopeType: "platform", Role: "admin", Effect: "allow", Enabled: true, ClusterIDs: []string{"cluster-a"}, Namespaces: []string{"payments"}}}}
			roles := map[string][]string{"admin": {PermPlatformNamespacesView, PermPlatformPodsView}}
			service := New(policy.NewEngine(), nil, grants, nil)
			service.SetPermissionResolver(NewPermissionResolver(stubRolePermissionReader{matrix: roles}))
			principal := domainidentity.Principal{UserID: "user-1", Roles: []string{"admin"}, AccessTokenID: "original-token-id"}
			revision, err := service.ExecutionAuthorizationRevision(t.Context(), principal)
			if err != nil || len(revision) != 64 {
				t.Fatalf("ceiling could not freeze: %s %v", revision, err)
			}
			switch change {
			case "namespace_expansion":
				grants.items[0].Namespaces = append(grants.items[0].Namespaces, "finance")
			case "selector_expansion":
				grants.items[0].NamespaceSelector = "team=all"
			case "membership_expansion":
				principal.Teams = []string{"new-team"}
			case "role_permissions":
				roles["admin"] = append(roles["admin"], PermPlatformPodsLogs)
			case "empty_ceiling":
				revision = ""
			}
			request := platformRequest(principal, domainaccess.ActionList, "", "", "Namespace")
			request.PermissionKey = PermPlatformNamespacesView
			ctx := WithExecutionAuthorizationCeiling(t.Context(), revision)
			decision, err := service.Authorize(ctx, request)
			if err != nil || decision.Allowed != (change == "unchanged") {
				t.Fatalf("scope change exceeded its ceiling: %+v %v", decision, err)
			}
			if change == "unchanged" {
				if decision.ResourceScope == nil || len(decision.ResourceScope.Namespaces) != 1 || decision.ResourceScope.Namespaces[0] != "payments" {
					t.Fatalf("existing namespace filtering was lost: %+v", decision)
				}
			} else if decision.ReasonCode != "authorization_ceiling_changed" {
				t.Fatalf("changed ceiling was not explicit: %+v", decision)
			}
		})
	}
}

func TestExecutionCeilingCannotFreezeUnavailablePermissions(t *testing.T) {
	service := New(policy.NewEngine(), nil, nil, nil)
	if _, err := service.ExecutionAuthorizationRevision(context.Background(), domainidentity.Principal{UserID: "user"}); err == nil {
		t.Fatal("missing authoritative permission source became a valid ceiling")
	}
}

func TestExecutionCeilingAcceptsEquivalentEmptyMemberships(t *testing.T) {
	service := New(policy.NewEngine(), nil, nil, nil)
	service.SetPermissionResolver(NewPermissionResolver(stubRolePermissionReader{matrix: map[string][]string{"reader": {PermObserveAIChatUse}}}))
	principal := domainidentity.Principal{UserID: "reader", Roles: []string{"reader"}}
	revision, err := service.ExecutionAuthorizationRevision(t.Context(), principal)
	if err != nil {
		t.Fatal(err)
	}
	principal.Teams, principal.Projects, principal.Tags = []string{}, []string{}, []string{}
	current, err := service.ExecutionAuthorizationRevision(t.Context(), principal)
	if err != nil || current != revision {
		t.Fatalf("equivalent empty memberships changed authorization: %s != %s: %v", current, revision, err)
	}
}
