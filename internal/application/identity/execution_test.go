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
