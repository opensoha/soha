import { Alert, Button, Form, Input, InputNumber, Select, Switch } from 'antd'
import { PlusOutlined, DeleteOutlined } from '@ant-design/icons'
import { useQuery } from '@tanstack/react-query'
import type {
  NetworkEndpointResponsePolicyConfiguration,
  NetworkEndpointResponseStep,
} from '@opensoha/contracts/gen/ts/sohaapi'
import { useI18n } from '@/i18n'
import { readableQueryData } from '@/utils/query-feedback'
import { networkAccessQueries } from '../queries'

export const responsePolicyDefaults: NetworkEndpointResponsePolicyConfiguration = {
  name: '',
  enabled: false,
  siteIds: [],
  nasBindingIds: [],
  deviceIds: [],
  subjects: { users: [], teams: [], tags: [] },
  signal: 'service_unavailable',
  requireHealthyForAdmission: false,
  heartbeatIntervalSeconds: 30,
  missedHeartbeats: 2,
  gracePeriodSeconds: 30,
  accountingFreshnessSeconds: 120,
  restoreHeartbeats: 3,
  restoreObservationSeconds: 60,
  autoRestore: false,
  steps: [{ action: 'record', afterSeconds: 0 }],
  failureAction: 'record',
}

export function EndpointResponsePolicyFields({ can }: { can: (key: string) => boolean }) {
  const { localeCode } = useI18n()
  const text = (zh: string, en: string) => (localeCode === 'zh_CN' ? zh : en)
  const form = Form.useFormInstance<NetworkEndpointResponsePolicyConfiguration>()
  const steps: NetworkEndpointResponseStep[] = Form.useWatch('steps', form) ?? []
  const sites = useQuery(
    networkAccessQueries.sites({ limit: 200 }, can('network_access.sites.view')),
  )
  const devices = useQuery(
    networkAccessQueries.devices({ limit: 200 }, can('network_access.endpoint_devices.view')),
  )
  const bindings = useQuery(
    networkAccessQueries.nasBindings({ limit: 200 }, can('network_access.sites.view')),
  )
  const siteChoices = can('network_access.sites.view') ? readableQueryData(sites) : undefined
  const bindingChoices = can('network_access.sites.view') ? readableQueryData(bindings) : undefined
  const deviceChoices = can('network_access.endpoint_devices.view')
    ? readableQueryData(devices)
    : undefined
  return (
    <>
      <Form.Item
        name="name"
        label={text('策略名称', 'Policy name')}
        rules={[{ required: true, whitespace: true, max: 200 }]}
      >
        <Input maxLength={200} />
      </Form.Item>
      <h3>{text('目标范围', 'Target scope')}</h3>
      <Form.Item
        name="siteIds"
        label={text('站点', 'Sites')}
        rules={[{ required: true, type: 'array', min: 1, max: 128 }]}
      >
        <Select
          mode="tags"
          maxCount={128}
          showSearch={{ optionFilterProp: 'label' }}
          loading={sites.isFetching}
          options={siteChoices?.map((item) => ({ value: item.id, label: item.name }))}
        />
      </Form.Item>
      <Form.Item
        name="nasBindingIds"
        label={text(
          '网络设备（留空匹配站点内全部设备）',
          'NAS bindings (empty matches all in the selected sites)',
        )}
      >
        <Select
          mode="tags"
          maxCount={128}
          showSearch={{ optionFilterProp: 'label' }}
          loading={bindings.isFetching}
          options={bindingChoices?.map((item) => ({ value: item.id, label: item.name }))}
        />
      </Form.Item>
      <Form.Item
        name="deviceIds"
        label={text(
          '终端（留空匹配全部授权终端）',
          'Endpoints (empty matches all authorized endpoints)',
        )}
      >
        <Select
          mode="tags"
          maxCount={512}
          showSearch={{ optionFilterProp: 'label' }}
          loading={devices.isFetching}
          options={deviceChoices?.map((item) => ({ value: item.id, label: item.name }))}
        />
      </Form.Item>
      <div className="endpoint-response-form-grid">
        <Form.Item name={['subjects', 'users']} label={text('用户 ID', 'User IDs')}>
          <Select mode="tags" tokenSeparators={[',']} />
        </Form.Item>
        <Form.Item name={['subjects', 'roles']} label={text('角色 ID', 'Role IDs')}>
          <Select mode="tags" tokenSeparators={[',']} maxCount={128} />
        </Form.Item>
        <Form.Item name={['subjects', 'teams']} label={text('团队 ID', 'Team IDs')}>
          <Select mode="tags" tokenSeparators={[',']} />
        </Form.Item>
        <Form.Item name={['subjects', 'tags']} label={text('用户标签', 'Subject tags')}>
          <Select mode="tags" tokenSeparators={[',']} />
        </Form.Item>
      </div>
      {(sites.isError || bindings.isError || devices.isError) && (
        <Alert
          className="endpoint-response-form-alert"
          type="warning"
          showIcon
          title={text(
            '部分范围选项加载失败，可重试或输入已知 ID',
            'Some scope choices could not load. Retry or enter a known ID.',
          )}
          action={
            <Button
              onClick={() => {
                if (can('network_access.sites.view') && sites.isError) void sites.refetch()
                if (can('network_access.sites.view') && bindings.isError) void bindings.refetch()
                if (can('network_access.endpoint_devices.view') && devices.isError)
                  void devices.refetch()
              }}
            >
              {text('重试', 'Retry')}
            </Button>
          }
        />
      )}
      <h3>{text('失联判断', 'Loss detection')}</h3>
      <Form.Item name="signal" label={text('检测信号', 'Signal')} rules={[{ required: true }]}>
        <Select
          options={[
            {
              value: 'service_unavailable',
              label: text('保护服务不健康或失联', 'Protection service unhealthy or unavailable'),
            },
            { value: 'ui_unavailable', label: text('界面进程失联', 'UI process unavailable') },
          ]}
        />
      </Form.Item>
      <Alert
        className="endpoint-response-form-alert"
        type="info"
        showIcon
        title={text(
          'UI、保护服务和 NAS 会话分别判断',
          'UI, protection service and NAS sessions are evaluated separately',
        )}
        description={text(
          '未知 UI 状态不会触发网络处置；网络动作需要已确认且新鲜的 NAS 会话。',
          'Unknown UI state does not trigger network actions. Network actions require a confirmed, fresh NAS session.',
        )}
      />
      <div className="endpoint-response-form-grid">
        {[
          {
            name: 'heartbeatIntervalSeconds',
            zh: '心跳间隔（秒）',
            en: 'Heartbeat interval (seconds)',
            min: 30,
            max: 300,
          },
          {
            name: 'missedHeartbeats',
            zh: '连续缺失次数',
            en: 'Missed heartbeats',
            min: 1,
            max: 20,
          },
          {
            name: 'gracePeriodSeconds',
            zh: '宽限时间（秒）',
            en: 'Grace period (seconds)',
            min: 0,
            max: 3600,
          },
          {
            name: 'accountingFreshnessSeconds',
            zh: 'NAS 会话有效期（秒）',
            en: 'NAS observation freshness (seconds)',
            min: 30,
            max: 3600,
          },
        ].map((field) => (
          <Form.Item
            key={field.name}
            name={field.name}
            label={text(field.zh, field.en)}
            rules={[{ required: true }]}
          >
            <InputNumber min={field.min} max={field.max} precision={0} style={{ width: '100%' }} />
          </Form.Item>
        ))}
      </div>
      <h3>{text('处置与升级', 'Actions and escalation')}</h3>
      <Form.List
        name="steps"
        rules={[
          {
            validator: (_, values) =>
              values?.length >= 1 && values.length <= 4
                ? Promise.resolve()
                : Promise.reject(new Error(text('需要 1–4 个处置步骤', 'Choose 1–4 action steps'))),
          },
        ]}
      >
        {(fields, { add, remove }, { errors }) => (
          <>
            {fields.map(({ key, name, ...rest }) => (
              <div className="endpoint-response-step" key={key}>
                <Form.Item
                  {...rest}
                  name={[name, 'afterSeconds']}
                  label={text('触发后等待（秒）', 'Delay after trigger (seconds)')}
                  rules={[
                    { required: true },
                    {
                      validator: (_, value) =>
                        name === 0 ||
                        value > form.getFieldValue(['steps', name - 1, 'afterSeconds'])
                          ? Promise.resolve()
                          : Promise.reject(
                              new Error(
                                text('升级时间必须递增', 'Escalation delays must increase'),
                              ),
                            ),
                    },
                  ]}
                >
                  <InputNumber min={0} max={86400} precision={0} style={{ width: '100%' }} />
                </Form.Item>
                <Form.Item
                  {...rest}
                  name={[name, 'action']}
                  label={text('处置', 'Action')}
                  rules={[{ required: true }]}
                >
                  <Select
                    options={[
                      { value: 'record', label: text('仅记录', 'Record') },
                      { value: 'notify', label: text('通知', 'Notify') },
                      { value: 'restrict', label: text('降级', 'Restrict') },
                      { value: 'disconnect', label: text('强制下线', 'Disconnect') },
                    ]}
                  />
                </Form.Item>
                {steps[name]?.action === 'restrict' && (
                  <Form.Item
                    {...rest}
                    name={[name, 'targetAccessProfile']}
                    preserve={false}
                    label={text('接入等级', 'Access profile')}
                    rules={[{ required: true }]}
                  >
                    <Select
                      options={[
                        { value: 'restricted', label: text('受限', 'Restricted') },
                        { value: 'quarantine', label: text('隔离', 'Quarantine') },
                      ]}
                    />
                  </Form.Item>
                )}
                <Button
                  aria-label={text('移除处置步骤', 'Remove action step')}
                  disabled={fields.length === 1}
                  icon={<DeleteOutlined />}
                  onClick={() => remove(name)}
                />
              </div>
            ))}
            <Form.ErrorList errors={errors} />
            <Button
              icon={<PlusOutlined />}
              disabled={fields.length >= 4 || steps.at(-1)?.action === 'disconnect'}
              onClick={() =>
                add({ action: 'record', afterSeconds: (steps.at(-1)?.afterSeconds ?? 0) + 60 })
              }
            >
              {text('增加升级步骤', 'Add escalation step')}
            </Button>
          </>
        )}
      </Form.List>
      <Form.Item
        name="notificationPolicyId"
        label={text('通知策略 ID', 'Notification policy ID')}
        rules={[{ required: steps.some((step) => step.action === 'notify') }]}
        extra={text(
          '复用已授权的通知策略及收件范围。',
          'Uses an authorized notification policy and recipient scope.',
        )}
      >
        <Input maxLength={128} />
      </Form.Item>
      <Form.Item
        name="failureAction"
        label={text(
          '执行不支持、失败或无法验证时',
          'When execution is unsupported, fails or cannot be verified',
        )}
      >
        <Select
          options={[
            {
              value: 'record',
              label: text(
                '保留限制并记录未验证状态',
                'Retain restrictions and record unverified execution',
              ),
            },
            {
              value: 'disconnect',
              label: text('保留限制并尝试强制下线', 'Retain restrictions and attempt disconnect'),
            },
          ]}
        />
      </Form.Item>
      <h3>{text('准入与恢复', 'Admission and recovery')}</h3>
      <Form.Item
        name="requireHealthyForAdmission"
        valuePropName="checked"
        label={text(
          '新认证必须通过连续健康检查',
          'Require continuous health for new authentication',
        )}
      >
        <Switch />
      </Form.Item>
      <div className="endpoint-response-form-grid">
        <Form.Item
          name="restoreHeartbeats"
          label={text('恢复前连续健康心跳', 'Healthy heartbeats before recovery')}
          rules={[{ required: true }]}
        >
          <InputNumber min={1} max={20} precision={0} style={{ width: '100%' }} />
        </Form.Item>
        <Form.Item
          name="restoreObservationSeconds"
          label={text('恢复观察时间（秒）', 'Recovery observation (seconds)')}
          rules={[{ required: true }]}
        >
          <InputNumber min={30} max={3600} precision={0} style={{ width: '100%' }} />
        </Form.Item>
      </div>
      <Form.Item
        name="autoRestore"
        valuePropName="checked"
        label={text(
          '条件满足后自动解除本条限制',
          'Automatically clear this restriction when recovery conditions pass',
        )}
      >
        <Switch />
      </Form.Item>
      <Form.Item
        name="enabled"
        valuePropName="checked"
        label={text('发布后启用', 'Enable after publication')}
      >
        <Switch />
      </Form.Item>
      <Alert
        className="endpoint-response-form-alert"
        showIcon
        type="warning"
        title={text(
          '停用或删除规则不会解除已生效限制',
          'Disabling or deleting a policy does not clear active restrictions',
        )}
        description={text(
          '恢复会重新核实当前授权和网络执行结果；旧会话需要重新认证。',
          'Recovery rechecks current authorization and network execution. Old sessions must authenticate again.',
        )}
      />
    </>
  )
}
