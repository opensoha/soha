import { KubernetesResourceActionsMenu } from '@/features/platform/shared/kubernetes-resource-actions-menu'
import { AdminTable } from '@/components/admin-table'
import { MetadataTag, StatusTag } from '@/components/status-tag'
import { TableCellLink, TableCellText } from '@/components/table-cell-content'
import {
  ManagementDensityButton,
  ManagementRefreshButton,
  ManagementState,
  ManagementTableToolbar,
} from '@/components/management-list'
import { hasAllowedAction, hasPermission, usePermissionSnapshot } from '@/features/auth'
import { useClusterCapability } from '@/features/platform/cluster-capabilities'
import '@/features/platform/extensions/styles.css'
import { K8S_TABLE_PAGE_SIZE } from '@/features/platform/shared/table-config'
import { useI18n } from '@/i18n'
import { usePlatformScopeStore } from '@/stores/platform-scope-store'
import { readableQueryData } from '@/utils/query-feedback'
import {
  CloudDownloadOutlined,
  InfoCircleOutlined,
  LinkOutlined,
  RocketOutlined,
} from '@ant-design/icons'
import { useQuery } from '@tanstack/react-query'
import type { TableColumnsType } from 'antd'
import { Alert, Button, Space, Spin, Tooltip, Typography } from 'antd'
import { lazy, Suspense, useDeferredValue, useEffect, useState } from 'react'
import { ResourceQueryPanel } from '../../shared/resource-query-panel'
import { normalizeSearchKeyword } from '../../shared/search'
import { helmQueries } from '../queries'
import type { HelmChart, HelmChartCatalogInput } from '../types'
import { formatHelmChartCount, getHelmChartBadges, hasHelmChartSecuritySummary } from './utils'

const { Text } = Typography
const MAX_PAGE_SIZE = 60
const PAGE_SIZE_OPTIONS = [K8S_TABLE_PAGE_SIZE, 30, MAX_PAGE_SIZE]
const OPTIONAL_COLUMN_KEYS = ['keywords', 'signals', 'stars', 'versionCount']

const HelmChartDrawer = lazy(async () => {
  const module = await import('./chart-drawer')
  return { default: module.HelmChartDrawer }
})

export function HelmChartsPage() {
  const { t, localeCode } = useI18n()
  const { clusterId } = usePlatformScopeStore()
  const permissionSnapshot = readableQueryData(usePermissionSnapshot())?.data
  const canView = hasPermission(permissionSnapshot, 'platform.helm.view')
  const canCreate = hasPermission(permissionSnapshot, 'platform.helm.releases.create')
  const capability = useClusterCapability('helm.releases', localeCode)
  const [searchKeyword, setSearchKeyword] = useState('')
  const [page, setPage] = useState(1)
  const [pageSize, setPageSize] = useState(K8S_TABLE_PAGE_SIZE)
  const [tableSize, setTableSize] = useState<'small' | 'middle'>('small')
  const [selection, setSelection] = useState<{
    chart: HelmChart
    clusterId: string
    initialTab: 'overview' | 'install'
  } | null>(null)
  const normalizedKeyword = normalizeSearchKeyword(useDeferredValue(searchKeyword))
  const offset = (page - 1) * pageSize
  const catalogInput: HelmChartCatalogInput | null = clusterId
    ? { clusterId, keyword: normalizedKeyword, limit: pageSize, offset }
    : null
  const catalogOptions = helmQueries.chartCatalog(catalogInput)
  const chartsQuery = useQuery({
    ...catalogOptions,
    enabled: Boolean(catalogOptions.enabled) && canView,
  })
  const visibleChartsQueryData = readableQueryData(chartsQuery)
  const catalog = canView ? visibleChartsQueryData : undefined
  const rawItems = catalog?.charts ?? []
  const total = catalog?.totalCount ?? catalog?.chartCount ?? 0
  const loaded = catalog?.loadedCount ?? rawItems.length
  const currentOffset = catalog?.offset ?? offset
  const rangeStart = loaded > 0 ? currentOffset + 1 : 0
  const rangeEnd = loaded > 0 ? currentOffset + loaded : 0
  const formattedTotal = formatHelmChartCount(total, localeCode)
  const mutationsDisabled = capability.status !== 'available'
  const capabilityReason = mutationsDisabled ? capability.reason : ''
  const densityLabel = localeCode === 'zh_CN' ? '切换表格密度' : 'Toggle table density'

  useEffect(() => setPage(1), [clusterId, normalizedKeyword])
  useEffect(() => setSelection(null), [clusterId])

  const openDrawer = (chart: HelmChart, tab: 'overview' | 'install' = 'overview') => {
    if (!clusterId || !canView) return
    if (
      tab === 'install' &&
      (!canCreate || mutationsDisabled || !hasAllowedAction(chart.allowedActions, 'create'))
    )
      return
    setSelection({ chart, clusterId, initialTab: tab })
  }

  const columns: TableColumnsType<HelmChart> = [
    {
      title: 'Chart',
      key: 'name',
      dataIndex: 'name',
      width: 320,
      render: (value: string, record) => (
        <div style={{ display: 'flex', alignItems: 'center', gap: 10, minWidth: 0 }}>
          {record.logoImageUrl ? (
            <img
              src={record.logoImageUrl}
              alt=""
              loading="lazy"
              referrerPolicy="no-referrer"
              style={{ width: 32, height: 32, objectFit: 'contain', flexShrink: 0 }}
            />
          ) : null}
          <div style={{ minWidth: 0, flex: 1 }}>
            <TableCellLink label={value} onClick={() => openDrawer(record)} />
            {getHelmChartBadges(record).length ? (
              <div style={{ display: 'flex', gap: 4, flexWrap: 'wrap', marginTop: 4 }}>
                {getHelmChartBadges(record).map((badge) =>
                  badge.label === 'Deprecated' ? (
                    <StatusTag key={badge.label} value="warning" label={badge.label} />
                  ) : badge.label === 'Verified' ? (
                    <StatusTag key={badge.label} value="verified" label={badge.label} />
                  ) : (
                    <MetadataTag
                      key={badge.label}
                      label={badge.label}
                      tone={
                        badge.label === 'Signed'
                          ? 'purple'
                          : badge.label === 'CNCF'
                            ? 'cyan'
                            : 'blue'
                      }
                    />
                  ),
                )}
              </div>
            ) : null}
          </div>
        </div>
      ),
    },
    {
      title: localeCode === 'zh_CN' ? '仓库' : 'Repository',
      key: 'repositoryName',
      dataIndex: 'repositoryName',
      width: 180,
      render: (value?: string, record?: HelmChart) => (
        <TableCellText value={record?.repositoryDisplay || value} />
      ),
    },
    {
      title: localeCode === 'zh_CN' ? 'Chart 版本' : 'Chart version',
      key: 'latestVersion',
      dataIndex: 'latestVersion',
      width: 140,
      render: (value?: string) => <TableCellText value={value} />,
    },
    {
      title: 'App Version',
      key: 'appVersion',
      dataIndex: 'appVersion',
      width: 140,
      render: (value?: string) => <TableCellText value={value} />,
    },
    {
      title: localeCode === 'zh_CN' ? '描述' : 'Description',
      key: 'description',
      dataIndex: 'description',
      width: 420,
      render: (value?: string) => <TableCellText value={value} />,
    },
    {
      title: localeCode === 'zh_CN' ? '关键词' : 'Keywords',
      key: 'keywords',
      dataIndex: 'keywords',
      width: 240,
      render: (values?: string[]) => <TableCellText value={values?.join(', ')} />,
    },
    {
      title: localeCode === 'zh_CN' ? '状态' : 'Signals',
      key: 'signals',
      width: 180,
      render: (_value, record) => (
        <Space size={[4, 4]} wrap>
          {getHelmChartBadges(record).map((badge) =>
            badge.label === 'Deprecated' ? (
              <StatusTag key={badge.label} value="warning" label={badge.label} />
            ) : (
              <MetadataTag key={badge.label} label={badge.label} />
            ),
          )}
          {record.hasValuesSchema ? <MetadataTag label="values.schema" /> : null}
          {hasHelmChartSecuritySummary(record) ? (
            <StatusTag
              value={
                (record.securityCritical ?? 0) > 0 || (record.securityHigh ?? 0) > 0
                  ? 'error'
                  : 'default'
              }
              label={`CVEs ${(record.securityCritical ?? 0) + (record.securityHigh ?? 0)}`}
            />
          ) : null}
        </Space>
      ),
    },
    { title: 'Stars', key: 'stars', dataIndex: 'stars', width: 100 },
    {
      title: localeCode === 'zh_CN' ? '版本数' : 'Versions',
      key: 'versionCount',
      dataIndex: 'versionCount',
      width: 100,
    },
    {
      title: '',
      key: 'actions',
      width: 64,
      fixed: 'right',
      align: 'center',
      render: (_value, record) => (
        <KubernetesResourceActionsMenu
          resourceLabel="Helm Chart"
          name={record.name}
          localeCode={localeCode}
          items={[
            {
              key: 'view',
              label: localeCode === 'zh_CN' ? '查看详情' : 'View details',
              icon: <RocketOutlined aria-hidden />,
            },
            ...(record.artifactHubUrl
              ? [
                  {
                    key: 'hub',
                    label: (
                      <a href={record.artifactHubUrl} target="_blank" rel="noreferrer">
                        Artifact Hub
                      </a>
                    ),
                    icon: <LinkOutlined aria-hidden />,
                  },
                ]
              : []),
            ...(canCreate && hasAllowedAction(record.allowedActions, 'create')
              ? [
                  {
                    key: 'install',
                    label: mutationsDisabled ? (
                      <span>
                        {localeCode === 'zh_CN' ? '安装 Chart' : 'Install Chart'}
                        <span className="soha-kubernetes-action-menu-reason">
                          {capabilityReason}
                        </span>
                      </span>
                    ) : localeCode === 'zh_CN' ? (
                      '安装 Chart'
                    ) : (
                      'Install Chart'
                    ),
                    disabled: mutationsDisabled,
                    icon: <CloudDownloadOutlined aria-hidden />,
                  },
                ]
              : []),
          ]}
          onSelect={(key) => {
            if (key === 'view') openDrawer(record)
            else if (key === 'install') openDrawer(record, 'install')
          }}
        />
      ),
    },
  ]

  const emptyTitle = !clusterId
    ? t('platformScope.clusterPlaceholder', 'Select cluster')
    : chartsQuery.isError
      ? t('page.extensions.helmCharts.errorTitle', 'Chart catalog unavailable')
      : normalizedKeyword
        ? localeCode === 'zh_CN'
          ? '没有匹配的 Helm Chart'
          : 'No matching Helm charts'
        : t('page.extensions.helmCharts.emptyTitle', 'No Helm charts')

  if (!canView) {
    return (
      <ManagementState
        kind="no-permission"
        title={
          localeCode === 'zh_CN' ? '没有 Helm Chart 查看权限' : 'No permission to view Helm charts'
        }
      />
    )
  }

  return (
    <div className="soha-page">
      <ResourceQueryPanel
        placeholder={
          localeCode === 'zh_CN'
            ? '搜索 Chart / 版本 / 描述 / 关键词 / 维护者'
            : 'Search chart / version / description / keyword / maintainer'
        }
        searchKeyword={searchKeyword}
        setSearchKeyword={(value) => {
          setSearchKeyword(value)
          setPage(1)
        }}
      />
      {capabilityReason ? (
        <Alert
          showIcon
          type="warning"
          style={{ marginBottom: 12 }}
          title={localeCode === 'zh_CN' ? '当前连接模式限制 Helm 安装' : 'Helm installs limited'}
          description={capabilityReason}
        />
      ) : null}
      <AdminTable
        error={chartsQuery.isEnabled === false ? null : chartsQuery.error}
        hasReadData={chartsQuery.data !== undefined}
        onRetry={chartsQuery.isEnabled === false ? undefined : () => chartsQuery.refetch()}
        title={
          <span className="soha-kubernetes-resource-count" role="status">
            {!clusterId
              ? localeCode === 'zh_CN'
                ? '请选择集群'
                : 'Select a cluster'
              : catalog === undefined
                ? chartsQuery.isLoading
                  ? localeCode === 'zh_CN'
                    ? '正在加载 Charts'
                    : 'Loading Charts'
                  : localeCode === 'zh_CN'
                    ? 'Charts 未加载'
                    : 'Charts not loaded'
                : localeCode === 'zh_CN'
                  ? `${formattedTotal} 个 Charts`
                  : `${formattedTotal} Charts`}
          </span>
        }
        className="soha-platform-table"
        columnSettingIconOnly
        columnSettingPlacement="header"
        columnLayoutKey="helm-chart-catalog"
        defaultHiddenColumnKeys={OPTIONAL_COLUMN_KEYS}
        shellClassName="soha-management-table-shell soha-helm-chart-table-shell"
        columns={columns}
        dataSource={clusterId && !chartsQuery.isError ? rawItems : []}
        rowKey={(record) =>
          record.packageId || `${record.repositoryName}:${record.name}:${record.latestVersion}`
        }
        loading={chartsQuery.isLoading}
        pagination={{
          current: page,
          currentPage: page,
          pageSize,
          pageSizeOptions: PAGE_SIZE_OPTIONS,
          showQuickJumper: false,
          total,
          onPageChange: setPage,
          onPageSizeChange: (nextPageSize: number) => {
            setPage(1)
            setPageSize(Math.min(nextPageSize, MAX_PAGE_SIZE))
          },
        }}
        paginationSummary={
          <Text className="soha-workload-table-summary" type="secondary">
            {localeCode === 'zh_CN'
              ? `当前 ${rangeStart}-${rangeEnd} / 总计 ${formattedTotal} 条`
              : `${rangeStart}-${rangeEnd} / ${formattedTotal} total`}
          </Text>
        }
        tableSize={tableSize}
        scroll={{ x: 'max-content' }}
        viewportScroll
        headerExtra={
          <ManagementTableToolbar>
            <Tooltip
              trigger={['hover', 'focus', 'click']}
              title={
                <Space orientation="vertical" size={4}>
                  <span>
                    {localeCode === 'zh_CN'
                      ? '来自 Artifact Hub 的 Helm packages，搜索与分页由服务端处理。'
                      : 'Helm packages from Artifact Hub. Search and pagination are handled by the server.'}
                  </span>
                  {catalog?.repository.url ? <span>{catalog.repository.url}</span> : null}
                </Space>
              }
            >
              <Button
                type="text"
                icon={<InfoCircleOutlined />}
                aria-label={
                  localeCode === 'zh_CN' ? 'Chart 目录来源与范围' : 'Chart catalog source and scope'
                }
              />
            </Tooltip>
            <ManagementDensityButton
              aria-label={densityLabel}
              title={densityLabel}
              tooltip={densityLabel}
              onClick={() => setTableSize((current) => (current === 'middle' ? 'small' : 'middle'))}
            />
            <ManagementRefreshButton
              aria-label={t('common.refresh', 'Refresh')}
              disabled={!clusterId}
              loading={chartsQuery.isFetching}
              tooltip={t('common.refresh', 'Refresh')}
              onClick={() => clusterId && void chartsQuery.refetch()}
            />
          </ManagementTableToolbar>
        }
        empty={
          <ManagementState
            bordered={false}
            compact
            kind={!clusterId ? 'select-scope' : chartsQuery.isError ? 'error' : 'empty'}
            title={emptyTitle}
            description={chartsQuery.error?.message}
          />
        }
      />
      {selection && selection.clusterId === clusterId ? (
        <Suspense fallback={<Spin />}>
          <HelmChartDrawer
            key={JSON.stringify([
              selection.clusterId,
              selection.chart.packageId,
              selection.chart.repositoryName,
              selection.chart.name,
              selection.initialTab,
            ])}
            chart={selection.chart}
            initialTab={selection.initialTab}
            onClose={() => setSelection(null)}
          />
        </Suspense>
      ) : null}
    </div>
  )
}
