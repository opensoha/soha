import { useMemo, useState } from 'react'
import { App, Button, Card, Descriptions, Empty, Form, Input, Modal, Popconfirm, Select, Space, Switch, Tag, Typography } from 'antd'
import { CodeOutlined, DeleteOutlined, EditOutlined, EyeOutlined, PlayCircleOutlined, PlusOutlined } from '@ant-design/icons'
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
type ApplicationWorkspaceCard = {
  app: DeliveryApplication
  bindings: ReleaseBoardEntry[]
  lastStatus: string
  activeTargets: number
  latestEnvironmentName: string
}

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

function renderTargetSummary(targets?: ApplicationBindingRow['targets']) {
  if (!targets?.length) {
    return '-'
  }
  const visibleTargets = targets.slice(0, 2)
  return (
    <Space direction="vertical" size={2}>
      {visibleTargets.map((target, index) => (
        <Text key={`${target.clusterId}-${target.namespace}-${target.workloadName}-${index}`}>
          {`${target.clusterId} / ${target.namespace} / ${target.workloadName}`}
        </Text>
      ))}
      {targets.length > visibleTargets.length ? (
        <Text type="secondary">{`+${targets.length - visibleTargets.length}`}</Text>
      ) : null}
    </Space>
  )
}

function buildStatus(entry?: ReleaseBoardEntry) {
  if (entry?.latestRelease?.status) return entry.latestRelease.status
  if (entry?.latestWorkflow?.status) return entry.latestWorkflow.status
  if (entry?.latestBuild?.status) return entry.latestBuild.status
  return 'unknown'
}

function summarizeApplicationRole(app: DeliveryApplication) {
  const language = app.language ? app.language.toUpperCase() : 'APP'
  const group = app.group || '业务应用'
  return `${language} · ${group}`
}

function summarizeEnvironmentCoverage(bindings: ReleaseBoardEntry[]) {
  const names = Array.from(
    new Set(bindings.map((item) => item.environmentName || item.environmentKey || item.environmentId).filter(Boolean)),
  )
  if (names.length === 0) return '尚未绑定环境'
  if (names.length === 1) return names[0]
  return `${names.slice(0, 2).join(' / ')}${names.length > 2 ? ` +${names.length - 2}` : ''}`
}

function canCancelExecutionTask(task?: ExecutionTask | null) {
  return task ? ['queued', 'dispatching', 'running'].includes(task.status) : false
}

function canRetryExecutionTask(task?: ExecutionTask | null) {
  return task ? ['failed', 'callback_timeout', 'canceled'].includes(task.status) : false
}

export function ApplicationsPage() {
  const navigate = useNavigate()
  const [activeGroup, setActiveGroup] = useState<string>('all')

  const applicationsQuery = useQuery({
    queryKey: ['applications'],
    queryFn: () => api.get<ApiResponse<DeliveryApplication[]>>('/applications'),
  })
  const releaseBoardQuery = useQuery({
    queryKey: ['delivery-release-board'],
    queryFn: () => api.get<ApiResponse<ReleaseBoardEntry[]>>('/delivery/release-board'),
  })

  const boardByApp = useMemo(() => {
    const items = releaseBoardQuery.data?.data ?? []
    return items.reduce<Record<string, ReleaseBoardEntry[]>>((acc, item) => {
      acc[item.applicationId] = [...(acc[item.applicationId] ?? []), item]
      return acc
    }, {})
  }, [releaseBoardQuery.data])
  const applicationCards = useMemo<ApplicationWorkspaceCard[]>(() => {
    return (applicationsQuery.data?.data ?? []).map((app) => {
      const bindings = boardByApp[app.id] ?? []
      return {
        app,
        bindings,
        lastStatus: buildStatus(bindings[0]),
        activeTargets: bindings.reduce((sum, item) => sum + (item.targets?.length ?? 0), 0),
        latestEnvironmentName: summarizeEnvironmentCoverage(bindings),
      }
    })
  }, [applicationsQuery.data, boardByApp])
  const groupOptions = useMemo(() => {
    const groups = Array.from(new Set(applicationCards.map(({ app }) => app.group).filter(Boolean)))
    return ['all', ...groups]
  }, [applicationCards])
  const visibleApplicationCards = useMemo(
    () => applicationCards.filter(({ app }) => activeGroup === 'all' || app.group === activeGroup),
    [activeGroup, applicationCards],
  )

  return (
    <div className="kc-page">
      <section className="kc-page-section">
        <div className="kc-application-center-shell">
          <div className="kc-application-center-header">
            <div className="kc-application-center-header__main">
              <h2 className="kc-application-center-header__title">应用中心</h2>
              <Text type="secondary">按应用查看构建来源、环境覆盖，并进入对应应用页。</Text>
              <div className="kc-application-group-tags">
                {groupOptions.map((group) => (
                  <Tag
                    key={group}
                    className={`kc-application-group-tag ${activeGroup === group ? 'is-active' : ''}`}
                    variant="filled"
                    onClick={() => setActiveGroup(group)}
                  >
                    {group === 'all' ? '全部' : group}
                  </Tag>
                ))}
              </div>
            </div>
            <Button type="primary" icon={<PlusOutlined />} onClick={() => navigate('/application-management')}>
              新建应用
            </Button>
          </div>
        </div>
      </section>

      <section className="kc-page-section">
        <div className="kc-application-card-grid">
          {visibleApplicationCards.length > 0 ? visibleApplicationCards.map(({ app, bindings, lastStatus, activeTargets, latestEnvironmentName }) => {
            const defaultBuildSource = (app.buildSources ?? []).find((item) => item.isDefault)
            return (
              <Card
                key={app.id}
                className="kc-application-card"
                hoverable
                onClick={() => navigate(`/applications/${app.id}`)}
                actions={[
                  <Button key="detail" type="link" icon={<EyeOutlined />} onClick={(event) => { event.stopPropagation(); navigate(`/applications/${app.id}`) }}>进入应用</Button>,
                ]}
              >
                <div className="kc-application-card__header">
                  <div className="kc-application-card__title-wrap">
                    <div className="kc-application-card__title-row">
                      <h3 className="kc-application-card__title">{app.name}</h3>
                      <StatusTag value={lastStatus} />
                    </div>
                    <Text type="secondary">{summarizeApplicationRole(app)}</Text>
                  </div>
                  <div className="kc-application-card__meta-chip">
                    <CodeOutlined />
                    <span>{summarizeBuildSource(defaultBuildSource)}</span>
                  </div>
                </div>

                <div className="kc-application-card__stats">
                  <div className="kc-application-card__stat">
                    <span className="kc-application-card__stat-label">环境覆盖</span>
                    <span className="kc-application-card__stat-value">{bindings.length || app.environmentCount || 0}</span>
                  </div>
                  <div className="kc-application-card__stat">
                    <span className="kc-application-card__stat-label">部署目标</span>
                    <span className="kc-application-card__stat-value">{activeTargets}</span>
                  </div>
                  <div className="kc-application-card__stat is-wide">
                    <span className="kc-application-card__stat-label">环境描述</span>
                    <span className="kc-application-card__stat-value">{latestEnvironmentName}</span>
                  </div>
                </div>
              </Card>
            )
          }) : (
            <Card className="kc-application-empty-card">
              <Empty description={activeGroup === 'all' ? '当前还没有应用，先创建第一个应用并接入仓库、构建来源和环境。' : '当前分组下还没有应用。'} />
            </Card>
          )}
        </div>
      </section>
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
            { title: '部署目标', dataIndex: 'targets', render: (value: ApplicationBindingRow['targets']) => renderTargetSummary(value) },
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
