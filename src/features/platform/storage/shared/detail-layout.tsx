import type { ReactNode } from 'react'
import type { TabsProps } from 'antd'
import { useAIPageContext } from '@/features/copilot'
import { PlatformResourceOverview } from '@/features/platform/shared/resource-overview'
import { usePlatformScopeStore } from '@/stores/platform-scope-store'
import {
  ResourceDetailHeader,
  ResourceDetailTabs,
} from '@/features/platform/shared/resource-detail-header'
import type { QueryKey } from '@tanstack/react-query'
import '../styles.css'

export function StorageDetailShell({
  children,
  kind,
  name,
  detail,
  queryKey,
}: {
  children: ReactNode
  kind: string
  name: string
  detail?: { namespace?: string; status?: string; createdAt?: string; ageSeconds?: number }
  queryKey?: QueryKey
}) {
  const { clusterId, namespace } = usePlatformScopeStore()
  useAIPageContext({
    sourceWorkbench: 'platform',
    sourceTitle: kind,
    entityKind: kind,
    entityName: name,
    clusterId: clusterId || undefined,
    namespace: kind === 'PersistentVolumeClaim' ? namespace || undefined : undefined,
  })
  return (
    <div className="soha-page soha-workload-detail-page soha-k8s-detail-page">
      <ResourceDetailHeader
        kind={kind}
        name={name}
        namespace={detail?.namespace}
        status={detail?.status}
        createdAt={detail?.createdAt}
        ageSeconds={detail?.ageSeconds}
        queryKey={queryKey}
      />
      {children}
    </div>
  )
}

export function StorageDetailTabs({
  activeKey,
  items,
  onChange,
}: {
  activeKey: string
  items: TabsProps['items']
  onChange: (key: string) => void
}) {
  return (
    <ResourceDetailTabs
      activeKey={activeKey}
      className="soha-resource-tabs soha-workload-detail-tabs"
      indicator={{ size: (origin) => Math.max(16, origin - 16), align: 'center' }}
      items={items}
      onChange={onChange}
      size="small"
      tabBarGutter={32}
    />
  )
}

export function StorageResourceOverview({
  ageSeconds,
  annotations,
  createdAt,
  extra,
  labels,
  name,
  namespace,
}: {
  ageSeconds?: number
  annotations?: Record<string, string>
  createdAt?: string
  extra: Array<{ key: string; value: ReactNode }>
  labels?: Record<string, string>
  name: string
  namespace: string
}) {
  return (
    <PlatformResourceOverview
      compact
      ageSeconds={ageSeconds}
      annotations={annotations}
      createdAt={createdAt}
      facts={extra.map((item) => ({ ...item, label: item.key }))}
      labels={labels}
      name={name}
      namespace={namespace}
    />
  )
}
