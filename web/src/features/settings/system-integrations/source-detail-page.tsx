import { ManagementState } from '@/components/management-list'
import { ReadFeedback } from '@/components/request-feedback'
import type { StepFormStep } from '@/components/step-form'
import { StepFormModal } from '@/components/step-form-modal'
import { useOperationMutation } from '@/components/use-operation-mutation'
import { hasPermission, usePermissionSnapshot } from '@/features/auth'
import { queryAccessDenied, readableQueryData } from '@/utils/query-feedback'
import { LinkOutlined, ThunderboltOutlined } from '@ant-design/icons'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { Alert, Button, Form, Input, InputNumber, Segmented, Select, Space, Switch } from 'antd'
import { useEffect, useRef, useState } from 'react'
import { useNavigate, useParams, useSearchParams } from 'react-router-dom'
import {
  createGitLabIntegration,
  gitLabFormValues,
  updateGitLabIntegration,
  type GitLabFormValues,
} from './model'
import { systemIntegrationMutations } from './mutations'
import { systemIntegrationQueries } from './queries'
import { SourceConnectionsPage } from './source-list-page'
import './styles.css'
import type { SystemIntegration } from './types'

type SourceConnectionFormValues = GitLabFormValues & {
  providerType: 'gitlab'
}

export function SourceConnectionDetailPage() {
  const navigate = useNavigate()
  const queryClient = useQueryClient()
  const params = useParams<{ integrationId: string }>()
  const [searchParams] = useSearchParams()
  const integrationId = params.integrationId ?? ''
  const isNew = !params.integrationId || integrationId === 'new'
  const permissionQuery = usePermissionSnapshot()
  const permissionSnapshot = readableQueryData(permissionQuery)?.data
  const canView = hasPermission(permissionSnapshot, 'settings.system-integrations.view')
  const canCreate = hasPermission(permissionSnapshot, 'settings.system-integrations.create')
  const canUpdate = hasPermission(permissionSnapshot, 'settings.system-integrations.update')
  const canTest = hasPermission(permissionSnapshot, 'settings.system-integrations.test')
  const canSave = isNew ? canCreate : canUpdate
  const detailQuery = useQuery(systemIntegrationQueries.detail(integrationId, canView && !isNew))
  const createMutation = useOperationMutation(systemIntegrationMutations.create(queryClient), {
    label: ['创建代码源连接', '创建代码源连接'],
    target: (v) => v.name,
  })
  const updateMutation = useOperationMutation(systemIntegrationMutations.update(queryClient), {
    label: ['保存代码源连接', '保存代码源连接'],
    target: (v) => v.id,
  })
  const testMutation = useOperationMutation(systemIntegrationMutations.test(queryClient), {
    label: ['测试代码源连接', '测试代码源连接'],
    target: (id) => id,
    readOnly: true,
    resultError: (r) =>
      r.status === 'failed' ? new Error(r.message || '连接测试失败') : undefined,
    receipt: (r) => ({ message: r.message || '连接测试已返回' }),
  })
  const authorizeOAuthMutation = useOperationMutation(systemIntegrationMutations.authorizeOAuth(), {
    label: ['获取 OAuth 授权入口', '获取 OAuth 授权入口'],
    target: (id) => id,
  })
  const [form] = Form.useForm<SourceConnectionFormValues>()
  const [currentStep, setCurrentStep] = useState(() => (isNew ? 0 : 1))
  const detail = canView ? readableQueryData(detailQuery) : undefined
  const [baseIntegration, setBaseIntegration] = useState<SystemIntegration>()
  const initialized = useRef('')
  const denied =
    !canView ||
    queryAccessDenied(detailQuery) ||
    [createMutation, updateMutation, authorizeOAuthMutation].some(queryAccessDenied)
  const blocked =
    denied ||
    [createMutation, updateMutation, testMutation, authorizeOAuthMutation].some((m) => m.blocked)
  const saving = createMutation.isPending || updateMutation.isPending
  const providerType = Form.useWatch('providerType', form) ?? 'gitlab'
  const authMode = Form.useWatch('authMode', form) ?? 'access_token'
  const configuredAuthMode = detail?.configuration.find((field) => field.key === 'auth_mode')?.value
  const oauthMode = configuredAuthMode === 'oauth'
  const connectionReady = detail?.credentialKeys.includes(oauthMode ? 'access_token' : 'token')

  useEffect(() => {
    if (denied) {
      initialized.current = ''
      form.resetFields()
      setBaseIntegration(undefined)
      return
    }
    const key = isNew ? 'new' : integrationId
    if (initialized.current === key || (!isNew && !detail)) return
    initialized.current = key
    form.resetFields()
    form.setFieldsValue({ providerType: 'gitlab', ...gitLabFormValues(detail) })
    setBaseIntegration(detail)
  }, [detail, form, isNew, integrationId, denied])

  if (!permissionQuery.isLoading && !canView) {
    return (
      <div className="soha-page">
        <ManagementState kind="no-permission" description="当前账号没有查看代码源连接的权限。" />
      </div>
    )
  }

  if (isNew && !permissionQuery.isLoading && !canCreate) {
    return (
      <div className="soha-page">
        <ManagementState kind="no-permission" description="当前账号没有新建代码源连接的权限。" />
      </div>
    )
  }

  const save = (values: SourceConnectionFormValues) => {
    if (!canSave || blocked) return
    if (isNew) {
      createMutation.mutate(createGitLabIntegration(values), {
        onSuccess: () => {
          form.resetFields()
          navigate('/settings/source-control', { state: { savedSource: values.name } })
        },
      })
      return
    }
    if (!baseIntegration) return
    updateMutation.mutate(
      { id: baseIntegration.id, values: updateGitLabIntegration(baseIntegration, values) },
      {
        onSuccess: () => {
          form.resetFields()
          navigate('/settings/source-control', { state: { savedSource: values.name } })
        },
      },
    )
  }

  const steps: StepFormStep[] = [
    {
      title: '选择 Provider',
      fieldNames: ['providerType'],
      children: (
        <div className="soha-system-integration-provider-step">
          <Form.Item
            name="providerType"
            label="Git Provider"
            rules={[{ required: true, message: '请选择 Git Provider' }]}
          >
            <Select
              disabled={!isNew || blocked}
              options={[{ label: 'GitLab', value: 'gitlab' }]}
              placeholder="选择 Git Provider"
            />
          </Form.Item>
        </div>
      ),
    },
    {
      title: '连接配置',
      children: (
        <>
          {searchParams.get('oauth') === 'success' ? (
            <Alert
              className="soha-system-integration-oauth-alert"
              showIcon
              type="success"
              title="GitLab OAuth 授权成功"
            />
          ) : null}
          {searchParams.get('oauth') === 'error' ? (
            <Alert
              className="soha-system-integration-oauth-alert"
              showIcon
              type="error"
              title="GitLab OAuth 授权失败，请重新授权"
            />
          ) : null}
          {!isNew && detail ? (
            <div className="soha-system-integration-tools">
              <Space wrap>
                <Button
                  disabled={blocked || !canTest || !detail.enabled || !connectionReady}
                  icon={<ThunderboltOutlined />}
                  loading={testMutation.isPending}
                  onClick={() => testMutation.mutate(detail!.id)}
                >
                  测试连接
                </Button>
                {oauthMode ? (
                  <Button
                    disabled={
                      blocked || !canUpdate || !detail.credentialKeys.includes('client_secret')
                    }
                    icon={<LinkOutlined />}
                    loading={authorizeOAuthMutation.isPending}
                    onClick={() =>
                      authorizeOAuthMutation.mutate(detail!.id, {
                        onSuccess: (result) => window.location.assign(result.authorizationUrl),
                      })
                    }
                  >
                    {connectionReady ? '重新授权' : '授权 GitLab'}
                  </Button>
                ) : null}
              </Space>
            </div>
          ) : null}
          {providerType === 'gitlab' ? (
            <div className="soha-system-integration-form-grid">
              <Form.Item
                name="name"
                label="连接名称"
                rules={[{ required: true, message: '请输入连接名称' }]}
              >
                <Input maxLength={200} placeholder="公司 GitLab" />
              </Form.Item>
              <Form.Item name="enabled" label="启用" valuePropName="checked">
                <Switch />
              </Form.Item>
              <Form.Item
                name="authMode"
                label="认证方式"
                rules={[{ required: true, message: '请选择认证方式' }]}
              >
                <Segmented
                  disabled={blocked || !canSave}
                  block
                  options={[
                    { label: 'Access Token', value: 'access_token' },
                    { label: 'OAuth Application', value: 'oauth' },
                  ]}
                />
              </Form.Item>
              <Form.Item
                name="baseUrl"
                label="API 地址"
                rules={[
                  { required: true, message: '请输入 GitLab API 地址' },
                  { type: 'url', message: '请输入有效的 URL' },
                ]}
              >
                <Input placeholder="https://gitlab.example.com/api/v4" />
              </Form.Item>
              <Form.Item name="groupId" label="默认 Group ID">
                <Input placeholder="可选，不限制时留空" />
              </Form.Item>
              <Form.Item
                name="perPage"
                label="每页数量"
                rules={[{ required: true, message: '请输入每页数量' }]}
              >
                <InputNumber min={1} max={100} precision={0} style={{ width: '100%' }} />
              </Form.Item>
              <Form.Item
                name="timeout"
                label="请求超时"
                rules={[{ required: true, message: '请输入请求超时' }]}
              >
                <Input placeholder="15s" />
              </Form.Item>
              {authMode === 'access_token' ? (
                <Form.Item
                  name="token"
                  label={
                    detail?.credentialKeys.includes('token')
                      ? '访问令牌（已配置，留空保持不变）'
                      : '访问令牌'
                  }
                  rules={isNew ? [{ required: true, message: '请输入访问令牌' }] : undefined}
                >
                  <Input.Password autoComplete="new-password" placeholder="GitLab access token" />
                </Form.Item>
              ) : (
                <>
                  <Form.Item
                    name="clientId"
                    label="Application ID"
                    rules={[{ required: true, message: '请输入 GitLab Application ID' }]}
                  >
                    <Input autoComplete="off" />
                  </Form.Item>
                  <Form.Item
                    name="clientSecret"
                    label={
                      detail?.credentialKeys.includes('client_secret')
                        ? 'Application Secret（已配置，留空保持不变）'
                        : 'Application Secret'
                    }
                    rules={
                      isNew || !detail?.credentialKeys.includes('client_secret')
                        ? [{ required: true, message: '请输入 GitLab Application Secret' }]
                        : undefined
                    }
                  >
                    <Input.Password autoComplete="new-password" />
                  </Form.Item>
                  <Form.Item
                    name="oauthRedirectUri"
                    label="GitLab Application Redirect URI"
                    rules={[
                      { required: true, message: '请输入 OAuth 回调地址' },
                      { type: 'url', message: '请输入有效的 URL' },
                    ]}
                  >
                    <Input />
                  </Form.Item>
                  <Alert
                    className="soha-system-integration-oauth-alert"
                    showIcon
                    type={connectionReady ? 'success' : 'info'}
                    title={
                      connectionReady ? 'OAuth 已授权' : '保存连接后，点击“授权 GitLab”完成授权'
                    }
                  />
                </>
              )}
              <Form.Item name="description" label="说明">
                <Input.TextArea maxLength={1000} rows={3} />
              </Form.Item>
              <details className="soha-system-integration-oauth-alert">
                <summary>Git 同步安全与 SSH</summary>
                <Form.Item
                  name="gitAllowedEndpoints"
                  label="额外允许的克隆端点"
                  extra="默认允许 API 同源 HTTPS 和同主机 SSH 22 端口；其他主机或端口需填写完整来源，如 ssh://git.example.com:2222。"
                >
                  <Input.TextArea
                    rows={2}
                    maxLength={8192}
                    placeholder="每行一个 HTTPS 或 SSH 来源，不含仓库路径"
                  />
                </Form.Item>
                <Form.Item
                  name="gitAllowedCidrs"
                  label="允许访问的内网 CIDR"
                  extra="公网默认可用；私网需显式授权。回环地址仅接受单地址 /32 或 /128，链路本地地址始终拒绝。"
                >
                  <Input.TextArea rows={2} maxLength={8192} placeholder="10.20.0.0/16" />
                </Form.Item>
                <Form.Item
                  name="gitCaCertificate"
                  label="私有 CA 证书（PEM）"
                  extra="留空时使用系统信任根；始终校验证书与主机名。"
                >
                  <Input.TextArea rows={3} maxLength={8192} spellCheck={false} />
                </Form.Item>
                <Form.Item
                  name="privateKey"
                  label={
                    detail?.credentialKeys.includes('private_key')
                      ? 'SSH 私钥（已配置，留空保持不变）'
                      : 'SSH 私钥'
                  }
                >
                  <Input.TextArea rows={3} autoComplete="off" spellCheck={false} />
                </Form.Item>
                <Form.Item
                  name="knownHosts"
                  label={
                    detail?.credentialKeys.includes('known_hosts')
                      ? 'SSH known_hosts（已配置，留空保持不变）'
                      : 'SSH known_hosts'
                  }
                  extra="SSH 克隆必须提供经管理员核实的主机公钥；不会自动接受新主机密钥。"
                >
                  <Input.TextArea rows={3} autoComplete="off" spellCheck={false} />
                </Form.Item>
              </details>
            </div>
          ) : null}
        </>
      ),
    },
  ]

  return (
    <>
      <SourceConnectionsPage />
      {denied && canView ? (
        <div className="soha-page">
          {createMutation.feedback}
          {updateMutation.feedback}
          {authorizeOAuthMutation.feedback}
          <ReadFeedback query={detailQuery} label="代码源连接" />
          <Button onClick={() => navigate('/settings/source-control')}>返回代码源连接</Button>
        </div>
      ) : null}
      <StepFormModal
        closeDisabled={blocked}
        contentMaxWidth={820}
        current={currentStep}
        disabled={!canSave || blocked || (!isNew && !baseIntegration)}
        feedback={
          <>
            {!isNew ? (
              <Button
                size="small"
                aria-label="刷新当前代码源连接"
                loading={detailQuery.isFetching}
                onClick={() => void detailQuery.refetch()}
              >
                刷新记录
              </Button>
            ) : null}
            <ReadFeedback query={detailQuery} label="代码源连接" canRead={canView && !isNew} />
            {createMutation.feedback}
            {updateMutation.feedback}
            {testMutation.feedback}
            {authorizeOAuthMutation.feedback}
            {baseIntegration && detail && baseIntegration.version !== detail.version ? (
              <Alert
                type="warning"
                showIcon
                title="连接记录已更新，保留当前草稿及原始版本；保存时由服务端检查冲突。"
              />
            ) : null}
          </>
        }
        form={form}
        initialValues={{ providerType: 'gitlab', ...gitLabFormValues() }}
        loading={saving}
        open={!denied}
        steps={steps}
        submitText="保存"
        title={isNew ? '新增 Git' : '编辑 Git 连接'}
        width={900}
        onClose={() => navigate('/settings/source-control')}
        onCurrentChange={setCurrentStep}
        onFinish={save}
      />
    </>
  )
}
