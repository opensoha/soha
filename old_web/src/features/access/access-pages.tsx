import { useMemo, useState } from 'react'
import { Alert, App, Avatar, Button, Col, Form, Input, InputNumber, Modal, Popconfirm, Row, Select, Space, Switch, Tag, Typography } from 'antd'
import { DeleteOutlined, EditOutlined, PlusOutlined } from '@ant-design/icons'
import type { TableColumnsType } from 'antd'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Navigate } from 'react-router-dom'
import { AdminTable } from '@/components/admin-table'
import { consolePermissionGroups, consolePermissionLabelMap } from '@/features/auth/permission-catalog'
import { hasPermission, usePermissionSnapshot } from '@/features/auth/permission-snapshot'
import { StatusTag } from '@/components/status-tag'
import { api } from '@/services/api-client'
import type { ApiResponse, BusinessLine, DeliveryEnvironment, ScopeGrant } from '@/types'
import { formatDateTime } from '@/utils/time'
import { tableColumnPresets } from '@/utils/table-columns'

const { Text } = Typography

const ACCESS_ACTION_OPTIONS = [
  { value: 'view', label: '查看 (view)' },
  { value: 'list', label: '列表 (list)' },
  { value: 'watch', label: '监听 (watch)' },
  { value: 'update', label: '修改 (update)' },
  { value: 'delete', label: '删除 (delete)' },
  { value: 'restart', label: '重启 (restart)' },
  { value: 'scale', label: '伸缩 (scale)' },
  { value: 'logs', label: '日志 (logs)' },
  { value: 'exec', label: 'Exec (exec)' },
]

const USER_STATUS_OPTIONS = [
  { value: 'active', label: '启用' },
  { value: 'disabled', label: '停用' },
]

const ROLE_SCOPE_OPTIONS = [
  { value: 'system', label: '系统角色' },
  { value: 'custom', label: '自定义角色' },
]

const POLICY_EFFECT_OPTIONS = [
  { value: 'allow', label: '允许' },
  { value: 'deny', label: '拒绝' },
]

type ColumnProps<T> = TableColumnsType<T>[number]

interface AccessUser {
  id: string
  username: string
  email: string
  displayName: string
  status: string
  lastLoginAt?: string
  tags: string[]
  roles: string[]
  teams: string[]
  projects: string[]
}

interface AccessRole {
  id: string
  name: string
  scope: string
  capabilities: string[]
  permissionKeys?: string[]
  userCount: number
}

interface AccessTeam {
  id: string
  name: string
  slug: string
  metadata: Record<string, unknown>
  userCount: number
}

interface AccessPolicy {
  id: string
  name: string
  effect: string
  priority: number
  subjects: {
    roles: string[]
    teams: string[]
    projects: string[]
    users: string[]
    tags: string[]
  }
  clusters: {
    ids: string[]
    regions: string[]
    environments: string[]
    labels: Record<string, string[]>
  }
  namespaces: {
    names: string[]
    ownerTeams: string[]
    labels: Record<string, string[]>
  }
  resources: {
    kinds: string[]
    names: string[]
    labels: Record<string, string[]>
  }
  actions: string[]
  conditions: {
    sources: string[]
    approvalStates: string[]
  }
  reason: string
}

function parseCSV(value: unknown) {
  return String(value ?? '')
    .split(',')
    .map((item) => item.trim())
    .filter(Boolean)
}

function toStringArray(value: unknown) {
  if (!Array.isArray(value)) {
    return []
  }
  return value
    .map((item) => String(item).trim())
    .filter(Boolean)
}

function joinCSV(items?: string[]) {
  return items?.join(', ') ?? ''
}

function getUserLabel(user?: Pick<AccessUser, 'displayName' | 'username' | 'email'> | null) {
  if (!user) {
    return '用户'
  }
  return user.displayName || user.username || user.email || '用户'
}

function getUserInitial(user: Pick<AccessUser, 'displayName' | 'username' | 'email'>) {
  const source = getUserLabel(user)
  const first = Array.from(source.trim())[0]
  return (first ?? 'U').toUpperCase()
}

function getGroupDescription(metadata?: Record<string, unknown>) {
  return String(metadata?.description ?? '').trim()
}

function renderMappedTags(values: string[], labelMap: Record<string, string>, emptyText = '-') {
  if (!values?.length) {
    return emptyText
  }
  return (
    <Space wrap size={4}>
      {values.map((value) => (
        <Tag key={value}>
          {labelMap[value] || value}
        </Tag>
      ))}
    </Space>
  )
}

function getRolePermissionSelectOptions() {
  return consolePermissionGroups.map((group) => ({
    label: group.label,
    options: group.options.map((option) => ({
      value: option.value,
      label: `${option.label} (${option.value})`,
    })),
  }))
}

function normalizePermissionKeys(value: unknown) {
  return toStringArray(value).sort((left, right) => left.localeCompare(right))
}

function buildPolicySubjectsSummary(policy: AccessPolicy, roleMap: Record<string, string>, teamMap: Record<string, string>) {
  const parts: string[] = []
  if (policy.subjects?.roles?.length) {
    parts.push(`角色: ${policy.subjects.roles.map((item) => roleMap[item] || item).join(', ')}`)
  }
  if (policy.subjects?.teams?.length) {
    parts.push(`用户组: ${policy.subjects.teams.map((item) => teamMap[item] || item).join(', ')}`)
  }
  if (policy.subjects?.users?.length) {
    parts.push(`用户: ${policy.subjects.users.join(', ')}`)
  }
  if (policy.subjects?.tags?.length) {
    parts.push(`标签: ${policy.subjects.tags.join(', ')}`)
  }
  return parts.join(' | ') || '全部主体'
}

function buildPolicyTargetsSummary(policy: AccessPolicy, teamMap: Record<string, string>) {
  const parts: string[] = []
  if (policy.clusters?.environments?.length) {
    parts.push(`环境: ${policy.clusters.environments.join(', ')}`)
  }
  if (policy.clusters?.regions?.length) {
    parts.push(`地域: ${policy.clusters.regions.join(', ')}`)
  }
  if (policy.clusters?.ids?.length) {
    parts.push(`集群: ${policy.clusters.ids.join(', ')}`)
  }
  if (policy.namespaces?.names?.length) {
    parts.push(`命名空间: ${policy.namespaces.names.join(', ')}`)
  }
  if (policy.namespaces?.ownerTeams?.length) {
    parts.push(`归属组: ${policy.namespaces.ownerTeams.map((item) => teamMap[item] || item).join(', ')}`)
  }
  if (policy.resources?.kinds?.length) {
    parts.push(`资源: ${policy.resources.kinds.join(', ')}`)
  }
  if (policy.resources?.names?.length) {
    parts.push(`对象: ${policy.resources.names.join(', ')}`)
  }
  return parts.join(' | ') || '全部资源'
}

function useCRUD<T extends { id: string }>(
  resource: string,
  options?: {
    invalidateKeys?: string[][]
  },
) {
  const { message } = App.useApp()
  const queryClient = useQueryClient()
  const [modalVisible, setModalVisible] = useState(false)
  const [editing, setEditing] = useState<T | null>(null)

  const invalidateAll = () => {
    queryClient.invalidateQueries({ queryKey: [resource] })
    options?.invalidateKeys?.forEach((key) => queryClient.invalidateQueries({ queryKey: key }))
  }

  const query = useQuery({
    queryKey: [resource],
    queryFn: () => api.get<ApiResponse<T[]>>(`/${resource}`),
  })

  const createMutation = useMutation({
    mutationFn: (values: Record<string, unknown>) => api.post(`/${resource}`, values),
    onSuccess: () => {
      message.success('创建成功')
      invalidateAll()
      setModalVisible(false)
    },
    onError: (err: Error) => message.error(err.message),
  })

  const updateMutation = useMutation({
    mutationFn: ({ id, values }: { id: string; values: Record<string, unknown> }) =>
      api.put(`/${resource}/${id}`, values),
    onSuccess: () => {
      message.success('更新成功')
      invalidateAll()
      setModalVisible(false)
      setEditing(null)
    },
    onError: (err: Error) => message.error(err.message),
  })

  const deleteMutation = useMutation({
    mutationFn: (id: string) => api.delete(`/${resource}/${id}`),
    onSuccess: () => {
      message.success('删除成功')
      invalidateAll()
    },
    onError: (err: Error) => message.error(err.message),
  })

  const handleSubmit = (values: Record<string, unknown>) => {
    if (editing) {
      updateMutation.mutate({ id: editing.id, values })
      return
    }
    createMutation.mutate(values)
  }

  const openCreate = () => {
    setEditing(null)
    setModalVisible(true)
  }

  const openEdit = (record: T) => {
    setEditing(record)
    setModalVisible(true)
  }

  const closeModal = () => {
    setModalVisible(false)
    setEditing(null)
  }

  return {
    data: query.data?.data ?? [],
    isLoading: query.isLoading,
    modalVisible,
    editing,
    openCreate,
    openEdit,
    closeModal,
    handleSubmit,
    deleteMutation,
    isSaving: createMutation.isPending || updateMutation.isPending,
  }
}

function ScopeGrantManager({
  subjectType,
  subjectId,
  visible,
  title,
  onClose,
}: {
  subjectType: 'user' | 'team'
  subjectId: string | null
  visible: boolean
  title: string
  onClose: () => void
}) {
  const { message } = App.useApp()
  const permissionSnapshotQuery = usePermissionSnapshot()
  const canManageScopeGrants = hasPermission(permissionSnapshotQuery.data?.data, 'access.scope-grants.manage')
  const queryClient = useQueryClient()
  const [form] = Form.useForm<Record<string, unknown>>()
  const [editing, setEditing] = useState<ScopeGrant | null>(null)
  const [grantModalVisible, setGrantModalVisible] = useState(false)

  const grantsQuery = useQuery({
    queryKey: ['scope-grants'],
    queryFn: () => api.get<ApiResponse<ScopeGrant[]>>('/access/scope-grants'),
    enabled: visible,
  })
  const businessLinesQuery = useQuery({
    queryKey: ['business-lines'],
    queryFn: () => api.get<ApiResponse<BusinessLine[]>>('/business-lines'),
    enabled: visible,
  })
  const environmentsQuery = useQuery({
    queryKey: ['delivery-environments'],
    queryFn: () => api.get<ApiResponse<DeliveryEnvironment[]>>('/delivery-environments'),
    enabled: visible,
  })
  const applicationsQuery = useQuery({
    queryKey: ['applications'],
    queryFn: () => api.get<ApiResponse<Array<{ id: string; name: string }>>>('/applications'),
    enabled: visible,
  })

  const businessLineMap = useMemo(
    () => Object.fromEntries((businessLinesQuery.data?.data ?? []).map((item) => [item.id, item.name])),
    [businessLinesQuery.data],
  )
  const environmentMap = useMemo(
    () => Object.fromEntries((environmentsQuery.data?.data ?? []).map((item) => [item.id, item.name])),
    [environmentsQuery.data],
  )
  const applicationMap = useMemo(
    () => Object.fromEntries((applicationsQuery.data?.data ?? []).map((item) => [item.id, item.name])),
    [applicationsQuery.data],
  )

  const grants = useMemo(
    () => (grantsQuery.data?.data ?? []).filter((item) => item.subjectType === subjectType && item.subjectId === subjectId),
    [grantsQuery.data, subjectId, subjectType],
  )

  const createMutation = useMutation({
    mutationFn: (values: Record<string, unknown>) => api.post('/access/scope-grants', values),
    onSuccess: () => {
      message.success('授权项创建成功')
      queryClient.invalidateQueries({ queryKey: ['scope-grants'] })
      setGrantModalVisible(false)
    },
    onError: (err: Error) => message.error(err.message),
  })
  const updateMutation = useMutation({
    mutationFn: ({ id, values }: { id: string; values: Record<string, unknown> }) => api.put(`/access/scope-grants/${id}`, values),
    onSuccess: () => {
      message.success('授权项更新成功')
      queryClient.invalidateQueries({ queryKey: ['scope-grants'] })
      setEditing(null)
      setGrantModalVisible(false)
    },
    onError: (err: Error) => message.error(err.message),
  })
  const deleteMutation = useMutation({
    mutationFn: (id: string) => api.delete(`/access/scope-grants/${id}`),
    onSuccess: () => {
      message.success('授权项已删除')
      queryClient.invalidateQueries({ queryKey: ['scope-grants'] })
    },
    onError: (err: Error) => message.error(err.message),
  })

  const submitGrant = (values: Record<string, unknown>) => {
    const payload = {
      ...values,
      subjectType,
      subjectId,
      environmentIds: parseCSV(values.environmentIds),
      applicationIds: parseCSV(values.applicationIds),
    }
    if (editing) {
      updateMutation.mutate({ id: editing.id, values: payload })
      return
    }
    createMutation.mutate(payload)
  }

  const columns: ColumnProps<ScopeGrant>[] = [
    { title: '业务线', dataIndex: 'businessLineId', render: (value: string) => businessLineMap[value] || value },
    {
      title: '环境',
      dataIndex: 'environmentIds',
      render: (values: string[]) => values?.length ? values.map((item) => <Tag key={item}>{environmentMap[item] || item}</Tag>) : '全部',
    },
    {
      title: '应用',
      dataIndex: 'applicationIds',
      render: (values: string[]) => values?.length ? values.map((item) => <Tag key={item}>{applicationMap[item] || item}</Tag>) : '全部',
    },
    { title: '角色', dataIndex: 'role' },
    { title: '效果', dataIndex: 'effect' },
    { title: '启用', dataIndex: 'enabled', render: (value: boolean) => <StatusTag value={value ? 'enabled' : 'disabled'} /> },
    {
      ...tableColumnPresets.action,
      title: '操作',
      dataIndex: 'id',
      render: (_: unknown, record: ScopeGrant) => (
        <Space>
          {canManageScopeGrants ? (
            <>
              <Button icon={<EditOutlined />} type="text" size="small" onClick={() => { setEditing(record); setGrantModalVisible(true) }} />
              <Popconfirm title="确认删除？" onConfirm={() => deleteMutation.mutate(record.id)}>
                <Button icon={<DeleteOutlined />} type="text" danger size="small" />
              </Popconfirm>
            </>
          ) : '-'}
        </Space>
      ),
    },
  ]

  return (
    <>
      <Modal title={title} open={visible} onCancel={onClose} footer={null} width={880}>
        <div className="kc-page">
          <div className="flex justify-end">
            {canManageScopeGrants ? (
              <Button icon={<PlusOutlined />} type="primary" onClick={() => { setEditing(null); setGrantModalVisible(true) }}>
                新建授权项
              </Button>
            ) : null}
          </div>
          <AdminTable columns={columns} dataSource={grants} rowKey="id" loading={grantsQuery.isLoading} />
        </div>
      </Modal>
      <Modal
        title={editing ? '编辑授权项' : '新建授权项'}
        open={grantModalVisible}
        onCancel={() => { setGrantModalVisible(false); setEditing(null) }}
        onOk={async () => {
          try {
            const values = await form.validateFields()
            submitGrant(values)
          } catch {
            return
          }
        }}
        okText={editing ? '更新' : '创建'}
        cancelText="取消"
        confirmLoading={createMutation.isPending || updateMutation.isPending}
        width={760}
        maskClosable={false}
        destroyOnClose
        styles={{ body: { maxHeight: '65vh', overflow: 'auto' } }}
      >
        <Form
          form={form}
          key={editing?.id ?? 'create-scope-grant'}
          layout="vertical"
          initialValues={editing ? {
            ...editing,
            environmentIds: joinCSV(editing.environmentIds),
            applicationIds: joinCSV(editing.applicationIds),
          } : { enabled: true, effect: 'allow', role: 'developer' }}
        >
          <Form.Item name="businessLineId" label="业务线" rules={[{ required: true, message: '请选择业务线' }]}>
            <Select options={(businessLinesQuery.data?.data ?? []).map((item) => ({ value: item.id, label: item.name }))} />
          </Form.Item>
          <Form.Item name="environmentIds" label="环境 IDs">
            <Input placeholder="留空表示全部环境，多个以逗号分隔" />
          </Form.Item>
          <Form.Item name="applicationIds" label="应用 IDs">
            <Input placeholder="留空表示全部应用，多个以逗号分隔" />
          </Form.Item>
          <Form.Item name="role" label="角色" rules={[{ required: true, message: '请输入角色' }]}>
            <Input />
          </Form.Item>
          <Form.Item name="effect" label="效果">
            <Input disabled />
          </Form.Item>
          <Form.Item name="enabled" label="启用" valuePropName="checked">
            <Switch />
          </Form.Item>
        </Form>
      </Modal>
    </>
  )
}

export function AccessUsersPage() {
  const { message } = App.useApp()
  const queryClient = useQueryClient()
  const permissionSnapshotQuery = usePermissionSnapshot()
  const snapshot = permissionSnapshotQuery.data?.data
  const canViewUsers = hasPermission(snapshot, 'access.users.view')
  const canManageUsers = hasPermission(snapshot, 'access.users.manage')
  const canManageScopeGrants = hasPermission(snapshot, 'access.scope-grants.manage')
  const [form] = Form.useForm<Record<string, unknown>>()
  const [modalVisible, setModalVisible] = useState(false)
  const [editing, setEditing] = useState<AccessUser | null>(null)
  const [grantUser, setGrantUser] = useState<AccessUser | null>(null)

  const usersQuery = useQuery({
    queryKey: ['access/users'],
    queryFn: () => api.get<ApiResponse<AccessUser[]>>('/access/users'),
  })
  const rolesQuery = useQuery({
    queryKey: ['access/roles'],
    queryFn: () => api.get<ApiResponse<AccessRole[]>>('/access/roles'),
  })
  const teamsQuery = useQuery({
    queryKey: ['access/teams'],
    queryFn: () => api.get<ApiResponse<AccessTeam[]>>('/access/teams'),
  })

  const roleMap = useMemo(
    () => Object.fromEntries((rolesQuery.data?.data ?? []).map((item) => [item.id, item.name])),
    [rolesQuery.data],
  )
  const teamMap = useMemo(
    () => Object.fromEntries((teamsQuery.data?.data ?? []).map((item) => [item.id, item.name])),
    [teamsQuery.data],
  )
  const roleOptions = useMemo(
    () => (rolesQuery.data?.data ?? []).map((item) => ({ value: item.id, label: item.name })),
    [rolesQuery.data],
  )
  const teamOptions = useMemo(
    () => (teamsQuery.data?.data ?? []).map((item) => ({ value: item.id, label: item.name })),
    [teamsQuery.data],
  )

  const createMutation = useMutation({
    mutationFn: (values: Record<string, unknown>) => api.post('/access/users', values),
    onSuccess: () => {
      message.success('用户创建成功')
      queryClient.invalidateQueries({ queryKey: ['access/users'] })
      setModalVisible(false)
    },
    onError: (err: Error) => message.error(err.message),
  })

  const updateMutation = useMutation({
    mutationFn: ({ id, values }: { id: string; values: Record<string, unknown> }) => api.put(`/access/users/${id}`, values),
    onSuccess: () => {
      message.success('用户更新成功')
      queryClient.invalidateQueries({ queryKey: ['access/users'] })
      setModalVisible(false)
      setEditing(null)
    },
    onError: (err: Error) => message.error(err.message),
  })

  const deleteMutation = useMutation({
    mutationFn: (id: string) => api.delete(`/access/users/${id}`),
    onSuccess: () => {
      message.success('用户删除成功')
      queryClient.invalidateQueries({ queryKey: ['access/users'] })
    },
    onError: (err: Error) => message.error(err.message),
  })

  const closeModal = () => {
    setModalVisible(false)
    setEditing(null)
  }

  const submitUser = (values: Record<string, unknown>) => {
    const payload = {
      username: String(values.username ?? '').trim(),
      displayName: String(values.displayName ?? '').trim(),
      email: String(values.email ?? '').trim(),
      status: String(values.status ?? 'active'),
      password: String(values.password ?? ''),
      roleIds: toStringArray(values.roleIds),
      teamIds: toStringArray(values.teamIds),
      tags: toStringArray(values.tags),
    }
    if (editing) {
      updateMutation.mutate({ id: editing.id, values: payload })
      return
    }
    createMutation.mutate(payload)
  }

  const columns: ColumnProps<AccessUser>[] = [
    {
      title: '用户',
      dataIndex: 'username',
      render: (_: string, record: AccessUser) => (
        <div className="flex items-center gap-3">
          <Avatar className="kc-user-avatar" size="small">
            {getUserInitial(record)}
          </Avatar>
          <div className="min-w-0 flex-1">
            <div className="truncate font-medium" style={{ color: 'var(--semi-color-text-0)' }}>{record.username}</div>
            <Text type="secondary" style={{ fontSize: 12 }}>
              {record.displayName || record.email || '-'}
            </Text>
          </div>
        </div>
      ),
    },
    { title: '邮箱', dataIndex: 'email' },
    {
      title: '角色',
      dataIndex: 'roles',
      render: (roles: string[]) => renderMappedTags(roles, roleMap, '未绑定'),
    },
    {
      title: '用户组',
      dataIndex: 'teams',
      render: (teams: string[]) => renderMappedTags(teams, teamMap, '未绑定'),
    },
    {
      ...tableColumnPresets.status,
      title: '状态',
      dataIndex: 'status',
      render: (value: string) => <StatusTag value={value} />,
    },
    {
      ...tableColumnPresets.datetime,
      title: '最近登录',
      dataIndex: 'lastLoginAt',
      render: (value?: string) => value ? formatDateTime(value) : '-',
    },
    {
      ...tableColumnPresets.action,
      title: '操作',
      dataIndex: 'id',
      render: (_: unknown, record: AccessUser) => (
        <Space>
          {canManageUsers || canManageScopeGrants ? (
            <>
              {canManageScopeGrants ? <Button type="text" size="small" onClick={() => setGrantUser(record)}>授权范围</Button> : null}
              {canManageUsers ? <Button icon={<EditOutlined />} type="text" size="small" onClick={() => { setEditing(record); setModalVisible(true) }} /> : null}
              {canManageUsers ? (
                <Popconfirm title="确认删除？" onConfirm={() => deleteMutation.mutate(record.id)}>
                  <Button icon={<DeleteOutlined />} type="text" danger size="small" />
                </Popconfirm>
              ) : null}
            </>
          ) : '-'}
        </Space>
      ),
    },
  ]

  if (!canViewUsers) {
    return <div className="kc-page">当前账号没有用户管理权限。</div>
  }

  return (
    <div className="kc-page">
      <AdminTable
        title="用户管理"
        className="kc-access-table"
        toolbarExtra={canManageUsers ? <Button icon={<PlusOutlined />} type="primary" onClick={() => { setEditing(null); setModalVisible(true) }}>添加用户</Button> : null}
        columns={columns}
        dataSource={usersQuery.data?.data ?? []}
        rowKey="id"
        loading={usersQuery.isLoading}
      />
      <Modal
        title={editing ? `编辑用户: ${getUserLabel(editing)}` : '添加用户'}
        open={modalVisible}
        onCancel={closeModal}
        onOk={async () => {
          try {
            const values = await form.validateFields()
            submitUser(values)
          } catch {
            return
          }
        }}
        okText={editing ? '更新' : '创建'}
        cancelText="取消"
        confirmLoading={createMutation.isPending || updateMutation.isPending}
        width={860}
        maskClosable={false}
        destroyOnClose
        styles={{ body: { maxHeight: '70vh', overflow: 'auto' } }}
      >
        <Form
          form={form}
          key={editing?.id ?? 'create-user'}
          layout="vertical"
          initialValues={editing ? {
            username: editing.username,
            displayName: editing.displayName,
            email: editing.email,
            status: editing.status || 'active',
            roleIds: editing.roles ?? [],
            teamIds: editing.teams ?? [],
            tags: editing.tags ?? [],
          } : {
            status: 'active',
            roleIds: [],
            teamIds: [],
            tags: [],
          }}
        >
          <Row gutter={16}>
            <Col span={12}>
              <Form.Item name="username" label="用户名" rules={[{ required: true, message: '请输入用户名' }]}>
                <Input />
              </Form.Item>
            </Col>
            <Col span={12}>
              <Form.Item name="displayName" label="显示名">
                <Input placeholder="留空时顶部默认展示用户名" />
              </Form.Item>
            </Col>
          </Row>
          <Row gutter={16}>
            <Col span={12}>
              <Form.Item name="email" label="邮箱" rules={[{ required: true, message: '请输入邮箱' }]}>
                <Input />
              </Form.Item>
            </Col>
            <Col span={12}>
              <Form.Item name="status" label="状态" rules={[{ required: true, message: '请选择状态' }]}>
                <Select options={USER_STATUS_OPTIONS} />
              </Form.Item>
            </Col>
          </Row>
          <Row gutter={16}>
            <Col span={12}>
              <Form.Item name="roleIds" label="角色">
                <Select mode="multiple" options={roleOptions} placeholder="选择用户角色" />
              </Form.Item>
            </Col>
            <Col span={12}>
              <Form.Item name="teamIds" label="用户组">
                <Select mode="multiple" options={teamOptions} placeholder="选择所属用户组" />
              </Form.Item>
            </Col>
          </Row>
          <Form.Item name="tags" label="标签">
            <Select mode="tags" tokenSeparators={[',']} placeholder="输入标签后按回车确认" />
          </Form.Item>
          <Form.Item
            name="password"
            label={editing ? '重置密码' : '登录密码'}
            rules={editing ? undefined : [{ required: true, message: '请输入密码' }]}
          >
            <Input.Password placeholder={editing ? '留空表示不修改密码' : '请输入初始密码'} />
          </Form.Item>
        </Form>
      </Modal>
      <ScopeGrantManager
        subjectType="user"
        subjectId={grantUser?.id ?? null}
        visible={!!grantUser}
        title={grantUser ? `用户授权范围: ${getUserLabel(grantUser)}` : '用户授权范围'}
        onClose={() => setGrantUser(null)}
      />
    </div>
  )
}

export function AccessRolesPage() {
  const permissionSnapshotQuery = usePermissionSnapshot()
  const snapshot = permissionSnapshotQuery.data?.data
  const canViewRoles = hasPermission(snapshot, 'access.roles.view')
  const canManageRoles = hasPermission(snapshot, 'access.roles.manage')
  const [form] = Form.useForm<Record<string, unknown>>()
  const crud = useCRUD<AccessRole>('access/roles', { invalidateKeys: [['access/users']] })
  const permissionSelectOptions = useMemo(() => getRolePermissionSelectOptions(), [])

  const columns: ColumnProps<AccessRole>[] = [
    { title: '角色名称', dataIndex: 'name' },
    { title: '范围', dataIndex: 'scope', render: (value: string) => value || 'custom' },
    {
      title: '权限动作',
      dataIndex: 'capabilities',
      render: (values: string[]) => renderMappedTags(values, {}, '未配置'),
    },
    {
      title: '控制台权限',
      dataIndex: 'permissionKeys',
      render: (values: string[] | undefined) => renderMappedTags(normalizePermissionKeys(values), consolePermissionLabelMap, '未配置'),
    },
    { title: '绑定用户', dataIndex: 'userCount' },
    {
      ...tableColumnPresets.action,
      title: '操作',
      dataIndex: 'id',
      render: (_: unknown, record: AccessRole) => (
        <Space>
          {canManageRoles ? (
            <>
              <Button icon={<EditOutlined />} type="text" size="small" onClick={() => crud.openEdit(record)} />
              <Popconfirm title="确认删除？" onConfirm={() => crud.deleteMutation.mutate(record.id)}>
                <Button icon={<DeleteOutlined />} type="text" danger size="small" />
              </Popconfirm>
            </>
          ) : '-'}
        </Space>
      ),
    },
  ]

  const submitRole = (values: Record<string, unknown>) => {
    crud.handleSubmit({
      name: String(values.name ?? '').trim(),
      scope: String(values.scope ?? 'custom'),
      capabilities: toStringArray(values.capabilities),
      permissionKeys: normalizePermissionKeys(values.permissionKeys),
    })
  }

  if (!canViewRoles) {
    return <div className="kc-page">当前账号没有角色管理权限。</div>
  }

  return (
    <div className="kc-page">
      <Alert
        className="mb-4"
        type="info"
        showIcon
        message="角色控制台权限通过 permissionKeys 提交。当前环境若尚未部署后端映射与持久化，保存后可能不会立即回显该字段。"
      />
      <AdminTable
        title="角色管理"
        className="kc-access-table"
        toolbarExtra={canManageRoles ? <Button icon={<PlusOutlined />} type="primary" onClick={crud.openCreate}>添加角色</Button> : null}
        columns={columns}
        dataSource={crud.data}
        rowKey="id"
        loading={crud.isLoading}
      />
      <Modal
        title={crud.editing ? `编辑角色: ${crud.editing.name}` : '添加角色'}
        open={crud.modalVisible}
        onCancel={crud.closeModal}
        onOk={async () => {
          try {
            const values = await form.validateFields()
            submitRole(values)
          } catch {
            return
          }
        }}
        okText={crud.editing ? '更新' : '创建'}
        cancelText="取消"
        confirmLoading={crud.isSaving}
        width={720}
        maskClosable={false}
        destroyOnClose
      >
        <Form
          form={form}
          key={crud.editing?.id ?? 'create-role'}
          layout="vertical"
          initialValues={crud.editing ? {
            name: crud.editing.name,
            scope: crud.editing.scope || 'custom',
            capabilities: crud.editing.capabilities ?? [],
            permissionKeys: normalizePermissionKeys(crud.editing.permissionKeys),
          } : {
            scope: 'custom',
            capabilities: [],
            permissionKeys: [],
          }}
        >
          <Form.Item name="name" label="角色名称" rules={[{ required: true, message: '请输入角色名称' }]}>
            <Input />
          </Form.Item>
          <Form.Item name="scope" label="角色范围">
            <Select options={ROLE_SCOPE_OPTIONS} />
          </Form.Item>
          <Form.Item
            name="capabilities"
            label="权限动作"
            rules={[{
              validator: (_, value) => toStringArray(value).length > 0
                ? Promise.resolve()
                : Promise.reject(new Error('请选择至少一个权限动作')),
            }]}
          >
            <Select mode="multiple" options={ACCESS_ACTION_OPTIONS} />
          </Form.Item>
          <Form.Item name="permissionKeys" label="控制台权限键">
            <Select
              mode="multiple"
              allowClear
              showSearch
              optionFilterProp="label"
              options={permissionSelectOptions}
              placeholder="选择该角色可见菜单与可调用 API 对应的权限键"
            />
          </Form.Item>
        </Form>
      </Modal>
    </div>
  )
}

export function AccessTeamsPage() {
  const permissionSnapshotQuery = usePermissionSnapshot()
  const snapshot = permissionSnapshotQuery.data?.data
  const canViewGroups = hasPermission(snapshot, 'access.groups.view')
  const canManageGroups = hasPermission(snapshot, 'access.groups.manage')
  const canManageScopeGrants = hasPermission(snapshot, 'access.scope-grants.manage')
  const [form] = Form.useForm<Record<string, unknown>>()
  const crud = useCRUD<AccessTeam>('access/teams', { invalidateKeys: [['access/users'], ['scope-grants']] })
  const [grantTeam, setGrantTeam] = useState<AccessTeam | null>(null)

  const columns: ColumnProps<AccessTeam>[] = [
    { title: '用户组名称', dataIndex: 'name' },
    { title: '标识', dataIndex: 'slug', render: (value: string) => value || '-' },
    { title: '说明', dataIndex: 'metadata', render: (value: Record<string, unknown>) => getGroupDescription(value) || '-' },
    { title: '成员数', dataIndex: 'userCount' },
    {
      ...tableColumnPresets.action,
      title: '操作',
      dataIndex: 'id',
      render: (_: unknown, record: AccessTeam) => (
        <Space>
          {canManageGroups || canManageScopeGrants ? (
            <>
              {canManageScopeGrants ? <Button type="text" size="small" onClick={() => setGrantTeam(record)}>授权范围</Button> : null}
              {canManageGroups ? <Button icon={<EditOutlined />} type="text" size="small" onClick={() => crud.openEdit(record)} /> : null}
              {canManageGroups ? (
                <Popconfirm title="确认删除？" onConfirm={() => crud.deleteMutation.mutate(record.id)}>
                  <Button icon={<DeleteOutlined />} type="text" danger size="small" />
                </Popconfirm>
              ) : null}
            </>
          ) : '-'}
        </Space>
      ),
    },
  ]

  const submitGroup = (values: Record<string, unknown>) => {
    crud.handleSubmit({
      name: String(values.name ?? '').trim(),
      slug: String(values.slug ?? '').trim(),
      metadata: {
        ...(crud.editing?.metadata ?? {}),
        description: String(values.description ?? '').trim(),
      },
    })
  }

  if (!canViewGroups) {
    return <div className="kc-page">当前账号没有用户组管理权限。</div>
  }

  return (
    <div className="kc-page">
      <AdminTable
        title="用户组管理"
        className="kc-access-table"
        toolbarExtra={canManageGroups ? <Button icon={<PlusOutlined />} type="primary" onClick={crud.openCreate}>添加用户组</Button> : null}
        columns={columns}
        dataSource={crud.data}
        rowKey="id"
        loading={crud.isLoading}
      />
      <Modal
        title={crud.editing ? `编辑用户组: ${crud.editing.name}` : '添加用户组'}
        open={crud.modalVisible}
        onCancel={crud.closeModal}
        onOk={async () => {
          try {
            const values = await form.validateFields()
            submitGroup(values)
          } catch {
            return
          }
        }}
        okText={crud.editing ? '更新' : '创建'}
        cancelText="取消"
        confirmLoading={crud.isSaving}
        width={720}
        maskClosable={false}
        destroyOnClose
      >
        <Form
          form={form}
          key={crud.editing?.id ?? 'create-group'}
          layout="vertical"
          initialValues={crud.editing ? {
            name: crud.editing.name,
            slug: crud.editing.slug,
            description: getGroupDescription(crud.editing.metadata),
          } : {}}
        >
          <Form.Item name="name" label="用户组名称" rules={[{ required: true, message: '请输入用户组名称' }]}>
            <Input />
          </Form.Item>
          <Form.Item name="slug" label="标识">
            <Input placeholder="留空时按名称自动生成" />
          </Form.Item>
          <Form.Item name="description" label="说明">
            <Input.TextArea rows={4} placeholder="说明该用户组的职责边界和适用成员" />
          </Form.Item>
        </Form>
      </Modal>
      <ScopeGrantManager
        subjectType="team"
        subjectId={grantTeam?.id ?? null}
        visible={!!grantTeam}
        title={grantTeam ? `用户组授权范围: ${grantTeam.name}` : '用户组授权范围'}
        onClose={() => setGrantTeam(null)}
      />
    </div>
  )
}

export function AccessPoliciesPage() {
  const permissionSnapshotQuery = usePermissionSnapshot()
  const snapshot = permissionSnapshotQuery.data?.data
  const canViewPolicies = hasPermission(snapshot, 'access.policies.view')
  const canManagePolicies = hasPermission(snapshot, 'access.policies.manage')
  const [form] = Form.useForm<Record<string, unknown>>()
  const crud = useCRUD<AccessPolicy>('access/policies')

  const rolesQuery = useQuery({
    queryKey: ['access/roles'],
    queryFn: () => api.get<ApiResponse<AccessRole[]>>('/access/roles'),
  })
  const teamsQuery = useQuery({
    queryKey: ['access/teams'],
    queryFn: () => api.get<ApiResponse<AccessTeam[]>>('/access/teams'),
  })

  const roleMap = useMemo(
    () => Object.fromEntries((rolesQuery.data?.data ?? []).map((item) => [item.id, item.name])),
    [rolesQuery.data],
  )
  const teamMap = useMemo(
    () => Object.fromEntries((teamsQuery.data?.data ?? []).map((item) => [item.id, item.name])),
    [teamsQuery.data],
  )
  const roleOptions = useMemo(
    () => (rolesQuery.data?.data ?? []).map((item) => ({ value: item.id, label: item.name })),
    [rolesQuery.data],
  )
  const teamOptions = useMemo(
    () => (teamsQuery.data?.data ?? []).map((item) => ({ value: item.id, label: item.name })),
    [teamsQuery.data],
  )

  const columns: ColumnProps<AccessPolicy>[] = [
    { title: '策略名称', dataIndex: 'name' },
    {
      title: '效果',
      dataIndex: 'effect',
      render: (value: string) => <StatusTag value={value} />,
    },
    { title: '优先级', dataIndex: 'priority' },
    {
      title: '动作',
      dataIndex: 'actions',
      render: (values: string[]) => renderMappedTags(values, {}, '未配置'),
    },
    {
      title: '主体',
      dataIndex: 'subjects',
      ellipsis: true,
      render: (_: unknown, record: AccessPolicy) => buildPolicySubjectsSummary(record, roleMap, teamMap),
    },
    {
      title: '目标',
      dataIndex: 'resources',
      ellipsis: true,
      render: (_: unknown, record: AccessPolicy) => buildPolicyTargetsSummary(record, teamMap),
    },
    { title: '原因', dataIndex: 'reason', ellipsis: true, render: (value: string) => value || '-' },
    {
      ...tableColumnPresets.action,
      title: '操作',
      dataIndex: 'id',
      render: (_: unknown, record: AccessPolicy) => (
        <Space>
          {canManagePolicies ? (
            <>
              <Button icon={<EditOutlined />} type="text" size="small" onClick={() => crud.openEdit(record)} />
              <Popconfirm title="确认删除？" onConfirm={() => crud.deleteMutation.mutate(record.id)}>
                <Button icon={<DeleteOutlined />} type="text" danger size="small" />
              </Popconfirm>
            </>
          ) : '-'}
        </Space>
      ),
    },
  ]

  const submitPolicy = (values: Record<string, unknown>) => {
    const current = crud.editing
    const baseSubjects = current?.subjects ?? { roles: [], teams: [], projects: [], users: [], tags: [] }
    const baseClusters = current?.clusters ?? { ids: [], regions: [], environments: [], labels: {} }
    const baseNamespaces = current?.namespaces ?? { names: [], ownerTeams: [], labels: {} }
    const baseResources = current?.resources ?? { kinds: [], names: [], labels: {} }
    const baseConditions = current?.conditions ?? { sources: [], approvalStates: [] }

    crud.handleSubmit({
      name: String(values.name ?? '').trim(),
      effect: String(values.effect ?? 'allow'),
      priority: Number(values.priority ?? 0),
      actions: toStringArray(values.actions),
      subjects: {
        ...baseSubjects,
        roles: toStringArray(values.subjectRoleIds),
        teams: toStringArray(values.subjectTeamIds),
        users: parseCSV(values.subjectUsers),
        tags: parseCSV(values.subjectTags),
      },
      clusters: {
        ...baseClusters,
        ids: parseCSV(values.clusterIds),
        regions: parseCSV(values.clusterRegions),
        environments: parseCSV(values.clusterEnvironments),
      },
      namespaces: {
        ...baseNamespaces,
        names: parseCSV(values.namespaceNames),
        ownerTeams: toStringArray(values.ownerTeamIds),
      },
      resources: {
        ...baseResources,
        kinds: parseCSV(values.resourceKinds),
        names: parseCSV(values.resourceNames),
      },
      conditions: {
        ...baseConditions,
        sources: parseCSV(values.sources),
        approvalStates: parseCSV(values.approvalStates),
      },
      reason: String(values.reason ?? '').trim(),
    })
  }

  if (!canViewPolicies) {
    return <div className="kc-page">当前账号没有策略管理权限。</div>
  }

  return (
    <div className="kc-page">
      <AdminTable
        title="策略管理"
        className="kc-access-table"
        toolbarExtra={canManagePolicies ? <Button icon={<PlusOutlined />} type="primary" onClick={crud.openCreate}>添加策略</Button> : null}
        columns={columns}
        dataSource={crud.data}
        rowKey="id"
        loading={crud.isLoading}
      />
      <Modal
        title={crud.editing ? `编辑策略: ${crud.editing.name}` : '添加策略'}
        open={crud.modalVisible}
        onCancel={crud.closeModal}
        onOk={async () => {
          try {
            const values = await form.validateFields()
            submitPolicy(values)
          } catch {
            return
          }
        }}
        okText={crud.editing ? '更新' : '创建'}
        cancelText="取消"
        confirmLoading={crud.isSaving}
        width={920}
        maskClosable={false}
        destroyOnClose
        styles={{ body: { maxHeight: '72vh', overflow: 'auto' } }}
      >
        <Form
          form={form}
          key={crud.editing?.id ?? 'create-policy'}
          layout="vertical"
          initialValues={crud.editing ? {
            name: crud.editing.name,
            effect: crud.editing.effect,
            priority: crud.editing.priority,
            actions: crud.editing.actions ?? [],
            subjectRoleIds: crud.editing.subjects?.roles ?? [],
            subjectTeamIds: crud.editing.subjects?.teams ?? [],
            subjectUsers: joinCSV(crud.editing.subjects?.users),
            subjectTags: joinCSV(crud.editing.subjects?.tags),
            clusterIds: joinCSV(crud.editing.clusters?.ids),
            clusterRegions: joinCSV(crud.editing.clusters?.regions),
            clusterEnvironments: joinCSV(crud.editing.clusters?.environments),
            namespaceNames: joinCSV(crud.editing.namespaces?.names),
            ownerTeamIds: crud.editing.namespaces?.ownerTeams ?? [],
            resourceKinds: joinCSV(crud.editing.resources?.kinds),
            resourceNames: joinCSV(crud.editing.resources?.names),
            sources: joinCSV(crud.editing.conditions?.sources),
            approvalStates: joinCSV(crud.editing.conditions?.approvalStates),
            reason: crud.editing.reason,
          } : {
            effect: 'allow',
            priority: 0,
            actions: [],
            subjectRoleIds: [],
            subjectTeamIds: [],
            ownerTeamIds: [],
          }}
        >
          <Row gutter={16}>
            <Col span={12}>
              <Form.Item name="name" label="策略名称" rules={[{ required: true, message: '请输入策略名称' }]}>
                <Input />
              </Form.Item>
            </Col>
            <Col span={6}>
              <Form.Item name="effect" label="效果" rules={[{ required: true, message: '请选择效果' }]}>
                <Select options={POLICY_EFFECT_OPTIONS} />
              </Form.Item>
            </Col>
            <Col span={6}>
              <Form.Item name="priority" label="优先级">
                <InputNumber style={{ width: '100%' }} />
              </Form.Item>
            </Col>
          </Row>
          <Form.Item
            name="actions"
            label="动作"
            rules={[{
              validator: (_, value) => toStringArray(value).length > 0
                ? Promise.resolve()
                : Promise.reject(new Error('请选择至少一个动作')),
            }]}
          >
            <Select mode="multiple" options={ACCESS_ACTION_OPTIONS} />
          </Form.Item>
          <Row gutter={16}>
            <Col span={12}>
              <Form.Item name="subjectRoleIds" label="主体角色">
                <Select mode="multiple" options={roleOptions} />
              </Form.Item>
            </Col>
            <Col span={12}>
              <Form.Item name="subjectTeamIds" label="主体用户组">
                <Select mode="multiple" options={teamOptions} />
              </Form.Item>
            </Col>
          </Row>
          <Row gutter={16}>
            <Col span={12}>
              <Form.Item name="subjectUsers" label="主体用户">
                <Input placeholder="多个用户名以逗号分隔" />
              </Form.Item>
            </Col>
            <Col span={12}>
              <Form.Item name="subjectTags" label="主体标签">
                <Input placeholder="多个标签以逗号分隔" />
              </Form.Item>
            </Col>
          </Row>
          <Row gutter={16}>
            <Col span={8}>
              <Form.Item name="clusterEnvironments" label="集群环境">
                <Input placeholder="development, staging" />
              </Form.Item>
            </Col>
            <Col span={8}>
              <Form.Item name="clusterRegions" label="集群地域">
                <Input placeholder="cn-beijing, us-west" />
              </Form.Item>
            </Col>
            <Col span={8}>
              <Form.Item name="clusterIds" label="集群 IDs">
                <Input placeholder="多个集群 ID 以逗号分隔" />
              </Form.Item>
            </Col>
          </Row>
          <Row gutter={16}>
            <Col span={12}>
              <Form.Item name="namespaceNames" label="命名空间">
                <Input placeholder="多个命名空间以逗号分隔" />
              </Form.Item>
            </Col>
            <Col span={12}>
              <Form.Item name="ownerTeamIds" label="归属用户组">
                <Select mode="multiple" options={teamOptions} />
              </Form.Item>
            </Col>
          </Row>
          <Row gutter={16}>
            <Col span={12}>
              <Form.Item name="resourceKinds" label="资源类型">
                <Input placeholder="Pod, Deployment, Namespace" />
              </Form.Item>
            </Col>
            <Col span={12}>
              <Form.Item name="resourceNames" label="资源名称">
                <Input placeholder="多个资源名称以逗号分隔" />
              </Form.Item>
            </Col>
          </Row>
          <Row gutter={16}>
            <Col span={12}>
              <Form.Item name="sources" label="请求来源">
                <Input placeholder="console, api, oidc" />
              </Form.Item>
            </Col>
            <Col span={12}>
              <Form.Item name="approvalStates" label="审批状态">
                <Input placeholder="approved, pending" />
              </Form.Item>
            </Col>
          </Row>
          <Form.Item name="reason" label="原因说明">
            <Input.TextArea rows={3} placeholder="说明该策略的意图和生效边界" />
          </Form.Item>
        </Form>
      </Modal>
    </div>
  )
}

export function AccessCenterPage() {
  const permissionSnapshotQuery = usePermissionSnapshot()
  const snapshot = permissionSnapshotQuery.data?.data
  const firstAccessiblePath = hasPermission(snapshot, 'access.users.view')
    ? '/access/users'
    : hasPermission(snapshot, 'access.roles.view')
      ? '/access/roles'
      : hasPermission(snapshot, 'access.groups.view')
        ? '/access/teams'
        : hasPermission(snapshot, 'access.policies.view')
          ? '/access/policies'
          : null

  if (permissionSnapshotQuery.isLoading) {
    return <div className="kc-page"><div className="flex items-center justify-center h-32">加载中...</div></div>
  }

  if (!firstAccessiblePath) {
    return <div className="kc-page"><div className="text-[var(--semi-color-text-2)]">当前账号没有访问控制页面权限。</div></div>
  }

  return <Navigate to={firstAccessiblePath} replace />
}
