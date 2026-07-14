package knowledge

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	domainknowledge "github.com/opensoha/soha/internal/domain/knowledge"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"gorm.io/gorm"
)

type Repository struct{ db *gorm.DB }

func New(db *gorm.DB) *Repository { return &Repository{db: db} }

func (r *Repository) ListBases(ctx context.Context, principal domainknowledge.PrincipalScope) ([]domainknowledge.KnowledgeBase, error) {
	clause, args := accessClause("b", principal)
	rows, err := r.db.WithContext(ctx).Raw(`SELECT b.id,b.tenant_id,b.workspace_id,b.name,b.description,b.status,b.owner_id,b.scope,b.retrieval_policy,b.created_at,b.updated_at FROM ai_knowledge_bases b WHERE b.deleted_at IS NULL AND (`+clause+`) ORDER BY b.updated_at DESC`, args...).Rows()
	if err != nil {
		return nil, fmt.Errorf("list knowledge bases: %w", err)
	}
	defer func() { _ = rows.Close() }()
	items := []domainknowledge.KnowledgeBase{}
	for rows.Next() {
		item, err := scanBase(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *Repository) GetBase(ctx context.Context, principal domainknowledge.PrincipalScope, id string) (domainknowledge.KnowledgeBase, error) {
	clause, args := accessClause("b", principal)
	args = append([]any{id}, args...)
	row := r.db.WithContext(ctx).Raw(`SELECT b.id,b.tenant_id,b.workspace_id,b.name,b.description,b.status,b.owner_id,b.scope,b.retrieval_policy,b.created_at,b.updated_at FROM ai_knowledge_bases b WHERE b.id=? AND b.deleted_at IS NULL AND (`+clause+`) LIMIT 1`, args...).Row()
	item, err := scanBase(row)
	if errors.Is(err, sql.ErrNoRows) {
		return item, fmt.Errorf("%w: %w", apperrors.ErrNotFound, domainknowledge.ErrBaseNotFound)
	}
	return item, err
}

func (r *Repository) CreateBase(ctx context.Context, item domainknowledge.KnowledgeBase) (domainknowledge.KnowledgeBase, error) {
	scope, policy, err := marshal(item.Scope, item.RetrievalPolicy)
	if err != nil {
		return item, err
	}
	err = r.db.WithContext(ctx).Exec(`INSERT INTO ai_knowledge_bases(id,tenant_id,workspace_id,name,description,status,owner_id,scope,retrieval_policy,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?)`, item.ID, item.TenantID, item.WorkspaceID, item.Name, item.Description, item.Status, item.OwnerID, scope, policy, item.CreatedAt, item.UpdatedAt).Error
	return item, err
}

func (r *Repository) UpdateBase(ctx context.Context, p domainknowledge.PrincipalScope, item domainknowledge.KnowledgeBase) (domainknowledge.KnowledgeBase, error) {
	scope, policy, err := marshal(item.Scope, item.RetrievalPolicy)
	if err != nil {
		return item, err
	}
	clause, args := accessClause("ai_knowledge_bases", p)
	params := []any{item.Name, item.Description, item.TenantID, item.WorkspaceID, scope, policy, item.UpdatedAt, item.ID}
	params = append(params, args...)
	result := r.db.WithContext(ctx).Exec(`UPDATE ai_knowledge_bases SET name=?,description=?,tenant_id=?,workspace_id=?,scope=?,retrieval_policy=?,updated_at=? WHERE id=? AND deleted_at IS NULL AND (`+clause+`)`, params...)
	if result.Error != nil {
		return item, result.Error
	}
	if result.RowsAffected == 0 {
		return item, fmt.Errorf("%w: %w", apperrors.ErrNotFound, domainknowledge.ErrBaseNotFound)
	}
	return item, nil
}

func (r *Repository) DeleteBase(ctx context.Context, p domainknowledge.PrincipalScope, id string) error {
	clause, args := accessClause("ai_knowledge_bases", p)
	params := []any{id}
	params = append(params, args...)
	result := r.db.WithContext(ctx).Exec(`UPDATE ai_knowledge_bases SET deleted_at=CURRENT_TIMESTAMP,updated_at=CURRENT_TIMESTAMP WHERE id=? AND deleted_at IS NULL AND (`+clause+`)`, params...)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("%w: %w", apperrors.ErrNotFound, domainknowledge.ErrBaseNotFound)
	}
	return nil
}

func (r *Repository) ListSources(ctx context.Context, p domainknowledge.PrincipalScope, baseID string) ([]domainknowledge.Source, error) {
	if _, err := r.GetBase(ctx, p, baseID); err != nil {
		return nil, err
	}
	rows, err := r.db.WithContext(ctx).Raw(`SELECT id,knowledge_base_id,name,kind,config_ref,config,sync_policy,cursor,status,last_error,last_synced_at,created_at,updated_at FROM ai_knowledge_sources WHERE knowledge_base_id=? ORDER BY updated_at DESC`, baseID).Rows()
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	items := []domainknowledge.Source{}
	for rows.Next() {
		item, err := scanSource(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *Repository) GetSource(ctx context.Context, p domainknowledge.PrincipalScope, baseID, sourceID string) (domainknowledge.Source, error) {
	if _, err := r.GetBase(ctx, p, baseID); err != nil {
		return domainknowledge.Source{}, err
	}
	item, err := scanSource(r.db.WithContext(ctx).Raw(`SELECT id,knowledge_base_id,name,kind,config_ref,config,sync_policy,cursor,status,last_error,last_synced_at,created_at,updated_at FROM ai_knowledge_sources WHERE id=? AND knowledge_base_id=? LIMIT 1`, sourceID, baseID).Row())
	if errors.Is(err, sql.ErrNoRows) {
		return item, fmt.Errorf("%w: %w", apperrors.ErrNotFound, domainknowledge.ErrSourceNotFound)
	}
	return item, err
}

func (r *Repository) CreateSource(ctx context.Context, p domainknowledge.PrincipalScope, item domainknowledge.Source) (domainknowledge.Source, error) {
	if _, err := r.GetBase(ctx, p, item.KnowledgeBaseID); err != nil {
		return item, err
	}
	config, policy, err := marshal(item.Config, item.SyncPolicy)
	if err != nil {
		return item, err
	}
	err = r.db.WithContext(ctx).Exec(`INSERT INTO ai_knowledge_sources(id,knowledge_base_id,name,kind,config_ref,config,sync_policy,cursor,status,last_error,last_synced_at,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)`, item.ID, item.KnowledgeBaseID, item.Name, item.Kind, item.ConfigRef, config, policy, item.Cursor, item.Status, item.LastError, item.LastSyncedAt, item.CreatedAt, item.UpdatedAt).Error
	return item, err
}

func (r *Repository) UpdateSource(ctx context.Context, p domainknowledge.PrincipalScope, item domainknowledge.Source) error {
	if _, err := r.GetBase(ctx, p, item.KnowledgeBaseID); err != nil {
		return err
	}
	config, policy, err := marshal(item.Config, item.SyncPolicy)
	if err != nil {
		return err
	}
	result := r.db.WithContext(ctx).Exec(`UPDATE ai_knowledge_sources SET name=?,config_ref=?,config=?,sync_policy=?,cursor=?,status=?,last_error=?,last_synced_at=?,updated_at=? WHERE id=? AND knowledge_base_id=?`, item.Name, item.ConfigRef, config, policy, item.Cursor, item.Status, item.LastError, item.LastSyncedAt, item.UpdatedAt, item.ID, item.KnowledgeBaseID)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("%w: %w", apperrors.ErrNotFound, domainknowledge.ErrSourceNotFound)
	}
	return nil
}

func (r *Repository) ListDocuments(ctx context.Context, p domainknowledge.PrincipalScope, baseID string, limit int) ([]domainknowledge.Document, error) {
	if _, err := r.GetBase(ctx, p, baseID); err != nil {
		return nil, err
	}
	clause, args := accessClause("d", p)
	params := []any{baseID}
	params = append(params, args...)
	params = append(params, limit)
	rows, err := r.db.WithContext(ctx).Raw(`SELECT d.id,d.knowledge_base_id,d.source_id,d.external_id,d.title,d.uri,d.version,d.content_hash,d.acl,d.status,d.chunk_count,d.created_at,d.updated_at FROM ai_knowledge_documents d WHERE d.knowledge_base_id=? AND d.status<>'deleted' AND (`+clause+`) ORDER BY d.updated_at DESC LIMIT ?`, params...).Rows()
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	items := []domainknowledge.Document{}
	for rows.Next() {
		item, err := scanDocument(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *Repository) UpsertDocument(ctx context.Context, document domainknowledge.Document, chunks []domainknowledge.Chunk) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		acl, err := json.Marshal(document.ACL)
		if err != nil {
			return err
		}
		if err := tx.Exec(`INSERT INTO ai_knowledge_documents(id,knowledge_base_id,source_id,external_id,title,uri,version,content_hash,acl,status,chunk_count,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?) ON CONFLICT(id) DO UPDATE SET title=EXCLUDED.title,uri=EXCLUDED.uri,version=EXCLUDED.version,content_hash=EXCLUDED.content_hash,acl=EXCLUDED.acl,status=EXCLUDED.status,chunk_count=EXCLUDED.chunk_count,updated_at=EXCLUDED.updated_at`, document.ID, document.KnowledgeBaseID, document.SourceID, document.ExternalID, document.Title, document.URI, document.Version, document.ContentHash, string(acl), document.Status, document.ChunkCount, document.CreatedAt, document.UpdatedAt).Error; err != nil {
			return err
		}
		if err := tx.Exec(`DELETE FROM ai_knowledge_chunks WHERE document_id=?`, document.ID).Error; err != nil {
			return err
		}
		for _, chunk := range chunks {
			acl, location, err := marshal(chunk.ACL, chunk.Location)
			if err != nil {
				return err
			}
			if err := tx.Exec(`INSERT INTO ai_knowledge_chunks(id,knowledge_base_id,document_id,document_title,ordinal,content,content_hash,location,token_count,acl,created_at) VALUES(?,?,?,?,?,?,?,?,?,?,?)`, chunk.ID, chunk.KnowledgeBaseID, chunk.DocumentID, chunk.DocumentTitle, chunk.Ordinal, chunk.Content, chunk.ContentHash, location, chunk.TokenCount, acl, chunk.CreatedAt).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

func (r *Repository) ListAuthorizedChunks(ctx context.Context, p domainknowledge.PrincipalScope, request domainknowledge.SearchRequest, limit int) ([]domainknowledge.Chunk, error) {
	if len(request.KnowledgeBaseIDs) == 0 {
		return []domainknowledge.Chunk{}, nil
	}
	baseClause, baseArgs := accessClause("b", p)
	docClause, docArgs := accessClause("d", p)
	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(request.KnowledgeBaseIDs)), ",")
	query := `SELECT c.id,c.knowledge_base_id,c.document_id,c.document_title,c.ordinal,c.content,c.content_hash,c.location,c.token_count,c.acl,c.created_at FROM ai_knowledge_chunks c JOIN ai_knowledge_documents d ON d.id=c.document_id JOIN ai_knowledge_bases b ON b.id=c.knowledge_base_id WHERE c.knowledge_base_id IN (` + placeholders + `) AND d.status='indexed' AND b.status='active' AND b.deleted_at IS NULL AND (` + baseClause + `) AND (` + docClause + `)`
	args := make([]any, 0)
	for _, id := range request.KnowledgeBaseIDs {
		args = append(args, id)
	}
	args = append(args, baseArgs...)
	args = append(args, docArgs...)
	if len(request.Filters.SourceIDs) > 0 {
		query += ` AND d.source_id IN (` + strings.TrimSuffix(strings.Repeat("?,", len(request.Filters.SourceIDs)), ",") + `)`
		for _, id := range request.Filters.SourceIDs {
			args = append(args, id)
		}
	}
	if len(request.Filters.DocumentIDs) > 0 {
		query += ` AND d.id IN (` + strings.TrimSuffix(strings.Repeat("?,", len(request.Filters.DocumentIDs)), ",") + `)`
		for _, id := range request.Filters.DocumentIDs {
			args = append(args, id)
		}
	}
	query += ` ORDER BY c.created_at DESC LIMIT ?`
	args = append(args, limit)
	rows, err := r.db.WithContext(ctx).Raw(query, args...).Rows()
	if err != nil {
		return nil, fmt.Errorf("list authorized knowledge chunks: %w", err)
	}
	defer func() { _ = rows.Close() }()
	items := []domainknowledge.Chunk{}
	for rows.Next() {
		item, err := scanChunk(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *Repository) CreateSyncRun(ctx context.Context, item domainknowledge.SyncRun) error {
	return r.db.WithContext(ctx).Exec(`INSERT INTO ai_knowledge_sync_runs(id,knowledge_base_id,source_id,status,documents_seen,documents_stored,chunks_stored,error,started_at,completed_at)VALUES(?,?,?,?,?,?,?,?,?,?)`, item.ID, item.KnowledgeBaseID, item.SourceID, item.Status, item.DocumentsSeen, item.DocumentsStored, item.ChunksStored, item.Error, item.StartedAt, item.CompletedAt).Error
}
func (r *Repository) UpdateSyncRun(ctx context.Context, item domainknowledge.SyncRun) error {
	return r.db.WithContext(ctx).Exec(`UPDATE ai_knowledge_sync_runs SET status=?,documents_seen=?,documents_stored=?,chunks_stored=?,error=?,completed_at=? WHERE id=?`, item.Status, item.DocumentsSeen, item.DocumentsStored, item.ChunksStored, item.Error, item.CompletedAt, item.ID).Error
}
func (r *Repository) ListSyncRuns(ctx context.Context, p domainknowledge.PrincipalScope, baseID string, limit int) ([]domainknowledge.SyncRun, error) {
	if _, err := r.GetBase(ctx, p, baseID); err != nil {
		return nil, err
	}
	rows, err := r.db.WithContext(ctx).Raw(`SELECT id,knowledge_base_id,source_id,status,documents_seen,documents_stored,chunks_stored,error,started_at,completed_at FROM ai_knowledge_sync_runs WHERE knowledge_base_id=? ORDER BY started_at DESC LIMIT ?`, baseID, limit).Rows()
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	items := []domainknowledge.SyncRun{}
	for rows.Next() {
		var item domainknowledge.SyncRun
		if err := rows.Scan(&item.ID, &item.KnowledgeBaseID, &item.SourceID, &item.Status, &item.DocumentsSeen, &item.DocumentsStored, &item.ChunksStored, &item.Error, &item.StartedAt, &item.CompletedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}
func (r *Repository) CreateIndexRevision(ctx context.Context, item domainknowledge.IndexRevision) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec(`UPDATE ai_knowledge_index_revisions SET status='superseded' WHERE knowledge_base_id=? AND status='active'`, item.KnowledgeBaseID).Error; err != nil {
			return err
		}
		return tx.Exec(`INSERT INTO ai_knowledge_index_revisions(id,knowledge_base_id,revision,embedding_model,chunker_version,document_count,chunk_count,status,created_at,activated_at)VALUES(?,?,?,?,?,?,?,?,?,?)`, item.ID, item.KnowledgeBaseID, item.Revision, item.EmbeddingModel, item.ChunkerVersion, item.DocumentCount, item.ChunkCount, item.Status, item.CreatedAt, item.ActivatedAt).Error
	})
}
func (r *Repository) ListIndexRevisions(ctx context.Context, p domainknowledge.PrincipalScope, baseID string, limit int) ([]domainknowledge.IndexRevision, error) {
	if _, err := r.GetBase(ctx, p, baseID); err != nil {
		return nil, err
	}
	rows, err := r.db.WithContext(ctx).Raw(`SELECT id,knowledge_base_id,revision,embedding_model,chunker_version,document_count,chunk_count,status,created_at,activated_at FROM ai_knowledge_index_revisions WHERE knowledge_base_id=? ORDER BY revision DESC LIMIT ?`, baseID, limit).Rows()
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	items := []domainknowledge.IndexRevision{}
	for rows.Next() {
		var item domainknowledge.IndexRevision
		if err := rows.Scan(&item.ID, &item.KnowledgeBaseID, &item.Revision, &item.EmbeddingModel, &item.ChunkerVersion, &item.DocumentCount, &item.ChunkCount, &item.Status, &item.CreatedAt, &item.ActivatedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

type rowScanner interface{ Scan(...any) error }

func scanBase(row rowScanner) (domainknowledge.KnowledgeBase, error) {
	var item domainknowledge.KnowledgeBase
	var scope, policy []byte
	err := row.Scan(&item.ID, &item.TenantID, &item.WorkspaceID, &item.Name, &item.Description, &item.Status, &item.OwnerID, &scope, &policy, &item.CreatedAt, &item.UpdatedAt)
	if err != nil {
		return item, err
	}
	if err := unmarshal(scope, &item.Scope); err != nil {
		return item, err
	}
	if err := unmarshal(policy, &item.RetrievalPolicy); err != nil {
		return item, err
	}
	return item, nil
}
func scanSource(row rowScanner) (domainknowledge.Source, error) {
	var item domainknowledge.Source
	var config, policy []byte
	err := row.Scan(&item.ID, &item.KnowledgeBaseID, &item.Name, &item.Kind, &item.ConfigRef, &config, &policy, &item.Cursor, &item.Status, &item.LastError, &item.LastSyncedAt, &item.CreatedAt, &item.UpdatedAt)
	if err != nil {
		return item, err
	}
	if err := unmarshal(config, &item.Config); err != nil {
		return item, err
	}
	if err := unmarshal(policy, &item.SyncPolicy); err != nil {
		return item, err
	}
	return item, nil
}
func scanDocument(row rowScanner) (domainknowledge.Document, error) {
	var item domainknowledge.Document
	var acl []byte
	err := row.Scan(&item.ID, &item.KnowledgeBaseID, &item.SourceID, &item.ExternalID, &item.Title, &item.URI, &item.Version, &item.ContentHash, &acl, &item.Status, &item.ChunkCount, &item.CreatedAt, &item.UpdatedAt)
	if err != nil {
		return item, err
	}
	if err := unmarshal(acl, &item.ACL); err != nil {
		return item, err
	}
	return item, nil
}
func scanChunk(row rowScanner) (domainknowledge.Chunk, error) {
	var item domainknowledge.Chunk
	var location, acl []byte
	err := row.Scan(&item.ID, &item.KnowledgeBaseID, &item.DocumentID, &item.DocumentTitle, &item.Ordinal, &item.Content, &item.ContentHash, &location, &item.TokenCount, &acl, &item.CreatedAt)
	if err != nil {
		return item, err
	}
	if err := unmarshal(location, &item.Location); err != nil {
		return item, err
	}
	if err := unmarshal(acl, &item.ACL); err != nil {
		return item, err
	}
	return item, nil
}
func marshal(values ...any) (string, string, error) {
	if len(values) != 2 {
		return "", "", fmt.Errorf("marshal expects two values")
	}
	a, err := json.Marshal(values[0])
	if err != nil {
		return "", "", err
	}
	b, err := json.Marshal(values[1])
	return string(a), string(b), err
}
func unmarshal(data []byte, target any) error {
	if len(data) == 0 {
		return nil
	}
	return json.Unmarshal(data, target)
}

// accessClause is deliberately rendered into SQL so unauthorized chunk content is
// never loaded into application memory before filtering.
func accessClause(alias string, p domainknowledge.PrincipalScope) (string, []any) {
	prefix := alias + "."
	jsonColumn := "scope"
	clauses := []string{}
	args := []any{}
	if alias != "d" {
		clauses = append(clauses, prefix+"owner_id = ?")
		args = append(args, p.UserID)
	} else {
		jsonColumn = "acl"
	}
	jsonExpr := prefix + jsonColumn
	clauses = append(clauses, jsonExpr+"->>'visibility' = 'public'", `jsonb_exists(`+jsonExpr+`->'users', ?)`)
	args = append(args, p.UserID)
	add := func(field string, values []string) {
		for _, value := range values {
			if strings.TrimSpace(value) == "" {
				continue
			}
			clauses = append(clauses, `jsonb_exists(`+jsonExpr+`->'`+field+`', ?)`)
			args = append(args, value)
		}
	}
	add("roles", p.Roles)
	add("teams", p.Teams)
	add("projects", p.Projects)
	return strings.Join(clauses, " OR "), args
}
