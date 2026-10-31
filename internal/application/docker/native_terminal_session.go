package docker

import (
	"context"
	"encoding/base64"
	"io"
	"sync"
	"sync/atomic"
	"time"

	protocol "github.com/opensoha/soha-contracts/docker/runtime"
	"github.com/opensoha/soha-contracts/resource"
	domain "github.com/opensoha/soha/internal/domain/docker"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type NativeDockerTerminalSession interface {
	Context() context.Context
	Lease() protocol.TerminalLease
	InitialSize() resource.NativeTerminalSize
	Cancel()
	Stream(io.ReadCloser, io.Writer, resource.NativeTerminalSizeQueue) (protocol.TerminalResult, error)
	Close() error
}

type nativeDockerTerminalSession struct {
	service       *NativeTerminals
	record        domain.NativeTerminalRecord
	ctx           context.Context
	cancel        context.CancelFunc
	stopLifecycle func() bool
	state         atomic.Int32
	runDone       chan struct{}
	closed        sync.Once
	closeErr      error
	finishMu      sync.Mutex
	finished      bool
	pendingFinish *domain.NativeTerminalRecord
}

func (service *NativeTerminals) Acquire(ctx context.Context, principal domainidentity.Principal, access domainidentity.AccessContext, projectID, id, ticket string) (NativeDockerTerminalSession, error) {
	record, err := service.authorizedRecord(ctx, principal, access, projectID, id)
	if err != nil {
		return nil, err
	}
	decoded, err := base64.RawURLEncoding.Strict().DecodeString(ticket)
	if err != nil || len(decoded) != 32 || len(ticket) != 43 || base64.RawURLEncoding.EncodeToString(decoded) != ticket {
		return nil, apperrors.ErrAccessDenied
	}
	if record.Lease.Status != "pending" {
		return nil, apperrors.ErrConflict
	}
	target, err := service.checkOriginal(ctx, record)
	if err != nil {
		return nil, err
	}
	if err := nativeDockerCapabilities(ctx, target); err != nil {
		return nil, err
	}
	if _, err := service.checkOriginal(ctx, record); err != nil {
		return nil, err
	}
	if err := service.begin(); err != nil {
		return nil, err
	}
	record, err = service.repository.Consume(ctx, record.ActorID, id, record.CredentialHash, nativeDockerDigest(ticket))
	if err != nil {
		service.active.Done()
		return nil, err
	}
	runCtx, cancel := context.WithDeadline(ctx, record.Lease.DeadlineAt)
	session := &nativeDockerTerminalSession{service: service, record: record, ctx: runCtx, cancel: cancel, runDone: make(chan struct{})}
	session.stopLifecycle = context.AfterFunc(service.ctx, cancel)
	return session, nil
}

func (service *NativeTerminals) checkOriginal(ctx context.Context, record domain.NativeTerminalRecord) (boundReadTarget, error) {
	if _, err := service.currentPrincipal(ctx, record.Principal, record.Access); err != nil {
		return boundReadTarget{}, err
	}
	target, err := service.service.boundReadTarget(ctx, record.Lease.ProjectID, record.Input.ServiceName)
	if err != nil {
		return boundReadTarget{}, err
	}
	if target.binding != record.Lease.Target || target.host.ID != record.HostID || target.service.ID != record.ServiceID || target.host.ConfigurationRevision != record.HostRevision || target.project.ConfigurationRevision != record.ProjectRevision || nativeDockerDigest(target.host.Endpoint) != record.EndpointDigest {
		return boundReadTarget{}, apperrors.ErrConflict
	}
	return target, nil
}

func (session *nativeDockerTerminalSession) Context() context.Context { return session.ctx }
func (session *nativeDockerTerminalSession) Lease() protocol.TerminalLease {
	return session.record.Lease
}
func (session *nativeDockerTerminalSession) Cancel() { session.cancel() }
func (session *nativeDockerTerminalSession) InitialSize() resource.NativeTerminalSize {
	return resource.NativeTerminalSize{Width: session.record.Input.Width, Height: session.record.Input.Height}
}

func (session *nativeDockerTerminalSession) check(ctx context.Context) (boundReadTarget, error) {
	stored, err := session.service.repository.Get(ctx, session.record.ActorID, session.record.Lease.ID)
	if err != nil {
		return boundReadTarget{}, err
	}
	if stored.Lease.Status != "active" || stored.CredentialHash != session.record.CredentialHash {
		return boundReadTarget{}, apperrors.ErrAccessDenied
	}
	return session.service.checkOriginal(ctx, session.record)
}

func (session *nativeDockerTerminalSession) Stream(stdin io.ReadCloser, stdout io.Writer, sizes resource.NativeTerminalSizeQueue) (protocol.TerminalResult, error) {
	if nativeTerminalDependencyMissing(stdin) || nativeTerminalDependencyMissing(stdout) || nativeTerminalDependencyMissing(sizes) {
		return protocol.TerminalResult{}, apperrors.ErrInvalidArgument
	}
	if !session.state.CompareAndSwap(0, 1) {
		return protocol.TerminalResult{}, apperrors.ErrConflict
	}
	defer close(session.runDone)
	unknown := protocol.TerminalResult{Status: "unknown", ReasonCode: "transport_lost"}
	target, err := session.check(session.ctx)
	if err != nil {
		_ = session.finish(unknown)
		return unknown, err
	}
	session.service.record(session.ctx, session.record.Principal, session.record.Lease, "start", "active")
	stopWatch := session.watchAuthorization()
	request := protocol.TerminalRequest{Protocol: protocol.TerminalProtocol, ProjectID: session.record.Lease.ProjectID, Container: session.record.Lease.Target, Input: session.record.Input, DeadlineAt: session.record.Lease.DeadlineAt}
	result, executeErr := streamNativeDockerTerminal(session.ctx, target, request, stdin, stdout, sizes)
	stopWatch()
	_, authErr := session.check(session.ctx)
	if executeErr != nil || result.Validate() != nil || authErr != nil || session.ctx.Err() != nil {
		result = unknown
	}
	if err := session.finish(result); err != nil {
		return protocol.TerminalResult{}, err
	}
	if authErr != nil {
		return result, authErr
	}
	return result, executeErr
}

func (session *nativeDockerTerminalSession) watchAuthorization() func() {
	ctx, stop := context.WithCancel(session.ctx)
	done := make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				checkCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
				_, err := session.check(checkCtx)
				cancel()
				if err != nil {
					if ctx.Err() == nil {
						session.cancel()
					}
					return
				}
			}
		}
	}()
	return func() { stop(); <-done }
}

func (session *nativeDockerTerminalSession) finish(result protocol.TerminalResult) error {
	session.finishMu.Lock()
	defer session.finishMu.Unlock()
	if session.finished {
		return nil
	}
	if session.pendingFinish == nil {
		now := time.Now().UTC().Truncate(time.Microsecond)
		record := session.record
		record.Lease.Status, record.Lease.ClosedAt, record.Lease.Result = "closed", &now, &result
		session.pendingFinish = &record
	}
	record := *session.pendingFinish
	ctx, cancel := context.WithTimeout(context.WithoutCancel(session.ctx), 4*time.Second)
	defer cancel()
	if err := session.service.repository.Finish(ctx, record); err != nil {
		stored, readErr := session.service.repository.Get(ctx, record.ActorID, record.Lease.ID)
		if readErr != nil || !sameNativeDockerFinish(stored, record) {
			return apperrors.ErrServiceUnavailable
		}
	}
	session.finished = true
	session.service.record(ctx, record.Principal, record.Lease, "finish", record.Lease.Result.Status)
	return nil
}

func sameNativeDockerFinish(a, b domain.NativeTerminalRecord) bool {
	if a.Lease.Status != "closed" || a.Lease.ID != b.Lease.ID || a.Lease.ProjectID != b.Lease.ProjectID || a.Lease.Target != b.Lease.Target || a.CredentialHash != b.CredentialHash || a.Lease.ClosedAt == nil || b.Lease.ClosedAt == nil || !a.Lease.ClosedAt.Equal(*b.Lease.ClosedAt) || a.Lease.Result == nil || b.Lease.Result == nil {
		return false
	}
	lhs, rhs := *a.Lease.Result, *b.Lease.Result
	if (lhs.ExitCode == nil) != (rhs.ExitCode == nil) || lhs.ExitCode != nil && *lhs.ExitCode != *rhs.ExitCode {
		return false
	}
	lhs.ExitCode, rhs.ExitCode = nil, nil
	return lhs == rhs
}

func (session *nativeDockerTerminalSession) Close() error {
	session.closed.Do(func() {
		session.cancel()
		session.stopLifecycle()
		if !session.state.CompareAndSwap(0, 2) {
			<-session.runDone
			session.state.Store(2)
		}
		session.closeErr = session.finish(protocol.TerminalResult{Status: "unknown", ReasonCode: "transport_lost"})
		session.service.active.Done()
	})
	return session.closeErr
}
