import { useState } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { Alert, Button, Form, Input, Select, Space, Switch, Typography } from 'antd'
import type { NetworkProxyRule, NetworkProxyRulesInput } from '@opensoha/contracts/gen/ts/sohaapi'
import { AdminTable } from '@/components/admin-table'
import { ReadFeedback } from '@/components/request-feedback'
import { useOperationMutation } from '@/components/use-operation-mutation'
import { readableQueryData } from '@/utils/query-feedback'
import { workspaceApi } from './workspace-api'
import { workspaceKeys, workspaceQueries } from './workspace-queries'
export function RulesPanel({ id, canUpdate }: { id: string; canUpdate: boolean }) {
  const [personal, setPersonal] = useState(false)
  return (
    <section className="soha-proxy-workspace-section">
      <h2>组织与个人规则</h2>
      <Alert type="info" title="平台保护与组织强制规则优先；个人规则不能绕过保护" />
      <Space wrap>
        <Button type={!personal ? 'primary' : 'default'} onClick={() => setPersonal(false)}>
          组织规则
        </Button>
        <Button type={personal ? 'primary' : 'default'} onClick={() => setPersonal(true)}>
          我的规则
        </Button>
      </Space>
      <RuleEditor key={String(personal)} id={id} canUpdate={canUpdate} personal={personal} />
    </section>
  )
}
function RuleEditor({
  id,
  canUpdate,
  personal,
}: {
  id: string
  canUpdate: boolean
  personal: boolean
}) {
  const client = useQueryClient()
  const query = useQuery(workspaceQueries.rules(id, personal, true))
  const data = readableQueryData(query)
  const [form] = Form.useForm<{ rules: NetworkProxyRule[] }>()
  const [editing, setEditing] = useState(false)
  const [base, setBase] = useState<{ org: number; personal: number }>()
  const save = useOperationMutation(
    {
      mutationFn: (input: NetworkProxyRulesInput) => workspaceApi.saveRules(id, input, personal),
      onSuccess: () => {
        setEditing(false)
        void client.invalidateQueries({ queryKey: workspaceKeys.instance(id) })
      },
    },
    {
      label: ['保存规则', 'Save rules'],
      target: () => (personal ? '当前主体' : '组织'),
      receipt: (d) => ({
        message: '规则版本已返回，请核对实际生效与屏蔽项。',
        evidence: `组织 ${d.orgRevision} / 个人 ${d.personalRevision}`,
      }),
    },
  )
  return (
    <>
      <ReadFeedback canRead query={query} label="规则" />
      <Typography.Paragraph>
        {personal
          ? '仅编辑当前主体的个人规则；本机规则主要在 App 管理。'
          : '组织强制与默认规则分别保存，不覆盖个人草稿。'}{' '}
        组织版本 {data?.orgRevision ?? '—'} / 个人版本 {data?.personalRevision ?? '—'}
      </Typography.Paragraph>
      <Space wrap>
        <Button onClick={() => void query.refetch()}>刷新规则</Button>
        <Button
          disabled={!canUpdate || !data || save.blocked}
          onClick={() => {
            setBase({ org: data!.orgRevision, personal: data!.personalRevision })
            form.setFieldsValue({
              rules: data!.rules.filter((r) =>
                personal
                  ? r.source === 'personal'
                  : ['organization_required', 'organization_default'].includes(r.source),
              ),
            })
            setEditing(true)
          }}
        >
          编辑规则
        </Button>
      </Space>
      {editing && (
        <Form
          form={form}
          layout="vertical"
          onFinish={(v) =>
            save.mutate({
              expectedOrgRevision: base!.org,
              expectedPersonalRevision: base!.personal,
              rules: (v.rules ?? []).map((rule, order) => ({
                ...rule,
                order,
                matcher: rule.matcher.type === 'all' ? { type: 'all' } : rule.matcher,
                action: ['direct', 'reject'].includes(rule.action.type)
                  ? { type: rule.action.type }
                  : rule.action,
              })),
            })
          }
        >
          <Form.List name="rules">
            {(fields, { add, remove }) => (
              <>
                {fields.map(({ key, name }, index) => (
                  <div key={key} className="soha-proxy-workspace-fields">
                    <Form.Item name={[name, 'ruleId']} label="规则 ID" rules={[{ required: true }]}>
                      <Input />
                    </Form.Item>
                    <Form.Item name={[name, 'source']} label="来源">
                      <Select
                        disabled={personal}
                        options={(personal
                          ? ['personal']
                          : ['organization_required', 'organization_default']
                        ).map((value) => ({ value, label: value }))}
                      />
                    </Form.Item>
                    <Form.Item name={[name, 'enabled']} label="启用" valuePropName="checked">
                      <Switch />
                    </Form.Item>
                    <Form.Item name={[name, 'order']} hidden>
                      <Input />
                    </Form.Item>
                    <Form.Item name={[name, 'matcher', 'type']} label="匹配类型">
                      <Select
                        options={[
                          'domain',
                          'domain_suffix',
                          'cidr',
                          'all',
                          ...(personal ? ['process_name', 'process_path'] : []),
                        ].map((value) => ({ value, label: value }))}
                      />
                    </Form.Item>
                    <Form.Item name={[name, 'matcher', 'value']} label="匹配值（全部匹配留空）">
                      <Input />
                    </Form.Item>
                    <Form.Item name={[name, 'action', 'type']} label="动作">
                      <Select
                        options={['reject', 'direct', 'node', 'group'].map((value) => ({
                          value,
                          label: value,
                        }))}
                      />
                    </Form.Item>
                    <Form.Item name={[name, 'action', 'targetId']} label="逻辑目标（节点/组）">
                      <Input />
                    </Form.Item>
                    <Button onClick={() => remove(index)}>移除</Button>
                  </div>
                ))}
                <Button
                  onClick={() =>
                    add({
                      ruleId: crypto.randomUUID(),
                      source: personal ? 'personal' : 'organization_default',
                      order: fields.length,
                      enabled: true,
                      matcher: { type: 'domain' },
                      action: { type: 'reject' },
                    })
                  }
                >
                  添加规则
                </Button>
              </>
            )}
          </Form.List>
          <Space wrap>
            <Button type="primary" htmlType="submit" disabled={save.blocked}>
              保存当前草稿
            </Button>
            <Button onClick={() => setEditing(false)}>取消编辑</Button>
          </Space>
          <Typography.Paragraph type="secondary">
            版本冲突时保留草稿。请读取最新规则并核对后重新编辑；刷新不会覆盖此草稿。
          </Typography.Paragraph>
        </Form>
      )}
      {save.feedback}
      <h3>实际有效规则</h3>
      <AdminTable
        rowKey="ruleId"
        pagination={false}
        dataSource={data?.effectiveRules ?? []}
        columns={[
          { title: '规则', dataIndex: 'ruleId' },
          { title: '来源', dataIndex: 'source' },
          { title: '顺序', dataIndex: 'order' },
          {
            title: '匹配',
            render: (_: unknown, r: NetworkProxyRule) =>
              `${r.matcher.type} ${r.matcher.value ?? ''}`,
          },
          {
            title: '动作',
            render: (_: unknown, r: NetworkProxyRule) =>
              `${r.action.type} ${r.action.targetId ?? ''}`,
          },
        ]}
      />
      <h3>屏蔽与冲突</h3>
      <AdminTable
        rowKey="ruleId"
        pagination={false}
        dataSource={data?.conflicts ?? []}
        columns={[
          { title: '规则', dataIndex: 'ruleId' },
          { title: '阻挡规则', dataIndex: 'blockingRuleId' },
          { title: '原因', dataIndex: 'reasonCode' },
        ]}
      />
    </>
  )
}
