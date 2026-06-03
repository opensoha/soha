import { useMemo } from 'react'
import { Alert, Avatar, Card, Descriptions, Empty, Skeleton, Space, Table, Tabs, Tag, Typography } from 'antd'
import type { DescriptionsProps, TableColumnsType, TabsProps } from 'antd'
import {
  IdcardOutlined,
  KeyOutlined,
  MailOutlined,
  PhoneOutlined,
  SafetyCertificateOutlined,
  UserOutlined,
} from '@ant-design/icons'
import { useQuery } from '@tanstack/react-query'
import { StatusTag } from '@/components/status-tag'
import { api } from '@/services/api-client'
import type { ApiResponse, LinkedIdentity, UserProfile, UserSession } from '@/types'
import { formatDateTime, formatRelativeTime } from '@/utils/time'

const { Text, Title } = Typography

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

function metadataText(metadata: Record<string, unknown> | undefined, key: string) {
  const value = metadata?.[key]
  return typeof value === 'string' && value.trim() ? value.trim() : '-'
}

export function UserProfilePage() {
  const { data, isLoading, error } = useQuery({
    queryKey: ['auth-profile'],
    queryFn: () => api.get<ApiResponse<UserProfile>>('/auth/profile'),
  })

  const profile = data?.data
  const displayName = valueOrUnset(profile?.displayName || profile?.username)
  const avatarText = displayName === '未设置' ? 'U' : displayName.charAt(0).toUpperCase()
  const primaryIdentity = profile?.identities?.[0]

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
    { key: 'teams', label: '用户组', children: tagList(profile?.teams) },
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
          title="个人信息加载失败"
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

  return (
    <div className="soha-page soha-profile-page">
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
          <div className="soha-profile-tag-section">
            <Text type="secondary">角色</Text>
            {tagList(profile.roles)}
          </div>
          <div className="soha-profile-tag-section">
            <Text type="secondary">用户组</Text>
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
    </div>
  )
}
