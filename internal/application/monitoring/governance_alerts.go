package monitoring

import (
	"fmt"
	"strings"

	domainalert "github.com/opensoha/soha/internal/domain/alert"
	domaingovernance "github.com/opensoha/soha/internal/domain/governance"
	"github.com/opensoha/soha/internal/platform/redaction"
)

func governanceAlertEventInput(input domaingovernance.AlertInput) domainalert.AlertEventInput {
	source := strings.ToLower(strings.TrimSpace(input.Source))
	if source == "" {
		source = "governance"
	}
	labels := map[string]string{
		"category": "governance",
		"source":   source,
	}
	for key, value := range input.Labels {
		if strings.TrimSpace(value) != "" {
			labels[key] = strings.TrimSpace(value)
		}
	}
	annotations := map[string]string{
		"eventId":       input.EventID,
		"actorId":       input.ActorID,
		"actorName":     input.ActorName,
		"action":        input.Action,
		"operationType": input.OperationType,
		"requestPath":   input.RequestPath,
		"requestMethod": input.RequestMethod,
		"requestId":     input.RequestID,
		"sourceIp":      input.SourceIP,
		"resourceKind":  input.ResourceKind,
		"resourceName":  input.ResourceName,
	}
	for key, value := range input.Annotations {
		if strings.TrimSpace(value) != "" {
			if redaction.SensitiveKey(key) {
				annotations[key] = redaction.Redacted
				continue
			}
			annotations[key] = redaction.Text(strings.TrimSpace(value))
		}
	}
	return domainalert.AlertEventInput{
		ID:           firstNonEmptyString(input.ID, fmt.Sprintf("governance:%s:%s", source, input.EventID)),
		SourceType:   "governance",
		SourceSystem: domaingovernance.AlertSourceSystem,
		Fingerprint:  governanceAlertFingerprint(input, source),
		Title:        governanceAlertTitle(input, source),
		Summary:      redaction.Text(strings.TrimSpace(input.Summary)),
		Severity:     firstNonEmptyString(strings.ToLower(strings.TrimSpace(input.Severity)), "warning"),
		Status:       "firing",
		ClusterID:    strings.TrimSpace(input.ClusterID),
		Namespace:    strings.TrimSpace(input.Namespace),
		Labels:       compactLabels(labels),
		Annotations:  compactLabels(annotations),
		CurrentState: "firing",
		StartsAt:     input.CreatedAt,
		LastSeenAt:   input.CreatedAt,
	}
}

func governanceAlertFingerprint(input domaingovernance.AlertInput, source string) string {
	parts := []string{
		"governance",
		source,
		firstNonEmptyString(input.Action, input.OperationType, "unknown"),
		firstNonEmptyString(input.RequestID, input.EventID, "unknown"),
	}
	return strings.Join(parts, ":")
}

func governanceAlertTitle(input domaingovernance.AlertInput, source string) string {
	action := firstNonEmptyString(input.Action, input.OperationType, "governance event")
	result := strings.TrimSpace(input.Result)
	if result == "" {
		return fmt.Sprintf("Governance %s alert: %s", source, action)
	}
	return fmt.Sprintf("Governance %s %s: %s", source, result, action)
}

func compactLabels(values map[string]string) map[string]string {
	out := make(map[string]string, len(values))
	for key, value := range values {
		if strings.TrimSpace(value) != "" {
			out[key] = strings.TrimSpace(value)
		}
	}
	return out
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
