package resource

import (
	"context"
	"fmt"
	"regexp"
	"slices"
	"strings"

	contractresource "github.com/opensoha/soha/contracts/resource"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainresource "github.com/opensoha/soha/internal/domain/resource"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

var metricsContainerName = regexp.MustCompile(`^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`)

func validPodMetricSelection(uid, container string) bool {
	if uid == "" {
		return container == ""
	}
	return contractresource.ValidWorkloadUID(uid) &&
		(container == "" || (len(container) <= 63 && metricsContainerName.MatchString(container)))
}

func (w *Workloads) checkPodMetricIdentity(ctx context.Context, principal domainidentity.Principal, clusterID, namespace, podName, uid, container string) error {
	pod, err := w.GetPodDetail(ctx, principal, clusterID, namespace, podName)
	if err != nil {
		return err
	}
	if pod.UID != uid || pod.Namespace != namespace || pod.Name != podName {
		return fmt.Errorf("%w: Pod identity changed; refresh the service before querying metrics", apperrors.ErrConflict)
	}
	if container != "" && !slices.ContainsFunc(pod.Containers, func(item domainresource.WorkloadContainerView) bool {
		return item.Name == container
	}) {
		return fmt.Errorf("%w: container no longer belongs to the Pod", apperrors.ErrNotFound)
	}
	return nil
}

func buildIdentityPodMetrics(namespace, name, uid, container, clusterValue, clusterLabel string) []metricDefinition {
	matchers := []string{
		fmt.Sprintf(`namespace=%q`, namespace),
		fmt.Sprintf(`pod=%q`, name),
		`container!=""`,
		`container!="POD"`,
	}
	if container != "" {
		matchers = append(matchers, fmt.Sprintf(`container=%q`, container))
	}
	if clusterLabel != "" && clusterValue != "" {
		matchers = append(matchers, fmt.Sprintf(`%s=%q`, clusterLabel, clusterValue))
	}
	base := strings.Join(matchers, ",")
	// Both kubelet cgroup drivers put the Pod UID in its parent cgroup.
	// A complete component match prevents a different UID sharing a prefix
	// from entering a rate window. Sources may also expose a trusted uid label.
	cgroup := `(/[^/]+)*/(pod` + regexp.QuoteMeta(uid) + `|kubepods[^/]*-pod` +
		regexp.QuoteMeta(strings.ReplaceAll(uid, "-", "_")) + `\.slice)(/[^/]+)*`
	cgroupFilter := base + fmt.Sprintf(`,id=~%q,uid=""`, cgroup)
	uidFilter := base + fmt.Sprintf(`,uid=%q`, uid)
	return []metricDefinition{
		{Key: "cpu", Label: "CPU Usage", Unit: "cores", Query: fmt.Sprintf(
			`sum(rate(container_cpu_usage_seconds_total{%s}[5m])) or sum(rate(container_cpu_usage_seconds_total{%s}[5m]))`,
			uidFilter, cgroupFilter,
		)},
		{Key: "memory", Label: "Memory Working Set", Unit: "bytes", Query: fmt.Sprintf(
			`sum(container_memory_working_set_bytes{%s}) or sum(container_memory_working_set_bytes{%s})`,
			uidFilter, cgroupFilter,
		)},
	}
}
