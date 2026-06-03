export type GatewayTimeRangeValue = readonly [
  { toISOString: () => string } | null,
  { toISOString: () => string } | null,
] | null

export type RiskLevel = 'read' | 'analyze' | 'mutate' | 'execute' | 'high'
export type GatewayEffect = 'allow' | 'deny'
export type GatewaySectionKey = 'overview' | 'manifest' | 'clients' | 'tokens' | 'governance' | 'call-logs'
export type GatewayTabKey = 'manifest' | 'clients' | 'tokens' | 'service-accounts' | 'grants' | 'policies' | 'bindings' | 'governance' | 'approvals' | 'audit'

export const gatewayMenuMeta: Record<GatewayTabKey, { description: string; title: string }> = {
  manifest: {
    title: 'Manifest',
    description: '当前身份可见的 MCP tools、resources、prompts 和 skills。',
  },
  clients: {
    title: 'AI Clients',
    description: '外部 IDE、CI、Agent 平台和 MCP 客户端注册入口。',
  },
  tokens: {
    title: 'Tokens',
    description: '用户 login key、服务账号 token 与一次性明文创建流程。',
  },
  'service-accounts': {
    title: 'Service Accounts',
    description: '服务账号、服务账号 token 和自动化调用身份。',
  },
  grants: {
    title: 'Tool Grants',
    description: '按主体、角色和客户端收窄可调用工具。',
  },
  policies: {
    title: 'Access Policies',
    description: 'Gateway allow/deny、审批、限流、预算和脱敏治理策略。',
  },
  bindings: {
    title: 'Skill Bindings',
    description: '约束 skill 能暴露的能力引用，不扩权。',
  },
  governance: {
    title: 'Governance',
    description: '健康检查、风险 finding、policy coverage 和治理建议。',
  },
  approvals: {
    title: 'Approvals',
    description: '高风险工具审批、决策轨迹和 workflow 关联。',
  },
  audit: {
    title: '调用日志',
    description: '按调用者、入口 client、调用内容、结果和 request 追踪 Gateway 调用。',
  },
}

export const gatewaySectionMeta: Record<GatewaySectionKey, { description: string; title: string }> = {
  overview: {
    title: '概览',
    description: '汇总 AI Gateway 的能力入口、身份对象、授权策略和治理状态。',
  },
  manifest: {
    title: '能力清单',
    description: '查看当前身份可见的 MCP tools、resources、prompts 和 skills。',
  },
  clients: {
    title: 'AI Clients',
    description: '管理外部 IDE、CI、Agent 平台和 MCP 客户端注册入口。',
  },
  tokens: {
    title: 'Tokens',
    description: '聚合管理用户 login key、service accounts 与自动化调用 token。',
  },
  governance: {
    title: 'Governance',
    description: '管理 tool grants、access policies、skill bindings 与审批治理。',
  },
  'call-logs': {
    title: '调用日志',
    description: '查看谁通过 AI Gateway 调用了什么能力，以及每次调用的结果和上下文。',
  },
}

export const gatewayTabSectionMap: Record<GatewayTabKey, GatewaySectionKey> = {
  manifest: 'manifest',
  clients: 'clients',
  tokens: 'tokens',
  'service-accounts': 'tokens',
  grants: 'governance',
  policies: 'governance',
  bindings: 'governance',
  governance: 'governance',
  approvals: 'governance',
  audit: 'call-logs',
}

export const gatewaySectionPaths: Record<GatewaySectionKey, string> = {
  overview: '/ai-gateway/overview',
  manifest: '/ai-gateway/manifest',
  clients: '/ai-gateway/clients',
  tokens: '/ai-gateway/tokens',
  governance: '/ai-gateway/governance',
  'call-logs': '/ai-gateway/call-logs',
}

export function gatewaySectionFromPath(pathname: string): GatewaySectionKey {
  if (pathname.startsWith('/ai-gateway/manifest')) return 'manifest'
  if (pathname.startsWith('/ai-gateway/clients')) return 'clients'
  if (pathname.startsWith('/ai-gateway/tokens')) return 'tokens'
  if (pathname.startsWith('/ai-gateway/governance')) return 'governance'
  if (pathname.startsWith('/ai-gateway/call-logs')) return 'call-logs'
  return 'overview'
}

export function normalizeGatewayTabKey(value: string | null): GatewayTabKey | null {
  if (!value) return null
  return Object.prototype.hasOwnProperty.call(gatewayTabSectionMap, value) ? (value as GatewayTabKey) : null
}

export function defaultGatewayTabForSection(section: GatewaySectionKey, focusedApprovalRequestId: string): GatewayTabKey {
  if (focusedApprovalRequestId) return 'approvals'
  if (section === 'manifest') return 'manifest'
  if (section === 'clients') return 'clients'
  if (section === 'tokens') return 'tokens'
  if (section === 'call-logs') return 'audit'
  return 'governance'
}

export function gatewayTabBelongsToSection(tab: GatewayTabKey, section: GatewaySectionKey) {
  return gatewayTabSectionMap[tab] === section
}

export interface AIClient {
  id: string
  name: string
  kind: string
  status: string
  redirectUris: string[]
  allowedOrigins: string[]
  metadata?: Record<string, unknown>
  createdBy: string
  createdAt: string
  updatedAt: string
}

export interface PersonalAccessToken {
  id: string
  userId: string
  name: string
  tokenPrefix: string
  scopes: string[]
  permissionKeys: string[]
  expiresAt?: string
  lastUsedAt?: string
  revokedAt?: string
  createdAt: string
}

export interface CreatedPersonalAccessToken {
  token: PersonalAccessToken
  value: string
}

export interface ServiceAccount {
  id: string
  name: string
  description?: string
  status: string
  ownerUserId?: string
  roleIds: string[]
  teamIds: string[]
  scopeGrantIds: string[]
  createdAt: string
  updatedAt: string
}

export interface ServiceAccountToken {
  id: string
  serviceAccountId: string
  name: string
  tokenPrefix: string
  scopes: string[]
  permissionKeys: string[]
  expiresAt?: string
  lastUsedAt?: string
  revokedAt?: string
  createdAt?: string
  updatedAt?: string
}

export interface CreatedServiceAccountToken {
  token: ServiceAccountToken
  value: string
}

export interface ToolGrant {
  id: string
  subjectType: string
  subjectId: string
  aiClientId?: string
  toolName: string
  effect: GatewayEffect
  riskLevel: RiskLevel
  permissionKeys?: string[]
  resourceScopes?: Record<string, unknown>
  requiresApproval: boolean
  expiresAt?: string
  createdAt: string
}

export interface AccessPolicy {
  id: string
  name: string
  description?: string
  enabled: boolean
  subjectType: string
  subjectId: string
  aiClientId?: string
  effect: GatewayEffect
  toolPatterns?: string[]
  skillIds?: string[]
  resourceScopes?: Record<string, unknown>
  riskLevels?: RiskLevel[]
  approvalPolicy?: Record<string, unknown>
  conditions?: Record<string, unknown>
  createdAt: string
  updatedAt: string
}

export interface SkillBinding {
  id: string
  subjectType: string
  subjectId: string
  aiClientId?: string
  skillId: string
  capabilityRefs?: string[]
  enabled: boolean
  metadata?: Record<string, unknown>
  createdAt: string
  updatedAt: string
}

export interface GatewayTool {
  name: string
  title: string
  domain: string
  action: string
  riskLevel: RiskLevel
  permissionKeys: string[]
  requiredScopes?: string[]
  requiresApproval: boolean
}

export interface GatewaySkill {
  id: string
  name: string
  category: string
  capabilityRefs?: string[]
  requiredScopes?: string[]
}

export interface GatewayManifest {
  name: string
  version: string
  generatedAt: string
  principal?: {
    userId?: string
    userName?: string
    roles?: string[]
    teams?: string[]
  }
  caller?: {
    aiClientId?: string
    aiClientName?: string
    skillId?: string
    subjectType?: string
    subjectId?: string
  }
  permissionKeys: string[]
  tools: GatewayTool[]
  skills?: GatewaySkill[]
  resources?: Array<{ name: string; description: string; requiredScopes?: string[] }>
  prompts?: Array<{ name: string; description: string; requiredScopes?: string[] }>
  summary: {
    toolCount: number
    resourceCount: number
    promptCount: number
    skillCount: number
    deniedCount: number
  }
}

export interface GatewayAuditLog {
  id: string
  actorType: string
  actorId: string
  actorName?: string
  aiClientId?: string
  aiClientName?: string
  skillId?: string
  toolName?: string
  riskLevel?: RiskLevel
  resourceScope?: Record<string, unknown>
  action: string
  result: string
  summary: string
  requestId?: string
  sourceIp?: string
  metadata?: Record<string, unknown>
  createdAt: string
}

export interface ApprovalRequest {
  id: string
  status: string
  strategy: string
  policyId?: string
  approvalPolicyRef?: string
  actorType: string
  actorId: string
  actorName?: string
  actorRoles?: string[]
  actorTeams?: string[]
  aiClientId?: string
  aiClientName?: string
  skillId?: string
  toolName: string
  riskLevel: RiskLevel
  requiresApproval: boolean
  resourceScope?: Record<string, unknown>
  toolInput?: Record<string, unknown>
  relatedIds?: Record<string, unknown>
  output?: unknown
  summary: string
  requestId?: string
  sourceIp?: string
  decidedBy?: string
  decidedByName?: string
  decidedAt?: string
  decisionComment?: string
  expiresAt?: string
  createdAt: string
  updatedAt: string
}

export interface ApprovalDecisionResult {
  request: ApprovalRequest
  invocation?: {
    result: string
    output?: unknown
    relatedIds?: Record<string, unknown>
  }
}

export interface GovernanceMetricCount {
  key: string
  count: number
}

export interface GovernanceHealthCheck {
  name: string
  status: string
  message: string
  count?: number
}

export interface GovernanceHealth {
  status: string
  message: string
  checks?: GovernanceHealthCheck[]
}

export interface GovernanceMetrics {
  totalCalls: number
  successCount: number
  denyCount: number
  failureCount: number
  pendingApprovalCount: number
  dryRunCount: number
  riskCounts?: Record<string, number>
  topTools?: GovernanceMetricCount[]
  topAiClients?: GovernanceMetricCount[]
  topActors?: GovernanceMetricCount[]
  recentResultBreakdown?: Record<string, number>
  recentActionBreakdown?: Record<string, number>
}

export interface GovernanceRedactionSummary {
  totalMatches: number
  auditsWithRedaction: number
  inputAudits: number
  outputAudits: number
  fieldMatches: number
  sensitiveKeyMatches: number
  sensitiveTextMatches: number
  valuePatternMatches: number
  secretClassifierMatches: number
  structuredSecretMatches: number
  topTargets?: GovernanceMetricCount[]
  topFieldPaths?: GovernanceMetricCount[]
  topMatchTypes?: GovernanceMetricCount[]
  topClassifiers?: GovernanceMetricCount[]
  topPolicies?: GovernanceMetricCount[]
  topTools?: GovernanceMetricCount[]
}

export interface GovernanceTokenCounts {
  total: number
  active: number
  revoked: number
  expired: number
  expiringSoon: number
  stale: number
  neverUsed: number
}

export interface GovernanceTokenFinding {
  kind: string
  id: string
  name: string
  ownerId?: string
  tokenPrefix: string
  severity: string
  message: string
  expiresAt?: string
  lastUsedAt?: string
  daysUntilDue?: number
  staleDays?: number
}

export interface GovernanceTokenSummary {
  personalAccessTokens: GovernanceTokenCounts
  serviceAccountTokens: GovernanceTokenCounts
  expiringSoon?: GovernanceTokenFinding[]
  expiredActive?: GovernanceTokenFinding[]
  stale?: GovernanceTokenFinding[]
  neverUsed?: GovernanceTokenFinding[]
  lastUsedTrackingState: string
}

export interface GovernanceClientSummary {
  total: number
  active: number
  disabled: number
  pendingApproval: number
  registrationApproval: string
  pendingApprovalClientIds?: string[]
}

export interface GovernanceApprovalSummary {
  pending: number
  dueSoon: number
  stalePending: number
  overdue: number
  oldestPendingHours?: number
  oldestPendingRequestId?: string
  nextDueAt?: string
  nextDueRequestId?: string
  dueSoonRequestIds?: string[]
  stalePendingRequestIds?: string[]
  overdueRequestIds?: string[]
}

export interface GovernancePolicyCoverage {
  accessPolicies: number
  toolGrants: number
  skillBindings: number
  activeAccessPolicies: number
  activeToolGrants: number
  activeSkillBindings: number
  budgetPolicies: number
  rateLimitPolicies: number
  redactionPolicies: number
  resourceScopedAccessPolicies: number
  resourceScopedToolGrants: number
  budgetState: string
  rateLimitState: string
  redactionPolicyState: string
  resourceScopeState: string
}

export interface GovernanceFinding {
  type: string
  severity: string
  summary: string
  count?: number
  actorType?: string
  actorId?: string
  subjectType?: string
  subjectId?: string
  aiClientId?: string
  policyId?: string
  approvalRequestId?: string
  grantId?: string
  toolName?: string
  riskLevel?: RiskLevel
}

export interface GovernanceQueueRow {
  key: 'due_soon' | 'stale' | 'overdue' | 'pending_clients'
  label: string
  count: number
  refs: string[]
}

export interface GovernanceTokenFindingRow extends GovernanceTokenFinding {
  key: string
  category: 'expiredActive' | 'expiringSoon' | 'stale' | 'neverUsed'
  categoryLabel: string
}

export interface GovernanceCoverageRow {
  key: 'access_policies' | 'tool_grants' | 'skill_bindings' | 'budget' | 'rate_limit' | 'redaction' | 'resource_scopes'
  label: string
  state: string
  configured: number
  total: number
}

export interface GovernanceRedactionRow {
  key: 'targets' | 'match_types' | 'classifiers' | 'field_paths' | 'policies' | 'tools'
  label: string
  count: number
  items: GovernanceMetricCount[]
  target?: GovernanceDrilldownTarget
}

export interface GovernanceDrilldownTarget {
  tab: GatewayTabKey
  approvalFilters?: Partial<ApprovalFilterState>
  auditFilters?: Partial<AuditFilterState>
  clientFilter?: string
  tokenFilter?: string
  serviceTokenFilter?: string
  policyFilter?: string
  grantFilter?: string
  serviceTokenRevokeId?: string
  policyDraft?: Record<string, unknown>
}

export interface GovernanceDrilldownAction {
  label: string
  target: GovernanceDrilldownTarget
}

export interface GovernanceRecommendationAction {
  type: string
  severity: string
  summary: string
  action: string
  targetKind?: string
  targetId?: string
  refs?: string[]
  count?: number
  metadata?: Record<string, unknown>
}

export interface AuditFilterState {
  actor: string
  aiClientId: string
  toolName: string
  action: string
  riskLevel: string
  result: string
  from: string
  to: string
}

export interface ApprovalFilterState {
  id: string
  status: string
  actor: string
  aiClientId: string
  toolName: string
  riskLevel: string
  strategy: string
  from: string
  to: string
}

export interface GovernanceStatus {
  generatedAt: string
  windowHours: number
  health: GovernanceHealth
  metrics: GovernanceMetrics
  tokens: GovernanceTokenSummary
  clients: GovernanceClientSummary
  approvals: GovernanceApprovalSummary
  policyCoverage: GovernancePolicyCoverage
  redaction?: GovernanceRedactionSummary
  anomalies?: GovernanceFinding[]
  recommendations?: string[]
  recommendationActions?: GovernanceRecommendationAction[]
  metadata?: Record<string, unknown>
}

export type DrawerKind =
  | 'ai-client'
  | 'personal-token'
  | 'service-account'
  | 'service-token'
  | 'service-token-revoke'
  | 'tool-grant'
  | 'access-policy'
  | 'skill-binding'

export interface DrawerState {
  kind: DrawerKind
  record?: AIClient | ServiceAccount | ToolGrant | AccessPolicy | SkillBinding
  initialValues?: Record<string, unknown>
}

export const subjectTypeOptions = [
  { label: '用户', value: 'user' },
  { label: '服务账号', value: 'service_account' },
  { label: '角色', value: 'role' },
  { label: '组织', value: 'team' },
  { label: 'AI Client', value: 'ai_client' },
]

export const riskLevelOptions = [
  { label: 'read', value: 'read' },
  { label: 'analyze', value: 'analyze' },
  { label: 'mutate', value: 'mutate' },
  { label: 'execute', value: 'execute' },
  { label: 'high', value: 'high' },
]

export const effectOptions = [
  { label: '允许', value: 'allow' },
  { label: '拒绝', value: 'deny' },
]

export const approvalStrategyOptions = [
  { label: '不强制', value: 'none' },
  { label: '直接允许', value: 'allow' },
  { label: '直接拒绝', value: 'deny' },
  { label: '要求审批', value: 'require_approval' },
  { label: '人工确认', value: 'require_human_confirm' },
  { label: '仅 dry-run', value: 'dry_run_only' },
]

export const approvalRequestStrategyOptions = approvalStrategyOptions.filter((item) => ['require_approval', 'require_human_confirm'].includes(item.value))

export const approvalRoutingModeOptions = [
  { label: '会签 all', value: 'all' },
  { label: '或签 any', value: 'any' },
]

export const approvalStatusOptions = [
  { label: '待处理', value: 'pending' },
  { label: '已批准', value: 'approved' },
  { label: '已执行', value: 'executed' },
  { label: '已拒绝', value: 'rejected' },
  { label: '已取消', value: 'canceled' },
  { label: '已超时', value: 'timeout' },
  { label: '执行失败', value: 'failed' },
]

export const auditActionOptions = [
  { label: '工具调用', value: 'ai_gateway.tool.invoke' },
  { label: '资源读取', value: 'ai_gateway.resource.read' },
  { label: 'Prompt 获取', value: 'ai_gateway.prompt.get' },
  { label: '审批请求', value: 'ai_gateway.approval.request' },
  { label: '审批决策', value: 'ai_gateway.approval.decision' },
  { label: '审批超时', value: 'ai_gateway.approval.timeout' },
]

export const auditResultOptions = [
  'success',
  'failure',
  'deny',
  'pending',
  'pending_approval',
  'pending_human_confirm',
  'dry_run',
  'approved',
  'executed',
  'rejected',
  'canceled',
  'timeout',
].map((value) => ({ label: value, value }))

export const governanceWindowOptions = [
  { label: '1h', value: '1' },
  { label: '6h', value: '6' },
  { label: '24h', value: '24' },
  { label: '48h', value: '48' },
  { label: '7d', value: '168' },
]

export const clientKindOptions = [
  { label: 'AI Coding', value: 'ai_coding' },
  { label: 'MCP Client', value: 'mcp_client' },
  { label: 'CI Agent', value: 'ci_agent' },
  { label: 'Enterprise Agent', value: 'enterprise_agent' },
]

export const statusOptions = [
  { label: 'active', value: 'active' },
  { label: 'disabled', value: 'disabled' },
]

export const gatewayLimitScopeOptions = [
  { label: 'Actor', value: 'actor' },
  { label: 'AI client', value: 'client' },
  { label: 'Actor + client', value: 'actor_client' },
  { label: 'Actor + tool', value: 'actor_tool' },
  { label: 'Client + tool', value: 'client_tool' },
  { label: 'Actor + client + tool', value: 'actor_client_tool' },
  { label: 'Global', value: 'global' },
]

export const rateLimitModeOptions = [
  { label: 'Fixed window', value: 'counter' },
  { label: 'Sliding window', value: 'sliding_window' },
  { label: 'GCRA / token bucket', value: 'gcra' },
]

export const redactionModeOptions = [
  { label: '不启用', value: 'none' },
  { label: 'Strict deny', value: 'strict' },
  { label: 'Sanitize', value: 'sanitize' },
  { label: 'Mask', value: 'mask' },
  { label: 'Redact', value: 'redact' },
]

export const redactionTargetOptions = [
  { label: 'Input', value: 'input' },
  { label: 'Output', value: 'output' },
  { label: 'Both', value: 'both' },
]

export const gatewaySecretTypeOptions = [
  'default',
  'github',
  'gitlab',
  'openai',
  'anthropic',
  'google_api_key',
  'huggingface',
  'cohere',
  'mistral',
  'deepseek',
  'groq',
  'together',
  'replicate',
  'langsmith',
  'pinecone',
  'xai',
  'perplexity',
  'tavily',
  'langfuse',
  'qdrant',
  'wandb',
  'linear',
  'openrouter',
  'fireworks',
  'voyage',
  'brave_search',
  'serpapi',
  'browserbase',
  'exa',
  'jina',
  'unstructured',
  'llama_cloud',
  'helicone',
  'dashscope',
  'moonshot',
  'zhipu',
  'siliconflow',
  'hunyuan',
  'qianfan',
  'volcengine',
  'grafana',
  'sentry',
  'newrelic',
  'azure_openai',
  'azure_devops',
  'datadog',
  'pagerduty',
  'posthog',
  'splunk',
  'elastic',
  'terraform',
  'npm',
  'stripe',
  'slack',
  'jwt',
  'aws',
  'private_key',
  'kubernetes_secret',
  'kubeconfig',
  'docker_config',
  'gcp_service_account',
  'aws_credentials',
].map((value) => ({ label: value, value }))

export const scopeFieldDefs = [
  { name: 'scopeBusinessLineIds', label: '范围 Key', key: 'businessLineIds' },
  { name: 'scopeApplicationIds', label: '应用', key: 'applicationIds' },
  { name: 'scopeApplicationEnvironmentIds', label: '应用环境绑定', key: 'applicationEnvironmentIds' },
  { name: 'scopeEnvironmentIds', label: '环境 Tag', key: 'environmentIds' },
  { name: 'scopeClusterIds', label: '集群', key: 'clusterIds' },
  { name: 'scopeNamespaces', label: '命名空间', key: 'namespaces' },
  { name: 'scopeReleaseBundleIds', label: '版本包', key: 'releaseBundleIds' },
  { name: 'scopeExecutionTaskIds', label: '执行任务', key: 'executionTaskIds' },
] as const

export function sortedRecordEntries(values?: Record<string, number>) {
  return Object.entries(values ?? {})
    .filter(([, value]) => Number.isFinite(value) && value > 0)
    .sort(([leftKey, leftValue], [rightKey, rightValue]) => rightValue - leftValue || leftKey.localeCompare(rightKey))
}

export function governanceRiskCountTags(values?: Record<string, number>) {
  return sortedRecordEntries(values).map(([key, count]) => `${key}:${count}`)
}

export function stringifyPayload(value: unknown) {
  return JSON.stringify(value ?? {}, null, 2)
}

export function scopeValuesFromRecord(scopes?: Record<string, unknown>) {
  const out: Record<string, string[]> = {}
  scopeFieldDefs.forEach((field) => {
    const singularKey = field.key.replace(/s$/, '')
    const value = scopes?.[field.key] ?? scopes?.[singularKey]
    if (Array.isArray(value)) {
      out[field.name] = value.map((item) => String(item)).filter(Boolean)
    } else if (typeof value === 'string' && value) {
      out[field.name] = [value]
    }
  })
  return out
}

export function approvalModeFromPolicy(policy?: Record<string, unknown>) {
  const raw = String(policy?.strategy ?? policy?.mode ?? policy?.approval ?? policy?.state ?? '').trim()
  if (raw) {
    const normalized = raw.toLowerCase().replace(/[-\s]+/g, '_')
    if (approvalStrategyOptions.some((item) => item.value === normalized)) {
      return normalized
    }
    if (normalized === 'required' || normalized === 'approval_required') {
      return 'require_approval'
    }
  }
  if (policy?.dryRunOnly === true) return 'dry_run_only'
  if (policy?.requiresHumanConfirm === true || policy?.humanConfirmRequired === true) return 'require_human_confirm'
  if (policy?.requiresApproval === true) return 'require_approval'
  return 'none'
}

export function approvalRoutingEnabled(strategy: unknown) {
  return ['require_approval', 'require_human_confirm'].includes(String(strategy ?? '').trim())
}

export function asRecord(value: unknown) {
  if (value && typeof value === 'object' && !Array.isArray(value)) {
    return value as Record<string, unknown>
  }
  return {}
}

export function firstValue(record: Record<string, unknown>, ...keys: string[]) {
  for (const key of keys) {
    const value = record[key]
    if (value !== undefined && value !== null && value !== '') {
      return value
    }
  }
  return undefined
}

export function firstNumber(record: Record<string, unknown>, ...keys: string[]) {
  const value = firstValue(record, ...keys)
  if (typeof value === 'number' && Number.isFinite(value)) return value
  if (typeof value === 'string' && value.trim()) {
    const parsed = Number(value)
    if (Number.isFinite(parsed)) return parsed
  }
  return undefined
}

export function firstString(record: Record<string, unknown>, ...keys: string[]) {
  const value = firstValue(record, ...keys)
  if (typeof value === 'string') return value.trim()
  if (typeof value === 'number' || typeof value === 'boolean') return String(value)
  return undefined
}

export function firstStringList(record: Record<string, unknown>, ...keys: string[]) {
  const value = firstValue(record, ...keys)
  if (Array.isArray(value)) {
    return value.map((item) => String(item).trim()).filter(Boolean)
  }
  if (typeof value === 'string' && value.trim()) {
    return value.split(/[,\n]/).map((item) => item.trim()).filter(Boolean)
  }
  return undefined
}

export function normalizeRateLimitMode(value?: string) {
  const normalized = String(value ?? '').trim().toLowerCase().replace(/[-\s]+/g, '_')
  if (['gcra', 'token_bucket', 'tokenbucket', 'leaky_bucket', 'leakybucket', 'strict', 'smooth'].includes(normalized)) {
    return 'gcra'
  }
  if (['sliding_window', 'slidingwindow', 'rolling_window', 'rollingwindow', 'audit_window', 'auditwindow'].includes(normalized)) {
    return 'sliding_window'
  }
  if (['fixed_window', 'fixedwindow', 'window', 'counter'].includes(normalized)) {
    return 'counter'
  }
  return normalized || undefined
}

export function approvalPolicyValues(policy?: Record<string, unknown>) {
  const source = asRecord(policy)
  const out: Record<string, unknown> = { ...source }
  ;['routing', 'approvalRouting', 'approvers', 'candidates', 'candidateApprovers'].forEach((key) => {
    Object.assign(out, asRecord(source[key]))
  })
  return out
}

export function accessPolicyFormValuesFromRecord(record: AccessPolicy) {
  const conditions = asRecord(record.conditions)
  const approvalPolicy = approvalPolicyValues(record.approvalPolicy)
  const changeWindow = asRecord(firstValue(approvalPolicy, 'changeWindow', 'approvalWindow', 'window'))
  const changeWindowValues = { ...approvalPolicy, ...changeWindow }
  const rateLimit = asRecord(firstValue(conditions, 'rateLimit', 'rate_limit', 'rateLimits'))
  const budget = asRecord(firstValue(conditions, 'budget', 'budgets', 'budgetPolicy'))
  const redactionPolicy = asRecord(firstValue(conditions, 'redactionPolicy', 'redaction', 'sensitiveDataRedaction'))
  const outputRedactionPolicy = asRecord(conditions.outputRedactionPolicy)
  const redactionMode = firstString(redactionPolicy, 'mode', 'strategy', 'redactionMode', 'action')

  return {
    ...record,
    toolPatterns: record.toolPatterns,
    skillIds: record.skillIds,
    riskLevels: record.riskLevels,
    approvalMode: approvalModeFromPolicy(record.approvalPolicy),
    approvalPolicyRef: firstString(approvalPolicy, 'approvalPolicyRef', 'approvalPolicyId', 'deliveryApprovalPolicyId', 'policyRef', 'policyKey'),
    approvalRoutingMode: firstString(approvalPolicy, 'approvalMode', 'approvalType', 'quorumMode', 'decisionMode') ?? 'all',
    approvalApproverUsers: firstStringList(approvalPolicy, 'approverUsers', 'approverUserIds', 'candidateUsers', 'candidateUserIds', 'approvalUsers', 'approvalUserIds', 'userIds', 'users') ?? [],
    approvalApproverRoles: firstStringList(approvalPolicy, 'approverRoles', 'candidateRoles', 'approvalRoles', 'roles', 'roleIds') ?? [],
    approvalApproverTeams: firstStringList(approvalPolicy, 'approverTeams', 'candidateTeams', 'approvalTeams', 'teams', 'teamIds', 'groups', 'groupIds') ?? [],
    approvalOnCallRef: firstString(approvalPolicy, 'onCallRef', 'oncallRef', 'onCall', 'oncall', 'dutyRef', 'routeRef', 'scheduleRef'),
    approvalRequiredApprovals: firstNumber(approvalPolicy, 'requiredApprovals', 'minApprovals', 'approvalQuorum', 'quorum', 'minApproverCount'),
    approvalChangeWindowStartsAt: firstString(changeWindowValues, 'startsAt', 'startAt', 'start', 'from', 'notBefore', 'beginAt'),
    approvalChangeWindowEndsAt: firstString(changeWindowValues, 'endsAt', 'endAt', 'end', 'to', 'until', 'notAfter'),
    approvalChangeWindowTimezone: firstString(changeWindowValues, 'timezone', 'timeZone', 'tz'),
    ...scopeValuesFromRecord(record.resourceScopes),
    rateLimitEnabled: Object.keys(rateLimit).length > 0,
    rateLimitMode: normalizeRateLimitMode(firstString(rateLimit, 'mode', 'algorithm', 'strategy')) ?? 'counter',
    rateLimitScope: firstString(rateLimit, 'scope', 'limitScope') ?? 'actor_client_tool',
    rateLimitMaxCallsPerMinute: firstNumber(rateLimit, 'maxCallsPerMinute', 'maxInvocationsPerMinute', 'callsPerMinute', 'rpm'),
    rateLimitMaxCallsPerHour: firstNumber(rateLimit, 'maxCallsPerHour', 'maxInvocationsPerHour', 'callsPerHour', 'rph'),
    rateLimitBurst: firstNumber(rateLimit, 'burst', 'burstSize', 'capacity', 'bucketSize', 'maxBurst'),
    budgetEnabled: Object.keys(budget).length > 0,
    budgetScope: firstString(budget, 'scope', 'limitScope') ?? 'actor_client',
    budgetMaxCallsPerDay: firstNumber(budget, 'maxCallsPerDay', 'maxInvocationsPerDay', 'maxDailyCalls', 'dailyCalls', 'dailyBudget'),
    budgetMaxTokensPerDay: firstNumber(budget, 'maxTokensPerDay', 'dailyTokens', 'dailyTokenBudget'),
    budgetMaxCostPerDay: firstNumber(budget, 'maxCostPerDay', 'dailyCost', 'dailyCostBudget'),
    redactionEnabled: Object.keys(redactionPolicy).length > 0 || Object.keys(outputRedactionPolicy).length > 0,
    redactionMode: redactionMode ?? (Object.keys(redactionPolicy).length > 0 ? 'sanitize' : 'none'),
    redactionTarget: firstString(redactionPolicy, 'target', 'appliesTo', 'direction') ?? (Object.keys(outputRedactionPolicy).length > 0 ? 'both' : 'input'),
    redactionFields: firstStringList(redactionPolicy, 'fields', 'field', 'paths', 'path', 'redactFields', 'maskFields', 'sensitiveFields') ?? [],
    redactionAllowFields: firstStringList(redactionPolicy, 'allowFields', 'allowedFields', 'allowlist', 'fieldAllowList', 'fieldAllowlist') ?? [],
    redactionSecretTypes: firstStringList(redactionPolicy, 'secretTypes', 'secretType', 'classifiers', 'classifier', 'detect', 'detectSecretTypes', 'secretClassifiers') ?? [],
    redactionValuePatterns: firstStringList(redactionPolicy, 'valuePatterns', 'valuePattern', 'valueRegex', 'valueRegexes', 'regex', 'regexes', 'matchValues', 'matchPatterns') ?? [],
    redactionReplacement: firstString(redactionPolicy, 'replacement', 'replacementText', 'redactionValue', 'maskValue') ?? '[REDACTED]',
    redactionPreserveFormat: Boolean(firstValue(redactionPolicy, 'preserveFormat', 'formatPreserving', 'preserveShape')),
    outputRedactionFields: firstStringList(outputRedactionPolicy, 'fields', 'field', 'paths', 'path', 'redactFields', 'maskFields', 'sensitiveFields') ?? [],
    outputRedactionSecretTypes: firstStringList(outputRedactionPolicy, 'secretTypes', 'secretType', 'classifiers', 'classifier', 'detect', 'detectSecretTypes', 'secretClassifiers') ?? [],
    outputRedactionValuePatterns: firstStringList(outputRedactionPolicy, 'valuePatterns', 'valuePattern', 'valueRegex', 'valueRegexes', 'regex', 'regexes', 'matchValues', 'matchPatterns') ?? [],
    outputRedactionReplacement: firstString(outputRedactionPolicy, 'replacement', 'replacementText', 'redactionValue', 'maskValue') ?? '[REDACTED]',
    outputRedactionPreserveFormat: Boolean(firstValue(outputRedactionPolicy, 'preserveFormat', 'formatPreserving', 'preserveShape')),
  }
}

export function positiveNumber(value: unknown) {
  if (typeof value === 'number' && Number.isFinite(value) && value > 0) return value
  if (typeof value === 'string' && value.trim()) {
    const parsed = Number(value)
    if (Number.isFinite(parsed) && parsed > 0) return parsed
  }
  return undefined
}

export function normalizedStringList(value: unknown) {
  if (!Array.isArray(value)) return []
  return value.map((item) => String(item).trim()).filter(Boolean)
}

export function compactObject<T extends Record<string, unknown>>(value: T) {
  const out: Record<string, unknown> = {}
  Object.entries(value).forEach(([key, item]) => {
    if (item === undefined || item === null || item === '') return
    if (Array.isArray(item) && item.length === 0) return
    if (typeof item === 'object' && !Array.isArray(item) && Object.keys(item as Record<string, unknown>).length === 0) return
    out[key] = item
  })
  return out
}

export function accessPolicyApprovalPolicyFromValues(values: Record<string, unknown>) {
  const strategy = firstString(values, 'approvalMode')
  if (!strategy || strategy === 'none') {
    return {}
  }
  const approvalPolicy = compactObject({
    strategy,
    approvalPolicyRef: firstString(values, 'approvalPolicyRef'),
  })
  if (approvalRoutingEnabled(strategy)) {
    const changeWindow = compactObject({
      startsAt: firstString(values, 'approvalChangeWindowStartsAt'),
      endsAt: firstString(values, 'approvalChangeWindowEndsAt'),
      timezone: firstString(values, 'approvalChangeWindowTimezone'),
    })
    Object.assign(approvalPolicy, compactObject({
      approvalMode: firstString(values, 'approvalRoutingMode') ?? 'all',
      approverUsers: normalizedStringList(values.approvalApproverUsers),
      approverRoles: normalizedStringList(values.approvalApproverRoles),
      approverTeams: normalizedStringList(values.approvalApproverTeams),
      onCallRef: firstString(values, 'approvalOnCallRef'),
      requiredApprovals: positiveNumber(values.approvalRequiredApprovals),
      changeWindow: Object.keys(changeWindow).length > 0 ? changeWindow : undefined,
    }))
  }
  return approvalPolicy
}

export function accessPolicyConditionsFromValues(values: Record<string, unknown>) {
  const conditions: Record<string, unknown> = {}
  if (values.rateLimitEnabled) {
    const rateLimitMode = normalizeRateLimitMode(firstString(values, 'rateLimitMode')) ?? 'counter'
    const rateLimit = compactObject({
      mode: rateLimitMode !== 'counter' ? rateLimitMode : undefined,
      scope: firstString(values, 'rateLimitScope') ?? 'actor_client_tool',
      maxCallsPerMinute: positiveNumber(values.rateLimitMaxCallsPerMinute),
      maxCallsPerHour: positiveNumber(values.rateLimitMaxCallsPerHour),
      burst: rateLimitMode === 'gcra' ? positiveNumber(values.rateLimitBurst) : undefined,
    })
    if (Object.keys(rateLimit).length > 1 || rateLimit.maxCallsPerMinute || rateLimit.maxCallsPerHour) {
      conditions.rateLimit = rateLimit
    }
  }
  if (values.budgetEnabled) {
    const budget = compactObject({
      scope: firstString(values, 'budgetScope') ?? 'actor_client',
      maxCallsPerDay: positiveNumber(values.budgetMaxCallsPerDay),
      maxTokensPerDay: positiveNumber(values.budgetMaxTokensPerDay),
      maxCostPerDay: positiveNumber(values.budgetMaxCostPerDay),
    })
    if (Object.keys(budget).length > 1 || budget.maxCallsPerDay || budget.maxTokensPerDay || budget.maxCostPerDay) {
      conditions.budget = budget
    }
  }
  if (values.redactionEnabled) {
    const mode = firstString(values, 'redactionMode')
    const redactionPolicy = compactObject({
      mode: mode && mode !== 'none' ? mode : 'sanitize',
      target: firstString(values, 'redactionTarget') ?? 'input',
      fields: normalizedStringList(values.redactionFields),
      allowFields: normalizedStringList(values.redactionAllowFields),
      secretTypes: normalizedStringList(values.redactionSecretTypes),
      valuePatterns: normalizedStringList(values.redactionValuePatterns),
      replacement: firstString(values, 'redactionReplacement') ?? '[REDACTED]',
      preserveFormat: values.redactionPreserveFormat === true ? true : undefined,
    })
    if (Object.keys(redactionPolicy).length > 0) {
      conditions.redactionPolicy = redactionPolicy
    }
    const outputRedactionFields = normalizedStringList(values.outputRedactionFields)
    const outputRedactionSecretTypes = normalizedStringList(values.outputRedactionSecretTypes)
    const outputRedactionValuePatterns = normalizedStringList(values.outputRedactionValuePatterns)
    if (outputRedactionFields.length > 0 || outputRedactionSecretTypes.length > 0 || outputRedactionValuePatterns.length > 0) {
      conditions.outputRedactionPolicy = compactObject({
        mode: 'sanitize',
        fields: outputRedactionFields,
        secretTypes: outputRedactionSecretTypes,
        valuePatterns: outputRedactionValuePatterns,
        replacement: firstString(values, 'outputRedactionReplacement') ?? '[REDACTED]',
        preserveFormat: values.outputRedactionPreserveFormat === true ? true : undefined,
      })
    }
  }
  return conditions
}

export function valuesToResourceScopes(values: Record<string, unknown>) {
  const scopes: Record<string, string[]> = {}
  scopeFieldDefs.forEach((field) => {
    const value = values[field.name]
    if (Array.isArray(value) && value.length) {
      scopes[field.key] = value.map((item) => String(item)).filter(Boolean)
    }
  })
  return scopes
}

export function queryString(params: Record<string, string | undefined>) {
  const search = new URLSearchParams()
  Object.entries(params).forEach(([key, value]) => {
    if (value) search.set(key, value)
  })
  const suffix = search.toString()
  return suffix ? `?${suffix}` : ''
}

export function gatewayTimeRangeQuery(value: GatewayTimeRangeValue) {
  return {
    from: value?.[0]?.toISOString() ?? '',
    to: value?.[1]?.toISOString() ?? '',
  }
}

export function governanceCoverageRows(coverage?: Partial<GovernancePolicyCoverage>): GovernanceCoverageRow[] {
  const activeAccessPolicies = coverage?.activeAccessPolicies ?? 0
  const activeToolGrants = coverage?.activeToolGrants ?? 0
  const activeSkillBindings = coverage?.activeSkillBindings ?? 0
  return [
    {
      key: 'access_policies',
      label: 'Access policies',
      state: activeAccessPolicies > 0 ? 'configured' : 'not_configured',
      configured: activeAccessPolicies,
      total: coverage?.accessPolicies ?? 0,
    },
    {
      key: 'tool_grants',
      label: 'Tool grants',
      state: activeToolGrants > 0 ? 'configured' : 'not_configured',
      configured: activeToolGrants,
      total: coverage?.toolGrants ?? 0,
    },
    {
      key: 'skill_bindings',
      label: 'Skill bindings',
      state: activeSkillBindings > 0 ? 'configured' : 'not_configured',
      configured: activeSkillBindings,
      total: coverage?.skillBindings ?? 0,
    },
    {
      key: 'budget',
      label: 'Budget',
      state: coverage?.budgetState ?? 'not_configured',
      configured: coverage?.budgetPolicies ?? 0,
      total: activeAccessPolicies,
    },
    {
      key: 'rate_limit',
      label: 'Rate limit',
      state: coverage?.rateLimitState ?? 'not_configured',
      configured: coverage?.rateLimitPolicies ?? 0,
      total: activeAccessPolicies,
    },
    {
      key: 'redaction',
      label: 'Redaction',
      state: coverage?.redactionPolicyState ?? 'built_in',
      configured: coverage?.redactionPolicies ?? 0,
      total: activeAccessPolicies,
    },
    {
      key: 'resource_scopes',
      label: 'Resource scopes',
      state: coverage?.resourceScopeState ?? 'not_configured',
      configured: (coverage?.resourceScopedAccessPolicies ?? 0) + (coverage?.resourceScopedToolGrants ?? 0),
      total: activeAccessPolicies + activeToolGrants,
    },
  ]
}

export function governanceApprovalQueueRows(approvals?: Partial<GovernanceApprovalSummary>, clients?: Partial<GovernanceClientSummary>): GovernanceQueueRow[] {
  return [
    {
      key: 'due_soon',
      label: 'Due soon approvals',
      count: approvals?.dueSoon ?? 0,
      refs: approvals?.dueSoonRequestIds ?? [],
    },
    {
      key: 'stale',
      label: 'Stale approvals',
      count: approvals?.stalePending ?? 0,
      refs: approvals?.stalePendingRequestIds ?? [],
    },
    {
      key: 'overdue',
      label: 'Overdue approvals',
      count: approvals?.overdue ?? 0,
      refs: approvals?.overdueRequestIds ?? [],
    },
    {
      key: 'pending_clients',
      label: 'Pending AI clients',
      count: clients?.pendingApproval ?? 0,
      refs: clients?.pendingApprovalClientIds ?? [],
    },
  ]
}

export function governanceTokenFindingRows(tokens?: Partial<GovernanceTokenSummary>): GovernanceTokenFindingRow[] {
  const groups = [
    { category: 'expiredActive', categoryLabel: 'Expired active', values: tokens?.expiredActive },
    { category: 'expiringSoon', categoryLabel: 'Expiring soon', values: tokens?.expiringSoon },
    { category: 'stale', categoryLabel: 'Stale', values: tokens?.stale },
    { category: 'neverUsed', categoryLabel: 'Never used', values: tokens?.neverUsed },
  ] as const
  return groups.flatMap((group) => (group.values ?? []).map((item) => ({
    ...item,
    key: `${group.category}:${item.kind}:${item.id || item.tokenPrefix}`,
    category: group.category,
    categoryLabel: group.categoryLabel,
  })))
}

export function governanceRedactionRows(redaction?: Partial<GovernanceRedactionSummary>): GovernanceRedactionRow[] {
  const firstPolicy = redaction?.topPolicies?.find((item) => item.key.trim())?.key ?? ''
  const firstTool = redaction?.topTools?.find((item) => item.key.trim())?.key ?? ''
  return [
    {
      key: 'targets',
      label: 'Targets',
      count: (redaction?.inputAudits ?? 0) + (redaction?.outputAudits ?? 0),
      items: redaction?.topTargets ?? [],
    },
    {
      key: 'match_types',
      label: 'Match types',
      count: redaction?.totalMatches ?? 0,
      items: redaction?.topMatchTypes ?? [],
    },
    {
      key: 'classifiers',
      label: 'Classifiers',
      count: redaction?.secretClassifierMatches ?? 0,
      items: redaction?.topClassifiers ?? [],
    },
    {
      key: 'field_paths',
      label: 'Field paths',
      count: (redaction?.fieldMatches ?? 0) + (redaction?.sensitiveKeyMatches ?? 0) + (redaction?.structuredSecretMatches ?? 0),
      items: redaction?.topFieldPaths ?? [],
    },
    {
      key: 'policies',
      label: 'Policies',
      count: redaction?.topPolicies?.reduce((sum, item) => sum + item.count, 0) ?? 0,
      items: redaction?.topPolicies ?? [],
      target: firstPolicy ? { tab: 'policies', policyFilter: firstPolicy } : undefined,
    },
    {
      key: 'tools',
      label: 'Tools',
      count: redaction?.topTools?.reduce((sum, item) => sum + item.count, 0) ?? 0,
      items: redaction?.topTools ?? [],
      target: firstTool ? { tab: 'audit', auditFilters: auditDrilldownFilters({ toolName: firstTool }) } : undefined,
    },
  ]
}

export const governanceDefaultPolicyDraft = {
  name: 'Gateway governance guardrail',
  description: 'Created from AI Gateway governance coverage.',
  subjectType: 'role',
  subjectId: 'developer',
  effect: 'allow',
  enabled: 'true',
  riskLevels: ['mutate', 'execute', 'high'],
  approvalMode: 'require_approval',
  approvalRoutingMode: 'all',
  approvalRequiredApprovals: 1,
  rateLimitEnabled: true,
  rateLimitMode: 'gcra',
  rateLimitScope: 'actor_client_tool',
  rateLimitMaxCallsPerHour: 60,
  rateLimitBurst: 10,
  budgetEnabled: true,
  budgetScope: 'actor_client',
  budgetMaxCallsPerDay: 500,
  redactionEnabled: true,
  redactionMode: 'sanitize',
  redactionTarget: 'both',
  redactionSecretTypes: ['default', 'github', 'openai', 'kubeconfig', 'docker_config'],
  outputRedactionSecretTypes: ['default', 'github', 'openai', 'kubeconfig', 'docker_config'],
  outputRedactionReplacement: '[REDACTED]',
  outputRedactionPreserveFormat: false,
}

export function governancePolicyDraftForCoverage(row: GovernanceCoverageRow): Record<string, unknown> {
  const draft: Record<string, unknown> = { ...governanceDefaultPolicyDraft }
  if (row.key === 'budget') {
    return {
      ...draft,
      name: 'Gateway daily budget guardrail',
      description: 'Limit AI Gateway invocations before widening access.',
      rateLimitEnabled: false,
      redactionEnabled: false,
    }
  }
  if (row.key === 'rate_limit') {
    return {
      ...draft,
      name: 'Gateway rate limit guardrail',
      description: 'Throttle AI Gateway tool invocations per actor/client/tool.',
      budgetEnabled: false,
      redactionEnabled: false,
    }
  }
  if (row.key === 'redaction') {
    return {
      ...draft,
      name: 'Gateway redaction guardrail',
      description: 'Sanitize sensitive AI Gateway input and output before persistence or display.',
      rateLimitEnabled: false,
      budgetEnabled: false,
    }
  }
  if (row.key === 'resource_scopes') {
    return {
      ...draft,
      name: 'Gateway scoped high-risk guardrail',
      description: 'Require concrete resource scope before allowing high-risk Gateway tools.',
      scopeClusterIds: [],
      scopeNamespaces: [],
    }
  }
  return draft
}

export function governanceCoverageDrilldown(row: GovernanceCoverageRow): GovernanceDrilldownTarget {
  if (['access_policies', 'budget', 'rate_limit', 'redaction', 'resource_scopes'].includes(row.key)) {
    return {
      tab: 'policies',
      policyDraft: governancePolicyDraftForCoverage(row),
    }
  }
  if (row.key === 'tool_grants') return { tab: 'grants' }
  if (row.key === 'skill_bindings') return { tab: 'bindings' }
  return { tab: 'policies' }
}

export function governanceCoverageRowForTemplate(template?: string): GovernanceCoverageRow {
  switch (template) {
    case 'budget':
      return { key: 'budget', label: 'Budget', state: 'not_configured', configured: 0, total: 0 }
    case 'rate_limit':
      return { key: 'rate_limit', label: 'Rate limit', state: 'not_configured', configured: 0, total: 0 }
    case 'redaction':
      return { key: 'redaction', label: 'Redaction', state: 'not_configured', configured: 0, total: 0 }
    case 'resource_scopes':
    case 'resource_scope_guardrail':
      return { key: 'resource_scopes', label: 'Resource scopes', state: 'not_configured', configured: 0, total: 0 }
    default:
      return { key: 'access_policies', label: 'Access policies', state: 'not_configured', configured: 0, total: 0 }
  }
}

export function governanceRecommendationDrilldownAction(record: GovernanceRecommendationAction): GovernanceDrilldownAction | null {
  const firstRef = record.targetId || record.refs?.find((item) => item.trim()) || ''
  const policyTemplate = typeof record.metadata?.policyTemplate === 'string' ? record.metadata.policyTemplate : ''
  if (record.targetKind === 'tokens') {
    const serviceTokenRef = serviceTokenRecommendationRef(record, firstRef)
    return {
      label: '处理 token',
      target: serviceTokenRef
        ? { tab: 'service-accounts', serviceTokenFilter: serviceTokenRef }
        : { tab: 'tokens', tokenFilter: firstRef },
    }
  }
  if (record.targetKind === 'approval_requests') {
    return { label: '处理审批', target: { tab: 'approvals', approvalFilters: approvalDrilldownFilters(firstRef) } }
  }
  if (record.targetKind === 'access_policies' || record.action.startsWith('create_')) {
    return {
      label: '创建 policy',
      target: {
        tab: 'policies',
        policyDraft: governancePolicyDraftForCoverage(governanceCoverageRowForTemplate(policyTemplate)),
      },
    }
  }
  if (record.targetKind === 'anomalies') {
    return { label: '查看 findings', target: { tab: 'governance' } }
  }
  return null
}

export function serviceTokenRecommendationRef(record: GovernanceRecommendationAction, preferredRef: string) {
  const values = record.metadata?.tokenRefs
  const tokenRefs = Array.isArray(values) ? values : []
  let fallback = ''
  for (const item of tokenRefs) {
    if (!item || typeof item !== 'object') continue
    const tokenRef = item as Record<string, unknown>
    if (asText(tokenRef.kind) !== 'service_account_token') continue
    const id = asText(tokenRef.id)
    const prefix = asText(tokenRef.tokenPrefix)
    const ref = id || prefix || asText(tokenRef.name)
    if (!fallback) fallback = ref
    if (preferredRef && (preferredRef === id || preferredRef === prefix)) {
      return ref
    }
  }
  if (fallback) return fallback
  return preferredRef.startsWith('sat-') || preferredRef.startsWith('soha_sat_') ? preferredRef : ''
}

export function approvalDrilldownFilters(id: string): ApprovalFilterState {
  return {
    id,
    status: '',
    actor: '',
    aiClientId: '',
    toolName: '',
    riskLevel: '',
    strategy: '',
    from: '',
    to: '',
  }
}

export function auditDrilldownFilters(filters: Partial<AuditFilterState>): AuditFilterState {
  return {
    actor: '',
    aiClientId: '',
    toolName: '',
    action: '',
    riskLevel: '',
    result: '',
    from: '',
    to: '',
    ...filters,
  }
}

export function governanceQueueDrilldown(row: GovernanceQueueRow, ref: string): GovernanceDrilldownTarget {
  const value = ref.trim()
  if (row.key === 'pending_clients') {
    return {
      tab: 'clients',
      clientFilter: value,
    }
  }
  return {
    tab: 'approvals',
    approvalFilters: approvalDrilldownFilters(value),
  }
}

export function governanceTokenFindingDrilldown(record: GovernanceTokenFindingRow): GovernanceDrilldownTarget {
  if (record.kind === 'service_account_token') {
    const tokenRef = record.id || record.tokenPrefix || record.name
    return {
      tab: 'service-accounts',
      serviceTokenFilter: tokenRef,
      serviceTokenRevokeId: record.id,
    }
  }
  return {
    tab: 'tokens',
    tokenFilter: record.id || record.tokenPrefix || record.name,
  }
}

export function asText(value: unknown) {
  if (typeof value === 'string') return value.trim()
  if (typeof value === 'number' || typeof value === 'boolean') return String(value)
  return ''
}

export function relatedText(record: ApprovalRequest, ...keys: string[]) {
  for (const key of keys) {
    const value = asText(record.relatedIds?.[key])
    if (value) return value
  }
  return ''
}

export function approvalTrace(record: ApprovalRequest) {
  return {
    approvalRequestId: relatedText(record, 'approvalRequestId') || record.id,
    workflowRunId: relatedText(record, 'workflowRunId', 'workflowRunID'),
    executionTaskId: relatedText(record, 'executionTaskId'),
    releaseBundleId: relatedText(record, 'releaseBundleId'),
    applicationId: relatedText(record, 'applicationId') || asText(record.toolInput?.applicationId),
    applicationEnvironmentId: relatedText(record, 'applicationEnvironmentId') || asText(record.toolInput?.applicationEnvironmentId),
  }
}

export function workflowTracePath(trace: ReturnType<typeof approvalTrace>) {
  return `/workflows${queryString({
    workflowRunId: trace.workflowRunId,
    gatewayApprovalRequestId: trace.approvalRequestId,
  })}`
}

export function governanceFindingDrilldownActions(record: GovernanceFinding): GovernanceDrilldownAction[] {
  const actions: GovernanceDrilldownAction[] = []
  if (record.approvalRequestId) {
    actions.push({
      label: '查看审批',
      target: {
        tab: 'approvals',
        approvalFilters: approvalDrilldownFilters(record.approvalRequestId),
      },
    })
  }
  if (record.aiClientId) {
    actions.push({
      label: '查看 client',
      target: {
        tab: 'clients',
        clientFilter: record.aiClientId,
      },
    })
  }
  if (record.policyId) {
    actions.push({
      label: '查看 policy',
      target: {
        tab: 'policies',
        policyFilter: record.policyId,
      },
    })
    if (['high_risk_allow_without_approval', 'high_risk_allow_without_resource_scope'].includes(record.type)) {
      actions.push({
        label: '修复 policy',
        target: {
          tab: 'policies',
          policyFilter: record.policyId,
          policyDraft: {
            ...governanceDefaultPolicyDraft,
            name: record.type === 'high_risk_allow_without_resource_scope' ? 'Gateway scoped high-risk guardrail' : 'Gateway high-risk approval guardrail',
            description: record.summary,
            toolPatterns: record.toolName ? [record.toolName] : [],
            riskLevels: record.riskLevel ? [record.riskLevel] : ['mutate', 'execute', 'high'],
            scopeClusterIds: record.type === 'high_risk_allow_without_resource_scope' ? [] : undefined,
            scopeNamespaces: record.type === 'high_risk_allow_without_resource_scope' ? [] : undefined,
          },
        },
      })
    }
  }
  if (record.grantId) {
    actions.push({
      label: '查看 grant',
      target: {
        tab: 'grants',
        grantFilter: record.grantId,
      },
    })
    if (['high_risk_grant_without_approval', 'high_risk_grant_without_resource_scope'].includes(record.type)) {
      actions.push({
        label: '补 guardrail',
        target: {
          tab: 'policies',
          grantFilter: record.grantId,
          policyDraft: {
            ...governanceDefaultPolicyDraft,
            name: record.type === 'high_risk_grant_without_resource_scope' ? 'Gateway scoped grant guardrail' : 'Gateway grant approval guardrail',
            description: record.summary,
            subjectType: record.subjectType || 'role',
            subjectId: record.subjectId || 'developer',
            aiClientId: record.aiClientId,
            toolPatterns: record.toolName ? [record.toolName] : [],
            riskLevels: record.riskLevel ? [record.riskLevel] : ['mutate', 'execute', 'high'],
            scopeClusterIds: record.type === 'high_risk_grant_without_resource_scope' ? [] : undefined,
            scopeNamespaces: record.type === 'high_risk_grant_without_resource_scope' ? [] : undefined,
          },
        },
      })
    }
  }
  if (record.actorId || record.aiClientId || record.toolName || record.riskLevel) {
    actions.push({
      label: '查日志',
      target: {
        tab: 'audit',
        auditFilters: auditDrilldownFilters({
          actor: record.actorId ?? '',
          aiClientId: record.aiClientId ?? '',
          toolName: record.toolName ?? '',
          riskLevel: record.riskLevel ?? '',
          result: '',
        }),
      },
    })
  }
  return actions
}
