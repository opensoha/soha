import { AdminTable } from '@/components/admin-table'
import { BooleanTag, MetadataTag, StatusTag } from '@/components/status-tag'
import { useI18n } from '@/i18n'
import { Alert, Card, Typography } from 'antd'
import type { TableColumnsType } from 'antd'
import type { ReactNode } from 'react'
import { publicIngressURL, type ConfiguredIngressRoute } from './configuration'
import type { Ingress, IngressPod } from './types'
export type IngressReference = (kind: string, name: string, namespace?: string) => ReactNode
export function IngressRulesTable({
  routes,
  ingress,
  reference,
  canReadServices,
  preview = false,
}: {
  routes: ConfiguredIngressRoute[] | undefined
  ingress: Ingress
  reference: IngressReference
  canReadServices: boolean
  preview?: boolean
}) {
  const { localeCode } = useI18n(),
    zh = localeCode === 'zh_CN'
  const columns: TableColumnsType<ConfiguredIngressRoute> = [
    {
      title: zh ? '域名' : 'Host',
      key: 'host',
      width: 220,
      render: (_, r) => {
        if (r.isDefault) return <MetadataTag label={zh ? '默认后端' : 'Default backend'} />
        const url = publicIngressURL(r)
        return url ? (
          <Typography.Link href={url} target="_blank" rel="noreferrer">
            {r.host}
          </Typography.Link>
        ) : (
          r.host || (zh ? '所有 Host' : 'All hosts')
        )
      },
    },
    {
      title: zh ? '路径' : 'Path',
      key: 'path',
      width: 250,
      render: (_, r) => (
        <div className="soha-ingress-rule-path">
          <span>{r.isDefault ? '—' : r.path || '—'}</span>
          {!r.isDefault && <MetadataTag label={r.pathType || '—'} />}
        </div>
      ),
    },
    {
      title: zh ? '后端' : 'Backend',
      key: 'backend',
      width: 260,
      render: (_, r) =>
        r.resource ? (
          <span>
            {r.resource.kind} / {r.resource.name}
          </span>
        ) : canReadServices ? (
          <span className="soha-ingress-rule-backend">
            {r.serviceName ? reference('Service', r.serviceName, ingress.namespace) : '—'}
            <span>:{r.servicePort || '—'}</span>
          </span>
        ) : zh ? (
          '无 Service 查看权限'
        ) : (
          'Service access unavailable'
        ),
    },
    ...(!preview
      ? [
          {
            title: 'TLS',
            key: 'tls',
            width: 90,
            render: (_: unknown, r: ConfiguredIngressRoute) =>
              r.tls === undefined ? (
                <span>{zh ? '未确认' : 'Unknown'}</span>
              ) : (
                <BooleanTag value={r.tls} trueLabel={zh ? '已配置' : 'Configured'} falseLabel="—" />
              ),
          },
          {
            title: zh ? '端点 / Pod' : 'Endpoints / Pods',
            key: 'targets',
            width: 130,
            render: (_: unknown, r: ConfiguredIngressRoute) => {
              const b = ingress.backends?.find((b) => b.serviceName === r.serviceName)
              return r.resource ? '—' : `${b?.endpoints?.length ?? '—'} / ${b?.pods?.length ?? '—'}`
            },
          },
        ]
      : []),
  ]
  return routes === undefined ? (
    <Alert showIcon type="info" title={zh ? '规则数据未返回' : 'Rule data was not returned'} />
  ) : (
    <AdminTable
      dataSource={preview ? routes.slice(0, 5) : routes}
      columns={columns}
      rowKey="key"
      pagination={false}
      enableColumnSelection={false}
      tableSize="small"
      scroll={{ x: preview ? 730 : 950, y: preview ? undefined : 520 }}
    />
  )
}
export function IngressBackends({
  ingress,
  reference,
  canReadServices,
  canReadPods,
  canReadEndpoints,
}: {
  ingress: Ingress
  reference: IngressReference
  canReadServices: boolean
  canReadPods: boolean
  canReadEndpoints: boolean
}) {
  const { localeCode } = useI18n(),
    zh = localeCode === 'zh_CN'
  if (!canReadServices)
    return (
      <Alert
        showIcon
        type="info"
        title={zh ? '无 Service 查看权限' : 'Service access unavailable'}
      />
    )
  if (ingress.backends === undefined)
    return (
      <Alert
        showIcon
        type="info"
        title={
          zh
            ? '未返回后端数据，当前结果无法确认是否为空'
            : 'Backend data was not returned; empty status is unconfirmed'
        }
      />
    )
  if (!ingress.backends.length)
    return (
      <Typography.Text type="secondary">
        {zh ? '未返回可展开的 Service 后端' : 'No Service backends returned'}
      </Typography.Text>
    )
  return (
    <>
      {ingress.backends.map((b) => (
        <Card
          key={b.serviceName}
          className="soha-detail-card"
          title={reference('Service', b.serviceName, ingress.namespace)}
        >
          <div className="soha-ingress-backend-grid">
            <section>
              <h3>{zh ? '端点' : 'Endpoints'}</h3>
              {!canReadEndpoints ? (
                <Typography.Text type="secondary">
                  {zh ? '无端点查看权限' : 'Endpoint access unavailable'}
                </Typography.Text>
              ) : b.endpoints === undefined ? (
                <Typography.Text type="secondary">
                  {zh ? '端点未返回' : 'Endpoints not returned'}
                </Typography.Text>
              ) : (
                <AdminTable
                  rowKey={(r) => `${r.address}/${r.targetRef || ''}`}
                  dataSource={b.endpoints}
                  pagination={false}
                  enableColumnSelection={false}
                  scroll={{ x: 400, y: 300 }}
                  columns={[
                    { title: zh ? '地址' : 'Address', dataIndex: 'address', width: 180 },
                    {
                      title: zh ? '就绪' : 'Ready',
                      dataIndex: 'ready',
                      width: 90,
                      render: (v?: boolean) => (v === undefined ? '—' : <BooleanTag value={v} />),
                    },
                    {
                      title: zh ? '目标' : 'Target',
                      dataIndex: 'targetRef',
                      render: (v?: string) => v || '—',
                    },
                  ]}
                />
              )}
            </section>
            <section>
              <h3>Pods</h3>
              {!canReadPods ? (
                <Typography.Text type="secondary">
                  {zh ? '无 Pod 查看权限' : 'Pod access unavailable'}
                </Typography.Text>
              ) : b.pods === undefined ? (
                <Typography.Text type="secondary">
                  {zh ? 'Pod 数据未返回' : 'Pods not returned'}
                </Typography.Text>
              ) : (
                <AdminTable
                  rowKey={(r) => `${r.namespace}/${r.name}`}
                  dataSource={b.pods}
                  pagination={false}
                  enableColumnSelection={false}
                  scroll={{ x: 530, y: 300 }}
                  columns={[
                    {
                      title: 'Pod',
                      key: 'name',
                      width: 230,
                      render: (_: unknown, r: IngressPod) => reference('Pod', r.name, r.namespace),
                    },
                    {
                      title: zh ? '状态' : 'Status',
                      dataIndex: 'phase',
                      width: 100,
                      render: (v: string) => <StatusTag value={v} />,
                    },
                    { title: zh ? '就绪' : 'Ready', dataIndex: 'readyContainers', width: 80 },
                    {
                      title: zh ? '工作负载' : 'Workloads',
                      key: 'workloads',
                      width: 240,
                      render: (_: unknown, r: IngressPod) => (
                        <div className="soha-ingress-reference-list">
                          {r.workloads?.map((w) => (
                            <span key={`${w.kind}/${w.name}`}>
                              {reference(w.kind, w.name, w.namespace || r.namespace)}
                            </span>
                          )) || '—'}
                        </div>
                      ),
                    },
                  ]}
                />
              )}
            </section>
          </div>
        </Card>
      ))}
    </>
  )
}
