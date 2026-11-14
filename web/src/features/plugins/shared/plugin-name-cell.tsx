import { MetadataTag } from '@/components/status-tag'
import { Space, Typography } from 'antd'
import { pluginTypeLabel } from '../plugin-model'

const { Paragraph, Text } = Typography

export function PluginNameCell({
  description,
  id,
  name,
  publisher,
  type,
  version,
}: {
  description?: string
  id: string
  name: string
  publisher: string
  type: string
  version: string
}) {
  return (
    <Space orientation="vertical" size={2} className="soha-plugin-name-cell">
      <Space size={6} wrap>
        <h1 className="soha-plugin-title">{name}</h1>
        <MetadataTag label={pluginTypeLabel(type)} />
        <Text type="secondary">v{version}</Text>
      </Space>
      <Text type="secondary">
        {publisher} / {id}
      </Text>
      {description ? <Paragraph className="soha-plugin-summary">{description}</Paragraph> : null}
    </Space>
  )
}
