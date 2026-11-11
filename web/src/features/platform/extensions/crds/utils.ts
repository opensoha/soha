import type { CRD, CRDApiGroupSummary } from './types'

export function isNamespacedCRD(crd: CRD | null | undefined) {
  return (crd?.scope ?? '').toLowerCase() === 'namespaced'
}

export function getServedVersions(crd: CRD) {
  return Array.from(new Set((crd.versions?.length ? crd.versions : [crd.version]).filter(Boolean)))
}

export function groupCRDsByApi(crds: CRD[]) {
  const grouped = new Map<string, CRD[]>()
  for (const crd of crds) {
    grouped.set(crd.group, [...(grouped.get(crd.group) ?? []), crd])
  }

  return Array.from(grouped.entries())
    .map(([group, items]) => {
      const sortedCRDs = [...items].sort((left, right) => left.kind.localeCompare(right.kind))
      return {
        clusterCount: sortedCRDs.filter((item) => !isNamespacedCRD(item)).length,
        crdCount: sortedCRDs.length,
        crdNames: sortedCRDs.map((item) => item.name),
        crds: sortedCRDs,
        group,
        kindNames: sortedCRDs.map((item) => item.kind),
        namespacedCount: sortedCRDs.filter((item) => isNamespacedCRD(item)).length,
        versions: Array.from(new Set(sortedCRDs.flatMap((item) => getServedVersions(item)))).sort(
          (left, right) => left.localeCompare(right),
        ),
      } satisfies CRDApiGroupSummary
    })
    .sort((left, right) => left.group.localeCompare(right.group))
}

export function safeDecodeURIComponent(value?: string) {
  if (!value) return ''
  try {
    return decodeURIComponent(value)
  } catch {
    return value
  }
}

function toKebabCase(value: string) {
  return value
    .replace(/([a-z0-9])([A-Z])/g, '$1-$2')
    .replace(/[^a-zA-Z0-9]+/g, '-')
    .replace(/^-+|-+$/g, '')
    .toLowerCase()
}

export function buildDefaultCustomResourceTemplate(crd: CRD, namespace?: string | null) {
  const lines = [
    `apiVersion: ${crd.group}/${crd.version}`,
    `kind: ${crd.kind}`,
    'metadata:',
    `  name: example-${toKebabCase(crd.kind || crd.plural || 'resource')}`,
  ]
  if (isNamespacedCRD(crd) && namespace) lines.push(`  namespace: ${namespace}`)
  lines.push('spec: {}', '')
  return lines.join('\n')
}
