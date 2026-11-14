import { ApiError, isApiError } from '@/services/api-error'
import { deliveryApi } from './api'
import { deploymentReferenceSignature, saveServiceDeployment } from './service-deployment-setup'
import type { ServiceDeploymentSetup } from './service-deployment-setup'
import type { ManifestPackage } from './manifests/types'
import type {
  BuildRepositoryBinding,
  BuildSource,
  DeliveryRecordInput,
  DeliveryRepository,
} from './types'

export function buildSourceRepositoryBindings(
  source?: Pick<BuildSource, 'type' | 'config'>,
): BuildRepositoryBinding[] {
  const configured = source?.config?.repositoryBindings
  if (Array.isArray(configured) && configured.length > 0) {
    return configured.filter((item) => Boolean(item?.repositoryId?.trim()))
  }
  const repositoryId = source?.config?.repositoryId
  return typeof repositoryId === 'string' && repositoryId.trim()
    ? [{ repositoryId, allowCommitSelection: false, submodules: false }]
    : []
}

export type RepositoryDraft = {
  draftId: string
  input: {
    name: string
    provider: 'gitlab' | 'git'
    protocol: 'https' | 'ssh'
    url: string
    path: string
    gitlabProjectId?: string
    credentialRef?: string
    defaultBranch: string
  }
}

export type ServiceSetupDraft = {
  deployment?: ServiceDeploymentSetup
  applicationId: string
  serviceId?: string
  service: DeliveryRecordInput
  buildSource?: BuildSource
  originalBuildSource?: BuildSource
  repositories: RepositoryDraft[]
}

export type ServiceSetupCheckpoint = {
  manifestPackage?: ManifestPackage
  pendingManifestPackage?: string[]
  serviceVersion?: number
  deploymentComplete?: boolean
  repositories: Record<string, DeliveryRepository>
  pendingRepository?: { draftId: string; priorIds: string[] }
  buildSourceSaved?: boolean
  pendingService?: string[]
  pendingServiceUpdate?: boolean
  serviceId?: string
}

const uncertainWrite = (error: unknown) =>
  !isApiError(error) || error.status === 0 || error.status >= 500

function serviceInputSignature(input: DeliveryRecordInput) {
  const textFields = [
    'key',
    'name',
    'description',
    'serviceKind',
    'ownerTeam',
    'repositoryProvider',
    'repositoryId',
    'repositoryProjectId',
    'repositoryPath',
    'defaultBranch',
    'buildSourceId',
  ]
  const containerTextFields = [
    'name',
    'imageRepository',
    'defaultTagTemplate',
    'dockerfilePath',
    'buildContextDir',
  ]
  return deploymentReferenceSignature({
    ...Object.fromEntries(textFields.map((key) => [key, String(input[key] ?? '').trim()])),
    enabled: input.enabled ?? false,
    metadata: input.metadata ?? {},
    deploymentTemplate: input.deploymentTemplate,
    containers: ((input.containers ?? []) as DeliveryRecordInput[])
      .map((container) => ({
        name: String(container.name ?? '').trim(),
        ...Object.fromEntries(
          containerTextFields.map((key) => [key, String(container[key] ?? '').trim()]),
        ),
        runtimePorts: container.runtimePorts ?? [],
        envSchema: container.envSchema ?? {},
        resourceProfile: container.resourceProfile ?? {},
        healthCheck: container.healthCheck ?? {},
        metadata: container.metadata ?? {},
      }))
      .sort((a, b) => String(a.name).localeCompare(String(b.name))),
  })
}

function buildSourceSignature(source?: BuildSource) {
  if (!source) return ''
  const {
    id,
    name,
    type,
    enabled,
    isDefault,
    buildImage = '',
    defaultTag = '',
    config = {},
  } = source
  return JSON.stringify(
    { id, name, type, enabled, isDefault, buildImage, defaultTag, config },
    (_key, value) =>
      value && typeof value === 'object' && !Array.isArray(value)
        ? Object.fromEntries(Object.entries(value).sort(([a], [b]) => a.localeCompare(b)))
        : value,
  )
}

// Keep this checkpoint with the submitted draft. Never replay an uncertain POST.
export async function saveServiceSetup({
  draft,
  checkpoint,
}: {
  draft: ServiceSetupDraft
  checkpoint: ServiceSetupCheckpoint
}): Promise<string> {
  if (checkpoint.serviceId) {
    await saveServiceDeployment(draft, checkpoint)
    return checkpoint.serviceId
  }
  const { applicationId } = draft
  const sourceBindings = buildSourceRepositoryBindings(draft.buildSource)
  const repositoryIds = new Set(sourceBindings?.map((binding) => binding.repositoryId))
  for (const repository of draft.repositories.filter((item) => repositoryIds.has(item.draftId))) {
    if (checkpoint.repositories[repository.draftId]) continue
    const pending = checkpoint.pendingRepository
    if (pending) {
      const candidates = (await deliveryApi.repositories.list({ applicationId })).filter(
        (item) =>
          !pending.priorIds.includes(item.id) &&
          item.name === repository.input.name &&
          item.url === repository.input.url &&
          item.path === repository.input.path &&
          item.provider === repository.input.provider &&
          item.protocol === repository.input.protocol &&
          (item.gitlabProjectId || '') === (repository.input.gitlabProjectId || '') &&
          item.defaultBranch === repository.input.defaultBranch,
      )
      if (pending.draftId !== repository.draftId || candidates.length !== 1)
        throw new Error('仓库创建结果尚未确认，请检查共享代码仓库后再继续；不会重复创建。')
      checkpoint.repositories[repository.draftId] = candidates[0]
      delete checkpoint.pendingRepository
      continue
    }
    checkpoint.pendingRepository = {
      draftId: repository.draftId,
      priorIds: (await deliveryApi.repositories.list({ applicationId })).map((item) => item.id),
    }
    try {
      checkpoint.repositories[repository.draftId] = await deliveryApi.repositories.create({
        ...repository.input,
        gitlabProjectId:
          repository.input.provider === 'gitlab' ? repository.input.gitlabProjectId : undefined,
        applicationIds: [applicationId],
      })
      if (!checkpoint.repositories[repository.draftId]?.id) {
        delete checkpoint.repositories[repository.draftId]
        throw new Error('仓库创建响应缺少 ID，请重试以核实保存结果。')
      }
      delete checkpoint.pendingRepository
    } catch (error) {
      if (!uncertainWrite(error)) delete checkpoint.pendingRepository
      throw error
    }
  }

  const resolveRepositoryId = (id?: string) => (id ? (checkpoint.repositories[id]?.id ?? id) : id)
  const source = draft.buildSource
    ? {
        ...draft.buildSource,
        config: {
          ...draft.buildSource.config,
          repositoryId: resolveRepositoryId(sourceBindings?.[0]?.repositoryId),
          repositoryBindings: sourceBindings?.map((binding) => ({
            ...binding,
            repositoryId: resolveRepositoryId(binding.repositoryId)!,
          })),
        },
      }
    : undefined
  if (source && !checkpoint.buildSourceSaved) {
    const { application } = await deliveryApi.applications.detail(applicationId)
    const current = application.buildSources?.find((item) => item.id === source.id)
    if (current && buildSourceSignature(current) === buildSourceSignature(source)) {
      checkpoint.buildSourceSaved = true
    } else {
      if (buildSourceSignature(current) !== buildSourceSignature(draft.originalBuildSource))
        throw new ApiError(
          409,
          '共享构建已被其他人修改，请重新加载共享构建后合并；当前草稿已保留。',
        )
      const buildSources = (application.buildSources ?? [])
        .filter((item) => item.id !== source.id)
        .map((item) => (source.isDefault ? { ...item, isDefault: false } : item))
        .concat(source)
      await deliveryApi.applications.update(applicationId, {
        ...application,
        expectedVersion: application.version,
        repositoryIds: [
          ...new Set([
            ...(application.repositoryIds ?? []),
            ...(source.config.repositoryBindings ?? []).map((binding) => binding.repositoryId),
          ]),
        ],
        buildSources,
      })
      checkpoint.buildSourceSaved = true
    }
  }

  const service: DeliveryRecordInput = {
    ...draft.service,
    repositoryId: resolveRepositoryId(draft.service.repositoryId as string | undefined),
  }
  if (draft.serviceId) {
    if (checkpoint.pendingServiceUpdate) {
      const current = (await deliveryApi.applications.services(applicationId)).find(
        (item) => item.id === draft.serviceId,
      )
      if (
        typeof service.expectedVersion !== 'number' ||
        !current ||
        current.version !== service.expectedVersion + 1 ||
        serviceInputSignature({ ...current }) !== serviceInputSignature(service)
      )
        throw new ApiError(409, '服务更新结果尚未确认，请重新加载服务后合并；不会覆盖其他修改。')
    } else {
      checkpoint.pendingServiceUpdate = true
      try {
        await deliveryApi.applications.updateService(applicationId, draft.serviceId, service)
      } catch (error) {
        if (!uncertainWrite(error)) delete checkpoint.pendingServiceUpdate
        throw error
      }
    }
    delete checkpoint.pendingServiceUpdate
    checkpoint.serviceId = draft.serviceId
    checkpoint.serviceVersion =
      typeof service.expectedVersion === 'number' ? service.expectedVersion + 1 : undefined
  } else if (checkpoint.pendingService) {
    const candidates = (await deliveryApi.applications.services(applicationId)).filter(
      (item) =>
        !checkpoint.pendingService!.includes(item.id) &&
        item.key === service.key &&
        item.name === service.name &&
        (item.buildSourceId || '') === (service.buildSourceId || '') &&
        (item.repositoryId || '') === (service.repositoryId || '') &&
        item.serviceKind === service.serviceKind &&
        deploymentReferenceSignature(item.deploymentTemplate) ===
          deploymentReferenceSignature(service.deploymentTemplate) &&
        item.metadata?.sourceMode ===
          (service.metadata as Record<string, unknown> | undefined)?.sourceMode,
    )
    if (candidates.length !== 1)
      throw new Error('服务创建结果尚未确认，请检查服务列表后再继续；不会重复创建。')
    checkpoint.serviceId = candidates[0].id
    checkpoint.serviceVersion = candidates[0].version
    delete checkpoint.pendingService
  } else {
    const existing = await deliveryApi.applications.services(applicationId)
    if (existing.some((item) => item.key === service.key))
      throw new Error('服务 Key 已存在，请修改后保存。')
    checkpoint.pendingService = existing.map((item) => item.id)
    try {
      const created = await deliveryApi.applications.createService(applicationId, service)
      if (!created?.id) throw new Error('服务创建响应缺少 ID，请重试以核实保存结果。')
      checkpoint.serviceId = created.id
      checkpoint.serviceVersion = created.version
      delete checkpoint.pendingService
    } catch (error) {
      if (!uncertainWrite(error)) delete checkpoint.pendingService
      throw error
    }
  }
  await saveServiceDeployment(draft, checkpoint)
  return checkpoint.serviceId
}
