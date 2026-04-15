import { useMemo, useRef, useState } from 'react'
import { Button, Modal, Form, Tag, Toast, Popconfirm, Space } from '@douyinfe/semi-ui'
import { IconPlus, IconEdit, IconDelete } from '@douyinfe/semi-icons'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { AdminTable } from '@/components/admin-table'
import { PageHeader } from '@/components/page-header'
import { BooleanTag } from '@/components/status-tag'
import { api } from '@/services/api-client'
import { formatDateTime } from '@/utils/time'
import { tableColumnPresets } from '@/utils/table-columns'
import type { ApiResponse, BusinessLine, DeliveryEnvironment, ScopeGrant } from '@/types'
import type { ColumnProps } from '@douyinfe/semi-ui/lib/es/table'

type ModalFormApi = {
  validate: () => Promise<Record<string, unknown>>
}

function parseCSV(value: unknown) {
  return String(value ?? '')
    .split(',')
    .map((item) => item.trim())
    .filter(Boolean)
}

export function AccessScopeGrantsPage() {
  const queryClient = useQueryClient()
  const formApiRef = useRef<ModalFormApi | null>(null)
  const [modalVisible, setModalVisible] = useState(false)
  const [editing, setEditing] = useState<ScopeGrant | null>(null)

  const grantsQuery = useQuery({
    queryKey: ['scope-grants'],
    queryFn: () => api.get<ApiResponse<ScopeGrant[]>>('/access/scope-grants'),
  })
  const businessLinesQuery = useQuery({
    queryKey: ['business-lines'],
    queryFn: () => api.get<ApiResponse<BusinessLine[]>>('/business-lines'),
  })
  const environmentsQuery = useQuery({
    queryKey: ['delivery-environments'],
    queryFn: () => api.get<ApiResponse<DeliveryEnvironment[]>>('/delivery-environments'),
  })
  const applicationsQuery = useQuery({
    queryKey: ['applications'],
    queryFn: () => api.get<ApiResponse<Array<{ id: string; name: string }>>>('/applications'),
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

  const createMutation = useMutation({
    mutationFn: (values: Record<string, unknown>) => api.post('/access/scope-grants', values),
    onSuccess: () => {
      Toast.success('授权项创建成功')
      queryClient.invalidateQueries({ queryKey: ['scope-grants'] })
      setModalVisible(false)
    },
    onError: (err: Error) => Toast.error(err.message),
  })
  const updateMutation = useMutation({
    mutationFn: ({ id, values }: { id: string; values: Record<string, unknown> }) => api.put(`/access/scope-grants/${id}`, values),
    onSuccess: () => {
      Toast.success('授权项更新成功')
      queryClient.invalidateQueries({ queryKey: ['scope-grants'] })
      setModalVisible(false)
      setEditing(null)
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
        title="授权范围"
        description="按用户或用户组维护业务线、环境、应用级别的可管理范围。"
        actions={<Button icon={<IconPlus />} theme="solid" onClick={() => { setEditing(null); setModalVisible(true) }}>新建授权项</Button>}
      />
      <AdminTable columns={columns} dataSource={grantsQuery.data?.data ?? []} rowKey="id" loading={grantsQuery.isLoading} />
      <Modal
        title={editing ? '编辑授权项' : '新建授权项'}
        visible={modalVisible}
        onCancel={() => { setModalVisible(false); setEditing(null) }}
        onOk={() => {
          formApiRef.current?.validate().then((values) => {
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
          }).catch(() => undefined)
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
          getFormApi={(formApi) => { formApiRef.current = formApi as ModalFormApi }}
          initValues={editing ? {
            ...editing,
            environmentIds: editing.environmentIds.join(', '),
            applicationIds: editing.applicationIds.join(', '),
          } : { enabled: true, effect: 'allow', subjectType: 'team' }}
        >
          <Form.Select field="subjectType" label="主体类型" optionList={[{ value: 'team', label: '用户组' }, { value: 'user', label: '用户' }]} />
          <Form.Input field="subjectId" label="主体 ID" rules={[{ required: true, message: '请输入主体 ID' }]} />
          <Form.Select
            field="businessLineId"
            label="业务线"
            optionList={(businessLinesQuery.data?.data ?? []).map((item) => ({ value: item.id, label: item.name }))}
            rules={[{ required: true, message: '请选择业务线' }]}
          />
          <Form.Input field="environmentIds" label="环境 IDs" placeholder="留空表示全部环境，多个以逗号分隔" />
          <Form.Input field="applicationIds" label="应用 IDs" placeholder="留空表示全部应用，多个以逗号分隔" />
          <Form.Input field="role" label="角色" rules={[{ required: true, message: '请输入角色' }]} />
          <Form.Select field="effect" label="效果" optionList={[{ value: 'allow', label: '允许' }]} />
          <Form.Switch field="enabled" label="启用" />
        </Form>
      </Modal>
    </div>
  )
}
