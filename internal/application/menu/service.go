package menu

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/google/uuid"
	appaccess "github.com/soha/soha/internal/application/access"
	domainaudit "github.com/soha/soha/internal/domain/audit"
	domainidentity "github.com/soha/soha/internal/domain/identity"
	domainmenu "github.com/soha/soha/internal/domain/menu"
	domainoperation "github.com/soha/soha/internal/domain/operation"
	"github.com/soha/soha/internal/platform/apperrors"
	"github.com/soha/soha/internal/platform/operationentry"
	"github.com/soha/soha/internal/platform/requestctx"
	"gorm.io/gorm"
)

type AuditRecorder interface {
	Record(context.Context, domainaudit.Entry) error
}

type OperationRecorder interface {
	Record(context.Context, domainoperation.Entry) error
}

type Service struct {
	repo        domainmenu.Repository
	permissions *appaccess.PermissionResolver
	audit       AuditRecorder
	operations  OperationRecorder
}

func New(repo domainmenu.Repository, permissions *appaccess.PermissionResolver, audit AuditRecorder, operations OperationRecorder) *Service {
	return &Service{repo: repo, permissions: permissions, audit: audit, operations: operations}
}

func (s *Service) ListAll(ctx context.Context, principal domainidentity.Principal) ([]domainmenu.Record, error) {
	if err := s.authorize(ctx, principal, appaccess.PermSystemMenusView); err != nil {
		return nil, err
	}
	items, err := s.repo.List(ctx)
	if err != nil {
		return nil, err
	}
	return buildTree(items), nil
}

func (s *Service) Get(ctx context.Context, principal domainidentity.Principal, menuID string) (domainmenu.Record, error) {
	if err := s.authorize(ctx, principal, appaccess.PermSystemMenusView); err != nil {
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
	permissionKeys, err := appaccess.RuntimePermissionKeys(ctx, s.permissions, principal)
	if err != nil {
		return nil, err
	}
	itemsByID := make(map[string]domainmenu.Record, len(items))
	visibleIDs := make(map[string]struct{}, len(items))
	for _, item := range items {
		itemsByID[item.ID] = item
		if shouldShowMenu(item, principal.Roles, permissionKeys) {
			visibleIDs[item.ID] = struct{}{}
		}
	}
	for _, item := range items {
		if _, ok := visibleIDs[item.ID]; !ok {
			continue
		}
		for parentID := strings.TrimSpace(item.ParentID); parentID != ""; {
			parent, ok := itemsByID[parentID]
			if !ok || !parent.Enabled {
				break
			}
			if _, seen := visibleIDs[parentID]; seen {
				break
			}
			visibleIDs[parentID] = struct{}{}
			parentID = strings.TrimSpace(parent.ParentID)
		}
	}
	filtered := make([]domainmenu.Record, 0, len(visibleIDs))
	for _, item := range items {
		if _, ok := visibleIDs[item.ID]; !ok {
			continue
		}
		filtered = append(filtered, item)
	}
	return buildTree(filtered), nil
}

func shouldShowMenu(item domainmenu.Record, roleIDs []string, permissionKeys []string) bool {
	if !item.Enabled {
		return false
	}
	if isVisibleByPermissions(item, permissionKeys) {
		return true
	}
	if len(item.RoleIDs) > 0 {
		return overlaps(item.RoleIDs, roleIDs)
	}
	_, derived := permissionRuleForMenu(item)
	return !derived
}

func (s *Service) Create(ctx context.Context, principal domainidentity.Principal, input domainmenu.Input) (domainmenu.Record, error) {
	if err := s.authorize(ctx, principal, appaccess.PermSystemMenusManage); err != nil {
		return domainmenu.Record{}, err
	}
	item, err := normalizeInput(input)
	if err != nil {
		return domainmenu.Record{}, err
	}
	created, err := s.repo.Create(ctx, item)
	if err == nil {
		s.recordWriteLogs(ctx, principal, "system.menu.create", created, "success", "created menu")
	}
	return created, err
}

func (s *Service) Update(ctx context.Context, principal domainidentity.Principal, menuID string, input domainmenu.Input) (domainmenu.Record, error) {
	if err := s.authorize(ctx, principal, appaccess.PermSystemMenusManage); err != nil {
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
	s.recordWriteLogs(ctx, principal, "system.menu.update", updated, "success", "updated menu")
	return updated, nil
}

func (s *Service) Delete(ctx context.Context, principal domainidentity.Principal, menuID string) error {
	if err := s.authorize(ctx, principal, appaccess.PermSystemMenusManage); err != nil {
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
	s.recordDeleteLogs(ctx, principal, strings.TrimSpace(menuID))
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

func (s *Service) authorize(ctx context.Context, principal domainidentity.Principal, permissionKey string) error {
	return appaccess.AuthorizeRuntimePermission(ctx, s.permissions, principal, permissionKey)
}

func (s *Service) recordWriteLogs(ctx context.Context, principal domainidentity.Principal, operationType string, item domainmenu.Record, result string, summary string) {
	meta := requestctx.FromContext(ctx)
	if s.audit != nil {
		_ = s.audit.Record(ctx, domainaudit.Entry{
			ActorID:       principal.UserID,
			ActorName:     principal.UserName,
			Roles:         principal.Roles,
			Teams:         principal.Teams,
			ResourceKind:  "Menu",
			ResourceName:  item.Path,
			Action:        strings.TrimPrefix(operationType, "system.menu."),
			Result:        result,
			Summary:       summary,
			RequestPath:   meta.Path,
			RequestMethod: meta.Method,
			RequestID:     meta.RequestID,
			SourceIP:      meta.SourceIP,
			Metadata: map[string]any{
				"menuId":    item.ID,
				"labelZh":   item.LabelZH,
				"labelEn":   item.LabelEN,
				"parentId":  item.ParentID,
				"iconKey":   item.IconKey,
				"menuPath":  item.Path,
				"menuGroup": item.Section,
				"source":    meta.Source,
			},
		})
	}
	if s.operations != nil {
		_ = s.operations.Record(ctx, operationentry.New(
			ctx,
			principal,
			operationType,
			map[string]any{
				"module":       "system",
				"resourceKind": "Menu",
				"targetId":     item.ID,
				"targetLabel":  item.LabelZH,
				"targetPath":   item.Path,
			},
			result,
			summary,
			map[string]any{
				"menuId": item.ID,
				"path":   item.Path,
			},
		))
	}
}

func (s *Service) recordDeleteLogs(ctx context.Context, principal domainidentity.Principal, menuID string) {
	meta := requestctx.FromContext(ctx)
	if s.audit != nil {
		_ = s.audit.Record(ctx, domainaudit.Entry{
			ActorID:       principal.UserID,
			ActorName:     principal.UserName,
			Roles:         principal.Roles,
			Teams:         principal.Teams,
			ResourceKind:  "Menu",
			ResourceName:  menuID,
			Action:        "delete",
			Result:        "success",
			Summary:       "deleted menu",
			RequestPath:   meta.Path,
			RequestMethod: meta.Method,
			RequestID:     meta.RequestID,
			SourceIP:      meta.SourceIP,
			Metadata: map[string]any{
				"menuId": menuID,
				"source": meta.Source,
			},
		})
	}
	if s.operations != nil {
		_ = s.operations.Record(ctx, operationentry.New(
			ctx,
			principal,
			"system.menu.delete",
			map[string]any{
				"module":       "system",
				"resourceKind": "Menu",
				"targetId":     menuID,
				"targetLabel":  menuID,
			},
			"success",
			"deleted menu",
			map[string]any{"menuId": menuID},
		))
	}
}
