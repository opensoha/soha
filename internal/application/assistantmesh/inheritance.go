package assistantmesh

import (
	"context"
	"fmt"

	appaccess "github.com/opensoha/soha/internal/application/access"
	domain "github.com/opensoha/soha/internal/domain/assistantmesh"
	domainaudit "github.com/opensoha/soha/internal/domain/audit"
	identity "github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"github.com/opensoha/soha/internal/platform/requestctx"
)

type InheritanceStore interface {
	ListInheritableAssets(context.Context, string, string) (domain.InheritancePage, error)
	DisposeInheritedAssets(context.Context, domain.InheritanceInput, func(context.Context, []string) error) (int, error)
}

type Inheritance struct {
	assets *Service
	store  InheritanceStore
}

func NewInheritance(assets *Service, store InheritanceStore) *Inheritance {
	return &Inheritance{assets: assets, store: store}
}

func (s *Inheritance) List(ctx context.Context, actor identity.Principal, ownerID, after string) (domain.InheritancePage, error) {
	if err := s.authorize(ctx, actor); err != nil {
		return domain.InheritancePage{}, err
	}
	if !validReceptionID(ownerID) || (after != "" && !validReceptionID(after)) {
		return domain.InheritancePage{}, apperrors.ErrInvalidArgument
	}
	owner, err := s.assets.users.GetByID(ctx, ownerID)
	if err != nil {
		return domain.InheritancePage{}, err
	}
	if owner.Status != "disabled" {
		return domain.InheritancePage{}, fmt.Errorf("%w: the source account must be disabled", apperrors.ErrConflict)
	}
	return s.store.ListInheritableAssets(ctx, ownerID, after)
}

func (s *Inheritance) Transfer(ctx context.Context, actor identity.Principal, input domain.InheritanceInput) (domain.InheritanceResult, error) {
	if err := s.authorize(ctx, actor); err != nil {
		return domain.InheritanceResult{}, err
	}
	if !validReceptionID(input.HeirID) || input.OwnerID == input.HeirID {
		return domain.InheritanceResult{}, apperrors.ErrInvalidArgument
	}
	count, err := s.dispose(ctx, actor, input)
	return domain.InheritanceResult{Transferred: count}, err
}

func (s *Inheritance) Retire(ctx context.Context, actor identity.Principal, ownerID string, assetIDs []string) (int, error) {
	if err := s.authorize(ctx, actor); err != nil {
		return 0, err
	}
	return s.dispose(ctx, actor, domain.InheritanceInput{OwnerID: ownerID, AssetIDs: assetIDs})
}

func (s *Inheritance) dispose(ctx context.Context, actor identity.Principal, input domain.InheritanceInput) (int, error) {
	if err := validateInheritance(input); err != nil {
		return 0, err
	}
	if s.assets.auditor == nil {
		return 0, apperrors.ErrServiceUnavailable
	}
	return s.store.DisposeInheritedAssets(ctx, input, func(tx context.Context, ids []string) error {
		return s.record(tx, actor, input, ids)
	})
}

func (s *Inheritance) authorize(ctx context.Context, actor identity.Principal) error {
	if actor.AccessTokenID != "" || actor.GovernedAI != nil {
		return apperrors.ErrAccessDenied
	}
	if err := s.assets.requireActive(ctx, actor.UserID); err != nil {
		return err
	}
	return appaccess.AuthorizeRuntimePermission(ctx, s.assets.permissions, actor, appaccess.PermAccessUsersInheritHeyaAssets)
}

func validateInheritance(input domain.InheritanceInput) error {
	if !validReceptionID(input.OwnerID) ||
		len(input.AssetIDs) > 100 || (input.AssetIDs != nil && len(input.AssetIDs) == 0) {
		return apperrors.ErrInvalidArgument
	}
	seen := make(map[string]struct{}, len(input.AssetIDs))
	for _, id := range input.AssetIDs {
		if _, duplicate := seen[id]; !validReceptionID(id) || duplicate {
			return apperrors.ErrInvalidArgument
		}
		seen[id] = struct{}{}
	}
	return nil
}

func (s *Inheritance) record(ctx context.Context, actor identity.Principal, input domain.InheritanceInput, ids []string) error {
	meta := requestctx.FromContext(ctx)
	action, summary := "assistant_mesh.inheritance.transfer", "Transferred published Heya assets"
	metadata := map[string]any{"ownerId": input.OwnerID, "assetIds": ids, "count": len(ids)}
	if input.HeirID != "" {
		metadata["heirId"] = input.HeirID
	} else {
		action, summary = "assistant_mesh.inheritance.retire", "Retired published Heya assets"
	}
	return s.assets.auditor.Record(ctx, domainaudit.Entry{
		ActorID: actor.UserID, ActorName: actor.UserName, Roles: actor.Roles, Teams: actor.Teams,
		ResourceKind: "AssistantMeshInheritance", ResourceName: input.OwnerID, Action: action,
		Result: "success", Summary: summary,
		RequestPath: meta.Path, RequestMethod: meta.Method, RequestID: meta.RequestID, SourceIP: meta.SourceIP,
		Metadata: metadata,
	})
}
