import { useEffect, useRef, useState } from 'react'
import { Descriptions, Form, Input, Modal, Typography } from 'antd'
import type {
  AssistantMeshInquiryAudit,
  AssistantMeshInquiryAuditEnvelope,
  AssistantMeshInquiryAuditInput,
} from '@opensoha/contracts/gen/ts/sohaapi'
import { ContextFeedback } from '@/components/context-feedback'
import { api } from '@/services/api-client'
import { ApiError } from '@/services/api-error'
import { formatDateTime } from '@/utils/time'

const inquiryIdPattern = /^[0-9a-f]{8}(?:-[0-9a-f]{4}){3}-[0-9a-f]{12}$/

export function AssistantInquiryAudit({
  inquiryId,
  onClose,
}: {
  inquiryId: string
  onClose: () => void
}) {
  const [form] = Form.useForm<AssistantMeshInquiryAuditInput>()
  const [body, setBody] = useState<AssistantMeshInquiryAudit>()
  const [error, setError] = useState('')
  const [loading, setLoading] = useState(false)
  const pending = useRef<AbortController | null>(null)
  useEffect(() => () => pending.current?.abort(), [])

  async function read(values: AssistantMeshInquiryAuditInput) {
    if (pending.current) return
    const controller = new AbortController()
    pending.current = controller
    setLoading(true)
    setError('')
    setBody(undefined)
    try {
      const { data } = await api.postWithSignal<AssistantMeshInquiryAuditEnvelope>(
        '/copilot/assistant-mesh/governance/inquiry-audit',
        { inquiryId: values.inquiryId.trim(), reason: values.reason.trim() },
        controller.signal,
      )
      if (!controller.signal.aborted) setBody(data)
    } catch (error) {
      if (!controller.signal.aborted) {
        setError(
          error instanceof ApiError && error.status === 410
            ? '问询正文已超过保留期或已清理。'
            : '未能读取问询正文，请核对问询 ID 和审计权限后重试。',
        )
      }
    } finally {
      pending.current = null
      if (!controller.signal.aborted) setLoading(false)
    }
  }

  return (
    <Modal
      open
      title="查看问询正文"
      onCancel={onClose}
      onOk={() => (body ? onClose() : form.submit())}
      okText={body ? '关闭' : '读取正文'}
      confirmLoading={loading}
      cancelButtonProps={{ hidden: !!body }}
    >
      {body ? (
        <Descriptions
          size="small"
          column={1}
          items={[
            { key: 'id', label: '问询 ID', children: body.id },
            {
              key: 'deadline',
              label: '正文到期时间',
              children: body.purgeAfter ? formatDateTime(body.purgeAfter) : '关闭后计算',
            },
            ...(
              [
                ['question', '问题'],
                ['answer', '答复'],
              ] as const
            ).map(([key, label]) => ({
              key,
              label,
              children: (
                <Typography.Paragraph style={{ whiteSpace: 'pre-wrap', overflowWrap: 'anywhere' }}>
                  {body[key] || '—'}
                </Typography.Paragraph>
              ),
            })),
          ]}
        />
      ) : (
        <Form
          form={form}
          layout="vertical"
          initialValues={{ inquiryId }}
          onFinish={read}
          disabled={loading}
        >
          {error ? <ContextFeedback type="error" title={error} /> : null}
          <Form.Item
            name="inquiryId"
            label="问询 ID"
            extra="可从 Heya 问询相关审计事件的资源字段复制。"
            rules={[
              { required: true, pattern: inquiryIdPattern, message: '请输入有效的问询 ID。' },
            ]}
          >
            <Input autoFocus />
          </Form.Item>
          <Form.Item
            name="reason"
            label="查看理由"
            extra="本次查看会记录操作者、问询 ID 和理由。"
            rules={[{ required: true, whitespace: true, max: 500 }]}
          >
            <Input.TextArea rows={3} maxLength={500} />
          </Form.Item>
        </Form>
      )}
    </Modal>
  )
}
