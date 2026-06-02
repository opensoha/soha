import { useEffect, useMemo, useState } from 'react'
import { App, Button, Card, Descriptions, Form, Input, Modal, Popconfirm, Select, Space, Switch, Tabs, Tag, Tooltip, Typography } from 'antd'
import { ArrowRightOutlined, CloudUploadOutlined, DeleteOutlined, EditOutlined, LinkOutlined, MinusCircleOutlined, PlayCircleOutlined, PlusOutlined, ReloadOutlined, RocketOutlined, SafetyCertificateOutlined } from '@ant-design/icons'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useNavigate, useParams } from 'react-router-dom'
import { ManagementDetailHeader, ManagementIconButton, ManagementState } from '@/components/management-list'
import { DeliveryTable } from '@/features/delivery/delivery-table'
import { PodLogViewer } from '@/components/pod-log-viewer'
import { PodTerminal } from '@/components/pod-terminal'
import { ResourceMetricsPanel } from '@/components/resource-metrics-panel'
import { hasPermission, usePermissionSnapshot } from '@/features/auth/permission-snapshot'
import { StatusTag } from '@/components/status-tag'
import { buildClusterScopedPath } from '@/features/platform/platform-scope-query'
import { api } from '@/services/api-client'
import { countReleaseDagNodes } from '@/components/release-flow-dag-definition'
import {
  countRuntimeArtifacts,
  countWorkflowValidationNodes,
  runtimeValidationNodeCount,
  summarizeDeliveryBuildSignal,
  summarizeDeliveryValidationSignal,
  workflowTemplateValidationNodeCount,
} from '@/features/delivery/delivery-status'
import {
  ApplicationManagementModals,
  summarizeBuildSource,
  useApplicationManagementState,
} from '@/features/delivery/application-management-pages'
import type {
  ApiResponse,
  ApplicationDeliveryActionKind,
  ApplicationDeliveryActionRequest,
  ApplicationDeliveryActionResponse,
  ApplicationEnvironment,
  DeliveryApplicationBindingSummary,
  ApplicationRuntimeDetail,
  ApplicationServiceComponent,
  ApplicationServiceContainer,
  ApplicationRuntimeWorkload,
  ApplicationWorkloadRuntimeDetail,
  BuildSource,
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

type DeliveryActionFormValues = {
  applicationEnvironmentId?: string
  targetId?: string
  buildSourceId?: string
  refType?: 'branch' | 'tag' | 'commit'
  refName?: string
  imageTag?: string
  releaseName?: string
  containerName?: string
}

const REF_TYPE_OPTIONS = [
  { value: 'branch', label: 'Branch' },
  { value: 'tag', label: 'Tag' },
  { value: 'commit', label: 'Commit' },
]

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

function renderBindingTargets(targets?: ApplicationEnvironment['targets']) {
  if (!targets?.length) return '-'
  return (
    <Space orientation="vertical" size={2}>
      {targets.slice(0, 2).map((target, index) => (
        <Text key={`${target.clusterId}-${target.namespace}-${target.workloadName}-${index}`}>
          {`${target.clusterId} / ${target.namespace} / ${target.workloadName}`}
        </Text>
      ))}
      {targets.length > 2 ? <Text type="secondary">{`+${targets.length - 2}`}</Text> : null}
    </Space>
  )
}

function renderSelectorLabels(selector?: ApplicationEnvironment['resourceSelector']) {
  const labels = Object.entries(selector?.matchLabels ?? {})
  if (!labels.length) return '-'
  return labels.map(([key, value]) => `${key}=${value}`).join(', ')
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

function buildDeliveryActionPayload(action: ApplicationDeliveryActionKind, values: DeliveryActionFormValues): ApplicationDeliveryActionRequest {
  return {
    action,
    applicationEnvironmentId: values.applicationEnvironmentId ?? '',
    targetId: values.targetId,
    buildSourceId: values.buildSourceId,
    refType: values.refType,
    refName: values.refName,
    imageTag: values.imageTag,
    releaseName: values.releaseName,
    containerName: values.containerName,
  }
}

function disabledReason(reasons: Array<string | false | undefined>) {
  return reasons.find(Boolean) || ''
}

function DeploymentOverview({ deployment }: { deployment: DeploymentDetail }) {
  return (
    <Card className="soha-management-panel-card">
      <Space orientation="vertical" style={{ width: '100%' }} size={12}>
        <div className="soha-application-runtime-overview">
          <div>
            <Text type="secondary">Workload</Text>
            <div className="soha-application-runtime-overview__title">{deployment.name}</div>
          </div>
          <Tag color="blue">{deployment.strategy}</Tag>
        </div>
        <div className="soha-application-runtime-statgrid">
          <Card className="soha-management-panel-card" size="small"><Text type="secondary">Desired</Text><div>{deployment.desiredReplicas}</div></Card>
          <Card className="soha-management-panel-card" size="small"><Text type="secondary">Ready</Text><div>{deployment.readyReplicas}</div></Card>
          <Card className="soha-management-panel-card" size="small"><Text type="secondary">Available</Text><div>{deployment.availableReplicas}</div></Card>
        </div>
        <Card className="soha-management-panel-card" size="small" title="Labels">
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
  const [deliveryForm] = Form.useForm<DeliveryActionFormValues>()
  const permissionSnapshotQuery = usePermissionSnapshot()
  const managementState = useApplicationManagementState()
  const canManageServices = hasPermission(permissionSnapshotQuery.data?.data, 'delivery.application-services.manage')
  const canTriggerBuild = hasPermission(permissionSnapshotQuery.data?.data, 'delivery.builds.trigger')
  const canTriggerWorkflow = hasPermission(permissionSnapshotQuery.data?.data, 'delivery.workflows.trigger')
  const canTriggerRelease = hasPermission(permissionSnapshotQuery.data?.data, 'delivery.releases.trigger')

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
  const selectedDeliveryBindingId = Form.useWatch('applicationEnvironmentId', deliveryForm)
  const selectedTargetId = Form.useWatch('targetId', deliveryForm)
  const selectedBuildSourceId = Form.useWatch('buildSourceId', deliveryForm)
  const selectedImageTag = Form.useWatch('imageTag', deliveryForm)
  const serviceBuildSourceOptions = useMemo(() => (runtime?.application.buildSources ?? []).map((item) => ({
    value: item.id,
    label: item.name,
  })), [runtime?.application.buildSources])

  useEffect(() => {
    if (!applicationId || managementState.selectedApplicationId === applicationId) return
    managementState.setSelectedApplicationId(applicationId)
  }, [applicationId, managementState])
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
  const deliveryActionMutation = useMutation({
    mutationFn: (payload: ApplicationDeliveryActionRequest) => api.post<ApiResponse<ApplicationDeliveryActionResponse>>(`/applications/${applicationId}/delivery-actions`, payload),
    onSuccess: (_data, payload) => {
      const labels: Record<ApplicationDeliveryActionKind, string> = {
        build: '构建',
        deploy: '部署',
        build_deploy: '构建并部署',
        workflow: '工作流',
        verify: '验证',
      }
      message.success(`${labels[payload.action]}已触发`)
      void queryClient.invalidateQueries({ queryKey: ['application-detail', applicationId] })
      void queryClient.invalidateQueries({ queryKey: ['application-runtime', applicationId] })
      void queryClient.invalidateQueries({ queryKey: ['application-builds', applicationId] })
      void queryClient.invalidateQueries({ queryKey: ['application-releases', applicationId] })
      void queryClient.invalidateQueries({ queryKey: ['application-workflows', applicationId] })
      void queryClient.invalidateQueries({ queryKey: ['delivery-release-board'] })
      void queryClient.invalidateQueries({ queryKey: ['execution-tasks'] })
    },
    onError: (err: Error) => message.error(err.message),
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

  useEffect(() => {
    if (!bindings.length) {
      deliveryForm.resetFields()
      return
    }
    const currentBindingId = deliveryForm.getFieldValue('applicationEnvironmentId')
    const nextBinding = bindings.find((item) => item.applicationEnvironmentId === currentBindingId) ?? bindings[0]
    const enabledTarget = nextBinding.targets?.find((item) => item.enabled) ?? nextBinding.targets?.[0]
    const defaultSource = nextBinding.buildSource ?? runtime?.application.buildSources?.find((item) => item.isDefault) ?? runtime?.application.buildSources?.[0]
    deliveryForm.setFieldsValue({
      applicationEnvironmentId: nextBinding.applicationEnvironmentId,
      targetId: enabledTarget?.id,
      buildSourceId: nextBinding.buildSourceId || defaultSource?.id,
      refType: nextBinding.buildPolicy?.refType as DeliveryActionFormValues['refType'] || 'branch',
      refName: nextBinding.buildPolicy?.refValue || 'main',
      imageTag: defaultSource?.defaultTag || runtime?.application.defaultTag,
      containerName: enabledTarget?.containerName,
    })
  }, [bindings, deliveryForm, runtime?.application.buildSources, runtime?.application.defaultTag])

  const activeEnvironment = environments.find((item) => item.applicationEnvironmentId === activeEnvironmentId) ?? environments[0]
  const workloads = activeEnvironment?.workloads ?? []
  const selectedDeliveryBinding = bindings.find((item) => item.applicationEnvironmentId === selectedDeliveryBindingId) ?? bindings[0]
  const enabledTargets = selectedDeliveryBinding?.targets?.filter((item) => item.enabled) ?? []
  const selectedDeliveryTarget = selectedDeliveryBinding?.targets?.find((item) => item.id === selectedTargetId) ?? enabledTargets[0] ?? selectedDeliveryBinding?.targets?.[0]
  const selectedBuildSource = runtime?.application.buildSources?.find((item) => item.id === selectedBuildSourceId)
    ?? selectedDeliveryBinding?.buildSource
    ?? runtime?.application.buildSources?.find((item) => item.isDefault)
    ?? runtime?.application.buildSources?.[0]
  const effectiveImageTag = selectedImageTag || selectedBuildSource?.defaultTag || runtime?.application.defaultTag || ''
  const deliverySignal = summarizeDeliveryBuildSignal([detail ?? {}, ...bindings])
  const runtimeValidationCount = runtimeValidationNodeCount(bindings)
  const gateSignal = summarizeDeliveryValidationSignal(bindings, { validationNodes: runtimeValidationCount })
  const runtimeTargetCount = bindings.reduce((sum, binding) => sum + (binding.targetCount || binding.targets?.length || 0), 0)
  const runtimeArtifactCount = countRuntimeArtifacts(detail, releaseBundleArtifactsQuery.data?.data, latestExecutionArtifactsQuery.data?.data)
  const validationNodeCount = workflowTemplateValidationNodeCount(selectedDeliveryBinding?.workflowTemplate)
  const bindingSummaryById = useMemo(
    () => Object.fromEntries(bindings.map((binding) => [binding.applicationEnvironmentId, binding])),
    [bindings],
  )
  const openApplicationEdit = () => {
    if (!runtime?.application) return
    managementState.setEditingApp(runtime.application)
    managementState.setBuildSources(runtime.application.buildSources ?? [])
    managementState.setAppModalVisible(true)
  }
  const openBindingCreate = () => {
    managementState.setEditingBinding(null)
    managementState.bindingForm.resetFields()
    managementState.setBindingModalVisible(true)
  }
  const openBindingEdit = (binding: ApplicationEnvironment) => {
    managementState.setEditingBinding(binding)
    managementState.setBindingModalVisible(true)
  }
  const triggerDeliveryAction = async (action: ApplicationDeliveryActionKind) => {
    try {
      const values = await deliveryForm.validateFields()
      deliveryActionMutation.mutate(buildDeliveryActionPayload(action, values))
    } catch {
      // antd Form has already marked the invalid fields.
    }
  }
  const buildDisabledReason = disabledReason([
    !selectedDeliveryBinding && '无环境绑定',
    !effectiveImageTag && '缺少 imageTag/defaultTag',
    !canTriggerBuild && '缺少构建权限',
  ])
  const deployDisabledReason = disabledReason([
    !selectedDeliveryBinding && '无环境绑定',
    !selectedDeliveryTarget && '无 target',
    !effectiveImageTag && '缺少 imageTag/defaultTag',
    !canTriggerRelease && '缺少发布权限',
  ])
  const buildDeployDisabledReason = disabledReason([
    !selectedDeliveryBinding && '无环境绑定',
    !selectedDeliveryTarget && '无 target',
    !selectedDeliveryBinding?.workflowTemplate && '无 workflow template',
    !effectiveImageTag && '缺少 imageTag/defaultTag',
    !canTriggerBuild && '缺少构建权限',
    !canTriggerWorkflow && '缺少工作流权限',
  ])
  const verifyDisabledReason = disabledReason([
    !selectedDeliveryBinding && '无环境绑定',
    !selectedDeliveryTarget && '无 target',
    !selectedDeliveryBinding?.workflowTemplate && '无 workflow template',
    validationNodeCount === 0 && '无验证节点',
    !canTriggerWorkflow && '缺少工作流权限',
  ])

  if (runtimeQuery.isLoading) {
    return <div className="soha-page"><ManagementState kind="loading" title="Loading..." /></div>
  }

  if (!runtime) {
    return <div className="soha-page"><ManagementState kind="not-found" description="Application not found" /></div>
  }

  const summaryBindings = bindings.slice(0, 4)
  const latestBuilds = latestBuildsQuery.data?.data ?? []
  const latestReleases = latestReleasesQuery.data?.data ?? []
  const latestWorkflows = latestWorkflowsQuery.data?.data ?? []

  return (
    <div className="soha-page">
      <ManagementDetailHeader
        title={runtime.application.name}
        description="围绕应用查看服务组件、容器、环境运行态和交付入口。"
        actions={<Button onClick={() => navigate('/applications')}>返回应用中心</Button>}
      />
      <div className="soha-application-runtime-service-summary">
        <Card className="soha-management-panel-card" size="small"><Text type="secondary">服务组件</Text><strong>{services.length}</strong></Card>
        <Card className="soha-management-panel-card" size="small"><Text type="secondary">容器</Text><strong>{services.reduce((sum, item) => sum + (item.containers?.length ?? 0), 0)}</strong></Card>
        <Card className="soha-management-panel-card" size="small"><Text type="secondary">环境</Text><strong>{environments.length}</strong></Card>
        <Card className="soha-management-panel-card" size="small"><Text type="secondary">运行目标</Text><strong>{environments.reduce((sum, item) => sum + (item.workloads?.length ?? 0), 0)}</strong></Card>
      </div>
      <div className="soha-application-runtime-delivery-summary">
        <Card className="soha-management-panel-card" size="small">
          <Text type="secondary">交付态势</Text>
          <div className="soha-application-runtime-delivery-summary__main">
            <Tag color={deliverySignal.color}>{deliverySignal.label}</Tag>
            <Text>{summarizeBuildRecord(detail?.latestBuild)}</Text>
          </div>
        </Card>
        <Card className="soha-management-panel-card" size="small">
          <Text type="secondary">门禁状态</Text>
          <div className="soha-application-runtime-delivery-summary__main">
            <Tag color={gateSignal.color}>{gateSignal.label}</Tag>
            <Text>{runtimeValidationCount} 个验证节点</Text>
          </div>
        </Card>
        <Card className="soha-management-panel-card" size="small">
          <Text type="secondary">候选版本</Text>
          <div className="soha-application-runtime-delivery-summary__main">
            <strong>{detail?.latestBundle?.version || '-'}</strong>
            <Text>{runtimeArtifactCount} 个交付物线索</Text>
          </div>
        </Card>
        <Card className="soha-management-panel-card" size="small">
          <Text type="secondary">环境矩阵</Text>
          <div className="soha-application-runtime-delivery-summary__main">
            <strong>{bindings.length}</strong>
            <Text>{runtimeTargetCount} 个发布目标</Text>
          </div>
        </Card>
      </div>
      <Card className="soha-application-delivery-actions soha-management-panel-card" title="交付操作">
        <Form form={deliveryForm} layout="vertical" size="middle" className="soha-application-delivery-actions__form">
          <div className="soha-application-delivery-actions__grid">
            <Form.Item name="applicationEnvironmentId" label="环境绑定" rules={[{ required: true, message: '请选择环境绑定' }]}>
              <Select
                options={bindings.map((binding) => ({
                  value: binding.applicationEnvironmentId,
                  label: binding.environmentName || binding.environmentKey || binding.environmentId,
                }))}
                onChange={(value) => {
                  const nextBinding = bindings.find((item) => item.applicationEnvironmentId === value)
                  const nextTarget = nextBinding?.targets?.find((item) => item.enabled) ?? nextBinding?.targets?.[0]
                  const nextSource = nextBinding?.buildSource ?? runtime.application.buildSources?.find((item) => item.isDefault) ?? runtime.application.buildSources?.[0]
                  deliveryForm.setFieldsValue({
                    targetId: nextTarget?.id,
                    buildSourceId: nextBinding?.buildSourceId || nextSource?.id,
                    imageTag: nextSource?.defaultTag || runtime.application.defaultTag,
                    containerName: nextTarget?.containerName,
                  })
                }}
              />
            </Form.Item>
            <Form.Item name="targetId" label="发布目标">
              <Select
                allowClear
                placeholder="选择 target"
                options={(selectedDeliveryBinding?.targets ?? []).map((target) => ({
                  value: target.id,
                  disabled: !target.enabled,
                  label: deliveryTargetSummary(target),
                }))}
              />
            </Form.Item>
            <Form.Item name="buildSourceId" label="构建来源">
              <Select allowClear options={serviceBuildSourceOptions} />
            </Form.Item>
            <Form.Item name="refType" label="Ref 类型">
              <Select options={REF_TYPE_OPTIONS} />
            </Form.Item>
            <Form.Item name="refName" label="分支 / Tag / Commit">
              <Input placeholder="main" />
            </Form.Item>
            <Form.Item name="imageTag" label="镜像 Tag">
              <Input placeholder={selectedBuildSource?.defaultTag || runtime.application.defaultTag || '必填'} />
            </Form.Item>
            <Form.Item name="releaseName" label="发布名称">
              <Input placeholder={effectiveImageTag || selectedDeliveryBinding?.applicationEnvironmentId || 'release'} />
            </Form.Item>
            <Form.Item name="containerName" label="容器">
              <Input placeholder={selectedDeliveryTarget?.containerName || '默认容器'} />
            </Form.Item>
          </div>
          <div className="soha-application-delivery-actions__footer">
            <Space wrap>
              <Tag>{selectedDeliveryBinding?.workflowTemplateName || selectedDeliveryBinding?.workflowTemplate?.name || '未绑定 workflow'}</Tag>
              <Tag>{selectedDeliveryBinding?.targetCount ?? 0} targets</Tag>
              <Tag>{validationNodeCount} 验证节点</Tag>
              {effectiveImageTag ? <Tag>imageTag {effectiveImageTag}</Tag> : <Tag color="warning">缺少 imageTag</Tag>}
            </Space>
            <Space wrap>
              <Tooltip title={buildDisabledReason || '触发构建'}>
                <Button icon={<CloudUploadOutlined />} disabled={!!buildDisabledReason} loading={deliveryActionMutation.isPending} onClick={() => void triggerDeliveryAction('build')}>构建</Button>
              </Tooltip>
              <Tooltip title={deployDisabledReason || '触发部署'}>
                <Button icon={<RocketOutlined />} disabled={!!deployDisabledReason} loading={deliveryActionMutation.isPending} onClick={() => void triggerDeliveryAction('deploy')}>部署</Button>
              </Tooltip>
              <Tooltip title={buildDeployDisabledReason || '通过 workflow template 编排'}>
                <Button type="primary" icon={<PlayCircleOutlined />} disabled={!!buildDeployDisabledReason} loading={deliveryActionMutation.isPending} onClick={() => void triggerDeliveryAction('build_deploy')}>构建并部署</Button>
              </Tooltip>
              <Tooltip title={verifyDisabledReason || '只运行验证节点'}>
                <Button icon={<SafetyCertificateOutlined />} disabled={!!verifyDisabledReason} loading={deliveryActionMutation.isPending} onClick={() => void triggerDeliveryAction('verify')}>运行验证</Button>
              </Tooltip>
            </Space>
          </div>
        </Form>
      </Card>
      <Tabs
        activeKey={activeTab}
        onChange={setActiveTab}
        items={[
          {
            key: 'overview',
            label: '总览',
            children: (
              <div className="soha-application-runtime-overview-grid">
                <Card className="soha-management-panel-card" title="最近执行">
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
                <Card className="soha-management-panel-card" title="环境概览">
                  <Space orientation="vertical" style={{ width: '100%' }} size={12}>
                    {summaryBindings.length > 0 ? summaryBindings.map((binding) => (
                      <div className="soha-application-runtime-binding-row" key={binding.applicationEnvironmentId}>
                        <div className="soha-application-runtime-binding-row__main">
                          <strong>{binding.environmentName || binding.environmentKey || binding.environmentId}</strong>
                          <Text type="secondary">{binding.workflowTemplate?.name || binding.workflowTemplateName || '未绑定工作流模板'}</Text>
                        </div>
                        <Space wrap>
                          <Tag>{summarizeBindingStatus(binding)}</Tag>
                          <Tag>{binding.targetCount} targets</Tag>
                          <ManagementIconButton
                            aria-label="查看绑定配置"
                            icon={<LinkOutlined />}
                            size="small"
                            tooltip="绑定配置"
                            onClick={() => setActiveTab('settings')}
                          />
                        </Space>
                      </div>
                    )) : <ManagementState bordered={false} compact description="尚未绑定任何环境" kind="not-configured" />}
                  </Space>
                </Card>
              </div>
            ),
          },
          {
            key: 'settings',
            label: '配置',
            children: (
              <div className="soha-application-runtime-settings-grid">
                <Card
                  className="soha-management-panel-card"
                  title="应用配置"
                  extra={managementState.canUpdateApplication ? <Button icon={<EditOutlined />} onClick={openApplicationEdit}>编辑应用</Button> : null}
                >
                  <Descriptions
                    column={1}
                    items={[
                      { key: 'key', label: '应用 Key', children: runtime.application.key || '-' },
                      { key: 'group', label: '分组', children: runtime.application.group || '-' },
                      { key: 'language', label: '语言', children: runtime.application.language || '-' },
                      { key: 'status', label: '状态', children: <StatusTag value={runtime.application.enabled ? 'enabled' : 'disabled'} /> },
                    ]}
                  />
                </Card>
                <DeliveryTable
                  title="构建来源"
                  rowKey="id"
                  pagination={false}
                  dataSource={runtime.application.buildSources ?? []}
                  columns={[
                    { title: '名称', dataIndex: 'name' },
                    { title: '类型', dataIndex: 'type', render: (_: unknown, record: BuildSource) => <Tag>{summarizeBuildSource(record)}</Tag> },
                    { title: '镜像', dataIndex: 'buildImage', render: (value: string) => value || '-' },
                    { title: '默认 Tag', dataIndex: 'defaultTag', render: (value: string) => value || '-' },
                    { title: '默认', dataIndex: 'isDefault', render: (value: boolean) => <StatusTag value={value ? 'enabled' : 'disabled'} /> },
                    { title: '启用', dataIndex: 'enabled', render: (value: boolean) => <StatusTag value={value ? 'enabled' : 'disabled'} /> },
                  ]}
                />
                <DeliveryTable
                  title="环境绑定"
                  actions={managementState.canManageBindings ? (
                    <Button type="primary" icon={<PlusOutlined />} onClick={openBindingCreate}>新建绑定</Button>
                  ) : null}
                  rowKey="id"
                  dataSource={managementState.filteredBindings}
                  loading={managementState.bindingsQuery.isLoading}
                  refreshing={managementState.bindingsQuery.isFetching}
                  onRefresh={() => void managementState.bindingsQuery.refetch()}
                  columns={[
                    { title: '环境', dataIndex: 'environmentId', render: (value: string, record: ApplicationEnvironment) => record.environmentKey || value },
                    { title: '构建来源', dataIndex: 'buildPolicy', render: (value: ApplicationEnvironment['buildPolicy']) => value?.sourceId || '-' },
                    { title: '发布流程模板', dataIndex: 'workflowTemplateId', render: (value?: string) => managementState.workflowTemplateMap[value || '']?.name || value || '-' },
                    { title: '发布目标', dataIndex: 'targets', render: (targets: ApplicationEnvironment['targets']) => renderBindingTargets(targets) },
                    { title: '资源选择器', dataIndex: 'resourceSelector', render: (value: ApplicationEnvironment['resourceSelector']) => renderSelectorLabels(value) },
                    { title: '最近状态', dataIndex: 'id', render: (value: string) => <StatusTag value={summarizeBindingStatus(bindingSummaryById[value])} /> },
                    {
                      title: '操作',
                      dataIndex: 'id',
                      fixed: 'right',
                      align: 'center',
                      width: 112,
                      render: (_: unknown, record: ApplicationEnvironment) => (
                        <Space className="soha-row-action-icons" size={2}>
                          <ManagementIconButton
                            aria-label="查看运行态"
                            icon={<ArrowRightOutlined />}
                            size="small"
                            tooltip="运行态"
                            onClick={() => {
                              setActiveEnvironmentId(record.id)
                              setActiveTab('environments')
                            }}
                          />
                          {managementState.canManageBindings ? (
                            <ManagementIconButton
                              aria-label="编辑绑定"
                              icon={<EditOutlined />}
                              size="small"
                              tooltip="编辑"
                              onClick={() => openBindingEdit(record)}
                            />
                          ) : null}
                          {managementState.canManageBindings ? (
                            <Popconfirm title="确认删除绑定？" onConfirm={() => managementState.deleteBindingMutation.mutate(record.id)} placement="topRight">
                              <ManagementIconButton
                                aria-label="删除绑定"
                                danger
                                icon={<DeleteOutlined />}
                                size="small"
                                tooltip="删除"
                              />
                            </Popconfirm>
                          ) : null}
                        </Space>
                      ),
                    },
                  ]}
                />
              </div>
            ),
          },
          {
            key: 'services',
            label: '服务组件',
            children: (
              <Card
                className="soha-management-panel-card"
                title="服务组件"
                extra={canManageServices ? <Button type="primary" icon={<PlusOutlined />} onClick={() => openServiceModal()}>新建服务</Button> : null}
              >
                {services.length > 0 ? (
                  <div className="soha-application-service-grid">
                    {services.map((service) => (
                      <Card
                        key={service.id}
                        size="small"
                        className="soha-application-service-card"
                        title={service.name}
                        extra={<StatusTag value={service.enabled ? 'enabled' : 'disabled'} />}
                        actions={canManageServices ? [
                          <ManagementIconButton
                            key="edit"
                            aria-label="编辑服务组件"
                            icon={<EditOutlined />}
                            size="small"
                            tooltip="编辑"
                            onClick={() => openServiceModal(service)}
                          />,
                          <Popconfirm key="delete" title="确认删除该服务组件？" onConfirm={() => deleteServiceMutation.mutate(service)}>
                            <ManagementIconButton
                              aria-label="删除服务组件"
                              danger
                              icon={<DeleteOutlined />}
                              size="small"
                              tooltip="删除"
                            />
                          </Popconfirm>,
                        ] : undefined}
                      >
                        <div className="soha-application-service-card__body">
                          <div className="soha-application-service-card__meta">
                            <Tag>{serviceKindLabel(service.serviceKind)}</Tag>
                            {service.ownerTeam ? <Tag>{service.ownerTeam}</Tag> : null}
                            {service.buildSourceId ? <Tag>{service.buildSourceId}</Tag> : null}
                          </div>
                          <Text type="secondary">{service.repositoryPath || '未配置服务仓库'}</Text>
                          <div className="soha-application-container-list">
                            {(service.containers ?? []).map((container) => (
                              <div className="soha-application-container-row" key={container.id || container.name}>
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
                  <ManagementState bordered={false} compact description="尚未配置服务组件。先把应用拆成服务和容器，后续 CI/CD DAG 才能按服务选择构建、测试和部署目标。" kind="not-configured" />
                )}
              </Card>
            ),
          },
          {
            key: 'environments',
            label: '环境矩阵',
            children: (
              <div className="soha-application-runtime-environment-stack">
                <Card className="soha-management-panel-card">
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
                <div className="soha-application-runtime-grid">
                  {workloads.length > 0 ? workloads.map((workload) => (
                    <Card
                      key={`${workload.clusterId}/${workload.namespace}/${workload.workloadName}`}
                      hoverable
                      className="soha-application-runtime-card"
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
                        <div className="soha-application-runtime-card__head">
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
                    <ManagementState className="soha-application-runtime-empty" bordered={false} compact description="当前环境下没有可显示的服务/Deployment" />
                  )}
                </div>
              </div>
            ),
          },
          {
            key: 'delivery',
            label: '交付物',
            children: (
              <div className="soha-application-runtime-delivery-grid">
                <Card className="soha-management-panel-card" title="Release Bundle">
                  <Descriptions column={1} items={[
                    { key: 'bundle', label: '当前 Bundle', children: summarizeReleaseBundle(detail?.latestBundle) },
                    { key: 'bundleArtifacts', label: 'Bundle 交付物', children: summarizeArtifacts(releaseBundleArtifactsQuery.data?.data) },
                    { key: 'task', label: '执行任务', children: summarizeExecutionTask(detail?.latestExecutionTask) },
                    { key: 'taskArtifacts', label: 'Task 交付物', children: summarizeArtifacts(latestExecutionArtifactsQuery.data?.data) },
                  ]} />
                </Card>
                <DeliveryTable
                  title="Build / Release / Workflow"
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
              </div>
            ),
          },
          {
            key: 'pipeline',
            label: '流水线',
            children: (
              <div className="soha-application-runtime-pipeline-grid">
                <Card className="soha-management-panel-card" title="DAG 模板">
                  <Space orientation="vertical" style={{ width: '100%' }} size={12}>
                    {bindings.length > 0 ? bindings.map((binding) => (
                      <div className="soha-application-runtime-binding-row" key={binding.applicationEnvironmentId}>
                        <div className="soha-application-runtime-binding-row__main">
                          <strong>{binding.environmentName || binding.environmentKey || binding.environmentId}</strong>
                          <Text type="secondary">{binding.workflowTemplate?.name || binding.workflowTemplateName || '未绑定工作流模板'}</Text>
                        </div>
                        <Space wrap>
                          <Tag>{workflowTemplateNodeCount(binding.workflowTemplate)} nodes</Tag>
                          <Tag>{binding.requiresApproval ? 'approval required' : 'no approval'}</Tag>
                        </Space>
                      </div>
                    )) : <ManagementState bordered={false} compact description="尚未绑定 CI/CD DAG 模板" kind="not-configured" />}
                  </Space>
                </Card>
                <DeliveryTable
                  title="最近工作流运行"
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
              </div>
            ),
          },
          {
            key: 'verification',
            label: '测试验证',
            children: (
              <div className="soha-application-runtime-verification-grid">
                <Card className="soha-management-panel-card" title="验证门禁">
                  <Descriptions column={1} items={[
                    { key: 'workflowTemplate', label: 'Workflow Template', children: detail?.bindings?.[0]?.workflowTemplate?.name || detail?.bindings?.[0]?.workflowTemplateName || '-' },
                    { key: 'workflowNodes', label: 'DAG 节点数', children: workflowTemplateNodeCount(detail?.bindings?.[0]?.workflowTemplate) },
                    { key: 'approval', label: '审批要求', children: detail?.bindings?.[0]?.requiresApproval ? '需要审批' : '无需审批' },
                    { key: 'releaseTarget', label: '当前目标', children: deliveryTargetSummary(detail?.bindings?.[0]?.targets?.[0]) },
                  ]} />
                </Card>
                <Card className="soha-management-panel-card" title="测试入口">
                  <Space orientation="vertical" style={{ width: '100%' }}>
                    <Button type="primary" onClick={() => setActiveTab('settings')} disabled={!bindings[0]?.applicationEnvironmentId}>查看绑定配置</Button>
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
          <div className="soha-application-service-form-grid">
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
              <Input placeholder="group/project" />
            </Form.Item>
            <Form.Item name="defaultBranch" label="默认分支">
              <Input placeholder="main" />
            </Form.Item>
          </div>
          <Form.Item name="description" label="描述">
            <Input.TextArea rows={2} />
          </Form.Item>

          <Form.List name="containers">
            {(fields, { add, remove }) => (
              <div className="soha-application-service-containers-editor">
                <div className="soha-application-service-containers-editor__head">
                  <Text strong>容器</Text>
                  <Button size="small" icon={<PlusOutlined />} onClick={() => add({ name: 'main' })}>添加容器</Button>
                </div>
                {fields.map((field) => (
                  <Card key={field.key} size="small" className="soha-application-service-container-editor">
                    <div className="soha-application-service-container-editor__grid">
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
                    <ManagementIconButton
                      aria-label="移除容器"
                      danger
                      icon={<MinusCircleOutlined />}
                      size="small"
                      tooltip="移除容器"
                      onClick={() => remove(field.name)}
                    />
                  </Card>
                ))}
              </div>
            )}
          </Form.List>

          <div className="soha-form-actions">
            <Button onClick={() => setServiceModalVisible(false)}>取消</Button>
            <Button htmlType="submit" type="primary" loading={createServiceMutation.isPending || updateServiceMutation.isPending}>
              保存
            </Button>
          </div>
        </Form>
      </Modal>
      <ApplicationManagementModals state={managementState} />
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
    return <div className="soha-page"><ManagementState kind="loading" title="Loading..." /></div>
  }
  if (!detail || !deployment) {
    return <div className="soha-page"><ManagementState kind="not-found" description="未找到运行详情" /></div>
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
        <DeliveryTable
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
        <div className="soha-application-runtime-network">
          <DeliveryTable
            title="Services"
            columns={[
              { title: '名称', dataIndex: 'name' },
              { title: '类型', dataIndex: 'type' },
              { title: 'Cluster IP', dataIndex: 'clusterIp', render: (value?: string) => value || '-' },
            ]}
            dataSource={serviceList}
            rowKey="name"
            pageSize={10}
          />
          <DeliveryTable
            title="Ingresses"
            columns={[
              { title: '名称', dataIndex: 'name' },
              { title: '主机', dataIndex: 'hosts', render: (value?: string[]) => (value ?? []).join(', ') || '-' },
              { title: '后端', dataIndex: 'backendServices', render: (value?: string[]) => (value ?? []).join(', ') || '-' },
            ]}
            dataSource={ingressList}
            rowKey="name"
            pageSize={10}
          />
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
        <ManagementState bordered={false} compact kind="select-scope" description="请选择一个 Pod" />
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
        <ManagementState bordered={false} compact kind="select-scope" description="请选择一个 Pod" />
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
    <div className="soha-page">
      <ManagementDetailHeader
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
