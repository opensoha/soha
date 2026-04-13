import { useState } from 'react'
import { Button, Modal, Form, Tag, Toast, Popconfirm, Space, Card, Typography } from '@douyinfe/semi-ui'
import { IconPlus, IconEdit, IconDelete, IconPlay } from '@douyinfe/semi-icons'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { AdminTable } from '@/components/admin-table'
import { useI18n } from '@/i18n'
import { PageHeader } from '@/components/page-header'
import { StatusTag } from '@/components/status-tag'
import { api } from '@/services/api-client'
import { formatDateTime } from '@/utils/time'
import { tableColumnPresets } from '@/utils/table-columns'
import type { ApiResponse, BusinessLine, WorkflowNodeRun } from '@/types'
import type { ColumnProps } from '@douyinfe/semi-ui/lib/es/table'

const { Text } = Typography

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
  const { t } = useI18n()
  const queryClient = useQueryClient()
  const [modalVisible, setModalVisible] = useState(false)
  const [editing, setEditing] = useState<Application | null>(null)

  const { data, isLoading } = useQuery({
    queryKey: ['applications'],
    queryFn: () => api.get<ApiResponse<Application[]>>('/applications'),
  })

  const businessLinesQuery = useQuery({
    queryKey: ['business-lines'],
    queryFn: () => api.get<ApiResponse<BusinessLine[]>>('/business-lines'),
  })

  const businessLineMap = Object.fromEntries((businessLinesQuery.data?.data ?? []).map((item) => [item.id, item.name]))

  const createMutation = useMutation({
    mutationFn: (values: Record<string, unknown>) => api.post('/applications', values),
    onSuccess: () => {
      Toast.success('应用创建成功')
      queryClient.invalidateQueries({ queryKey: ['applications'] })
      setModalVisible(false)
    },
    onError: (err: Error) => Toast.error(err.message),
  })

  const updateMutation = useMutation({
    mutationFn: ({ id, values }: { id: string; values: Record<string, unknown> }) =>
      api.put(`/applications/${id}`, values),
    onSuccess: () => {
      Toast.success('应用更新成功')
      queryClient.invalidateQueries({ queryKey: ['applications'] })
      setModalVisible(false)
      setEditing(null)
    },
    onError: (err: Error) => Toast.error(err.message),
  })

  const deleteMutation = useMutation({
    mutationFn: (id: string) => api.delete(`/applications/${id}`),
    onSuccess: () => {
      Toast.success('应用已删除')
      queryClient.invalidateQueries({ queryKey: ['applications'] })
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

  const columns: ColumnProps<Application>[] = [
    { title: '名称', dataIndex: 'name' },
    { title: 'Key', dataIndex: 'key' },
    { title: '分组', dataIndex: 'group' },
    { title: '业务线', dataIndex: 'businessLineId', render: (value: string) => businessLineMap[value] || value || '-' },
    { title: '语言', dataIndex: 'language' },
    { title: '构建方式', dataIndex: 'dockerfilePath', render: () => <Tag color="cyan">Dockerfile</Tag> },
    { title: '仓库路径', dataIndex: 'repositoryPath', ellipsis: true },
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
        title={t('page.delivery.applications.title', 'Applications')}
        description={t('page.delivery.applications.desc', 'Manage application repositories, Dockerfile build parameters, and recent deployment state.')}
        actions={<Button icon={<IconPlus />} theme="solid" onClick={() => { setEditing(null); setModalVisible(true) }}>新建应用</Button>}
      />
      <Card className="kc-scope-hint-card">
        <Text type="tertiary">
          当前构建链路先只支持 `Dockerfile`。这里配置的是镜像仓库、默认 Tag、构建上下文目录和 Dockerfile 路径，后续发布流程会直接复用这些参数。
        </Text>
      </Card>
      <AdminTable columns={columns} dataSource={data?.data ?? []} rowKey="id" loading={isLoading} />
      <Modal
        title={editing ? '编辑应用' : '新建应用'}
        visible={modalVisible}
        onCancel={() => { setModalVisible(false); setEditing(null) }}
        footer={null}
      >
        <Form
          onSubmit={handleSubmit}
          initValues={
            editing
              ? {
                  name: editing.name,
                  key: editing.key,
                  group: editing.group,
                  businessLineId: editing.businessLineId,
                  language: editing.language,
                  repositoryPath: editing.repositoryPath,
                  defaultBranch: editing.defaultBranch,
                  defaultTag: editing.defaultTag,
                  buildImage: editing.buildImage,
                  buildContextDir: editing.buildContextDir,
                  dockerfilePath: editing.dockerfilePath,
                  enabled: editing.enabled,
                }
              : { enabled: true, language: 'node', defaultBranch: 'main' }
          }
        >
          <Form.Input field="name" label="应用名称" rules={[{ required: true, message: '请输入应用名称' }]} />
          <Form.Input field="key" label="应用 Key" rules={[{ required: true, message: '请输入应用 Key' }]} />
          <Form.Input field="group" label="应用分组" rules={[{ required: true, message: '请输入应用分组' }]} />
          <Form.Select
            field="businessLineId"
            label="业务线"
            optionList={(businessLinesQuery.data?.data ?? []).map((item) => ({ value: item.id, label: item.name }))}
            rules={[{ required: true, message: '请选择业务线' }]}
          />
          <Form.Select
            field="language"
            label="语言"
            optionList={[
              { value: 'node', label: 'Node.js' },
              { value: 'java', label: 'Java' },
              { value: 'go', label: 'Go' },
              { value: 'python', label: 'Python' },
            ]}
          />
          <Form.Input field="repositoryPath" label="代码仓库路径" />
          <Form.Input field="defaultBranch" label="默认分支" />
          <Form.Input field="defaultTag" label="默认镜像 Tag" />
          <Form.Input field="buildImage" label="镜像仓库地址" />
          <Form.Input field="buildContextDir" label="构建上下文目录" placeholder="." />
          <Form.Input field="dockerfilePath" label="Dockerfile 路径" placeholder="./Dockerfile" />
          <Form.Switch field="enabled" label="启用" />
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
  const queryClient = useQueryClient()

  const { data, isLoading } = useQuery({
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
      Toast.success(localeCode === 'zh_CN' ? '工作流已触发' : 'Workflow triggered')
      queryClient.invalidateQueries({ queryKey: ['workflows'] })
    },
    onError: (err: Error) => Toast.error(err.message),
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
        <Button icon={<IconPlay />} size="small" theme="borderless" onClick={() => triggerMutation.mutate(record)}>
          {localeCode === 'zh_CN' ? '触发' : 'Trigger'}
        </Button>
      ),
    },
  ]

  return (
    <div className="kc-page">
      <PageHeader title={t('page.delivery.workflows.title', 'Workflows')} description={t('page.delivery.workflows.desc', 'Inspect automation flow records, trigger methods, and recent execution state.')} />
      <AdminTable columns={columns} dataSource={data?.data ?? []} rowKey="id" loading={isLoading} />
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
  const { t } = useI18n()
  const queryClient = useQueryClient()

  const { data, isLoading } = useQuery({
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
      Toast.success('发布已触发')
      queryClient.invalidateQueries({ queryKey: ['releases'] })
    },
    onError: (err: Error) => Toast.error(err.message),
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
        <Button icon={<IconPlay />} size="small" theme="borderless" onClick={() => triggerMutation.mutate(record)}>
          部署
        </Button>
      ),
    },
  ]

  return (
    <div className="kc-page">
      <PageHeader title={t('page.delivery.releases.title', 'Releases')} description={t('page.delivery.releases.desc', 'Inspect environment release versions and trigger deployments when needed.')} />
      <AdminTable columns={columns} dataSource={data?.data ?? []} rowKey="id" loading={isLoading} />
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
  const { t } = useI18n()
  const queryClient = useQueryClient()
  const [modalVisible, setModalVisible] = useState(false)
  const [editing, setEditing] = useState<Registry | null>(null)

  const { data, isLoading } = useQuery({
    queryKey: ['registries'],
    queryFn: () => api.get<ApiResponse<Registry[]>>('/registries'),
  })

  const createMutation = useMutation({
    mutationFn: (values: Record<string, string>) => api.post('/registries', values),
    onSuccess: () => {
      Toast.success('仓库创建成功')
      queryClient.invalidateQueries({ queryKey: ['registries'] })
      setModalVisible(false)
    },
    onError: (err: Error) => Toast.error(err.message),
  })

  const updateMutation = useMutation({
    mutationFn: ({ id, values }: { id: string; values: Record<string, string> }) =>
      api.put(`/registries/${id}`, values),
    onSuccess: () => {
      Toast.success('仓库更新成功')
      queryClient.invalidateQueries({ queryKey: ['registries'] })
      setModalVisible(false)
      setEditing(null)
    },
    onError: (err: Error) => Toast.error(err.message),
  })

  const deleteMutation = useMutation({
    mutationFn: (id: string) => api.delete(`/registries/${id}`),
    onSuccess: () => {
      Toast.success('仓库已删除')
      queryClient.invalidateQueries({ queryKey: ['registries'] })
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
        title={t('page.delivery.registries.title', 'Registries')}
        description={t('page.delivery.registries.desc', 'Manage registry connections, credentials, and connectivity status.')}
        actions={<Button icon={<IconPlus />} theme="solid" onClick={() => { setEditing(null); setModalVisible(true) }}>添加仓库</Button>}
      />
      <AdminTable columns={columns} dataSource={data?.data ?? []} rowKey="id" loading={isLoading} />
      <Modal
        title={editing ? '编辑仓库' : '添加仓库'}
        visible={modalVisible}
        onCancel={() => { setModalVisible(false); setEditing(null) }}
        footer={null}
      >
        <Form onSubmit={handleSubmit} initValues={editing ? { name: editing.name, type: editing.type, endpoint: editing.endpoint, username: editing.username } : {}}>
          <Form.Input field="name" label="名称" rules={[{ required: true, message: '请输入名称' }]} />
          <Form.Select field="type" label="类型" optionList={[
            { value: 'docker', label: 'Docker Hub' },
            { value: 'harbor', label: 'Harbor' },
            { value: 'acr', label: 'ACR' },
            { value: 'ecr', label: 'ECR' },
            { value: 'gcr', label: 'GCR' },
          ]} rules={[{ required: true, message: '请选择类型' }]} />
          <Form.Input field="endpoint" label="Endpoint" rules={[{ required: true, message: '请输入地址' }]} />
          <Form.Input field="username" label="用户名" />
          <Form.Input field="password" label="密码" mode="password" />
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
