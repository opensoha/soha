import { Space, Typography } from 'antd'
import { MetadataTag } from '@/components/status-tag'

const { Text } = Typography

export function formatDateTime(value?: string) {
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

export function compactList(values?: string[], max = 3) {
  const items = values?.filter(Boolean) ?? []
  if (!items.length) return <Text type="secondary">-</Text>
  return (
    <Space size={[4, 4]} wrap>
      {items.slice(0, max).map((item) => (
        <MetadataTag key={item} label={item} />
      ))}
      {items.length > max ? <MetadataTag label={`+${items.length - max}`} /> : null}
    </Space>
  )
}
