import { useI18n } from '@/i18n'
import { usePlatformScopeStore } from '@/stores/platform-scope-store'
import { formatAgeSeconds } from '@/utils/time'
import { tableColumnPresets } from '@/utils/table-columns'
import type { TableColumnsType } from 'antd'
import { useNavigate } from 'react-router-dom'
import { ConfigurationNameLink, ConfigurationResourceListPage } from '../shared/list-page'
import { MUTATING_WEBHOOK_TEMPLATE, type MutatingWebhookConfigurationResource } from './types'

export function ConfigurationMutatingWebhooksPage() {
  const { localeCode } = useI18n(),
    zh = localeCode === 'zh_CN'
  const { clusterId } = usePlatformScopeStore(),
    navigate = useNavigate()
  const count = (value: MutatingWebhookConfigurationResource['webhooks']) =>
    Array.isArray(value) ? value.length : value
  const columns: TableColumnsType<MutatingWebhookConfigurationResource> = [
    {
      title: zh ? '名称' : 'Name',
      dataIndex: 'name',
      width: 440,
      sorter: (a, b) => a.name.localeCompare(b.name),
      render: (value: string) => (
        <ConfigurationNameLink includeCluster kind="mutatingwebhookconfigurations" name={value} />
      ),
    },
    {
      title: 'Webhooks',
      dataIndex: 'webhooks',
      width: 140,
      sorter: (a, b) => count(a.webhooks) - count(b.webhooks),
      render: count,
    },
    {
      ...tableColumnPresets.datetime,
      title: zh ? '时长' : 'Age',
      dataIndex: 'ageSeconds',
      width: 140,
      sorter: (a, b) => a.ageSeconds - b.ageSeconds,
      render: (value: number) => formatAgeSeconds(value),
    },
  ]
  return (
    <ConfigurationResourceListPage
      columns={columns}
      autoRefresh
      permissionKey="platform.configuration.mutating-webhook-configurations"
      defaultTemplate={MUTATING_WEBHOOK_TEMPLATE}
      singularLabel="MutatingWebhookConfiguration"
      emptyDescription={{
        zh_CN: '当前集群没有 MutatingWebhookConfiguration',
        en_US: 'No mutating webhook configurations in this cluster',
      }}
      kind="mutatingwebhookconfigurations"
      label="MutatingWebhookConfigurations"
      scopeMode="cluster"
      onEdit={(item) =>
        navigate(
          '/configuration/mutatingwebhookconfigurations/' +
            encodeURIComponent(item.name) +
            '?' +
            new URLSearchParams({ clusterId: clusterId || '', tab: 'yaml' }),
        )
      }
    />
  )
}
