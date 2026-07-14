package knowledge

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	domainknowledge "github.com/opensoha/soha/internal/domain/knowledge"
	"gorm.io/gorm"
)

func (r *Repository) ListConnectors(
	ctx context.Context,
	principal domainknowledge.PrincipalScope,
	limit int,
) ([]domainknowledge.ConnectorDefinition, error) {
	baseClause, args := accessClause("b", principal)
	rows, err := r.db.WithContext(ctx).Raw(`
		SELECT s.id,s.knowledge_base_id,s.name,s.kind,s.config_ref,s.config,s.sync_policy,
		       s.status,s.created_at,s.updated_at
		FROM ai_knowledge_sources s
		JOIN ai_knowledge_bases b ON b.id=s.knowledge_base_id
		WHERE s.kind IN ('http','git','object') AND b.deleted_at IS NULL AND (`+baseClause+`)
		ORDER BY s.updated_at DESC,s.id ASC LIMIT ?`, append(args, limit)...).Rows()
	if err != nil {
		return nil, fmt.Errorf("list knowledge connectors: %w", err)
	}
	defer func() { _ = rows.Close() }()
	items := []domainknowledge.ConnectorDefinition{}
	for rows.Next() {
		var item domainknowledge.ConnectorDefinition
		var config, policy []byte
		if err := rows.Scan(
			&item.ID,
			&item.KnowledgeBaseID,
			&item.Name,
			&item.Kind,
			&item.SecretRef,
			&config,
			&policy,
			&item.Status,
			&item.CreatedAt,
			&item.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan knowledge connector: %w", err)
		}
		item.Version = "v1"
		item.Config = map[string]any{}
		if err := json.Unmarshal(config, &item.Config); err != nil {
			return nil, fmt.Errorf("decode knowledge connector config: %w", err)
		}
		if err := json.Unmarshal(policy, &item.SyncPolicy); err != nil {
			return nil, fmt.Errorf("decode knowledge connector sync policy: %w", err)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate knowledge connectors: %w", err)
	}
	return items, nil
}

func (r *Repository) GetConnector(
	ctx context.Context,
	principal domainknowledge.PrincipalScope,
	connectorID string,
) (domainknowledge.ConnectorDefinition, error) {
	baseClause, args := accessClause("b", principal)
	queryArgs := append([]any{connectorID}, args...)
	var item domainknowledge.ConnectorDefinition
	var config, policy []byte
	err := r.db.WithContext(ctx).Raw(`
		SELECT s.id,s.knowledge_base_id,s.name,s.kind,s.config_ref,s.config,s.sync_policy,
		       s.status,s.created_at,s.updated_at
		FROM ai_knowledge_sources s
		JOIN ai_knowledge_bases b ON b.id=s.knowledge_base_id
		WHERE s.id=? AND s.kind IN ('http','git','object') AND b.deleted_at IS NULL AND (`+baseClause+`)`,
		queryArgs...,
	).Row().Scan(
		&item.ID,
		&item.KnowledgeBaseID,
		&item.Name,
		&item.Kind,
		&item.SecretRef,
		&config,
		&policy,
		&item.Status,
		&item.CreatedAt,
		&item.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return domainknowledge.ConnectorDefinition{}, domainknowledge.ErrSourceNotFound
	}
	if err != nil {
		return domainknowledge.ConnectorDefinition{}, fmt.Errorf("read knowledge connector: %w", err)
	}
	item.Version = "v1"
	item.Config = map[string]any{}
	if err := json.Unmarshal(config, &item.Config); err != nil {
		return domainknowledge.ConnectorDefinition{}, fmt.Errorf("decode knowledge connector config: %w", err)
	}
	if err := json.Unmarshal(policy, &item.SyncPolicy); err != nil {
		return domainknowledge.ConnectorDefinition{}, fmt.Errorf("decode knowledge connector policy: %w", err)
	}
	return item, nil
}

func (r *Repository) CreateIngestionJob(ctx context.Context, job domainknowledge.IngestionJob) error {
	checkpoint, principal, err := encodeIngestionState(job)
	if err != nil {
		return err
	}
	result := r.db.WithContext(ctx).Exec(`
		INSERT INTO ai_knowledge_ingestion_jobs(
			id,knowledge_base_id,source_id,target_revision,stage,status,attempt,max_attempts,
			cancel_requested,checkpoint,principal_snapshot,error_code,error,next_attempt_at,
			lease_token,lease_expires_at,created_at,updated_at,completed_at
		) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		job.ID,
		job.KnowledgeBaseID,
		job.SourceID,
		job.TargetRevision,
		job.Stage,
		job.Status,
		job.Attempt,
		job.MaxAttempts,
		job.CancelRequested,
		checkpoint,
		principal,
		job.ErrorCode,
		job.Error,
		job.NextAttemptAt,
		job.LeaseToken,
		job.LeaseExpiresAt,
		job.CreatedAt,
		job.UpdatedAt,
		job.CompletedAt,
	)
	if result.Error != nil {
		return fmt.Errorf("create knowledge ingestion job: %w", result.Error)
	}
	return nil
}

func (r *Repository) GetIngestionJob(
	ctx context.Context,
	principal domainknowledge.PrincipalScope,
	jobID string,
) (domainknowledge.IngestionJob, error) {
	baseClause, args := accessClause("b", principal)
	queryArgs := append([]any{jobID}, args...)
	job, err := scanIngestionJob(r.db.WithContext(ctx).Raw(`
		SELECT j.id,j.knowledge_base_id,j.source_id,j.target_revision,j.stage,j.status,
		       j.attempt,j.max_attempts,j.cancel_requested,j.checkpoint,j.principal_snapshot,
		       j.error_code,j.error,j.next_attempt_at,j.lease_token,j.lease_expires_at,
		       j.created_at,j.updated_at,j.completed_at
		FROM ai_knowledge_ingestion_jobs j
		JOIN ai_knowledge_bases b ON b.id=j.knowledge_base_id
		WHERE j.id=? AND b.deleted_at IS NULL AND (`+baseClause+`)`, queryArgs...).Row())
	return mapIngestionScanError(job, err)
}

func (r *Repository) GetIngestionJobInternal(ctx context.Context, jobID string) (domainknowledge.IngestionJob, error) {
	job, err := scanIngestionJob(r.db.WithContext(ctx).Raw(`
		SELECT id,knowledge_base_id,source_id,target_revision,stage,status,attempt,max_attempts,
		       cancel_requested,checkpoint,principal_snapshot,error_code,error,next_attempt_at,
		       lease_token,lease_expires_at,
		       created_at,updated_at,completed_at
		FROM ai_knowledge_ingestion_jobs WHERE id=?`, jobID).Row())
	return mapIngestionScanError(job, err)
}

func (r *Repository) ClaimIngestionJob(
	ctx context.Context,
	now time.Time,
	leaseToken string,
	leaseExpiresAt time.Time,
) (*domainknowledge.IngestionJob, error) {
	var claimed *domainknowledge.IngestionJob
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		job, err := scanIngestionJob(tx.Raw(`
			SELECT id,knowledge_base_id,source_id,target_revision,stage,status,attempt,max_attempts,
			       cancel_requested,checkpoint,principal_snapshot,error_code,error,next_attempt_at,
			       lease_token,lease_expires_at,
			       created_at,updated_at,completed_at
			FROM ai_knowledge_ingestion_jobs
			WHERE attempt < max_attempts AND (
				status='queued' OR (status='retry_wait' AND next_attempt_at<=?)
				OR (status='running' AND lease_expires_at<=?)
			)
			ORDER BY created_at ASC,id ASC FOR UPDATE SKIP LOCKED LIMIT 1`, now, now).Row())
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("select knowledge ingestion job: %w", err)
		}
		result := tx.Exec(`
			UPDATE ai_knowledge_ingestion_jobs
			SET status='running',attempt=attempt+1,next_attempt_at=NULL,
			    lease_token=?,lease_expires_at=?,updated_at=?
			WHERE id=? AND status=? AND stage=?`, leaseToken, leaseExpiresAt, now, job.ID, job.Status, job.Stage)
		if result.Error != nil {
			return fmt.Errorf("claim knowledge ingestion job: %w", result.Error)
		}
		if result.RowsAffected != 1 {
			return fmt.Errorf("%w: ingestion claim lost", domainknowledge.ErrIngestionConflict)
		}
		if err := tx.Exec(`DELETE FROM ai_knowledge_ingestion_document_staging WHERE job_id=?`, job.ID).Error; err != nil {
			return fmt.Errorf("clear stale knowledge ingestion staging: %w", err)
		}
		job.Status = domainknowledge.IngestionJobRunning
		job.Attempt++
		job.NextAttemptAt = nil
		job.LeaseToken = leaseToken
		job.LeaseExpiresAt = &leaseExpiresAt
		job.UpdatedAt = now
		claimed = &job
		return nil
	})
	if err != nil {
		return nil, err
	}
	return claimed, nil
}

func (r *Repository) StageIngestionDocument(
	ctx context.Context,
	jobID string,
	leaseToken string,
	document domainknowledge.Document,
	chunks []domainknowledge.Chunk,
) error {
	documentPayload, err := json.Marshal(document)
	if err != nil {
		return fmt.Errorf("encode staged knowledge document: %w", err)
	}
	chunksPayload, err := json.Marshal(chunks)
	if err != nil {
		return fmt.Errorf("encode staged knowledge chunks: %w", err)
	}
	result := r.db.WithContext(ctx).Exec(`
		INSERT INTO ai_knowledge_ingestion_document_staging(
			job_id,lease_token,document_id,document_payload,chunks_payload
		)
		SELECT ?,?,?,?,?
		WHERE EXISTS (
			SELECT 1 FROM ai_knowledge_ingestion_jobs
			WHERE id=? AND status='running' AND lease_token=? AND cancel_requested=false
		)
		ON CONFLICT(job_id,document_id) DO UPDATE SET
			lease_token=EXCLUDED.lease_token,
			document_payload=EXCLUDED.document_payload,
			chunks_payload=EXCLUDED.chunks_payload`,
		jobID, leaseToken, document.ID, documentPayload, chunksPayload, jobID, leaseToken,
	)
	if result.Error != nil {
		return fmt.Errorf("stage knowledge document: %w", result.Error)
	}
	if result.RowsAffected != 1 {
		return fmt.Errorf("%w: stale or cancelled ingestion staging", domainknowledge.ErrIngestionConflict)
	}
	return nil
}

func (r *Repository) AdvanceIngestionJob(
	ctx context.Context,
	job domainknowledge.IngestionJob,
	expectedStatus domainknowledge.IngestionJobStatus,
	expectedStage domainknowledge.IngestionStage,
) error {
	checkpoint, _, err := encodeIngestionState(job)
	if err != nil {
		return err
	}
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		result := tx.Exec(`
			UPDATE ai_knowledge_ingestion_jobs
			SET stage=?,status=?,attempt=?,cancel_requested=?,checkpoint=?,error_code=?,error=?,
			    next_attempt_at=?,lease_expires_at=?,updated_at=?,completed_at=?
			WHERE id=? AND status=? AND stage=? AND lease_token=?`,
			job.Stage,
			job.Status,
			job.Attempt,
			job.CancelRequested,
			checkpoint,
			job.ErrorCode,
			job.Error,
			job.NextAttemptAt,
			job.LeaseExpiresAt,
			job.UpdatedAt,
			job.CompletedAt,
			job.ID,
			expectedStatus,
			expectedStage,
			job.LeaseToken,
		)
		if result.Error != nil {
			return fmt.Errorf("advance knowledge ingestion job: %w", result.Error)
		}
		if result.RowsAffected != 1 {
			return fmt.Errorf("%w: stale ingestion stage callback", domainknowledge.ErrIngestionConflict)
		}
		return insertIngestionStage(tx, job)
	})
}

func (r *Repository) RequestIngestionCancel(
	ctx context.Context,
	principal domainknowledge.PrincipalScope,
	jobID string,
	now time.Time,
) (domainknowledge.IngestionJob, error) {
	job, err := r.GetIngestionJob(ctx, principal, jobID)
	if err != nil {
		return domainknowledge.IngestionJob{}, err
	}
	if job.Terminal() {
		return domainknowledge.IngestionJob{}, fmt.Errorf("%w: ingestion job is terminal", domainknowledge.ErrIngestionConflict)
	}
	expectedStatus, expectedStage := job.Status, job.Stage
	job.CancelRequested = true
	job.UpdatedAt = now
	switch job.Status {
	case domainknowledge.IngestionJobQueued, domainknowledge.IngestionJobRetryWait:
		job.Status = domainknowledge.IngestionJobCancelled
		job.CompletedAt = &now
	case domainknowledge.IngestionJobRunning:
		job.Status = domainknowledge.IngestionJobCancelling
	case domainknowledge.IngestionJobCancelling:
		return job, nil
	default:
		return domainknowledge.IngestionJob{}, fmt.Errorf("%w: ingestion job cannot be cancelled", domainknowledge.ErrIngestionConflict)
	}
	if err := r.AdvanceIngestionJob(ctx, job, expectedStatus, expectedStage); err != nil {
		return domainknowledge.IngestionJob{}, err
	}
	return job, nil
}

func (r *Repository) RetryIngestionJob(
	ctx context.Context,
	principal domainknowledge.PrincipalScope,
	jobID string,
	now time.Time,
) (domainknowledge.IngestionJob, error) {
	job, err := r.GetIngestionJob(ctx, principal, jobID)
	if err != nil {
		return domainknowledge.IngestionJob{}, err
	}
	if job.Status != domainknowledge.IngestionJobFailed && job.Status != domainknowledge.IngestionJobCancelled {
		return domainknowledge.IngestionJob{}, fmt.Errorf("%w: only failed or cancelled jobs can be retried", domainknowledge.ErrIngestionConflict)
	}
	expectedStatus, expectedStage := job.Status, job.Stage
	job.Status = domainknowledge.IngestionJobQueued
	job.Stage = domainknowledge.IngestionStageDiscovering
	job.Attempt = 0
	job.CancelRequested = false
	job.ErrorCode = ""
	job.Error = ""
	job.NextAttemptAt = nil
	job.CompletedAt = nil
	job.UpdatedAt = now
	job.Checkpoint = domainknowledge.IngestionCheckpoint{Stage: job.Stage, RecordedAt: now}
	if err := r.AdvanceIngestionJob(ctx, job, expectedStatus, expectedStage); err != nil {
		return domainknowledge.IngestionJob{}, err
	}
	return job, nil
}

func (r *Repository) GetSourceInternal(
	ctx context.Context,
	baseID string,
	sourceID string,
) (domainknowledge.Source, error) {
	item, err := scanSource(r.db.WithContext(ctx).Raw(`
		SELECT id,knowledge_base_id,name,kind,config_ref,config,sync_policy,cursor,status,last_error,
		       last_synced_at,created_at,updated_at
		FROM ai_knowledge_sources WHERE id=? AND knowledge_base_id=?`, sourceID, baseID).Row())
	if errors.Is(err, sql.ErrNoRows) {
		return domainknowledge.Source{}, domainknowledge.ErrSourceNotFound
	}
	return item, err
}

func (r *Repository) PublishIngestionJob(
	ctx context.Context,
	job domainknowledge.IngestionJob,
	source domainknowledge.Source,
	revision domainknowledge.IndexRevision,
) error {
	checkpoint, _, err := encodeIngestionState(job)
	if err != nil {
		return err
	}
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		staged, err := loadStagedIngestionDocuments(tx, job.ID, job.LeaseToken)
		if err != nil {
			return err
		}
		if len(staged) != revision.DocumentCount {
			return fmt.Errorf("%w: staged document count mismatch", domainknowledge.ErrIngestionConflict)
		}
		for _, item := range staged {
			if err := upsertPublishedDocument(tx, item.document, item.chunks, job.UpdatedAt); err != nil {
				return err
			}
		}
		if err := tx.Exec(`
			UPDATE ai_knowledge_index_revisions SET status='superseded'
			WHERE knowledge_base_id=? AND status='active'`, job.KnowledgeBaseID).Error; err != nil {
			return fmt.Errorf("supersede knowledge revision: %w", err)
		}
		if err := tx.Exec(`
			INSERT INTO ai_knowledge_index_revisions(
				id,knowledge_base_id,revision,embedding_model,chunker_version,document_count,
				chunk_count,status,created_at,activated_at
			) VALUES(?,?,?,?,?,?,?,?,?,?)`,
			revision.ID,
			revision.KnowledgeBaseID,
			revision.Revision,
			revision.EmbeddingModel,
			revision.ChunkerVersion,
			revision.DocumentCount,
			revision.ChunkCount,
			revision.Status,
			revision.CreatedAt,
			revision.ActivatedAt,
		).Error; err != nil {
			return fmt.Errorf("create knowledge revision: %w", err)
		}
		if err := tx.Exec(`
			UPDATE ai_knowledge_sources
			SET cursor=?,status='ready',last_error='',last_synced_at=?,updated_at=? WHERE id=?`,
			source.Cursor,
			source.LastSyncedAt,
			source.UpdatedAt,
			source.ID,
		).Error; err != nil {
			return fmt.Errorf("publish knowledge source cursor: %w", err)
		}
		result := tx.Exec(`
			UPDATE ai_knowledge_ingestion_jobs
			SET status='succeeded',stage='publishing',checkpoint=?,error_code='',error='',
			    updated_at=?,completed_at=?
			WHERE id=? AND status='running' AND stage='publishing'
			  AND cancel_requested=false AND lease_token=?`,
			checkpoint,
			job.UpdatedAt,
			job.CompletedAt,
			job.ID,
			job.LeaseToken,
		)
		if result.Error != nil {
			return fmt.Errorf("complete knowledge ingestion publish: %w", result.Error)
		}
		if result.RowsAffected != 1 {
			return fmt.Errorf("%w: stale or cancelled ingestion publish", domainknowledge.ErrIngestionConflict)
		}
		if err := tx.Exec(`DELETE FROM ai_knowledge_ingestion_document_staging WHERE job_id=? AND lease_token=?`, job.ID, job.LeaseToken).Error; err != nil {
			return fmt.Errorf("clear published knowledge staging: %w", err)
		}
		return insertIngestionStage(tx, job)
	})
}

type stagedIngestionDocument struct {
	document domainknowledge.Document
	chunks   []domainknowledge.Chunk
}

func loadStagedIngestionDocuments(tx *gorm.DB, jobID, leaseToken string) ([]stagedIngestionDocument, error) {
	rows, err := tx.Raw(`
		SELECT document_payload,chunks_payload
		FROM ai_knowledge_ingestion_document_staging
		WHERE job_id=? AND lease_token=? ORDER BY document_id FOR UPDATE`, jobID, leaseToken).Rows()
	if err != nil {
		return nil, fmt.Errorf("list staged knowledge documents: %w", err)
	}
	defer func() { _ = rows.Close() }()
	items := make([]stagedIngestionDocument, 0)
	for rows.Next() {
		var documentPayload, chunksPayload []byte
		if err := rows.Scan(&documentPayload, &chunksPayload); err != nil {
			return nil, fmt.Errorf("scan staged knowledge document: %w", err)
		}
		var item stagedIngestionDocument
		if err := json.Unmarshal(documentPayload, &item.document); err != nil {
			return nil, fmt.Errorf("decode staged knowledge document: %w", err)
		}
		if err := json.Unmarshal(chunksPayload, &item.chunks); err != nil {
			return nil, fmt.Errorf("decode staged knowledge chunks: %w", err)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate staged knowledge documents: %w", err)
	}
	return items, nil
}

func upsertPublishedDocument(tx *gorm.DB, document domainknowledge.Document, chunks []domainknowledge.Chunk, publishedAt time.Time) error {
	document.Status = domainknowledge.DocumentStatusIndexed
	document.UpdatedAt = publishedAt
	acl, err := json.Marshal(document.ACL)
	if err != nil {
		return fmt.Errorf("encode published knowledge document ACL: %w", err)
	}
	if err := tx.Exec(`
		INSERT INTO ai_knowledge_documents(
			id,knowledge_base_id,source_id,external_id,title,uri,version,content_hash,acl,status,
			chunk_count,created_at,updated_at
		) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)
		ON CONFLICT(id) DO UPDATE SET
			title=EXCLUDED.title,uri=EXCLUDED.uri,version=EXCLUDED.version,
			content_hash=EXCLUDED.content_hash,acl=EXCLUDED.acl,status=EXCLUDED.status,
			chunk_count=EXCLUDED.chunk_count,updated_at=EXCLUDED.updated_at`,
		document.ID, document.KnowledgeBaseID, document.SourceID, document.ExternalID,
		document.Title, document.URI, document.Version, document.ContentHash, acl,
		document.Status, document.ChunkCount, document.CreatedAt, document.UpdatedAt,
	).Error; err != nil {
		return fmt.Errorf("publish knowledge document: %w", err)
	}
	if err := tx.Exec(`DELETE FROM ai_knowledge_chunks WHERE document_id=?`, document.ID).Error; err != nil {
		return fmt.Errorf("replace published knowledge chunks: %w", err)
	}
	for _, chunk := range chunks {
		chunkACL, location, err := marshal(chunk.ACL, chunk.Location)
		if err != nil {
			return fmt.Errorf("encode published knowledge chunk: %w", err)
		}
		if err := tx.Exec(`
			INSERT INTO ai_knowledge_chunks(
				id,knowledge_base_id,document_id,document_title,ordinal,content,content_hash,
				location,token_count,acl,created_at
			) VALUES(?,?,?,?,?,?,?,?,?,?,?)`,
			chunk.ID, chunk.KnowledgeBaseID, chunk.DocumentID, chunk.DocumentTitle, chunk.Ordinal,
			chunk.Content, chunk.ContentHash, location, chunk.TokenCount, chunkACL, chunk.CreatedAt,
		).Error; err != nil {
			return fmt.Errorf("publish knowledge chunk: %w", err)
		}
	}
	return nil
}

func encodeIngestionState(job domainknowledge.IngestionJob) ([]byte, []byte, error) {
	checkpoint, err := json.Marshal(job.Checkpoint)
	if err != nil {
		return nil, nil, fmt.Errorf("encode knowledge ingestion checkpoint: %w", err)
	}
	principal, err := json.Marshal(job.PrincipalSnapshot)
	if err != nil {
		return nil, nil, fmt.Errorf("encode knowledge ingestion principal: %w", err)
	}
	return checkpoint, principal, nil
}

type ingestionRowScanner interface {
	Scan(...any) error
}

func scanIngestionJob(row ingestionRowScanner) (domainknowledge.IngestionJob, error) {
	var job domainknowledge.IngestionJob
	var checkpoint, principal []byte
	if err := row.Scan(
		&job.ID,
		&job.KnowledgeBaseID,
		&job.SourceID,
		&job.TargetRevision,
		&job.Stage,
		&job.Status,
		&job.Attempt,
		&job.MaxAttempts,
		&job.CancelRequested,
		&checkpoint,
		&principal,
		&job.ErrorCode,
		&job.Error,
		&job.NextAttemptAt,
		&job.LeaseToken,
		&job.LeaseExpiresAt,
		&job.CreatedAt,
		&job.UpdatedAt,
		&job.CompletedAt,
	); err != nil {
		return job, err
	}
	if err := json.Unmarshal(checkpoint, &job.Checkpoint); err != nil {
		return job, fmt.Errorf("decode knowledge ingestion checkpoint: %w", err)
	}
	if err := json.Unmarshal(principal, &job.PrincipalSnapshot); err != nil {
		return job, fmt.Errorf("decode knowledge ingestion principal: %w", err)
	}
	return job, nil
}

func mapIngestionScanError(job domainknowledge.IngestionJob, err error) (domainknowledge.IngestionJob, error) {
	if errors.Is(err, sql.ErrNoRows) {
		return domainknowledge.IngestionJob{}, domainknowledge.ErrIngestionNotFound
	}
	if err != nil {
		return domainknowledge.IngestionJob{}, fmt.Errorf("read knowledge ingestion job: %w", err)
	}
	return job, nil
}

func insertIngestionStage(tx *gorm.DB, job domainknowledge.IngestionJob) error {
	checkpoint, err := json.Marshal(job.Checkpoint)
	if err != nil {
		return fmt.Errorf("encode knowledge ingestion stage: %w", err)
	}
	if err := tx.Exec(`
		INSERT INTO ai_knowledge_ingestion_stages(
			job_id,sequence,stage,status,checkpoint,error_code,started_at,completed_at
		)
		SELECT ?,COALESCE(MAX(sequence),-1)+1,?,?,?,?,?,?
		FROM ai_knowledge_ingestion_stages WHERE job_id=?`,
		job.ID,
		job.Stage,
		job.Status,
		checkpoint,
		job.ErrorCode,
		job.UpdatedAt,
		job.CompletedAt,
		job.ID,
	).Error; err != nil {
		return fmt.Errorf("record knowledge ingestion stage: %w", err)
	}
	return nil
}
