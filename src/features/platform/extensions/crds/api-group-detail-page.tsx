import { ResourceDetailHeader } from '@/features/platform/shared/resource-detail-header'
import { ManagementState } from '@/components/management-list'
import { ReadFeedback } from '@/components/request-feedback'
import { useAIPageContext } from '@/features/copilot'
import '@/features/platform/extensions/styles.css'
import { useI18n } from '@/i18n'
import { usePlatformScopeStore } from '@/stores/platform-scope-store'
import { readableQueryData } from '@/utils/query-feedback'
import { useQuery } from '@tanstack/react-query'
import { Card } from 'antd'
import { useEffect, useMemo, useState } from 'react'
import { useParams } from 'react-router-dom'
import { CRDKindWorkspace } from './kind-workspace'
import { crdQueries } from './queries'
import { groupCRDsByApi, safeDecodeURIComponent } from './utils'

export function CRDApiGroupDetailPage() {
  const { t, localeCode } = useI18n()
  const { groupName } = useParams()
  const { clusterId, namespace } = usePlatformScopeStore()
  const [selectedCRDName, setSelectedCRDName] = useState<string | null>(null)
  const decodedGroupName = safeDecodeURIComponent(groupName)
  const catalogQuery = useQuery(crdQueries.catalog(clusterId))
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
    () => groupCRDs.find((item) => item.name === selectedCRDName) ?? groupCRDs[0] ?? null,
    [groupCRDs, selectedCRDName],
  )
  useAIPageContext({
    sourceWorkbench: 'platform',
    sourceTitle: decodedGroupName || 'Kubernetes API Group',
    entityKind: 'CustomResourceDefinition',
    entityName: selectedCRD?.name || decodedGroupName,
    clusterId: clusterId || undefined,
  })

  useEffect(() => {
    if (!groupCRDs.length) {
      setSelectedCRDName(null)
    } else if (!selectedCRDName || !groupCRDs.some((item) => item.name === selectedCRDName)) {
      setSelectedCRDName(groupCRDs[0].name)
    }
  }, [groupCRDs, selectedCRDName])

  return (
    <div className="soha-page soha-k8s-detail-page">
      <ResourceDetailHeader
        hideBack
        kind="API Group"
        name={decodedGroupName || 'API Group'}
        facts={groupSummary ? <span>{groupCRDs.length} CRDs</span> : undefined}
      />
      <ReadFeedback canRead={catalogQuery.isEnabled} query={catalogQuery} label="资源目录" />

      {!clusterId ? (
        <Card className="soha-detail-card" style={{ marginTop: 0 }}>
          <ManagementState compact kind="select-scope" />
        </Card>
      ) : catalogQuery.isLoading ? (
        <Card className="soha-detail-card" style={{ marginTop: 0 }} loading />
      ) : catalogQuery.isError ? (
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
          key={JSON.stringify([clusterId, namespace, decodedGroupName])}
          crd={selectedCRD}
          kinds={groupCRDs}
          onKindSelect={setSelectedCRDName}
        />
      ) : (
        <ManagementState compact />
      )}
    </div>
  )
}
