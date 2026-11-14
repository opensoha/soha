import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import './styles.css'
import {
  App,
  Button,
  Collapse,
  Form,
  Input,
  InputNumber,
  Modal,
  Select,
  Space,
  Switch,
  Tabs,
  Tag,
  Typography,
} from 'antd'
import {
  DeleteOutlined,
  EditOutlined,
  EyeOutlined,
  PlusOutlined,
  SaveOutlined,
} from '@ant-design/icons'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useNavigate, useSearchParams } from 'react-router-dom'
import {
  ManagementIconButton,
  ManagementRefreshButton,
  ManagementSearchableListPane,
  ManagementState,
  TemplateDesignerShell,
} from '@/components/management-list'
import { hasPermission, usePermissionSnapshot } from '@/features/auth'
import { StatusTag } from '@/components/status-tag'
import {
  TemplateUsageImpactPanel,
  shouldConfirmTemplateUsageSave,
  templateUsageConfirmText,
} from '../template-usage-impact'
import { deliveryMutations } from '../mutations'
import { deliveryQueries } from '../queries'
import type { DeliveryBlueprint, RenderedDeliverySpec } from '../types'
import { formatDateTime } from '@/utils/time'
import { parseReleaseTargets } from '../release-targets'
import { ExternalPipelineFields } from '../external-pipeline-fields'

const { Text } = Typography

const BUILD_SOURCE_TYPE_OPTIONS = [
  { value: 'repo_dockerfile', label: '仓库 Dockerfile' },
  { value: 'platform_build_template', label: '平台构建模板' },
  { value: 'external_pipeline', label: 'GitLab CI' },
]

const REPOSITORY_PROVIDER_OPTIONS = [
  { value: 'git', label: '标准 Git' },
  { value: 'gitlab', label: 'GitLab' },
]

const LANGUAGE_OPTIONS = [
  { value: 'go', label: 'Go' },
  { value: 'node', label: 'Node.js' },
  { value: 'java', label: 'Java' },
  { value: 'python', label: 'Python' },
  { value: 'other', label: 'Other' },
]

const SERVICE_KIND_OPTIONS = [
  { value: 'kubernetes_workload', label: 'Kubernetes 工作负载' },
  { value: 'helm_release', label: 'Helm Release' },
  { value: 'job', label: 'Job' },
  { value: 'external_service', label: '外部服务' },
]

const REF_TYPE_OPTIONS = [
  { value: 'branch', label: 'branch' },
  { value: 'tag', label: 'tag' },
  { value: 'commit', label: 'commit' },
]

const IMAGE_TAG_MODE_OPTIONS = [
  { value: 'input', label: '手工输入' },
  { value: 'template', label: '模板生成' },
  { value: 'build_output', label: '构建输出' },
]

const ACTION_KIND_OPTIONS = [
  { value: 'deploy', label: 'deploy' },
  { value: 'release', label: 'release' },
]

const VERIFICATION_MODE_OPTIONS = [
  { value: 'workflow', label: '发布流程验证' },
  { value: 'none', label: '不运行验证' },
]

const FILE_KIND_OPTIONS = [
  { value: 'dockerfile', label: 'Dockerfile' },
  { value: 'yaml_manifest', label: 'Kubernetes YAML' },
  { value: 'helm_values', label: 'Helm Values' },
  { value: 'helm_chart', label: 'Helm Chart' },
  { value: 'kustomization', label: 'Kustomize' },
  { value: 'deployment', label: 'Deployment（兼容）' },
  { value: 'service', label: 'Service（兼容）' },
  { value: 'readme', label: 'README' },
  { value: 'other', label: 'Other' },
]

type FileTemplatePreset =
  'dockerfile' | 'yaml_manifest' | 'helm_values' | 'helm_chart' | 'kustomization'

interface FileTemplateDraft {
  path: string
  kind: string
  purpose: string
  required: boolean
  content: string
}

const FILE_TEMPLATE_PRESET_OPTIONS: Array<{ value: FileTemplatePreset; label: string }> = [
  { value: 'dockerfile', label: 'Dockerfile 模板' },
  { value: 'yaml_manifest', label: 'YAML 模板' },
  { value: 'helm_values', label: 'Helm Values 模板' },
  { value: 'helm_chart', label: 'Helm Chart 模板' },
  { value: 'kustomization', label: 'Kustomize 模板' },
]

const DEFAULT_ENVIRONMENT_OPTIONS = ['dev', 'test', 'staging', 'prod'].map((value) => ({
  value,
  label: value,
}))

type BlueprintFormValues = Record<string, unknown>

interface BlueprintListItem {
  applicationName: string
  bindingCount: number
  blueprint?: DeliveryBlueprint
  buildSourceCount: number
  description?: string
  enabled: boolean
  fileCount: number
  id: string
  isDraft?: boolean
  key: string
  name: string
  updatedAt?: string
}

function stringify(value: unknown, fallback: string) {
  if (value == null) return fallback
  return JSON.stringify(value, null, 2)
}

function trimString(value: unknown) {
  return String(value ?? '').trim()
}

function optionalString(value: unknown) {
  const text = trimString(value)
  return text || undefined
}

function compactRecord(record: Record<string, unknown>) {
  return Object.fromEntries(
    Object.entries(record).filter(([, value]) => value !== undefined && value !== ''),
  )
}

function normalizeTags(value: unknown) {
  if (Array.isArray(value)) {
    return value.map((item) => trimString(item)).filter(Boolean)
  }
  return trimString(value)
    .split(/[\n,]/)
    .map((item) => item.trim())
    .filter(Boolean)
}

function slugFrom(value: unknown, fallback: string) {
  const slug = trimString(value)
    .toLowerCase()
    .replace(/[^a-z0-9_-]+/g, '-')
    .replace(/^-+|-+$/g, '')
  return slug || fallback
}

function normalizeObjectArray(value: unknown) {
  return Array.isArray(value)
    ? value.filter(
        (item): item is Record<string, unknown> =>
          Boolean(item) && typeof item === 'object' && !Array.isArray(item),
      )
    : []
}

function parseJSONObject(raw: unknown, field: string) {
  const text = trimString(raw)
  if (!text) return {}
  const value = JSON.parse(text)
  if (!value || typeof value !== 'object' || Array.isArray(value)) {
    throw new Error(`${field} 需要是 JSON 对象`)
  }
  return value as Record<string, unknown>
}

function parseJSONArray(raw: unknown, field: string) {
  const value = JSON.parse(trimString(raw) || '[]')
  const objects = normalizeObjectArray(value)
  if (!Array.isArray(value) || value.length !== objects.length)
    throw new Error(`${field} 需要是 JSON 对象数组`)
  return objects
}

function createDefaultBuildSource(index = 0) {
  return {
    id: `source-${index + 1}`,
    name: index === 0 ? '仓库 Dockerfile' : `构建源 ${index + 1}`,
    type: 'repo_dockerfile',
    enabled: true,
    isDefault: index === 0,
    buildImage: '',
    defaultTag: '',
    buildTemplateId: '',
    contextDir: '.',
    dockerfilePath: 'Dockerfile',
    externalPipeline: { provider: 'gitlab' },
    configText: '{\n  "contextDir": ".",\n  "dockerfilePath": "Dockerfile"\n}',
  }
}

function createDefaultService(index = 0) {
  const key = index === 0 ? 'api' : `service-${index + 1}`
  return {
    key,
    name: index === 0 ? 'API' : `服务 ${index + 1}`,
    serviceKind: 'kubernetes_workload',
    buildSourceId: 'source-1',
    enabled: true,
    dockerfilePath: 'Dockerfile',
    buildContextDir: '.',
  }
}

function createDefaultEnvironmentBinding(index = 0) {
  return {
    environmentKey: index === 0 ? 'dev' : '',
    workflowTemplateId: '',
    buildSourceId: 'source-1',
    refType: 'branch',
    refValue: 'main',
    imageTagMode: 'input',
    imageTagTemplate: '',
    buildVariablesText: '{}',
    buildArgsText: '{}',
    actionKind: 'deploy',
    requiresApproval: false,
    approverRoles: [],
    autoRollback: true,
    rolloutTimeoutSeconds: 300,
    verificationMode: 'workflow',
    resourceSelectorText: '{}',
    targetsText: '[]',
  }
}

function createDefaultFileTemplate(
  index = 0,
  preset: FileTemplatePreset = 'dockerfile',
): FileTemplateDraft {
  const templates: Record<FileTemplatePreset, FileTemplateDraft> = {
    dockerfile: {
      path: 'Dockerfile',
      kind: 'dockerfile',
      purpose: '平台规范 Dockerfile 草稿',
      required: index === 0,
      content:
        'FROM node:22-alpine\nWORKDIR /app\nCOPY . .\nRUN npm ci && npm run build\nCMD ["npm", "start"]\n',
    },
    yaml_manifest: {
      path: 'deploy/deployment.yaml',
      kind: 'yaml_manifest',
      purpose: 'Kubernetes 工作负载清单',
      required: false,
      content:
        'apiVersion: apps/v1\nkind: Deployment\nmetadata:\n  name: {{appKey}}\nspec:\n  replicas: 1\n',
    },
    helm_values: {
      path: 'deploy/values.yaml',
      kind: 'helm_values',
      purpose: 'Helm 环境 Values',
      required: false,
      content: 'replicaCount: 1\nimage:\n  repository: {{imageRepository}}\n  tag: {{imageTag}}\n',
    },
    helm_chart: {
      path: 'deploy/Chart.yaml',
      kind: 'helm_chart',
      purpose: 'Helm Chart 元数据',
      required: false,
      content: 'apiVersion: v2\nname: {{appKey}}\nversion: 0.1.0\nappVersion: {{imageTag}}\n',
    },
    kustomization: {
      path: 'deploy/overlays/dev/kustomization.yaml',
      kind: 'kustomization',
      purpose: 'Kustomize 环境 Overlay',
      required: false,
      content:
        'apiVersion: kustomize.config.k8s.io/v1beta1\nkind: Kustomization\nresources:\n  - ../../base\n',
    },
  }
  return templates[preset]
}

function createBlueprintDraftValues(
  key = `app-template-${Date.now().toString(36)}`,
): BlueprintFormValues {
  return {
    key,
    name: '新建接入模板',
    description: '',
    enabled: true,
    appName: 'sample-app',
    appKey: 'sample-app',
    appGroup: 'default',
    businessLineId: '',
    language: 'go',
    appDescription: '',
    ownerTeam: '',
    repositoryProvider: 'git',
    repositoryProjectId: '',
    repositoryPath: '',
    defaultBranch: 'main',
    defaultTag: '',
    buildImage: '',
    buildContextDir: '.',
    dockerfilePath: 'Dockerfile',
    appEnabled: true,
    metadataText: '{}',
    services: [createDefaultService()],
    buildSources: [createDefaultBuildSource()],
    environmentBindings: [createDefaultEnvironmentBinding()],
    files: [createDefaultFileTemplate()],
    executionHintsText: '{}',
    postCreateActions: ['render_spec', 'create_manifest_package'],
  }
}

function blueprintToFormValues(blueprint: DeliveryBlueprint): BlueprintFormValues {
  const draft = blueprint.applicationDraft ?? {}
  return {
    key: blueprint.key,
    name: blueprint.name,
    description: blueprint.description ?? '',
    enabled: blueprint.enabled,
    appName: draft.name ?? '',
    appKey: draft.key ?? '',
    appGroup: draft.group ?? 'default',
    businessLineId: draft.businessLineId ?? '',
    language: draft.language ?? 'go',
    appDescription: draft.description ?? '',
    ownerTeam: draft.ownerTeam ?? '',
    repositoryProvider: draft.repositoryProvider ?? 'git',
    repositoryProjectId: draft.repositoryProjectId ?? '',
    repositoryPath: draft.repositoryPath ?? '',
    defaultBranch: draft.defaultBranch ?? 'main',
    defaultTag: draft.defaultTag ?? '',
    buildImage: draft.buildImage ?? '',
    buildContextDir: draft.buildContextDir ?? '.',
    dockerfilePath: draft.dockerfilePath ?? 'Dockerfile',
    appEnabled: draft.enabled !== false,
    metadataText: stringify(draft.metadata ?? {}, '{}'),
    services: (blueprint.services ?? []).map((service) => ({
      key: service.key,
      name: service.name,
      serviceKind: service.serviceKind,
      ownerTeam: service.ownerTeam ?? '',
      repositoryPath: service.repositoryPath ?? '',
      defaultBranch: service.defaultBranch ?? '',
      buildSourceId: service.buildSourceId ?? '',
      deploymentTemplateText: service.deploymentTemplate
        ? stringify(service.deploymentTemplate, '{}')
        : '',
      containersText: stringify(service.containers ?? [], '[]'),
      enabled: service.enabled,
      dockerfilePath: service.containers?.[0]?.dockerfilePath ?? 'Dockerfile',
      buildContextDir: service.containers?.[0]?.buildContextDir ?? '.',
    })),
    buildSources: (blueprint.buildSources ?? []).map((source) => {
      const config = source.config ?? {}
      return {
        id: source.id,
        name: source.name,
        type: source.type,
        enabled: source.enabled,
        isDefault: source.isDefault,
        buildImage: source.buildImage ?? '',
        defaultTag: source.defaultTag ?? '',
        buildTemplateId: String(config.buildTemplateId ?? ''),
        contextDir: String(config.contextDir ?? '.'),
        dockerfilePath: String(config.dockerfilePath ?? 'Dockerfile'),
        externalPipeline: config.externalPipeline ?? { provider: 'gitlab' },
        configText: stringify(config, '{}'),
      }
    }),
    environmentBindings: (blueprint.environmentBindings ?? []).map((binding) => ({
      environmentId: binding.environmentId ?? '',
      environmentKey: binding.environmentKey ?? '',
      businessLineId: binding.businessLineId ?? '',
      strategyProfileId: binding.strategyProfileId ?? '',
      promotionPolicyId: binding.promotionPolicyId ?? '',
      artifactPolicyId: binding.artifactPolicyId ?? '',
      workflowTemplateId: binding.workflowTemplateId ?? '',
      buildSourceId: binding.buildPolicy?.sourceId ?? '',
      refType: binding.buildPolicy?.refType ?? 'branch',
      refValue: binding.buildPolicy?.refValue ?? 'main',
      imageTagMode: binding.buildPolicy?.imageTagMode ?? 'input',
      imageTagTemplate: binding.buildPolicy?.imageTagTemplate ?? '',
      buildVariablesText: stringify(binding.buildPolicy?.variables ?? {}, '{}'),
      buildArgsText: stringify(binding.buildPolicy?.buildArgs ?? {}, '{}'),
      actionKind: binding.releasePolicy?.actionKind ?? 'deploy',
      requiresApproval: binding.releasePolicy?.requiresApproval ?? false,
      approverRoles: binding.releasePolicy?.approverRoles ?? [],
      autoRollback: binding.releasePolicy?.autoRollback ?? true,
      rolloutTimeoutSeconds: binding.releasePolicy?.rolloutTimeoutSeconds ?? 300,
      verificationMode: binding.releasePolicy?.verificationMode ?? 'workflow',
      resourceSelectorText: stringify(binding.resourceSelector ?? {}, '{}'),
      targetsText: stringify(binding.targets ?? [], '[]'),
    })),
    files: (blueprint.files ?? []).map((file) => ({
      path: file.path,
      kind: file.kind,
      purpose: file.purpose ?? '',
      required: file.required,
      content: file.content ?? '',
    })),
    executionHintsText: stringify(blueprint.executionHints ?? {}, '{}'),
    postCreateActions: blueprint.postCreateActions ?? [],
  }
}

function buildBlueprintPayload(values: BlueprintFormValues, id?: string) {
  const services = normalizeObjectArray(values.services).map((service, index) => {
    const key = trimString(service.key) || `service-${index + 1}`
    return compactRecord({
      key,
      name: trimString(service.name) || key,
      serviceKind: trimString(service.serviceKind) || 'kubernetes_workload',
      ownerTeam: optionalString(service.ownerTeam),
      repositoryPath: optionalString(service.repositoryPath),
      defaultBranch: optionalString(service.defaultBranch),
      buildSourceId: optionalString(service.buildSourceId),
      deploymentTemplate: trimString(service.deploymentTemplateText)
        ? parseJSONObject(service.deploymentTemplateText, `服务 ${index + 1} 部署模板`)
        : undefined,
      enabled: service.enabled !== false,
      metadata: {},
      containers:
        trimString(service.containersText) && trimString(service.containersText) !== '[]'
          ? parseJSONArray(service.containersText, `服务 ${index + 1} 容器`).map(
              (container, containerIndex) => ({
                ...container,
                ...(containerIndex === 0
                  ? {
                      dockerfilePath: trimString(service.dockerfilePath) || 'Dockerfile',
                      buildContextDir: trimString(service.buildContextDir) || '.',
                    }
                  : {}),
              }),
            )
          : [
              {
                name: key,
                dockerfilePath: trimString(service.dockerfilePath) || 'Dockerfile',
                buildContextDir: trimString(service.buildContextDir) || '.',
                metadata: {},
              },
            ],
    })
  })
  const buildSources = normalizeObjectArray(values.buildSources).map((source, index) =>
    compactRecord({
      id: optionalString(source.id) ?? slugFrom(source.name, `source-${index + 1}`),
      name: trimString(source.name) || `构建源 ${index + 1}`,
      type: trimString(source.type) || 'repo_dockerfile',
      enabled: source.enabled !== false,
      isDefault: Boolean(source.isDefault),
      buildImage: optionalString(source.buildImage),
      defaultTag: optionalString(source.defaultTag),
      config: compactRecord({
        ...parseJSONObject(source.configText, `构建源 ${index + 1} 配置`),
        buildTemplateId:
          source.type === 'platform_build_template'
            ? optionalString(source.buildTemplateId)
            : undefined,
        contextDir:
          source.type === 'external_pipeline' ? undefined : optionalString(source.contextDir),
        dockerfilePath:
          source.type === 'repo_dockerfile' ? optionalString(source.dockerfilePath) : undefined,
        externalPipeline: source.type === 'external_pipeline' ? source.externalPipeline : undefined,
      }),
    }),
  )

  const environmentBindings = normalizeObjectArray(values.environmentBindings).map(
    (binding, index) =>
      compactRecord({
        environmentId: optionalString(binding.environmentId),
        environmentKey: optionalString(binding.environmentKey),
        businessLineId: optionalString(binding.businessLineId),
        strategyProfileId: optionalString(binding.strategyProfileId),
        promotionPolicyId: optionalString(binding.promotionPolicyId),
        artifactPolicyId: optionalString(binding.artifactPolicyId),
        workflowTemplateId: optionalString(binding.workflowTemplateId),
        buildPolicy: compactRecord({
          sourceId: optionalString(binding.buildSourceId),
          refType: trimString(binding.refType) || 'branch',
          refValue: optionalString(binding.refValue),
          imageTagMode: trimString(binding.imageTagMode) || 'input',
          imageTagTemplate: optionalString(binding.imageTagTemplate),
          variables: parseJSONObject(binding.buildVariablesText, `环境绑定 ${index + 1} 构建变量`),
          buildArgs: parseJSONObject(binding.buildArgsText, `环境绑定 ${index + 1} 构建参数`),
        }),
        releasePolicy: compactRecord({
          actionKind: trimString(binding.actionKind) || 'deploy',
          requiresApproval: Boolean(binding.requiresApproval),
          approverRoles: normalizeTags(binding.approverRoles),
          autoRollback: Boolean(binding.autoRollback),
          rolloutTimeoutSeconds: Number(binding.rolloutTimeoutSeconds || 300),
          verificationMode: trimString(binding.verificationMode) || 'workflow',
        }),
        resourceSelector: parseJSONObject(
          binding.resourceSelectorText,
          `环境绑定 ${index + 1} 资源选择器`,
        ),
        targets: parseReleaseTargets(binding.targetsText, `环境绑定 ${index + 1} 发布目标`),
      }),
  )

  const files = normalizeObjectArray(values.files).map((file, index) =>
    compactRecord({
      path: trimString(file.path) || `template-${index + 1}.yaml`,
      kind: trimString(file.kind) || 'other',
      content: String(file.content ?? ''),
      required: file.required !== false,
      purpose: optionalString(file.purpose),
    }),
  )

  return compactRecord({
    id,
    key: trimString(values.key),
    name: trimString(values.name),
    description: optionalString(values.description),
    applicationDraft: compactRecord({
      name: trimString(values.appName),
      key: trimString(values.appKey),
      group: trimString(values.appGroup) || 'default',
      businessLineId: optionalString(values.businessLineId),
      language: trimString(values.language) || 'go',
      description: optionalString(values.appDescription),
      ownerTeam: optionalString(values.ownerTeam),
      repositoryProvider: optionalString(values.repositoryProvider),
      repositoryProjectId: optionalString(values.repositoryProjectId),
      repositoryPath: optionalString(values.repositoryPath),
      defaultBranch: optionalString(values.defaultBranch),
      defaultTag: optionalString(values.defaultTag),
      buildImage: optionalString(values.buildImage),
      buildContextDir: optionalString(values.buildContextDir),
      dockerfilePath: optionalString(values.dockerfilePath),
      enabled: values.appEnabled !== false,
      metadata: parseJSONObject(values.metadataText, '应用元数据'),
    }),
    services,
    buildSources,
    environmentBindings,
    files,
    executionHints: parseJSONObject(values.executionHintsText, '执行提示'),
    postCreateActions: normalizeTags(values.postCreateActions),
    enabled: values.enabled !== false,
  })
}

function createListItemFromForm(values: BlueprintFormValues, isDraft: boolean): BlueprintListItem {
  return {
    id: isDraft ? 'new' : trimString(values.id),
    key: trimString(values.key) || 'new-app-template',
    name: trimString(values.name) || '新建接入模板草稿',
    description: optionalString(values.description),
    enabled: values.enabled !== false,
    applicationName: trimString(values.appName) || trimString(values.appKey) || '-',
    buildSourceCount: normalizeObjectArray(values.buildSources).length,
    bindingCount: normalizeObjectArray(values.environmentBindings).length,
    fileCount: normalizeObjectArray(values.files).length,
    isDraft,
  }
}

function createListItemFromBlueprint(
  blueprint: DeliveryBlueprint,
  activeValues?: BlueprintFormValues,
): BlueprintListItem {
  if (activeValues) {
    return {
      ...createListItemFromForm({ ...activeValues, id: blueprint.id }, false),
      blueprint,
      updatedAt: blueprint.updatedAt,
    }
  }
  return {
    id: blueprint.id,
    key: blueprint.key,
    name: blueprint.name,
    description: blueprint.description,
    enabled: blueprint.enabled,
    applicationName: blueprint.applicationDraft?.name || blueprint.applicationDraft?.key || '-',
    buildSourceCount: blueprint.buildSources?.length ?? 0,
    bindingCount: blueprint.environmentBindings?.length ?? 0,
    fileCount: blueprint.files?.length ?? 0,
    updatedAt: blueprint.updatedAt,
    blueprint,
  }
}

export function DeliveryBlueprintsPage() {
  const navigate = useNavigate()
  const { message } = App.useApp()
  const queryClient = useQueryClient()
  const permissionSnapshotQuery = usePermissionSnapshot()
  const canManage = hasPermission(permissionSnapshotQuery.data?.data, 'delivery.application.update')
  const [searchParams, setSearchParams] = useSearchParams()
  const [form] = Form.useForm<BlueprintFormValues>()
  const repositoryProvider = Form.useWatch('repositoryProvider', form)
  const [selectedBlueprintId, setSelectedBlueprintId] = useState('')
  const [searchText, setSearchText] = useState('')
  const [activeTabKey, setActiveTabKey] = useState('basic')
  const [filePreset, setFilePreset] = useState<FileTemplatePreset>('dockerfile')
  const [isDirty, setIsDirty] = useState(false)
  const [formSnapshot, setFormSnapshot] = useState<BlueprintFormValues>({})
  const [specModalVisible, setSpecModalVisible] = useState(false)
  const [renderedSpec, setRenderedSpec] = useState<RenderedDeliverySpec | null>(null)
  const suppressFormChangeRef = useRef(false)
  const loadedQueryIdRef = useRef<string | null>(null)

  const blueprintsQuery = useQuery(deliveryQueries.blueprints.list())
  const environmentsQuery = useQuery(deliveryQueries.environments.list())
  const buildTemplatesQuery = useQuery(deliveryQueries.buildTemplates.list())
  const workflowTemplatesQuery = useQuery(deliveryQueries.workflowTemplates.list())
  const gitProjectsQuery = useQuery(
    deliveryQueries.repositories.gitProjects({ limit: 100 }, repositoryProvider === 'gitlab'),
  )
  const createMutation = useMutation(deliveryMutations.blueprints.create(queryClient))
  const updateMutation = useMutation(deliveryMutations.blueprints.update(queryClient))
  const renderMutation = useMutation(deliveryMutations.blueprints.renderSpec())

  const blueprints = blueprintsQuery.data ?? []
  const selectedBlueprint =
    selectedBlueprintId && selectedBlueprintId !== 'new'
      ? (blueprints.find((item) => item.id === selectedBlueprintId) ?? null)
      : null
  const selectedBlueprintUsageQuery = useQuery(
    deliveryQueries.blueprints.usage(selectedBlueprint?.id ?? '', Boolean(selectedBlueprint?.id)),
  )
  const selectedBlueprintUsage = selectedBlueprintUsageQuery.data
  const isNewDraft = selectedBlueprintId === 'new'
  const hasSelection = isNewDraft || !!selectedBlueprint

  const updateSearchParam = useCallback(
    (blueprintId?: string) => {
      setSearchParams(
        (current) => {
          const next = new URLSearchParams(current)
          if (blueprintId) {
            next.set('templateId', blueprintId)
          } else {
            next.delete('templateId')
          }
          next.delete('blueprintId')
          return next
        },
        { replace: true },
      )
    },
    [setSearchParams],
  )

  const confirmDiscardChanges = useCallback(() => {
    if (!isDirty) return true
    return window.confirm('当前接入模板有未保存更改，确认放弃？')
  }, [isDirty])

  const applyFormValues = useCallback(
    (values: BlueprintFormValues, dirtyAfterApply: boolean) => {
      suppressFormChangeRef.current = true
      setFormSnapshot(values)
      form.setFieldsValue(values)
      window.setTimeout(() => {
        suppressFormChangeRef.current = false
        setIsDirty(dirtyAfterApply)
      }, 0)
    },
    [form],
  )

  const loadBlueprint = useCallback(
    (
      blueprint: DeliveryBlueprint,
      options?: { dirtyAfterLoad?: boolean; formOverrides?: BlueprintFormValues; tabKey?: string },
    ) => {
      const values = {
        ...blueprintToFormValues(blueprint),
        ...options?.formOverrides,
      }
      setSelectedBlueprintId(blueprint.id)
      setActiveTabKey(options?.tabKey ?? searchParams.get('tab') ?? 'basic')
      applyFormValues(values, Boolean(options?.dirtyAfterLoad))
      updateSearchParam(blueprint.id)
    },
    [applyFormValues, searchParams, updateSearchParam],
  )

  useEffect(() => {
    const requestedTab = searchParams.get('tab')
    if (
      requestedTab &&
      ['basic', 'application', 'build', 'environment', 'files', 'advanced'].includes(requestedTab)
    ) {
      setActiveTabKey(requestedTab)
    }
    const requestedPreset = searchParams.get('preset') as FileTemplatePreset | null
    if (
      requestedPreset &&
      FILE_TEMPLATE_PRESET_OPTIONS.some((item) => item.value === requestedPreset)
    ) {
      setFilePreset(requestedPreset)
    }
  }, [searchParams])

  useEffect(() => {
    if (!blueprints.length) return
    const queryBlueprintId = searchParams.get('templateId') || searchParams.get('blueprintId')
    // Ignore the previous URL while a local template selection is navigating.
    const queryChanged = queryBlueprintId !== loadedQueryIdRef.current
    loadedQueryIdRef.current = queryBlueprintId
    const queryBlueprint = queryBlueprintId
      ? blueprints.find((item) => item.id === queryBlueprintId)
      : undefined
    if (queryChanged && queryBlueprint && queryBlueprint.id !== selectedBlueprintId && !isDirty) {
      loadBlueprint(queryBlueprint)
      return
    }
    if (!selectedBlueprintId) {
      loadBlueprint(queryBlueprint ?? blueprints[0])
    }
  }, [blueprints, isDirty, loadBlueprint, searchParams, selectedBlueprintId])

  useEffect(() => {
    if (!isDirty) return undefined
    const beforeUnload = (event: BeforeUnloadEvent) => {
      event.preventDefault()
      event.returnValue = ''
    }
    window.addEventListener('beforeunload', beforeUnload)
    return () => window.removeEventListener('beforeunload', beforeUnload)
  }, [isDirty])

  const previewState = useMemo(() => {
    if (!hasSelection) return { error: '', json: '' }
    try {
      return {
        error: '',
        json: JSON.stringify(buildBlueprintPayload(formSnapshot, selectedBlueprint?.id), null, 2),
      }
    } catch (error) {
      return {
        error: error instanceof Error ? error.message : '模板预览生成失败',
        json: '',
      }
    }
  }, [formSnapshot, hasSelection, selectedBlueprint?.id])

  const listItems = useMemo(() => {
    const items = blueprints.map((blueprint) =>
      createListItemFromBlueprint(
        blueprint,
        blueprint.id === selectedBlueprintId ? formSnapshot : undefined,
      ),
    )
    if (isNewDraft) {
      return [createListItemFromForm(formSnapshot, true), ...items]
    }
    return items
  }, [blueprints, formSnapshot, isNewDraft, selectedBlueprintId])

  const visibleListItems = useMemo(() => {
    const keyword = searchText.trim().toLowerCase()
    if (!keyword) return listItems
    return listItems.filter((item) =>
      [item.name, item.key, item.description, item.applicationName].some((value) =>
        String(value || '')
          .toLowerCase()
          .includes(keyword),
      ),
    )
  }, [listItems, searchText])

  const handleNewBlueprint = () => {
    if (!confirmDiscardChanges()) return
    const values = createBlueprintDraftValues()
    setSelectedBlueprintId('new')
    setActiveTabKey('basic')
    applyFormValues(values, true)
    updateSearchParam()
  }

  const handleSelectListItem = (item: BlueprintListItem) => {
    if (item.id === selectedBlueprintId) return
    if (!confirmDiscardChanges()) return
    if (item.isDraft) {
      setSelectedBlueprintId('new')
      return
    }
    if (item.blueprint) {
      loadBlueprint(item.blueprint)
    }
  }

  const handleCancelChanges = () => {
    if (selectedBlueprint) {
      loadBlueprint(selectedBlueprint)
      return
    }
    const firstBlueprint = blueprints[0]
    if (firstBlueprint) {
      loadBlueprint(firstBlueprint)
      return
    }
    form.resetFields()
    setSelectedBlueprintId('')
    setFormSnapshot({})
    setIsDirty(false)
    updateSearchParam()
  }

  const handleSave = async () => {
    try {
      const values = await form.validateFields()
      const payload = buildBlueprintPayload(values, selectedBlueprint?.id)
      if (selectedBlueprint) {
        const usageForSave =
          selectedBlueprintUsage ?? (await selectedBlueprintUsageQuery.refetch()).data
        if (
          shouldConfirmTemplateUsageSave(usageForSave) &&
          !window.confirm(templateUsageConfirmText(selectedBlueprint.name, usageForSave))
        ) {
          return
        }
        await updateMutation.mutateAsync({ id: selectedBlueprint.id, payload })
        void message.success('应用接入模板已更新')
        setFormSnapshot(values)
        setIsDirty(false)
        return
      }
      const createdBlueprint = await createMutation.mutateAsync(payload)
      void message.success('应用接入模板已创建')
      setFormSnapshot(values)
      setIsDirty(false)
      if (createdBlueprint?.id) {
        setSelectedBlueprintId(createdBlueprint.id)
        updateSearchParam(createdBlueprint.id)
      }
    } catch (error) {
      if (error instanceof Error) {
        void message.error(error.message)
      }
    }
  }

  const handleBlueprintEnabledChange = (item: BlueprintListItem, enabled: boolean) => {
    if (item.id !== selectedBlueprintId) {
      if (!confirmDiscardChanges()) return
      if (item.blueprint) {
        loadBlueprint(item.blueprint, {
          dirtyAfterLoad: true,
          formOverrides: { enabled },
          tabKey: 'basic',
        })
      }
      return
    }
    form.setFieldsValue({ enabled })
    setFormSnapshot((current) => ({ ...current, enabled }))
    setIsDirty(true)
  }

  const handleRenderSpec = () => {
    if (!selectedBlueprint) {
      void message.warning('请先保存模板，再渲染规范')
      return
    }
    renderMutation.mutate(selectedBlueprint.id, {
      onSuccess: (payload) => {
        setRenderedSpec(payload)
        setSpecModalVisible(true)
      },
      onError: (error) =>
        void message.error(error instanceof Error ? error.message : '渲染规范失败'),
    })
  }

  const buildSourceOptions = normalizeObjectArray(formSnapshot.buildSources).map((item, index) => ({
    value: trimString(item.id) || `source-${index + 1}`,
    label: trimString(item.name) || `构建源 ${index + 1}`,
  }))

  const environmentOptions = Array.from(
    new Map(
      [
        ...DEFAULT_ENVIRONMENT_OPTIONS,
        ...(environmentsQuery.data ?? []).map((item) => ({
          value: item.environmentKey || item.environmentId,
          label: item.environmentKey || item.environmentId,
          environmentId: item.environmentId,
        })),
      ].map((item) => [item.value, item]),
    ).values(),
  )
  const workflowTemplateOptions = (workflowTemplatesQuery.data ?? []).map((item) => ({
    value: item.id,
    label: item.name,
  }))
  const buildTemplateOptions = (buildTemplatesQuery.data ?? []).map((item) => ({
    value: item.id,
    label: item.name,
  }))
  const gitProjectOptions = (gitProjectsQuery.data ?? []).map((item) => ({
    value: item.id,
    label: `${item.path} (${item.id})`,
  }))

  const designerTabs = [
    {
      key: 'basic',
      label: '基础信息',
      children: (
        <div className="soha-delivery-blueprint-form-grid">
          <Form.Item
            name="key"
            label="模板 Key"
            rules={[{ required: true, message: '请输入模板 Key' }]}
          >
            <Input placeholder="node-service-standard" />
          </Form.Item>
          <Form.Item
            name="name"
            label="模板名称"
            rules={[{ required: true, message: '请输入模板名称' }]}
          >
            <Input placeholder="Node 服务标准接入" />
          </Form.Item>
          <Form.Item
            className="soha-delivery-blueprint-switch-field"
            name="enabled"
            label="启用"
            valuePropName="checked"
          >
            <Switch />
          </Form.Item>
          <Form.Item
            className="soha-delivery-blueprint-form-grid__wide"
            name="description"
            label="说明"
          >
            <Input.TextArea rows={3} placeholder="适用语言、仓库形态、默认环境和平台规范说明" />
          </Form.Item>
          <Form.Item
            className="soha-delivery-blueprint-form-grid__wide"
            name="postCreateActions"
            label="接入后动作"
          >
            <Select
              mode="tags"
              tokenSeparators={[',', '\n']}
              options={[
                { value: 'render_spec', label: '渲染规范' },
                { value: 'bootstrap_application', label: '创建应用档案' },
                { value: 'create_environment_bindings', label: '创建环境绑定' },
                { value: 'create_manifest_package', label: '生成扩展资源包' },
              ]}
            />
          </Form.Item>
        </div>
      ),
    },
    {
      key: 'application',
      label: '应用档案',
      children: (
        <div className="soha-delivery-blueprint-form-grid">
          <Form.Item
            name="appName"
            label="应用名称"
            rules={[{ required: true, message: '请输入应用名称' }]}
          >
            <Input placeholder="Sample API" />
          </Form.Item>
          <Form.Item
            name="appKey"
            label="应用 Key"
            rules={[{ required: true, message: '请输入应用 Key' }]}
          >
            <Input placeholder="sample-api" />
          </Form.Item>
          <Form.Item
            name="appGroup"
            label="应用分组"
            rules={[{ required: true, message: '请输入应用分组' }]}
          >
            <Input placeholder="mall / payment / frontend" />
          </Form.Item>
          <Form.Item name="language" label="语言">
            <Select options={LANGUAGE_OPTIONS} />
          </Form.Item>
          <Form.Item name="businessLineId" label="业务线 ID">
            <Input placeholder="business-line-id" />
          </Form.Item>
          <Form.Item name="ownerTeam" label="负责团队">
            <Input placeholder="payments-dev" />
          </Form.Item>
          <Form.Item name="repositoryProvider" label="代码源">
            <Select options={REPOSITORY_PROVIDER_OPTIONS} />
          </Form.Item>
          <Form.Item name="repositoryProjectId" label="仓库项目 ID">
            {repositoryProvider === 'gitlab' && gitProjectOptions.length ? (
              <Select
                allowClear
                showSearch={{ optionFilterProp: 'label' }}
                loading={gitProjectsQuery.isLoading}
                options={gitProjectOptions}
                placeholder="选择 GitLab 项目"
              />
            ) : (
              <Input placeholder="project-id" />
            )}
          </Form.Item>
          <Form.Item
            className="soha-delivery-blueprint-form-grid__wide"
            name="repositoryPath"
            label="仓库路径"
          >
            <Input placeholder="group/project/service" />
          </Form.Item>
          <Form.Item name="defaultBranch" label="默认分支">
            <Input placeholder="main" />
          </Form.Item>
          <Form.Item name="defaultTag" label="默认 Tag">
            <Input placeholder="v1.0.0" />
          </Form.Item>
          <Form.Item
            className="soha-delivery-blueprint-switch-field"
            name="appEnabled"
            label="应用启用"
            valuePropName="checked"
          >
            <Switch />
          </Form.Item>
          <Form.Item
            className="soha-delivery-blueprint-form-grid__wide"
            name="appDescription"
            label="应用说明"
          >
            <Input.TextArea rows={3} />
          </Form.Item>
          <Form.Item noStyle name="buildImage">
            <Input type="hidden" />
          </Form.Item>
          <Form.Item noStyle name="buildContextDir">
            <Input type="hidden" />
          </Form.Item>
          <Form.Item noStyle name="dockerfilePath">
            <Input type="hidden" />
          </Form.Item>
        </div>
      ),
    },
    {
      key: 'services',
      label: '服务组件',
      children: (
        <Form.List
          name="services"
          rules={[
            {
              validator: async (_, services) => {
                if (!services?.length) throw new Error('至少配置一个服务组件')
                const keys = services
                  .map((service: Record<string, unknown>) => trimString(service?.key))
                  .filter(Boolean)
                if (new Set(keys).size !== keys.length) throw new Error('服务 Key 不能重复')
              },
            },
          ]}
        >
          {(fields, { add, remove }, { errors }) => (
            <div className="soha-delivery-blueprint-repeat-list">
              <div className="soha-delivery-blueprint-repeat-list__toolbar">
                <Text type="secondary">定义模板生成的多个独立构建和发布服务。</Text>
                <Button
                  icon={<PlusOutlined />}
                  onClick={() => add(createDefaultService(fields.length))}
                >
                  添加服务
                </Button>
              </div>
              {fields.map((field, index) => (
                <div className="soha-delivery-blueprint-repeat-item" key={field.key}>
                  <div className="soha-delivery-blueprint-repeat-item__head">
                    <strong>{`服务 ${index + 1}`}</strong>
                    <Button
                      danger
                      icon={<DeleteOutlined />}
                      size="small"
                      onClick={() => remove(field.name)}
                    >
                      删除
                    </Button>
                  </div>
                  <div className="soha-delivery-blueprint-form-grid">
                    <Form.Item
                      name={[field.name, 'name']}
                      label="服务名称"
                      rules={[{ required: true, message: '请输入服务名称' }]}
                    >
                      <Input placeholder="API" />
                    </Form.Item>
                    <Form.Item
                      name={[field.name, 'key']}
                      label="服务 Key"
                      rules={[{ required: true, message: '请输入服务 Key' }]}
                    >
                      <Input placeholder="api" />
                    </Form.Item>
                    <Form.Item name={[field.name, 'serviceKind']} label="服务类型">
                      <Select options={SERVICE_KIND_OPTIONS} />
                    </Form.Item>
                    <Form.Item name={[field.name, 'buildSourceId']} label="构建源">
                      <Select allowClear options={buildSourceOptions} />
                    </Form.Item>
                    <Form.Item
                      name={[field.name, 'deploymentTemplateText']}
                      label="部署模板与参数 (JSON)"
                    >
                      <Input.TextArea
                        rows={4}
                        placeholder='{"templateId":"…","version":1,"parameters":{}}'
                      />
                    </Form.Item>
                    <Form.Item name={[field.name, 'containersText']} label="容器配置 (JSON)">
                      <Input.TextArea rows={4} placeholder='[{"name":"main"}]' />
                    </Form.Item>
                    <Form.Item name={[field.name, 'dockerfilePath']} label="Dockerfile">
                      <Input placeholder="Dockerfile" />
                    </Form.Item>
                    <Form.Item name={[field.name, 'buildContextDir']} label="构建目录">
                      <Input placeholder="." />
                    </Form.Item>
                    <Form.Item name={[field.name, 'repositoryPath']} label="仓库子路径">
                      <Input placeholder="services/api" />
                    </Form.Item>
                    <Form.Item
                      className="soha-delivery-blueprint-switch-field"
                      name={[field.name, 'enabled']}
                      label="启用"
                      valuePropName="checked"
                    >
                      <Switch />
                    </Form.Item>
                  </div>
                </div>
              ))}
              <Form.ErrorList errors={errors} />
            </div>
          )}
        </Form.List>
      ),
    },
    {
      key: 'build',
      label: '构建源',
      children: (
        <Form.List name="buildSources">
          {(fields, { add, remove }) => (
            <div className="soha-delivery-blueprint-repeat-list">
              <div className="soha-delivery-blueprint-repeat-list__toolbar">
                <Text type="secondary">定义应用创建后默认可选的构建来源。</Text>
                <Button
                  icon={<PlusOutlined />}
                  onClick={() => add(createDefaultBuildSource(fields.length))}
                >
                  添加构建源
                </Button>
              </div>
              {fields.length === 0 ? (
                <ManagementState
                  bordered={false}
                  compact
                  kind="empty"
                  title="暂无构建源"
                  description="至少添加一个构建源，才能形成可发布的应用接入规范。"
                />
              ) : null}
              {fields.map((field, index) => (
                <div className="soha-delivery-blueprint-repeat-item" key={field.key}>
                  <div className="soha-delivery-blueprint-repeat-item__head">
                    <strong>{`构建源 ${index + 1}`}</strong>
                    <Button
                      danger
                      icon={<DeleteOutlined />}
                      size="small"
                      onClick={() => remove(field.name)}
                    >
                      删除
                    </Button>
                  </div>
                  <div className="soha-delivery-blueprint-form-grid">
                    <Form.Item
                      name={[field.name, 'id']}
                      label="Source ID"
                      rules={[{ required: true, message: '请输入 Source ID' }]}
                    >
                      <Input placeholder="source-1" />
                    </Form.Item>
                    <Form.Item
                      name={[field.name, 'name']}
                      label="名称"
                      rules={[{ required: true, message: '请输入名称' }]}
                    >
                      <Input placeholder="Repo Dockerfile" />
                    </Form.Item>
                    <Form.Item name={[field.name, 'type']} label="类型">
                      <Select options={BUILD_SOURCE_TYPE_OPTIONS} />
                    </Form.Item>
                    <Form.Item name={[field.name, 'buildImage']} label="构建镜像">
                      <Input placeholder="node:22 / golang:1.23" />
                    </Form.Item>
                    <Form.Item name={[field.name, 'defaultTag']} label="默认 Tag">
                      <Input placeholder="latest / {{branch}}-{{sha}}" />
                    </Form.Item>
                    <Form.Item
                      className="soha-delivery-blueprint-switch-field"
                      name={[field.name, 'enabled']}
                      label="启用"
                      valuePropName="checked"
                    >
                      <Switch />
                    </Form.Item>
                    <Form.Item
                      className="soha-delivery-blueprint-switch-field"
                      name={[field.name, 'isDefault']}
                      label="默认"
                      valuePropName="checked"
                    >
                      <Switch />
                    </Form.Item>
                    <Form.Item noStyle shouldUpdate>
                      {({ getFieldValue }) => {
                        const sourceType = getFieldValue(['buildSources', field.name, 'type'])
                        if (sourceType === 'platform_build_template') {
                          return (
                            <Form.Item
                              className="soha-delivery-blueprint-form-grid__wide"
                              name={[field.name, 'buildTemplateId']}
                              label="平台构建模板"
                            >
                              <Select
                                allowClear
                                showSearch={{ optionFilterProp: 'label' }}
                                loading={buildTemplatesQuery.isLoading}
                                options={buildTemplateOptions}
                                placeholder="选择构建模板"
                              />
                            </Form.Item>
                          )
                        }
                        if (sourceType === 'external_pipeline') {
                          return (
                            <ExternalPipelineFields prefix={[field.name, 'externalPipeline']} />
                          )
                        }
                        return (
                          <>
                            <Form.Item name={[field.name, 'contextDir']} label="构建目录">
                              <Input placeholder="." />
                            </Form.Item>
                            <Form.Item
                              name={[field.name, 'dockerfilePath']}
                              label="Dockerfile 路径"
                            >
                              <Input placeholder="Dockerfile" />
                            </Form.Item>
                          </>
                        )
                      }}
                    </Form.Item>
                    <Collapse
                      className="soha-delivery-blueprint-form-grid__wide"
                      ghost
                      items={[
                        {
                          key: 'source-json',
                          label: '高级扩展配置（JSON，兼容旧模板）',
                          children: (
                            <Form.Item name={[field.name, 'configText']} noStyle>
                              <Input.TextArea
                                rows={4}
                                spellCheck={false}
                                placeholder="可选：保留执行器专用扩展字段"
                              />
                            </Form.Item>
                          ),
                        },
                      ]}
                    />
                  </div>
                </div>
              ))}
            </div>
          )}
        </Form.List>
      ),
    },
    {
      key: 'environment',
      label: '发布计划',
      children: (
        <Form.List name="environmentBindings">
          {(fields, { add, remove }) => (
            <div className="soha-delivery-blueprint-repeat-list">
              <div className="soha-delivery-blueprint-repeat-list__toolbar">
                <Text type="secondary">
                  用环境、构建来源和发布流程组成可复用的发布计划；复杂策略放在高级配置中。
                </Text>
                <Button
                  icon={<PlusOutlined />}
                  onClick={() => add(createDefaultEnvironmentBinding(fields.length))}
                >
                  添加发布计划
                </Button>
              </div>
              {fields.length === 0 ? (
                <ManagementState
                  bordered={false}
                  compact
                  kind="empty"
                  title="暂无发布计划"
                  description="可以先保存应用档案模板，后续再补发布计划。"
                />
              ) : null}
              {fields.map((field, index) => (
                <div className="soha-delivery-blueprint-repeat-item" key={field.key}>
                  <div className="soha-delivery-blueprint-repeat-item__head">
                    <strong>{`发布计划 ${index + 1}`}</strong>
                    <Button
                      danger
                      icon={<DeleteOutlined />}
                      size="small"
                      onClick={() => remove(field.name)}
                    >
                      删除
                    </Button>
                  </div>
                  <div className="soha-delivery-blueprint-form-grid">
                    <Form.Item name={[field.name, 'environmentKey']} label="环境">
                      <Select
                        showSearch={{ optionFilterProp: 'label' }}
                        options={environmentOptions}
                        placeholder="选择 dev / test / prod"
                        onChange={(value) => {
                          const option = environmentOptions.find((item) => item.value === value)
                          form.setFieldValue(
                            ['environmentBindings', field.name, 'environmentId'],
                            option && 'environmentId' in option ? option.environmentId : undefined,
                          )
                        }}
                      />
                    </Form.Item>
                    <Form.Item noStyle name={[field.name, 'environmentId']}>
                      <Input type="hidden" />
                    </Form.Item>
                    <Form.Item name={[field.name, 'workflowTemplateId']} label="发布流程模板">
                      <Select
                        allowClear
                        showSearch={{ optionFilterProp: 'label' }}
                        loading={workflowTemplatesQuery.isLoading}
                        options={workflowTemplateOptions}
                        placeholder="选择发布流程模板"
                      />
                    </Form.Item>
                    <Form.Item name={[field.name, 'buildSourceId']} label="构建来源">
                      <Select allowClear options={buildSourceOptions} />
                    </Form.Item>
                    <Form.Item name={[field.name, 'refType']} label="引用类型">
                      <Select options={REF_TYPE_OPTIONS} />
                    </Form.Item>
                    <Form.Item name={[field.name, 'refValue']} label="引用值">
                      <Input placeholder="main / v1.0.0" />
                    </Form.Item>
                    <Form.Item name={[field.name, 'imageTagMode']} label="镜像 Tag 模式">
                      <Select options={IMAGE_TAG_MODE_OPTIONS} />
                    </Form.Item>
                    <Form.Item name={[field.name, 'imageTagTemplate']} label="镜像 Tag 模板">
                      <Input placeholder="{{branch}}-{{timestamp}}" />
                    </Form.Item>
                    <Form.Item name={[field.name, 'actionKind']} label="发布动作">
                      <Select options={ACTION_KIND_OPTIONS} />
                    </Form.Item>
                    <Form.Item
                      className="soha-delivery-blueprint-switch-field"
                      name={[field.name, 'requiresApproval']}
                      label="需要审批"
                      valuePropName="checked"
                    >
                      <Switch />
                    </Form.Item>
                    <Form.Item name={[field.name, 'approverRoles']} label="审批角色">
                      <Select
                        mode="tags"
                        tokenSeparators={[',', '\n']}
                        placeholder="release-manager, ops-lead"
                      />
                    </Form.Item>
                    <Form.Item
                      className="soha-delivery-blueprint-switch-field"
                      name={[field.name, 'autoRollback']}
                      label="自动回滚"
                      valuePropName="checked"
                    >
                      <Switch />
                    </Form.Item>
                    <Form.Item name={[field.name, 'rolloutTimeoutSeconds']} label="Rollout 超时">
                      <InputNumber min={30} step={30} style={{ width: '100%' }} />
                    </Form.Item>
                    <Form.Item name={[field.name, 'verificationMode']} label="验证模式">
                      <Select options={VERIFICATION_MODE_OPTIONS} />
                    </Form.Item>
                    <Collapse
                      className="soha-delivery-blueprint-form-grid__wide"
                      ghost
                      items={[
                        {
                          key: 'plan-advanced',
                          label: '高级策略与目标（可选）',
                          children: (
                            <div className="soha-delivery-blueprint-form-grid">
                              <Form.Item name={[field.name, 'businessLineId']} label="业务线 ID">
                                <Input />
                              </Form.Item>
                              <Form.Item
                                name={[field.name, 'strategyProfileId']}
                                label="发布策略 Profile"
                              >
                                <Input placeholder="rolling-default" />
                              </Form.Item>
                              <Form.Item name={[field.name, 'promotionPolicyId']} label="晋级策略">
                                <Input />
                              </Form.Item>
                              <Form.Item name={[field.name, 'artifactPolicyId']} label="制品策略">
                                <Input />
                              </Form.Item>
                              <Form.Item
                                className="soha-delivery-blueprint-form-grid__wide"
                                name={[field.name, 'buildVariablesText']}
                                label="构建变量(JSON)"
                              >
                                <Input.TextArea rows={3} spellCheck={false} />
                              </Form.Item>
                              <Form.Item
                                className="soha-delivery-blueprint-form-grid__wide"
                                name={[field.name, 'buildArgsText']}
                                label="构建参数(JSON)"
                              >
                                <Input.TextArea rows={3} spellCheck={false} />
                              </Form.Item>
                              <Form.Item
                                className="soha-delivery-blueprint-form-grid__wide"
                                name={[field.name, 'resourceSelectorText']}
                                label="资源选择器(JSON)"
                              >
                                <Input.TextArea
                                  rows={3}
                                  spellCheck={false}
                                  placeholder='{"matchLabels":{"app":"sample"}}'
                                />
                              </Form.Item>
                              <Form.Item
                                className="soha-delivery-blueprint-form-grid__wide"
                                name={[field.name, 'targetsText']}
                                label="发布目标矩阵(JSON Array)"
                              >
                                <Input.TextArea
                                  rows={6}
                                  spellCheck={false}
                                  placeholder='[{"clusterId":"dev","namespace":"default","targetKind":"k8s_workload","workloadKind":"Deployment","workloadName":"sample","enabled":true}]'
                                />
                              </Form.Item>
                            </div>
                          ),
                        },
                      ]}
                    />
                  </div>
                </div>
              ))}
            </div>
          )}
        </Form.List>
      ),
    },
    {
      key: 'files',
      label: '文件模板',
      children: (
        <Form.List name="files">
          {(fields, { add, remove }) => (
            <div className="soha-delivery-blueprint-repeat-list">
              <div className="soha-delivery-blueprint-repeat-list__toolbar">
                <Text type="secondary">
                  按预设快速添加 Dockerfile、YAML、Helm 或 Kustomize 模板。
                </Text>
                <Space className="soha-delivery-blueprint-file-preset-actions">
                  <Select
                    value={filePreset}
                    options={FILE_TEMPLATE_PRESET_OPTIONS}
                    onChange={(value: FileTemplatePreset) => setFilePreset(value)}
                  />
                  <Button
                    icon={<PlusOutlined />}
                    onClick={() => add(createDefaultFileTemplate(fields.length, filePreset))}
                  >
                    添加预设
                  </Button>
                </Space>
              </div>
              {fields.length === 0 ? (
                <ManagementState
                  bordered={false}
                  compact
                  kind="empty"
                  title="暂无文件模板"
                  description="模板可以只创建平台对象，也可以附带可复用的文件模板。"
                />
              ) : null}
              {fields.map((field, index) => (
                <div className="soha-delivery-blueprint-repeat-item" key={field.key}>
                  <div className="soha-delivery-blueprint-repeat-item__head">
                    <strong>{`文件模板 ${index + 1}`}</strong>
                    <Button
                      danger
                      icon={<DeleteOutlined />}
                      size="small"
                      onClick={() => remove(field.name)}
                    >
                      删除
                    </Button>
                  </div>
                  <div className="soha-delivery-blueprint-form-grid">
                    <Form.Item
                      name={[field.name, 'path']}
                      label="路径"
                      rules={[{ required: true, message: '请输入文件路径' }]}
                    >
                      <Input placeholder="Dockerfile / deploy/values.yaml" />
                    </Form.Item>
                    <Form.Item name={[field.name, 'kind']} label="类型">
                      <Select options={FILE_KIND_OPTIONS} />
                    </Form.Item>
                    <Form.Item name={[field.name, 'purpose']} label="用途">
                      <Input placeholder="构建镜像 / Helm values / K8s deployment" />
                    </Form.Item>
                    <Form.Item
                      className="soha-delivery-blueprint-switch-field"
                      name={[field.name, 'required']}
                      label="必需"
                      valuePropName="checked"
                    >
                      <Switch />
                    </Form.Item>
                    <Form.Item
                      className="soha-delivery-blueprint-form-grid__wide"
                      name={[field.name, 'content']}
                      label="内容"
                    >
                      <Input.TextArea
                        className="soha-delivery-blueprint-code-area"
                        rows={10}
                        spellCheck={false}
                      />
                    </Form.Item>
                  </div>
                </div>
              ))}
            </div>
          )}
        </Form.List>
      ),
    },
    {
      key: 'advanced',
      label: '高级配置',
      children: (
        <div className="soha-delivery-blueprint-advanced">
          <Form.Item name="metadataText" label="应用元数据(JSON)">
            <Input.TextArea
              rows={4}
              spellCheck={false}
              placeholder='{"serviceKind":"kubernetes_workload"}'
            />
          </Form.Item>
          <Form.Item name="executionHintsText" label="执行提示(JSON)">
            <Input.TextArea
              rows={5}
              spellCheck={false}
              placeholder='{"aiGatewaySkill":"delivery-developer"}'
            />
          </Form.Item>
          {previewState.error ? <Text type="danger">{previewState.error}</Text> : null}
          <pre className="soha-json-block soha-delivery-blueprint-json-preview">
            {previewState.error ? '请修正上方 JSON 字段后再查看完整 payload。' : previewState.json}
          </pre>
        </div>
      ),
    },
  ]

  const blueprintToolbar = (
    <header className="soha-delivery-blueprint-toolbar" aria-label="当前接入模板操作">
      {hasSelection ? isDirty ? <Tag color="gold">未保存</Tag> : <Tag>已同步</Tag> : null}
      <Space wrap>
        <Button
          icon={<SaveOutlined />}
          type="primary"
          disabled={!hasSelection || !canManage}
          loading={createMutation.isPending || updateMutation.isPending}
          onClick={() => void handleSave()}
        >
          保存
        </Button>
        <Button disabled={!hasSelection || !isDirty} onClick={handleCancelChanges}>
          取消更改
        </Button>
        <Button
          icon={<EyeOutlined />}
          disabled={!hasSelection}
          loading={renderMutation.isPending}
          onClick={handleRenderSpec}
        >
          渲染规范
        </Button>
      </Space>
    </header>
  )

  const blueprintList = (
    <ManagementSearchableListPane
      activeKey={selectedBlueprintId}
      className="soha-delivery-blueprint-list"
      emptyDescription="新建模板后，可在右侧维护应用档案、构建源、发布计划和文件模板。"
      emptyTitle="暂无接入模板"
      getItemKey={(item) => item.id}
      isError={blueprintsQuery.isError}
      isLoading={blueprintsQuery.isLoading}
      itemClassName="soha-delivery-blueprint-list__item"
      items={visibleListItems}
      searchPlaceholder="搜索接入模板"
      searchValue={searchText}
      searchActions={
        <>
          <ManagementIconButton
            aria-label="新建模板"
            tooltip="新建模板"
            icon={<PlusOutlined />}
            color="primary"
            variant="solid"
            disabled={!canManage}
            onClick={handleNewBlueprint}
          />

          <ManagementRefreshButton
            tooltip="刷新"
            aria-label="刷新"
            loading={blueprintsQuery.isFetching}
            onClick={() => {
              if (confirmDiscardChanges()) void blueprintsQuery.refetch()
            }}
          />
        </>
      }
      onItemSelect={handleSelectListItem}
      onRetry={() => void blueprintsQuery.refetch()}
      onSearchChange={setSearchText}
      renderItemActions={(item) => (
        <span className="soha-delivery-blueprint-list__item-actions">
          <Switch
            checked={item.enabled}
            disabled={!canManage}
            size="small"
            onChange={(checked) => handleBlueprintEnabledChange(item, checked)}
          />
          <ManagementIconButton
            aria-label="编辑模板"
            icon={<EditOutlined />}
            size="small"
            tooltip="编辑"
            onClick={() => {
              handleSelectListItem(item)
              setActiveTabKey('basic')
            }}
          />
        </span>
      )}
      renderItem={(item) => (
        <>
          <span className="soha-delivery-blueprint-list__item-head">
            <span className="soha-delivery-blueprint-list__item-main">
              <strong>{item.name}</strong>
              <Text type="secondary">{item.key}</Text>
            </span>
          </span>
          <span className="soha-delivery-blueprint-list__item-meta">
            <StatusTag value={item.enabled ? 'enabled' : 'disabled'} />
            <Tag>{`构建 ${item.buildSourceCount}`}</Tag>
            <Tag>{`计划 ${item.bindingCount}`}</Tag>
            <Tag>{`模板 ${item.fileCount}`}</Tag>
            {item.isDraft ? <Tag color="gold">草稿</Tag> : null}
          </span>
          <Text type="secondary" className="text-xs">
            {item.applicationName}
          </Text>
          <Text type="secondary" className="text-xs">
            {item.updatedAt ? formatDateTime(item.updatedAt) : '尚未保存'}
          </Text>
        </>
      )}
    />
  )

  const blueprintDesigner = hasSelection ? (
    <Form
      className="soha-delivery-blueprint-form"
      disabled={!canManage}
      form={form}
      layout="vertical"
      onValuesChange={(_changedValues, allValues) => {
        if (suppressFormChangeRef.current) return
        setFormSnapshot(allValues)
        setIsDirty(true)
      }}
    >
      <TemplateUsageImpactPanel
        loading={selectedBlueprintUsageQuery.isFetching && !!selectedBlueprint}
        onNavigate={navigate}
        usage={selectedBlueprintUsage}
      />
      <Tabs
        activeKey={activeTabKey}
        className="soha-delivery-blueprint-tabs"
        destroyOnHidden={false}
        items={designerTabs}
        onChange={(key) => {
          setActiveTabKey(key)
          setSearchParams(
            (current) => {
              const next = new URLSearchParams(current)
              next.set('tab', key)
              return next
            },
            { replace: true },
          )
        }}
      />
    </Form>
  ) : (
    <ManagementState
      bordered={false}
      kind="select-scope"
      title="选择或新建接入模板"
      description="左侧选择模板后，在右侧设计应用档案、构建源、发布计划和文件模板。"
    />
  )

  return (
    <TemplateDesignerShell
      className="soha-page soha-delivery-blueprint-page"
      designer={
        <>
          {blueprintToolbar}
          {blueprintDesigner}
        </>
      }
      designerClassName="soha-delivery-blueprint-designer"
      list={blueprintList}
      workspaceClassName="soha-delivery-blueprint-workspace"
    >
      <Modal
        width={960}
        title="渲染结果"
        open={specModalVisible}
        onCancel={() => setSpecModalVisible(false)}
        footer={null}
      >
        <pre className="soha-json-block">{JSON.stringify(renderedSpec ?? {}, null, 2)}</pre>
      </Modal>
    </TemplateDesignerShell>
  )
}
