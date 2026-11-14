import { Alert, App, Button, Input, Modal, Space, Typography } from 'antd'
import { CopyOutlined } from '@ant-design/icons'
import type { IdentityOIDCClientSecretReveal } from '../types'

const { Paragraph, Text } = Typography

export type IdentityOIDCSecretReveal = Pick<
  IdentityOIDCClientSecretReveal,
  'clientId' | 'clientSecret'
>

interface SecretRevealModalProps {
  onClose: () => void
  value: IdentityOIDCSecretReveal | null
}

export function SecretRevealModal({ onClose, value }: SecretRevealModalProps) {
  const { message } = App.useApp()

  return (
    <Modal
      okText="关闭"
      onCancel={onClose}
      onOk={onClose}
      open={Boolean(value)}
      title="OIDC Client Secret"
    >
      <Space className="soha-identity-secret-reveal" orientation="vertical" size={12}>
        <Alert
          showIcon
          type="info"
          title="Client Secret 已加密保存，稍后仍可再次查看；每次查看都会记录审计日志。"
        />
        <div>
          <Text type="secondary">Client ID</Text>
          <Paragraph copyable className="soha-identity-secret-value">
            {value?.clientId}
          </Paragraph>
        </div>
        <div>
          <Text type="secondary">Client Secret</Text>
          <Space.Compact block>
            <Input.Password readOnly value={value?.clientSecret ?? ''} />
            <Button
              aria-label="复制 client secret"
              icon={<CopyOutlined />}
              onClick={() => {
                const secret = value?.clientSecret
                if (!secret || !navigator.clipboard) return
                navigator.clipboard.writeText(secret).then(
                  () => message.success('已复制 client secret'),
                  () => message.error('复制失败'),
                )
              }}
            />
          </Space.Compact>
        </div>
      </Space>
    </Modal>
  )
}
