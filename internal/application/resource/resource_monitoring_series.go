package resource

import (
	"math"
	"net"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	cr "github.com/opensoha/soha-contracts/resource"
)

var monitoringUIDPattern = regexp.MustCompile(`[a-fA-F0-9]{8}[-_][a-fA-F0-9]{4}[-_][a-fA-F0-9]{4}[-_][a-fA-F0-9]{4}[-_][a-fA-F0-9]{12}`)

func monitoringNodeAliases(nodes []cr.ResourceMonitoringNode, identity []monitoringRawSeries) map[string]string {
	aliases := map[string]string{}
	add := func(alias, name string) {
		if alias == "" {
			return
		}
		if previous, ok := aliases[alias]; ok && previous != name {
			aliases[alias] = ""
		} else if !ok {
			aliases[alias] = name
		}
	}
	known := map[string]bool{}
	for _, node := range nodes {
		known[node.NodeRef.Name] = true
		add(node.NodeRef.Name, node.NodeRef.Name)
		add(node.InternalIP, node.NodeRef.Name)
	}
	for _, series := range identity {
		name := series.Metric["nodename"]
		if !known[name] {
			continue
		}
		add(series.Metric["instance"], name)
	}
	return aliases
}
func monitoringSeriesNode(labels map[string]string, aliases map[string]string) string {
	explicit := ""
	for _, key := range []string{"node", "nodename"} {
		if value := labels[key]; value != "" {
			name := aliases[value]
			if name == "" || (explicit != "" && explicit != name) {
				return ""
			}
			explicit = name
		}
	}
	instance := labels["instance"]
	mapped := aliases[instance]
	if mapped == "" {
		host, _, err := net.SplitHostPort(instance)
		if err == nil {
			mapped = aliases[host]
		} else {
			mapped = aliases[strings.Trim(instance, "[]")]
		}
	}
	if explicit != "" {
		if mapped != "" && mapped != explicit {
			return ""
		}
		return explicit
	}
	return mapped
}
func monitoringDimension(definition monitoringDefinition, labels map[string]string) (string, bool) {
	switch definition.dimension {
	case "cpu":
		value := labels["cpu"]
		return value, value != ""
	case "container":
		uid := labels["uid"]
		if uid == "" {
			uid = strings.ReplaceAll(monitoringUIDPattern.FindString(labels["id"]), "_", "-")
		}
		container := labels["container"]
		if uid == "" || container == "" {
			return "", false
		}
		return uid + "/" + container + "/" + labels["cpu"], true
	default:
		return "node", true
	}
}

type monitoringCell struct {
	value    float64
	set      bool
	identity string
	conflict bool
}
type monitoringNodePoints struct {
	dimensions map[string]map[int64]monitoringCell
	partial    bool
	reason     string
}

func buildMonitoringPanel(definition monitoringDefinition, result monitoringResult, nodes []cr.ResourceMonitoringNode, aliases map[string]string, window monitoringWindow, remaining *int) cr.ResourceMonitoringPanel {
	panel := monitoringEmptyPanel(definition, result.quality, result.reason)
	grouped := groupMonitoringSeries(definition, result, aliases, window)
	anyValues := false
	for _, node := range nodes {
		series := monitoringSeriesForNode(definition, node, grouped[node.NodeRef.Name], window, remaining)
		if grouped[node.NodeRef.Name] == nil && result.quality != "valid" && result.quality != "not_collected" {
			series.Quality = result.quality
			series.ReasonCode = result.reason
		}
		if series.Latest != nil {
			anyValues = true
		}
		if series.Quality == "partial" || series.Quality == "stale" {
			panel.Quality = "partial"
			panel.ReasonCode = series.ReasonCode
		}
		panel.Series = append(panel.Series, series)
	}
	if anyValues {
		for _, series := range panel.Series {
			if series.Latest == nil {
				panel.Quality = "partial"
				panel.ReasonCode = "node_coverage_incomplete"
			}
		}
	}
	if !anyValues && panel.Quality == "valid" {
		panel.Quality = "not_collected"
		panel.ReasonCode = "no_verified_samples"
	}
	if definition.dimension == "allocation" && anyValues {
		panel.Quality = "partial"
		panel.ReasonCode = "container_allocations_only"
	}
	return panel
}

func groupMonitoringSeries(definition monitoringDefinition, result monitoringResult, aliases map[string]string, window monitoringWindow) map[string]*monitoringNodePoints {
	grouped := map[string]*monitoringNodePoints{}
	ages := monitoringAges(result.age)
	aggregates := monitoringAggregateContainers(definition, result.series)
	// Sorting makes replica deduplication deterministic without summing exporters twice.
	sort.Slice(result.series, func(i, j int) bool {
		return monitoringLabelsKey(result.series[i].Metric) < monitoringLabelsKey(result.series[j].Metric)
	})
	for _, series := range result.series {
		node := monitoringSeriesNode(series.Metric, aliases)
		if node == "" {
			continue
		}
		points := grouped[node]
		if points == nil {
			points = &monitoringNodePoints{dimensions: map[string]map[int64]monitoringCell{}}
			grouped[node] = points
		}
		dimension, ok := monitoringDimension(definition, series.Metric)
		if !ok {
			points.partial = true
			points.reason = "container_identity_unverified"
			continue
		}
		cpu := series.Metric["cpu"]
		container := strings.TrimSuffix(dimension, "/"+cpu)
		if definition.key == "pod.cpu.used" && aggregates[container] {
			if cpu != "" && cpu != "total" {
				continue
			}
			dimension = container + "/total"
		}
		cells := points.dimensions[dimension]
		if cells == nil {
			cells = map[int64]monitoringCell{}
			points.dimensions[dimension] = cells
		}
		identity := monitoringLabelsKey(series.Metric)
		for _, raw := range series.Values {
			mergeMonitoringPoint(definition, raw, identity, ages[identity], window, cells, points)
		}
	}
	return grouped
}

func monitoringAggregateContainers(definition monitoringDefinition, series []monitoringRawSeries) map[string]bool {
	result := map[string]bool{}
	if definition.key != "pod.cpu.used" {
		return result
	}
	for _, item := range series {
		cpu := item.Metric["cpu"]
		if cpu != "" && cpu != "total" {
			continue
		}
		if dimension, ok := monitoringDimension(definition, item.Metric); ok {
			result[strings.TrimSuffix(dimension, "/"+cpu)] = true
		}
	}
	return result
}
func monitoringAges(series []monitoringRawSeries) map[string]map[int64]float64 {
	result := map[string]map[int64]float64{}
	for _, item := range series {
		points := map[int64]float64{}
		for _, raw := range item.Values {
			timestamp, value, ok := monitoringPoint(raw)
			if ok {
				points[timestamp] = value
			}
		}
		result[monitoringLabelsKey(item.Metric)] = points
	}
	return result
}
func mergeMonitoringPoint(definition monitoringDefinition, raw []any, identity string, ages map[int64]float64, window monitoringWindow, cells map[int64]monitoringCell, points *monitoringNodePoints) {
	timestamp, value, ok := monitoringPoint(raw)
	if !ok || timestamp < window.from.Unix() || timestamp > window.to.Unix() {
		return
	}
	if value < 0 {
		points.partial = true
		points.reason = "invalid_sample"
		return
	}
	if definition.ageQuery != "" {
		age, known := ages[timestamp]
		if !known {
			points.partial = true
			points.reason = "freshness_unverified"
			return
		}
		if definition.dimension == "counter" {
			if age < 5 {
				points.partial = true
				points.reason = "insufficient_counter_samples"
				return
			}
		} else if age > float64(max(120, window.step*2)) {
			points.partial = true
			points.reason = "stale_samples"
			return
		}
	}
	cell := cells[timestamp]
	if cell.set {
		if math.Abs(cell.value-value) > math.Max(1e-6, math.Abs(value)*0.01) {
			points.partial = true
			points.reason = "replica_values_differ"
			cell.conflict = true
			cells[timestamp] = cell
		}
		return
	}
	cells[timestamp] = monitoringCell{value: value, set: true, identity: identity}
}
func monitoringPoint(raw []any) (int64, float64, bool) {
	if len(raw) != 2 {
		return 0, 0, false
	}
	timestamp, ok := asUnixTimestamp(raw[0])
	if !ok {
		return 0, 0, false
	}
	value, ok := asPrometheusFloat(raw[1])
	return timestamp, value, ok && !math.IsNaN(value) && !math.IsInf(value, 0)
}
func monitoringLabelsKey(labels map[string]string) string {
	keys := make([]string, 0, len(labels))
	for key := range labels {
		if key != "__name__" {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		parts = append(parts, key+"="+strconv.Quote(labels[key]))
	}
	return strings.Join(parts, ",")
}
func monitoringSeriesForNode(definition monitoringDefinition, node cr.ResourceMonitoringNode, values *monitoringNodePoints, window monitoringWindow, remaining *int) cr.ResourceMonitoringSeries {
	series := cr.ResourceMonitoringSeries{NodeRef: node.NodeRef, MetricKey: definition.key, Unit: definition.unit, Quality: "not_collected", Source: "prometheus", Points: []cr.ResourceMonitoringPoint{}}
	if strings.HasPrefix(definition.key, "conntrack.") && node.OS != "linux" {
		series.Quality = "unsupported"
		series.ReasonCode = "linux_collector_required"
		if node.OS == "" {
			series.Quality = "partial"
			series.ReasonCode = "node_os_unverified"
		}
		return series
	}
	if values == nil {
		series.ReasonCode = "metric_not_collected"
		return series
	}
	created, _ := time.Parse(time.RFC3339, node.CreatedAt)
	validCount := 0
	for timestamp := window.from.Unix(); timestamp <= window.to.Unix(); timestamp += int64(window.step) {
		if *remaining <= 0 {
			series.Quality = "partial"
			series.ReasonCode = "point_budget"
			break
		}
		*remaining--
		point := cr.ResourceMonitoringPoint{Timestamp: time.Unix(timestamp, 0).UTC().Format(time.RFC3339)}
		value, found := sumMonitoringDimensions(values, timestamp)
		if definition.dimension == "cpu" && !completeMonitoringDimensions(values, timestamp, node.Capacity.CPUCores) {
			found = false
			values.partial = true
			values.reason = "cpu_coverage_incomplete"
		}
		if definition.dimension == "container" && !monitoringDimensionsPresent(values, timestamp) {
			values.partial = true
			values.reason = "container_coverage_incomplete"
		}
		if found && (created.IsZero() || timestamp >= created.Unix()) {
			point.Value = &value
			series.Latest = &value
			series.SampledAt = point.Timestamp
			validCount++
		}
		series.Points = append(series.Points, point)
	}
	finishMonitoringSeries(&series, values, validCount, window, definition)
	return series
}

func finishMonitoringSeries(series *cr.ResourceMonitoringSeries, values *monitoringNodePoints, validCount int, window monitoringWindow, definition monitoringDefinition) {
	if validCount > 0 && series.Quality != "partial" {
		series.Quality = "valid"
	}
	if values.partial {
		series.Quality = "partial"
		series.ReasonCode = values.reason
	}
	if validCount < len(series.Points) && validCount > 0 && series.Quality == "valid" {
		series.Quality = "partial"
		series.ReasonCode = "sample_gaps"
	}
	// shortcut: Without a time-indexed container census, wholly missing containers cannot be proven absent; add one before claiming full coverage.
	if definition.dimension == "container" && validCount > 0 && series.Quality == "valid" {
		series.Quality = "partial"
		series.ReasonCode = "container_coverage_unverified"
	}
	if window.live && series.SampledAt != "" {
		sampled, _ := time.Parse(time.RFC3339, series.SampledAt)
		if window.to.Sub(sampled) > time.Duration(max(120, window.step*2))*time.Second {
			series.Quality = "stale"
			series.ReasonCode = "stale_samples"
		}
	}
}
func sumMonitoringDimensions(points *monitoringNodePoints, timestamp int64) (float64, bool) {
	total := 0.0
	found := false
	for _, dimension := range points.dimensions {
		if cell, ok := dimension[timestamp]; ok && cell.set {
			total += cell.value
			found = true
		}
	}
	return total, found
}

func completeMonitoringDimensions(points *monitoringNodePoints, timestamp int64, cpuCores *float64) bool {
	if cpuCores == nil || *cpuCores <= 0 || float64(len(points.dimensions)) != *cpuCores {
		return false
	}
	return monitoringDimensionsPresent(points, timestamp)
}

func monitoringDimensionsPresent(points *monitoringNodePoints, timestamp int64) bool {
	for _, dimension := range points.dimensions {
		if cell, ok := dimension[timestamp]; !ok || !cell.set {
			return false
		}
	}
	return len(points.dimensions) > 0
}
