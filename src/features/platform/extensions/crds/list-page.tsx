import { KubernetesResourceTable } from '@/features/platform/shared/kubernetes-resource-table'
import {
  ManagementDensityButton,
  ManagementIconButton,
  ManagementRefreshButton,
  ManagementState,
  ManagementTableToolbar,
} from '@/components/management-list'
import { hasPermission, usePermissionSnapshot } from '@/features/auth'
import '@/features/platform/extensions/styles.css'
import { useI18n } from '@/i18n'
import { usePlatformScopeStore } from '@/stores/platform-scope-store'
import { readableQueryData } from '@/utils/query-feedback'
import { tableColumnPresets } from '@/utils/table-columns'
import { DeleteOutlined } from '@ant-design/icons'
import { useQuery } from '@tanstack/react-query'
import type { TableColumnsType } from 'antd'
import { Button, Space, Tag, Typography } from 'antd'
import { useDeferredValue, useMemo, useState } from 'react'
import { useNavigate, useSearchParams } from 'react-router-dom'
import { ResourceQueryPanel } from '../shared/resource-query-panel'
import { includesSearch, normalizeSearchKeyword } from '../shared/search'
import { DeleteCRDDefinitionModal } from './delete-definition-modal'
import { buildCRDApiGroupDetailPath } from './paths'
import { crdQueries } from './queries'
import type { CRDApiGroupSummary } from './types'
import { groupCRDsByApi } from './utils'

const { Text } = Typography

export function CRDPage() {
  const { t, localeCode } = useI18n()
  const { clusterId } = usePlatformScopeStore()
  const navigate = useNavigate()
  const permissionSnapshot = usePermissionSnapshot().data?.data
  const canDelete = hasPermission(permissionSnapshot, 'platform.extensions.crds.delete')
  const [deleteTarget, setDeleteTarget] = useState<{ clusterId: string; group: string } | null>(
    null,
  )
  const [params, setParams] = useSearchParams()
  const searchKeyword = params.get('q') ?? ''
  const setSearchKeyword = (keyword: string) =>
    setParams(
      (current) => {
        const next = new URLSearchParams(current)
        if (keyword) next.set('q', keyword)
        else next.delete('q')
        next.delete('page')
        next.delete('pageSize')
        return next
      },
      { replace: true },
    )
  const [tableSize, setTableSize] = useState<'small' | 'middle'>('small')
  const normalizedKeyword = normalizeSearchKeyword(useDeferredValue(searchKeyword))
  const catalogQuery = useQuery(crdQueries.catalog(clusterId))
  const visibleCatalogQueryData = readableQueryData(catalogQuery)
  const apiGroups = useMemo(
    () => groupCRDsByApi(visibleCatalogQueryData ?? []),
    [visibleCatalogQueryData],
  )
  const filteredApiGroups = useMemo(
    () =>
      apiGroups.filter((item) =>
        includesSearch(
          [item.group, ...item.crdNames, ...item.kindNames, ...item.versions],
          normalizedKeyword,
        ),
      ),
    [apiGroups, normalizedKeyword],
  )
  const densityLabel = localeCode === 'zh_CN' ? '切换表格密度' : 'Toggle table density'
  const openGroup = (group: string) => navigate(buildCRDApiGroupDetailPath(group))

  const columns: TableColumnsType<CRDApiGroupSummary> = [
    {
      title: t('page.extensions.crd.apiGroupColumn', 'API Group'),
      dataIndex: 'group',
      width: 220,
      render: (_value, record) => (
        <Button
          type="link"
          className="soha-crd-group-link"
          onClick={(event) => {
            event.stopPropagation()
            openGroup(record.group)
          }}
        >
          <span className="soha-crd-group-card">
            <code className="soha-crd-group-card__value">{record.group}</code>
          </span>
        </Button>
      ),
    },
    {
      title: t('page.extensions.crd.crdNameColumn', 'CRD Names'),
      key: 'crdNames',
      width: 360,
      render: (_value, record) => (
        <div className="soha-crd-name-chip-list">
          {record.crdNames.slice(0, 2).map((value) => (
            <code key={value} className="soha-crd-name-chip">
              {value}
            </code>
          ))}
          {record.crdNames.length > 2 ? (
            <code className="soha-crd-name-chip is-summary">
              {`+${record.crdNames.length - 2} ${localeCode === 'zh_CN' ? '个 CRD' : 'more CRDs'}`}
            </code>
          ) : null}
        </div>
      ),
    },
    {
      title: t('page.extensions.crd.kindCountColumn', 'Kind Count'),
      key: 'kindCount',
      width: 120,
      render: (_value, record) => (
        <Text>{localeCode === 'zh_CN' ? `${record.crdCount} 个` : record.crdCount}</Text>
      ),
    },
    {
      title: t('page.extensions.crd.kindPreviewColumn', 'Served kinds'),
      key: 'kinds',
      width: 360,
      render: (_value, record) => (
        <Space size={[4, 4]} wrap>
          {record.kindNames.slice(0, 6).map((value) => (
            <Tag key={value}>{value}</Tag>
          ))}
          {record.kindNames.length > 6 ? <Tag>{`+${record.kindNames.length - 6}`}</Tag> : null}
        </Space>
      ),
    },
    {
      title: t('page.extensions.crd.versionsColumn', 'Versions'),
      key: 'versions',
      width: 240,
      render: (_value, record) => record.versions.map((value) => <Tag key={value}>{value}</Tag>),
    },
    {
      title: t('page.extensions.crd.scopeColumn', 'Scope mix'),
      key: 'scope',
      width: 220,
      render: (_value, record) => (
        <Space size={[4, 4]} wrap>
          {record.namespacedCount ? (
            <Tag color="gold">Namespaced {record.namespacedCount}</Tag>
          ) : null}
          {record.clusterCount ? <Tag color="blue">Cluster {record.clusterCount}</Tag> : null}
        </Space>
      ),
    },
    {
      ...tableColumnPresets.action,
      title: '',
      key: 'actions',
      render: (_value, record) => (
        <ManagementIconButton
          icon={<DeleteOutlined />}
          danger
          aria-label={localeCode === 'zh_CN' ? '删除 CRD 定义' : 'Delete CRD definition'}
          disabled={
            !clusterId ||
            !record.crds.some(
              (item) => item.uid && !item.deletingAt && item.allowedActions?.includes('delete'),
            )
          }
          tooltip={
            record.crds.some(
              (item) => item.uid && !item.deletingAt && item.allowedActions?.includes('delete'),
            )
              ? localeCode === 'zh_CN'
                ? '删除 CRD 定义'
                : 'Delete CRD definition'
              : localeCode === 'zh_CN'
                ? '需要删除授权和包含 UID 的最新目录；Agent 需启用 platform.crds.delete'
                : 'Requires deletion access and a current catalog with UIDs; enable platform.crds.delete on the agent'
          }
          onClick={(event) => {
            event.stopPropagation()
            if (clusterId) setDeleteTarget({ clusterId, group: record.group })
          }}
        />
      ),
    },
  ]

  return (
    <div className="soha-page">
      <ResourceQueryPanel
        placeholder={
          localeCode === 'zh_CN'
            ? '搜索 API Group / CRD / Kind / Version'
            : 'Search API group / CRD / kind / version'
        }
        searchKeyword={searchKeyword}
        setSearchKeyword={setSearchKeyword}
      />
      <KubernetesResourceTable
        resourceKey={'crds'}
        resourceLabel={'API Groups'}
        allRows={apiGroups}
        filterKey={searchKeyword}
        scopeMode={'cluster'}
        error={catalogQuery.isEnabled === false ? null : catalogQuery.error}
        hasReadData={catalogQuery.data !== undefined}
        onRetry={catalogQuery.isEnabled === false ? undefined : () => catalogQuery.refetch()}
        className="soha-platform-table"
        columnSettingIconOnly
        columnSettingPlacement="header"
        shellClassName="soha-management-table-shell"
        columns={canDelete ? columns : columns.filter((column) => column.key !== 'actions')}
        dataSource={clusterId ? filteredApiGroups : []}
        rowKey="group"
        loading={catalogQuery.isLoading}
        localSorting
        tableSize={tableSize}
        scroll={{ x: 'max-content' }}
        onRow={(record: CRDApiGroupSummary) => ({
          onClick: () => openGroup(record.group),
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
              loading={catalogQuery.isFetching}
              tooltip={t('common.refresh', 'Refresh')}
              onClick={() => clusterId && void catalogQuery.refetch()}
            />
          </ManagementTableToolbar>
        }
        empty={
          <ManagementState
            bordered={false}
            compact
            kind={!clusterId ? 'select-scope' : catalogQuery.isError ? 'error' : 'empty'}
            title={
              !clusterId
                ? t('platformScope.clusterPlaceholder', 'Select cluster')
                : catalogQuery.isError
                  ? localeCode === 'zh_CN'
                    ? 'CRD 目录加载失败'
                    : 'Failed to load the CRD catalog'
                  : t('page.extensions.crd.empty', 'No CRDs in the current cluster.')
            }
          />
        }
      />
      {deleteTarget && deleteTarget.clusterId === clusterId ? (
        <DeleteCRDDefinitionModal
          key={`${deleteTarget.clusterId}/${deleteTarget.group}`}
          clusterId={deleteTarget.clusterId}
          definitions={apiGroups.find((item) => item.group === deleteTarget.group)?.crds ?? []}
          onClose={() => setDeleteTarget(null)}
        />
      ) : null}
    </div>
  )
}
