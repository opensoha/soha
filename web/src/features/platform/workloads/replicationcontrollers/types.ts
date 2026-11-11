import type { Pod, WorkloadRelation } from '@/types'

export interface ReplicationController {
  ageSeconds: number
  allowedActions?: string[]
  availableReplicas: number
  currentReplicas: number
  desiredReplicas: number
  name: string
  namespace: string
  readyReplicas: number
}

export interface ReplicationControllerDetail {
  allowedActions?: string[]
  annotations?: Record<string, string>
  availableReplicas: number
  createdAt: string
  currentReplicas: number
  desiredReplicas: number
  labels?: Record<string, string>
  name: string
  namespace: string
  pods?: Pod[]
  readyReplicas: number
  relatedResources?: WorkloadRelation[]
  selector?: Record<string, string>
}
