import { ReadFeedback } from '@/components/request-feedback'
import { useOperationMutation } from '@/components/use-operation-mutation'
import { queryAccessDenied, readableQueryData } from '@/utils/query-feedback'
import type { AssistantMeshSettings as Settings } from '@opensoha/contracts/gen/ts/sohaapi'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { Button, Col, Form, InputNumber, Row, Select, Switch, Typography } from 'antd'
import { settingsMutations } from '../mutations'
import { settingsQueries } from '../queries'
import { SettingsCard } from '../shared/components'

const inquiryFields = [
  {
    name: 'inquiryPairLimitPerHour',
    label: '同事间提问上限（次 / 小时）',
    initial: 5,
    min: 1,
    max: 60,
  },
  {
    name: 'inquiryCallerLimitPerDay',
    label: '跨员工提问上限（次 / 天）',
    initial: 30,
    min: 1,
    max: 500,
  },
  {
    name: 'inquiryMergeWindowHours',
    label: '重复问询合并窗口（小时）',
    initial: 24,
    min: 1,
    max: 168,
  },
  { name: 'inquiryPendingLimit', label: '每位员工的未决待办上限', initial: 20, min: 5, max: 200 },
  {
    name: 'inquiryTimeoutHours',
    label: '新问询超时（小时）',
    initial: 72,
    min: 1,
    max: 720,
    hint: '保存后适用于新问询。',
  },
  {
    name: 'inquiryRetentionDays',
    label: '问询正文保留期（天）',
    initial: 7,
    min: 1,
    max: 365,
    hint: '问询关闭时确定，已关闭的问询沿用原到期日。',
  },
] as const

const inquiryDefaults = Object.fromEntries(
  inquiryFields.map(({ name, initial }) => [name, initial]),
)

export function AssistantMeshSettings({ owner }: { owner: string }) {
  const queryClient = useQueryClient()
  const query = useQuery(settingsQueries.ai.assistantMesh(owner))
  const settings = readableQueryData(query)
  const save = useOperationMutation(settingsMutations.ai.saveAssistantMesh(queryClient, owner), {
    label: ['保存助手互联设置', 'Save Assistant Mesh settings'],
    target: () => 'Heya 助手互联',
    receipt: () => ({ message: '助手互联设置已保存' }),
  })
  const editable = query.isSuccess && !queryAccessDenied(save) && !save.blocked

  return (
    <SettingsCard title="Heya 助手互联">
      <ReadFeedback label="助手互联设置" query={query} canRead={query.isEnabled} />
      {save.feedback}
      {settings && (
        <Form<Settings>
          key={JSON.stringify(settings)}
          layout="vertical"
          initialValues={{ delegationLifetimeHours: 4, ...inquiryDefaults, ...settings }}
          disabled={!editable || save.isPending}
          onFinish={(values) => {
            if (editable && !save.isPending) save.mutate(values)
          }}
        >
          <Form.Item label="启用组织助手互联" name="enabled" valuePropName="checked">
            <Switch />
          </Form.Item>
          <Form.Item
            label="全组织发布审批人"
            name="organizationApproverUserIds"
            extra="输入有效用户的 UUID 后按回车。留空时由平台管理员审批。"
          >
            <Select mode="tags" maxCount={100} />
          </Form.Item>
          <Form.Item label="发布默认可见范围" name="defaultVisibility">
            <Select
              options={[
                { label: '仅自己', value: 'private' },
                { label: '所属团队', value: 'teams' },
                { label: '所属项目', value: 'projects' },
              ]}
            />
          </Form.Item>
          <Form.Item
            label="新经验卡默认有效天数"
            name="experienceCardLifetimeDays"
            rules={[{ required: true, type: 'integer', min: 7, max: 365 }]}
          >
            <InputNumber min={7} max={365} precision={0} />
          </Form.Item>
          <Form.Item
            label="允许新经验卡长期有效"
            name="allowPermanentExperienceCards"
            valuePropName="checked"
          >
            <Switch />
          </Form.Item>
          <Form.Item
            label="新本机委托超时（小时）"
            name="delegationLifetimeHours"
            extra="保存后适用于新创建的委托。"
            rules={[{ required: true, type: 'integer', min: 1, max: 72 }]}
          >
            <InputNumber min={1} max={72} precision={0} />
          </Form.Item>
          <Typography.Title level={5}>问询治理</Typography.Title>
          <Row gutter={16}>
            {inquiryFields.map((field) => (
              <Col key={field.name} xs={24} md={12}>
                <Form.Item
                  label={field.label}
                  name={field.name}
                  extra={'hint' in field ? field.hint : undefined}
                  rules={[{ required: true, type: 'integer', min: field.min, max: field.max }]}
                >
                  <InputNumber min={field.min} max={field.max} precision={0} />
                </Form.Item>
              </Col>
            ))}
          </Row>
          <Button type="primary" htmlType="submit" disabled={!editable} loading={save.isPending}>
            保存助手互联设置
          </Button>
        </Form>
      )}
    </SettingsCard>
  )
}
