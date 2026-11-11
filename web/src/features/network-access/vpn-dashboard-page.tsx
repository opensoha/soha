import { AdminTable } from '@/components/admin-table'
import {
  ManagementIconButton,
  ManagementQueryActions,
  ManagementQueryField,
} from '@/components/management-list'
import { ReloadOutlined } from '@ant-design/icons'
import { formatDateTime } from '@/utils/time'
import { ManagementDataPage } from '@/components/management-data-page'
import { ReadFeedback } from '@/components/request-feedback'
import { buildCompactChartSpec, compactMetricColors } from '@/components/resource-metrics-panel'
import { StatusTag } from '@/components/status-tag'
import { useOperationMutation } from '@/components/use-operation-mutation'
import { hasPermission, usePermissionSnapshot } from '@/features/auth'
import { useI18n } from '@/i18n'
import { queryAccessDenied, readableQueryData } from '@/utils/query-feedback'
import type {
  NetworkVPNConnectionView,
  NetworkVPNDecision,
  NetworkVPNGatewayMetrics,
  NetworkVPNPreviewInput,
} from '@opensoha/contracts/gen/ts/sohaapi'
import { useQuery } from '@tanstack/react-query'
import { LineChart } from '@visactor/react-vchart'
import {
  Alert,
  Button,
  Card,
  Drawer,
  Form,
  Input,
  Select,
  Space,
  Statistic,
  Tabs,
  Timeline,
  Typography,
} from 'antd'
import { useState } from 'react'
import { previewVPN, vpnDashboard, vpnDecision, vpnDocuments, vpnKeys } from './vpn-api'
import { VPNSessionRevocation } from './vpn-session-revocation'
import './styles.css'
export default function VPNDashboardPage() {
  const { localeCode } = useI18n()
  const text = (zh: string, en: string) => (localeCode === 'zh_CN' ? zh : en)
  const permissions = usePermissionSnapshot()
  const can = (key: string) => hasPermission(permissions.data?.data, key)
  const [filter, setFilter] = useState<Record<string, string>>(() => ({
    from: new Date(Date.now() - 3600000).toISOString(),
    to: new Date().toISOString(),
  }))
  const [decisionID, setDecisionID] = useState('')
  const [revocationSessionId, setRevocationSessionId] = useState('')
  const [previewOpen, setPreviewOpen] = useState(false)
  const [form] = Form.useForm<Record<string, string>>()
  const query = useQuery({
    queryKey: vpnKeys.dashboard(filter),
    queryFn: () => vpnDashboard(filter),
    enabled: can('network_access.vpn_dashboard.view'),
    retry: false,
  })
  const visibleQueryData = query.isEnabled === false ? undefined : readableQueryData(query)
  const profiles = useQuery({
    queryKey: vpnKeys.documents('profiles'),
    queryFn: () => vpnDocuments('profiles'),
    enabled: can('network_access.vpn_profiles.view'),
  })
  const visibleProfilesData = profiles.isEnabled === false ? undefined : readableQueryData(profiles)
  const decision = useQuery({
    queryKey: vpnKeys.decision(decisionID),
    queryFn: () => vpnDecision(decisionID),
    enabled: !!decisionID,
  })
  const visibleDecisionData = decision.isEnabled === false ? undefined : readableQueryData(decision)
  const simulation = useOperationMutation(
    { mutationFn: previewVPN },
    {
      label: ['VPN 选择预览', 'Preview network policy VPN decision'],
      target: () => 'VPN选择预览',
      readOnly: true,
    },
  )
  const operationBlocked = query.isEnabled !== false && queryAccessDenied(query)
  const data = visibleQueryData
  const n = (value: number | undefined, unit = '') =>
    value == null
      ? text('未采集', 'Not collected')
      : `${value.toLocaleString(undefined, { maximumFractionDigits: 1 })}${unit}`
  const reload = () => void query.refetch()
  const chart = (
    title: string,
    field:
      | 'latencyP50Ms'
      | 'latencyP95Ms'
      | 'uploadBytes'
      | 'downloadBytes'
      | 'attempts'
      | 'fallbacks',
    unit: string,
  ) => {
    const points = (data?.series || [])
      .filter((p) => p[field] != null)
      .map((p) => ({ timestamp: p.at, value: p[field]! }))
    return (
      <Card key={field} title={title} size="small">
        <div style={{ height: 220 }}>
          {points.length ? (
            <LineChart
              spec={buildCompactChartSpec(
                [{ key: field, label: title, color: compactMetricColors.default, points, unit }],
                unit,
                localeCode,
              )}
            />
          ) : (
            <Typography.Text type="secondary">
              {text('该时间段无样本', 'No samples in this window')}
            </Typography.Text>
          )}
        </div>
      </Card>
    )
  }
  const summary = (
    <>
      {data?.partial ? (
        <Alert
          type="warning"
          showIcon
          description={text(
            '当前结果达到有界查询上限；请缩短时间或收窄范围。',
            'This bounded result is partial. Shorten the window or narrow the scope.',
          )}
        />
      ) : null}
      {data && !data.telemetryAvailable ? (
        <Alert
          type="info"
          showIcon
          description={text(
            '遥测暂不可用；未知指标不会按零计算。',
            'Telemetry is unavailable; unknown measurements are not treated as zero.',
          )}
        />
      ) : null}
      <div className="soha-network-metrics">
        <Statistic
          title={text('在线 VPN 会话', 'Online VPN sessions')}
          value={data?.activeSessions ?? '—'}
        />
        <Statistic
          title={text('可用 / 总入口', 'Available / total entrances')}
          value={data ? `${data.availableGateways} / ${data.totalGateways}` : '—'}
        />
        <Statistic
          title={text('逻辑连接成功率', 'Logical connection success rate')}
          value={data?.attempts ? `${((100 * data.successes) / data.attempts).toFixed(1)}%` : '—'}
        />
        <Statistic
          title="P50 / P95 RTT"
          value={`${n(data?.latencyP50Ms, ' ms')} / ${n(data?.latencyP95Ms, ' ms')}`}
        />
        <Statistic
          title={text('上传 / 下载速率', 'Upload / download rate')}
          value={`${n(data?.uploadBytesPerSecond, ' B/s')} / ${n(data?.downloadBytesPerSecond, ' B/s')}`}
        />
      </div>
      <Typography.Paragraph className="soha-network-sample-time" type="secondary">
        {text('当前权限范围', 'Current permission scope')} · {formatDateTime(data?.from)} —{' '}
        {formatDateTime(data?.to)} · {text('采样时间', 'As of')} {formatDateTime(data?.asOf)}
      </Typography.Paragraph>
    </>
  )
  const gatewayTable = (
    <AdminTable
      rowKey="gatewayId"
      loading={query.isPending}
      dataSource={data?.gateways || []}
      columns={[
        {
          title: text('接入点', 'Entrance'),
          dataIndex: 'name',
          render: (name: string, row: NetworkVPNGatewayMetrics) => (
            <Button type="link" onClick={() => setFilter({ ...filter, gatewayId: row.gatewayId })}>
              {name || row.gatewayId}
            </Button>
          ),
        },
        {
          title: text('地区 / 供应商', 'Region / provider'),
          key: 'provider',
          render: (_: unknown, row: NetworkVPNGatewayMetrics) =>
            `${row.region} · ${row.providerName || row.providerCode}`,
        },
        {
          title: text('健康', 'Health'),
          key: 'health',
          render: (_: unknown, row: NetworkVPNGatewayMetrics) => (
            <StatusTag
              value={row.healthy == null ? 'unknown' : row.healthy ? 'healthy' : 'unavailable'}
            />
          ),
        },
        {
          title: text('范围内会话 / 入口上限', 'Scoped sessions / entrance limit'),
          key: 'capacity',
          render: (_: unknown, row: NetworkVPNGatewayMetrics) =>
            `${row.activeSessions} / ${row.maxSessions == null ? '—' : row.maxSessions || text('不限', 'Unlimited')}`,
        },
        {
          title: text('成功 / 请求 / 回退', 'Success / attempts / fallbacks'),
          key: 'attempts',
          render: (_: unknown, row: NetworkVPNGatewayMetrics) =>
            `${row.successes} / ${row.attempts} / ${row.fallbacks}`,
        },
        {
          title: 'P50 / P95 RTT',
          key: 'latency',
          render: (_: unknown, row: NetworkVPNGatewayMetrics) =>
            `${n(row.latencyP50Ms, ' ms')} / ${n(row.latencyP95Ms, ' ms')}`,
        },
        {
          title: text('丢包', 'Packet loss'),
          key: 'loss',
          render: () => text('未采集', 'Not collected'),
        },
        { title: text('测量时间', 'Measured at'), dataIndex: 'measuredAt', render: formatDateTime },
      ]}
    />
  )
  const sessions = (
    <AdminTable
      rowKey="sessionId"
      dataSource={data?.sessions || []}
      columns={[
        {
          title: text('用户 / 设备', 'User / device'),
          key: 'owner',
          render: (_: unknown, s: NetworkVPNConnectionView) => `${s.subjectId} / ${s.deviceId}`,
        },
        { title: text('目标方案', 'Target profile'), dataIndex: 'profileName' },
        {
          title: text('偏好 / 实际入口', 'Preference / actual entrance'),
          key: 'entrance',
          render: (_: unknown, s: NetworkVPNConnectionView) => `${s.selection} / ${s.gatewayName}`,
        },
        { title: text('模式', 'Mode'), dataIndex: 'mode' },
        { title: 'IP', dataIndex: 'tunnelIP' },
        {
          title: text('状态', 'State'),
          dataIndex: 'state',
          render: (value: string) => <StatusTag value={value} />,
        },
        { title: text('最后握手', 'Last handshake'), dataIndex: 'lastHandshakeAt' },
        {
          title: text('操作', 'Action'),
          key: 'detail',
          render: (_: unknown, s: NetworkVPNConnectionView) => (
            <Space>
              <Button onClick={() => setDecisionID(s.decisionId)}>
                {text('连接详情', 'Connection details')}
              </Button>
              {can('network_access.vpn_sessions.view') && (
                <Button onClick={() => setRevocationSessionId(s.sessionId)}>
                  {text('撤销与限制', 'Revocation and restrictions')}
                </Button>
              )}
            </Space>
          ),
        },
      ]}
    />
  )
  const decisions = (
    <AdminTable
      rowKey="id"
      dataSource={data?.decisions || []}
      columns={[
        { title: text('时间', 'Time'), dataIndex: 'createdAt' },
        { title: text('策略', 'Strategy'), dataIndex: 'strategy' },
        { title: text('原因', 'Reason'), dataIndex: 'reasonCode' },
        {
          title: text('状态', 'State'),
          dataIndex: 'state',
          render: (value: string) => <StatusTag value={value} />,
        },
        {
          title: text('操作', 'Action'),
          key: 'detail',
          render: (_: unknown, d: NetworkVPNDecision) => (
            <Button onClick={() => setDecisionID(d.id)}>
              {text('候选与决策', 'Candidates and decision')}
            </Button>
          ),
        },
      ]}
    />
  )
  return (
    <ManagementDataPage
      className="soha-network-page soha-network-monitor"
      query={{
        form,
        onFinish: (values) => {
          const hours = Number(values.hours || 1)
          const next: Record<string, string> = {
            from: new Date(Date.now() - hours * 3600000).toISOString(),
            to: new Date().toISOString(),
          }
          for (const [key, value] of Object.entries(values)) {
            if (key !== 'hours' && typeof value === 'string' && value) next[key] = value
          }
          setFilter(next)
        },
        moreLabel: text('更多筛选', 'More filters'),
        lessLabel: text('收起筛选', 'Fewer filters'),
        actions: (
          <ManagementQueryActions
            onReset={() => {
              form.resetFields()
              const now = Date.now()
              setFilter({
                from: new Date(now - 3600000).toISOString(),
                to: new Date(now).toISOString(),
              })
            }}
          />
        ),
        children: (
          <>
            <ManagementQueryField
              width={240}
              name="hours"
              label={text('时间范围', 'Window')}
              initialValue="1"
            >
              <Select
                options={[
                  { value: '1', label: text('最近 1 小时', 'Last hour') },
                  { value: '24', label: text('最近 24 小时', 'Last 24 hours') },
                  { value: '168', label: text('最近 7 天', 'Last 7 days') },
                ]}
              />
            </ManagementQueryField>
            <ManagementQueryField width={280} name="profileId" label={text('方案', 'Profile')}>
              <Select
                placeholder={text('全部方案', 'All profiles')}
                allowClear
                options={visibleProfilesData?.map((p) => ({
                  value: p.id,
                  label: p.configuration.name,
                }))}
              />
            </ManagementQueryField>
            {(['siteId', 'gatewayId', 'providerCode', 'subjectId', 'teamId'] as const).map(
              (key, index) => (
                <ManagementQueryField
                  width={280}
                  key={key}
                  name={key}
                  label={text(
                    ['站点 ID', '入口 ID', '供应商', '用户 ID', '团队 ID'][index],
                    ['Site ID', 'Gateway ID', 'Provider', 'User ID', 'Team ID'][index],
                  )}
                >
                  <Input allowClear />
                </ManagementQueryField>
              ),
            )}
          </>
        ),
      }}
      beforeQuery={
        <>
          <h1 className="soha-network-sr-only">VPN Dashboard</h1>
          <div className="soha-network-monitor-actions">
            <Typography.Text type="secondary">
              {text(
                '连接质量、入口与 Auto 决策',
                'Connection quality, entrances and Auto decisions',
              )}
            </Typography.Text>
            <Space>
              <ManagementIconButton
                aria-label={text('刷新', 'Refresh')}
                tooltip={text('刷新', 'Refresh')}
                icon={<ReloadOutlined />}
                onClick={reload}
                loading={query.isFetching}
              />
              {can('network_access.vpn_selection_policies.preview') ? (
                <Button onClick={() => setPreviewOpen(true)} disabled={operationBlocked}>
                  {text('模拟选择', 'Simulate selection')}
                </Button>
              ) : null}
            </Space>
          </div>
          <section
            className="soha-network-summary"
            aria-label={text('VPN 运行概况', 'VPN runtime summary')}
          >
            {summary}
          </section>
        </>
      }
      tableNode={
        <Tabs
          items={[
            { key: 'gateways', label: text('接入点', 'Entrances'), children: gatewayTable },
            {
              key: 'trends',
              label: text('趋势', 'Trends'),
              children: (
                <>
                  <div className="soha-network-chart-grid">
                    {chart('P50 RTT', 'latencyP50Ms', 'ms')}
                    {chart('P95 RTT', 'latencyP95Ms', 'ms')}
                    {chart(text('上传量 / 小时', 'Uploaded per hour'), 'uploadBytes', 'bytes')}
                    {chart(text('下载量 / 小时', 'Downloaded per hour'), 'downloadBytes', 'bytes')}
                    {chart(text('连接请求', 'Connection attempts'), 'attempts', 'count')}
                    {chart(text('自动回退', 'Automatic fallbacks'), 'fallbacks', 'count')}
                  </div>
                  <AdminTable
                    rowKey="at"
                    dataSource={data?.series || []}
                    columns={[
                      {
                        title: text('采样时间', 'Sampled at'),
                        dataIndex: 'at',
                        render: formatDateTime,
                      },
                      { title: text('连接请求（次）', 'Attempts'), dataIndex: 'attempts' },
                      { title: text('连接成功（次）', 'Successes'), dataIndex: 'successes' },
                      { title: text('自动回退（次）', 'Fallbacks'), dataIndex: 'fallbacks' },
                      { title: text('上传量（B）', 'Uploaded (B)'), dataIndex: 'uploadBytes' },
                      { title: text('下载量（B）', 'Downloaded (B)'), dataIndex: 'downloadBytes' },
                      {
                        title: 'P50 RTT (ms)',
                        dataIndex: 'latencyP50Ms',
                        render: (value?: number) => n(value),
                      },
                      {
                        title: 'P95 RTT (ms)',
                        dataIndex: 'latencyP95Ms',
                        render: (value?: number) => n(value),
                      },
                    ]}
                  />
                </>
              ),
            },
            { key: 'sessions', label: text('当前与历史会话', 'Sessions'), children: sessions },
            { key: 'decisions', label: text('Auto 分析', 'Auto analysis'), children: decisions },
          ]}
        />
      }
    >
      {!(
        (!!decisionID && !(query.isEnabled !== false && queryAccessDenied(query))) ||
        (previewOpen && !(query.isEnabled !== false && queryAccessDenied(query)))
      ) && <> {simulation.feedback} </>}

      <ReadFeedback canRead={query.isEnabled} query={query} label="当前记录" />
      <ReadFeedback canRead={profiles.isEnabled} query={profiles} label="连接方案" />
      <ReadFeedback canRead={decision.isEnabled} query={decision} label="策略决策" />

      <Drawer
        title={text('VPN 会话撤销', 'VPN session revocation')}
        open={Boolean(revocationSessionId)}
        onClose={() => setRevocationSessionId('')}
        size="large"
        destroyOnHidden
      >
        {revocationSessionId && (
          <VPNSessionRevocation
            key={revocationSessionId}
            sessionId={revocationSessionId}
            canView={can('network_access.vpn_sessions.view')}
            canRevoke={can('network_access.vpn_sessions.revoke')}
            canRelease={can('network_access.vpn_sessions.release')}
          />
        )}
      </Drawer>
      <Drawer
        open={!!decisionID && !(query.isEnabled !== false && queryAccessDenied(query))}
        onClose={() => {
          if (!operationBlocked) (() => setDecisionID(''))()
        }}
        title={text('连接详情', 'Connection details')}
        size={760}
      >
        {simulation.feedback}

        {visibleDecisionData ? (
          <VPNDecisionDetail decision={visibleDecisionData} />
        ) : decision.isError && !visibleDecisionData ? null : null}
      </Drawer>
      <Drawer
        open={previewOpen && !(query.isEnabled !== false && queryAccessDenied(query))}
        onClose={() => {
          if (!operationBlocked) (() => setPreviewOpen(false))()
        }}
        title={text('模拟选择（不创建连接）', 'Simulate selection (no connection created)')}
        size={760}
      >
        {simulation.feedback}

        <Form<NetworkVPNPreviewInput>
          layout="vertical"
          initialValues={{ selection: 'auto' }}
          onFinish={(input) => simulation.mutate(input)}
          disabled={operationBlocked}
        >
          <Form.Item name="profileId" label={text('方案', 'Profile')} rules={[{ required: true }]}>
            <Select
              options={visibleProfilesData?.map((p) => ({
                value: p.id,
                label: p.configuration.name,
              }))}
            />
          </Form.Item>
          <Form.Item
            name="deviceId"
            label={text('设备 ID', 'Device ID')}
            rules={[{ required: true }]}
          >
            <Input />
          </Form.Item>
          <Form.Item name="selection" label={text('选择', 'Selection')}>
            <Select
              options={[
                { value: 'auto', label: 'Auto' },
                { value: 'manual', label: text('手动', 'Manual') },
              ]}
            />
          </Form.Item>
          <Form.Item name="gatewayId" label={text('手动入口 ID', 'Manual gateway ID')}>
            <Input />
          </Form.Item>
          <Form.Item
            name="probeBatchId"
            label={text('已有测量批次（可选）', 'Existing probe batch (optional)')}
          >
            <Input />
          </Form.Item>
          <Button
            htmlType="submit"
            type="primary"
            loading={simulation.isPending}
            disabled={operationBlocked}
          >
            {text('模拟', 'Simulate')}
          </Button>
        </Form>
        {simulation.isError ? null : null}
        {simulation.data ? <VPNDecisionDetail decision={simulation.data} /> : null}
      </Drawer>
    </ManagementDataPage>
  )
}
function VPNDecisionDetail({ decision: d }: { decision: NetworkVPNDecision }) {
  const { localeCode } = useI18n()
  const text = (zh: string, en: string) => (localeCode === 'zh_CN' ? zh : en)
  return (
    <>
      <Typography.Paragraph>
        {text('方案修订', 'Profile revision')} r{d.profileRevision} ·{' '}
        {text('策略修订', 'Policy revision')} r{d.selectionPolicyRevision} · {d.strategy}
      </Typography.Paragraph>
      <Timeline
        items={[
          {
            content: `${formatDateTime(d.createdAt)} · ${text('请求与候选选择', 'Request and candidate selection')}`,
          },
          { content: `${formatDateTime(d.updatedAt)} · ${d.state} · ${d.reasonCode}` },
        ]}
      />
      <AdminTable
        rowKey="gatewayId"
        dataSource={d.candidates}
        columns={[
          { title: text('接入点', 'Entrance'), dataIndex: 'name' },
          { title: text('供应商', 'Provider'), dataIndex: 'providerCode' },
          { title: text('排名', 'Rank'), dataIndex: 'rank' },
          {
            title: text('资格', 'Eligibility'),
            dataIndex: 'eligible',
            render: (value: boolean) => <StatusTag value={value ? 'allow' : 'denied'} />,
          },
          { title: text('原因', 'Reason'), dataIndex: 'reasonCode' },
          {
            title: 'RTT (ms)',
            dataIndex: 'latencyMs',
            render: (value: number | undefined) => value ?? text('未采集', 'Not collected'),
          },
          {
            title: text('测量时间', 'Measured at'),
            dataIndex: 'measuredAt',
            render: formatDateTime,
          },
        ]}
      />
    </>
  )
}
