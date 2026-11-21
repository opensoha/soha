import { ContextFeedback } from '@/components/context-feedback'
import { ReadFeedback } from '@/components/request-feedback'
import { useOperationMutation } from '@/components/use-operation-mutation'
import { readableQueryData } from '@/utils/query-feedback'
import { formatDateTime } from '@/utils/time'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { Button, Card, Descriptions, Form, InputNumber, Skeleton, Space } from 'antd'
import { hostedRuntimeMutations, hostedRuntimeQueries } from './runtime-controls-data'
import { formatHostedAppBytes } from './types'

const mib = 1024 * 1024

function RuntimeWarningForm({ appId, warningBytes }: { appId: string; warningBytes: number }) {
  const mutation = useOperationMutation(
    hostedRuntimeMutations.updateConfig(useQueryClient(), appId),
    {
      label: ['保存用量预警阈值', 'Save usage warning threshold'],
      target: () => appId,
      receipt: () => ({ message: '用量预警阈值已保存' }),
    },
  )

  return (
    <Form<{ warningMiB: number }>
      layout="vertical"
      initialValues={{ warningMiB: warningBytes / mib }}
      onFinish={({ warningMiB }) =>
        mutation.mutate({ dataWarningBytes: Math.round(warningMiB * mib) })
      }
    >
      {mutation.feedback}
      <Form.Item
        name="warningMiB"
        label="预警阈值（MiB）"
        rules={[{ required: true, type: 'number', min: 1 / mib, max: 1048576 }]}
      >
        <InputNumber min={1 / mib} max={1048576} step={1} disabled={mutation.blocked} />
      </Form.Item>
      <Button htmlType="submit" disabled={mutation.blocked} loading={mutation.isPending}>
        保存预警阈值
      </Button>
    </Form>
  )
}

export function HostedAppRuntimeUsage({
  appId,
  canSetWarning,
}: {
  appId: string
  canSetWarning: boolean
}) {
  const query = useQuery(hostedRuntimeQueries.usage(appId))
  const usage = readableQueryData(query)

  return (
    <Card
      size="small"
      title="受管数据库用量"
      extra={
        <Button size="small" loading={query.isFetching} onClick={() => void query.refetch()}>
          刷新用量
        </Button>
      }
    >
      <ReadFeedback query={query} label="受管数据库用量" />
      {query.isLoading ? <Skeleton active paragraph={{ rows: 2 }} /> : null}
      {usage ? (
        <Space orientation="vertical" style={{ width: '100%' }}>
          <Descriptions
            column={1}
            size="small"
            items={[
              {
                key: 'usage',
                label: '已使用',
                children:
                  usage.status === 'not_enabled'
                    ? '未启用受管数据库'
                    : usage.status === 'available' && usage.bytes !== undefined
                      ? formatHostedAppBytes(usage.bytes)
                      : '暂时无法获取用量',
              },
              { key: 'sampledAt', label: '采样时间', children: formatDateTime(usage.sampledAt) },
              {
                key: 'warning',
                label: '预警阈值',
                children: `${usage.warningBytes / mib} MiB${usage.warningBytes === 200 * mib ? '（默认值）' : ''}`,
              },
            ]}
          />
          {usage.warning ? (
            <ContextFeedback type="warning" showIcon title="数据库用量已达到预警阈值" />
          ) : null}
          {canSetWarning ? (
            <RuntimeWarningForm appId={appId} warningBytes={usage.warningBytes} />
          ) : null}
        </Space>
      ) : null}
    </Card>
  )
}
