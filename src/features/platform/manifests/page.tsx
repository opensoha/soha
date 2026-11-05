import { KubernetesResourceActionsMenu } from '@/features/platform/shared/kubernetes-resource-actions-menu'
import { NamespaceScopeTag } from '@/features/platform/shared/namespace-scope-tag'
import { ReadFeedback } from '@/components/request-feedback'
import { ManagementDataPage } from '@/components/management-data-page'
import {
  ManagementDensityButton,
  ManagementKeywordField,
  ManagementQueryPanel,
  ManagementState,
  ManagementTableToolbar,
} from '@/components/management-list'
import { MetadataTag, StatusTag } from '@/components/status-tag'
import { manifestQueries, deliveryQueries } from '@/features/delivery'
import { K8S_TABLE_PAGE_SIZE } from '@/features/platform/shared/table-config'
import { useI18n } from '@/i18n'
import { usePlatformScopeStore } from '@/stores/platform-scope-store'
import { readableQueryData } from '@/utils/query-feedback'
import { formatDateTime } from '@/utils/time'
import { CodeOutlined, ExportOutlined } from '@ant-design/icons'
import { useQueries, useQuery } from '@tanstack/react-query'
import type { TableColumnsType } from 'antd'
import { Button, Pagination, Space, Typography } from 'antd'
import { useEffect, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import './styles.css'

import { ManifestRuntimeDrawer } from './runtime-drawer'
import { runtimeRowKey, type ManifestRuntimeRow } from './runtime-model'
import { hasPermission, usePermissionSnapshot } from '@/features/auth'
import { WorkloadRefresh, useWorkloadRefresh } from '../workloads/shared/workload-refresh'
import { clusterQueries } from '../clusters/queries'
const { Text } = Typography

export function PlatformManifestsPage() {
  const { localeCode, t } = useI18n()
  const navigate = useNavigate()
  const { clusterId, namespace } = usePlatformScopeStore()
  const [selectedKey, setSelectedKey] = useState('')
  const [selectedTab, setSelectedTab] = useState<'overview' | 'desired'>('overview')
  const [search, setSearch] = useState('')
  const [draftSearch, setDraftSearch] = useState('')
  const refresh = useWorkloadRefresh()
  const permissions = readableQueryData(usePermissionSnapshot())?.data
  const canRead = hasPermission(permissions, 'delivery.applications.view')
  const canReadClusters = hasPermission(permissions, 'platform.clusters.view')
  const clustersQuery = useQuery(clusterQueries.list(canReadClusters))
  const clusters = canReadClusters ? (readableQueryData(clustersQuery) ?? []) : []
  const [tableSize, setTableSize] = useState<'small' | 'middle'>('small')
  const [pagination, setPagination] = useState({ page: 1, pageSize: K8S_TABLE_PAGE_SIZE })

  useEffect(() => {
    setPagination((current) => ({ ...current, page: 1 }))
    setSelectedKey('')
  }, [clusterId, namespace])
  const manifestsQuery = useQuery({
    ...manifestQueries.list(
      {
        clusterId: clusterId || undefined,
        namespace: namespace || undefined,
        search,
        page: pagination.page,
        pageSize: pagination.pageSize,
      },
      Boolean(clusterId && canRead),
    ),
    refetchInterval: refresh.interval,
  })
  const applicationsQuery = useQuery(deliveryQueries.applications.list(canRead))
  const applications = readableQueryData(applicationsQuery) ?? []
  const applicationNames = new Map(applications.map((app) => [app.id, app.name]))
  const visibleManifestsQueryData = canRead ? readableQueryData(manifestsQuery) : undefined

  const manifestItems = canRead ? (visibleManifestsQueryData?.items ?? []) : []
  const deploymentQueries = useQueries({
    queries: manifestItems.map((item) => ({
      ...manifestQueries.deployments(item.id, Boolean(clusterId && canRead)),
      refetchInterval: refresh.interval,
    })),
  })
  const deploymentsByBinding = new Map(
    deploymentQueries
      .flatMap((query) => readableQueryData(query) ?? [])
      .map((item) => [item.bindingId, item]),
  )
  const scopedBindings: ManifestRuntimeRow[] = manifestItems.flatMap((item, index) =>
    item.bindings
      .filter((binding) => !clusterId || binding.clusterId === clusterId)
      .filter((binding) => !namespace || binding.namespace === namespace)
      .map((binding) => ({
        ...binding,
        manifest: item,
        deployment: binding.id ? deploymentsByBinding.get(binding.id) : undefined,
        readState: deploymentQueries[index]?.isError
          ? 'error'
          : deploymentQueries[index]?.isPending
            ? 'loading'
            : readableQueryData(deploymentQueries[index])
              ? 'ready'
              : 'unknown',
        readError: deploymentQueries[index]?.error?.message,
      })),
  )
  const selected = scopedBindings.find((item) => runtimeRowKey(item) === selectedKey)
  const selectedDeploymentQuery = selected
    ? deploymentQueries[manifestItems.findIndex((item) => item.id === selected.manifest.id)]
    : undefined

  const columns: TableColumnsType<ManifestRuntimeRow> = [
    {
      title: t('common.manifest', '应用清单'),
      dataIndex: ['manifest', 'name'],
      width: 280,
      fixed: 'left',
      render: (_value, item) => (
        <Button
          type="link"
          className="soha-platform-manifest-link"
          onClick={() => {
            setSelectedTab('overview')
            setSelectedKey(runtimeRowKey(item))
          }}
        >
          {item.manifest.name}
        </Button>
      ),
    },
    {
      title: t('common.application', '应用'),
      key: 'source',
      width: 200,
      ellipsis: true,
      render: (_value, item) => (
        <Space size={4} wrap>
          <Text title={item.manifest.applicationId}>
            {applicationNames.get(item.manifest.applicationId) || item.manifest.applicationId}
          </Text>
          <MetadataTag
            label={item.manifest.serviceId ? `服务 ${item.manifest.serviceId}` : '应用级'}
          />
        </Space>
      ),
    },
    {
      title: t('common.environment', '环境'),
      dataIndex: 'environmentKey',
      width: 110,
      render: (value) => <MetadataTag label={value} />,
    },
    {
      title: t('common.namespace', '命名空间'),
      dataIndex: 'namespace',
      width: 180,
      render: (value: string) => <NamespaceScopeTag namespace={value} />,
    },
    {
      title: t('common.version', '版本'),
      key: 'revision',
      width: 150,
      render: (_value, item) => (
        <Space size={8}>
          <Text>{`期望 v${item.deployment?.spec.desiredRevision ?? item.manifest.currentRevision}`}</Text>
          <Text type="secondary">{`实际 ${item.deployment?.status.appliedRevision ? `v${item.deployment.status.appliedRevision}` : '-'}`}</Text>
        </Space>
      ),
    },
    {
      title: t('common.runtimeStatus', '运行状态'),
      key: 'status',
      width: 120,
      render: (_value, item) => (
        <StatusTag
          value={
            item.readState === 'error' || item.readState === 'loading'
              ? 'unknown'
              : item.deployment?.status.phase || item.status || 'not_deployed'
          }
        />
      ),
    },
    {
      title: t('common.diff', '差异'),
      key: 'diff',
      width: 100,
      render: (_value, item) => {
        const resources = item.deployment?.status.drift?.resources.length
        if (resources === undefined)
          return <Text type="secondary">{localeCode === 'zh_CN' ? '未观测' : 'Not observed'}</Text>
        return <Text type={resources > 0 ? 'warning' : 'secondary'}>{`${resources} 个资源`}</Text>
      },
    },
    {
      title: t('common.updatedAt', '更新时间'),
      dataIndex: ['manifest', 'updatedAt'],
      width: 180,
      render: formatDateTime,
    },
    {
      title: '',
      key: 'actions',
      fixed: 'right',
      width: 64,
      align: 'center',
      render: (_value, item) => (
        <KubernetesResourceActionsMenu
          resourceLabel={localeCode === 'zh_CN' ? '应用清单' : 'Manifest'}
          name={item.manifest.name}
          localeCode={localeCode}
          items={[
            {
              key: 'yaml',
              label: t('common.viewYaml', '查看 YAML'),
              icon: <CodeOutlined aria-hidden />,
            },
            {
              key: 'library',
              label: t('common.openManifestLibrary', '查看扩展资源'),
              icon: <ExportOutlined aria-hidden />,
            },
          ]}
          onSelect={(key) => {
            if (key === 'yaml') {
              setSelectedTab('desired')
              setSelectedKey(runtimeRowKey(item))
            } else if (key === 'library')
              navigate(
                `/applications/${encodeURIComponent(item.manifest.applicationId)}?tab=resources`,
              )
          }}
        />
      ),
    },
  ]

  return (
    <>
      <ManagementDataPage
        children={
          <>
            {deploymentQueries.map((query, index) => (
              <ReadFeedback
                key={manifestItems[index].id}
                canRead={query.isEnabled && selected?.manifest.id !== manifestItems[index].id}
                query={query}
                label={`清单 ${manifestItems[index].name} 的部署记录`}
              />
            ))}
          </>
        }
        className="soha-platform-manifests"
        beforeQuery={
          <>
            <h1 className="soha-platform-manifests-heading">
              {localeCode === 'zh_CN' ? '应用清单' : 'Manifests'}
            </h1>
            <ManagementQueryPanel
              onFinish={() => {
                setSearch(draftSearch.trim())
                setSelectedKey('')
                setPagination((p) => ({ ...p, page: 1 }))
              }}
              actions={
                <>
                  <Button
                    autoInsertSpace={false}
                    onClick={() => {
                      setDraftSearch('')
                      setSearch('')
                      setSelectedKey('')
                      setPagination((p) => ({ ...p, page: 1 }))
                    }}
                  >
                    {localeCode === 'zh_CN' ? '重置' : 'Reset'}
                  </Button>
                  <Button autoInsertSpace={false} type="primary" htmlType="submit">
                    {localeCode === 'zh_CN' ? '查询' : 'Search'}
                  </Button>
                </>
              }
            >
              <ManagementKeywordField
                label={localeCode === 'zh_CN' ? '关键词' : 'Keyword'}
                placeholder={localeCode === 'zh_CN' ? '搜索清单名称或描述' : 'Search manifests'}
                value={draftSearch}
                onChange={setDraftSearch}
              />
            </ManagementQueryPanel>
          </>
        }
        table={{
          title: (
            <span className="soha-kubernetes-resource-count" role="status">
              {!clusterId
                ? localeCode === 'zh_CN'
                  ? '请选择集群'
                  : 'Select a cluster'
                : visibleManifestsQueryData === undefined
                  ? manifestsQuery.isLoading
                    ? localeCode === 'zh_CN'
                      ? '正在加载清单'
                      : 'Loading manifests'
                    : localeCode === 'zh_CN'
                      ? '清单未加载'
                      : 'Manifests not loaded'
                  : localeCode === 'zh_CN'
                    ? `${visibleManifestsQueryData.total} 份清单`
                    : `${visibleManifestsQueryData.total} manifests`}
            </span>
          ),
          error: manifestsQuery.isEnabled === false ? null : manifestsQuery.error,
          hasReadData: manifestsQuery.data !== undefined,
          onRetry: manifestsQuery.isEnabled === false ? undefined : () => manifestsQuery.refetch(),
          headerExtra: (
            <ManagementTableToolbar>
              <ManagementDensityButton
                aria-label={localeCode === 'zh_CN' ? '切换表格密度' : 'Toggle table density'}
                tooltip={
                  tableSize === 'small'
                    ? localeCode === 'zh_CN'
                      ? '切换为舒展密度'
                      : 'Use comfortable density'
                    : localeCode === 'zh_CN'
                      ? '切换为紧凑密度'
                      : 'Use compact density'
                }
                onClick={() =>
                  setTableSize((current) => (current === 'small' ? 'middle' : 'small'))
                }
              />
              <WorkloadRefresh
                compact
                disabled={!clusterId || !canRead}
                settings={refresh}
                queries={[manifestsQuery, ...deploymentQueries]}
                resourceLabel={localeCode === 'zh_CN' ? '应用清单' : 'Manifests'}
                onRefresh={() => {
                  void manifestsQuery.refetch()
                  deploymentQueries.forEach((q) => void q.refetch())
                }}
              />
            </ManagementTableToolbar>
          ),
          columnSettingIconOnly: true,
          columnSettingPlacement: 'header',
          columns,
          dataSource: scopedBindings,
          loading: manifestsQuery.isLoading || deploymentQueries.some((query) => query.isLoading),
          rowKey: runtimeRowKey,
          tableSize,
          pagination: false,
          scroll: { x: 'max-content' },
          viewportScroll: true,
          empty: (
            <ManagementState
              bordered={false}
              compact
              description={
                !canRead
                  ? localeCode === 'zh_CN'
                    ? '没有查看应用清单的权限'
                    : 'No permission to view manifests'
                  : clusterId
                    ? localeCode === 'zh_CN'
                      ? '当前页清单在此作用域没有绑定'
                      : 'No bindings for this scope on the current manifest page'
                    : localeCode === 'zh_CN'
                      ? '请先选择集群'
                      : 'Please select a cluster'
              }
              kind={!clusterId ? 'select-scope' : 'empty'}
            />
          ),
        }}
        afterTable={
          visibleManifestsQueryData && clusterId ? (
            <nav aria-label={localeCode === 'zh_CN' ? '清单包分页' : 'Manifest package pages'}>
              <Pagination
                align="end"
                size="small"
                current={visibleManifestsQueryData.page}
                pageSize={visibleManifestsQueryData.pageSize}
                total={visibleManifestsQueryData.total}
                showSizeChanger
                showLessItems
                pageSizeOptions={[10, 15, 20, 50, 100]}
                locale={{ items_per_page: localeCode === 'zh_CN' ? '份清单/页' : 'manifests/page' }}
                showTotal={() => (
                  <Text type="secondary">
                    {localeCode === 'zh_CN'
                      ? `共 ${visibleManifestsQueryData.total} 份清单 · 当前页 ${manifestItems.length} 份清单 / ${scopedBindings.length} 条绑定`
                      : `${visibleManifestsQueryData.total} manifests · This page: ${manifestItems.length} manifests / ${scopedBindings.length} bindings`}
                  </Text>
                )}
                onChange={(page, pageSize) => {
                  setSelectedKey('')
                  setPagination({ page: pageSize === pagination.pageSize ? page : 1, pageSize })
                }}
              />
            </nav>
          ) : null
        }
      />

      <ManifestRuntimeDrawer
        clusterName={clusters.find((cluster) => cluster.id === selected?.clusterId)?.name}
        applicationName={
          selected ? applicationNames.get(selected.manifest.applicationId) : undefined
        }
        key={`${selectedKey}:${selectedTab}`}
        initialTab={selectedTab}
        selected={selected}
        deploymentQuery={selectedDeploymentQuery}
        onClose={() => setSelectedKey('')}
      />
    </>
  )
}
