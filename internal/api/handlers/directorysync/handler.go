package directorysync

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	apiMiddleware "github.com/opensoha/soha/internal/api/middleware"
	apiresponse "github.com/opensoha/soha/internal/api/response"
	appdirectorysync "github.com/opensoha/soha/internal/application/directorysync"
	domainaudit "github.com/opensoha/soha/internal/domain/audit"
	domain "github.com/opensoha/soha/internal/domain/directorysync"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainoperation "github.com/opensoha/soha/internal/domain/operation"
	dingtalkdirectory "github.com/opensoha/soha/internal/infrastructure/directoryconnector/dingtalk"
	feishudirectory "github.com/opensoha/soha/internal/infrastructure/directoryconnector/feishu"
	wecomdirectory "github.com/opensoha/soha/internal/infrastructure/directoryconnector/wecom"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"github.com/opensoha/soha/internal/platform/operationentry"
	"github.com/opensoha/soha/internal/platform/requestctx"
)

var errDirectoryLoginProviderUnavailable = errors.New("directory login provider unavailable")

type Repository interface {
	CreateConnection(context.Context, domain.Connection, domain.Policy) (domain.Connection, error)
	UpdateConnection(context.Context, domain.Connection, domain.Policy) (domain.Connection, error)
	GetConnection(context.Context, string) (domain.Connection, domain.Policy, error)
	ListConnections(context.Context) ([]domain.Connection, error)
	GetRun(context.Context, string) (domain.Run, error)
	ListRuns(context.Context, string, int) ([]domain.Run, error)
	GetActiveRun(context.Context, string) (domain.Run, error)
	ListConflicts(context.Context, string, int) ([]domain.Conflict, error)
	ResolveConflict(context.Context, string, string, string, time.Time) error
	UnlinkIdentity(context.Context, string, string, time.Time) (domain.IdentityLinkSuppression, error)
	ClearSuppression(context.Context, string, string, time.Time) error
	SetWebhookCredential(context.Context, domain.WebhookCredential) error
	GetWebhookCredential(context.Context, string) (domain.WebhookCredential, error)
	EnqueueEvent(context.Context, domain.EventEnvelope) (bool, error)
	SetSCIMToken(context.Context, string, string, time.Time) error
	ResolveSCIMConnectionForScope(context.Context, string, string) (string, error)
	UpsertSCIMOrganization(context.Context, string, domain.Organization) error
	DeleteSCIMOrganization(context.Context, string, string) error
	UpsertSCIMPerson(context.Context, string, domain.Person) error
	DeleteSCIMPerson(context.Context, string, string) error
	ReplaceSCIMMemberships(context.Context, string, string, []string) error
	SCIMSnapshot(context.Context, string) (domain.Snapshot, error)
	SetConnectionCredential(context.Context, domain.ConnectionCredential) error
	GetConnectionCredential(context.Context, string) (domain.ConnectionCredential, error)
	TransitionRun(context.Context, string, string, domain.RunStats, string, string) error
}

type Service interface {
	CreateConnection(context.Context, domain.Connection, domain.Policy) (domain.Connection, error)
	PullSnapshot(context.Context, string, appdirectorysync.Connector) (domain.Snapshot, string, error)
	Preview(context.Context, string, domain.Snapshot) (domain.Plan, error)
	Apply(context.Context, string, domain.Snapshot, string) (domain.Run, domain.Plan, error)
	ApplyTriggered(context.Context, string, domain.Snapshot, string, string) (domain.Run, domain.Plan, error)
}

type ConnectorFactory interface {
	Connector(string) (appdirectorysync.Connector, error)
}

type Handler struct {
	repository Repository
	service    Service
	connectors ConnectorFactory
	now        func() time.Time
	audit      interface {
		Record(context.Context, domainaudit.Entry) error
	}
	operations interface {
		Record(context.Context, domainoperation.Entry) error
	}
}

func (h *Handler) SetRecorders(audit interface {
	Record(context.Context, domainaudit.Entry) error
}, operations interface {
	Record(context.Context, domainoperation.Entry) error
}) {
	h.audit, h.operations = audit, operations
}

func New(repository Repository, service Service, connectors ConnectorFactory) *Handler {
	return &Handler{repository: repository, service: service, connectors: connectors, now: time.Now}
}

type connectionRequest struct {
	Name                     string         `json:"name" binding:"required"`
	ProviderType             string         `json:"providerType" binding:"required"`
	LoginProviderID          string         `json:"loginProviderId"`
	CredentialRef            string         `json:"credentialRef"`
	Enabled                  *bool          `json:"enabled"`
	Metadata                 map[string]any `json:"metadata"`
	Policy                   policyRequest  `json:"policy"`
	WebhookVerificationToken string         `json:"webhookVerificationToken"`
	WebhookEncryptKey        string         `json:"webhookEncryptKey"`
	SCIMBearerToken          string         `json:"scimBearerToken"`
	LDAPBindDN               string         `json:"ldapBindDn"`
	LDAPBindPassword         string         `json:"ldapBindPassword"`
}

type policyRequest struct {
	SyncOrganizations     *bool             `json:"syncOrganizations"`
	SyncPeople            bool              `json:"syncPeople"`
	Mode                  string            `json:"mode"`
	Schedule              string            `json:"schedule"`
	FullReconcileSchedule string            `json:"fullReconcileSchedule"`
	ProvisionMode         string            `json:"provisionMode"`
	TrustedEmailDomains   []string          `json:"trustedEmailDomains"`
	VerifiedEmailAutoLink bool              `json:"verifiedEmailAutoLink"`
	UserDisablePolicy     string            `json:"userDisablePolicy"`
	MissingObjectPolicy   string            `json:"missingObjectPolicy"`
	FieldMappings         map[string]string `json:"fieldMappings"`
}

type connectionResponse struct {
	ID              string         `json:"id"`
	Name            string         `json:"name"`
	ProviderType    string         `json:"providerType"`
	LoginProviderID string         `json:"loginProviderId,omitempty"`
	Enabled         bool           `json:"enabled"`
	Capabilities    []string       `json:"capabilities"`
	Status          string         `json:"status"`
	Metadata        map[string]any `json:"metadata,omitempty"`
	LastValidatedAt *time.Time     `json:"lastValidatedAt,omitempty"`
	CreatedBy       string         `json:"createdBy"`
	UpdatedBy       string         `json:"updatedBy"`
	CreatedAt       time.Time      `json:"createdAt"`
	UpdatedAt       time.Time      `json:"updatedAt"`
	Policy          policyResponse `json:"policy"`
}

type policyResponse struct {
	SyncOrganizations     bool              `json:"syncOrganizations"`
	SyncPeople            bool              `json:"syncPeople"`
	Mode                  string            `json:"mode"`
	Schedule              string            `json:"schedule,omitempty"`
	FullReconcileSchedule string            `json:"fullReconcileSchedule,omitempty"`
	ProvisionMode         string            `json:"provisionMode"`
	TrustedEmailDomains   []string          `json:"trustedEmailDomains"`
	VerifiedEmailAutoLink bool              `json:"verifiedEmailAutoLink"`
	UserDisablePolicy     string            `json:"userDisablePolicy"`
	MissingObjectPolicy   string            `json:"missingObjectPolicy"`
	FieldMappings         map[string]string `json:"fieldMappings"`
}

type runResponse struct {
	ID            string         `json:"id"`
	ConnectionID  string         `json:"connectionId"`
	Trigger       string         `json:"trigger"`
	Status        string         `json:"status"`
	StartedAt     *time.Time     `json:"startedAt,omitempty"`
	FinishedAt    *time.Time     `json:"finishedAt,omitempty"`
	Error         string         `json:"error,omitempty"`
	Organizations map[string]int `json:"organizations,omitempty"`
	People        map[string]int `json:"people,omitempty"`
}

func runView(run domain.Run) runResponse {
	return runResponse{ID: run.ID, ConnectionID: run.ConnectionID, Trigger: run.Trigger, Status: run.Status, StartedAt: run.StartedAt, FinishedAt: run.FinishedAt, Error: run.ErrorSummary,
		Organizations: map[string]int{"create": run.Stats.OrganizationsCreated, "update": run.Stats.OrganizationsUpdated, "move": run.Stats.OrganizationsMoved, "archive": run.Stats.OrganizationsArchived},
		People:        map[string]int{"create": run.Stats.PeopleCreated, "update": run.Stats.PeopleUpdated, "archive": run.Stats.PeopleArchived, "conflict": run.Stats.Conflicts}}
}

func connectionView(item domain.Connection, policy domain.Policy) connectionResponse {
	return connectionResponse{ID: item.ID, Name: item.Name, ProviderType: item.ProviderType, LoginProviderID: item.LoginProviderID, Enabled: item.Enabled, Capabilities: capabilityNames(item.Capabilities), Status: item.Status, Metadata: item.Metadata, LastValidatedAt: item.LastValidatedAt, CreatedBy: item.CreatedBy, UpdatedBy: item.UpdatedBy, CreatedAt: item.CreatedAt, UpdatedAt: item.UpdatedAt, Policy: policyResponse{SyncOrganizations: policy.SyncOrganizations, SyncPeople: policy.SyncPeople, Mode: policy.Mode, Schedule: policy.Schedule, FullReconcileSchedule: policy.FullReconcileSchedule, ProvisionMode: policy.ProvisionMode, TrustedEmailDomains: policy.TrustedEmailDomains, VerifiedEmailAutoLink: policy.VerifiedEmailAutoLink, UserDisablePolicy: policy.UserDisablePolicy, MissingObjectPolicy: policy.MissingObjectPolicy, FieldMappings: policy.FieldMappings}}
}

func capabilityNames(c domain.Capabilities) []string {
	result := []string{}
	if c.Organizations {
		result = append(result, "organizations")
	}
	if c.People {
		result = append(result, "people")
	}
	if c.Memberships {
		result = append(result, "memberships")
	}
	if c.Events {
		result = append(result, "events")
	}
	return result
}

func (h *Handler) ListConnections(c *gin.Context) {
	items, err := h.repository.ListConnections(c.Request.Context())
	if err != nil {
		writeError(c, err)
		return
	}
	result := make([]connectionResponse, 0, len(items))
	for _, item := range items {
		_, policy, err := h.repository.GetConnection(c.Request.Context(), item.ID)
		if err != nil {
			writeError(c, err)
			return
		}
		result = append(result, connectionView(item, policy))
	}
	apiresponse.Items(c, http.StatusOK, result)
}

func (h *Handler) GetConnection(c *gin.Context) {
	item, policy, err := h.repository.GetConnection(c.Request.Context(), c.Param("connectionID"))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, connectionView(item, policy))
}

func (h *Handler) CreateConnection(c *gin.Context) {
	var request connectionRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		writeError(c, fmt.Errorf("%w: invalid directory connection payload", apperrors.ErrInvalidArgument))
		return
	}
	principal := apiMiddleware.PrincipalFromContext(c)
	connection, policy := request.models(uuid.NewString(), principal.UserID, h.now().UTC())
	item, err := h.service.CreateConnection(c.Request.Context(), connection, policy)
	if err != nil {
		writeError(c, normalizeError(err))
		return
	}
	if request.WebhookVerificationToken != "" {
		if err := h.repository.SetWebhookCredential(c.Request.Context(), domain.WebhookCredential{ConnectionID: item.ID, VerificationToken: request.WebhookVerificationToken, EncryptKey: request.WebhookEncryptKey}); err != nil {
			writeError(c, err)
			return
		}
	}
	if request.SCIMBearerToken != "" {
		if err := h.repository.SetSCIMToken(c.Request.Context(), item.ID, hashToken(request.SCIMBearerToken), h.now().UTC()); err != nil {
			writeError(c, err)
			return
		}
	}
	if request.LDAPBindPassword != "" {
		if err := h.repository.SetConnectionCredential(c.Request.Context(), domain.ConnectionCredential{ConnectionID: item.ID, Username: request.LDAPBindDN, Password: request.LDAPBindPassword}); err != nil {
			writeError(c, err)
			return
		}
	}
	h.record(c, principal, "access.directory.connection.create", "DirectoryConnection", item.ID, "created directory connection")
	apiresponse.Item(c, http.StatusCreated, connectionView(item, policy))
}

func (h *Handler) UpdateConnection(c *gin.Context) {
	var request connectionRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		writeError(c, fmt.Errorf("%w: invalid directory connection payload", apperrors.ErrInvalidArgument))
		return
	}
	existing, _, err := h.repository.GetConnection(c.Request.Context(), c.Param("connectionID"))
	if err != nil {
		writeError(c, err)
		return
	}
	principal := apiMiddleware.PrincipalFromContext(c)
	item, policy := request.models(existing.ID, principal.UserID, h.now().UTC())
	if err := policy.Validate(); err != nil {
		writeError(c, normalizeError(err))
		return
	}
	if policy.Mode != domain.PolicyManual {
		if err := appdirectorysync.ValidateSchedule(policy.Schedule); err != nil {
			writeError(c, fmt.Errorf("%w: %v", apperrors.ErrInvalidArgument, err))
			return
		}
	}
	item.CreatedAt, item.CreatedBy = existing.CreatedAt, existing.CreatedBy
	item.Status, item.Capabilities, item.LastValidatedAt = existing.Status, existing.Capabilities, existing.LastValidatedAt
	item, err = h.repository.UpdateConnection(c.Request.Context(), item, policy)
	if err != nil {
		writeError(c, normalizeError(err))
		return
	}
	if request.WebhookVerificationToken != "" {
		if err := h.repository.SetWebhookCredential(c.Request.Context(), domain.WebhookCredential{ConnectionID: item.ID, VerificationToken: request.WebhookVerificationToken, EncryptKey: request.WebhookEncryptKey}); err != nil {
			writeError(c, err)
			return
		}
	}
	if request.SCIMBearerToken != "" {
		if err := h.repository.SetSCIMToken(c.Request.Context(), item.ID, hashToken(request.SCIMBearerToken), h.now().UTC()); err != nil {
			writeError(c, err)
			return
		}
	}
	if request.LDAPBindPassword != "" {
		if err := h.repository.SetConnectionCredential(c.Request.Context(), domain.ConnectionCredential{ConnectionID: item.ID, Username: request.LDAPBindDN, Password: request.LDAPBindPassword}); err != nil {
			writeError(c, err)
			return
		}
	}
	h.record(c, principal, "access.directory.connection.update", "DirectoryConnection", item.ID, "updated directory connection")
	apiresponse.Item(c, http.StatusOK, connectionView(item, policy))
}

func (h *Handler) DeleteConnection(c *gin.Context) {
	writeError(c, fmt.Errorf("%w: directory connection deletion is not implemented", apperrors.ErrUnsupportedOperation))
}

func (h *Handler) ValidateConnection(c *gin.Context) {
	connection, policy, err := h.repository.GetConnection(c.Request.Context(), c.Param("connectionID"))
	if err != nil {
		writeError(c, err)
		return
	}
	connector, err := h.connectors.Connector(connection.ProviderType)
	if err != nil {
		writeError(c, normalizeError(err))
		return
	}
	capabilities, err := connector.Validate(c.Request.Context(), connection)
	if err != nil {
		writeError(c, normalizeConnectorError("directory connection validation failed", err))
		return
	}
	now := h.now().UTC()
	connection.Capabilities, connection.Status, connection.LastValidatedAt, connection.UpdatedAt = capabilities, domain.ConnectionHealthy, &now, now
	if _, err = h.repository.UpdateConnection(c.Request.Context(), connection, policy); err != nil {
		writeError(c, err)
		return
	}
	h.record(c, apiMiddleware.PrincipalFromContext(c), "access.directory.connection.validate", "DirectoryConnection", connection.ID, "validated directory connection")
	apiresponse.Item(c, http.StatusOK, gin.H{"valid": true, "capabilities": capabilityNames(capabilities), "status": connection.Status, "validatedAt": now})
}

func (h *Handler) Preview(c *gin.Context) {
	snapshot, cursor, err := h.pull(c)
	if err != nil {
		writeError(c, err)
		return
	}
	plan, err := h.service.Preview(c.Request.Context(), c.Param("connectionID"), snapshot)
	if err != nil {
		writeError(c, normalizeError(err))
		return
	}
	organizations, people := summarizeChanges(plan.Changes)
	apiresponse.Item(c, http.StatusOK, gin.H{"connectionId": c.Param("connectionID"), "organizations": organizations, "people": people, "generatedAt": h.now().UTC(), "cursor": cursor})
}

func (h *Handler) Sync(c *gin.Context) {
	snapshot, cursor, err := h.pull(c)
	if err != nil {
		writeError(c, err)
		return
	}
	principal := apiMiddleware.PrincipalFromContext(c)
	run, plan, err := h.service.Apply(c.Request.Context(), c.Param("connectionID"), snapshot, principal.UserID)
	if err != nil {
		writeError(c, normalizeError(err))
		return
	}
	h.record(c, principal, "access.directory.sync", "DirectorySyncRun", run.ID, "completed manual directory synchronization")
	_, _ = plan, cursor
	apiresponse.Item(c, http.StatusAccepted, runView(run))
}

func (h *Handler) GetRun(c *gin.Context) {
	run, err := h.repository.GetRun(c.Request.Context(), c.Param("runID"))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, runView(run))
}

func (h *Handler) Cancel(c *gin.Context) {
	var run domain.Run
	var err error
	if c.Param("runID") == "" {
		run, err = h.repository.GetActiveRun(c.Request.Context(), c.Param("connectionID"))
	} else {
		run, err = h.repository.GetRun(c.Request.Context(), c.Param("runID"))
	}
	if err != nil {
		writeError(c, err)
		return
	}
	if !domain.CanTransitionRun(run.Status, domain.RunCanceled) {
		writeError(c, fmt.Errorf("%w: run cannot be canceled from status %q", apperrors.ErrConflict, run.Status))
		return
	}
	if err := h.repository.TransitionRun(c.Request.Context(), run.ID, domain.RunCanceled, run.Stats, "canceled", "canceled by user"); err != nil {
		writeError(c, err)
		return
	}
	h.record(c, apiMiddleware.PrincipalFromContext(c), "access.directory.sync.cancel", "DirectorySyncRun", run.ID, "canceled directory synchronization")
	apiresponse.Item(c, http.StatusOK, gin.H{"id": run.ID, "status": domain.RunCanceled})
}

func (h *Handler) ListRuns(c *gin.Context) {
	runs, err := h.repository.ListRuns(c.Request.Context(), c.Param("connectionID"), 50)
	if err != nil {
		writeError(c, err)
		return
	}
	result := make([]runResponse, 0, len(runs))
	for _, run := range runs {
		result = append(result, runView(run))
	}
	apiresponse.Items(c, http.StatusOK, result)
}
func (h *Handler) ListConflicts(c *gin.Context) {
	items, err := h.repository.ListConflicts(c.Request.Context(), strings.TrimSpace(c.Query("connectionId")), 100)
	if err != nil {
		writeError(c, err)
		return
	}
	result := make([]gin.H, 0, len(items))
	for _, item := range items {
		result = append(result, gin.H{"id": item.ID, "connectionId": item.ConnectionID, "objectType": item.ObjectType, "externalId": item.ExternalID, "reason": item.Reason, "status": item.Status, "createdAt": item.CreatedAt})
	}
	apiresponse.Items(c, http.StatusOK, result)
}
func (h *Handler) ResolveConflict(c *gin.Context) {
	var request struct {
		Resolution string `json:"resolution" binding:"required"`
	}
	if err := c.ShouldBindJSON(&request); err != nil {
		writeError(c, fmt.Errorf("%w: invalid conflict resolution", apperrors.ErrInvalidArgument))
		return
	}
	principal := apiMiddleware.PrincipalFromContext(c)
	if err := h.repository.ResolveConflict(c.Request.Context(), c.Param("conflictID"), request.Resolution, principal.UserID, h.now().UTC()); err != nil {
		writeError(c, err)
		return
	}
	h.record(c, principal, "access.directory.conflict.resolve", "DirectoryConflict", c.Param("conflictID"), "resolved directory conflict")
	apiresponse.Item(c, http.StatusOK, gin.H{"id": c.Param("conflictID"), "status": "resolved"})
}

func (h *Handler) UnlinkIdentity(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	item, err := h.repository.UnlinkIdentity(c.Request.Context(), c.Param("identityID"), principal.UserID, h.now().UTC())
	if err != nil {
		writeError(c, err)
		return
	}
	h.record(c, principal, "access.identity.unlink", "IdentityLink", c.Param("identityID"), "unlinked external identity and created suppression")
	apiresponse.Item(c, http.StatusOK, gin.H{"id": item.ID, "userId": item.UserID, "providerType": item.ProviderType, "providerId": item.ProviderID, "providerUserId": item.ProviderUserID, "reason": item.Reason, "createdAt": item.CreatedAt})
}

func (h *Handler) ClearSuppression(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	if err := h.repository.ClearSuppression(c.Request.Context(), c.Param("suppressionID"), principal.UserID, h.now().UTC()); err != nil {
		writeError(c, err)
		return
	}
	h.record(c, principal, "access.identity.suppression.clear", "IdentityLinkSuppression", c.Param("suppressionID"), "cleared identity link suppression")
	apiresponse.Item(c, http.StatusOK, gin.H{"id": c.Param("suppressionID"), "status": "cleared"})
}

func (h *Handler) IngestEvent(c *gin.Context) {
	connection, policy, err := h.repository.GetConnection(c.Request.Context(), c.Param("connectionID"))
	if err != nil || !connection.Enabled || policy.Mode != domain.PolicyScheduledAndRealtime || (connection.ProviderType != domain.ProviderFeishu && connection.ProviderType != domain.ProviderWeCom && connection.ProviderType != domain.ProviderDingTalk) {
		apiresponse.Error(c, http.StatusNotFound, "not_found", "directory event endpoint is unavailable")
		return
	}
	credential, err := h.repository.GetWebhookCredential(c.Request.Context(), connection.ID)
	if err != nil {
		apiresponse.Error(c, http.StatusUnauthorized, "unauthorized", "directory event credential is not configured")
		return
	}
	body, err := io.ReadAll(io.LimitReader(c.Request.Body, 1<<20))
	if err != nil {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid directory event payload")
		return
	}
	providerEventID, eventType, occurredAt, err := h.normalizeEvent(c, connection, credential, body)
	if err != nil {
		apiresponse.Error(c, http.StatusUnauthorized, "unauthorized", "invalid directory event")
		return
	}
	if providerEventID == "" {
		return
	}
	queued, err := h.repository.EnqueueEvent(c.Request.Context(), domain.EventEnvelope{ID: uuid.NewString(), ConnectionID: connection.ID, ProviderEventID: providerEventID, EventType: eventType, OccurredAt: occurredAt, ReceivedAt: h.now().UTC(), Status: "queued"})
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusAccepted, gin.H{"accepted": true, "duplicate": !queued})
}

func (h *Handler) VerifyEventEndpoint(c *gin.Context) {
	connection, policy, err := h.repository.GetConnection(c.Request.Context(), c.Param("connectionID"))
	if err != nil || !connection.Enabled || connection.ProviderType != domain.ProviderWeCom || policy.Mode != domain.PolicyScheduledAndRealtime {
		apiresponse.Error(c, http.StatusNotFound, "not_found", "directory event endpoint is unavailable")
		return
	}
	credential, err := h.repository.GetWebhookCredential(c.Request.Context(), connection.ID)
	if err != nil {
		apiresponse.Error(c, http.StatusUnauthorized, "unauthorized", "directory event credential is not configured")
		return
	}
	encrypted := c.Query("echostr")
	if err := wecomdirectory.VerifyEventSignature(credential.VerificationToken, c.Query("timestamp"), c.Query("nonce"), encrypted, c.Query("msg_signature")); err != nil {
		apiresponse.Error(c, http.StatusUnauthorized, "unauthorized", "invalid directory event signature")
		return
	}
	plain, err := wecomdirectory.DecryptEvent(credential.EncryptKey, encrypted)
	if err != nil {
		apiresponse.Error(c, http.StatusUnauthorized, "unauthorized", "invalid directory event")
		return
	}
	c.Data(http.StatusOK, "text/plain; charset=utf-8", plain)
}

func (h *Handler) normalizeEvent(c *gin.Context, connection domain.Connection, credential domain.WebhookCredential, body []byte) (string, string, time.Time, error) {
	now := h.now().UTC()
	switch connection.ProviderType {
	case domain.ProviderFeishu:
		if credential.EncryptKey != "" {
			if err := feishudirectory.VerifySignature(c.GetHeader("X-Lark-Request-Timestamp"), c.GetHeader("X-Lark-Request-Nonce"), credential.EncryptKey, body, c.GetHeader("X-Lark-Signature"), now, 5*time.Minute); err != nil {
				return "", "", time.Time{}, err
			}
		}
		event, challenge, err := feishudirectory.ParseEvent(body, credential.VerificationToken)
		if err != nil {
			return "", "", time.Time{}, err
		}
		if challenge != nil {
			c.JSON(http.StatusOK, gin.H{"challenge": challenge.Challenge})
			return "", "", now, nil
		}
		return event.ID, event.Type, event.OccurredAt, nil
	case domain.ProviderWeCom:
		encrypted, err := wecomdirectory.ParseEncryptedXML(body)
		if err != nil {
			return "", "", time.Time{}, err
		}
		signature := firstNonEmptyString(c.Query("msg_signature"), c.GetHeader("X-WeCom-Signature"))
		timestamp := firstNonEmptyString(c.Query("timestamp"), c.GetHeader("X-WeCom-Timestamp"))
		nonce := firstNonEmptyString(c.Query("nonce"), c.GetHeader("X-WeCom-Nonce"))
		if err := wecomdirectory.VerifyEventSignature(credential.VerificationToken, timestamp, nonce, encrypted, signature); err != nil {
			return "", "", time.Time{}, err
		}
		plain, err := wecomdirectory.DecryptEvent(credential.EncryptKey, encrypted)
		if err != nil {
			return "", "", time.Time{}, err
		}
		event, err := wecomdirectory.ParseEvent(plain)
		if err != nil {
			return "", "", time.Time{}, err
		}
		eventType := strings.Trim(strings.Join([]string{event.Event, event.ChangeType}, ":"), ":")
		return digestEventID(encrypted), eventType, time.Unix(event.CreateTime, 0), nil
	case domain.ProviderDingTalk:
		timestamp := c.GetHeader("X-DingTalk-Timestamp")
		if err := dingtalkdirectory.VerifyEventSignature(timestamp, credential.VerificationToken, c.GetHeader("X-DingTalk-Signature"), now, 5*time.Minute); err != nil {
			return "", "", time.Time{}, err
		}
		var envelope struct {
			EventID   string `json:"eventId"`
			EventType string `json:"eventType"`
			Timestamp int64  `json:"timestamp"`
		}
		if err := json.Unmarshal(body, &envelope); err != nil {
			return "", "", time.Time{}, err
		}
		if envelope.EventID == "" {
			envelope.EventID = digestEventID(string(body))
		}
		occurred := now
		if envelope.Timestamp > 0 {
			occurred = time.UnixMilli(envelope.Timestamp)
		}
		return envelope.EventID, envelope.EventType, occurred, nil
	default:
		return "", "", time.Time{}, fmt.Errorf("unsupported event provider")
	}
}

func digestEventID(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}
func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func (h *Handler) unsupported(c *gin.Context, operation string) {
	writeError(c, fmt.Errorf("%w: %s is not implemented", apperrors.ErrUnsupportedOperation, operation))
}

func summarizeChanges(changes []domain.Change) (map[string]int, map[string]int) {
	organizations := map[string]int{}
	people := map[string]int{}
	for _, change := range changes {
		target := organizations
		if change.Kind == "person" {
			target = people
		} else if change.Kind != "organization" {
			continue
		}
		action := change.Action
		if action == domain.ChangeRestore {
			action = domain.ChangeUpdate
		}
		if action == domain.ChangeNoop {
			action = "skip"
		}
		target[action]++
	}
	return organizations, people
}

func hashToken(value string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(value)))
	return hex.EncodeToString(sum[:])
}

func (h *Handler) pull(c *gin.Context) (domain.Snapshot, string, error) {
	connection, _, err := h.repository.GetConnection(c.Request.Context(), c.Param("connectionID"))
	if err != nil {
		return domain.Snapshot{}, "", err
	}
	connector, err := h.connectors.Connector(connection.ProviderType)
	if err != nil {
		return domain.Snapshot{}, "", normalizeError(err)
	}
	snapshot, cursor, err := h.service.PullSnapshot(c.Request.Context(), connection.ID, connector)
	if err != nil {
		return domain.Snapshot{}, "", normalizeConnectorError("pull directory snapshot failed", err)
	}
	return snapshot, cursor, nil
}

func (r connectionRequest) models(id, actor string, now time.Time) (domain.Connection, domain.Policy) {
	enabled := true
	if r.Enabled != nil {
		enabled = *r.Enabled
	}
	connection := domain.Connection{ID: id, Name: strings.TrimSpace(r.Name), ProviderType: strings.ToLower(strings.TrimSpace(r.ProviderType)), LoginProviderID: strings.TrimSpace(r.LoginProviderID), CredentialRef: strings.TrimSpace(r.CredentialRef), Enabled: enabled, Metadata: r.Metadata, CreatedBy: actor, UpdatedBy: actor, UpdatedAt: now}
	policy := domain.DefaultPolicy(id)
	if r.Policy.SyncOrganizations != nil {
		policy.SyncOrganizations = *r.Policy.SyncOrganizations
	}
	policy.SyncPeople = r.Policy.SyncPeople
	if r.Policy.Mode != "" {
		policy.Mode = r.Policy.Mode
	}
	policy.Schedule, policy.FullReconcileSchedule = strings.TrimSpace(r.Policy.Schedule), strings.TrimSpace(r.Policy.FullReconcileSchedule)
	if r.Policy.ProvisionMode != "" {
		policy.ProvisionMode = r.Policy.ProvisionMode
	}
	if r.Policy.UserDisablePolicy != "" {
		policy.UserDisablePolicy = r.Policy.UserDisablePolicy
	}
	if r.Policy.MissingObjectPolicy != "" {
		policy.MissingObjectPolicy = r.Policy.MissingObjectPolicy
	}
	policy.TrustedEmailDomains, policy.VerifiedEmailAutoLink, policy.FieldMappings = r.Policy.TrustedEmailDomains, r.Policy.VerifiedEmailAutoLink, r.Policy.FieldMappings
	policy.ConnectionID, policy.UpdatedBy, policy.UpdatedAt = id, actor, now
	return connection, policy
}

func (h *Handler) record(c *gin.Context, principal domainidentity.Principal, operationType, resourceKind, targetID, summary string) {
	meta := requestctx.FromContext(c.Request.Context())
	if h.audit != nil {
		_ = h.audit.Record(c.Request.Context(), domainaudit.Entry{ActorID: principal.UserID, ActorName: principal.UserName, Roles: principal.Roles, Teams: principal.Teams, ResourceKind: resourceKind, ResourceName: targetID, Action: strings.TrimPrefix(operationType, "access."), Result: "success", Summary: summary, RequestPath: meta.Path, RequestMethod: meta.Method, RequestID: meta.RequestID, SourceIP: meta.SourceIP, Metadata: map[string]any{"targetId": targetID, "source": meta.Source}})
	}
	if h.operations != nil {
		_ = h.operations.Record(c.Request.Context(), operationentry.New(c.Request.Context(), principal, operationType, map[string]any{"module": "access", "resourceKind": resourceKind, "targetId": targetID}, "success", summary, map[string]any{"targetId": targetID}))
	}
}

func normalizeError(err error) error {
	if errors.Is(err, domain.ErrInvalidPolicy) || errors.Is(err, domain.ErrInvalidSnapshot) || errors.Is(err, domain.ErrPeopleSyncDisabled) {
		return fmt.Errorf("%w: %v", apperrors.ErrInvalidArgument, err)
	}
	return err
}

func normalizeConnectorError(operation string, err error) error {
	if errors.Is(err, apperrors.ErrNotFound) {
		return fmt.Errorf("%w: %w: %s: %w", apperrors.ErrInvalidArgument, errDirectoryLoginProviderUnavailable, operation, err)
	}
	return fmt.Errorf("%w: %s: %w", apperrors.ErrInvalidArgument, operation, err)
}

func writeError(c *gin.Context, err error) {
	_ = c.Error(err)
	status, code, message := http.StatusInternalServerError, "internal_error", "internal server error"
	switch {
	case errors.Is(err, errDirectoryLoginProviderUnavailable):
		status, code, message = http.StatusBadRequest, "directory_login_provider_unavailable", "login provider not found, disabled, or incompatible; select it again in the directory connection"
	case errors.Is(err, apperrors.ErrUnsupportedOperation):
		status, code, message = http.StatusNotImplemented, "unsupported_operation", "operation is not supported"
	case errors.Is(err, apperrors.ErrInvalidArgument):
		status, code, message = http.StatusBadRequest, "invalid_argument", "invalid request"
	case errors.Is(err, apperrors.ErrConflict):
		status, code, message = http.StatusConflict, "conflict", "resource conflict"
	case errors.Is(err, apperrors.ErrNotFound):
		status, code, message = http.StatusNotFound, "not_found", "resource not found"
	}
	apiresponse.Error(c, status, code, message)
}
