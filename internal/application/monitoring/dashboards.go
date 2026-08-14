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

var grafanaVariablePattern = regexp.MustCompile(`\$(?:\{([A-Za-z_][A-Za-z0-9_]*)\}|([A-Za-z_][A-Za-z0-9_]*))`)
var dashboardVariableNamePattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

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
	s.recordMonitoringMutation(ctx, principal, "ObservabilityDashboard", result.Dashboard.ID, "observability.dashboard.import", "imported Grafana dashboard")
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
	s.recordMonitoringMutation(ctx, principal, "ObservabilityDashboard", dashboard.ID, "observability.dashboard.delete", "deleted dashboard")
	return nil
}

func (s *Service) QueryDashboardPanel(ctx context.Context, principal domainidentity.Principal, dashboardID, panelID string, from, to time.Time, step time.Duration, variableInput map[string]string) (MetricQueryResult, error) {
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
	variables, err := resolveDashboardVariables(dashboard.Variables, variableInput)
	if err != nil {
		return MetricQueryResult{}, err
	}
	source, err := s.selectSignalDataSource(ctx, dashboard.DataSourceID, map[string]struct{}{"prometheus": {}})
	if err != nil {
		return MetricQueryResult{}, err
	}
	series := make([]telemetry.MetricSeries, 0)
	expressions := make([]string, 0, len(panel.Targets))
	for _, target := range panel.Targets {
		if err := validateDashboardTargetBinding(dashboard, target); err != nil {
			return MetricQueryResult{}, err
		}
		expression := expandDashboardVariables(target.Expression, variables)
		expression = expandGrafanaBuiltins(expression, from, to, step)
		expressions = append(expressions, expression)
		items, _, queryErr := s.metricBackend().RangeQuery(ctx, source.BackendType, source.ID, source.Config, telemetry.MetricRangeQuery{
			MetricKey: target.RefID, Expression: expression, Legend: target.Legend,
			TimeFrom: from, TimeTo: to, Step: step,
		})
		if queryErr != nil {
			return MetricQueryResult{}, queryErr
		}
		series = append(series, items...)
	}
	observedAt := time.Now().UTC()
	state := "success"
	if len(series) == 0 {
		state = "empty"
	}
	warnings := []string{}
	if len(expressions) > 1 {
		warnings = append(warnings, "QuerySnapshot records the first panel target; the response series includes every approved target.")
	}
	snapshotQuery := ""
	if len(expressions) > 0 {
		snapshotQuery = expressions[0]
	}
	return MetricQueryResult{
		DataSourceID: source.ID, BackendType: source.BackendType, Series: series,
		Meta: &QueryMeta{State: state, Warnings: warnings, ObservedAt: observedAt, Snapshot: map[string]any{
			"version": "v1", "signal": "metrics", "dataSourceId": source.ID, "backendType": source.BackendType,
			"context":       map[string]any{"version": "v1", "scope": map[string]any{}, "timeRange": map[string]any{"from": from, "to": to}},
			"queryLanguage": "promql", "query": snapshotQuery, "createdAt": observedAt,
		}},
	}, nil
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
	if isGrafanaV2Resource(wrapper) {
		return domainobservability.DashboardImportResult{}, fmt.Errorf("%w: Grafana dashboard V2 resources are not supported", apperrors.ErrInvalidArgument)
	}
	payload := []byte(rawJSON)
	sourceFormat := "classic"
	if len(wrapper.Dashboard) > 0 && string(wrapper.Dashboard) != "null" {
		payload = wrapper.Dashboard
	} else if len(wrapper.Spec) > 0 && string(wrapper.Spec) != "null" {
		payload = wrapper.Spec
		sourceFormat = "v1"
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
		ID: "dashboard:" + uuid.NewString(), Name: name, Source: "grafana", SourceFormat: sourceFormat, SourceSchemaVersion: source.SchemaVersion,
		DataSourceID: dataSourceID, Tags: normalizeDashboardTags(source.Tags), Panels: []domainobservability.DashboardPanel{},
		Variables: []domainobservability.DashboardVariable{}, DataSourceBindings: []domainobservability.DashboardDataSourceBinding{},
		ImportWarnings: []domainobservability.DashboardImportWarning{}, RawJSON: rawJSON,
		CreatedAt: now, UpdatedAt: now,
	}
	result.Dashboard.Variables, result.Warnings = normalizeGrafanaVariables(source.Templating.List, result.Warnings)
	result.Dashboard.DataSourceBindings = grafanaDataSourceBindings(source, dataSourceID)
	if len(result.Dashboard.DataSourceBindings) > 0 {
		result.Warnings = appendWarning(result.Warnings, domainobservability.DashboardImportWarning{
			Code: "datasource_rebound", Message: "Grafana Prometheus data-source references are mapped to the selected Soha data source.",
		})
	}
	normalizeGrafanaPanels(source.Panels, &result, seen)
	result.Dashboard.ImportWarnings = append([]domainobservability.DashboardImportWarning(nil), result.Warnings...)
	return result, nil
}

type grafanaWrapper struct {
	APIVersion string          `json:"apiVersion"`
	Kind       string          `json:"kind"`
	Dashboard  json.RawMessage `json:"dashboard"`
	Spec       json.RawMessage `json:"spec"`
}

type grafanaDashboard struct {
	Title         string         `json:"title"`
	SchemaVersion int            `json:"schemaVersion"`
	Tags          []string       `json:"tags"`
	Panels        []grafanaPanel `json:"panels"`
	Templating    struct {
		List []grafanaVariable `json:"list"`
	} `json:"templating"`
}

type grafanaVariable struct {
	Name    string `json:"name"`
	Label   string `json:"label"`
	Type    string `json:"type"`
	Query   any    `json:"query"`
	Current struct {
		Text  any `json:"text"`
		Value any `json:"value"`
	} `json:"current"`
	Options []struct {
		Text  string `json:"text"`
		Value any    `json:"value"`
	} `json:"options"`
}

type grafanaPanel struct {
	RawJSON    json.RawMessage `json:"-"`
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

func (panel *grafanaPanel) UnmarshalJSON(raw []byte) error {
	type alias grafanaPanel
	var value alias
	if err := json.Unmarshal(raw, &value); err != nil {
		return err
	}
	*panel = grafanaPanel(value)
	panel.RawJSON = append(panel.RawJSON[:0], raw...)
	return nil
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
			result.Warnings = appendWarning(result.Warnings, domainobservability.DashboardImportWarning{Code: "unsupported_panel_type", Message: fmt.Sprintf("Grafana panel type %q was retained but cannot be rendered.", strings.TrimSpace(source.Type)), PanelID: panelID})
			result.Dashboard.Panels = append(result.Dashboard.Panels, domainobservability.DashboardPanel{
				ID: panelID, Title: boundedPanelTitle(source.Title, panelID), Type: "unsupported", SourcePanelType: truncateString(strings.TrimSpace(source.Type), 128),
				Layout: normalizeGrafanaLayout(source.GridPos, result.ImportedPanelCount), Targets: []domainobservability.DashboardTarget{}, Unsupported: true, RawJSON: truncateString(string(source.RawJSON), maxGrafanaDashboardBytes),
			})
			result.ImportedPanelCount++
			continue
		}
		panel := domainobservability.DashboardPanel{
			ID: panelID, Title: boundedPanelTitle(source.Title, panelID), Type: panelType,
			SourcePanelType: truncateString(strings.TrimSpace(source.Type), 128), Layout: normalizeGrafanaLayout(source.GridPos, result.ImportedPanelCount), Targets: []domainobservability.DashboardTarget{},
		}
		if panelType == "text" {
			panel.Markdown = truncateString(source.Options.Content, 32768)
		}
		if panelType == "timeseries" || panelType == "table" || panelType == "stat" || panelType == "gauge" {
			normalizeGrafanaTargets(source, &panel, result)
		}
		result.Dashboard.Panels = append(result.Dashboard.Panels, panel)
		result.ImportedPanelCount++
		if panelType == "row" && len(source.Panels) > 0 {
			normalizeGrafanaPanels(source.Panels, result, seen)
		}
	}
}

func normalizeGrafanaTargets(source grafanaPanel, panel *domainobservability.DashboardPanel, result *domainobservability.DashboardImportResult) {
	for _, target := range source.Targets {
		if len(panel.Targets) >= maxDashboardTargets {
			result.Warnings = appendWarning(result.Warnings, domainobservability.DashboardImportWarning{Code: "skipped_target", Message: "Targets above the 8-target panel limit were skipped.", PanelID: panel.ID})
			break
		}
		expression := strings.TrimSpace(target.Expr)
		dataSource := target.DataSource
		if len(dataSource) == 0 || string(dataSource) == "null" {
			dataSource = source.DataSource
		}
		if !supportsGrafanaPrometheusBinding(dataSource) {
			result.Warnings = appendWarning(result.Warnings, domainobservability.DashboardImportWarning{Code: "skipped_target", Message: "A target using a non-Prometheus Grafana data source was skipped.", PanelID: panel.ID})
			continue
		}
		if target.Hide || expression == "" || len(expression) > maxDashboardExpression {
			result.Warnings = appendWarning(result.Warnings, domainobservability.DashboardImportWarning{Code: "skipped_target", Message: "A hidden, empty, or oversized target was skipped.", PanelID: panel.ID})
			continue
		}
		if hasUnsupportedGrafanaVariable(expression, result.Dashboard.Variables) {
			result.Warnings = appendWarning(result.Warnings, domainobservability.DashboardImportWarning{Code: "unsupported_variable", Message: "A target using custom Grafana variables was skipped.", PanelID: panel.ID})
			continue
		}
		refID := strings.TrimSpace(target.RefID)
		if refID == "" {
			refID = string(rune('A' + len(panel.Targets)))
		}
		bindingType, bindingUID := grafanaDataSourceRef(dataSource)
		panel.Targets = append(panel.Targets, domainobservability.DashboardTarget{
			RefID: truncateString(refID, 16), Expression: expression, Legend: truncateString(strings.TrimSpace(target.LegendFormat), 256),
			DataSourceType: bindingType, DataSourceUID: bindingUID, DataSourceID: result.Dashboard.DataSourceID,
		})
	}
	panel.Queryable = len(panel.Targets) > 0
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

func isGrafanaV2Resource(wrapper grafanaWrapper) bool {
	version := strings.ToLower(strings.TrimSpace(wrapper.APIVersion))
	return strings.Contains(version, "/v2") || strings.Contains(version, "v2beta") || strings.Contains(version, "v2alpha")
}

func normalizeGrafanaVariables(items []grafanaVariable, warnings []domainobservability.DashboardImportWarning) ([]domainobservability.DashboardVariable, []domainobservability.DashboardImportWarning) {
	result := make([]domainobservability.DashboardVariable, 0, min(len(items), 50))
	for _, item := range items {
		name := strings.TrimSpace(item.Name)
		variableType := strings.ToLower(strings.TrimSpace(item.Type))
		if len(result) >= 50 || len(name) > 64 || !dashboardVariableNamePattern.MatchString(name) || (variableType != "custom" && variableType != "constant") {
			warnings = appendWarning(warnings, domainobservability.DashboardImportWarning{Code: "unsupported_variable", Message: "Only bounded Grafana custom and constant variables are available in Soha."})
			continue
		}
		options := grafanaVariableOptions(item, variableType)
		if len(options) == 0 {
			warnings = appendWarning(warnings, domainobservability.DashboardImportWarning{Code: "unsupported_variable", Message: fmt.Sprintf("Grafana variable %q has no bounded options.", name)})
			continue
		}
		current := grafanaScalar(item.Current.Value)
		if current == "" {
			current = grafanaScalar(item.Current.Text)
		}
		if !slicesContains(options, current) {
			current = options[0]
		}
		result = append(result, domainobservability.DashboardVariable{
			Name: name, Label: truncateString(strings.TrimSpace(item.Label), 200), Type: variableType,
			Current: current, Options: options,
		})
	}
	return result, warnings
}

func grafanaVariableOptions(item grafanaVariable, variableType string) []string {
	values := make([]string, 0, min(len(item.Options)+1, 100))
	seen := map[string]struct{}{}
	appendValue := func(value string) {
		value = truncateString(strings.TrimSpace(value), 512)
		if value == "" || len(values) >= 100 {
			return
		}
		if _, ok := seen[value]; ok {
			return
		}
		seen[value] = struct{}{}
		values = append(values, value)
	}
	for _, option := range item.Options {
		value := grafanaScalar(option.Value)
		if value == "" {
			value = option.Text
		}
		appendValue(value)
	}
	query := grafanaScalar(item.Query)
	if variableType == "constant" {
		appendValue(query)
	} else if len(values) == 0 {
		for _, value := range strings.Split(query, ",") {
			appendValue(value)
		}
	}
	return values
}

func grafanaScalar(value any) string {
	switch typed := value.(type) {
	case string:
		return strings.TrimSpace(typed)
	case json.Number:
		return typed.String()
	case float64:
		return strconv.FormatFloat(typed, 'f', -1, 64)
	default:
		return ""
	}
}

func slicesContains(items []string, value string) bool {
	for _, item := range items {
		if item == value {
			return true
		}
	}
	return false
}

func grafanaDataSourceBindings(source grafanaDashboard, dataSourceID string) []domainobservability.DashboardDataSourceBinding {
	result := make([]domainobservability.DashboardDataSourceBinding, 0)
	seen := map[string]struct{}{}
	var visit func([]grafanaPanel)
	visit = func(panels []grafanaPanel) {
		for _, panel := range panels {
			refs := []json.RawMessage{panel.DataSource}
			for _, target := range panel.Targets {
				refs = append(refs, target.DataSource)
			}
			for _, raw := range refs {
				bindingType, uid := grafanaDataSourceRef(raw)
				if bindingType == "" || !strings.Contains(bindingType, "prometheus") {
					continue
				}
				key := bindingType + "\x00" + uid
				if _, ok := seen[key]; ok {
					continue
				}
				seen[key] = struct{}{}
				result = append(result, domainobservability.DashboardDataSourceBinding{Type: bindingType, UID: uid, DataSourceID: dataSourceID})
			}
			visit(panel.Panels)
		}
	}
	visit(source.Panels)
	return result
}

func grafanaDataSourceRef(raw json.RawMessage) (string, string) {
	if len(raw) == 0 || string(raw) == "null" {
		return "", ""
	}
	var uid string
	if json.Unmarshal(raw, &uid) == nil {
		return "prometheus", truncateString(strings.TrimSpace(uid), 200)
	}
	var value struct {
		Type string `json:"type"`
		UID  string `json:"uid"`
	}
	if json.Unmarshal(raw, &value) != nil {
		return "", ""
	}
	bindingType := strings.ToLower(truncateString(strings.TrimSpace(value.Type), 128))
	if bindingType == "" && strings.TrimSpace(value.UID) != "" {
		bindingType = "prometheus"
	}
	return bindingType, truncateString(strings.TrimSpace(value.UID), 200)
}

func normalizeGrafanaPanelType(value string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "timeseries", "graph":
		return "timeseries", true
	case "stat", "singlestat":
		return "stat", true
	case "table":
		return "table", true
	case "gauge":
		return "gauge", true
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

func hasUnsupportedGrafanaVariable(expression string, variables []domainobservability.DashboardVariable) bool {
	for _, builtin := range []string{"$__interval", "${__interval}", "$__rate_interval", "${__rate_interval}", "$__range", "${__range}", "$__range_s", "${__range_s}", "$__range_ms", "${__range_ms}"} {
		expression = strings.ReplaceAll(expression, builtin, "")
	}
	allowed := make(map[string]struct{}, len(variables))
	for _, variable := range variables {
		allowed[variable.Name] = struct{}{}
	}
	for _, match := range grafanaVariablePattern.FindAllStringSubmatch(expression, -1) {
		name := match[1]
		if name == "" {
			name = match[2]
		}
		if _, ok := allowed[name]; !ok {
			return true
		}
	}
	return false
}

func resolveDashboardVariables(definitions []domainobservability.DashboardVariable, input map[string]string) (map[string]string, error) {
	if len(input) > 50 {
		return nil, fmt.Errorf("%w: dashboard variables exceed the 50-variable limit", apperrors.ErrInvalidArgument)
	}
	definitionsByName := make(map[string]domainobservability.DashboardVariable, len(definitions))
	result := make(map[string]string, len(definitions))
	for _, definition := range definitions {
		definitionsByName[definition.Name] = definition
		value := definition.Current
		if value == "" && len(definition.Options) > 0 {
			value = definition.Options[0]
		}
		result[definition.Name] = value
	}
	for name, value := range input {
		definition, ok := definitionsByName[name]
		if !ok || len(value) > 512 || !slicesContains(definition.Options, value) {
			return nil, fmt.Errorf("%w: dashboard variable %q has an unapproved value", apperrors.ErrInvalidArgument, name)
		}
		result[name] = value
	}
	return result, nil
}

func expandDashboardVariables(expression string, variables map[string]string) string {
	return grafanaVariablePattern.ReplaceAllStringFunc(expression, func(match string) string {
		parts := grafanaVariablePattern.FindStringSubmatch(match)
		name := parts[1]
		if name == "" {
			name = parts[2]
		}
		if value, ok := variables[name]; ok {
			return value
		}
		return match
	})
}

func validateDashboardTargetBinding(dashboard domainobservability.Dashboard, target domainobservability.DashboardTarget) error {
	if target.DataSourceID != "" && target.DataSourceID != dashboard.DataSourceID {
		return fmt.Errorf("%w: dashboard target data source mapping is invalid", apperrors.ErrInvalidArgument)
	}
	if target.DataSourceType != "" && !strings.Contains(strings.ToLower(target.DataSourceType), "prometheus") {
		return fmt.Errorf("%w: dashboard target data source is not Prometheus", apperrors.ErrInvalidArgument)
	}
	if target.DataSourceUID == "" {
		return nil
	}
	for _, binding := range dashboard.DataSourceBindings {
		if binding.UID == target.DataSourceUID && binding.DataSourceID == dashboard.DataSourceID && strings.Contains(strings.ToLower(binding.Type), "prometheus") {
			return nil
		}
	}
	return fmt.Errorf("%w: dashboard target data source mapping is unavailable", apperrors.ErrInvalidArgument)
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

func (s *Service) recordMonitoringMutation(ctx context.Context, principal domainidentity.Principal, resourceKind, resourceName, action, summary string) {
	if s.audit == nil {
		return
	}
	metadata := requestctx.FromContext(ctx)
	_ = s.audit.Record(ctx, domainaudit.Entry{
		ActorID: principal.UserID, ActorName: principal.UserName, Roles: principal.Roles, Teams: principal.Teams,
		ResourceKind: resourceKind, ResourceName: resourceName, Action: action, Result: "success", Summary: summary,
		RequestPath: metadata.Path, RequestMethod: metadata.Method, RequestID: metadata.RequestID, SourceIP: metadata.SourceIP, CreatedAt: time.Now().UTC(),
	})
}
