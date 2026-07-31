package resource

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"slices"
	"strings"
	"time"

	sohaapi "github.com/opensoha/soha-contracts/gen/go/sohaapi"
	domainaccess "github.com/opensoha/soha/internal/domain/access"
	domaincluster "github.com/opensoha/soha/internal/domain/cluster"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainresource "github.com/opensoha/soha/internal/domain/resource"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

const (
	clusterLogQueryMetadataKey = "clusterLogQuery"
	clusterLogIDMetadataKey    = "clusterId"
	clusterLogMaxSources       = 50
	clusterLogMaxEntries       = 5000
	clusterLogStreamDuration   = 30 * time.Minute
)

type logRuntimeRoute interface {
	QueryPodLogs(context.Context, domainresource.LogQuery) (domainresource.LogPage, error)
	StreamPodLogEvents(context.Context, domainresource.LogQuery, func(domainresource.LogStreamEvent) error) error
	Source() string
	AuditClusterID() string
	RuntimeError(error) error
}

type agentLogRoute struct {
	client    LogAgent
	clusterID string
}

func (r agentLogRoute) QueryPodLogs(ctx context.Context, query domainresource.LogQuery) (domainresource.LogPage, error) {
	return r.client.QueryPodLogs(ctx, query)
}

func (r agentLogRoute) StreamPodLogEvents(ctx context.Context, query domainresource.LogQuery, emit func(domainresource.LogStreamEvent) error) error {
	return r.client.StreamPodLogEvents(ctx, query, emit)
}

func (agentLogRoute) Source() string { return "agent" }

func (r agentLogRoute) AuditClusterID() string { return r.clusterID }

func (agentLogRoute) RuntimeError(err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%w: %v", apperrors.ErrClusterUnready, err)
}

type directLogRoute struct {
	backend   DirectLogs
	clusterID string
}

func (r directLogRoute) QueryPodLogs(ctx context.Context, query domainresource.LogQuery) (domainresource.LogPage, error) {
	return r.backend.QueryPodLogs(ctx, r.clusterID, query)
}

func (r directLogRoute) StreamPodLogEvents(ctx context.Context, query domainresource.LogQuery, emit func(domainresource.LogStreamEvent) error) error {
	return r.backend.StreamPodLogEvents(ctx, r.clusterID, query, emit)
}

func (directLogRoute) Source() string { return "live" }

func (r directLogRoute) AuditClusterID() string { return r.clusterID }

func (directLogRoute) RuntimeError(err error) error { return err }

func (l *Logs) QueryClusterLogs(ctx context.Context, principal domainidentity.Principal, clusterID string, query domainresource.LogQuery) (domainresource.LogPage, error) {
	query, err := normalizeClusterLogQuery(query)
	if err != nil {
		return domainresource.LogPage{}, err
	}
	if query.SourceMode == sohaapi.LogSourceModeDurable {
		return l.queryDurableClusterLogs(ctx, principal, clusterID, query)
	}
	query, connection, route, err := l.prepareClusterLogQuery(ctx, principal, clusterID, query, false)
	if err != nil {
		return domainresource.LogPage{}, err
	}
	startedAt := time.Now()
	page, err := route.QueryPodLogs(ctx, query)
	if err != nil {
		_ = l.recordAudit(ctx, principal, route.AuditClusterID(), query.Selector.Namespace, "Pod", "", string(domainaccess.ActionLogs), "failure", "aggregate runtime log query failed")
		return domainresource.LogPage{}, route.RuntimeError(err)
	}
	page.ScopeRestricted = false
	details := fmt.Sprintf("queried %d runtime log entries via %s in namespace %s; partial=%t truncated=%t durationMs=%d", len(page.Entries), route.Source(), displayNamespace(query.Selector.Namespace), page.Partial, page.Truncated, time.Since(startedAt).Milliseconds())
	_ = l.recordAudit(ctx, principal, connection.Summary.ID, query.Selector.Namespace, "Pod", "", string(domainaccess.ActionLogs), "success", details)
	return page, nil
}

func (l *Logs) queryDurableClusterLogs(ctx context.Context, principal domainidentity.Principal, clusterID string, query domainresource.LogQuery) (domainresource.LogPage, error) {
	if _, _, err := l.authorize(ctx, principal, clusterID, query.Selector.Namespace, "Pod", domainaccess.ActionLogs); err != nil {
		return domainresource.LogPage{}, err
	}
	if l.durable == nil {
		return domainresource.LogPage{}, fmt.Errorf("%w: durable log backend is not configured", apperrors.ErrClusterUnready)
	}
	page, err := l.durable.QueryDurableLogs(ctx, principal, clusterID, query)
	status, details := "success", "queried durable logs"
	if err != nil {
		status, details = "failure", "durable log query failed"
	}
	_ = l.recordAudit(ctx, principal, clusterID, query.Selector.Namespace, "Pod", "", string(domainaccess.ActionLogs), status, details)
	return page, err
}

func (l *Logs) IssueClusterLogStreamTicket(ctx context.Context, principal domainidentity.Principal, accessCtx domainidentity.AccessContext, clusterID string, query domainresource.LogQuery) (domainidentity.StreamTicket, error) {
	query, _, _, err := l.prepareClusterLogQuery(ctx, principal, clusterID, query, true)
	if err != nil {
		return domainidentity.StreamTicket{}, err
	}
	if l.tickets == nil {
		return domainidentity.StreamTicket{}, fmt.Errorf("%w: stream ticket issuer is not configured", apperrors.ErrClusterUnready)
	}
	return l.tickets.IssueStreamTicket(ctx, principal, accessCtx, domainidentity.StreamTicketRequest{
		Path: clusterLogStreamPath(clusterID),
		Metadata: map[string]any{
			clusterLogIDMetadataKey:    clusterID,
			clusterLogQueryMetadataKey: query,
		},
	})
}

func (l *Logs) StreamClusterLogsFromTicket(ctx context.Context, principal domainidentity.Principal, accessCtx domainidentity.AccessContext, clusterID string, emit func(domainresource.LogStreamEvent) error) error {
	query, err := clusterLogQueryFromTicket(accessCtx, clusterID)
	if err != nil {
		return err
	}
	return l.StreamClusterLogs(ctx, principal, clusterID, query, emit)
}

func (l *Logs) AuthorizeClusterLogs(ctx context.Context, principal domainidentity.Principal, clusterID string, query domainresource.LogQuery, stream bool) error {
	_, _, _, err := l.prepareClusterLogQuery(ctx, principal, clusterID, query, stream)
	return err
}

func (l *Logs) StreamClusterLogs(ctx context.Context, principal domainidentity.Principal, clusterID string, query domainresource.LogQuery, emit func(domainresource.LogStreamEvent) error) error {
	query, _, route, err := l.prepareClusterLogQuery(ctx, principal, clusterID, query, true)
	if err != nil {
		return err
	}
	streamCtx, cancel := context.WithTimeout(ctx, clusterLogStreamDuration)
	defer cancel()
	err = route.StreamPodLogEvents(streamCtx, query, emit)
	status := "success"
	details := fmt.Sprintf("streamed aggregate runtime logs via %s in namespace %s", route.Source(), displayNamespace(query.Selector.Namespace))
	if err != nil {
		status = "failure"
		details = "aggregate runtime log stream failed"
	}
	_ = l.recordAudit(ctx, principal, route.AuditClusterID(), query.Selector.Namespace, "Pod", "", string(domainaccess.ActionLogs), status, details)
	return route.RuntimeError(err)
}

func (l *Logs) prepareClusterLogQuery(ctx context.Context, principal domainidentity.Principal, clusterID string, query domainresource.LogQuery, stream bool) (domainresource.LogQuery, domaincluster.Connection, logRuntimeRoute, error) {
	query, err := normalizeClusterLogQuery(query)
	if err != nil {
		return query, domaincluster.Connection{}, nil, err
	}
	if query.SourceMode == sohaapi.LogSourceModeDurable {
		return query, domaincluster.Connection{}, nil, fmt.Errorf("%w: durable logs do not support runtime streaming", apperrors.ErrUnsupportedOperation)
	}
	connection, _, err := l.authorize(ctx, principal, clusterID, query.Selector.Namespace, "Pod", domainaccess.ActionLogs)
	if err != nil {
		return query, domaincluster.Connection{}, nil, err
	}
	route, err := l.routeClusterLogs(connection, clusterID, stream)
	if err != nil {
		return query, domaincluster.Connection{}, nil, err
	}
	return query, connection, route, nil
}

func (l *Logs) routeClusterLogs(connection domaincluster.Connection, clusterID string, stream bool) (logRuntimeRoute, error) {
	if connection.Summary.ConnectionMode != domaincluster.ConnectionModeAgent {
		if l.direct == nil {
			return nil, fmt.Errorf("%w: direct log backend is not configured", apperrors.ErrClusterUnready)
		}
		return directLogRoute{backend: l.direct, clusterID: clusterID}, nil
	}
	required := []string{"logs.runtime.aggregate", "logs.runtime.snapshot"}
	if stream {
		required = append(required, "logs.runtime.stream")
	}
	for _, capability := range required {
		if !slices.Contains(connection.Summary.Capabilities, capability) {
			return nil, unsupportedAgentOperation("connected agent does not publish " + capability)
		}
	}
	client, err := resolveAgentClient(l.agent, connection)
	if err != nil {
		return nil, err
	}
	return agentLogRoute{client: client, clusterID: connection.Summary.ID}, nil
}

func normalizeClusterLogQuery(query domainresource.LogQuery) (domainresource.LogQuery, error) {
	mode := strings.TrimSpace(string(query.SourceMode))
	if mode == "" {
		mode = "runtime"
	}
	if mode != "runtime" && mode != "auto" && mode != "durable" {
		return query, fmt.Errorf("%w: unsupported log source mode", apperrors.ErrUnsupportedOperation)
	}
	query.SourceMode = sohaapi.LogSourceMode(mode)
	if query.Selector == nil {
		return query, fmt.Errorf("%w: selector is required", apperrors.ErrInvalidArgument)
	}
	selector := *query.Selector
	selector.Namespace = strings.TrimSpace(selector.Namespace)
	if selector.Namespace == "" {
		return query, fmt.Errorf("%w: namespace is required", apperrors.ErrInvalidArgument)
	}
	var err error
	selector.PodNames, err = normalizeLogNames(selector.PodNames, "pod")
	if err != nil {
		return query, err
	}
	selector.Containers, err = normalizeLogNames(selector.Containers, "container")
	if err != nil {
		return query, err
	}
	selector.WorkloadKind = strings.TrimSpace(selector.WorkloadKind)
	selector.WorkloadName = strings.TrimSpace(selector.WorkloadName)
	selector.LabelSelector = strings.TrimSpace(selector.LabelSelector)
	query.Selector = &selector
	if err := validateClusterLogQuery(query); err != nil {
		return query, err
	}
	return query, nil
}

func validateClusterLogQuery(query domainresource.LogQuery) error {
	if err := validateClusterLogSelector(query.Selector); err != nil {
		return err
	}
	if err := validateClusterLogMode(query); err != nil {
		return err
	}
	return validateClusterLogBounds(query)
}

func validateClusterLogMode(query domainresource.LogQuery) error {
	if query.SourceMode != sohaapi.LogSourceModeDurable && (query.Cursor != "" || len(query.Severities) > 0) {
		return fmt.Errorf("%w: cursor and severity filters require durable logs", apperrors.ErrUnsupportedOperation)
	}
	if query.SourceMode == sohaapi.LogSourceModeDurable && query.RuntimeOptions != nil {
		return fmt.Errorf("%w: runtime options are not available for durable logs", apperrors.ErrUnsupportedOperation)
	}
	return nil
}

func validateClusterLogBounds(query domainresource.LogQuery) error {
	if query.Tail < 0 || query.Tail > clusterLogMaxEntries || query.Limit < 0 || query.Limit > clusterLogMaxEntries {
		return fmt.Errorf("%w: tail and limit must not exceed %d", apperrors.ErrInvalidArgument, clusterLogMaxEntries)
	}
	if len(query.Text) > 2048 {
		return fmt.Errorf("%w: text filter is too long", apperrors.ErrInvalidArgument)
	}
	if query.RuntimeOptions != nil && (query.RuntimeOptions.SinceSeconds < 0 || query.RuntimeOptions.SinceSeconds > 604800) {
		return fmt.Errorf("%w: sinceSeconds is outside the supported range", apperrors.ErrInvalidArgument)
	}
	if direction := string(query.Direction); direction != "" && direction != "forward" && direction != "backward" {
		return fmt.Errorf("%w: unsupported direction", apperrors.ErrInvalidArgument)
	}
	if query.From != nil && query.To != nil && query.From.After(*query.To) {
		return fmt.Errorf("%w: from must not be after to", apperrors.ErrInvalidArgument)
	}
	return nil
}

func validateClusterLogSelector(selector *domainresource.LogSourceSelector) error {
	if len(selector.PodNames) > clusterLogMaxSources || len(selector.Containers) > clusterLogMaxSources {
		return fmt.Errorf("%w: selector exceeds the %d source limit", apperrors.ErrInvalidArgument, clusterLogMaxSources)
	}
	if selector.AllContainers && len(selector.Containers) > 0 {
		return fmt.Errorf("%w: allContainers and containers cannot be combined", apperrors.ErrInvalidArgument)
	}
	if (selector.WorkloadKind == "") != (selector.WorkloadName == "") {
		return fmt.Errorf("%w: workload kind and name must be provided together", apperrors.ErrInvalidArgument)
	}
	if !supportedLogWorkloadKind(selector.WorkloadKind) {
		return fmt.Errorf("%w: unsupported workload kind", apperrors.ErrInvalidArgument)
	}
	return nil
}

func supportedLogWorkloadKind(kind string) bool {
	switch strings.ToLower(kind) {
	case "", "deployment", "deployments", "statefulset", "statefulsets", "daemonset", "daemonsets", "replicaset", "replicasets", "job", "jobs":
		return true
	default:
		return false
	}
}

func normalizeLogNames(values []string, kind string) ([]string, error) {
	result := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			return nil, fmt.Errorf("%w: %s names must not be empty", apperrors.ErrInvalidArgument, kind)
		}
		if _, ok := seen[value]; ok {
			return nil, fmt.Errorf("%w: %s names must be unique", apperrors.ErrInvalidArgument, kind)
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result, nil
}

func clusterLogStreamPath(clusterID string) string {
	return "/api/v1/clusters/" + url.PathEscape(strings.TrimSpace(clusterID)) + "/logs/stream"
}

func clusterLogQueryFromTicket(accessCtx domainidentity.AccessContext, clusterID string) (domainresource.LogQuery, error) {
	if accessCtx.TokenKind != "stream_ticket" {
		return domainresource.LogQuery{}, fmt.Errorf("%w: cluster log stream requires a stream ticket", apperrors.ErrUnauthorized)
	}
	boundClusterID, _ := accessCtx.Metadata[clusterLogIDMetadataKey].(string)
	if strings.TrimSpace(boundClusterID) != strings.TrimSpace(clusterID) {
		return domainresource.LogQuery{}, fmt.Errorf("%w: stream ticket does not match cluster", apperrors.ErrUnauthorized)
	}
	rawQuery, ok := accessCtx.Metadata[clusterLogQueryMetadataKey]
	if !ok {
		return domainresource.LogQuery{}, fmt.Errorf("%w: stream ticket is missing its query", apperrors.ErrUnauthorized)
	}
	payload, err := json.Marshal(rawQuery)
	if err != nil {
		return domainresource.LogQuery{}, fmt.Errorf("%w: invalid stream ticket query", apperrors.ErrUnauthorized)
	}
	var query domainresource.LogQuery
	if err := json.Unmarshal(payload, &query); err != nil {
		return domainresource.LogQuery{}, fmt.Errorf("%w: invalid stream ticket query", apperrors.ErrUnauthorized)
	}
	return query, nil
}
