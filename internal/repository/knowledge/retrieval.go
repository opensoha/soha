package knowledge

import (
	"context"
	"fmt"
	"strings"

	domainknowledge "github.com/opensoha/soha/internal/domain/knowledge"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"gorm.io/gorm"
)

const versionChunkColumns = `c.id,d.knowledge_base_id,d.id AS document_id,d.title AS document_title,c.ordinal,c.content,c.content_hash,c.location,c.token_count,d.acl,c.created_at,v.id AS document_version_id,v.index_revision_id,c.embedding,c.embedding_model,c.embedding_route,c.embedding_dimension,c.search_vector,
		c.embedding_upstream_id,c.embedding_provider_kind,c.embedding_upstream_model`

func authorizedKnowledgeWhere(p domainknowledge.PrincipalScope, request domainknowledge.SearchRequest) (string, []any) {
	baseClause, baseArgs := accessClause("b", p)
	docClause, docArgs := accessClause("d", p)
	query := `d.status='indexed' AND b.status='active' AND b.deleted_at IS NULL AND (` + baseClause + `) AND (` + docClause + `)`
	args := append(baseArgs, docArgs...)
	for _, filter := range []struct {
		column string
		values []string
	}{{"d.knowledge_base_id", request.KnowledgeBaseIDs}, {"d.source_id", request.Filters.SourceIDs}, {"d.id", request.Filters.DocumentIDs}} {
		if len(filter.values) == 0 {
			continue
		}
		query += ` AND ` + filter.column + ` IN (` + strings.TrimSuffix(strings.Repeat("?,", len(filter.values)), ",") + `)`
		for _, value := range filter.values {
			args = append(args, value)
		}
	}
	return query, args
}

func authorizedKnowledgeCTE(p domainknowledge.PrincipalScope, request domainknowledge.SearchRequest) (string, []any) {
	where, args := authorizedKnowledgeWhere(p, request)
	query := `WITH authorized_current AS NOT MATERIALIZED (
		SELECT ` + versionChunkColumns + ` FROM ai_knowledge_version_chunks c
		JOIN ai_knowledge_document_versions v ON v.id=c.document_version_id
		JOIN ai_knowledge_documents d ON d.id=v.document_id AND d.current_version_id=v.id AND d.content_hash=v.content_hash
		JOIN ai_knowledge_bases b ON b.id=d.knowledge_base_id WHERE ` + where + `
	), authorized AS NOT MATERIALIZED (
		SELECT * FROM authorized_current
		UNION ALL
		SELECT c.id,d.knowledge_base_id,d.id,d.title,c.ordinal,c.content,c.content_hash,c.location,c.token_count,d.acl,c.created_at,'' AS document_version_id,'' AS index_revision_id,NULL::vector AS embedding,'' AS embedding_model,'' AS embedding_route,0 AS embedding_dimension,to_tsvector('simple',c.content) AS search_vector,
			'' AS embedding_upstream_id,'' AS embedding_provider_kind,'' AS embedding_upstream_model
		FROM ai_knowledge_chunks c JOIN ai_knowledge_documents d ON d.id=c.document_id
		JOIN ai_knowledge_bases b ON b.id=d.knowledge_base_id
		WHERE ` + where + ` AND NOT EXISTS (SELECT 1 FROM ai_knowledge_document_versions v WHERE v.id=d.current_version_id AND v.content_hash=d.content_hash)
	)`
	return query, append(args, args...)
}

func (r *Repository) SearchAuthorizedChunks(ctx context.Context, p domainknowledge.PrincipalScope, request domainknowledge.SearchRequest, batch domainknowledge.EmbeddingBatch, limit int) ([]domainknowledge.Chunk, error) {
	if len(request.KnowledgeBaseIDs) == 0 {
		return []domainknowledge.Chunk{}, nil
	}
	limit = min(max(limit, 10), 500)
	query, args := authorizedKnowledgeCTE(p, request)
	policyQuery, policyArgs, err := r.retrievalPolicyQuery(ctx, p, request)
	if err != nil {
		return nil, err
	}
	query += `, retrieval_policies AS (` + policyQuery + `)`
	args = append(args, policyArgs...)
	availabilityQuery, availabilityArgs := knowledgeVectorAvailability(batch)
	query += `, vector_availability AS (` + availabilityQuery + `)`
	args = append(args, availabilityArgs...)
	pattern := "%" + strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(request.Query) + "%"
	// Mixed-weight RRF ranks a bounded pool: at most 500 raw matches per group.
	query += `, lexical_weights AS (
		SELECT p.knowledge_base_id,CASE WHEN a.knowledge_base_id IS NULL THEN 1.0 ELSE p.lexical_weight END AS weight
		FROM retrieval_policies p LEFT JOIN vector_availability a ON a.knowledge_base_id=p.knowledge_base_id
	), lexical_groups AS (
		SELECT weight,array_agg(knowledge_base_id) AS base_ids FROM lexical_weights GROUP BY weight
	), vector_groups AS (
		SELECT p.vector_weight AS weight,array_agg(p.knowledge_base_id) AS base_ids FROM retrieval_policies p
		JOIN vector_availability a ON a.knowledge_base_id=p.knowledge_base_id GROUP BY p.vector_weight
	), lexical_raw AS MATERIALIZED (
		SELECT matches.id,matches.relevance FROM lexical_groups weights CROSS JOIN LATERAL (
			SELECT c.id,ts_rank_cd(search_vector,websearch_to_tsquery('simple',?))+similarity(content,?) AS relevance
			FROM authorized c WHERE c.knowledge_base_id=ANY(weights.base_ids) AND (search_vector @@ websearch_to_tsquery('simple',?) OR content ILIKE ?)
			ORDER BY relevance DESC,c.id LIMIT (SELECT CASE WHEN count(*)=1 THEN ?::bigint ELSE 500 END FROM lexical_groups)
		) matches
	), lexical_ranked AS (SELECT id,61.0/(60+row_number() OVER (ORDER BY relevance DESC,id)) AS lexical_score FROM lexical_raw), lexical AS (
		SELECT l.id,l.lexical_score,0.0 AS vector_score FROM lexical_ranked l
		JOIN authorized c ON c.id=l.id JOIN lexical_weights w ON w.knowledge_base_id=c.knowledge_base_id
		ORDER BY (l.lexical_score*w.weight) DESC,l.id LIMIT ?)`
	args = append(args, request.Query, request.Query, request.Query, pattern, limit, limit)
	vectorQuery, vectorArgs, err := knowledgeVectorCandidates(batch, limit, false)
	if err != nil {
		return nil, err
	}
	exactQuery, exactArgs, err := knowledgeVectorCandidates(batch, limit, true)
	if err != nil {
		return nil, err
	}
	vectorScore, scoreArgs, err := knowledgeShortlistedVectorScore(batch)
	if err != nil {
		return nil, err
	}
	// Preserve known channel RRF; only missing lexical scores use the bounded shortlist's RRF.
	query += `, vector_candidates AS MATERIALIZED (` + vectorQuery + `), vector_exact_candidates AS MATERIALIZED (` + exactQuery + `), vectors AS (
		SELECT id,0.0 AS lexical_score,max(vector_score) AS vector_score FROM (
			SELECT * FROM vector_candidates UNION ALL SELECT * FROM vector_exact_candidates
		) grouped GROUP BY id ORDER BY max(vector_score*weight) DESC,id LIMIT ?
	), candidate_ids AS MATERIALIZED (
		SELECT id FROM lexical UNION SELECT id FROM vectors
	), candidate_rows AS MATERIALIZED (
		SELECT c.* FROM authorized c JOIN candidate_ids candidate ON candidate.id=c.id
	), shortlisted_lexical_raw AS (
		SELECT id,ts_rank_cd(search_vector,websearch_to_tsquery('simple',?))+similarity(content,?) AS relevance
		FROM candidate_rows WHERE search_vector @@ websearch_to_tsquery('simple',?) OR content ILIKE ?
	), shortlisted_lexical AS (
		SELECT id,61.0/(60+row_number() OVER (ORDER BY relevance DESC,id)) AS lexical_score FROM shortlisted_lexical_raw
	), scores AS (
		SELECT c.id,coalesce(original.lexical_score,l.lexical_score,0.0) AS lexical_score,` + vectorScore + ` AS vector_score
		FROM candidate_rows c LEFT JOIN lexical original ON original.id=c.id LEFT JOIN shortlisted_lexical l ON l.id=c.id
	)
	SELECT c.id,c.knowledge_base_id,c.document_id,c.document_title,c.ordinal,c.content,c.content_hash,c.location,c.token_count,c.acl,c.created_at,c.document_version_id,c.index_revision_id,s.lexical_score,s.vector_score,c.embedding_model,c.embedding_route,coalesce(c.embedding_dimension,0),
		c.embedding_upstream_id,c.embedding_provider_kind,c.embedding_upstream_model,a.knowledge_base_id IS NOT NULL
	FROM authorized c JOIN scores s ON s.id=c.id JOIN retrieval_policies p ON p.knowledge_base_id=c.knowledge_base_id
	LEFT JOIN vector_availability a ON a.knowledge_base_id=c.knowledge_base_id
	CROSS JOIN LATERAL (SELECT CASE WHEN a.knowledge_base_id IS NULL THEN s.lexical_score ELSE p.lexical_weight*s.lexical_score+p.vector_weight*s.vector_score END AS score) ranking
	WHERE ranking.score>=p.min_score ORDER BY ranking.score DESC,c.id LIMIT ?`
	args = append(args, vectorArgs...)
	args = append(args, exactArgs...)
	args = append(args, limit)
	args = append(args, request.Query, request.Query, request.Query, pattern)
	args = append(args, scoreArgs...)
	args = append(args, limit)
	items := []domainknowledge.Chunk{}
	err = r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec(`SET LOCAL hnsw.iterative_scan = 'strict_order'`).Error; err != nil {
			return err
		}
		rows, err := tx.Raw(query, args...).Rows()
		if err != nil {
			return err
		}
		defer func() { _ = rows.Close() }()
		for rows.Next() {
			var item domainknowledge.Chunk
			var acl, location []byte
			source := &item.EmbeddingSource
			if err := rows.Scan(&item.ID, &item.KnowledgeBaseID, &item.DocumentID, &item.DocumentTitle, &item.Ordinal, &item.Content, &item.ContentHash, &location, &item.TokenCount, &acl, &item.CreatedAt, &item.DocumentVersionID, &item.IndexRevisionID, &item.LexicalScore, &item.VectorScore, &item.EmbeddingModel, &item.EmbeddingRoute, &item.EmbeddingDimension,
				&source.UpstreamID, &source.ProviderKind, &source.UpstreamModel, &item.VectorAvailable); err != nil {
				return err
			}
			if err := unmarshal(location, &item.Location); err != nil {
				return err
			}
			if err := unmarshal(acl, &item.ACL); err != nil {
				return err
			}
			items = append(items, item)
		}
		return rows.Err()
	})
	return items, err
}

func knowledgeVectorCandidates(batch domainknowledge.EmbeddingBatch, limit int, exact bool) (string, []any, error) {
	if len(batch.Vectors) == 0 || !batch.Source.Known() {
		return `SELECT 0.0::float8 AS weight,''::text AS id,0.0 AS lexical_score,0.0 AS vector_score WHERE false`, nil, nil
	}
	if len(batch.Vectors) != 1 || batch.Model == "" {
		return "", nil, fmt.Errorf("invalid retrieval embedding batch")
	}
	vector, err := knowledgeVector(batch.Vectors[0])
	if err != nil {
		return "", nil, err
	}
	dimension := len(batch.Vectors[0])
	distance := knowledgeVectorDistance(dimension)
	// The legacy UNION has no embeddings and prevents an ordered HNSW scan.
	filter, order := "", distance
	args := []any{vector, dimension, batch.Model, batch.Route, batch.Source.UpstreamID, batch.Source.ProviderKind, batch.Source.UpstreamModel}
	if exact {
		// Filtered ANN can exhaust its scan after returning only some authorized rows.
		filter = ` AND (SELECT count(*) FROM vector_candidates ann WHERE ann.weight=weights.weight) < ?`
		args = append(args, limit)
		order = `(` + distance + `)+0`
	}
	// Keep the group KNN order as pure distance; apply policy weights only outside its window.
	query := `SELECT weights.weight, matches.id,0.0 AS lexical_score,matches.vector_score FROM vector_groups weights CROSS JOIN LATERAL (
		SELECT c.id,greatest(0.0,1-(` + distance + `)) AS vector_score FROM authorized_current c
		WHERE c.knowledge_base_id=ANY(weights.base_ids) AND embedding_dimension=? AND embedding_model=? AND embedding_route=? AND embedding_upstream_id=? AND embedding_provider_kind=? AND embedding_upstream_model=?` + filter + ` ORDER BY ` + order + ` LIMIT ?
	) matches`
	return query, append(args, vector, limit), nil
}

func knowledgeShortlistedVectorScore(batch domainknowledge.EmbeddingBatch) (string, []any, error) {
	if len(batch.Vectors) == 0 || !batch.Source.Known() {
		return `0.0`, nil, nil
	}
	if len(batch.Vectors) != 1 || batch.Model == "" {
		return "", nil, fmt.Errorf("invalid retrieval embedding batch")
	}
	vector, err := knowledgeVector(batch.Vectors[0])
	if err != nil {
		return "", nil, err
	}
	dimension := len(batch.Vectors[0])
	score := `CASE WHEN c.embedding IS NOT NULL AND c.embedding_dimension=? AND c.embedding_model=? AND c.embedding_route=?
		AND c.embedding_upstream_id=? AND c.embedding_provider_kind=? AND c.embedding_upstream_model=?
		THEN greatest(0.0,1-(` + knowledgeVectorDistance(dimension) + `)) ELSE 0.0 END`
	return score, []any{dimension, batch.Model, batch.Route, batch.Source.UpstreamID, batch.Source.ProviderKind, batch.Source.UpstreamModel, vector}, nil
}

func knowledgeVectorDistance(dimension int) string {
	switch dimension {
	case 1536:
		return `c.embedding::vector(1536) <=> ?::vector(1536)`
	case 3072:
		return `c.embedding::halfvec(3072) <=> ?::halfvec(3072)`
	default:
		return `c.embedding <=> ?::vector`
	}
}

// ValidateCitations reloads current ACL and the exact current content before another model call.
func (r *Repository) ValidateCitations(ctx context.Context, p domainknowledge.PrincipalScope, citations []domainknowledge.Citation) error {
	if len(citations) > 100 {
		return apperrors.ErrInvalidArgument
	}
	for _, citation := range citations {
		request := domainknowledge.SearchRequest{KnowledgeBaseIDs: []string{citation.KnowledgeBaseID}, Filters: domainknowledge.SearchFilters{DocumentIDs: []string{citation.DocumentID}}}
		query, args := authorizedKnowledgeCTE(p, request)
		query += ` SELECT count(*) FROM authorized WHERE id=? AND content_hash=? AND document_version_id=?`
		args = append(args, citation.ChunkID, citation.ContentHash, citation.DocumentVersionID)
		var count int64
		if err := r.db.WithContext(ctx).Raw(query, args...).Scan(&count).Error; err != nil {
			return err
		}
		if count != 1 {
			return fmt.Errorf("%w: citation is revoked, deleted or superseded", apperrors.ErrAccessDenied)
		}
	}
	return nil
}
