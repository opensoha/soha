import { AdminTable } from '@/components/admin-table'
import { useI18n } from '@/i18n'
import { formatDateTime } from '@/utils/time'
import type { components } from '@opensoha/contracts/gen/ts/sohaapi'
import { Card, Typography } from 'antd'
import type { TableColumnsType } from 'antd'

type ActionCheck = components['schemas']['KubernetesAgentActionCheck']

const labels: Record<string, [string, string]> = {
  supported: ['已实现', 'Implemented'],
  unsupported: ['未实现', 'Unsupported'],
  allowed: ['允许', 'Allowed'],
  denied: ['拒绝', 'Denied'],
  unknown: ['未核验', 'Unknown'],
  not_applicable: ['无需 Kubernetes 权限', 'No Kubernetes permission required'],
}

export function AgentActionsCard({ checks }: { checks?: ActionCheck[] }) {
  const { localeCode } = useI18n()
  const zh = localeCode === 'zh_CN'
  const state = (value: string) => labels[value]?.[zh ? 0 : 1] ?? labels.unknown[zh ? 0 : 1]
  const columns: TableColumnsType<ActionCheck> = [
    { title: zh ? '动作' : 'Action', dataIndex: 'action', width: 310 },
    { title: zh ? 'Agent 实现' : 'Implementation', dataIndex: 'implementation', render: state },
    { title: zh ? '动作配置' : 'Action policy', dataIndex: 'configured', render: state },
    {
      title: zh ? '集群范围 RBAC' : 'Cluster-wide RBAC',
      dataIndex: 'kubernetesRBAC',
      render: state,
    },
    {
      title: zh ? '核验时间' : 'Checked at',
      dataIndex: 'checkedAt',
      render: (value?: string) => (value ? formatDateTime(value) : '—'),
    },
  ]

  return (
    <Card
      className="soha-detail-card"
      title={zh ? '连接 Agent 的动作能力' : 'Connected Agent actions'}
    >
      <Typography.Paragraph type="secondary">
        {zh
          ? '配置结果来自连接 Agent 的实际策略。RBAC 仅核验集群范围；通用资源、Helm 与转发需要具体目标才能完整判断。命名空间内的授权可能不同，实际操作仍检查用户权限、资源所有权与 Kubernetes 权限。'
          : 'Policy results come from the connected Agent. RBAC checks cover cluster-wide access; generic resources, Helm and forwarding require a concrete target. Namespace grants can differ. Operations still check user access, ownership and Kubernetes permissions.'}
      </Typography.Paragraph>
      {checks?.length ? (
        <AdminTable
          rowKey="action"
          columns={columns}
          dataSource={checks}
          pagination={false}
          viewportScroll={false}
          enableColumnSelection={false}
          scroll={{ x: 850 }}
        />
      ) : (
        <Typography.Text type="secondary">
          {zh
            ? '当前连接未返回动作诊断，能力尚未核验。'
            : 'This connection has no action diagnostics; capabilities are unverified.'}
        </Typography.Text>
      )}
    </Card>
  )
}
