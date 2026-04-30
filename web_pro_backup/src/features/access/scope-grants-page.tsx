import { useMemo, useState } from 'react'
import { App, Button, Form, Input, Modal, Popconfirm, Select, Space, Switch, Tag } from 'antd'
import { DeleteOutlined, EditOutlined, PlusOutlined } from '@ant-design/icons'
import type { TableColumnsType } from 'antd'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { AdminTable } from '@/components/admin-table'
import { hasPermission, usePermissionSnapshot } from '@/features/auth/permission-snapshot'
import { accessScopeGrantsQueryKey, accessScopeGrantsQueryOptions } from '@/features/access/queries'
import {
  deliveryApplicationsQueryOptions,
  deliveryBusinessLinesQueryOptions,
  deliveryEnvironmentsQueryOptions,
} from '@/features/delivery/queries'
import { PageHeader } from '@/components/page-header'
import { BooleanTag } from '@/components/status-tag'
import { api } from '@/services/api-client'
import { formatDateTime } from '@/utils/time'
import { tableColumnPresets } from '@/utils/table-columns'
import type { ScopeGrant } from '@/types'

type ColumnProps<T> = TableColumnsType<T>[number]
type ScopeGrantFormValues = {
  applicationIds?: string
  businessLineId: string
  effect: string
  enabled: boolean
  environmentIds?: string
  role: string
  subjectId: string
  subjectType: 'team' | 'user'
}

function parseCSV(value: unknown) {
  return String(value ?? '')
    .split(',')
    .map((item) => item.trim())
    .filter(Boolean)
}

export function AccessScopeGrantsPage() {
  const { message } = App.useApp()
  const permissionSnapshotQuery = usePermissionSnapshot()
  const snapshot = permissionSnapshotQuery.data?.data
  const canViewScopeGrants = hasPermission(snapshot, 'access.scope-grants.view')
  const canManageScopeGrants = hasPermission(snapshot, 'access.scope-grants.manage')
  const queryClient = useQueryClient()
  const [form] = Form.useForm<ScopeGrantFormValues>()
  const [modalVisible, setModalVisible] = useState(false)
  const [editing, setEditing] = useState<ScopeGrant | null>(null)

  const grantsQuery = useQuery(accessScopeGrantsQueryOptions())
  const businessLinesQuery = useQuery(deliveryBusinessLinesQueryOptions())
  const environmentsQuery = useQuery(deliveryEnvironmentsQueryOptions())
  const applicationsQuery = useQuery(deliveryApplicationsQueryOptions())

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

  const createMutation = useMutation({
    mutationFn: (values: Record<string, unknown>) => api.post('/access/scope-grants', values),
    onSuccess: () => {
      message.success('授权项创建成功')
      queryClient.invalidateQueries({ queryKey: accessScopeGrantsQueryKey })
      setModalVisible(false)
    },
    onError: (err: Error) => message.error(err.message),
  })
  const updateMutation = useMutation({
    mutationFn: ({ id, values }: { id: string; values: Record<string, unknown> }) => api.put(`/access/scope-grants/${id}`, values),
    onSuccess: () => {
      message.success('授权项更新成功')
      queryClient.invalidateQueries({ queryKey: accessScopeGrantsQueryKey })
      setModalVisible(false)
      setEditing(null)
    },
    onError: (err: Error) => message.error(err.message),
  })
  const deleteMutation = useMutation({
    mutationFn: (id: string) => api.delete(`/access/scope-grants/${id}`),
    onSuccess: () => {
      message.success('授权项已删除')
      queryClient.invalidateQueries({ queryKey: accessScopeGrantsQueryKey })
    },
    onError: (err: Error) => message.error(err.message),
  })

  const columns: ColumnProps<ScopeGrant>[] = [
    { title: '主体类型', dataIndex: 'subjectType', render: (value: string) => value === 'team' ? '用户组' : '用户' },
    { title: '主体 ID', dataIndex: 'subjectId' },
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
    { title: '启用', dataIndex: 'enabled', render: (value: boolean) => <BooleanTag value={value} /> },
    { ...tableColumnPresets.datetime, title: '更新时间', dataIndex: 'updatedAt', render: (value: string) => formatDateTime(value) },
    {
      ...tableColumnPresets.action,
      title: '操作',
      dataIndex: 'id',
      render: (_: unknown, record: ScopeGrant) => (
        <Space>
          {canManageScopeGrants ? (
            <>
              <Button icon={<EditOutlined />} type="text" size="small" onClick={() => { setEditing(record); setModalVisible(true) }} />
              <Popconfirm title="确认删除？" onConfirm={() => deleteMutation.mutate(record.id)}>
                <Button icon={<DeleteOutlined />} type="text" danger size="small" />
              </Popconfirm>
            </>
          ) : '-'}
        </Space>
      ),
    },
  ]

  if (!canViewScopeGrants) {
    return <div className="kc-page">当前账号没有授权范围页面权限。</div>
  }

  return (
    <div className="kc-page">
      <PageHeader
        title="授权范围"
        description="按用户或用户组维护业务线、环境、应用级别的可管理范围。"
        actions={canManageScopeGrants ? <Button icon={<PlusOutlined />} type="primary" onClick={() => { setEditing(null); setModalVisible(true) }}>新建授权项</Button> : null}
      />
      <AdminTable columns={columns} dataSource={grantsQuery.data?.data ?? []} rowKey="id" loading={grantsQuery.isLoading} />
      <Modal
        title={editing ? '编辑授权项' : '新建授权项'}
        open={modalVisible}
        onCancel={() => { setModalVisible(false); setEditing(null) }}
        onOk={async () => {
          try {
            const values = await form.validateFields()
            const payload = {
              ...values,
              environmentIds: parseCSV(values.environmentIds),
              applicationIds: parseCSV(values.applicationIds),
            }
            if (editing) {
              updateMutation.mutate({ id: editing.id, values: payload })
              return
            }
            createMutation.mutate(payload)
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
            environmentIds: editing.environmentIds.join(', '),
            applicationIds: editing.applicationIds.join(', '),
          } : { enabled: true, effect: 'allow', subjectType: 'team' }}
        >
          <Form.Item name="subjectType" label="主体类型">
            <Select options={[{ value: 'team', label: '用户组' }, { value: 'user', label: '用户' }]} />
          </Form.Item>
          <Form.Item name="subjectId" label="主体 ID" rules={[{ required: true, message: '请输入主体 ID' }]}>
            <Input />
          </Form.Item>
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
            <Select options={[{ value: 'allow', label: '允许' }]} />
          </Form.Item>
          <Form.Item name="enabled" label="启用" valuePropName="checked">
            <Switch />
          </Form.Item>
        </Form>
      </Modal>
    </div>
  )
}
