import { readableQueryData } from '@/utils/query-feedback'
import {
  Alert,
  App,
  Button,
  Card,
  Form,
  Input,
  InputNumber,
  Select,
  Space,
  Spin,
  Typography,
} from 'antd'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import type {
  VirtualMachineAccessBinding,
  VirtualMachineAccessBindingInput,
} from '@opensoha/contracts/gen/ts/sohaapi'
import { hasPermission, usePermissionSnapshot } from '@/features/auth'
import { ApiError } from '@/services/api-error'

import { localeText, useI18n } from '@/i18n'
import { secretQueries, buildSecretReference } from '@/features/secrets'
import { virtualizationQueries } from '../queries'
import { virtualizationMutations } from '../mutations'
import { useVirtualizationPermissions } from '../shared/use-virtualization-permissions'
import { VMSSHTerminal } from './ssh-terminal'
import type { VirtualMachine } from '../virtualization-types'

function AccessForm({
  vm,
  binding,
  canManage,
}: {
  vm: VirtualMachine
  binding?: VirtualMachineAccessBinding
  canManage: boolean
}) {
  const [form] = Form.useForm<VirtualMachineAccessBindingInput>()
  const { message } = App.useApp()
  const client = useQueryClient()
  const permissions = usePermissionSnapshot().data?.data
  const secrets = useQuery(secretQueries.list({}, hasPermission(permissions, 'secret.view')))
  const clusters = useQuery(
    virtualizationQueries.platformClusterOptions(
      hasPermission(permissions, 'platform.clusters.view'),
    ),
  )
  const save = useMutation({
    ...virtualizationMutations.saveAccessBinding(client),
    onError: (error) => message.error(error.message),
    onSuccess: async () => {
      await client.invalidateQueries({
        queryKey: virtualizationQueries.accessBinding(vm.id).queryKey,
      })
      message.success('登录绑定已保存')
    },
  })
  const credentialOptions = (secrets.data ?? [])
    .filter(
      (secret) =>
        secret.status === 'active' &&
        ['ssh_keypair', 'ssh_password'].includes(secret.kind ?? '') &&
        secret.bindings.some(
          (target) =>
            target.targetType === 'vm' &&
            target.targetRef === vm.id &&
            target.purpose === 'vm_auth',
        ),
    )
    .map((secret) => ({
      value: buildSecretReference(secret.id, secret.currentVersion),
      label: `${secret.name} · v${secret.currentVersion}`,
    }))
  return (
    <Form
      form={form}
      layout="vertical"
      disabled={!canManage || save.isPending}
      initialValues={{
        ...binding,
        expectedRevision: binding?.revision ?? 0,
        port: binding?.port ?? 22,
        address: binding?.address ?? vm.ipAddresses?.[0],
        trustedHostKey: '',
      }}
      onFinish={(input) => save.mutate({ id: vm.id, input })}
    >
      <Form.Item name="expectedRevision" hidden>
        <InputNumber />
      </Form.Item>
      <Space align="start" wrap>
        <Form.Item
          name="username"
          label="Guest 登录账号"
          rules={[{ required: true, pattern: /^(?!root$)[a-z_][a-z0-9_-]{0,31}$/ }]}
        >
          <Input autoComplete="off" placeholder="soha" />
        </Form.Item>
        <Form.Item name="address" label="VM IP" rules={[{ required: true }]}>
          <Input />
        </Form.Item>
        <Form.Item
          name="port"
          label="SSH 端口"
          rules={[{ required: true, type: 'number', min: 1, max: 65535 }]}
        >
          <InputNumber min={1} max={65535} precision={0} />
        </Form.Item>
      </Space>
      {credentialOptions.length ? (
        <Form.Item label="选择凭证版本">
          <Select
            showSearch={{ optionFilterProp: 'label' }}
            options={credentialOptions}
            onChange={(ref) => form.setFieldValue('credentialRef', ref)}
          />
        </Form.Item>
      ) : null}
      <Form.Item
        name="credentialRef"
        label="凭证版本引用"
        extra="在凭证管理中允许此 VM 的交互登录用途。保存后固定版本，轮换不会自动更换绑定。"
        rules={[
          {
            required: true,
            pattern: /^soha:\/\/secrets\/[A-Za-z0-9._-]+(?:\/versions\/[1-9][0-9]*)?$/,
          },
        ]}
      >
        <Input placeholder="soha://secrets/…/versions/1" />
      </Form.Item>
      <Form.Item name="executionClusterId" label="SSH 执行节点" rules={[{ required: true }]}>
        {clusters.data?.length ? (
          <Select
            options={clusters.data
              .filter((cluster) => cluster.connectionMode === 'agent')
              .map((cluster) => ({ value: cluster.id, label: cluster.name }))}
          />
        ) : (
          <Input placeholder="已登记的 Agent 集群 ID" />
        )}
      </Form.Item>
      <Form.Item name="executionAgentId" label="执行 Agent ID" rules={[{ required: true }]}>
        <Input placeholder="执行端配置中的 control_plane.agent_id" />
      </Form.Item>
      {binding ? (
        <Typography.Paragraph>
          已登记 host key：<Typography.Text copyable>{binding.hostKeyFingerprint}</Typography.Text>{' '}
          · 绑定修订 {binding.revision}
        </Typography.Paragraph>
      ) : null}
      <Form.Item
        name="trustedHostKey"
        label="已核验的 SSH host 公钥"
        extra="通过可信控制台或管理员独立核验后粘贴完整公钥。修改绑定需要重新确认，不自动信任扫描结果。"
        rules={[{ required: true }]}
      >
        <Input.TextArea rows={3} autoComplete="off" />
      </Form.Item>
      {canManage ? (
        <Button type="primary" htmlType="submit" loading={save.isPending}>
          保存登录绑定
        </Button>
      ) : null}
    </Form>
  )
}

export function VMAccessPanel({ vm }: { vm: VirtualMachine }) {
  const { localeCode } = useI18n()
  const { canConnectVMs, canManageVMAccess } = useVirtualizationPermissions()
  const binding = useQuery(virtualizationQueries.accessBinding(vm.id))
  const absent = binding.error instanceof ApiError && binding.error.status === 404
  const readableBinding = absent ? undefined : readableQueryData(binding)
  return (
    <div className="soha-vm-access">
      <Card
        size="small"
        title={<h2>{localeText(localeCode, 'SSH 登录配置', 'SSH access configuration')}</h2>}
      >
        <p className="soha-vm-access-description">
          {localeText(
            localeCode,
            '配置登录账号、受管凭证与执行节点。保存绑定只更新连接信息，不会修改虚拟机内的账号、公钥或磁盘。',
            'Configure the account, managed credentials and execution agent. Saving updates connection information without changing the VM account, public keys or disks.',
          )}
        </p>
        {binding.isPending ? (
          <Spin />
        ) : binding.error && !absent ? (
          <Alert
            type="error"
            title="登录绑定加载失败"
            description={binding.error.message}
            action={<Button onClick={() => void binding.refetch()}>重试</Button>}
          />
        ) : (
          <AccessForm
            key={readableBinding?.revision ?? 0}
            vm={vm}
            binding={readableBinding}
            canManage={canManageVMAccess}
          />
        )}
      </Card>
      {readableBinding ? (
        <VMSSHTerminal
          key={readableBinding.revision}
          binding={readableBinding}
          enabled={canConnectVMs && vm.status === 'running'}
        />
      ) : (
        <Alert type="info" title="尚未配置 VM 登录绑定" />
      )}
    </div>
  )
}
