package logs

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"time"
)

type clickHouseDriver struct {
	http *http.Client
}

var clickHouseIdentifierPattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_.]*$`)

func newClickHouseDriver() Driver {
	return clickHouseDriver{http: &http.Client{Timeout: 8 * time.Second}}
}

func (clickHouseDriver) BackendType() string {
	return "clickhouse"
}

func (clickHouseDriver) ValidateConfig(config map[string]any) error {
	if config == nil {
		return fmt.Errorf("clickhouse config is required")
	}
	if err := validateHTTPConfig(config, "clickhouse"); err != nil {
		return err
	}
	table, _ := config["table"].(string)
	if strings.TrimSpace(table) == "" {
		return fmt.Errorf("clickhouse table is required")
	}
	for _, key := range []string{"table", "timestampField", "messageField", "severityField", "serviceField", "workloadField", "namespaceField", "clusterField", "podField", "containerField", "traceIdField", "spanIdField"} {
		if value := stringConfig(config, key, ""); value != "" && !clickHouseIdentifierPattern.MatchString(value) {
			return fmt.Errorf("clickhouse %s is not a valid identifier", key)
		}
	}
	return nil
}

func (d clickHouseDriver) Correlate(ctx context.Context, sourceID string, config map[string]any, query CorrelationQuery) (CorrelationResult, error) {
	result, err := d.Search(ctx, sourceID, config, searchQueryFromCorrelation(query))
	if err != nil {
		return CorrelationResult{}, err
	}
	return correlationResultFromSearch(sourceID, query, result), nil
}

func (d clickHouseDriver) Search(ctx context.Context, sourceID string, config map[string]any, query SearchQuery) (SearchResult, error) {
	if err := d.ValidateConfig(config); err != nil {
		return SearchResult{}, err
	}
	query, cursor, err := normalizeSearchQuery(query)
	if err != nil {
		return SearchResult{}, err
	}
	endpoint, _ := config["endpoint"].(string)
	table, _ := config["table"].(string)
	timestampField := stringConfig(config, "timestampField", "timestamp")
	messageField := stringConfig(config, "messageField", "message")
	severityField := stringConfig(config, "severityField", "severity")
	serviceField := stringConfig(config, "serviceField", "service")
	workloadField := stringConfig(config, "workloadField", "workload")
	namespaceField := stringConfig(config, "namespaceField", "namespace")
	clusterField := stringConfig(config, "clusterField", "cluster")
	podField := stringConfig(config, "podField", "pod")
	containerField := stringConfig(config, "containerField", "container")
	traceIDField := stringConfig(config, "traceIdField", "trace_id")
	spanIDField := stringConfig(config, "spanIdField", "span_id")

	sql := buildClickHouseSearchSQL(strings.TrimSpace(table), timestampField, messageField, severityField, serviceField, workloadField, namespaceField, clusterField, podField, containerField, traceIDField, spanIDField, query, cursor, providerFetchLimit(query, cursor))
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(strings.TrimSpace(endpoint), "/"), strings.NewReader(sql))
	if err != nil {
		return SearchResult{}, err
	}
	req.Header.Set("Content-Type", "text/plain")
	applyProviderHeaders(req, config)
	if username := stringConfig(config, "username", ""); username != "" {
		req.SetBasicAuth(username, stringConfig(config, "password", ""))
	}
	resp, err := d.http.Do(req)
	if err != nil {
		return SearchResult{}, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode >= 300 {
		return SearchResult{}, fmt.Errorf("clickhouse search failed with status %d", resp.StatusCode)
	}

	records := make([]Record, 0)
	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 64*1024), 2*1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.TrimSpace(line) == "" {
			continue
		}
		record := parseClickHouseJSONEachRow(line)
		if record.Severity == "" || record.Severity == "info" {
			record.Severity = severityFromMessage(record.Message)
		}
		records = append(records, record)
	}
	if err := scanner.Err(); err != nil {
		return SearchResult{ErrorKind: "read_failed"}, fmt.Errorf("read clickhouse search response: %w", err)
	}
	records = filterSeverities(records, query.Severities)
	records, nextPageToken, truncated := paginateRecords(records, query, cursor)
	return SearchResult{
		SourceID:      sourceID,
		Records:       records,
		NextPageToken: nextPageToken,
		Truncated:     truncated,
		QueryCost: map[string]any{
			"backendType": "clickhouse",
			"limit":       query.Limit,
			"recordCount": len(records),
		},
	}, nil
}

func buildClickHouseSearchSQL(table, timestampField, messageField, severityField, serviceField, workloadField, namespaceField, clusterField, podField, containerField, traceIDField, spanIDField string, query SearchQuery, cursor timestampCursor, limit int) string {
	conditions := make([]string, 0)
	pods := scopePodValues(query.Scope)
	pod := ""
	if len(pods) == 1 {
		pod = pods[0]
	}
	containers := scopeContainerValues(query.Scope)
	container := ""
	if len(containers) == 1 {
		container = containers[0]
	}
	if !query.TimeFrom.IsZero() {
		conditions = append(conditions, fmt.Sprintf("%s >= parseDateTimeBestEffort('%s')", timestampField, quoteLiteral(query.TimeFrom.Format(time.RFC3339))))
	}
	if !query.TimeTo.IsZero() {
		conditions = append(conditions, fmt.Sprintf("%s <= parseDateTimeBestEffort('%s')", timestampField, quoteLiteral(query.TimeTo.Format(time.RFC3339))))
	}
	if !cursor.Timestamp.IsZero() {
		operator := "<="
		if query.Direction == "forward" {
			operator = ">="
		}
		conditions = append(conditions, fmt.Sprintf("%s %s parseDateTime64BestEffort('%s')", timestampField, operator, quoteLiteral(cursor.Timestamp.Format(time.RFC3339Nano))))
	}
	for field, value := range map[string]string{
		clusterField:   query.Scope.ClusterID,
		namespaceField: query.Scope.Namespace,
		serviceField:   query.Scope.Service,
		workloadField:  query.Scope.Workload,
		podField:       pod,
		containerField: container,
		traceIDField:   query.TraceID,
		spanIDField:    query.SpanID,
	} {
		if strings.TrimSpace(field) == "" || strings.TrimSpace(value) == "" {
			continue
		}
		conditions = append(conditions, fmt.Sprintf("%s = '%s'", field, quoteLiteral(value)))
	}
	if len(pods) > 1 && strings.TrimSpace(podField) != "" {
		values := make([]string, 0, len(pods))
		for _, value := range pods {
			values = append(values, "'"+quoteLiteral(value)+"'")
		}
		conditions = append(conditions, fmt.Sprintf("%s IN (%s)", podField, strings.Join(values, ", ")))
	}
	if len(containers) > 1 && strings.TrimSpace(containerField) != "" {
		values := make([]string, 0, len(containers))
		for _, value := range containers {
			values = append(values, "'"+quoteLiteral(value)+"'")
		}
		conditions = append(conditions, fmt.Sprintf("%s IN (%s)", containerField, strings.Join(values, ", ")))
	}
	terms := query.Terms
	if len(terms) > 0 {
		textConditions := make([]string, 0, len(terms))
		for _, term := range terms {
			textConditions = append(textConditions, fmt.Sprintf("positionCaseInsensitiveUTF8(%s, '%s') > 0", messageField, quoteLiteral(term)))
		}
		conditions = append(conditions, "("+strings.Join(textConditions, " OR ")+")")
	}
	where := "1"
	if len(conditions) > 0 {
		where = strings.Join(conditions, " AND ")
	}
	direction := "DESC"
	if query.Direction == "forward" {
		direction = "ASC"
	}
	return fmt.Sprintf(`
SELECT
    %s AS timestamp,
    %s AS severity,
    %s AS message,
    %s AS service,
    %s AS workload,
    %s AS namespace,
    %s AS cluster,
    %s AS pod,
    %s AS container,
    %s AS trace_id,
    %s AS span_id
FROM %s
WHERE %s
ORDER BY %s %s
LIMIT %d
FORMAT JSONEachRow
`, timestampField, severityField, messageField, serviceField, workloadField, namespaceField, clusterField, podField, containerField, traceIDField, spanIDField, table, where, timestampField, direction, limit)
}

func quoteLiteral(value string) string {
	value = strings.ReplaceAll(value, `\`, `\\`)
	value = strings.ReplaceAll(value, `'`, `''`)
	return value
}

func parseClickHouseJSONEachRow(line string) Record {
	var payload map[string]any
	_ = json.Unmarshal([]byte(line), &payload)
	record := Record{
		Timestamp: timeValue(payload, "timestamp"),
		Severity:  nestedString(payload, "severity"),
		Message:   nestedString(payload, "message"),
		Service:   nestedString(payload, "service"),
		Workload:  nestedString(payload, "workload"),
		Namespace: nestedString(payload, "namespace"),
		ClusterID: nestedString(payload, "cluster"),
		Pod:       nestedString(payload, "pod"),
		Container: nestedString(payload, "container"),
		TraceID:   nestedString(payload, "trace_id"),
		SpanID:    nestedString(payload, "span_id"),
		Attributes: map[string]any{
			"row": payload,
		},
	}
	return record
}
