import type { JobExecution } from '../shared/job-execution'
import type { ScopeKey, WorkloadRelation } from '@/types'

export interface CronJob {
  name: string
  namespace: string
  schedule: string
  suspend: boolean
  activeJobs: number
  lastScheduleTime?: string
  ageSeconds: number
  allowedActions?: string[]
}

export interface CronJobDetail extends CronJob {
  concurrencyPolicy?: string
  timeZone?: string
  createdAt?: string
  labels?: Record<string, string>
  annotations?: Record<string, string>
  jobs?: CronJobChildJob[]
  relatedResources?: WorkloadRelation[]
}

export interface CronJobTarget {
  readonly scope: ScopeKey
  readonly name: string
}

export interface SuspendCronJobVariables extends CronJobTarget {
  readonly suspend: boolean
}

export interface CronJobChildJob extends JobExecution {
  name: string
  namespace: string
  succeeded: number
  failed: number
  active: number
  ageSeconds: number
}
