package assistantmesh

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"time"

	appaccess "github.com/opensoha/soha/internal/application/access"
	"github.com/opensoha/soha/internal/application/contentdlp"
	domain "github.com/opensoha/soha/internal/domain/assistantmesh"
	domainaudit "github.com/opensoha/soha/internal/domain/audit"
	domaindlp "github.com/opensoha/soha/internal/domain/contentdlp"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainsettings "github.com/opensoha/soha/internal/domain/settings"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"github.com/opensoha/soha/internal/platform/requestctx"
)

const (
	listPageSize = 50
	maxFindings  = 64
)

func publicationDigest(value any) string {
	raw, _ := json.Marshal(value)
	return fmt.Sprintf("%x", sha256.Sum256(raw))
}

// The platform policy scans every user-owned text field with the default
// private-key/credential detectors plus bearer tokens and private networks.
var meshDLPPolicy = domaindlp.Content{
	Enabled:         true,
	Mode:            "block",
	DetectorVersion: domaindlp.ArchiveDetectorVersion,
	Detectors:       []string{"private_key", "aws_access_key", "credential_assignment"},
	Rules: []domaindlp.Rule{
		{ID: "bearer_token", Expression: `(?i)\bbearer\s+[A-Za-z0-9._~+/=-]{12,}`},
		{ID: "internal_ip", Expression: `\b(?:10\.\d{1,3}|192\.168|172\.(?:1[6-9]|2[0-9]|3[01]))\.\d{1,3}\.\d{1,3}\b`},
	},
}

func encodePayload(value payload) (json.RawMessage, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("encode assistant mesh asset: %w", err)
	}
	return data, nil
}

func decodePayload(raw json.RawMessage) (payload, error) {
	var value payload
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &value); err != nil {
			return value, fmt.Errorf("decode assistant mesh asset: %w", err)
		}
	}
	return value, nil
}

func validateInput(input domain.AssetInput) error {
	if input.Kind == "mcp" {
		return fmt.Errorf("%w: MCP assets are not supported yet", apperrors.ErrUnsupportedOperation)
	}
	if err := input.Validate(); err != nil {
		return fmt.Errorf("%w: %v", apperrors.ErrInvalidArgument, err)
	}
	return nil
}

// scanAsset fails closed: any detector error blocks the caller before a trial
// or publication can be accepted.
func scanAsset(ctx context.Context, input domain.AssetInput) ([]string, error) {
	segments := []string{input.Title, input.Body, input.Applicability, input.BlockerReason}
	for name, content := range input.Files {
		if domain.IsTextArchive(name) {
			data, err := domain.DecodeTextArchive(content)
			if err != nil {
				return nil, err
			}
			content = string(data)
		}
		segments = append(segments, name, content)
	}
	for _, dependency := range input.Dependencies {
		segments = append(segments, dependency.ID, dependency.Version)
	}
	for name, parameter := range input.Parameters {
		segments = append(segments, name, parameter.Type, parameter.Description)
	}
	seen := map[string]struct{}{}
	findings := make([]string, 0, 4)
	for _, segment := range segments {
		if segment == "" {
			continue
		}
		decision := contentdlp.Inspect(ctx, meshDLPPolicy, []byte(segment))
		switch decision.Status {
		case "clean":
		case "matched":
		default:
			return nil, fmt.Errorf("%w: content scan %s", apperrors.ErrServiceUnavailable, decision.Status)
		}
		for _, id := range decision.MatchedRuleIDs {
			if _, ok := seen[id]; ok {
				continue
			}
			seen[id] = struct{}{}
			if len(findings) < maxFindings {
				findings = append(findings, id)
			}
		}
	}
	slices.Sort(findings)
	return findings, nil
}

func findingsFor(ctx context.Context, input domain.AssetInput) []string {
	findings, err := scanAsset(ctx, input)
	if err != nil {
		return []string{"inspection_unavailable"}
	}
	return findings
}

func workPulseLifetime(settings domainsettings.AssistantMeshSettings) (defaultHours, maxDays int) {
	defaults := domainsettings.DefaultAssistantMeshSettings()
	return configuredValue(settings.WorkPulseDefaultLifetimeHours, defaults.WorkPulseDefaultLifetimeHours),
		configuredValue(settings.WorkPulseMaxLifetimeDays, defaults.WorkPulseMaxLifetimeDays)
}

func expiresAt(settings domainsettings.AssistantMeshSettings, kind string, request ActionRequest, now time.Time) (*time.Time, error) {
	if kind == domain.KindWorkPulse {
		if request.Permanent || request.LifetimeDays != 0 {
			return nil, fmt.Errorf("%w: work pulses require an hours lifetime", apperrors.ErrInvalidArgument)
		}
		defaultHours, maxDays := workPulseLifetime(settings)
		hours := configuredValue(request.LifetimeHours, defaultHours)
		if maxHours := maxDays * 24; hours < 1 || hours > maxHours {
			return nil, fmt.Errorf("%w: work pulse lifetime hours must be between 1 and %d", apperrors.ErrInvalidArgument, maxHours)
		}
		expires := now.Add(time.Duration(hours) * time.Hour)
		return &expires, nil
	}
	if request.LifetimeHours != 0 {
		return nil, fmt.Errorf("%w: lifetime hours are only valid for work pulses", apperrors.ErrInvalidArgument)
	}
	if request.Permanent {
		if kind == domain.KindExperienceCard && !settings.AllowPermanentExperienceCards {
			return nil, fmt.Errorf("%w: permanent experience cards are disabled", apperrors.ErrInvalidArgument)
		}
		return nil, nil
	}
	days := request.LifetimeDays
	if days == 0 && kind == domain.KindExperienceCard {
		days = settings.ExperienceCardLifetimeDays
		if days == 0 {
			days = domainsettings.DefaultAssistantMeshSettings().ExperienceCardLifetimeDays
		}
	}
	if days == 0 {
		return nil, nil
	}
	if days < 7 || days > 365 {
		return nil, fmt.Errorf("%w: lifetime days must be between 7 and 365", apperrors.ErrInvalidArgument)
	}
	expires := now.AddDate(0, 0, days)
	return &expires, nil
}

func nowUTC() time.Time { return time.Now().UTC() }

func expired(version Version, now time.Time) bool {
	return version.ExpiresAt != nil && !now.Before(*version.ExpiresAt)
}

func (s *Service) settings(ctx context.Context) (domainsettings.AssistantMeshSettings, error) {
	if s.config == nil {
		return domainsettings.AssistantMeshSettings{}, apperrors.ErrServiceUnavailable
	}
	return s.config.ResolveAssistantMeshSettings(ctx)
}

// prepare enforces the employee access gate shared by every public method.
func (s *Service) prepare(ctx context.Context, principal domainidentity.Principal) (domainsettings.AssistantMeshSettings, error) {
	if principal.UserID == "" {
		return domainsettings.AssistantMeshSettings{}, apperrors.ErrUnauthorized
	}
	if err := s.requireActive(ctx, principal.UserID); err != nil {
		return domainsettings.AssistantMeshSettings{}, err
	}
	settings, err := s.settings(ctx)
	if err != nil {
		return domainsettings.AssistantMeshSettings{}, err
	}
	if !settings.Enabled {
		return domainsettings.AssistantMeshSettings{}, fmt.Errorf("%w: assistant mesh is disabled", apperrors.ErrAccessDenied)
	}
	if err := appaccess.AuthorizeRuntimePermission(ctx, s.permissions, principal, appaccess.PermObserveAIChatUse); err != nil {
		return domainsettings.AssistantMeshSettings{}, err
	}
	return settings, nil
}

func (s *Service) requireActive(ctx context.Context, userID string) error {
	if s.users == nil {
		return apperrors.ErrServiceUnavailable
	}
	user, err := s.users.GetByID(ctx, userID)
	if err != nil {
		if errors.Is(err, apperrors.ErrNotFound) {
			return apperrors.ErrUnauthorized
		}
		return err
	}
	if user.ID != userID || user.Status != "active" {
		return apperrors.ErrUnauthorized
	}
	return nil
}

// requireAssetOwner treats a revoked or inactive owner as absent so shared
// readers never see a stale asset.
func (s *Service) requireAssetOwner(ctx context.Context, ownerID string) error {
	if err := s.requireActive(ctx, ownerID); err != nil {
		return apperrors.ErrNotFound
	}
	return nil
}

func (s *Service) canReview(ctx context.Context, settings domainsettings.AssistantMeshSettings, principal domainidentity.Principal) bool {
	if slices.Contains(settings.OrganizationApproverUserIDs, principal.UserID) {
		return true
	}
	if len(settings.OrganizationApproverUserIDs) > 0 || s.permissions == nil {
		return false
	}
	return appaccess.AuthorizeRuntimePermission(ctx, s.permissions, principal, appaccess.ManagedActionPermission(appaccess.PermSettingsAIManage, "update")) == nil
}

func (s *Service) ownerAsset(record domain.Record, value payload) Asset {
	asset := Asset{
		ID: record.ID, OwnerUserID: record.OwnerID, OriginalAuthorUserID: value.OriginalAuthorID, Kind: record.Kind,
		Draft: value.Draft, DraftDigest: value.DraftDigest,
		Published: value.Published, Review: value.Review, Trial: value.Trial,
	}
	if len(value.Findings) > 0 {
		asset.Findings = append([]string(nil), value.Findings...)
	}
	return asset
}

func (s *Service) reviewAsset(record domain.Record, value payload) Asset {
	return Asset{ID: record.ID, OwnerUserID: record.OwnerID, OriginalAuthorUserID: value.OriginalAuthorID, Kind: record.Kind, Review: value.Review}
}

func (s *Service) sharedAsset(ctx context.Context, principal domainidentity.Principal, record domain.Record, value payload) (Asset, error) {
	if value.Published == nil || expired(*value.Published, nowUTC()) {
		return Asset{}, apperrors.ErrNotFound
	}
	if err := s.requireAssetOwner(ctx, record.OwnerID); err != nil {
		return Asset{}, err
	}
	if !domain.AudienceAllows(record.OwnerID, value.Published.Audience, principal) {
		return Asset{}, apperrors.ErrNotFound
	}
	return Asset{ID: record.ID, OwnerUserID: record.OwnerID, OriginalAuthorUserID: value.OriginalAuthorID, Kind: record.Kind, Published: value.Published}, nil
}

func (s *Service) audit(ctx context.Context, principal domainidentity.Principal, action, id, result string, metadata map[string]any) error {
	if s.auditor == nil {
		return nil
	}
	meta := requestctx.FromContext(ctx)
	return s.auditor.Record(ctx, domainaudit.Entry{
		ActorID: principal.UserID, ActorName: principal.UserName, Roles: principal.Roles, Teams: principal.Teams,
		ResourceKind: "AssistantMeshAsset", ResourceName: id,
		Action: action, Result: result, Summary: "Heya Assistant Mesh asset " + action,
		RequestPath: meta.Path, RequestMethod: meta.Method, RequestID: meta.RequestID, SourceIP: meta.SourceIP,
		Metadata: metadata, CreatedAt: time.Now().UTC(),
	})
}

// auditCompletedAssetAction records actions whose durable change or run launch has already completed.
func (s *Service) auditCompletedAssetAction(ctx context.Context, principal domainidentity.Principal, action, id string, metadata map[string]any) {
	if err := s.audit(ctx, principal, action, id, "success", metadata); err != nil {
		slog.WarnContext(ctx, "failed to record completed Heya asset action", "action", action, "asset_id", id)
	}
}
