import type { Pod, ResourceMetrics, ScopeKey, WorkloadRelation } from '@/types'

export interface DaemonSet {
  name: string
  namespace: string
  desiredNumber: number
  currentNumber: number
  readyNumber: number
  availableNumber: number
  updatedNumber: number
  ageSeconds: number
  allowedActions?: string[]
}

export interface DaemonSetDetail {
  name: string
  namespace: string
  labels?: Record<string, string>
  annotations?: Record<string, string>
  desiredNumber?: number
  currentNumber?: number
  readyNumber?: number
  availableNumber?: number
  updatedNumber?: number
  updateStrategy?: string
  createdAt: string
  selector?: Record<string, string>
  pods?: Pod[]
  relatedResources?: WorkloadRelation[]
}

export interface DaemonSetTarget {
  readonly scope: ScopeKey
  readonly name: string
}

export type { Pod, ResourceMetrics, WorkloadRelation }
