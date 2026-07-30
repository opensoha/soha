package manifest

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	domainmanifest "github.com/opensoha/soha/internal/domain/manifest"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"gorm.io/gorm"
)

type Repository struct{ db *gorm.DB }

func New(db *gorm.DB) *Repository { return &Repository{db: db} }

func (r *Repository) List(ctx context.Context, filter domainmanifest.Filter) (domainmanifest.Page, error) {
	page, pageSize := normalizePage(filter)
	where, args, empty := manifestWhere(filter)
	if empty {
		return domainmanifest.Page{Items: []domainmanifest.Package{}, Page: page, PageSize: pageSize}, nil
	}
	var total int
	if err := r.db.WithContext(ctx).Raw(`SELECT COUNT(*) FROM manifest_packages WHERE archived_at IS NULL`+where, args...).Scan(&total).Error; err != nil {
		return domainmanifest.Page{}, fmt.Errorf("count manifest packages: %w", err)
	}
	query := `SELECT id, name, description, application_id, business_line_id, renderer, status, current_revision, files, bindings, created_by, updated_by, created_at, updated_at FROM manifest_packages WHERE archived_at IS NULL` + where + ` ORDER BY updated_at DESC LIMIT ? OFFSET ?`
	queryArgs := append(append([]any{}, args...), pageSize, (page-1)*pageSize)
	rows, err := r.db.WithContext(ctx).Raw(query, queryArgs...).Rows()
	if err != nil {
		return domainmanifest.Page{}, fmt.Errorf("list manifest packages: %w", err)
	}
	defer func() { _ = rows.Close() }()
	items := make([]domainmanifest.Package, 0, pageSize)
	for rows.Next() {
		item, scanErr := scanPackage(rows)
		if scanErr != nil {
			return domainmanifest.Page{}, scanErr
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return domainmanifest.Page{}, err
	}
	return domainmanifest.Page{Items: items, Total: total, Page: page, PageSize: pageSize}, nil
}

func manifestWhere(filter domainmanifest.Filter) (string, []any, bool) {
	query := ""
	args := make([]any, 0, 6)
	if value := strings.TrimSpace(filter.ApplicationID); value != "" {
		query += ` AND application_id = ?`
		args = append(args, value)
	} else if filter.ApplicationIDs != nil {
		if len(filter.ApplicationIDs) == 0 {
			return "", nil, true
		}
		query += ` AND application_id IN ?`
		args = append(args, filter.ApplicationIDs)
	}
	clusterID := strings.TrimSpace(filter.ClusterID)
	namespace := strings.TrimSpace(filter.Namespace)
	if clusterID != "" || namespace != "" {
		binding := make(map[string]string, 2)
		if clusterID != "" {
			binding["clusterId"] = clusterID
		}
		if namespace != "" {
			binding["namespace"] = namespace
		}
		query += ` AND bindings @> ?::jsonb`
		payload, _ := json.Marshal([]map[string]string{binding})
		args = append(args, string(payload))
	}
	if value := strings.TrimSpace(filter.Search); value != "" {
		query += ` AND (LOWER(name) LIKE LOWER(?) OR LOWER(application_id) LIKE LOWER(?))`
		args = append(args, "%"+value+"%", "%"+value+"%")
	}
	return query, args, false
}

func normalizePage(filter domainmanifest.Filter) (int, int) {
	page := filter.Page
	if page <= 0 {
		page = 1
	}
	pageSize := filter.PageSize
	if pageSize <= 0 {
		pageSize = filter.Limit
	}
	if pageSize <= 0 || pageSize > 100 {
		pageSize = 20
	}
	return page, pageSize
}

func (r *Repository) Get(ctx context.Context, id string) (domainmanifest.Package, error) {
	row := r.db.WithContext(ctx).Raw(`SELECT id, name, description, application_id, business_line_id, renderer, status, current_revision, files, bindings, created_by, updated_by, created_at, updated_at FROM manifest_packages WHERE id = ? AND archived_at IS NULL LIMIT 1`, id).Row()
	item, err := scanPackageRow(row)
	if errors.Is(err, sql.ErrNoRows) {
		return domainmanifest.Package{}, apperrors.ErrNotFound
	}
	return item, err
}

func (r *Repository) Create(ctx context.Context, item domainmanifest.Package) (domainmanifest.Package, error) {
	files, bindings, err := encodePackageJSON(item)
	if err != nil {
		return domainmanifest.Package{}, err
	}
	err = r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec(`INSERT INTO manifest_packages (id, name, description, application_id, business_line_id, renderer, status, current_revision, files, bindings, created_by, updated_by, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?::jsonb, ?::jsonb, ?, ?, ?, ?)`, item.ID, item.Name, item.Description, item.ApplicationID, item.BusinessLineID, item.Renderer, item.Status, item.CurrentRevision, files, bindings, item.CreatedBy, item.UpdatedBy, item.CreatedAt, item.UpdatedAt).Error; err != nil {
			return err
		}
		return syncLegacyBindingRelations(tx, item.ID, item.Bindings, item.UpdatedAt)
	})
	if err != nil {
		return domainmanifest.Package{}, fmt.Errorf("create manifest package: %w", err)
	}
	return r.Get(ctx, item.ID)
}

func (r *Repository) Update(ctx context.Context, id string, item domainmanifest.Package) (domainmanifest.Package, error) {
	files, bindings, err := encodePackageJSON(item)
	if err != nil {
		return domainmanifest.Package{}, err
	}
	err = r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		result := tx.Exec(`UPDATE manifest_packages SET name=?, description=?, application_id=?, business_line_id=?, renderer=?, status=?, files=?::jsonb, bindings=?::jsonb, updated_by=?, updated_at=? WHERE id=? AND archived_at IS NULL`, item.Name, item.Description, item.ApplicationID, item.BusinessLineID, item.Renderer, item.Status, files, bindings, item.UpdatedBy, item.UpdatedAt, id)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return apperrors.ErrNotFound
		}
		return syncLegacyBindingRelations(tx, id, item.Bindings, item.UpdatedAt)
	})
	if err != nil {
		return domainmanifest.Package{}, fmt.Errorf("update manifest package: %w", err)
	}
	return r.Get(ctx, id)
}

func (r *Repository) Delete(ctx context.Context, id string) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		result := tx.Exec(`DELETE FROM manifest_packages WHERE id = ? AND archived_at IS NULL AND current_revision = 0`, id)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected > 0 {
			return nil
		}
		result = tx.Exec(`UPDATE manifest_packages SET archived_at=CURRENT_TIMESTAMP, updated_at=CURRENT_TIMESTAMP WHERE id = ? AND archived_at IS NULL AND current_revision > 0`, id)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return apperrors.ErrNotFound
		}
		return nil
	})
}

func (r *Repository) Publish(ctx context.Context, item domainmanifest.Package, revision domainmanifest.Revision) (domainmanifest.Package, error) {
	files, bindings, err := encodePackageJSON(item)
	if err != nil {
		return domainmanifest.Package{}, err
	}
	err = r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec(`INSERT INTO manifest_revisions (id, package_id, version, digest, note, files, bindings, created_by, created_at) VALUES (?, ?, ?, ?, ?, ?::jsonb, ?::jsonb, ?, ?)`, revision.ID, revision.PackageID, revision.Version, revision.Digest, revision.Note, files, bindings, revision.CreatedBy, revision.CreatedAt).Error; err != nil {
			return err
		}
		result := tx.Exec(`UPDATE manifest_packages SET status=?, current_revision=?, updated_by=?, updated_at=? WHERE id=? AND archived_at IS NULL`, item.Status, item.CurrentRevision, item.UpdatedBy, item.UpdatedAt, item.ID)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return apperrors.ErrNotFound
		}
		return nil
	})
	if err != nil {
		return domainmanifest.Package{}, fmt.Errorf("publish manifest revision: %w", err)
	}
	return r.Get(ctx, item.ID)
}

func (r *Repository) ListRevisions(ctx context.Context, packageID string) ([]domainmanifest.Revision, error) {
	rows, err := r.db.WithContext(ctx).Raw(`SELECT id, package_id, version, digest, note, files, bindings, created_by, created_at FROM manifest_revisions WHERE package_id=? ORDER BY version DESC`, packageID).Rows()
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	items := make([]domainmanifest.Revision, 0)
	for rows.Next() {
		var item domainmanifest.Revision
		var files, bindings []byte
		if err := rows.Scan(&item.ID, &item.PackageID, &item.Version, &item.Digest, &item.Note, &files, &bindings, &item.CreatedBy, &item.CreatedAt); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(files, &item.Files); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(bindings, &item.Bindings); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

type scanner interface{ Scan(...any) error }

func scanPackage(rows *sql.Rows) (domainmanifest.Package, error)  { return scan(rows) }
func scanPackageRow(row *sql.Row) (domainmanifest.Package, error) { return scan(row) }
func scan(source scanner) (domainmanifest.Package, error) {
	var item domainmanifest.Package
	var files, bindings []byte
	if err := source.Scan(&item.ID, &item.Name, &item.Description, &item.ApplicationID, &item.BusinessLineID, &item.Renderer, &item.Status, &item.CurrentRevision, &files, &bindings, &item.CreatedBy, &item.UpdatedBy, &item.CreatedAt, &item.UpdatedAt); err != nil {
		return domainmanifest.Package{}, err
	}
	if err := json.Unmarshal(files, &item.Files); err != nil {
		return domainmanifest.Package{}, fmt.Errorf("decode manifest files: %w", err)
	}
	if err := json.Unmarshal(bindings, &item.Bindings); err != nil {
		return domainmanifest.Package{}, fmt.Errorf("decode manifest bindings: %w", err)
	}
	return item, nil
}

func encodePackageJSON(item domainmanifest.Package) (string, string, error) {
	files, err := json.Marshal(item.Files)
	if err != nil {
		return "", "", err
	}
	bindings, err := json.Marshal(item.Bindings)
	if err != nil {
		return "", "", err
	}
	return string(files), string(bindings), nil
}
