package monitoring

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"text/template"
	"time"

	appaccess "github.com/kubecrux/kubecrux/internal/application/access"
	domainalert "github.com/kubecrux/kubecrux/internal/domain/alert"
	domainidentity "github.com/kubecrux/kubecrux/internal/domain/identity"
	domainworkflow "github.com/kubecrux/kubecrux/internal/domain/workflow"
	"github.com/kubecrux/kubecrux/internal/platform/apperrors"
)

type WorkflowExecutor interface {
	ExecuteSystemDAG(context.Context, domainidentity.Principal, string, string, string, map[string]any, domainworkflow.Input, map[string]any) (domainworkflow.Run, error)
	GetSystemRun(context.Context, string) (domainworkflow.Run, error)
}

type ruleMatch struct {
	Fingerprint string
	Title       string
	Summary     string
	Severity    string
	ClusterID   string
	Namespace   string
	Labels      map[string]string
}

func (s *Service) Start(ctx context.Context) {
	s.startMu.Lock()
	if s.started {
		s.startMu.Unlock()
		return
	}
	s.started = true
	interval := s.ruleInterval
	if interval <= 0 {
		interval = time.Minute
	}
	s.startMu.Unlock()

	go func() {
		s.evaluateEnabledRules(ctx)
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				s.evaluateEnabledRules(ctx)
			}
		}
	}()
}

func (s *Service) ListRuleRuns(ctx context.Context, principal domainidentity.Principal, filter domainalert.AlertRuleRunFilter) ([]domainalert.AlertRuleRun, error) {
	if err := s.authorize(ctx, principal, appaccess.PermObserveAlertRulesView); err != nil {
		return nil, err
	}
	if s.repo == nil {
		return []domainalert.AlertRuleRun{}, nil
	}
	return s.repo.ListRuleRuns(ctx, filter)
}

func (s *Service) PreviewNotificationPolicy(ctx context.Context, principal domainidentity.Principal, policyID, eventID string) ([]map[string]any, error) {
	if err := s.authorize(ctx, principal, appaccess.PermObserveNotificationsView); err != nil {
		return nil, err
	}
	policy, err := s.findNotificationPolicy(ctx, policyID)
	if err != nil {
		return nil, err
	}
	event, err := s.repo.GetEvent(ctx, strings.TrimSpace(eventID))
	if err != nil {
		return nil, err
	}
	return s.buildNotificationOutputs(ctx, policy, event), nil
}

func (s *Service) GetCurrentOnCall(ctx context.Context, principal domainidentity.Principal, ref string) (map[string]any, error) {
	if err := s.authorize(ctx, principal, appaccess.PermObserveOncallView); err != nil {
		return nil, err
	}
	return s.resolveCurrentOnCall(ctx, ref)
}

func (s *Service) evaluateEnabledRules(ctx context.Context) {
	if s == nil || s.repo == nil || s.dataSources == nil {
		return
	}
	rules, err := s.repo.ListRules(ctx)
	if err != nil {
		return
	}
	for _, rule := range rules {
		if !rule.Enabled {
			continue
		}
		s.evaluateRuleRun(ctx, rule)
	}
}

func (s *Service) evaluateRuleRun(ctx context.Context, rule domainalert.AlertRule) {
	startedAt := time.Now().UTC()
	result, err := s.evaluateRule(ctx, rule)
	durationMs := int(time.Since(startedAt).Milliseconds())
	matches := buildRuleMatches(rule, result)
	runInput := domainalert.AlertRuleRunInput{
		RuleID:     rule.ID,
		Status:     "clear",
		Summary:    result.Summary,
		Matched:    result.Matched,
		DurationMs: durationMs,
		Result: map[string]any{
			"samples":             result.Samples,
			"dataSources":         result.DataSources,
			"notificationPreview": result.NotificationPreview,
			"matches":             buildRuleMatchPayloads(matches),
		},
	}
	if err != nil {
		runInput.Status = "error"
		runInput.Error = err.Error()
		runInput.Summary = err.Error()
		runInput.Result = map[string]any{}
	} else if result.Matched {
		runInput.Status = "matched"
	}
	run, createErr := s.repo.CreateRuleRun(ctx, runInput)
	if createErr != nil || err != nil {
		return
	}
	if !result.Matched {
		s.resolveInternalRuleEvents(ctx, rule, nil, startedAt)
		return
	}
	firingFingerprints := make([]string, 0, len(matches))
	for _, match := range matches {
		satisfied, matchStart := s.ruleWindowSatisfied(ctx, rule, match.Fingerprint)
		if !satisfied {
			continue
		}
		firingFingerprints = append(firingFingerprints, match.Fingerprint)
		event := s.upsertInternalRuleEvent(ctx, rule, result, run, matchStart, match)
		if event.ID == "" {
			continue
		}
		_, _ = s.fanOutEvent(ctx, event)
	}
	if len(firingFingerprints) > 0 {
		s.resolveInternalRuleEvents(ctx, rule, firingFingerprints, startedAt)
	}
}

func (s *Service) ruleWindowSatisfied(ctx context.Context, rule domainalert.AlertRule, fingerprint string) (bool, time.Time) {
	now := time.Now().UTC()
	if rule.ForSeconds <= 0 {
		return true, now
	}
	intervalSeconds := int(s.ruleInterval.Seconds())
	if intervalSeconds <= 0 {
		intervalSeconds = 60
	}
	runs, err := s.repo.ListRuleRuns(ctx, domainalert.AlertRuleRunFilter{
		RuleID: rule.ID,
		Limit:  maxInt(10, rule.ForSeconds/intervalSeconds+5),
	})
	if err != nil || len(runs) == 0 {
		return false, time.Time{}
	}
	oldest := time.Time{}
	seenLatestMatch := false
	for _, run := range runs {
		matched, found := ruleRunMatchedFingerprint(run, fingerprint)
		if !matched || !found {
			break
		}
		if !seenLatestMatch {
			seenLatestMatch = true
		}
		oldest = run.CreatedAt
	}
	if !seenLatestMatch || oldest.IsZero() {
		return false, time.Time{}
	}
	if rule.ForSeconds <= intervalSeconds {
		return true, oldest
	}
	return now.Sub(oldest) >= time.Duration(rule.ForSeconds)*time.Second, oldest
}

func (s *Service) resolveInternalRuleEvents(ctx context.Context, rule domainalert.AlertRule, activeFingerprints []string, now time.Time) {
	events, err := s.repo.ListEvents(ctx, domainalert.AlertEventFilter{RuleID: rule.ID, Limit: 200})
	if err != nil {
		return
	}
	activeSet := make(map[string]struct{}, len(activeFingerprints))
	for _, item := range activeFingerprints {
		activeSet[item] = struct{}{}
	}
	for _, event := range events {
		if event.SourceType != "internal_rule" {
			continue
		}
		if _, ok := activeSet[event.Fingerprint]; ok {
			continue
		}
		if event.Status == "resolved" && event.CurrentState == "resolved" {
			continue
		}
		event.Status = "resolved"
		event.CurrentState = "resolved"
		event.EndsAt = now
		event.LastSeenAt = now
		event.UpdatedAt = now
		_, _ = s.repo.UpdateEvent(ctx, event.ID, toAlertEventInput(event))
	}
}

func (s *Service) upsertInternalRuleEvent(ctx context.Context, rule domainalert.AlertRule, result domainalert.RuleTestResult, run domainalert.AlertRuleRun, matchStart time.Time, match ruleMatch) domainalert.AlertEvent {
	now := time.Now().UTC()
	eventID := internalRuleEventID(rule, match.Fingerprint)
	event, err := s.repo.GetEvent(ctx, eventID)
	if err != nil {
		event = domainalert.AlertEvent{
			ID:          eventID,
			Fingerprint: match.Fingerprint,
			StartsAt:    matchStart,
			CreatedAt:   now,
		}
	}
	status := event.Status
	currentState := event.CurrentState
	if status == "" || status == "resolved" {
		status = "firing"
	}
	if currentState == "" || currentState == "resolved" {
		currentState = status
	}
	event.RuleID = rule.ID
	event.SourceType = "internal_rule"
	event.SourceSystem = "kubecrux"
	event.Fingerprint = match.Fingerprint
	event.Title = firstNonEmpty(match.Title, strings.TrimSpace(rule.Name), "Alert Rule")
	event.Summary = firstNonEmpty(match.Summary, strings.TrimSpace(result.Summary), event.Title)
	event.Severity = firstNonEmpty(match.Severity, normalizeRuleSeverity(rule, result), "warning")
	event.Status = status
	event.ClusterID = firstNonEmpty(match.ClusterID, stringValue(rule.DatasourceSelector["clusterId"], ""))
	event.Namespace = firstNonEmpty(match.Namespace, stringValue(rule.DatasourceSelector["namespace"], ""))
	event.Labels = mergeLabelMaps(mergeLabelMaps(rule.Labels, match.Labels), map[string]string{
		"ruleId":     rule.ID,
		"ruleType":   rule.RuleType,
		"sourceType": "internal_rule",
		"matched":    fmt.Sprintf("%t", result.Matched),
	})
	event.Annotations = mergeLabelMaps(rule.Annotations, map[string]string{
		"ruleSummary": result.Summary,
	})
	event.CurrentState = currentState
	event.LastSeenAt = now
	event.UpdatedAt = now
	if event.StartsAt.IsZero() {
		event.StartsAt = matchStart
	}
	input := toAlertEventInput(event)
	item, createErr := s.repo.CreateEvent(ctx, input)
	if createErr == nil {
		return item
	}
	item, updateErr := s.repo.UpdateEvent(ctx, event.ID, input)
	if updateErr == nil {
		return item
	}
	return domainalert.AlertEvent{}
}

func (s *Service) enrichHealingRun(ctx context.Context, run domainalert.HealingRun) domainalert.HealingRun {
	if s.workflow == nil || strings.TrimSpace(run.WorkflowRunID) == "" {
		return run
	}
	workflowRun, err := s.workflow.GetSystemRun(ctx, run.WorkflowRunID)
	if err != nil {
		return run
	}
	run.WorkflowStatus = workflowRun.Status
	run.WorkflowSummary = summarizeWorkflowRun(workflowRun)
	if strings.TrimSpace(run.WorkflowStatus) != "" && run.Status != "rejected" && run.Status != "pending_approval" {
		run.Status = workflowRun.Status
	}
	if run.Result == nil {
		run.Result = map[string]any{}
	}
	run.Result["workflowStatus"] = run.WorkflowStatus
	run.Result["workflowSummary"] = run.WorkflowSummary
	return run
}

func (s *Service) resolveCurrentOnCall(ctx context.Context, ref string) (map[string]any, error) {
	ref = strings.TrimSpace(strings.TrimPrefix(ref, "schedule:"))
	if ref == "" {
		return nil, fmt.Errorf("%w: ref is required", apperrors.ErrInvalidArgument)
	}
	schedules, err := s.repo.ListOnCallSchedules(ctx)
	if err != nil {
		return nil, err
	}
	rotations, err := s.repo.ListOnCallRotations(ctx)
	if err != nil {
		return nil, err
	}
	policies, err := s.repo.ListOnCallEscalationPolicies(ctx)
	if err != nil {
		return nil, err
	}

	scheduleID := ref
	escalationRef := ""
	for _, policy := range policies {
		if policy.ID != ref {
			continue
		}
		escalationRef = policy.ID
		if len(policy.Steps) > 0 {
			if value := stringValue(policy.Steps[0]["scheduleId"], ""); value != "" {
				scheduleID = value
			}
		}
	}

	var schedule domainalert.OnCallSchedule
	found := false
	for _, item := range schedules {
		if item.ID == scheduleID {
			schedule = item
			found = true
			break
		}
	}
	if !found {
		return nil, fmt.Errorf("%w: oncall schedule not found", apperrors.ErrNotFound)
	}
	var rotation *domainalert.OnCallRotation
	for _, item := range rotations {
		if item.ScheduleID == schedule.ID && item.Enabled {
			copyItem := item
			rotation = &copyItem
			break
		}
	}
	if rotation == nil {
		return map[string]any{
			"ref":        ref,
			"scheduleId": schedule.ID,
			"schedule":   schedule.Name,
			"status":     "no_rotation",
		}, nil
	}
	now := time.Now().UTC()
	if strings.TrimSpace(schedule.TimeZone) != "" {
		if location, locErr := time.LoadLocation(strings.TrimSpace(schedule.TimeZone)); locErr == nil {
			now = now.In(location)
		}
	}
	shiftMinutes := intValue(rotation.RotationConfig["rotationMinutes"], 0)
	if shiftMinutes <= 0 {
		shiftMinutes = intValue(rotation.RotationConfig["shiftHours"], 24) * 60
	}
	if shiftMinutes <= 0 {
		shiftMinutes = 24 * 60
	}
	startAt := schedule.CreatedAt
	if text := stringValue(rotation.RotationConfig["startAt"], ""); text != "" {
		if parsed, parseErr := time.Parse(time.RFC3339, text); parseErr == nil {
			startAt = parsed
		}
	}
	if startAt.IsZero() {
		startAt = now
	}
	participants := normalizeStrings(rotation.Participants)
	if len(participants) == 0 {
		return map[string]any{
			"ref":        ref,
			"scheduleId": schedule.ID,
			"rotationId": rotation.ID,
			"status":     "no_participants",
		}, nil
	}
	elapsed := int(now.Sub(startAt).Minutes())
	if elapsed < 0 {
		elapsed = 0
	}
	index := 0
	if shiftMinutes > 0 {
		index = (elapsed / shiftMinutes) % len(participants)
	}
	windowStart := startAt.Add(time.Duration((elapsed/shiftMinutes)*shiftMinutes) * time.Minute)
	windowEnd := windowStart.Add(time.Duration(shiftMinutes) * time.Minute)
	return map[string]any{
		"ref":                ref,
		"scheduleId":         schedule.ID,
		"schedule":           schedule.Name,
		"rotationId":         rotation.ID,
		"rotation":           rotation.Name,
		"currentParticipant": participants[index],
		"nextParticipant":    participants[(index+1)%len(participants)],
		"shiftMinutes":       shiftMinutes,
		"windowStart":        windowStart.Format(time.RFC3339),
		"windowEnd":          windowEnd.Format(time.RFC3339),
		"escalationPolicyId": escalationRef,
	}, nil
}

func buildRuleMatches(rule domainalert.AlertRule, result domainalert.RuleTestResult) []ruleMatch {
	baseLabels := mergeLabelMaps(rule.Labels, map[string]string{
		"clusterId": stringValue(rule.DatasourceSelector["clusterId"], ""),
		"namespace": stringValue(rule.DatasourceSelector["namespace"], ""),
		"workload":  stringValue(rule.DatasourceSelector["workload"], ""),
		"service":   stringValue(rule.DatasourceSelector["service"], ""),
	})
	if len(rule.GroupBy) == 0 {
		return []ruleMatch{{
			Fingerprint: internalRuleFingerprint(rule.ID, nil),
			Title:       strings.TrimSpace(rule.Name),
			Summary:     result.Summary,
			Severity:    normalizeRuleSeverity(rule, result),
			ClusterID:   baseLabels["clusterId"],
			Namespace:   baseLabels["namespace"],
			Labels:      baseLabels,
		}}
	}

	matches := make([]ruleMatch, 0)
	seen := map[string]struct{}{}
	for _, sample := range result.Samples {
		labels := mergeLabelMaps(baseLabels, ruleMatchLabels(rule.GroupBy, baseLabels, sample))
		keyValues := make(map[string]string, len(rule.GroupBy))
		for _, key := range rule.GroupBy {
			keyValues[key] = labels[key]
		}
		fingerprint := internalRuleFingerprint(rule.ID, keyValues)
		if _, ok := seen[fingerprint]; ok {
			continue
		}
		seen[fingerprint] = struct{}{}
		matches = append(matches, ruleMatch{
			Fingerprint: fingerprint,
			Title:       strings.TrimSpace(rule.Name),
			Summary:     buildGroupedSummary(result.Summary, keyValues),
			Severity:    normalizeRuleSeverity(rule, result),
			ClusterID:   labels["clusterId"],
			Namespace:   labels["namespace"],
			Labels:      labels,
		})
	}
	if len(matches) == 0 {
		return []ruleMatch{{
			Fingerprint: internalRuleFingerprint(rule.ID, nil),
			Title:       strings.TrimSpace(rule.Name),
			Summary:     result.Summary,
			Severity:    normalizeRuleSeverity(rule, result),
			ClusterID:   baseLabels["clusterId"],
			Namespace:   baseLabels["namespace"],
			Labels:      baseLabels,
		}}
	}
	return matches
}

func ruleMatchLabels(groupBy []string, baseLabels map[string]string, sample map[string]any) map[string]string {
	items := map[string]string{}
	for _, key := range groupBy {
		if value := lookupGroupValue(key, sample, baseLabels); value != "" {
			items[key] = value
		}
	}
	if value := lookupGroupValue("clusterId", sample, baseLabels); value != "" {
		items["clusterId"] = value
	}
	if value := lookupGroupValue("namespace", sample, baseLabels); value != "" {
		items["namespace"] = value
	}
	return items
}

func lookupGroupValue(key string, sample map[string]any, fallback map[string]string) string {
	if value := strings.TrimSpace(fmt.Sprint(sample[key])); value != "" && value != "<nil>" {
		return value
	}
	if labels, ok := sample["labels"].(map[string]any); ok {
		if value := strings.TrimSpace(fmt.Sprint(labels[key])); value != "" && value != "<nil>" {
			return value
		}
	}
	if strings.HasPrefix(key, "label:") {
		labelKey := strings.TrimPrefix(key, "label:")
		if labels, ok := sample["labels"].(map[string]any); ok {
			if value := strings.TrimSpace(fmt.Sprint(labels[labelKey])); value != "" && value != "<nil>" {
				return value
			}
		}
		if value := strings.TrimSpace(fmt.Sprint(sample[labelKey])); value != "" && value != "<nil>" {
			return value
		}
		if value := strings.TrimSpace(fallback[labelKey]); value != "" {
			return value
		}
	}
	if value := strings.TrimSpace(fallback[key]); value != "" {
		return value
	}
	return ""
}

func buildGroupedSummary(summary string, keyValues map[string]string) string {
	if len(keyValues) == 0 {
		return summary
	}
	parts := make([]string, 0, len(keyValues))
	for key, value := range keyValues {
		if strings.TrimSpace(value) == "" {
			continue
		}
		parts = append(parts, fmt.Sprintf("%s=%s", key, value))
	}
	if len(parts) == 0 {
		return summary
	}
	return firstNonEmpty(summary, "rule matched") + " [" + strings.Join(parts, ", ") + "]"
}

func buildRuleMatchPayloads(matches []ruleMatch) []map[string]any {
	items := make([]map[string]any, 0, len(matches))
	for _, match := range matches {
		items = append(items, map[string]any{
			"fingerprint": match.Fingerprint,
			"title":       match.Title,
			"summary":     match.Summary,
			"severity":    match.Severity,
			"clusterId":   match.ClusterID,
			"namespace":   match.Namespace,
			"labels":      match.Labels,
			"matched":     true,
		})
	}
	return items
}

func ruleRunMatchedFingerprint(run domainalert.AlertRuleRun, fingerprint string) (bool, bool) {
	rawMatches, ok := run.Result["matches"]
	if !ok {
		return false, false
	}
	items, ok := rawMatches.([]any)
	if !ok {
		return false, false
	}
	for _, rawItem := range items {
		item, ok := rawItem.(map[string]any)
		if !ok {
			continue
		}
		if strings.TrimSpace(fmt.Sprint(item["fingerprint"])) != strings.TrimSpace(fingerprint) {
			continue
		}
		matched := false
		switch current := item["matched"].(type) {
		case bool:
			matched = current
		default:
			matched = strings.EqualFold(strings.TrimSpace(fmt.Sprint(current)), "true")
		}
		return matched, true
	}
	return false, false
}

func (s *Service) fanOutEvent(ctx context.Context, event domainalert.AlertEvent) (bool, error) {
	policies, err := s.repo.ListNotificationPolicies(ctx)
	if err != nil || len(policies) == 0 {
		return false, err
	}
	if silence, ok := s.firstMatchingSilenceForEvent(ctx, event); ok {
		_ = s.repo.CreateDeliveryLog(ctx, domainalert.DeliveryLog{
			ID:        "delivery:" + internalRuleFingerprint(event.RuleID, map[string]string{"eventId": event.ID}),
			AlertID:   event.ID,
			Status:    "silenced",
			Summary:   silence.Reason,
			Metadata:  map[string]any{"silenceId": silence.ID, "silenceName": silence.Name},
			CreatedAt: time.Now().UTC(),
		})
		return true, nil
	}

	handled := false
	for _, policy := range policies {
		if !policy.Enabled || !matchesNotificationPolicy(policy, event) {
			continue
		}
		if s.notificationCoolingDown(ctx, event, policy) {
			continue
		}
		handled = true
		for _, preview := range s.buildNotificationOutputs(ctx, policy, event) {
			if strings.TrimSpace(fmt.Sprint(preview["status"])) == "preview_failed" {
				_ = s.repo.CreateDeliveryLog(ctx, domainalert.DeliveryLog{
					ID:        "delivery:" + time.Now().UTC().Format("20060102150405.000000000"),
					AlertID:   event.ID,
					ChannelID: stringValue(preview["channelId"], ""),
					Status:    "failed",
					Summary:   stringValue(preview["summary"], "notification preview failed"),
					Metadata:  mergeAnyMaps(preview, map[string]any{"policyId": policy.ID, "fingerprint": event.Fingerprint}),
					CreatedAt: time.Now().UTC(),
				})
				continue
			}
			status, summary, metadata := s.sendNotificationOutput(ctx, preview)
			_ = s.repo.CreateDeliveryLog(ctx, domainalert.DeliveryLog{
				ID:        "delivery:" + time.Now().UTC().Format("20060102150405.000000000"),
				AlertID:   event.ID,
				ChannelID: stringValue(preview["channelId"], ""),
				Status:    status,
				Summary:   summary,
				Metadata:  mergeAnyMaps(metadata, map[string]any{"policyId": policy.ID, "fingerprint": event.Fingerprint}),
				CreatedAt: time.Now().UTC(),
			})
			now := time.Now().UTC()
			event.LastNotificationAt = now
			event.UpdatedAt = now
			_, _ = s.repo.UpdateEvent(ctx, event.ID, toAlertEventInput(event))
		}
		if containsString(policy.ProcessorChain, "self_heal_trigger") {
			_ = s.triggerSelfHealFromPolicy(ctx, event)
		}
	}
	return handled, nil
}

func (s *Service) notificationCoolingDown(ctx context.Context, event domainalert.AlertEvent, policy domainalert.NotificationPolicy) bool {
	if policy.CooldownSeconds <= 0 {
		return false
	}
	logs, err := s.repo.ListDeliveryLogs(ctx, domainalert.DeliveryFilter{AlertID: event.ID, Limit: 50})
	if err != nil {
		return false
	}
	windowStart := time.Now().UTC().Add(-time.Duration(policy.CooldownSeconds) * time.Second)
	for _, item := range logs {
		if item.CreatedAt.Before(windowStart) {
			continue
		}
		if item.Status != "delivered" {
			continue
		}
		if strings.TrimSpace(fmt.Sprint(item.Metadata["policyId"])) == policy.ID {
			return true
		}
	}
	return false
}

func (s *Service) buildNotificationOutputs(ctx context.Context, policy domainalert.NotificationPolicy, event domainalert.AlertEvent) []map[string]any {
	channels, err := s.repo.ListChannels(ctx)
	if err != nil || len(channels) == 0 {
		return []map[string]any{{"status": "preview_failed", "summary": "no notification channels available", "policyId": policy.ID}}
	}
	templates, _ := s.repo.ListNotificationTemplates(ctx)
	channelMap := make(map[string]domainalert.NotificationChannel, len(channels))
	for _, item := range channels {
		channelMap[item.ID] = item
	}
	oncall := map[string]any{}
	if strings.TrimSpace(policy.OnCallRef) != "" {
		if current, currentErr := s.resolveCurrentOnCall(ctx, policy.OnCallRef); currentErr == nil {
			oncall = current
		}
	}
	outputs := make([]map[string]any, 0, len(policy.ChannelRefs))
	for _, channelID := range policy.ChannelRefs {
		channel, ok := channelMap[channelID]
		if !ok || !channel.Enabled {
			outputs = append(outputs, map[string]any{
				"status":    "preview_failed",
				"summary":   "channel is missing or disabled",
				"policyId":  policy.ID,
				"channelId": channelID,
			})
			continue
		}
		outputs = append(outputs, s.renderNotificationOutput(policy, channel, templates, event, oncall))
	}
	return outputs
}

func (s *Service) sendNotificationOutput(ctx context.Context, output map[string]any) (string, string, map[string]any) {
	targetURL := stringValue(output["url"], "")
	if targetURL == "" {
		return "skipped", "channel does not expose a supported webhook url", output
	}
	method := firstNonEmpty(strings.TrimSpace(stringValue(output["method"], "")), http.MethodPost)
	body := stringValue(output["body"], "{}")
	req, err := http.NewRequestWithContext(ctx, method, targetURL, strings.NewReader(body))
	if err != nil {
		return "failed", err.Error(), output
	}
	headers, _ := output["headers"].(map[string]string)
	for key, value := range headers {
		req.Header.Set(key, value)
	}
	if req.Header.Get("Content-Type") == "" {
		req.Header.Set("Content-Type", firstNonEmpty(strings.TrimSpace(stringValue(output["contentType"], "")), "application/json"))
	}
	resp, err := s.httpClient.Do(req)
	if err != nil {
		return "failed", err.Error(), output
	}
	defer resp.Body.Close()
	if resp.StatusCode >= http.StatusBadRequest {
		return "failed", fmt.Sprintf("delivery failed with status %d", resp.StatusCode), output
	}
	return "delivered", "alert delivered by notification policy", output
}

func (s *Service) renderNotificationOutput(policy domainalert.NotificationPolicy, channel domainalert.NotificationChannel, templates []domainalert.NotificationTemplate, event domainalert.AlertEvent, oncall map[string]any) map[string]any {
	targetURL := resolveChannelURL(channel)
	templateItem := resolveNotificationTemplate(channel, templates)
	data := map[string]any{
		"event":   event,
		"alert":   event,
		"policy":  policy,
		"channel": channel,
		"oncall":  oncall,
	}
	body := ""
	contentType := "application/json"
	if templateItem.ID != "" {
		contentType = firstNonEmpty(strings.TrimSpace(templateItem.ContentType), contentType)
		rendered, err := renderTemplateString(templateItem.BodyTemplate, data)
		if err != nil {
			return map[string]any{"status": "preview_failed", "summary": err.Error(), "policyId": policy.ID, "channelId": channel.ID}
		}
		body = rendered
	}
	if strings.TrimSpace(body) == "" {
		payload, _ := json.Marshal(map[string]any{
			"policyId": policy.ID,
			"event":    event,
			"oncall":   oncall,
		})
		body = string(payload)
	}
	headers := renderStringMap(mergeAnyMaps(templateItem.Headers, mapFromAny(channel.Config["headers"])), data)
	queryParams := renderStringMap(mergeAnyMaps(templateItem.QueryParams, mapFromAny(channel.Config["queryParams"])), data)
	finalURL := applyQueryParams(targetURL, queryParams)
	return map[string]any{
		"policyId":     policy.ID,
		"channelId":    channel.ID,
		"channelType":  channel.ChannelType,
		"url":          finalURL,
		"method":       firstNonEmpty(stringValue(channel.Config["method"], ""), http.MethodPost),
		"contentType":  firstNonEmpty(stringValue(channel.Config["contentType"], ""), contentType),
		"body":         body,
		"headers":      headers,
		"queryParams":  queryParams,
		"templateId":   templateItem.ID,
		"templateType": templateItem.TemplateType,
		"oncall":       oncall,
		"summary":      firstNonEmpty(event.Summary, event.Title),
		"status":       "preview_ready",
	}
}

func (s *Service) firstMatchingSilenceForEvent(ctx context.Context, event domainalert.AlertEvent) (domainalert.AlertSilence, bool) {
	silences, err := s.repo.ListSilences(ctx)
	if err != nil {
		return domainalert.AlertSilence{}, false
	}
	instance := domainalert.Instance{
		ID:          event.ID,
		Severity:    event.Severity,
		Status:      event.Status,
		ClusterID:   event.ClusterID,
		Namespace:   event.Namespace,
		Labels:      event.Labels,
		Annotations: event.Annotations,
	}
	return firstMatchingSilence(silences, instance, time.Now().UTC())
}

func (s *Service) triggerSelfHealFromPolicy(ctx context.Context, event domainalert.AlertEvent) error {
	if strings.TrimSpace(event.RuleID) == "" {
		return nil
	}
	rule, err := s.repo.GetRule(ctx, event.RuleID)
	if err != nil || len(rule.HealingPolicyIDs) == 0 {
		return err
	}
	existing, _ := s.repo.ListHealingRuns(ctx, domainalert.HealingRunFilter{EventID: event.ID, Limit: 20})
	for _, run := range existing {
		if run.PolicyID == rule.HealingPolicyIDs[0] && run.Status != "rejected" && run.Status != "failed" && run.Status != "completed" {
			return nil
		}
	}
	_, err = s.repo.CreateHealingRun(ctx, domainalert.HealingRunInput{
		PolicyID:       rule.HealingPolicyIDs[0],
		EventID:        event.ID,
		Status:         "pending_approval",
		ApprovalStatus: "pending",
		RequestedBy:    monitoringSystemPrincipal().UserID,
		Result: map[string]any{
			"trigger": "notification_policy",
			"ruleId":  rule.ID,
		},
	})
	return err
}

func matchesNotificationPolicy(policy domainalert.NotificationPolicy, event domainalert.AlertEvent) bool {
	if len(policy.Matchers) == 0 {
		return true
	}
	for key, rawValue := range policy.Matchers {
		values := matcherValues(rawValue)
		switch {
		case key == "severity":
			if !containsMatcher(values, event.Severity) {
				return false
			}
		case key == "status":
			if !containsMatcher(values, event.Status) {
				return false
			}
		case key == "clusterId":
			if !containsMatcher(values, event.ClusterID) {
				return false
			}
		case key == "namespace":
			if !containsMatcher(values, event.Namespace) {
				return false
			}
		case key == "ruleId":
			if !containsMatcher(values, event.RuleID) {
				return false
			}
		case key == "sourceType":
			if !containsMatcher(values, event.SourceType) {
				return false
			}
		case strings.HasPrefix(key, "label:"):
			labelKey := strings.TrimPrefix(key, "label:")
			if !containsMatcher(values, event.Labels[labelKey]) {
				return false
			}
		}
	}
	return true
}

func silenceMatches(matchers map[string]any, event domainalert.Instance) bool {
	if len(matchers) == 0 {
		return true
	}
	for key, rawValue := range matchers {
		values := matcherValues(rawValue)
		switch {
		case key == "severity":
			if !containsMatcher(values, event.Severity) {
				return false
			}
		case key == "status":
			if !containsMatcher(values, event.Status) {
				return false
			}
		case key == "clusterId":
			if !containsMatcher(values, event.ClusterID) {
				return false
			}
		case key == "namespace":
			if !containsMatcher(values, event.Namespace) {
				return false
			}
		case strings.HasPrefix(key, "label:"):
			labelKey := strings.TrimPrefix(key, "label:")
			if !containsMatcher(values, event.Labels[labelKey]) {
				return false
			}
		}
	}
	return true
}

func resolveNotificationTemplate(channel domainalert.NotificationChannel, templates []domainalert.NotificationTemplate) domainalert.NotificationTemplate {
	templateKey := firstNonEmpty(stringValue(channel.Config["templateKey"], ""), stringValue(channel.Config["templateId"], ""))
	templateType := stringValue(channel.Config["templateType"], "")
	for _, item := range templates {
		if !item.Enabled {
			continue
		}
		if templateKey != "" && (item.ID == templateKey || item.Name == templateKey) {
			return item
		}
		if templateKey == "" && templateType != "" && item.TemplateType == templateType {
			return item
		}
	}
	return domainalert.NotificationTemplate{}
}

func matcherValues(value any) []string {
	switch typed := value.(type) {
	case string:
		return []string{typed}
	case []string:
		return typed
	case []any:
		items := make([]string, 0, len(typed))
		for _, item := range typed {
			if text, ok := item.(string); ok {
				items = append(items, text)
			}
		}
		return items
	default:
		return []string{}
	}
}

func containsMatcher(values []string, actual string) bool {
	if len(values) == 0 {
		return true
	}
	for _, value := range values {
		if strings.EqualFold(strings.TrimSpace(value), strings.TrimSpace(actual)) {
			return true
		}
	}
	return false
}

func (s *Service) findNotificationPolicy(ctx context.Context, policyID string) (domainalert.NotificationPolicy, error) {
	policies, err := s.repo.ListNotificationPolicies(ctx)
	if err != nil {
		return domainalert.NotificationPolicy{}, err
	}
	for _, item := range policies {
		if item.ID == strings.TrimSpace(policyID) {
			return item, nil
		}
	}
	return domainalert.NotificationPolicy{}, fmt.Errorf("%w: notification policy not found", apperrors.ErrNotFound)
}

func renderTemplateString(source string, data map[string]any) (string, error) {
	if strings.TrimSpace(source) == "" {
		return "", nil
	}
	tpl, err := template.New("notification").Option("missingkey=zero").Parse(source)
	if err != nil {
		return "", err
	}
	var buffer bytes.Buffer
	if err := tpl.Execute(&buffer, data); err != nil {
		return "", err
	}
	return buffer.String(), nil
}

func renderStringMap(values map[string]any, data map[string]any) map[string]string {
	if len(values) == 0 {
		return map[string]string{}
	}
	rendered := make(map[string]string, len(values))
	for key, value := range values {
		text, err := renderTemplateString(fmt.Sprint(value), data)
		if err != nil {
			rendered[key] = fmt.Sprint(value)
			continue
		}
		rendered[key] = text
	}
	return rendered
}

func applyQueryParams(rawURL string, params map[string]string) string {
	if rawURL == "" || len(params) == 0 {
		return rawURL
	}
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return rawURL
	}
	query := parsed.Query()
	for key, value := range params {
		query.Set(key, value)
	}
	parsed.RawQuery = query.Encode()
	return parsed.String()
}

func findStatusSignal(signals []map[string]any) string {
	for _, signal := range signals {
		if trend := strings.TrimSpace(fmt.Sprint(signal["trend"])); trend != "" && trend != "stable" {
			return trend
		}
	}
	return ""
}

func normalizeRuleSeverity(rule domainalert.AlertRule, result domainalert.RuleTestResult) string {
	if value := strings.TrimSpace(rule.Labels["severity"]); value != "" {
		return strings.ToLower(value)
	}
	for _, sample := range result.Samples {
		if signals, ok := sample["signals"].([]map[string]any); ok {
			switch findStatusSignal(signals) {
			case "spike":
				return "critical"
			case "drop":
				return "warning"
			}
		}
	}
	return "warning"
}

func internalRuleEventID(rule domainalert.AlertRule, fingerprint string) string {
	return "rule-event:" + firstNonEmpty(strings.TrimSpace(rule.ID), fingerprint) + ":" + sanitizeIdentifier(fingerprint)
}

func internalRuleFingerprint(ruleID string, keyValues map[string]string) string {
	base := "internal-rule:" + strings.TrimSpace(ruleID)
	if len(keyValues) == 0 {
		return base
	}
	keys := make([]string, 0, len(keyValues))
	for key := range keyValues {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		value := keyValues[key]
		if strings.TrimSpace(value) == "" {
			continue
		}
		parts = append(parts, key+"="+value)
	}
	if len(parts) == 0 {
		return base
	}
	return base + ":" + sanitizeIdentifier(strings.Join(parts, ","))
}

func mergeLabelMaps(left, right map[string]string) map[string]string {
	items := map[string]string{}
	for key, value := range left {
		items[key] = value
	}
	for key, value := range right {
		items[key] = value
	}
	return items
}

func mergeAnyMaps(left, right map[string]any) map[string]any {
	items := map[string]any{}
	for key, value := range left {
		items[key] = value
	}
	for key, value := range right {
		items[key] = value
	}
	return items
}

func mapFromAny(value any) map[string]any {
	items, ok := value.(map[string]any)
	if !ok {
		return map[string]any{}
	}
	return items
}

func summarizeWorkflowRun(run domainworkflow.Run) string {
	if len(run.Steps) == 0 {
		return run.Status
	}
	last := run.Steps[len(run.Steps)-1]
	return firstNonEmpty(strings.TrimSpace(last.Summary), run.Status)
}

func monitoringSystemPrincipal() domainidentity.Principal {
	return domainidentity.Principal{
		UserID:   "system:monitoring",
		UserName: "monitoring-system",
		Roles:    []string{"admin"},
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func maxInt(left, right int) int {
	if left > right {
		return left
	}
	return right
}

func minInt(left, right int) int {
	if left < right {
		return left
	}
	return right
}

func sanitizeIdentifier(value string) string {
	replacer := strings.NewReplacer(" ", "_", "/", "_", ":", "_", ",", "_", "=", "_")
	return replacer.Replace(strings.TrimSpace(value))
}
