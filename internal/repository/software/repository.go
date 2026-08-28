package software

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"hash"
	"io"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	appsoftware "github.com/opensoha/soha/internal/application/software"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"gorm.io/gorm"
)

const packageColumns = `id, tenant_id, workspace_id, software_id, name, description, publisher, category,
	version, platform, arch, file_name, storage_integration_id, object_key, size_bytes, sha256,
	visibility, status, download_count, created_at, updated_at`

type Store struct {
	db    *gorm.DB
	blobs appsoftware.BlobStore
	now   func() time.Time
}

func New(db *gorm.DB, blobs appsoftware.BlobStore) (*Store, error) {
	if db == nil || blobs == nil {
		return nil, fmt.Errorf("software package database and object storage are required")
	}
	return &Store{db: db, blobs: blobs, now: time.Now}, nil
}

func (s *Store) List(ctx context.Context, filter appsoftware.Filter) ([]appsoftware.Package, string, error) {
	return s.list(ctx, filter, true)
}

func (s *Store) Storage(ctx context.Context, storageIntegrationID, cursor string, limit int) (appsoftware.Storage, error) {
	filter := appsoftware.Filter{StorageIntegrationID: storageIntegrationID, Cursor: cursor, Limit: limit}
	where := `tenant_id='default' AND workspace_id='default' AND visibility='workspace' AND status='ready'`
	args := []any{}
	if storageIntegrationID != "" {
		where += ` AND storage_integration_id=?`
		args = append(args, storageIntegrationID)
	}
	var objectCount, totalBytes int64
	if err := s.db.WithContext(ctx).Raw(`SELECT COUNT(*), COALESCE(SUM(size_bytes), 0) FROM software_packages WHERE `+where, args...).Row().Scan(&objectCount, &totalBytes); err != nil {
		return appsoftware.Storage{}, fmt.Errorf("summarize software package storage: %w", err)
	}
	items, next, err := s.list(ctx, filter, true)
	if err != nil {
		return appsoftware.Storage{}, err
	}
	return appsoftware.Storage{
		Backend: "s3", IntegrationID: storageIntegrationID, ProviderType: "s3",
		ObjectCount: objectCount, TotalBytes: totalBytes, Items: items, NextCursor: next,
	}, nil
}

func (s *Store) Create(ctx context.Context, input appsoftware.UploadInput, content io.Reader) (appsoftware.Package, error) {
	backend, err := s.blobs.Active(ctx, input.StorageIntegrationID)
	if err != nil {
		return appsoftware.Package{}, err
	}
	id := uuid.NewString()
	objectKey := "software/packages/" + id
	size, digest, err := s.blobs.Put(ctx, backend.IntegrationID, objectKey, content)
	if err != nil {
		return appsoftware.Package{}, err
	}
	now := s.now().UTC()
	item := appsoftware.Package{
		ID: id, TenantID: input.TenantID, WorkspaceID: input.WorkspaceID, SoftwareID: input.SoftwareID,
		Name: input.Name, Description: input.Description, Publisher: input.Publisher, Category: input.Category,
		Version: input.Version, Platform: input.Platform, Arch: input.Arch, FileName: input.FileName,
		StorageIntegrationID: backend.IntegrationID, ObjectKey: objectKey, SizeBytes: size, SHA256: digest,
		Visibility: input.Visibility, Status: "ready", DownloadPath: downloadPath(id), CreatedAt: now, UpdatedAt: now,
	}
	err = s.db.WithContext(ctx).Exec(`
		INSERT INTO software_packages (
			id, tenant_id, workspace_id, software_id, name, description, publisher, category, version,
			platform, arch, file_name, storage_integration_id, object_key, size_bytes, sha256,
			visibility, status, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 'ready', ?, ?)
	`, item.ID, item.TenantID, item.WorkspaceID, item.SoftwareID, item.Name, item.Description, item.Publisher, item.Category,
		item.Version, item.Platform, item.Arch, item.FileName, item.StorageIntegrationID, item.ObjectKey,
		item.SizeBytes, item.SHA256, item.Visibility, item.CreatedAt, item.UpdatedAt).Error
	if err != nil {
		cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
		cleanupErr := s.blobs.Delete(cleanupCtx, backend.IntegrationID, objectKey)
		cancel()
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return appsoftware.Package{}, errors.Join(fmt.Errorf("%w: software package version already exists for platform and architecture", apperrors.ErrConflict), cleanupErr)
		}
		return appsoftware.Package{}, errors.Join(fmt.Errorf("create software package metadata: %w", err), cleanupErr)
	}
	return item, nil
}

func (s *Store) Open(ctx context.Context, id string) (appsoftware.Package, io.ReadCloser, error) {
	item, err := s.get(ctx, id)
	if err != nil {
		return appsoftware.Package{}, nil, err
	}
	if item.Status != "ready" {
		return appsoftware.Package{}, nil, fmt.Errorf("%w: software package %s", apperrors.ErrNotFound, id)
	}
	reader, err := s.blobs.Open(ctx, item.StorageIntegrationID, item.ObjectKey)
	if err != nil {
		return appsoftware.Package{}, nil, err
	}
	return item, newIntegrityReader(reader, item.SizeBytes, item.SHA256), nil
}

func (s *Store) IncrementDownloadCount(ctx context.Context, id string) error {
	result := s.db.WithContext(ctx).Exec(`UPDATE software_packages SET download_count=download_count+1 WHERE id=?`, strings.TrimSpace(id))
	if result.Error != nil {
		return fmt.Errorf("increment software package download count: %w", result.Error)
	}
	if result.RowsAffected != 1 {
		return fmt.Errorf("%w: software package %s", apperrors.ErrNotFound, id)
	}
	return nil
}

func (s *Store) Delete(ctx context.Context, id string) error {
	item, err := s.get(ctx, id)
	if err != nil {
		return err
	}
	if item.Status != "deleted" {
		result := s.db.WithContext(ctx).Exec(`UPDATE software_packages SET status='deleted', updated_at=? WHERE id=?`, s.now().UTC(), item.ID)
		if result.Error != nil {
			return fmt.Errorf("mark software package deleted: %w", result.Error)
		}
	}
	if err := s.blobs.Delete(ctx, item.StorageIntegrationID, item.ObjectKey); err != nil {
		return err
	}
	if err := s.db.WithContext(ctx).Exec(`DELETE FROM software_packages WHERE id=?`, item.ID).Error; err != nil {
		return fmt.Errorf("delete software package metadata: %w", err)
	}
	return nil
}

func (s *Store) list(ctx context.Context, filter appsoftware.Filter, readyOnly bool) ([]appsoftware.Package, string, error) {
	if filter.Limit <= 0 {
		filter.Limit = 50
	}
	query := `SELECT ` + packageColumns + ` FROM software_packages WHERE tenant_id='default' AND workspace_id='default' AND visibility='workspace'`
	args := make([]any, 0, 6)
	if readyOnly {
		query += ` AND status='ready'`
	}
	if filter.Platform != "" {
		query += ` AND platform=?`
		args = append(args, filter.Platform)
	}
	if filter.Arch != "" {
		query += ` AND arch=?`
		args = append(args, filter.Arch)
	}
	if filter.StorageIntegrationID != "" {
		query += ` AND storage_integration_id=?`
		args = append(args, filter.StorageIntegrationID)
	}
	if filter.Cursor != "" {
		var createdAt time.Time
		cursorQuery := `SELECT created_at FROM software_packages WHERE id=? AND tenant_id='default' AND workspace_id='default' AND visibility='workspace'`
		cursorArgs := []any{filter.Cursor}
		if filter.StorageIntegrationID != "" {
			cursorQuery += ` AND storage_integration_id=?`
			cursorArgs = append(cursorArgs, filter.StorageIntegrationID)
		}
		if err := s.db.WithContext(ctx).Raw(cursorQuery, cursorArgs...).Row().Scan(&createdAt); errors.Is(err, sql.ErrNoRows) {
			return nil, "", fmt.Errorf("%w: invalid software package cursor", apperrors.ErrInvalidArgument)
		} else if err != nil {
			return nil, "", fmt.Errorf("resolve software package cursor: %w", err)
		}
		query += ` AND (created_at < ? OR (created_at = ? AND id < ?))`
		args = append(args, createdAt, createdAt, filter.Cursor)
	}
	query += ` ORDER BY created_at DESC, id DESC LIMIT ?`
	args = append(args, filter.Limit+1)
	rows, err := s.db.WithContext(ctx).Raw(query, args...).Rows()
	if err != nil {
		return nil, "", fmt.Errorf("list software packages: %w", err)
	}
	defer rows.Close()
	items := make([]appsoftware.Package, 0, filter.Limit+1)
	for rows.Next() {
		item, err := scanPackage(rows)
		if err != nil {
			return nil, "", err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, "", err
	}
	next := ""
	if len(items) > filter.Limit {
		next = items[filter.Limit-1].ID
		items = items[:filter.Limit]
	}
	return items, next, nil
}

func (s *Store) get(ctx context.Context, id string) (appsoftware.Package, error) {
	row := s.db.WithContext(ctx).Raw(`SELECT `+packageColumns+` FROM software_packages WHERE id=? AND tenant_id='default' AND workspace_id='default' AND visibility='workspace'`, strings.TrimSpace(id)).Row()
	item, err := scanPackage(row)
	if errors.Is(err, sql.ErrNoRows) {
		return appsoftware.Package{}, fmt.Errorf("%w: software package %s", apperrors.ErrNotFound, id)
	}
	if err != nil {
		return appsoftware.Package{}, fmt.Errorf("get software package: %w", err)
	}
	return item, nil
}

type rowScanner interface{ Scan(...any) error }

func scanPackage(row rowScanner) (appsoftware.Package, error) {
	var item appsoftware.Package
	err := row.Scan(&item.ID, &item.TenantID, &item.WorkspaceID, &item.SoftwareID, &item.Name, &item.Description,
		&item.Publisher, &item.Category, &item.Version, &item.Platform, &item.Arch, &item.FileName,
		&item.StorageIntegrationID, &item.ObjectKey, &item.SizeBytes, &item.SHA256, &item.Visibility, &item.Status,
		&item.DownloadCount, &item.CreatedAt, &item.UpdatedAt)
	item.DownloadPath = downloadPath(item.ID)
	return item, err
}

func downloadPath(id string) string { return "/api/v1/software/packages/" + id + "/download" }

type integrityReader struct {
	io.ReadCloser
	hash         hash.Hash
	expectedHash []byte
	expectedSize int64
	read         int64
}

func newIntegrityReader(reader io.ReadCloser, size int64, digest string) io.ReadCloser {
	expected, _ := hex.DecodeString(digest)
	return &integrityReader{ReadCloser: reader, hash: sha256.New(), expectedHash: expected, expectedSize: size}
}

func (r *integrityReader) Read(buffer []byte) (int, error) {
	n, err := r.ReadCloser.Read(buffer)
	if n > 0 {
		_, _ = r.hash.Write(buffer[:n])
		r.read += int64(n)
		if r.read > r.expectedSize {
			return 0, fmt.Errorf("software package integrity check failed")
		}
	}
	if errors.Is(err, io.EOF) && (r.read != r.expectedSize || subtle.ConstantTimeCompare(r.hash.Sum(nil), r.expectedHash) != 1) {
		return 0, fmt.Errorf("software package integrity check failed")
	}
	return n, err
}
