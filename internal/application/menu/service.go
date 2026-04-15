package menu

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/google/uuid"
	appaccess "github.com/kubecrux/kubecrux/internal/application/access"
	domainidentity "github.com/kubecrux/kubecrux/internal/domain/identity"
	domainmenu "github.com/kubecrux/kubecrux/internal/domain/menu"
	"github.com/kubecrux/kubecrux/internal/platform/apperrors"
	"gorm.io/gorm"
)

type Service struct {
	repo domainmenu.Repository
}

func New(repo domainmenu.Repository) *Service {
	return &Service{repo: repo}
}

func (s *Service) ListAll(ctx context.Context, principal domainidentity.Principal) ([]domainmenu.Record, error) {
	if err := authorizePrincipal(principal, appaccess.PermSystemMenusView); err != nil {
		return nil, err
	}
	items, err := s.repo.List(ctx)
	if err != nil {
		return nil, err
	}
	return buildTree(items), nil
}

func (s *Service) Get(ctx context.Context, principal domainidentity.Principal, menuID string) (domainmenu.Record, error) {
	if err := authorizePrincipal(principal, appaccess.PermSystemMenusView); err != nil {
		return domainmenu.Record{}, err
	}
	item, err := s.repo.Get(ctx, strings.TrimSpace(menuID))
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return domainmenu.Record{}, fmt.Errorf("%w: menu not found", apperrors.ErrNotFound)
		}
		return domainmenu.Record{}, err
	}
	return item, nil
}

func (s *Service) ListVisible(ctx context.Context, principal domainidentity.Principal) ([]domainmenu.Record, error) {
	items, err := s.repo.List(ctx)
	if err != nil {
		return nil, err
	}
	visible := make([]domainmenu.Record, 0)
	for _, item := range items {
		if !item.Enabled {
			continue
		}
		if len(item.RoleIDs) == 0 || overlaps(item.RoleIDs, principal.Roles) {
			visible = append(visible, item)
		}
	}
	visibleIDs := make(map[string]struct{}, len(visible))
	for _, item := range visible {
		visibleIDs[item.ID] = struct{}{}
	}
	filtered := make([]domainmenu.Record, 0, len(visible))
	for _, item := range visible {
		if item.ParentID != "" {
			if _, ok := visibleIDs[item.ParentID]; !ok {
				continue
			}
		}
		filtered = append(filtered, item)
	}
	return buildTree(filtered), nil
}

func (s *Service) Create(ctx context.Context, principal domainidentity.Principal, input domainmenu.Input) (domainmenu.Record, error) {
	if err := authorizePrincipal(principal, appaccess.PermSystemMenusManage); err != nil {
		return domainmenu.Record{}, err
	}
	item, err := normalizeInput(input)
	if err != nil {
		return domainmenu.Record{}, err
	}
	return s.repo.Create(ctx, item)
}

func (s *Service) Update(ctx context.Context, principal domainidentity.Principal, menuID string, input domainmenu.Input) (domainmenu.Record, error) {
	if err := authorizePrincipal(principal, appaccess.PermSystemMenusManage); err != nil {
		return domainmenu.Record{}, err
	}
	item, err := normalizeInput(input)
	if err != nil {
		return domainmenu.Record{}, err
	}
	item.ID = strings.TrimSpace(menuID)
	updated, err := s.repo.Update(ctx, menuID, item)
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return domainmenu.Record{}, fmt.Errorf("%w: menu not found", apperrors.ErrNotFound)
		}
		return domainmenu.Record{}, err
	}
	return updated, nil
}

func (s *Service) Delete(ctx context.Context, principal domainidentity.Principal, menuID string) error {
	if err := authorizePrincipal(principal, appaccess.PermSystemMenusManage); err != nil {
		return err
	}
	if strings.TrimSpace(menuID) == "" {
		return fmt.Errorf("%w: menu id is required", apperrors.ErrInvalidArgument)
	}
	if err := s.repo.Delete(ctx, menuID); err != nil {
		if err == gorm.ErrRecordNotFound {
			return fmt.Errorf("%w: menu not found", apperrors.ErrNotFound)
		}
		return err
	}
	return nil
}

func normalizeInput(input domainmenu.Input) (domainmenu.Record, error) {
	path := strings.TrimSpace(input.Path)
	labelZH := strings.TrimSpace(input.LabelZH)
	labelEN := strings.TrimSpace(input.LabelEN)
	iconKey := strings.TrimSpace(input.IconKey)
	section := strings.TrimSpace(input.Section)
	if path == "" {
		return domainmenu.Record{}, fmt.Errorf("%w: menu path is required", apperrors.ErrInvalidArgument)
	}
	if labelZH == "" || labelEN == "" {
		return domainmenu.Record{}, fmt.Errorf("%w: menu labels are required", apperrors.ErrInvalidArgument)
	}
	if iconKey == "" {
		return domainmenu.Record{}, fmt.Errorf("%w: menu icon key is required", apperrors.ErrInvalidArgument)
	}
	if section == "" {
		return domainmenu.Record{}, fmt.Errorf("%w: menu section is required", apperrors.ErrInvalidArgument)
	}
	id := strings.TrimSpace(input.ID)
	if id == "" {
		id = uuid.NewString()
	}
	return domainmenu.Record{
		ID:        id,
		ParentID:  strings.TrimSpace(input.ParentID),
		Path:      path,
		LabelZH:   labelZH,
		LabelEN:   labelEN,
		IconKey:   iconKey,
		Section:   section,
		SortOrder: input.SortOrder,
		Enabled:   input.Enabled,
		RoleIDs:   uniqueStrings(input.RoleIDs),
	}, nil
}

func buildTree(items []domainmenu.Record) []domainmenu.Record {
	nodes := make(map[string]*domainmenu.Record, len(items))
	for _, item := range items {
		copyItem := item
		copyItem.Children = nil
		nodes[item.ID] = &copyItem
	}
	rootIDs := make([]string, 0)
	for _, item := range items {
		node := nodes[item.ID]
		if item.ParentID == "" || nodes[item.ParentID] == nil {
			rootIDs = append(rootIDs, item.ID)
			continue
		}
		parent := nodes[item.ParentID]
		parent.Children = append(parent.Children, *node)
	}
	roots := make([]domainmenu.Record, 0, len(rootIDs))
	for _, rootID := range rootIDs {
		if node := nodes[rootID]; node != nil {
			roots = append(roots, *node)
		}
	}
	for index := range roots {
		sortChildren(&roots[index])
	}
	slices.SortFunc(roots, func(left, right domainmenu.Record) int {
		if left.Section != right.Section {
			if left.Section < right.Section {
				return -1
			}
			return 1
		}
		if left.SortOrder != right.SortOrder {
			return left.SortOrder - right.SortOrder
		}
		return strings.Compare(left.Path, right.Path)
	})
	return roots
}

func sortChildren(item *domainmenu.Record) {
	for index := range item.Children {
		sortChildren(&item.Children[index])
	}
	slices.SortFunc(item.Children, func(left, right domainmenu.Record) int {
		if left.SortOrder != right.SortOrder {
			return left.SortOrder - right.SortOrder
		}
		return strings.Compare(left.Path, right.Path)
	})
}

func overlaps(left []string, right []string) bool {
	for _, item := range left {
		if slices.Contains(right, item) {
			return true
		}
	}
	return false
}

func uniqueStrings(items []string) []string {
	unique := make([]string, 0, len(items))
	for _, item := range items {
		value := strings.TrimSpace(item)
		if value == "" || slices.Contains(unique, value) {
			continue
		}
		unique = append(unique, value)
	}
	return unique
}

func authorizePrincipal(principal domainidentity.Principal, permissionKey string) error {
	if appaccess.HasPermission(principal.Roles, permissionKey) {
		return nil
	}
	return fmt.Errorf("%w: missing permission %s", apperrors.ErrAccessDenied, permissionKey)
}
