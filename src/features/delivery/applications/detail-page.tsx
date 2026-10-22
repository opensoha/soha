import { ManifestDeploymentState } from './manifest-deployment-state'
import { WorkflowProgress, WorkloadProgress } from './delivery-progress'
import { EnvironmentNotice } from './environment-notice'
import { lazy, Suspense, useCallback, useEffect, useMemo, useRef, useState } from 'react'

import './styles.css'
import './workspace.css'
import {
  Alert,
  App,
  Button,
  Card,
  Collapse,
  Descriptions,
  Drawer,
  Form,
  Input,
  InputNumber,
  Modal,
  Popconfirm,
  Progress,
  Radio,
  Select,
  Space,
  Tabs,
  Tooltip,
  Typography,
  type FormInstance,
} from 'antd'
import {
  ArrowRightOutlined,
  DeleteOutlined,
  DeploymentUnitOutlined,
  EditOutlined,
  LinkOutlined,
  PlayCircleOutlined,
  PlusOutlined,
  SettingOutlined,
} from '@ant-design/icons'
import { useMutation, useQueries, useQuery, useQueryClient } from '@tanstack/react-query'
import { Link, useNavigate, useParams, useSearchParams } from 'react-router-dom'
import {
  ManagementIconButton,
  ManagementRefreshButton,
  ManagementState,
} from '@/components/management-list'
import { visuallyHiddenModalTitleStyle } from '@/components/modal-styles'
import { OverviewMetricCard, OverviewSectionBar } from '@/components/overview-visuals'
import { analyzeReleaseDagDefinition } from '@/components/release-flow-dag-definition'
import { MutationFeedback, ReadFeedback } from '@/components/request-feedback'
import { readableQueryData, writeOutcomeUnknown } from '@/utils/query-feedback'
import { MetadataTag, StatusTag } from '@/components/status-tag'
import { hasPermission, usePermissionSnapshot } from '@/features/auth'
import { ScopeGrantManager } from '@/features/access'
import { useAIPageContext } from '@/features/copilot'
import { useClusterCapabilityForCluster } from '@/features/platform'
import { isApiError } from '@/services/api-error'
import { localeText, useI18n } from '@/i18n'
import { TemplateUpdateEntry } from '../template-updates/entry'
import { workflowTemplateValidationNodeCount } from '../delivery-status'
import { DeliveryTable } from '../delivery-table'
import {
  ApplicationCenterModals,
  summarizeBuildSource,
  useApplicationCenterState,
} from '../application-center-model'
import { deliveryMutations } from '../mutations'
import { deliveryApi } from '../api'
import { ManifestLibraryWorkspace } from '../manifests'
import { deliveryQueries } from '../queries'
import { deliveryKeys } from '../keys'
import { buildSourceRepositoryBindings as buildRepositoryBindings } from '../service-setup'
import type {
  ApplicationDeliveryActionKind,
  ApplicationEnvironment,
  ApplicationRuntimeEnvironment,
  ApplicationRuntimeWorkload,
  ApplicationServiceComponent,
  BuildRepositoryBinding,
  BuildRepositoryRefInput,
  BuildSource,
  DeliveryApplication,
  DeliveryApplicationBindingSummary,
  DeliveryRepository,
  DeliveryPlan,
  DeliveryPlanConfirmResult,
  DeliveryPlanRequest,
  WorkflowTemplate,
} from '../types'
import {
  APPLICATION_SETTINGS_KEYS,
  applicationWorkspacePath,
  APPLICATION_WORKSPACE_LABELS,
  APPLICATION_SETTINGS_NAV_ITEMS,
} from './workspace-navigation'

import { BuildSourceFields } from './build-source-fields'
import { ServiceEditor, SERVICE_KIND_OPTIONS } from './service-editor'
import { RepositoryFields } from './repository-fields'
import { environmentRuntimeStatus } from './runtime-status'
import { formatDateTime } from '@/utils/time'
import { ApplicationEnvironmentNavigation } from '../navigation'

const ServiceDeliveryForm = lazy(() =>
  import('./service-delivery-form').then((module) => ({ default: module.ServiceDeliveryForm })),
)
const ImportedSourceBrowser = lazy(async () => {
  const module = await import('./imported-source-browser')
  return { default: module.ImportedSourceBrowser }
})
const ServiceResourceModal = lazy(async () => {
  const module = await import('./service-resource-modal')
  return { default: module.ServiceResourceModal }
})
const ServiceRuntimeActions = lazy(async () => {
  const module = await import('../runtime/service-pod-workspace')
  return { default: module.ServiceRuntimeActions }
})
const ServiceRuntimeSummary = lazy(async () => {
  const module = await import('../runtime/service-pod-workspace')
  return { default: module.ServiceRuntimeSummary }
})
const ServicePodWorkspace = lazy(async () => {
  const module = await import('../runtime/service-pod-workspace')
  return { default: module.ServicePodWorkspace }
})

const { Text } = Typography

type RepositoryFormValues = {
  id?: string
  name: string
  provider: 'gitlab' | 'git'
  protocol: 'https' | 'ssh'
  url: string
  path: string
  gitlabProjectId?: string
  credentialRef?: string
  defaultBranch: string
}

type BuildSourceFormValues = Omit<BuildSource, 'id'> & { id?: string }

type DeliveryActionFormValues = {
  manifestRevision?: number
  applicationEnvironmentId?: string
  targetId?: string
  buildSourceId?: string
  refType?: 'branch' | 'tag' | 'commit'
  refName?: string
  repositoryRefs?: BuildRepositoryRefInput[]
  imageTag?: string
  releaseName?: string
  containerName?: string
}

type WorkflowCreateFormValues = {
  name: string
  applicationEnvironmentId: string
  source: 'blank' | 'template'
  templateId?: string
}

const REF_TYPE_OPTIONS = [
  { value: 'branch', label: 'Branch' },
  { value: 'tag', label: 'Tag' },
  { value: 'commit', label: 'Commit' },
]

function buildRepositoryRefs(
  source: BuildSource | undefined,
  repositories: DeliveryRepository[],
): BuildRepositoryRefInput[] {
  return buildRepositoryBindings(source).map((binding) => {
    const repository = repositories.find((item) => item.id === binding.repositoryId)
    return {
      repositoryId: binding.repositoryId,
      refType: 'branch',
      refName: binding.defaultBranch || repository?.defaultBranch || 'main',
    }
  })
}

const DELIVERY_ACTION_LABELS: Record<ApplicationDeliveryActionKind, string> = {
  build: '构建',
  deploy: '部署',
  build_deploy: '运行工作流',
  workflow: '工作流',
  verify: '测试',
  rollback: '回滚',
}

function serviceKindLabel(value?: string) {
  return SERVICE_KIND_OPTIONS.find((item) => item.value === value)?.label ?? value ?? '-'
}

function summarizeBindingStatus(binding?: DeliveryApplicationBindingSummary | null) {
  return (
    binding?.latestRelease?.status ||
    binding?.latestWorkflow?.status ||
    binding?.latestBuild?.status ||
    binding?.latestExecutionTask?.status ||
    binding?.latestBundle?.status ||
    'unknown'
  )
}

function applicationWorkflowDesignPath(
  applicationId: string,
  values: {
    applicationEnvironmentId: string
    name?: string
    source?: WorkflowCreateFormValues['source']
    templateId?: string
    templateVersion?: number
  },
) {
  const search = new URLSearchParams({ bindingId: values.applicationEnvironmentId })
  if (values.name) search.set('name', values.name)
  if (values.source) search.set('source', values.source)
  if (values.templateId) search.set('templateId', values.templateId)
  if (values.templateVersion) search.set('templateVersion', String(values.templateVersion))
  return `/applications/${encodeURIComponent(applicationId)}/workflows/design?${search}`
}

function renderWorkflowTemplateAnalysisTags(
  template: WorkflowTemplate | undefined,
  requiresApproval?: boolean,
  hasTemplateRef = false,
) {
  if (!template) {
    if (hasTemplateRef) return <StatusTag value="error" label="模板缺失" />
    return <StatusTag value="warning" label="无模板" />
  }
  const analysis = analyzeReleaseDagDefinition(template.definition)
  return (
    <Space wrap>
      <MetadataTag label={`${analysis.nodeCount} 个节点`} />
      <StatusTag
        value={analysis.validationNodeCount > 0 ? 'success' : 'warning'}
        label={analysis.validationNodeCount > 0 ? '有测试节点' : '无测试节点'}
      />
      <StatusTag
        value={analysis.rollbackNodeCount > 0 ? 'success' : 'warning'}
        label={analysis.rollbackNodeCount > 0 ? '有回滚节点' : '无回滚节点'}
      />
      {analysis.approvalNodeCount > 0 || requiresApproval ? (
        <StatusTag value="warning" label="包含审批" />
      ) : (
        <StatusTag value="default" label="无审批" />
      )}
      <StatusTag
        value={analysis.isReleaseDagCompatible ? 'success' : 'error'}
        label={analysis.isReleaseDagCompatible ? 'DAG 正常' : 'DAG 异常'}
      />
    </Space>
  )
}

function renderWorkflowTemplateHealth(binding: DeliveryApplicationBindingSummary) {
  return renderWorkflowTemplateAnalysisTags(
    binding.workflowTemplate,
    binding.requiresApproval,
    Boolean(binding.workflowTemplateId),
  )
}

function renderEnvironmentBindingWorkflowHealth(
  record: ApplicationEnvironment,
  summary: DeliveryApplicationBindingSummary | undefined,
  workflowTemplateMap: Record<string, WorkflowTemplate>,
) {
  const workflowTemplateId = summary?.workflowTemplateId || record.workflowTemplateId || ''
  const template = summary?.workflowTemplate ?? workflowTemplateMap[workflowTemplateId]
  const requiresApproval = summary?.requiresApproval ?? record.releasePolicy?.requiresApproval
  return renderWorkflowTemplateAnalysisTags(template, requiresApproval, Boolean(workflowTemplateId))
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

function deliveryTargetSummary(
  target?: {
    clusterId: string
    namespace: string
    workloadKind: string
    workloadName: string
    containerName?: string
    targetKind?: string
    executorKind?: string
    groupKey?: string
    waveKey?: string
    regionKey?: string
    configRef?: string
  },
  serviceName?: string,
) {
  if (!target) return '-'
  const parts = [target.clusterId, target.namespace, target.workloadName]
  return [serviceName, target.workloadKind, parts.join(' / '), target.containerName]
    .filter(Boolean)
    .join(' · ')
}

function RepositoryRefField({
  form,
  index,
  binding,
  repository,
}: {
  form: FormInstance<DeliveryActionFormValues>
  index: number
  binding: BuildRepositoryBinding
  repository?: DeliveryRepository
}) {
  const refType = Form.useWatch(['repositoryRefs', index, 'refType'], form) ?? 'branch'
  const gitProjectId = repository?.gitlabProjectId ?? ''
  const branchesQuery = useQuery(
    deliveryQueries.repositories.gitBranches(
      { projectId: gitProjectId },
      Boolean(gitProjectId && refType === 'branch'),
    ),
  )
  const tagsQuery = useQuery(
    deliveryQueries.repositories.gitTags(
      { projectId: gitProjectId },
      Boolean(gitProjectId && refType === 'tag'),
    ),
  )
  const commitsQuery = useQuery(
    deliveryQueries.repositories.gitCommits(
      { projectId: gitProjectId, page: 1, limit: 50 },
      Boolean(gitProjectId && refType === 'commit'),
    ),
  )
  const referenceOptions =
    refType === 'branch'
      ? (branchesQuery.data ?? []).map((item) => ({ value: item.name, label: item.name }))
      : refType === 'tag'
        ? (tagsQuery.data ?? []).map((item) => ({ value: item.name, label: item.name }))
        : (commitsQuery.data?.items ?? []).map((item) => ({
            value: item.id,
            label: `${item.shortId} ${item.title}`,
          }))
  const loading =
    refType === 'branch'
      ? branchesQuery.isFetching
      : refType === 'tag'
        ? tagsQuery.isFetching
        : commitsQuery.isFetching

  return (
    <div className="soha-application-delivery-repository-ref">
      <Form.Item name={['repositoryRefs', index, 'repositoryId']} hidden>
        <Input />
      </Form.Item>
      <div className="soha-application-delivery-repository-ref__identity">
        <Text strong>{repository?.name || binding.repositoryId}</Text>
        <Text type="secondary">
          {binding.checkoutPath || (index === 0 ? '.' : `repository-${index + 1}`)}
        </Text>
      </div>
      <Form.Item
        name={['repositoryRefs', index, 'refType']}
        label="版本类型"
        rules={[{ required: true, message: '请选择版本类型' }]}
      >
        <Select
          options={REF_TYPE_OPTIONS.filter(
            (item) => item.value !== 'commit' || binding.allowCommitSelection,
          )}
        />
      </Form.Item>
      <Form.Item
        name={['repositoryRefs', index, 'refName']}
        label="分支 / Tag / Commit"
        rules={[{ required: true, message: '请选择或输入版本' }]}
      >
        {gitProjectId ? (
          <Select
            allowClear
            showSearch={{ optionFilterProp: 'label' }}
            loading={loading}
            options={referenceOptions}
          />
        ) : (
          <Input placeholder={binding.defaultBranch || repository?.defaultBranch || 'main'} />
        )}
      </Form.Item>
    </div>
  )
}

function buildDeliveryActionPayload(
  action: ApplicationDeliveryActionKind,
  values: DeliveryActionFormValues,
) {
  const repositoryRefs = values.repositoryRefs?.filter(
    (item) => item.repositoryId && item.refType && item.refName,
  )
  const primaryRepositoryRef = repositoryRefs?.[0]
  return {
    action,
    applicationEnvironmentId: values.applicationEnvironmentId ?? '',
    targetId: values.targetId,
    buildSourceId: values.buildSourceId,
    refType: primaryRepositoryRef?.refType ?? values.refType,
    refName: primaryRepositoryRef?.refName ?? values.refName,
    repositoryRefs: repositoryRefs?.length ? repositoryRefs : undefined,
    imageTag: values.imageTag,
    releaseName: values.releaseName,
    containerName: values.containerName,
  }
}

function buildDeliveryPlanPayload(
  applicationId: string,
  action: ApplicationDeliveryActionKind,
  values: DeliveryActionFormValues,
): DeliveryPlanRequest {
  if (action === 'deploy') {
    return {
      applicationId,
      action,
      applicationEnvironmentId: values.applicationEnvironmentId ?? '',
      targetId: values.targetId,
      manifestRevision: values.manifestRevision,
      source: 'manual',
    }
  }
  return {
    ...buildDeliveryActionPayload(action, values),
    applicationId,
    source: 'manual',
    reason: `${DELIVERY_ACTION_LABELS[action]} from application runtime page`,
  }
}

function disabledReason(reasons: Array<string | false | undefined>) {
  return reasons.find(Boolean) || ''
}

export function ApplicationDetailPage() {
  const { localeCode } = useI18n()
  const settingsLabel = localeText(localeCode, '应用设置', 'Application settings')
  const { applicationId } = useParams()
  const navigate = useNavigate()
  const [searchParams, setSearchParams] = useSearchParams()
  const { message } = App.useApp()
  const queryClient = useQueryClient()
  const requestedTab = searchParams.get('tab')
  const requestedLaunch = searchParams.get('launch')
  const requestedBuildSourceId = searchParams.get('buildSourceId') || ''
  const requestedEnvironmentId = searchParams.get('applicationEnvironmentId')?.trim() ?? ''
  const focusedServiceId = searchParams.get('serviceId')?.trim() ?? ''
  const requestedServiceTab = searchParams.get('serviceTab') || 'pods'
  const serviceTab = ['pods', 'metrics', 'related-resources', 'build', 'resources'].includes(
    requestedServiceTab,
  )
    ? requestedServiceTab
    : 'pods'
  const requestedSettingsKey =
    searchParams.get('settings') ||
    (requestedTab &&
    ['overview', 'capabilities', ...APPLICATION_SETTINGS_KEYS].includes(requestedTab)
      ? requestedTab
      : '')
  const settingsKey = requestedSettingsKey
    ? APPLICATION_SETTINGS_KEYS.includes(requestedSettingsKey)
      ? requestedSettingsKey
      : 'application'
    : ''
  const activeTab = requestedTab === 'delivery' ? 'delivery' : 'services'
  const [serviceModalVisible, setServiceModalVisible] = useState(false)
  const [serviceSearch, setServiceSearch] = useState('')
  const [serviceKindFilter, setServiceKindFilter] = useState('all')
  const [editingService, setEditingService] = useState<ApplicationServiceComponent | null>(null)
  const [repositoryModalVisible, setRepositoryModalVisible] = useState(false)
  const [editingRepositoryId, setEditingRepositoryId] = useState('')
  const [repositoryBindingTarget, setRepositoryBindingTarget] = useState<number | null>(null)
  const [buildSourceModalVisible, setBuildSourceModalVisible] = useState(false)
  const [editingBuildSourceId, setEditingBuildSourceId] = useState('')
  const [buildSourceDraft, setBuildSourceDraft] = useState<BuildSourceFormValues | null>(null)
  const [buildSourceApplication, setBuildSourceApplication] = useState<DeliveryApplication | null>(
    null,
  )
  const [workflowCreateModalVisible, setWorkflowCreateModalVisible] = useState(false)
  const [deliveryActionModalVisible, setDeliveryActionModalVisible] = useState(false)
  const [deliveryActionKind, setDeliveryActionKind] = useState<'build' | 'build_deploy' | 'deploy'>(
    'build_deploy',
  )
  const [deliveryPlanModalVisible, setDeliveryPlanModalVisible] = useState(false)
  const [pendingDeliveryPlan, setPendingDeliveryPlan] = useState<DeliveryPlan | null>(null)
  const [confirmedDeliveryPlan, setConfirmedDeliveryPlan] =
    useState<DeliveryPlanConfirmResult | null>(null)
  const [repositoryForm] = Form.useForm<RepositoryFormValues>()
  const [buildSourceForm] = Form.useForm<BuildSourceFormValues>()
  const [workflowCreateForm] = Form.useForm<WorkflowCreateFormValues>()
  const [deliveryForm] = Form.useForm<DeliveryActionFormValues>()
  const workflowCreateSource = Form.useWatch('source', workflowCreateForm)
  const workflowCreateEnvironmentId = Form.useWatch('applicationEnvironmentId', workflowCreateForm)
  const setActiveTab = (tab: string, environmentId?: string) => {
    const next = new URLSearchParams(searchParams)
    next.set('tab', tab)
    if (environmentId) next.set('applicationEnvironmentId', environmentId)
    next.delete('settings')
    next.delete('launch')
    next.delete('buildSourceId')
    if (tab !== 'delivery') {
      next.delete('buildId')
      next.delete('releaseId')
      next.delete('workflowRunId')
    }
    setSearchParams(next)
  }
  const setFocusedService = (
    serviceId?: string,
    nextServiceTab = 'pods',
    nextSettings?: string,
  ) => {
    const next = new URLSearchParams(searchParams)
    next.delete('pod')
    next.delete('podCluster')
    next.delete('podNamespace')
    next.delete('tool')
    next.delete('container')
    next.delete('workload')
    next.delete('metricPod')
    next.delete('metricCluster')
    next.delete('metricNamespace')
    next.delete('metricRange')
    if (serviceId) {
      next.delete('settings')
      next.set('tab', 'services')
      next.set('serviceId', serviceId)
      next.set('serviceTab', nextServiceTab)
      if (nextSettings) next.set('settings', nextSettings)
      if (activeEnvironmentId) next.set('applicationEnvironmentId', activeEnvironmentId)
    } else {
      next.delete('serviceId')
      next.delete('serviceTab')
    }
    setSearchParams(next)
  }
  const permissionSnapshotQuery = usePermissionSnapshot()
  const runtimeQuery = useQuery(
    deliveryQueries.applications.runtime(
      applicationId ?? '',
      Boolean(applicationId),
      ['services', 'delivery'].includes(activeTab) ? 5_000 : false,
    ),
  )
  const detailQuery = useQuery(
    deliveryQueries.applications.detail(
      applicationId ?? '',
      Boolean(applicationId),
      activeTab === 'delivery' ? 5_000 : false,
    ),
  )
  const servicesQuery = useQuery(
    deliveryQueries.applications.services(applicationId ?? '', Boolean(applicationId)),
  )
  const repositoriesQuery = useQuery(
    deliveryQueries.repositories.list({ applicationId }, Boolean(applicationId)),
  )
  const runtime = readableQueryData(runtimeQuery)
  const detail = readableQueryData(detailQuery)
  const accessDenied = [runtimeQuery, detailQuery].some(
    (query) => isApiError(query.error) && [401, 403].includes(query.error.status),
  )
  const application = accessDenied ? undefined : (runtime?.application ?? detail?.application)
  const runtimePending = !runtime && runtimeQuery.isLoading
  const runtimeUnavailable = !runtime && runtimeQuery.isError
  const managementState = useApplicationCenterState({
    currentApplication: application,
    loadApplications: false,
    loadWorkflowTemplates: workflowCreateModalVisible && workflowCreateSource === 'template',
    loadClusters: activeTab === 'services',
  })
  const permissionSnapshot = permissionSnapshotQuery.data?.data
  const canCreateService = hasPermission(permissionSnapshot, 'delivery.application-services.create')
  const canUpdateService = hasPermission(permissionSnapshot, 'delivery.application-services.update')
  const canDeleteService = hasPermission(permissionSnapshot, 'delivery.application-services.delete')
  const canUpdateApplicationEnvironment = hasPermission(
    permissionSnapshot,
    'delivery.application-environments.update',
  )
  const canManageRepositories = managementState.canUpdateApplication
  const canViewBuilds = hasPermission(permissionSnapshot, 'delivery.applications.view')
  const canViewWorkflows = hasPermission(permissionSnapshot, 'delivery.workflows.view')
  const canTriggerBuild = hasPermission(
    permissionSnapshotQuery.data?.data,
    'delivery.builds.trigger',
  )
  const canTriggerWorkflow = hasPermission(
    permissionSnapshotQuery.data?.data,
    'delivery.workflows.trigger',
  )
  const canTriggerRelease = hasPermission(
    permissionSnapshotQuery.data?.data,
    'delivery.releases.trigger',
  )
  const canApproveDelivery = hasPermission(
    permissionSnapshotQuery.data?.data,
    'delivery.application-environments.approve',
  )
  const canViewScopeGrants = hasPermission(permissionSnapshot, 'access.scope-grants.view')
  const environmentCatalogQuery = useQuery(deliveryQueries.environmentCatalog.list())
  const catalogEnvironment = (id?: string) =>
    environmentCatalogQuery.isError
      ? undefined
      : environmentCatalogQuery.data?.find((item) => item.id === id)
  const environments = runtime?.environments ?? []
  const activeEnvironment = requestedEnvironmentId
    ? environments.find((item) => item.applicationEnvironmentId === requestedEnvironmentId)
    : environments[0]
  const activeEnvironmentId = activeEnvironment?.applicationEnvironmentId ?? ''
  useEffect(() => {
    if (requestedEnvironmentId || !activeEnvironmentId) return
    const next = new URLSearchParams(searchParams)
    next.set('applicationEnvironmentId', activeEnvironmentId)
    setSearchParams(next, { replace: true })
  }, [requestedEnvironmentId, activeEnvironmentId, searchParams, setSearchParams])
  const planBinding = managementState.filteredBindings.find(
    (item) => item.id === pendingDeliveryPlan?.applicationEnvironmentId,
  )
  const planProduction = catalogEnvironment(planBinding?.environmentId)?.isProduction
  const invalidEnvironment = Boolean(requestedEnvironmentId && runtime && !activeEnvironment)
  const services = servicesQuery.data ?? runtime?.services ?? []
  const servicesPending = !servicesQuery.data && !runtime?.services && servicesQuery.isLoading
  const servicesUnavailable = !servicesQuery.data && !runtime?.services && servicesQuery.isError
  const serviceRecordsState =
    servicesPending || servicesUnavailable ? (
      <ManagementState
        compact
        bordered={false}
        kind={servicesPending ? 'loading' : 'error'}
        title={servicesPending ? '正在加载服务档案' : '服务档案加载失败'}
        description="运行实例仍可查看；服务配置将在档案恢复后显示。"
        actions={
          servicesUnavailable ? (
            <Button onClick={() => void servicesQuery.refetch()}>重试服务档案</Button>
          ) : undefined
        }
      />
    ) : null
  const repositories = repositoriesQuery.data ?? []
  const bindings = detail?.bindings ?? []
  const fixedScopeGrantApplication = useMemo(
    () =>
      application
        ? {
            businessLineId: application.businessLineId || application.group || 'default',
            id: application.id,
            name: application.name,
          }
        : undefined,
    [application?.businessLineId, application?.group, application?.id, application?.name],
  )
  const selectedDeliveryBindingId = Form.useWatch('applicationEnvironmentId', deliveryForm)
  const selectedTargetId = Form.useWatch('targetId', deliveryForm)
  const selectedBuildSourceId = Form.useWatch('buildSourceId', deliveryForm)
  const selectedImageTag = Form.useWatch('imageTag', deliveryForm)
  useAIPageContext({
    sourceWorkbench: 'delivery',
    sourceTitle: detail?.application?.name ? `应用 ${detail.application.name}` : '应用详情',
    entityKind: 'delivery.application',
    entityName: detail?.application?.name ?? detail?.application?.key ?? applicationId,
    applicationId,
    visibleFilters: {
      tab: activeTab,
      activeEnvironmentId,
      focusedServiceId,
    },
    pinnedData: {
      environmentCount: environments.length,
      serviceCount: services.length,
      bindingCount: bindings.length,
    },
  })
  const serviceBuildSourceOptions = useMemo(
    () =>
      (application?.buildSources ?? []).map((item) => ({
        value: item.id,
        label: `${item.name} · ${summarizeBuildSource(item)}`,
      })),
    [application?.buildSources],
  )
  const populateDeliveryBinding = useCallback(
    (bindingId?: string, sourceId?: string) => {
      const binding = bindingId
        ? bindings.find((item) => item.applicationEnvironmentId === bindingId)
        : bindings[0]
      const target = binding?.targets?.find((item) => item.enabled) ?? binding?.targets?.[0]
      const source =
        application?.buildSources?.find((item) => item.id === sourceId) ??
        binding?.buildSource ??
        application?.buildSources?.find((item) => item.isDefault) ??
        application?.buildSources?.[0]
      const repositoryRefs = buildRepositoryRefs(source, repositories)
      const primaryRepositoryRef = repositoryRefs[0]
      deliveryForm.setFieldsValue({
        applicationEnvironmentId: binding?.applicationEnvironmentId,
        targetId: target?.id,
        buildSourceId: sourceId || binding?.buildSourceId || source?.id,
        refType:
          primaryRepositoryRef?.refType ||
          (binding?.buildPolicy?.refType as DeliveryActionFormValues['refType']) ||
          'branch',
        refName: primaryRepositoryRef?.refName || binding?.buildPolicy?.refValue || 'main',
        repositoryRefs,
        imageTag: source?.defaultTag || application?.defaultTag,
        containerName: target?.containerName,
        manifestRevision: undefined,
      })
    },
    [bindings, deliveryForm, repositories, application?.buildSources, application?.defaultTag],
  )

  useEffect(() => {
    if (!applicationId || managementState.selectedApplicationId === applicationId) return
    managementState.setSelectedApplicationId(applicationId)
  }, [applicationId, managementState])
  const createDeliveryPlanOptions = deliveryMutations.plans.create(queryClient)
  const directBuildMutation = useMutation({
    mutationFn: deliveryApi.builds.trigger,
    onSuccess: (build) => {
      setDeliveryActionModalVisible(false)
      void queryClient.invalidateQueries({ queryKey: deliveryKeys.builds.all })
      navigate('/builds/' + encodeURIComponent(build.id))
    },
  })
  const createDeliveryPlanMutation = useMutation({
    ...createDeliveryPlanOptions,
    onSuccess: (plan, variables, onMutateResult, context) => {
      void createDeliveryPlanOptions.onSuccess?.(plan, variables, onMutateResult, context)
      setDeliveryActionModalVisible(false)
      setPendingDeliveryPlan(plan)
      setConfirmedDeliveryPlan(null)
      setDeliveryPlanModalVisible(true)
      message.success('交付计划已生成')
    },
  })

  const confirmDeliveryPlanOptions = deliveryMutations.plans.confirm(queryClient)
  const confirmDeliveryPlanMutation = useMutation({
    ...confirmDeliveryPlanOptions,
    onSuccess: (result, variables, onMutateResult, context) => {
      void confirmDeliveryPlanOptions.onSuccess?.(result, variables, onMutateResult, context)
      setPendingDeliveryPlan(result.plan)
      setConfirmedDeliveryPlan(result.plan.status === 'confirmed' ? result : null)
      message.success(
        result.plan.status === 'waiting_approval'
          ? '交付计划已提交审批'
          : `${DELIVERY_ACTION_LABELS[result.plan.action]}已触发`,
      )
    },
  })
  const approvalOptions = deliveryMutations.plans.approval(queryClient)
  const approvalMutation = useMutation({
    ...approvalOptions,
    onSuccess: (plan, variables, onMutateResult, context) => {
      void approvalOptions.onSuccess?.(plan, variables, onMutateResult, context)
      setPendingDeliveryPlan(plan)
      message.success(
        variables.action === 'approve' ? '交付计划已批准，可再次确认执行' : '交付计划已拒绝',
      )
    },
  })

  const appConfigSubmitting = useRef(false)
  const deliverySubmitting = useRef(false)
  const planSubmitting = useRef(false)
  const deliveryUnknown =
    writeOutcomeUnknown(directBuildMutation.error) ||
    writeOutcomeUnknown(createDeliveryPlanMutation.error)
  const planLocked =
    confirmDeliveryPlanMutation.isPending ||
    approvalMutation.isPending ||
    writeOutcomeUnknown(confirmDeliveryPlanMutation.error) ||
    writeOutcomeUnknown(approvalMutation.error)
  const planRead = useQuery({
    ...deliveryQueries.plans.detail(pendingDeliveryPlan?.id || ''),
    enabled: false,
  })
  const actOnPlan = async (action: 'confirm' | 'approve' | 'reject') => {
    if (planSubmitting.current || planLocked || !pendingDeliveryPlan) return
    if (action !== 'confirm' && !canApproveDelivery) return
    planSubmitting.current = true
    try {
      if (action === 'confirm')
        await confirmDeliveryPlanMutation.mutateAsync(pendingDeliveryPlan.id)
      else await approvalMutation.mutateAsync({ id: pendingDeliveryPlan.id, action })
    } catch {
      /* Keep the request and plan visible for reconciliation. */
    } finally {
      planSubmitting.current = false
    }
  }

  const deleteServiceOptions = deliveryMutations.applications.deleteService(queryClient)
  const deleteServiceMutation = useMutation({
    ...deleteServiceOptions,
    onSuccess: (result, variables, onMutateResult, context) => {
      void deleteServiceOptions.onSuccess?.(result, variables, onMutateResult, context)
      message.success('服务组件已删除')
    },
    onError: (err: Error) => message.error(err.message),
  })

  const createRepositoryOptions = deliveryMutations.repositories.create(queryClient)
  const closeRepositoryModal = () => {
    setRepositoryModalVisible(false)
    setEditingRepositoryId('')
    setRepositoryBindingTarget(null)
    repositoryForm.resetFields()
  }
  const createRepositoryMutation = useMutation({
    ...createRepositoryOptions,
    onSuccess: (result, variables, onMutateResult, context) => {
      void createRepositoryOptions.onSuccess?.(result, variables, onMutateResult, context)
      if (repositoryBindingTarget !== null) {
        buildSourceForm.setFieldValue(
          ['config', 'repositoryBindings', repositoryBindingTarget, 'repositoryId'],
          result.id,
        )
        buildSourceForm.setFieldValue(
          ['config', 'repositoryBindings', repositoryBindingTarget, 'defaultBranch'],
          result.defaultBranch,
        )
      }
      message.success('代码仓库已创建')
      closeRepositoryModal()
    },
    onError: (err: Error) => message.error(err.message),
  })
  const updateRepositoryOptions = deliveryMutations.repositories.update(queryClient)
  const updateRepositoryMutation = useMutation({
    ...updateRepositoryOptions,
    onSuccess: (result, variables, onMutateResult, context) => {
      void updateRepositoryOptions.onSuccess?.(result, variables, onMutateResult, context)
      message.success('代码仓库已更新')
      closeRepositoryModal()
    },
    onError: (err: Error) => message.error(err.message),
  })
  const deleteRepositoryOptions = deliveryMutations.repositories.delete(queryClient)
  const deleteRepositoryMutation = useMutation({
    ...deleteRepositoryOptions,
    onSuccess: (result, variables, onMutateResult, context) => {
      void deleteRepositoryOptions.onSuccess?.(result, variables, onMutateResult, context)
      message.success('代码仓库已删除')
    },
    onError: (err: Error) => message.error(err.message),
  })

  const openServiceModal = (service?: ApplicationServiceComponent) => {
    const nextService = service ?? null
    setEditingService(nextService)
    setServiceModalVisible(true)
    const next = new URLSearchParams(searchParams)
    next.set('tab', 'services')
    next.set('settings', 'application')
    setSearchParams(next)
  }

  const openRepositoryModal = (repositoryId = '', bindingTarget: number | null = null) => {
    const repository = repositories.find((item) => item.id === repositoryId)
    setEditingRepositoryId(repositoryId)
    setRepositoryBindingTarget(bindingTarget)
    setRepositoryModalVisible(true)
    repositoryForm.setFieldsValue(
      repository
        ? { ...repository }
        : { provider: 'gitlab', protocol: 'https', defaultBranch: 'main' },
    )
  }

  const openBuildSourceModal = (source?: BuildSource, snapshot = application) => {
    if (!canManageRepositories) return
    const repositoryBindings = source
      ? buildRepositoryBindings(source)
      : repositories[0]
        ? [
            {
              repositoryId: repositories[0].id,
              checkoutPath: '.',
              defaultBranch: repositories[0].defaultBranch,
              allowCommitSelection: false,
              submodules: false,
            },
          ]
        : [
            {
              repositoryId: '',
              checkoutPath: '.',
              defaultBranch: 'main',
              allowCommitSelection: false,
              submodules: false,
            },
          ]
    setEditingBuildSourceId(source?.id ?? '')
    setBuildSourceApplication(snapshot ?? null)
    const draft: BuildSourceFormValues = source
      ? {
          ...source,
          config: { ...source.config, repositoryBindings },
        }
      : {
          name: '',
          type: 'repo_dockerfile',
          enabled: true,
          isDefault: !snapshot?.buildSources?.length,
          config: {
            repositoryId: repositories[0]?.id,
            repositoryBindings,
            contextDir: '.',
            dockerfilePath: 'Dockerfile',
            builderKind: 'docker',
          },
        }
    setBuildSourceDraft(draft)
    if (buildSourceModalVisible) {
      buildSourceForm.resetFields()
      buildSourceForm.setFieldsValue(draft)
    }
    setBuildSourceModalVisible(true)
  }

  const reloadBuildSource = useMutation({
    mutationFn: async () => (await deliveryApi.applications.detail(applicationId!)).application,
    onSuccess: (latest) => {
      const source = latest.buildSources?.find((item) => item.id === editingBuildSourceId)
      if (editingBuildSourceId && !source) {
        message.error('该共享构建已被删除，请关闭编辑器后重新选择')
        return
      }
      openBuildSourceModal(source, latest)
      managementState.updateAppMutation.reset()
    },
    onError: (error: Error) => message.error(error.message),
  })

  useEffect(() => {
    if (!deliveryActionModalVisible)
      populateDeliveryBinding(activeEnvironmentId || requestedEnvironmentId)
  }, [
    activeEnvironmentId,
    requestedEnvironmentId,
    deliveryActionModalVisible,
    populateDeliveryBinding,
  ])

  useEffect(() => {
    if (
      !['build', 'workflow'].includes(requestedLaunch || '') ||
      activeTab !== 'delivery' ||
      (!activeEnvironmentId && requestedLaunch !== 'build') ||
      !detail ||
      (runtimeUnavailable && requestedLaunch !== 'build') ||
      invalidEnvironment
    )
      return
    const build = requestedLaunch === 'build'
    if (
      build
        ? !canViewBuilds ||
          !canTriggerBuild ||
          !application?.buildSources?.some(
            (source) => source.id === requestedBuildSourceId && source.enabled !== false,
          )
        : !canViewWorkflows || !canTriggerWorkflow || !canTriggerBuild
    )
      return
    populateDeliveryBinding(activeEnvironmentId, build ? requestedBuildSourceId : undefined)
    setDeliveryActionKind(build ? 'build' : 'build_deploy')
    setDeliveryActionModalVisible(true)
    const next = new URLSearchParams(searchParams)
    next.delete('launch')
    setSearchParams(next, { replace: true })
  }, [
    requestedLaunch,
    activeTab,
    activeEnvironmentId,
    detail,
    runtimeUnavailable,
    invalidEnvironment,
    canViewBuilds,
    canTriggerBuild,
    canViewWorkflows,
    canTriggerWorkflow,
    application?.buildSources,
    requestedBuildSourceId,
    populateDeliveryBinding,
    searchParams,
    setSearchParams,
  ])

  const workloads = activeEnvironment?.workloads ?? []
  const runtimeWorkloads = workloads
  const activeRuntimeBinding = managementState.filteredBindings.find(
    (item) => item.id === activeEnvironmentId,
  )
  const activeRuntimeClusterId =
    activeRuntimeBinding?.clusterId ||
    runtimeWorkloads[0]?.clusterId ||
    activeEnvironment?.targets?.[0]?.clusterId ||
    ''
  const activeRuntimeClusterName =
    managementState.clustersQuery.data?.find((item) => item.id === activeRuntimeClusterId)?.name ||
    activeRuntimeClusterId
  const activeRuntimeNamespace =
    activeRuntimeBinding?.namespace ||
    runtimeWorkloads[0]?.namespace ||
    activeEnvironment?.targets?.[0]?.namespace ||
    ''
  const selectedBindingId = deliveryActionModalVisible
    ? selectedDeliveryBindingId
    : activeEnvironmentId || requestedEnvironmentId
  const selectedDeliveryBinding = selectedBindingId
    ? bindings.find((item) => item.applicationEnvironmentId === selectedBindingId)
    : invalidEnvironment
      ? undefined
      : bindings[0]
  const enabledTargets = selectedDeliveryBinding?.targets?.filter((item) => item.enabled) ?? []
  const selectedDeliveryTarget =
    selectedDeliveryBinding?.targets?.find((item) => item.id === selectedTargetId) ??
    enabledTargets[0] ??
    selectedDeliveryBinding?.targets?.[0]
  const manifestDelivery = deliveryActionKind === 'deploy'
  const deliveryActionLabel = manifestDelivery
    ? '部署配置'
    : deliveryActionKind === 'build'
      ? '运行构建'
      : '运行工作流'
  const manifestSnapshots = pendingDeliveryPlan?.manifestSnapshots ?? []
  const manifestPreflights = useQueries({
    queries: manifestSnapshots.map((snapshot) =>
      deliveryQueries.executionTasks.detail(snapshot.preflightTaskId, deliveryPlanModalVisible),
    ),
  })
  const manifestPreflightReady = manifestSnapshots.every((snapshot, index) => {
    const task = manifestPreflights[index]?.data
    const preflight = task?.result?.preflight
    return (
      task?.status === 'completed' &&
      preflight != null &&
      typeof preflight === 'object' &&
      'ready' in preflight &&
      preflight.ready === true &&
      'renderedDigest' in preflight &&
      preflight.renderedDigest === snapshot.renderedDigest
    )
  })
  const deliveryActionsCapability = useClusterCapabilityForCluster(
    'delivery.actions',
    'zh_CN',
    activeTab === 'delivery' || deliveryActionModalVisible
      ? selectedDeliveryTarget?.clusterId
      : undefined,
  )
  const selectedBuildSource =
    application?.buildSources?.find((item) => item.id === selectedBuildSourceId) ??
    selectedDeliveryBinding?.buildSource ??
    application?.buildSources?.find((item) => item.isDefault) ??
    application?.buildSources?.[0]
  const selectedBuildRepositoryBindings = buildRepositoryBindings(selectedBuildSource)
  const effectiveImageTag =
    selectedImageTag || selectedBuildSource?.defaultTag || application?.defaultTag || ''
  const validationNodeCount = workflowTemplateValidationNodeCount(
    selectedDeliveryBinding?.workflowTemplate,
  )
  const bindingSummaryById = useMemo(
    () =>
      Object.fromEntries(bindings.map((binding) => [binding.applicationEnvironmentId, binding])),
    [bindings],
  )
  const openBindingCreate = () => {
    managementState.setEditingBinding(null)
    managementState.bindingForm.resetFields()
    managementState.setBindingModalVisible(true)
  }
  const openBindingEdit = (binding: ApplicationEnvironment) => {
    managementState.setEditingBinding(binding)
    managementState.setBindingModalVisible(true)
  }
  const openWorkflowCreate = () => {
    if (!canUpdateApplicationEnvironment) return
    workflowCreateForm.setFieldsValue({
      name: `${application?.name || '应用'}工作流`,
      applicationEnvironmentId: activeEnvironmentId || bindings[0]?.applicationEnvironmentId,
      source: 'blank',
      templateId: undefined,
    })
    setWorkflowCreateModalVisible(true)
  }
  const openWorkflowDesigner = (binding: DeliveryApplicationBindingSummary) => {
    if (!applicationId || !canUpdateApplicationEnvironment) return
    navigate(
      applicationWorkflowDesignPath(applicationId, {
        applicationEnvironmentId: binding.applicationEnvironmentId,
        templateId: binding.workflowTemplateId,
      }),
    )
  }
  const deliveryTargetActionsDisabled =
    deliveryActionsCapability.status !== 'unknown' &&
    deliveryActionsCapability.status !== 'available' &&
    !(
      deliveryActionsCapability.status === 'partial' &&
      deliveryActionKind === 'deploy' &&
      ['manifest_ssa', 'helm_sdk'].includes(selectedDeliveryTarget?.executorKind ?? '')
    )
  const deliveryTargetCapabilityReason = deliveryTargetActionsDisabled
    ? deliveryActionsCapability.reason
    : ''
  const triggerDeliveryAction = async (action: ApplicationDeliveryActionKind) => {
    if (
      deliverySubmitting.current ||
      deliveryUnknown ||
      directBuildMutation.isPending ||
      createDeliveryPlanMutation.isPending
    )
      return
    if (
      action === 'deploy'
        ? !canTriggerRelease
        : action === 'build'
          ? !canViewBuilds || !canTriggerBuild
          : !canViewWorkflows || !canTriggerBuild || !canTriggerWorkflow
    )
      return
    try {
      deliverySubmitting.current = true
      const values = await deliveryForm.validateFields()
      if (!applicationId) return
      if (action === 'build' && !values.applicationEnvironmentId) {
        const payload = buildDeliveryActionPayload(action, values)
        await directBuildMutation.mutateAsync({
          applicationId,
          buildSourceId: payload.buildSourceId,
          refType: payload.refType || 'branch',
          refName: payload.refName || '',
          repositoryRefs: payload.repositoryRefs,
          imageTag: payload.imageTag,
        })
        return
      }
      await createDeliveryPlanMutation.mutateAsync(
        buildDeliveryPlanPayload(applicationId, action, values),
      )
    } catch {
      // Field validation and operation feedback retain the submitted form.
    } finally {
      deliverySubmitting.current = false
    }
  }
  const deliveryActionPending =
    directBuildMutation.isPending ||
    createDeliveryPlanMutation.isPending ||
    confirmDeliveryPlanMutation.isPending
  const buildDeployDisabledReason = disabledReason(
    manifestDelivery
      ? [
          !selectedDeliveryBinding && '无环境绑定',
          selectedDeliveryTarget?.executorKind !== 'manifest_ssa' && '请选择资源包目标',
          !canTriggerRelease && '缺少发布权限',
          deliveryTargetCapabilityReason,
        ]
      : [
          deliveryActionKind !== 'build' && !selectedDeliveryBinding && '无环境绑定',
          deliveryActionKind !== 'build' && !selectedDeliveryTarget && '无服务 / Workload',
          deliveryActionKind !== 'build' &&
            !selectedDeliveryBinding?.workflowTemplate &&
            '无工作流模板',
          !selectedBuildSource && '无构建定义',
          selectedBuildSource?.enabled === false && '构建定义已停用',
          deliveryActionKind !== 'build' && deliveryTargetCapabilityReason,
          !effectiveImageTag && '缺少镜像 Tag / 默认 Tag',
          !canTriggerBuild && '缺少构建权限',
          deliveryActionKind !== 'build' && !canTriggerWorkflow && '缺少工作流权限',
          deliveryActionKind !== 'build' &&
            selectedDeliveryTarget?.executorKind === 'manifest_ssa' &&
            '资源包请使用部署配置',
        ],
  )

  if (!application && (runtimeQuery.isLoading || detailQuery.isLoading)) {
    return (
      <div className="soha-page">
        <ManagementState kind="loading" title="正在加载应用" />
      </div>
    )
  }

  if (!application) {
    const notFound = [runtimeQuery.error, detailQuery.error].some(
      (error) => isApiError(error) && error.status === 404,
    )
    return (
      <div className="soha-page">
        <ManagementState
          kind={notFound ? 'not-found' : 'error'}
          title={notFound ? '应用不存在' : '应用加载失败'}
          description={notFound ? '应用不存在或已被删除' : '暂时无法读取应用，请重试。'}
          actions={
            notFound ? undefined : (
              <Button
                aria-label="重试"
                onClick={() => {
                  void runtimeQuery.refetch()
                  void detailQuery.refetch()
                }}
              >
                重试
              </Button>
            )
          }
        />
      </div>
    )
  }

  const requestedWorkload = searchParams.get('workload') || ''
  const focusedService = focusedServiceId
    ? services.find((service) => service.id === focusedServiceId)
    : requestedWorkload
      ? undefined
      : services[0]
  const focusedRepository = repositories.find(
    (repository) => repository.id === focusedService?.repositoryId,
  )
  const orphanWorkloads = runtimeWorkloads.filter(
    (workload) =>
      !services.some(
        (service) => service.id === workload.serviceId || service.key === workload.serviceKey,
      ),
  )
  const selectedWorkloads = requestedWorkload
    ? runtimeWorkloads.filter((workload) => workload.workloadName === requestedWorkload)
    : focusedService
      ? runtimeWorkloads.filter(
          (workload) =>
            workload.serviceId === focusedService.id || workload.serviceKey === focusedService.key,
        )
      : focusedServiceId
        ? []
        : orphanWorkloads.slice(0, 1)
  const selectedPodWorkloads = selectedWorkloads.filter(
    (item) =>
      !item.sourceStatus ||
      (item.workloadKind === 'Deployment' && ['linked', 'changed'].includes(item.sourceStatus)),
  )
  const serviceManifestDeployments = (serviceId?: string) =>
    (activeEnvironment?.manifestDeployments ?? []).filter(
      (deployment) =>
        serviceId &&
        (activeRuntimeBinding?.targets ?? []).some(
          (target) =>
            target.enabled &&
            target.executorKind === 'manifest_ssa' &&
            target.configRef === deployment.bindingId &&
            target.metadata?.serviceId === serviceId,
        ),
    )
  const selectedManifestDeployments = serviceManifestDeployments(focusedService?.id)
  const serviceStatus = (items: ApplicationRuntimeWorkload[], serviceId?: string) =>
    runtimePending || runtimeUnavailable || invalidEnvironment
      ? { tone: 'default', label: runtimePending ? '读取状态' : '状态读取失败' }
      : environmentRuntimeStatus({
          ...activeEnvironment!,
          workloads: items,
          manifestDeployments: serviceManifestDeployments(serviceId),
        })
  const selectionValue =
    focusedService?.id ||
    (selectedWorkloads[0] ? `workload:${selectedWorkloads[0].workloadName}` : undefined)
  const selectService = (value: string) => {
    if (!value.startsWith('workload:')) {
      setFocusedService(value, serviceTab)
      return
    }
    const next = new URLSearchParams(searchParams)
    next.set('tab', 'services')
    next.set('workload', value.slice(9))
    next.set('serviceTab', serviceTab)
    next.delete('serviceId')
    next.delete('pod')
    next.delete('podCluster')
    next.delete('podNamespace')
    next.delete('tool')
    next.delete('container')
    for (const key of ['metricPod', 'metricCluster', 'metricNamespace', 'metricRange'])
      next.delete(key)
    if (activeEnvironmentId) next.set('applicationEnvironmentId', activeEnvironmentId)
    setSearchParams(next)
  }
  const openServiceTab = (nextTab: string) => {
    const next = new URLSearchParams(searchParams)
    next.set('tab', 'services')
    next.set('serviceTab', nextTab)
    if (focusedService) next.set('serviceId', focusedService.id)
    next.delete('pod')
    next.delete('podCluster')
    next.delete('podNamespace')
    next.delete('tool')
    next.delete('container')
    if (activeEnvironmentId) next.set('applicationEnvironmentId', activeEnvironmentId)
    setSearchParams(next)
  }
  const configuredWorkflowBindings = bindings.filter(
    (binding) =>
      binding.workflowTemplateId && binding.applicationEnvironmentId === activeEnvironmentId,
  )
  const selectedWorkflowCreateBinding = bindings.find(
    (binding) => binding.applicationEnvironmentId === workflowCreateEnvironmentId,
  )
  const reusableWorkflowTemplates = (managementState.workflowTemplatesQuery.data ?? []).filter(
    (template) =>
      !template.category?.startsWith('application:') &&
      template.enabled &&
      (template.publishedVersion ?? 0) > 0 &&
      template.publicationState !== 'deprecated',
  )
  const permissionRows = [
    { key: 'delivery.builds.trigger', label: '构建', enabled: canTriggerBuild },
    { key: 'delivery.workflows.trigger', label: '工作流', enabled: canTriggerWorkflow },
    { key: 'delivery.releases.trigger', label: '发布', enabled: canTriggerRelease },
    {
      key: 'delivery.application-environments.approve',
      label: '审批',
      enabled: canApproveDelivery,
    },
    { key: 'delivery.application-services.create', label: '新建服务', enabled: canCreateService },
    { key: 'delivery.application-services.update', label: '修改服务', enabled: canUpdateService },
    { key: 'delivery.application-services.delete', label: '删除服务', enabled: canDeleteService },
  ]
  const attentionEnvironments = environments.filter((environment) =>
    ['danger', 'warning'].includes(environmentRuntimeStatus(environment).tone),
  )
  const serviceCards = [
    ...services.map((service) => ({
      id: service.id,
      name: service.name || service.key,
      kind: serviceKindLabel(service.serviceKind),
      workloads: runtimeWorkloads.filter(
        (workload) => workload.serviceId === service.id || workload.serviceKey === service.key,
      ),
    })),
    ...orphanWorkloads.map((workload) => ({
      id: `workload:${workload.workloadName}`,
      name: workload.workloadName,
      kind: workload.workloadKind,
      workloads: [workload],
    })),
  ]
  const currentServiceCard = serviceCards.find((service) => service.id === selectionValue)
  const filteredServiceCards = serviceCards.filter(
    (service) =>
      (serviceKindFilter === 'all' || service.kind === serviceKindFilter) &&
      [service.name, service.id].some((value) =>
        value.toLowerCase().includes(serviceSearch.trim().toLowerCase()),
      ),
  )
  const selectedStatus = serviceStatus(selectedWorkloads, focusedService?.id)
  const selectedReady = selectedWorkloads.reduce((sum, item) => sum + item.readyReplicas, 0)
  const selectedDesired = selectedWorkloads.reduce((sum, item) => sum + item.desiredReplicas, 0)
  const serviceDetails = (
    <div className="soha-service-detail">
      <div className="soha-service-summary">
        <div className="soha-service-overview" data-tone={selectedStatus.tone}>
          <span className="soha-service-overview__icon" aria-hidden="true">
            <DeploymentUnitOutlined />
          </span>
          <div className="soha-service-overview__identity">
            <h1>{currentServiceCard?.name || focusedService?.name || '服务'}</h1>
            <Text type="secondary">{currentServiceCard?.kind}</Text>
          </div>
          <div className="soha-service-overview__status" data-tone={selectedStatus.tone}>
            <StatusTag value={selectedStatus.tone} label={selectedStatus.label} />
            {selectedWorkloads.length > 0 &&
            selectedWorkloads.every(
              (item) =>
                !item.sourceStatus ||
                (item.workloadKind === 'Deployment' && item.healthStatus !== 'unknown'),
            ) &&
            !runtimePending &&
            !runtimeUnavailable &&
            !invalidEnvironment &&
            activeEnvironment?.status !== 'unavailable' ? (
              <div
                className="soha-service-overview__readiness"
                aria-label={`实例就绪 ${selectedReady} / ${selectedDesired}`}
              >
                <Progress
                  type="circle"
                  size={48}
                  percent={
                    selectedDesired > 0 ? Math.min(100, (selectedReady / selectedDesired) * 100) : 0
                  }
                  strokeColor="var(--soha-service-status-color)"
                  railColor="var(--soha-border-color)"
                  format={() => `${selectedReady}/${selectedDesired}`}
                />
                <Text strong>实例就绪</Text>
              </div>
            ) : null}
          </div>
        </div>
        <div className="soha-service-location">
          <Text type="secondary">
            {[activeRuntimeClusterName, activeRuntimeNamespace].filter(Boolean).join(' / ')}
          </Text>
          {catalogEnvironment(activeEnvironment?.environmentId)?.isProduction ? (
            <StatusTag value="warning" label="生产环境 PROD" />
          ) : null}
          <ManagementRefreshButton
            tooltip="刷新当前环境"
            loading={runtimeQuery.isFetching || detailQuery.isFetching}
            onClick={() => {
              void runtimeQuery.refetch()
              void detailQuery.refetch()
            }}
          />
        </div>
        <Space size={8} className="soha-service-actions" wrap>
          {focusedService?.deploymentTemplate && !focusedService.deploymentTemplate.detached ? (
            <TemplateUpdateEntry
              kind="deployment"
              templateId={focusedService.deploymentTemplate.templateId}
              applicationId={application.id}
              serviceId={focusedService.id}
              environmentId={activeEnvironmentId}
            />
          ) : null}
          <Button onClick={() => openServiceTab('related-resources')}>关联资源</Button>
          {focusedService ? (
            <Button onClick={() => openServiceTab('resources')}>资源清单</Button>
          ) : null}
          {canTriggerRelease &&
          (activeRuntimeBinding?.targets ?? []).some(
            (target) =>
              target.enabled &&
              target.executorKind === 'manifest_ssa' &&
              (!focusedService || target.metadata?.serviceId === focusedService.id),
          ) ? (
            <Button
              icon={<DeploymentUnitOutlined />}
              onClick={() => {
                const target = activeRuntimeBinding?.targets?.find(
                  (item) =>
                    item.enabled &&
                    item.executorKind === 'manifest_ssa' &&
                    (!focusedService || item.metadata?.serviceId === focusedService.id),
                )
                populateDeliveryBinding(activeEnvironmentId)
                deliveryForm.setFieldsValue({ targetId: target?.id, manifestRevision: undefined })
                setDeliveryActionKind('deploy')
                setDeliveryActionModalVisible(true)
              }}
            >
              部署配置
            </Button>
          ) : null}
          {selectedWorkloads.length > 0 &&
          !runtimePending &&
          !runtimeUnavailable &&
          !invalidEnvironment ? (
            <Suspense fallback={null}>
              <ServiceRuntimeActions
                key={`${activeEnvironmentId}/${selectionValue}`}
                applicationId={application.id}
                workloads={selectedWorkloads}
              />
            </Suspense>
          ) : null}
          {focusedService && canDeleteService ? (
            <Popconfirm
              title={`确认删除服务组件 ${focusedService.name || focusedService.key}？`}
              description={`${environments.some((environment) => catalogEnvironment(environment.environmentId)?.isProduction) ? '此应用包含生产环境。' : ''}这是应用级服务配置，删除范围不限于当前环境。`}
              okText="删除服务组件"
              onConfirm={() => {
                setFocusedService()
                deleteServiceMutation.mutate({
                  applicationId: applicationId ?? '',
                  serviceId: focusedService.id,
                })
              }}
            >
              <Button danger icon={<DeleteOutlined />}>
                删除
              </Button>
            </Popconfirm>
          ) : null}
          {canViewWorkflows || canViewBuilds ? (
            <Button
              icon={<PlayCircleOutlined />}
              type="primary"
              onClick={() => setActiveTab('delivery', activeEnvironmentId)}
            >
              构建与更新
            </Button>
          ) : null}
        </Space>
        {focusedService ? (
          <dl className="soha-service-summary__facts">
            <div>
              <dt>服务 Key</dt>
              <dd>{focusedService.key}</dd>
            </div>
            <div>
              <dt>状态</dt>
              <dd>
                <StatusTag value={focusedService.enabled ? 'enabled' : 'disabled'} />
              </dd>
            </div>
            <div>
              <dt>类型</dt>
              <dd>{serviceKindLabel(focusedService.serviceKind)}</dd>
            </div>
            <div>
              <dt>负责人 / 团队</dt>
              <dd>{focusedService.ownerTeam || '未设置'}</dd>
            </div>
            <div>
              <dt>代码仓库</dt>
              <dd>
                {focusedRepository?.path ||
                  focusedService.repositoryPath ||
                  focusedRepository?.name ||
                  focusedService.repositoryId ||
                  '未关联'}
              </dd>
            </div>
            <div>
              <dt>分支</dt>
              <dd>
                {focusedService.defaultBranch || focusedRepository?.defaultBranch || '未配置'}
              </dd>
            </div>
          </dl>
        ) : null}
        {selectedWorkloads.length > 0 &&
        !runtimePending &&
        !runtimeUnavailable &&
        !invalidEnvironment ? (
          <Suspense fallback={<ManagementState compact kind="loading" />}>
            <ServiceRuntimeSummary applicationId={application.id} workloads={selectedWorkloads} />
          </Suspense>
        ) : null}
      </div>
      {!runtimePending &&
      !runtimeUnavailable &&
      !invalidEnvironment &&
      activeEnvironment?.status !== 'unavailable'
        ? selectedWorkloads.map((workload) => (
            <WorkloadProgress
              key={`${workload.clusterId}/${workload.namespace}/${workload.workloadName}`}
              workload={workload}
            />
          ))
        : null}
      {!runtimePending &&
      !runtimeUnavailable &&
      !invalidEnvironment &&
      activeEnvironment?.status !== 'unavailable'
        ? selectedManifestDeployments.map((deployment) => (
            <ManifestDeploymentState key={deployment.id} deployment={deployment} />
          ))
        : null}
      <Tabs
        activeKey={serviceTab === 'metrics' ? 'metrics' : 'pods'}
        onChange={openServiceTab}
        items={[
          { key: 'pods', label: '运行资源' },
          { key: 'metrics', label: '指标' },
        ]}
      />
      <section
        className="soha-service-pods-panel"
        aria-label={serviceTab === 'metrics' ? '服务指标' : 'Pods'}
      >
        <Suspense fallback={<ManagementState compact kind="loading" />}>
          {runtimePending ||
          runtimeUnavailable ||
          invalidEnvironment ||
          activeEnvironment?.status === 'unavailable' ? (
            <ManagementState
              compact
              kind={runtimePending ? 'loading' : 'error'}
              actions={<Button onClick={() => void runtimeQuery.refetch()}>重试</Button>}
            />
          ) : selectedWorkloads.some((item) => item.sourceStatus) ? (
            <>
              <ImportedSourceBrowser
                key={`${activeEnvironmentId}/${selectionValue}`}
                workloads={selectedWorkloads}
                binding={activeRuntimeBinding}
                view={serviceTab === 'metrics' ? 'metrics' : 'resources'}
                onOpenPlan={
                  canTriggerRelease
                    ? (targetId, revision) => {
                        populateDeliveryBinding(activeEnvironmentId)
                        deliveryForm.setFieldsValue({ targetId, manifestRevision: revision })
                        setDeliveryActionKind('deploy')
                        setDeliveryActionModalVisible(true)
                      }
                    : undefined
                }
              />
              {selectedPodWorkloads.length > 0 ? (
                <ServicePodWorkspace
                  key={`${activeEnvironmentId}/${selectionValue}/pods`}
                  applicationId={application.id}
                  workloads={selectedPodWorkloads}
                  view={serviceTab === 'metrics' ? 'metrics' : 'pods'}
                />
              ) : null}
            </>
          ) : selectedWorkloads.length ? (
            <ServicePodWorkspace
              key={`${activeEnvironmentId}/${selectionValue}`}
              applicationId={application.id}
              workloads={selectedPodWorkloads}
              view={serviceTab === 'metrics' ? 'metrics' : 'pods'}
            />
          ) : (
            <ManagementState
              compact
              bordered={false}
              kind={focusedServiceId && !focusedService ? 'not-found' : 'not-configured'}
              title={
                focusedServiceId && !focusedService
                  ? '服务不存在或不可访问'
                  : selectedManifestDeployments.length
                    ? '当前资源清单中没有可展示的 Deployment Pods'
                    : '当前环境尚未部署此服务'
              }
              description={
                selectedManifestDeployments.length
                  ? '配置应用和资源就绪状态见上方资源清单。'
                  : '服务配置仍可查看，可进入工作流安排交付。'
              }
              actions={
                selectedManifestDeployments.length ? undefined : (
                  <Button onClick={() => setActiveTab('delivery', activeEnvironmentId)}>
                    查看工作流
                  </Button>
                )
              }
            />
          )}
        </Suspense>
      </section>
      {serviceTab === 'resources' || serviceTab === 'related-resources' ? (
        <Suspense fallback={null}>
          <ServiceResourceModal
            key={`${activeEnvironmentId}/${selectionValue}/${serviceTab}`}
            applicationId={application.id}
            serviceId={focusedService?.id}
            workloads={selectedWorkloads}
            mode={serviceTab}
            onClose={() => openServiceTab('pods')}
          />
        </Suspense>
      ) : null}
    </div>
  )
  const applicationEnvironmentOverview = (
    <details className="soha-application-inline-disclosure">
      <summary>查看环境运行概览</summary>
      <div className="soha-application-overview">
        <div className="soha-overview-metric-grid">
          <OverviewMetricCard
            label="服务"
            value={servicesQuery.isError ? '不可用' : services.length}
            loading={servicesQuery.isLoading}
            helper="应用中的服务定义"
          />
          <OverviewMetricCard
            label="环境"
            value={runtimeUnavailable ? '不可用' : environments.length}
            loading={runtimePending}
            helper="可访问的应用环境"
          />
          <OverviewMetricCard
            label="运行需关注"
            value={runtimeUnavailable ? '不可用' : attentionEnvironments.length}
            loading={runtimePending}
            tone={attentionEnvironments.length ? 'warning' : 'default'}
            helper="异常或部分就绪的环境"
          />
          <OverviewMetricCard
            label="最新版本包"
            value={detailQuery.isError ? '不可用' : detail?.latestBundle?.version || '—'}
            loading={detailQuery.isLoading}
            helper={
              detail?.latestBundle
                ? formatDateTime(detail.latestBundle.createdAt)
                : '尚无版本包记录'
            }
          />
        </div>
        <Card
          title="环境与版本"
          extra={
            <Button type="link" onClick={() => setActiveTab('services')}>
              查看环境
            </Button>
          }
        >
          {runtimePending ? (
            <ManagementState bordered={false} compact kind="loading" title="正在加载运行态" />
          ) : runtimeUnavailable ? (
            <ManagementState
              bordered={false}
              compact
              kind="error"
              title="运行态加载失败"
              description="应用基础信息仍可使用，可重试获取环境与服务状态。"
              actions={<Button onClick={() => void runtimeQuery.refetch()}>重试运行态</Button>}
            />
          ) : (
            <DeliveryTable
              rowKey="applicationEnvironmentId"
              pagination={false}
              enableDensity={false}
              showColumnSettings={false}
              dataSource={environments}
              empty={
                <ManagementState
                  compact
                  bordered={false}
                  kind="not-configured"
                  title="暂无应用环境"
                  description="在环境配置中接入测试、预发或生产环境。"
                />
              }
              columns={[
                {
                  title: '环境',
                  key: 'name',
                  render: (_: unknown, environment: ApplicationRuntimeEnvironment) => (
                    <Link
                      to={applicationWorkspacePath(
                        application.id,
                        'services',
                        environment.applicationEnvironmentId,
                      )}
                    >
                      {environment.environmentName ||
                        environment.environmentKey ||
                        environment.environmentId}
                    </Link>
                  ),
                },
                {
                  title: '运行状态',
                  key: 'health',
                  render: (_: unknown, environment: ApplicationRuntimeEnvironment) => {
                    const status = environmentRuntimeStatus(environment)
                    return <StatusTag value={status.value} label={status.label} />
                  },
                },
                {
                  title: '运行实例',
                  key: 'count',
                  render: (_: unknown, environment: ApplicationRuntimeEnvironment) =>
                    environment.status === 'unavailable'
                      ? '不可用'
                      : (environment.workloads ?? []).length,
                },
                {
                  title: '最近版本包',
                  key: 'version',
                  render: (_: unknown, environment: ApplicationRuntimeEnvironment) =>
                    bindingSummaryById[environment.applicationEnvironmentId]?.latestBundle
                      ?.version || '—',
                },
                {
                  title: '最近交付',
                  key: 'delivery',
                  render: (_: unknown, environment: ApplicationRuntimeEnvironment) => {
                    const binding = bindingSummaryById[environment.applicationEnvironmentId]
                    const status = summarizeBindingStatus(binding)
                    return (
                      <StatusTag
                        value={status}
                        label={status === 'unknown' ? '尚无记录' : undefined}
                      />
                    )
                  },
                },
                {
                  title: '操作',
                  key: 'actions',
                  render: (_: unknown, environment: ApplicationRuntimeEnvironment) => (
                    <Button
                      type="link"
                      onClick={() =>
                        setActiveTab('verification', environment.applicationEnvironmentId)
                      }
                    >
                      测试验证
                    </Button>
                  ),
                },
              ]}
            />
          )}
        </Card>
      </div>
    </details>
  )
  const applicationWorkflowOverview = (
    <Card title="最近工作流">
      <DeliveryTable
        rowKey="applicationEnvironmentId"
        pagination={false}
        enableDensity={false}
        showColumnSettings={false}
        loading={detailQuery.isLoading}
        isError={detailQuery.isError}
        error={detailQuery.error}
        hasReadData={detailQuery.data !== undefined}
        onRetry={() => void detailQuery.refetch()}
        dataSource={bindings.filter((binding) => binding.workflowTemplateId)}
        empty={
          <ManagementState
            compact
            bordered={false}
            kind="not-configured"
            title="尚未配置工作流"
            description="应用仍可查看运行状态；需要自动交付时，在工作流中配置构建、部署和验证。"
          />
        }
        columns={[
          {
            title: '工作流',
            key: 'workflow',
            render: (_: unknown, binding: DeliveryApplicationBindingSummary) =>
              binding.workflowTemplate?.name || binding.workflowTemplateName || '未命名工作流',
          },
          {
            title: '环境',
            key: 'environment',
            render: (_: unknown, binding: DeliveryApplicationBindingSummary) =>
              binding.environmentName || binding.environmentKey || binding.environmentId,
          },
          {
            title: '最近结果',
            key: 'status',
            render: (_: unknown, binding: DeliveryApplicationBindingSummary) => (
              <StatusTag
                value={binding.latestWorkflow?.status || 'default'}
                label={binding.latestWorkflow ? undefined : '未运行'}
              />
            ),
          },
          {
            title: '运行时间',
            key: 'time',
            render: (_: unknown, binding: DeliveryApplicationBindingSummary) =>
              formatDateTime(binding.latestWorkflow?.createdAt),
          },
        ]}
      />
    </Card>
  )

  return (
    <div className="soha-page soha-application-workbench">
      <ApplicationEnvironmentNavigation
        environments={environments}
        activeId={activeEnvironmentId}
        productionIds={(environmentCatalogQuery.data ?? [])
          .filter((environment) => environment.isProduction)
          .map((environment) => environment.id)}
        clusterName={activeRuntimeClusterName}
        onCreate={managementState.canCreateBinding ? openBindingCreate : undefined}
      />
      {!buildSourceModalVisible && !managementState.appModalVisible ? (
        <MutationFeedback
          operations={[
            {
              mutation: managementState.updateAppMutation,
              label: '更新构建配置',
              target: managementState.updateAppMutation.variables?.id,
            },
          ]}
        />
      ) : null}
      <ReadFeedback query={runtimeQuery} label="应用运行状态" />
      <ReadFeedback query={detailQuery} label="应用配置" />
      <Modal
        width={820}
        title={planProduction ? '生产环境交付计划确认' : '交付计划确认'}
        open={deliveryPlanModalVisible}
        onCancel={() => setDeliveryPlanModalVisible(false)}
        footer={[
          <Button key="close" onClick={() => setDeliveryPlanModalVisible(false)}>
            关闭
          </Button>,
          ...(pendingDeliveryPlan?.status === 'waiting_approval' && canApproveDelivery
            ? [
                <Button
                  key="reject"
                  danger
                  disabled={planLocked}
                  loading={approvalMutation.isPending}
                  onClick={() => void actOnPlan('reject')}
                >
                  拒绝
                </Button>,
                <Button
                  key="approve"
                  type="primary"
                  disabled={planLocked}
                  loading={approvalMutation.isPending}
                  onClick={() => void actOnPlan('approve')}
                >
                  批准
                </Button>,
              ]
            : []),
          <Button
            key="confirm"
            type="primary"
            disabled={
              planLocked ||
              !pendingDeliveryPlan ||
              pendingDeliveryPlan.status !== 'draft' ||
              !manifestPreflightReady
            }
            loading={confirmDeliveryPlanMutation.isPending}
            onClick={() => void actOnPlan('confirm')}
          >
            {planProduction ? '确认执行生产交付' : '确认执行'}
          </Button>,
        ]}
      >
        <ReadFeedback query={planRead} label="交付计划" />
        <MutationFeedback
          operations={[
            {
              mutation: confirmDeliveryPlanMutation,
              label: '确认交付计划',
              target: confirmDeliveryPlanMutation.variables,
            },
            {
              mutation: approvalMutation,
              label: approvalMutation.variables?.action === 'reject' ? '拒绝计划' : '批准计划',
              target: approvalMutation.variables?.id,
            },
          ]}
        />
        {pendingDeliveryPlan ? (
          <Button
            loading={planRead.isFetching}
            onClick={async () => {
              const result = await planRead.refetch()
              if (!result.error && result.data) setPendingDeliveryPlan(result.data)
            }}
          >
            刷新计划记录
          </Button>
        ) : null}
        {confirmedDeliveryPlan ? (
          <Alert
            showIcon
            type="success"
            title="计划已确认并触发执行"
            description={
              confirmedDeliveryPlan.result.relatedIds?.executionTaskId ||
              confirmedDeliveryPlan.result.relatedIds?.workflowRunId ||
              confirmedDeliveryPlan.result.relatedIds?.releaseBundleId ||
              '执行请求已提交'
            }
          />
        ) : (
          <Alert
            showIcon
            type="warning"
            title="确认前不会触发执行"
            description={
              manifestSnapshots.length
                ? '预检只验证资源；预检通过并完成审批、确认后才会应用清单。'
                : '请核对风险、审批要求、目标环境和回滚策略。确认后才会触发现有交付动作 API。'
            }
          />
        )}
        {pendingDeliveryPlan ? (
          <EnvironmentNotice
            isProduction={planProduction}
            name={
              pendingDeliveryPlan.environmentKey ||
              pendingDeliveryPlan.applicationEnvironmentId ||
              '-'
            }
          />
        ) : null}
        {pendingDeliveryPlan ? (
          <Space orientation="vertical" size={12} style={{ width: '100%', marginTop: 12 }}>
            <Descriptions
              bordered
              size="small"
              column={1}
              items={[
                {
                  key: 'action',
                  label: '动作',
                  children: DELIVERY_ACTION_LABELS[pendingDeliveryPlan.action],
                },
                {
                  key: 'app',
                  label: '应用',
                  children:
                    pendingDeliveryPlan.applicationName || pendingDeliveryPlan.applicationId,
                },
                {
                  key: 'env',
                  label: '环境',
                  children:
                    pendingDeliveryPlan.environmentKey ||
                    pendingDeliveryPlan.applicationEnvironmentId,
                },
                {
                  key: 'target',
                  label: '目标',
                  children: pendingDeliveryPlan.targetSummary || '-',
                },
                {
                  key: 'ref',
                  label: '版本来源',
                  children:
                    [pendingDeliveryPlan.refType, pendingDeliveryPlan.refName]
                      .filter(Boolean)
                      .join(' / ') || '-',
                },
                {
                  key: 'risk',
                  label: '风险',
                  children: (
                    <StatusTag
                      value={pendingDeliveryPlan.riskLevel || 'unknown'}
                      label={pendingDeliveryPlan.riskLevel || 'unknown'}
                    />
                  ),
                },
                {
                  key: 'approval',
                  label: '审批',
                  children: pendingDeliveryPlan.requiresApproval ? (
                    <StatusTag value="warning" label="需要审批" />
                  ) : (
                    <StatusTag value="default" label="无需审批" />
                  ),
                },
                {
                  key: 'rollback',
                  label: '回滚策略',
                  children: pendingDeliveryPlan.rollbackStrategy || '-',
                },
              ]}
            />
            {manifestSnapshots.map((snapshot, index) => (
              <Card
                key={snapshot.bindingId}
                size="small"
                title={`资源包 ${snapshot.packageId} · v${snapshot.revision}`}
                extra={
                  <StatusTag
                    value={manifestPreflights[index]?.data?.status || 'unknown'}
                    label={manifestPreflights[index]?.isError ? '预检状态获取失败' : undefined}
                  />
                }
              >
                <Descriptions
                  size="small"
                  column={1}
                  items={[
                    {
                      key: 'scope',
                      label: '目标',
                      children: `${snapshot.clusterId} / ${snapshot.namespace}`,
                    },
                    {
                      key: 'source',
                      label: 'Commit',
                      children: snapshot.sourceCommit || 'Soha 托管版本',
                    },
                    ...(snapshot.rootResource
                      ? [
                          {
                            key: 'rootControl',
                            label: '根 CR 发布控制',
                            children: `${snapshot.rootResource.kind}/${snapshot.rootResource.name} · UID ${snapshot.rootResource.uid} · generation ${snapshot.rootResource.generation} · Soha 发布 spec，Operator 协调子资源`,
                          },
                        ]
                      : []),
                    { key: 'renderer', label: '渲染器', children: snapshot.rendererVersion },
                    {
                      key: 'input',
                      label: '输入摘要',
                      children: (
                        <Text code copyable>
                          {snapshot.inputDigest}
                        </Text>
                      ),
                    },
                    {
                      key: 'digest',
                      label: '清单摘要',
                      children: (
                        <Text code copyable>
                          {snapshot.renderedDigest}
                        </Text>
                      ),
                    },
                    {
                      key: 'preflight',
                      label: '预检任务',
                      children: (
                        <Link to={`/delivery/execution-tasks/${snapshot.preflightTaskId}`}>
                          {snapshot.preflightTaskId}
                        </Link>
                      ),
                    },
                  ]}
                />
                <details>
                  <summary>待应用资源（{snapshot.documents.length}）</summary>
                  {snapshot.documents.map((document) => (
                    <pre
                      className="soha-json-block"
                      key={`${document.apiVersion}/${document.kind}/${document.namespace}/${document.name}`}
                    >
                      {document.content}
                    </pre>
                  ))}
                </details>
                {manifestPreflights[index]?.isError ? (
                  <Button onClick={() => void manifestPreflights[index]?.refetch()}>
                    重试预检状态
                  </Button>
                ) : null}
              </Card>
            ))}
            <Card size="small" title="影响范围">
              <pre className="soha-json-block">
                {JSON.stringify(pendingDeliveryPlan.impact ?? {}, null, 2)}
              </pre>
            </Card>
          </Space>
        ) : null}
      </Modal>
      <Modal
        forceRender
        title="创建工作流"
        styles={{ header: { minHeight: 32 }, title: visuallyHiddenModalTitleStyle }}
        okText="创建工作流"
        open={workflowCreateModalVisible}
        width={600}
        onCancel={() => setWorkflowCreateModalVisible(false)}
        onOk={() => workflowCreateForm.submit()}
      >
        <Form
          form={workflowCreateForm}
          layout="vertical"
          onFinish={(values) => {
            if (!applicationId) return
            setWorkflowCreateModalVisible(false)
            navigate(
              applicationWorkflowDesignPath(applicationId, {
                applicationEnvironmentId: values.applicationEnvironmentId,
                name: values.name.trim(),
                source: values.source,
                templateId: values.source === 'template' ? values.templateId : undefined,
                templateVersion:
                  values.source === 'template'
                    ? reusableWorkflowTemplates.find((item) => item.id === values.templateId)
                        ?.publishedVersion
                    : undefined,
              }),
            )
          }}
        >
          <Form.Item
            name="name"
            label="工作流名称"
            rules={[{ required: true, whitespace: true, message: '请输入工作流名称' }]}
          >
            <Input maxLength={80} placeholder="例如：测试环境发布" />
          </Form.Item>
          <Form.Item
            name="applicationEnvironmentId"
            label="运行环境"
            rules={[{ required: true, message: '请选择运行环境' }]}
          >
            <Select
              options={bindings.map((binding) => ({
                value: binding.applicationEnvironmentId,
                label: `${binding.environmentName || binding.environmentKey || binding.environmentId}${binding.workflowTemplateId ? ' · 已有工作流' : ''}`,
              }))}
              placeholder="选择工作流所属环境"
            />
          </Form.Item>
          {selectedWorkflowCreateBinding?.workflowTemplateId ? (
            <Alert
              showIcon
              type="warning"
              title="该环境已有工作流"
              description="保存新设计后会替换该环境当前绑定的工作流；原全局模板不会被修改。"
            />
          ) : null}
          <Form.Item name="source" label="创建方式" rules={[{ required: true }]}>
            <Radio.Group className="soha-application-workflow-source-options">
              <Radio value="blank">
                <span className="soha-application-workflow-source-option__copy">
                  <Text strong>空白画布</Text>
                  <Text type="secondary">自行添加构建、部署、测试与审批节点。</Text>
                </span>
              </Radio>
              <Radio value="template">
                <span className="soha-application-workflow-source-option__copy">
                  <Text strong>从模板开始</Text>
                  <Text type="secondary">复制平台模板作为起点，创建后可独立编辑。</Text>
                </span>
              </Radio>
            </Radio.Group>
          </Form.Item>
          {workflowCreateSource === 'template' ? (
            <Form.Item
              name="templateId"
              label="起始模板"
              rules={[{ required: true, message: '请选择起始模板' }]}
            >
              <Select
                loading={managementState.workflowTemplatesQuery.isFetching}
                options={reusableWorkflowTemplates.map((template) => ({
                  value: template.id,
                  label: `${template.name} · v${template.publishedVersion}`,
                }))}
                placeholder="选择模板创建工作流"
              />
            </Form.Item>
          ) : (
            <Text type="secondary">创建后在空白画布中自行添加构建、部署、测试与审批流程。</Text>
          )}
        </Form>
      </Modal>
      <div className="soha-application-pane">
        {runtime && runtimeQuery.isError ? (
          <Alert type="warning" showIcon title="运行状态刷新失败，当前显示上次读取的结果" />
        ) : null}
        {activeTab === 'delivery' && detail && detailQuery.isError ? (
          <Alert type="warning" showIcon title="工作流刷新失败，当前显示上次读取的结果" />
        ) : null}
        {invalidEnvironment ? (
          <ManagementState
            kind="not-found"
            compact
            title="所选环境不存在或不可访问"
            description="请重新选择环境；不会自动切换到其他环境。"
            actions={
              <Button
                onClick={() => {
                  const next = new URLSearchParams(searchParams)
                  next.delete('applicationEnvironmentId')
                  setSearchParams(next)
                }}
              >
                重新选择环境
              </Button>
            }
          />
        ) : null}
        {(() => {
          const existingTabs = [
            {
              key: 'settings',
              label: '配置',
              children: (
                <div className="soha-application-runtime-settings-grid">
                  <section className="soha-application-settings-summary soha-application-service-config-section">
                    <OverviewSectionBar
                      title="服务配置"
                      description="服务 → Git 仓库 → 构建定义 → 产物镜像"
                      extra={
                        <Space>
                          {canCreateService ? (
                            <Button
                              type="primary"
                              icon={<PlusOutlined />}
                              onClick={() => openServiceModal()}
                            >
                              新建服务
                            </Button>
                          ) : null}
                        </Space>
                      }
                    />
                    {services.length > 0 ? (
                      <Collapse
                        accordion
                        defaultActiveKey={focusedService?.id || services[0]?.id}
                        items={services.map((service) => {
                          const repository = repositories.find(
                            (item) => item.id === service.repositoryId,
                          )
                          const buildSource = application.buildSources?.find(
                            (item) => item.id === service.buildSourceId,
                          )
                          return {
                            key: service.id,
                            label: (
                              <Space size={6} wrap>
                                <Text strong>{service.name}</Text>
                                <MetadataTag label={serviceKindLabel(service.serviceKind)} />
                                <StatusTag value={service.enabled ? 'enabled' : 'disabled'} />
                              </Space>
                            ),
                            extra: (
                              <Space size={2} onClick={(event) => event.stopPropagation()}>
                                {canUpdateService ? (
                                  <ManagementIconButton
                                    aria-label={`编辑服务 ${service.name}`}
                                    icon={<EditOutlined />}
                                    size="small"
                                    tooltip="编辑服务"
                                    onClick={() => openServiceModal(service)}
                                  />
                                ) : null}
                                <ManagementIconButton
                                  aria-label={`查看服务 ${service.name}`}
                                  icon={<ArrowRightOutlined />}
                                  size="small"
                                  tooltip="查看服务"
                                  onClick={() => setFocusedService(service.id)}
                                />
                              </Space>
                            ),
                            children: (
                              <Descriptions
                                size="small"
                                column={{ xs: 1, sm: 2, lg: 4 }}
                                items={[
                                  {
                                    key: 'repository',
                                    label: 'Git 仓库',
                                    children:
                                      repository?.name || service.repositoryPath || '未配置',
                                  },
                                  {
                                    key: 'build-source',
                                    label: '构建来源',
                                    children: buildSource?.name || '未配置',
                                  },
                                  {
                                    key: 'artifact',
                                    label: '产物镜像',
                                    children:
                                      service.containers?.[0]?.imageRepository ||
                                      buildSource?.buildImage ||
                                      '未配置',
                                  },
                                  {
                                    key: 'branch',
                                    label: '默认分支',
                                    children:
                                      service.defaultBranch || repository?.defaultBranch || '-',
                                  },
                                ]}
                              />
                            ),
                          }
                        })}
                      />
                    ) : (
                      serviceRecordsState || (
                        <ManagementState
                          bordered={false}
                          compact
                          kind="not-configured"
                          description="尚未配置服务。"
                        />
                      )
                    )}
                  </section>
                  <DeliveryTable
                    title="共享代码仓库"
                    actions={
                      canManageRepositories ? (
                        <Button
                          type="primary"
                          icon={<PlusOutlined />}
                          onClick={() => openRepositoryModal()}
                        >
                          添加仓库
                        </Button>
                      ) : null
                    }
                    rowKey="id"
                    pagination={false}
                    dataSource={repositories}
                    loading={repositoriesQuery.isLoading}
                    columns={[
                      { title: '名称', dataIndex: 'name' },
                      {
                        title: '提供方',
                        dataIndex: 'provider',
                        render: (value: string) => <MetadataTag label={value} />,
                      },
                      { title: '路径', dataIndex: 'path' },
                      { title: '默认分支', dataIndex: 'defaultBranch' },
                      { title: '协议', dataIndex: 'protocol' },
                      ...(canManageRepositories
                        ? [
                            {
                              title: '操作',
                              dataIndex: 'id',
                              width: 96,
                              render: (id: string) => (
                                <Space size={2}>
                                  <ManagementIconButton
                                    aria-label="编辑代码仓库"
                                    icon={<EditOutlined />}
                                    tooltip="编辑"
                                    onClick={() => openRepositoryModal(id)}
                                  />
                                  <Popconfirm
                                    title="确认删除该代码仓库？"
                                    onConfirm={() => deleteRepositoryMutation.mutate(id)}
                                  >
                                    <ManagementIconButton
                                      aria-label="删除代码仓库"
                                      danger
                                      icon={<DeleteOutlined />}
                                      tooltip="删除"
                                    />
                                  </Popconfirm>
                                </Space>
                              ),
                            },
                          ]
                        : []),
                    ]}
                  />
                  <DeliveryTable
                    title="共享构建定义"
                    actions={
                      managementState.canUpdateApplication ? (
                        <Button
                          type="primary"
                          icon={<PlusOutlined />}
                          onClick={() => openBuildSourceModal()}
                        >
                          添加构建
                        </Button>
                      ) : null
                    }
                    rowKey="id"
                    pagination={false}
                    dataSource={application.buildSources ?? []}
                    columns={[
                      { title: '名称', dataIndex: 'name' },
                      {
                        title: '类型',
                        dataIndex: 'type',
                        render: (_: unknown, record: BuildSource) => (
                          <MetadataTag label={summarizeBuildSource(record)} />
                        ),
                      },
                      {
                        title: '镜像',
                        dataIndex: 'buildImage',
                        render: (value: string) => value || '-',
                      },
                      {
                        title: '默认 Tag',
                        dataIndex: 'defaultTag',
                        render: (value: string) => value || '-',
                      },
                      {
                        title: '默认',
                        dataIndex: 'isDefault',
                        render: (value: boolean) => (
                          <StatusTag value={value ? 'enabled' : 'disabled'} />
                        ),
                      },
                      {
                        title: '启用',
                        dataIndex: 'enabled',
                        render: (value: boolean) => (
                          <StatusTag value={value ? 'enabled' : 'disabled'} />
                        ),
                      },
                      ...(managementState.canUpdateApplication
                        ? [
                            {
                              title: '操作',
                              dataIndex: 'id',
                              width: 96,
                              render: (id: string, record: BuildSource) => (
                                <Space size={2}>
                                  {record.type === 'platform_build_template' &&
                                  typeof record.config?.buildTemplateId === 'string' ? (
                                    <TemplateUpdateEntry
                                      kind="build"
                                      templateId={record.config.buildTemplateId}
                                      applicationId={application.id}
                                      iconOnly
                                      label="采用构建模板版本"
                                    />
                                  ) : null}
                                  <ManagementIconButton
                                    aria-label="编辑构建源"
                                    icon={<EditOutlined />}
                                    tooltip="编辑"
                                    onClick={() => openBuildSourceModal(record)}
                                  />
                                  <Popconfirm
                                    title="确认删除该构建源？"
                                    disabled={
                                      managementState.updateAppMutation.isPending ||
                                      writeOutcomeUnknown(managementState.updateAppMutation.error)
                                    }
                                    onConfirm={() => {
                                      if (
                                        managementState.updateAppMutation.isPending ||
                                        writeOutcomeUnknown(managementState.updateAppMutation.error)
                                      )
                                        return
                                      managementState.updateAppMutation.mutate({
                                        id: application.id,
                                        payload: {
                                          ...application,
                                          buildSources: (application.buildSources ?? []).filter(
                                            (item) => item.id !== id,
                                          ),
                                        },
                                      })
                                    }}
                                  >
                                    <ManagementIconButton
                                      aria-label="删除构建源"
                                      danger
                                      icon={<DeleteOutlined />}
                                      tooltip="删除"
                                    />
                                  </Popconfirm>
                                </Space>
                              ),
                            },
                          ]
                        : []),
                    ]}
                  />
                </div>
              ),
            },
            {
              key: 'environment-bindings',
              label: '环境绑定',
              children: (
                <div className="soha-application-pane">
                  <Card
                    className="soha-management-panel-card"
                    title="环境绑定"
                    loading={managementState.bindingsQuery.isLoading}
                    extra={
                      <Space>
                        <ManagementRefreshButton
                          aria-label="刷新环境绑定"
                          tooltip="刷新"
                          loading={managementState.bindingsQuery.isFetching}
                          onClick={() => void managementState.bindingsQuery.refetch()}
                        />
                        {managementState.canCreateBinding ? (
                          <Button
                            type="primary"
                            icon={<PlusOutlined />}
                            onClick={openBindingCreate}
                          >
                            新增环境
                          </Button>
                        ) : null}
                      </Space>
                    }
                  >
                    {managementState.filteredBindings.length > 0 ? (
                      <div
                        className="soha-application-long-card-list soha-application-environment-binding-list"
                        role="list"
                      >
                        {managementState.filteredBindings.map((record) => (
                          <Card
                            key={record.id}
                            size="small"
                            role="listitem"
                            className="soha-application-long-card"
                            title={
                              <Space size={6} wrap>
                                <strong>{record.environmentKey || record.environmentId}</strong>
                                <StatusTag
                                  value={summarizeBindingStatus(bindingSummaryById[record.id])}
                                />
                              </Space>
                            }
                            extra={
                              <Space className="soha-row-action-icons" size={2}>
                                <ManagementIconButton
                                  aria-label="查看运行态"
                                  icon={<ArrowRightOutlined />}
                                  size="small"
                                  tooltip="运行态"
                                  onClick={() => setActiveTab('services', record.id)}
                                />
                                {managementState.canUpdateBinding ? (
                                  <ManagementIconButton
                                    aria-label="编辑绑定"
                                    icon={<EditOutlined />}
                                    size="small"
                                    tooltip="编辑"
                                    onClick={() => openBindingEdit(record)}
                                  />
                                ) : null}
                                {managementState.canDeleteBinding ? (
                                  <Popconfirm
                                    title={
                                      catalogEnvironment(record.environmentId)?.isProduction
                                        ? '确认删除生产环境绑定？'
                                        : '确认删除绑定？'
                                    }
                                    description={`环境：${record.alias || record.environmentKey || record.environmentId} · 目标：${(record.targets ?? []).map((target) => `${target.clusterId}/${target.namespace}`).join('、') || '-'}`}
                                    okText={
                                      catalogEnvironment(record.environmentId)?.isProduction
                                        ? '删除生产绑定'
                                        : '删除绑定'
                                    }
                                    onConfirm={() =>
                                      managementState.deleteBindingMutation.mutate(record.id)
                                    }
                                    placement="topRight"
                                  >
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
                            }
                          >
                            <Descriptions
                              size="small"
                              column={{ xs: 1, sm: 2, lg: 4 }}
                              items={[
                                {
                                  key: 'buildSource',
                                  label: '构建来源',
                                  children: record.buildPolicy?.sourceId || '-',
                                },
                                {
                                  key: 'workflow',
                                  label: '发布流程模板',
                                  children: (
                                    <Space orientation="vertical">
                                      <span>
                                        {managementState.workflowTemplateMap[
                                          record.workflowTemplateId || ''
                                        ]?.name ||
                                          record.workflowTemplateId ||
                                          '-'}
                                      </span>
                                      {record.workflowTemplateId &&
                                      !managementState.workflowTemplateMap[
                                        record.workflowTemplateId
                                      ]?.category?.startsWith('application:') ? (
                                        <TemplateUpdateEntry
                                          kind="workflow"
                                          templateId={record.workflowTemplateId}
                                          applicationId={application.id}
                                          environmentId={record.id}
                                          label="采用流程模板版本"
                                        />
                                      ) : null}
                                    </Space>
                                  ),
                                },
                                {
                                  key: 'targets',
                                  label: '发布目标',
                                  children: renderBindingTargets(record.targets),
                                },
                                {
                                  key: 'selector',
                                  label: '资源选择器',
                                  children: renderSelectorLabels(record.resourceSelector),
                                },
                                {
                                  key: 'workflowHealth',
                                  label: '模板健康',
                                  children: renderEnvironmentBindingWorkflowHealth(
                                    record,
                                    bindingSummaryById[record.id],
                                    managementState.workflowTemplateMap,
                                  ),
                                },
                              ]}
                            />
                          </Card>
                        ))}
                      </div>
                    ) : (
                      <ManagementState
                        bordered={false}
                        compact
                        kind="not-configured"
                        description="尚未绑定交付环境"
                      />
                    )}
                  </Card>
                  <DeliveryTable
                    title="部署与审批"
                    rowKey="applicationEnvironmentId"
                    pagination={false}
                    dataSource={bindings}
                    columns={[
                      {
                        title: '环境',
                        dataIndex: 'environmentId',
                        render: (_: string, record: DeliveryApplicationBindingSummary) =>
                          record.environmentName || record.environmentKey || record.environmentId,
                      },

                      {
                        title: '动作',
                        dataIndex: 'actionKind',
                        render: (value?: string) => value || 'deploy',
                      },
                      {
                        title: '审批',
                        dataIndex: 'requiresApproval',
                        render: (value: boolean) => (
                          <StatusTag
                            value={value ? 'warning' : 'default'}
                            label={value ? '需要' : '无需'}
                          />
                        ),
                      },
                      {
                        title: '发布目标',
                        dataIndex: 'targetCount',
                        render: (_: number, record: DeliveryApplicationBindingSummary) =>
                          record.targetCount || record.targets?.length || 0,
                      },
                      {
                        title: '工作流',
                        dataIndex: 'workflowTemplateName',
                        render: (_: string, record: DeliveryApplicationBindingSummary) =>
                          record.workflowTemplate?.name || record.workflowTemplateName || '-',
                      },
                    ]}
                  />
                  {applicationEnvironmentOverview}
                </div>
              ),
            },
            {
              key: 'permissions',
              label: '权限',
              children: (
                <div className="soha-application-runtime-settings-grid">
                  {canViewScopeGrants ? (
                    <ScopeGrantManager
                      fixedApplication={fixedScopeGrantApplication}
                      inline
                      visible={settingsKey === 'permissions'}
                      title="应用环境授权记录"
                    />
                  ) : (
                    <Card
                      className="soha-management-panel-card soha-application-settings-summary"
                      title="应用环境授权"
                    >
                      <ManagementState
                        bordered={false}
                        compact
                        kind="no-permission"
                        description="当前账号没有查看应用额外授权的权限。"
                      />
                    </Card>
                  )}
                  <details className="soha-application-inline-disclosure">
                    <summary>查看我的权限</summary>
                    <Descriptions
                      column={1}
                      items={[
                        {
                          key: 'app',
                          label: '应用',
                          children: `${application.name} / ${application.key}`,
                        },
                        {
                          key: 'scope',
                          label: '范围',
                          children:
                            bindings
                              .map((binding) => binding.environmentKey || binding.environmentId)
                              .filter(Boolean)
                              .join(', ') || 'default',
                        },
                        {
                          key: 'permissions',
                          label: '可执行操作',
                          children: (
                            <Space wrap>
                              {permissionRows.map((item) => (
                                <StatusTag
                                  key={item.key}
                                  value={item.enabled ? 'success' : 'error'}
                                  label={`${item.label}: ${item.enabled ? '允许' : '无权限'}`}
                                />
                              ))}
                            </Space>
                          ),
                        },
                      ]}
                    />
                  </details>
                </div>
              ),
            },
            {
              key: 'services',
              label: '服务',
              children: (
                <section aria-label="服务工作台" className="soha-service-console">
                  <aside className="soha-service-selector">
                    <div className="soha-service-selector__heading">
                      <strong>
                        服务{' '}
                        <Text type="secondary">
                          {filteredServiceCards.length} / {serviceCards.length}
                        </Text>
                      </strong>
                      <ManagementIconButton
                        icon={<SettingOutlined />}
                        aria-label={settingsLabel}
                        tooltip={settingsLabel}
                        onClick={() => {
                          const next = new URLSearchParams(searchParams)
                          next.set('tab', 'services')
                          next.set('settings', 'application')
                          if (activeEnvironmentId)
                            next.set('applicationEnvironmentId', activeEnvironmentId)
                          if (focusedServiceId) next.set('serviceId', focusedServiceId)
                          setSearchParams(next)
                        }}
                      />
                    </div>
                    {canCreateService ? (
                      <Button block icon={<PlusOutlined />} onClick={() => openServiceModal()}>
                        添加服务
                      </Button>
                    ) : null}
                    <Input
                      aria-label="搜索服务"
                      placeholder="搜索名称或标识"
                      value={serviceSearch}
                      onChange={(event) => setServiceSearch(event.target.value)}
                      allowClear
                    />
                    <Select
                      aria-label="筛选服务类型"
                      value={serviceKindFilter}
                      options={[
                        { value: 'all', label: '全部服务类型' },
                        ...Array.from(new Set(serviceCards.map((service) => service.kind))).map(
                          (kind) => ({ value: kind, label: kind }),
                        ),
                      ]}
                      onChange={setServiceKindFilter}
                    />
                    <nav aria-label="服务列表" className="soha-service-selector__list">
                      {filteredServiceCards.map((service) => {
                        const status = serviceStatus(service.workloads, service.id)
                        return (
                          <button
                            key={service.id}
                            type="button"
                            aria-current={selectionValue === service.id ? 'page' : undefined}
                            onClick={() => selectService(service.id)}
                          >
                            <span className="soha-service-selector__icon" aria-hidden="true">
                              <DeploymentUnitOutlined />
                            </span>
                            <span>
                              <strong>{service.name}</strong>
                              <small>
                                <span
                                  className={`soha-service-status-dot is-${status.tone}`}
                                  aria-hidden="true"
                                />
                                {status.label}
                              </small>
                              <small>{service.kind}</small>
                            </span>
                          </button>
                        )
                      })}
                    </nav>
                    {!filteredServiceCards.length && serviceCards.length ? (
                      <ManagementState
                        compact
                        bordered={false}
                        kind="empty"
                        title="没有匹配的服务"
                      />
                    ) : null}
                    <Select
                      className="soha-service-selector__mobile"
                      aria-label="选择服务"
                      value={selectionValue}
                      showSearch={{ optionFilterProp: 'label' }}
                      options={filteredServiceCards.map((service) => ({
                        value: service.id,
                        label: service.name,
                      }))}
                      onChange={selectService}
                    />
                  </aside>
                  <div className="soha-service-console__body">
                    {serviceRecordsState}
                    {serviceCards.length || focusedServiceId ? (
                      serviceDetails
                    ) : (
                      <ManagementState
                        compact
                        kind="not-configured"
                        title="暂无服务"
                        description="添加服务后，在当前环境中查看运行实例。"
                      />
                    )}
                  </div>
                </section>
              ),
            },
            {
              key: 'delivery',
              label: '运行工作流',
              children: (
                <Modal
                  title={deliveryActionLabel}
                  open={deliveryActionModalVisible}
                  width={960}
                  forceRender
                  footer={null}
                  onCancel={() => setDeliveryActionModalVisible(false)}
                >
                  <MutationFeedback
                    operations={[
                      {
                        mutation: directBuildMutation,
                        label: '触发构建',
                        target: directBuildMutation.variables?.applicationId,
                      },
                      {
                        mutation: createDeliveryPlanMutation,
                        label: '生成交付计划',
                        target: createDeliveryPlanMutation.variables?.applicationId,
                      },
                    ]}
                  />
                  {writeOutcomeUnknown(directBuildMutation.error) ? (
                    <Button
                      onClick={() =>
                        navigate(
                          `/execution-history?${new URLSearchParams({ applicationId: directBuildMutation.variables?.applicationId || createDeliveryPlanMutation.variables?.applicationId || applicationId || '', ...(directBuildMutation.variables?.buildSourceId ? { buildSourceId: directBuildMutation.variables.buildSourceId } : {}) })}`,
                        )
                      }
                    >
                      核对执行记录
                    </Button>
                  ) : null}
                  {writeOutcomeUnknown(createDeliveryPlanMutation.error) ? (
                    <Alert
                      type="warning"
                      showIcon
                      title="未收到计划标识，当前无法直接重读该计划"
                      description="请先核对审计记录中的请求 ID；确认结果前暂停再次生成计划。生成计划本身不会启动交付。"
                    />
                  ) : null}
                  <Form
                    form={deliveryForm}
                    layout="vertical"
                    size="middle"
                    className="soha-application-delivery-actions__form"
                    disabled={deliveryActionPending}
                  >
                    <div className="soha-application-delivery-actions__grid">
                      <Form.Item
                        name="applicationEnvironmentId"
                        label="环境"
                        hidden={deliveryActionKind === 'build' && !bindings.length}
                        rules={
                          deliveryActionKind === 'build'
                            ? []
                            : [{ required: true, message: '请选择环境' }]
                        }
                      >
                        <Select
                          options={(manifestDelivery
                            ? bindings.filter((binding) =>
                                binding.targets?.some(
                                  (target) => target.executorKind === 'manifest_ssa',
                                ),
                              )
                            : deliveryActionKind === 'build'
                              ? bindings.filter(
                                  (binding) =>
                                    binding.applicationEnvironmentId === activeEnvironmentId,
                                )
                              : configuredWorkflowBindings
                          ).map((binding) => ({
                            value: binding.applicationEnvironmentId,
                            label:
                              binding.environmentName ||
                              binding.environmentKey ||
                              binding.environmentId,
                          }))}
                          onChange={(id) => {
                            populateDeliveryBinding(id)
                            if (manifestDelivery)
                              deliveryForm.setFieldValue(
                                'targetId',
                                bindings
                                  .find((binding) => binding.applicationEnvironmentId === id)
                                  ?.targets?.find(
                                    (target) =>
                                      target.enabled && target.executorKind === 'manifest_ssa',
                                  )?.id,
                              )
                          }}
                        />
                      </Form.Item>
                      <Form.Item
                        name="targetId"
                        label={manifestDelivery ? '资源包目标' : '服务 / Workload'}
                        hidden={deliveryActionKind === 'build'}
                        rules={
                          manifestDelivery ? [{ required: true, message: '请选择资源包目标' }] : []
                        }
                      >
                        <Select
                          allowClear
                          placeholder="选择服务或 Workload"
                          onChange={() => deliveryForm.setFieldValue('manifestRevision', undefined)}
                          options={(selectedDeliveryBinding?.targets ?? [])
                            .filter(
                              (target) =>
                                !manifestDelivery || target.executorKind === 'manifest_ssa',
                            )
                            .map((target) => {
                              const workload = environments
                                .find(
                                  (environment) =>
                                    environment.applicationEnvironmentId ===
                                    selectedDeliveryBinding?.applicationEnvironmentId,
                                )
                                ?.workloads?.find(
                                  (item) =>
                                    item.workloadName === target.workloadName &&
                                    item.namespace === target.namespace,
                                )
                              const service = services.find(
                                (item) =>
                                  item.id === workload?.serviceId ||
                                  item.key === workload?.serviceKey,
                              )
                              return {
                                value: target.id,
                                disabled: !target.enabled,
                                label:
                                  target.executorKind === 'manifest_ssa'
                                    ? String(
                                        target.metadata?.manifestPackageName || target.workloadName,
                                      )
                                    : deliveryTargetSummary(target, service?.name),
                              }
                            })}
                        />
                      </Form.Item>
                      {manifestDelivery ? (
                        <Form.Item
                          name="manifestRevision"
                          label="资源包版本"
                          tooltip="留空使用最新已发布版本；选择旧版本将重新预检并创建新的交付计划。"
                        >
                          <InputNumber
                            min={1}
                            precision={0}
                            placeholder="最新已发布版本"
                            style={{ width: '100%' }}
                          />
                        </Form.Item>
                      ) : (
                        <>
                          <Form.Item name="buildSourceId" label="构建定义">
                            <Select
                              allowClear
                              options={serviceBuildSourceOptions}
                              onChange={(sourceId) => {
                                const source = application.buildSources?.find(
                                  (item) => item.id === sourceId,
                                )
                                const repositoryRefs = buildRepositoryRefs(source, repositories)
                                const primaryRepositoryRef = repositoryRefs[0]
                                deliveryForm.setFieldsValue({
                                  repositoryRefs,
                                  refType: primaryRepositoryRef?.refType ?? 'branch',
                                  refName: primaryRepositoryRef?.refName ?? 'main',
                                  imageTag: source?.defaultTag || application.defaultTag,
                                })
                              }}
                            />
                          </Form.Item>
                          {selectedBuildRepositoryBindings.length > 0 ? (
                            <div className="soha-application-delivery-repository-refs">
                              <div className="soha-application-delivery-repository-refs__head">
                                <Text strong>运行版本</Text>
                                <Text type="secondary">
                                  {`${selectedBuildRepositoryBindings.length} 个源码仓库`}
                                </Text>
                              </div>
                              {selectedBuildRepositoryBindings.map((binding, index) => (
                                <RepositoryRefField
                                  key={binding.repositoryId}
                                  form={deliveryForm}
                                  index={index}
                                  binding={binding}
                                  repository={repositories.find(
                                    (item) => item.id === binding.repositoryId,
                                  )}
                                />
                              ))}
                            </div>
                          ) : (
                            <>
                              <Form.Item name="refType" label="版本类型">
                                <Select options={REF_TYPE_OPTIONS} />
                              </Form.Item>
                              <Form.Item name="refName" label="分支 / Tag / Commit">
                                <Input placeholder="main" />
                              </Form.Item>
                            </>
                          )}
                          <Form.Item name="imageTag" label="镜像 Tag">
                            <Input
                              placeholder={
                                selectedBuildSource?.defaultTag || application.defaultTag || '必填'
                              }
                            />
                          </Form.Item>
                          <Form.Item
                            name="releaseName"
                            label="发布名称"
                            hidden={deliveryActionKind === 'build'}
                          >
                            <Input
                              placeholder={
                                effectiveImageTag ||
                                selectedDeliveryBinding?.applicationEnvironmentId ||
                                'release'
                              }
                            />
                          </Form.Item>
                          <Form.Item
                            name="containerName"
                            label="容器"
                            hidden={deliveryActionKind === 'build'}
                          >
                            <Input
                              placeholder={selectedDeliveryTarget?.containerName || '默认容器'}
                            />
                          </Form.Item>
                        </>
                      )}
                    </div>
                    <div className="soha-application-delivery-actions__footer">
                      <div className="soha-application-delivery-actions__summary">
                        {deliveryActionKind !== 'build' && deliveryTargetCapabilityReason ? (
                          <Alert
                            showIcon
                            type="warning"
                            title="当前目标集群限制交付写入"
                            description={deliveryTargetCapabilityReason}
                          />
                        ) : null}
                        {deliveryActionKind === 'build' ? (
                          <Text type="secondary">
                            只构建产物，镜像 Tag：{effectiveImageTag || '未设置'}
                          </Text>
                        ) : manifestDelivery ? (
                          <Text type="secondary">使用资源包中已固定的镜像与环境配置。</Text>
                        ) : (
                          <Space wrap>
                            <MetadataTag
                              label={
                                selectedDeliveryBinding?.workflowTemplateName ||
                                selectedDeliveryBinding?.workflowTemplate?.name ||
                                '未绑定工作流'
                              }
                            />
                            <MetadataTag
                              label={`${selectedDeliveryBinding?.targetCount ?? 0} 个 Workload 目标`}
                            />
                            <MetadataTag label={`${validationNodeCount} 个测试节点`} />
                            {effectiveImageTag ? (
                              <MetadataTag label={`镜像 Tag ${effectiveImageTag}`} />
                            ) : (
                              <StatusTag value="warning" label="缺少镜像 Tag" />
                            )}
                          </Space>
                        )}
                      </div>
                      <Space>
                        <Button onClick={() => setDeliveryActionModalVisible(false)}>取消</Button>
                        <Tooltip title={buildDeployDisabledReason || '按工作流模板执行'}>
                          <Button
                            type="primary"
                            icon={<PlayCircleOutlined />}
                            disabled={deliveryUnknown || !!buildDeployDisabledReason}
                            loading={deliveryActionPending}
                            onClick={() => void triggerDeliveryAction(deliveryActionKind)}
                          >
                            {deliveryActionLabel}
                          </Button>
                        </Tooltip>
                      </Space>
                    </div>
                  </Form>
                </Modal>
              ),
            },
            {
              key: 'pipeline',
              label: '工作流',
              children: (
                <div className="soha-application-runtime-pipeline-grid">
                  <div className="soha-application-workflow-list">
                    <OverviewSectionBar
                      title="构建与发布工作流"
                      description="编排构建、部署、测试与审批，执行结果按当前环境展示"
                      extra={
                        canUpdateApplicationEnvironment ? (
                          <Button
                            type="primary"
                            icon={<PlusOutlined />}
                            onClick={openWorkflowCreate}
                          >
                            创建工作流
                          </Button>
                        ) : null
                      }
                    />
                    {runtimePending || (!detail && detailQuery.isPending) ? (
                      <ManagementState compact kind="loading" title="正在读取环境工作流" />
                    ) : runtimeUnavailable || (!detail && detailQuery.isError) ? (
                      <ManagementState
                        compact
                        kind="error"
                        title="环境工作流读取失败"
                        actions={
                          <Button
                            onClick={() => {
                              void runtimeQuery.refetch()
                              void detailQuery.refetch()
                            }}
                          >
                            重试
                          </Button>
                        }
                      />
                    ) : configuredWorkflowBindings.length > 0 ? (
                      <div className="soha-application-long-card-list" role="list">
                        {configuredWorkflowBindings.map((binding) => (
                          <Card
                            className="soha-application-workflow-card"
                            key={binding.applicationEnvironmentId}
                            role="listitem"
                          >
                            <div className="soha-application-runtime-binding-row__head">
                              <div className="soha-application-runtime-binding-row__main">
                                <strong>
                                  {binding.workflowTemplate?.name ||
                                    binding.workflowTemplateName ||
                                    binding.workflowTemplateId}
                                </strong>
                                <Text type="secondary">
                                  {`${binding.environmentName || binding.environmentKey || binding.environmentId} · ${binding.workflowTemplate?.key || binding.workflowTemplateId}`}
                                </Text>
                              </div>
                              <Space wrap>
                                <StatusTag
                                  value={binding.latestWorkflow?.status || 'default'}
                                  label={binding.latestWorkflow ? undefined : '未运行'}
                                />
                                <Button
                                  type="primary"
                                  icon={<PlayCircleOutlined />}
                                  disabled={
                                    !binding.workflowTemplate ||
                                    !canViewWorkflows ||
                                    !canTriggerBuild ||
                                    !canTriggerWorkflow
                                  }
                                  onClick={() => {
                                    setDeliveryActionKind('build_deploy')
                                    populateDeliveryBinding(
                                      binding.applicationEnvironmentId,
                                      focusedService?.buildSourceId,
                                    )
                                    const target = binding.targets?.find((item) =>
                                      selectedWorkloads.some(
                                        (workload) =>
                                          workload.workloadName === item.workloadName &&
                                          workload.namespace === item.namespace &&
                                          workload.clusterId === item.clusterId,
                                      ),
                                    )
                                    if (target) deliveryForm.setFieldValue('targetId', target.id)
                                    setDeliveryActionModalVisible(true)
                                  }}
                                >
                                  运行
                                </Button>
                                {canUpdateApplicationEnvironment ? (
                                  <Button
                                    icon={<LinkOutlined />}
                                    onClick={() => openWorkflowDesigner(binding)}
                                  >
                                    设计
                                  </Button>
                                ) : null}
                              </Space>
                            </div>
                            <Text type="secondary">
                              最近运行：{formatDateTime(binding.latestWorkflow?.createdAt)}
                            </Text>
                            <WorkflowProgress
                              binding={binding}
                              onSelectStage={
                                canViewWorkflows
                                  ? (runId, stageKey) =>
                                      navigate(
                                        `/workflows/${encodeURIComponent(runId)}?${new URLSearchParams({ stage: stageKey })}`,
                                      )
                                  : undefined
                              }
                            />
                            <details className="soha-application-inline-disclosure">
                              <summary>配置检查</summary>
                              {renderWorkflowTemplateHealth(binding)}
                            </details>
                          </Card>
                        ))}
                      </div>
                    ) : (
                      <ManagementState
                        bordered={false}
                        kind="not-configured"
                        title="还没有工作流"
                        description="创建工作流以连接构建、部署与测试；可从预设模板开始。"
                      />
                    )}
                  </div>
                </div>
              ),
            },
          ]
          const tab = (key: string) => existingTabs.find((item) => item.key === key)!
          const sections = [
            {
              key: 'delivery',
              label: APPLICATION_WORKSPACE_LABELS.delivery,
              children: (
                <div className="soha-application-workflow-workspace">
                  {focusedService ? (
                    <Suspense fallback={<ManagementState compact kind="loading" />}>
                      <ServiceDeliveryForm
                        service={focusedService}
                        applicationEnvironmentId={activeEnvironmentId}
                        onClose={() => setActiveTab('services')}
                      />
                    </Suspense>
                  ) : null}
                  <details className="soha-application-inline-disclosure">
                    <summary>环境工作流</summary>
                    {tab('pipeline').children}
                    {applicationWorkflowOverview}
                  </details>
                </div>
              ),
            },
            { ...tab('services'), label: APPLICATION_WORKSPACE_LABELS.services },
            {
              key: 'resources',
              label: APPLICATION_WORKSPACE_LABELS.resources,
              children: (
                <ManifestLibraryWorkspace applicationId={applicationId ?? ''} services={services} />
              ),
            },
            {
              ...tab('settings'),
              key: 'application',
              label: APPLICATION_WORKSPACE_LABELS.application,
              children: (
                <div className="soha-application-service-config-workspace">
                  {tab('settings').children}
                </div>
              ),
            },
            tab('environment-bindings'),
            { ...tab('permissions'), label: APPLICATION_WORKSPACE_LABELS.permissions },
          ]
          return (
            <>
              {tab('delivery').children}
              {!invalidEnvironment ? tab('services').children : null}
              <Drawer
                className={
                  activeTab === 'delivery' ? undefined : 'soha-application-settings-drawer'
                }
                title={
                  activeTab === 'delivery' ? '构建与更新' : `${settingsLabel} · ${application.name}`
                }
                open={
                  (activeTab === 'delivery' &&
                    !deliveryActionModalVisible &&
                    !deliveryPlanModalVisible) ||
                  Boolean(settingsKey)
                }
                size="min(1080px, 100vw)"
                destroyOnHidden
                onClose={() => {
                  setServiceModalVisible(false)
                  setEditingService(null)
                  setActiveTab('services')
                }}
              >
                {activeTab === 'delivery' ? (
                  sections.find((item) => item.key === 'delivery')?.children
                ) : (
                  <div className="soha-application-settings">
                    <nav
                      className="soha-application-settings__navigation"
                      aria-label={localeText(localeCode, '设置分区', 'Settings sections')}
                    >
                      {APPLICATION_SETTINGS_NAV_ITEMS.map((item) => (
                        <button
                          key={item.key}
                          id={`application-settings-${item.key}`}
                          type="button"
                          aria-current={settingsKey === item.key ? 'page' : undefined}
                          aria-controls="application-settings-content"
                          className={item.key === 'resources' ? 'is-extension' : undefined}
                          onClick={() => {
                            setServiceModalVisible(false)
                            setEditingService(null)
                            const next = new URLSearchParams(searchParams)
                            next.set('settings', item.key)
                            setSearchParams(next, { replace: true })
                          }}
                        >
                          {localeText(localeCode, item.label, item.labelEn)}
                        </button>
                      ))}
                    </nav>
                    <section
                      key={settingsKey}
                      id="application-settings-content"
                      className="soha-application-settings__content"
                      aria-labelledby={`application-settings-${settingsKey}`}
                    >
                      {serviceModalVisible && settingsKey === 'application' ? (
                        <ServiceEditor
                          application={application}
                          service={editingService}
                          services={services}
                          repositories={repositories}
                          canManageBuild={canManageRepositories}
                          onCancel={() => {
                            setServiceModalVisible(false)
                            setEditingService(null)
                          }}
                          onSaved={(id) => {
                            setServiceModalVisible(false)
                            setEditingService(null)
                            const hasEnvironment =
                              environments.some((environment) =>
                                environment.workloads?.some(
                                  (workload) => workload.serviceId === id,
                                ),
                              ) ||
                              bindings.some((binding) =>
                                binding.targets?.some(
                                  (target) => target.metadata?.serviceId === id,
                                ),
                              )
                            setFocusedService(
                              id,
                              'pods',
                              !hasEnvironment && canUpdateApplicationEnvironment
                                ? 'environment-bindings'
                                : undefined,
                            )
                          }}
                        />
                      ) : (
                        sections.find((item) => item.key === settingsKey)?.children
                      )}
                    </section>
                  </div>
                )}
              </Drawer>
            </>
          )
        })()}
      </div>
      <Modal
        title={
          editingRepositoryId
            ? '编辑代码仓库'
            : repositoryBindingTarget !== null
              ? '从代码源接入仓库'
              : '添加代码仓库'
        }
        open={repositoryModalVisible}
        footer={null}
        destroyOnHidden
        width={720}
        onCancel={() => {
          closeRepositoryModal()
        }}
      >
        <Form
          form={repositoryForm}
          layout="vertical"
          onFinish={(values) => {
            const payload = {
              ...values,
              applicationIds: [applicationId],
              gitlabProjectId: values.provider === 'gitlab' ? values.gitlabProjectId : undefined,
            }
            if (editingRepositoryId)
              updateRepositoryMutation.mutate({ id: editingRepositoryId, payload })
            else createRepositoryMutation.mutate(payload)
          }}
        >
          <RepositoryFields form={repositoryForm} active={repositoryModalVisible} />
          <div className="soha-form-actions">
            <Button onClick={closeRepositoryModal}>取消</Button>
            <Button
              htmlType="submit"
              type="primary"
              loading={createRepositoryMutation.isPending || updateRepositoryMutation.isPending}
            >
              保存
            </Button>
          </div>
        </Form>
      </Modal>
      <Modal
        title={editingBuildSourceId ? '编辑构建' : '添加构建'}
        open={buildSourceModalVisible}
        footer={null}
        destroyOnHidden
        width={920}
        onCancel={() => {
          setBuildSourceModalVisible(false)
          setEditingBuildSourceId('')
          setBuildSourceDraft(null)
          buildSourceForm.resetFields()
        }}
      >
        <MutationFeedback
          operations={[
            {
              mutation: managementState.updateAppMutation,
              label: '保存构建配置',
              target: managementState.updateAppMutation.variables?.id,
            },
          ]}
        />
        <Form
          form={buildSourceForm}
          layout="vertical"
          initialValues={buildSourceDraft ?? undefined}
          onFinish={() => {
            if (
              appConfigSubmitting.current ||
              managementState.updateAppMutation.isPending ||
              writeOutcomeUnknown(managementState.updateAppMutation.error)
            )
              return
            appConfigSubmitting.current = true
            const values = buildSourceForm.getFieldsValue(true) as BuildSourceFormValues
            const snapshot = buildSourceApplication ?? application
            const sources = [...(snapshot.buildSources ?? [])]
            const repositoryBindings = buildRepositoryBindings(values)
            const source = {
              ...values,
              id: editingBuildSourceId || `source-${Date.now()}`,
              config: {
                ...values.config,
                repositoryId: repositoryBindings[0]?.repositoryId,
                repositoryBindings,
              },
            }
            const normalized = (
              source.isDefault ? sources.map((item) => ({ ...item, isDefault: false })) : sources
            )
              .filter((item) => item.id !== source.id)
              .concat(source as BuildSource)
            managementState.updateAppMutation.mutate(
              {
                id: application.id,
                payload: {
                  ...snapshot,
                  expectedVersion: snapshot.version,
                  buildSources: normalized,
                },
              },
              {
                onSettled: () => {
                  appConfigSubmitting.current = false
                },
                onSuccess: () => {
                  setBuildSourceModalVisible(false)
                  setEditingBuildSourceId('')
                  setBuildSourceDraft(null)
                  buildSourceForm.resetFields()
                },
              },
            )
          }}
        >
          {isApiError(managementState.updateAppMutation.error) &&
          (managementState.updateAppMutation.error.status === 409 ||
            writeOutcomeUnknown(managementState.updateAppMutation.error)) ? (
            <Alert
              type="warning"
              title={
                writeOutcomeUnknown(managementState.updateAppMutation.error)
                  ? '保存结果待核对'
                  : '应用配置已被其他人更新'
              }
              description="当前草稿已保留。重新加载会放弃本次修改并载入最新配置。"
              action={
                <Button
                  loading={reloadBuildSource.isPending}
                  onClick={() => reloadBuildSource.mutate()}
                >
                  重新加载构建
                </Button>
              }
            />
          ) : null}
          <BuildSourceFields
            applicationId={applicationId}
            form={buildSourceForm}
            repositories={repositories}
            repositoriesLoading={repositoriesQuery.isFetching}
            onConnectRepository={(index) => openRepositoryModal('', index)}
          />
          <div className="soha-form-actions">
            <Button onClick={() => setBuildSourceModalVisible(false)}>取消</Button>
            <Button
              htmlType="submit"
              type="primary"
              disabled={writeOutcomeUnknown(managementState.updateAppMutation.error)}
              loading={managementState.updateAppMutation.isPending}
            >
              保存
            </Button>
          </div>
        </Form>
      </Modal>
      <ApplicationCenterModals state={managementState} />
    </div>
  )
}
