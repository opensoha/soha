package module

import (
	"context"
	"maps"
	"strings"

	domainmodule "github.com/opensoha/soha/internal/domain/module"
	"github.com/opensoha/soha/internal/platform/appconfig"
)

type Service struct {
	cfg     appconfig.Modules
	runtime interface {
		ModuleEnabled(string) bool
		FeatureEnabled(string, string) bool
	}
}

func New(cfg appconfig.Modules) *Service {
	return &Service{cfg: cfg}
}

func NewRuntime(runtime interface {
	ModuleEnabled(string) bool
	FeatureEnabled(string, string) bool
}) *Service {
	return &Service{runtime: runtime}
}

func (s *Service) List(context.Context) ([]domainmodule.Status, error) {
	descriptors := []domainmodule.Descriptor{
		{
			ID:                 "home",
			Name:               "首页",
			DefaultPath:        "/portal",
			EnabledConfigKey:   "modules.home.enabled",
			Dependencies:       []string{},
			VisiblePermissions: []string{"workbench.home.view"},
			SeedMenus:          []string{"home-workbench"},
		},
		{
			ID:                 "platform",
			Name:               "k8s工作台",
			DefaultPath:        "/",
			EnabledConfigKey:   "",
			Dependencies:       []string{},
			VisiblePermissions: []string{"workbench.platform.view"},
			SeedMenus:          []string{"dashboard", "clusters", "workloads", "configuration", "network", "network-gateway-api-gatewayclasses", "network-gateway-api-gateways", "network-gateway-api-httproutes", "network-gateway-api-backendtlspolicies", "network-gateway-api-grpcroutes", "network-gateway-api-referencegrants", "storage", "platform-access-control", "extensions", "helm"},
		},
		{
			ID:                 "compute",
			Name:               "计算资源工作台",
			DefaultPath:        "/compute/overview",
			EnabledConfigKey:   "modules.virtualization.enabled|modules.docker.enabled",
			Dependencies:       []string{},
			VisiblePermissions: []string{"workbench.compute.view"},
			SeedMenus:          []string{"compute-workbench", "compute-workbench-overview", "virtualization-workbench", "virtualization-workbench-vms", "virtualization-workbench-clusters", "virtualization-workbench-images", "virtualization-workbench-storage", "virtualization-workbench-flavors", "docker-workbench", "docker-workbench-hosts", "docker-workbench-projects", "docker-workbench-templates", "compute-workbench-tasks-operations"},
		},
		{
			ID:                 "virtualization",
			Name:               "虚拟化资源",
			DefaultPath:        "/compute/virtualization",
			EnabledConfigKey:   "modules.virtualization.enabled",
			Dependencies:       []string{},
			VisiblePermissions: []string{"virtualization.overview.view", "virtualization.vms.view", "virtualization.clusters.view", "virtualization.images.view", "virtualization.flavors.view", "virtualization.operations.view", "virtualization.sync.view"},
			SeedMenus:          []string{"virtualization-workbench", "virtualization-workbench-vms", "virtualization-workbench-clusters", "virtualization-workbench-images", "virtualization-workbench-storage", "virtualization-workbench-flavors"},
		},
		{
			ID:                 "docker",
			Name:               "容器运行时",
			DefaultPath:        "/compute/runtimes",
			EnabledConfigKey:   "modules.docker.enabled",
			Dependencies:       []string{},
			VisiblePermissions: []string{"docker.overview.view", "docker.hosts.view", "docker.projects.view", "docker.services.view", "docker.ports.view", "docker.templates.view", "docker.operations.view"},
			SeedMenus:          []string{"docker-workbench", "docker-workbench-hosts", "docker-workbench-projects", "docker-workbench-templates"},
		},
		{
			ID:                 "delivery",
			Name:               "应用交付工作台",
			DefaultPath:        "/applications",
			EnabledConfigKey:   "modules.delivery.enabled",
			Dependencies:       []string{},
			VisiblePermissions: []string{"workbench.delivery.view"},
			SeedMenus:          []string{"builds", "delivery-overview", "delivery-onboarding", "release-board", "delivery-testing", "delivery-analysis", "build-templates", "release-bundles", "execution-tasks", "workflow-templates", "application-environments", "workflows", "releases", "registries", "delivery-blueprints"},
		},
		{
			ID:                 "ai",
			Name:               "AI工作台",
			DefaultPath:        "/ai-workbench",
			EnabledConfigKey:   "modules.ai.enabled",
			Dependencies:       []string{"delivery"},
			VisiblePermissions: []string{"workbench.ai.view"},
			SeedMenus:          []string{"ai-workbench", "ai-workbench-overview", "ai-workbench-chat", "ai-workbench-companion", "ai-workbench-knowledge", "ai-workbench-knowledge-pipelines", "ai-workbench-inspection", "ai-workbench-agent-runs", "ai-workbench-evaluations", "ai-workbench-evaluation-lifecycle", "ai-workbench-context", "ai-workbench-memory", "ai-workbench-tool-settings", "ai-workbench-agent-providers", "ai-workbench-provider-fleet", "ai-workbench-environments", "ai-workbench-model-settings", "ai-workbench-production-operations"},
		},
		{
			ID:                 "aiGateway",
			Name:               "AI Gateway",
			DefaultPath:        "/ai-gateway/manifest",
			EnabledConfigKey:   "modules.ai_gateway.enabled",
			Dependencies:       []string{},
			VisiblePermissions: []string{"ai.gateway.view", "ai.gateway.invoke", "ai.gateway.relay.view", "ai.gateway.clients.view", "ai.gateway.tokens.view", "ai.gateway.approvals.view", "ai.gateway.grants.view", "ai.gateway.policies.view", "ai.gateway.skills.view"},
			SeedMenus:          []string{"ai-gateway-relay", "ai-gateway-manifest", "ai-gateway-clients", "ai-gateway-tokens", "ai-gateway-governance", "ai-gateway-call-logs"},
		},
		{
			ID:                 "monitoring",
			Name:               "可观测性工作台",
			DefaultPath:        "/monitoring-workbench",
			EnabledConfigKey:   "modules.monitoring.enabled",
			Dependencies:       []string{"ai"},
			VisiblePermissions: []string{"workbench.monitoring.view"},
			SeedMenus:          []string{"monitoring-workbench", "monitoring-workbench-overview", "monitoring-workbench-services", "monitoring-workbench-explore", "monitoring-workbench-dashboards", "monitoring-workbench-providers", "monitoring-workbench-log-data-sources", "monitoring-workbench-integrations", "monitoring-workbench-alerts", "monitoring-workbench-rules", "monitoring-workbench-notifications", "monitoring-workbench-healing", "monitoring-workbench-oncall", "monitoring-workbench-events"},
		},
		{
			ID:                 "security",
			Name:               "内网工作台",
			DefaultPath:        "/internal-workbench/overview",
			EnabledConfigKey:   "modules.security.enabled",
			VisiblePermissions: []string{"workbench.security.view"},
			SeedMenus:          []string{"identity", "identity-applications", "identity-providers", "identity-outposts", "identity-policies"},
		},
		{
			ID:                 "cmdb",
			Name:               "CMDB 工作台",
			DefaultPath:        "/cmdb",
			EnabledConfigKey:   "modules.cmdb.enabled",
			VisiblePermissions: []string{"cmdb.view"},
		},
	}
	out := make([]domainmodule.Status, 0, len(descriptors))
	for _, descriptor := range descriptors {
		var features map[string]bool
		if descriptor.ID == "ai" {
			features = maps.Clone(s.cfg.AI.FeatureFlags())
			if s.runtime != nil {
				if features == nil {
					features = map[string]bool{}
				}
				features["assistant.global"] = s.runtime.FeatureEnabled("ai", "assistant.global")
			}
		}
		out = append(out, domainmodule.Status{
			Descriptor: descriptor,
			Enabled:    s.enabled(descriptor.ID),
			Features:   features,
		})
	}
	return out, nil
}

func (s *Service) enabled(id string) bool {
	if s.runtime != nil {
		return s.runtime.ModuleEnabled(id)
	}
	switch strings.TrimSpace(id) {
	case "home":
		return s.cfg.Home.Enabled
	case "compute":
		return s.cfg.Virtualization.Enabled || s.cfg.Docker.Enabled
	case "delivery":
		return s.cfg.Delivery.Enabled
	case "monitoring":
		return s.cfg.Monitoring.Enabled
	case "ai":
		return s.cfg.AI.Enabled
	case "aiGateway", "ai-gateway":
		return s.cfg.AIGateway.Enabled
	case "virtualization":
		return s.cfg.Virtualization.Enabled
	case "docker":
		return s.cfg.Docker.Enabled
	case "security":
		return s.cfg.Security.Enabled
	case "cmdb":
		return s.cfg.CMDB.Enabled
	case "platform":
		return true
	default:
		return true
	}
}
