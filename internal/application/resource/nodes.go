package resource

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	domainaccess "github.com/opensoha/soha/internal/domain/access"
	domaincluster "github.com/opensoha/soha/internal/domain/cluster"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainresource "github.com/opensoha/soha/internal/domain/resource"
	domainsettings "github.com/opensoha/soha/internal/domain/settings"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

const nodePodUsageTimeout = 1200 * time.Millisecond

type resourceTotals struct {
	cpuMilli       int64
	memoryBytes    int64
	ephemeralBytes int64
	pods           int64
}

func (i *Inventory) GetNodeDetail(ctx context.Context, principal domainidentity.Principal, clusterID, nodeName string) (domainresource.NodeDetailView, error) {
	connection, decision, err := i.authorize(ctx, principal, clusterID, "", "Node", domainaccess.ActionView)
	if err != nil {
		return domainresource.NodeDetailView{}, err
	}
	var item domainresource.NodeDetailView
	source := "agent"
	if connection.Summary.ConnectionMode == domaincluster.ConnectionModeAgent {
		client, err := i.inventoryAgentClient(connection)
		if err != nil {
			return domainresource.NodeDetailView{}, err
		}
		item, err = client.GetNodeDetail(ctx, nodeName)
		if err != nil {
			return domainresource.NodeDetailView{}, fmt.Errorf("%w: %v", apperrors.ErrClusterUnready, err)
		}
	} else {
		direct, err := i.directInventory()
		if err != nil {
			return domainresource.NodeDetailView{}, err
		}
		item, err = direct.GetNodeDetail(ctx, clusterID, nodeName)
		if err != nil {
			return domainresource.NodeDetailView{}, err
		}
		source = "direct"
	}
	i.applyNodeUsageMetrics(ctx, clusterID, &item)
	item.AllowedActions = stringifyActions(decision.AllowedActions)
	_ = i.recordAudit(ctx, principal, connection.Summary.ID, "", "Node", nodeName, string(domainaccess.ActionView), "success", fmt.Sprintf("read node detail via %s", source))
	return item, nil
}

func (i *Inventory) applyNodeUsageMetrics(ctx context.Context, clusterID string, item *domainresource.NodeDetailView) {
	if item == nil {
		return
	}
	settings := i.resolveClusterPrometheusSettings(ctx, clusterID)
	switch {
	case !settings.Enabled || strings.TrimSpace(settings.BaseURL) == "":
		item.MetricsMessage = "prometheus is not configured"
		return
	}
	identities := make([]podIdentity, 0, len(item.Pods))
	for _, pod := range item.Pods {
		identities = append(identities, podIdentity{Name: pod.Name, Namespace: pod.Namespace})
	}
	values, err := i.listNodePodUsageValues(ctx, settings, clusterID, identities)
	item.MetricsConfigured = true
	if err != nil {
		item.MetricsMessage = err.Error()
		return
	}
	usage := resourceTotals{pods: int64(item.PodCount)}
	for index := range item.Pods {
		value, ok := values[podMetricsKey(item.Pods[index].Namespace, item.Pods[index].Name)]
		if !ok {
			continue
		}
		if value.CPUCores > 0 {
			item.Pods[index].CPU = formatCPUUsage(value.CPUCores)
			usage.cpuMilli += int64(math.Round(value.CPUCores * 1000))
		}
		if value.MemoryBytes > 0 {
			item.Pods[index].Memory = formatMemoryBytes(value.MemoryBytes)
			usage.memoryBytes += int64(math.Round(value.MemoryBytes))
		}
	}
	item.Resources.Usage = formatUsageTotals(usage)
	item.Resources.UsagePercentages = calculateResourcePercentages(usage, resourceTotalsFromQuantityView(item.Resources.Allocatable))
	if len(values) == 0 && len(item.Pods) > 0 {
		item.MetricsMessage = "prometheus returned no pod metrics for this node"
	}
}

func (i *Inventory) listNodePodUsageValues(ctx context.Context, settings domainsettings.PrometheusSettings, clusterID string, pods []podIdentity) (map[string]podUsageValue, error) {
	metricsCtx, cancel := context.WithTimeout(ctx, nodePodUsageTimeout)
	defer cancel()
	values, err := i.listPodUsageValues(metricsCtx, settings, clusterID, pods)
	if err == nil {
		return values, nil
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(metricsCtx.Err(), context.DeadlineExceeded) {
		return nil, fmt.Errorf("timed out while querying prometheus pod metrics")
	}
	return nil, err
}

func resourceTotalsFromQuantityView(view domainresource.ResourceQuantityView) resourceTotals {
	return resourceTotals{
		cpuMilli: parseCPUMilli(view.CPU), memoryBytes: parseBinaryQuantity(view.Memory),
		ephemeralBytes: parseBinaryQuantity(view.EphemeralStorage), pods: parseIntegerQuantity(view.Pods),
	}
}

func parseCPUMilli(value string) int64 {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0
	}
	if strings.HasSuffix(value, "m") {
		parsed, _ := strconv.ParseFloat(strings.TrimSuffix(value, "m"), 64)
		return int64(math.Round(parsed))
	}
	parsed, _ := strconv.ParseFloat(value, 64)
	return int64(math.Round(parsed * 1000))
}

func parseBinaryQuantity(value string) int64 {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0
	}
	units := []struct {
		suffix     string
		multiplier float64
	}{{"Ei", 1 << 60}, {"Pi", 1 << 50}, {"Ti", 1 << 40}, {"Gi", 1 << 30}, {"Mi", 1 << 20}, {"Ki", 1 << 10}, {"E", 1e18}, {"P", 1e15}, {"T", 1e12}, {"G", 1e9}, {"M", 1e6}, {"k", 1e3}}
	for _, unit := range units {
		if strings.HasSuffix(value, unit.suffix) {
			parsed, _ := strconv.ParseFloat(strings.TrimSuffix(value, unit.suffix), 64)
			return int64(math.Round(parsed * unit.multiplier))
		}
	}
	parsed, _ := strconv.ParseFloat(value, 64)
	return int64(math.Round(parsed))
}
func parseIntegerQuantity(value string) int64 {
	parsed, _ := strconv.ParseInt(strings.TrimSpace(value), 10, 64)
	return parsed
}

func formatUsageTotals(t resourceTotals) domainresource.ResourceQuantityView {
	view := domainresource.ResourceQuantityView{}
	if t.cpuMilli > 0 {
		view.CPU = formatCPUUsage(float64(t.cpuMilli) / 1000)
	}
	if t.memoryBytes > 0 {
		view.Memory = formatMemoryBytes(float64(t.memoryBytes))
	}
	if t.ephemeralBytes > 0 {
		view.EphemeralStorage = formatMemoryBytes(float64(t.ephemeralBytes))
	}
	if t.pods > 0 {
		view.Pods = strconv.FormatInt(t.pods, 10)
	}
	return view
}
func calculateResourcePercentages(current, total resourceTotals) domainresource.ResourcePercentageView {
	return domainresource.ResourcePercentageView{CPU: resourcePercentage(current.cpuMilli, total.cpuMilli), Memory: resourcePercentage(current.memoryBytes, total.memoryBytes), EphemeralStorage: resourcePercentage(current.ephemeralBytes, total.ephemeralBytes), Pods: resourcePercentage(current.pods, total.pods)}
}
func resourcePercentage(current, total int64) float64 {
	if current <= 0 || total <= 0 {
		return 0
	}
	return math.Round((float64(current)/float64(total))*1000) / 10
}
