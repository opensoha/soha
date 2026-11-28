import { ManagementIconButton, ManagementState } from '@/components/management-list'
import {
  OverviewHeroCard,
  type OverviewMetricItem,
} from '@/components/overview-visuals'
import { ReadFeedback } from '@/components/request-feedback'
import { StatusTag } from '@/components/status-tag'
import { readableQueryData } from '@/utils/query-feedback'
import { formatDateTime } from '@/utils/time'
import {
  AlertOutlined,
  CheckCircleOutlined,
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

  return (
    <div className="soha-page soha-overview-page soha-monitoring-overview-page">
      <ReadFeedback label="告警摘要" query={summaryQuery} />
      <ReadFeedback label="Provider 状态" query={providersQuery} />
      <h1 className="soha-monitoring-overview-heading">可观测性总览</h1>
      {summaryQuery.isLoading ? (
        <ManagementState bordered={false} compact kind="loading" title="正在加载告警摘要" />
      ) : (
        <div className="soha-overview-bento-top">
          <OverviewHeroCard
          variant="monitoring"
          eyebrowIcon={<AlertOutlined />}
          eyebrowLabel={overviewStats[0].label}
          value={overviewStats[0].value}
          valueSuffix="全局黄金信号与告警态势"
          caption={
            summary?.firingCount
              ? '优先处置 Critical 与持续触发信号'
              : '全链路观测信号平稳，暂无未收敛活跃告警'
          }
          extraTopRight={
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
          }
          kpis={[
            {
              key: 'providers',
              label: '健康数据源',
              value: providersData
                ? `${healthyProviders.length}/${configuredProviders.length}`
                : '—',
            },
            {
              key: 'events',
              label: '近期事件',
              value: recentEvents.length,
            },
          ]}
          telemetryNodes={[
            {
              id: 'signal-hub',
              label: `FIRING · ${summary?.firingCount ?? 0}`,
              title: '全局告警评估中枢',
              metric: `活跃 ${summary?.firingCount ?? 0} · 已恢复 ${summary?.resolvedCount ?? 0}`,
              status: (summary?.firingCount ?? 0) > 0 ? 'warning' : 'healthy',
              path: '/monitoring-workbench/alerts',
            },
            {
              id: 'critical-signal',
              label: `CRITICAL · ${summary?.criticalCount ?? 0}`,
              title: '高优 Critical 告警流',
              metric:
                (summary?.criticalCount ?? 0) > 0
                  ? `${summary?.criticalCount} 条高危告警待响应`
                  : '无高危告警触发',
              status: (summary?.criticalCount ?? 0) > 0 ? 'critical' : 'healthy',
              path: '/monitoring-workbench/alerts',
            },
            {
              id: 'prom-metrics',
              label: `PROVIDERS · ${healthyProviders.length}/${configuredProviders.length}`,
              title: '可观测性 Provider 集群',
              metric:
                unhealthyProviders.length > 0
                  ? `${unhealthyProviders.length} 个数据源异常`
                  : '全部已启用数据源健康',
              status: unhealthyProviders.length > 0 ? 'warning' : 'healthy',
              path: '/monitoring-workbench/providers',
            },
            {
              id: 'event-bus',
              label: `EVENTS · ${recentEvents.length}`,
              title: '实时事件时间线',
              metric: `最近采集 ${recentEvents.length} 条平台事件`,
              status: 'primary',
              path: '/monitoring-workbench/events',
            },
          ]}
          embeddedPods={overviewStats.slice(1).map((item) => ({
            key: item.key,
            label: item.label,
            value: item.value,
            helper: item.helper,
            icon: item.icon,
            tone: item.tone,
            loading:
              item.key.endsWith('-providers') ? providersQuery.isLoading : summaryQuery.isLoading,
            to: item.key.endsWith('-providers')
              ? '/monitoring-workbench/providers'
              : '/monitoring-workbench/alerts',
          }))}
          segments={
            summary
              ? [
                  {
                    key: 'resolved',
                    label: '已恢复',
                    value: summary.resolvedCount,
                    tone: 'success',
                  },
                  {
                    key: 'warning',
                    label: 'Warning',
                    value: summary.warningCount,
                    tone: 'warning',
                  },
                  {
                    key: 'critical',
                    label: 'Critical',
                    value: summary.criticalCount,
                    tone: 'danger',
                  },
                  {
                    key: 'info',
                    label: 'Info',
                    value: summary.infoCount,
                    tone: 'neutral',
                  },
                ]
              : undefined
          }
          ctaLabel="进入告警处理"
          ctaTo="/monitoring-workbench/alerts"
          detailDrawerTitle="最近告警与事件明细"
          graphicDeck={[
            {
              key: 'obs-resolution-rings',
              kind: 'rings',
              title: '告警收敛与数据源光谱环',
              subtitle: '全栈指标 · 日志 · 链路',
              actionLabel: '数据源',
              actionTo: '/monitoring-workbench/providers',
              centerValue:
                summary && (summary.totalCount ?? 0) > 0
                  ? `${Math.round(((summary.resolvedCount ?? 0) / Math.max(1, summary.totalCount ?? 1)) * 100)}%`
                  : '100%',
              centerLabel: '已收敛',
              rings: [
                {
                  key: 'resolved',
                  label: '已恢复告警',
                  value: summary?.resolvedCount ?? 0,
                  ratio:
                    summary && (summary.totalCount ?? 0) > 0
                      ? (summary.resolvedCount ?? 0) / Math.max(1, summary.totalCount ?? 1)
                      : 1,
                  tone: 'success',
                  to: '/monitoring-workbench/alerts',
                },
                {
                  key: 'providers',
                  label: '健康数据源',
                  value: `${healthyProviders.length}/${configuredProviders.length}`,
                  ratio:
                    configuredProviders.length > 0
                      ? healthyProviders.length / configuredProviders.length
                      : 1,
                  tone: 'primary',
                  to: '/monitoring-workbench/providers',
                },
                {
                  key: 'warning',
                  label: 'Warning 活跃',
                  value: summary?.warningCount ?? 0,
                  ratio:
                    summary && (summary.totalCount ?? 0) > 0
                      ? (summary.warningCount ?? 0) / Math.max(1, summary.totalCount ?? 1)
                      : 0,
                  tone: 'warning',
                },
                {
                  key: 'critical',
                  label: 'Critical 高危',
                  value: summary?.criticalCount ?? 0,
                  ratio:
                    summary && (summary.totalCount ?? 0) > 0
                      ? (summary.criticalCount ?? 0) / Math.max(1, summary.totalCount ?? 1)
                      : 0,
                  tone: 'danger',
                },
              ],
            },
            {
              key: 'obs-signal-spectrum',
              kind: 'spectrum',
              title: '多级可观测信号脉冲波谱',
              subtitle: '分级密度与事件吞吐',
              actionLabel: '事件流',
              actionTo: '/monitoring-workbench/events',
              bars: [
                {
                  key: 'critical',
                  label: 'Critical',
                  value: summary?.criticalCount ?? 0,
                  tone: 'danger',
                  to: '/monitoring-workbench/alerts',
                },
                {
                  key: 'warning',
                  label: 'Warning',
                  value: summary?.warningCount ?? 0,
                  tone: 'warning',
                  to: '/monitoring-workbench/alerts',
                },
                {
                  key: 'info',
                  label: 'Info',
                  value: summary?.infoCount ?? 0,
                  tone: 'primary',
                },
                {
                  key: 'resolved',
                  label: '已恢复',
                  value: summary?.resolvedCount ?? 0,
                  tone: 'success',
                },
                {
                  key: 'events',
                  label: '实时事件',
                  value: recentEvents.length,
                  tone: 'primary',
                  to: '/monitoring-workbench/events',
                },
              ],
            },
            {
              key: 'obs-firing-radar',
              kind: 'radar',
              title: '实时告警与事件扫描雷达',
              subtitle: `活跃 ${summary?.firingCount ?? 0} 条`,
              actionLabel: '告警中心',
              actionTo: '/monitoring-workbench/alerts',
              emptyLabel: '零活跃告警 · 全域静默平稳',
              blips: [
                ...recentAlerts.slice(0, 2).map((alert) => ({
                  key: `alert-${alert.id}`,
                  label: alert.title || alert.id,
                  badge: alert.severity.toUpperCase(),
                  tone:
                    alert.severity.toLowerCase() === 'critical'
                      ? ('danger' as const)
                      : alert.status === 'resolved'
                        ? ('success' as const)
                        : ('warning' as const),
                  to: `/monitoring-workbench/alerts/${alert.id}`,
                })),
                ...recentEvents.slice(0, 1).map((ev) => ({
                  key: `ev-${ev.id}`,
                  label: ev.summary || ev.id,
                  badge: ev.category || 'EVENT',
                  tone: 'primary' as const,
                  to: '/monitoring-workbench/events',
                })),
              ],
            },
          ]}
        />
        </div>
      )}

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
