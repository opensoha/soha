package cluster

func DefaultCapabilityMatrix() []CapabilityMatrixEntry {
	return []CapabilityMatrixEntry{
		capability("cluster.inventory", "Cluster inventory", "platform", scopes("cluster"), CapabilityRiskRead, false, "/architecture/multi-cluster-model", available(), available()),
		capability("namespace.lifecycle", "Namespace lifecycle", "platform", scopes("cluster", "namespace"), CapabilityRiskMutate, true, "/architecture/access-model", available(), partial("list namespaces through the agent; namespace create, update, and delete still require direct mode")),
		capability("workload.read", "Workload read model", "workloads", scopes("cluster", "namespace"), CapabilityRiskRead, false, "/architecture/multi-cluster-model", available(), available()),
		capability("workload.mutations", "Workload mutations", "workloads", scopes("cluster", "namespace"), CapabilityRiskMutate, true, "/architecture/authorization", available(), partial("deployment restart, rollback, scale, statefulset restart/scale, and daemonset restart are available through the agent; pod deletion and YAML apply remain direct-only")),
		capability("resource.yaml.view", "YAML view", "configuration", scopes("cluster", "namespace"), CapabilityRiskRead, false, "/operations/agent-runtime", available(), partial("built-in generic and custom-resource YAML can be read through the agent; direct-specific YAML endpoints still need parity cleanup")),
		capability("resource.yaml.apply", "YAML apply and delete", "configuration", scopes("cluster", "namespace"), CapabilityRiskMutate, true, "/operations/agent-runtime", available(), partial("built-in and custom-resource YAML apply/delete are available through the agent; direct-specific endpoints still need parity cleanup")),
		capability("resource.creation", "Resource creation", "configuration", scopes("cluster", "namespace"), CapabilityRiskMutate, true, "/operations/agent-runtime", available(), available("preflight and create are available when the connected agent publishes resource.creation")),
		capability("configuration.inventory", "Configuration inventory", "configuration", scopes("cluster", "namespace"), CapabilityRiskRead, false, "/architecture/multi-cluster-model", available(), partial("ConfigMap and Secret lists are available through the agent; detail views still need parity cleanup")),
		capability("rbac.inventory", "RBAC inventory", "access", scopes("cluster", "namespace"), CapabilityRiskRead, false, "/architecture/access-model", available(), partial("RBAC lists and several detail views are available through the agent; creation uses the separately gated resource.creation capability")),
		capability("network.inventory", "Network inventory", "network", scopes("cluster", "namespace"), CapabilityRiskRead, false, "/architecture/multi-cluster-model", available(), available()),
		capability("port.forward", "Port forwarding", "network", scopes("cluster", "namespace", "pod"), CapabilityRiskExecute, true, "/operations/agent-runtime", available(), available("live port-forward tunnels are available through the agent")),
		capability("storage.inventory", "Storage inventory", "storage", scopes("cluster", "namespace"), CapabilityRiskRead, false, "/architecture/multi-cluster-model", available(), partial("storage lists are available through the agent; PVC, PV, and StorageClass detail views remain direct-only while creation uses resource.creation")),
		capability("custom.resources", "Custom resources", "extensions", scopes("cluster", "namespace"), CapabilityRiskMutate, true, "/development/add-resource-module", available(), available("CRD discovery and custom-resource list, YAML, create, apply, and delete are available through the agent")),
		capability("helm.releases", "Helm releases", "helm", scopes("cluster", "namespace"), CapabilityRiskMutate, true, "/operations/agent-runtime", available(), available("release list, detail, history, values read, install, values update, and delete are available through the agent")),
		capability("delivery.actions", "Delivery actions", "delivery", scopes("application", "environment", "cluster", "namespace"), CapabilityRiskExecute, true, "/architecture/application-delivery", available(), partial("build actions remain available; deploy, build-deploy, verification, and rollback against agent-connected targets require delivery runner parity")),
		capability("pod.logs", "Pod logs", "observability", scopes("cluster", "namespace", "pod"), CapabilityRiskRead, false, "/operations/agent-runtime", available(), available("snapshot and streaming pod logs are available through the agent")),
		capability("pod.exec", "Pod exec and terminal", "workloads", scopes("cluster", "namespace", "pod"), CapabilityRiskExecute, true, "/operations/agent-runtime", available(), available("non-interactive exec and interactive terminal sessions are available through the agent")),
		capability("metrics", "Metrics", "observability", scopes("cluster", "namespace"), CapabilityRiskRead, false, "/architecture/monitoring-and-alerting", available(), available()),
	}
}

func capability(key, label, category string, requiredScopes []string, riskLevel CapabilityRiskLevel, requiresApproval bool, docsURL string, direct, agent CapabilityModeSupport) CapabilityMatrixEntry {
	return CapabilityMatrixEntry{
		Key:              key,
		Label:            label,
		Category:         category,
		RequiredScopes:   requiredScopes,
		RiskLevel:        riskLevel,
		RequiresApproval: requiresApproval,
		DocsURL:          docsURL,
		Direct:           direct,
		Agent:            agent,
	}
}

func available(notes ...string) CapabilityModeSupport {
	return capabilityModeSupport(CapabilityStatusAvailable, notes...)
}

func partial(notes ...string) CapabilityModeSupport {
	return capabilityModeSupport(CapabilityStatusPartial, notes...)
}

func capabilityModeSupport(status CapabilityStatus, notes ...string) CapabilityModeSupport {
	cleaned := cleanNotes(notes)
	reason := ""
	if len(cleaned) > 0 {
		reason = cleaned[0]
	}
	return CapabilityModeSupport{Status: status, Reason: reason, Notes: cleaned}
}

func scopes(items ...string) []string {
	return cleanNotes(items)
}

func cleanNotes(notes []string) []string {
	out := make([]string, 0, len(notes))
	for _, note := range notes {
		if note != "" {
			out = append(out, note)
		}
	}
	return out
}
