import { Card, Form, Button, Toast, Spin, Modal, Tag, Space } from '@douyinfe/semi-ui'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { useEffect, useState } from 'react'
import { AdminTable } from '@/components/admin-table'
import { PageHeader } from '@/components/page-header'
import { api } from '@/services/api-client'
import { StatusTag } from '@/components/status-tag'
import { formatDateTime } from '@/utils/time'
import { tableColumnPresets } from '@/utils/table-columns'
import type { ApiResponse } from '@/types'
import type { ColumnProps } from '@douyinfe/semi-ui/lib/es/table'

/* ─── Identity Settings (OIDC) ─── */

interface OIDCSettings {
  enabled: boolean
  providerName: string
  issuer: string
  clientId: string
  clientSecret: string
  redirectUrl: string
  frontendRedirectUrl: string
  scopes: string[]
  defaultRoles: string[]
}

export function IdentitySettingsPage() {
  const queryClient = useQueryClient()

  const { data, isLoading } = useQuery({
    queryKey: ['settings-identity'],
    queryFn: () => api.get<ApiResponse<OIDCSettings>>('/settings/identity'),
    select: (response: any) => ({ data: response.data.oidc as OIDCSettings }),
  })

  const saveMutation = useMutation({
    mutationFn: (values: Record<string, unknown>) => api.put('/settings/identity/oidc', values),
    onSuccess: () => {
      Toast.success('身份设置已保存')
      queryClient.invalidateQueries({ queryKey: ['settings-identity'] })
    },
    onError: (err: Error) => Toast.error(err.message),
  })

  if (isLoading) {
    return <div className="flex items-center justify-center h-64"><Spin size="large" /></div>
  }

  const settings = data?.data

  return (
    <div className="kc-page">
      <PageHeader title="身份设置" description="配置 OIDC 身份提供商、客户端凭证和回调信息。" />
      <Card>
        <Form
          onSubmit={(values) => saveMutation.mutate(values as Record<string, unknown>)}
          initValues={settings ?? {}}
          labelPosition="left"
          labelWidth={140}
        >
          <Form.Switch field="enabled" label="启用 OIDC" />
          <Form.Input field="providerName" label="Provider Name" />
          <Form.Input field="issuer" label="Issuer URL" placeholder="https://accounts.example.com" rules={[{ required: true, message: '请输入 Issuer URL' }]} />
          <Form.Input field="clientId" label="Client ID" rules={[{ required: true, message: '请输入 Client ID' }]} />
          <Form.Input field="clientSecret" label="Client Secret" mode="password" rules={[{ required: true, message: '请输入 Client Secret' }]} />
          <Form.Input field="redirectUrl" label="Redirect URL" placeholder="https://your-app.com/auth/oidc/callback" />
          <Form.Input field="frontendRedirectUrl" label="Frontend Redirect URL" />
          <Form.TagInput field="scopes" label="Scopes" placeholder="openid / profile / email" />
          <Form.TagInput field="defaultRoles" label="Default Roles" placeholder="readonly / admin" />
          <div className="kc-form-actions">
            <Button htmlType="submit" theme="solid" loading={saveMutation.isPending}>保存设置</Button>
          </div>
        </Form>
      </Card>
    </div>
  )
}

/* ─── Monitoring Settings (Prometheus) ─── */

interface PrometheusSettings {
  enabled: boolean
  baseUrl: string
  bearerToken: string
  defaultRangeMinutes: number
  stepSeconds: number
  clusterLabel: string
  grafanaBaseUrl: string
}

export function MonitoringSettingsPage() {
  const queryClient = useQueryClient()

  const { data, isLoading } = useQuery({
    queryKey: ['settings-monitoring'],
    queryFn: () => api.get<ApiResponse<PrometheusSettings>>('/settings/monitoring'),
    select: (response: any) => ({ data: response.data.prometheus as PrometheusSettings }),
  })

  const saveMutation = useMutation({
    mutationFn: (values: Record<string, unknown>) => api.put('/settings/monitoring/prometheus', values),
    onSuccess: () => {
      Toast.success('监控设置已保存')
      queryClient.invalidateQueries({ queryKey: ['settings-monitoring'] })
    },
    onError: (err: Error) => Toast.error(err.message),
  })

  if (isLoading) {
    return <div className="flex items-center justify-center h-64"><Spin size="large" /></div>
  }

  const settings = data?.data

  return (
    <div className="kc-page">
      <PageHeader title="监控设置" description="配置 Prometheus 地址、默认查询范围和访问凭证。" />
      <Card>
        <Form
          onSubmit={(values) => saveMutation.mutate(values as Record<string, string>)}
          initValues={settings ?? {}}
          labelPosition="left"
          labelWidth={140}
        >
          <Form.Switch field="enabled" label="启用监控" />
          <Form.Input field="baseUrl" label="Prometheus URL" placeholder="http://prometheus:9090" rules={[{ required: true, message: '请输入 Prometheus URL' }]} />
          <Form.Input field="bearerToken" label="Bearer Token" mode="password" />
          <Form.InputNumber field="defaultRangeMinutes" label="默认范围(分钟)" />
          <Form.InputNumber field="stepSeconds" label="默认步长(秒)" />
          <Form.Input field="clusterLabel" label="Cluster Label" />
          <Form.Input field="grafanaBaseUrl" label="Grafana URL" />
          <div className="kc-form-actions">
            <Button htmlType="submit" theme="solid" loading={saveMutation.isPending}>保存设置</Button>
          </div>
        </Form>
      </Card>
    </div>
  )
}

/* ─── AI Settings ─── */

interface AISettings {
  enabled: boolean
  baseUrl: string
  apiKey: string
  model: string
}

interface DataSource {
  id: string
  name: string
  sourceKind: string
  backendType: string
  enabled: boolean
  credentialRef?: string
  mcpAdapter: string
  scope?: Record<string, unknown>
  queryBudget?: Record<string, unknown>
  redactionPolicy?: Record<string, unknown>
  config?: Record<string, unknown>
  validationStatus?: string
  validationMessage?: string
  lastValidatedAt?: string
}

interface AnalysisProfile {
  id: string
  name: string
  mode: string
  enabledSources?: string[]
  enabledPlaybooks?: string[]
  remediationPolicy: string
  enabled: boolean
  queryBudgets?: Record<string, unknown>
  outputStyle?: Record<string, unknown>
}

interface AutomationPolicy {
  id: string
  name: string
  triggerType: string
  analysisProfileId: string
  remediationPolicy: string
  enabled: boolean
  dedupWindowSeconds: number
  triggerConditions?: Record<string, unknown>
  approvalPolicy?: Record<string, unknown>
}

const PLAYBOOK_OPTIONS = [
  { value: 'release-correlation', label: 'release-correlation' },
  { value: 'cluster-health', label: 'cluster-health' },
  { value: 'access-drift', label: 'access-drift' },
  { value: 'runtime-instability', label: 'runtime-instability' },
  { value: 'alert-pressure', label: 'alert-pressure' },
  { value: 'build-queue', label: 'build-queue' },
  { value: 'error-burst', label: 'error-burst' },
  { value: 'dependency-timeout', label: 'dependency-timeout' },
]

const SEVERITY_OPTIONS = [
  { value: 'critical', label: 'critical' },
  { value: 'warning', label: 'warning' },
  { value: 'info', label: 'info' },
]

const STATUS_OPTIONS = [
  { value: 'firing', label: 'firing' },
  { value: 'resolved', label: 'resolved' },
]

function buildDataSourceFormValues(item?: DataSource | null) {
  return {
    id: item?.id,
    name: item?.name ?? '',
    sourceKind: item?.sourceKind ?? 'logs',
    backendType: item?.backendType ?? 'es',
    enabled: item?.enabled ?? true,
    credentialRef: item?.credentialRef ?? '',
    mcpAdapter: item?.mcpAdapter ?? 'logs.v1',
    scopeClusterId: String(item?.scope?.clusterId ?? ''),
    scopeNamespace: String(item?.scope?.namespace ?? ''),
    scopeService: String(item?.scope?.service ?? ''),
    scopeWorkload: String(item?.scope?.workload ?? ''),
    budgetMaxQueries: Number(item?.queryBudget?.maxQueries ?? 12),
    budgetMaxLogBytes: Number(item?.queryBudget?.maxLogBytes ?? 20_000_000),
    budgetTimeoutSeconds: Number(item?.queryBudget?.timeoutSeconds ?? 90),
    redactionMaskFields: Array.isArray(item?.redactionPolicy?.maskFields) ? item?.redactionPolicy?.maskFields as string[] : [],
    redactionMaskPatterns: Array.isArray(item?.redactionPolicy?.maskPatterns) ? item?.redactionPolicy?.maskPatterns as string[] : [],
    redactionTruncateLongLines: Boolean(item?.redactionPolicy?.truncateLongLines ?? true),
    configEndpoint: String(item?.config?.endpoint ?? ''),
    configIndex: String(item?.config?.index ?? ''),
    configTable: String(item?.config?.table ?? ''),
    configUsername: String(item?.config?.username ?? ''),
    configPassword: String(item?.config?.password ?? ''),
    configBearerToken: String(item?.config?.bearerToken ?? ''),
    configTimestampField: String(item?.config?.timestampField ?? '@timestamp'),
    configMessageField: String(item?.config?.messageField ?? 'message'),
    configSeverityField: String(item?.config?.severityField ?? 'level'),
    configServiceField: String(item?.config?.serviceField ?? 'service'),
    configWorkloadField: String(item?.config?.workloadField ?? 'workload'),
    configNamespaceField: String(item?.config?.namespaceField ?? 'namespace'),
    configClusterField: String(item?.config?.clusterField ?? 'cluster'),
    lokiLabelCluster: String((item?.config?.labelKeys as Record<string, unknown> | undefined)?.cluster ?? 'cluster'),
    lokiLabelNamespace: String((item?.config?.labelKeys as Record<string, unknown> | undefined)?.namespace ?? 'namespace'),
    lokiLabelService: String((item?.config?.labelKeys as Record<string, unknown> | undefined)?.service ?? 'service'),
    lokiLabelWorkload: String((item?.config?.labelKeys as Record<string, unknown> | undefined)?.workload ?? 'workload'),
    lokiLabelSeverity: String((item?.config?.labelKeys as Record<string, unknown> | undefined)?.severity ?? 'level'),
  }
}

function buildDataSourcePayload(values: Record<string, unknown>) {
  const sourceKind = String(values.sourceKind ?? 'logs')
  const backendType = String(values.backendType ?? 'es')
  const config: Record<string, unknown> = {
    endpoint: values.configEndpoint || undefined,
    timestampField: values.configTimestampField || undefined,
    messageField: values.configMessageField || undefined,
    severityField: values.configSeverityField || undefined,
    serviceField: values.configServiceField || undefined,
    workloadField: values.configWorkloadField || undefined,
    namespaceField: values.configNamespaceField || undefined,
    clusterField: values.configClusterField || undefined,
    username: values.configUsername || undefined,
    password: values.configPassword || undefined,
    bearerToken: values.configBearerToken || undefined,
  }
  if (backendType === 'es') config.index = values.configIndex || undefined
  if (backendType === 'clickhouse') config.table = values.configTable || undefined
  if (backendType === 'loki') {
    config.labelKeys = {
      cluster: values.lokiLabelCluster || 'cluster',
      namespace: values.lokiLabelNamespace || 'namespace',
      service: values.lokiLabelService || 'service',
      workload: values.lokiLabelWorkload || 'workload',
      severity: values.lokiLabelSeverity || 'level',
    }
  }
  return {
    id: values.id,
    name: values.name,
    sourceKind,
    backendType,
    enabled: values.enabled,
    credentialRef: values.credentialRef,
    mcpAdapter: values.mcpAdapter,
    scope: {
      clusterId: values.scopeClusterId || undefined,
      namespace: values.scopeNamespace || undefined,
      service: values.scopeService || undefined,
      workload: values.scopeWorkload || undefined,
    },
    queryBudget: {
      maxQueries: Number(values.budgetMaxQueries || 0),
      maxLogBytes: Number(values.budgetMaxLogBytes || 0),
      timeoutSeconds: Number(values.budgetTimeoutSeconds || 0),
    },
    redactionPolicy: {
      maskFields: values.redactionMaskFields || [],
      maskPatterns: values.redactionMaskPatterns || [],
      truncateLongLines: Boolean(values.redactionTruncateLongLines),
    },
    config,
  }
}

function buildProfileFormValues(item?: AnalysisProfile | null) {
  return {
    id: item?.id,
    name: item?.name ?? '',
    mode: item?.mode ?? 'root_cause',
    enabledSources: item?.enabledSources ?? [],
    enabledPlaybooks: item?.enabledPlaybooks ?? [],
    remediationPolicy: item?.remediationPolicy ?? 'suggest_only',
    defaultTimeRangeMinutes: Number((item as unknown as { defaultTimeRangeMinutes?: number } | undefined)?.defaultTimeRangeMinutes ?? 60),
    timeoutSeconds: Number((item as unknown as { timeoutSeconds?: number } | undefined)?.timeoutSeconds ?? 90),
    enabled: item?.enabled ?? true,
    budgetMaxQueries: Number(item?.queryBudgets?.maxQueries ?? 12),
    budgetMaxLogBytes: Number(item?.queryBudgets?.maxLogBytes ?? 20_000_000),
    budgetMaxEvidenceItems: Number(item?.queryBudgets?.maxEvidenceItems ?? 20),
    outputSummaryLevel: String(item?.outputStyle?.summaryLevel ?? 'standard'),
    outputIncludeEvidenceDetail: Boolean(item?.outputStyle?.includeEvidenceDetail ?? true),
    outputIncludeRecommendations: Boolean(item?.outputStyle?.includeRecommendations ?? true),
    outputIncludeTimeline: Boolean(item?.outputStyle?.includeTimeline ?? false),
  }
}

function buildProfilePayload(values: Record<string, unknown>) {
  return {
    id: values.id,
    name: values.name,
    mode: values.mode,
    enabledSources: values.enabledSources || [],
    enabledPlaybooks: values.enabledPlaybooks || [],
    remediationPolicy: values.remediationPolicy,
    defaultTimeRangeMinutes: Number(values.defaultTimeRangeMinutes || 60),
    timeoutSeconds: Number(values.timeoutSeconds || 90),
    enabled: values.enabled,
    queryBudgets: {
      maxQueries: Number(values.budgetMaxQueries || 0),
      maxLogBytes: Number(values.budgetMaxLogBytes || 0),
      maxEvidenceItems: Number(values.budgetMaxEvidenceItems || 0),
    },
    outputStyle: {
      summaryLevel: values.outputSummaryLevel,
      includeEvidenceDetail: Boolean(values.outputIncludeEvidenceDetail),
      includeRecommendations: Boolean(values.outputIncludeRecommendations),
      includeTimeline: Boolean(values.outputIncludeTimeline),
    },
  }
}

function buildPolicyFormValues(item?: AutomationPolicy | null) {
  const conditions = item?.triggerConditions ?? {}
  const labels = (conditions.labels as Record<string, unknown> | undefined) ?? {}
  const approval = item?.approvalPolicy ?? {}
  return {
    id: item?.id,
    name: item?.name ?? '',
    triggerType: item?.triggerType ?? 'alert_webhook',
    analysisProfileId: item?.analysisProfileId ?? '',
    remediationPolicy: item?.remediationPolicy ?? 'suggest_only',
    enabled: item?.enabled ?? true,
    dedupWindowSeconds: Number(item?.dedupWindowSeconds ?? 900),
    cooldownSeconds: Number((item as unknown as { cooldownSeconds?: number } | undefined)?.cooldownSeconds ?? 0),
    triggerSeverity: Array.isArray(conditions.severity) ? conditions.severity as string[] : [],
    triggerStatus: Array.isArray(conditions.status) ? conditions.status as string[] : [],
    triggerMinDurationSeconds: Number(conditions.min_duration_seconds ?? 120),
    triggerLabelKey: Object.keys(labels)[0] ?? '',
    triggerLabelValue: String(Object.values(labels)[0] ?? ''),
    triggerTimeRangeMinutes: Number(conditions.time_range_minutes ?? 60),
    approvalRequired: Boolean(approval.required ?? false),
    approvalRoles: Array.isArray(approval.approverRoles) ? approval.approverRoles as string[] : [],
  }
}

function buildPolicyPayload(values: Record<string, unknown>) {
  const labels: Record<string, unknown> = {}
  if (values.triggerLabelKey && values.triggerLabelValue) {
    labels[String(values.triggerLabelKey)] = values.triggerLabelValue
  }
  return {
    id: values.id,
    name: values.name,
    enabled: values.enabled,
    triggerType: values.triggerType,
    analysisProfileId: values.analysisProfileId,
    remediationPolicy: values.remediationPolicy,
    dedupWindowSeconds: Number(values.dedupWindowSeconds || 0),
    cooldownSeconds: Number(values.cooldownSeconds || 0),
    triggerConditions: {
      severity: values.triggerSeverity || [],
      status: values.triggerStatus || [],
      min_duration_seconds: Number(values.triggerMinDurationSeconds || 0),
      time_range_minutes: Number(values.triggerTimeRangeMinutes || 0),
      labels,
    },
    approvalPolicy: {
      required: Boolean(values.approvalRequired),
      approverRoles: values.approvalRoles || [],
    },
  }
}

export function AISettingsPage() {
  const queryClient = useQueryClient()
  const [dataSourceModalVisible, setDataSourceModalVisible] = useState(false)
  const [profileModalVisible, setProfileModalVisible] = useState(false)
  const [policyModalVisible, setPolicyModalVisible] = useState(false)
  const [editingDataSource, setEditingDataSource] = useState<DataSource | null>(null)
  const [editingProfile, setEditingProfile] = useState<AnalysisProfile | null>(null)
  const [editingPolicy, setEditingPolicy] = useState<AutomationPolicy | null>(null)
  const [dataSourceSourceKind, setDataSourceSourceKind] = useState('logs')
  const [dataSourceBackendType, setDataSourceBackendType] = useState('es')

  useEffect(() => {
    if (dataSourceModalVisible && editingDataSource) {
      setDataSourceSourceKind(editingDataSource.sourceKind)
      setDataSourceBackendType(editingDataSource.backendType)
      return
    }
    if (dataSourceModalVisible && !editingDataSource) {
      setDataSourceSourceKind('logs')
      setDataSourceBackendType('es')
    }
  }, [dataSourceModalVisible, editingDataSource])

  const { data, isLoading } = useQuery({
    queryKey: ['settings-ai'],
    queryFn: () => api.get<ApiResponse<AISettings>>('/settings/ai'),
    select: (response: any) => ({ data: response.data.provider as AISettings }),
  })
  const dataSourcesQuery = useQuery({
    queryKey: ['copilot-data-sources'],
    queryFn: () => api.get<ApiResponse<DataSource[]>>('/copilot/data-sources'),
  })
  const profilesQuery = useQuery({
    queryKey: ['copilot-analysis-profiles'],
    queryFn: () => api.get<ApiResponse<AnalysisProfile[]>>('/copilot/analysis-profiles'),
  })
  const policiesQuery = useQuery({
    queryKey: ['copilot-automation-policies'],
    queryFn: () => api.get<ApiResponse<AutomationPolicy[]>>('/copilot/automation-policies'),
  })
  const capabilitiesQuery = useQuery({
    queryKey: ['copilot-data-source-capabilities'],
    queryFn: () => api.get<ApiResponse<Array<{ id: string; name: string; sourceKind: string; supportedBackends?: string[] }>>>('/copilot/data-source-capabilities'),
  })

  const saveMutation = useMutation({
    mutationFn: (values: Record<string, unknown>) => api.put('/settings/ai/provider', values),
    onSuccess: () => {
      Toast.success('AI 设置已保存')
      queryClient.invalidateQueries({ queryKey: ['settings-ai'] })
    },
    onError: (err: Error) => Toast.error(err.message),
  })
  const dataSourceMutation = useMutation({
    mutationFn: ({ id, values }: { id?: string; values: Record<string, unknown> }) =>
      id ? api.put(`/copilot/data-sources/${id}`, buildDataSourcePayload(values)) : api.post('/copilot/data-sources', buildDataSourcePayload(values)),
    onSuccess: () => {
      Toast.success('数据源已保存')
      queryClient.invalidateQueries({ queryKey: ['copilot-data-sources'] })
      setDataSourceModalVisible(false)
      setEditingDataSource(null)
      setDataSourceBackendType('es')
    },
    onError: (err: Error) => Toast.error(err.message),
  })
  const validateDataSourceMutation = useMutation({
    mutationFn: (dataSourceID: string) => api.post<ApiResponse<DataSource>>(`/copilot/data-sources/${dataSourceID}/validate`),
    onSuccess: () => {
      Toast.success('数据源校验通过')
    },
    onError: (err: Error) => {
      Toast.error(err.message)
    },
    onSettled: () => {
      queryClient.invalidateQueries({ queryKey: ['copilot-data-sources'] })
    },
  })
  const profileMutation = useMutation({
    mutationFn: ({ id, values }: { id?: string; values: Record<string, unknown> }) =>
      id ? api.put(`/copilot/analysis-profiles/${id}`, buildProfilePayload(values)) : api.post('/copilot/analysis-profiles', buildProfilePayload(values)),
    onSuccess: () => {
      Toast.success('分析模板已保存')
      queryClient.invalidateQueries({ queryKey: ['copilot-analysis-profiles'] })
      setProfileModalVisible(false)
      setEditingProfile(null)
    },
    onError: (err: Error) => Toast.error(err.message),
  })
  const policyMutation = useMutation({
    mutationFn: ({ id, values }: { id?: string; values: Record<string, unknown> }) =>
      id ? api.put(`/copilot/automation-policies/${id}`, buildPolicyPayload(values)) : api.post('/copilot/automation-policies', buildPolicyPayload(values)),
    onSuccess: () => {
      Toast.success('自动化策略已保存')
      queryClient.invalidateQueries({ queryKey: ['copilot-automation-policies'] })
      setPolicyModalVisible(false)
      setEditingPolicy(null)
    },
    onError: (err: Error) => Toast.error(err.message),
  })

  if (isLoading) {
    return <div className="flex items-center justify-center h-64"><Spin size="large" /></div>
  }

  const settings = data?.data
  const dataSources = dataSourcesQuery.data?.data ?? []
  const profiles = profilesQuery.data?.data ?? []
  const policies = policiesQuery.data?.data ?? []
  const capabilityOptions = capabilitiesQuery.data?.data ?? []
  const filteredCapabilityOptions = capabilityOptions.filter((item) => item.sourceKind === dataSourceSourceKind)
  const backendOptions = dataSourceSourceKind === 'logs'
    ? [{ value: 'es', label: 'es' }, { value: 'loki', label: 'loki' }, { value: 'clickhouse', label: 'clickhouse' }]
    : dataSourceSourceKind === 'metrics'
      ? [{ value: 'prometheus', label: 'prometheus' }]
      : dataSourceSourceKind === 'traces'
        ? [{ value: 'jaeger', label: 'jaeger' }]
        : [{ value: 'platform', label: 'platform' }]

  const dataSourceColumns: ColumnProps<DataSource>[] = [
    { title: '名称', dataIndex: 'name' },
    { title: '能力层', dataIndex: 'mcpAdapter' },
    { title: '源类型', dataIndex: 'sourceKind', render: (value: string, record: DataSource) => `${value} / ${record.backendType}` },
    {
      title: '校验状态',
      dataIndex: 'validationStatus',
      render: (value: string | undefined, record: DataSource) => {
        const isPending = validateDataSourceMutation.isPending && validateDataSourceMutation.variables === record.id
        if (isPending) return <Tag color="orange">校验中</Tag>
        if (!value) return <Tag color="grey">未校验</Tag>
        const normalized = value.toLowerCase()
        const color = normalized === 'success' ? 'green' : normalized === 'error' ? 'red' : 'grey'
        const label = normalized === 'success' ? '已通过' : normalized === 'error' ? '失败' : value
        return (
          <div className="flex max-w-[240px] flex-col gap-1">
            <Tag color={color}>{label}</Tag>
            {record.validationMessage && normalized === 'error' ? (
              <div className="text-xs text-[var(--semi-color-text-2)]">{record.validationMessage}</div>
            ) : null}
          </div>
        )
      },
    },
    {
      title: '最近校验',
      dataIndex: 'lastValidatedAt',
      render: (value: string | undefined) => value ? formatDateTime(value) : '-',
    },
    { title: '启用', dataIndex: 'enabled', render: (value: boolean) => <StatusTag value={value ? 'success' : 'default'} /> },
    {
      ...tableColumnPresets.action,
      title: '操作',
      dataIndex: 'id',
      render: (_: unknown, record: DataSource) => (
        <Space>
          <Button
            size="small"
            theme="light"
            loading={validateDataSourceMutation.isPending && validateDataSourceMutation.variables === record.id}
            onClick={() => validateDataSourceMutation.mutate(record.id)}
          >
            校验连接
          </Button>
          <Button size="small" theme="borderless" onClick={() => { setEditingDataSource(record); setDataSourceSourceKind(record.sourceKind); setDataSourceBackendType(record.backendType); setDataSourceModalVisible(true) }}>编辑</Button>
        </Space>
      ),
    },
  ]

  const profileColumns: ColumnProps<AnalysisProfile>[] = [
    { title: '名称', dataIndex: 'name' },
    { title: '模式', dataIndex: 'mode' },
    { title: '数据源', dataIndex: 'enabledSources', render: (value: string[]) => <div className="flex flex-wrap gap-1">{(value ?? []).map((item) => <Tag key={item}>{item}</Tag>)}</div> },
    { title: 'Playbooks', dataIndex: 'enabledPlaybooks', render: (value: string[]) => <div className="flex flex-wrap gap-1">{(value ?? []).map((item) => <Tag key={item}>{item}</Tag>)}</div> },
    { title: '策略', dataIndex: 'remediationPolicy' },
    { ...tableColumnPresets.action, title: '操作', dataIndex: 'id', render: (_: unknown, record: AnalysisProfile) => <Button size="small" theme="borderless" onClick={() => { setEditingProfile(record); setProfileModalVisible(true) }}>编辑</Button> },
  ]

  const policyColumns: ColumnProps<AutomationPolicy>[] = [
    { title: '名称', dataIndex: 'name' },
    { title: '触发类型', dataIndex: 'triggerType' },
    { title: '分析模板', dataIndex: 'analysisProfileId' },
    { title: 'Dedup(s)', dataIndex: 'dedupWindowSeconds' },
    { title: '策略', dataIndex: 'remediationPolicy' },
    { title: '启用', dataIndex: 'enabled', render: (value: boolean) => <StatusTag value={value ? 'success' : 'default'} /> },
    { ...tableColumnPresets.action, title: '操作', dataIndex: 'id', render: (_: unknown, record: AutomationPolicy) => <Button size="small" theme="borderless" onClick={() => { setEditingPolicy(record); setPolicyModalVisible(true) }}>编辑</Button> },
  ]

  return (
    <div className="kc-page">
      <PageHeader title="AI 设置" description="配置 AI 提供商、模型、API Key 与基础接入地址。" />
      <Card>
        <Form
          onSubmit={(values) => saveMutation.mutate(values as Record<string, unknown>)}
          initValues={settings ?? {}}
          labelPosition="left"
          labelWidth={140}
        >
          <Form.Switch field="enabled" label="启用 AI" />
          <Form.Input field="apiKey" label="API Key" mode="password" rules={[{ required: true, message: '请输入 API Key' }]} />
          <Form.Input field="model" label="模型" placeholder="gpt-4o / claude-sonnet-4-20250514" />
          <Form.Input field="baseUrl" label="Base URL" placeholder="https://api.openai.com/v1 (可选)" />
          <div className="kc-form-actions">
            <Button htmlType="submit" theme="solid" loading={saveMutation.isPending}>保存设置</Button>
          </div>
        </Form>
      </Card>
      <Card title="Data Sources" headerExtraContent={<Button theme="solid" onClick={() => { setEditingDataSource(null); setDataSourceSourceKind('logs'); setDataSourceBackendType('es'); setDataSourceModalVisible(true) }}>新增</Button>}>
        <AdminTable columns={dataSourceColumns} dataSource={dataSources} rowKey="id" loading={dataSourcesQuery.isLoading} />
      </Card>
      <Card title="Analysis Profiles" headerExtraContent={<Button theme="solid" onClick={() => { setEditingProfile(null); setProfileModalVisible(true) }}>新增</Button>}>
        <AdminTable columns={profileColumns} dataSource={profiles} rowKey="id" loading={profilesQuery.isLoading} />
      </Card>
      <Card title="Automation Policies" headerExtraContent={<Button theme="solid" onClick={() => { setEditingPolicy(null); setPolicyModalVisible(true) }}>新增</Button>}>
        <AdminTable columns={policyColumns} dataSource={policies} rowKey="id" loading={policiesQuery.isLoading} />
      </Card>

      <Modal title={editingDataSource ? '编辑数据源' : '新增数据源'} visible={dataSourceModalVisible} footer={null} onCancel={() => { setDataSourceModalVisible(false); setEditingDataSource(null); setDataSourceSourceKind('logs'); setDataSourceBackendType('es') }}>
        <Form
          initValues={buildDataSourceFormValues(editingDataSource)}
          onSubmit={(values) => dataSourceMutation.mutate({ id: editingDataSource?.id, values })}
          labelPosition="left"
          labelWidth={140}
        >
          <div className="mb-4 rounded border border-[var(--semi-color-border)] bg-[var(--semi-color-fill-0)] p-3 text-sm">
            <div className="font-medium">1. 基础信息</div>
            <div className="mt-1 text-[var(--semi-color-text-2)]">先选择数据源的能力类别和后端类型，再填写连接与查询约束。</div>
          </div>
          <Form.Input field="name" label="名称" rules={[{ required: true, message: '请输入名称' }]} />
          <Form.Select
            field="sourceKind"
            label="源类型"
            optionList={[{ value: 'logs', label: 'logs' }, { value: 'metrics', label: 'metrics' }, { value: 'traces', label: 'traces' }, { value: 'platform-native', label: 'platform-native' }]}
            onChange={(value) => {
              const next = String(value)
              setDataSourceSourceKind(next)
              setDataSourceBackendType(next === 'logs' ? 'es' : next === 'metrics' ? 'prometheus' : next === 'traces' ? 'jaeger' : 'platform')
            }}
          />
          <Form.Select
            field="backendType"
            label="后端类型"
            optionList={backendOptions}
            onChange={(value) => setDataSourceBackendType(String(value))}
          />
          <Form.Select field="mcpAdapter" label="能力层" optionList={filteredCapabilityOptions.map((item) => ({ value: item.id, label: item.name }))} />
          <Form.Input field="credentialRef" label="凭据引用" />
          <div className="mb-4 mt-6 rounded border border-[var(--semi-color-border)] bg-[var(--semi-color-fill-0)] p-3 text-sm">
            <div className="font-medium">2. 作用范围与预算</div>
            <div className="mt-1 text-[var(--semi-color-text-2)]">限制这个数据源在 AI 分析中的默认作用范围、查询次数和输出规模。</div>
          </div>
          <Form.Input field="scopeClusterId" label="Scope Cluster" />
          <Form.Input field="scopeNamespace" label="Scope Namespace" />
          <Form.Input field="scopeService" label="Scope Service" />
          <Form.Input field="scopeWorkload" label="Scope Workload" />
          <Form.InputNumber field="budgetMaxQueries" label="Max Queries" min={1} />
          <Form.InputNumber field="budgetMaxLogBytes" label="Max Log Bytes" min={1024} />
          <Form.InputNumber field="budgetTimeoutSeconds" label="Timeout(s)" min={1} />
          <Form.TagInput field="redactionMaskFields" label="Mask Fields" />
          <Form.TagInput field="redactionMaskPatterns" label="Mask Patterns" />
          <Form.Switch field="redactionTruncateLongLines" label="Truncate Long Lines" />
          <div className="mb-4 mt-6 rounded border border-[var(--semi-color-border)] bg-[var(--semi-color-fill-0)] p-3 text-sm">
            <div className="font-medium">3. 后端连接</div>
            <div className="mt-1 text-[var(--semi-color-text-2)]">这里只展示当前后端类型需要的关键字段，避免无关配置干扰。</div>
          </div>
          <Form.Input field="configEndpoint" label="Endpoint" rules={[{ required: true, message: '请输入 Endpoint' }]} />
          {dataSourceBackendType === 'es' ? <Form.Input field="configIndex" label="ES Index" rules={[{ required: true, message: '请输入 ES Index' }]} /> : null}
          {dataSourceBackendType === 'clickhouse' ? <Form.Input field="configTable" label="CK Table" rules={[{ required: true, message: '请输入 CK Table' }]} /> : null}
          {dataSourceBackendType === 'clickhouse' ? <Form.Input field="configUsername" label="Username" /> : null}
          {dataSourceBackendType === 'clickhouse' ? <Form.Input field="configPassword" label="Password" mode="password" /> : null}
          {dataSourceBackendType !== 'clickhouse' && dataSourceBackendType !== 'platform' ? <Form.Input field="configBearerToken" label="Bearer Token" mode="password" /> : null}
          {dataSourceSourceKind === 'logs' ? <Form.Input field="configTimestampField" label="Timestamp Field" /> : null}
          {dataSourceSourceKind === 'logs' ? <Form.Input field="configMessageField" label="Message Field" /> : null}
          {dataSourceSourceKind === 'logs' ? <Form.Input field="configSeverityField" label="Severity Field" /> : null}
          {dataSourceSourceKind === 'logs' ? <Form.Input field="configServiceField" label="Service Field" /> : null}
          {dataSourceSourceKind === 'logs' ? <Form.Input field="configWorkloadField" label="Workload Field" /> : null}
          {dataSourceSourceKind === 'logs' ? <Form.Input field="configNamespaceField" label="Namespace Field" /> : null}
          {dataSourceSourceKind === 'logs' ? <Form.Input field="configClusterField" label="Cluster Field" /> : null}
          {dataSourceBackendType === 'loki' ? <Form.Input field="lokiLabelCluster" label="Loki Cluster Label" rules={[{ required: true, message: '请输入 Loki Cluster Label' }]} /> : null}
          {dataSourceBackendType === 'loki' ? <Form.Input field="lokiLabelNamespace" label="Loki Namespace Label" rules={[{ required: true, message: '请输入 Loki Namespace Label' }]} /> : null}
          {dataSourceBackendType === 'loki' ? <Form.Input field="lokiLabelService" label="Loki Service Label" rules={[{ required: true, message: '请输入 Loki Service Label' }]} /> : null}
          {dataSourceBackendType === 'loki' ? <Form.Input field="lokiLabelWorkload" label="Loki Workload Label" rules={[{ required: true, message: '请输入 Loki Workload Label' }]} /> : null}
          {dataSourceBackendType === 'loki' ? <Form.Input field="lokiLabelSeverity" label="Loki Severity Label" rules={[{ required: true, message: '请输入 Loki Severity Label' }]} /> : null}
          <Form.Switch field="enabled" label="启用" />
          <div className="kc-form-actions">
            <Button onClick={() => { setDataSourceModalVisible(false); setEditingDataSource(null); setDataSourceSourceKind('logs'); setDataSourceBackendType('es') }}>取消</Button>
            <Button htmlType="submit" theme="solid" loading={dataSourceMutation.isPending}>保存</Button>
          </div>
        </Form>
      </Modal>

      <Modal title={editingProfile ? '编辑分析模板' : '新增分析模板'} visible={profileModalVisible} footer={null} onCancel={() => { setProfileModalVisible(false); setEditingProfile(null) }}>
        <Form
          initValues={buildProfileFormValues(editingProfile)}
          onSubmit={(values) => profileMutation.mutate({ id: editingProfile?.id, values })}
          labelPosition="left"
          labelWidth={140}
        >
          <Form.Input field="name" label="名称" rules={[{ required: true, message: '请输入名称' }]} />
          <Form.Select field="mode" label="模式" optionList={[{ value: 'root_cause', label: 'root_cause' }, { value: 'inspection', label: 'inspection' }]} />
          <Form.Select field="enabledSources" label="数据源" multiple optionList={dataSources.map((item) => ({ value: item.id, label: `${item.name} (${item.sourceKind}/${item.backendType})` }))} />
          <Form.Select field="enabledPlaybooks" label="Playbooks" multiple optionList={PLAYBOOK_OPTIONS} />
          <Form.Input field="remediationPolicy" label="修复策略" />
          <Form.InputNumber field="defaultTimeRangeMinutes" label="默认时间范围(分钟)" min={5} />
          <Form.InputNumber field="timeoutSeconds" label="超时(秒)" min={10} />
          <Form.InputNumber field="budgetMaxQueries" label="Max Queries" min={1} />
          <Form.InputNumber field="budgetMaxLogBytes" label="Max Log Bytes" min={1024} />
          <Form.InputNumber field="budgetMaxEvidenceItems" label="Max Evidence Items" min={1} />
          <Form.Select field="outputSummaryLevel" label="Summary Level" optionList={[{ value: 'compact', label: 'compact' }, { value: 'standard', label: 'standard' }, { value: 'detailed', label: 'detailed' }]} />
          <Form.Switch field="outputIncludeEvidenceDetail" label="Include Evidence Detail" />
          <Form.Switch field="outputIncludeRecommendations" label="Include Recommendations" />
          <Form.Switch field="outputIncludeTimeline" label="Include Timeline" />
          <Form.Switch field="enabled" label="启用" />
          <div className="kc-form-actions">
            <Button onClick={() => { setProfileModalVisible(false); setEditingProfile(null) }}>取消</Button>
            <Button htmlType="submit" theme="solid" loading={profileMutation.isPending}>保存</Button>
          </div>
        </Form>
      </Modal>

      <Modal title={editingPolicy ? '编辑自动化策略' : '新增自动化策略'} visible={policyModalVisible} footer={null} onCancel={() => { setPolicyModalVisible(false); setEditingPolicy(null) }}>
        <Form
          initValues={buildPolicyFormValues(editingPolicy)}
          onSubmit={(values) => policyMutation.mutate({ id: editingPolicy?.id, values })}
          labelPosition="left"
          labelWidth={140}
        >
          <Form.Input field="name" label="名称" rules={[{ required: true, message: '请输入名称' }]} />
          <Form.Select field="triggerType" label="触发类型" optionList={[{ value: 'alert_webhook', label: 'alert_webhook' }]} />
          <Form.Select field="analysisProfileId" label="分析模板" optionList={profiles.map((item) => ({ value: item.id, label: item.name }))} />
          <Form.Input field="remediationPolicy" label="修复策略" />
          <Form.InputNumber field="dedupWindowSeconds" label="Dedup 窗口(s)" min={0} />
          <Form.InputNumber field="cooldownSeconds" label="Cooldown(s)" min={0} />
          <Form.Select field="triggerSeverity" label="告警级别" multiple optionList={SEVERITY_OPTIONS} />
          <Form.Select field="triggerStatus" label="告警状态" multiple optionList={STATUS_OPTIONS} />
          <Form.InputNumber field="triggerMinDurationSeconds" label="最小持续(s)" min={0} />
          <Form.Input field="triggerLabelKey" label="标签 Key" />
          <Form.Input field="triggerLabelValue" label="标签 Value" />
          <Form.InputNumber field="triggerTimeRangeMinutes" label="分析时间范围(分钟)" min={5} />
          <Form.Switch field="approvalRequired" label="需要审批" />
          <Form.TagInput field="approvalRoles" label="审批角色" />
          <Form.Switch field="enabled" label="启用" />
          <div className="kc-form-actions">
            <Button onClick={() => { setPolicyModalVisible(false); setEditingPolicy(null) }}>取消</Button>
            <Button htmlType="submit" theme="solid" loading={policyMutation.isPending}>保存</Button>
          </div>
        </Form>
      </Modal>
    </div>
  )
}
