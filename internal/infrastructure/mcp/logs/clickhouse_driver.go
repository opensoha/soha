package logs

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

type clickHouseDriver struct {
	http *http.Client
}

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
	endpoint, _ := config["endpoint"].(string)
	table, _ := config["table"].(string)
	if strings.TrimSpace(endpoint) == "" {
		return fmt.Errorf("clickhouse endpoint is required")
	}
	if strings.TrimSpace(table) == "" {
		return fmt.Errorf("clickhouse table is required")
	}
	return nil
}

func (d clickHouseDriver) Correlate(ctx context.Context, sourceID string, config map[string]any, query CorrelationQuery) (CorrelationResult, error) {
	if err := d.ValidateConfig(config); err != nil {
		return CorrelationResult{}, err
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
	limit := intConfig(config, "correlationLimit", query.Limit)
	if limit <= 0 {
		limit = 20
	}

	sql := buildClickHouseCorrelationSQL(strings.TrimSpace(table), timestampField, messageField, severityField, serviceField, workloadField, namespaceField, clusterField, query, limit)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(strings.TrimSpace(endpoint), "/"), strings.NewReader(sql))
	if err != nil {
		return CorrelationResult{}, err
	}
	req.Header.Set("Content-Type", "text/plain")
	if username := stringConfig(config, "username", ""); username != "" {
		req.SetBasicAuth(username, stringConfig(config, "password", ""))
	}
	resp, err := d.http.Do(req)
	if err != nil {
		return CorrelationResult{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return CorrelationResult{}, fmt.Errorf("clickhouse correlate failed with status %d", resp.StatusCode)
	}

	records := make([]Record, 0)
	scanner := bufio.NewScanner(resp.Body)
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
		return CorrelationResult{ErrorKind: "read_failed"}, fmt.Errorf("read clickhouse correlate response: %w", err)
	}
	signatures := summarizeSignatures(records)
	summary := "no correlated logs found"
	if len(records) > 0 {
		summary = fmt.Sprintf("%d correlated logs found", len(records))
	}
	return CorrelationResult{
		SourceID:   sourceID,
		Summary:    summary,
		Records:    records,
		Signatures: signatures,
		Truncated:  len(records) >= limit,
		QueryCost: map[string]any{
			"backendType": "clickhouse",
			"limit":       limit,
			"recordCount": len(records),
		},
		SampleWindow: map[string]any{
			"timeFrom": query.TimeFrom.Format(time.RFC3339),
			"timeTo":   query.TimeTo.Format(time.RFC3339),
		},
	}, nil
}

func buildClickHouseCorrelationSQL(table, timestampField, messageField, severityField, serviceField, workloadField, namespaceField, clusterField string, query CorrelationQuery, limit int) string {
	conditions := make([]string, 0)
	if !query.TimeFrom.IsZero() {
		conditions = append(conditions, fmt.Sprintf("%s >= parseDateTimeBestEffort('%s')", timestampField, quoteLiteral(query.TimeFrom.Format(time.RFC3339))))
	}
	if !query.TimeTo.IsZero() {
		conditions = append(conditions, fmt.Sprintf("%s <= parseDateTimeBestEffort('%s')", timestampField, quoteLiteral(query.TimeTo.Format(time.RFC3339))))
	}
	for field, value := range map[string]string{
		clusterField:   query.Scope.ClusterID,
		namespaceField: query.Scope.Namespace,
		serviceField:   query.Scope.Service,
		workloadField:  query.Scope.Workload,
	} {
		if strings.TrimSpace(field) == "" || strings.TrimSpace(value) == "" {
			continue
		}
		conditions = append(conditions, fmt.Sprintf("%s = '%s'", field, quoteLiteral(value)))
	}
	terms := correlationTerms(query)
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
	return fmt.Sprintf(`
SELECT
    %s AS timestamp,
    %s AS severity,
    %s AS message,
    %s AS service,
    %s AS workload,
    %s AS namespace,
    %s AS cluster
FROM %s
WHERE %s
ORDER BY %s DESC
LIMIT %d
FORMAT JSONEachRow
`, timestampField, severityField, messageField, serviceField, workloadField, namespaceField, clusterField, table, where, timestampField, limit)
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
		Severity:  fmt.Sprint(payload["severity"]),
		Message:   fmt.Sprint(payload["message"]),
		Service:   fmt.Sprint(payload["service"]),
		Workload:  fmt.Sprint(payload["workload"]),
		Namespace: fmt.Sprint(payload["namespace"]),
		ClusterID: fmt.Sprint(payload["cluster"]),
		Attributes: map[string]any{
			"row": payload,
		},
	}
	return record
}
