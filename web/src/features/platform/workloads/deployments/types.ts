import type {
  DeploymentDetail,
  DeploymentRolloutStatus,
  ResourceMetrics,
  RolloutHistory,
  ScopeKey,
} from '@/types'

export interface Deployment {
  name: string
  namespace: string
  uid?: string
  labels?: Record<string, string>
  desiredReplicas: number
  readyReplicas: number
  updatedReplicas: number
  available: number
  ageSeconds: number
  allowedActions?: string[]
}

export interface DeploymentTarget {
  readonly scope: ScopeKey
  readonly name: string
  readonly uid?: string
}

export interface ScaleDeploymentVariables extends DeploymentTarget {
  readonly replicas: number
}

export interface RollbackDeploymentVariables extends DeploymentTarget {
  readonly revision?: string
}

export type { DeploymentDetail, DeploymentRolloutStatus, ResourceMetrics, RolloutHistory }
