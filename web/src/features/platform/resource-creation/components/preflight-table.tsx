import { Alert, Table, Typography } from 'antd'
import type { TableColumnsType } from 'antd'
import { StatusTag } from '@/components/status-tag'
import { useI18n } from '@/i18n'
import type { ResourcePreflightItem } from '../types'

const { Text } = Typography

export function resourcePreflightDiagnostic(item: ResourcePreflightItem) {
  return (
    item.errors[0]?.message ||
    item.authorization.error?.message ||
    item.authorization.reason ||
    item.capability.reason ||
    item.dryRun.error?.message ||
    item.warnings[0]?.message ||
    '-'
  )
}

export function ResourcePreflightTable({ items }: { items: ResourcePreflightItem[] }) {
  const { localeCode } = useI18n()
  const isChinese = localeCode === 'zh_CN'
  const columns: TableColumnsType<ResourcePreflightItem> = [
    {
      title: '#',
      dataIndex: ['document', 'index'],
      width: 54,
      render: (value: number) => value + 1,
    },
    {
      title: isChinese ? '资源' : 'Resource',
      key: 'resource',
      render: (_value, item) => (
        <div>
          <Text strong>{`${item.document.kind || '-'} / ${item.document.name || '-'}`}</Text>
          <br />
          <Text type="secondary">{item.document.apiVersion || '-'}</Text>
        </div>
      ),
    },
    {
      title: isChinese ? '目标范围' : 'Target scope',
      key: 'scope',
      render: (_value, item) =>
        item.document.scopeMode === 'cluster'
          ? isChinese
            ? '集群级'
            : 'Cluster'
          : item.resolvedNamespace || '-',
    },
    {
      title: isChinese ? '授权' : 'Authorization',
      key: 'authorization',
      width: 100,
      render: (_value, item) => <StatusTag value={item.authorization.allowed ? 'allow' : 'deny'} />,
    },
    {
      title: isChinese ? '能力' : 'Capability',
      key: 'capability',
      width: 110,
      render: (_value, item) => <StatusTag value={item.capability.status} />,
    },
    {
      title: 'Dry-run',
      key: 'dryRun',
      width: 105,
      render: (_value, item) => <StatusTag value={item.dryRun.status} />,
    },
    {
      title: isChinese ? '诊断' : 'Diagnostic',
      key: 'diagnostic',
      render: (_value, item) => resourcePreflightDiagnostic(item),
    },
  ]
  const warningCount = items.reduce((count, item) => count + item.warnings.length, 0)
  return (
    <div className="soha-resource-create-table-section">
      {warningCount > 0 ? (
        <Alert
          showIcon
          title={
            isChinese
              ? `${warningCount} 条规范化提示将在创建时应用`
              : `${warningCount} normalization warning(s) will be applied`
          }
          type="warning"
        />
      ) : null}
      <Table
        columns={columns}
        dataSource={items}
        pagination={false}
        rowKey={(item) => item.document.index}
        scroll={{ x: 840 }}
        size="small"
      />
    </div>
  )
}
