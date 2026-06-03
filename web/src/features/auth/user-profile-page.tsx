import { useMemo, useState } from 'react'
import {
  Alert,
  App,
  Avatar,
  Button,
  Card,
  Descriptions,
  Empty,
  Form,
  Input,
  Modal,
  Popconfirm,
  Select,
  Skeleton,
  Space,
  Table,
  Tabs,
  Tag,
  Typography,
} from 'antd'
import type { DescriptionsProps, TableColumnsType, TabsProps } from 'antd'
import {
  ApiOutlined,
  IdcardOutlined,
  KeyOutlined,
  MailOutlined,
  PhoneOutlined,
  PlusOutlined,
  ReloadOutlined,
  SafetyCertificateOutlined,
  StopOutlined,
  UserOutlined,
} from '@ant-design/icons'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { ManagementIconButton } from '@/components/management-list'
import { StatusTag } from '@/components/status-tag'
import { hasPermission, usePermissionSnapshot } from '@/features/auth/permission-snapshot'
import { consolePermissionLabelMap } from '@/features/auth/permission-catalog'
import type { CreatedPersonalAccessToken, PersonalAccessToken } from '@/features/copilot/ai-gateway-model'
import { api } from '@/services/api-client'
import type { ApiResponse, LinkedIdentity, UserProfile, UserSession } from '@/types'
import { formatDateTime, formatRelativeTime } from '@/utils/time'
import './user-profile-page.css'

const { Paragraph, Text, Title } = Typography

type GatewayTokenExpiration = '7d' | '30d' | '90d' | 'never' | 'custom'

interface GatewayTokenFormValues {
  name: string
  permissionKeys?: string[]
  scopes?: string[]
  expiresIn?: GatewayTokenExpiration
  customExpiresAt?: string
}

const gatewayPermissionOptions = ['ai.gateway.invoke', 'ai.gateway.view'].map((value) => ({
  label: consolePermissionLabelMap[value] ? `${consolePermissionLabelMap[value]} (${value})` : value,
  value,
}))

const gatewayExpirationOptions = [
  { label: '7 天', value: '7d' },
  { label: '30 天', value: '30d' },
  { label: '90 天', value: '90d' },
  { label: '不过期', value: 'never' },
  { label: '自定义', value: 'custom' },
]

function compact(value?: null | string) {
  return String(value || '').trim()
}

function valueOrUnset(value?: null | string) {
  return compact(value) || '未设置'
}

function providerLabel(value?: string) {
  const normalized = compact(value).toLowerCase()
  const labels: Record<string, string> = {
    password: '账号密码',
    oidc: 'OIDC',
    oauth2: 'OAuth2',
    saml: 'SAML',
    feishu: '飞书',
    dingtalk: '钉钉',
    wecom: '企业微信',
  }
  return labels[normalized] || valueOrUnset(value)
}

function providerTag(value?: string) {
  const normalized = compact(value).toLowerCase()
  const color = normalized === 'password' ? 'default' : normalized === 'oidc' ? 'processing' : 'success'
  return <Tag color={color}>{providerLabel(value)}</Tag>
}

function tagList(items?: string[], empty = '暂无') {
  const values = (items ?? []).map(compact).filter(Boolean)
  if (values.length === 0) {
    return <Text type="secondary">{empty}</Text>
  }
  return (
    <Space size={[6, 6]} wrap>
      {values.map((item) => <Tag key={item}>{item}</Tag>)}
    </Space>
  )
}

function compactTagList(items?: string[], max = 3) {
  const values = (items ?? []).map(compact).filter(Boolean)
  if (values.length === 0) {
    return <Text type="secondary">-</Text>
  }
  return (
    <Space size={[4, 4]} wrap>
      {values.slice(0, max).map((item) => <Tag key={item}>{item}</Tag>)}
      {values.length > max ? <Tag>+{values.length - max}</Tag> : null}
    </Space>
  )
}

function metadataText(metadata: Record<string, unknown> | undefined, key: string) {
  const value = metadata?.[key]
  return typeof value === 'string' && value.trim() ? value.trim() : '-'
}

function gatewayTokenStatus(record: Pick<PersonalAccessToken, 'expiresAt' | 'revokedAt'>) {
  if (record.revokedAt) return 'revoked'
  if (record.expiresAt) {
    const expiresAt = new Date(record.expiresAt).getTime()
    if (Number.isFinite(expiresAt) && expiresAt <= Date.now()) return 'expired'
  }
  return 'active'
}

function gatewayTokenExpiresAt(values: GatewayTokenFormValues) {
  const expiresIn = values.expiresIn ?? '30d'
  if (expiresIn === 'never') return undefined
  if (expiresIn === 'custom') return compact(values.customExpiresAt) || undefined
  const days = expiresIn === '7d' ? 7 : expiresIn === '90d' ? 90 : 30
  const expiresAt = new Date()
  expiresAt.setDate(expiresAt.getDate() + days)
  return expiresAt.toISOString()
}

function validDateTime(value?: string) {
  if (!value) return true
  return Number.isFinite(new Date(value).getTime())
}

function defaultGatewayTokenName(profile?: UserProfile) {
  const base = compact(profile?.username) || compact(profile?.displayName) || 'user'
  return `${base}-gateway-key`
}

function tokenSummaryText(tokens: PersonalAccessToken[]) {
  const activeCount = tokens.filter((item) => gatewayTokenStatus(item) === 'active').length
  const expiredCount = tokens.filter((item) => gatewayTokenStatus(item) === 'expired').length
  const revokedCount = tokens.filter((item) => gatewayTokenStatus(item) === 'revoked').length
  return { activeCount, expiredCount, revokedCount }
}

export function UserProfilePage() {
  const { message } = App.useApp()
  const queryClient = useQueryClient()
  const [tokenForm] = Form.useForm<GatewayTokenFormValues>()
  const [createTokenOpen, setCreateTokenOpen] = useState(false)
  const [oneTimeToken, setOneTimeToken] = useState<{ title: string; value: string; prefix?: string } | null>(null)

  const { data, isLoading, error } = useQuery({
    queryKey: ['auth-profile'],
    queryFn: () => api.get<ApiResponse<UserProfile>>('/auth/profile'),
  })
  const permissionSnapshotQuery = usePermissionSnapshot()
  const snapshot = permissionSnapshotQuery.data?.data
  const canViewGatewayKeys = hasPermission(snapshot, 'ai.gateway.view')
  const canIssueGatewayKeys = hasPermission(snapshot, 'ai.gateway.invoke')

  const gatewayTokensQuery = useQuery({
    queryKey: ['ai-gateway', 'personal-access-tokens'],
    queryFn: () => api.get<ApiResponse<PersonalAccessToken[]>>('/ai-gateway/personal-access-tokens'),
    enabled: canViewGatewayKeys,
  })

  const profile = data?.data
  const gatewayTokens = gatewayTokensQuery.data?.data ?? []
  const gatewayTokenSummary = tokenSummaryText(gatewayTokens)
  const displayName = valueOrUnset(profile?.displayName || profile?.username)
  const avatarText = displayName === '未设置' ? 'U' : displayName.charAt(0).toUpperCase()
  const primaryIdentity = profile?.identities?.[0]

  const openCreateToken = () => {
    tokenForm.setFieldsValue({
      name: defaultGatewayTokenName(profile),
      permissionKeys: ['ai.gateway.invoke'],
      scopes: [],
      expiresIn: '30d',
      customExpiresAt: undefined,
    })
    setCreateTokenOpen(true)
  }

  const refreshGatewayTokens = () => queryClient.invalidateQueries({ queryKey: ['ai-gateway', 'personal-access-tokens'] })

  const createTokenMutation = useMutation<ApiResponse<CreatedPersonalAccessToken>, Error, GatewayTokenFormValues>({
    mutationFn: (values) => api.post<ApiResponse<CreatedPersonalAccessToken>>('/ai-gateway/personal-access-tokens', {
      name: values.name,
      permissionKeys: values.permissionKeys ?? [],
      scopes: values.scopes ?? [],
      expiresAt: gatewayTokenExpiresAt(values),
    }),
    onSuccess: (res) => {
      const created = res.data
      setCreateTokenOpen(false)
      tokenForm.resetFields()
      if (created?.value) {
        setOneTimeToken({
          title: 'Soha AI Gateway Login Key 已生成',
          value: created.value,
          prefix: created.token?.tokenPrefix,
        })
      }
      void refreshGatewayTokens()
      message.success('Gateway key 已生成')
    },
    onError: (mutationError) => message.error(mutationError.message),
  })

  const revokeTokenMutation = useMutation<unknown, Error, string>({
    mutationFn: (tokenId) => api.post(`/ai-gateway/personal-access-tokens/${tokenId}/revoke`),
    onSuccess: () => {
      void refreshGatewayTokens()
      message.success('Gateway key 已吊销')
    },
    onError: (mutationError) => message.error(mutationError.message),
  })

  const rotateTokenMutation = useMutation<ApiResponse<CreatedPersonalAccessToken>, Error, PersonalAccessToken>({
    mutationFn: (record) => api.post<ApiResponse<CreatedPersonalAccessToken>>(`/ai-gateway/personal-access-tokens/${record.id}/rotate`),
    onSuccess: (res) => {
      const created = res.data
      if (created?.value) {
        setOneTimeToken({
          title: 'Soha AI Gateway Login Key 已轮换',
          value: created.value,
          prefix: created.token?.tokenPrefix,
        })
      }
      void refreshGatewayTokens()
      message.success('Gateway key 已轮换')
    },
    onError: (mutationError) => message.error(mutationError.message),
  })

  const identityColumns = useMemo<TableColumnsType<LinkedIdentity>>(() => [
    {
      title: '登录方式',
      dataIndex: 'providerType',
      width: 120,
      render: (value: string) => providerTag(value),
    },
    {
      title: '提供方',
      dataIndex: 'providerId',
      width: 150,
      render: (value: string) => valueOrUnset(value),
    },
    {
      title: '外部账号',
      dataIndex: 'providerUserId',
      ellipsis: true,
      render: (value: string) => valueOrUnset(value),
    },
    {
      title: '关联资料',
      key: 'profile',
      width: 220,
      render: (_: unknown, record) => (
        <Space orientation="vertical" size={0}>
          <Text>{valueOrUnset(record.displayName || record.email)}</Text>
          {record.email ? <Text type="secondary">{record.email}</Text> : null}
        </Space>
      ),
    },
    {
      title: '最近登录',
      dataIndex: 'lastLoginAt',
      width: 180,
      render: (value: string) => formatDateTime(value),
    },
  ], [])

  const sessionColumns = useMemo<TableColumnsType<UserSession>>(() => [
    {
      title: '登录方式',
      dataIndex: 'providerType',
      width: 120,
      render: (value: string) => providerTag(value),
    },
    {
      title: '来源',
      key: 'source',
      width: 110,
      render: (_: unknown, record) => metadataText(record.metadata, 'source'),
    },
    {
      title: 'IP',
      key: 'sourceIp',
      width: 140,
      ellipsis: true,
      render: (_: unknown, record) => metadataText(record.metadata, 'sourceIp'),
    },
    {
      title: '设备',
      key: 'userAgent',
      ellipsis: true,
      render: (_: unknown, record) => metadataText(record.metadata, 'userAgent'),
    },
    {
      title: '状态',
      dataIndex: 'status',
      width: 110,
      render: (value: string) => <StatusTag value={value} />,
    },
    {
      title: '最近活跃',
      dataIndex: 'lastSeenAt',
      width: 170,
      render: (value: string) => (
        <Space orientation="vertical" size={0}>
          <Text>{formatDateTime(value)}</Text>
          <Text type="secondary">{formatRelativeTime(value)}</Text>
        </Space>
      ),
    },
    {
      title: '过期时间',
      dataIndex: 'expiresAt',
      width: 170,
      render: (value: string) => formatDateTime(value),
    },
  ], [])

  const gatewayTokenColumns = useMemo<TableColumnsType<PersonalAccessToken>>(() => [
    {
      title: 'Key',
      dataIndex: 'name',
      width: 240,
      render: (_: string, record) => (
        <Space orientation="vertical" size={0}>
          <Text strong>{record.name || record.id}</Text>
          <Text type="secondary" copyable={{ text: record.tokenPrefix }}>{record.tokenPrefix}</Text>
        </Space>
      ),
    },
    {
      title: '权限',
      dataIndex: 'permissionKeys',
      render: (value: string[]) => compactTagList(value, 3),
    },
    {
      title: 'Scopes',
      dataIndex: 'scopes',
      width: 160,
      render: (value: string[]) => compactTagList(value, 2),
    },
    {
      title: '最近使用',
      dataIndex: 'lastUsedAt',
      width: 160,
      render: (value: string) => (
        <Space orientation="vertical" size={0}>
          <Text>{formatDateTime(value)}</Text>
          {value ? <Text type="secondary">{formatRelativeTime(value)}</Text> : null}
        </Space>
      ),
    },
    {
      title: '过期时间',
      dataIndex: 'expiresAt',
      width: 160,
      render: (value: string) => formatDateTime(value),
    },
    {
      title: '状态',
      key: 'status',
      width: 110,
      render: (_: unknown, record) => <StatusTag value={gatewayTokenStatus(record)} />,
    },
    {
      title: '操作',
      key: 'actions',
      fixed: 'right',
      align: 'center',
      className: 'soha-table-actions-column',
      width: 104,
      render: (_: unknown, record) => (
        <Space className="soha-row-action-icons" size={2}>
          <Popconfirm
            title="轮换 Gateway key？"
            description="旧 key 会被吊销，新明文只展示一次。"
            okButtonProps={{ loading: rotateTokenMutation.isPending }}
            onConfirm={() => rotateTokenMutation.mutate(record)}
          >
            <ManagementIconButton
              size="small"
              tooltip="轮换"
              aria-label="轮换 Gateway key"
              icon={<ReloadOutlined />}
              loading={rotateTokenMutation.isPending}
              disabled={!canIssueGatewayKeys || !!record.revokedAt}
            />
          </Popconfirm>
          <Popconfirm
            title="吊销 Gateway key？"
            description="吊销后外部 AI Client 将不能再使用该 key 登录 Gateway。"
            okButtonProps={{ danger: true, loading: revokeTokenMutation.isPending }}
            onConfirm={() => revokeTokenMutation.mutate(record.id)}
          >
            <ManagementIconButton
              size="small"
              tooltip="吊销"
              aria-label="吊销 Gateway key"
              danger
              icon={<StopOutlined />}
              loading={revokeTokenMutation.isPending}
              disabled={!canIssueGatewayKeys || !!record.revokedAt}
            />
          </Popconfirm>
        </Space>
      ),
    },
  ], [canIssueGatewayKeys, revokeTokenMutation, rotateTokenMutation])

  const descriptionItems = useMemo<DescriptionsProps['items']>(() => [
    { key: 'username', label: '用户名', children: valueOrUnset(profile?.username) },
    { key: 'displayName', label: '显示名', children: displayName },
    { key: 'email', label: '邮箱', children: valueOrUnset(profile?.email) },
    { key: 'phone', label: '电话', children: valueOrUnset(profile?.phone) },
    { key: 'status', label: '账号状态', children: profile?.status ? <StatusTag value={profile.status} /> : '未设置' },
    { key: 'lastLoginAt', label: '最近登录', children: formatDateTime(profile?.lastLoginAt) },
    { key: 'provider', label: '主要登录方式', children: providerTag(primaryIdentity?.providerType) },
    { key: 'userId', label: '用户 ID', children: <Text copyable>{valueOrUnset(profile?.userId)}</Text>, span: 2 },
  ], [displayName, primaryIdentity?.providerType, profile])

  const permissionItems = useMemo<DescriptionsProps['items']>(() => [
    { key: 'roles', label: '角色', children: tagList(profile?.roles) },
    { key: 'teams', label: '组织', children: tagList(profile?.teams) },
    { key: 'projects', label: '项目', children: tagList(profile?.projects) },
    { key: 'tags', label: '标签', children: tagList(profile?.tags) },
  ], [profile])

  const tabItems = useMemo<TabsProps['items']>(() => [
    {
      key: 'identities',
      label: '关联登录',
      children: (
        <Table
          columns={identityColumns}
          dataSource={profile?.identities ?? []}
          locale={{ emptyText: <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="暂无关联登录方式" /> }}
          pagination={false}
          rowKey="id"
          scroll={{ x: 760 }}
          size="small"
        />
      ),
    },
    {
      key: 'sessions',
      label: '活跃会话',
      children: (
        <Table
          columns={sessionColumns}
          dataSource={profile?.sessions ?? []}
          locale={{ emptyText: <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="暂无活跃会话" /> }}
          pagination={false}
          rowKey="id"
          scroll={{ x: 900 }}
          size="small"
        />
      ),
    },
    {
      key: 'access',
      label: '权限归属',
      children: (
        <Descriptions
          bordered
          column={1}
          items={permissionItems}
          size="small"
        />
      ),
    },
  ], [identityColumns, permissionItems, profile?.identities, profile?.sessions, sessionColumns])

  if (error) {
    return (
      <div className="soha-page">
        <Alert
          type="error"
          showIcon
          message="个人中心加载失败"
          description={(error as Error).message}
        />
      </div>
    )
  }

  if (isLoading || !profile) {
    return (
      <div className="soha-page soha-profile-page">
        <Skeleton active paragraph={{ rows: 10 }} />
      </div>
    )
  }

  const gatewayKeysUnavailable = !permissionSnapshotQuery.isLoading && !canViewGatewayKeys
  const gatewayKeysSummary = canViewGatewayKeys
    ? `${gatewayTokenSummary.activeCount} active / ${gatewayTokens.length} total`
    : '未授权'

  return (
    <div className="soha-page soha-profile-page">
      <div className="soha-profile-page-header">
        <div className="soha-profile-page-header__main">
          <Text className="soha-profile-eyebrow">ACCOUNT CENTER</Text>
          <Title level={3}>个人中心</Title>
        </div>
        <Space size={8} wrap>
          <StatusTag value={profile.status} />
          {providerTag(primaryIdentity?.providerType)}
        </Space>
      </div>

      <div className="soha-profile-layout">
        <Card className="soha-profile-summary-card">
          <div className="soha-profile-identity">
            <Avatar className="soha-profile-avatar" size={64}>
              {avatarText}
            </Avatar>
            <div className="soha-profile-identity__main">
              <Title level={4}>{displayName}</Title>
              <Text type="secondary">{profile.email || profile.username}</Text>
            </div>
          </div>
          <div className="soha-profile-summary-list">
            <div className="soha-profile-summary-item">
              <UserOutlined />
              <span>{valueOrUnset(profile.username)}</span>
            </div>
            <div className="soha-profile-summary-item">
              <MailOutlined />
              <span>{valueOrUnset(profile.email)}</span>
            </div>
            <div className="soha-profile-summary-item">
              <PhoneOutlined />
              <span>{valueOrUnset(profile.phone)}</span>
            </div>
            <div className="soha-profile-summary-item">
              <KeyOutlined />
              <span>{providerLabel(primaryIdentity?.providerType)}</span>
            </div>
          </div>
          <div className="soha-profile-kpi-grid">
            <div>
              <Text type="secondary">活跃会话</Text>
              <strong>{profile.sessions?.length ?? 0}</strong>
            </div>
            <div>
              <Text type="secondary">Gateway keys</Text>
              <strong>{gatewayKeysSummary}</strong>
            </div>
          </div>
          <div className="soha-profile-tag-section">
            <Text type="secondary">角色</Text>
            {tagList(profile.roles)}
          </div>
          <div className="soha-profile-tag-section">
            <Text type="secondary">组织</Text>
            {tagList(profile.teams)}
          </div>
        </Card>

        <div className="soha-profile-main">
          <Card
            title={(
              <Space>
                <IdcardOutlined />
                <span>账号资料</span>
              </Space>
            )}
            extra={<StatusTag value={profile.status} />}
          >
            <Descriptions
              bordered
              column={{ xs: 1, md: 2 }}
              items={descriptionItems}
              size="small"
            />
          </Card>

          <Card
            className="soha-profile-gateway-card"
            title={(
              <Space>
                <ApiOutlined />
                <span>AI Gateway Login Key</span>
              </Space>
            )}
            extra={(
              <Button
                size="small"
                type="primary"
                icon={<PlusOutlined />}
                disabled={!canIssueGatewayKeys}
                onClick={openCreateToken}
              >
                生成 key
              </Button>
            )}
          >
            <div className="soha-profile-gateway-overview">
              <div>
                <Text strong>面向当前用户的 Soha Gateway 登录凭证</Text>
                <Paragraph type="secondary">
                  用于 soha-cli、MCP 客户端或外部 AI Client 以你的用户身份调用 Soha AI Gateway。明文只在生成或轮换后展示一次。
                </Paragraph>
              </div>
              <Space size={6} wrap>
                <Tag color="success">active {gatewayTokenSummary.activeCount}</Tag>
                <Tag>expired {gatewayTokenSummary.expiredCount}</Tag>
                <Tag>revoked {gatewayTokenSummary.revokedCount}</Tag>
              </Space>
            </div>

            {gatewayKeysUnavailable ? (
              <Alert
                type="warning"
                showIcon
                message="当前账号没有 AI Gateway key 查看权限"
                description="需要 ai.gateway.view 查看已有 key，需要 ai.gateway.invoke 生成、轮换或吊销个人 key。"
              />
            ) : (
              <Table
                columns={gatewayTokenColumns}
                dataSource={gatewayTokens}
                loading={gatewayTokensQuery.isLoading || permissionSnapshotQuery.isLoading}
                locale={{ emptyText: <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="暂无 AI Gateway login key" /> }}
                pagination={false}
                rowKey="id"
                scroll={{ x: 980 }}
                size="small"
              />
            )}
          </Card>

          <Card
            title={(
              <Space>
                <SafetyCertificateOutlined />
                <span>安全与访问</span>
              </Space>
            )}
          >
            <Tabs items={tabItems} size="small" />
          </Card>
        </div>
      </div>

      <Modal
        title="生成 Soha AI Gateway Login Key"
        open={createTokenOpen}
        okText="生成 key"
        cancelText="取消"
        confirmLoading={createTokenMutation.isPending}
        destroyOnHidden
        onCancel={() => setCreateTokenOpen(false)}
        onOk={() => tokenForm.submit()}
      >
        <Form
          form={tokenForm}
          layout="vertical"
          initialValues={{ permissionKeys: ['ai.gateway.invoke'], scopes: [], expiresIn: '30d' }}
          onFinish={(values) => createTokenMutation.mutate(values)}
        >
          <Form.Item name="name" label="Key 名称" rules={[{ required: true, message: '请输入 key 名称' }]}>
            <Input placeholder="例如 codex-local" />
          </Form.Item>
          <Form.Item name="permissionKeys" label="权限 keys">
            <Select mode="tags" tokenSeparators={[',', ' ']} options={gatewayPermissionOptions} />
          </Form.Item>
          <Form.Item name="scopes" label="Scopes">
            <Select mode="tags" tokenSeparators={[',', ' ']} placeholder="留空表示不额外收窄" />
          </Form.Item>
          <Form.Item name="expiresIn" label="有效期">
            <Select options={gatewayExpirationOptions} />
          </Form.Item>
          <Form.Item noStyle shouldUpdate={(prev, next) => prev.expiresIn !== next.expiresIn}>
            {({ getFieldValue }) => getFieldValue('expiresIn') === 'custom' ? (
              <Form.Item
                name="customExpiresAt"
                label="自定义过期时间"
                rules={[
                  { required: true, message: '请输入自定义过期时间' },
                  {
                    validator: (_, value) => (
                      validDateTime(value)
                        ? Promise.resolve()
                        : Promise.reject(new Error('请输入有效的 RFC3339 时间'))
                    ),
                  },
                ]}
              >
                <Input placeholder="例如 2026-06-30T00:00:00Z" />
              </Form.Item>
            ) : null}
          </Form.Item>
        </Form>
      </Modal>

      <Modal
        title={oneTimeToken?.title}
        open={!!oneTimeToken}
        okText="我已保存"
        cancelButtonProps={{ style: { display: 'none' } }}
        destroyOnHidden
        onCancel={() => setOneTimeToken(null)}
        onOk={() => setOneTimeToken(null)}
      >
        <Space orientation="vertical" size={12} style={{ width: '100%' }}>
          <Alert
            type="warning"
            showIcon
            message="明文只展示一次"
            description="关闭后无法再次查看完整 key；如果丢失，需要轮换或重新生成。"
          />
          {oneTimeToken?.prefix ? (
            <Text type="secondary">Prefix: {oneTimeToken.prefix}</Text>
          ) : null}
          <Input.TextArea
            className="soha-profile-secret-value"
            readOnly
            autoSize={{ minRows: 3, maxRows: 6 }}
            value={oneTimeToken?.value}
          />
          <Paragraph copyable={{ text: oneTimeToken?.value ?? '' }} type="secondary">
            复制完整 key
          </Paragraph>
        </Space>
      </Modal>
    </div>
  )
}
