import { ReadFeedback } from '@/components/request-feedback'
import { useOperationMutation } from '@/components/use-operation-mutation'
import { readableQueryData } from '@/utils/query-feedback'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { Button, Card, Form, Input, Popconfirm, Skeleton, Space, Typography } from 'antd'
import { useEffect, useRef } from 'react'
import { hostedRuntimeMutations, hostedRuntimeQueries } from './runtime-controls-data'

const { Text } = Typography

interface EnvironmentInput {
  name: string
  value: string
}

function EnvironmentValueForm({ appId, ownerId }: { appId: string; ownerId: string }) {
  const [form] = Form.useForm<EnvironmentInput>()
  const staged = useRef<EnvironmentInput | null>(null)
  useEffect(
    () => () => {
      staged.current = null
    },
    [],
  )
  const queryClient = useQueryClient()
  const mutation = useOperationMutation(
    hostedRuntimeMutations.writeEnvironment(queryClient, appId, ownerId, () => {
      const input = staged.current
      staged.current = null
      if (!input) throw new Error('请重新填写环境变量')
      return input
    }),
    {
      label: ['保存加密环境变量', 'Save encrypted environment variable'],
      target: () => appId,
      receipt: () => ({ message: '环境变量已保存，将在下次发布时应用' }),
    },
  )

  return (
    <Form<EnvironmentInput>
      form={form}
      layout="vertical"
      autoComplete="off"
      clearOnDestroy
      disabled={mutation.blocked}
      onFinish={(input) => {
        if (mutation.blocked) return
        staged.current = input
        void mutation
          .mutateAsync()
          .then(() => form.resetFields())
          .catch(() => undefined)
          .finally(() => {
            staged.current = null
          })
      }}
    >
      {mutation.feedback}
      <Form.Item
        name="name"
        label="变量名"
        rules={[
          { required: true, message: '请输入变量名' },
          {
            pattern: /^[A-Z][A-Z0-9_]{0,63}$/,
            message: '请使用大写字母、数字和下划线，以字母开头',
          },
        ]}
      >
        <Input maxLength={64} placeholder="APP_API_TOKEN" />
      </Form.Item>
      <Form.Item
        name="value"
        label="变量值"
        rules={[
          { required: true, message: '请输入变量值' },
          {
            validator: async (_, value: string | undefined) => {
              if (value && new TextEncoder().encode(value).length > 4096) {
                throw new Error('变量值最多为 4096 字节')
              }
            },
          },
        ]}
      >
        <Input.Password autoComplete="new-password" visibilityToggle={false} maxLength={4096} />
      </Form.Item>
      <Button
        type="primary"
        htmlType="submit"
        loading={mutation.isPending}
        disabled={mutation.blocked}
      >
        新增或替换变量
      </Button>
    </Form>
  )
}

export function HostedAppRuntimeEnvironment({
  appId,
  ownerId,
  canEdit,
}: {
  appId: string
  ownerId: string
  canEdit: boolean
}) {
  const query = useQuery(hostedRuntimeQueries.config(appId, ownerId))
  const config = readableQueryData(query)
  const mutation = useOperationMutation(
    hostedRuntimeMutations.updateConfig(useQueryClient(), appId),
    {
      label: ['移除环境变量', 'Remove environment variable'],
      target: (input) => input.removeEnvironment?.[0] ?? appId,
      receipt: () => ({ message: '环境变量已移除，将在下次发布时应用' }),
    },
  )

  return (
    <Card size="small" title="加密环境变量">
      <ReadFeedback query={query} label="加密环境变量" />
      {mutation.feedback}
      {query.isLoading ? <Skeleton active paragraph={{ rows: 2 }} /> : null}
      {config ? (
        <Space orientation="vertical" style={{ width: '100%' }}>
          <Text type="secondary">
            变量值保存后仅显示掩码。新增、替换和移除在下次发布时应用。替换或移除后，使用旧值的版本无法回滚。
          </Text>
          {config.environmentKeys.length ? (
            config.environmentKeys.map((name) => (
              <Space key={name} wrap>
                <Text code style={{ overflowWrap: 'anywhere' }}>
                  {name}
                </Text>
                <Text aria-label="值已隐藏">••••••••</Text>
                {canEdit ? (
                  <Popconfirm
                    title={`移除 ${name}？`}
                    description="使用该值的版本将无法回滚。"
                    okText="移除"
                    cancelText="取消"
                    onConfirm={() => mutation.mutate({ removeEnvironment: [name] })}
                  >
                    <Button
                      size="small"
                      danger
                      type="text"
                      disabled={mutation.blocked}
                      aria-label={`移除变量 ${name}`}
                    >
                      移除
                    </Button>
                  </Popconfirm>
                ) : null}
              </Space>
            ))
          ) : (
            <Text type="secondary">尚未配置环境变量</Text>
          )}
          {canEdit ? <EnvironmentValueForm appId={appId} ownerId={ownerId} /> : null}
        </Space>
      ) : null}
    </Card>
  )
}
