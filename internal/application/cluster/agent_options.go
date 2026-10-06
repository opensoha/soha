package cluster

import (
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"slices"

	domaincluster "github.com/opensoha/soha/internal/domain/cluster"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

var (
	customGroupPattern     = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]*[a-z0-9])?(\.[a-z0-9]([a-z0-9-]*[a-z0-9])?)*$`)
	customPluralPattern    = regexp.MustCompile(`^[a-z]([a-z0-9-]*[a-z0-9])?$`)
	customNamespacePattern = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]*[a-z0-9])?$`)
)

func prometheusTransport(metadata map[string]any) string {
	if value := metadataString(metadata, "prometheus_transport"); value != "" {
		return value
	}
	return "direct"
}

func customResourceRules(metadata map[string]any) []domaincluster.AgentCustomResourceRule {
	var rules []domaincluster.AgentCustomResourceRule
	data, err := json.Marshal(metadata["agent_custom_resource_rules"])
	if err == nil {
		_ = json.Unmarshal(data, &rules)
	}
	return rules
}

func validateAgentOptions(input *domaincluster.RegisterInput, mode domaincluster.ConnectionMode) error {
	if input.PrometheusTransport == "" {
		input.PrometheusTransport = "direct"
	}
	if input.PrometheusTransport != "direct" && input.PrometheusTransport != "agent" {
		return fmt.Errorf("%w: invalid Prometheus transport", apperrors.ErrInvalidArgument)
	}
	if input.PrometheusTransport == "agent" && mode != domaincluster.ConnectionModeAgent {
		return fmt.Errorf("%w: Prometheus Agent transport requires an Agent connection", apperrors.ErrInvalidArgument)
	}
	if input.PrometheusBaseURL != "" {
		u, err := url.Parse(input.PrometheusBaseURL)
		if err != nil || u.Hostname() == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
			return fmt.Errorf("%w: Prometheus URL must be an HTTP(S) endpoint without credentials, query or fragment", apperrors.ErrInvalidArgument)
		}
	}
	if len(input.AgentCustomResourceRules) > 50 {
		return fmt.Errorf("%w: too many custom resource rules", apperrors.ErrInvalidArgument)
	}
	for _, rule := range input.AgentCustomResourceRules {
		if err := validateCustomResourceRule(rule); err != nil {
			return err
		}
	}

	return nil
}

func validateCustomResourceRule(rule domaincluster.AgentCustomResourceRule) error {
	if len(rule.APIGroup) > 253 || !customGroupPattern.MatchString(rule.APIGroup) || len(rule.Resources) == 0 || len(rule.Resources) > 50 || len(rule.Verbs) == 0 || len(rule.Verbs) > 7 || len(rule.Namespaces) > 50 {
		return fmt.Errorf("%w: custom resource rules require an explicit API group, resources and verbs", apperrors.ErrInvalidArgument)
	}
	for _, resource := range rule.Resources {
		if len(resource) > 63 || !customPluralPattern.MatchString(resource) {
			return fmt.Errorf("%w: invalid custom resource plural", apperrors.ErrInvalidArgument)
		}
	}
	for _, verb := range rule.Verbs {
		if !slices.Contains([]string{"get", "list", "watch", "create", "update", "patch", "delete"}, verb) {
			return fmt.Errorf("%w: invalid custom resource verb", apperrors.ErrInvalidArgument)
		}
	}
	for _, namespace := range rule.Namespaces {
		if len(namespace) > 63 || !customNamespacePattern.MatchString(namespace) {
			return fmt.Errorf("%w: invalid custom resource namespace", apperrors.ErrInvalidArgument)
		}
	}
	return nil
}
