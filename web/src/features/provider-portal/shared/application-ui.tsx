import { AppstoreOutlined } from '@ant-design/icons'
import { Avatar, Space, Tag, Typography } from 'antd'
import type { IdentityApplication, IdentityProviderType } from '@/features/identity'
import { useI18n } from '@/i18n'

const { Text } = Typography

export const portalProviderLabels: Record<IdentityProviderType, string> = {
  link: 'Link',
  oidc: 'OIDC',
  proxy: 'Proxy',
  saml: 'SAML',
}

export const portalStatusLabels: Record<
  string,
  { color: string; label: string; status: 'success' | 'warning' | 'default' }
> = {
  enabled: { color: 'green', label: 'Available', status: 'success' },
  maintenance: { color: 'gold', label: 'Maintenance', status: 'warning' },
  disabled: { color: 'default', label: 'Disabled', status: 'default' },
  draft: { color: 'default', label: 'Draft', status: 'default' },
}

export function PortalApplicationAvatar({ application }: { application: IdentityApplication }) {
  return (
    <Avatar
      alt={application.name}
      className="soha-portal-app-avatar"
      icon={application.iconUrl ? undefined : <AppstoreOutlined />}
      shape="square"
      size={46}
      src={application.iconUrl || undefined}
    >
      {application.name.slice(0, 1).toUpperCase()}
    </Avatar>
  )
}

export function PortalTags({ values, max = 3 }: { values?: string[]; max?: number }) {
  const tags = values?.filter(Boolean) ?? []
  if (!tags.length) return null
  return (
    <Space size={[4, 4]} wrap>
      {tags.slice(0, max).map((tag) => (
        <Tag key={tag}>{tag}</Tag>
      ))}
      {tags.length > max ? <Tag>+{tags.length - max}</Tag> : null}
    </Space>
  )
}

export function PortalTagsOrEmpty({ values, max = 8 }: { values?: string[]; max?: number }) {
  const { t } = useI18n()
  if (!values?.some(Boolean)) {
    return <Text type="secondary">{t('providerPortal.empty.none', 'None')}</Text>
  }
  return <PortalTags values={values} max={max} />
}
