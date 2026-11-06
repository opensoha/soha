import { ReadFeedback } from '@/components/request-feedback'
import { hasPermission, usePermissionSnapshot } from '@/features/auth'
import { podQueries } from '@/features/platform/workloads/pods/queries'
import { useI18n } from '@/i18n'
import { toScopeKey } from '@/types'
import { readableQueryData } from '@/utils/query-feedback'
import { useQuery } from '@tanstack/react-query'
import { Alert, AutoComplete, Form, Input, InputNumber, Modal, Select } from 'antd'
import { useState, type ReactNode } from 'react'
import { serviceQueries } from '../services/queries'
import { hasKnownPortConflict, validForwardPort } from './model'
import type { PortForwardDraft, PortForwardSession } from './types'

export type ForwardFields = Omit<PortForwardDraft, 'scope'>
export function CreateForwardForm({
  clusterId,
  currentClusterId,
  namespace,
  items,
  blocked,
  pending,
  submitDisabled,
  feedback,
  onClose,
  onSubmit,
}: {
  clusterId: string
  currentClusterId: string | null | undefined
  namespace: string | null
  items: readonly PortForwardSession[]
  blocked: boolean
  pending: boolean
  submitDisabled: boolean
  feedback: ReactNode
  onClose: () => void
  onSubmit: (values: ForwardFields) => void
}) {
  const { localeCode } = useI18n(),
    zh = localeCode === 'zh_CN'
  const [form] = Form.useForm<ForwardFields>()
  const [targetFocused, setTargetFocused] = useState(false)
  const kind = Form.useWatch('targetKind', form) || 'Pod',
    ns = Form.useWatch('namespace', form) || namespace || 'default',
    local = Form.useWatch('localPort', form)
  const scopeChanged = clusterId !== currentClusterId,
    conflict = hasKnownPortConflict(items, local)
  const permissions = readableQueryData(usePermissionSnapshot())?.data
  const podRead = hasPermission(permissions, 'platform.pods.view'),
    serviceRead = hasPermission(permissions, 'platform.network.services.view')
  const canList =
    targetFocused &&
    !scopeChanged &&
    /^[a-z0-9]([-a-z0-9]*[a-z0-9])?$/.test(ns.trim()) &&
    ns.trim().length <= 63
  const podOptions = podQueries.list(toScopeKey(clusterId, ns.trim())),
    serviceOptions = serviceQueries.list(toScopeKey(clusterId, ns.trim()))
  const pods = useQuery({ ...podOptions, enabled: canList && kind === 'Pod' && podRead })
  const services = useQuery({
    ...serviceOptions,
    enabled: canList && kind === 'Service' && serviceRead,
  })
  const options =
    (kind === 'Pod'
      ? pods.isEnabled && readableQueryData(pods)
      : services.isEnabled && readableQueryData(services)) || []
  const required = { required: true, whitespace: true, message: zh ? '请填写此项' : 'Required' }
  const portRules = [
    { required: true, message: zh ? '请填写端口' : 'Port required' },
    {
      validator: (_: unknown, value: unknown) =>
        validForwardPort(value)
          ? Promise.resolve()
          : Promise.reject(
              new Error(zh ? '端口须为 1–65535 的整数' : 'Use an integer from 1 to 65535'),
            ),
    },
  ]
  return (
    <Modal
      open
      width={640}
      title={zh ? '新建端口转发' : 'New port forward'}
      okText={zh ? '创建会话' : 'Create session'}
      cancelText={zh ? '取消' : 'Cancel'}
      confirmLoading={pending}
      okButtonProps={{ disabled: blocked || submitDisabled || scopeChanged || conflict }}
      cancelButtonProps={{ disabled: blocked }}
      onCancel={() => {
        if (!blocked) onClose()
      }}
      onOk={() => form.submit()}
    >
      <div className="soha-forward-form-content">
        <Alert
          showIcon
          type="info"
          title={zh ? '监听在 Core 所在机器' : 'Listens on the Core host'}
          description={
            zh
              ? '本地端口指 Core 的 127.0.0.1，不是浏览器电脑。创建后以会话状态为准，不自动证明目标服务可达。'
              : 'The local port belongs to Core’s 127.0.0.1, not the browser computer. Session status does not establish target reachability.'
          }
        />
        {scopeChanged && (
          <Alert
            showIcon
            type="warning"
            title={
              zh
                ? '集群已切换，请关闭后重新创建；草稿未转移到新集群。'
                : 'Cluster changed. Close and reopen; this draft will not be sent to the new cluster.'
            }
          />
        )}
        {conflict && (
          <Alert
            showIcon
            type="warning"
            title={
              zh
                ? '当前已读会话中存在相同活跃本地端口，请更换端口。'
                : 'A returned active session already uses this local port. Choose another.'
            }
          />
        )}
        {feedback}
        <Form
          form={form}
          layout="vertical"
          disabled={blocked || submitDisabled || scopeChanged}
          initialValues={{
            targetKind: 'Pod',
            targetName: '',
            namespace: namespace || 'default',
            localPort: 18080,
            remotePort: 80,
          }}
          onValuesChange={(changed) => {
            if ('namespace' in changed || 'targetKind' in changed) {
              form.setFieldValue('targetName', '')
              setTargetFocused(false)
            }
          }}
          onFinish={(values) => {
            if (
              !blocked &&
              !submitDisabled &&
              !scopeChanged &&
              !hasKnownPortConflict(items, values.localPort)
            )
              onSubmit({
                ...values,
                namespace: values.namespace.trim(),
                targetName: values.targetName.trim(),
              })
          }}
        >
          <div className="soha-forward-form-grid">
            <Form.Item
              name="targetKind"
              label={zh ? '目标类型' : 'Target kind'}
              rules={[{ required: true }]}
            >
              <Select
                options={[
                  { value: 'Pod', label: 'Pod' },
                  { value: 'Service', label: 'Service' },
                ]}
              />
            </Form.Item>
            <Form.Item
              name="namespace"
              label={zh ? '命名空间' : 'Namespace'}
              rules={[
                required,
                { max: 63 },
                {
                  pattern: /^[a-z0-9]([-a-z0-9]*[a-z0-9])?$/,
                  message: zh ? '填写有效的命名空间名称' : 'Use a valid namespace name',
                },
              ]}
            >
              <Input autoComplete="off" />
            </Form.Item>
          </div>
          <Form.Item
            name="targetName"
            label={zh ? '目标名称' : 'Target name'}
            rules={[
              required,
              { max: 253 },
              {
                pattern: /^[a-z0-9]([-a-z0-9.]*[a-z0-9])?$/,
                message: zh ? '填写有效资源名称' : 'Use a valid resource name',
              },
            ]}
            extra={
              zh
                ? '可选择本次有权读取的对象，也可手动填写；目标最终由服务端确认。'
                : 'Choose a readable returned object or type its name; the server validates the target.'
            }
          >
            <AutoComplete
              options={options.map((o) => ({ value: o.name }))}
              onFocus={() => setTargetFocused(true)}
              showSearch={{
                filterOption: (input, option) =>
                  String(option?.value || '')
                    .toLowerCase()
                    .includes(input.toLowerCase()),
              }}
            />
          </Form.Item>
          {pods.isEnabled && <ReadFeedback canRead query={pods} label="Pod" />}
          {services.isEnabled && <ReadFeedback canRead query={services} label="Service" />}
          <div className="soha-forward-form-grid">
            <Form.Item
              name="localPort"
              label={zh ? 'Core 本地端口' : 'Core local port'}
              rules={portRules}
            >
              <InputNumber className="soha-forward-port-input" min={1} max={65535} precision={0} />
            </Form.Item>
            <Form.Item
              name="remotePort"
              label={
                kind === 'Service'
                  ? zh
                    ? 'Service 端口'
                    : 'Service port'
                  : zh
                    ? 'Pod 远端端口'
                    : 'Pod remote port'
              }
              rules={portRules}
            >
              <InputNumber className="soha-forward-port-input" min={1} max={65535} precision={0} />
            </Form.Item>
          </div>
        </Form>
      </div>
    </Modal>
  )
}
