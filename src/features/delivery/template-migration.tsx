import { Alert, Form, Input, Space, Switch, Typography } from 'antd'
import type {
  TemplateMigrationPolicy,
  TemplateMigrationAssessment,
} from '@opensoha/contracts/gen/ts/sohaapi'
import type { DeliveryPlan } from './types'

export function migrationPolicyInput(
  value?: Partial<TemplateMigrationPolicy>,
): TemplateMigrationPolicy | undefined {
  if (!value) return undefined
  return {
    sequential: Boolean(value.sequential),
    irreversible: Boolean(value.irreversible),
    guide: value.guide?.trim() || undefined,
  }
}

export function TemplateMigrationFields({ path = ['migrationPolicy'] }: { path?: string[] }) {
  const form = Form.useFormInstance()
  const policy = Form.useWatch(path, form) as TemplateMigrationPolicy | undefined
  const declared = policy?.sequential || policy?.irreversible
  return (
    <>
      <Form.Item
        name={[...path, 'sequential']}
        label="必须按相邻版本发布"
        valuePropName="checked"
        extra="普通审批不能绕过该边界；配置已采用不代表迁移已执行。"
      >
        <Switch aria-label="必须按相邻版本发布" />
      </Form.Item>
      <Form.Item
        name={[...path, 'irreversible']}
        label="不可逆数据变更"
        valuePropName="checked"
        extra="需要Plan审批，禁止跨此边界普通回退。"
      >
        <Switch aria-label="不可逆数据变更" />
      </Form.Item>
      <Form.Item
        name={[...path, 'guide']}
        label="迁移与恢复指引"
        dependencies={[
          [...path, 'sequential'],
          [...path, 'irreversible'],
        ]}
        required={Boolean(declared)}
        rules={[
          {
            validator: (_, value: string | undefined) => {
              const current = form.getFieldValue(path) as TemplateMigrationPolicy | undefined
              return (current?.sequential || current?.irreversible) && !value?.trim()
                ? Promise.reject(new Error('请填写迁移前提、验证方式和恢复方案'))
                : Promise.resolve()
            },
          },
        ]}
      >
        <Input.TextArea
          aria-label="迁移与恢复指引"
          maxLength={2000}
          showCount
          autoSize={{ minRows: 3, maxRows: 8 }}
          placeholder="填写备份前提、逐版操作、业务验证与恢复方式；系统不会执行此文本。"
        />
      </Form.Item>
    </>
  )
}

export function MigrationAssessment({ value }: { value?: TemplateMigrationAssessment }) {
  if (!value) return null
  return (
    <Alert
      type={value.blocked ? 'error' : value.requiresApproval ? 'warning' : 'info'}
      showIcon
      title={
        value.initialization
          ? `首次发布 → v${value.toVersion} · 待确认运行基线`
          : `迁移基线 ${value.fromVersion ? `v${value.fromVersion}` : '未知'} → v${value.toVersion}`
      }
      description={
        <Space orientation="vertical" size={8}>
          {value.initialization && (
            <Typography.Text>
              仅创建不存在的资源；已有部署但基线不明时仍阻断。执行后核对原始资源 UID
              与健康状态，确认成功后才登记基线。
            </Typography.Text>
          )}
          <Typography.Text>{value.reason || '此区间没有额外迁移要求。'}</Typography.Text>
          {value.steps.map((step) => (
            <div key={`${step.version}:${step.contentDigest}`}>
              <Typography.Text strong>
                v{step.version} ·{' '}
                {[
                  step.policy.sequential && '按相邻版本发布',
                  step.policy.irreversible && '不可逆数据变更',
                ]
                  .filter(Boolean)
                  .join(' · ')}
              </Typography.Text>
              <Typography.Paragraph style={{ whiteSpace: 'pre-wrap', marginBlockEnd: 0 }}>
                {step.policy.guide}
              </Typography.Paragraph>
            </div>
          ))}
        </Space>
      }
    />
  )
}

export function planMigrations(
  plan?: Pick<DeliveryPlan, 'impact' | 'manifestSnapshots' | 'helmSnapshots'> | null,
): TemplateMigrationAssessment[] {
  const adoption = plan?.impact?.templateMigration
  const candidate =
    adoption && typeof adoption === 'object' && 'steps' in adoption && Array.isArray(adoption.steps)
      ? (adoption as TemplateMigrationAssessment)
      : undefined
  return [
    ...(candidate ? [candidate] : []),
    ...(plan?.manifestSnapshots
      ?.map((s) => s.migration)
      .filter((v): v is TemplateMigrationAssessment => Boolean(v)) ?? []),
    ...(plan?.helmSnapshots
      ?.map((s) => s.migration)
      .filter((v): v is TemplateMigrationAssessment => Boolean(v)) ?? []),
  ]
}
