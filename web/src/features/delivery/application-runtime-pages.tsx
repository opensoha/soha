import { useEffect, useMemo, useState } from 'react'
import { App, Button, Card, Descriptions, Empty, Form, Input, Modal, Popconfirm, Select, Space, Switch, Tabs, Tag, Typography } from 'antd'
import { ArrowRightOutlined, DeleteOutlined, EditOutlined, PlusOutlined, ReloadOutlined } from '@ant-design/icons'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useNavigate, useParams } from 'react-router-dom'
import { AdminTable } from '@/components/admin-table'
import { PageHeader } from '@/components/page-header'
import { PodLogViewer } from '@/components/pod-log-viewer'
import { PodTerminal } from '@/components/pod-terminal'
import { ResourceMetricsPanel } from '@/components/resource-metrics-panel'
import { hasPermission, usePermissionSnapshot } from '@/features/auth/permission-snapshot'
import { StatusTag } from '@/components/status-tag'
import { buildClusterScopedPath } from '@/features/platform/platform-scope-query'
import { api } from '@/services/api-client'
import { countReleaseDagNodes } from '@/components/release-flow-dag-definition'
import type {
  ApiResponse,
  DeliveryApplicationBindingSummary,
  ApplicationRuntimeDetail,
  ApplicationServiceComponent,
  ApplicationServiceContainer,
  ApplicationRuntimeWorkload,
  ApplicationWorkloadRuntimeDetail,
  BuildRecord,
  DeliveryApplicationDetail,
  DeploymentDetail,
  ExecutionArtifact,
  ExecutionTask,
  ReleaseBundle,
  ReleaseRecord,
  WorkflowRun,
  Pod,
  ResourceMetrics,
} from '@/types'

const { Text } = Typography

const SERVICE_KIND_OPTIONS = [
  { value: 'kubernetes_workload', label: 'Kubernetes Workload' },
  { value: 'helm_release', label: 'Helm Release' },
  { value: 'external_service', label: 'External Service' },
  { value: 'job', label: 'Job' },
]

type ServiceFormValues = Omit<ApplicationServiceComponent, 'applicationId' | 'createdAt' | 'updatedAt' | 'containers'> & {
  containers?: Array<Omit<ApplicationServiceContainer, 'createdAt' | 'updatedAt' | 'runtimePorts'> & {
    runtimePortsText?: string
  }>
}

function firstPodName(pods?: Pod[]) {
  return pods?.[0]?.name || ''
}

function summarizeStatus(item: ApplicationRuntimeWorkload | undefined) {
  if (!item) return 'unknown'
  return item.latestRelease?.status || item.latestWorkflow?.status || item.latestBuild?.status || 'unknown'
}

function parsePorts(value?: string) {
  return String(value ?? '')
    .split(',')
    .map((item) => Number.parseInt(item.trim(), 10))
    .filter((item) => Number.isFinite(item) && item > 0)
}

function formatPorts(value?: number[]) {
  return value?.join(', ') ?? ''
}

function serviceInitialValues(service?: ApplicationServiceComponent | null): ServiceFormValues {
  if (!service) {
    return {
      serviceKind: 'kubernetes_workload',
      enabled: true,
      containers: [
        {
          name: 'main',
          runtimePortsText: '',
        },
      ],
    } as ServiceFormValues
  }
  return {
    ...service,
    containers: (service.containers ?? []).map((container) => ({
      ...container,
      runtimePortsText: formatPorts(container.runtimePorts),
    })),
  } as ServiceFormValues
}

function mapServicePayload(values: ServiceFormValues) {
  return {
    ...values,
    containers: (values.containers ?? []).map((container) => ({
      ...container,
      runtimePorts: parsePorts(container.runtimePortsText),
      runtimePortsText: undefined,
    })),
  }
}

function serviceKindLabel(value?: string) {
  return SERVICE_KIND_OPTIONS.find((item) => item.value === value)?.label ?? value ?? '-'
}

function summarizeBindingStatus(binding?: DeliveryApplicationBindingSummary | null) {
  return binding?.latestRelease?.status
    || binding?.latestWorkflow?.status
    || binding?.latestBuild?.status
    || binding?.latestExecutionTask?.status
    || binding?.latestBundle?.status
    || 'unknown'
}

function countWorkflowValidationNodes(run?: WorkflowRun | null) {
  return run?.nodeRuns?.filter((item) => ['smoke_test', 'check_http', 'check_k8s_event'].includes(item.type)).length ?? 0
}

function summarizeExecutionTask(task?: ExecutionTask | null) {
  if (!task) return '-'
  return `${task.status} · ${task.taskKind}`
}

function summarizeBuildRecord(record?: BuildRecord | null) {
  if (!record) return '-'
  return `${record.status} · ${record.sourceSystem}`
}

function summarizeReleaseRecord(record?: ReleaseRecord | null) {
  if (!record) return '-'
  return `${record.status} · ${record.clusterId}/${record.namespace}`
}

function summarizeReleaseBundle(bundle?: ReleaseBundle | null) {
  if (!bundle) return '-'
  return `${bundle.status} · ${bundle.version}`
}

function workflowTemplateNodeCount(template?: DeliveryApplicationBindingSummary['workflowTemplate'] | null) {
  if (!template) return 0
  return countReleaseDagNodes(template.definition)
}

function summarizeWorkflowRun(run?: WorkflowRun | null) {
  if (!run) return '-'
  return `${run.status} · ${countWorkflowValidationNodes(run)} validation nodes`
}

function summarizeArtifacts(artifacts?: ExecutionArtifact[] | null) {
  if (!artifacts?.length) return '-'
  return artifacts.slice(0, 3).map((item) => item.name || item.ref || item.path || item.kind).join(' / ')
}

function deliveryTargetSummary(target?: {
  clusterId: string
  namespace: string
  workloadName: string
  containerName?: string
  targetKind?: string
  executorKind?: string
  groupKey?: string
  waveKey?: string
  regionKey?: string
  configRef?: string
}) {
  if (!target) return '-'
  const parts = [target.clusterId, target.namespace, target.workloadName]
  return [parts.join(' / '), target.containerName, target.targetKind, target.executorKind]
    .filter(Boolean)
    .join(' · ')
}

function DeploymentOverview({ deployment }: { deployment: DeploymentDetail }) {
  return (
    <Card className="kc-detail-card">
      <Space orientation="vertical" style={{ width: '100%' }} size={12}>
        <div className="kc-application-runtime-overview">
          <div>
            <Text type="secondary">Workload</Text>
            <div className="kc-application-runtime-overview__title">{deployment.name}</div>
          </div>
          <Tag color="blue">{deployment.strategy}</Tag>
        </div>
        <div className="kc-application-runtime-statgrid">
          <Card size="small"><Text type="secondary">Desired</Text><div>{deployment.desiredReplicas}</div></Card>
          <Card size="small"><Text type="secondary">Ready</Text><div>{deployment.readyReplicas}</div></Card>
          <Card size="small"><Text type="secondary">Available</Text><div>{deployment.availableReplicas}</div></Card>
        </div>
        <Card size="small" title="Labels">
          <Space wrap>
            {Object.entries(deployment.labels ?? {}).map(([key, value]) => <Tag key={key}>{`${key}=${value}`}</Tag>)}
          </Space>
        </Card>
      </Space>
    </Card>
  )
}

export function ApplicationDetailPage() {
  const { applicationId } = useParams()
  const navigate = useNavigate()
  const { message } = App.useApp()
  const queryClient = useQueryClient()
  const [activeEnvironmentId, setActiveEnvironmentId] = useState('')
  const [activeTab, setActiveTab] = useState('overview')
  const [serviceModalVisible, setServiceModalVisible] = useState(false)
  const [editingService, setEditingService] = useState<ApplicationServiceComponent | null>(null)
  const [serviceForm] = Form.useForm<ServiceFormValues>()
  const permissionSnapshotQuery = usePermissionSnapshot()
  const canManageServices = hasPermission(permissionSnapshotQuery.data?.data, 'delivery.application-services.manage')

  const runtimeQuery = useQuery({
    queryKey: ['application-runtime', applicationId],
    queryFn: () => api.get<ApiResponse<ApplicationRuntimeDetail>>(`/applications/${applicationId}/runtime`),
    enabled: !!applicationId,
  })
  const detailQuery = useQuery({
    queryKey: ['application-detail', applicationId],
    queryFn: () => api.get<ApiResponse<DeliveryApplicationDetail>>(`/applications/${applicationId}/detail`),
    enabled: !!applicationId,
  })
  const servicesQuery = useQuery({
    queryKey: ['application-services', applicationId],
    queryFn: () => api.get<ApiResponse<ApplicationServiceComponent[]>>(`/applications/${applicationId}/services`),
    enabled: !!applicationId,
  })

  const runtime = runtimeQuery.data?.data
  const detail = detailQuery.data?.data
  const environments = runtime?.environments ?? []
  const services = servicesQuery.data?.data ?? []
  const bindings = detail?.bindings ?? []
  const serviceBuildSourceOptions = useMemo(() => (runtime?.application.buildSources ?? []).map((item) => ({
    value: item.id,
    label: item.name,
  })), [runtime?.application.buildSources])
  const releaseBundleArtifactsQuery = useQuery({
    queryKey: ['application-release-bundle-artifacts', detail?.latestBundle?.id],
    queryFn: () => api.get<ApiResponse<ExecutionArtifact[]>>(`/delivery/release-bundles/${detail!.latestBundle!.id}/artifacts`),
    enabled: !!detail?.latestBundle?.id,
  })
  const latestExecutionArtifactsQuery = useQuery({
    queryKey: ['application-execution-task-artifacts', detail?.latestExecutionTask?.id],
    queryFn: () => api.get<ApiResponse<ExecutionArtifact[]>>(`/delivery/execution-tasks/${detail!.latestExecutionTask!.id}/artifacts`),
    enabled: !!detail?.latestExecutionTask?.id,
  })
  const latestBuildsQuery = useQuery({
    queryKey: ['application-builds', applicationId],
    queryFn: () => api.get<ApiResponse<BuildRecord[]>>(`/builds?applicationId=${applicationId ?? ''}`),
    enabled: !!applicationId,
  })
  const latestReleasesQuery = useQuery({
    queryKey: ['application-releases', applicationId],
    queryFn: () => api.get<ApiResponse<ReleaseRecord[]>>(`/releases?applicationId=${applicationId ?? ''}`),
    enabled: !!applicationId,
  })
  const latestWorkflowsQuery = useQuery({
    queryKey: ['application-workflows', applicationId],
    queryFn: () => api.get<ApiResponse<WorkflowRun[]>>(`/workflows?applicationId=${applicationId ?? ''}`),
    enabled: !!applicationId,
  })

  const createServiceMutation = useMutation({
    mutationFn: (values: ServiceFormValues) => api.post(`/applications/${applicationId}/services`, mapServicePayload(values)),
    onSuccess: () => {
      message.success('服务组件已创建')
      setServiceModalVisible(false)
      setEditingService(null)
      serviceForm.resetFields()
      void queryClient.invalidateQueries({ queryKey: ['application-services', applicationId] })
    },
    onError: (err: Error) => message.error(err.message),
  })
  const updateServiceMutation = useMutation({
    mutationFn: ({ service, values }: { service: ApplicationServiceComponent; values: ServiceFormValues }) => api.put(`/applications/${applicationId}/services/${service.id}`, mapServicePayload(values)),
    onSuccess: () => {
      message.success('服务组件已更新')
      setServiceModalVisible(false)
      setEditingService(null)
      serviceForm.resetFields()
      void queryClient.invalidateQueries({ queryKey: ['application-services', applicationId] })
    },
    onError: (err: Error) => message.error(err.message),
  })
  const deleteServiceMutation = useMutation({
    mutationFn: (service: ApplicationServiceComponent) => api.delete(`/applications/${applicationId}/services/${service.id}`),
    onSuccess: () => {
      message.success('服务组件已删除')
      void queryClient.invalidateQueries({ queryKey: ['application-services', applicationId] })
    },
    onError: (err: Error) => message.error(err.message),
  })

  const openServiceModal = (service?: ApplicationServiceComponent) => {
    const nextService = service ?? null
    setEditingService(nextService)
    setServiceModalVisible(true)
    serviceForm.setFieldsValue(serviceInitialValues(nextService))
  }

  useEffect(() => {
    if (!environments.length) {
      setActiveEnvironmentId('')
      return
    }
    if (activeEnvironmentId && environments.some((item) => item.applicationEnvironmentId === activeEnvironmentId)) {
      return
    }
    setActiveEnvironmentId(environments[0].applicationEnvironmentId)
  }, [activeEnvironmentId, environments])

  const activeEnvironment = environments.find((item) => item.applicationEnvironmentId === activeEnvironmentId) ?? environments[0]
  const workloads = activeEnvironment?.workloads ?? []

  if (runtimeQuery.isLoading) {
    return <div className="kc-page"><Card>Loading...</Card></div>
  }

  if (!runtime) {
    return <div className="kc-page"><Card><Empty description="Application not found" /></Card></div>
  }

  const summaryBindings = bindings.slice(0, 4)
  const latestBuilds = latestBuildsQuery.data?.data ?? []
  const latestReleases = latestReleasesQuery.data?.data ?? []
  const latestWorkflows = latestWorkflowsQuery.data?.data ?? []

  return (
    <div className="kc-page">
      <PageHeader
        title={runtime.application.name}
        description="围绕应用查看服务组件、容器、环境运行态和交付入口。"
        actions={<Button onClick={() => navigate('/applications')}>返回应用中心</Button>}
      />
      <div className="kc-application-runtime-service-summary">
        <Card size="small"><Text type="secondary">服务组件</Text><strong>{services.length}</strong></Card>
        <Card size="small"><Text type="secondary">容器</Text><strong>{services.reduce((sum, item) => sum + (item.containers?.length ?? 0), 0)}</strong></Card>
        <Card size="small"><Text type="secondary">环境</Text><strong>{environments.length}</strong></Card>
        <Card size="small"><Text type="secondary">运行目标</Text><strong>{environments.reduce((sum, item) => sum + (item.workloads?.length ?? 0), 0)}</strong></Card>
      </div>
      <Tabs
        activeKey={activeTab}
        onChange={setActiveTab}
        items={[
          {
            key: 'overview',
            label: '总览',
            children: (
              <div className="kc-application-runtime-overview-grid">
                <Card title="最近执行">
                  <Space orientation="vertical" style={{ width: '100%' }} size={12}>
                    <Descriptions column={1} items={[
                      { key: 'build', label: 'Build', children: summarizeBuildRecord(latestBuilds[0]) },
                      { key: 'workflow', label: 'Workflow', children: summarizeWorkflowRun(latestWorkflows[0]) },
                      { key: 'release', label: 'Release', children: summarizeReleaseRecord(latestReleases[0]) },
                      { key: 'bundle', label: 'Bundle', children: summarizeReleaseBundle(detail?.latestBundle) },
                      { key: 'task', label: 'Execution Task', children: summarizeExecutionTask(detail?.latestExecutionTask) },
                      { key: 'artifacts', label: 'Artifacts', children: summarizeArtifacts(detail?.latestExecutionTask?.artifacts ?? latestExecutionArtifactsQuery.data?.data) },
                    ]} />
                  </Space>
                </Card>
                <Card title="环境概览">
                  <Space orientation="vertical" style={{ width: '100%' }} size={12}>
                    {summaryBindings.length > 0 ? summaryBindings.map((binding) => (
                      <div className="kc-application-runtime-binding-row" key={binding.applicationEnvironmentId}>
                        <div className="kc-application-runtime-binding-row__main">
                          <strong>{binding.environmentName || binding.environmentKey || binding.environmentId}</strong>
                          <Text type="secondary">{binding.workflowTemplate?.name || binding.workflowTemplateName || '未绑定工作流模板'}</Text>
                        </div>
                        <Space wrap>
                          <Tag>{summarizeBindingStatus(binding)}</Tag>
                          <Tag>{binding.targetCount} targets</Tag>
                          <Button size="small" type="link" onClick={() => navigate(`/application-environments/${binding.applicationEnvironmentId}`)}>绑定详情</Button>
                        </Space>
                      </div>
                    )) : <Empty description="尚未绑定任何环境" />}
                  </Space>
                </Card>
              </div>
            ),
          },
          {
            key: 'services',
            label: '服务组件',
            children: (
              <Card
                className="kc-detail-card"
                title="服务组件"
                extra={canManageServices ? <Button type="primary" icon={<PlusOutlined />} onClick={() => openServiceModal()}>新建服务</Button> : null}
              >
                {services.length > 0 ? (
                  <div className="kc-application-service-grid">
                    {services.map((service) => (
                      <Card
                        key={service.id}
                        size="small"
                        className="kc-application-service-card"
                        title={service.name}
                        extra={<StatusTag value={service.enabled ? 'enabled' : 'disabled'} />}
                        actions={canManageServices ? [
                          <Button key="edit" type="link" size="small" icon={<EditOutlined />} onClick={() => openServiceModal(service)}>编辑</Button>,
                          <Popconfirm key="delete" title="确认删除该服务组件？" onConfirm={() => deleteServiceMutation.mutate(service)}>
                            <Button type="link" size="small" danger icon={<DeleteOutlined />}>删除</Button>
                          </Popconfirm>,
                        ] : undefined}
                      >
                        <div className="kc-application-service-card__body">
                          <div className="kc-application-service-card__meta">
                            <Tag>{serviceKindLabel(service.serviceKind)}</Tag>
                            {service.ownerTeam ? <Tag>{service.ownerTeam}</Tag> : null}
                            {service.buildSourceId ? <Tag>{service.buildSourceId}</Tag> : null}
                          </div>
                          <Text type="secondary">{service.repositoryPath || runtime.application.repositoryPath || '未配置独立仓库'}</Text>
                          <div className="kc-application-container-list">
                            {(service.containers ?? []).map((container) => (
                              <div className="kc-application-container-row" key={container.id || container.name}>
                                <span>{container.name}</span>
                                <Text type="secondary">{container.imageRepository || '未配置镜像仓库'}</Text>
                                {container.runtimePorts?.length ? <Tag>{container.runtimePorts.join(', ')}</Tag> : null}
                              </div>
                            ))}
                            {!service.containers?.length ? <Text type="secondary">尚未配置容器</Text> : null}
                          </div>
                        </div>
                      </Card>
                    ))}
                  </div>
                ) : (
                  <Empty description="尚未配置服务组件。先把应用拆成服务和容器，后续 CI/CD DAG 才能按服务选择构建、测试和部署目标。" />
                )}
              </Card>
            ),
          },
          {
            key: 'environments',
            label: '环境矩阵',
            children: (
              <div className="kc-application-runtime-environment-stack">
                <Card className="kc-detail-card">
                  <Space wrap>
                    {environments.map((item) => (
                      <Tag
                        key={item.applicationEnvironmentId}
                        color={activeEnvironmentId === item.applicationEnvironmentId ? 'blue' : undefined}
                        onClick={() => setActiveEnvironmentId(item.applicationEnvironmentId)}
                      >
                        {item.environmentName || item.environmentKey || item.environmentId}
                      </Tag>
                    ))}
                  </Space>
                </Card>
                <div className="kc-application-runtime-grid">
                  {workloads.length > 0 ? workloads.map((workload) => (
                    <Card
                      key={`${workload.clusterId}/${workload.namespace}/${workload.workloadName}`}
                      hoverable
                      className="kc-application-runtime-card"
                      onClick={() => navigate(`/applications/${runtime.application.id}/application-environments/${workload.applicationEnvironmentId}/workloads/${encodeURIComponent(workload.workloadName)}`)}
                      actions={[
                        <Button
                          key="open"
                          type="link"
                          icon={<ArrowRightOutlined />}
                          onClick={(event) => {
                            event.stopPropagation()
                            navigate(`/applications/${runtime.application.id}/application-environments/${workload.applicationEnvironmentId}/workloads/${encodeURIComponent(workload.workloadName)}`)
                          }}
                        >
                          进入详情
                        </Button>,
                      ]}
                    >
                      <Space orientation="vertical" style={{ width: '100%' }}>
                        <div className="kc-application-runtime-card__head">
                          <strong>{workload.workloadName}</strong>
                          <StatusTag value={summarizeStatus(workload)} />
                        </div>
                        <Text type="secondary">{`${workload.workloadKind} · ${workload.namespace}`}</Text>
                        <Space wrap>
                          <Tag>Desired {workload.desiredReplicas}</Tag>
                          <Tag>Ready {workload.readyReplicas}</Tag>
                          <Tag>{workload.clusterId}</Tag>
                        </Space>
                      </Space>
                    </Card>
                  )) : (
                    <Card className="kc-application-runtime-empty"><Empty description="当前环境下没有可显示的服务/Deployment" /></Card>
                  )}
                </div>
              </div>
            ),
          },
          {
            key: 'delivery',
            label: '交付物',
            children: (
              <div className="kc-application-runtime-delivery-grid">
                <Card title="Release Bundle">
                  <Descriptions column={1} items={[
                    { key: 'bundle', label: '当前 Bundle', children: summarizeReleaseBundle(detail?.latestBundle) },
                    { key: 'bundleArtifacts', label: 'Bundle 交付物', children: summarizeArtifacts(releaseBundleArtifactsQuery.data?.data) },
                    { key: 'task', label: '执行任务', children: summarizeExecutionTask(detail?.latestExecutionTask) },
                    { key: 'taskArtifacts', label: 'Task 交付物', children: summarizeArtifacts(latestExecutionArtifactsQuery.data?.data) },
                  ]} />
                </Card>
                <Card title="Build / Release / Workflow">
                  <AdminTable
                    rowKey="id"
                    pagination={false}
                    dataSource={[
                      ...latestBuilds.map((item) => ({ kind: 'build', id: item.id, status: item.status, label: item.sourceSystem, summary: item.metadata?.artifact ? 'artifact ready' : 'build record' })),
                      ...latestReleases.map((item) => ({ kind: 'release', id: item.id, status: item.status, label: `${item.clusterId}/${item.namespace}`, summary: item.deploymentName })),
                      ...latestWorkflows.map((item) => ({ kind: 'workflow', id: item.id, status: item.status, label: item.workflowName, summary: `${item.steps?.length ?? 0} steps` })),
                    ]}
                    columns={[
                      { title: '类型', dataIndex: 'kind' },
                      { title: 'ID', dataIndex: 'id' },
                      { title: '状态', dataIndex: 'status', render: (value: string) => <StatusTag value={value} /> },
                      { title: '主体', dataIndex: 'label' },
                      { title: '说明', dataIndex: 'summary' },
                    ]}
                  />
                </Card>
              </div>
            ),
          },
          {
            key: 'pipeline',
            label: '流水线',
            children: (
              <div className="kc-application-runtime-pipeline-grid">
                <Card title="DAG 模板">
                  <Space orientation="vertical" style={{ width: '100%' }} size={12}>
                    {bindings.length > 0 ? bindings.map((binding) => (
                      <div className="kc-application-runtime-binding-row" key={binding.applicationEnvironmentId}>
                        <div className="kc-application-runtime-binding-row__main">
                          <strong>{binding.environmentName || binding.environmentKey || binding.environmentId}</strong>
                          <Text type="secondary">{binding.workflowTemplate?.name || binding.workflowTemplateName || '未绑定工作流模板'}</Text>
                        </div>
                        <Space wrap>
                          <Tag>{workflowTemplateNodeCount(binding.workflowTemplate)} nodes</Tag>
                          <Tag>{binding.requiresApproval ? 'approval required' : 'no approval'}</Tag>
                        </Space>
                      </div>
                    )) : <Empty description="尚未绑定 CI/CD DAG 模板" />}
                  </Space>
                </Card>
                <Card title="最近工作流运行">
                  <AdminTable
                    rowKey="id"
                    pagination={false}
                    dataSource={latestWorkflows}
                    columns={[
                      { title: 'ID', dataIndex: 'id' },
                      { title: '工作流', dataIndex: 'workflowName' },
                      { title: '状态', dataIndex: 'status', render: (value: string) => <StatusTag value={value} /> },
                      { title: '步骤', dataIndex: 'steps', render: (_: unknown, record: WorkflowRun) => `${record.steps?.length ?? 0} steps` },
                      { title: '验证节点', dataIndex: 'nodeRuns', render: (_: unknown, record: WorkflowRun) => countWorkflowValidationNodes(record) },
                    ]}
                  />
                </Card>
              </div>
            ),
          },
          {
            key: 'verification',
            label: '测试验证',
            children: (
              <div className="kc-application-runtime-verification-grid">
                <Card title="验证门禁">
                  <Descriptions column={1} items={[
                    { key: 'workflowTemplate', label: 'Workflow Template', children: detail?.bindings?.[0]?.workflowTemplate?.name || detail?.bindings?.[0]?.workflowTemplateName || '-' },
                    { key: 'workflowNodes', label: 'DAG 节点数', children: workflowTemplateNodeCount(detail?.bindings?.[0]?.workflowTemplate) },
                    { key: 'approval', label: '审批要求', children: detail?.bindings?.[0]?.requiresApproval ? '需要审批' : '无需审批' },
                    { key: 'releaseTarget', label: '当前目标', children: deliveryTargetSummary(detail?.bindings?.[0]?.targets?.[0]) },
                  ]} />
                </Card>
                <Card title="测试入口">
                  <Space orientation="vertical" style={{ width: '100%' }}>
                    <Button type="primary" onClick={() => navigate(`/application-environments/${bindings[0]?.applicationEnvironmentId ?? ''}`)} disabled={!bindings[0]?.applicationEnvironmentId}>进入环境绑定</Button>
                    <Button onClick={() => navigate('/workflow-templates')}>查看 DAG 模板</Button>
                    <Button onClick={() => navigate('/delivery/release-bundles')}>查看交付物中心</Button>
                  </Space>
                </Card>
              </div>
            ),
          },
        ]}
      />
      <Modal
        title={editingService ? '编辑服务组件' : '新建服务组件'}
        open={serviceModalVisible}
        onCancel={() => {
          setServiceModalVisible(false)
          setEditingService(null)
          serviceForm.resetFields()
        }}
        footer={null}
        destroyOnHidden
        width={900}
      >
        <Form
          form={serviceForm}
          layout="vertical"
          initialValues={serviceInitialValues(editingService)}
          onFinish={(values) => {
            if (editingService) {
              updateServiceMutation.mutate({ service: editingService, values })
            } else {
              createServiceMutation.mutate(values)
            }
          }}
        >
          <div className="kc-application-service-form-grid">
            <Form.Item name="key" label="服务 Key" rules={[{ required: true, message: '请输入服务 Key' }]}>
              <Input placeholder="api" />
            </Form.Item>
            <Form.Item name="name" label="服务名称" rules={[{ required: true, message: '请输入服务名称' }]}>
              <Input placeholder="API 服务" />
            </Form.Item>
            <Form.Item name="serviceKind" label="服务类型">
              <Select options={SERVICE_KIND_OPTIONS} />
            </Form.Item>
            <Form.Item name="enabled" label="启用" valuePropName="checked">
              <Switch />
            </Form.Item>
            <Form.Item name="ownerTeam" label="负责人团队">
              <Input />
            </Form.Item>
            <Form.Item name="buildSourceId" label="构建来源">
              <Select allowClear options={serviceBuildSourceOptions} />
            </Form.Item>
            <Form.Item name="repositoryPath" label="服务仓库">
              <Input placeholder={runtime.application.repositoryPath || 'group/project'} />
            </Form.Item>
            <Form.Item name="defaultBranch" label="默认分支">
              <Input placeholder={runtime.application.defaultBranch || 'main'} />
            </Form.Item>
          </div>
          <Form.Item name="description" label="描述">
            <Input.TextArea rows={2} />
          </Form.Item>

          <Form.List name="containers">
            {(fields, { add, remove }) => (
              <div className="kc-application-service-containers-editor">
                <div className="kc-application-service-containers-editor__head">
                  <Text strong>容器</Text>
                  <Button size="small" icon={<PlusOutlined />} onClick={() => add({ name: 'main' })}>添加容器</Button>
                </div>
                {fields.map((field) => (
                  <Card key={field.key} size="small" className="kc-application-service-container-editor">
                    <div className="kc-application-service-container-editor__grid">
                      <Form.Item name={[field.name, 'name']} label="容器名" rules={[{ required: true, message: '请输入容器名' }]}>
                        <Input placeholder="main" />
                      </Form.Item>
                      <Form.Item name={[field.name, 'imageRepository']} label="镜像仓库">
                        <Input placeholder="registry.example.com/team/api" />
                      </Form.Item>
                      <Form.Item name={[field.name, 'defaultTagTemplate']} label="Tag 模板">
                        <Input placeholder="{{branch}}-{{sha}}" />
                      </Form.Item>
                      <Form.Item name={[field.name, 'runtimePortsText']} label="端口">
                        <Input placeholder="8080, 9090" />
                      </Form.Item>
                      <Form.Item name={[field.name, 'dockerfilePath']} label="Dockerfile">
                        <Input placeholder="Dockerfile" />
                      </Form.Item>
                      <Form.Item name={[field.name, 'buildContextDir']} label="构建上下文">
                        <Input placeholder="." />
                      </Form.Item>
                    </div>
                    <Button type="text" danger icon={<DeleteOutlined />} onClick={() => remove(field.name)}>移除容器</Button>
                  </Card>
                ))}
              </div>
            )}
          </Form.List>

          <div className="kc-form-actions">
            <Button onClick={() => setServiceModalVisible(false)}>取消</Button>
            <Button htmlType="submit" type="primary" loading={createServiceMutation.isPending || updateServiceMutation.isPending}>
              保存
            </Button>
          </div>
        </Form>
      </Modal>
    </div>
  )
}

export function ApplicationWorkloadDetailPage() {
  const { applicationId, applicationEnvironmentId, workloadName } = useParams()
  const navigate = useNavigate()
  const { message } = App.useApp()
  const queryClient = useQueryClient()
  const [activeTab, setActiveTab] = useState('overview')
  const [selectedPodName, setSelectedPodName] = useState('')
  const [terminalVisible, setTerminalVisible] = useState(false)
  const permissionSnapshotQuery = usePermissionSnapshot()
  const canManage = hasPermission(permissionSnapshotQuery.data?.data, 'delivery.application.update')

  const detailQuery = useQuery({
    queryKey: ['application-workload-runtime', applicationId, applicationEnvironmentId, workloadName],
    queryFn: () => api.get<ApiResponse<ApplicationWorkloadRuntimeDetail>>(`/applications/${applicationId}/application-environments/${applicationEnvironmentId}/workloads/${encodeURIComponent(workloadName || '')}/runtime`),
    enabled: !!applicationId && !!applicationEnvironmentId && !!workloadName,
  })

  const detail = detailQuery.data?.data
  const podList = detail?.pods ?? []
  const serviceList = detail?.services ?? []
  const ingressList = detail?.ingresses ?? []
  const deployment = detail?.deployment

  useEffect(() => {
    if (!selectedPodName) {
      setSelectedPodName(firstPodName(podList))
    }
  }, [podList, selectedPodName])

  const selectedPod = podList.find((item) => item.name === selectedPodName) ?? podList[0]

  const metricsQuery = useQuery({
    queryKey: ['application-workload-metrics', detail?.workload.clusterId, detail?.workload.namespace, detail?.workload.workloadName],
    queryFn: () => api.get<ApiResponse<ResourceMetrics>>(
      buildClusterScopedPath(detail!.workload.clusterId, `workloads/deployments/${encodeURIComponent(detail!.workload.workloadName)}/metrics`, detail!.workload.namespace, { rangeMinutes: 60 }),
    ),
    enabled: !!detail && activeTab === 'metrics',
  })

  const restartMutation = useMutation({
    mutationFn: () => api.post(`/clusters/${detail!.workload.clusterId}/workloads/deployments/restart`, {
      namespace: detail!.workload.namespace,
      name: detail!.workload.workloadName,
    }),
    onSuccess: () => {
      message.success('已触发重启')
      void queryClient.invalidateQueries({ queryKey: ['application-workload-runtime', applicationId, applicationEnvironmentId, workloadName] })
    },
    onError: (err: Error) => message.error(err.message),
  })

  if (detailQuery.isLoading) {
    return <div className="kc-page"><Card>Loading...</Card></div>
  }
  if (!detail || !deployment) {
    return <div className="kc-page"><Card><Empty description="未找到运行详情" /></Card></div>
  }

  const tabItems = [
    {
      key: 'overview',
      label: '概览',
      children: <DeploymentOverview deployment={deployment} />,
    },
    {
      key: 'pods',
      label: 'Pods',
      children: (
        <AdminTable
          columns={[
            { title: '名称', dataIndex: 'name' },
            { title: '状态', dataIndex: 'phase' },
            { title: '节点', dataIndex: 'nodeName', render: (value?: string) => value || '-' },
            { title: '重启', dataIndex: 'restarts' },
          ]}
          dataSource={podList}
          rowKey="name"
          pageSize={10}
          onRow={(record: Pod) => ({
            onClick: () => setSelectedPodName(record.name),
          })}
        />
      ),
    },
    {
      key: 'network',
      label: '网络',
      children: (
        <div className="kc-application-runtime-network">
          <Card title="Services">
            <AdminTable
              columns={[
                { title: '名称', dataIndex: 'name' },
                { title: '类型', dataIndex: 'type' },
                { title: 'Cluster IP', dataIndex: 'clusterIp', render: (value?: string) => value || '-' },
              ]}
              dataSource={serviceList}
              rowKey="name"
              pageSize={10}
            />
          </Card>
          <Card title="Ingresses">
            <AdminTable
              columns={[
                { title: '名称', dataIndex: 'name' },
                { title: '主机', dataIndex: 'hosts', render: (value?: string[]) => (value ?? []).join(', ') || '-' },
                { title: '后端', dataIndex: 'backendServices', render: (value?: string[]) => (value ?? []).join(', ') || '-' },
              ]}
              dataSource={ingressList}
              rowKey="name"
              pageSize={10}
            />
          </Card>
        </div>
      ),
    },
    {
      key: 'logs',
      label: '日志',
      children: selectedPod ? (
        <PodLogViewer
          clusterId={detail.workload.clusterId}
          namespace={detail.workload.namespace}
          podName={selectedPod.name}
        />
      ) : (
        <Empty description="请选择一个 Pod" />
      ),
    },
    {
      key: 'terminal',
      label: '终端',
      children: selectedPod ? (
        <Space orientation="vertical" style={{ width: '100%' }}>
          <Button type="primary" onClick={() => setTerminalVisible(true)}>打开终端</Button>
          <Card title={selectedPod.name}>
            <Text type="secondary">终端会复用当前 Pod 的 cluster / namespace / workload 上下文。</Text>
          </Card>
          <Modal title={`Terminal: ${selectedPod.name}`} open={terminalVisible} onCancel={() => setTerminalVisible(false)} footer={null} width={1080}>
            <PodTerminal clusterId={detail.workload.clusterId} namespace={detail.workload.namespace} podName={selectedPod.name} />
          </Modal>
        </Space>
      ) : (
        <Empty description="请选择一个 Pod" />
      ),
    },
    {
      key: 'metrics',
      label: '监控',
      children: (
        <ResourceMetricsPanel
          title="Deployment Metrics"
          data={metricsQuery.data?.data}
          loading={metricsQuery.isLoading}
          rangeMinutes={60}
          compact
        />
      ),
    },
  ]

  return (
    <div className="kc-page">
      <PageHeader
        title={detail.application.name}
        description={`${detail.environment?.name || detail.binding.environmentKey || detail.binding.environmentId} · ${detail.workload.workloadName}`}
        actions={(
          <Space>
            <Button onClick={() => navigate(`/applications/${detail.application.id}`)}>返回应用</Button>
            {canManage ? <Button icon={<ReloadOutlined />} onClick={() => restartMutation.mutate()}>重启</Button> : null}
          </Space>
        )}
      />
      <Tabs items={tabItems} activeKey={activeTab} onChange={setActiveTab} />
    </div>
  )
}
