package assistantmesh

import (
	"context"
	"testing"
	"time"

	domain "github.com/opensoha/soha/internal/domain/assistantmesh"
	identity "github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/domain/knowledge"
)

type policyReceptionStore struct {
	*receptionTestStore
	limits domain.InquiryLimits
}

func (s *policyReceptionStore) CreateInquiry(ctx context.Context, item domain.Inquiry, limits domain.InquiryLimits) (domain.Inquiry, error) {
	s.limits = limits
	return s.receptionTestStore.CreateInquiry(ctx, item, limits)
}

func (s *policyReceptionStore) ReplyInquiry(_ context.Context, owner, id, answer string, now time.Time, retention time.Duration) (domain.Inquiry, error) {
	for key, item := range s.inquiries {
		if item.ID == id && item.OwnerID == owner {
			purge := now.Add(retention)
			item.Answer, item.Status, item.ClosedAt, item.PurgeAfter = answer, domain.InquiryReplied, &now, &purge
			s.inquiries[key] = item
			return item, nil
		}
	}
	panic("test expected the original inquiry")
}

func TestInquiryGovernanceSnapshotAppliesDeadlineAtCreationAndRetentionAtClosure(t *testing.T) {
	f := newFixture(t)
	settings := f.service.config.(*fakeSettings)
	settings.value.InquiryPairLimitPerHour = 8
	settings.value.InquiryCallerLimitPerDay = 100
	settings.value.InquiryMergeWindowHours = 72
	settings.value.InquiryPendingLimit = 12
	settings.value.InquiryTimeoutHours = 36
	settings.value.InquiryRetentionDays = 9

	caller, owner := employee(receptionCaller), employee(receptionOwner)
	for _, p := range []identity.Principal{caller, owner} {
		f.service.users.(*fakeUsers).users[p.UserID] = identity.User{ID: p.UserID, Status: "active"}
	}
	store := &policyReceptionStore{receptionTestStore: &receptionTestStore{
		profile:   domain.Profile{OwnerID: owner.UserID, Open: true},
		inquiries: map[string]domain.Inquiry{},
	}}
	publishReceptionFact(t, f, "topic", "共享发布", "已发布经验", knowledge.AccessScope{Visibility: "organization"}, nil)
	service := NewReception(f.service, store, receptionTestIdentities{caller.UserID: caller, owner.UserID: owner}, receptionTestAccess{}, nil)
	item, err := service.Ask(t.Context(), caller, InquiryInput{OwnerID: owner.UserID, RequestID: receptionReq, Question: "一个尚无匹配的问题"})
	want := domain.InquiryLimits{PairPerHour: 8, CallerPerDay: 100, OwnerPending: 12, MergeWindow: 72 * time.Hour, Retention: 9 * 24 * time.Hour}
	if err != nil || store.limits != want || item.DueAt.Sub(item.CreatedAt) != 36*time.Hour {
		t.Fatalf("question did not use configured policy: item=%+v limits=%+v error=%v", item, store.limits, err)
	}
	settings.value.InquiryTimeoutHours = 5
	settings.value.InquiryRetentionDays = 12
	replied, err := service.Reply(t.Context(), owner, item.ID, "只答复原提问人")
	if err != nil || replied.ClosedAt == nil || replied.PurgeAfter == nil ||
		replied.PurgeAfter.Sub(*replied.ClosedAt) != 12*24*time.Hour || !replied.DueAt.Equal(item.DueAt) {
		t.Fatalf("closure did not retain configured deadline semantics: %+v, %v", replied, err)
	}
	profile, err := service.Profile(t.Context(), owner)
	if err != nil || profile.RetentionDays != 12 {
		t.Fatalf("profile did not advertise current retention: %+v, %v", profile, err)
	}
}
