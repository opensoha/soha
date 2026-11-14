import { Card, Space, Tag, Typography } from 'antd'

const { Paragraph, Text, Title } = Typography

export function AboutPage() {
  return (
    <div className="soha-page">
      <Card>
        <Space orientation="vertical" size={12}>
          <Title level={3}>关于 OpenSoha</Title>
          <Paragraph>Soha AI Gateway 与云原生运维控制台。</Paragraph>
          <Space size={8} wrap>
            <Tag color="blue">Console</Tag>
            <Tag>Apache-2.0</Tag>
          </Space>
          <Text type="secondary">OpenSoha 开源、自托管，面向统一平台管理场景。</Text>
        </Space>
      </Card>
    </div>
  )
}
