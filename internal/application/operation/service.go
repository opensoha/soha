package operation

import (
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	appaccess "github.com/opensoha/soha/internal/application/access"
	domaingovernance "github.com/opensoha/soha/internal/domain/governance"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainoperation "github.com/opensoha/soha/internal/domain/operation"
	"github.com/opensoha/soha/internal/platform/redaction"
)

const defaultRetentionDays = 90

type AlertSink interface {
	RecordGovernanceAlert(context.Context, domaingovernance.AlertInput) error
}

type Service struct {
	repo        domainoperation.Repository
	permissions *appaccess.PermissionResolver
	alerts      AlertSink
}

func New(repo domainoperation.Repository, permissions *appaccess.PermissionResolver) *Service {
	return &Service{repo: repo, permissions: permissions}
}

func (s *Service) SetAlertSink(alerts AlertSink) {
	s.alerts = alerts
}

func (s *Service) Record(ctx context.Context, entry domainoperation.Entry) error {
	if entry.ID == "" {
		entry.ID = uuid.NewString()
	}
	if entry.CreatedAt.IsZero() {
		entry.CreatedAt = time.Now().UTC()
	}
	if entry.TargetScope == nil {
		entry.TargetScope = map[string]any{}
	}
	if entry.Metadata == nil {
		entry.Metadata = map[string]any{}
	}
	entry.Summary = redaction.Text(entry.Summary)
	entry.TargetScope = redaction.Map(entry.TargetScope)
	entry.Metadata = redaction.Map(entry.Metadata)
	if err := s.repo.Create(ctx, entry); err != nil {
		return err
	}
	s.recordOperationAlert(ctx, entry)
	return nil
}

func (s *Service) List(ctx context.Context, filter domainoperation.Filter) ([]domainoperation.Entry, error) {
	items, err := s.repo.List(ctx, normalizeOperationFilter(filter))
	if err != nil {
		return nil, err
	}
	return sanitizeOperationEntries(items), nil
}

func (s *Service) ListAuthorized(ctx context.Context, principal domainidentity.Principal, filter domainoperation.Filter) ([]domainoperation.Entry, error) {
	if err := appaccess.AuthorizeRuntimePermission(ctx, s.permissions, principal, appaccess.PermSystemOperationsView); err != nil {
		return nil, err
	}
	return s.List(ctx, filter)
}

func (s *Service) Summary(ctx context.Context, filter domainoperation.Filter) (domainoperation.Summary, error) {
	filter = normalizeOperationFilter(filter)
	return s.repo.Summary(ctx, filter, defaultRetentionDays)
}

func (s *Service) SummaryAuthorized(ctx context.Context, principal domainidentity.Principal, filter domainoperation.Filter) (domainoperation.Summary, error) {
	if err := appaccess.AuthorizeRuntimePermission(ctx, s.permissions, principal, appaccess.PermSystemOperationsView); err != nil {
		return domainoperation.Summary{}, err
	}
	return s.Summary(ctx, filter)
}

func (s *Service) ExportCSV(ctx context.Context, filter domainoperation.Filter) (domainoperation.Export, error) {
	filter = normalizeOperationFilterWithLimit(filter, 5000)
	if filter.Limit <= 0 {
		filter.Limit = 500
	}
	if filter.Limit > 5000 {
		filter.Limit = 5000
	}
	items, err := s.repo.List(ctx, filter)
	if err != nil {
		return domainoperation.Export{}, err
	}
	generatedAt := time.Now().UTC()
	var buf bytes.Buffer
	writer := csv.NewWriter(&buf)
	if err := writer.Write([]string{"id", "actorId", "actorName", "operationType", "targetScope", "result", "summary", "requestPath", "requestMethod", "requestId", "sourceIp", "metadata", "createdAt"}); err != nil {
		return domainoperation.Export{}, err
	}
	for _, item := range items {
		item = sanitizeOperationEntry(item)
		targetScope, err := json.Marshal(item.TargetScope)
		if err != nil {
			return domainoperation.Export{}, fmt.Errorf("marshal operation export target scope: %w", err)
		}
		metadata, err := json.Marshal(item.Metadata)
		if err != nil {
			return domainoperation.Export{}, fmt.Errorf("marshal operation export metadata: %w", err)
		}
		if err := writer.Write([]string{
			item.ID,
			item.ActorID,
			item.ActorName,
			item.OperationType,
			string(targetScope),
			item.Result,
			item.Summary,
			item.RequestPath,
			item.RequestMethod,
			item.RequestID,
			item.SourceIP,
			string(metadata),
			item.CreatedAt.UTC().Format(time.RFC3339),
		}); err != nil {
			return domainoperation.Export{}, err
		}
	}
	writer.Flush()
	if err := writer.Error(); err != nil {
		return domainoperation.Export{}, err
	}
	return domainoperation.Export{
		Filename:    fmt.Sprintf("operation-logs-%s.csv", generatedAt.Format("20060102T150405Z")),
		Content:     buf.Bytes(),
		ContentType: "text/csv; charset=utf-8",
		Count:       len(items),
		GeneratedAt: generatedAt,
	}, nil
}

func (s *Service) ExportCSVAuthorized(ctx context.Context, principal domainidentity.Principal, filter domainoperation.Filter) (domainoperation.Export, error) {
	if err := appaccess.AuthorizeRuntimePermission(ctx, s.permissions, principal, appaccess.PermSystemOperationsView); err != nil {
		return domainoperation.Export{}, err
	}
	return s.ExportCSV(ctx, filter)
}

func sanitizeOperationEntries(items []domainoperation.Entry) []domainoperation.Entry {
	out := make([]domainoperation.Entry, len(items))
	for index, item := range items {
		out[index] = sanitizeOperationEntry(item)
	}
	return out
}

func sanitizeOperationEntry(item domainoperation.Entry) domainoperation.Entry {
	item.Summary = redaction.Text(item.Summary)
	item.TargetScope = redaction.Map(item.TargetScope)
	if item.TargetScope == nil {
		item.TargetScope = map[string]any{}
	}
	item.Metadata = redaction.Map(item.Metadata)
	if item.Metadata == nil {
		item.Metadata = map[string]any{}
	}
	return item
}

func (s *Service) recordOperationAlert(ctx context.Context, entry domainoperation.Entry) {
	if s.alerts == nil || !shouldAlertOperation(entry) {
		return
	}
	_ = s.alerts.RecordGovernanceAlert(ctx, domaingovernance.AlertInput{
		Source:        "operation",
		EventID:       entry.ID,
		ActorID:       entry.ActorID,
		ActorName:     entry.ActorName,
		OperationType: entry.OperationType,
		Result:        entry.Result,
		Summary:       entry.Summary,
		ClusterID:     stringFromMap(entry.TargetScope, "clusterId", "clusterID", "cluster"),
		Namespace:     stringFromMap(entry.TargetScope, "namespace"),
		ResourceKind:  stringFromMap(entry.TargetScope, "resourceKind", "kind"),
		ResourceName:  stringFromMap(entry.TargetScope, "resourceName", "name"),
		RequestPath:   entry.RequestPath,
		RequestMethod: entry.RequestMethod,
		RequestID:     entry.RequestID,
		SourceIP:      entry.SourceIP,
		Severity:      operationAlertSeverity(entry),
		Labels: map[string]string{
			"governanceSource": "operation",
			"result":           normalizedResult(entry.Result),
		},
		Annotations: operationAlertAnnotations(entry),
		CreatedAt:   entry.CreatedAt,
	})
}

func shouldAlertOperation(entry domainoperation.Entry) bool {
	result := normalizedResult(entry.Result)
	if result == "failure" || result == "denied" || result == "error" {
		return true
	}
	operationType := strings.ToLower(strings.TrimSpace(entry.OperationType))
	if strings.Contains(operationType, "delete") || strings.Contains(operationType, "exec") || strings.Contains(operationType, "credential") || strings.Contains(operationType, "token") {
		return true
	}
	return false
}

func operationAlertSeverity(entry domainoperation.Entry) string {
	result := normalizedResult(entry.Result)
	switch result {
	case "failure", "denied", "error":
		return "warning"
	}
	operationType := strings.ToLower(strings.TrimSpace(entry.OperationType))
	if strings.Contains(operationType, "delete") || strings.Contains(operationType, "exec") {
		return "critical"
	}
	return "warning"
}

func operationAlertAnnotations(entry domainoperation.Entry) map[string]string {
	annotations := map[string]string{
		"operationId":   entry.ID,
		"operationType": entry.OperationType,
		"actorId":       entry.ActorID,
		"actorName":     entry.ActorName,
		"requestId":     entry.RequestID,
		"requestPath":   entry.RequestPath,
		"requestMethod": entry.RequestMethod,
		"sourceIp":      entry.SourceIP,
	}
	if approvalRequestID := stringFromMap(entry.Metadata, "approvalRequestId", "approvalID", "approvalId"); approvalRequestID != "" {
		annotations["approvalRequestId"] = approvalRequestID
	}
	return compactStringMap(annotations)
}

func stringFromMap(values map[string]any, keys ...string) string {
	for _, key := range keys {
		if value, ok := values[key]; ok {
			if text := strings.TrimSpace(fmt.Sprint(value)); text != "" && text != "<nil>" {
				return text
			}
		}
	}
	return ""
}

func normalizedResult(result string) string {
	return strings.ToLower(strings.TrimSpace(result))
}

func compactStringMap(values map[string]string) map[string]string {
	out := make(map[string]string, len(values))
	for key, value := range values {
		if strings.TrimSpace(value) != "" {
			out[key] = value
		}
	}
	return out
}

func normalizeOperationFilter(filter domainoperation.Filter) domainoperation.Filter {
	return normalizeOperationFilterWithLimit(filter, 500)
}

func normalizeOperationFilterWithLimit(filter domainoperation.Filter, maxLimit int) domainoperation.Filter {
	filter.OperationType = strings.TrimSpace(filter.OperationType)
	filter.ActorID = strings.TrimSpace(filter.ActorID)
	filter.ClusterID = strings.TrimSpace(filter.ClusterID)
	filter.Namespace = strings.TrimSpace(filter.Namespace)
	filter.ResourceKind = strings.TrimSpace(filter.ResourceKind)
	filter.ResourceName = strings.TrimSpace(filter.ResourceName)
	filter.Result = strings.ToLower(strings.TrimSpace(filter.Result))
	filter.RequestID = strings.TrimSpace(filter.RequestID)
	filter.RequestPath = strings.TrimSpace(filter.RequestPath)
	filter.RequestMethod = strings.ToUpper(strings.TrimSpace(filter.RequestMethod))
	filter.SourceIP = strings.TrimSpace(filter.SourceIP)
	filter.ApprovalRequestID = strings.TrimSpace(filter.ApprovalRequestID)
	filter.AgentRunID = strings.TrimSpace(filter.AgentRunID)
	filter.RootCauseRunID = strings.TrimSpace(filter.RootCauseRunID)
	filter.MetadataKey = strings.TrimSpace(filter.MetadataKey)
	filter.MetadataValue = strings.TrimSpace(filter.MetadataValue)
	if filter.MetadataValue == "" {
		filter.MetadataKey = ""
	}
	if filter.Limit <= 0 {
		filter.Limit = 50
	}
	if maxLimit > 0 && filter.Limit > maxLimit {
		filter.Limit = maxLimit
	}
	return filter
}
