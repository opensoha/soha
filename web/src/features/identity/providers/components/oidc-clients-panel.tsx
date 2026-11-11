import { AdminTable } from '@/components/admin-table'
import {
  ManagementDensityButton,
  ManagementIconButton,
  ManagementRefreshButton,
  ManagementState,
  ManagementTableToolbar,
} from '@/components/management-list'
import { ReadFeedback } from '@/components/request-feedback'
import { MetadataTag, StatusTag } from '@/components/status-tag'
import { useOperationMutation } from '@/components/use-operation-mutation'
import { useI18n } from '@/i18n'
import { isApiError } from '@/services/api-error'
import { queryAccessDenied, readableQueryData } from '@/utils/query-feedback'
import { tableColumnPresets } from '@/utils/table-columns'
import {
  DeleteOutlined,
  EditOutlined,
  EyeOutlined,
  KeyOutlined,
  PlusOutlined,
} from '@ant-design/icons'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import type { TableColumnsType } from 'antd'
import { Button, Popconfirm, Space, Typography } from 'antd'
import { useMemo, useState } from 'react'
import { createIdentityOIDCClient, revealIdentityOIDCClientSecret } from '../api'
import { identityProviderKeys } from '../keys'
import { identityProviderMutations } from '../mutations'
import { formatIdentityProviderDateTime, identityProviderTagsSummary } from '../presentation'
import { identityProviderQueries } from '../queries'
import type {
  IdentityOIDCClient,
  IdentityOIDCClientInput,
  IdentityOIDCClientStatus,
  IdentityProvider,
} from '../types'
import { OIDCClientFormModal } from './oidc-client-form-modal'
import type { IdentityOIDCSecretReveal } from './secret-reveal-modal'
const { Text } = Typography
interface OIDCClientsPanelProps {
  canCreate: boolean
  canUpdate: boolean
  canDelete: boolean
  canRotate: boolean
  onSecretCreated: (secret: IdentityOIDCSecretReveal) => void
  provider: IdentityProvider
}
export function OIDCClientsPanel({
  canCreate,
  canUpdate,
  canDelete,
  canRotate,
  onSecretCreated,
  provider,
}: OIDCClientsPanelProps) {
  const { t } = useI18n()
  const queryClient = useQueryClient()
  const [modalOpen, setModalOpen] = useState(false)
  const [editing, setEditing] = useState<IdentityOIDCClient | null>(null)
  const [creatingClient, setCreatingClient] = useState(false)
  const [revealingClientId, setRevealingClientId] = useState('')
  const [tableSize, setTableSize] = useState<'small' | 'middle'>('small')
  const clientsQuery = useQuery(
    identityProviderQueries.oidcClients(provider.id, provider.type === 'oidc'),
  )
  const visibleClientsQueryData =
    clientsQuery.isEnabled === false ? undefined : readableQueryData(clientsQuery)
  const updateMutation = useOperationMutation(
    identityProviderMutations.updateOIDCClient(queryClient),
    {
      label: ['保存 OIDC Client', 'Save OIDC client provider'],
      target: (v) => v.providerId + ' / ' + v.clientId,
    },
  )
  const deleteMutation = useOperationMutation(
    identityProviderMutations.removeOIDCClient(queryClient),
    {
      label: ['删除 OIDC Client', 'Delete OIDC client provider'],
      target: (v) => v.providerId + ' / ' + v.clientId,
    },
  )
  const rotateSigningKeyMutation = useOperationMutation(
    identityProviderMutations.rotateSigningKey(queryClient),
    {
      label: ['轮换签名密钥', 'Rotate signing key provider'],
      target: (v) => v,
      receipt: (key) => ({ message: '签名密钥已轮换', evidence: `kid: ${key.kid}` }),
    },
  )
  const closeModal = () => {
    setModalOpen(false)
    setEditing(null)
  }
  const openCreate = () => {
    if (operationBlocked) return
    setEditing(null)
    setModalOpen(true)
  }
  const openEdit = (client: IdentityOIDCClient) => {
    if (operationBlocked) return
    setEditing(client)
    setModalOpen(true)
  }
  const createClientMutation = useOperationMutation(
    {
      gcTime: 0,
      mutationFn: async (input: IdentityOIDCClientInput) => {
        const result = await createIdentityOIDCClient({ providerId: provider.id, input })
        if (result.clientSecret)
          onSecretCreated({ clientId: result.client.clientId, clientSecret: result.clientSecret })
        return result.client
      },
      onSuccess: () =>
        queryClient.invalidateQueries({ queryKey: identityProviderKeys.oidcClients(provider.id) }),
    },
    {
      label: ['创建 OIDC Client', 'Create OIDC client'],
      target: (input) => provider.id + ' / ' + input.clientId,
    },
  )
  const revealMutation = useOperationMutation(
    {
      gcTime: 0,
      mutationFn: async (clientId: string) => {
        const secret = await revealIdentityOIDCClientSecret(clientId)
        onSecretCreated({ clientId: secret.clientId, clientSecret: secret.clientSecret })
        return secret.clientId
      },
    },
    {
      label: ['读取客户端凭据', 'Read client credentials'],
      target: (id) => id,
      readOnly: true,
      receipt: () => ({ message: '凭据已读取', evidence: '敏感值仅在原有专用窗口临时展示。' }),
    },
  )
  const revealSecret = async (client: IdentityOIDCClient) => {
    setRevealingClientId(client.id)
    try {
      await revealMutation.mutateAsync(client.id)
    } catch {
      /* The inline receipt owns the read error. */
    } finally {
      setRevealingClientId('')
    }
  }
  const createClient = async (input: IdentityOIDCClientInput) => {
    setCreatingClient(true)
    try {
      await createClientMutation.mutateAsync(input)
      closeModal()
    } catch {
      /* Keep the draft and the original operation feedback. */
    } finally {
      setCreatingClient(false)
    }
  }
  const operationBlocked =
    updateMutation.blocked ||
    deleteMutation.blocked ||
    rotateSigningKeyMutation.blocked ||
    createClientMutation.blocked ||
    (clientsQuery.isEnabled !== false && queryAccessDenied(clientsQuery))
  const submitForm = (input: IdentityOIDCClientInput) => {
    if (editing) {
      updateMutation.mutate(
        { providerId: provider.id, clientId: editing.id, input },
        {
          onSuccess: () => {
            closeModal()
          },
        },
      )
      return
    }
    void createClient(input)
  }
  const columns = useMemo<TableColumnsType<IdentityOIDCClient>>(
    () => [
      {
        title: t('identity.providers.client.id'),
        dataIndex: 'clientId',
        width: 240,
        render: (value: string, record) => (
          <Space orientation="vertical" size={2}>
            <Text strong ellipsis title={value}>
              {value}
            </Text>
            <Text type="secondary" ellipsis title={record.id}>
              {record.id}
            </Text>
          </Space>
        ),
      },
      {
        title: t('identity.providers.client.redirectUris'),
        key: 'redirectUris',
        width: 320,
        render: (_, record) =>
          identityProviderTagsSummary([
            ...record.redirectUris,
            ...(record.redirectUriRegexes ?? []).map((pattern) => `正则: ${pattern}`),
          ]),
      },
      {
        title: t('identity.providers.client.type'),
        dataIndex: 'clientType',
        width: 120,
        render: (value: string) => <MetadataTag label={value || 'confidential'} />,
      },
      {
        title: t('identity.providers.client.scopes'),
        dataIndex: 'allowedScopes',
        width: 220,
        render: (values: string[]) => identityProviderTagsSummary(values),
      },
      {
        title: t('identity.providers.client.grantTypes'),
        dataIndex: 'allowedGrantTypes',
        width: 180,
        render: (values: string[]) => identityProviderTagsSummary(values),
      },
      {
        title: t('identity.providers.client.ttl'),
        key: 'ttl',
        width: 170,
        render: (_, record) => (
          <Space orientation="vertical" size={2}>
            <Text>access {record.accessTokenTtlSeconds}s</Text>
            <Text type="secondary">id {record.idTokenTtlSeconds}s</Text>
          </Space>
        ),
      },
      {
        ...tableColumnPresets.status,
        title: t('identity.providers.client.status'),
        dataIndex: 'status',
        render: (value: IdentityOIDCClientStatus, record) => (
          <Space orientation="vertical" size={2}>
            <StatusTag value={value} />
            <MetadataTag
              label={record.requirePkce ? 'PKCE' : 'No PKCE'}
              tone={record.requirePkce ? 'blue' : 'default'}
            />
          </Space>
        ),
      },
      {
        ...tableColumnPresets.datetime,
        title: t('identity.providers.client.updated'),
        dataIndex: 'updatedAt',
        render: formatIdentityProviderDateTime,
      },
      {
        ...tableColumnPresets.action,
        key: 'actions',
        render: (_, record) => (
          <Space size={4}>
            <Popconfirm
              cancelText="取消"
              description="查看操作会记录到审计日志。"
              disabled={
                operationBlocked ||
                !canUpdate ||
                record.clientType === 'public' ||
                record.clientSecretAvailable === false
              }
              okButtonProps={{
                ...{ loading: revealingClientId === record.id },
                disabled: operationBlocked,
              }}
              okText="查看"
              title={`查看 ${record.clientId} 的 Client Secret`}
              onConfirm={() => void revealSecret(record)}
            >
              <ManagementIconButton
                aria-label="查看 Client Secret"
                disabled={
                  !canUpdate ||
                  record.clientType === 'public' ||
                  record.clientSecretAvailable === false
                }
                icon={<EyeOutlined />}
                tooltip={
                  record.clientType === 'public'
                    ? 'Public client 不使用 Secret'
                    : record.clientSecretAvailable === false
                      ? 'Secret 不可查看，请编辑并设置新 Secret'
                      : '查看 Client Secret'
                }
              />
            </Popconfirm>
            <ManagementIconButton
              disabled={operationBlocked || !canUpdate}
              icon={<EditOutlined />}
              onClick={() => openEdit(record)}
              tooltip="编辑"
            />
            <Popconfirm
              cancelText="取消"
              disabled={operationBlocked || !canDelete}
              okButtonProps={{
                ...{ danger: true, loading: deleteMutation.isPending },
                disabled: operationBlocked,
              }}
              okText="删除"
              onConfirm={() =>
                deleteMutation.mutate({ providerId: provider.id, clientId: record.id })
              }
              title={`删除 ${record.clientId}`}
            >
              <Button danger disabled={!canDelete} icon={<DeleteOutlined />} size="small" />
            </Popconfirm>
          </Space>
        ),
      },
    ],
    [
      canDelete,
      canUpdate,
      deleteMutation,
      onSecretCreated,
      provider.id,
      queryClient,
      revealSecret,
      revealingClientId,
      t,
    ],
  )
  if (provider.type !== 'oidc') {
    return (
      <ManagementState
        compact
        description="OIDC client 仅适用于 OIDC Provider。"
        kind="unsupported"
        title="非 OIDC Provider"
      />
    )
  }
  if (clientsQuery.isError && !visibleClientsQueryData) {
    return (
      <ManagementState
        kind={
          isApiError(clientsQuery.error) && clientsQuery.error.status === 403
            ? 'no-permission'
            : 'error'
        }
        title={t('identity.providers.client.loadError')}
        description={clientsQuery.error.message}
        actions={
          <Button
            onClick={() =>
              void (clientsQuery.isEnabled === false ? undefined : clientsQuery.refetch())
            }
          >
            {t('common.retry')}
          </Button>
        }
      />
    )
  }
  return (
    <div className="soha-identity-oidc-panel">
      {!(modalOpen && !(clientsQuery.isEnabled !== false && queryAccessDenied(clientsQuery))) && (
        <>
          {' '}
          {createClientMutation.feedback}
          {revealMutation.feedback}
          {updateMutation.feedback}
          {deleteMutation.feedback}
          {rotateSigningKeyMutation.feedback}{' '}
        </>
      )}

      <ReadFeedback canRead={clientsQuery.isEnabled} query={clientsQuery} label="OIDC Clients" />

      <AdminTable
        shellClassName="soha-management-table-shell"
        columnSettingPlacement="header"
        columnSettingIconOnly
        tableSize={tableSize}
        rowKey="id"
        columns={columns}
        dataSource={visibleClientsQueryData ?? []}
        empty={
          <ManagementState
            description="创建 client 后可接入下游 OIDC 应用。"
            kind="empty"
            title="暂无 OIDC client"
          />
        }
        loading={clientsQuery.isLoading || clientsQuery.isFetching}
        headerExtra={
          <ManagementTableToolbar>
            <Popconfirm
              cancelText="取消"
              description="旧公钥会保留 24 小时，之后使用旧密钥签发的令牌将无法验证。"
              disabled={operationBlocked || !canRotate}
              okButtonProps={{
                ...{ loading: rotateSigningKeyMutation.isPending },
                disabled: operationBlocked,
              }}
              okText="轮换"
              onConfirm={() => rotateSigningKeyMutation.mutate(provider.id)}
              title="轮换 OIDC 签名密钥"
            >
              <ManagementIconButton
                aria-label="轮换签名密钥"
                disabled={!canRotate}
                icon={<KeyOutlined />}
                tooltip="轮换签名密钥"
              />
            </Popconfirm>
            <Button
              disabled={operationBlocked || !canCreate}
              icon={<PlusOutlined />}
              onClick={openCreate}
              size="small"
              type="primary"
            >
              新建 client
            </Button>
            <ManagementDensityButton
              aria-label={t('table.density')}
              tooltip={t('table.density')}
              onClick={() => setTableSize((size) => (size === 'small' ? 'middle' : 'small'))}
            />
            <ManagementRefreshButton
              aria-label={t('common.refresh')}
              tooltip={t('common.refresh')}
              loading={clientsQuery.isFetching}
              onClick={() =>
                void (clientsQuery.isEnabled === false ? undefined : clientsQuery.refetch())
              }
            />
          </ManagementTableToolbar>
        }
      />

      <OIDCClientFormModal
        blocked={operationBlocked}
        feedback={
          <>
            {createClientMutation.feedback}
            {updateMutation.feedback}
          </>
        }
        editing={editing}
        onCancel={() => {
          if (!operationBlocked) closeModal()
        }}
        onSubmit={submitForm}
        open={modalOpen && !(clientsQuery.isEnabled !== false && queryAccessDenied(clientsQuery))}
        providerId={provider.id}
        submitting={creatingClient || updateMutation.isPending}
      />
    </div>
  )
}
