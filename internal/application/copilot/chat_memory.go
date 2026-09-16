package copilot

import (
	"context"
	"fmt"
	"slices"
	"time"

	appaccess "github.com/opensoha/soha/internal/application/access"
	appmemory "github.com/opensoha/soha/internal/application/memory"
	domaincopilot "github.com/opensoha/soha/internal/domain/copilot"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/apperrors"
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
		policy, err := s.chatMemory.GetPolicy(ctx, record.PolicyID, record.PolicyVer)
		if err != nil || !policy.Enabled || !policy.ExplicitWriteOnly || !slices.Contains(policy.OwnerTypes, "user") {
			return fmt.Errorf("%w: memory policy no longer permits this reference", apperrors.ErrAccessDenied)
		}
		appendChatEvidence(envelope, "个人记忆", "memory:"+record.ID, record.Fact)
	}
	return nil
}

func chatMemoryAvailable(record appmemory.Record, owner string, now time.Time) bool {
	return record.OwnerType == "user" && record.OwnerID == owner && record.Status == "active" && record.SourceType == "explicit_user" && record.DeletedAt == nil && !record.ValidFrom.After(now) && record.ExpiresAt != nil && record.ExpiresAt.After(now)
}
