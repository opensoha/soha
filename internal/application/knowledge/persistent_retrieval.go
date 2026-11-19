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
	vectorAvailable   map[string]bool
}

func (s *Service) searchCandidates(ctx context.Context, p domainidentity.Principal, request domainknowledge.SearchRequest, limit int) (retrievalCandidates, error) {
	if len(request.KnowledgeBaseIDs) == 0 {
		return retrievalCandidates{mode: "lexical", degradation: "embedding_unavailable"}, nil
	}
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
		available := make(map[string]bool)
		hybrid := false
		for i, chunk := range chunks {
			lexical[i], vector[i] = chunk.LexicalScore, chunk.VectorScore
			available[chunk.KnowledgeBaseID] = chunk.VectorAvailable
			hybrid = hybrid || chunk.VectorAvailable
		}
		if mode == "hybrid" && !hybrid {
			mode, degradation = "lexical", "embedding_missing_or_mismatched"
		}
		return retrievalCandidates{chunks, lexical, vector, mode, degradation, available}, err
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
	available := make(map[string]bool)
	for _, chunk := range chunks {
		available[chunk.KnowledgeBaseID] = vectorErr == nil
	}
	return retrievalCandidates{chunks, s.lexical.Score(request.Query, chunks), vector, mode, degradation, available}, nil
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
