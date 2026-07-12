package traces

import (
	"fmt"
	"time"
)

func normalizeSlowSpanQuery(query Query) Query {
	if query.TimeTo.IsZero() {
		query.TimeTo = time.Now().UTC()
	}
	if query.TimeFrom.IsZero() {
		query.TimeFrom = query.TimeTo.Add(-60 * time.Minute)
	}
	if query.MinDuration <= 0 {
		query.MinDuration = 250 * time.Millisecond
	}
	if query.Limit <= 0 {
		query.Limit = 20
	}
	return query
}

type traceHotspot struct {
	service     string
	operation   string
	count       int
	maxDuration float64
	errorCount  int
}

func traceHotspots(spans []Span) []map[string]any {
	groups := make(map[string]*traceHotspot)
	for _, span := range spans {
		key := fmt.Sprintf("%s::%s", span.Service, span.Operation)
		group := groups[key]
		if group == nil {
			group = &traceHotspot{service: span.Service, operation: span.Operation}
			groups[key] = group
		}
		group.count++
		if span.DurationMS > group.maxDuration {
			group.maxDuration = span.DurationMS
		}
		if span.Error {
			group.errorCount++
		}
	}
	items := make([]map[string]any, 0, len(groups))
	for _, group := range groups {
		items = append(items, map[string]any{
			"service": group.service, "operation": group.operation,
			"count": group.count, "maxDuration": group.maxDuration,
			"errorCount": group.errorCount,
		})
	}
	return items
}
