import type { ComputeProviderInstanceFilters, ComputeTaskFilters } from './api'
import type {
  ComputeDomain,
  ComputeProviderDomain,
  ComputeResourceKind,
  ComputeTaskDomain,
} from '@opensoha/contracts/gen/ts/sohaapi'

function clean<T extends object>(value: T) {
  return Object.fromEntries(
    Object.entries(value).filter(([, item]) => item !== undefined && item !== ''),
  ) as Partial<T>
}

export const computeKeys = {
  all: ['compute'] as const,
  overview: () => [...computeKeys.all, 'overview'] as const,
  providers: () => [...computeKeys.all, 'providers'] as const,
  providerInstances: (filters: ComputeProviderInstanceFilters = {}) =>
    [...computeKeys.providers(), 'instances', clean(filters)] as const,
  providerInstance: (domain: ComputeProviderDomain, providerKey: string, instanceRef: string) =>
    [
      ...computeKeys.providers(),
      'instances',
      domain,
      providerKey.trim(),
      instanceRef.trim(),
    ] as const,
  resources: () => [...computeKeys.all, 'resources'] as const,
  resourceRelations: (domain: ComputeDomain, kind: ComputeResourceKind, id: string) =>
    [...computeKeys.resources(), domain, kind, id.trim(), 'relations', 'pages'] as const,
  tasks: (filters: ComputeTaskFilters = {}) =>
    [...computeKeys.all, 'tasks', clean(filters)] as const,
  task: (domain: ComputeTaskDomain, taskId: string) =>
    [...computeKeys.all, 'tasks', domain, taskId] as const,
  taskLogs: (domain: ComputeTaskDomain, taskId: string) =>
    [...computeKeys.task(domain, taskId), 'logs'] as const,
}

export const computeMutationKeys = {
  task: (action: 'cancel' | 'retry') => [...computeKeys.all, 'mutation', 'task', action] as const,
  provider: (action: 'health' | 'discover') =>
    [...computeKeys.all, 'mutation', 'provider', action] as const,
  resourceAction: (action: string) =>
    [...computeKeys.all, 'mutation', 'resource', action.trim()] as const,
}
