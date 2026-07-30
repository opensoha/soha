package manifest

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	appaccess "github.com/opensoha/soha/internal/application/access"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainmanifest "github.com/opensoha/soha/internal/domain/manifest"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

func (s *DeclarativeService) CreateDeliveryIntent(ctx context.Context, principal domainidentity.Principal, packageID string, input domainmanifest.DeliveryIntentInput) (domainmanifest.DeliveryIntent, error) {
	if err := s.base.authorize(ctx, principal, appaccess.PermObserveAIChatUse); err != nil {
		return domainmanifest.DeliveryIntent{}, err
	}
	if err := s.base.authorize(ctx, principal, appaccess.PermDeliveryApplicationsUpdate); err != nil {
		return domainmanifest.DeliveryIntent{}, err
	}
	item, err := s.base.Get(ctx, principal, strings.TrimSpace(packageID))
	if err != nil {
		return domainmanifest.DeliveryIntent{}, err
	}
	source, err := s.repository.GetSource(ctx, item.ID)
	if err != nil {
		return domainmanifest.DeliveryIntent{}, err
	}
	if source.Mode != domainmanifest.SourceModeSohaManaged {
		return domainmanifest.DeliveryIntent{}, fmt.Errorf("%w: AI proposals cannot mutate Git-synchronized manifests", apperrors.ErrConflict)
	}
	input.Provider = strings.TrimSpace(input.Provider)
	input.Model = strings.TrimSpace(input.Model)
	input.PromptTemplateVersion = strings.TrimSpace(input.PromptTemplateVersion)
	input.EvidenceDigest = strings.TrimSpace(input.EvidenceDigest)
	input.Rationale = strings.TrimSpace(input.Rationale)
	input.Risk = strings.TrimSpace(input.Risk)
	if input.Provider == "" || input.Model == "" || input.PromptTemplateVersion == "" || input.EvidenceDigest == "" || input.Rationale == "" || input.Risk == "" {
		return domainmanifest.DeliveryIntent{}, fmt.Errorf("%w: AI proposal provenance, evidence, rationale, and risk are required", apperrors.ErrInvalidArgument)
	}
	if !validIntentDigest(input.EvidenceDigest) {
		return domainmanifest.DeliveryIntent{}, fmt.Errorf("%w: evidenceDigest must be a SHA-256 digest", apperrors.ErrInvalidArgument)
	}
	candidate, err := normalizeInput(domainmanifest.Input{
		Name: item.Name, Description: item.Description, ApplicationID: item.ApplicationID,
		BusinessLineID: item.BusinessLineID, Renderer: item.Renderer, Files: input.Files, Bindings: item.Bindings,
	})
	if err != nil {
		return domainmanifest.DeliveryIntent{}, err
	}
	if err := validateRenderableFiles(candidate); err != nil {
		return domainmanifest.DeliveryIntent{}, err
	}
	validation, bindingID, err := s.validateIntentRender(ctx, candidate, strings.TrimSpace(input.BindingID))
	if err != nil {
		return domainmanifest.DeliveryIntent{}, err
	}
	payload, _ := json.Marshal(struct {
		Files                 []domainmanifest.File `json:"files"`
		Provider              string                `json:"provider"`
		Model                 string                `json:"model"`
		PromptTemplateVersion string                `json:"promptTemplateVersion"`
		EvidenceDigest        string                `json:"evidenceDigest"`
	}{candidate.Files, input.Provider, input.Model, input.PromptTemplateVersion, input.EvidenceDigest})
	digest := sha256.Sum256(payload)
	now := time.Now().UTC()
	intent := domainmanifest.DeliveryIntent{
		ID: uuid.NewString(), PackageID: item.ID, BindingID: bindingID,
		Status: domainmanifest.IntentStatusDraft, Files: candidate.Files,
		Provider: input.Provider, Model: input.Model, PromptTemplateVersion: input.PromptTemplateVersion,
		RequestID: strings.TrimSpace(input.RequestID), EvidenceDigest: input.EvidenceDigest,
		EvidenceRefs: normalizedStrings(input.EvidenceRefs), ProposalDigest: "sha256:" + hex.EncodeToString(digest[:]),
		Rationale: input.Rationale, Risk: input.Risk, Validation: validation,
		CreatedBy: principal.UserID, CreatedAt: now, UpdatedAt: now,
	}
	created, err := s.repository.CreateDeliveryIntent(ctx, intent)
	if err != nil {
		return domainmanifest.DeliveryIntent{}, err
	}
	s.base.record(ctx, principal, "delivery.manifest.intent.create", item, "created governed AI manifest proposal")
	return created, nil
}

func (s *DeclarativeService) validateIntentRender(ctx context.Context, item domainmanifest.Package, bindingID string) (domainmanifest.PreflightResult, string, error) {
	if s.renderer == nil {
		return domainmanifest.PreflightResult{}, "", fmt.Errorf("%w: manifest renderer is unavailable", apperrors.ErrInvalidArgument)
	}
	var binding domainmanifest.EnvironmentBinding
	if bindingID != "" {
		loaded, err := s.repository.GetBinding(ctx, bindingID)
		if err != nil {
			return domainmanifest.PreflightResult{}, "", err
		}
		if loaded.PackageID != item.ID {
			return domainmanifest.PreflightResult{}, "", fmt.Errorf("%w: binding does not belong to manifest package", apperrors.ErrInvalidArgument)
		}
		binding = loaded
	} else {
		bindings, err := s.repository.ListBindings(ctx, item.ID)
		if err != nil {
			return domainmanifest.PreflightResult{}, "", err
		}
		if len(bindings) > 0 {
			binding = bindings[0]
			bindingID = binding.ID
		} else {
			binding = domainmanifest.EnvironmentBinding{ID: "proposal-preview", PackageID: item.ID, Namespace: "default", Overlay: map[string]string{}}
		}
	}
	rendered, err := s.renderer.Render(ctx, item, binding, item.Files, 0)
	if err != nil {
		return domainmanifest.PreflightResult{}, "", err
	}
	return domainmanifest.PreflightResult{Ready: true, Capability: "available", RenderedDigest: rendered.RenderedDigest, ResourceCount: len(rendered.Documents), Diagnostics: rendered.Diagnostics}, bindingID, nil
}

func (s *DeclarativeService) ListDeliveryIntents(ctx context.Context, principal domainidentity.Principal, packageID string) ([]domainmanifest.DeliveryIntent, error) {
	if _, err := s.base.Get(ctx, principal, strings.TrimSpace(packageID)); err != nil {
		return nil, err
	}
	return s.repository.ListDeliveryIntents(ctx, strings.TrimSpace(packageID))
}

func (s *DeclarativeService) DecideDeliveryIntent(ctx context.Context, principal domainidentity.Principal, intentID, decision string, input domainmanifest.DeliveryIntentDecisionInput) (domainmanifest.DeliveryIntent, error) {
	if err := s.base.authorize(ctx, principal, appaccess.PermObserveAIChatUse); err != nil {
		return domainmanifest.DeliveryIntent{}, err
	}
	if err := s.base.authorize(ctx, principal, appaccess.PermDeliveryApplicationsUpdate); err != nil {
		return domainmanifest.DeliveryIntent{}, err
	}
	if decision != domainmanifest.IntentStatusAccepted && decision != domainmanifest.IntentStatusRejected {
		return domainmanifest.DeliveryIntent{}, fmt.Errorf("%w: unsupported manifest intent decision", apperrors.ErrInvalidArgument)
	}
	intent, err := s.repository.GetDeliveryIntent(ctx, strings.TrimSpace(intentID))
	if err != nil {
		return domainmanifest.DeliveryIntent{}, err
	}
	item, err := s.base.Get(ctx, principal, intent.PackageID)
	if err != nil {
		return domainmanifest.DeliveryIntent{}, err
	}
	if input.ExpectedPackageUpdatedAt.IsZero() {
		return domainmanifest.DeliveryIntent{}, fmt.Errorf("%w: expectedPackageUpdatedAt is required", apperrors.ErrInvalidArgument)
	}
	intent.Status = decision
	intent.DecisionComment = strings.TrimSpace(input.Comment)
	intent.DecidedBy = principal.UserID
	now := time.Now().UTC()
	intent.DecidedAt = &now
	intent.UpdatedAt = now
	updated, err := s.repository.DecideDeliveryIntent(ctx, intent, input)
	if err != nil {
		return domainmanifest.DeliveryIntent{}, err
	}
	s.base.record(ctx, principal, "delivery.manifest.intent."+decision, item, decision+" governed AI manifest proposal")
	return updated, nil
}

func normalizedStrings(values []string) []string {
	result := make([]string, 0, len(values))
	seen := map[string]struct{}{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}

func validIntentDigest(value string) bool {
	value = strings.TrimPrefix(strings.ToLower(strings.TrimSpace(value)), "sha256:")
	if len(value) != 64 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}
