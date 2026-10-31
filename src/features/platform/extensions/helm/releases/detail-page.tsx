import { KubernetesResourceActionsMenu } from '@/features/platform/shared/kubernetes-resource-actions-menu'
import {
  ResourceDetailHeader,
  ResourceDetailTabs,
  useResourceDetailTab,
} from '@/features/platform/shared/resource-detail-header'
import { AdminTable } from '@/components/admin-table'
import { ManagementState } from '@/components/management-list'
import { OperationalPlanModal } from '@/components/operational-plan-modal'
import { ReadFeedback } from '@/components/request-feedback'
import { StatusTag } from '@/components/status-tag'
import { useYamlDraft } from '@/components/use-yaml-draft'
import { YamlDraftDiffEditor } from '@/components/yaml-draft-diff-editor'
import { hasAllowedAction, hasPermission, usePermissionSnapshot } from '@/features/auth'
import { useAIPageContext } from '@/features/copilot'
import { useClusterCapability } from '@/features/platform/cluster-capabilities'
import '@/features/platform/extensions/styles.css'
import {
  platformResourceTarget,
  usePlatformMutation,
} from '@/features/platform/shared/use-platform-mutation'
import { useI18n } from '@/i18n'
import { usePlatformScopeStore } from '@/stores/platform-scope-store'
import { readableQueryData } from '@/utils/query-feedback'
import { tableColumnPresets } from '@/utils/table-columns'
import { formatDateTime } from '@/utils/time'
import { DiffOutlined, HistoryOutlined, RollbackOutlined } from '@ant-design/icons'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import type { TableColumnsType, TabsProps } from 'antd'
import { Alert, Card, Descriptions, Space, Spin } from 'antd'
import { lazy, Suspense, useState } from 'react'
import { useParams, useSearchParams } from 'react-router-dom'
import { planHelmReleaseRollback } from '../api'
import { helmMutations } from '../mutations'
import { helmQueries } from '../queries'
import type { HelmReleaseHistory, HelmReleaseRollbackVariables, HelmReleaseTarget } from '../types'

const HelmReleaseValuesPanel = lazy(async () => {
  const module = await import('./values-panel')
  return { default: module.HelmReleaseValuesPanel }
})

export function HelmReleaseDetailPage() {
  const { t, localeCode } = useI18n()
  const { clusterId, namespace } = usePlatformScopeStore()
  const { releaseName = '' } = useParams()
  const [searchParams] = useSearchParams()
  const queryClient = useQueryClient()
  const permissionSnapshotQuery = usePermissionSnapshot()
  const canViewValues = hasPermission(
    permissionSnapshotQuery.data?.data,
    'platform.helm.values.view',
  )
  const capability = useClusterCapability('helm.releases', localeCode)
  const detailNamespace = searchParams.get('namespace') || namespace || ''
  const [requestedTab, setActiveTab] = useResourceDetailTab()
  const activeTab =
    requestedTab === 'values' && !canViewValues
      ? 'history'
      : ['overview', 'history', ...(canViewValues ? ['values'] : [])].includes(requestedTab)
        ? requestedTab
        : 'overview'
  const [selectedRevision, setSelectedRevision] = useState<string | null>(null)
  const [pendingRollback, setPendingRollback] = useState<HelmReleaseRollbackVariables | null>(null)
  const target: HelmReleaseTarget | null = clusterId
    ? { clusterId, name: releaseName, namespace: detailNamespace }
    : null
  const detailQuery = useQuery(helmQueries.releaseDetail(target))
  const visibleDetailQueryData = readableQueryData(detailQuery)
  const valuesQuery = useQuery(
    helmQueries.releaseValues(target, canViewValues && activeTab === 'values'),
  )
  const visibleValuesQueryData = canViewValues ? readableQueryData(valuesQuery) : undefined
  const historyQuery = useQuery(helmQueries.releaseHistory(target, activeTab === 'history'))
  const visibleHistoryQueryData = readableQueryData(historyQuery)
  const currentManifestQuery = useQuery(
    helmQueries.releaseManifest(
      target,
      visibleDetailQueryData?.revision,
      activeTab === 'history' && canViewValues && Boolean(selectedRevision),
    ),
  )
  const visibleCurrentManifestQueryData = readableQueryData(currentManifestQuery)
  const selectedManifestQuery = useQuery(
    helmQueries.releaseManifest(
      target,
      selectedRevision ?? undefined,
      activeTab === 'history' && canViewValues && Boolean(selectedRevision),
    ),
  )
  const visibleSelectedManifestQueryData = readableQueryData(selectedManifestQuery)
  const updateMutation = usePlatformMutation(helmMutations.updateValues(queryClient), {
    label: ['更新资源', 'Update resource'],
    target: platformResourceTarget,
  })
  const rollbackPlanMutation = usePlatformMutation(
    { mutationFn: planHelmReleaseRollback },
    {
      label: ['Helm 回滚预检', 'Helm rollback preflight'],
      target: platformResourceTarget,
      readOnly: true,
    },
  )
  const rollbackMutation = usePlatformMutation(helmMutations.rollbackRelease(queryClient), {
    label: ['回滚资源', 'Roll back resource'],
    target: platformResourceTarget,
  })
  const platformWriteBlocked = updateMutation.blocked || rollbackMutation.blocked
  useAIPageContext({
    sourceWorkbench: 'platform',
    sourceTitle: `Helm Release ${releaseName}`,
    entityKind: 'HelmRelease',
    entityName: releaseName,
    clusterId: clusterId || undefined,
    namespace: detailNamespace || undefined,
  })

  const valuesState = useYamlDraft(
    JSON.stringify([clusterId, detailNamespace, releaseName]),
    visibleValuesQueryData?.content,
  )
  const { value: valuesDraft, setValue: setValuesDraft } = valuesState

  const detail = visibleDetailQueryData
  const values = visibleValuesQueryData
  const valuesOriginal = values?.original || values?.content || ''
  const canEdit = Boolean(
    (values?.editable || detail?.valuesEditable) &&
    hasAllowedAction(values?.allowedActions ?? detail?.allowedActions, 'update'),
  )
  const mutationsDisabled = capability.status !== 'available'
  const capabilityReason = mutationsDisabled ? capability.reason : ''
  const clearRollbackPlan = () => {
    if (rollbackMutation.blocked) return
    rollbackPlanMutation.reset()
    setPendingRollback(null)
  }
  const requestRollback = (record: HelmReleaseHistory) => {
    if (!target) return
    const revision = Number(record.revision)
    if (!Number.isInteger(revision) || revision < 1) return
    const variables = { ...target, revision, wait: true, timeoutSeconds: 300 }
    setPendingRollback(variables)
    rollbackPlanMutation.mutate(variables, {})
  }

  const historyColumns: TableColumnsType<HelmReleaseHistory> = [
    { title: 'Revision', dataIndex: 'revision', width: 96 },
    {
      title: 'Status',
      dataIndex: 'status',
      render: (value?: string) => (value ? <StatusTag value={value} /> : '-'),
    },
    { title: 'Chart', dataIndex: 'chart' },
    { title: 'App Version', dataIndex: 'appVersion' },
    {
      title: 'Values Digest',
      dataIndex: 'valuesDigest',
      render: (value?: string) => (value ? value.slice(0, 12) : '-'),
    },
    {
      ...tableColumnPresets.datetime,
      title: localeCode === 'zh_CN' ? '更新时间' : 'Updated',
      dataIndex: 'updatedAt',
      render: (value?: string) => (value ? formatDateTime(value) : '-'),
    },
    {
      title: '',
      key: 'actions',
      width: 64,
      fixed: 'right',
      align: 'center',
      render: (_value, record) => {
        const isCurrent = record.revision === detail?.revision
        const canRollback = hasAllowedAction(record.allowedActions, 'rollback') && !isCurrent
        return (
          <KubernetesResourceActionsMenu
            resourceLabel="Revision"
            name={record.revision}
            localeCode={localeCode}
            items={[
              ...(canViewValues
                ? [
                    {
                      key: 'compare',
                      label:
                        localeCode === 'zh_CN'
                          ? `比较 revision ${record.revision}`
                          : `Compare revision ${record.revision}`,
                      icon: <DiffOutlined aria-hidden />,
                    },
                  ]
                : []),
              ...(canRollback
                ? [
                    {
                      key: 'rollback',
                      label:
                        localeCode === 'zh_CN'
                          ? `回滚到 revision ${record.revision}`
                          : `Rollback to revision ${record.revision}`,
                      icon: <RollbackOutlined aria-hidden />,
                      disabled: platformWriteBlocked || mutationsDisabled,
                    },
                  ]
                : []),
            ]}
            onSelect={(key) => {
              if (key === 'compare') setSelectedRevision(record.revision)
              else if (key === 'rollback') requestRollback(record)
            }}
          />
        )
      },
    },
  ]

  const tabs: TabsProps['items'] = [
    {
      key: 'values',
      label: 'values.yaml',
      children:
        activeTab === 'values' ? (
          <Suspense fallback={<Spin />}>
            {!canViewValues ||
            (valuesQuery.isError && visibleValuesQueryData === undefined) ? null : (
              <HelmReleaseValuesPanel
                original={valuesOriginal}
                draft={valuesDraft}
                onChange={setValuesDraft}
                onReset={valuesState.reset}
                onApply={() => {
                  if (!target) return
                  updateMutation.mutate(
                    { ...target, content: valuesDraft },
                    {
                      onSuccess: (nextValues) => {
                        valuesState.applied(nextValues.content)
                      },
                    },
                  )
                }}
                error={valuesQuery.error}
                canEdit={canEdit}
                applying={updateMutation.isPending}
                applyDisabled={
                  platformWriteBlocked ||
                  Boolean(valuesQuery.error) ||
                  mutationsDisabled ||
                  !canEdit ||
                  !valuesDraft.trim() ||
                  valuesDraft === valuesOriginal ||
                  updateMutation.isPending
                }
              />
            )}
          </Suspense>
        ) : null,
    },
    {
      key: 'history',
      label: (
        <Space size={6}>
          <HistoryOutlined aria-hidden />
          <span>{t('page.extensions.helm.historyTitle', 'Revision History')}</span>
        </Space>
      ),
      children:
        activeTab === 'history' ? (
          <Space orientation="vertical" size={12} style={{ width: '100%' }}>
            <AdminTable
              className="soha-platform-table"
              columns={historyColumns}
              dataSource={visibleHistoryQueryData ?? []}
              rowKey={(record) => record.revision}
              pagination={false}
              tableSize="small"
              localSorting
              viewportScroll
            />
            {selectedRevision &&
            !currentManifestQuery.isLoading &&
            !selectedManifestQuery.isLoading &&
            visibleCurrentManifestQueryData &&
            visibleSelectedManifestQueryData ? (
              <YamlDraftDiffEditor
                editable={false}
                modified={visibleSelectedManifestQueryData.content}
                original={visibleCurrentManifestQueryData.content}
                title={localeCode === 'zh_CN' ? '渲染清单差异' : 'Rendered manifest diff'}
                leftLabel={`Revision ${selectedRevision}`}
                rightLabel={`Current · Revision ${visibleCurrentManifestQueryData.revision}`}
              />
            ) : selectedRevision && (currentManifestQuery.error || selectedManifestQuery.error) ? (
              <Alert
                showIcon
                type="error"
                title={localeCode === 'zh_CN' ? '清单加载失败' : 'Failed to load manifests'}
                description={(currentManifestQuery.error || selectedManifestQuery.error)?.message}
              />
            ) : selectedRevision ? (
              <Spin />
            ) : null}
          </Space>
        ) : null,
    },
  ]
  const visibleTabs = canViewValues ? tabs : tabs.filter((tab) => tab.key !== 'values')

  return (
    <div className="soha-page soha-workload-detail-page soha-k8s-detail-page">
      <ReadFeedback canRead={detailQuery.isEnabled} query={detailQuery} label="资源详情" />
      <ReadFeedback canRead={valuesQuery.isEnabled} query={valuesQuery} label="Helm Values" />
      <ReadFeedback canRead={historyQuery.isEnabled} query={historyQuery} label="Helm 历史" />
      <ReadFeedback
        canRead={currentManifestQuery.isEnabled}
        query={currentManifestQuery}
        label="当前 Manifest"
      />
      <ReadFeedback
        canRead={selectedManifestQuery.isEnabled}
        query={selectedManifestQuery}
        label="所选版本 Manifest"
      />

      {updateMutation.feedback}
      {rollbackPlanMutation.feedback}
      {rollbackMutation.feedback}

      {!clusterId || !detailNamespace ? (
        <Card className="soha-detail-card" style={{ marginTop: 0 }}>
          <ManagementState compact kind="select-scope" />
        </Card>
      ) : detailQuery.isLoading ? (
        <Card className="soha-detail-card" style={{ marginTop: 0 }} loading />
      ) : !detail ? (
        <Card className="soha-detail-card" style={{ marginTop: 0 }}>
          <ManagementState compact kind="not-found" />
        </Card>
      ) : (
        <>
          <ResourceDetailHeader
            kind="Helm Release"
            name={detail.name}
            namespace={detail.namespace}
            status={detail.status}
            facts={<span>Revision {detail.revision || '—'}</span>}
            queryKey={helmQueries.releaseDetail(target).queryKey}
            onRefresh={() =>
              Promise.all([
                detailQuery.refetch(),
                ...(activeTab === 'history' ? [historyQuery.refetch()] : []),
                ...(activeTab === 'values' && canViewValues ? [valuesQuery.refetch()] : []),
              ])
            }
          />
          <ResourceDetailTabs
            activeKey={activeTab}
            onChange={setActiveTab}
            items={[
              {
                key: 'overview',
                label: localeCode === 'zh_CN' ? '概览' : 'Overview',
                children: (
                  <Card className="soha-detail-card" style={{ marginTop: 0 }}>
                    <Descriptions
                      column={{ xs: 1, sm: 2, lg: 4 }}
                      items={[
                        { key: 'revision', label: 'Revision', children: detail.revision || '-' },
                        {
                          key: 'status',
                          label: 'Status',
                          children: detail.status ? <StatusTag value={detail.status} /> : '-',
                        },
                        { key: 'chart', label: 'Chart', children: detail.chart || '-' },
                        {
                          key: 'appVersion',
                          label: 'App Version',
                          children: detail.appVersion || '-',
                        },
                        {
                          key: 'storageDriver',
                          label: 'Storage',
                          children: detail.storageDriver || '-',
                        },
                        {
                          key: 'updatedAt',
                          label: localeCode === 'zh_CN' ? '更新时间' : 'Updated',
                          children: detail.updatedAt ? formatDateTime(detail.updatedAt) : '-',
                        },
                      ]}
                    />
                    {detail.description ? (
                      <Alert
                        style={{ marginTop: 16 }}
                        type="info"
                        showIcon
                        description={detail.description}
                      />
                    ) : null}
                    {capabilityReason ? (
                      <Alert
                        style={{ marginTop: 16 }}
                        type="warning"
                        showIcon
                        title="Helm writes limited"
                        description={capabilityReason}
                      />
                    ) : null}
                  </Card>
                ),
              },
              ...visibleTabs.filter((tab) => tab.key === 'history'),
              ...visibleTabs.filter((tab) => tab.key !== 'history'),
            ]}
          />
        </>
      )}
      <OperationalPlanModal
        disabled={platformWriteBlocked}
        feedback={
          <>
            {updateMutation.feedback}
            {rollbackPlanMutation.feedback}
            {rollbackMutation.feedback}
          </>
        }
        confirmText={localeCode === 'zh_CN' ? '确认回滚' : 'Confirm rollback'}
        loading={rollbackMutation.isPending}
        onCancel={clearRollbackPlan}
        onConfirm={() => {
          if (!pendingRollback) return
          rollbackMutation.mutate(pendingRollback, {
            onSuccess: () => {
              clearRollbackPlan()
            },
          })
        }}
        plan={rollbackPlanMutation.data ?? null}
        title={localeCode === 'zh_CN' ? '确认 Helm 回滚' : 'Confirm Helm rollback'}
      />
    </div>
  )
}
