package identity

import (
	"errors"
	"testing"
	"time"

	appaccess "github.com/opensoha/soha/internal/application/access"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

func TestStreamingPrincipalRetainsOriginalSessionEpochAndExpiry(t *testing.T) {
	for _, scenario := range []string{"valid", "expired", "revoked", "changed-epoch", "missing-epoch", "wrong-subject", "governed"} {
		t.Run(scenario, func(t *testing.T) {
			service, repo := newAuthzTestService()
			service.permissions = appaccess.NewPermissionResolver(authzRolePermissions{})
			initial, err := service.loadPrincipal(t.Context(), "u1")
			if err != nil {
				t.Fatal(err)
			}
			issued, err := service.issueAuthResult(t.Context(), initial, "password")
			if err != nil {
				t.Fatal(err)
			}
			principal, access, err := service.ParseAccessToken(t.Context(), issued.Tokens.AccessToken)
			if err != nil {
				t.Fatal(err)
			}
			switch scenario {
			case "expired":
				access.ExpiresAt = time.Now().Add(-time.Second)
			case "revoked":
				session := repo.sessionsByID[access.SessionID]
				session.Status = "revoked"
				repo.sessionsByID[access.SessionID] = session
			case "changed-epoch":
				user := repo.usersByID["u1"]
				user.AuthzVersion++
				repo.usersByID["u1"] = user
			case "missing-epoch":
				access.AuthzVersion = 0
			case "wrong-subject":
				access.SubjectID = "other"
			case "governed":
				principal.GovernedAI = &domainidentity.GovernedAIBinding{Mode: "gateway_only", AIClientID: "codex", SkillID: "k8s-sre"}
			}
			current, err := service.CurrentStreamingPrincipal(t.Context(), principal, access)
			if scenario == "valid" {
				if err != nil || len(current.PermissionKeys) != 1 || current.PermissionKeys[0] != "observe.ai.chat" {
					t.Fatalf("lost ceiling: %+v %v", current, err)
				}
			} else if !errors.Is(err, apperrors.ErrUnauthorized) {
				t.Fatalf("invalid stream remained authorized: %v", err)
			}
		})
	}
}
