import type { Pod, WorkloadRelation } from '@/types'

export interface ReplicaSet {
  ageSeconds: number
  allowedActions?: string[]
  availableReplicas: number
  desiredReplicas: number
  name: string
  namespace: string
  readyReplicas: number
}

export interface ReplicaSetDetail {
  allowedActions?: string[]
  annotations?: Record<string, string>
  availableReplicas: number
  createdAt: string
  desiredReplicas: number
  labels?: Record<string, string>
  name: string
  namespace: string
  pods?: Pod[]
  readyReplicas: number
  relatedResources?: WorkloadRelation[]
  selector?: Record<string, string>
}
