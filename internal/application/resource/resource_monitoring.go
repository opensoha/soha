package resource

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"

	cr "github.com/opensoha/soha-contracts/resource"
	domainaccess "github.com/opensoha/soha/internal/domain/access"
	domaincluster "github.com/opensoha/soha/internal/domain/cluster"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

var monitoringNodePattern = regexp.MustCompile(`^[a-z0-9]([a-z0-9.-]*[a-z0-9])?$`)
var monitoringLabelPattern = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_]*$`)

type monitoringWindow struct {
	from, to time.Time
	step     int
	live     bool
}

func normalizeMonitoringQuery(input cr.ResourceMonitoringQuery, now time.Time) (cr.ResourceMonitoringQuery, monitoringWindow, error) {
	if input.View == "" {
		input.View = "overview"
	}
	if !slices.Contains([]string{"overview", "cpu", "memory", "conntrack", "pods"}, input.View) || len(input.Node) > 253 || (input.Node != "" && !monitoringNodePattern.MatchString(input.Node)) {
		return input, monitoringWindow{}, apperrors.ErrInvalidArgument
	}
	if input.RangeMinutes == 0 {
		input.RangeMinutes = 60
	}
	if input.StepSeconds == 0 {
		input.StepSeconds = 60
	}
	if input.RangeMinutes < 1 || input.RangeMinutes > 1440 || input.StepSeconds < 15 || input.StepSeconds > 3600 {
		return input, monitoringWindow{}, apperrors.ErrInvalidArgument
	}
	end := now.UTC().Truncate(time.Second)
	live := input.To == ""
	if input.To != "" {
		parsed, err := time.Parse(time.RFC3339, input.To)
		if err != nil {
			return input, monitoringWindow{}, apperrors.ErrInvalidArgument
		}
		end = parsed.UTC()
	}
	start := end.Add(-time.Duration(input.RangeMinutes) * time.Minute)
	if input.From != "" {
		parsed, err := time.Parse(time.RFC3339, input.From)
		if err != nil {
			return input, monitoringWindow{}, apperrors.ErrInvalidArgument
		}
		start = parsed.UTC()
	}
	duration := end.Sub(start)
	if duration < time.Minute || duration > 24*time.Hour || end.After(now.Add(time.Minute)) {
		return input, monitoringWindow{}, apperrors.ErrInvalidArgument
	}
	step := max(input.StepSeconds, int((duration.Seconds()+cr.ResourceMonitoringMaxPoints-2)/(cr.ResourceMonitoringMaxPoints-1)))
	return input, monitoringWindow{from: start.Truncate(time.Second), to: end.Truncate(time.Second), step: step, live: live}, nil
}

func (i *Inventory) authorizeMonitoring(ctx context.Context, principal domainidentity.Principal, clusterID, view string) (domaincluster.Connection, error) {
	connection, decision, err := i.authorize(ctx, principal, clusterID, "", "Node", domainaccess.ActionView)
	if err != nil {
		return connection, err
	}
	if !clusterWideResourceScope(decision) {
		return connection, apperrors.ErrAccessDenied
	}
	request := i.resourceAccessRequest(ctx, principal, connection, "", "resource-monitoring", view, domainaccess.ActionView)
	request.PermissionKey = "platform.resource-monitoring." + view + ".view"
	decision, err = i.authorizer.Authorize(ctx, request)
	if err != nil {
		return connection, err
	}
	if !clusterWideResourceScope(decision) {
		return connection, apperrors.ErrAccessDenied
	}
	return connection, nil
}

func (i *Inventory) GetResourceMonitoring(ctx context.Context, principal domainidentity.Principal, clusterID string, input cr.ResourceMonitoringQuery) (cr.ResourceMonitoringView, error) {
	input, window, err := normalizeMonitoringQuery(input, time.Now())
	if err != nil {
		return cr.ResourceMonitoringView{}, err
	}
	connection, err := i.authorizeMonitoring(ctx, principal, clusterID, input.View)
	if err != nil {
		return cr.ResourceMonitoringView{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	ctx = context.WithValue(ctx, prometheusClusterKey{}, clusterID)
	inventory, inventoryErr := i.monitoringInventory(ctx, connection)
	view := cr.ResourceMonitoringView{ClusterID: clusterID, View: input.View, Node: input.Node, From: window.from.Format(time.RFC3339), To: window.to.Format(time.RFC3339), StepSeconds: window.step, GeneratedAt: time.Now().UTC().Format(time.RFC3339), SnapshotQuality: inventory.Quality, SnapshotObservedAt: inventory.ObservedAt, Nodes: []cr.ResourceMonitoringNode{}, Panels: []cr.ResourceMonitoringPanel{}, UnscheduledPendingPods: inventory.UnscheduledPendingPods, Truncated: inventory.Truncated}
	if inventoryErr != nil {
		view.SnapshotQuality = "error"
		if errors.Is(inventoryErr, apperrors.ErrUnsupportedOperation) {
			view.SnapshotQuality = "unsupported"
		}
	}
	inventory.Nodes = validMonitoringNodes(clusterID, inventory.Nodes)
	for _, node := range inventory.Nodes {
		view.NodeOptions = append(view.NodeOptions, node.NodeRef)
		if input.Node == "" || node.NodeRef.Name == input.Node {
			view.Nodes = append(view.Nodes, node)
		}
	}
	if input.Node != "" && len(view.Nodes) == 0 && inventoryErr == nil && inventory.Quality == "valid" {
		return view, apperrors.ErrNotFound
	}
	settings := i.resolveClusterPrometheusSettings(ctx, clusterID)
	definitions := monitoringDefinitions(input.View, clusterID, settings.ClusterLabel)
	state, reason := monitoringPrerequisites(settings.Enabled, settings.BaseURL, settings.ClusterLabel, len(view.Nodes), view.SnapshotQuality)
	if state != "" {
		for _, definition := range definitions {
			view.Panels = append(view.Panels, monitoringEmptyPanel(definition, state, reason))
		}
		return view, nil
	}
	window, historicalCount, budgetTruncated := fitMonitoringBudget(window, len(view.Nodes), len(definitions))
	view.StepSeconds = window.step
	view.Truncated = view.Truncated || budgetTruncated
	historicalNodes := view.Nodes[:historicalCount]
	results := i.queryMonitoringDefinitions(ctx, clusterID, settings, definitions, window)
	aliases := monitoringNodeAliases(inventory.Nodes, results["node.identity"].series)
	remaining := cr.ResourceMonitoringMaxTotalPoints
	for _, definition := range definitions {
		panel := buildMonitoringPanel(definition, results[definition.key], historicalNodes, aliases, window, &remaining)
		if budgetTruncated {
			panel.Quality = "partial"
			panel.ReasonCode = "point_budget"
		}
		view.Panels = append(view.Panels, panel)
	}
	view.Truncated = view.Truncated || remaining <= 0
	_ = i.recordAudit(ctx, principal, clusterID, "", "Node", input.Node, "resource-monitoring", "success", fmt.Sprintf("view=%s nodes=%d panels=%d", input.View, len(view.Nodes), len(view.Panels)))
	return view, nil
}

func (i *Inventory) monitoringInventory(ctx context.Context, connection domaincluster.Connection) (cr.ResourceMonitoringInventory, error) {
	if connection.Summary.ConnectionMode == domaincluster.ConnectionModeAgent {
		client, err := resolveAgentClient(i.monitoringAgent, connection)
		if err != nil {
			return cr.ResourceMonitoringInventory{}, err
		}
		return client.ResourceMonitoringInventory(ctx)
	}
	if i.monitoringDirect == nil {
		return cr.ResourceMonitoringInventory{}, apperrors.ErrUnsupportedOperation
	}
	return i.monitoringDirect.ResourceMonitoringInventory(ctx, connection.Summary.ID)
}
func validMonitoringNodes(clusterID string, nodes []cr.ResourceMonitoringNode) []cr.ResourceMonitoringNode {
	counts := map[string]int{}
	for _, node := range nodes {
		counts[node.NodeRef.Name]++
	}
	result := []cr.ResourceMonitoringNode{}
	for index, node := range nodes {
		if index >= cr.ResourceMonitoringMaxNodes {
			break
		}
		ref := node.NodeRef
		if ref.ClusterID != clusterID || ref.Kind != "Node" || ref.Namespace != "" || ref.UID == "" || !monitoringNodePattern.MatchString(ref.Name) || counts[ref.Name] != 1 {
			continue
		}
		result = append(result, node)
	}
	slices.SortFunc(result, func(a, b cr.ResourceMonitoringNode) int { return strings.Compare(a.NodeRef.Name, b.NodeRef.Name) })
	return result
}
func monitoringPrerequisites(enabled bool, url, label string, nodeCount int, quality string) (string, string) {
	if !enabled || strings.TrimSpace(url) == "" {
		return "not_configured", "prometheus_not_configured"
	}
	if !monitoringLabelPattern.MatchString(label) {
		return "not_configured", "cluster_label_required"
	}
	if nodeCount == 0 {
		if quality == "error" || quality == "denied" || quality == "partial" {
			return quality, "node_snapshot_unavailable"
		}
		if quality == "unsupported" {
			return "unsupported", "upgrade_agent"
		}
		return "empty", "no_verified_node_identity"
	}
	return "", ""
}
func monitoringEmptyPanel(definition monitoringDefinition, quality, reason string) cr.ResourceMonitoringPanel {
	return cr.ResourceMonitoringPanel{Key: definition.key, Label: definition.label, Unit: definition.unit, Quality: quality, ReasonCode: reason, Estimated: definition.estimated, WindowSeconds: definition.window, Series: []cr.ResourceMonitoringSeries{}}
}

func fitMonitoringBudget(window monitoringWindow, nodes, panels int) (monitoringWindow, int, bool) {
	if nodes == 0 || panels == 0 {
		return window, nodes, false
	}
	pointsPerSeries := max(2, min(cr.ResourceMonitoringMaxPoints, cr.ResourceMonitoringMaxTotalPoints/(nodes*panels)))
	duration := int(window.to.Sub(window.from).Seconds())
	window.step = min(3600, max(window.step, (duration+pointsPerSeries-2)/(pointsPerSeries-1)))
	actualPoints := duration/window.step + 1
	allowedNodes := min(nodes, cr.ResourceMonitoringMaxTotalPoints/(actualPoints*panels))
	return window, allowedNodes, allowedNodes < nodes
}
