import { queryOptions } from '@tanstack/react-query'
import type { ScopeKey } from '@/types'
import { workloadKeys } from '@/features/platform/workloads/shared/keys'
import {
  hasNamespacedWorkloadScope,
  hasWorkloadCluster,
} from '@/features/platform/workloads/shared/scope'
import { getCronJobDetail, getCronJobConfiguration, listCronJobEvents, listCronJobs } from './api'
import type { CronJob, CronJobDetail } from './types'
import { cronJobKeys } from './keys'

function target(scope: ScopeKey, name: string) {
  return { scope, name }
}

function hasCronJobReference(scope: ScopeKey, name: string) {
  return hasNamespacedWorkloadScope(scope) && Boolean(name.trim())
}

export const cronJobQueries = {
  configuration: (scope: ScopeKey, name: string) =>
    queryOptions({
      queryKey: cronJobKeys.configuration(scope, name),
      queryFn: () => getCronJobConfiguration(target(scope, name)),
      enabled: hasCronJobReference(scope, name),
    }),
  list: (scope: ScopeKey) =>
    queryOptions<CronJob[]>({
      queryKey: workloadKeys.list('cronjobs', scope),
      queryFn: () => listCronJobs(scope),
      enabled: hasWorkloadCluster(scope),
    }),
  detail: (scope: ScopeKey, name: string) =>
    queryOptions<CronJobDetail>({
      queryKey: workloadKeys.detail('cronjobs', scope, name),
      queryFn: () => getCronJobDetail(target(scope, name)),
      enabled: hasCronJobReference(scope, name),
    }),
  events: (scope: ScopeKey, name: string) =>
    queryOptions({
      queryKey: workloadKeys.events('cronjobs', scope, name),
      queryFn: () => listCronJobEvents(target(scope, name)),
      enabled: hasCronJobReference(scope, name),
    }),
}
