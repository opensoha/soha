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
import { useWorkloadDetailScope } from '@/features/platform/workloads/shared/detail-scope'
import {
  WorkloadRefresh,
  useWorkloadRefresh,
} from '@/features/platform/workloads/shared/workload-refresh'
import {
  useKubernetesResourceActions,
  kubernetesDeleteConfirmation,
} from '@/features/platform/shared/use-kubernetes-resource-actions'
import { HelmReleaseOverview } from './overview'
import '../styles.css'
import { readableQueryData } from '@/utils/query-feedback'
import { tableColumnPresets } from '@/utils/table-columns'
import { formatDateTime } from '@/utils/time'
import { DiffOutlined, HistoryOutlined, RollbackOutlined } from '@ant-design/icons'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import type { TableColumnsType, TabsProps } from 'antd'
import { Alert, Button, Card, Input, Modal, Space, Spin } from 'antd'
import { lazy, Suspense, useState, useRef, useEffect, useLayoutEffect } from 'react'
import { useParams, useSearchParams, type SetURLSearchParams } from 'react-router-dom'
import { SubmittedValues } from './submitted-values'
import { planHelmReleaseRollback } from '../api'
import { helmMutations } from '../mutations'
import { helmQueries } from '../queries'
import type {
  HelmReleaseHistory,
  HelmReleaseRollbackVariables,
  HelmReleaseTarget,
  HelmValues,
} from '../types'

const HelmReleaseValuesPanel = lazy(async () => {
  const module = await import('./values-panel')
  return { default: module.HelmReleaseValuesPanel }
})

export function HelmReleaseDetailPage() {
  const { clusterId, namespace } = useWorkloadDetailScope()
  const { releaseName = '' } = useParams()
  return (
    <HelmReleaseContent
      key={JSON.stringify([clusterId, namespace, releaseName])}
      clusterId={clusterId || ''}
      detailNamespace={namespace || ''}
      releaseName={releaseName}
    />
  )
}
function HelmReleaseContent({
  clusterId,
  detailNamespace,
  releaseName,
}: {
  clusterId: string
  detailNamespace: string
  releaseName: string
}) {
  const { t, localeCode } = useI18n()
  const refresh = useWorkloadRefresh()
  const [searchParams, setSearchParams] = useSearchParams()
  const currentEditorLocation = useRef<{
    searchParams: URLSearchParams
    setSearchParams: SetURLSearchParams
  } | null>(null)
  useLayoutEffect(() => {
    currentEditorLocation.current = { searchParams, setSearchParams }
    return () => {
      currentEditorLocation.current = null
    }
  }, [searchParams, setSearchParams])
  const [editorOpen, setEditorOpen] = useState(searchParams.get('mode') === 'edit')
  const [visitedEditor, setVisitedEditor] = useState(editorOpen)
  const closeEditor = () => {
    const location = currentEditorLocation.current
    if (!location) return
    setEditorOpen(false)
    if (location.searchParams.get('mode') !== 'edit') return
    const next = new URLSearchParams(location.searchParams)
    next.delete('mode')
    location.setSearchParams(next, { replace: true })
  }
  const [historySearch, setHistorySearch] = useState('')
  const queryClient = useQueryClient()
  const permissionSnapshotQuery = usePermissionSnapshot()
  const snapshot = readableQueryData(permissionSnapshotQuery)?.data
  const canRead = hasPermission(snapshot, 'platform.helm.view')
  const canViewValues = canRead && hasPermission(snapshot, 'platform.helm.values.view')
  const capability = useClusterCapability('helm.releases', localeCode)
  const [requestedTab, setActiveTab] = useResourceDetailTab()
  const activeTab =
    requestedTab === 'values' && !canViewValues
      ? 'history'
      : ['overview', 'history', ...(canViewValues ? ['values'] : [])].includes(requestedTab)
        ? requestedTab
        : 'overview'
  const [selectedRevision, setSelectedRevision] = useState<string | null>(null)
  const [pendingRollback, setPendingRollback] = useState<{
    request: HelmReleaseRollbackVariables
    currentRevision: string
  } | null>(null)
  const [valuesEdit, setValuesEdit] = useState<{ base: HelmValues; content: string } | null>(null)
  const target: HelmReleaseTarget | null = clusterId
    ? { clusterId, name: releaseName, namespace: detailNamespace }
    : null
  const detailQuery = useQuery({
    ...helmQueries.releaseDetail(target),
    enabled: canRead && Boolean(target?.namespace),
    refetchInterval: refresh.interval,
  })
  const visibleDetailQueryData = readableQueryData(detailQuery)
  useEffect(() => {
    if (editorOpen) setVisitedEditor(true)
  }, [editorOpen])
  const valuesQuery = useQuery(
    helmQueries.releaseValues(
      target,
      canRead && canViewValues && (activeTab === 'values' || editorOpen || visitedEditor),
    ),
  )
  const visibleValuesQueryData = canViewValues ? readableQueryData(valuesQuery) : undefined
  const historyQuery = useQuery(
    helmQueries.releaseHistory(target, canRead && activeTab === 'history'),
  )
  const visibleHistoryQueryData = readableQueryData(historyQuery)
  const currentManifestQuery = useQuery(
    helmQueries.releaseManifest(
      target,
      visibleDetailQueryData?.revision,
      canRead && activeTab === 'history' && canViewValues && Boolean(selectedRevision),
    ),
  )
  const visibleCurrentManifestQueryData = readableQueryData(currentManifestQuery)
  const selectedManifestQuery = useQuery(
    helmQueries.releaseManifest(
      target,
      selectedRevision ?? undefined,
      canRead && activeTab === 'history' && canViewValues && Boolean(selectedRevision),
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
  const removeMutation = usePlatformMutation(helmMutations.removeRelease(queryClient), {
    label: ['卸载 Helm Release', 'Uninstall Helm release'],
    target: platformResourceTarget,
  })
  const platformWriteBlocked =
    updateMutation.blocked || rollbackMutation.blocked || removeMutation.blocked
  useAIPageContext({
    sourceWorkbench: 'platform',
    sourceTitle: `Helm Release ${releaseName}`,
    entityKind: 'HelmRelease',
    entityName: releaseName,
    clusterId: clusterId || undefined,
    namespace: detailNamespace || undefined,
  })

  const detail =
    canRead &&
    visibleDetailQueryData?.name === releaseName &&
    visibleDetailQueryData.namespace === detailNamespace
      ? visibleDetailQueryData
      : undefined
  const values =
    visibleValuesQueryData?.name === releaseName &&
    visibleValuesQueryData.namespace === detailNamespace
      ? visibleValuesQueryData
      : undefined
  const valuesBase = valuesEdit?.base ?? values
  const valuesDraft = valuesEdit?.content ?? values?.content ?? ''
  const valuesRevision = Number(valuesBase?.revision)
  const hasValuesRevision = Number.isSafeInteger(valuesRevision) && valuesRevision > 0
  const valuesChanged = Boolean(
    valuesEdit &&
    values &&
    (valuesEdit.base.revision !== values.revision || valuesEdit.base.content !== values.content),
  )
  const changeValuesDraft = (content: string) => {
    if (!valuesBase) return
    setValuesEdit(
      content === valuesBase.content && !valuesChanged ? null : { base: valuesBase, content },
    )
  }
  const canEdit = Boolean(
    canRead &&
    canViewValues &&
    hasPermission(snapshot, 'platform.helm.releases.update') &&
    (values?.editable || detail?.valuesEditable) &&
    hasAllowedAction(values?.allowedActions ?? detail?.allowedActions, 'update'),
  )
  const mutationsDisabled = capability.status !== 'available'
  const applyValuesDisabled =
    platformWriteBlocked ||
    Boolean(valuesQuery.error) ||
    mutationsDisabled ||
    !canEdit ||
    !values ||
    !hasValuesRevision ||
    valuesChanged ||
    !valuesDraft.trim() ||
    valuesDraft === valuesBase?.content
  const rollbackPlan =
    pendingRollback &&
    rollbackPlanMutation.isSuccess &&
    rollbackPlanMutation.variables === pendingRollback.request
      ? rollbackPlanMutation.data
      : null
  const rollbackReadsReady =
    detailQuery.isSuccess &&
    historyQuery.isSuccess &&
    detailQuery.fetchStatus === 'idle' &&
    historyQuery.fetchStatus === 'idle'
  const canRollbackTo = (record: HelmReleaseHistory) =>
    hasPermission(snapshot, 'platform.helm.releases.rollback') &&
    hasAllowedAction(record.allowedActions, 'rollback') &&
    record.revision !== detail?.revision
  const rollbackTargetValid = Boolean(
    canRead &&
    activeTab === 'history' &&
    detail &&
    pendingRollback &&
    detail.revision === pendingRollback.currentRevision &&
    visibleHistoryQueryData?.some(
      (record) =>
        Number(record.revision) === pendingRollback.request.revision && canRollbackTo(record),
    ),
  )
  const capabilityReason = mutationsDisabled ? capability.reason : ''
  const uninstall = useKubernetesResourceActions({
    resourceKey: 'helm-release-detail',
    resourceLabel: 'Helm Release',
    scopeKey: JSON.stringify(target),
    records: detail ? [detail] : [],
    getName: (r) => r.name,
    getKey: (r) => r.namespace + '/' + r.name,
    actions: (record) =>
      hasPermission(snapshot, 'platform.helm.releases.delete') &&
      hasAllowedAction(record.allowedActions, 'delete')
        ? [
            {
              key: 'delete',
              label: localeCode === 'zh_CN' ? '卸载' : 'Uninstall',
              danger: true,
              disabled: platformWriteBlocked || mutationsDisabled,
              confirm: kubernetesDeleteConfirmation(
                record,
                localeCode,
                localeCode === 'zh_CN'
                  ? '卸载会移除该 Release 管理的资源；持久卷和保留资源的结果取决于 Chart 与回收策略。'
                  : 'Uninstall removes release-managed resources; retained resources and volumes depend on chart and reclaim policies.',
              ),
              prepare: () => removeMutation.reset(),
              feedback: removeMutation.feedback,
              run: async () => {
                if (target) await removeMutation.mutateAsync(target)
              },
            },
          ]
        : [],
  })
  const clearRollbackPlan = () => {
    if (rollbackMutation.blocked) return
    rollbackPlanMutation.reset()
    setPendingRollback(null)
  }
  const requestRollback = (record: HelmReleaseHistory) => {
    if (
      !target ||
      !detail?.revision ||
      platformWriteBlocked ||
      mutationsDisabled ||
      !rollbackReadsReady ||
      rollbackPlanMutation.isPending ||
      !canRollbackTo(record)
    )
      return
    const revision = Number(record.revision)
    if (!Number.isInteger(revision) || revision < 1) return
    const variables = { ...target, revision, wait: true, timeoutSeconds: 300 }
    setPendingRollback({ request: variables, currentRevision: detail.revision })
    rollbackPlanMutation.mutate(variables, {})
  }

  const historyColumns: TableColumnsType<HelmReleaseHistory> = [
    {
      title: 'Revision',
      dataIndex: 'revision',
      width: 120,
      render: (value: string) => (
        <span>
          {value}
          {value === detail?.revision ? (localeCode === 'zh_CN' ? ' · 当前' : ' · Current') : ''}
        </span>
      ),
    },
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
              ...(canRollbackTo(record)
                ? [
                    {
                      key: 'rollback',
                      label:
                        localeCode === 'zh_CN'
                          ? `回滚到 revision ${record.revision}`
                          : `Rollback to revision ${record.revision}`,
                      icon: <RollbackOutlined aria-hidden />,
                      disabled:
                        platformWriteBlocked ||
                        mutationsDisabled ||
                        !rollbackReadsReady ||
                        rollbackPlanMutation.isPending,
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
        activeTab === 'values' && canViewValues ? (
          values ? (
            <SubmittedValues content={values.content} revision={values.revision} />
          ) : valuesQuery.isPending ? (
            <ManagementState kind="loading" />
          ) : null
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
            <Card
              className="soha-detail-card"
              title={localeCode === 'zh_CN' ? '版本历史' : 'Revision history'}
              extra={visibleHistoryQueryData?.length ?? '—'}
            >
              <div style={{ marginBottom: 16 }}>
                <Input
                  value={historySearch}
                  onChange={(e) => setHistorySearch(e.target.value)}
                  placeholder={
                    localeCode === 'zh_CN'
                      ? '搜索版本、Chart、状态'
                      : 'Search revision, chart, status'
                  }
                />
              </div>
              <AdminTable
                className="soha-platform-table"
                columns={historyColumns}
                dataSource={(visibleHistoryQueryData ?? []).filter((r) =>
                  [r.revision, r.chart, r.status, r.appVersion]
                    .join(' ')
                    .toLowerCase()
                    .includes(historySearch.trim().toLowerCase()),
                )}
                rowKey={(record) => record.revision}
                pagination={false}
                tableSize="small"
                localSorting
                enableDensity={false}
                enableColumnSelection={false}
                scroll={{ x: 1000 }}
              />
            </Card>
          </Space>
        ) : null,
    },
  ]
  const visibleTabs = canViewValues ? tabs : tabs.filter((tab) => tab.key !== 'values')

  return (
    <div className="soha-helm-release-detail">
      <div className="soha-page soha-workload-detail-page soha-k8s-detail-page">
        {uninstall.confirmation}
        {!uninstall.isConfirming && removeMutation.feedback}
        {!rollbackPlan && <ReadFeedback canRead={canRead} query={detailQuery} label="资源详情" />}
        <ReadFeedback canRead={valuesQuery.isEnabled} query={valuesQuery} label="Helm Values" />
        {!rollbackPlan && (
          <ReadFeedback canRead={historyQuery.isEnabled} query={historyQuery} label="Helm 历史" />
        )}

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
              backTo={`/helm/releases?clusterId=${encodeURIComponent(clusterId)}`}
              actions={
                <>
                  <WorkloadRefresh
                    settings={refresh}
                    queries={[detailQuery, valuesQuery, historyQuery]}
                    resourceLabel="Helm Release"
                    onRefresh={() =>
                      Promise.all([
                        detailQuery.refetch(),
                        ...(activeTab === 'history' ? [historyQuery.refetch()] : []),
                        ...((activeTab === 'values' || editorOpen) && canViewValues
                          ? [valuesQuery.refetch()]
                          : []),
                      ])
                    }
                    beforeRefresh={
                      canEdit ? (
                        <Button
                          disabled={platformWriteBlocked || mutationsDisabled}
                          onClick={() => setEditorOpen(true)}
                        >
                          {localeCode === 'zh_CN' ? '更新 Values' : 'Update values'}
                        </Button>
                      ) : undefined
                    }
                  />
                  {hasPermission(snapshot, 'platform.helm.releases.delete') &&
                  hasAllowedAction(detail.allowedActions, 'delete') ? (
                    <Button
                      danger
                      disabled={platformWriteBlocked || mutationsDisabled}
                      onClick={() => uninstall.selectAction(detail, 'delete')}
                    >
                      {localeCode === 'zh_CN' ? '卸载' : 'Uninstall'}
                    </Button>
                  ) : null}
                </>
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
                    <HelmReleaseOverview detail={detail} capabilityReason={capabilityReason} />
                  ),
                },
                ...visibleTabs.filter((tab) => tab.key === 'history'),
                ...visibleTabs.filter((tab) => tab.key !== 'history'),
              ]}
            />
          </>
        )}
        <Modal
          open={Boolean(selectedRevision) && activeTab === 'history' && canRead && canViewValues}
          title={
            localeCode === 'zh_CN'
              ? `渲染清单比较 · 当前 Revision ${detail?.revision ?? '—'} / Revision ${selectedRevision ?? '—'}`
              : `Manifest comparison · Current ${detail?.revision ?? '—'} / Revision ${selectedRevision ?? '—'}`
          }
          width="min(1440px, calc(100vw - 48px))"
          style={{ top: 24 }}
          styles={{ body: { maxHeight: 'calc(100vh - 130px)', overflow: 'auto' } }}
          footer={null}
          destroyOnHidden
          onCancel={() => setSelectedRevision(null)}
        >
          {selectedRevision &&
          !currentManifestQuery.isLoading &&
          !selectedManifestQuery.isLoading &&
          visibleCurrentManifestQueryData &&
          visibleSelectedManifestQueryData ? (
            <YamlDraftDiffEditor
              baselineOnLeft
              editable={false}
              modified={visibleSelectedManifestQueryData.content}
              original={visibleCurrentManifestQueryData.content}
              title={localeCode === 'zh_CN' ? '渲染清单差异' : 'Rendered manifest diff'}
              leftLabel={`${localeCode === 'zh_CN' ? '当前' : 'Current'} · Revision ${visibleCurrentManifestQueryData.revision}`}
              rightLabel={`Revision ${visibleSelectedManifestQueryData.revision}`}
            />
          ) : selectedRevision && (currentManifestQuery.error || selectedManifestQuery.error) ? (
            <Alert
              showIcon
              type="error"
              title={localeCode === 'zh_CN' ? '清单加载失败' : 'Failed to load manifests'}
              description={(currentManifestQuery.error || selectedManifestQuery.error)?.message}
              action={
                <Button
                  onClick={() => {
                    void currentManifestQuery.refetch()
                    void selectedManifestQuery.refetch()
                  }}
                >
                  {localeCode === 'zh_CN' ? '重试' : 'Retry'}
                </Button>
              }
            />
          ) : selectedRevision ? (
            <Spin />
          ) : null}
        </Modal>
        <Modal
          open={editorOpen && canRead && canViewValues}
          title={
            localeCode === 'zh_CN'
              ? '更新 Values · ' + releaseName
              : 'Update values · ' + releaseName
          }
          width="min(1200px, calc(100vw - 48px))"
          footer={null}
          onCancel={() => {
            if (!updateMutation.blocked) closeEditor()
          }}
          closable={!updateMutation.isPending && !updateMutation.blocked}
          mask={{ closable: false }}
          keyboard={!updateMutation.isPending && !updateMutation.blocked}
          styles={{ body: { maxHeight: 'calc(100vh - 180px)', overflow: 'auto' } }}
        >
          {updateMutation.feedback}
          <ReadFeedback canRead={canViewValues} query={valuesQuery} label="Helm Values" />
          {editorOpen && !values ? (
            <ManagementState kind={valuesQuery.isPending ? 'loading' : 'empty'} />
          ) : null}
          {(editorOpen || visitedEditor) && values && canViewValues ? (
            <Suspense fallback={<Spin />}>
              <HelmReleaseValuesPanel
                original={valuesBase?.content ?? ''}
                serverChanged={valuesChanged}
                onCompareLatest={() =>
                  setValuesEdit(
                    valuesDraft === values.content ? null : { base: values, content: valuesDraft },
                  )
                }
                draft={valuesDraft}
                onChange={changeValuesDraft}
                onReset={() => setValuesEdit(null)}
                onApply={() => {
                  if (!target || !valuesBase || applyValuesDisabled) return
                  updateMutation.mutate(
                    {
                      ...target,
                      content: valuesDraft,
                      expectedRevision: valuesRevision,
                      expectedContent: valuesBase.content,
                    },
                    {
                      onSuccess: () => {
                        if (currentEditorLocation.current) {
                          setValuesEdit(null)
                          closeEditor()
                        }
                      },
                    },
                  )
                }}
                error={valuesQuery.error}
                canEdit={canEdit}
                applying={updateMutation.isPending}
                applyDisabled={applyValuesDisabled}
              />
            </Suspense>
          ) : null}
        </Modal>
        <OperationalPlanModal
          disabled={
            platformWriteBlocked || mutationsDisabled || !rollbackReadsReady || !rollbackTargetValid
          }
          feedback={
            <>
              {updateMutation.feedback}
              {rollbackPlanMutation.feedback}
              {rollbackMutation.feedback}
              <ReadFeedback canRead={canRead} query={detailQuery} label="资源详情" />
              <ReadFeedback
                canRead={historyQuery.isEnabled}
                query={historyQuery}
                label="Helm 历史"
              />
              {rollbackPlan && rollbackReadsReady && !rollbackTargetValid ? (
                <Alert
                  showIcon
                  type="warning"
                  title={
                    localeCode === 'zh_CN'
                      ? '当前版本、目标版本或权限已变化，请重新预检。'
                      : 'The current revision, target revision, or permissions changed. Run preflight again.'
                  }
                />
              ) : null}
            </>
          }
          confirmText={localeCode === 'zh_CN' ? '确认回滚' : 'Confirm rollback'}
          loading={rollbackMutation.isPending}
          onCancel={clearRollbackPlan}
          onConfirm={() => {
            if (
              !pendingRollback ||
              !rollbackPlan?.ready ||
              platformWriteBlocked ||
              mutationsDisabled ||
              !rollbackReadsReady ||
              !rollbackTargetValid
            )
              return
            rollbackMutation.mutate(
              { ...pendingRollback.request, currentRevision: pendingRollback.currentRevision },
              {
                onSuccess: () => {
                  clearRollbackPlan()
                },
              },
            )
          }}
          plan={canRead ? rollbackPlan : null}
          title={localeCode === 'zh_CN' ? '确认 Helm 回滚' : 'Confirm Helm rollback'}
        />
      </div>
    </div>
  )
}
