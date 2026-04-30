import { useMemo, useState } from 'react'
import {
  Alert,
  Button,
  Form,
  Input,
  InputNumber,
  Modal,
  Popconfirm,
  Select,
  Space,
  Switch,
  Tag,
  Tooltip,
  message,
} from 'antd'
import type { TableColumnsType } from 'antd'
import { DeleteOutlined, EditOutlined, PlusOutlined } from '@ant-design/icons'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { AdminTable } from '@/components/admin-table'
import { hasPermission, usePermissionSnapshot } from '@/features/auth/permission-snapshot'
import { PageHeader } from '@/components/page-header'
import { BooleanTag, StatusTag } from '@/components/status-tag'
import { resolveRouteMenuId, resolveRoutePermission, routeMeta } from '@/routes/meta'
import { api } from '@/services/api-client'
import { formatDateTime, formatRelativeTime } from '@/utils/time'
import { tableColumnPresets } from '@/utils/table-columns'
import type { ApiResponse } from '@/types'

const MODAL_FORM_LAYOUT = {
  labelAlign: 'left' as const,
  labelCol: { flex: '120px' },
  wrapperCol: { flex: 'auto' },
}

const MENU_SECTION_OPTIONS = [
  { value: 'observe', label: '平台资源 / 运维智能' },
  { value: 'catalog', label: '主数据' },
  { value: 'deliver', label: '应用交付' },
  { value: 'control', label: '访问控制 / 系统 / 设置' },
]

/* ─── Online Users ─── */

interface OnlineUser {
  id: string
  userId: string
  userName: string
  email: string
  providerType: string
  status: string
  loginTime: string
  lastSeenAt: string
  expiry: string
  source?: string
  sourceIp?: string
  userAgent?: string
}

function SourceTag({ value }: { value?: string }) {
  const normalized = (value || '').toLowerCase()
  if (!normalized) return <>-</>
  if (normalized === 'console') return <Tag color="blue">Console</Tag>
  if (normalized === 'oidc') return <Tag color="green">OIDC</Tag>
  if (normalized === 'api') return <Tag color="orange">API</Tag>
  return <Tag>{value}</Tag>
}

export function OnlineUsersPage() {
  const queryClient = useQueryClient()
  const permissionSnapshotQuery = usePermissionSnapshot()
  const [selectedSessionIds, setSelectedSessionIds] = useState<string[]>([])
  const canManageOnlineUsers = hasPermission(permissionSnapshotQuery.data?.data, 'system.online-users.manage')

  const { data, isLoading } = useQuery({
    queryKey: ['online-users'],
    queryFn: () => api.get<ApiResponse<OnlineUser[]>>('/auth/sessions'),
    select: (response: any) => ({
      data: (response.data ?? []).map((item: any) => ({
        id: item.id,
        userId: item.userId,
        userName: item.userName,
        email: item.email,
        providerType: item.providerType,
        status: item.status,
        loginTime: item.createdAt,
        lastSeenAt: item.lastSeenAt,
        expiry: item.expiresAt,
        source: item.metadata?.source,
        sourceIp: item.metadata?.sourceIp,
        userAgent: item.metadata?.userAgent,
      })),
    }),
    refetchInterval: 10000,
  })

  const revokeMutation = useMutation({
    mutationFn: (sessionId: string) => api.post(`/auth/sessions/${sessionId}/revoke`),
    onSuccess: (_result, sessionId) => {
      void message.success('用户会话已下线')
      void queryClient.invalidateQueries({ queryKey: ['online-users'] })
      setSelectedSessionIds((current) => current.filter((id) => id !== sessionId))
    },
    onError: (err: Error) => void message.error(err.message),
  })

  const batchRevokeMutation = useMutation({
    mutationFn: async (sessionIds: string[]) =>
      Promise.allSettled(sessionIds.map((sessionId) => api.post(`/auth/sessions/${sessionId}/revoke`))),
    onSuccess: (results) => {
      const successCount = results.filter((item) => item.status === 'fulfilled').length
      const failureCount = results.length - successCount
      void message.success(failureCount > 0 ? `批量下线完成，成功 ${successCount}，失败 ${failureCount}` : `已批量下线 ${successCount} 个会话`)
      setSelectedSessionIds([])
      void queryClient.invalidateQueries({ queryKey: ['online-users'] })
    },
    onError: (err: Error) => void message.error(err.message),
  })

  const sessions = data?.data ?? []
  const selectedSessions = useMemo(
    () => sessions.filter((item: OnlineUser) => selectedSessionIds.includes(item.id)),
    [selectedSessionIds, sessions],
  )

  const columns: TableColumnsType<OnlineUser> = [
    { title: '用户 ID', dataIndex: 'userId' },
    { title: '用户名', dataIndex: 'userName' },
    { title: '邮箱', dataIndex: 'email' },
    { title: '登录方式', dataIndex: 'providerType' },
    { title: '来源', dataIndex: 'source', render: (value: string) => <SourceTag value={value} /> },
    { title: 'IP', dataIndex: 'sourceIp', render: (value: string) => value || '-' },
    { title: '设备', dataIndex: 'userAgent', ellipsis: true, render: (value: string) => value || '-' },
    {
      ...tableColumnPresets.status,
      title: '状态',
      dataIndex: 'status',
      render: (value: string) => <StatusTag value={value} />,
    },
    { ...tableColumnPresets.datetime, title: '登录时间', dataIndex: 'loginTime', render: (_: string, record: OnlineUser) => formatDateTime(record.loginTime) },
    { ...tableColumnPresets.datetime, title: '最近活跃', dataIndex: 'lastSeenAt', render: (_: string, record: OnlineUser) => formatDateTime(record.lastSeenAt) },
    { title: '活跃时长', dataIndex: 'lastSeenAt', render: (_: string, record: OnlineUser) => formatRelativeTime(record.lastSeenAt) },
    { ...tableColumnPresets.datetime, title: '过期时间', dataIndex: 'expiry', render: (_: string, record: OnlineUser) => formatDateTime(record.expiry) },
    {
      ...tableColumnPresets.action,
      title: '操作',
      dataIndex: 'id',
      render: (_: string, record: OnlineUser) => (
        canManageOnlineUsers ? (
          <Popconfirm title="确认下线该用户会话？" onConfirm={() => revokeMutation.mutate(record.id)}>
            <Button
              size="small"
              type="text"
              danger
              icon={<DeleteOutlined />}
              aria-label="下线用户"
              loading={revokeMutation.isPending}
            />
          </Popconfirm>
        ) : '-'
      ),
    },
  ]

  return (
    <div className="kc-page">
      <PageHeader
        title="在线用户"
        description="查看当前在线会话、登录来源、最后活跃时间与会话到期信息。"
        actions={
          canManageOnlineUsers ? (
            <Button
              danger
              variant="outlined"
              disabled={selectedSessions.length === 0}
              loading={batchRevokeMutation.isPending}
              onClick={() => batchRevokeMutation.mutate(selectedSessions.map((item: OnlineUser) => item.id))}
            >
              {`批量下线 (${selectedSessions.length})`}
            </Button>
          ) : null
        }
      />
      <AdminTable
        columns={columns}
        dataSource={sessions}
        rowKey="id"
        loading={isLoading}
        pageSize={20}
        rowSelection={canManageOnlineUsers ? {
          selectedRowKeys: selectedSessionIds,
          onChange: (selectedRowKeys: React.Key[]) => setSelectedSessionIds(selectedRowKeys.map(String)),
        } : undefined}
      />
    </div>
  )
}

/* ─── Announcements ─── */

interface Announcement {
  id: string
  title: string
  summary: string
  content: string
  level: string
  status: string
  createdAt: string
}

export function AnnouncementsPage() {
  const queryClient = useQueryClient()
  const permissionSnapshotQuery = usePermissionSnapshot()
  const [modalVisible, setModalVisible] = useState(false)
  const [editing, setEditing] = useState<Announcement | null>(null)
  const canManageAnnouncements = hasPermission(permissionSnapshotQuery.data?.data, 'system.announcements.manage')

  const { data, isLoading } = useQuery({
    queryKey: ['announcements'],
    queryFn: () => api.get<ApiResponse<Announcement[]>>('/announcements'),
  })

  const createMutation = useMutation({
    mutationFn: (values: Record<string, string>) => api.post('/announcements', values),
    onSuccess: () => {
      void message.success('公告创建成功')
      void queryClient.invalidateQueries({ queryKey: ['announcements'] })
      setModalVisible(false)
    },
    onError: (err: Error) => void message.error(err.message),
  })

  const updateMutation = useMutation({
    mutationFn: ({ id, values }: { id: string; values: Record<string, string> }) =>
      api.put(`/announcements/${id}`, values),
    onSuccess: () => {
      void message.success('公告更新成功')
      void queryClient.invalidateQueries({ queryKey: ['announcements'] })
      setModalVisible(false)
      setEditing(null)
    },
    onError: (err: Error) => void message.error(err.message),
  })

  const deleteMutation = useMutation({
    mutationFn: (id: string) => api.delete(`/announcements/${id}`),
    onSuccess: () => {
      void message.success('公告已删除')
      void queryClient.invalidateQueries({ queryKey: ['announcements'] })
    },
    onError: (err: Error) => void message.error(err.message),
  })

  const handleSubmit = (values: Record<string, string>) => {
    if (editing) {
      updateMutation.mutate({ id: editing.id, values })
    } else {
      createMutation.mutate(values)
    }
  }

  const columns: TableColumnsType<Announcement> = [
    { title: '标题', dataIndex: 'title' },
    { title: '摘要', dataIndex: 'summary', ellipsis: true },
    { title: '级别', dataIndex: 'level' },
    {
      ...tableColumnPresets.status,
      title: '状态',
      dataIndex: 'status',
      render: (s: string) => <StatusTag value={s} />,
    },
    { ...tableColumnPresets.datetime, title: '创建时间', dataIndex: 'createdAt', render: (value: string) => formatDateTime(value) },
    {
      ...tableColumnPresets.action,
      title: '操作',
      dataIndex: 'id',
      render: (_: unknown, record: Announcement) => (
        <Space>
          {canManageAnnouncements ? <Button icon={<EditOutlined />} type="text" size="small" onClick={() => { setEditing(record); setModalVisible(true) }} /> : null}
          {canManageAnnouncements ? (
            <Popconfirm title="确认删除？" onConfirm={() => deleteMutation.mutate(record.id)}>
              <Button icon={<DeleteOutlined />} type="text" danger size="small" />
            </Popconfirm>
          ) : null}
          {!canManageAnnouncements ? '-' : null}
        </Space>
      ),
    },
  ]

  return (
    <div className="kc-page">
      <PageHeader
        title="公告管理"
        description="维护系统公告内容、发布状态与创建时间。"
        actions={canManageAnnouncements ? <Button icon={<PlusOutlined />} type="primary" onClick={() => { setEditing(null); setModalVisible(true) }}>新建公告</Button> : null}
      />
      <AdminTable columns={columns} dataSource={data?.data ?? []} rowKey="id" loading={isLoading} />
      <Modal
        title={editing ? '编辑公告' : '新建公告'}
        open={modalVisible}
        onCancel={() => { setModalVisible(false); setEditing(null) }}
        footer={null}
        destroyOnClose
      >
        <Form
          {...MODAL_FORM_LAYOUT}
          onFinish={(values) => { if (!canManageAnnouncements) return; handleSubmit(values as Record<string, string>) }}
          initialValues={editing ? { title: editing.title, summary: editing.summary, content: editing.content, level: editing.level, status: editing.status } : { level: 'info', status: 'draft' }}
        >
          <Form.Item name="title" label="标题" rules={[{ required: true, message: '请输入标题' }]}>
            <Input />
          </Form.Item>
          <Form.Item name="summary" label="摘要">
            <Input />
          </Form.Item>
          <Form.Item name="content" label="内容" rules={[{ required: true, message: '请输入内容' }]}>
            <Input.TextArea rows={4} />
          </Form.Item>
          <Form.Item name="level" label="级别">
            <Select options={[
              { value: 'info', label: '信息' },
              { value: 'warning', label: '警告' },
              { value: 'critical', label: '严重' },
            ]}
            />
          </Form.Item>
          <Form.Item name="status" label="状态">
            <Select options={[
              { value: 'draft', label: '草稿' },
              { value: 'published', label: '已发布' },
            ]}
            />
          </Form.Item>
          <div className="kc-form-actions">
            <Button onClick={() => setModalVisible(false)}>取消</Button>
            {canManageAnnouncements ? (
              <Button htmlType="submit" type="primary" loading={createMutation.isPending || updateMutation.isPending}>
                {editing ? '更新' : '创建'}
              </Button>
            ) : null}
          </div>
        </Form>
      </Modal>
    </div>
  )
}

/* ─── Menus ─── */

interface MenuItem {
  id: string
  parentId?: string
  labelZh: string
  labelEn: string
  path: string
  iconKey: string
  section: string
  sortOrder: number
  enabled: boolean
  roleIds?: string[]
  visibilityMode?: 'derived' | 'explicit'
  derivedPermissionKeys?: string[]
  children?: MenuItem[]
  depth?: number
  parentLabelZh?: string
}

interface AccessRoleOption {
  id: string
  name: string
}

type MenuVisibilityMode = 'derived' | 'explicit' | 'unmapped'

interface MenuVisibilitySummary {
  derivedPermissionKeys: string[]
  explicitRoleIds: string[]
  mode: MenuVisibilityMode
}

function flattenMenuItems(items: MenuItem[], depth = 0, parent?: MenuItem): MenuItem[] {
  return items.flatMap((item) => {
    const { children, ...rest } = item
    const nextItem: MenuItem = {
      ...rest,
      depth,
      parentLabelZh: parent?.labelZh,
    }
    return [nextItem, ...flattenMenuItems(children ?? [], depth + 1, item)]
  })
}

function collectMenuDescendantIds(item: MenuItem): string[] {
  return (item.children ?? []).flatMap((child) => [child.id, ...collectMenuDescendantIds(child)])
}

function findMenuItemByID(items: MenuItem[], id: string): MenuItem | null {
  for (const item of items) {
    if (item.id === id) {
      return item
    }
    const child = findMenuItemByID(item.children ?? [], id)
    if (child) {
      return child
    }
  }
  return null
}

function normalizeMenuSubmitValues(values: Record<string, unknown>) {
  const normalizedParentId = typeof values.parentId === 'string' ? values.parentId.trim() : values.parentId
  const roleIds = Array.isArray(values.roleIds)
    ? values.roleIds.map((item) => String(item).trim()).filter(Boolean)
    : []
  const visibilityMode = values.visibilityMode === 'explicit' ? 'explicit' : 'derived'

  return {
    id: typeof values.id === 'string' ? values.id.trim() : values.id,
    labelZh: typeof values.labelZh === 'string' ? values.labelZh.trim() : values.labelZh,
    labelEn: typeof values.labelEn === 'string' ? values.labelEn.trim() : values.labelEn,
    path: typeof values.path === 'string' ? values.path.trim() : values.path,
    iconKey: typeof values.iconKey === 'string' ? values.iconKey.trim() : values.iconKey,
    section: typeof values.section === 'string' ? values.section.trim() : values.section,
    sortOrder: values.sortOrder,
    enabled: values.enabled,
    parentId: normalizedParentId ? normalizedParentId : null,
    roleIds: visibilityMode === 'explicit' ? roleIds : [],
  }
}

function compactUniqueStrings(values: string[]) {
  return Array.from(new Set(values.map((item) => item.trim()).filter(Boolean))).sort((left, right) => left.localeCompare(right))
}

export function getMenuDerivedPermissionKeys(item: Pick<MenuItem, 'id' | 'path' | 'derivedPermissionKeys'>) {
  if (item.derivedPermissionKeys?.length) {
    return compactUniqueStrings(item.derivedPermissionKeys)
  }
  return compactUniqueStrings(
    routeMeta
      .filter((route) => {
        const routeMenuId = resolveRouteMenuId(route)
        return routeMenuId === item.id || route.path === item.path
      })
      .map((route) => resolveRoutePermission(route))
      .filter((value): value is string => Boolean(value)),
  )
}

export function summarizeMenuVisibility(item: Pick<MenuItem, 'id' | 'path' | 'roleIds' | 'visibilityMode' | 'derivedPermissionKeys'>): MenuVisibilitySummary {
  const derivedPermissionKeys = getMenuDerivedPermissionKeys(item)
  const explicitRoleIds = compactUniqueStrings(item.roleIds ?? [])

  if (item.visibilityMode === 'explicit') {
    return { derivedPermissionKeys, explicitRoleIds, mode: 'explicit' }
  }
  if (item.visibilityMode === 'derived') {
    return { derivedPermissionKeys, explicitRoleIds, mode: derivedPermissionKeys.length > 0 ? 'derived' : 'unmapped' }
  }
  if (explicitRoleIds.length > 0) {
    return { derivedPermissionKeys, explicitRoleIds, mode: 'explicit' }
  }
  if (derivedPermissionKeys.length > 0) {
    return { derivedPermissionKeys, explicitRoleIds, mode: 'derived' }
  }
  return { derivedPermissionKeys, explicitRoleIds, mode: 'unmapped' }
}

function getMenuVisibilityModeOptions(summary: MenuVisibilitySummary) {
  return [
    {
      value: 'derived',
      label: '自动派生',
      disabled: summary.derivedPermissionKeys.length === 0,
    },
    {
      value: 'explicit',
      label: '显式覆盖',
    },
  ]
}

function MenuVisibilityTags({ item }: { item: Pick<MenuItem, 'id' | 'path' | 'roleIds' | 'visibilityMode' | 'derivedPermissionKeys'> }) {
  const summary = summarizeMenuVisibility(item)

  if (summary.mode === 'explicit') {
    return (
      <Space wrap size={[4, 4]}>
        <Tag color="gold">显式覆盖</Tag>
        {summary.explicitRoleIds.length > 0 ? (
          <Tooltip title={summary.explicitRoleIds.join(', ')}>
            <Tag>{`角色 ${summary.explicitRoleIds.length}`}</Tag>
          </Tooltip>
        ) : (
          <Tag>未绑定角色</Tag>
        )}
      </Space>
    )
  }

  if (summary.mode === 'derived') {
    return (
      <Space wrap size={[4, 4]}>
        <Tag color="blue">自动派生</Tag>
        <Tooltip title={summary.derivedPermissionKeys.join(', ')}>
          <Tag>{summary.derivedPermissionKeys.length === 1 ? summary.derivedPermissionKeys[0] : `权限键 ${summary.derivedPermissionKeys.length}`}</Tag>
        </Tooltip>
      </Space>
    )
  }

  return (
    <Space wrap size={[4, 4]}>
      <Tag>未映射</Tag>
      <Tag color="default">需显式配置</Tag>
    </Space>
  )
}

export function MenusPage() {
  const queryClient = useQueryClient()
  const permissionSnapshotQuery = usePermissionSnapshot()
  const [form] = Form.useForm()
  const [modalVisible, setModalVisible] = useState(false)
  const [editing, setEditing] = useState<MenuItem | null>(null)
  const canManageMenus = hasPermission(permissionSnapshotQuery.data?.data, 'system.menus.manage')

  const { data, isLoading } = useQuery({
    queryKey: ['menus'],
    queryFn: () => api.get<ApiResponse<MenuItem[]>>('/menus'),
  })

  const { data: rolesResponse } = useQuery({
    queryKey: ['access-roles', 'menu-overrides'],
    queryFn: () => api.get<ApiResponse<AccessRoleOption[]>>('/access/roles'),
    enabled: canManageMenus && modalVisible,
    retry: false,
  })

  const createMutation = useMutation({
    mutationFn: (values: Record<string, unknown>) => api.post('/menus', values),
    onSuccess: () => {
      void message.success('菜单创建成功')
      void queryClient.invalidateQueries({ queryKey: ['menus'] })
      setModalVisible(false)
    },
    onError: (err: Error) => void message.error(err.message),
  })

  const updateMutation = useMutation({
    mutationFn: ({ id, values }: { id: string; values: Record<string, unknown> }) =>
      api.put(`/menus/${id}`, values),
    onSuccess: () => {
      void message.success('菜单更新成功')
      void queryClient.invalidateQueries({ queryKey: ['menus'] })
      setModalVisible(false)
      setEditing(null)
    },
    onError: (err: Error) => void message.error(err.message),
  })

  const deleteMutation = useMutation({
    mutationFn: (id: string) => api.delete(`/menus/${id}`),
    onSuccess: () => {
      void message.success('菜单已删除')
      void queryClient.invalidateQueries({ queryKey: ['menus'] })
    },
    onError: (err: Error) => void message.error(err.message),
  })

  const handleSubmit = (values: Record<string, unknown>) => {
    const normalizedValues = normalizeMenuSubmitValues(values)
    if (editing) {
      updateMutation.mutate({ id: editing.id, values: normalizedValues })
    } else {
      createMutation.mutate(normalizedValues)
    }
  }

  const menuTree = data?.data ?? []
  const menuItems = flattenMenuItems(menuTree)
  const menuPageSize = Math.max(menuItems.length, 1)
  const roleOptions = (rolesResponse?.data ?? []).map((role) => ({
    value: role.id,
    label: role.name || role.id,
  }))
  const blockedParentIds = new Set(editing ? [editing.id, ...collectMenuDescendantIds(editing)] : [])
  const parentOptions = [
    { label: '顶级菜单', value: '' },
    ...menuItems
      .filter((item) => !blockedParentIds.has(item.id))
      .map((item) => ({
        value: item.id,
        label: `${'— '.repeat(item.depth ?? 0)}${item.labelZh}`,
      })),
  ]

  const columns: TableColumnsType<MenuItem> = [
    {
      title: '中文名称',
      dataIndex: 'labelZh',
      render: (value: string, record: MenuItem) => (
        <div style={{ paddingLeft: `${(record.depth ?? 0) * 16}px` }}>
          <Space>
            <span>{value}</span>
            <Tag>{record.parentId ? '子菜单' : '顶级'}</Tag>
          </Space>
        </div>
      ),
    },
    { title: '英文名称', dataIndex: 'labelEn' },
    { title: '父级菜单', dataIndex: 'parentLabelZh', render: (value: string, record: MenuItem) => value || (record.parentId || '-') },
    { title: '路径', dataIndex: 'path' },
    { title: '图标', dataIndex: 'iconKey' },
    { title: '分组', dataIndex: 'section' },
    { title: '排序', dataIndex: 'sortOrder' },
    {
      title: '可见',
      dataIndex: 'enabled',
      render: (v: boolean) => <BooleanTag value={v} />,
    },
    {
      title: '可见性策略',
      key: 'visibilityModel',
      render: (_: unknown, record: MenuItem) => <MenuVisibilityTags item={record} />,
    },
    {
      ...tableColumnPresets.action,
      title: '操作',
      dataIndex: 'id',
      render: (_: unknown, record: MenuItem) => (
        <Space>
          {canManageMenus ? <Button icon={<EditOutlined />} type="text" size="small" onClick={() => { setEditing(findMenuItemByID(menuTree, record.id) ?? record); setModalVisible(true) }} /> : null}
          {canManageMenus ? (
            <Popconfirm title="确认删除？" onConfirm={() => deleteMutation.mutate(record.id)}>
              <Button icon={<DeleteOutlined />} type="text" danger size="small" />
            </Popconfirm>
          ) : null}
          {!canManageMenus ? '-' : null}
        </Space>
      ),
    },
  ]

  return (
    <div className="kc-page">
      <PageHeader
        title="菜单管理"
        description="维护菜单层级、路径、启用状态、排序，以及菜单可见性的派生或覆盖规则。"
        actions={canManageMenus ? <Button icon={<PlusOutlined />} type="primary" onClick={() => { setEditing(null); setModalVisible(true) }}>新建菜单</Button> : null}
      />
      <Alert
        showIcon
        type="info"
        message="默认可见性已改为按 permissionKeys 自动派生"
        description="菜单映射到已知路由权限时，角色拥有对应 permissionKeys 后会自动看到该菜单。只有无权限映射的菜单，或确实需要例外处理的场景，才使用显式角色覆盖。"
        style={{ marginBottom: 16 }}
      />
      <AdminTable columns={columns} dataSource={menuItems} rowKey="id" loading={isLoading} pageSize={menuPageSize} />
      <Modal
        title={editing ? '编辑菜单' : '新建菜单'}
        open={modalVisible}
        onCancel={() => { setModalVisible(false); setEditing(null); form.resetFields() }}
        footer={null}
        destroyOnClose
      >
        <Form
          form={form}
          {...MODAL_FORM_LAYOUT}
          onFinish={(values) => { if (!canManageMenus) return; handleSubmit(values as Record<string, unknown>) }}
          initialValues={editing
            ? {
                id: editing.id,
                labelZh: editing.labelZh,
                labelEn: editing.labelEn,
                parentId: editing.parentId || '',
                path: editing.path,
                iconKey: editing.iconKey,
                section: editing.section,
                sortOrder: editing.sortOrder,
                enabled: editing.enabled,
                roleIds: editing.roleIds ?? [],
                visibilityMode: summarizeMenuVisibility(editing).mode === 'explicit' ? 'explicit' : 'derived',
              }
            : {
                enabled: true,
                sortOrder: 0,
                section: 'control',
                parentId: '',
                roleIds: [],
                visibilityMode: 'derived',
              }}
        >
          <Form.Item noStyle shouldUpdate={(prev, next) => prev.path !== next.path || prev.roleIds !== next.roleIds || prev.visibilityMode !== next.visibilityMode || prev.id !== next.id}>
            {({ getFieldValue }) => {
              const draftMenu = {
                id: String(getFieldValue('id') || ''),
                path: String(getFieldValue('path') || ''),
                roleIds: Array.isArray(getFieldValue('roleIds')) ? getFieldValue('roleIds').map(String) : [],
                visibilityMode: getFieldValue('visibilityMode') === 'explicit' ? 'explicit' : 'derived',
              } satisfies Pick<MenuItem, 'id' | 'path' | 'roleIds' | 'visibilityMode'>
              const visibilitySummary = summarizeMenuVisibility(draftMenu)
              const visibilityMode = getFieldValue('visibilityMode') === 'explicit' ? 'explicit' : 'derived'

              return (
                <>
                  <Alert
                    showIcon
                    type={visibilitySummary.mode === 'unmapped' ? 'warning' : 'info'}
                    message={visibilitySummary.mode === 'explicit' ? '当前菜单使用显式角色覆盖' : visibilitySummary.mode === 'derived' ? '当前菜单将按权限键自动派生可见性' : '当前菜单尚未映射已知权限键'}
                    description={visibilitySummary.mode === 'explicit'
                      ? '仅为少数例外场景保留显式角色覆盖。保存后会提交 roleIds，覆盖默认的 permissionKeys 派生行为。'
                      : visibilitySummary.mode === 'derived'
                        ? `当前可派生权限键: ${visibilitySummary.derivedPermissionKeys.join(', ')}`
                        : '该菜单没有匹配到前端路由权限键。若仍需控制可见性，请切换为显式覆盖并填写角色 ID。'}
                    style={{ marginBottom: 16 }}
                  />
                  <Form.Item
                    name="visibilityMode"
                    label="可见性模式"
                    rules={[{ required: true, message: '请选择可见性模式' }]}
                  >
                    <Select options={getMenuVisibilityModeOptions(visibilitySummary)} />
                  </Form.Item>
                  {visibilitySummary.derivedPermissionKeys.length > 0 ? (
                    <Form.Item label="派生权限键">
                      <Select
                        mode="multiple"
                        open={false}
                        value={visibilitySummary.derivedPermissionKeys}
                        options={visibilitySummary.derivedPermissionKeys.map((permissionKey) => ({
                          value: permissionKey,
                          label: permissionKey,
                        }))}
                      />
                    </Form.Item>
                  ) : null}
                  {visibilityMode === 'explicit' ? (
                    <Form.Item name="roleIds" label="覆盖角色">
                      <Select
                        mode="tags"
                        options={roleOptions}
                        placeholder="输入角色 ID，或选择已有角色"
                        tokenSeparators={[',']}
                      />
                    </Form.Item>
                  ) : null}
                </>
              )
            }}
          </Form.Item>
          <Form.Item name="labelZh" label="中文名称" rules={[{ required: true, message: '请输入中文名称' }]}>
            <Input />
          </Form.Item>
          <Form.Item name="labelEn" label="英文名称">
            <Input />
          </Form.Item>
          <Form.Item name="parentId" label="父级菜单">
            <Select options={parentOptions} />
          </Form.Item>
          <Form.Item name="path" label="路径" rules={[{ required: true, message: '请输入路径' }]}>
            <Input />
          </Form.Item>
          <Form.Item name="iconKey" label="图标">
            <Input />
          </Form.Item>
          <Form.Item name="section" label="分组">
            <Select options={MENU_SECTION_OPTIONS} />
          </Form.Item>
          <Form.Item name="sortOrder" label="排序">
            <InputNumber style={{ width: '100%' }} />
          </Form.Item>
          <Form.Item name="enabled" label="是否启用" valuePropName="checked">
            <Switch />
          </Form.Item>
          <div className="kc-form-actions">
            <Button onClick={() => { setModalVisible(false); setEditing(null); form.resetFields() }}>取消</Button>
            {canManageMenus ? (
              <Button htmlType="submit" type="primary" loading={createMutation.isPending || updateMutation.isPending}>
                {editing ? '更新' : '创建'}
              </Button>
            ) : null}
          </div>
        </Form>
      </Modal>
    </div>
  )
}

/* ─── Audit Logs ─── */

interface AuditLog {
  id: string
  timestamp: string
  user: string
  action: string
  resource: string
  result: string
}

export function AuditLogsPage() {
  const { data, isLoading } = useQuery({
    queryKey: ['audit-logs'],
    queryFn: () => api.get<ApiResponse<AuditLog[]>>('/audit/logs'),
  })

  const columns: TableColumnsType<AuditLog> = [
    { ...tableColumnPresets.datetime, title: '时间', dataIndex: 'timestamp', render: (value: string) => formatDateTime(value) },
    { title: '用户', dataIndex: 'user' },
    { title: '操作', dataIndex: 'action' },
    { title: '资源', dataIndex: 'resource' },
    {
      title: '结果',
      dataIndex: 'result',
      render: (r: string) => <StatusTag value={r} />,
    },
  ]

  return (
    <div className="kc-page">
      <PageHeader title="审计日志" description="查看关键操作的审计记录、资源对象和执行结果。" />
      <AdminTable columns={columns} dataSource={data?.data ?? []} rowKey="id" loading={isLoading} pageSize={50} />
    </div>
  )
}

/* ─── Operation Logs ─── */

interface OperationLog {
  id: string
  timestamp: string
  user: string
  operation: string
  target: string
  status: string
}

export function OperationLogsPage() {
  const { data, isLoading } = useQuery({
    queryKey: ['operation-logs'],
    queryFn: () => api.get<ApiResponse<OperationLog[]>>('/operations/logs'),
  })

  const columns: TableColumnsType<OperationLog> = [
    { ...tableColumnPresets.datetime, title: '时间', dataIndex: 'timestamp', render: (value: string) => formatDateTime(value) },
    { title: '用户', dataIndex: 'user' },
    { title: '操作', dataIndex: 'operation' },
    { title: '目标', dataIndex: 'target' },
    {
      ...tableColumnPresets.status,
      title: '状态',
      dataIndex: 'status',
      render: (s: string) => <StatusTag value={s} />,
    },
  ]

  return (
    <div className="kc-page">
      <PageHeader title="操作日志" description="查看系统操作流水、目标对象与处理状态。" />
      <AdminTable columns={columns} dataSource={data?.data ?? []} rowKey="id" loading={isLoading} pageSize={50} />
    </div>
  )
}
