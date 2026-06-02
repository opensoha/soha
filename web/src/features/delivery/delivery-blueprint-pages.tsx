import { useState } from 'react'
import { App, Button, Form, Input, Modal, Space, Switch, Tag } from 'antd'
import { EditOutlined, EyeOutlined, PlayCircleOutlined, PlusOutlined } from '@ant-design/icons'
import type { TableColumnsType } from 'antd'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { ManagementIconButton } from '@/components/management-list'
import { DeliveryTable } from '@/features/delivery/delivery-table'
import { hasPermission, usePermissionSnapshot } from '@/features/auth/permission-snapshot'
import { StatusTag } from '@/components/status-tag'
import { api } from '@/services/api-client'
import type {
  ApiResponse,
  BlueprintBootstrapResult,
  DeliveryBlueprint,
  RenderedDeliverySpec,
} from '@/types'
import { formatDateTime } from '@/utils/time'
import { tableColumnPresets } from '@/utils/table-columns'

function stringify(value: unknown, fallback: string) {
  if (value == null) return fallback
  return JSON.stringify(value, null, 2)
}

function parseJSONObject(raw: string, field: string) {
  const text = raw.trim()
  if (!text) return {}
  const value = JSON.parse(text)
  if (!value || typeof value !== 'object' || Array.isArray(value)) {
    throw new Error(`${field} 需要是 JSON 对象`)
  }
  return value
}

function parseJSONArray(raw: string, field: string) {
  const text = raw.trim()
  if (!text) return []
  const value = JSON.parse(text)
  if (!Array.isArray(value)) {
    throw new Error(`${field} 需要是 JSON 数组`)
  }
  return value
}

export function DeliveryBlueprintsPage() {
  const { message } = App.useApp()
  const queryClient = useQueryClient()
  const permissionSnapshotQuery = usePermissionSnapshot()
  const canManage = hasPermission(permissionSnapshotQuery.data?.data, 'delivery.application.update')
  const [form] = Form.useForm<Record<string, unknown>>()
  const [modalVisible, setModalVisible] = useState(false)
  const [editing, setEditing] = useState<DeliveryBlueprint | null>(null)
  const [specModalVisible, setSpecModalVisible] = useState(false)
  const [bootstrapModalVisible, setBootstrapModalVisible] = useState(false)
  const [renderedSpec, setRenderedSpec] = useState<RenderedDeliverySpec | null>(null)
  const [bootstrapResult, setBootstrapResult] = useState<BlueprintBootstrapResult | null>(null)

  const blueprintsQuery = useQuery({
    queryKey: ['delivery-blueprints'],
    queryFn: () => api.get<ApiResponse<DeliveryBlueprint[]>>('/delivery/blueprints'),
  })

  const createMutation = useMutation({
    mutationFn: (values: Record<string, unknown>) => api.post('/delivery/blueprints', values),
    onSuccess: () => {
      void message.success('交付蓝图已创建')
      void queryClient.invalidateQueries({ queryKey: ['delivery-blueprints'] })
      setModalVisible(false)
      setEditing(null)
    },
    onError: (err: Error) => void message.error(err.message),
  })

  const updateMutation = useMutation({
    mutationFn: ({ id, values }: { id: string; values: Record<string, unknown> }) => api.put(`/delivery/blueprints/${id}`, values),
    onSuccess: () => {
      void message.success('交付蓝图已更新')
      void queryClient.invalidateQueries({ queryKey: ['delivery-blueprints'] })
      setModalVisible(false)
      setEditing(null)
    },
    onError: (err: Error) => void message.error(err.message),
  })

  const renderMutation = useMutation({
    mutationFn: (id: string) => api.post<ApiResponse<RenderedDeliverySpec>>(`/delivery/blueprints/${id}/render-spec`, {}),
    onSuccess: (payload: ApiResponse<RenderedDeliverySpec>) => {
      setRenderedSpec(payload.data)
      setSpecModalVisible(true)
    },
    onError: (err: Error) => void message.error(err.message),
  })

  const bootstrapMutation = useMutation({
    mutationFn: (id: string) => api.post<ApiResponse<BlueprintBootstrapResult>>(`/delivery/blueprints/${id}/bootstrap-application`, {}),
    onSuccess: (payload: ApiResponse<BlueprintBootstrapResult>) => {
      setBootstrapResult(payload.data)
      setBootstrapModalVisible(true)
      void queryClient.invalidateQueries({ queryKey: ['applications'] })
      void queryClient.invalidateQueries({ queryKey: ['application-environments'] })
    },
    onError: (err: Error) => void message.error(err.message),
  })

  const columns: TableColumnsType<DeliveryBlueprint> = [
    { title: '名称', dataIndex: 'name' },
    { title: 'Key', dataIndex: 'key', render: (value: string) => <Tag>{value}</Tag> },
    { title: '应用草稿', dataIndex: 'applicationDraft', render: (value: DeliveryBlueprint['applicationDraft']) => value?.name || value?.key || '-' },
    { title: '构建源', dataIndex: 'buildSources', render: (value?: DeliveryBlueprint['buildSources']) => `${value?.length ?? 0} 个` },
    { title: '环境绑定', dataIndex: 'environmentBindings', render: (value?: DeliveryBlueprint['environmentBindings']) => `${value?.length ?? 0} 个` },
    {
      ...tableColumnPresets.status,
      title: '状态',
      dataIndex: 'enabled',
      render: (value: boolean) => <StatusTag value={value ? 'enabled' : 'disabled'} />,
    },
    { ...tableColumnPresets.datetime, title: '更新时间', dataIndex: 'updatedAt', render: (value: string) => formatDateTime(value) },
    {
      ...tableColumnPresets.action,
      title: '操作',
      dataIndex: 'id',
      render: (_: unknown, record: DeliveryBlueprint) => (
        <Space className="soha-row-action-icons" size={2}>
          <ManagementIconButton
            aria-label="渲染规范"
            icon={<EyeOutlined />}
            size="small"
            tooltip="渲染规范"
            onClick={() => renderMutation.mutate(record.id)}
          />
          <ManagementIconButton
            aria-label="平台接入"
            icon={<PlayCircleOutlined />}
            size="small"
            tooltip="平台接入"
            onClick={() => bootstrapMutation.mutate(record.id)}
          />
          {canManage ? (
            <ManagementIconButton
              aria-label="编辑蓝图"
              icon={<EditOutlined />}
              size="small"
              tooltip="编辑"
              onClick={() => { setEditing(record); setModalVisible(true) }}
            />
          ) : null}
        </Space>
      ),
    },
  ]

  const handleSubmit = (values: Record<string, unknown>) => {
    try {
      const payload = {
        id: editing?.id,
        key: String(values.key || '').trim(),
        name: String(values.name || '').trim(),
        description: String(values.description || '').trim(),
        applicationDraft: parseJSONObject(String(values.applicationDraftText || '{}'), 'applicationDraft'),
        buildSources: parseJSONArray(String(values.buildSourcesText || '[]'), 'buildSources'),
        environmentBindings: parseJSONArray(String(values.environmentBindingsText || '[]'), 'environmentBindings'),
        files: parseJSONArray(String(values.filesText || '[]'), 'files'),
        executionHints: parseJSONObject(String(values.executionHintsText || '{}'), 'executionHints'),
        postCreateActions: String(values.postCreateActionsText || '')
          .split('\n')
          .map((item) => item.trim())
          .filter(Boolean),
        enabled: Boolean(values.enabled),
      }
      if (editing) {
        updateMutation.mutate({ id: editing.id, values: payload })
      } else {
        createMutation.mutate(payload)
      }
    } catch (error) {
      void message.error(error instanceof Error ? error.message : '交付蓝图保存失败')
    }
  }

  return (
    <div className="soha-page">
      <DeliveryTable
        rowKey="id"
        loading={blueprintsQuery.isLoading}
        columns={columns}
        dataSource={blueprintsQuery.data?.data ?? []}
        actions={canManage ? (
          <Button type="primary" icon={<PlusOutlined />} onClick={() => { setEditing(null); setModalVisible(true) }}>
            新建蓝图
          </Button>
        ) : null}
        refreshing={blueprintsQuery.isFetching}
        onRefresh={() => void blueprintsQuery.refetch()}
      />

      <Modal
        width={960}
        title={editing ? '编辑交付蓝图' : '新建交付蓝图'}
        open={modalVisible}
        onCancel={() => { setModalVisible(false); setEditing(null) }}
        footer={null}
        destroyOnHidden
      >
        <Form
          form={form}
          layout="vertical"
          key={editing?.id ?? 'create-blueprint'}
          initialValues={{
            key: editing?.key ?? '',
            name: editing?.name ?? '',
            description: editing?.description ?? '',
            applicationDraftText: stringify(editing?.applicationDraft ?? {
              name: 'sample-app',
              key: 'sample-app',
              group: 'default',
              language: 'go',
              enabled: true,
              metadata: {},
            }, '{}'),
            buildSourcesText: stringify(editing?.buildSources ?? [], '[]'),
            environmentBindingsText: stringify(editing?.environmentBindings ?? [], '[]'),
            filesText: stringify(editing?.files ?? [], '[]'),
            executionHintsText: stringify(editing?.executionHints ?? {}, '{}'),
            postCreateActionsText: (editing?.postCreateActions ?? []).join('\n'),
            enabled: editing?.enabled ?? true,
          }}
          onFinish={handleSubmit}
        >
          <Form.Item name="key" label="蓝图 Key" rules={[{ required: true, message: '请输入蓝图 Key' }]}>
            <Input />
          </Form.Item>
          <Form.Item name="name" label="蓝图名称" rules={[{ required: true, message: '请输入蓝图名称' }]}>
            <Input />
          </Form.Item>
          <Form.Item name="description" label="说明">
            <Input.TextArea rows={2} />
          </Form.Item>
          <Form.Item name="applicationDraftText" label="Application Draft(JSON)" rules={[{ required: true, message: '请输入应用草稿 JSON' }]}>
            <Input.TextArea rows={8} spellCheck={false} />
          </Form.Item>
          <Form.Item name="buildSourcesText" label="Build Sources(JSON Array)">
            <Input.TextArea rows={8} spellCheck={false} />
          </Form.Item>
          <Form.Item name="environmentBindingsText" label="Environment Bindings(JSON Array)">
            <Input.TextArea rows={8} spellCheck={false} />
          </Form.Item>
          <Form.Item name="filesText" label="Files(JSON Array)">
            <Input.TextArea rows={8} spellCheck={false} />
          </Form.Item>
          <Form.Item name="executionHintsText" label="Execution Hints(JSON)">
            <Input.TextArea rows={4} spellCheck={false} />
          </Form.Item>
          <Form.Item name="postCreateActionsText" label="Post Create Actions(每行一个)">
            <Input.TextArea rows={4} spellCheck={false} />
          </Form.Item>
          <Form.Item name="enabled" label="启用" valuePropName="checked">
            <Switch />
          </Form.Item>
          <div className="soha-form-actions">
            <Button onClick={() => { setModalVisible(false); setEditing(null) }}>取消</Button>
            <Button htmlType="submit" type="primary" loading={createMutation.isPending || updateMutation.isPending}>保存</Button>
          </div>
        </Form>
      </Modal>

      <Modal width={960} title="渲染结果" open={specModalVisible} onCancel={() => setSpecModalVisible(false)} footer={null}>
        <pre className="soha-json-block">{JSON.stringify(renderedSpec ?? {}, null, 2)}</pre>
      </Modal>

      <Modal width={960} title="平台接入结果" open={bootstrapModalVisible} onCancel={() => setBootstrapModalVisible(false)} footer={null}>
        <pre className="soha-json-block">{JSON.stringify(bootstrapResult ?? {}, null, 2)}</pre>
      </Modal>
    </div>
  )
}
