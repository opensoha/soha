import { ContextFeedback } from '@/components/context-feedback'
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
import { hasPermission, usePermissionSnapshot } from '@/features/auth'
import { queryAccessDenied, readableQueryData } from '@/utils/query-feedback'
import {
  CloudDownloadOutlined,
  ImportOutlined,
  ReloadOutlined,
  SettingOutlined,
} from '@ant-design/icons'
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
import type {
  InstalledPlugin,
  MarketplacePlugin,
  PluginMarketplaceFilters,
  PluginUpdate,
} from '../plugin-model'
import {
  manifestAssetCount,
  manifestCapabilityCount,
  manifestExtensionCount,
  normalizePluginMarketplaceFilters,
  pluginSourceLabel,
  pluginTypeLabel,
  pluginTypeOptions,
} from '../plugin-model'
import { pluginQueries } from '../queries'
import { pluginRiskTag, pluginStatusBadge } from '../shared/formatters'
import { canInstallPlugin, canUpgradePlugins } from '../shared/permissions'
import { useRepositoryUpgradeAction } from '../installed/repository-upgrade'
import '../shared/styles.css'
import { marketplaceDetailPath, useInstallPluginAction } from './install-action'
import { useRepositoryInstallAction } from './repository-install'

const { Text } = Typography
const PAGE_SIZE = 12

const initialInstalledFilters = { query: '', type: '' }
const initialMarketFilters: Required<PluginMarketplaceFilters> = {
  ...initialInstalledFilters,
  publisher: '',
  sourceId: '',
  marketplaceUrl: '',
  version: '',
}
const initialFilters = { market: initialMarketFilters, installed: initialInstalledFilters }

type InstallationFilter = 'all' | 'installed' | 'available'

const installationOptions = [
  { label: '市场', value: 'all' },
  { label: '已安装', value: 'installed' },
  { label: '未安装', value: 'available' },
] as const

function matchesInstalledFilters(plugin: InstalledPlugin, filters: typeof initialInstalledFilters) {
  const query = filters.query?.trim().toLowerCase()
  const type = filters.type?.trim()
  return (
    (!type || plugin.type === type) &&
    (!query ||
      [plugin.id, plugin.name, plugin.publisher].some((value) =>
        value.toLowerCase().includes(query),
      ))
  )
}

function installedDetailPath(pluginId: string) {
  return `/plugins/installed/${encodeURIComponent(pluginId)}`
}

function InstalledPluginCard({
  plugin,
  update,
  upgrade,
  upgradeDisabled,
}: {
  plugin: InstalledPlugin
  update?: PluginUpdate
  upgrade: (plugin: InstalledPlugin, update: PluginUpdate) => void
  upgradeDisabled: boolean
}) {
  return (
    <Card
      size="small"
      classNames={{ root: 'soha-plugin-market-card', body: 'soha-plugin-market-card-body' }}
    >
      <Link
        aria-label={`管理 ${plugin.name}`}
        className="soha-plugin-market-card-link"
        to={installedDetailPath(plugin.id)}
      >
        <div className="soha-plugin-market-card-title-row">
          <MetadataTag label={pluginTypeLabel(plugin.type)} />
          {pluginStatusBadge(plugin.status)}
        </div>
        <div className="soha-plugin-market-card-meta">
          <div className="soha-plugin-market-card-heading">
            <h2 className="soha-plugin-market-card-title" title={plugin.name}>
              {plugin.name}
            </h2>
            <span className="soha-plugin-market-card-detail" aria-hidden="true">
              管理
            </span>
          </div>
          <Text type="secondary" title={`${plugin.publisher} / ${plugin.id}`}>
            {plugin.publisher} / {plugin.id}
          </Text>
        </div>
        <Text className="soha-plugin-market-card-summary">
          {plugin.manifest.description || '暂无简介'}
        </Text>
        <Text type="secondary" className="soha-plugin-market-card-source">
          <span>版本 {plugin.version}</span>
          <span title={plugin.sourceInfo?.canonicalSource || plugin.source}>
            来源 {pluginSourceLabel(plugin.sourceInfo, plugin.source)}
          </span>
        </Text>
      </Link>
      {update ? (
        <div className="soha-plugin-market-card-actions">
          <MetadataTag
            className="soha-plugin-update-version"
            label={`${update.installedVersion} → ${update.targetVersion}`}
          />
          <Button
            size="small"
            aria-label={`升级 ${plugin.name}`}
            disabled={upgradeDisabled}
            onClick={() => upgrade(plugin, update)}
          >
            升级
          </Button>
        </div>
      ) : null}
    </Card>
  )
}

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
            onClick={() => navigate(installedDetailPath(plugin.id))}
          >
            管理
          </Button>
        ) : (
          <Button
            disabled={
              installBlocked ||
              !canInstallPlugin(snapshot) ||
              Boolean(plugin.repositoryUrl && !plugin.sourceId)
            }
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
  const snapshot = usePermissionSnapshot().data?.data
  const [filterForm] = Form.useForm<typeof initialFilters>()
  const [filters, setFilters] = useState(initialFilters)
  const [searchParams, setSearchParams] = useSearchParams()
  const installationParam = searchParams.get('installation')
  const installationFilter =
    installationParam === 'installed' || installationParam === 'available'
      ? installationParam
      : 'all'
  const isInstalledView = installationFilter === 'installed'
  const filterScope = isInstalledView ? 'installed' : 'market'
  const [page, setPage] = useState(1)
  const installAction = useInstallPluginAction()
  const repositoryInstallAction = useRepositoryInstallAction()
  const repositoryUpgrade = useRepositoryUpgradeAction()
  const marketplaceQuery = useQuery(pluginQueries.marketplace(filters.market, !isInstalledView))
  const installedQuery = useQuery(pluginQueries.installed())
  const canReadUpdates = isInstalledView && hasPermission(snapshot, 'plugin.view')
  const activeQuery = isInstalledView ? installedQuery : marketplaceQuery
  const marketplaceResult = readableQueryData(marketplaceQuery)
  const marketplaceData = marketplaceResult?.items
  const unavailableSources =
    marketplaceResult?.meta?.sources?.filter((source) => source.status === 'unavailable') ?? []
  const installedData = readableQueryData(installedQuery)
  const installedById = useMemo(
    () => new Map((installedData ?? []).map((plugin) => [plugin.id, plugin])),
    [installedData],
  )
  const marketResults =
    installationFilter === 'available'
      ? marketplaceData?.filter((plugin) => !plugin.installed)
      : marketplaceData
  const installedResults = useMemo(
    () => installedData?.filter((plugin) => matchesInstalledFilters(plugin, filters.installed)),
    [installedData, filters.installed],
  )
  const results = isInstalledView ? installedResults : marketResults
  const hasFilters = Object.keys(normalizePluginMarketplaceFilters(filters[filterScope])).length > 0
  const maxPage = Math.max(1, Math.ceil((results?.length ?? 0) / PAGE_SIZE))
  const currentPage = Math.min(page, maxPage)
  const pageStart = (currentPage - 1) * PAGE_SIZE
  const pageEnd = currentPage * PAGE_SIZE
  const updateIds =
    installedResults
      ?.slice(pageStart, pageEnd)
      .filter((item) => item.sourceInfo?.kind === 'github-repo' || item.sourceInfo?.repositoryUrl)
      .map((item) => item.id) ?? []
  const updatesQuery = useQuery(pluginQueries.updates(updateIds, canReadUpdates))
  const updates = canReadUpdates ? readableQueryData(updatesQuery) : undefined
  const updatesById = useMemo(
    () => new Map(updates?.map((item) => [item.pluginId, item])),
    [updates],
  )

  useEffect(() => setPage(1), [filters, installationFilter])

  function changeView(nextView: InstallationFilter) {
    const nextSearchParams = new URLSearchParams(searchParams)
    if (nextView === 'all') nextSearchParams.delete('installation')
    else nextSearchParams.set('installation', nextView)
    setSearchParams(nextSearchParams)
  }

  function refreshVisiblePlugins() {
    if (isInstalledView) {
      return void Promise.all([
        installedQuery.refetch(),
        ...(canReadUpdates ? [updatesQuery.refetch()] : []),
      ])
    }
    void Promise.all([marketplaceQuery.refetch(), installedQuery.refetch()])
  }

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
                  loading={activeQuery.isFetching || (canReadUpdates && updatesQuery.isFetching)}
                  onClick={refreshVisiblePlugins}
                />
              </Tooltip>
              <ManagementQueryActions
                disabledReset={!hasFilters}
                loading={activeQuery.isFetching}
                onReset={() => {
                  filterForm.setFieldsValue({ [filterScope]: initialFilters[filterScope] })
                  setFilters((previous) => ({
                    ...previous,
                    [filterScope]: initialFilters[filterScope],
                  }))
                }}
              />
            </>
          }
          form={filterForm}
          initialValues={initialFilters}
          moreLabel="更多筛选"
          lessLabel="收起筛选"
          onFinish={(values) => {
            if (isInstalledView) {
              setFilters((previous) => ({
                ...previous,
                installed: { ...initialInstalledFilters, ...values.installed },
              }))
            } else {
              setFilters((previous) => ({
                ...previous,
                market: { ...initialMarketFilters, ...values.market },
              }))
            }
          }}
        >
          <ManagementKeywordField
            label="关键词"
            minWidth={300}
            name={[filterScope, 'query']}
            placeholder="搜索 ID、名称、发布者"
            width={300}
          />
          <ManagementQueryField
            label="类型"
            minWidth={180}
            name={[filterScope, 'type']}
            width={180}
          >
            <Select
              allowClear
              placeholder="全部类型"
              options={[{ label: '全部类型', value: '' }, ...pluginTypeOptions]}
            />
          </ManagementQueryField>
          {!isInstalledView && (
            <>
              <ManagementQueryField
                label="发布者"
                minWidth={220}
                name={['market', 'publisher']}
                width={220}
              >
                <Input allowClear placeholder="发布者" />
              </ManagementQueryField>
              <ManagementQueryField
                label="来源 ID"
                minWidth={220}
                name={['market', 'sourceId']}
                width={220}
              >
                <Input allowClear placeholder="来源 ID" />
              </ManagementQueryField>
              <ManagementQueryField
                label="市场目录地址"
                minWidth={300}
                name={['market', 'marketplaceUrl']}
                width={300}
              >
                <Input allowClear placeholder="市场目录地址" />
              </ManagementQueryField>
              <ManagementQueryField
                label="版本"
                minWidth={160}
                name={['market', 'version']}
                width={160}
              >
                <Input allowClear placeholder="版本" />
              </ManagementQueryField>
            </>
          )}
        </ManagementQueryPanel>
      </div>

      <div className="soha-plugin-market-filters">
        <ManagementTableToolbar>
          <Space wrap>
            <Segmented<InstallationFilter>
              aria-label="插件列表视图"
              options={[...installationOptions]}
              value={installationFilter}
              onChange={changeView}
            />
            <Tooltip
              title={
                canInstallPlugin(snapshot) ? undefined : '缺少 plugin.install，无法预览仓库安装'
              }
            >
              <Button
                icon={<ImportOutlined />}
                disabled={!canInstallPlugin(snapshot) || repositoryInstallAction.blocked}
                onClick={repositoryInstallAction.open}
              >
                从仓库安装
              </Button>
            </Tooltip>
            {!isInstalledView && hasPermission(snapshot, 'settings.runtime-config.view') && (
              <Link to="/settings/runtime-configuration">管理市场来源</Link>
            )}
          </Space>
        </ManagementTableToolbar>
        <Text type="secondary" aria-live="polite">
          {results
            ? `${results.length} 个插件${activeQuery.isError ? '（未刷新）' : ''}`
            : activeQuery.isLoading
              ? '正在读取插件数量'
              : '插件数量暂不可用'}
        </Text>
      </div>

      {!isInstalledView && <ReadFeedback query={marketplaceQuery} label="插件市场" />}
      {!isInstalledView && unavailableSources.length > 0 && (
        <ContextFeedback
          type="warning"
          showIcon
          title={`部分市场来源暂不可用：${unavailableSources.map((source) => source.sourceId).join('、')}`}
          description={
            <>
              可继续使用已加载的插件。公开 HTTPS 来源列表位于运行配置的
              plugins.marketplace.third_party_urls。
            </>
          }
        />
      )}
      <ReadFeedback query={installedQuery} label="已安装插件" />
      {canReadUpdates ? <ReadFeedback query={updatesQuery} label="插件更新" /> : null}
      {isInstalledView ? repositoryUpgrade.feedback : null}
      {repositoryUpgrade.dialog}
      {!isInstalledView && !queryAccessDenied(marketplaceQuery) ? installAction.feedback : null}
      {!isInstalledView && !queryAccessDenied(marketplaceQuery) ? installAction.dialog : null}
      {repositoryInstallAction.dialog}
      {!isInstalledView && marketplaceQuery.isError && !queryAccessDenied(marketplaceQuery) && (
        <div>
          <Button onClick={() => changeView('installed')}>查看已安装插件</Button>
        </div>
      )}
      {activeQuery.isLoading ? (
        <div
          className="soha-plugin-market-grid"
          role="status"
          aria-label={isInstalledView ? '正在加载已安装插件' : '正在加载插件市场'}
        >
          {Array.from({ length: 6 }, (_, index) => (
            <Card key={index} loading className="soha-plugin-market-card" />
          ))}
        </div>
      ) : results?.length ? (
        <>
          <div className="soha-plugin-market-grid">
            {isInstalledView
              ? installedResults?.slice(pageStart, pageEnd).map((plugin) => {
                  const update = updatesById.get(plugin.id)
                  return (
                    <InstalledPluginCard
                      key={plugin.id}
                      plugin={plugin}
                      update={update?.installedVersion === plugin.version ? update : undefined}
                      upgrade={repositoryUpgrade.open}
                      upgradeDisabled={
                        repositoryUpgrade.blocked ||
                        !canUpgradePlugins(snapshot) ||
                        !canInstallPlugin(snapshot)
                      }
                    />
                  )
                })
              : marketResults
                  ?.slice(pageStart, pageEnd)
                  .map((plugin) => (
                    <PluginMarketplaceCard
                      key={`${plugin.sourceId || 'static'}:${plugin.id}:${plugin.version}`}
                      installedPlugin={installedById.get(plugin.id)}
                      installLoading={installAction.loading || repositoryInstallAction.loading}
                      installBlocked={
                        installAction.blocked || repositoryInstallAction.blocked || !installedData
                      }
                      plugin={plugin}
                      onInstall={(item) =>
                        item.repositoryUrl
                          ? repositoryInstallAction.openMarketplace(
                              item.repositoryUrl,
                              item.id,
                              item.sourceId,
                            )
                          : installAction.confirmInstall(item)
                      }
                    />
                  ))}
          </div>
          <Pagination
            align="end"
            current={currentPage}
            hideOnSinglePage
            pageSize={PAGE_SIZE}
            showSizeChanger={false}
            total={results.length}
            onChange={setPage}
          />
        </>
      ) : results && !activeQuery.isError ? (
        <ManagementState
          kind="empty"
          title={
            hasFilters || installationFilter === 'available'
              ? '没有匹配插件'
              : isInstalledView
                ? '尚未安装插件'
                : '暂无市场插件'
          }
          description={
            hasFilters || installationFilter === 'available'
              ? '调整筛选条件后重试。'
              : isInstalledView
                ? '安装完成后，可在此查看和管理插件。'
                : '市场目录当前没有可浏览的插件。'
          }
        />
      ) : null}
    </div>
  )
}
