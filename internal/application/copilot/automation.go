package copilot

import (
	"context"
	"fmt"
	"strings"
	"time"

	domainalert "github.com/opensoha/soha/internal/domain/alert"
	domaincopilot "github.com/opensoha/soha/internal/domain/copilot"
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
		policyRuns, err := s.repo.ListRootCauseRuns(ctx, automationRootCauseCreatedBy, domaincopilot.RootCauseRunFilter{
			TriggerType:    "alert_webhook",
			DedupKeyPrefix: buildAlertAutomationDedupPrefix(policy.ID),
			Limit:          1,
		})
		if err == nil && withinCooldownWindow(policyRuns, policy.CooldownSeconds) {
			continue
		}
		policyAgentRuns, err := s.repo.ListAgentRuns(ctx, domaincopilot.AgentRunFilter{
			CreatedBy:      automationRootCauseCreatedBy,
			TriggerType:    "alert_webhook",
			DedupKeyPrefix: buildAlertAutomationDedupPrefix(policy.ID),
			Limit:          1,
		})
		if err == nil && withinAgentRunCooldownWindow(policyAgentRuns, policy.CooldownSeconds) {
			continue
		}
		dedupKey := buildAlertAutomationDedupKey(policy.ID, instance)
		existing, err := s.repo.ListRootCauseRuns(ctx, automationRootCauseCreatedBy, domaincopilot.RootCauseRunFilter{
			AlertID:     instance.ID,
			TriggerType: "alert_webhook",
			DedupKey:    dedupKey,
			Limit:       5,
		})
		if err == nil && withinDedupWindow(existing, policy.DedupWindowSeconds) {
			continue
		}
		existingAgentRuns, err := s.repo.ListAgentRuns(ctx, domaincopilot.AgentRunFilter{
			CreatedBy:   automationRootCauseCreatedBy,
			TriggerType: "alert_webhook",
			DedupKey:    dedupKey,
			Limit:       5,
		})
		if err == nil && withinAgentRunDedupWindow(existingAgentRuns, policy.DedupWindowSeconds) {
			continue
		}
		kinds := policy.AnalysisKinds
		if len(kinds) == 0 {
			kinds = []string{"root_cause"}
		}
		for _, kind := range kinds {
			var runErr error
			kind = strings.TrimSpace(kind)
			if s.shouldUseExternalAgent(policy.AgentProviderID) {
				if normalizeAnalysisKind(kind) == "root_cause" {
					_, runErr = s.queueRootCauseAgentRun(ctx, systemPrincipal(), automationRootCauseCreatedBy, domaincopilot.RootCauseRunInput{
						Kind:              "root_cause",
						Title:             instance.Title,
						AnalysisProfileID: policy.AnalysisProfileID,
						AgentProviderID:   policy.AgentProviderID,
						TriggerType:       "alert_webhook",
						ClusterID:         instance.ClusterID,
						Namespace:         instance.Namespace,
						WorkloadKind:      "Deployment",
						WorkloadName:      resolveAlertWorkload(instance),
						AlertID:           instance.ID,
						TimeRangeMinutes:  policyTimeRangeMinutes(policy),
						Question:          fmt.Sprintf("Investigate alert %s", instance.ID),
					}, dedupKey, "en-US")
				} else {
					_, runErr = s.queueAutomationAgentRun(ctx, policy, kind, instance, dedupKey)
				}
				if runErr != nil {
					return runErr
				}
				continue
			}
			switch kind {
			case "performance", "trace":
				_, _, _ = s.analyzeConversation(ctx, systemPrincipal(), domaincopilot.Session{
					ID:        "automation:" + policy.ID,
					Title:     instance.Title,
					CreatedBy: automationRootCauseCreatedBy,
					Metadata: map[string]any{
						"mode": kind,
						"pinnedContext": map[string]any{
							"automationPolicyId": policy.ID,
							"dedupKey":           dedupKey,
							"triggerType":        "alert_webhook",
						},
						"scope": map[string]any{
							"clusterId":        instance.ClusterID,
							"namespace":        instance.Namespace,
							"workload":         resolveAlertWorkload(instance),
							"alertId":          instance.ID,
							"timeRangeMinutes": policyTimeRangeMinutes(policy),
						},
					},
				}, fmt.Sprintf("Investigate alert %s", instance.ID), "en-US")
			case "root_cause":
				_, runErr = s.executeRootCauseRun(ctx, systemPrincipal(), automationRootCauseCreatedBy, domaincopilot.RootCauseRunInput{
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
				}, dedupKey, domaincopilot.SessionToolset{}, "en-US")
			default:
				continue
			}
			if runErr != nil {
				return runErr
			}
		}
	}
	return nil
}

func (s *Service) queueAutomationAgentRun(ctx context.Context, policy domaincopilot.AutomationPolicy, kind string, instance domainalert.Instance, dedupKey string) (domaincopilot.AgentRun, error) {
	kind = normalizeAnalysisKind(kind)
	scope := domaincopilot.SessionScope{
		ClusterID:        instance.ClusterID,
		Namespace:        instance.Namespace,
		Workload:         resolveAlertWorkload(instance),
		AlertID:          instance.ID,
		TimeRangeMinutes: policyTimeRangeMinutes(policy),
	}
	profile, _ := s.repo.GetAnalysisProfile(ctx, policy.AnalysisProfileID)
	toolset := domaincopilot.SessionToolset{
		EnabledAdapterIDs: profile.EnabledSources,
		EnabledSkillIDs:   profile.EnabledPlaybooks,
		BudgetOverrides: map[string]any{
			"timeoutSeconds":   profile.TimeoutSeconds,
			"maxEvidenceItems": intCondition(profile.QueryBudgets["maxEvidenceItems"]),
		},
		ScopeOverrides: map[string]any{
			"clusterId":        scope.ClusterID,
			"namespace":        scope.Namespace,
			"workload":         scope.Workload,
			"alertId":          scope.AlertID,
			"timeRangeMinutes": scope.TimeRangeMinutes,
		},
	}
	return s.createAgentRun(ctx, systemPrincipal(), domaincopilot.AgentRunInput{
		ProviderID:   policy.AgentProviderID,
		CapabilityID: kind,
		SkillIDs:     automationAgentSkillIDs(kind, toolset.EnabledSkillIDs),
		CreatedBy:    automationRootCauseCreatedBy,
		Scope:        scope,
		Toolset:      toolset,
		Input: map[string]any{
			"question":           fmt.Sprintf("Investigate alert %s", instance.ID),
			"mode":               kind,
			"analysisProfileId":  policy.AnalysisProfileID,
			"analysisProfile":    profile.Name,
			"triggerType":        "alert_webhook",
			"automationPolicyId": policy.ID,
			"dedupKey":           dedupKey,
			"alert": map[string]any{
				"id":           instance.ID,
				"fingerprint":  instance.Fingerprint,
				"title":        instance.Title,
				"summary":      instance.Summary,
				"severity":     instance.Severity,
				"status":       instance.Status,
				"source":       instance.Source,
				"labels":       instance.Labels,
				"annotations":  instance.Annotations,
				"generatorUrl": instance.GeneratorURL,
				"startsAt":     instance.StartsAt,
				"lastSeenAt":   instance.LastSeenAt,
			},
			"capabilityId": kind,
		},
		TimeoutSeconds: firstPositive(profile.TimeoutSeconds, 600),
	})
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

func buildAlertAutomationDedupPrefix(policyID string) string {
	policyID = strings.TrimSpace(policyID)
	if policyID == "" {
		return ""
	}
	return policyID + ":"
}

func withinDedupWindow(runs []domaincopilot.RootCauseRun, dedupWindowSeconds int) bool {
	if dedupWindowSeconds <= 0 {
		dedupWindowSeconds = 900
	}
	return withinRunWindow(runs, dedupWindowSeconds)
}

func withinCooldownWindow(runs []domaincopilot.RootCauseRun, cooldownSeconds int) bool {
	if cooldownSeconds <= 0 {
		return false
	}
	return withinRunWindow(runs, cooldownSeconds)
}

func withinAgentRunDedupWindow(runs []domaincopilot.AgentRun, dedupWindowSeconds int) bool {
	if dedupWindowSeconds <= 0 {
		dedupWindowSeconds = 900
	}
	return withinAgentRunWindow(runs, dedupWindowSeconds)
}

func withinAgentRunCooldownWindow(runs []domaincopilot.AgentRun, cooldownSeconds int) bool {
	if cooldownSeconds <= 0 {
		return false
	}
	return withinAgentRunWindow(runs, cooldownSeconds)
}

func withinRunWindow(runs []domaincopilot.RootCauseRun, windowSeconds int) bool {
	windowStart := time.Now().UTC().Add(-time.Duration(windowSeconds) * time.Second)
	for _, item := range runs {
		if item.CreatedAt.After(windowStart) || item.CreatedAt.Equal(windowStart) {
			return true
		}
	}
	return false
}

func withinAgentRunWindow(runs []domaincopilot.AgentRun, windowSeconds int) bool {
	windowStart := time.Now().UTC().Add(-time.Duration(windowSeconds) * time.Second)
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

func automationAgentSkillIDs(kind string, configured []string) []string {
	if len(configured) > 0 {
		values := normalizeStringList(configured)
		out := make([]string, 0, len(values)+1)
		seen := map[string]struct{}{}
		for _, value := range values {
			trimmed := strings.TrimSpace(value)
			if trimmed == "" {
				continue
			}
			out = append(out, trimmed)
			seen[trimmed] = struct{}{}
		}
		providerSkillID := agentProviderSkillIDForCapability(kind, "")
		if providerSkillID != "" {
			if _, ok := seen[providerSkillID]; !ok {
				out = append(out, providerSkillID)
			}
		}
		return out
	}
	if providerSkillID := agentProviderSkillIDForCapability(kind, ""); providerSkillID != "" {
		return []string{providerSkillID}
	}
	return []string{strings.TrimSpace(kind) + "-analysis"}
}

func firstPositive(values ...int) int {
	for _, value := range values {
		if value > 0 {
			return value
		}
	}
	return 0
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
