import { useState } from 'react'
import { Descriptions, Space, Tabs, Typography } from 'antd'
import { InfoCircleOutlined } from '@ant-design/icons'
import { ManagementIconButton } from '@/components/management-list'
import { hasPermission, usePermissionSnapshot } from '@/features/auth'
import { useI18n } from '@/i18n'
import { ProviderSetupPanel } from './provider-setup-panel'
import { ProviderProtocolMetadataPanel, ProviderUserMetadataPanel } from './provider-metadata-panel'
import { OIDCClientsPanel } from './oidc-clients-panel'
import { SAMLProviderPanel } from './saml-provider-panel'
import { SecretRevealModal, type IdentityOIDCSecretReveal } from './secret-reveal-modal'
import type { IdentityProvider } from '../types'

export function ProviderDetailContent({ provider }: { provider: IdentityProvider }) {
  const { t } = useI18n()
  const snapshot = usePermissionSnapshot().data?.data
  const [secret, setSecret] = useState<IdentityOIDCSecretReveal | null>(null)
  const can = (action: string) => hasPermission(snapshot, 'identity.providers.' + action)
  return (
    <>
      <Tabs
        className="soha-resource-tabs"
        destroyOnHidden
        items={[
          {
            key: 'setup',
            label: t('identity.providers.tabs.setup'),
            children: <ProviderSetupPanel provider={provider} />,
          },
          ...(provider.type === 'oidc'
            ? [
                {
                  key: 'clients',
                  label: t('identity.providers.tabs.clients'),
                  children: (
                    <OIDCClientsPanel
                      provider={provider}
                      canCreate={can('create')}
                      canUpdate={can('update')}
                      canDelete={can('delete')}
                      canRotate={can('rotate')}
                      onSecretCreated={setSecret}
                    />
                  ),
                },
              ]
            : []),
          ...(provider.type === 'saml'
            ? [
                {
                  key: 'saml',
                  label: t('identity.providers.tabs.saml'),
                  children: <SAMLProviderPanel provider={provider} canRotate={can('rotate')} />,
                },
              ]
            : []),
          {
            key: 'user-metadata',
            label: t('identity.providers.tabs.userMetadata'),
            children: <ProviderUserMetadataPanel key={provider.id} provider={provider} />,
          },
          ...(provider.type !== 'proxy'
            ? [
                {
                  key: 'protocol-metadata',
                  label: t('identity.providers.tabs.protocolMetadata'),
                  children: <ProviderProtocolMetadataPanel provider={provider} />,
                },
              ]
            : []),
          {
            key: 'config',
            label: t('identity.providers.tabs.config'),
            children: (
              <>
                <Descriptions
                  bordered
                  size="small"
                  column={1}
                  styles={{ label: { width: 160 }, content: { overflowWrap: 'anywhere' } }}
                  items={[
                    {
                      key: 'id',
                      label: 'Provider ID',
                      children: <Typography.Text copyable>{provider.id}</Typography.Text>,
                    },
                    {
                      key: 'config',
                      label: (
                        <Space size={4}>
                          {t('identity.providers.configJson')}
                          <ManagementIconButton
                            aria-label={t('identity.providers.configHelp')}
                            tooltip={t('identity.providers.configHint')}
                            icon={<InfoCircleOutlined />}
                          />
                        </Space>
                      ),
                      children: Object.keys(provider.config ?? {}).length ? (
                        <pre className="soha-identity-provider-config-json">
                          {JSON.stringify(provider.config, null, 2)}
                        </pre>
                      ) : (
                        <Typography.Text type="secondary">
                          {t('identity.providers.configEmpty')}
                        </Typography.Text>
                      ),
                    },
                  ]}
                />
              </>
            ),
          },
        ]}
      />
      <SecretRevealModal value={secret} onClose={() => setSecret(null)} />
    </>
  )
}
