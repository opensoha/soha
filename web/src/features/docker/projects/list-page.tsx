import { readableQueryData, writeOutcomeUnknown } from '@/utils/query-feedback'
import { ReadFeedback } from '@/components/request-feedback'
import { ComputeOperationFeedback, ComputeMutationFeedback } from '@/features/compute'
import { useEffect, useMemo, useRef, useState } from 'react'
import type { Key, ReactNode } from 'react'
import {
  App,
  Button,
  Descriptions,
  Form,
  Input,
  InputNumber,
  Popconfirm,
  Segmented,
  Select,
  Space,
  Switch,
  Tabs,
  Typography,
} from 'antd'
import type { ColumnsType } from 'antd/es/table'
import {
  BranchesOutlined,
  CloudOutlined,
  DeleteOutlined,
  EditOutlined,
  FileTextOutlined,
  MinusCircleOutlined,
  PlayCircleOutlined,
  PlusOutlined,
  PoweroffOutlined,
  ReloadOutlined,
  SyncOutlined,
} from '@ant-design/icons'
import { useMutation, useQueries, useQuery, useQueryClient } from '@tanstack/react-query'
import { Link } from 'react-router-dom'
import { accessQueries } from '@/features/access'
import { hasPermission, usePermissionSnapshot } from '@/features/auth'
import {
  ManagementIconButton,
  ManagementKeywordField,
  ManagementQueryActions,
  ManagementQueryField,
  ManagementQueryPanel,
} from '@/components/management-list'
import { StepFormModal } from '@/components/step-form-modal'
import { OperationalPlanModal } from '@/components/operational-plan-modal'
import { MetadataTag } from '@/components/status-tag'
import type { OperationalPlan } from '@opensoha/contracts/gen/ts/sohaapi'
import { createUUID } from '@/utils/uuid'
import { tableColumnPresets } from '@/utils/table-columns'
import { computeQueries, latestTaskForResource, ResourceTaskActions } from '@/features/compute'
import { localeText, useI18n } from '@/i18n'
import { formatStatusLabel } from '@/i18n/status'
import { sourceControlApi, sourceControlQueries } from '@/features/settings'
import { dockerApi } from '../docker-api'
import { dockerQueries } from '../queries'
import type {
  DockerContainerStartInput,
  DockerProject,
  DockerProjectInput,
  DockerService,
} from '../docker-types'
import {
  ARCHITECTURE_OPTIONS,
  DEFAULT_COMPOSE,
  DEFAULT_CONTAINER_PORTS,
  DockerAdminTable,
  bytesFromMiB,
  compactRecord,
  normalizePage,
  operationActionLabel,
  pageTablePagination,
  refreshDocker,
  renderProjectPortSummary,
  stringValue,
  statusTag,
  type DockerFilterState,
  useDockerOptions,
  useDockerPermissions,
} from '../shared/ui'

const { Text } = Typography
const { TextArea } = Input

type ContainerStartResourceFormValues = NonNullable<DockerContainerStartInput['resources']> & {
  memoryMiB?: number
  memoryReservationMiB?: number
}

interface ContainerStartFormValues extends Omit<DockerContainerStartInput, 'resources'> {
  resources?: ContainerStartResourceFormValues
}

interface DockerProjectTreeRow {
  key: string
  kind: 'project' | 'service'
  project: DockerProject
  service?: DockerService
  children?: DockerProjectTreeRow[]
}

interface ComposeProjectFormValues extends DockerProjectInput {
  sourceConnectionId?: string
  sourceRepositoryId?: string
  sourceRevision?: string
  sourcePath?: string
}

function isSingleContainerProject(project: DockerProject) {
  return ['single_container', 'git_dockerfile'].includes(project.sourceKind ?? '')
}

function projectTypeLabel(project: DockerProject, localeCode: 'zh_CN' | 'en_US') {
  return isSingleContainerProject(project)
    ? localeText(localeCode, '单容器', 'Single container')
    : 'Compose'
}

function QuickStartFormSection({ children, title }: { children: ReactNode; title: string }) {
  return (
    <section className="soha-docker-quick-form-section">
      <h2>{title}</h2>
      {children}
    </section>
  )
}

export function buildProjectPayload(values: ComposeProjectFormValues): DockerProjectInput {
  const { sourceConnectionId, sourceRepositoryId, sourceRevision, sourcePath, ...project } = values
  const gitSource = project.sourceKind === 'git'
  const config = compactRecord({
    ...project.config,
    sourceConnectionId: gitSource ? sourceConnectionId : undefined,
    sourceRepositoryId: gitSource ? sourceRepositoryId : undefined,
    sourceRevision: gitSource ? sourceRevision : undefined,
    sourcePath: gitSource ? sourcePath : undefined,
  })
  return compactRecord({
    ...project,
    composeContent:
      project.sourceKind === 'url' ? undefined : project.composeContent || DEFAULT_COMPOSE,
    desiredState: project.desiredState || 'running',
    status: project.status || 'draft',
    sourceKind: project.sourceKind || 'inline_compose',
    ttlSeconds: project.ttlSeconds || 600,
    config: Object.keys(config).length ? config : undefined,
  })
}

function decodeSourceFile(content: string, encoding: 'utf8' | 'base64') {
  if (encoding === 'utf8') return content
  return new TextDecoder().decode(Uint8Array.from(atob(content), (char) => char.charCodeAt(0)))
}

function projectFormValues(project: DockerProject): ComposeProjectFormValues {
  return {
    ...project,
    sourceConnectionId: stringValue(project.config?.sourceConnectionId),
    sourceRepositoryId: stringValue(project.config?.sourceRepositoryId),
    sourceRevision: stringValue(project.config?.sourceRevision),
    sourcePath: stringValue(project.config?.sourcePath),
  }
}

export function buildContainerStartPayload(
  values: ContainerStartFormValues,
): DockerContainerStartInput {
  const {
    gitBuild: formGitBuild,
    ports: formPorts,
    volumes: formVolumes,
    environmentVariables: formEnvVars,
    resources: formResources,
    ...rest
  } = values
  const ports = (formPorts ?? [])
    .map((port) =>
      compactRecord({
        ...port,
        protocol: port.protocol || rest.protocol || 'tcp',
        exposureScope: port.exposureScope || rest.exposureScope || 'internal',
        domainScheme: port.domainName
          ? port.domainScheme || (port.domainTlsEnabled ? 'https' : 'http')
          : undefined,
        domainTlsEnabled: port.domainName ? Boolean(port.domainTlsEnabled) : undefined,
      }),
    )
    .filter((port) => port.hostPort || port.containerPort)
  const volumes = (formVolumes ?? [])
    .map((volume) =>
      compactRecord({
        ...volume,
        type: volume.type || undefined,
        readOnly: Boolean(volume.readOnly),
      }),
    )
    .filter((volume) => volume.source || volume.target)
  const environmentVariables = (formEnvVars ?? [])
    .map((item) => compactRecord(item))
    .filter((item) => item.name || item.value)
  const resources = compactRecord({
    cpus: formResources?.cpus && formResources.cpus > 0 ? formResources.cpus : undefined,
    memoryBytes: bytesFromMiB(formResources?.memoryMiB) ?? formResources?.memoryBytes,
    memoryReservationBytes:
      bytesFromMiB(formResources?.memoryReservationMiB) ?? formResources?.memoryReservationBytes,
  })
  const primaryPort = ports[0]
  return compactRecord({
    ...rest,
    sourceKind: rest.sourceKind || 'image',
    gitBuild:
      rest.sourceKind === 'git_dockerfile'
        ? compactRecord({
            repositoryUrl: formGitBuild?.repositoryUrl ?? '',
            ref: formGitBuild?.ref || 'main',
            dockerfilePath: formGitBuild?.dockerfilePath || 'Dockerfile',
            contextDir: formGitBuild?.contextDir || '.',
            pull: Boolean(formGitBuild?.pull),
            noCache: Boolean(formGitBuild?.noCache),
          })
        : undefined,
    architecture: rest.architecture || undefined,
    containerPort: primaryPort?.containerPort ?? rest.containerPort,
    hostIp: primaryPort?.hostIp ?? rest.hostIp,
    hostPort: primaryPort?.hostPort ?? rest.hostPort,
    protocol: primaryPort?.protocol || rest.protocol || 'tcp',
    exposureScope: primaryPort?.exposureScope || rest.exposureScope || 'internal',
    domainName: primaryPort?.domainName ?? rest.domainName,
    domainScheme: primaryPort?.domainName
      ? primaryPort.domainScheme || (primaryPort.domainTlsEnabled ? 'https' : 'http')
      : undefined,
    domainTlsEnabled: primaryPort?.domainName ? Boolean(primaryPort.domainTlsEnabled) : undefined,
    restartPolicy: rest.restartPolicy || 'unless-stopped',
    imagePullPolicy:
      rest.sourceKind === 'git_dockerfile' ? 'never' : rest.imagePullPolicy || undefined,
    ports: ports.length ? ports : undefined,
    volumes: volumes.length ? volumes : undefined,
    environmentVariables: environmentVariables.length ? environmentVariables : undefined,
    resources: Object.keys(resources).length ? resources : undefined,
  })
}

function ProjectsTable({ embedded = false }: { embedded?: boolean }) {
  const [filters, setFilters] = useState<DockerFilterState>({
    page: 1,
    pageSize: embedded ? 5 : 15,
  })
  const [filterForm] = Form.useForm<DockerFilterState>()
  const [form] = Form.useForm<ComposeProjectFormValues>()
  const [containerForm] = Form.useForm<ContainerStartFormValues>()
  const [drawerOpen, setDrawerOpen] = useState(false)
  const [currentStep, setCurrentStep] = useState(0)
  const [containerStep, setContainerStep] = useState(0)
  const [containerDrawerOpen, setContainerDrawerOpen] = useState(false)
  const [containerSourceKind, setContainerSourceKind] = useState<'image' | 'git_dockerfile'>(
    'image',
  )
  const [containerReviewValues, setContainerReviewValues] = useState<
    Partial<ContainerStartFormValues>
  >({})
  const [editing, setEditing] = useState<DockerProject | null>(null)
  const [deployPlan, setDeployPlan] = useState<{
    action: string
    idempotencyKey: string
    plan: OperationalPlan
    project: DockerProject
  } | null>(null)
  const {
    dockerModuleEnabled,
    canCreateProjects,
    canUpdateProjects,
    canDeleteProjects,
    canDeployProjects,
    canCreatePorts,
    canStartServices,
    canStopServices,
    canRestartServices,
    canViewServices,
    canViewOperations,
  } = useDockerPermissions()
  const { hosts, hostOptions } = useDockerOptions({
    includeProjects: false,
    includeServices: false,
  })
  const queryClient = useQueryClient()
  const { message } = App.useApp()
  const { localeCode } = useI18n()
  const permissionSnapshot = usePermissionSnapshot().data?.data
  const sourceKind = Form.useWatch('sourceKind', form) || 'inline_compose'
  const sourceConnectionId = Form.useWatch('sourceConnectionId', form) || ''
  const sourceRepositoryId = Form.useWatch('sourceRepositoryId', form) || ''
  const canViewSourceControl = hasPermission(
    permissionSnapshot,
    'settings.system-integrations.view',
  )
  const canViewUsers = hasPermission(permissionSnapshot, 'access.users.view')
  const canViewTeams = hasPermission(permissionSnapshot, 'access.groups.view')
  const projectsQuery = useQuery(dockerQueries.projects(filters, dockerModuleEnabled))
  const templatesQuery = useQuery(
    dockerQueries.templates(
      { page: 1, pageSize: 200, enabled: true },
      dockerModuleEnabled && drawerOpen && sourceKind === 'template',
    ),
  )
  const usersQuery = useQuery(accessQueries.users(drawerOpen && canViewUsers))
  const teamsQuery = useQuery(accessQueries.teams(drawerOpen && canViewTeams))
  const sourceConnectionsQuery = useQuery(
    sourceControlQueries.connections(drawerOpen && canViewSourceControl && sourceKind === 'git'),
  )
  const sourceRepositoriesQuery = useQuery(
    sourceControlQueries.repositories(sourceConnectionId, drawerOpen && sourceKind === 'git'),
  )
  const sourceBranchesQuery = useQuery(
    sourceControlQueries.branches(
      sourceConnectionId,
      sourceRepositoryId,
      drawerOpen && sourceKind === 'git',
    ),
  )
  const templates = normalizePage(templatesQuery.data, 1, 200).items
  const tasksQuery = useQuery({
    ...computeQueries.tasks({ domain: 'container_runtime', limit: 100 }),
    enabled: dockerModuleEnabled && canViewOperations,
  })
  const saveMutation = useMutation({
    mutationFn: async (values: ComposeProjectFormValues) => {
      let resolved = {
        ...values,
        desiredState: editing?.desiredState,
        slug: editing?.slug,
        status: editing?.status,
      }
      if (values.sourceKind === 'template') {
        const template = templates.find((item) => item.id === values.templateId)
        if (!template?.composeContent) throw new Error('所选模板没有 Compose 配置')
        resolved = {
          ...resolved,
          composeContent: template.composeContent,
          envContent: template.envContent,
          sourceRef: template.name,
        }
      }
      if (values.sourceKind === 'git' && !canViewSourceControl && editing?.sourceKind === 'git') {
        resolved = {
          ...resolved,
          composeContent: editing.composeContent,
          config: editing.config,
          sourceRef: editing.sourceRef,
        }
      } else if (values.sourceKind === 'git') {
        const { sourceConnectionId, sourceRepositoryId, sourceRevision, sourcePath } = values
        if (!sourceConnectionId || !sourceRepositoryId || !sourceRevision || !sourcePath) {
          throw new Error('请选择代码源、仓库、分支并填写 Compose 文件路径')
        }
        const file = await sourceControlApi.file(
          sourceConnectionId,
          sourceRepositoryId,
          sourceRevision,
          sourcePath,
        )
        const repository = sourceRepositoriesQuery.data?.find(
          (item) => item.id === sourceRepositoryId,
        )
        resolved = {
          ...resolved,
          composeContent: decodeSourceFile(file.content, file.encoding),
          sourceRef: `${repository?.fullName || sourceRepositoryId}@${sourceRevision}:${sourcePath}`,
        }
      }
      const payload = buildProjectPayload(resolved)
      return editing
        ? dockerApi.updateProject(editing.id, payload)
        : dockerApi.createProject(payload)
    },
    onSuccess: () => {
      message.success(editing ? '项目已更新' : '项目已创建')
      setDrawerOpen(false)
      setEditing(null)
      form.resetFields()
      refreshDocker(queryClient)
    },
  })
  const containerStartMutation = useMutation({
    mutationFn: (values: ContainerStartFormValues) =>
      dockerApi.startContainer(buildContainerStartPayload(values)),
    onSuccess: () => {
      message.success('单容器启动任务已提交')
      setContainerDrawerOpen(false)
      containerForm.resetFields()
      refreshDocker(queryClient)
    },
  })
  const deleteMutation = useMutation({
    mutationFn: dockerApi.deleteProject,
    onSuccess: () => {
      message.success('项目已删除')
      refreshDocker(queryClient)
    },
  })
  const deployPlanMutation = useMutation({
    mutationFn: ({ id, action }: { id: string; action: string }) =>
      dockerApi.planProjectDeploy(id, action),
  })
  const deployMutation = useMutation({
    mutationFn: ({
      id,
      action,
      idempotencyKey,
    }: {
      id: string
      action: string
      idempotencyKey: string
    }) => dockerApi.deployProject(id, action, idempotencyKey),
    onSuccess: (_response, variables) => {
      message.success(`${operationActionLabel(variables.action)}任务已提交`)
      setDeployPlan(null)
      refreshDocker(queryClient)
    },
    onError: (error) => void message.error(error.message),
  })
  const reviewDeploy = (project: DockerProject, action: string) => {
    deployPlanMutation.mutate(
      { id: project.id, action },
      {
        onSuccess: (plan) => setDeployPlan({ action, idempotencyKey: createUUID(), plan, project }),
      },
    )
  }
  const serviceActionMutation = useMutation({
    mutationFn: ({ id, action }: { id: string; action: string }) =>
      dockerApi.serviceAction(id, action),
    onSuccess: (_response, variables) => {
      message.success(`${operationActionLabel(variables.action)}服务任务已提交`)
      refreshDocker(queryClient)
    },
  })
  const page = normalizePage(
    readableQueryData(projectsQuery),
    filters.page ?? 1,
    filters.pageSize ?? (embedded ? 5 : 15),
  )
  const projectServiceQueries = useQueries({
    queries: page.items.map((project) =>
      dockerQueries.projectServices(
        project.id,
        dockerModuleEnabled && canViewServices && !isSingleContainerProject(project),
      ),
    ),
  })
  const canStartContainer = canCreateProjects && canDeployProjects && canCreatePorts
  const ownerOptions = (usersQuery.data ?? []).map((user) => ({
    value: user.username,
    label: user.displayName ? `${user.displayName} (${user.username})` : user.username,
  }))
  const teamOptions = (teamsQuery.data ?? []).map((team) => ({
    value: team.slug || team.name,
    label: team.path || team.name,
  }))
  const sourceConnectionOptions = (sourceConnectionsQuery.data ?? []).map((connection) => ({
    value: connection.id,
    label: `${connection.name} (${connection.providerType})`,
  }))
  const sourceRepositoryOptions = (sourceRepositoriesQuery.data ?? [])
    .filter((repository) => !repository.archived)
    .map((repository) => ({ value: repository.id, label: repository.fullName }))
  const sourceRevisionOptions = (sourceBranchesQuery.data ?? []).map((branch) => ({
    value: branch.name,
    label: branch.defaultBranch ? `${branch.name}（默认）` : branch.name,
  }))
  const templateOptions = templates.map((template) => ({
    value: template.id,
    label: template.name,
  }))
  useEffect(() => {
    if (sourceKind !== 'git' || sourceConnectionId || !sourceConnectionsQuery.data?.length) return
    const connection =
      sourceConnectionsQuery.data.find((item) => item.defaultConnection) ??
      sourceConnectionsQuery.data[0]
    form.setFieldValue('sourceConnectionId', connection?.id)
  }, [form, sourceConnectionId, sourceConnectionsQuery.data, sourceKind])
  const serviceActions = [
    { action: 'restart', allowed: canRestartServices, icon: <ReloadOutlined /> },
    { action: 'start', allowed: canStartServices, icon: <PlayCircleOutlined /> },
    { action: 'stop', allowed: canStopServices, icon: <PoweroffOutlined /> },
  ].filter((item) => item.allowed)
  const [expandedRowKeys, setExpandedRowKeys] = useState<string[]>([])
  const autoExpandedProjectKeys = useRef(new Set<string>())
  const servicesByProject = useMemo(() => {
    const grouped = new Map<string, DockerService[]>()
    page.items.forEach((project, index) => {
      const query = projectServiceQueries[index]
      const services = canViewServices && query ? (readableQueryData(query)?.items ?? []) : []
      if (services.length > 0) grouped.set(project.id, services)
    })
    return grouped
  }, [canViewServices, page.items, projectServiceQueries])
  const treeRows = useMemo<DockerProjectTreeRow[]>(
    () =>
      page.items.map((project) => {
        const children = isSingleContainerProject(project)
          ? []
          : (servicesByProject.get(project.id) ?? []).map<DockerProjectTreeRow>((service) => ({
              key: `${project.id}:service:${service.id}`,
              kind: 'service',
              project,
              service,
            }))
        return {
          key: project.id,
          kind: 'project',
          project,
          children: children.length > 0 ? children : undefined,
        }
      }),
    [page.items, servicesByProject],
  )
  useEffect(() => {
    const newlyExpandableKeys = treeRows
      .filter((row) => (row.children?.length ?? 0) > 0)
      .map((row) => row.key)
      .filter((key) => !autoExpandedProjectKeys.current.has(key))
    if (newlyExpandableKeys.length === 0) return
    newlyExpandableKeys.forEach((key) => autoExpandedProjectKeys.current.add(key))
    setExpandedRowKeys((current) => {
      const next = Array.from(new Set([...current, ...newlyExpandableKeys]))
      return next.length === current.length && next.every((key, index) => key === current[index])
        ? current
        : next
    })
  }, [treeRows])
  const applyContainerHostDefaults = (hostID?: string) => {
    const host = hosts.find((item) => item.id === hostID)
    if (host?.architecture) {
      containerForm.setFieldsValue({ architecture: host.architecture })
    }
  }
  const columns: ColumnsType<DockerProjectTreeRow> = [
    {
      title: localeText(localeCode, '项目', 'Project'),
      fixed: 'left',
      width: 190,
      render: (_value, record) =>
        record.kind === 'project' ? (
          <Space orientation="vertical" size={4}>
            <Link to={`/compute/runtimes/projects/${record.project.id}`}>
              {record.project.name}
            </Link>
            <MetadataTag label={projectTypeLabel(record.project, localeCode)} tone="purple" />
          </Space>
        ) : (
          <Text type="secondary">-</Text>
        ),
    },
    {
      title: localeText(localeCode, '服务', 'Service'),
      width: 190,
      render: (_value, record) =>
        record.kind === 'service' && record.service ? (
          <Space orientation="vertical" size={4}>
            <Space size={4}>
              <Text>{record.service.name}</Text>
              <MetadataTag label={localeText(localeCode, '服务', 'Service')} tone="cyan" />
            </Space>
            <Text type="secondary">{record.service.containerId || record.service.id}</Text>
          </Space>
        ) : (
          stringValue(record.project.config?.serviceName) || <Text type="secondary">-</Text>
        ),
    },
    {
      title: localeText(localeCode, '状态', 'Status'),
      width: 110,
      render: (_value, record) =>
        statusTag(record.kind === 'service' ? record.service?.status : record.project.status),
    },
    {
      title: localeText(localeCode, '主机', 'Host'),
      width: 170,
      render: (_value, record) => {
        const hostId = record.kind === 'service' ? record.service?.hostId : record.project.hostId
        return hostOptions.find((item) => item.value === hostId)?.label || hostId || '-'
      },
    },
    {
      title: localeText(localeCode, '镜像', 'Image'),
      width: 210,
      render: (_value, record) => {
        const image =
          record.kind === 'service'
            ? record.service?.image
            : stringValue(record.project.config?.image)
        return image ? (
          <MetadataTag
            label={<span className="soha-docker-image-value">{image}</span>}
            tone="blue"
          />
        ) : (
          <Text type="secondary">-</Text>
        )
      },
    },
    {
      title: localeText(localeCode, '端口', 'Ports'),
      width: 220,
      render: (_value, record) =>
        record.kind === 'project' && isSingleContainerProject(record.project) ? (
          renderProjectPortSummary(record.project)
        ) : (
          <Text type="secondary">-</Text>
        ),
    },
    {
      ...tableColumnPresets.task,
      title: localeText(localeCode, '最近任务', 'Latest task'),
      render: (_value, record) =>
        record.kind === 'service' ? (
          <Text type="secondary">-</Text>
        ) : (
          <ResourceTaskActions
            task={latestTaskForResource(
              canViewOperations ? (readableQueryData(tasksQuery)?.items ?? []) : [],
              'project',
              record.project.id,
            )}
            resourceKind="project"
            resourceId={record.project.id}
          />
        ),
    },
    {
      title: localeText(localeCode, '操作', 'Actions'),
      align: 'center',
      className: 'soha-table-actions-column',
      fixed: 'right',
      width: 200,
      render: (_value, record) => {
        if (record.kind === 'service' && record.service) {
          return (
            <Space className="soha-row-action-icons">
              {serviceActions.map(({ action, icon }) => (
                <ManagementIconButton
                  key={action}
                  aria-label={localeText(
                    localeCode,
                    `${operationActionLabel(action, localeCode)}服务`,
                    `${operationActionLabel(action, localeCode)} service`,
                  )}
                  size="small"
                  tooltip={operationActionLabel(action, localeCode)}
                  icon={icon}
                  loading={serviceActionMutation.isPending}
                  onClick={() => serviceActionMutation.mutate({ id: record.service!.id, action })}
                />
              ))}
            </Space>
          )
        }
        const project = record.project
        return (
          <Space className="soha-row-action-icons">
            {canDeployProjects ? (
              <ManagementIconButton
                aria-label={localeText(localeCode, '部署项目', 'Deploy project')}
                size="small"
                tooltip={localeText(localeCode, '部署', 'Deploy')}
                icon={<PlayCircleOutlined />}
                loading={deployPlanMutation.isPending || deployMutation.isPending}
                onClick={() => reviewDeploy(project, 'deploy')}
              />
            ) : null}
            {canDeployProjects ? (
              <ManagementIconButton
                aria-label={localeText(localeCode, '重启项目', 'Restart project')}
                size="small"
                tooltip={localeText(localeCode, '重启', 'Restart')}
                icon={<ReloadOutlined />}
                loading={deployPlanMutation.isPending || deployMutation.isPending}
                onClick={() => reviewDeploy(project, 'restart')}
              />
            ) : null}
            {canDeployProjects ? (
              <ManagementIconButton
                aria-label={localeText(localeCode, '停止项目', 'Stop project')}
                size="small"
                tooltip={localeText(localeCode, '停止', 'Stop')}
                icon={<PoweroffOutlined />}
                loading={deployPlanMutation.isPending || deployMutation.isPending}
                onClick={() => reviewDeploy(project, 'down')}
              />
            ) : null}
            {canDeployProjects && isSingleContainerProject(project) ? (
              <ManagementIconButton
                aria-label={localeText(localeCode, '销毁重建应用', 'Recreate application')}
                size="small"
                tooltip={localeText(localeCode, '销毁重建', 'Recreate')}
                danger
                icon={<SyncOutlined />}
                loading={deployPlanMutation.isPending || deployMutation.isPending}
                onClick={() => reviewDeploy(project, 'redeploy')}
              />
            ) : null}
            <Link to={`/compute/runtimes/projects/${project.id}`}>
              <ManagementIconButton
                aria-label={localeText(localeCode, '查看容器详情', 'View container details')}
                size="small"
                tooltip={localeText(localeCode, '详情', 'Details')}
                icon={<FileTextOutlined />}
              />
            </Link>
            {canUpdateProjects ? (
              <ManagementIconButton
                aria-label={localeText(localeCode, '编辑项目', 'Edit project')}
                size="small"
                tooltip={localeText(localeCode, '编辑', 'Edit')}
                icon={<EditOutlined />}
                onClick={() => {
                  if (saveMutation.isPending) return
                  if (!writeOutcomeUnknown(saveMutation.error)) saveMutation.reset()
                  setEditing(project)
                  setCurrentStep(0)
                  setDrawerOpen(true)
                }}
              />
            ) : null}
            {canDeleteProjects ? (
              <Popconfirm
                title={
                  isSingleContainerProject(project)
                    ? localeText(
                        localeCode,
                        '确认删除单容器服务？',
                        'Delete this single-container service?',
                      )
                    : localeText(
                        localeCode,
                        '确认删除 Compose 项目？',
                        'Delete this Compose project?',
                      )
                }
                onConfirm={() => deleteMutation.mutate(project.id)}
              >
                <ManagementIconButton
                  aria-label={localeText(localeCode, '删除项目', 'Delete project')}
                  size="small"
                  tooltip={localeText(localeCode, '删除', 'Delete')}
                  danger
                  icon={<DeleteOutlined />}
                />
              </Popconfirm>
            ) : null}
          </Space>
        )
      },
    },
  ]
  return (
    <>
      {!embedded ? (
        <div className="soha-vrt-query">
          <ManagementQueryPanel
            form={filterForm}
            actions={
              <ManagementQueryActions
                loading={projectsQuery.isFetching}
                onReset={() => {
                  filterForm.resetFields()
                  setFilters({ page: 1, pageSize: filters.pageSize ?? (embedded ? 5 : 15) })
                }}
              />
            }
            onFinish={(values) => setFilters((current) => ({ ...current, ...values, page: 1 }))}
          >
            <ManagementKeywordField
              placeholder={localeText(localeCode, '项目、Slug 或来源', 'Project, slug, or source')}
            />
            <ManagementQueryField
              minWidth={180}
              width={220}
              name="hostId"
              label={localeText(localeCode, '主机', 'Host')}
            >
              <Select
                allowClear
                showSearch={{ optionFilterProp: 'label' }}
                placeholder={localeText(localeCode, '全部主机', 'All hosts')}
                options={hostOptions}
              />
            </ManagementQueryField>
            <ManagementQueryField
              minWidth={132}
              width={150}
              name="status"
              label={localeText(localeCode, '状态', 'Status')}
            >
              <Select
                allowClear
                placeholder={localeText(localeCode, '全部', 'All')}
                options={['draft', 'defined', 'running', 'stopped', 'failed'].map((item) => ({
                  value: item,
                  label: formatStatusLabel(item, localeCode),
                }))}
              />
            </ManagementQueryField>
            <ManagementQueryField
              minWidth={132}
              width={150}
              name="sourceKind"
              label={localeText(localeCode, '类型', 'Type')}
            >
              <Select
                allowClear
                placeholder={localeText(localeCode, '全部', 'All')}
                options={[
                  { value: 'compose', label: 'Compose' },
                  {
                    value: 'single_container',
                    label: localeText(localeCode, '单容器', 'Single container'),
                  },
                ]}
              />
            </ManagementQueryField>
            <ManagementQueryField
              minWidth={150}
              width={180}
              name="environment"
              label={localeText(localeCode, '环境', 'Environment')}
            >
              <Input allowClear placeholder="dev / test" />
            </ManagementQueryField>
          </ManagementQueryPanel>
        </div>
      ) : null}
      {canViewServices
        ? projectServiceQueries.map((query, index) => (
            <ReadFeedback
              key={page.items[index].id}
              query={query}
              label={localeText(
                localeCode,
                `${page.items[index].name} 的服务`,
                `${page.items[index].name} services`,
              )}
            />
          ))
        : null}
      <ReadFeedback
        query={projectsQuery}
        label={localeText(localeCode, '容器项目', 'Container projects')}
      />
      <ReadFeedback
        query={tasksQuery}
        label={localeText(localeCode, '容器任务', 'Container tasks')}
      />
      <ComputeMutationFeedback
        operations={[
          {
            label: localeText(localeCode, '保存项目', 'Save project'),
            mutation: saveMutation,
            target: saveMutation.variables?.name,
            receipt: { message: localeText(localeCode, '项目记录已保存', 'Project record saved') },
          },
          {
            label: localeText(localeCode, '删除项目记录', 'Delete project record'),
            mutation: deleteMutation,
            target: deleteMutation.variables,
            receipt: {
              message: localeText(localeCode, '项目记录已删除', 'Project record deleted'),
            },
          },
          {
            label: localeText(localeCode, '容器启动', 'Container start'),
            mutation: containerStartMutation,
            receipt: {
              message: localeText(
                localeCode,
                '容器启动任务已提交，请核对执行结果',
                'Container start task submitted; check the result',
              ),
              taskId: containerStartMutation.data?.id,
              domain: 'container_runtime',
              canViewTask: canViewOperations,
            },
          },
          {
            readOnly: true,
            label: localeText(localeCode, '项目操作计划', 'Project operation plan'),
            mutation: deployPlanMutation,
            target: deployPlanMutation.variables?.id,
          },
          {
            label: localeText(localeCode, '项目操作', 'Project operation'),
            mutation: deployMutation,
            target: deployMutation.variables?.id,
            receipt: {
              message: localeText(
                localeCode,
                '项目操作任务已提交，请核对执行结果',
                'Project task submitted; check the result',
              ),
              taskId: deployMutation.data?.id,
              domain: 'container_runtime',
              canViewTask: canViewOperations,
            },
          },
          {
            label: localeText(localeCode, '服务操作', 'Service operation'),
            mutation: serviceActionMutation,
            target: serviceActionMutation.variables?.id,
            receipt: {
              message: localeText(
                localeCode,
                '服务操作任务已提交，请核对执行结果',
                'Service task submitted; check the result',
              ),
              taskId: serviceActionMutation.data?.id,
              domain: 'container_runtime',
              canViewTask: canViewOperations,
            },
          },
        ]}
      />
      <DockerAdminTable
        empty={
          projectsQuery.isError
            ? localeText(localeCode, '数据读取失败，请先重试', 'Read failed; retry first')
            : undefined
        }
        rowKey="key"
        expandable={{
          expandedRowKeys,
          onExpandedRowsChange: (keys: readonly Key[]) => setExpandedRowKeys(keys.map(String)),
          rowExpandable: (record: DockerProjectTreeRow) => (record.children?.length ?? 0) > 0,
        }}
        enableColumnSelection={!embedded}
        loading={projectsQuery.isLoading || projectServiceQueries.some((query) => query.isLoading)}
        dataSource={treeRows}
        columns={columns}
        pagination={pageTablePagination(page, embedded, setFilters)}
        actions={
          !embedded ? (
            <>
              {canStartContainer ? (
                <Button
                  type="primary"
                  icon={<PlayCircleOutlined />}
                  onClick={() => {
                    setContainerSourceKind('image')
                    setContainerReviewValues({})
                    setContainerStep(0)
                    setContainerDrawerOpen(true)
                  }}
                >
                  {localeText(localeCode, '快速启动', 'Quick start')}
                </Button>
              ) : null}
              {canCreateProjects ? (
                <Button
                  type="primary"
                  icon={<PlusOutlined />}
                  onClick={() => {
                    if (saveMutation.isPending) return
                    if (!writeOutcomeUnknown(saveMutation.error)) saveMutation.reset()
                    setEditing(null)
                    setCurrentStep(0)
                    setDrawerOpen(true)
                  }}
                >
                  {localeText(localeCode, '创建 Compose', 'Create Compose project')}
                </Button>
              ) : null}
            </>
          ) : null
        }
        enableDensity={!embedded}
        refreshing={projectsQuery.isFetching}
        showColumnSettings={!embedded}
        showRefresh={!embedded}
        onRefresh={() => {
          void projectsQuery.refetch()
          projectServiceQueries.forEach((query) => void query.refetch())
        }}
      />
      <OperationalPlanModal
        confirmText={
          deployPlan
            ? operationActionLabel(deployPlan.action, localeCode)
            : localeText(localeCode, '确认执行', 'Confirm')
        }
        loading={deployMutation.isPending}
        onCancel={() => setDeployPlan(null)}
        onConfirm={() => {
          if (!deployPlan) return
          deployMutation.mutate({
            action: deployPlan.action,
            id: deployPlan.project.id,
            idempotencyKey: deployPlan.idempotencyKey,
          })
        }}
        plan={deployPlan?.plan ?? null}
        title={
          deployPlan
            ? `${operationActionLabel(deployPlan.action, localeCode)}: ${deployPlan.project.name}`
            : ''
        }
      />
      <StepFormModal
        feedback={
          <ComputeOperationFeedback
            label={localeText(localeCode, '保存项目', 'Save project')}
            pending={saveMutation.isPending}
            error={saveMutation.error}
            target={saveMutation.variables?.name}
          />
        }
        title={
          editing
            ? localeText(localeCode, '编辑 Compose 项目', 'Edit Compose project')
            : localeText(localeCode, '创建 Compose 项目', 'Create Compose project')
        }
        current={currentStep}
        form={form}
        initialValues={
          editing
            ? projectFormValues(editing)
            : {
                composeContent: DEFAULT_COMPOSE,
                desiredState: 'running',
                status: 'draft',
                sourceKind: 'inline_compose',
                ttlSeconds: 600,
              }
        }
        loading={saveMutation.isPending}
        open={drawerOpen}
        onClose={() => {
          if (!saveMutation.isPending) setDrawerOpen(false)
        }}
        onCurrentChange={setCurrentStep}
        onFinish={(values) => saveMutation.mutate(values)}
        steps={[
          {
            title: localeText(localeCode, '基础信息', 'Basic information'),
            fieldNames: ['name', 'hostId', 'description'],
            children: (
              <>
                <Form.Item
                  name="name"
                  label={localeText(localeCode, '名称', 'Name')}
                  rules={[{ required: true }]}
                >
                  <Input />
                </Form.Item>
                <Form.Item
                  name="hostId"
                  label={localeText(localeCode, 'Docker 主机', 'Docker host')}
                  rules={[{ required: true }]}
                >
                  <Select showSearch={{ optionFilterProp: 'label' }} options={hostOptions} />
                </Form.Item>
                <Form.Item name="description" label={localeText(localeCode, '描述', 'Description')}>
                  <TextArea rows={3} maxLength={1000} showCount />
                </Form.Item>
              </>
            ),
          },
          {
            title: localeText(localeCode, '项目设置', 'Project settings'),
            fieldNames: ['environment', 'owner', 'team', 'ttlSeconds'],
            children: (
              <div className="grid gap-3 md:grid-cols-2">
                <Form.Item name="environment" label={localeText(localeCode, '环境', 'Environment')}>
                  <Input />
                </Form.Item>
                <Form.Item name="owner" label={localeText(localeCode, '负责人', 'Owner')}>
                  <Select
                    allowClear
                    disabled={!canViewUsers}
                    showSearch={{ optionFilterProp: 'label' }}
                    options={ownerOptions}
                  />
                </Form.Item>
                <Form.Item name="team" label={localeText(localeCode, '团队', 'Team')}>
                  <Select
                    allowClear
                    disabled={!canViewTeams}
                    showSearch={{ optionFilterProp: 'label' }}
                    options={teamOptions}
                  />
                </Form.Item>
                <Form.Item
                  name="ttlSeconds"
                  label={localeText(localeCode, 'TTL 秒数', 'TTL seconds')}
                  rules={[
                    {
                      required: true,
                      message: localeText(localeCode, '请输入 TTL', 'Enter a TTL'),
                    },
                  ]}
                >
                  <InputNumber id="ttlSeconds" min={60} precision={0} className="w-full" />
                </Form.Item>
              </div>
            ),
          },
          {
            title: localeText(localeCode, '部署来源', 'Deployment source'),
            fieldNames:
              sourceKind === 'template'
                ? ['sourceKind', 'templateId']
                : sourceKind === 'url'
                  ? ['sourceKind', 'sourceRef']
                  : sourceKind === 'git' && canViewSourceControl
                    ? [
                        'sourceKind',
                        'sourceConnectionId',
                        'sourceRepositoryId',
                        'sourceRevision',
                        'sourcePath',
                      ]
                    : ['sourceKind'],
            children: (
              <>
                <Form.Item
                  name="sourceKind"
                  label={localeText(localeCode, '来源类型', 'Source type')}
                  rules={[{ required: true }]}
                >
                  <Segmented
                    block
                    options={[
                      {
                        value: 'inline_compose',
                        label: localeText(localeCode, '在线编辑', 'Inline editor'),
                      },
                      { value: 'url', label: localeText(localeCode, '在线获取', 'Remote URL') },
                      {
                        value: 'git',
                        label: localeText(localeCode, 'Git 仓库', 'Git repository'),
                        disabled: !canViewSourceControl && editing?.sourceKind !== 'git',
                      },
                      {
                        value: 'template',
                        label: localeText(localeCode, '项目模板', 'Project template'),
                      },
                    ]}
                    onChange={(value) => {
                      if (value === 'inline_compose') {
                        form.setFieldsValue({
                          sourceRef: undefined,
                          sourceConnectionId: undefined,
                          sourceRepositoryId: undefined,
                          sourceRevision: undefined,
                          sourcePath: undefined,
                          templateId: undefined,
                        })
                      } else if (value === 'git') {
                        form.setFieldsValue({ templateId: undefined, sourcePath: 'compose.yaml' })
                      } else if (value === 'url') {
                        form.setFieldsValue({
                          sourceRef: undefined,
                          sourceConnectionId: undefined,
                          sourceRepositoryId: undefined,
                          sourceRevision: undefined,
                          sourcePath: undefined,
                          templateId: undefined,
                        })
                      } else {
                        form.setFieldsValue({
                          sourceConnectionId: undefined,
                          sourceRepositoryId: undefined,
                          sourceRevision: undefined,
                          sourcePath: undefined,
                        })
                      }
                    }}
                  />
                </Form.Item>
                {sourceKind === 'inline_compose' ? (
                  <Tabs
                    items={[
                      {
                        key: 'compose',
                        label: 'Compose',
                        children: (
                          <Form.Item name="composeContent" preserve rules={[{ required: true }]}>
                            <TextArea rows={14} spellCheck={false} />
                          </Form.Item>
                        ),
                      },
                      {
                        key: 'env',
                        label: '.env',
                        children: (
                          <Form.Item name="envContent" preserve>
                            <TextArea rows={10} spellCheck={false} />
                          </Form.Item>
                        ),
                      },
                    ]}
                  />
                ) : null}
                {sourceKind === 'url' ? (
                  <Form.Item
                    name="sourceRef"
                    label="Compose URL"
                    rules={[
                      { required: true, message: '请输入 Compose URL' },
                      { pattern: /^https:\/\//i, message: '仅支持 HTTPS URL' },
                    ]}
                  >
                    <Input placeholder="https://example.com/compose.yaml" />
                  </Form.Item>
                ) : null}
                {sourceKind === 'template' ? (
                  <Form.Item
                    name="templateId"
                    label={localeText(localeCode, '项目模板', 'Project template')}
                    rules={[{ required: true }]}
                  >
                    <Select
                      showSearch={{ optionFilterProp: 'label' }}
                      loading={templatesQuery.isLoading}
                      options={templateOptions}
                    />
                  </Form.Item>
                ) : null}
                {sourceKind === 'git' && canViewSourceControl ? (
                  <div className="grid gap-3 md:grid-cols-2">
                    <Form.Item
                      name="sourceConnectionId"
                      label={localeText(localeCode, '代码源', 'Source connection')}
                      rules={[{ required: true }]}
                    >
                      <Select
                        showSearch={{ optionFilterProp: 'label' }}
                        loading={sourceConnectionsQuery.isLoading}
                        options={sourceConnectionOptions}
                        onChange={() =>
                          form.setFieldsValue({
                            sourceRepositoryId: undefined,
                            sourceRevision: undefined,
                          })
                        }
                      />
                    </Form.Item>
                    <Form.Item
                      name="sourceRepositoryId"
                      label={localeText(localeCode, 'Git 仓库', 'Git repository')}
                      rules={[{ required: true }]}
                    >
                      <Select
                        showSearch={{ optionFilterProp: 'label' }}
                        loading={sourceRepositoriesQuery.isLoading}
                        options={sourceRepositoryOptions}
                        onChange={(repositoryId) => {
                          const repository = sourceRepositoriesQuery.data?.find(
                            (item) => item.id === repositoryId,
                          )
                          form.setFieldValue('sourceRevision', repository?.defaultBranch)
                        }}
                      />
                    </Form.Item>
                    <Form.Item
                      name="sourceRevision"
                      label={localeText(localeCode, '分支', 'Branch')}
                      rules={[{ required: true }]}
                    >
                      <Select
                        showSearch={{ optionFilterProp: 'label' }}
                        loading={sourceBranchesQuery.isLoading}
                        options={sourceRevisionOptions}
                      />
                    </Form.Item>
                    <Form.Item
                      name="sourcePath"
                      label={localeText(localeCode, 'Compose 文件', 'Compose file')}
                      rules={[{ required: true }]}
                    >
                      <Input placeholder="compose.yaml" />
                    </Form.Item>
                  </div>
                ) : null}
                {sourceKind === 'git' && !canViewSourceControl ? (
                  <Form.Item label={localeText(localeCode, 'Git 来源', 'Git source')}>
                    <Input value={editing?.sourceRef} disabled />
                  </Form.Item>
                ) : null}
              </>
            ),
          },
        ]}
        submitText={localeText(localeCode, '保存', 'Save')}
        width={780}
      />
      <StepFormModal
        feedback={
          <ComputeOperationFeedback
            label={localeText(localeCode, '启动容器', 'Start container')}
            pending={containerStartMutation.isPending}
            error={containerStartMutation.error}
            target={containerStartMutation.variables?.name}
          />
        }
        title={localeText(localeCode, '快速启动 Docker 应用', 'Quick start Docker application')}
        current={containerStep}
        form={containerForm}
        initialValues={{
          sourceKind: 'image',
          gitBuild: {
            ref: 'main',
            dockerfilePath: 'Dockerfile',
            contextDir: '.',
            pull: false,
            noCache: false,
          },
          architecture: 'amd64',
          protocol: 'tcp',
          exposureScope: 'internal',
          restartPolicy: 'unless-stopped',
          domainScheme: 'http',
          domainTlsEnabled: false,
          ports: DEFAULT_CONTAINER_PORTS,
        }}
        loading={containerStartMutation.isPending}
        open={containerDrawerOpen}
        onClose={() => {
          if (!containerStartMutation.isPending) setContainerDrawerOpen(false)
        }}
        onCurrentChange={(nextStep) => {
          if (nextStep === 3) {
            setContainerReviewValues(containerForm.getFieldsValue(true))
          }
          setContainerStep(nextStep)
        }}
        onFinish={(values) => containerStartMutation.mutate(values)}
        steps={[
          {
            title: localeText(localeCode, '来源', 'Source'),
            fieldNames: [
              'sourceKind',
              'name',
              'hostId',
              'image',
              ['gitBuild', 'repositoryUrl'],
              ['gitBuild', 'ref'],
              ['gitBuild', 'dockerfilePath'],
              ['gitBuild', 'contextDir'],
            ],
            children: (
              <div className="soha-docker-quick-form">
                <QuickStartFormSection title={localeText(localeCode, '来源方式', 'Source type')}>
                  <Form.Item
                    name="sourceKind"
                    label={localeText(localeCode, '应用来源', 'Application source')}
                    rules={[{ required: true }]}
                  >
                    <Segmented
                      block
                      onChange={(value) =>
                        setContainerSourceKind(value as 'image' | 'git_dockerfile')
                      }
                      options={[
                        {
                          value: 'image',
                          label: localeText(localeCode, '已有镜像', 'Existing image'),
                          icon: <CloudOutlined />,
                        },
                        {
                          value: 'git_dockerfile',
                          label: localeText(localeCode, 'Git 构建', 'Git build'),
                          icon: <BranchesOutlined />,
                        },
                      ]}
                    />
                  </Form.Item>
                </QuickStartFormSection>
                <QuickStartFormSection
                  title={localeText(localeCode, '应用与目标', 'Application and target')}
                >
                  <div className="grid gap-3 md:grid-cols-2">
                    <Form.Item
                      name="name"
                      label={localeText(localeCode, '应用名称', 'Application name')}
                      rules={[{ required: true }]}
                    >
                      <Input placeholder="preview-api" />
                    </Form.Item>
                    <Form.Item
                      name="hostId"
                      label={localeText(localeCode, 'Docker 主机', 'Docker host')}
                      rules={[{ required: true }]}
                    >
                      <Select
                        showSearch={{ optionFilterProp: 'label' }}
                        options={hostOptions}
                        onChange={applyContainerHostDefaults}
                      />
                    </Form.Item>
                    <Form.Item
                      name="architecture"
                      label={localeText(localeCode, '架构', 'Architecture')}
                    >
                      <Select options={ARCHITECTURE_OPTIONS} />
                    </Form.Item>
                  </div>
                </QuickStartFormSection>
                <QuickStartFormSection
                  title={localeText(localeCode, '镜像与构建', 'Image and build')}
                >
                  <Form.Item
                    name="image"
                    label={
                      containerSourceKind === 'git_dockerfile'
                        ? localeText(localeCode, '构建镜像', 'Build image')
                        : localeText(localeCode, '镜像', 'Image')
                    }
                    rules={[{ required: true }]}
                  >
                    <Input
                      placeholder={
                        containerSourceKind === 'git_dockerfile'
                          ? 'preview-api:git-main'
                          : 'nginx:alpine'
                      }
                    />
                  </Form.Item>
                  {containerSourceKind === 'git_dockerfile' ? (
                    <>
                      <Form.Item
                        name={['gitBuild', 'repositoryUrl']}
                        label={localeText(localeCode, 'Git 仓库', 'Git repository')}
                        rules={[
                          { required: true },
                          {
                            pattern: /^(https?|ssh):\/\/[^\s]+$/i,
                            message: localeText(
                              localeCode,
                              '请输入有效的仓库 URL',
                              'Enter a valid repository URL',
                            ),
                          },
                        ]}
                      >
                        <Input placeholder="https://github.com/org/repository.git" />
                      </Form.Item>
                      <div className="grid gap-3 md:grid-cols-3">
                        <Form.Item
                          name={['gitBuild', 'ref']}
                          label={localeText(
                            localeCode,
                            '分支 / Tag / Commit',
                            'Branch / Tag / Commit',
                          )}
                          rules={[{ required: true }]}
                        >
                          <Input placeholder="main" />
                        </Form.Item>
                        <Form.Item
                          name={['gitBuild', 'dockerfilePath']}
                          label="Dockerfile"
                          rules={[{ required: true }]}
                        >
                          <Input placeholder="Dockerfile" />
                        </Form.Item>
                        <Form.Item
                          name={['gitBuild', 'contextDir']}
                          label={localeText(localeCode, '构建目录', 'Build context')}
                          rules={[{ required: true }]}
                        >
                          <Input placeholder="." />
                        </Form.Item>
                      </div>
                      <div className="grid gap-3 md:grid-cols-2">
                        <Form.Item
                          name={['gitBuild', 'pull']}
                          label={localeText(
                            localeCode,
                            '拉取最新基础镜像',
                            'Pull latest base image',
                          )}
                          valuePropName="checked"
                        >
                          <Switch />
                        </Form.Item>
                        <Form.Item
                          name={['gitBuild', 'noCache']}
                          label={localeText(localeCode, '禁用构建缓存', 'Disable build cache')}
                          valuePropName="checked"
                        >
                          <Switch />
                        </Form.Item>
                      </div>
                    </>
                  ) : (
                    <Form.Item
                      name="imagePullPolicy"
                      label={localeText(localeCode, '拉取策略', 'Pull policy')}
                    >
                      <Select
                        allowClear
                        options={['always', 'missing', 'never'].map((item) => ({
                          value: item,
                          label: item,
                        }))}
                      />
                    </Form.Item>
                  )}
                </QuickStartFormSection>
              </div>
            ),
          },
          {
            title: localeText(localeCode, '运行配置', 'Runtime configuration'),
            fieldNames: ['restartPolicy', 'environmentVariables', 'resources'],
            children: (
              <>
                <div className="grid gap-3 md:grid-cols-2">
                  <Form.Item
                    name="restartPolicy"
                    label={localeText(localeCode, '重启策略', 'Restart policy')}
                  >
                    <Select
                      options={['unless-stopped', 'always', 'on-failure', 'no'].map((item) => ({
                        value: item,
                        label: item,
                      }))}
                    />
                  </Form.Item>
                  <Form.Item
                    name="environment"
                    label={localeText(localeCode, '环境', 'Environment')}
                  >
                    <Input />
                  </Form.Item>
                  <Form.Item name="owner" label={localeText(localeCode, '负责人', 'Owner')}>
                    <Input />
                  </Form.Item>
                  <Form.Item name="team" label={localeText(localeCode, '团队', 'Team')}>
                    <Input />
                  </Form.Item>
                  <Form.Item
                    name="ttlSeconds"
                    label={localeText(localeCode, 'TTL 秒数', 'TTL seconds')}
                  >
                    <InputNumber min={0} className="w-full" />
                  </Form.Item>
                  <Form.Item name="command" label={localeText(localeCode, '启动命令', 'Command')}>
                    <Input />
                  </Form.Item>
                  <Form.Item name="entrypoint" label="Entrypoint">
                    <Input />
                  </Form.Item>
                </div>
                <Form.List name="environmentVariables">
                  {(fields, { add, remove }) => (
                    <div className="mb-3 space-y-3">
                      {fields.map((field, index) => (
                        <div
                          key={field.key}
                          className="grid items-start gap-3 md:grid-cols-[1fr_1fr_40px]"
                        >
                          <Form.Item
                            name={[field.name, 'name']}
                            label={
                              index === 0 ? localeText(localeCode, '变量名', 'Variable') : undefined
                            }
                          >
                            <Input placeholder="APP_ENV" />
                          </Form.Item>
                          <Form.Item
                            name={[field.name, 'value']}
                            label={
                              index === 0 ? localeText(localeCode, '变量值', 'Value') : undefined
                            }
                          >
                            <Input />
                          </Form.Item>
                          <Button
                            className={index === 0 ? 'mt-8' : undefined}
                            type="text"
                            danger
                            icon={<MinusCircleOutlined />}
                            onClick={() => remove(field.name)}
                          />
                        </div>
                      ))}
                      <Button type="dashed" icon={<PlusOutlined />} onClick={() => add({})}>
                        {localeText(localeCode, '添加环境变量', 'Add environment variable')}
                      </Button>
                    </div>
                  )}
                </Form.List>
                <div className="grid gap-3 md:grid-cols-3">
                  <Form.Item
                    name={['resources', 'cpus']}
                    label={localeText(localeCode, 'CPU 限制', 'CPU limit')}
                  >
                    <InputNumber min={0} step={0.1} className="w-full" />
                  </Form.Item>
                  <Form.Item
                    name={['resources', 'memoryMiB']}
                    label={localeText(localeCode, '内存限制 MiB', 'Memory limit MiB')}
                  >
                    <InputNumber min={0} className="w-full" />
                  </Form.Item>
                  <Form.Item
                    name={['resources', 'memoryReservationMiB']}
                    label={localeText(localeCode, '内存预留 MiB', 'Memory reservation MiB')}
                  >
                    <InputNumber min={0} className="w-full" />
                  </Form.Item>
                </div>
                <Form.Item name="envContent" label=".env">
                  <TextArea rows={7} spellCheck={false} placeholder="KEY=value" />
                </Form.Item>
              </>
            ),
          },
          {
            title: localeText(localeCode, '网络与存储', 'Network and storage'),
            fieldNames: ['ports', 'volumes', 'network'],
            children: (
              <>
                <Form.List name="ports">
                  {(fields, { add, remove }) => (
                    <div className="mb-3 space-y-3">
                      {fields.map((field, index) => (
                        <div
                          key={field.key}
                          className="rounded border border-[var(--soha-border-color)] p-3"
                        >
                          <div className="mb-2 flex items-center justify-between">
                            <Text strong>
                              {localeText(localeCode, `端口 ${index + 1}`, `Port ${index + 1}`)}
                            </Text>
                            {fields.length > 1 ? (
                              <Button
                                type="text"
                                danger
                                icon={<MinusCircleOutlined />}
                                onClick={() => remove(field.name)}
                              />
                            ) : null}
                          </div>
                          <div className="grid gap-3 md:grid-cols-4">
                            <Form.Item
                              name={[field.name, 'name']}
                              label={localeText(localeCode, '名称', 'Name')}
                            >
                              <Input placeholder="http" />
                            </Form.Item>
                            <Form.Item
                              name={[field.name, 'containerPort']}
                              label={localeText(localeCode, '容器端口', 'Container port')}
                              rules={[{ required: true }]}
                            >
                              <InputNumber min={1} max={65535} className="w-full" />
                            </Form.Item>
                            <Form.Item
                              name={[field.name, 'hostPort']}
                              label={localeText(localeCode, '主机端口', 'Host port')}
                              rules={[{ required: true }]}
                            >
                              <InputNumber min={1} max={65535} className="w-full" />
                            </Form.Item>
                            <Form.Item
                              name={[field.name, 'hostIp']}
                              label={localeText(localeCode, '监听 IP', 'Listen IP')}
                            >
                              <Input placeholder="0.0.0.0" />
                            </Form.Item>
                            <Form.Item
                              name={[field.name, 'protocol']}
                              label={localeText(localeCode, '协议', 'Protocol')}
                            >
                              <Select
                                options={[
                                  { value: 'tcp', label: 'tcp' },
                                  { value: 'udp', label: 'udp' },
                                ]}
                              />
                            </Form.Item>
                            <Form.Item
                              name={[field.name, 'exposureScope']}
                              label={localeText(localeCode, '暴露范围', 'Exposure')}
                            >
                              <Select
                                options={['internal', 'vpn', 'public'].map((item) => ({
                                  value: item,
                                  label: item,
                                }))}
                              />
                            </Form.Item>
                            <Form.Item
                              name={[field.name, 'domainName']}
                              label={localeText(localeCode, '访问域名', 'Domain name')}
                            >
                              <Input placeholder="preview.internal.example.com" />
                            </Form.Item>
                            <Form.Item
                              name={[field.name, 'domainScheme']}
                              label={localeText(localeCode, '域名协议', 'Scheme')}
                            >
                              <Select
                                options={[
                                  { value: 'http', label: 'http' },
                                  { value: 'https', label: 'https' },
                                ]}
                              />
                            </Form.Item>
                            <Form.Item
                              name={[field.name, 'domainTlsEnabled']}
                              label="TLS"
                              valuePropName="checked"
                            >
                              <Switch />
                            </Form.Item>
                          </div>
                        </div>
                      ))}
                      <Button
                        type="dashed"
                        icon={<PlusOutlined />}
                        onClick={() =>
                          add({
                            hostIp: '0.0.0.0',
                            protocol: 'tcp',
                            exposureScope: 'internal',
                            domainScheme: 'http',
                            domainTlsEnabled: false,
                          })
                        }
                      >
                        {localeText(localeCode, '添加端口', 'Add port')}
                      </Button>
                    </div>
                  )}
                </Form.List>
                <Form.List name="volumes">
                  {(fields, { add, remove }) => (
                    <div className="mb-3 space-y-3">
                      {fields.map((field, index) => (
                        <div
                          key={field.key}
                          className="rounded border border-[var(--soha-border-color)] p-3"
                        >
                          <div className="mb-2 flex items-center justify-between">
                            <Text strong>
                              {localeText(localeCode, `卷 ${index + 1}`, `Volume ${index + 1}`)}
                            </Text>
                            <Button
                              type="text"
                              danger
                              icon={<MinusCircleOutlined />}
                              onClick={() => remove(field.name)}
                            />
                          </div>
                          <div className="grid gap-3 md:grid-cols-4">
                            <Form.Item
                              name={[field.name, 'name']}
                              label={localeText(localeCode, '名称', 'Name')}
                            >
                              <Input placeholder="data" />
                            </Form.Item>
                            <Form.Item
                              name={[field.name, 'type']}
                              label={localeText(localeCode, '类型', 'Type')}
                            >
                              <Select
                                options={[
                                  { value: 'bind', label: 'bind' },
                                  { value: 'volume', label: 'volume' },
                                ]}
                              />
                            </Form.Item>
                            <Form.Item
                              name={[field.name, 'source']}
                              label={localeText(localeCode, '来源', 'Source')}
                            >
                              <Input
                                placeholder={localeText(
                                  localeCode,
                                  '/data/app 或 app-data',
                                  '/data/app or app-data',
                                )}
                              />
                            </Form.Item>
                            <Form.Item
                              name={[field.name, 'target']}
                              label={localeText(localeCode, '挂载路径', 'Mount path')}
                            >
                              <Input placeholder="/var/lib/app" />
                            </Form.Item>
                            <Form.Item
                              name={[field.name, 'readOnly']}
                              label={localeText(localeCode, '只读', 'Read-only')}
                              valuePropName="checked"
                            >
                              <Switch />
                            </Form.Item>
                          </div>
                        </div>
                      ))}
                      <Button
                        type="dashed"
                        icon={<PlusOutlined />}
                        onClick={() => add({ type: 'bind', readOnly: false })}
                      >
                        {localeText(localeCode, '添加卷', 'Add volume')}
                      </Button>
                    </div>
                  )}
                </Form.List>
                <Form.Item
                  name="network"
                  label={localeText(localeCode, '外部网络', 'External network')}
                >
                  <Input
                    placeholder={localeText(
                      localeCode,
                      'traefik 或已有 Docker network',
                      'traefik or an existing Docker network',
                    )}
                  />
                </Form.Item>
              </>
            ),
          },
          {
            title: localeText(localeCode, '确认启动', 'Review'),
            children: (
              <Descriptions
                bordered
                column={2}
                size="small"
                items={[
                  {
                    key: 'source',
                    label: localeText(localeCode, '来源', 'Source'),
                    children:
                      containerSourceKind === 'git_dockerfile'
                        ? localeText(localeCode, 'Git 构建', 'Git build')
                        : localeText(localeCode, '已有镜像', 'Existing image'),
                  },
                  {
                    key: 'name',
                    label: localeText(localeCode, '应用', 'Application'),
                    children: containerReviewValues.name || '-',
                  },
                  {
                    key: 'host',
                    label: localeText(localeCode, 'Docker 主机', 'Docker host'),
                    children:
                      hostOptions.find((item) => item.value === containerReviewValues.hostId)
                        ?.label ||
                      containerReviewValues.hostId ||
                      '-',
                  },
                  {
                    key: 'image',
                    label:
                      containerSourceKind === 'git_dockerfile'
                        ? localeText(localeCode, '构建镜像', 'Build image')
                        : localeText(localeCode, '镜像', 'Image'),
                    children: containerReviewValues.image || '-',
                  },
                  ...(containerSourceKind === 'git_dockerfile'
                    ? [
                        {
                          key: 'repository',
                          label: localeText(localeCode, 'Git 仓库', 'Git repository'),
                          children: containerReviewValues.gitBuild?.repositoryUrl || '-',
                        },
                        {
                          key: 'ref',
                          label: localeText(localeCode, '版本', 'Revision'),
                          children: containerReviewValues.gitBuild?.ref || 'main',
                        },
                      ]
                    : []),
                  {
                    key: 'ports',
                    label: localeText(localeCode, '端口', 'Ports'),
                    children: localeText(
                      localeCode,
                      `${containerReviewValues.ports?.length || 0} 项`,
                      `${containerReviewValues.ports?.length || 0}`,
                    ),
                  },
                  {
                    key: 'volumes',
                    label: localeText(localeCode, '卷', 'Volumes'),
                    children: localeText(
                      localeCode,
                      `${containerReviewValues.volumes?.length || 0} 项`,
                      `${containerReviewValues.volumes?.length || 0}`,
                    ),
                  },
                ]}
              />
            ),
          },
        ]}
        submitText={localeText(localeCode, '启动', 'Start')}
        width={820}
      />
    </>
  )
}

export function DockerProjectsPage() {
  return (
    <div className="soha-page soha-docker-page">
      <ProjectsTable />
    </div>
  )
}
