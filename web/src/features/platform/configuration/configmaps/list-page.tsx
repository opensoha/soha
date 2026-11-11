import { formatAgeSeconds } from '@/utils/time'
import { tableColumnPresets } from '@/utils/table-columns'
import { NamespaceScopeTag } from '@/features/platform/shared/namespace-scope-tag'
import { usePlatformScopeStore } from '@/stores/platform-scope-store'
import { useI18n } from '@/i18n'
import type { TableColumnsType } from 'antd'
import { useNavigate } from 'react-router-dom'
import { ConfigurationNameLink, ConfigurationResourceListPage } from '../shared/list-page'
import { CONFIGMAP_DEFAULT_TEMPLATE, type ConfigMapResource } from './types'

export function ConfigurationConfigMapsPage() {
  const { localeCode } = useI18n()
  const zh = localeCode === 'zh_CN'
  const { clusterId } = usePlatformScopeStore()
  const navigate = useNavigate()
  const columns: TableColumnsType<ConfigMapResource> = [
    {
      title: zh ? '名称' : 'Name',
      dataIndex: 'name',
      width: 340,
      sorter: (a, b) => a.name.localeCompare(b.name),
      render: (value: string, record) => (
        <ConfigurationNameLink
          includeCluster
          kind="configmaps"
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
      title: zh ? '文本键' : 'Text keys',
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
      permissionKey="platform.configuration.config-maps"
      columns={columns}
      defaultTemplate={CONFIGMAP_DEFAULT_TEMPLATE}
      emptyDescription={{
        zh_CN: '当前范围没有 ConfigMaps',
        en_US: 'No configmaps in the current scope',
      }}
      kind="configmaps"
      label="ConfigMaps"
      singularLabel="ConfigMap"
      onEdit={(record) =>
        navigate(
          `/configuration/configmaps/${encodeURIComponent(record.name)}?${new URLSearchParams({ clusterId: clusterId || '', namespace: record.namespace, tab: 'data', edit: 'true' })}`,
        )
      }
    />
  )
}
