import { useResourceDetailTab } from '@/features/platform/shared/resource-detail-header'
import { ManagementState } from '@/components/management-list'
import { ReadFeedback } from '@/components/request-feedback'
import { useI18n } from '@/i18n'
import { readableQueryData } from '@/utils/query-feedback'
import { useQuery } from '@tanstack/react-query'
import { Card } from 'antd'
import type { ReactNode } from 'react'
import { useParams } from 'react-router-dom'
import { useNetworkDetailScope } from '../shared/detail-scope'
import { NetworkDetailShell } from '../shared/detail-shell'
import type { NetworkResourceRecord } from '../shared/types'
import { gatewayAPIQueries } from './queries'
import type { GatewayAPIKind } from './types'

export function GatewayAPIResourceDetail<T extends NetworkResourceRecord>({
  content,
  facts,
  kind,
  label,
}: {
  content?: (detail: T) => ReactNode
  facts?: (detail: T) => Array<{ key: string; value: ReactNode }>
  kind: GatewayAPIKind
  label: string
}) {
  const { localeCode } = useI18n()
  const name = (useParams().name as string | undefined) ?? ''
  const scope = useNetworkDetailScope()
  const { clusterId, namespace: detailNamespace } = scope
  const [activeTabKey, setActiveTabKey] = useResourceDetailTab()
  const query = useQuery(gatewayAPIQueries.detail<T>(kind, scope, name))
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
          description={`${label} ${localeCode === 'zh_CN' ? '未找到' : 'not found'}`}
        />
      </div>
    )

  return (
    <>
      <ReadFeedback canRead={query.isEnabled} query={query} label="资源记录" />
      <NetworkDetailShell
        activeTabKey={activeTabKey}
        detail={detail}
        kind={kind}
        label={label}
        onTabChange={setActiveTabKey}
        overviewContent={content?.(detail)}
        overviewExtra={facts?.(detail)}
        target={{ scope, name }}
      />
    </>
  )
}
