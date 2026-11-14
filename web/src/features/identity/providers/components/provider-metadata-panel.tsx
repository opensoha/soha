import { AdminTable } from '@/components/admin-table'
import { ManagementRefreshButton, ManagementState } from '@/components/management-list'
import { ReadFeedback } from '@/components/request-feedback'
import { accessQueries } from '@/features/access'
import { hasPermission, usePermissionSnapshot } from '@/features/auth'
import { useI18n } from '@/i18n'
import { queryAccessDenied, readableQueryData } from '@/utils/query-feedback'
import { QuestionCircleOutlined } from '@ant-design/icons'
import { useQuery } from '@tanstack/react-query'
import { Button, Form, Select, Space, Tooltip, Typography } from 'antd'
import { useState } from 'react'
import { identityProviderQueries } from '../queries'
import type { IdentityProvider } from '../types'
export function ProviderProtocolMetadataPanel({ provider }: { provider: IdentityProvider }) {
  const { t } = useI18n()
  const query = useQuery(identityProviderQueries.protocolMetadata(provider))
  const visibleQueryData = query.isEnabled === false ? undefined : readableQueryData(query)
  return (
    <div className="soha-identity-provider-metadata">
      <ReadFeedback canRead={query.isEnabled} query={query} label="当前记录" />

      <div className="soha-identity-provider-setup-summary">
        <Typography.Text strong>
          {provider.type === 'oidc' ? 'OpenID Connect Discovery' : 'SAML IdP Metadata'}
        </Typography.Text>
        <Space>
          {visibleQueryData && (
            <Typography.Text copyable={{ text: visibleQueryData }}>
              {t('common.copy', '复制')}
            </Typography.Text>
          )}
          <ManagementRefreshButton
            loading={query.isFetching}
            onClick={() => void (query.isEnabled === false ? undefined : query.refetch())}
            tooltip={t('common.refresh', '刷新')}
          />
        </Space>
      </div>
      {query.isPending ? (
        <ManagementState kind="loading" compact />
      ) : query.isError && !visibleQueryData ? null : (
        <pre className="soha-json-block" aria-label={t('identity.providers.tabs.protocolMetadata')}>
          {visibleQueryData}
        </pre>
      )}
    </div>
  )
}
export function ProviderUserMetadataPanel({ provider }: { provider: IdentityProvider }) {
  const { t } = useI18n()
  const snapshot = usePermissionSnapshot().data?.data
  const canViewUsers = hasPermission(snapshot, 'access.users.view')
  const users = useQuery(accessQueries.users(canViewUsers))
  const visibleUsersData = users.isEnabled === false ? undefined : readableQueryData(users)
  const clients = useQuery(
    identityProviderQueries.oidcClients(provider.id, canViewUsers && provider.type === 'oidc'),
  )
  const visibleClientsData = clients.isEnabled === false ? undefined : readableQueryData(clients)
  const [selection, setSelection] = useState<{
    userId: string
    clientId?: string
  } | null>(null)
  const metadata = useQuery({
    ...identityProviderQueries.userMetadata(
      provider.id,
      selection?.userId ?? '',
      selection?.clientId,
    ),
    enabled: canViewUsers && Boolean(selection),
  })
  const visibleMetadataData = metadata.isEnabled === false ? undefined : readableQueryData(metadata)
  if (!canViewUsers)
    return (
      <ManagementState
        kind="no-permission"
        title={t('identity.providers.metadata.noUserPermission')}
      />
    )
  if (queryAccessDenied(users) || (provider.type === 'oidc' && queryAccessDenied(clients)))
    return (
      <>
        <ReadFeedback query={users} canRead={users.isEnabled} label="用户目录" />
        <ReadFeedback query={clients} canRead={clients.isEnabled} label="OIDC Client" />
      </>
    )
  return (
    <div className="soha-identity-provider-metadata">
      <ReadFeedback canRead={users.isEnabled} query={users} label="用户目录" />
      <ReadFeedback canRead={clients.isEnabled} query={clients} label="OIDC Client" />
      <ReadFeedback canRead={metadata.isEnabled} query={metadata} label="用户元数据" />

      <Form
        layout="inline"
        style={{ rowGap: 12 }}
        onValuesChange={() => setSelection(null)}
        onFinish={(values) => {
          if (selection) void (metadata.isEnabled === false ? undefined : metadata.refetch())
          else setSelection(values)
        }}
      >
        <Form.Item
          name="userId"
          label={t('identity.providers.metadata.user')}
          rules={[{ required: true, message: t('identity.providers.metadata.selectUser') }]}
        >
          <Select
            aria-label={t('identity.providers.metadata.user')}
            style={{ width: 220 }}
            showSearch={{ optionFilterProp: 'label' }}
            loading={users.isPending}
            placeholder={t('identity.providers.metadata.selectUser')}
            options={(visibleUsersData ?? [])
              .filter((user) => user.status === 'active')
              .map((user) => ({
                value: user.id,
                label: (user.displayName || user.username) + ' · ' + (user.email || user.username),
              }))}
          />
        </Form.Item>
        {provider.type === 'oidc' && (
          <Form.Item
            name="clientId"
            label={t('identity.providers.metadata.client')}
            rules={[{ required: true, message: t('identity.providers.metadata.selectClient') }]}
          >
            <Select
              aria-label={t('identity.providers.metadata.client')}
              style={{ width: 200 }}
              loading={clients.isPending}
              placeholder={t('identity.providers.metadata.selectClient')}
              options={(visibleClientsData ?? []).map((client) => ({
                value: client.id,
                label: client.clientId,
              }))}
            />
          </Form.Item>
        )}
        <Form.Item>
          <Space size={4}>
            <Button type="primary" htmlType="submit" loading={metadata.isFetching}>
              {t('identity.providers.metadata.preview')}
            </Button>
            <Tooltip title={t('identity.providers.metadata.hint')} trigger={['hover', 'focus']}>
              <Button
                type="text"
                icon={<QuestionCircleOutlined />}
                aria-label={t('identity.providers.metadata.help')}
              />
            </Tooltip>
          </Space>
        </Form.Item>
      </Form>
      {metadata.isError && !visibleMetadataData ? null : !selection ? (
        <ManagementState kind="empty" compact title={t('identity.providers.metadata.selectHint')} />
      ) : metadata.isPending ? (
        <ManagementState kind="loading" compact />
      ) : !visibleMetadataData ? null : (
        <>
          {visibleMetadataData.scopes?.length ? (
            <Typography.Text type="secondary">
              Scopes: {visibleMetadataData.scopes.join(', ')}
            </Typography.Text>
          ) : null}
          {visibleMetadataData.subject && (
            <Typography.Text>NameID: {visibleMetadataData.subject}</Typography.Text>
          )}
          <AdminTable
            rowKey="name"
            scroll={{ x: '100%' }}
            tableLayout="fixed"
            pagination={false}
            enableColumnSelection={false}
            columns={[
              { title: t('identity.providers.metadata.attribute'), dataIndex: 'name', width: 200 },
              {
                title: t('identity.providers.metadata.value'),
                dataIndex: 'values',
                render: (values: string[]) => (
                  <span style={{ overflowWrap: 'anywhere' }}>{values.join(', ') || '—'}</span>
                ),
              },
            ]}
            dataSource={Object.entries(visibleMetadataData.attributes)
              .sort(([a], [b]) => a.localeCompare(b))
              .map(([name, values]) => ({ name, values }))}
          />
        </>
      )}
    </div>
  )
}
