import { ManagementState } from '@/components/management-list'
import {
  OverviewChip,
  OverviewHeroCard,
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
      <div className="soha-overview-bento-top">
        <OverviewHeroCard
          variant="identity"
          eyebrowIcon={<SafetyCertificateOutlined />}
          eyebrowLabel={overviewStats[0].label}
          value={available(reads.applications) ? applications.length : '—'}
          valueSuffix="个受管 SSO 应用"
          caption={available(reads.applications) ? overviewStats[0].helper : '当前数据不可用'}
          loading={overviewStats[0].loading}
          kpis={[
            {
              key: 'outposts',
              label: '在线 Outpost',
              value: available(reads.outposts) ? `${onlineOutposts.length}/${outposts.length}` : '—',
            },
            {
              key: 'sessions',
              label: '活跃会话',
              value: available(reads.sessions) ? sessions.length : '—',
            },
          ]}
          telemetryNodes={[
            {
              id: 'trust-core',
              label: `SSO-APPS · ${applications.length}`,
              title: '企业统一门户应用目录',
              metric: `${enabledApplications.length} 个应用已启用`,
              status: 'primary',
              path: '/identity/applications',
            },
            {
              id: 'oidc-idp',
              label: `PROVIDERS · ${providers.length}`,
              title: 'OIDC / SAML / Proxy 身份源',
              metric: `${enabledProviders.length} 个认证协议运行中`,
              status: 'healthy',
              path: '/identity/providers',
            },
            {
              id: 'ztna-outpost',
              label: `OUTPOST · ${onlineOutposts.length}/${outposts.length}`,
              title: '边缘零信任代理节点',
              metric:
                onlineOutposts.length < outposts.length ? '存在离线 Outpost' : '全部接入端在线',
              status: onlineOutposts.length < outposts.length ? 'warning' : 'healthy',
              path: '/identity/outposts',
            },
            {
              id: 'sessions-live',
              label: `SESSIONS · ${sessions.length}`,
              title: '当前活跃身份会话',
              metric: `${sessions.length} 条已签发在线会话`,
              status: 'healthy',
              path: '/system/audit',
            },
          ]}
          embeddedPods={overviewStats.slice(1).map((item) => ({
            key: item.key,
            label: item.label,
            value: available(reads[item.key]) ? item.value : '—',
            helper: available(reads[item.key]) ? item.helper : '当前数据不可用',
            icon: item.icon,
            tone: item.tone,
            loading: item.loading,
          }))}
          segments={[
            {
              key: 'enabled',
              label: '已启用应用',
              value: enabledApplications.length,
              tone: 'success',
            },
            {
              key: 'oidc',
              label: 'OIDC',
              value: oidcProviders.length,
              tone: 'primary',
            },
            {
              key: 'saml',
              label: 'SAML',
              value: samlProviders.length,
              tone: 'neutral',
            },
            {
              key: 'proxy',
              label: 'Proxy',
              value: proxyProviders.length,
              tone: 'neutral',
            },
          ]}
          ctaLabel="管理应用目录"
          ctaTo="/identity/applications"
          detailDrawerTitle="身份审计与软件存储明细"
          graphicDeck={[
            {
              key: 'identity-ztna-rings',
              kind: 'rings',
              title: '零信任门户与代理在线环',
              subtitle: 'SSO 应用 · 认证源 · 边缘 Outpost',
              actionLabel: '身份源',
              actionTo: '/identity/providers',
              centerValue:
                applications.length > 0
                  ? `${Math.round((enabledApplications.length / applications.length) * 100)}%`
                  : '100%',
              centerLabel: '应用启用',
              rings: [
                {
                  key: 'apps-enabled',
                  label: '已启用门户应用',
                  value: `${enabledApplications.length}/${applications.length}`,
                  ratio:
                    applications.length > 0 ? enabledApplications.length / applications.length : 1,
                  tone: 'success',
                  to: '/identity/applications',
                },
                {
                  key: 'providers-enabled',
                  label: '活跃认证协议',
                  value: `${enabledProviders.length}/${providers.length}`,
                  ratio: providers.length > 0 ? enabledProviders.length / providers.length : 1,
                  tone: 'primary',
                  to: '/identity/providers',
                },
                {
                  key: 'outposts-online',
                  label: '在线 Outpost',
                  value: `${onlineOutposts.length}/${outposts.length}`,
                  ratio: outposts.length > 0 ? onlineOutposts.length / outposts.length : 1,
                  tone: onlineOutposts.length < outposts.length ? 'warning' : 'success',
                  to: '/identity/outposts',
                },
                {
                  key: 'storage-healthy',
                  label: '健康对象存储源',
                  value: `${healthyStorageIntegrations.length}/${storageIntegrations.length}`,
                  ratio:
                    storageIntegrations.length > 0
                      ? healthyStorageIntegrations.length / storageIntegrations.length
                      : 1,
                  tone: 'primary',
                  to: '/internal-workbench/software',
                },
              ],
            },
            {
              key: 'identity-protocol-spectrum',
              kind: 'spectrum',
              title: '多协议认证与分发资产频谱',
              subtitle: 'OIDC · SAML · Proxy · 软件包仓',
              actionLabel: '软件库',
              actionTo: '/internal-workbench/software',
              bars: [
                {
                  key: 'oidc',
                  label: 'OIDC',
                  value: oidcProviders.length,
                  tone: 'primary',
                  to: '/identity/providers',
                },
                {
                  key: 'saml',
                  label: 'SAML',
                  value: samlProviders.length,
                  tone: 'primary',
                },
                {
                  key: 'proxy',
                  label: 'Proxy',
                  value: proxyProviders.length,
                  tone: 'success',
                },
                {
                  key: 'sessions',
                  label: '在线会话',
                  value: sessions.length,
                  tone: 'success',
                },
                {
                  key: 'pkg-files',
                  label: '软件包',
                  value: softwareStorage?.objectCount ?? 0,
                  tone: 'primary',
                  to: '/internal-workbench/software',
                },
              ],
            },
            {
              key: 'identity-audit-radar',
              kind: 'radar',
              title: '安全审计与访问轨迹雷达',
              subtitle: `最近采集 ${audits.length} 条审计`,
              actionLabel: '审计中心',
              actionTo: '/system/audit',
              emptyLabel: '零异常越权审计事件',
              blips: audits.slice(0, 3).map((item) => ({
                key: `audit-${item.id}`,
                label: item.action || item.summary || item.id,
                badge: (item.result || 'OK').toUpperCase(),
                tone:
                  item.result === 'denied' || item.result === 'failed'
                    ? ('danger' as const)
                    : ('primary' as const),
                to: '/system/audit',
              })),
            },
          ]}
        />
      </div>

      <div className="soha-overview-summary-grid">
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

        <Card
          className="soha-overview-panel-card soha-identity-overview-software-card"
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
    </div>
  )
}
