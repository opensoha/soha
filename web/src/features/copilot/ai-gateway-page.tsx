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
  Form,
  Input,
  InputNumber,
  Modal,
  Popconfirm,
  Select,
  Space,
  Tabs,
  Tag,
  Typography,
} from 'antd'
import { useLocation, useNavigate, useSearchParams } from 'react-router-dom'
import { AdminTable } from '@/components/admin-table'
import { ManagementDetailHeader, ManagementIconButton, ManagementState, ManagementTableToolbar } from '@/components/management-list'
import { StatusTag } from '@/components/status-tag'
import { hasPermission, usePermissionSnapshot } from '@/features/auth/permission-snapshot'
import { api } from '@/services/api-client'
import { useAuthStore } from '@/stores/auth-store'
import type { ApiResponse } from '@/types'
import {
  accessPolicyApprovalPolicyFromValues,
  accessPolicyConditionsFromValues,
  accessPolicyFormValuesFromRecord,
  approvalRoutingEnabled,
  approvalRequestStrategyOptions,
  approvalRoutingModeOptions,
  approvalStatusOptions,
  approvalStrategyOptions,
  approvalTrace,
  asRecord,
  auditActionOptions,
  auditResultOptions,
  clientKindOptions,
  defaultGatewayTabForSection,
  effectOptions,
  firstNumber,
  firstValue,
  firstString,
  gatewayLimitScopeOptions,
  gatewayMenuMeta,
  gatewaySecretTypeOptions,
  gatewaySectionFromPath,
  gatewaySectionMeta,
  gatewaySectionPaths,
  gatewayTabBelongsToSection,
  gatewayTabSectionMap,
  gatewayTimeRangeQuery,
  governanceApprovalQueueRows,
  governanceCoverageDrilldown,
  governanceCoverageRows,
  governanceFindingDrilldownActions,
  governancePolicyDraftForCoverage,
  governanceQueueDrilldown,
  governanceRecommendationDrilldownAction,
  governanceRedactionRows,
  governanceRiskCountTags,
  governanceTokenFindingDrilldown,
  governanceTokenFindingRows,
  governanceWindowOptions,
  normalizeGatewayTabKey,
  normalizeRateLimitMode,
  queryString,
  rateLimitModeOptions,
  redactionModeOptions,
  redactionTargetOptions,
  riskLevelOptions,
  scopeFieldDefs,
  scopeValuesFromRecord,
  statusOptions,
  stringifyPayload,
  subjectTypeOptions,
  valuesToResourceScopes,
  workflowTracePath,
} from './ai-gateway-model'
import type {
  AIClient,
  AccessPolicy,
  ApprovalDecisionResult,
  ApprovalFilterState,
  ApprovalRequest,
  AuditFilterState,
  CreatedPersonalAccessToken,
  CreatedServiceAccountToken,
  DrawerKind,
  DrawerState,
  GatewayAuditLog,
  GatewayManifest,
  GatewayTabKey,
  GatewayTool,
  GovernanceDrilldownTarget,
  GovernanceFinding,
  GovernanceHealthCheck,
  GovernanceMetricCount,
  GovernanceRecommendationAction,
  GovernanceRedactionRow,
  GovernanceStatus,
  GovernanceTokenFindingRow,
  PersonalAccessToken,
  ServiceAccount,
  ServiceAccountToken,
  SkillBinding,
  ToolGrant,
} from './ai-gateway-model'
export {
  accessPolicyApprovalPolicyFromValues,
  accessPolicyConditionsFromValues,
  accessPolicyFormValuesFromRecord,
  gatewaySecretTypeOptions,
  gatewayTimeRangeQuery,
  governanceApprovalQueueRows,
  governanceCoverageDrilldown,
  governanceCoverageRows,
  governanceFindingDrilldownActions,
  governancePolicyDraftForCoverage,
  governanceQueueDrilldown,
  governanceRecommendationDrilldownAction,
  governanceRedactionRows,
  governanceRiskCountTags,
  governanceTokenFindingDrilldown,
  governanceTokenFindingRows,
  rateLimitModeOptions,
}

const { Paragraph, Text } = Typography
const { RangePicker } = DatePicker



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

function tokenStatus(record: { expiresAt?: string; revokedAt?: string }) {
  if (record.revokedAt) return 'revoked'
  if (record.expiresAt) {
    const expiresAt = new Date(record.expiresAt).getTime()
    if (Number.isFinite(expiresAt) && expiresAt <= Date.now()) return 'expired'
  }
  return 'active'
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
  const { message } = App.useApp()
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
  const currentUser = useAuthStore((state) => state.user)
  const permissionSnapshot = usePermissionSnapshot()
  const snapshot = permissionSnapshot.data?.data
  const canView = hasPermission(snapshot, 'ai.gateway.view')
  const canManage = hasPermission(snapshot, 'ai.gateway.manage')
  const canInvoke = hasPermission(snapshot, 'ai.gateway.invoke')
  const canUseGateway = canView || canInvoke || canManage
  const personalTokenScope = canManage ? 'all' : 'mine'

  const clientsQuery = useQuery({
    queryKey: ['ai-gateway', 'ai-clients'],
    queryFn: () => api.get<ApiResponse<AIClient[]>>('/ai-gateway/ai-clients'),
    enabled: canManage,
  })
  const personalTokensQuery = useQuery({
    queryKey: ['ai-gateway', 'personal-access-tokens', personalTokenScope],
    queryFn: () => api.get<ApiResponse<PersonalAccessToken[]>>(canManage ? '/ai-gateway/personal-access-tokens?scope=all' : '/ai-gateway/personal-access-tokens'),
    enabled: canManage || canView,
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

  const rotateTokenMutation = useMutation<ApiResponse<CreatedPersonalAccessToken | CreatedServiceAccountToken>, Error, { kind: 'personal-token' | 'service-token'; id: string }>({
    mutationFn: ({ kind, id }: { kind: 'personal-token' | 'service-token'; id: string }) => {
      if (kind === 'personal-token') {
        return api.post<ApiResponse<CreatedPersonalAccessToken>>(`/ai-gateway/personal-access-tokens/${id}/rotate`)
      }
      return api.post<ApiResponse<CreatedServiceAccountToken>>(`/ai-gateway/service-account-tokens/${id}/rotate`)
    },
    onSuccess: (res: any, variables) => {
      const value = res?.data?.value
      const token = res?.data?.token
      if (value) {
        setOneTimeToken({
          title: variables.kind === 'service-token' ? '服务账号 token 已轮换' : 'Personal access token 已轮换',
          value,
          prefix: token?.tokenPrefix,
        })
      }
      void refreshAll()
      message.success('已轮换')
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
        <Space className="soha-row-action-icons">
          <ManagementIconButton size="small" tooltip="编辑" aria-label="编辑 client" icon={<EditOutlined />} disabled={!canManage} onClick={() => setDrawer({ kind: 'ai-client', record })} />
          <Popconfirm title="确认禁用 client？" description="该操作会立即更新 AI Gateway 控制面。" okButtonProps={{ danger: true, loading: disableClientMutation.isPending }} onConfirm={() => disableClientMutation.mutate(record)}>
            <ManagementIconButton size="small" tooltip="禁用" aria-label="禁用 client" danger icon={<StopOutlined />} loading={disableClientMutation.isPending} disabled={!canManage || record.status === 'disabled'} />
          </Popconfirm>
        </Space>
      ),
    },
  ]

  const tokenColumns: TableColumnsType<PersonalAccessToken> = [
    { title: '名称', dataIndex: 'name', width: 220, render: (_, record) => <Space orientation="vertical" size={0}><Text strong>{record.name}</Text><Text type="secondary">{record.tokenPrefix}</Text></Space> },
    { title: 'Owner', dataIndex: 'userId', width: 180, render: (value) => <Tag>{value}</Tag> },
    { title: '权限', dataIndex: 'permissionKeys', render: (value) => compactList(value, 3) },
    { title: '过期', dataIndex: 'expiresAt', width: 140, render: formatDateTime },
    { title: '最近使用', dataIndex: 'lastUsedAt', width: 140, render: formatDateTime },
    { title: '状态', key: 'status', width: 110, render: (_, record) => <StatusTag value={tokenStatus(record)} /> },
    {
      title: '',
      key: 'actions',
      fixed: 'right',
      width: 120,
      render: (_, record) => {
        const isOwner = record.userId === currentUser?.userId
        const canRotate = canInvoke && isOwner
        const canRevoke = canManage || (canInvoke && isOwner)
        return (
          <Space className="soha-row-action-icons">
            <Popconfirm title="轮换 personal login key？" description="旧 key 会被吊销，新明文只展示一次。只有 owner 本人可以轮换。" okButtonProps={{ loading: rotateTokenMutation.isPending }} onConfirm={() => rotateTokenMutation.mutate({ kind: 'personal-token', id: record.id })}>
              <ManagementIconButton size="small" tooltip="轮换" aria-label="轮换 personal login key" icon={<ReloadOutlined />} loading={rotateTokenMutation.isPending} disabled={!canRotate || !!record.revokedAt} />
            </Popconfirm>
            <Popconfirm title="吊销 personal login key？" description="吊销后该 key 将不能再登录或调用 AI Gateway，审计记录会保留。" okButtonProps={{ danger: true, loading: deleteMutation.isPending }} onConfirm={() => deleteMutation.mutate({ kind: 'personal-token', id: record.id })}>
              <ManagementIconButton size="small" tooltip="吊销" aria-label="吊销 personal login key" danger icon={<StopOutlined />} disabled={!canRevoke || !!record.revokedAt} />
            </Popconfirm>
          </Space>
        )
      },
    },
  ]

  const serviceAccountColumns: TableColumnsType<ServiceAccount> = [
    { title: '服务账号', dataIndex: 'name', width: 240, render: (_, record) => <Space orientation="vertical" size={0}><Text strong>{record.name}</Text><Text type="secondary">{record.id}</Text></Space> },
    { title: '状态', dataIndex: 'status', width: 110, render: (value) => <StatusTag value={value} /> },
    { title: '角色', dataIndex: 'roleIds', render: (value) => compactList(value, 3) },
    { title: '组织', dataIndex: 'teamIds', render: (value) => compactList(value, 2) },
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
    { title: '状态', key: 'status', width: 110, render: (_, record) => <StatusTag value={tokenStatus(record)} /> },
    {
      title: '',
      key: 'actions',
      fixed: 'right',
      width: 120,
      render: (_, record) => (
        <Space className="soha-row-action-icons">
          <Popconfirm title="轮换服务账号 token？" description="系统会生成新明文并吊销旧 token，新明文只展示一次。" okButtonProps={{ loading: rotateTokenMutation.isPending }} onConfirm={() => rotateTokenMutation.mutate({ kind: 'service-token', id: record.id })}>
            <ManagementIconButton size="small" tooltip="轮换" aria-label="轮换服务 token" icon={<ReloadOutlined />} loading={rotateTokenMutation.isPending} disabled={!canManage || !!record.revokedAt} />
          </Popconfirm>
          <ManagementIconButton
            size="small"
            tooltip="吊销"
            aria-label="吊销服务 token"
            danger
            icon={<StopOutlined />}
            disabled={!canManage || !!record.revokedAt}
            onClick={() => setDrawer({ kind: 'service-token-revoke', initialValues: { tokenId: record.id } })}
          />
        </Space>
      ),
    },
  ]

  const grantColumns: TableColumnsType<ToolGrant> = [
    { title: 'Subject', dataIndex: 'subjectId', width: 220, render: (_, record) => <Space orientation="vertical" size={0}><Text strong>{record.subjectId}</Text><Text type="secondary">{record.subjectType}{record.aiClientId ? ` / ${record.aiClientId}` : ''}</Text></Space> },
    { title: 'Tool', dataIndex: 'toolName', width: 240, render: (value) => <Tag>{value}</Tag> },
    { title: 'Effect', dataIndex: 'effect', width: 100, render: (value) => <StatusTag value={value} /> },
    { title: 'Risk', dataIndex: 'riskLevel', width: 100, render: (value) => <StatusTag value={value} /> },
    { title: 'Scope', dataIndex: 'resourceScopes', render: scopeSummary },
    {
      title: '',
      key: 'actions',
      fixed: 'right',
      width: 80,
      render: (_, record) => (
        <Popconfirm title="删除 MCP tool grant？" description="该操作会立即更新 AI Gateway 控制面。" okButtonProps={{ danger: true, loading: deleteMutation.isPending }} onConfirm={() => deleteMutation.mutate({ kind: 'grant', id: record.id })}>
          <ManagementIconButton size="small" tooltip="删除" aria-label="删除 MCP tool grant" danger icon={<DeleteOutlined />} disabled={!canManage} />
        </Popconfirm>
      ),
    },
  ]

  const policyColumns: TableColumnsType<AccessPolicy> = [
    { title: 'Policy', dataIndex: 'name', width: 240, render: (_, record) => <Space orientation="vertical" size={0}><Text strong>{record.name}</Text><Text type="secondary">{record.id}</Text></Space> },
    { title: 'Subject', dataIndex: 'subjectId', width: 180, render: (_, record) => <Text>{record.subjectType}:{record.subjectId}</Text> },
    { title: 'Effect', dataIndex: 'effect', width: 100, render: (value) => <StatusTag value={value} /> },
    { title: 'Risk', dataIndex: 'riskLevels', width: 160, render: (value) => compactList(value, 3) },
    { title: 'Conditions', dataIndex: 'conditions', width: 220, render: policyConditionSummary },
    { title: 'Scope', dataIndex: 'resourceScopes', render: scopeSummary },
    { title: '启用', dataIndex: 'enabled', width: 90, render: (value) => <StatusTag value={value ? 'enabled' : 'disabled'} /> },
    {
      title: '',
      key: 'actions',
      fixed: 'right',
      width: 96,
      render: (_, record) => (
        <Space className="soha-row-action-icons">
          <ManagementIconButton size="small" tooltip="编辑" aria-label="编辑 access policy" icon={<EditOutlined />} disabled={!canManage} onClick={() => setDrawer({ kind: 'access-policy', record })} />
          <Popconfirm title="删除 access policy？" description="该操作会立即更新 AI Gateway 控制面。" okButtonProps={{ danger: true, loading: deleteMutation.isPending }} onConfirm={() => deleteMutation.mutate({ kind: 'policy', id: record.id })}>
            <ManagementIconButton size="small" tooltip="删除" aria-label="删除 access policy" danger icon={<DeleteOutlined />} disabled={!canManage} />
          </Popconfirm>
        </Space>
      ),
    },
  ]

  const bindingColumns: TableColumnsType<SkillBinding> = [
    { title: 'Subject', dataIndex: 'subjectId', width: 220, render: (_, record) => <Space orientation="vertical" size={0}><Text strong>{record.subjectId}</Text><Text type="secondary">{record.subjectType}{record.aiClientId ? ` / ${record.aiClientId}` : ''}</Text></Space> },
    { title: 'Skill', dataIndex: 'skillId', width: 180, render: (value) => <Tag>{value}</Tag> },
    { title: 'Capabilities', dataIndex: 'capabilityRefs', render: (value) => compactList(value, 4) },
    { title: '启用', dataIndex: 'enabled', width: 90, render: (value) => <StatusTag value={value ? 'enabled' : 'disabled'} /> },
    {
      title: '',
      key: 'actions',
      fixed: 'right',
      width: 96,
      render: (_, record) => (
        <Space className="soha-row-action-icons">
          <ManagementIconButton size="small" tooltip="编辑" aria-label="编辑 skill binding" icon={<EditOutlined />} disabled={!canManage} onClick={() => setDrawer({ kind: 'skill-binding', record })} />
          <Popconfirm title="删除 skill binding？" description="该操作会立即更新 AI Gateway 控制面。" okButtonProps={{ danger: true, loading: deleteMutation.isPending }} onConfirm={() => deleteMutation.mutate({ kind: 'binding', id: record.id })}>
            <ManagementIconButton size="small" tooltip="删除" aria-label="删除 skill binding" danger icon={<DeleteOutlined />} disabled={!canManage} />
          </Popconfirm>
        </Space>
      ),
    },
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
    return <div className="soha-page"><ManagementState kind="no-permission" description="当前账号没有 AI Gateway 权限。" /></div>
  }

  return (
    <div className="soha-page">
      <ManagementDetailHeader
        title="企业 AI 运维控制面"
        description="统一管理 AI client、用户 login key、service account、tool grant、access policy、skill binding、审批与调用日志。"
        actions={(
          <ManagementTableToolbar>
            <Button size="small" icon={<ReloadOutlined />} onClick={() => void refreshAll()}>刷新</Button>
          </ManagementTableToolbar>
        )}
      />

      <Card
        size="small"
        variant="outlined"
        className="soha-management-panel-card"
        title={cardTitle(<SafetyCertificateOutlined />, gatewayPanelMeta.title)}
      >
        <Space orientation="vertical" size={12} style={{ width: '100%' }}>
          <Text type="secondary">{gatewayPanelMeta.description}</Text>
          {section === 'tokens' || section === 'governance' || section === 'call-logs' ? (
            <Text type="secondary">
              {sectionActiveTab === 'tokens'
                ? '用户 login key 在这里聚合展示；管理员可吊销，明文只在生成或轮换后展示一次。'
                : gatewayMenuMeta[sectionActiveTab].description}
            </Text>
          ) : null}
          {section === 'overview' ? (
            <Space orientation="vertical" size={12} style={{ width: '100%' }}>
              <div className="grid gap-3 lg:grid-cols-2">
                <Descriptions size="small" column={2} bordered items={[
                  { key: 'tools', label: 'Visible tools', children: summary.tools },
                  { key: 'clients', label: 'AI clients', children: summary.clients },
                  { key: 'tokens', label: 'Login keys', children: personalTokens.length },
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
                      <AdminTable shellClassName="soha-management-table-shell" columnSettingIconOnly columnSettingPlacement="header" rowKey="name" tableSize="small" columns={toolColumns} dataSource={manifest.tools} loading={manifestQuery.isLoading} pagination={{ pageSize: 8 }} scroll={{ x: 960 }} />
                    </>
                  ) : (
                    <ManagementState bordered={false} compact title="暂无 Manifest" description="选择 AI client、skill 或 source 后查看可调用工具清单。" />
                  )}
                </Space>
              ),
            },
            {
              key: 'clients',
              label: 'AI Clients',
              children: (
                <AdminTable
                  shellClassName="soha-management-table-shell"
                  columnSettingIconOnly
                  columnSettingPlacement="header"
                  rowKey="id"
                  tableSize="small"
                  columns={aiClientColumns}
                  dataSource={filteredClients}
                  loading={clientsQuery.isLoading}
                  scroll={{ x: 920 }}
                  title="AI Clients"
                  headerExtra={(
                    <ManagementTableToolbar>
                      <Button type="primary" size="small" icon={<PlusOutlined />} disabled={!canManage} onClick={() => setDrawer({ kind: 'ai-client' })}>新增 client</Button>
                      <Input allowClear size="small" style={{ width: 220 }} placeholder="过滤 client" value={clientFilter} onChange={(event) => setClientFilter(event.target.value)} />
                    </ManagementTableToolbar>
                  )}
                />
              ),
            },
            {
              key: 'tokens',
              label: 'Tokens',
              children: (
                <AdminTable
                  shellClassName="soha-management-table-shell"
                  columnSettingIconOnly
                  columnSettingPlacement="header"
                  rowKey="id"
                  tableSize="small"
                  columns={tokenColumns}
                  dataSource={filteredPersonalTokens}
                  loading={personalTokensQuery.isLoading}
                  scroll={{ x: 1080 }}
                  title={canManage ? 'User Login Keys' : 'My Login Keys'}
                  headerExtra={(
                    <ManagementTableToolbar>
                      <Button type="primary" size="small" icon={<PlusOutlined />} disabled={!canInvoke} onClick={() => setDrawer({ kind: 'personal-token' })}>生成我的 key</Button>
                      <Input allowClear size="small" style={{ width: 260 }} placeholder="过滤 key / owner" value={tokenFilter} onChange={(event) => setTokenFilter(event.target.value)} />
                    </ManagementTableToolbar>
                  )}
                />
              ),
            },
            {
              key: 'service-accounts',
              label: 'Service Accounts',
              children: (
                <Space orientation="vertical" size={12} style={{ width: '100%' }}>
                  <AdminTable
                    shellClassName="soha-management-table-shell"
                    columnSettingIconOnly
                    columnSettingPlacement="header"
                    rowKey="id"
                    tableSize="small"
                    columns={serviceAccountColumns}
                    dataSource={serviceAccounts}
                    loading={serviceAccountsQuery.isLoading}
                    scroll={{ x: 860 }}
                    title="Service Accounts"
                    headerExtra={(
                      <ManagementTableToolbar>
                        <Button type="primary" size="small" icon={<PlusOutlined />} disabled={!canManage} onClick={() => setDrawer({ kind: 'service-account' })}>新增服务账号</Button>
                      </ManagementTableToolbar>
                    )}
                  />
                  <AdminTable
                    shellClassName="soha-management-table-shell"
                    columnSettingIconOnly
                    columnSettingPlacement="header"
                    rowKey="id"
                    tableSize="small"
                    columns={serviceAccountTokenColumns}
                    dataSource={filteredServiceAccountTokens}
                    loading={serviceAccountTokensQuery.isLoading}
                    scroll={{ x: 1180 }}
                    title="Service Tokens"
                    headerExtra={(
                      <ManagementTableToolbar>
                        <Button size="small" danger icon={<StopOutlined />} disabled={!canManage} onClick={() => setDrawer({ kind: 'service-token-revoke' })}>吊销服务 token</Button>
                        <Input allowClear size="small" style={{ width: 260 }} placeholder="过滤 service token" value={serviceTokenFilter} onChange={(event) => setServiceTokenFilter(event.target.value)} />
                      </ManagementTableToolbar>
                    )}
                  />
                </Space>
              ),
            },
            {
              key: 'grants',
              label: 'Tool Grants',
              children: <AdminTable shellClassName="soha-management-table-shell" columnSettingIconOnly columnSettingPlacement="header" rowKey="id" tableSize="small" columns={grantColumns} dataSource={filteredGrants} loading={grantsQuery.isLoading} scroll={{ x: 1000 }} title="Tool Grants" headerExtra={(
                <ManagementTableToolbar>
                  <Button type="primary" size="small" icon={<PlusOutlined />} disabled={!canManage} onClick={() => setDrawer({ kind: 'tool-grant' })}>新增 grant</Button>
                  <Input allowClear size="small" style={{ width: 240 }} placeholder="过滤 grant" value={grantFilter} onChange={(event) => setGrantFilter(event.target.value)} />
                </ManagementTableToolbar>
              )} />,
            },
            {
              key: 'policies',
              label: 'Access Policies',
              children: <AdminTable shellClassName="soha-management-table-shell" columnSettingIconOnly columnSettingPlacement="header" rowKey="id" tableSize="small" columns={policyColumns} dataSource={filteredPolicies} loading={policiesQuery.isLoading} scroll={{ x: 1080 }} title="Access Policies" headerExtra={(
                <ManagementTableToolbar>
                  <Button type="primary" size="small" icon={<PlusOutlined />} disabled={!canManage} onClick={() => setDrawer({ kind: 'access-policy' })}>新增 policy</Button>
                  <Input allowClear size="small" style={{ width: 240 }} placeholder="过滤 policy" value={policyFilter} onChange={(event) => setPolicyFilter(event.target.value)} />
                </ManagementTableToolbar>
              )} />,
            },
            {
              key: 'bindings',
              label: 'Skill Bindings',
              children: <AdminTable shellClassName="soha-management-table-shell" columnSettingIconOnly columnSettingPlacement="header" rowKey="id" tableSize="small" columns={bindingColumns} dataSource={bindings} loading={bindingsQuery.isLoading} scroll={{ x: 920 }} title="Skill Bindings" headerExtra={<ManagementTableToolbar><Button type="primary" size="small" icon={<PlusOutlined />} disabled={!canManage} onClick={() => setDrawer({ kind: 'skill-binding' })}>新增 binding</Button></ManagementTableToolbar>} />,
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
                        <AdminTable
                          shellClassName="soha-management-table-shell"
                          columnSettingIconOnly
                          columnSettingPlacement="header"
                          rowKey={(record) => `${record.type}:${record.action}:${record.targetKind || ''}:${record.targetId || ''}`}
                          tableSize="small"
                          title="Recommendation actions"
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
                      <AdminTable shellClassName="soha-management-table-shell" columnSettingIconOnly columnSettingPlacement="header" rowKey="name" tableSize="small" columns={governanceHealthColumns} dataSource={governanceStatus.health.checks ?? []} loading={governanceQuery.isLoading} pagination={false} scroll={{ x: 860 }} />
                      <AdminTable shellClassName="soha-management-table-shell" columnSettingIconOnly columnSettingPlacement="header" rowKey="key" tableSize="small" columns={governanceCoverageColumns} dataSource={governanceCoverageRows(governanceStatus.policyCoverage)} pagination={false} scroll={{ x: 760 }} />
                      <AdminTable shellClassName="soha-management-table-shell" columnSettingIconOnly columnSettingPlacement="header" rowKey="key" tableSize="small" title="Redaction hits" columns={governanceRedactionColumns} dataSource={governanceRedactionRows(governanceStatus.redaction)} pagination={false} scroll={{ x: 820 }} />
                      <AdminTable shellClassName="soha-management-table-shell" columnSettingIconOnly columnSettingPlacement="header" rowKey="key" tableSize="small" title="Token findings" columns={governanceTokenFindingColumns} dataSource={governanceTokenFindingRows(governanceStatus.tokens)} pagination={{ pageSize: 6 }} scroll={{ x: 1160 }} />
                      <AdminTable shellClassName="soha-management-table-shell" columnSettingIconOnly columnSettingPlacement="header" rowKey="key" tableSize="small" columns={governanceQueueColumns} dataSource={governanceApprovalQueueRows(governanceStatus.approvals, governanceStatus.clients)} pagination={false} scroll={{ x: 720 }} />
                      <div className="grid gap-3 lg:grid-cols-3">
                        <AdminTable shellClassName="soha-management-table-shell" columnSettingIconOnly columnSettingPlacement="header" rowKey="key" tableSize="small" title="Top tools" columns={governanceMetricColumns} dataSource={governanceStatus.metrics.topTools ?? []} pagination={false} />
                        <AdminTable shellClassName="soha-management-table-shell" columnSettingIconOnly columnSettingPlacement="header" rowKey="key" tableSize="small" title="Top AI clients" columns={governanceMetricColumns} dataSource={governanceStatus.metrics.topAiClients ?? []} pagination={false} />
                        <AdminTable shellClassName="soha-management-table-shell" columnSettingIconOnly columnSettingPlacement="header" rowKey="key" tableSize="small" title="Top actors" columns={governanceMetricColumns} dataSource={governanceStatus.metrics.topActors ?? []} pagination={false} />
                      </div>
                      <AdminTable shellClassName="soha-management-table-shell" columnSettingIconOnly columnSettingPlacement="header" rowKey={(record) => `${record.type}:${record.policyId || record.grantId || record.approvalRequestId || record.actorId || record.aiClientId || record.toolName || record.summary}`} tableSize="small" columns={governanceFindingColumns} dataSource={governanceStatus.anomalies ?? []} pagination={{ pageSize: 8 }} scroll={{ x: 1180 }} />
                    </>
                  ) : (
                    <AdminTable
                      shellClassName="soha-management-table-shell"
                      columnSettingIconOnly
                      columnSettingPlacement="header"
                      rowKey="name"
                      tableSize="small"
                      columns={governanceHealthColumns}
                      dataSource={[]}
                      loading={governanceQuery.isLoading}
                      pagination={false}
                      empty={<ManagementState bordered={false} compact title="暂无治理状态" description="治理状态生成后会展示健康检查和建议动作。" />}
                    />
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
                  <AdminTable
                    shellClassName="soha-management-table-shell"
                    columnSettingIconOnly
                    columnSettingPlacement="header"
                    rowKey="id"
                    tableSize="small"
                    columns={approvalColumns}
                    dataSource={approvalRequests}
                    loading={approvalsQuery.isLoading}
                    scroll={{ x: 1560 }}
                    expandable={{ expandedRowRender: (record: ApprovalRequest) => <ApprovalTracePanel record={record} /> }}
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
                  <AdminTable shellClassName="soha-management-table-shell" columnSettingIconOnly columnSettingPlacement="header" rowKey="id" tableSize="small" columns={auditColumns} dataSource={auditLogs} loading={auditQuery.isLoading} scroll={{ x: 1220 }} expandable={{ expandedRowRender: (record: GatewayAuditLog) => <JsonBlock value={{ requestId: record.requestId, sourceIp: record.sourceIp, resourceScope: record.resourceScope, metadata: record.metadata }} /> }} />
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
          <Form.Item name="teamIds" label="组织">
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
                <Form.Item name="approvalApproverTeams" label="候选组织">
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
