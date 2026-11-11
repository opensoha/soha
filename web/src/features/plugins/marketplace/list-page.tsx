import {
  ManagementKeywordField,
  ManagementQueryActions,
  ManagementQueryField,
  ManagementQueryPanel,
  ManagementState,
  ManagementTableToolbar,
} from '@/components/management-list'
import { ReadFeedback } from '@/components/request-feedback'
import { BooleanTag, MetadataTag } from '@/components/status-tag'
import { usePermissionSnapshot } from '@/features/auth'
import { queryAccessDenied, readableQueryData } from '@/utils/query-feedback'
import { CloudDownloadOutlined, ReloadOutlined, SettingOutlined } from '@ant-design/icons'
import { useQuery } from '@tanstack/react-query'
import {
  Button,
  Card,
  Form,
  Input,
  Pagination,
  Segmented,
  Select,
  Space,
  Tooltip,
  Typography,
} from 'antd'
import { useEffect, useMemo, useState } from 'react'
import { Link, useNavigate, useSearchParams } from 'react-router-dom'
import type { InstalledPlugin, MarketplacePlugin, PluginMarketplaceFilters } from '../plugin-model'
import {
  manifestAssetCount,
  manifestCapabilityCount,
  manifestExtensionCount,
  pluginTypeLabel,
  pluginTypeOptions,
} from '../plugin-model'
import { pluginQueries } from '../queries'
import { pluginRiskTag, pluginStatusBadge } from '../shared/formatters'
import { canInstallPlugin } from '../shared/permissions'
import '../shared/styles.css'
import { marketplaceDetailPath, useInstallPluginAction } from './install-action'

const { Text } = Typography
const PAGE_SIZE = 12

const initialFilters: Required<PluginMarketplaceFilters> = {
  query: '',
  type: '',
  publisher: '',
  sourceId: '',
  marketplaceUrl: '',
  version: '',
}

type InstallationFilter = 'all' | 'installed' | 'available'

const installationOptions = [
  { label: '全部', value: 'all' },
  { label: '已安装', value: 'installed' },
  { label: '未安装', value: 'available' },
] as const

function PluginMarketplaceCard({
  installedPlugin,
  installLoading,
  installBlocked,
  plugin,
  onInstall,
}: {
  installedPlugin?: InstalledPlugin
  installLoading: boolean
  installBlocked: boolean
  plugin: MarketplacePlugin
  onInstall: (plugin: MarketplacePlugin) => void
}) {
  const navigate = useNavigate()
  const snapshot = usePermissionSnapshot().data?.data
  const detailPath = marketplaceDetailPath(plugin)

  return (
    <Card
      size="small"
      classNames={{ root: 'soha-plugin-market-card', body: 'soha-plugin-market-card-body' }}
    >
      <Link
        aria-label={`查看 ${plugin.name} 详情`}
        className="soha-plugin-market-card-link"
        to={detailPath}
      >
        <div className="soha-plugin-market-card-title-row">
          <MetadataTag label={pluginTypeLabel(plugin.type)} />
          {installedPlugin ? (
            pluginStatusBadge(installedPlugin.status)
          ) : plugin.installed ? (
            <BooleanTag value trueLabel="已安装" />
          ) : (
            <Text type="secondary">未安装</Text>
          )}
        </div>
        <div className="soha-plugin-market-card-meta">
          <div className="soha-plugin-market-card-heading">
            <h2 className="soha-plugin-market-card-title" title={plugin.name}>
              {plugin.name}
            </h2>
            <span className="soha-plugin-market-card-detail" aria-hidden="true">
              查看详情
            </span>
          </div>
          <Text type="secondary" title={`${plugin.publisher} / ${plugin.id}`}>
            {plugin.publisher} / {plugin.id}
          </Text>
        </div>
        <Text className="soha-plugin-market-card-summary">
          {plugin.summary || plugin.manifest.description || '暂无简介'}
        </Text>
        <Space size={[4, 4]} wrap>
          <BooleanTag value={Boolean(plugin.verified)} trueLabel="已验证" falseLabel="未验证" />
          {pluginRiskTag(plugin.riskLevel)}
        </Space>
        <div className="soha-plugin-market-card-metrics">
          <div>
            <strong>{manifestAssetCount(plugin.manifest)}</strong>
            <Text type="secondary">资产</Text>
          </div>
          <div>
            <strong>{manifestCapabilityCount(plugin.manifest)}</strong>
            <Text type="secondary">能力</Text>
          </div>
          <div>
            <strong>{manifestExtensionCount(plugin.manifest)}</strong>
            <Text type="secondary">扩展点</Text>
          </div>
        </div>
      </Link>
      <div className="soha-plugin-market-card-actions">
        <Text type="secondary" className="soha-plugin-market-card-source">
          <span>版本 {plugin.latestVersion || plugin.version}</span>
          <span title={plugin.sourceId || 'static'}>来源 {plugin.sourceId || 'static'}</span>
        </Text>
        {plugin.installed ? (
          <Button
            icon={<SettingOutlined />}
            onClick={() => navigate(`/plugins/installed/${encodeURIComponent(plugin.id)}`)}
          >
            管理
          </Button>
        ) : (
          <Button
            disabled={installBlocked || !canInstallPlugin(snapshot)}
            icon={<CloudDownloadOutlined />}
            loading={installLoading}
            onClick={() => onInstall(plugin)}
          >
            安装
          </Button>
        )}
      </div>
    </Card>
  )
}

export function PluginMarketplacePage() {
  const [filterForm] = Form.useForm<Required<PluginMarketplaceFilters>>()
  const [filters, setFilters] = useState(initialFilters)
  const [searchParams] = useSearchParams()
  const [installationFilter, setInstallationFilter] = useState<InstallationFilter>(() =>
    searchParams.get('installation') === 'installed' ? 'installed' : 'all',
  )
  const [page, setPage] = useState(1)
  const installAction = useInstallPluginAction()
  const marketplaceQuery = useQuery(pluginQueries.marketplace(filters))
  const installedQuery = useQuery(pluginQueries.installed())
  const marketplaceData = readableQueryData(marketplaceQuery)
  const installedData = readableQueryData(installedQuery)
  const installedById = useMemo(
    () => new Map((installedData ?? []).map((plugin) => [plugin.id, plugin])),
    [installedData],
  )
  const marketplacePlugins = useMemo(
    () =>
      (marketplaceData ?? []).filter((plugin) => {
        if (installationFilter === 'installed') return plugin.installed
        if (installationFilter === 'available') return !plugin.installed
        return true
      }),
    [installationFilter, marketplaceData],
  )
  const isLoading =
    marketplaceQuery.isLoading ||
    marketplaceQuery.isFetching ||
    installedQuery.isLoading ||
    installedQuery.isFetching
  const maxPage = Math.max(1, Math.ceil(marketplacePlugins.length / PAGE_SIZE))
  const currentPage = Math.min(page, maxPage)
  const visiblePlugins = marketplacePlugins.slice(
    (currentPage - 1) * PAGE_SIZE,
    currentPage * PAGE_SIZE,
  )

  useEffect(() => setPage(1), [filters, installationFilter])

  return (
    <div className="soha-page soha-plugin-page">
      <h1 className="soha-plugin-market-page-title">插件市场</h1>

      <div className="soha-plugin-market-query">
        <ManagementQueryPanel
          actions={
            <>
              <Tooltip title="刷新插件列表">
                <Button
                  aria-label="刷新插件列表"
                  icon={<ReloadOutlined />}
                  loading={isLoading}
                  onClick={() =>
                    Promise.all([marketplaceQuery.refetch(), installedQuery.refetch()])
                  }
                />
              </Tooltip>
              <ManagementQueryActions
                disabledReset={Object.values(filters).every((value) => !value)}
                loading={isLoading}
                onReset={() => {
                  filterForm.resetFields()
                  setFilters(initialFilters)
                }}
              />
            </>
          }
          form={filterForm}
          initialValues={initialFilters}
          moreLabel="更多筛选"
          lessLabel="收起筛选"
          onFinish={(values) => setFilters({ ...initialFilters, ...values })}
        >
          <ManagementKeywordField
            label="关键词"
            minWidth={300}
            name="query"
            placeholder="搜索 ID、名称、发布者"
            width={300}
          />
          <ManagementQueryField label="类型" minWidth={180} name="type" width={180}>
            <Select
              allowClear
              placeholder="全部类型"
              options={[{ label: '全部类型', value: '' }, ...pluginTypeOptions]}
            />
          </ManagementQueryField>
          <ManagementQueryField label="发布者" minWidth={220} name="publisher" width={220}>
            <Input allowClear placeholder="发布者" />
          </ManagementQueryField>
          <ManagementQueryField label="来源 ID" minWidth={220} name="sourceId" width={220}>
            <Input allowClear placeholder="来源 ID" />
          </ManagementQueryField>
          <ManagementQueryField
            label="市场目录地址"
            minWidth={300}
            name="marketplaceUrl"
            width={300}
          >
            <Input allowClear placeholder="市场目录地址" />
          </ManagementQueryField>
          <ManagementQueryField label="版本" minWidth={160} name="version" width={160}>
            <Input allowClear placeholder="版本" />
          </ManagementQueryField>
        </ManagementQueryPanel>
      </div>

      <div className="soha-plugin-market-filters">
        <ManagementTableToolbar>
          <Segmented<InstallationFilter>
            options={[...installationOptions]}
            value={installationFilter}
            onChange={setInstallationFilter}
          />
        </ManagementTableToolbar>
        <Text type="secondary" aria-live="polite">
          {marketplaceData ? `${marketplacePlugins.length} 个插件` : '插件数量暂不可用'}
        </Text>
      </div>

      <ReadFeedback query={marketplaceQuery} label="插件市场" />
      <ReadFeedback query={installedQuery} label="安装记录" />
      {!queryAccessDenied(marketplaceQuery) ? installAction.feedback : null}
      {!queryAccessDenied(marketplaceQuery) ? installAction.dialog : null}
      {marketplaceQuery.isError && !marketplaceData ? null : marketplaceQuery.isLoading ? (
        <div className="soha-plugin-market-grid" aria-label="正在加载插件">
          {Array.from({ length: 6 }, (_, index) => (
            <Card key={index} loading className="soha-plugin-market-card" />
          ))}
        </div>
      ) : visiblePlugins.length ? (
        <>
          <div className="soha-plugin-market-grid">
            {visiblePlugins.map((plugin) => (
              <PluginMarketplaceCard
                key={`${plugin.sourceId || 'static'}:${plugin.id}:${plugin.version}`}
                installedPlugin={installedById.get(plugin.id)}
                installLoading={installAction.loading}
                installBlocked={
                  installAction.blocked || !installedData || queryAccessDenied(marketplaceQuery)
                }
                plugin={plugin}
                onInstall={installAction.confirmInstall}
              />
            ))}
          </div>
          <Pagination
            align="end"
            current={currentPage}
            hideOnSinglePage
            pageSize={PAGE_SIZE}
            showSizeChanger={false}
            total={marketplacePlugins.length}
            onChange={setPage}
          />
        </>
      ) : (
        <ManagementState
          kind="empty"
          title={installationFilter === 'installed' ? '尚未安装插件' : '没有匹配插件'}
          description={
            installationFilter === 'installed'
              ? '从插件市场完成安装后，可直接在此筛选和管理。'
              : '调整筛选条件后重试。'
          }
        />
      )}
    </div>
  )
}
