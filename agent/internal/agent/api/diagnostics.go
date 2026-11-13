package api

import (
	"strings"
	"time"

	"github.com/opensoha/soha-agent/internal/agent/buildinfo"
	cfgpkg "github.com/opensoha/soha-agent/internal/agent/config"
	runnerpkg "github.com/opensoha/soha-agent/internal/agent/runner"
)

type diagnosticsView struct {
	Build        buildinfo.Info              `json:"build"`
	App          diagnosticsAppView          `json:"app"`
	HTTP         diagnosticsHTTPView         `json:"http"`
	Security     diagnosticsSecurityView     `json:"security"`
	Kubernetes   diagnosticsKubernetesView   `json:"kubernetes"`
	Capabilities diagnosticsCapabilitiesView `json:"capabilities"`
	ControlPlane diagnosticsControlPlaneView `json:"controlPlane"`
	Runtime      diagnosticsRuntimeView      `json:"runtime"`
	Metrics      *runnerpkg.MetricsSnapshot  `json:"metrics,omitempty"`
}

type diagnosticsAppView struct {
	Name string `json:"name"`
	Env  string `json:"env"`
}

type diagnosticsHTTPView struct {
	AddrConfigured      bool   `json:"addrConfigured"`
	BasePath            string `json:"basePath"`
	AllowedOriginsCount int    `json:"allowedOriginsCount"`
}

type diagnosticsSecurityView struct {
	AllowedActionsCount int  `json:"allowedActionsCount"`
	AuditFileConfigured bool `json:"auditFileConfigured"`
}

type diagnosticsKubernetesView struct {
	Enabled              bool   `json:"enabled"`
	ClientAvailable      bool   `json:"clientAvailable"`
	ID                   string `json:"id,omitempty"`
	Name                 string `json:"name,omitempty"`
	Region               string `json:"region,omitempty"`
	Environment          string `json:"environment,omitempty"`
	ContextConfigured    bool   `json:"contextConfigured"`
	KubeconfigConfigured bool   `json:"kubeconfigConfigured"`
	KubeconfigDataLoaded bool   `json:"kubeconfigDataLoaded"`
	LabelsCount          int    `json:"labelsCount"`
}

type diagnosticsCapabilitiesView struct {
	Mode          string                      `json:"mode"`
	Status        string                      `json:"status"`
	RequiredKeys  []string                    `json:"requiredKeys"`
	AvailableKeys []string                    `json:"availableKeys,omitempty"`
	DegradedKeys  []string                    `json:"degradedKeys,omitempty"`
	Items         []diagnosticsCapabilityItem `json:"items"`
	Message       string                      `json:"message,omitempty"`
}

type diagnosticsCapabilityItem struct {
	Key    string `json:"key"`
	Status string `json:"status"`
	Reason string `json:"reason,omitempty"`
}

type diagnosticsControlPlaneView struct {
	Enabled                   bool     `json:"enabled"`
	BaseURLConfigured         bool     `json:"baseUrlConfigured"`
	AgentID                   string   `json:"agentId,omitempty"`
	RuntimeEndpointConfigured bool     `json:"runtimeEndpointConfigured"`
	MaxConcurrency            int      `json:"maxConcurrency"`
	PollInterval              string   `json:"pollInterval,omitempty"`
	DefaultTimeout            string   `json:"defaultTimeout,omitempty"`
	ProviderKinds             []string `json:"providerKinds,omitempty"`
	WorkspaceRootConfigured   bool     `json:"workspaceRootConfigured"`
	CallbackRetry             struct {
		MaxAttempts int    `json:"maxAttempts"`
		Backoff     string `json:"backoff,omitempty"`
	} `json:"callbackRetry"`
	Docker       diagnosticsDockerRunnerView `json:"docker"`
	AgentRuntime diagnosticsAgentRuntimeView `json:"agentRuntime"`
	Outpost      runnerpkg.OutpostStatus     `json:"outpost"`
}

type diagnosticsDockerRunnerView struct {
	Enabled               bool     `json:"enabled"`
	WorkerIDConfigured    bool     `json:"workerIdConfigured"`
	HostCount             int      `json:"hostCount"`
	OperationKinds        []string `json:"operationKinds,omitempty"`
	ComposeRootConfigured bool     `json:"composeRootConfigured"`
	PollInterval          string   `json:"pollInterval,omitempty"`
}

type diagnosticsAgentRuntimeView struct {
	Enabled                 bool     `json:"enabled"`
	WorkerIDConfigured      bool     `json:"workerIdConfigured"`
	ProviderIDs             []string `json:"providerIds,omitempty"`
	ProviderKinds           []string `json:"providerKinds,omitempty"`
	ProviderCount           int      `json:"providerCount"`
	HermesCommandConfigured bool     `json:"hermesCommandConfigured"`
	WorkspaceRootConfigured bool     `json:"workspaceRootConfigured"`
	PollInterval            string   `json:"pollInterval,omitempty"`
}

type diagnosticsRuntimeView struct {
	ControllerAvailable bool `json:"controllerAvailable"`
	ActiveTasks         int  `json:"activeTasks"`
	MetricsAvailable    bool `json:"metricsAvailable"`
}

func buildDiagnosticsView(cfg cfgpkg.Config, kubernetesClientAvailable bool, runtime RuntimeTaskController) diagnosticsView {
	view := diagnosticsView{
		Build: buildinfo.Current(),
		App: diagnosticsAppView{
			Name: cfg.App.Name,
			Env:  cfg.App.Env,
		},
		HTTP: diagnosticsHTTPView{
			AddrConfigured:      strings.TrimSpace(cfg.HTTP.Addr) != "",
			BasePath:            cfg.HTTP.BasePath,
			AllowedOriginsCount: len(cfg.HTTP.AllowedOrigins),
		},
		Security: diagnosticsSecurityView{
			AllowedActionsCount: len(cfg.Security.AllowedActions),
			AuditFileConfigured: strings.TrimSpace(cfg.Audit.FilePath) != "",
		},
		Kubernetes: diagnosticsKubernetesView{
			Enabled:              cfg.Kubernetes.Enabled,
			ClientAvailable:      kubernetesClientAvailable,
			ID:                   cfg.Kubernetes.ID,
			Name:                 cfg.Kubernetes.Name,
			Region:               cfg.Kubernetes.Region,
			Environment:          cfg.Kubernetes.Environment,
			ContextConfigured:    strings.TrimSpace(cfg.Kubernetes.Context) != "",
			KubeconfigConfigured: strings.TrimSpace(cfg.Kubernetes.Kubeconfig) != "",
			KubeconfigDataLoaded: strings.TrimSpace(cfg.Kubernetes.KubeconfigData) != "",
			LabelsCount:          len(cfg.Kubernetes.Labels),
		},
		Capabilities: buildDiagnosticsCapabilitiesView(cfg, kubernetesClientAvailable),
		ControlPlane: diagnosticsControlPlaneView{
			Enabled:                   cfg.ControlPlane.Enabled,
			BaseURLConfigured:         strings.TrimSpace(cfg.ControlPlane.BaseURL) != "",
			AgentID:                   cfg.ControlPlane.AgentID,
			RuntimeEndpointConfigured: strings.TrimSpace(cfg.ControlPlane.RuntimeEndpoint) != "",
			MaxConcurrency:            cfg.ControlPlane.MaxConcurrency,
			PollInterval:              durationString(cfg.ControlPlane.PollInterval),
			DefaultTimeout:            durationString(cfg.ControlPlane.DefaultTimeout),
			ProviderKinds:             append([]string(nil), cfg.ControlPlane.ProviderKinds...),
			WorkspaceRootConfigured:   strings.TrimSpace(cfg.ControlPlane.WorkspaceRoot) != "",
			Docker: diagnosticsDockerRunnerView{
				Enabled:               cfg.ControlPlane.Docker.Enabled,
				WorkerIDConfigured:    strings.TrimSpace(cfg.ControlPlane.Docker.WorkerID) != "",
				HostCount:             len(cfg.ControlPlane.Docker.HostIDs),
				OperationKinds:        append([]string(nil), cfg.ControlPlane.Docker.OperationKinds...),
				ComposeRootConfigured: strings.TrimSpace(cfg.ControlPlane.Docker.ComposeRoot) != "",
				PollInterval:          durationString(cfg.ControlPlane.Docker.PollInterval),
			},
			AgentRuntime: diagnosticsAgentRuntimeView{
				Enabled:                 cfg.ControlPlane.AgentRuntime.Enabled,
				WorkerIDConfigured:      strings.TrimSpace(cfg.ControlPlane.AgentRuntime.WorkerID) != "",
				ProviderIDs:             append([]string(nil), cfg.ControlPlane.AgentRuntime.ProviderIDs...),
				ProviderKinds:           append([]string(nil), cfg.ControlPlane.AgentRuntime.ProviderKinds...),
				ProviderCount:           len(cfg.ControlPlane.AgentRuntime.Providers),
				HermesCommandConfigured: strings.TrimSpace(cfg.ControlPlane.AgentRuntime.HermesCommand) != "",
				WorkspaceRootConfigured: strings.TrimSpace(cfg.ControlPlane.AgentRuntime.WorkspaceRoot) != "",
				PollInterval:            durationString(cfg.ControlPlane.AgentRuntime.PollInterval),
			},
		},
		Runtime: diagnosticsRuntimeView{
			ControllerAvailable: runtime != nil,
		},
	}
	view.ControlPlane.CallbackRetry.MaxAttempts = cfg.ControlPlane.CallbackRetry.MaxAttempts
	view.ControlPlane.CallbackRetry.Backoff = durationString(cfg.ControlPlane.CallbackRetry.Backoff)
	if runtime != nil {
		view.Runtime.ActiveTasks = len(runtime.ListActiveTasks())
	}
	if metrics, ok := runtime.(RuntimeMetricsController); ok {
		snapshot := metrics.MetricsSnapshot()
		view.Runtime.ActiveTasks = snapshot.ActiveTasks
		view.Runtime.MetricsAvailable = true
		view.Metrics = &snapshot
	}
	if outpost, ok := runtime.(interface {
		OutpostStatus() runnerpkg.OutpostStatus
	}); ok {
		view.ControlPlane.Outpost = outpost.OutpostStatus()
	}
	return view
}

var managedAgentDiagnosticCapabilityKeys = []string{
	"cluster.inventory",
	"workload.read",
	"network.inventory",
	"port.forward",
	"pod.logs",
	"logs.runtime.snapshot",
	"logs.runtime.stream",
	"logs.runtime.aggregate",
	"pod.exec",
	"workload.mutations",
	"resource.creation",
	"helm.releases",
}

func buildDiagnosticsCapabilitiesView(cfg cfgpkg.Config, kubernetesClientAvailable bool) diagnosticsCapabilitiesView {
	items := make([]diagnosticsCapabilityItem, 0, len(managedAgentDiagnosticCapabilityKeys))
	if !cfg.Kubernetes.Enabled {
		for _, key := range managedAgentDiagnosticCapabilityKeys {
			items = append(items, diagnosticsCapabilityItem{Key: key, Status: "unsupported", Reason: "kubernetes runtime is disabled"})
		}
		return diagnosticsCapabilitiesView{
			Mode:         "agent",
			Status:       "degraded",
			RequiredKeys: append([]string(nil), managedAgentDiagnosticCapabilityKeys...),
			DegradedKeys: append([]string(nil), managedAgentDiagnosticCapabilityKeys...),
			Items:        items,
			Message:      "Managed-agent Kubernetes capabilities are unavailable because Kubernetes runtime is disabled.",
		}
	}
	if !kubernetesClientAvailable {
		for _, key := range managedAgentDiagnosticCapabilityKeys {
			items = append(items, diagnosticsCapabilityItem{Key: key, Status: "unsupported", Reason: "kubernetes client is not available"})
		}
		return diagnosticsCapabilitiesView{
			Mode:         "agent",
			Status:       "degraded",
			RequiredKeys: append([]string(nil), managedAgentDiagnosticCapabilityKeys...),
			DegradedKeys: append([]string(nil), managedAgentDiagnosticCapabilityKeys...),
			Items:        items,
			Message:      "Managed-agent Kubernetes capabilities are unavailable because Kubernetes client initialization failed or was not configured.",
		}
	}

	items = append(items,
		diagnosticsCapabilityItem{Key: "cluster.inventory", Status: "available"},
		diagnosticsCapabilityItem{Key: "workload.read", Status: "available"},
		diagnosticsCapabilityItem{Key: "network.inventory", Status: "available"},
		guardedCapabilityItem("port.forward",
			cfg.Security.AllowedActions,
			"port-forward actions are not fully allowlisted",
			actionPlatformPortForwardsCreate,
			actionPlatformPortForwardsTunnel,
			actionPlatformPortForwardsDelete,
		),
		diagnosticsCapabilityItem{Key: "pod.logs", Status: "available"},
		diagnosticsCapabilityItem{Key: "logs.runtime.snapshot", Status: "available"},
		diagnosticsCapabilityItem{Key: "logs.runtime.stream", Status: "available"},
		diagnosticsCapabilityItem{Key: "logs.runtime.aggregate", Status: "available"},
		guardedCapabilityItem("pod.exec",
			cfg.Security.AllowedActions,
			"pod exec action is not allowlisted",
			actionPlatformPodsExec,
		),
		guardedCapabilityItem("workload.mutations",
			cfg.Security.AllowedActions,
			"workload mutation actions are not fully allowlisted",
			actionPlatformDeploymentRestart,
			actionPlatformDeploymentScale,
			actionPlatformDeploymentImage,
			actionPlatformDeploymentRollback,
			actionPlatformStatefulSetRestart,
			actionPlatformStatefulSetScale,
			actionPlatformDaemonSetRestart,
		),
		guardedCapabilityItem("resource.creation",
			cfg.Security.AllowedActions,
			"resource create action is not allowlisted",
			actionPlatformResourcesCreate,
		),
		guardedCapabilityItem("helm.releases",
			cfg.Security.AllowedActions,
			"Helm release mutation actions are not fully allowlisted",
			actionPlatformHelmReleaseInstall,
			actionPlatformHelmReleaseValuesUpdate,
			actionPlatformHelmReleaseDelete,
			actionPlatformHelmReleaseRollback,
		),
	)

	view := diagnosticsCapabilitiesView{
		Mode:         "agent",
		Status:       "available",
		RequiredKeys: append([]string(nil), managedAgentDiagnosticCapabilityKeys...),
		Items:        items,
		Message:      "All managed-agent required capabilities are available.",
	}
	for _, item := range items {
		if item.Status == "available" {
			view.AvailableKeys = append(view.AvailableKeys, item.Key)
			continue
		}
		view.DegradedKeys = append(view.DegradedKeys, item.Key)
	}
	if len(view.DegradedKeys) > 0 {
		view.Status = "degraded"
		view.Message = "One or more managed-agent capabilities are partial or unsupported by current Agent configuration."
	}
	return view
}

func guardedCapabilityItem(key string, allowedActions []string, reason string, actions ...string) diagnosticsCapabilityItem {
	for _, action := range actions {
		if !securityActionAllowed(allowedActions, action) {
			return diagnosticsCapabilityItem{Key: key, Status: "partial", Reason: reason}
		}
	}
	return diagnosticsCapabilityItem{Key: key, Status: "available"}
}

func securityActionAllowed(allowedActions []string, action string) bool {
	normalized := normalizeAction(action)
	if normalized == "" {
		return false
	}
	for _, allowed := range allowedActions {
		switch normalizeAction(allowed) {
		case "*", normalized:
			return true
		}
	}
	return false
}

func durationString(value time.Duration) string {
	if value <= 0 {
		return ""
	}
	return value.String()
}
