package assistantmesh

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	domain "github.com/opensoha/soha/internal/domain/assistantmesh"
	identity "github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type InquiryInput struct {
	OwnerID   string `json:"ownerId"`
	RequestID string `json:"requestId"`
	Question  string `json:"question"`
}

func (s *Reception) Ask(ctx context.Context, p identity.Principal, input InquiryInput) (InquiryView, error) {
	caller, err := s.prepare(ctx, p)
	if err != nil {
		return InquiryView{}, err
	}
	if !validReceptionID(input.OwnerID) || !validReceptionID(input.RequestID) || caller.UserID == input.OwnerID {
		return InquiryView{}, apperrors.ErrInvalidArgument
	}
	question, err := cleanReceptionText(ctx, input.Question, 2000, false)
	if err != nil {
		return InquiryView{}, err
	}
	policy, err := s.assets.InquiryPolicy(ctx)
	if err != nil {
		return InquiryView{}, err
	}
	now := nowUTC()
	item := domain.Inquiry{
		ID: uuid.NewString(), CallerID: caller.UserID, OwnerID: input.OwnerID, RequestID: input.RequestID,
		QuestionKey: inquiryQuestionKey(question), Question: question, Status: domain.InquiryPending,
		CreatedAt: now, DueAt: now.Add(policy.Timeout),
	}
	previous, err := s.store.GetInquiry(ctx, item.CallerID, item.RequestID)
	if err == nil {
		if previous.OwnerID != item.OwnerID || previous.QuestionKey != item.QuestionKey {
			return InquiryView{}, apperrors.ErrConflict
		}
		return s.inquiryView(ctx, caller, previous)
	}
	if !errors.Is(err, apperrors.ErrNotFound) {
		return InquiryView{}, err
	}
	return s.createInquiry(ctx, caller, item, policy.Limits)
}

func inquiryQuestionKey(question string) string {
	normalized := strings.ToLower(strings.Join(strings.Fields(question), " "))
	digest := sha256.Sum256([]byte(normalized))
	return hex.EncodeToString(digest[:])
}

func (s *Reception) createInquiry(ctx context.Context, caller identity.Principal, item domain.Inquiry, limits domain.InquiryLimits) (InquiryView, error) {
	if _, err := s.openOwner(ctx, item.OwnerID); err != nil {
		return InquiryView{}, err
	}
	facts, err := s.facts(ctx, caller, item.OwnerID)
	if err != nil {
		return InquiryView{}, err
	}
	if len(facts) == 0 {
		return InquiryView{}, apperrors.ErrNotFound
	}
	fact, matched := bestReceptionFact(item.Question, facts)
	if s.model != nil && matched {
		item.Status = domain.InquiryProcessing
	}
	result, err := s.store.CreateInquiry(ctx, item, limits)
	if err != nil {
		return InquiryView{}, err
	}
	if result.ID == item.ID {
		_ = s.assets.audit(ctx, caller, "inquiry.create", item.ID, "success", map[string]any{"ownerId": item.OwnerID})
		if item.Status == domain.InquiryProcessing {
			result, err = s.answerInquiry(ctx, caller, result, fact)
			if err != nil {
				return InquiryView{}, err
			}
		}
	}
	return s.inquiryView(ctx, caller, result)
}

func (s *Reception) answerInquiry(ctx context.Context, caller identity.Principal, item domain.Inquiry, fact ReceptionFact) (domain.Inquiry, error) {
	current, err := s.recheckReception(ctx, caller, item.OwnerID)
	if err != nil {
		return domain.Inquiry{}, err
	}
	latest, err := s.store.GetInquiry(ctx, item.CallerID, item.RequestID)
	if err != nil || latest.Status != domain.InquiryProcessing {
		return latest, err
	}
	answer, err := s.tryReceptionAnswer(ctx, current, item, fact)
	if err != nil {
		return domain.Inquiry{}, err
	}
	policy, err := s.assets.InquiryPolicy(ctx)
	if err != nil {
		return domain.Inquiry{}, err
	}
	if answer == "" {
		return s.store.FallbackInquiry(ctx, item, policy.Limits.OwnerPending, nowUTC(), policy.Limits.Retention)
	}
	return s.completeReception(ctx, current, item, answer, fact.Citation, policy.Limits.Retention)
}

func (s *Reception) recheckReception(ctx context.Context, caller identity.Principal, ownerID string) (identity.Principal, error) {
	current, err := s.prepare(ctx, caller)
	if err != nil {
		return identity.Principal{}, err
	}
	_, err = s.openOwner(ctx, ownerID)
	return current, err
}

func (s *Reception) tryReceptionAnswer(ctx context.Context, caller identity.Principal, item domain.Inquiry, fact ReceptionFact) (string, error) {
	visible, err := s.citationVisible(ctx, caller, item.OwnerID, fact.Citation)
	if err != nil || !visible {
		return "", err
	}
	answer, err := s.model.AnswerReception(ctx, caller, item.Question, fact)
	if err != nil || answer == "" {
		return "", nil
	}
	current, err := s.recheckReception(ctx, caller, item.OwnerID)
	if err != nil {
		return "", err
	}
	visible, err = s.citationVisible(ctx, current, item.OwnerID, fact.Citation)
	if err != nil || !visible {
		return "", err
	}
	return answer, nil
}

func (s *Reception) completeReception(ctx context.Context, caller identity.Principal, item domain.Inquiry, answer string, citation domain.Citation, bodyRetention time.Duration) (domain.Inquiry, error) {
	now := nowUTC()
	purge := now.Add(bodyRetention)
	item.Status, item.Answer, item.Citations = domain.InquiryAnswered, answer, []domain.Citation{citation}
	item.ClosedAt, item.PurgeAfter = &now, &purge
	if err := s.store.CompleteInquiry(ctx, item); err != nil {
		if errors.Is(err, apperrors.ErrConflict) {
			return s.store.GetInquiry(ctx, item.CallerID, item.RequestID)
		}
		return domain.Inquiry{}, err
	}
	_ = s.assets.audit(ctx, caller, "inquiry.answer", item.ID, "success", nil)
	return item, nil
}

func (s *Reception) inquiryView(ctx context.Context, p identity.Principal, item domain.Inquiry) (InquiryView, error) {
	view := InquiryView{Inquiry: item}
	if p.UserID != item.CallerID && p.UserID != item.OwnerID {
		return InquiryView{}, apperrors.ErrNotFound
	}
	view.CallerName = s.participantName(ctx, item.CallerID)
	view.OwnerName = s.participantName(ctx, item.OwnerID)
	if item.PurgeAfter != nil && !nowUTC().Before(*item.PurgeAfter) {
		view.Question, view.Answer, view.Citations = "", "", nil
	}
	if item.Status == domain.InquiryAnswered && view.Answer != "" {
		if err := s.filterStoredAnswer(ctx, &view); err != nil {
			return InquiryView{}, err
		}
	}
	return view, nil
}

func (s *Reception) participantName(ctx context.Context, userID string) string {
	p, err := s.identities.CurrentPrincipal(ctx, userID)
	if err != nil || p.UserName == "" {
		return "员工"
	}
	return p.UserName
}

// A stored generated answer has exactly the same visibility as its source,
// including after a caller loses team/project access or the version changes.
func (s *Reception) filterStoredAnswer(ctx context.Context, view *InquiryView) error {
	caller, err := s.identities.CurrentPrincipal(ctx, view.CallerID)
	if errors.Is(err, apperrors.ErrUnauthorized) || errors.Is(err, apperrors.ErrNotFound) {
		view.Answer, view.Citations = "", nil
		return nil
	}
	if err != nil {
		return err
	}
	if len(view.Citations) == 1 {
		visible, err := s.citationVisible(ctx, caller, view.OwnerID, view.Citations[0])
		if err != nil {
			return err
		}
		if visible {
			_, err = s.openOwner(ctx, view.OwnerID)
			if err == nil {
				return nil
			}
			if !errors.Is(err, apperrors.ErrNotFound) {
				return err
			}
		}
	}
	view.Answer, view.Citations = "", nil
	return nil
}

func (s *Reception) ListInquiries(ctx context.Context, p identity.Principal, direction string) ([]InquiryView, error) {
	p, err := s.prepare(ctx, p)
	if err != nil {
		return nil, err
	}
	if direction != "sent" && direction != "inbox" {
		return nil, fmt.Errorf("%w: unsupported inquiry direction", apperrors.ErrInvalidArgument)
	}
	policy, err := s.assets.InquiryPolicy(ctx)
	if err != nil {
		return nil, err
	}
	items, err := s.store.ListInquiries(ctx, p.UserID, direction == "inbox", nowUTC(), policy.Limits.Retention)
	if err != nil {
		return nil, err
	}
	results := make([]InquiryView, 0, len(items))
	for _, item := range items {
		view, err := s.inquiryView(ctx, p, item)
		if err != nil {
			return nil, err
		}
		results = append(results, view)
	}
	return results, nil
}

func (s *Reception) Reply(ctx context.Context, p identity.Principal, inquiryID, text string) (InquiryView, error) {
	p, err := s.prepare(ctx, p)
	if err != nil {
		return InquiryView{}, err
	}
	if !validReceptionID(inquiryID) {
		return InquiryView{}, apperrors.ErrInvalidArgument
	}
	answer, err := cleanReceptionText(ctx, text, 4000, false)
	if err != nil {
		return InquiryView{}, err
	}
	policy, err := s.assets.InquiryPolicy(ctx)
	if err != nil {
		return InquiryView{}, err
	}
	item, err := s.store.ReplyInquiry(ctx, p.UserID, inquiryID, answer, nowUTC(), policy.Limits.Retention)
	if err != nil {
		return InquiryView{}, err
	}
	_ = s.assets.audit(ctx, p, "inquiry.reply", inquiryID, "success", nil)
	return s.inquiryView(ctx, p, item)
}
