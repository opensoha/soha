import type { CRD, CustomResourceTarget } from './types'
import { isNamespacedCRD } from './utils'

function resourceScope(crd: CRD, namespace?: string | null) {
  return isNamespacedCRD(crd) ? namespace?.trim() || '__all__' : '__cluster__'
}

export const crdKeys = {
  all: ['platform', 'extensions', 'crds'] as const,
  apply: () => [...crdKeys.all, 'apply'] as const,
  catalogs: () => [...crdKeys.all, 'catalog'] as const,
  catalog: (clusterId?: string | null) => [...crdKeys.catalogs(), clusterId ?? null] as const,
  resources: (clusterId: string, crd: CRD, namespace?: string | null) =>
    [
      ...crdKeys.all,
      'resources',
      clusterId,
      crd.name,
      crd.version,
      resourceScope(crd, namespace),
    ] as const,
  access: (clusterId: string, crd: CRD, namespace?: string | null) =>
    [...crdKeys.resources(clusterId, crd, namespace), 'access'] as const,
  yaml: (target: CustomResourceTarget) =>
    [
      ...crdKeys.resources(target.clusterId, target.crd, target.namespace),
      target.resourceName,
      'yaml',
    ] as const,
}
