import type { MonitoringQuery } from './types'

export const monitoringKeys = {
  all: ['platform', 'resource-monitoring'] as const,
  view: (
    principal: string,
    permissionSignature: string,
    clusterId: string,
    input: MonitoringQuery,
  ) => [...monitoringKeys.all, principal, permissionSignature, { clusterId, ...input }] as const,
}
