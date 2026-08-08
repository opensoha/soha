package companion

import (
	"context"
	"fmt"
	"strings"
	"time"

	appaccess "github.com/opensoha/soha/internal/application/access"
	domainaudit "github.com/opensoha/soha/internal/domain/audit"
	domaincompanion "github.com/opensoha/soha/internal/domain/companion"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainplugin "github.com/opensoha/soha/internal/domain/plugin"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"github.com/opensoha/soha/internal/platform/idempotency"
	"github.com/opensoha/soha/internal/platform/requestctx"
)

type PluginReader interface {
	GetInstalled(context.Context, string) (domainplugin.InstalledPlugin, error)
}

type AuditRecorder interface {
	Record(context.Context, domainaudit.Entry) error
}

type Service struct {
	repo        domaincompanion.Repository
	plugins     PluginReader
	permissions *appaccess.PermissionResolver
	audit       AuditRecorder
	now         func() time.Time
}

func New(repo domaincompanion.Repository, plugins PluginReader, permissions *appaccess.PermissionResolver, audit AuditRecorder) (*Service, error) {
	if repo == nil || plugins == nil || permissions == nil {
		return nil, fmt.Errorf("%w: companion dependencies are required", apperrors.ErrInvalidArgument)
	}
	return &Service{repo: repo, plugins: plugins, permissions: permissions, audit: audit, now: func() time.Time { return time.Now().UTC() }}, nil
}

func (s *Service) GetProfile(ctx context.Context, principal domainidentity.Principal) (domaincompanion.Profile, error) {
	if err := appaccess.AuthorizeRuntimePermission(ctx, s.permissions, principal, appaccess.PermObserveAIChatUse); err != nil {
		return domaincompanion.Profile{}, err
	}
	ownerID, err := principalOwner(principal)
	if err != nil {
		return domaincompanion.Profile{}, err
	}
	return s.repo.EnsureProfile(ctx, ownerID, s.now())
}

func (s *Service) RecordInteraction(ctx context.Context, principal domainidentity.Principal, key string, input domaincompanion.InteractionRequest) (domaincompanion.InteractionReceipt, bool, error) {
	if err := appaccess.AuthorizeRuntimePermission(ctx, s.permissions, principal, appaccess.PermObserveAIChatUse); err != nil {
		return domaincompanion.InteractionReceipt{}, false, err
	}
	ownerID, err := principalOwner(principal)
	if err != nil {
		return domaincompanion.InteractionReceipt{}, false, err
	}
	key = strings.TrimSpace(key)
	if len(key) < 8 || len(key) > 128 {
		return domaincompanion.InteractionReceipt{}, false, fmt.Errorf("%w: Idempotency-Key must contain 8 to 128 characters", apperrors.ErrInvalidArgument)
	}
	pluginID := strings.TrimSpace(input.PluginID)
	interactionID := strings.TrimSpace(input.InteractionID)
	if pluginID == "" || interactionID == "" {
		return domaincompanion.InteractionReceipt{}, false, fmt.Errorf("%w: pluginId and interactionId are required", apperrors.ErrInvalidArgument)
	}
	definition, unlocks, version, err := s.resolveInteraction(ctx, pluginID, interactionID)
	if err != nil {
		return domaincompanion.InteractionReceipt{}, false, err
	}
	_, inputHash, err := idempotency.Derive("companion.interaction", ownerID, key, input)
	if err != nil {
		return domaincompanion.InteractionReceipt{}, false, fmt.Errorf("derive companion interaction idempotency: %w", err)
	}
	receipt, created, err := s.repo.ApplyInteraction(ctx, domaincompanion.ApplyInteraction{
		OwnerID: ownerID, IdempotencyKey: key, InputHash: inputHash,
		PluginID: pluginID, Version: version, InteractionID: interactionID,
		Cooldown: time.Duration(definition.CooldownSeconds) * time.Second, ClientRevision: input.ClientRevision,
		RewardXP: definition.RewardXp, RewardAffinity: definition.RewardAffinity,
		Unlocks: unlocks, Now: s.now(),
	})
	if err != nil {
		return domaincompanion.InteractionReceipt{}, false, err
	}
	if created {
		s.recordAudit(ctx, principal, "interact", pluginID, interactionID, receipt.Profile)
	}
	return receipt, created, nil
}

func (s *Service) ResetProfile(ctx context.Context, principal domainidentity.Principal, key string, input domaincompanion.ProfileResetRequest) (domaincompanion.Profile, error) {
	if err := appaccess.AuthorizeRuntimePermission(ctx, s.permissions, principal, appaccess.PermObserveAIChatUse); err != nil {
		return domaincompanion.Profile{}, err
	}
	ownerID, err := principalOwner(principal)
	if err != nil {
		return domaincompanion.Profile{}, err
	}
	key = strings.TrimSpace(key)
	if len(key) < 8 || len(key) > 128 {
		return domaincompanion.Profile{}, fmt.Errorf("%w: Idempotency-Key must contain 8 to 128 characters", apperrors.ErrInvalidArgument)
	}
	pluginID := strings.TrimSpace(input.PluginID)
	version := domaincompanion.BuiltinVersion
	if pluginID == "" {
		pluginID = domaincompanion.BuiltinPluginID
	} else if pluginID != domaincompanion.BuiltinPluginID {
		item, err := s.plugins.GetInstalled(ctx, pluginID)
		if err != nil {
			return domaincompanion.Profile{}, err
		}
		if item.Type != "companion-pack" || item.Manifest.CompanionPack == nil {
			return domaincompanion.Profile{}, fmt.Errorf("%w: plugin is not a companion pack", apperrors.ErrInvalidArgument)
		}
		version = firstNonEmpty(item.ActiveVersion, item.Version)
	}
	input.PluginID = pluginID
	_, inputHash, err := idempotency.Derive("companion.reset", ownerID, key, input)
	if err != nil {
		return domaincompanion.Profile{}, fmt.Errorf("derive companion reset idempotency: %w", err)
	}
	profile, created, err := s.repo.ResetProfile(ctx, domaincompanion.ResetProfile{
		OwnerID: ownerID, IdempotencyKey: key, InputHash: inputHash,
		PluginID: pluginID, Version: version, Now: s.now(),
	})
	if err != nil {
		return domaincompanion.Profile{}, err
	}
	if created {
		s.recordAudit(ctx, principal, "reset", pluginID, "reset", profile)
	}
	return profile, nil
}

func (s *Service) resolveInteraction(ctx context.Context, pluginID, interactionID string) (domaincompanion.InteractionDefinition, []domaincompanion.UnlockDefinition, string, error) {
	if pluginID == domaincompanion.BuiltinPluginID {
		for _, item := range builtinInteractions() {
			if item.ID == interactionID {
				return item, builtinUnlocks(), domaincompanion.BuiltinVersion, nil
			}
		}
		return domaincompanion.InteractionDefinition{}, nil, "", fmt.Errorf("%w: companion interaction not found", apperrors.ErrNotFound)
	}
	item, err := s.plugins.GetInstalled(ctx, pluginID)
	if err != nil {
		return domaincompanion.InteractionDefinition{}, nil, "", err
	}
	if item.Type != "companion-pack" || item.Manifest.CompanionPack == nil {
		return domaincompanion.InteractionDefinition{}, nil, "", fmt.Errorf("%w: plugin is not a companion pack", apperrors.ErrInvalidArgument)
	}
	if item.Status != "enabled" {
		return domaincompanion.InteractionDefinition{}, nil, "", fmt.Errorf("%w: companion pack is not enabled", apperrors.ErrConflict)
	}
	for _, interaction := range item.Manifest.CompanionPack.Interactions {
		if interaction.ID == interactionID {
			return interaction, item.Manifest.CompanionPack.Unlocks, firstNonEmpty(item.ActiveVersion, item.Version), nil
		}
	}
	return domaincompanion.InteractionDefinition{}, nil, "", fmt.Errorf("%w: companion interaction not found", apperrors.ErrNotFound)
}

func builtinInteractions() []domaincompanion.InteractionDefinition {
	return []domaincompanion.InteractionDefinition{
		{ID: "tap", Action: "tap", RewardXp: 1, RewardAffinity: 1},
		{ID: "pet", Action: "pet", RewardXp: 2, RewardAffinity: 2, CooldownSeconds: 5},
		{ID: "greet", Action: "greet", RewardXp: 3, RewardAffinity: 1, CooldownSeconds: 30},
	}
}

func builtinUnlocks() []domaincompanion.UnlockDefinition {
	return []domaincompanion.UnlockDefinition{
		{ID: "mood.happy", Kind: "animation", Level: 2, Ref: "happy"},
		{ID: "action.play", Kind: "interaction", Level: 3, Ref: "play"},
	}
}

func principalOwner(principal domainidentity.Principal) (string, error) {
	ownerID := strings.TrimSpace(principal.UserID)
	if ownerID == "" {
		return "", fmt.Errorf("%w: authenticated user id is required", apperrors.ErrUnauthorized)
	}
	return ownerID, nil
}

func (s *Service) recordAudit(ctx context.Context, principal domainidentity.Principal, action, pluginID, interactionID string, profile domaincompanion.Profile) {
	if s.audit == nil {
		return
	}
	meta := requestctx.FromContext(ctx)
	_ = s.audit.Record(ctx, domainaudit.Entry{
		ActorID: principal.UserID, ActorName: principal.UserName, Roles: principal.Roles, Teams: principal.Teams,
		ResourceKind: "CompanionProfile", ResourceName: profile.ID, Action: "companion." + action,
		Result: "success", Summary: "updated companion progression",
		RequestPath: meta.Path, RequestMethod: meta.Method, RequestID: meta.RequestID, SourceIP: meta.SourceIP,
		Metadata: map[string]any{"pluginId": pluginID, "interactionId": interactionID, "level": profile.Level, "revision": profile.Revision},
	})
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
