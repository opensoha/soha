package resource

import (
	domainsettings "github.com/opensoha/soha/internal/domain/settings"
	"strings"
)

// The metric alias never replaces the real cluster ID used for authorization or transport.
func prometheusClusterValue(settings domainsettings.PrometheusSettings, clusterID string) string {
	if value := strings.TrimSpace(settings.ClusterLabelValue); value != "" {
		return value
	}
	return clusterID
}
