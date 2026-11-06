package cluster

import (
	"context"
	"fmt"
	"strings"
	"unicode/utf8"

	appaccess "github.com/opensoha/soha/internal/application/access"
	domainaccess "github.com/opensoha/soha/internal/domain/access"
	domaincluster "github.com/opensoha/soha/internal/domain/cluster"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/appconfig"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

func normalizeClusterMetricIdentity(input *domaincluster.RegisterInput) error {
	input.PrometheusClusterLabel = strings.TrimSpace(input.PrometheusClusterLabel)
	input.PrometheusClusterLabelValue = strings.TrimSpace(input.PrometheusClusterLabelValue)
	if utf8.RuneCountInString(input.PrometheusClusterLabelValue) > 1024 {
		return fmt.Errorf("%w: Prometheus cluster label value exceeds 1024 characters", apperrors.ErrInvalidArgument)
	}
	if !domaincluster.ValidPrometheusMetricIdentity(input.PrometheusClusterLabel, input.PrometheusClusterLabelValue) {
		return fmt.Errorf("%w: configure a valid Prometheus cluster label that is independent of metric dimensions", apperrors.ErrInvalidArgument)
	}
	return nil
}

func updatedClusterMetricIdentity(metadata map[string]any, input domaincluster.UpdateInput) (label, value string) {
	label = metadataString(metadata, "prometheus_cluster_label")
	value = metadataString(metadata, "prometheus_cluster_label_value")
	if input.PrometheusClusterLabel != nil {
		label = strings.TrimSpace(*input.PrometheusClusterLabel)
		if label == "" {
			value = ""
		}
	}
	if input.PrometheusClusterLabelValue != nil {
		value = *input.PrometheusClusterLabelValue
	}
	return label, value
}

// A monitoring-only edit must preserve file-backed versus inline credential semantics.
func preserveDirectUpdateCredential(existing domaincluster.Connection, connection *domaincluster.Connection, cfg *appconfig.Cluster) error {
	previous, err := runtimeClusterConfig(existing)
	if err != nil {
		return err
	}
	cfg.Kubeconfig, cfg.KubeconfigData = previous.Kubeconfig, previous.KubeconfigData
	connection.SourceType, connection.SourceRef = existing.SourceType, existing.SourceRef
	for _, key := range []string{"kubeconfig", "kubeconfig_data"} {
		if value, ok := existing.Metadata[key]; ok {
			connection.Metadata[key] = value
		} else {
			delete(connection.Metadata, key)
		}
	}
	return nil
}

func (s *Service) authorizeMetricSourceChange(ctx context.Context, principal domainidentity.Principal, previous, next domaincluster.Connection) error {
	if !prometheusSourceChanged(previous, next) {
		return nil
	}
	if s.authorizer == nil {
		return apperrors.ErrAccessDenied
	}
	permission := appaccess.PlatformActionPermission("inventory", "Cluster", string(domainaccess.ActionUpdate))
	if err := s.authorizeClusterRequest(ctx, principal, domaincluster.Summary{}, domainaccess.ActionUpdate, permission, true); err != nil {
		return err
	}
	if metadataString(next.Metadata, "prometheus_url") == "" {
		return nil
	}
	connections, err := s.repo.ListConnections(ctx)
	if err != nil {
		return fmt.Errorf("check Prometheus cluster identity: %w", err)
	}
	for _, connection := range connections {
		if domaincluster.PrometheusSourceConflict(next, connection) {
			return fmt.Errorf("%w: clusters sharing a Prometheus source require the same cluster label and distinct values", apperrors.ErrConflict)
		}
	}
	return nil
}

func prometheusSourceChanged(previous, next domaincluster.Connection) bool {
	if metadataString(previous.Metadata, "prometheus_url") == "" && metadataString(next.Metadata, "prometheus_url") == "" {
		return false
	}
	for _, key := range []string{"prometheus_url", "prometheus_cluster_label", "prometheus_cluster_label_value", "prometheus_bearer_token"} {
		if metadataString(previous.Metadata, key) != metadataString(next.Metadata, key) {
			return true
		}
	}
	if prometheusTransport(previous.Metadata) != prometheusTransport(next.Metadata) {
		return true
	}
	return prometheusTransport(next.Metadata) == "agent" &&
		(previous.Summary.ConnectionMode != next.Summary.ConnectionMode ||
			metadataString(previous.Metadata, "endpoint") != metadataString(next.Metadata, "endpoint") ||
			metadataString(previous.Metadata, "token") != metadataString(next.Metadata, "token"))
}

func hasClusterRequestScope(scope *domainaccess.ResourceScope) bool {
	return scope != nil && (len(scope.Clusters) > 0 || len(scope.Namespaces) > 0 ||
		len(scope.ExcludedNamespaces) > 0 || len(scope.NamespaceSelectors) > 0 ||
		len(scope.ExcludedNamespaceSelectors) > 0 || len(scope.ResourceGroups) > 0 ||
		len(scope.ResourceKinds) > 0 || scope.LabelSelector != "")
}
