import './resource-inventory.css'
import { Typography } from 'antd'
import type { TableColumnsType } from 'antd'
import { AdminTable } from '@/components/admin-table'
import { StatusTag } from '@/components/status-tag'
import type { ManifestResourceInventory } from './types'

export function ManifestResourceInventoryTable({ items }: { items: ManifestResourceInventory[] }) {
  const columns: TableColumnsType<ManifestResourceInventory> = [
    {
      title: '资源',
      key: 'resource',
      width: 230,
      render: (_, item) => (
        <div className="manifest-resource-inventory__resource">
          <Typography.Text strong>{item.name}</Typography.Text>
          <Typography.Text type="secondary">{item.kind}</Typography.Text>
        </div>
      ),
    },
    {
      title: '命名空间',
      dataIndex: 'namespace',
      width: 140,
      render: (value?: string) => (
        <span className="manifest-resource-inventory__namespace">{value || '—'}</span>
      ),
    },
    {
      title: '健康状态',
      key: 'health',
      width: 110,
      render: (_, item) => (
        <StatusTag
          value={item.deletingAt ? 'deleting' : item.health || 'unknown'}
          label={
            item.deletingAt
              ? '删除中'
              : item.kind === 'WorkloadCronJob' && item.health === 'healthy'
                ? '配置已同步'
                : undefined
          }
        />
      ),
    },
    {
      title: (
        <span title="控制器已观察的资源代次 / 当前资源代次；未报告表示尚未收到观察值">
          观察 / 当前代次
        </span>
      ),
      width: 145,
      key: 'generation',
      render: (_, item) =>
        item.resourceGeneration
          ? `${item.observedResourceGeneration ?? '未报告'} / ${item.resourceGeneration}`
          : '—',
    },
    {
      title: '资源标识与清理',
      key: 'identity',
      width: 180,
      render: (_, item) => (
        <div className="manifest-resource-inventory__identity">
          {item.uid ? (
            <details>
              <summary>
                UID{' '}
                {item.uid.length > 16 ? `${item.uid.slice(0, 8)}…${item.uid.slice(-4)}` : item.uid}
              </summary>
              <Typography.Text copyable={{ text: item.uid, tooltips: ['复制完整 UID', '已复制'] }}>
                {item.uid}
              </Typography.Text>
            </details>
          ) : (
            <Typography.Text type="secondary">UID 未报告</Typography.Text>
          )}
          <details>
            <summary>
              清理等待项{item.finalizers?.length ? ` · ${item.finalizers.length}` : ' · 无'}
            </summary>
            <span>{item.finalizers?.join(', ') || '—'}</span>
          </details>
        </div>
      ),
    },
  ]
  return (
    <AdminTable
      className="manifest-resource-inventory"
      tableLayout="fixed"
      tableSize="small"
      pagination={false}
      enableColumnSelection={false}
      rowKey={(item: ManifestResourceInventory) =>
        `${item.apiVersion}/${item.kind}/${item.namespace}/${item.name}`
      }
      dataSource={items}
      empty="等待执行器返回资源清单"
      scroll={{ x: 805 }}
      columns={columns}
    />
  )
}
