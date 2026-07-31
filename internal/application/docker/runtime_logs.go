package docker

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"sort"
	"strings"
	"time"

	sohaapi "github.com/opensoha/soha-contracts/gen/go/sohaapi"
	appaccess "github.com/opensoha/soha/internal/application/access"
	domainaudit "github.com/opensoha/soha/internal/domain/audit"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainresource "github.com/opensoha/soha/internal/domain/resource"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"github.com/opensoha/soha/internal/platform/requestctx"
)

const (
	dockerLogQueryMetadataKey   = "dockerLogQuery"
	dockerLogProjectMetadataKey = "dockerProjectId"
)

func (s *Service) QueryProjectLogs(ctx context.Context, principal domainidentity.Principal, projectID string, query domainresource.LogQuery) (domainresource.LogPage, error) {
	query, err := normalizeDockerLogQuery(query)
	if err != nil {
		return domainresource.LogPage{}, err
	}
	serviceName := query.Selector.DockerService
	startedAt := time.Now()
	sinceSeconds := int64(0)
	if query.RuntimeOptions != nil {
		sinceSeconds = query.RuntimeOptions.SinceSeconds
	}
	raw, err := s.getProjectLogs(ctx, principal, projectID, serviceName, dockerLogTail(query), sinceSeconds)
	if err != nil {
		s.recordLogAudit(ctx, principal, projectID, serviceName, "docker.logs.query", "failure", "docker runtime log query failed")
		return domainresource.LogPage{}, err
	}
	entries := parseDockerLogContent(raw.Content, projectID, raw.ServiceName, query, time.Now().UTC())
	limit := dockerLogLimit(query)
	truncated := len(entries) > limit
	if truncated {
		entries = entries[:limit]
	}
	coverage := &domainresource.LogCoverage{ResolvedSources: 1, SuccessfulSources: 1}
	page := domainresource.LogPage{Entries: entries, Coverage: coverage, Truncated: truncated}
	s.recordLogAudit(ctx, principal, projectID, raw.ServiceName, "docker.logs.query", "success", fmt.Sprintf("queried %d docker runtime log entries; truncated=%t durationMs=%d", len(entries), truncated, time.Since(startedAt).Milliseconds()))
	return page, nil
}

func (s *Service) IssueProjectLogStreamTicket(ctx context.Context, principal domainidentity.Principal, accessCtx domainidentity.AccessContext, projectID string, query domainresource.LogQuery) (domainidentity.StreamTicket, error) {
	query, err := normalizeDockerLogQuery(query)
	if err != nil {
		return domainidentity.StreamTicket{}, err
	}
	if err := s.authorize(ctx, principal, appaccess.PermDockerServicesView); err != nil {
		return domainidentity.StreamTicket{}, err
	}
	target, err := s.projectRuntimeTarget(ctx, projectID, query.Selector.DockerService)
	if err != nil {
		return domainidentity.StreamTicket{}, err
	}
	query.Selector.DockerService = target.ServiceName
	if s.logStreamTickets == nil {
		return domainidentity.StreamTicket{}, fmt.Errorf("%w: stream ticket issuer is not configured", apperrors.ErrClusterUnready)
	}
	return s.logStreamTickets.IssueStreamTicket(ctx, principal, accessCtx, domainidentity.StreamTicketRequest{
		Path:     dockerLogStreamPath(projectID),
		Metadata: map[string]any{dockerLogProjectMetadataKey: projectID, dockerLogQueryMetadataKey: query},
	})
}

func (s *Service) StreamProjectLogEventsFromTicket(ctx context.Context, principal domainidentity.Principal, accessCtx domainidentity.AccessContext, projectID string, emit func(domainresource.LogStreamEvent) error) error {
	query, err := dockerLogQueryFromTicket(accessCtx, projectID)
	if err != nil {
		return err
	}
	query, err = normalizeDockerLogQuery(query)
	if err != nil {
		return err
	}
	if err := s.authorize(ctx, principal, appaccess.PermDockerServicesView); err != nil {
		return err
	}
	target, err := s.projectRuntimeTarget(ctx, projectID, query.Selector.DockerService)
	if err != nil {
		return err
	}
	query.Selector.DockerService = target.ServiceName
	if err := emit(domainresource.LogStreamEvent{Type: "status", Status: &domainresource.LogStreamStatus{State: "live"}}); err != nil {
		return err
	}
	writer := &dockerLogEventWriter{projectID: projectID, serviceName: query.Selector.DockerService, query: query, emit: emit}
	sinceSeconds := int64(0)
	if query.RuntimeOptions != nil {
		sinceSeconds = query.RuntimeOptions.SinceSeconds
	}
	err = s.streamProjectLogs(ctx, principal, projectID, query.Selector.DockerService, dockerLogTail(query), sinceSeconds, writer)
	if err == nil {
		err = writer.Flush()
	}
	result, summary := "success", "streamed docker runtime logs"
	if err != nil {
		result, summary = "failure", "docker runtime log stream failed"
	}
	s.recordLogAudit(ctx, principal, projectID, query.Selector.DockerService, "docker.logs.stream", result, summary)
	if err != nil {
		return err
	}
	return emit(domainresource.LogStreamEvent{Type: "end", Status: &domainresource.LogStreamStatus{State: "ended"}})
}

func normalizeDockerLogQuery(query domainresource.LogQuery) (domainresource.LogQuery, error) {
	mode := strings.TrimSpace(string(query.SourceMode))
	if mode == "" || mode == "auto" {
		mode = "runtime"
	}
	if mode != "runtime" {
		return query, fmt.Errorf("%w: docker logs currently support runtime mode", apperrors.ErrUnsupportedOperation)
	}
	query.SourceMode = sohaapi.LogSourceModeRuntime
	if query.Selector == nil {
		query.Selector = &domainresource.LogSourceSelector{}
	}
	selector := *query.Selector
	selector.DockerService = strings.TrimSpace(selector.DockerService)
	query.Selector = &selector
	if err := validateDockerLogFeatures(query); err != nil {
		return query, err
	}
	if err := validateDockerLogLimits(query); err != nil {
		return query, err
	}
	return query, nil
}

func validateDockerLogFeatures(query domainresource.LogQuery) error {
	selector := query.Selector
	if hasKubernetesDockerLogSelector(selector) {
		return fmt.Errorf("%w: kubernetes selectors are not valid for docker logs", apperrors.ErrInvalidArgument)
	}
	if selector.DockerService != "" && !runtimeServiceNamePattern.MatchString(selector.DockerService) {
		return fmt.Errorf("%w: invalid docker service name", apperrors.ErrInvalidArgument)
	}
	if query.Cursor != "" || len(query.Severities) > 0 {
		return fmt.Errorf("%w: cursor and severity filters require durable logs", apperrors.ErrUnsupportedOperation)
	}
	return nil
}

func hasKubernetesDockerLogSelector(selector *domainresource.LogSourceSelector) bool {
	return selector.Namespace != "" || selector.WorkloadKind != "" || selector.WorkloadName != "" ||
		selector.LabelSelector != "" || selector.AllContainers || len(selector.PodNames) > 0 || len(selector.Containers) > 0
}

func validateDockerLogLimits(query domainresource.LogQuery) error {
	if query.RuntimeOptions != nil {
		if query.RuntimeOptions.Previous {
			return fmt.Errorf("%w: previous logs are not available for docker", apperrors.ErrUnsupportedOperation)
		}
		if query.RuntimeOptions.SinceSeconds < 0 || query.RuntimeOptions.SinceSeconds > 604800 {
			return fmt.Errorf("%w: sinceSeconds is outside the supported range", apperrors.ErrInvalidArgument)
		}
	}
	if query.Tail < 0 || query.Tail > maxRuntimeLogTailLines || query.Limit < 0 || query.Limit > maxRuntimeLogTailLines {
		return fmt.Errorf("%w: tail and limit must not exceed %d", apperrors.ErrInvalidArgument, maxRuntimeLogTailLines)
	}
	if len(query.Text) > 2048 {
		return fmt.Errorf("%w: text filter is too long", apperrors.ErrInvalidArgument)
	}
	if query.From != nil && query.To != nil && query.From.After(*query.To) {
		return fmt.Errorf("%w: from must not be after to", apperrors.ErrInvalidArgument)
	}
	if direction := string(query.Direction); direction != "" && direction != "forward" && direction != "backward" {
		return fmt.Errorf("%w: unsupported direction", apperrors.ErrInvalidArgument)
	}
	return nil
}

func dockerLogTail(query domainresource.LogQuery) int {
	if query.Tail > 0 {
		return query.Tail
	}
	if query.Limit > 0 {
		return query.Limit
	}
	return defaultRuntimeLogTailLines
}

func dockerLogLimit(query domainresource.LogQuery) int {
	if query.Limit > 0 {
		return query.Limit
	}
	return dockerLogTail(query)
}

func parseDockerLogContent(content, projectID, serviceName string, query domainresource.LogQuery, observedAt time.Time) []domainresource.LogEntry {
	lines := strings.Split(strings.ReplaceAll(content, "\r\n", "\n"), "\n")
	entries := make([]domainresource.LogEntry, 0, len(lines))
	for _, line := range lines {
		entry, ok := parseDockerLogLine(line, projectID, serviceName, observedAt)
		if !ok || !dockerLogEntryMatches(entry, query) {
			continue
		}
		entries = append(entries, entry)
	}
	sort.SliceStable(entries, func(i, j int) bool {
		if query.Direction == sohaapi.LogDirectionForward {
			return entries[i].Timestamp.Before(entries[j].Timestamp)
		}
		return entries[i].Timestamp.After(entries[j].Timestamp)
	})
	return entries
}

func parseDockerLogLine(line, projectID, serviceName string, observedAt time.Time) (domainresource.LogEntry, bool) {
	line = strings.TrimSpace(line)
	if line == "" {
		return domainresource.LogEntry{}, false
	}
	if index := strings.Index(line, " | "); index >= 0 {
		line = strings.TrimSpace(line[index+3:])
	}
	timestamp := observedAt
	message := line
	if fields := strings.SplitN(line, " ", 2); len(fields) == 2 {
		if parsed, err := time.Parse(time.RFC3339Nano, fields[0]); err == nil {
			timestamp, message = parsed, fields[1]
		}
	}
	observed := observedAt
	return domainresource.LogEntry{
		Timestamp:  timestamp,
		ObservedAt: &observed,
		Message:    message,
		Stream:     sohaapi.Stdout,
		SourceMode: sohaapi.LogSourceModeRuntime,
		Source:     domainresource.LogSource{Domain: sohaapi.LogSourceDomainDocker, DockerProjectID: projectID, DockerService: serviceName},
	}, true
}

func dockerLogEntryMatches(entry domainresource.LogEntry, query domainresource.LogQuery) bool {
	if query.From != nil && entry.Timestamp.Before(*query.From) {
		return false
	}
	if query.To != nil && entry.Timestamp.After(*query.To) {
		return false
	}
	return query.Text == "" || strings.Contains(strings.ToLower(entry.Message), strings.ToLower(query.Text))
}

type dockerLogEventWriter struct {
	projectID   string
	serviceName string
	query       domainresource.LogQuery
	emit        func(domainresource.LogStreamEvent) error
	pending     string
	err         error
}

func (w *dockerLogEventWriter) Write(p []byte) (int, error) {
	if w.err != nil {
		return 0, w.err
	}
	w.pending += string(p)
	for {
		index := strings.IndexByte(w.pending, '\n')
		if index < 0 {
			break
		}
		line := w.pending[:index]
		w.pending = w.pending[index+1:]
		if err := w.emitLine(line); err != nil {
			w.err = err
			return 0, err
		}
	}
	return len(p), nil
}

func (w *dockerLogEventWriter) Flush() error {
	if w.err != nil {
		return w.err
	}
	if strings.TrimSpace(w.pending) == "" {
		return nil
	}
	line := w.pending
	w.pending = ""
	return w.emitLine(line)
}

func (w *dockerLogEventWriter) emitLine(line string) error {
	entry, ok := parseDockerLogLine(line, w.projectID, w.serviceName, time.Now().UTC())
	if !ok || !dockerLogEntryMatches(entry, w.query) {
		return nil
	}
	return w.emit(domainresource.LogStreamEvent{Type: "entry", Entry: &entry})
}

func dockerLogStreamPath(projectID string) string {
	return "/api/v1/docker/projects/" + url.PathEscape(strings.TrimSpace(projectID)) + "/logs/stream"
}

func dockerLogQueryFromTicket(accessCtx domainidentity.AccessContext, projectID string) (domainresource.LogQuery, error) {
	if accessCtx.TokenKind != "stream_ticket" {
		return domainresource.LogQuery{}, fmt.Errorf("%w: docker log stream requires a stream ticket", apperrors.ErrUnauthorized)
	}
	boundProjectID, _ := accessCtx.Metadata[dockerLogProjectMetadataKey].(string)
	if strings.TrimSpace(boundProjectID) != strings.TrimSpace(projectID) {
		return domainresource.LogQuery{}, fmt.Errorf("%w: stream ticket does not match docker project", apperrors.ErrUnauthorized)
	}
	rawQuery, ok := accessCtx.Metadata[dockerLogQueryMetadataKey]
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

func (s *Service) recordLogAudit(ctx context.Context, principal domainidentity.Principal, projectID, serviceName, action, result, summary string) {
	if s.audit == nil {
		return
	}
	meta := requestctx.FromContext(ctx)
	_ = s.audit.Record(ctx, domainaudit.Entry{
		ActorID: principal.UserID, ActorName: principal.UserName, Roles: principal.Roles, Teams: principal.Teams,
		ResourceKind: "DockerProject", ResourceName: projectID, Action: action, Result: result, Summary: summary,
		RequestPath: meta.Path, RequestMethod: meta.Method, RequestID: meta.RequestID, SourceIP: meta.SourceIP,
		Metadata: map[string]any{"source": meta.Source, "dockerProjectId": projectID, "dockerService": serviceName},
	})
}

var _ io.Writer = (*dockerLogEventWriter)(nil)
