import { Input, InputNumber, Select, Switch } from 'antd'

export type PluginSchemaObject = Record<string, unknown>

export function isPluginSchemaObject(value: unknown): value is PluginSchemaObject {
  return Boolean(value) && typeof value === 'object' && !Array.isArray(value)
}

export function pluginSchemaProperties(schema?: unknown) {
  if (!isPluginSchemaObject(schema) || !isPluginSchemaObject(schema.properties)) return []
  return Object.entries(schema.properties).map(([name, raw]) => ({
    name,
    schema: isPluginSchemaObject(raw) ? raw : {},
  }))
}

export function pluginSchemaRequired(schema?: unknown) {
  if (!isPluginSchemaObject(schema) || !Array.isArray(schema.required)) {
    return new Set<string>()
  }
  return new Set(schema.required.filter((item): item is string => typeof item === 'string'))
}

export function pluginSchemaFieldLabel(name: string, schema: PluginSchemaObject) {
  return String(schema.title || schema.label || name)
}

export function pluginSchemaFieldControl(schema: PluginSchemaObject) {
  return String(schema['x-control'] || schema.format || schema.type || 'text')
}

export function renderPluginSchemaInput(schema: PluginSchemaObject) {
  const control = pluginSchemaFieldControl(schema)
  if (Array.isArray(schema.enum)) {
    return (
      <Select
        allowClear
        options={schema.enum.map((value) => ({ value: String(value), label: String(value) }))}
      />
    )
  }
  if (schema.type === 'boolean') return <Switch />
  if (schema.type === 'number' || schema.type === 'integer') {
    return <InputNumber style={{ width: '100%' }} />
  }
  if (control === 'secret-ref') return <Input.Password placeholder="secret://..." />
  if (control === 'endpoint' || schema.format === 'uri') {
    return <Input placeholder="https://..." />
  }
  if (control === 'resource-selector') {
    return <Input.TextArea rows={3} placeholder='{"clusterId":"prod","namespace":"default"}' />
  }
  return <Input />
}
