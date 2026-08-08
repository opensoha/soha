package monitoring

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	appaccess "github.com/opensoha/soha/internal/application/access"
	domainaudit "github.com/opensoha/soha/internal/domain/audit"
	domaincopilot "github.com/opensoha/soha/internal/domain/copilot"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainobservability "github.com/opensoha/soha/internal/domain/observability"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"github.com/opensoha/soha/internal/platform/requestctx"
	"github.com/opensoha/soha/internal/platform/telemetry"
)

const (
	maxGrafanaDashboardBytes = 2 * 1024 * 1024
	maxDashboardPanels       = 200
	maxDashboardTargets      = 8
	maxDashboardExpression   = 8192
)

var grafanaVariablePattern = regexp.MustCompile(`\$\{?[A-Za-z_][A-Za-z0-9_]*`)

type AuditRecorder interface {
	Record(context.Context, domainaudit.Entry) error
}

func (s *Service) ListDashboards(ctx context.Context, principal domainidentity.Principal) ([]domainobservability.Dashboard, error) {
	if err := s.authorize(ctx, principal, appaccess.PermObserveMonitoringView); err != nil {
		return nil, err
	}
	if s.dashboards == nil {
		return nil, fmt.Errorf("%w: dashboard repository is unavailable", apperrors.ErrInvalidArgument)
	}
	return s.dashboards.ListDashboards(ctx)
}

func (s *Service) ListMetricDataSources(ctx context.Context, principal domainidentity.Principal) ([]domaincopilot.DataSource, error) {
	if err := s.authorize(ctx, principal, appaccess.PermObserveMonitoringView); err != nil {
		return nil, err
	}
	if s.dataSources == nil {
		return []domaincopilot.DataSource{}, nil
	}
	items, err := s.dataSources.ListDataSources(ctx)
	if err != nil {
		return nil, err
	}
	result := make([]domaincopilot.DataSource, 0, len(items))
	for _, item := range items {
		if signalDataSourceReady(item, map[string]struct{}{"prometheus": {}}) {
			result = append(result, item)
		}
	}
	sort.Slice(result, func(i, j int) bool {
		left := strings.ToLower(strings.TrimSpace(result[i].Name))
		right := strings.ToLower(strings.TrimSpace(result[j].Name))
		if left == right {
			return result[i].ID < result[j].ID
		}
		return left < right
	})
	return result, nil
}

func (s *Service) GetDashboard(ctx context.Context, principal domainidentity.Principal, dashboardID string) (domainobservability.Dashboard, error) {
	if err := s.authorize(ctx, principal, appaccess.PermObserveMonitoringView); err != nil {
		return domainobservability.Dashboard{}, err
	}
	if s.dashboards == nil {
		return domainobservability.Dashboard{}, fmt.Errorf("%w: dashboard repository is unavailable", apperrors.ErrInvalidArgument)
	}
	return s.dashboards.GetDashboard(ctx, strings.TrimSpace(dashboardID))
}

func (s *Service) ImportGrafanaDashboard(ctx context.Context, principal domainidentity.Principal, rawJSON, dataSourceID string) (domainobservability.DashboardImportResult, error) {
	if err := s.authorize(ctx, principal, appaccess.ManagedActionPermission(appaccess.PermObserveDashboardsManage, "create")); err != nil {
		return domainobservability.DashboardImportResult{}, err
	}
	if s.dashboards == nil {
		return domainobservability.DashboardImportResult{}, fmt.Errorf("%w: dashboard repository is unavailable", apperrors.ErrInvalidArgument)
	}
	dataSourceID = strings.TrimSpace(dataSourceID)
	if dataSourceID == "" {
		return domainobservability.DashboardImportResult{}, fmt.Errorf("%w: Prometheus data source is required", apperrors.ErrInvalidArgument)
	}
	if _, err := s.selectSignalDataSource(ctx, dataSourceID, map[string]struct{}{"prometheus": {}}); err != nil {
		return domainobservability.DashboardImportResult{}, err
	}
	result, err := importGrafanaDashboard(rawJSON, dataSourceID, time.Now().UTC())
	if err != nil {
		return domainobservability.DashboardImportResult{}, err
	}
	result.Dashboard, err = s.dashboards.CreateDashboard(ctx, result.Dashboard)
	if err != nil {
		return domainobservability.DashboardImportResult{}, err
	}
	s.recordDashboardMutation(ctx, principal, result.Dashboard, "observability.dashboard.import", "imported Grafana dashboard")
	return result, nil
}

func (s *Service) DeleteDashboard(ctx context.Context, principal domainidentity.Principal, dashboardID string) error {
	if err := s.authorize(ctx, principal, appaccess.ManagedActionPermission(appaccess.PermObserveDashboardsManage, "delete")); err != nil {
		return err
	}
	if s.dashboards == nil {
		return fmt.Errorf("%w: dashboard repository is unavailable", apperrors.ErrInvalidArgument)
	}
	dashboard, err := s.dashboards.GetDashboard(ctx, strings.TrimSpace(dashboardID))
	if err != nil {
		return err
	}
	if err := s.dashboards.DeleteDashboard(ctx, dashboard.ID); err != nil {
		return err
	}
	s.recordDashboardMutation(ctx, principal, dashboard, "observability.dashboard.delete", "deleted dashboard")
	return nil
}

func (s *Service) QueryDashboardPanel(ctx context.Context, principal domainidentity.Principal, dashboardID, panelID string, from, to time.Time, step time.Duration) (MetricQueryResult, error) {
	if err := s.authorize(ctx, principal, appaccess.PermObserveMonitoringView); err != nil {
		return MetricQueryResult{}, err
	}
	if err := validateSignalTimeRange(from, to); err != nil {
		return MetricQueryResult{}, err
	}
	if step == 0 {
		step = time.Minute
	}
	if step < time.Second || step > time.Hour {
		return MetricQueryResult{}, fmt.Errorf("%w: metric step must be between 1 and 3600 seconds", apperrors.ErrInvalidArgument)
	}
	if s.dashboards == nil {
		return MetricQueryResult{}, fmt.Errorf("%w: dashboard repository is unavailable", apperrors.ErrInvalidArgument)
	}
	dashboard, err := s.dashboards.GetDashboard(ctx, strings.TrimSpace(dashboardID))
	if err != nil {
		return MetricQueryResult{}, err
	}
	panel, ok := findDashboardPanel(dashboard.Panels, panelID)
	if !ok {
		return MetricQueryResult{}, fmt.Errorf("%w: dashboard panel not found", apperrors.ErrNotFound)
	}
	if !panel.Queryable || len(panel.Targets) == 0 {
		return MetricQueryResult{}, fmt.Errorf("%w: dashboard panel is not queryable", apperrors.ErrInvalidArgument)
	}
	source, err := s.selectSignalDataSource(ctx, dashboard.DataSourceID, map[string]struct{}{"prometheus": {}})
	if err != nil {
		return MetricQueryResult{}, err
	}
	series := make([]telemetry.MetricSeries, 0)
	for _, target := range panel.Targets {
		expression := expandGrafanaBuiltins(target.Expression, from, to, step)
		items, _, queryErr := s.metricBackend().RangeQuery(ctx, source.BackendType, source.ID, source.Config, telemetry.MetricRangeQuery{
			MetricKey: target.RefID, Expression: expression, Legend: target.Legend,
			TimeFrom: from, TimeTo: to, Step: step,
		})
		if queryErr != nil {
			return MetricQueryResult{}, queryErr
		}
		series = append(series, items...)
	}
	return MetricQueryResult{DataSourceID: source.ID, BackendType: source.BackendType, Series: series}, nil
}

func importGrafanaDashboard(rawJSON, dataSourceID string, now time.Time) (domainobservability.DashboardImportResult, error) {
	dataSourceID = strings.TrimSpace(dataSourceID)
	if dataSourceID == "" {
		return domainobservability.DashboardImportResult{}, fmt.Errorf("%w: Prometheus data source is required", apperrors.ErrInvalidArgument)
	}
	rawJSON = strings.TrimSpace(rawJSON)
	if len(rawJSON) < 2 || len(rawJSON) > maxGrafanaDashboardBytes {
		return domainobservability.DashboardImportResult{}, fmt.Errorf("%w: Grafana dashboard JSON must be between 2 bytes and 2 MiB", apperrors.ErrInvalidArgument)
	}
	var wrapper grafanaWrapper
	if err := json.Unmarshal([]byte(rawJSON), &wrapper); err != nil {
		return domainobservability.DashboardImportResult{}, fmt.Errorf("%w: invalid Grafana dashboard JSON", apperrors.ErrInvalidArgument)
	}
	payload := []byte(rawJSON)
	if len(wrapper.Dashboard) > 0 && string(wrapper.Dashboard) != "null" {
		payload = wrapper.Dashboard
	} else if len(wrapper.Spec) > 0 && string(wrapper.Spec) != "null" {
		payload = wrapper.Spec
	}
	var source grafanaDashboard
	if err := json.Unmarshal(payload, &source); err != nil {
		return domainobservability.DashboardImportResult{}, fmt.Errorf("%w: unsupported Grafana dashboard resource", apperrors.ErrInvalidArgument)
	}
	name := strings.TrimSpace(source.Title)
	if name == "" {
		return domainobservability.DashboardImportResult{}, fmt.Errorf("%w: Grafana dashboard title is required", apperrors.ErrInvalidArgument)
	}
	if len(name) > 200 {
		name = name[:200]
	}
	result := domainobservability.DashboardImportResult{Warnings: []domainobservability.DashboardImportWarning{}}
	seen := map[string]int{}
	result.Dashboard = domainobservability.Dashboard{
		ID: "dashboard:" + uuid.NewString(), Name: name, Source: "grafana", SourceSchemaVersion: source.SchemaVersion,
		DataSourceID: dataSourceID, Tags: normalizeDashboardTags(source.Tags), Panels: []domainobservability.DashboardPanel{},
		CreatedAt: now, UpdatedAt: now,
	}
	if len(source.Templating.List) > 0 {
		result.Warnings = appendWarning(result.Warnings, domainobservability.DashboardImportWarning{
			Code: "unsupported_variable", Message: "Grafana dashboard variables are not imported; targets using custom variables are disabled.",
		})
	}
	normalizeGrafanaPanels(source.Panels, &result, seen)
	if result.ImportedPanelCount == 0 {
		return domainobservability.DashboardImportResult{}, fmt.Errorf("%w: dashboard has no supported panels", apperrors.ErrInvalidArgument)
	}
	return result, nil
}

type grafanaWrapper struct {
	Dashboard json.RawMessage `json:"dashboard"`
	Spec      json.RawMessage `json:"spec"`
}

type grafanaDashboard struct {
	Title         string         `json:"title"`
	SchemaVersion int            `json:"schemaVersion"`
	Tags          []string       `json:"tags"`
	Panels        []grafanaPanel `json:"panels"`
	Templating    struct {
		List []json.RawMessage `json:"list"`
	} `json:"templating"`
}

type grafanaPanel struct {
	ID         json.RawMessage `json:"id"`
	Title      string          `json:"title"`
	Type       string          `json:"type"`
	DataSource json.RawMessage `json:"datasource"`
	GridPos    *grafanaGridPos `json:"gridPos"`
	Targets    []struct {
		RefID        string          `json:"refId"`
		Expr         string          `json:"expr"`
		LegendFormat string          `json:"legendFormat"`
		Hide         bool            `json:"hide"`
		DataSource   json.RawMessage `json:"datasource"`
	} `json:"targets"`
	Options struct {
		Content string `json:"content"`
	} `json:"options"`
	Panels []grafanaPanel `json:"panels"`
}

type grafanaGridPos struct {
	X int `json:"x"`
	Y int `json:"y"`
	W int `json:"w"`
	H int `json:"h"`
}

func normalizeGrafanaPanels(panels []grafanaPanel, result *domainobservability.DashboardImportResult, seen map[string]int) {
	for _, source := range panels {
		if result.ImportedPanelCount >= maxDashboardPanels {
			result.SkippedPanelCount++
			result.Warnings = appendWarning(result.Warnings, domainobservability.DashboardImportWarning{Code: "unsupported_feature", Message: "Panels above the 200-panel import limit were skipped."})
			continue
		}
		panelID := uniquePanelID(grafanaPanelID(source.ID, result.ImportedPanelCount+result.SkippedPanelCount+1), seen)
		panelType, supported := normalizeGrafanaPanelType(source.Type)
		if !supported {
			result.SkippedPanelCount++
			result.Warnings = appendWarning(result.Warnings, domainobservability.DashboardImportWarning{Code: "unsupported_panel_type", Message: fmt.Sprintf("Grafana panel type %q was skipped.", strings.TrimSpace(source.Type)), PanelID: panelID})
			continue
		}
		panel := domainobservability.DashboardPanel{
			ID: panelID, Title: boundedPanelTitle(source.Title, panelID), Type: panelType,
			Layout: normalizeGrafanaLayout(source.GridPos, result.ImportedPanelCount), Targets: []domainobservability.DashboardTarget{},
		}
		if panelType == "text" {
			panel.Markdown = truncateString(source.Options.Content, 32768)
		}
		if panelType == "timeseries" || panelType == "stat" {
			for _, target := range source.Targets {
				if len(panel.Targets) >= maxDashboardTargets {
					result.Warnings = appendWarning(result.Warnings, domainobservability.DashboardImportWarning{Code: "skipped_target", Message: "Targets above the 8-target panel limit were skipped.", PanelID: panelID})
					break
				}
				expression := strings.TrimSpace(target.Expr)
				dataSource := target.DataSource
				if len(dataSource) == 0 || string(dataSource) == "null" {
					dataSource = source.DataSource
				}
				if !supportsGrafanaPrometheusBinding(dataSource) {
					result.Warnings = appendWarning(result.Warnings, domainobservability.DashboardImportWarning{Code: "skipped_target", Message: "A target using a non-Prometheus Grafana data source was skipped.", PanelID: panelID})
					continue
				}
				if target.Hide || expression == "" || len(expression) > maxDashboardExpression {
					result.Warnings = appendWarning(result.Warnings, domainobservability.DashboardImportWarning{Code: "skipped_target", Message: "A hidden, empty, or oversized target was skipped.", PanelID: panelID})
					continue
				}
				if hasUnsupportedGrafanaVariable(expression) {
					result.Warnings = appendWarning(result.Warnings, domainobservability.DashboardImportWarning{Code: "unsupported_variable", Message: "A target using custom Grafana variables was skipped.", PanelID: panelID})
					continue
				}
				refID := strings.TrimSpace(target.RefID)
				if refID == "" {
					refID = string(rune('A' + len(panel.Targets)))
				}
				panel.Targets = append(panel.Targets, domainobservability.DashboardTarget{RefID: truncateString(refID, 16), Expression: expression, Legend: truncateString(strings.TrimSpace(target.LegendFormat), 256)})
			}
			panel.Queryable = len(panel.Targets) > 0
		}
		result.Dashboard.Panels = append(result.Dashboard.Panels, panel)
		result.ImportedPanelCount++
		if panelType == "row" && len(source.Panels) > 0 {
			normalizeGrafanaPanels(source.Panels, result, seen)
		}
	}
}

func supportsGrafanaPrometheusBinding(raw json.RawMessage) bool {
	if len(raw) == 0 || string(raw) == "null" {
		return true
	}
	var value struct {
		Type string `json:"type"`
	}
	if json.Unmarshal(raw, &value) != nil || strings.TrimSpace(value.Type) == "" {
		return true
	}
	return strings.Contains(strings.ToLower(strings.TrimSpace(value.Type)), "prometheus")
}

func normalizeGrafanaPanelType(value string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "timeseries", "graph":
		return "timeseries", true
	case "stat", "singlestat":
		return "stat", true
	case "text":
		return "text", true
	case "row":
		return "row", true
	default:
		return "", false
	}
}

func normalizeGrafanaLayout(value *grafanaGridPos, index int) domainobservability.DashboardPanelLayout {
	if value == nil {
		return domainobservability.DashboardPanelLayout{X: (index % 2) * 12, Y: (index / 2) * 8, W: 12, H: 8}
	}
	x := max(0, min(value.X, 23))
	w := max(1, min(value.W, 24-x))
	return domainobservability.DashboardPanelLayout{X: x, Y: max(0, value.Y), W: w, H: max(1, min(value.H, 100))}
}

func grafanaPanelID(raw json.RawMessage, fallback int) string {
	var text string
	if len(raw) > 0 && json.Unmarshal(raw, &text) == nil && strings.TrimSpace(text) != "" {
		return truncateString(strings.TrimSpace(text), 128)
	}
	var number json.Number
	if len(raw) > 0 && json.Unmarshal(raw, &number) == nil && number.String() != "" {
		return truncateString(number.String(), 128)
	}
	return fmt.Sprintf("panel-%d", fallback)
}

func uniquePanelID(id string, seen map[string]int) string {
	seen[id]++
	if seen[id] == 1 {
		return id
	}
	return truncateString(id+"-"+strconv.Itoa(seen[id]), 128)
}

func boundedPanelTitle(title, panelID string) string {
	title = strings.TrimSpace(title)
	if title == "" {
		title = "Panel " + panelID
	}
	return truncateString(title, 200)
}

func normalizeDashboardTags(values []string) []string {
	seen := map[string]struct{}{}
	result := make([]string, 0, min(len(values), 50))
	for _, value := range values {
		value = truncateString(strings.TrimSpace(value), 100)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
		if len(result) == 50 {
			break
		}
	}
	return result
}

func appendWarning(items []domainobservability.DashboardImportWarning, item domainobservability.DashboardImportWarning) []domainobservability.DashboardImportWarning {
	if len(items) >= 500 {
		return items
	}
	return append(items, item)
}

func hasUnsupportedGrafanaVariable(expression string) bool {
	for _, builtin := range []string{"$__interval", "${__interval}", "$__rate_interval", "${__rate_interval}", "$__range", "${__range}", "$__range_s", "${__range_s}", "$__range_ms", "${__range_ms}"} {
		expression = strings.ReplaceAll(expression, builtin, "")
	}
	return grafanaVariablePattern.MatchString(expression)
}

func expandGrafanaBuiltins(expression string, from, to time.Time, step time.Duration) string {
	interval := durationPromQL(step)
	rateInterval := durationPromQL(max(step*4, time.Minute))
	rangeDuration := to.Sub(from)
	replacements := map[string]string{
		"$__interval": interval, "${__interval}": interval,
		"$__rate_interval": rateInterval, "${__rate_interval}": rateInterval,
		"$__range": durationPromQL(rangeDuration), "${__range}": durationPromQL(rangeDuration),
		"$__range_s": strconv.FormatInt(int64(rangeDuration.Seconds()), 10), "${__range_s}": strconv.FormatInt(int64(rangeDuration.Seconds()), 10),
		"$__range_ms": strconv.FormatInt(rangeDuration.Milliseconds(), 10), "${__range_ms}": strconv.FormatInt(rangeDuration.Milliseconds(), 10),
	}
	keys := make([]string, 0, len(replacements))
	for key := range replacements {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool { return len(keys[i]) > len(keys[j]) })
	for _, key := range keys {
		expression = strings.ReplaceAll(expression, key, replacements[key])
	}
	return expression
}

func durationPromQL(value time.Duration) string {
	seconds := max(int64(value.Seconds()), int64(1))
	if seconds%3600 == 0 {
		return fmt.Sprintf("%dh", seconds/3600)
	}
	if seconds%60 == 0 {
		return fmt.Sprintf("%dm", seconds/60)
	}
	return fmt.Sprintf("%ds", seconds)
}

func findDashboardPanel(items []domainobservability.DashboardPanel, id string) (domainobservability.DashboardPanel, bool) {
	id = strings.TrimSpace(id)
	for _, item := range items {
		if item.ID == id {
			return item, true
		}
	}
	return domainobservability.DashboardPanel{}, false
}

func truncateString(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	return value[:limit]
}

func (s *Service) recordDashboardMutation(ctx context.Context, principal domainidentity.Principal, item domainobservability.Dashboard, action, summary string) {
	if s.audit == nil {
		return
	}
	metadata := requestctx.FromContext(ctx)
	_ = s.audit.Record(ctx, domainaudit.Entry{
		ActorID: principal.UserID, ActorName: principal.UserName, Roles: principal.Roles, Teams: principal.Teams,
		ResourceKind: "ObservabilityDashboard", ResourceName: item.ID, Action: action, Result: "success", Summary: summary,
		RequestPath: metadata.Path, RequestMethod: metadata.Method, RequestID: metadata.RequestID, SourceIP: metadata.SourceIP, CreatedAt: time.Now().UTC(),
	})
}
