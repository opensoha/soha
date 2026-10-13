package copilot

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/url"
	"slices"
	"strings"
	"sync/atomic"
	"time"

	appaccess "github.com/opensoha/soha/internal/application/access"
	appmemory "github.com/opensoha/soha/internal/application/memory"
	domaincopilot "github.com/opensoha/soha/internal/domain/copilot"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainknowledge "github.com/opensoha/soha/internal/domain/knowledge"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"github.com/opensoha/soha/internal/platform/redaction"
)

type chatMemoryReader interface {
	ListRecords(context.Context, string, string) ([]appmemory.Record, error)
	GetPolicy(context.Context, string, string) (appmemory.Policy, error)
}

func (s *Service) SetChatMemoryReader(reader chatMemoryReader) { s.chatMemory = reader }
func (s *Service) appendChatMemory(ctx context.Context, principal domainidentity.Principal, envelope *domaincopilot.ContextEnvelope, ids []string) error {
	if len(ids) == 0 {
		return nil
	}
	ctx = boundedMemoryLineage(ctx)
	if len(ids) > 20 || len(normalizeStringList(ids)) != len(ids) {
		return fmt.Errorf("%w: select at most 20 distinct memories", apperrors.ErrInvalidArgument)
	}
	if s.chatMemory == nil {
		return fmt.Errorf("%w: explicit memory is disabled", apperrors.ErrUnsupportedOperation)
	}
	if err := s.authorizePrincipal(ctx, principal, appaccess.PermAIMemoryView); err != nil {
		return err
	}
	records, err := s.chatMemory.ListRecords(ctx, "user", principal.UserID)
	if err != nil {
		return err
	}
	now := time.Now()
	for _, id := range ids {
		index := slices.IndexFunc(records, func(record appmemory.Record) bool {
			return record.ID == id && chatMemoryAvailable(record, principal.UserID, now)
		})
		if index < 0 {
			return fmt.Errorf("%w: selected memory is unavailable or expired", apperrors.ErrNotFound)
		}
		record := records[index]
		lineage, err := s.authorizeChatMemoryRecord(ctx, principal, record)
		if err != nil {
			return err
		}
		before := len(envelope.Citations)
		appendChatEvidence(envelope, "个人记忆", "memory:"+record.ID, record.Fact)
		if len(envelope.Citations) > before {
			envelope.Citations[len(envelope.Citations)-1].ContentHash = chatMemoryContentHash(record)
			envelope.Citations = append(envelope.Citations, lineage...)
		}
	}
	return nil
}

type memoryLineageDepthKey struct{}
type memoryLineageBudgetKey struct{}

func boundedMemoryLineage(ctx context.Context) context.Context {
	if ctx.Value(memoryLineageBudgetKey{}) != nil {
		return ctx
	}
	remaining := &atomic.Int64{}
	remaining.Store(200)
	return context.WithValue(ctx, memoryLineageBudgetKey{}, remaining)
}

func (s *Service) authorizeChatMemoryRecord(ctx context.Context, principal domainidentity.Principal, record appmemory.Record) ([]domainknowledge.Citation, error) {
	ctx = boundedMemoryLineage(ctx)
	if len(record.SourceRefs) > 32 {
		return nil, apperrors.ErrAccessDenied
	}
	policy, err := s.chatMemory.GetPolicy(ctx, record.PolicyID, record.PolicyVer)
	if err != nil || !policy.Enabled || !policy.ExplicitWriteOnly || !slices.Contains(policy.OwnerTypes, "user") {
		return nil, fmt.Errorf("%w: memory policy no longer permits this reference", apperrors.ErrAccessDenied)
	}
	depth, _ := ctx.Value(memoryLineageDepthKey{}).(int)
	if depth >= 4 {
		return nil, fmt.Errorf("%w: memory source chain exceeds four levels", apperrors.ErrAccessDenied)
	}
	ctx = context.WithValue(ctx, memoryLineageDepthKey{}, depth+1)
	remaining, ok := ctx.Value(memoryLineageBudgetKey{}).(*atomic.Int64)
	if !ok || remaining == nil {
		return nil, apperrors.ErrAccessDenied
	}
	lineage := []domainknowledge.Citation{}
	for _, source := range record.SourceRefs {
		if remaining.Add(-1) < 0 {
			return nil, fmt.Errorf("%w: memory source chain exceeds 200 receipts", apperrors.ErrAccessDenied)
		}
		ref, err := chatMemorySourceReference(source)
		if err != nil {
			return nil, err
		}
		if _, _, _, err := s.readChatSessionReference(ctx, principal, ref, &lineage); err != nil {
			return nil, err
		}
	}
	return lineage, nil
}

func chatMemorySourceReference(source string) (domaincopilot.ContextReference, error) {
	u, err := url.Parse(source)
	if err != nil || u.Scheme != "soha" || u.Host != "session" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || len(source) > 2048 {
		return domaincopilot.ContextReference{}, fmt.Errorf("%w: memory source requires an owned message receipt URI", apperrors.ErrAccessDenied)
	}
	parts := strings.Split(strings.TrimPrefix(u.Path, "/"), "/")
	if len(parts) != 3 || parts[0] == "" || parts[1] != "message" || parts[2] == "" {
		return domaincopilot.ContextReference{}, fmt.Errorf("%w: invalid memory source receipt URI", apperrors.ErrAccessDenied)
	}
	return domaincopilot.ContextReference{Kind: "session", SessionID: parts[0], MessageIDs: []string{parts[2]}}, nil
}

func chatMemoryContentHash(record appmemory.Record) string {
	hash := sha256.Sum256([]byte(redaction.Text(record.Fact)))
	return hex.EncodeToString(hash[:])
}

func chatMemoryAvailable(record appmemory.Record, owner string, now time.Time) bool {
	return record.OwnerType == "user" && record.OwnerID == owner && record.Status == "active" && record.SourceType == "explicit_user" && record.DeletedAt == nil && !record.ValidFrom.After(now) && record.ExpiresAt != nil && record.ExpiresAt.After(now)
}
