import { ContextFeedback } from '@/components/context-feedback'
import { ManagementRefreshButton, ManagementState } from '@/components/management-list'
import { operationModalStyles } from '@/components/modal-styles'
import { ReadFeedback } from '@/components/request-feedback'
import { StatusTag } from '@/components/status-tag'
import { useOperationMutation } from '@/components/use-operation-mutation'
import { systemIntegrationQueries, type SystemIntegrationCategory } from '@/features/settings'
import { queryAccessDenied, readableQueryData } from '@/utils/query-feedback'
import { formatDateTime } from '@/utils/time'
import { CopyOutlined, KeyOutlined, SaveOutlined } from '@ant-design/icons'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import {
  App,
  Button,
  Card,
  Descriptions,
  Form,
  Input,
  InputNumber,
  Modal,
  Select,
  Space,
  Switch,
  Typography,
} from 'antd'
import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { bytesToMiB, mibToBytes, resolveOrganizationVisibilityPolicy } from '../model'
import { appHostingAdminMutations } from '../mutations'
import { appHostingAdminQueries } from '../queries'
import type {
  AppHostingGatewayTokenSecret,
  AppHostingOrganizationVisibilityPolicy,
  AppHostingSettings,
} from '../types'
import {
  buildGovernanceDraft,
  buildGovernancePayload,
  GovernanceSettingsFields,
  type GovernanceDraft,
  governanceDraftError,
} from './governance-fields'

const { Text } = Typography

interface SettingsFormDraft {
  enabled: boolean
  appsDomain: string
  publicScheme: 'https' | 'http'
  storageIntegrationId: string
  storagePrefix: string
  organizationVisibilityPolicy: AppHostingOrganizationVisibilityPolicy
  maxBundleCompressedMiB: number
  maxBundleUncompressedMiB: number
  maxBundleFileCount: number
  maxAppsPerOwner: number
  retainedRevisionsPerApp: number
  governance: GovernanceDraft
}

const POLICY_OPTIONS: Array<{
  label: string
  value: AppHostingOrganizationVisibilityPolicy
}> = [
  {
    label: '允许直接公开（无需审批）',
    value: 'allow_direct',
  },
  {
    label: '须管理员审批后公开（require_approval）',
    value: 'require_approval',
  },
  {
    label: '禁止新增全公司公开（disabled）',
    value: 'disabled',
  },
]

const SCHEME_OPTIONS = [
  { label: 'HTTPS（推荐）', value: 'https' },
  { label: 'HTTP（仅内网联调）', value: 'http' },
]

function buildDraftFromSettings(settings: AppHostingSettings): SettingsFormDraft {
  return {
    enabled: settings.enabled,
    appsDomain: settings.appsDomain ?? '',
    publicScheme: settings.publicScheme === 'http' ? 'http' : 'https',
    storageIntegrationId: settings.storageIntegrationId ?? '',
    storagePrefix: settings.storagePrefix ?? 'app-hosting',
    organizationVisibilityPolicy: resolveOrganizationVisibilityPolicy(settings),
    maxBundleCompressedMiB: bytesToMiB(settings.maxBundleCompressedBytes),
    maxBundleUncompressedMiB: bytesToMiB(settings.maxBundleUncompressedBytes),
    maxBundleFileCount: settings.maxBundleFileCount || 2000,
    maxAppsPerOwner: settings.maxAppsPerOwner || 20,
    retainedRevisionsPerApp: settings.retainedRevisionsPerApp || 10,
    governance: buildGovernanceDraft(settings),
  }
}

export function GovernanceSettingsPanel({
  canView,
  canManage,
  canViewStorageIntegrations,
}: {
  canView: boolean
  canManage: boolean
  canViewStorageIntegrations: boolean
}) {
  const { message } = App.useApp()
  const queryClient = useQueryClient()

  const settingsQuery = useQuery(appHostingAdminQueries.settings(canView))
  const visibleSettings =
    settingsQuery.isEnabled === false ? undefined : readableQueryData(settingsQuery)

  const storageIntegrationsQuery = useQuery(
    systemIntegrationQueries.list(
      {
        category: 'storage' as SystemIntegrationCategory,
        providerType: 's3',
      },
      canView && canViewStorageIntegrations,
    ),
  )
  const visibleStorageIntegrations =
    storageIntegrationsQuery.isEnabled === false
      ? undefined
      : readableQueryData(storageIntegrationsQuery)

  const [draft, setDraft] = useState<SettingsFormDraft | null>(() =>
    visibleSettings ? buildDraftFromSettings(visibleSettings) : null,
  )
  const [dirty, setDirty] = useState(false)
  const [manualStorageInput, setManualStorageInput] = useState(false)
  const syncedSignatureRef = useRef<string>(visibleSettings ? JSON.stringify(visibleSettings) : '')

  const [rotateConfirmOpen, setRotateConfirmOpen] = useState(false)
  const [rotateReason, setRotateReason] = useState('')
  const [rotatedSecret, setRotatedSecret] = useState<AppHostingGatewayTokenSecret | null>(null)

  const updateSettingsMutation = useOperationMutation(
    appHostingAdminMutations.updateSettings(queryClient),
    {
      label: ['保存应用托管平台策略', 'Save App Hosting settings'],
      target: () => '应用托管治理策略',
    },
  )

  const rotateTokenMutation = useOperationMutation(
    appHostingAdminMutations.rotateGatewayToken(queryClient),
    {
      label: ['轮换应用网关令牌', 'Rotate App Gateway token'],
      target: () => '应用网关内部令牌',
      receipt: (data) => ({
        message: '应用网关内部令牌已轮换',
        evidence: `轮换时间：${formatDateTime(data.rotatedAt)}`,
      }),
    },
  )

  const rotateResetRef = useRef(rotateTokenMutation.reset)
  rotateResetRef.current = rotateTokenMutation.reset

  const clearRotatedSecret = useCallback(() => {
    setRotatedSecret(null)
    rotateResetRef.current()
  }, [])

  useEffect(() => {
    return () => {
      rotateResetRef.current()
    }
  }, [])

  const settingsSignature = visibleSettings ? JSON.stringify(visibleSettings) : ''

  useEffect(() => {
    if (!visibleSettings || !settingsSignature) return
    if (syncedSignatureRef.current === settingsSignature) return
    syncedSignatureRef.current = settingsSignature
    if (!dirty) {
      setDraft(buildDraftFromSettings(visibleSettings))
    }
  }, [dirty, settingsSignature, visibleSettings])

  const operationBlocked =
    updateSettingsMutation.blocked ||
    rotateTokenMutation.blocked ||
    (settingsQuery.isEnabled !== false && queryAccessDenied(settingsQuery))

  const storageOptions = useMemo(() => {
    const list = visibleStorageIntegrations ?? []
    const options = list.map((item) => ({
      label: `${item.name} (${item.id})${item.enabled ? '' : ' · 已停用'}`,
      value: item.id,
    }))
    const currentId = draft?.storageIntegrationId?.trim()
    if (currentId && !options.some((opt) => opt.value === currentId)) {
      options.unshift({
        label: `当前配置：${currentId}`,
        value: currentId,
      })
    }
    return options
  }, [draft?.storageIntegrationId, visibleStorageIntegrations])

  const canSelectFromIntegrationList =
    canViewStorageIntegrations &&
    !storageIntegrationsQuery.isError &&
    (visibleStorageIntegrations?.length ?? 0) > 0 &&
    !manualStorageInput

  const savedPolicy = resolveOrganizationVisibilityPolicy(visibleSettings)
  const policyChanged = Boolean(
    draft && visibleSettings && draft.organizationVisibilityPolicy !== savedPolicy,
  )
  const governanceError = dirty && draft ? governanceDraftError(draft.governance) : undefined

  const updateField = <K extends keyof SettingsFormDraft>(key: K, value: SettingsFormDraft[K]) => {
    setDraft((current) => (current ? { ...current, [key]: value } : current))
    setDirty(true)
  }

  const updateGovernance = (governance: GovernanceDraft) => {
    updateField('governance', governance)
  }

  const handleSaveSettings = () => {
    if (!draft || !canManage || operationBlocked) return
    const error = governanceDraftError(draft.governance)
    if (error) {
      message.error(error)
      return
    }
    updateSettingsMutation.mutate(
      {
        enabled: draft.enabled,
        appsDomain: draft.appsDomain.trim(),
        publicScheme: draft.publicScheme,
        storageIntegrationId: draft.storageIntegrationId.trim(),
        storagePrefix: draft.storagePrefix.trim(),
        organizationVisibilityPolicy: draft.organizationVisibilityPolicy,
        allowOrganizationVisibility: draft.organizationVisibilityPolicy !== 'disabled',
        maxBundleCompressedBytes: mibToBytes(draft.maxBundleCompressedMiB),
        maxBundleUncompressedBytes: mibToBytes(draft.maxBundleUncompressedMiB),
        maxBundleFileCount: Math.max(1, Math.round(draft.maxBundleFileCount)),
        maxAppsPerOwner: Math.max(1, Math.round(draft.maxAppsPerOwner)),
        retainedRevisionsPerApp: Math.max(1, Math.round(draft.retainedRevisionsPerApp)),
        governance: buildGovernancePayload(draft.governance),
      },
      {
        onSuccess: (updated) => {
          syncedSignatureRef.current = JSON.stringify(updated)
          setDraft(buildDraftFromSettings(updated))
          setDirty(false)
        },
      },
    )
  }

  const handleConfirmRotateToken = () => {
    if (!canManage || operationBlocked) return
    rotateTokenMutation.mutate(
      { reason: rotateReason.trim() || undefined },
      {
        onSuccess: (secret) => {
          setRotateConfirmOpen(false)
          setRotateReason('')
          setRotatedSecret(secret)
        },
      },
    )
  }

  const handleCopyToken = async () => {
    if (!rotatedSecret?.token) return
    try {
      await navigator.clipboard.writeText(rotatedSecret.token)
      message.success('网关令牌已复制到剪贴板')
    } catch {
      message.warning('自动复制受限，请手动选中上方令牌复制')
    }
  }

  if (settingsQuery.isLoading && !visibleSettings) {
    return <ManagementState kind="loading" />
  }

  return (
    <div className="soha-identity-app-hosting-section soha-identity-app-hosting-settings">
      {updateSettingsMutation.feedback}
      {!rotateConfirmOpen && rotateTokenMutation.feedback}

      <ReadFeedback
        canRead={settingsQuery.isEnabled}
        query={settingsQuery}
        label="应用托管平台设置"
      />

      {settingsQuery.isError && !visibleSettings ? (
        <ManagementState
          kind="error"
          title="读取应用托管平台设置失败"
          description="请检查网络连接或稍后重试。"
          actions={<Button onClick={() => void settingsQuery.refetch()}>立即重试</Button>}
        />
      ) : draft ? (
        <Space orientation="vertical" size={16} style={{ width: '100%' }}>
          <Card
            variant="outlined"
            title="基础设施与平台策略设置"
            extra={
              <Space size={8}>
                {visibleSettings?.policyVersion != null ? (
                  <Text type="secondary">{`策略版本 v${visibleSettings.policyVersion}`}</Text>
                ) : null}
                <ManagementRefreshButton
                  aria-label="刷新平台策略"
                  loading={settingsQuery.isFetching}
                  tooltip="刷新平台策略"
                  onClick={() => void settingsQuery.refetch()}
                />
              </Space>
            }
          >
            <Form layout="vertical" onFinish={handleSaveSettings}>
              <div className="soha-identity-app-hosting-settings-enable-row">
                <Space size={12} wrap>
                  <Switch
                    aria-label="启用应用托管"
                    checked={draft.enabled}
                    disabled={!canManage || operationBlocked}
                    onChange={(checked) => updateField('enabled', checked)}
                  />
                  <Space orientation="vertical" size={0}>
                    <Text strong>启用工作区应用托管</Text>
                    <Text type="secondary" className="soha-identity-app-hosting-subtext">
                      启用后允许授权员工发布托管作品，并通过独立泛域名应用网关提供企业 SSO 访问。
                    </Text>
                  </Space>
                </Space>
              </div>

              <div className="soha-identity-app-hosting-form-grid">
                <Form.Item label={<label htmlFor="soha-app-hosting-apps-domain">泛域名后缀</label>}>
                  <Input
                    id="soha-app-hosting-apps-domain"
                    aria-label="泛域名后缀"
                    disabled={!canManage || operationBlocked}
                    placeholder="apps.corp.example.com"
                    value={draft.appsDomain}
                    onChange={(event) => updateField('appsDomain', event.target.value)}
                  />
                </Form.Item>

                <Form.Item label="公开访问协议">
                  <Select
                    aria-label="公开访问协议"
                    disabled={!canManage || operationBlocked}
                    options={SCHEME_OPTIONS}
                    value={draft.publicScheme}
                    onChange={(value: 'https' | 'http') => updateField('publicScheme', value)}
                  />
                </Form.Item>

                <Form.Item
                  label={
                    <Space size={8}>
                      <label htmlFor="soha-app-hosting-storage-id">对象存储集成</label>
                      {canViewStorageIntegrations &&
                      !storageIntegrationsQuery.isError &&
                      (visibleStorageIntegrations?.length ?? 0) > 0 ? (
                        <Button
                          disabled={!canManage || operationBlocked}
                          size="small"
                          type="link"
                          onClick={() => setManualStorageInput((prev) => !prev)}
                        >
                          {manualStorageInput ? '从列表选择' : '手动输入 ID'}
                        </Button>
                      ) : null}
                    </Space>
                  }
                  extra={
                    !canViewStorageIntegrations
                      ? '当前账号无系统集成列表查看权限（settings.system-integrations.view），已自动切换为直接填写对象存储集成 ID。'
                      : storageIntegrationsQuery.isError
                        ? '对象存储集成列表暂不可读，可直接输入对象存储集成 ID 保存。'
                        : undefined
                  }
                >
                  {canSelectFromIntegrationList ? (
                    <Select
                      aria-label="对象存储集成"
                      disabled={!canManage || operationBlocked}
                      loading={storageIntegrationsQuery.isFetching}
                      options={storageOptions}
                      placeholder="选择 S3 / MinIO 对象存储集成"
                      value={draft.storageIntegrationId || undefined}
                      onChange={(value) => updateField('storageIntegrationId', value ?? '')}
                    />
                  ) : (
                    <Input
                      id="soha-app-hosting-storage-id"
                      aria-label="对象存储集成"
                      disabled={!canManage || operationBlocked}
                      placeholder="输入对象存储集成 ID（如 storage-minio-default）"
                      value={draft.storageIntegrationId}
                      onChange={(event) => updateField('storageIntegrationId', event.target.value)}
                    />
                  )}
                </Form.Item>

                <Form.Item
                  label={<label htmlFor="soha-app-hosting-storage-prefix">对象存储前缀</label>}
                >
                  <Input
                    id="soha-app-hosting-storage-prefix"
                    aria-label="对象存储前缀"
                    disabled={!canManage || operationBlocked}
                    placeholder="app-hosting"
                    value={draft.storagePrefix}
                    onChange={(event) => updateField('storagePrefix', event.target.value)}
                  />
                </Form.Item>
              </div>

              <Form.Item label="全公司公开策略（organizationVisibilityPolicy）">
                <Select
                  aria-label="全公司公开策略"
                  disabled={!canManage || operationBlocked}
                  options={POLICY_OPTIONS}
                  value={draft.organizationVisibilityPolicy}
                  onChange={(value: AppHostingOrganizationVisibilityPolicy) =>
                    updateField('organizationVisibilityPolicy', value)
                  }
                />
              </Form.Item>

              <ContextFeedback
                className="soha-identity-app-hosting-policy-hint"
                type={policyChanged ? 'warning' : 'info'}
                title={
                  policyChanged
                    ? '即将变更全公司公开策略：现有待审申请将在保存后自动失效'
                    : '全公司公开策略说明'
                }
                description="切换全公司公开策略会递增策略版本（policyVersion），使当前所有待审的全公司公开申请自动置为失效；已获批在线的应用保持原范围，新公开请求按最新策略执行。"
              />

              <div className="soha-identity-app-hosting-form-grid soha-identity-app-hosting-quota-grid">
                <Form.Item label="单包压缩上限（MiB）">
                  <InputNumber
                    aria-label="单包压缩上限（MiB）"
                    disabled={!canManage || operationBlocked}
                    min={1}
                    max={2048}
                    style={{ width: '100%' }}
                    value={draft.maxBundleCompressedMiB}
                    onChange={(value) => updateField('maxBundleCompressedMiB', Number(value ?? 1))}
                  />
                </Form.Item>

                <Form.Item label="单包解压上限（MiB）">
                  <InputNumber
                    aria-label="单包解压上限（MiB）"
                    disabled={!canManage || operationBlocked}
                    min={1}
                    max={4096}
                    style={{ width: '100%' }}
                    value={draft.maxBundleUncompressedMiB}
                    onChange={(value) =>
                      updateField('maxBundleUncompressedMiB', Number(value ?? 1))
                    }
                  />
                </Form.Item>

                <Form.Item label="单应用最大文件数">
                  <InputNumber
                    aria-label="单应用最大文件数"
                    disabled={!canManage || operationBlocked}
                    min={1}
                    max={100000}
                    precision={0}
                    style={{ width: '100%' }}
                    value={draft.maxBundleFileCount}
                    onChange={(value) => updateField('maxBundleFileCount', Number(value ?? 1))}
                  />
                </Form.Item>

                <Form.Item label="每作者静态应用配额上限">
                  <InputNumber
                    aria-label="每作者静态应用配额上限"
                    disabled={!canManage || operationBlocked}
                    min={1}
                    max={10000}
                    precision={0}
                    style={{ width: '100%' }}
                    value={draft.maxAppsPerOwner}
                    onChange={(value) => updateField('maxAppsPerOwner', Number(value ?? 1))}
                  />
                </Form.Item>

                <Form.Item label="每应用保留版本数">
                  <InputNumber
                    aria-label="每应用保留版本数"
                    disabled={!canManage || operationBlocked}
                    min={1}
                    max={500}
                    precision={0}
                    style={{ width: '100%' }}
                    value={draft.retainedRevisionsPerApp}
                    onChange={(value) => updateField('retainedRevisionsPerApp', Number(value ?? 1))}
                  />
                </Form.Item>
              </div>

              <GovernanceSettingsFields
                disabled={!canManage || operationBlocked}
                draft={draft.governance}
                error={governanceError}
                onChange={updateGovernance}
              />

              <div className="soha-identity-app-hosting-settings-footer">
                <Text type="secondary" className="soha-identity-app-hosting-subtext">
                  {visibleSettings?.updatedAt
                    ? `最近保存：${formatDateTime(visibleSettings.updatedAt)}${visibleSettings.updatedBy ? `（操作人：${visibleSettings.updatedBy}）` : ''}`
                    : '尚未记录人工配置变更'}
                </Text>
                <Space>
                  <Button
                    disabled={!canManage || !dirty || operationBlocked}
                    onClick={() => {
                      if (visibleSettings) {
                        setDraft(buildDraftFromSettings(visibleSettings))
                        setDirty(false)
                      }
                    }}
                  >
                    重置修改
                  </Button>
                  <Button
                    disabled={!canManage || operationBlocked}
                    htmlType="submit"
                    icon={<SaveOutlined />}
                    loading={updateSettingsMutation.isPending}
                    type="primary"
                  >
                    保存策略配置
                  </Button>
                </Space>
              </div>
            </Form>
          </Card>

          <Card variant="outlined" title="应用网关内部通信令牌（app-gateway）">
            <Space orientation="vertical" size={12} style={{ width: '100%' }}>
              <Descriptions
                column={{ xs: 1, sm: 2 }}
                size="small"
                items={[
                  {
                    key: 'gatewayTokenConfigured',
                    label: '网关令牌状态',
                    children: visibleSettings?.gatewayTokenConfigured ? (
                      <StatusTag label="已配置" value="ready" />
                    ) : (
                      <StatusTag label="未配置" value="pending" />
                    ),
                  },
                  {
                    key: 'usage',
                    label: '用途说明',
                    children:
                      '供独立数据面 app-gateway 向 Core 兑换 SSO 登录票据与逐请求校验受众权限',
                  },
                ]}
              />
              <div>
                <Button
                  danger={Boolean(visibleSettings?.gatewayTokenConfigured)}
                  disabled={!canManage || operationBlocked}
                  icon={<KeyOutlined />}
                  onClick={() => setRotateConfirmOpen(true)}
                >
                  轮换网关令牌
                </Button>
              </div>
            </Space>
          </Card>
        </Space>
      ) : null}

      <Modal
        closable={!operationBlocked}
        destroyOnHidden
        mask={{ closable: false }}
        open={rotateConfirmOpen}
        style={{ top: 32 }}
        styles={operationModalStyles}
        title="确认轮换应用网关内部令牌"
        width={520}
        footer={
          <Space>
            <Button
              disabled={operationBlocked}
              onClick={() => {
                if (!operationBlocked) {
                  setRotateConfirmOpen(false)
                  setRotateReason('')
                }
              }}
            >
              取消
            </Button>
            <Button
              danger
              disabled={!canManage || operationBlocked}
              loading={rotateTokenMutation.isPending}
              type="primary"
              onClick={handleConfirmRotateToken}
            >
              确认轮换令牌
            </Button>
          </Space>
        }
        onCancel={() => {
          if (!operationBlocked) {
            setRotateConfirmOpen(false)
            setRotateReason('')
          }
        }}
      >
        {rotateTokenMutation.feedback}
        <Space orientation="vertical" size={16} style={{ width: '100%' }}>
          <ContextFeedback
            type="warning"
            title="轮换后旧网关令牌将立即失效"
            description="生成新令牌后，请立即更新 Kubernetes app-gateway Secret 并滚动网关实例；新令牌明文仅在生成成功后的弹窗中单次展示。"
          />
          <Form layout="vertical">
            <Form.Item
              label={<label htmlFor="soha-app-hosting-rotate-reason">轮换原因（可选）</label>}
            >
              <Input.TextArea
                id="soha-app-hosting-rotate-reason"
                aria-label="轮换原因"
                disabled={!canManage || operationBlocked}
                maxLength={500}
                placeholder="例如：首次部署初始化 / 定期凭据轮换"
                rows={2}
                value={rotateReason}
                onChange={(event) => setRotateReason(event.target.value)}
              />
            </Form.Item>
          </Form>
        </Space>
      </Modal>

      <Modal
        destroyOnHidden
        mask={{ closable: false }}
        open={Boolean(rotatedSecret)}
        style={{ top: 32 }}
        styles={operationModalStyles}
        title="新网关内部令牌（仅展示一次）"
        width={580}
        footer={
          <Space>
            <Button icon={<CopyOutlined />} onClick={() => void handleCopyToken()}>
              复制令牌
            </Button>
            <Button type="primary" onClick={clearRotatedSecret}>
              我已保存并关闭
            </Button>
          </Space>
        }
        onCancel={clearRotatedSecret}
      >
        {rotatedSecret ? (
          <Space orientation="vertical" size={16} style={{ width: '100%' }}>
            <ContextFeedback
              type="warning"
              title="请立即复制到 app-gateway Secret，关闭弹窗后明文令牌将从内存中彻底清除"
              description={`生成时间：${formatDateTime(rotatedSecret.rotatedAt)}。平台仅存储令牌哈希，关闭此窗口后无法再次查看该明文值。`}
            />
            <div className="soha-identity-app-hosting-token-box">
              <code data-testid="rotated-gateway-token-value">{rotatedSecret.token}</code>
            </div>
          </Space>
        ) : null}
      </Modal>
    </div>
  )
}
