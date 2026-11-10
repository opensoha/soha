import { parse, stringify } from 'yaml'
import { applicationEnvironmentPayload } from './environment-payload'
import type { ApplicationEnvironment, ApplicationRuntimeWorkload } from '../types'
import type { ManifestPackage, ManifestPackageInput } from '../manifests/types'

export function rootCRConfiguration(content: string, workload: ApplicationRuntimeWorkload) {
  const value = parse(content)
  const source = workload.source!
  if (
    !value ||
    value.apiVersion !== source.resource.apiVersion ||
    value.kind !== source.resource.kind ||
    value.metadata?.uid !== source.resource.uid ||
    value.metadata?.name !== workload.workloadName ||
    value.metadata?.namespace !== workload.namespace ||
    !Number.isSafeInteger(value.metadata?.generation) ||
    value.metadata.generation < 1 ||
    !value.spec ||
    Array.isArray(value.spec) ||
    typeof value.spec !== 'object'
  )
    throw new Error('根 CR 身份或配置无效，请重新读取源配置。')
  const labels = value.metadata.labels ?? {}
  const annotations = value.metadata.annotations ?? {}
  const manager = String(labels['app.kubernetes.io/managed-by'] ?? '').toLowerCase()
  const external =
    Object.keys(annotations).some(
      (key) => key.startsWith('meta.helm.sh/') || key.startsWith('argocd.argoproj.io/'),
    ) ||
    Object.keys(labels).some(
      (key) => key.endsWith('.toolkit.fluxcd.io/name') || key === 'argocd.argoproj.io/instance',
    ) ||
    value.metadata.managedFields?.some(
      (field: { manager?: string }) =>
        field.manager?.startsWith('argocd') ||
        ['kustomize-controller', 'helm-controller'].includes(field.manager ?? ''),
    ) ||
    (manager && manager !== 'soha')
  const blocked = value.metadata.deletionTimestamp
    ? '根 CR 正在删除，等待原控制器处理。'
    : value.metadata.ownerReferences?.length
      ? '该资源有上级拥有者，请更新真正的根配置。'
      : external
        ? '检测到外部发布写入方，请修改 Git、Helm 或原平台的权威来源。'
        : ''
  return {
    generation: value.metadata.generation as number,
    blocked,
    content: stringify({
      apiVersion: value.apiVersion,
      kind: value.kind,
      metadata: { name: value.metadata.name, namespace: value.metadata.namespace },
      spec: value.spec,
    }),
  }
}

export function validateRootCRDraft(content: string, workload: ApplicationRuntimeWorkload) {
  const object = parse(content)
  const source = workload.source!
  if (
    !object ||
    object.apiVersion !== source.resource.apiVersion ||
    object.kind !== source.resource.kind ||
    object.metadata?.name !== workload.workloadName ||
    object.metadata?.namespace !== workload.namespace ||
    Object.keys(object).some((key) => !['apiVersion', 'kind', 'metadata', 'spec'].includes(key)) ||
    Object.keys(object.metadata).some((key) => !['name', 'namespace'].includes(key)) ||
    !object.spec ||
    Array.isArray(object.spec) ||
    typeof object.spec !== 'object'
  )
    throw new Error('仅可更新当前根 CR 的 spec，不能修改身份、状态或运行时元数据。')
  return content
}

export function rootCRPackageInput(
  binding: ApplicationEnvironment,
  workload: ApplicationRuntimeWorkload,
  content: string,
  item?: ManifestPackage,
): ManifestPackageInput {
  validateRootCRDraft(content, workload)
  return {
    applicationId: binding.applicationId,
    serviceId: workload.serviceId || undefined,
    name: item?.name ?? `${workload.workloadName} · ${binding.alias || binding.environmentKey}`,
    description: item?.description ?? '根 CR 配置；发布使用应用交付计划。',
    renderer: 'raw_yaml',
    expectedUpdatedAt: item?.updatedAt,
    files: [{ path: `root-cr-${workload.source!.resource.uid}.yaml`, content }],
    bindings: item?.bindings ?? [
      {
        applicationEnvironmentId: binding.id,
        environmentKey: binding.environmentKey || binding.environmentId,
        clusterId: workload.clusterId,
        namespace: workload.namespace,
      },
    ],
  }
}

export function rootCRControlPayload(
  binding: ApplicationEnvironment,
  workload: ApplicationRuntimeWorkload,
  configRef: string,
  generation: number,
  managed: boolean,
) {
  const payload = applicationEnvironmentPayload(binding)
  const target = payload.targets.find((item) => item.id === workload.targetId)
  if (!target || target.workloadKind !== 'CustomResource' || !workload.source?.crdName)
    throw new Error('根 CR 关联已失效。')
  let source: { resource?: { uid?: string } }
  try {
    source = JSON.parse(String(target.metadata?.resourceSource ?? ''))
  } catch {
    throw new Error('根 CR 身份无效。')
  }
  if (
    source.resource?.uid !== workload.source.resource.uid ||
    !Number.isSafeInteger(generation) ||
    generation < 1 ||
    (managed && !configRef)
  )
    throw new Error('发布控制权必须绑定原 UID 和已审阅的配置基线。')
  return {
    ...payload,
    targets: payload.targets.map((item) =>
      item.id === workload.targetId
        ? {
            ...item,
            executorKind: managed ? 'manifest_ssa' : 'observe_only',
            configRef: managed ? configRef : undefined,
            metadata: {
              ...item.metadata,
              ownershipMode: managed ? 'managed' : 'observe_only',
              rootCRGeneration: generation,
            },
          }
        : item,
    ),
  }
}
