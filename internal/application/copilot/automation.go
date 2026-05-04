package copilot

import (
	"context"
	"fmt"
	"strings"
	"time"

	domainalert "github.com/kubecrux/kubecrux/internal/domain/alert"
	domaincopilot "github.com/kubecrux/kubecrux/internal/domain/copilot"
)

const automationRootCauseCreatedBy = "system:automation"

func (s *Service) HandleAlertAutomation(ctx context.Context, instance domainalert.Instance) error {
	policies, err := s.repo.ListAutomationPolicies(ctx)
	if err != nil {
		return err
	}
	for _, policy := range policies {
		if !policy.Enabled || policy.TriggerType != "alert_webhook" {
			continue
		}
		matched, err := s.matchesAlertAutomationPolicy(instance, policy)
		if err != nil || !matched {
			continue
		}
		dedupKey := buildAlertAutomationDedupKey(policy.ID, instance)
		existing, err := s.repo.ListRootCauseRuns(ctx, automationRootCauseCreatedBy, domaincopilot.RootCauseRunFilter{
			AlertID:  instance.ID,
			DedupKey: dedupKey,
			Limit:    5,
		})
		if err == nil && withinDedupWindow(existing, policy.DedupWindowSeconds) {
			continue
		}
		kinds := policy.AnalysisKinds
		if len(kinds) == 0 {
			kinds = []string{"root_cause"}
		}
		for _, kind := range kinds {
			switch strings.TrimSpace(kind) {
			case "performance", "trace":
				_, _, _ = s.analyzeConversation(ctx, systemPrincipal(), domaincopilot.Session{
					ID:        "automation:" + policy.ID,
					Title:     instance.Title,
					CreatedBy: automationRootCauseCreatedBy,
					Metadata: map[string]any{
						"mode": kind,
						"scope": map[string]any{
							"clusterId":        instance.ClusterID,
							"namespace":        instance.Namespace,
							"workload":         resolveAlertWorkload(instance),
							"alertId":          instance.ID,
							"timeRangeMinutes": policyTimeRangeMinutes(policy),
						},
					},
				}, fmt.Sprintf("Investigate alert %s", instance.ID), "en-US")
			default:
				_, err = s.executeRootCauseRun(ctx, systemPrincipal(), automationRootCauseCreatedBy, domaincopilot.RootCauseRunInput{
					Kind:              "root_cause",
					Title:             instance.Title,
					AnalysisProfileID: policy.AnalysisProfileID,
					TriggerType:       "alert_webhook",
					ClusterID:         instance.ClusterID,
					Namespace:         instance.Namespace,
					WorkloadKind:      "Deployment",
					WorkloadName:      resolveAlertWorkload(instance),
					AlertID:           instance.ID,
					TimeRangeMinutes:  policyTimeRangeMinutes(policy),
					Question:          fmt.Sprintf("Investigate alert %s", instance.ID),
				}, dedupKey, "en-US")
			}
			if err != nil {
				return err
			}
		}
	}
	return nil
}

func (s *Service) matchesAlertAutomationPolicy(instance domainalert.Instance, policy domaincopilot.AutomationPolicy) (bool, error) {
	conditions := policy.TriggerConditions
	if len(conditions) == 0 {
		return true, nil
	}
	if severities := stringSliceCondition(conditions["severity"]); len(severities) > 0 && !containsString(severities, instance.Severity) {
		return false, nil
	}
	if statuses := stringSliceCondition(conditions["status"]); len(statuses) > 0 && !containsString(statuses, instance.Status) {
		return false, nil
	}
	if labels, ok := conditions["labels"].(map[string]any); ok {
		for key, rawValue := range labels {
			if instance.Labels[key] != fmt.Sprint(rawValue) {
				return false, nil
			}
		}
	}
	if minDuration := intCondition(conditions["min_duration_seconds"]); minDuration > 0 {
		if instance.StartsAt.IsZero() {
			return false, nil
		}
		if time.Since(instance.StartsAt) < time.Duration(minDuration)*time.Second {
			return false, nil
		}
	}
	return true, nil
}

func buildAlertAutomationDedupKey(policyID string, instance domainalert.Instance) string {
	return strings.Join([]string{policyID, instance.Fingerprint, instance.ClusterID, instance.Namespace}, ":")
}

func withinDedupWindow(runs []domaincopilot.RootCauseRun, dedupWindowSeconds int) bool {
	if dedupWindowSeconds <= 0 {
		dedupWindowSeconds = 900
	}
	windowStart := time.Now().UTC().Add(-time.Duration(dedupWindowSeconds) * time.Second)
	for _, item := range runs {
		if item.CreatedAt.After(windowStart) || item.CreatedAt.Equal(windowStart) {
			return true
		}
	}
	return false
}

func resolveAlertWorkload(instance domainalert.Instance) string {
	for _, key := range []string{"workload", "deployment", "app", "service"} {
		if value := strings.TrimSpace(instance.Labels[key]); value != "" {
			return value
		}
	}
	return ""
}

func policyTimeRangeMinutes(policy domaincopilot.AutomationPolicy) int {
	if value := intCondition(policy.TriggerConditions["time_range_minutes"]); value > 0 {
		return value
	}
	return 60
}

func stringSliceCondition(value any) []string {
	switch current := value.(type) {
	case []string:
		return current
	case []any:
		items := make([]string, 0, len(current))
		for _, item := range current {
			items = append(items, fmt.Sprint(item))
		}
		return items
	default:
		return nil
	}
}

func intCondition(value any) int {
	switch current := value.(type) {
	case int:
		return current
	case int64:
		return int(current)
	case float64:
		return int(current)
	default:
		return 0
	}
}
