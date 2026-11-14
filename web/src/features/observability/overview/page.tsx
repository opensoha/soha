import { ManagementIconButton, ManagementState } from '@/components/management-list'
import {
  OverviewChip,
  OverviewMetricCard,
  OverviewSectionBar,
  type OverviewChipItem,
  type OverviewMetricItem,
} from '@/components/overview-visuals'
import { ReadFeedback } from '@/components/request-feedback'
import { StatusTag } from '@/components/status-tag'
import { readableQueryData } from '@/utils/query-feedback'
import { formatDateTime } from '@/utils/time'
import {
  AlertOutlined,
  CheckCircleOutlined,
  DatabaseOutlined,
  EyeOutlined,
  FireOutlined,
  ReloadOutlined,
  WarningOutlined,
} from '@ant-design/icons'
import { useQuery } from '@tanstack/react-query'
import { Button, Card, Typography } from 'antd'
import { useNavigate } from 'react-router-dom'
import { observabilityAlertQueries } from '../alerts/queries'
import { observabilityEventQueries } from '../events/queries'
import '../observability-pages.css'
import { observabilityProviderQueries } from '../provider-queries'
import { observabilityOverviewQueries } from './queries'

const { Text } = Typography

export function MonitoringPage() {
  const navigate = useNavigate()
  const summaryQuery = useQuery(observabilityOverviewQueries.summary())
  const alertsQuery = useQuery(observabilityAlertQueries.recent(8))
  const providersQuery = useQuery(observabilityProviderQueries.providers())
  const eventsQuery = useQuery(observabilityEventQueries.list())

  const summary = readableQueryData(summaryQuery)
  const recentAlerts = readableQueryData(alertsQuery) ?? []
  const providersData = readableQueryData(providersQuery)
  const providers = providersData ?? []
  const recentEvents = (readableQueryData(eventsQuery) ?? []).slice(0, 8)
  const configuredProviders = providers.filter((item) => item.configured)
  const healthyProviders = configuredProviders.filter((item) => item.runtimeStatus === 'healthy')
  const unhealthyProviders = configuredProviders.filter((item) => item.runtimeStatus !== 'healthy')

  const overviewStats = [
    {
      key: 'firing',
      label: '活跃告警',
      helper: '当前仍需处理的告警事件',
      value: summary?.firingCount ?? '不可用',
      icon: <AlertOutlined />,
      tone: (summary?.firingCount ?? 0) > 0 ? 'warning' : 'default',
    },
    {
      key: 'critical',
      label: '严重告警',
      helper: 'Critical 优先级信号',
      value: summary?.criticalCount ?? '不可用',
      icon: <FireOutlined />,
      tone: (summary?.criticalCount ?? 0) > 0 ? 'danger' : 'default',
    },
    {
      key: 'healthy-providers',
      label: '健康 Provider',
      helper: !providersData
        ? 'Provider 状态暂不可用'
        : `已配置 ${configuredProviders.length} 个 Provider`,
      value: providersData ? healthyProviders.length : '不可用',
      icon: <CheckCircleOutlined />,
      tone: healthyProviders.length > 0 ? 'success' : 'default',
    },
    {
      key: 'unhealthy-providers',
      label: '异常 Provider',
      helper: '已配置但未通过运行态校验',
      value: providersData ? unhealthyProviders.length : '不可用',
      icon: <WarningOutlined />,
      tone: unhealthyProviders.length > 0 ? 'warning' : 'default',
    },
  ] satisfies OverviewMetricItem[]

  const alertChips = [
    { key: 'total', label: '总数', value: summary?.totalCount ?? '不可用', tone: 'default' },
    { key: 'firing', label: '活跃', value: summary?.firingCount ?? '不可用', tone: 'warning' },
    {
      key: 'resolved',
      label: '已恢复',
      value: summary?.resolvedCount ?? '不可用',
      tone: 'success',
    },
    {
      key: 'critical',
      label: 'Critical',
      value: summary?.criticalCount ?? '不可用',
      tone: 'danger',
    },
    {
      key: 'warning',
      label: 'Warning',
      value: summary?.warningCount ?? '不可用',
      tone: 'warning',
    },
    { key: 'info', label: 'Info', value: summary?.infoCount ?? '不可用', tone: 'default' },
  ] satisfies OverviewChipItem[]

  const sourceStats = [
    {
      key: 'total',
      label: '已发现',
      value: providersData ? providers.length : '不可用',
      helper: '当前 Provider 能力',
      icon: <DatabaseOutlined />,
      tone: 'default',
    },
    {
      key: 'configured',
      label: '已配置',
      value: providersData ? configuredProviders.length : '不可用',
      helper: '存在启用的数据源',
      tone: 'success',
    },
    {
      key: 'healthy',
      label: '健康',
      value: providersData ? healthyProviders.length : '不可用',
      helper: '最近验证成功',
      tone: 'success',
    },
    {
      key: 'unconfigured',
      label: '未配置',
      value: providersData ? providers.filter((item) => !item.configured).length : '不可用',
      helper: '仅声明适配能力',
      tone: 'default',
    },
  ] satisfies OverviewChipItem[]

  return (
    <div className="soha-page soha-overview-page soha-monitoring-overview-page">
      <h1 className="soha-monitoring-overview-heading">可观测性总览</h1>
      <div style={{ display: 'flex', justifyContent: 'flex-end' }}>
        <ManagementIconButton
          aria-label="刷新观测总览"
          tooltip="刷新观测总览"
          icon={
            <ReloadOutlined
              spin={
                summaryQuery.isFetching ||
                providersQuery.isFetching ||
                alertsQuery.isFetching ||
                eventsQuery.isFetching
              }
            />
          }
          onClick={() => {
            void summaryQuery.refetch()
            void providersQuery.refetch()
            void alertsQuery.refetch()
            void eventsQuery.refetch()
          }}
        />
      </div>
      <div className="soha-overview-metric-grid">
        {overviewStats.map((item) => (
          <OverviewMetricCard
            key={item.key}
            label={item.label}
            value={item.value}
            helper={item.helper}
            icon={item.icon}
            tone={item.tone}
            loading={
              item.key.endsWith('-providers') ? providersQuery.isLoading : summaryQuery.isLoading
            }
          />
        ))}
      </div>

      <div className="soha-overview-summary-grid">
        <Card
          className="soha-overview-panel-card"
          title="告警态势"
          extra={
            summary?.lastReceivedAt ? (
              <Text type="secondary" className="text-xs">
                最近接收: {formatDateTime(summary?.lastReceivedAt)}
              </Text>
            ) : null
          }
        >
          <ReadFeedback label="告警摘要" query={summaryQuery} />
          {summaryQuery.isLoading ? (
            <ManagementState bordered={false} compact kind="loading" title="正在加载告警摘要" />
          ) : summary ? (
            <div className="soha-overview-alert-stack">
              <OverviewSectionBar
                title="告警分布"
                description={
                  summary.firingCount > 0
                    ? '当前仍有活跃告警，优先处置 Critical 和 Warning。'
                    : '当前没有活跃告警，继续关注规则、通知和值班链路。'
                }
                extra={
                  <Button
                    type="text"
                    icon={<EyeOutlined />}
                    onClick={() => navigate('/monitoring-workbench/alerts')}
                  >
                    查看活跃告警
                  </Button>
                }
              />
              <div className="soha-overview-chip-grid">
                {alertChips.map((item) => (
                  <OverviewChip
                    key={item.key}
                    label={item.label}
                    value={item.value}
                    tone={item.tone}
                  />
                ))}
              </div>
            </div>
          ) : summaryQuery.isError ? null : (
            <ManagementState bordered={false} compact description="暂无告警摘要" />
          )}
        </Card>

        <Card
          className="soha-overview-panel-card"
          title="Provider 运行态"
          extra={
            <Button type="text" onClick={() => navigate('/monitoring-workbench/providers')}>
              查看 Provider
            </Button>
          }
        >
          <ReadFeedback label="Provider 状态" query={providersQuery} />
          {providersQuery.isLoading ? (
            <ManagementState bordered={false} compact kind="loading" title="正在加载 Provider" />
          ) : providersData ? (
            <div className="soha-monitoring-operation-grid">
              {sourceStats.map((item) => (
                <OverviewChip
                  key={item.key}
                  label={item.label}
                  value={item.value}
                  helper={item.helper}
                  icon={item.icon}
                  tone={item.tone}
                />
              ))}
            </div>
          ) : null}
        </Card>
      </div>

      <div className="soha-monitoring-recent-grid">
        <Card
          className="soha-overview-runtime-card"
          title="最近告警"
          extra={
            <ManagementIconButton
              aria-label="进入告警处理"
              icon={<EyeOutlined />}
              size="small"
              tooltip="进入告警处理"
              onClick={() => navigate('/monitoring-workbench/alerts')}
            />
          }
        >
          <ReadFeedback label="最近告警" query={alertsQuery} />
          {alertsQuery.isLoading ? (
            <div className="soha-monitoring-alert-list">
              {[0, 1, 2].map((item) => (
                <Card key={item} loading size="small" />
              ))}
            </div>
          ) : !readableQueryData(alertsQuery) ? null : recentAlerts.length === 0 ? (
            <ManagementState bordered={false} compact description="暂无最近告警" />
          ) : (
            <div className="soha-monitoring-alert-list">
              {recentAlerts.map((item) => (
                <div
                  key={item.id}
                  className={`soha-overview-attention-row${item.status === 'firing' ? ' is-active' : ''}`}
                >
                  <div className="soha-overview-attention-main">
                    <div className="soha-monitoring-alert-title-row">
                      <Text strong>{item.title || item.id}</Text>
                      <StatusTag value={item.severity} />
                      <StatusTag value={item.status} />
                    </div>
                    <div className="soha-overview-inline-caption">{item.summary || '-'}</div>
                  </div>
                  <div className="soha-overview-attention-meta">
                    <span>
                      {[item.clusterId, item.namespace].filter(Boolean).join(' / ') || '-'}
                    </span>
                    <span>{formatDateTime(item.lastSeenAt || item.startsAt)}</span>
                    <Button
                      type="link"
                      onClick={() => navigate(`/monitoring-workbench/alerts/${item.id}`)}
                    >
                      详情
                    </Button>
                  </div>
                </div>
              ))}
            </div>
          )}
        </Card>

        <Card
          className="soha-overview-runtime-card"
          title="近期事件"
          extra={
            <ManagementIconButton
              aria-label="进入事件流"
              icon={<EyeOutlined />}
              size="small"
              tooltip="进入事件流"
              onClick={() => navigate('/monitoring-workbench/events')}
            />
          }
        >
          <ReadFeedback label="近期事件" query={eventsQuery} />
          {eventsQuery.isLoading ? (
            <ManagementState bordered={false} compact kind="loading" title="正在加载事件" />
          ) : !readableQueryData(eventsQuery) ? null : recentEvents.length === 0 ? (
            <ManagementState bordered={false} compact description="暂无近期事件" />
          ) : (
            <div className="soha-monitoring-alert-list">
              {recentEvents.map((item) => (
                <div key={item.id} className="soha-overview-attention-row">
                  <div className="soha-overview-attention-main">
                    <div className="soha-monitoring-alert-title-row">
                      <Text strong>{item.summary || item.id}</Text>
                      {item.severity ? <StatusTag value={item.severity} /> : null}
                    </div>
                    <div className="soha-overview-inline-caption">
                      {item.source} / {item.category}
                    </div>
                  </div>
                  <div className="soha-overview-attention-meta">
                    <span>
                      {[item.clusterId, item.namespace].filter(Boolean).join(' / ') || '-'}
                    </span>
                    <span>{formatDateTime(item.occurredAt)}</span>
                  </div>
                </div>
              ))}
            </div>
          )}
        </Card>
      </div>
    </div>
  )
}
