package aigateway

import (
	"context"
	"errors"
	"strings"
	"testing"

	appaccess "github.com/opensoha/soha/internal/application/access"
	domainaigateway "github.com/opensoha/soha/internal/domain/aigateway"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

func TestGovernedAICannotSpoofCallerAtApplicationBoundary(t *testing.T) {
	principal := domainidentity.Principal{UserID: "user", GovernedAI: &domainidentity.GovernedAIBinding{Mode: "gateway_only", AIClientID: "codex", SkillID: "k8s-sre"}}
	// No provider is configured: denial must precede any provider or persistence access.
	service := &Service{}
	for _, caller := range [][2]string{{"", ""}, {"other", "k8s-sre"}, {"codex", "other"}} {
		client, skill := caller[0], caller[1]
		_, err := service.InvokeTool(t.Context(), principal, domainaigateway.ToolInvocationRequest{AIClientID: client, SkillID: skill})
		if !errors.Is(err, apperrors.ErrAccessDenied) {
			t.Fatalf("tool call was not rejected: %v", err)
		}
		_, err = service.ReadResource(t.Context(), principal, domainaigateway.ResourceReadRequest{AIClientID: client, SkillID: skill})
		if !errors.Is(err, apperrors.ErrAccessDenied) {
			t.Fatalf("resource read was not rejected: %v", err)
		}
		_, err = service.GetPrompt(t.Context(), principal, domainaigateway.PromptGetRequest{AIClientID: client, SkillID: skill})
		if !errors.Is(err, apperrors.ErrAccessDenied) {
			t.Fatalf("prompt read was not rejected: %v", err)
		}
		_, err = service.ValidateCapabilityPlan(t.Context(), principal, domainaigateway.CapabilityTaskInput{AIClientID: client, SkillID: skill})
		if !errors.Is(err, apperrors.ErrAccessDenied) {
			t.Fatalf("plan was not rejected: %v", err)
		}
		_, err = service.authorizeCapabilityCall(t.Context(), principal, domainaigateway.ToolInvocationRequest{AIClientID: client, SkillID: skill})
		if !errors.Is(err, apperrors.ErrAccessDenied) {
			t.Fatalf("recovered call was not rejected: %v", err)
		}
	}
	if err := authorizeGovernedAI(principal, "codex", "k8s-sre"); err != nil {
		t.Fatal(err)
	}
	if err := authorizeGovernedAI(domainidentity.Principal{}, "", ""); err != nil {
		t.Fatal("ordinary identity behavior changed")
	}
}

func TestGovernedAIBindingRequiresCurrentRegisteredReferences(t *testing.T) {
	repo := &memoryGatewayRepository{aiClients: map[string]domainaigateway.AIClient{"codex": {ID: "codex", Status: "active"}}}
	service := contractTestService(repo, nil)
	provider := customTestCapabilityProvider()
	service.SetCapabilityProviders(provider)
	principal := testPrincipal("developer")
	for _, entry := range []struct {
		client, skill string
		valid         bool
	}{{"codex", "custom-skill", true}, {"missing", "custom-skill", false}, {"codex", "missing", false}} {
		metadata := map[string]any{"governedAI": map[string]any{"mode": "gateway_only", "aiClientId": entry.client, "skillId": entry.skill}}
		created, err := service.CreatePersonalAccessToken(t.Context(), principal, domainaigateway.PersonalAccessTokenInput{Name: "governed-test", Metadata: metadata})
		if entry.valid && (err != nil || created.Value == "") {
			t.Fatalf("registered binding rejected: %v", err)
		}
		if entry.valid && (!strings.HasPrefix(created.Value, domainaigateway.GovernedPersonalAccessTokenPrefix) || !strings.HasPrefix(created.Token.TokenPrefix, domainaigateway.GovernedPersonalAccessTokenPrefix)) {
			t.Fatal("governed credential used an ordinary token prefix")
		}
		if !entry.valid && (!errors.Is(err, apperrors.ErrInvalidArgument) || created.Value != "") {
			t.Fatalf("unknown binding minted a credential: %v", err)
		}
	}
	principal.GovernedAI = &domainidentity.GovernedAIBinding{Mode: "gateway_only", AIClientID: "codex", SkillID: "custom-skill"}
	if err := service.authorizeGovernedAICaller(t.Context(), principal, "codex", "custom-skill"); err != nil {
		t.Fatal(err)
	}
	repo.aiClients["codex"] = domainaigateway.AIClient{ID: "codex", Status: "disabled"}
	if _, err := service.Capabilities(t.Context(), principal, domainaigateway.ManifestRequest{AIClientID: "codex", SkillID: "custom-skill"}); !errors.Is(err, apperrors.ErrAccessDenied) {
		t.Fatalf("disabled client retained discovery: %v", err)
	}
	if _, err := service.authorizeCapabilityCall(t.Context(), principal, domainaigateway.ToolInvocationRequest{AIClientID: "codex", SkillID: "custom-skill"}); !errors.Is(err, apperrors.ErrAccessDenied) {
		t.Fatalf("disabled client retained asynchronous execution: %v", err)
	}
}

func TestGovernedAISkillCeilingCannotBeWidenedByMissingBindings(t *testing.T) {
	repo := &memoryGatewayRepository{aiClients: map[string]domainaigateway.AIClient{"codex": {ID: "codex", Status: "active"}}}
	service := contractTestService(repo, nil)
	provider := customTestCapabilityProvider()
	calls := 0
	provider.invoke = func(context.Context, domainidentity.Principal, domainaigateway.ToolCapability, map[string]any) (any, map[string]any, error) {
		calls++
		return map[string]any{"ok": true}, nil, nil
	}
	service.SetCapabilityProviders(provider)
	principal := testPrincipal("developer")
	principal.GovernedAI = &domainidentity.GovernedAIBinding{Mode: "gateway_only", AIClientID: "codex", SkillID: "custom-skill"}
	manifest, err := service.Capabilities(t.Context(), principal, domainaigateway.ManifestRequest{AIClientID: "codex", SkillID: "custom-skill"})
	if err != nil {
		t.Fatal(err)
	}
	for _, tool := range manifest.Tools {
		if tool.Name != "custom.echo" {
			t.Fatalf("catalog escaped bound skill: %s", tool.Name)
		}
	}
	if len(manifest.Tools) != 1 {
		t.Fatal("bound catalog lost its authorized capability")
	}
	_, err = service.InvokeTool(t.Context(), principal, domainaigateway.ToolInvocationRequest{ToolName: "custom.blocked", AIClientID: "codex", SkillID: "custom-skill", Input: map[string]any{}})
	if !errors.Is(err, apperrors.ErrAccessDenied) || calls != 0 {
		t.Fatalf("out-of-skill call executed without configured bindings: %v calls=%d", err, calls)
	}
	_, err = service.InvokeTool(t.Context(), principal, domainaigateway.ToolInvocationRequest{ToolName: "custom.echo", AIClientID: "codex", SkillID: "custom-skill", Input: map[string]any{}})
	if err != nil || calls != 1 {
		t.Fatalf("bound capability was not executable: %v calls=%d", err, calls)
	}
	// Ordinary identities continue to use the existing role/grant policy.
	principal.GovernedAI = nil
	principal.PermissionKeys = []string{appaccess.PermAIGatewayInvoke, appaccess.PermAIGatewayView}
	if _, err := service.InvokeTool(t.Context(), principal, domainaigateway.ToolInvocationRequest{ToolName: "custom.blocked", Input: map[string]any{}}); err != nil || calls != 2 {
		t.Fatalf("ordinary credential behavior changed: %v", err)
	}
}

func TestGovernedCredentialCreateAndRotationPreserveDistinctPrefixes(t *testing.T) {
	for _, governed := range []bool{false, true} {
		t.Run(map[bool]string{false: "ordinary", true: "governed"}[governed], func(t *testing.T) {
			repo := &memoryGatewayRepository{
				aiClients:       map[string]domainaigateway.AIClient{"codex": {ID: "codex", Status: "active"}},
				serviceAccounts: map[string]domainaigateway.ServiceAccount{"ci": {ID: "ci", Name: "CI", Status: "active", RoleIDs: []string{"developer"}}},
			}
			service := contractTestService(repo, nil)
			service.SetCapabilityProviders(customTestCapabilityProvider())
			var metadata map[string]any
			patPrefix, satPrefix := domainaigateway.PersonalAccessTokenPrefix, domainaigateway.ServiceAccountTokenPrefix
			if governed {
				metadata = map[string]any{"governedAI": map[string]any{"mode": "gateway_only", "aiClientId": "codex", "skillId": "custom-skill"}}
				patPrefix, satPrefix = domainaigateway.GovernedPersonalAccessTokenPrefix, domainaigateway.GovernedServiceAccountTokenPrefix
			}
			personal, err := service.CreatePersonalAccessToken(t.Context(), testPrincipal("developer"), domainaigateway.PersonalAccessTokenInput{Name: "PAT", Metadata: metadata, GovernedOnly: governed})
			if err != nil || !strings.HasPrefix(personal.Value, patPrefix) {
				t.Fatal("invalid PAT issuance", err)
			}
			account, err := service.CreateServiceAccountToken(t.Context(), testPrincipal("admin"), "ci", domainaigateway.ServiceAccountTokenInput{Name: "SAT", Metadata: metadata, GovernedOnly: governed})
			if err != nil || !strings.HasPrefix(account.Value, satPrefix) {
				t.Fatal("invalid SAT issuance", err)
			}
			if !governed {
				_, err = service.RotatePersonalAccessToken(t.Context(), testPrincipal("developer"), personal.Token.ID, domainaigateway.TokenRotationInput{GovernedOnly: true})
				if !errors.Is(err, apperrors.ErrInvalidArgument) || repo.personalTokens[0].RevokedAt != nil {
					t.Fatal("governed route rotated an unbound PAT", err)
				}
				_, err = service.RotateServiceAccountToken(t.Context(), testPrincipal("admin"), account.Token.ID, domainaigateway.TokenRotationInput{GovernedOnly: true})
				if !errors.Is(err, apperrors.ErrInvalidArgument) || repo.serviceAccountTokens[0].RevokedAt != nil {
					t.Fatal("governed route rotated an unbound SAT", err)
				}
			}
			rotatedPersonal, err := service.RotatePersonalAccessToken(t.Context(), testPrincipal("developer"), personal.Token.ID, domainaigateway.TokenRotationInput{GovernedOnly: governed})
			if err != nil || !strings.HasPrefix(rotatedPersonal.Value, patPrefix) || repo.personalTokens[0].RevokedAt == nil {
				t.Fatal("PAT rotation lost prefix or revocation", err)
			}
			rotatedAccount, err := service.RotateServiceAccountToken(t.Context(), testPrincipal("admin"), account.Token.ID, domainaigateway.TokenRotationInput{GovernedOnly: governed})
			if err != nil || !strings.HasPrefix(rotatedAccount.Value, satPrefix) || repo.serviceAccountTokens[0].RevokedAt == nil {
				t.Fatal("SAT rotation lost prefix or revocation", err)
			}
		})
	}
	service := contractTestService(&memoryGatewayRepository{}, nil)
	if created, err := service.CreatePersonalAccessToken(t.Context(), testPrincipal("developer"), domainaigateway.PersonalAccessTokenInput{Name: "invalid", GovernedOnly: true}); !errors.Is(err, apperrors.ErrInvalidArgument) || created.Value != "" {
		t.Fatal("governed route minted an ordinary credential", err)
	}
}

func TestGovernedSkillDeniesBeforeDomainScopeLookup(t *testing.T) {
	repo := &memoryGatewayRepository{aiClients: map[string]domainaigateway.AIClient{"codex": {ID: "codex", Status: "active"}}}
	// No Docker service: resolving the project would fail before the skill gate.
	service := contractTestService(repo, nil)
	principal := testPrincipal("developer")
	principal.GovernedAI = &domainidentity.GovernedAIBinding{Mode: "gateway_only", AIClientID: "codex", SkillID: "compute-sre"}
	call := domainaigateway.ToolInvocationRequest{ToolName: "docker.projects.deploy.trigger", CapabilityVersion: "1", AIClientID: "codex", SkillID: "compute-sre", Input: map[string]any{"projectId": "unavailable", "idempotencyKey": "fixed-attempt"}}
	for _, approval := range []bool{false, true} {
		if _, err := service.invokeTool(t.Context(), principal, call, approval); !errors.Is(err, apperrors.ErrAccessDenied) {
			t.Fatalf("outside-skill call reached domain lookup: %v", err)
		}
	}
	if _, err := service.authorizeCapabilityCall(t.Context(), principal, call); !errors.Is(err, apperrors.ErrAccessDenied) {
		t.Fatalf("outside-skill asynchronous call reached domain lookup: %v", err)
	}
}
