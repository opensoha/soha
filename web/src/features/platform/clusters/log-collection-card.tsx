import { ReadFeedback } from '@/components/request-feedback'
import { hasPermission, usePermissionSnapshot } from '@/features/auth'
import {
  platformResourceTarget,
  usePlatformMutation,
} from '@/features/platform/shared/use-platform-mutation'
import type { ScopeKey } from '@/types'
import { readableQueryData } from '@/utils/query-feedback'
import { formatDateTime } from '@/utils/time'
import {
  CheckCircleOutlined,
  DeleteOutlined,
  PauseOutlined,
  PlayCircleOutlined,
} from '@ant-design/icons'
import type {
  LogCollectionPlan,
  LogCollectionPreflightInput,
  LogCollectionProfile,
} from '@opensoha/contracts/gen/ts/sohaapi'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import {
  Alert,
  Button,
  Descriptions,
  Form,
  Input,
  InputNumber,
  Modal,
  Popconfirm,
  Segmented,
  Select,
  Space,
  Spin,
  Tag,
} from 'antd'
import { useState } from 'react'
import { clusterMutations } from './mutations'
import { clusterQueries } from './queries'

const statusColors: Record<string, string> = {
  healthy: 'success',
  degraded: 'warning',
  failed: 'error',
  installing: 'processing',
  stopping: 'processing',
  disabled: 'default',
}

const zhMessages: Record<string, string> = {
  'managed collection is not enabled': '增强日志采集尚未启用',
  'The collector mounts /var/log/pods read-only on every selected node.':
    '采集器会在每个选定节点上以只读方式挂载 /var/log/pods。',
  'Stopping or uninstalling collection does not delete durable history.':
    '停止或卸载采集组件不会删除已持久化的历史日志。',
  'Starter runs a single-replica Loki backend and is intended for small installations.':
    'Starter 使用单副本 Loki，适合小规模环境。',
}

function displayMessage(message: string | undefined, zh: boolean) {
  if (!message) return '-'
  return zh ? (zhMessages[message] ?? message) : message
}

type FormValues = LogCollectionPreflightInput

export function LogCollectionCard({ scope, localeCode }: { scope: ScopeKey; localeCode: string }) {
  const queryClient = useQueryClient()
  const [form] = Form.useForm<FormValues>()
  const [open, setOpen] = useState(false)
  const [plan, setPlan] = useState<LogCollectionPlan | null>(null)
  const profile = Form.useWatch('profile', form) as LogCollectionProfile | undefined
  const stateQuery = useQuery(clusterQueries.logCollection(scope))
  const visibleStateQueryData = readableQueryData(stateQuery)
  const dataSourcesQuery = useQuery(clusterQueries.logDataSources(open && profile !== 'starter'))
  const visibleDataSourcesQueryData = readableQueryData(dataSourcesQuery)
  const permissionQuery = usePermissionSnapshot()
  const permissionSnapshot = permissionQuery.data?.data
  const canEnable = hasPermission(permissionSnapshot, 'platform.observability.logging.enable')
  const canDisable = hasPermission(permissionSnapshot, 'platform.observability.logging.disable')
  const preflight = usePlatformMutation(clusterMutations.preflightLogCollection(), {
    label: ['日志采集预检', 'Log collection preflight'],
    target: platformResourceTarget,
    readOnly: true,
  })
  const enable = usePlatformMutation(clusterMutations.enableLogCollection(queryClient), {
    label: ['启用日志采集', 'Enable log collection'],
    target: platformResourceTarget,
  })
  const disable = usePlatformMutation(clusterMutations.disableLogCollection(queryClient), {
    label: ['停用日志采集', 'Disable log collection'],
    target: platformResourceTarget,
  })
  const platformWriteBlocked = enable.blocked || disable.blocked
  const state = visibleStateQueryData
  const zh = localeCode === 'zh_CN'
  const busy =
    platformWriteBlocked ||
    stateQuery.isError ||
    ['installing', 'stopping'].includes(state?.status ?? '')

  const showPreflight = () => {
    if (busy) return
    setPlan(null)
    preflight.reset()
    form.resetFields()
    form.setFieldsValue({
      profile: state?.profile ?? 'starter',
      namespace: state?.namespace || 'soha-observability',
      dataSourceId: state?.dataSourceId,
      retentionDays: state?.retentionDays || 7,
      storageSize: '10Gi',
      namespaceAllowlist: state?.namespaceAllowlist ?? [],
    })
    setOpen(true)
  }

  const runPreflight = async () => {
    const input = await form.validateFields()
    setPlan(await preflight.mutateAsync({ scope, input }))
  }

  const confirmEnable = async () => {
    if (!plan?.planToken || platformWriteBlocked || stateQuery.isError) return
    await enable.mutateAsync({ scope, input: { planToken: plan.planToken } })
    setOpen(false)
    setPlan(null)
  }

  if (stateQuery.isLoading) return <Spin />

  return (
    <>
      {!open ? (
        <>
          <ReadFeedback canRead={stateQuery.isEnabled} query={stateQuery} label="日志采集状态" />
          {preflight.feedback}
          {enable.feedback}
          {disable.feedback}
        </>
      ) : null}

      {state ? (
        <>
          <Descriptions
            size="small"
            column={1}
            items={[
              {
                key: 'mode',
                label: zh ? '模式' : 'Mode',
                children: <Tag>{state?.mode ?? 'runtime_only'}</Tag>,
              },
              {
                key: 'status',
                label: zh ? '状态' : 'Status',
                children: (
                  <Tag color={statusColors[state?.status ?? 'disabled']}>
                    {state?.status ?? 'disabled'}
                  </Tag>
                ),
              },
              {
                key: 'collector',
                label: zh ? '采集器' : 'Collector',
                children: displayMessage(state?.collector.message, zh),
              },
              {
                key: 'backend',
                label: zh ? '存储查询' : 'Backend',
                children: displayMessage(state?.backend.message, zh),
              },
              {
                key: 'scope',
                label: zh ? '采集范围' : 'Collection scope',
                children: state?.namespaceAllowlist?.length
                  ? state.namespaceAllowlist.join(', ')
                  : zh
                    ? '全集群'
                    : 'Cluster-wide',
              },
              {
                key: 'updatedAt',
                label: zh ? '更新时间' : 'Updated',
                children: state?.updatedAt ? formatDateTime(state.updatedAt) : '-',
              },
            ]}
          />
          {state?.lastError ? <Alert type="warning" showIcon title={state.lastError} /> : null}
          {enable.isPending ||
          disable.isPending ||
          ['installing', 'stopping'].includes(state.status) ? (
            <Alert
              type="info"
              showIcon
              title={
                enable.isPending || state?.status === 'installing'
                  ? zh
                    ? '正在安装日志采集组件'
                    : 'Installing log collection components'
                  : zh
                    ? '正在停用日志采集组件'
                    : 'Stopping log collection components'
              }
            />
          ) : null}
          {canEnable || canDisable ? (
            <Space wrap className="soha-log-collection-actions">
              {canEnable ? (
                <Button
                  icon={<PlayCircleOutlined />}
                  type="primary"
                  onClick={showPreflight}
                  disabled={busy}
                >
                  {state?.status === 'failed' || state?.status === 'degraded'
                    ? zh
                      ? '重新预检'
                      : 'Run preflight again'
                    : zh
                      ? '启用增强采集'
                      : 'Enable managed collection'}
                </Button>
              ) : null}
              {canDisable && state?.mode === 'soha_managed' && state.status !== 'disabled' ? (
                <Popconfirm
                  okButtonProps={{ disabled: platformWriteBlocked }}
                  title={zh ? '停止采集并保留历史日志？' : 'Stop collection and retain history?'}
                  onConfirm={() => disable.mutateAsync({ scope, input: { action: 'stop' } })}
                >
                  <Button icon={<PauseOutlined />} disabled={busy}>
                    {zh ? '停止采集' : 'Stop'}
                  </Button>
                </Popconfirm>
              ) : null}
              {canDisable && state?.mode === 'soha_managed' ? (
                <Popconfirm
                  okButtonProps={{ disabled: platformWriteBlocked }}
                  title={
                    zh
                      ? '卸载组件并保留历史存储？'
                      : 'Uninstall components and retain stored history?'
                  }
                  onConfirm={() => disable.mutateAsync({ scope, input: { action: 'uninstall' } })}
                >
                  <Button danger icon={<DeleteOutlined />} disabled={busy}>
                    {zh ? '卸载' : 'Uninstall'}
                  </Button>
                </Popconfirm>
              ) : null}
            </Space>
          ) : null}
        </>
      ) : null}

      <Modal
        okButtonProps={{ disabled: platformWriteBlocked }}
        open={open}
        width={720}
        title={zh ? '日志采集预检' : 'Log collection preflight'}
        onCancel={() => {
          if (!platformWriteBlocked) (() => !enable.isPending && setOpen(false))?.()
        }}
        footer={
          plan ? (
            <Space>
              <Button onClick={() => setPlan(null)} disabled={platformWriteBlocked}>
                {zh ? '返回修改' : 'Back'}
              </Button>
              <Button
                type="primary"
                icon={<CheckCircleOutlined />}
                disabled={
                  platformWriteBlocked || stateQuery.isError || !plan.canEnable || !plan.planToken
                }
                loading={enable.isPending}
                onClick={confirmEnable}
              >
                {zh ? '确认启用' : 'Confirm and enable'}
              </Button>
            </Space>
          ) : (
            <Space>
              <Button onClick={() => setOpen(false)}>{zh ? '取消' : 'Cancel'}</Button>
              <Button type="primary" loading={preflight.isPending} onClick={runPreflight}>
                {zh ? '运行预检' : 'Run preflight'}
              </Button>
            </Space>
          )
        }
      >
        <ReadFeedback canRead={stateQuery.isEnabled} query={stateQuery} label="日志采集状态" />
        <ReadFeedback
          canRead={dataSourcesQuery.isEnabled}
          query={dataSourcesQuery}
          label="日志数据源"
        />
        {preflight.feedback}
        {enable.feedback}
        {disable.feedback}

        {plan ? (
          <Space orientation="vertical" size="middle" className="w-full">
            <Descriptions
              size="small"
              column={1}
              items={[
                { key: 'profile', label: zh ? '方案' : 'Profile', children: plan.profile },
                {
                  key: 'components',
                  label: zh ? '组件' : 'Components',
                  children: plan.components.join(', '),
                },
                {
                  key: 'hostPaths',
                  label: 'Host paths',
                  children: plan.hostPaths.join(', ') || '-',
                },
                {
                  key: 'rbac',
                  label: 'RBAC',
                  children:
                    plan.rbacRules.join(', ') ||
                    (zh ? '无需 Kubernetes API 权限' : 'No Kubernetes API access'),
                },
                {
                  key: 'destination',
                  label: zh ? '目标' : 'Destination',
                  children: plan.destination.endpoint,
                },
                {
                  key: 'retention',
                  label: zh ? '保留期' : 'Retention',
                  children: `${plan.retentionDays}d / ${plan.storageSize}`,
                },
                {
                  key: 'resources',
                  label: zh ? '资源估算' : 'Resources',
                  children: `${plan.resources.cpu} / ${plan.resources.memory}`,
                },
                {
                  key: 'expires',
                  label: zh ? '确认有效期' : 'Confirmation expires',
                  children: formatDateTime(plan.expiresAt),
                },
              ]}
            />
            {plan.blockers.map((item) => (
              <Alert key={item} type="error" showIcon title={displayMessage(item, zh)} />
            ))}
            {plan.warnings.map((item) => (
              <Alert key={item} type="warning" showIcon title={displayMessage(item, zh)} />
            ))}
          </Space>
        ) : (
          <Form<FormValues> form={form} layout="vertical" requiredMark={false}>
            <Form.Item
              name="profile"
              label={zh ? '采集方案' : 'Profile'}
              rules={[{ required: true }]}
            >
              <Segmented
                block
                options={[
                  { label: 'Starter', value: 'starter' },
                  { label: zh ? '仅采集器' : 'Collector only', value: 'collector_only' },
                  {
                    label: zh ? '生产外部' : 'Production external',
                    value: 'production_external',
                  },
                ]}
              />
            </Form.Item>
            <Form.Item
              name="namespace"
              label={zh ? '安装命名空间' : 'Install namespace'}
              rules={[{ required: true }]}
            >
              <Input />
            </Form.Item>
            {profile !== 'starter' ? (
              <>
                <Form.Item
                  name="dataSourceId"
                  label={zh ? 'Loki 数据源' : 'Loki data source'}
                  rules={[{ required: true }]}
                >
                  <Select
                    loading={dataSourcesQuery.isLoading}
                    options={(visibleDataSourcesQueryData ?? [])
                      .filter((item) => item.backendType === 'loki' && item.enabled)
                      .map((item) => ({ label: item.name, value: item.id }))}
                  />
                </Form.Item>
                <Form.Item
                  name="credentialSecretName"
                  label={zh ? '凭据 Secret' : 'Credential Secret'}
                >
                  <Input />
                </Form.Item>
              </>
            ) : null}
            <Form.Item name="namespaceAllowlist" label={zh ? '命名空间范围' : 'Namespace scope'}>
              <Select mode="tags" tokenSeparators={[',']} />
            </Form.Item>
            <Space wrap size="large">
              <Form.Item
                name="retentionDays"
                label={zh ? '保留天数' : 'Retention days'}
                rules={[{ required: true }]}
              >
                <InputNumber min={1} max={365} />
              </Form.Item>
              <Form.Item
                name="storageSize"
                label={zh ? '存储大小' : 'Storage size'}
                rules={[{ required: true }]}
              >
                <Input />
              </Form.Item>
              <Form.Item name="storageClassName" label="StorageClass">
                <Input />
              </Form.Item>
            </Space>
          </Form>
        )}
      </Modal>
    </>
  )
}
