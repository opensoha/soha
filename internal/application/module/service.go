package module

import (
	"context"
	"strings"

	domainmodule "github.com/opensoha/soha/internal/domain/module"
	cfgpkg "github.com/opensoha/soha/internal/infrastructure/config"
)

type Service struct {
	cfg cfgpkg.ModulesConfig
}

func New(cfg cfgpkg.ModulesConfig) *Service {
	return &Service{cfg: cfg}
}

func (s *Service) List(context.Context) ([]domainmodule.Status, error) {
	descriptors := []domainmodule.Descriptor{
		{
			ID:                 "platform",
			Name:               "k8s工作台",
			DefaultPath:        "/",
			EnabledConfigKey:   "",
			Dependencies:       []string{},
			VisiblePermissions: []string{"workspace.resource.view"},
			SeedMenus:          []string{"dashboard", "clusters", "workloads", "configuration", "network", "network-gateway-api", "network-gateway-api-gatewayclasses", "network-gateway-api-gateways", "storage", "platform-access-control", "extensions", "helm"},
		},
		{
			ID:                 "virtualization",
			Name:               "虚拟化管理工作台",
			DefaultPath:        "/virtualization",
			EnabledConfigKey:   "modules.virtualization.enabled",
			Dependencies:       []string{},
			VisiblePermissions: []string{"virtualization.overview.view", "virtualization.vms.view", "virtualization.clusters.view", "virtualization.images.view", "virtualization.flavors.view", "virtualization.operations.view", "virtualization.sync.view", "virtualization.sync.manage"},
			SeedMenus:          []string{"virtualization-workbench", "virtualization-workbench-overview", "virtualization-workbench-vms", "virtualization-workbench-clusters", "virtualization-workbench-images", "virtualization-workbench-flavors", "virtualization-workbench-operations", "virtualization-workbench-sync"},
		},
		{
			ID:                 "docker",
			Name:               "Docker 工作台",
			DefaultPath:        "/docker",
			EnabledConfigKey:   "modules.docker.enabled",
			Dependencies:       []string{"virtualization"},
			VisiblePermissions: []string{"docker.overview.view", "docker.hosts.view", "docker.projects.view", "docker.services.view", "docker.ports.view", "docker.templates.view", "docker.operations.view"},
			SeedMenus:          []string{"docker-workbench", "docker-workbench-overview", "docker-workbench-hosts", "docker-workbench-projects", "docker-workbench-templates", "docker-workbench-operations"},
		},
		{
			ID:                 "delivery",
			Name:               "应用交付工作台",
			DefaultPath:        "/applications",
			EnabledConfigKey:   "modules.delivery.enabled",
			Dependencies:       []string{},
			VisiblePermissions: []string{"workspace.application.view"},
			SeedMenus:          []string{"builds", "application-management", "build-templates", "release-bundles", "execution-tasks", "approval-policies", "workflow-templates", "release-board", "application-environments", "workflows", "releases", "registries", "delivery-blueprints"},
		},
		{
			ID:                 "ai",
			Name:               "AI工作台",
			DefaultPath:        "/ai-workbench",
			EnabledConfigKey:   "modules.ai.enabled",
			Dependencies:       []string{"delivery"},
			VisiblePermissions: []string{"observe.ai.view", "observe.ai.chat"},
			SeedMenus:          []string{"ai-workbench", "ai-workbench-chat", "ai-workbench-inspection", "ai-workbench-tool-settings", "ai-workbench-model-settings"},
		},
		{
			ID:                 "aiGateway",
			Name:               "AI Gateway",
			DefaultPath:        "/ai-gateway/overview",
			EnabledConfigKey:   "modules.ai_gateway.enabled",
			Dependencies:       []string{},
			VisiblePermissions: []string{"ai.gateway.view", "ai.gateway.invoke", "ai.gateway.manage"},
			SeedMenus:          []string{"ai-gateway", "ai-gateway-overview", "ai-gateway-manifest", "ai-gateway-clients", "ai-gateway-tokens", "ai-gateway-governance", "ai-gateway-call-logs"},
		},
		{
			ID:                 "monitoring",
			Name:               "监控工作台",
			DefaultPath:        "/monitoring-workbench",
			EnabledConfigKey:   "modules.monitoring.enabled",
			Dependencies:       []string{"ai"},
			VisiblePermissions: []string{"observe.monitoring.view", "observe.alerts.view"},
			SeedMenus:          []string{"monitoring-workbench", "monitoring-workbench-overview", "monitoring-workbench-alerts", "monitoring-workbench-rules", "monitoring-workbench-notifications", "monitoring-workbench-healing", "monitoring-workbench-oncall", "monitoring-workbench-events"},
		},
		{
			ID:                 "security",
			Name:               "安全工作台",
			DefaultPath:        "/security",
			EnabledConfigKey:   "modules.security.enabled",
			VisiblePermissions: []string{"security.view"},
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
		out = append(out, domainmodule.Status{
			Descriptor: descriptor,
			Enabled:    s.enabled(descriptor.ID),
		})
	}
	return out, nil
}

func (s *Service) enabled(id string) bool {
	switch strings.TrimSpace(id) {
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
