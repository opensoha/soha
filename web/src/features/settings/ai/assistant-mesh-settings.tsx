import { ReadFeedback } from '@/components/request-feedback'
import { useOperationMutation } from '@/components/use-operation-mutation'
import { queryAccessDenied, readableQueryData } from '@/utils/query-feedback'
import type { AssistantMeshSettings as Settings } from '@opensoha/contracts/gen/ts/sohaapi'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { Button, Form, InputNumber, Select, Switch } from 'antd'
import { settingsMutations } from '../mutations'
import { settingsQueries } from '../queries'
import { SettingsCard } from '../shared/components'

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
          initialValues={settings}
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
          <Button type="primary" htmlType="submit" disabled={!editable} loading={save.isPending}>
            保存助手互联设置
          </Button>
        </Form>
      )}
    </SettingsCard>
  )
}
