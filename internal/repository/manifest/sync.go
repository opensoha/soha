package manifest

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	domaindelivery "github.com/opensoha/soha/internal/domain/delivery"
	domainmanifest "github.com/opensoha/soha/internal/domain/manifest"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"gorm.io/gorm"
)

const sourceSelectQuery = `
	SELECT source.id, source.package_id, source.mode, COALESCE(source.repository_id, ''),
		COALESCE(source.ref_type, ''), source.ref_value, source.source_path,
		source.include_patterns, source.exclude_patterns, source.sync_policy,
		COALESCE(source.poll_interval_seconds, 0), source.auto_publish, source.auto_deploy,
		COALESCE(status.last_resolved_commit, ''), COALESCE(status.last_tree_digest, ''),
		COALESCE(status.last_canonical_digest, ''), status.last_successful_sync_at,
		COALESCE(status.last_error_code, ''), COALESCE(status.last_error_message, ''),
		source.generation, source.created_at, source.updated_at
	FROM manifest_sources source
	LEFT JOIN manifest_source_status status ON status.source_id = source.id
`

func (r *Repository) GetSourceByID(ctx context.Context, sourceID string) (domainmanifest.Source, error) {
	item, err := scanSource(r.db.WithContext(ctx).Raw(sourceSelectQuery+` WHERE source.id = ? LIMIT 1`, strings.TrimSpace(sourceID)).Row())
	if errors.Is(err, sql.ErrNoRows) {
		return domainmanifest.Source{}, apperrors.ErrNotFound
	}
	return item, err
}

func (r *Repository) CreateSyncTask(ctx context.Context, run domainmanifest.SyncRun, task domaindelivery.ExecutionTask) (string, bool, error) {
	payload, err := json.Marshal(task.Payload)
	if err != nil {
		return "", false, err
	}
	result, err := json.Marshal(task.Result)
	if err != nil {
		return "", false, err
	}
	created := false
	err = r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec(`
			INSERT INTO execution_tasks (
				id, release_bundle_id, application_id, application_environment_id,
				task_kind, provider_kind, target_kind, status, queue_key, lock_key,
				max_retries, attempt_count, timeout_seconds, callback_token,
				payload, result, created_at, updated_at
			) VALUES (?, NULL, ?, NULL, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?::jsonb, ?::jsonb, ?, ?)
		`, task.ID, task.ApplicationID, task.TaskKind, task.ProviderKind, task.TargetKind,
			task.Status, task.QueueKey, task.LockKey, task.MaxRetries, task.AttemptCount,
			task.TimeoutSeconds, task.CallbackToken, string(payload), string(result),
			task.CreatedAt, task.UpdatedAt).Error; err != nil {
			return err
		}
		files, _ := json.Marshal(run.Files)
		insert := tx.Exec(`
			INSERT INTO manifest_sync_runs (
				id, source_id, package_id, execution_task_id, source_generation,
				trigger, status, idempotency_key, requested_commit, files, actor,
				created_at, updated_at
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, NULLIF(?, ''), ?::jsonb, NULLIF(?, ''), ?, ?)
			ON CONFLICT (idempotency_key) DO NOTHING
		`, run.ID, run.SourceID, run.PackageID, task.ID, run.SourceGeneration,
			run.Trigger, run.Status, run.IdempotencyKey, run.RequestedCommit,
			string(files), run.Actor, run.CreatedAt, run.UpdatedAt)
		if insert.Error != nil {
			return insert.Error
		}
		if insert.RowsAffected == 0 {
			return tx.Exec(`DELETE FROM execution_tasks WHERE id = ?`, task.ID).Error
		}
		created = true
		return nil
	})
	if err != nil {
		return "", false, fmt.Errorf("create manifest sync task: %w", err)
	}
	if created {
		return task.ID, true, nil
	}
	var existing string
	if err := r.db.WithContext(ctx).Raw(`SELECT execution_task_id FROM manifest_sync_runs WHERE idempotency_key = ?`, run.IdempotencyKey).Row().Scan(&existing); err != nil {
		return "", false, err
	}
	return existing, false, nil
}

func (r *Repository) ListSyncRuns(ctx context.Context, packageID string, limit int) ([]domainmanifest.SyncRun, error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	rows, err := r.db.WithContext(ctx).Raw(`
		SELECT id, source_id, package_id, execution_task_id, source_generation,
			trigger, status, idempotency_key, requested_commit, resolved_commit,
			tree_digest, canonical_digest, files, revision, error_code, error_message,
			actor, started_at, finished_at, created_at, updated_at
		FROM manifest_sync_runs WHERE package_id = ? ORDER BY created_at DESC LIMIT ?
	`, strings.TrimSpace(packageID), limit).Rows()
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]domainmanifest.SyncRun, 0)
	for rows.Next() {
		var item domainmanifest.SyncRun
		var requested, resolved, tree, canonical, errorCode, errorMessage, actor sql.NullString
		var revision sql.NullInt64
		var files []byte
		if err := rows.Scan(&item.ID, &item.SourceID, &item.PackageID, &item.ExecutionTaskID,
			&item.SourceGeneration, &item.Trigger, &item.Status, &item.IdempotencyKey,
			&requested, &resolved, &tree, &canonical, &files, &revision, &errorCode,
			&errorMessage, &actor, &item.StartedAt, &item.FinishedAt, &item.CreatedAt, &item.UpdatedAt); err != nil {
			return nil, err
		}
		item.RequestedCommit, item.ResolvedCommit = requested.String, resolved.String
		item.TreeDigest, item.CanonicalDigest = tree.String, canonical.String
		item.Revision, item.ErrorCode, item.ErrorMessage, item.Actor = int(revision.Int64), errorCode.String, errorMessage.String, actor.String
		_ = json.Unmarshal(files, &item.Files)
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *Repository) ListDueSources(ctx context.Context, limit int) ([]domainmanifest.Source, error) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	rows, err := r.db.WithContext(ctx).Raw(sourceSelectQuery+`
		WHERE source.mode = 'git_synced' AND source.sync_policy = 'poll'
			AND (status.last_successful_sync_at IS NULL OR status.last_successful_sync_at + make_interval(secs => source.poll_interval_seconds) <= NOW())
			AND NOT EXISTS (
				SELECT 1 FROM manifest_sync_runs run
				WHERE run.source_id = source.id AND run.status IN ('queued', 'running')
			)
		ORDER BY COALESCE(status.last_successful_sync_at, source.created_at) ASC LIMIT ?
	`, limit).Rows()
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]domainmanifest.Source, 0)
	for rows.Next() {
		item, err := scanSource(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *Repository) UpdateSyncTask(ctx context.Context, task domaindelivery.ExecutionTask, payload domainmanifest.TaskPayload, result domainmanifest.TaskResult) error {
	now := time.Now().UTC()
	if task.Status == "dispatching" || task.Status == "running" {
		return r.db.WithContext(ctx).Exec(`UPDATE manifest_sync_runs SET status='running', started_at=COALESCE(started_at, ?), updated_at=? WHERE execution_task_id=? AND source_generation=?`, now, now, task.ID, payload.Generation).Error
	}
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var sourceID, packageID, actor string
		var sourceGeneration, currentSourceGeneration int64
		var autoPublish bool
		var previousCanonical string
		if err := tx.Raw(`
			SELECT run.source_id, run.package_id, run.source_generation, source.generation, COALESCE(run.actor, ''),
				source.auto_publish, COALESCE(status.last_canonical_digest, '')
			FROM manifest_sync_runs run
			JOIN manifest_sources source ON source.id = run.source_id
			JOIN manifest_source_status status ON status.source_id = source.id
			WHERE run.execution_task_id = ? FOR UPDATE
		`, task.ID).Row().Scan(&sourceID, &packageID, &sourceGeneration, &currentSourceGeneration, &actor, &autoPublish, &previousCanonical); err != nil {
			return err
		}
		if sourceGeneration != payload.Generation || currentSourceGeneration != payload.Generation {
			return tx.Exec(`
				UPDATE manifest_sync_runs SET status='ignored', error_code='stale_source_generation',
					error_message='source configuration changed before task completion',
					finished_at=?, updated_at=? WHERE execution_task_id=?
			`, now, now, task.ID).Error
		}
		if task.Status != "completed" {
			message := syncTaskError(task.Result)
			if err := tx.Exec(`UPDATE manifest_sync_runs SET status='failed', error_code='git_sync_failed', error_message=?, finished_at=?, updated_at=? WHERE execution_task_id=?`, message, now, now, task.ID).Error; err != nil {
				return err
			}
			return tx.Exec(`UPDATE manifest_source_status SET last_error_code='git_sync_failed', last_error_message=?, updated_at=? WHERE source_id=?`, message, now, sourceID).Error
		}
		filesJSON, err := json.Marshal(result.SyncedFiles)
		if err != nil {
			return err
		}
		paths := make([]string, 0, len(result.SyncedFiles))
		for _, file := range result.SyncedFiles {
			paths = append(paths, file.Path)
		}
		pathsJSON, _ := json.Marshal(paths)
		if previousCanonical != "" && previousCanonical == result.CanonicalDigest {
			if err := tx.Exec(`
				UPDATE manifest_source_status SET last_resolved_commit=?, last_tree_digest=?,
					last_successful_sync_at=?, last_error_code='', last_error_message='', updated_at=?
				WHERE source_id=?
			`, result.ResolvedCommit, result.TreeDigest, now, now, sourceID).Error; err != nil {
				return err
			}
			return tx.Exec(`
				UPDATE manifest_sync_runs SET status='ignored', resolved_commit=?, tree_digest=?,
					canonical_digest=?, files=?::jsonb, finished_at=?, updated_at=?
				WHERE execution_task_id=?
			`, result.ResolvedCommit, result.TreeDigest, result.CanonicalDigest, string(pathsJSON), now, now, task.ID).Error
		}
		updatedBy := actor
		if strings.TrimSpace(updatedBy) == "" {
			updatedBy = "system:manifest-sync"
		}
		if err := tx.Exec(`UPDATE manifest_packages SET files=?::jsonb, status='draft', updated_by=?, updated_at=? WHERE id=? AND archived_at IS NULL`, string(filesJSON), updatedBy, now, packageID).Error; err != nil {
			return err
		}
		revision := 0
		if autoPublish {
			var currentRevision int
			var bindings []byte
			if err := tx.Raw(`SELECT current_revision, bindings FROM manifest_packages WHERE id=? FOR UPDATE`, packageID).Row().Scan(&currentRevision, &bindings); err != nil {
				return err
			}
			revision = currentRevision + 1
			var bindingValue any
			_ = json.Unmarshal(bindings, &bindingValue)
			digestInput, _ := json.Marshal(struct {
				Files    []domainmanifest.File `json:"files"`
				Bindings any                   `json:"bindings"`
			}{Files: result.SyncedFiles, Bindings: bindingValue})
			digest := sha256.Sum256(digestInput)
			if err := tx.Exec(`INSERT INTO manifest_revisions (id, package_id, version, digest, note, files, bindings, created_by, created_at) VALUES (?, ?, ?, ?, ?, ?::jsonb, ?::jsonb, ?, ?)`, uuid.NewString(), packageID, revision, hex.EncodeToString(digest[:]), "Published by Git synchronization", string(filesJSON), string(bindings), updatedBy, now).Error; err != nil {
				return err
			}
			if err := tx.Exec(`UPDATE manifest_packages SET status='published', current_revision=? WHERE id=?`, revision, packageID).Error; err != nil {
				return err
			}
		}
		if err := tx.Exec(`
			UPDATE manifest_source_status SET last_resolved_commit=?, last_tree_digest=?,
				last_canonical_digest=?, last_successful_sync_at=?, last_error_code='',
				last_error_message='', updated_at=? WHERE source_id=?
		`, result.ResolvedCommit, result.TreeDigest, result.CanonicalDigest, now, now, sourceID).Error; err != nil {
			return err
		}
		return tx.Exec(`
			UPDATE manifest_sync_runs SET status='succeeded', resolved_commit=?, tree_digest=?,
				canonical_digest=?, files=?::jsonb, revision=NULLIF(?, 0), finished_at=?, updated_at=?
			WHERE execution_task_id=?
		`, result.ResolvedCommit, result.TreeDigest, result.CanonicalDigest, string(pathsJSON), revision, now, now, task.ID).Error
	})
}

func syncTaskError(result map[string]any) string {
	message := strings.TrimSpace(fmt.Sprint(result["error"]))
	if message == "" {
		message = "manifest Git synchronization failed"
	}
	if len(message) > 500 {
		message = message[:500]
	}
	return message
}
