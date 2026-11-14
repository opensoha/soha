import { writeOutcomeUnknown } from '@/utils/query-feedback'
import { ReadFeedback } from '@/components/request-feedback'
import { ComputeMutationFeedback } from '@/features/compute'
import { useEffect, useMemo, useRef, useState } from 'react'
import { Alert, App, Collapse, Descriptions, Form, Input, Segmented, Select, Switch } from 'antd'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { StepFormModal } from '@/components/step-form-modal'
import type { StepFormStep } from '@/components/step-form'
import { localeText, useI18n } from '@/i18n'
import {
  virtualizationMutations,
  withVirtualizationMutationSuccess,
} from '@/features/virtualization/mutations'
import {
  buildClusterPayload,
  type VirtualizationClusterFormValues,
} from '@/features/virtualization/virtualization-model'
import { virtualizationQueries } from '@/features/virtualization/queries'
import { useVirtualizationPermissions } from '@/features/virtualization/shared/use-virtualization-permissions'
import type { VirtualizationCluster } from '@/features/virtualization/virtualization-types'

interface VirtualizationConnectionStepModalProps {
  editing?: VirtualizationCluster | null
  initialProvider?: 'kubevirt' | 'pve'
  onClose: () => void
  onSuccess?: () => void
  open: boolean
}

type PVEAuthMode = 'password' | 'token' | 'ticket'

const PVE_AUTH_OPTIONS = [
  { label: '账号密码', value: 'password' },
  { label: 'API Token', value: 'token' },
  { label: 'Ticket', value: 'ticket' },
] satisfies Array<{ label: string; value: PVEAuthMode }>

function connectionFormValues(
  record?: VirtualizationCluster | null,
): Partial<VirtualizationClusterFormValues> {
  if (!record) return { provider: 'kubevirt', enabled: true, verifyTls: true }
  return {
    name: record.name,
    provider: record.provider === 'pve' ? 'pve' : 'kubevirt',
    endpoint: record.endpoint,
    kubernetesClusterId: record.kubernetesClusterId,
    defaultNamespace: record.defaultNamespace,
    enabled: record.enabled !== false,
    verifyTls: record.verifyTls !== false,
    region: record.region,
    description: record.description,
    defaultNode:
      typeof record.config?.defaultNode === 'string' ? record.config.defaultNode : undefined,
    defaultStorage:
      typeof record.config?.defaultStorage === 'string' ? record.config.defaultStorage : undefined,
    defaultBridge:
      typeof record.config?.defaultBridge === 'string' ? record.config.defaultBridge : undefined,
    defaultSnippetStorage:
      typeof record.config?.defaultSnippetStorage === 'string'
        ? record.config.defaultSnippetStorage
        : typeof record.config?.snippetStorage === 'string'
          ? record.config.snippetStorage
          : undefined,
    backendUrl:
      typeof record.config?.backendUrl === 'string' ? record.config.backendUrl : undefined,
    prometheusUrl:
      typeof record.config?.prometheusUrl === 'string' ? record.config.prometheusUrl : undefined,
    mode: typeof record.config?.mode === 'string' ? record.config.mode : undefined,
  }
}

export function VirtualizationConnectionStepModal({
  editing,
  initialProvider = 'kubevirt',
  onClose,
  onSuccess,
  open,
}: VirtualizationConnectionStepModalProps) {
  const [current, setCurrent] = useState(0)
  const [pveAuthMode, setPveAuthMode] = useState<PVEAuthMode>('password')
  const [replaceCredential, setReplaceCredential] = useState(false)
  const [form] = Form.useForm<VirtualizationClusterFormValues>()
  const initialValues = useMemo(
    () =>
      editing
        ? connectionFormValues(editing)
        : { ...connectionFormValues(), provider: initialProvider },
    [editing, initialProvider],
  )
  const provider = Form.useWatch('provider', form) ?? 'kubevirt'
  const selectedKubernetesClusterId = Form.useWatch('kubernetesClusterId', form)
  const { virtualizationModuleEnabled, canCreateClusters, canUpdateClusters } =
    useVirtualizationPermissions()
  const platformClustersQuery = useQuery(
    virtualizationQueries.platformClusterOptions(
      virtualizationModuleEnabled && (editing ? canUpdateClusters : canCreateClusters),
    ),
  )
  const selectedPlatformCluster = useMemo(
    () =>
      (platformClustersQuery.data ?? []).find((item) => item.id === selectedKubernetesClusterId),
    [platformClustersQuery.data, selectedKubernetesClusterId],
  )
  const queryClient = useQueryClient()
  const { message } = App.useApp()
  const { localeCode } = useI18n()
  const finish = () => {
    message.success(
      editing
        ? localeText(localeCode, '虚拟化连接已更新', 'Virtualization connection updated')
        : localeText(localeCode, '虚拟化连接已创建', 'Virtualization connection created'),
    )
    onSuccess?.()
    onClose()
  }
  const createMutation = useMutation(
    withVirtualizationMutationSuccess(virtualizationMutations.createCluster(queryClient), () => {
      finish()
    }),
  )
  const updateMutation = useMutation(
    withVirtualizationMutationSuccess(virtualizationMutations.updateCluster(queryClient), finish),
  )
  const resetEditorFeedback = useRef(() => {})
  resetEditorFeedback.current = () => {
    if (!createMutation.isPending && !writeOutcomeUnknown(createMutation.error))
      createMutation.reset()
    if (!updateMutation.isPending && !writeOutcomeUnknown(updateMutation.error))
      updateMutation.reset()
  }

  useEffect(() => {
    if (!open) return
    resetEditorFeedback.current()
    setCurrent(0)
    setPveAuthMode('password')
    setReplaceCredential(!editing)
    form.resetFields()
    form.setFieldsValue(initialValues)
  }, [editing, form, initialValues, open])

  const pveCredentialFields =
    pveAuthMode === 'token'
      ? ['tokenID', 'tokenSecret']
      : pveAuthMode === 'ticket'
        ? ['ticket', 'csrfToken']
        : ['username', 'password']
  const showPVECredentialFields = !editing || replaceCredential

  const steps: StepFormStep[] = [
    {
      title: localeText(localeCode, '基本信息', 'Basic information'),
      fieldNames: ['name', 'provider'],
      children: (
        <>
          <Form.Item
            name="name"
            label={localeText(localeCode, '名称', 'Name')}
            rules={[
              { required: true, message: localeText(localeCode, '请输入名称', 'Enter a name') },
            ]}
          >
            <Input />
          </Form.Item>
          <Form.Item
            name="provider"
            label={localeText(localeCode, '提供方', 'Provider')}
            rules={[{ required: true, message: '请选择 Provider' }]}
          >
            <Select
              options={[
                { value: 'kubevirt', label: 'KubeVirt' },
                { value: 'pve', label: 'PVE' },
              ]}
              onChange={(nextProvider) => {
                setPveAuthMode('password')
                if (nextProvider === 'pve') setReplaceCredential(true)
                form.setFieldsValue({
                  endpoint: undefined,
                  kubernetesClusterId: undefined,
                  mode: undefined,
                  username: undefined,
                  password: undefined,
                  tokenID: undefined,
                  tokenSecret: undefined,
                  ticket: undefined,
                  csrfToken: undefined,
                })
              }}
            />
          </Form.Item>
          <Form.Item name="region" label="Region">
            <Input />
          </Form.Item>
        </>
      ),
    },
    {
      title: localeText(localeCode, '连接配置', 'Connection configuration'),
      fieldNames:
        provider === 'kubevirt'
          ? ['kubernetesClusterId']
          : ['endpoint', ...(showPVECredentialFields ? pveCredentialFields : [])],
      children: (
        <>
          <Form.Item name="mode" hidden>
            <Input />
          </Form.Item>
          {provider === 'kubevirt' ? (
            <>
              <Alert
                showIcon
                type={selectedPlatformCluster?.connectionMode === 'agent' ? 'warning' : 'info'}
                title={
                  selectedPlatformCluster?.connectionMode === 'agent'
                    ? localeText(
                        localeCode,
                        '当前集群使用 Agent 通道',
                        'This cluster uses the Agent channel',
                      )
                    : localeText(
                        localeCode,
                        '当前连接使用直连 kubeconfig 通道',
                        'This connection uses direct kubeconfig access',
                      )
                }
              />
              <Form.Item
                name="kubernetesClusterId"
                label={localeText(localeCode, 'Kubernetes 集群', 'Kubernetes cluster')}
                rules={[{ required: true, message: '请选择 Kubernetes 集群' }]}
              >
                <Select
                  showSearch={{ optionFilterProp: 'label' }}
                  loading={platformClustersQuery.isLoading}
                  options={(platformClustersQuery.data ?? []).map((item) => ({
                    value: item.id,
                    label: `${item.name} (${item.connectionMode})`,
                  }))}
                  onChange={(value) => {
                    const cluster = (platformClustersQuery.data ?? []).find(
                      (item) => item.id === value,
                    )
                    form.setFieldValue(
                      'mode',
                      cluster?.connectionMode === 'agent' ? 'agent' : 'direct_kubeconfig',
                    )
                  }}
                />
              </Form.Item>
              <Form.Item
                name="defaultNamespace"
                label={localeText(localeCode, '默认命名空间', 'Default namespace')}
              >
                <Input />
              </Form.Item>
              <Form.Item name="backendUrl" label="Console Backend URL">
                <Input placeholder="https://kube-api.example:6443" />
              </Form.Item>
              <Form.Item name="prometheusUrl" label="Prometheus URL">
                <Input placeholder="https://prometheus.example" />
              </Form.Item>
              <Form.Item name="prometheusBearerToken" label="Prometheus Bearer Token">
                <Input.Password
                  placeholder={
                    editing
                      ? localeText(
                          localeCode,
                          '留空保持现有 Token',
                          'Leave empty to keep the current token',
                        )
                      : localeText(localeCode, '可选', 'Optional')
                  }
                />
              </Form.Item>
            </>
          ) : (
            <>
              <Form.Item
                name="endpoint"
                label="Endpoint"
                rules={[{ required: true, message: '请输入 PVE Endpoint' }]}
              >
                <Input placeholder="https://pve.example:8006" />
              </Form.Item>
              {editing ? (
                <Form.Item label={localeText(localeCode, '更新凭证', 'Update credentials')}>
                  <Switch checked={replaceCredential} onChange={setReplaceCredential} />
                </Form.Item>
              ) : null}
              {showPVECredentialFields ? (
                <>
                  <Form.Item label={localeText(localeCode, '认证方式', 'Authentication')}>
                    <Segmented
                      block
                      options={PVE_AUTH_OPTIONS.map((option) =>
                        option.value === 'password'
                          ? {
                              ...option,
                              label: localeText(localeCode, '账号密码', 'Username and password'),
                            }
                          : option,
                      )}
                      value={pveAuthMode}
                      onChange={(value) => setPveAuthMode(value as PVEAuthMode)}
                    />
                  </Form.Item>
                  {pveAuthMode === 'password' ? (
                    <>
                      <Form.Item
                        name="username"
                        label="Username"
                        preserve={false}
                        rules={[
                          { required: true, message: '请输入 PVE 用户名' },
                          {
                            pattern: /^[^@\s]+@[^@\s]+$/,
                            message: '请输入包含 realm 的用户名，例如 root@pam',
                          },
                        ]}
                      >
                        <Input autoComplete="username" placeholder="root@pam" />
                      </Form.Item>
                      <Form.Item
                        name="password"
                        label="Password"
                        preserve={false}
                        rules={[{ required: true, message: '请输入 PVE 密码' }]}
                      >
                        <Input.Password autoComplete="current-password" />
                      </Form.Item>
                    </>
                  ) : null}
                  {pveAuthMode === 'token' ? (
                    <>
                      <Form.Item
                        name="tokenID"
                        label="Token ID"
                        preserve={false}
                        rules={[{ required: true, message: '请输入 PVE Token ID' }]}
                      >
                        <Input placeholder="root@pam!soha" />
                      </Form.Item>
                      <Form.Item
                        name="tokenSecret"
                        label="Token Secret"
                        preserve={false}
                        rules={[{ required: true, message: '请输入 PVE Token Secret' }]}
                      >
                        <Input.Password autoComplete="off" />
                      </Form.Item>
                    </>
                  ) : null}
                  {pveAuthMode === 'ticket' ? (
                    <>
                      <Form.Item
                        name="ticket"
                        label="Ticket"
                        preserve={false}
                        rules={[{ required: true, message: '请输入 PVE Ticket' }]}
                      >
                        <Input.Password autoComplete="off" />
                      </Form.Item>
                      <Form.Item
                        name="csrfToken"
                        label="CSRF Token"
                        preserve={false}
                        rules={[{ required: true, message: '请输入 PVE CSRF Token' }]}
                      >
                        <Input.Password autoComplete="off" />
                      </Form.Item>
                    </>
                  ) : null}
                </>
              ) : null}
            </>
          )}
          <Form.Item
            name="verifyTls"
            label={localeText(localeCode, '校验 TLS', 'Verify TLS')}
            valuePropName="checked"
          >
            <Switch />
          </Form.Item>
        </>
      ),
    },
    {
      title: localeText(localeCode, '可选配置', 'Optional settings'),
      children: (
        <>
          {provider === 'pve' ? (
            <Collapse
              ghost
              items={[
                {
                  key: 'pve-resource-defaults',
                  label: localeText(
                    localeCode,
                    'PVE 资源默认值（可选）',
                    'PVE resource defaults (optional)',
                  ),
                  forceRender: true,
                  children: (
                    <>
                      <Form.Item
                        name="defaultNode"
                        label={localeText(localeCode, '默认节点', 'Default node')}
                      >
                        <Input />
                      </Form.Item>
                      <Form.Item
                        name="defaultStorage"
                        label={localeText(localeCode, '默认存储', 'Default storage')}
                      >
                        <Input />
                      </Form.Item>
                      <Form.Item
                        name="defaultBridge"
                        label={localeText(localeCode, '默认网桥', 'Default bridge')}
                      >
                        <Input />
                      </Form.Item>
                      <Form.Item
                        name="defaultSnippetStorage"
                        label={localeText(
                          localeCode,
                          '默认 Snippet Storage',
                          'Default snippet storage',
                        )}
                      >
                        <Input />
                      </Form.Item>
                    </>
                  ),
                },
              ]}
            />
          ) : null}
          <Form.Item
            name="enabled"
            label={localeText(localeCode, '创建后启用', 'Enable after creation')}
            valuePropName="checked"
          >
            <Switch />
          </Form.Item>
          <Form.Item name="description" label={localeText(localeCode, '描述', 'Description')}>
            <Input.TextArea rows={3} />
          </Form.Item>
          <Descriptions bordered size="small" column={1}>
            <Descriptions.Item label={localeText(localeCode, '提供方', 'Provider')}>
              {provider}
            </Descriptions.Item>
            <Descriptions.Item label={localeText(localeCode, '接入方式', 'Access mode')}>
              {provider === 'kubevirt'
                ? selectedPlatformCluster?.connectionMode || 'direct'
                : 'direct'}
            </Descriptions.Item>
          </Descriptions>
        </>
      ),
    },
  ]

  return (
    <>
      {!open ? (
        <ComputeMutationFeedback
          operations={[
            {
              label: localeText(localeCode, '保存连接', 'Save connection'),
              mutation: createMutation,
              target: createMutation.variables?.name,
              receipt: {
                message: localeText(
                  localeCode,
                  '连接记录已保存，连接健康以检查结果为准',
                  'Connection record saved; check health separately',
                ),
              },
            },
            {
              label: localeText(localeCode, '更新连接', 'Update connection'),
              mutation: updateMutation,
              target: updateMutation.variables?.id,
              receipt: {
                message: localeText(
                  localeCode,
                  '连接记录已保存，连接健康以检查结果为准',
                  'Connection record saved; check health separately',
                ),
              },
            },
          ]}
        />
      ) : null}
      <StepFormModal
        feedback={
          <>
            <ReadFeedback
              query={platformClustersQuery}
              label={localeText(localeCode, 'Kubernetes 集群选项', 'Kubernetes cluster options')}
            />
            <ComputeMutationFeedback
              operations={[
                {
                  label: localeText(localeCode, '保存连接', 'Save connection'),
                  mutation: createMutation,
                  target: createMutation.variables?.name,
                  receipt: {
                    message: localeText(
                      localeCode,
                      '连接记录已保存，连接健康以检查结果为准',
                      'Connection record saved; check health separately',
                    ),
                  },
                },
                {
                  label: localeText(localeCode, '更新连接', 'Update connection'),
                  mutation: updateMutation,
                  target: updateMutation.variables?.id,
                  receipt: {
                    message: localeText(
                      localeCode,
                      '连接记录已保存，连接健康以检查结果为准',
                      'Connection record saved; check health separately',
                    ),
                  },
                },
              ]}
            />
          </>
        }
        current={current}
        form={form}
        initialValues={initialValues}
        loading={createMutation.isPending || updateMutation.isPending}
        onClose={() => {
          if (!createMutation.isPending && !updateMutation.isPending) onClose()
        }}
        onCurrentChange={setCurrent}
        onFinish={(values) => {
          const payload = buildClusterPayload(values, editing)
          if (editing) updateMutation.mutate({ id: editing.id, payload })
          else createMutation.mutate(payload)
        }}
        open={open}
        steps={steps}
        submitText={
          editing
            ? localeText(localeCode, '保存连接', 'Save connection')
            : localeText(localeCode, '创建连接', 'Create connection')
        }
        title={
          editing
            ? localeText(localeCode, '编辑虚拟化连接', 'Edit virtualization connection')
            : localeText(localeCode, '新增虚拟化连接', 'Add virtualization connection')
        }
      />
    </>
  )
}
