import { AdminTable } from '@/components/admin-table'
import { ContextFeedback } from '@/components/context-feedback'
import {
  OperationFeedback,
  ReadFeedback,
  requestErrorMessage,
  useInlineApiError,
} from '@/components/request-feedback'
import { accessQueries } from '@/features/access'
import { useI18n } from '@/i18n'
import { queryAccessDenied, readableQueryData } from '@/utils/query-feedback'
import type {
  NetworkAccessNetwork,
  NetworkAccessNetworkDecision,
  NetworkAccessNetworkInput,
  NetworkAccessNetworkPreviewInput,
} from '@opensoha/contracts/gen/ts/sohaapi'
import { useMutation, useQuery } from '@tanstack/react-query'
import { AutoComplete, Form, Modal, Select, Tag } from 'antd'
import { authenticationMethods } from './access-network-editor'
import { networkAccessMutations } from './mutations'
import { networkAccessQueries } from './queries'

type PreviewSubject = Pick<
  NetworkAccessNetworkPreviewInput,
  'userId' | 'deviceId' | 'authenticationMethod'
>

export function AccessNetworkPreview({
  siteId,
  networks,
  draft,
  canViewUsers,
  canViewDevices,
  onClose,
}: {
  siteId: string
  networks: NetworkAccessNetwork[]
  draft?: NetworkAccessNetworkInput
  canViewUsers: boolean
  canViewDevices: boolean
  onClose: () => void
}) {
  const { localeCode } = useI18n()
  const text = (zh: string, en: string) => (localeCode === 'zh_CN' ? zh : en)
  const [form] = Form.useForm<PreviewSubject>()
  const userId = (Form.useWatch('userId', form) ?? '').trim()
  const users = useQuery(accessQueries.users(canViewUsers))
  const devices = useQuery(
    networkAccessQueries.devices(
      { siteId, ownerUserId: userId, limit: 200 },
      canViewDevices && Boolean(userId),
    ),
  )
  const preview = useMutation(networkAccessMutations.accessNetworks.preview())
  useInlineApiError(preview.error)
  const rank = new Map(networks.map((item) => [item.id, item.configuration.priority]))
  if (draft) rank.set('draft', draft.priority)
  const rows = [...(preview.data ?? [])].sort(
    (a, b) =>
      Number(b.allowed) - Number(a.allowed) ||
      (rank.get(a.accessNetworkId) ?? Infinity) - (rank.get(b.accessNetworkId) ?? Infinity) ||
      a.name.localeCompare(b.name),
  )
  const reasons: Record<string, string> = {
    access_network_allowed: text('命中允许规则', 'An allow rule matched'),
    access_network_disabled: text('网络已停用', 'Network is disabled'),
    access_network_requires_other_admission: text(
      '此网络不授予 802.1X 接入',
      'Network grants no 802.1X access',
    ),
    site_admission_denied: text('站点准入策略拒绝', 'Site admission denied'),
    access_network_nas_unavailable: text(
      '站点无可用的关联 NAS',
      'No associated NAS is available at this site',
    ),
    access_network_explicit_deny: text('命中拒绝规则', 'A deny rule matched'),
    site_profile_binding_unavailable: text(
      '缺少有效的 RADIUS 权限档位映射',
      'No effective RADIUS profile mapping',
    ),
    access_network_device_proof_required: text(
      '设备规则需要 EAP-TLS 设备凭据',
      'Device rules require EAP-TLS proof',
    ),
    access_network_missing_allow: text('未命中任何允许规则', 'No allow rule matched'),
  }
  const previewTitle = draft
    ? text('预览草稿授权', 'Preview draft authorization')
    : text('预览接入授权', 'Preview network authorization')
  const filterOption = (input: string, option?: { label?: unknown; value?: unknown }) =>
    `${option?.label} ${option?.value}`.toLocaleLowerCase().includes(input.toLocaleLowerCase())

  return (
    <Modal
      open
      title={previewTitle}
      width={720}
      okText={text('运行预览', 'Run preview')}
      cancelText={text('关闭', 'Close')}
      confirmLoading={preview.isPending}
      onCancel={onClose}
      onOk={() => form.submit()}
    >
      <p>
        {draft
          ? draft.kind === 'standard'
            ? text(
                '模拟启用后的规则，草稿保持未保存。',
                'Simulates the rules as if enabled. The draft stays unsaved.',
              )
            : text('仅评估此草稿，配置不会保存。', 'Evaluates this draft without saving it.')
          : text(
              '评估此站点已保存的接入网络。',
              'Evaluates the saved access networks at this site.',
            )}
      </p>
      <Form
        form={form}
        layout="vertical"
        initialValues={{ authenticationMethod: 'eap_tls' }}
        disabled={preview.isPending}
        onValuesChange={(changed) => {
          if ('userId' in changed) form.setFieldValue('deviceId', undefined)
          preview.reset()
        }}
        onFinish={(values) =>
          preview.mutate({
            siteId,
            userId: values.userId.trim(),
            deviceId: values.deviceId.trim(),
            authenticationMethod: values.authenticationMethod,
            ...(draft ? { draft } : {}),
          })
        }
      >
        <Form.Item
          name="userId"
          label={text('用户', 'User')}
          rules={[{ required: true, whitespace: true }]}
        >
          <AutoComplete
            options={(canViewUsers ? (readableQueryData(users) ?? []) : []).map((user) => ({
              value: user.id,
              label: `${user.displayName || user.username} · ${user.id}`,
            }))}
            filterOption={filterOption}
            placeholder={text('选择用户或输入已知 ID', 'Choose a user or enter a known ID')}
            maxLength={128}
          />
        </Form.Item>
        <Form.Item
          name="deviceId"
          label={text('终端', 'Endpoint')}
          rules={[{ required: true, whitespace: true }]}
          extra={text(
            '终端须属于所选用户和站点。',
            'The endpoint must belong to the selected user and site.',
          )}
        >
          <AutoComplete
            options={(devices.isEnabled ? (readableQueryData(devices) ?? []) : []).map(
              (device) => ({
                value: device.id,
                label: `${device.name} · ${device.id}`,
              }),
            )}
            filterOption={filterOption}
            placeholder={text('选择终端或输入已知 ID', 'Choose an endpoint or enter a known ID')}
            maxLength={128}
          />
        </Form.Item>
        <Form.Item name="authenticationMethod" label={text('认证方式', 'Authentication method')}>
          <Select options={authenticationMethods} />
        </Form.Item>
      </Form>
      <ReadFeedback query={users} label={text('用户选项', 'User choices')} />
      <ReadFeedback query={devices} label={text('终端选项', 'Endpoint choices')} />
      <OperationFeedback
        readOnly
        pending={preview.isPending}
        submittedAt={preview.submittedAt}
        label={previewTitle}
      />
      {preview.error && (
        <ContextFeedback
          type="error"
          showIcon
          title={
            queryAccessDenied(preview)
              ? text('无权预览此站点的授权', 'You do not have permission to preview this site')
              : text('预览失败', 'Preview failed')
          }
          description={requestErrorMessage(preview.error, localeCode)}
        />
      )}
      {preview.isSuccess && (
        <section role="status" aria-label={text('预览结果', 'Preview results')}>
          <AdminTable
            rowKey="accessNetworkId"
            pagination={false}
            enableColumnSelection={false}
            dataSource={rows}
            empty={text('此站点没有接入网络。', 'This site has no access networks.')}
            columns={[
              { title: text('接入网络', 'Access network'), dataIndex: 'name' },
              {
                title: text('授权结果', 'Decision'),
                dataIndex: 'allowed',
                render: (allowed: boolean) => (
                  <Tag color={allowed ? 'success' : 'error'}>
                    {allowed ? text('允许', 'Allowed') : text('拒绝', 'Denied')}
                  </Tag>
                ),
              },
              {
                title: text('原因', 'Reason'),
                dataIndex: 'reasonCode',
                render: (reason: string, item: NetworkAccessNetworkDecision) => {
                  const label = reasons[reason] ?? reason
                  return item.matchedRuleIndex
                    ? `${label} · ${text('规则', 'Rule')} ${item.matchedRuleIndex}`
                    : label
                },
              },
              {
                title: text('推荐优先级', 'Priority'),
                key: 'priority',
                render: (_: unknown, item: NetworkAccessNetworkDecision) =>
                  item.allowed ? (rank.get(item.accessNetworkId) ?? '—') : '—',
              },
            ]}
            scroll={{ x: 500 }}
          />
        </section>
      )}
    </Modal>
  )
}
