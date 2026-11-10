package identity

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	domainaigateway "github.com/opensoha/soha/internal/domain/aigateway"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type executionTokenStore struct {
	*loginMappingUserRepo
	account  domainaigateway.ServiceAccount
	token    domainaigateway.ServiceAccountToken
	personal domainaigateway.PersonalAccessToken
}

func (r *executionTokenStore) GetServiceAccountTokenByID(_ context.Context, id string) (domainaigateway.ServiceAccountToken, error) {
	if id != r.token.ID {
		return domainaigateway.ServiceAccountToken{}, apperrors.ErrNotFound
	}
	return r.token, nil
}

func (r *executionTokenStore) GetServiceAccount(_ context.Context, id string) (domainaigateway.ServiceAccount, error) {
	if id != r.account.ID {
		return domainaigateway.ServiceAccount{}, apperrors.ErrNotFound
	}
	return r.account, nil
}

func (r *executionTokenStore) GetPersonalAccessTokenByID(_ context.Context, id string) (domainaigateway.PersonalAccessToken, error) {
	if id != r.personal.ID {
		return domainaigateway.PersonalAccessToken{}, apperrors.ErrNotFound
	}
	return r.personal, nil
}

func TestExecutionPrincipalRechecksTokenAndSubjectWithoutWideningPermissions(t *testing.T) {
	store := &executionTokenStore{loginMappingUserRepo: newLoginMappingUserRepo(),
		account:  domainaigateway.ServiceAccount{ID: "automation", Name: "Automation", Status: "active", RoleIDs: []string{"operator"}, TeamIDs: []string{"release"}},
		token:    domainaigateway.ServiceAccountToken{ID: "token", ServiceAccountID: "automation", PermissionKeys: []string{"delivery.workflows.trigger"}},
		personal: domainaigateway.PersonalAccessToken{ID: "personal", UserID: "user", PermissionKeys: []string{"delivery.workflows.trigger"}},
	}
	store.usersByID["user"] = domainidentity.User{ID: "user", Status: "active"}
	service := newTestServiceWithUserStore(store.loginMappingUserRepo)
	service.gateway = store
	ctx := context.Background()
	for _, entry := range []struct{ subject, token string }{{"service_account:automation", "token"}, {"user", "personal"}} {
		principal, err := service.CurrentExecutionPrincipal(ctx, entry.subject, entry.token)
		if err != nil || principal.UserID != entry.subject || principal.AccessTokenID != entry.token || !slices.Equal(principal.PermissionKeys, []string{"delivery.workflows.trigger"}) {
			t.Fatalf("token identity or caps lost: %+v %v", principal, err)
		}
	}
	for _, entry := range []struct{ subject, token string }{{"service_account:automation", ""}, {"service_account:other", "token"}, {"other", "personal"}, {"user", "missing"}} {
		if _, err := service.CurrentExecutionPrincipal(ctx, entry.subject, entry.token); !errors.Is(err, apperrors.ErrUnauthorized) {
			t.Fatalf("invalid identity accepted: %+v %v", entry, err)
		}
	}
	now := time.Now().Add(-time.Minute)
	for _, state := range []string{"revoked", "expired", "disabled"} {
		t.Run(state, func(t *testing.T) {
			store.token.RevokedAt, store.token.ExpiresAt = nil, nil
			store.personal.RevokedAt, store.personal.ExpiresAt = nil, nil
			store.account.Status = "active"
			store.usersByID["user"] = domainidentity.User{ID: "user", Status: "active"}
			switch state {
			case "revoked":
				store.token.RevokedAt, store.personal.RevokedAt = &now, &now
			case "expired":
				store.token.ExpiresAt, store.personal.ExpiresAt = &now, &now
			case "disabled":
				store.account.Status = "disabled"
				store.usersByID["user"] = domainidentity.User{ID: "user", Status: "disabled"}
			}
			for _, entry := range []struct{ subject, token string }{{"service_account:automation", "token"}, {"user", "personal"}} {
				if _, err := service.CurrentExecutionPrincipal(ctx, entry.subject, entry.token); !errors.Is(err, apperrors.ErrUnauthorized) {
					t.Fatalf("inactive token accepted: %+v %v", entry, err)
				}
			}
		})
	}
}

func TestExecutionPrincipalReloadsGovernedAIBinding(t *testing.T) {
	store := &executionTokenStore{loginMappingUserRepo: newLoginMappingUserRepo(),
		account:  domainaigateway.ServiceAccount{ID: "automation", Name: "Automation", Status: "active"},
		token:    domainaigateway.ServiceAccountToken{ID: "token", ServiceAccountID: "automation", TokenPrefix: domainaigateway.GovernedServiceAccountTokenPrefix},
		personal: domainaigateway.PersonalAccessToken{ID: "personal", UserID: "user", TokenPrefix: domainaigateway.GovernedPersonalAccessTokenPrefix},
	}
	store.usersByID["user"] = domainidentity.User{ID: "user", Status: "active"}
	service := newTestServiceWithUserStore(store.loginMappingUserRepo)
	service.gateway = store
	for _, skill := range []string{"k8s-sre", "delivery-developer"} {
		metadata := map[string]any{"governedAI": map[string]any{"mode": "gateway_only", "aiClientId": "codex", "skillId": skill}}
		store.token.Metadata, store.personal.Metadata = metadata, metadata
		for _, entry := range []struct{ subject, token string }{{"service_account:automation", "token"}, {"user", "personal"}} {
			principal, err := service.CurrentExecutionPrincipal(t.Context(), entry.subject, entry.token)
			if err != nil || principal.GovernedAI == nil || principal.GovernedAI.SkillID != skill {
				t.Fatalf("current stored binding was not restored: %+v %v", principal.GovernedAI, err)
			}
		}
	}
	store.token.Metadata, store.personal.Metadata = map[string]any{"governedAI": nil}, map[string]any{"governedAI": nil}
	for _, entry := range []struct{ subject, token string }{{"service_account:automation", "token"}, {"user", "personal"}} {
		if _, err := service.CurrentExecutionPrincipal(t.Context(), entry.subject, entry.token); !errors.Is(err, apperrors.ErrUnauthorized) {
			t.Fatalf("malformed stored binding did not fail closed: %v", err)
		}
	}
}

func TestGovernedCredentialPrefixMustMatchStoredBinding(t *testing.T) {
	binding := &domainidentity.GovernedAIBinding{Mode: "gateway_only", AIClientID: "codex", SkillID: "k8s-sre"}
	for _, entry := range []struct {
		prefix, expected string
		binding          *domainidentity.GovernedAIBinding
		allowed          bool
	}{
		{"soha_pat_ordinary", "soha_gpat_", nil, true},
		{"soha_sat_ordinary", "soha_gsat_", nil, true},
		{"soha_gpat_bound", "soha_gpat_", binding, true},
		{"soha_gsat_bound", "soha_gsat_", binding, true},
		{"soha_gpat_bound", "soha_gpat_", nil, false},
		{"soha_gsat_bound", "soha_gsat_", nil, false},
		{"soha_pat_ordinary", "soha_gpat_", binding, false},
		{"soha_sat_ordinary", "soha_gsat_", binding, false},
		{"soha_gsat_bound", "soha_gpat_", binding, false},
		{"soha_gpat_bound", "soha_gsat_", binding, false},
		{"soha_gsat_bound", "soha_gpat_", nil, false},
	} {
		if got := credentialBindingMatchesPrefix(entry.binding, entry.prefix, entry.expected); got != entry.allowed {
			t.Errorf("binding/prefix validation = %v for %s; want %v", got, entry.prefix, entry.allowed)
		}
	}
}
