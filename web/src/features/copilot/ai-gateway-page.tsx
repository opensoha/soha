import { useEffect, useMemo, useState, type ReactNode } from 'react'
import {
  CheckOutlined,
  CloseOutlined,
  DeleteOutlined,
  EditOutlined,
  HistoryOutlined,
  LinkOutlined,
  KeyOutlined,
  PlusOutlined,
  ReloadOutlined,
  SafetyCertificateOutlined,
  StopOutlined,
} from '@ant-design/icons'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import type { TableColumnsType } from 'antd'
import {
  Alert,
  App,
  Button,
  Card,
  Checkbox,
  DatePicker,
  Descriptions,
  Divider,
  Drawer,
  Empty,
  Form,
  Input,
  InputNumber,
  Modal,
  Select,
  Space,
  Table,
  Tabs,
  Tag,
  Typography,
} from 'antd'
import { useLocation, useNavigate, useSearchParams } from 'react-router-dom'
import { StatusTag } from '@/components/status-tag'
import { hasPermission, usePermissionSnapshot } from '@/features/auth/permission-snapshot'
import { api } from '@/services/api-client'
import type { ApiResponse } from '@/types'

const { Paragraph, Text } = Typography
const { RangePicker } = DatePicker

type GatewayTimeRangeValue = readonly [
  { toISOString: () => string } | null,
  { toISOString: () => string } | null,
] | null

type RiskLevel = 'read' | 'analyze' | 'mutate' | 'execute' | 'high'
type GatewayEffect = 'allow' | 'deny'
type GatewaySectionKey = 'overview' | 'manifest' | 'clients' | 'tokens' | 'governance' | 'call-logs'
export type GatewayTabKey = 'manifest' | 'clients' | 'tokens' | 'service-accounts' | 'grants' | 'policies' | 'bindings' | 'governance' | 'approvals' | 'audit'

const gatewayMenuMeta: Record<GatewayTabKey, { description: string; title: string }> = {
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
    description: '个人访问令牌与一次性明文创建流程。',
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

const gatewaySectionMeta: Record<GatewaySectionKey, { description: string; title: string }> = {
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
    description: '管理 personal access tokens、service accounts 与自动化调用 token。',
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

const gatewayTabSectionMap: Record<GatewayTabKey, GatewaySectionKey> = {
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

const gatewaySectionPaths: Record<GatewaySectionKey, string> = {
  overview: '/ai-gateway/overview',
  manifest: '/ai-gateway/manifest',
  clients: '/ai-gateway/clients',
  tokens: '/ai-gateway/tokens',
  governance: '/ai-gateway/governance',
  'call-logs': '/ai-gateway/call-logs',
}

function gatewaySectionFromPath(pathname: string): GatewaySectionKey {
  if (pathname.startsWith('/ai-gateway/manifest')) return 'manifest'
  if (pathname.startsWith('/ai-gateway/clients')) return 'clients'
  if (pathname.startsWith('/ai-gateway/tokens')) return 'tokens'
  if (pathname.startsWith('/ai-gateway/governance')) return 'governance'
  if (pathname.startsWith('/ai-gateway/call-logs')) return 'call-logs'
  return 'overview'
}

function normalizeGatewayTabKey(value: string | null): GatewayTabKey | null {
  if (!value) return null
  return Object.prototype.hasOwnProperty.call(gatewayTabSectionMap, value) ? (value as GatewayTabKey) : null
}

function defaultGatewayTabForSection(section: GatewaySectionKey, focusedApprovalRequestId: string): GatewayTabKey {
  if (focusedApprovalRequestId) return 'approvals'
  if (section === 'manifest') return 'manifest'
  if (section === 'clients') return 'clients'
  if (section === 'tokens') return 'tokens'
  if (section === 'call-logs') return 'audit'
  return 'governance'
}

function gatewayTabBelongsToSection(tab: GatewayTabKey, section: GatewaySectionKey) {
  return gatewayTabSectionMap[tab] === section
}

interface AIClient {
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

interface PersonalAccessToken {
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

interface CreatedPersonalAccessToken {
  token: PersonalAccessToken
  value: string
}

interface ServiceAccount {
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

interface ServiceAccountToken {
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

interface CreatedServiceAccountToken {
  token: ServiceAccountToken
  value: string
}

interface ToolGrant {
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

interface AccessPolicy {
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

interface SkillBinding {
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

interface GatewayTool {
  name: string
  title: string
  domain: string
  action: string
  riskLevel: RiskLevel
  permissionKeys: string[]
  requiredScopes?: string[]
  requiresApproval: boolean
}

interface GatewaySkill {
  id: string
  name: string
  category: string
  capabilityRefs?: string[]
  requiredScopes?: string[]
}

interface GatewayManifest {
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

interface GatewayAuditLog {
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

interface ApprovalRequest {
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

interface ApprovalDecisionResult {
  request: ApprovalRequest
  invocation?: {
    result: string
    output?: unknown
    relatedIds?: Record<string, unknown>
  }
}

interface GovernanceMetricCount {
  key: string
  count: number
}

interface GovernanceHealthCheck {
  name: string
  status: string
  message: string
  count?: number
}

interface GovernanceHealth {
  status: string
  message: string
  checks?: GovernanceHealthCheck[]
}

interface GovernanceMetrics {
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

interface GovernanceRedactionSummary {
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

interface GovernanceTokenCounts {
  total: number
  active: number
  revoked: number
  expired: number
  expiringSoon: number
  stale: number
  neverUsed: number
}

interface GovernanceTokenFinding {
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

interface GovernanceTokenSummary {
  personalAccessTokens: GovernanceTokenCounts
  serviceAccountTokens: GovernanceTokenCounts
  expiringSoon?: GovernanceTokenFinding[]
  expiredActive?: GovernanceTokenFinding[]
  stale?: GovernanceTokenFinding[]
  neverUsed?: GovernanceTokenFinding[]
  lastUsedTrackingState: string
}

interface GovernanceClientSummary {
  total: number
  active: number
  disabled: number
  pendingApproval: number
  registrationApproval: string
  pendingApprovalClientIds?: string[]
}

interface GovernanceApprovalSummary {
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

interface AuditFilterState {
  actor: string
  aiClientId: string
  toolName: string
  action: string
  riskLevel: string
  result: string
  from: string
  to: string
}

interface ApprovalFilterState {
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

interface GovernanceStatus {
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

type DrawerKind =
  | 'ai-client'
  | 'personal-token'
  | 'service-account'
  | 'service-token'
  | 'service-token-revoke'
  | 'tool-grant'
  | 'access-policy'
  | 'skill-binding'

interface DrawerState {
  kind: DrawerKind
  record?: AIClient | ServiceAccount | ToolGrant | AccessPolicy | SkillBinding
  initialValues?: Record<string, unknown>
}

const subjectTypeOptions = [
  { label: '用户', value: 'user' },
  { label: '服务账号', value: 'service_account' },
  { label: '角色', value: 'role' },
  { label: 'AI Client', value: 'ai_client' },
]

const riskLevelOptions = [
  { label: 'read', value: 'read' },
  { label: 'analyze', value: 'analyze' },
  { label: 'mutate', value: 'mutate' },
  { label: 'execute', value: 'execute' },
  { label: 'high', value: 'high' },
]

const effectOptions = [
  { label: '允许', value: 'allow' },
  { label: '拒绝', value: 'deny' },
]

const approvalStrategyOptions = [
  { label: '不强制', value: 'none' },
  { label: '直接允许', value: 'allow' },
  { label: '直接拒绝', value: 'deny' },
  { label: '要求审批', value: 'require_approval' },
  { label: '人工确认', value: 'require_human_confirm' },
  { label: '仅 dry-run', value: 'dry_run_only' },
]

const approvalRequestStrategyOptions = approvalStrategyOptions.filter((item) => ['require_approval', 'require_human_confirm'].includes(item.value))

const approvalRoutingModeOptions = [
  { label: '会签 all', value: 'all' },
  { label: '或签 any', value: 'any' },
]

const approvalStatusOptions = [
  { label: '待处理', value: 'pending' },
  { label: '已批准', value: 'approved' },
  { label: '已执行', value: 'executed' },
  { label: '已拒绝', value: 'rejected' },
  { label: '已取消', value: 'canceled' },
  { label: '已超时', value: 'timeout' },
  { label: '执行失败', value: 'failed' },
]

const auditActionOptions = [
  { label: '工具调用', value: 'ai_gateway.tool.invoke' },
  { label: '资源读取', value: 'ai_gateway.resource.read' },
  { label: 'Prompt 获取', value: 'ai_gateway.prompt.get' },
  { label: '审批请求', value: 'ai_gateway.approval.request' },
  { label: '审批决策', value: 'ai_gateway.approval.decision' },
  { label: '审批超时', value: 'ai_gateway.approval.timeout' },
]

const auditResultOptions = [
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

const governanceWindowOptions = [
  { label: '1h', value: '1' },
  { label: '6h', value: '6' },
  { label: '24h', value: '24' },
  { label: '48h', value: '48' },
  { label: '7d', value: '168' },
]

const clientKindOptions = [
  { label: 'AI Coding', value: 'ai_coding' },
  { label: 'MCP Client', value: 'mcp_client' },
  { label: 'CI Agent', value: 'ci_agent' },
  { label: 'Enterprise Agent', value: 'enterprise_agent' },
]

const statusOptions = [
  { label: 'active', value: 'active' },
  { label: 'disabled', value: 'disabled' },
]

const gatewayLimitScopeOptions = [
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

const redactionModeOptions = [
  { label: '不启用', value: 'none' },
  { label: 'Strict deny', value: 'strict' },
  { label: 'Sanitize', value: 'sanitize' },
  { label: 'Mask', value: 'mask' },
  { label: 'Redact', value: 'redact' },
]

const redactionTargetOptions = [
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

const scopeFieldDefs = [
  { name: 'scopeBusinessLineIds', label: '业务线', key: 'businessLineIds' },
  { name: 'scopeApplicationIds', label: '应用', key: 'applicationIds' },
  { name: 'scopeApplicationEnvironmentIds', label: '应用环境绑定', key: 'applicationEnvironmentIds' },
  { name: 'scopeEnvironmentIds', label: '环境', key: 'environmentIds' },
  { name: 'scopeClusterIds', label: '集群', key: 'clusterIds' },
  { name: 'scopeNamespaces', label: '命名空间', key: 'namespaces' },
  { name: 'scopeReleaseBundleIds', label: '版本包', key: 'releaseBundleIds' },
  { name: 'scopeExecutionTaskIds', label: '执行任务', key: 'executionTaskIds' },
] as const

function formatDateTime(value?: string) {
  if (!value) return '-'
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return value
  return new Intl.DateTimeFormat('zh-CN', {
    month: '2-digit',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
  }).format(date)
}

function compactList(values?: string[], max = 3) {
 const items = values?.filter(Boolean) ?? []
 if (!items.length) return <Text type="secondary">-</Text>
 return (
    <Space size={[4, 4]} wrap>
      {items.slice(0, max).map((item) => <Tag key={item}>{item}</Tag>)}
      {items.length > max ? <Tag>+{items.length - max}</Tag> : null}
    </Space>
  )
}

function sortedRecordEntries(values?: Record<string, number>) {
  return Object.entries(values ?? {})
    .filter(([, value]) => Number.isFinite(value) && value > 0)
    .sort(([leftKey, leftValue], [rightKey, rightValue]) => rightValue - leftValue || leftKey.localeCompare(rightKey))
}

export function governanceRiskCountTags(values?: Record<string, number>) {
  return sortedRecordEntries(values).map(([key, count]) => `${key}:${count}`)
}

function stringifyPayload(value: unknown) {
  return JSON.stringify(value ?? {}, null, 2)
}

function scopeValuesFromRecord(scopes?: Record<string, unknown>) {
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

function approvalModeFromPolicy(policy?: Record<string, unknown>) {
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

function approvalRoutingEnabled(strategy: unknown) {
  return ['require_approval', 'require_human_confirm'].includes(String(strategy ?? '').trim())
}

function asRecord(value: unknown) {
  if (value && typeof value === 'object' && !Array.isArray(value)) {
    return value as Record<string, unknown>
  }
  return {}
}

function firstValue(record: Record<string, unknown>, ...keys: string[]) {
  for (const key of keys) {
    const value = record[key]
    if (value !== undefined && value !== null && value !== '') {
      return value
    }
  }
  return undefined
}

function firstNumber(record: Record<string, unknown>, ...keys: string[]) {
  const value = firstValue(record, ...keys)
  if (typeof value === 'number' && Number.isFinite(value)) return value
  if (typeof value === 'string' && value.trim()) {
    const parsed = Number(value)
    if (Number.isFinite(parsed)) return parsed
  }
  return undefined
}

function firstString(record: Record<string, unknown>, ...keys: string[]) {
  const value = firstValue(record, ...keys)
  if (typeof value === 'string') return value.trim()
  if (typeof value === 'number' || typeof value === 'boolean') return String(value)
  return undefined
}

function firstStringList(record: Record<string, unknown>, ...keys: string[]) {
  const value = firstValue(record, ...keys)
  if (Array.isArray(value)) {
    return value.map((item) => String(item).trim()).filter(Boolean)
  }
  if (typeof value === 'string' && value.trim()) {
    return value.split(/[,\n]/).map((item) => item.trim()).filter(Boolean)
  }
  return undefined
}

function normalizeRateLimitMode(value?: string) {
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

function approvalPolicyValues(policy?: Record<string, unknown>) {
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

function positiveNumber(value: unknown) {
  if (typeof value === 'number' && Number.isFinite(value) && value > 0) return value
  if (typeof value === 'string' && value.trim()) {
    const parsed = Number(value)
    if (Number.isFinite(parsed) && parsed > 0) return parsed
  }
  return undefined
}

function normalizedStringList(value: unknown) {
  if (!Array.isArray(value)) return []
  return value.map((item) => String(item).trim()).filter(Boolean)
}

function compactObject<T extends Record<string, unknown>>(value: T) {
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

function valuesToResourceScopes(values: Record<string, unknown>) {
  const scopes: Record<string, string[]> = {}
  scopeFieldDefs.forEach((field) => {
    const value = values[field.name]
    if (Array.isArray(value) && value.length) {
      scopes[field.key] = value.map((item) => String(item)).filter(Boolean)
    }
  })
  return scopes
}

function scopeSummary(scopes?: Record<string, unknown>) {
  const entries = scopeFieldDefs.flatMap((field) => {
    const values = scopeValuesFromRecord(scopes)[field.name]
    return values?.length ? [`${field.label}:${values.join(',')}`] : []
  })
  if (!entries.length) return <Text type="secondary">全局</Text>
  return (
    <Paragraph ellipsis={{ rows: 2, tooltip: entries.join(' / ') }} style={{ marginBottom: 0 }}>
      {entries.join(' / ')}
    </Paragraph>
  )
}

function auditActionLabel(action?: string) {
  const value = String(action ?? '').trim()
  return auditActionOptions.find((item) => item.value === value)?.label ?? value
}

function auditCallTarget(record: GatewayAuditLog) {
  const metadata = asRecord(record.metadata)
  return (
    record.toolName ||
    firstString(metadata, 'resourceUri', 'promptName', 'mcpToolName', 'resourceName') ||
    firstString(asRecord(metadata.relatedIds), 'toolName', 'resourceName') ||
    record.action ||
    '-'
  )
}

function primitiveScopeValue(value: unknown): string {
  if (typeof value === 'string') return value.trim()
  if (typeof value === 'number' || typeof value === 'boolean') return String(value)
  if (Array.isArray(value)) return value.map((item) => primitiveScopeValue(item)).filter(Boolean).join(',')
  return ''
}

function auditScopeItems(record: GatewayAuditLog) {
  const scope = asRecord(record.resourceScope)
  const orderedKeys = [
    'businessLineId',
    'applicationId',
    'applicationEnvironmentId',
    'environmentId',
    'clusterId',
    'namespace',
    'podName',
    'deploymentName',
    'serviceName',
    'releaseBundleId',
    'executionTaskId',
  ]
  const seen = new Set<string>()
  const items: string[] = []
  orderedKeys.forEach((key) => {
    const value = primitiveScopeValue(scope[key])
    if (value) {
      seen.add(key)
      items.push(`${key}:${value}`)
    }
  })
  Object.entries(scope).forEach(([key, value]) => {
    if (seen.has(key)) return
    const text = primitiveScopeValue(value)
    if (text) items.push(`${key}:${text}`)
  })
  return items
}

function auditScopeSummary(record: GatewayAuditLog) {
  const items = auditScopeItems(record)
  return items.length ? compactList(items, 3) : <Text type="secondary">全局</Text>
}

function auditCaller(record: GatewayAuditLog) {
  return (
    <Space orientation="vertical" size={0}>
      <Text strong>{record.actorName || record.actorId}</Text>
      <Text type="secondary">{record.actorType}:{record.actorId}</Text>
      {record.sourceIp ? <Text type="secondary">{record.sourceIp}</Text> : null}
    </Space>
  )
}

function auditEntryPoint(record: GatewayAuditLog) {
  return (
    <Space orientation="vertical" size={0}>
      <Text>{record.aiClientName || record.aiClientId || '-'}</Text>
      <Text type="secondary">{record.skillId ? `skill:${record.skillId}` : 'skill:-'}</Text>
    </Space>
  )
}

function auditInvocation(record: GatewayAuditLog) {
  const target = auditCallTarget(record)
  return (
    <Space orientation="vertical" size={4}>
      <Space size={4} wrap>
        <Text strong>{target}</Text>
        {record.action ? <Tag>{auditActionLabel(record.action)}</Tag> : null}
      </Space>
      {auditScopeSummary(record)}
    </Space>
  )
}

function auditResult(record: GatewayAuditLog) {
  return (
    <Space orientation="vertical" size={4}>
      <StatusTag value={record.result} />
      {record.riskLevel ? <StatusTag value={record.riskLevel} /> : null}
    </Space>
  )
}

function policyConditionSummary(conditions?: Record<string, unknown>) {
  const items: string[] = []
  const source = asRecord(conditions)
  const rateLimit = asRecord(firstValue(source, 'rateLimit', 'rate_limit', 'rateLimits'))
  const budget = asRecord(firstValue(source, 'budget', 'budgets', 'budgetPolicy'))
  const redactionPolicy = asRecord(firstValue(source, 'redactionPolicy', 'redaction', 'sensitiveDataRedaction'))
  const outputRedactionPolicy = asRecord(source.outputRedactionPolicy)

  if (Object.keys(rateLimit).length > 0) {
    const normalizedMode = normalizeRateLimitMode(firstString(rateLimit, 'mode', 'algorithm', 'strategy'))
    const mode = normalizedMode === 'gcra' ? 'GCRA' : normalizedMode === 'sliding_window' ? 'sliding-window' : 'fixed-window'
    const perMinute = firstNumber(rateLimit, 'maxCallsPerMinute', 'maxInvocationsPerMinute', 'callsPerMinute', 'rpm')
    const perHour = firstNumber(rateLimit, 'maxCallsPerHour', 'maxInvocationsPerHour', 'callsPerHour', 'rph')
    items.push(`rateLimit:${mode}${perMinute ? ` ${perMinute}/m` : ''}${perHour ? ` ${perHour}/h` : ''}`)
  }
  if (Object.keys(budget).length > 0) {
    const calls = firstNumber(budget, 'maxCallsPerDay', 'maxInvocationsPerDay', 'maxDailyCalls', 'dailyCalls', 'dailyBudget')
    const tokens = firstNumber(budget, 'maxTokensPerDay', 'dailyTokens', 'dailyTokenBudget')
    const cost = firstNumber(budget, 'maxCostPerDay', 'dailyCost', 'dailyCostBudget')
    items.push(`budget${calls ? ` ${calls}/d` : ''}${tokens ? ` ${tokens} tokens/d` : ''}${cost ? ` $${cost}/d` : ''}`)
  }
  if (Object.keys(redactionPolicy).length > 0 || Object.keys(outputRedactionPolicy).length > 0) {
    items.push(`redaction:${firstString(redactionPolicy, 'mode', 'strategy', 'redactionMode', 'action') ?? 'sanitize'}`)
  }

  return items.length ? compactList(items, 2) : <Text type="secondary">-</Text>
}

function queryString(params: Record<string, string | undefined>) {
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

const governanceDefaultPolicyDraft = {
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

function governanceCoverageRowForTemplate(template?: string): GovernanceCoverageRow {
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

function serviceTokenRecommendationRef(record: GovernanceRecommendationAction, preferredRef: string) {
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

function approvalDrilldownFilters(id: string): ApprovalFilterState {
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

function auditDrilldownFilters(filters: Partial<AuditFilterState>): AuditFilterState {
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

function asText(value: unknown) {
  if (typeof value === 'string') return value.trim()
  if (typeof value === 'number' || typeof value === 'boolean') return String(value)
  return ''
}

function relatedText(record: ApprovalRequest, ...keys: string[]) {
  for (const key of keys) {
    const value = asText(record.relatedIds?.[key])
    if (value) return value
  }
  return ''
}

function approvalTrace(record: ApprovalRequest) {
  return {
    approvalRequestId: relatedText(record, 'approvalRequestId') || record.id,
    workflowRunId: relatedText(record, 'workflowRunId', 'workflowRunID'),
    executionTaskId: relatedText(record, 'executionTaskId'),
    releaseBundleId: relatedText(record, 'releaseBundleId'),
    applicationId: relatedText(record, 'applicationId') || asText(record.toolInput?.applicationId),
    applicationEnvironmentId: relatedText(record, 'applicationEnvironmentId') || asText(record.toolInput?.applicationEnvironmentId),
  }
}

function workflowTracePath(trace: ReturnType<typeof approvalTrace>) {
  return `/workflows${queryString({
    workflowRunId: trace.workflowRunId,
    gatewayApprovalRequestId: trace.approvalRequestId,
  })}`
}

function governanceFindingTarget(record: GovernanceFinding) {
  const values = [
    record.actorId ? `${record.actorType || 'actor'}:${record.actorId}` : '',
    record.subjectId ? `${record.subjectType || 'subject'}:${record.subjectId}` : '',
    record.aiClientId ? `client:${record.aiClientId}` : '',
    record.toolName ? `tool:${record.toolName}` : '',
    record.policyId ? `policy:${record.policyId}` : '',
    record.grantId ? `grant:${record.grantId}` : '',
    record.approvalRequestId ? `approval:${record.approvalRequestId}` : '',
  ].filter(Boolean)
  return values.length ? compactList(values, 3) : <Text type="secondary">-</Text>
}

function governanceTokenTiming(record: GovernanceTokenFindingRow) {
  const items = [
    record.expiresAt ? `expires ${formatDateTime(record.expiresAt)}` : '',
    record.daysUntilDue !== undefined ? `${record.daysUntilDue}d due` : '',
    record.lastUsedAt ? `last ${formatDateTime(record.lastUsedAt)}` : '',
    record.staleDays ? `${record.staleDays}d stale` : '',
  ].filter(Boolean)
  return compactList(items, 3)
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

function cardTitle(icon: ReactNode, title: string) {
  return <Space size={8}>{icon}<span>{title}</span></Space>
}

function JsonBlock({ value }: { value: unknown }) {
  return <pre className="soha-system-json-block">{stringifyPayload(value)}</pre>
}

function ApprovalTracePanel({ record }: { record: ApprovalRequest }) {
  const navigate = useNavigate()
  const trace = approvalTrace(record)
  const workflowPath = workflowTracePath(trace)
  return (
    <Space orientation="vertical" size={12} style={{ width: '100%' }}>
      <Descriptions size="small" bordered column={3} items={[
        { key: 'approval', label: 'Gateway Approval', children: trace.approvalRequestId || '-' },
        { key: 'workflow', label: 'Workflow Run', children: trace.workflowRunId ? <Button size="small" type="link" onClick={() => navigate(workflowPath)}>{trace.workflowRunId}</Button> : '-' },
        { key: 'tool', label: 'Tool', children: record.toolName },
        { key: 'application', label: 'Application', children: trace.applicationId || '-' },
        { key: 'environment', label: 'App Env', children: trace.applicationEnvironmentId || '-' },
        { key: 'executionTask', label: 'Execution Task', children: trace.executionTaskId || '-' },
        { key: 'releaseBundle', label: 'Release Bundle', children: trace.releaseBundleId || '-' },
        { key: 'decision', label: 'Decision', children: record.decisionComment || '-' },
        {
          key: 'jump',
          label: 'Drilldown',
          children: trace.workflowRunId ? (
            <Button size="small" type="link" icon={<LinkOutlined />} onClick={() => navigate(workflowPath)}>
              查看工作流
            </Button>
          ) : <Text type="secondary">-</Text>,
        },
      ]} />
      <JsonBlock value={{ id: record.id, resourceScope: record.resourceScope, toolInput: record.toolInput, relatedIds: record.relatedIds, output: record.output, decisionComment: record.decisionComment }} />
    </Space>
  )
}

function isPendingApproval(record: ApprovalRequest) {
  return record.status === 'pending'
}

function recordMatchesText(values: Array<unknown>, filter: string) {
  const needle = filter.trim().toLowerCase()
  if (!needle) return true
  return values.some((value) => String(value ?? '').toLowerCase().includes(needle))
}

function ScopeFields() {
  return (
    <>
      {scopeFieldDefs.map((field) => (
        <Form.Item key={field.name} name={field.name} label={field.label}>
          <Select mode="tags" tokenSeparators={[',', ' ']} placeholder="留空表示不收窄" />
        </Form.Item>
      ))}
    </>
  )
}

function PolicyConditionFields() {
  return (
    <>
      <Divider plain>治理条件</Divider>
      <Form.Item name="rateLimitEnabled" valuePropName="checked">
        <Checkbox>启用 rate limit</Checkbox>
      </Form.Item>
      <Form.Item noStyle shouldUpdate={(prev, next) => prev.rateLimitEnabled !== next.rateLimitEnabled || prev.rateLimitMode !== next.rateLimitMode}>
        {({ getFieldValue }) => getFieldValue('rateLimitEnabled') ? (
          <>
            <Form.Item name="rateLimitMode" label="限流算法">
              <Select options={rateLimitModeOptions} />
            </Form.Item>
            <Form.Item name="rateLimitScope" label="限流维度">
              <Select options={gatewayLimitScopeOptions} />
            </Form.Item>
            <Space size={12} style={{ width: '100%' }} align="start">
              <Form.Item name="rateLimitMaxCallsPerMinute" label="每分钟上限" style={{ flex: 1 }}>
                <InputNumber min={1} precision={0} style={{ width: '100%' }} />
              </Form.Item>
              <Form.Item name="rateLimitMaxCallsPerHour" label="每小时上限" style={{ flex: 1 }}>
                <InputNumber min={1} precision={0} style={{ width: '100%' }} />
              </Form.Item>
              {getFieldValue('rateLimitMode') === 'gcra' ? (
                <Form.Item name="rateLimitBurst" label="突发容量" style={{ flex: 1 }}>
                  <InputNumber min={1} precision={0} style={{ width: '100%' }} />
                </Form.Item>
              ) : null}
            </Space>
          </>
        ) : null}
      </Form.Item>

      <Form.Item name="budgetEnabled" valuePropName="checked">
        <Checkbox>启用 budget</Checkbox>
      </Form.Item>
      <Form.Item noStyle shouldUpdate={(prev, next) => prev.budgetEnabled !== next.budgetEnabled}>
        {({ getFieldValue }) => getFieldValue('budgetEnabled') ? (
          <>
            <Form.Item name="budgetScope" label="预算维度">
              <Select options={gatewayLimitScopeOptions} />
            </Form.Item>
            <Space size={12} style={{ width: '100%' }} align="start">
              <Form.Item name="budgetMaxCallsPerDay" label="每日调用" style={{ flex: 1 }}>
                <InputNumber min={1} precision={0} style={{ width: '100%' }} />
              </Form.Item>
              <Form.Item name="budgetMaxTokensPerDay" label="每日 tokens" style={{ flex: 1 }}>
                <InputNumber min={1} precision={0} style={{ width: '100%' }} />
              </Form.Item>
              <Form.Item name="budgetMaxCostPerDay" label="每日成本" style={{ flex: 1 }}>
                <InputNumber min={0} precision={4} style={{ width: '100%' }} />
              </Form.Item>
            </Space>
          </>
        ) : null}
      </Form.Item>

      <Form.Item name="redactionEnabled" valuePropName="checked">
        <Checkbox>启用 redaction</Checkbox>
      </Form.Item>
      <Form.Item noStyle shouldUpdate={(prev, next) => prev.redactionEnabled !== next.redactionEnabled}>
        {({ getFieldValue }) => getFieldValue('redactionEnabled') ? (
          <>
            <Form.Item name="redactionMode" label="脱敏模式">
              <Select options={redactionModeOptions} />
            </Form.Item>
            <Form.Item name="redactionTarget" label="脱敏目标">
              <Select options={redactionTargetOptions} />
            </Form.Item>
            <Form.Item name="redactionFields" label="字段路径">
              <Select mode="tags" tokenSeparators={[',', ' ']} placeholder="例如 metadata.apiToken" />
            </Form.Item>
            <Form.Item name="redactionAllowFields" label="例外字段">
              <Select mode="tags" tokenSeparators={[',', ' ']} placeholder="例如 search" />
            </Form.Item>
            <Form.Item name="redactionSecretTypes" label="Secret classifiers">
              <Select mode="multiple" options={gatewaySecretTypeOptions} />
            </Form.Item>
            <Form.Item name="redactionValuePatterns" label="值正则">
              <Select mode="tags" tokenSeparators={[',']} placeholder="例如 APP-[0-9]{4}" />
            </Form.Item>
            <Space size={12} style={{ width: '100%' }} align="start">
              <Form.Item name="redactionReplacement" label="替换值" style={{ flex: 1 }}>
                <Input />
              </Form.Item>
              <Form.Item name="redactionPreserveFormat" valuePropName="checked" label="格式保留" style={{ flex: 1 }}>
                <Checkbox>保留尾部</Checkbox>
              </Form.Item>
            </Space>
            <Form.Item name="outputRedactionFields" label="输出脱敏字段">
              <Select mode="tags" tokenSeparators={[',', ' ']} placeholder="例如 application.buildSources.*.config.token" />
            </Form.Item>
            <Form.Item name="outputRedactionSecretTypes" label="输出 Secret classifiers">
              <Select mode="multiple" options={gatewaySecretTypeOptions} />
            </Form.Item>
            <Form.Item name="outputRedactionValuePatterns" label="输出值正则">
              <Select mode="tags" tokenSeparators={[',']} placeholder="例如 token=[A-Za-z0-9_-]{16,}" />
            </Form.Item>
            <Space size={12} style={{ width: '100%' }} align="start">
              <Form.Item name="outputRedactionReplacement" label="输出替换值" style={{ flex: 1 }}>
                <Input />
              </Form.Item>
              <Form.Item name="outputRedactionPreserveFormat" valuePropName="checked" label="输出格式保留" style={{ flex: 1 }}>
                <Checkbox>保留尾部</Checkbox>
              </Form.Item>
            </Space>
          </>
        ) : null}
      </Form.Item>
    </>
  )
}

export function AIGatewayPage() {
  const { message, modal } = App.useApp()
  const [form] = Form.useForm()
  const [decisionForm] = Form.useForm()
  const queryClient = useQueryClient()
  const navigate = useNavigate()
  const [drawer, setDrawer] = useState<DrawerState | null>(null)
  const [oneTimeToken, setOneTimeToken] = useState<{ title: string; value: string; prefix?: string } | null>(null)
  const [decisionTarget, setDecisionTarget] = useState<{ action: 'approve' | 'reject' | 'cancel'; record: ApprovalRequest } | null>(null)
  const location = useLocation()
  const [searchParams] = useSearchParams()
  const section = gatewaySectionFromPath(location.pathname)
  const requestedTab = normalizeGatewayTabKey(searchParams.get('tab'))
  const focusedApprovalRequestId = searchParams.get('approvalRequestId')?.trim() ?? ''
  const [activeTab, setActiveTab] = useState<GatewayTabKey>(() => (
    requestedTab && gatewayTabBelongsToSection(requestedTab, section)
      ? requestedTab
      : defaultGatewayTabForSection(section, focusedApprovalRequestId)
  ))
  const [manifestFilters, setManifestFilters] = useState({ aiClientId: '', skillId: '', source: 'console' })
  const [auditFilters, setAuditFilters] = useState<AuditFilterState>({ actor: '', aiClientId: '', toolName: '', action: '', riskLevel: '', result: '', from: '', to: '' })
  const [approvalFilters, setApprovalFilters] = useState<ApprovalFilterState>({ id: focusedApprovalRequestId, status: focusedApprovalRequestId ? '' : 'pending', actor: '', aiClientId: '', toolName: '', riskLevel: '', strategy: '', from: '', to: '' })
  const [clientFilter, setClientFilter] = useState('')
  const [tokenFilter, setTokenFilter] = useState('')
  const [serviceTokenFilter, setServiceTokenFilter] = useState('')
  const [policyFilter, setPolicyFilter] = useState('')
  const [grantFilter, setGrantFilter] = useState('')
  const [governanceWindowHours, setGovernanceWindowHours] = useState('24')
  const permissionSnapshot = usePermissionSnapshot()
  const snapshot = permissionSnapshot.data?.data
  const canView = hasPermission(snapshot, 'ai.gateway.view')
  const canManage = hasPermission(snapshot, 'ai.gateway.manage')
  const canInvoke = hasPermission(snapshot, 'ai.gateway.invoke')
  const canUseGateway = canView || canInvoke || canManage

  const clientsQuery = useQuery({
    queryKey: ['ai-gateway', 'ai-clients'],
    queryFn: () => api.get<ApiResponse<AIClient[]>>('/ai-gateway/ai-clients'),
    enabled: canManage,
  })
  const personalTokensQuery = useQuery({
    queryKey: ['ai-gateway', 'personal-access-tokens'],
    queryFn: () => api.get<ApiResponse<PersonalAccessToken[]>>('/ai-gateway/personal-access-tokens'),
    enabled: canView,
  })
  const serviceAccountsQuery = useQuery({
    queryKey: ['ai-gateway', 'service-accounts'],
    queryFn: () => api.get<ApiResponse<ServiceAccount[]>>('/ai-gateway/service-accounts'),
    enabled: canManage,
  })
  const serviceAccountTokensQuery = useQuery({
    queryKey: ['ai-gateway', 'service-account-tokens'],
    queryFn: () => api.get<ApiResponse<ServiceAccountToken[]>>('/ai-gateway/service-account-tokens'),
    enabled: canManage,
  })
  const grantsQuery = useQuery({
    queryKey: ['ai-gateway', 'tool-grants'],
    queryFn: () => api.get<ApiResponse<ToolGrant[]>>('/ai-gateway/tool-grants'),
    enabled: canManage,
  })
  const policiesQuery = useQuery({
    queryKey: ['ai-gateway', 'access-policies'],
    queryFn: () => api.get<ApiResponse<AccessPolicy[]>>('/ai-gateway/access-policies?includeDisabled=true'),
    enabled: canManage,
  })
  const bindingsQuery = useQuery({
    queryKey: ['ai-gateway', 'skill-bindings'],
    queryFn: () => api.get<ApiResponse<SkillBinding[]>>('/ai-gateway/skill-bindings?includeDisabled=true'),
    enabled: canManage,
  })
  const manifestQuery = useQuery({
    queryKey: ['ai-gateway', 'capabilities', manifestFilters],
    queryFn: () => api.get<ApiResponse<GatewayManifest>>(`/ai-gateway/capabilities${queryString(manifestFilters)}`),
    enabled: canView,
  })
  const auditQuery = useQuery({
    queryKey: ['ai-gateway', 'audit-logs', auditFilters],
    queryFn: () => api.get<ApiResponse<GatewayAuditLog[]>>(`/ai-gateway/audit-logs${queryString({ ...auditFilters, actorId: auditFilters.actor, actor: undefined })}`),
    enabled: canManage,
  })
  const approvalsQuery = useQuery({
    queryKey: ['ai-gateway', 'approval-requests', approvalFilters],
    queryFn: () => api.get<ApiResponse<ApprovalRequest[]>>(`/ai-gateway/approval-requests${queryString({ ...approvalFilters, actorId: approvalFilters.actor, actor: undefined })}`),
    enabled: canManage,
  })
  const governanceQuery = useQuery({
    queryKey: ['ai-gateway', 'governance-status', governanceWindowHours],
    queryFn: () => api.get<ApiResponse<GovernanceStatus>>(`/ai-gateway/governance/status${queryString({ windowHours: governanceWindowHours })}`),
    enabled: canManage,
  })

  const clients = clientsQuery.data?.data ?? []
  const personalTokens = personalTokensQuery.data?.data ?? []
  const serviceAccounts = serviceAccountsQuery.data?.data ?? []
  const serviceAccountTokens = serviceAccountTokensQuery.data?.data ?? []
  const grants = grantsQuery.data?.data ?? []
  const policies = policiesQuery.data?.data ?? []
  const bindings = bindingsQuery.data?.data ?? []
  const manifest = manifestQuery.data?.data
  const auditLogs = auditQuery.data?.data ?? []
  const approvalRequests = approvalsQuery.data?.data ?? []
  const governanceStatus = governanceQuery.data?.data
  const filteredClients = useMemo(() => clients.filter((item) => recordMatchesText([item.id, item.name, item.kind, item.status], clientFilter)), [clientFilter, clients])
  const filteredPersonalTokens = useMemo(() => personalTokens.filter((item) => recordMatchesText([item.id, item.name, item.userId, item.tokenPrefix, ...(item.permissionKeys ?? []), ...(item.scopes ?? [])], tokenFilter)), [personalTokens, tokenFilter])
  const filteredServiceAccountTokens = useMemo(() => serviceAccountTokens.filter((item) => recordMatchesText([item.id, item.name, item.serviceAccountId, item.tokenPrefix, ...(item.permissionKeys ?? []), ...(item.scopes ?? [])], serviceTokenFilter)), [serviceAccountTokens, serviceTokenFilter])
  const filteredPolicies = useMemo(() => policies.filter((item) => recordMatchesText([item.id, item.name, item.subjectType, item.subjectId, item.aiClientId, ...(item.toolPatterns ?? []), ...(item.skillIds ?? [])], policyFilter)), [policies, policyFilter])
  const filteredGrants = useMemo(() => grants.filter((item) => recordMatchesText([item.id, item.subjectType, item.subjectId, item.aiClientId, item.toolName], grantFilter)), [grantFilter, grants])

  useEffect(() => {
    if (!drawer) {
      form.resetFields()
      return
    }
    const record = drawer.record as any
    const initialValues = drawer.initialValues ?? {}
    form.resetFields()
    if (!record) {
      form.setFieldsValue({ ...defaultFormValues(drawer.kind), ...initialValues })
      return
    }
    form.setFieldsValue(drawer.kind === 'access-policy'
      ? { ...accessPolicyFormValuesFromRecord(record as AccessPolicy), ...initialValues }
      : {
          ...record,
          redirectUris: record.redirectUris,
          allowedOrigins: record.allowedOrigins,
          roleIds: record.roleIds,
          teamIds: record.teamIds,
          scopeGrantIds: record.scopeGrantIds,
          permissionKeys: record.permissionKeys,
          capabilityRefs: record.capabilityRefs,
          ...scopeValuesFromRecord(record.resourceScopes),
          ...initialValues,
        })
  }, [drawer, form])

  useEffect(() => {
    if (!focusedApprovalRequestId) return
    setActiveTab('approvals')
    setApprovalFilters((prev) => ({
      ...prev,
      id: focusedApprovalRequestId,
      status: '',
    }))
  }, [focusedApprovalRequestId])

  useEffect(() => {
    const nextTab = requestedTab && gatewayTabBelongsToSection(requestedTab, section)
      ? requestedTab
      : defaultGatewayTabForSection(section, focusedApprovalRequestId)
    setActiveTab((current) => {
      if (requestedTab) {
        return current === requestedTab ? current : nextTab
      }
      if (focusedApprovalRequestId && current !== 'approvals') {
        return 'approvals'
      }
      return gatewayTabBelongsToSection(current, section) ? current : nextTab
    })
  }, [focusedApprovalRequestId, requestedTab, section])

  const applyGovernanceDrilldown = (target: GovernanceDrilldownTarget) => {
    const targetSection = gatewayTabSectionMap[target.tab]
    if (targetSection !== section) {
      navigate(gatewaySectionPaths[targetSection])
    }
    setActiveTab(target.tab)
    if (target.approvalFilters) {
      setApprovalFilters((prev) => ({ ...prev, ...target.approvalFilters }))
    }
    if (target.auditFilters) {
      setAuditFilters((prev) => ({ ...prev, ...target.auditFilters }))
    }
    if (target.clientFilter !== undefined) {
      setClientFilter(target.clientFilter)
    }
    if (target.tokenFilter !== undefined) {
      setTokenFilter(target.tokenFilter)
    }
    if (target.serviceTokenFilter !== undefined) {
      setServiceTokenFilter(target.serviceTokenFilter)
    }
    if (target.policyFilter !== undefined) {
      setPolicyFilter(target.policyFilter)
    }
    if (target.grantFilter !== undefined) {
      setGrantFilter(target.grantFilter)
    }
    if (target.serviceTokenRevokeId) {
      setDrawer({ kind: 'service-token-revoke', initialValues: { tokenId: target.serviceTokenRevokeId } })
    }
    if (target.policyDraft) {
      setDrawer({ kind: 'access-policy', initialValues: target.policyDraft })
    }
  }

  const refreshAll = () => queryClient.invalidateQueries({ queryKey: ['ai-gateway'] })

  const upsertMutation = useMutation({
    mutationFn: async (values: Record<string, any>) => {
      if (!drawer) return null
      const record = drawer.record as any
      switch (drawer.kind) {
        case 'ai-client': {
          const payload = {
            id: values.id,
            name: values.name,
            kind: values.kind,
            status: values.status,
            redirectUris: values.redirectUris ?? [],
            allowedOrigins: values.allowedOrigins ?? [],
          }
          return record?.id
            ? api.put<ApiResponse<AIClient>>(`/ai-gateway/ai-clients/${record.id}`, payload)
            : api.post<ApiResponse<AIClient>>('/ai-gateway/ai-clients', payload)
        }
        case 'personal-token':
          return api.post<ApiResponse<CreatedPersonalAccessToken>>('/ai-gateway/personal-access-tokens', {
            name: values.name,
            scopes: values.scopes ?? [],
            permissionKeys: values.permissionKeys ?? [],
            expiresAt: values.expiresAt || undefined,
          })
        case 'service-account':
          return api.post<ApiResponse<ServiceAccount>>('/ai-gateway/service-accounts', {
            id: values.id,
            name: values.name,
            description: values.description,
            status: values.status,
            ownerUserId: values.ownerUserId,
            roleIds: values.roleIds ?? [],
            teamIds: values.teamIds ?? [],
            scopeGrantIds: values.scopeGrantIds ?? [],
          })
        case 'service-token':
          return api.post<ApiResponse<CreatedServiceAccountToken>>(`/ai-gateway/service-accounts/${record.id}/tokens`, {
            name: values.name,
            scopes: values.scopes ?? [],
            permissionKeys: values.permissionKeys ?? [],
            expiresAt: values.expiresAt || undefined,
          })
        case 'service-token-revoke':
          return api.post<ApiResponse<{ status: string }>>(`/ai-gateway/service-account-tokens/${values.tokenId}/revoke`)
        case 'tool-grant':
          return api.post<ApiResponse<ToolGrant>>('/ai-gateway/tool-grants', {
            subjectType: values.subjectType,
            subjectId: values.subjectId,
            aiClientId: values.aiClientId,
            toolName: values.toolName,
            effect: values.effect,
            riskLevel: values.riskLevel,
            permissionKeys: values.permissionKeys ?? [],
            resourceScopes: valuesToResourceScopes(values),
            requiresApproval: values.requiresApproval === 'true',
            expiresAt: values.expiresAt || undefined,
          })
        case 'access-policy': {
          const payload = {
            name: values.name,
            description: values.description,
            enabled: values.enabled === 'true',
            subjectType: values.subjectType,
            subjectId: values.subjectId,
            aiClientId: values.aiClientId,
            effect: values.effect,
            toolPatterns: values.toolPatterns ?? [],
            skillIds: values.skillIds ?? [],
            riskLevels: values.riskLevels ?? [],
            resourceScopes: valuesToResourceScopes(values),
            approvalPolicy: accessPolicyApprovalPolicyFromValues(values),
            conditions: accessPolicyConditionsFromValues(values),
          }
          return record?.id
            ? api.put<ApiResponse<AccessPolicy>>(`/ai-gateway/access-policies/${record.id}`, payload)
            : api.post<ApiResponse<AccessPolicy>>('/ai-gateway/access-policies', payload)
        }
        case 'skill-binding': {
          const payload = {
            subjectType: values.subjectType,
            subjectId: values.subjectId,
            aiClientId: values.aiClientId,
            skillId: values.skillId,
            capabilityRefs: values.capabilityRefs ?? [],
            enabled: values.enabled === 'true',
          }
          return record?.id
            ? api.put<ApiResponse<SkillBinding>>(`/ai-gateway/skill-bindings/${record.id}`, payload)
            : api.post<ApiResponse<SkillBinding>>('/ai-gateway/skill-bindings', payload)
        }
        default:
          return null
      }
    },
    onSuccess: (res: any) => {
      const value = res?.data?.value
      const token = res?.data?.token
      if (value) {
        setOneTimeToken({
          title: drawer?.kind === 'service-token' ? '服务账号 token' : 'Personal access token',
          value,
          prefix: token?.tokenPrefix,
        })
      }
      setDrawer(null)
      void refreshAll()
      message.success('已保存')
    },
    onError: (error: Error) => message.error(error.message),
  })

  const deleteMutation = useMutation({
    mutationFn: ({ kind, id }: { kind: 'grant' | 'policy' | 'binding' | 'personal-token'; id: string }) => {
      if (kind === 'grant') return api.delete(`/ai-gateway/tool-grants/${id}`)
      if (kind === 'policy') return api.delete(`/ai-gateway/access-policies/${id}`)
      if (kind === 'binding') return api.delete(`/ai-gateway/skill-bindings/${id}`)
      return api.post(`/ai-gateway/personal-access-tokens/${id}/revoke`)
    },
    onSuccess: () => {
      void refreshAll()
      message.success('已更新')
    },
    onError: (error: Error) => message.error(error.message),
  })

  const disableClientMutation = useMutation({
    mutationFn: (record: AIClient) => api.put<ApiResponse<AIClient>>(`/ai-gateway/ai-clients/${record.id}`, {
      id: record.id,
      name: record.name,
      kind: record.kind,
      status: 'disabled',
      redirectUris: record.redirectUris ?? [],
      allowedOrigins: record.allowedOrigins ?? [],
    }),
    onSuccess: () => {
      void refreshAll()
      message.success('已禁用')
    },
    onError: (error: Error) => message.error(error.message),
  })

  const decisionMutation = useMutation({
    mutationFn: ({ action, id, comment }: { action: 'approve' | 'reject' | 'cancel'; id: string; comment?: string }) =>
      api.post<ApiResponse<ApprovalDecisionResult>>(`/ai-gateway/approval-requests/${id}/${action}`, { comment }),
    onSuccess: (res) => {
      void refreshAll()
      setDecisionTarget(null)
      decisionForm.resetFields()
      const status = res.data.request.status
      message.success(status === 'executed' ? '已批准并执行' : '已更新审批请求')
    },
    onError: (error: Error) => message.error(error.message),
  })

  const summary = useMemo(() => ({
    clients: clients.length,
    serviceAccounts: serviceAccounts.length,
    grants: grants.length,
    policies: policies.length,
    bindings: bindings.length,
    tools: manifest?.summary.toolCount ?? 0,
  }), [bindings.length, clients.length, grants.length, manifest, policies.length, serviceAccounts.length])
  const gatewayPanelMeta = gatewaySectionMeta[section]
  const sectionActiveTab = gatewayTabBelongsToSection(activeTab, section)
    ? activeTab
    : defaultGatewayTabForSection(section, focusedApprovalRequestId)

  const confirmDelete = (kind: 'grant' | 'policy' | 'binding' | 'personal-token', id: string, title: string) => {
    modal.confirm({
      title,
      content: '该操作会立即更新 AI Gateway 控制面。',
      okText: '确认',
      cancelText: '取消',
      onOk: () => deleteMutation.mutate({ kind, id }),
    })
  }

  const clientOptions = clients.map((item) => ({ label: `${item.name} (${item.id})`, value: item.id }))
  const skillOptions = manifest?.skills?.map((item) => ({ label: `${item.name} (${item.id})`, value: item.id })) ?? []
  const toolOptions = manifest?.tools.map((item) => ({ label: item.name, value: item.name })) ?? []

  const aiClientColumns: TableColumnsType<AIClient> = [
    { title: 'Client', dataIndex: 'name', width: 220, render: (_, record) => <Space orientation="vertical" size={0}><Text strong>{record.name}</Text><Text type="secondary">{record.id}</Text></Space> },
    { title: '类型', dataIndex: 'kind', width: 140, render: (value) => <Tag>{value}</Tag> },
    { title: '状态', dataIndex: 'status', width: 110, render: (value) => <StatusTag value={value} /> },
    { title: 'Redirect URIs', dataIndex: 'redirectUris', render: (value) => compactList(value, 2) },
    { title: '更新时间', dataIndex: 'updatedAt', width: 140, render: formatDateTime },
    {
      title: '',
      key: 'actions',
      fixed: 'right',
      width: 120,
      render: (_, record) => (
        <Space>
          <Button size="small" icon={<EditOutlined />} disabled={!canManage} onClick={() => setDrawer({ kind: 'ai-client', record })} />
          <Button size="small" danger icon={<StopOutlined />} loading={disableClientMutation.isPending} disabled={!canManage || record.status === 'disabled'} onClick={() => disableClientMutation.mutate(record)} />
        </Space>
      ),
    },
  ]

  const tokenColumns: TableColumnsType<PersonalAccessToken> = [
    { title: '名称', dataIndex: 'name', width: 200, render: (_, record) => <Space orientation="vertical" size={0}><Text strong>{record.name}</Text><Text type="secondary">{record.tokenPrefix}</Text></Space> },
    { title: '权限', dataIndex: 'permissionKeys', render: (value) => compactList(value, 3) },
    { title: '过期', dataIndex: 'expiresAt', width: 140, render: formatDateTime },
    { title: '最近使用', dataIndex: 'lastUsedAt', width: 140, render: formatDateTime },
    { title: '状态', dataIndex: 'revokedAt', width: 110, render: (value) => <StatusTag value={value ? 'revoked' : 'active'} /> },
    { title: '', key: 'actions', fixed: 'right', width: 80, render: (_, record) => <Button size="small" danger icon={<StopOutlined />} disabled={!canInvoke || !!record.revokedAt} onClick={() => confirmDelete('personal-token', record.id, '吊销 personal access token')} /> },
  ]

  const serviceAccountColumns: TableColumnsType<ServiceAccount> = [
    { title: '服务账号', dataIndex: 'name', width: 240, render: (_, record) => <Space orientation="vertical" size={0}><Text strong>{record.name}</Text><Text type="secondary">{record.id}</Text></Space> },
    { title: '状态', dataIndex: 'status', width: 110, render: (value) => <StatusTag value={value} /> },
    { title: '角色', dataIndex: 'roleIds', render: (value) => compactList(value, 3) },
    { title: '用户组', dataIndex: 'teamIds', render: (value) => compactList(value, 2) },
    {
      title: '',
      key: 'actions',
      fixed: 'right',
      width: 150,
      render: (_, record) => (
        <Space>
          <Button size="small" icon={<KeyOutlined />} disabled={!canManage || record.status !== 'active'} onClick={() => setDrawer({ kind: 'service-token', record })} />
        </Space>
      ),
    },
  ]

  const serviceAccountTokenColumns: TableColumnsType<ServiceAccountToken> = [
    { title: 'Token', dataIndex: 'name', width: 240, render: (_, record) => <Space orientation="vertical" size={0}><Text strong>{record.name || record.id}</Text><Text type="secondary">{record.tokenPrefix || record.id}</Text></Space> },
    { title: '服务账号', dataIndex: 'serviceAccountId', width: 180, render: (value) => <Tag>{value}</Tag> },
    { title: '权限', dataIndex: 'permissionKeys', render: (value) => compactList(value, 3) },
    { title: 'Scopes', dataIndex: 'scopes', render: (value) => compactList(value, 2) },
    { title: '过期', dataIndex: 'expiresAt', width: 140, render: formatDateTime },
    { title: '最近使用', dataIndex: 'lastUsedAt', width: 140, render: formatDateTime },
    { title: '状态', dataIndex: 'revokedAt', width: 110, render: (value) => <StatusTag value={value ? 'revoked' : 'active'} /> },
    {
      title: '',
      key: 'actions',
      fixed: 'right',
      width: 90,
      render: (_, record) => (
        <Button
          size="small"
          danger
          icon={<StopOutlined />}
          disabled={!canManage || !!record.revokedAt}
          onClick={() => setDrawer({ kind: 'service-token-revoke', initialValues: { tokenId: record.id } })}
        />
      ),
    },
  ]

  const grantColumns: TableColumnsType<ToolGrant> = [
    { title: 'Subject', dataIndex: 'subjectId', width: 220, render: (_, record) => <Space orientation="vertical" size={0}><Text strong>{record.subjectId}</Text><Text type="secondary">{record.subjectType}{record.aiClientId ? ` / ${record.aiClientId}` : ''}</Text></Space> },
    { title: 'Tool', dataIndex: 'toolName', width: 240, render: (value) => <Tag>{value}</Tag> },
    { title: 'Effect', dataIndex: 'effect', width: 100, render: (value) => <StatusTag value={value} /> },
    { title: 'Risk', dataIndex: 'riskLevel', width: 100, render: (value) => <StatusTag value={value} /> },
    { title: 'Scope', dataIndex: 'resourceScopes', render: scopeSummary },
    { title: '', key: 'actions', fixed: 'right', width: 80, render: (_, record) => <Button size="small" danger icon={<DeleteOutlined />} disabled={!canManage} onClick={() => confirmDelete('grant', record.id, '删除 MCP tool grant')} /> },
  ]

  const policyColumns: TableColumnsType<AccessPolicy> = [
    { title: 'Policy', dataIndex: 'name', width: 240, render: (_, record) => <Space orientation="vertical" size={0}><Text strong>{record.name}</Text><Text type="secondary">{record.id}</Text></Space> },
    { title: 'Subject', dataIndex: 'subjectId', width: 180, render: (_, record) => <Text>{record.subjectType}:{record.subjectId}</Text> },
    { title: 'Effect', dataIndex: 'effect', width: 100, render: (value) => <StatusTag value={value} /> },
    { title: 'Risk', dataIndex: 'riskLevels', width: 160, render: (value) => compactList(value, 3) },
    { title: 'Conditions', dataIndex: 'conditions', width: 220, render: policyConditionSummary },
    { title: 'Scope', dataIndex: 'resourceScopes', render: scopeSummary },
    { title: '启用', dataIndex: 'enabled', width: 90, render: (value) => <StatusTag value={value ? 'enabled' : 'disabled'} /> },
    { title: '', key: 'actions', fixed: 'right', width: 120, render: (_, record) => <Space><Button size="small" icon={<EditOutlined />} disabled={!canManage} onClick={() => setDrawer({ kind: 'access-policy', record })} /><Button size="small" danger icon={<DeleteOutlined />} disabled={!canManage} onClick={() => confirmDelete('policy', record.id, '删除 access policy')} /></Space> },
  ]

  const bindingColumns: TableColumnsType<SkillBinding> = [
    { title: 'Subject', dataIndex: 'subjectId', width: 220, render: (_, record) => <Space orientation="vertical" size={0}><Text strong>{record.subjectId}</Text><Text type="secondary">{record.subjectType}{record.aiClientId ? ` / ${record.aiClientId}` : ''}</Text></Space> },
    { title: 'Skill', dataIndex: 'skillId', width: 180, render: (value) => <Tag>{value}</Tag> },
    { title: 'Capabilities', dataIndex: 'capabilityRefs', render: (value) => compactList(value, 4) },
    { title: '启用', dataIndex: 'enabled', width: 90, render: (value) => <StatusTag value={value ? 'enabled' : 'disabled'} /> },
    { title: '', key: 'actions', fixed: 'right', width: 120, render: (_, record) => <Space><Button size="small" icon={<EditOutlined />} disabled={!canManage} onClick={() => setDrawer({ kind: 'skill-binding', record })} /><Button size="small" danger icon={<DeleteOutlined />} disabled={!canManage} onClick={() => confirmDelete('binding', record.id, '删除 skill binding')} /></Space> },
  ]

  const toolColumns: TableColumnsType<GatewayTool> = [
    { title: 'Tool', dataIndex: 'name', width: 280, render: (_, record) => <Space orientation="vertical" size={0}><Text strong>{record.name}</Text><Text type="secondary">{record.title}</Text></Space> },
    { title: 'Domain', dataIndex: 'domain', width: 120, render: (value) => <Tag>{value}</Tag> },
    { title: 'Action', dataIndex: 'action', width: 110 },
    { title: 'Risk', dataIndex: 'riskLevel', width: 100, render: (value) => <StatusTag value={value} /> },
    { title: 'Approval', dataIndex: 'requiresApproval', width: 110, render: (value) => <StatusTag value={value ? 'required' : 'none'} /> },
    { title: 'Scopes', dataIndex: 'requiredScopes', render: (value) => compactList(value, 4) },
  ]

  const auditColumns: TableColumnsType<GatewayAuditLog> = [
    { title: '时间', dataIndex: 'createdAt', width: 140, render: formatDateTime },
    { title: '调用者', dataIndex: 'actorId', width: 210, render: (_, record) => auditCaller(record) },
    { title: '调用入口', dataIndex: 'aiClientId', width: 200, render: (_, record) => auditEntryPoint(record) },
    { title: '调用内容', dataIndex: 'toolName', width: 340, render: (_, record) => auditInvocation(record) },
    { title: '结果', dataIndex: 'result', width: 120, render: (_, record) => auditResult(record) },
    { title: '摘要', dataIndex: 'summary', render: (value) => <Paragraph style={{ marginBottom: 0 }} ellipsis={{ rows: 2, tooltip: value }}>{value}</Paragraph> },
  ]

  const approvalColumns: TableColumnsType<ApprovalRequest> = [
    { title: '创建时间', dataIndex: 'createdAt', width: 140, render: formatDateTime },
    { title: '状态', dataIndex: 'status', width: 110, render: (value) => <StatusTag value={value} /> },
    { title: 'Actor', dataIndex: 'actorId', width: 190, render: (_, record) => <Space orientation="vertical" size={0}><Text strong>{record.actorName || record.actorId}</Text><Text type="secondary">{record.actorType}:{record.actorId}</Text></Space> },
    { title: 'Client / Skill', dataIndex: 'aiClientId', width: 180, render: (_, record) => <Space orientation="vertical" size={0}><Text>{record.aiClientName || record.aiClientId || '-'}</Text><Text type="secondary">{record.skillId || '-'}</Text></Space> },
    { title: 'Tool', dataIndex: 'toolName', width: 240, render: (value) => <Tag>{value}</Tag> },
    { title: 'Risk', dataIndex: 'riskLevel', width: 100, render: (value) => <StatusTag value={value} /> },
    { title: '策略', dataIndex: 'strategy', width: 170, render: (value) => <Tag>{value}</Tag> },
    {
      title: 'Trace',
      key: 'trace',
      width: 190,
      render: (_, record) => {
        const trace = approvalTrace(record)
        return trace.workflowRunId ? (
          <Space orientation="vertical" size={0}>
            <Button size="small" type="link" icon={<LinkOutlined />} onClick={() => navigate(workflowTracePath(trace))}>
              {trace.workflowRunId}
            </Button>
            <Text type="secondary">{trace.approvalRequestId}</Text>
          </Space>
        ) : <Text type="secondary">-</Text>
      },
    },
    { title: '过期', dataIndex: 'expiresAt', width: 140, render: formatDateTime },
    { title: '摘要', dataIndex: 'summary', render: (value) => <Paragraph style={{ marginBottom: 0 }} ellipsis={{ rows: 2, tooltip: value }}>{value}</Paragraph> },
    {
      title: '',
      key: 'actions',
      fixed: 'right',
      width: 150,
      render: (_, record) => (
        <Space>
          <Button size="small" icon={<CheckOutlined />} disabled={!canManage || !isPendingApproval(record)} loading={decisionMutation.isPending} onClick={() => setDecisionTarget({ action: 'approve', record })} />
          <Button size="small" danger icon={<CloseOutlined />} disabled={!canManage || !isPendingApproval(record)} loading={decisionMutation.isPending} onClick={() => setDecisionTarget({ action: 'reject', record })} />
          <Button size="small" icon={<StopOutlined />} disabled={!canManage || !isPendingApproval(record)} loading={decisionMutation.isPending} onClick={() => setDecisionTarget({ action: 'cancel', record })} />
        </Space>
      ),
    },
  ]

  const governanceHealthColumns: TableColumnsType<GovernanceHealthCheck> = [
    { title: 'Check', dataIndex: 'name', width: 220, render: (value) => <Tag>{value}</Tag> },
    { title: 'Status', dataIndex: 'status', width: 120, render: (value) => <StatusTag value={value} /> },
    { title: 'Count', dataIndex: 'count', width: 90, render: (value) => value ?? 0 },
    { title: 'Message', dataIndex: 'message', render: (value) => <Paragraph style={{ marginBottom: 0 }} ellipsis={{ rows: 2, tooltip: value }}>{value}</Paragraph> },
  ]

  const governanceCoverageColumns: TableColumnsType<ReturnType<typeof governanceCoverageRows>[number]> = [
    { title: 'Control', dataIndex: 'label', width: 180 },
    { title: 'State', dataIndex: 'state', width: 150, render: (value) => <StatusTag value={value} /> },
    { title: 'Configured', dataIndex: 'configured', width: 120 },
    { title: 'Total', dataIndex: 'total', width: 100 },
    {
      title: '',
      key: 'actions',
      width: 130,
      render: (_, record) => (
        <Button size="small" type="link" onClick={() => applyGovernanceDrilldown(governanceCoverageDrilldown(record))}>
          {['access_policies', 'budget', 'rate_limit', 'redaction', 'resource_scopes'].includes(record.key) ? '创建 policy' : '定位'}
        </Button>
      ),
    },
  ]

  const governanceFindingColumns: TableColumnsType<GovernanceFinding> = [
    { title: 'Severity', dataIndex: 'severity', width: 120, render: (value) => <StatusTag value={value} /> },
    { title: 'Type', dataIndex: 'type', width: 250, render: (value) => <Tag>{value}</Tag> },
    { title: 'Count', dataIndex: 'count', width: 90, render: (value) => value ?? 1 },
    { title: 'Risk', dataIndex: 'riskLevel', width: 100, render: (value) => value ? <StatusTag value={value} /> : '-' },
    { title: 'Target', key: 'target', width: 320, render: (_, record) => governanceFindingTarget(record) },
    { title: 'Summary', dataIndex: 'summary', render: (value) => <Paragraph style={{ marginBottom: 0 }} ellipsis={{ rows: 2, tooltip: value }}>{value}</Paragraph> },
    {
      title: '',
      key: 'actions',
      fixed: 'right',
      width: 230,
      render: (_, record) => (
        <Space wrap size={4}>
          {governanceFindingDrilldownActions(record).slice(0, 4).map((action) => (
            <Button key={`${record.type}:${action.label}`} size="small" type="link" onClick={() => applyGovernanceDrilldown(action.target)}>
              {action.label}
            </Button>
          ))}
        </Space>
      ),
    },
  ]

  const governanceMetricColumns: TableColumnsType<GovernanceMetricCount> = [
    { title: 'Key', dataIndex: 'key', render: (value) => <Tag>{value}</Tag> },
    { title: 'Count', dataIndex: 'count', width: 90 },
  ]

  const governanceRedactionColumns: TableColumnsType<GovernanceRedactionRow> = [
    { title: 'Dimension', dataIndex: 'label', width: 160 },
    { title: 'Count', dataIndex: 'count', width: 100 },
    { title: 'Top values', dataIndex: 'items', render: (items: GovernanceMetricCount[]) => compactList(items.map((item) => `${item.key}:${item.count}`), 6) },
    {
      title: '',
      key: 'actions',
      width: 120,
      render: (_, record) => record.target ? (
        <Button size="small" type="link" onClick={() => applyGovernanceDrilldown(record.target!)}>
          定位
        </Button>
      ) : <Text type="secondary">-</Text>,
    },
  ]

  const governanceQueueColumns: TableColumnsType<ReturnType<typeof governanceApprovalQueueRows>[number]> = [
    { title: 'Queue', dataIndex: 'label', width: 220 },
    { title: 'Count', dataIndex: 'count', width: 90 },
    {
      title: 'IDs',
      dataIndex: 'refs',
      render: (value: string[], record) => value?.length ? (
        <Space size={[4, 4]} wrap>
          {value.slice(0, 5).map((item) => (
            <Button key={item} size="small" type="link" onClick={() => applyGovernanceDrilldown(governanceQueueDrilldown(record, item))}>
              {item}
            </Button>
          ))}
          {value.length > 5 ? <Tag>+{value.length - 5}</Tag> : null}
        </Space>
      ) : <Text type="secondary">-</Text>,
    },
    {
      title: '',
      key: 'actions',
      width: 120,
      render: (_, record) => (
        <Button
          size="small"
          type="link"
          disabled={!record.refs.length}
          onClick={() => applyGovernanceDrilldown(governanceQueueDrilldown(record, record.refs[0] ?? ''))}
        >
          定位
        </Button>
      ),
    },
  ]

  const governanceTokenFindingColumns: TableColumnsType<GovernanceTokenFindingRow> = [
    { title: 'Category', dataIndex: 'categoryLabel', width: 150, render: (value) => <Tag>{value}</Tag> },
    { title: 'Severity', dataIndex: 'severity', width: 120, render: (value) => <StatusTag value={value} /> },
    { title: 'Token', dataIndex: 'name', width: 260, render: (_, record) => <Space orientation="vertical" size={0}><Text strong>{record.name || record.id}</Text><Text type="secondary">{record.kind} / {record.tokenPrefix || record.id}</Text></Space> },
    { title: 'Owner', dataIndex: 'ownerId', width: 180, render: (value) => value || '-' },
    { title: 'Timing', key: 'timing', width: 220, render: (_, record) => governanceTokenTiming(record) },
    { title: 'Message', dataIndex: 'message', render: (value) => <Paragraph style={{ marginBottom: 0 }} ellipsis={{ rows: 2, tooltip: value }}>{value}</Paragraph> },
    {
      title: '',
      key: 'actions',
      fixed: 'right',
      width: 150,
      render: (_, record) => (
        <Button size="small" type="link" onClick={() => applyGovernanceDrilldown(governanceTokenFindingDrilldown(record))}>
          {record.kind === 'service_account_token' ? '吊销 token' : '查看 PAT'}
        </Button>
      ),
    },
  ]

  const governanceRecommendationColumns: TableColumnsType<GovernanceRecommendationAction> = [
    { title: 'Severity', dataIndex: 'severity', width: 110, render: (value) => <StatusTag value={value} /> },
    { title: 'Type', dataIndex: 'type', width: 210, render: (value) => <Tag>{value}</Tag> },
    { title: 'Action', dataIndex: 'action', width: 230, render: (value) => <Tag>{value}</Tag> },
    { title: 'Target', key: 'target', width: 210, render: (_, record) => compactList([record.targetKind, record.targetId, ...(record.refs ?? []).slice(0, 2)].flatMap((item) => item ? [item] : []), 3) },
    { title: 'Summary', dataIndex: 'summary', render: (value) => <Paragraph style={{ marginBottom: 0 }} ellipsis={{ rows: 2, tooltip: value }}>{value}</Paragraph> },
    {
      title: '',
      key: 'actions',
      fixed: 'right',
      width: 130,
      render: (_, record) => {
        const action = governanceRecommendationDrilldownAction(record)
        return action ? (
          <Button size="small" type="link" onClick={() => applyGovernanceDrilldown(action.target)}>
            {action.label}
          </Button>
        ) : <Text type="secondary">-</Text>
      },
    },
  ]

  if (!canUseGateway && !permissionSnapshot.isLoading) {
    return <div className="soha-page"><Alert type="warning" title="当前账号没有 AI Gateway 权限。" /></div>
  }

  return (
    <div className="soha-page">
      <section className="soha-page-section">
        <Space orientation="vertical" size={4}>
          <Text type="secondary">AI Gateway</Text>
          <Typography.Title level={3} style={{ margin: 0 }}>企业 AI 运维控制面</Typography.Title>
          <Paragraph type="secondary" style={{ marginBottom: 0 }}>
            统一管理 AI client、token、service account、tool grant、access policy、skill binding、审批与调用日志。
          </Paragraph>
        </Space>
      </section>

      <Card
        title={cardTitle(<SafetyCertificateOutlined />, gatewayPanelMeta.title)}
        extra={<Button size="small" icon={<ReloadOutlined />} onClick={() => void refreshAll()}>刷新</Button>}
      >
        <Space orientation="vertical" size={12} style={{ width: '100%' }}>
          <Text type="secondary">{gatewayPanelMeta.description}</Text>
          {section === 'tokens' || section === 'governance' || section === 'call-logs' ? (
            <Text type="secondary">{gatewayMenuMeta[sectionActiveTab].description}</Text>
          ) : null}
          {section === 'overview' ? (
            <Space orientation="vertical" size={12} style={{ width: '100%' }}>
              <div className="grid gap-3 lg:grid-cols-2">
                <Descriptions size="small" column={2} bordered items={[
                  { key: 'tools', label: 'Visible tools', children: summary.tools },
                  { key: 'clients', label: 'AI clients', children: summary.clients },
                  { key: 'tokens', label: 'PAT', children: personalTokens.length },
                  { key: 'serviceAccounts', label: 'Service accounts', children: summary.serviceAccounts },
                ]} />
                <Descriptions size="small" column={2} bordered items={[
                  { key: 'policies', label: 'Policies', children: summary.policies },
                  { key: 'grants', label: 'Tool grants', children: summary.grants },
                  { key: 'bindings', label: 'Skill bindings', children: summary.bindings },
                  { key: 'approvals', label: 'Approvals', children: approvalRequests.length },
                ]} />
              </div>
              <Space wrap>
                <Button icon={<SafetyCertificateOutlined />} onClick={() => navigate(gatewaySectionPaths.manifest)}>能力清单</Button>
                <Button icon={<LinkOutlined />} disabled={!canManage} onClick={() => navigate(gatewaySectionPaths.clients)}>AI Clients</Button>
                <Button icon={<KeyOutlined />} onClick={() => navigate(gatewaySectionPaths.tokens)}>Tokens</Button>
                <Button icon={<StopOutlined />} disabled={!canManage} onClick={() => navigate(gatewaySectionPaths.governance)}>Governance</Button>
                <Button icon={<HistoryOutlined />} disabled={!canManage} onClick={() => navigate(gatewaySectionPaths['call-logs'])}>调用日志</Button>
              </Space>
            </Space>
          ) : (
            <Tabs
              activeKey={sectionActiveTab}
              onChange={(key) => setActiveTab(key as GatewayTabKey)}
              renderTabBar={section === 'manifest' || section === 'clients' || section === 'call-logs' ? () => <></> : undefined}
              items={[
            {
              key: 'manifest',
              label: 'Manifest',
              children: (
                <Space orientation="vertical" size={12} style={{ width: '100%' }}>
                  <Space wrap>
                    <Select allowClear style={{ width: 260 }} placeholder="AI client" options={clientOptions} value={manifestFilters.aiClientId || undefined} onChange={(value) => setManifestFilters((prev) => ({ ...prev, aiClientId: value ?? '' }))} />
                    <Select allowClear style={{ width: 260 }} placeholder="Skill" options={skillOptions} value={manifestFilters.skillId || undefined} onChange={(value) => setManifestFilters((prev) => ({ ...prev, skillId: value ?? '' }))} />
                    <Input style={{ width: 180 }} placeholder="source" value={manifestFilters.source} onChange={(event) => setManifestFilters((prev) => ({ ...prev, source: event.target.value }))} />
                    <Button icon={<ReloadOutlined />} onClick={() => void manifestQuery.refetch()}>刷新</Button>
                  </Space>
                  {manifest ? (
                    <>
                      <Descriptions size="small" column={4} bordered items={[
                        { key: 'principal', label: 'Subject', children: manifest.principal?.userName || manifest.principal?.userId || '-' },
                        { key: 'roles', label: 'Roles', children: compactList(manifest.principal?.roles, 2) },
                        { key: 'permissions', label: 'Permissions', children: manifest.permissionKeys.length },
                        { key: 'denied', label: 'Denied', children: manifest.summary.deniedCount },
                      ]} />
                      <Table rowKey="name" size="small" columns={toolColumns} dataSource={manifest.tools} loading={manifestQuery.isLoading} pagination={{ pageSize: 8 }} scroll={{ x: 960 }} />
                    </>
                  ) : <Empty />}
                </Space>
              ),
            },
            {
              key: 'clients',
              label: 'AI Clients',
              children: (
                <Table
                  rowKey="id"
                  size="small"
                  columns={aiClientColumns}
                  dataSource={filteredClients}
                  loading={clientsQuery.isLoading}
                  scroll={{ x: 920 }}
                  title={() => (
                    <Space wrap>
                      <Button type="primary" size="small" icon={<PlusOutlined />} disabled={!canManage} onClick={() => setDrawer({ kind: 'ai-client' })}>新增 client</Button>
                      <Input allowClear size="small" style={{ width: 220 }} placeholder="过滤 client" value={clientFilter} onChange={(event) => setClientFilter(event.target.value)} />
                    </Space>
                  )}
                />
              ),
            },
            {
              key: 'tokens',
              label: 'Tokens',
              children: (
                <Table
                  rowKey="id"
                  size="small"
                  columns={tokenColumns}
                  dataSource={filteredPersonalTokens}
                  loading={personalTokensQuery.isLoading}
                  scroll={{ x: 900 }}
                  title={() => (
                    <Space wrap>
                      <Button type="primary" size="small" icon={<PlusOutlined />} disabled={!canInvoke} onClick={() => setDrawer({ kind: 'personal-token' })}>创建 PAT</Button>
                      <Input allowClear size="small" style={{ width: 240 }} placeholder="过滤 token" value={tokenFilter} onChange={(event) => setTokenFilter(event.target.value)} />
                    </Space>
                  )}
                />
              ),
            },
            {
              key: 'service-accounts',
              label: 'Service Accounts',
              children: (
                <Space orientation="vertical" size={12} style={{ width: '100%' }}>
                  <Table
                    rowKey="id"
                    size="small"
                    columns={serviceAccountColumns}
                    dataSource={serviceAccounts}
                    loading={serviceAccountsQuery.isLoading}
                    scroll={{ x: 860 }}
                    title={() => (
                      <Space wrap>
                        <Button type="primary" size="small" icon={<PlusOutlined />} disabled={!canManage} onClick={() => setDrawer({ kind: 'service-account' })}>新增服务账号</Button>
                      </Space>
                    )}
                  />
                  <Table
                    rowKey="id"
                    size="small"
                    columns={serviceAccountTokenColumns}
                    dataSource={filteredServiceAccountTokens}
                    loading={serviceAccountTokensQuery.isLoading}
                    scroll={{ x: 1180 }}
                    title={() => (
                      <Space wrap>
                        <Button size="small" danger icon={<StopOutlined />} disabled={!canManage} onClick={() => setDrawer({ kind: 'service-token-revoke' })}>吊销服务 token</Button>
                        <Input allowClear size="small" style={{ width: 260 }} placeholder="过滤 service token" value={serviceTokenFilter} onChange={(event) => setServiceTokenFilter(event.target.value)} />
                      </Space>
                    )}
                  />
                </Space>
              ),
            },
            {
              key: 'grants',
              label: 'Tool Grants',
              children: <Table rowKey="id" size="small" columns={grantColumns} dataSource={filteredGrants} loading={grantsQuery.isLoading} scroll={{ x: 1000 }} title={() => (
                <Space wrap>
                  <Button type="primary" size="small" icon={<PlusOutlined />} disabled={!canManage} onClick={() => setDrawer({ kind: 'tool-grant' })}>新增 grant</Button>
                  <Input allowClear size="small" style={{ width: 240 }} placeholder="过滤 grant" value={grantFilter} onChange={(event) => setGrantFilter(event.target.value)} />
                </Space>
              )} />,
            },
            {
              key: 'policies',
              label: 'Access Policies',
              children: <Table rowKey="id" size="small" columns={policyColumns} dataSource={filteredPolicies} loading={policiesQuery.isLoading} scroll={{ x: 1080 }} title={() => (
                <Space wrap>
                  <Button type="primary" size="small" icon={<PlusOutlined />} disabled={!canManage} onClick={() => setDrawer({ kind: 'access-policy' })}>新增 policy</Button>
                  <Input allowClear size="small" style={{ width: 240 }} placeholder="过滤 policy" value={policyFilter} onChange={(event) => setPolicyFilter(event.target.value)} />
                </Space>
              )} />,
            },
            {
              key: 'bindings',
              label: 'Skill Bindings',
              children: <Table rowKey="id" size="small" columns={bindingColumns} dataSource={bindings} loading={bindingsQuery.isLoading} scroll={{ x: 920 }} title={() => <Button type="primary" size="small" icon={<PlusOutlined />} disabled={!canManage} onClick={() => setDrawer({ kind: 'skill-binding' })}>新增 binding</Button>} />,
            },
            {
              key: 'governance',
              label: 'Governance',
              children: (
                <Space orientation="vertical" size={12} style={{ width: '100%' }}>
                  <Space wrap>
                    <Select style={{ width: 140 }} options={governanceWindowOptions} value={governanceWindowHours} onChange={(value) => setGovernanceWindowHours(String(value))} />
                    <Button icon={<ReloadOutlined />} disabled={!canManage} loading={governanceQuery.isFetching} onClick={() => void governanceQuery.refetch()}>刷新</Button>
                  </Space>
                  {governanceStatus ? (
                    <>
                      <Descriptions size="small" column={4} bordered items={[
                        { key: 'status', label: 'Health', children: <StatusTag value={governanceStatus.health.status} /> },
                        { key: 'message', label: 'Message', span: 2, children: governanceStatus.health.message || '-' },
                        { key: 'window', label: 'Window', children: `${governanceStatus.windowHours}h` },
                        { key: 'generatedAt', label: 'Generated', children: formatDateTime(governanceStatus.generatedAt) },
                        { key: 'calls', label: 'Calls', children: governanceStatus.metrics.totalCalls },
                        { key: 'success', label: 'Success', children: governanceStatus.metrics.successCount },
                        { key: 'deny', label: 'Denied', children: governanceStatus.metrics.denyCount },
                        { key: 'failure', label: 'Failures', children: governanceStatus.metrics.failureCount },
                        { key: 'pending', label: 'Pending approvals', children: governanceStatus.approvals.pending },
                        { key: 'approvalSla', label: 'Approval SLA', children: `${governanceStatus.approvals.overdue} overdue / ${governanceStatus.approvals.dueSoon} due soon / ${governanceStatus.approvals.stalePending} stale` },
                        { key: 'tokens', label: 'Active tokens', children: `${governanceStatus.tokens.personalAccessTokens.active + governanceStatus.tokens.serviceAccountTokens.active} / ${governanceStatus.tokens.personalAccessTokens.total + governanceStatus.tokens.serviceAccountTokens.total}` },
                        { key: 'clients', label: 'AI clients', children: `${governanceStatus.clients.active} active / ${governanceStatus.clients.total} total` },
                      ]} />
                      <Descriptions size="small" column={4} bordered items={[
                        { key: 'expiring', label: 'Token expiration', children: `${governanceStatus.tokens.expiredActive?.length ?? 0} expired / ${governanceStatus.tokens.expiringSoon?.length ?? 0} soon` },
                        { key: 'stale', label: 'Token usage', children: `${governanceStatus.tokens.stale?.length ?? 0} stale / ${governanceStatus.tokens.neverUsed?.length ?? 0} never used` },
                        { key: 'lastUsed', label: 'last_used tracking', children: <StatusTag value={governanceStatus.tokens.lastUsedTrackingState} /> },
                        { key: 'clientApproval', label: 'Client registration', children: <StatusTag value={governanceStatus.clients.registrationApproval} /> },
                        { key: 'riskCounts', label: 'Risk counts', span: 2, children: compactList(governanceRiskCountTags(governanceStatus.metrics.riskCounts), 4) },
                        { key: 'oldestPending', label: 'Oldest pending', children: governanceStatus.approvals.oldestPendingRequestId ? `${governanceStatus.approvals.oldestPendingRequestId} / ${governanceStatus.approvals.oldestPendingHours ?? 0}h` : '-' },
                        { key: 'nextDue', label: 'Next due', children: governanceStatus.approvals.nextDueRequestId ? `${governanceStatus.approvals.nextDueRequestId} / ${formatDateTime(governanceStatus.approvals.nextDueAt)}` : '-' },
                        { key: 'redactionHits', label: 'Redaction hits', children: `${governanceStatus.redaction?.totalMatches ?? 0} / ${governanceStatus.redaction?.auditsWithRedaction ?? 0} audits` },
                        { key: 'redactionTargets', label: 'Redaction targets', children: `${governanceStatus.redaction?.inputAudits ?? 0} input / ${governanceStatus.redaction?.outputAudits ?? 0} output` },
                      ]} />
                      {governanceStatus.recommendationActions?.length ? (
                        <Table
                          rowKey={(record) => `${record.type}:${record.action}:${record.targetKind || ''}:${record.targetId || ''}`}
                          size="small"
                          title={() => <Text strong>Recommendation actions</Text>}
                          columns={governanceRecommendationColumns}
                          dataSource={governanceStatus.recommendationActions}
                          pagination={{ pageSize: 6 }}
                          scroll={{ x: 1120 }}
                        />
                      ) : governanceStatus.recommendations?.length ? (
                        <Space orientation="vertical" size={8} style={{ width: '100%' }}>
                          {governanceStatus.recommendations.map((item) => (
                            <Alert key={item} type="warning" showIcon message={item} />
                          ))}
                        </Space>
                      ) : (
                        <Alert type="success" showIcon message="AI Gateway governance controls are healthy" />
                      )}
                      <Table rowKey="name" size="small" columns={governanceHealthColumns} dataSource={governanceStatus.health.checks ?? []} loading={governanceQuery.isLoading} pagination={false} scroll={{ x: 860 }} />
                      <Table rowKey="key" size="small" columns={governanceCoverageColumns} dataSource={governanceCoverageRows(governanceStatus.policyCoverage)} pagination={false} scroll={{ x: 760 }} />
                      <Table rowKey="key" size="small" title={() => <Text strong>Redaction hits</Text>} columns={governanceRedactionColumns} dataSource={governanceRedactionRows(governanceStatus.redaction)} pagination={false} scroll={{ x: 820 }} />
                      <Table rowKey="key" size="small" title={() => <Text strong>Token findings</Text>} columns={governanceTokenFindingColumns} dataSource={governanceTokenFindingRows(governanceStatus.tokens)} pagination={{ pageSize: 6 }} scroll={{ x: 1160 }} />
                      <Table rowKey="key" size="small" columns={governanceQueueColumns} dataSource={governanceApprovalQueueRows(governanceStatus.approvals, governanceStatus.clients)} pagination={false} scroll={{ x: 720 }} />
                      <div className="grid gap-3 lg:grid-cols-3">
                        <Table rowKey="key" size="small" title={() => <Text strong>Top tools</Text>} columns={governanceMetricColumns} dataSource={governanceStatus.metrics.topTools ?? []} pagination={false} />
                        <Table rowKey="key" size="small" title={() => <Text strong>Top AI clients</Text>} columns={governanceMetricColumns} dataSource={governanceStatus.metrics.topAiClients ?? []} pagination={false} />
                        <Table rowKey="key" size="small" title={() => <Text strong>Top actors</Text>} columns={governanceMetricColumns} dataSource={governanceStatus.metrics.topActors ?? []} pagination={false} />
                      </div>
                      <Table rowKey={(record) => `${record.type}:${record.policyId || record.grantId || record.approvalRequestId || record.actorId || record.aiClientId || record.toolName || record.summary}`} size="small" columns={governanceFindingColumns} dataSource={governanceStatus.anomalies ?? []} pagination={{ pageSize: 8 }} scroll={{ x: 1180 }} />
                    </>
                  ) : (
                    <Table rowKey="name" size="small" columns={governanceHealthColumns} dataSource={[]} loading={governanceQuery.isLoading} pagination={false} locale={{ emptyText: <Empty /> }} />
                  )}
                </Space>
              ),
            },
            {
              key: 'approvals',
              label: 'Approvals',
              children: (
                <Space orientation="vertical" size={12} style={{ width: '100%' }}>
                  <Space wrap>
                    <Input style={{ width: 220 }} placeholder="approvalRequestId" value={approvalFilters.id} onChange={(event) => setApprovalFilters((prev) => ({ ...prev, id: event.target.value, status: event.target.value ? '' : prev.status }))} />
                    <Select allowClear style={{ width: 150 }} placeholder="状态" options={approvalStatusOptions} value={approvalFilters.status || undefined} onChange={(value) => setApprovalFilters((prev) => ({ ...prev, status: value ?? '' }))} />
                    <Input style={{ width: 180 }} placeholder="actorId" value={approvalFilters.actor} onChange={(event) => setApprovalFilters((prev) => ({ ...prev, actor: event.target.value }))} />
                    <Select allowClear style={{ width: 220 }} placeholder="AI client" options={clientOptions} value={approvalFilters.aiClientId || undefined} onChange={(value) => setApprovalFilters((prev) => ({ ...prev, aiClientId: value ?? '' }))} />
                    <Select allowClear style={{ width: 260 }} placeholder="Tool" options={toolOptions} value={approvalFilters.toolName || undefined} onChange={(value) => setApprovalFilters((prev) => ({ ...prev, toolName: value ?? '' }))} />
                    <Select allowClear style={{ width: 140 }} placeholder="Risk" options={riskLevelOptions} value={approvalFilters.riskLevel || undefined} onChange={(value) => setApprovalFilters((prev) => ({ ...prev, riskLevel: value ?? '' }))} />
                    <Select allowClear style={{ width: 190 }} placeholder="Strategy" options={approvalRequestStrategyOptions} value={approvalFilters.strategy || undefined} onChange={(value) => setApprovalFilters((prev) => ({ ...prev, strategy: value ?? '' }))} />
                    <RangePicker showTime allowClear style={{ width: 340 }} placeholder={['开始时间', '结束时间']} onChange={(value) => setApprovalFilters((prev) => ({ ...prev, ...gatewayTimeRangeQuery(value) }))} />
                    <Button icon={<ReloadOutlined />} onClick={() => void approvalsQuery.refetch()}>刷新</Button>
                  </Space>
                  <Table
                    rowKey="id"
                    size="small"
                    columns={approvalColumns}
                    dataSource={approvalRequests}
                    loading={approvalsQuery.isLoading}
                    scroll={{ x: 1560 }}
                    expandable={{ expandedRowRender: (record) => <ApprovalTracePanel record={record} /> }}
                  />
                </Space>
              ),
            },
            {
              key: 'audit',
              label: '调用日志',
              children: (
                <Space orientation="vertical" size={12} style={{ width: '100%' }}>
                  <Space wrap>
                    <Input style={{ width: 190 }} placeholder="调用者 ID" value={auditFilters.actor} onChange={(event) => setAuditFilters((prev) => ({ ...prev, actor: event.target.value }))} />
                    <Select allowClear style={{ width: 220 }} placeholder="调用入口 client" options={clientOptions} value={auditFilters.aiClientId || undefined} onChange={(value) => setAuditFilters((prev) => ({ ...prev, aiClientId: value ?? '' }))} />
                    <Select allowClear style={{ width: 260 }} placeholder="调用内容 / Tool" options={toolOptions} value={auditFilters.toolName || undefined} onChange={(value) => setAuditFilters((prev) => ({ ...prev, toolName: value ?? '' }))} />
                    <Select allowClear style={{ width: 180 }} placeholder="动作" options={auditActionOptions} value={auditFilters.action || undefined} onChange={(value) => setAuditFilters((prev) => ({ ...prev, action: value ?? '' }))} />
                    <Select allowClear style={{ width: 140 }} placeholder="Risk" options={riskLevelOptions} value={auditFilters.riskLevel || undefined} onChange={(value) => setAuditFilters((prev) => ({ ...prev, riskLevel: value ?? '' }))} />
                    <Select allowClear style={{ width: 190 }} placeholder="Result" options={auditResultOptions} value={auditFilters.result || undefined} onChange={(value) => setAuditFilters((prev) => ({ ...prev, result: value ?? '' }))} />
                    <RangePicker showTime allowClear style={{ width: 340 }} placeholder={['开始时间', '结束时间']} onChange={(value) => setAuditFilters((prev) => ({ ...prev, ...gatewayTimeRangeQuery(value) }))} />
                    <Button icon={<ReloadOutlined />} onClick={() => void auditQuery.refetch()}>刷新</Button>
                  </Space>
                  <Table rowKey="id" size="small" columns={auditColumns} dataSource={auditLogs} loading={auditQuery.isLoading} scroll={{ x: 1220 }} expandable={{ expandedRowRender: (record) => <JsonBlock value={{ requestId: record.requestId, sourceIp: record.sourceIp, resourceScope: record.resourceScope, metadata: record.metadata }} /> }} />
                </Space>
              ),
            },
          ].filter((item) => gatewayTabBelongsToSection(item.key as GatewayTabKey, section))}
            />
          )}
        </Space>
      </Card>

      <Drawer
        title={drawerTitle(drawer)}
        size={560}
        open={!!drawer}
        onClose={() => setDrawer(null)}
        forceRender
        footer={(
          <Space style={{ justifyContent: 'flex-end', width: '100%' }}>
            <Button onClick={() => setDrawer(null)}>取消</Button>
            <Button type="primary" loading={upsertMutation.isPending} onClick={() => form.submit()}>保存</Button>
          </Space>
        )}
      >
        <Form form={form} layout="vertical" onFinish={(values) => upsertMutation.mutate(values)} initialValues={drawer ? defaultFormValues(drawer.kind) : undefined}>
          {drawer ? renderDrawerFields(drawer, clients, manifest) : null}
        </Form>
      </Drawer>

      <Modal
        title={oneTimeToken?.title}
        open={!!oneTimeToken}
        onCancel={() => setOneTimeToken(null)}
        footer={<Button type="primary" onClick={() => setOneTimeToken(null)}>关闭</Button>}
      >
        <Alert type="warning" showIcon message="token 只展示一次；关闭后无法再次查看明文。" style={{ marginBottom: 12 }} />
        <Input.TextArea value={oneTimeToken?.value} autoSize={{ minRows: 3 }} readOnly />
        {oneTimeToken?.prefix ? <Text type="secondary">prefix: {oneTimeToken.prefix}</Text> : null}
      </Modal>

      <Modal
        title={decisionModalTitle(decisionTarget)}
        open={!!decisionTarget}
        onCancel={() => setDecisionTarget(null)}
        okText="确认"
        cancelText="取消"
        confirmLoading={decisionMutation.isPending}
        okButtonProps={{ danger: decisionTarget?.action !== 'approve' }}
        onOk={() => {
          void decisionForm.validateFields().then((values) => {
            if (!decisionTarget) return
            decisionMutation.mutate({
              action: decisionTarget.action,
              id: decisionTarget.record.id,
              comment: values.comment,
            })
          })
        }}
      >
        {decisionTarget ? (
          <Space orientation="vertical" size={12} style={{ width: '100%' }}>
            <Descriptions size="small" column={1} bordered items={[
              { key: 'tool', label: 'Tool', children: decisionTarget.record.toolName },
              { key: 'actor', label: 'Actor', children: `${decisionTarget.record.actorType}:${decisionTarget.record.actorId}` },
              { key: 'scope', label: 'Scope', children: scopeSummary(decisionTarget.record.resourceScope) },
            ]} />
            <Form form={decisionForm} layout="vertical">
              <Form.Item name="comment" label="备注">
                <Input.TextArea autoSize={{ minRows: 3 }} />
              </Form.Item>
            </Form>
          </Space>
        ) : null}
      </Modal>
    </div>
  )
}

function defaultFormValues(kind: DrawerKind) {
  switch (kind) {
    case 'ai-client':
      return { kind: 'mcp_client', status: 'active' }
    case 'service-account':
      return { status: 'active' }
    case 'tool-grant':
      return { subjectType: 'role', effect: 'allow', riskLevel: 'read', requiresApproval: 'false' }
    case 'access-policy':
      return {
        subjectType: 'role',
        effect: 'allow',
        enabled: 'true',
        approvalMode: 'none',
        approvalRoutingMode: 'all',
        rateLimitEnabled: false,
        rateLimitMode: 'counter',
        rateLimitScope: 'actor_client_tool',
        budgetEnabled: false,
        budgetScope: 'actor_client',
        redactionEnabled: false,
        redactionMode: 'none',
        redactionTarget: 'input',
        redactionReplacement: '[REDACTED]',
        redactionPreserveFormat: false,
        outputRedactionReplacement: '[REDACTED]',
        outputRedactionPreserveFormat: false,
      }
    case 'skill-binding':
      return { subjectType: 'role', enabled: 'true' }
    default:
      return {}
  }
}

function drawerTitle(drawer: DrawerState | null) {
  if (!drawer) return ''
  switch (drawer.kind) {
    case 'ai-client':
      return drawer.record ? '编辑 AI client' : '新增 AI client'
    case 'personal-token':
      return '创建 personal access token'
    case 'service-account':
      return '新增服务账号'
    case 'service-token':
      return '创建服务账号 token'
    case 'service-token-revoke':
      return '吊销服务账号 token'
    case 'tool-grant':
      return '新增 MCP tool grant'
    case 'access-policy':
      return drawer.record ? '编辑 access policy' : '新增 access policy'
    case 'skill-binding':
      return drawer.record ? '编辑 skill binding' : '新增 skill binding'
    default:
      return ''
  }
}

function decisionModalTitle(target: { action: 'approve' | 'reject' | 'cancel'; record: ApprovalRequest } | null) {
  if (!target) return ''
  if (target.action === 'approve') return '批准并执行审批请求'
  if (target.action === 'reject') return '拒绝审批请求'
  return '取消审批请求'
}

function renderDrawerFields(drawer: DrawerState, clients: AIClient[], manifest?: GatewayManifest) {
  const clientOptions = clients.map((item) => ({ label: `${item.name} (${item.id})`, value: item.id }))
  const toolOptions = manifest?.tools.map((item) => ({ label: item.name, value: item.name })) ?? []
  const skillOptions = manifest?.skills?.map((item) => ({ label: `${item.name} (${item.id})`, value: item.id })) ?? []
  const capabilityOptions = manifest?.tools.map((item) => ({ label: item.name, value: item.name })) ?? []
  switch (drawer.kind) {
    case 'ai-client':
      return (
        <>
          <Form.Item name="id" label="Client ID" rules={[{ required: true }]}>
            <Input disabled={!!drawer.record} />
          </Form.Item>
          <Form.Item name="name" label="名称" rules={[{ required: true }]}>
            <Input />
          </Form.Item>
          <Form.Item name="kind" label="类型" rules={[{ required: true }]}>
            <Select options={clientKindOptions} />
          </Form.Item>
          <Form.Item name="status" label="状态" rules={[{ required: true }]}>
            <Select options={statusOptions} />
          </Form.Item>
          <Form.Item name="redirectUris" label="Redirect URIs">
            <Select mode="tags" tokenSeparators={[',', ' ']} />
          </Form.Item>
          <Form.Item name="allowedOrigins" label="Allowed origins">
            <Select mode="tags" tokenSeparators={[',', ' ']} />
          </Form.Item>
        </>
      )
    case 'personal-token':
      return (
        <>
          <Form.Item name="name" label="名称" rules={[{ required: true }]}>
            <Input />
          </Form.Item>
          <Form.Item name="permissionKeys" label="权限 keys">
            <Select mode="tags" tokenSeparators={[',', ' ']} />
          </Form.Item>
          <Form.Item name="scopes" label="Scopes">
            <Select mode="tags" tokenSeparators={[',', ' ']} />
          </Form.Item>
          <Form.Item name="expiresAt" label="过期时间">
            <Input placeholder="RFC3339，例如 2026-06-30T00:00:00Z" />
          </Form.Item>
        </>
      )
    case 'service-account':
      return (
        <>
          <Form.Item name="id" label="服务账号 ID">
            <Input placeholder="留空由后端生成" />
          </Form.Item>
          <Form.Item name="name" label="名称" rules={[{ required: true }]}>
            <Input />
          </Form.Item>
          <Form.Item name="description" label="描述">
            <Input.TextArea autoSize={{ minRows: 2 }} />
          </Form.Item>
          <Form.Item name="status" label="状态">
            <Select options={statusOptions} />
          </Form.Item>
          <Form.Item name="roleIds" label="角色">
            <Select mode="tags" tokenSeparators={[',', ' ']} />
          </Form.Item>
          <Form.Item name="teamIds" label="用户组">
            <Select mode="tags" tokenSeparators={[',', ' ']} />
          </Form.Item>
          <Form.Item name="scopeGrantIds" label="Scope grants">
            <Select mode="tags" tokenSeparators={[',', ' ']} />
          </Form.Item>
        </>
      )
    case 'service-token':
      return (
        <>
          <Alert type="info" showIcon message={`服务账号：${(drawer.record as ServiceAccount).name}`} style={{ marginBottom: 12 }} />
          <Form.Item name="name" label="名称" rules={[{ required: true }]}>
            <Input />
          </Form.Item>
          <Form.Item name="permissionKeys" label="权限 keys">
            <Select mode="tags" tokenSeparators={[',', ' ']} />
          </Form.Item>
          <Form.Item name="scopes" label="Scopes">
            <Select mode="tags" tokenSeparators={[',', ' ']} />
          </Form.Item>
          <Form.Item name="expiresAt" label="过期时间">
            <Input placeholder="RFC3339，例如 2026-06-30T00:00:00Z" />
          </Form.Item>
        </>
      )
    case 'service-token-revoke':
      return (
        <Form.Item name="tokenId" label="Token ID" rules={[{ required: true }]}>
          <Input />
        </Form.Item>
      )
    case 'tool-grant':
      return (
        <>
          <Form.Item name="subjectType" label="Subject 类型" rules={[{ required: true }]}>
            <Select options={subjectTypeOptions} />
          </Form.Item>
          <Form.Item name="subjectId" label="Subject ID" rules={[{ required: true }]}>
            <Input />
          </Form.Item>
          <Form.Item name="aiClientId" label="AI client">
            <Select allowClear options={clientOptions} />
          </Form.Item>
          <Form.Item name="toolName" label="Tool" rules={[{ required: true }]}>
            <Select showSearch options={toolOptions} />
          </Form.Item>
          <Form.Item name="effect" label="Effect" rules={[{ required: true }]}>
            <Select options={effectOptions} />
          </Form.Item>
          <Form.Item name="riskLevel" label="Risk">
            <Select options={riskLevelOptions} />
          </Form.Item>
          <Form.Item name="permissionKeys" label="额外权限 keys">
            <Select mode="tags" tokenSeparators={[',', ' ']} />
          </Form.Item>
          <Form.Item name="requiresApproval" label="需要审批">
            <Select options={[{ label: '否', value: 'false' }, { label: '是', value: 'true' }]} />
          </Form.Item>
          <ScopeFields />
        </>
      )
    case 'access-policy':
      return (
        <>
          <Form.Item name="name" label="名称" rules={[{ required: true }]}>
            <Input />
          </Form.Item>
          <Form.Item name="description" label="描述">
            <Input.TextArea autoSize={{ minRows: 2 }} />
          </Form.Item>
          <Form.Item name="enabled" label="启用">
            <Select options={[{ label: '启用', value: 'true' }, { label: '禁用', value: 'false' }]} />
          </Form.Item>
          <Form.Item name="subjectType" label="Subject 类型" rules={[{ required: true }]}>
            <Select options={subjectTypeOptions} />
          </Form.Item>
          <Form.Item name="subjectId" label="Subject ID" rules={[{ required: true }]}>
            <Input />
          </Form.Item>
          <Form.Item name="aiClientId" label="AI client">
            <Select allowClear options={clientOptions} />
          </Form.Item>
          <Form.Item name="effect" label="Effect" rules={[{ required: true }]}>
            <Select options={effectOptions} />
          </Form.Item>
          <Form.Item name="toolPatterns" label="Tool patterns">
            <Select mode="tags" tokenSeparators={[',', ' ']} options={toolOptions} />
          </Form.Item>
          <Form.Item name="skillIds" label="Skills">
            <Select mode="multiple" options={skillOptions} />
          </Form.Item>
          <Form.Item name="riskLevels" label="Risk levels">
            <Select mode="multiple" options={riskLevelOptions} />
          </Form.Item>
          <Form.Item name="approvalMode" label="审批策略">
            <Select options={approvalStrategyOptions} />
          </Form.Item>
          <Form.Item noStyle shouldUpdate={(prev, next) => prev.approvalMode !== next.approvalMode}>
            {({ getFieldValue }) => approvalRoutingEnabled(getFieldValue('approvalMode')) ? (
              <>
                <Divider plain>审批路由</Divider>
                <Form.Item name="approvalPolicyRef" label="Delivery approval policy">
                  <Input placeholder="例如 delivery-standard" />
                </Form.Item>
                <Form.Item name="approvalRoutingMode" label="审批模式">
                  <Select options={approvalRoutingModeOptions} />
                </Form.Item>
                <Form.Item name="approvalApproverUsers" label="候选用户">
                  <Select mode="tags" tokenSeparators={[',', ' ']} />
                </Form.Item>
                <Form.Item name="approvalApproverRoles" label="候选角色">
                  <Select mode="tags" tokenSeparators={[',', ' ']} />
                </Form.Item>
                <Form.Item name="approvalApproverTeams" label="候选用户组">
                  <Select mode="tags" tokenSeparators={[',', ' ']} />
                </Form.Item>
                <Form.Item name="approvalOnCallRef" label="On-call ref">
                  <Input placeholder="例如 sre-primary" />
                </Form.Item>
                <Form.Item name="approvalRequiredApprovals" label="最少审批人数">
                  <InputNumber min={1} precision={0} style={{ width: '100%' }} />
                </Form.Item>
                <Space size={12} style={{ width: '100%' }} align="start">
                  <Form.Item name="approvalChangeWindowStartsAt" label="窗口开始" style={{ flex: 1 }}>
                    <Input placeholder="2026-06-01T09:00:00Z" />
                  </Form.Item>
                  <Form.Item name="approvalChangeWindowEndsAt" label="窗口结束" style={{ flex: 1 }}>
                    <Input placeholder="2026-06-01T18:00:00Z" />
                  </Form.Item>
                </Space>
                <Form.Item name="approvalChangeWindowTimezone" label="窗口时区">
                  <Input placeholder="例如 Asia/Shanghai" />
                </Form.Item>
              </>
            ) : null}
          </Form.Item>
          <ScopeFields />
          <PolicyConditionFields />
        </>
      )
    case 'skill-binding':
      return (
        <>
          <Form.Item name="subjectType" label="Subject 类型" rules={[{ required: true }]}>
            <Select options={subjectTypeOptions} />
          </Form.Item>
          <Form.Item name="subjectId" label="Subject ID" rules={[{ required: true }]}>
            <Input />
          </Form.Item>
          <Form.Item name="aiClientId" label="AI client">
            <Select allowClear options={clientOptions} />
          </Form.Item>
          <Form.Item name="skillId" label="Skill" rules={[{ required: true }]}>
            <Select showSearch options={skillOptions} />
          </Form.Item>
          <Form.Item name="capabilityRefs" label="Capability refs">
            <Select mode="multiple" options={capabilityOptions} />
          </Form.Item>
          <Form.Item name="enabled" label="启用">
            <Select options={[{ label: '启用', value: 'true' }, { label: '禁用', value: 'false' }]} />
          </Form.Item>
        </>
      )
    default:
      return null
  }
}
