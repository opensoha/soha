import { useI18n } from '@/i18n'
import { usePlatformScopeStore } from '@/stores/platform-scope-store'
import { formatAgeSeconds } from '@/utils/time'
import { tableColumnPresets } from '@/utils/table-columns'
import type { TableColumnsType } from 'antd'
import { useNavigate } from 'react-router-dom'
import { ConfigurationNameLink, ConfigurationResourceListPage } from '../shared/list-page'
import { VALIDATING_WEBHOOK_TEMPLATE, type ValidatingWebhookConfigurationResource } from './types'

export function ConfigurationValidatingWebhooksPage() {
  const { localeCode } = useI18n(),
    zh = localeCode === 'zh_CN'
  const { clusterId } = usePlatformScopeStore(),
    navigate = useNavigate()
  const count = (value: ValidatingWebhookConfigurationResource['webhooks']) =>
    Array.isArray(value) ? value.length : value
  const columns: TableColumnsType<ValidatingWebhookConfigurationResource> = [
    {
      title: zh ? '名称' : 'Name',
      dataIndex: 'name',
      width: 440,
      sorter: (a, b) => a.name.localeCompare(b.name),
      render: (value: string) => (
        <ConfigurationNameLink includeCluster kind="validatingwebhookconfigurations" name={value} />
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
      permissionKey="platform.configuration.validating-webhook-configurations"
      defaultTemplate={VALIDATING_WEBHOOK_TEMPLATE}
      singularLabel="ValidatingWebhookConfiguration"
      emptyDescription={{
        zh_CN: '当前集群没有 ValidatingWebhookConfiguration',
        en_US: 'No validating webhook configurations in this cluster',
      }}
      kind="validatingwebhookconfigurations"
      label="ValidatingWebhookConfigurations"
      scopeMode="cluster"
      onEdit={(item) =>
        navigate(
          '/configuration/validatingwebhookconfigurations/' +
            encodeURIComponent(item.name) +
            '?' +
            new URLSearchParams({ clusterId: clusterId || '', tab: 'yaml' }),
        )
      }
    />
  )
}
