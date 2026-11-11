import { operationModalStyles } from '@/components/modal-styles'
import { App, Collapse, Form, Input, Modal, Select } from 'antd'
import type { ReactNode } from 'react'
import { useEffect } from 'react'
import { identityOutpostModeOptions } from '../options'
import type { IdentityOutpost, IdentityOutpostInput, IdentityOutpostMode } from '../types'
export interface IdentityOutpostFormValues {
  endpoint?: string
  forwardAuthUrl?: string
  metadataJson?: string
  mode: IdentityOutpostMode
  name: string
}
interface IdentityOutpostFormModalProps {
  feedback?: ReactNode
  blocked?: boolean
  allowedModes?: IdentityOutpostMode[]
  editing: IdentityOutpost | null
  onCancel: () => void
  onSubmit: (input: IdentityOutpostInput) => void
  open: boolean
  submitting: boolean
}
function parseMetadata(value?: string) {
  const trimmed = String(value ?? '').trim()
  if (!trimmed) return {}
  const parsed: unknown = JSON.parse(trimmed)
  if (!parsed || Array.isArray(parsed) || typeof parsed !== 'object') {
    throw new Error('metadata must be a JSON object')
  }
  return parsed as Record<string, unknown>
}
function formValues(outpost?: IdentityOutpost | null): IdentityOutpostFormValues {
  return {
    endpoint: outpost?.endpoint ?? '',
    forwardAuthUrl: outpost?.forwardAuthUrl ?? '',
    metadataJson: JSON.stringify(outpost?.metadata ?? {}, null, 2),
    mode: outpost?.mode ?? 'embedded',
    name: outpost?.name ?? '',
  }
}
export function buildIdentityOutpostInput(values: IdentityOutpostFormValues): IdentityOutpostInput {
  return {
    endpoint: values.endpoint?.trim(),
    forwardAuthUrl: values.forwardAuthUrl?.trim(),
    metadata: parseMetadata(values.metadataJson),
    mode: values.mode,
    name: values.name.trim(),
    status: 'offline',
  }
}
export function IdentityOutpostFormModal({
  feedback,
  blocked = false,
  allowedModes = ['embedded'],
  editing,
  onCancel,
  onSubmit,
  open,
  submitting,
}: IdentityOutpostFormModalProps) {
  const { message } = App.useApp()
  const [form] = Form.useForm<IdentityOutpostFormValues>()
  const mode = Form.useWatch('mode', form)
  useEffect(() => {
    if (open) form.setFieldsValue(formValues(editing))
  }, [editing, form, open])
  const submit = async () => {
    const values = await form.validateFields()
    try {
      onSubmit(buildIdentityOutpostInput(values))
    } catch (error) {
      message.error(error instanceof Error ? error.message : 'metadata JSON 无效')
    }
  }
  return (
    <Modal
      confirmLoading={submitting}
      destroyOnHidden
      okText={editing ? '保存' : '创建'}
      onCancel={onCancel}
      onOk={() => void submit()}
      open={open}
      title={editing ? `编辑 ${editing.name}` : '新建 Outpost'}
      width={720}
      style={{ top: 32 }}
      styles={operationModalStyles}
      closable={!blocked}
      okButtonProps={{ disabled: blocked }}
      cancelButtonProps={{ disabled: blocked }}
    >
      {feedback}

      <Form form={form} layout="vertical" preserve={false} disabled={blocked}>
        <Form.Item
          name="name"
          label="名称"
          rules={[
            { required: true, whitespace: true, max: 200, message: '请输入 1–200 字符的名称' },
          ]}
        >
          <Input placeholder="edge-grafana" />
        </Form.Item>
        <Form.Item name="mode" label="部署方式" rules={[{ required: true }]}>
          <Select
            options={identityOutpostModeOptions.map((option) => ({
              ...option,
              disabled: !allowedModes.includes(option.value) && editing?.mode !== option.value,
            }))}
          />
        </Form.Item>
        {mode !== 'embedded' && (
          <Form.Item
            name="forwardAuthUrl"
            label="鉴权地址"
            extra="边缘代理调用此地址；包含 Agent 的完整 forward-auth 路径。"
            rules={[{ max: 2048 }]}
          >
            <Input placeholder="https://outpost.example.com/api/v1/outpost/forward-auth" />
          </Form.Item>
        )}
        <Collapse
          items={[
            {
              key: 'advanced',
              label: '高级配置',
              forceRender: true,
              children: (
                <>
                  <Form.Item name="endpoint" label="管理地址（兼容字段）">
                    <Input placeholder="https://outpost.example.com" />
                  </Form.Item>
                  <Form.Item name="metadataJson" label="Metadata JSON">
                    <Input.TextArea autoSize={{ minRows: 4, maxRows: 10 }} spellCheck={false} />
                  </Form.Item>
                </>
              ),
            },
          ]}
        />
      </Form>
    </Modal>
  )
}
