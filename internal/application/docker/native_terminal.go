package docker

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"reflect"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	protocol "github.com/opensoha/soha/contracts/docker/runtime"
	"github.com/opensoha/soha/contracts/resource"
	appaccess "github.com/opensoha/soha/internal/application/access"
	domain "github.com/opensoha/soha/internal/domain/docker"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type NativeTerminalRepository interface {
	Create(context.Context, domain.NativeTerminalRecord) error
	Get(context.Context, string, string) (domain.NativeTerminalRecord, error)
	Consume(context.Context, string, string, string, string) (domain.NativeTerminalRecord, error)
	RequestClose(context.Context, string, string) (domain.NativeTerminalRecord, error)
	Finish(context.Context, domain.NativeTerminalRecord) error
}

type NativeTerminalIdentity interface {
	CurrentStreamingPrincipal(context.Context, domainidentity.Principal, domainidentity.AccessContext) (domainidentity.Principal, error)
}

type NativeTerminals struct {
	service    *Service
	repository NativeTerminalRepository
	identity   NativeTerminalIdentity
	ctx        context.Context
	cancel     context.CancelFunc
	mu         sync.Mutex
	closing    bool
	active     sync.WaitGroup
}

func NewNativeTerminals(service *Service, repository NativeTerminalRepository, identity NativeTerminalIdentity, lifecycle context.Context) (*NativeTerminals, error) {
	if service == nil || nativeTerminalDependencyMissing(repository) || nativeTerminalDependencyMissing(identity) {
		return nil, apperrors.ErrInvalidArgument
	}
	if lifecycle == nil {
		lifecycle = context.Background()
	}
	ctx, cancel := context.WithCancel(lifecycle)
	return &NativeTerminals{service: service, repository: repository, identity: identity, ctx: ctx, cancel: cancel}, nil
}

func nativeTerminalDependencyMissing(value any) bool {
	if value == nil {
		return true
	}
	switch reflect.ValueOf(value).Kind() {
	case reflect.Pointer, reflect.Map, reflect.Slice, reflect.Interface, reflect.Func, reflect.Chan:
		return reflect.ValueOf(value).IsNil()
	}
	return false
}

func (service *NativeTerminals) Shutdown(ctx context.Context) error {
	service.mu.Lock()
	service.closing = true
	service.cancel()
	service.mu.Unlock()
	done := make(chan struct{})
	go func() { service.active.Wait(); close(done) }()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (service *NativeTerminals) begin() error {
	service.mu.Lock()
	defer service.mu.Unlock()
	if service.closing || service.ctx.Err() != nil {
		return apperrors.ErrServiceUnavailable
	}
	service.active.Add(1)
	return nil
}

func (service *NativeTerminals) Create(ctx context.Context, principal domainidentity.Principal, access domainidentity.AccessContext, projectID string, input protocol.TerminalInput) (protocol.TerminalCreated, error) {
	var empty protocol.TerminalCreated
	if service.ctx.Err() != nil {
		return empty, apperrors.ErrServiceUnavailable
	}
	input, err := input.Normalized()
	if err != nil {
		return empty, apperrors.ErrInvalidArgument
	}
	principal, err = service.currentPrincipal(ctx, principal, access)
	if err != nil {
		return empty, err
	}
	binding, err := nativeDockerCredential(principal, access)
	if err != nil {
		return empty, err
	}
	target, err := service.service.boundReadTarget(ctx, projectID, input.ServiceName)
	if err != nil {
		return empty, err
	}
	if target.binding.ContainerID != input.ExpectedContainerID {
		return empty, apperrors.ErrConflict
	}
	if err := nativeDockerCapabilities(ctx, target); err != nil {
		return empty, err
	}
	if _, err := service.currentPrincipal(ctx, principal, access); err != nil {
		return empty, err
	}
	if err := service.service.confirmBoundReadTarget(ctx, target); err != nil {
		return empty, err
	}
	record, ticket, err := newNativeDockerRecord(principal, access, binding, target, input)
	if err != nil {
		return empty, err
	}
	if err := service.repository.Create(ctx, record); err != nil {
		return empty, err
	}
	service.record(ctx, principal, record.Lease, "create", "pending")
	return protocol.TerminalCreated{Session: record.Lease, Ticket: ticket}, nil
}

func newNativeDockerRecord(principal domainidentity.Principal, access domainidentity.AccessContext, binding string, target boundReadTarget, input protocol.TerminalInput) (domain.NativeTerminalRecord, string, error) {
	var secret [32]byte
	if _, err := rand.Read(secret[:]); err != nil {
		return domain.NativeTerminalRecord{}, "", apperrors.ErrServiceUnavailable
	}
	ticket := base64.RawURLEncoding.EncodeToString(secret[:])
	now := time.Now().UTC().Truncate(time.Microsecond)
	deadline := now.Add(time.Duration(input.TimeoutSeconds) * time.Second)
	if !access.ExpiresAt.IsZero() && access.ExpiresAt.Before(deadline) {
		deadline = access.ExpiresAt.UTC().Truncate(time.Microsecond)
	}
	if !deadline.After(now) {
		return domain.NativeTerminalRecord{}, "", apperrors.ErrUnauthorized
	}
	expires := minNativeTerminalTime(now.Add(time.Minute), deadline)
	lease := protocol.TerminalLease{Protocol: protocol.TerminalProtocol, ID: uuid.NewString(), Status: "pending", ProjectID: target.project.ID, Target: target.binding, Shell: input.Shell, CreatedAt: now, TicketExpiresAt: expires, DeadlineAt: deadline, MaxInputBytes: resource.NativePodTerminalMaxInputBytes, MaxOutputBytes: resource.NativePodTerminalMaxOutputBytes}
	access.Metadata = nil
	return domain.NativeTerminalRecord{ActorID: principal.UserID, CredentialHash: binding, TicketHash: nativeDockerDigest(ticket), HostID: target.host.ID, ServiceID: target.service.ID, HostRevision: target.host.ConfigurationRevision, ProjectRevision: target.project.ConfigurationRevision, EndpointDigest: nativeDockerDigest(target.host.Endpoint), Principal: principal, Access: access, Input: input, Lease: lease}, ticket, nil
}

func minNativeTerminalTime(a, b time.Time) time.Time {
	if a.Before(b) {
		return a
	}
	return b
}

func nativeDockerCredential(principal domainidentity.Principal, access domainidentity.AccessContext) (string, error) {
	if principal.UserID == "" || principal.GovernedAI != nil {
		return "", apperrors.ErrUnauthorized
	}
	if access.SessionID != "" {
		return nativeDockerDigest("session\x00" + access.SessionID), nil
	}
	if principal.AccessTokenID != "" {
		return nativeDockerDigest("key\x00" + principal.AccessTokenID), nil
	}
	return "", apperrors.ErrUnauthorized
}

func nativeDockerDigest(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func (service *NativeTerminals) currentPrincipal(ctx context.Context, principal domainidentity.Principal, access domainidentity.AccessContext) (domainidentity.Principal, error) {
	current, err := service.identity.CurrentStreamingPrincipal(ctx, principal, access)
	if err != nil {
		return current, err
	}
	if err := service.service.authorize(ctx, current, appaccess.ManagedActionPermission(appaccess.PermDockerServicesManage, "terminal")); err != nil {
		return current, err
	}
	return current, nil
}

func (service *NativeTerminals) Get(ctx context.Context, principal domainidentity.Principal, access domainidentity.AccessContext, projectID, id string) (protocol.TerminalLease, error) {
	record, err := service.authorizedRecord(ctx, principal, access, projectID, id)
	return record.Lease, err
}

func (service *NativeTerminals) Close(ctx context.Context, principal domainidentity.Principal, access domainidentity.AccessContext, projectID, id string) (protocol.TerminalLease, error) {
	record, err := service.authorizedRecord(ctx, principal, access, projectID, id)
	if err != nil {
		return protocol.TerminalLease{}, err
	}
	record, err = service.repository.RequestClose(ctx, principal.UserID, record.Lease.ID)
	if err == nil {
		service.record(ctx, principal, record.Lease, "close", record.Lease.Status)
	}
	return record.Lease, err
}

func (service *NativeTerminals) authorizedRecord(ctx context.Context, principal domainidentity.Principal, access domainidentity.AccessContext, projectID, id string) (domain.NativeTerminalRecord, error) {
	var empty domain.NativeTerminalRecord
	parsed, err := uuid.Parse(id)
	if err != nil || parsed == uuid.Nil || parsed.String() != id || projectID == "" || strings.TrimSpace(projectID) != projectID {
		return empty, apperrors.ErrInvalidArgument
	}
	principal, err = service.currentPrincipal(ctx, principal, access)
	if err != nil {
		return empty, err
	}
	binding, err := nativeDockerCredential(principal, access)
	if err != nil {
		return empty, err
	}
	record, err := service.repository.Get(ctx, principal.UserID, id)
	if err != nil {
		return empty, err
	}
	if record.CredentialHash != binding {
		return empty, apperrors.ErrAccessDenied
	}
	if record.Lease.ProjectID != projectID {
		return empty, apperrors.ErrNotFound
	}
	project, err := service.service.repo.GetProject(ctx, projectID)
	if err != nil {
		return empty, err
	}
	if project.ResourceUID != record.Lease.Target.ProjectUID {
		return empty, apperrors.ErrNotFound
	}
	if err := checkProjectScope(ctx, project); err != nil {
		return empty, err
	}
	return record, nil
}

func (service *NativeTerminals) record(ctx context.Context, principal domainidentity.Principal, lease protocol.TerminalLease, action, result string) {
	summary := "native Docker terminal " + action
	service.service.recordLogAudit(ctx, principal, lease.ProjectID, lease.Target.ServiceName, "docker.native_terminal."+action, result, summary)
	service.service.recordOperation(ctx, principal, "docker.native_terminal."+action, lease.ProjectID, lease.Target.ServiceName, result, summary, map[string]any{"terminalSessionId": lease.ID, "status": lease.Status})
}
