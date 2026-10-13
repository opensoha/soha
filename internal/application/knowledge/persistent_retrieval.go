package knowledge

import (
	"context"
	"fmt"

	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainknowledge "github.com/opensoha/soha/internal/domain/knowledge"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type retrievalCandidates struct {
	chunks            []domainknowledge.Chunk
	lexical, vector   []float64
	mode, degradation string
}

func (s *Service) searchCandidates(ctx context.Context, p domainidentity.Principal, request domainknowledge.SearchRequest, limit int) (retrievalCandidates, error) {
	if persistent, ok := s.repo.(PersistentRetrieval); ok {
		batch := domainknowledge.EmbeddingBatch{}
		mode, degradation := "lexical", "embedding_unavailable"
		if embedder, ok := s.vector.(Embedder); ok {
			modelCtx, cancel := context.WithTimeout(ctx, ingestionModelTimeout)
			var err error
			batch, err = embedder.Embed(modelCtx, p, []string{request.Query})
			cancel()
			if err == nil {
				mode, degradation = "hybrid", ""
			} else {
				batch = domainknowledge.EmbeddingBatch{}
			}
		}
		chunks, err := persistent.SearchAuthorizedChunks(ctx, principalScope(p), request, batch, limit)
		lexical, vector := make([]float64, len(chunks)), make([]float64, len(chunks))
		for i, chunk := range chunks {
			lexical[i], vector[i] = chunk.LexicalScore, chunk.VectorScore
		}
		if mode == "hybrid" && !compatibleIndexedEmbedding(chunks, batch) {
			mode, degradation = "lexical", "embedding_missing_or_mismatched"
		}
		return retrievalCandidates{chunks, lexical, vector, mode, degradation}, err
	}
	chunks, err := s.repo.ListAuthorizedChunks(ctx, principalScope(p), request, limit)
	if err != nil {
		return retrievalCandidates{}, err
	}
	vector, vectorErr := s.vector.Score(ctx, p, request.Query, chunks)
	mode, degradation := "hybrid", "legacy_candidate_window"
	if vectorErr != nil {
		vector = make([]float64, len(chunks))
		mode, degradation = "lexical", "embedding_unavailable"
	}
	return retrievalCandidates{chunks, s.lexical.Score(request.Query, chunks), vector, mode, degradation}, nil
}

func compatibleIndexedEmbedding(chunks []domainknowledge.Chunk, batch domainknowledge.EmbeddingBatch) bool {
	if len(batch.Vectors) != 1 {
		return false
	}
	for _, chunk := range chunks {
		if chunk.EmbeddingModel == batch.Model && chunk.EmbeddingRoute == batch.Route && chunk.EmbeddingDimension == len(batch.Vectors[0]) {
			return true
		}
	}
	return false
}

func (s *Service) ValidateCitations(ctx context.Context, p domainidentity.Principal, citations []domainknowledge.Citation) error {
	if err := s.authorize(ctx, p, PermKnowledgeView); err != nil {
		return err
	}
	repo, ok := s.repo.(PersistentRetrieval)
	if !ok {
		return fmt.Errorf("%w: citation validation unavailable", apperrors.ErrUnsupportedOperation)
	}
	return repo.ValidateCitations(ctx, principalScope(p), citations)
}
