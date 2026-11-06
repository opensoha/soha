import { MetadataTag } from '@/components/status-tag'
import { TableCellText } from '@/components/table-cell-content'
import { hasPermission, usePermissionSnapshot } from '@/features/auth'
import { NamespaceScopeTag } from '@/features/platform/shared/namespace-scope-tag'
import { useI18n } from '@/i18n'
import { usePlatformScopeStore } from '@/stores/platform-scope-store'
import { readableQueryData } from '@/utils/query-feedback'
import { formatAgeSeconds } from '@/utils/time'
import { tableColumnPresets } from '@/utils/table-columns'
import type { TableColumnsType } from 'antd'
import { useNavigate } from 'react-router-dom'
import { ConfigurationNameLink, ConfigurationResourceListPage } from '../shared/list-page'
import { SECRET_DEFAULT_TEMPLATE, type SecretResource } from './types'

export function ConfigurationSecretsPage() {
  const { localeCode } = useI18n()
  const zh = localeCode === 'zh_CN'
  const { clusterId } = usePlatformScopeStore()
  const permissions = readableQueryData(usePermissionSnapshot())?.data
  const canReadData = hasPermission(permissions, 'platform.configuration.secret-data.view')
  const navigate = useNavigate()
  const columns: TableColumnsType<SecretResource> = [
    {
      title: zh ? '名称' : 'Name',
      dataIndex: 'name',
      width: 340,
      sorter: (a, b) => a.name.localeCompare(b.name),
      render: (value: string, record) => (
        <ConfigurationNameLink
          includeCluster
          kind="secrets"
          name={value}
          namespace={record.namespace}
        />
      ),
    },
    {
      title: zh ? '命名空间' : 'Namespace',
      dataIndex: 'namespace',
      width: 200,
      sorter: (a, b) => a.namespace.localeCompare(b.namespace),
      render: (value: string) => <NamespaceScopeTag namespace={value} />,
    },
    {
      title: zh ? '类型' : 'Type',
      dataIndex: 'type',
      width: 260,
      sorter: (a, b) => a.type.localeCompare(b.type),
      render: (value: string) => (
        <MetadataTag label={<TableCellText value={value || 'Opaque'} />} />
      ),
    },
    {
      title: zh ? '数据键' : 'Data keys',
      dataIndex: 'dataEntries',
      width: 120,
      sorter: (a, b) => a.dataEntries - b.dataEntries,
    },
    {
      ...tableColumnPresets.datetime,
      title: zh ? '时长' : 'Age',
      dataIndex: 'ageSeconds',
      width: 120,
      sorter: (a, b) => a.ageSeconds - b.ageSeconds,
      render: (value: number) => formatAgeSeconds(value),
    },
  ]
  return (
    <ConfigurationResourceListPage
      autoRefresh
      permissionKey="platform.configuration.secrets"
      columns={columns}
      defaultTemplate={SECRET_DEFAULT_TEMPLATE}
      emptyDescription={{ zh_CN: '当前范围没有 Secrets', en_US: 'No secrets in the current scope' }}
      kind="secrets"
      label="Secrets"
      singularLabel="Secret"
      onEdit={
        canReadData
          ? (record) =>
              navigate(
                '/configuration/secrets/' +
                  encodeURIComponent(record.name) +
                  '?' +
                  new URLSearchParams({
                    clusterId: clusterId || '',
                    namespace: record.namespace,
                    tab: 'data',
                    edit: 'true',
                  }),
              )
          : undefined
      }
    />
  )
}
