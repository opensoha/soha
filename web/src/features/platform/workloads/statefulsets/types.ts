import type { Pod, ResourceMetrics, ScopeKey, WorkloadRelation } from '@/types'

export interface StatefulSet {
  name: string
  namespace: string
  serviceName?: string
  desiredReplicas: number
  readyReplicas: number
  currentReplicas: number
  ageSeconds: number
  allowedActions?: string[]
}

export interface StatefulSetDetail {
  allowedActions?: string[]
  name: string
  namespace: string
  labels?: Record<string, string>
  annotations?: Record<string, string>
  serviceName?: string
  desiredReplicas?: number
  readyReplicas?: number
  currentReplicas?: number
  updateStrategy?: string
  currentRevision?: string
  updateRevision?: string
  createdAt: string
  selector?: Record<string, string>
  pods?: Pod[]
  relatedResources?: WorkloadRelation[]
}

export interface StatefulSetTarget {
  readonly scope: ScopeKey
  readonly name: string
}

export interface ScaleStatefulSetVariables extends StatefulSetTarget {
  readonly replicas: number
}

export type { Pod, ResourceMetrics, WorkloadRelation }
