import { Badge, Space, Tag, Typography } from 'antd'
import { pluginRiskLabels } from '../plugin-model'

const { Text } = Typography

export function formatPluginDateTime(value?: string) {
  if (!value) return '-'
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return value
  return new Intl.DateTimeFormat('zh-CN', {
    month: '2-digit',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
  }).format(date)
}

export function compactPluginTags(values?: string[], max = 4) {
  const items = values?.filter(Boolean) ?? []
  if (!items.length) return <Text type="secondary">-</Text>
  return (
    <Space size={[4, 4]} wrap>
      {items.slice(0, max).map((item) => (
        <Tag key={item}>{item}</Tag>
      ))}
      {items.length > max ? <Tag>+{items.length - max}</Tag> : null}
    </Space>
  )
}

export function pluginStatusBadge(status?: string) {
  switch (status) {
    case 'enabled':
      return <Badge status="success" text="Enabled" />
    case 'pending_config':
      return <Badge status="warning" text="Pending config" />
    case 'failed':
      return <Badge status="error" text="Failed" />
    case 'deprecated':
      return <Badge status="default" text="Deprecated" />
    case 'installed':
      return <Badge status="processing" text="Installed" />
    default:
      return <Badge status="default" text="Disabled" />
  }
}

export function pluginRiskTag(riskLevel?: string) {
  const value = String(riskLevel ?? '').trim()
  if (!value) return <Text type="secondary">-</Text>
  const color =
    value === 'read' ? 'green' : value === 'write' || value === 'mutate' ? 'gold' : 'red'
  return <Tag color={color}>{pluginRiskLabels[value] ?? value}</Tag>
}

export function pluginJsonBlock(value: unknown) {
  return <pre className="soha-plugin-json">{JSON.stringify(value ?? {}, null, 2)}</pre>
}
