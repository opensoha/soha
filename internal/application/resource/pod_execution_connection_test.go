package resource

import (
	"context"
	"errors"
	"io"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	contractresource "github.com/opensoha/soha/contracts/resource"
	domaincluster "github.com/opensoha/soha/internal/domain/cluster"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type changingPodConnection struct{ revision atomic.Int64 }

func (connection *changingPodConnection) GetConnection(_ context.Context, id string) (domaincluster.Connection, error) {
	return domaincluster.Connection{Summary: domaincluster.Summary{ID: id}, Metadata: map[string]any{"fixtureConnectionRevision": connection.revision.Load()}}, nil
}

func TestPodExecutionPinsActualTransportConfiguration(t *testing.T) {
	workloads, _, direct, _ := podExecutionTestService()
	connection := &changingPodConnection{}
	workloads.resourceAccess.resolver = connection
	writes := 0
	direct.execute = func(ctx context.Context, input contractresource.PodArgvInput) (contractresource.PodArgvResult, error) {
		connection.revision.Store(1)
		current, _ := connection.GetConnection(ctx, "cluster-a")
		// This is the same independent destination fence used by the actual
		// bootstrap transport resolver, after the earlier permission check.
		if domaincluster.VerifyExecutionConnection(ctx, current) != nil {
			return contractresource.PodArgvResult{}, apperrors.ErrConflict
		}
		writes++
		return testPodExecutionResult(input), nil
	}
	_, err := workloads.ExecutePodArgv(t.Context(), podExecutionTestPrincipal, domainidentity.AccessContext{}, "cluster-a", "team-a", "app", "connection-resolution", podExecutionTestInput())
	if !errors.Is(err, apperrors.ErrConflict) || writes != 0 {
		t.Fatalf("changed transport executed: err=%v writes=%d", err, writes)
	}
}

func TestPodExecutionConnectionChangeCancelsAndNeverReplays(t *testing.T) {
	workloads, _, direct, _ := podExecutionTestService()
	connection := &changingPodConnection{}
	workloads.resourceAccess.resolver = connection
	direct.execute = func(ctx context.Context, input contractresource.PodArgvInput) (contractresource.PodArgvResult, error) {
		connection.revision.Store(1)
		select {
		case <-ctx.Done():
			return contractresource.PodArgvResult{}, ctx.Err()
		case <-time.After(4 * time.Second):
			return testPodExecutionResult(input), nil
		}
	}
	item, err := workloads.ExecutePodArgv(t.Context(), podExecutionTestPrincipal, domainidentity.AccessContext{}, "cluster-a", "team-a", "app", "connection-active", podExecutionTestInput())
	if !errors.Is(err, apperrors.ErrAccessDenied) || item.Output != nil {
		t.Fatalf("changed connection returned output: %+v %v", item, err)
	}
	item, err = workloads.ExecutePodArgv(t.Context(), podExecutionTestPrincipal, domainidentity.AccessContext{}, "cluster-a", "team-a", "app", "connection-active", podExecutionTestInput())
	if err != nil || item.Status != "unknown" || item.Output != nil || direct.calls.Load() != 1 {
		t.Fatalf("original changed-connection attempt replayed: %+v %v calls=%d", item, err, direct.calls.Load())
	}
}

func TestNativeTerminalRejectsConnectionChangeBeforeTicketConsumption(t *testing.T) {
	service, _, direct, _, principal, access := terminalTestService(t)
	connection := &changingPodConnection{}
	service.workloads.resourceAccess.resolver = connection
	created, err := service.Create(t.Context(), principal, access, "cluster-a", "team-a", "app", contractresource.PodTerminalInput{ExpectedUID: "reviewed"})
	if err != nil {
		t.Fatal(err)
	}
	connection.revision.Store(1)
	session, err := service.Acquire(t.Context(), principal, access, "cluster-a", "team-a", "app", created.Session.ID, created.Ticket)
	if session != nil {
		defer func() { _ = session.Close() }()
	}
	if !errors.Is(err, apperrors.ErrConflict) || session != nil || direct.calls.Load() != 0 {
		t.Fatalf("changed pending connection admitted: session=%v err=%v", session != nil, err)
	}
}

func TestNativeTerminalLegacyPendingLeaseRemainsQueryableAndClosable(t *testing.T) {
	service, store, _, _, principal, access := terminalTestService(t)
	binding, err := terminalCredentialBinding(principal, access)
	if err != nil {
		t.Fatal(err)
	}
	input, _ := (contractresource.PodTerminalInput{ExpectedUID: "reviewed", Container: "main"}).Normalized()
	target := contractresource.PodExecutionTarget{Namespace: "team-a", PodName: "app", UID: "reviewed", Container: "main", CheckedAt: time.Now()}
	// Older encrypted records have no connection fingerprint. They remain
	// readable/closable, but must not be rebound to a newly selected endpoint.
	record, ticket, err := newPodTerminalRecord(principal, access, binding, "cluster-a", input, target)
	if err != nil {
		t.Fatal(err)
	}
	if err = store.Create(t.Context(), record); err != nil {
		t.Fatal(err)
	}
	session, err := service.Acquire(t.Context(), principal, access, "cluster-a", "team-a", "app", record.Lease.ID, ticket)
	if session != nil {
		_ = session.Close()
	}
	if !errors.Is(err, apperrors.ErrConflict) || session != nil {
		t.Fatalf("unbound legacy lease admitted: session=%v err=%v", session != nil, err)
	}
	if _, err = service.Get(t.Context(), principal, "cluster-a", "team-a", "app", record.Lease.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = service.Close(t.Context(), principal, "cluster-a", "team-a", "app", record.Lease.ID); err != nil {
		t.Fatal(err)
	}
}

func TestNativeTerminalConnectionChangeCancelsActiveStream(t *testing.T) {
	service, _, direct, _, principal, access := terminalTestService(t)
	connection := &changingPodConnection{}
	service.workloads.resourceAccess.resolver = connection
	direct.stream = func(ctx context.Context, _ io.ReadCloser, _ io.Writer) (contractresource.PodTerminalResult, error) {
		connection.revision.Store(1)
		select {
		case <-ctx.Done():
			return contractresource.PodTerminalResult{}, ctx.Err()
		case <-time.After(4 * time.Second):
			zero := int64(0)
			return contractresource.PodTerminalResult{Status: "succeeded", ExitCode: &zero, UIDVerifiedBeforeStart: true}, nil
		}
	}
	created, err := service.Create(t.Context(), principal, access, "cluster-a", "team-a", "app", contractresource.PodTerminalInput{ExpectedUID: "reviewed"})
	if err != nil {
		t.Fatal(err)
	}
	session, err := service.Acquire(t.Context(), principal, access, "cluster-a", "team-a", "app", created.Session.ID, created.Ticket)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = session.Close() }()
	result, err := session.Stream(io.NopCloser(strings.NewReader("")), io.Discard, nativeTerminalSizes{})
	if !errors.Is(err, apperrors.ErrAccessDenied) || result.Status == "succeeded" || result.ExitCode != nil || direct.calls.Load() != 1 {
		t.Fatalf("changed stream produced success: %+v %v calls=%d", result, err, direct.calls.Load())
	}
	stored, err := service.Get(t.Context(), principal, "cluster-a", "team-a", "app", created.Session.ID)
	if err != nil || stored.Status != "closed" || stored.Result == nil || stored.Result.Status != "unknown" || stored.Result.ExitCode != nil {
		t.Fatalf("original terminal did not retain unknown: %+v %v", stored, err)
	}
	if _, err = service.Acquire(t.Context(), principal, access, "cluster-a", "team-a", "app", created.Session.ID, created.Ticket); !errors.Is(err, apperrors.ErrConflict) || direct.calls.Load() != 1 {
		t.Fatalf("original changed-connection terminal replayed: %v calls=%d", err, direct.calls.Load())
	}
}
