import { useMemo, useState } from 'react'
import { App, Button, Card, Descriptions, Form, Input, Modal, Popconfirm, Select, Space, Switch, Tag, Typography } from 'antd'
import { DeleteOutlined, EditOutlined, EyeOutlined, PlayCircleOutlined, PlusOutlined } from '@ant-design/icons'
import type { TableColumnsType } from 'antd'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useNavigate, useParams } from 'react-router-dom'
import { AdminTable } from '@/components/admin-table'
import { PageHeader } from '@/components/page-header'
import { BooleanTag, StatusTag } from '@/components/status-tag'
import { hasPermission, usePermissionSnapshot } from '@/features/auth/permission-snapshot'
import { useI18n } from '@/i18n'
import { api } from '@/services/api-client'
import { formatDateTime } from '@/utils/time'
import { tableColumnPresets } from '@/utils/table-columns'
import type {
  ApprovalPolicy,
  ApiResponse,
  BuildSource,
  BuildTemplate,
  BusinessLine,
  DeliveryApplication,
  DeliveryApplicationDetail,
  ExecutionArtifact,
  ExecutionTask,
  ReleaseBoardEntry,
  ReleaseBundle,
  WorkflowRun,
} from '@/types'

const { Text } = Typography
type ColumnProps<T> = TableColumnsType<T>[number]
type ApplicationBindingRow = NonNullable<DeliveryApplicationDetail['bindings']>[number]

function parseJSONObject(raw: unknown, field: string) {
  const value = typeof raw === 'string' ? raw.trim() : ''
  if (!value) return {}
  try {
    const parsed = JSON.parse(value)
    if (!parsed || typeof parsed !== 'object' || Array.isArray(parsed)) {
      throw new Error('invalid')
    }
    return parsed
  } catch {
    throw new Error(`${field} 需要是合法 JSON 对象`)
  }
}

function createBuildSource(type: BuildSource['type'], isDefault = false): BuildSource {
  switch (type) {
    case 'platform_build_template':
      return {
        id: '',
        name: 'Platform Template',
        type,
        enabled: true,
        isDefault,
        buildImage: '',
        defaultTag: '',
        config: { buildTemplateId: '', contextDir: '.' },
      }
    case 'external_pipeline':
      return {
        id: '',
        name: 'External Pipeline',
        type,
        enabled: true,
        isDefault,
        buildImage: '',
        defaultTag: '',
        config: { provider: '', pipelineKey: '' },
      }
    default:
      return {
        id: '',
        name: 'Repository Dockerfile',
        type: 'repo_dockerfile',
        enabled: true,
        isDefault,
        buildImage: '',
        defaultTag: '',
        config: { contextDir: '.', dockerfilePath: 'Dockerfile', builderKind: 'docker' },
      }
  }
}

function summarizeBuildSource(source?: BuildSource) {
  if (!source) return '-'
  switch (source.type) {
    case 'repo_dockerfile':
      return 'Repo Dockerfile'
    case 'platform_build_template':
      return 'Platform Template'
    case 'external_pipeline':
      return 'External Pipeline'
    default:
      return source.type
  }
}

function buildStatus(entry?: ReleaseBoardEntry) {
  if (entry?.latestRelease?.status) return entry.latestRelease.status
  if (entry?.latestWorkflow?.status) return entry.latestWorkflow.status
  if (entry?.latestBuild?.status) return entry.latestBuild.status
  return 'unknown'
}

function canCancelExecutionTask(task?: ExecutionTask | null) {
  return task ? ['queued', 'dispatching', 'running'].includes(task.status) : false
}

function canRetryExecutionTask(task?: ExecutionTask | null) {
  return task ? ['failed', 'callback_timeout', 'canceled'].includes(task.status) : false
}

export function ApplicationsPage() {
  const { t } = useI18n()
  const { message } = App.useApp()
  const navigate = useNavigate()
  const queryClient = useQueryClient()
  const permissionSnapshotQuery = usePermissionSnapshot()
  const permissionSnapshot = permissionSnapshotQuery.data?.data
  const [form] = Form.useForm<Record<string, unknown>>()
  const [modalVisible, setModalVisible] = useState(false)
  const [editing, setEditing] = useState<DeliveryApplication | null>(null)
  const [buildSources, setBuildSources] = useState<BuildSource[]>([])

  const applicationsQuery = useQuery({
    queryKey: ['applications'],
    queryFn: () => api.get<ApiResponse<DeliveryApplication[]>>('/applications'),
  })
  const businessLinesQuery = useQuery({
    queryKey: ['business-lines'],
    queryFn: () => api.get<ApiResponse<BusinessLine[]>>('/business-lines'),
  })
  const releaseBoardQuery = useQuery({
    queryKey: ['delivery-release-board'],
    queryFn: () => api.get<ApiResponse<ReleaseBoardEntry[]>>('/delivery/release-board'),
  })
  const buildTemplatesQuery = useQuery({
    queryKey: ['build-templates'],
    queryFn: () => api.get<ApiResponse<BuildTemplate[]>>('/build-templates'),
  })

  const businessLineMap = Object.fromEntries((businessLinesQuery.data?.data ?? []).map((item) => [item.id, item.name]))
  const boardByApp = useMemo(() => {
    const items = releaseBoardQuery.data?.data ?? []
    return items.reduce<Record<string, ReleaseBoardEntry[]>>((acc, item) => {
      acc[item.applicationId] = [...(acc[item.applicationId] ?? []), item]
      return acc
    }, {})
  }, [releaseBoardQuery.data])

  const canCreateApplication = hasPermission(permissionSnapshot, 'delivery.application.create')
  const canUpdateApplication = hasPermission(permissionSnapshot, 'delivery.application.update')
  const canDeleteApplication = hasPermission(permissionSnapshot, 'delivery.application.delete')

  const createMutation = useMutation({
    mutationFn: (values: Record<string, unknown>) => api.post('/applications', values),
    onSuccess: () => {
      message.success('应用创建成功')
      queryClient.invalidateQueries({ queryKey: ['applications'] })
      queryClient.invalidateQueries({ queryKey: ['delivery-release-board'] })
      setModalVisible(false)
    },
    onError: (err: Error) => message.error(err.message),
  })
  const updateMutation = useMutation({
    mutationFn: ({ id, values }: { id: string; values: Record<string, unknown> }) => api.put(`/applications/${id}`, values),
    onSuccess: () => {
      message.success('应用更新成功')
      queryClient.invalidateQueries({ queryKey: ['applications'] })
      queryClient.invalidateQueries({ queryKey: ['delivery-release-board'] })
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
      queryClient.invalidateQueries({ queryKey: ['delivery-release-board'] })
    },
    onError: (err: Error) => message.error(err.message),
  })

  const columns: ColumnProps<DeliveryApplication>[] = [
    { title: '名称', dataIndex: 'name' },
    { title: 'Key', dataIndex: 'key' },
    { title: '分组', dataIndex: 'group' },
    { title: '业务线', dataIndex: 'businessLineId', render: (value: string) => businessLineMap[value] || value || '-' },
    { title: '默认构建来源', dataIndex: 'buildSources', render: (value: BuildSource[]) => summarizeBuildSource((value ?? []).find((item) => item.isDefault)) },
    { title: '环境覆盖', dataIndex: 'id', render: (value: string) => boardByApp[value]?.length ?? 0 },
    { title: '最近状态', dataIndex: 'id', render: (value: string) => <StatusTag value={buildStatus(boardByApp[value]?.[0])} /> },
    { title: '仓库路径', dataIndex: 'repositoryPath', ellipsis: true },
    { ...tableColumnPresets.datetime, title: '更新时间', dataIndex: 'updatedAt', render: (value: string) => formatDateTime(value) },
    {
      ...tableColumnPresets.action,
      title: '操作',
      dataIndex: 'id',
      render: (_: unknown, record: DeliveryApplication) => (
        <Space>
          <Button icon={<EyeOutlined />} size="small" type="text" onClick={() => navigate(`/applications/${record.id}`)} />
          {canUpdateApplication ? <Button icon={<EditOutlined />} size="small" type="text" onClick={() => { setEditing(record); setBuildSources(record.buildSources ?? []); setModalVisible(true) }} /> : null}
          {canDeleteApplication ? <Popconfirm title="确认删除？" onConfirm={() => deleteMutation.mutate(record.id)}><Button icon={<DeleteOutlined />} size="small" type="text" danger /></Popconfirm> : null}
        </Space>
      ),
    },
  ]

  return (
    <div className="kc-page">
      <PageHeader
        title={t('page.delivery.applications.title', 'Applications')}
        description="管理应用仓库、多构建来源与最近交付状态。"
        actions={canCreateApplication ? <Button icon={<PlusOutlined />} type="primary" onClick={() => { setEditing(null); setBuildSources([createBuildSource('repo_dockerfile', true)]); setModalVisible(true) }}>新建应用</Button> : null}
      />
      <Card className="kc-scope-hint-card">
        <Text type="secondary">应用中心现在显示默认构建来源、环境覆盖矩阵和最近执行状态；环境级工作流与发布动作通过应用环境绑定接入。</Text>
      </Card>
      <AdminTable columns={columns} dataSource={applicationsQuery.data?.data ?? []} rowKey="id" loading={applicationsQuery.isLoading || releaseBoardQuery.isLoading} />
      <Modal title={editing ? '编辑应用' : '新建应用'} open={modalVisible} onCancel={() => { setModalVisible(false); setEditing(null) }} footer={null} destroyOnClose width={860}>
        <Form
          form={form}
          key={editing?.id ?? 'create-application'}
          layout="vertical"
          initialValues={editing ? { ...editing, enabled: editing.enabled } : { enabled: true, language: 'go', defaultBranch: 'main' }}
          onFinish={(values) => {
            const payload = { ...values, buildSources }
            if (editing) {
              updateMutation.mutate({ id: editing.id, values: payload })
            } else {
              createMutation.mutate(payload)
            }
          }}
        >
          <Form.Item name="name" label="应用名称" rules={[{ required: true, message: '请输入应用名称' }]}><Input /></Form.Item>
          <Form.Item name="key" label="应用 Key" rules={[{ required: true, message: '请输入应用 Key' }]}><Input /></Form.Item>
          <Form.Item name="group" label="应用分组" rules={[{ required: true, message: '请输入应用分组' }]}><Input /></Form.Item>
          <Form.Item name="businessLineId" label="业务线" rules={[{ required: true, message: '请选择业务线' }]}><Select options={(businessLinesQuery.data?.data ?? []).map((item) => ({ value: item.id, label: item.name }))} /></Form.Item>
          <Form.Item name="language" label="语言"><Select options={[{ value: 'go', label: 'Go' }, { value: 'java', label: 'Java' }, { value: 'node', label: 'Node.js' }, { value: 'python', label: 'Python' }]} /></Form.Item>
          <Form.Item name="repositoryPath" label="代码仓库路径"><Input /></Form.Item>
          <Form.Item name="defaultBranch" label="默认分支"><Input /></Form.Item>
          <Form.Item name="enabled" label="启用" valuePropName="checked"><Switch /></Form.Item>
          <Card title="构建来源" size="small">
            <Space direction="vertical" style={{ width: '100%' }}>
              {buildSources.map((item, index) => (
                <Card
                  key={`${item.id || 'new'}-${index}`}
                  size="small"
                  extra={<Button type="text" danger size="small" onClick={() => setBuildSources((current) => current.filter((_, currentIndex) => currentIndex !== index))}>删除</Button>}
                >
                  <Space direction="vertical" style={{ width: '100%' }}>
                    <Input value={item.name} placeholder="来源名称" onChange={(event) => setBuildSources((current) => current.map((source, currentIndex) => currentIndex === index ? { ...source, name: event.target.value } : source))} />
                    <Select
                      value={item.type}
                      options={[
                        { value: 'repo_dockerfile', label: 'Repo Dockerfile' },
                        { value: 'platform_build_template', label: 'Platform Template' },
                        { value: 'external_pipeline', label: 'External Pipeline' },
                      ]}
                      onChange={(value) => setBuildSources((current) => current.map((source, currentIndex) => currentIndex === index ? { ...createBuildSource(value as BuildSource['type'], source.isDefault), id: source.id, enabled: source.enabled } : source))}
                    />
                    <Input value={item.buildImage} placeholder="镜像地址" onChange={(event) => setBuildSources((current) => current.map((source, currentIndex) => currentIndex === index ? { ...source, buildImage: event.target.value } : source))} />
                    <Input value={item.defaultTag} placeholder="默认 Tag" onChange={(event) => setBuildSources((current) => current.map((source, currentIndex) => currentIndex === index ? { ...source, defaultTag: event.target.value } : source))} />
                    <Input value={String(item.config?.workspacePath ?? '')} placeholder="Agent Workspace Path (可选)" onChange={(event) => setBuildSources((current) => current.map((source, currentIndex) => currentIndex === index ? { ...source, config: { ...source.config, workspacePath: event.target.value } } : source))} />
                    <Input value={String(item.config?.repositoryURL ?? item.config?.repositoryUrl ?? '')} placeholder="Repository URL (可选)" onChange={(event) => setBuildSources((current) => current.map((source, currentIndex) => currentIndex === index ? { ...source, config: { ...source.config, repositoryURL: event.target.value } } : source))} />
                    {item.type === 'repo_dockerfile' ? (
                      <>
                        <Input value={String(item.config?.contextDir ?? '.')} placeholder="构建上下文目录" onChange={(event) => setBuildSources((current) => current.map((source, currentIndex) => currentIndex === index ? { ...source, config: { ...source.config, contextDir: event.target.value } } : source))} />
                        <Input value={String(item.config?.dockerfilePath ?? 'Dockerfile')} placeholder="Dockerfile 路径" onChange={(event) => setBuildSources((current) => current.map((source, currentIndex) => currentIndex === index ? { ...source, config: { ...source.config, dockerfilePath: event.target.value } } : source))} />
                        <Select
                          value={String(item.config?.builderKind ?? 'docker')}
                          options={[{ value: 'docker', label: 'docker' }, { value: 'buildx', label: 'buildx' }, { value: 'kaniko', label: 'kaniko' }]}
                          onChange={(value) => setBuildSources((current) => current.map((source, currentIndex) => currentIndex === index ? { ...source, config: { ...source.config, builderKind: value } } : source))}
                        />
                      </>
                    ) : null}
                    {item.type === 'platform_build_template' ? (
                      <>
                        <Select
                          value={String(item.config?.buildTemplateId ?? '') || undefined}
                          placeholder="选择构建模板"
                          options={(buildTemplatesQuery.data?.data ?? []).map((template) => ({ value: template.id, label: template.name }))}
                          onChange={(value) => setBuildSources((current) => current.map((source, currentIndex) => currentIndex === index ? { ...source, config: { ...source.config, buildTemplateId: value } } : source))}
                        />
                        <Input value={String(item.config?.contextDir ?? '.')} placeholder="构建上下文目录" onChange={(event) => setBuildSources((current) => current.map((source, currentIndex) => currentIndex === index ? { ...source, config: { ...source.config, contextDir: event.target.value } } : source))} />
                        <Input.TextArea rows={4} value={JSON.stringify(item.config?.variables ?? {}, null, 2)} placeholder="模板变量(JSON)" onChange={(event) => {
                          try {
                            const next = parseJSONObject(event.target.value, '模板变量')
                            setBuildSources((current) => current.map((source, currentIndex) => currentIndex === index ? { ...source, config: { ...source.config, variables: next } } : source))
                          } catch {
                            return
                          }
                        }} />
                      </>
                    ) : null}
                    {item.type === 'external_pipeline' ? (
                      <>
                        <Input value={String(item.config?.provider ?? '')} placeholder="Pipeline Provider" onChange={(event) => setBuildSources((current) => current.map((source, currentIndex) => currentIndex === index ? { ...source, config: { ...source.config, provider: event.target.value } } : source))} />
                        <Input value={String(item.config?.pipelineKey ?? '')} placeholder="Pipeline Key" onChange={(event) => setBuildSources((current) => current.map((source, currentIndex) => currentIndex === index ? { ...source, config: { ...source.config, pipelineKey: event.target.value } } : source))} />
                        <Input.TextArea rows={4} value={JSON.stringify(item.config?.triggerConfig ?? {}, null, 2)} placeholder="触发参数(JSON)" onChange={(event) => {
                          try {
                            const next = parseJSONObject(event.target.value, '触发参数')
                            setBuildSources((current) => current.map((source, currentIndex) => currentIndex === index ? { ...source, config: { ...source.config, triggerConfig: next } } : source))
                          } catch {
                            return
                          }
                        }} />
                      </>
                    ) : null}
                    <Space>
                      <Button size="small" type={item.isDefault ? 'primary' : 'default'} onClick={() => setBuildSources((current) => current.map((source, currentIndex) => ({ ...source, isDefault: currentIndex === index })))}>设为默认</Button>
                      <Button size="small" onClick={() => setBuildSources((current) => current.map((source, currentIndex) => currentIndex === index ? { ...source, enabled: !source.enabled } : source))}>{item.enabled ? '停用' : '启用'}</Button>
                      <Tag>{summarizeBuildSource(item)}</Tag>
                    </Space>
                  </Space>
                </Card>
              ))}
              <Button onClick={() => setBuildSources((current) => [...current, createBuildSource('repo_dockerfile', current.length === 0)])}>新增构建来源</Button>
            </Space>
          </Card>
          <div className="kc-form-actions">
            <Button onClick={() => setModalVisible(false)}>取消</Button>
            <Button htmlType="submit" type="primary" loading={createMutation.isPending || updateMutation.isPending}>保存</Button>
          </div>
        </Form>
      </Modal>
    </div>
  )
}

export function ApplicationDetailPage() {
  const { applicationId } = useParams()
  const navigate = useNavigate()
  const detailQuery = useQuery({
    queryKey: ['application-detail', applicationId],
    queryFn: () => api.get<ApiResponse<DeliveryApplicationDetail>>(`/applications/${applicationId}/detail`),
    enabled: !!applicationId,
  })
  const detail = detailQuery.data?.data
  const application = detail?.application

  return (
    <div className="kc-page">
      <PageHeader title={application?.name || 'Application Detail'} description="应用总览、构建来源、环境矩阵与最近执行记录。" actions={<Button onClick={() => navigate('/applications')}>返回应用中心</Button>} />
      <Card>
        <Descriptions items={[
          { key: 'group', label: '分组', children: application?.group || '-' },
          { key: 'language', label: '语言', children: application?.language || '-' },
          { key: 'repo', label: '仓库', children: application?.repositoryPath || '-' },
          { key: 'branch', label: '默认分支', children: application?.defaultBranch || '-' },
          { key: 'status', label: '最近状态', children: <StatusTag value={detail?.latestRelease?.status || detail?.latestWorkflow?.status || detail?.latestBuild?.status || 'unknown'} /> },
        ]} />
      </Card>
      <Card title="构建来源">
        <AdminTable
          rowKey="id"
          pagination={false}
          dataSource={application?.buildSources ?? []}
          loading={detailQuery.isLoading}
          columns={[
            { title: '名称', dataIndex: 'name' },
            { title: '类型', dataIndex: 'type', render: (value: string) => <Tag>{value}</Tag> },
            { title: '镜像', dataIndex: 'buildImage', render: (value: string) => value || '-' },
            { title: '默认', dataIndex: 'isDefault', render: (value: boolean) => <BooleanTag value={value} /> },
            { title: '启用', dataIndex: 'enabled', render: (value: boolean) => <BooleanTag value={value} /> },
          ]}
        />
      </Card>
      <Card title="环境矩阵">
        <AdminTable
          rowKey="applicationEnvironmentId"
          pagination={false}
          dataSource={detail?.bindings ?? []}
          loading={detailQuery.isLoading}
          columns={[
            { title: '环境', dataIndex: 'environmentName', render: (value: string, record: ApplicationBindingRow) => value || record.environmentKey || record.environmentId },
            { title: '动作', dataIndex: 'actionKind', render: (value: string) => value || 'deploy' },
            { title: '构建来源', dataIndex: 'buildSource', render: (value: BuildSource | undefined) => summarizeBuildSource(value) },
            { title: '目标数', dataIndex: 'targetCount' },
            { title: '审批', dataIndex: 'requiresApproval', render: (value: boolean) => <BooleanTag value={value} /> },
            { title: 'Bundle', dataIndex: 'latestBundle', render: (value: ApplicationBindingRow['latestBundle']) => <StatusTag value={value?.status || 'unknown'} /> },
            { title: 'Task', dataIndex: 'latestExecutionTask', render: (value: ApplicationBindingRow['latestExecutionTask']) => <StatusTag value={value?.status || 'unknown'} /> },
            { title: 'Workflow', dataIndex: 'latestWorkflow', render: (value: WorkflowRun | undefined) => <StatusTag value={value?.status || 'unknown'} /> },
            { title: 'Release', dataIndex: 'latestRelease', render: (value: ApplicationBindingRow['latestRelease']) => <StatusTag value={value?.status || 'unknown'} /> },
            { ...tableColumnPresets.action, title: '操作', dataIndex: 'applicationEnvironmentId', render: (_: unknown, record: ApplicationBindingRow) => <Button type="link" onClick={() => navigate(`/application-environments/${record.applicationEnvironmentId}`)}>查看绑定</Button> },
          ]}
        />
      </Card>
    </div>
  )
}

export function BuildTemplatesPage() {
  const { message } = App.useApp()
  const queryClient = useQueryClient()
  const permissionSnapshotQuery = usePermissionSnapshot()
  const canManage = hasPermission(permissionSnapshotQuery.data?.data, 'delivery.build-templates.manage')
  const [form] = Form.useForm<Record<string, unknown>>()
  const [modalVisible, setModalVisible] = useState(false)
  const [editing, setEditing] = useState<BuildTemplate | null>(null)

  const templatesQuery = useQuery({
    queryKey: ['build-templates'],
    queryFn: () => api.get<ApiResponse<BuildTemplate[]>>('/build-templates'),
  })

  const createMutation = useMutation({
    mutationFn: (values: Record<string, unknown>) => api.post('/build-templates', values),
    onSuccess: () => {
      message.success('构建模板创建成功')
      queryClient.invalidateQueries({ queryKey: ['build-templates'] })
      setModalVisible(false)
    },
    onError: (err: Error) => message.error(err.message),
  })
  const updateMutation = useMutation({
    mutationFn: ({ id, values }: { id: string; values: Record<string, unknown> }) => api.put(`/build-templates/${id}`, values),
    onSuccess: () => {
      message.success('构建模板更新成功')
      queryClient.invalidateQueries({ queryKey: ['build-templates'] })
      setModalVisible(false)
      setEditing(null)
    },
    onError: (err: Error) => message.error(err.message),
  })
  const deleteMutation = useMutation({
    mutationFn: (id: string) => api.delete(`/build-templates/${id}`),
    onSuccess: () => {
      message.success('构建模板已删除')
      queryClient.invalidateQueries({ queryKey: ['build-templates'] })
    },
    onError: (err: Error) => message.error(err.message),
  })

  return (
    <div className="kc-page">
      <PageHeader title="构建模板" description="维护平台级 Dockerfile 模板、构建命令和默认变量。" actions={canManage ? <Button icon={<PlusOutlined />} type="primary" onClick={() => { setEditing(null); setModalVisible(true) }}>新建模板</Button> : null} />
      <AdminTable
        rowKey="id"
        loading={templatesQuery.isLoading}
        dataSource={templatesQuery.data?.data ?? []}
        columns={[
          { title: '名称', dataIndex: 'name' },
          { title: 'Key', dataIndex: 'key' },
          { title: 'Builder', dataIndex: 'builderKind' },
          { title: '命令数', dataIndex: 'buildCommands', render: (value: string[]) => value?.length ?? 0 },
          { title: '启用', dataIndex: 'enabled', render: (value: boolean) => <BooleanTag value={value} /> },
          { ...tableColumnPresets.action, title: '操作', dataIndex: 'id', render: (_: unknown, record: BuildTemplate) => <Space>{canManage ? <Button icon={<EditOutlined />} size="small" type="text" onClick={() => { setEditing(record); setModalVisible(true) }} /> : null}{canManage ? <Popconfirm title="确认删除？" onConfirm={() => deleteMutation.mutate(record.id)}><Button icon={<DeleteOutlined />} size="small" type="text" danger /></Popconfirm> : null}</Space> },
        ]}
      />
      <Modal title={editing ? '编辑构建模板' : '新建构建模板'} open={modalVisible} onCancel={() => { setModalVisible(false); setEditing(null) }} footer={null} destroyOnClose width={960}>
        <Form
          form={form}
          key={editing?.id ?? 'build-template'}
          layout="vertical"
          initialValues={editing ? { ...editing, buildCommandsText: (editing.buildCommands ?? []).join('\n'), variableSchemaText: JSON.stringify(editing.variableSchema ?? {}, null, 2), defaultVariablesText: JSON.stringify(editing.defaultVariables ?? {}, null, 2) } : { enabled: true, builderKind: 'custom', variableSchemaText: '{}', defaultVariablesText: '{}' }}
          onFinish={(values) => {
            let payload: Record<string, unknown>
            try {
              payload = {
                ...values,
                buildCommands: String(values.buildCommandsText || '').split('\n').map((item) => item.trim()).filter(Boolean),
                variableSchema: parseJSONObject(values.variableSchemaText, '变量 Schema'),
                defaultVariables: parseJSONObject(values.defaultVariablesText, '默认变量'),
              }
            } catch (err) {
              message.error((err as Error).message)
              return
            }
            if (editing) {
              updateMutation.mutate({ id: editing.id, values: payload })
            } else {
              createMutation.mutate(payload)
            }
          }}
        >
          <Form.Item name="key" label="模板 Key" rules={[{ required: true, message: '请输入模板 Key' }]}><Input /></Form.Item>
          <Form.Item name="name" label="模板名称" rules={[{ required: true, message: '请输入模板名称' }]}><Input /></Form.Item>
          <Form.Item name="builderKind" label="Builder Kind"><Select options={[{ value: 'docker', label: 'docker' }, { value: 'buildx', label: 'buildx' }, { value: 'kaniko', label: 'kaniko' }, { value: 'custom', label: 'custom' }]} /></Form.Item>
          <Form.Item name="description" label="描述"><Input /></Form.Item>
          <Form.Item name="dockerfileTemplate" label="Dockerfile 模板"><Input.TextArea rows={8} /></Form.Item>
          <Form.Item name="buildCommandsText" label="构建命令"><Input.TextArea rows={8} placeholder="one command per line" /></Form.Item>
          <Form.Item name="variableSchemaText" label="变量 Schema(JSON)"><Input.TextArea rows={6} /></Form.Item>
          <Form.Item name="defaultVariablesText" label="默认变量(JSON)"><Input.TextArea rows={6} /></Form.Item>
          <Form.Item name="enabled" label="启用" valuePropName="checked"><Switch /></Form.Item>
          <div className="kc-form-actions">
            <Button onClick={() => setModalVisible(false)}>取消</Button>
            <Button htmlType="submit" type="primary" loading={createMutation.isPending || updateMutation.isPending}>保存</Button>
          </div>
        </Form>
      </Modal>
    </div>
  )
}

export function WorkflowsPage() {
  const { t, localeCode } = useI18n()
  const { message } = App.useApp()
  const queryClient = useQueryClient()
  const permissionSnapshotQuery = usePermissionSnapshot()
  const canTriggerWorkflow = hasPermission(permissionSnapshotQuery.data?.data, 'delivery.workflows.trigger')

  const workflowsQuery = useQuery({
    queryKey: ['workflows'],
    queryFn: () => api.get<ApiResponse<WorkflowRun[]>>('/workflows'),
  })

  const triggerMutation = useMutation({
    mutationFn: (record: WorkflowRun) => api.post('/workflows/trigger', {
      applicationId: record.applicationId,
      workflowName: record.workflowName,
      clusterId: record.clusterId,
      namespace: record.namespace,
      deploymentName: record.deploymentName,
      triggerBuild: true,
      triggerRelease: true,
    }),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ['workflows'] }),
    onError: (err: Error) => message.error(err.message),
  })
  const approveMutation = useMutation({
    mutationFn: (record: WorkflowRun) => api.post(`/workflows/${record.id}/approve`, { comment: 'Approved from console' }),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ['workflows'] }),
    onError: (err: Error) => message.error(err.message),
  })
  const rejectMutation = useMutation({
    mutationFn: (record: WorkflowRun) => api.post(`/workflows/${record.id}/reject`, { comment: 'Rejected from console' }),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ['workflows'] }),
    onError: (err: Error) => message.error(err.message),
  })

  const columns: ColumnProps<WorkflowRun>[] = [
    { title: t('common.workflow', 'Workflow'), dataIndex: 'workflowName' },
    { title: t('common.application', 'Application'), dataIndex: 'applicationId' },
    { title: t('common.cluster', 'Cluster'), dataIndex: 'clusterId' },
    { title: t('common.namespace', 'Namespace'), dataIndex: 'namespace' },
    { ...tableColumnPresets.status, title: t('common.status', 'Status'), dataIndex: 'status', render: (value: string) => <StatusTag value={value} /> },
    {
      title: t('page.delivery.workflows.nodeProgress', 'Node Progress'),
      dataIndex: 'nodeRuns',
      render: (value: WorkflowRun['nodeRuns']) => `${value?.filter((item) => item.status !== 'pending').length ?? 0}/${value?.length ?? 0}`,
    },
    { ...tableColumnPresets.datetime, title: localeCode === 'zh_CN' ? '最近运行' : 'Last Run', dataIndex: 'updatedAt', render: (value: string) => formatDateTime(value) },
    {
      ...tableColumnPresets.action,
      title: t('common.actions', 'Actions'),
      dataIndex: 'id',
      render: (_: unknown, record: WorkflowRun) => (
        <Space>
          {canTriggerWorkflow ? <Button icon={<PlayCircleOutlined />} size="small" type="text" onClick={() => triggerMutation.mutate(record)}>{localeCode === 'zh_CN' ? '触发' : 'Trigger'}</Button> : null}
          {canTriggerWorkflow && record.status === 'waiting_approval' ? <Button size="small" type="link" onClick={() => approveMutation.mutate(record)}>批准</Button> : null}
          {canTriggerWorkflow && record.status === 'waiting_approval' ? <Button size="small" type="link" danger onClick={() => rejectMutation.mutate(record)}>拒绝</Button> : null}
        </Space>
      ),
    },
  ]

  return (
    <div className="kc-page">
      <PageHeader title={t('page.delivery.workflows.title', 'Workflows')} description={t('page.delivery.workflows.desc', 'Inspect automation flow records, trigger methods, and recent execution state.')} />
      <AdminTable columns={columns} dataSource={workflowsQuery.data?.data ?? []} rowKey="id" loading={workflowsQuery.isLoading} />
    </div>
  )
}

export function ReleaseBundlesPage() {
  const bundlesQuery = useQuery({
    queryKey: ['release-bundles'],
    queryFn: () => api.get<ApiResponse<ReleaseBundle[]>>('/delivery/release-bundles'),
  })

  return (
    <div className="kc-page">
      <PageHeader title="版本包" description="查看不可变交付版本包、制品引用和当前状态。" />
      <AdminTable
        rowKey="id"
        loading={bundlesQuery.isLoading}
        dataSource={bundlesQuery.data?.data ?? []}
        columns={[
          { title: 'Version', dataIndex: 'version' },
          { title: 'Application', dataIndex: 'applicationId' },
          { title: 'Environment Binding', dataIndex: 'applicationEnvironmentId', render: (value: string) => value || '-' },
          { title: 'Source', dataIndex: 'sourceType' },
          { title: 'Artifact', dataIndex: 'artifactRef', render: (value: string) => value || '-' },
          { title: 'Digest', dataIndex: 'artifactDigest', render: (value: string) => value || '-' },
          { title: 'Status', dataIndex: 'status', render: (value: string) => <StatusTag value={value} /> },
          { ...tableColumnPresets.datetime, title: 'Updated', dataIndex: 'updatedAt', render: (value: string) => formatDateTime(value) },
        ]}
      />
    </div>
  )
}

export function ExecutionTasksPage() {
  const { message } = App.useApp()
  const permissionSnapshotQuery = usePermissionSnapshot()
  const canManage = hasPermission(permissionSnapshotQuery.data?.data, 'delivery.execution-tasks.manage')
  const [selectedTask, setSelectedTask] = useState<ExecutionTask | null>(null)
  const tasksQuery = useQuery({
    queryKey: ['execution-tasks'],
    queryFn: () => api.get<ApiResponse<ExecutionTask[]>>('/delivery/execution-tasks'),
    refetchInterval: 5000,
  })
  const logsQuery = useQuery({
    queryKey: ['execution-task-logs', selectedTask?.id],
    queryFn: () => api.get<ApiResponse<Array<{ id: string; logLevel: string; message: string; createdAt: string }>>>(`/delivery/execution-tasks/${selectedTask!.id}/logs`),
    enabled: !!selectedTask?.id,
    refetchInterval: selectedTask?.id ? 5000 : false,
  })
  const callbackMutation = useMutation({
    mutationFn: (task: ExecutionTask) => api.post('/delivery/execution-callbacks', {
      callbackToken: task.callbackToken,
      status: 'completed',
      payload: {
        logs: [`manual callback for ${task.id}`],
      },
    }),
    onSuccess: () => {
      message.success('回调已记录')
      tasksQuery.refetch()
      if (selectedTask?.id) {
        logsQuery.refetch()
      }
    },
    onError: (err: Error) => message.error(err.message),
  })
  const cancelMutation = useMutation({
    mutationFn: (task: ExecutionTask) => api.post(`/delivery/execution-tasks/${task.id}/cancel`, {
      reason: 'Canceled from execution tasks console',
    }),
    onSuccess: () => {
      message.success('任务已取消')
      tasksQuery.refetch()
      if (selectedTask?.id) {
        logsQuery.refetch()
      }
    },
    onError: (err: Error) => message.error(err.message),
  })
  const retryMutation = useMutation({
    mutationFn: (task: ExecutionTask) => api.post(`/delivery/execution-tasks/${task.id}/retry`, {
      reason: 'Retried from execution tasks console',
    }),
    onSuccess: () => {
      message.success('任务已重新入队')
      tasksQuery.refetch()
      if (selectedTask?.id) {
        logsQuery.refetch()
      }
    },
    onError: (err: Error) => message.error(err.message),
  })

  return (
    <div className="kc-page">
      <PageHeader title="执行任务" description="查看执行平面任务、provider 状态和任务日志。" />
      <AdminTable
        rowKey="id"
        loading={tasksQuery.isLoading}
        dataSource={tasksQuery.data?.data ?? []}
        columns={[
          { title: 'Task', dataIndex: 'taskKind' },
          { title: 'Provider', dataIndex: 'providerKind' },
          { title: 'Target', dataIndex: 'targetKind' },
          { title: 'Application', dataIndex: 'applicationId' },
          { title: 'Bundle', dataIndex: 'releaseBundleId', render: (value: string) => value || '-' },
          { title: 'Artifacts', dataIndex: 'artifacts', render: (value?: ExecutionArtifact[]) => value?.length ?? 0 },
          { title: 'Status', dataIndex: 'status', render: (value: string) => <StatusTag value={value} /> },
          { title: 'Retries', dataIndex: 'attemptCount', render: (value: number, record: ExecutionTask) => `${value}/${record.maxRetries}` },
          { title: 'Timeout(s)', dataIndex: 'timeoutSeconds' },
          { ...tableColumnPresets.datetime, title: 'Heartbeat', dataIndex: 'lastHeartbeatAt', render: (value?: string) => value ? formatDateTime(value) : '-' },
          { ...tableColumnPresets.datetime, title: 'Updated', dataIndex: 'updatedAt', render: (value: string) => formatDateTime(value) },
          {
            ...tableColumnPresets.action,
            title: '操作',
            dataIndex: 'id',
            render: (_: unknown, record: ExecutionTask) => (
              <Space>
                <Button type="link" size="small" onClick={() => setSelectedTask(record)}>日志</Button>
                {canManage && canCancelExecutionTask(record) ? (
                  <Popconfirm title="确认取消该任务？" onConfirm={() => cancelMutation.mutate(record)}>
                    <Button type="link" size="small" danger>取消</Button>
                  </Popconfirm>
                ) : null}
                {canManage && canRetryExecutionTask(record) ? (
                  <Button type="link" size="small" onClick={() => retryMutation.mutate(record)}>重试</Button>
                ) : null}
                {canManage && record.providerKind !== 'k8s_job_runner' && record.callbackToken ? <Button type="link" size="small" onClick={() => callbackMutation.mutate(record)}>模拟回调</Button> : null}
              </Space>
            ),
          },
        ]}
      />
      <Modal
        title={selectedTask ? `任务日志 · ${selectedTask.id}` : '任务日志'}
        open={!!selectedTask}
        onCancel={() => setSelectedTask(null)}
        footer={null}
        width={920}
        destroyOnClose
      >
        <Descriptions
          items={selectedTask ? [
            { key: 'provider', label: 'Provider', children: selectedTask.providerKind },
            { key: 'status', label: 'Status', children: <StatusTag value={selectedTask.status} /> },
            { key: 'bundle', label: 'Bundle', children: selectedTask.releaseBundleId || '-' },
            { key: 'heartbeat', label: 'Last Heartbeat', children: selectedTask.lastHeartbeatAt ? formatDateTime(selectedTask.lastHeartbeatAt) : '-' },
            { key: 'callback', label: 'Callback Token', children: selectedTask.callbackToken || '-' },
          ] : []}
        />
        {canManage && selectedTask ? (
          <Space style={{ marginBottom: 12 }}>
            {canCancelExecutionTask(selectedTask) ? <Button danger onClick={() => cancelMutation.mutate(selectedTask)} loading={cancelMutation.isPending}>取消任务</Button> : null}
            {canRetryExecutionTask(selectedTask) ? <Button onClick={() => retryMutation.mutate(selectedTask)} loading={retryMutation.isPending}>重新入队</Button> : null}
          </Space>
        ) : null}
        <Card size="small" title="Execution Logs">
          <pre className="kc-json-block">
            {logsQuery.data?.data?.map((item) => `[${item.createdAt}] ${item.logLevel.toUpperCase()} ${item.message}`).join('\n') || 'No logs'}
          </pre>
        </Card>
        <Card size="small" title="Artifacts">
          <pre className="kc-json-block">{JSON.stringify(selectedTask?.artifacts ?? [], null, 2)}</pre>
        </Card>
        <Card size="small" title="Result">
          <pre className="kc-json-block">{JSON.stringify(selectedTask?.result ?? {}, null, 2)}</pre>
        </Card>
      </Modal>
    </div>
  )
}

export function ApprovalPoliciesPage() {
  const { message } = App.useApp()
  const queryClient = useQueryClient()
  const permissionSnapshotQuery = usePermissionSnapshot()
  const canManage = hasPermission(permissionSnapshotQuery.data?.data, 'delivery.approval-policies.manage')
  const [form] = Form.useForm<Record<string, unknown>>()
  const [modalVisible, setModalVisible] = useState(false)
  const [editing, setEditing] = useState<ApprovalPolicy | null>(null)

  const policiesQuery = useQuery({
    queryKey: ['approval-policies'],
    queryFn: () => api.get<ApiResponse<ApprovalPolicy[]>>('/delivery/approval-policies'),
  })

  const createMutation = useMutation({
    mutationFn: (values: Record<string, unknown>) => api.post('/delivery/approval-policies', values),
    onSuccess: () => {
      message.success('审批策略创建成功')
      queryClient.invalidateQueries({ queryKey: ['approval-policies'] })
      setModalVisible(false)
    },
    onError: (err: Error) => message.error(err.message),
  })
  const updateMutation = useMutation({
    mutationFn: ({ id, values }: { id: string; values: Record<string, unknown> }) => api.put(`/delivery/approval-policies/${id}`, values),
    onSuccess: () => {
      message.success('审批策略更新成功')
      queryClient.invalidateQueries({ queryKey: ['approval-policies'] })
      setModalVisible(false)
      setEditing(null)
    },
    onError: (err: Error) => message.error(err.message),
  })
  const deleteMutation = useMutation({
    mutationFn: (id: string) => api.delete(`/delivery/approval-policies/${id}`),
    onSuccess: () => {
      message.success('审批策略已删除')
      queryClient.invalidateQueries({ queryKey: ['approval-policies'] })
    },
    onError: (err: Error) => message.error(err.message),
  })

  return (
    <div className="kc-page">
      <PageHeader title="审批策略" description="维护 delivery 审批策略、会签模式和 SLA。" actions={canManage ? <Button icon={<PlusOutlined />} type="primary" onClick={() => { setEditing(null); setModalVisible(true) }}>新建策略</Button> : null} />
      <AdminTable
        rowKey="id"
        loading={policiesQuery.isLoading}
        dataSource={policiesQuery.data?.data ?? []}
        columns={[
          { title: '名称', dataIndex: 'name' },
          { title: 'Key', dataIndex: 'key' },
          { title: '模式', dataIndex: 'mode', render: (value: string) => value || 'single' },
          { title: '审批数', dataIndex: 'requiredApprovals' },
          { title: 'SLA(min)', dataIndex: 'slaMinutes' },
          { title: '角色', dataIndex: 'approverRoles', render: (value: string[]) => value?.join(', ') || '-' },
          { title: '启用', dataIndex: 'enabled', render: (value: boolean) => <BooleanTag value={value} /> },
          { ...tableColumnPresets.action, title: '操作', dataIndex: 'id', render: (_: unknown, record: ApprovalPolicy) => <Space>{canManage ? <Button icon={<EditOutlined />} size="small" type="text" onClick={() => { setEditing(record); setModalVisible(true) }} /> : null}{canManage ? <Popconfirm title="确认删除？" onConfirm={() => deleteMutation.mutate(record.id)}><Button icon={<DeleteOutlined />} size="small" type="text" danger /></Popconfirm> : null}</Space> },
        ]}
      />
      <Modal title={editing ? '编辑审批策略' : '新建审批策略'} open={modalVisible} onCancel={() => { setModalVisible(false); setEditing(null) }} footer={null} destroyOnClose width={860}>
        <Form
          form={form}
          key={editing?.id ?? 'approval-policy'}
          layout="vertical"
          initialValues={editing ? { ...editing, approverRolesText: (editing.approverRoles ?? []).join(', '), changeWindowText: JSON.stringify(editing.changeWindow ?? {}, null, 2), metadataText: JSON.stringify(editing.metadata ?? {}, null, 2) } : { enabled: true, mode: 'single', requiredApprovals: 1, slaMinutes: 60, changeWindowText: '{}', metadataText: '{}' }}
          onFinish={(values) => {
            let payload: Record<string, unknown>
            try {
              payload = {
                ...values,
                approverRoles: String(values.approverRolesText || '').split(',').map((item) => item.trim()).filter(Boolean),
                changeWindow: parseJSONObject(values.changeWindowText, 'Change Window'),
                metadata: parseJSONObject(values.metadataText, 'Metadata'),
              }
            } catch (err) {
              message.error((err as Error).message)
              return
            }
            if (editing) {
              updateMutation.mutate({ id: editing.id, values: payload })
            } else {
              createMutation.mutate(payload)
            }
          }}
        >
          <Form.Item name="key" label="策略 Key" rules={[{ required: true, message: '请输入策略 Key' }]}><Input /></Form.Item>
          <Form.Item name="name" label="策略名称" rules={[{ required: true, message: '请输入策略名称' }]}><Input /></Form.Item>
          <Form.Item name="description" label="描述"><Input /></Form.Item>
          <Form.Item name="mode" label="模式"><Select options={[{ value: 'single', label: 'single' }, { value: 'multi', label: 'multi' }, { value: 'all', label: 'all' }]} /></Form.Item>
          <Form.Item name="requiredApprovals" label="需要审批数"><Input type="number" /></Form.Item>
          <Form.Item name="slaMinutes" label="SLA(分钟)"><Input type="number" /></Form.Item>
          <Form.Item name="approverRolesText" label="审批角色"><Input placeholder="release-manager, ops-lead" /></Form.Item>
          <Form.Item name="changeWindowText" label="变更窗口(JSON)"><Input.TextArea rows={5} /></Form.Item>
          <Form.Item name="metadataText" label="Metadata(JSON)"><Input.TextArea rows={5} /></Form.Item>
          <Form.Item name="enabled" label="启用" valuePropName="checked"><Switch /></Form.Item>
          <div className="kc-form-actions">
            <Button onClick={() => setModalVisible(false)}>取消</Button>
            <Button htmlType="submit" type="primary" loading={createMutation.isPending || updateMutation.isPending}>保存</Button>
          </div>
        </Form>
      </Modal>
    </div>
  )
}
