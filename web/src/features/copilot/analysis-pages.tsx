import { useEffect, useMemo, useState } from 'react'
import {
  Button,
  Card,
  Empty,
  Form,
  Modal,
  Space,
  Tag,
  Toast,
  Typography,
} from '@douyinfe/semi-ui'
import { IconPlay, IconPulse, IconSearch, IconPlus } from '@douyinfe/semi-icons'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { AdminTable } from '@/components/admin-table'
import { PageHeader } from '@/components/page-header'
import { StatGrid } from '@/components/stat-grid'
import { BooleanTag, StatusTag } from '@/components/status-tag'
import { useI18n } from '@/i18n'
import { api } from '@/services/api-client'
import { formatDateTime, formatRelativeTime } from '@/utils/time'
import { tableColumnPresets } from '@/utils/table-columns'
import type { ApiResponse, Cluster } from '@/types'
import type { ColumnProps } from '@douyinfe/semi-ui/lib/es/table'

const { Text, Paragraph } = Typography

interface Insight {
  title: string
  description: string
  severity: string
  actions?: string[]
}

interface AlertSummary {
  totalCount: number
  firingCount: number
  resolvedCount: number
  criticalCount: number
  warningCount: number
  infoCount: number
  channelCount: number
  lastReceivedAt?: string
}

interface AlertItem {
  id: string
  title?: string
  name?: string
  severity: string
  status: string
  source: string
  startsAt: string
}

interface RootCauseEvidence {
  id: string
  kind: string
  title: string
  summary: string
  severity?: string
  clusterId?: string
  namespace?: string
  timestamp?: string
  attributes?: Record<string, unknown>
}

interface SampleLogRecord {
  timestamp?: string
  severity?: string
  service?: string
  workload?: string
  namespace?: string
  clusterId?: string
  message?: string
}

interface RootCauseHypothesis {
  id: string
  title: string
  summary: string
  confidence: number
  evidenceIds?: string[]
  recommendations?: string[]
}

interface RootCauseRun {
  id: string
  title: string
  createdBy: string
  analysisProfileId?: string
  triggerType?: string
  status: string
  severity: string
  summary: string
  clusterId?: string
  namespace?: string
  workloadKind?: string
  workloadName?: string
  alertId?: string
  timeRangeMinutes: number
  question?: string
  evidence?: RootCauseEvidence[]
  hypotheses?: RootCauseHypothesis[]
  recommendations?: string[]
  dataSourceSnapshot?: Record<string, unknown>
  playbookResults?: Record<string, unknown>
  remediationPlan?: Record<string, unknown>
  createdAt: string
  updatedAt: string
}

function evidenceAttribute(record: RootCauseEvidence, key: string) {
  return record.attributes?.[key]
}

function formatMapPreview(value: unknown) {
  if (!value || typeof value !== 'object') return '-'
  return JSON.stringify(value, null, 2)
}

function renderSampleWindow(record: RootCauseEvidence) {
  const sampleWindow = evidenceAttribute(record, 'sampleWindow')
  if (!sampleWindow || typeof sampleWindow !== 'object') return '-'
  const current = sampleWindow as { timeFrom?: string; timeTo?: string }
  if (!current.timeFrom && !current.timeTo) return '-'
  return [current.timeFrom ? formatDateTime(current.timeFrom) : '-', current.timeTo ? formatDateTime(current.timeTo) : '-'].join(' ~ ')
}

function sampleRecords(record: RootCauseEvidence) {
  const items = evidenceAttribute(record, 'sampleRecords')
  return Array.isArray(items) ? items as SampleLogRecord[] : []
}

function isLogEvidence(record: RootCauseEvidence) {
  const kind = String(record.kind ?? '').toLowerCase()
  return kind.startsWith('logs') || kind === 'log'
}

interface AnalysisProfile {
  id: string
  name: string
  mode: string
  enabledSources?: string[]
}

interface InspectionTask {
  id: string
  title: string
  scopeType: string
  clusterId?: string
  namespace?: string
  checks?: string[]
  enabled: boolean
  intervalMinutes: number
  lastRunAt?: string
  createdAt: string
  updatedAt: string
}

interface InspectionFinding {
  id: string
  title: string
  severity: string
  summary: string
}

interface InspectionRun {
  id: string
  taskId: string
  triggeredBy: string
  status: string
  severity: string
  summary: string
  findings?: InspectionFinding[]
  startedAt: string
  completedAt?: string
  createdAt: string
}

const INSPECTION_CHECK_OPTIONS = [
  { value: 'cluster_health', label: '集群健康 / Cluster Health' },
  { value: 'alert_pressure', label: '告警压力 / Alert Pressure' },
  { value: 'channel_readiness', label: '通知通道 / Channel Readiness' },
  { value: 'release_safety', label: '发布安全 / Release Safety' },
  { value: 'build_queue', label: '构建队列 / Build Queue' },
  { value: 'audit_denials', label: '审计拒绝 / Audit Denials' },
]

function buildPerformanceSignals(summary?: AlertSummary, clusters: Cluster[] = [], localeCode: 'zh_CN' | 'en_US' = 'zh_CN') {
  const degradedClusters = clusters.filter((item) => item.health?.status && item.health.status !== 'healthy' && item.health.status !== 'ok')
  const items = []

  if (summary) {
    items.push({
      title: localeCode === 'zh_CN' ? '告警吞吐压力' : 'Alert Throughput Pressure',
      severity: summary.criticalCount > 0 ? 'warning' : 'info',
      description: localeCode === 'zh_CN'
        ? `当前 ${summary.firingCount} 条活跃告警，其中 ${summary.criticalCount} 条为严重告警。`
        : `${summary.firingCount} active alerts are firing, including ${summary.criticalCount} critical alerts.`,
      recommendation: localeCode === 'zh_CN'
        ? '先核对告警分组与通知路径，确认是否存在重复告警或未收敛信号。'
        : 'Review alert grouping and notification paths first to confirm whether duplicated or noisy signals are inflating pressure.',
    })
    items.push({
      title: localeCode === 'zh_CN' ? '通知覆盖度' : 'Notification Coverage',
      severity: summary.channelCount > 0 ? 'info' : 'warning',
      description: localeCode === 'zh_CN'
        ? `当前登记 ${summary.channelCount} 个通知渠道，最近一次告警接收时间为 ${summary.lastReceivedAt ? formatDateTime(summary.lastReceivedAt) : '-'}.`
        : `${summary.channelCount} notification channels are registered. Last alert ingestion time: ${summary.lastReceivedAt ? formatDateTime(summary.lastReceivedAt) : '-'} .`,
      recommendation: localeCode === 'zh_CN'
        ? '建议把高优先级告警至少路由到一个实时通知渠道，并确认静默规则没有过度覆盖。'
        : 'Route high-priority alerts to at least one real-time channel and verify silence rules are not masking too much.',
    })
  }

  items.push({
    title: localeCode === 'zh_CN' ? '集群稳定性' : 'Cluster Stability',
    severity: degradedClusters.length > 0 ? 'warning' : 'info',
    description: localeCode === 'zh_CN'
      ? `当前发现 ${degradedClusters.length} 个异常集群。`
      : `${degradedClusters.length} clusters are currently degraded.`,
    recommendation: localeCode === 'zh_CN'
      ? '优先回看异常集群的最近检查时间、事件流和工作负载变更窗口。'
      : 'Review degraded cluster checks, event streams, and recent workload change windows first.',
  })

  return items
}

export function RootCauseAnalysisPage() {
  const { localeCode } = useI18n()
  const queryClient = useQueryClient()
  const [selectedRunID, setSelectedRunID] = useState<string>()
  const [evidenceKindFilter, setEvidenceKindFilter] = useState<string>('all')
  const [logsOnly, setLogsOnly] = useState(false)
  const insightsQuery = useQuery({
    queryKey: ['copilot-insights'],
    queryFn: () => api.get<ApiResponse<Insight[]>>('/copilot/insights'),
  })
  const clustersQuery = useQuery({
    queryKey: ['clusters', 'root-cause'],
    queryFn: () => api.get<ApiResponse<Cluster[]>>('/clusters'),
  })
  const profilesQuery = useQuery({
    queryKey: ['copilot-analysis-profiles', 'root-cause'],
    queryFn: () => api.get<ApiResponse<AnalysisProfile[]>>('/copilot/analysis-profiles'),
  })
  const alertsQuery = useQuery({
    queryKey: ['alerts', 'root-cause'],
    queryFn: () => api.get<ApiResponse<AlertItem[]>>('/alerts'),
  })
  const runsQuery = useQuery({
    queryKey: ['copilot-root-cause-runs'],
    queryFn: () => api.get<ApiResponse<RootCauseRun[]>>('/copilot/root-cause/runs'),
  })

  const createRunMutation = useMutation({
    mutationFn: (values: Record<string, unknown>) => api.post<ApiResponse<RootCauseRun>>('/copilot/root-cause/runs', values),
    onSuccess: (response) => {
      Toast.success(localeCode === 'zh_CN' ? '根因分析已创建' : 'Root cause analysis started')
      queryClient.invalidateQueries({ queryKey: ['copilot-root-cause-runs'] })
      setSelectedRunID(response.data.id)
    },
    onError: (err: Error) => Toast.error(err.message),
  })

  const insights = insightsQuery.data?.data ?? []
  const alerts = alertsQuery.data?.data ?? []
  const activeAlerts = alerts.filter((item) => item.status === 'firing')
  const clusters = clustersQuery.data?.data ?? []
  const profiles = (profilesQuery.data?.data ?? []).filter((item) => item.mode === 'root_cause')
  const runs = runsQuery.data?.data ?? []
  const selectedRun = useMemo(() => {
    if (runs.length === 0) return undefined
    return runs.find((item) => item.id === selectedRunID) ?? runs[0]
  }, [runs, selectedRunID])
  const availableEvidenceKinds = useMemo(() => {
    return Array.from(new Set((selectedRun?.evidence ?? []).map((item) => item.kind).filter(Boolean))).sort()
  }, [selectedRun])
  const filteredEvidence = useMemo(() => {
    return (selectedRun?.evidence ?? []).filter((item) => {
      if (logsOnly && !isLogEvidence(item)) return false
      if (evidenceKindFilter !== 'all' && item.kind !== evidenceKindFilter) return false
      return true
    })
  }, [selectedRun, evidenceKindFilter, logsOnly])

  useEffect(() => {
    setEvidenceKindFilter('all')
    setLogsOnly(false)
  }, [selectedRun?.id])

  return (
    <div className="kc-page">
      <PageHeader
        title={localeCode === 'zh_CN' ? '链路根因分析' : 'Root Cause Analysis'}
        description={localeCode === 'zh_CN' ? '汇总集群、告警与交付信号，形成 AI 根因洞察和建议排查路径。' : 'Aggregate cluster, alert, and delivery signals into AI-assisted root cause insights and investigation paths.'}
      />
      <StatGrid
        items={[
          { label: localeCode === 'zh_CN' ? 'AI洞察数' : 'AI Insights', value: insights.length, icon: <IconSearch size="extra-large" /> },
          { label: localeCode === 'zh_CN' ? '活跃告警' : 'Active Alerts', value: activeAlerts.length, icon: <IconPulse size="extra-large" /> },
          { label: localeCode === 'zh_CN' ? '分析运行' : 'Analysis Runs', value: runs.length, icon: <IconSearch size="extra-large" /> },
        ]}
      />
      <Card title={localeCode === 'zh_CN' ? '发起根因分析' : 'Start Root Cause Analysis'}>
        <Form
          initValues={{ timeRangeMinutes: 60, workloadKind: 'Deployment' }}
          onSubmit={(values) => createRunMutation.mutate(values as Record<string, unknown>)}
          labelPosition="left"
          labelWidth={140}
        >
          <Form.Select field="analysisProfileId" label={localeCode === 'zh_CN' ? '分析模板' : 'Analysis Profile'} optionList={profiles.map((item) => ({ value: item.id, label: item.name }))} />
          <Form.Select field="clusterId" label={localeCode === 'zh_CN' ? '集群' : 'Cluster'} optionList={clusters.map((item) => ({ value: item.id, label: item.name }))} />
          <Form.Input field="namespace" label={localeCode === 'zh_CN' ? '命名空间' : 'Namespace'} />
          <Form.Input field="workloadName" label={localeCode === 'zh_CN' ? '工作负载' : 'Workload'} />
          <Form.Select field="alertId" label={localeCode === 'zh_CN' ? '关联告警' : 'Alert'} optionList={activeAlerts.map((item) => ({ value: item.id, label: item.title || item.name || item.id }))} />
          <Form.InputNumber field="timeRangeMinutes" label={localeCode === 'zh_CN' ? '时间范围(分钟)' : 'Time Range (min)'} min={5} />
          <Form.Input field="question" label={localeCode === 'zh_CN' ? '补充问题' : 'Question'} />
          <div className="kc-form-actions">
            <Button htmlType="submit" theme="solid" loading={createRunMutation.isPending}>
              {localeCode === 'zh_CN' ? '开始分析' : 'Run Analysis'}
            </Button>
          </div>
        </Form>
      </Card>
      <Card title={localeCode === 'zh_CN' ? 'AI 根因洞察' : 'AI Root Cause Insights'}>
        {insights.length === 0 ? (
          <Empty description={localeCode === 'zh_CN' ? '当前没有可用洞察' : 'No insights available'} />
        ) : (
          <div className="kc-list-panel">
            {insights.map((item, index) => (
              <div key={`${item.title}-${index}`} className="kc-list-row">
                <div className="kc-list-row-meta">
                  <Text strong>{item.title}</Text>
                  <StatusTag value={item.severity || 'info'} />
                </div>
                <Paragraph style={{ marginBottom: 8 }}>{item.description}</Paragraph>
                <div className="flex flex-wrap gap-2">
                  {(item.actions ?? []).map((action) => (
                    <Tag key={action} color="blue">{action}</Tag>
                  ))}
                </div>
              </div>
            ))}
          </div>
        )}
      </Card>
      <Card title={localeCode === 'zh_CN' ? '最近活跃告警' : 'Recent Active Alerts'}>
        <AdminTable
          columns={[
            { title: localeCode === 'zh_CN' ? '名称' : 'Name', dataIndex: 'name' },
            { ...tableColumnPresets.status, title: localeCode === 'zh_CN' ? '严重程度' : 'Severity', dataIndex: 'severity', render: (value: string) => <StatusTag value={value} /> },
            { title: localeCode === 'zh_CN' ? '来源' : 'Source', dataIndex: 'source' },
            { ...tableColumnPresets.datetime, title: localeCode === 'zh_CN' ? '触发时间' : 'Triggered At', dataIndex: 'startsAt', render: (value: string) => formatDateTime(value) },
          ]}
          dataSource={activeAlerts}
          rowKey="id"
          loading={alertsQuery.isLoading}
          pageSize={10}
        />
      </Card>
      <Card title={localeCode === 'zh_CN' ? '根因分析运行记录' : 'Root Cause Analysis Runs'}>
        <AdminTable
          columns={[
            { title: localeCode === 'zh_CN' ? '标题' : 'Title', dataIndex: 'title' },
            { ...tableColumnPresets.status, title: localeCode === 'zh_CN' ? '状态' : 'Status', dataIndex: 'status', render: (value: string) => <StatusTag value={value} /> },
            { ...tableColumnPresets.status, title: localeCode === 'zh_CN' ? '严重度' : 'Severity', dataIndex: 'severity', render: (value: string) => <StatusTag value={value} /> },
            { title: localeCode === 'zh_CN' ? '范围' : 'Scope', dataIndex: 'clusterId', render: (_: string, record: RootCauseRun) => [record.clusterId, record.namespace, record.workloadName].filter(Boolean).join(' / ') || '-' },
            { ...tableColumnPresets.datetime, title: localeCode === 'zh_CN' ? '创建时间' : 'Created At', dataIndex: 'createdAt', render: (value: string) => formatDateTime(value) },
            { ...tableColumnPresets.action, title: localeCode === 'zh_CN' ? '操作' : 'Actions', dataIndex: 'id', render: (value: string) => <Button size="small" theme="borderless" onClick={() => setSelectedRunID(value)}>{localeCode === 'zh_CN' ? '查看' : 'View'}</Button> },
          ]}
          dataSource={runs}
          rowKey="id"
          loading={runsQuery.isLoading}
          pageSize={10}
        />
      </Card>
      <Card title={localeCode === 'zh_CN' ? '选中运行详情' : 'Selected Run Detail'}>
        {!selectedRun ? (
          <Empty description={localeCode === 'zh_CN' ? '暂无分析运行' : 'No analysis runs yet'} />
        ) : (
          <div className="kc-list-panel">
            <div className="kc-list-row">
              <div className="kc-list-row-meta">
                <Text strong>{selectedRun.title}</Text>
                <Space>
                  <StatusTag value={selectedRun.status} />
                  <StatusTag value={selectedRun.severity} />
                </Space>
              </div>
              <Paragraph style={{ marginBottom: 8 }}>{selectedRun.summary}</Paragraph>
              <Text type="tertiary">{localeCode === 'zh_CN' ? '分析模板' : 'Profile'}: {selectedRun.analysisProfileId || '-'}</Text>
            </div>
            <div className="kc-list-row">
              <Text strong>{localeCode === 'zh_CN' ? '假设' : 'Hypotheses'}</Text>
              {(selectedRun.hypotheses ?? []).length === 0 ? (
                <Text type="tertiary">-</Text>
              ) : (
                <div className="flex flex-col gap-2">
                  {(selectedRun.hypotheses ?? []).map((item) => (
                    <div key={item.id}>
                      <Text strong>{item.title}</Text>
                      <Text type="tertiary"> ({item.confidence}%)</Text>
                      <Paragraph style={{ marginBottom: 4 }}>{item.summary}</Paragraph>
                    </div>
                  ))}
                </div>
              )}
            </div>
            <div className="kc-list-row">
              <Text strong>{localeCode === 'zh_CN' ? '证据' : 'Evidence'}</Text>
              {(selectedRun.evidence ?? []).length === 0 ? (
                <Text type="tertiary">-</Text>
              ) : (
                <div className="flex flex-col gap-3">
                  <div className="flex flex-wrap items-center gap-2">
                    <Text type="tertiary">
                      {localeCode === 'zh_CN'
                        ? `当前显示 ${filteredEvidence.length} / ${(selectedRun.evidence ?? []).length} 条证据`
                        : `Showing ${filteredEvidence.length} / ${(selectedRun.evidence ?? []).length} evidence items`}
                    </Text>
                    <Button size="small" theme={evidenceKindFilter === 'all' ? 'solid' : 'light'} onClick={() => setEvidenceKindFilter('all')}>
                      {localeCode === 'zh_CN' ? '全部' : 'All'}
                    </Button>
                    {availableEvidenceKinds.map((item) => (
                      <Button key={item} size="small" theme={evidenceKindFilter === item ? 'solid' : 'light'} onClick={() => setEvidenceKindFilter(item)}>
                        {item}
                      </Button>
                    ))}
                    <Button size="small" theme={logsOnly ? 'solid' : 'light'} onClick={() => setLogsOnly((current) => !current)}>
                      {localeCode === 'zh_CN' ? '只看 logs evidence' : 'Logs Only'}
                    </Button>
                  </div>
                  <AdminTable
                    columns={[
                      { title: localeCode === 'zh_CN' ? '类型' : 'Kind', dataIndex: 'kind' },
                      { title: localeCode === 'zh_CN' ? '标题' : 'Title', dataIndex: 'title' },
                      { title: localeCode === 'zh_CN' ? '来源' : 'Source', dataIndex: 'attributes', render: (_: unknown, record: RootCauseEvidence) => evidenceAttribute(record, 'sourceId') || '-' },
                      { title: localeCode === 'zh_CN' ? '后端' : 'Backend', dataIndex: 'attributes', render: (_: unknown, record: RootCauseEvidence) => evidenceAttribute(record, 'backendType') || '-' },
                      { title: localeCode === 'zh_CN' ? '摘要' : 'Summary', dataIndex: 'summary', ellipsis: true },
                      { title: localeCode === 'zh_CN' ? '时间窗' : 'Sample Window', dataIndex: 'attributes', render: (_: unknown, record: RootCauseEvidence) => renderSampleWindow(record) },
                      { title: localeCode === 'zh_CN' ? '截断' : 'Truncated', dataIndex: 'attributes', render: (_: unknown, record: RootCauseEvidence) => String(Boolean(evidenceAttribute(record, 'truncated'))) },
                      { ...tableColumnPresets.status, title: localeCode === 'zh_CN' ? '严重度' : 'Severity', dataIndex: 'severity', render: (value: string) => value ? <StatusTag value={value} /> : '-' },
                    ]}
                    dataSource={filteredEvidence}
                    rowKey="id"
                    pagination={false}
                    expandedRowRender={(record: RootCauseEvidence) => (
                      <div className="flex flex-col gap-2 py-2">
                        <Text type="tertiary">{localeCode === 'zh_CN' ? '源 ID' : 'Source ID'}: {String(evidenceAttribute(record, 'sourceId') || '-')}</Text>
                        <Text type="tertiary">{localeCode === 'zh_CN' ? '后端类型' : 'Backend Type'}: {String(evidenceAttribute(record, 'backendType') || '-')}</Text>
                        <Text type="tertiary">{localeCode === 'zh_CN' ? '截断' : 'Truncated'}: {String(Boolean(evidenceAttribute(record, 'truncated')))}</Text>
                        <div>
                          <Text strong>{localeCode === 'zh_CN' ? 'Query Cost' : 'Query Cost'}</Text>
                          <pre className="mt-2 whitespace-pre-wrap rounded bg-[var(--semi-color-fill-0)] p-3 text-xs">{formatMapPreview(evidenceAttribute(record, 'queryCost'))}</pre>
                        </div>
                        <div>
                          <Text strong>{localeCode === 'zh_CN' ? 'Sample Window' : 'Sample Window'}</Text>
                          <pre className="mt-2 whitespace-pre-wrap rounded bg-[var(--semi-color-fill-0)] p-3 text-xs">{formatMapPreview(evidenceAttribute(record, 'sampleWindow'))}</pre>
                        </div>
                        {sampleRecords(record).length > 0 ? (
                          <div>
                            <Text strong>{localeCode === 'zh_CN' ? 'Sample Records' : 'Sample Records'}</Text>
                            <div className="mt-2">
                              <AdminTable
                                columns={[
                                  { ...tableColumnPresets.datetime, title: localeCode === 'zh_CN' ? '时间' : 'Timestamp', dataIndex: 'timestamp', render: (value: string) => value ? formatDateTime(value) : '-' },
                                  { ...tableColumnPresets.status, title: localeCode === 'zh_CN' ? '严重度' : 'Severity', dataIndex: 'severity', render: (value: string) => value ? <StatusTag value={value} /> : '-' },
                                  { title: localeCode === 'zh_CN' ? '服务' : 'Service', dataIndex: 'service' },
                                  { title: localeCode === 'zh_CN' ? '工作负载' : 'Workload', dataIndex: 'workload' },
                                  { title: localeCode === 'zh_CN' ? '命名空间' : 'Namespace', dataIndex: 'namespace' },
                                  { title: localeCode === 'zh_CN' ? '集群' : 'Cluster', dataIndex: 'clusterId' },
                                  { title: localeCode === 'zh_CN' ? '消息' : 'Message', dataIndex: 'message', ellipsis: true },
                                ]}
                                dataSource={sampleRecords(record)}
                                rowKey={(_item: SampleLogRecord, index?: number) => `${record.id}:sample:${index ?? 0}`}
                                pagination={false}
                                enableColumnSelection={false}
                              />
                            </div>
                          </div>
                        ) : null}
                      </div>
                    )}
                  />
                </div>
              )}
            </div>
            <div className="kc-list-row">
              <Text strong>{localeCode === 'zh_CN' ? '数据源快照' : 'Data Source Snapshot'}</Text>
              <pre className="mt-2 whitespace-pre-wrap rounded bg-[var(--semi-color-fill-0)] p-3 text-xs">{formatMapPreview(selectedRun.dataSourceSnapshot)}</pre>
            </div>
            <div className="kc-list-row">
              <Text strong>{localeCode === 'zh_CN' ? 'Playbook 结果' : 'Playbook Results'}</Text>
              <pre className="mt-2 whitespace-pre-wrap rounded bg-[var(--semi-color-fill-0)] p-3 text-xs">{formatMapPreview(selectedRun.playbookResults)}</pre>
            </div>
            <div className="kc-list-row">
              <Text strong>{localeCode === 'zh_CN' ? '建议动作' : 'Recommendations'}</Text>
              <div className="flex flex-wrap gap-2">
                {(selectedRun.recommendations ?? []).map((item) => (
                  <Tag key={item} color="blue">{item}</Tag>
                ))}
              </div>
            </div>
          </div>
        )}
      </Card>
    </div>
  )
}

export function PerformanceAnalysisPage() {
  const { localeCode } = useI18n()
  const summaryQuery = useQuery({
    queryKey: ['monitoring-summary', 'ai-performance'],
    queryFn: () => api.get<ApiResponse<AlertSummary>>('/monitoring/summary'),
  })
  const clustersQuery = useQuery({
    queryKey: ['clusters', 'ai-performance'],
    queryFn: () => api.get<ApiResponse<Cluster[]>>('/clusters'),
  })

  const summary = summaryQuery.data?.data
  const clusters = clustersQuery.data?.data ?? []
  const degradedClusters = clusters.filter((item) => item.health?.status && item.health.status !== 'healthy' && item.health.status !== 'ok')
  const signals = useMemo(() => buildPerformanceSignals(summary, clusters, localeCode), [summary, clusters, localeCode])

  return (
    <div className="kc-page">
      <PageHeader
        title={localeCode === 'zh_CN' ? '性能分析' : 'Performance Analysis'}
        description={localeCode === 'zh_CN' ? '基于告警吞吐、集群稳定性和通知覆盖度，给出当前平台运行性能判断。' : 'Assess current platform performance using alert pressure, fleet stability, and notification coverage.'}
      />
      <StatGrid
        items={[
          { label: localeCode === 'zh_CN' ? '健康集群' : 'Healthy Clusters', value: clusters.length - degradedClusters.length, icon: <IconPulse size="extra-large" /> },
          { label: localeCode === 'zh_CN' ? '异常集群' : 'Degraded Clusters', value: degradedClusters.length, icon: <IconPulse size="extra-large" /> },
          { label: localeCode === 'zh_CN' ? '活跃告警' : 'Firing Alerts', value: summary?.firingCount ?? 0, icon: <IconPulse size="extra-large" /> },
          { label: localeCode === 'zh_CN' ? '通知渠道' : 'Channels', value: summary?.channelCount ?? 0, icon: <IconPulse size="extra-large" /> },
        ]}
      />
      <Card title={localeCode === 'zh_CN' ? 'AI 性能结论' : 'AI Performance Signals'}>
        <div className="kc-list-panel">
          {signals.map((item) => (
            <div key={item.title} className="kc-list-row">
              <div className="kc-list-row-meta">
                <Text strong>{item.title}</Text>
                <StatusTag value={item.severity} />
              </div>
              <Paragraph style={{ marginBottom: 8 }}>{item.description}</Paragraph>
              <Text type="tertiary">{item.recommendation}</Text>
            </div>
          ))}
        </div>
      </Card>
      <Card title={localeCode === 'zh_CN' ? '异常集群' : 'Degraded Clusters'}>
        <AdminTable
          columns={[
            { title: localeCode === 'zh_CN' ? '集群' : 'Cluster', dataIndex: 'name' },
            { ...tableColumnPresets.status, title: localeCode === 'zh_CN' ? '状态' : 'Status', dataIndex: 'health', render: (health: Cluster['health']) => <StatusTag value={health?.status ?? 'unknown'} /> },
            { title: localeCode === 'zh_CN' ? '说明' : 'Message', dataIndex: 'health', render: (health: Cluster['health']) => health?.message || '-' },
            { ...tableColumnPresets.datetime, title: localeCode === 'zh_CN' ? '最近检查' : 'Last Checked', dataIndex: 'health', render: (health: Cluster['health']) => health?.lastChecked ? formatDateTime(health.lastChecked) : '-' },
          ]}
          dataSource={degradedClusters}
          rowKey="id"
          loading={clustersQuery.isLoading}
          pageSize={10}
        />
      </Card>
    </div>
  )
}

export function InspectionCenterPage() {
  const { localeCode } = useI18n()
  const queryClient = useQueryClient()
  const [modalVisible, setModalVisible] = useState(false)
  const [editing, setEditing] = useState<InspectionTask | null>(null)

  const clustersQuery = useQuery({
    queryKey: ['clusters', 'inspection-center'],
    queryFn: () => api.get<ApiResponse<Cluster[]>>('/clusters'),
  })
  const tasksQuery = useQuery({
    queryKey: ['copilot-inspection-tasks'],
    queryFn: () => api.get<ApiResponse<InspectionTask[]>>('/copilot/inspection-tasks'),
  })
  const runsQuery = useQuery({
    queryKey: ['copilot-inspection-runs'],
    queryFn: () => api.get<ApiResponse<InspectionRun[]>>('/copilot/inspection-runs'),
  })

  const createMutation = useMutation({
    mutationFn: (values: Record<string, unknown>) => api.post('/copilot/inspection-tasks', values),
    onSuccess: () => {
      Toast.success(localeCode === 'zh_CN' ? '巡检任务已创建' : 'Inspection task created')
      queryClient.invalidateQueries({ queryKey: ['copilot-inspection-tasks'] })
      setModalVisible(false)
    },
    onError: (err: Error) => Toast.error(err.message),
  })
  const updateMutation = useMutation({
    mutationFn: ({ id, values }: { id: string; values: Record<string, unknown> }) => api.put(`/copilot/inspection-tasks/${id}`, values),
    onSuccess: () => {
      Toast.success(localeCode === 'zh_CN' ? '巡检任务已更新' : 'Inspection task updated')
      queryClient.invalidateQueries({ queryKey: ['copilot-inspection-tasks'] })
      setModalVisible(false)
      setEditing(null)
    },
    onError: (err: Error) => Toast.error(err.message),
  })
  const executeMutation = useMutation({
    mutationFn: (taskId: string) => api.post(`/copilot/inspection-tasks/${taskId}/execute`),
    onSuccess: () => {
      Toast.success(localeCode === 'zh_CN' ? '巡检已触发' : 'Inspection triggered')
      queryClient.invalidateQueries({ queryKey: ['copilot-inspection-runs'] })
      queryClient.invalidateQueries({ queryKey: ['copilot-inspection-tasks'] })
    },
    onError: (err: Error) => Toast.error(err.message),
  })

  const taskNameMap = useMemo(
    () => Object.fromEntries((tasksQuery.data?.data ?? []).map((item) => [item.id, item.title])),
    [tasksQuery.data],
  )

  const taskColumns: ColumnProps<InspectionTask>[] = [
    { title: localeCode === 'zh_CN' ? '任务名称' : 'Task', dataIndex: 'title' },
    {
      title: localeCode === 'zh_CN' ? '范围' : 'Scope',
      dataIndex: 'scopeType',
      render: (_: string, record: InspectionTask) => {
        if (record.scopeType === 'namespace') {
          return `${record.clusterId || '-'} / ${record.namespace || '-'}`
        }
        if (record.scopeType === 'cluster') {
          return record.clusterId || '-'
        }
        return localeCode === 'zh_CN' ? '平台级' : 'Platform'
      },
    },
    {
      title: localeCode === 'zh_CN' ? '检查项' : 'Checks',
      dataIndex: 'checks',
      render: (checks: string[]) => (
        <div className="flex flex-wrap gap-1">
          {(checks ?? []).map((item) => <Tag key={item}>{item}</Tag>)}
        </div>
      ),
    },
    { title: localeCode === 'zh_CN' ? '间隔(分钟)' : 'Interval (min)', dataIndex: 'intervalMinutes' },
    { title: localeCode === 'zh_CN' ? '启用' : 'Enabled', dataIndex: 'enabled', render: (value: boolean) => <BooleanTag value={value} /> },
    { ...tableColumnPresets.datetime, title: localeCode === 'zh_CN' ? '最近运行' : 'Last Run', dataIndex: 'lastRunAt', render: (value: string) => value ? formatRelativeTime(value) : '-' },
    {
      ...tableColumnPresets.action,
      title: localeCode === 'zh_CN' ? '操作' : 'Actions',
      dataIndex: 'id',
      render: (_: unknown, record: InspectionTask) => (
        <Space>
          <Button size="small" theme="borderless" onClick={() => { setEditing(record); setModalVisible(true) }}>
            {localeCode === 'zh_CN' ? '编辑' : 'Edit'}
          </Button>
          <Button
            size="small"
            theme="borderless"
            icon={<IconPlay />}
            loading={executeMutation.isPending}
            onClick={() => executeMutation.mutate(record.id)}
          >
            {localeCode === 'zh_CN' ? '执行' : 'Run'}
          </Button>
        </Space>
      ),
    },
  ]

  const runColumns: ColumnProps<InspectionRun>[] = [
    { title: localeCode === 'zh_CN' ? '任务' : 'Task', dataIndex: 'taskId', render: (value: string) => taskNameMap[value] || value },
    { ...tableColumnPresets.status, title: localeCode === 'zh_CN' ? '状态' : 'Status', dataIndex: 'status', render: (value: string) => <StatusTag value={value} /> },
    { ...tableColumnPresets.status, title: localeCode === 'zh_CN' ? '严重度' : 'Severity', dataIndex: 'severity', render: (value: string) => <StatusTag value={value} /> },
    { title: localeCode === 'zh_CN' ? '发现项' : 'Findings', dataIndex: 'findings', render: (value: InspectionFinding[]) => value?.length ?? 0 },
    { title: localeCode === 'zh_CN' ? '摘要' : 'Summary', dataIndex: 'summary', ellipsis: true },
    { ...tableColumnPresets.datetime, title: localeCode === 'zh_CN' ? '开始时间' : 'Started At', dataIndex: 'startedAt', render: (value: string) => formatDateTime(value) },
    { ...tableColumnPresets.datetime, title: localeCode === 'zh_CN' ? '完成时间' : 'Completed At', dataIndex: 'completedAt', render: (value: string) => value ? formatDateTime(value) : '-' },
  ]

  return (
    <div className="kc-page">
      <PageHeader
        title={localeCode === 'zh_CN' ? '智能巡检' : 'Inspection Center'}
        description={localeCode === 'zh_CN' ? '维护巡检任务，按平台 / 集群 / 命名空间范围定时执行 AI 巡检。' : 'Manage inspection tasks and run AI inspections on platform, cluster, or namespace scopes.'}
        actions={
          <Button icon={<IconPlus />} theme="solid" onClick={() => { setEditing(null); setModalVisible(true) }}>
            {localeCode === 'zh_CN' ? '新建任务' : 'New Task'}
          </Button>
        }
      />
      <Card title={localeCode === 'zh_CN' ? '巡检任务' : 'Inspection Tasks'}>
        <AdminTable columns={taskColumns} dataSource={tasksQuery.data?.data ?? []} rowKey="id" loading={tasksQuery.isLoading} />
      </Card>
      <Card title={localeCode === 'zh_CN' ? '最近运行' : 'Recent Runs'}>
        <AdminTable columns={runColumns} dataSource={runsQuery.data?.data ?? []} rowKey="id" loading={runsQuery.isLoading} pageSize={10} />
      </Card>
      <Modal
        title={editing ? (localeCode === 'zh_CN' ? '编辑巡检任务' : 'Edit Inspection Task') : (localeCode === 'zh_CN' ? '新建巡检任务' : 'New Inspection Task')}
        visible={modalVisible}
        onCancel={() => { setModalVisible(false); setEditing(null) }}
        footer={null}
      >
        <Form
          initValues={editing ?? { enabled: true, scopeType: 'platform', intervalMinutes: 30, checks: INSPECTION_CHECK_OPTIONS.map((item) => item.value) }}
          onSubmit={(values) => {
            const payload = {
              ...values,
              checks: Array.isArray(values.checks) ? values.checks : [],
            }
            if (editing) {
              updateMutation.mutate({ id: editing.id, values: payload })
            } else {
              createMutation.mutate(payload)
            }
          }}
        >
          <Form.Input field="title" label={localeCode === 'zh_CN' ? '任务名称' : 'Title'} rules={[{ required: true, message: localeCode === 'zh_CN' ? '请输入任务名称' : 'Enter task title' }]} />
          <Form.Select
            field="scopeType"
            label={localeCode === 'zh_CN' ? '巡检范围' : 'Scope'}
            optionList={[
              { value: 'platform', label: localeCode === 'zh_CN' ? '平台级' : 'Platform' },
              { value: 'cluster', label: localeCode === 'zh_CN' ? '集群级' : 'Cluster' },
              { value: 'namespace', label: localeCode === 'zh_CN' ? '命名空间级' : 'Namespace' },
            ]}
          />
          <Form.Select
            field="clusterId"
            label={localeCode === 'zh_CN' ? '集群' : 'Cluster'}
            optionList={(clustersQuery.data?.data ?? []).map((item) => ({ value: item.id, label: item.name }))}
          />
          <Form.Input field="namespace" label={localeCode === 'zh_CN' ? '命名空间' : 'Namespace'} />
          <Form.Select
            field="checks"
            label={localeCode === 'zh_CN' ? '检查项' : 'Checks'}
            multiple
            optionList={INSPECTION_CHECK_OPTIONS}
          />
          <Form.InputNumber field="intervalMinutes" label={localeCode === 'zh_CN' ? '执行间隔(分钟)' : 'Interval (min)'} min={5} />
          <Form.Switch field="enabled" label={localeCode === 'zh_CN' ? '启用' : 'Enabled'} />
          <div className="kc-form-actions">
            <Button onClick={() => { setModalVisible(false); setEditing(null) }}>
              {localeCode === 'zh_CN' ? '取消' : 'Cancel'}
            </Button>
            <Button htmlType="submit" theme="solid" loading={createMutation.isPending || updateMutation.isPending}>
              {editing ? (localeCode === 'zh_CN' ? '更新' : 'Update') : (localeCode === 'zh_CN' ? '创建' : 'Create')}
            </Button>
          </div>
        </Form>
      </Modal>
    </div>
  )
}
