import { readableQueryData } from '@/utils/query-feedback'
import { ReadFeedback } from '@/components/request-feedback'
import { ComputeMutationFeedback } from '@/features/compute'
import { useRef, useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import {
  Alert,
  App,
  Button,
  Drawer,
  Form,
  Input,
  InputNumber,
  Modal,
  Popconfirm,
  Select,
  Space,
  Switch,
  Typography,
} from 'antd'
import type {
  VirtualizationWorkerPool,
  VirtualizationWorkerPoolSpec,
} from '@opensoha/contracts/gen/ts/sohaapi'
import { AdminTable } from '@/components/admin-table'
import { BooleanTag, StatusTag } from '@/components/status-tag'
import { localeText, useI18n } from '@/i18n'
import { virtualizationQueries } from '../queries'
import { virtualizationMutations, withVirtualizationMutationSuccess } from '../mutations'
import { useVirtualizationPermissions } from '../shared/use-virtualization-permissions'
import type { VirtualizationCluster } from '../virtualization-types'
import { formatDateTime } from '@/utils/time'
import { createUUID } from '@/utils/uuid'
import { useAuthStore } from '@/stores/auth-store'

type WorkerCreation = {
  pool: VirtualizationWorkerPool
  key: string
  operationId?: string
  submitted?: boolean
}

function pendingWorkerCreation(storageKey: string): {
  creation: WorkerCreation | null
  error: string
} {
  try {
    const raw = localStorage.getItem(storageKey)
    if (!raw) return { creation: null, error: '' }
    const value = JSON.parse(raw) as WorkerCreation
    if (!value.key || !value.pool?.id || !value.pool.spec || value.pool.revision < 1)
      throw new Error('Invalid saved worker request')
    return { creation: value, error: '' }
  } catch {
    return { creation: null, error: '无法读取本机保留的节点请求，请先在任务列表核对原操作。' }
  }
}

type PoolFields = Omit<VirtualizationWorkerPoolSpec, 'requiredDaemonSets' | 'labels'> & {
  daemons: string
  nodeLabels?: string
}

export function workerPoolSpec(values: PoolFields): VirtualizationWorkerPoolSpec {
  const { daemons, nodeLabels, ...spec } = values
  const requiredDaemonSets = daemons
    .trim()
    .split(/\r?\n/)
    .filter(Boolean)
    .map((line) => {
      const parts = line.trim().split('/')
      if (parts.length !== 2 || parts.some((part) => !part))
        throw new Error('DaemonSet 需要 namespace/name 格式')
      return { namespace: parts[0], name: parts[1] }
    })
  const labels: Record<string, string> = {}
  for (const line of (nodeLabels ?? '').trim().split(/\r?\n/).filter(Boolean)) {
    const split = line.indexOf('=')
    const key = line.slice(0, split).trim()
    if (split < 1 || Object.prototype.hasOwnProperty.call(labels, key))
      throw new Error('节点标签需要唯一的 key=value')
    labels[key] = line.slice(split + 1).trim()
  }
  return { ...spec, requiredDaemonSets, labels }
}

export function WorkerReadiness({ operationId }: { operationId: string }) {
  const { canViewWorkerPools, canViewTasks } = useVirtualizationPermissions()
  const assessment = useQuery(
    virtualizationQueries.workerReadiness(operationId, canViewWorkerPools && canViewTasks),
  )
  const { localeCode } = useI18n()
  if (!canViewWorkerPools || !canViewTasks) return null
  if (assessment.error)
    return (
      <Alert
        type="error"
        title={assessment.error.message}
        action={<Button onClick={() => void assessment.refetch()}>重试</Button>}
      />
    )
  if (!assessment.data)
    return (
      <Typography.Text type="secondary">
        {localeText(localeCode, '读取节点就绪证据…', 'Reading worker evidence…')}
      </Typography.Text>
    )
  return (
    <Space orientation="vertical">
      <Space>
        <StatusTag value={assessment.data.verdict} />
        <Typography.Text>{assessment.data.summary}</Typography.Text>
      </Space>
      <Typography.Text type="secondary">
        {localeText(localeCode, '观测时间', 'Observed at')}:{' '}
        {formatDateTime(assessment.data.evidence[0]?.observedAt)}
      </Typography.Text>
      <Typography.Text type="secondary">
        {localeText(
          localeCode,
          '节点就绪后仍需重新执行交付调度预检。',
          'Repeat delivery scheduling preflight after the worker is ready.',
        )}
      </Typography.Text>
    </Space>
  )
}

export function WorkerPoolsDrawer(props: {
  connection: VirtualizationCluster
  onClose: () => void
}) {
  const userId = useAuthStore((state) => state.user?.userId ?? '')
  return <WorkerPoolsContent key={`${userId}:${props.connection.id}`} {...props} />
}

function WorkerPoolsContent({
  connection,
  onClose,
}: {
  connection: VirtualizationCluster
  onClose: () => void
}) {
  const { localeCode } = useI18n()
  const text = (zh: string, en: string) => localeText(localeCode, zh, en)
  const permissions = useVirtualizationPermissions()
  const queryClient = useQueryClient()
  const { message } = App.useApp()
  const pools = useQuery(
    virtualizationQueries.workerPools(connection.id, permissions.canViewWorkerPools),
  )
  const clusters = useQuery(
    virtualizationQueries.platformClusterOptions(permissions.canViewWorkerPools),
  )
  const images = useQuery(
    virtualizationQueries.images(
      { connectionId: connection.id, pageSize: 500 },
      permissions.canCreateWorkerPools || permissions.canUpdateWorkerPools,
    ),
  )
  const [editing, setEditing] = useState<{ id: string; revision: number } | null>(null)
  const [form] = Form.useForm<PoolFields>()
  const userId = useAuthStore((state) => state.user?.userId ?? '')
  const storageKey = `soha:worker-create:${userId}:${connection.id}`
  const [initialRequest] = useState(() => pendingWorkerCreation(storageKey))
  const [creation, setCreation] = useState<WorkerCreation | null>(initialRequest.creation)
  const [storageError, setStorageError] = useState(initialRequest.error)
  function keepCreation(value: WorkerCreation | null) {
    setCreation(value)
    try {
      if (value) localStorage.setItem(storageKey, JSON.stringify(value))
      else localStorage.removeItem(storageKey)
      setStorageError('')
      return true
    } catch {
      setStorageError(
        text(
          '无法保留节点请求，请恢复本机存储后按原请求重试。',
          'Cannot retain this request. Restore local storage and retry the original request.',
        ),
      )
      return false
    }
  }
  const pendingCreation = useRef(false)
  const saved = useMutation(
    withVirtualizationMutationSuccess(virtualizationMutations.saveWorkerPool(queryClient), () => {
      setEditing(null)
      message.success(text('节点池已保存', 'Worker pool saved'))
    }),
  )
  const removed = useMutation(virtualizationMutations.deleteWorkerPool(queryClient))
  const created = useMutation(virtualizationMutations.createWorker(queryClient))
  const imageItems = Array.isArray(images.data) ? images.data : (images.data?.items ?? [])
  function edit(pool?: VirtualizationWorkerPool) {
    saved.reset()
    form.resetFields()
    setEditing({ id: pool?.id ?? createUUID(), revision: pool?.revision ?? 0 })
    form.setFieldsValue(
      pool
        ? {
            ...pool.spec,
            daemons: pool.spec.requiredDaemonSets
              .map((item) => `${item.namespace}/${item.name}`)
              .join('\n'),
            nodeLabels: Object.entries(pool.spec.labels ?? {})
              .map(([key, value]) => `${key}=${value}`)
              .join('\n'),
          }
        : {
            name: '',
            connectionId: connection.id,
            owner: 'soha-kubeadm',
            osProfile: 'ubuntu-24.04-amd64-containerd',
            cpu: 4,
            memoryMiB: 8192,
            diskGiB: 80,
            maxNodes: 1,
            enabled: false,
          },
    )
  }
  async function save(values: PoolFields) {
    if (!editing) return
    try {
      await saved.mutateAsync({
        id: editing.id,
        input: { expectedRevision: editing.revision, spec: workerPoolSpec(values) },
      })
    } catch (error) {
      if (!(error instanceof Error && saved.error))
        void message.error(error instanceof Error ? error.message : text('保存失败', 'Save failed'))
    }
  }
  async function create() {
    if (
      !creation ||
      creation.operationId ||
      pendingCreation.current ||
      !permissions.canCreateWorkers ||
      !userId
    )
      return
    if (!keepCreation({ ...creation, submitted: true })) return
    pendingCreation.current = true
    try {
      const operation = await created.mutateAsync({
        id: creation.pool.id,
        input: { poolRevision: creation.pool.revision, idempotencyKey: creation.key },
      })
      keepCreation({ ...creation, submitted: true, operationId: operation.id })
    } catch {
      /* The visible mutation error keeps the original key for an explicit retry. */
    } finally {
      pendingCreation.current = false
    }
  }
  const error = pools.error ?? removed.error
  return (
    <Drawer
      open
      title={`${text('节点池', 'Worker pools')} · ${connection.name}`}
      size="large"
      onClose={onClose}
    >
      <Space orientation="vertical" style={{ width: '100%' }} size="middle">
        <Typography.Paragraph type="secondary">
          {text(
            '使用独立 PVE 上预装 containerd、kubeadm/kubelet 的 Ubuntu 24.04 amd64 模板；版本必须与目标 kubeadm 集群一致。保存配置不会创建节点。',
            'Use an independent PVE provider and an Ubuntu 24.04 amd64 template with containerd and matching kubeadm/kubelet. Saving configuration does not create nodes.',
          )}
        </Typography.Paragraph>
        {storageError && <Alert type="error" title={storageError} />}
        {error && (
          <Alert
            type="error"
            title={error.message}
            action={
              <Button
                onClick={() => {
                  removed.reset()
                  void pools.refetch()
                }}
              >
                {text('重试', 'Retry')}
              </Button>
            }
          />
        )}
        <ReadFeedback query={pools} label={text('节点池', 'Worker pools')} />
        <ComputeMutationFeedback
          operations={[
            {
              label: text('保存节点池', 'Save worker pool'),
              mutation: saved,
              receipt: { message: text('节点池记录已保存', 'Worker pool record saved') },
            },
            {
              label: text('删除节点池', 'Delete worker pool'),
              mutation: removed,
              receipt: { message: text('节点池记录已删除', 'Worker pool record deleted') },
            },
            {
              label: text('创建节点', 'Create worker'),
              mutation: created,
              target: creation?.pool.spec.name,
              receipt: {
                message: text(
                  '节点创建请求已受理，就绪状态以观测证据为准',
                  'Worker request accepted; check readiness evidence',
                ),
                taskId: creation?.operationId,
                domain: 'virtualization',
                canViewTask: permissions.canViewTasks,
              },
            },
          ]}
        />
        <AdminTable
          rowKey="id"
          loading={pools.isLoading}
          dataSource={readableQueryData(pools) ?? []}
          pagination={false}
          enableColumnSelection={false}
          headerExtra={
            permissions.canCreateWorkerPools ? (
              <Button type="primary" onClick={() => edit()}>
                {text('新增节点池', 'Add worker pool')}
              </Button>
            ) : null
          }
          columns={[
            {
              title: text('名称', 'Name'),
              key: 'name',
              render: (_: unknown, pool: VirtualizationWorkerPool) => (
                <Space orientation="vertical" size={0}>
                  <Typography.Text>{pool.spec.name}</Typography.Text>
                  <Typography.Text type="secondary">
                    {pool.spec.kubernetesVersion} · r{pool.revision}
                  </Typography.Text>
                </Space>
              ),
            },
            {
              title: text('目标集群', 'Cluster'),
              key: 'cluster',
              render: (_: unknown, pool: VirtualizationWorkerPool) =>
                clusters.data?.find((cluster) => cluster.id === pool.spec.clusterId)?.name ??
                pool.spec.clusterId,
            },
            {
              title: text('节点上限', 'Node limit'),
              key: 'limit',
              render: (_: unknown, pool: VirtualizationWorkerPool) => pool.spec.maxNodes,
            },
            {
              title: text('启用', 'Enabled'),
              key: 'enabled',
              render: (_: unknown, pool: VirtualizationWorkerPool) => (
                <BooleanTag value={pool.spec.enabled} />
              ),
            },
            {
              title: text('操作', 'Actions'),
              key: 'actions',
              render: (_: unknown, pool: VirtualizationWorkerPool) => (
                <Space wrap>
                  {permissions.canCreateWorkers && (
                    <Button
                      size="small"
                      disabled={
                        !pool.spec.enabled || Boolean(creation) || Boolean(storageError) || !userId
                      }
                      onClick={() => {
                        created.reset()
                        keepCreation({ pool, key: createUUID() })
                      }}
                    >
                      {text('创建节点', 'Create worker')}
                    </Button>
                  )}
                  {permissions.canUpdateWorkerPools && (
                    <Button size="small" onClick={() => edit(pool)}>
                      {text('编辑', 'Edit')}
                    </Button>
                  )}
                  {permissions.canDeleteWorkerPools && (
                    <Popconfirm
                      title={text('删除未使用的节点池配置？', 'Delete unused pool configuration?')}
                      description={text(
                        '有操作历史的池应停用并保留。',
                        'Keep pools with operation history disabled.',
                      )}
                      onConfirm={() =>
                        removed.mutateAsync({
                          id: pool.id,
                          revision: pool.revision,
                          connectionId: connection.id,
                        })
                      }
                    >
                      <Button size="small" danger>
                        {text('删除', 'Delete')}
                      </Button>
                    </Popconfirm>
                  )}
                </Space>
              ),
            },
          ]}
        />
        {creation && (
          <Alert
            type="info"
            title={creation.pool.spec.name}
            description={
              <Space orientation="vertical">
                <Typography.Text>
                  {text(
                    `创建 1 个节点：${creation.pool.spec.cpu} CPU / ${creation.pool.spec.memoryMiB} MiB / ${creation.pool.spec.diskGiB} GiB，池版本 ${creation.pool.revision}。`,
                    `Create one worker: ${creation.pool.spec.cpu} CPU / ${creation.pool.spec.memoryMiB} MiB / ${creation.pool.spec.diskGiB} GiB, pool revision ${creation.pool.revision}.`,
                  )}
                </Typography.Text>
                {created.error && (
                  <Typography.Text type="danger">{created.error.message}</Typography.Text>
                )}
                {creation.operationId ? (
                  <>
                    <Typography.Text copyable>{creation.operationId}</Typography.Text>
                    <WorkerReadiness operationId={creation.operationId} />
                    <Button onClick={() => keepCreation(null)}>
                      {text('保留任务并关闭', 'Keep operation and close')}
                    </Button>
                  </>
                ) : (
                  <Space>
                    <Button
                      type="primary"
                      loading={created.isPending}
                      disabled={!permissions.canCreateWorkers || !userId}
                      onClick={() => void create()}
                    >
                      {created.error
                        ? text('按原请求重试', 'Retry original request')
                        : text('确认创建', 'Confirm creation')}
                    </Button>
                    {!creation.submitted && !created.isPending && (
                      <Button onClick={() => keepCreation(null)}>{text('取消', 'Cancel')}</Button>
                    )}
                  </Space>
                )}
              </Space>
            }
          />
        )}
      </Space>
      <Modal
        open={Boolean(editing)}
        title={text('节点池配置', 'Worker pool configuration')}
        onCancel={() => setEditing(null)}
        onOk={() => form.submit()}
        confirmLoading={saved.isPending}
        width={720}
        destroyOnHidden
      >
        <ComputeMutationFeedback
          operations={[{ label: text('保存节点池', 'Save worker pool'), mutation: saved }]}
        />
        {(clusters.error || images.error) && (
          <Alert type="error" title={(clusters.error ?? images.error)?.message} />
        )}
        <Form form={form} layout="vertical" onFinish={(values) => void save(values)}>
          <Form.Item name="connectionId" hidden>
            <Input />
          </Form.Item>
          <Form.Item name="owner" hidden>
            <Input />
          </Form.Item>
          <Form.Item name="osProfile" hidden>
            <Input />
          </Form.Item>
          <Form.Item
            name="name"
            label={text('名称', 'Name')}
            rules={[{ required: true, whitespace: true, max: 100 }]}
          >
            <Input />
          </Form.Item>
          <Form.Item
            name="clusterId"
            label={text('目标 Kubernetes 集群', 'Target Kubernetes cluster')}
            rules={[{ required: true }]}
          >
            <Select
              disabled={Boolean(editing?.revision)}
              options={clusters.data?.map((item) => ({ value: item.id, label: item.name }))}
            />
          </Form.Item>
          <Form.Item
            name="imageId"
            label={text('受信模板镜像', 'Trusted template image')}
            rules={[{ required: true }]}
          >
            <Select options={imageItems.map((item) => ({ value: item.id, label: item.name }))} />
          </Form.Item>
          <Form.Item
            name="kubernetesVersion"
            label={text('Kubernetes 精确版本', 'Exact Kubernetes version')}
            rules={[{ required: true, pattern: /^v1\.\d+\.\d+$/ }]}
          >
            <Input placeholder="v1.x.y" />
          </Form.Item>
          <div className="grid grid-cols-1 gap-x-4 sm:grid-cols-2">
            {(
              [
                ['providerNode', text('PVE 节点', 'PVE node')],
                ['storage', text('根盘存储', 'Root disk storage')],
                ['bridge', text('网络桥接', 'Network bridge')],
                ['snippetStorage', text('Cloud-init 存储', 'Cloud-init storage')],
              ] as const
            ).map(([name, label]) => (
              <Form.Item
                key={name}
                name={name}
                label={label}
                rules={[{ required: true, whitespace: true }]}
              >
                <Input />
              </Form.Item>
            ))}
            {(
              [
                ['cpu', 'CPU', 2, 256],
                ['memoryMiB', text('内存 MiB', 'Memory MiB'), 2048, 1048576],
                ['diskGiB', text('根盘 GiB', 'Root disk GiB'), 20, 16384],
                ['maxNodes', text('节点上限', 'Node limit'), 1, 1000],
              ] as const
            ).map(([name, label, min, max]) => (
              <Form.Item key={name} name={name} label={label} rules={[{ required: true }]}>
                <InputNumber min={min} max={max} precision={0} style={{ width: '100%' }} />
              </Form.Item>
            ))}
          </div>
          <Form.Item
            name="daemons"
            label={text('必需的网络 / 存储 DaemonSet', 'Required network / storage DaemonSets')}
            tooltip={text(
              '每行 namespace/name；将在目标集群验证身份与就绪。',
              'One namespace/name per line; identity and readiness are verified in the target cluster.',
            )}
            rules={[{ required: true, whitespace: true }]}
          >
            <Input.TextArea rows={3} placeholder="kube-system/network-agent" />
          </Form.Item>
          <Form.Item
            name="nodeLabels"
            label={text('节点标签', 'Node labels')}
            tooltip={text(
              '每行 key=value；不允许 Kubernetes 保留标签。',
              'One key=value per line; Kubernetes reserved labels are rejected.',
            )}
          >
            <Input.TextArea rows={2} />
          </Form.Item>
          <Form.Item
            name="enabled"
            label={text('允许创建节点', 'Allow worker creation')}
            valuePropName="checked"
          >
            <Switch />
          </Form.Item>
        </Form>
      </Modal>
    </Drawer>
  )
}
