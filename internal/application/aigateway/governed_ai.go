package aigateway

import (
	"context"
	"fmt"
	"strings"

	domainaigateway "github.com/opensoha/soha/internal/domain/aigateway"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

func authorizeGovernedAI(principal domainidentity.Principal, clientID, skillID string) error {
	binding := principal.GovernedAI
	if binding != nil && (binding.Mode != "gateway_only" || binding.AIClientID != clientID || binding.SkillID != skillID) {
		return fmt.Errorf("%w: credential requires its bound AI client and skill", apperrors.ErrAccessDenied)
	}
	return nil
}

func (s *Service) validateGovernedAIReferences(ctx context.Context, binding *domainidentity.GovernedAIBinding) error {
	if binding == nil {
		return nil
	}
	repo := s.clientRepository()
	if repo == nil {
		return fmt.Errorf("%w: governed AI client registry is unavailable", apperrors.ErrAccessDenied)
	}
	client, err := repo.GetAIClient(ctx, binding.AIClientID)
	if err != nil || client.ID != binding.AIClientID || strings.TrimSpace(client.Status) != "active" {
		return fmt.Errorf("%w: governed AI client is unavailable", apperrors.ErrAccessDenied)
	}
	if _, ok := s.skillByID(binding.SkillID); !ok {
		return fmt.Errorf("%w: governed AI skill is unavailable", apperrors.ErrAccessDenied)
	}
	return nil
}

func (s *Service) authorizeGovernedAICaller(ctx context.Context, principal domainidentity.Principal, clientID, skillID string) error {
	if err := authorizeGovernedAI(principal, clientID, skillID); err != nil {
		return err
	}
	return s.validateGovernedAIReferences(ctx, principal.GovernedAI)
}

// Apply this ceiling separately from configured bindings: combining their rows
// would union grants and could widen the credential's fixed skill.
func governedSkillCeiling(principal domainidentity.Principal) []domainaigateway.SkillBinding {
	if principal.GovernedAI == nil {
		return nil
	}
	return []domainaigateway.SkillBinding{{SkillID: principal.GovernedAI.SkillID, Enabled: true}}
}

func (s *Service) authorizeGovernedAITool(principal domainidentity.Principal, tool domainaigateway.ToolCapability) error {
	if principal.GovernedAI == nil {
		return nil
	}
	if allowed, _ := toolAllowedBySkillBindingsWithSkills(tool, governedSkillCeiling(principal), principal.GovernedAI.SkillID, s.gatewaySkills()); !allowed {
		return fmt.Errorf("%w: tool is outside the credential's bound skill", apperrors.ErrAccessDenied)
	}
	return nil
}

func gatewayCredentialPrefix(metadata map[string]any, serviceAccount, governedOnly bool) (string, error) {
	binding, err := domainidentity.ParseGovernedAIBinding(metadata)
	if err != nil {
		return "", fmt.Errorf("%w: invalid governed AI binding", apperrors.ErrInvalidArgument)
	}
	if governedOnly && binding == nil {
		return "", fmt.Errorf("%w: governed credential route requires a binding", apperrors.ErrInvalidArgument)
	}
	if serviceAccount {
		if binding != nil {
			return domainaigateway.GovernedServiceAccountTokenPrefix, nil
		}
		return domainaigateway.ServiceAccountTokenPrefix, nil
	}
	if binding != nil {
		return domainaigateway.GovernedPersonalAccessTokenPrefix, nil
	}
	return domainaigateway.PersonalAccessTokenPrefix, nil
}
