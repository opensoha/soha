package assistantmesh

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf8"

	domain "github.com/opensoha/soha/internal/domain/assistantmesh"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type RunSpec struct {
	AssetID    string            `json:"assetId"`
	Digest     string            `json:"digest"`
	Version    int               `json:"version,omitempty"`
	SessionID  string            `json:"sessionId"`
	RequestID  string            `json:"requestId"`
	Prompt     string            `json:"prompt"`
	Input      domain.AssetInput `json:"input"`
	Parameters map[string]any    `json:"parameters,omitempty"`
	Trial      bool              `json:"trial"`
}

type Runner interface {
	LaunchMeshSkill(ctx context.Context, caller domainidentity.Principal, spec RunSpec) (string, error)
	MeshRunStatus(ctx context.Context, caller domainidentity.Principal, runID, assetID, digest string) (string, error)
}

func (s *Service) trial(ctx context.Context, principal domainidentity.Principal, id string, request ActionRequest) (ActionResult, error) {
	record, value, err := s.load(ctx, id)
	if err != nil {
		return ActionResult{}, err
	}
	if record.OwnerID != principal.UserID {
		return ActionResult{}, apperrors.ErrNotFound
	}
	if record.Kind != domain.KindSkill {
		return ActionResult{}, apperrors.ErrUnsupportedOperation
	}
	if value.Draft == nil || value.DraftDigest == "" {
		return ActionResult{}, fmt.Errorf("%w: draft is required", apperrors.ErrConflict)
	}
	if request.ExpectedDigest != value.DraftDigest {
		return ActionResult{}, fmt.Errorf("%w: draft digest is stale", apperrors.ErrConflict)
	}
	if err := requireSession(request); err != nil {
		return ActionResult{}, err
	}
	findings, err := scanAsset(ctx, *value.Draft)
	if err != nil {
		return ActionResult{}, err
	}
	if len(findings) > 0 {
		return ActionResult{}, fmt.Errorf("%w: remove sensitive content before trial", apperrors.ErrInvalidArgument)
	}
	if s.runner == nil {
		return ActionResult{}, apperrors.ErrServiceUnavailable
	}
	digest := value.DraftDigest
	runID, err := s.runner.LaunchMeshSkill(ctx, principal, RunSpec{
		AssetID: id, Digest: digest, SessionID: request.SessionID, RequestID: request.RequestID,
		Prompt: request.Prompt, Input: *value.Draft, Parameters: request.Parameters, Trial: true,
	})
	if err != nil {
		return ActionResult{}, err
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
		if value.DraftDigest != digest {
			return nil, fmt.Errorf("%w: draft changed while starting the trial", apperrors.ErrConflict)
		}
		value.Trial = &Trial{RunID: runID, Digest: digest, Status: "queued"}
		raw, err := encodePayload(value)
		if err != nil {
			return nil, err
		}
		asset = s.ownerAsset(record, value)
		return raw, nil
	})
	if err != nil {
		return ActionResult{}, err
	}
	s.auditCompletedAssetAction(ctx, principal, "assistant.mesh.asset.trial", id, map[string]any{"digest": digest, "runId": runID})
	return ActionResult{Asset: asset, RunID: runID}, nil
}

func (s *Service) run(ctx context.Context, principal domainidentity.Principal, id string, request ActionRequest) (ActionResult, error) {
	if err := requireSession(request); err != nil {
		return ActionResult{}, err
	}
	record, value, version, err := s.resolvePublished(ctx, principal, id, request.ExpectedDigest)
	if err != nil {
		return ActionResult{}, err
	}
	if s.runner == nil {
		return ActionResult{}, apperrors.ErrServiceUnavailable
	}
	runID, err := s.runner.LaunchMeshSkill(ctx, principal, RunSpec{
		AssetID: record.ID, Digest: version.Digest, Version: version.Version, SessionID: request.SessionID, RequestID: request.RequestID,
		Prompt: request.Prompt, Input: version.Input, Parameters: request.Parameters,
	})
	if err != nil {
		return ActionResult{}, err
	}
	asset, err := s.sharedAsset(ctx, principal, record, value)
	if err != nil {
		return ActionResult{}, err
	}
	s.auditCompletedAssetAction(ctx, principal, "assistant.mesh.asset.run", id, map[string]any{"digest": version.Digest, "runId": runID})
	return ActionResult{Asset: asset, RunID: runID}, nil
}

// ResolveMeshSkill is the runtime claim/callback entry: it re-checks the caller,
// the exact frozen digest, the asset owner and the audience on every use.
func (s *Service) ResolveMeshSkill(ctx context.Context, principal domainidentity.Principal, id, digest string, version int, trial bool) (domain.AssetInput, error) {
	if _, err := s.prepare(ctx, principal); err != nil {
		return domain.AssetInput{}, err
	}
	if trial {
		record, value, err := s.load(ctx, id)
		if err != nil {
			return domain.AssetInput{}, err
		}
		if record.OwnerID != principal.UserID || record.Kind != domain.KindSkill || digest == "" ||
			value.Draft == nil || value.DraftDigest != digest || len(value.Findings) != 0 {
			return domain.AssetInput{}, apperrors.ErrAccessDenied
		}
		return *value.Draft, nil
	}
	_, _, published, err := s.resolvePublished(ctx, principal, id, digest)
	if err != nil {
		return domain.AssetInput{}, err
	}
	if version <= 0 || published.Version != version {
		return domain.AssetInput{}, apperrors.ErrAccessDenied
	}
	return published.Input, nil
}

func (s *Service) resolvePublished(ctx context.Context, principal domainidentity.Principal, id, digest string) (domain.Record, payload, Version, error) {
	record, value, err := s.load(ctx, id)
	if err != nil {
		return domain.Record{}, payload{}, Version{}, err
	}
	if record.Kind != domain.KindSkill || value.Published == nil || digest == "" || value.Published.Digest != digest {
		return domain.Record{}, payload{}, Version{}, apperrors.ErrNotFound
	}
	if expired(*value.Published, nowUTC()) {
		return domain.Record{}, payload{}, Version{}, apperrors.ErrNotFound
	}
	if err := s.requireAssetOwner(ctx, record.OwnerID); err != nil {
		return domain.Record{}, payload{}, Version{}, err
	}
	if !domain.AudienceAllows(record.OwnerID, value.Published.Audience, principal) {
		return domain.Record{}, payload{}, Version{}, apperrors.ErrAccessDenied
	}
	return record, value, *value.Published, nil
}

func requireSession(request ActionRequest) error {
	if strings.TrimSpace(request.SessionID) == "" || strings.TrimSpace(request.Prompt) == "" ||
		utf8.RuneCountInString(request.Prompt) > 4096 || len(request.RequestID) > 128 {
		return fmt.Errorf("%w: sessionId and prompt are required", apperrors.ErrInvalidArgument)
	}
	return nil
}
