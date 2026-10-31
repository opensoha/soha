import { useResourceDetailTab } from '@/features/platform/shared/resource-detail-header'
import { ManagementState } from '@/components/management-list'
import { ReadFeedback } from '@/components/request-feedback'
import { StatusTag } from '@/components/status-tag'
import { useI18n } from '@/i18n'
import { usePlatformScopeStore } from '@/stores/platform-scope-store'
import { toScopeKey } from '@/types'
import { readableQueryData } from '@/utils/query-feedback'
import { useQuery } from '@tanstack/react-query'
import { Card } from 'antd'
import { useParams } from 'react-router-dom'
import { NetworkDetailShell } from '../shared/detail-shell'
import { ConditionsSection, GatewaysSection } from './detail-sections'
import { gatewayAPIQueries } from './queries'
import type { GatewayClassDetail } from './types'

export function GatewayClassDetailPage() {
  const { localeCode } = useI18n()
  const { clusterId } = usePlatformScopeStore()
  const name = (useParams().name as string | undefined) ?? ''
  const scope = toScopeKey(clusterId, null)
  const [activeTabKey, setActiveTabKey] = useResourceDetailTab()
  const query = useQuery(
    gatewayAPIQueries.detail<GatewayClassDetail>('gatewayclasses', scope, name, true),
  )
  const visibleQueryData = readableQueryData(query)
  const detail = visibleQueryData

  if (!clusterId)
    return (
      <div className="soha-page">
        <ManagementState
          kind="select-scope"
          description={localeCode === 'zh_CN' ? '请选择集群' : 'Select a cluster'}
        />
      </div>
    )
  if (query.isLoading) return <Card loading className="soha-detail-card" />
  if (!detail)
    return (
      <div className="soha-page">
        <ManagementState
          kind="not-found"
          description={localeCode === 'zh_CN' ? 'GatewayClass 未找到' : 'GatewayClass not found'}
        />
      </div>
    )

  return (
    <>
      <ReadFeedback canRead={query.isEnabled} query={query} label="资源记录" />
      <NetworkDetailShell
        activeTabKey={activeTabKey}
        clusterScoped
        detail={detail}
        kind="gatewayclasses"
        label="GatewayClass"
        onTabChange={setActiveTabKey}
        overviewExtra={[
          { key: 'Controller', value: detail.controllerName || '-' },
          { key: 'Accepted', value: detail.accepted ? <StatusTag value={detail.accepted} /> : '-' },
          { key: 'Parameters', value: detail.parametersRef || '-' },
        ]}
        overviewContent={
          <>
            <GatewaysSection gateways={detail.gateways} />
            <ConditionsSection conditions={detail.conditions} />
          </>
        }
        target={{ scope, name }}
      />
    </>
  )
}
