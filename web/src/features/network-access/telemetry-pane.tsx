import { AdminTable } from '@/components/admin-table'
import {
  ManagementIconButton,
  ManagementKeywordField,
  ManagementQueryActions,
  ManagementQueryPanel,
  ManagementState,
  ManagementTableToolbar,
  useManagementTextFilter,
} from '@/components/management-list'
import { ReadFeedback, useInlineApiError } from '@/components/request-feedback'
import { formatBytes } from '@/components/resource-metrics-format'
import { useI18n } from '@/i18n'
import { readableQueryData } from '@/utils/query-feedback'
import { formatDateTime } from '@/utils/time'
import { ApiOutlined, DownloadOutlined, ReloadOutlined, UploadOutlined } from '@ant-design/icons'
import type {
  NetworkProxyFlowSummary,
  NetworkTelemetryProducer,
} from '@opensoha/contracts/gen/ts/sohaapi'
import { useQuery } from '@tanstack/react-query'
import type { TableColumnsType } from 'antd'
import { Button, Card, Space, Statistic, Typography } from 'antd'
import { useState } from 'react'
import './proxy.css'
import { networkAccessQueries } from './queries'
const FILTER = { limit: 100 } as const
interface NetworkTelemetryPaneProps {
  proxyOnly?: boolean
}
interface ProxyConnectionRow extends NetworkProxyFlowSummary {
  deviceId: string
  deviceName: string
  id: string
  ownerUserId: string
  profileName: string
}
export function NetworkProxyOverviewPane() {
  return <NetworkTelemetryPane proxyOnly />
}
export function NetworkTelemetryPane({ proxyOnly = false }: NetworkTelemetryPaneProps = {}) {
  const { t } = useI18n()
  const [flowDraft, setFlowDraft] = useState('')
  const [flowKeyword, setFlowKeyword] = useState('')
  const telemetry = useQuery(networkAccessQueries.telemetrySummary(FILTER))
  const visibleTelemetryData =
    telemetry.isEnabled === false ? undefined : readableQueryData(telemetry)
  useInlineApiError(telemetry.isError ? telemetry.error : null)
  const summary = visibleTelemetryData
  const visibleFlows = useManagementTextFilter(summary?.proxyFlows ?? [], flowKeyword, (flow) => [
    flow.producerId,
    flow.profileId,
    flow.selectedProxy,
    flow.engine,
  ])
  const rankedFlows = [...visibleFlows].sort(
    (left, right) =>
      right.uploadBytes + right.downloadBytes - left.uploadBytes - left.downloadBytes,
  )
  const largestFlow = rankedFlows[0] ? rankedFlows[0].uploadBytes + rankedFlows[0].downloadBytes : 0
  const window = summary
    ? `${formatDateTime(summary.from)} — ${formatDateTime(summary.to)}`
    : t('networkAccess.telemetry.lastHour', '最近 1 小时')
  const metrics = [
    {
      icon: <ApiOutlined />,
      label: t('networkAccess.telemetry.events', '遥测事件'),
      value: summary?.eventCount ?? 0,
      helper: `${t('networkAccess.telemetry.heartbeats', '心跳')} ${summary?.heartbeatCount ?? 0} · RADIUS ${summary?.radiusAccountingCount ?? 0}`,
    },
    {
      icon: <UploadOutlined />,
      label: t('networkAccess.telemetry.upload', '上传'),
      value: `${(summary?.uploadBytes ?? 0).toLocaleString()} B`,
    },
    {
      icon: <DownloadOutlined />,
      label: t('networkAccess.telemetry.download', '下载'),
      value: `${(summary?.downloadBytes ?? 0).toLocaleString()} B`,
    },
    {
      icon: <ApiOutlined />,
      label: t('networkAccess.telemetry.connections', '活动连接'),
      value: summary?.activeConnections ?? 0,
      helper: `${t('networkAccess.telemetry.proxyFlows', '代理流量组')} ${summary?.proxyFlowCount ?? 0}`,
    },
  ]
  const producerColumns: TableColumnsType<NetworkTelemetryProducer> = [
    { title: t('networkAccess.telemetry.producerId', '生产者 ID'), dataIndex: 'producerId' },
    {
      title: t('networkAccess.telemetry.producerKind', '类型'),
      dataIndex: 'producerKind',
      width: 150,
    },
    {
      title: t('networkAccess.telemetry.lastSeen', '最近上报'),
      dataIndex: 'lastSeenAt',
      width: 190,
      render: formatDateTime,
    },
    { title: t('networkAccess.telemetry.gaps', '序列缺口'), dataIndex: 'gapCount', width: 120 },
    {
      title: t('networkAccess.telemetry.regressions', '序列回退'),
      dataIndex: 'regressionCount',
      width: 120,
    },
  ]
  const proxyColumns: TableColumnsType<NetworkProxyFlowSummary> = [
    { title: t('networkAccess.telemetry.producerId', '生产者 ID'), dataIndex: 'producerId' },
    { title: t('networkAccess.mihomo.engine', '引擎'), dataIndex: 'engine', width: 100 },
    { title: t('networkAccess.telemetry.profileId', '配置 ID'), dataIndex: 'profileId' },
    { title: t('networkAccess.mode', '访问形态'), dataIndex: 'mode', width: 160 },
    {
      title: t('networkAccess.telemetry.selectedProxy', '代理节点'),
      dataIndex: 'selectedProxy',
    },
    {
      title: t('networkAccess.telemetry.upload', '上传'),
      dataIndex: 'uploadBytes',
      width: 110,
      render: formatBytes,
    },
    {
      title: t('networkAccess.telemetry.download', '下载'),
      dataIndex: 'downloadBytes',
      width: 110,
      render: formatBytes,
    },
    {
      title: t('networkAccess.proxyConnections.sampled', '采样连接数'),
      dataIndex: 'activeConnections',
      width: 120,
    },
    {
      title: t('networkAccess.telemetry.lastSeen', '最近上报'),
      dataIndex: 'lastOccurredAt',
      width: 190,
      render: formatDateTime,
    },
  ]
  if (!proxyOnly && telemetry.isError && !summary)
    return (
      <ReadFeedback
        canRead={telemetry.isEnabled}
        query={telemetry}
        label={t('networkAccess.telemetry.title', '遥测')}
      />
    )
  if (proxyOnly) {
    const unavailable = telemetry.isError && !summary
    const transfer = (summary?.uploadBytes ?? 0) + (summary?.downloadBytes ?? 0)
    const uploadShare = transfer > 0 ? ((summary?.uploadBytes ?? 0) / transfer) * 100 : 0
    return (
      <div className="soha-proxy-page">
        <h1 className="soha-proxy-sr-only">{t('networkAccess.proxyOverview.title', '流量概览')}</h1>
        {!unavailable ? (
          <div className="soha-proxy-page-head">
            <div>
              <strong>{t('networkAccess.proxyOverview.windowTraffic', '窗口内代理流量')}</strong>
              <Typography.Text type="secondary">{window}</Typography.Text>
            </div>
          </div>
        ) : null}
        {telemetry.isError && summary ? null : null}
        {!unavailable ? (
          <div className="soha-proxy-traffic-summary">
            <div className="soha-proxy-traffic-main">
              <span>{t('networkAccess.proxyOverview.totalTransfer', '总传输量')}</span>
              <strong>{summary ? formatBytes(transfer) : '—'}</strong>
              <div
                className={`soha-proxy-traffic-bar${transfer === 0 ? ' is-empty' : ''}`}
                role="img"
                aria-label={`${t('networkAccess.telemetry.upload', '上传')} ${summary ? formatBytes(summary.uploadBytes) : '—'}；${t('networkAccess.telemetry.download', '下载')} ${summary ? formatBytes(summary.downloadBytes) : '—'}`}
              >
                {transfer > 0 ? <span style={{ width: `${uploadShare}%` }} /> : null}
              </div>
              <div className="soha-proxy-traffic-legend">
                <span>
                  <UploadOutlined /> {t('networkAccess.telemetry.upload', '上传')}{' '}
                  <b>{summary ? formatBytes(summary.uploadBytes) : '—'}</b>
                </span>
                <span>
                  <DownloadOutlined /> {t('networkAccess.telemetry.download', '下载')}{' '}
                  <b>{summary ? formatBytes(summary.downloadBytes) : '—'}</b>
                </span>
              </div>
            </div>
            <div className="soha-proxy-traffic-facts">
              <div>
                <span>{t('networkAccess.proxyOverview.reports', '代理上报事件')}</span>
                <strong>{summary?.proxyFlowCount ?? '—'}</strong>
              </div>
              <div>
                <span>{t('networkAccess.proxyConnections.sampled', '采样连接数')}</span>
                <strong>{summary?.activeConnections ?? '—'}</strong>
              </div>
            </div>
          </div>
        ) : null}
        {!unavailable ? (
          <Typography.Text type="secondary" className="soha-proxy-scope-note">
            {t(
              'networkAccess.proxyOverview.scope',
              '流量为当前时间窗内的累计值；连接数取各终端在窗口内最后一次上报。列表最多展示 100 组汇总。',
            )}
          </Typography.Text>
        ) : null}
        {!unavailable ? (
          <ManagementQueryPanel
            actions={
              <ManagementQueryActions
                disabledReset={!flowDraft && !flowKeyword}
                onReset={() => {
                  setFlowDraft('')
                  setFlowKeyword('')
                }}
              />
            }
            onFinish={() => setFlowKeyword(flowDraft.trim())}
          >
            <ManagementKeywordField
              onChange={setFlowDraft}
              placeholder={t('networkAccess.proxyOverview.search', '搜索生产者、配置或代理节点')}
              value={flowDraft}
            />
          </ManagementQueryPanel>
        ) : null}
        <section className="soha-proxy-list-panel" aria-labelledby="proxy-flow-heading">
          <div className="soha-proxy-list-head">
            <div>
              <h2 id="proxy-flow-heading">
                {t('networkAccess.proxyOverview.traffic', '代理流量')}
              </h2>
              <Typography.Text type="secondary">
                {t('networkAccess.proxyOverview.distribution', '按已返回的流量组比较传输量')}
              </Typography.Text>
            </div>
            <ManagementIconButton
              aria-label={t('networkAccess.refresh', '刷新')}
              icon={<ReloadOutlined />}
              loading={telemetry.isFetching}
              tooltip={t('networkAccess.refresh', '刷新')}
              onClick={() => void (telemetry.isEnabled === false ? undefined : telemetry.refetch())}
            />
          </div>
          {unavailable ? (
            <ManagementState
              actions={
                <Button
                  onClick={() =>
                    void (telemetry.isEnabled === false ? undefined : telemetry.refetch())
                  }
                >
                  {t('networkAccess.refresh', '刷新')}
                </Button>
              }
              compact
              kind="error"
              description={t(
                'networkAccess.proxyOverview.error',
                '代理遥测暂不可用，请检查遥测查询服务后重试。',
              )}
            />
          ) : telemetry.isLoading ? (
            <ManagementState compact kind="loading" />
          ) : rankedFlows.length === 0 ? (
            <ManagementState
              compact
              kind="empty"
              description={
                flowKeyword
                  ? t('networkAccess.proxyOverview.noMatch', '没有符合查询条件的代理流量。')
                  : t(
                      'networkAccess.proxyOverview.empty',
                      '此时间窗内没有代理上报。请确认终端已启用代理并正常上报。',
                    )
              }
            />
          ) : (
            <ul className="soha-proxy-flow-list">
              {rankedFlows.map((flow) => {
                const bytes = flow.uploadBytes + flow.downloadBytes
                return (
                  <li
                    key={`${flow.producerId}:${flow.engine}:${flow.profileId}:${flow.profileRevision}:${flow.mode}:${flow.selectedProxy}`}
                  >
                    <div className="soha-proxy-flow-label">
                      <strong>{flow.selectedProxy}</strong>
                      <span>
                        {flow.engine} · {flow.profileId} · {flow.producerId}
                      </span>
                    </div>
                    <div className="soha-proxy-flow-volume">
                      <strong>{formatBytes(bytes)}</strong>
                      <div className="soha-proxy-flow-track" aria-hidden="true">
                        <span
                          style={{ width: `${largestFlow > 0 ? (bytes / largestFlow) * 100 : 0}%` }}
                        />
                      </div>
                      <span>
                        {t('networkAccess.telemetry.upload', '上传')}{' '}
                        {formatBytes(flow.uploadBytes)} ·{' '}
                        {t('networkAccess.telemetry.download', '下载')}{' '}
                        {formatBytes(flow.downloadBytes)}
                      </span>
                    </div>
                  </li>
                )
              })}
            </ul>
          )}
        </section>
      </div>
    )
  }
  return (
    <Space orientation="vertical" size={16} style={{ width: '100%' }}>
      <ReadFeedback canRead={telemetry.isEnabled} query={telemetry} label="遥测" />

      <ManagementTableToolbar>
        <Typography.Text type="secondary">{window}</Typography.Text>
        <ManagementIconButton
          aria-label={t('networkAccess.refresh', '刷新')}
          icon={<ReloadOutlined />}
          loading={telemetry.isFetching}
          tooltip={t('networkAccess.refresh', '刷新')}
          onClick={() => void (telemetry.isEnabled === false ? undefined : telemetry.refetch())}
        />
      </ManagementTableToolbar>
      <div className="soha-overview-metric-grid">
        {metrics.map((metric) => (
          <Card key={metric.label} size="small" loading={telemetry.isLoading}>
            <Statistic title={metric.label} value={metric.value} prefix={metric.icon} />
            {metric.helper ? (
              <Typography.Text type="secondary">{metric.helper}</Typography.Text>
            ) : null}
          </Card>
        ))}
      </div>
      <AdminTable
        columns={producerColumns}
        dataSource={summary?.producers ?? []}
        loading={telemetry.isLoading}
        localSorting
        rowKey="producerId"
        title={t('networkAccess.telemetry.producers', '遥测生产者')}
      />
      <AdminTable
        columns={proxyColumns}
        dataSource={summary?.proxyFlows ?? []}
        loading={telemetry.isLoading}
        localSorting
        rowKey={(item) =>
          `${item.producerId}:${item.engine}:${item.profileId}:${item.profileRevision}:${item.mode}:${item.selectedProxy}`
        }
        title={t('networkAccess.telemetry.proxyFlows', '代理流量组')}
      />
    </Space>
  )
}
export function NetworkProxyConnectionsPane({
  canViewDevices,
  canViewProfiles,
}: {
  canViewDevices: boolean
  canViewProfiles: boolean
}) {
  const { t } = useI18n()
  const [draftSearch, setDraftSearch] = useState('')
  const [search, setSearch] = useState('')
  const enabled = canViewDevices && canViewProfiles
  const telemetry = useQuery(networkAccessQueries.telemetrySummary(FILTER, enabled))
  const visibleTelemetryData =
    telemetry.isEnabled === false ? undefined : readableQueryData(telemetry)
  const profiles = useQuery(networkAccessQueries.mihomoProfiles({ limit: 200 }, enabled))
  const visibleProfilesData = profiles.isEnabled === false ? undefined : readableQueryData(profiles)
  const devices = useQuery(networkAccessQueries.devices({ limit: 200 }, enabled))
  const visibleDevicesData = devices.isEnabled === false ? undefined : readableQueryData(devices)
  const profilesById = new Map((visibleProfilesData ?? []).map((item) => [item.id, item]))
  const devicesById = new Map((visibleDevicesData ?? []).map((item) => [item.id, item]))
  const rows: ProxyConnectionRow[] = (visibleTelemetryData?.proxyFlows ?? []).map((flow) => {
    const profile = profilesById.get(flow.profileId)
    const device = profile ? devicesById.get(profile.deviceId) : undefined
    return {
      ...flow,
      id: `${flow.producerId}:${flow.engine}:${flow.profileId}:${flow.profileRevision}:${flow.mode}:${flow.selectedProxy}`,
      profileName: profile?.name ?? flow.profileId,
      deviceId: profile?.deviceId ?? '',
      deviceName: device?.name ?? profile?.deviceId ?? '—',
      ownerUserId: device?.ownerUserId || '—',
    }
  })
  const visibleRows = useManagementTextFilter(rows, search, (item) => [
    item.ownerUserId,
    item.deviceId,
    item.deviceName,
    item.profileId,
    item.profileName,
    item.selectedProxy,
    item.producerId,
  ])
  if (!enabled) {
    return (
      <ManagementState
        kind="no-permission"
        description={t(
          'networkAccess.proxyConnections.permission',
          '查看用户连接还需要代理隧道与终端资产查看权限。',
        )}
      />
    )
  }
  const unavailable =
    (telemetry.isError || profiles.isError || devices.isError) && !visibleTelemetryData
  return (
    <div className="soha-proxy-page">
      <ReadFeedback canRead={telemetry.isEnabled} query={telemetry} label="遥测" />
      <ReadFeedback canRead={profiles.isEnabled} query={profiles} label="连接方案" />
      <ReadFeedback canRead={devices.isEnabled} query={devices} label="设备" />

      <h1 className="soha-proxy-sr-only">
        {t('networkAccess.proxyConnections.title', '用户连接')}
      </h1>
      {!unavailable ? (
        <div className="soha-proxy-page-head">
          <div>
            <strong>{t('networkAccess.proxyConnections.summaryTitle', '终端代理连接汇总')}</strong>
            <Typography.Text type="secondary">
              {visibleTelemetryData
                ? `${formatDateTime(visibleTelemetryData.from)} — ${formatDateTime(visibleTelemetryData.to)}`
                : t('networkAccess.telemetry.lastHour', '最近 1 小时')}
            </Typography.Text>
          </div>
          <div className="soha-proxy-connection-count">
            <span>{t('networkAccess.proxyConnections.sampled', '采样连接数')}</span>
            <strong>{visibleTelemetryData?.activeConnections ?? '—'}</strong>
          </div>
        </div>
      ) : null}
      {!unavailable ? (
        <Typography.Text type="secondary" className="soha-proxy-scope-note">
          {t(
            'networkAccess.proxyConnections.scope',
            '每张卡按上报来源、配置和节点汇总，并关联可见的终端及用户；连接数是窗口内最后一次采样值，不是实时连接明细。',
          )}
        </Typography.Text>
      ) : null}
      {!unavailable && (telemetry.isError || profiles.isError || devices.isError) ? null : null}
      {!unavailable ? (
        <ManagementQueryPanel
          actions={
            <ManagementQueryActions
              disabledReset={!draftSearch && !search}
              onReset={() => {
                setDraftSearch('')
                setSearch('')
              }}
            />
          }
          onFinish={() => setSearch(draftSearch.trim())}
        >
          <ManagementKeywordField
            onChange={setDraftSearch}
            placeholder={t(
              'networkAccess.proxyConnections.search',
              '搜索用户、终端、配置或代理节点',
            )}
            value={draftSearch}
          />
        </ManagementQueryPanel>
      ) : null}
      <section className="soha-proxy-list-panel" aria-labelledby="proxy-connections-heading">
        <div className="soha-proxy-list-head">
          <div>
            <h2 id="proxy-connections-heading">
              {t('networkAccess.proxyConnections.groups', '连接采样组')}
            </h2>
            {!unavailable && visibleTelemetryData ? (
              <Typography.Text type="secondary">
                {t('networkAccess.proxyConnections.groupCount', '当前返回 {count} 组').replace(
                  '{count}',
                  String(rows.length),
                )}
              </Typography.Text>
            ) : null}
          </div>
          <ManagementIconButton
            aria-label={t('networkAccess.refresh', '刷新')}
            icon={<ReloadOutlined />}
            loading={telemetry.isFetching || profiles.isFetching || devices.isFetching}
            tooltip={t('networkAccess.refresh', '刷新')}
            onClick={() => {
              void (telemetry.isEnabled === false ? undefined : telemetry.refetch())
              void (profiles.isEnabled === false ? undefined : profiles.refetch())
              void (devices.isEnabled === false ? undefined : devices.refetch())
            }}
          />
        </div>
        {unavailable ? (
          <ManagementState
            actions={
              <Button
                onClick={() => {
                  void (telemetry.isEnabled === false ? undefined : telemetry.refetch())
                  void (profiles.isEnabled === false ? undefined : profiles.refetch())
                  void (devices.isEnabled === false ? undefined : devices.refetch())
                }}
              >
                {t('networkAccess.refresh', '刷新')}
              </Button>
            }
            compact
            kind="error"
            description={t(
              'networkAccess.proxyConnections.error',
              '无法读取代理汇总、隧道配置或终端资产，请检查服务后重试。',
            )}
          />
        ) : telemetry.isLoading || profiles.isLoading || devices.isLoading ? (
          <ManagementState compact kind="loading" />
        ) : visibleRows.length === 0 ? (
          <ManagementState
            compact
            kind="empty"
            description={
              search
                ? t('networkAccess.proxyConnections.noMatch', '没有符合查询条件的连接采样组。')
                : t(
                    'networkAccess.proxyConnections.empty',
                    '此时间窗内没有连接采样。请确认代理终端正在上报。',
                  )
            }
          />
        ) : (
          <ul className="soha-proxy-connection-list">
            {visibleRows.map((row) => (
              <li key={row.id}>
                <div className="soha-proxy-connection-identity">
                  <span>{row.ownerUserId}</span>
                  <strong>{row.deviceName}</strong>
                  <span>
                    {row.profileName} · {row.engine}
                  </span>
                </div>
                <div className="soha-proxy-connection-path">
                  <span>{t('networkAccess.telemetry.selectedProxy', '代理节点')}</span>
                  <strong>{row.selectedProxy}</strong>
                  <span>
                    {t('networkAccess.telemetry.lastSeen', '最近上报')}{' '}
                    {formatDateTime(row.lastOccurredAt)}
                  </span>
                </div>
                <div className="soha-proxy-connection-transfer">
                  <span>
                    {t('networkAccess.telemetry.upload', '上传')} {formatBytes(row.uploadBytes)}
                  </span>
                  <span>
                    {t('networkAccess.telemetry.download', '下载')} {formatBytes(row.downloadBytes)}
                  </span>
                </div>
                <div className="soha-proxy-connection-sample">
                  <strong>{row.activeConnections}</strong>
                  <span>{t('networkAccess.proxyConnections.sampled', '采样连接数')}</span>
                </div>
              </li>
            ))}
          </ul>
        )}
      </section>
    </div>
  )
}
