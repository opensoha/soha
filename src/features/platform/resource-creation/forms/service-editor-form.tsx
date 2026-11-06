import { MetadataTag } from '@/components/status-tag'
import { Collapse, Form, Input, Typography } from 'antd'
import { useEffect } from 'react'
import { KeyValueFields } from './field-sections'
import { ServiceFields } from './service-fields'
import type { ResourceFormRendererProps, ServiceFormValues } from './types'

export function ServiceEditorForm({
  id,
  clusterId,
  disabled,
  localeCode,
  onChange,
  onSubmit,
  value,
}: ResourceFormRendererProps<ServiceFormValues> & { id: string }) {
  const [form] = Form.useForm<ServiceFormValues>()
  const zh = localeCode === 'zh_CN'
  useEffect(() => {
    form.setFieldsValue(value)
  }, [form, value])
  return (
    <Form
      id={id}
      form={form}
      initialValues={value}
      layout="vertical"
      disabled={disabled}
      onValuesChange={() => onChange(form.getFieldsValue(true) as ServiceFormValues)}
      onFinish={(values) => onSubmit?.({ ...value, ...values })}
    >
      <Form.Item name="name" hidden>
        <Input disabled />
      </Form.Item>
      <Form.Item name="namespace" hidden>
        <Input disabled />
      </Form.Item>
      <div className="soha-service-editor-identity">
        <Typography.Text type="secondary">{zh ? '命名空间' : 'Namespace'}</Typography.Text>
        <MetadataTag label={value.namespace || '—'} tone="blue" />
        <Typography.Text type="secondary">
          {zh ? '名称与命名空间保持不变' : 'Name and namespace are fixed'}
        </Typography.Text>
      </div>
      <ServiceFields clusterId={clusterId} editor />
      <Collapse
        items={[
          {
            key: 'metadata',
            label: zh
              ? `标签 ${value.labels?.length || 0} · 注释 ${value.annotations?.length || 0}`
              : `Labels ${value.labels?.length || 0} · Annotations ${value.annotations?.length || 0}`,
            forceRender: true,
            children: (
              <>
                <KeyValueFields label="标签" name="labels" />
                <KeyValueFields label="注解" name="annotations" />
              </>
            ),
          },
        ]}
      />
    </Form>
  )
}
