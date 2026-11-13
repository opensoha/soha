package resource

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"sync"
	"time"

	"github.com/google/uuid"
	contractresource "github.com/opensoha/soha/contracts/resource"
	domainaccess "github.com/opensoha/soha/internal/domain/access"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainresource "github.com/opensoha/soha/internal/domain/resource"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type PodTerminals struct {
	workloads  *Workloads
	repository PodTerminalRepository
	ctx        context.Context
	cancel     context.CancelFunc
	mu         sync.Mutex
	closing    bool
	active     sync.WaitGroup
}

func (service *Service) PodTerminals() *PodTerminals { return service.terminals }

func newPodTerminals(workloads *Workloads, repository PodTerminalRepository, lifecycle context.Context) *PodTerminals {
	if lifecycle == nil {
		lifecycle = context.Background()
	}
	ctx, cancel := context.WithCancel(lifecycle)
	return &PodTerminals{workloads: workloads, repository: repository, ctx: ctx, cancel: cancel}
}

func (service *PodTerminals) Shutdown(ctx context.Context) error {
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

func (service *PodTerminals) begin() error {
	service.mu.Lock()
	defer service.mu.Unlock()
	if service.closing || service.ctx.Err() != nil {
		return apperrors.ErrServiceUnavailable
	}
	service.active.Add(1)
	return nil
}

func (service *PodTerminals) Create(ctx context.Context, principal domainidentity.Principal, access domainidentity.AccessContext, clusterID, namespace, name string, input contractresource.PodTerminalInput) (contractresource.PodTerminalCreated, error) {
	var empty contractresource.PodTerminalCreated
	if service.ctx.Err() != nil {
		return empty, apperrors.ErrServiceUnavailable
	}
	input, err := input.Normalized()
	if err != nil || conditionalTerminalTarget(namespace, name) != nil {
		return empty, apperrors.ErrInvalidArgument
	}
	if service.repository == nil || service.workloads.executionIdentity == nil {
		return empty, apperrors.ErrUnsupportedOperation
	}
	principal, err = service.workloads.executionIdentity.CurrentStreamingPrincipal(ctx, principal, access)
	if err != nil {
		return empty, err
	}
	binding, err := terminalCredentialBinding(principal, access)
	if err != nil {
		return empty, err
	}
	connection, _, err := service.workloads.authorize(ctx, principal, clusterID, namespace, "Pod", domainaccess.ActionExec)
	if err != nil {
		return empty, err
	}
	ctx, connectionHash, err := bindPodExecutionConnection(ctx, connection)
	if err != nil {
		return empty, err
	}
	route, err := service.workloads.podTerminalRoute(connection)
	if err != nil {
		return empty, err
	}
	if err = route.CheckPodTerminal(ctx); err != nil {
		return empty, wrapAgentResourceError(err)
	}
	target, err := route.InspectPodExecutionTarget(ctx, namespace, name, input.ExpectedUID, input.Container)
	if err != nil {
		return empty, wrapAgentResourceError(err)
	}
	if !matchesPodExecutionTarget(target, namespace, name, contractresource.PodArgvInput{ExpectedUID: input.ExpectedUID, Container: input.Container}) {
		return empty, apperrors.ErrClusterUnready
	}
	input.Container = target.Container
	record, ticket, err := newPodTerminalRecord(principal, access, binding, connection.Summary.ID, input, target)
	if err != nil {
		return empty, err
	}
	record.ConnectionHash = connectionHash
	if err = service.repository.Create(ctx, record); err != nil {
		return empty, err
	}
	return contractresource.PodTerminalCreated{Session: record.Lease, Ticket: ticket}, nil
}

func newPodTerminalRecord(principal domainidentity.Principal, access domainidentity.AccessContext, binding, clusterID string, input contractresource.PodTerminalInput, target contractresource.PodExecutionTarget) (domainresource.PodTerminalRecord, string, error) {
	var ticketBytes [32]byte
	if _, err := rand.Read(ticketBytes[:]); err != nil {
		return domainresource.PodTerminalRecord{}, "", apperrors.ErrServiceUnavailable
	}
	ticket := base64.RawURLEncoding.EncodeToString(ticketBytes[:])
	now := time.Now().UTC().Truncate(time.Microsecond)
	deadline := now.Add(time.Duration(input.TimeoutSeconds) * time.Second)
	if !access.ExpiresAt.IsZero() && access.ExpiresAt.Before(deadline) {
		deadline = access.ExpiresAt.UTC().Truncate(time.Microsecond)
	}
	if !deadline.After(now) {
		return domainresource.PodTerminalRecord{}, "", apperrors.ErrUnauthorized
	}
	ticketExpiry := now.Add(time.Minute)
	if deadline.Before(ticketExpiry) {
		ticketExpiry = deadline
	}
	lease := contractresource.PodTerminalLease{ID: uuid.NewString(), Status: "pending", ClusterID: clusterID, Target: target, Shell: input.Shell, CreatedAt: now, TicketExpiresAt: ticketExpiry, DeadlineAt: deadline, MaxInputBytes: contractresource.NativePodTerminalMaxInputBytes, MaxOutputBytes: contractresource.NativePodTerminalMaxOutputBytes}
	access.Metadata = nil
	return domainresource.PodTerminalRecord{ActorID: principal.UserID, CredentialHash: binding, TicketHash: terminalDigest(ticket), Principal: principal, Access: access, Input: input, Lease: lease}, ticket, nil
}

func terminalCredentialBinding(principal domainidentity.Principal, access domainidentity.AccessContext) (string, error) {
	if principal.UserID == "" || principal.GovernedAI != nil {
		return "", apperrors.ErrUnauthorized
	}
	if access.SessionID != "" {
		return terminalDigest("session\x00" + access.SessionID), nil
	}
	if principal.AccessTokenID != "" {
		return terminalDigest("key\x00" + principal.AccessTokenID), nil
	}
	return "", apperrors.ErrUnauthorized
}
func terminalDigest(value string) string {
	digest := sha256.Sum256([]byte(value))
	return hex.EncodeToString(digest[:])
}
func conditionalTerminalTarget(namespace, name string) error {
	_, err := conditionalResourceKind(namespace, "Pod", name)
	return err
}

func (service *PodTerminals) Get(ctx context.Context, principal domainidentity.Principal, clusterID, namespace, name, id string) (contractresource.PodTerminalLease, error) {
	record, err := service.authorizedRecord(ctx, principal, clusterID, namespace, name, id)
	return record.Lease, err
}

func (service *PodTerminals) Close(ctx context.Context, principal domainidentity.Principal, clusterID, namespace, name, id string) (contractresource.PodTerminalLease, error) {
	record, err := service.authorizedRecord(ctx, principal, clusterID, namespace, name, id)
	if err != nil {
		return contractresource.PodTerminalLease{}, err
	}
	record, err = service.repository.RequestClose(ctx, principal.UserID, record.Lease.ID)
	return record.Lease, err
}

func (service *PodTerminals) authorizedRecord(ctx context.Context, principal domainidentity.Principal, clusterID, namespace, name, id string) (domainresource.PodTerminalRecord, error) {
	var empty domainresource.PodTerminalRecord
	if principal.UserID == "" || !validPodExecutionID(id) || conditionalTerminalTarget(namespace, name) != nil {
		return empty, apperrors.ErrInvalidArgument
	}
	if service.repository == nil {
		return empty, apperrors.ErrUnsupportedOperation
	}
	connection, _, err := service.workloads.authorize(ctx, principal, clusterID, namespace, "Pod", domainaccess.ActionExec)
	if err != nil {
		return empty, err
	}
	record, err := service.repository.Get(ctx, principal.UserID, id)
	if err != nil {
		return empty, err
	}
	if record.Lease.ClusterID != connection.Summary.ID || record.Lease.Target.Namespace != namespace || record.Lease.Target.PodName != name {
		return empty, apperrors.ErrNotFound
	}
	return record, nil
}
