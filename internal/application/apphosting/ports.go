package apphosting

import (
	"context"
	"errors"
	"io"
	"reflect"
	"strings"
	"time"

	"github.com/opensoha/soha/contracts/gen/go/sohaapi"
	appaccess "github.com/opensoha/soha/internal/application/access"
	domainapphosting "github.com/opensoha/soha/internal/domain/apphosting"
	domainaudit "github.com/opensoha/soha/internal/domain/audit"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"github.com/opensoha/soha/internal/platform/requestctx"
)

var ErrDNSLabelConflict = errors.New("hosted app dns label already exists")

type ActivateRevisionInput struct {
	AppID                string
	RevisionID           string
	SpaFallback          bool
	Files                []sohaapi.HostedAppManifestFile
	RetainedRevisionsMax int32
	Now                  time.Time
}

type ActivateRevisionOutput struct {
	App              domainapphosting.App
	Revision         domainapphosting.Revision
	EvictedRevisions []domainapphosting.Revision
}

type Repository interface {
	GetSettings(ctx context.Context) (domainapphosting.Settings, error)
	SaveSettings(ctx context.Context, mutate func(current domainapphosting.Settings) (domainapphosting.Settings, error)) (domainapphosting.Settings, error)
	CreateApp(ctx context.Context, candidate domainapphosting.App, maxAppsPerOwner int32) (domainapphosting.App, bool, error)
	GetAppByID(ctx context.Context, appID string) (domainapphosting.App, error)
	GetAppByDNSLabel(ctx context.Context, dnsLabel string) (domainapphosting.App, error)
	ListApps(ctx context.Context, filter domainapphosting.AppListFilter) ([]domainapphosting.App, int, error)
	MutateApp(ctx context.Context, appID string, mutate func(current domainapphosting.App) (domainapphosting.App, error)) (domainapphosting.App, error)
	FindRevisionByIdempotencyKey(ctx context.Context, appID, idempotencyKey string) (domainapphosting.Revision, error)
	CreateUploadingRevision(ctx context.Context, appID string, draft domainapphosting.Revision) (domainapphosting.Revision, error)
	FailRevision(ctx context.Context, appID, revisionID, stage, reason string) error
	ActivateRevision(ctx context.Context, input ActivateRevisionInput) (ActivateRevisionOutput, error)
	GetRevision(ctx context.Context, appID, revisionID string) (domainapphosting.Revision, error)
	ListRevisions(ctx context.Context, appID string, limit, offset int) ([]domainapphosting.Revision, int, error)
	RollbackRevision(ctx context.Context, appID, targetRevisionID, idempotencyKey string, now time.Time) (domainapphosting.App, domainapphosting.Revision, bool, error)
	CreateAuthTicket(ctx context.Context, ticket domainapphosting.AuthTicket) error
	ConsumeAuthTicket(ctx context.Context, ticketSHA256, host string, now time.Time) (domainapphosting.AuthTicket, error)
}

type BrowserSessionState struct {
	Active    bool
	UserID    string
	ExpiresAt time.Time
}

type SessionValidator interface {
	ValidateBrowserSession(ctx context.Context, sessionID, expectedUserID string) (BrowserSessionState, error)
}

type ObjectStore interface {
	PutObject(ctx context.Context, integrationID, key, contentType string, content io.Reader) (size int64, sha256Hex string, etag string, err error)
	DeleteKeys(ctx context.Context, integrationID string, keys []string) error
}

type AudienceDirectory interface {
	SearchCandidates(ctx context.Context, query string, limit int) (sohaapi.AppHostingAudienceCandidates, error)
	ResolveVisitor(ctx context.Context, userID string) (domainapphosting.VisitorProfile, error)
}

type PermissionChecker interface {
	Authorize(ctx context.Context, principal domainidentity.Principal, permissionKey string) error
	AuthorizeAny(ctx context.Context, principal domainidentity.Principal, permissionKeys ...string) error
}

type AuditRecorder interface {
	Record(ctx context.Context, entry domainaudit.Entry) error
}

type Option func(*Service)

func WithFallbackGatewayToken(token string) Option {
	return func(s *Service) {
		trimmed := strings.TrimSpace(token)
		if trimmed != "" {
			s.fallbackGatewayTokenSHA256 = sha256HexString(trimmed)
		}
	}
}

func WithClock(now func() time.Time) Option {
	return func(s *Service) {
		if now != nil {
			s.now = now
		}
	}
}

func WithSessionValidator(validator SessionValidator) Option {
	return func(s *Service) {
		s.sessions = nilIfTypedNil(validator)
	}
}

func WithVisitorIDSecret(secret string) Option {
	return func(s *Service) {
		if trimmed := strings.TrimSpace(secret); trimmed != "" {
			s.visitorIDSecret = []byte(trimmed)
		}
	}
}

type Service struct {
	repo                       Repository
	objectStore                ObjectStore
	audience                   AudienceDirectory
	permissions                PermissionChecker
	audit                      AuditRecorder
	sessions                   SessionValidator
	visitorIDSecret            []byte
	fallbackGatewayTokenSHA256 string
	now                        func() time.Time
}

func New(
	repo Repository,
	objectStore ObjectStore,
	audience AudienceDirectory,
	permissions PermissionChecker,
	audit AuditRecorder,
	opts ...Option,
) (*Service, error) {
	if isNilDependency(repo) {
		return nil, errors.New("apphosting repository is required")
	}
	if isNilDependency(permissions) {
		return nil, errors.New("apphosting permission checker is required")
	}
	svc := &Service{
		repo:        repo,
		objectStore: nilIfTypedNil(objectStore),
		audience:    nilIfTypedNil(audience),
		permissions: permissions,
		audit:       nilIfTypedNil(audit),
		now:         func() time.Time { return time.Now().UTC() },
	}
	for _, opt := range opts {
		if opt != nil {
			opt(svc)
		}
	}
	return svc, nil
}

// Keep unused struct padding free; helper methods below enforce typed-nil safety.
func (s *Service) authorize(ctx context.Context, principal domainidentity.Principal, permission string) error {
	if s == nil || isNilDependency(s.permissions) {
		return apperrors.ErrAccessDenied
	}
	return s.permissions.Authorize(ctx, principal, permission)
}

func (s *Service) authorizeAny(ctx context.Context, principal domainidentity.Principal, permissions ...string) error {
	if s == nil || isNilDependency(s.permissions) {
		return apperrors.ErrAccessDenied
	}
	return s.permissions.AuthorizeAny(ctx, principal, permissions...)
}

func (s *Service) hasAdminRead(ctx context.Context, principal domainidentity.Principal) bool {
	return s.authorizeAny(ctx, principal, appaccess.PermAppHostingAdminView, appaccess.PermAppHostingAdminManage) == nil
}

func (s *Service) recordAudit(ctx context.Context, principal domainidentity.Principal, action, result, resourceName, summary string, metadata map[string]any) {
	if s == nil || s.audit == nil {
		return
	}
	req := requestctx.FromContext(ctx)
	detached, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	_ = s.audit.Record(detached, domainaudit.Entry{
		ActorID:       principal.UserID,
		ActorName:     principal.UserName,
		Roles:         principal.Roles,
		Teams:         principal.Teams,
		ResourceKind:  "HostedApp",
		ResourceName:  resourceName,
		Action:        action,
		Result:        result,
		Summary:       summary,
		RequestPath:   req.Path,
		RequestMethod: req.Method,
		RequestID:     req.RequestID,
		SourceIP:      req.SourceIP,
		Metadata:      metadata,
		CreatedAt:     s.now(),
	})
}

func isNilDependency(value any) bool {
	if value == nil {
		return true
	}
	rv := reflect.ValueOf(value)
	switch rv.Kind() {
	case reflect.Pointer, reflect.Interface, reflect.Slice, reflect.Map, reflect.Func, reflect.Chan:
		return rv.IsNil()
	default:
		return false
	}
}

func nilIfTypedNil[T any](value T) T {
	if isNilDependency(value) {
		var zero T
		return zero
	}
	return value
}
