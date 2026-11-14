import { Button, Table, Typography } from 'antd'
import type { TableColumnsType } from 'antd'
import { LinkOutlined } from '@ant-design/icons'
import { StatusTag } from '@/components/status-tag'
import { useI18n } from '@/i18n'
import type { ResourceCreateResultItem, ResourceRef } from '../types'

const { Text } = Typography

function resourcePath(ref: ResourceRef) {
  const name = encodeURIComponent(ref.name)
  const namespace = ref.namespace ? `?namespace=${encodeURIComponent(ref.namespace)}` : ''
  const paths: Record<string, string> = {
    configmap: `/configuration/configmaps/${name}${namespace}`,
    secret: `/configuration/secrets/${name}${namespace}`,
    persistentvolumeclaim: `/storage/persistentvolumeclaims/${name}${namespace}`,
    persistentvolume: `/storage/persistentvolumes/${name}`,
    storageclass: `/storage/storageclasses/${name}`,
    serviceaccount: `/platform-access-control/serviceaccounts/${name}${namespace}`,
    role: `/platform-access-control/roles/${name}${namespace}`,
    rolebinding: `/platform-access-control/rolebindings/${name}${namespace}`,
    clusterrole: `/platform-access-control/clusterroles/${name}`,
    clusterrolebinding: `/platform-access-control/clusterrolebindings/${name}`,
    deployment: `/workloads/deployments/${name}${namespace}`,
    statefulset: `/workloads/statefulsets/${name}${namespace}`,
    daemonset: `/workloads/daemonsets/${name}${namespace}`,
    job: `/workloads/jobs/${name}${namespace}`,
    cronjob: `/workloads/cronjobs/${name}${namespace}`,
    service: `/network/services/${name}${namespace}`,
  }
  return paths[ref.kind.toLowerCase()]
}

export function ResourceCreateResultTable({ items }: { items: ResourceCreateResultItem[] }) {
  const { localeCode } = useI18n()
  const isChinese = localeCode === 'zh_CN'
  const canShowActions = items.some((item) => item.resourceRef && resourcePath(item.resourceRef))
  const columns: TableColumnsType<ResourceCreateResultItem> = [
    {
      title: '#',
      dataIndex: ['document', 'index'],
      width: 54,
      render: (value: number) => value + 1,
    },
    {
      title: isChinese ? '资源' : 'Resource',
      key: 'resource',
      render: (_value, item) => {
        const ref = item.resourceRef
        return (
          <div>
            <Text
              strong
            >{`${ref?.kind || item.document.kind || '-'} / ${ref?.name || item.document.name || '-'}`}</Text>
            <br />
            <Text type="secondary">
              {ref?.scopeMode === 'cluster'
                ? isChinese
                  ? '集群级'
                  : 'Cluster'
                : ref?.namespace || item.document.namespace || '-'}
            </Text>
          </div>
        )
      },
    },
    {
      title: isChinese ? '结果' : 'Result',
      dataIndex: 'status',
      width: 120,
      render: (value: string) => <StatusTag value={value} />,
    },
    {
      title: isChinese ? '说明' : 'Message',
      key: 'message',
      render: (_value, item) => item.error?.message || item.warnings[0]?.message || '-',
    },
    {
      title: '',
      key: 'actions',
      width: 90,
      render: (_value, item) => {
        const path = item.resourceRef ? resourcePath(item.resourceRef) : undefined
        return path ? (
          <Button href={path} icon={<LinkOutlined />} size="small" type="link">
            {isChinese ? '查看' : 'View'}
          </Button>
        ) : null
      },
    },
  ]
  return (
    <Table
      columns={canShowActions ? columns : columns.filter((column) => column.key !== 'actions')}
      dataSource={items}
      pagination={false}
      rowKey={(item) => item.document.index}
      scroll={{ x: 720 }}
      size="small"
    />
  )
}
