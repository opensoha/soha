import { useResourceDetailTab } from '@/features/platform/shared/resource-detail-header'
import { AdminTable } from '@/components/admin-table'
import { ManagementState } from '@/components/management-list'
import { ReadFeedback } from '@/components/request-feedback'
import { BooleanTag } from '@/components/status-tag'
import { useYamlDraft } from '@/components/use-yaml-draft'
import {
  platformResourceTarget,
  usePlatformMutation,
} from '@/features/platform/shared/use-platform-mutation'
import { useI18n } from '@/i18n'
import { usePlatformScopeStore } from '@/stores/platform-scope-store'
import type { PersistentVolume, PersistentVolumeClaim } from '@/types/platform'
import { readableQueryData } from '@/utils/query-feedback'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { Button, Card, Descriptions, Spin } from 'antd'
import { lazy, Suspense } from 'react'
import { useNavigate, useParams } from 'react-router-dom'
import {
  StorageDetailShell,
  StorageDetailTabs,
  StorageResourceOverview,
} from '../shared/detail-layout'
import { toClusterStorageScope } from '../shared/scope'
import { storageClassMutations } from './mutations'
import { storageClassQueries } from './queries'

const StorageYAMLPanel = lazy(() => import('../shared/yaml-panel'))

export function StorageClassDetailPage() {
  const { localeCode } = useI18n()
  const navigate = useNavigate()
  const { name = '' } = useParams()
  const { clusterId } = usePlatformScopeStore()
  const scope = toClusterStorageScope(clusterId)
  const queryClient = useQueryClient()
  const [activeTab, setActiveTab] = useResourceDetailTab()
  const detailQuery = useQuery(storageClassQueries.detail(scope, name))
  const visibleDetailQueryData = readableQueryData(detailQuery)
  const yamlOptions = storageClassQueries.yaml(scope, name)
  const yamlQuery = useQuery({
    ...yamlOptions,
    enabled: Boolean(yamlOptions.enabled) && activeTab === 'yaml',
  })
  const visibleYamlQueryData =
    Boolean(yamlOptions.enabled) && activeTab === 'yaml' ? readableQueryData(yamlQuery) : undefined
  const updateYAMLMutation = usePlatformMutation(storageClassMutations.updateYAML(queryClient), {
    label: ['应用 YAML', 'Apply YAML'],
    target: platformResourceTarget,
  })
  const platformWriteBlocked = updateYAMLMutation.blocked

  const yamlState = useYamlDraft(
    JSON.stringify([scope.clusterId, scope.namespace, name]),
    visibleYamlQueryData?.content,
  )
  const { value: yamlDraft, setValue: setYAMLDraft } = yamlState

  if (!clusterId) {
    return (
      <div className="soha-page">
        <ManagementState
          kind="select-scope"
          description={localeCode === 'zh_CN' ? '请选择集群' : 'Select a cluster'}
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
          description={localeCode === 'zh_CN' ? 'StorageClass 未找到' : 'StorageClass not found'}
        />
      </div>
    )
  }

  return (
    <StorageDetailShell
      kind="StorageClass"
      name={detail.name}
      detail={detail}
      queryKey={storageClassQueries.detail(scope, name).queryKey}
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
                  namespace="-"
                  createdAt={detail.createdAt}
                  ageSeconds={detail.ageSeconds}
                  labels={detail.labels}
                  annotations={detail.annotations}
                  extra={[
                    { key: 'Provisioner', value: detail.provisioner },
                    { key: 'ReclaimPolicy', value: detail.reclaimPolicy || '-' },
                    { key: 'BindingMode', value: detail.volumeBindingMode || '-' },
                    {
                      key: localeCode === 'zh_CN' ? '允许扩容' : 'Expansion',
                      value: (
                        <BooleanTag
                          value={detail.allowVolumeExpansion}
                          trueLabel="Yes"
                          falseLabel="No"
                        />
                      ),
                    },
                  ]}
                />
                <Card
                  className="soha-detail-card"
                  title={localeCode === 'zh_CN' ? '参数' : 'Parameters'}
                >
                  {detail.parameters && Object.keys(detail.parameters).length > 0 ? (
                    <Descriptions
                      column={1}
                      items={Object.entries(detail.parameters).map(([key, value]) => ({
                        key,
                        label: key,
                        children: value,
                      }))}
                    />
                  ) : (
                    <ManagementState
                      bordered={false}
                      compact
                      description={localeCode === 'zh_CN' ? '暂无参数' : 'No parameters'}
                    />
                  )}
                </Card>
                <Card
                  className="soha-detail-card"
                  title={`${localeCode === 'zh_CN' ? '关联 PV' : 'Related PVs'}${detail.volumesTruncated ? ' (first 200)' : ''}`}
                >
                  <AdminTable
                    dataSource={detail.volumes ?? []}
                    rowKey="name"
                    pagination={false}
                    tableSize="small"
                    enableColumnSelection={false}
                    columns={[
                      {
                        title: 'PV',
                        dataIndex: 'name',
                        render: (value: string) => (
                          <Button
                            type="link"
                            onClick={() =>
                              navigate(`/storage/persistentvolumes/${encodeURIComponent(value)}`)
                            }
                          >
                            {value}
                          </Button>
                        ),
                      },
                      { title: localeCode === 'zh_CN' ? '状态' : 'Status', dataIndex: 'status' },
                      {
                        title: localeCode === 'zh_CN' ? '容量' : 'Capacity',
                        dataIndex: 'capacity',
                        render: (value?: string) => value || '-',
                      },
                      {
                        title: 'Claim',
                        dataIndex: 'claimRef',
                        render: (value: string | undefined, volume: PersistentVolume) =>
                          volume.claimName && volume.claimNamespace ? (
                            <Button
                              type="link"
                              onClick={() =>
                                navigate(
                                  `/storage/persistentvolumeclaims/${encodeURIComponent(volume.claimName!)}?namespace=${encodeURIComponent(volume.claimNamespace!)}`,
                                )
                              }
                            >
                              {value || `${volume.claimNamespace}/${volume.claimName}`}
                            </Button>
                          ) : (
                            value || '-'
                          ),
                      },
                    ]}
                  />
                </Card>
                <Card
                  className="soha-detail-card"
                  title={`${localeCode === 'zh_CN' ? '关联 PVC' : 'Related PVCs'}${detail.claimsTruncated ? ' (first 200)' : ''}`}
                >
                  <AdminTable
                    dataSource={detail.claims ?? []}
                    rowKey={(claim) => `${claim.namespace}/${claim.name}`}
                    pagination={false}
                    tableSize="small"
                    enableColumnSelection={false}
                    columns={[
                      {
                        title: 'PVC',
                        dataIndex: 'name',
                        render: (value: string, claim: PersistentVolumeClaim) => (
                          <Button
                            type="link"
                            onClick={() =>
                              navigate(
                                `/storage/persistentvolumeclaims/${encodeURIComponent(value)}?namespace=${encodeURIComponent(claim.namespace)}`,
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
                      { title: localeCode === 'zh_CN' ? '状态' : 'Status', dataIndex: 'status' },
                      {
                        title: 'PV',
                        dataIndex: 'volumeName',
                        render: (value?: string) =>
                          value ? (
                            <Button
                              type="link"
                              onClick={() =>
                                navigate(`/storage/persistentvolumes/${encodeURIComponent(value)}`)
                              }
                            >
                              {value}
                            </Button>
                          ) : (
                            '-'
                          ),
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
                <Suspense fallback={<Spin size="large" />}>
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
