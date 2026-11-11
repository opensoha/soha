import {
  DEFAULT_WORKLOAD_SNAPSHOT_INHERITANCE,
  type JobFormValues,
  type KubernetesManifest,
  type WorkloadFormValues,
} from '../types'
import type { WorkloadSnapshotRequest } from '../../types'
import { appLabels, buildMetadata, buildPodSpec, compactObject, manifest } from './shared'

type ControllerKind = 'Deployment' | 'StatefulSet' | 'DaemonSet'

function lines(value?: string) {
  const items = value
    ?.split(/\r?\n/)
    .map((item) => item.trim())
    .filter(Boolean)
  return items?.length ? items : undefined
}

export function buildControllerManifest(
  kind: ControllerKind,
  values: WorkloadFormValues,
): KubernetesManifest {
  const labels = appLabels(values)
  const podTemplate = {
    metadata: { labels },
    spec: buildPodSpec(values),
  }
  const baseSpec = {
    selector: { matchLabels: { 'app.kubernetes.io/name': labels['app.kubernetes.io/name'] } },
    template: podTemplate,
  }

  if (kind === 'DaemonSet') {
    return manifest('apps/v1', kind, buildMetadata(values), { spec: baseSpec })
  }

  return manifest('apps/v1', kind, buildMetadata(values), {
    spec: compactObject({
      ...baseSpec,
      replicas: values.replicas ?? 1,
      serviceName:
        kind === 'StatefulSet' ? values.serviceName?.trim() || values.name.trim() : undefined,
    }),
  })
}

export function buildJobManifest(
  kind: 'Job' | 'CronJob',
  values: JobFormValues,
): KubernetesManifest {
  const labels = appLabels(values)
  const metadata = buildMetadata(values)
  const description = values.description?.trim()
  const podSpec = buildPodSpec(values)
  const containers = podSpec.containers as Array<Record<string, unknown>>
  const jobSpec = compactObject({
    parallelism: values.parallelism,
    completions: values.completions,
    backoffLimit: values.backoffLimit ?? 6,
    activeDeadlineSeconds: values.activeDeadlineSeconds,
    template: {
      metadata: { labels },
      spec: {
        ...podSpec,
        containers: [
          compactObject({
            ...containers[0],
            command: lines(values.commandText),
            args: lines(values.argsText),
          }),
        ],
        restartPolicy: values.restartPolicy,
      },
    },
  })
  const targetMetadata = description
    ? {
        ...metadata,
        annotations: { ...metadata.annotations, 'soha.io/description': description },
      }
    : metadata

  if (kind === 'CronJob') {
    return manifest('batch/v1', kind, targetMetadata, {
      spec: {
        schedule: values.schedule?.trim() || '0 * * * *',
        suspend: values.suspend ?? false,
        jobTemplate: { spec: jobSpec },
      },
    })
  }

  return manifest('batch/v1', kind, targetMetadata, { spec: jobSpec })
}

export function buildWorkloadSnapshotRequest(
  kind: 'Job' | 'CronJob',
  values: JobFormValues,
): WorkloadSnapshotRequest {
  const metadata = buildMetadata(values)
  const targetKind =
    kind === 'CronJob' && values.imagePolicy === 'follow' ? 'WorkloadCronJob' : kind
  return {
    namespace: values.namespace?.trim() || '',
    sourceKind: values.sourceKind,
    sourceName: values.sourceName?.trim() || '',
    sourceContainer: values.sourceContainer?.trim() || undefined,
    targetKind,
    targetName: values.name.trim(),
    description: values.description?.trim() || undefined,
    labels: metadata.labels,
    annotations: metadata.annotations,
    inherit: [...(values.inherit ?? DEFAULT_WORKLOAD_SNAPSHOT_INHERITANCE)],
    command: lines(values.commandText),
    args: lines(values.argsText),
    restartPolicy: values.restartPolicy,
    parallelism: values.parallelism,
    completions: values.completions,
    backoffLimit: values.backoffLimit,
    activeDeadlineSeconds: values.activeDeadlineSeconds,
    schedule: kind === 'CronJob' ? values.schedule?.trim() : undefined,
    suspend: kind === 'CronJob' ? (values.suspend ?? false) : undefined,
  }
}
