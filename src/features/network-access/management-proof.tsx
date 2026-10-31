import { useEffect, useState } from 'react'
import { Alert, Form, Input, Modal, Typography } from 'antd'
import { SafetyCertificateOutlined } from '@ant-design/icons'
import { useQueryClient } from '@tanstack/react-query'
import type {
  EndpointDevice,
  NetworkManagementProofInput,
} from '@opensoha/contracts/gen/ts/sohaapi'
import { ManagementIconButton } from '@/components/management-list'
import { operationModalStyles } from '@/components/modal-styles'
import { useOperationMutation } from '@/components/use-operation-mutation'
import { useI18n } from '@/i18n'
import { formatDateTime } from '@/utils/time'
import { submitManagementProof } from './api'
import { networkAccessKeys } from './keys'

// The issuer signs outside the UI. This form transports an already signed proof;
// the server binds it to the current device, site and credential generation.
export function ManagementProofAction({
  device,
  disabled,
}: {
  device: EndpointDevice
  disabled: boolean
}) {
  const [open, setOpen] = useState(false)
  const [form] = Form.useForm<NetworkManagementProofInput>()
  useEffect(() => {
    if (disabled) {
      setOpen(false)
      form.resetFields()
    }
  }, [disabled, form])
  const client = useQueryClient()
  const { localeCode } = useI18n()
  const text = (zh: string, en: string) => (localeCode === 'zh_CN' ? zh : en)
  const proof = useOperationMutation(
    {
      mutationFn: (input: NetworkManagementProofInput) => submitManagementProof(device.id, input),
      onSuccess: () => {
        form.resetFields()
        void client.invalidateQueries({ queryKey: networkAccessKeys.devices.all })
        void client.invalidateQueries({ queryKey: networkAccessKeys.sessions.all })
      },
    },
    {
      label: ['提交管理证明', 'Submit management proof'],
      target: () => device.name,
      receipt: (data) => ({
        message: text('签名证明已核验', 'Signed proof verified'),
        evidence: data.managementVerifiedUntil
          ? formatDateTime(data.managementVerifiedUntil)
          : text('已撤回受管证明', 'Management proof withdrawn'),
      }),
    },
  )
  return (
    <>
      <ManagementIconButton
        aria-label={text('提交可信管理证明', 'Submit trusted management proof')}
        tooltip={text('可信管理证明', 'Trusted management proof')}
        icon={<SafetyCertificateOutlined />}
        disabled={disabled || !device.siteId}
        onClick={() => setOpen(true)}
      />
      <Modal
        title={text('可信管理证明', 'Trusted management proof')}
        open={open && !disabled}
        styles={operationModalStyles}
        destroyOnHidden
        confirmLoading={proof.isPending}
        closable={!proof.isPending}
        cancelButtonProps={{ disabled: proof.isPending }}
        okButtonProps={{ disabled: proof.blocked }}
        onCancel={() => {
          if (!proof.isPending) {
            setOpen(false)
            form.resetFields()
          }
        }}
        onOk={() => {
          if (!disabled && !proof.blocked)
            void form.validateFields().then((input) => proof.mutate(input))
        }}
      >
        {proof.feedback}
        <Typography.Paragraph>
          {device.name} · {device.siteId}
        </Typography.Paragraph>
        <Alert
          type="info"
          showIcon
          title={text(
            '粘贴组织 MDM 已签名的短期证明，不要填写私钥。服务端核对发行者、当前设备公钥与凭据代次、时间和序号；人工受管标签不能代替此证明。',
            'Paste the short-lived proof signed by your organization MDM. Never paste a private key. The server checks issuer, device key, credential generation, time and sequence. A manual managed label does not replace this proof.',
          )}
        />
        <Form form={form} layout="vertical" disabled={proof.blocked} preserve={false}>
          <Form.Item
            name="payload"
            label={text('签名载荷（base64url）', 'Signed payload (base64url)')}
            rules={[{ required: true, max: 8192, pattern: /^[A-Za-z0-9_-]+$/ }]}
          >
            <Input.TextArea rows={5} maxLength={8192} spellCheck={false} autoComplete="off" />
          </Form.Item>
          <Form.Item
            name="signature"
            label={text('Ed25519 签名（base64url）', 'Ed25519 signature (base64url)')}
            rules={[{ required: true, pattern: /^[A-Za-z0-9_-]{86}$/ }]}
          >
            <Input.TextArea rows={2} maxLength={86} spellCheck={false} autoComplete="off" />
          </Form.Item>
        </Form>
      </Modal>
    </>
  )
}
