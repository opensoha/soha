import { useMemo, useState } from 'react'
import { Button, Modal, Form, Tag, Toast, Popconfirm, Space } from '@douyinfe/semi-ui'
import { IconPlus, IconEdit, IconDelete } from '@douyinfe/semi-icons'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { AdminTable } from '@/components/admin-table'
import { PageHeader } from '@/components/page-header'
import { StatusTag } from '@/components/status-tag'
import { api } from '@/services/api-client'
import { tableColumnPresets } from '@/utils/table-columns'
import type { ApiResponse, BusinessLine, DeliveryEnvironment, ScopeGrant } from '@/types'
import type { ColumnProps } from '@douyinfe/semi-ui/lib/es/table'

/* ─── generic CRUD helpers ─── */

function useCRUD<T extends { id: string }>(resource: string) {
  const queryClient = useQueryClient()
  const [modalVisible, setModalVisible] = useState(false)
  const [editing, setEditing] = useState<T | null>(null)

  const query = useQuery({
    queryKey: [resource],
    queryFn: () => api.get<ApiResponse<T[]>>(`/${resource}`),
  })

  const createMutation = useMutation({
    mutationFn: (values: Record<string, unknown>) => api.post(`/${resource}`, values),
    onSuccess: () => {
      Toast.success('创建成功')
      queryClient.invalidateQueries({ queryKey: [resource] })
      setModalVisible(false)
    },
    onError: (err: Error) => Toast.error(err.message),
  })

  const updateMutation = useMutation({
    mutationFn: ({ id, values }: { id: string; values: Record<string, unknown> }) =>
      api.put(`/${resource}/${id}`, values),
    onSuccess: () => {
      Toast.success('更新成功')
      queryClient.invalidateQueries({ queryKey: [resource] })
      setModalVisible(false)
      setEditing(null)
    },
    onError: (err: Error) => Toast.error(err.message),
  })

  const deleteMutation = useMutation({
    mutationFn: (id: string) => api.delete(`/${resource}/${id}`),
    onSuccess: () => {
      Toast.success('删除成功')
      queryClient.invalidateQueries({ queryKey: [resource] })
    },
    onError: (err: Error) => Toast.error(err.message),
  })

  const handleSubmit = (values: Record<string, unknown>) => {
    if (editing) {
      updateMutation.mutate({ id: editing.id, values })
    } else {
      createMutation.mutate(values)
    }
  }

  const openCreate = () => { setEditing(null); setModalVisible(true) }
  const openEdit = (record: T) => { setEditing(record); setModalVisible(true) }
  const closeModal = () => { setModalVisible(false); setEditing(null) }

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

function parseCSV(value: unknown) {
  return String(value ?? '')
    .split(',')
    .map((item) => item.trim())
    .filter(Boolean)
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
      <Modal title={editing ? '编辑授权项' : '新建授权项'} visible={grantModalVisible} onCancel={() => { setGrantModalVisible(false); setEditing(null) }} footer={null} width={760}>
        <Form
          onSubmit={(values) => {
            const payload = {
              ...values,
              subjectType,
              subjectId,
              environmentIds: parseCSV(values.environmentIds),
              applicationIds: parseCSV(values.applicationIds),
            }
            if (editing) {
              updateMutation.mutate({ id: editing.id, values: payload })
            } else {
              createMutation.mutate(payload)
            }
          }}
          initValues={editing ? {
            ...editing,
            environmentIds: editing.environmentIds.join(', '),
            applicationIds: editing.applicationIds.join(', '),
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
          <div className="kc-form-actions">
            <Button onClick={() => { setGrantModalVisible(false); setEditing(null) }}>取消</Button>
            <Button htmlType="submit" theme="solid" loading={createMutation.isPending || updateMutation.isPending}>
              {editing ? '更新' : '创建'}
            </Button>
          </div>
        </Form>
      </Modal>
    </>
  )
}

/* ─── Users ─── */

interface AccessUser {
  id: string
  name: string
  email: string
  roles: string[]
  status: string
}

export function AccessUsersPage() {
  const crud = useCRUD<AccessUser>('access/users')
  const [grantUser, setGrantUser] = useState<AccessUser | null>(null)

  const columns: ColumnProps<AccessUser>[] = [
    { title: '用户名', dataIndex: 'name' },
    { title: '邮箱', dataIndex: 'email' },
    {
      title: '角色',
      dataIndex: 'roles',
      render: (roles: string[]) => roles?.map((r) => <Tag key={r} size="small" className="mr-1">{r}</Tag>) ?? '-',
    },
    {
      ...tableColumnPresets.status,
      title: '状态',
      dataIndex: 'status',
      render: (s: string) => <StatusTag value={s} />,
    },
    {
      ...tableColumnPresets.action,
      title: '操作',
      dataIndex: 'id',
      render: (_: unknown, record: AccessUser) => (
        <Space>
          <Button theme="borderless" size="small" onClick={() => setGrantUser(record)}>授权范围</Button>
          <Button icon={<IconEdit />} theme="borderless" size="small" onClick={() => crud.openEdit(record)} />
          <Popconfirm title="确认删除？" onConfirm={() => crud.deleteMutation.mutate(record.id)}>
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
        description="管理用户账号、角色绑定和启用状态。"
        actions={<Button icon={<IconPlus />} theme="solid" onClick={crud.openCreate}>添加用户</Button>}
      />
      <AdminTable columns={columns} dataSource={crud.data} rowKey="id" loading={crud.isLoading} />
      <Modal title={crud.editing ? '编辑用户' : '添加用户'} visible={crud.modalVisible} onCancel={crud.closeModal} footer={null}>
        <Form onSubmit={crud.handleSubmit} initValues={crud.editing ? { name: crud.editing.name, email: crud.editing.email } : {}}>
          <Form.Input field="name" label="用户名" rules={[{ required: true, message: '请输入用户名' }]} />
          <Form.Input field="email" label="邮箱" rules={[{ required: true, message: '请输入邮箱' }]} />
          {!crud.editing && <Form.Input field="password" label="密码" mode="password" rules={[{ required: true, message: '请输入密码' }]} />}
          <div className="kc-form-actions">
            <Button onClick={crud.closeModal}>取消</Button>
            <Button htmlType="submit" theme="solid" loading={crud.isSaving}>{crud.editing ? '更新' : '创建'}</Button>
          </div>
        </Form>
      </Modal>
      <ScopeGrantManager
        subjectType="user"
        subjectId={grantUser?.id ?? null}
        visible={!!grantUser}
        title={grantUser ? `用户授权范围: ${grantUser.name}` : '用户授权范围'}
        onClose={() => setGrantUser(null)}
      />
    </div>
  )
}

/* ─── Roles ─── */

interface AccessRole {
  id: string
  name: string
  description: string
  permissionsCount: number
}

export function AccessRolesPage() {
  const crud = useCRUD<AccessRole>('access/roles')

  const columns: ColumnProps<AccessRole>[] = [
    { title: '角色名称', dataIndex: 'name' },
    { title: '描述', dataIndex: 'description', ellipsis: true },
    { title: '权限数', dataIndex: 'permissionsCount' },
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

  return (
    <div className="kc-page">
      <PageHeader
        title="角色管理"
        description="定义角色职责边界，并维护角色权限集合。"
        actions={<Button icon={<IconPlus />} theme="solid" onClick={crud.openCreate}>添加角色</Button>}
      />
      <AdminTable columns={columns} dataSource={crud.data} rowKey="id" loading={crud.isLoading} />
      <Modal title={crud.editing ? '编辑角色' : '添加角色'} visible={crud.modalVisible} onCancel={crud.closeModal} footer={null}>
        <Form onSubmit={crud.handleSubmit} initValues={crud.editing ? { name: crud.editing.name, description: crud.editing.description } : {}}>
          <Form.Input field="name" label="角色名称" rules={[{ required: true, message: '请输入角色名称' }]} />
          <Form.TextArea field="description" label="描述" />
          <div className="kc-form-actions">
            <Button onClick={crud.closeModal}>取消</Button>
            <Button htmlType="submit" theme="solid" loading={crud.isSaving}>{crud.editing ? '更新' : '创建'}</Button>
          </div>
        </Form>
      </Modal>
    </div>
  )
}

/* ─── Teams ─── */

interface AccessTeam {
  id: string
  name: string
  description: string
  memberCount: number
}

export function AccessTeamsPage() {
  const crud = useCRUD<AccessTeam>('access/teams')
  const [grantTeam, setGrantTeam] = useState<AccessTeam | null>(null)

  const columns: ColumnProps<AccessTeam>[] = [
    { title: '团队名称', dataIndex: 'name' },
    { title: '描述', dataIndex: 'description', ellipsis: true },
    { title: '成员数', dataIndex: 'memberCount' },
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

  return (
    <div className="kc-page">
      <PageHeader
        title="团队管理"
        description="组织团队成员与职责分工，作为权限分派的中间层。"
        actions={<Button icon={<IconPlus />} theme="solid" onClick={crud.openCreate}>添加团队</Button>}
      />
      <AdminTable columns={columns} dataSource={crud.data} rowKey="id" loading={crud.isLoading} />
      <Modal title={crud.editing ? '编辑团队' : '添加团队'} visible={crud.modalVisible} onCancel={crud.closeModal} footer={null}>
        <Form onSubmit={crud.handleSubmit} initValues={crud.editing ? { name: crud.editing.name, description: crud.editing.description } : {}}>
          <Form.Input field="name" label="团队名称" rules={[{ required: true, message: '请输入团队名称' }]} />
          <Form.TextArea field="description" label="描述" />
          <div className="kc-form-actions">
            <Button onClick={crud.closeModal}>取消</Button>
            <Button htmlType="submit" theme="solid" loading={crud.isSaving}>{crud.editing ? '更新' : '创建'}</Button>
          </div>
        </Form>
      </Modal>
      <ScopeGrantManager
        subjectType="team"
        subjectId={grantTeam?.id ?? null}
        visible={!!grantTeam}
        title={grantTeam ? `团队授权范围: ${grantTeam.name}` : '团队授权范围'}
        onClose={() => setGrantTeam(null)}
      />
    </div>
  )
}

/* ─── Policies ─── */

interface AccessPolicy {
  id: string
  name: string
  effect: string
  scope: string
  description: string
}

export function AccessPoliciesPage() {
  const crud = useCRUD<AccessPolicy>('access/policies')

  const columns: ColumnProps<AccessPolicy>[] = [
    { title: '策略名称', dataIndex: 'name' },
    {
      title: '效果',
      dataIndex: 'effect',
      render: (e: string) => <StatusTag value={e} />,
    },
    { title: '范围', dataIndex: 'scope' },
    { title: '描述', dataIndex: 'description', ellipsis: true },
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

  return (
    <div className="kc-page">
      <PageHeader
        title="策略管理"
        description="维护访问控制策略的效果、范围与描述信息。"
        actions={<Button icon={<IconPlus />} theme="solid" onClick={crud.openCreate}>添加策略</Button>}
      />
      <AdminTable columns={columns} dataSource={crud.data} rowKey="id" loading={crud.isLoading} />
      <Modal title={crud.editing ? '编辑策略' : '添加策略'} visible={crud.modalVisible} onCancel={crud.closeModal} footer={null}>
        <Form onSubmit={crud.handleSubmit} initValues={crud.editing ? { name: crud.editing.name, effect: crud.editing.effect, scope: crud.editing.scope, description: crud.editing.description } : {}}>
          <Form.Input field="name" label="策略名称" rules={[{ required: true, message: '请输入策略名称' }]} />
          <Form.Select field="effect" label="效果" optionList={[{ value: 'allow', label: '允许' }, { value: 'deny', label: '拒绝' }]} rules={[{ required: true, message: '请选择效果' }]} />
          <Form.Input field="scope" label="范围" />
          <Form.TextArea field="description" label="描述" />
          <div className="kc-form-actions">
            <Button onClick={crud.closeModal}>取消</Button>
            <Button htmlType="submit" theme="solid" loading={crud.isSaving}>{crud.editing ? '更新' : '创建'}</Button>
          </div>
        </Form>
      </Modal>
    </div>
  )
}
