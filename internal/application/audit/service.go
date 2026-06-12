package audit

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
	domainaudit "github.com/opensoha/soha/internal/domain/audit"
	domaingovernance "github.com/opensoha/soha/internal/domain/governance"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/redaction"
)

const defaultRetentionDays = 90

type AlertSink interface {
	RecordGovernanceAlert(context.Context, domaingovernance.AlertInput) error
}

type Service struct {
	repo        domainaudit.Repository
	permissions *appaccess.PermissionResolver
	alerts      AlertSink
}

func New(repo domainaudit.Repository, permissions *appaccess.PermissionResolver) *Service {
	return &Service{repo: repo, permissions: permissions}
}

func (s *Service) SetAlertSink(alerts AlertSink) {
	s.alerts = alerts
}

func (s *Service) Record(ctx context.Context, entry domainaudit.Entry) error {
	if entry.ID == "" {
		entry.ID = uuid.NewString()
	}
	if entry.CreatedAt.IsZero() {
		entry.CreatedAt = time.Now().UTC()
	}
	if entry.Roles == nil {
		entry.Roles = []string{}
	}
	if entry.Teams == nil {
		entry.Teams = []string{}
	}
	if entry.Metadata == nil {
		entry.Metadata = map[string]any{}
	}
	entry.Summary = redaction.Text(entry.Summary)
	entry.Metadata = redaction.Map(entry.Metadata)
	if err := s.repo.Create(ctx, entry); err != nil {
		return err
	}
	s.recordAuditAlert(ctx, entry)
	return nil
}

func (s *Service) List(ctx context.Context, filter domainaudit.Filter) ([]domainaudit.Entry, error) {
	items, err := s.repo.List(ctx, normalizeAuditFilter(filter))
	if err != nil {
		return nil, err
	}
	return sanitizeAuditEntries(items), nil
}

func (s *Service) ListAuthorized(ctx context.Context, principal domainidentity.Principal, filter domainaudit.Filter) ([]domainaudit.Entry, error) {
	if err := appaccess.AuthorizeRuntimePermission(ctx, s.permissions, principal, appaccess.PermSystemAuditView); err != nil {
		return nil, err
	}
	return s.List(ctx, filter)
}

func (s *Service) Summary(ctx context.Context, filter domainaudit.Filter) (domainaudit.Summary, error) {
	filter = normalizeAuditFilter(filter)
	return s.repo.Summary(ctx, filter, defaultRetentionDays)
}

func (s *Service) SummaryAuthorized(ctx context.Context, principal domainidentity.Principal, filter domainaudit.Filter) (domainaudit.Summary, error) {
	if err := appaccess.AuthorizeRuntimePermission(ctx, s.permissions, principal, appaccess.PermSystemAuditView); err != nil {
		return domainaudit.Summary{}, err
	}
	return s.Summary(ctx, filter)
}

func (s *Service) ExportCSV(ctx context.Context, filter domainaudit.Filter) (domainaudit.Export, error) {
	filter = normalizeAuditFilterWithLimit(filter, 5000)
	if filter.Limit <= 0 {
		filter.Limit = 500
	}
	if filter.Limit > 5000 {
		filter.Limit = 5000
	}
	items, err := s.repo.List(ctx, filter)
	if err != nil {
		return domainaudit.Export{}, err
	}
	generatedAt := time.Now().UTC()
	var buf bytes.Buffer
	writer := csv.NewWriter(&buf)
	if err := writer.Write([]string{"id", "actorId", "actorName", "roles", "teams", "clusterId", "namespace", "resourceKind", "resourceName", "action", "result", "summary", "requestPath", "requestMethod", "requestId", "sourceIp", "metadata", "createdAt"}); err != nil {
		return domainaudit.Export{}, err
	}
	for _, item := range items {
		item = sanitizeAuditEntry(item)
		metadata, err := json.Marshal(item.Metadata)
		if err != nil {
			return domainaudit.Export{}, fmt.Errorf("marshal audit export metadata: %w", err)
		}
		if err := writer.Write([]string{
			item.ID,
			item.ActorID,
			item.ActorName,
			strings.Join(item.Roles, "|"),
			strings.Join(item.Teams, "|"),
			item.ClusterID,
			item.Namespace,
			item.ResourceKind,
			item.ResourceName,
			item.Action,
			item.Result,
			item.Summary,
			item.RequestPath,
			item.RequestMethod,
			item.RequestID,
			item.SourceIP,
			string(metadata),
			item.CreatedAt.UTC().Format(time.RFC3339),
		}); err != nil {
			return domainaudit.Export{}, err
		}
	}
	writer.Flush()
	if err := writer.Error(); err != nil {
		return domainaudit.Export{}, err
	}
	return domainaudit.Export{
		Filename:    fmt.Sprintf("audit-logs-%s.csv", generatedAt.Format("20060102T150405Z")),
		Content:     buf.Bytes(),
		ContentType: "text/csv; charset=utf-8",
		Count:       len(items),
		GeneratedAt: generatedAt,
	}, nil
}

func sanitizeAuditEntries(items []domainaudit.Entry) []domainaudit.Entry {
	out := make([]domainaudit.Entry, len(items))
	for index, item := range items {
		out[index] = sanitizeAuditEntry(item)
	}
	return out
}

func sanitizeAuditEntry(item domainaudit.Entry) domainaudit.Entry {
	item.Summary = redaction.Text(item.Summary)
	item.Metadata = redaction.Map(item.Metadata)
	if item.Metadata == nil {
		item.Metadata = map[string]any{}
	}
	return item
}

func (s *Service) recordAuditAlert(ctx context.Context, entry domainaudit.Entry) {
	if s.alerts == nil || !shouldAlertAudit(entry) {
		return
	}
	_ = s.alerts.RecordGovernanceAlert(ctx, domaingovernance.AlertInput{
		Source:        "audit",
		EventID:       entry.ID,
		ActorID:       entry.ActorID,
		ActorName:     entry.ActorName,
		Action:        entry.Action,
		Result:        entry.Result,
		Summary:       entry.Summary,
		ClusterID:     entry.ClusterID,
		Namespace:     entry.Namespace,
		ResourceKind:  entry.ResourceKind,
		ResourceName:  entry.ResourceName,
		RequestPath:   entry.RequestPath,
		RequestMethod: entry.RequestMethod,
		RequestID:     entry.RequestID,
		SourceIP:      entry.SourceIP,
		Severity:      auditAlertSeverity(entry),
		Labels: map[string]string{
			"governanceSource": "audit",
			"result":           normalizedResult(entry.Result),
		},
		Annotations: auditAlertAnnotations(entry),
		CreatedAt:   entry.CreatedAt,
	})
}

func shouldAlertAudit(entry domainaudit.Entry) bool {
	result := normalizedResult(entry.Result)
	if result == "failure" || result == "denied" || result == "error" {
		return true
	}
	action := strings.ToLower(strings.TrimSpace(entry.Action))
	if strings.Contains(action, "delete") || strings.Contains(action, "exec") || strings.Contains(action, "credential") || strings.Contains(action, "token") {
		return true
	}
	return false
}

func auditAlertSeverity(entry domainaudit.Entry) string {
	result := normalizedResult(entry.Result)
	switch result {
	case "failure", "denied", "error":
		return "warning"
	}
	action := strings.ToLower(strings.TrimSpace(entry.Action))
	if strings.Contains(action, "delete") || strings.Contains(action, "exec") {
		return "critical"
	}
	return "warning"
}

func auditAlertAnnotations(entry domainaudit.Entry) map[string]string {
	annotations := map[string]string{
		"auditId":       entry.ID,
		"action":        entry.Action,
		"actorId":       entry.ActorID,
		"actorName":     entry.ActorName,
		"requestId":     entry.RequestID,
		"requestPath":   entry.RequestPath,
		"requestMethod": entry.RequestMethod,
		"sourceIp":      entry.SourceIP,
	}
	if entry.ResourceName != "" {
		annotations["resourceName"] = entry.ResourceName
	}
	return compactStringMap(annotations)
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

func (s *Service) ExportCSVAuthorized(ctx context.Context, principal domainidentity.Principal, filter domainaudit.Filter) (domainaudit.Export, error) {
	if err := appaccess.AuthorizeRuntimePermission(ctx, s.permissions, principal, appaccess.PermSystemAuditView); err != nil {
		return domainaudit.Export{}, err
	}
	return s.ExportCSV(ctx, filter)
}

func normalizeAuditFilter(filter domainaudit.Filter) domainaudit.Filter {
	return normalizeAuditFilterWithLimit(filter, 500)
}

func normalizeAuditFilterWithLimit(filter domainaudit.Filter, maxLimit int) domainaudit.Filter {
	filter.ActorID = strings.TrimSpace(filter.ActorID)
	filter.ActorName = strings.TrimSpace(filter.ActorName)
	filter.ClusterID = strings.TrimSpace(filter.ClusterID)
	filter.Namespace = strings.TrimSpace(filter.Namespace)
	filter.ResourceKind = strings.TrimSpace(filter.ResourceKind)
	filter.ResourceName = strings.TrimSpace(filter.ResourceName)
	filter.Action = strings.TrimSpace(filter.Action)
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
