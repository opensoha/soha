import type { DeliveryTargetCandidate, ReleaseTarget } from './domain-types'
import type { ManifestPackage } from './manifests/types'

export const RELEASE_TARGET_KIND_OPTIONS = [
  { value: 'k8s_workload', label: 'YAML / Kubernetes workload' },
  { value: 'helm_release', label: 'Helm release' },
  { value: 'kustomize_overlay', label: 'Kustomize overlay' },
  { value: 'host_service', label: 'Host service' },
] as const

type ReleaseTargetInput = Omit<ReleaseTarget, 'id'> & { id?: string }

type ReleaseTargetIdentity = Pick<
  ReleaseTarget,
  'clusterId' | 'namespace' | 'workloadKind' | 'workloadName'
> &
  Partial<Pick<ReleaseTarget, 'targetKind' | 'helm' | 'docker'>>

export function releaseTargetKey(target: ReleaseTargetIdentity) {
  if (target.docker)
    return JSON.stringify(['docker_compose', target.docker.hostId, target.docker.projectId])
  return JSON.stringify([
    target.clusterId,
    target.namespace,
    target.targetKind || 'k8s_workload',
    target.helm ? 'HelmRelease' : target.workloadKind,
    target.helm?.releaseName || target.workloadName,
  ])
}

export function manifestReleaseTargets(
  packages: ManifestPackage[],
  environmentId: string,
  clusterId?: string,
  namespace?: string,
): ReleaseTargetInput[] {
  return packages.flatMap((item) =>
    item.bindings.flatMap((binding) =>
      binding.id &&
      binding.applicationEnvironmentId === environmentId &&
      binding.clusterId === clusterId &&
      binding.namespace === namespace
        ? [
            {
              clusterId: binding.clusterId,
              namespace: binding.namespace,
              targetKind: item.renderer === 'kustomize' ? 'kustomize_overlay' : 'k8s_workload',
              executorKind: 'manifest_ssa',
              configRef: binding.id,
              workloadKind: 'ManifestPackage',
              workloadName: item.id,
              metadata: {
                manifestPackageName: item.name,
                ...(item.serviceId ? { serviceId: item.serviceId } : {}),
              },
              enabled: true,
            },
          ]
        : [],
    ),
  )
}

export function releaseTargetsFromCandidates(
  candidates: DeliveryTargetCandidate[],
  selectedKeys: string[] = [],
  existing: ReleaseTarget[] = [],
  manifests: ReleaseTargetInput[] = [],
): ReleaseTargetInput[] {
  const targets = new Map<string, ReleaseTargetInput>(
    candidates.map((candidate) => [
      releaseTargetKey(candidate),
      {
        clusterId: candidate.clusterId,
        namespace: candidate.namespace,
        targetKind: 'k8s_workload',
        executorKind: 'k8s_job_runner',
        workloadKind: candidate.workloadKind,
        workloadName: candidate.workloadName,
        metadata: {},
        enabled: true,
      },
    ]),
  )
  manifests.forEach((target) => targets.set(releaseTargetKey(target), target))
  existing.forEach((target) => targets.set(releaseTargetKey(target), target))
  return selectedKeys.flatMap((key) => {
    const target = targets.get(key)
    return target ? [target] : []
  })
}

function requiredText(value: unknown, label: string, index: number) {
  const text = String(value ?? '').trim()
  if (!text) throw new Error(`发布目标 ${index + 1} 缺少 ${label}`)
  return text
}

export function parseReleaseTargets(raw: unknown, field = '发布目标'): ReleaseTargetInput[] {
  const text = typeof raw === 'string' ? raw.trim() : ''
  if (!text) return []
  let value: unknown
  try {
    value = JSON.parse(text)
  } catch {
    throw new Error(`${field} 需要是合法 JSON 数组`)
  }
  if (!Array.isArray(value)) throw new Error(`${field} 需要是合法 JSON 数组`)

  return value.map((item, index) => {
    if (!item || typeof item !== 'object' || Array.isArray(item)) {
      throw new Error(`发布目标 ${index + 1} 需要是 JSON 对象`)
    }
    const target = item as Record<string, unknown>
    const targetKind = String(target.targetKind || 'k8s_workload')
    if (!RELEASE_TARGET_KIND_OPTIONS.some((option) => option.value === targetKind)) {
      throw new Error(`发布目标 ${index + 1} 的 targetKind 不受支持`)
    }
    const metadata =
      target.metadata && typeof target.metadata === 'object' && !Array.isArray(target.metadata)
        ? (target.metadata as Record<string, unknown>)
        : {}
    if (target.executorKind === 'docker_compose') {
      if (targetKind !== 'host_service' || target.clusterId || target.namespace || target.helm)
        throw new Error(`发布目标 ${index + 1} 的 Docker 配置不能包含 Kubernetes 或 Helm 目标`)
      const docker = target.docker as Record<string, unknown> | undefined
      if (!docker || typeof docker !== 'object' || Array.isArray(docker))
        throw new Error(`发布目标 ${index + 1} 缺少 docker 配置`)
      const mappings = docker.imageMappings
      if (
        !mappings ||
        typeof mappings !== 'object' ||
        Array.isArray(mappings) ||
        !Object.keys(mappings).length ||
        Object.entries(mappings).some(
          ([key, value]) => !key.trim() || typeof value !== 'string' || !value.trim(),
        )
      )
        throw new Error(`发布目标 ${index + 1} 需要 Compose 服务到产物容器的镜像映射`)
      return {
        ...target,
        targetKind,
        executorKind: 'docker_compose',
        clusterId: '',
        namespace: '',
        workloadKind: 'ComposeProject',
        workloadName: requiredText(target.workloadName, 'workloadName', index),
        docker: {
          hostId: requiredText(docker.hostId, 'docker.hostId', index),
          projectId: requiredText(docker.projectId, 'docker.projectId', index),
          imageMappings: mappings,
        },
        metadata,
        enabled: target.enabled !== false,
      } as ReleaseTargetInput
    }
    if (targetKind === 'helm_release') {
      if (target.helm && typeof target.helm === 'object' && !Array.isArray(target.helm)) {
        const helm = target.helm as Record<string, unknown>
        requiredText(helm.releaseName, 'helm.releaseName', index)
        if (!helm.source || typeof helm.source !== 'object' || Array.isArray(helm.source))
          throw new Error(`发布目标 ${index + 1} 缺少 helm.source`)
        for (const key of ['repositoryUrl', 'chart', 'version'])
          requiredText((helm.source as Record<string, unknown>)[key], `helm.source.${key}`, index)
      } else requiredText(metadata.chartRef, 'metadata.chartRef', index)
    }
    if (target.executorKind === 'manifest_ssa') {
      requiredText(target.configRef, 'configRef（Manifest 环境绑定）', index)
    } else if (targetKind === 'kustomize_overlay') {
      requiredText(
        metadata.overlayPath || target.configRef,
        'metadata.overlayPath 或 configRef',
        index,
      )
    }
    return {
      ...target,
      clusterId: requiredText(target.clusterId, 'clusterId', index),
      namespace: requiredText(target.namespace, 'namespace', index),
      targetKind,
      executorKind: String(target.executorKind || 'k8s_job_runner'),
      workloadKind: requiredText(target.workloadKind, 'workloadKind', index),
      workloadName: requiredText(target.workloadName, 'workloadName', index),
      metadata,
      enabled: target.enabled !== false,
    } as ReleaseTargetInput
  })
}

export function summarizeReleaseTargets(targets: ReleaseTarget[] = []) {
  if (!targets.length) return '-'
  const counts = new Map<string, number>()
  targets.forEach((target) => {
    const kind = target.targetKind || 'k8s_workload'
    counts.set(kind, (counts.get(kind) ?? 0) + 1)
  })
  return Array.from(counts, ([kind, count]) => `${kind} ${count}`).join(' · ')
}
