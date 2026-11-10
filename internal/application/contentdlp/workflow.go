package contentdlp

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"strings"
	"time"

	appaccess "github.com/opensoha/soha/internal/application/access"
	domainaccess "github.com/opensoha/soha/internal/domain/access"
	domain "github.com/opensoha/soha/internal/domain/contentdlp"
	domaindocker "github.com/opensoha/soha/internal/domain/docker"
	"github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

const PermissionReview = "docker.dlp-appeals.review"
const PermissionLink = "network_access.dlp_linkage.manage"

type WorkflowStore interface {
	Event(context.Context, string, string) (domain.EventRecord, error)
	IssueAuthorization(context.Context, domain.EventRecord, string, string, string, time.Time) (time.Time, error)
	SubmitAppeal(context.Context, domain.EventRecord, string, string, time.Time) (domain.Appeal, error)
	ResolveAppeal(context.Context, domain.EventRecord, string, domain.ReviewInput, time.Time, time.Time) (domain.Appeal, error)
	Cases(context.Context, string, string) ([]domain.Case, error)
}
type TransferStore interface {
	Within(context.Context, func(context.Context) error) error
	Proof(context.Context, string) (*domain.Proof, error)
	CommitTransfer(context.Context, domain.Event, domain.Binding, *domain.Proof, time.Time) (domain.Decision, error)
	AttachHold(context.Context, string, string) error
}
type NetworkLinkage interface {
	Within(context.Context, func(context.Context) error) error
	Restrict(context.Context, domain.Event) (string, error)
	Release(context.Context, string, string) error
	State(context.Context, string) (domain.NetworkHold, error)
}
type Option func(*Service)

func WithNetworkLinkage(link NetworkLinkage) Option { return func(s *Service) { s.linkage = link } }
func (s *Service) atomic(ctx context.Context, fn func(context.Context) error) error {
	if s.linkage != nil {
		return s.linkage.Within(ctx, fn)
	}
	return s.transfers.Within(ctx, fn)
}

func (s *Service) authorizeLink(ctx context.Context, p identity.Principal) error {
	if s.linkage == nil {
		return apperrors.ErrServiceUnavailable
	}
	if err := appaccess.AuthorizeRuntimePermission(ctx, s.permissions, p, PermissionLink); err != nil {
		return err
	}
	d, err := s.scope.Authorize(ctx, domainaccess.Request{Principal: p, PermissionKey: PermissionLink, Subject: domainaccess.SubjectAttributes{UserID: p.UserID, Roles: p.Roles, Teams: p.Teams, Projects: p.Projects, Tags: p.Tags}, Resource: domainaccess.ResourceAttributes{Group: "network_access", Kind: "NetworkSecurityScope", Name: "all-subjects"}, Context: domainaccess.ContextAttributes{Source: "api", OccurredAt: time.Now().UTC()}})
	if err != nil {
		return err
	}
	if !d.Allowed {
		return apperrors.ErrAccessDenied
	}
	return nil
}
func (s *Service) authorizeContentLink(ctx context.Context, p identity.Principal, c domain.Content) error {
	if c.NetworkAction == "revoke_network" {
		return s.authorizeLink(ctx, p)
	}
	return nil
}

func (s *Service) caseScope(ctx context.Context, p identity.Principal, id string) (domaindocker.Project, bool, error) {
	for _, key := range []string{PermissionReview, PermissionEvents} {
		has, err := s.permissions.HasPermission(ctx, p, key)
		if err != nil {
			return domaindocker.Project{}, false, err
		}
		if has {
			project, err := s.project(ctx, p, id, key)
			return project, true, err
		}
	}
	project, err := s.project(ctx, p, id, appaccess.PermDockerServicesView)
	return project, false, err
}
func (s *Service) Activity(ctx context.Context, p identity.Principal, id string) ([]domain.Case, error) {
	project, privileged, err := s.caseScope(ctx, p, id)
	if err != nil {
		return nil, err
	}
	actor := p.UserID
	if privileged {
		actor = ""
	}
	return s.workflows.Cases(ctx, project.ID, actor)
}
func (s *Service) Case(ctx context.Context, p identity.Principal, id, eventID string) (domain.Case, error) {
	project, privileged, err := s.caseScope(ctx, p, id)
	if err != nil {
		return domain.Case{}, err
	}
	record, err := s.workflows.Event(ctx, project.ID, eventID)
	if err != nil {
		return domain.Case{}, err
	}
	if !privileged && record.Event.ActorID != p.UserID {
		return domain.Case{}, apperrors.ErrAccessDenied
	}
	result := domain.Case{Event: record.Event, Appeal: record.Appeal}
	if record.Event.NetworkHoldID != "" {
		if s.linkage == nil {
			return result, apperrors.ErrServiceUnavailable
		}
		state, err := s.linkage.State(ctx, record.Event.NetworkHoldID)
		if err != nil {
			return result, err
		}
		result.Linkage = &state
	}
	return result, nil
}
func (s *Service) ownEvent(ctx context.Context, p identity.Principal, id, eventID string) (domain.EventRecord, error) {
	project, err := s.project(ctx, p, id, appaccess.PermDockerServicesView)
	if err != nil {
		return domain.EventRecord{}, err
	}
	record, err := s.workflows.Event(ctx, project.ID, eventID)
	if err != nil {
		return record, err
	}
	if record.Event.ActorID != p.UserID || record.Event.Action != "transfer" {
		return record, apperrors.ErrAccessDenied
	}
	return record, nil
}
func (s *Service) Authorize(ctx context.Context, p identity.Principal, id, eventID string, input domain.AuthorizationInput) (domain.Authorization, error) {
	if input.Action != "acknowledge" && input.Action != "approved_appeal" {
		return domain.Authorization{}, apperrors.ErrInvalidArgument
	}
	record, err := s.ownEvent(ctx, p, id, eventID)
	if err != nil {
		return domain.Authorization{}, err
	}
	if record.Event.StepUpRequired {
		if err := RequireReleaseMFA(ctx, s.stepUp, p.UserID, input.SessionID); err != nil {
			return domain.Authorization{}, err
		}
	}
	var secret [32]byte
	if _, err := rand.Read(secret[:]); err != nil {
		return domain.Authorization{}, err
	}
	token := base64.RawURLEncoding.EncodeToString(secret[:])
	var expires time.Time
	err = s.atomic(ctx, func(ctx context.Context) error {
		var err error
		expires, err = s.workflows.IssueAuthorization(ctx, record, p.UserID, input.Action, ReleaseTokenHash(token, input.SessionID, record.Event.StepUpRequired), time.Now().UTC())
		return err
	})
	if err != nil {
		return domain.Authorization{}, err
	}
	return domain.Authorization{Token: token, ExpiresAt: expires}, nil
}
func (s *Service) Appeal(ctx context.Context, p identity.Principal, id, eventID string, input domain.AppealInput) (domain.Appeal, error) {
	if input.ReasonCode != "false_positive" && input.ReasonCode != "business_need" {
		return domain.Appeal{}, apperrors.ErrInvalidArgument
	}
	record, err := s.ownEvent(ctx, p, id, eventID)
	if err != nil {
		return domain.Appeal{}, err
	}
	var result domain.Appeal
	err = s.atomic(ctx, func(ctx context.Context) error {
		var err error
		result, err = s.workflows.SubmitAppeal(ctx, record, p.UserID, input.ReasonCode, time.Now().UTC())
		return err
	})
	return result, err
}
func (s *Service) Review(ctx context.Context, p identity.Principal, id, eventID string, input domain.ReviewInput) (domain.Appeal, error) {
	if input.ExpectedRevision < 1 || input.Resolution != "approved" && input.Resolution != "rejected" {
		return domain.Appeal{}, apperrors.ErrInvalidArgument
	}
	if input.Resolution == "approved" && input.ReasonCode != "confirmed_false_positive" && input.ReasonCode != "approved_business_exception" || input.Resolution == "rejected" && input.ReasonCode != "policy_required" && input.ReasonCode != "insufficient_context" {
		return domain.Appeal{}, apperrors.ErrInvalidArgument
	}
	project, err := s.project(ctx, p, id, PermissionReview)
	if err != nil {
		return domain.Appeal{}, err
	}
	record, err := s.workflows.Event(ctx, project.ID, eventID)
	if err != nil {
		return domain.Appeal{}, err
	}
	if record.Event.ActorID == p.UserID {
		return domain.Appeal{}, apperrors.ErrAccessDenied
	}
	release := input.Resolution == "approved" && record.Event.NetworkHoldID != ""
	if release {
		if err := s.authorizeLink(ctx, p); err != nil {
			return domain.Appeal{}, err
		}
	}
	var result domain.Appeal
	err = s.atomic(ctx, func(ctx context.Context) error {
		var err error
		result, err = s.workflows.ResolveAppeal(ctx, record, p.UserID, input, project.UpdatedAt, time.Now().UTC())
		if err != nil {
			return err
		}
		if release {
			return s.linkage.Release(ctx, record.Event.NetworkHoldID, p.UserID)
		}
		return nil
	})
	return result, err
}

type Denial struct{ Decision domain.Decision }

func (e *Denial) Error() string { return "DLP content transfer denied; event " + e.Decision.EventID }
func (e *Denial) Unwrap() error {
	code := "content_dlp_blocked"
	en := "Content transfer blocked by project DLP policy. Event: "
	zh := "内容传输已被项目 DLP 策略阻断。事件："
	if e.Decision.RequiredAction == "acknowledge" {
		code = "content_dlp_warning"
		en = "Explicit DLP acknowledgement required. Event: "
		zh = "需要显式确认 DLP 警告。事件："
	}
	return apperrors.NewBusiness(apperrors.ErrAccessDenied, code, en+e.Decision.EventID, zh+e.Decision.EventID)
}

func bindingTag(key []byte, actor string, policy domain.Policy, file domaindocker.ProjectVolumeFileContent) []byte {
	data, _ := json.Marshal([]any{actor, policy.ProjectID, file.ServiceName, file.Target, file.Path, policy.PublishedVersion, domain.DetectorVersion, file.Content})
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write(data)
	return mac.Sum(nil)
}
func (s *Service) transferProof(ctx context.Context, p identity.Principal, policy domain.Policy, file domaindocker.ProjectVolumeFileContent, access domain.TransferAccess) (*domain.Proof, error) {
	token := access.Token
	if token == "" {
		return nil, nil
	}
	if len(token) != 43 || strings.ContainsAny(token, " \r\n") {
		return nil, nil
	}
	if policy.Published.RequireStepUp {
		if err := RequireReleaseMFA(ctx, s.stepUp, p.UserID, access.SessionID); err != nil {
			return nil, err
		}
	}
	proof, err := s.transfers.Proof(ctx, ReleaseTokenHash(token, access.SessionID, policy.Published.RequireStepUp))
	if err != nil || proof == nil {
		return proof, err
	}
	e := proof.Record.Event
	b := proof.Record.Binding
	if e.ActorID != p.UserID || e.ProjectID != policy.ProjectID || e.PolicyVersion != policy.PublishedVersion || e.Mode != policy.Published.Mode || len(b.Key) != 32 || !b.ExpiresAt.After(time.Now().UTC()) || !hmac.Equal(b.Tag, bindingTag(b.Key, p.UserID, policy, file)) {
		return nil, nil
	}
	return proof, nil
}
