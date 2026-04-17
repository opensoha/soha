import { useMemo, useState } from 'react'
import { Button, Modal, Form, Toast, Popconfirm, Space, Tag } from '@douyinfe/semi-ui'
import { IconPlus, IconEdit, IconDelete } from '@douyinfe/semi-icons'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { AdminTable } from '@/components/admin-table'
import { hasPermission, usePermissionSnapshot } from '@/features/auth/permission-snapshot'
import { PageHeader } from '@/components/page-header'
import { BooleanTag, StatusTag } from '@/components/status-tag'
import { api } from '@/services/api-client'
import { formatDateTime, formatRelativeTime } from '@/utils/time'
import { tableColumnPresets } from '@/utils/table-columns'
import type { ApiResponse } from '@/types'
import type { ColumnProps } from '@douyinfe/semi-ui/lib/es/table'

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
      Toast.success('用户会话已下线')
      queryClient.invalidateQueries({ queryKey: ['online-users'] })
      setSelectedSessionIds((current) => current.filter((id) => id !== sessionId))
    },
    onError: (err: Error) => Toast.error(err.message),
  })

  const batchRevokeMutation = useMutation({
    mutationFn: async (sessionIds: string[]) =>
      Promise.allSettled(sessionIds.map((sessionId) => api.post(`/auth/sessions/${sessionId}/revoke`))),
    onSuccess: (results) => {
      const successCount = results.filter((item) => item.status === 'fulfilled').length
      const failureCount = results.length - successCount
      Toast.success(failureCount > 0 ? `批量下线完成，成功 ${successCount}，失败 ${failureCount}` : `已批量下线 ${successCount} 个会话`)
      setSelectedSessionIds([])
      queryClient.invalidateQueries({ queryKey: ['online-users'] })
    },
    onError: (err: Error) => Toast.error(err.message),
  })

  const sessions = data?.data ?? []
  const selectedSessions = useMemo(
    () => sessions.filter((item: OnlineUser) => selectedSessionIds.includes(item.id)),
    [selectedSessionIds, sessions],
  )

  const columns: ColumnProps<OnlineUser>[] = [
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
              theme="borderless"
              type="danger"
              icon={<IconDelete />}
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
              theme="light"
              type="danger"
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
          onChange: (selectedRowKeys: string[]) => setSelectedSessionIds(selectedRowKeys),
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
      Toast.success('公告创建成功')
      queryClient.invalidateQueries({ queryKey: ['announcements'] })
      setModalVisible(false)
    },
    onError: (err: Error) => Toast.error(err.message),
  })

  const updateMutation = useMutation({
    mutationFn: ({ id, values }: { id: string; values: Record<string, string> }) =>
      api.put(`/announcements/${id}`, values),
    onSuccess: () => {
      Toast.success('公告更新成功')
      queryClient.invalidateQueries({ queryKey: ['announcements'] })
      setModalVisible(false)
      setEditing(null)
    },
    onError: (err: Error) => Toast.error(err.message),
  })

  const deleteMutation = useMutation({
    mutationFn: (id: string) => api.delete(`/announcements/${id}`),
    onSuccess: () => {
      Toast.success('公告已删除')
      queryClient.invalidateQueries({ queryKey: ['announcements'] })
    },
    onError: (err: Error) => Toast.error(err.message),
  })

  const handleSubmit = (values: Record<string, string>) => {
    if (editing) {
      updateMutation.mutate({ id: editing.id, values })
    } else {
      createMutation.mutate(values)
    }
  }

  const columns: ColumnProps<Announcement>[] = [
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
          {canManageAnnouncements ? <Button icon={<IconEdit />} theme="borderless" size="small" onClick={() => { setEditing(record); setModalVisible(true) }} /> : null}
          {canManageAnnouncements ? (
            <Popconfirm title="确认删除？" onConfirm={() => deleteMutation.mutate(record.id)}>
              <Button icon={<IconDelete />} theme="borderless" type="danger" size="small" />
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
        actions={canManageAnnouncements ? <Button icon={<IconPlus />} theme="solid" onClick={() => { setEditing(null); setModalVisible(true) }}>新建公告</Button> : null}
      />
      <AdminTable columns={columns} dataSource={data?.data ?? []} rowKey="id" loading={isLoading} />
      <Modal title={editing ? '编辑公告' : '新建公告'} visible={modalVisible} onCancel={() => { setModalVisible(false); setEditing(null) }} footer={null}>
        <Form onSubmit={(values) => { if (!canManageAnnouncements) return; handleSubmit(values) }} initValues={editing ? { title: editing.title, summary: editing.summary, content: editing.content, level: editing.level, status: editing.status } : { level: 'info', status: 'draft' }}>
          <Form.Input field="title" label="标题" rules={[{ required: true, message: '请输入标题' }]} />
          <Form.Input field="summary" label="摘要" />
          <Form.TextArea field="content" label="内容" rules={[{ required: true, message: '请输入内容' }]} rows={4} />
          <Form.Select field="level" label="级别" optionList={[
            { value: 'info', label: '信息' },
            { value: 'warning', label: '警告' },
            { value: 'critical', label: '严重' },
          ]} />
          <Form.Select field="status" label="状态" optionList={[
            { value: 'draft', label: '草稿' },
            { value: 'published', label: '已发布' },
          ]} />
          <div className="kc-form-actions">
            <Button onClick={() => setModalVisible(false)}>取消</Button>
            {canManageAnnouncements ? (
              <Button htmlType="submit" theme="solid" loading={createMutation.isPending || updateMutation.isPending}>
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
  children?: MenuItem[]
  depth?: number
  parentLabelZh?: string
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
  return {
    ...values,
    parentId: normalizedParentId ? normalizedParentId : null,
  }
}

export function MenusPage() {
  const queryClient = useQueryClient()
  const permissionSnapshotQuery = usePermissionSnapshot()
  const [modalVisible, setModalVisible] = useState(false)
  const [editing, setEditing] = useState<MenuItem | null>(null)
  const canManageMenus = hasPermission(permissionSnapshotQuery.data?.data, 'system.menus.manage')

  const { data, isLoading } = useQuery({
    queryKey: ['menus'],
    queryFn: () => api.get<ApiResponse<MenuItem[]>>('/menus'),
  })

  const createMutation = useMutation({
    mutationFn: (values: Record<string, unknown>) => api.post('/menus', values),
    onSuccess: () => {
      Toast.success('菜单创建成功')
      queryClient.invalidateQueries({ queryKey: ['menus'] })
      setModalVisible(false)
    },
    onError: (err: Error) => Toast.error(err.message),
  })

  const updateMutation = useMutation({
    mutationFn: ({ id, values }: { id: string; values: Record<string, unknown> }) =>
      api.put(`/menus/${id}`, values),
    onSuccess: () => {
      Toast.success('菜单更新成功')
      queryClient.invalidateQueries({ queryKey: ['menus'] })
      setModalVisible(false)
      setEditing(null)
    },
    onError: (err: Error) => Toast.error(err.message),
  })

  const deleteMutation = useMutation({
    mutationFn: (id: string) => api.delete(`/menus/${id}`),
    onSuccess: () => {
      Toast.success('菜单已删除')
      queryClient.invalidateQueries({ queryKey: ['menus'] })
    },
    onError: (err: Error) => Toast.error(err.message),
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

  const columns: ColumnProps<MenuItem>[] = [
    {
      title: '中文名称',
      dataIndex: 'labelZh',
      render: (value: string, record: MenuItem) => (
        <div style={{ paddingLeft: `${(record.depth ?? 0) * 16}px` }}>
          <Space>
            <span>{value}</span>
            <Tag size="small">{record.parentId ? '子菜单' : '顶级'}</Tag>
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
      ...tableColumnPresets.action,
      title: '操作',
      dataIndex: 'id',
      render: (_: unknown, record: MenuItem) => (
        <Space>
          {canManageMenus ? <Button icon={<IconEdit />} theme="borderless" size="small" onClick={() => { setEditing(findMenuItemByID(menuTree, record.id) ?? record); setModalVisible(true) }} /> : null}
          {canManageMenus ? (
            <Popconfirm title="确认删除？" onConfirm={() => deleteMutation.mutate(record.id)}>
              <Button icon={<IconDelete />} theme="borderless" type="danger" size="small" />
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
        description="维护菜单层级、路径、可见性、排序和图标信息。"
        actions={canManageMenus ? <Button icon={<IconPlus />} theme="solid" onClick={() => { setEditing(null); setModalVisible(true) }}>新建菜单</Button> : null}
      />
      <AdminTable columns={columns} dataSource={menuItems} rowKey="id" loading={isLoading} pageSize={menuPageSize} />
      <Modal title={editing ? '编辑菜单' : '新建菜单'} visible={modalVisible} onCancel={() => { setModalVisible(false); setEditing(null) }} footer={null}>
        <Form onSubmit={(values) => { if (!canManageMenus) return; handleSubmit(values) }} initValues={editing ? { labelZh: editing.labelZh, labelEn: editing.labelEn, parentId: editing.parentId || '', path: editing.path, iconKey: editing.iconKey, section: editing.section, sortOrder: editing.sortOrder, enabled: editing.enabled } : { enabled: true, sortOrder: 0, section: 'system', parentId: '' }}>
          <Form.Input field="labelZh" label="中文名称" rules={[{ required: true, message: '请输入中文名称' }]} />
          <Form.Input field="labelEn" label="英文名称" />
          <Form.Select field="parentId" label="父级菜单" optionList={parentOptions} />
          <Form.Input field="path" label="路径" rules={[{ required: true, message: '请输入路径' }]} />
          <Form.Input field="iconKey" label="图标" />
          <Form.Input field="section" label="分组" />
          <Form.InputNumber field="sortOrder" label="排序" />
          <Form.Switch field="enabled" label="是否启用" />
          <div className="kc-form-actions">
            <Button onClick={() => setModalVisible(false)}>取消</Button>
            {canManageMenus ? (
              <Button htmlType="submit" theme="solid" loading={createMutation.isPending || updateMutation.isPending}>
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

  const columns: ColumnProps<AuditLog>[] = [
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

  const columns: ColumnProps<OperationLog>[] = [
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
