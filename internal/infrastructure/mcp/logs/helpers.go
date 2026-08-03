package logs

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
)

const maxSearchEntries = 1000

type timestampCursor struct {
	Timestamp time.Time `json:"timestamp"`
	Skip      int       `json:"skip"`
}

func severityFromMessage(message string) string {
	lower := strings.ToLower(strings.TrimSpace(message))
	switch {
	case strings.Contains(lower, "panic"), strings.Contains(lower, "fatal"), strings.Contains(lower, "critical"):
		return "critical"
	case strings.Contains(lower, "error"), strings.Contains(lower, "exception"), strings.Contains(lower, "timeout"), strings.Contains(lower, "refused"), strings.Contains(lower, "unavailable"):
		return "warning"
	default:
		return "info"
	}
}

func normalizeLogSeverity(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	switch value {
	case "critical", "fatal", "panic":
		return "critical"
	case "warn", "warning", "error":
		return "warning"
	case "info", "debug", "trace":
		return "info"
	default:
		return ""
	}
}

func correlationTerms(query CorrelationQuery) []string {
	items := make([]string, 0)
	for _, item := range []string{query.Query, query.AlertID, query.Workload, query.Scope.Workload} {
		item = strings.TrimSpace(item)
		if item != "" {
			items = append(items, item)
		}
	}
	return uniqueStrings(items)
}

func normalizeSearchQuery(query SearchQuery) (SearchQuery, timestampCursor, error) {
	if query.Limit <= 0 {
		query.Limit = 100
	}
	if query.Limit > maxSearchEntries {
		query.Limit = maxSearchEntries
	}
	query.Direction = strings.ToLower(strings.TrimSpace(query.Direction))
	if query.Direction == "" {
		query.Direction = "backward"
	}
	if query.Direction != "backward" && query.Direction != "forward" {
		return SearchQuery{}, timestampCursor{}, fmt.Errorf("unsupported log search direction")
	}
	query.Terms = uniqueStrings(append(query.Terms, query.Query))
	query.Severities = uniqueStrings(query.Severities)
	cursor, err := decodeTimestampCursor(query.PageToken)
	if err != nil {
		return SearchQuery{}, timestampCursor{}, err
	}
	if cursor.Skip < 0 || cursor.Skip > maxSearchEntries {
		return SearchQuery{}, timestampCursor{}, fmt.Errorf("invalid log search page token")
	}
	return query, cursor, nil
}

func searchQueryFromCorrelation(query CorrelationQuery) SearchQuery {
	return SearchQuery{
		Scope:     query.Scope,
		TimeFrom:  query.TimeFrom,
		TimeTo:    query.TimeTo,
		TraceID:   query.TraceID,
		SpanID:    query.SpanID,
		Terms:     correlationTerms(query),
		Limit:     query.Limit,
		Direction: "backward",
	}
}

func scopePodValues(scope Scope) []string {
	if pods := uniqueStrings(scope.Pods); len(pods) > 0 {
		return pods
	}
	return uniqueStrings([]string{scope.Pod})
}

func scopeContainerValues(scope Scope) []string {
	if containers := uniqueStrings(scope.Containers); len(containers) > 0 {
		return containers
	}
	return uniqueStrings([]string{scope.Container})
}

func correlationResultFromSearch(sourceID string, query CorrelationQuery, result SearchResult) CorrelationResult {
	summary := "no correlated logs found"
	if len(result.Records) > 0 {
		summary = fmt.Sprintf("%d correlated logs found", len(result.Records))
	}
	return CorrelationResult{
		SourceID:     sourceID,
		Summary:      summary,
		Records:      result.Records,
		Signatures:   summarizeSignatures(result.Records),
		Truncated:    result.Truncated,
		QueryCost:    result.QueryCost,
		ErrorKind:    result.ErrorKind,
		SampleWindow: map[string]any{"timeFrom": query.TimeFrom.Format(time.RFC3339), "timeTo": query.TimeTo.Format(time.RFC3339)},
	}
}

func decodeTimestampCursor(value string) (timestampCursor, error) {
	if strings.TrimSpace(value) == "" {
		return timestampCursor{}, nil
	}
	payload, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		return timestampCursor{}, fmt.Errorf("invalid log search page token")
	}
	var cursor timestampCursor
	if err := json.Unmarshal(payload, &cursor); err != nil || cursor.Timestamp.IsZero() {
		return timestampCursor{}, fmt.Errorf("invalid log search page token")
	}
	return cursor, nil
}

func encodeTimestampCursor(cursor timestampCursor) string {
	payload, _ := json.Marshal(cursor)
	return base64.RawURLEncoding.EncodeToString(payload)
}

func providerFetchLimit(query SearchQuery, cursor timestampCursor) int {
	if len(query.Severities) > 0 {
		return maxSearchEntries
	}
	return min(maxSearchEntries+1, query.Limit+cursor.Skip+1)
}

func paginateRecords(records []Record, query SearchQuery, cursor timestampCursor) ([]Record, string, bool) {
	sort.SliceStable(records, func(i, j int) bool {
		if query.Direction == "forward" {
			return records[i].Timestamp.Before(records[j].Timestamp)
		}
		return records[i].Timestamp.After(records[j].Timestamp)
	})
	if !cursor.Timestamp.IsZero() && cursor.Skip > 0 {
		skipped := 0
		filtered := records[:0]
		for _, record := range records {
			if record.Timestamp.Equal(cursor.Timestamp) && skipped < cursor.Skip {
				skipped++
				continue
			}
			filtered = append(filtered, record)
		}
		records = filtered
	}
	hasMore := len(records) > query.Limit
	if hasMore {
		records = records[:query.Limit]
	}
	if !hasMore || len(records) == 0 {
		return records, "", false
	}
	last := records[len(records)-1].Timestamp
	skip := 0
	if last.Equal(cursor.Timestamp) {
		skip = cursor.Skip
	}
	for _, record := range records {
		if record.Timestamp.Equal(last) {
			skip++
		}
	}
	return records, encodeTimestampCursor(timestampCursor{Timestamp: last, Skip: skip}), true
}

func validateHTTPConfig(config map[string]any, backend string) error {
	endpoint := stringConfig(config, "endpoint", "")
	parsed, err := url.Parse(endpoint)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return fmt.Errorf("%s endpoint must be an HTTP(S) URL", backend)
	}
	return nil
}

func filterSeverities(records []Record, severities []string) []Record {
	if len(severities) == 0 {
		return records
	}
	allowed := make(map[string]struct{}, len(severities))
	for _, severity := range severities {
		if normalized := normalizeLogSeverity(severity); normalized != "" {
			allowed[normalized] = struct{}{}
		}
	}
	filtered := records[:0]
	for _, record := range records {
		if _, ok := allowed[normalizeLogSeverity(record.Severity)]; ok {
			filtered = append(filtered, record)
		}
	}
	return filtered
}

func applyProviderHeaders(req *http.Request, config map[string]any) {
	if bearerToken := stringConfig(config, "bearerToken", ""); bearerToken != "" {
		req.Header.Set("Authorization", "Bearer "+bearerToken)
	}
	if tenantID := stringConfig(config, "tenantId", ""); tenantID != "" {
		req.Header.Set("X-Scope-OrgID", tenantID)
	}
}
