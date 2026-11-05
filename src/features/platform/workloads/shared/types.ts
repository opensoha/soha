import type { ScopeKey } from '@/types'

export type WorkloadKind =
  | 'deployments'
  | 'pods'
  | 'statefulsets'
  | 'daemonsets'
  | 'replicasets'
  | 'replicationcontrollers'
  | 'jobs'
  | 'cronjobs'

export interface WorkloadReference {
  readonly scope: ScopeKey
  readonly name: string
}

export interface UpdateWorkloadYAMLVariables extends WorkloadReference {
  readonly content: string
}

export interface WorkloadEvent {
  name: string
  namespace?: string
  type: string
  reason: string
  involvedKind?: string
  involvedName?: string
  message: string
  count: number
  ageSeconds: number
  lastTimestamp?: string
}

export interface WorkloadYAMLInput {
  content: string
}
