import { Space, Tag, Typography } from 'antd'
import type { PluginExtensionPoints, PluginExtensionRecord } from '../plugin-model'
import { compactPluginTags } from './formatters'
import { isPluginSchemaObject } from './schema'

const { Text } = Typography

export function extensionPointEntries(points?: PluginExtensionPoints | null) {
  if (!points) return []
  const entries: Array<{
    key: string
    point: string
    id: string
    label?: string
    actionRef?: string
    permissionKeys?: string[]
  }> = []
  Object.entries(points).forEach(([scope, group]) => {
    if (!isPluginSchemaObject(group)) return
    Object.entries(group).forEach(([name, contributions]) => {
      if (!Array.isArray(contributions)) return
      contributions.forEach((item, index) => {
        if (!isPluginSchemaObject(item)) return
        const id = String(item.id || `${scope}.${name}.${index}`)
        entries.push({
          key: `${scope}.${name}.${id}`,
          point: `${scope}.${name}`,
          id,
          label: typeof item.label === 'string' ? item.label : undefined,
          actionRef: typeof item.actionRef === 'string' ? item.actionRef : undefined,
          permissionKeys: Array.isArray(item.permissionKeys)
            ? item.permissionKeys.filter((value): value is string => typeof value === 'string')
            : undefined,
        })
      })
    })
  })
  return entries
}

export function ExtensionPointList({ points }: { points?: PluginExtensionPoints | null }) {
  const entries = extensionPointEntries(points)
  if (!entries.length) return <Text type="secondary">未声明扩展点</Text>
  return (
    <Space orientation="vertical" size={8} className="soha-plugin-extension-list">
      {entries.map((entry) => (
        <div className="soha-plugin-extension-row" key={entry.key}>
          <Space size={6} wrap>
            <Tag color="blue">{entry.point}</Tag>
            <Text strong>{entry.label || entry.id}</Text>
            {entry.actionRef ? <Tag>{entry.actionRef}</Tag> : null}
          </Space>
          {entry.permissionKeys?.length ? (
            <div>{compactPluginTags(entry.permissionKeys, 4)}</div>
          ) : null}
        </div>
      ))}
    </Space>
  )
}

export function RegisteredExtensionList({ records }: { records: PluginExtensionRecord[] }) {
  if (!records.length) return <Text type="secondary">当前没有已注册扩展</Text>
  return (
    <Space orientation="vertical" size={8} className="soha-plugin-extension-list">
      {records.map((record) => (
        <div
          className="soha-plugin-extension-row"
          key={`${record.pluginId}:${record.point}:${record.id}`}
        >
          <Space size={6} wrap>
            <Tag color="blue">{record.point}</Tag>
            <Text strong>{record.label || record.id}</Text>
            <Tag>{record.runtimeMode || 'manifest-only'}</Tag>
            {record.configured ? (
              <Tag color="green">configured</Tag>
            ) : (
              <Tag color="gold">pending config</Tag>
            )}
          </Space>
          {record.actionRef ? <Text type="secondary">{record.actionRef}</Text> : null}
          {record.permissionKeys?.length ? (
            <div>{compactPluginTags(record.permissionKeys, 4)}</div>
          ) : null}
        </div>
      ))}
    </Space>
  )
}
