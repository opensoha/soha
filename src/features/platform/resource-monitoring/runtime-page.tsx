import { localeText, useI18n } from '@/i18n'
import { AdminTable } from '@/components/admin-table'
import { KubernetesResourceTable } from '@/features/platform/shared/kubernetes-resource-table'
import { KubernetesResourceActionsMenu } from '@/features/platform/shared/kubernetes-resource-actions-menu'
import { NodeStateTags } from '@/features/platform/shared/node-state-tags'
import { MonitoringUsageMeter } from './usage-meter'
import { ManagementRefreshButton, ManagementState } from '@/components/management-list'
import { ReadFeedback } from '@/components/request-feedback'
import { StatusTag } from '@/components/status-tag'
import { formatMetricValue } from '@/components/resource-metrics-format'
import { hasPermission, usePermissionSnapshot } from '@/features/auth'
import { usePlatformScopeStore } from '@/stores/platform-scope-store'
import { useAuthStore } from '@/stores/auth-store'
import { readableQueryData } from '@/utils/query-feedback'
import { formatDateTime } from '@/utils/time'
import { useDocumentHidden } from '@/utils/use-document-hidden'
import { useQueries, type UseQueryResult } from '@tanstack/react-query'
import {
  Alert,
  Button,
  Card,
  DatePicker,
  Drawer,
  Empty,
  Input,
  Tabs,
  Tooltip,
  Select,
  Space,
  Typography,
  type TableColumnsType,
} from 'antd'
import dayjs from 'dayjs'
import { useLayoutEffect, useMemo, useRef, useState } from 'react'
import { Link, useLocation, useNavigate, useSearchParams } from 'react-router-dom'
import { MonitoringChart } from './chart'
import { MonitoringDashboard } from './dashboard'
import {
  latestMetric,
  monitoringNodeLimit,
  monitoringPermission,
  monitoringRatio,
  monitoringViews,
  nodeMetricScore,
  alignedMetricDenominator,
  qualityLabels,
  qualityStatus,
  reasonLabels,
} from './model'
import { monitoringQueries } from './queries'
import type {
  MonitoringNode,
  MonitoringPanel,
  MonitoringViewName,
  MonitoringQuery,
  MonitoringView,
} from './types'
import './styles.css'
const { Text } = Typography

function Quality({ value }: { value: string }) {
  const { t } = useI18n()
  return (
    <StatusTag
      value={qualityStatus[value] || 'unknown'}
      label={t(`resourceMonitoring.quality.${value}`, qualityLabels[value] || value)}
    />
  )
}
function valueText(value: number | null | undefined, unit: string) {
  return value == null ? '—' : formatMetricValue(value, unit)
}
function Ratio({ value }: { value: number | null }) {
  const { localeCode } = useI18n()
  return value == null ? (
    <Text type="secondary">
      {localeText(localeCode, '采样或分母不足', 'Samples or denominator unavailable')}
    </Text>
  ) : (
    <Text type={value > 100 ? 'danger' : undefined}>
      {value.toFixed(1)}%{value > 100 ? localeText(localeCode, '· 超配', ' · Overcommitted') : ''}
    </Text>
  )
}

function Trend({
  panels,
  defaultKey,
  names,
  title,
}: {
  panels: MonitoringPanel[]
  defaultKey: string
  names: string[]
  title: string
}) {
  const { localeCode, t } = useI18n()
  const [chosen, setChosen] = useState(defaultKey)
  const [samplesOpen, setSamplesOpen] = useState(false)
  const panel =
    panels.find((panel) => panel.key === chosen) ||
    panels.find((panel) => panel.key === defaultKey) ||
    panels[0]
  const rows = useMemo(
    () =>
      panel?.series
        .filter((series) => names.includes(series.nodeRef.name))
        .flatMap((series) =>
          series.points.map((point) => ({
            id: `${series.nodeRef.uid}:${point.timestamp}`,
            node: series.nodeRef.name,
            ...point,
          })),
        ) ?? [],
    [panel, names],
  )
  return (
    <Card className="soha-detail-card" title={title}>
      {panel && (
        <Space wrap className="soha-monitoring-trend-controls">
          <Quality value={panel.quality} />
          <Select
            aria-label={`${title} ${localeText(localeCode, '指标', 'metric')}`}
            style={{ width: 240, maxWidth: '100%' }}
            value={panel.key}
            onChange={setChosen}
            options={panels.map((panel) => ({ value: panel.key, label: panel.label }))}
          />
          <Button onClick={() => setSamplesOpen(true)}>
            {localeText(localeCode, '采样数据', 'Samples')}
          </Button>
        </Space>
      )}
      {panel && (
        <Text type="secondary">
          {t(
            `resourceMonitoring.reason.${panel.reasonCode}`,
            reasonLabels[panel.reasonCode || ''] || '',
          ) ||
            panel.reasonCode ||
            localeText(
              localeCode,
              '仅展示已确认节点身份的序列',
              'Only series with verified node identities are shown',
            )}
          {panel.estimated
            ? localeText(localeCode, '· 窗口增量为估算值', ' · Window increase is an estimate')
            : ''}
          {panel.windowSeconds
            ? ` · ${localeText(localeCode, '窗口', 'Window')} ${panel.windowSeconds}s`
            : ''}
        </Text>
      )}
      {panel?.series.some(
        (series) => names.includes(series.nodeRef.name) && series.latest != null,
      ) ? (
        <MonitoringChart panel={panel} names={names} />
      ) : (
        <Empty
          description={
            panel
              ? t(`resourceMonitoring.quality.${panel.quality}`, qualityLabels[panel.quality])
              : localeText(localeCode, '没有时序指标', 'No time series')
          }
        />
      )}
      <Drawer
        title={`${panel?.label || title} · ${localeText(localeCode, '采样数据', 'Samples')}`}
        size="min(720px, 100vw)"
        open={samplesOpen}
        onClose={() => setSamplesOpen(false)}
      >
        <Text type="secondary">
          {localeText(
            localeCode,
            '空值表示缺失，未补为 0；时间为查询采样时间。最多展示当前选中节点。',
            'Null means missing; no zero fill is applied. Times are query sample times. Only selected nodes are shown.',
          )}
        </Text>
        <AdminTable
          rowKey="id"
          columns={[
            { title: localeText(localeCode, '节点', 'Node'), dataIndex: 'node' },
            {
              title: localeText(localeCode, '时间', 'Time'),
              dataIndex: 'timestamp',
              render: (value: string) => formatDateTime(value),
            },
            {
              title: panel?.unit || localeText(localeCode, '值', 'Value'),
              dataIndex: 'value',
              render: (value: number | null) => valueText(value, panel?.unit || 'count'),
            },
          ]}
          dataSource={rows}
          pagination={false}
          virtual
          scroll={{ x: 640, y: 520 }}
          enableColumnSelection={false}
        />
      </Drawer>
    </Card>
  )
}

export function ResourceMonitoringRuntimePage() {
  const { localeCode, t } = useI18n()
  const [params, setParams] = useSearchParams()
  const navigate = useNavigate()
  const location = useLocation()
  const { clusterId: scopedClusterId, setClusterId } = usePlatformScopeStore()
  const lastScope = useRef(scopedClusterId)
  const requestedClusterId = params.get('clusterId')
  useLayoutEffect(() => {
    if (scopedClusterId !== lastScope.current) {
      lastScope.current = scopedClusterId
      setParams(
        (previous) => {
          const next = new URLSearchParams(previous)
          if (scopedClusterId) next.set('clusterId', scopedClusterId)
          else next.delete('clusterId')
          next.delete('node')
          return next
        },
        { replace: true },
      )
      return
    }
    if (requestedClusterId && requestedClusterId !== scopedClusterId) {
      lastScope.current = requestedClusterId
      setClusterId(requestedClusterId)
    }
  }, [scopedClusterId, requestedClusterId, setClusterId, setParams])
  const clusterId = requestedClusterId || scopedClusterId || ''
  const principal = useAuthStore((state) => state.user?.userId || '')
  const permissionQuery = usePermissionSnapshot()
  const snapshot = readableQueryData(permissionQuery)?.data
  const canReadView = (view: MonitoringViewName) =>
    hasPermission(snapshot, 'platform.nodes.view') &&
    hasPermission(snapshot, monitoringPermission(view))
  const canRead = monitoringViews.some((item) => canReadView(item.key))
  const permissionsSignature = `${(snapshot?.permissionKeys ?? []).slice().sort().join(',')}@${permissionQuery.dataUpdatedAt}`
  const input: MonitoringQuery = {
    view: 'overview',
    node: params.get('node') || undefined,
    from: params.get('from') || undefined,
    to: params.get('to') || undefined,
    rangeMinutes: Number(params.get('rangeMinutes') || 60),
    stepSeconds: 60,
  }
  const [autoRefresh, setAutoRefresh] = useState(
    () => localStorage.getItem('soha-resource-monitoring-auto') !== '0',
  )
  const [coverageOpen, setCoverageOpen] = useState(false)
  const chartScope = JSON.stringify([principal, clusterId, input.node])
  const [highlighted, setHighlighted] = useState({ scope: chartScope, names: [] as string[] })
  const selectNames = (names: string[]) => setHighlighted({ scope: chartScope, names })
  const hidden = useDocumentHidden()
  const queries = useQueries({
    queries: monitoringViews.map(({ key }) => ({
      ...monitoringQueries.view(principal, permissionsSignature, clusterId, {
        ...input,
        view: key,
      }),
      enabled: Boolean(clusterId) && canReadView(key),
      refetchInterval: autoRefresh && !hidden && !input.to ? 30000 : false,
      refetchIntervalInBackground: false,
    })),
  })
  const views = monitoringViews.flatMap((item, index) => {
    if (!canReadView(item.key)) return []
    const readable = readableQueryData(queries[index]!)
    return [
      {
        ...item,
        query: queries[index]!,
        data: readable?.clusterId === clusterId ? readable : undefined,
      },
    ]
  })
  const data = views.find((item) => item.data)?.data
  const nodes = data?.nodes ?? []
  const names = (highlighted.scope === chartScope ? highlighted.names : []).filter((name) =>
    nodes.some((node) => node.nodeRef.name === name),
  )
  const activeNames = names.length
    ? names
    : nodes.slice(0, monitoringNodeLimit).map((node) => node.nodeRef.name)
  const panels = [
    ...new Map(
      views.flatMap((item) => item.data?.panels ?? []).map((panel) => [panel.key, panel]),
    ).values(),
  ]
  const requestedView =
    location.hash.replace('#monitoring-', '') || location.pathname.split('/').pop()
  const activeView = views.find((view) => view.key === requestedView)?.key ?? views[0]?.key
  const openView = (key: string) =>
    navigate(`/resource-monitoring${location.search}#monitoring-${key}`, { replace: true })
  const fetching = views.some((item) => item.query.isFetching)
  const failed = views.some((item) => item.query.isError)
  const paused = !autoRefresh || hidden || Boolean(input.to) || !clusterId || !canRead
  const refreshState = fetching ? 'refreshing' : failed ? 'failed' : paused ? 'paused' : 'live'
  const refreshLabel = fetching
    ? localeText(localeCode, '刷新中', 'Refreshing')
    : failed
      ? localeText(localeCode, '刷新失败，请重试', 'Refresh failed; retry')
      : input.to
        ? localeText(
            localeCode,
            '历史窗口，自动刷新已暂停',
            'Historical window; auto-refresh paused',
          )
        : hidden
          ? localeText(localeCode, '页面隐藏，自动刷新已暂停', 'Hidden page; auto-refresh paused')
          : !autoRefresh
            ? localeText(localeCode, '自动刷新已关闭', 'Auto-refresh off')
            : localeText(
                localeCode,
                '自动刷新正常，每 30 秒',
                'Auto-refresh active, every 30 seconds',
              )
  const update = (values: Record<string, string | undefined>) =>
    setParams(
      (previous) => {
        const next = new URLSearchParams(previous)
        Object.entries(values).forEach(([key, value]) => {
          if (value) next.set(key, value)
          else next.delete(key)
        })
        return next
      },
      { replace: true },
    )
  return (
    <div className="soha-page soha-monitoring-page">
      <h1 className="soha-monitoring-screen-reader">
        {localeText(localeCode, '资源监控', 'Resource monitoring')}
      </h1>
      <ReadFeedback query={permissionQuery} label={localeText(localeCode, '权限', 'Permissions')} />
      {!clusterId ? (
        <ManagementState
          kind="select-scope"
          title={localeText(
            localeCode,
            '选择集群查看资源监控',
            'Select a cluster to view resource monitoring',
          )}
        />
      ) : !canRead && !permissionQuery.isLoading && !permissionQuery.isError ? (
        <ManagementState
          kind="error"
          title={localeText(
            localeCode,
            '需要资源监控及集群级节点查看权限',
            'Resource monitoring and cluster-wide node access are required',
          )}
        />
      ) : null}
      <Card className="soha-detail-card" styles={{ body: { padding: 16 } }}>
        <div className="soha-monitoring-toolbar">
          <Select
            aria-label={localeText(localeCode, '监控节点', 'Monitored node')}
            showSearch={{ optionFilterProp: 'label' }}
            value={input.node || ''}
            onChange={(value) => {
              selectNames([])
              update({ node: value || undefined })
            }}
            options={[
              { value: '', label: localeText(localeCode, '全部节点', 'All nodes') },
              ...(data?.nodeOptions ?? nodes.map((node) => node.nodeRef)).map((node) => ({
                value: node.name,
                label: node.name,
              })),
            ]}
          />
          <Select
            aria-label={localeText(localeCode, '时间范围', 'Time range')}
            value={input.from ? 'custom' : String(input.rangeMinutes)}
            onChange={(value) => {
              if (value === 'custom') {
                const end = dayjs()
                update({
                  from: end.subtract(input.rangeMinutes || 60, 'minute').toISOString(),
                  to: end.toISOString(),
                })
              } else update({ rangeMinutes: value, from: undefined, to: undefined })
            }}
            options={[
              { value: '15', label: localeText(localeCode, '最近 15 分钟', 'Last 15 minutes') },
              { value: '60', label: localeText(localeCode, '最近 1 小时', 'Last hour') },
              { value: '360', label: localeText(localeCode, '最近 6 小时', 'Last 6 hours') },
              { value: '1440', label: localeText(localeCode, '最近 24 小时', 'Last 24 hours') },
              { value: 'custom', label: localeText(localeCode, '自定义时间', 'Custom time') },
            ]}
          />
          {input.from && (
            <DatePicker.RangePicker
              aria-label={localeText(
                localeCode,
                '固定历史时间窗口',
                'Fixed historical time window',
              )}
              showTime
              value={input.from && input.to ? [dayjs(input.from), dayjs(input.to)] : null}
              onChange={(dates) =>
                update({ from: dates?.[0]?.toISOString(), to: dates?.[1]?.toISOString() })
              }
            />
          )}
          <div className="soha-monitoring-toolbar-actions">
            <Tooltip title={refreshLabel} trigger={['hover', 'focus']}>
              <Button
                type="text"
                size="small"
                role="switch"
                aria-checked={autoRefresh && !input.to}
                aria-label={`${localeText(localeCode, '自动刷新', 'Auto-refresh')}：${refreshLabel}`}
                disabled={Boolean(input.to) || !clusterId || !canRead}
                data-refresh-state={refreshState}
                className="soha-monitoring-refresh-dot"
                onClick={() => {
                  setAutoRefresh(!autoRefresh)
                  localStorage.setItem('soha-resource-monitoring-auto', autoRefresh ? '0' : '1')
                }}
              >
                <span aria-hidden />
              </Button>
            </Tooltip>
            <ManagementRefreshButton
              aria-label={localeText(localeCode, '刷新资源监控', 'Refresh resource monitoring')}
              tooltip={localeText(localeCode, '刷新资源监控', 'Refresh resource monitoring')}
              loading={views.some((item) => item.query.isFetching)}
              disabled={!clusterId || !canRead}
              onClick={() => void Promise.all(views.map((item) => item.query.refetch()))}
            />
            <Button disabled={!data} onClick={() => setCoverageOpen(true)}>
              {localeText(localeCode, '来源与覆盖', 'Sources and coverage')}
            </Button>
          </div>
        </div>
        {data && (
          <div className="soha-monitoring-caption">
            <Text type="secondary">
              {localeText(localeCode, '已加载', 'Loaded ')}
              {nodes.length}{' '}
              {localeText(localeCode, '节点 · 当前容量采样', ' nodes · Current capacity observed ')}{' '}
              {data.snapshotObservedAt
                ? formatDateTime(data.snapshotObservedAt)
                : localeText(localeCode, '未提供', 'Unavailable')}{' '}
              {localeText(localeCode, '· 历史步长', ' · History step ')}
              {data.stepSeconds}s
            </Text>
            <Quality value={data.snapshotQuality} />
          </div>
        )}
      </Card>
      <Tabs
        className="soha-monitoring-tabs"
        activeKey={activeView}
        onChange={openView}
        items={[...views]
          .sort((a, b) => {
            const order = ['overview', 'cpu', 'memory', 'pods', 'conntrack']
            return order.indexOf(a.key) - order.indexOf(b.key)
          })
          .map((item) => ({
            key: item.key,
            label: item[localeCode === 'zh_CN' ? 'label' : 'englishLabel'],
            children: (
              <>
                {item.key === 'overview' ? (
                  <MonitoringDashboard views={views} historical={Boolean(input.to)} />
                ) : (
                  data && (
                    <div className="soha-monitoring-compare-controls">
                      <Select
                        aria-label={localeText(localeCode, '曲线节点', 'Chart nodes')}
                        mode="multiple"
                        maxCount={monitoringNodeLimit}
                        maxTagCount="responsive"
                        value={activeNames}
                        onChange={selectNames}
                        options={nodes.map((node) => ({
                          value: node.nodeRef.name,
                          label: node.nodeRef.name,
                        }))}
                      />
                      <Text type="secondary">
                        {localeText(
                          localeCode,
                          `曲线最多 ${monitoringNodeLimit} 个节点，节点表展示当前范围全部结果`,
                          `Compare up to ${monitoringNodeLimit} chart nodes; the table shows the full loaded scope`,
                        )}
                      </Text>
                    </div>
                  )
                )}
                <MonitoringSection
                  view={item.key}
                  query={item.query}
                  data={item.data}
                  clusterId={clusterId}
                  params={params}
                  activeNames={activeNames}
                  snapshot={snapshot}
                  historical={Boolean(input.to)}
                  onFocus={(name, view) => {
                    selectNames([name])
                    openView(view)
                  }}
                />
              </>
            ),
          }))}
      />
      <Drawer
        title={localeText(localeCode, '数据来源与覆盖', 'Data sources and coverage')}
        size="min(620px, 100vw)"
        open={coverageOpen}
        onClose={() => setCoverageOpen(false)}
      >
        <Typography.Paragraph>
          {localeText(
            localeCode,
            '主机 CPU 为非 idle cores；Pod CPU 为带 UID 的容器累计 CPU 速率。主机内存为 total - available，Pod 内存为 working set，两者不相加。当前 requests/limits 来自 Kubernetes Spec，含初始化、sidecar 与适用 overhead；未设置上限保留为空。',
            'Host CPU is non-idle cores; Pod CPU is the rate of container CPU counters identified by UID. Host memory is total minus available; Pod memory is working set. They are not added together. Current requests/limits come from Kubernetes Spec, including init containers, sidecars and applicable overhead. Unset limits remain null.',
          )}
        </Typography.Paragraph>
        <Typography.Paragraph>
          {localeText(
            localeCode,
            '连接跟踪依赖 Linux node_exporter。drop / early_drop / insert_failed 对每条稳定序列先做重置感知 rate / increase，再去重与聚合；需要 5 分钟窗口内至少 5 个样本，且最新样本小于 120 秒。窗口增量是估算。',
            'Conntrack requires Linux node_exporter. Reset-aware rate/increase is calculated per stable drop / early_drop / insert_failed series before deduplication and aggregation. At least five samples in a five-minute window and a latest sample younger than 120 seconds are required. Window increases are estimates.',
          )}
        </Typography.Paragraph>
        <Typography.Paragraph>
          {localeText(
            localeCode,
            '所有查询保留显式集群标签；无法唯一关联当前节点的序列不显示。历史曲线不会用当前快照补齐，数据源、角色和节点变化均重新读取。',
            'All queries retain an explicit cluster label. Series that cannot be mapped uniquely to a current node are excluded. Current snapshots never fill history. Source, role and node changes trigger new reads.',
          )}
        </Typography.Paragraph>
        {panels.map((panel) => (
          <div className="soha-monitoring-coverage" key={panel.key}>
            <Space wrap>
              <Text strong>{panel.label}</Text>
              <Quality value={panel.quality} />
            </Space>
            <Text type="secondary">
              {t(
                `resourceMonitoring.reason.${panel.reasonCode}`,
                reasonLabels[panel.reasonCode || ''] || '',
              ) ||
                panel.reasonCode ||
                localeText(localeCode, '已取得样本', 'Samples received')}
            </Text>
            <Text type="secondary">
              {panel.series.filter((series) => series.latest != null).length} /{' '}
              {panel.series.length}{' '}
              {localeText(localeCode, '个已加载节点有数值', ' loaded nodes have values')}
            </Text>
          </div>
        ))}
      </Drawer>
    </div>
  )
}

function MonitoringSection({
  view,
  query,
  data,
  clusterId,
  params,
  activeNames,
  snapshot,
  historical,
  onFocus,
}: {
  view: MonitoringViewName
  query: UseQueryResult<MonitoringView>
  data: MonitoringView | undefined
  clusterId: string
  params: URLSearchParams
  activeNames: string[]
  snapshot: Parameters<typeof hasPermission>[0]
  historical: boolean
  onFocus: (name: string, view: MonitoringViewName) => void
}) {
  const { localeCode } = useI18n()
  const [keyword, setKeyword] = useState('')

  const nodes = data?.nodes ?? []
  const ranked = nodes
    .slice()
    .sort(
      (a, b) =>
        (nodeMetricScore(data, b, view, historical) ?? -1) -
        (nodeMetricScore(data, a, view, historical) ?? -1),
    )
  const panels = data?.panels ?? []
  const columns: TableColumnsType<MonitoringNode> = [
    {
      title: localeText(localeCode, '节点', 'Node'),
      key: 'name',
      fixed: 'left',
      width: 280,
      sorter: (a, b) => a.nodeRef.name.localeCompare(b.nodeRef.name),
      render: (_, node) => (
        <div>
          <Link
            to={`/cluster-resources/nodes/${encodeURIComponent(node.nodeRef.name)}?clusterId=${encodeURIComponent(clusterId)}`}
          >
            {node.nodeRef.name}
          </Link>
        </div>
      ),
    },
  ]
  columns.push({
    title: localeText(localeCode, '状态', 'Status'),
    key: 'status',
    width: view === 'overview' ? 96 : 136,
    render: (_, node) => (
      <Tooltip
        title={`${localeText(localeCode, '当前快照', 'Current snapshot')} ${node.observedAt ? formatDateTime(node.observedAt) : '—'}`}
      >
        <span>
          <NodeStateTags
            status={node.ready}
            cordoned={node.cordoned}
            conditions={[
              { type: 'MemoryPressure', status: node.memoryPressure },
              { type: 'DiskPressure', status: node.diskPressure },
              { type: 'PIDPressure', status: node.pidPressure },
            ]}
          />
          {view === 'overview' && node.quality !== 'valid' && <Quality value={node.quality} />}
        </span>
      </Tooltip>
    ),
  })
  if (view === 'overview') {
    columns.push(
      ...(['cpu', 'memory', 'pods', 'conntrack'] as const).map((key) => ({
        title: monitoringViews.find((view) => view.key === key)?.[
          localeCode === 'zh_CN' ? 'label' : 'englishLabel'
        ],
        key,
        width: 136,
        sorter: (a: MonitoringNode, b: MonitoringNode) =>
          (nodeMetricScore(data, a, key, historical) ?? -1) -
          (nodeMetricScore(data, b, key, historical) ?? -1),
        render: (_: unknown, node: MonitoringNode) => {
          const metricKey =
            key === 'cpu'
              ? 'host.cpu.used'
              : key === 'memory'
                ? 'host.memory.used'
                : key === 'pods'
                  ? 'pods.bound'
                  : 'conntrack.entries'
          const unit = key === 'cpu' ? 'cores' : key === 'memory' ? 'bytes' : 'count'
          const value =
            key === 'pods' && !historical
              ? node.boundPods
              : latestMetric(data, metricKey, node.nodeRef.name)
          const capacity =
            key === 'cpu'
              ? historical
                ? null
                : node.capacity.cpuCores
              : key === 'pods'
                ? historical
                  ? null
                  : node.allocatable.podSlots
                : alignedMetricDenominator(
                    data,
                    metricKey,
                    key === 'memory' ? 'host.memory.total' : 'conntrack.limit',
                    node.nodeRef.name,
                  )
          const panel = data?.panels.find((panel) => panel.key === metricKey)
          return (
            <div className="soha-monitoring-table-metric">
              <MonitoringUsageMeter
                value={value}
                capacity={capacity}
                unit={unit}
                quality={panel?.quality}
              />
              {panel && panel.quality !== 'valid' && <Quality value={panel.quality} />}
            </div>
          )
        },
      })),
    )
  } else if (view === 'cpu' || view === 'memory') {
    const cpu = view === 'cpu'
    const unit = cpu ? 'cores' : 'bytes'
    const quantity = cpu ? 'cpuCores' : 'memoryBytes'
    columns.push(
      {
        title:
          cpu && historical
            ? localeText(localeCode, '主机历史用量', 'Historical host usage')
            : localeText(
                localeCode,
                cpu ? '主机用量 / 当前容量' : '主机用量 / 同刻容量',
                cpu ? 'Host usage / current capacity' : 'Host usage / aligned capacity',
              ),
        key: 'host',
        sorter: (a, b) =>
          (latestMetric(data, `host.${view}.used`, a.nodeRef.name) ?? -1) -
          (latestMetric(data, `host.${view}.used`, b.nodeRef.name) ?? -1),
        render: (_, node) => (
          <div>
            {valueText(latestMetric(data, `host.${view}.used`, node.nodeRef.name), unit)}
            {!(cpu && historical) && (
              <>
                {' '}
                /{' '}
                {valueText(
                  cpu
                    ? node.capacity.cpuCores
                    : alignedMetricDenominator(
                        data,
                        'host.memory.used',
                        'host.memory.total',
                        node.nodeRef.name,
                      ),
                  unit,
                )}
              </>
            )}
            <div>
              {cpu && historical ? (
                <Text type="secondary">
                  {localeText(
                    localeCode,
                    '历史百分比不计算 · 当前容量：',
                    'Historical percentage unavailable · Current capacity: ',
                  )}
                  {valueText(node.capacity.cpuCores, unit)}
                </Text>
              ) : (
                <Ratio value={nodeMetricScore(data, node, view, historical)} />
              )}
            </div>
          </div>
        ),
      },
      {
        title: localeText(
          localeCode,
          historical ? 'Pod 历史已观测用量' : 'Pod 已观测用量 / allocatable',
          historical ? 'Historical observed Pod usage' : 'Observed Pod usage / allocatable',
        ),
        key: 'pod',
        sorter: (a, b) =>
          (latestMetric(data, `pod.${view}.used`, a.nodeRef.name) ?? -1) -
          (latestMetric(data, `pod.${view}.used`, b.nodeRef.name) ?? -1),
        render: (_, node) => (
          <div>
            {valueText(latestMetric(data, `pod.${view}.used`, node.nodeRef.name), unit)}
            {historical ? (
              <div>
                <Text type="secondary">
                  {localeText(localeCode, '当前 allocatable：', 'Current allocatable: ')}
                  {valueText(node.allocatable[quantity], unit)}
                </Text>
              </div>
            ) : (
              <> / {valueText(node.allocatable[quantity], unit)}</>
            )}
          </div>
        ),
      },
      {
        title: localeText(localeCode, '当前 requests', 'Current requests'),
        key: 'requests',
        sorter: (a, b) => (a.requests[quantity] ?? -1) - (b.requests[quantity] ?? -1),
        render: (_, node) => (
          <div>
            {valueText(node.requests[quantity], unit)}
            <div>
              <Ratio value={monitoringRatio(node.requests[quantity], node.allocatable[quantity])} />
            </div>
          </div>
        ),
      },
      {
        title: localeText(localeCode, '当前 limits', 'Current limits'),
        key: 'limits',
        sorter: (a, b) => (a.limits[quantity] ?? -1) - (b.limits[quantity] ?? -1),
        render: (_, node) =>
          (cpu ? node.cpuLimitsComplete : node.memoryLimitsComplete) ? (
            <div>
              {valueText(node.limits[quantity], unit)}
              <div>
                <Ratio value={monitoringRatio(node.limits[quantity], node.allocatable[quantity])} />
              </div>
            </div>
          ) : (
            <Text type="secondary">
              {localeText(
                localeCode,
                '存在未设置上限 / 覆盖不足',
                'Unlimited resources / incomplete coverage',
              )}
            </Text>
          ),
      },
    )
    if (!cpu)
      columns.push({
        title: 'MemoryPressure',
        key: 'pressure',
        render: (_, node) => node.memoryPressure,
      })
  } else if (view === 'conntrack') {
    columns.push(
      {
        title: localeText(localeCode, '条目 / 上限', 'Entries / limit'),
        key: 'entries',
        sorter: (a, b) =>
          (latestMetric(data, 'conntrack.entries', a.nodeRef.name) ?? -1) -
          (latestMetric(data, 'conntrack.entries', b.nodeRef.name) ?? -1),
        render: (_, node) => (
          <div>
            {valueText(latestMetric(data, 'conntrack.entries', node.nodeRef.name), 'count')} /{' '}
            {valueText(
              alignedMetricDenominator(
                data,
                'conntrack.entries',
                'conntrack.limit',
                node.nodeRef.name,
              ),
              'count',
            )}
            <div>
              <Ratio value={nodeMetricScore(data, node, 'conntrack', historical)} />
            </div>
          </div>
        ),
      },
      ...['drop', 'early_drop', 'insert_failed'].map((key) => ({
        title: `${key} /s`,
        key,
        render: (_: unknown, node: MonitoringNode) =>
          valueText(latestMetric(data, `conntrack.${key}.rate`, node.nodeRef.name), 'events/s'),
      })),
    )
  } else {
    columns.push(
      {
        title: localeText(localeCode, '非终态 Pod / allocatable', 'Nonterminal Pods / allocatable'),
        key: 'pods',
        sorter: (a, b) => (a.boundPods ?? -1) - (b.boundPods ?? -1),
        render: (_, node) => (
          <div>
            {valueText(node.boundPods, 'count')} / {valueText(node.allocatable.podSlots, 'count')}
            <div>
              <Ratio value={nodeMetricScore(data, node, 'pods', historical)} />
            </div>
          </div>
        ),
      },
      {
        title: localeText(localeCode, '剩余 / 溢出槽位', 'Remaining / overflow slots'),
        key: 'slots',
        render: (_, node) =>
          `${valueText(node.remainingPodSlots, 'count')} / ${valueText(node.overflowPodSlots, 'count')}`,
      },
      {
        title: localeText(localeCode, '绑定的 Pending Pod', 'Bound Pending Pods'),
        key: 'pending',
        render: (_, node) => valueText(node.pendingPods, 'count'),
      },
      {
        title: localeText(localeCode, '压力状态', 'Pressure conditions'),
        key: 'pressure',
        render: (_, node) =>
          `Memory ${node.memoryPressure} · Disk ${node.diskPressure} · PID ${node.pidPressure}`,
      },
    )
  }
  if (view !== 'overview')
    columns.push({
      title: localeText(localeCode, '当前快照 / 质量', 'Current snapshot / quality'),
      key: 'quality',
      width: 170,
      render: (_, node) => (
        <div>
          <Quality value={node.quality} />
          <Text type="secondary">
            {node.observedAt
              ? formatDateTime(node.observedAt)
              : localeText(localeCode, '未提供采样时间', 'Observation time unavailable')}
          </Text>
        </div>
      ),
    })
  const trendViews = monitoringViews.filter(
    (item) => item.key !== 'overview' && hasPermission(snapshot, monitoringPermission(item.key)),
  )
  if (trendViews.length)
    columns.push({
      key: 'actions',
      className: 'soha-table-actions-column',
      title: '',
      width: 64,
      minWidth: 64,
      fixed: 'right',
      render: (_, node) => (
        <KubernetesResourceActionsMenu
          name={node.nodeRef.name}
          resourceLabel="Node"
          localeCode={localeCode}
          items={trendViews.map((item) => ({
            key: item.key,
            label: `${localeText(localeCode, '聚焦', 'Focus')} ${item[localeCode === 'zh_CN' ? 'label' : 'englishLabel']}${localeText(localeCode, '趋势', ' trend')}`,
          }))}
          onSelect={(key) => onFocus(node.nodeRef.name, key as MonitoringViewName)}
        />
      ),
    })
  const title =
    monitoringViews.find((item) => item.key === view)?.[
      localeCode === 'zh_CN' ? 'label' : 'englishLabel'
    ] || localeText(localeCode, '资源监控', 'Resource monitoring')
  const primary =
    view === 'cpu'
      ? 'host.cpu.used'
      : view === 'memory'
        ? 'host.memory.used'
        : view === 'conntrack'
          ? 'conntrack.entries'
          : view === 'pods'
            ? 'pods.bound'
            : 'host.cpu.used'
  const secondary =
    view === 'cpu'
      ? 'pod.cpu.used'
      : view === 'memory'
        ? 'pod.memory.used'
        : view === 'conntrack'
          ? 'conntrack.drop.rate'
          : undefined
  const unavailable =
    panels.length > 0 &&
    panels.every((panel) => panel.series.every((series) => series.latest == null))
  return (
    <>
      <ReadFeedback label={title} query={query} canRead={Boolean(clusterId)} />
      {(data?.truncated || query.isError) && (
        <Alert
          showIcon
          type="warning"
          title={
            data?.truncated
              ? localeText(
                  localeCode,
                  '结果达到预算，请选择节点或缩短窗口',
                  'Result budget reached. Select a node or shorten the window.',
                )
              : localeText(
                  localeCode,
                  '此面板刷新失败，保留允许显示的上次结果',
                  'This section failed to refresh. The last permitted result is retained.',
                )
          }
          description={
            data
              ? `${localeText(localeCode, '上次成功生成：', 'Last successful result: ')}${formatDateTime(data.generatedAt)}`
              : undefined
          }
        />
      )}
      {data && (
        <section id={`monitoring-${view}`} className="soha-monitoring-section" aria-label={title}>
          {unavailable && (
            <Alert
              showIcon
              type="info"
              title={localeText(
                localeCode,
                '当前没有可展示的历史样本',
                'No historical samples to display',
              )}
              description={localeText(
                localeCode,
                '容量表独立展示当前 API 快照；缺失值不会补为 0。',
                'The table separately shows the current API snapshot. Missing values are not filled with zero.',
              )}
            />
          )}
          {view !== 'overview' && !unavailable && (
            <>
              <div className="soha-monitoring-trends">
                <Trend
                  key={`${view}-primary`}
                  panels={panels}
                  defaultKey={primary}
                  names={activeNames}
                  title={localeText(localeCode, '用量趋势', 'Usage trend')}
                />
                {secondary && (
                  <Trend
                    key={`${view}-secondary`}
                    panels={panels.filter((panel) => panel.key !== primary)}
                    defaultKey={secondary}
                    names={activeNames}
                    title={
                      view === 'conntrack'
                        ? localeText(localeCode, '异常事件趋势', 'Failure event trend')
                        : localeText(
                            localeCode,
                            'Pod 用量与分配趋势',
                            'Pod usage and allocation trend',
                          )
                    }
                  />
                )}
              </div>
            </>
          )}
          <Text className="soha-monitoring-snapshot-note" type="secondary">
            {localeText(
              localeCode,
              historical
                ? '曲线为历史数据；下表 requests、limits 和 Pod 槽位为当前快照，不代表历史容量或调度保证。'
                : '主机实际用量与 Pod 用量分开统计；requests / limits 为当前分配，Pod 槽位不代表调度保证。',
              historical
                ? 'Charts show history. Requests, limits and Pod slots below are current snapshots, not historical capacity or scheduling guarantees.'
                : 'Host and Pod usage are separate. Requests / limits are current allocations; Pod slots do not guarantee scheduling.',
            )}
          </Text>
          {view === 'pods' && (
            <Space wrap className="soha-monitoring-caption">
              <Text>
                {localeText(
                  localeCode,
                  '集群未绑定节点的 Pending Pod：',
                  'Unbound Pending Pods in this cluster: ',
                )}
                {valueText(data.unscheduledPendingPods, 'count')}
              </Text>
              <Text type="secondary">
                {localeText(
                  localeCode,
                  '已绑定非终态 Pod 包含终止中 Pod；不等同于 kubelet sandbox 数量。',
                  'Bound nonterminal Pods include terminating Pods. This is not a kubelet sandbox count.',
                )}
              </Text>
              {hasPermission(snapshot, 'platform.configuration.resource-quotas.view') && (
                <Link to="/configuration/resourcequotas">
                  {localeText(
                    localeCode,
                    '查看授权命名空间配额',
                    'View authorized namespace quotas',
                  )}
                </Link>
              )}
            </Space>
          )}
          <KubernetesResourceTable
            resourceKey={`resource-monitoring-${view}`}
            shellClassName="soha-management-table-shell"
            columnSettingPlacement="header"
            columnSettingIconOnly
            resourceLabel="Nodes"
            scopeMode="cluster"
            rowKey={(node: MonitoringNode) =>
              `${node.nodeRef.clusterId}:${node.nodeRef.uid || node.nodeRef.name}`
            }
            allRows={nodes}
            columns={columns}
            dataSource={ranked.filter((node) =>
              node.nodeRef.name.toLocaleLowerCase().includes(keyword.trim().toLocaleLowerCase()),
            )}
            filterKey={`${params.toString()}:${keyword}`}
            loading={query.isPending}
            error={query.isError ? query.error : null}
            hasReadData={Boolean(data)}
            headerExtra={
              <Input.Search
                aria-label={localeText(localeCode, '搜索监控节点', 'Search monitored nodes')}
                placeholder={localeText(localeCode, '搜索节点', 'Search nodes')}
                allowClear
                value={keyword}
                onChange={(event) => setKeyword(event.target.value)}
                style={{ width: 240, maxWidth: '100%' }}
              />
            }
            title={
              data.truncated ? (
                <Text>
                  {localeText(
                    localeCode,
                    `已加载 ${nodes.length} 个节点 · 结果已截断`,
                    `Loaded ${nodes.length} nodes · Results truncated`,
                  )}
                </Text>
              ) : undefined
            }
          />
        </section>
      )}
    </>
  )
}
