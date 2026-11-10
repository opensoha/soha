import type {
  ApplicationRuntimeWorkload,
  ApplicationServiceComponent,
  DeliveryApplicationBindingSummary,
  ReleaseTarget,
  WorkflowRun,
} from '../types'

export function serviceDeliveryKind(
  service: ApplicationServiceComponent,
  targets: Pick<
    ReleaseTarget,
    'metadata' | 'executorKind' | 'targetKind' | 'workloadKind' | 'helm'
  >[],
  workloads: ApplicationRuntimeWorkload[],
): string | undefined {
  const related = targets.filter((target) => target.metadata?.serviceId === service.id)
  if (
    related.some((target) => target.workloadKind === 'CustomResource') ||
    workloads.some(
      (workload) => workload.source?.crdName || workload.workloadKind === 'CustomResource',
    )
  )
    return 'Operator CR'
  if (
    service.serviceKind === 'helm_release' ||
    related.some((target) => target.helm || target.workloadKind === 'HelmRelease')
  )
    return 'Helm Release'
  if (related.some((target) => target.targetKind === 'kustomize_overlay')) return 'Kustomize'
  if (related.some((target) => target.executorKind === 'manifest_ssa')) return '托管 YAML'
  return undefined
}

export function serviceWorkflow(
  service: ApplicationServiceComponent | undefined,
  environmentId: string,
  binding: DeliveryApplicationBindingSummary | undefined,
  workloads: ApplicationRuntimeWorkload[],
  runs: WorkflowRun[] = [],
): WorkflowRun | undefined {
  if (!service || !environmentId) return undefined
  const candidates = [
    ...runs,
    binding?.latestWorkflow,
    ...workloads.map((item) => item.latestWorkflow),
  ]
  return candidates
    .filter((run): run is WorkflowRun => {
      if (!run || run.applicationId !== service.applicationId || run.scope === 'delivery_batch')
        return false
      const metadata = run.metadata ?? {}
      const scope = metadata.bindingId || metadata.applicationEnvironmentId
      if (
        scope !== environmentId ||
        (metadata.bindingId && metadata.bindingId !== environmentId) ||
        (metadata.applicationEnvironmentId && metadata.applicationEnvironmentId !== environmentId)
      )
        return false
      if (metadata.serviceId && metadata.serviceId !== service.id) return false
      const target = binding?.targets?.find((item) => item.id === metadata.targetId)
      if (metadata.targetId && target?.metadata?.serviceId !== service.id) return false
      const matchesWorkload = workloads.some(
        (item) =>
          item.applicationEnvironmentId === environmentId &&
          item.clusterId === run.clusterId &&
          item.namespace === run.namespace &&
          item.workloadName === run.deploymentName,
      )
      return Boolean(
        (scope === environmentId && (metadata.serviceId === service.id || target)) ||
        matchesWorkload,
      )
    })
    .sort((left, right) => right.createdAt.localeCompare(left.createdAt))[0]
}

export function serviceReadiness(workloads: ApplicationRuntimeWorkload[]) {
  if (
    !workloads.length ||
    workloads.some(
      (item) =>
        !['deployment', 'statefulset', 'daemonset'].includes(item.workloadKind.toLowerCase()) ||
        item.healthStatus === 'unknown' ||
        (item.sourceStatus && !['linked', 'changed'].includes(item.sourceStatus)) ||
        !Number.isFinite(item.desiredReplicas) ||
        !Number.isFinite(item.readyReplicas) ||
        item.desiredReplicas < 0 ||
        item.readyReplicas < 0,
    )
  )
    return undefined
  return workloads.reduce(
    (sum, item) => ({
      ready: sum.ready + item.readyReplicas,
      desired: sum.desired + item.desiredReplicas,
    }),
    { ready: 0, desired: 0 },
  )
}
