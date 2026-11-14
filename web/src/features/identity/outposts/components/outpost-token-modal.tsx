import { Alert, Button, Modal, Space, Typography } from 'antd'

const { Text } = Typography

export interface IdentityOutpostCreatedToken {
  name: string
  token: string
}

interface IdentityOutpostTokenModalProps {
  onClose: () => void
  value: IdentityOutpostCreatedToken | null
}

export function IdentityOutpostTokenModal({ onClose, value }: IdentityOutpostTokenModalProps) {
  return (
    <Modal
      destroyOnHidden
      footer={
        <Button type="primary" onClick={onClose}>
          关闭
        </Button>
      }
      onCancel={onClose}
      open={Boolean(value)}
      title={value ? `${value.name} token` : 'Outpost token'}
      width={720}
    >
      <Space orientation="vertical" size={12} style={{ width: '100%' }}>
        <Alert
          showIcon
          type="warning"
          title="Token is shown once"
          description="Use this token for outpost claim, heartbeat, check, and events calls."
        />
        <Text code copyable={{ text: value?.token ?? '' }} style={{ wordBreak: 'break-all' }}>
          {value?.token}
        </Text>
      </Space>
    </Modal>
  )
}
