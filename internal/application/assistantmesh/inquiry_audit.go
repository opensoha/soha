package assistantmesh

import (
	"context"
	"errors"
	"time"

	appaccess "github.com/opensoha/soha/internal/application/access"
	domain "github.com/opensoha/soha/internal/domain/assistantmesh"
	domainaudit "github.com/opensoha/soha/internal/domain/audit"
	identity "github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"github.com/opensoha/soha/internal/platform/requestctx"
)

type InquiryAuditStore interface {
	ReadInquiryForAudit(context.Context, string, func(context.Context, domain.Inquiry) error) error
}

type InquiryAudit struct {
	assets *Service
	store  InquiryAuditStore
}

func NewInquiryAudit(assets *Service, store InquiryAuditStore) *InquiryAudit {
	return &InquiryAudit{assets: assets, store: store}
}

type InquiryAuditInput struct {
	InquiryID string `json:"inquiryId"`
	Reason    string `json:"reason"`
}

type InquiryAuditView struct {
	ID         string     `json:"id"`
	CallerID   string     `json:"callerId"`
	OwnerID    string     `json:"ownerId"`
	Status     string     `json:"status"`
	Question   string     `json:"question"`
	Answer     string     `json:"answer"`
	CreatedAt  time.Time  `json:"createdAt"`
	PurgeAfter *time.Time `json:"purgeAfter,omitempty"`
}

func (s *InquiryAudit) Read(ctx context.Context, actor identity.Principal, input InquiryAuditInput) (InquiryAuditView, error) {
	if err := s.authorize(ctx, actor); err != nil {
		return InquiryAuditView{}, err
	}
	if !validReceptionID(input.InquiryID) {
		return InquiryAuditView{}, apperrors.ErrInvalidArgument
	}
	reason, err := cleanReceptionText(ctx, input.Reason, 500, false)
	if errors.Is(err, errSensitiveInquiry) {
		return InquiryAuditView{}, apperrors.ErrInvalidArgument
	}
	if err != nil {
		return InquiryAuditView{}, err
	}
	policy, err := s.assets.InquiryPolicy(ctx)
	if err != nil {
		return InquiryAuditView{}, err
	}
	var view InquiryAuditView
	err = s.store.ReadInquiryForAudit(ctx, input.InquiryID, func(tx context.Context, item domain.Inquiry) error {
		now := nowUTC()
		if !inquiryBodyRetained(item, now, policy.Limits.Retention) {
			return apperrors.ErrGone
		}
		if err := s.record(tx, actor, input.InquiryID, reason); err != nil {
			return err
		}
		view = InquiryAuditView{
			ID: item.ID, CallerID: item.CallerID, OwnerID: item.OwnerID, Status: string(item.Status),
			Question: item.Question, Answer: item.Answer, CreatedAt: item.CreatedAt, PurgeAfter: item.PurgeAfter,
		}
		return nil
	})
	if err != nil {
		return InquiryAuditView{}, err
	}
	return view, nil
}

func (s *InquiryAudit) authorize(ctx context.Context, actor identity.Principal) error {
	if actor.AccessTokenID != "" || actor.GovernedAI != nil {
		return apperrors.ErrAccessDenied
	}
	if err := s.assets.requireActive(ctx, actor.UserID); err != nil {
		return err
	}
	if s.assets.auditor == nil {
		return apperrors.ErrServiceUnavailable
	}
	return appaccess.AuthorizeRuntimePermission(ctx, s.assets.permissions, actor, appaccess.PermSystemAuditView)
}

func inquiryBodyRetained(item domain.Inquiry, now time.Time, retention time.Duration) bool {
	if item.Question == "" && item.Answer == "" {
		return false
	}
	if item.PurgeAfter != nil {
		return now.Before(*item.PurgeAfter)
	}
	// The janitor need not have run yet for an expired question to be hidden.
	return now.Before(item.DueAt.Add(retention))
}

func (s *InquiryAudit) record(ctx context.Context, actor identity.Principal, inquiryID, reason string) error {
	meta := requestctx.FromContext(ctx)
	return s.assets.auditor.Record(ctx, domainaudit.Entry{
		ActorID: actor.UserID, ActorName: actor.UserName, Roles: actor.Roles, Teams: actor.Teams,
		ResourceKind: "AssistantMeshInquiry", ResourceName: inquiryID, Action: "assistant_mesh.inquiry.audit_read",
		Result: "success", Summary: "Read retained Heya inquiry content",
		RequestPath: meta.Path, RequestMethod: meta.Method, RequestID: meta.RequestID, SourceIP: meta.SourceIP,
		Metadata: map[string]any{"reason": reason},
	})
}
