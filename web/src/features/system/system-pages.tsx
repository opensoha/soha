import { useMemo, useState } from 'react'
import { Button, Modal, Form, Toast, Popconfirm, Space } from '@douyinfe/semi-ui'
import { IconPlus, IconEdit, IconDelete } from '@douyinfe/semi-icons'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { AdminTable } from '@/components/admin-table'
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

export function OnlineUsersPage() {
  const queryClient = useQueryClient()
  const [selectedSessionIds, setSelectedSessionIds] = useState<string[]>([])

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
    { title: '来源', dataIndex: 'source', render: (value: string) => value || '-' },
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
        <Popconfirm title="确认下线该用户会话？" onConfirm={() => revokeMutation.mutate(record.id)}>
          <Button size="small" theme="borderless" type="danger" loading={revokeMutation.isPending}>
            下线用户
          </Button>
        </Popconfirm>
      ),
    },
  ]

  return (
    <div className="kc-page">
      <PageHeader
        title="在线用户"
        description="查看当前在线会话、登录来源、最后活跃时间与会话到期信息。"
        actions={
          <Button
            theme="light"
            type="danger"
            disabled={selectedSessions.length === 0}
            loading={batchRevokeMutation.isPending}
            onClick={() => batchRevokeMutation.mutate(selectedSessions.map((item: OnlineUser) => item.id))}
          >
            {`批量下线 (${selectedSessions.length})`}
          </Button>
        }
      />
      <AdminTable
        columns={columns}
        dataSource={sessions}
        rowKey="id"
        loading={isLoading}
        pageSize={20}
        rowSelection={{
          selectedRowKeys: selectedSessionIds,
          onChange: (selectedRowKeys: string[]) => setSelectedSessionIds(selectedRowKeys),
        }}
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
  const [modalVisible, setModalVisible] = useState(false)
  const [editing, setEditing] = useState<Announcement | null>(null)

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
        title="公告管理"
        description="维护系统公告内容、发布状态与创建时间。"
        actions={<Button icon={<IconPlus />} theme="solid" onClick={() => { setEditing(null); setModalVisible(true) }}>新建公告</Button>}
      />
      <AdminTable columns={columns} dataSource={data?.data ?? []} rowKey="id" loading={isLoading} />
      <Modal title={editing ? '编辑公告' : '新建公告'} visible={modalVisible} onCancel={() => { setModalVisible(false); setEditing(null) }} footer={null}>
        <Form onSubmit={handleSubmit} initValues={editing ? { title: editing.title, summary: editing.summary, content: editing.content, level: editing.level, status: editing.status } : { level: 'info', status: 'draft' }}>
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
            <Button htmlType="submit" theme="solid" loading={createMutation.isPending || updateMutation.isPending}>
              {editing ? '更新' : '创建'}
            </Button>
          </div>
        </Form>
      </Modal>
    </div>
  )
}

/* ─── Menus ─── */

interface MenuItem {
  id: string
  labelZh: string
  labelEn: string
  path: string
  iconKey: string
  section: string
  sortOrder: number
  enabled: boolean
}

export function MenusPage() {
  const queryClient = useQueryClient()
  const [modalVisible, setModalVisible] = useState(false)
  const [editing, setEditing] = useState<MenuItem | null>(null)

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
    if (editing) {
      updateMutation.mutate({ id: editing.id, values })
    } else {
      createMutation.mutate(values)
    }
  }

  const columns: ColumnProps<MenuItem>[] = [
    { title: '中文名称', dataIndex: 'labelZh' },
    { title: '英文名称', dataIndex: 'labelEn' },
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
        title="菜单管理"
        description="维护菜单路径、可见性、排序和图标信息。"
        actions={<Button icon={<IconPlus />} theme="solid" onClick={() => { setEditing(null); setModalVisible(true) }}>新建菜单</Button>}
      />
      <AdminTable columns={columns} dataSource={data?.data ?? []} rowKey="id" loading={isLoading} pageSize={20} />
      <Modal title={editing ? '编辑菜单' : '新建菜单'} visible={modalVisible} onCancel={() => { setModalVisible(false); setEditing(null) }} footer={null}>
        <Form onSubmit={handleSubmit} initValues={editing ? { labelZh: editing.labelZh, labelEn: editing.labelEn, path: editing.path, iconKey: editing.iconKey, section: editing.section, sortOrder: editing.sortOrder, enabled: editing.enabled } : { enabled: true, sortOrder: 0, section: 'system' }}>
          <Form.Input field="labelZh" label="中文名称" rules={[{ required: true, message: '请输入中文名称' }]} />
          <Form.Input field="labelEn" label="英文名称" />
          <Form.Input field="path" label="路径" rules={[{ required: true, message: '请输入路径' }]} />
          <Form.Input field="iconKey" label="图标" />
          <Form.Input field="section" label="分组" />
          <Form.InputNumber field="sortOrder" label="排序" />
          <Form.Switch field="enabled" label="是否启用" />
          <div className="kc-form-actions">
            <Button onClick={() => setModalVisible(false)}>取消</Button>
            <Button htmlType="submit" theme="solid" loading={createMutation.isPending || updateMutation.isPending}>
              {editing ? '更新' : '创建'}
            </Button>
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
