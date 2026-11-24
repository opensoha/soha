import { operationModalStyles, viewportModalStyle } from '@/components/modal-styles'
import { SecretRefSelect } from '@/features/secrets'
import { localeText, useI18n, type LocaleCode } from '@/i18n'
import { Button, Form, Input, InputNumber, Modal, Select, Switch, Typography } from 'antd'
import { useState, type ReactNode } from 'react'
import {
  storageFieldError,
  type PluginStorageDriver,
  type StorageField,
} from './plugin-storage-schema'
import type { SystemIntegration } from './types'

export interface PluginStorageFormValues {
  name: string
  enabled: boolean
  configuration?: Record<string, string | null | undefined>
}

const fieldLabels: Record<string, [string, string]> = {
  account: ['存储帐户', 'Storage account'],
  container: ['容器名称', 'Container name'],
  endpoint: ['服务端点', 'Endpoint'],
  prefix: ['对象键前缀', 'Object key prefix'],
  sas_token: ['SAS 令牌引用', 'SAS token reference'],
}

const fieldErrors: Record<NonNullable<ReturnType<typeof storageFieldError>>, [string, string]> = {
  required: ['请填写此字段。', 'Complete this field.'],
  reference: ['请选择平台 Secret 引用。', 'Select a platform secret reference.'],
  option: ['请选择有效选项。', 'Select a valid option.'],
  integer: ['请输入有效整数。', 'Enter a valid integer.'],
  range: ['请输入允许范围内的整数。', 'Enter an integer within the allowed range.'],
  length: ['文本长度不符合驱动要求。', 'Enter text of the length required by the driver.'],
  pattern: ['格式不符合驱动要求。', 'Use the format required by the driver.'],
}

function storageFieldControl(field: StorageField, localeCode: LocaleCode) {
  if (field.secretReference) {
    return (
      <SecretRefSelect placeholder={localeText(localeCode, '选择 Secret', 'Select a secret')} />
    )
  }
  const options = field.options ?? (field.kind === 'boolean' ? ['true', 'false'] : undefined)
  if (options) {
    return (
      <Select
        allowClear={!field.required}
        options={options.map((value) => ({
          value,
          label:
            field.kind === 'boolean'
              ? value === 'true'
                ? localeText(localeCode, '是', 'Yes')
                : localeText(localeCode, '否', 'No')
              : value,
        }))}
      />
    )
  }
  if (field.kind === 'integer') {
    return (
      <InputNumber<string>
        stringMode
        min={field.minimum?.toString()}
        max={field.maximum?.toString()}
        style={{ width: '100%' }}
      />
    )
  }
  return <Input autoComplete="off" />
}

export function PluginStorageConnectionModal({
  drivers,
  editing,
  blocked,
  canSave,
  canTest,
  busy,
  saving,
  testing,
  feedback,
  onClose,
  onSave,
}: {
  drivers: PluginStorageDriver[]
  editing?: SystemIntegration
  blocked: boolean
  canSave: boolean
  canTest: boolean
  busy: boolean
  saving: boolean
  testing: boolean
  feedback: ReactNode
  onClose: () => void
  onSave: (driver: PluginStorageDriver, values: PluginStorageFormValues, test: boolean) => void
}) {
  const { localeCode } = useI18n()
  const [form] = Form.useForm<PluginStorageFormValues>()
  const enabled = Form.useWatch('enabled', form) ?? editing?.enabled ?? true
  const [driverId, setDriverId] = useState(editing?.providerType ?? drivers[0]?.id)
  const driver = drivers.find((item) => item.id === driverId)
  const unableToSave = blocked || !canSave || !driver

  const submit = (values: PluginStorageFormValues, test: boolean) => {
    if (!unableToSave && driver) onSave(driver, values, test && canTest)
  }

  return (
    <Modal
      open
      closable={!busy}
      keyboard={!busy}
      mask={{ closable: false }}
      zIndex={1100}
      style={viewportModalStyle}
      styles={operationModalStyles}
      title={
        editing
          ? localeText(localeCode, '编辑插件存储连接', 'Edit plugin storage connection')
          : localeText(localeCode, '创建插件存储连接', 'Create plugin storage connection')
      }
      footer={[
        <Button key="cancel" disabled={busy} onClick={onClose}>
          {localeText(localeCode, '取消', 'Cancel')}
        </Button>,
        canTest && (
          <Button
            key="test"
            disabled={unableToSave || !enabled}
            loading={testing}
            onClick={() => {
              void form.validateFields().then(
                (values) => submit(values, true),
                ({ errorFields }) => {
                  if (errorFields?.[0]) form.scrollToField(errorFields[0].name, { focus: true })
                },
              )
            }}
          >
            {localeText(localeCode, '保存并测试', 'Save and test')}
          </Button>
        ),
        <Button
          key="save"
          type="primary"
          disabled={unableToSave}
          form="plugin-storage-connection"
          htmlType="submit"
          loading={saving}
        >
          {localeText(localeCode, '保存', 'Save')}
        </Button>,
      ]}
      onCancel={onClose}
    >
      {feedback}
      <Form
        name="plugin-storage-connection"
        form={form}
        layout="vertical"
        initialValues={{ name: editing?.name ?? '', enabled: editing?.enabled ?? true }}
        scrollToFirstError={{ focus: true }}
        disabled={blocked || !canSave}
        onFinish={(values) => submit(values, false)}
      >
        <Form.Item
          label={localeText(localeCode, '存储驱动', 'Storage driver')}
          htmlFor="plugin-storage-driver"
          required
        >
          <Select
            id="plugin-storage-driver"
            aria-required
            disabled={Boolean(editing) || blocked}
            value={driverId}
            options={drivers.map(({ id, name }) => ({ label: name, value: id }))}
            onChange={setDriverId}
          />
        </Form.Item>
        <Form.Item
          name="name"
          label={localeText(localeCode, '连接名称', 'Connection name')}
          rules={[
            {
              required: true,
              whitespace: true,
              message: localeText(localeCode, '请输入连接名称。', 'Enter a connection name.'),
            },
          ]}
        >
          <Input maxLength={200} />
        </Form.Item>
        {editing ? (
          <Typography.Paragraph type="secondary">
            {localeText(
              localeCode,
              '如需变更存储位置或凭据引用，请创建连接。',
              'Create a connection to change the storage location or secret reference.',
            )}
          </Typography.Paragraph>
        ) : (
          driver?.fields.map((field) => {
            const labels = fieldLabels[field.key]
            return (
              <Form.Item
                key={`${driver.id}/${field.key}`}
                name={['configuration', field.key]}
                label={labels ? localeText(localeCode, ...labels) : (field.title ?? field.key)}
                extra={field.description}
                initialValue={field.defaultValue}
                preserve={false}
                validateTrigger="onBlur"
                rules={[
                  {
                    required: field.required,
                    validator: async (_, value: string | null | undefined) => {
                      const error = storageFieldError(field, value)
                      if (error) throw new Error(localeText(localeCode, ...fieldErrors[error]))
                    },
                  },
                ]}
              >
                {storageFieldControl(field, localeCode)}
              </Form.Item>
            )
          })
        )}
        <Form.Item
          name="enabled"
          label={localeText(localeCode, '启用连接', 'Enable connection')}
          valuePropName="checked"
        >
          <Switch />
        </Form.Item>
      </Form>
    </Modal>
  )
}
