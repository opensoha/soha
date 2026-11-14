import { mutationOptions, type QueryClient } from '@tanstack/react-query'
import { executeResourceCreate, preflightResourceCreate } from './api'
import { resourceCreationKeys } from './keys'
import type { ResourceCreateRequest, ResourceRef } from './types'

interface ResourceCreateVariables {
  readonly clusterId: string
  readonly request: ResourceCreateRequest
}

interface ResourceExecuteVariables extends ResourceCreateVariables {
  readonly idempotencyKey: string
}

const resourcePrefixes: Record<string, readonly unknown[]> = {
  configmap: ['platform', 'configuration', 'configmaps'],
  secret: ['platform', 'configuration', 'secrets'],
  persistentvolumeclaim: ['platform', 'storage', 'persistentvolumeclaims'],
  persistentvolume: ['platform', 'storage', 'persistentvolumes'],
  storageclass: ['platform', 'storage', 'storageclasses'],
  serviceaccount: ['platform', 'access-control', 'serviceaccounts'],
  role: ['platform', 'access-control', 'roles'],
  rolebinding: ['platform', 'access-control', 'rolebindings'],
  clusterrole: ['platform', 'access-control', 'clusterroles'],
  clusterrolebinding: ['platform', 'access-control', 'clusterrolebindings'],
  deployment: ['platform', 'workloads', 'deployments'],
  statefulset: ['platform', 'workloads', 'statefulsets'],
  daemonset: ['platform', 'workloads', 'daemonsets'],
  job: ['platform', 'workloads', 'jobs'],
  cronjob: ['platform', 'workloads', 'cronjobs'],
  service: ['platform', 'network', 'services'],
  ingress: ['platform', 'network', 'ingresses'],
  namespace: ['platform', 'cluster-resources', 'namespaces'],
}

export async function invalidateCreatedResourceQueries(
  queryClient: QueryClient,
  refs: ResourceRef[],
) {
  const prefixes = new Map<string, readonly unknown[]>()
  refs.forEach((ref) => {
    const prefix = resourcePrefixes[ref.kind.toLowerCase()]
    if (prefix) prefixes.set(JSON.stringify(prefix), prefix)
  })
  await Promise.all(
    [...prefixes.values()].map((queryKey) => queryClient.invalidateQueries({ queryKey })),
  )
}

export const resourceCreationMutations = {
  preflight: () =>
    mutationOptions({
      mutationKey: [...resourceCreationKeys.preflights(), 'run'] as const,
      mutationFn: ({ clusterId, request }: ResourceCreateVariables) =>
        preflightResourceCreate(clusterId, request),
    }),
  execute: (queryClient: QueryClient) =>
    mutationOptions({
      mutationKey: resourceCreationKeys.execute(),
      mutationFn: ({ clusterId, request, idempotencyKey }: ResourceExecuteVariables) =>
        executeResourceCreate(clusterId, request, idempotencyKey),
      onSuccess: async (result) => {
        await invalidateCreatedResourceQueries(
          queryClient,
          result.items.flatMap((item) => (item.resourceRef ? [item.resourceRef] : [])),
        )
      },
    }),
}
