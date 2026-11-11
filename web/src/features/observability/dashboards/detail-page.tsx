import { ManagementDataPage } from '@/components/management-data-page'
import { ManagementIconButton, ManagementState } from '@/components/management-list'
import { ReadFeedback } from '@/components/request-feedback'
import {
  buildCompactChartSpec,
  compactMetricColors,
  formatMetricValue,
  type CompactChartLine,
} from '@/components/resource-metrics-panel'
import { MetadataTag } from '@/components/status-tag'
import { hasPermission, usePermissionSnapshot } from '@/features/auth'
import { useAIPageContext } from '@/features/copilot'
import { readableQueryData } from '@/utils/query-feedback'
import { formatDateTime } from '@/utils/time'
import {
  AlertOutlined,
  LeftOutlined,
  PauseCircleOutlined,
  PlayCircleOutlined,
  ReloadOutlined,
  RightOutlined,
} from '@ant-design/icons'
import type {
  ObservabilityDashboardPanel,
  ObservabilityDashboardPanelQueryInput,
  ObservabilityMetricSeries,
} from '@opensoha/contracts/gen/ts/sohaapi'
import { useQuery } from '@tanstack/react-query'
import { LineChart } from '@visactor/react-vchart'
import { Alert, Card, Flex, Select, Space, Table, Typography } from 'antd'
import { useCallback, useEffect, useMemo, useState } from 'react'
import { useLocation, useNavigate, useParams, useSearchParams } from 'react-router-dom'
import {
  dashboardPanelAlertRulePath,
  dashboardPanelQueryInput,
  dashboardPlaybackParams,
  dashboardVariableValues,
  readDashboardPlayback,
  shiftDashboardPlayback,
  type DashboardPlaybackWindow,
} from './model'
import { observabilityDashboardQueries } from './queries'
import './styles.css'

const { Paragraph, Text } = Typography
const rangeOptions = [
  { label: '最近 15 分钟', value: 15 },
  { label: '最近 1 小时', value: 60 },
  { label: '最近 6 小时', value: 360 },
  { label: '最近 24 小时', value: 1440 },
]
const lineColors = [
  compactMetricColors.default,
  compactMetricColors.cpu,
  compactMetricColors.memory,
  compactMetricColors.diskRead,
  compactMetricColors.diskWrite,
]

export function ObservabilityDashboardDetailPage() {
  const dashboardId = decodeURIComponent(useParams<{ dashboardId: string }>().dashboardId ?? '')
  const navigate = useNavigate()
  const imported = useLocation().state?.importedDashboard as
    | { id: string; panelCount: number; warnings: string[] }
    | undefined
  const [searchParams, setSearchParams] = useSearchParams()
  const [playing, setPlaying] = useState(false)
  const pausePlayback = useCallback(() => setPlaying(false), [])
  const dashboardQuery = useQuery(observabilityDashboardQueries.detail(dashboardId))
  const dataSourcesQuery = useQuery(observabilityDashboardQueries.metricDataSources())
  const permissionQuery = usePermissionSnapshot()
  const dashboardData = readableQueryData(dashboardQuery)
  const canManageRules = hasPermission(permissionQuery.data?.data, 'observe.alert-rules.manage')
  const playback = useMemo(() => readDashboardPlayback(searchParams), [searchParams])
  const variables = useMemo(
    () => dashboardVariableValues(dashboardData?.variables ?? [], searchParams),
    [dashboardData?.variables, searchParams],
  )
  const variableSignature = JSON.stringify(variables)
  const panelQuery = useMemo(
    () => dashboardPanelQueryInput(playback, variables),
    [playback, variables],
  )
  useAIPageContext({
    sourceWorkbench: 'monitoring',
    sourceTitle: '仪表盘调查',
    entityKind: 'monitoring.dashboard',
    entityName: dashboardData?.name || dashboardId || '仪表盘',
    timeRangeMinutes: playback.rangeMinutes,
    visibleFilters: {
      timeFrom: playback.from,
      timeTo: playback.to,
      ...variables,
    },
    pinnedData: dashboardData
      ? {
          dashboardId: dashboardData.id,
          dataSourceId: dashboardData.dataSourceId,
          panelCount: dashboardData.panels.length,
        }
      : { dashboardId },
    promptHint: '分析当前仪表盘时间窗、变量和面板趋势，并给出可打开的查询证据。',
  })
  const updatePlayback = useCallback(
    (next: DashboardPlaybackWindow, nextVariables = variables) => {
      setSearchParams(dashboardPlaybackParams(searchParams, next, nextVariables), {
        replace: true,
      })
    },
    [searchParams, setSearchParams, variables],
  )

  useEffect(() => {
    if (!dashboardData) return
    const next = dashboardPlaybackParams(searchParams, playback, variables)
    if (next.toString() !== searchParams.toString()) setSearchParams(next, { replace: true })
  }, [dashboardData, playback, searchParams, setSearchParams, variableSignature, variables])

  useEffect(() => {
    if (!playing || dashboardQuery.isError || !dashboardData) return
    const timer = window.setInterval(
      () => updatePlayback(shiftDashboardPlayback(playback, 1), variables),
      5_000,
    )
    return () => window.clearInterval(timer)
  }, [
    playback,
    playing,
    updatePlayback,
    variableSignature,
    variables,
    dashboardQuery.isError,
    dashboardData,
  ])

  useEffect(() => {
    if (dashboardQuery.isError) pausePlayback()
  }, [dashboardQuery.isError, pausePlayback])

  if (dashboardQuery.isLoading) {
    return <ManagementState kind="loading" title="正在加载仪表盘" />
  }
  if (!dashboardData) {
    return <ReadFeedback label="仪表盘" query={dashboardQuery} />
  }
  const dashboard = dashboardData
  const dataSourceName = readableQueryData(dataSourcesQuery)?.find(
    (item) => item.id === dashboard.dataSourceId,
  )?.name
  return (
    <ManagementDataPage
      className="soha-dashboard-detail-page"
      beforeQuery={
        <>
          {imported?.id === dashboardId ? (
            <Alert
              showIcon
              type={imported.warnings.length ? 'warning' : 'success'}
              title={`已导入 ${imported.panelCount} 个面板`}
              description={
                imported.warnings.join('；') || '仪表盘记录已保存，面板数据仍以实际查询结果为准。'
              }
            />
          ) : null}
          <ReadFeedback label="仪表盘" query={dashboardQuery} />
          <ReadFeedback label="指标数据源" query={dataSourcesQuery} />
        </>
      }
      header={{
        title: dashboard.name,
        meta: (
          <Flex gap={4} wrap>
            <MetadataTag
              label={dashboard.tags.includes('soha-template') ? 'Soha 模板' : 'Grafana'}
              tone={dashboard.tags.includes('soha-template') ? 'blue' : 'orange'}
            />
            <MetadataTag
              label={dataSourceName ?? (dashboard.dataSourceId ? '数据源不可用' : '未绑定数据源')}
              tone={dataSourceName ? 'blue' : 'orange'}
            />
            {dashboard.tags.slice(0, 5).map((tag) => (
              <MetadataTag key={tag} label={tag} />
            ))}
          </Flex>
        ),
        actions: (
          <Space wrap>
            <ManagementIconButton
              aria-label="刷新仪表盘记录"
              tooltip="刷新仪表盘记录"
              icon={<ReloadOutlined spin={dashboardQuery.isFetching} />}
              onClick={() => void dashboardQuery.refetch()}
            />
            {(dashboard.variables ?? []).map((variable) => (
              <Select
                aria-label={variable.label || variable.name}
                key={variable.name}
                options={variable.options.map((value) => ({ label: value, value }))}
                value={variables[variable.name]}
                onChange={(value) =>
                  updatePlayback(playback, { ...variables, [variable.name]: value })
                }
              />
            ))}
            <Select
              aria-label="时间范围"
              options={rangeOptions}
              value={playback.rangeMinutes}
              onChange={(rangeMinutes) => {
                const to = new Date()
                updatePlayback({
                  from: new Date(to.getTime() - rangeMinutes * 60_000).toISOString(),
                  rangeMinutes,
                  to: to.toISOString(),
                })
              }}
            />
            <Flex gap={2}>
              <ManagementIconButton
                aria-label="上一时间窗"
                icon={<LeftOutlined />}
                tooltip="上一时间窗"
                onClick={() => updatePlayback(shiftDashboardPlayback(playback, -1))}
              />
              <ManagementIconButton
                disabled={dashboardQuery.isError}
                aria-label={playing ? '暂停播放' : '播放时间窗'}
                aria-pressed={playing}
                icon={playing ? <PauseCircleOutlined /> : <PlayCircleOutlined />}
                tooltip={playing ? '暂停' : '播放'}
                onClick={() => setPlaying((value) => !value)}
              />
              <ManagementIconButton
                aria-label="下一时间窗"
                icon={<RightOutlined />}
                tooltip="下一时间窗"
                onClick={() => updatePlayback(shiftDashboardPlayback(playback, 1))}
              />
            </Flex>
            <Text aria-atomic="true" aria-live="polite" role="status" type="secondary">
              自动播放：{playing && !dashboardQuery.isError ? '进行中' : '已暂停'}
            </Text>
          </Space>
        ),
      }}
      tableNode={
        <Space orientation="vertical" size={12} style={{ width: '100%' }}>
          {(dashboard.importWarnings ?? []).length > 0 ? (
            <Alert
              showIcon
              type="warning"
              title="导入兼容提示"
              description={dashboard.importWarnings
                ?.slice(0, 5)
                .map((warning) => warning.message)
                .join('；')}
            />
          ) : null}
          <Typography.Text type="secondary">
            {new Date(playback.from).toLocaleString()} - {new Date(playback.to).toLocaleString()}
          </Typography.Text>
          <div className="soha-dashboard-grid">
            {dashboard.panels.map((panel) => (
              <DashboardPanel
                key={panel.id}
                onReadError={pausePlayback}
                canManageRules={canManageRules}
                dashboardId={dashboard.id}
                dashboardName={dashboard.name}
                dataSourceId={dashboard.dataSourceId ?? ''}
                input={panelQuery}
                panel={panel}
                onNavigate={navigate}
              />
            ))}
          </div>
        </Space>
      }
    />
  )
}

function DashboardPanel({
  onReadError,
  canManageRules,
  dashboardId,
  dashboardName,
  dataSourceId,
  input,
  onNavigate,
  panel,
}: {
  onReadError: () => void
  canManageRules: boolean
  dashboardId: string
  dashboardName: string
  dataSourceId: string
  input: ObservabilityDashboardPanelQueryInput
  onNavigate: (path: string) => void
  panel: ObservabilityDashboardPanel
}) {
  const style = {
    gridColumn: `${panel.layout.x + 1} / span ${panel.layout.w}`,
    gridRow: `${panel.layout.y + 1} / span ${panel.layout.h}`,
  }
  if (panel.type === 'row') {
    return (
      <div className="soha-dashboard-row" style={style}>
        {panel.title}
      </div>
    )
  }
  return (
    <Card
      className="soha-dashboard-panel"
      size="small"
      style={style}
      title={panel.title}
      extra={
        panel.queryable && canManageRules ? (
          <ManagementIconButton
            aria-label={`基于 ${panel.title} 创建告警`}
            icon={<AlertOutlined />}
            size="small"
            tooltip="创建告警"
            onClick={() =>
              onNavigate(dashboardPanelAlertRulePath(dashboardName, dataSourceId, panel, input))
            }
          />
        ) : undefined
      }
    >
      {panel.type === 'text' ? (
        <Paragraph className="soha-dashboard-text-panel">{panel.markdown || '-'}</Paragraph>
      ) : panel.unsupported ? (
        <ManagementState
          bordered={false}
          compact
          description={`暂不支持 Grafana ${panel.sourcePanelType || 'plugin'} renderer；原始 JSON 已保留。`}
        />
      ) : panel.queryable ? (
        <DashboardMetricPanel
          onReadError={onReadError}
          dashboardId={dashboardId}
          input={input}
          panel={panel}
        />
      ) : (
        <ManagementState bordered={false} compact description="没有可执行的 Prometheus 查询" />
      )}
    </Card>
  )
}

function DashboardMetricPanel({
  onReadError,
  dashboardId,
  input,
  panel,
}: {
  onReadError: () => void
  dashboardId: string
  input: ObservabilityDashboardPanelQueryInput
  panel: ObservabilityDashboardPanel
}) {
  const panelQuery = useQuery(observabilityDashboardQueries.panel(dashboardId, panel.id, input))
  useEffect(() => {
    if (panelQuery.isError) onReadError()
  }, [panelQuery.isError, onReadError])
  if (panelQuery.isLoading) {
    return <ManagementState bordered={false} compact kind="loading" title="正在查询" />
  }
  const series = readableQueryData(panelQuery)?.series
  return (
    <>
      <ReadFeedback label="面板查询" query={panelQuery} />
      <ManagementIconButton
        aria-label={`刷新 ${panel.title} 指标`}
        tooltip="刷新指标"
        icon={<ReloadOutlined spin={panelQuery.isFetching} />}
        onClick={() => void panelQuery.refetch()}
      />
      {series ? (
        series.length ? (
          <DashboardMetricSeries panelType={panel.type} series={series} />
        ) : (
          <ManagementState bordered={false} compact description="当前范围暂无指标数据" />
        )
      ) : null}
    </>
  )
}

export function DashboardMetricSeries({
  panelType,
  series,
}: {
  panelType: ObservabilityDashboardPanel['type']
  series: ObservabilityMetricSeries[]
}) {
  const [dataVisible, setDataVisible] = useState(false)
  if (panelType === 'table') {
    return (
      <Table
        columns={[
          { title: '序列', dataIndex: 'label' },
          { title: '当前值', dataIndex: 'value' },
        ]}
        dataSource={series.map((item) => ({
          key: item.key,
          label: item.label,
          value: formatMetricValue(item.latest, item.unit ?? ''),
        }))}
        pagination={false}
        size="small"
      />
    )
  }
  if (panelType === 'stat' || panelType === 'gauge') {
    return (
      <Flex className="soha-dashboard-stat-values" gap={16} wrap>
        {series.map((item) => (
          <div key={item.key}>
            <Text type="secondary">{item.label}</Text>
            <div className="soha-dashboard-stat-value">
              {formatMetricValue(item.latest, item.unit ?? '')}
            </div>
          </div>
        ))}
      </Flex>
    )
  }
  const lines = metricLines(series)
  return (
    <>
      <div aria-hidden="true" className="soha-dashboard-chart">
        <LineChart
          spec={{
            ...buildCompactChartSpec(lines, series[0]?.unit ?? '', 'zh_CN'),
            background: 'transparent',
          }}
        />
      </div>
      <details
        className="soha-dashboard-data-details"
        onToggle={(event) => event.currentTarget.open && setDataVisible(true)}
      >
        <summary>查看数据表</summary>
        {dataVisible ? (
          <div aria-label="仪表盘指标数据表" className="soha-dashboard-data-region" role="region">
            <table className="soha-dashboard-data-table">
              <thead>
                <tr>
                  <th scope="col">序列</th>
                  <th scope="col">时间</th>
                  <th scope="col">数值</th>
                </tr>
              </thead>
              <tbody>
                {series.map((item) =>
                  item.points.map((point, index) => (
                    <tr key={`${item.key}:${point.timestamp}:${index}`}>
                      <th scope="row">{item.label}</th>
                      <td>{formatDateTime(point.timestamp)}</td>
                      <td>{formatMetricValue(point.value, item.unit ?? '')}</td>
                    </tr>
                  )),
                )}
              </tbody>
            </table>
          </div>
        ) : null}
      </details>
    </>
  )
}

function metricLines(series: ObservabilityMetricSeries[]): CompactChartLine[] {
  return series.map((item, index) => ({
    color: lineColors[index % lineColors.length],
    fill: series.length === 1,
    key: item.key,
    label: item.label,
    points: item.points,
    unit: item.unit ?? '',
  }))
}
