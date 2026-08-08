package runtimeconfig

import (
	"fmt"
	"net/url"
	"os"
	"sort"
	"strings"
	"sync/atomic"
	"time"

	sohaapi "github.com/opensoha/soha-contracts/gen/go/sohaapi"
)

const (
	DefaultMarketplaceURL      = "https://marketplace.opensoha.com/marketplace/index.json"
	DefaultMarketplaceSourceID = "opensoha-official"
)

type RegistryOptions struct {
	AccessURL                     string
	AssistantGlobal               bool
	ModuleHome                    bool
	ModuleAI                      bool
	ModuleMonitoring              bool
	ModuleVirtualization          bool
	ModuleDocker                  bool
	ModuleAIGateway               bool
	ModuleDelivery                bool
	ModuleSecurity                bool
	ModuleCMDB                    bool
	MarketplaceURL                string
	MarketplaceSourceID           string
	WorkflowWorkers               int
	WorkflowQueueSize             int
	WorkflowNodeParallelism       int
	ClusterSyncParallelism        int
	CopilotInspectionParallelism  int
	AlertUpsertBatchSize          int
	VirtualizationWorkerInterval  time.Duration
	VirtualizationSyncConcurrency int
	ExecutionJobClusterID         string
	ExecutionJobNamespace         string
	ExecutionJobImage             string
	ExecutionJobGitImage          string
	ExecutionJobTTLSeconds        int
	MCPDefaultTimeout             time.Duration
	AIGatewayDefaultTimeout       time.Duration
	AIGatewayStreamTimeout        time.Duration
	AIGatewayFirstByteTimeout     time.Duration
	AIGatewayStreamIdleTimeout    time.Duration
	AIGatewayHealthCheckEnabled   bool
	AIGatewayHealthCheckInterval  time.Duration
	AIGatewayMaxRequestBodyMB     int
	AIGatewayIncludeStreamUsage   bool
}

const (
	KeyAccessURL                     = "system.access_url"
	KeyAssistantGlobal               = "modules.ai.features.assistant.global"
	KeyModuleHome                    = "modules.home.enabled"
	KeyModuleAI                      = "modules.ai.enabled"
	KeyModuleMonitoring              = "modules.monitoring.enabled"
	KeyModuleVirtualization          = "modules.virtualization.enabled"
	KeyModuleDocker                  = "modules.docker.enabled"
	KeyModuleAIGateway               = "modules.ai_gateway.enabled"
	KeyModuleDelivery                = "modules.delivery.enabled"
	KeyModuleSecurity                = "modules.security.enabled"
	KeyModuleCMDB                    = "modules.cmdb.enabled"
	KeyMarketplaceURL                = "plugins.marketplace.url"
	KeyMarketplaceSourceID           = "plugins.marketplace.source_id"
	KeyWorkflowWorkers               = "runtime.workflow_workers"
	KeyWorkflowQueueSize             = "runtime.workflow_queue_size"
	KeyWorkflowNodeParallelism       = "runtime.workflow_node_parallelism"
	KeyClusterSyncParallelism        = "runtime.cluster_sync_parallelism"
	KeyCopilotInspectionParallelism  = "runtime.copilot_inspection_parallelism"
	KeyAlertUpsertBatchSize          = "runtime.alert_upsert_batch_size"
	KeyVirtualizationWorkerInterval  = "runtime.virtualization_worker_interval"
	KeyVirtualizationSyncConcurrency = "runtime.virtualization_sync_concurrency"
	KeyExecutionJobClusterID         = "runtime.execution_job_cluster_id"
	KeyExecutionJobNamespace         = "runtime.execution_job_namespace"
	KeyExecutionJobImage             = "runtime.execution_job_image"
	KeyExecutionJobGitImage          = "runtime.execution_job_git_image"
	KeyExecutionJobTTLSeconds        = "runtime.execution_job_ttl_seconds"
	KeyMCPDefaultTimeout             = "mcp.default_timeout"
	KeyAIGatewayDefaultTimeout       = "ai_gateway.relay.default_timeout"
	KeyAIGatewayStreamTimeout        = "ai_gateway.relay.stream_timeout"
	KeyAIGatewayFirstByteTimeout     = "ai_gateway.relay.first_byte_timeout"
	KeyAIGatewayStreamIdleTimeout    = "ai_gateway.relay.stream_idle_timeout"
	KeyAIGatewayHealthCheckEnabled   = "ai_gateway.relay.health_check_enabled"
	KeyAIGatewayHealthCheckInterval  = "ai_gateway.relay.health_check_interval"
	KeyAIGatewayMaxRequestBodyMB     = "ai_gateway.relay.max_request_body_mb"
	KeyAIGatewayIncludeStreamUsage   = "ai_gateway.relay.include_usage_for_openai_stream"
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
		{
			Key: KeyAccessURL, Category: "系统", Label: "访问地址",
			Description: "Soha 对外访问地址，用于生成 Agent 安装清单和建立 Agent 会话",
			ValueType:   sohaapi.RuntimeConfigValueTypeURL, ApplyMode: sohaapi.RuntimeConfigApplyModeHot,
			DefaultValue: "", BaselineValue: strings.TrimRight(strings.TrimSpace(options.AccessURL), "/"),
			EnvironmentVariable: "SOHA_HTTP_ACCESS_URL", Editable: true, Validate: validateHTTPURL,
		},
		booleanDefinitionWithDescription(KeyModuleHome, "模块", "首页", "门户首页与工作台入口；关闭后从工作台列表和导航中隐藏", sohaapi.RuntimeConfigApplyModeHot, options.ModuleHome),
		booleanDefinitionWithDescription(KeyAssistantGlobal, "模块", "全局 AI 助手", "AI 工作台内的全局入口；仅在 AI 工作台开启时可用", sohaapi.RuntimeConfigApplyModeHot, options.AssistantGlobal),
		booleanDefinitionWithDescription(KeyModuleAI, "模块", "AI 工作台", "启停 AI 工作台运行服务；关闭时全局 AI 助手必须同时关闭", sohaapi.RuntimeConfigApplyModeLifecycle, options.ModuleAI),
		booleanDefinition(KeyModuleMonitoring, "模块", "可观测性工作台", sohaapi.RuntimeConfigApplyModeLifecycle, options.ModuleMonitoring),
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
		integerDefinition(KeyWorkflowWorkers, "任务调度", "工作流 Worker 数", "并行处理工作流任务的 Worker 数量", sohaapi.RuntimeConfigApplyModeRestart, 4, options.WorkflowWorkers, 1, 256),
		integerDefinition(KeyWorkflowQueueSize, "任务调度", "工作流队列容量", "等待执行的工作流任务队列容量", sohaapi.RuntimeConfigApplyModeRestart, 64, options.WorkflowQueueSize, 1, 65536),
		integerDefinition(KeyWorkflowNodeParallelism, "任务调度", "工作流节点并行度", "单个工作流内可并行执行的节点数量", sohaapi.RuntimeConfigApplyModeHot, 4, options.WorkflowNodeParallelism, 1, 256),
		integerDefinition(KeyClusterSyncParallelism, "任务调度", "集群同步并行度", "集群资源同步的最大并行数量", sohaapi.RuntimeConfigApplyModeHot, 4, options.ClusterSyncParallelism, 1, 256),
		integerDefinition(KeyCopilotInspectionParallelism, "任务调度", "AI 巡检并行度", "AI 巡检任务的最大并行数量", sohaapi.RuntimeConfigApplyModeHot, 2, options.CopilotInspectionParallelism, 1, 128),
		integerDefinition(KeyAlertUpsertBatchSize, "任务调度", "告警写入批量", "单次批量写入的告警数量", sohaapi.RuntimeConfigApplyModeHot, 100, options.AlertUpsertBatchSize, 1, 10000),
		durationDefinition(KeyVirtualizationWorkerInterval, "虚拟化", "任务轮询间隔", "虚拟化任务 Worker 的轮询间隔", sohaapi.RuntimeConfigApplyModeReconfigure, 2*time.Second, options.VirtualizationWorkerInterval),
		integerDefinition(KeyVirtualizationSyncConcurrency, "虚拟化", "同步并行度", "虚拟化资产同步的最大并行数量", sohaapi.RuntimeConfigApplyModeReconfigure, 1, options.VirtualizationSyncConcurrency, 1, 128),
		stringDefinition(KeyExecutionJobClusterID, "执行任务", "默认执行集群", "执行任务未指定集群时使用的集群 ID", sohaapi.RuntimeConfigApplyModeHot, "", options.ExecutionJobClusterID, false),
		stringDefinition(KeyExecutionJobNamespace, "执行任务", "执行命名空间", "Kubernetes 执行任务使用的命名空间", sohaapi.RuntimeConfigApplyModeHot, "soha-system", options.ExecutionJobNamespace, true),
		stringDefinition(KeyExecutionJobImage, "执行任务", "默认执行镜像", "普通执行步骤使用的默认容器镜像", sohaapi.RuntimeConfigApplyModeHot, "alpine:3.20", options.ExecutionJobImage, true),
		stringDefinition(KeyExecutionJobGitImage, "执行任务", "Git 执行镜像", "Git 操作步骤使用的默认容器镜像", sohaapi.RuntimeConfigApplyModeHot, "alpine/git:2.47.0", options.ExecutionJobGitImage, true),
		integerDefinition(KeyExecutionJobTTLSeconds, "执行任务", "任务保留时间", "Kubernetes Job 完成后的保留秒数", sohaapi.RuntimeConfigApplyModeHot, 3600, options.ExecutionJobTTLSeconds, 1, 604800),
		durationDefinition(KeyMCPDefaultTimeout, "连接", "MCP 默认超时", "连接 Agent 与 MCP 服务时使用的默认请求超时", sohaapi.RuntimeConfigApplyModeHot, 10*time.Second, options.MCPDefaultTimeout),
		durationDefinition(KeyAIGatewayDefaultTimeout, "AI Gateway", "默认请求超时", "非流式中继请求的总超时时间", sohaapi.RuntimeConfigApplyModeRestart, 120*time.Second, options.AIGatewayDefaultTimeout),
		durationDefinition(KeyAIGatewayStreamTimeout, "AI Gateway", "流式请求超时", "流式中继请求的总超时时间", sohaapi.RuntimeConfigApplyModeRestart, 300*time.Second, options.AIGatewayStreamTimeout),
		durationDefinition(KeyAIGatewayFirstByteTimeout, "AI Gateway", "首字节超时", "等待上游返回首字节的最长时间", sohaapi.RuntimeConfigApplyModeRestart, 30*time.Second, options.AIGatewayFirstByteTimeout),
		durationDefinition(KeyAIGatewayStreamIdleTimeout, "AI Gateway", "流空闲超时", "流式连接无数据传输时的最长等待时间", sohaapi.RuntimeConfigApplyModeRestart, 60*time.Second, options.AIGatewayStreamIdleTimeout),
		booleanDefinitionWithDescription(KeyAIGatewayHealthCheckEnabled, "AI Gateway", "上游健康检查", "是否定期检查中继上游健康状态", sohaapi.RuntimeConfigApplyModeRestart, options.AIGatewayHealthCheckEnabled),
		durationDefinition(KeyAIGatewayHealthCheckInterval, "AI Gateway", "健康检查间隔", "中继上游健康检查周期", sohaapi.RuntimeConfigApplyModeRestart, time.Minute, options.AIGatewayHealthCheckInterval),
		integerDefinition(KeyAIGatewayMaxRequestBodyMB, "AI Gateway", "最大请求体", "中继请求体大小上限（MB）", sohaapi.RuntimeConfigApplyModeRestart, 32, options.AIGatewayMaxRequestBodyMB, 1, 1024),
		booleanDefinitionWithDescription(KeyAIGatewayIncludeStreamUsage, "AI Gateway", "流式响应 Usage", "在 OpenAI 流式响应中请求返回 usage 数据", sohaapi.RuntimeConfigApplyModeRestart, options.AIGatewayIncludeStreamUsage),
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
		EnvironmentVariable: environmentKey(key), Editable: true,
	}
}

func booleanDefinitionWithDescription(key, category, label, description string, mode sohaapi.RuntimeConfigApplyMode, baseline bool) Definition {
	definition := booleanDefinition(key, category, label, mode, baseline)
	definition.Description = description
	return definition
}

func integerDefinition(key, category, label, description string, mode sohaapi.RuntimeConfigApplyMode, defaultValue, baseline, minimum, maximum int) Definition {
	if baseline <= 0 {
		baseline = defaultValue
	}
	return Definition{
		Key: key, Category: category, Label: label, Description: description,
		ValueType: sohaapi.RuntimeConfigValueTypeInteger, ApplyMode: mode,
		DefaultValue: int64(defaultValue), BaselineValue: int64(baseline),
		EnvironmentVariable: environmentKey(key), Editable: true,
		Validate: func(value any) error {
			integer, ok := value.(int64)
			if !ok || integer < int64(minimum) || integer > int64(maximum) {
				return fmt.Errorf("must be between %d and %d", minimum, maximum)
			}
			return nil
		},
	}
}

func durationDefinition(key, category, label, description string, mode sohaapi.RuntimeConfigApplyMode, defaultValue, baseline time.Duration) Definition {
	if baseline <= 0 {
		baseline = defaultValue
	}
	return Definition{
		Key: key, Category: category, Label: label, Description: description,
		ValueType: sohaapi.RuntimeConfigValueTypeDuration, ApplyMode: mode,
		DefaultValue: defaultValue.String(), BaselineValue: baseline.String(),
		EnvironmentVariable: environmentKey(key), Editable: true,
		Validate: func(value any) error {
			duration, err := time.ParseDuration(fmt.Sprint(value))
			if err != nil || duration <= 0 {
				return fmt.Errorf("must be a positive duration")
			}
			return nil
		},
	}
}

func stringDefinition(key, category, label, description string, mode sohaapi.RuntimeConfigApplyMode, defaultValue, baseline string, required bool) Definition {
	if strings.TrimSpace(baseline) == "" {
		baseline = defaultValue
	}
	definition := Definition{
		Key: key, Category: category, Label: label, Description: description,
		ValueType: sohaapi.RuntimeConfigValueTypeString, ApplyMode: mode,
		DefaultValue: defaultValue, BaselineValue: strings.TrimSpace(baseline),
		EnvironmentVariable: environmentKey(key), Editable: true,
	}
	if required {
		definition.Validate = func(value any) error {
			if strings.TrimSpace(fmt.Sprint(value)) == "" {
				return fmt.Errorf("must not be empty")
			}
			return nil
		}
	}
	return definition
}

func environmentKey(key string) string {
	return "SOHA_" + strings.ToUpper(strings.NewReplacer(".", "_", "-", "_").Replace(key))
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

func (s Snapshot) Definition(key string) (Definition, bool) {
	if s.registry == nil {
		return Definition{}, false
	}
	return s.registry.Definition(key)
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

func (s Snapshot) Int(key string, fallback int) int {
	value, ok := s.Value(key)
	if !ok {
		return fallback
	}
	switch typed := value.(type) {
	case int:
		return typed
	case int64:
		return int(typed)
	case float64:
		return int(typed)
	default:
		return fallback
	}
}

func (s Snapshot) Duration(key string, fallback time.Duration) time.Duration {
	value := s.String(key, "")
	duration, err := time.ParseDuration(value)
	if err != nil || duration <= 0 {
		return fallback
	}
	return duration
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
