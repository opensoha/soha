package cluster

import (
	"context"
	"slices"
	"time"

	contractresource "github.com/opensoha/soha-contracts/resource"
	domaincluster "github.com/opensoha/soha/internal/domain/cluster"
)

type agentActionCheckReader interface {
	GetActionChecks(context.Context) ([]contractresource.AgentActionCheck, error)
}

func (s *Service) describeAgentActions(ctx context.Context, connection domaincluster.Connection, detail *domaincluster.Detail) {
	checks := unknownAgentActionChecks()
	if s.agents != nil {
		client, err := s.agents(connection)
		if reader, ok := client.(agentActionCheckReader); err == nil && ok {
			queryCtx, cancel := context.WithTimeout(ctx, 4*time.Second)
			defer cancel()
			if reported, err := reader.GetActionChecks(queryCtx); err == nil && len(reported) <= 64 {
				checks = normalizeAgentActionChecks(reported)
			}
		}
	}
	detail.Diagnostics.ActionChecks = checks
	for index := range detail.CapabilityMatrix {
		entry := &detail.CapabilityMatrix[index]
		actions := agentCapabilityActions(entry.Key)
		if len(actions) == 0 {
			continue
		}
		for _, check := range checks {
			if slices.Contains(actions, check.Action) && (check.Implementation != "supported" || check.Configured != "allowed" || (check.KubernetesRBAC != "allowed" && check.KubernetesRBAC != "not_applicable")) {
				entry.Agent = domaincluster.CapabilityModeSupport{Status: domaincluster.CapabilityStatusPartial, Reason: "Connected Agent action policy or Kubernetes access is denied or not fully verified; inspect action checks and target permissions."}
				break
			}
		}
	}
}

func unknownAgentActionChecks() []contractresource.AgentActionCheck {
	checks := make([]contractresource.AgentActionCheck, 0, 24)
	for _, action := range contractresource.KubernetesAgentActions() {
		checks = append(checks, contractresource.AgentActionCheck{Action: action, Implementation: "unknown", Configured: "unknown", KubernetesRBAC: "unknown", ReasonCode: "diagnostics_unavailable", Scope: "cluster"})
	}
	return checks
}

func normalizeAgentActionChecks(reported []contractresource.AgentActionCheck) []contractresource.AgentActionCheck {
	checks := unknownAgentActionChecks()
	for index, check := range checks {
		for _, candidate := range reported {
			if candidate.Action != check.Action || !validAgentActionCheck(candidate) {
				continue
			}
			candidate.Scope = "cluster"
			if !slices.Contains([]string{"target_required", "self_subject_access_review", "runtime_unavailable", "agent_session_only"}, candidate.ReasonCode) {
				candidate.ReasonCode = "agent_reported"
			}
			checks[index] = candidate
			break
		}
	}
	return checks
}

func validAgentActionCheck(check contractresource.AgentActionCheck) bool {
	checkedAt, err := time.Parse(time.RFC3339, check.CheckedAt)
	if err != nil || time.Since(checkedAt) > 2*time.Minute || time.Until(checkedAt) > 10*time.Second {
		return false
	}
	return slices.Contains([]string{"supported", "unsupported", "unknown"}, check.Implementation) && slices.Contains([]string{"allowed", "denied", "unknown"}, check.Configured) && slices.Contains([]string{"allowed", "denied", "unknown", "not_applicable"}, check.KubernetesRBAC)
}

func agentCapabilityActions(key string) []string {
	switch key {
	case "namespace.lifecycle", "resource.yaml.apply", "configuration.inventory":
		return []string{"platform.resources.create", "platform.resources.apply", "platform.resources.delete"}
	case "workload.mutations":
		return []string{"platform.deployments.restart", "platform.deployments.scale", "platform.deployments.image", "platform.deployments.rollback", "platform.statefulsets.restart", "platform.statefulsets.scale", "platform.daemonsets.restart", "platform.resources.apply", "platform.resources.delete"}
	case "resource.creation":
		return []string{"platform.resources.create"}
	case "custom.resources":
		return []string{"platform.custom_resources.list", "platform.custom_resources.create", "platform.custom_resources.apply", "platform.custom_resources.delete"}
	case "helm.releases":
		return []string{"platform.helm_releases.install", "platform.helm_releases.values_update", "platform.helm_releases.rollback", "platform.helm_releases.delete"}
	case "port.forward":
		return []string{"platform.port_forwards.create", "platform.port_forwards.tunnel", "platform.port_forwards.delete"}
	case "pod.exec":
		return []string{"platform.pods.exec"}
	default:
		return nil
	}
}
