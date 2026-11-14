import { parseDocument } from 'yaml'
import type { GatewayCondition } from './gateway-configuration'

export interface GatewayClassParameters {
  group: string
  kind: string
  name: string
  namespace?: string
}
export interface GatewayClassConfiguration {
  generation?: number
  createdAt?: string
  description?: string
  parameters: GatewayClassParameters | null
  conditions?: GatewayCondition[]
}
const object = (value: unknown): Record<string, unknown> =>
  value && typeof value === 'object' && !Array.isArray(value)
    ? (value as Record<string, unknown>)
    : {}
const text = (value: unknown) => (typeof value === 'string' ? value : '')
const generation = (value: unknown) =>
  typeof value === 'number' && Number.isSafeInteger(value) && value >= 0 ? value : undefined

export function gatewayClassConfiguration(
  content: string,
  name: string,
): GatewayClassConfiguration {
  if (!content.trim() || new TextEncoder().encode(content).byteLength > 4 * 1024 * 1024)
    throw new Error('GatewayClass YAML is empty or exceeds 4 MiB')
  const document = parseDocument(content)
  if (document.errors.length) throw new Error('Invalid GatewayClass YAML')
  const manifest = object(document.toJS({ maxAliasCount: 50 })),
    metadata = object(manifest.metadata)
  if (
    manifest.kind !== 'GatewayClass' ||
    !['gateway.networking.k8s.io/v1', 'gateway.networking.k8s.io/v1beta1'].includes(
      text(manifest.apiVersion),
    ) ||
    metadata.name !== name ||
    metadata.namespace
  )
    throw new Error('GatewayClass YAML identity does not match the current resource')
  const spec = object(manifest.spec),
    status = object(manifest.status)
  let parameters: GatewayClassParameters | null = null
  if (spec.parametersRef !== undefined && spec.parametersRef !== null) {
    const ref = object(spec.parametersRef)
    if (
      typeof ref.group !== 'string' ||
      !text(ref.kind).trim() ||
      !text(ref.name).trim() ||
      (ref.namespace !== undefined && !text(ref.namespace).trim())
    )
      throw new Error('Invalid GatewayClass parameters reference')
    parameters = {
      group: ref.group,
      kind: text(ref.kind),
      name: text(ref.name),
      namespace: ref.namespace === undefined ? undefined : text(ref.namespace),
    }
  }
  return {
    generation: generation(metadata.generation),
    createdAt: text(metadata.creationTimestamp),
    description: text(spec.description),
    parameters,
    conditions: Array.isArray(status.conditions)
      ? status.conditions.flatMap((raw: unknown) => {
          const item = object(raw)
          return text(item.type) && text(item.status)
            ? [
                {
                  type: text(item.type),
                  status: text(item.status),
                  reason: text(item.reason),
                  message: text(item.message),
                  lastTransitionTime: text(item.lastTransitionTime),
                  observedGeneration: generation(item.observedGeneration),
                },
              ]
            : []
        })
      : undefined,
  }
}
