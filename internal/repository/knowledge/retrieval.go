package knowledge

import (
	"context"
	"fmt"
	"strings"

	domainknowledge "github.com/opensoha/soha/internal/domain/knowledge"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"gorm.io/gorm"
)

const versionChunkColumns = `c.id,d.knowledge_base_id,d.id AS document_id,d.title AS document_title,c.ordinal,c.content,c.content_hash,c.location,c.token_count,d.acl,c.created_at,v.id AS document_version_id,v.index_revision_id,c.embedding,c.embedding_model,c.embedding_route,c.embedding_dimension,c.search_vector`

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
		SELECT c.id,d.knowledge_base_id,d.id,d.title,c.ordinal,c.content,c.content_hash,c.location,c.token_count,d.acl,c.created_at,'' AS document_version_id,'' AS index_revision_id,NULL::vector AS embedding,'' AS embedding_model,'' AS embedding_route,0 AS embedding_dimension,to_tsvector('simple',c.content) AS search_vector
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
	pattern := "%" + strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(request.Query) + "%"
	query += `, lexical_raw AS (
		SELECT id,ts_rank_cd(search_vector,websearch_to_tsquery('simple',?))+similarity(content,?) AS relevance
		FROM authorized WHERE search_vector @@ websearch_to_tsquery('simple',?) OR content ILIKE ?
		ORDER BY relevance DESC,id LIMIT ?
	), lexical AS (SELECT id,61.0/(60+row_number() OVER (ORDER BY relevance DESC,id)) AS lexical_score,0.0 AS vector_score FROM lexical_raw)`
	args = append(args, request.Query, request.Query, request.Query, pattern, limit)
	vectorQuery, vectorArgs, err := knowledgeVectorCandidates(batch, limit, false)
	if err != nil {
		return nil, err
	}
	exactQuery, exactArgs, err := knowledgeVectorCandidates(batch, limit, true)
	if err != nil {
		return nil, err
	}
	query += `, vector_candidates AS MATERIALIZED (` + vectorQuery + `), vectors AS (
		SELECT * FROM vector_candidates UNION ALL (` + exactQuery + `)
	), scores AS (
		SELECT id,max(lexical_score) AS lexical_score,max(vector_score) AS vector_score FROM
		(SELECT * FROM lexical UNION ALL SELECT * FROM vectors) ranked GROUP BY id
	)
	SELECT c.id,c.knowledge_base_id,c.document_id,c.document_title,c.ordinal,c.content,c.content_hash,c.location,c.token_count,c.acl,c.created_at,c.document_version_id,c.index_revision_id,s.lexical_score,s.vector_score,c.embedding_model,c.embedding_route,coalesce(c.embedding_dimension,0)
	FROM authorized c JOIN scores s ON s.id=c.id ORDER BY (0.45*s.lexical_score+0.55*s.vector_score) DESC,c.id LIMIT ?`
	args = append(args, vectorArgs...)
	args = append(args, exactArgs...)
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
			if err := rows.Scan(&item.ID, &item.KnowledgeBaseID, &item.DocumentID, &item.DocumentTitle, &item.Ordinal, &item.Content, &item.ContentHash, &location, &item.TokenCount, &acl, &item.CreatedAt, &item.DocumentVersionID, &item.IndexRevisionID, &item.LexicalScore, &item.VectorScore, &item.EmbeddingModel, &item.EmbeddingRoute, &item.EmbeddingDimension); err != nil {
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
	if len(batch.Vectors) == 0 {
		return `SELECT ''::text AS id,0.0 AS lexical_score,0.0 AS vector_score WHERE false`, nil, nil
	}
	if len(batch.Vectors) != 1 || batch.Model == "" {
		return "", nil, fmt.Errorf("invalid retrieval embedding batch")
	}
	vector, err := knowledgeVector(batch.Vectors[0])
	if err != nil {
		return "", nil, err
	}
	dimension := len(batch.Vectors[0])
	distance := `embedding <=> ?::vector`
	if dimension == 1536 {
		distance = `embedding::vector(1536) <=> ?::vector(1536)`
	}
	if dimension == 3072 {
		distance = `embedding::halfvec(3072) <=> ?::halfvec(3072)`
	}
	// The legacy UNION has no embeddings and prevents an ordered HNSW scan.
	filter, order := "", distance
	args := []any{vector, dimension, batch.Model, batch.Route}
	if exact {
		// Filtered ANN can exhaust its scan after returning only some authorized rows.
		filter = ` AND (SELECT count(*) FROM vector_candidates) < ?`
		args = append(args, limit)
		order = `(` + distance + `)+0`
	}
	query := `SELECT id,0.0 AS lexical_score,greatest(0.0,1-(` + distance + `)) AS vector_score FROM authorized_current WHERE embedding_dimension=? AND embedding_model=? AND embedding_route=?` + filter + ` ORDER BY ` + order + ` LIMIT ?`
	return query, append(args, vector, limit), nil
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
