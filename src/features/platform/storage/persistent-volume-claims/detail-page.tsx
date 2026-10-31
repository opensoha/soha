import { useResourceDetailTab } from '@/features/platform/shared/resource-detail-header'
import { AdminTable } from '@/components/admin-table'
import { ManagementState } from '@/components/management-list'
import { ReadFeedback } from '@/components/request-feedback'
import { StatusTag } from '@/components/status-tag'
import { useYamlDraft } from '@/components/use-yaml-draft'
import {
  platformResourceTarget,
  usePlatformMutation,
} from '@/features/platform/shared/use-platform-mutation'
import { useI18n } from '@/i18n'
import { usePlatformScopeStore } from '@/stores/platform-scope-store'
import type { StoragePodReference } from '@/types/platform'
import { readableQueryData } from '@/utils/query-feedback'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { Button, Card, Spin } from 'antd'
import { lazy, Suspense } from 'react'
import { useNavigate, useParams, useSearchParams } from 'react-router-dom'
import {
  StorageDetailShell,
  StorageDetailTabs,
  StorageResourceOverview,
} from '../shared/detail-layout'
import { toStorageScope } from '../shared/scope'
import { PersistentVolumeClaimLifecyclePanel } from './lifecycle-panel'
import { persistentVolumeClaimMutations } from './mutations'
import { persistentVolumeClaimQueries } from './queries'

const StorageYAMLPanel = lazy(() => import('../shared/yaml-panel'))

export function StoragePvcDetailPage() {
  const { localeCode } = useI18n()
  const navigate = useNavigate()
  const { name = '' } = useParams()
  const [searchParams] = useSearchParams()
  const { clusterId, namespace } = usePlatformScopeStore()
  const detailNamespace = namespace || searchParams.get('namespace') || ''
  const scope = toStorageScope(clusterId, detailNamespace)
  const queryClient = useQueryClient()
  const [activeTab, setActiveTab] = useResourceDetailTab()
  const detailQuery = useQuery(persistentVolumeClaimQueries.detail(scope, name))
  const visibleDetailQueryData = readableQueryData(detailQuery)
  const yamlOptions = persistentVolumeClaimQueries.yaml(scope, name)
  const yamlQuery = useQuery({
    ...yamlOptions,
    enabled: Boolean(yamlOptions.enabled) && activeTab === 'yaml',
  })
  const visibleYamlQueryData =
    Boolean(yamlOptions.enabled) && activeTab === 'yaml' ? readableQueryData(yamlQuery) : undefined
  const updateYAMLMutation = usePlatformMutation(
    persistentVolumeClaimMutations.updateYAML(queryClient),
    { label: ['应用 YAML', 'Apply YAML'], target: platformResourceTarget },
  )
  const platformWriteBlocked = updateYAMLMutation.blocked

  const yamlState = useYamlDraft(
    JSON.stringify([scope.clusterId, scope.namespace, name]),
    visibleYamlQueryData?.content,
  )
  const { value: yamlDraft, setValue: setYAMLDraft } = yamlState

  if (!clusterId || !detailNamespace) {
    return (
      <div className="soha-page">
        <ManagementState
          kind="select-scope"
          description={
            localeCode === 'zh_CN' ? '请选择集群和命名空间' : 'Select a cluster and namespace'
          }
        />
      </div>
    )
  }
  if (detailQuery.isLoading) return <Card loading className="soha-detail-card" />
  const detail = visibleDetailQueryData
  if (detailQuery.isError && !detail)
    return <ReadFeedback canRead={detailQuery.isEnabled} query={detailQuery} label="资源详情" />
  if (!detail) {
    return (
      <div className="soha-page">
        <ManagementState
          kind="not-found"
          description={localeCode === 'zh_CN' ? 'PVC 未找到' : 'PVC not found'}
        />
      </div>
    )
  }

  return (
    <StorageDetailShell
      kind="PVC"
      name={detail.name}
      detail={detail}
      queryKey={persistentVolumeClaimQueries.detail(scope, name).queryKey}
    >
      <ReadFeedback canRead={detailQuery.isEnabled} query={detailQuery} label="资源详情" />
      <ReadFeedback canRead={yamlQuery.isEnabled} query={yamlQuery} label="YAML" />

      {updateYAMLMutation.feedback}

      <StorageDetailTabs
        activeKey={activeTab}
        onChange={setActiveTab}
        items={[
          {
            key: 'overview',
            label: localeCode === 'zh_CN' ? '概览' : 'Overview',
            children: (
              <div className="soha-storage-detail-stack">
                <StorageResourceOverview
                  name={detail.name}
                  namespace={detail.namespace}
                  createdAt={detail.createdAt}
                  ageSeconds={detail.ageSeconds}
                  labels={detail.labels}
                  annotations={detail.annotations}
                  extra={[
                    {
                      key: localeCode === 'zh_CN' ? '状态' : 'Status',
                      value: <StatusTag value={detail.status} />,
                    },
                    {
                      key: 'Volume',
                      value: detail.volumeName ? (
                        <Button
                          type="link"
                          onClick={() =>
                            navigate(
                              `/storage/persistentvolumes/${encodeURIComponent(detail.volumeName!)}`,
                            )
                          }
                        >
                          {detail.volumeName}
                        </Button>
                      ) : (
                        '-'
                      ),
                    },
                    {
                      key: 'StorageClass',
                      value: detail.storageClass ? (
                        <Button
                          type="link"
                          onClick={() =>
                            navigate(
                              `/storage/storageclasses/${encodeURIComponent(detail.storageClass!)}`,
                            )
                          }
                        >
                          {detail.storageClass}
                        </Button>
                      ) : (
                        '-'
                      ),
                    },
                    {
                      key: localeCode === 'zh_CN' ? '申请容量' : 'Requested',
                      value: detail.requested || '-',
                    },
                    {
                      key: localeCode === 'zh_CN' ? '已分配容量' : 'Capacity',
                      value: detail.capacity || '-',
                    },
                    { key: 'VolumeMode', value: detail.volumeMode || '-' },
                    { key: 'AccessModes', value: detail.accessModes?.join(', ') || '-' },
                  ]}
                />
                <PersistentVolumeClaimLifecyclePanel claim={detail} />
                <Card
                  className="soha-detail-card"
                  title={`${localeCode === 'zh_CN' ? '使用此 PVC 的 Pods' : 'Pods using this PVC'}${detail.podsTruncated ? ' (first 200)' : ''}`}
                >
                  <AdminTable
                    dataSource={detail.pods ?? []}
                    rowKey={(pod) => `${pod.namespace}/${pod.name}`}
                    pagination={false}
                    tableSize="small"
                    enableColumnSelection={false}
                    columns={[
                      {
                        title: 'Pod',
                        dataIndex: 'name',
                        render: (value: string, pod: StoragePodReference) => (
                          <Button
                            type="link"
                            onClick={() =>
                              navigate(
                                `/workloads/pods/${encodeURIComponent(value)}?namespace=${encodeURIComponent(pod.namespace)}`,
                              )
                            }
                          >
                            {value}
                          </Button>
                        ),
                      },
                      {
                        title: localeCode === 'zh_CN' ? '命名空间' : 'Namespace',
                        dataIndex: 'namespace',
                      },
                      {
                        title: localeCode === 'zh_CN' ? '状态' : 'Status',
                        dataIndex: 'phase',
                        render: (value?: string) => (value ? <StatusTag value={value} /> : '-'),
                      },
                      {
                        title: localeCode === 'zh_CN' ? '节点' : 'Node',
                        dataIndex: 'nodeName',
                        render: (value?: string) => value || '-',
                      },
                    ]}
                  />
                </Card>
              </div>
            ),
          },
          {
            key: 'yaml',
            label: 'YAML',
            children:
              activeTab === 'yaml' ? (
                <Suspense
                  fallback={
                    <Card className="soha-detail-card">
                      <Spin size="large" />
                    </Card>
                  }
                >
                  {yamlQuery.isError && visibleYamlQueryData === undefined ? null : (
                    <StorageYAMLPanel
                      blocked={platformWriteBlocked}
                      error={yamlQuery.error}
                      original={yamlState.original}
                      serverChanged={yamlState.serverChanged}
                      onCompareLatest={yamlState.compareLatest}
                      applying={updateYAMLMutation.isPending}
                      draft={yamlDraft}
                      onChange={setYAMLDraft}
                      onReset={yamlState.reset}
                      onApply={() =>
                        updateYAMLMutation.mutate(
                          { scope, name, content: yamlDraft },
                          {
                            onSuccess: (resourceYAML) => {
                              yamlState.applied(resourceYAML.content ?? yamlDraft)
                            },
                          },
                        )
                      }
                    />
                  )}
                </Suspense>
              ) : null,
          },
        ]}
      />
    </StorageDetailShell>
  )
}
