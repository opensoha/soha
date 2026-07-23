package runtimeconfig

import (
	"fmt"
	"net/url"
	"os"
	"sort"
	"strings"
	"sync/atomic"

	sohaapi "github.com/opensoha/soha-contracts/gen/go/sohaapi"
)

const (
	DefaultMarketplaceURL      = "https://marketplace.opensoha.com/marketplace/index.json"
	DefaultMarketplaceSourceID = "opensoha-official"
)

type RegistryOptions struct {
	AssistantGlobal      bool
	ModuleHome           bool
	ModuleAI             bool
	ModuleMonitoring     bool
	ModuleVirtualization bool
	ModuleDocker         bool
	ModuleAIGateway      bool
	ModuleDelivery       bool
	ModuleSecurity       bool
	ModuleCMDB           bool
	MarketplaceURL       string
	MarketplaceSourceID  string
}

const (
	KeyAssistantGlobal      = "modules.ai.features.assistant.global"
	KeyModuleHome           = "modules.home.enabled"
	KeyModuleAI             = "modules.ai.enabled"
	KeyModuleMonitoring     = "modules.monitoring.enabled"
	KeyModuleVirtualization = "modules.virtualization.enabled"
	KeyModuleDocker         = "modules.docker.enabled"
	KeyModuleAIGateway      = "modules.ai_gateway.enabled"
	KeyModuleDelivery       = "modules.delivery.enabled"
	KeyModuleSecurity       = "modules.security.enabled"
	KeyModuleCMDB           = "modules.cmdb.enabled"
	KeyMarketplaceURL       = "plugins.marketplace.url"
	KeyMarketplaceSourceID  = "plugins.marketplace.source_id"
)

type Definition struct {
	Key                 string
	Category            string
	Label               string
	Description         string
	ValueType           sohaapi.RuntimeConfigValueType
	ApplyMode           sohaapi.RuntimeConfigApplyMode
	DefaultValue        any
	BaselineValue       any
	EnvironmentVariable string
	Editable            bool
	Sensitive           bool
	Validate            func(any) error
}

type Registry struct {
	definitions map[string]Definition
	keys        []string
}

func NewRegistry(options RegistryOptions) *Registry {
	marketplaceURL := firstValue(options.MarketplaceURL, DefaultMarketplaceURL)
	marketplaceSourceID := firstValue(options.MarketplaceSourceID, DefaultMarketplaceSourceID)
	definitions := []Definition{
		booleanDefinitionWithDescription(KeyModuleHome, "模块", "首页", "门户首页与工作台入口；关闭后从工作台列表和导航中隐藏", sohaapi.RuntimeConfigApplyModeHot, options.ModuleHome),
		booleanDefinitionWithDescription(KeyAssistantGlobal, "模块", "全局 AI 助手", "AI 工作台内的全局入口；仅在 AI 工作台开启时可用", sohaapi.RuntimeConfigApplyModeHot, options.AssistantGlobal),
		booleanDefinitionWithDescription(KeyModuleAI, "模块", "AI 工作台", "启停 AI 工作台运行服务；关闭时全局 AI 助手必须同时关闭", sohaapi.RuntimeConfigApplyModeLifecycle, options.ModuleAI),
		booleanDefinition(KeyModuleMonitoring, "模块", "监控工作台", sohaapi.RuntimeConfigApplyModeLifecycle, options.ModuleMonitoring),
		booleanDefinition(KeyModuleVirtualization, "计算资源", "虚拟化资源", sohaapi.RuntimeConfigApplyModeLifecycle, options.ModuleVirtualization),
		booleanDefinition(KeyModuleDocker, "计算资源", "容器运行时", sohaapi.RuntimeConfigApplyModeHot, options.ModuleDocker),
		booleanDefinitionWithDescription(KeyModuleAIGateway, "模块", "AI Gateway", "独立网关能力，可脱离 AI 工作台运行；关闭后相关接口与菜单即时不可用", sohaapi.RuntimeConfigApplyModeHot, options.ModuleAIGateway),
		booleanDefinition(KeyModuleDelivery, "模块", "交付工作台", sohaapi.RuntimeConfigApplyModeHot, options.ModuleDelivery),
		placeholderModuleDefinition(KeyModuleSecurity, "内网工作台", options.ModuleSecurity),
		placeholderModuleDefinition(KeyModuleCMDB, "CMDB 工作台", options.ModuleCMDB),
		{
			Key: KeyMarketplaceURL, Category: "连接", Label: "插件市场地址",
			ValueType: sohaapi.RuntimeConfigValueTypeURL, ApplyMode: sohaapi.RuntimeConfigApplyModeReconfigure,
			DefaultValue: DefaultMarketplaceURL, BaselineValue: marketplaceURL,
			EnvironmentVariable: "SOHA_PLUGINS_MARKETPLACE_URL", Editable: true, Validate: validateHTTPURL,
		},
		{
			Key: KeyMarketplaceSourceID, Category: "连接", Label: "插件市场来源 ID",
			ValueType: sohaapi.RuntimeConfigValueTypeString, ApplyMode: sohaapi.RuntimeConfigApplyModeReconfigure,
			DefaultValue: DefaultMarketplaceSourceID, BaselineValue: marketplaceSourceID,
			EnvironmentVariable: "SOHA_PLUGINS_MARKETPLACE_SOURCE_ID", Editable: true,
			Validate: func(value any) error {
				if strings.TrimSpace(fmt.Sprint(value)) == "" {
					return fmt.Errorf("source id is required")
				}
				return nil
			},
		},
	}
	items := make(map[string]Definition, len(definitions))
	keys := make([]string, 0, len(definitions))
	for _, definition := range definitions {
		items[definition.Key] = definition
		keys = append(keys, definition.Key)
	}
	sort.Strings(keys)
	return &Registry{definitions: items, keys: keys}
}

func booleanDefinition(key, category, label string, mode sohaapi.RuntimeConfigApplyMode, baseline bool) Definition {
	return Definition{
		Key: key, Category: category, Label: label, ValueType: sohaapi.RuntimeConfigValueTypeBoolean,
		ApplyMode: mode, DefaultValue: baseline, BaselineValue: baseline,
		EnvironmentVariable: "SOHA_" + strings.ToUpper(strings.NewReplacer(".", "_", "-", "_").Replace(key)), Editable: true,
	}
}

func booleanDefinitionWithDescription(key, category, label, description string, mode sohaapi.RuntimeConfigApplyMode, baseline bool) Definition {
	definition := booleanDefinition(key, category, label, mode, baseline)
	definition.Description = description
	return definition
}

func placeholderModuleDefinition(key, label string, baseline bool) Definition {
	definition := booleanDefinition(key, "模块", label, sohaapi.RuntimeConfigApplyModeRestart, baseline)
	definition.Description = "尚未接入运行时启停，修改后需重启 Soha"
	return definition
}

func (r *Registry) Definition(key string) (Definition, bool) {
	definition, ok := r.definitions[strings.TrimSpace(key)]
	return definition, ok
}

func (r *Registry) Definitions() []Definition {
	items := make([]Definition, 0, len(r.keys))
	for _, key := range r.keys {
		items = append(items, r.definitions[key])
	}
	return items
}

func (d Definition) lockedByEnvironment() bool {
	if d.EnvironmentVariable == "" {
		return false
	}
	_, ok := os.LookupEnv(d.EnvironmentVariable)
	return ok
}

func validateHTTPURL(value any) error {
	parsed, err := url.Parse(strings.TrimSpace(fmt.Sprint(value)))
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return fmt.Errorf("must be an absolute http or https URL")
	}
	return nil
}

func firstValue(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

type Snapshot struct {
	Version          int64
	ActiveRevisionID string
	Overrides        map[string]any
	registry         *Registry
}

func (s Snapshot) Value(key string) (any, bool) {
	definition, ok := s.registry.Definition(key)
	if !ok {
		return nil, false
	}
	if definition.lockedByEnvironment() {
		return definition.BaselineValue, true
	}
	if value, exists := s.Overrides[key]; exists {
		return value, true
	}
	return definition.BaselineValue, true
}

func (s Snapshot) Bool(key string, fallback bool) bool {
	value, ok := s.Value(key)
	if !ok {
		return fallback
	}
	result, ok := value.(bool)
	if !ok {
		return fallback
	}
	return result
}

func (s Snapshot) String(key, fallback string) string {
	value, ok := s.Value(key)
	if !ok {
		return fallback
	}
	result, ok := value.(string)
	if !ok {
		return fallback
	}
	return result
}

func (s Snapshot) ModuleEnabled(id string) bool {
	switch strings.TrimSpace(id) {
	case "home":
		return s.Bool(KeyModuleHome, true)
	case "platform":
		return true
	case "compute":
		return s.Bool(KeyModuleVirtualization, false) || s.Bool(KeyModuleDocker, false)
	case "ai":
		return s.Bool(KeyModuleAI, false)
	case "monitoring":
		return s.Bool(KeyModuleMonitoring, false)
	case "virtualization":
		return s.Bool(KeyModuleVirtualization, false)
	case "docker":
		return s.Bool(KeyModuleDocker, false)
	case "delivery":
		return s.Bool(KeyModuleDelivery, false)
	case "security":
		return s.Bool(KeyModuleSecurity, false)
	case "cmdb":
		return s.Bool(KeyModuleCMDB, false)
	case "aiGateway", "ai-gateway", "ai_gateway":
		return s.Bool(KeyModuleAIGateway, false)
	default:
		return true
	}
}

type snapshotPointer struct {
	value atomic.Pointer[Snapshot]
}

func (p *snapshotPointer) Load() Snapshot {
	value := p.value.Load()
	if value == nil {
		return Snapshot{}
	}
	return *value
}

func (p *snapshotPointer) Store(snapshot Snapshot) {
	snapshot.Overrides = cloneValues(snapshot.Overrides)
	p.value.Store(&snapshot)
}

func cloneValues(source map[string]any) map[string]any {
	out := make(map[string]any, len(source))
	for key, value := range source {
		out[key] = value
	}
	return out
}
