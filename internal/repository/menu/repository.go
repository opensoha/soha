package menu

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"time"

	"github.com/google/uuid"
	domainmenu "github.com/kubecrux/kubecrux/internal/domain/menu"
	"gorm.io/gorm"
)

type Repository struct {
	db *gorm.DB
}

func New(db *gorm.DB) *Repository {
	return &Repository{db: db}
}

func (r *Repository) List(ctx context.Context) ([]domainmenu.Record, error) {
	rows, err := r.db.WithContext(ctx).Raw(`
		SELECT
			m.id,
			m.parent_id,
			m.path,
			m.label_zh,
			m.label_en,
			m.icon_key,
			m.section,
			m.sort_order,
			m.enabled
		FROM menus m
		ORDER BY COALESCE(m.section, '') ASC, m.sort_order ASC, m.path ASC
	`).Rows()
	if err != nil {
		return nil, fmt.Errorf("query menus: %w", err)
	}
	defer rows.Close()

	items := make([]domainmenu.Record, 0)
	menuIDs := make([]string, 0)
	for rows.Next() {
		item, err := scanMenu(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
		menuIDs = append(menuIDs, item.ID)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	roleMap, err := r.loadRoleBindings(ctx, menuIDs)
	if err != nil {
		return nil, err
	}
	for index := range items {
		items[index].RoleIDs = roleMap[items[index].ID]
	}
	return items, nil
}

func (r *Repository) Get(ctx context.Context, menuID string) (domainmenu.Record, error) {
	row := r.db.WithContext(ctx).Raw(`
		SELECT
			m.id,
			m.parent_id,
			m.path,
			m.label_zh,
			m.label_en,
			m.icon_key,
			m.section,
			m.sort_order,
			m.enabled
		FROM menus m
		WHERE m.id = ?
		LIMIT 1
	`, menuID).Row()

	var item domainmenu.Record
	var parentID sql.NullString
	var section sql.NullString
	if err := row.Scan(&item.ID, &parentID, &item.Path, &item.LabelZH, &item.LabelEN, &item.IconKey, &section, &item.SortOrder, &item.Enabled); err != nil {
		return domainmenu.Record{}, err
	}
	if parentID.Valid {
		item.ParentID = parentID.String
	}
	if section.Valid {
		item.Section = section.String
	}
	roleMap, err := r.loadRoleBindings(ctx, []string{item.ID})
	if err != nil {
		return domainmenu.Record{}, err
	}
	item.RoleIDs = roleMap[item.ID]
	return item, nil
}

func (r *Repository) Create(ctx context.Context, item domainmenu.Record) (domainmenu.Record, error) {
	tx := r.db.WithContext(ctx).Begin()
	if tx.Error != nil {
		return domainmenu.Record{}, tx.Error
	}
	now := time.Now().UTC()
	if err := tx.Exec(`
		INSERT INTO menus (id, parent_id, path, label_zh, label_en, icon_key, section, sort_order, enabled, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, item.ID, nullable(item.ParentID), item.Path, item.LabelZH, item.LabelEN, item.IconKey, nullable(item.Section), item.SortOrder, item.Enabled, now, now).Error; err != nil {
		tx.Rollback()
		return domainmenu.Record{}, err
	}
	if err := replaceBindings(tx, item.ID, item.RoleIDs, now); err != nil {
		tx.Rollback()
		return domainmenu.Record{}, err
	}
	if err := tx.Commit().Error; err != nil {
		return domainmenu.Record{}, err
	}
	return item, nil
}

func (r *Repository) Update(ctx context.Context, menuID string, item domainmenu.Record) (domainmenu.Record, error) {
	tx := r.db.WithContext(ctx).Begin()
	if tx.Error != nil {
		return domainmenu.Record{}, tx.Error
	}
	now := time.Now().UTC()
	result := tx.Exec(`
		UPDATE menus
		SET parent_id = ?, path = ?, label_zh = ?, label_en = ?, icon_key = ?, section = ?, sort_order = ?, enabled = ?, updated_at = ?
		WHERE id = ?
	`, nullable(item.ParentID), item.Path, item.LabelZH, item.LabelEN, item.IconKey, nullable(item.Section), item.SortOrder, item.Enabled, now, menuID)
	if result.Error != nil {
		tx.Rollback()
		return domainmenu.Record{}, result.Error
	}
	if result.RowsAffected == 0 {
		tx.Rollback()
		return domainmenu.Record{}, gorm.ErrRecordNotFound
	}
	if err := replaceBindings(tx, menuID, item.RoleIDs, now); err != nil {
		tx.Rollback()
		return domainmenu.Record{}, err
	}
	if err := tx.Commit().Error; err != nil {
		return domainmenu.Record{}, err
	}
	return item, nil
}

func (r *Repository) Delete(ctx context.Context, menuID string) error {
	result := r.db.WithContext(ctx).Exec(`DELETE FROM menus WHERE id = ?`, menuID)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

func replaceBindings(tx *gorm.DB, menuID string, roleIDs []string, now time.Time) error {
	if err := tx.Exec(`DELETE FROM menu_role_bindings WHERE menu_id = ?`, menuID).Error; err != nil {
		return err
	}
	for _, roleID := range roleIDs {
		if err := tx.Exec(`
			INSERT INTO menu_role_bindings (id, menu_id, role_id, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?)
		`, uuid.NewString(), menuID, roleID, now, now).Error; err != nil {
			return err
		}
	}
	return nil
}

func scanMenu(rows *sql.Rows) (domainmenu.Record, error) {
	var item domainmenu.Record
	var parentID sql.NullString
	var section sql.NullString
	if err := rows.Scan(&item.ID, &parentID, &item.Path, &item.LabelZH, &item.LabelEN, &item.IconKey, &section, &item.SortOrder, &item.Enabled); err != nil {
		return domainmenu.Record{}, err
	}
	if parentID.Valid {
		item.ParentID = parentID.String
	}
	if section.Valid {
		item.Section = section.String
	}
	return item, nil
}

func (r *Repository) loadRoleBindings(ctx context.Context, menuIDs []string) (map[string][]string, error) {
	if len(menuIDs) == 0 {
		return map[string][]string{}, nil
	}
	rows, err := r.db.WithContext(ctx).Raw(`
		SELECT menu_id, role_id
		FROM menu_role_bindings
		WHERE menu_id IN ?
		ORDER BY menu_id ASC, role_id ASC
	`, menuIDs).Rows()
	if err != nil {
		return nil, fmt.Errorf("query menu role bindings: %w", err)
	}
	defer rows.Close()

	items := make(map[string][]string, len(menuIDs))
	for _, menuID := range menuIDs {
		items[menuID] = []string{}
	}

	for rows.Next() {
		var menuID string
		var roleID string
		if err := rows.Scan(&menuID, &roleID); err != nil {
			return nil, fmt.Errorf("scan menu role binding: %w", err)
		}
		items[menuID] = append(items[menuID], roleID)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for key := range items {
		items[key] = compactSortedStrings(items[key])
	}
	return items, nil
}

func compactSortedStrings(items []string) []string {
	if len(items) == 0 {
		return []string{}
	}
	sort.Strings(items)
	result := items[:0]
	var last string
	for index, item := range items {
		if index == 0 || item != last {
			result = append(result, item)
			last = item
		}
	}
	return append([]string(nil), result...)
}

func nullable(value string) any {
	if value == "" {
		return nil
	}
	return value
}
