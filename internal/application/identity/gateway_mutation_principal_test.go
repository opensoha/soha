package identity

import (
	"errors"
	"testing"
	"time"

	appaccess "github.com/opensoha/soha/internal/application/access"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

func TestGatewayMutationPrincipalRechecksSessionEpochAndCeiling(t *testing.T) {
	for _, scenario := range []string{"valid", "expired", "revoked", "wrong-owner", "changed-epoch", "missing-epoch", "governed", "missing-session"} {
		t.Run(scenario, func(t *testing.T) {
			service, repo := newAuthzTestService()
			service.permissions = appaccess.NewPermissionResolver(authzRolePermissions{})
			original, err := service.loadPrincipal(t.Context(), "u1")
			if err != nil {
				t.Fatal(err)
			}
			issued, err := service.issueAuthResult(t.Context(), original, "password")
			if err != nil {
				t.Fatal(err)
			}
			principal, access, err := service.ParseAccessToken(t.Context(), issued.Tokens.AccessToken)
			if err != nil {
				t.Fatal(err)
			}
			session := repo.sessionsByID[access.SessionID]
			switch scenario {
			case "expired":
				session.ExpiresAt = time.Now().Add(-time.Second)
			case "revoked":
				session.Status = "revoked"
			case "wrong-owner":
				session.UserID = "other"
			case "missing-epoch":
				session.AuthzVersion = 0
			case "changed-epoch":
				user := repo.usersByID["u1"]
				user.AuthzVersion++
				repo.usersByID["u1"] = user
			case "governed":
				principal.GovernedAI = &domainidentity.GovernedAIBinding{Mode: "gateway_only"}
			case "missing-session":
				access.SessionID = ""
			}
			if access.SessionID != "" {
				repo.sessionsByID[access.SessionID] = session
			}
			current, err := service.CurrentGatewayMutationPrincipal(t.Context(), principal, access.SessionID)
			if scenario == "valid" {
				if err != nil || len(current.PermissionKeys) != 1 || current.PermissionKeys[0] != "observe.ai.chat" {
					t.Fatal("original ceiling lost", current, err)
				}
			} else if !errors.Is(err, apperrors.ErrUnauthorized) {
				t.Fatal("invalid session accepted", err)
			}
		})
	}
}
