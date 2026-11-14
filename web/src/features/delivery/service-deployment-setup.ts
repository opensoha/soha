import { ApiError, isApiError } from '@/services/api-error'
import { deliveryApi } from './api'
import { manifestApi } from './manifests/api'
import { manifestReleaseTargets } from './release-targets'
import type { ManifestPackage, ManifestPackageInput } from './manifests/types'
import type { ServiceSetupCheckpoint, ServiceSetupDraft } from './service-setup'
import type {
  DockerDeliveryConfiguration,
  HelmDeliveryConfiguration,
} from '@opensoha/contracts/gen/ts/sohaapi'
import type {
  ApplicationEnvironment,
  ServiceDeploymentTemplate,
  TemplateParameterValues,
  ServiceDeploymentTemplateBinding,
  ReleaseTarget,
} from './types'

export type ServiceDeploymentSetup = {
  template?: ServiceDeploymentTemplate
  helm?: boolean
  docker?: boolean
  environments: Array<{
    environment: ApplicationEnvironment
    parameters: TemplateParameterValues
    helm?: HelmDeliveryConfiguration
    docker?: DockerDeliveryConfiguration
  }>
  originalPackage?: ManifestPackage
  originalEnvironments?: ApplicationEnvironment[]
}

export function deploymentReferenceSignature(reference: unknown) {
  return JSON.stringify(reference, (key, value) =>
    key === 'detachedTemplate'
      ? undefined
      : value && typeof value === 'object' && !Array.isArray(value)
        ? Object.fromEntries(Object.entries(value).sort(([a], [b]) => a.localeCompare(b)))
        : value,
  )
}

export function deploymentConfigurationSignature(item: ManifestPackageInput) {
  const { name, applicationId, serviceId, renderer, files, bindings } = item
  return JSON.stringify(
    {
      name,
      applicationId,
      serviceId,
      renderer,
      files,
      bindings: bindings.map(
        ({
          applicationEnvironmentId,
          clusterId,
          namespace,
          overlay = {},
          kustomize,
          templateParameters = {},
        }) => ({
          applicationEnvironmentId,
          clusterId,
          namespace,
          overlay,
          kustomize,
          templateParameters,
        }),
      ),
    },
    (_key, value) =>
      value && typeof value === 'object' && !Array.isArray(value)
        ? Object.fromEntries(Object.entries(value).sort(([a], [b]) => a.localeCompare(b)))
        : value,
  )
}

export async function saveServiceDeployment(
  draft: ServiceSetupDraft,
  checkpoint: ServiceSetupCheckpoint,
) {
  if (!draft.deployment || checkpoint.deploymentComplete) return
  const { template, environments, originalPackage } = draft.deployment
  const serviceId = checkpoint.serviceId!
  if (draft.deployment.docker || draft.deployment.helm || template?.source.renderer === 'helm') {
    await saveServiceReleaseTargets(draft, checkpoint)
    checkpoint.deploymentComplete = true
    return
  }
  if (!template || template.source.git || !template.source.files?.length)
    throw new Error('此部署来源尚未接入服务配置保存。')
  const input: ManifestPackageInput = {
    name: originalPackage?.name || `${String(draft.service.name)} 部署配置`,
    description: originalPackage?.description,
    applicationId: draft.applicationId,
    serviceId,
    renderer: template.source.renderer,
    files: template.source.files,
    expectedUpdatedAt: originalPackage?.updatedAt,
    bindings: environments.map(({ environment, parameters }) => ({
      ...originalPackage?.bindings.find(
        (binding) => binding.applicationEnvironmentId === environment.id,
      ),
      applicationEnvironmentId: environment.id,
      environmentKey: environment.environmentKey || environment.environmentId,
      clusterId: environment.clusterId!,
      namespace: environment.namespace!,
      kustomize: template.source.kustomize,
      templateParameters: parameters,
    })),
  }
  if (!checkpoint.manifestPackage) {
    if (originalPackage) {
      const current = await manifestApi.get(originalPackage.id)
      if (deploymentConfigurationSignature(current) === deploymentConfigurationSignature(input))
        checkpoint.manifestPackage = current
      else checkpoint.manifestPackage = await manifestApi.update(originalPackage.id, input)
    } else if (checkpoint.pendingManifestPackage) {
      const matches = (await manifestApi.applicationPackages(draft.applicationId)).filter(
        (item) =>
          !checkpoint.pendingManifestPackage!.includes(item.id) &&
          deploymentConfigurationSignature(item) === deploymentConfigurationSignature(input),
      )
      if (matches.length !== 1)
        throw new Error('部署配置创建结果尚未确认，请检查清单库后继续；不会重复创建。')
      checkpoint.manifestPackage = matches[0]
      delete checkpoint.pendingManifestPackage
    } else {
      checkpoint.pendingManifestPackage = (
        await manifestApi.applicationPackages(draft.applicationId)
      ).map((item) => item.id)
      try {
        const created = await manifestApi.create(input)
        if (!created?.id) throw new Error('部署配置创建响应缺少 ID，请重试核实结果。')
        checkpoint.manifestPackage = created
        delete checkpoint.pendingManifestPackage
      } catch (error) {
        if (isApiError(error) && error.status > 0 && error.status < 500)
          delete checkpoint.pendingManifestPackage
        throw error
      }
    }
  }
  if (
    checkpoint.manifestPackage.status !== 'published' ||
    checkpoint.manifestPackage.currentRevision < 1
  ) {
    const currentPackage = await manifestApi.get(checkpoint.manifestPackage.id)
    if (
      deploymentConfigurationSignature(currentPackage) !== deploymentConfigurationSignature(input)
    )
      throw new ApiError(409, '部署配置已被修改，请重新加载后合并。')
    checkpoint.manifestPackage =
      currentPackage.status === 'published' && currentPackage.currentRevision > 0
        ? currentPackage
        : await manifestApi.saveRevision(currentPackage.id, currentPackage.updatedAt)
  }
  const current = (await deliveryApi.applications.services(draft.applicationId)).find(
    (item) => item.id === serviceId,
  )
  if (!current) throw new Error('服务已不存在，请重新加载。')
  const reference = draft.service.deploymentTemplate as ServiceDeploymentTemplateBinding
  const desired = { ...reference, manifestPackageId: checkpoint.manifestPackage.id }
  if (
    deploymentReferenceSignature(current.deploymentTemplate) !==
    deploymentReferenceSignature(desired)
  ) {
    if (!checkpoint.serviceVersion || current.version !== checkpoint.serviceVersion)
      throw new ApiError(409, '服务已被修改，请重新加载后合并；部署配置已保留。')
    await deliveryApi.applications.updateService(draft.applicationId, serviceId, {
      ...current,
      deploymentTemplate: desired,
      expectedVersion: current.version,
    })
    checkpoint.serviceVersion = current.version + 1
  }
  await saveServiceReleaseTargets(draft, checkpoint)
  checkpoint.deploymentComplete = true
}

async function saveServiceReleaseTargets(
  draft: ServiceSetupDraft,
  checkpoint: ServiceSetupCheckpoint,
) {
  const { environments, originalEnvironments = [], helm, docker, template } = draft.deployment!
  const isHelm = helm || template?.source.renderer === 'helm'
  const serviceId = checkpoint.serviceId!
  const owns = (target: ReleaseTarget) =>
    target.executorKind === (docker ? 'docker_compose' : isHelm ? 'helm_sdk' : 'manifest_ssa') &&
    target.metadata?.serviceId === serviceId
  const affected = new Map(originalEnvironments.map((environment) => [environment.id, environment]))
  environments.forEach(({ environment }) => affected.set(environment.id, environment))
  const currentEnvironments = await deliveryApi.environments.list()
  for (const [id, original] of affected) {
    const selected = environments.find((item) => item.environment.id === id)
    const current = currentEnvironments.find(
      (item) => item.id === id && item.applicationId === draft.applicationId,
    )
    if (!current) throw new Error('所选环境已不存在，请重新加载。')
    const ownTargets = (current.targets ?? []).filter(owns)
    if (ownTargets.length > 1)
      throw new Error('此服务在同一环境有多个发布目标，请在环境配置中分别管理。')
    let target: ReleaseTarget | undefined = selected?.helm
      ? {
          ...ownTargets[0],
          id: ownTargets[0]?.id || '',
          clusterId: original.clusterId!,
          namespace: original.namespace!,
          targetKind: 'helm_release',
          executorKind: 'helm_sdk',
          workloadKind: 'HelmRelease',
          workloadName: selected.helm.releaseName,
          helm: selected.helm,
          enabled: true,
          metadata: {
            ...ownTargets[0]?.metadata,
            serviceId,
            templateParameters: selected.parameters,
          },
        }
      : undefined
    if (selected?.docker)
      target = {
        ...ownTargets[0],
        id: ownTargets[0]?.id || '',
        clusterId: '',
        namespace: '',
        targetKind: 'host_service',
        executorKind: 'docker_compose',
        workloadKind: 'ComposeProject',
        workloadName: selected.docker.projectId,
        docker: selected.docker,
        enabled: true,
        metadata: { ...ownTargets[0]?.metadata, serviceId },
      }
    if (selected && !isHelm && !docker && checkpoint.manifestPackage) {
      const manifest = manifestReleaseTargets(
        [checkpoint.manifestPackage],
        id,
        original.clusterId,
        original.namespace,
      )
      if (manifest.length === 1)
        target = {
          ...ownTargets[0],
          ...manifest[0],
          id: ownTargets[0]?.id || '',
          enabled: ownTargets[0]?.enabled ?? true,
          metadata: { ...ownTargets[0]?.metadata, ...manifest[0].metadata },
        }
    }
    if (selected && !target) throw new Error('发布配置不完整，请返回编辑。')
    if (
      deploymentReferenceSignature(ownTargets) ===
      deploymentReferenceSignature(target ? [target] : [])
    )
      continue
    if (current.updatedAt !== original.updatedAt)
      throw new ApiError(409, '环境配置已被修改，请重新加载后合并；已保存的服务和其他环境已保留。')
    await deliveryApi.environments.update(id, {
      ...current,
      expectedUpdatedAt: original.updatedAt,
      targets: [
        ...(current.targets ?? []).filter((item) => !owns(item)),
        ...(target ? [target] : []),
      ],
    })
  }
}
