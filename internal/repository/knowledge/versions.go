package knowledge

import (
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	domainknowledge "github.com/opensoha/soha/internal/domain/knowledge"
	"gorm.io/gorm"
)

func publishImmutableVersion(tx *gorm.DB, document domainknowledge.Document, chunks []domainknowledge.Chunk, revisionID string, publishedAt time.Time) error {
	acl, err := json.Marshal(document.ACL)
	if err != nil {
		return err
	}
	identity := document.ID + "\n" + document.Version + "\n" + document.ContentHash + "\n" + document.Title + "\n" + document.URI + "\n" + revisionID + "\n" + string(acl)
	if len(chunks) > 0 {
		chunk := chunks[0]
		identity += fmt.Sprintf("\n%s@%s:%d", chunk.EmbeddingModel, chunk.EmbeddingRoute, len(chunk.Embedding))
		if source := chunk.EmbeddingSource; source.Known() {
			identity += fmt.Sprintf("\n%q %q %q", source.UpstreamID, source.ProviderKind, source.UpstreamModel)
		}
	}
	versionID := uuid.NewSHA1(uuid.NameSpaceOID, []byte(identity)).String()
	if err := tx.Exec(`INSERT INTO ai_knowledge_document_versions(id,document_id,index_revision_id,title,uri,version,content_hash,created_at) VALUES(?,?,?,?,?,?,?,?) ON CONFLICT(id) DO NOTHING`, versionID, document.ID, revisionID, document.Title, document.URI, document.Version, document.ContentHash, publishedAt).Error; err != nil {
		return err
	}
	model, route, dimension := "", "", 0
	var source domainknowledge.EmbeddingSource
	embedded := 0
	for _, chunk := range chunks {
		if len(chunk.Embedding) > 0 {
			embedded++
			if dimension > 0 && (dimension != len(chunk.Embedding) || model != chunk.EmbeddingModel || route != chunk.EmbeddingRoute || source != chunk.EmbeddingSource) {
				return fmt.Errorf("knowledge version mixes embedding spaces")
			}
			model, route, dimension = chunk.EmbeddingModel, chunk.EmbeddingRoute, len(chunk.Embedding)
			source = chunk.EmbeddingSource
		}
		location, err := json.Marshal(chunk.Location)
		if err != nil {
			return err
		}
		vector, err := knowledgeVector(chunk.Embedding)
		if err != nil {
			return err
		}
		if len(chunk.Embedding) > 0 && chunk.EmbeddingModel == "" {
			return fmt.Errorf("knowledge embedding model is required")
		}
		chunkID := uuid.NewSHA1(uuid.NameSpaceOID, []byte(versionID+":"+strconv.Itoa(chunk.Ordinal))).String()
		chunkSource := chunk.EmbeddingSource
		if err := tx.Exec(`INSERT INTO ai_knowledge_version_chunks(
			id,document_version_id,ordinal,content,content_hash,location,token_count,embedding_model,embedding_route,embedding,created_at,
			embedding_upstream_id,embedding_provider_kind,embedding_upstream_model
		) VALUES(?,?,?,?,?,?,?,?,?,?::vector,?,?,?,?) ON CONFLICT(document_version_id,ordinal) DO NOTHING`,
			chunkID, versionID, chunk.Ordinal, chunk.Content, chunk.ContentHash, string(location), chunk.TokenCount, chunk.EmbeddingModel, chunk.EmbeddingRoute, vector, publishedAt,
			chunkSource.UpstreamID, chunkSource.ProviderKind, chunkSource.UpstreamModel).Error; err != nil {
			return err
		}
	}
	if embedded > 0 && embedded != len(chunks) {
		return fmt.Errorf("knowledge version has a partial embedding batch")
	}
	return tx.Exec(`UPDATE ai_knowledge_documents SET current_version_id=? WHERE id=?`, versionID, document.ID).Error
}

func knowledgeVector(values []float64) (any, error) {
	if len(values) == 0 {
		return nil, nil
	}
	if len(values) > 4096 {
		return nil, fmt.Errorf("knowledge embedding dimension exceeds 4096")
	}
	parts := make([]string, len(values))
	var norm float64
	for i, value := range values {
		if math.IsNaN(value) || math.IsInf(value, 0) {
			return nil, fmt.Errorf("knowledge embedding is not finite")
		}
		parts[i] = strconv.FormatFloat(value, 'g', -1, 64)
		norm += value * value
	}
	if norm == 0 {
		return nil, fmt.Errorf("knowledge embedding has zero norm")
	}
	return "[" + strings.Join(parts, ",") + "]", nil
}
