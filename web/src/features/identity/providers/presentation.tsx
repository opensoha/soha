import { Space, Typography } from 'antd'
import { MetadataTag, StatusTag } from '@/components/status-tag'
import type { IdentityOIDCClientStatus, IdentityRuntimeProviderStatus } from './types'

const { Text } = Typography

const providerStatusLabels: Record<IdentityRuntimeProviderStatus, string> = {
  disabled: 'Disabled',
  enabled: 'Enabled',
}

const oidcClientStatusLabels: Record<IdentityOIDCClientStatus, string> = {
  disabled: 'Disabled',
  enabled: 'Enabled',
}

export function formatIdentityProviderDateTime(value?: string) {
  if (!value) return '-'
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return value
  return new Intl.DateTimeFormat('zh-CN', {
    month: '2-digit',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
  }).format(date)
}

export function identityProviderStatusTag(status: IdentityRuntimeProviderStatus) {
  const normalizedStatus = providerStatusLabels[status] ? status : 'disabled'
  return <StatusTag label={providerStatusLabels[normalizedStatus]} value={normalizedStatus} />
}

export function identityOIDCClientStatusTag(status: IdentityOIDCClientStatus) {
  const normalizedStatus = oidcClientStatusLabels[status] ? status : 'disabled'
  return <StatusTag label={oidcClientStatusLabels[normalizedStatus]} value={normalizedStatus} />
}

export function identityProviderTagsSummary(values: string[], empty = '-') {
  const items = values ?? []
  if (!items.length) return <Text type="secondary">{empty}</Text>
  return (
    <Space size={[4, 4]} wrap>
      {items.slice(0, 4).map((value) => (
        <MetadataTag key={value} label={value} />
      ))}
      {items.length > 4 ? <MetadataTag label={`+${items.length - 4}`} /> : null}
    </Space>
  )
}
