package logs

import "strings"

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
