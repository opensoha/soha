package assistantmesh

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	appaccess "github.com/opensoha/soha/internal/application/access"
	domain "github.com/opensoha/soha/internal/domain/assistantmesh"
	domainaudit "github.com/opensoha/soha/internal/domain/audit"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainsettings "github.com/opensoha/soha/internal/domain/settings"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type Store interface {
	Create(ctx context.Context, record domain.Record) error
	Get(ctx context.Context, id string) (domain.Record, error)
	List(ctx context.Context, kind, owner string, limit, offset int) ([]domain.Record, error)
	Update(ctx context.Context, id string, mutate func(context.Context, domain.Record) (json.RawMessage, error)) error
}

type Settings interface {
	ResolveAssistantMeshSettings(context.Context) (domainsettings.AssistantMeshSettings, error)
}

type Users interface {
	GetByID(context.Context, string) (domainidentity.User, error)
}

type Audit interface {
	Record(context.Context, domainaudit.Entry) error
}

type Service struct {
	store       Store
	config      Settings
	runner      Runner
	users       Users
	permissions *appaccess.PermissionResolver
	auditor     Audit
}

func New(store Store, config Settings, runner Runner, users Users, permissions *appaccess.PermissionResolver, auditor Audit) *Service {
	return &Service{store: store, config: config, runner: runner, users: users, permissions: permissions, auditor: auditor}
}

func (s *Service) Create(ctx context.Context, principal domainidentity.Principal, input domain.AssetInput) (Asset, error) {
	if _, err := s.prepare(ctx, principal); err != nil {
		return Asset{}, err
	}
	if err := validateInput(input); err != nil {
		return Asset{}, err
	}
	if s.store == nil {
		return Asset{}, apperrors.ErrServiceUnavailable
	}
	draft := input
	value := payload{Draft: &draft, DraftDigest: input.Digest(), Findings: findingsFor(ctx, input)}
	raw, err := encodePayload(value)
	if err != nil {
		return Asset{}, err
	}
	record := domain.Record{ID: uuid.NewString(), OwnerID: principal.UserID, Kind: input.Kind, Payload: raw}
	if err := s.store.Create(ctx, record); err != nil {
		return Asset{}, err
	}
	asset := s.ownerAsset(record, value)
	s.audit(ctx, principal, "assistant.mesh.asset.create", record.ID, "success", map[string]any{"kind": record.Kind, "digest": value.DraftDigest})
	return asset, nil
}

func (s *Service) Get(ctx context.Context, principal domainidentity.Principal, id string) (Asset, error) {
	settings, err := s.prepare(ctx, principal)
	if err != nil {
		return Asset{}, err
	}
	if strings.TrimSpace(id) == "" {
		return Asset{}, apperrors.ErrInvalidArgument
	}
	record, value, err := s.load(ctx, id)
	if err != nil {
		return Asset{}, err
	}
	if record.OwnerID == principal.UserID {
		asset := s.ownerAsset(record, value)
		if asset.Trial != nil && s.runner != nil {
			status, err := s.runner.MeshRunStatus(ctx, principal, asset.Trial.RunID, record.ID, asset.Trial.Digest)
			if err != nil {
				return Asset{}, err
			}
			asset.Trial.Status = status
		}
		return asset, nil
	}
	if value.Review != nil && value.Review.Status == "pending" && s.canReview(ctx, settings, principal) {
		return s.reviewAsset(record, value), nil
	}
	return s.sharedAsset(ctx, principal, record, value)
}

func (s *Service) List(ctx context.Context, principal domainidentity.Principal, view, kind string, offset int) (Page, error) {
	settings, err := s.prepare(ctx, principal)
	if err != nil {
		return Page{}, err
	}
	if offset < 0 {
		return Page{}, fmt.Errorf("%w: offset must not be negative", apperrors.ErrInvalidArgument)
	}
	if view != "mine" && view != "shared" && view != "review" {
		return Page{}, fmt.Errorf("%w: view must be mine, shared or review", apperrors.ErrInvalidArgument)
	}
	owner := ""
	if view == "mine" {
		owner = principal.UserID
	} else if view == "review" && !s.canReview(ctx, settings, principal) {
		return Page{}, apperrors.ErrAccessDenied
	}
	records, err := s.store.List(ctx, kind, owner, listPageSize, offset)
	if err != nil {
		return Page{}, err
	}
	items := make([]Asset, 0, len(records))
	owners := map[string]error{}
	for _, record := range records {
		if record.Kind == "mcp" {
			continue
		}
		value, err := decodePayload(record.Payload)
		if err != nil {
			return Page{}, err
		}
		item, visible := s.listAsset(ctx, principal, record, value, view, owners)
		if visible {
			items = append(items, item)
		}
	}
	page := Page{Items: items, Defaults: PublishingDefaults{
		Visibility: settings.DefaultVisibility, LifetimeDays: settings.ExperienceCardLifetimeDays,
		Permanent: settings.AllowPermanentExperienceCards,
	}}
	if len(records) == listPageSize {
		next := offset + len(records)
		page.NextOffset = &next
	}
	return page, nil
}

func (s *Service) listAsset(ctx context.Context, principal domainidentity.Principal, record domain.Record, value payload, view string, owners map[string]error) (Asset, bool) {
	switch view {
	case "mine":
		return s.ownerAsset(record, value), record.OwnerID == principal.UserID
	case "review":
		return s.reviewAsset(record, value), record.OwnerID != principal.UserID && value.Review != nil && value.Review.Status == "pending"
	default:
		if record.OwnerID == principal.UserID || !sharedVisible(record, value, principal) {
			return Asset{}, false
		}
		if _, seen := owners[record.OwnerID]; !seen {
			owners[record.OwnerID] = s.requireAssetOwner(ctx, record.OwnerID)
		}
		return Asset{ID: record.ID, OwnerUserID: record.OwnerID, Kind: record.Kind, Published: value.Published}, owners[record.OwnerID] == nil
	}
}

func sharedVisible(record domain.Record, value payload, principal domainidentity.Principal) bool {
	if value.Published == nil || expired(*value.Published, nowUTC()) {
		return false
	}
	return domain.AudienceAllows(record.OwnerID, value.Published.Audience, principal)
}

func (s *Service) load(ctx context.Context, id string) (domain.Record, payload, error) {
	if s.store == nil {
		return domain.Record{}, payload{}, apperrors.ErrServiceUnavailable
	}
	record, err := s.store.Get(ctx, id)
	if err != nil {
		if errors.Is(err, apperrors.ErrNotFound) {
			return domain.Record{}, payload{}, apperrors.ErrNotFound
		}
		return domain.Record{}, payload{}, err
	}
	if record.Kind == "mcp" {
		return domain.Record{}, payload{}, apperrors.ErrNotFound
	}
	value, err := decodePayload(record.Payload)
	if err != nil {
		return domain.Record{}, payload{}, apperrors.ErrServiceUnavailable
	}
	return record, value, nil
}
