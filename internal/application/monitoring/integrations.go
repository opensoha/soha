package monitoring

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	appaccess "github.com/soha/soha/internal/application/access"
	domainalert "github.com/soha/soha/internal/domain/alert"
	domainidentity "github.com/soha/soha/internal/domain/identity"
	"github.com/soha/soha/internal/platform/apperrors"
)

func (s *Service) ListAlertIntegrations(ctx context.Context, principal domainidentity.Principal) ([]domainalert.AlertIntegration, error) {
	if err := s.authorize(ctx, principal, appaccess.PermObserveAlertIntegrationsView); err != nil {
		return nil, err
	}
	if s.repo == nil {
		return []domainalert.AlertIntegration{}, nil
	}
	items, err := s.repo.ListAlertIntegrations(ctx)
	if err != nil {
		return nil, err
	}
	for index := range items {
		items[index] = redactAlertIntegrationToken(items[index])
	}
	return items, nil
}

func (s *Service) GetAlertIntegration(ctx context.Context, principal domainidentity.Principal, integrationID string) (domainalert.AlertIntegration, error) {
	if err := s.authorize(ctx, principal, appaccess.PermObserveAlertIntegrationsView); err != nil {
		return domainalert.AlertIntegration{}, err
	}
	if s.repo == nil {
		return domainalert.AlertIntegration{}, fmt.Errorf("%w: alert repository is not configured", apperrors.ErrInvalidArgument)
	}
	item, err := s.repo.GetAlertIntegration(ctx, strings.TrimSpace(integrationID))
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			return domainalert.AlertIntegration{}, fmt.Errorf("%w: %s", apperrors.ErrNotFound, strings.TrimSpace(integrationID))
		}
		return domainalert.AlertIntegration{}, err
	}
	return redactAlertIntegrationToken(item), nil
}

func (s *Service) CreateAlertIntegration(ctx context.Context, principal domainidentity.Principal, input domainalert.AlertIntegrationInput) (domainalert.AlertIntegration, error) {
	if err := s.authorize(ctx, principal, appaccess.PermObserveAlertIntegrationsManage); err != nil {
		return domainalert.AlertIntegration{}, err
	}
	if s.repo == nil {
		return domainalert.AlertIntegration{}, fmt.Errorf("%w: alert repository is not configured", apperrors.ErrInvalidArgument)
	}
	if err := validateAlertIntegrationInput(input, true); err != nil {
		return domainalert.AlertIntegration{}, err
	}
	item, err := s.repo.CreateAlertIntegration(ctx, input)
	if err != nil {
		return domainalert.AlertIntegration{}, err
	}
	return item, nil
}

func (s *Service) UpdateAlertIntegration(ctx context.Context, principal domainidentity.Principal, integrationID string, input domainalert.AlertIntegrationInput) (domainalert.AlertIntegration, error) {
	if err := s.authorize(ctx, principal, appaccess.PermObserveAlertIntegrationsManage); err != nil {
		return domainalert.AlertIntegration{}, err
	}
	if s.repo == nil {
		return domainalert.AlertIntegration{}, fmt.Errorf("%w: alert repository is not configured", apperrors.ErrInvalidArgument)
	}
	if err := validateAlertIntegrationInput(input, false); err != nil {
		return domainalert.AlertIntegration{}, err
	}
	item, err := s.repo.UpdateAlertIntegration(ctx, strings.TrimSpace(integrationID), input)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			return domainalert.AlertIntegration{}, fmt.Errorf("%w: %s", apperrors.ErrNotFound, strings.TrimSpace(integrationID))
		}
		return domainalert.AlertIntegration{}, err
	}
	if strings.TrimSpace(input.Token) == "" {
		return redactAlertIntegrationToken(item), nil
	}
	return item, nil
}

func (s *Service) TestAlertIntegration(ctx context.Context, principal domainidentity.Principal, input domainalert.AlertIntegrationTestInput) (domainalert.AlertIntegrationTestResult, error) {
	if err := s.authorize(ctx, principal, appaccess.PermObserveAlertIntegrationsManage); err != nil {
		return domainalert.AlertIntegrationTestResult{}, err
	}
	integration := domainalert.AlertIntegration{
		ID:              "preview",
		Name:            "preview",
		IntegrationType: strings.ToLower(strings.TrimSpace(input.IntegrationType)),
		LabelMapping:    input.LabelMapping,
		DedupeConfig:    input.DedupeConfig,
		Enabled:         true,
	}
	if integration.IntegrationType == "" {
		integration.IntegrationType = "generic_json"
	}
	if !isSupportedAlertIntegrationType(integration.IntegrationType) {
		return domainalert.AlertIntegrationTestResult{}, fmt.Errorf("%w: unsupported alert integration type %q", apperrors.ErrInvalidArgument, input.IntegrationType)
	}
	source, alerts, summary, err := normalizeAlertIntegrationPayload(integration, input.Payload)
	if err != nil {
		return domainalert.AlertIntegrationTestResult{}, err
	}
	return domainalert.AlertIntegrationTestResult{
		IntegrationType: integration.IntegrationType,
		Source:          source,
		AcceptedCount:   len(alerts),
		Alerts:          alerts,
		Summary:         summary,
	}, nil
}

func (s *Service) IngestAlertIntegration(ctx context.Context, integrationID string, token string, payload map[string]any) (int, error) {
	if s.repo == nil {
		return 0, fmt.Errorf("%w: alert repository is not configured", apperrors.ErrInvalidArgument)
	}
	if !s.enabled {
		return 0, fmt.Errorf("%w: monitoring integrations are disabled", apperrors.ErrAccessDenied)
	}
	integration, err := s.repo.GetAlertIntegration(ctx, strings.TrimSpace(integrationID))
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			return 0, fmt.Errorf("%w: %s", apperrors.ErrNotFound, strings.TrimSpace(integrationID))
		}
		return 0, err
	}
	if !integration.Enabled {
		return 0, fmt.Errorf("%w: alert integration is disabled", apperrors.ErrAccessDenied)
	}
	if !alertIntegrationTokenMatches(integration.Token, token) {
		return 0, fmt.Errorf("%w: invalid alert integration token", apperrors.ErrUnauthorized)
	}
	source, alerts, _, err := normalizeAlertIntegrationPayload(integration, payload)
	if err != nil {
		_, _ = s.repo.UpdateAlertIntegrationStatus(ctx, integration.ID, domainalert.AlertIntegrationStatusInput{Status: "error", LastError: err.Error()})
		return 0, err
	}
	count, err := s.Ingest(ctx, domainalert.IngestRequest{Source: source, Alerts: alerts})
	if err != nil {
		_, _ = s.repo.UpdateAlertIntegrationStatus(ctx, integration.ID, domainalert.AlertIntegrationStatusInput{Status: "error", LastError: err.Error()})
		return 0, err
	}
	_, _ = s.repo.UpdateAlertIntegrationStatus(ctx, integration.ID, domainalert.AlertIntegrationStatusInput{Status: "active", LastReceivedAt: time.Now().UTC()})
	return count, nil
}

func validateAlertIntegrationInput(input domainalert.AlertIntegrationInput, create bool) error {
	if strings.TrimSpace(input.Name) == "" {
		return fmt.Errorf("%w: alert integration name is required", apperrors.ErrInvalidArgument)
	}
	if !isSupportedAlertIntegrationType(strings.ToLower(strings.TrimSpace(input.IntegrationType))) {
		return fmt.Errorf("%w: unsupported alert integration type %q", apperrors.ErrInvalidArgument, input.IntegrationType)
	}
	if create && strings.TrimSpace(input.ID) != "" && strings.ContainsAny(strings.TrimSpace(input.ID), `/\?#`) {
		return fmt.Errorf("%w: alert integration id cannot contain URL path separators", apperrors.ErrInvalidArgument)
	}
	return nil
}

func isSupportedAlertIntegrationType(integrationType string) bool {
	switch strings.ToLower(strings.TrimSpace(integrationType)) {
	case "alertmanager_v1", "grafana_alerting_v1", "generic_json":
		return true
	default:
		return false
	}
}

func redactAlertIntegrationToken(item domainalert.AlertIntegration) domainalert.AlertIntegration {
	item.Token = ""
	return item
}

func alertIntegrationTokenMatches(expected string, actual string) bool {
	expected = strings.TrimSpace(expected)
	actual = strings.TrimSpace(actual)
	if expected == "" || actual == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(expected), []byte(actual)) == 1
}

func normalizeAlertIntegrationPayload(integration domainalert.AlertIntegration, payload map[string]any) (string, []domainalert.IngestAlert, string, error) {
	if len(payload) == 0 {
		return "", nil, "", fmt.Errorf("%w: alert integration payload cannot be empty", apperrors.ErrInvalidArgument)
	}
	var alerts []domainalert.IngestAlert
	var summary string
	switch integration.IntegrationType {
	case "alertmanager_v1":
		alerts = normalizeAlertmanagerPayload(integration, payload)
		summary = "Alertmanager payload normalized"
	case "grafana_alerting_v1":
		alerts = normalizeGrafanaAlertingPayload(integration, payload)
		summary = "Grafana Alerting payload normalized"
	case "generic_json":
		alerts = normalizeGenericWebhookPayload(integration, payload)
		summary = "Generic webhook payload normalized"
	default:
		return "", nil, "", fmt.Errorf("%w: unsupported alert integration type %q", apperrors.ErrInvalidArgument, integration.IntegrationType)
	}
	if len(alerts) == 0 {
		return "", nil, "", fmt.Errorf("%w: payload did not contain any alerts", apperrors.ErrInvalidArgument)
	}
	for index := range alerts {
		alerts[index] = applyAlertIntegrationMapping(integration, alerts[index])
	}
	return integration.ID, alerts, summary, nil
}

func normalizeAlertmanagerPayload(integration domainalert.AlertIntegration, payload map[string]any) []domainalert.IngestAlert {
	receiver := stringFromAny(payload["receiver"])
	status := normalizeAlertStatus(firstNonEmpty(stringFromAny(payload["status"]), "firing"))
	commonLabels := mapStringFromAny(payload["commonLabels"])
	commonAnnotations := mapStringFromAny(payload["commonAnnotations"])
	externalURL := stringFromAny(payload["externalURL"])
	groupKey := stringFromAny(payload["groupKey"])
	items := arrayFromAny(payload["alerts"])
	alerts := make([]domainalert.IngestAlert, 0, len(items))
	for _, raw := range items {
		item := mapAnyFromAny(raw)
		labels := mergeLabelMaps(commonLabels, mapStringFromAny(item["labels"]))
		annotations := mergeLabelMaps(commonAnnotations, mapStringFromAny(item["annotations"]))
		alertStatus := normalizeAlertStatus(firstNonEmpty(stringFromAny(item["status"]), status))
		title := firstNonEmpty(labels["alertname"], labels["alert"], annotations["title"], "Alertmanager alert")
		summary := firstNonEmpty(annotations["summary"], annotations["description"], title)
		fingerprint := firstNonEmpty(stringFromAny(item["fingerprint"]), fingerprintFromParts(integration.ID, title, labels, stringFromAny(item["startsAt"])))
		alerts = append(alerts, domainalert.IngestAlert{
			Fingerprint:  fingerprint,
			Title:        title,
			Summary:      summary,
			Severity:     firstNonEmpty(strings.ToLower(labels["severity"]), "warning"),
			Status:       alertStatus,
			ClusterID:    firstNonEmpty(labels["clusterId"], labels["cluster_id"], labels["cluster"]),
			Namespace:    labels["namespace"],
			Labels:       enrichIntegrationLabels(integration, labels, "alertmanager", groupKey),
			Annotations:  enrichIntegrationAnnotations(annotations, externalURL),
			Receiver:     receiver,
			GeneratorURL: firstNonEmpty(stringFromAny(item["generatorURL"]), externalURL),
			StartsAt:     timeFromAny(item["startsAt"]),
			EndsAt:       timeFromAny(item["endsAt"]),
		})
	}
	return alerts
}

func normalizeGrafanaAlertingPayload(integration domainalert.AlertIntegration, payload map[string]any) []domainalert.IngestAlert {
	receiver := stringFromAny(payload["receiver"])
	status := normalizeAlertStatus(firstNonEmpty(stringFromAny(payload["status"]), stringFromAny(payload["state"]), "firing"))
	commonLabels := mapStringFromAny(payload["commonLabels"])
	commonAnnotations := mapStringFromAny(payload["commonAnnotations"])
	externalURL := stringFromAny(payload["externalURL"])
	groupKey := stringFromAny(payload["groupKey"])
	defaultTitle := firstNonEmpty(stringFromAny(payload["title"]), "Grafana alert")
	defaultSummary := firstNonEmpty(stringFromAny(payload["message"]), defaultTitle)
	items := arrayFromAny(payload["alerts"])
	if len(items) == 0 {
		items = []any{payload}
	}
	alerts := make([]domainalert.IngestAlert, 0, len(items))
	for _, raw := range items {
		item := mapAnyFromAny(raw)
		labels := mergeLabelMaps(commonLabels, mapStringFromAny(item["labels"]))
		annotations := mergeLabelMaps(commonAnnotations, mapStringFromAny(item["annotations"]))
		alertStatus := normalizeAlertStatus(firstNonEmpty(stringFromAny(item["status"]), stringFromAny(item["state"]), status))
		title := firstNonEmpty(labels["alertname"], labels["grafana_rule_name"], annotations["title"], defaultTitle)
		summary := firstNonEmpty(annotations["summary"], annotations["description"], annotations["message"], defaultSummary, title)
		generatorURL := firstNonEmpty(stringFromAny(item["generatorURL"]), stringFromAny(item["dashboardURL"]), stringFromAny(item["panelURL"]), externalURL)
		fingerprint := firstNonEmpty(stringFromAny(item["fingerprint"]), labels["rule_uid"], labels["alertname"], fingerprintFromParts(integration.ID, title, labels, stringFromAny(item["startsAt"])))
		alerts = append(alerts, domainalert.IngestAlert{
			Fingerprint:  fingerprint,
			Title:        title,
			Summary:      summary,
			Severity:     firstNonEmpty(strings.ToLower(labels["severity"]), "warning"),
			Status:       alertStatus,
			ClusterID:    firstNonEmpty(labels["clusterId"], labels["cluster_id"], labels["cluster"]),
			Namespace:    labels["namespace"],
			Labels:       enrichIntegrationLabels(integration, labels, "grafana_alerting", groupKey),
			Annotations:  enrichGrafanaAnnotations(enrichIntegrationAnnotations(annotations, externalURL), item),
			Receiver:     receiver,
			GeneratorURL: generatorURL,
			StartsAt:     timeFromAny(item["startsAt"]),
			EndsAt:       timeFromAny(item["endsAt"]),
		})
	}
	return alerts
}

func normalizeGenericWebhookPayload(integration domainalert.AlertIntegration, payload map[string]any) []domainalert.IngestAlert {
	items := arrayFromAny(payload["alerts"])
	if len(items) == 0 {
		items = []any{payload}
	}
	alerts := make([]domainalert.IngestAlert, 0, len(items))
	for _, raw := range items {
		item := mapAnyFromAny(raw)
		labels := mapStringFromAny(item["labels"])
		annotations := mapStringFromAny(item["annotations"])
		title := firstNonEmpty(stringFromAny(item["title"]), labels["alertname"], labels["alert"], "Generic alert")
		summary := firstNonEmpty(stringFromAny(item["summary"]), annotations["summary"], annotations["description"], title)
		fingerprint := firstNonEmpty(stringFromAny(item["fingerprint"]), fingerprintFromParts(integration.ID, title, labels, stringFromAny(item["startsAt"])))
		alerts = append(alerts, domainalert.IngestAlert{
			Fingerprint:  fingerprint,
			Title:        title,
			Summary:      summary,
			Severity:     firstNonEmpty(strings.ToLower(stringFromAny(item["severity"])), strings.ToLower(labels["severity"]), "warning"),
			Status:       normalizeAlertStatus(firstNonEmpty(stringFromAny(item["status"]), "firing")),
			ClusterID:    firstNonEmpty(stringFromAny(item["clusterId"]), stringFromAny(item["clusterID"]), labels["clusterId"], labels["cluster"]),
			Namespace:    firstNonEmpty(stringFromAny(item["namespace"]), labels["namespace"]),
			Labels:       enrichIntegrationLabels(integration, labels, "generic_json", ""),
			Annotations:  annotations,
			Receiver:     stringFromAny(item["receiver"]),
			GeneratorURL: firstNonEmpty(stringFromAny(item["generatorUrl"]), stringFromAny(item["generatorURL"])),
			StartsAt:     timeFromAny(item["startsAt"]),
			EndsAt:       timeFromAny(item["endsAt"]),
		})
	}
	return alerts
}

func applyAlertIntegrationMapping(integration domainalert.AlertIntegration, alert domainalert.IngestAlert) domainalert.IngestAlert {
	for target, rawLabelKey := range integration.LabelMapping {
		labelKey := strings.TrimSpace(fmt.Sprint(rawLabelKey))
		if labelKey == "" {
			continue
		}
		value := firstNonEmpty(alert.Labels[labelKey], alert.Annotations[labelKey])
		if value == "" {
			continue
		}
		switch strings.TrimSpace(target) {
		case "clusterId", "clusterID":
			alert.ClusterID = value
		case "namespace":
			alert.Namespace = value
		case "severity":
			alert.Severity = strings.ToLower(value)
		case "title":
			alert.Title = value
		case "summary":
			alert.Summary = value
		case "service", "businessLineId", "business_line_id", "role", "alertCategory":
			if alert.Labels == nil {
				alert.Labels = map[string]string{}
			}
			alert.Labels[target] = value
		}
	}
	if labels := stringSliceValue(integration.DedupeConfig["fingerprintLabels"]); len(labels) > 0 {
		values := make(map[string]string, len(labels))
		for _, label := range labels {
			values[label] = firstNonEmpty(alert.Labels[label], alert.Annotations[label])
		}
		alert.Fingerprint = fingerprintFromParts(integration.ID, alert.Title, values, alert.Status)
	}
	return alert
}

func enrichIntegrationLabels(integration domainalert.AlertIntegration, labels map[string]string, sourceType string, groupKey string) map[string]string {
	out := mergeLabelMaps(labels, map[string]string{
		"integrationId":   integration.ID,
		"integrationName": integration.Name,
		"integrationType": integration.IntegrationType,
		"sourceType":      sourceType,
	})
	if strings.TrimSpace(groupKey) != "" {
		out["groupKey"] = strings.TrimSpace(groupKey)
	}
	return out
}

func enrichIntegrationAnnotations(annotations map[string]string, externalURL string) map[string]string {
	if strings.TrimSpace(externalURL) == "" {
		return annotations
	}
	return mergeLabelMaps(annotations, map[string]string{"externalURL": strings.TrimSpace(externalURL)})
}

func enrichGrafanaAnnotations(annotations map[string]string, item map[string]any) map[string]string {
	additions := map[string]string{}
	for _, key := range []string{"dashboardURL", "panelURL", "silenceURL", "imageURL"} {
		if value := stringFromAny(item[key]); value != "" {
			additions[key] = value
		}
	}
	return mergeLabelMaps(annotations, additions)
}

func normalizeAlertStatus(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "resolved", "ok", "normal":
		return "resolved"
	case "acknowledged":
		return "acknowledged"
	default:
		return "firing"
	}
}

func mapAnyFromAny(value any) map[string]any {
	if item, ok := value.(map[string]any); ok {
		return item
	}
	return map[string]any{}
}

func mapStringFromAny(value any) map[string]string {
	switch typed := value.(type) {
	case map[string]string:
		out := make(map[string]string, len(typed))
		for key, item := range typed {
			out[key] = strings.TrimSpace(item)
		}
		return out
	case map[string]any:
		out := make(map[string]string, len(typed))
		for key, item := range typed {
			if text := strings.TrimSpace(fmt.Sprint(item)); text != "" && text != "<nil>" {
				out[key] = text
			}
		}
		return out
	default:
		return map[string]string{}
	}
}

func arrayFromAny(value any) []any {
	switch typed := value.(type) {
	case []any:
		return typed
	case []map[string]any:
		items := make([]any, 0, len(typed))
		for _, item := range typed {
			items = append(items, item)
		}
		return items
	default:
		return nil
	}
}

func stringFromAny(value any) string {
	if value == nil {
		return ""
	}
	if text, ok := value.(string); ok {
		return strings.TrimSpace(text)
	}
	return strings.TrimSpace(fmt.Sprint(value))
}

func timeFromAny(value any) time.Time {
	switch typed := value.(type) {
	case time.Time:
		return typed
	case string:
		text := strings.TrimSpace(typed)
		if text == "" {
			return time.Time{}
		}
		if parsed, err := time.Parse(time.RFC3339Nano, text); err == nil {
			return parsed
		}
		if parsed, err := time.Parse(time.RFC3339, text); err == nil {
			return parsed
		}
		return time.Time{}
	default:
		return time.Time{}
	}
}

func fingerprintFromParts(parts ...any) string {
	payload, _ := json.Marshal(parts)
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:])[:24]
}
