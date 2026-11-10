package identity

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	appaccess "github.com/opensoha/soha/internal/application/access"
	domainai "github.com/opensoha/soha/internal/domain/aigateway"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type delegatedRolePermissions struct{ keys []string }

func (r *delegatedRolePermissions) ListRolePermissions(context.Context) (map[string][]string, error) {
	return map[string][]string{"operator": slices.Clone(r.keys)}, nil
}

func delegatedExecutionFixture(t *testing.T, serviceAccount bool) (*Service, *executionTokenStore, *delegatedRolePermissions, domainidentity.Principal) {
	t.Helper()
	metadata := map[string]any{"governedAI": map[string]any{"mode": "gateway_only", "aiClientId": "codex", "skillId": "k8s-pod-executor"}}
	store := &executionTokenStore{loginMappingUserRepo: newLoginMappingUserRepo(),
		account: domainai.ServiceAccount{ID: "automation", Name: "Automation", Status: "active", RoleIDs: []string{"operator"}},
		token: domainai.ServiceAccountToken{ID: "token", ServiceAccountID: "automation", TokenPrefix: domainai.GovernedServiceAccountTokenPrefix,
			PermissionKeys: []string{appaccess.PermPlatformPodsExec}, Metadata: metadata},
		personal: domainai.PersonalAccessToken{ID: "personal", UserID: "user", TokenPrefix: domainai.GovernedPersonalAccessTokenPrefix,
			PermissionKeys: []string{appaccess.PermPlatformPodsExec}, Metadata: metadata},
	}
	store.usersByID["user"] = domainidentity.User{ID: "user", Status: "active"}
	if err := store.ReplaceRoleBindings(t.Context(), "user", []string{"operator"}); err != nil {
		t.Fatal(err)
	}
	roles := &delegatedRolePermissions{keys: []string{appaccess.PermPlatformPodsExec, appaccess.PermAIGatewayInvoke}}
	service := newTestServiceWithUserStore(store.loginMappingUserRepo)
	service.gateway, service.permissions = store, appaccess.NewPermissionResolver(roles)
	subject, token := "user", "personal"
	if serviceAccount {
		subject, token = "service_account:automation", "token"
	}
	principal, err := service.CurrentExecutionPrincipal(t.Context(), subject, token)
	if err != nil {
		t.Fatal(err)
	}
	return service, store, roles, principal
}

func TestDelegatedExecutionKeepsCredentialAndFrozenPermissionCeiling(t *testing.T) {
	for _, account := range []bool{false, true} {
		service, store, roles, original := delegatedExecutionFixture(t, account)
		store.token.PermissionKeys, store.personal.PermissionKeys = slices.Clone(roles.keys), slices.Clone(roles.keys)
		current, err := service.CurrentDelegatedExecutionPrincipal(t.Context(), original)
		if err != nil || current.GovernedAI == nil || current.AccessTokenID != original.AccessTokenID || !slices.Equal(current.PermissionKeys, []string{appaccess.PermPlatformPodsExec}) {
			t.Fatalf("credential or original ceiling changed: %v", err)
		}
		// The returned principal is the accepted ceiling retained by the runner.
		// Growing the role or token later cannot add a new operation to that run.
		roles.keys = append(roles.keys, "platform.secrets.data")
		store.token.PermissionKeys, store.personal.PermissionKeys = slices.Clone(roles.keys), slices.Clone(roles.keys)
		current, err = service.CurrentDelegatedExecutionPrincipal(t.Context(), current)
		if err != nil || !slices.Equal(current.PermissionKeys, []string{appaccess.PermPlatformPodsExec}) {
			t.Fatal("accepted execution ceiling widened")
		}
		roles.keys = []string{appaccess.PermAIGatewayInvoke}
		if _, err := service.CurrentDelegatedExecutionPrincipal(t.Context(), current); !errors.Is(err, apperrors.ErrAccessDenied) {
			t.Fatalf("removed role permission remained authorized: %v", err)
		}
	}
}

func TestDelegatedExecutionRejectsChangedOrMissingCredential(t *testing.T) {
	for _, account := range []bool{false, true} {
		for _, scenario := range []string{"missing-token", "wrong-subject", "wrong-token", "revoked", "expired", "disabled", "changed-governed-class"} {
			t.Run(scenario, func(t *testing.T) {
				service, store, _, original := delegatedExecutionFixture(t, account)
				past := time.Now().Add(-time.Minute)
				switch scenario {
				case "missing-token":
					original.AccessTokenID = ""
				case "wrong-subject":
					original.UserID = "other"
				case "wrong-token":
					original.AccessTokenID = "other"
				case "revoked":
					store.token.RevokedAt, store.personal.RevokedAt = &past, &past
				case "expired":
					store.token.ExpiresAt, store.personal.ExpiresAt = &past, &past
				case "disabled":
					store.account.Status = "disabled"
					store.usersByID["user"] = domainidentity.User{ID: "user", Status: "disabled"}
				case "changed-governed-class":
					store.token.TokenPrefix, store.personal.TokenPrefix = "soha_sat_ordinary", "soha_pat_ordinary"
					store.token.Metadata, store.personal.Metadata = nil, nil
				}
				if _, err := service.CurrentDelegatedExecutionPrincipal(t.Context(), original); err == nil {
					t.Fatal("changed or inactive credential remained authorized")
				}
			})
		}
	}
}

func TestDelegatedExecutionReturnsCurrentPartialCeilingAndBinding(t *testing.T) {
	service, store, roles, original := delegatedExecutionFixture(t, false)
	store.personal.PermissionKeys = slices.Clone(roles.keys)
	original.PermissionKeys = slices.Clone(roles.keys)
	roles.keys = []string{appaccess.PermAIGatewayInvoke}
	store.personal.Metadata = map[string]any{"governedAI": map[string]any{"mode": "gateway_only", "aiClientId": "changed-client", "skillId": "changed-skill"}}
	current, err := service.CurrentDelegatedExecutionPrincipal(t.Context(), original)
	if err != nil || !slices.Equal(current.PermissionKeys, roles.keys) || current.GovernedAI == nil || current.GovernedAI.AIClientID != "changed-client" || current.GovernedAI.SkillID != "changed-skill" {
		t.Fatal("current smaller ceiling or stored binding was replaced with an old snapshot")
	}
	// The identity layer returns the current intersection. The owning resource
	// and Gateway must still reject loss of the specific exec/tool permission.
	if err := appaccess.AuthorizeRuntimePermission(t.Context(), service.permissions, current, appaccess.PermPlatformPodsExec); !errors.Is(err, apperrors.ErrAccessDenied) {
		t.Fatal("removed exec permission survived the current principal")
	}
}

func TestDelegatedExecutionDoesNotOpenNativeStreamingForGovernedAI(t *testing.T) {
	service, _, _, original := delegatedExecutionFixture(t, false)
	if _, err := service.CurrentDelegatedExecutionPrincipal(t.Context(), original); err != nil {
		t.Fatal(err)
	}
	if _, err := service.CurrentStreamingPrincipal(t.Context(), original, domainidentity.AccessContext{}); !errors.Is(err, apperrors.ErrUnauthorized) {
		t.Fatal("delegated identity opened native streaming")
	}
	service.sessions = nil
	if _, err := service.CurrentExecutionPrincipal(t.Context(), "user", cliSessionTokenPrefix+"missing"); !errors.Is(err, apperrors.ErrUnauthorized) {
		t.Fatal("missing session store did not fail closed")
	}
}
