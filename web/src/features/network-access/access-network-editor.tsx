import type {
  NetworkAccessNetworkInput,
  NetworkAccessNetworkRule,
  NetworkNASBinding,
} from '@opensoha/contracts/gen/ts/sohaapi'
import {
  Button,
  Card,
  Divider,
  Drawer,
  Flex,
  Form,
  Input,
  InputNumber,
  Select,
  Space,
  Switch,
} from 'antd'
import { useI18n } from '@/i18n'
import type { ReactNode } from 'react'

type Text = (zh: string, en: string) => string

export const authenticationMethods = [
  { value: 'eap_tls', label: 'EAP-TLS' },
  { value: 'eap_ttls_pap', label: 'EAP-TTLS/PAP' },
]

export function networkKindLabels(text: Text) {
  return {
    standard: text('普通业务网', 'Standard'),
    strict_online: text('严格在线网（待支持）', 'Strict online (pending support)'),
    open_guest_bootstrap: text('开放访客引导网', 'Open guest bootstrap'),
  }
}

export function accessNetworkInput(
  siteId: string,
  values: NetworkAccessNetworkInput,
): NetworkAccessNetworkInput {
  const bootstrap = values.kind === 'open_guest_bootstrap'
  const accessMedium = bootstrap ? 'wifi' : values.accessMedium
  return {
    siteId,
    name: values.name.trim(),
    kind: values.kind,
    enabled: values.kind !== 'strict_online' && values.enabled,
    accessMedium,
    priority: values.priority,
    autoConnect: values.autoConnect,
    nasBindingIds: bootstrap ? [] : values.nasBindingIds,
    ...(accessMedium === 'wifi'
      ? { ssid: values.ssid, bssids: bootstrap ? undefined : values.bssids }
      : {
          portIds: values.portIds,
          portStart: values.portStart ?? undefined,
          portEnd: values.portEnd ?? undefined,
        }),
    ...(bootstrap ? {} : radiusAttributes(values)),
    rules: bootstrap
      ? []
      : (values.rules ?? []).map((rule) => ({
          ...rule,
          ...radiusAttributes(rule),
          scheduleStartMinute: rule.scheduleStartMinute ?? undefined,
          scheduleEndMinute: rule.scheduleEndMinute ?? undefined,
          subjects: {
            ...rule.subjects,
            users: rule.subjects?.users ?? [],
            teams: rule.subjects?.teams ?? [],
            tags: rule.subjects?.tags ?? [],
          },
        })),
  }
}

function radiusAttributes(
  values: Pick<NetworkAccessNetworkRule, 'vlanId' | 'filterId' | 'sessionTimeoutSeconds'>,
) {
  return {
    vlanId: values.vlanId ?? undefined,
    filterId: values.filterId?.trim() || undefined,
    sessionTimeoutSeconds: values.sessionTimeoutSeconds ?? undefined,
  }
}

function RadiusFields({ path, text }: { path: number[]; text: Text }) {
  return (
    <Flex wrap gap="small">
      <Form.Item name={[...path, 'vlanId']} label="VLAN">
        <InputNumber min={1} max={4094} precision={0} />
      </Form.Item>
      <Form.Item name={[...path, 'filterId']} label="Filter-ID">
        <Input maxLength={128} />
      </Form.Item>
      <Form.Item
        name={[...path, 'sessionTimeoutSeconds']}
        label={text('会话超时（秒）', 'Session timeout (s)')}
      >
        <InputNumber min={1} max={86400} precision={0} />
      </Form.Item>
    </Flex>
  )
}

function RuleFields({ index, text }: { index: number; text: Text }) {
  const subjects = [
    ['users', text('用户 ID', 'User IDs')],
    ['teams', text('团队 ID', 'Team IDs')],
    ['tags', text('标签', 'Tags')],
    ['roles', text('角色绑定 ID', 'Role binding IDs')],
  ] as const
  return (
    <>
      <Form.Item
        name={[index, 'effect']}
        label={text('结果', 'Effect')}
        rules={[{ required: true }]}
      >
        <Select
          options={[
            { value: 'allow', label: text('允许', 'Allow') },
            { value: 'deny', label: text('拒绝', 'Deny') },
          ]}
        />
      </Form.Item>
      {subjects.map(([key, label]) => (
        <Form.Item key={key} name={[index, 'subjects', key]} label={label}>
          <Select mode="tags" tokenSeparators={[',']} />
        </Form.Item>
      ))}
      <Form.Item name={[index, 'deviceIds']} label={text('终端 ID', 'Device IDs')}>
        <Select mode="tags" tokenSeparators={[',']} />
      </Form.Item>
      <Form.Item name={[index, 'deviceStatuses']} label={text('终端状态', 'Device status')}>
        <Select
          mode="multiple"
          options={[
            { value: 'pending', label: text('待审核', 'Pending') },
            { value: 'active', label: text('有效', 'Active') },
            { value: 'quarantined', label: text('隔离', 'Quarantined') },
            { value: 'revoked', label: text('已撤销', 'Revoked') },
          ]}
        />
      </Form.Item>
      <Form.Item name={[index, 'postureStatuses']} label={text('合规状态', 'Device posture')}>
        <Select
          mode="multiple"
          options={[
            { value: 'unknown', label: text('未知', 'Unknown') },
            { value: 'compliant', label: text('合规', 'Compliant') },
            { value: 'non_compliant', label: text('不合规', 'Non-compliant') },
          ]}
        />
      </Form.Item>
      <Form.Item
        name={[index, 'authenticationMethods']}
        label={text('认证方式', 'Authentication methods')}
        extra={text(
          '使用终端或合规条件时，仅选择 EAP-TLS。',
          'Select only EAP-TLS for device or posture conditions.',
        )}
      >
        <Select mode="multiple" options={authenticationMethods} />
      </Form.Item>
      <Flex wrap gap="small">
        <Form.Item
          name={[index, 'scheduleStartMinute']}
          label={text('开始分钟（UTC）', 'Start minute (UTC)')}
        >
          <InputNumber min={0} max={1439} precision={0} />
        </Form.Item>
        <Form.Item
          name={[index, 'scheduleEndMinute']}
          label={text('结束分钟（UTC）', 'End minute (UTC)')}
        >
          <InputNumber min={0} max={1439} precision={0} />
        </Form.Item>
      </Flex>
      <RadiusFields path={[index]} text={text} />
    </>
  )
}

export function AccessNetworkEditor({
  siteId,
  value,
  bindings,
  bindingsLoading,
  blocked,
  pending,
  feedback,
  onClose,
  onSave,
  onPreview,
}: {
  siteId: string
  value?: NetworkAccessNetworkInput
  bindings: NetworkNASBinding[]
  bindingsLoading: boolean
  blocked: boolean
  pending: boolean
  feedback: ReactNode
  onClose: () => void
  onSave: (input: NetworkAccessNetworkInput) => void
  onPreview: (input: NetworkAccessNetworkInput) => void
}) {
  const { localeCode } = useI18n()
  const text: Text = (zh, en) => (localeCode === 'zh_CN' ? zh : en)
  const [form] = Form.useForm<NetworkAccessNetworkInput>()
  const initialValues: NetworkAccessNetworkInput = value ?? {
    siteId,
    name: '',
    kind: 'standard',
    accessMedium: 'wifi',
    enabled: false,
    autoConnect: false,
    priority: 100,
    nasBindingIds: [],
    rules: [],
  }
  const kind = Form.useWatch('kind', form) ?? initialValues.kind
  const medium = Form.useWatch('accessMedium', form) ?? initialValues.accessMedium
  const bootstrap = kind === 'open_guest_bootstrap'
  const wifi = bootstrap || medium === 'wifi'
  const nasOptions = bindings
    .filter((binding) => binding.accessMedium === medium)
    .map((binding) => ({
      label:
        binding.status === 'disabled'
          ? `${binding.name} · ${text('已停用', 'Disabled')}`
          : binding.name,
      value: binding.id,
      disabled: binding.status === 'disabled',
    }))
  const kindLabels = networkKindLabels(text)

  return (
    <Drawer
      open
      size="large"
      title={
        value
          ? text('编辑接入网络', 'Edit access network')
          : text('新增接入网络', 'Add access network')
      }
      onClose={pending ? undefined : onClose}
      closable={!pending}
      mask={{ closable: !pending }}
      keyboard={!pending}
      extra={
        <Space wrap>
          <Button
            disabled={pending}
            onClick={() => {
              void form.validateFields().then(
                (values) => onPreview(accessNetworkInput(siteId, values)),
                () => undefined,
              )
            }}
          >
            {text('预览草稿', 'Preview draft')}
          </Button>
          <Button type="primary" loading={pending} disabled={blocked} onClick={() => form.submit()}>
            {text('保存', 'Save')}
          </Button>
        </Space>
      }
    >
      {feedback}
      <Form
        form={form}
        initialValues={initialValues}
        layout="vertical"
        disabled={blocked}
        onFinish={(values) => onSave(accessNetworkInput(siteId, values))}
      >
        <Form.Item
          name="name"
          label={text('名称', 'Name')}
          rules={[{ required: true, whitespace: true }]}
        >
          <Input maxLength={200} />
        </Form.Item>
        <Form.Item
          name="kind"
          label={text('网络类型', 'Network type')}
          rules={[{ required: true }]}
        >
          <Select
            options={Object.entries(kindLabels).map(([value, label]) => ({ value, label }))}
            onChange={(nextKind) => {
              if (nextKind === 'strict_online') form.setFieldValue('enabled', false)
            }}
          />
        </Form.Item>
        {!bootstrap && (
          <Form.Item
            name="accessMedium"
            label={text('接入方式', 'Access medium')}
            rules={[{ required: true }]}
          >
            <Select
              onChange={() => form.setFieldValue('nasBindingIds', [])}
              options={[
                { value: 'wifi', label: 'Wi-Fi' },
                { value: 'wired', label: text('有线', 'Wired') },
              ]}
            />
          </Form.Item>
        )}
        {wifi ? (
          <>
            <Form.Item
              name="ssid"
              label="SSID"
              rules={[
                { required: true },
                {
                  validator: (_, ssid?: string) =>
                    ssid &&
                    (new TextEncoder().encode(ssid).length > 32 ||
                      Array.from(ssid).some(
                        (character) =>
                          character.charCodeAt(0) < 32 || character.charCodeAt(0) === 127,
                      ))
                      ? Promise.reject(
                          new Error(
                            text(
                              'SSID 须为 1–32 字节且不含控制字符',
                              'Use 1–32 bytes without control characters',
                            ),
                          ),
                        )
                      : Promise.resolve(),
                },
              ]}
            >
              <Input />
            </Form.Item>
            {!bootstrap && (
              <Form.Item name="bssids" label="BSSID">
                <Select mode="tags" tokenSeparators={[',']} />
              </Form.Item>
            )}
          </>
        ) : (
          <>
            <Form.Item
              name="portIds"
              label={text('端口 ID', 'Port IDs')}
              extra={text(
                '填写端口 ID、完整范围或两者。',
                'Enter port IDs, a complete range, or both.',
              )}
            >
              <Select mode="tags" tokenSeparators={[',']} />
            </Form.Item>
            <Flex wrap gap="small">
              <Form.Item name="portStart" label={text('起始端口', 'First port')}>
                <InputNumber min={0} max={4294967295} precision={0} />
              </Form.Item>
              <Form.Item name="portEnd" label={text('结束端口', 'Last port')}>
                <InputNumber min={0} max={4294967295} precision={0} />
              </Form.Item>
            </Flex>
          </>
        )}
        {!bootstrap && (
          <Form.Item
            name="nasBindingIds"
            label={text('站点 NAS', 'Site NAS')}
            rules={[{ required: true, type: 'array', min: 1 }]}
          >
            <Select
              mode="multiple"
              loading={bindingsLoading}
              showSearch={{ optionFilterProp: 'label' }}
              options={nasOptions}
            />
          </Form.Item>
        )}
        <Form.Item
          name="enabled"
          label={text('启用网络', 'Enable network')}
          valuePropName="checked"
          extra={
            kind === 'strict_online'
              ? text(
                  '当前版本仅可保存停用的严格在线配置。',
                  'This release supports strict-online configurations as disabled drafts.',
                )
              : kind === 'standard'
                ? text(
                    '首次启用普通业务网后，整个站点按接入网络规则授权，未匹配的接入默认拒绝。保存停用草稿保留站点当前的授权方式。',
                    'Enabling the first standard network activates per-network policy for this site and denies unmatched connections. Saving a disabled draft preserves current admission.',
                  )
                : undefined
          }
        >
          <Switch disabled={kind === 'strict_online'} />
        </Form.Item>
        {!bootstrap && (
          <>
            <Divider titlePlacement="start">{text('谁能连接', 'Who can connect')}</Divider>
            <Form.List name="rules">
              {(fields, { add, remove }) => (
                <Space orientation="vertical" style={{ width: '100%' }}>
                  {text(
                    '条件留空时匹配任意值。拒绝规则优先，至少命中一条允许规则才可连接。',
                    'Empty selectors match any value. Deny rules take precedence, and a matching allow rule is required.',
                  )}
                  {fields.map((field, index) => (
                    <Card
                      key={field.key}
                      size="small"
                      title={`${text('规则', 'Rule')} ${index + 1}`}
                      extra={
                        <Button type="link" danger onClick={() => remove(field.name)}>
                          {text('移除规则', 'Remove rule')}
                        </Button>
                      }
                    >
                      <RuleFields index={field.name} text={text} />
                    </Card>
                  ))}
                  <Button
                    onClick={() =>
                      add({ effect: 'allow', subjects: { users: [], teams: [], tags: [] } })
                    }
                  >
                    {text('添加规则', 'Add rule')}
                  </Button>
                </Space>
              )}
            </Form.List>
          </>
        )}
        <Divider titlePlacement="start">
          {text('推荐与自动连接', 'Recommendation & auto-connect')}
        </Divider>
        <Form.Item
          name="priority"
          label={text('推荐优先级（越小越优先）', 'Priority (lower first)')}
          rules={[{ required: true }]}
        >
          <InputNumber min={1} max={10000} precision={0} />
        </Form.Item>
        <Form.Item
          name="autoConnect"
          label={text('允许自动连接', 'Allow automatic connection')}
          valuePropName="checked"
        >
          <Switch />
        </Form.Item>
        {!bootstrap && (
          <>
            <Divider titlePlacement="start">{text('RADIUS 默认属性', 'RADIUS defaults')}</Divider>
            <RadiusFields path={[]} text={text} />
          </>
        )}
      </Form>
    </Drawer>
  )
}
