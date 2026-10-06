package cluster

func DefaultCapabilityMatrix() []CapabilityMatrixEntry {
	return []CapabilityMatrixEntry{
		capability("cluster.inventory", "Cluster inventory", "platform", scopes("cluster"), CapabilityRiskRead, false, "/architecture/multi-cluster-model", available(), available()),
		capability("namespace.lifecycle", "Namespace lifecycle", "platform", scopes("cluster", "namespace"), CapabilityRiskMutate, true, "/architecture/access-model", available(), available("namespace list, create, metadata update, and delete are available through an upgraded Agent")),
		capability("workload.read", "Workload read model", "workloads", scopes("cluster", "namespace"), CapabilityRiskRead, false, "/architecture/multi-cluster-model", available(), available()),
		capability("workload.mutations", "Workload mutations", "workloads", scopes("cluster", "namespace"), CapabilityRiskMutate, true, "/architecture/authorization", available(), available("workload shortcuts, Pod delete, and CronJob suspend/resume are available through an upgraded Agent")),
		capability("resource.yaml.view", "YAML view", "configuration", scopes("cluster", "namespace"), CapabilityRiskRead, false, "/operations/agent-runtime", available(), available("built-in and custom-resource YAML, including Node YAML, are available subject to resource permissions")),
		capability("resource.yaml.apply", "YAML apply and delete", "configuration", scopes("cluster", "namespace"), CapabilityRiskMutate, true, "/operations/agent-runtime", available(), available("ownership-protected built-in YAML apply/delete are available with Agent action allowlists and Kubernetes RBAC")),
		capability("resource.creation", "Resource creation", "configuration", scopes("cluster", "namespace"), CapabilityRiskMutate, true, "/operations/agent-runtime", available(), available("preflight and create are available when the connected agent publishes resource.creation")),
		capability("configuration.inventory", "Configuration inventory", "configuration", scopes("cluster", "namespace"), CapabilityRiskRead, false, "/architecture/multi-cluster-model", available(), available("ConfigMap and Secret metadata, detail, data editing, and references are available through an upgraded Agent; Secret values require secret-data.view")),
		capability("rbac.inventory", "RBAC inventory", "access", scopes("cluster", "namespace"), CapabilityRiskRead, false, "/architecture/access-model", available(), available("RBAC list/detail and SubjectAccessReview are available; creation is gated by resource.creation")),
		capability("network.inventory", "Network inventory", "network", scopes("cluster", "namespace"), CapabilityRiskRead, false, "/architecture/multi-cluster-model", available(), available()),
		capability("port.forward", "Port forwarding", "network", scopes("cluster", "namespace", "pod"), CapabilityRiskExecute, true, "/operations/agent-runtime", available(), available("live port-forward tunnels are available through the agent")),
		capability("storage.inventory", "Storage inventory", "storage", scopes("cluster", "namespace"), CapabilityRiskRead, false, "/architecture/multi-cluster-model", available(), available("PVC, PV, and StorageClass list/detail are available; creation is gated by resource.creation")),
		capability("custom.resources", "Custom resources", "extensions", scopes("cluster", "namespace"), CapabilityRiskMutate, true, "/development/add-resource-module", available(), partial("CRD discovery is available; custom-resource read and mutation require explicit Kubernetes RBAC for each target API group and resource")),
		capability("helm.releases", "Helm releases", "helm", scopes("cluster", "namespace"), CapabilityRiskMutate, true, "/operations/agent-runtime", available(), available("release list, detail, history, values read, install, values update, and delete are available through the agent")),
		capability("delivery.actions", "Delivery actions", "delivery", scopes("application", "environment", "cluster", "namespace"), CapabilityRiskExecute, true, "/architecture/application-delivery", available(), partial("Manifest SSA and Helm SDK targets use the cluster-bound Agent runner; generic Kubernetes Job executors still require a direct runtime")),
		capability("pod.logs", "Pod logs", "observability", scopes("cluster", "namespace", "pod"), CapabilityRiskRead, false, "/operations/agent-runtime", available(), available("snapshot and streaming pod logs are available through the agent")),
		capability("logs.runtime.snapshot", "Runtime log snapshots", "observability", scopes("cluster", "namespace", "workload", "pod"), CapabilityRiskRead, false, "/operations/log-observability", available(), available("bounded runtime log snapshots are available through the agent")),
		capability("logs.runtime.stream", "Runtime log streams", "observability", scopes("cluster", "namespace", "workload", "pod"), CapabilityRiskRead, false, "/operations/log-observability", available(), available("bounded runtime log streams are available through the agent")),
		capability("logs.runtime.aggregate", "Runtime log aggregation", "observability", scopes("cluster", "namespace", "workload", "pod"), CapabilityRiskRead, false, "/operations/log-observability", available(), available("bounded multi-source runtime aggregation is available through the agent")),
		capability("pod.exec", "Pod exec and terminal", "workloads", scopes("cluster", "namespace", "pod"), CapabilityRiskExecute, true, "/operations/agent-runtime", available(), available("non-interactive exec and interactive terminal sessions are available through the agent")),
		capability("metrics", "Metrics", "observability", scopes("cluster", "namespace"), CapabilityRiskRead, false, "/architecture/monitoring-and-alerting", available(), available("Prometheus transport can be selected independently: Core direct or the Agent configured endpoint")),
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
