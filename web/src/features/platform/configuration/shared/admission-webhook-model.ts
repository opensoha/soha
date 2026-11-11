import { parseDocument } from 'yaml'
import type { ConfigurationDetailBase } from './types'

export interface WebhookSelector {
  matchLabels: Record<string, string>
  matchExpressions: { key: string; operator: string; values: string[] }[]
}
export interface AdmissionWebhookDefinition {
  name: string
  url?: string
  service?: { name: string; namespace: string; path?: string; port?: number }
  caBundleConfigured: boolean
  failurePolicy?: string
  matchPolicy?: string
  sideEffects?: string
  timeoutSeconds?: number
  admissionReviewVersions: string[]
  reinvocationPolicy?: string
  rules: {
    operations: string[]
    apiGroups: string[]
    apiVersions: string[]
    resources: string[]
    scope?: string
  }[]
  namespaceSelector?: WebhookSelector
  objectSelector?: WebhookSelector
  matchConditions: { name: string; expression: string }[]
}
export type AdmissionWebhookKind = 'MutatingWebhookConfiguration' | 'ValidatingWebhookConfiguration'
export interface AdmissionWebhookDetail extends ConfigurationDetailBase {
  configurationKind: AdmissionWebhookKind
  uid?: string
  webhooks: AdmissionWebhookDefinition[]
}
const object = (value: unknown): Record<string, unknown> => {
  if (!value || typeof value !== 'object' || Array.isArray(value))
    throw new Error('Expected an object')
  return value as Record<string, unknown>
}
const text = (value: unknown): string | undefined => {
  if (value == null) return undefined
  if (typeof value !== 'string') throw new Error('Expected a string')
  return value
}
const requiredText = (value: unknown): string => {
  const result = text(value)
  if (!result) throw new Error('Required string missing')
  return result
}
const list = (value: unknown): unknown[] => {
  if (value == null) return []
  if (!Array.isArray(value)) throw new Error('Expected an array')
  return value
}
const strings = (value: unknown) =>
  list(value).map((item) => {
    if (typeof item !== 'string') throw new Error('Invalid string array')
    return item
  })
const integer = (value: unknown): number | undefined => {
  if (value == null) return undefined
  if (typeof value !== 'number' || !Number.isSafeInteger(value)) throw new Error('Invalid integer')
  return value
}
const stringMap = (value: unknown): Record<string, string> =>
  value == null
    ? {}
    : Object.fromEntries(
        Object.entries(object(value)).map(([key, item]) => {
          if (typeof item !== 'string') throw new Error('Invalid string map')
          return [key, item]
        }),
      )
const selector = (value: unknown): WebhookSelector | undefined => {
  if (value == null) return undefined
  const item = object(value)
  return {
    matchLabels: stringMap(item.matchLabels),
    matchExpressions: list(item.matchExpressions).map((raw) => {
      const expr = object(raw)
      return {
        key: requiredText(expr.key),
        operator: requiredText(expr.operator),
        values: strings(expr.values),
      }
    }),
  }
}
export function admissionWebhookFromYAML(
  content: string,
  name: string,
  configurationKind: AdmissionWebhookKind,
): AdmissionWebhookDetail {
  if (new TextEncoder().encode(content).byteLength > 4 * 1024 * 1024)
    throw new Error('Webhook read budget exceeded')
  const doc = parseDocument(content)
  if (doc.errors.length) throw new Error('Invalid webhook YAML')
  const root = object(doc.toJS({ maxAliasCount: 50 })),
    metadata = object(root.metadata)
  if (
    root.apiVersion !== 'admissionregistration.k8s.io/v1' ||
    root.kind !== configurationKind ||
    metadata.name !== name ||
    (metadata.namespace != null && metadata.namespace !== '')
  )
    throw new Error('Webhook resource identity mismatch')
  const webhooks = list(root.webhooks).map((raw): AdmissionWebhookDefinition => {
    const item = object(raw),
      client = object(item.clientConfig)
    const service = client.service == null ? undefined : object(client.service)
    const url = text(client.url)
    if (Boolean(service) === Boolean(url))
      throw new Error('Webhook requires exactly one connection target')
    return {
      name: requiredText(item.name),
      url,
      service: service
        ? {
            name: requiredText(service.name),
            namespace: requiredText(service.namespace),
            path: text(service.path),
            port: integer(service.port),
          }
        : undefined,
      caBundleConfigured: Boolean(text(client.caBundle)),
      failurePolicy: text(item.failurePolicy),
      matchPolicy: text(item.matchPolicy),
      sideEffects: text(item.sideEffects),
      timeoutSeconds: integer(item.timeoutSeconds),
      admissionReviewVersions: strings(item.admissionReviewVersions),
      reinvocationPolicy:
        configurationKind === 'MutatingWebhookConfiguration'
          ? text(item.reinvocationPolicy)
          : undefined,
      rules: list(item.rules).map((rawRule) => {
        const rule = object(rawRule)
        return {
          operations: strings(rule.operations),
          apiGroups: strings(rule.apiGroups),
          apiVersions: strings(rule.apiVersions),
          resources: strings(rule.resources),
          scope: text(rule.scope),
        }
      }),
      namespaceSelector: selector(item.namespaceSelector),
      objectSelector: selector(item.objectSelector),
      matchConditions: list(item.matchConditions).map((rawCondition) => {
        const condition = object(rawCondition)
        return {
          name: requiredText(condition.name),
          expression: requiredText(condition.expression),
        }
      }),
    }
  })
  if (new Set(webhooks.map((item) => item.name)).size !== webhooks.length)
    throw new Error('Duplicate webhook names')
  const createdAt = text(metadata.creationTimestamp)
  return {
    configurationKind,
    name,
    uid: text(metadata.uid),
    createdAt,
    ageSeconds:
      createdAt && Number.isFinite(Date.parse(createdAt))
        ? Math.max(0, Math.floor((Date.now() - Date.parse(createdAt)) / 1000))
        : 0,
    labels: stringMap(metadata.labels),
    annotations: stringMap(metadata.annotations),
    webhooks,
  }
}
export const webhookTarget = (hook: AdmissionWebhookDefinition) =>
  hook.service ? `${hook.service.namespace}/${hook.service.name}` : hook.url || '—'
