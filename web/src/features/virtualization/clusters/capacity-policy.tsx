import { readableQueryData } from '@/utils/query-feedback'
import { ReadFeedback } from '@/components/request-feedback'
import { ComputeMutationFeedback } from '@/features/compute'
import {
  Alert,
  App,
  Button,
  Drawer,
  Form,
  Input,
  InputNumber,
  Select,
  Space,
  Spin,
  Switch,
  Typography,
} from 'antd'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import type {
  VirtualizationCapacityPolicy,
  VirtualizationCapacityPolicyInput,
  VirtualizationCapacityInput,
  VirtualizationCapacityCheckDiagnostic,
} from '@opensoha/contracts/gen/ts/sohaapi'
import { virtualizationQueries } from '../queries'
import { virtualizationMutations } from '../mutations'
import { VirtualizationAdminTable } from '../shared/ui'
import type { VirtualizationCluster } from '../virtualization-types'

function PolicyForm({
  connectionId,
  policy,
  canUpdate,
}: {
  connectionId: string
  policy: VirtualizationCapacityPolicy
  canUpdate: boolean
}) {
  const [form] = Form.useForm<VirtualizationCapacityPolicyInput>()
  const cpuEnabled = Form.useWatch(['cpu', 'enabled'], form)
  const memoryEnabled = Form.useWatch(['memory', 'enabled'], form)
  const client = useQueryClient()
  const { message } = App.useApp()
  const save = useMutation({
    ...virtualizationMutations.saveCapacityPolicy(client),
    onSuccess: async () => {
      await client.invalidateQueries({
        queryKey: virtualizationQueries.capacityPolicy(connectionId).queryKey,
      })
      message.success('容量策略已保存')
    },
  })
  const needsSource = policy.mode === 'unconfigured' || policy.sourceConfirmed === false
  return (
    <Form
      form={form}
      layout="vertical"
      disabled={!canUpdate || save.isPending}
      initialValues={{
        expectedRevision: policy.revision,
        mode: policy.mode === 'unconfigured' ? 'observing' : policy.mode,
        cpu: policy.cpu,
        memory: policy.memory,
      }}
      onFinish={(input) =>
        save.mutate({
          id: connectionId,
          input: {
            ...input,
            cpu: { ...input.cpu, percent: input.cpu.enabled ? input.cpu.percent : 100 },
            memory: { ...input.memory, percent: input.memory.enabled ? input.memory.percent : 100 },
          },
        })
      }
    >
      <ComputeMutationFeedback
        operations={[
          {
            label: '保存容量策略',
            mutation: save,
            target: connectionId,
            receipt: { message: '容量策略记录已保存' },
          },
        ]}
      />
      <Typography.Paragraph type="secondary">
        同一物理 PVE source 的连接共享策略和预留。保存会影响后续创建、启动与扩容；不会修改或停止已有
        VM。
      </Typography.Paragraph>
      {needsSource ? (
        <Form.Item
          name="physicalSourceId"
          label="物理集群标识"
          extra="管理员确认同一 PVE 集群的所有入口使用相同标识；相同标识会继承已保存策略。"
          rules={[{ required: true, pattern: /^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$/ }]}
        >
          <Input placeholder="例如 pve-production" />
        </Form.Item>
      ) : (
        <Typography.Paragraph>
          物理 source：{policy.sourceId} · 修订 {policy.revision}
        </Typography.Paragraph>
      )}
      <Form.Item name="expectedRevision" hidden>
        <InputNumber />
      </Form.Item>
      <Form.Item name="mode" label="准入策略" rules={[{ required: true }]}>
        <Select
          options={[
            { value: 'observing', label: '观察：报告配置预算，仍检查实际容量' },
            { value: 'enforced', label: '强制：两道准入均通过才允许' },
          ]}
        />
      </Form.Item>
      <Space align="start" wrap>
        <Form.Item name={['cpu', 'enabled']} label="允许 CPU 超分" valuePropName="checked">
          <Switch />
        </Form.Item>
        <Form.Item
          name={['cpu', 'percent']}
          label="CPU 配置预算上限 (%)"
          rules={[{ required: true, type: 'number', min: 100, max: 800 }]}
        >
          <InputNumber disabled={!canUpdate || !cpuEnabled} min={100} max={800} precision={0} />
        </Form.Item>
        <Form.Item name={['memory', 'enabled']} label="允许内存超分" valuePropName="checked">
          <Switch />
        </Form.Item>
        <Form.Item
          name={['memory', 'percent']}
          label="内存配置预算上限 (%)"
          rules={[{ required: true, type: 'number', min: 100, max: 150 }]}
        >
          <InputNumber disabled={!canUpdate || !memoryEnabled} min={100} max={150} precision={0} />
        </Form.Item>
      </Space>
      <Alert
        showIcon
        type="info"
        title="超分只放宽配置预算"
        description="实际空闲 CPU、内存与磁盘保持原容量。关闭开关按 100% 检查；已有配置超限时不改 VM，拒绝新增资源需求。"
      />
      {canUpdate ? (
        <Form.Item style={{ marginTop: 16 }}>
          <Button type="primary" htmlType="submit" loading={save.isPending}>
            保存容量策略
          </Button>
        </Form.Item>
      ) : null}
    </Form>
  )
}

export function CapacityPolicyDrawer({
  connection,
  canUpdate,
  onClose,
}: {
  connection: VirtualizationCluster
  canUpdate: boolean
  onClose: () => void
}) {
  const policy = useQuery(virtualizationQueries.capacityPolicy(connection.id))
  const preview = useMutation(virtualizationMutations.checkCapacity())
  const [form] = Form.useForm<VirtualizationCapacityInput>()
  const rows = (preview.data?.diagnostics ?? []).flatMap((node) =>
    node.checks.map((check, index) => ({
      key: `${node.node}/${index}`,
      node: node.node,
      ...check,
    })),
  )
  return (
    <Drawer
      open
      title={`${connection.name} · 容量与超分`}
      onClose={onClose}
      size={760}
      styles={{ wrapper: { maxWidth: '100vw' } }}
      destroyOnHidden
    >
      <Space orientation="vertical" size="large" style={{ width: '100%' }}>
        <ReadFeedback query={policy} label="容量策略" />
        {policy.isPending ? (
          <Spin />
        ) : readableQueryData(policy) ? (
          <PolicyForm
            key={policy.data?.revision}
            connectionId={connection.id}
            policy={readableQueryData(policy)!}
            canUpdate={canUpdate}
          />
        ) : null}
        <ComputeMutationFeedback
          operations={[{ readOnly: true, label: '创建前预检', mutation: preview }]}
        />
        <Typography.Title level={5}>创建前预检</Typography.Title>
        <Form
          form={form}
          layout="vertical"
          initialValues={{ cpu: 2, memoryMiB: 4096, diskGiB: 40, startAfterCreate: true }}
          onValuesChange={() => preview.reset()}
          onFinish={(input) => {
            preview.reset()
            preview.mutate({ ...input, connectionId: connection.id })
          }}
        >
          <Space align="start" wrap>
            <Form.Item name="cpu" label="vCPU" rules={[{ required: true, type: 'number', min: 1 }]}>
              <InputNumber min={1} precision={0} />
            </Form.Item>
            <Form.Item
              name="memoryMiB"
              label="内存 (MiB)"
              rules={[{ required: true, type: 'number', min: 1 }]}
            >
              <InputNumber min={1} precision={0} />
            </Form.Item>
            <Form.Item
              name="diskGiB"
              label="根盘 (GiB)"
              rules={[{ required: true, type: 'number', min: 1 }]}
            >
              <InputNumber min={1} precision={0} />
            </Form.Item>
          </Space>
          <Form.Item name="node" label="节点（可选）">
            <Input />
          </Form.Item>
          <Form.Item name="storage" label="存储（可选）">
            <Input />
          </Form.Item>
          <Form.Item name="startAfterCreate" label="创建后启动" valuePropName="checked">
            <Switch />
          </Form.Item>
          <Button htmlType="submit" loading={preview.isPending}>
            检查两道准入
          </Button>
        </Form>
        {preview.data ? (
          <Alert
            showIcon
            type={preview.data.status === 'available' ? 'success' : 'warning'}
            title={
              preview.data.status === 'available' ? '预检通过，提交时会重新检查' : '当前容量不满足'
            }
            description={preview.data.reason}
          />
        ) : null}
        {rows.length > 0 ? (
          <VirtualizationAdminTable
            rowKey="key"
            dataSource={rows}
            pagination={false}
            showRefresh={false}
            showColumnSettings={false}
            enableDensity={false}
            scroll={{ x: 760 }}
            columns={[
              { title: '节点', dataIndex: 'node' },
              {
                title: '准入',
                dataIndex: 'check',
                render: (value: VirtualizationCapacityCheckDiagnostic['check']) =>
                  ({ budget: '配置预算', runtime: '实际容量', storage: '存储' })[value],
              },
              { title: '资源', dataIndex: 'dimension' },
              { title: '状态', dataIndex: 'status' },
              { title: '上限', dataIndex: 'limit' },
              { title: '已配置', dataIndex: 'committed' },
              { title: '预留', dataIndex: 'pending' },
              { title: '本次', dataIndex: 'demand' },
              { title: '原因', dataIndex: 'reason', width: 240 },
            ]}
          />
        ) : null}
      </Space>
    </Drawer>
  )
}
