import { ManagementState } from '@/components/management-list'
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
  ApiOutlined,
  AppstoreOutlined,
  AuditOutlined,
  CheckCircleOutlined,
  CloudServerOutlined,
  DatabaseOutlined,
  FileOutlined,
  KeyOutlined,
  LinkOutlined,
  SafetyCertificateOutlined,
  UserSwitchOutlined,
} from '@ant-design/icons'
import { Button, Card, Space, Typography } from 'antd'
import { useNavigate } from 'react-router-dom'
import './styles.css'
import { useIdentityOverviewData } from './use-overview-data'
const { Text } = Typography
function resultTag(result?: string) {
  const value = result || '-'
  return <StatusTag value={value} />
}
function latestUpdated(
  items: Array<{
    updatedAt?: string
    createdAt?: string
  }>,
) {
  const latest = items
    .map((item) => new Date(item.updatedAt || item.createdAt || '').getTime())
    .filter((value) => Number.isFinite(value))
    .sort((left, right) => right - left)[0]
  return latest ? new Date(latest).toISOString() : ''
}
function formatStorageBytes(value: number) {
  const units = ['B', 'KiB', 'MiB', 'GiB']
  let size = value
  let index = 0
  while (size >= 1024 && index < units.length - 1) {
    size /= 1024
    index += 1
  }
  return `${size >= 10 || index === 0 ? size.toFixed(0) : size.toFixed(1)} ${units[index]}`
}
function available(query: { data?: unknown; error: Error | null; isEnabled: boolean }) {
  return query.isEnabled && readableQueryData(query) !== undefined
}
export function IdentityOverviewPage() {
  const navigate = useNavigate()
  const {
    reads,
    applications,
    providers,
    outposts,
    sessions,
    audits,
    softwareStorage,
    storageIntegrations,
    softwareError,
    runtime,
    loading,
    permissions,
    refreshAll,
  } = useIdentityOverviewData()
  const oidcProviders = providers.filter((provider) => provider.type === 'oidc')
  const proxyProviders = providers.filter((provider) => provider.type === 'proxy')
  const samlProviders = providers.filter((provider) => provider.type === 'saml')
  const enabledProviders = providers.filter(
    (provider) => provider.enabled && provider.status === 'enabled',
  )
  const enabledApplications = applications.filter((application) => application.status === 'enabled')
  const onlineOutposts = outposts.filter((outpost) => outpost.status === 'online')
  const oidcEnabled = oidcProviders.some(
    (provider) => provider.enabled && provider.status === 'enabled',
  )
  const proxyEnabled = proxyProviders.some(
    (provider) => provider.enabled && provider.status === 'enabled',
  )
  const enabledStorageIntegrations = storageIntegrations.filter((item) => item.enabled)
  const healthyStorageIntegrations = storageIntegrations.filter(
    (item) => item.enabled && item.healthStatus === 'healthy',
  )
  const storageProviderTypes = [
    ...new Set(storageIntegrations.map((item) => item.providerType.toUpperCase())),
  ]
  const overviewStats = [
    {
      key: 'applications' as const,
      label: '应用目录',
      value: applications.length,
      helper: `${enabledApplications.length} 个已启用 / 最近更新 ${formatDateTime(latestUpdated(applications))}`,
      icon: <AppstoreOutlined />,
      loading: loading.applications,
      tone: enabledApplications.length > 0 ? 'success' : 'default',
    },
    {
      key: 'providers' as const,
      label: 'Provider',
      value: providers.length,
      helper: `${enabledProviders.length} 个已启用 / ${oidcProviders.length} OIDC / ${proxyProviders.length} Proxy / ${samlProviders.length} SAML`,
      icon: <ApiOutlined />,
      loading: loading.providers,
      tone: enabledProviders.length > 0 ? 'success' : 'default',
    },
    {
      key: 'outposts' as const,
      label: 'Outpost',
      value: outposts.length,
      helper: `${onlineOutposts.length} 个在线 / ${
        [
          runtime?.outpost.embeddedRuntime.available && 'Embedded',
          runtime?.outpost.agentRuntime.available && 'Agent',
          runtime?.outpost.kubernetesArtifact.available && 'Kubernetes',
          runtime?.outpost.externalProtocol.available && 'External-compatible',
        ]
          .filter(Boolean)
          .join('、') || '无可用执行端'
      }`,
      icon: <LinkOutlined />,
      loading: loading.outposts,
      tone: onlineOutposts.length > 0 ? 'success' : 'default',
    },
    {
      key: 'sessions' as const,
      label: '活跃会话',
      value: sessions.length,
      helper: '来自统一登录源与 Provider Portal',
      icon: <UserSwitchOutlined />,
      loading: loading.sessions,
      tone: sessions.length > 0 ? 'success' : 'default',
    },
  ] satisfies Array<
    OverviewMetricItem & {
      loading?: boolean
    }
  >
  const protocolStatus = [
    {
      key: 'oidc',
      label: 'OIDC Provider',
      value: oidcProviders.length,
      helper: oidcEnabled ? '已启用' : '未启用',
      icon: <KeyOutlined />,
      tone: oidcEnabled ? 'success' : 'default',
    },
    {
      key: 'saml',
      label: 'SAML Provider',
      value: samlProviders.length,
      helper: runtime?.samlApplicationProvider.available
        ? samlProviders.some((provider) => provider.enabled && provider.status === 'enabled')
          ? '已启用'
          : '运行时可用，未启用'
        : runtime?.samlApplicationProvider.reason || '运行时不可用',
      icon: <SafetyCertificateOutlined />,
      tone: runtime?.samlApplicationProvider.available ? 'success' : 'default',
    },
    {
      key: 'proxy',
      label: 'Proxy Provider',
      value: proxyProviders.length,
      helper: proxyEnabled ? '已启用' : '未启用',
      icon: <ApiOutlined />,
      tone: proxyEnabled ? 'success' : 'default',
    },
  ] satisfies OverviewChipItem[]
  const softwareStatus = [
    {
      key: 'storage-sources',
      label: '存储源',
      value: permissions.storageIntegrations ? storageIntegrations.length : '-',
      helper: permissions.storageIntegrations
        ? storageProviderTypes.join(' / ') || '尚未配置对象存储'
        : '无存储配置查看权限',
      icon: <CloudServerOutlined />,
    },
    {
      key: 'storage-enabled',
      label: '已启用',
      value: permissions.storageIntegrations ? enabledStorageIntegrations.length : '-',
      helper: permissions.storageIntegrations
        ? `${healthyStorageIntegrations.length} 个连接正常`
        : '无存储配置查看权限',
      icon: <CheckCircleOutlined />,
      tone: healthyStorageIntegrations.length > 0 ? 'success' : 'default',
    },
    {
      key: 'software-files',
      label: '文件数量',
      value: softwareStorage?.objectCount ?? '-',
      helper: '全部存储合计',
      icon: <FileOutlined />,
    },
    {
      key: 'software-size',
      label: '已用空间',
      value: softwareStorage ? formatStorageBytes(softwareStorage.totalBytes) : '-',
      helper: '全部软件包占用',
      icon: <DatabaseOutlined />,
    },
  ] satisfies OverviewChipItem[]
  return (
    <div className="soha-page soha-overview-page soha-identity-overview-page">
      {Object.entries(reads).map(([key, query]) => (
        <ReadFeedback<unknown>
          key={key}
          query={query}
          canRead={query.isEnabled}
          label={
            {
              applications: '应用',
              providers: '认证接入',
              outposts: '接入代理',
              sessions: '活跃会话',
              audits: '最近审计',
              softwareStorage: '软件存储',
              storageIntegrations: '存储源',
              runtime: '运行能力',
            }[key as keyof typeof reads]
          }
        />
      ))}
      <div className="soha-overview-metric-grid">
        {overviewStats.map((item) => (
          <OverviewMetricCard
            key={item.key}
            label={item.label}
            value={available(reads[item.key]) ? item.value : '—'}
            helper={available(reads[item.key]) ? item.helper : '当前数据不可用'}
            icon={item.icon}
            tone={item.tone}
            loading={item.loading}
          />
        ))}
      </div>

      <div className="soha-overview-summary-grid">
        <Card
          className="soha-overview-panel-card"
          title="Provider 运行"
          extra={
            <Button
              size="small"
              icon={<KeyOutlined />}
              onClick={() => navigate('/identity/providers')}
            >
              Provider
            </Button>
          }
        >
          {permissions.providers ? (
            <div className="soha-overview-alert-stack">
              <OverviewSectionBar
                title="协议层"
                description="按后端 runtime capability 查看协议支持与启用状态。"
                extra={
                  <Button
                    type="text"
                    icon={<ApiOutlined />}
                    onClick={() => navigate('/identity/providers')}
                  >
                    管理 Provider
                  </Button>
                }
              />
              <div className="soha-overview-chip-grid">
                {protocolStatus.map((item) => (
                  <OverviewChip
                    key={item.key}
                    label={item.label}
                    value={readableQueryData(reads.providers) === undefined ? '—' : item.value}
                    helper={
                      readableQueryData(reads.providers) === undefined ||
                      readableQueryData(reads.runtime) === undefined
                        ? '当前数据不可用'
                        : item.helper
                    }
                    icon={item.icon}
                    tone={item.tone}
                  />
                ))}
              </div>
            </div>
          ) : (
            <ManagementState
              compact
              bordered={false}
              kind="no-permission"
              title="无 Provider 权限"
            />
          )}
        </Card>

        <Card
          className="soha-overview-panel-card soha-identity-overview-audit-card"
          title="最近审计"
          extra={
            <Button size="small" icon={<AuditOutlined />} onClick={() => navigate('/system/audit')}>
              审计
            </Button>
          }
        >
          {!permissions.audit ? (
            <ManagementState compact bordered={false} kind="no-permission" title="无审计权限" />
          ) : readableQueryData(reads.audits) === undefined ? (
            <ManagementState
              compact
              bordered={false}
              kind={reads.audits.isError ? 'error' : 'loading'}
              title="审计数据不可用"
            />
          ) : audits.length === 0 ? (
            <ManagementState compact bordered={false} kind="empty" title="暂无审计记录" />
          ) : (
            <div className="soha-overview-attention-list soha-identity-overview-audit-list">
              {audits.slice(0, 6).map((item) => (
                <div key={item.id} className="soha-overview-attention-row">
                  <div className="soha-overview-attention-main">
                    <Space size={6} wrap>
                      {resultTag(item.result)}
                      <Text strong>{item.action}</Text>
                    </Space>
                    <div className="soha-overview-inline-caption">
                      {item.summary || item.resourceName || item.resourceKind || '-'}
                    </div>
                  </div>
                  <div className="soha-overview-attention-meta">
                    <span>{formatDateTime(item.createdAt)}</span>
                    <span>{item.actorName || item.actorId || '-'}</span>
                  </div>
                </div>
              ))}
            </div>
          )}
        </Card>
      </div>

      <Card
        className="soha-overview-panel-card"
        title="软件库"
        extra={
          <Button
            size="small"
            icon={<AppstoreOutlined />}
            onClick={() => navigate('/internal-workbench/software')}
          >
            软件库
          </Button>
        }
      >
        {!permissions.software ? (
          <ManagementState compact bordered={false} kind="no-permission" title="无软件库权限" />
        ) : softwareError && !softwareStorage ? (
          <ManagementState
            compact
            bordered={false}
            kind="error"
            title="软件库摘要加载失败"
            actions={
              <Button size="small" onClick={refreshAll}>
                重试
              </Button>
            }
          />
        ) : loading.software ? (
          <ManagementState compact bordered={false} kind="loading" />
        ) : (
          <div className="soha-overview-chip-grid">
            {softwareStatus.map((item) => (
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
        )}
      </Card>
    </div>
  )
}
