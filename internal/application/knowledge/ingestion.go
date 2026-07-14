package knowledge

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainknowledge "github.com/opensoha/soha/internal/domain/knowledge"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

func (s *Service) SyncSource(ctx context.Context, principal domainidentity.Principal, baseID, sourceID string) (domainknowledge.SyncRun, error) {
	if err := s.authorize(ctx, principal, PermKnowledgeManage); err != nil {
		return domainknowledge.SyncRun{}, err
	}
	if s.loader == nil {
		return domainknowledge.SyncRun{}, fmt.Errorf("%w: source loader is not configured", apperrors.ErrUnsupportedOperation)
	}
	source, err := s.repo.GetSource(ctx, principalScope(principal), strings.TrimSpace(baseID), strings.TrimSpace(sourceID))
	if err != nil {
		return domainknowledge.SyncRun{}, err
	}
	now := s.now()
	run := domainknowledge.SyncRun{ID: uuid.NewString(), KnowledgeBaseID: source.KnowledgeBaseID, SourceID: source.ID, Status: domainknowledge.RunStatusRunning, StartedAt: now}
	if err := s.repo.CreateSyncRun(ctx, run); err != nil {
		return domainknowledge.SyncRun{}, err
	}
	source.Status, source.UpdatedAt = domainknowledge.SourceStatusSyncing, now
	_ = s.repo.UpdateSource(ctx, principalScope(principal), source)
	documents, cursor, loadErr := s.loader.Load(ctx, principal, source)
	if loadErr != nil {
		return s.failSync(ctx, principal, source, run, loadErr)
	}
	if len(documents) > 10000 {
		return s.failSync(ctx, principal, source, run, fmt.Errorf("%w: source returned too many documents", domainknowledge.ErrRetrievalExhausted))
	}
	for _, loaded := range documents {
		if err := ctx.Err(); err != nil {
			return s.failSync(ctx, principal, source, run, err)
		}
		loaded.Content = strings.TrimSpace(loaded.Content)
		if loaded.ExternalID == "" || loaded.Content == "" {
			continue
		}
		documentNow := s.now()
		document := domainknowledge.Document{ID: stableDocumentID(source.ID, loaded.ExternalID), KnowledgeBaseID: source.KnowledgeBaseID, SourceID: source.ID, ExternalID: loaded.ExternalID, Title: strings.TrimSpace(loaded.Title), URI: strings.TrimSpace(loaded.URI), Version: firstNonEmpty(loaded.Version, contentHash(loaded.Content)), ContentHash: contentHash(loaded.Content), ACL: normalizeAccessScope(loaded.ACL, principal.UserID), Status: domainknowledge.DocumentStatusIndexed, CreatedAt: documentNow, UpdatedAt: documentNow}
		if document.Title == "" {
			document.Title = document.ExternalID
		}
		chunks := chunkDocument(document, loaded.Content)
		document.ChunkCount = len(chunks)
		if err := s.repo.UpsertDocument(ctx, document, chunks); err != nil {
			return s.failSync(ctx, principal, source, run, err)
		}
		run.DocumentsSeen++
		run.DocumentsStored++
		run.ChunksStored += len(chunks)
	}
	completed := s.now()
	run.Status, run.CompletedAt = domainknowledge.RunStatusSucceeded, &completed
	if err := s.repo.UpdateSyncRun(ctx, run); err != nil {
		return domainknowledge.SyncRun{}, err
	}
	source.Status, source.Cursor, source.LastError, source.LastSyncedAt, source.UpdatedAt = domainknowledge.SourceStatusReady, cursor, "", &completed, completed
	if err := s.repo.UpdateSource(ctx, principalScope(principal), source); err != nil {
		return domainknowledge.SyncRun{}, err
	}
	revision := domainknowledge.IndexRevision{ID: uuid.NewString(), KnowledgeBaseID: source.KnowledgeBaseID, Revision: int(completed.UnixNano()), ChunkerVersion: "rune-v1-1200-150", DocumentCount: run.DocumentsStored, ChunkCount: run.ChunksStored, Status: domainknowledge.IndexStatusActive, CreatedAt: completed, ActivatedAt: &completed}
	if err := s.repo.CreateIndexRevision(ctx, revision); err != nil {
		return domainknowledge.SyncRun{}, err
	}
	s.record(ctx, principal, "knowledge.source.sync", source.ID, source.Name, nil)
	return run, nil
}

func (s *Service) failSync(ctx context.Context, principal domainidentity.Principal, source domainknowledge.Source, run domainknowledge.SyncRun, cause error) (domainknowledge.SyncRun, error) {
	completed := s.now()
	run.Status, run.Error, run.CompletedAt = domainknowledge.RunStatusFailed, cause.Error(), &completed
	_ = s.repo.UpdateSyncRun(ctx, run)
	source.Status, source.LastError, source.UpdatedAt = domainknowledge.SourceStatusFailed, cause.Error(), completed
	_ = s.repo.UpdateSource(ctx, principalScope(principal), source)
	s.record(ctx, principal, "knowledge.source.sync", source.ID, source.Name, cause)
	return run, cause
}

func stableDocumentID(sourceID, externalID string) string {
	return uuid.NewSHA1(uuid.NameSpaceURL, []byte(sourceID+"\x00"+externalID)).String()
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
