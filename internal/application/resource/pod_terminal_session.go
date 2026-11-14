package resource

import (
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"sync"
	"sync/atomic"
	"time"

	contractresource "github.com/opensoha/soha/contracts/resource"
	domaincluster "github.com/opensoha/soha/internal/domain/cluster"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainresource "github.com/opensoha/soha/internal/domain/resource"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type nativePodTerminalSession struct {
	service       *PodTerminals
	record        domainresource.PodTerminalRecord
	route         PodTerminalAgent
	ctx           context.Context
	cancel        context.CancelFunc
	stopLifecycle func() bool
	closed        sync.Once
	state         atomic.Int32 // 0 admitted, 1 streaming, 2 closed
	runDone       chan struct{}
	finishMu      sync.Mutex
	finished      bool
	pendingFinish *domainresource.PodTerminalRecord
}

func (service *PodTerminals) Acquire(ctx context.Context, principal domainidentity.Principal, access domainidentity.AccessContext, clusterID, namespace, name, id, ticket string) (NativePodTerminalSession, error) {
	record, err := service.authorizedRecord(ctx, principal, clusterID, namespace, name, id)
	if err != nil {
		return nil, err
	}
	binding, err := terminalCredentialBinding(principal, access)
	if err != nil || binding != record.CredentialHash || !validNativeTerminalTicket(ticket) {
		return nil, apperrors.ErrAccessDenied
	}
	if record.Lease.Status != "pending" {
		return nil, apperrors.ErrConflict
	}
	ctx = domaincluster.WithExpectedExecutionConnection(ctx, record.Lease.ClusterID, record.ConnectionHash)
	if err = service.checkOriginalAuthorization(ctx, record); err != nil {
		return nil, err
	}
	connection, err := service.workloads.authorizePodExecutionConnection(ctx, principal, record.Lease.ClusterID, namespace, record.ConnectionHash)
	if err != nil {
		return nil, err
	}
	route, err := service.workloads.podTerminalRoute(connection)
	if err != nil {
		return nil, err
	}
	if err = route.CheckPodTerminal(ctx); err != nil {
		return nil, wrapAgentResourceError(err)
	}
	if err = service.begin(); err != nil {
		return nil, err
	}
	record, err = service.repository.Consume(ctx, record.ActorID, id, binding, terminalDigest(ticket))
	if err != nil {
		service.active.Done()
		return nil, err
	}
	runCtx, cancel := context.WithDeadline(ctx, record.Lease.DeadlineAt)
	session := &nativePodTerminalSession{service: service, record: record, route: route, ctx: runCtx, cancel: cancel, runDone: make(chan struct{})}
	session.stopLifecycle = context.AfterFunc(service.ctx, cancel)
	return session, nil
}

func validNativeTerminalTicket(ticket string) bool {
	if len(ticket) != 43 {
		return false
	}
	data, err := base64.RawURLEncoding.Strict().DecodeString(ticket)
	return err == nil && len(data) == 32 && base64.RawURLEncoding.EncodeToString(data) == ticket
}

func (service *PodTerminals) checkOriginalAuthorization(ctx context.Context, record domainresource.PodTerminalRecord) error {
	if service.workloads.executionIdentity == nil {
		return apperrors.ErrUnsupportedOperation
	}
	principal, err := service.workloads.executionIdentity.CurrentStreamingPrincipal(ctx, record.Principal, record.Access)
	if err != nil {
		return err
	}
	_, err = service.workloads.authorizePodExecutionConnection(ctx, principal, record.Lease.ClusterID, record.Lease.Target.Namespace, record.ConnectionHash)
	return err
}

func (session *nativePodTerminalSession) Context() context.Context { return session.ctx }
func (session *nativePodTerminalSession) Lease() contractresource.PodTerminalLease {
	return session.record.Lease
}

func (session *nativePodTerminalSession) InitialSize() contractresource.NativeTerminalSize {
	return contractresource.NativeTerminalSize{Width: session.record.Input.Width, Height: session.record.Input.Height}
}

func (session *nativePodTerminalSession) check(ctx context.Context) error {
	if err := session.service.checkOriginalAuthorization(ctx, session.record); err != nil {
		return err
	}
	record, err := session.service.repository.Get(ctx, session.record.ActorID, session.record.Lease.ID)
	if err != nil {
		return err
	}
	if record.Lease.Status != "active" {
		return apperrors.ErrAccessDenied
	}
	return nil
}

func (session *nativePodTerminalSession) Stream(stdin io.ReadCloser, stdout io.Writer, sizes contractresource.NativeTerminalSizeQueue) (contractresource.PodTerminalResult, error) {
	if !session.state.CompareAndSwap(0, 1) {
		return contractresource.PodTerminalResult{}, apperrors.ErrConflict
	}
	defer close(session.runDone)
	item := session.record.Lease
	result := contractresource.PodTerminalResult{Status: "unknown", ReasonCode: "transport_lost"}
	if err := session.check(session.ctx); err != nil {
		_ = session.finish(result)
		return result, err
	}
	w := session.service.workloads
	_ = w.recordAudit(session.ctx, session.record.Principal, item.ClusterID, item.Target.Namespace, "Pod", item.Target.PodName, "exec", "started", "native Pod terminal admitted")
	w.recordOperation(session.ctx, session.record.Principal, "platform.pod.exec", item.ClusterID, item.Target.Namespace, "Pod", item.Target.PodName, "native Pod terminal admitted", map[string]any{"terminalSessionId": item.ID, "status": "active"})
	stop := watchPodExecutionAuthorization(session.ctx, session.cancel, session.check)
	result, executeErr := session.route.StreamNativePodTerminal(session.ctx, item.Target.Namespace, item.Target.PodName, session.record.Input, stdin, terminalAuthorizedWriter{ctx: session.ctx, writer: stdout}, sizes)
	authErr := stop()
	if authErr == nil && session.ctx.Err() == nil {
		authErr = session.check(session.ctx)
	}
	if executeErr != nil || result.Validate() != nil || authErr != nil {
		result = contractresource.PodTerminalResult{Status: "unknown", ReasonCode: "transport_lost"}
	}
	if err := session.finish(result); err != nil {
		return contractresource.PodTerminalResult{}, err
	}
	if authErr != nil {
		return contractresource.PodTerminalResult{}, authErr
	}
	if executeErr != nil {
		return result, wrapAgentResourceError(executeErr)
	}
	return result, nil
}

func (session *nativePodTerminalSession) finish(result contractresource.PodTerminalResult) error {
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
	saveCtx, cancel := context.WithTimeout(context.WithoutCancel(session.ctx), 4*time.Second)
	defer cancel()
	if err := session.service.repository.Finish(saveCtx, record); err != nil {
		// An uncertain storage reply may follow a committed CAS. Reading that
		// identical terminal record is recovery, never another provider attempt.
		stored, readErr := session.service.repository.Get(saveCtx, record.ActorID, record.Lease.ID)
		if readErr != nil || !sameTerminalFinish(stored, record) {
			return fmt.Errorf("%w: terminal outcome could not be persisted; query its session", apperrors.ErrServiceUnavailable)
		}
	}
	session.finished = true
	item := record.Lease
	w := session.service.workloads
	_ = w.recordAudit(saveCtx, session.record.Principal, item.ClusterID, item.Target.Namespace, "Pod", item.Target.PodName, "exec", item.Result.Status, "native Pod terminal finished")
	w.recordOperation(saveCtx, session.record.Principal, "platform.pod.exec", item.ClusterID, item.Target.Namespace, "Pod", item.Target.PodName, "native Pod terminal finished", map[string]any{"terminalSessionId": item.ID, "status": item.Result.Status})
	return nil
}

func sameTerminalFinish(left, right domainresource.PodTerminalRecord) bool {
	if left.Lease.Status != "closed" || left.CredentialHash != right.CredentialHash || left.Lease.ClosedAt == nil || right.Lease.ClosedAt == nil || !left.Lease.ClosedAt.Equal(*right.Lease.ClosedAt) || left.Lease.Result == nil || right.Lease.Result == nil {
		return false
	}
	lhs, rhs := *left.Lease.Result, *right.Lease.Result
	if (lhs.ExitCode == nil) != (rhs.ExitCode == nil) || lhs.ExitCode != nil && *lhs.ExitCode != *rhs.ExitCode {
		return false
	}
	lhs.ExitCode, rhs.ExitCode = nil, nil
	return lhs == rhs
}

func (session *nativePodTerminalSession) Close() error {
	var finishErr error
	session.closed.Do(func() {
		session.cancel()
		session.stopLifecycle()
		if !session.state.CompareAndSwap(0, 2) {
			<-session.runDone
			session.state.Store(2)
		}
		finishErr = session.finish(contractresource.PodTerminalResult{Status: "unknown", ReasonCode: "transport_lost"})
		session.service.active.Done()
	})
	return finishErr
}

type terminalAuthorizedWriter struct {
	ctx    context.Context
	writer io.Writer
}

func (writer terminalAuthorizedWriter) Write(data []byte) (int, error) {
	if err := writer.ctx.Err(); err != nil {
		return 0, err
	}
	return writer.writer.Write(data)
}
