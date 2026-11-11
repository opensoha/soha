type UnknownRecord = Record<string, unknown>

export type QuickEditableConfigurationKind = 'hpas' | 'poddisruptionbudgets'

export interface ConfigurationQuickEditValues {
  minReplicas?: number
  maxReplicas?: number
  availabilityMode?: 'minAvailable' | 'maxUnavailable'
  availabilityValue?: string
}

function asRecord(value: unknown): UnknownRecord {
  return value != null && typeof value === 'object' && !Array.isArray(value)
    ? (value as UnknownRecord)
    : {}
}

function specOf(manifest: unknown) {
  return asRecord(asRecord(manifest).spec)
}

export function configurationQuickEditValuesFromManifest(
  manifest: unknown,
  kind: QuickEditableConfigurationKind,
): ConfigurationQuickEditValues {
  const spec = specOf(manifest)
  if (kind === 'hpas') {
    return {
      minReplicas: typeof spec.minReplicas === 'number' ? spec.minReplicas : 1,
      maxReplicas: typeof spec.maxReplicas === 'number' ? spec.maxReplicas : 1,
    }
  }
  const mode = spec.maxUnavailable != null ? 'maxUnavailable' : 'minAvailable'
  const value = spec[mode]
  return {
    availabilityMode: mode,
    availabilityValue: value == null ? '1' : String(value),
  }
}

export function mergeConfigurationQuickEditManifest(
  manifest: unknown,
  kind: QuickEditableConfigurationKind,
  values: ConfigurationQuickEditValues,
) {
  const cloned = JSON.parse(JSON.stringify(asRecord(manifest))) as UnknownRecord
  const spec = asRecord(cloned.spec)
  cloned.spec = spec
  if (kind === 'hpas') {
    if (values.minReplicas == null || values.maxReplicas == null) {
      throw new Error('HPA min and max replicas are required')
    }
    if (values.minReplicas > values.maxReplicas) {
      throw new Error('HPA minimum replicas cannot exceed maximum replicas')
    }
    spec.minReplicas = values.minReplicas
    spec.maxReplicas = values.maxReplicas
    return cloned
  }

  const mode = values.availabilityMode ?? 'minAvailable'
  const raw = values.availabilityValue?.trim()
  if (!raw) throw new Error('PDB availability value is required')
  delete spec.minAvailable
  delete spec.maxUnavailable
  spec[mode] = /^\d+$/.test(raw) ? Number(raw) : raw
  return cloned
}
