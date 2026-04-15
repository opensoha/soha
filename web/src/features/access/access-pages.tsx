import { useMemo, useRef, useState } from 'react'
import { Avatar, Button, Col, Form, Modal, Popconfirm, Row, Space, TabPane, Tabs, Tag, Toast, Typography } from '@douyinfe/semi-ui'
import { IconDelete, IconEdit, IconPlus } from '@douyinfe/semi-icons'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useLocation, useNavigate } from 'react-router-dom'
import type { ColumnProps } from '@douyinfe/semi-ui/lib/es/table'
import { AdminTable } from '@/components/admin-table'
import { hasPermission, usePermissionSnapshot } from '@/features/auth/permission-snapshot'
import { PageHeader } from '@/components/page-header'
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

type ModalFormApi = {
  validate: () => Promise<Record<string, unknown>>
}

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
    <Space wrap spacing={4}>
      {values.map((value) => (
        <Tag key={value} size="small">
          {labelMap[value] || value}
        </Tag>
      ))}
    </Space>
  )
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
      Toast.success('创建成功')
      invalidateAll()
      setModalVisible(false)
    },
    onError: (err: Error) => Toast.error(err.message),
  })

  const updateMutation = useMutation({
    mutationFn: ({ id, values }: { id: string; values: Record<string, unknown> }) =>
      api.put(`/${resource}/${id}`, values),
    onSuccess: () => {
      Toast.success('更新成功')
      invalidateAll()
      setModalVisible(false)
      setEditing(null)
    },
    onError: (err: Error) => Toast.error(err.message),
  })

  const deleteMutation = useMutation({
    mutationFn: (id: string) => api.delete(`/${resource}/${id}`),
    onSuccess: () => {
      Toast.success('删除成功')
      invalidateAll()
    },
    onError: (err: Error) => Toast.error(err.message),
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
  const queryClient = useQueryClient()
  const grantFormApiRef = useRef<ModalFormApi | null>(null)
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
      Toast.success('授权项创建成功')
      queryClient.invalidateQueries({ queryKey: ['scope-grants'] })
      setGrantModalVisible(false)
    },
    onError: (err: Error) => Toast.error(err.message),
  })
  const updateMutation = useMutation({
    mutationFn: ({ id, values }: { id: string; values: Record<string, unknown> }) => api.put(`/access/scope-grants/${id}`, values),
    onSuccess: () => {
      Toast.success('授权项更新成功')
      queryClient.invalidateQueries({ queryKey: ['scope-grants'] })
      setEditing(null)
      setGrantModalVisible(false)
    },
    onError: (err: Error) => Toast.error(err.message),
  })
  const deleteMutation = useMutation({
    mutationFn: (id: string) => api.delete(`/access/scope-grants/${id}`),
    onSuccess: () => {
      Toast.success('授权项已删除')
      queryClient.invalidateQueries({ queryKey: ['scope-grants'] })
    },
    onError: (err: Error) => Toast.error(err.message),
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
          <Button icon={<IconEdit />} theme="borderless" size="small" onClick={() => { setEditing(record); setGrantModalVisible(true) }} />
          <Popconfirm title="确认删除？" onConfirm={() => deleteMutation.mutate(record.id)}>
            <Button icon={<IconDelete />} theme="borderless" type="danger" size="small" />
          </Popconfirm>
        </Space>
      ),
    },
  ]

  return (
    <>
      <Modal title={title} visible={visible} onCancel={onClose} footer={null} width={880}>
        <div className="kc-page">
          <div className="flex justify-end">
            <Button icon={<IconPlus />} theme="solid" onClick={() => { setEditing(null); setGrantModalVisible(true) }}>
              新建授权项
            </Button>
          </div>
          <AdminTable columns={columns} dataSource={grants} rowKey="id" loading={grantsQuery.isLoading} />
        </div>
      </Modal>
      <Modal
        title={editing ? '编辑授权项' : '新建授权项'}
        visible={grantModalVisible}
        onCancel={() => { setGrantModalVisible(false); setEditing(null) }}
        onOk={() => {
          grantFormApiRef.current?.validate().then(submitGrant).catch(() => undefined)
        }}
        okText={editing ? '更新' : '创建'}
        cancelText="取消"
        confirmLoading={createMutation.isPending || updateMutation.isPending}
        width={760}
        maskClosable={false}
        bodyStyle={{ maxHeight: '65vh', overflow: 'auto' }}
      >
        <Form
          key={editing?.id ?? 'create-scope-grant'}
          getFormApi={(formApi) => { grantFormApiRef.current = formApi as ModalFormApi }}
          initValues={editing ? {
            ...editing,
            environmentIds: joinCSV(editing.environmentIds),
            applicationIds: joinCSV(editing.applicationIds),
          } : { enabled: true, effect: 'allow', role: 'developer' }}
        >
          <Form.Select
            field="businessLineId"
            label="业务线"
            optionList={(businessLinesQuery.data?.data ?? []).map((item) => ({ value: item.id, label: item.name }))}
            rules={[{ required: true, message: '请选择业务线' }]}
          />
          <Form.Input field="environmentIds" label="环境 IDs" placeholder="留空表示全部环境，多个以逗号分隔" />
          <Form.Input field="applicationIds" label="应用 IDs" placeholder="留空表示全部应用，多个以逗号分隔" />
          <Form.Input field="role" label="角色" rules={[{ required: true, message: '请输入角色' }]} />
          <Form.Input field="effect" label="效果" disabled initValue="allow" />
          <Form.Switch field="enabled" label="启用" />
        </Form>
      </Modal>
    </>
  )
}

export function AccessUsersPage() {
  const queryClient = useQueryClient()
  const userFormApiRef = useRef<ModalFormApi | null>(null)
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
      Toast.success('用户创建成功')
      queryClient.invalidateQueries({ queryKey: ['access/users'] })
      setModalVisible(false)
    },
    onError: (err: Error) => Toast.error(err.message),
  })

  const updateMutation = useMutation({
    mutationFn: ({ id, values }: { id: string; values: Record<string, unknown> }) => api.put(`/access/users/${id}`, values),
    onSuccess: () => {
      Toast.success('用户更新成功')
      queryClient.invalidateQueries({ queryKey: ['access/users'] })
      setModalVisible(false)
      setEditing(null)
    },
    onError: (err: Error) => Toast.error(err.message),
  })

  const deleteMutation = useMutation({
    mutationFn: (id: string) => api.delete(`/access/users/${id}`),
    onSuccess: () => {
      Toast.success('用户删除成功')
      queryClient.invalidateQueries({ queryKey: ['access/users'] })
    },
    onError: (err: Error) => Toast.error(err.message),
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
            <Text type="tertiary" size="small">
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
          <Button theme="borderless" size="small" onClick={() => setGrantUser(record)}>授权范围</Button>
          <Button icon={<IconEdit />} theme="borderless" size="small" onClick={() => { setEditing(record); setModalVisible(true) }} />
          <Popconfirm title="确认删除？" onConfirm={() => deleteMutation.mutate(record.id)}>
            <Button icon={<IconDelete />} theme="borderless" type="danger" size="small" />
          </Popconfirm>
        </Space>
      ),
    },
  ]

  return (
    <div className="kc-page">
      <PageHeader
        title="用户管理"
        description="维护用户账号、角色绑定、用户组归属与启用状态。"
        actions={<Button icon={<IconPlus />} theme="solid" onClick={() => { setEditing(null); setModalVisible(true) }}>添加用户</Button>}
      />
      <AdminTable columns={columns} dataSource={usersQuery.data?.data ?? []} rowKey="id" loading={usersQuery.isLoading} />
      <Modal
        title={editing ? `编辑用户: ${getUserLabel(editing)}` : '添加用户'}
        visible={modalVisible}
        onCancel={closeModal}
        onOk={() => {
          userFormApiRef.current?.validate().then(submitUser).catch(() => undefined)
        }}
        okText={editing ? '更新' : '创建'}
        cancelText="取消"
        confirmLoading={createMutation.isPending || updateMutation.isPending}
        width={860}
        maskClosable={false}
        bodyStyle={{ maxHeight: '70vh', overflow: 'auto' }}
      >
        <Form
          key={editing?.id ?? 'create-user'}
          getFormApi={(formApi) => { userFormApiRef.current = formApi as ModalFormApi }}
          initValues={editing ? {
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
              <Form.Input field="username" label="用户名" rules={[{ required: true, message: '请输入用户名' }]} />
            </Col>
            <Col span={12}>
              <Form.Input field="displayName" label="显示名" placeholder="留空时顶部默认展示用户名" />
            </Col>
          </Row>
          <Row gutter={16}>
            <Col span={12}>
              <Form.Input field="email" label="邮箱" rules={[{ required: true, message: '请输入邮箱' }]} />
            </Col>
            <Col span={12}>
              <Form.Select field="status" label="状态" optionList={USER_STATUS_OPTIONS} rules={[{ required: true, message: '请选择状态' }]} />
            </Col>
          </Row>
          <Row gutter={16}>
            <Col span={12}>
              <Form.Select field="roleIds" label="角色" multiple optionList={roleOptions} placeholder="选择用户角色" />
            </Col>
            <Col span={12}>
              <Form.Select field="teamIds" label="用户组" multiple optionList={teamOptions} placeholder="选择所属用户组" />
            </Col>
          </Row>
          <Form.TagInput field="tags" label="标签" placeholder="输入标签后按回车确认" />
          <Form.Input
            field="password"
            label={editing ? '重置密码' : '登录密码'}
            mode="password"
            placeholder={editing ? '留空表示不修改密码' : '请输入初始密码'}
            rules={editing ? undefined : [{ required: true, message: '请输入密码' }]}
          />
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
  const roleFormApiRef = useRef<ModalFormApi | null>(null)
  const crud = useCRUD<AccessRole>('access/roles', { invalidateKeys: [['access/users']] })

  const columns: ColumnProps<AccessRole>[] = [
    { title: '角色名称', dataIndex: 'name' },
    { title: '范围', dataIndex: 'scope', render: (value: string) => value || 'custom' },
    {
      title: '权限动作',
      dataIndex: 'capabilities',
      render: (values: string[]) => renderMappedTags(values, {}, '未配置'),
    },
    { title: '绑定用户', dataIndex: 'userCount' },
    {
      ...tableColumnPresets.action,
      title: '操作',
      dataIndex: 'id',
      render: (_: unknown, record: AccessRole) => (
        <Space>
          <Button icon={<IconEdit />} theme="borderless" size="small" onClick={() => crud.openEdit(record)} />
          <Popconfirm title="确认删除？" onConfirm={() => crud.deleteMutation.mutate(record.id)}>
            <Button icon={<IconDelete />} theme="borderless" type="danger" size="small" />
          </Popconfirm>
        </Space>
      ),
    },
  ]

  const submitRole = (values: Record<string, unknown>) => {
    crud.handleSubmit({
      name: String(values.name ?? '').trim(),
      scope: String(values.scope ?? 'custom'),
      capabilities: toStringArray(values.capabilities),
    })
  }

  return (
    <div className="kc-page">
      <PageHeader
        title="角色管理"
        description="维护 RBAC 角色的能力集合，角色能力会直接进入平台权限基线。"
        actions={<Button icon={<IconPlus />} theme="solid" onClick={crud.openCreate}>添加角色</Button>}
      />
      <AdminTable columns={columns} dataSource={crud.data} rowKey="id" loading={crud.isLoading} />
      <Modal
        title={crud.editing ? `编辑角色: ${crud.editing.name}` : '添加角色'}
        visible={crud.modalVisible}
        onCancel={crud.closeModal}
        onOk={() => {
          roleFormApiRef.current?.validate().then(submitRole).catch(() => undefined)
        }}
        okText={crud.editing ? '更新' : '创建'}
        cancelText="取消"
        confirmLoading={crud.isSaving}
        width={720}
        maskClosable={false}
      >
        <Form
          key={crud.editing?.id ?? 'create-role'}
          getFormApi={(formApi) => { roleFormApiRef.current = formApi as ModalFormApi }}
          initValues={crud.editing ? {
            name: crud.editing.name,
            scope: crud.editing.scope || 'custom',
            capabilities: crud.editing.capabilities ?? [],
          } : {
            scope: 'custom',
            capabilities: [],
          }}
        >
          <Form.Input field="name" label="角色名称" rules={[{ required: true, message: '请输入角色名称' }]} />
          <Form.Select field="scope" label="角色范围" optionList={ROLE_SCOPE_OPTIONS} />
          <Form.Select
            field="capabilities"
            label="权限动作"
            multiple
            optionList={ACCESS_ACTION_OPTIONS}
            validate={(value) => toStringArray(value).length > 0 ? '' : '请选择至少一个权限动作'}
          />
        </Form>
      </Modal>
    </div>
  )
}

export function AccessTeamsPage() {
  const groupFormApiRef = useRef<ModalFormApi | null>(null)
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
          <Button theme="borderless" size="small" onClick={() => setGrantTeam(record)}>授权范围</Button>
          <Button icon={<IconEdit />} theme="borderless" size="small" onClick={() => crud.openEdit(record)} />
          <Popconfirm title="确认删除？" onConfirm={() => crud.deleteMutation.mutate(record.id)}>
            <Button icon={<IconDelete />} theme="borderless" type="danger" size="small" />
          </Popconfirm>
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

  return (
    <div className="kc-page">
      <PageHeader
        title="用户组管理"
        description="把用户归拢到用户组，用于批量授权、范围控制与访问策略匹配。"
        actions={<Button icon={<IconPlus />} theme="solid" onClick={crud.openCreate}>添加用户组</Button>}
      />
      <AdminTable columns={columns} dataSource={crud.data} rowKey="id" loading={crud.isLoading} />
      <Modal
        title={crud.editing ? `编辑用户组: ${crud.editing.name}` : '添加用户组'}
        visible={crud.modalVisible}
        onCancel={crud.closeModal}
        onOk={() => {
          groupFormApiRef.current?.validate().then(submitGroup).catch(() => undefined)
        }}
        okText={crud.editing ? '更新' : '创建'}
        cancelText="取消"
        confirmLoading={crud.isSaving}
        width={720}
        maskClosable={false}
      >
        <Form
          key={crud.editing?.id ?? 'create-group'}
          getFormApi={(formApi) => { groupFormApiRef.current = formApi as ModalFormApi }}
          initValues={crud.editing ? {
            name: crud.editing.name,
            slug: crud.editing.slug,
            description: getGroupDescription(crud.editing.metadata),
          } : {}}
        >
          <Form.Input field="name" label="用户组名称" rules={[{ required: true, message: '请输入用户组名称' }]} />
          <Form.Input field="slug" label="标识" placeholder="留空时按名称自动生成" />
          <Form.TextArea field="description" label="说明" rows={4} placeholder="说明该用户组的职责边界和适用成员" />
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
  const policyFormApiRef = useRef<ModalFormApi | null>(null)
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
          <Button icon={<IconEdit />} theme="borderless" size="small" onClick={() => crud.openEdit(record)} />
          <Popconfirm title="确认删除？" onConfirm={() => crud.deleteMutation.mutate(record.id)}>
            <Button icon={<IconDelete />} theme="borderless" type="danger" size="small" />
          </Popconfirm>
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

  return (
    <div className="kc-page">
      <PageHeader
        title="策略管理"
        description="在角色能力之上继续用主体、环境、命名空间和资源维度约束最终可执行权限。"
        actions={<Button icon={<IconPlus />} theme="solid" onClick={crud.openCreate}>添加策略</Button>}
      />
      <AdminTable columns={columns} dataSource={crud.data} rowKey="id" loading={crud.isLoading} />
      <Modal
        title={crud.editing ? `编辑策略: ${crud.editing.name}` : '添加策略'}
        visible={crud.modalVisible}
        onCancel={crud.closeModal}
        onOk={() => {
          policyFormApiRef.current?.validate().then(submitPolicy).catch(() => undefined)
        }}
        okText={crud.editing ? '更新' : '创建'}
        cancelText="取消"
        confirmLoading={crud.isSaving}
        width={920}
        maskClosable={false}
        bodyStyle={{ maxHeight: '72vh', overflow: 'auto' }}
      >
        <Form
          key={crud.editing?.id ?? 'create-policy'}
          getFormApi={(formApi) => { policyFormApiRef.current = formApi as ModalFormApi }}
          initValues={crud.editing ? {
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
              <Form.Input field="name" label="策略名称" rules={[{ required: true, message: '请输入策略名称' }]} />
            </Col>
            <Col span={6}>
              <Form.Select field="effect" label="效果" optionList={POLICY_EFFECT_OPTIONS} rules={[{ required: true, message: '请选择效果' }]} />
            </Col>
            <Col span={6}>
              <Form.InputNumber field="priority" label="优先级" />
            </Col>
          </Row>
          <Form.Select
            field="actions"
            label="动作"
            multiple
            optionList={ACCESS_ACTION_OPTIONS}
            validate={(value) => toStringArray(value).length > 0 ? '' : '请选择至少一个动作'}
          />
          <Row gutter={16}>
            <Col span={12}>
              <Form.Select field="subjectRoleIds" label="主体角色" multiple optionList={roleOptions} />
            </Col>
            <Col span={12}>
              <Form.Select field="subjectTeamIds" label="主体用户组" multiple optionList={teamOptions} />
            </Col>
          </Row>
          <Row gutter={16}>
            <Col span={12}>
              <Form.Input field="subjectUsers" label="主体用户" placeholder="多个用户名以逗号分隔" />
            </Col>
            <Col span={12}>
              <Form.Input field="subjectTags" label="主体标签" placeholder="多个标签以逗号分隔" />
            </Col>
          </Row>
          <Row gutter={16}>
            <Col span={8}>
              <Form.Input field="clusterEnvironments" label="集群环境" placeholder="development, staging" />
            </Col>
            <Col span={8}>
              <Form.Input field="clusterRegions" label="集群地域" placeholder="cn-beijing, us-west" />
            </Col>
            <Col span={8}>
              <Form.Input field="clusterIds" label="集群 IDs" placeholder="多个集群 ID 以逗号分隔" />
            </Col>
          </Row>
          <Row gutter={16}>
            <Col span={12}>
              <Form.Input field="namespaceNames" label="命名空间" placeholder="多个命名空间以逗号分隔" />
            </Col>
            <Col span={12}>
              <Form.Select field="ownerTeamIds" label="归属用户组" multiple optionList={teamOptions} />
            </Col>
          </Row>
          <Row gutter={16}>
            <Col span={12}>
              <Form.Input field="resourceKinds" label="资源类型" placeholder="Pod, Deployment, Namespace" />
            </Col>
            <Col span={12}>
              <Form.Input field="resourceNames" label="资源名称" placeholder="多个资源名称以逗号分隔" />
            </Col>
          </Row>
          <Row gutter={16}>
            <Col span={12}>
              <Form.Input field="sources" label="请求来源" placeholder="console, api, oidc" />
            </Col>
            <Col span={12}>
              <Form.Input field="approvalStates" label="审批状态" placeholder="approved, pending" />
            </Col>
          </Row>
          <Form.TextArea field="reason" label="原因说明" rows={3} placeholder="说明该策略的意图和生效边界" />
        </Form>
      </Modal>
    </div>
  )
}

export function AccessCenterPage() {
  const location = useLocation()
  const navigate = useNavigate()
  const permissionSnapshotQuery = usePermissionSnapshot()
  const snapshot = permissionSnapshotQuery.data?.data
  const tabs = [
    { key: 'users', path: '/access/users', label: '用户', visible: hasPermission(snapshot, 'access.users.view') },
    { key: 'roles', path: '/access/roles', label: '角色', visible: hasPermission(snapshot, 'access.roles.view') },
    { key: 'groups', path: '/access/teams', label: '用户组', visible: hasPermission(snapshot, 'access.groups.view') },
    { key: 'policies', path: '/access/policies', label: '策略', visible: hasPermission(snapshot, 'access.policies.view') },
  ].filter((item) => item.visible)

  const activeKey = location.pathname.startsWith('/access/roles')
    ? 'roles'
    : location.pathname.startsWith('/access/teams')
      ? 'groups'
      : location.pathname.startsWith('/access/policies')
        ? 'policies'
        : 'users'

  return (
    <div className="kc-page">
      <Tabs
        type="line"
        activeKey={activeKey}
        onChange={(key) => {
          const next = tabs.find((item) => item.key === key)
          if (next) {
            navigate(next.path)
          }
        }}
      >
        {tabs.map((tab) => (
          <TabPane tab={tab.label} itemKey={tab.key} key={tab.key}>
            {tab.key === 'users' ? <AccessUsersPage /> : null}
            {tab.key === 'roles' ? <AccessRolesPage /> : null}
            {tab.key === 'groups' ? <AccessTeamsPage /> : null}
            {tab.key === 'policies' ? <AccessPoliciesPage /> : null}
          </TabPane>
        ))}
      </Tabs>
    </div>
  )
}
