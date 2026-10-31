import { useResourceDetailTab } from '@/features/platform/shared/resource-detail-header'
import { ManagementState } from '@/components/management-list'
import { ReadFeedback } from '@/components/request-feedback'
import { BooleanTag } from '@/components/status-tag'
import { useI18n } from '@/i18n'
import { usePlatformScopeStore } from '@/stores/platform-scope-store'
import { toScopeKey } from '@/types'
import { readableQueryData } from '@/utils/query-feedback'
import { useQuery } from '@tanstack/react-query'
import { Card, Table } from 'antd'
import { Link, useParams } from 'react-router-dom'
import { NetworkDetailShell } from '../shared/detail-shell'
import { buildNetworkRoutePath } from '../shared/paths'
import { renderNetworkTextList } from '../shared/renderers'
import { networkCoreQueries } from './queries'
import type { IngressClassDetail } from './types'

export function IngressClassDetailPage() {
  const { localeCode } = useI18n()
  const { clusterId } = usePlatformScopeStore()
  const name = (useParams().name as string | undefined) ?? ''
  const scope = toScopeKey(clusterId, null)
  const [activeTabKey, setActiveTabKey] = useResourceDetailTab()
  const query = useQuery(
    networkCoreQueries.detail<IngressClassDetail>('ingressclasses', scope, name, true),
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
          description={localeCode === 'zh_CN' ? 'IngressClass 未找到' : 'IngressClass not found'}
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
        kind="ingressclasses"
        label="IngressClass"
        onTabChange={setActiveTabKey}
        overviewExtra={[
          { key: 'Controller', value: detail.controller || '-' },
          {
            key: 'Default',
            value: <BooleanTag value={detail.isDefault} trueLabel="Yes" falseLabel="No" />,
          },
          { key: 'Parameters', value: detail.parameters || '-' },
        ]}
        overviewContent={
          detail.ingresses?.length ? (
            <Card
              className="soha-detail-card"
              title={localeCode === 'zh_CN' ? '关联 Ingresses' : 'Related Ingresses'}
            >
              <Table
                dataSource={detail.ingresses}
                pagination={false}
                rowKey={(item) => `${item.namespace}/${item.name}`}
                size="small"
                columns={[
                  {
                    title: 'Ingress',
                    dataIndex: 'name',
                    render: (value: string, item) => (
                      <Link to={buildNetworkRoutePath('ingresses', value, item.namespace)}>
                        {value}
                      </Link>
                    ),
                  },
                  {
                    title: localeCode === 'zh_CN' ? '命名空间' : 'Namespace',
                    dataIndex: 'namespace',
                  },
                  {
                    title: 'Hosts',
                    dataIndex: 'hosts',
                    render: (value?: string[]) => renderNetworkTextList(value),
                  },
                  {
                    title: localeCode === 'zh_CN' ? '地址' : 'Address',
                    dataIndex: 'address',
                    render: (value?: string) => value || '-',
                  },
                  {
                    title: localeCode === 'zh_CN' ? '后端 Services' : 'Backend Services',
                    dataIndex: 'backendServices',
                    render: (value?: string[]) => renderNetworkTextList(value),
                  },
                ]}
              />
            </Card>
          ) : null
        }
        target={{ scope, name }}
      />
    </>
  )
}
