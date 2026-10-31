import { useResourceDetailTab } from '@/features/platform/shared/resource-detail-header'
import { ManagementState } from '@/components/management-list'
import { ReadFeedback } from '@/components/request-feedback'
import { useI18n } from '@/i18n'
import { usePlatformScopeStore } from '@/stores/platform-scope-store'
import { toScopeKey } from '@/types'
import { readableQueryData } from '@/utils/query-feedback'
import { useQuery } from '@tanstack/react-query'
import { Card } from 'antd'
import { useParams, useSearchParams } from 'react-router-dom'
import { NetworkDetailShell } from '../shared/detail-shell'
import { renderNetworkTextList } from '../shared/renderers'
import { resolveNetworkNamespace } from '../shared/scope'
import { ConditionsSection, GatewayListenersSection, GatewayRoutesSection } from './detail-sections'
import { gatewayAPIQueries } from './queries'
import type { GatewayDetail } from './types'

export function GatewayDetailPage() {
  const { localeCode } = useI18n()
  const { clusterId, namespace } = usePlatformScopeStore()
  const name = (useParams().name as string | undefined) ?? ''
  const [searchParams] = useSearchParams()
  const detailNamespace = resolveNetworkNamespace(namespace, searchParams.get('namespace'))
  const scope = toScopeKey(clusterId, detailNamespace)
  const [activeTabKey, setActiveTabKey] = useResourceDetailTab()
  const query = useQuery(gatewayAPIQueries.detail<GatewayDetail>('gateways', scope, name))
  const visibleQueryData = readableQueryData(query)
  const detail = visibleQueryData

  if (!clusterId || !detailNamespace)
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
  if (query.isLoading) return <Card loading className="soha-detail-card" />
  if (!detail)
    return (
      <div className="soha-page">
        <ManagementState
          kind="not-found"
          description={localeCode === 'zh_CN' ? 'Gateway 未找到' : 'Gateway not found'}
        />
      </div>
    )

  return (
    <>
      <ReadFeedback canRead={query.isEnabled} query={query} label="资源记录" />
      <NetworkDetailShell
        activeTabKey={activeTabKey}
        detail={detail}
        kind="gateways"
        label="Gateway"
        onTabChange={setActiveTabKey}
        overviewExtra={[
          { key: 'GatewayClass', value: detail.gatewayClass || '-' },
          { key: 'Addresses', value: renderNetworkTextList(detail.addresses) },
          { key: 'Listeners', value: detail.listenerCount },
        ]}
        overviewContent={
          <>
            <GatewayListenersSection listeners={detail.listeners} />
            <GatewayRoutesSection routes={detail.routes} />
            <ConditionsSection conditions={detail.conditions} />
          </>
        }
        target={{ scope, name }}
      />
    </>
  )
}
