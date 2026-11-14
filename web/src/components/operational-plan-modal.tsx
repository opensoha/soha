import { localeText, useI18n } from '@/i18n'
import type { OperationalPlan } from '@opensoha/contracts/gen/ts/sohaapi'
import { Alert, Checkbox, Descriptions, Modal } from 'antd'
import { useEffect, useState, type ReactNode } from 'react'
import { StatusTag } from './status-tag'

interface OperationalPlanModalProps {
  feedback?: ReactNode
  disabled?: boolean
  confirmText?: string
  loading?: boolean
  onCancel: () => void
  onConfirm: () => void
  plan: OperationalPlan | null
  title: string
}

export function OperationalPlanModal({
  feedback,
  disabled,
  confirmText,
  loading,
  onCancel,
  onConfirm,
  plan,
  title,
}: OperationalPlanModalProps) {
  const { localeCode } = useI18n()
  const [approvalConfirmed, setApprovalConfirmed] = useState(false)
  const warnings = plan?.warnings ?? []
  const changes = plan?.changes ?? []
  const resourceUpdate = plan?.kubernetesResourceUpdate
  const approvalRequired = Boolean(plan?.requiresApproval)

  useEffect(() => setApprovalConfirmed(false), [plan])

  const confirmPlan = () => {
    if (disabled || loading || !plan?.ready || (approvalRequired && !approvalConfirmed)) return
    onConfirm()
  }

  return (
    <Modal
      style={{ top: 32, paddingBottom: 0 }}
      styles={{
        container: { display: 'flex', flexDirection: 'column', maxHeight: 'calc(100dvh - 64px)' },
        body: { minHeight: 0, overflowY: 'auto', overscrollBehavior: 'contain' },
        footer: { flexShrink: 0 },
      }}
      destroyOnHidden
      confirmLoading={loading}
      mask={{ closable: false }}
      okButtonProps={{
        'aria-label': confirmText ?? localeText(localeCode, '确认执行', 'Confirm'),
        disabled: disabled || !plan?.ready || (approvalRequired && !approvalConfirmed),
      }}
      okText={confirmText ?? localeText(localeCode, '确认执行', 'Confirm')}
      onCancel={onCancel}
      onOk={confirmPlan}
      open={Boolean(plan)}
      title={title}
      width={720}
    >
      {feedback}
      {plan ? (
        <div className="space-y-4">
          <Descriptions
            bordered
            column={2}
            size="small"
            items={[
              {
                key: 'target',
                label: localeText(localeCode, '目标', 'Target'),
                children: plan.target,
              },
              {
                key: 'risk',
                label: localeText(localeCode, '风险级别', 'Risk level'),
                children: <StatusTag value={plan.riskLevel} />,
              },
              {
                key: 'capability',
                label: localeText(localeCode, '能力', 'Capability'),
                children: plan.capability,
              },
              {
                key: 'approval',
                label: localeText(localeCode, '确认', 'Confirmation'),
                children: approvalRequired
                  ? localeText(localeCode, '需要显式确认', 'Explicit confirmation required')
                  : localeText(localeCode, '标准确认', 'Standard confirmation'),
              },
            ]}
          />
          {approvalRequired ? (
            <Checkbox
              checked={approvalConfirmed}
              onChange={(event) => setApprovalConfirmed(event.target.checked)}
            >
              {localeText(
                localeCode,
                '我已核对目标、风险与计划变更，并确认继续执行。',
                'I reviewed the target, risk, and planned changes and confirm execution.',
              )}
            </Checkbox>
          ) : null}
          {resourceUpdate ? (
            <Descriptions
              bordered
              column={1}
              size="small"
              title={localeText(localeCode, '字段所有权', 'Field ownership')}
              items={[
                {
                  key: 'field-manager',
                  label: localeText(localeCode, '本次 Manager', 'Current manager'),
                  children: resourceUpdate.fieldManager,
                },
                {
                  key: 'changed-fields',
                  label: localeText(localeCode, '变更字段', 'Changed fields'),
                  children: resourceUpdate.changedFields.join(', ') || '-',
                },
                {
                  key: 'owners',
                  label: localeText(localeCode, '现有 Owner', 'Existing owners'),
                  children:
                    resourceUpdate.owners
                      .map(
                        (owner) =>
                          `${owner.manager} · ${owner.operation} · ${owner.fields.join(', ') || '-'}`,
                      )
                      .join('\n') || '-',
                },
              ]}
            />
          ) : null}
          {resourceUpdate && resourceUpdate.conflicts.length > 0 ? (
            <Alert
              description={
                <ul className="list-disc pl-5">
                  {resourceUpdate.conflicts.map((conflict) => (
                    <li key={`${conflict.field}-${conflict.manager ?? ''}`}>
                      {`${conflict.field}${conflict.manager ? ` · ${conflict.manager}` : ''}: ${conflict.message}`}
                    </li>
                  ))}
                </ul>
              }
              showIcon
              title={localeText(localeCode, '字段所有权冲突', 'Field ownership conflicts')}
              type="error"
            />
          ) : null}
          {warnings.length > 0 ? (
            <Alert
              description={
                <ul className="list-disc pl-5">
                  {warnings.map((warning) => (
                    <li key={warning}>{warning}</li>
                  ))}
                </ul>
              }
              showIcon
              title={localeText(localeCode, '执行提示', 'Execution notes')}
              type="warning"
            />
          ) : null}
          <Descriptions
            bordered
            column={1}
            size="small"
            title={localeText(localeCode, '计划变更', 'Planned changes')}
            items={changes.map((change, index) => ({
              key: `${change.action}-${change.resource}-${index}`,
              label: `${change.action} · ${change.resource}`,
              children: change.summary,
            }))}
          />
          {!plan.ready ? (
            <Alert
              showIcon
              title={localeText(
                localeCode,
                '计划存在阻止项，请返回修改后重新生成。',
                'The plan has blocking issues. Update the form and regenerate it.',
              )}
              type="error"
            />
          ) : null}
        </div>
      ) : null}
    </Modal>
  )
}
