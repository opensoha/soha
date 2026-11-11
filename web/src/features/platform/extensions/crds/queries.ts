import { queryOptions } from '@tanstack/react-query'
import {
  getCustomResourceAccess,
  getCustomResourceYAML,
  listCRDs,
  listCustomResources,
} from './api'
import { crdKeys } from './keys'
import type { CRD, CustomResourceTarget } from './types'

export const crdQueries = {
  catalog: (clusterId?: string | null) =>
    queryOptions({
      queryKey: crdKeys.catalog(clusterId),
      queryFn: () => listCRDs(clusterId!),
      enabled: Boolean(clusterId),
      refetchInterval: (query) =>
        query.state.status === 'error'
          ? false
          : query.state.data?.some((item) => item.deletingAt)
            ? 3000
            : false,
    }),
  resources: (
    clusterId: string | null | undefined,
    crd: CRD,
    namespace: string | null | undefined,
    capabilityEnabled: boolean,
  ) =>
    queryOptions({
      queryKey: crdKeys.resources(clusterId ?? '', crd, namespace),
      queryFn: () => listCustomResources(clusterId!, crd, namespace),
      enabled: Boolean(clusterId) && capabilityEnabled,
      refetchInterval: (query) =>
        query.state.status === 'error'
          ? false
          : query.state.data?.some((item) => item.deletingAt)
            ? 3000
            : false,
    }),
  access: (
    clusterId: string | null | undefined,
    crd: CRD,
    namespace: string | null | undefined,
    capabilityEnabled: boolean,
  ) =>
    queryOptions({
      queryKey: crdKeys.access(clusterId ?? '', crd, namespace),
      queryFn: () => getCustomResourceAccess(clusterId!, crd, namespace),
      enabled: Boolean(clusterId) && capabilityEnabled,
    }),
  yaml: (target: CustomResourceTarget | null, enabled: boolean) =>
    queryOptions({
      queryKey: target ? crdKeys.yaml(target) : [...crdKeys.all, 'yaml', 'disabled'],
      queryFn: () => getCustomResourceYAML(target!),
      enabled: enabled && Boolean(target),
    }),
}
