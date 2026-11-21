package assistantmesh

import (
	"context"
	"errors"
	"testing"
	"time"

	appaccess "github.com/opensoha/soha/internal/application/access"
	domain "github.com/opensoha/soha/internal/domain/assistantmesh"
	domainaudit "github.com/opensoha/soha/internal/domain/audit"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type auditInquiryStub struct {
	item  domain.Inquiry
	calls int
}

func (s *auditInquiryStub) ReadInquiryForAudit(ctx context.Context, _ string, record func(context.Context, domain.Inquiry) error) error {
	s.calls++
	return record(ctx, s.item)
}

type inquiryAuditRoles struct{}

func (inquiryAuditRoles) ListRolePermissions(context.Context) (map[string][]string, error) {
	return map[string][]string{"admin": {appaccess.PermSystemAuditView}}, nil
}

type rejectingInquiryAuditor struct{}

func (rejectingInquiryAuditor) Record(context.Context, domainaudit.Entry) error {
	return errors.New("audit write failed")
}

func TestInquiryAuditDoesNotDiscloseWithoutPermissionReasonAndReceipt(t *testing.T) {
	f := newFixture(t)
	f.service.permissions = appaccess.NewPermissionResolver(inquiryAuditRoles{})
	now := nowUTC()
	purge := now.Add(time.Hour)
	store := &auditInquiryStub{item: domain.Inquiry{
		ID: receptionReq, CallerID: receptionCaller, OwnerID: receptionOwner, Status: domain.InquiryReplied,
		Question: "sensitive question", Answer: "sensitive answer", CreatedAt: now, PurgeAfter: &purge,
	}}
	service := NewInquiryAudit(f.service, store)
	input := InquiryAuditInput{InquiryID: receptionReq, Reason: "Incident evidence review"}
	if result, err := service.Read(t.Context(), employee("user-a"), input); !errors.Is(err, apperrors.ErrAccessDenied) || result != (InquiryAuditView{}) || store.calls != 0 {
		t.Fatalf("unauthorized read returned data or reached store: %+v, %v", result, err)
	}
	bad := input
	bad.Reason = "   "
	if _, err := service.Read(t.Context(), admin(), bad); !errors.Is(err, apperrors.ErrInvalidArgument) || store.calls != 0 {
		t.Fatalf("no reason reached store: %v", err)
	}
	got, err := service.Read(t.Context(), admin(), input)
	if err != nil || got.Question != store.item.Question || len(f.audit.entries) != 1 {
		t.Fatalf("audit read: %+v, %v", got, err)
	}
	entry := f.audit.entries[0]
	if entry.ResourceName != receptionReq || len(entry.Metadata) != 1 || entry.Metadata["reason"] != input.Reason {
		t.Fatalf("audit metadata retained incorrect fields: %+v", entry.Metadata)
	}
	f.service.auditor = rejectingInquiryAuditor{}
	if result, err := service.Read(t.Context(), admin(), input); err == nil || result != (InquiryAuditView{}) {
		t.Fatalf("failed audit sink released body: %+v, %v", result, err)
	}
	purge = now.Add(-time.Minute)
	if result, err := service.Read(t.Context(), admin(), input); !errors.Is(err, apperrors.ErrGone) || result != (InquiryAuditView{}) {
		t.Fatalf("expired inquiry released body: %+v, %v", result, err)
	}
}
