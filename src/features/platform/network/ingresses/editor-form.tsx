import { Button, Collapse, Form, Input, Select, Switch, Typography } from 'antd'
import { PlusOutlined, MinusCircleOutlined } from '@ant-design/icons'
import { KeyValueFields } from '@/features/platform/resource-creation/forms'
import { useI18n } from '@/i18n'
import type { IngressEditValues } from './edit-model'

export function IngressEditorForm({
  id,
  value,
  disabled,
  onSubmit,
}: {
  id: string
  value: IngressEditValues
  disabled: boolean
  onSubmit: (v: IngressEditValues) => void
}) {
  const [form] = Form.useForm<IngressEditValues>(),
    { localeCode } = useI18n(),
    zh = localeCode === 'zh_CN'
  const values = Form.useWatch([], { form, preserve: true }) || value
  const backendFields = (prefix: (string | number)[], resourceLabel?: string) =>
    resourceLabel ? (
      <Typography.Text type="secondary">
        {zh
          ? '资源后端（保留原配置，使用 YAML 编辑）'
          : 'Resource backend (preserved; edit in YAML)'}
        : {resourceLabel}
      </Typography.Text>
    ) : (
      <div className="soha-ingress-editor-backend">
        <Form.Item name={[...prefix, 'serviceName']} label="Service" rules={[{ required: true }]}>
          <Input aria-label="后端 Service" />
        </Form.Item>
        <Form.Item
          name={[...prefix, 'servicePort']}
          label={zh ? '端口 / 名称' : 'Port / name'}
          rules={[{ required: true }]}
        >
          <Input aria-label="后端端口" />
        </Form.Item>
      </div>
    )
  return (
    <Form
      id={id}
      form={form}
      initialValues={value}
      layout="vertical"
      disabled={disabled}
      onFinish={() => onSubmit(form.getFieldsValue(true) as IngressEditValues)}
    >
      <Form.Item label="IngressClass" name="className">
        <Input
          placeholder={zh ? '留空由默认控制器处理' : 'Leave empty for the default controller'}
        />
      </Form.Item>
      <Form.List name="rules">
        {(rules, { add, remove }) => (
          <section className="soha-ingress-editor-section">
            <h3>{zh ? 'Host 与路径' : 'Hosts and paths'}</h3>
            {rules.map((rule) => (
              <div className="soha-ingress-editor-rule" key={rule.key}>
                <div className="soha-ingress-editor-host">
                  <Form.Item name={[rule.name, 'host']} label="Host">
                    <Input
                      aria-label="规则 Host"
                      placeholder={zh ? '留空匹配所有 Host' : 'Empty matches all hosts'}
                    />
                  </Form.Item>
                  <Button
                    aria-label={zh ? '删除 Host 规则' : 'Remove host rule'}
                    icon={<MinusCircleOutlined />}
                    onClick={() => remove(rule.name)}
                  />
                </div>
                <Form.List name={[rule.name, 'paths']}>
                  {(paths, { add: addPath, remove: removePath }) => (
                    <>
                      {paths.map((path) => (
                        <div className="soha-ingress-editor-path" key={path.key}>
                          <div className="soha-ingress-editor-path-fields">
                            <Form.Item
                              name={[path.name, 'path']}
                              label={zh ? '路径' : 'Path'}
                              rules={[{ required: true }]}
                            >
                              <Input aria-label="规则路径" />
                            </Form.Item>
                            <Form.Item name={[path.name, 'pathType']} label="PathType">
                              <Select
                                aria-label="路径类型"
                                options={['Prefix', 'Exact', 'ImplementationSpecific'].map(
                                  (value) => ({ value }),
                                )}
                              />
                            </Form.Item>
                          </div>
                          {backendFields(
                            [path.name],
                            values.rules?.[rule.name]?.paths?.[path.name]?.resourceLabel,
                          )}
                          <Button
                            aria-label={zh ? '删除路径' : 'Remove path'}
                            icon={<MinusCircleOutlined />}
                            onClick={() => removePath(path.name)}
                          />
                        </div>
                      ))}
                      <Button
                        icon={<PlusOutlined />}
                        onClick={() =>
                          addPath({
                            path: '/',
                            pathType: 'Prefix',
                            serviceName: '',
                            servicePort: '80',
                          })
                        }
                      >
                        {zh ? '添加路径' : 'Add path'}
                      </Button>
                    </>
                  )}
                </Form.List>
              </div>
            ))}
            <Button
              icon={<PlusOutlined />}
              onClick={() =>
                add({
                  host: '',
                  paths: [{ path: '/', pathType: 'Prefix', serviceName: '', servicePort: '80' }],
                })
              }
            >
              {zh ? '添加 Host 规则' : 'Add host rule'}
            </Button>
          </section>
        )}
      </Form.List>
      <section className="soha-ingress-editor-section">
        <Form.Item
          label={zh ? '默认后端' : 'Default backend'}
          name="defaultEnabled"
          valuePropName="checked"
        >
          <Switch size="small" aria-label={zh ? '启用默认后端' : 'Enable default backend'} />
        </Form.Item>
        {values.defaultEnabled &&
          backendFields(['defaultBackend'], values.defaultBackend?.resourceLabel)}
      </section>
      <Form.List name="tls">
        {(fields, { add, remove }) => (
          <section className="soha-ingress-editor-section">
            <h3>TLS</h3>
            {fields.map((field) => (
              <div className="soha-ingress-editor-tls" key={field.key}>
                <Form.Item name={[field.name, 'hosts']} label="Hosts">
                  <Input aria-label="TLS Hosts" placeholder="example.com, *.example.com" />
                </Form.Item>
                <Form.Item name={[field.name, 'secretName']} label="Secret">
                  <Input aria-label="TLS Secret" />
                </Form.Item>
                <Button
                  aria-label={zh ? '删除 TLS' : 'Remove TLS'}
                  icon={<MinusCircleOutlined />}
                  onClick={() => remove(field.name)}
                />
              </div>
            ))}
            <Button icon={<PlusOutlined />} onClick={() => add({ hosts: '', secretName: '' })}>
              {zh ? '添加 TLS' : 'Add TLS'}
            </Button>
          </section>
        )}
      </Form.List>
      <Collapse
        items={[
          {
            key: 'metadata',
            label: zh ? '标签与注释' : 'Labels and annotations',
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
