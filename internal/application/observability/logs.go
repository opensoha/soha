package observability

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	sohaapi "github.com/opensoha/soha-contracts/gen/go/sohaapi"
	domainaudit "github.com/opensoha/soha/internal/domain/audit"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainobservability "github.com/opensoha/soha/internal/domain/observability"
	domainresource "github.com/opensoha/soha/internal/domain/resource"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"github.com/opensoha/soha/internal/platform/requestctx"
	"github.com/opensoha/soha/internal/platform/telemetry"
)

type durableCursor struct {
	DataSourceID string `json:"dataSourceId"`
	PrincipalID  string `json:"principalId"`
	ClusterID    string `json:"clusterId"`
	Namespace    string `json:"namespace"`
	QueryHash    string `json:"queryHash"`
	Provider     string `json:"provider"`
	ExpiresAt    int64  `json:"expiresAt"`
}

func (s *Service) QueryDurableLogs(ctx context.Context, principal domainidentity.Principal, clusterID string, query domainresource.LogQuery) (domainresource.LogPage, error) {
	item, err := s.selectDataSource(ctx, clusterID, query.Selector.Namespace)
	if err != nil {
		return domainresource.LogPage{}, err
	}
	budget := apiBudget(item.QueryBudget)
	search, err := durableSearchQuery(query, clusterID, budget, s.now().UTC())
	if err != nil {
		return domainresource.LogPage{}, err
	}
	queryHash := durableQueryHash(clusterID, query)
	if query.Cursor != "" {
		cursor, cursorErr := s.verifyCursor(query.Cursor)
		if cursorErr != nil || cursor.DataSourceID != item.ID || cursor.PrincipalID != principal.UserID || cursor.ClusterID != clusterID || cursor.Namespace != query.Selector.Namespace || cursor.QueryHash != queryHash {
			return domainresource.LogPage{}, fmt.Errorf("%w: invalid or expired log cursor", apperrors.ErrInvalidArgument)
		}
		search.PageToken = cursor.Provider
	}
	config, err := s.runtimeConfig(item)
	if err != nil {
		return domainresource.LogPage{}, fmt.Errorf("%w: data-source credentials are unavailable", apperrors.ErrClusterUnready)
	}
	queryCtx, cancel := context.WithTimeout(ctx, time.Duration(budget.TimeoutSeconds)*time.Second)
	startedAt := s.now()
	result, err := s.logs.Search(queryCtx, item.BackendType, item.ID, config, search)
	cancel()
	if err != nil {
		s.recordLogQuery(ctx, principal, item, clusterID, query.Selector.Namespace, 0, false, "failure", s.now().Sub(startedAt))
		return domainresource.LogPage{}, fmt.Errorf("%w: durable log backend query failed", apperrors.ErrClusterUnready)
	}
	entries := make([]domainresource.LogEntry, 0, len(result.Records))
	for _, record := range result.Records {
		entries = append(entries, durableLogEntry(record, query, item.RedactionPolicy))
	}
	nextCursor := ""
	if result.NextPageToken != "" {
		nextCursor, err = s.signCursor(durableCursor{
			DataSourceID: item.ID, PrincipalID: principal.UserID, ClusterID: clusterID, Namespace: query.Selector.Namespace,
			QueryHash: queryHash, Provider: result.NextPageToken, ExpiresAt: s.now().UTC().Add(15 * time.Minute).Unix(),
		})
		if err != nil {
			return domainresource.LogPage{}, fmt.Errorf("%w: durable log cursor signing is unavailable", apperrors.ErrClusterUnready)
		}
	}
	s.recordLogQuery(ctx, principal, item, clusterID, query.Selector.Namespace, len(entries), result.Truncated, "success", s.now().Sub(startedAt))
	return domainresource.LogPage{
		Entries: entries, NextCursor: nextCursor, Partial: false, Truncated: result.Truncated, ScopeRestricted: false,
		Coverage:      &domainresource.LogCoverage{ResolvedSources: 1, SuccessfulSources: 1, FailedSources: 0},
		RetentionHint: "持久化范围由数据源保留策略决定。",
	}, nil
}

func (s *Service) selectDataSource(ctx context.Context, clusterID, namespace string) (domainobservability.DataSource, error) {
	items, err := s.dataSources.ListDataSources(ctx)
	if err != nil {
		return domainobservability.DataSource{}, err
	}
	for _, item := range items {
		if item.SourceKind == dataSourceKindLogs && item.Enabled && scopeAllows(item.Scope, "clusterIds", clusterID) && scopeAllows(item.Scope, "namespaces", namespace) {
			return item, nil
		}
	}
	return domainobservability.DataSource{}, fmt.Errorf("%w: durable log data source is not configured for this scope", apperrors.ErrClusterUnready)
}

func durableSearchQuery(query domainresource.LogQuery, clusterID string, budget sohaapi.ObservabilityLogQueryBudget, now time.Time) (telemetry.LogSearchQuery, error) {
	from, to := now.Add(-15*time.Minute), now
	if query.From != nil {
		from = query.From.UTC()
	}
	if query.To != nil {
		to = query.To.UTC()
	}
	if from.After(to) {
		return telemetry.LogSearchQuery{}, fmt.Errorf("%w: from must not be after to", apperrors.ErrInvalidArgument)
	}
	if to.Sub(from) > time.Duration(budget.MaxRangeSeconds)*time.Second {
		return telemetry.LogSearchQuery{}, fmt.Errorf("%w: durable log time range exceeds the data-source budget", apperrors.ErrInvalidArgument)
	}
	limit := query.Limit
	if limit <= 0 {
		limit = budget.MaxEntries
	}
	limit = min(limit, budget.MaxEntries)
	direction := strings.TrimSpace(string(query.Direction))
	if direction == "" {
		direction = "backward"
	}
	selector := query.Selector
	if selector == nil {
		return telemetry.LogSearchQuery{}, fmt.Errorf("%w: log selector is required", apperrors.ErrInvalidArgument)
	}
	if len(selector.PodNames) > 1 || len(selector.Containers) > 1 {
		return telemetry.LogSearchQuery{}, fmt.Errorf("%w: durable logs support one pod and one container per query", apperrors.ErrInvalidArgument)
	}
	if strings.TrimSpace(selector.LabelSelector) != "" {
		return telemetry.LogSearchQuery{}, fmt.Errorf("%w: durable logs do not support Kubernetes label selectors", apperrors.ErrInvalidArgument)
	}
	scope := telemetry.LogScope{ClusterID: clusterID, Namespace: selector.Namespace, Workload: selector.WorkloadName}
	if len(selector.PodNames) == 1 {
		scope.Pod = selector.PodNames[0]
	}
	if len(selector.Containers) == 1 {
		scope.Container = selector.Containers[0]
	}
	return telemetry.LogSearchQuery{Scope: scope, TimeFrom: from, TimeTo: to, Query: query.Text, Severities: query.Severities, Limit: limit, Direction: direction}, nil
}

func durableLogEntry(record telemetry.LogRecord, query domainresource.LogQuery, redaction map[string]any) domainresource.LogEntry {
	attributes := flattenLogAttributes(record.Attributes, droppedAttributeKeys(redaction))
	return domainresource.LogEntry{
		Timestamp: record.Timestamp.UTC(), Message: record.Message, Severity: record.Severity, Attributes: attributes,
		SourceMode: sohaapi.LogSourceModeDurable,
		Source: domainresource.LogSource{
			Domain: sohaapi.LogSourceDomainKubernetes, ClusterID: record.ClusterID, Namespace: record.Namespace,
			WorkloadKind: query.Selector.WorkloadKind, WorkloadName: firstNonEmpty(record.Workload, query.Selector.WorkloadName),
			PodName: record.Pod, ContainerName: record.Container,
		},
		TraceID: firstNonEmpty(attributes["traceId"], attributes["trace_id"]),
		SpanID:  firstNonEmpty(attributes["spanId"], attributes["span_id"]),
	}
}

func scopeAllows(scope map[string]any, key, requested string) bool {
	values := stringSlice(scope[key])
	if len(values) == 0 {
		return true
	}
	for _, value := range values {
		if value == requested {
			return true
		}
	}
	return false
}

func stringSlice(value any) []string {
	switch values := value.(type) {
	case []string:
		return values
	case []any:
		result := make([]string, 0, len(values))
		for _, value := range values {
			result = append(result, fmt.Sprint(value))
		}
		return result
	default:
		return nil
	}
}

func durableQueryHash(clusterID string, query domainresource.LogQuery) string {
	query.Cursor = ""
	payload, _ := json.Marshal(struct {
		ClusterID string                  `json:"clusterId"`
		Query     domainresource.LogQuery `json:"query"`
	}{ClusterID: clusterID, Query: query})
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:])
}

func (s *Service) signCursor(cursor durableCursor) (string, error) {
	key := s.keys.Active()
	if key.ID() == "" {
		return "", fmt.Errorf("cursor signing key is not configured")
	}
	payload, err := json.Marshal(cursor)
	if err != nil {
		return "", err
	}
	encodedKeyID := base64.RawURLEncoding.EncodeToString([]byte(key.ID()))
	encoded := base64.RawURLEncoding.EncodeToString(payload)
	signed := encodedKeyID + "." + encoded
	mac := hmac.New(sha256.New, []byte(key.Secret()))
	_, _ = mac.Write([]byte(signed))
	return signed + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil)), nil
}

func (s *Service) verifyCursor(value string) (durableCursor, error) {
	parts := strings.Split(value, ".")
	if len(parts) != 3 {
		return durableCursor{}, fmt.Errorf("invalid cursor")
	}
	keyID, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return durableCursor{}, err
	}
	key, ok := s.keys.Find(string(keyID), s.now().UTC())
	if !ok {
		return durableCursor{}, fmt.Errorf("invalid cursor key")
	}
	signature, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return durableCursor{}, err
	}
	mac := hmac.New(sha256.New, []byte(key.Secret()))
	_, _ = mac.Write([]byte(parts[0] + "." + parts[1]))
	if subtle.ConstantTimeCompare(signature, mac.Sum(nil)) != 1 {
		return durableCursor{}, fmt.Errorf("invalid cursor signature")
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return durableCursor{}, err
	}
	var cursor durableCursor
	if err := json.Unmarshal(payload, &cursor); err != nil || cursor.ExpiresAt <= s.now().UTC().Unix() {
		return durableCursor{}, fmt.Errorf("expired cursor")
	}
	return cursor, nil
}

func droppedAttributeKeys(policy map[string]any) map[string]struct{} {
	result := map[string]struct{}{}
	for _, key := range stringSlice(policy["dropAttributeKeys"]) {
		result[key] = struct{}{}
	}
	return result
}

func flattenLogAttributes(value map[string]any, dropped map[string]struct{}) map[string]string {
	result := map[string]string{}
	var visit func(string, any)
	visit = func(prefix string, value any) {
		if len(result) >= 100 {
			return
		}
		if _, drop := dropped[prefix]; drop {
			return
		}
		switch current := value.(type) {
		case map[string]any:
			for key, item := range current {
				name := key
				if prefix != "" {
					name = prefix + "." + key
				}
				visit(name, item)
			}
		case map[string]string:
			for key, item := range current {
				name := key
				if prefix != "" {
					name = prefix + "." + key
				}
				visit(name, item)
			}
		default:
			text := fmt.Sprint(current)
			if prefix != "" && len(text) <= 4096 {
				result[prefix] = text
			}
		}
	}
	for key, item := range value {
		visit(key, item)
	}
	return result
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func (s *Service) recordLogQuery(ctx context.Context, principal domainidentity.Principal, item domainobservability.DataSource, clusterID, namespace string, count int, truncated bool, result string, duration time.Duration) {
	if s.audit == nil {
		return
	}
	metadata := requestctx.FromContext(ctx)
	summary := fmt.Sprintf("queried %d durable log entries from %s; truncated=%t durationMs=%d", count, item.BackendType, truncated, duration.Milliseconds())
	_ = s.audit.Record(ctx, domainaudit.Entry{ActorID: principal.UserID, ActorName: principal.UserName, Roles: principal.Roles, Teams: principal.Teams, ResourceKind: "ObservabilityDataSource", ResourceName: item.ID, Namespace: namespace, ClusterID: clusterID, Action: "logs.query", Result: result, Summary: summary, RequestPath: metadata.Path, RequestMethod: metadata.Method, RequestID: metadata.RequestID, SourceIP: metadata.SourceIP, CreatedAt: s.now().UTC()})
}
