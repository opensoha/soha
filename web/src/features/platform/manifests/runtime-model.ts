import type { ManifestPackage, ManifestDeployment } from '@/features/delivery'
export type ManifestRuntimeRow = ManifestPackage['bindings'][number] & {
  manifest: ManifestPackage
  deployment?: ManifestDeployment
  readState: 'loading' | 'error' | 'ready' | 'unknown'
  readError?: string
}

interface ManifestDiffRow {
  key: string
  resource: string
  path: string
  desiredValue: unknown
  observedValue: unknown
}

export function runtimeRowKey(item: ManifestRuntimeRow) {
  return `${item.manifest.id}:${item.id || item.applicationEnvironmentId}`
}

export function manifestDiffRows(deployment?: ManifestDeployment): ManifestDiffRow[] {
  return (deployment?.status.drift?.resources ?? []).flatMap((resource) =>
    resource.fields.map((field) => ({
      key: `${resource.apiVersion}:${resource.kind}:${resource.namespace}:${resource.name}:${field.path}`,
      resource: `${resource.kind}/${resource.name}`,
      path: field.path,
      desiredValue: field.desiredValue,
      observedValue: field.observedValue,
    })),
  )
}

export function formatManifestValue(value: unknown) {
  if (typeof value === 'string') return value
  return JSON.stringify(value) ?? '-'
}
