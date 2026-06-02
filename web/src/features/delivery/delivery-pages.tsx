import { useState } from 'react'
import { App, Button, Card, Form, Input, Modal, Popconfirm, Select, Space, Switch, Tag, Typography } from 'antd'
import { DeleteOutlined, EditOutlined, PlayCircleOutlined, PlusOutlined } from '@ant-design/icons'
import type { TableColumnsType } from 'antd'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { ManagementIconButton } from '@/components/management-list'
import { DeliveryTable } from '@/features/delivery/delivery-table'
import { hasPermission, usePermissionSnapshot } from '@/features/auth/permission-snapshot'
import { useI18n } from '@/i18n'
import { StatusTag } from '@/components/status-tag'
import { api } from '@/services/api-client'
import { formatDateTime } from '@/utils/time'
import { tableColumnPresets } from '@/utils/table-columns'
import type { ApiResponse, WorkflowNodeRun } from '@/types'

const { Text } = Typography
type ColumnProps<T> = TableColumnsType<T>[number]

/* ─── Applications ─── */

interface Application {
  id: string
  name: string
  key: string
  group: string
  businessLineId?: string
  language: string
  repositoryPath?: string
  defaultBranch?: string
  defaultTag?: string
  buildImage?: string
  buildContextDir?: string
  dockerfilePath?: string
  enabled: boolean
  createdAt: string
  updatedAt: string
}

export function ApplicationsPage() {
  const { message } = App.useApp()
  const queryClient = useQueryClient()
  const permissionSnapshotQuery = usePermissionSnapshot()
  const [form] = Form.useForm<Record<string, unknown>>()
  const [modalVisible, setModalVisible] = useState(false)
  const [editing, setEditing] = useState<Application | null>(null)

  const { data, isLoading, isFetching, refetch } = useQuery({
    queryKey: ['applications'],
    queryFn: () => api.get<ApiResponse<Application[]>>('/applications'),
  })

  const permissionSnapshot = permissionSnapshotQuery.data?.data
  const canCreateApplication = hasPermission(permissionSnapshot, 'delivery.application.create')
  const canUpdateApplication = hasPermission(permissionSnapshot, 'delivery.application.update')
  const canDeleteApplication = hasPermission(permissionSnapshot, 'delivery.application.delete')

  const createMutation = useMutation({
    mutationFn: (values: Record<string, unknown>) => api.post('/applications', values),
    onSuccess: () => {
      message.success('应用创建成功')
      queryClient.invalidateQueries({ queryKey: ['applications'] })
      setModalVisible(false)
    },
    onError: (err: Error) => message.error(err.message),
  })

  const updateMutation = useMutation({
    mutationFn: ({ id, values }: { id: string; values: Record<string, unknown> }) =>
      api.put(`/applications/${id}`, values),
    onSuccess: () => {
      message.success('应用更新成功')
      queryClient.invalidateQueries({ queryKey: ['applications'] })
      setModalVisible(false)
      setEditing(null)
    },
    onError: (err: Error) => message.error(err.message),
  })

  const deleteMutation = useMutation({
    mutationFn: (id: string) => api.delete(`/applications/${id}`),
    onSuccess: () => {
      message.success('应用已删除')
      queryClient.invalidateQueries({ queryKey: ['applications'] })
    },
    onError: (err: Error) => message.error(err.message),
  })

  const handleSubmit = (values: Record<string, unknown>) => {
    if (editing) {
      updateMutation.mutate({ id: editing.id, values })
    } else {
      createMutation.mutate(values)
    }
  }

  const columns: ColumnProps<Application>[] = [
    { title: '名称', dataIndex: 'name' },
    { title: 'Key', dataIndex: 'key' },
    { title: '分组', dataIndex: 'group' },
    { title: '语言', dataIndex: 'language' },
    {
      ...tableColumnPresets.status,
      title: '状态',
      dataIndex: 'enabled',
      render: (enabled: boolean) => <StatusTag value={enabled ? 'enabled' : 'disabled'} />,
    },
    { ...tableColumnPresets.datetime, title: '更新时间', dataIndex: 'updatedAt', render: (value: string) => formatDateTime(value) },
    {
      ...tableColumnPresets.action,
      title: '操作',
      dataIndex: 'id',
      render: (_: unknown, record: Application) => (
        <Space className="soha-row-action-icons" size={2}>
          {canUpdateApplication ? (
            <ManagementIconButton
              aria-label="编辑应用"
              icon={<EditOutlined />}
              size="small"
              tooltip="编辑"
              onClick={() => { setEditing(record); setModalVisible(true) }}
            />
          ) : null}
          {canDeleteApplication ? (
            <Popconfirm title="确认删除？" onConfirm={() => deleteMutation.mutate(record.id)} placement="topRight">
              <ManagementIconButton
                aria-label="删除应用"
                danger
                icon={<DeleteOutlined />}
                size="small"
                tooltip="删除"
              />
            </Popconfirm>
          ) : null}
          {!canUpdateApplication && !canDeleteApplication ? '-' : null}
        </Space>
      ),
    },
  ]

  return (
    <div className="soha-page">
      <Card className="soha-scope-hint-card">
        <Text type="secondary">
          应用作为服务集合维护基础信息；服务仓库、分支和构建参数在应用详情的服务配置中维护。
        </Text>
      </Card>
      <DeliveryTable
        actions={canCreateApplication ? (
          <Button icon={<PlusOutlined />} type="primary" onClick={() => { setEditing(null); setModalVisible(true) }}>
            新建应用
          </Button>
        ) : null}
        refreshing={isFetching}
        onRefresh={() => void refetch()}
        columns={columns}
        dataSource={data?.data ?? []}
        rowKey="id"
        loading={isLoading}
      />
      <Modal
        title={editing ? '编辑应用' : '新建应用'}
        open={modalVisible}
        onCancel={() => { setModalVisible(false); setEditing(null) }}
        footer={null}
        destroyOnHidden
      >
        <Form
          form={form}
          key={editing?.id ?? 'create-application'}
          layout="vertical"
          onFinish={handleSubmit}
          initialValues={
            editing
              ? {
                  name: editing.name,
	                  key: editing.key,
	                  group: editing.group,
	                  language: editing.language,
	                  defaultTag: editing.defaultTag,
	                  buildImage: editing.buildImage,
	                  buildContextDir: editing.buildContextDir,
	                  dockerfilePath: editing.dockerfilePath,
	                  enabled: editing.enabled,
	                }
	              : { enabled: true, language: 'node' }
          }
        >
          <Form.Item name="name" label="应用名称" rules={[{ required: true, message: '请输入应用名称' }]}>
            <Input />
          </Form.Item>
          <Form.Item name="key" label="应用 Key" rules={[{ required: true, message: '请输入应用 Key' }]}>
            <Input />
          </Form.Item>
          <Form.Item name="group" label="应用分组" rules={[{ required: true, message: '请输入应用分组' }]}>
            <Input />
          </Form.Item>
          <Form.Item name="language" label="语言">
            <Select
              options={[
                { value: 'node', label: 'Node.js' },
                { value: 'java', label: 'Java' },
                { value: 'go', label: 'Go' },
                { value: 'python', label: 'Python' },
              ]}
            />
          </Form.Item>
          <Form.Item name="defaultTag" label="默认镜像 Tag">
            <Input />
          </Form.Item>
          <Form.Item name="buildImage" label="镜像仓库地址">
            <Input />
          </Form.Item>
          <Form.Item name="buildContextDir" label="构建上下文目录">
            <Input placeholder="." />
          </Form.Item>
          <Form.Item name="dockerfilePath" label="Dockerfile 路径">
            <Input placeholder="./Dockerfile" />
          </Form.Item>
          <Form.Item name="enabled" label="启用" valuePropName="checked">
            <Switch />
          </Form.Item>
          <div className="soha-form-actions">
            <Button onClick={() => setModalVisible(false)}>取消</Button>
            <Button htmlType="submit" type="primary" loading={createMutation.isPending || updateMutation.isPending}>
              {editing ? '更新' : '创建'}
            </Button>
          </div>
        </Form>
      </Modal>
    </div>
  )
}

/* ─── Workflows ─── */

interface Workflow {
  id: string
  applicationId: string
  workflowName: string
  clusterId: string
  namespace: string
  deploymentName: string
  status: string
  metadata?: {
    triggerBuild?: boolean
    triggerRelease?: boolean
    nodeRuns?: WorkflowNodeRun[]
  }
  nodeRuns?: WorkflowNodeRun[]
  createdAt: string
  updatedAt: string
}

export function WorkflowsPage() {
  const { t, localeCode } = useI18n()
  const { message } = App.useApp()
  const queryClient = useQueryClient()
  const permissionSnapshotQuery = usePermissionSnapshot()
  const canTriggerWorkflow = hasPermission(permissionSnapshotQuery.data?.data, 'delivery.workflows.trigger')

  const { data, isLoading, isFetching, refetch } = useQuery({
    queryKey: ['workflows'],
    queryFn: () => api.get<ApiResponse<Workflow[]>>('/workflows'),
  })

  const triggerMutation = useMutation({
    mutationFn: (record: Workflow) =>
      api.post('/workflows/trigger', {
        applicationId: record.applicationId,
        workflowName: record.workflowName,
        clusterId: record.clusterId,
        namespace: record.namespace,
        deploymentName: record.deploymentName,
        triggerBuild: record.metadata?.triggerBuild ?? true,
        triggerRelease: record.metadata?.triggerRelease ?? true,
    }),
    onSuccess: () => {
      message.success(localeCode === 'zh_CN' ? '工作流已触发' : 'Workflow triggered')
      queryClient.invalidateQueries({ queryKey: ['workflows'] })
    },
    onError: (err: Error) => message.error(err.message),
  })

  const columns: ColumnProps<Workflow>[] = [
    { title: t('common.workflow', 'Workflow'), dataIndex: 'workflowName' },
    { title: t('common.application', 'Application'), dataIndex: 'applicationId' },
    { title: t('common.cluster', 'Cluster'), dataIndex: 'clusterId' },
    { title: t('common.namespace', 'Namespace'), dataIndex: 'namespace' },
    {
      ...tableColumnPresets.status,
      title: t('common.status', 'Status'),
      dataIndex: 'status',
      render: (s: string) => <StatusTag value={s} />,
    },
    {
      title: t('page.delivery.workflows.nodeProgress', 'Node Progress'),
      dataIndex: 'nodeRuns',
      render: (_: unknown, record: Workflow) => {
        const nodeRuns = record.nodeRuns?.length ? record.nodeRuns : record.metadata?.nodeRuns ?? []
        if (!nodeRuns.length) return '-'
        const resolved = nodeRuns.filter((item) => item.status && item.status !== 'pending' && item.status !== 'running').length
        const running = nodeRuns.filter((item) => item.status === 'running').length
        const summary = `${resolved}/${nodeRuns.length}`
        if (running > 0) {
          return `${summary} · ${localeCode === 'zh_CN' ? '运行中' : 'running'} ${running}`
        }
        return summary
      },
    },
    { ...tableColumnPresets.datetime, title: localeCode === 'zh_CN' ? '最近运行' : 'Last Run', dataIndex: 'updatedAt', render: (value: string) => formatDateTime(value) },
    {
      ...tableColumnPresets.action,
      title: t('common.actions', 'Actions'),
      dataIndex: 'id',
      render: (_: unknown, record: Workflow) => (
        canTriggerWorkflow ? (
          <ManagementIconButton
            aria-label={localeCode === 'zh_CN' ? '触发工作流' : 'Trigger workflow'}
            icon={<PlayCircleOutlined />}
            size="small"
            tooltip={localeCode === 'zh_CN' ? '触发' : 'Trigger'}
            onClick={() => triggerMutation.mutate(record)}
          />
        ) : '-'
      ),
    },
  ]

  return (
    <div className="soha-page">
      <DeliveryTable
        refreshing={isFetching}
        onRefresh={() => void refetch()}
        columns={columns}
        dataSource={data?.data ?? []}
        rowKey="id"
        loading={isLoading}
      />
    </div>
  )
}

/* ─── Releases ─── */

interface Release {
  id: string
  applicationId: string
  clusterId: string
  namespace: string
  deploymentName: string
  status: string
  metadata?: Record<string, any>
  deployedAt?: string
  createdAt: string
}

export function ReleasesPage() {
  const { message } = App.useApp()
  const queryClient = useQueryClient()
  const permissionSnapshotQuery = usePermissionSnapshot()
  const canTriggerRelease = hasPermission(permissionSnapshotQuery.data?.data, 'delivery.releases.trigger')

  const { data, isLoading, isFetching, refetch } = useQuery({
    queryKey: ['releases'],
    queryFn: () => api.get<ApiResponse<Release[]>>('/releases'),
  })

  const triggerMutation = useMutation({
    mutationFn: (record: Release) =>
      api.post('/releases/trigger', {
        applicationId: record.applicationId,
        clusterId: record.clusterId,
        namespace: record.namespace,
        deploymentName: record.deploymentName,
        containerName: record.metadata?.containerName,
        image: record.metadata?.image,
        imageTag: record.metadata?.imageTag,
        releaseName: record.metadata?.releaseName,
    }),
    onSuccess: () => {
      message.success('发布已触发')
      queryClient.invalidateQueries({ queryKey: ['releases'] })
    },
    onError: (err: Error) => message.error(err.message),
  })

  const columns: ColumnProps<Release>[] = [
    { title: '应用', dataIndex: 'applicationId' },
    { title: '集群', dataIndex: 'clusterId' },
    { title: '命名空间', dataIndex: 'namespace' },
    { title: 'Deployment', dataIndex: 'deploymentName' },
    {
      ...tableColumnPresets.status,
      title: '状态',
      dataIndex: 'status',
      render: (s: string) => <StatusTag value={s} />,
    },
    { ...tableColumnPresets.datetime, title: '部署时间', dataIndex: 'deployedAt', render: (value: string, record: Release) => formatDateTime(value || record.createdAt) },
    {
      ...tableColumnPresets.action,
      title: '操作',
      dataIndex: 'id',
      render: (_: unknown, record: Release) => (
        canTriggerRelease ? (
          <ManagementIconButton
            aria-label="部署"
            icon={<PlayCircleOutlined />}
            size="small"
            tooltip="部署"
            onClick={() => triggerMutation.mutate(record)}
          />
        ) : '-'
      ),
    },
  ]

  return (
    <div className="soha-page">
      <DeliveryTable
        refreshing={isFetching}
        onRefresh={() => void refetch()}
        columns={columns}
        dataSource={data?.data ?? []}
        rowKey="id"
        loading={isLoading}
      />
    </div>
  )
}

/* ─── Registries ─── */

interface Registry {
  id: string
  name: string
  type: string
  endpoint: string
  username: string
  status: string
}

export function RegistriesPage() {
  const { message } = App.useApp()
  const queryClient = useQueryClient()
  const permissionSnapshotQuery = usePermissionSnapshot()
  const [form] = Form.useForm<Record<string, string>>()
  const [modalVisible, setModalVisible] = useState(false)
  const [editing, setEditing] = useState<Registry | null>(null)
  const canManageRegistry = hasPermission(permissionSnapshotQuery.data?.data, 'delivery.registries.manage')

  const { data, isLoading, isFetching, refetch } = useQuery({
    queryKey: ['registries'],
    queryFn: () => api.get<ApiResponse<Registry[]>>('/registries'),
  })

  const createMutation = useMutation({
    mutationFn: (values: Record<string, string>) => api.post('/registries', values),
    onSuccess: () => {
      message.success('仓库创建成功')
      queryClient.invalidateQueries({ queryKey: ['registries'] })
      setModalVisible(false)
    },
    onError: (err: Error) => message.error(err.message),
  })

  const updateMutation = useMutation({
    mutationFn: ({ id, values }: { id: string; values: Record<string, string> }) =>
      api.put(`/registries/${id}`, values),
    onSuccess: () => {
      message.success('仓库更新成功')
      queryClient.invalidateQueries({ queryKey: ['registries'] })
      setModalVisible(false)
      setEditing(null)
    },
    onError: (err: Error) => message.error(err.message),
  })

  const deleteMutation = useMutation({
    mutationFn: (id: string) => api.delete(`/registries/${id}`),
    onSuccess: () => {
      message.success('仓库已删除')
      queryClient.invalidateQueries({ queryKey: ['registries'] })
    },
    onError: (err: Error) => message.error(err.message),
  })

  const handleSubmit = (values: Record<string, string>) => {
    if (editing) {
      updateMutation.mutate({ id: editing.id, values })
    } else {
      createMutation.mutate(values)
    }
  }

  const columns: ColumnProps<Registry>[] = [
    { title: '名称', dataIndex: 'name' },
    { title: '类型', dataIndex: 'type', render: (t: string) => <Tag>{t}</Tag> },
    { title: 'Endpoint', dataIndex: 'endpoint', ellipsis: true },
    { title: '用户名', dataIndex: 'username' },
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
      render: (_: unknown, record: Registry) => (
        <Space className="soha-row-action-icons" size={2}>
          {canManageRegistry ? (
            <ManagementIconButton
              aria-label="编辑仓库"
              icon={<EditOutlined />}
              size="small"
              tooltip="编辑"
              onClick={() => { setEditing(record); setModalVisible(true) }}
            />
          ) : null}
          {canManageRegistry ? (
            <Popconfirm title="确认删除？" onConfirm={() => deleteMutation.mutate(record.id)} placement="topRight">
              <ManagementIconButton
                aria-label="删除仓库"
                danger
                icon={<DeleteOutlined />}
                size="small"
                tooltip="删除"
              />
            </Popconfirm>
          ) : null}
          {!canManageRegistry ? '-' : null}
        </Space>
      ),
    },
  ]

  return (
    <div className="soha-page">
      <DeliveryTable
        actions={canManageRegistry ? (
          <Button icon={<PlusOutlined />} type="primary" onClick={() => { setEditing(null); setModalVisible(true) }}>
            添加仓库
          </Button>
        ) : null}
        refreshing={isFetching}
        onRefresh={() => void refetch()}
        columns={columns}
        dataSource={data?.data ?? []}
        rowKey="id"
        loading={isLoading}
      />
      <Modal
        title={editing ? '编辑仓库' : '添加仓库'}
        open={modalVisible}
        onCancel={() => { setModalVisible(false); setEditing(null) }}
        footer={null}
        destroyOnHidden
      >
        <Form
          form={form}
          key={editing?.id ?? 'create-registry'}
          layout="vertical"
          onFinish={handleSubmit}
          initialValues={editing ? { name: editing.name, type: editing.type, endpoint: editing.endpoint, username: editing.username } : {}}
        >
          <Form.Item name="name" label="名称" rules={[{ required: true, message: '请输入名称' }]}>
            <Input />
          </Form.Item>
          <Form.Item name="type" label="类型" rules={[{ required: true, message: '请选择类型' }]}>
            <Select options={[
              { value: 'docker', label: 'Docker Hub' },
              { value: 'harbor', label: 'Harbor' },
              { value: 'acr', label: 'ACR' },
              { value: 'ecr', label: 'ECR' },
              { value: 'gcr', label: 'GCR' },
            ]} />
          </Form.Item>
          <Form.Item name="endpoint" label="Endpoint" rules={[{ required: true, message: '请输入地址' }]}>
            <Input />
          </Form.Item>
          <Form.Item name="username" label="用户名">
            <Input />
          </Form.Item>
          <Form.Item name="password" label="密码">
            <Input.Password />
          </Form.Item>
          <div className="soha-form-actions">
            <Button onClick={() => setModalVisible(false)}>取消</Button>
            <Button htmlType="submit" type="primary" loading={createMutation.isPending || updateMutation.isPending}>
              {editing ? '更新' : '创建'}
            </Button>
          </div>
        </Form>
      </Modal>
    </div>
  )
}
