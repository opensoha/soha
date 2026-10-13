package knowledge

import (
	"context"
	"fmt"
	"math"
	"reflect"

	appaccess "github.com/opensoha/soha/internal/application/access"
	domainknowledge "github.com/opensoha/soha/internal/domain/knowledge"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

func (s *Service) authorizeIngestionExecution(ctx context.Context, execution *ingestionExecution) error {
	if err := s.refreshIngestionPrincipal(ctx, execution); err != nil {
		return s.failIngestion(ctx, execution.job, execution.principal, "authorization_revoked", err)
	}
	return nil
}

func (s *Service) refreshIngestionPrincipal(ctx context.Context, execution *ingestionExecution) error {
	if s.principals == nil {
		return fmt.Errorf("%w: ingestion identity reader is unavailable", apperrors.ErrAccessDenied)
	}
	snapshot := execution.job.PrincipalSnapshot
	principal, err := s.principals.CurrentExecutionPrincipal(ctx, snapshot.UserID, snapshot.AccessTokenID)
	if err != nil || principal.UserID != snapshot.UserID {
		return fmt.Errorf("%w: ingestion subject is no longer authorized", apperrors.ErrAccessDenied)
	}
	principal, err = appaccess.RuntimePrincipalWithin(ctx, s.permissions, principal, snapshot.PermissionKeys)
	if err != nil {
		return err
	}
	if err := s.authorize(ctx, principal, PermKnowledgeIngestionOperate); err != nil {
		return err
	}
	source, err := s.repo.GetSource(ctx, principalScope(principal), execution.job.KnowledgeBaseID, execution.job.SourceID)
	if err != nil {
		return err
	}
	if previous := execution.source; previous.ID != "" && (previous.Kind != source.Kind || previous.ConfigRef != source.ConfigRef || previous.Cursor != source.Cursor || !reflect.DeepEqual(previous.Config, source.Config)) {
		return fmt.Errorf("%w: source changed during ingestion; enqueue its current version", domainknowledge.ErrIngestionConflict)
	}
	execution.principal = principal
	return nil
}

func (s *Service) embedResumableIngestion(ctx context.Context, execution *ingestionExecution, embedder Embedder) error {
	groups := execution.prepared.chunks
	var chunks []*domainknowledge.Chunk
	owners := map[*domainknowledge.Chunk]int{}
	for i := range groups {
		for j := range groups[i] {
			chunk := &groups[i][j]
			chunks = append(chunks, chunk)
			owners[chunk] = i
		}
	}
	// One current-provider probe pins the model, route and dimension. Resume
	// never silently combines vectors produced by different embedding spaces.
	space := domainknowledge.EmbeddingBatch{}
	if err := embedPreparedChunks(ctx, embedder, execution.principal, chunks[:1], &space); err != nil {
		return err
	}
	if err := s.restoreIngestionVectors(ctx, execution, space); err != nil {
		return err
	}
	if err := s.stageEmbeddedDocuments(ctx, execution, map[int]bool{owners[chunks[0]]: true}); err != nil {
		return err
	}
	var pending []*domainknowledge.Chunk
	for _, chunk := range chunks {
		if len(chunk.Embedding) == 0 {
			pending = append(pending, chunk)
		}
	}
	for start := 0; start < len(pending); start += ingestionModelBatch {
		if err := s.stopIfIngestionCancelled(ctx, execution); err != nil {
			return err
		}
		batch := pending[start:min(start+ingestionModelBatch, len(pending))]
		if err := embedPreparedChunks(ctx, embedder, execution.principal, batch, &space); err != nil {
			return err
		}
		changed := map[int]bool{}
		for _, chunk := range batch {
			changed[owners[chunk]] = true
		}
		if err := s.stageEmbeddedDocuments(ctx, execution, changed); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) restoreIngestionVectors(ctx context.Context, execution *ingestionExecution, space domainknowledge.EmbeddingBatch) error {
	for i, document := range execution.prepared.documents {
		staged, err := s.productionRepo.LoadIngestionDocument(ctx, execution.job.ID, execution.job.LeaseToken, document)
		if err != nil {
			return err
		}
		for j := range execution.prepared.chunks[i] {
			chunk := &execution.prepared.chunks[i][j]
			if j >= len(staged) || len(chunk.Embedding) > 0 {
				continue
			}
			previous := staged[j]
			if previous.Ordinal != chunk.Ordinal || previous.DocumentID != document.ID || previous.KnowledgeBaseID != document.KnowledgeBaseID || previous.ContentHash != chunk.ContentHash || previous.EmbeddingModel != space.Model || previous.EmbeddingRoute != space.Route || !validIngestionVector(previous.Embedding, len(space.Vectors[0])) {
				continue
			}
			chunk.Embedding = previous.Embedding
			chunk.EmbeddingModel, chunk.EmbeddingRoute = previous.EmbeddingModel, previous.EmbeddingRoute
		}
	}
	return nil
}

func validIngestionVector(vector []float64, dimension int) bool {
	if len(vector) != dimension || dimension == 0 || dimension > 4096 {
		return false
	}
	norm := 0.0
	for _, value := range vector {
		if math.IsNaN(value) || math.IsInf(value, 0) {
			return false
		}
		norm += value * value
	}
	return norm > 0 && !math.IsInf(norm, 0)
}

func (s *Service) stageEmbeddedDocuments(ctx context.Context, execution *ingestionExecution, changed map[int]bool) error {
	for i := range changed {
		if err := s.productionRepo.StageIngestionDocument(ctx, execution.job.ID, execution.job.LeaseToken, execution.prepared.documents[i], execution.prepared.chunks[i]); err != nil {
			return err
		}
	}
	return s.advanceIngestion(ctx, execution.job, domainknowledge.IngestionJobRunning, domainknowledge.IngestionStageEmbedding, checkpointWithStage(execution.checkpoint, domainknowledge.IngestionStageEmbedding, s.now()))
}
