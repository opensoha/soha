import { useState } from 'react'
import { Alert, Button, Form, Input, Modal, Select, Space, Switch, Typography } from 'antd'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { deliveryMutations } from '../mutations'
import type { DeliveryTrigger, DeliveryTriggerInput } from '../types'
import { useUnsavedDocument } from '../documents/use-unsaved-document'
import type { TriggerTarget } from './manager'
import { calendarInput, calendarInstant } from './model'

interface TriggerForm {
  name: string
  enabled: boolean
  type: DeliveryTrigger['type']
  serviceAccountToken?: string
  webhookSigningSecret?: string
  webhookRef?: string
  timeZone: string
  cron?: string
  calendar?: string[]
  excludedDates?: string[]
}

const refKey = (ref: TriggerTarget['webhookRefs'][number]) =>
  JSON.stringify([ref.repositoryId, ref.refType, ref.refValue])

export function TriggerEditor({
  target,
  item,
  onClose,
}: {
  target: TriggerTarget
  item?: DeliveryTrigger
  onClose: () => void
}) {
  const [form] = Form.useForm<TriggerForm>()
  const [dirty, setDirty] = useState(false)
  const [error, setError] = useState('')
  useUnsavedDocument(dirty)
  const client = useQueryClient()
  const save = useMutation(deliveryMutations.triggers.save(client))
  const initialZone = item?.schedule?.timeZone || Intl.DateTimeFormat().resolvedOptions().timeZone
  const type =
    Form.useWatch('type', form) ??
    item?.type ??
    (target.targetKind === 'workflow' ? 'schedule' : 'poll')
  const zone = Form.useWatch('timeZone', form) || initialZone
  const refs = [...new Map(target.webhookRefs.map((ref) => [refKey(ref), ref])).values()]
  const close = () => {
    if (!save.isPending && (!dirty || window.confirm('放弃未保存的触发器配置？'))) onClose()
  }
  const submit = async () => {
    setError('')
    try {
      const values = await form.validateFields()
      const common = {
        expectedRevision: item?.revision ?? 0,
        name: values.name,
        enabled: values.enabled,
        targetKind: target.targetKind,
        targetId: target.targetId,
        ...(target.workflowVersion ? { workflowVersion: target.workflowVersion } : {}),
        ...(values.serviceAccountToken ? { serviceAccountToken: values.serviceAccountToken } : {}),
      }
      let input: DeliveryTriggerInput
      if (values.type === 'webhook') {
        const webhook = refs.find((ref) => refKey(ref) === values.webhookRef)
        if (!webhook) throw new Error('请选择当前目标中明确指定的分支或标签。')
        input = {
          ...common,
          type: 'webhook',
          webhook,
          ...(values.webhookSigningSecret
            ? { webhookSigningSecret: values.webhookSigningSecret }
            : {}),
        }
      } else {
        const runAt =
          values.type === 'schedule'
            ? (values.calendar ?? []).map((value) => calendarInstant(value, values.timeZone))
            : []
        if (!values.cron?.trim() && !runAt.length)
          throw new Error('请填写定时规则或添加发布日历时间。')
        input = {
          ...common,
          type: values.type,
          schedule: {
            timeZone: values.timeZone,
            ...(values.cron?.trim()
              ? {
                  cron: values.cron.trim(),
                  ...(runAt.length ? { runAt: [...new Set(runAt)] } : {}),
                }
              : { runAt: [...new Set(runAt)] }),
            excludedDates: [...new Set(values.excludedDates ?? [])],
          },
        }
      }
      await save.mutateAsync({ id: item?.id, input })
      form.resetFields()
      save.reset()
      onClose()
    } catch (failure) {
      if (failure instanceof Error) setError(failure.message)
    }
  }
  return (
    <Modal
      open
      title={item ? '配置触发器' : '添加触发器'}
      width={720}
      okText="保存触发器"
      confirmLoading={save.isPending}
      onOk={() => void submit()}
      onCancel={close}
    >
      {error ? <Alert type="error" title="触发器保存失败" description={error} /> : null}
      {target.targetKind === 'workflow' ? (
        <Typography.Paragraph>
          目标版本：v{target.workflowVersion}
          {item?.workflowVersion && item.workflowVersion !== target.workflowVersion
            ? `（当前触发器为 v${item.workflowVersion}，保存后更新）`
            : ''}
        </Typography.Paragraph>
      ) : null}
      <Form
        form={form}
        layout="vertical"
        disabled={save.isPending}
        onValuesChange={() => setDirty(true)}
        initialValues={{
          name: item?.name ?? '',
          enabled: item?.enabled ?? true,
          type: item?.type ?? (target.targetKind === 'workflow' ? 'schedule' : 'poll'),
          timeZone: initialZone,
          cron:
            item?.schedule?.cron ?? (target.targetKind === 'template_source' ? '*/5 * * * *' : ''),
          calendar: item?.schedule?.runAt?.map((at) => calendarInput(at, initialZone)) ?? [],
          excludedDates: item?.schedule?.excludedDates ?? [],
          webhookRef: item?.webhook ? refKey(item.webhook) : refs[0] ? refKey(refs[0]) : undefined,
        }}
      >
        <Form.Item
          name="name"
          label="名称"
          rules={[{ required: true, whitespace: true, max: 200 }]}
        >
          <Input />
        </Form.Item>
        <Form.Item name="type" label="触发方式">
          <Select
            options={[
              {
                value: target.targetKind === 'workflow' ? 'schedule' : 'poll',
                label: target.targetKind === 'workflow' ? '定时 / 发布日历' : '轮询同步',
              },
              { value: 'webhook', label: 'Git Webhook', disabled: !refs.length },
            ]}
          />
        </Form.Item>
        <Form.Item
          name="serviceAccountToken"
          label={item ? `更换执行令牌（当前：${item.serviceAccountName}）` : '服务账号令牌'}
          rules={[{ required: !item, max: 8192 }]}
          tooltip="使用服务账号的令牌。执行前会重新检查配置者和服务账号的当前权限；令牌撤销后停止后续派发。编辑时留空保留。"
        >
          <Input.Password autoComplete="new-password" />
        </Form.Item>
        {type === 'webhook' ? (
          <>
            <Form.Item
              name="webhookRef"
              label="触发仓库与引用"
              rules={[{ required: true }]}
              tooltip="需使用 GitLab 19+ Standard Webhooks 签名。来源同步使用来源引用，工作流使用构建目标中明确选择的引用。"
            >
              <Select
                options={refs.map((ref) => ({
                  value: refKey(ref),
                  label: `${ref.repositoryId} · ${ref.refType === 'branch' ? '分支' : '标签'} ${ref.refValue}`,
                }))}
              />
            </Form.Item>
            <Form.Item
              name="webhookSigningSecret"
              label="Webhook 签名令牌"
              rules={[{ required: !item?.signingSecretConfigured, max: 2000 }]}
              tooltip="填入 GitLab webhook 的 whsec_ 签名令牌。更换后旧签名立即失效；编辑时留空保留。"
            >
              <Input.Password autoComplete="new-password" />
            </Form.Item>
            {item ? (
              <Typography.Paragraph
                copyable={{
                  text: `${window.location.origin}/api/v1/delivery/triggers/${encodeURIComponent(item.id)}/webhook`,
                }}
              >{`${window.location.origin}/api/v1/delivery/triggers/${encodeURIComponent(item.id)}/webhook`}</Typography.Paragraph>
            ) : (
              <Typography.Paragraph type="secondary">
                保存后在配置中复制 Webhook 地址。
              </Typography.Paragraph>
            )}
          </>
        ) : (
          <>
            <Form.Item
              name="timeZone"
              label="时区"
              rules={[{ required: true }]}
              tooltip="定时规则、日历时间和排除日期均以此时区解释。错过的分钟不会补跑，夏令时重复分钟只执行一次。"
            >
              <Input placeholder="Asia/Shanghai" />
            </Form.Item>
            <Form.Item
              name="cron"
              label="定时规则"
              rules={[{ required: type === 'poll', max: 200 }]}
              tooltip="五段：分钟 小时 日期 月份 星期。支持 *、逗号列表和 */步长；星期日为 0。例：0 9 * * 1,2,3,4,5。"
            >
              <Input placeholder={type === 'poll' ? '*/5 * * * *' : '0 9 * * 1,2,3,4,5'} />
            </Form.Item>
            {type === 'schedule' ? (
              <Form.List name="calendar">
                {(fields, { add, remove }) => (
                  <Form.Item label={`发布日历 · ${zone}`}>
                    <Space orientation="vertical" style={{ width: '100%' }}>
                      {fields.map((field) => (
                        <Space key={field.key}>
                          <Form.Item name={field.name} noStyle rules={[{ required: true }]}>
                            <Input
                              type="datetime-local"
                              step={60}
                              aria-label={`发布时间 ${field.name + 1}`}
                            />
                          </Form.Item>
                          <Button
                            onClick={() => remove(field.name)}
                            aria-label={`移除发布时间 ${field.name + 1}`}
                          >
                            移除
                          </Button>
                        </Space>
                      ))}
                      <Button disabled={fields.length >= 100} onClick={() => add('')}>
                        添加发布时间
                      </Button>
                    </Space>
                  </Form.Item>
                )}
              </Form.List>
            ) : null}
            <Form.List name="excludedDates">
              {(fields, { add, remove }) => (
                <Form.Item label="排除日期">
                  <Space orientation="vertical" style={{ width: '100%' }}>
                    {fields.map((field) => (
                      <Space key={field.key}>
                        <Form.Item name={field.name} noStyle rules={[{ required: true }]}>
                          <Input type="date" aria-label={`排除日期 ${field.name + 1}`} />
                        </Form.Item>
                        <Button
                          onClick={() => remove(field.name)}
                          aria-label={`移除排除日期 ${field.name + 1}`}
                        >
                          移除
                        </Button>
                      </Space>
                    ))}
                    <Button disabled={fields.length >= 366} onClick={() => add('')}>
                      添加排除日期
                    </Button>
                  </Space>
                </Form.Item>
              )}
            </Form.List>
          </>
        )}
        <Form.Item name="enabled" label="启用" valuePropName="checked">
          <Switch />
        </Form.Item>
      </Form>
    </Modal>
  )
}
