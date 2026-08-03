package resource

import (
	"context"
	"errors"
	"testing"
	"time"

	sohaapi "github.com/opensoha/soha-contracts/gen/go/sohaapi"
	domainaccess "github.com/opensoha/soha/internal/domain/access"
	domaincluster "github.com/opensoha/soha/internal/domain/cluster"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainresource "github.com/opensoha/soha/internal/domain/resource"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type recordingLogAuthorizer struct {
	request domainaccess.Request
	allowed bool
}

func (a *recordingLogAuthorizer) Authorize(_ context.Context, request domainaccess.Request) (domainaccess.Decision, error) {
	a.request = request
	return domainaccess.Decision{Allowed: a.allowed, Reason: "test policy", AllowedActions: []domainaccess.Action{domainaccess.ActionLogs}}, nil
}

type recordingDirectLogs struct {
	queryCalled  bool
	streamCalled bool
	clusterID    string
	query        domainresource.LogQuery
}

type recordingDurableLogs struct {
	called    bool
	clusterID string
	query     domainresource.LogQuery
}

func (r *recordingDurableLogs) QueryDurableLogs(_ context.Context, _ domainidentity.Principal, clusterID string, query domainresource.LogQuery) (domainresource.LogPage, error) {
	r.called, r.clusterID, r.query = true, clusterID, query
	return domainresource.LogPage{Entries: []domainresource.LogEntry{{Message: "history"}}}, nil
}

func (r *recordingDirectLogs) QueryPodLogs(_ context.Context, clusterID string, query domainresource.LogQuery) (domainresource.LogPage, error) {
	r.queryCalled = true
	r.clusterID = clusterID
	r.query = query
	return domainresource.LogPage{Entries: []domainresource.LogEntry{{Message: "ready"}}}, nil
}

func (r *recordingDirectLogs) StreamPodLogEvents(_ context.Context, clusterID string, query domainresource.LogQuery, emit func(domainresource.LogStreamEvent) error) error {
	r.streamCalled = true
	r.clusterID = clusterID
	r.query = query
	return emit(domainresource.LogStreamEvent{Type: "end", Status: &domainresource.LogStreamStatus{State: "ended"}})
}

type recordingLogTicketIssuer struct {
	request domainidentity.StreamTicketRequest
}

func (i *recordingLogTicketIssuer) IssueStreamTicket(_ context.Context, _ domainidentity.Principal, _ domainidentity.AccessContext, request domainidentity.StreamTicketRequest) (domainidentity.StreamTicket, error) {
	i.request = request
	return domainidentity.StreamTicket{Ticket: "ticket-1", ExpiresAt: time.Now().UTC().Add(time.Minute)}, nil
}

func TestQueryClusterLogsAuthorizesNamespaceBeforeDirectRead(t *testing.T) {
	authorizer := &recordingLogAuthorizer{allowed: true}
	backend := &recordingDirectLogs{}
	service := New(Dependencies{
		Connections: stubConnectionResolver{connection: domaincluster.Connection{Summary: domaincluster.Summary{ID: "cluster-a", ConnectionMode: domaincluster.ConnectionModeDirectKubeconfig}}},
		Authorizer:  authorizer, Audit: discardAuditRecorder{}, DirectLogs: backend,
	})
	selector := domainresource.LogSourceSelector{Namespace: "team-a", WorkloadKind: "Deployment", WorkloadName: "api"}
	page, err := service.Logs().QueryClusterLogs(context.Background(), domainidentity.Principal{UserID: "user-1"}, "cluster-a", domainresource.LogQuery{Selector: &selector})
	if err != nil {
		t.Fatalf("QueryClusterLogs() error = %v", err)
	}
	if !backend.queryCalled || backend.clusterID != "cluster-a" || backend.query.Selector.Namespace != "team-a" {
		t.Fatalf("direct query = called %t cluster %q query %#v", backend.queryCalled, backend.clusterID, backend.query)
	}
	if authorizer.request.Action != domainaccess.ActionLogs || authorizer.request.Namespace.Namespace != "team-a" || authorizer.request.Resource.Kind != "Pod" {
		t.Fatalf("authorization request = %#v", authorizer.request)
	}
	if len(page.Entries) != 1 || page.ScopeRestricted {
		t.Fatalf("page = %#v", page)
	}
}

func TestQueryClusterLogsRequiresNamespace(t *testing.T) {
	backend := &recordingDirectLogs{}
	service := New(Dependencies{DirectLogs: backend})
	selector := domainresource.LogSourceSelector{}

	_, err := service.Logs().QueryClusterLogs(context.Background(), domainidentity.Principal{UserID: "user-1"}, "cluster-a", domainresource.LogQuery{Selector: &selector})
	if !errors.Is(err, apperrors.ErrInvalidArgument) {
		t.Fatalf("QueryClusterLogs() error = %v, want invalid argument", err)
	}
	if backend.queryCalled {
		t.Fatal("direct backend called without an explicit namespace")
	}
}

func TestQueryClusterLogsDenialDoesNotCallBackend(t *testing.T) {
	authorizer := &recordingLogAuthorizer{allowed: false}
	backend := &recordingDirectLogs{}
	service := New(Dependencies{
		Connections: stubConnectionResolver{connection: domaincluster.Connection{Summary: domaincluster.Summary{ID: "cluster-a"}}},
		Authorizer:  authorizer, Audit: discardAuditRecorder{}, DirectLogs: backend,
	})
	selector := domainresource.LogSourceSelector{Namespace: "restricted"}
	_, err := service.Logs().QueryClusterLogs(context.Background(), domainidentity.Principal{UserID: "user-1"}, "cluster-a", domainresource.LogQuery{Selector: &selector})
	if !errors.Is(err, apperrors.ErrAccessDenied) {
		t.Fatalf("QueryClusterLogs() error = %v, want access denied", err)
	}
	if backend.queryCalled {
		t.Fatal("direct backend called before authorization")
	}
}

func TestQueryDurableClusterLogsAuthorizesBeforeProvider(t *testing.T) {
	authorizer := &recordingLogAuthorizer{allowed: true}
	durable := &recordingDurableLogs{}
	direct := &recordingDirectLogs{}
	service := New(Dependencies{
		Connections: stubConnectionResolver{connection: domaincluster.Connection{Summary: domaincluster.Summary{ID: "cluster-a"}}},
		Authorizer:  authorizer, Audit: discardAuditRecorder{}, DirectLogs: direct, DurableLogs: durable,
	})
	selector := domainresource.LogSourceSelector{Namespace: "team-a"}
	page, err := service.Logs().QueryClusterLogs(context.Background(), domainidentity.Principal{UserID: "user-1"}, "cluster-a", domainresource.LogQuery{Selector: &selector, SourceMode: sohaapi.LogSourceModeDurable})
	if err != nil {
		t.Fatalf("QueryClusterLogs() error = %v", err)
	}
	if !durable.called || direct.queryCalled || durable.clusterID != "cluster-a" || len(page.Entries) != 1 {
		t.Fatalf("durable=%#v direct=%#v page=%#v", durable, direct, page)
	}
	if authorizer.request.Action != domainaccess.ActionLogs || authorizer.request.Namespace.Namespace != "team-a" {
		t.Fatalf("authorization request = %#v", authorizer.request)
	}

	allNamespaces := domainresource.LogSourceSelector{}
	durable.called = false
	page, err = service.Logs().QueryClusterLogs(context.Background(), domainidentity.Principal{UserID: "user-1"}, "cluster-a", domainresource.LogQuery{Selector: &allNamespaces, SourceMode: sohaapi.LogSourceModeDurable})
	if err != nil || !durable.called || len(page.Entries) != 1 {
		t.Fatalf("all-namespace durable query error=%v providerCalled=%t page=%#v", err, durable.called, page)
	}
	if authorizer.request.Namespace.Namespace != "" || durable.query.Selector.Namespace != "" {
		t.Fatalf("all-namespace query authorization=%#v query=%#v", authorizer.request, durable.query)
	}

	authorizer.allowed = false
	durable.called = false
	_, err = service.Logs().QueryClusterLogs(context.Background(), domainidentity.Principal{UserID: "user-1"}, "cluster-a", domainresource.LogQuery{Selector: &selector, SourceMode: sohaapi.LogSourceModeDurable})
	if !errors.Is(err, apperrors.ErrAccessDenied) || durable.called {
		t.Fatalf("denied durable query error=%v providerCalled=%t", err, durable.called)
	}
}

func TestDurableClusterLogsCannotIssueStreamTicket(t *testing.T) {
	tickets := &recordingLogTicketIssuer{}
	service := New(Dependencies{
		Connections: stubConnectionResolver{connection: domaincluster.Connection{Summary: domaincluster.Summary{ID: "cluster-a"}}},
		Authorizer:  &recordingLogAuthorizer{allowed: true}, Audit: discardAuditRecorder{}, StreamTickets: tickets,
	})
	selector := domainresource.LogSourceSelector{Namespace: "team-a"}
	_, err := service.Logs().IssueClusterLogStreamTicket(context.Background(), domainidentity.Principal{UserID: "user-1"}, domainidentity.AccessContext{}, "cluster-a", domainresource.LogQuery{Selector: &selector, SourceMode: sohaapi.LogSourceModeDurable})
	if !errors.Is(err, apperrors.ErrUnsupportedOperation) || tickets.request.Path != "" {
		t.Fatalf("IssueClusterLogStreamTicket() error=%v ticket=%#v", err, tickets.request)
	}
}

func TestClusterLogStreamTicketBindsAndReauthorizesQuery(t *testing.T) {
	authorizer := &recordingLogAuthorizer{allowed: true}
	backend := &recordingDirectLogs{}
	tickets := &recordingLogTicketIssuer{}
	service := New(Dependencies{
		Connections: stubConnectionResolver{connection: domaincluster.Connection{Summary: domaincluster.Summary{ID: "cluster-a"}}},
		Authorizer:  authorizer, Audit: discardAuditRecorder{}, DirectLogs: backend, StreamTickets: tickets,
	})
	selector := domainresource.LogSourceSelector{Namespace: "team-a", PodNames: []string{"api-0"}}
	query := domainresource.LogQuery{Selector: &selector, Tail: 50}
	ticket, err := service.Logs().IssueClusterLogStreamTicket(context.Background(), domainidentity.Principal{UserID: "user-1"}, domainidentity.AccessContext{TokenKind: "session_access", SessionID: "session-1"}, "cluster-a", query)
	if err != nil || ticket.Ticket == "" {
		t.Fatalf("IssueClusterLogStreamTicket() ticket=%#v error=%v", ticket, err)
	}
	if tickets.request.Path != "/api/v1/clusters/cluster-a/logs/stream" {
		t.Fatalf("ticket path = %q", tickets.request.Path)
	}
	accessCtx := domainidentity.AccessContext{TokenKind: "stream_ticket", Metadata: tickets.request.Metadata}
	var events []domainresource.LogStreamEvent
	if err := service.Logs().StreamClusterLogsFromTicket(context.Background(), domainidentity.Principal{UserID: "user-1"}, accessCtx, "cluster-a", func(event domainresource.LogStreamEvent) error {
		events = append(events, event)
		return nil
	}); err != nil {
		t.Fatalf("StreamClusterLogsFromTicket() error = %v", err)
	}
	if !backend.streamCalled || backend.query.Tail != 50 || len(events) != 1 || events[0].Type != "end" {
		t.Fatalf("stream called=%t query=%#v events=%#v", backend.streamCalled, backend.query, events)
	}
	if _, err := clusterLogQueryFromTicket(accessCtx, "cluster-b"); !errors.Is(err, apperrors.ErrUnauthorized) {
		t.Fatalf("mismatched cluster error = %v", err)
	}
}
