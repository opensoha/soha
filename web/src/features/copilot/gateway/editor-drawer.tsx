import type { FormInstance } from 'antd'
import {
  Alert,
  Button,
  Checkbox,
  Divider,
  Drawer,
  Form,
  Input,
  InputNumber,
  Select,
  Space,
} from 'antd'
import { useEffect, type ReactNode } from 'react'
import type {
  AIClient,
  DrawerState,
  GatewayDrawerFormValues,
  GatewayManifest,
  LLMUpstream,
  ServiceAccount,
} from './types'
import {
  approvalRoutingEnabled,
  approvalRoutingModeOptions,
  approvalStrategyOptions,
  clientKindOptions,
  effectOptions,
  gatewayLimitScopeOptions,
  gatewaySecretTypeOptions,
  gatewayTokenPurposeOptions,
  rateLimitModeOptions,
  redactionModeOptions,
  redactionTargetOptions,
  relayProviderKindOptions,
  relayUpstreamStatusOptions,
  riskLevelOptions,
  scopeFieldDefs,
  statusOptions,
  subjectTypeOptions,
} from './types'

function ScopeFields() {
  return (
    <>
      {scopeFieldDefs.map((field) => (
        <Form.Item key={field.name} name={field.name} label={field.label}>
          <Select mode="tags" tokenSeparators={[',', ' ']} placeholder="留空表示不收窄" />
        </Form.Item>
      ))}
    </>
  )
}

function PolicyConditionFields() {
  return (
    <>
      <Divider plain>治理条件</Divider>
      <Form.Item name="rateLimitEnabled" valuePropName="checked">
        <Checkbox>启用 rate limit</Checkbox>
      </Form.Item>
      <Form.Item
        noStyle
        shouldUpdate={(prev, next) =>
          prev.rateLimitEnabled !== next.rateLimitEnabled ||
          prev.rateLimitMode !== next.rateLimitMode
        }
      >
        {({ getFieldValue }) =>
          getFieldValue('rateLimitEnabled') ? (
            <>
              <Form.Item name="rateLimitMode" label="限流算法">
                <Select options={rateLimitModeOptions} />
              </Form.Item>
              <Form.Item name="rateLimitScope" label="限流维度">
                <Select options={gatewayLimitScopeOptions} />
              </Form.Item>
              <Space size={12} style={{ width: '100%' }} align="start">
                <Form.Item name="rateLimitMaxCallsPerMinute" label="每分钟上限" style={{ flex: 1 }}>
                  <InputNumber min={1} precision={0} style={{ width: '100%' }} />
                </Form.Item>
                <Form.Item name="rateLimitMaxCallsPerHour" label="每小时上限" style={{ flex: 1 }}>
                  <InputNumber min={1} precision={0} style={{ width: '100%' }} />
                </Form.Item>
                {getFieldValue('rateLimitMode') === 'gcra' ? (
                  <Form.Item name="rateLimitBurst" label="突发容量" style={{ flex: 1 }}>
                    <InputNumber min={1} precision={0} style={{ width: '100%' }} />
                  </Form.Item>
                ) : null}
              </Space>
            </>
          ) : null
        }
      </Form.Item>

      <Form.Item name="budgetEnabled" valuePropName="checked">
        <Checkbox>启用 budget</Checkbox>
      </Form.Item>
      <Form.Item noStyle shouldUpdate={(prev, next) => prev.budgetEnabled !== next.budgetEnabled}>
        {({ getFieldValue }) =>
          getFieldValue('budgetEnabled') ? (
            <>
              <Form.Item name="budgetScope" label="预算维度">
                <Select options={gatewayLimitScopeOptions} />
              </Form.Item>
              <Space size={12} style={{ width: '100%' }} align="start">
                <Form.Item name="budgetMaxCallsPerDay" label="每日调用" style={{ flex: 1 }}>
                  <InputNumber min={1} precision={0} style={{ width: '100%' }} />
                </Form.Item>
                <Form.Item name="budgetMaxTokensPerDay" label="每日 tokens" style={{ flex: 1 }}>
                  <InputNumber min={1} precision={0} style={{ width: '100%' }} />
                </Form.Item>
                <Form.Item name="budgetMaxCostPerDay" label="每日成本" style={{ flex: 1 }}>
                  <InputNumber min={0} precision={4} style={{ width: '100%' }} />
                </Form.Item>
              </Space>
            </>
          ) : null
        }
      </Form.Item>

      <Form.Item name="redactionEnabled" valuePropName="checked">
        <Checkbox>启用 redaction</Checkbox>
      </Form.Item>
      <Form.Item
        noStyle
        shouldUpdate={(prev, next) => prev.redactionEnabled !== next.redactionEnabled}
      >
        {({ getFieldValue }) =>
          getFieldValue('redactionEnabled') ? (
            <>
              <Form.Item name="redactionMode" label="脱敏模式">
                <Select options={redactionModeOptions} />
              </Form.Item>
              <Form.Item name="redactionTarget" label="脱敏目标">
                <Select options={redactionTargetOptions} />
              </Form.Item>
              <Form.Item name="redactionFields" label="字段路径">
                <Select
                  mode="tags"
                  tokenSeparators={[',', ' ']}
                  placeholder="例如 metadata.apiToken"
                />
              </Form.Item>
              <Form.Item name="redactionAllowFields" label="例外字段">
                <Select mode="tags" tokenSeparators={[',', ' ']} placeholder="例如 search" />
              </Form.Item>
              <Form.Item name="redactionSecretTypes" label="Secret classifiers">
                <Select mode="multiple" options={gatewaySecretTypeOptions} />
              </Form.Item>
              <Form.Item name="redactionValuePatterns" label="值正则">
                <Select mode="tags" tokenSeparators={[',']} placeholder="例如 APP-[0-9]{4}" />
              </Form.Item>
              <Space size={12} style={{ width: '100%' }} align="start">
                <Form.Item name="redactionReplacement" label="替换值" style={{ flex: 1 }}>
                  <Input />
                </Form.Item>
                <Form.Item
                  name="redactionPreserveFormat"
                  valuePropName="checked"
                  label="格式保留"
                  style={{ flex: 1 }}
                >
                  <Checkbox>保留尾部</Checkbox>
                </Form.Item>
              </Space>
              <Form.Item name="outputRedactionFields" label="输出脱敏字段">
                <Select
                  mode="tags"
                  tokenSeparators={[',', ' ']}
                  placeholder="例如 application.buildSources.*.config.token"
                />
              </Form.Item>
              <Form.Item name="outputRedactionSecretTypes" label="输出 Secret classifiers">
                <Select mode="multiple" options={gatewaySecretTypeOptions} />
              </Form.Item>
              <Form.Item name="outputRedactionValuePatterns" label="输出值正则">
                <Select
                  mode="tags"
                  tokenSeparators={[',']}
                  placeholder="例如 token=[A-Za-z0-9_-]{16,}"
                />
              </Form.Item>
              <Space size={12} style={{ width: '100%' }} align="start">
                <Form.Item name="outputRedactionReplacement" label="输出替换值" style={{ flex: 1 }}>
                  <Input />
                </Form.Item>
                <Form.Item
                  name="outputRedactionPreserveFormat"
                  valuePropName="checked"
                  label="输出格式保留"
                  style={{ flex: 1 }}
                >
                  <Checkbox>保留尾部</Checkbox>
                </Form.Item>
              </Space>
            </>
          ) : null
        }
      </Form.Item>
    </>
  )
}

function drawerTitle(drawer: DrawerState | null) {
  if (!drawer) return ''
  switch (drawer.kind) {
    case 'ai-client':
      return drawer.record ? '编辑 AI client' : '新增 AI client'
    case 'relay-upstream':
      return drawer.record ? '编辑上游' : '新增上游'
    case 'relay-route':
      return drawer.record ? '编辑模型路由' : '新增模型路由'
    case 'personal-token':
      return '创建 personal access token'
    case 'service-account':
      return '新增服务账号'
    case 'service-token':
      return '创建服务账号 token'
    case 'service-token-revoke':
      return '吊销服务账号 token'
    case 'tool-grant':
      return '新增 MCP tool grant'
    case 'access-policy':
      return drawer.record ? '编辑 access policy' : '新增 access policy'
    case 'skill-binding':
      return drawer.record ? '编辑 skill binding' : '新增 skill binding'
    default:
      return ''
  }
}

function TokenRelayMetadataFields({
  upstreamOptions,
}: {
  upstreamOptions: Array<{ label: string; value: string }>
}) {
  return (
    <>
      <Divider plain>Token 用途</Divider>
      <Form.Item name="purpose" label="用途" rules={[{ required: true }]}>
        <Select options={gatewayTokenPurposeOptions} />
      </Form.Item>
      <Form.Item name="allowedModels" label="Allowed models">
        <Select
          mode="tags"
          tokenSeparators={[',', ' ']}
          placeholder="例如 gpt-4.1, claude-sonnet-4-5"
        />
      </Form.Item>
      <Form.Item name="allowedProviderKinds" label="Allowed providers">
        <Select mode="tags" tokenSeparators={[',', ' ']} options={relayProviderKindOptions} />
      </Form.Item>
      <Form.Item name="allowedUpstreamIds" label="Allowed upstreams">
        <Select mode="multiple" showSearch options={upstreamOptions} />
      </Form.Item>
      <Form.Item name="allowedIPCIDRs" label="Allowed IP CIDRs">
        <Select mode="tags" tokenSeparators={[',', ' ']} placeholder="例如 10.0.0.0/8" />
      </Form.Item>
      <Form.Item name="allowedTeams" label="Allowed teams">
        <Select mode="tags" tokenSeparators={[',', ' ']} placeholder="例如 platform, ml" />
      </Form.Item>
      <Form.Item name="deniedTeams" label="Denied teams">
        <Select mode="tags" tokenSeparators={[',', ' ']} placeholder="例如 suspended" />
      </Form.Item>
      <Form.Item name="rateLimitProfileId" label="Rate limit profile">
        <Input />
      </Form.Item>
    </>
  )
}

function renderDrawerFields(
  drawer: DrawerState,
  clients: AIClient[],
  manifest?: GatewayManifest,
  upstreams: LLMUpstream[] = [],
  canTestUpstream = false,
  testingUpstream = false,
  onTestUpstream?: () => void,
) {
  const clientOptions = clients.map((item) => ({
    label: `${item.name} (${item.id})`,
    value: item.id,
  }))
  const upstreamOptions = upstreams.map((item) => ({
    label: `${item.name} (${item.id})`,
    value: item.id,
  }))
  const toolOptions = manifest?.tools.map((item) => ({ label: item.name, value: item.name })) ?? []
  const skillOptions =
    manifest?.skills?.map((item) => ({ label: `${item.name} (${item.id})`, value: item.id })) ?? []
  const capabilityOptions =
    manifest?.tools.map((item) => ({ label: item.name, value: item.name })) ?? []
  switch (drawer.kind) {
    case 'ai-client':
      return (
        <>
          <Form.Item name="id" label="Client ID" rules={[{ required: true }]}>
            <Input disabled={!!drawer.record} />
          </Form.Item>
          <Form.Item name="name" label="名称" rules={[{ required: true }]}>
            <Input />
          </Form.Item>
          <Form.Item name="kind" label="类型" rules={[{ required: true }]}>
            <Select options={clientKindOptions} />
          </Form.Item>
          <Form.Item name="status" label="状态" rules={[{ required: true }]}>
            <Select options={statusOptions} />
          </Form.Item>
          <Form.Item name="redirectUris" label="Redirect URIs">
            <Select mode="tags" tokenSeparators={[',', ' ']} />
          </Form.Item>
          <Form.Item name="allowedOrigins" label="Allowed origins">
            <Select mode="tags" tokenSeparators={[',', ' ']} />
          </Form.Item>
        </>
      )
    case 'relay-upstream':
      return (
        <>
          <Form.Item name="name" label="名称" rules={[{ required: true }]}>
            <Input />
          </Form.Item>
          <Form.Item name="providerKind" label="Provider" rules={[{ required: true }]}>
            <Select options={relayProviderKindOptions} />
          </Form.Item>
          <Form.Item name="baseUrl" label="Base URL" rules={[{ required: true }]}>
            <Input />
          </Form.Item>
          <Form.Item name="apiKey" label="API key" rules={[{ required: !drawer.record }]}>
            <Input.Password
              autoComplete="new-password"
              placeholder={drawer.record ? '已配置，留空不更新' : undefined}
            />
          </Form.Item>
          <Form.Item name="apiKeyPrefix" label="Key prefix">
            <Input disabled />
          </Form.Item>
          <Form.Item name="status" label="状态">
            <Select options={relayUpstreamStatusOptions} />
          </Form.Item>
          <Space size={12} style={{ width: '100%' }} align="start">
            <Form.Item name="priority" label="优先级" style={{ flex: 1 }}>
              <InputNumber min={0} precision={0} style={{ width: '100%' }} />
            </Form.Item>
            <Form.Item name="weight" label="权重" style={{ flex: 1 }}>
              <InputNumber min={0} precision={0} style={{ width: '100%' }} />
            </Form.Item>
          </Space>
          <Space size={12} style={{ width: '100%' }} align="start">
            <Form.Item name="timeoutSeconds" label="超时秒数" style={{ flex: 1 }}>
              <InputNumber min={1} precision={0} style={{ width: '100%' }} />
            </Form.Item>
            <Form.Item name="streamTimeoutSeconds" label="流式超时秒数" style={{ flex: 1 }}>
              <InputNumber min={1} precision={0} style={{ width: '100%' }} />
            </Form.Item>
          </Space>
          <Form.Item name="maxConcurrency" label="最大并发">
            <InputNumber min={0} precision={0} style={{ width: '100%' }} />
          </Form.Item>
          <Form.Item
            name="supportedModels"
            label="支持模型"
            extra={
              <Button
                type="link"
                size="small"
                disabled={!canTestUpstream}
                loading={testingUpstream}
                style={{ paddingInline: 0 }}
                onClick={onTestUpstream}
              >
                测试连接并获取模型
              </Button>
            }
          >
            <Select mode="tags" tokenSeparators={[',', ' ']} />
          </Form.Item>
          <Form.Item name="defaultHeadersJson" label="Default headers">
            <Input.TextArea
              autoSize={{ minRows: 3 }}
              placeholder={'{\n  "X-Provider": "soha"\n}'}
            />
          </Form.Item>
          <Form.Item name="proxyUrl" label="Proxy URL">
            <Input />
          </Form.Item>
          <Form.Item name="metadataJson" label="Metadata">
            <Input.TextArea autoSize={{ minRows: 3 }} placeholder="{}" />
          </Form.Item>
        </>
      )
    case 'relay-route':
      return (
        <>
          <Form.Item name="id" label="路由 ID">
            <Input disabled={!!drawer.record} placeholder="留空由后端生成" />
          </Form.Item>
          <Form.Item name="publicModel" label="Public model" rules={[{ required: true }]}>
            <Input />
          </Form.Item>
          <Form.Item name="providerKind" label="Provider">
            <Select allowClear options={relayProviderKindOptions} />
          </Form.Item>
          <Form.Item name="upstreamId" label="上游">
            <Select allowClear showSearch options={upstreamOptions} />
          </Form.Item>
          <Form.Item name="upstreamModel" label="Upstream model" rules={[{ required: true }]}>
            <Input />
          </Form.Item>
          <Form.Item name="routeGroup" label="Route group">
            <Input />
          </Form.Item>
          <Space size={12} style={{ width: '100%' }} align="start">
            <Form.Item name="priority" label="优先级" style={{ flex: 1 }}>
              <InputNumber min={0} precision={0} style={{ width: '100%' }} />
            </Form.Item>
            <Form.Item name="weight" label="权重" style={{ flex: 1 }}>
              <InputNumber min={0} precision={0} style={{ width: '100%' }} />
            </Form.Item>
          </Space>
          <Form.Item name="enabled" label="启用">
            <Select
              options={[
                { label: '启用', value: 'true' },
                { label: '禁用', value: 'false' },
              ]}
            />
          </Form.Item>
          <Form.Item name="fallbackPolicyJson" label="Fallback policy">
            <Input.TextArea autoSize={{ minRows: 3 }} placeholder="{}" />
          </Form.Item>
          <Form.Item name="cachePolicyJson" label="Cache policy">
            <Input.TextArea autoSize={{ minRows: 3 }} placeholder="{}" />
          </Form.Item>
          <Form.Item name="transformPolicyJson" label="Transform policy">
            <Input.TextArea autoSize={{ minRows: 3 }} placeholder="{}" />
          </Form.Item>
          <Form.Item name="rateLimitProfileId" label="Rate limit profile">
            <Input />
          </Form.Item>
          <Form.Item name="metadataJson" label="Metadata">
            <Input.TextArea autoSize={{ minRows: 3 }} placeholder="{}" />
          </Form.Item>
        </>
      )
    case 'personal-token':
      return (
        <>
          <Form.Item name="name" label="名称" rules={[{ required: true }]}>
            <Input />
          </Form.Item>
          <Form.Item name="permissionKeys" label="权限 keys">
            <Select mode="tags" tokenSeparators={[',', ' ']} />
          </Form.Item>
          <Form.Item name="scopes" label="Scopes">
            <Select mode="tags" tokenSeparators={[',', ' ']} />
          </Form.Item>
          <Form.Item name="expiresAt" label="过期时间">
            <Input placeholder="RFC3339，例如 2026-06-30T00:00:00Z" />
          </Form.Item>
          <TokenRelayMetadataFields upstreamOptions={upstreamOptions} />
        </>
      )
    case 'service-account':
      return (
        <>
          <Form.Item name="id" label="服务账号 ID">
            <Input placeholder="留空由后端生成" />
          </Form.Item>
          <Form.Item name="name" label="名称" rules={[{ required: true }]}>
            <Input />
          </Form.Item>
          <Form.Item name="description" label="描述">
            <Input.TextArea autoSize={{ minRows: 2 }} />
          </Form.Item>
          <Form.Item name="status" label="状态">
            <Select options={statusOptions} />
          </Form.Item>
          <Form.Item name="roleIds" label="角色">
            <Select mode="tags" tokenSeparators={[',', ' ']} />
          </Form.Item>
          <Form.Item name="teamIds" label="组织">
            <Select mode="tags" tokenSeparators={[',', ' ']} />
          </Form.Item>
          <Form.Item name="scopeGrantIds" label="Scope grants">
            <Select mode="tags" tokenSeparators={[',', ' ']} />
          </Form.Item>
        </>
      )
    case 'service-token':
      return (
        <>
          <Alert
            type="info"
            showIcon
            title={`服务账号：${(drawer.record as ServiceAccount).name}`}
            style={{ marginBottom: 12 }}
          />
          <Form.Item name="name" label="名称" rules={[{ required: true }]}>
            <Input />
          </Form.Item>
          <Form.Item name="permissionKeys" label="权限 keys">
            <Select mode="tags" tokenSeparators={[',', ' ']} />
          </Form.Item>
          <Form.Item name="scopes" label="Scopes">
            <Select mode="tags" tokenSeparators={[',', ' ']} />
          </Form.Item>
          <Form.Item name="expiresAt" label="过期时间">
            <Input placeholder="RFC3339，例如 2026-06-30T00:00:00Z" />
          </Form.Item>
          <TokenRelayMetadataFields upstreamOptions={upstreamOptions} />
        </>
      )
    case 'service-token-revoke':
      return (
        <Form.Item name="tokenId" label="Token ID" rules={[{ required: true }]}>
          <Input />
        </Form.Item>
      )
    case 'tool-grant':
      return (
        <>
          <Form.Item name="subjectType" label="Subject 类型" rules={[{ required: true }]}>
            <Select options={subjectTypeOptions} />
          </Form.Item>
          <Form.Item name="subjectId" label="Subject ID" rules={[{ required: true }]}>
            <Input />
          </Form.Item>
          <Form.Item name="aiClientId" label="AI client">
            <Select allowClear options={clientOptions} />
          </Form.Item>
          <Form.Item name="toolName" label="Tool" rules={[{ required: true }]}>
            <Select showSearch options={toolOptions} />
          </Form.Item>
          <Form.Item name="effect" label="Effect" rules={[{ required: true }]}>
            <Select options={effectOptions} />
          </Form.Item>
          <Form.Item name="riskLevel" label="Risk">
            <Select options={riskLevelOptions} />
          </Form.Item>
          <Form.Item name="permissionKeys" label="额外权限 keys">
            <Select mode="tags" tokenSeparators={[',', ' ']} />
          </Form.Item>
          <Form.Item name="requiresApproval" label="需要审批">
            <Select
              options={[
                { label: '否', value: 'false' },
                { label: '是', value: 'true' },
              ]}
            />
          </Form.Item>
          <ScopeFields />
        </>
      )
    case 'access-policy':
      return (
        <>
          <Form.Item name="name" label="名称" rules={[{ required: true }]}>
            <Input />
          </Form.Item>
          <Form.Item name="description" label="描述">
            <Input.TextArea autoSize={{ minRows: 2 }} />
          </Form.Item>
          <Form.Item name="enabled" label="启用">
            <Select
              options={[
                { label: '启用', value: 'true' },
                { label: '禁用', value: 'false' },
              ]}
            />
          </Form.Item>
          <Form.Item name="subjectType" label="Subject 类型" rules={[{ required: true }]}>
            <Select options={subjectTypeOptions} />
          </Form.Item>
          <Form.Item name="subjectId" label="Subject ID" rules={[{ required: true }]}>
            <Input />
          </Form.Item>
          <Form.Item name="aiClientId" label="AI client">
            <Select allowClear options={clientOptions} />
          </Form.Item>
          <Form.Item name="effect" label="Effect" rules={[{ required: true }]}>
            <Select options={effectOptions} />
          </Form.Item>
          <Form.Item name="toolPatterns" label="Tool patterns">
            <Select mode="tags" tokenSeparators={[',', ' ']} options={toolOptions} />
          </Form.Item>
          <Form.Item name="skillIds" label="Skills">
            <Select mode="multiple" options={skillOptions} />
          </Form.Item>
          <Form.Item name="riskLevels" label="Risk levels">
            <Select mode="multiple" options={riskLevelOptions} />
          </Form.Item>
          <Form.Item name="approvalMode" label="审批策略">
            <Select options={approvalStrategyOptions} />
          </Form.Item>
          <Form.Item noStyle shouldUpdate={(prev, next) => prev.approvalMode !== next.approvalMode}>
            {({ getFieldValue }) =>
              approvalRoutingEnabled(getFieldValue('approvalMode')) ? (
                <>
                  <Divider plain>审批路由</Divider>
                  <Form.Item name="approvalPolicyRef" label="Gateway approval policy ref">
                    <Input placeholder="例如 gateway-standard" />
                  </Form.Item>
                  <Form.Item name="approvalRoutingMode" label="审批模式">
                    <Select options={approvalRoutingModeOptions} />
                  </Form.Item>
                  <Form.Item name="approvalApproverUsers" label="候选用户">
                    <Select mode="tags" tokenSeparators={[',', ' ']} />
                  </Form.Item>
                  <Form.Item name="approvalApproverRoles" label="候选角色">
                    <Select mode="tags" tokenSeparators={[',', ' ']} />
                  </Form.Item>
                  <Form.Item name="approvalApproverTeams" label="候选组织">
                    <Select mode="tags" tokenSeparators={[',', ' ']} />
                  </Form.Item>
                  <Form.Item name="approvalOnCallRef" label="On-call ref">
                    <Input placeholder="例如 sre-primary" />
                  </Form.Item>
                  <Form.Item name="approvalRequiredApprovals" label="最少审批人数">
                    <InputNumber min={1} precision={0} style={{ width: '100%' }} />
                  </Form.Item>
                  <Space size={12} style={{ width: '100%' }} align="start">
                    <Form.Item
                      name="approvalChangeWindowStartsAt"
                      label="窗口开始"
                      style={{ flex: 1 }}
                    >
                      <Input placeholder="2026-06-01T09:00:00Z" />
                    </Form.Item>
                    <Form.Item
                      name="approvalChangeWindowEndsAt"
                      label="窗口结束"
                      style={{ flex: 1 }}
                    >
                      <Input placeholder="2026-06-01T18:00:00Z" />
                    </Form.Item>
                  </Space>
                  <Form.Item name="approvalChangeWindowTimezone" label="窗口时区">
                    <Input placeholder="例如 Asia/Shanghai" />
                  </Form.Item>
                </>
              ) : null
            }
          </Form.Item>
          <ScopeFields />
          <PolicyConditionFields />
        </>
      )
    case 'skill-binding':
      return (
        <>
          <Form.Item name="subjectType" label="Subject 类型" rules={[{ required: true }]}>
            <Select options={subjectTypeOptions} />
          </Form.Item>
          <Form.Item name="subjectId" label="Subject ID" rules={[{ required: true }]}>
            <Input />
          </Form.Item>
          <Form.Item name="aiClientId" label="AI client">
            <Select allowClear options={clientOptions} />
          </Form.Item>
          <Form.Item name="skillId" label="Skill" rules={[{ required: true }]}>
            <Select showSearch options={skillOptions} />
          </Form.Item>
          <Form.Item name="capabilityRefs" label="Capability refs">
            <Select mode="multiple" options={capabilityOptions} />
          </Form.Item>
          <Form.Item name="enabled" label="启用">
            <Select
              options={[
                { label: '启用', value: 'true' },
                { label: '禁用', value: 'false' },
              ]}
            />
          </Form.Item>
        </>
      )
    default:
      return null
  }
}

export interface GatewayEditorDrawerProps {
  drawer: DrawerState
  form: FormInstance<GatewayDrawerFormValues>
  initialValues: Record<string, unknown>
  clients: AIClient[]
  manifest?: GatewayManifest
  upstreams: LLMUpstream[]
  saving: boolean
  disabled?: boolean
  feedback?: ReactNode
  canTestUpstream: boolean
  testingUpstream: boolean
  onClose: () => void
  onSubmit: (values: GatewayDrawerFormValues) => void
  onTestUpstream: () => void
}

export function GatewayEditorDrawer({
  drawer,
  form,
  initialValues,
  clients,
  manifest,
  upstreams,
  saving,
  disabled = false,
  feedback,
  canTestUpstream,
  testingUpstream,
  onClose,
  onSubmit,
  onTestUpstream,
}: GatewayEditorDrawerProps) {
  useEffect(() => {
    form.resetFields()
    form.setFieldsValue(initialValues)
  }, [form, initialValues])

  return (
    <Drawer
      title={drawerTitle(drawer)}
      size={560}
      open
      closable={!disabled}
      keyboard={!disabled}
      mask={{ closable: !disabled }}
      onClose={onClose}
      footer={
        <Space style={{ justifyContent: 'flex-end', width: '100%' }}>
          <Button disabled={disabled} onClick={onClose}>
            取消
          </Button>
          <Button disabled={disabled} type="primary" loading={saving} onClick={() => form.submit()}>
            保存
          </Button>
        </Space>
      }
    >
      {feedback}
      <Form
        disabled={disabled}
        form={form}
        layout="vertical"
        onFinish={onSubmit}
        initialValues={initialValues}
      >
        {renderDrawerFields(
          drawer,
          clients,
          manifest,
          upstreams,
          canTestUpstream && !disabled,
          testingUpstream,
          onTestUpstream,
        )}
      </Form>
    </Drawer>
  )
}
