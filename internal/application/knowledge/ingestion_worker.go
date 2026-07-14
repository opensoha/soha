package knowledge

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainknowledge "github.com/opensoha/soha/internal/domain/knowledge"
)

const (
	ingestionPollInterval  = 500 * time.Millisecond
	ingestionLeaseDuration = 5 * time.Minute
	ingestionFetchTimeout  = 2 * time.Minute
	ingestionModelTimeout  = time.Minute
	ingestionModelBatch    = 255
)

type preparedIngestion struct {
	documents []domainknowledge.Document
	chunks    [][]domainknowledge.Chunk
	cursor    string
	hash      string
}

type ingestionExecution struct {
	job        *domainknowledge.IngestionJob
	principal  domainidentity.Principal
	source     domainknowledge.Source
	documents  []domainknowledge.SourceDocument
	prepared   preparedIngestion
	checkpoint domainknowledge.IngestionCheckpoint
}

// RunIngestionWorker runs one bounded worker until its lifecycle context is cancelled.
func (s *Service) RunIngestionWorker(ctx context.Context) {
	if s == nil || s.productionRepo == nil || s.loader == nil {
		return
	}
	ticker := time.NewTicker(ingestionPollInterval)
	defer ticker.Stop()
	for {
		worked, err := s.processNextIngestion(ctx)
		if err != nil && ctx.Err() != nil {
			return
		}
		if worked {
			continue
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (s *Service) processNextIngestion(ctx context.Context) (bool, error) {
	now := s.now()
	job, err := s.productionRepo.ClaimIngestionJob(ctx, now, uuid.NewString(), now.Add(ingestionLeaseDuration))
	if err != nil || job == nil {
		return false, err
	}
	if err := s.executeIngestion(ctx, job); err != nil {
		return true, err
	}
	return true, nil
}

func (s *Service) executeIngestion(ctx context.Context, job *domainknowledge.IngestionJob) error {
	execution := &ingestionExecution{job: job, principal: ingestionPrincipal(job.PrincipalSnapshot)}
	steps := []func(context.Context, *ingestionExecution) error{
		s.initializeIngestion,
		s.fetchIngestion,
		s.prepareIngestionStage,
		s.embedIngestion,
		s.indexIngestion,
		s.verifyAndPublishIngestion,
	}
	for _, step := range steps {
		if err := step(ctx, execution); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) initializeIngestion(ctx context.Context, execution *ingestionExecution) error {
	job := execution.job
	if job.Stage != domainknowledge.IngestionStageDiscovering {
		checkpoint := domainknowledge.IngestionCheckpoint{Stage: domainknowledge.IngestionStageDiscovering, RecordedAt: s.now()}
		if err := s.advanceIngestion(ctx, job, domainknowledge.IngestionJobRunning, domainknowledge.IngestionStageDiscovering, checkpoint); err != nil {
			return err
		}
	}
	source, err := s.productionRepo.GetSourceInternal(ctx, job.KnowledgeBaseID, job.SourceID)
	if err != nil {
		return s.failIngestion(ctx, job, execution.principal, "source_unavailable", err)
	}
	execution.source = source
	return s.stopIfIngestionCancelled(ctx, execution)
}

func (s *Service) fetchIngestion(ctx context.Context, execution *ingestionExecution) error {
	job := execution.job
	if err := s.advanceIngestion(ctx, job, domainknowledge.IngestionJobRunning, domainknowledge.IngestionStageFetching, checkpointFor(job, domainknowledge.IngestionStageFetching, "")); err != nil {
		return err
	}
	fetchCtx, cancelFetch := context.WithTimeout(ctx, ingestionFetchTimeout)
	documents, cursor, err := s.loader.Load(fetchCtx, execution.principal, execution.source)
	cancelFetch()
	if err != nil {
		return s.failIngestion(ctx, job, execution.principal, "fetch_failed", err)
	}
	execution.documents = documents
	execution.prepared.cursor = cursor
	return s.stopIfIngestionCancelled(ctx, execution)
}

func (s *Service) prepareIngestionStage(ctx context.Context, execution *ingestionExecution) error {
	job := execution.job
	cursor := execution.prepared.cursor
	if err := s.advanceIngestion(ctx, job, domainknowledge.IngestionJobRunning, domainknowledge.IngestionStageParsing, checkpointFor(job, domainknowledge.IngestionStageParsing, cursor)); err != nil {
		return err
	}
	prepared, err := prepareIngestion(execution.source, execution.principal.UserID, execution.documents, cursor, s.now)
	if err != nil {
		return s.failIngestion(ctx, job, execution.principal, "parse_failed", err)
	}
	checkpoint := checkpointFor(job, domainknowledge.IngestionStageChunking, cursor)
	checkpoint.DocumentsSeen = len(execution.documents)
	checkpoint.DocumentsStored = len(prepared.documents)
	checkpoint.ContentHash = prepared.hash
	for _, chunks := range prepared.chunks {
		checkpoint.ChunksStored += len(chunks)
	}
	if err := s.advanceIngestion(ctx, job, domainknowledge.IngestionJobRunning, domainknowledge.IngestionStageChunking, checkpoint); err != nil {
		return err
	}
	execution.prepared = prepared
	execution.checkpoint = checkpoint
	return nil
}

func (s *Service) embedIngestion(ctx context.Context, execution *ingestionExecution) error {
	job := execution.job
	checkpoint := execution.checkpoint
	if err := s.advanceIngestion(ctx, job, domainknowledge.IngestionJobRunning, domainknowledge.IngestionStageEmbedding, checkpointWithStage(checkpoint, domainknowledge.IngestionStageEmbedding, s.now())); err != nil {
		return err
	}
	allChunks := flattenChunks(execution.prepared.chunks)
	for start := 0; start < len(allChunks); start += ingestionModelBatch {
		end := min(start+ingestionModelBatch, len(allChunks))
		modelCtx, cancelModel := context.WithTimeout(ctx, ingestionModelTimeout)
		_, scoreErr := s.vector.Score(modelCtx, execution.principal, "index:"+job.ID, allChunks[start:end])
		cancelModel()
		if scoreErr != nil {
			return s.failIngestion(ctx, job, execution.principal, "embedding_failed", scoreErr)
		}
		if err := s.stopIfIngestionCancelled(ctx, execution); err != nil {
			return err
		}
		if err := s.advanceIngestion(ctx, job, domainknowledge.IngestionJobRunning, domainknowledge.IngestionStageEmbedding, checkpointWithStage(checkpoint, domainknowledge.IngestionStageEmbedding, s.now())); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) indexIngestion(ctx context.Context, execution *ingestionExecution) error {
	job := execution.job
	checkpoint := execution.checkpoint
	if err := s.advanceIngestion(ctx, job, domainknowledge.IngestionJobRunning, domainknowledge.IngestionStageIndexing, checkpointWithStage(checkpoint, domainknowledge.IngestionStageIndexing, s.now())); err != nil {
		return err
	}
	for index, document := range execution.prepared.documents {
		if err := ctx.Err(); err != nil {
			return s.failIngestionAfterCancellation(ctx, job, execution.principal, err)
		}
		if err := s.productionRepo.StageIngestionDocument(ctx, job.ID, job.LeaseToken, document, execution.prepared.chunks[index]); err != nil {
			return s.failIngestion(ctx, job, execution.principal, "index_failed", err)
		}
	}
	return nil
}

func (s *Service) verifyAndPublishIngestion(ctx context.Context, execution *ingestionExecution) error {
	job := execution.job
	checkpoint := execution.checkpoint
	if err := s.advanceIngestion(ctx, job, domainknowledge.IngestionJobRunning, domainknowledge.IngestionStageVerifying, checkpointWithStage(checkpoint, domainknowledge.IngestionStageVerifying, s.now())); err != nil {
		return err
	}
	if len(execution.prepared.documents) == 0 || checkpoint.ChunksStored == 0 {
		return s.failIngestion(ctx, job, execution.principal, "verification_failed", fmt.Errorf("no indexable content"))
	}
	if err := s.stopIfIngestionCancelled(ctx, execution); err != nil {
		return err
	}
	if err := s.advanceIngestion(ctx, job, domainknowledge.IngestionJobRunning, domainknowledge.IngestionStagePublishing, checkpointWithStage(checkpoint, domainknowledge.IngestionStagePublishing, s.now())); err != nil {
		return err
	}
	completed := s.now()
	publishedJob := *job
	publishedJob.Status = domainknowledge.IngestionJobSucceeded
	publishedJob.UpdatedAt = completed
	publishedJob.CompletedAt = &completed
	publishedJob.Checkpoint = checkpointWithStage(checkpoint, domainknowledge.IngestionStagePublishing, completed)
	execution.source.Cursor = execution.prepared.cursor
	execution.source.Status = domainknowledge.SourceStatusReady
	execution.source.LastError = ""
	execution.source.LastSyncedAt = &completed
	execution.source.UpdatedAt = completed
	revision := domainknowledge.IndexRevision{
		ID:              uuid.NewString(),
		KnowledgeBaseID: job.KnowledgeBaseID,
		Revision:        int(job.TargetRevision),
		EmbeddingModel:  "gateway-route",
		ChunkerVersion:  "rune-v1-1200-150",
		DocumentCount:   len(execution.prepared.documents),
		ChunkCount:      checkpoint.ChunksStored,
		Status:          domainknowledge.IndexStatusActive,
		CreatedAt:       completed,
		ActivatedAt:     &completed,
	}
	if err := s.productionRepo.PublishIngestionJob(ctx, publishedJob, execution.source, revision); err != nil {
		return s.failIngestion(ctx, job, execution.principal, "publish_failed", err)
	}
	*job = publishedJob
	s.record(ctx, execution.principal, "knowledge.ingestion.complete", job.ID, execution.source.Name, nil)
	return nil
}

func (s *Service) stopIfIngestionCancelled(ctx context.Context, execution *ingestionExecution) error {
	cancelled, err := s.cancelIngestionIfRequested(ctx, execution.job, execution.principal)
	if err != nil || cancelled {
		return err
	}
	return nil
}

func (s *Service) advanceIngestion(
	ctx context.Context,
	job *domainknowledge.IngestionJob,
	status domainknowledge.IngestionJobStatus,
	stage domainknowledge.IngestionStage,
	checkpoint domainknowledge.IngestionCheckpoint,
) error {
	if err := validateIngestionTransition(job.Status, job.Stage, status, stage); err != nil {
		return err
	}
	expectedStatus, expectedStage := job.Status, job.Stage
	job.Status = status
	job.Stage = stage
	job.Checkpoint = checkpoint
	job.UpdatedAt = checkpoint.RecordedAt
	leaseExpiresAt := checkpoint.RecordedAt.Add(ingestionLeaseDuration)
	job.LeaseExpiresAt = &leaseExpiresAt
	if err := s.productionRepo.AdvanceIngestionJob(ctx, *job, expectedStatus, expectedStage); err != nil {
		job.Status, job.Stage = expectedStatus, expectedStage
		return err
	}
	return nil
}

func validateIngestionTransition(
	fromStatus domainknowledge.IngestionJobStatus,
	fromStage domainknowledge.IngestionStage,
	toStatus domainknowledge.IngestionJobStatus,
	toStage domainknowledge.IngestionStage,
) error {
	if fromStatus == domainknowledge.IngestionJobRunning && toStatus == domainknowledge.IngestionJobRunning {
		if toStage == fromStage || toStage == domainknowledge.IngestionStageDiscovering || nextIngestionStage(fromStage) == toStage {
			return nil
		}
	}
	if fromStatus == domainknowledge.IngestionJobRunning {
		switch toStatus {
		case domainknowledge.IngestionJobRetryWait, domainknowledge.IngestionJobFailed, domainknowledge.IngestionJobCancelled:
			return nil
		}
	}
	if fromStatus == domainknowledge.IngestionJobCancelling && toStatus == domainknowledge.IngestionJobCancelled {
		return nil
	}
	return fmt.Errorf("%w: invalid ingestion transition %s/%s -> %s/%s", domainknowledge.ErrIngestionConflict, fromStatus, fromStage, toStatus, toStage)
}

func nextIngestionStage(stage domainknowledge.IngestionStage) domainknowledge.IngestionStage {
	switch stage {
	case domainknowledge.IngestionStageDiscovering:
		return domainknowledge.IngestionStageFetching
	case domainknowledge.IngestionStageFetching:
		return domainknowledge.IngestionStageParsing
	case domainknowledge.IngestionStageParsing:
		return domainknowledge.IngestionStageChunking
	case domainknowledge.IngestionStageChunking:
		return domainknowledge.IngestionStageEmbedding
	case domainknowledge.IngestionStageEmbedding:
		return domainknowledge.IngestionStageIndexing
	case domainknowledge.IngestionStageIndexing:
		return domainknowledge.IngestionStageVerifying
	case domainknowledge.IngestionStageVerifying:
		return domainknowledge.IngestionStagePublishing
	default:
		return ""
	}
}

func (s *Service) cancelIngestionIfRequested(
	ctx context.Context,
	job *domainknowledge.IngestionJob,
	principal domainidentity.Principal,
) (bool, error) {
	current, err := s.productionRepo.GetIngestionJobInternal(ctx, job.ID)
	if err != nil {
		return false, err
	}
	if !current.CancelRequested {
		return false, nil
	}
	completed := s.now()
	current.Status = domainknowledge.IngestionJobCancelled
	current.CompletedAt = &completed
	current.UpdatedAt = completed
	current.ErrorCode = "cancelled"
	current.Error = "ingestion cancelled"
	if err := s.productionRepo.AdvanceIngestionJob(ctx, current, domainknowledge.IngestionJobCancelling, current.Stage); err != nil {
		return false, err
	}
	*job = current
	s.record(ctx, principal, "knowledge.ingestion.cancelled", job.ID, job.SourceID, nil)
	return true, nil
}

func (s *Service) failIngestionAfterCancellation(
	ctx context.Context,
	job *domainknowledge.IngestionJob,
	principal domainidentity.Principal,
	cause error,
) error {
	finalizeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	return s.failIngestion(finalizeCtx, job, principal, "worker_cancelled", cause)
}

func (s *Service) failIngestion(
	ctx context.Context,
	job *domainknowledge.IngestionJob,
	principal domainidentity.Principal,
	code string,
	cause error,
) error {
	if ctx.Err() != nil {
		finalizeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		return s.failIngestion(finalizeCtx, job, principal, code, cause)
	}
	if cancelled, cancelErr := s.cancelIngestionIfRequested(ctx, job, principal); cancelled {
		return cancelErr
	} else if cancelErr != nil && !errors.Is(cancelErr, domainknowledge.ErrIngestionNotFound) {
		return errors.Join(cause, cancelErr)
	}
	now := s.now()
	expectedStatus, expectedStage := job.Status, job.Stage
	job.Stage = domainknowledge.IngestionStageDiscovering
	job.ErrorCode = code
	job.Error = "ingestion stage failed"
	job.UpdatedAt = now
	job.Checkpoint = domainknowledge.IngestionCheckpoint{Stage: job.Stage, RecordedAt: now}
	if job.Attempt < job.MaxAttempts && !job.CancelRequested {
		next := nextIngestionRetry(job.Attempt, now)
		job.Status = domainknowledge.IngestionJobRetryWait
		job.NextAttemptAt = &next
	} else {
		job.Status = domainknowledge.IngestionJobFailed
		job.CompletedAt = &now
	}
	if err := s.productionRepo.AdvanceIngestionJob(ctx, *job, expectedStatus, expectedStage); err != nil {
		return errors.Join(cause, err)
	}
	s.record(ctx, principal, "knowledge.ingestion.failed", job.ID, job.SourceID, cause)
	return cause
}

func prepareIngestion(
	source domainknowledge.Source,
	ownerID string,
	loaded []domainknowledge.SourceDocument,
	cursor string,
	now func() time.Time,
) (preparedIngestion, error) {
	if len(loaded) > 10000 {
		return preparedIngestion{}, fmt.Errorf("%w: source returned too many documents", domainknowledge.ErrRetrievalExhausted)
	}
	prepared := preparedIngestion{
		documents: make([]domainknowledge.Document, 0, len(loaded)),
		chunks:    make([][]domainknowledge.Chunk, 0, len(loaded)),
		cursor:    cursor,
	}
	hasher := sha256.New()
	for _, item := range loaded {
		item.ExternalID = strings.TrimSpace(item.ExternalID)
		item.Content = strings.TrimSpace(item.Content)
		if item.ExternalID == "" || item.Content == "" {
			continue
		}
		documentNow := now()
		document := domainknowledge.Document{
			ID:              stableDocumentID(source.ID, item.ExternalID),
			KnowledgeBaseID: source.KnowledgeBaseID,
			SourceID:        source.ID,
			ExternalID:      item.ExternalID,
			Title:           firstNonEmpty(item.Title, item.ExternalID),
			URI:             strings.TrimSpace(item.URI),
			Version:         firstNonEmpty(item.Version, contentHash(item.Content)),
			ContentHash:     contentHash(item.Content),
			ACL:             normalizeAccessScope(item.ACL, ownerID),
			Status:          domainknowledge.DocumentStatusPending,
			CreatedAt:       documentNow,
			UpdatedAt:       documentNow,
		}
		chunks := chunkDocument(document, item.Content)
		document.ChunkCount = len(chunks)
		prepared.documents = append(prepared.documents, document)
		prepared.chunks = append(prepared.chunks, chunks)
		_, _ = hasher.Write([]byte(document.ContentHash))
	}
	prepared.hash = "sha256:" + hex.EncodeToString(hasher.Sum(nil))
	return prepared, nil
}

func flattenChunks(groups [][]domainknowledge.Chunk) []domainknowledge.Chunk {
	total := 0
	for _, group := range groups {
		total += len(group)
	}
	chunks := make([]domainknowledge.Chunk, 0, total)
	for _, group := range groups {
		chunks = append(chunks, group...)
	}
	return chunks
}

func checkpointFor(job *domainknowledge.IngestionJob, stage domainknowledge.IngestionStage, cursor string) domainknowledge.IngestionCheckpoint {
	checkpoint := job.Checkpoint
	checkpoint.Stage = stage
	checkpoint.Cursor = cursor
	checkpoint.RecordedAt = time.Now().UTC()
	return checkpoint
}

func checkpointWithStage(checkpoint domainknowledge.IngestionCheckpoint, stage domainknowledge.IngestionStage, now time.Time) domainknowledge.IngestionCheckpoint {
	checkpoint.Stage = stage
	checkpoint.RecordedAt = now
	return checkpoint
}
