import { useResourceDetailTab } from '@/features/platform/shared/resource-detail-header'
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
import { readableQueryData } from '@/utils/query-feedback'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { Button, Card, Spin } from 'antd'
import { lazy, Suspense } from 'react'
import { useNavigate, useParams } from 'react-router-dom'
import {
  StorageDetailShell,
  StorageDetailTabs,
  StorageResourceOverview,
} from '../shared/detail-layout'
import { toClusterStorageScope } from '../shared/scope'
import { persistentVolumeMutations } from './mutations'
import { persistentVolumeQueries } from './queries'

const StorageYAMLPanel = lazy(() => import('../shared/yaml-panel'))

export function StoragePvDetailPage() {
  const { localeCode } = useI18n()
  const navigate = useNavigate()
  const { name = '' } = useParams()
  const { clusterId } = usePlatformScopeStore()
  const scope = toClusterStorageScope(clusterId)
  const queryClient = useQueryClient()
  const [activeTab, setActiveTab] = useResourceDetailTab()
  const detailQuery = useQuery(persistentVolumeQueries.detail(scope, name))
  const visibleDetailQueryData = readableQueryData(detailQuery)
  const yamlOptions = persistentVolumeQueries.yaml(scope, name)
  const yamlQuery = useQuery({
    ...yamlOptions,
    enabled: Boolean(yamlOptions.enabled) && activeTab === 'yaml',
  })
  const visibleYamlQueryData =
    Boolean(yamlOptions.enabled) && activeTab === 'yaml' ? readableQueryData(yamlQuery) : undefined
  const updateYAMLMutation = usePlatformMutation(
    persistentVolumeMutations.updateYAML(queryClient),
    { label: ['应用 YAML', 'Apply YAML'], target: platformResourceTarget },
  )
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
          description={localeCode === 'zh_CN' ? 'PV 未找到' : 'PV not found'}
        />
      </div>
    )
  }

  return (
    <StorageDetailShell
      kind="PV"
      name={detail.name}
      detail={detail}
      queryKey={persistentVolumeQueries.detail(scope, name).queryKey}
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
              <StorageResourceOverview
                name={detail.name}
                namespace="-"
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
                    key: localeCode === 'zh_CN' ? '容量' : 'Capacity',
                    value: detail.capacity || '-',
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
                    key: 'Claim',
                    value:
                      detail.claimName && detail.claimNamespace ? (
                        <Button
                          type="link"
                          onClick={() =>
                            navigate(
                              `/storage/persistentvolumeclaims/${encodeURIComponent(detail.claimName!)}?namespace=${encodeURIComponent(detail.claimNamespace!)}`,
                            )
                          }
                        >
                          {detail.claimRef || `${detail.claimNamespace}/${detail.claimName}`}
                        </Button>
                      ) : (
                        detail.claimRef || '-'
                      ),
                  },
                  { key: 'AccessModes', value: detail.accessModes?.join(', ') || '-' },
                  { key: 'ReclaimPolicy', value: detail.reclaimPolicy || '-' },
                  { key: 'VolumeMode', value: detail.volumeMode || '-' },
                ]}
              />
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
