package directorysync

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	appaccess "github.com/opensoha/soha/internal/application/access"
	domainaudit "github.com/opensoha/soha/internal/domain/audit"
	domain "github.com/opensoha/soha/internal/domain/directorysync"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainoperation "github.com/opensoha/soha/internal/domain/operation"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"github.com/opensoha/soha/internal/platform/operationentry"
	"github.com/opensoha/soha/internal/platform/requestctx"
)

type ConnectionStore interface {
	CreateConnection(context.Context, domain.Connection, domain.Policy, domain.ConnectionCredentials) (domain.Connection, error)
	UpdateConnection(context.Context, domain.Connection, domain.Policy, domain.ConnectionCredentials) (domain.Connection, error)
	GetConnection(context.Context, string) (domain.Connection, domain.Policy, error)
	SaveConnectionValidation(context.Context, string, time.Time, domain.Capabilities, time.Time) error
}

type connectionAuditRecorder interface {
	Record(context.Context, domainaudit.Entry) error
}

type connectionOperationRecorder interface {
	Record(context.Context, domainoperation.Entry) error
}

type ConnectionService struct {
	store       ConnectionStore
	connectors  ConnectorFactory
	permissions *appaccess.PermissionResolver
	audit       connectionAuditRecorder
	operations  connectionOperationRecorder
}

func NewConnectionService(store ConnectionStore, connectors ConnectorFactory, permissions *appaccess.PermissionResolver, audit connectionAuditRecorder, operations connectionOperationRecorder) (*ConnectionService, error) {
	if nilSyncDependency(store) || nilSyncDependency(connectors) || permissions == nil {
		return nil, fmt.Errorf("directory connection management requires a store, connector factory, and permission resolver")
	}
	service := &ConnectionService{store: store, connectors: connectors, permissions: permissions}
	if !nilSyncDependency(audit) {
		service.audit = audit
	}
	if !nilSyncDependency(operations) {
		service.operations = operations
	}
	return service, nil
}

type ConnectionInput struct {
	Name, ProviderType, LoginProviderID, CredentialRef string
	Enabled                                            *bool
	Metadata                                           map[string]any
	Policy                                             ConnectionPolicyInput
	WebhookVerificationToken, WebhookEncryptKey        string
	SCIMBearerToken                                    string
	LDAPBindDN, LDAPBindPassword                       string
}

type ConnectionPolicyInput struct {
	SyncOrganizations     *bool
	SyncPeople            bool
	Mode                  string
	Schedule              string
	FullReconcileSchedule string
	ProvisionMode         string
	TrustedEmailDomains   []string
	VerifiedEmailAutoLink bool
	UserDisablePolicy     string
	MissingObjectPolicy   string
	FieldMappings         map[string]string
}

func (s *ConnectionService) CreateConnection(ctx context.Context, principal domainidentity.Principal, input ConnectionInput) (domain.Connection, domain.Policy, error) {
	if err := s.authorize(ctx, principal, "create"); err != nil {
		return domain.Connection{}, domain.Policy{}, err
	}
	now := time.Now().UTC()
	connection, policy := input.models(uuid.NewString(), principal.UserID, now)
	connection.CreatedAt, connection.CreatedBy, connection.Status = now, principal.UserID, domain.ConnectionPending
	if err := validateConnectionPolicy(connection, policy); err != nil {
		return domain.Connection{}, domain.Policy{}, err
	}
	item, err := s.store.CreateConnection(ctx, connection, policy, input.credentials())
	if err != nil {
		return domain.Connection{}, domain.Policy{}, err
	}
	s.record(ctx, principal, "create", item.ID, "created directory connection")
	return item, policy, nil
}

func (s *ConnectionService) UpdateConnection(ctx context.Context, principal domainidentity.Principal, id string, input ConnectionInput) (domain.Connection, domain.Policy, error) {
	if err := s.authorize(ctx, principal, "update"); err != nil {
		return domain.Connection{}, domain.Policy{}, err
	}
	existing, _, err := s.store.GetConnection(ctx, id)
	if err != nil {
		return domain.Connection{}, domain.Policy{}, err
	}
	connection, policy := input.models(existing.ID, principal.UserID, time.Now().UTC())
	if err := validateConnectionPolicy(connection, policy); err != nil {
		return domain.Connection{}, domain.Policy{}, err
	}
	item, err := s.store.UpdateConnection(ctx, connection, policy, input.credentials())
	if err != nil {
		return domain.Connection{}, domain.Policy{}, err
	}
	s.record(ctx, principal, "update", item.ID, "updated directory connection")
	return item, policy, nil
}

func (s *ConnectionService) ValidateConnection(ctx context.Context, principal domainidentity.Principal, id string) (domain.Connection, error) {
	if err := s.authorize(ctx, principal, "validate"); err != nil {
		return domain.Connection{}, err
	}
	item, _, err := s.store.GetConnection(ctx, id)
	if err != nil {
		return domain.Connection{}, err
	}
	connector, err := s.connectors.Connector(item.ProviderType)
	if err != nil {
		return domain.Connection{}, err
	}
	if nilSyncDependency(connector) {
		return domain.Connection{}, apperrors.ErrServiceUnavailable
	}
	capabilities, err := connector.Validate(ctx, item)
	if err != nil {
		return domain.Connection{}, ConnectorError("directory connection validation failed", err)
	}
	now := time.Now().UTC()
	if err := s.store.SaveConnectionValidation(ctx, id, item.UpdatedAt, capabilities, now); err != nil {
		return domain.Connection{}, err
	}
	item.Capabilities, item.Status, item.LastValidatedAt, item.UpdatedAt = capabilities, domain.ConnectionHealthy, &now, now
	s.record(ctx, principal, "validate", id, "validated directory connection")
	return item, nil
}

func (s *ConnectionService) authorize(ctx context.Context, principal domainidentity.Principal, action string) error {
	return appaccess.AuthorizeRuntimePermission(ctx, s.permissions, principal, appaccess.ManagedActionPermission(appaccess.PermAccessDirectoryManage, action))
}

func validateConnectionPolicy(connection domain.Connection, policy domain.Policy) error {
	if err := policy.ValidateForProvider(connection.ProviderType); err != nil {
		return err
	}
	if policy.Mode == domain.PolicyManual {
		return nil
	}
	if err := ValidateSchedule(policy.Schedule); err != nil {
		return fmt.Errorf("%w: %v", domain.ErrInvalidPolicy, err)
	}
	if policy.FullReconcileSchedule != "" {
		if err := ValidateSchedule(policy.FullReconcileSchedule); err != nil {
			return fmt.Errorf("%w: %v", domain.ErrInvalidPolicy, err)
		}
	}
	return nil
}

func (input ConnectionInput) models(id, actor string, now time.Time) (domain.Connection, domain.Policy) {
	enabled := true
	if input.Enabled != nil {
		enabled = *input.Enabled
	}
	connection := domain.Connection{
		ID: id, Name: strings.TrimSpace(input.Name), ProviderType: strings.ToLower(strings.TrimSpace(input.ProviderType)),
		LoginProviderID: strings.TrimSpace(input.LoginProviderID), CredentialRef: strings.TrimSpace(input.CredentialRef),
		Enabled: enabled, Metadata: input.Metadata, UpdatedBy: actor, UpdatedAt: now,
	}
	in := input.Policy
	policy := domain.DefaultPolicy(id)
	if in.SyncOrganizations != nil {
		policy.SyncOrganizations = *in.SyncOrganizations
	}
	policy.SyncPeople = in.SyncPeople
	if in.Mode != "" {
		policy.Mode = in.Mode
	}
	policy.Schedule, policy.FullReconcileSchedule = strings.TrimSpace(in.Schedule), strings.TrimSpace(in.FullReconcileSchedule)
	if in.ProvisionMode != "" {
		policy.ProvisionMode = in.ProvisionMode
	}
	if in.UserDisablePolicy != "" {
		policy.UserDisablePolicy = in.UserDisablePolicy
	}
	if in.MissingObjectPolicy != "" {
		policy.MissingObjectPolicy = in.MissingObjectPolicy
	}
	policy.TrustedEmailDomains, policy.VerifiedEmailAutoLink, policy.FieldMappings = in.TrustedEmailDomains, in.VerifiedEmailAutoLink, in.FieldMappings
	policy.UpdatedBy, policy.UpdatedAt = actor, now
	return connection, policy
}

func (input ConnectionInput) credentials() domain.ConnectionCredentials {
	// Omitted and empty primary credentials both retain any existing binding.
	var credentials domain.ConnectionCredentials
	if input.WebhookVerificationToken != "" {
		credentials.Webhook = domain.WebhookCredential{VerificationToken: input.WebhookVerificationToken, EncryptKey: input.WebhookEncryptKey}
	}
	if input.SCIMBearerToken != "" {
		credentials.SCIMTokenHash = HashSCIMToken(input.SCIMBearerToken)
	}
	if input.LDAPBindPassword != "" {
		credentials.LDAP = domain.ConnectionCredential{Username: input.LDAPBindDN, Password: input.LDAPBindPassword}
	}
	return credentials
}

func HashSCIMToken(value string) string {
	return fmt.Sprintf("%x", sha256.Sum256([]byte(strings.TrimSpace(value))))
}

func ConnectorError(operation string, err error) error {
	if errors.Is(err, apperrors.ErrNotFound) {
		return apperrors.NewBusiness(apperrors.ErrInvalidArgument,
			"directory_login_provider_unavailable",
			"login provider not found, disabled, or incompatible; select it again in the directory connection",
			"登录提供方不存在、已禁用或不兼容，请在目录连接中重新选择",
		)
	}
	// Provider errors can contain request data and are attached to Gin errors.
	return fmt.Errorf("%w: %s", apperrors.ErrInvalidArgument, operation)
}

func (s *ConnectionService) record(ctx context.Context, principal domainidentity.Principal, action, id, summary string) {
	const kind = "DirectoryConnection"
	if s.audit != nil {
		meta := requestctx.FromContext(ctx)
		_ = s.audit.Record(ctx, domainaudit.Entry{
			ActorID: principal.UserID, ActorName: principal.UserName, Roles: principal.Roles, Teams: principal.Teams,
			ResourceKind: kind, ResourceName: id, Action: "directory.connection." + action, Result: "success", Summary: summary,
			RequestPath: meta.Path, RequestMethod: meta.Method, RequestID: meta.RequestID, SourceIP: meta.SourceIP,
			Metadata: map[string]any{"targetId": id, "source": meta.Source},
		})
	}
	if s.operations != nil {
		_ = s.operations.Record(ctx, operationentry.New(ctx, principal, "access.directory.connection."+action,
			map[string]any{"module": "access", "resourceKind": kind, "targetId": id}, "success", summary, map[string]any{"targetId": id}))
	}
}
