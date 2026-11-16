import { Card, Space, Tag, Typography } from 'antd'

const { Paragraph, Text, Title } = Typography

export function AboutPage() {
  return (
    <div className="soha-page">
      <Card>
        <Space orientation="vertical" size={12}>
          <Title level={3}>关于 Soha</Title>
          <Paragraph>Soha，解决中小企业运维难题。</Paragraph>
          <Paragraph>
            让人手有限的团队，更轻松地管理、维护和保障业务运行。围绕实际运维需要持续接入新能力。
          </Paragraph>
          <Space size={8} wrap>
            <Tag color="blue">运维工作台</Tag>
            <Tag>Apache-2.0</Tag>
          </Space>
          <Text type="secondary">Soha 由 OpenSoha 社区维护，开源、自托管。</Text>
        </Space>
      </Card>
    </div>
  )
}
