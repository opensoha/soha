import { ManagementIconButton } from '@/components/management-list'
import { useOperationMutation } from '@/components/use-operation-mutation'
import { SecretRefSelect } from '@/features/secrets'
import { queryAccessDenied, readableQueryData } from '@/utils/query-feedback'
import { DeleteOutlined, PlusOutlined, SendOutlined } from '@ant-design/icons'
import { Alert, App, Button, Descriptions, Drawer, Form, Input, Space, Typography } from 'antd'
import { useEffect } from 'react'
import { invokeGatewayTool, type GatewayToolInvocationValues } from './mutations'
import { stringifyPayload, type GatewayTool } from './types'

const { Text } = Typography

export function GatewayToolInvokeDrawer({
  aiClientId,
  onClose,
  skillId,
  tool,
}: {
  aiClientId?: string
  onClose: () => void
  skillId?: string
  tool?: GatewayTool
}) {
  const { modal } = App.useApp()
  const [form] = Form.useForm<GatewayToolInvocationValues>()
  const mutation = useOperationMutation(
    {
      mutationFn: (values: GatewayToolInvocationValues) =>
        invokeGatewayTool({
          toolName: tool?.name ?? '',
          capabilityVersion: tool?.version,
          values,
          aiClientId,
          skillId,
        }),
    },
    {
      label: ['调用 Gateway 工具', 'Invoke Gateway tool'],
      target: () => tool?.name || '当前工具',
      receipt: (result) => ({ message: '工具调用请求已返回', evidence: result.data.result }),
    },
  )
  useEffect(() => {
    if (queryAccessDenied(mutation)) form.resetFields()
  }, [mutation.error, form])

  const close = () => {
    if (mutation.blocked) return
    mutation.reset()
    form.resetFields()
    onClose()
  }

  const submit = async () => {
    if (mutation.blocked) return
    let values: GatewayToolInvocationValues
    try {
      values = await form.validateFields()
    } catch {
      return
    }
    if (
      tool?.requiresApproval ||
      tool?.riskLevel === 'mutate' ||
      tool?.riskLevel === 'execute' ||
      tool?.riskLevel === 'high'
    ) {
      modal.confirm({
        title: `提交 ${tool.name}？`,
        content: `Risk: ${tool.riskLevel}`,
        okText: '提交',
        onOk: () => mutation.mutateAsync(values).catch(() => undefined),
      })
      return
    }
    mutation.mutate(values)
  }

  return (
    <Drawer
      destroyOnHidden
      size={640}
      open={Boolean(tool)}
      closable={!mutation.blocked}
      keyboard={!mutation.blocked}
      mask={{ closable: !mutation.blocked }}
      title={tool?.name}
      onClose={close}
      extra={
        <Space>
          <Button disabled={mutation.blocked} onClick={close}>
            取消
          </Button>
          <Button
            type="primary"
            disabled={mutation.blocked}
            icon={<SendOutlined />}
            loading={mutation.isPending}
            onClick={() => void submit()}
          >
            调用
          </Button>
        </Space>
      }
    >
      {mutation.feedback}
      {tool ? (
        <Space orientation="vertical" size={16} style={{ width: '100%' }}>
          <Descriptions
            size="small"
            bordered
            column={3}
            items={[
              { key: 'risk', label: 'Risk', children: tool.riskLevel },
              {
                key: 'approval',
                label: 'Approval',
                children: tool.requiresApproval ? 'required' : 'policy',
              },
              { key: 'scopes', label: 'Scopes', children: tool.requiredScopes?.join(', ') || '-' },
            ]}
          />
          <Form
            disabled={mutation.blocked}
            form={form}
            layout="vertical"
            preserve={false}
            initialValues={{ inputJson: '{}', secretRefs: [] }}
          >
            <Form.Item label="Input JSON" name="inputJson" rules={[{ required: true }]}>
              <Input.TextArea autoSize={{ minRows: 8, maxRows: 16 }} spellCheck={false} />
            </Form.Item>
            <Form.List name="secretRefs">
              {(fields, { add, remove }) => (
                <Space orientation="vertical" size={8} style={{ width: '100%' }}>
                  {fields.map((field) => (
                    <Space key={field.key} align="baseline" style={{ width: '100%' }}>
                      <Form.Item
                        {...field}
                        name={[field.name, 'alias']}
                        rules={[
                          { required: true },
                          { pattern: /^[A-Z_][A-Z0-9_]*$/, message: '使用大写 alias' },
                        ]}
                      >
                        <Input placeholder="ALIAS" style={{ width: 160 }} />
                      </Form.Item>
                      <Form.Item
                        {...field}
                        name={[field.name, 'ref']}
                        rules={[{ required: true }]}
                        style={{ flex: 1 }}
                      >
                        <SecretRefSelect enabled={Boolean(tool)} />
                      </Form.Item>
                      <ManagementIconButton
                        danger
                        disabled={mutation.blocked}
                        aria-label="删除 Secret reference"
                        tooltip="删除"
                        icon={<DeleteOutlined />}
                        onClick={() => remove(field.name)}
                      />
                    </Space>
                  ))}
                  <Button
                    disabled={mutation.blocked}
                    type="dashed"
                    icon={<PlusOutlined />}
                    onClick={() => add()}
                  >
                    添加 Secret reference
                  </Button>
                </Space>
              )}
            </Form.List>
          </Form>
          {tool.inputSchema ? (
            <details>
              <summary>Input schema</summary>
              <pre className="soha-system-json-block">{stringifyPayload(tool.inputSchema)}</pre>
            </details>
          ) : null}
          {!queryAccessDenied(mutation) && readableQueryData(mutation) ? (
            <Space orientation="vertical" size={8} style={{ width: '100%' }}>
              <Alert type="success" showIcon title={readableQueryData(mutation)?.data.result} />
              <Text code>{readableQueryData(mutation)?.data.toolName}</Text>
              <pre className="soha-system-json-block">
                {stringifyPayload(readableQueryData(mutation)?.data)}
              </pre>
            </Space>
          ) : null}
        </Space>
      ) : null}
    </Drawer>
  )
}
