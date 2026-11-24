import { ManagementState } from '@/components/management-list'
import { ReadFeedback, useInlineApiError } from '@/components/request-feedback'
import { BooleanTag } from '@/components/status-tag'
import { usePermissionSnapshot } from '@/features/auth'
import { isApiError } from '@/services/api-error'
import { queryAccessDenied, readableQueryData } from '@/utils/query-feedback'
import { CloudDownloadOutlined, ReloadOutlined, SettingOutlined } from '@ant-design/icons'
import { useQuery } from '@tanstack/react-query'
import { Button, Card, Descriptions, Space } from 'antd'
import { useMemo, useState } from 'react'
import { useNavigate, useParams, useSearchParams } from 'react-router-dom'
import {
  manifestAssetCount,
  manifestExtensionCount,
  pluginRequiresSignedRelease,
} from '../plugin-model'
import { pluginQueries } from '../queries'
import { compactPluginTags, pluginRiskTag } from '../shared/formatters'
import { PluginManifestSections } from '../shared/manifest-sections'
import { PluginPageShell } from '../shared/page-shell'
import { canInstallPlugin, canTrustPluginPublisher } from '../shared/permissions'
import { PluginNameCell } from '../shared/plugin-name-cell'
import { RepositoryRefs } from '../shared/repository-refs'
import { useInstallPluginAction } from './install-action'
import { useRepositoryInstallAction } from './repository-install'

export function PluginMarketplaceDetailPage() {
  const { pluginId = '' } = useParams()
  const [searchParams] = useSearchParams()
  const navigate = useNavigate()
  const snapshot = usePermissionSnapshot().data?.data
  const installAction = useInstallPluginAction()
  const repositoryInstallAction = useRepositoryInstallAction(canTrustPluginPublisher(snapshot))
  const [historyRepository, setHistoryRepository] = useState<string>()
  const filters = useMemo(
    () => ({
      sourceId: searchParams.get('sourceId') ?? '',
      marketplaceUrl: searchParams.get('marketplaceUrl') ?? '',
      version: searchParams.get('version') ?? '',
    }),
    [searchParams],
  )
  const detailQuery = useQuery(pluginQueries.marketplaceDetail(pluginId, filters))
  const plugin = readableQueryData(detailQuery)
  const notFound = isApiError(detailQuery.error) && detailQuery.error.status === 404
  useInlineApiError(notFound ? detailQuery.error : null)

  if (detailQuery.isLoading) {
    return (
      <PluginPageShell>
        <ManagementState kind="loading" />
      </PluginPageShell>
    )
  }
  if (!plugin) {
    return (
      <PluginPageShell>
        {!queryAccessDenied(detailQuery) ? installAction.feedback : null}
        {notFound || !detailQuery.isError ? (
          <ManagementState kind="not-found" />
        ) : (
          <ReadFeedback query={detailQuery} label="插件详情" />
        )}
      </PluginPageShell>
    )
  }

  const { repositoryUrl } = plugin
  const signedRelease = pluginRequiresSignedRelease(plugin.manifest)
  const requiresRepository = signedRelease && !repositoryUrl
  const allowInstall =
    canInstallPlugin(snapshot) &&
    !requiresRepository &&
    (!repositoryUrl || Boolean(plugin.sourceId))
  const installBlocked = installAction.blocked || repositoryInstallAction.blocked
  const showHistory = Boolean(repositoryUrl && historyRepository === repositoryUrl)

  function startRepositoryInstall(ref?: string) {
    if (plugin && repositoryUrl && allowInstall && !installBlocked) {
      repositoryInstallAction.openMarketplace(repositoryUrl, plugin.id, plugin.sourceId, ref)
    }
  }

  return (
    <PluginPageShell>
      <ReadFeedback query={detailQuery} label="插件详情" />
      {installAction.feedback}
      {installAction.dialog}
      {repositoryInstallAction.dialog}
      <Card size="small" className="soha-plugin-identity">
        <div className="soha-plugin-detail-heading">
          <PluginNameCell
            id={plugin.id}
            name={plugin.name}
            publisher={plugin.publisher}
            type={plugin.type}
            version={plugin.version}
            description={plugin.summary || plugin.manifest.description}
          />
          <Space wrap className="soha-plugin-detail-actions">
            <Button onClick={() => navigate('/plugins/marketplace')}>返回市场</Button>
            {repositoryUrl ? (
              <Button
                aria-expanded={showHistory}
                onClick={() => setHistoryRepository(showHistory ? undefined : repositoryUrl)}
              >
                {showHistory ? '收起发布历史' : '查看发布历史'}
              </Button>
            ) : null}
            <Button
              aria-label="刷新插件详情"
              icon={<ReloadOutlined />}
              loading={detailQuery.isFetching}
              onClick={() => void detailQuery.refetch()}
            />
            {plugin.installed ? (
              <Button
                icon={<SettingOutlined />}
                type="primary"
                onClick={() => navigate(`/plugins/installed/${encodeURIComponent(plugin.id)}`)}
              >
                管理插件
              </Button>
            ) : (
              <Button
                disabled={installBlocked || !allowInstall}
                title={requiresRepository ? '可执行插件需要经过仓库签名 Release 预览' : undefined}
                icon={<CloudDownloadOutlined />}
                loading={installAction.loading || repositoryInstallAction.loading}
                type="primary"
                onClick={() =>
                  repositoryUrl ? startRepositoryInstall() : installAction.confirmInstall(plugin)
                }
              >
                安装
              </Button>
            )}
          </Space>
        </div>
        <dl className="soha-plugin-facts">
          <div>
            <dt>风险等级</dt>
            <dd>{pluginRiskTag(plugin.riskLevel)}</dd>
          </div>
          <div>
            <dt>资产文件</dt>
            <dd>{manifestAssetCount(plugin.manifest)}</dd>
          </div>
          <div>
            <dt>扩展点</dt>
            <dd>{manifestExtensionCount(plugin.manifest)}</dd>
          </div>
          <div>
            <dt>已安装</dt>
            <dd>
              <BooleanTag value={Boolean(plugin.installed)} />
            </dd>
          </div>
        </dl>
      </Card>
      {showHistory && repositoryUrl ? (
        <Card title={<h2 className="soha-plugin-section-title">发布历史</h2>} size="small">
          <RepositoryRefs
            key={repositoryUrl}
            kind="release"
            repositoryUrl={repositoryUrl}
            disabled={!allowInstall || Boolean(plugin.installed) || installBlocked}
            onSelect={startRepositoryInstall}
          />
        </Card>
      ) : null}
      <PluginManifestSections manifest={plugin.manifest} />
      <Card title={<h2 className="soha-plugin-section-title">市场来源</h2>} size="small">
        <Descriptions
          column={{ xs: 1, sm: 2, lg: 3 }}
          layout="vertical"
          size="small"
          items={[
            { key: 'source', label: '来源', children: plugin.source },
            { key: 'sourceId', label: '来源 ID', children: plugin.sourceId || '-' },
            { key: 'latest', label: '最新版本', children: plugin.latestVersion || '-' },
            {
              key: 'sourceUrl',
              label: '目录地址',
              span: { xs: 1, sm: 2, lg: 3 },
              children: plugin.sourceUrl || '-',
            },
            {
              key: 'verified',
              label: '目录验证',
              children: <BooleanTag value={Boolean(plugin.verified)} />,
            },
            ...(signedRelease
              ? [
                  {
                    key: 'publisherSignature',
                    label: '发布者签名',
                    children: plugin.installed ? '查看已安装凭据' : '在仓库预览中核验',
                  },
                ]
              : []),
            {
              key: 'categories',
              label: '分类',
              children: compactPluginTags(plugin.categories, plugin.categories?.length ?? 0),
            },
          ]}
        />
      </Card>
    </PluginPageShell>
  )
}
