import { isSecretReference } from '@/features/secrets'
import type { StorageDriverDescriptor } from '@opensoha/contracts/gen/ts/sohaapi'

type StorageFieldKind = 'string' | 'boolean' | 'integer'

export interface StorageField {
  key: string
  kind: StorageFieldKind
  title?: string
  description?: string
  required: boolean
  secretReference: boolean
  defaultValue?: string
  options?: string[]
  pattern?: RegExp
  minLength?: number
  maxLength?: number
  minimum?: number
  maximum?: number
}

export interface PluginStorageDriver {
  id: string
  name: string
  fields: StorageField[]
}

function isObject(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null && !Array.isArray(value)
}

function stringValue(value: unknown) {
  return typeof value === 'string' ? value : undefined
}

function numberValue(value: unknown) {
  return typeof value === 'number' && Number.isFinite(value) ? value : undefined
}

function fieldValue(kind: StorageFieldKind, value: unknown) {
  if (kind === 'boolean' && typeof value === 'boolean') return String(value)
  if (kind === 'integer' && typeof value === 'number' && Number.isSafeInteger(value)) {
    return String(value)
  }
  if (kind === 'string') return stringValue(value)
}

export function parsePluginStorageDriver(descriptor: StorageDriverDescriptor) {
  const schema = descriptor.configSchema
  if (
    descriptor.id === 's3' ||
    !isObject(schema) ||
    schema.type !== 'object' ||
    schema.additionalProperties !== false ||
    schema['x-soha-test-connection-action'] !== 'storage.testConnection' ||
    !isObject(schema.properties)
  ) {
    return undefined
  }
  const { properties } = schema
  const required = new Set<string>(Array.isArray(schema.required) ? schema.required : [])
  const fields: StorageField[] = []
  for (const [key, spec] of Object.entries(properties)) {
    if (!isObject(spec)) return undefined
    const kind = spec.type
    const widget = spec['x-soha-widget']
    if (kind !== 'string' && kind !== 'boolean' && kind !== 'integer') return undefined
    if (widget && (widget !== 'secret-ref' || kind !== 'string')) return undefined
    const secretReference = widget === 'secret-ref'
    const rawPattern = stringValue(spec.pattern)
    let pattern: RegExp | undefined
    if (rawPattern) {
      try {
        pattern = new RegExp(rawPattern, 'u')
      } catch {
        return undefined
      }
    }
    const field: StorageField = {
      key,
      kind,
      title: stringValue(spec.title),
      description: stringValue(spec.description),
      required: required.has(key),
      secretReference,
      options: Array.isArray(spec.enum)
        ? spec.enum.map((value) => fieldValue(kind, value)).filter((value) => value !== undefined)
        : undefined,
      pattern,
      minLength: numberValue(spec.minLength),
      maxLength: numberValue(spec.maxLength),
      minimum: numberValue(spec.minimum),
      maximum: numberValue(spec.maximum),
    }
    const defaultValue = fieldValue(kind, spec.default)
    if (!secretReference && defaultValue !== undefined && !storageFieldError(field, defaultValue)) {
      field.defaultValue = defaultValue
    }
    fields.push(field)
  }
  if (fields.length === 0 || [...required].some((key) => !Object.hasOwn(properties, key))) {
    return undefined
  }
  return { id: descriptor.id, name: descriptor.name, fields } satisfies PluginStorageDriver
}

export function storageFieldError(field: StorageField, input: string | null | undefined) {
  const value = input?.trim()
  if (!value) return field.required ? 'required' : undefined
  if (field.secretReference && !isSecretReference(value)) return 'reference'
  if (field.options && !field.options.includes(value)) return 'option'
  if (field.kind === 'boolean' && value !== 'true' && value !== 'false') return 'option'
  if (field.kind === 'integer') {
    const number = Number(value)
    if (
      !/^-?\d+$/.test(value) ||
      !Number.isInteger(number) ||
      number < -(2 ** 52) ||
      number >= 2 ** 52
    ) {
      return 'integer'
    }
    if (
      (field.minimum !== undefined && number < field.minimum) ||
      (field.maximum !== undefined && number > field.maximum)
    ) {
      return 'range'
    }
  }
  const length = [...value].length
  if (
    (field.minLength !== undefined && length < field.minLength) ||
    length > Math.min(field.maxLength ?? 8192, 8192)
  ) {
    return 'length'
  }
  if (field.pattern && !field.pattern.test(value)) return 'pattern'
}

export function pluginStorageConfiguration(
  fields: StorageField[],
  values: Record<string, string | null | undefined>,
) {
  return fields.flatMap((field) => {
    const value = values[field.key]?.trim()
    if (storageFieldError(field, value)) throw new Error('Invalid storage configuration')
    return value ? [{ key: field.key, value }] : []
  })
}
