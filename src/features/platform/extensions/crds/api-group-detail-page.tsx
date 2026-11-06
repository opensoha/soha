import { ResourceDetailHeader } from '@/features/platform/shared/resource-detail-header'
import { ManagementState } from '@/components/management-list'
import { ReadFeedback } from '@/components/request-feedback'
import { useAIPageContext } from '@/features/copilot'
import '@/features/platform/extensions/styles.css'
import { useI18n } from '@/i18n'
import { readableQueryData } from '@/utils/query-feedback'
import { useQuery } from '@tanstack/react-query'
import { Card } from 'antd'
import { useMemo } from 'react'
import { useParams, useSearchParams } from 'react-router-dom'
import { CRDKindWorkspace } from './kind-workspace'
import { crdQueries } from './queries'
import { groupCRDsByApi, safeDecodeURIComponent } from './utils'
import { useCRDNavigationGuard } from './use-navigation-guard'
import { useCRDClusterScope } from './use-cluster-scope'

export function CRDApiGroupDetailPage() {
  const { t, localeCode } = useI18n()
  const { groupName } = useParams()
  const [params, setParams] = useSearchParams()
  const { requestLeave, setEditorState } = useCRDNavigationGuard()
  const { clusterId, scopeReady } = useCRDClusterScope(params, setParams, requestLeave)
  const selectedCRDName = params.get('kind')
  const decodedGroupName = safeDecodeURIComponent(groupName)
  const catalogQuery = useQuery({
    ...crdQueries.catalog(clusterId),
    enabled: Boolean(clusterId) && scopeReady,
  })
  const visibleCatalogQueryData = readableQueryData(catalogQuery)
  const apiGroups = useMemo(
    () => groupCRDsByApi(visibleCatalogQueryData ?? []),
    [visibleCatalogQueryData],
  )
  const groupSummary = useMemo(
    () => apiGroups.find((item) => item.group === decodedGroupName) ?? null,
    [apiGroups, decodedGroupName],
  )
  const groupCRDs = useMemo(() => groupSummary?.crds ?? [], [groupSummary?.crds])
  const selectedCRD = useMemo(
    () =>
      selectedCRDName !== null
        ? (groupCRDs.find((item) => item.name === selectedCRDName) ?? null)
        : (groupCRDs[0] ?? null),
    [groupCRDs, selectedCRDName],
  )
  useAIPageContext({
    sourceWorkbench: 'platform',
    sourceTitle: decodedGroupName || 'Kubernetes API Group',
    entityKind: 'CustomResourceDefinition',
    entityName: selectedCRD?.name || decodedGroupName,
    clusterId: clusterId || undefined,
  })

  return (
    <div className="soha-page soha-k8s-detail-page soha-crd-group-detail">
      <ResourceDetailHeader
        kind="API Group"
        name={decodedGroupName || 'API Group'}
        backTo={`/extensions?${new URLSearchParams({ ...(clusterId ? { clusterId } : {}), ...(params.get('q') ? { q: params.get('q')! } : {}) })}`}
        facts={groupSummary ? <span>{groupCRDs.length} CRDs</span> : undefined}
      />
      <ReadFeedback canRead={catalogQuery.isEnabled} query={catalogQuery} label="资源目录" />

      {!clusterId ? (
        <Card className="soha-detail-card" style={{ marginTop: 0 }}>
          <ManagementState compact kind="select-scope" />
        </Card>
      ) : !scopeReady || catalogQuery.isLoading ? (
        <Card className="soha-detail-card" style={{ marginTop: 0 }} loading />
      ) : catalogQuery.isError && !visibleCatalogQueryData ? (
        <Card className="soha-detail-card" style={{ marginTop: 0 }}>
          <ManagementState
            compact
            kind="error"
            title={localeCode === 'zh_CN' ? 'API Group 加载失败' : 'Failed to load the API group'}
          />
        </Card>
      ) : !groupSummary ? (
        <Card className="soha-detail-card" style={{ marginTop: 0 }}>
          <ManagementState
            compact
            kind="not-found"
            title={t(
              'page.extensions.crd.groupEmpty',
              'The selected API group is not available in the current cluster.',
            )}
          />
        </Card>
      ) : selectedCRD ? (
        <CRDKindWorkspace
          key={JSON.stringify([clusterId, decodedGroupName])}
          crd={selectedCRD}
          kinds={groupCRDs}
          onLeave={requestLeave}
          onStateChange={setEditorState}
          onKindSelect={(name) =>
            setParams((previous) => {
              const next = new URLSearchParams(previous)
              next.set('kind', name)
              next.set('tab', 'instances')
              next.delete('resource')
              next.delete('resourceNamespace')
              if (clusterId) next.set('clusterId', clusterId)
              return next
            })
          }
        />
      ) : (
        <ManagementState
          compact
          kind="not-found"
          title={
            localeCode === 'zh_CN'
              ? '指定的 CRD 类型不存在'
              : 'The requested CRD kind was not found'
          }
        />
      )}
    </div>
  )
}
