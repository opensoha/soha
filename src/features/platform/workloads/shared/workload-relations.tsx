import { ReadFeedback } from '@/components/request-feedback'
import { usePodObservations } from './use-pod-observations'
import { Button, Card, Tag } from 'antd'
import { AdminTable } from '@/components/admin-table'
import { WorkloadPodsTable } from './workload-pods-table'
import { useWorkloadDetailScope } from './detail-scope'
import { useResourceDetailTab } from '@/features/platform/shared/resource-detail-header'
import type { TableColumnsType } from 'antd'
import { useNavigate } from 'react-router-dom'
import { ManagementState } from '@/components/management-list'
import {
  buildRelatedResourcePath,
  localizeRelatedRelation,
  localizeRelatedResourceKind,
} from '@/features/platform/workloads-model'
import { useI18n } from '@/i18n'
import { usePlatformScopeStore } from '@/stores/platform-scope-store'
import type { Pod, WorkloadRelation } from '@/types'

export function WorkloadPodsCard({
  pods = [],
  namespace,
  preview = false,
}: {
  pods?: Pod[]
  namespace: string
  preview?: boolean
}) {
  const scope = useWorkloadDetailScope()
  const { localeCode } = useI18n()
  const [, changeTab] = useResourceDetailTab()
  const targetScope = { ...scope, namespace }
  const observed = usePodObservations(targetScope, pods, !preview && pods.length > 0)
  return (
    <>
      <ReadFeedback
        canRead={observed.query.isEnabled}
        query={observed.query}
        label={localeCode === 'zh_CN' ? 'Pod 用量' : 'Pod usage'}
      />
      <WorkloadPodsTable
        pods={observed.pods}
        scope={targetScope}
        compact={preview}
        onShowAll={() => changeTab('pods')}
      />
    </>
  )
}

export function WorkloadRelationsCard({
  resources = [],
  namespace,
}: {
  resources?: WorkloadRelation[]
  namespace: string
}) {
  const { localeCode } = useI18n()
  const navigate = useNavigate()
  const clusterId = usePlatformScopeStore((state) => state.clusterId)
  const columns: TableColumnsType<WorkloadRelation> = [
    {
      title: localeCode === 'zh_CN' ? '资源类型' : 'Kind',
      dataIndex: 'kind',
      width: 150,
      render: (value: string) => <Tag>{localizeRelatedResourceKind(value, localeCode)}</Tag>,
    },
    {
      title: localeCode === 'zh_CN' ? '名称' : 'Name',
      dataIndex: 'name',
      render: (value: string, record) => {
        const path = buildRelatedResourcePath(record, namespace, clusterId)
        return path ? (
          <Button type="link" onClick={() => navigate(path)}>
            {value}
          </Button>
        ) : (
          value
        )
      },
    },
    {
      title: localeCode === 'zh_CN' ? '命名空间' : 'Namespace',
      dataIndex: 'namespace',
      width: 160,
      render: (value?: string) => value || namespace || '-',
    },
    {
      title: localeCode === 'zh_CN' ? '关联关系' : 'Relation',
      dataIndex: 'relation',
      width: 200,
      render: (value?: string) =>
        value ? <Tag>{localizeRelatedRelation(value, localeCode)}</Tag> : '-',
    },
  ]

  return (
    <Card
      className="soha-detail-card"
      size="small"
      title={localeCode === 'zh_CN' ? '关联资源' : 'Related Resources'}
    >
      <AdminTable
        columns={columns}
        dataSource={resources}
        pagination={false}
        rowKey={(record) =>
          `${record.kind}:${record.namespace || namespace}:${record.name}:${record.relation || ''}`
        }
        tableSize="small"
        enableColumnSelection={false}
        scroll={{ x: 800, y: 520 }}
        empty={
          <ManagementState
            bordered={false}
            compact
            title={localeCode === 'zh_CN' ? '暂无关联资源' : 'No related resources'}
          />
        }
      />
    </Card>
  )
}
