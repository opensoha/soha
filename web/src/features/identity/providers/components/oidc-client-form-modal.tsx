import { operationModalStyles } from '@/components/modal-styles'
import { DeleteOutlined, PlusOutlined } from '@ant-design/icons'
import type { FormInstance } from 'antd'
import { Button, Collapse, Form, Input, InputNumber, Modal, Select, Switch } from 'antd'
import type { ReactNode } from 'react'
import { useEffect } from 'react'
import {
  defaultOIDCClientValues,
  oidcClientInputFromValues,
  oidcClientStatusOptions,
  oidcClientTypeOptions,
  oidcClientValuesFor,
  oidcGrantTypeOptions,
  oidcRedirectMatchModeOptions,
  type OIDCClientFormValues,
} from '../provider-form-model'
import type { IdentityOIDCClient, IdentityOIDCClientInput } from '../types'
interface OIDCClientFormModalProps {
  feedback?: ReactNode
  blocked?: boolean
  editing: IdentityOIDCClient | null
  onCancel: () => void
  onSubmit: (input: IdentityOIDCClientInput) => void
  open: boolean
  providerId: string
  submitting: boolean
  title?: string
}
export function OIDCClientFormModal({
  feedback,
  blocked = false,
  editing,
  onCancel,
  onSubmit,
  open,
  providerId,
  submitting,
  title,
}: OIDCClientFormModalProps) {
  const [form] = Form.useForm<OIDCClientFormValues>()
  useEffect(() => {
    if (open)
      form.setFieldsValue(editing ? oidcClientValuesFor(editing) : defaultOIDCClientValues())
  }, [editing, form, open])
  return (
    <Modal
      destroyOnHidden
      footer={null}
      onCancel={onCancel}
      open={open}
      title={title ?? (editing ? '编辑 OIDC client' : '新建 OIDC client')}
      width={840}
      style={{ top: 32 }}
      styles={operationModalStyles}
      closable={!blocked}
      okButtonProps={{ disabled: blocked }}
      cancelButtonProps={{ disabled: blocked }}
    >
      {feedback}

      <Form
        form={form}
        className="soha-identity-provider-form"
        initialValues={defaultOIDCClientValues()}
        layout="vertical"
        onFinish={(values) => onSubmit(oidcClientInputFromValues(providerId, values))}
        disabled={blocked}
      >
        <OIDCClientFields form={form} editing={Boolean(editing)} />

        <div className="soha-identity-provider-form-actions">
          <Button onClick={onCancel} disabled={blocked}>
            取消
          </Button>
          <Button htmlType="submit" loading={submitting} type="primary" disabled={blocked}>
            保存
          </Button>
        </div>
      </Form>
    </Modal>
  )
}
export function OIDCClientFields({
  form,
  editing = false,
}: {
  form: FormInstance<OIDCClientFormValues>
  editing?: boolean
}) {
  const clientType = Form.useWatch('clientType', form)
  return (
    <>
      {!editing && (
        <Form.Item label="应用形态">
          <Select
            defaultValue="web"
            options={[
              { label: 'Web 服务端', value: 'web' },
              { label: 'SPA 单页应用', value: 'spa' },
              { label: '原生应用', value: 'native' },
            ]}
            onChange={(value) =>
              form.setFieldsValue({
                clientType: value === 'web' ? 'confidential' : 'public',
                clientSecret: '',
                requirePkce: true,
                allowedGrantTypes: ['authorization_code'],
                redirectRules: [{ mode: 'strict', value: '' }],
              })
            }
          />
        </Form.Item>
      )}
      <div className="soha-identity-provider-form-grid">
        <Form.Item extra="新建时留空由服务端自动生成" label="Client ID" name="clientId">
          <Input placeholder="留空自动生成" />
        </Form.Item>
        <Form.Item label="Client Secret" name="clientSecret">
          <Input.Password
            disabled={clientType === 'public'}
            placeholder={
              clientType === 'public'
                ? 'Public client 不使用 secret'
                : editing
                  ? '留空表示不轮换'
                  : '留空自动生成'
            }
          />
        </Form.Item>
        <Form.Item label="Client Type" name="clientType">
          <Select
            onChange={(value) => {
              if (value === 'public') {
                form.setFieldsValue({ clientSecret: '', requirePkce: true })
              }
            }}
            options={oidcClientTypeOptions}
          />
        </Form.Item>
        <Form.Item label="Status" name="status">
          <Select options={oidcClientStatusOptions} />
        </Form.Item>
        <Form.Item label="Require PKCE" name="requirePkce" valuePropName="checked">
          <Switch disabled={clientType === 'public'} />
        </Form.Item>
      </div>

      <Form.Item label="重定向 URI/Origin" required>
        <Form.List
          name="redirectRules"
          rules={[
            {
              validator: async (_, rules) => {
                if (rules?.some((rule: { value?: string }) => rule.value?.trim())) return
                throw new Error('至少配置一个重定向规则')
              },
            },
          ]}
        >
          {(fields, { add, remove }, { errors }) => (
            <div className="soha-identity-redirect-rule-list">
              {fields.map((field) => (
                <div className="soha-identity-redirect-rule" key={field.key}>
                  <Form.Item name={[field.name, 'mode']} rules={[{ required: true }]}>
                    <Select aria-label="重定向匹配方式" options={oidcRedirectMatchModeOptions} />
                  </Form.Item>
                  <Form.Item
                    name={[field.name, 'value']}
                    rules={[
                      { required: true, whitespace: true, message: '请输入 URI 或正则表达式' },
                    ]}
                  >
                    <Input placeholder="https://app.example.com/oauth/callback" />
                  </Form.Item>
                  <Button
                    aria-label="删除重定向规则"
                    danger
                    disabled={fields.length === 1}
                    icon={<DeleteOutlined />}
                    onClick={() => remove(field.name)}
                    type="text"
                  />
                </div>
              ))}
              <Button
                icon={<PlusOutlined />}
                onClick={() => add({ mode: 'strict', value: '' })}
                type="link"
              >
                添加条目
              </Button>
              <Form.ErrorList errors={errors} />
            </div>
          )}
        </Form.List>
      </Form.Item>

      <Form.Item label="Allowed scopes" name="allowedScopes">
        <Select mode="tags" tokenSeparators={[',']} />
      </Form.Item>
      <Collapse
        items={[
          {
            key: 'advanced',
            label: '高级客户端设置',
            forceRender: true,
            children: (
              <>
                <Form.Item label="Post Logout Redirect URIs" name="postLogoutRedirectUris">
                  <Select
                    mode="tags"
                    placeholder="https://app.example.com/logout"
                    tokenSeparators={[',']}
                  />
                </Form.Item>
                <div className="soha-identity-provider-form-grid">
                  <Form.Item
                    extra="启用 refresh_token 后，授权请求还需包含 offline_access 才会签发刷新令牌。"
                    label="Allowed grant types"
                    name="allowedGrantTypes"
                  >
                    <Select mode="multiple" options={oidcGrantTypeOptions} />
                  </Form.Item>
                </div>
                <div className="soha-identity-provider-form-grid is-three">
                  <Form.Item label="Access token TTL" name="accessTokenTtlSeconds">
                    <InputNumber min={60} precision={0} style={{ width: '100%' }} />
                  </Form.Item>
                  <Form.Item label="ID token TTL" name="idTokenTtlSeconds">
                    <InputNumber min={60} precision={0} style={{ width: '100%' }} />
                  </Form.Item>
                  <Form.Item
                    extra="留空或填 0 使用服务端默认绝对过期时间。"
                    label="Refresh token TTL"
                    name="refreshTokenTtlSeconds"
                  >
                    <InputNumber min={0} precision={0} style={{ width: '100%' }} />
                  </Form.Item>
                </div>
              </>
            ),
          },
        ]}
      />
    </>
  )
}
