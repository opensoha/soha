package resourcecreation

import (
	"context"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	domainresource "github.com/opensoha/soha/internal/domain/resource"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"gorm.io/gorm"
)

const maxErrorSummaryBytes = 2048

type Repository struct {
	db *gorm.DB
}

func New(db *gorm.DB) *Repository {
	return &Repository{db: db}
}

func (r *Repository) Claim(ctx context.Context, actorID, clusterID, idempotencyKey, contentHash string, documents []domainresource.ResourceCreateExecutionDocument) (domainresource.ResourceCreateBatchClaim, error) {
	actorID = strings.TrimSpace(actorID)
	clusterID = strings.TrimSpace(clusterID)
	idempotencyKey = strings.TrimSpace(idempotencyKey)
	contentHash = strings.TrimSpace(contentHash)
	if err := validateClaim(actorID, clusterID, idempotencyKey, contentHash, documents); err != nil {
		return domainresource.ResourceCreateBatchClaim{}, err
	}

	now := time.Now().UTC()
	batch := domainresource.ResourceCreateBatch{
		ID: uuid.NewString(), ActorID: actorID, ClusterID: clusterID,
		IdempotencyKey: idempotencyKey, ContentHash: contentHash,
		Status:    domainresource.ResourceCreateBatchRunning,
		Documents: normalizeInitialDocuments(documents), CreatedAt: now, UpdatedAt: now,
	}
	claim := domainresource.ResourceCreateBatchClaim{}
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		result := tx.Exec(`
			INSERT INTO platform_resource_creation_batches (
				id, actor_id, cluster_id, idempotency_key, content_hash, status, created_at, updated_at
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT (actor_id, cluster_id, idempotency_key) DO NOTHING
		`, batch.ID, batch.ActorID, batch.ClusterID, batch.IdempotencyKey, batch.ContentHash, batch.Status, batch.CreatedAt, batch.UpdatedAt)
		if result.Error != nil {
			return fmt.Errorf("claim resource creation batch: %w", result.Error)
		}
		if result.RowsAffected == 0 {
			existing, err := getByIdentity(ctx, tx, actorID, clusterID, idempotencyKey)
			if err != nil {
				return err
			}
			if existing.ContentHash != contentHash {
				return fmt.Errorf("%w: idempotency key is already bound to different content", apperrors.ErrConflict)
			}
			claim = domainresource.ResourceCreateBatchClaim{Batch: existing}
			return nil
		}
		for _, document := range batch.Documents {
			if err := insertDocument(tx, batch.ID, document, now); err != nil {
				return err
			}
		}
		claim = domainresource.ResourceCreateBatchClaim{Batch: batch, Created: true}
		return nil
	})
	if err != nil {
		return domainresource.ResourceCreateBatchClaim{}, err
	}
	return claim, nil
}

func (r *Repository) Get(ctx context.Context, batchID string) (domainresource.ResourceCreateBatch, error) {
	return getByID(ctx, r.db, strings.TrimSpace(batchID))
}

func (r *Repository) GetByIdentity(ctx context.Context, actorID, clusterID, idempotencyKey string) (domainresource.ResourceCreateBatch, error) {
	return getByIdentity(
		ctx,
		r.db,
		strings.TrimSpace(actorID),
		strings.TrimSpace(clusterID),
		strings.TrimSpace(idempotencyKey),
	)
}

func (r *Repository) UpdateDocument(ctx context.Context, batchID string, document domainresource.ResourceCreateExecutionDocument) error {
	batchID = strings.TrimSpace(batchID)
	if batchID == "" || (document.Status != "succeeded" && document.Status != "failed") {
		return fmt.Errorf("%w: batch id and a terminal document status are required", apperrors.ErrInvalidArgument)
	}
	document.Error = truncateUTF8(document.Error, maxErrorSummaryBytes)
	result := r.db.WithContext(ctx).Exec(`
		UPDATE platform_resource_creation_documents AS document
		SET api_version = ?, kind = ?, resource_name = ?, namespace = ?, namespaced = ?,
			status = ?, error_code = ?, error_summary = ?, updated_at = ?
		FROM platform_resource_creation_batches AS batch
		WHERE document.batch_id = batch.id AND batch.id = ? AND document.document_index = ?
			AND batch.status = 'running' AND document.status = 'not_started'
	`, document.Resource.APIVersion, document.Resource.Kind, document.Resource.Name,
		document.Resource.Namespace, document.Resource.Namespaced, document.Status,
		document.ErrorCode, document.Error, time.Now().UTC(), batchID, document.Index)
	if result.Error != nil {
		return fmt.Errorf("update resource creation document: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return r.documentUpdateMiss(ctx, batchID, document)
	}
	return nil
}

func (r *Repository) Complete(ctx context.Context, batchID string, status domainresource.ResourceCreateBatchStatus) (domainresource.ResourceCreateBatch, error) {
	batchID = strings.TrimSpace(batchID)
	if batchID == "" || !status.Terminal() {
		return domainresource.ResourceCreateBatch{}, fmt.Errorf("%w: batch id and terminal status are required", apperrors.ErrInvalidArgument)
	}
	var completed domainresource.ResourceCreateBatch
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		now := time.Now().UTC()
		result := tx.Exec(`
			UPDATE platform_resource_creation_batches
			SET status = ?, updated_at = ?, finished_at = ?
			WHERE id = ? AND status = 'running'
		`, status, now, now, batchID)
		if result.Error != nil {
			return fmt.Errorf("complete resource creation batch: %w", result.Error)
		}
		batch, err := getByID(ctx, tx, batchID)
		if err != nil {
			return err
		}
		if result.RowsAffected == 0 && batch.Status != status {
			return fmt.Errorf("%w: resource creation batch is already %s", apperrors.ErrConflict, batch.Status)
		}
		completed = batch
		return nil
	})
	if err != nil {
		return domainresource.ResourceCreateBatch{}, err
	}
	return completed, nil
}

func validateClaim(actorID, clusterID, idempotencyKey, contentHash string, documents []domainresource.ResourceCreateExecutionDocument) error {
	if actorID == "" || clusterID == "" || idempotencyKey == "" {
		return fmt.Errorf("%w: actor id, cluster id and idempotency key are required", apperrors.ErrInvalidArgument)
	}
	if len(contentHash) != 64 {
		return fmt.Errorf("%w: content hash must be a SHA-256 hex digest", apperrors.ErrInvalidArgument)
	}
	if _, err := hex.DecodeString(contentHash); err != nil {
		return fmt.Errorf("%w: content hash must be a SHA-256 hex digest", apperrors.ErrInvalidArgument)
	}
	if len(documents) == 0 || len(documents) > domainresource.ResourceCreateMaxDocuments {
		return fmt.Errorf("%w: document count must be between 1 and %d", apperrors.ErrInvalidArgument, domainresource.ResourceCreateMaxDocuments)
	}
	indexes := make(map[int]struct{}, len(documents))
	for _, document := range documents {
		if document.Index < 0 || strings.TrimSpace(document.Resource.APIVersion) == "" || strings.TrimSpace(document.Resource.Kind) == "" {
			return fmt.Errorf("%w: every document requires a non-negative index, apiVersion and kind", apperrors.ErrInvalidArgument)
		}
		if _, exists := indexes[document.Index]; exists {
			return fmt.Errorf("%w: duplicate document index %d", apperrors.ErrInvalidArgument, document.Index)
		}
		indexes[document.Index] = struct{}{}
	}
	return nil
}

func normalizeInitialDocuments(documents []domainresource.ResourceCreateExecutionDocument) []domainresource.ResourceCreateExecutionDocument {
	result := make([]domainresource.ResourceCreateExecutionDocument, len(documents))
	for index, document := range documents {
		document.Status = "not_started"
		document.ErrorCode = ""
		document.Error = ""
		result[index] = document
	}
	return result
}

func insertDocument(tx *gorm.DB, batchID string, document domainresource.ResourceCreateExecutionDocument, now time.Time) error {
	if err := tx.Exec(`
		INSERT INTO platform_resource_creation_documents (
			batch_id, document_index, api_version, kind, resource_name, namespace,
			namespaced, status, error_code, error_summary, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, '', '', ?, ?)
	`, batchID, document.Index, document.Resource.APIVersion, document.Resource.Kind,
		document.Resource.Name, document.Resource.Namespace, document.Resource.Namespaced,
		document.Status, now, now).Error; err != nil {
		return fmt.Errorf("create resource creation document %d: %w", document.Index, err)
	}
	return nil
}

func getByIdentity(ctx context.Context, db *gorm.DB, actorID, clusterID, idempotencyKey string) (domainresource.ResourceCreateBatch, error) {
	row := db.WithContext(ctx).Raw(`
		SELECT id, actor_id, cluster_id, idempotency_key, content_hash, status,
			created_at, updated_at, finished_at
		FROM platform_resource_creation_batches
		WHERE actor_id = ? AND cluster_id = ? AND idempotency_key = ?
	`, actorID, clusterID, idempotencyKey).Row()
	return scanBatchWithDocuments(ctx, db, row)
}

func getByID(ctx context.Context, db *gorm.DB, batchID string) (domainresource.ResourceCreateBatch, error) {
	row := db.WithContext(ctx).Raw(`
		SELECT id, actor_id, cluster_id, idempotency_key, content_hash, status,
			created_at, updated_at, finished_at
		FROM platform_resource_creation_batches
		WHERE id = ?
	`, batchID).Row()
	return scanBatchWithDocuments(ctx, db, row)
}

type rowScanner interface {
	Scan(...any) error
}

func scanBatchWithDocuments(ctx context.Context, db *gorm.DB, row rowScanner) (domainresource.ResourceCreateBatch, error) {
	var batch domainresource.ResourceCreateBatch
	var finishedAt sql.NullTime
	if err := row.Scan(&batch.ID, &batch.ActorID, &batch.ClusterID, &batch.IdempotencyKey,
		&batch.ContentHash, &batch.Status, &batch.CreatedAt, &batch.UpdatedAt, &finishedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domainresource.ResourceCreateBatch{}, fmt.Errorf("%w: resource creation batch not found", apperrors.ErrNotFound)
		}
		return domainresource.ResourceCreateBatch{}, fmt.Errorf("scan resource creation batch: %w", err)
	}
	if finishedAt.Valid {
		value := finishedAt.Time
		batch.FinishedAt = &value
	}
	rows, err := db.WithContext(ctx).Raw(`
		SELECT document_index, api_version, kind, resource_name, namespace, namespaced,
			status, error_code, error_summary
		FROM platform_resource_creation_documents
		WHERE batch_id = ?
		ORDER BY document_index ASC
	`, batch.ID).Rows()
	if err != nil {
		return domainresource.ResourceCreateBatch{}, fmt.Errorf("query resource creation documents: %w", err)
	}
	defer func() { _ = rows.Close() }()
	batch.Documents = make([]domainresource.ResourceCreateExecutionDocument, 0)
	for rows.Next() {
		var document domainresource.ResourceCreateExecutionDocument
		if err := rows.Scan(&document.Index, &document.Resource.APIVersion, &document.Resource.Kind,
			&document.Resource.Name, &document.Resource.Namespace, &document.Resource.Namespaced,
			&document.Status, &document.ErrorCode, &document.Error); err != nil {
			return domainresource.ResourceCreateBatch{}, fmt.Errorf("scan resource creation document: %w", err)
		}
		batch.Documents = append(batch.Documents, document)
	}
	if err := rows.Err(); err != nil {
		return domainresource.ResourceCreateBatch{}, fmt.Errorf("iterate resource creation documents: %w", err)
	}
	return batch, nil
}

func (r *Repository) documentUpdateMiss(ctx context.Context, batchID string, document domainresource.ResourceCreateExecutionDocument) error {
	batch, err := r.Get(ctx, batchID)
	if err != nil {
		return err
	}
	for _, existing := range batch.Documents {
		if existing.Index != document.Index {
			continue
		}
		if sameDocumentResult(existing, document) {
			return nil
		}
		return fmt.Errorf("%w: resource creation document %d is already %s", apperrors.ErrConflict, document.Index, existing.Status)
	}
	if batch.Status.Terminal() {
		return fmt.Errorf("%w: resource creation batch is already %s", apperrors.ErrConflict, batch.Status)
	}
	return fmt.Errorf("%w: resource creation document %d not found", apperrors.ErrNotFound, document.Index)
}

func sameDocumentResult(left, right domainresource.ResourceCreateExecutionDocument) bool {
	return left.Index == right.Index && left.Resource == right.Resource && left.Status == right.Status &&
		left.ErrorCode == right.ErrorCode && left.Error == right.Error
}

func truncateUTF8(value string, maxBytes int) string {
	if len(value) <= maxBytes {
		return value
	}
	value = value[:maxBytes]
	for !utf8.ValidString(value) {
		value = value[:len(value)-1]
	}
	return value
}
