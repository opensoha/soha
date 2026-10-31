import { useResourceDetailTab } from '@/features/platform/shared/resource-detail-header'
import { AdminTable } from '@/components/admin-table'
import { ManagementState } from '@/components/management-list'
import { ReadFeedback } from '@/components/request-feedback'
import { BooleanTag, StatusTag } from '@/components/status-tag'
import { useI18n } from '@/i18n'
import { usePlatformScopeStore } from '@/stores/platform-scope-store'
import { toScopeKey } from '@/types'
import { readableQueryData } from '@/utils/query-feedback'
import { useQuery } from '@tanstack/react-query'
import type { TableColumnsType } from 'antd'
import { Button, Card, Typography } from 'antd'
import { useNavigate, useParams, useSearchParams } from 'react-router-dom'
import { NetworkDetailShell } from '../shared/detail-shell'
import { resolveNetworkNamespace } from '../shared/scope'
import { ingressQueries } from './queries'
import type { IngressPod, IngressRoute, IngressWorkload } from './types'

function resourcePath(kind: string, name: string, namespace: string) {
  const paths: Record<string, string> = {
    Deployment: 'deployments',
    ReplicaSet: 'replicasets',
    StatefulSet: 'statefulsets',
    DaemonSet: 'daemonsets',
    Job: 'jobs',
    CronJob: 'cronjobs',
  }
  const segment = paths[kind]
  if (!segment) return undefined
  return `/workloads/${segment}/${encodeURIComponent(name)}?${new URLSearchParams({ namespace })}`
}

function publicIngressURL(route: IngressRoute) {
  const host = route.host?.trim()
  const path = route.path?.trim() || '/'
  if (!host || host.includes('*') || !path.startsWith('/') || /[\\^$+?()[\]{}|]/.test(path)) {
    return undefined
  }
  return `${route.tls ? 'https' : 'http'}://${host}${path}`
}

export function IngressDetailPage() {
  const { localeCode } = useI18n()
  const navigate = useNavigate()
  const params = useParams()
  const [searchParams] = useSearchParams()
  const { clusterId, namespace } = usePlatformScopeStore()
  const name = (params.name as string | undefined) ?? ''
  const detailNamespace = resolveNetworkNamespace(namespace, searchParams.get('namespace'))
  const scope = toScopeKey(clusterId, detailNamespace)
  const [activeTabKey, setActiveTabKey] = useResourceDetailTab()
  const detailQuery = useQuery(ingressQueries.detail(scope, name))
  const visibleDetailQueryData = readableQueryData(detailQuery)
  const ingress = visibleDetailQueryData

  if (!clusterId || !detailNamespace) {
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
  }
  if (detailQuery.isLoading) return <Card loading className="soha-detail-card" />
  if (!ingress) {
    return (
      <div className="soha-page">
        <ManagementState
          kind="not-found"
          description={localeCode === 'zh_CN' ? 'Ingress 未找到' : 'Ingress not found'}
        />
      </div>
    )
  }

  const backendByService = new Map((ingress.backends ?? []).map((item) => [item.serviceName, item]))
  const routeColumns: TableColumnsType<IngressRoute> = [
    {
      title: localeCode === 'zh_CN' ? '域名' : 'Host',
      dataIndex: 'host',
      render: (value: string | undefined, record) => {
        const href = publicIngressURL(record)
        return href ? (
          <Typography.Link href={href} rel="noreferrer" target="_blank">
            {value}
          </Typography.Link>
        ) : (
          value || '-'
        )
      },
    },
    {
      title: 'TLS',
      dataIndex: 'tls',
      width: 80,
      render: (value: boolean) => <BooleanTag value={value} trueLabel="Yes" falseLabel="No" />,
    },
    {
      title: localeCode === 'zh_CN' ? '路径' : 'Path',
      dataIndex: 'path',
      render: (value?: string) => value || '/',
    },
    { title: 'PathType', dataIndex: 'pathType', render: (value?: string) => value || '-' },
    {
      title: 'Service',
      dataIndex: 'serviceName',
      render: (value: string) => (
        <Button
          type="text"
          onClick={() =>
            navigate(
              `/network/services/${encodeURIComponent(value)}?${new URLSearchParams({ namespace: ingress.namespace })}`,
            )
          }
        >
          {value}
        </Button>
      ),
    },
    {
      title: localeCode === 'zh_CN' ? '端口' : 'Port',
      dataIndex: 'servicePort',
      width: 100,
      render: (value?: string) => value || '-',
    },
    {
      title: 'Endpoints',
      width: 110,
      render: (_, record) => backendByService.get(record.serviceName)?.endpoints?.length ?? 0,
    },
    {
      title: 'Pods',
      width: 80,
      render: (_, record) => backendByService.get(record.serviceName)?.pods?.length ?? 0,
    },
  ]
  const relatedPods = (ingress.backends ?? []).flatMap((backend) =>
    (backend.pods ?? []).map((pod) => ({ ...pod, serviceName: backend.serviceName })),
  )
  const podColumns: TableColumnsType<IngressPod & { serviceName: string }> = [
    {
      title: 'Service',
      dataIndex: 'serviceName',
      render: (value: string) => (
        <Button
          type="text"
          onClick={() =>
            navigate(
              `/network/services/${encodeURIComponent(value)}?${new URLSearchParams({ namespace: ingress.namespace })}`,
            )
          }
        >
          {value}
        </Button>
      ),
    },
    {
      title: 'Pod',
      dataIndex: 'name',
      render: (value: string, record) => (
        <Button
          type="text"
          onClick={() =>
            navigate(
              `/workloads/pods/${encodeURIComponent(value)}?${new URLSearchParams({ namespace: record.namespace })}`,
            )
          }
        >
          {value}
        </Button>
      ),
    },
    {
      title: localeCode === 'zh_CN' ? '状态' : 'Status',
      dataIndex: 'phase',
      render: (value: string) => <StatusTag value={value} />,
    },
    { title: 'Ready', dataIndex: 'readyContainers' },
    {
      title: localeCode === 'zh_CN' ? '关联工作负载' : 'Workloads',
      dataIndex: 'workloads',
      render: (items?: IngressWorkload[], record?) =>
        items?.length
          ? items.map((item, index) => {
              const path = resourcePath(
                item.kind,
                item.name,
                item.namespace || record?.namespace || ingress.namespace,
              )
              return (
                <span key={`${item.kind}/${item.name}`}>
                  {index ? ', ' : ''}
                  {path ? (
                    <Button type="link" size="small" onClick={() => navigate(path)}>
                      {item.kind}/{item.name}
                    </Button>
                  ) : (
                    `${item.kind}/${item.name}`
                  )}
                </span>
              )
            })
          : '-',
    },
  ]

  return (
    <>
      <ReadFeedback canRead={detailQuery.isEnabled} query={detailQuery} label="资源详情" />
      <NetworkDetailShell
        activeTabKey={activeTabKey}
        aiContext={{
          timeRangeMinutes: 60,
          pinnedData: {
            className: ingress.className,
            hosts: ingress.routes?.map((route) => route.host).filter(Boolean),
            address: ingress.address,
            backendServices: ingress.backendServices,
            activeTab: activeTabKey,
          },
          promptHint:
            localeCode === 'zh_CN'
              ? `排查 Ingress ${ingress.name} 的域名、地址、IngressClass 和后端 Service。`
              : `Troubleshoot Ingress ${ingress.name} hosts, addresses, IngressClass, and backend services.`,
        }}
        detail={ingress}
        kind="ingresses"
        label="Ingress"
        onTabChange={setActiveTabKey}
        overviewExtra={[
          { key: 'IngressClass', value: ingress.className || '-' },
          { key: 'Address', value: ingress.address || '-' },
        ]}
        overviewContent={
          <>
            <Card
              className="soha-detail-card"
              title={localeCode === 'zh_CN' ? '域名与路由' : 'Hosts and routes'}
            >
              <AdminTable
                columns={routeColumns}
                dataSource={ingress.routes ?? []}
                rowKey={(record) =>
                  `${record.host}/${record.path}/${record.serviceName}/${record.servicePort}`
                }
                pagination={false}
                scroll={{ x: 800, y: 520 }}
                tableSize="small"
                enableColumnSelection={false}
              />
            </Card>
            <Card
              className="soha-detail-card"
              title={localeCode === 'zh_CN' ? '关联 Pods 与工作负载' : 'Related pods and workloads'}
            >
              <AdminTable
                columns={podColumns}
                dataSource={relatedPods}
                rowKey={(record) => `${record.serviceName}/${record.namespace}/${record.name}`}
                pagination={false}
                scroll={{ x: 800, y: 520 }}
                tableSize="small"
                enableColumnSelection={false}
              />
            </Card>
          </>
        }
        target={{ scope, name }}
      />
    </>
  )
}
