package delivery

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"sync"

	sohaapi "github.com/opensoha/soha-contracts/gen/go/sohaapi"
	domainaudit "github.com/opensoha/soha/internal/domain/audit"
	domaincatalog "github.com/opensoha/soha/internal/domain/catalog"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainresource "github.com/opensoha/soha/internal/domain/resource"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"github.com/opensoha/soha/internal/platform/requestctx"
)

const (
	deliveryLogApplicationMetadataKey = "deliveryLogApplicationId"
	deliveryLogEnvironmentMetadataKey = "deliveryLogEnvironmentId"
	deliveryLogQueryMetadataKey       = "deliveryLogQuery"
	deliveryLogMaxTargets             = 20
	deliveryLogMaxEntries             = 5000
)

type deliveryLogTarget struct {
	clusterID      string
	environmentKey string
	query          domainresource.LogQuery
}

func (s *Service) QueryApplicationEnvironmentLogs(ctx context.Context, principal domainidentity.Principal, applicationID, environmentID string, query domainresource.LogQuery) (domainresource.LogPage, error) {
	targets, err := s.resolveDeliveryLogTargets(ctx, principal, applicationID, environmentID, query)
	if err != nil {
		s.recordDeliveryLogAudit(ctx, principal, applicationID, environmentID, "delivery.logs.query", "failure", "delivery log query rejected", 0)
		return domainresource.LogPage{}, err
	}
	if len(targets) > 1 && query.Cursor != "" {
		return domainresource.LogPage{}, fmt.Errorf("%w: cursor pagination requires a single delivery target", apperrors.ErrUnsupportedOperation)
	}

	result := domainresource.LogPage{Entries: make([]domainresource.LogEntry, 0)}
	for _, target := range targets {
		page, queryErr := s.logs.QueryClusterLogs(ctx, principal, target.clusterID, target.query)
		if queryErr != nil {
			s.recordDeliveryLogAudit(ctx, principal, applicationID, environmentID, "delivery.logs.query", "failure", "delivery log query failed", len(targets))
			return domainresource.LogPage{}, queryErr
		}
		remapDeliveryLogPage(&page, applicationID, target.environmentKey)
		result.Entries = append(result.Entries, page.Entries...)
		result.Partial = result.Partial || page.Partial
		result.Truncated = result.Truncated || page.Truncated
		result.ScopeRestricted = result.ScopeRestricted || page.ScopeRestricted
		result.Warnings = append(result.Warnings, page.Warnings...)
		if page.Coverage != nil {
			if result.Coverage == nil {
				result.Coverage = &domainresource.LogCoverage{}
			}
			result.Coverage.ResolvedSources += page.Coverage.ResolvedSources
			result.Coverage.SuccessfulSources += page.Coverage.SuccessfulSources
			result.Coverage.FailedSources += page.Coverage.FailedSources
		}
		if len(targets) == 1 {
			result.NextCursor = page.NextCursor
			result.RetentionHint = page.RetentionHint
		}
	}

	sort.SliceStable(result.Entries, func(i, j int) bool {
		if query.Direction == sohaapi.LogDirectionForward {
			return result.Entries[i].Timestamp.Before(result.Entries[j].Timestamp)
		}
		return result.Entries[i].Timestamp.After(result.Entries[j].Timestamp)
	})
	limit := query.Limit
	if limit <= 0 || limit > deliveryLogMaxEntries {
		limit = deliveryLogMaxEntries
	}
	if len(result.Entries) > limit {
		result.Entries = result.Entries[:limit]
		result.Truncated = true
	}
	s.recordDeliveryLogAudit(ctx, principal, applicationID, environmentID, "delivery.logs.query", "success", fmt.Sprintf("queried %d delivery log entries", len(result.Entries)), len(targets))
	return result, nil
}

func (s *Service) IssueApplicationEnvironmentLogStreamTicket(ctx context.Context, principal domainidentity.Principal, accessCtx domainidentity.AccessContext, applicationID, environmentID string, query domainresource.LogQuery) (domainidentity.StreamTicket, error) {
	targets, err := s.resolveDeliveryLogTargets(ctx, principal, applicationID, environmentID, query)
	if err != nil {
		return domainidentity.StreamTicket{}, err
	}
	for _, target := range targets {
		if err := s.logs.AuthorizeClusterLogs(ctx, principal, target.clusterID, target.query, true); err != nil {
			return domainidentity.StreamTicket{}, err
		}
	}
	if s.logTickets == nil {
		return domainidentity.StreamTicket{}, fmt.Errorf("%w: stream ticket issuer is not configured", apperrors.ErrClusterUnready)
	}
	return s.logTickets.IssueStreamTicket(ctx, principal, accessCtx, domainidentity.StreamTicketRequest{
		Path: deliveryLogStreamPath(applicationID, environmentID),
		Metadata: map[string]any{
			deliveryLogApplicationMetadataKey: applicationID,
			deliveryLogEnvironmentMetadataKey: environmentID,
			deliveryLogQueryMetadataKey:       query,
		},
	})
}

func (s *Service) StreamApplicationEnvironmentLogsFromTicket(ctx context.Context, principal domainidentity.Principal, accessCtx domainidentity.AccessContext, applicationID, environmentID string, emit func(domainresource.LogStreamEvent) error) error {
	query, err := deliveryLogQueryFromTicket(accessCtx, applicationID, environmentID)
	if err != nil {
		return err
	}
	targets, err := s.resolveDeliveryLogTargets(ctx, principal, applicationID, environmentID, query)
	if err != nil {
		return err
	}
	for _, target := range targets {
		if err := s.logs.AuthorizeClusterLogs(ctx, principal, target.clusterID, target.query, true); err != nil {
			return err
		}
	}
	if err := emit(domainresource.LogStreamEvent{Type: "status", Status: &domainresource.LogStreamStatus{State: "live"}}); err != nil {
		return err
	}

	streamCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	events := make(chan domainresource.LogStreamEvent)
	errs := make(chan error, len(targets))
	var streams sync.WaitGroup
	for _, target := range targets {
		target := target
		streams.Add(1)
		go func() {
			defer streams.Done()
			errs <- s.logs.StreamClusterLogs(streamCtx, principal, target.clusterID, target.query, func(event domainresource.LogStreamEvent) error {
				if event.Type != "entry" || event.Entry == nil {
					return nil
				}
				entry := *event.Entry
				remapDeliveryLogEntry(&entry, applicationID, target.environmentKey)
				select {
				case events <- domainresource.LogStreamEvent{Type: "entry", Entry: &entry}:
					return nil
				case <-streamCtx.Done():
					return streamCtx.Err()
				}
			})
		}()
	}
	go func() {
		streams.Wait()
		close(events)
		close(errs)
	}()

	var streamFailures int
	for event := range events {
		if err := emit(event); err != nil {
			cancel()
			for range errs {
			}
			return err
		}
	}
	for streamErr := range errs {
		if streamErr != nil && !errors.Is(streamErr, context.Canceled) && !errors.Is(streamErr, context.DeadlineExceeded) {
			streamFailures++
		}
	}
	auditResult, auditSummary := "success", "streamed delivery logs"
	if streamFailures > 0 {
		auditResult, auditSummary = "failure", "one or more delivery log sources became unavailable"
		if err := emit(domainresource.LogStreamEvent{Type: "source_error", Status: &domainresource.LogStreamStatus{State: "degraded", Message: auditSummary}}); err != nil {
			return err
		}
	}
	s.recordDeliveryLogAudit(ctx, principal, applicationID, environmentID, "delivery.logs.stream", auditResult, auditSummary, len(targets))
	return emit(domainresource.LogStreamEvent{Type: "end", Status: &domainresource.LogStreamStatus{State: "ended"}})
}

func (s *Service) resolveDeliveryLogTargets(ctx context.Context, principal domainidentity.Principal, applicationID, environmentID string, query domainresource.LogQuery) ([]deliveryLogTarget, error) {
	if s.logs == nil {
		return nil, fmt.Errorf("%w: log runtime is not configured", apperrors.ErrClusterUnready)
	}
	applicationID = strings.TrimSpace(applicationID)
	environmentID = strings.TrimSpace(environmentID)
	app, err := s.applications.Get(ctx, principal, applicationID)
	if err != nil {
		return nil, err
	}
	binding, err := s.catalog.GetApplicationEnvironment(ctx, principal, environmentID)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(binding.ApplicationID) != strings.TrimSpace(app.ID) {
		return nil, fmt.Errorf("%w: application environment was not found", apperrors.ErrNotFound)
	}
	if query.Selector == nil {
		query.Selector = &domainresource.LogSourceSelector{}
	}
	if strings.TrimSpace(query.Selector.DockerService) != "" {
		return nil, fmt.Errorf("%w: docker selectors are not valid for delivery logs", apperrors.ErrInvalidArgument)
	}

	targets := make([]deliveryLogTarget, 0, len(binding.Targets))
	seen := make(map[string]struct{}, len(binding.Targets))
	for _, target := range binding.Targets {
		targetQuery, ok := deliveryTargetQuery(query, target)
		if !ok {
			continue
		}
		clusterID := strings.TrimSpace(target.ClusterID)
		key := clusterID + "\x00" + targetQuery.Selector.Namespace + "\x00" + targetQuery.Selector.WorkloadKind + "\x00" + targetQuery.Selector.WorkloadName + "\x00" + strings.Join(targetQuery.Selector.Containers, "\x00")
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		targets = append(targets, deliveryLogTarget{clusterID: clusterID, environmentKey: binding.EnvironmentKey, query: targetQuery})
	}
	if len(targets) == 0 {
		return nil, fmt.Errorf("%w: no enabled delivery log targets matched the query", apperrors.ErrNotFound)
	}
	if len(targets) > deliveryLogMaxTargets {
		return nil, fmt.Errorf("%w: delivery log query resolves more than %d targets", apperrors.ErrInvalidArgument, deliveryLogMaxTargets)
	}
	return targets, nil
}

func deliveryTargetQuery(query domainresource.LogQuery, target domaincatalog.ReleaseTarget) (domainresource.LogQuery, bool) {
	clusterID := strings.TrimSpace(target.ClusterID)
	namespace := strings.TrimSpace(target.Namespace)
	workloadKind := strings.TrimSpace(target.WorkloadKind)
	workloadName := strings.TrimSpace(target.WorkloadName)
	if !target.Enabled || clusterID == "" || namespace == "" || workloadKind == "" || workloadName == "" {
		return query, false
	}
	selector := *query.Selector
	if requested := strings.TrimSpace(selector.Namespace); requested != "" && requested != namespace {
		return query, false
	}
	if requested := strings.TrimSpace(selector.WorkloadKind); requested != "" && !strings.EqualFold(requested, workloadKind) {
		return query, false
	}
	if requested := strings.TrimSpace(selector.WorkloadName); requested != "" && requested != workloadName {
		return query, false
	}
	selector.Namespace = namespace
	selector.WorkloadKind = workloadKind
	selector.WorkloadName = workloadName
	if container := strings.TrimSpace(target.ContainerName); container != "" {
		if len(selector.Containers) > 0 && !containsTrimmedString(selector.Containers, container) {
			return query, false
		}
		selector.Containers = []string{container}
		selector.AllContainers = false
	}
	query.Selector = &selector
	return query, true
}

func containsTrimmedString(values []string, target string) bool {
	for _, value := range values {
		if strings.TrimSpace(value) == target {
			return true
		}
	}
	return false
}

func remapDeliveryLogPage(page *domainresource.LogPage, applicationID, environmentKey string) {
	for index := range page.Entries {
		remapDeliveryLogEntry(&page.Entries[index], applicationID, environmentKey)
	}
	for index := range page.Warnings {
		if page.Warnings[index].Source != nil {
			source := *page.Warnings[index].Source
			source.Domain = sohaapi.LogSourceDomainDelivery
			source.ApplicationID = applicationID
			source.EnvironmentKey = environmentKey
			page.Warnings[index].Source = &source
		}
	}
}

func remapDeliveryLogEntry(entry *domainresource.LogEntry, applicationID, environmentKey string) {
	entry.Source.Domain = sohaapi.LogSourceDomainDelivery
	entry.Source.ApplicationID = applicationID
	entry.Source.EnvironmentKey = environmentKey
}

func deliveryLogStreamPath(applicationID, environmentID string) string {
	return "/api/v1/delivery/applications/" + url.PathEscape(strings.TrimSpace(applicationID)) + "/environments/" + url.PathEscape(strings.TrimSpace(environmentID)) + "/logs/stream"
}

func deliveryLogQueryFromTicket(accessCtx domainidentity.AccessContext, applicationID, environmentID string) (domainresource.LogQuery, error) {
	if accessCtx.TokenKind != "stream_ticket" {
		return domainresource.LogQuery{}, fmt.Errorf("%w: delivery log stream requires a stream ticket", apperrors.ErrUnauthorized)
	}
	boundApplicationID, _ := accessCtx.Metadata[deliveryLogApplicationMetadataKey].(string)
	boundEnvironmentID, _ := accessCtx.Metadata[deliveryLogEnvironmentMetadataKey].(string)
	if strings.TrimSpace(boundApplicationID) != strings.TrimSpace(applicationID) || strings.TrimSpace(boundEnvironmentID) != strings.TrimSpace(environmentID) {
		return domainresource.LogQuery{}, fmt.Errorf("%w: stream ticket does not match delivery environment", apperrors.ErrUnauthorized)
	}
	rawQuery, ok := accessCtx.Metadata[deliveryLogQueryMetadataKey]
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

func (s *Service) recordDeliveryLogAudit(ctx context.Context, principal domainidentity.Principal, applicationID, environmentID, action, result, summary string, targetCount int) {
	if s.audit == nil {
		return
	}
	meta := requestctx.FromContext(ctx)
	_ = s.audit.Record(ctx, domainaudit.Entry{
		ActorID: principal.UserID, ActorName: principal.UserName, Roles: principal.Roles, Teams: principal.Teams,
		ResourceKind: "ApplicationEnvironment", ResourceName: environmentID, Action: action, Result: result, Summary: summary,
		RequestPath: meta.Path, RequestMethod: meta.Method, RequestID: meta.RequestID, SourceIP: meta.SourceIP,
		Metadata: map[string]any{"source": meta.Source, "applicationId": applicationID, "applicationEnvironmentId": environmentID, "targetCount": targetCount},
	})
}
