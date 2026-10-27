import type {
  Pod as PlatformPod,
  PodDetail as PlatformPodDetail,
  ResourceMetrics as PlatformResourceMetrics,
  ScopeKey,
} from '@/types'
import { toScopeKey } from '@/types'

export type Pod = PlatformPod
export type PodContainerStatus = NonNullable<Pod['containerStatuses']>[number]
export type PodDetail = PlatformPodDetail
export type PodMetrics = PlatformResourceMetrics

export interface PodTarget {
  readonly scope: ScopeKey
  readonly name: string
}

export function podTargetFromRecord(
  clusterId: string | null | undefined,
  record: Pick<Pod, 'name' | 'namespace'>,
): PodTarget {
  return {
    scope: toScopeKey(clusterId, record.namespace),
    name: record.name,
  }
}

export interface BatchDeletePodsVariables {
  readonly targets: readonly PodTarget[]
}

export type BatchDeletePodsResult = PromiseSettledResult<void>[]
