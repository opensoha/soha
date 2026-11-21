package assistantmesh

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	domain "github.com/opensoha/soha/internal/domain/assistantmesh"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainsettings "github.com/opensoha/soha/internal/domain/settings"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

func (s *Service) Update(ctx context.Context, principal domainidentity.Principal, id, expectedDigest string, input domain.AssetInput) (Asset, error) {
	_, err := s.prepare(ctx, principal)
	if err != nil {
		return Asset{}, err
	}
	if err := validateInput(input); err != nil {
		return Asset{}, err
	}
	var asset Asset
	err = s.store.Update(ctx, id, func(ctx context.Context, record domain.Record) (json.RawMessage, error) {
		if record.OwnerID != principal.UserID {
			return nil, apperrors.ErrNotFound
		}
		value, err := decodePayload(record.Payload)
		if err != nil {
			return nil, err
		}
		if value.DraftDigest == "" || value.DraftDigest != expectedDigest {
			return nil, fmt.Errorf("%w: draft digest is stale", apperrors.ErrConflict)
		}
		if input.Kind != record.Kind {
			return nil, fmt.Errorf("%w: asset kind cannot change", apperrors.ErrInvalidArgument)
		}
		draft := input
		digest := input.Digest()
		value.Draft = &draft
		value.DraftDigest = digest
		value.Findings = findingsFor(ctx, input)
		if digest != expectedDigest {
			value.Trial = nil
			if value.Review != nil {
				if value.Review.Status == "pending" {
					value.Review.Status, value.Review.Reason = "withdrawn", ""
				} else {
					value.Review = nil
				}
			}
		}
		raw, err := encodePayload(value)
		if err != nil {
			return nil, err
		}
		asset = s.ownerAsset(record, value)
		return raw, nil
	})
	if err != nil {
		return Asset{}, err
	}
	s.audit(ctx, principal, "assistant.mesh.asset.update", id, "success", map[string]any{"digest": input.Digest()})
	return asset, nil
}

func (s *Service) Act(ctx context.Context, principal domainidentity.Principal, id, action string, request ActionRequest) (ActionResult, error) {
	settings, err := s.prepare(ctx, principal)
	if err != nil {
		return ActionResult{}, err
	}
	switch action {
	case "trial":
		return s.trial(ctx, principal, id, request)
	case "run":
		return s.run(ctx, principal, id, request)
	case "publish":
		return s.publish(ctx, principal, id, request, settings)
	case "approve":
		return s.review(ctx, principal, id, request, settings, true)
	case "reject":
		return s.review(ctx, principal, id, request, settings, false)
	case "withdraw":
		return s.withdraw(ctx, principal, id, request)
	case "revoke":
		return s.revoke(ctx, principal, id, request)
	case "invoke":
		return ActionResult{}, fmt.Errorf("%w: MCP invocation is not supported yet", apperrors.ErrUnsupportedOperation)
	default:
		return ActionResult{}, fmt.Errorf("%w: unknown assistant mesh action", apperrors.ErrInvalidArgument)
	}
}

func (s *Service) publish(ctx context.Context, principal domainidentity.Principal, id string, request ActionRequest, settings domainsettings.AssistantMeshSettings) (ActionResult, error) {
	if err := domain.ValidateAudience(principal, request.Audience); err != nil {
		return ActionResult{}, fmt.Errorf("%w: %v", apperrors.ErrInvalidArgument, err)
	}
	return s.changePublication(ctx, id, func(ctx context.Context, record domain.Record, value *payload) (Asset, error) {
		if record.OwnerID != principal.UserID || record.Kind == "mcp" {
			return Asset{}, apperrors.ErrNotFound
		}
		if value.Draft == nil || value.DraftDigest == "" || value.DraftDigest != request.ExpectedDigest {
			return Asset{}, fmt.Errorf("%w: draft digest is stale", apperrors.ErrConflict)
		}
		if value.Review != nil && value.Review.Status == "pending" {
			return Asset{}, fmt.Errorf("%w: a publication review is already pending", apperrors.ErrConflict)
		}
		findings, err := scanAsset(ctx, *value.Draft)
		if err != nil {
			return Asset{}, err
		}
		if len(findings) != 0 {
			return Asset{}, fmt.Errorf("%w: remove sensitive content before publishing", apperrors.ErrInvalidArgument)
		}
		if err := s.requireCompletedTrial(ctx, record, *value, principal, value.DraftDigest); err != nil {
			return Asset{}, err
		}
		now := nowUTC()
		expires, err := expiresAt(settings, record.Kind, request, now)
		if err != nil {
			return Asset{}, err
		}
		value.LastVersion++
		version := Version{Input: *value.Draft, Digest: value.DraftDigest, Version: value.LastVersion, Audience: request.Audience, ExpiresAt: expires, PublishedAt: now}
		action := "assistant.mesh.asset.publish"
		if version.Audience.Visibility == "organization" {
			candidate := version
			candidate.Digest = publicationDigest(version)
			value.Review = &Review{Status: "pending", Candidate: candidate}
			action = "assistant.mesh.asset.review.request"
		} else {
			value.Published, value.Review = &version, nil
		}
		err = s.audit(ctx, principal, action, record.ID, "success", map[string]any{"digest": version.Digest, "version": version.Version, "visibility": version.Audience.Visibility})
		return s.ownerAsset(record, *value), err
	})
}

func (s *Service) review(ctx context.Context, principal domainidentity.Principal, id string, request ActionRequest, settings domainsettings.AssistantMeshSettings, approve bool) (ActionResult, error) {
	if !s.canReview(ctx, settings, principal) {
		return ActionResult{}, apperrors.ErrAccessDenied
	}
	return s.changePublication(ctx, id, func(ctx context.Context, record domain.Record, value *payload) (Asset, error) {
		if record.OwnerID == principal.UserID || record.Kind == "mcp" {
			return Asset{}, apperrors.ErrAccessDenied
		}
		candidate, err := pendingVersion(*value, request.ExpectedDigest)
		if err != nil {
			return Asset{}, err
		}
		action := "assistant.mesh.asset.review.reject"
		if approve {
			if err := s.requireAssetOwner(ctx, record.OwnerID); err != nil {
				return Asset{}, err
			}
			if candidate.Audience.Visibility != "organization" || expired(candidate, nowUTC()) {
				return Asset{}, apperrors.ErrConflict
			}
			if err := s.requireCompletedTrial(ctx, record, *value, domainidentity.Principal{UserID: record.OwnerID}, value.DraftDigest); err != nil {
				return Asset{}, err
			}
			if record.Kind != domain.KindWorkPulse {
				now := nowUTC()
				if candidate.ExpiresAt != nil {
					deadline := now.Add(candidate.ExpiresAt.Sub(candidate.PublishedAt))
					candidate.ExpiresAt = &deadline
				}
				candidate.PublishedAt = now
			}
			candidate.Digest = value.DraftDigest
			value.Published = &candidate
			value.Review.Status, action = "approved", "assistant.mesh.asset.review.approve"
		} else {
			if strings.TrimSpace(request.Reason) == "" {
				return Asset{}, fmt.Errorf("%w: rejection reason is required", apperrors.ErrInvalidArgument)
			}
			value.Review.Status, value.Review.Reason = "rejected", strings.TrimSpace(request.Reason)
		}
		err = s.audit(ctx, principal, action, record.ID, "success", map[string]any{"digest": candidate.Digest, "version": candidate.Version, "ownerUserId": record.OwnerID})
		return s.reviewAsset(record, *value), err
	})
}

func pendingVersion(value payload, expectedDigest string) (Version, error) {
	if value.Review == nil || value.Review.Status != "pending" || expectedDigest == "" ||
		value.Review.Candidate.Digest != expectedDigest || value.DraftDigest != value.Review.Candidate.Input.Digest() {
		return Version{}, fmt.Errorf("%w: pending publication has changed", apperrors.ErrConflict)
	}
	return value.Review.Candidate, nil
}

func (s *Service) withdraw(ctx context.Context, principal domainidentity.Principal, id string, request ActionRequest) (ActionResult, error) {
	return s.changePublication(ctx, id, func(ctx context.Context, record domain.Record, value *payload) (Asset, error) {
		if record.OwnerID != principal.UserID || record.Kind == "mcp" {
			return Asset{}, apperrors.ErrNotFound
		}
		candidate, err := pendingVersion(*value, request.ExpectedDigest)
		if err != nil {
			return Asset{}, err
		}
		value.Review.Status, value.Review.Reason = "withdrawn", strings.TrimSpace(request.Reason)
		err = s.audit(ctx, principal, "assistant.mesh.asset.review.withdraw", id, "success", map[string]any{"digest": candidate.Digest})
		return s.ownerAsset(record, *value), err
	})
}

func (s *Service) revoke(ctx context.Context, principal domainidentity.Principal, id string, request ActionRequest) (ActionResult, error) {
	return s.changePublication(ctx, id, func(ctx context.Context, record domain.Record, value *payload) (Asset, error) {
		if record.OwnerID != principal.UserID || record.Kind == "mcp" {
			return Asset{}, apperrors.ErrNotFound
		}
		if value.Published == nil || request.ExpectedDigest != value.Published.Digest {
			return Asset{}, fmt.Errorf("%w: published version has changed", apperrors.ErrConflict)
		}
		digest := value.Published.Digest
		value.Published = nil
		if value.Review != nil {
			value.Review.Status = "withdrawn"
		}
		err := s.audit(ctx, principal, "assistant.mesh.asset.revoke", id, "success", map[string]any{"digest": digest})
		return s.ownerAsset(record, *value), err
	})
}

func (s *Service) changePublication(ctx context.Context, id string, change func(context.Context, domain.Record, *payload) (Asset, error)) (ActionResult, error) {
	var asset Asset
	err := s.store.Update(ctx, id, func(ctx context.Context, record domain.Record) (json.RawMessage, error) {
		value, err := decodePayload(record.Payload)
		if err != nil {
			return nil, err
		}
		asset, err = change(ctx, record, &value)
		if err != nil {
			return nil, err
		}
		return encodePayload(value)
	})
	if err != nil {
		return ActionResult{}, err
	}
	return ActionResult{Asset: asset}, nil
}

func (s *Service) requireCompletedTrial(ctx context.Context, record domain.Record, value payload, trialOwner domainidentity.Principal, digest string) error {
	if record.Kind != domain.KindSkill {
		return nil
	}
	if value.Trial == nil || value.Trial.Digest != digest || value.Trial.RunID == "" {
		return fmt.Errorf("%w: a successful trial of this exact draft is required", apperrors.ErrConflict)
	}
	if s.runner == nil {
		return apperrors.ErrServiceUnavailable
	}
	status, err := s.runner.MeshRunStatus(ctx, trialOwner, value.Trial.RunID, record.ID, digest)
	if err != nil {
		return err
	}
	if status != "completed" {
		return fmt.Errorf("%w: trial has not completed successfully", apperrors.ErrConflict)
	}
	return nil
}
