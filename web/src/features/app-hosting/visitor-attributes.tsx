import { ContextFeedback } from '@/components/context-feedback'
import { ReadFeedback } from '@/components/request-feedback'
import { useOperationMutation } from '@/components/use-operation-mutation'
import { readableQueryData } from '@/utils/query-feedback'
import { formatDateTime } from '@/utils/time'
import type {
  HostedAppVisitorAttribute,
  HostedAppVisitorAttributeRequest,
} from '@opensoha/contracts/gen/ts/sohaapi'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import {
  Button,
  Card,
  Checkbox,
  Descriptions,
  Form,
  Input,
  Popconfirm,
  Skeleton,
  Space,
  Typography,
} from 'antd'
import { useState } from 'react'
import { hostedRuntimeMutations, hostedRuntimeQueries } from './runtime-controls-data'

const { Text } = Typography
const attributeLabels: Record<HostedAppVisitorAttribute, string> = {
  email: '邮箱',
  department: '部门',
}
const attributes: HostedAppVisitorAttribute[] = ['email', 'department']
const attributeOptions = (fields: HostedAppVisitorAttribute[]) =>
  fields.map((value) => ({ value, label: attributeLabels[value] }))
const formatAttributes = (fields: HostedAppVisitorAttribute[]) =>
  fields.map((field) => attributeLabels[field]).join('、') || '暂无额外属性'

interface AttributeRequestInput {
  fields: HostedAppVisitorAttribute[]
  reason: string
}

function VisitorAttributeRequestForm({
  busy,
  blocked,
  onSubmit,
}: {
  busy: boolean
  blocked: boolean
  onSubmit: (input: AttributeRequestInput) => void
}) {
  return (
    <Form<AttributeRequestInput>
      layout="vertical"
      disabled={blocked}
      onFinish={({ fields, reason }) => onSubmit({ fields, reason: reason.trim() })}
    >
      <Form.Item
        name="fields"
        label="申请属性"
        rules={[{ required: true, message: '至少选择一项属性' }]}
      >
        <Checkbox.Group options={attributeOptions(attributes)} />
      </Form.Item>
      <Form.Item
        name="reason"
        label="申请用途"
        rules={[{ required: true, whitespace: true, message: '请输入用途' }]}
      >
        <Input.TextArea maxLength={500} rows={3} />
      </Form.Item>
      <Button type="primary" htmlType="submit" loading={busy} disabled={blocked}>
        提交申请
      </Button>
    </Form>
  )
}

function VisitorAttributeReview({
  pending,
  canReview,
  canApprove,
  hasGrant,
  busy,
  blocked,
  onDecide,
}: {
  pending?: HostedAppVisitorAttributeRequest
  canReview: boolean
  canApprove: boolean
  hasGrant: boolean
  busy: boolean
  blocked: boolean
  onDecide: (input: { requestId?: string; fields: HostedAppVisitorAttribute[] }) => void
}) {
  const [selected, setSelected] = useState<HostedAppVisitorAttribute[]>(pending?.fields ?? [])

  return (
    <Space orientation="vertical" style={{ width: '100%' }}>
      {pending && canApprove ? (
        <fieldset style={{ border: 0, padding: 0, margin: 0 }}>
          <legend>批准字段（批准后将替换当前授权）</legend>
          <Checkbox.Group
            options={attributeOptions(pending.fields)}
            value={selected}
            onChange={setSelected}
            disabled={blocked}
          />
        </fieldset>
      ) : null}
      <Space wrap>
        {pending && canApprove ? (
          <Button
            type="primary"
            loading={busy}
            disabled={blocked || selected.length === 0}
            onClick={() => onDecide({ requestId: pending.id, fields: selected })}
          >
            批准所选字段
          </Button>
        ) : null}
        {pending && canReview ? (
          <Popconfirm
            title="拒绝此访客属性申请？"
            okText="拒绝"
            cancelText="取消"
            onConfirm={() => onDecide({ requestId: pending.id, fields: [] })}
          >
            <Button disabled={blocked}>拒绝申请</Button>
          </Popconfirm>
        ) : null}
        {hasGrant ? (
          <Popconfirm
            title="撤销当前访客属性授权？"
            description={pending ? '待审批的申请也将关闭。' : undefined}
            okText="撤销授权"
            cancelText="取消"
            onConfirm={() => onDecide({ fields: [] })}
          >
            <Button danger disabled={blocked}>
              撤销授权
            </Button>
          </Popconfirm>
        ) : null}
      </Space>
    </Space>
  )
}

type VisitorAttributeAccess =
  | { role: 'owner'; canRequest: boolean }
  | { role: 'admin'; canReview: boolean; canApprove: boolean; canRevoke: boolean }

export function HostedAppVisitorAttributes({
  appId,
  access,
}: {
  appId: string
  access: VisitorAttributeAccess
}) {
  const query = useQuery(hostedRuntimeQueries.attributes(appId))
  const data = readableQueryData(query)
  const client = useQueryClient()
  const request = useOperationMutation(hostedRuntimeMutations.requestAttributes(client, appId), {
    label: ['提交访客属性申请', 'Request visitor attributes'],
    target: () => appId,
    receipt: () => ({ message: '访客属性申请已提交，等待管理员审批' }),
  })
  const decision = useOperationMutation(hostedRuntimeMutations.decideAttributes(client, appId), {
    label: ['处理访客属性授权', 'Review visitor attribute grant'],
    target: () => appId,
    receipt: (_, input) => ({
      message: input.fields.length
        ? '所选访客属性已获授权'
        : input.requestId
          ? '访客属性申请已拒绝'
          : '访客属性授权已撤销',
    }),
  })

  return (
    <Card size="small" title="访客属性授权">
      <ReadFeedback query={query} label="访客属性授权" />
      {request.feedback}
      {decision.feedback}
      {query.isLoading ? <Skeleton active paragraph={{ rows: 2 }} /> : null}
      {data ? (
        <Space orientation="vertical" style={{ width: '100%' }}>
          <Descriptions
            column={1}
            size="small"
            items={[
              { key: 'granted', label: '当前授权', children: formatAttributes(data.granted) },
              {
                key: 'pending',
                label: '待审批申请',
                children: data.pending ? formatAttributes(data.pending.fields) : '无',
              },
              ...(data.pending
                ? [
                    {
                      key: 'reason',
                      label: '申请用途',
                      children: (
                        <Text style={{ overflowWrap: 'anywhere' }}>{data.pending.reason}</Text>
                      ),
                    },
                    {
                      key: 'requestedAt',
                      label: '申请时间',
                      children: formatDateTime(data.pending.requestedAt),
                    },
                  ]
                : []),
            ]}
          />
          {access.role === 'owner' && access.canRequest && !data.pending ? (
            <VisitorAttributeRequestForm
              busy={request.isPending}
              blocked={request.blocked}
              onSubmit={request.mutate}
            />
          ) : null}
          {access.role === 'admin' && (access.canReview || access.canRevoke) ? (
            <>
              {data.pending && !access.canReview ? (
                <ContextFeedback type="info" title="由其他管理员审批此应用的访客属性申请" />
              ) : null}
              <VisitorAttributeReview
                key={data.pending?.id ?? 'none'}
                pending={data.pending}
                canReview={access.canReview}
                busy={decision.isPending}
                blocked={decision.blocked}
                onDecide={decision.mutate}
                canApprove={access.canApprove}
                hasGrant={access.canRevoke && data.granted.length > 0}
              />
            </>
          ) : null}
        </Space>
      ) : null}
    </Card>
  )
}
