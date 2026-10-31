import { KubernetesResourceActionsMenu } from '@/features/platform/shared/kubernetes-resource-actions-menu'
import { AdminTable } from '@/components/admin-table'
import {
  ManagementDensityButton,
  ManagementRefreshButton,
  ManagementState,
  ManagementTableToolbar,
} from '@/components/management-list'
import { hasAllowedAction } from '@/features/auth'
import { useClusterCapability } from '@/features/platform/cluster-capabilities'
import '@/features/platform/extensions/styles.css'
import { K8S_TABLE_PAGE_SIZE } from '@/features/platform/shared/table-config'
import { useI18n } from '@/i18n'
import { usePlatformScopeStore } from '@/stores/platform-scope-store'
import { readableQueryData } from '@/utils/query-feedback'
import { CloudDownloadOutlined, LinkOutlined, RocketOutlined } from '@ant-design/icons'
import { useQuery } from '@tanstack/react-query'
import type { TableColumnsType } from 'antd'
import { Alert, Space, Spin, Tag, Typography } from 'antd'
import { lazy, Suspense, useDeferredValue, useEffect, useMemo, useState } from 'react'
import { ResourceQueryPanel } from '../../shared/resource-query-panel'
import { normalizeSearchKeyword } from '../../shared/search'
import { helmQueries } from '../queries'
import type { HelmChart, HelmChartCatalogInput } from '../types'
import { formatHelmChartCount, getHelmChartBadges, hasHelmChartSecuritySummary } from './utils'

const { Text } = Typography
const MAX_PAGE_SIZE = 60
const PAGE_SIZE_OPTIONS = [K8S_TABLE_PAGE_SIZE, 30, MAX_PAGE_SIZE]

const HelmChartDrawer = lazy(async () => {
  const module = await import('./chart-drawer')
  return { default: module.HelmChartDrawer }
})

export function HelmChartsPage() {
  const { t, localeCode } = useI18n()
  const { clusterId } = usePlatformScopeStore()
  const capability = useClusterCapability('helm.releases', localeCode)
  const [searchKeyword, setSearchKeyword] = useState('')
  const [page, setPage] = useState(1)
  const [pageSize, setPageSize] = useState(K8S_TABLE_PAGE_SIZE)
  const [tableSize, setTableSize] = useState<'small' | 'middle'>('small')
  const [selectedChart, setSelectedChart] = useState<HelmChart | null>(null)
  const [initialDrawerTab, setInitialDrawerTab] = useState<'overview' | 'install'>('overview')
  const normalizedKeyword = normalizeSearchKeyword(useDeferredValue(searchKeyword))
  const offset = (page - 1) * pageSize
  const catalogInput: HelmChartCatalogInput | null = clusterId
    ? { clusterId, keyword: normalizedKeyword, limit: pageSize, offset }
    : null
  const chartsQuery = useQuery(helmQueries.chartCatalog(catalogInput))
  const visibleChartsQueryData = readableQueryData(chartsQuery)
  const catalog = visibleChartsQueryData
  const rawItems = catalog?.charts ?? []
  const total = catalog?.totalCount ?? catalog?.chartCount ?? rawItems.length
  const loaded = catalog?.loadedCount ?? catalog?.chartCount ?? rawItems.length
  const currentOffset = catalog?.offset ?? offset
  const rangeStart = loaded > 0 ? currentOffset + 1 : 0
  const rangeEnd = loaded > 0 ? currentOffset + loaded : 0
  const formattedTotal = formatHelmChartCount(total, localeCode)
  const mutationsDisabled = capability.status !== 'available'
  const capabilityReason = mutationsDisabled ? capability.reason : ''
  const densityLabel = localeCode === 'zh_CN' ? '切换表格密度' : 'Toggle table density'

  useEffect(() => setPage(1), [clusterId, normalizedKeyword])

  const openDrawer = (chart: HelmChart, tab: 'overview' | 'install' = 'overview') => {
    setInitialDrawerTab(tab)
    setSelectedChart(chart)
  }

  const columns: TableColumnsType<HelmChart> = useMemo(
    () => [
      {
        title: 'Chart',
        dataIndex: 'name',
        width: 300,
        render: (value: string, record: HelmChart) => (
          <Space size={10} align="start">
            {record.logoImageUrl ? (
              <img
                src={record.logoImageUrl}
                alt=""
                style={{ width: 32, height: 32, borderRadius: 4, objectFit: 'contain' }}
              />
            ) : null}
            <Space orientation="vertical" size={2}>
              <Space size={6} wrap>
                <Text strong>{value}</Text>
                {getHelmChartBadges(record)
                  .slice(0, 3)
                  .map((badge) => (
                    <Tag key={badge.label} color={badge.color}>
                      {badge.label}
                    </Tag>
                  ))}
              </Space>
              <Text type="secondary" style={{ fontSize: 12 }}>
                {record.repositoryDisplay || record.repositoryName || '-'}
              </Text>
            </Space>
          </Space>
        ),
      },
      {
        title: localeCode === 'zh_CN' ? '仓库' : 'Repository',
        dataIndex: 'repositoryName',
        width: 180,
      },
      {
        title: localeCode === 'zh_CN' ? '最新版本' : 'Latest Version',
        dataIndex: 'latestVersion',
        width: 140,
      },
      { title: 'App Version', dataIndex: 'appVersion', width: 140 },
      {
        title: localeCode === 'zh_CN' ? '描述' : 'Description',
        dataIndex: 'description',
        width: 420,
        render: (value?: string) => (value ? <Text type="secondary">{value}</Text> : '-'),
      },
      {
        title: localeCode === 'zh_CN' ? '关键词' : 'Keywords',
        dataIndex: 'keywords',
        width: 240,
        render: (values?: string[]) =>
          values?.length ? (
            <Space size={[4, 4]} wrap>
              {values.slice(0, 4).map((value) => (
                <Tag key={value}>{value}</Tag>
              ))}
            </Space>
          ) : (
            '-'
          ),
      },
      {
        title: localeCode === 'zh_CN' ? '状态' : 'Signals',
        key: 'signals',
        width: 180,
        render: (_value, record) => (
          <Space size={[4, 4]} wrap>
            {typeof record.stars === 'number' ? <Tag>Stars {record.stars}</Tag> : null}
            {record.hasValuesSchema ? <Tag color="processing">values.schema</Tag> : null}
            {hasHelmChartSecuritySummary(record) ? (
              <Tag
                color={
                  (record.securityCritical ?? 0) > 0 || (record.securityHigh ?? 0) > 0
                    ? 'error'
                    : 'default'
                }
              >
                CVEs {(record.securityCritical ?? 0) + (record.securityHigh ?? 0)}
              </Tag>
            ) : null}
          </Space>
        ),
      },
      { title: localeCode === 'zh_CN' ? '版本数' : 'Versions', dataIndex: 'versionCount' },
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
              ...(hasAllowedAction(record.allowedActions, 'create')
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
    ],
    [capability, localeCode, mutationsDisabled],
  )

  const emptyTitle = !clusterId
    ? t('platformScope.clusterPlaceholder', 'Select cluster')
    : chartsQuery.isError
      ? t('page.extensions.helmCharts.errorTitle', 'Chart catalog unavailable')
      : normalizedKeyword
        ? localeCode === 'zh_CN'
          ? '没有匹配的 Helm Chart'
          : 'No matching Helm charts'
        : t('page.extensions.helmCharts.emptyTitle', 'No Helm charts')

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
        toolbar={
          catalog?.repository ? (
            <Space className="soha-helm-chart-catalog-toolbar" size={8}>
              <Tag color="processing">Artifact Hub</Tag>
              <Tag>{localeCode === 'zh_CN' ? '仅 Helm packages' : 'Helm packages only'}</Tag>
              <Text className="soha-helm-chart-catalog-url" type="secondary">
                {catalog.repository.url}
              </Text>
              <Text className="soha-helm-chart-catalog-total" type="secondary">
                {localeCode === 'zh_CN' ? `总计 ${formattedTotal} 个` : `${formattedTotal} total`}
              </Text>
            </Space>
          ) : null
        }
        tableSize={tableSize}
        scroll={{ x: 'max-content' }}
        viewportScroll
        onRow={(record: HelmChart) => ({
          onClick: () => openDrawer(record),
          style: { cursor: 'pointer' },
        })}
        headerExtra={
          <ManagementTableToolbar>
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
      {selectedChart ? (
        <Suspense fallback={<Spin />}>
          <HelmChartDrawer
            chart={selectedChart}
            initialTab={initialDrawerTab}
            onClose={() => setSelectedChart(null)}
          />
        </Suspense>
      ) : null}
    </div>
  )
}
