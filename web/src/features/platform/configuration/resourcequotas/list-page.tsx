import { MetadataTag } from '@/components/status-tag'
import { NamespaceScopeTag } from '@/features/platform/shared/namespace-scope-tag'
import { useI18n } from '@/i18n'
import { usePlatformScopeStore } from '@/stores/platform-scope-store'
import { formatAgeSeconds } from '@/utils/time'
import { tableColumnPresets } from '@/utils/table-columns'
import type { TableColumnsType } from 'antd'
import { useNavigate } from 'react-router-dom'
import { ConfigurationNameLink, ConfigurationResourceListPage } from '../shared/list-page'
import { ResourceQuotaUsage } from './quota-usage'
import { quotaPeak, quotaRows } from './quota-model'
import { RESOURCE_QUOTA_TEMPLATE, type ResourceQuotaResource } from './types'

export function ConfigurationResourceQuotasPage() {
  const { localeCode } = useI18n()
  const zh = localeCode === 'zh_CN'
  const { clusterId } = usePlatformScopeStore()
  const navigate = useNavigate()
  const columns: TableColumnsType<ResourceQuotaResource> = [
    {
      title: zh ? '名称' : 'Name',
      dataIndex: 'name',
      width: 320,
      sorter: (a, b) => a.name.localeCompare(b.name),
      render: (value: string, record) => (
        <ConfigurationNameLink
          includeCluster
          kind="resourcequotas"
          name={value}
          namespace={record.namespace}
        />
      ),
    },
    {
      title: zh ? '命名空间' : 'Namespace',
      dataIndex: 'namespace',
      width: 220,
      sorter: (a, b) => a.namespace.localeCompare(b.namespace),
      render: (value: string) => <NamespaceScopeTag namespace={value} />,
    },
    {
      title: 'Scopes',
      dataIndex: 'scopes',
      width: 200,
      sorter: (a, b) => (a.scopes || []).join(',').localeCompare((b.scopes || []).join(',')),
      render: (value: string[] | undefined) =>
        value?.length ? (
          <span>
            {value.map((item) => (
              <MetadataTag key={item} label={item} />
            ))}
          </span>
        ) : (
          <span className="soha-quota-muted">—</span>
        ),
    },
    {
      title: zh ? '资源项' : 'Resources',
      key: 'resources',
      width: 100,
      sorter: (a, b) => quotaRows(a).length - quotaRows(b).length,
      render: (_, record) => quotaRows(record).length,
    },
    {
      title: zh ? '最高配额占用' : 'Highest quota usage',
      key: 'usage',
      width: 260,
      sorter: (a, b) => {
        const first = quotaPeak(a).score,
          second = quotaPeak(b).score
        return first === second ? 0 : first > second ? 1 : -1
      },
      render: (_, record) => <ResourceQuotaUsage record={record} />,
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
      columns={columns}
      autoRefresh
      permissionKey="platform.configuration.resource-quotas"
      defaultTemplate={RESOURCE_QUOTA_TEMPLATE}
      singularLabel="ResourceQuota"
      emptyDescription={{
        zh_CN: '当前范围没有 ResourceQuota',
        en_US: 'No resource quotas in the current scope',
      }}
      kind="resourcequotas"
      label="ResourceQuotas"
      onEdit={(record) =>
        navigate(
          '/configuration/resourcequotas/' +
            encodeURIComponent(record.name) +
            '?' +
            new URLSearchParams({
              clusterId: clusterId || '',
              namespace: record.namespace,
              tab: 'yaml',
            }),
        )
      }
    />
  )
}
